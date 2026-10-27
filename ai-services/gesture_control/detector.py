"""YOLO gesture inference and hold-to-confirm logic."""

from dataclasses import dataclass
import time

import numpy as np
from ultralytics import YOLO

from . import settings


@dataclass(frozen=True)
class GestureObservation:
    gesture: str
    confidence: float


class GestureDetector:
    """Runs the supplied YOLO ONNX model against one BGR OpenCV frame."""

    CLASS_MAPPING = {
        "one": "ONE",
        "point": "ONE",
        "peace": "PEACE",
        "three": "THREE",
        "four": "FOUR",
        "palm": "OPEN_PALM",
        "fist": "FIST",
        "grip": "FIST",
        "like": "THUMBS_UP",
        "thumbs_up": "THUMBS_UP",
        "ok": "PINCH_OK",
        "pinch_ok": "PINCH_OK",
    }

    def __init__(self) -> None:
        if not settings.MODEL_PATH.is_file():
            raise FileNotFoundError(f"Gesture model not found: {settings.MODEL_PATH}")
        self._model = YOLO(str(settings.MODEL_PATH), task="detect")

    def detect(self, frame: np.ndarray) -> GestureObservation:
        result = self._model(
            frame,
            imgsz=320,
            conf=settings.MIN_DETECTION_CONFIDENCE,
            verbose=False,
        )[0]
        if result.boxes is None or len(result.boxes) == 0:
            return GestureObservation("NONE", 0.0)

        confidences = result.boxes.conf.cpu().numpy()
        best_index = int(np.argmax(confidences))
        class_id = int(result.boxes.cls[best_index].cpu().numpy())
        raw_name = str(result.names.get(class_id, "")).lower()
        return GestureObservation(
            self.CLASS_MAPPING.get(raw_name, "UNCERTAIN"),
            float(confidences[best_index]),
        )


class GestureStabilizer:
    """Requires a gesture to be held, then latches until it is released."""

    NON_ACTIONABLE_GESTURES = {"NONE", "OPEN_PALM", "UNCERTAIN"}

    def __init__(self) -> None:
        self._candidate = "NONE"
        self._candidate_since = 0.0
        self._latched = False
        self._last_trigger_at = 0.0

    def progress(self) -> float:
        """Returns how close the current candidate is to triggering."""
        if self._candidate in self.NON_ACTIONABLE_GESTURES:
            return 0.0
        held_for = time.monotonic() - self._candidate_since
        return min(1.0, held_for / settings.HOLD_TRIGGER_SECONDS)

    def observe(self, observation: GestureObservation) -> str | None:
        now = time.monotonic()
        gesture = observation.gesture

        if gesture in self.NON_ACTIONABLE_GESTURES:
            self._candidate = gesture
            self._candidate_since = now
            self._latched = False
            return None

        if gesture != self._candidate:
            self._candidate = gesture
            self._candidate_since = now
            self._latched = False
            return None

        held_for = now - self._candidate_since
        cooled_down = now - self._last_trigger_at >= settings.TRIGGER_COOLDOWN_SECONDS
        if (
            not self._latched
            and held_for >= settings.HOLD_TRIGGER_SECONDS
            and cooled_down
        ):
            self._latched = True
            self._last_trigger_at = now
            return gesture
        return None
