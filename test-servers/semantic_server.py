#!/usr/bin/env python3
"""
Vaani Semantic Fallback Server (Tier 2 Decision Engine)
======================================================
Provides a local HTTP API (port 42070) implementing "System One" decision parsing
for colloquial, indirect, or complex voice commands (e.g. "it's too dark in here",
"time for sleep", "make it brighter") using Laya (convaiinnovations/laya).

Architecture:
  Voice Client -> Go Server (Tier 1: Fuzzy <1ms)
                     │ (if unrecognized / low confidence)
                     ▼
                 Semantic Server (Tier 2: Laya Decision ~20ms)
"""

import json
import os
import sys
import time
import argparse
from http.server import HTTPServer, BaseHTTPRequestHandler
from socketserver import ThreadingMixIn

# Ensure UTF-8 stdout handling on Windows
if sys.platform == "win32":
    try:
        sys.stdout.reconfigure(encoding="utf-8")
        sys.stderr.reconfigure(encoding="utf-8")
    except Exception:
        pass

PORT = 42070
LAYA_AGENT = None
MODEL_NAME = "convaiinnovations/laya"

# Check if Laya is available
try:
    import laya
    HAS_LAYA = True
except ImportError:
    HAS_LAYA = False


def init_laya_model(model_name: str = MODEL_NAME):
    global LAYA_AGENT
    if not HAS_LAYA:
        print("⚠️  [WARN] 'laya' package is not installed.")
        print("   Run: python -m pip install laya")
        return None

    print(f"⏳ Loading Laya decision model ('{model_name}')...")
    try:
        LAYA_AGENT = laya.load(model_name)
        print("✨ Laya decision model loaded and ready!")
        return LAYA_AGENT
    except Exception as e:
        print(f"❌ Error loading Laya model: {e}")
        return None


def extract_conf(ans_dict: dict, selected_choice: str) -> float:
    if not ans_dict:
        return 0.0
    if "answer_confidence" in ans_dict and ans_dict["answer_confidence"] is not None:
        return float(ans_dict["answer_confidence"])
    if "probability" in ans_dict and ans_dict["probability"] is not None:
        return float(ans_dict["probability"])
    probs = ans_dict.get("probabilities", {})
    if selected_choice in probs:
        return float(probs[selected_choice])
    return 0.0


def resolve_semantic_intent(text: str, devices: list) -> dict:
    """
    Evaluates the utterance using Laya's typed decision primitives (Choice).
    Returns a structured classification with confidence score.
    """
    clean_text = text.strip()
    if not clean_text:
        return {"success": False, "intent": "UNKNOWN", "confidence": 0.0}

    # If Laya is loaded, run decision inference
    if LAYA_AGENT is not None:
        try:
            device_criteria = {}
            for d in devices:
                cw = d.get("custom_word", "")
                name = d.get("name", "")
                key = cw if cw else name
                if key:
                    extra = ""
                    lower_name = name.lower()
                    lower_cw = cw.lower()
                    if "desk" in lower_name or "prism" in lower_cw:
                        extra = ", desk lamp, laptop light, computer light, monitor light, table lamp, work light"
                    elif "reading" in lower_name or "halo" in lower_cw:
                        extra = ", reading light, book light, study lamp"
                    elif "accent" in lower_name or "ember" in lower_cw:
                        extra = ", ambient light, mood light, warm lamp"
                    elif "night" in lower_name or "nova" in lower_cw:
                        extra = ", night lamp, bedside light, sleep lamp"
                    device_criteria[key] = f"{name}, {cw}{extra}"
            
            device_criteria["All Devices"] = "all lights, everything, whole room, every light"
            device_criteria["None"] = "no specific device named, ambient statement"

            questions = {
                "intent": {
                    "type": "choice",
                    "instructions": "What smart home IoT operation does the user want?",
                    "criteria": {
                        "TURN_ON": "turn on, switch on, start, illuminate, brighten, light up, make brighter, dark",
                        "TURN_OFF": "turn off, switch off, shut down, extinguish, darken, make darker",
                        "ALL_ON": "turn all lights on, turn everything on, brighten entire room",
                        "ALL_OFF": "turn off all lights, kill all lights, power off everything, bedtime, sleep, good night",
                        "QUERY_STATUS": "check status, check light state, is the light on or off",
                        "UNKNOWN": "unrelated chatter, gibberish, noise, conversation not requesting action",
                    },
                },
                "target": {
                    "type": "choice",
                    "instructions": "Which device is targeted in this command?",
                    "criteria": device_criteria,
                },
            }

            t_start = time.perf_counter()
            result = LAYA_AGENT.predict({"utterance": clean_text}, questions)
            latency_ms = round((time.perf_counter() - t_start) * 1000, 1)

            answers = result.get("answers", {})

            intent_ans = answers.get("intent", {})
            target_ans = answers.get("target", {})

            selected_intent = intent_ans.get("choice", "UNKNOWN")
            selected_target = target_ans.get("choice", "None")

            intent_raw_conf = extract_conf(intent_ans, selected_intent)
            target_raw_conf = extract_conf(target_ans, selected_target)

            # Extract probability distributions and margins (runner-up gap)
            intent_probs = sorted(intent_ans.get("probabilities", {}).values(), reverse=True)
            intent_margin = round((intent_probs[0] - intent_probs[1]) if len(intent_probs) > 1 else intent_raw_conf, 3)

            target_probs = sorted(target_ans.get("probabilities", {}).values(), reverse=True)
            target_margin = round((target_probs[0] - target_probs[1]) if len(target_probs) > 1 else target_raw_conf, 3)

            # Safety Guard 1: Specific out-of-scope appliances/rooms must NEVER widen to All Devices!
            lower_text = clean_text.lower()
            unknown_appliances = {"bathroom", "kitchen", "hallway", "balcony", "garage", "garden", "tv", "fan", "ac", "heater", "refrigerator"}
            if any(w in lower_text for w in unknown_appliances):
                return {
                    "success": False,
                    "intent": "UNKNOWN",
                    "confidence": 0.20,
                    "model": "laya",
                    "message": "Out-of-scope device: target is not registered in this room",
                    "latencyMs": latency_ms,
                }

            # Safety Guard 2: Raw Probability + Margin gating (No artificial linear inflation)
            is_intent_valid = (intent_raw_conf >= 0.45 and intent_margin >= 0.15) if selected_intent != "UNKNOWN" else False
            is_target_valid = (target_raw_conf >= 0.35 and target_margin >= 0.10) if selected_target not in ("None", "All Devices") else True

            combined_conf = min(intent_raw_conf, target_raw_conf) if selected_target not in ("None", "All Devices") else intent_raw_conf

            # Safety Guard 3: requiresConfirmation for scene / colloquial commands without explicit "all"
            is_scene = any(w in lower_text for w in ["dark", "sleep", "bedtime", "morning", "night", "leaving"])
            has_explicit_global = any(w in lower_text for w in ["all", "every", "everything", "entire", "whole"])
            requires_confirm = is_scene and not has_explicit_global

            # Global command handling
            if (selected_intent in ("ALL_ON", "ALL_OFF") or selected_target == "All Devices") and is_intent_valid:
                action = "ON" if selected_intent in ("ALL_ON", "TURN_ON") else "OFF"
                resolved_intent = "ALL_ON" if action == "ON" else "ALL_OFF"
                dev_ids = [d["device"] for d in devices]
                return {
                    "success": True,
                    "intent": resolved_intent,
                    "action": action,
                    "deviceIds": dev_ids,
                    "targetName": "All Devices",
                    "confidence": round(intent_raw_conf, 2),
                    "matchedOn": f"laya:choice({resolved_intent}) [raw={intent_raw_conf:.2f}, margin={intent_margin:.2f}]",
                    "model": "laya",
                    "requiresConfirmation": requires_confirm,
                    "latencyMs": latency_ms,
                }

            # Map target device
            matched_dev = None
            for d in devices:
                if d.get("custom_word") == selected_target or d.get("name") == selected_target:
                    matched_dev = d
                    break

            if matched_dev and selected_intent in ("TURN_ON", "TURN_OFF", "QUERY_STATUS") and is_intent_valid and is_target_valid:
                action = "ON" if selected_intent == "TURN_ON" else ("OFF" if selected_intent == "TURN_OFF" else "QUERY")
                return {
                    "success": True,
                    "intent": selected_intent,
                    "action": action,
                    "deviceIds": [matched_dev["device"]],
                    "targetName": matched_dev["name"],
                    "confidence": round(combined_conf, 2),
                    "matchedOn": f"laya:choice({selected_intent}/{selected_target}) [raw={combined_conf:.2f}, margin={intent_margin:.2f}]",
                    "model": "laya",
                    "requiresConfirmation": False,
                    "latencyMs": latency_ms,
                }

            return {
                "success": False,
                "intent": selected_intent,
                "confidence": round(intent_raw_conf, 2),
                "model": "laya",
                "latencyMs": latency_ms,
            }
        except Exception as err:
            print(f"❌ Error during Laya inference: {err}")

    # Fallback heuristic if Laya is not installed yet (allows immediate testing)
    lower = clean_text.lower()
    is_brighten = any(w in lower for w in ["dark", "bright", "brighter", "light up", "illuminate", "blind"])
    is_sleep = any(w in lower for w in ["sleep", "good night", "bedtime", "leaving", "goodnight"])

    if is_sleep:
        dev_ids = [d["device"] for d in devices]
        return {
            "success": True,
            "intent": "ALL_OFF",
            "action": "OFF",
            "deviceIds": dev_ids,
            "targetName": "All Devices",
            "confidence": 0.88,
            "matchedOn": "semantic:context(sleep_routine)",
            "model": "semantic-heuristic",
        }

    if is_brighten:
        # Default to primary desk light or all
        target_dev = devices[0] if devices else {"device": 1, "name": "Desk Light"}
        return {
            "success": True,
            "intent": "TURN_ON",
            "action": "ON",
            "deviceIds": [target_dev["device"]],
            "targetName": target_dev["name"],
            "confidence": 0.82,
            "matchedOn": "semantic:context(dark_room)",
            "model": "semantic-heuristic",
        }

    return {
        "success": False,
        "intent": "UNKNOWN",
        "confidence": 0.0,
        "model": "unresolved",
    }


class ThreadingHTTPServer(ThreadingMixIn, HTTPServer):
    daemon_threads = True


class SemanticHandler(BaseHTTPRequestHandler):
    def _send_json(self, status: int, data: dict):
        body = json.dumps(data).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self):
        if self.path == "/health" or self.path == "/api/health":
            self._send_json(200, {
                "status": "ok",
                "laya_installed": HAS_LAYA,
                "model_loaded": LAYA_AGENT is not None,
                "model_name": MODEL_NAME,
            })
            return
        self._send_json(404, {"error": "Not Found"})

    def do_POST(self):
        if self.path != "/api/semantic_intent":
            self._send_json(404, {"error": "Endpoint not found. Use POST /api/semantic_intent"})
            return

        try:
            content_length = int(self.headers.get("Content-Length", 0))
            raw_body = self.rfile.read(content_length).decode("utf-8")
            payload = json.loads(raw_body)
        except Exception as e:
            self._send_json(400, {"success": False, "error": f"Invalid JSON payload: {e}"})
            return

        text = payload.get("text", "")
        devices = payload.get("devices", [])

        result = resolve_semantic_intent(text, devices)
        print(f"🧠 [SEMANTIC] '{text}' -> Intent: {result.get('intent')} (conf: {result.get('confidence')}) [{result.get('model')}]")
        self._send_json(200, result)

    def log_message(self, format, *args):
        # Suppress noisy HTTP access logs
        return


def run_server(port: int = PORT, load_model: bool = True):
    if load_model and HAS_LAYA:
        init_laya_model()

    server = ThreadingHTTPServer(("127.0.0.1", port), SemanticHandler)
    print("=" * 60, flush=True)
    print("🧠 VAANI SEMANTIC FALLBACK SERVER (TIER 2)", flush=True)
    print(f"📡 Listening on: http://localhost:{port}/api/semantic_intent", flush=True)
    print(f"📦 Laya package: {'Available ✅' if HAS_LAYA else 'Not installed (Heuristic mode) ⚠️'}", flush=True)
    print("=" * 60, flush=True)

    try:
        server.serve_forever()
    except KeyboardInterrupt:
        print("\nShutting down semantic server.", flush=True)
        server.server_close()


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Vaani Semantic Fallback Server")
    parser.add_argument("--port", type=int, default=PORT, help="Server port (default: 42070)")
    parser.add_argument("--no-model", action="store_true", help="Skip loading heavy model (heuristic only)")
    args = parser.parse_args()

    run_server(port=args.port, load_model=not args.no_model)
