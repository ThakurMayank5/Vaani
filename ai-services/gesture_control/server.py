"""Headless ESP32-CAM hand-gesture server for VAANI.

Run from the ai-services directory with:
    python -m gesture_control.server
"""

from http import HTTPStatus
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
import json
import logging
import threading
import time
from typing import Any

import cv2

from . import settings
from .controller import DeviceController
from .detector import GestureDetector, GestureStabilizer


class GestureWorker:
    def __init__(self) -> None:
        self._lock = threading.Lock()
        self._frame_condition = threading.Condition()
        self._stop_event = threading.Event()
        self._thread: threading.Thread | None = None
        self._latest_frame: bytes | None = None
        self._frame_number = 0
        self._status: dict[str, Any] = {
            "running": False,
            "state": "stopped",
            "gesture": "NONE",
            "confidence": 0.0,
            "lastAction": None,
            "lastError": None,
        }

    def status(self) -> dict[str, Any]:
        with self._lock:
            return dict(self._status)

    def wait_for_frame(self, after_frame: int, timeout: float = 10.0) -> tuple[int, bytes] | None:
        """Wait for the next JPEG frame for the browser preview client."""
        deadline = time.monotonic() + timeout
        with self._frame_condition:
            while self._frame_number <= after_frame or self._latest_frame is None:
                remaining = deadline - time.monotonic()
                if remaining <= 0:
                    return None
                self._frame_condition.wait(remaining)
            return self._frame_number, self._latest_frame

    def _publish_frame(self, frame: Any) -> None:
        encoded, jpeg = cv2.imencode(
            ".jpg",
            frame,
            [cv2.IMWRITE_JPEG_QUALITY, settings.PREVIEW_JPEG_QUALITY],
        )
        if not encoded:
            return
        with self._frame_condition:
            self._latest_frame = jpeg.tobytes()
            self._frame_number += 1
            self._frame_condition.notify_all()

    def _open_camera(self) -> cv2.VideoCapture:
        """Open the MJPEG stream with short FFmpeg timeouts for fast recovery."""
        try:
            return cv2.VideoCapture(
                settings.ESP32_CAM_STREAM_URL,
                cv2.CAP_FFMPEG,
                [
                    cv2.CAP_PROP_OPEN_TIMEOUT_MSEC,
                    settings.CAMERA_OPEN_TIMEOUT_MILLISECONDS,
                    cv2.CAP_PROP_READ_TIMEOUT_MSEC,
                    settings.CAMERA_READ_TIMEOUT_MILLISECONDS,
                ],
            )
        except cv2.error as error:
            # Older OpenCV builds can reject FFmpeg parameter lists. Keep a
            # compatible fallback rather than ending the service.
            logging.warning("Could not apply camera timeouts: %s", error)
            return cv2.VideoCapture(settings.ESP32_CAM_STREAM_URL)

    def start(self) -> tuple[bool, str]:
        if not settings.ESP32_CAM_STREAM_URL:
            return False, "ESP32_CAM_STREAM_URL is not configured"
        if self._thread is not None and self._thread.is_alive():
            return False, "Gesture worker is already running"

        self._stop_event.clear()
        with self._lock:
            self._status.update(running=True, state="starting", lastError=None)
        self._thread = threading.Thread(target=self._run, name="gesture-worker", daemon=True)
        self._thread.start()
        return True, "Gesture worker is starting"

    def stop(self) -> tuple[bool, str]:
        if self._thread is None or not self._thread.is_alive():
            return False, "Gesture worker is not running"
        self._stop_event.set()
        return True, "Gesture worker is stopping"

    def _update(self, **values: Any) -> None:
        with self._lock:
            self._status.update(values)

    def _run(self) -> None:
        capture: cv2.VideoCapture | None = None
        try:
            detector = GestureDetector()
            stabilizer = GestureStabilizer()
            controller = DeviceController()
            self._update(state="connecting")

            while not self._stop_event.is_set():
                if capture is None or not capture.isOpened():
                    if capture is not None:
                        capture.release()
                    logging.info("Connecting to ESP32-CAM stream: %s", settings.ESP32_CAM_STREAM_URL)
                    capture = self._open_camera()
                    capture.set(cv2.CAP_PROP_BUFFERSIZE, 1)
                    if not capture.isOpened():
                        self._update(state="waiting_for_camera", lastError="Could not open ESP32-CAM stream")
                        self._stop_event.wait(settings.CAMERA_RETRY_SECONDS)
                        continue
                    self._update(state="processing", lastError=None)

                received, frame = capture.read()
                if not received or frame is None:
                    self._update(state="waiting_for_camera", lastError="ESP32-CAM frame timed out; reconnecting")
                    logging.warning("ESP32-CAM frame timed out; reconnecting")
                    capture.release()
                    capture = None
                    self._stop_event.wait(settings.CAMERA_RETRY_SECONDS)
                    continue

                self._publish_frame(frame)
                try:
                    observation = detector.detect(frame)
                except Exception as error:
                    # Keep the camera preview alive if one inference fails.
                    message = f"Gesture inference failed: {error}"
                    self._update(lastError=message)
                    logging.exception(message)
                    continue
                self._update(
                    state="processing",
                    gesture=observation.gesture,
                    confidence=round(observation.confidence, 4),
                )
                confirmed_gesture = stabilizer.observe(observation)
                if confirmed_gesture is not None:
                    try:
                        result = controller.execute_gesture(confirmed_gesture)
                        self._update(lastAction={"gesture": confirmed_gesture, "result": result, "at": time.time()})
                        logging.info("Gesture %s: %s", confirmed_gesture, result)
                    except Exception as error:  # Keep inference alive when a device is offline.
                        message = str(error)
                        self._update(lastError=message)
                        logging.warning("Gesture command failed: %s", message)

        except Exception as error:
            logging.exception("Gesture worker stopped unexpectedly")
            self._update(state="error", lastError=str(error))
        finally:
            if capture is not None:
                capture.release()
            with self._frame_condition:
                self._frame_condition.notify_all()
            self._update(running=False, state="stopped")


WORKER = GestureWorker()


class APIHandler(BaseHTTPRequestHandler):
    server_version = "VAANIGestureService/1.0"

    def do_OPTIONS(self) -> None:  # noqa: N802
        self._respond(HTTPStatus.NO_CONTENT, None)

    def do_GET(self) -> None:  # noqa: N802
        if self.path == "/":
            self._preview_page()
            return
        if self.path == "/health":
            self._respond(HTTPStatus.OK, {"success": True, "service": "vaani-gesture-control"})
            return
        if self.path == "/status":
            self._respond(HTTPStatus.OK, {"success": True, "worker": WORKER.status()})
            return
        if self.path == "/preview.mjpeg":
            self._preview_stream()
            return
        self._respond(HTTPStatus.NOT_FOUND, {"success": False, "message": "Not found"})

    def do_POST(self) -> None:  # noqa: N802
        if self.path == "/control/start":
            started, message = WORKER.start()
            self._respond(HTTPStatus.ACCEPTED if started else HTTPStatus.CONFLICT, {"success": started, "message": message})
            return
        if self.path == "/control/stop":
            stopped, message = WORKER.stop()
            self._respond(HTTPStatus.ACCEPTED if stopped else HTTPStatus.CONFLICT, {"success": stopped, "message": message})
            return
        self._respond(HTTPStatus.NOT_FOUND, {"success": False, "message": "Not found"})

    def _respond(self, status: HTTPStatus, payload: dict[str, Any] | None) -> None:
        self.send_response(status)
        self.send_header("Access-Control-Allow-Origin", "*")
        self.send_header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
        self.send_header("Access-Control-Allow-Headers", "Content-Type")
        if payload is None:
            self.end_headers()
            return
        encoded = json.dumps(payload).encode("utf-8")
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(encoded)))
        self.end_headers()
        self.wfile.write(encoded)

    def _preview_page(self) -> None:
        page = b"""<!doctype html><html><head><meta name=\"viewport\" content=\"width=device-width,initial-scale=1\"><title>VAANI Camera</title><style>html,body{height:100%;margin:0;background:#111;display:grid;place-items:center}img{max-width:100vw;max-height:100vh;width:auto;height:auto;object-fit:contain}</style></head><body><img src=\"/preview.mjpeg\" alt=\"ESP32-CAM feed\"></body></html>"""
        self.send_response(HTTPStatus.OK)
        self.send_header("Content-Type", "text/html; charset=utf-8")
        self.send_header("Content-Length", str(len(page)))
        self.end_headers()
        self.wfile.write(page)

    def _preview_stream(self) -> None:
        boundary = b"frame"
        self.send_response(HTTPStatus.OK)
        self.send_header("Cache-Control", "no-cache, no-store, must-revalidate")
        self.send_header("Pragma", "no-cache")
        self.send_header("Content-Type", "multipart/x-mixed-replace; boundary=frame")
        self.end_headers()

        frame_number = -1
        try:
            while True:
                next_frame = WORKER.wait_for_frame(frame_number)
                if next_frame is None:
                    continue
                frame_number, jpeg = next_frame
                self.wfile.write(b"--" + boundary + b"\r\n")
                self.wfile.write(b"Content-Type: image/jpeg\r\n")
                self.wfile.write(f"Content-Length: {len(jpeg)}\r\n\r\n".encode("ascii"))
                self.wfile.write(jpeg)
                self.wfile.write(b"\r\n")
                self.wfile.flush()
        except (BrokenPipeError, ConnectionResetError):
            return

    def log_message(self, format: str, *args: Any) -> None:
        logging.info("%s - %s", self.address_string(), format % args)


def main() -> None:
    logging.basicConfig(level=logging.INFO, format="%(asctime)s %(levelname)s %(message)s")
    server = ThreadingHTTPServer((settings.SERVICE_HOST, settings.SERVICE_PORT), APIHandler)
    logging.info("Gesture service listening on http://%s:%d", settings.SERVICE_HOST, settings.SERVICE_PORT)
    logging.info("POST /control/start to begin inference; open http://localhost:%d for the raw camera preview", settings.SERVICE_PORT)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        logging.info("Stopping gesture service")
    finally:
        WORKER.stop()
        server.server_close()


if __name__ == "__main__":
    main()
