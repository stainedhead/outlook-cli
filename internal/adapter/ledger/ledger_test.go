package ledger_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/outlook-cli/internal/adapter/ledger"
	"github.com/stainedhead/outlook-cli/internal/usecase"
)

var (
	ctx = context.Background()
	t0  = time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
)

func newLedger(t *testing.T) (*ledger.File, string) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "state", "ledger.json")
	l, err := ledger.New(ledger.Config{Path: p})
	if err != nil {
		t.Fatal(err)
	}
	return l, p
}

func TestNewRequiresPath(t *testing.T) {
	if _, err := ledger.New(ledger.Config{}); output.CategoryOf(err) != output.CategoryUsage {
		t.Fatal(err)
	}
}

func TestReserveNewThenPendingThenSent(t *testing.T) {
	l, p := newLedger(t)
	e, created, err := l.Reserve(ctx, "k1", "fp1", t0)
	if err != nil || !created || e.Status != usecase.LedgerNew || e.Key != "k1" || e.Fingerprint != "fp1" || !e.At.Equal(t0) {
		t.Fatalf("%+v %v %v", e, created, err)
	}
	e, created, err = l.Reserve(ctx, "k1", "other", t0.Add(time.Minute))
	if err != nil || created || e.Status != usecase.LedgerPending || e.Fingerprint != "fp1" || !e.At.Equal(t0) {
		t.Fatalf("existing entry must be returned untouched: %+v %v %v", e, created, err)
	}
	if err := l.Complete(ctx, "k1", "ref9", t0.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	e, created, err = l.Reserve(ctx, "k1", "fp1", t0.Add(3*time.Minute))
	if err != nil || created || e.Status != usecase.LedgerSent || e.RefID != "ref9" || !e.At.Equal(t0.Add(2*time.Minute)) {
		t.Fatalf("%+v %v %v", e, created, err)
	}
	st, err := os.Stat(p)
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("ledger mode %v %v", st, err)
	}
	if d, _ := os.Stat(filepath.Dir(p)); d.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode %v", d.Mode())
	}
}

func TestFailedKeyIsResetOnReserve(t *testing.T) {
	l, _ := newLedger(t)
	_, _, _ = l.Reserve(ctx, "k", "fp", t0)
	if err := l.Fail(ctx, "k", t0.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	e, created, err := l.Reserve(ctx, "k", "fp2", t0.Add(time.Minute))
	if err != nil || !created || e.Status != usecase.LedgerPending || e.Fingerprint != "fp2" || !e.At.Equal(t0.Add(time.Minute)) {
		t.Fatalf("%+v %v %v", e, created, err)
	}
}

func TestCompleteAndFailUnknownKeyAreNotFound(t *testing.T) {
	l, _ := newLedger(t)
	if err := l.Complete(ctx, "nope", "", t0); output.CategoryOf(err) != output.CategoryNotFound {
		t.Fatal(err)
	}
	if err := l.Fail(ctx, "nope", t0); output.CategoryOf(err) != output.CategoryNotFound {
		t.Fatal(err)
	}
}

func TestReserveEmptyKey(t *testing.T) {
	l, _ := newLedger(t)
	if _, _, err := l.Reserve(ctx, "", "fp", t0); output.CategoryOf(err) != output.CategoryValidation {
		t.Fatal(err)
	}
}

func TestSentSinceCountsOnlySentAtOrAfter(t *testing.T) {
	l, _ := newLedger(t)
	for i, k := range []string{"a", "b", "c", "d"} {
		_, _, _ = l.Reserve(ctx, k, "fp", t0)
		at := t0.Add(time.Duration(i) * time.Hour)
		switch k {
		case "a", "b", "c":
			_ = l.Complete(ctx, k, "", at)
		case "d":
			_ = l.Fail(ctx, k, at)
		}
	}
	_, _, _ = l.Reserve(ctx, "pending", "fp", t0.Add(5*time.Hour))
	for since, want := range map[time.Time]int{t0: 3, t0.Add(time.Hour): 2, t0.Add(2 * time.Hour): 1, t0.Add(2*time.Hour + 1): 0} {
		n, err := l.SentSince(ctx, since)
		if err != nil || n != want {
			t.Errorf("since %v: %d (%v), want %d", since, n, err, want)
		}
	}
}

func TestSentSinceOnEmptyLedger(t *testing.T) {
	l, _ := newLedger(t)
	if n, err := l.SentSince(ctx, t0); err != nil || n != 0 {
		t.Fatal(n, err)
	}
}

func TestFailClosedOnBrokenFiles(t *testing.T) {
	for name, content := range map[string]string{
		"garbage":     "{{{",
		"future":      `{"version":2,"entries":{}}`,
		"zero":        `{"entries":{}}`,
		"badstatus":   `{"version":1,"entries":{"k":{"status":"weird"}}}`,
		"emptykey":    `{"version":1,"entries":{"":{"status":"sent"}}}`,
		"trailing":    `{"version":1,"entries":{}} garbage`,
		"empty":       ``,
		"wrongshape":  `[]`,
		"badtime":     `{"version":1,"entries":{"k":{"status":"sent","at":"x"}}}`,
		"nullentries": `{"version":1,"entries":null} x`,
	} {
		l, p := newLedger(t)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := l.Reserve(ctx, "k", "fp", t0); err == nil {
			t.Errorf("%s: Reserve must fail closed", name)
		}
		if err := l.Complete(ctx, "k", "", t0); err == nil {
			t.Errorf("%s: Complete must fail", name)
		}
		if _, err := l.SentSince(ctx, t0); err == nil {
			t.Errorf("%s: SentSince must fail", name)
		}
		b, _ := os.ReadFile(p)
		if string(b) != content {
			t.Errorf("%s: corrupt file must not be overwritten", name)
		}
	}
}

func TestNullEntriesMapIsAccepted(t *testing.T) {
	l, p := newLedger(t)
	_ = os.MkdirAll(filepath.Dir(p), 0o700)
	_ = os.WriteFile(p, []byte(`{"version":1,"entries":null}`), 0o600)
	if _, created, err := l.Reserve(ctx, "k", "fp", t0); err != nil || !created {
		t.Fatal(created, err)
	}
}

func TestUnreadableLedgerFailsClosed(t *testing.T) {
	l, p := newLedger(t)
	_ = os.MkdirAll(filepath.Dir(p), 0o700)
	if err := os.Mkdir(p, 0o700); err != nil { // a directory where the file should be
		t.Fatal(err)
	}
	if _, _, err := l.Reserve(ctx, "k", "fp", t0); err == nil {
		t.Fatal("must fail")
	}
}

func TestUnwritableDirectoryFailsClosed(t *testing.T) {
	dir := t.TempDir()
	blocker := filepath.Join(dir, "file")
	_ = os.WriteFile(blocker, nil, 0o600)
	l, _ := ledger.New(ledger.Config{Path: filepath.Join(blocker, "sub", "ledger.json")})
	if _, _, err := l.Reserve(ctx, "k", "fp", t0); err == nil {
		t.Fatal("must fail")
	}
}

func TestReadOnlyDirectoryFailsClosedOnWrite(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory modes")
	}
	l, p := newLedger(t)
	if _, _, err := l.Reserve(ctx, "k0", "fp", t0); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Dir(p)
	// Keep the lock file creatable but the directory unwritable for the temp file.
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	if _, _, err := l.Reserve(ctx, "k1", "fp", t0); err == nil {
		t.Fatal("write into a read-only directory must fail")
	}
}

func TestNoTempFilesLeftBehind(t *testing.T) {
	l, p := newLedger(t)
	for i := 0; i < 5; i++ {
		_, _, _ = l.Reserve(ctx, string(rune('a'+i)), "fp", t0)
	}
	entries, _ := os.ReadDir(filepath.Dir(p))
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("leftover %s", e.Name())
		}
	}
}

func TestNoBodiesOrExtraFieldsInFile(t *testing.T) {
	l, p := newLedger(t)
	_, _, _ = l.Reserve(ctx, "k", "sha256:abc", t0)
	b, _ := os.ReadFile(p)
	for _, want := range []string{`"version": 1`, `"fingerprint": "sha256:abc"`, `"status": "pending"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("missing %s in %s", want, b)
		}
	}
}

func TestConcurrentReserveHasExactlyOneWinner(t *testing.T) {
	_, p := newLedger(t)
	const n = 16
	var wg sync.WaitGroup
	wins := make(chan bool, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			l, _ := ledger.New(ledger.Config{Path: p}) // separate instance = separate fd, like a separate process
			_, created, err := l.Reserve(ctx, "same", "fp", t0)
			if err != nil {
				t.Error(err)
			}
			wins <- created
		}()
	}
	wg.Wait()
	close(wins)
	got := 0
	for w := range wins {
		if w {
			got++
		}
	}
	if got != 1 {
		t.Fatalf("%d goroutines reserved the same key, want exactly 1", got)
	}
}

func TestConcurrentDistinctKeysAllPersist(t *testing.T) {
	_, p := newLedger(t)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			l, _ := ledger.New(ledger.Config{Path: p})
			k := string(rune('a' + i))
			if _, _, err := l.Reserve(ctx, k, "fp", t0); err != nil {
				t.Error(err)
			}
			if err := l.Complete(ctx, k, "", t0.Add(time.Minute)); err != nil {
				t.Error(err)
			}
		}(i)
	}
	wg.Wait()
	l, _ := ledger.New(ledger.Config{Path: p})
	if n, err := l.SentSince(ctx, t0); err != nil || n != 12 {
		t.Fatalf("lost updates: %d %v", n, err)
	}
}

func TestStateSurvivesNewInstance(t *testing.T) {
	l, p := newLedger(t)
	_, _, _ = l.Reserve(ctx, "k", "fp", t0)
	_ = l.Complete(ctx, "k", "r", t0)
	l2, _ := ledger.New(ledger.Config{Path: p})
	e, created, err := l2.Reserve(ctx, "k", "fp", t0)
	if err != nil || created || e.Status != usecase.LedgerSent {
		t.Fatalf("%+v %v %v", e, created, err)
	}
}

func TestPruneDropsOldSentAndFailedButNeverPending(t *testing.T) {
	p := filepath.Join(t.TempDir(), "l.json")
	l, _ := ledger.New(ledger.Config{Path: p, Retention: 24 * time.Hour})
	for _, k := range []string{"sent", "failed", "pending"} {
		_, _, _ = l.Reserve(ctx, k, "fp", t0)
	}
	_ = l.Complete(ctx, "sent", "", t0)
	_ = l.Fail(ctx, "failed", t0)
	_, _, _ = l.Reserve(ctx, "new", "fp", t0.Add(48*time.Hour)) // triggers prune
	if e, created, _ := l.Reserve(ctx, "pending", "fp", t0.Add(48*time.Hour)); created || e.Status != usecase.LedgerPending {
		t.Fatalf("pending entries must survive pruning: %+v", e)
	}
	if _, created, _ := l.Reserve(ctx, "sent", "fp", t0.Add(48*time.Hour)); !created {
		t.Fatal("old sent entry should have been pruned")
	}
	if n, _ := l.SentSince(ctx, t0); n != 0 {
		t.Fatalf("pruned sent entry still counted: %d", n)
	}
}

func TestContextCancelledAndLockTimeout(t *testing.T) {
	l, p := newLedger(t)
	c, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, err := l.Reserve(c, "k", "fp", t0); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	// Hold the lock from another instance's perspective and time out.
	holder, _ := ledger.New(ledger.Config{Path: p})
	_, _, _ = holder.Reserve(ctx, "seed", "fp", t0)
	lock, err := os.OpenFile(p+".lock", os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = lock.Close() }()
	if err := flockEx(lock); err != nil {
		t.Fatal(err)
	}
	quick, _ := ledger.New(ledger.Config{Path: p, LockTimeout: 30 * time.Millisecond})
	if _, _, err := quick.Reserve(ctx, "k", "fp", t0); err == nil || output.CategoryOf(err) != output.CategoryGeneral {
		t.Fatalf("lock timeout must fail closed: %v", err)
	}
	cc, cancel2 := context.WithTimeout(ctx, 30*time.Millisecond)
	defer cancel2()
	slow, _ := ledger.New(ledger.Config{Path: p})
	if _, _, err := slow.Reserve(cc, "k", "fp", t0); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("ctx must abort lock wait: %v", err)
	}
}
