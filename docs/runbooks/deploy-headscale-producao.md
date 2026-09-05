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
