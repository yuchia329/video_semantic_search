# End-to-End Video-LLM Infrastructure

This stack uses a **3-service** architecture:
- **`cyankiwi/Qwen3-VL-8B-Instruct-AWQ-4bit`** (Port 8900) — Video-LLM: visual understanding, Q&A, and chat
- **WhisperX** (Port 8902) — Audio transcription with word-level timestamps
- **wav2vec2-emotion** (Port 8904) — Audio emotion classification per speech segment (runs on CPU)

All three services run on the **remote GPU server**. They are accessed:
- **Locally**: via `ssh -N -L` port forwarding to `localhost`
- **In production (K8s)**: via the `gpu-tunnel` Deployment which maintains the SSH forward inside the cluster

---

## 1. Setup on the Remote Server

```bash
ssh user@your_gpu_server_ip
curl -LsSf https://astral.sh/uv/install.sh | sh
uv venv --python 3.11
source .venv/bin/activate
uv pip install -r requirements.txt
```

---

## 2. Running the Services (use tmux or screen)

### A. Transcription Server — GPU (Port 8902)
```bash
CUDA_VISIBLE_DEVICES=0 python serve_transcription.py
```

### B. Emotion Classification Server — CPU (Port 8904)
```bash
python serve_emotion.py
```

### C. Video-LLM Server — GPU (Port 8900)
Serves `cyankiwi/Qwen3-VL-8B-Instruct-AWQ-4bit` via vLLM. Prefix caching keeps the KV cache
warm across chat turns so only new question tokens are prefilled on follow-up messages.

```bash
PYTORCH_ALLOC_CONF=expandable_segments:True \
CUDA_VISIBLE_DEVICES=1 \
uv run vllm serve cyankiwi/Qwen3-VL-8B-Instruct-AWQ-4bit \
  --port 8900 \
  --host 0.0.0.0 \
  --max-model-len 160000 \
  --gpu-memory-utilization 0.95 \
  --enable-prefix-caching \
  --limit-mm-per-prompt image=600 \
  --quantization compressed-tensors \
  --kv-cache-dtype fp8
```
Old one:
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

> **`--enable-prefix-caching`**: KV cache for the video context (frames + transcript) stays warm in VRAM. Subsequent chat turns reuse this — only the new question tokens are prefilled.

> **`--limit-mm-per-prompt image=600`**: Allows up to 600 frames per request (~20 min at 0.5 fps).

---

## 3. Prometheus Telemetry (LLM-Native Metrics)

vLLM automatically exposes a **Prometheus `/metrics` endpoint** on the same port (8900) — no extra configuration is needed.

### Verify metrics are live
```bash
curl http://localhost:8900/metrics | grep vllm
```

### Key metrics exposed

| Metric | Description |
|--------|-------------|
| `vllm:generation_tokens_total` | Total output tokens generated |
| `vllm:prompt_tokens_total` | Total prompt tokens processed |
| `vllm:time_to_first_token_seconds` | TTFT histogram |
| `vllm:time_per_output_token_seconds` | Per-output-token latency |
| `vllm:e2e_request_latency_seconds` | End-to-end request latency |
| `vllm:gpu_cache_usage_perc` | KV cache GPU utilisation (0–1) |
| `vllm:num_requests_running` | Requests currently being processed |
| `vllm:num_requests_waiting` | Requests queued waiting for capacity |

### Kubernetes scraping

In production the `gpu-tunnel` Service exposes port 8900 cluster-wide. Apply the
ServiceMonitor so Prometheus (from `k8s-observability-platform`) scrapes it:

```bash
kubectl apply -f ml-infrastructure/kubernetes/vllm-servicemonitor.yaml
```

Verify the target appears in Prometheus: **Status → Targets → vllm-qwen**

---

## 4. SSH Port Forwarding

### Local development (on your Mac)
```bash
ssh -N \
  -L 8900:localhost:8900 \
  -L 8902:localhost:8902 \
  -L 8904:localhost:8904 \
  user@your_gpu_server_ip
```
*(Leave this terminal running in the background)*

### Production (K8s `gpu-tunnel` pod)
The `kubernetes/gpu-tunnel/deployment.yaml` pod runs this automatically inside the cluster.
Create the SSH key secret first:
```bash
kubectl create secret generic gpu-ssh-key \
  --from-file=id_rsa=~/.ssh/your_gpu_key \
  -n video-search
```

---

## GPU Assignment

| Resource | Model | VRAM/RAM |
|----------|-------|----------|
| GPU 1 | Qwen3-VL-8B-Instruct-AWQ-4bit | ~10GB |
| GPU 0 | WhisperX large-v2 | ~3GB |
| CPU | wav2vec2-emotion | ~300MB RAM |
