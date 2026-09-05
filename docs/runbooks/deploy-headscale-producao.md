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
3. Preparar a política de ACL de produção: **não existe** (nem deve existir) um template de
   política de produção separado no repositório — a política de produção é sempre derivada da
   política de dev já commitada (`infra/headscale/acl-policy.hujson`), trocando apenas o nome de
   usuário. Partir desse arquivo e substituir TODAS as ocorrências de `marcelo@` pelo nome de
   usuário administrador real que será criado no passo 8 — os dois valores precisam ser
   idênticos. Salvar o resultado em `/etc/headscale/acl-policy.hujson` no servidor.
4. Rodar `sudo /tmp/headscale/install.sh` (ou o caminho onde os arquivos foram copiados no
   passo 1).
5. Rodar `sudo systemctl enable --now remoto-headscale`.
6. **Nota explícita:** entre este passo e o passo 8, o servidor está no ar mas a política de ACL
   copiada no passo 3 referencia um usuário (`group:admins`) que ainda não existe no Headscale —
   isso é um estado transitório esperado e seguro (grupo não resolvido = nenhum acesso concedido
   a ninguém, não é uma falha de segurança), não um bug. Não é motivo de alarme se o healthcheck
   do passo 7 passar mas nenhum dispositivo conseguir se conectar ainda.
7. Verificar: `curl -s https://remoto.darckware.net/health` deve retornar `{"status":"pass"}`
   (depende do Plane A/reverse proxy já estar configurado — Fase 3).
8. Criar o usuário administrador real: `headscale users create <nome>` — `<nome>` deve ser
   EXATAMENTE o mesmo nome usado para substituir `marcelo@` no passo 3.
9. Validar a política de ACL agora que o usuário existe: `headscale policy check --file
   /etc/headscale/acl-policy.hujson` — deve retornar "Policy is valid".

## Rollback

`systemctl stop remoto-headscale` interrompe o serviço sem apagar dados (`/var/lib/headscale`
permanece intacto).
