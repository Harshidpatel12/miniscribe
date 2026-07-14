#!/bin/bash
set -e

# Detect OS and Architecture
OS=$(uname -s | tr '[:upper:]' '[:lower:]')
ARCH=$(uname -m)

if [ "$ARCH" = "x86_64" ]; then
    ARCH="amd64"
elif [ "$ARCH" = "aarch64" ] || [ "$ARCH" = "arm64" ]; then
    ARCH="arm64"
fi

# Define the repository
REPO="Harshidpatel12/miniscribe"

# Get latest release from GitHub API
echo "Fetching latest version from GitHub..."
LATEST_RELEASE=$(curl -s "https://api.github.com/repos/${REPO}/releases/latest" | grep '"tag_name":' | sed -E 's/.*"([^"]+)".*/\1/')

if [ -z "$LATEST_RELEASE" ]; then
    # Fallback default if no releases are published yet
    VERSION="v0.1.0"
else
    VERSION="${LATEST_RELEASE}"
fi

URL="https://github.com/${REPO}/releases/download/${VERSION}/miniscribe-${OS}-${ARCH}.tar.gz"

# Detect if we should use sudo based on permissions
if [ "$EUID" -ne 0 ]; then
    # Local user install
    INSTALL_DIR="${HOME}/.local/share/miniscribe"
    BIN_DIR="${HOME}/.local/bin"
    CMD_PREFIX=""
else
    # Global root install
    INSTALL_DIR="/usr/local/miniscribe"
    BIN_DIR="/usr/local/bin"
    CMD_PREFIX="sudo"
fi

echo "Installing miniscribe ${VERSION} for ${OS}-${ARCH}..."
echo "Target directory: ${INSTALL_DIR}"

$CMD_PREFIX mkdir -p "${INSTALL_DIR}"
$CMD_PREFIX mkdir -p "${BIN_DIR}"

# Download and extract tarball directly to the installation directory
echo "Downloading from ${URL}..."
if curl -fsSL "${URL}" | $CMD_PREFIX tar -xz -C "${INSTALL_DIR}"; then
    # Create a symlink in the binary path pointing to the installation dir
    $CMD_PREFIX ln -sf "${INSTALL_DIR}/miniscribe" "${BIN_DIR}/miniscribe"
    
    echo "--------------------------------------------------------"
    echo "miniscribe installed successfully in ${BIN_DIR}/miniscribe!"
    echo "--------------------------------------------------------"
    
    # Check if target bin dir is in path
    if [[ ":$PATH:" != *":${BIN_DIR}:"* ]]; then
        echo "Warning: ${BIN_DIR} is not in your PATH."
        echo "Please add it to your ~/.bashrc, ~/.zshrc or shell profile:"
        echo "  export PATH=\"\$PATH:${BIN_DIR}\""
    fi
else
    echo "Error: Failed to download or extract the release archive."
    echo "Make sure a release exists for version ${VERSION} at ${URL}."
    exit 1
fi
