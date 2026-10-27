#!/usr/bin/env bash
cd "$(dirname "$0")/.." || exit 1
python -m uvicorn asr.asr:app --host 0.0.0.0 --port 43001
