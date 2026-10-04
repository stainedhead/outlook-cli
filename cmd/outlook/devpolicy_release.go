//go:build !outlookdev

package main

import "github.com/stainedhead/outlook-cli/internal/adapter/policyfile"

// devBuild is false in release builds: the policy ownership check cannot be
// relaxed by any environment variable (FR-R2).
const devBuild = false

// devPolicyOpts returns no options in a release build.
func devPolicyOpts(func(string) string) []policyfile.Option { return nil }
