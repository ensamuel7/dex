#!/bin/sh
set -e

REPO="ensamuel7/dex"

# Detect OS
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$OS" in
    linux)  OS="linux" ;;
    darwin) OS="darwin" ;;
    *)
        echo "Error: unsupported OS: $OS"
        echo "dex supports Linux and macOS only."
        exit 1
        ;;
esac

# Detect architecture
ARCH=$(uname -m)
case "$ARCH" in
    x86_64|amd64)  ARCH="amd64" ;;
    aarch64|arm64) ARCH="arm64" ;;
    *)
        echo "Error: unsupported architecture: $ARCH"
        echo "dex supports amd64 and arm64 only."
        exit 1
        ;;
esac

TARBALL="dex-${OS}-${ARCH}.tar.gz"
URL="https://github.com/${REPO}/releases/latest/download/${TARBALL}"
INSTALL_DIR="/usr/local/bin"

echo "Downloading dex for ${OS}/${ARCH}..."
TMPDIR=$(mktemp -d)
trap 'rm -rf "$TMPDIR"' EXIT

if ! curl -fsSL "$URL" -o "${TMPDIR}/${TARBALL}"; then
    echo "Error: failed to download ${URL}"
    echo "Check that a release exists at https://github.com/${REPO}/releases"
    exit 1
fi

tar xzf "${TMPDIR}/${TARBALL}" -C "$TMPDIR"

echo "Installing dex to ${INSTALL_DIR}..."
if [ -w "$INSTALL_DIR" ]; then
    mv "${TMPDIR}/dex" "${INSTALL_DIR}/dex"
else
    sudo mv "${TMPDIR}/dex" "${INSTALL_DIR}/dex"
fi

chmod +x "${INSTALL_DIR}/dex"

echo ""
echo "dex installed successfully!"
dex version
