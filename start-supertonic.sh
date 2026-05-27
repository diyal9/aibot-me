#!/bin/bash
# Start Supertonic TTS server for aibot-me
# Usage: ./start-supertonic.sh

set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
VENV_PYTHON="$SCRIPT_DIR/supertonic-venv/bin/python"
PID_FILE="$SCRIPT_DIR/supertonic.pid"
LOG_FILE="$SCRIPT_DIR/supertonic.log"

# Check if already running
if [ -f "$PID_FILE" ]; then
    PID=$(cat "$PID_FILE")
    if kill -0 "$PID" 2>/dev/null; then
        echo "Supertonic already running (PID: $PID)"
        exit 0
    else
        echo "Stale PID file, cleaning up"
        rm -f "$PID_FILE"
    fi
fi

# Check venv exists
if [ ! -f "$VENV_PYTHON" ]; then
    echo "Error: Virtual environment not found at supertonic-venv"
    echo "Run: cd /root/aibot-me && uv venv --python 3.11 supertonic-venv && uv pip install --python supertonic-venv/bin/python 'supertonic[serve]'"
    exit 1
fi

echo "Starting Supertonic TTS server on :7788..."
nohup "$VENV_PYTHON" "$SCRIPT_DIR/supertonic-server.py" > "$LOG_FILE" 2>&1 &
PID=$!
echo $PID > "$PID_FILE"
echo "Supertonic started (PID: $PID)"
echo "Log: $LOG_FILE"

# Wait for server to be ready
for i in $(seq 1 30); do
    if curl -s http://127.0.0.1:7788/health > /dev/null 2>&1; then
        echo "Supertonic is ready!"
        exit 0
    fi
    sleep 2
done

echo "Warning: Supertonic may not be fully ready yet. Check log: $LOG_FILE"
