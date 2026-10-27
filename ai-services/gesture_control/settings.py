"""Configuration for the VAANI hand-gesture service."""

from pathlib import Path


SERVICE_HOST = "0.0.0.0"
SERVICE_PORT = 43000

# Raw frames are available in a browser at http://localhost:43000 while running.
PREVIEW_JPEG_QUALITY = 85

# Change these two IP addresses when your local network addresses change.
ESP32_CAM_IP = "192.168.137.91"
GO_CONTROLLER_IP = "192.168.137.1"

# The ESP32-CAM normally exposes its MJPEG feed at port 81.
ESP32_CAM_STREAM_URL = f"http://{ESP32_CAM_IP}:81/stream"

# The existing Go server remains the only service which sends commands to ESP.
GO_CONTROLLER_URL = f"http://{GO_CONTROLLER_IP}:42069"
HTTP_TIMEOUT_SECONDS = 3.0

MODEL_PATH = Path(__file__).parent / "models" / "yolo_gesture.onnx"
MIN_DETECTION_CONFIDENCE = 0.45
HOLD_TRIGGER_SECONDS = 0.35
TRIGGER_COOLDOWN_SECONDS = 0.6
CAMERA_RETRY_SECONDS = 2.0
CAMERA_OPEN_TIMEOUT_MILLISECONDS = 5_000
CAMERA_READ_TIMEOUT_MILLISECONDS = 5_000

# A gesture must be released (or replaced) after it fires before it can fire again.
# The mapping uses device IDs defined by server/main.go.
GESTURE_ACTIONS = {
    "ONE": {"kind": "toggle", "device": 1, "description": "Toggle Desk Light"},
    "PEACE": {"kind": "toggle", "device": 2, "description": "Toggle Reading Light"},
    "THREE": {"kind": "toggle", "device": 3, "description": "Toggle Accent Light"},
    "FOUR": {"kind": "toggle", "device": 4, "description": "Toggle Night Light"},
    "PINCH_OK": {"kind": "toggle", "device": 1, "description": "Toggle Desk Light"},
    "FIST": {"kind": "all_off", "description": "Turn all devices off"},
    "THUMBS_UP": {"kind": "all_on", "description": "Turn all devices on"},
}
