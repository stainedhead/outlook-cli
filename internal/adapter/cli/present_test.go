package cli

import (
	"reflect"
	"testing"

	"github.com/stainedhead/outlook-cli/internal/domain"
	"github.com/stainedhead/outlook-cli/internal/usecase"
)

func TestPresentSendNewFields(t *testing.T) {
	o := presentSend(domain.SendResult{AlreadyDrafted: true, PrefixApplied: true, DraftID: "D1",
		Warnings: []string{"ledger_update_failed", "probe=inconclusive"}})
	if o["already_drafted"] != true || o["prefix_applied"] != true {
		t.Fatalf("%v", o)
	}
	if !reflect.DeepEqual(o["warnings"], []string{"ledger_update_failed", "probe=inconclusive"}) {
		t.Fatalf("warnings = %v", o["warnings"])
	}
	o = presentSend(domain.SendResult{})
	if o["already_drafted"] != false || o["prefix_applied"] != false {
		t.Fatalf("flags must always be present: %v", o)
	}
	if _, ok := o["warnings"]; ok {
		t.Fatalf("empty warnings must be omitted: %v", o)
	}
}

func TestPresentWhoamiPolicyPath(t *testing.T) {
	if o := presentWhoami(usecase.WhoamiResult{PolicyPath: "/etc/agent-cli/outlook.policy.yaml"}); o["policy_path"] != "/etc/agent-cli/outlook.policy.yaml" {
		t.Fatalf("%v", o)
	}
}
