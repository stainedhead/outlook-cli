package usecase

import (
	"context"
	"errors"
	"testing"

	"github.com/stainedhead/outlook-cli/internal/domain"
)

func TestMarkRead(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.addMessage("m1", "f-inbox")
	e.addMessage("arch", "f-arch")
	if err := e.cmds.MarkRead(ctx, "m1", true); err != nil || !e.w.reads["m1"] {
		t.Fatalf("mark read: %v", err)
	}
	if err := e.cmds.MarkRead(ctx, "m1", false); err != nil || e.w.reads["m1"] {
		t.Fatalf("mark unread: %v", err)
	}
	if a := e.lastAudit(t); a.Verb != domain.VerbWrite || a.Resource != "mail.mark" {
		t.Errorf("audit = %+v", a)
	}
	if err := e.cmds.MarkRead(ctx, "arch", true); exitOf(err) != 6 {
		t.Errorf("forbidden folder: %v", err)
	}
	if err := e.cmds.MarkRead(ctx, "zzz", true); exitOf(err) != 5 {
		t.Errorf("missing: %v", err)
	}
	if err := e.cmds.MarkRead(ctx, "", true); exitOf(err) != 2 {
		t.Errorf("empty: %v", err)
	}
	e.w.readErr = errors.New("boom")
	if err := e.cmds.MarkRead(ctx, "m1", true); err == nil {
		t.Error("adapter error swallowed")
	}
	e2 := newEnv(t, func(p *domain.Policy) { p.Limits.MaxWritesPerRun = 1 })
	e2.addMessage("m1", "f-inbox")
	if err := e2.cmds.MarkRead(ctx, "m1", true); err != nil {
		t.Fatal(err)
	}
	if err := e2.cmds.MarkRead(ctx, "m1", false); exitOf(err) != 6 || ruleOf(err) != domain.RuleWritesPerRun {
		t.Errorf("write cap: %v", err)
	}
}

func TestMove(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.addMessage("m1", "f-inbox")
	e.addMessage("arch", "f-arch")
	res, err := e.cmds.Move(ctx, MoveRequest{MessageID: "m1", Folder: "Processed"})
	if err != nil || res.NewID != "moved-m1" || res.Folder.ID != "f-proc" || len(e.w.moves) != 1 || e.w.moves[0] != [2]string{"m1", "f-proc"} {
		t.Fatalf("res=%+v err=%v", res, err)
	}
	if a := e.lastAudit(t); a.Resource != "mail.move" || a.Outcome != "ok" {
		t.Errorf("audit = %+v", a)
	}
	cases := []struct {
		name string
		req  MoveRequest
		exit int
		rule string
	}{
		{"deleted items", MoveRequest{MessageID: "m1", Folder: "Deleted Items"}, 6, domain.RuleMoveDeletedItems},
		{"deleted alias", MoveRequest{MessageID: "m1", Folder: "deleteditems"}, 6, domain.RuleMoveDeletedItems},
		{"unlisted folder", MoveRequest{MessageID: "m1", Folder: "Archive"}, 6, domain.RuleMoveFolder},
		{"source not readable", MoveRequest{MessageID: "arch", Folder: "Processed"}, 6, domain.RuleReadFolders},
		{"unknown target", MoveRequest{MessageID: "m1", Folder: "Nope"}, 5, ""},
		{"missing message", MoveRequest{MessageID: "zzz", Folder: "Processed"}, 5, ""},
		{"no folder", MoveRequest{MessageID: "m1"}, 2, ""},
	}
	for _, c := range cases {
		before := e.w.total()
		_, err := e.cmds.Move(ctx, c.req)
		if exitOf(err) != c.exit || (c.rule != "" && ruleOf(err) != c.rule) || e.w.total() != before {
			t.Errorf("%s: exit %d rule %q err %v", c.name, exitOf(err), ruleOf(err), err)
		}
	}
	e.w.moveErr = errors.New("boom")
	if _, err := e.cmds.Move(ctx, MoveRequest{MessageID: "m1", Folder: "Processed"}); err == nil {
		t.Error("adapter error swallowed")
	}
	e2 := newEnv(t, func(p *domain.Policy) { p.Limits.MaxWritesPerRun = 0 })
	e2.addMessage("m1", "f-inbox")
	if _, err := e2.cmds.Move(ctx, MoveRequest{MessageID: "m1", Folder: "Processed"}); exitOf(err) != 6 {
		t.Errorf("write cap: %v", err)
	}
}

func TestMoveNeverToDeletedEvenIfPolicyListsIt(t *testing.T) {
	e := newEnv(t, func(p *domain.Policy) { p.Read.Folders = append(p.Read.Folders, "deleteditems", "Deleted Items") })
	e.addMessage("m1", "f-inbox")
	_, err := e.cmds.Move(context.Background(), MoveRequest{MessageID: "m1", Folder: "Deleted Items"})
	if exitOf(err) != 6 || len(e.w.moves) != 0 {
		t.Errorf("err=%v moves=%v", err, e.w.moves)
	}
}
