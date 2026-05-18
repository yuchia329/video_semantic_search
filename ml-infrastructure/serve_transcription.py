import os
os.environ["CUDA_VISIBLE_DEVICES"] = "0"
import tempfile
import grpc
from concurrent import futures
import whisperx
import torch
import numpy as np

# Import generated gRPC code
import mlpb.ml_pb2 as ml_pb2
import mlpb.ml_pb2_grpc as ml_pb2_grpc

device = "cuda" if torch.cuda.is_available() else "cpu"
batch_size = 16 # reduce if low on GPU mem
compute_type = "float16" if device == "cuda" else "int8"

print(f"Loading WhisperX model on {device}...")
model = whisperx.load_model(
    "large-v2", 
    device, 
    compute_type=compute_type,
)
print("WhisperX model loaded successfully.")

class TranscriptionService(ml_pb2_grpc.TranscriptionServiceServicer):
    def Transcribe(self, request, context):
        try:
            # Save audio bytes to a temporary file because WhisperX requires a file path
            with tempfile.NamedTemporaryFile(suffix=".wav", delete=False) as tmp:
                tmp.write(request.audio_data)
                tmp_path = tmp.name

            # 1. Transcribe with WhisperX
            audio = whisperx.load_audio(tmp_path)
            result = model.transcribe(audio, batch_size=batch_size)

            # 2. Align Whisper output
            model_a, metadata = whisperx.load_align_model(language_code=result["language"], device=device)
            aligned_result = whisperx.align(result["segments"], model_a, metadata, audio, device, return_char_alignments=False)

            # Cleanup
            os.remove(tmp_path)

            response = ml_pb2.TranscribeResponse(language=result["language"])
            for seg in aligned_result["segments"]:
                response.segments.append(ml_pb2.TranscriptionSegment(
                    start=seg.get("start", 0.0),
                    end=seg.get("end", 0.0),
                    text=seg.get("text", "")
                ))
            
            return response

        except Exception as e:
            context.set_code(grpc.StatusCode.INTERNAL)
            context.set_details(str(e))
            return ml_pb2.TranscribeResponse()

def serve():
    server = grpc.server(futures.ThreadPoolExecutor(max_workers=4), options=[
        ('grpc.max_receive_message_length', 50 * 1024 * 1024), # 50 MB
    ])
    ml_pb2_grpc.add_TranscriptionServiceServicer_to_server(TranscriptionService(), server)
    server.add_insecure_port('[::]:8902')
    print("gRPC Transcription Service listening on port 8902...")
    server.start()
    server.wait_for_termination()

if __name__ == "__main__":
    serve()
