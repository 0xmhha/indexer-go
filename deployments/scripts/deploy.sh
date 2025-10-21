#!/bin/bash
set -euo pipefail

# Indexer-Go Deployment Script
# Usage: ./deploy.sh [version]

VERSION="${1:-latest}"
INSTALL_DIR="/opt/indexer-go"
CONFIG_DIR="/etc/indexer-go"
DATA_DIR="/var/lib/indexer-go"
LOG_DIR="/var/log/indexer-go"
USER="indexer"
GROUP="indexer"

echo "=== Indexer-Go Deployment Script ==="
echo "Version: ${VERSION}"
echo ""

# Check if running as root
if [ "$EUID" -ne 0 ]; then
    echo "ERROR: This script must be run as root"
    exit 1
fi

# Create user and group
echo "[1/8] Creating user and group..."
if ! id -u ${USER} >/dev/null 2>&1; then
    useradd --system --user-group --shell /bin/false \
        --home-dir ${DATA_DIR} --create-home ${USER}
    echo "  ✓ User '${USER}' created"
else
    echo "  ✓ User '${USER}' already exists"
fi

# Create directories
echo "[2/8] Creating directories..."
mkdir -p ${INSTALL_DIR}/{bin,backup}
mkdir -p ${CONFIG_DIR}
mkdir -p ${DATA_DIR}
mkdir -p ${LOG_DIR}
echo "  ✓ Directories created"

# Set permissions
echo "[3/8] Setting permissions..."
chown -R ${USER}:${GROUP} ${DATA_DIR}
chown -R ${USER}:${GROUP} ${LOG_DIR}
chmod 750 ${DATA_DIR}
chmod 750 ${LOG_DIR}
echo "  ✓ Permissions set"

# Backup current binary (if exists)
echo "[4/8] Backing up current binary..."
if [ -f "${INSTALL_DIR}/bin/indexer-go" ]; then
    BACKUP_NAME="indexer-go.$(date +%Y%m%d_%H%M%S)"
    cp "${INSTALL_DIR}/bin/indexer-go" "${INSTALL_DIR}/backup/${BACKUP_NAME}"
    echo "  ✓ Backup created: ${BACKUP_NAME}"
else
    echo "  ℹ No existing binary to backup"
fi

# Download or copy new binary
echo "[5/8] Installing binary..."
if [ "${VERSION}" = "latest" ] || [ "${VERSION}" = "local" ]; then
    # Copy from build directory (for local development)
    if [ -f "../../build/indexer-go" ]; then
        cp "../../build/indexer-go" "${INSTALL_DIR}/bin/indexer-go"
        echo "  ✓ Binary copied from local build"
    else
        echo "  ERROR: Local binary not found at ../../build/indexer-go"
        echo "  Run 'make build' first"
        exit 1
    fi
else
    # Download from GitHub releases
    DOWNLOAD_URL="https://github.com/0xmhha/indexer-go/releases/download/${VERSION}/indexer-go-linux-amd64"
    if wget -q --spider "${DOWNLOAD_URL}"; then
        wget -O "${INSTALL_DIR}/bin/indexer-go" "${DOWNLOAD_URL}"
        echo "  ✓ Binary downloaded: ${VERSION}"
    else
        echo "  ERROR: Version ${VERSION} not found"
        exit 1
    fi
fi

# Set binary permissions
chmod 755 "${INSTALL_DIR}/bin/indexer-go"
chown root:root "${INSTALL_DIR}/bin/indexer-go"

# Install configuration (if not exists)
echo "[6/8] Installing configuration..."
if [ ! -f "${CONFIG_DIR}/config.yaml" ]; then
    if [ -f "../../config.example.yaml" ]; then
        cp "../../config.example.yaml" "${CONFIG_DIR}/config.yaml"
        echo "  ✓ Configuration template installed"
        echo "  ⚠ IMPORTANT: Edit ${CONFIG_DIR}/config.yaml before starting"
    fi
fi

if [ ! -f "${CONFIG_DIR}/indexer-go.env" ]; then
    if [ -f "../systemd/indexer-go.env.example" ]; then
        cp "../systemd/indexer-go.env.example" "${CONFIG_DIR}/indexer-go.env"
        echo "  ✓ Environment file template installed"
    fi
fi

# Install systemd service
echo "[7/8] Installing systemd service..."
if [ -f "../systemd/indexer-go.service" ]; then
    cp "../systemd/indexer-go.service" "/etc/systemd/system/"
    systemctl daemon-reload
    echo "  ✓ Systemd service installed"
else
    echo "  ⚠ Systemd service file not found"
fi

# Install logrotate configuration
echo "[8/8] Installing logrotate configuration..."
if [ -f "../logrotate/indexer-go" ]; then
    cp "../logrotate/indexer-go" "/etc/logrotate.d/"
    chmod 644 "/etc/logrotate.d/indexer-go"
    echo "  ✓ Logrotate configuration installed"
fi

echo ""
echo "=== Deployment Complete ==="
echo ""
echo "Next steps:"
echo "  1. Edit configuration:"
echo "     ${CONFIG_DIR}/config.yaml"
echo "     ${CONFIG_DIR}/indexer-go.env"
echo ""
echo "  2. Enable and start service:"
echo "     systemctl enable indexer-go"
echo "     systemctl start indexer-go"
echo ""
echo "  3. Check status:"
echo "     systemctl status indexer-go"
echo "     journalctl -u indexer-go -f"
echo ""
echo "  4. Verify health:"
echo "     curl http://localhost:8080/health"
echo ""
