import os
import shutil
import tempfile
import json
from typing import List

os.environ["CUDA_VISIBLE_DEVICES"] = ""  # Run on CPU — model is only ~300MB

from fastapi import FastAPI, File, UploadFile, HTTPException
from pydantic import BaseModel
import torch
import librosa
import numpy as np
from transformers import pipeline

app = FastAPI(title="Audio Emotion Classification Service")

print("Loading wav2vec2 emotion model on CPU...")
emotion_pipe = pipeline(
    "audio-classification",
    model="ehcalabres/wav2vec2-lg-xlsr-en-speech-emotion-recognition",
    device=-1,  # CPU
)
print("Emotion model loaded successfully.")


class Segment(BaseModel):
    start: float
    end: float
    text: str


class EmotionResult(BaseModel):
    start: float
    end: float
    text: str
    emotion: str
    confidence: float


class EmotionResponse(BaseModel):
    segments: List[EmotionResult]


@app.post("/emotion", response_model=EmotionResponse)
async def classify_emotions(
    file: UploadFile = File(...),
    segments: str = "",  # JSON string of WhisperX segments
):
    """
    Classify emotions for each transcript segment in the audio file.
    - file: the WAV audio file
    - segments: JSON array of {start, end, text} objects from WhisperX
    """
    try:
        # Save uploaded audio
        suffix = os.path.splitext(file.filename or "audio.wav")[1] or ".wav"
        with tempfile.NamedTemporaryFile(suffix=suffix, delete=False) as tmp:
            shutil.copyfileobj(file.file, tmp)
            tmp_path = tmp.name

        # Load full audio
        audio, sr = librosa.load(tmp_path, sr=16000, mono=True)
        os.remove(tmp_path)

        # Parse segment list
        seg_list: List[Segment] = []
        if segments:
            raw = json.loads(segments)
            seg_list = [Segment(**s) for s in raw]

        results: List[EmotionResult] = []
        for seg in seg_list:
            start_sample = int(seg.start * sr)
            end_sample = int(seg.end * sr)
            clip = audio[start_sample:end_sample]

            # Skip very short clips (< 0.5s) — unreliable
            if len(clip) < sr * 0.5:
                results.append(EmotionResult(
                    start=seg.start,
                    end=seg.end,
                    text=seg.text,
                    emotion="neutral",
                    confidence=0.0,
                ))
                continue

            preds = emotion_pipe(clip, sampling_rate=sr, top_k=1)
            top = preds[0]
            results.append(EmotionResult(
                start=seg.start,
                end=seg.end,
                text=seg.text,
                emotion=top["label"],
                confidence=round(top["score"], 3),
            ))

        return EmotionResponse(segments=results)

    except Exception as e:
        raise HTTPException(status_code=500, detail=str(e))


@app.get("/health")
async def health():
    return {"status": "ok", "model": "wav2vec2-emotion"}


if __name__ == "__main__":
    import uvicorn
    uvicorn.run(app, host="0.0.0.0", port=8904)
