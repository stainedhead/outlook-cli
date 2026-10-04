package cli

import (
	"regexp"
	"time"

	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/outlook-cli/internal/domain"
	"github.com/stainedhead/outlook-cli/internal/usecase"
)

// The presenter marks everything derived from sender or tenant content as
// untrusted: Subject, Body.Text, Address.Name, Attachment.Name, Link.Text,
// Link.URL, Link.Domain, folder names, and any address or content type that is
// not well formed (FR-R10). Every wrapped value passes through CleanText
// (FR-R5). Ids, well-formed addresses, dates and sizes stay plain.

type obj = map[string]any

func ts(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func untrusted(value, author string, at time.Time) output.Untrusted {
	if !at.IsZero() {
		at = at.UTC()
	}
	return output.Untrusted{Value: domain.CleanText(value), Author: author, Timestamp: at}
}

func presentAddr(a domain.Address, at time.Time) obj {
	o := obj{}
	if _, err := domain.ParseAddress(a.Address); err != nil {
		o["address"] = untrusted(a.Address, a.Address, at)
		o["address_flag"] = "non_conforming"
	} else {
		o["address"] = a.Address
	}
	if a.Name != "" {
		o["name"] = untrusted(a.Name, a.Address, at)
	}
	return o
}

func presentAddrs(as []domain.Address, at time.Time) []obj {
	out := make([]obj, 0, len(as))
	for _, a := range as {
		out = append(out, presentAddr(a, at))
	}
	return out
}

func presentSummary(m domain.MessageSummary) obj {
	return obj{
		"id":              m.ID,
		"conversation_id": m.ConversationID,
		"received":        ts(m.Received),
		"from":            presentAddr(m.From, m.Received),
		"to":              presentAddrs(m.To, m.Received),
		"subject":         untrusted(m.Subject, m.From.Address, m.Received),
		"sender_trust":    string(m.SenderTrust),
		"is_read":         m.IsRead,
		"is_draft":        m.IsDraft,
		"has_attachments": m.HasAttachments,
	}
}

// presentPage renders a page as a JSON ARRAY so core's output bounding can cut
// it by whole items (an object cannot be cut: ErrBoundTooSmall). The Graph
// continuation, which core's Meta cannot carry, is a final element
// {"next_page_token": "..."}; when the output is truncated that element is cut
// too, and the caller resumes with --offset until it arrives.
func presentPage(p domain.Page[domain.MessageSummary]) []any {
	out := make([]any, 0, len(p.Items)+1)
	for _, m := range p.Items {
		out = append(out, presentSummary(m))
	}
	if p.NextPageToken != "" {
		out = append(out, obj{"next_page_token": p.NextPageToken})
	}
	return out
}

func presentMessage(m domain.Message) obj {
	o := presentSummary(m.MessageSummary)
	o["cc"] = presentAddrs(m.Cc, m.Received)
	body := obj{"format": string(m.Body.Format), "truncated": m.Body.Truncated}
	if m.Body.Format != domain.BodyNone {
		body["text"] = untrusted(m.Body.Text, m.From.Address, m.Received)
	}
	o["body"] = body
	links := make([]obj, 0, len(m.Links))
	for _, l := range m.Links {
		lo := obj{
			"url":    untrusted(l.URL, m.From.Address, m.Received),
			"domain": untrusted(l.Domain, m.From.Address, m.Received),
		}
		if l.Text != "" {
			lo["text"] = untrusted(l.Text, m.From.Address, m.Received)
		}
		links = append(links, lo)
	}
	o["links"] = links
	o["attachments"] = presentAttachments(m.Attachments, m.From.Address)
	if m.AuthResults != nil {
		o["auth_results"] = obj{"spf": m.AuthResults.SPF, "dkim": m.AuthResults.DKIM, "dmarc": m.AuthResults.DMARC}
	}
	return o
}

func presentAttachment(a domain.Attachment, author string) obj {
	return obj{
		"id": a.ID, "name": untrusted(a.Name, author, time.Time{}),
		"content_type": presentContentType(a.ContentType, author), "size": a.Size,
		"is_inline": a.IsInline, "downloadable": a.Downloadable,
	}
}

var contentTypeRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9!#$&^_.+-]{0,126}/[A-Za-z0-9][A-Za-z0-9!#$&^_.+-]{0,126}$`)

// presentContentType leaves a bare type/subtype token plain and wraps anything
// else (parameters, prose) as untrusted.
func presentContentType(ct, author string) any {
	if ct == "" || contentTypeRe.MatchString(ct) {
		return ct
	}
	return untrusted(ct, author, time.Time{})
}

func presentAttachments(as []domain.Attachment, author string) []obj {
	out := make([]obj, 0, len(as))
	for _, a := range as {
		out = append(out, presentAttachment(a, author))
	}
	return out
}

func presentFolder(f domain.Folder) obj {
	return obj{"id": f.ID, "name": untrusted(f.Name, "", time.Time{}), "well_known": string(f.WellKnown), "unread": f.UnreadCount, "total": f.TotalCount}
}

func presentFolders(fs []domain.Folder) []obj {
	out := make([]obj, 0, len(fs))
	for _, f := range fs {
		out = append(out, presentFolder(f))
	}
	return out
}

func presentDraft(d domain.Draft) obj {
	o := presentSummary(d.MessageSummary)
	o["id"] = d.ID
	return o
}

func presentOutgoing(m domain.OutgoingMessage) obj {
	addrs := func(as []domain.Address) []string {
		out := make([]string, 0, len(as))
		for _, a := range as {
			out = append(out, a.Address)
		}
		return out
	}
	return obj{"to": addrs(m.To), "cc": addrs(m.Cc), "bcc": addrs(m.Bcc), "subject": m.Subject, "body": m.Body}
}

func presentSend(r domain.SendResult) obj {
	o := obj{
		"dry_run": r.DryRun, "already_sent": r.AlreadySent,
		"decision": r.Decision.AuditString(), "rendered": presentOutgoing(r.Rendered),
	}
	if r.DraftID != "" {
		o["draft_id"] = r.DraftID
	}
	if r.IdempotencyKey != "" {
		o["idempotency_key"] = r.IdempotencyKey
	}
	return o
}

func presentWhoami(w usecase.WhoamiResult) obj {
	return obj{
		"mailbox": w.Mailbox, "agent_id": w.AgentID, "run_id": w.RunID, "profile": w.Profile,
		"send_mode": string(w.SendMode), "external": string(w.External), "max_recipients": w.MaxTotal,
		"rate":   obj{"per_hour": w.Rate.PerHour, "per_day": w.Rate.PerDay},
		"limits": obj{"max_results": w.Limits.MaxResults, "max_writes_per_run": w.Limits.MaxWritesPerRun},
	}
}
