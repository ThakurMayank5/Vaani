"""Adapter from gesture actions to VAANI's existing Go device API."""

from typing import Any

import requests

from . import settings


class DeviceController:
    """Issues device commands only through the Go server, never directly to ESP."""

    def _request(self, method: str, path: str, **kwargs: Any) -> dict[str, Any]:
        response = requests.request(
            method,
            f"{settings.GO_CONTROLLER_URL}{path}",
            timeout=settings.HTTP_TIMEOUT_SECONDS,
            **kwargs,
        )
        try:
            payload = response.json()
        except ValueError as error:
            raise RuntimeError("Go controller returned invalid JSON") from error
        if not response.ok or payload.get("success") is False:
            raise RuntimeError(payload.get("message", f"Go controller returned HTTP {response.status_code}"))
        return payload

    def devices(self) -> list[dict[str, Any]]:
        return self._request("GET", "/api/devices").get("devices", [])

    def set_device(self, device_id: int, action: str) -> dict[str, Any]:
        return self._request("POST", "/api/led", json={"device": device_id, "action": action})

    def toggle_device(self, device_id: int) -> str:
        device = next((item for item in self.devices() if item.get("device") == device_id), None)
        if device is None:
            raise RuntimeError(f"Device {device_id} is not configured by the Go controller")
        action = "OFF" if device.get("state") == "ON" else "ON"
        self.set_device(device_id, action)
        return f"{device.get('name', f'Device {device_id}')} -> {action}"

    def set_all(self, action: str) -> str:
        devices = self.devices()
        failures: list[str] = []
        for device in devices:
            device_id = device.get("device")
            try:
                self.set_device(device_id, action)
            except RuntimeError as error:
                failures.append(f"{device_id}: {error}")
        if failures:
            raise RuntimeError("; ".join(failures))
        return f"All devices -> {action}"

    def execute_gesture(self, gesture: str) -> str:
        action = settings.GESTURE_ACTIONS.get(gesture)
        if action is None:
            return f"No action configured for {gesture}"

        kind = action["kind"]
        if kind == "toggle":
            return self.toggle_device(int(action["device"]))
        if kind == "all_on":
            return self.set_all("ON")
        if kind == "all_off":
            return self.set_all("OFF")
        raise RuntimeError(f"Unsupported configured action: {kind}")
