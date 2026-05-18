import os
os.environ["CUDA_VISIBLE_DEVICES"] = ""  # Run on CPU — model is only ~300MB

import tempfile
import grpc
from concurrent import futures
import librosa
from transformers import pipeline

import mlpb.ml_pb2 as ml_pb2
import mlpb.ml_pb2_grpc as ml_pb2_grpc

print("Loading wav2vec2 emotion model on CPU...")
emotion_pipe = pipeline(
    "audio-classification",
    model="ehcalabres/wav2vec2-lg-xlsr-en-speech-emotion-recognition",
    device=-1,  # CPU
)
print("Emotion model loaded successfully.")

class EmotionService(ml_pb2_grpc.EmotionServiceServicer):
    def ClassifyEmotions(self, request, context):
        try:
            # Save audio bytes to a temporary file
            with tempfile.NamedTemporaryFile(suffix=".wav", delete=False) as tmp:
                tmp.write(request.audio_data)
                tmp_path = tmp.name

            # Load full audio
            audio, sr = librosa.load(tmp_path, sr=16000, mono=True)
            os.remove(tmp_path)

            response = ml_pb2.EmotionResponse()
            
            for seg in request.segments:
                start_sample = int(seg.start * sr)
                end_sample = int(seg.end * sr)
                clip = audio[start_sample:end_sample]

                # Skip very short clips (< 0.5s) — unreliable
                if len(clip) < sr * 0.5:
                    response.segments.append(ml_pb2.EmotionResult(
                        start=seg.start,
                        end=seg.end,
                        text=seg.text,
                        emotion="neutral",
                        confidence=0.0
                    ))
                    continue

                preds = emotion_pipe(clip, sampling_rate=sr, top_k=1)
                top = preds[0]
                response.segments.append(ml_pb2.EmotionResult(
                    start=seg.start,
                    end=seg.end,
                    text=seg.text,
                    emotion=top["label"],
                    confidence=round(top["score"], 3)
                ))

            return response

        except Exception as e:
            context.set_code(grpc.StatusCode.INTERNAL)
            context.set_details(str(e))
            return ml_pb2.EmotionResponse()

def serve():
    server = grpc.server(futures.ThreadPoolExecutor(max_workers=4), options=[
        ('grpc.max_receive_message_length', 50 * 1024 * 1024), # 50 MB
    ])
    ml_pb2_grpc.add_EmotionServiceServicer_to_server(EmotionService(), server)
    server.add_insecure_port('[::]:8904')
    print("gRPC Emotion Service listening on port 8904...")
    server.start()
    server.wait_for_termination()

if __name__ == "__main__":
    serve()
