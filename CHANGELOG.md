# Changelog

All notable changes to the **Vaani** project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [Unreleased]

### Added
- **Fuzzy Intent Parser Engine (`server/fuzzy.go`, `server/intent.go`)**:
  - Implemented Levenshtein distance algorithm and sliding $n$-gram scoring in pure Go with zero external dependencies.
  - Added text normalization that strips punctuation, collapses whitespace, and handles rune-safe character conversions.
  - Added intent classification for `TURN_ON`, `TURN_OFF`, `TOGGLE`, `QUERY_STATUS`, `ALL_ON`, `ALL_OFF`, and `UNKNOWN`.
  - Added domain phonetic alias tables for common ASR mishearings (*"prison"* $\rightarrow$ *Prism*, *"amber"* $\rightarrow$ *Ember*, *"noah"* $\rightarrow$ *Nova*).
- **Intent Gateway API Endpoint (`server/main.go`)**:
  - Added `POST /api/intent` supporting both dry-run intent inspection (`"execute": false`) and live ESP32 dispatch (`"execute": true`).
  - Added execution confidence gating (refuses hardware dispatch if confidence $< 0.70$).
  - Refactored ESP32 HTTP dispatch into reusable helper `executeESP32Command`.
- **Automated Go Test Suites (`server/fuzzy_test.go`, `server/intent_test.go`)**:
  - Added unit tests for normalization, edit distances, n-gram matching, exact phrases, ASR phonetic drift, global commands, and false-positive rejections.
- **Interactive Voice Client (`test-servers/voice_client.py`)**:
  - Integrated on-device Whisper (`base.en` INT8 via `faster-whisper`) with laptop microphone input.
  - Implemented DC offset removal and 99.5th percentile peak gain normalization with gain capping.
  - Added Whisper hotword conditioning (`hotwords="prism halo ember nova..."`) and natural contextual prompting.
  - Added an **Audio Review & Playback workflow** allowing users to record, listen back through speakers (`p`), re-record (`r`), or submit (`s` / Enter).
  - Added direct audio file transcription mode via `--file <path>`.
- **Repository Cleanliness**:
  - Added root `.gitignore` covering audio recordings (`*.wav`, `*.raw`), Go binaries (`*.exe`), Python bytecode (`__pycache__`), and OS metadata.
  - Added this `CHANGELOG.md`.

### Changed
- **Consolidated Audio Testing Utilities**:
  - Merged `test-servers/convert.py` directly into `test-servers/server.py`.
  - Added `--convert` CLI flag and `GET /convert` HTTP endpoint in `server.py` for raw PCM to WAV conversion.
- **Server Guardrails**:
  - Replaced substring matching with whole-token/word matching to prevent false triggers (e.g., *"hall"* or *"call"* triggering `ALL_ON`).
  - Added support for singular *"light on"* / *"light off"* for room-wide light control.
  - Added descriptive qualifier guard ensuring unknown lights (e.g., *"laptop light"*, *"kitchen light"*) are safely rejected rather than turning on all lights.
  - Added rhyming collision guards preventing generic word *"light"* from fuzzily matching *"night"* (*Night Light*).
  - Added contextual feedback when an action (*"ON"* / *"OFF"*) is spoken without a recognized device.
  - Removed high-risk conversational aliases (*"hello"*, *"know"*, *"yellow"*, *"members"*).
- **Documentation**:
  - Updated root `README.md` with system architecture diagrams, GPIO pin mappings, API references, and testing procedures.

### Removed
- Removed standalone `test-servers/convert.py` (logic merged into `test-servers/server.py`).
- Removed temporary recorded test WAV files from tracking.

