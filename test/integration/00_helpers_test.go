//go:build integration

package integration

import (
	"fmt"
	"net/http"
	"os/exec"
	"testing"
	"time"
)

const (
	composeFile          = "../../docker-compose.dev.yml"
	headscaleContainer   = "remoto-dev-headscale"
	headscaleBaseURL     = "http://127.0.0.1:8080"
)

func runCmd(t *testing.T, name string, args ...string) string {
	t.Helper()
	return runCmdEnv(t, nil, name, args...)
}

func runCmdEnv(t *testing.T, extraEnv []string, name string, args ...string) string {
	t.Helper()

	cmd := exec.Command(name, args...)
	if len(extraEnv) > 0 {
		cmd.Env = append(cmd.Environ(), extraEnv...)
	}

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("command %q %v failed: %v\noutput:\n%s", name, args, err, out)
	}

	return string(out)
}

func dockerComposeUp(t *testing.T, services ...string) {
	t.Helper()

	args := append([]string{"compose", "-f", composeFile, "up", "-d"}, services...)
	runCmd(t, "docker", args...)
}

func headscaleExec(t *testing.T, args ...string) string {
	t.Helper()

	dockerArgs := append([]string{"exec", headscaleContainer, "headscale"}, args...)
	return runCmd(t, "docker", dockerArgs...)
}

func waitForHTTPOK(t *testing.T, url string, timeout time.Duration) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}

	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := client.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return
			}
			lastErr = fmt.Errorf("status %d", resp.StatusCode)
		} else {
			lastErr = err
		}
		time.Sleep(500 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for %s to become healthy: %v", url, lastErr)
}
