#!/usr/bin/env bash
# Installs the LANHub agent as a systemd service on Ubuntu/Debian.
# The agent itself is a single static binary — nothing else to install.
# Pass --tls to also generate a self-signed cert and enable HTTPS.
set -euo pipefail

INSTALL_DIR="/opt/lanhub-agent"
SERVICE_USER="${SUDO_USER:-$USER}"
TLS=false
for arg in "$@"; do
    case "$arg" in
        --tls) TLS=true ;;
    esac
done

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

sudo mkdir -p "$INSTALL_DIR"
sudo cp "$SCRIPT_DIR/../dist/lanhub-agent-linux-amd64" "$INSTALL_DIR/lanhub-agent"
sudo chmod +x "$INSTALL_DIR/lanhub-agent"

CONFIG_FRESH=false
if [ ! -f "$INSTALL_DIR/config.json" ]; then
    sudo cp "$SCRIPT_DIR/../config.example.json" "$INSTALL_DIR/config.json"
    echo "Wrote default config to $INSTALL_DIR/config.json — edit the token before starting the service."
    CONFIG_FRESH=true
fi

if [ "$TLS" = true ]; then
    sudo mkdir -p "$INSTALL_DIR/certs"
    if [ ! -f "$INSTALL_DIR/certs/agent.crt" ]; then
        sudo openssl req -x509 -newkey rsa:2048 -nodes \
            -keyout "$INSTALL_DIR/certs/agent.key" \
            -out "$INSTALL_DIR/certs/agent.crt" \
            -days 3650 -subj "/CN=$(hostname)"
        echo "Generated a self-signed TLS cert at $INSTALL_DIR/certs/agent.crt (valid 10 years). This is for a trusted LAN — it will not validate against a public CA."
    fi

    if [ "$CONFIG_FRESH" = true ]; then
        sudo python3 - "$INSTALL_DIR/config.json" "$INSTALL_DIR/certs/agent.crt" "$INSTALL_DIR/certs/agent.key" <<'PYEOF'
import json, sys
path, cert, key = sys.argv[1], sys.argv[2], sys.argv[3]
with open(path) as f:
    data = json.load(f)
data["cert_file"] = cert
data["key_file"] = key
with open(path, "w") as f:
    json.dump(data, f, indent=2)
PYEOF
    else
        echo "config.json already existed — set \"cert_file\": \"$INSTALL_DIR/certs/agent.crt\" and \"key_file\": \"$INSTALL_DIR/certs/agent.key\" in it yourself to enable HTTPS."
    fi
fi

sudo chown -R "$SERVICE_USER" "$INSTALL_DIR"

sudo sed "s/%i/${SERVICE_USER}/" "$SCRIPT_DIR/lanhub-agent.service" | sudo tee /etc/systemd/system/lanhub-agent.service > /dev/null

sudo systemctl daemon-reload
sudo systemctl enable lanhub-agent
echo "Edit $INSTALL_DIR/config.json, then run: sudo systemctl start lanhub-agent"
