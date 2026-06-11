#!/bin/bash
# ==========================================
# AI Digital Human - Verification Gate
# Cross-checks: BUILD_INFO vs served content vs running binary
# Usage: ./scripts/verify.sh
# ==========================================
set -e

PROJECT_DIR="$(cd "$(dirname "$0")/.." && pwd)"
PASS=0
FAIL=0
WARN=0

BASE_URL="https://127.0.0.1:8089"
INTERNAL_BACKEND="http://127.0.0.1:8085"
INTERNAL_TTS="http://127.0.0.1:7788"

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

check_pass() { echo -e "  ${GREEN}✅${NC} $1"; PASS=$((PASS+1)); }
check_fail() { echo -e "  ${RED}❌${NC} $1"; FAIL=$((FAIL+1)); }
check_warn() { echo -e "  ${YELLOW}⚠️${NC} $1"; WARN=$((WARN+1)); }

echo -e "\n🔍 \033[1mRunning Verification Gate...\033[0m"

# ==========================================
# Phase 1: Build Integrity
# ==========================================
echo -e "\n📋 \033[1m[Phase 1] Build Integrity\033[0m"

if [ -f "$PROJECT_DIR/BUILD_INFO" ]; then
    check_pass "BUILD_INFO exists"
    
    # Cross-check frontend SHA
    BUILD_SHA=$(grep '^FRONTEND_SHA256=' "$PROJECT_DIR/BUILD_INFO" | cut -d= -f2)
    CURRENT_SHA=$(sha256sum "$PROJECT_DIR/frontend/talking-head.html" | cut -d' ' -f1)
    if [ "$BUILD_SHA" = "$CURRENT_SHA" ]; then
        check_pass "Frontend SHA matches BUILD_INFO"
    else
        check_warn "Frontend file changed after build (run build.sh if intentional)"
    fi
    
    # Check index.html is symlink
    if [ -L "$PROJECT_DIR/frontend/index.html" ]; then
        check_pass "index.html is symlink (single source of truth)"
    else
        check_fail "index.html is NOT a symlink — may serve stale content!"
    fi
    
    # Check backend_bin exists and is recent
    if [ -f "$PROJECT_DIR/backend_bin" ]; then
        BIN_AGE=$(( $(date +%s) - $(stat -c %Y "$PROJECT_DIR/backend_bin") ))
        if [ $BIN_AGE -lt 3600 ]; then
            BIN_MINS=$((BIN_AGE / 60))
            check_pass "backend_bin is recent (${BIN_MINS}m ago)"
        else
            check_warn "backend_bin is old ($((BIN_AGE / 3600))h ago) — consider rebuild"
        fi
    else
        check_fail "backend_bin not found"
    fi
else
    check_fail "BUILD_INFO missing — no build metadata available"
fi

# ==========================================
# Phase 2: Service Health
# ==========================================
echo -e "\n📋 \033[1m[Phase 2] Service Health\033[0m"

# Backend
BACKEND_CODE=$(curl -s -o /dev/null -w "%{http_code}" "$INTERNAL_BACKEND/api/blogs" 2>/dev/null || echo "000")
if [ "$BACKEND_CODE" = "200" ]; then
    check_pass "Backend (8085): responding"
else
    check_fail "Backend (8085): HTTP $BACKEND_CODE"
fi

# TTS
TTS_CODE=$(curl -s -o /dev/null -w "%{http_code}" "$INTERNAL_TTS/health" 2>/dev/null || echo "000")
if [ "$TTS_CODE" = "200" ]; then
    check_pass "Supertonic TTS (7788): healthy"
else
    check_fail "Supertonic TTS (7788): HTTP $TTS_CODE"
fi

# Nginx
NGINX_CODE=$(curl -sk -o /dev/null -w "%{http_code}" "$BASE_URL/human/" 2>/dev/null || echo "000")
if [ "$NGINX_CODE" = "200" ]; then
    check_pass "Nginx (8089): responding"
else
    check_fail "Nginx (8089): HTTP $NGINX_CODE"
fi

# TTS Audio format
TTS_AUDIO_CODE=$(curl -s -o /tmp/verify_tts.wav -w "%{http_code}" -X POST "$INTERNAL_BACKEND/tts" \
    -H "Content-Type: application/json" \
    -d '{"text":"Test", "voice":"M1", "speed":1.0, "language":"zh"}' 2>/dev/null || echo "000")
if [ "$TTS_AUDIO_CODE" = "200" ]; then
    FILE_TYPE=$(file -b /tmp/verify_tts.wav 2>/dev/null || echo "unknown")
    if echo "$FILE_TYPE" | grep -qi "RIFF\|audio\|wave"; then
        check_pass "TTS Audio: valid format"
    else
        check_fail "TTS Audio: invalid format ($FILE_TYPE)"
    fi
else
    check_fail "TTS Audio: HTTP $TTS_AUDIO_CODE"
fi

# ==========================================
# Phase 3: Content Consistency
# ==========================================
echo -e "\n📋 \033[1m[Phase 3] Content Consistency\033[0m"

# Served version vs BUILD_INFO version
SERVED_HTML=$(curl -s "$INTERNAL_BACKEND/" 2>/dev/null || echo "")
SERVED_VERSION=$(echo "$SERVED_HTML" | grep -oP 'v\d+\.\d+\.\d+' | head -1)

if [ -f "$PROJECT_DIR/BUILD_INFO" ]; then
    BUILD_VERSION=$(grep '^VERSION=' "$PROJECT_DIR/BUILD_INFO" | cut -d= -f2)
    if [ "$SERVED_VERSION" = "$BUILD_VERSION" ]; then
        check_pass "Served version ($SERVED_VERSION) matches BUILD_INFO"
    else
        check_fail "Version mismatch! Served: $SERVED_VERSION, Expected: $BUILD_VERSION"
    fi
    
    # Build ID consistency
    SERVED_BUILD_ID=$(echo "$SERVED_HTML" | grep -oP 'build-id.*?content="([^"]*)"' | grep -oP '\d{14}' | head -1)
    BUILD_ID=$(grep '^BUILD_ID=' "$PROJECT_DIR/BUILD_INFO" | cut -d= -f2)
    if [ "$SERVED_BUILD_ID" = "$BUILD_ID" ]; then
        check_pass "Build ID consistency: $BUILD_ID"
    else
        check_fail "Build ID mismatch! Served: $SERVED_BUILD_ID, Expected: $BUILD_ID"
    fi
else
    if [ -n "$SERVED_VERSION" ]; then
        check_warn "Served version: $SERVED_VERSION (no BUILD_INFO to compare)"
    else
        check_fail "Cannot determine served version"
    fi
fi

# Nginx served same as backend
NGINX_HTML=$(curl -sk "$BASE_URL/human/" 2>/dev/null || echo "")
NGINX_VERSION=$(echo "$NGINX_HTML" | grep -oP 'v\d+\.\d+\.\d+' | head -1)
if [ "$NGINX_VERSION" = "$SERVED_VERSION" ]; then
    check_pass "Nginx serves same version as backend ($NGINX_VERSION)"
else
    check_fail "Nginx/backend version mismatch! Nginx: $NGINX_VERSION, Backend: $SERVED_VERSION"
fi

# Cache control headers
CACHE_HEADER=$(curl -skI "$BASE_URL/human/" 2>/dev/null | grep -i 'cache-control' | tr -d '\r' || echo "")
if echo "$CACHE_HEADER" | grep -qi 'no-cache\|no-store\|max-age=0'; then
    check_pass "Cache-Control: enforced ($CACHE_HEADER)"
else
    check_warn "Cache-Control not enforced ($CACHE_HEADER) — browser may stale-cache"
fi

# ==========================================
# Summary
# ==========================================
echo -e "\n╔══════════════════════════════════════╗"
echo -e "║  Results: ${GREEN}$PASS Passed${NC} | ${RED}$FAIL Failed${NC} | ${YELLOW}$WARN Warn${NC}   ║"
echo "╚══════════════════════════════════════╝"

if [ $FAIL -gt 0 ]; then
    echo -e "${RED}⛔ VERIFICATION FAILED. Do not trust this deployment.${NC}"
    exit 1
elif [ $WARN -gt 0 ]; then
    echo -e "${YELLOW}⚠️  Passed with warnings. Review before trusting.${NC}"
    exit 0
else
    echo -e "${GREEN}🚀 All checks passed. Deployment is trustworthy.${NC}"
    exit 0
fi
