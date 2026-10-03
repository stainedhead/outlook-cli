package ledger_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/outlook-cli/internal/adapter/ledger"
)

func TestQuarantineSaveWritesExclusive0600(t *testing.T) {
	root := t.TempDir()
	q := ledger.Quarantine{Root: root}
	path, n, err := q.Save(ctx, filepath.Join(root, "sub"), "report.pdf", strings.NewReader("hello"), 100)
	if err != nil || n != 5 {
		t.Fatal(n, err)
	}
	if path != mustEval(t, filepath.Join(root, "sub", "report.pdf")) {
		t.Fatalf("path %s", path)
	}
	st, _ := os.Stat(path)
	if st.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", st.Mode())
	}
	if b, _ := os.ReadFile(path); string(b) != "hello" {
		t.Fatal(string(b))
	}
	if _, _, err := q.Save(ctx, filepath.Join(root, "sub"), "report.pdf", strings.NewReader("x"), 100); output.CategoryOf(err) != output.CategoryConflict {
		t.Fatalf("existing file must not be overwritten: %v", err)
	}
	if b, _ := os.ReadFile(path); string(b) != "hello" {
		t.Fatal("existing file was modified")
	}
}

func mustEval(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestQuarantineSizeCapRemovesPartial(t *testing.T) {
	root := t.TempDir()
	q := ledger.Quarantine{Root: root}
	_, _, err := q.Save(ctx, root, "big.bin", strings.NewReader(strings.Repeat("a", 11)), 10)
	if output.CategoryOf(err) != output.CategoryPolicyDenied {
		t.Fatal(err)
	}
	if es, _ := os.ReadDir(root); len(es) != 0 {
		t.Fatalf("partial file left: %v", es)
	}
	// Exactly at the cap is fine.
	if _, n, err := q.Save(ctx, root, "ok.bin", strings.NewReader(strings.Repeat("a", 10)), 10); err != nil || n != 10 {
		t.Fatal(n, err)
	}
}

func TestQuarantineRefusesOutsideRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	q := ledger.Quarantine{Root: root}
	link := filepath.Join(root, "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	for name, dir := range map[string]string{
		"dotdot":  filepath.Join(root, "..", filepath.Base(outside)),
		"abs":     outside,
		"symlink": link,
		"sibling": root + "-evil",
	} {
		_, _, err := q.Save(ctx, dir, "a.txt", strings.NewReader("x"), 10)
		if output.CategoryOf(err) != output.CategoryPolicyDenied {
			t.Errorf("%s: %v", name, err)
		}
	}
	if es, _ := os.ReadDir(outside); len(es) != 0 {
		t.Fatalf("something was written outside the root: %v", es)
	}
	_ = os.RemoveAll(root + "-evil")
}

func TestQuarantineDoesNotFollowSymlinkedFile(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "victim")
	_ = os.WriteFile(target, []byte("orig"), 0o600)
	if err := os.Symlink(target, filepath.Join(root, "evil.txt")); err != nil {
		t.Fatal(err)
	}
	q := ledger.Quarantine{Root: root}
	if _, _, err := q.Save(ctx, root, "evil.txt", strings.NewReader("pwn"), 10); err == nil {
		t.Fatal("must refuse")
	}
	if b, _ := os.ReadFile(target); string(b) != "orig" {
		t.Fatal("symlink target was written")
	}
}

func TestQuarantineSanitizesNames(t *testing.T) {
	root := t.TempDir()
	q := ledger.Quarantine{Root: root}
	path, _, err := q.Save(ctx, root, "../../etc/pass\x00wd", strings.NewReader("x"), 10)
	if err != nil || filepath.Dir(path) != mustEval(t, root) || filepath.Base(path) != "passwd" {
		t.Fatal(path, err)
	}
}

func TestSanitizeName(t *testing.T) {
	long := "a" + strings.Repeat("€", 100)
	cases := map[string]string{
		"a.txt":               "a.txt",
		"../x":                "x",
		`..\..\win\x.exe`:     "x.exe",
		".hidden":             "hidden",
		"...":                 "attachment",
		"":                    "attachment",
		"  ":                  "attachment",
		"a\nb\tc":             "abc",
		"we:ird*na<me>?.pdf":  "we_ird_na_me__.pdf",
		"dir/":                "attachment",
		"zero\u200bwidth.txt": "zerowidth.txt",
		"rtl\u202egpj.exe":    "rtlgpj.exe",
		"has space.txt":       "has space.txt",
		"unicodé.txt":         "unicodé.txt",
	}
	for in, want := range cases {
		if got := ledger.SanitizeName(in); got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
	got := ledger.SanitizeName(long)
	if len(got) > 120 || len(got) == 0 || strings.ContainsRune(got, '�') {
		t.Errorf("long name: %d bytes", len(got))
	}
}

func TestQuarantineWithoutRootResolvesOutDirOnly(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "q")
	if _, _, err := (ledger.Quarantine{}).Save(ctx, dir, "a", strings.NewReader("x"), 5); err != nil {
		t.Fatal(err)
	}
	if st, _ := os.Stat(dir); st.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode %v", st.Mode())
	}
}

func TestQuarantineInputValidation(t *testing.T) {
	q := ledger.Quarantine{}
	d := t.TempDir()
	if _, _, err := q.Save(ctx, d, "a", strings.NewReader("x"), 0); output.CategoryOf(err) != output.CategoryValidation {
		t.Fatal(err)
	}
	if _, _, err := q.Save(ctx, "", "a", strings.NewReader("x"), 5); output.CategoryOf(err) != output.CategoryValidation {
		t.Fatal(err)
	}
	c, cancel := context.WithCancel(ctx)
	cancel()
	if _, _, err := q.Save(c, d, "a", strings.NewReader("x"), 5); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

type failReader struct{}

func (failReader) Read([]byte) (int, error) { return 0, errors.New("boom") }

func TestQuarantineReadErrorRemovesPartial(t *testing.T) {
	d := t.TempDir()
	r := io.MultiReader(strings.NewReader("abc"), failReader{})
	if _, _, err := (ledger.Quarantine{}).Save(ctx, d, "a", r, 50); err == nil {
		t.Fatal("expected error")
	}
	if es, _ := os.ReadDir(d); len(es) != 0 {
		t.Fatalf("partial file left: %v", es)
	}
}

func TestQuarantineCancelledMidCopy(t *testing.T) {
	d := t.TempDir()
	c, cancel := context.WithCancel(ctx)
	r := &cancelAfter{cancel: cancel}
	if _, _, err := (ledger.Quarantine{}).Save(c, d, "a", r, 1<<20); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if es, _ := os.ReadDir(d); len(es) != 0 {
		t.Fatalf("partial file left: %v", es)
	}
}

type cancelAfter struct {
	n      int
	cancel context.CancelFunc
}

func (c *cancelAfter) Read(p []byte) (int, error) {
	c.n++
	if c.n == 2 {
		c.cancel()
	}
	copy(p, "xxxx")
	return 4, nil
}

func TestQuarantineUncreatableDirectory(t *testing.T) {
	f := filepath.Join(t.TempDir(), "file")
	_ = os.WriteFile(f, nil, 0o600)
	if _, _, err := (ledger.Quarantine{}).Save(ctx, filepath.Join(f, "sub"), "a", strings.NewReader("x"), 5); err == nil {
		t.Fatal("expected error")
	}
	if _, _, err := (ledger.Quarantine{Root: filepath.Join(f, "root")}).Save(ctx, t.TempDir(), "a", strings.NewReader("x"), 5); err == nil {
		t.Fatal("expected error for bad root")
	}
}
