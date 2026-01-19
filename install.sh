#!/bin/sh
set -e

# Tock Installer Script
#
# Usage:
#   curl -sS https://raw.githubusercontent.com/kriuchkov/tock/master/install.sh | sh
#
# or with sudo if required:
#   curl -sS https://raw.githubusercontent.com/kriuchkov/tock/master/install.sh | sudo sh

OWNER="kriuchkov"
REPO="tock"
BINARY="tock"
FORMAT="tar.gz"
BINDIR="/usr/local/bin"

# Detect OS
OS=$(uname -s)
case "$OS" in
    Linux)
        OS_TYPE="Linux"
        ;;
    Darwin)
        OS_TYPE="Darwin"
        ;;
    *)
        echo "Unsupported OS: $OS"
        exit 1
        ;;
esac

# Detect Architecture
ARCH=$(uname -m)
case "$ARCH" in
    x86_64)
        ARCH_TYPE="x86_64"
        ;;
    aarch64|arm64)
        ARCH_TYPE="arm64"
        ;;
    *)
        echo "Unsupported architecture: $ARCH"
        exit 1
        ;;
esac

# Get latest release tag if not specified
if [ -z "$VERSION" ]; then
    LATEST_RELEASE_URL="https://api.github.com/repos/$OWNER/$REPO/releases/latest"
    VERSION=$(curl -s $LATEST_RELEASE_URL | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')
fi

if [ -z "$VERSION" ]; then
    echo "Could not fetch latest version."
    exit 1
fi

# Ensure version doesn't have 'v' prefix for asset name construction if needed, 
# but usually tag has 'v'. Goreleaser config uses `{{ .ProjectName }}_{{ title .Os }}_{{ ... }}`.
# The artifacts usually don't have the 'v' in the filename unless specified.
# Based on .goreleaser.yaml: {{ .ProjectName }}_{{ title .Os }}_{{ Arch }}...
# Example: tock_Linux_x86_64.tar.gz

ASSET_NAME="${BINARY}_${OS_TYPE}_${ARCH_TYPE}.${FORMAT}"
DOWNLOAD_URL="https://github.com/$OWNER/$REPO/releases/download/$VERSION/$ASSET_NAME"

echo "Downloading $BINARY $VERSION for $OS_TYPE $ARCH_TYPE..."
echo "URL: $DOWNLOAD_URL"

TMP_DIR=$(mktemp -d)
curl -sL "$DOWNLOAD_URL" -o "$TMP_DIR/$ASSET_NAME"

echo "Installing to $BINDIR..."
tar -xzf "$TMP_DIR/$ASSET_NAME" -C "$TMP_DIR"
if [ -w "$BINDIR" ]; then
    mv "$TMP_DIR/$BINARY" "$BINDIR/$BINARY"
else
    sudo mv "$TMP_DIR/$BINARY" "$BINDIR/$BINARY"
fi

chmod +x "$BINDIR/$BINARY"
rm -rf "$TMP_DIR"

echo "$BINARY installed successfully to $BINDIR/$BINARY"
"$BINARY" --version
