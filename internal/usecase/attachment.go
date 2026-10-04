package usecase

import (
	"context"

	"github.com/stainedhead/outlook-cli/internal/domain"
)

// ListAttachments implements FR-014: metadata only, with Downloadable derived
// from policy.
func (s *service) ListAttachments(ctx context.Context, messageID string) (out []domain.Attachment, err error) {
	err = s.exec(ctx, domain.VerbRead, "attachment.list", func(*call) error {
		p, e := s.begin(ctx)
		if e != nil {
			return e
		}
		if _, e = s.readableMessage(ctx, p, messageID, false); e != nil {
			return e
		}
		atts, e := s.d.Reader.ListAttachments(ctx, messageID)
		if e != nil {
			return e
		}
		out = s.markAttachments(p, atts)
		return nil
	})
	return out, err
}

// GetAttachment implements FR-015: off unless policy enables it; type and size
// are checked from metadata before any content is requested; the content goes
// straight to the quarantine store and is never opened or interpreted.
func (s *service) GetAttachment(ctx context.Context, r AttachmentRequest) (res AttachmentResult, err error) {
	err = s.exec(ctx, domain.VerbRead, "attachment.get", func(c *call) error {
		p, e := s.begin(ctx)
		if e != nil {
			return e
		}
		if s.d.Quarantine == nil {
			return domain.ErrFor(domain.Decision{Mode: domain.DecisionDeny, RuleID: domain.RuleAttachmentDownload,
				Reason: "attachment download is not available in this build"})
		}
		if !p.Read.Attachments.Download {
			return domain.ErrFor(p.EvalAttachmentDownload(domain.Attachment{}))
		}
		outDir, d := p.ResolveOutDir(r.OutDir)
		if !d.Allowed() {
			return domain.ErrFor(d)
		}
		if r.AttachmentID == "" {
			return domain.NewUsage("attachment id is required")
		}
		if _, e = s.readableMessage(ctx, p, r.MessageID, false); e != nil {
			return e
		}
		atts, e := s.d.Reader.ListAttachments(ctx, r.MessageID)
		if e != nil {
			return e
		}
		var meta *domain.Attachment
		for i := range atts {
			if atts[i].ID == r.AttachmentID {
				meta = &atts[i]
				break
			}
		}
		if meta == nil {
			return domain.NewNotFound("attachment not found on that message")
		}
		if d := p.EvalAttachmentDownload(*meta); !d.Allowed() {
			c.setDecision(d)
			return domain.ErrFor(d)
		}
		opened, rc, e := s.d.Reader.OpenAttachment(ctx, r.MessageID, r.AttachmentID)
		if e != nil {
			return e
		}
		defer func() { _ = rc.Close() }()
		// The adapter reports metadata first; re-check what it actually declares.
		if d := p.EvalAttachmentDownload(opened); !d.Allowed() {
			c.setDecision(d)
			return domain.ErrFor(d)
		}
		path, written, e := s.d.Quarantine.Save(ctx, outDir, domain.CleanText(opened.Name), rc, p.Read.Attachments.MaxBytes)
		if e != nil {
			return e
		}
		opened.Name = domain.CleanText(opened.Name)
		opened.Downloadable = true
		res = AttachmentResult{Attachment: opened, Path: path, Written: written}
		return nil
	})
	return res, err
}
