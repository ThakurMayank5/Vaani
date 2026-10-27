# Changelog

All notable changes to the **Vaani** project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [Unreleased]

### Added
- **Multi-Device & Compound Intent Engine (`server/intent.go`, `server/fuzzy.go`, `server/main.go`, `server/intent_test.go`, `test-servers/semantic_server.py`)**:
  - **Opposing & Multi-Device Actions**: Upgraded the intent engine to support compound multi-device commands containing distinct or opposing actions in a single utterance (e.g., *"turn off ember turn on halo"* or *"ember off halo on"*).
  - **ASR Phonetic Action Resilience (`server/intent.go`)**: Added phonetic aliases (`"term of"`, `"term off"`, `"tern off"`, `"tern of"`, `"turn of"`, `"terms of"`, `"tune off"`, `"tone off"`) and a fuzzy 2-gram action fallback to [`DetectAction`](file:///c:/Codes/Vaani/server/intent.go#L153) ensuring Whisper transcription slips like *"term of nova"* reliably resolve to `OFF`.
  - **Whisper Raw Acoustic Mode (`test-servers/voice_client.py`)**: Removed synthetic `initial_prompt` conditioning, enabling pure, unconstrained acoustic speech decoding and avoiding decoder hallucination loops.
  - **Clause Window & Token Span Tracking (`server/fuzzy.go`)**: Added `BestFuzzyMatchSpan` tracking exact `[startTok, endTok)` token boundaries for matched devices.
  - **Per-Device Action Scoping & Propagation**:
    - **Prefix actions**: Evaluates leading verbs before the device (*"turn off ember"* $\rightarrow$ `Ember: OFF`).
    - **Inter-device mid-slices**: Accurately splits tokens between consecutive devices into trailing postfix states for the preceding device and prefix verbs for the succeeding device (*"ember off turn on halo"* $\rightarrow$ `Ember: OFF`, `Halo: ON`).
    - **Trailing states & Conjunctions**: Evaluates trailing postfix actions (*"ember and halo off"* $\rightarrow$ both `OFF` via backward propagation; *"turn on ember and halo"* $\rightarrow$ both `ON` via forward propagation).
  - **`MULTI_ACTION` Intent Type**: Added `IntentMulti IntentType = "MULTI_ACTION"`, `Action = "MULTI"`, and `DeviceActions map[int]string` mapping each device ID to its specific target state.
  - **Per-Device Hardware Dispatch (`server/main.go`)**: Updated execution loop to inspect `parsed.DeviceActions[deviceID]` so each targeted light receives its individual state command on ESP32 hardware.
  - **Comprehensive Unit Testing**: Added 7 comprehensive test scenarios in `server/intent_test.go` covering opposing prefix verbs, postfix states, mixed conjunctions, backward/forward action propagation, and alternating 3-device actions.
  - **Voice Client Per-Device Formatting (`test-servers/voice_client.py`)**: Added dedicated display line formatting `Dev <id>: <ACTION>` when compound device actions are present.
- **Hybrid 2-Tier Intent Engine (`server/semantic.go`, `server/main.go`, `test-servers/semantic_server.py`)**:
  - Implemented a 2-tier decision gateway:
    - **Tier 1 (Fast-Path):** Pure Go fuzzy engine handles explicit commands (*"prism on"*, *"all lights off"*) in $<1\text{ms}$ with zero memory overhead.
    - **Tier 2 (Semantic Fallback):** When Tier 1 is unrecognized or confidence $< 0.80$, queries local semantic server (`POST /api/semantic_intent`) to parse indirect or colloquial speech.
  - **Removed Artificial Inflation**: Eliminated the linear remap formula; switched to raw probability + runner-up margin gating ($\text{raw} \ge 0.45$, $\text{margin} \ge 0.15$).
  - **Out-of-Scope Device Guard**: Specific unregistered rooms/appliances (*"bathroom light"*, *"kitchen light"*) are safely rejected rather than widening to all lights.
  - **Confirmation-Before-Action Guard (`requiresConfirmation`)**: Ambient scenes and bedtime routines (*"time for sleep"*, *"it's getting dark"*) return confirmation proposals without executing hardware until verified.
  - **Interactive Confirmation Handshake**: Added `confirmed: bool` to Go `IntentRequest` (`POST /api/intent`) and interactive `👉 Execute this proposal? [y/N]` prompt in `voice_client.py` to allow safe, confirmed execution.
  - **Semantic Polarity Cross-Check (`server/semantic.go`)**: Enforced cross-validation preventing positive illumination phrases from triggering `TURN_OFF`.
  - **Network Security**: Bound `semantic_server.py` strictly to `127.0.0.1`.
  - **CPU Latency Profiling**: Added live `time.perf_counter()` latency reporting per semantic inference.
- **Intent Engine Safety & Priority Patches (`server/intent.go`, `server/intent_test.go`)**:
  - **Candidate Device Priority**: Device matching now executes *before* global room-wide triggers, fixing the critical bug where *"turn on all nigght lights"* erroneously activated all devices instead of Night Light.
  - **Determiner Guard for Prepositional 'On'/'Off'**: Bare `"on"`/`"off"` is only accepted as an action when it is the trailing word or not followed by determiners (`the`, `a`, `my`, `this`, `that`). Prevents phrases like *"put the book on the desk"* from triggering `ON`.
  - **Added Action Verbs**: Explicitly mapped `"illuminate"`, `"illumination"`, `"light up"`, and `"brighten"` to `ON` (Conf: 1.00).
  - **Bedtime Routines**: Added direct `ALL_OFF` routing for smart home sleep triggers (*"time for sleep"*, *"good night"*, *"bedtime"*).
  - **Calibrated Intent Confidence**: Intent confidence now combines action verb confidence and device match similarity ($\min(\text{actionConf}, \text{deviceConf})$).
- **Voice Client Microphone Meter & Gain Calibration (`test-servers/voice_client.py`)**:
  - Raised "Good volume" threshold to $0.12 \le \text{peak} \le 0.85$, adding warning tiers for $<0.03$ (too quiet) and $0.03-0.12$ (low volume).
  - Upgraded percentile gain scaling to $25.0\times$ cap targeting $0.70$ peak amplitude.
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

