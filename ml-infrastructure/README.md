# End-to-End Video-LLM Infrastructure

This stack uses a **3-service** architecture:
- **Qwen2.5-VL-7B-Instruct** (Port 8900) — Single Video-LLM: handles visual understanding, Q&A, and chat
- **WhisperX** (Port 8902) — Audio transcription with word-level timestamps
- **wav2vec2-emotion** (Port 8904) — Audio emotion classification per speech segment (runs on CPU)

Removed from old architecture: BGE-m3 embeddings (8901), moondream2 vision captions (8903), pgvector.

---

## 1. Setup on the Remote Server

SSH into your remote GPU server:
```bash
ssh user@your_school_server_ip
```

### Install `uv`
```bash
curl -LsSf https://astral.sh/uv/install.sh | sh
```

### Create Environment and Install Dependencies
```bash
uv venv --python 3.11
source .venv/bin/activate
uv pip install -r requirements.txt
```

---

## 2. Running the Services (use tmux or screen)

### A. Transcription Server — GPU 0 (Port 8902)
```bash
CUDA_VISIBLE_DEVICES=0 python serve_transcription.py
```

### B. Emotion Classification Server — CPU (Port 8904)
```bash
python serve_emotion.py
```

### C. Video-LLM Server — GPUs 0,1,2 (Port 8900)
Serves `Qwen2.5-VL-7B-Instruct`. Prefix caching keeps KV cache warm across chat turns.
```bash
PYTORCH_ALLOC_CONF=expandable_segments:True CUDA_VISIBLE_DEVICES=0,1,2 \
uv run vllm serve Qwen/Qwen2.5-VL-7B-Instruct \
  --tensor-parallel-size 3 \
  --port 8900 \
  --host 0.0.0.0 \
  --max-model-len 131072 \
  --gpu-memory-utilization 0.90 \
  --enable-prefix-caching \
  --limit-mm-per-prompt image=600
```

> **Note on `--enable-prefix-caching`**: After a video is processed, the VLM's KV cache for the video context (frames + transcript) stays warm in VRAM. Subsequent chat turns reuse this cache — only the new question tokens are prefilled. This makes chat fast after the initial load.

> **Note on `--limit-mm-per-prompt image=600`**: Allows up to 600 frames per request (~20 min at 0.5fps).

---

## 3. SSH Port Forwarding (On Your Local Mac)

```bash
ssh -N \
  -L 8900:localhost:8900 \
  -L 8902:localhost:8902 \
  -L 8904:localhost:8904 \
  user@your_school_server_ip
```
*(Leave this terminal running in the background)*

The Go backend communicates with all three services over `localhost` via this tunnel.

---

## GPU Assignment Summary

| GPU | Model | VRAM Used |
|-----|-------|-----------|
| GPU 0 | WhisperX large-v2 + Qwen2.5-VL-7B shard | ~3GB + ~5GB |
| GPU 1 | Qwen2.5-VL-7B shard | ~5GB |
| GPU 2 | Qwen2.5-VL-7B shard | ~5GB |
| CPU | wav2vec2-emotion | ~300MB RAM |

Total VRAM used: ~18GB / 72GB — leaves 54GB for KV cache (holds ~30-min video at 0.5fps).
