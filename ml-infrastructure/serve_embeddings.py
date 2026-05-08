import os
from fastapi import FastAPI, HTTPException
from pydantic import BaseModel
from sentence_transformers import SentenceTransformer
import torch

app = FastAPI(title="Embedding Service")

# Setup device
device = "cuda" if torch.cuda.is_available() else "cpu"
print(f"Loading embedding model on {device}...")

# Load the BGE-m3 model (same one we planned to use with TEI)
# You can change this to 'nomic-ai/nomic-embed-text-v1' if desired.
model = SentenceTransformer("BAAI/bge-m3", device=device)

class EmbeddingRequest(BaseModel):
    text: str

class EmbeddingResponse(BaseModel):
    embedding: list[float]

@app.post("/embed", response_model=EmbeddingResponse)
async def embed_text(req: EmbeddingRequest):
    try:
        # Encode the text
        embedding = model.encode(req.text, normalize_embeddings=True)
        return EmbeddingResponse(embedding=embedding.tolist())
    except Exception as e:
        raise HTTPException(status_code=500, detail=str(e))

if __name__ == "__main__":
    import uvicorn
    # Bind to 0.0.0.0 to allow SSH tunneling from external interfaces
    uvicorn.run(app, host="0.0.0.0", port=8080)
