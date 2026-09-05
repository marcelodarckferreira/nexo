# Remoto — Design de Arquitetura

**Projeto:** Remoto
**Autor:** Marcelo (via Porthus / Claude Code)
**Data:** 2026-09-04
**Versão:** 1.1
**Status:** Aprovado para plano de implementação
**Classificação:** Uso interno

---

## 1. Objetivo

O Remoto é uma plataforma de gerenciamento remoto autorizado de máquinas, servidores e redes
pertencentes ou explicitamente autorizadas pelo administrador. O acesso é operado através de uma
CLI administrativa (`remotoctl`), com um agente instalado nos dispositivos gerenciados e um
servidor central de gestão.

A arquitetura minimiza ao máximo a conectividade inbound: nem o agente remoto nem o servidor de
origem dependem de portas abertas para conexões vindas da Internet. Toda comunicação de controle é
iniciada pelo lado que precisa de menos confiança (o agente), nunca pelo servidor central em
direção ao agente.

### 1.1 Não-objetivos e limites de segurança

Este documento **não** especifica, e a implementação **não deve** incluir:

- qualquer mecanismo de ocultação de processo, evasão de EDR/antivírus, ou desativação de firewall;
- persistência furtiva ou command-and-control não identificável ao usuário do dispositivo;
- bypass de autenticação, bypass de proxy corporativo ou bypass de DPI;
- acesso a qualquer dispositivo sem enrollment explícito e autorizado pelo administrador do Remoto.

Todo agente é visível, identificável, desinstalável pelo usuário local e reportado no inventário
central. O objetivo é suporte remoto e administração de infraestrutura autorizada — não vigilância
oculta.

---

## 2. Variáveis de ambiente e decisões assumidas

As variáveis abaixo foram fornecidas parcialmente. Onde não preenchidas, foi escolhida uma
alternativa conservadora, marcada como `ASSUMPTION`, com uma rota clara de substituição.

| Variável | Valor | Origem |
|---|---|---|
| `PROJECT_NAME` | `Remoto` | ASSUMPTION — deriva do nome do diretório/hostname |
| `CLI_NAME` | `remotoctl` | ASSUMPTION — evita colisão com o nome do produto |
| `SERVER_OS` | Linux | ASSUMPTION — consistente com o restante da infraestrutura do operador |
| `AGENT_OS_LIST` | Linux, Windows, macOS | ASSUMPTION — os três já são suportados pelos clientes Tailscale/Headscale reaproveitados |
| `IMPLEMENTATION_LANGUAGE` | Go | ASSUMPTION — nenhum padrão organizacional obriga linguagem; Go interopera com o código-fonte do Headscale e compila agentes single-binary para os três SOs |
| `DATABASE` | PostgreSQL | ASSUMPTION — motor suportado nativamente pelo Headscale em produção e compatível com o padrão de modelagem em 3FN |
| `EXPECTED_AGENT_COUNT` | pequeno/médio (< 500) | ASSUMPTION — evita HA/sharding prematuro; revisitar se a base crescer |
| `INFRASTRUCTURE_PROVIDER` | VPS/bare-metal próprio + Cloudflare | ASSUMPTION |
| `PUBLIC_HOSTNAME` | `remoto.darckware.net` | Fornecido pelo usuário |
| `INTERNAL_FILE_SERVICE` | `127.0.0.1:3333` | Fornecido pelo usuário |
| `CLOUDFLARE_ENABLED` | `true` | Decorre do `PUBLIC_HOSTNAME` e da seção Cloudflare Mode fornecidas |
| `VPN_TECHNOLOGY` | WireGuard, via protocolo Headscale/Tailscale | Decorre da decisão de overlay L3 completo (seção 4.3) |

Qualquer uma dessas escolhas pode ser revista sem impacto estrutural no restante do design — são
parâmetros de configuração, não decisões de arquitetura.

---

## 3. Requisitos arquiteturais primários

```text
NO_DIRECT_AGENT_INBOUND_DEPENDENCY = true
OUTBOUND_INITIATED_CONTROL_CHANNEL = true
TLS_REQUIRED = true
TLS_MINIMUM_VERSION = 1.3

CLOUDFLARE_COMPATIBLE = true
DIRECT_OUTBOUND_TRANSPORT_SUPPORTED = true

ZERO_TRUST_IDENTITY = true
DEVICE_SPECIFIC_IDENTITY = true

PRIVATE_OVERLAY_NETWORK = true
SSH_OVER_PRIVATE_NETWORK = true

CENTRAL_MANAGEMENT = true
AUDIT_LOGGING = true

FAIL_CLOSED = true
```

`CLOUDFLARE_COMPATIBLE` e `DIRECT_OUTBOUND_TRANSPORT_SUPPORTED` coexistem: o Plane A (seção 4)
suporta tanto o Cloudflare Tunnel quanto um HTTPS Gateway direto próprio, selecionáveis por
configuração (`CLOUDFLARE_ENABLED`), sem alterar o restante da arquitetura.

---

## 4. Arquitetura — separação em planos

A arquitetura é obrigatoriamente separada em quatro planos independentes, cada um com uma
responsabilidade única e uma interface clara com os demais.

### 4.1 Plane A — Public Ingress

Responsável por publicar `remoto.darckware.net` sem expor `127.0.0.1:3333` diretamente à Internet.

```text
Internet
   |
   v
remoto.darckware.net
   |
   v
HTTPS :443
   |
   v
Cloudflare Tunnel  (CLOUDFLARE_ENABLED=true)
   ou HTTPS Gateway direto próprio (CLOUDFLARE_ENABLED=false)
   |
   v
Reverse Proxy
   |
   v
127.0.0.1:3333
```

A porta `3333/tcp` permanece bound em `127.0.0.1`. Nunca é feito bind em `0.0.0.0:3333` sem
necessidade arquitetural explícita e documentada.

**Componente:** `cloudflared` (dependência de runtime, BSD-3-Clause, mantida pela Cloudflare) +
reverse proxy próprio (Caddy ou equivalente, decisão de implementação). Nenhum fork necessário
neste plano.

### 4.2 Plane B — Agent Control Plane

Responsável por enrollment, autenticação, heartbeat, configuração, mensagens de controle, status
de dispositivo e negociação de sessão. O agente sempre inicia a conexão.

```text
Remote Agent
     |
     | outbound authenticated TLS 1.3
     v
Management Gateway
     |
     v
Management Server
```

Não existe dependência normal de conexão inbound Internet → Agent.

### 4.3 Plane C — Private Overlay / Data Plane

Rede privada autenticada entre dispositivos autorizados, com ACLs explícitas.

```text
Management Server
        |
Authenticated Private Overlay (WireGuard)
        |
   +----+-------------+
   |          |        |
Agent A    Agent B  Support PC
```

**Decisão de composição (aprovada em 2026-09-04):** overlay L3 completo — qualquer agente
autorizado alcança qualquer outro dispositivo autorizado por IP privado, sem sessão mediada por
relay. Base: **Headscale** (BSD-3-Clause), reaproveitando os clientes oficiais Tailscale, que já
entregam identidade por dispositivo, distribuição de chaves WireGuard, ACLs de rede e heartbeat.
Headscale é vendorizado/incorporado ao Management Server; não é forkado como produto isolado.

Motivo da escolha frente às alternativas pesquisadas (ver seção 9): licença mais limpa
(BSD-3-Clause, contra `NOASSERTION` de NetBird/Narrowlink/OpenRport e AGPL-3.0 de Octelium),
maior atividade e comunidade, e reaproveitamento de clientes oficiais já maduros.

### 4.4 Plane D — SSH Administration Plane

SSH utiliza exclusivamente a rede privada (Plane C) por padrão.

```text
Support PC
     |
Private Overlay
     |
Agent VPN IP
     |
SSH
```

Os agentes nunca expõem a porta SSH diretamente à Internet por padrão. Qualquer exceção exige
configuração explícita e é registrada em audit log.

---

## 5. Componentes do produto

Os planos B/C/D fornecem a infraestrutura de rede e identidade (Headscale). Os componentes abaixo
são o produto Remoto propriamente dito, construídos por cima dessa infraestrutura:

| Componente | Responsabilidade |
|---|---|
| **Management Server** | Orquestra enrollment, autenticação, políticas de ACL, sessões de suporte, audit logging e observabilidade. Consome a API do Headscale para gestão de dispositivos e rede. |
| **`remotoctl` (CLI administrativa)** | Interface de operação: enrollment de dispositivos, gestão de ACLs, abertura/encerramento de sessões de suporte, consulta de audit log, status de agentes. |
| **Remote Agent** | Processo instalado no dispositivo gerenciado. Combina o cliente Tailscale/Headscale (overlay) com um agente de controle próprio (heartbeat, metadados, execução de comandos autorizados). |
| **HTTPS Gateway / Reverse Proxy** | Plane A. Publica serviços internos (`127.0.0.1:3333`) via `remoto.darckware.net`. |
| **Motor de sessões e audit logging** | Registra toda sessão de suporte/administração (quem, quando, para qual dispositivo, o que foi executado), com retenção e formato compatíveis com os padrões organizacionais de governança de dados. |

### 5.1 Indicador visual de sessão ativa ("Client Mode")

Enquanto uma sessão de suporte/administração estiver ativa em um dispositivo gerenciado, o Remote
Agent exibe na tela do dispositivo um indicador visual persistente contendo a logomarca Darckware
(`assets/branding/darckware-lockup-dark.svg`) e uma indicação textual de que uma sessão remota está
em andamento (ex.: "Suporte remoto ativo").

Regras do indicador:

- aparece automaticamente ao início da sessão e desaparece automaticamente ao encerramento — não é
  configurável para iniciar oculto;
- não pode ser ocultado, minimizado ou encerrado pelo lado do operador remoto — apenas o usuário
  local do dispositivo ou o encerramento real da sessão o removem;
- é um requisito de transparência ao usuário do dispositivo, consistente com a seção 1.1 (nenhum
  agente oculto, furtivo ou de vigilância não identificável).

Detalhamento de implementação (overlay always-on-top, comportamento em múltiplos monitores,
equivalentes por SO) fica para o plano de implementação da Fase 4.

---

## 6. Requisitos de segurança e falha

- Todo tráfego público usa TLS 1.3 como mínimo.
- Validação de certificado e de hostname é obrigatória; `verify=false` ou equivalente é proibido em
  produção.
- Falha de validação TLS resulta em `FAIL_CLOSED` — a conexão é recusada, nunca degradada para um
  modo inseguro.
- Renovação de certificado, configuração segura de cifras e logging de falha de TLS são
  responsabilidades do Plane A.
- Identidade é por dispositivo (device-specific identity), não compartilhada entre agentes.
- Autorização de rede (quem alcança quem no Plane C) é explícita via ACL — negar por padrão.

---

## 7. DNS

`remoto.darckware.net` resolve para a infraestrutura pública/Cloudflare do Plane A — DNS não é
tratado como mecanismo de port forwarding. O caminho real de uma requisição pública é:

```text
DNS → HTTPS endpoint → reverse proxy / Cloudflare Tunnel → 127.0.0.1:3333
```

---

## 8. Roteiro de entrega faseado

Dado o tamanho do sistema, a implementação é decomposta em fases sequenciais, cada uma com seu
próprio plano de implementação (via skill `writing-plans`), depois desta spec de arquitetura única:

1. **Fase 1 — Overlay e identidade (Planes B+C+D):** incorporação do Headscale, enrollment de
   dispositivo, ACLs básicas, SSH funcional sobre a overlay.
2. **Fase 2 — Management Server e `remotoctl` (v1):** CLI mínima cobrindo enrollment, status e
   ACLs administradas via Management Server (não diretamente no Headscale).
3. **Fase 3 — Plane A (Cloudflare/Gateway):** publicação de `127.0.0.1:3333` via
   `remoto.darckware.net`, com os dois modos de conectividade coexistindo.
4. **Fase 4 — Sessões de suporte, audit logging e observabilidade:** motor de sessões,
   audit trail completo, métricas/logs.

---

## 9. Alternativas consideradas (pesquisa de fork)

Pesquisa realizada em 2026-09-04 via GitHub API para avaliar projetos open-source existentes como
base de fork:

| Projeto | Stars | Licença | Veredito |
|---|---|---|---|
| Headscale | 43,6k | BSD-3-Clause | **Escolhido** para Planes B/C/D |
| NetBird | 28,9k | `NOASSERTION` (não padrão) | Descartado — ambiguidade de licença |
| MeshCentral | 7,1k | Apache-2.0 | Descartado para este design — modelo de sessão mediada, não overlay L3 |
| Octelium | 4,0k | AGPL-3.0 | Descartado — implicação legal de disponibilização de fonte para produto comercial hospedado |
| Narrowlink | 658 | `NOASSERTION` | Descartado — comunidade pequena, licença ambígua |
| OpenRport | 398 | `NOASSERTION` | Descartado — sem commits desde jun/2025, risco de abandono |

Nenhum candidato cobre os quatro planos isoladamente; a composição escolhida (cloudflared +
Headscale + Management Server próprio) evita forçar um monólito a cumprir um papel para o qual não
foi desenhado.

---

## 10. Testes (alto nível)

Detalhamento fica para os planos de implementação de cada fase (seção 8). Em alto nível:

- Fase 1: testes de integração de enrollment e conectividade da overlay (dispositivo real ou VM).
- Fase 2/3: testes de contrato da API do Management Server e da CLI; teste de que `3333` nunca
  fica acessível publicamente sem passar pelo Plane A.
- Fase 4: teste de que toda sessão de suporte gera entrada de audit log íntegra e imutável.
