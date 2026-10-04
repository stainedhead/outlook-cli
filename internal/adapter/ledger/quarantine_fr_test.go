package ledger_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stainedhead/agent-cli-core/output"
	"github.com/stainedhead/outlook-cli/internal/adapter/ledger"
)

func TestFRR12SymlinkEscapeCreatesNothingOutsideRoot(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	q, err := ledger.NewQuarantine(root)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = q.Save(ctx, filepath.Join(root, "link", "a", "b"), "x.txt", strings.NewReader("x"), 10)
	if output.CategoryOf(err) != output.CategoryPolicyDenied {
		t.Fatalf("%v", err)
	}
	if es, _ := os.ReadDir(outside); len(es) != 0 {
		t.Fatalf("directories created outside root: %v", es)
	}
}

func TestFRR12DotDotEscapeCreatesNothing(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	_ = os.Mkdir(root, 0o700)
	q, _ := ledger.NewQuarantine(root)
	_, _, err := q.Save(ctx, filepath.Join(root, "..", "evil", "z"), "x", strings.NewReader("x"), 10)
	if output.CategoryOf(err) != output.CategoryPolicyDenied {
		t.Fatalf("%v", err)
	}
	if _, err := os.Stat(filepath.Join(base, "evil")); err == nil {
		t.Fatal("created outside root")
	}
}

func TestFRR12EmptyRootRejected(t *testing.T) {
	if _, err := ledger.NewQuarantine(""); output.CategoryOf(err) != output.CategoryValidation {
		t.Fatalf("%v", err)
	}
	// The zero value fails closed too.
	if _, _, err := (ledger.Quarantine{}).Save(ctx, t.TempDir(), "a", strings.NewReader("x"), 5); output.CategoryOf(err) != output.CategoryValidation {
		t.Fatalf("%v", err)
	}
}

func TestFRR12NameCollisionGetsUniqueSuffix(t *testing.T) {
	root := t.TempDir()
	q, _ := ledger.NewQuarantine(root)
	var paths []string
	for _, body := range []string{"one", "two", "three"} {
		p, _, err := q.Save(ctx, root, "report.pdf", strings.NewReader(body), 100)
		if err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}
	seen := map[string]bool{}
	for i, p := range paths {
		if seen[p] {
			t.Fatalf("duplicate path %s", p)
		}
		seen[p] = true
		b, _ := os.ReadFile(p)
		if string(b) != []string{"one", "two", "three"}[i] {
			t.Fatalf("%s holds %q", p, b)
		}
		if !strings.HasSuffix(p, ".pdf") {
			t.Fatalf("extension lost: %s", p)
		}
	}
	if filepath.Base(paths[0]) != "report.pdf" {
		t.Fatal(paths[0])
	}
}

func TestFRR12RootCreatedWhenMissing(t *testing.T) {
	root := filepath.Join(t.TempDir(), "a", "quarantine")
	q, _ := ledger.NewQuarantine(root)
	if _, _, err := q.Save(ctx, filepath.Join(root, "sub"), "f", strings.NewReader("x"), 5); err != nil {
		t.Fatal(err)
	}
}
