import torch
import os
import urllib.request

# Get the default PyTorch cache directory
model_dir = torch.hub._get_torch_home()
os.makedirs(model_dir, exist_ok=True)

# The exact filename WhisperX looks for
model_fp = os.path.join(model_dir, "whisperx-vad-segmentation.bin")

print(f"Downloading VAD model directly to: {model_fp}")

# Download the model directly from the WhisperX GitHub repository
url = "https://raw.githubusercontent.com/m-bain/whisperX/main/whisperx/assets/pytorch_model.bin"
urllib.request.urlretrieve(url, model_fp)

print("Download complete! WhisperX will now skip the broken link.")