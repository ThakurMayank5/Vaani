"""Local Whisper ASR HTTP service for VAANI voice commands.

Run from the ai-services directory:
    python -m uvicorn asr.asr:app --host 0.0.0.0 --port 43001
"""

import os
from pathlib import Path
import tempfile
import threading

from fastapi import FastAPI, File, HTTPException, UploadFile
from fastapi.middleware.cors import CORSMiddleware
from faster_whisper import WhisperModel


ASR_HOST = "0.0.0.0"
ASR_PORT = 43001
WHISPER_MODEL_SIZE = "base"
WHISPER_DEVICE = "cpu"
WHISPER_COMPUTE_TYPE = "int8"
MAX_AUDIO_BYTES = 15 * 1024 * 1024

app = FastAPI(title="VAANI ASR", version="1.0")
app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_credentials=False,
    allow_methods=["GET", "POST", "OPTIONS"],
    allow_headers=["*"],
)

_model: WhisperModel | None = None
_model_lock = threading.Lock()
_transcription_lock = threading.Lock()


def get_model() -> WhisperModel:
    """Load Whisper once, on the first real transcription request."""
    global _model
    with _model_lock:
        if _model is None:
            _model = WhisperModel(
                WHISPER_MODEL_SIZE,
                device=WHISPER_DEVICE,
                compute_type=WHISPER_COMPUTE_TYPE,
            )
        return _model


@app.get("/health")
def health() -> dict[str, object]:
    return {
        "success": True,
        "service": "vaani-asr",
        "model": WHISPER_MODEL_SIZE,
        "modelLoaded": _model is not None,
    }


VAANI_DOMAIN_PROMPT = (
    "Vaani smart home. Control devices: Prism, Halo, Ember, Nova, "
    "Desk Light, Reading Light, Accent Light, Night Light. "
    "Commands: Turn on, turn off, switch on, switch off, lights on, lights off, "
    "all lights on, all lights off, band karo, chalu karo."
)

WHISPER_HALLUCINATIONS = {
    "thank you",
    "thank you.",
    "thank you very much",
    "thank you very much.",
    "thanks for watching",
    "thanks for watching.",
    "bye",
    "bye.",
    "you",
}


@app.post("/transcribe")
async def transcribe(audio: UploadFile = File(...)) -> dict[str, object]:
    if not audio.filename:
        raise HTTPException(status_code=400, detail="Audio filename is required")

    suffix = Path(audio.filename).suffix or ".m4a"
    audio_bytes = await audio.read(MAX_AUDIO_BYTES + 1)
    if not audio_bytes:
        raise HTTPException(status_code=400, detail="Audio file is empty")
    if len(audio_bytes) > MAX_AUDIO_BYTES:
        raise HTTPException(status_code=413, detail="Audio file exceeds the 15 MB limit")

    temporary_path = ""
    try:
        with tempfile.NamedTemporaryFile(suffix=suffix, delete=False) as temporary_file:
            temporary_file.write(audio_bytes)
            temporary_path = temporary_file.name

        # Serializing inference avoids CPU saturation when several phones speak at once.
        with _transcription_lock:
            segments, info = get_model().transcribe(
                temporary_path,
                language="en",
                initial_prompt=VAANI_DOMAIN_PROMPT,
                vad_filter=True,
                beam_size=3,
            )
            transcript = " ".join(segment.text.strip() for segment in segments).strip()

        # Suppress common Whisper silence hallucinations
        if transcript.lower().strip() in WHISPER_HALLUCINATIONS:
            print(f"[ASR] Filtered silence hallucination: '{transcript}'", flush=True)
            transcript = ""

        print(f"[ASR] Transcribed ({info.language}, prob={info.language_probability:.2f}): '{transcript}'", flush=True)

        return {
            "success": True,
            "transcript": transcript,
            "language": info.language,
            "languageProbability": info.language_probability,
        }
    except Exception as error:
        raise HTTPException(status_code=500, detail=f"Transcription failed: {error}") from error
    finally:
        await audio.close()
        if temporary_path:
            try:
                os.remove(temporary_path)
            except FileNotFoundError:
                pass
