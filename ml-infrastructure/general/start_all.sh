#!/usr/bin/env bash
# Start all ML inference servers in the background using nohup.
# Run from the ml-infrastructure/ directory with the venv activated.
#
# Usage:
#   source .venv/bin/activate
#   bash start_all.sh

set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LOG_DIR="$SCRIPT_DIR/logs"
mkdir -p "$LOG_DIR"

echo "=== Starting all ML inference servers ==="

echo "[1/4] Embeddings Server (port 8901)..."
nohup uv run "$SCRIPT_DIR/serve_embeddings.py" \
  > "$LOG_DIR/embeddings.log" 2>&1 &
echo "       PID=$!  Log: $LOG_DIR/embeddings.log"

# echo "[2/4] Transcription Server (port 8902)..."
# nohup uv run "$SCRIPT_DIR/serve_transcription.py" \
#   > "$LOG_DIR/transcription.log" 2>&1 &
# echo "       PID=$!  Log: $LOG_DIR/transcription.log"

# echo "[3/4] LLM Server (port 8900)..."
# CUDA_VISIBLE_DEVICES=1,2,3,4 nohup uv run python -m vllm.entrypoints.openai.api_server \
#   --model Qwen/Qwen3-32B-AWQ \
#   --quantization awq \
#   --tensor-parallel-size 4 \
#   --port 8900 \
#   --host 0.0.0.0 \
#   --max-model-len 32768 \
#   > "$LOG_DIR/llm.log" 2>&1 &
# echo "       PID=$!  Log: $LOG_DIR/llm.log"

echo "[4/4] Vision Embedding Server (port 8903)..."
nohup uv run "$SCRIPT_DIR/serve_vision.py" \
  > "$LOG_DIR/vision.log" 2>&1 &
echo "       PID=$!  Log: $LOG_DIR/vision.log"

echo ""
echo "=== All servers started ==="
echo "PIDs written above. Logs in: $LOG_DIR/"
echo ""
echo "To follow all logs:  tail -f $LOG_DIR/*.log"
echo "To stop all:         bash stop_all.sh"
