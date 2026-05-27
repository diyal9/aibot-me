#!/bin/bash
# Stop Supertonic TTS server

PID_FILE="$(cd "$(dirname "$0")" && pwd)/supertonic.pid"

if [ ! -f "$PID_FILE" ]; then
    echo "No PID file found. Supertonic may not be running."
    exit 0
fi

PID=$(cat "$PID_FILE")
if kill -0 "$PID" 2>/dev/null; then
    echo "Stopping Supertonic (PID: $PID)..."
    kill "$PID"
    rm -f "$PID_FILE"
    echo "Stopped."
else
    echo "Process $PID not running. Cleaning up PID file."
    rm -f "$PID_FILE"
fi
