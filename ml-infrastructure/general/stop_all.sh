#!/usr/bin/env bash
# Stop all ML inference servers by killing processes on their known ports.
#
# Usage:  bash stop_all.sh

set -euo pipefail

PORTS=(8900)
NAMES=("LLM" "Embeddings" "Transcription" "Vision")

echo "=== Stopping ML inference servers ==="

for i in "${!PORTS[@]}"; do
  PORT=${PORTS[$i]}
  NAME=${NAMES[$i]}
  PIDS=$(lsof -ti :"$PORT" 2>/dev/null || true)
  if [ -n "$PIDS" ]; then
    echo "Stopping $NAME server (port $PORT, PIDs: $PIDS)..."
    echo "$PIDS" | xargs kill -SIGTERM 2>/dev/null || true
  else
    echo "$NAME server (port $PORT) — not running."
  fi
done

echo "=== Done ==="
