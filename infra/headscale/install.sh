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
