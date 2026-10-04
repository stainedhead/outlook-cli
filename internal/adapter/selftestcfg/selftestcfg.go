package selftestcfg

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/agent-cli-core/selftest"
	"github.com/stainedhead/outlook-cli/internal/domain"
	"github.com/stainedhead/outlook-cli/internal/usecase"
)

// Resources used in rows; they match the audit resource names.
const (
	ResMailList      = "mail.list"
	ResMailSearch    = "mail.search"
	ResAttachmentGet = "attachment.get"
	ResMailSend      = "mail.send"
	ResMailReply     = "mail.reply"
	ResMailMove      = "mail.move"
)

// Fixed probe inputs. The external domain is reserved (RFC 2606) so even a
// probe that escaped dry-run could not reach a real recipient. The AWS key is
// the documented example key, not a credential.
const (
	externalAddr  = "selftest@external.invalid"
	exampleSecret = "AKIAIOSFODNN7EXAMPLE"
	probeID       = "selftest-nonexistent-id"
	probeSubject  = "outlook selftest"
)

type testCase struct {
	row selftest.Row
	run func(ctx context.Context, c usecase.Commands) error
}

// Rows returns the matrix for p in a stable order.
func Rows(p domain.Policy) []selftest.Row {
	cs := cases(p)
	rows := make([]selftest.Row, len(cs))
	for i, c := range cs {
		rows[i] = c.row
	}
	return rows
}

// Probe returns the selftest probe that executes each row of Rows(p) against c.
func Probe(c usecase.Commands, p domain.Policy) selftest.Probe {
	byName := map[string]testCase{}
	for _, tc := range cases(p) {
		byName[tc.row.Name] = tc
	}
	return func(ctx context.Context, row selftest.Row) (selftest.Outcome, error) {
		tc, ok := byName[row.Name]
		if !ok {
			return "", fmt.Errorf("selftest: unknown row %q", row.Name)
		}
		err := tc.run(ctx, c)
		if err == nil {
			return selftest.Allow, nil
		}
		var ce output.CategoryError
		if errors.As(err, &ce) && ce.Category() == output.CategoryPolicyDenied {
			return selftest.Deny, nil
		}
		return "", err
	}
}

// Runner returns a ready selftest.Runner. Live mode sets readOnly so write
// rows are skipped.
func Runner(c usecase.Commands, p domain.Policy, readOnly bool) selftest.Runner {
	return selftest.Runner{Rows: Rows(p), Probe: Probe(c, p), ReadOnly: readOnly}
}

func outcome(allow bool) selftest.Outcome {
	if allow {
		return selftest.Allow
	}
	return selftest.Deny
}

func cases(p domain.Policy) []testCase {
	var out []testCase
	add := func(name string, verb domain.Verb, res string, expect selftest.Outcome, readOnly bool,
		run func(context.Context, usecase.Commands) error) {
		out = append(out, testCase{
			row: selftest.Row{Name: name, Verb: string(verb), Resource: res, Expect: expect, ReadOnly: readOnly},
			run: run,
		})
	}

	// ---- read ----
	if len(p.Read.Folders) > 0 {
		f := p.Read.Folders[0]
		add("read.folder.allowed", domain.VerbRead, ResMailList, selftest.Allow, true,
			func(ctx context.Context, c usecase.Commands) error {
				_, err := c.ListMessages(ctx, usecase.ListRequest{Folder: f, Limit: 1})
				return err
			})
	}
	unlisted := unlistedFolder(p.Read.Folders)
	add("read.folder.unlisted", domain.VerbRead, ResMailList, selftest.Deny, true,
		func(ctx context.Context, c usecase.Commands) error {
			_, err := c.ListMessages(ctx, usecase.ListRequest{Folder: unlisted, Limit: 1})
			return err
		})
	add("read.search.unlisted-folder", domain.VerbRead, ResMailSearch, selftest.Deny, true,
		func(ctx context.Context, c usecase.Commands) error {
			_, err := c.SearchMessages(ctx, usecase.SearchRequest{Query: "selftest", Folder: unlisted, Limit: 1})
			return err
		})
	add("attachment.outside-quarantine", domain.VerbRead, ResAttachmentGet, selftest.Deny, true,
		func(ctx context.Context, c usecase.Commands) error {
			_, err := c.GetAttachment(ctx, usecase.AttachmentRequest{MessageID: probeID, AttachmentID: probeID, OutDir: "/"})
			return err
		})

	// ---- send / write ----
	sendOK := p.Send.Mode == domain.SendAllow || p.Send.Mode == domain.SendDryRunOnly
	allowed, haveAllowed := allowedRecipient(p)
	base := allowed
	if !haveAllowed {
		base = fallbackInternal(p)
	}
	send := func(r usecase.SendRequest) func(context.Context, usecase.Commands) error {
		r.Subject, r.Body, r.DryRun = probeSubject, "selftest body", true
		return func(ctx context.Context, c usecase.Commands) error {
			_, err := c.Send(ctx, r)
			return err
		}
	}

	if haveAllowed {
		add("send.allowed-recipient.dry-run", domain.VerbSend, ResMailSend, outcome(sendOK), false,
			send(usecase.SendRequest{To: []string{allowed}}))
	}
	extExpect := outcome(sendOK && p.Send.Recipients.External != domain.ExternalDeny)
	add("send.external-recipient.dry-run", domain.VerbSend, ResMailSend, extExpect, false,
		send(usecase.SendRequest{To: []string{externalAddr}}))
	if !p.Send.Recipients.BccAllowed {
		add("send.bcc.dry-run", domain.VerbSend, ResMailSend, selftest.Deny, false,
			send(usecase.SendRequest{To: []string{base}, Bcc: []string{base}}))
	}
	if n := p.Send.Recipients.MaxTotal; n > 0 && n < 1000 {
		to := make([]string, n+1)
		for i := range to {
			to[i] = indexed(base, i)
		}
		add("send.over-max-total.dry-run", domain.VerbSend, ResMailSend, selftest.Deny, false,
			send(usecase.SendRequest{To: to}))
	}
	if !p.Send.Attachments {
		add("send.attachments.dry-run", domain.VerbSend, ResMailSend, selftest.Deny, false,
			send(usecase.SendRequest{To: []string{base}, HasAttachments: true}))
	}
	if has(p.Send.ContentFilters, "secret_patterns") {
		add("send.content-filter.secret", domain.VerbSend, ResMailSend, selftest.Deny, false,
			func(ctx context.Context, c usecase.Commands) error {
				_, err := c.Send(ctx, usecase.SendRequest{To: []string{base}, Subject: probeSubject, Body: "key " + exampleSecret, DryRun: true})
				return err
			})
	}
	if has(p.Send.ContentFilters, "classification_markers") {
		add("send.content-filter.classification", domain.VerbSend, ResMailSend, selftest.Deny, false,
			func(ctx context.Context, c usecase.Commands) error {
				_, err := c.Send(ctx, usecase.SendRequest{To: []string{base}, Subject: probeSubject, Body: "CONFIDENTIAL: do not distribute", DryRun: true})
				return err
			})
	}
	if !p.Send.ReplyAll {
		add("reply.all", domain.VerbSend, ResMailReply, selftest.Deny, false,
			func(ctx context.Context, c usecase.Commands) error {
				_, err := c.Reply(ctx, usecase.ReplyRequest{MessageID: probeID, Body: "selftest", All: true, DryRun: true})
				return err
			})
	}
	add("move.deleted-items", domain.VerbWrite, ResMailMove, selftest.Deny, false,
		func(ctx context.Context, c usecase.Commands) error {
			_, err := c.Move(ctx, usecase.MoveRequest{MessageID: probeID, Folder: "Deleted Items"})
			return err
		})
	return out
}

func has(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// unlistedFolder picks a well-known folder that policy read.folders does not
// list, case-insensitively.
func unlistedFolder(listed []string) string {
	for _, cand := range []string{"deleteditems", "junkemail", "sentitems", "drafts", "archive"} {
		found := false
		for _, l := range listed {
			if strings.EqualFold(strings.ReplaceAll(l, " ", ""), cand) {
				found = true
			}
		}
		if !found {
			return cand
		}
	}
	return "selftest-unlisted-folder"
}

// allowedRecipient returns an address the policy's recipient allowlist
// accepts, if the policy has any allowlist entry.
func allowedRecipient(p domain.Policy) (string, bool) {
	r := p.Send.Recipients
	if len(r.AllowAddresses) > 0 {
		return r.AllowAddresses[0], true
	}
	if len(r.AllowDomains) > 0 {
		return "selftest@" + r.AllowDomains[0], true
	}
	return "", false
}

func fallbackInternal(p domain.Policy) string {
	if len(p.InternalDomains) > 0 {
		return "selftest@" + p.InternalDomains[0]
	}
	return "selftest@internal.invalid"
}

// indexed derives distinct addresses on the same domain as base.
func indexed(base string, i int) string {
	at := strings.LastIndexByte(base, '@')
	return fmt.Sprintf("selftest-%d%s", i, base[at:])
}
