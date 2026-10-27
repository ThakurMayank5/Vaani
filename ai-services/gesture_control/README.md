# VAANI gesture-control service

This service reads an ESP32-CAM MJPEG stream, detects gestures with the copied
`models/yolo_gesture.onnx` model, then sends commands to VAANI's existing Go
server. It never sends commands directly to the ESP, so the mobile app receives
the same updated device state as a manual toggle.

## Configure

Edit the two IP constants at the top of `settings.py`:

```python
ESP32_CAM_IP = "192.168.1.150"
GO_CONTROLLER_IP = "10.104.200.21"
```

`ESP32_CAM_IP` must be the IP printed by the ESP32-CAM firmware. The service
builds its `/stream` URL automatically. `GO_CONTROLLER_IP` is the computer
running VAANI's Go server.

## Start

Install the dependencies from `requirements.txt`, then, from `ai-services`, run:

```powershell
python -m gesture_control.server
```

The API intentionally keeps camera inference stopped until explicitly enabled:

```powershell
Invoke-RestMethod -Method Post http://localhost:43000/control/start
```

Use `GET http://localhost:43000/status` to view the latest detected gesture,
action, and errors. Stop it with `POST /control/stop`.

## Camera preview

Open [http://localhost:43000](http://localhost:43000) in a browser while the
gesture worker is running. It shows only the raw ESP32-CAM feed at the largest
size that fits the browser window—there are no labels, boxes, or overlays.

Use `GET /status` separately to see the gesture currently recognized and any
device-command errors.

If the ESP32-CAM stream drops, the service waits at most five seconds for a
frame, releases the connection, and retries automatically. While the camera is
unavailable, gesture control is paused; check the camera's Wi-Fi signal, power,
and stream firmware if dropouts continue.

## Gesture bindings

| Gesture | VAANI action |
| --- | --- |
| One finger | Toggle Desk Light |
| Peace | Toggle Reading Light |
| Three fingers | Toggle Accent Light |
| Four fingers | Toggle Night Light |
| Pinch / OK | Toggle Desk Light |
| Fist | Turn all devices off |
| Thumbs up | Turn all devices on |

Gesture bindings, camera settings, detection threshold, and hold/cooldown timings
are centralized in `settings.py`.
