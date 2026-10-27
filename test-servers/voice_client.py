import argparse
import json
import sys
import time
import urllib.error
import urllib.request
from pathlib import Path

import numpy as np
import sounddevice as sd
import soundfile as sf
from faster_whisper import WhisperModel

SAMPLE_RATE = 16000
SERVER_URL = "http://localhost:42069/api/intent"
RECORDING_PATH = Path("test-servers/recorded_cmd.wav")

HOTWORDS = "prism halo ember nova desk reading accent night lights"
NATURAL_PROMPT = "Turn on the desk light. Turn off prism. Reading light on. Halo off."


def record_clean_audio(duration: float = 3.0) -> np.ndarray:
    """
    Record audio with DC offset removal and 99.5th percentile normalization.
    """
    time.sleep(0.20)  # let keyboard press sound die out

    print(f"\n🔴 [RECORDING] Speak your command now... ({duration:.1f}s)")
    raw = sd.rec(
        int(duration * SAMPLE_RATE),
        samplerate=SAMPLE_RATE,
        channels=1,
        dtype="float32",
    )

    steps = int(duration * 10)
    for _ in range(steps):
        time.sleep(0.1)
        sys.stdout.write("█")
        sys.stdout.flush()
    sd.wait()
    print(" ✅ Done!\n")

    flat = raw.flatten()

    # 1. Remove DC offset
    flat = flat - np.mean(flat)

    # 2. Robust percentile normalization
    peak = float(np.percentile(np.abs(flat), 99.5))
    level_bars = min(10, int(peak * 25))
    meter = "█" * level_bars + "░" * (10 - level_bars)

    if peak < 0.03:
        print(f"⚠️  Mic Level: [{meter}] Peak={peak:.3f} (Too quiet! Speak closer or type 'm' to choose mic)")
    elif peak < 0.12:
        print(f"🔉 Mic Level: [{meter}] Peak={peak:.3f} (Low volume — speak louder for reliable transcription)")
    elif peak <= 0.85:
        print(f"🎤 Mic Level: [{meter}] Peak={peak:.3f} (Good volume ✅)")
    else:
        print(f"🔊 Mic Level: [{meter}] Peak={peak:.3f} (Very loud / High gain ⚠️)")

    # 3. Robust percentile gain normalization with capped gain
    if peak > 0.005:
        target_peak = 0.70
        gain = min(target_peak / max(peak, 1e-4), 25.0)
        flat = flat * gain

    flat = np.clip(flat, -1.0, 1.0).astype(np.float32)

    # Save to disk
    RECORDING_PATH.parent.mkdir(parents=True, exist_ok=True)
    sf.write(str(RECORDING_PATH), flat, SAMPLE_RATE)
    print(f"💾 Audio saved to: {RECORDING_PATH.resolve()}")

    return flat


def play_audio(audio: np.ndarray):
    """Play the recorded audio through default speakers/headphones."""
    print("🔊 [PLAYING] Listening to captured audio...")
    sd.play(audio, SAMPLE_RATE)
    sd.wait()
    print("✅ Playback finished.\n")


def send_to_server(text: str, execute: bool = False, confirmed: bool = False):
    """Send transcribed text to the Go intent parser API."""
    payload = {"text": text, "execute": execute}
    if confirmed:
        payload["confirmed"] = True

    data = json.dumps(payload).encode("utf-8")
    req = urllib.request.Request(
        SERVER_URL,
        data=data,
        headers={"Content-Type": "application/json"},
        method="POST",
    )

    try:
        with urllib.request.urlopen(req, timeout=5) as response:
            result = json.loads(response.read().decode("utf-8"))
            return result
    except urllib.error.URLError as e:
        print(f"❌ Could not reach Go server at {SERVER_URL}: {e}")
        print("   Make sure 'go run .' is running in c:\\Codes\\Vaani\\server")
        return None


def print_intent_response(res: dict):
    """Format and print the Go intent parser response."""
    intent_data = res.get("intent", {})
    success = res.get("success", False)

    print("\n" + "=" * 46)
    if success:
        print("🎯 PARSED INTENT:")
        print(f"   • Intent:     {intent_data.get('intent')}")
        print(f"   • Action:     {intent_data.get('action')}")
        print(f"   • Confidence: {intent_data.get('confidence', 0):.2f}")
        print(f"   • Target:     {intent_data.get('targetName')} (IDs: {intent_data.get('deviceIds')})")
        print(f"   • Matched On: {intent_data.get('matchedOn')}")
        print(f"   • Executed:   {res.get('executed')}")
        if res.get("requiresConfirmation") or intent_data.get("requiresConfirmation"):
            print(f"   ⚠️  CONFIRM:  {res.get('message')}")
        if res.get("results"):
            print("   • Hardware Status:")
            for r in res.get("results"):
                status = "✅ OK" if r.get("success") else f"❌ Error: {r.get('error')}"
                print(f"     - [{r.get('name')}] GPIO {r.get('gpio')} -> {status}")
    else:
        print(f"❓ UNRECOGNIZED: {res.get('message')}")
    print("=" * 46)


def process_audio_file(model: WhisperModel, audio: np.ndarray, execute: bool):
    """Transcribe audio and send intent to Go server."""
    print("⏳ Transcribing audio with Whisper...")
    start_t = time.time()
    segments, info = model.transcribe(
        audio,
        language="en",
        hotwords=HOTWORDS,
        initial_prompt=NATURAL_PROMPT,
        beam_size=5,
        temperature=0.0,
        condition_on_previous_text=False,
        vad_filter=True,
        vad_parameters=dict(
            threshold=0.35,
            min_silence_duration_ms=500,
            speech_pad_ms=300,
        ),
    )
    seg_list = list(segments)
    elapsed = time.time() - start_t

    if not seg_list or max(s.no_speech_prob for s in seg_list) > 0.65:
        print("⚠️  No clear speech detected in the audio.")
        return

    transcript = " ".join([seg.text.strip() for seg in seg_list]).strip()

    if not transcript:
        print("⚠️  Empty transcription.")
        return

    print(f"🗣️  Transcribed ({elapsed:.2f}s): \"{transcript}\"")

    # Send to Go server
    print("🚀 Sending to Go Intent Parser...")
    res = send_to_server(transcript, execute=execute)
    if res:
        print_intent_response(res)
        if res.get("requiresConfirmation") and execute:
            ans = input("👉 Execute this proposal? [y/N]: ").strip().lower()
            if ans in ("y", "yes"):
                print("🚀 Sending confirmed execution...")
                res_conf = send_to_server(transcript, execute=True, confirmed=True)
                if res_conf:
                    print_intent_response(res_conf)


def main():
    parser = argparse.ArgumentParser(description="Vaani Voice Client with Audio Review")
    parser.add_argument("--duration", type=float, default=3.0, help="Recording duration in seconds (default: 3.0s)")
    parser.add_argument("--execute", action="store_true", help="Send hardware commands to ESP32 (default: dry-run)")
    parser.add_argument("--model", type=str, default="base.en", help="Whisper model size (default: base.en)")
    parser.add_argument("--file", type=str, default=None, help="Process an existing audio .wav file directly")
    args = parser.parse_args()

    print("=" * 60)
    print("🎙️  VAANI VOICE CLIENT (AUDIO REVIEW & PLAYBACK)")
    print("=" * 60)
    print(f"Loading Whisper model '{args.model}' (INT8 CPU)...")
    model = WhisperModel(args.model, device="cpu", compute_type="int8")
    print("✨ Model ready!")
    print(f"📡 Target API: {SERVER_URL} (execute={args.execute})")
    print("=" * 60)

    # Direct file mode
    if args.file:
        file_path = Path(args.file)
        if not file_path.exists():
            print(f"❌ File not found: {file_path}")
            return
        print(f"📂 Loading audio file: {file_path}")
        audio, sr = sf.read(str(file_path))
        if sr != SAMPLE_RATE:
            print(f"⚠️  Note: Audio sample rate is {sr}Hz (expected {SAMPLE_RATE}Hz)")
        process_audio_file(model, audio.flatten().astype(np.float32), args.execute)
        return

    # Interactive recording with review loop
    while True:
        try:
            print("\nOptions:")
            print("  [Enter] 🎙️  Record new audio")
            print("  't: <text>' ⌨️  Type command directly")
            print("  'q' 🚪 Quit")
            choice = input("👉 Your choice: ").strip()

            if choice.lower() == "q":
                print("Exiting. Bye!")
                break

            if choice.lower().startswith("t:"):
                text = choice[2:].strip()
                print(f"⌨️  Using text input: \"{text}\"")
                print("🚀 Sending to Go Intent Parser...")
                res = send_to_server(text, execute=args.execute)
                if res:
                    print_intent_response(res)
                continue

            # Record
            audio = record_clean_audio(args.duration)

            # Review & Decide Loop
            while True:
                print("\nReview your recording:")
                print("  [Enter] / 's' 👉 Submit to Whisper & Go server")
                print("  'p'           🔊 Play audio back through speakers")
                print("  'r'           🔄 Re-record (discard this audio)")
                print("  'q'           🚪 Cancel & Quit")
                decision = input("👉 What would you like to do? ").strip().lower()

                if decision in ("", "s", "y", "yes"):
                    process_audio_file(model, audio, args.execute)
                    break
                elif decision == "p":
                    play_audio(audio)
                elif decision == "r":
                    print("🔄 Discarding recording. Let's record again.")
                    break
                elif decision == "q":
                    print("Exiting. Bye!")
                    return
                else:
                    print("Invalid choice. Enter 's' to submit, 'p' to play, 'r' to re-record, 'q' to quit.")

        except KeyboardInterrupt:
            print("\nExiting. Bye!")
            break


if __name__ == "__main__":
    main()