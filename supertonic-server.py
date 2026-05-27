"""
Supertonic HTTP Server wrapper for aibot-me
Provides OpenAI-compatible /v1/audio/speech endpoint using supertonic library.
"""
import sys
import io
import json
from http.server import HTTPServer, BaseHTTPRequestHandler
from supertonic import TTS

# Initialize TTS once (downloads model on first call if needed)
tts = TTS(auto_download=True)

class TTSHandler(BaseHTTPRequestHandler):
    def do_POST(self):
        if self.path == '/v1/audio/speech' or self.path == '/v1/tts':
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

            style = tts.get_voice_style(voice_name=voice)

            wav, duration = tts.synthesize(
                text=text,
                lang=language,
                voice_style=style,
                total_steps=8,
                speed=speed,
            )

            # Convert numpy array to WAV bytes
            import numpy as np
            import wave
            wav_bytes = io.BytesIO()
            with wave.open(wav_bytes, 'wb') as wf:
                wf.setnchannels(1)
                wf.setsampwidth(2)  # 16-bit
                wf.setframerate(44100)
                audio_data = (wav.squeeze() * 32767).astype(np.int16).tobytes()
                wf.writeframes(audio_data)

            self.send_response(200)
            self.send_header('Content-Type', 'audio/wav')
            self.end_headers()
            self.wfile.write(wav_bytes.getvalue())
            print(f"TTS: synthesized {len(text)} chars -> {duration[0]:.2f}s audio", flush=True)

        else:
            self.send_error(404, 'Not found')

    def do_GET(self):
        if self.path == '/health':
            self.send_response(200)
            self.send_header('Content-Type', 'application/json')
            self.end_headers()
            self.wfile.write(json.dumps({"status": "ok", "model": "supertonic"}).encode())
        else:
            self.send_error(404, 'Not found')

    def log_message(self, format, *args):
        print(f"[Supertonic] {args[0]}", flush=True)

if __name__ == '__main__':
    port = 7788
    server = HTTPServer(('0.0.0.0', port), TTSHandler)
    print(f"Supertonic HTTP server running on :{port}", flush=True)
    print(f"Endpoints: POST /v1/audio/speech, POST /v1/tts, GET /health", flush=True)
    server.serve_forever()
