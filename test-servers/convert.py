
import numpy as np
import wave
from pathlib import Path

input_file = Path("test-servers/audio_chunk.raw")
output_file = Path("test-servers/audio.wav")

SAMPLE_RATE = 16000

# ESP32 sends signed 16-bit PCM samples.
samples = np.fromfile(input_file, dtype="<i2")

print("Samples:", len(samples))
print("Duration:", len(samples) / SAMPLE_RATE, "seconds")
print("Min:", samples.min() if len(samples) else 0)
print("Max:", samples.max() if len(samples) else 0)

with wave.open(str(output_file), "wb") as wav:
    wav.setnchannels(1)
    wav.setsampwidth(2)
    wav.setframerate(SAMPLE_RATE)
    wav.writeframes(samples.tobytes())

print("Created:", output_file)
