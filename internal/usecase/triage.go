package usecase

import (
	"context"
	"strings"

	"github.com/stainedhead/outlook-cli/internal/domain"
)

// MarkRead implements FR-012. The message must live in a readable folder.
func (s *service) MarkRead(ctx context.Context, messageID string, read bool) error {
	return s.exec(ctx, domain.VerbWrite, "mail.mark", func(*call) error {
		p, e := s.begin(ctx)
		if e != nil {
			return e
		}
		if _, e = s.readableMessage(ctx, p, messageID, false); e != nil {
			return e
		}
		if e = s.countWrite(p); e != nil {
			return e
		}
		return s.d.Writer.SetRead(ctx, messageID, read)
	})
}

// Move implements FR-013: only into a policy-readable folder, never into
// Deleted Items, and only for messages in a readable folder.
func (s *service) Move(ctx context.Context, r MoveRequest) (res MoveResult, err error) {
	err = s.exec(ctx, domain.VerbWrite, "mail.move", func(*call) error {
		p, e := s.begin(ctx)
		if e != nil {
			return e
		}
		name := strings.TrimSpace(r.Folder)
		if name == "" {
			return domain.NewUsage("--folder is required")
		}
		target, e := s.d.Reader.ResolveFolder(ctx, name)
		if e != nil {
			return e
		}
		if d := p.EvalMove(target); !d.Allowed() {
			return domain.ErrFor(d)
		}
		if _, e = s.readableMessage(ctx, p, r.MessageID, false); e != nil {
			return e
		}
		if e = s.countWrite(p); e != nil {
			return e
		}
		newID, e := s.d.Writer.MoveMessage(ctx, r.MessageID, target.ID)
		if e != nil {
			return e
		}
		target.Name = domain.CleanText(target.Name)
		res = MoveResult{NewID: newID, Folder: target}
		return nil
	})
	return res, err
}
