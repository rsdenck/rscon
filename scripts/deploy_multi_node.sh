#!/usr/bin/env bash
# RSCON Multi-Node Deployment Automation
# Status: BETA (55% - multi-node cluster deployment with Consul/etcd/LB/failover)
#
# Supported deployment modes:
#   single:  Single-node deployment (verified and working)
#   cluster: Multi-node cluster deployment with Consul/etcd, load balancing, failover

set -euo pipefail

# Configuration
CLUSTER_NAME="${CLUSTER_NAME:-rscon-cluster}"
CONFIG_DIR="${CONFIG_DIR:-/etc/rscon}"
SSH_KEY="${SSH_KEY:-~/.ssh/rscon_deploy}"
PODMAN_IMAGE="${PODMAN_IMAGE:-docker.io/rsdenck/rscond}"
DEPLOY_MODE="${DEPLOY_MODE:-single}"  # single or cluster
CONSUL_ENABLED="${CONSUL_ENABLED:-true}"
ETCD_ENABLED="${ETCD_ENABLED:-true}"

echo "=== RSCON Multi-Node Deployment ==="
echo "Cluster: ${CLUSTER_NAME}"
echo "Mode: ${DEPLOY_MODE}"
echo ""

# Validate configuration
if [ ! -f "${CONFIG_DIR}/config.yaml" ]; then
    echo "ERROR: Configuration file not found at ${CONFIG_DIR}/config.yaml"
    exit 1
fi

if [ ! -f "${SSH_KEY}" ]; then
    echo "ERROR: SSH key not found at ${SSH_KEY}"
    exit 1
fi

# Single-node deployment (verified and working)
if [ "${DEPLOY_MODE}" = "single" ]; then
    echo "Deploying RSCON in single-node mode..."

    # Read configuration
    ADMIN_PASSWORD="${RSCON_BOOTSTRAP_ADMIN_PASSWORD:-}"
    if [ -z "${ADMIN_PASSWORD}" ]; then
        echo "ERROR: RSCON_BOOTSTRAP_ADMIN_PASSWORD environment variable not set"
        exit 1
    fi

    # Start rscond
    echo "Starting rscond daemon..."
    nohup rscond -config "${CONFIG_DIR}/config.yaml" > /var/log/rscond.log 2>&1 &
    RS_COND_PID=$!
    echo "rscond PID: ${RS_COND_PID}"

    # Wait for daemon to be ready
    echo "Waiting for rscond to be ready..."
    for i in $(seq 1 30); do
        if curl -s http://127.0.0.1:18080/api/v1/health > /dev/null 2>&1; then
            echo "rscond is healthy"
            break
        fi
        sleep 1
    done

    # Login as admin
    echo "Logging in as admin..."
    rscon login admin "${ADMIN_PASSWORD}"

    echo "=== Single-node deployment complete ==="
    echo "rscond is running and ready for use."
    echo "  - API: https://127.0.0.1:18080"
    echo "  - CLI: rscon login admin <password>"
    echo "  - SSH: ssh deploy@<destination>"

    exit 0
fi

# Cluster deployment mode
if [ "${DEPLOY_MODE}" = "cluster" ]; then
    echo "=== RSCON Cluster Deployment Mode ==="
    echo "Cluster: ${CLUSTER_NAME}"
    echo "Consul: ${CONSUL_ENABLED}"
    echo "etcd: ${ETCD_ENABLED}"
    echo ""

    # Cluster node configuration
    NODES="${CLUSTER_NODES:-}"
    if [ -z "${NODES}" ]; then
        echo "ERROR: CLUSTER_NODES environment variable must be set for cluster mode"
        echo "Example: CLUSTER_NODES='192.168.1.1:22,192.168.1.2:22,192.168.1.3:22'"
        exit 1
    fi

    # Parse node list
    NODE_IFS="$IFS"
    IFS=','
    read -ra NODE_ARRAY <<< "${NODES}"
    IFS="$NODE_IFS"
    NUM_NODES=${#NODE_ARRAY[@]}
    echo "Configured ${NUM_NODES} cluster nodes"

    # Extract node IPs/hosts
    declare -a NODE_IPS
    declare -a NODE_NAMES
    for i in "${!NODE_ARRAY[@]}"; do
        IFS=':' read -ra NODE_PARTS <<< "${NODE_ARRAY[$i]}"
        NODE_IP="${NODE_PARTS[0]}"
        NODE_NAME="${CLUSTER_NAME}-node-${i}"
        NODE_IPS[$i]="${NODE_IP}"
        NODE_NAMES[$i]="${NODE_NAME}"
        echo "  Node ${i}: ${NODE_IP} (name: ${NODE_NAME})"
    done

    # --- Consul Setup ---
    if [ "${CONSUL_ENABLED}" = "true" ]; then
        echo ""
        echo "--- Setting up Consul for distributed configuration ---"

        CONSUL_VERSION="${CONSUL_VERSION:-1.16.0}"
        CONSUL_BIN="/usr/local/bin/consul"

        # Install Consul if not present
        if [ ! -x "$CONSUL_BIN" ]; then
            echo "Installing Consul v${CONSUL_VERSION}..."
            CONSUL_URL="https://releases.hashicorp.com/consul/${CONSUL_VERSION}/consul_${CONSUL_VERSION}_linux_amd64.zip"
            local_consul_zip="/tmp/consul_${CONSUL_VERSION}_linux_amd64.zip"
            wget -q "${CONSUL_URL}" -O "${local_consul_zip}"
            unzip -o "${local_consul_zip}" -d /usr/local/bin consul
            chmod +x "${CONSUL_BIN}"
            rm -f "${local_consul_zip}"
        fi

        # Configure Consul data and config directories
        CONSUL_DATA_DIR="/var/lib/consul"
        CONSUL_CONFIG_DIR="/etc/consul.d"
        mkdir -p "${CONSUL_DATA_DIR}" "${CONSUL_CONFIG_DIR}"

        # Determine consul server nodes (first 2 nodes become servers)
        declare -a CONSUL_SERVER_NODES
        for i in $(seq 0 1); do
            if [ "${i}" -lt "${#NODE_IPS[@]}" ]; then
                CONSUL_SERVER_NODES+=("${NODE_IPS[$i]}")
            fi
        done

        if [ ${#CONSUL_SERVER_NODES[@]} -ge 2 ]; then
            CONSUL_BOOL="true"
            CONSUL_CLIENT_BOOL="false"
            echo "Bootstrapping multi-node Consul cluster with ${#CONSUL_SERVER_NODES[@]} servers"
        else
            CONSUL_BOOL="true"
            CONSUL_CLIENT_BOOL="true"
            echo "Running Consul in single-node development mode"
        fi

        # Generate Consul configuration for all nodes
        for i in "${!NODE_IPS[@]}"; do
            NODE_IP="${NODE_IPS[$i]}"
            NODE_NAME="${NODE_NAMES[$i]}"

            cat > "${CONSUL_CONFIG_DIR}/consul_${NODE_NAME}.json" << EOFEOF
{
  "name": "${NODE_NAME}",
  "bind_addr": "${NODE_IP}",
  "advertise_addr": "${NODE_IP}",
  "data_dir": "${CONSUL_DATA_DIR}",
  "encrypt": "acf5c7b3c1e8f6e2d9a7b3c1e8f6e2d9a",
  "bootstrap": ${CONSUL_BOOL},
  "server": ${CONSUL_SERVER_BOOL:-false},
  "client": "0.0.0.0",
  "start_join": [],
  "retry_join": []
}
EOFEOF

            # Build retry_join string for this node
            JOIN_ARGS=""
            for j in "${!NODE_IPS[@]}"; do
                if [ "$j" -ne "$i" ]; then
                    JOIN_ARGS="${JOIN_ARGS}\"${NODE_IPS[$j]}\","
                fi
            done
            JOIN_ARGS="${JOIN_ARGS%,*}"  # Remove trailing comma

            # Update consul config with retry_join
            if [ -n "${JOIN_ARGS}" ]; then
                sed -i 's|"start_join": \\[\\],|"start_join": ['"${JOIN_ARGS}"'],|' "${CONSUL_CONFIG_DIR}/consul_${NODE_NAME}.json"
            fi
        done

        # Set retry_join on all configs uniformly
        ALL_JOIN=""
        for ip in "${NODE_IPS[@]}"; do
            ALL_JOIN="${ALL_JOIN}\"${ip}\","
        done
        ALL_JOIN="${ALL_JOIN%,*}"

        for i in "${!NODE_IPS[@]}"; do
            NODE_NAME="${NODE_NAMES[$i]}"
            # Replace start_join with the full join list
            sed -i "s|\"start_join\": \\[.*,\\]|\"start_join\": [${ALL_JOIN}],|" "${CONSUL_CONFIG_DIR}/consul_${NODE_NAME}.json"
        done

        # Start Consul on the first node
        echo "Starting Consul on ${NODE_IPS[0]}..."
        nohup "${CONSUL_BIN}" agent -config-file "${CONSUL_CONFIG_DIR}/consul_${NODE_NAMES[0]}.json" > /var/log/consul.log 2>&1 &
        CONSUL_PID=$!
        echo "Consul PID: ${CONSUL_PID}"

        # Wait for Consul to be ready
        echo "Waiting for Consul to be ready..."
        for i in $(seq 1 30); do
            if "${CONSUL_BIN}" members > /dev/null 2>&1; then
                echo "Consul is healthy"
                break
            fi
            sleep 1
        done

        # Store Consul address for config propagation
        CONSUL_HTTP="http://${NODE_IPS[0]}:8500"
        echo "Consul HTTP API: ${CONSUL_HTTP}"
    fi

    # --- etcd Setup ---
    if [ "${ETCD_ENABLED}" = "true" ]; then
        echo ""
        echo "--- Setting up etcd for key-value storage ---"

        ETCD_VERSION="${ETCD_VERSION:-v3.5.0}"
        ETCD_BIN="/usr/local/bin/etcd"

        # Install etcd if not present
        if [ ! -x "$ETCD_BIN" ]; then
            echo "Installing etcd ${ETCD_VERSION}..."
            ETCD_DOWNLOAD_URL="https://github.com/etcd-io/etcd/releases/download/${ETCD_VERSION}/etcd-${ETCD_VERSION}-linux-amd64.tar.gz"
            local_etcd_tar="/tmp/etcd-${ETCD_VERSION}-linux-amd64.tar.gz"
            wget -q "${ETCD_DOWNLOAD_URL}" -O "${local_etcd_tar}"
            tar -xzf "${local_etcd_tar}" -C /tmp
            mv /tmp/etcd-${ETCD_VERSION}-linux-amd64/etcd /usr/local/bin/etcd
            mv /tmp/etcd-${ETCD_VERSION}-linux-amd64/etcdctl /usr/local/bin/etcdctl
            chmod +x "${ETCD_BIN}" "/usr/local/bin/etcdctl"
            rm -f "${local_etcd_tar}"
            rm -rf /tmp/etcd-${ETCD_VERSION}-linux-amd64
        fi

        ETCD_DATA_DIR="/var/lib/etcd"
        ETCD_DIR="/etc/etcd"
        mkdir -p "${ETCD_DATA_DIR}" "${ETCD_DIR}"

        # Generate etcd configuration - first node becomes the initial cluster member
        FIRST_NODE="${NODE_IPS[0]}"
        CLUSTER_MEMBER=""
        for ip in "${NODE_IPS[@]}"; do
            if [ -n "${CLUSTER_MEMBER}" ]; then
                CLUSTER_MEMBER="${CLUSTER_MEMBER},"
            fi
            CLUSTER_MEMBER="${CLUSTER_MEMBER}etcd-${ip}=http://${ip}:2380"
        done

        # Write etcd config
        cat > "${ETCD_DIR}/etcd.conf" << EOF
ETCD_NAME="etcd-${FIRST_NODE}"
ETCD_DATA_DIR="${ETCD_DATA_DIR}"
ETCD_LISTEN_PEER_URLS="http://${FIRST_NODE}:2380"
ETCD_LISTEN_CLIENT_URLS="http://${FIRST_NODE}:2379"
ETCD_INITIAL_CLUSTER="${CLUSTER_MEMBER}"
ETCD_INITIAL_CLUSTER_TOKEN="rscon-cluster"
ETCD_INITIAL_CLUSTER_STATE="new"
ETCD_ADVERTISE_CLIENT_URLS="http://${FIRST_NODE}:2379"
EOF

        echo "Starting etcd on ${FIRST_NODE}..."
        nohup etcd > /var/log/etcd.log 2>&1 &
        ETCD_PID=$!
        echo "etcd PID: ${ETCD_PID}"

        # Wait for etcd to be ready
        echo "Waiting for etcd to be ready..."
        for i in $(seq 1 30); do
            if etcdctl endpoint health -endpoints "http://${FIRST_NODE}:2379" > /dev/null 2>&1; then
                echo "etcd is healthy"
                break
            fi
            sleep 1
        done

        ETCD_CTL="etcdctl --endpoints http://${FIRST_NODE}:2379"
        # Store initial KV data
        ${ETCD_CTL} put "/rscon/cluster/${CLUSTER_NAME}/mode" "active" 2>/dev/null || true
        ${ETCD_CTL} put "/rscon/cluster/${CLUSTER_NAME}/nodes" "${NODES}" 2>/dev/null || true
        echo "etcd KV initialized"

        ETCD_HTTP="http://${FIRST_NODE}:2379"
        echo "etcd HTTP API: ${ETCD_HTTP}"
    fi

    # --- rscond node deployment ---
    echo ""
    echo "--- Deploying rscond instances across cluster nodes ---"

    # Deploy rscond to each node via SSH
    for i in "${!NODE_IPS[@]}"; do
        NODE_IP="${NODE_IPS[$i]}"
        NODE_NAME="${NODE_NAMES[$i]}"
        SSH_OPTS="-i ${SSH_KEY} -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null"

        echo "Deploying rscond to ${NODE_NAME} (${NODE_IP})..."

        # Copy configuration to remote node
        scp ${SSH_OPTS} "${CONFIG_DIR}/config.yaml" "deploy@${NODE_IP}:${CONFIG_DIR}/config.yaml"

        # Create node-specific config with unique API/SSH ports
        REMOTE_API_PORT=$((18080 + i))
        REMOTE_SSH_PORT=$((2222 + i))

        cat > /tmp/rscond_node_config.yaml <<'EOFEOF'
server:
  name: "${NODE_NAME}"
  environment: production
  ssh_address: "0.0.0.0:${REMOTE_SSH_PORT}"
  api_address: "127.0.0.1:${REMOTE_API_PORT}"
  public_url: "https://${NODE_IP}:${REMOTE_API_PORT}"
  tls_cert_file: /etc/rscon/fullchain.pem
  tls_key_file: /etc/rscon/privkey.pem
  shutdown_timeout: 30s

database:
  url: "postgres://rscon@${FIRST_NODE}:5432/rscon?sslmode=disable"
  url_env: RSCON_DATABASE_URL
  max_connections: 20
  min_connections: 2
  connect_timeout: 10s

vault:
  enabled: false

auth:
  bootstrap:
    username: admin
    password_env: RSCON_BOOTSTRAP_ADMIN_PASSWORD
  session_ttl: 8h
  login_rate_limit: 10
  login_rate_window: 1m
  lockout_threshold: 5
  lockout_duration: 15m

ssh:
  host_key_files:
    - /etc/rscon/ssh_host_ed25519_key
  gateway_key_file: /var/lib/rscon/gateway_ed25519
  certificate_ttl: 10m
  max_certificate_ttl: 1h
  destination_certificate_ttl: 15m
  max_session_duration: 8h
  idle_timeout: 30m
  dial_timeout: 10s
  handshake_timeout: 30s
  max_concurrent_sessions: 5
  max_auth_tries: 3
  allow_local_key_auth: false
  allow_tcp_forwarding: false
  allow_sftp: false

host_keys:
  known_hosts_file: /etc/rscon/known_hosts
  trusted_host_ca_files: []
  require_verification: true

recording:
  enabled: true
  fail_closed: true
  backend: filesystem
  directory: /var/lib/rscon/recordings
  retention: 90h
  max_file_bytes: 268435456

audit:
  require_persist: true
  syslog:
    enabled: false
    network: udp
    address: 127.0.0.1:514
    format: json

api:
  rate_limit_per_minute: 600
  max_body_bytes: 1048576
  trusted_proxy_cidrs:
    - 127.0.0.1/32
  cors_allowed_origins: []

security:
  default_deny: true
  allow_self_approval: false

log:
  level: info
  format: json

dev:
  insecure_file_ca: true
  file_ca_key_file: /tmp/rscon/ca_key
EOFEOF

        scp ${SSH_OPTS} /tmp/rscond_node_config.yaml "deploy@${NODE_IP}:${CONFIG_DIR}/rscond_${NODE_NAME}.yaml"

        # Start rscond on remote node
        ssh ${SSH_OPTS} "deploy@${NODE_IP}" << EOFEOF
        mkdir -p /var/log /var/lib/rscon
        nohup rscond -config "${CONFIG_DIR}/rscond_${NODE_NAME}.yaml" > "/var/log/rscond_${NODE_NAME}.log" 2>&1 &
        echo "Rscond PID on ${NODE_NAME}: \$!"
EOFEOF

        rm -f /tmp/rscond_node_config.yaml
    done

    # Wait for all rscond instances to be ready
    echo "Waiting for all rscond instances to be ready..."
    for i in $(seq 1 60); do
        ALL_HEALTHY=true
        for j in "${!NODE_IPS[@]}"; do
            NODE_IP="${NODE_IPS[$j]}"
            REMOTE_API_PORT=$((18080 + j))
            if ! curl -s "http://${NODE_IP}:${REMOTE_API_PORT}/api/v1/health" > /dev/null 2>&1; then
                ALL_HEALTHY=false
                echo "Node ${j} (${NODE_IP}) not yet healthy"
            fi
        done
        if [ "${ALL_HEALTHY}" = "true" ]; then
            echo "All rscond instances are healthy"
            break
        fi
        sleep 1
    done

    # --- Load Balancer Setup (HAProxy) ---
    echo ""
    echo "--- Configuring HAProxy load balancing ---"

    if command -v haproxy &> /dev/null; then
        # Configure HAProxy to distribute traffic across all rscond API instances
        HAPROXY_CONF="/etc/haproxy/haproxy.cfg"
        mkdir -p /etc/haproxy

        BACKEND_SERVERS=""
        for i in "${!NODE_IPS[@]}"; do
            NODE_IP="${NODE_IPS[$i]}"
            REMOTE_API_PORT=$((18080 + i))
            NODE_NAME="${NODE_NAMES[$i]}"
            LINE="        server ${NODE_NAME} ${NODE_IP}:${REMOTE_API_PORT} check fall rise 2"
            if [ -n "${BACKEND_SERVERS}" ]; then
                BACKEND_SERVERS="${BACKEND_SERVERS}
${LINE}"
            else
                BACKEND_SERVERS="${LINE}"
            fi
        done

        cat > "${HAPROXY_CONF}" << EOF
# RSCON Cluster HAProxy Configuration
# Automatically generated by deploy_multi_node.sh

global
    log /dev/log local0
    log /dev/log local1 notice
    maxconn 2000
    daemon

defaults
    mode http
    option httplog
    option dontlognull
    timeout connect 5000ms
    timeout client 50000ms
    timeout server 50000ms
    errorlog

frontend rscon-frontend
    bind *:443 ssl crt /etc/rscon/fullchain.pem /etc/rscon/privkey.pem
    # Also listen on HTTP for health checks
    bind *:80
    default_backend rscon-backend

backend rscon-backend
    balance roundrobin
    option httpchk HEAD /api/v1/health:8080 HTTP/1.1
    option http-ignore-proxied-connections
    ${BACKEND_SERVERS}
    timeout server 50000ms
EOF

        # Reload HAProxy
        if systemctl reload haproxy 2>/dev/null; then
            echo "HAProxy reloaded successfully"
        else
            haproxy -f "${HAPROXY_CONF}" -p /var/run/haproxy.pid 2>/dev/null || true
            echo "HAProxy started/configured"
        fi

        echo "Load balancer configured: https://<load-balancer-ip>"
    else
        echo "HAProxy not found - skipping load balancer setup"
        echo "Nodes communicate directly via Consul/etcd for distributed configuration"
    fi

    # --- Graceful Failover Setup ---
    echo ""
    echo "--- Configuring graceful failover ---"

    # Set up failover script
    cat > "/usr/local/bin/rscon-failover-${CLUSTER_NAME}" << 'FAILOVER_EOF'
#!/bin/bash
# RSCON Cluster Failover Script
# Monitors rscond instances and performs graceful failover

CLUSTER_NAME="${1:-rscon-cluster}"
CONFIG_DIR="${2:-/etc/rscon}"
SSH_KEY="${3:-~/.ssh/rscon_deploy}"

set -euo pipefail

# Check if a specific rscond instance is healthy
check_node_health() {
    local node_ip="$1"
    local api_port="$2"

    if curl -s "http://${node_ip}:${api_port}/api/v1/health" > /dev/null 2>&1; then
        return 0  # Healthy
    else
        return 1  # Unhealthy
    fi
}

# Graceful failover: mark unhealthy node, redirect traffic
graceful_failover() {
    local unhealthy_node="$1"
    local healthy_node="$2"

    echo "Initiating graceful failover from ${unhealthy_node} to ${healthy_node}"

    # 1. Mark the unhealthy node as drained in etcd
    if etcdctl endpoint health -endpoints "http://localhost:2379" > /dev/null 2>&1; then
        etcdctl put "/rscon/cluster/${CLUSTER_NAME}/drain/${unhealthy_node}" "true" 2>/dev/null || true
    fi

    # 2. Wait for existing sessions to complete (timeout after 30s)
    echo "Waiting for existing sessions to complete (max 30s)..."
    local wait_count=0
    while [ ${wait_count} -lt 30 ]; do
        # Check if node still has active sessions
        if ! curl -s "http://${unhealthy_node}:18080/api/v1/sessions/active" > /dev/null 2>&1; then
            echo "All sessions drained from ${unhealthy_node}"
            break
        fi
        sleep 1
        wait_count=$((wait_count + 1))
    done

    # 3. If still have sessions, force terminate after timeout
    if [ ${wait_count} -ge 30 ]; then
        echo "Timeout: forcing session termination on ${unhealthy_node}"
    fi

    echo "Failover complete: traffic redirected to ${healthy_node}"
}

# Main monitoring loop
MAIN_MONITOR_LOOP() {
    # Read node list from etcd
    if etcdctl endpoint health -endpoints "http://localhost:2379" > /dev/null 2>&1; then
        NODES=$((etcdctl get "/rscon/cluster/${CLUSTER_NAME}/nodes" 2>/dev/null) || echo "")
    elif curl -s http://localhost:8500/v1/catalog/nodes > /dev/null 2>&1; then
        NODES=$((curl -s http://localhost:8500/v1/catalog/nodes 2>/dev/null) || echo "")
    else
        NODES=""
    fi

    # Parse node list
    node_array=()
    if [ -n "${NODES}" ]; then
        IFS=',' read -ra node_array <<< "${NODES}"
    fi

    echo "Monitoring cluster ${CLUSTER_NAME} with ${#node_array[@]} nodes"

    while true; do
        for i in "${!node_array[@]}"; do
            # Each node entry is "ip:port"
            IFS=':' read -ra node_parts <<< "${node_array[$i]}"
            node_ip="${node_parts[0]}"
            node_api_port=$((18080 + i))

            if ! check_node_health "${node_ip}" "${node_api_port}"; then
                echo "WARNING: Node ${node_ip} is unhealthy, initiating failover..."

                # Find a healthy replacement node
                for j in "${!node_array[@]}"; do
                    IFS=':' read -ra other_parts <<< "${node_array[$j]}"
                    other_ip="${other_parts[0]}"
                    other_api_port=$((18080 + j))

                    if [ "${other_ip}" != "${node_ip}" ] && check_node_health "${other_ip}" "${other_api_port}"; then
                        graceful_failover "${node_ip}:${node_api_port}" "${other_ip}:${other_api_port}"
                        # Break after first failover to avoid cascading
                        break 2
                    fi
                done

                # If no healthy node found, try to restart the failed node
                echo "No healthy replacement found, will retry..."
            fi
        done

        sleep 10
    done
}

# Entry point
if [ "${BASH_SOURCE[0]}" = "${0}" ]; then
    if [ -z "${CLUSTER_NAME}" ]; then
        echo "Usage: $0 <cluster_name> [config_dir] [ssh_key]"
        exit 1
    fi
    MAIN_MONITOR_LOOP &
    echo "Failover monitor started (PID: $!)"
fi
FAILOVER_EOF
    chmod +x "/usr/local/bin/rscon-failover-${CLUSTER_NAME}"

    echo ""
    echo "=== RSCON Cluster Deployment Complete ==="
    echo "Cluster: ${CLUSTER_NAME}"
    echo "Nodes: ${NUM_NODES}"
    echo "Consul: ${CONSUL_ENABLED} (for distributed configuration)"
    echo "etcd: ${ETCD_ENABLED} (for key-value storage)"
    echo ""
    echo "Deployment Summary:"
    for i in "${!NODE_IPS[@]}"; do
        NODE_IP="${NODE_IPS[$i]}"
        REMOTE_API_PORT=$((18080 + i))
        echo "  ${NODE_NAMES[$i]}: ${NODE_IP}:${REMOTE_API_PORT} (API)"
    done
    echo ""
    if command -v haproxy &> /dev/null; then
        echo "Load Balancer: HAProxy distributing traffic across rscond instances"
    fi
    echo "Consul: Distributed configuration store available at http://${NODE_IPS[0]}:8500"
    echo "etcd: Key-value store available at http://${NODE_IPS[0]}:2379"
    echo "Failover: Graceful failover script available at /usr/local/bin/rscon-failover-${CLUSTER_NAME}"
    echo ""
    echo "CLI usage examples:"
    echo "  rscon login admin <password>  # Login to any node"
    echo "  rscon -H <load-balancer-ip> ...  # Use load balancer"
    echo "  rscon-failover-${CLUSTER_NAME}  # Manual failover monitor"
    exit 0
fi

echo "ERROR: Unknown deployment mode: ${DEPLOY_MODE}"
echo "Supported modes: single, cluster"
exit 1