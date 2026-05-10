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
Runs `vLLM` to serve large language models natively.
```bash
python -m vllm.entrypoints.openai.api_server \
  --model meta-llama/Llama-3.1-8B-Instruct \
  --port 8900 \
  --host 0.0.0.0
```

## 3. Setup SSH Port Forwarding (On Your Local Mac)

To securely connect your local Go backend to these remote Python APIs, open a terminal on your M4 Mac and run the following command. This will bind your local ports to the server's ports over an encrypted SSH tunnel.

```bash
ssh -N -L 8000:localhost:8000 \
       -L 8080:localhost:8080 \
       -L 8081:localhost:8081 \
       user@your_school_server_ip
```
*(Leave this terminal running in the background).*

Now, your local Go backend can send requests to `localhost:8000`, `localhost:8080`, and `localhost:8081`, and they will automatically and securely be forwarded to the GPU models running on your school server!
