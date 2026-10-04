package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestFRR1KeyProviderCreatesPrivateKeyOnce(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state", pageKeyFileName)
	k1, err := newPageKeyProvider(p)()
	if err != nil || len(k1) != pageKeyLen {
		t.Fatalf("%v len=%d", err, len(k1))
	}
	fi, err := os.Stat(p)
	if err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("%v %v", err, fi)
	}
	if d, _ := os.Stat(filepath.Dir(p)); d.Mode().Perm() != 0o700 {
		t.Errorf("dir mode %v", d.Mode().Perm())
	}
	// A later process (new provider) gets the same key; a different install
	// gets a different one.
	k2, err := newPageKeyProvider(p)()
	if err != nil || !bytes.Equal(k1, k2) {
		t.Fatalf("key not stable: %v", err)
	}
	k3, _ := newPageKeyProvider(filepath.Join(t.TempDir(), pageKeyFileName))()
	if bytes.Equal(k1, k3) {
		t.Fatal("keys must be per install")
	}
}

func TestFRR1KeyProviderCachesWithinProcess(t *testing.T) {
	p := filepath.Join(t.TempDir(), pageKeyFileName)
	f := newPageKeyProvider(p)
	k1, _ := f()
	_ = os.Remove(p)
	k2, err := f()
	if err != nil || !bytes.Equal(k1, k2) {
		t.Fatalf("%v", err)
	}
}

func TestFRR1KeyProviderConcurrentFirstUse(t *testing.T) {
	p := filepath.Join(t.TempDir(), pageKeyFileName)
	keys := make([][]byte, 8)
	var wg sync.WaitGroup
	for i := range keys {
		wg.Add(1)
		go func() {
			defer wg.Done()
			keys[i], _ = newPageKeyProvider(p)()
		}()
	}
	wg.Wait()
	for _, k := range keys {
		if len(k) != pageKeyLen || !bytes.Equal(k, keys[0]) {
			t.Fatalf("racing creators disagree: %x vs %x", k, keys[0])
		}
	}
}

func TestFRR1KeyProviderRefusesBadFiles(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]func(p string){
		"short": func(p string) { _ = os.WriteFile(p, []byte("short"), 0o600) },
		"open-mode": func(p string) {
			_ = os.WriteFile(p, bytes.Repeat([]byte{1}, pageKeyLen), 0o644)
			_ = os.Chmod(p, 0o644)
		},
		"directory": func(p string) { _ = os.Mkdir(p, 0o700) },
		"all-zero":  func(p string) { _ = os.WriteFile(p, make([]byte, pageKeyLen), 0o600) },
	}
	for name, setup := range cases {
		p := filepath.Join(dir, name)
		setup(p)
		if _, err := newPageKeyProvider(p)(); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestFRR1KeyProviderUnwritableDir(t *testing.T) {
	f := filepath.Join(t.TempDir(), "file")
	_ = os.WriteFile(f, nil, 0o600)
	if _, err := newPageKeyProvider(filepath.Join(f, "sub", pageKeyFileName))(); err == nil {
		t.Fatal("expected error")
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("no entropy") }

func TestFRR1KeyProviderEntropyFailure(t *testing.T) {
	_, err := pageKeyProvider(filepath.Join(t.TempDir(), pageKeyFileName), errReader{})()
	if err == nil || !strings.Contains(err.Error(), "page-token key") {
		t.Fatalf("%v", err)
	}
}
