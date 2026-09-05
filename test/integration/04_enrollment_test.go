//go:build integration

package integration

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

type preAuthKeyResponse struct {
	Key     string   `json:"key"`
	ACLTags []string `json:"acl_tags"`
}

func createPreAuthKey(t *testing.T, tags ...string) string {
	t.Helper()

	args := []string{"preauthkeys", "create", "--user", "1", "--reusable=false", "--expiration", "1h", "-o", "json"}
	if len(tags) > 0 {
		args = append(args, "--tags", strings.Join(tags, ","))
	}

	out := headscaleExec(t, args...)

	var resp preAuthKeyResponse
	if err := json.Unmarshal([]byte(out), &resp); err != nil {
		t.Fatalf("decoding preauthkey response %q: %v", out, err)
	}

	if resp.Key == "" {
		t.Fatalf("expected non-empty preauthkey, got %q", out)
	}

	return resp.Key
}

func tailscaleIP(t *testing.T, container string) string {
	t.Helper()

	out := runCmd(t, "docker", "exec", container, "tailscale", "ip", "-4")
	return strings.TrimSpace(out)
}

func waitForNodeCount(t *testing.T, want int, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		out := headscaleExec(t, "nodes", "list", "-o", "json")

		var nodes []map[string]any
		if err := json.Unmarshal([]byte(out), &nodes); err == nil && len(nodes) >= want {
			return
		}

		time.Sleep(1 * time.Second)
	}

	t.Fatalf("timed out waiting for %d registered nodes", want)
}

func TestThreeDevicesEnrolled(t *testing.T) {
	supportKey := createPreAuthKey(t)
	agentAKey := createPreAuthKey(t, "tag:remoto-agent")
	agentBKey := createPreAuthKey(t, "tag:remoto-agent")

	dockerComposeUp(t, "headscale")

	runCmdEnv(t, []string{"SUPPORT_PC_AUTHKEY=" + supportKey}, "docker", "compose", "-f", composeFile, "up", "-d", "support-pc")
	runCmdEnv(t, []string{"AGENT_A_AUTHKEY=" + agentAKey}, "docker", "compose", "-f", composeFile, "up", "-d", "agent-a")
	runCmdEnv(t, []string{"AGENT_B_AUTHKEY=" + agentBKey}, "docker", "compose", "-f", composeFile, "up", "-d", "agent-b")

	waitForNodeCount(t, 3, 30*time.Second)

	supportIP := tailscaleIP(t, "remoto-dev-support-pc")
	agentAIP := tailscaleIP(t, "remoto-dev-agent-a")
	agentBIP := tailscaleIP(t, "remoto-dev-agent-b")

	for name, ip := range map[string]string{"support-pc": supportIP, "agent-a": agentAIP, "agent-b": agentBIP} {
		if ip == "" {
			t.Fatalf("expected %s to have an overlay IP, got empty string", name)
		}
	}
}
