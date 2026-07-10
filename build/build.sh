#!/bin/sh
set -e

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT_DIR="$REPO_ROOT/dist"
mkdir -p "$OUT_DIR"

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
    echo "Note: this xpagent targets modern Windows (7/8/10/11), not XP/Server 2003 — use build-agent.ps1 -LegacyGoRoot from a Windows host with go1.10.8 for that."
fi
