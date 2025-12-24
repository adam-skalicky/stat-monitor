#!/bin/bash
set -e

# --- Configuration (Dynamically Set during Build) ---
BASE_URL="{{BASE_URL}}" 

INSTALL_DIR="/opt/stat-monitor"
SERVICE_NAME="stat-monitor"
SERVICE_FILE="/etc/systemd/system/${SERVICE_NAME}.service"

# --- Helper Functions ---
error_exit() {
    echo "Error: $1"
    exit 1
}

# --- 1. Detect Architecture ---
ARCH=$(uname -m)
case $ARCH in
    x86_64) GOARCH="amd64" ;;
    aarch64) GOARCH="arm64" ;;
    *) error_exit "Unsupported architecture: $ARCH" ;;
esac

OS=$(uname -s | tr '[:upper:]' '[:lower:]')
if [ "$OS" != "linux" ]; then
    error_exit "This script only supports Linux."
fi

BIN_NAME="stat-monitor-${OS}-${GOARCH}"

echo "Detected System: $OS/$GOARCH"
echo "Fetching artifacts from: $BASE_URL"

# --- 2. Stop Existing Service (Fix for 'Text file busy') ---
if systemctl is-active --quiet "$SERVICE_NAME"; then
    echo "Service is running. Stopping it to allow update..."
    systemctl stop "$SERVICE_NAME"
fi

# --- 3. Prepare Install Directory ---
echo "Preparing $INSTALL_DIR..."
mkdir -p "$INSTALL_DIR"

# --- 4. Download Artifacts ---
echo "Downloading binary..."
# We use -o to overwrite. Since service is stopped, the file lock is released.
curl -f -L "$BASE_URL/$BIN_NAME" -o "$INSTALL_DIR/stat-monitor" || error_exit "Failed to download binary"
chmod +x "$INSTALL_DIR/stat-monitor"

echo "Downloading service definition..."
curl -f -L "$BASE_URL/stat-monitor.service.sample" -o "$INSTALL_DIR/stat-monitor.service.temp" || error_exit "Failed to download service sample"

if [ ! -f "$INSTALL_DIR/config.yaml" ]; then
    echo "Downloading default config..."
    curl -f -L "$BASE_URL/config.yaml.sample" -o "$INSTALL_DIR/config.yaml"
else
    echo "Existing config found. Skipping config download."
fi

# --- 5. Install Service ---
echo "Installing systemd service..."
mv "$INSTALL_DIR/stat-monitor.service.temp" "$SERVICE_FILE"

echo "Reloading systemd..."
systemctl daemon-reload
systemctl enable "$SERVICE_NAME"
systemctl restart "$SERVICE_NAME"

echo "------------------------------------------------"
echo "Installation Complete!"
echo "Status: systemctl status $SERVICE_NAME"
echo "------------------------------------------------"