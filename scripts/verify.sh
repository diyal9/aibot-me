#!/bin/bash
# ==========================================
# AI Digital Human - Verification Gate
# Usage: ./scripts/verify.sh
# ==========================================

set -e

PASS=0
FAIL=0
BASE_URL="https://127.0.0.1:8089"
INTERNAL_BACKEND="http://127.0.0.1:8085"
INTERNAL_TTS="http://127.0.0.1:7788"

echo -e "\n🔍 \033[1mRunning Pre-Deployment Verification...\033[0m"

# 1. Backend Health
check_status() {
    local url=$1
    local name=$2
    local expected_code=$3
    local actual_code=$(curl -s -o /dev/null -w "%{http_code}" -k "$url")
    if [ "$actual_code" == "$expected_code" ]; then
        echo -e "  \033[32m✅\033[0m $name: $actual_code"
        PASS=$((PASS+1))
    else
        echo -e "  \033[31m❌\033[0m $name: Expected $expected_code, got $actual_code"
        FAIL=$((FAIL+1))
    fi
}

# 2. TTS Check
check_tts() {
    local resp_code=$(curl -s -o /tmp/verify_tts.wav -w "%{http_code}" -X POST "$INTERNAL_BACKEND/tts" \
        -H "Content-Type: application/json" \
        -d '{"text":"Test TTS", "voice":"M1", "speed":1.0, "language":"zh"}')

    if [ "$resp_code" == "200" ]; then
        local file_type=$(file -b /tmp/verify_tts.wav)
        if echo "$file_type" | grep -qi "RIFF\|audio\|wave"; then
            echo -e "  \033[32m✅\033[0m TTS Supertonic: Audio OK ($file_type)"
            PASS=$((PASS+1))
        else
            echo -e "  \033[31m❌\033[0m TTS Supertonic: Invalid format ($file_type)"
            FAIL=$((FAIL+1))
        fi
    else
        echo -e "  \033[31m❌\033[0m TTS Supertonic: HTTP $resp_code"
        FAIL=$((FAIL+1))
    fi
}

# 3. Frontend Version Check
check_frontend() {
    local version=$(curl -s -k "$BASE_URL/human/" | grep -o 'v[0-9]\+\.[0-9]\+\.[0-9]\+' | head -1)
    if [ -n "$version" ]; then
        echo -e "  \033[32m✅\033[0m Frontend Version: $version"
        PASS=$((PASS+1))
    else
        echo -e "  \033[31m❌\033[0m Frontend Version: Not found"
        FAIL=$((FAIL+1))
    fi
}

echo -e "\n📋 \033[1m[Services]\033[0m"
check_status "$INTERNAL_BACKEND/api/blogs" "Backend (8085)" "200"
check_status "$INTERNAL_TTS/health" "Supertonic (7788)" "200"
check_status "$BASE_URL/human/" "Nginx/HTML (8089)" "200"

echo -e "\n📋 \033[1m[Integrations]\033[0m"
check_tts
check_frontend

echo -e "\n╔════════════════════════════════════╗"
echo -e "║  Results: $PASS Passed | $FAIL Failed   ║"
echo -e "╚════════════════════════════════════╝"

if [ $FAIL -gt 0 ]; then
    echo -e "\033[31m⛔ Verification FAILED. Do not deploy.\033[0m"
    exit 1
else
    echo -e "\033[32m🚀 All checks passed. Ready to deploy.\033[0m"
    exit 0
fi
