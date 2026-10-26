
from flask import Flask, request
from pathlib import Path

app = Flask(__name__)

OUTPUT_DIR = Path("test-servers")
OUTPUT_DIR.mkdir(parents=True, exist_ok=True)

RAW_FILE = OUTPUT_DIR / "audio_chunk.raw"

# Start each test with an empty recording.
RAW_FILE.write_bytes(b"")


@app.post("/audio")
def audio():
    data = request.get_data()

    if not data:
        return "Empty audio chunk", 400

    with RAW_FILE.open("ab") as f:
        f.write(data)

    print(
        f"Received: {len(data)} bytes | "
        f"Total: {RAW_FILE.stat().st_size} bytes"
    )

    return "OK", 200


if __name__ == "__main__":
    app.run(
        host="0.0.0.0",
        port=8000,
        debug=False,
        threaded=True
    )
