# Native ML Infrastructure & SSH Port Forwarding

Since the school GPU server does not support Docker and only exposes port 22, we will run our inference servers directly using Python and `uv`, and access them securely using SSH local port forwarding.

## 1. Setup on the Remote Server

SSH into your remote GPU server:
```bash
ssh user@your_school_server_ip
```

### Install `uv`
If you haven't installed `uv` yet:
```bash
curl -LsSf https://astral.sh/uv/install.sh | sh
```

### Create Environment and Install Dependencies
Navigate to this directory (`ml-infrastructure`) on the server and run:
```bash
# Create a virtual environment using python 3.10+
uv venv

# Activate the virtual environment
source .venv/bin/activate

# Install the dependencies
uv pip install -r requirements.txt
uv pip install transformers pillow torchvision

```

## 2. Running the Inference Servers

You will need to start three separate processes (using `tmux`, `screen`, or `nohup`) inside your activated `uv` environment.

### A. Embeddings Server (Port 8901)
Runs `BAAI/bge-m3` using `sentence-transformers`.
```bash
python serve_embeddings.py
```

### B. Transcription Server (Port 8902)
Runs `WhisperX` for fast transcription with timestamps.
```bash
python serve_transcription.py
```

### C. LLM Server (Port 8900)
Runs `vLLM` to serve a quantized Llama-3 70B model natively (fits on 2x 3090 GPUs).
```bash
python -m vllm.entrypoints.openai.api_server \
  --model casperhansen/llama-3-70b-instruct-awq \
  --quantization awq \
  --tensor-parallel-size 2 \
  --port 8900 \
  --host 0.0.0.0
```

### D. Vision Embedding Server (Port 8903)
Runs `SigLIP` to encode video frames and visual search queries into a shared multimodal space.
```bash
python serve_vision.py
```

## 3. Setup SSH Port Forwarding (On Your Local Mac)

To securely connect your local Go backend to these remote Python APIs, open a terminal on your M4 Mac and run the following command. This will bind your local ports to the server's ports over an encrypted SSH tunnel.

```bash
ssh -N -L 8900:localhost:8900 \
       -L 8901:localhost:8901 \
       -L 8902:localhost:8902 \
       -L 8903:localhost:8903 \
       user@your_school_server_ip
```
*(Leave this terminal running in the background).*

Now, your local Go backend can send requests to `localhost:8900`, `localhost:8901`, `localhost:8902`, and `localhost:8903`, and they will automatically and securely be forwarded to the GPU models running on your school server!
