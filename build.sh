#!/bin/bash
set -e

# --- Configuration ---
APP_NAME="stat-monitor"
VERSION=$1
DOMAIN="http://stat-monitor.wal-sys.com"

if [ -z "$VERSION" ]; then
    echo "Usage: ./build.sh <version>"
    exit 1
fi

# --- Safety Checks ---
if [ ! -f "install.sh" ]; then
    echo "ERROR: 'install.sh' not found. Please create it or rename 'instal.sh'."
    exit 1
fi
if [ ! -f "config.yaml.sample" ]; then
    echo "ERROR: 'config.yaml.sample' not found."
    exit 1
fi
if [ ! -f "stat-monitor.service.sample" ]; then
    echo "ERROR: 'stat-monitor.service.sample' not found."
    exit 1
fi

OUTPUT_DIR="output"
LATEST_DIR="$OUTPUT_DIR/latest"
VERSION_DIR="$OUTPUT_DIR/$VERSION"

# --- 0. Prepare Go Dependencies ---
echo "Updating Go dependencies..."
go get github.com/shirou/gopsutil/v3/disk@v3.24.5
go get github.com/shirou/gopsutil/v3/mem@v3.24.5
go get github.com/shirou/gopsutil/v3/cpu@v3.24.5
go mod tidy

# --- 1. Clean and Prepare Dirs ---
rm -rf "$LATEST_DIR" "$VERSION_DIR"
mkdir -p "$LATEST_DIR"
mkdir -p "$VERSION_DIR"

# --- 2. Build Binaries ---
PLATFORMS=("linux/amd64" "linux/arm64")

for PLATFORM in "${PLATFORMS[@]}"; do
    GOOS=${PLATFORM%/*}
    GOARCH=${PLATFORM#*/}
    BIN_NAME="${APP_NAME}-${GOOS}-${GOARCH}"

    echo "Building for $GOOS/$GOARCH..."
    env GOOS=$GOOS GOARCH=$GOARCH go build -o "$LATEST_DIR/$BIN_NAME" main.go
done

# --- 3. Copy Static Assets to Latest ---
echo "Copying sample files to Latest..."
cp config.yaml.sample "$LATEST_DIR/"
cp stat-monitor.service.sample "$LATEST_DIR/"

# --- 4. Generate 'Latest' Install Script ---
echo "Generating install.sh for Latest..."
# We use sed to replace the placeholder
sed "s|{{BASE_URL}}|$DOMAIN/latest|g" install.sh > "$LATEST_DIR/install.sh"

# --- 5. Create Versioned Release ---
echo "Creating versioned release ($VERSION)..."
# Copy ALL files from latest to version dir
cp "$LATEST_DIR/"* "$VERSION_DIR/"

# --- 6. Generate 'Versioned' Install Script ---
echo "Generating install.sh for version $VERSION..."
# Overwrite the install.sh in the version folder to point to the specific version URL
sed "s|{{BASE_URL}}|$DOMAIN/$VERSION|g" install.sh > "$VERSION_DIR/install.sh"

echo "Build complete!"
echo "------------------------------------------------"
echo "Artifacts ready:"
echo "  Latest:  $LATEST_DIR"
echo "  Version: $VERSION_DIR"
echo "------------------------------------------------"