@echo off
echo Starting VAANI Whisper ASR service on port 43001...
cd /d "%~dp0ai-services"
"C:\Program Files\Python310\python.exe" -m uvicorn asr.asr:app --host 0.0.0.0 --port 43001
pause
