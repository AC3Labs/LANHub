#!/usr/bin/env bash
# LANHub agent installer for Linux (systemd), served by your LANHub hub.
#
#   curl -fsSL {{%.Hub%}}/agent/install.sh | sudo bash -s -- --token TOKEN
#
# Options: --token TOKEN (the machine's token from the hub), --port N
# (default 8765), --tls (a self-signed certificate, for HTTPS).
# Running it again updates the agent and keeps its settings and token.
set -euo pipefail

HUB="{{%.Hub%}}"
VERSION="{{%.Version%}}"
INSTALL_DIR=/opt/lanhub-agent
UNIT=/etc/systemd/system/lanhub-agent.service

TOKEN="" PORT="" TLS=false
while [ $# -gt 0 ]; do
    case "$1" in
        --token) TOKEN="${2:-}"; shift 2 ;;
        --port) PORT="${2:-}"; shift 2 ;;
        --tls) TLS=true; shift ;;
        *) echo "Unknown option: $1" >&2; exit 2 ;;
    esac
done

if [ "$(id -u)" -ne 0 ]; then
    echo "Run it with sudo: curl -fsSL $HUB/agent/install.sh | sudo bash -s -- --token TOKEN" >&2
    exit 1
fi
command -v systemctl >/dev/null || { echo "This installer needs systemd." >&2; exit 1; }

case "$(uname -m)" in
    x86_64|amd64) ARCH=amd64; SUM="{{%index .Sums "linux-amd64"%}}" ;;
    aarch64|arm64) ARCH=arm64; SUM="{{%index .Sums "linux-arm64"%}}" ;;
    *) echo "This hub has no agent build for $(uname -m)." >&2; exit 1 ;;
esac
[ -n "$SUM" ] || { echo "This hub has no agent build for linux/$ARCH." >&2; exit 1; }

# The agent runs as the account it already ran as, or the one that ran
# sudo; not root, unless that's who ran this. It owns its folder so it can
# update itself when the hub asks.
SERVICE_USER=""
[ -f "$UNIT" ] && SERVICE_USER="$(sed -n 's/^User=//p' "$UNIT" | head -n 1)"
if [ -z "$SERVICE_USER" ] || ! id "$SERVICE_USER" >/dev/null 2>&1; then
    SERVICE_USER="${SUDO_USER:-root}"
fi

TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
echo "Downloading the LANHub agent $VERSION (linux/$ARCH) from $HUB"
if command -v curl >/dev/null; then
    curl -fsSL "$HUB/agent/download/lanhub-agent-linux-$ARCH" -o "$TMP/lanhub-agent"
else
    wget -qO "$TMP/lanhub-agent" "$HUB/agent/download/lanhub-agent-linux-$ARCH"
fi
if ! echo "$SUM  $TMP/lanhub-agent" | sha256sum -c --quiet - >/dev/null 2>&1; then
    echo "The download doesn't match its checksum, so nothing was changed." >&2
    exit 1
fi
chmod 755 "$TMP/lanhub-agent"
if [ "$("$TMP/lanhub-agent" version 2>/dev/null)" != "$VERSION" ]; then
    echo "The downloaded agent won't run on this machine, so nothing was changed." >&2
    exit 1
fi

mkdir -p "$INSTALL_DIR"
systemctl stop lanhub-agent 2>/dev/null || true
install -m 755 "$TMP/lanhub-agent" "$INSTALL_DIR/lanhub-agent"

CONF=()
[ -n "$TOKEN" ] && CONF+=(--token "$TOKEN")
[ -n "$PORT" ] && CONF+=(--port "$PORT")
if [ "$TLS" = true ]; then
    mkdir -p "$INSTALL_DIR/certs"
    if [ ! -f "$INSTALL_DIR/certs/agent.crt" ]; then
        command -v openssl >/dev/null || { echo "--tls needs openssl installed." >&2; exit 1; }
        openssl req -x509 -newkey rsa:2048 -nodes -keyout "$INSTALL_DIR/certs/agent.key" \
            -out "$INSTALL_DIR/certs/agent.crt" -days 3650 -subj "/CN=$(hostname)" 2>/dev/null
        echo "Made a self-signed certificate (10 years). Tick \"Use HTTPS\" for this machine in the hub."
    fi
    CONF+=(--cert "$INSTALL_DIR/certs/agent.crt" --key "$INSTALL_DIR/certs/agent.key")
fi
"$INSTALL_DIR/lanhub-agent" configure ${CONF[@]+"${CONF[@]}"}
chown -R "$SERVICE_USER" "$INSTALL_DIR"

cat > "$UNIT" <<EOF
[Unit]
Description=LANHub Agent
After=network.target

[Service]
Type=simple
User=$SERVICE_USER
WorkingDirectory=$INSTALL_DIR
ExecStart=$INSTALL_DIR/lanhub-agent
Restart=always
RestartSec=3

[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload
systemctl enable lanhub-agent >/dev/null 2>&1

if ! "$INSTALL_DIR/lanhub-agent" has-token; then
    echo "Installed, but it has no token yet. Run this again with --token TOKEN (from the hub's Add Machine)."
    exit 0
fi
systemctl restart lanhub-agent
sleep 2
if ! systemctl is-active --quiet lanhub-agent; then
    echo "The agent didn't start. See: journalctl -u lanhub-agent -n 50" >&2
    exit 1
fi
echo "The LANHub agent $VERSION is running (as $SERVICE_USER)."
if command -v csf >/dev/null; then
    echo "This machine runs CSF: allow the agent's port in /etc/csf/csf.conf (TCP_IN) and run 'csf -r' if the hub can't reach it."
fi
