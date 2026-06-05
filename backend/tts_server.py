import os
import subprocess
from fastapi import FastAPI
from fastapi.responses import Response
import uvicorn
import json

app = FastAPI()

@app.post("/v1/audio/speech")
async def tts(request: dict):
    text = request.get("input", "")
    voice = request.get("voice", "M1")
    lang = request.get("language", "na")
    # speed = request.get("speed", 1.0)
    
    if not text:
        return Response(content="Empty text", status_code=400)
    
    # Generate audio using supertonic CLI
    # We save to a temp file and read it back
    output_file = "/tmp/supertonic_out.wav"
    
    # Construct command
    cmd = [
        "supertonic", "tts", text, 
        "-o", output_file, 
        "--voice", voice, 
        "--lang", lang
    ]
    
    # Set PATH to include miniconda
    env = os.environ.copy()
    env["PATH"] = "/opt/miniconda3/bin:" + env["PATH"]
    
    try:
        subprocess.run(cmd, env=env, check=True, timeout=30)
        
        with open(output_file, "rb") as f:
            audio_data = f.read()
            
        # Return WAV
        return Response(content=audio_data, media_type="audio/wav")
    except subprocess.TimeoutExpired:
        return Response(content="TTS Generation Timeout", status_code=504)
    except Exception as e:
        return Response(content=str(e), status_code=500)

@app.get("/health")
async def health():
    return {"status": "ok"}

if __name__ == "__main__":
    uvicorn.run(app, host="127.0.0.1", port=7788)
