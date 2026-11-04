#!/usr/bin/env bash
# RSCON daemon uninstaller script
# Uninstalls rscond binary and systemd unit

set -euo pipefail

echo "=== RSCON rscond Uninstaller ==="
echo ""

# Check if running as root
if [ "$(id -u)" -ne 0 ]; then
    echo "ERROR: This uninstaller must be run as root"
    exit 1
fi

# Stop and disable rscond
echo "Stopping and disabling rscond..."
systemctl stop rscond || true
systemctl disable rscond || true

# Remove systemd unit
echo "Removing systemd unit..."
rm -f /etc/systemd/system/rscond.service
systemctl daemon-reload

# Remove binary
echo "Removing binary..."
rm -f /usr/local/bin/rscond

# Optionally remove data directories
read -p "Remove data directories? (recordings, config) [y/N]: " remove_data
if [ "${remove_data}" = "y" ] || [ "${remove_data}" = "Y" ]; then
    rm -rf /var/lib/rscon
    rm -rf /etc/rscon
    echo "Data directories removed."
fi

# Reload systemd
echo "Reloading systemd..."
systemctl daemon-reload

echo ""
echo "=== Uninstallation Complete ==="
echo "RSCON rscond has been uninstalled."
