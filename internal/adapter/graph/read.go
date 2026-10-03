package graph

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/outlook-cli/internal/domain"
)

const (
	summarySelect    = "id,conversationId,receivedDateTime,from,toRecipients,subject,isRead,isDraft,hasAttachments"
	folderSelect     = "id,displayName,unreadItemCount,totalItemCount"
	attachmentSelect = "id,name,contentType,size,isInline"
	defaultLimit     = 25
	maxLimit         = 1000
)

var wellKnownAliases = []domain.WellKnown{
	domain.WellKnownInbox, domain.WellKnownDrafts, domain.WellKnownSentItems,
	domain.WellKnownDeletedItems, domain.WellKnownArchive, domain.WellKnownJunk, domain.WellKnownOutbox,
}

func clampLimit(n int) int {
	switch {
	case n <= 0:
		return defaultLimit
	case n > maxLimit:
		return maxLimit
	}
	return n
}

// Me returns the signed-in profile.
//
// ASSUMPTION(unverified against a real tenant): GET /me?$select=mail,userPrincipalName;
// mail may be null for some accounts.
func (c *Client) Me(ctx context.Context) (domain.Profile, error) {
	var p profileDTO
	if err := c.getJSON(ctx, c.meURL(query("$select", "mail,userPrincipalName")), nil, &p, "profile not found"); err != nil {
		return domain.Profile{}, err
	}
	return domain.Profile{Mail: p.Mail, UserPrincipalName: p.UserPrincipalName}, nil
}

// ListFolders returns all top-level mail folders with counts.
//
// ASSUMPTION(unverified against a real tenant): GET /me/mailFolders with
// $top=100 and @odata.nextLink paging; well-known folders are identified by
// resolving each alias (GET /me/mailFolders/{alias}?$select=id) and matching
// ids, because the listing itself carries no alias.
func (c *Client) ListFolders(ctx context.Context) ([]domain.Folder, error) {
	next := c.meURL(query("$top", "100", "$select", folderSelect), "mailFolders")
	var out []domain.Folder
	for next != "" {
		var page folderListDTO
		if err := c.getJSON(ctx, next, nil, &page, "mail folders not found"); err != nil {
			return nil, err
		}
		for _, f := range page.Value {
			out = append(out, folderFromDTO(f, domain.WellKnownNone))
		}
		next = page.NextLink
	}
	ids, err := c.wellKnownIDs(ctx)
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].WellKnown = ids[out[i].ID]
	}
	return out, nil
}

func folderFromDTO(f folderDTO, wk domain.WellKnown) domain.Folder {
	return domain.Folder{ID: f.ID, Name: f.DisplayName, WellKnown: wk, UnreadCount: f.UnreadItemCount, TotalCount: f.TotalItemCount}
}

// wellKnownIDs maps folder id to alias, fetched once per Client. A missing
// alias folder (404, for example Archive) is skipped.
func (c *Client) wellKnownIDs(ctx context.Context) (map[string]domain.WellKnown, error) {
	c.mu.Lock()
	cached := c.wellKnown
	c.mu.Unlock()
	if cached != nil {
		return cached, nil
	}
	ids := make(map[string]domain.WellKnown, len(wellKnownAliases))
	for _, a := range wellKnownAliases {
		var f folderDTO
		err := c.getJSON(ctx, c.meURL(query("$select", "id"), "mailFolders", seg(string(a))), nil, &f, "folder not found")
		if err != nil {
			if isNotFound(err) {
				continue
			}
			return nil, err
		}
		ids[f.ID] = a
	}
	c.mu.Lock()
	c.wellKnown = ids
	c.mu.Unlock()
	return ids, nil
}

func isNotFound(err error) bool {
	return output.CategoryOf(err) == output.CategoryNotFound
}

// aliasFor normalises a user supplied name to a well-known alias.
func aliasFor(name string) (domain.WellKnown, bool) {
	n := strings.NewReplacer(" ", "", "_", "", "-", "").Replace(strings.ToLower(strings.TrimSpace(name)))
	if n == "junk" {
		n = string(domain.WellKnownJunk)
	}
	for _, a := range wellKnownAliases {
		if n == string(a) {
			return a, true
		}
	}
	return "", false
}

// ResolveFolder finds a folder by alias or display name.
//
// ASSUMPTION(unverified against a real tenant): GET /me/mailFolders/{alias}
// accepts the Graph well-known names; custom folders are matched by
// case-insensitive displayName among top-level folders.
func (c *Client) ResolveFolder(ctx context.Context, name string) (domain.Folder, error) {
	if strings.TrimSpace(name) == "" {
		return domain.Folder{}, domain.NewValidation("folder name is empty")
	}
	if a, ok := aliasFor(name); ok {
		var f folderDTO
		if err := c.getJSON(ctx, c.meURL(query("$select", folderSelect), "mailFolders", seg(string(a))), nil, &f, "folder not found: "+name); err != nil {
			return domain.Folder{}, err
		}
		return folderFromDTO(f, a), nil
	}
	folders, err := c.ListFolders(ctx)
	if err != nil {
		return domain.Folder{}, err
	}
	for _, f := range folders {
		if strings.EqualFold(f.Name, strings.TrimSpace(name)) {
			return f, nil
		}
	}
	return domain.Folder{}, domain.NewNotFound("folder not found: " + name)
}

func (c *Client) listMessages(ctx context.Context, first, token string) (domain.Page[domain.MessageSummary], error) {
	target := first
	if token != "" {
		var err error
		if target, err = c.decodeToken(token); err != nil {
			return domain.Page[domain.MessageSummary]{}, err
		}
	}
	var dto messageListDTO
	if err := c.getJSON(ctx, target, nil, &dto, "folder not found"); err != nil {
		return domain.Page[domain.MessageSummary]{}, err
	}
	page := domain.Page[domain.MessageSummary]{NextPageToken: encodeToken(dto.NextLink)}
	for _, m := range dto.Value {
		page.Items = append(page.Items, m.summary())
	}
	return page, nil
}

func odataString(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

// ListMessages lists a folder, newest first.
//
// ASSUMPTION(unverified against a real tenant): GET /me/mailFolders/{id}/messages
// with $top, $orderby=receivedDateTime desc and $select. Graph rejects
// $orderby on a property that is not in $filter when a $filter is present
// (InefficientFilter), so a filter always starts with receivedDateTime ge
// (the epoch when no Since is given). The sender filter uses
// from/emailAddress/address eq '...'.
func (c *Client) ListMessages(ctx context.Context, q domain.MessageQuery) (domain.Page[domain.MessageSummary], error) {
	if q.PageToken != "" {
		return c.listMessages(ctx, "", q.PageToken)
	}
	if err := requireID("folder", q.FolderID); err != nil {
		return domain.Page[domain.MessageSummary]{}, err
	}
	var conds []string
	if !q.Since.IsZero() || q.UnreadOnly || q.From != "" {
		since := q.Since
		if since.IsZero() {
			since = time.Unix(0, 0)
		}
		conds = append(conds, "receivedDateTime ge "+since.UTC().Format("2006-01-02T15:04:05Z"))
	}
	if q.UnreadOnly {
		conds = append(conds, "isRead eq false")
	}
	if q.From != "" {
		conds = append(conds, "from/emailAddress/address eq "+odataString(q.From))
	}
	qs := query("$top", strconv.Itoa(clampLimit(q.Limit)), "$orderby", "receivedDateTime desc",
		"$select", summarySelect, "$filter", strings.Join(conds, " and "))
	return c.listMessages(ctx, c.meURL(qs, "mailFolders", seg(q.FolderID), "messages"), "")
}

// SearchMessages runs a free-text search.
//
// ASSUMPTION(unverified against a real tenant): $search="..." on
// /me/messages (or /me/mailFolders/{id}/messages) cannot be combined with
// $orderby or $filter, returns results in relevance order, and pages with
// @odata.nextLink.
func (c *Client) SearchMessages(ctx context.Context, q domain.SearchQuery) (domain.Page[domain.MessageSummary], error) {
	if q.PageToken != "" {
		return c.listMessages(ctx, "", q.PageToken)
	}
	if strings.TrimSpace(q.Text) == "" {
		return domain.Page[domain.MessageSummary]{}, domain.NewValidation("search text is empty")
	}
	text := strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(q.Text)
	qs := query("$search", `"`+text+`"`, "$top", strconv.Itoa(clampLimit(q.Limit)), "$select", summarySelect)
	if q.FolderID != "" {
		return c.listMessages(ctx, c.meURL(qs, "mailFolders", seg(q.FolderID), "messages"), "")
	}
	return c.listMessages(ctx, c.meURL(qs, "messages"), "")
}

// GetMessage returns one message.
//
// ASSUMPTION(unverified against a real tenant): GET /me/messages/{id} with
// $select (body, ccRecipients, internetMessageId, parentFolderId, and
// internetMessageHeaders when wanted), $expand=attachments($select=...) and
// the header Prefer: outlook.body-content-type="text".
func (c *Client) GetMessage(ctx context.Context, id string, wantHeaders bool) (domain.RawMessage, error) {
	if err := requireID("message", id); err != nil {
		return domain.RawMessage{}, err
	}
	sel := summarySelect + ",body,ccRecipients,bccRecipients,internetMessageId,parentFolderId"
	if wantHeaders {
		sel += ",internetMessageHeaders"
	}
	qs := query("$select", sel, "$expand", "attachments($select="+attachmentSelect+")")
	var m messageDTO
	hdr := map[string]string{"Prefer": `outlook.body-content-type="text"`}
	if err := c.getJSON(ctx, c.meURL(qs, "messages", seg(id)), hdr, &m, "message not found"); err != nil {
		return domain.RawMessage{}, err
	}
	return m.raw(), nil
}

// ListAttachments returns attachment metadata.
//
// ASSUMPTION(unverified against a real tenant): GET /me/messages/{id}/attachments?$select=...
func (c *Client) ListAttachments(ctx context.Context, messageID string) ([]domain.Attachment, error) {
	if err := requireID("message", messageID); err != nil {
		return nil, err
	}
	var l attachmentListDTO
	if err := c.getJSON(ctx, c.meURL(query("$select", attachmentSelect), "messages", seg(messageID), "attachments"), nil, &l, "message not found"); err != nil {
		return nil, err
	}
	out := make([]domain.Attachment, 0, len(l.Value))
	for _, a := range l.Value {
		out = append(out, a.toDomain())
	}
	return out, nil
}

// OpenAttachment fetches metadata, then streams the raw content.
//
// ASSUMPTION(unverified against a real tenant): GET
// /me/messages/{id}/attachments/{aid}?$select=... for metadata and
// GET .../attachments/{aid}/$value for the raw bytes (file attachments only).
func (c *Client) OpenAttachment(ctx context.Context, messageID, attachmentID string) (domain.Attachment, io.ReadCloser, error) {
	if err := requireID("message", messageID); err != nil {
		return domain.Attachment{}, nil, err
	}
	if err := requireID("attachment", attachmentID); err != nil {
		return domain.Attachment{}, nil, err
	}
	var a attachmentDTO
	base := c.meURL("", "messages", seg(messageID), "attachments", seg(attachmentID))
	meta := c.meURL(query("$select", attachmentSelect), "messages", seg(messageID), "attachments", seg(attachmentID))
	if err := c.getJSON(ctx, meta, nil, &a, "attachment not found"); err != nil {
		return domain.Attachment{}, nil, err
	}
	resp, err := c.do(ctx, http.MethodGet, base+"/$value", map[string]string{"Accept": "*/*"}, nil, "attachment not found")
	if err != nil {
		return domain.Attachment{}, nil, err
	}
	return a.toDomain(), resp.Body, nil
}

// ListDrafts lists the Drafts folder.
//
// ASSUMPTION(unverified against a real tenant): the "drafts" alias works as a
// folder id in /me/mailFolders/drafts/messages; drafts are ordered by
// lastModifiedDateTime desc.
func (c *Client) ListDrafts(ctx context.Context, limit int, pageToken string) (domain.Page[domain.MessageSummary], error) {
	if pageToken != "" {
		return c.listMessages(ctx, "", pageToken)
	}
	qs := query("$top", strconv.Itoa(clampLimit(limit)), "$orderby", "lastModifiedDateTime desc", "$select", summarySelect)
	return c.listMessages(ctx, c.meURL(qs, "mailFolders", "drafts", "messages"), "")
}
