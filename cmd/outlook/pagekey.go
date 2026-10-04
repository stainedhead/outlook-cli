package main

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// Per-install page-token key (FR-R1). The graph adapter signs page tokens with
// an HMAC key so a forged or cross-install token is rejected. The key is 32
// random bytes persisted 0600 next to the ledger/state directory, created on
// first use. The graph config field that consumes it is wired separately:
// graph.Config.PageTokenKey = newPageKeyProvider(pageKeyPath(ledgerPath)).
const (
	pageKeyFileName = "outlook.pagekey"
	pageKeyLen      = 32
)

// pageKeyPath puts the key in the same directory as the ledger.
func pageKeyPath(ledgerPath string) string {
	return filepath.Join(filepath.Dir(ledgerPath), pageKeyFileName)
}

// newPageKeyProvider returns a lazy, process-cached key provider backed by
// path and crypto/rand.
func newPageKeyProvider(path string) func() ([]byte, error) {
	return pageKeyProvider(path, rand.Reader)
}

func pageKeyProvider(path string, entropy io.Reader) func() ([]byte, error) {
	var (
		once sync.Once
		key  []byte
		err  error
	)
	return func() ([]byte, error) {
		once.Do(func() { key, err = loadOrCreateKey(path, entropy) })
		if err != nil {
			return nil, err
		}
		return append([]byte(nil), key...), nil
	}
}

func loadOrCreateKey(path string, entropy io.Reader) ([]byte, error) {
	if k, err := readKey(path); err == nil || !errors.Is(err, os.ErrNotExist) {
		return k, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("page-token key: cannot create directory: %w", err)
	}
	k := make([]byte, pageKeyLen)
	if _, err := io.ReadFull(entropy, k); err != nil {
		return nil, fmt.Errorf("page-token key: no entropy: %w", err)
	}
	// Write a private temp file then link it into place: a concurrent
	// creator that loses the race reads the winner's key, and a reader never
	// sees a partial file.
	tmp, err := os.CreateTemp(filepath.Dir(path), ".pagekey-*")
	if err != nil {
		return nil, fmt.Errorf("page-token key: cannot create file: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.Write(k); err != nil {
		_ = tmp.Close()
		return nil, fmt.Errorf("page-token key: cannot write file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return nil, fmt.Errorf("page-token key: cannot write file: %w", err)
	}
	if err := os.Link(tmp.Name(), path); err != nil && !errors.Is(err, os.ErrExist) {
		return nil, fmt.Errorf("page-token key: cannot install file: %w", err)
	}
	return readKey(path)
}

// readKey reads and validates the key file: regular, 0600-or-stricter, exact
// length, not all zero.
func readKey(path string) ([]byte, error) {
	fi, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() || fi.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("page-token key %s must be a regular file with mode 0600", path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("page-token key: %w", err)
	}
	if len(b) != pageKeyLen || allZero(b) {
		return nil, fmt.Errorf("page-token key %s is malformed; delete it to regenerate (outstanding page tokens become invalid)", path)
	}
	return b, nil
}

func allZero(b []byte) bool {
	for _, c := range b {
		if c != 0 {
			return false
		}
	}
	return true
}
