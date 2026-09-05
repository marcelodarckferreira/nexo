//go:build integration

package integration

import (
	"strings"
	"testing"
)

func TestACLPolicyIsValid(t *testing.T) {
	dockerComposeUp(t, "headscale")

	out := headscaleExec(t, "policy", "check", "--file", "/etc/headscale/acl-policy.hujson")

	if !strings.Contains(out, "Policy is valid") {
		t.Fatalf("expected policy to be valid, got: %s", out)
	}
}
