//go:build integration

package integration

import (
	"context"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func tailscaleSSH(t *testing.T, sourceContainer, targetIP, remoteCommand string) (string, error) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	target := "root@" + targetIP
	cmd := exec.CommandContext(ctx, "docker", "exec", sourceContainer, "tailscale", "ssh", target, "--", remoteCommand)

	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestSSHOverOverlay(t *testing.T) {
	agentAIP := tailscaleIP(t, "remoto-dev-agent-a")

	out, err := tailscaleSSH(t, "remoto-dev-support-pc", agentAIP, "echo remote-ok")
	if err != nil {
		t.Fatalf("tailscale ssh support-pc -> agent-a failed: %v\noutput: %s", err, out)
	}

	if !strings.Contains(out, "remote-ok") {
		t.Fatalf("expected output to contain %q, got %q", "remote-ok", out)
	}
}

func TestSSHDeniedForUnauthorizedSource(t *testing.T) {
	agentBIP := tailscaleIP(t, "remoto-dev-agent-b")

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", "exec", "remoto-dev-agent-a", "tailscale", "nc", agentBIP, "22")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected agent-a -> agent-b:22 to be denied by ACL, but connection succeeded: %s", out)
	}
}
