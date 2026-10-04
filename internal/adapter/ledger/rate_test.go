package ledger_test

import (
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/outlook-cli/internal/adapter/ledger"
	"github.com/stainedhead/outlook-cli/internal/usecase"
)

func win(cap int) []usecase.RateWindow {
	return []usecase.RateWindow{{Since: t0.Add(-time.Hour), Cap: cap}, {Since: t0.Add(-24 * time.Hour), Cap: 0}}
}

func TestFRR8ConcurrentReserveWithinCapOneExactlyOne(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state", "ledger.json")
	var created, over int32
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// A separate File per goroutine mimics separate processes.
			l, err := ledger.New(ledger.Config{Path: p})
			if err != nil {
				t.Error(err)
				return
			}
			key := string(rune('a' + i))
			e, c, _, err := l.ReserveWithin(ctx, key, "fp", usecase.LedgerKindSend, t0, win(1))
			if err != nil {
				t.Error(err)
				return
			}
			if c {
				atomic.AddInt32(&created, 1)
			} else if e.Status == usecase.LedgerOverCap {
				atomic.AddInt32(&over, 1)
			}
		}(i)
	}
	wg.Wait()
	if created != 1 || over != 15 {
		t.Fatalf("created=%d over=%d", created, over)
	}
}

func TestFRR8PendingCountsAndDraftsDoNot(t *testing.T) {
	l, _ := newLedger(t)
	if _, c, counts, err := l.ReserveWithin(ctx, "s1", "f", usecase.LedgerKindSend, t0, win(2)); err != nil || !c || len(counts) != 2 || counts[0] != 0 {
		t.Fatal(c, counts, err)
	}
	// ambiguous send stays pending but still counts
	if _, _, counts, _ := l.ReserveWithin(ctx, "d1", "f", usecase.LedgerKindDraft, t0, win(2)); counts != nil {
		t.Fatalf("draft counts %v", counts)
	}
	if n, _ := l.SentSince(ctx, t0.Add(-time.Hour)); n != 1 {
		t.Fatalf("SentSince %d: pending send counts, draft does not", n)
	}
	_ = l.Complete(ctx, "d1", "draftid", t0)
	if n, _ := l.SentSince(ctx, t0.Add(-time.Hour)); n != 1 {
		t.Fatalf("completed draft must not count: %d", n)
	}
	_, c, counts, err := l.ReserveWithin(ctx, "s2", "f", usecase.LedgerKindSend, t0, win(2))
	if err != nil || !c || counts[0] != 1 {
		t.Fatal(c, counts, err)
	}
	e, c, counts, err := l.ReserveWithin(ctx, "s3", "f", usecase.LedgerKindSend, t0, win(2))
	if err != nil || c || e.Status != usecase.LedgerOverCap || counts[0] != 2 || counts[1] != 2 {
		t.Fatalf("%+v %v %v %v", e, c, counts, err)
	}
	// nothing written for the over-cap key
	if e, _, _ := l.Reserve(ctx, "s3", "f", t0); e.Status != usecase.LedgerNew {
		t.Fatalf("over-cap wrote an entry: %+v", e)
	}
}

func TestFRR8ExistingKeyReturnedUntouchedAndKindPersisted(t *testing.T) {
	l, _ := newLedger(t)
	_, _, _, _ = l.ReserveWithin(ctx, "d", "f", usecase.LedgerKindDraft, t0, nil)
	e, c, counts, err := l.ReserveWithin(ctx, "d", "f", usecase.LedgerKindDraft, t0, win(1))
	if err != nil || c || counts != nil || e.Kind != usecase.LedgerKindDraft || e.Status != usecase.LedgerPending {
		t.Fatalf("%+v %v %v %v", e, c, counts, err)
	}
	_ = l.Complete(ctx, "d", "id1", t0)
	e, _, _, _ = l.ReserveWithin(ctx, "d", "f", usecase.LedgerKindDraft, t0, nil)
	if e.Status != usecase.LedgerSent || e.RefID != "id1" || e.Kind != usecase.LedgerKindDraft {
		t.Fatalf("%+v", e)
	}
	// failed entry is reset to pending with the new kind
	_ = l.Fail(ctx, "d", t0)
	e, c, _, _ = l.ReserveWithin(ctx, "d", "f", usecase.LedgerKindSend, t0, win(5))
	if !c || e.Kind != usecase.LedgerKindSend || e.Status != usecase.LedgerPending {
		t.Fatalf("%+v %v", e, c)
	}
}

func TestFRR8EmptyKeyAndLegacyEntry(t *testing.T) {
	l, p := newLedger(t)
	if _, _, _, err := l.ReserveWithin(ctx, "", "f", usecase.LedgerKindSend, t0, nil); output.CategoryOf(err) != output.CategoryValidation {
		t.Fatal(err)
	}
	_, _, _ = l.Reserve(ctx, "old", "f", t0) // plain Reserve is a send
	if n, _ := l.SentSince(ctx, t0); n != 1 {
		t.Fatal(n)
	}
	// legacy file without kind counts as send
	legacy := `{"version":1,"entries":{"x":{"fingerprint":"f","status":"sent","at":"2026-10-03T12:00:00Z"}}}`
	if err := os.WriteFile(p, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	if n, _ := l.SentSince(ctx, t0); n != 1 {
		t.Fatal(n)
	}
	if e, _, _ := l.Reserve(ctx, "x", "f", t0); e.Kind != usecase.LedgerKindSend {
		t.Fatalf("%+v", e)
	}
	bad := `{"version":1,"entries":{"x":{"fingerprint":"f","status":"sent","kind":"weird","at":"2026-10-03T12:00:00Z"}}}`
	_ = os.WriteFile(p, []byte(bad), 0o600)
	if _, _, _, err := l.ReserveWithin(ctx, "y", "f", usecase.LedgerKindSend, t0, nil); err == nil {
		t.Fatal("unknown kind must fail closed")
	}
}

func TestFRR13LedgerDirPermsAndOwnerRefused(t *testing.T) {
	dir := t.TempDir()
	mk := func(mode os.FileMode, uid int) error {
		_, err := ledger.New(ledger.Config{
			Path:    filepath.Join(dir, "ledger.json"),
			DirInfo: func(string) (os.FileMode, int, error) { return mode, uid, nil },
			UID:     func() int { return 1000 },
		})
		return err
	}
	if err := mk(0o700|os.ModeDir, 1000); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]struct {
		mode os.FileMode
		uid  int
	}{"group-readable": {0o750, 1000}, "world": {0o777, 1000}, "other owner": {0o700, 0}} {
		err := mk(c.mode, c.uid)
		if output.CategoryOf(err) != output.CategoryGeneral {
			t.Errorf("%s: %v", name, err)
			continue
		}
		if de, ok := err.(interface{ Error() string }); !ok || de.Error() == "" {
			t.Errorf("%s: no message", name)
		}
	}
}

func TestFRR13RealDirChecksAtOpenAndUse(t *testing.T) {
	root := t.TempDir()
	d := filepath.Join(root, "loose")
	if err := os.Mkdir(d, 0o755); err != nil {
		t.Fatal(err)
	}
	_ = os.Chmod(d, 0o755)
	if _, err := ledger.New(ledger.Config{Path: filepath.Join(d, "l.json")}); output.CategoryOf(err) != output.CategoryGeneral {
		t.Fatalf("0755 dir must be refused: %v", err)
	}
	// Missing directory is fine at open (created 0700 on first write); also
	// re-checked at use if it is loosened later.
	l, err := ledger.New(ledger.Config{Path: filepath.Join(root, "new", "l.json")})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := l.Reserve(ctx, "k", "f", t0); err != nil {
		t.Fatal(err)
	}
	_ = os.Chmod(filepath.Join(root, "new"), 0o755)
	if _, _, err := l.Reserve(ctx, "k2", "f", t0); output.CategoryOf(err) != output.CategoryGeneral {
		t.Fatalf("loosened dir must be refused at use: %v", err)
	}
}
