#!/bin/sh
# ports-cli installer — detects OS/arch, fetches the latest release binary,
# and installs it as `ports` (default: /usr/local/bin; override with BINDIR).
#
#   curl -fsSL https://raw.githubusercontent.com/dupe-com/ports-cli/main/install.sh | sh
set -eu

REPO="dupe-com/ports-cli"
BINDIR="${BINDIR:-/usr/local/bin}"

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
case "$os" in
  darwin|linux) ;;
  *) echo "unsupported OS: $os — grab a binary from https://github.com/$REPO/releases" >&2; exit 1 ;;
esac

arch="$(uname -m)"
case "$arch" in
  x86_64|amd64) arch=amd64 ;;
  arm64|aarch64) arch=arm64 ;;
  *) echo "unsupported arch: $arch" >&2; exit 1 ;;
esac

tag="$(curl -fsSL "https://api.github.com/repos/$REPO/releases/latest" | grep -o '"tag_name": *"[^"]*"' | head -1 | cut -d'"' -f4)"
[ -n "$tag" ] || { echo "couldn't determine latest release — is there one yet?" >&2; exit 1; }
version="${tag#v}"

url="https://github.com/$REPO/releases/download/$tag/ports-cli_${version}_${os}_${arch}.tar.gz"
echo "→ downloading $url"

tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
curl -fsSL "$url" | tar -xz -C "$tmp" ports

echo "→ installing to $BINDIR/ports"
if [ -w "$BINDIR" ]; then
  install -m 0755 "$tmp/ports" "$BINDIR/ports"
else
  echo "  ($BINDIR not writable — using sudo)"
  sudo install -m 0755 "$tmp/ports" "$BINDIR/ports"
fi

echo "✓ installed: $("$BINDIR/ports" --version)"
