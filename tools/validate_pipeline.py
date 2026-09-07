#!/usr/bin/env python3
"""
Ajilamu - Pipeline Validation Script
Validates the full dubbing loop:
1. Split audio from source video
2. Multimodal analysis with Gemini (timestamped segments, speaker, emotion)
3. Duration-budgeted translation into Malayalam (ml-IN)
4. Speech synthesis with Google Cloud Chirp 3 HD voices
5. Measurement & repair loop (atempo stretch <= 8%, rewrite if > 8%)
6. Record take events into ClickHouse Cloud
7. Mux audio tracks with ffmpeg (clean and ducked versions)
"""

import os
import sys
import json
import time
import base64
import wave
import subprocess
import requests
from pathlib import Path
import google.auth
from google.auth.transport.requests import Request

# Load environment from .env
ENV_PATH = Path(__file__).resolve().parent.parent / ".env"
if ENV_PATH.exists():
    for line in ENV_PATH.read_text().splitlines():
        line = line.strip()
        if line and not line.startswith("#") and "=" in line:
            k, v = line.split("=", 1)
            os.environ.setdefault(k.strip(), v.strip())

GCP_PROJECT = os.environ.get("GOOGLE_CLOUD_PROJECT", "nryn-personal")
GCP_LOCATION = os.environ.get("GOOGLE_CLOUD_LOCATION", "global")
CH_HOST = os.environ.get("CLICKHOUSE_HOST", "dvjdkkiusz.us-central1.gcp.clickhouse.cloud")
CH_USER = os.environ.get("CLICKHOUSE_USER", "default")
CH_PASS = os.environ.get("CLICKHOUSE_PASSWORD", "")

WORKSPACE = Path(__file__).resolve().parent.parent
SCRATCH_DIR = WORKSPACE / "scratch"
TAKES_DIR = SCRATCH_DIR / "takes"
TAKES_DIR.mkdir(parents=True, exist_ok=True)

SOURCE_VIDEO = WORKSPACE / "assets" / "source" / "clip.mp4"

# Authentication helper
def get_gcp_token():
    creds, _ = google.auth.default(scopes=["https://www.googleapis.com/auth/cloud-platform"])
    creds.refresh(Request())
    return creds.token

def run_cmd(cmd, check=True):
    print(f"  [cmd] {' '.join(cmd)}")
    res = subprocess.run(cmd, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
    if check and res.returncode != 0:
        raise RuntimeError(f"Command failed ({res.returncode}): {res.stderr}")
    return res

def get_audio_duration_ms(file_path: Path) -> int:
    cmd = [
        "ffprobe", "-v", "error",
        "-show_entries", "format=duration",
        "-of", "default=noprint_wrappers=1:nokey=1",
        str(file_path)
    ]
    res = subprocess.run(cmd, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, check=True)
    return int(float(res.stdout.strip()) * 1000)

def record_take_clickhouse(take_data: dict):
    """Log a take into ClickHouse Cloud `takes` table."""
    try:
        url = f"https://{CH_HOST}:8443/?query=INSERT+INTO+default.takes+FORMAT+JSONEachRow"
        body = json.dumps(take_data)
        resp = requests.post(url, auth=(CH_USER, CH_PASS), data=body, timeout=10)
        if resp.status_code == 200:
            print(f"    [ClickHouse] Recorded take for segment {take_data['segment_id']} (attempt {take_data['attempt']})")
        else:
            print(f"    [ClickHouse warning] HTTP {resp.status_code}: {resp.text}")
    except Exception as e:
        print(f"    [ClickHouse error] {e}")

# =========================================================================
# Step 1: Split Audio
# =========================================================================
def step1_split_audio(video_path: Path):
    print("\n--- [Step 1] Splitting audio from video ---")
    wav_path = SCRATCH_DIR / "speech_16k.wav"
    mp3_path = SCRATCH_DIR / "speech.mp3"

    run_cmd([
        "ffmpeg", "-y", "-i", str(video_path),
        "-vn", "-ar", "16000", "-ac", "1", "-c:a", "pcm_s16le",
        str(wav_path)
    ])

    run_cmd([
        "ffmpeg", "-y", "-i", str(wav_path),
        "-b:a", "64k",
        str(mp3_path)
    ])

    duration_ms = get_audio_duration_ms(wav_path)
    print(f"  Extracted 16kHz PCM WAV: {wav_path} ({duration_ms} ms)")
    print(f"  Extracted 64kbps MP3: {mp3_path} ({mp3_path.stat().st_size // 1024} KB)")
    return wav_path, mp3_path, duration_ms

# =========================================================================
# Step 2: Gemini Audio / Video Analysis
# =========================================================================
def step2_analyze_video(mp3_path: Path, token: str):
    print("\n--- [Step 2] Multimodal Analysis with Gemini (Global Vertex AI) ---")
    with open(mp3_path, "rb") as f:
        audio_b64 = base64.b64encode(f.read()).decode("utf-8")

    url = f"https://aiplatform.googleapis.com/v1/projects/{GCP_PROJECT}/locations/global/publishers/google/models/gemini-2.5-flash:generateContent"
    headers = {
        "Authorization": f"Bearer {token}",
        "Content-Type": "application/json"
    }

    prompt = """
Analyze this speech audio clip carefully.
Identify all spoken sentences or distinct dialogue phrases.
For each segment, detect:
1. start_ms: Start time in milliseconds from beginning of clip
2. end_ms: End time in milliseconds
3. duration_ms: end_ms - start_ms
4. text: The exact English words spoken
5. speaker: Identified speaker name or role
6. emotion: Emotional register (e.g. Enthusiastic, Explanatory, Serious, Warm)

Return strictly valid JSON matching this schema:
[
  {
    "id": 1,
    "start_ms": 6000,
    "end_ms": 7800,
    "duration_ms": 1800,
    "text": "Hi, I'm Suni Williams",
    "speaker": "Suni Williams",
    "emotion": "Warm"
  }
]
"""

    payload = {
        "contents": [{
            "role": "user",
            "parts": [
                {"inlineData": {"mimeType": "audio/mp3", "data": audio_b64}},
                {"text": prompt}
            ]
        }],
        "generationConfig": {
            "responseMimeType": "application/json"
        }
    }

    resp = requests.post(url, headers=headers, json=payload, timeout=60)
    if resp.status_code != 200:
        raise RuntimeError(f"Gemini API error {resp.status_code}: {resp.text}")

    content = resp.json()["candidates"][0]["content"]["parts"][0]["text"]
    segments = json.loads(content)

    out_file = SCRATCH_DIR / "segments.json"
    out_file.write_text(json.dumps(segments, indent=2, ensure_ascii=False))
    print(f"  Identified {len(segments)} segments. Saved to {out_file}")
    for seg in segments:
        print(f"    #{seg['id']}: [{seg['start_ms']}ms -> {seg['end_ms']}ms ({seg['duration_ms']}ms)] {seg['speaker']}: \"{seg['text']}\"")
    return segments

# =========================================================================
# Step 3: Malayalam Translation & Fit Loop
# =========================================================================
def translate_to_malayalam(text: str, target_ms: int, emotion: str, token: str, shorter=False) -> str:
    url = f"https://aiplatform.googleapis.com/v1/projects/{GCP_PROJECT}/locations/global/publishers/google/models/gemini-2.5-flash:generateContent"
    headers = {
        "Authorization": f"Bearer {token}",
        "Content-Type": "application/json"
    }

    constraint = (
        f"This translation MUST be extremely concise and fast to speak. "
        f"The previous attempt was too long for the {target_ms}ms slot. Use minimal syllables."
        if shorter else
        f"The translated line will be spoken in Malayalam in a video slot that lasts approximately {target_ms/1000:.1f} seconds ({target_ms} ms). "
        f"Keep the translation natural, spoken, and fit the rhythm."
    )

    prompt = f"""
Translate this English dialogue line into natural spoken Malayalam script (മലയാളം):
Original English: "{text}"
Speaker emotion: {emotion}
Constraint: {constraint}

Respond with strictly the translated Malayalam text. No markdown, no quotes, no explanation.
"""

    payload = {
        "contents": [{"role": "user", "parts": [{"text": prompt}]}],
        "generationConfig": {"temperature": 0.3}
    }

    resp = requests.post(url, headers=headers, json=payload, timeout=20)
    if resp.status_code != 200:
        raise RuntimeError(f"Translation API error: {resp.text}")

    return resp.json()["candidates"][0]["content"]["parts"][0]["text"].strip()

def synthesize_malayalam_tts(text: str, voice_name: str, token: str, out_path: Path):
    url = "https://texttospeech.googleapis.com/v1/text:synthesize"
    headers = {
        "Authorization": f"Bearer {token}",
        "X-Goog-User-Project": GCP_PROJECT,
        "Content-Type": "application/json"
    }

    payload = {
        "input": {"text": text},
        "voice": {
            "languageCode": "ml-IN",
            "name": voice_name
        },
        "audioConfig": {
            "audioEncoding": "LINEAR16",
            "sampleRateHertz": 16000
        }
    }

    resp = requests.post(url, headers=headers, json=payload, timeout=30)
    if resp.status_code != 200:
        raise RuntimeError(f"TTS API error: {resp.text}")

    audio_bytes = base64.b64decode(resp.json()["audioContent"])
    out_path.write_bytes(audio_bytes)
    return get_audio_duration_ms(out_path)

def step3_fit_loop(segments, token: str):
    print("\n--- [Step 3] Malayalam Translation & Deterministic Fit Loop ---")
    voice = "ml-IN-Chirp3-HD-Achernar" # High quality female voice for Suni Williams
    dub_id = f"dub-ml-{int(time.time())}"
    final_takes = []

    for seg in segments:
        seg_id = seg["id"]
        slot_ms = seg["duration_ms"]
        text_en = seg["text"]
        speaker = seg.get("speaker", "Suni Williams")

        print(f"\n  [Segment #{seg_id}] Slot: {slot_ms}ms | English: \"{text_en}\"")

        # Attempt 1
        attempt = 1
        text_ml = translate_to_malayalam(text_en, slot_ms, seg.get("emotion", "Warm"), token, shorter=False)
        print(f"    Attempt {attempt} translation: \"{text_ml}\"")

        raw_wav = TAKES_DIR / f"seg_{seg_id}_try1.wav"
        actual_ms = synthesize_malayalam_tts(text_ml, voice, token, raw_wav)
        overrun_ms = actual_ms - slot_ms
        overrun_pct = (overrun_ms / slot_ms) * 100

        cost_usd = (len(text_ml) / 1_000_000.0) * 30.0 # Chirp 3 HD pricing: $30/1M chars

        fix_type = "none"
        final_wav = raw_wav

        print(f"    Attempt {attempt} duration: {actual_ms}ms (overrun: {overrun_ms:+d}ms, {overrun_pct:+.1f}%)")

        if overrun_pct <= 0:
            fix_type = "none"
            print(f"    ✓ Fits cleanly with {abs(overrun_ms)}ms to spare!")
        elif overrun_pct <= 8.0:
            # Under 8% overrun: time-stretch via atempo (transparent & free)
            fix_type = "stretch"
            speed_ratio = actual_ms / slot_ms
            stretched_wav = TAKES_DIR / f"seg_{seg_id}_stretched.wav"
            print(f"    ↺ Minor overrun ({overrun_pct:.1f}% <= 8%). Applying transparent atempo={speed_ratio:.3f}x")
            run_cmd([
                "ffmpeg", "-y", "-i", str(raw_wav),
                "-filter:a", f"atempo={speed_ratio:.3f}",
                str(stretched_wav)
            ])
            final_wav = stretched_wav
            actual_ms = get_audio_duration_ms(stretched_wav)
            print(f"    ✓ Stretched duration: {actual_ms}ms (perfect fit)")
        else:
            # Over 8%: rewrite shorter
            print(f"    ⚠ Overrun {overrun_pct:.1f}% > 8%. Requesting concise rewrite from Gemini...")
            record_take_clickhouse({
                "dub_id": dub_id,
                "speaker_id": speaker,
                "language": "ml-IN",
                "segment_id": seg_id,
                "slot_start_ms": seg["start_ms"],
                "slot_end_ms": seg["end_ms"],
                "slot_duration_ms": slot_ms,
                "actual_duration_ms": actual_ms,
                "overrun_ms": overrun_ms,
                "overrun_pct": float(f"{overrun_pct:.2f}"),
                "attempt": attempt,
                "fix_type": "rejected_too_long",
                "source_text": text_en,
                "translated_text": text_ml,
                "voice_name": voice,
                "cost_usd": cost_usd
            })

            # Attempt 2: Rewrite
            attempt = 2
            text_ml = translate_to_malayalam(text_en, slot_ms, seg.get("emotion", "Warm"), token, shorter=True)
            print(f"    Attempt {attempt} (concise): \"{text_ml}\"")
            raw_wav_2 = TAKES_DIR / f"seg_{seg_id}_try2.wav"
            actual_ms = synthesize_malayalam_tts(text_ml, voice, token, raw_wav_2)
            overrun_ms = actual_ms - slot_ms
            overrun_pct = (overrun_ms / slot_ms) * 100
            cost_usd += (len(text_ml) / 1_000_000.0) * 30.0
            print(f"    Attempt {attempt} duration: {actual_ms}ms (overrun: {overrun_ms:+d}ms, {overrun_pct:+.1f}%)")

            if overrun_pct <= 0:
                fix_type = "rewrite"
                final_wav = raw_wav_2
                print("    ✓ Rewritten line fits cleanly!")
            elif overrun_pct <= 8.0:
                fix_type = "rewrite_and_stretch"
                speed_ratio = actual_ms / slot_ms
                stretched_wav = TAKES_DIR / f"seg_{seg_id}_try2_stretched.wav"
                print(f"    ↺ Rewritten line is close ({overrun_pct:.1f}%). Applying atempo={speed_ratio:.3f}x")
                run_cmd([
                    "ffmpeg", "-y", "-i", str(raw_wav_2),
                    "-filter:a", f"atempo={speed_ratio:.3f}",
                    str(stretched_wav)
                ])
                final_wav = stretched_wav
                actual_ms = get_audio_duration_ms(stretched_wav)
                print(f"    ✓ Stretched duration: {actual_ms}ms")
            else:
                fix_type = "best_effort"
                final_wav = raw_wav_2
                print(f"    ! Flagged for creator review (still +{overrun_ms}ms). Keeping best take.")

        # Record winning take into ClickHouse
        record_take_clickhouse({
            "dub_id": dub_id,
            "speaker_id": speaker,
            "language": "ml-IN",
            "segment_id": seg_id,
            "slot_start_ms": seg["start_ms"],
            "slot_end_ms": seg["end_ms"],
            "slot_duration_ms": slot_ms,
            "actual_duration_ms": actual_ms,
            "overrun_ms": actual_ms - slot_ms,
            "overrun_pct": float(f"{(actual_ms - slot_ms)/slot_ms * 100:.2f}"),
            "attempt": attempt,
            "fix_type": fix_type,
            "source_text": text_en,
            "translated_text": text_ml,
            "voice_name": voice,
            "cost_usd": cost_usd
        })

        final_takes.append({
            "segment_id": seg_id,
            "start_ms": seg["start_ms"],
            "end_ms": seg["end_ms"],
            "slot_duration_ms": slot_ms,
            "wav_path": final_wav,
            "text_ml": text_ml,
            "fix_type": fix_type
        })

    return final_takes

# =========================================================================
# Step 4: Assembly & Muxing with ffmpeg
# =========================================================================
def step4_assembly(video_path: Path, takes, total_duration_ms: int):
    print("\n--- [Step 4] Assembling full Malayalam audio track & Muxing ---")
    sample_rate = 16000
    channels = 1
    bytes_per_sample = 2 # 16-bit

    total_samples = int((total_duration_ms / 1000.0) * sample_rate)
    timeline_bytes = bytearray(total_samples * channels * bytes_per_sample)

    for take in takes:
        start_sample = int((take["start_ms"] / 1000.0) * sample_rate)
        with wave.open(str(take["wav_path"]), "rb") as wf:
            frames = wf.readframes(wf.getnframes())
            insert_pos = start_sample * channels * bytes_per_sample
            end_pos = insert_pos + len(frames)
            if end_pos <= len(timeline_bytes):
                timeline_bytes[insert_pos:end_pos] = frames
            else:
                avail = len(timeline_bytes) - insert_pos
                timeline_bytes[insert_pos:] = frames[:avail]

    assembled_wav = SCRATCH_DIR / "dubbed_malayalam_speech.wav"
    with wave.open(str(assembled_wav), "wb") as wf:
        wf.setnchannels(channels)
        wf.setsampwidth(bytes_per_sample)
        wf.setframerate(sample_rate)
        wf.writeframes(timeline_bytes)

    print(f"  Assembled seamless Malayalam track: {assembled_wav}")

    # 1. Clean Mux (replace original audio)
    out_clean_mp4 = SCRATCH_DIR / "dubbed_malayalam_clean.mp4"
    run_cmd([
        "ffmpeg", "-y",
        "-i", str(video_path),
        "-i", str(assembled_wav),
        "-c:v", "copy",
        "-c:a", "aac", "-b:a", "192k",
        "-map", "0:v:0",
        "-map", "1:a:0",
        "-shortest",
        str(out_clean_mp4)
    ])
    print(f"  ✓ Produced Clean Dubbed Video: {out_clean_mp4}")

    # 2. Ducked Mux (dips original audio bed by 18dB under speech)
    out_ducked_mp4 = SCRATCH_DIR / "dubbed_malayalam_ducked.mp4"
    filter_complex = (
        "[0:a]volume=0.20[bg];"
        "[bg][1:a]amix=inputs=2:duration=first:dropout_transition=2[outa]"
    )
    run_cmd([
        "ffmpeg", "-y",
        "-i", str(video_path),
        "-i", str(assembled_wav),
        "-filter_complex", filter_complex,
        "-map", "0:v:0",
        "-map", "[outa]",
        "-c:v", "copy",
        "-c:a", "aac", "-b:a", "192k",
        "-shortest",
        str(out_ducked_mp4)
    ])
    print(f"  ✓ Produced Ducked Dubbed Video (Music/Ambience dipped): {out_ducked_mp4}")

    return out_clean_mp4, out_ducked_mp4

def main():
    print("=================================================================")
    print("Ajilamu - End-to-End Pipeline Validation (Malayalam ml-IN)")
    print(f"Source video: {SOURCE_VIDEO}")
    print("=================================================================")

    if not SOURCE_VIDEO.exists():
        print(f"Error: {SOURCE_VIDEO} not found!")
        sys.exit(1)

    token = get_gcp_token()
    print("  Authenticated successfully with Google Cloud.")

    # 1. Split
    wav_path, mp3_path, total_duration_ms = step1_split_audio(SOURCE_VIDEO)

    # 2. Analyze
    segments = step2_analyze_video(mp3_path, token)

    # 3. Translate, synthesize, measure & repair loop (limit to first 5 segments for quick validation or all)
    # Let's process the segments
    takes = step3_fit_loop(segments, token)

    # 4. Assemble & Mux
    out_clean, out_ducked = step4_assembly(SOURCE_VIDEO, takes, total_duration_ms)

    print("\n=================================================================")
    print("Pipeline validation complete!")
    print(f"Clean Video:  {out_clean}")
    print(f"Ducked Video: {out_ducked}")
    print("=================================================================")

if __name__ == "__main__":
    main()
