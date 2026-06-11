#!/bin/bash
# ==========================================
# AI Digital Human - Build Pipeline
# Ensures: version sync, file consistency, cache busting
# Usage: ./scripts/build.sh [version]
# ==========================================
set -e

PROJECT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
FRONTEND_DIR="$PROJECT_DIR/frontend"
BUILD_ID=$(date +%Y%m%d%H%M%S)

# === 1. Version Management ===
VERSION="${1:-$(grep -oP 'v\d+\.\d+\.\d+' "$FRONTEND_DIR/talking-head.html" | head -1 | sed 's/v//')}"
if [ -z "$VERSION" ]; then
    VERSION="0.0.1"
fi

echo "=== Build: v${VERSION} (id: ${BUILD_ID}) ==="

# === 2. Single Source of Truth: sync index.html → talking-head.html ===
echo "[1/4] Enforcing single source of truth..."
if [ -L "$FRONTEND_DIR/index.html" ]; then
    echo "  index.html is a symlink → talking-head.html ✓"
else
    echo "  Removing stale index.html copy..."
    rm -f "$FRONTEND_DIR/index.html"
    echo "  Creating symlink: index.html → talking-head.html"
    cd "$FRONTEND_DIR" && ln -s talking-head.html index.html
fi

# === 3. Inject Build ID & Version into HTML ===
echo "[2/4] Injecting build metadata..."
SRC="$FRONTEND_DIR/talking-head.html"

# Replace version string (handles v0.4.82, v0.5.0, etc.)
sed -i "s/v[0-9]\+\.[0-9]\+\.[0-9]\+/v${VERSION}/g" "$SRC"

# Inject BUILD_ID as cache buster for <meta> tag
# If BUILD_META comment exists, replace it; otherwise add after <title>
if grep -q 'BUILD_ID' "$SRC"; then
    sed -i "s/<meta name=\"build-id\" content=\"[^\"]*\"/<meta name=\"build-id\" content=\"${BUILD_ID}\"/" "$SRC"
else
    sed -i "s|<title>|<meta name=\"build-id\" content=\"${BUILD_ID}\">\n    <title>|" "$SRC"
fi

# Inject cache buster for dynamic imports (the ?v=xxx in JS imports)
sed -i "s/const cacheBuster = '?v=[^']*'/const cacheBuster = '?v=${VERSION}-${BUILD_ID}'/" "$SRC"

echo "  Version: v${VERSION}"
echo "  Build ID: ${BUILD_ID}"
echo "  Cache buster: ?v=${VERSION}-${BUILD_ID}"

# === 4. Build Go Backend ===
echo "[3/4] Compiling backend..."
cd "$PROJECT_DIR/backend"
go build -o "$PROJECT_DIR/backend_bin" main.go
echo "  backend_bin compiled ($(du -h "$PROJECT_DIR/backend_bin" | cut -f1))"

# === 5. Generate BUILD_INFO ===
echo "[4/4] Writing BUILD_INFO..."
cat > "$PROJECT_DIR/BUILD_INFO" <<EOF
VERSION=v${VERSION}
BUILD_ID=${BUILD_ID}
BUILT_AT=$(date -Iseconds)
GIT_HASH=$(cd "$PROJECT_DIR" && git rev-parse --short HEAD 2>/dev/null || echo "unknown")
FRONTEND_SHA256=$(sha256sum "$SRC" | cut -d' ' -f1)
BACKEND_SHA256=$(sha256sum "$PROJECT_DIR/backend_bin" | cut -d' ' -f1)
EOF

echo ""
echo "=== Build Complete ==="
cat "$PROJECT_DIR/BUILD_INFO"
echo ""
echo "Next step: ./scripts/deploy.sh"
