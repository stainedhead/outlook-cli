//go:build outlookdev

package main

import "github.com/stainedhead/outlook-cli/internal/adapter/policyfile"

// devBuild is true only when built with -tags outlookdev (never released).
const devBuild = true

// envPolicyInsecure, in an outlookdev build only, skips the policy ownership
// check so a developer can use a policy in their own home directory.
const envPolicyInsecure = "OUTLOOK_POLICY_INSECURE"

func devPolicyOpts(getenv func(string) string) []policyfile.Option {
	if getenv(envPolicyInsecure) == "1" {
		return []policyfile.Option{policyfile.AllowUntrusted()}
	}
	return nil
}
