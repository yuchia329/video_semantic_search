import os
os.environ["CUDA_VISIBLE_DEVICES"] = "0"
import shutil
from fastapi import FastAPI, File, UploadFile, HTTPException
import whisperx
import torch

app = FastAPI(title="Transcription Service")

device = "cuda" if torch.cuda.is_available() else "cpu"
batch_size = 16 # reduce if low on GPU mem
compute_type = "float16" if device == "cuda" else "int8"

print(f"Loading WhisperX model on {device}...")
# Load the WhisperX model once
model = whisperx.load_model(
    "large-v2", 
    device, 
    compute_type=compute_type,
    # multilingual=True,
    # max_new_tokens=5024,
    # clip_timestamps=True,
    # hallucination_silence_threshold
    )

@app.post("/transcribe")
async def transcribe_audio(file: UploadFile = File(...)):
    try:
        # Save uploaded file temporarily
        temp_path = f"/tmp/{file.filename}"
        with open(temp_path, "wb") as buffer:
            shutil.copyfileobj(file.file, buffer)

        # 1. Transcribe with WhisperX
        audio = whisperx.load_audio(temp_path)
        result = model.transcribe(audio, batch_size=batch_size)

        # 2. Optional: Align Whisper output
        model_a, metadata = whisperx.load_align_model(language_code=result["language"], device=device)
        aligned_result = whisperx.align(result["segments"], model_a, metadata, audio, device, return_char_alignments=False)

        # Cleanup
        os.remove(temp_path)

        return {"language": result["language"], "segments": aligned_result["segments"]}

    except Exception as e:
        raise HTTPException(status_code=500, detail=str(e))

if __name__ == "__main__":
    import uvicorn
    # Bind to 0.0.0.0 to allow SSH tunneling
    uvicorn.run(app, host="0.0.0.0", port=8902)
