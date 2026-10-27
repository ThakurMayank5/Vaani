import argparse
import wave
from pathlib import Path
import numpy as np
from flask import Flask, request

app = Flask(__name__)

OUTPUT_DIR = Path("test-servers")
OUTPUT_DIR.mkdir(parents=True, exist_ok=True)

RAW_FILE = OUTPUT_DIR / "audio_chunk.raw"
WAV_FILE = OUTPUT_DIR / "audio.wav"
SAMPLE_RATE = 16000


def convert_raw_to_wav(raw_path: Path = RAW_FILE, wav_path: Path = WAV_FILE, sample_rate: int = SAMPLE_RATE):
    """Convert raw signed 16-bit PCM samples into a standard mono WAV file."""
    if not raw_path.exists():
        print(f"❌ Raw file not found: {raw_path}")
        return False

    samples = np.fromfile(raw_path, dtype="<i2")
    if len(samples) == 0:
        print("⚠️  Warning: Raw audio file is empty.")
        return False

    duration = len(samples) / sample_rate
    print(f"🎵 Audio details:")
    print(f"   • Samples:  {len(samples)}")
    print(f"   • Duration: {duration:.2f} seconds")
    print(f"   • Range:    [{samples.min()}, {samples.max()}]")

    with wave.open(str(wav_path), "wb") as wav:
        wav.setnchannels(1)
        wav.setsampwidth(2)
        wav.setframerate(sample_rate)
        wav.writeframes(samples.tobytes())

    print(f"✅ Created WAV: {wav_path}")
    return True


@app.post("/audio")
def audio():
    """Endpoint for ESP32 to stream raw 16-bit PCM audio chunks."""
    data = request.get_data()
    if not data:
        return "Empty audio chunk", 400

    with RAW_FILE.open("ab") as f:
        f.write(data)

    total_bytes = RAW_FILE.stat().st_size
    print(f"Received chunk: {len(data)} bytes | Total: {total_bytes} bytes")
    return "OK", 200


@app.get("/convert")
def convert_endpoint():
    """Trigger WAV conversion via HTTP."""
    if convert_raw_to_wav():
        return {"success": True, "message": f"Converted to {WAV_FILE.name}"}, 200
    return {"success": False, "message": "Conversion failed"}, 400


def main():
    parser = argparse.ArgumentParser(description="ESP32 Audio Stream Receiver & Converter")
    parser.add_argument("--convert", action="store_true", help="Convert existing audio_chunk.raw to audio.wav and exit")
    parser.add_argument("--port", type=int, default=8000, help="Server port (default: 8000)")
    args = parser.parse_args()

    if args.convert:
        convert_raw_to_wav()
        return

    # Start new recording session with a fresh file
    RAW_FILE.write_bytes(b"")
    print(f"🎙️  Audio Receiver running on port {args.port}...")
    print(f"   • Streaming endpoint: POST http://<IP>:{args.port}/audio")
    print(f"   • Conversion endpoint: GET  http://<IP>:{args.port}/convert")
    print(f"   • Output: {RAW_FILE}")

    app.run(host="0.0.0.0", port=args.port, debug=False, threaded=True)


if __name__ == "__main__":
    main()
