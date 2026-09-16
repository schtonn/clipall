#!/usr/bin/env bash
set -euo pipefail

REPO="schtonn/clipall"
INSTALL_DIR="/usr/local/bin"
BINARY="clipall"

info()  { printf "\033[1;34m==>\033[0m %s\n" "$*"; }
error() { printf "\033[1;31merror:\033[0m %s\n" "$*" >&2; exit 1; }

# Detect OS and architecture.
OS="$(uname -s)"
ARCH="$(uname -m)"

case "$OS" in
    Darwin) GOOS="darwin" ;;
    *)      error "Unsupported OS: $OS" ;;
esac

case "$ARCH" in
    x86_64|amd64)  GOARCH="amd64" ;;
    arm64|aarch64) GOARCH="arm64" ;;
    *)             error "Unsupported architecture: $ARCH" ;;
esac

ASSET="${BINARY}-${GOOS}-${GOARCH}"
info "Detected platform: ${GOOS}/${GOARCH}"

# Fetch latest release tag from GitHub API.
info "Fetching latest release..."
TAG="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
    | grep '"tag_name"' | head -1 | cut -d '"' -f 4)"

if [ -z "$TAG" ]; then
    error "Could not determine latest release"
fi

# Check the exact path this script installs, rather than another binary on PATH.
if [ -x "${INSTALL_DIR}/${BINARY}" ]; then
    CURRENT="$("${INSTALL_DIR}/${BINARY}" --version 2>/dev/null || echo "unknown")"
    if [ "$CURRENT" = "clipall ${TAG}" ]; then
        info "Already up to date (${TAG})"
        exit 0
    fi
    info "Updating: ${CURRENT:-unknown} -> ${TAG}"
else
    info "Installing: ${TAG}"
fi

# Download binary.
URL="https://github.com/${REPO}/releases/download/${TAG}/${ASSET}"
CHECKSUM_URL="https://github.com/${REPO}/releases/download/${TAG}/checksums.txt"
TMPDIR="$(mktemp -d)"
trap 'rm -rf "$TMPDIR"' EXIT

info "Downloading ${URL}..."
if ! curl -fSL --progress-bar -o "${TMPDIR}/${BINARY}" "$URL"; then
    error "Download failed. Check https://github.com/${REPO}/releases for available assets."
fi

info "Verifying SHA-256 checksum..."
curl -fsSL -o "${TMPDIR}/checksums.txt" "$CHECKSUM_URL" || error "Could not download checksums.txt"
EXPECTED="$(awk -v asset="$ASSET" '$2 == asset { print $1 }' "${TMPDIR}/checksums.txt")"
[ -n "$EXPECTED" ] || error "No checksum found for ${ASSET}"
ACTUAL="$(shasum -a 256 "${TMPDIR}/${BINARY}" | awk '{ print $1 }')"
[ "$ACTUAL" = "$EXPECTED" ] || error "Checksum mismatch for ${ASSET}"

chmod +x "${TMPDIR}/${BINARY}"

# Remove macOS quarantine flag if present.
if [ "$GOOS" = "darwin" ]; then
    xattr -d com.apple.quarantine "${TMPDIR}/${BINARY}" 2>/dev/null || true
fi

# Install to INSTALL_DIR (may need sudo).
if [ -w "$INSTALL_DIR" ]; then
    mv "${TMPDIR}/${BINARY}" "${INSTALL_DIR}/${BINARY}"
else
    info "Installing to ${INSTALL_DIR} (requires sudo)..."
    sudo mv "${TMPDIR}/${BINARY}" "${INSTALL_DIR}/${BINARY}"
fi

info "Installed ${BINARY} ${TAG} to ${INSTALL_DIR}/${BINARY}"
printf "\n  Run: %s\n" "$BINARY"
printf "  First run discovers Tailscale peers and offers to enable autostart.\n"
printf "  Override discovery: %s --peers <hostname>:9876\n\n" "$BINARY"
