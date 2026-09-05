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

// waitForPeerReachable retries tailscalePing until it succeeds or the timeout elapses.
// Used only for POSITIVE assertions: right after enrollment, netmap propagation and the
// WireGuard handshake may not have finished yet, so a single 8s attempt can fail spuriously
// on a freshly (re)created container. Negative assertions must NOT use this helper — a single
// timeout is already the correct (and faster) way to observe an ACL denial.
func waitForPeerReachable(t *testing.T, sourceContainer, targetIP string, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		if err := tailscalePing(sourceContainer, targetIP); err == nil {
			return
		} else {
			lastErr = err
		}
		time.Sleep(1 * time.Second)
	}

	t.Fatalf("timed out waiting for %s to reach %s: %v", sourceContainer, targetIP, lastErr)
}

func TestOverlayConnectivityAndIsolation(t *testing.T) {
	agentAIP := tailscaleIP(t, agentAContainer)
	agentBIP := tailscaleIP(t, agentBContainer)

	waitForPeerReachable(t, supportPCContainer, agentAIP, 20*time.Second)
	waitForPeerReachable(t, supportPCContainer, agentBIP, 20*time.Second)

	if err := tailscalePing(agentAContainer, agentBIP); err == nil {
		t.Errorf("expected agent-a to be DENIED reaching agent-b by default-deny ACL, but ping succeeded")
	}
}
