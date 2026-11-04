#!/usr/bin/env bash
# RSCON daemon installer script
# Installs rscond binary and systemd unit

set -euo pipefail

RS_COND_BINARY="${RS_COND_BINARY:-rscond}"
RS_COND_URL="${RS_COND_URL:-https://github.com/rsdenck/rscon/releases/download/v0.1.1/rscond-linux-amd64}"
CONFIG_DIR="${CONFIG_DIR:-/etc/rscon}"
DATA_DIR="${DATA_DIR:-/var/lib/rscon}"
LOG_DIR="${LOG_DIR:-/var/log}"

echo "=== RSCON rscond Installer ==="
echo ""

# Check if running as root
if [ "$(id -u)" -ne 0 ]; then
    echo "ERROR: This installer must be run as root"
    exit 1
fi

# Create directories
echo "Creating directories..."
mkdir -p "${CONFIG_DIR}"
mkdir -p "${DATA_DIR}"
mkdir -p "${LOG_DIR}"

# Download binary
echo "Downloading rscond binary..."
curl -fsSL "${RS_COND_URL}" -o /usr/local/bin/${RS_COND_BINARY}
chmod +x /usr/local/bin/${RS_COND_BINARY}

# Create systemd unit
echo "Creating systemd unit..."
cat > /etc/systemd/system/rscond.service << 'UNITEOF'
[Unit]
Description=RSCON SSH Bastion Daemon
After=network.target postgresql.service
Wants=postgresql.service

[Service]
Type=notify
ExecStart=/usr/local/bin/rscond -config /etc/rscon/config.yaml
ExecReload=/bin/kill -HMAINPID
Restart=on-failure
RestartSec=5
LimitNOFILE=65536
AmbientCapabilities="~CAP_NET_ADMIN~CAP_NET_RAW"

[Install]
WantedBy=multi-user.target
UNITEOF

# Create default config if not exists
if [ ! -f "${CONFIG_DIR}/config.yaml" ]; then
    echo "Creating default configuration..."
    cat > "${CONFIG_DIR}/config.yaml" << 'CONFEOF'
# RSCON default configuration
# See: https://github.com/rsdenck/rscon

server:
  # API server configuration
  bind_addr: "127.0.0.1:18080"
  # TLS configuration - TLS is always verified, never disabled
  # TLS certificate and key would be configured here for HTTPS API
  
  # SSH gateway configuration
  # Listen address for SSH connections
  # ssh_listen_addr: "0.0.0.0:22"
  
  # Gateway settings
  max_concurrent_sessions: 5
  idle_timeout: "30m"
  max_session_duration: "8h"
  
  # Policy defaults
  # default_policy: "deny"
  # forward_agent: false
  # forward_x11: false
  
  # Certificate authority configuration
  # ca:
  #   type: "vault"  # or "file" (development only!)
  #   file:
  #     cert_path: "/path/to/ca.crt"
  #     key_path: "/path/to/ca.key"  # NEVER in production without Vault!
  
  # Vault configuration
  # vault:
  #   address: "http://127.0.0.1:8200"
  #   role: "approle"
  #   path: "ssh"

recording:
  # Recording backend configuration
  backend: "filesystem"  # or "s3" (experimental)
  # filesystem root directory
  filesystem_root: "/var/lib/rscon/recordings"
  # maximum recording size in bytes (default: 256MiB)
  max_file_bytes: 268435456
  # retention period (default: never delete)
  retention: "never"
  
  # S3 backend (experimental - pending)
  # s3:
  #   bucket_name: "my-recordings"
  #   region: "us-east-1"
  #   endpoint: "s3.amazonaws.com"
  #   access_key_id: ""
  #   secret_access_key: ""
  #   use_path_style_endpoint: false

security:
  # Bootstrap administrator password
  # Set via RSCON_BOOTSTRAP_ADMIN_PASSWORD environment variable
  # Minimum 12 characters, Argon2id hashing (64 MiB, t=3, p=2)
  
  # Rate limiting
  api_rate_limit: "100/m"
  
  # TLS configuration - TLS verification never disabled
  # tlsv13_only: true

audit:
  # Hash-chained audit configuration
  # Advisory lock key: 0x5253434f4e
  # Genesis hash: 64 zeros
  # Events are immutable and hash-chained
  
  # Log level
  log_level: "info"

# Environment variable substitution is supported
# ${VARIABLE_NAME} will be replaced at runtime
CONFEOF
    echo "Default configuration created at ${CONFIG_DIR}/config.yaml"
fi

# Reload systemd
echo "Reloading systemd..."
systemctl daemon-reload

# Enable and start rscond
echo "Enabling and starting rscond..."
systemctl enable --now rscond

# Wait for daemon to be ready
echo "Waiting for rscond to be ready..."
for i in $(seq 1 30); do
    if curl -s http://127.0.0.1:18080/api/v1/health > /dev/null 2>&1; then
        echo "rscond is healthy"
        break
    fi
    sleep 1
done

echo ""
echo "=== Installation Complete ==="
echo "RSCON rscond is installed and running."
echo "  - Binary: /usr/local/bin/rscond"
echo "  - Config: /etc/rscon/config.yaml"
echo "  - Systemd: systemctl status rscond"
echo "  - API: http://127.0.0.1:18080/api/v1/health"
echo "  - Login: rscon login admin \${RSCON_BOOTSTRAP_ADMIN_PASSWORD}"
echo ""
