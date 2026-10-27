# VAANI Whisper ASR service

This is a local speech-to-text service. It transcribes microphone audio only;
the Go server remains responsible for parsing and executing commands through
`/api/intent`.

## Install and start

From `ai-services`:

```powershell
& "C:\Program Files\Python310\python.exe" -m pip install -r asr\requirements.txt
& "C:\Program Files\Python310\python.exe" -m uvicorn asr.asr:app --host 0.0.0.0 --port 43001
```

The first transcription downloads the configured Whisper `base` model. Change
`WHISPER_MODEL_SIZE` in `asr.py` to use a different local model later.

Endpoints:

- `GET /health`
- `POST /transcribe` as multipart form data, field name `audio`
