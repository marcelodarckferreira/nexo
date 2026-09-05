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
	agentAIP := tailscaleIP(t, agentAContainer)

	out, err := tailscaleSSH(t, supportPCContainer, agentAIP, "echo remote-ok")
	if err != nil {
		t.Fatalf("tailscale ssh support-pc -> agent-a failed: %v\noutput: %s", err, out)
	}

	if !strings.Contains(out, "remote-ok") {
		t.Fatalf("expected output to contain %q, got %q", "remote-ok", out)
	}
}

func TestSSHDeniedForUnauthorizedSource(t *testing.T) {
	agentBIP := tailscaleIP(t, agentBContainer)

	// Controle positivo: confirma que a sondagem (tailscale nc) funciona a partir de uma origem
	// autorizada (support-pc -> agent-b) ANTES de confiar no resultado negativo abaixo. Sem isso,
	// um container agent-a quebrado/parado/renomeado faria o teste "passar" mesmo sem provar nada
	// sobre a política de ACL.
	ctxOK, cancelOK := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancelOK()
	okCmd := exec.CommandContext(ctxOK, "docker", "exec", supportPCContainer, "tailscale", "nc", agentBIP, "22")
	if out, err := okCmd.CombinedOutput(); err != nil {
		t.Fatalf("controle positivo falhou: esperava que support-pc -> agent-b:22 tivesse sucesso, obteve erro: %v\nsaída: %s", err, out)
	}

	// Caso negativo: a mesma sondagem a partir de uma origem não autorizada deve ser negada.
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, "docker", "exec", agentAContainer, "tailscale", "nc", agentBIP, "22")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("expected agent-a -> agent-b:22 to be denied by ACL, but connection succeeded: %s", out)
	}
}
