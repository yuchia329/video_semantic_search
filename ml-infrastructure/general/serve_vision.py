import os
import io
import torch
from fastapi import FastAPI, UploadFile, File, HTTPException
from transformers import AutoProcessor, AutoModel
from pydantic import BaseModel
from PIL import Image

# Force CUDA 0 before any torch imports if possible, or keep as is
os.environ["CUDA_VISIBLE_DEVICES"] = "0"

app = FastAPI(title="Vision Embedding Service (SigLIP)")

# Use 'cuda' only if the driver is actually compatible
device = "cuda" if torch.cuda.is_available() else "cpu"
print(f"Loading SigLIP model on {device}...")

model_name = "google/siglip-base-patch16-224"
processor = AutoProcessor.from_pretrained(model_name)
model = AutoModel.from_pretrained(model_name).to(device)

class TextEmbeddingRequest(BaseModel):
    text: str

class EmbeddingResponse(BaseModel):
    embedding: list[float]

@app.post("/embed_image", response_model=EmbeddingResponse)
async def embed_image(file: UploadFile = File(...)):
    try:
        # READ DIRECTLY INTO MEMORY (Fixes permission/path errors)
        request_object_content = await file.read()
        image = Image.open(io.BytesIO(request_object_content)).convert("RGB")
        
        inputs = processor(images=image, return_tensors="pt").to(device)
        
        with torch.no_grad():
            image_features = model.get_image_features(**inputs)
            
        # Normalize the embedding
        image_features = image_features / image_features.norm(dim=-1, keepdim=True)
        embedding = image_features.cpu().numpy()[0].tolist()
        
        return EmbeddingResponse(embedding=embedding)
    except Exception as e:
        # LOG THE ACTUAL ERROR to the terminal
        print(f"ERROR: {str(e)}")
        raise HTTPException(status_code=500, detail=f"Inference failed: {str(e)}")

@app.post("/embed_text", response_model=EmbeddingResponse)
async def embed_text(req: TextEmbeddingRequest):
    try:
        # SigLIP text embeddings for matching against image embeddings
        inputs = processor(text=[req.text], padding="max_length", return_tensors="pt").to(device)
        
        with torch.no_grad():
            text_features = model.get_text_features(**inputs)
            
        # Normalize the embedding
        text_features = text_features / text_features.norm(dim=-1, keepdim=True)
        embedding = text_features.cpu().numpy()[0].tolist()
        
        return EmbeddingResponse(embedding=embedding)
    except Exception as e:
        raise HTTPException(status_code=500, detail=str(e))

if __name__ == "__main__":
    import uvicorn
    # Bind to 0.0.0.0 to allow SSH tunneling from external interfaces
    uvicorn.run(app, host="0.0.0.0", port=8903)
