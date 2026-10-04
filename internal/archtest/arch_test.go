// Package archtest enforces the Clean Architecture dependency rule: imports
// point inward only.
package archtest

import (
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const (
	modPath  = "github.com/stainedhead/outlook-cli"
	corePath = "github.com/stainedhead/agent-cli-core"
)

// layer describes what a package tree may import from this module and from
// agent-cli-core. Standard library imports are always allowed. Third-party
// imports other than core are rejected in inner layers.
type layer struct {
	dir        string   // relative to repo root
	internalOK []string // module-relative prefixes this layer may import
	coreOK     []string // core packages (path under corePath) it may import
	thirdParty bool     // allow other third-party modules
}

var layers = []layer{
	{dir: "internal/domain", internalOK: []string{"internal/domain"}, coreOK: []string{"output"}},
	{dir: "internal/usecase", internalOK: []string{"internal/domain", "internal/usecase"}, coreOK: []string{"output"}},
	{dir: "internal/adapter", internalOK: []string{"internal/domain", "internal/usecase", "internal/adapter"},
		coreOK: []string{"output", "auth", "httpx", "policy", "audit", "selftest", "docgen"}, thirdParty: true},
}

// adapterSiblings: adapters must not import each other (they meet in cmd).
func TestInnerLayersDoNotImportOuterLayers(t *testing.T) {
	root := repoRoot(t)
	for _, l := range layers {
		walkGo(t, filepath.Join(root, l.dir), func(file string, imports []string) {
			for _, imp := range imports {
				if msg := violation(l, file, imp, root); msg != "" {
					t.Errorf("%s imports %s: %s", rel(root, file), imp, msg)
				}
			}
		})
	}
}

func TestAdaptersDoNotImportEachOther(t *testing.T) {
	root := repoRoot(t)
	base := filepath.Join(root, "internal/adapter")
	entries, err := os.ReadDir(base)
	if err != nil {
		return // no adapters yet
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		self := modPath + "/internal/adapter/" + e.Name()
		walkGo(t, filepath.Join(base, e.Name()), func(file string, imports []string) {
			for _, imp := range imports {
				if strings.HasPrefix(imp, modPath+"/internal/adapter/") && !strings.HasPrefix(imp, self) {
					t.Errorf("%s imports sibling adapter %s", rel(root, file), imp)
				}
			}
		})
	}
}

func TestNothingImportsCmd(t *testing.T) {
	root := repoRoot(t)
	walkGo(t, filepath.Join(root, "internal"), func(file string, imports []string) {
		for _, imp := range imports {
			if strings.HasPrefix(imp, modPath+"/cmd") {
				t.Errorf("%s imports %s: internal packages must not import cmd", rel(root, file), imp)
			}
		}
	})
}

// TestNoDaemonModuleImported keeps the daemon client behind core's oktad
// adapter: the only direct import of agent-okta-d is the clienttest fake
// daemon, and only from the composition root's tests.
func TestNoDaemonModuleImported(t *testing.T) {
	root := repoRoot(t)
	const fake = "github.com/stainedhead/agent-okta-d/pkg/client/clienttest"
	walkGo(t, root, func(file string, imports []string) {
		for _, imp := range imports {
			if !strings.Contains(imp, "agent-okta-d") {
				continue
			}
			inCmdTest := strings.HasPrefix(rel(root, file), "cmd/outlook/") && strings.HasSuffix(file, "_test.go")
			if imp == fake && inCmdTest {
				continue
			}
			t.Errorf("%s imports %s: use core's auth/oktad; only cmd/outlook tests may import clienttest", rel(root, file), imp)
		}
	})
}

func TestViolationRules(t *testing.T) {
	l := layers[0]
	cases := []struct {
		imp  string
		want bool
	}{
		{"fmt", false},
		{modPath + "/internal/domain", false},
		{modPath + "/internal/usecase", true},
		{modPath + "/internal/adapter/graph", true},
		{corePath + "/output", false},
		{corePath + "/httpx", true},
		{"gopkg.in/yaml.v3", true},
	}
	for _, c := range cases {
		if got := violation(l, "x.go", c.imp, "/r") != ""; got != c.want {
			t.Errorf("violation(%q) = %v, want %v", c.imp, got, c.want)
		}
	}
}

func violation(l layer, _ string, imp, _ string) string {
	switch {
	case strings.HasPrefix(imp, modPath+"/"):
		r := strings.TrimPrefix(imp, modPath+"/")
		for _, ok := range l.internalOK {
			if r == ok || strings.HasPrefix(r, ok+"/") {
				return ""
			}
		}
		return "outward or sideways dependency"
	case imp == corePath || strings.HasPrefix(imp, corePath+"/"):
		r := strings.TrimPrefix(strings.TrimPrefix(imp, corePath), "/")
		for _, ok := range l.coreOK {
			if r == ok || strings.HasPrefix(r, ok+"/") {
				return ""
			}
		}
		return "core package not allowed in this layer"
	case isStdlib(imp):
		return ""
	case l.thirdParty:
		return ""
	}
	return "third-party package not allowed in this layer"
}

// isStdlib reports whether an import path is in the standard library: its first
// element has no dot.
func isStdlib(imp string) bool {
	first, _, _ := strings.Cut(imp, "/")
	return !strings.Contains(first, ".")
}

func walkGo(t *testing.T, dir string, fn func(file string, imports []string)) {
	t.Helper()
	if _, err := os.Stat(dir); err != nil {
		return
	}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == ".worktrees" || d.Name() == "dist" || d.Name() == "bin") {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(p, ".go") {
			return nil
		}
		f, perr := parser.ParseFile(token.NewFileSet(), p, nil, parser.ImportsOnly)
		if perr != nil {
			return perr
		}
		var imps []string
		for _, s := range f.Imports {
			imps = append(imps, strings.Trim(s.Path.Value, `"`))
		}
		fn(p, imps)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}

func rel(root, p string) string {
	r, err := filepath.Rel(root, p)
	if err != nil {
		return p
	}
	return r
}
