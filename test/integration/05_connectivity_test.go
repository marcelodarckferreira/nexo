//go:build integration

package integration

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

func tailscalePing(container, targetIP string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", "exec", container, "tailscale", "ping", "-c", "1", targetIP)
	return cmd.Run()
}

func TestOverlayConnectivityAndIsolation(t *testing.T) {
	agentAIP := tailscaleIP(t, "remoto-dev-agent-a")
	agentBIP := tailscaleIP(t, "remoto-dev-agent-b")

	if err := tailscalePing("remoto-dev-support-pc", agentAIP); err != nil {
		t.Errorf("expected support-pc to reach agent-a, got error: %v", err)
	}

	if err := tailscalePing("remoto-dev-support-pc", agentBIP); err != nil {
		t.Errorf("expected support-pc to reach agent-b, got error: %v", err)
	}

	if err := tailscalePing("remoto-dev-agent-a", agentBIP); err == nil {
		t.Errorf("expected agent-a to be DENIED reaching agent-b by default-deny ACL, but ping succeeded")
	}
}
