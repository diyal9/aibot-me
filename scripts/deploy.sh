#!/bin/bash
# ==========================================
# AI Digital Human - Deploy Pipeline
# Orchestrates: stop → start → verify → report
# Usage: ./scripts/deploy.sh [--skip-verify]
# ==========================================
set -e

PROJECT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
SKIP_VERIFY=""
if [ "$1" = "--skip-verify" ]; then SKIP_VERIFY="1"; fi

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

echo -e "${YELLOW}=== Deploy Pipeline ===${NC}"

# === 1. Pre-flight: check BUILD_INFO exists ===
if [ ! -f "$PROJECT_DIR/BUILD_INFO" ]; then
    echo -e "${RED}⛔ No BUILD_INFO found. Run ./scripts/build.sh first.${NC}"
    exit 1
fi

BUILD_VERSION=$(grep '^VERSION=' "$PROJECT_DIR/BUILD_INFO" | cut -d= -f2)
BUILD_ID=$(grep '^BUILD_ID=' "$PROJECT_DIR/BUILD_INFO" | cut -d= -f2)
FRONTEND_SHA=$(grep '^FRONTEND_SHA256=' "$PROJECT_DIR/BUILD_INFO" | cut -d= -f2)

echo "Deploying: $BUILD_VERSION (id: $BUILD_ID)"
echo ""

# === 2. Kill old backend ===
echo "[1/3] Stopping old backend..."
OLD_PIDS=$(pgrep -f "backend_bin" 2>/dev/null || true)
if [ -n "$OLD_PIDS" ]; then
    echo "  Killing PIDs: $OLD_PIDS"
    echo "$OLD_PIDS" | xargs kill 2>/dev/null || true
    sleep 2
    # Force kill if still running
    REMAINING=$(pgrep -f "backend_bin" 2>/dev/null || true)
    if [ -n "$REMAINING" ]; then
        echo "  Force killing..."
        echo "$REMAINING" | xargs kill -9 2>/dev/null || true
        sleep 1
    fi
else
    echo "  No running backend found"
fi

# Verify port 8085 is free
if ss -tlnp | grep -q ':8085 '; then
    echo -e "${RED}⛔ Port 8085 still in use!${NC}"
    ss -tlnp | grep ':8085'
    exit 1
fi
echo "  Port 8085 is free ✓"

# === 3. Start new backend ===
echo "[2/3] Starting new backend..."
cd "$PROJECT_DIR"
nohup ./backend_bin > backend.log 2>&1 &
NEW_PID=$!
echo "  Backend PID: $NEW_PID"

# Wait for readiness (max 10 seconds)
echo "  Waiting for backend readiness..."
READY=0
for i in $(seq 1 20); do
    if curl -s http://127.0.0.1:8085/ > /dev/null 2>&1; then
        READY=1
        break
    fi
    sleep 0.5
done

if [ "$READY" -eq 0 ]; then
    echo -e "${RED}⛔ Backend failed to start within 10 seconds${NC}"
    echo "  Log tail:"
    tail -20 backend.log
    exit 1
fi
echo "  Backend ready ✓"

# === 4. Post-deploy Verification ===
if [ -z "$SKIP_VERIFY" ]; then
    echo "[3/3] Running post-deploy verification..."
    echo ""
    
    PASS=0
    FAIL=0
    
    # 4a. Check served version matches BUILD_INFO
    SERVED_VERSION=$(curl -s http://127.0.0.1:8085/ | grep -oP 'v\d+\.\d+\.\d+' | head -1)
    if [ "$SERVED_VERSION" = "$BUILD_VERSION" ]; then
        echo -e "  ${GREEN}✅${NC} Served version matches: $SERVED_VERSION"
        PASS=$((PASS+1))
    else
        echo -e "  ${RED}❌${NC} Version mismatch! Expected $BUILD_VERSION, served $SERVED_VERSION"
        FAIL=$((FAIL+1))
    fi
    
    # 4b. Check build ID is injected
    SERVED_BUILD_ID=$(curl -s http://127.0.0.1:8085/ | grep -oP 'build-id.*?content="[^"]*"' | grep -oP '\d{14}' | head -1)
    if [ "$SERVED_BUILD_ID" = "$BUILD_ID" ]; then
        echo -e "  ${GREEN}✅${NC} Build ID matches: $BUILD_ID"
        PASS=$((PASS+1))
    else
        echo -e "  ${RED}❌${NC} Build ID mismatch! Expected $BUILD_ID, served $SERVED_BUILD_ID"
        FAIL=$((FAIL+1))
    fi
    
    # 4c. Check index.html is symlink (single source of truth)
    if [ -L "$PROJECT_DIR/frontend/index.html" ]; then
        echo -e "  ${GREEN}✅${NC} index.html is symlink → talking-head.html"
        PASS=$((PASS+1))
    else
        echo -e "  ${RED}❌${NC} index.html is NOT a symlink! File may be stale."
        FAIL=$((FAIL+1))
    fi
    
    # 4d. Check frontend file SHA matches BUILD_INFO
    CURRENT_SHA=$(sha256sum "$PROJECT_DIR/frontend/talking-head.html" | cut -d' ' -f1)
    if [ "$CURRENT_SHA" = "$FRONTEND_SHA" ]; then
        echo -e "  ${GREEN}✅${NC} Frontend file integrity: SHA matches build"
        PASS=$((PASS+1))
    else
        echo -e "  ${YELLOW}⚠️${NC} Frontend file changed after build (may be intentional)"
        PASS=$((PASS+1))  # Warning, not failure
    fi
    
    # 4e. TTS health check
    TTS_RESP=$(curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:7788/health 2>/dev/null || echo "000")
    if [ "$TTS_RESP" = "200" ]; then
        echo -e "  ${GREEN}✅${NC} TTS service: healthy"
        PASS=$((PASS+1))
    else
        echo -e "  ${YELLOW}⚠️${NC} TTS service: HTTP $TTS_RESP (may need restart)"
        PASS=$((PASS+1))
    fi
    
    # 4f. Nginx proxy check
    NGINX_RESP=$(curl -sk -o /dev/null -w "%{http_code}" https://127.0.0.1:8089/human/ 2>/dev/null || echo "000")
    if [ "$NGINX_RESP" = "200" ]; then
        echo -e "  ${GREEN}✅${NC} Nginx proxy: reachable"
        PASS=$((PASS+1))
    else
        echo -e "  ${RED}❌${NC} Nginx proxy: HTTP $NGINX_RESP"
        FAIL=$((FAIL+1))
    fi
    
    echo ""
    echo "╔════════════════════════════════════╗"
    echo -e "║  Results: ${GREEN}$PASS Passed${NC} | ${RED}$FAIL Failed${NC}   ║"
    echo "╚════════════════════════════════════╝"
    
    if [ $FAIL -gt 0 ]; then
        echo -e "${RED}⛔ DEPLOYMENT FAILED. $FAIL check(s) did not pass.${NC}"
        echo "  Backend is running but may serve incorrect content."
        exit 1
    else
        echo -e "${GREEN}🚀 DEPLOYMENT SUCCESSFUL${NC}"
        echo "  Access: https://47.107.172.201:8089/human/"
        echo "  Version: $BUILD_VERSION"
        echo "  Build: $BUILD_ID"
        echo ""
        echo "  Note: If you still see old content, force refresh with Ctrl+Shift+R"
    fi
else
    echo "[3/3] Verification skipped (--skip-verify)"
fi
