package usecase

import (
	"context"
	"sort"
	"strings"

	"github.com/stainedhead/outlook-cli/internal/domain"
)

// maxRawBodyBytes bounds how much of a hostile body is parsed.
const maxRawBodyBytes = 2 << 20

// Whoami implements FR-001.
func (s *service) Whoami(ctx context.Context) (res WhoamiResult, err error) {
	err = s.exec(ctx, domain.VerbRead, "whoami", func(*call) error {
		p, e := s.begin(ctx)
		if e != nil {
			return e
		}
		res = WhoamiResult{
			Mailbox:    p.Mailbox,
			AgentID:    s.d.Run.AgentID,
			RunID:      s.d.Run.RunID,
			Profile:    p.Profile,
			PolicyPath: s.d.PolicyPath,
			Limits: domain.Limits{
				MaxResults:      p.EffectiveMaxResults(0),
				MaxWritesPerRun: p.Limits.MaxWritesPerRun,
			},
			SendMode: p.Send.Mode,
			External: p.Send.Recipients.External,
			MaxTotal: p.Send.Recipients.MaxTotal,
			Rate:     p.Send.Rate,
		}
		if res.SendMode == "" {
			res.SendMode = domain.SendDeny
		}
		if res.External == "" {
			res.External = domain.ExternalDeny
		}
		return nil
	})
	return res, err
}

// ListFolders implements FR-003. Only folders named by policy read.folders are
// returned, so a hidden folder is never revealed.
func (s *service) ListFolders(ctx context.Context) (out []domain.Folder, err error) {
	err = s.exec(ctx, domain.VerbRead, "folder.list", func(*call) error {
		p, e := s.begin(ctx)
		if e != nil {
			return e
		}
		all, e := s.d.Reader.ListFolders(ctx)
		if e != nil {
			return e
		}
		for _, f := range all {
			if p.FolderAllowed(f) {
				f.Name = domain.CleanText(f.Name)
				out = append(out, f)
			}
		}
		return nil
	})
	return out, err
}

// ListMessages implements FR-004.
func (s *service) ListMessages(ctx context.Context, r ListRequest) (page domain.Page[domain.MessageSummary], err error) {
	err = s.exec(ctx, domain.VerbRead, "mail.list", func(*call) error {
		p, e := s.begin(ctx)
		if e != nil {
			return e
		}
		name := strings.TrimSpace(r.Folder)
		if name == "" {
			name = string(domain.WellKnownInbox)
		}
		f, e := s.resolveReadable(ctx, p, name)
		if e != nil {
			return e
		}
		q := domain.MessageQuery{
			FolderID:   f.ID,
			UnreadOnly: r.UnreadOnly,
			From:       strings.TrimSpace(r.From),
			Since:      r.Since,
			Limit:      p.EffectiveMaxResults(r.Limit),
			PageToken:  r.PageToken,
		}
		pg, e := s.d.Reader.ListMessages(ctx, q)
		if e != nil {
			return e
		}
		page = s.cleanPage(p, pg)
		return nil
	})
	return page, err
}

func (s *service) cleanPage(p domain.Policy, pg domain.Page[domain.MessageSummary]) domain.Page[domain.MessageSummary] {
	items := make([]domain.MessageSummary, len(pg.Items))
	for i, m := range pg.Items {
		items[i] = s.cleanSummary(p, m)
	}
	return domain.Page[domain.MessageSummary]{Items: items, NextPageToken: pg.NextPageToken}
}

// SearchMessages implements FR-006. Without --folder every policy-readable
// folder is searched and the merged result is newest first; pagination is then
// unavailable (a page token requires --folder).
func (s *service) SearchMessages(ctx context.Context, r SearchRequest) (page domain.Page[domain.MessageSummary], err error) {
	err = s.exec(ctx, domain.VerbRead, "mail.search", func(*call) error {
		p, e := s.begin(ctx)
		if e != nil {
			return e
		}
		if strings.TrimSpace(r.Query) == "" {
			return domain.NewUsage("search query is empty")
		}
		limit := p.EffectiveMaxResults(r.Limit)
		var folders []domain.Folder
		if name := strings.TrimSpace(r.Folder); name != "" {
			f, e := s.resolveReadable(ctx, p, name)
			if e != nil {
				return e
			}
			folders = []domain.Folder{f}
		} else {
			if folders, e = s.readableFolders(ctx, p); e != nil {
				return e
			}
			if len(folders) == 0 {
				return domain.ErrFor(domain.Decision{Mode: domain.DecisionDeny, RuleID: domain.RuleReadFolders,
					Reason: "no folder is readable under policy read.folders"})
			}
			if len(folders) > 1 && r.PageToken != "" {
				return domain.NewUsage("--page-token requires --folder when more than one folder is readable")
			}
		}
		var merged []domain.MessageSummary
		next := ""
		for _, f := range folders {
			pg, e := s.d.Reader.SearchMessages(ctx, domain.SearchQuery{
				Text: r.Query, FolderID: f.ID, Limit: limit, PageToken: r.PageToken,
			})
			if e != nil {
				return e
			}
			merged = append(merged, pg.Items...)
			if len(folders) == 1 {
				next = pg.NextPageToken
			}
		}
		if len(folders) > 1 {
			sort.SliceStable(merged, func(i, j int) bool { return merged[i].Received.After(merged[j].Received) })
		}
		if len(merged) > limit {
			merged = merged[:limit]
		}
		page = s.cleanPage(p, domain.Page[domain.MessageSummary]{Items: merged, NextPageToken: next})
		return nil
	})
	return page, err
}

// GetMessage implements FR-005 and FR-016: it applies the folder policy, turns
// the body into bounded text, extracts and defangs links, computes
// sender_trust and attachment downloadability, and surfaces auth results.
func (s *service) GetMessage(ctx context.Context, r GetRequest) (msg domain.Message, err error) {
	err = s.exec(ctx, domain.VerbRead, "mail.get", func(*call) error {
		p, e := s.begin(ctx)
		if e != nil {
			return e
		}
		raw, e := s.readableMessage(ctx, p, r.ID, true)
		if e != nil {
			return e
		}
		msg = s.buildMessage(p, raw, r)
		return nil
	})
	return msg, err
}

func (s *service) buildMessage(p domain.Policy, raw domain.RawMessage, r GetRequest) domain.Message {
	m := domain.Message{
		MessageSummary: s.cleanSummary(p, raw.MessageSummary),
		Cc:             cleanAddresses(raw.Cc),
		Attachments:    s.markAttachments(p, raw.Attachments),
		AuthResults:    domain.ParseAuthResultsFor(raw.InternetHeaders, p.Read.TrustedAuthservIDs),
	}
	if r.BodyFormat == domain.BodyNone {
		m.Body = domain.Body{Format: domain.BodyNone}
		return m
	}
	m.Body, m.Links = s.convertBody(p, raw.Body, p.EffectiveBodyBytes(r.MaxBytes))
	return m
}

// convertBody turns a raw body into bounded, cleaned text and its links.
func (s *service) convertBody(p domain.Policy, rb domain.RawBody, maxBytes int) (domain.Body, []domain.Link) {
	content, cut := domain.TruncateBytes(rb.Content, maxRawBodyBytes)
	defang := p.Read.DefangLinks
	var text string
	var links []domain.Link
	if rb.Format == domain.BodyHTML {
		text, links = domain.HTMLToText(content, defang)
	} else {
		text = domain.CleanText(content)
		links = domain.LinksFromText(text, defang)
		if defang {
			text = domain.DefangText(text)
		}
	}
	text, cut2 := domain.TruncateBytes(text, maxBytes)
	return domain.Body{Format: domain.BodyText, Text: text, Truncated: cut || cut2}, links
}

func cleanAddresses(in []domain.Address) []domain.Address {
	if len(in) == 0 {
		return nil
	}
	out := make([]domain.Address, len(in))
	for i, a := range in {
		out[i] = domain.Address{Address: lower(a.Address), Name: domain.CleanText(a.Name)}
	}
	return out
}

// markAttachments cleans names and sets Downloadable from policy.
func (s *service) markAttachments(p domain.Policy, in []domain.Attachment) []domain.Attachment {
	if len(in) == 0 {
		return nil
	}
	out := make([]domain.Attachment, len(in))
	for i, a := range in {
		a.Name = domain.CleanText(a.Name)
		a.Downloadable = p.EvalAttachmentDownload(a).Allowed() && s.d.Quarantine != nil
		out[i] = a
	}
	return out
}
