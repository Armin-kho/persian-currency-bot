#!/usr/bin/env bash
set -euo pipefail

SERVICE="persian-currency-bot.service"
CONFIG_DIR="/etc/persian-currency-bot"
DATA_DIR="/var/lib/persian-currency-bot"
BIN_PATH="/usr/local/bin/persian-currency-bot"
RUN_USER="pcb"

if [[ "${EUID:-$(id -u)}" -ne 0 ]]; then
  echo "Please run as root (sudo)."
  exit 1
fi

echo "Stopping systemd service (if exists)..."
if systemctl list-units --full -all | grep -Fq "$SERVICE"; then
  systemctl disable --now "$SERVICE" || true
  rm -f "/etc/systemd/system/$SERVICE" || true
  systemctl daemon-reload || true
fi

echo "Stopping docker container (if running)..."
if command -v docker >/dev/null 2>&1; then
  docker rm -f persian-currency-bot 2>/dev/null || true
fi

echo "Removing binary/config/data..."
rm -f "$BIN_PATH" || true
rm -rf "$CONFIG_DIR" || true
rm -rf "$DATA_DIR" || true

echo "Removing user..."
if id "$RUN_USER" >/dev/null 2>&1; then
  userdel "$RUN_USER" 2>/dev/null || true
  deluser "$RUN_USER" 2>/dev/null || true
fi

echo "✅ Uninstall complete."
