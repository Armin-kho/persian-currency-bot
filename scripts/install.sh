#!/usr/bin/env bash
set -euo pipefail

# Persian Currency Bot installer
# Supports:
#  - systemd install (builds binary using go)
#  - docker compose install (builds container)

MODE="${1:-}"

if [[ "${EUID:-$(id -u)}" -ne 0 ]]; then
  echo "Please run as root (sudo)."
  exit 1
fi

detect_pm() {
  if command -v apt-get >/dev/null 2>&1; then echo "apt"; return; fi
  if command -v dnf >/dev/null 2>&1; then echo "dnf"; return; fi
  if command -v yum >/dev/null 2>&1; then echo "yum"; return; fi
  if command -v apk >/dev/null 2>&1; then echo "apk"; return; fi
  if command -v pacman >/dev/null 2>&1; then echo "pacman"; return; fi
  echo "unknown"
}

install_packages() {
  local pm="$1"; shift
  case "$pm" in
    apt)
      apt-get update -y
      DEBIAN_FRONTEND=noninteractive apt-get install -y "$@"
      ;;
    dnf)
      dnf install -y "$@"
      ;;
    yum)
      yum install -y "$@"
      ;;
    apk)
      apk add --no-cache "$@"
      ;;
    pacman)
      pacman -Sy --noconfirm "$@"
      ;;
    *)
      echo "Unknown package manager. Please install: $*"
      ;;
  esac
}

need_cmd() {
  command -v "$1" >/dev/null 2>&1
}

PROJECT_DIR="$(pwd)"
CONFIG_DIR="/etc/persian-currency-bot"
DATA_DIR="/var/lib/persian-currency-bot"
CONFIG_PATH="${CONFIG_DIR}/config.json"
BIN_PATH="/usr/local/bin/persian-currency-bot"
SERVICE_PATH="/etc/systemd/system/persian-currency-bot.service"
RUN_USER="pcb"

echo "=== Persian Currency Bot Installer ==="
echo "Project dir: $PROJECT_DIR"
echo "Config:      $CONFIG_PATH"
echo "Data dir:    $DATA_DIR"
echo

if [[ -z "$MODE" ]]; then
  if need_cmd docker && (need_cmd docker-compose || docker compose version >/dev/null 2>&1); then
    MODE="docker"
  else
    MODE="systemd"
  fi
fi

if [[ "$MODE" != "docker" && "$MODE" != "systemd" ]]; then
  echo "Usage: $0 [docker|systemd]"
  exit 1
fi

PM="$(detect_pm)"
echo "Detected package manager: $PM"

# Basic deps
install_packages "$PM" ca-certificates curl tzdata

mkdir -p "$CONFIG_DIR" "$DATA_DIR"

read -rp "Enter Telegram Bot Token (BOT_TOKEN): " BOT_TOKEN
read -rp "Enter initial admin user IDs (comma-separated) or leave blank: " ADMINS

# IMPORTANT NOTE: If you leave admins blank, the first person who starts the bot in private chat becomes the super admin.
cat > "$CONFIG_PATH" <<EOF
{
  "bot_token": "${BOT_TOKEN}",
  "data_dir": "${DATA_DIR}",
  "initial_admin_ids": [$(echo "$ADMINS" | awk -F',' '{for(i=1;i<=NF;i++){gsub(/^[ \t]+|[ \t]+$/,"",$i); if($i!=""){printf "%s%s", (c?"," : ""), $i; c=1}}}')],
  "bonbast_api_username": "",
  "bonbast_api_hash": "",
  "navasan_api_key": "",
  "debug": false
}
EOF

chmod 600 "$CONFIG_PATH"

if [[ "$MODE" == "docker" ]]; then
  echo "Installing in Docker mode..."

  if ! need_cmd docker; then
    echo "Docker not found. Please install docker first, then re-run with: $0 docker"
    exit 1
  fi

  if ! (need_cmd docker-compose || docker compose version >/dev/null 2>&1); then
    echo "Docker Compose not found. Please install docker-compose plugin."
    exit 1
  fi

  echo "Starting docker compose..."
  cd "$PROJECT_DIR"
  if need_cmd docker-compose; then
    docker-compose up -d --build
  else
    docker compose up -d --build
  fi

  echo
  echo "✅ Done."
  echo "Open the bot in Telegram. If initial_admin_ids was empty, the FIRST user who opens the bot becomes the super admin."
  exit 0
fi

echo "Installing in systemd mode..."

# Ensure go is installed
if ! need_cmd go; then
  echo "Go not found. Installing Go + git from distro packages..."
  case "$PM" in
    apt) install_packages "$PM" golang-go git ;;
    dnf) install_packages "$PM" golang git ;;
    yum) install_packages "$PM" golang git ;;
    apk) install_packages "$PM" go git ;;
    pacman) install_packages "$PM" go git ;;
    *) echo "Unknown distro. Please install Go and Git, then re-run."; exit 1 ;;
  esac
fi

# Create system user
if ! id "$RUN_USER" >/dev/null 2>&1; then
  if need_cmd adduser; then
    adduser -S -H -D -s /sbin/nologin "$RUN_USER" 2>/dev/null || true
  fi
  if ! id "$RUN_USER" >/dev/null 2>&1 && need_cmd useradd; then
    useradd --system --no-create-home --shell /usr/sbin/nologin "$RUN_USER"
  fi
fi

chown -R "$RUN_USER":"$RUN_USER" "$DATA_DIR"
chmod 750 "$DATA_DIR"

echo "Building binary..."
cd "$PROJECT_DIR"
CGO_ENABLED=0 go build -o "$BIN_PATH" ./cmd/bot

chmod 755 "$BIN_PATH"

cat > "$SERVICE_PATH" <<EOF
[Unit]
Description=Persian Currency Bot
After=network.target

[Service]
Type=simple
User=${RUN_USER}
Group=${RUN_USER}
ExecStart=${BIN_PATH} -config ${CONFIG_PATH}
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable --now persian-currency-bot.service

echo
echo "✅ Done."
echo "Service status:"
systemctl --no-pager status persian-currency-bot.service || true
echo
echo "NOTE: If initial_admin_ids was empty, the FIRST user who opens the bot in private chat becomes the super admin."
