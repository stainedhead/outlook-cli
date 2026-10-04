//go:build outlookdev

package main

import (
	"os"
	"testing"
)

func TestFRR2DevBuildOverrideNeedsExplicitValue(t *testing.T) {
	t.Setenv(envPolicyInsecure, "")
	if len(devPolicyOpts(os.Getenv)) != 0 {
		t.Fatal("override must be off by default")
	}
	t.Setenv(envPolicyInsecure, "1")
	if len(devPolicyOpts(os.Getenv)) != 1 {
		t.Fatal("explicit opt-in expected")
	}
}
