#!/bin/sh
set -e

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT_DIR="$REPO_ROOT/dist"
mkdir -p "$OUT_DIR"

# "xp" builds xpagent for Windows XP/Server 2003 (windows/386) with the legacy go1.10.8 toolchain,
# straight from Linux/macOS. Set XPSHELL_GO110_ROOT to an extracted go1.10.8 (GOROOT), e.g.
#   curl -sSLO https://go.dev/dl/go1.10.8.linux-amd64.tar.gz && tar xzf go1.10.8.linux-amd64.tar.gz
#   XPSHELL_GO110_ROOT=$PWD/go ./build/build.sh xp
if [ "$1" = "xp" ]; then
    : "${XPSHELL_GO110_ROOT:?set XPSHELL_GO110_ROOT to a go1.10.8 GOROOT}"
    GOPATH_TMP="$(mktemp -d)"
    mkdir -p "$GOPATH_TMP/src/github.com/BaseMax"
    ln -s "$REPO_ROOT" "$GOPATH_TMP/src/github.com/BaseMax/remote-shell-windows-xp"
    echo "Building xpagent for windows/386 (XP-compatible) with $XPSHELL_GO110_ROOT..."
    (cd "$GOPATH_TMP/src/github.com/BaseMax/remote-shell-windows-xp" && \
     GOROOT="$XPSHELL_GO110_ROOT" GOPATH="$GOPATH_TMP" GO111MODULE=off GOOS=windows GOARCH=386 CGO_ENABLED=0 \
     "$XPSHELL_GO110_ROOT/bin/go" build -o "$OUT_DIR/xpagent-xp.exe" ./cmd/xpagent)
    rm -rf "$GOPATH_TMP"
    echo "Built $OUT_DIR/xpagent-xp.exe"
    exit 0
fi

TARGET_OS="${1:-$(go env GOOS)}"
TARGET_ARCH="${2:-$(go env GOARCH)}"

EXT=""
if [ "$TARGET_OS" = "windows" ]; then
    EXT=".exe"
fi

cd "$REPO_ROOT"

echo "Building xpctl for $TARGET_OS/$TARGET_ARCH..."
GOOS="$TARGET_OS" GOARCH="$TARGET_ARCH" go build -o "$OUT_DIR/xpctl-$TARGET_OS-$TARGET_ARCH$EXT" ./cmd/xpctl

echo "Building xpagent for $TARGET_OS/$TARGET_ARCH..."
GOOS="$TARGET_OS" GOARCH="$TARGET_ARCH" go build -o "$OUT_DIR/xpagent-$TARGET_OS-$TARGET_ARCH$EXT" ./cmd/xpagent

echo "Built $OUT_DIR/xpctl-$TARGET_OS-$TARGET_ARCH$EXT and $OUT_DIR/xpagent-$TARGET_OS-$TARGET_ARCH$EXT"
if [ "$TARGET_OS" = "windows" ]; then
    echo "Note: this xpagent targets modern Windows (7/8/10/11), not XP/Server 2003; use build-agent.ps1 -LegacyGoRoot from a Windows host with go1.10.8 for that."
fi
