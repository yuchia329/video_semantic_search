import os
os.environ["CUDA_VISIBLE_DEVICES"] = "0"
from fastapi import FastAPI, UploadFile, File, HTTPException
import torch
from transformers import AutoModelForCausalLM, AutoTokenizer
from pydantic import BaseModel
import shutil
from PIL import Image

app = FastAPI(title="Vision Captioning Service (moondream2)")

device = "cuda" if torch.cuda.is_available() else "cpu"
print(f"Loading moondream2 model on {device}...")

model_name = "vikhyatk/moondream2"
tokenizer = AutoTokenizer.from_pretrained(model_name, trust_remote_code=True)
model = AutoModelForCausalLM.from_pretrained(
    model_name,
    trust_remote_code=True,
    torch_dtype=torch.float16 if device == "cuda" else torch.float32,
    device_map={"": device},
)
model.eval()

print("moondream2 loaded successfully.")

class CaptionResponse(BaseModel):
    caption: str

@app.post("/caption", response_model=CaptionResponse)
async def caption_image(file: UploadFile = File(...)):
    """Generate a text caption/description for an uploaded image."""
    try:
        temp_path = f"/tmp/{file.filename}"
        with open(temp_path, "wb") as buffer:
            shutil.copyfileobj(file.file, buffer)

        image = Image.open(temp_path).convert("RGB")

        # moondream2 uses encode_image + answer
        enc_image = model.encode_image(image)
        caption = model.answer_question(
            enc_image,
            "Describe this image in detail. Include objects, people, colors, text, actions, and setting.",
            tokenizer,
        )

        os.remove(temp_path)
        return CaptionResponse(caption=caption)
    except Exception as e:
        raise HTTPException(status_code=500, detail=str(e))

@app.get("/health")
async def health():
    return {"status": "ok", "model": "moondream2"}

if __name__ == "__main__":
    import uvicorn
    # Bind to 0.0.0.0 to allow SSH tunneling from external interfaces
    uvicorn.run(app, host="0.0.0.0", port=8903)
