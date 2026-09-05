# Fase 1 — Overlay Headscale (Planes B+C+D) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ter uma overlay privada L3 funcional (Headscale + clientes Tailscale) rodando localmente
via Docker, com um usuário administrador, uma política de ACL default-deny, três dispositivos de
teste enrolados (um "Support PC" e dois "Remote Agent" tagueados), conectividade L3 comprovada
entre Support PC e os agentes, isolamento lateral comprovado entre agentes, e SSH funcional sobre
a overlay (Plane D) — tudo verificável por testes de integração automatizados.

**Architecture:** Headscale (BSD-3-Clause) roda como coordenador da overlay WireGuard, com SQLite
como armazenamento interno (não Postgres — ver nota na spec v1.2) e uma política de ACL em HuJSON
carregada de arquivo. Os "dispositivos" desta fase são containers Docker rodando o cliente oficial
Tailscale, unidos à tailnet via preauthkeys gerados pela CLI do Headscale. Toda a infraestrutura
desta fase é local/dev (Docker Compose); o provisionamento do Headscale real de produção
(`remoto.darckware.net`) é escrito nesta fase mas não executado (Tarefa 7).

**Tech Stack:** Go 1.22 (testes de integração via `os/exec`), Docker + Docker Compose v2,
Headscale `0.29.3` (imagem oficial `headscale/headscale:0.29.3`), cliente Tailscale `v1.102.3`
(imagem oficial `tailscale/tailscale:v1.102.3`), Alpine (base da imagem do cliente, usada para
adicionar `openssh-client` na role "Support PC").

**Spec:** `docs/superpowers/specs/2026-09-04-remoto-design.md` (v1.2)

## Global Constraints

- `NO_DIRECT_AGENT_INBOUND_DEPENDENCY = true` — nenhum dispositivo abre porta inbound; toda adesão
  à tailnet é iniciada pelo cliente.
- `OUTBOUND_INITIATED_CONTROL_CHANNEL = true`.
- `ZERO_TRUST_IDENTITY = true`, `DEVICE_SPECIFIC_IDENTITY = true` — cada dispositivo tem sua
  própria chave/identidade; nenhuma chave é compartilhada entre containers.
- `PRIVATE_OVERLAY_NETWORK = true`, `SSH_OVER_PRIVATE_NETWORK = true` — SSH usa exclusivamente o IP
  da overlay (100.64.0.0/10), nunca a rede Docker/host diretamente.
- `FAIL_CLOSED = true` — a política de ACL é default-deny: qualquer par (origem, destino) não
  listado explicitamente em `grants`/`ssh` é negado. Isso é verificado por teste (Tarefa 5).
- Versões fixadas nesta fase (não usar `latest`): `headscale/headscale:0.29.3`,
  `tailscale/tailscale:v1.102.3`.
- O banco do Headscale é SQLite (correção da spec v1.2); PostgreSQL é do Management Server, que não
  existe ainda nesta fase.
- Todo o design deste plano foi validado manualmente contra os binários reais (Headscale e
  Tailscale) antes de ser escrito — os comandos, formatos de config e saídas JSON abaixo não são
  hipotéticos.

---

### Task 1: Esqueleto do projeto e stack de desenvolvimento do Headscale

**Files:**
- Create: `go.mod`
- Create: `docker-compose.dev.yml`
- Create: `infra/headscale/config.dev.yaml`
- Create: `infra/headscale/acl-policy.hujson`
- Test: `test/integration/00_helpers_test.go`
- Test: `test/integration/01_stack_test.go`

**Interfaces:**
- Produces: `dockerComposeUp(t *testing.T, services ...string)`, `runCmd(t *testing.T, name string, args ...string) string`, `runCmdEnv(t *testing.T, extraEnv []string, name string, args ...string) string`, `headscaleExec(t *testing.T, args ...string) string`, `waitForHTTPOK(t *testing.T, url string, timeout time.Duration)` — usadas por todas as tarefas seguintes.
- Consumes: nada (primeira tarefa).

- [ ] **Step 1: Escrever o teste que falha**

Criar `test/integration/00_helpers_test.go`:

```go
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
```

Criar `test/integration/01_stack_test.go`:

```go
//go:build integration

package integration

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

type healthResponse struct {
	Status string `json:"status"`
}

func TestHeadscaleStackHealthy(t *testing.T) {
	dockerComposeUp(t, "headscale")

	waitForHTTPOK(t, headscaleBaseURL+"/health", 30*time.Second)

	resp, err := http.Get(headscaleBaseURL + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	defer resp.Body.Close()

	var hr healthResponse
	if err := json.NewDecoder(resp.Body).Decode(&hr); err != nil {
		t.Fatalf("decoding health response: %v", err)
	}

	if hr.Status != "pass" {
		t.Fatalf("expected health status %q, got %q", "pass", hr.Status)
	}
}
```

- [ ] **Step 2: Rodar o teste e confirmar que falha**

Run: `cd /root/project/remoto && go test ./test/integration/... -tags=integration -run TestHeadscaleStackHealthy -v`
Expected: FAIL — `docker compose -f ../../docker-compose.dev.yml up -d headscale` falha porque o
arquivo `docker-compose.dev.yml` ainda não existe ("no such file or directory" ou erro equivalente
do Docker Compose).

- [ ] **Step 3: Escrever a implementação mínima**

Criar `go.mod`:

```
module github.com/darckware/remoto

go 1.22
```

Criar `infra/headscale/config.dev.yaml` (validado manualmente contra o binário real do Headscale
`0.29.3` — cada campo abaixo é necessário; a ausência de `dns.override_local_dns: false` faz o
servidor recusar subir exigindo `dns.nameservers.global`, e a ausência do bloco `derp.urls` faz o
servidor falhar com "initial DERPMap is empty"):

```yaml
server_url: http://headscale:8080
listen_addr: 0.0.0.0:8080
metrics_listen_addr: 127.0.0.1:9090
grpc_listen_addr: 127.0.0.1:50443
grpc_allow_insecure: false

noise:
  private_key_path: /var/lib/headscale/noise_private.key

prefixes:
  v4: 100.64.0.0/10
  v6: fd7a:115c:a1e0::/48

database:
  type: sqlite
  sqlite:
    path: /var/lib/headscale/db.sqlite
    write_ahead_log: true

policy:
  mode: file
  path: /etc/headscale/acl-policy.hujson

dns:
  magic_dns: false
  base_domain: remoto-dev.internal
  override_local_dns: false

derp:
  server:
    enabled: false
  urls:
    - https://controlplane.tailscale.com/derpmap/default
  paths: []
  auto_update_enabled: true
  update_frequency: 24h

log:
  level: info
```

Criar `infra/headscale/acl-policy.hujson` — política **intencionalmente permissiva** ("allow all"
é o comportamento documentado do Headscale para uma política vazia `{}`). A Tarefa 3 substitui este
arquivo pela política default-deny real:

```jsonc
// Fase 1 — Tarefa 1: política temporária "allow all" (comportamento padrão do Headscale
// para uma política vazia). A Tarefa 3 substitui este arquivo por uma política default-deny
// com grants explícitos.
{}
```

Criar `docker-compose.dev.yml`:

```yaml
name: remoto-dev

services:
  headscale:
    image: headscale/headscale:0.29.3
    container_name: remoto-dev-headscale
    command: serve
    ports:
      - "127.0.0.1:8080:8080"
    volumes:
      - ./infra/headscale/config.dev.yaml:/etc/headscale/config.yaml:ro
      - ./infra/headscale/acl-policy.hujson:/etc/headscale/acl-policy.hujson:ro
      - remoto_dev_headscale_data:/var/lib/headscale

volumes:
  remoto_dev_headscale_data:
```

- [ ] **Step 4: Rodar o teste e confirmar que passa**

Run: `go test ./test/integration/... -tags=integration -run TestHeadscaleStackHealthy -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add go.mod docker-compose.dev.yml infra/headscale/config.dev.yaml \
        infra/headscale/acl-policy.hujson test/integration/00_helpers_test.go \
        test/integration/01_stack_test.go
git commit -m "feat(fase1): stack de dev do Headscale sobe e responde /health"
```

---

### Task 2: Usuário administrador

**Files:**
- Test: `test/integration/02_admin_user_test.go`

**Interfaces:**
- Consumes: `dockerComposeUp`, `headscaleExec` (Task 1).
- Produces: usuário Headscale `marcelo` com `id` numérico (usado como referência `marcelo@` na
  política da Tarefa 3, e como `--user <id>` na geração de preauthkeys da Tarefa 4).

- [ ] **Step 1: Escrever o teste que falha**

Criar `test/integration/02_admin_user_test.go`:

```go
//go:build integration

package integration

import (
	"encoding/json"
	"testing"
)

type headscaleUser struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

func TestAdminUserExists(t *testing.T) {
	dockerComposeUp(t, "headscale")

	out := headscaleExec(t, "users", "list", "-o", "json")

	var users []headscaleUser
	if err := json.Unmarshal([]byte(out), &users); err != nil {
		t.Fatalf("decoding users list %q: %v", out, err)
	}

	for _, u := range users {
		if u.Name == "marcelo" {
			return
		}
	}

	t.Fatalf("expected user %q to exist, got %v", "marcelo", users)
}
```

- [ ] **Step 2: Rodar o teste e confirmar que falha**

Run: `go test ./test/integration/... -tags=integration -run TestAdminUserExists -v`
Expected: FAIL — a lista de usuários está vazia (nenhum usuário `marcelo` foi criado ainda).

- [ ] **Step 3: Criar o usuário**

Run: `docker exec remoto-dev-headscale headscale users create marcelo -o json`
Expected output (validado manualmente):

```json
{
	"id": 1,
	"name": "marcelo",
	"created_at": { "seconds": 0, "nanos": 0 }
}
```

Este passo é uma ação operacional (não um arquivo de código) — deve ser executado uma vez contra o
ambiente de dev. Se o teste rodar em uma máquina limpa (stack recriada do zero), execute este
comando antes de rodar os testes; ele é idempotente na prática (rodar de novo com o mesmo nome
falha com "user already exists", o que é aceitável — não precisa de tratamento especial aqui).

- [ ] **Step 4: Rodar o teste e confirmar que passa**

Run: `go test ./test/integration/... -tags=integration -run TestAdminUserExists -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add test/integration/02_admin_user_test.go
git commit -m "test(fase1): verifica existência do usuário administrador marcelo"
```

---

### Task 3: Política de ACL default-deny (grants + ssh)

**Files:**
- Modify: `infra/headscale/acl-policy.hujson`
- Test: `test/integration/03_acl_policy_test.go`

**Interfaces:**
- Consumes: usuário `marcelo` (Task 2).
- Produces: grupo `group:admins` (contém `marcelo@`), tag `tag:remoto-agent` (owner `marcelo@`),
  grant único `group:admins → tag:remoto-agent` (todas as portas) e regra `ssh` correspondente.
  Qualquer outro par é implicitamente negado (default-deny) — verificado na Tarefa 5.

- [ ] **Step 1: Escrever o teste que falha**

Criar `test/integration/03_acl_policy_test.go`:

```go
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
```

- [ ] **Step 2: Rodar o teste e confirmar que falha**

Run: `go test ./test/integration/... -tags=integration -run TestACLPolicyIsValid -v`
Expected: neste ponto a política ainda é `{}` (allow-all da Tarefa 1), então `headscale policy
check` já retorna válido para ela — o teste passaria "por acidente". Para confirmar que o teste
está de fato testando a política nova, rode primeiro com o arquivo **ainda não substituído** e
verifique manualmente que o conteúdo do arquivo é `{}` (não o de Step 3). Prossiga para Step 3 e
trate a validação real como o critério de sucesso.

- [ ] **Step 3: Escrever a política default-deny**

Substituir o conteúdo de `infra/headscale/acl-policy.hujson` (validado manualmente contra o
binário real — `headscale policy check` retornou "Policy is valid" para este conteúdo exato):

```jsonc
{
  "groups": {
    "group:admins": ["marcelo@"]
  },
  "tagOwners": {
    "tag:remoto-agent": ["marcelo@"]
  },
  "grants": [
    {
      "src": ["group:admins"],
      "dst": ["tag:remoto-agent"],
      "ip": ["*"]
    }
  ],
  "ssh": [
    {
      "action": "accept",
      "src": ["group:admins"],
      "dst": ["tag:remoto-agent"],
      "users": ["autogroup:nonroot", "root"]
    }
  ]
}
```

Como o arquivo é montado por bind mount, o container precisa reiniciar para reler o arquivo:

Run: `docker compose -f docker-compose.dev.yml restart headscale`

- [ ] **Step 4: Rodar o teste e confirmar que passa**

Run: `go test ./test/integration/... -tags=integration -run TestACLPolicyIsValid -v`
Expected: PASS, com saída de `headscale policy check` contendo `Policy is valid`.

- [ ] **Step 5: Commit**

```bash
git add infra/headscale/acl-policy.hujson test/integration/03_acl_policy_test.go
git commit -m "feat(fase1): política de ACL default-deny (group:admins -> tag:remoto-agent)"
```

---

### Task 4: Preauthkeys e registro dos três dispositivos de teste

**Files:**
- Create: `infra/docker/support-client.Dockerfile`
- Modify: `docker-compose.dev.yml`
- Test: `test/integration/04_enrollment_test.go`

**Interfaces:**
- Consumes: usuário `marcelo` id (Task 2), `tag:remoto-agent` (Task 3), `runCmdEnv` (Task 1).
- Produces: três nós registrados na overlay — `support-pc` (dispositivo pessoal do usuário
  `marcelo`, sem tag), `agent-a` e `agent-b` (ambos com `tag:remoto-agent`). IPs de overlay
  descobertos via `tailscale ip -4` (usados pelas Tarefas 5 e 6).

- [ ] **Step 1: Escrever o teste que falha**

Criar `test/integration/04_enrollment_test.go`:

```go
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
```

- [ ] **Step 2: Rodar o teste e confirmar que falha**

Run: `go test ./test/integration/... -tags=integration -run TestThreeDevicesEnrolled -v`
Expected: FAIL — os serviços `support-pc`, `agent-a`, `agent-b` ainda não existem no
`docker-compose.dev.yml` ("no such service").

- [ ] **Step 3: Adicionar os três serviços de teste ao compose**

Criar `infra/docker/support-client.Dockerfile` (o "Support PC" é o único papel que inicia sessões
SSH nesta fase, então é o único que precisa de um cliente OpenSSH — os agentes usam o servidor SSH
embutido do `tailscaled` via `--ssh`, sem precisar de pacote adicional):

```dockerfile
FROM tailscale/tailscale:v1.102.3
RUN apk add --no-cache openssh-client
```

Adicionar ao final de `docker-compose.dev.yml` (mantendo o serviço `headscale` já existente):

```yaml
  support-pc:
    build:
      context: .
      dockerfile: infra/docker/support-client.Dockerfile
    container_name: remoto-dev-support-pc
    cap_add:
      - NET_ADMIN
    devices:
      - /dev/net/tun
    environment:
      - TS_AUTHKEY=${SUPPORT_PC_AUTHKEY}
      - TS_EXTRA_ARGS=--login-server=http://headscale:8080 --accept-dns=false --hostname=support-pc
      - TS_USERSPACE=false
    depends_on:
      - headscale

  agent-a:
    image: tailscale/tailscale:v1.102.3
    container_name: remoto-dev-agent-a
    cap_add:
      - NET_ADMIN
    devices:
      - /dev/net/tun
    environment:
      - TS_AUTHKEY=${AGENT_A_AUTHKEY}
      - TS_EXTRA_ARGS=--login-server=http://headscale:8080 --accept-dns=false --hostname=agent-a --ssh
      - TS_USERSPACE=false
    depends_on:
      - headscale

  agent-b:
    image: tailscale/tailscale:v1.102.3
    container_name: remoto-dev-agent-b
    cap_add:
      - NET_ADMIN
    devices:
      - /dev/net/tun
    environment:
      - TS_AUTHKEY=${AGENT_B_AUTHKEY}
      - TS_EXTRA_ARGS=--login-server=http://headscale:8080 --accept-dns=false --hostname=agent-b --ssh
      - TS_USERSPACE=false
    depends_on:
      - headscale
```

- [ ] **Step 4: Rodar o teste e confirmar que passa**

Run: `go test ./test/integration/... -tags=integration -run TestThreeDevicesEnrolled -v`
Expected: PASS — os três nós aparecem em `headscale nodes list -o json` e todos têm IP de overlay
não vazio (faixa `100.64.0.0/10`).

- [ ] **Step 5: Commit**

```bash
git add infra/docker/support-client.Dockerfile docker-compose.dev.yml test/integration/04_enrollment_test.go
git commit -m "feat(fase1): enrollment de support-pc, agent-a e agent-b via preauthkey"
```

---

### Task 5: Conectividade L3 e isolamento lateral

**Files:**
- Test: `test/integration/05_connectivity_test.go`

**Interfaces:**
- Consumes: `tailscaleIP` (Task 4), containers `remoto-dev-support-pc`, `remoto-dev-agent-a`,
  `remoto-dev-agent-b`.
- Produces: prova de que `FAIL_CLOSED`/default-deny funciona de fato na rede, não só na validação
  estática da política (Task 3).

- [ ] **Step 1: Escrever o teste que falha**

Criar `test/integration/05_connectivity_test.go`:

```go
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
```

- [ ] **Step 2: Rodar o teste e confirmar que falha (ou passa por acaso, e por quê isso seria um problema)**

Run: `go test ./test/integration/... -tags=integration -run TestOverlayConnectivityAndIsolation -v`
Expected: se as Tarefas 1-4 já foram implementadas corretamente, este teste já deve passar de
primeira — isso é esperado aqui, porque o comportamento (ACL grants + ssh escritos na Tarefa 3) já
existe antes deste teste ser escrito. Este é o caso em que "vermelho" seria a exceção, não a regra:
se o teste FALHAR neste ponto, o bug está nas Tarefas 3 ou 4 (política incorreta ou containers não
tagueados como esperado), e deve ser corrigido ali antes de prosseguir — não neste teste.

- [ ] **Step 3: (sem implementação nova — este teste valida comportamento já implementado)**

Nenhuma mudança de infraestrutura é necessária neste passo. Se o Step 2 falhou, volte à Tarefa 3
(conteúdo de `acl-policy.hujson`) ou Tarefa 4 (tags aplicadas aos preauthkeys) e corrija lá.

- [ ] **Step 4: Rodar o teste e confirmar que passa**

Run: `go test ./test/integration/... -tags=integration -run TestOverlayConnectivityAndIsolation -v`
Expected: PASS — `support-pc` alcança `agent-a` e `agent-b`; `agent-a` é negado ao tentar alcançar
`agent-b`.

- [ ] **Step 5: Commit**

```bash
git add test/integration/05_connectivity_test.go
git commit -m "test(fase1): prova conectividade L3 support-pc->agentes e isolamento agente<->agente"
```

---

### Task 6: SSH funcional sobre a overlay (Plane D)

**Files:**
- Test: `test/integration/06_ssh_test.go`

**Interfaces:**
- Consumes: `tailscaleIP` (Task 4), container `remoto-dev-support-pc` (tem `openssh-client` desde a
  Task 4), regra `ssh` da política (Task 3).
- Produces: confirmação de que o Plane D (SSH Administration Plane) funciona fim-a-fim usando
  exclusivamente o IP da overlay.

- [ ] **Step 1: Escrever o teste que falha**

Criar `test/integration/06_ssh_test.go`:

```go
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

	out, err := tailscaleSSH(t, "remoto-dev-agent-a", agentBIP, "echo should-not-run")
	if err == nil {
		t.Fatalf("expected agent-a -> agent-b SSH to be denied by ACL, but it succeeded: %s", out)
	}
}
```

- [ ] **Step 2: Rodar o teste e confirmar que falha**

Run: `go test ./test/integration/... -tags=integration -run TestSSHOverOverlay -v`
Expected: nesta fase o comportamento correto já deve existir (a regra `ssh` foi escrita na Tarefa
3, e o `--ssh` do `tailscaled` foi ligado nos agentes na Tarefa 4) — assim como na Tarefa 5, espera-
se que o teste já passe. Se falhar, o diagnóstico mais provável é: (a) a regra `ssh` da política
não inclui `"users": ["root", ...]` corretamente, ou (b) o container `agent-a`/`agent-b` não subiu
com `--ssh` no `TS_EXTRA_ARGS`.

- [ ] **Step 3: (sem implementação nova — mesma lógica da Tarefa 5)**

Se necessário, corrigir a política (Tarefa 3) ou os `TS_EXTRA_ARGS` dos agentes (Tarefa 4).

- [ ] **Step 4: Rodar os testes e confirmar que passam**

Run: `go test ./test/integration/... -tags=integration -run TestSSH -v`
Expected: PASS em ambos — `support-pc` consegue SSH em `agent-a`; `agent-a` é negado ao tentar SSH
em `agent-b`.

- [ ] **Step 5: Commit**

```bash
git add test/integration/06_ssh_test.go
git commit -m "test(fase1): SSH funcional sobre a overlay (Plane D) e negado por padrão fora do grant"
```

---

### Task 7: Provisionamento de produção (systemd) — escrito, não executado

Este é o único ponto do plano que produz artefatos para o servidor real (`remoto.darckware.net`),
fora do ambiente de dev via Docker das Tarefas 1-6. **Nenhum destes arquivos é executado contra
infraestrutura real como parte desta tarefa** — a implantação em produção é uma ação autorizada
separadamente, fora do escopo deste plano de código.

**Files:**
- Create: `infra/headscale/install.sh`
- Create: `infra/headscale/remoto-headscale.service`
- Create: `infra/headscale/config.prod.yaml.example`
- Create: `docs/runbooks/deploy-headscale-producao.md`

**Interfaces:**
- Consumes: nada dos testes anteriores (script standalone).
- Produces: procedimento documentado de deploy manual para a Fase 3 (Plane A / Cloudflare) usar.

- [ ] **Step 1: Escrever `infra/headscale/install.sh`**

```bash
#!/usr/bin/env bash
# Provisiona o Headscale 0.29.3 em um host Linux de produção.
# Idempotente: pode ser executado múltiplas vezes com segurança.
# NUNCA execute este script sem ler docs/runbooks/deploy-headscale-producao.md primeiro.
set -euo pipefail

HEADSCALE_VERSION="0.29.3"
INSTALL_DIR="/opt/headscale"
BIN_PATH="/usr/local/bin/headscale"
CONFIG_DIR="/etc/headscale"
DATA_DIR="/var/lib/headscale"
ARCH="$(dpkg --print-architecture)"

if [ "$(id -u)" -ne 0 ]; then
  echo "este script precisa rodar como root (systemd, /usr/local/bin, /etc)" >&2
  exit 1
fi

mkdir -p "$INSTALL_DIR" "$CONFIG_DIR" "$DATA_DIR"

DOWNLOAD_URL="https://github.com/juanfont/headscale/releases/download/v${HEADSCALE_VERSION}/headscale_${HEADSCALE_VERSION}_linux_${ARCH}"

if [ ! -x "$BIN_PATH" ] || ! "$BIN_PATH" version 2>/dev/null | grep -q "$HEADSCALE_VERSION"; then
  echo "baixando headscale ${HEADSCALE_VERSION} (${ARCH})..."
  curl -fsSL -o "$BIN_PATH.tmp" "$DOWNLOAD_URL"
  chmod +x "$BIN_PATH.tmp"
  mv "$BIN_PATH.tmp" "$BIN_PATH"
else
  echo "headscale ${HEADSCALE_VERSION} já instalado em ${BIN_PATH}, pulando download"
fi

if [ ! -f "$CONFIG_DIR/config.yaml" ]; then
  echo "AVISO: $CONFIG_DIR/config.yaml não existe. Copie e adapte" \
       "infra/headscale/config.prod.yaml.example manualmente antes de iniciar o serviço." >&2
fi

install -m 644 "$(dirname "$0")/remoto-headscale.service" /etc/systemd/system/remoto-headscale.service
systemctl daemon-reload

echo "instalação concluída. Revise ${CONFIG_DIR}/config.yaml e a política de ACL, depois rode:"
echo "  systemctl enable --now remoto-headscale"
```

Run: `chmod +x infra/headscale/install.sh`

- [ ] **Step 2: Escrever `infra/headscale/remoto-headscale.service`**

```ini
[Unit]
Description=Remoto — Headscale coordination server
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart=/usr/local/bin/headscale serve
WorkingDirectory=/var/lib/headscale
Restart=on-failure
RestartSec=5
User=root
AmbientCapabilities=CAP_NET_BIND_SERVICE

[Install]
WantedBy=multi-user.target
```

- [ ] **Step 3: Escrever `infra/headscale/config.prod.yaml.example`**

Cópia de `config.dev.yaml` (Tarefa 1) com os campos que mudam em produção comentados:

```yaml
# Copie para /etc/headscale/config.yaml no servidor e ajuste os valores marcados com CHANGEME.
server_url: https://remoto.darckware.net  # CHANGEME se o domínio mudar
listen_addr: 127.0.0.1:8080  # Plane A (reverse proxy/Cloudflare Tunnel) fala com este endereço,
                              # NUNCA 0.0.0.0, conforme requisito de não expor a porta diretamente.
metrics_listen_addr: 127.0.0.1:9090
grpc_listen_addr: 127.0.0.1:50443
grpc_allow_insecure: false

noise:
  private_key_path: /var/lib/headscale/noise_private.key

prefixes:
  v4: 100.64.0.0/10
  v6: fd7a:115c:a1e0::/48

database:
  type: sqlite
  sqlite:
    path: /var/lib/headscale/db.sqlite
    write_ahead_log: true

policy:
  mode: file
  path: /etc/headscale/acl-policy.hujson  # CHANGEME: copiar a política real, nunca a de dev

dns:
  magic_dns: false
  base_domain: remoto.darckware.net  # CHANGEME se o domínio mudar
  override_local_dns: false

derp:
  server:
    enabled: false
  urls:
    - https://controlplane.tailscale.com/derpmap/default
  paths: []
  auto_update_enabled: true
  update_frequency: 24h

log:
  level: info
```

- [ ] **Step 4: Escrever o runbook de deploy**

Criar `docs/runbooks/deploy-headscale-producao.md`:

```markdown
# Runbook: deploy do Headscale em produção (remoto.darckware.net)

**Não execute este runbook sem autorização explícita para alterar o servidor de produção.**

## Pré-requisitos

- Acesso root ao host de produção definido em `INFRASTRUCTURE_PROVIDER` (spec, seção 2).
- DNS de `remoto.darckware.net` já apontando para a infraestrutura Cloudflare/servidor (Plane A,
  fora do escopo deste runbook — ver Fase 3 do roteiro na spec).

## Passos

1. Copiar `infra/headscale/` para o servidor (ex.: `scp -r infra/headscale usuario@servidor:/tmp/`).
2. Copiar `infra/headscale/config.prod.yaml.example` para `/etc/headscale/config.yaml` e substituir
   todos os `CHANGEME`.
3. Copiar a política de ACL real (não a de dev) para `/etc/headscale/acl-policy.hujson`.
4. Rodar `sudo /tmp/headscale/install.sh`.
5. Rodar `sudo systemctl enable --now remoto-headscale`.
6. Verificar: `curl -s https://remoto.darckware.net/health` deve retornar `{"status":"pass"}`
   (depende do Plane A/reverse proxy já estar configurado — Fase 3).
7. Criar o usuário administrador real: `headscale users create <nome>`.
8. Copiar a política de ACL de produção (não reaproveitar a de dev) e validar com
   `headscale policy check --file /etc/headscale/acl-policy.hujson`.

## Rollback

`systemctl stop remoto-headscale` interrompe o serviço sem apagar dados (`/var/lib/headscale`
permanece intacto).
```

- [ ] **Step 5: Commit**

```bash
git add infra/headscale/install.sh infra/headscale/remoto-headscale.service \
        infra/headscale/config.prod.yaml.example docs/runbooks/deploy-headscale-producao.md
git commit -m "docs(fase1): script de provisionamento e runbook de deploy do Headscale em produção"
```

---

## Verificação final da Fase 1

Após todas as tarefas, rodar a suíte completa do zero garante que nada ficou acoplado a estado
manual deixado por um passo anterior:

```bash
docker compose -f docker-compose.dev.yml down -v
go test ./test/integration/... -tags=integration -v
```

Nota: como a Tarefa 2 cria o usuário `marcelo` e a Tarefa 4 gera preauthkeys via comando manual
documentado inline nos testes, uma execução do zero exige recriar o usuário (Tarefa 2, Step 3)
antes de rodar a suíte completa — isso é esperado nesta fase (a automação completa de bootstrap do
usuário/ACL fica para o Management Server na Fase 2, que hoje ainda não existe).
