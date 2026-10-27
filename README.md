# Vaani 🎙️💡

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Platform: ESP32](https://img.shields.io/badge/Platform-ESP32-red.svg)](https://www.espressif.com/)
[![Backend: Go](https://img.shields.io/badge/Backend-Go_1.20+-00ADD8.svg)](https://go.dev/)
[![Mobile: Expo / React Native](https://img.shields.io/badge/Mobile-Expo_React_Native-black.svg)](https://expo.dev/)

**Vaani** (meaning *Voice* in Sanskrit/Hindi) is an end-to-end IoT smart lighting automation ecosystem. It connects a cross-platform mobile application to an ESP32 microcontroller through an intermediate Go API gateway, featuring support for remote LED control, device management, custom trigger words, and hardware audio streaming for voice automation.

---

## 🏛️ System Architecture

```mermaid
flowchart LR
    subgraph Client ["Client Layer"]
        App["📱 Vaani Mobile App\n(React Native / Expo)"]
    end

    subgraph Backend ["Backend Gateway"]
        Server["🐹 Go Device Server\n(Port :42069)"]
    end

    subgraph Hardware ["Hardware Layer"]
        ESP["⚡ ESP32 Controller\n(Port :80 / PlatformIO)"]
        LED1["💡 Desk Light (GPIO 18)"]
        LED2["💡 Reading Light (GPIO 19)"]
        LED3["💡 Accent Light (GPIO 21)"]
        LED4["💡 Night Light (GPIO 22)"]
    end

    subgraph AudioPipeline ["Voice / Audio Pipeline (Experimental)"]
        PyServer["🐍 Flask Audio Receiver\n(Port :8000)"]
        RawAudio["🎵 16kHz PCM Audio Stream"]
    end

    App -- "HTTP REST / JSON" --> Server
    Server -- "HTTP / Plain Text POST" --> ESP
    ESP --> LED1
    ESP --> LED2
    ESP --> LED3
    ESP --> LED4

    ESP -. "Raw Audio Stream" .-> PyServer
    PyServer -. "Convert" .-> RawAudio
```

---

## 📁 Repository Structure

```text
Vaani/
├── Vaani/              # ESP32 firmware (PlatformIO / C++)
│   ├── src/
│   │   └── main.cpp    # Wi-Fi setup, WebServer on port 80, GPIO toggling
│   └── platformio.ini  # PlatformIO configuration for NodeMCU-32S
├── server/             # Go REST API gateway & intent parser
│   ├── go.mod
│   ├── main.go         # Device state manager & HTTP router
│   ├── intent.go       # Intent classification & domain resolution
│   ├── fuzzy.go        # Levenshtein distance & text normalization algorithms
│   ├── intent_test.go  # Intent parser unit test suite
│   └── fuzzy_test.go   # Fuzzy logic unit test suite
├── vaani-app/          # Mobile application (Expo / React Native)
│   ├── src/
│   │   ├── api/        # API client for Go backend
│   │   ├── app/        # Expo Router screens (Home, Device details)
│   │   └── constants/  # Theme & UI styling tokens
│   └── package.json
├── test-servers/       # Audio stream testing utilities
│   ├── server.py       # Flask server receiving ESP32 PCM chunks & WAV converter
│   └── voice_client.py # Interactive laptop mic client with Whisper ASR & playback review
├── CHANGELOG.md        # Detailed version history and changes
└── LICENSE             # MIT License
```

---

## 🔌 Hardware Configuration

| Device ID | Default Name | Wake / Trigger Word | GPIO Pin | Initial State |
|:---------:|:-------------|:--------------------|:--------:|:-------------:|
| **1**     | Desk Light   | `Prism`             | GPIO 18  | OFF           |
| **2**     | Reading Light| `Halo`              | GPIO 19  | OFF           |
| **3**     | Accent Light | `Ember`             | GPIO 21  | OFF           |
| **4**     | Night Light  | `Nova`              | GPIO 22  | OFF           |

*Target Microcontroller*: NodeMCU-32S (ESP-WROOM-32).

---

## 🚀 Getting Started

### 1. Flash the ESP32 Firmware

1. Install [PlatformIO](https://platformio.org/) in VS Code or CLI.
2. Open [`Vaani/src/main.cpp`](Vaani/src/main.cpp) and set your Wi-Fi credentials:
   ```cpp
   const char* WIFI_SSID = "YOUR_WIFI_SSID";
   const char* WIFI_PASSWORD = "YOUR_WIFI_PASSWORD";
   ```
3. Connect your ESP32 board via USB.
4. Build and upload:
   ```bash
   cd Vaani
   pio run --target upload
   pio device monitor
   ```
5. Note the assigned ESP32 IP address printed on the serial monitor (e.g., `192.168.1.100`).

---

### 2. Run the Go API Server

1. Update the `esp32URL` in [`server/main.go`](server/main.go) with your ESP32's IP address:
   ```go
   esp32URL = "http://<ESP32-IP>/led"
   ```
2. Run the automated test suite to verify intent parsing:
   ```bash
   cd server
   go test -v ./...
   ```
3. Start the server (compiles `main.go`, `intent.go`, and `fuzzy.go` together):
   ```bash
   go run .
   ```
   *By default, the server runs on port `42069` (override using the `PORT` environment variable).*

---

### 3. Launch the Mobile App

1. Ensure the `API_BASE_URL` in [`vaani-app/src/api/led.ts`](vaani-app/src/api/led.ts) points to your Go server's LAN IP address:
   ```typescript
   const API_BASE_URL = 'http://<YOUR-COMPUTER-LAN-IP>:42069';
   ```
2. Install dependencies and start Expo:
   ```bash
   cd vaani-app
   npm install
   npx expo start
   ```
3. Scan the QR code with **Expo Go** (Android / iOS) or press `w` to open in a web browser.

---

### 4. Voice Testing with Laptop Mic or ESP32 Audio

#### A. Interactive Mic Voice Client (Record $\rightarrow$ Review/Playback $\rightarrow$ Whisper $\rightarrow$ Go Server):
Test spoken commands directly using your laptop microphone with audio review:
```bash
python test-servers/voice_client.py
```
* **Features:**
  - Automatically saves each recording to `test-servers/recorded_cmd.wav`.
  - Press `p` to listen back through your speakers/headphones to verify speech clarity before submitting.
  - Press `s` (or Enter) to transcribe with Whisper and dispatch to the Go intent parser.
  - Press `r` to re-record if background noise interrupted your speech.
  - Or process any existing `.wav` file directly: `python test-servers/voice_client.py --file my_sample.wav`.
  - Add `--execute` to trigger real ESP32 GPIO actions, or leave default for dry-run parsing.

#### B. ESP32 Audio Streaming (Raw PCM Capture & Conversion):
Verify raw audio streaming from an ESP32 microphone:
1. **Start the audio receiver:**
   ```bash
   cd test-servers
   python server.py
   ```
2. **Convert recorded raw PCM to WAV:**
   ```bash
   python server.py --convert
   ```
   *(Or query `GET http://<IP>:8000/convert` while the server is running).*  
   *Output*: `test-servers/audio.wav` (16kHz, 16-bit signed PCM, Mono).

---

## 📡 API Reference

### Go Backend API (`http://localhost:42069`)

- **`GET /health`**
  - Check if the backend gateway is running.
- **`GET /api/devices`**
  - List all configured smart devices with their current states and assigned wake words.
- **`GET /api/devices/:id`**
  - Retrieve details for a specific device (`1`-`4`).
- **`PATCH /api/devices/:id`**
  - Rename a device.
  - Body: `{"name": "New Device Name"}`
- **`POST /api/led`**
  - Toggle light state directly by device ID.
  - Body: `{"device": 1, "action": "ON"}` (Actions: `"ON"` | `"OFF"`)
- **`POST /api/intent`**
  - Parse and execute natural voice commands from transcribed speech using Go + Fuzzy logic.
  - Body:
    ```json
    {
      "text": "turn on prism",
      "execute": true
    }
    ```
  - **Intents Supported**: `TURN_ON`, `TURN_OFF`, `TOGGLE`, `ALL_ON`, `ALL_OFF`, `QUERY_STATUS`.
  - **Safety & Guardrails**:
    - Execution is gated by a minimum confidence score ($\ge 0.70$) to prevent accidental triggers.
    - Word-boundary matching prevents substring false positives (e.g., *"hall"* or *"call"* will not trigger `ALL_ON`).
    - Rhyming collision guards prevent generic word *"light"* from accidentally targeting *"Night Light"*.
  - Response:
    ```json
    {
      "success": true,
      "message": "Parsed intent: TURN_ON (confidence: 1.00)",
      "intent": {
        "intent": "TURN_ON",
        "action": "ON",
        "deviceIds": [1],
        "targetName": "Desk Light",
        "confidence": 1.0,
        "rawText": "turn on prism"
      },
      "executed": true,
      "results": [
        {"device": 1, "name": "Desk Light", "gpio": 18, "state": "ON", "success": true}
      ]
    }
    ```

### ESP32 Direct HTTP Endpoint (`http://<ESP32-IP>:80`)

- **`POST /led`**
  - Direct endpoint called by the Go server.
  - Plaintext body:
    ```text
    device:1
    action:ON
    ```

---

## 📄 License

This project is licensed under the [MIT License](LICENSE) - see the LICENSE file for details.
