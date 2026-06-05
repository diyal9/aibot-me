"""
TTS Server for aibot-me
Provides OpenAI-compatible /v1/audio/speech endpoint.

Strategy:
- Chinese/Japanese/Korean/English -> Edge TTS (returns MP3, Azure-quality)
- Other languages -> Supertonic (returns WAV, local ONNX)
"""
import io
import re
import json
import asyncio
import wave
import numpy as np
from http.server import HTTPServer, BaseHTTPRequestHandler
from socketserver import ThreadingMixIn

from supertonic import TTS
import edge_tts

print("[TTS] Initializing Supertonic...", flush=True)
supertonic = TTS(auto_download=False)
print("[TTS] Supertonic ready.", flush=True)

EDGE_VOICES = {
    'zh': 'zh-CN-YunxiNeural',
    'en': 'en-US-JennyNeural',
    'ja': 'ja-JP-NanamiNeural',
    'ko': 'ko-KR-SunHiNeural',
}


def detect_language(text: str) -> str:
    if re.search(r'[\u4e00-\u9fff]', text):
        return 'zh'
    elif re.search(r'[\u3040-\u309f\u30a0-\u30ff]', text):
        return 'ja'
    elif re.search(r'[\uac00-\ud7af]', text):
        return 'ko'
    return 'en'


def edge_tts_synth(text: str, voice: str = 'zh-CN-YunxiNeural', rate: float = 1.0) -> bytes:
    """Generate MP3 audio via Edge TTS."""
    if rate < 1.0:
        rate_str = f'-{int((1 - rate) * 100)}%'
    elif rate > 1.0:
        rate_str = f'+{int((rate - 1) * 100)}%'
    else:
        rate_str = '+0%'

    async def _gen():
        comm = edge_tts.Communicate(text, voice, rate=rate_str)
        audio = b''
        async for chunk in comm.stream():
            if chunk['type'] == 'audio':
                audio += chunk['data']
        return audio

    loop = asyncio.new_event_loop()
    try:
        return loop.run_until_complete(_gen())
    finally:
        loop.close()


def supertonic_synth(text: str, voice: str = 'M1', language: str = 'na', speed: float = 1.0) -> bytes:
    """Generate WAV audio via Supertonic."""
    style = supertonic.get_voice_style(voice)
    wav, duration = supertonic.synthesize(
        text=text,
        lang=language,
        voice_style=style,
        total_steps=8,
        speed=speed,
    )

    wav_bytes = io.BytesIO()
    with wave.open(wav_bytes, 'wb') as wf:
        wf.setnchannels(1)
        wf.setsampwidth(2)
        wf.setframerate(44100)
        audio_data = (wav.squeeze() * 32767).astype(np.int16).tobytes()
        wf.writeframes(audio_data)
    return wav_bytes.getvalue()


class Handler(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def do_POST(self):
        if self.path in ('/v1/audio/speech', '/v1/tts'):
            try:
                content_length = int(self.headers.get('Content-Length', 0))
                body = self.rfile.read(content_length)
                data = json.loads(body)

                text = data.get('input', '')
                voice = data.get('voice', 'M1')
                language = data.get('language', 'na')
                speed = data.get('speed', 1.0)

                if not text:
                    self.send_error(400, 'Empty input')
                    return

                detected = detect_language(text) if language == 'na' else language

                if detected in ('zh', 'ja', 'ko', 'en'):
                    # Edge TTS -> MP3
                    edge_voice = EDGE_VOICES.get(detected, EDGE_VOICES[detected])
                    print(f"[TTS] Edge TTS ({detected}): '{text[:40]}'", flush=True)
                    audio_data = edge_tts_synth(text, voice=edge_voice, rate=speed)
                    content_type = 'audio/mpeg'
                else:
                    # Supertonic -> WAV
                    print(f"[TTS] Supertonic ({detected}): '{text[:40]}'", flush=True)
                    audio_data = supertonic_synth(text, voice=voice, language=detected, speed=speed)
                    content_type = 'audio/wav'

                self.send_response(200)
                self.send_header('Content-Type', content_type)
                self.send_header('Content-Length', str(len(audio_data)))
                self.end_headers()
                self.wfile.write(audio_data)

            except Exception as e:
                print(f"[TTS] Error: {e}", flush=True)
                self.send_error(500, str(e))
        else:
            self.send_error(404, 'Not found')

    def do_GET(self):
        if self.path == '/health':
            resp = json.dumps({"status": "ok", "tts": "edge+supertonic"}).encode()
            self.send_response(200)
            self.send_header('Content-Type', 'application/json')
            self.send_header('Content-Length', str(len(resp)))
            self.end_headers()
            self.wfile.write(resp)
        else:
            self.send_error(404, 'Not found')

    def log_message(self, format, *args):
        print(f"[TTS] {args[0]}", flush=True)


class Server(ThreadingMixIn, HTTPServer):
    daemon_threads = True


if __name__ == '__main__':
    port = 7788
    server = Server(('0.0.0.0', port), Handler)
    print(f"[TTS] Server running on :{port}", flush=True)
    print(f"[TTS] Endpoints: POST /v1/audio/speech, POST /v1/tts, GET /health", flush=True)
    server.serve_forever()
