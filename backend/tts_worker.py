
import sys
import json
from gtts import gTTS
import tempfile
import os

def main():
    if len(sys.argv) < 2:
        print("Usage: tts_worker.py <text> [lang]", file=sys.stderr)
        sys.exit(1)
    
    text = sys.argv[1]
    lang = sys.argv[2] if len(sys.argv) > 2 else 'zh-cn'
    
    try:
        tts = gTTS(text=text, lang=lang)
        temp_mp3 = tempfile.mktemp(suffix='.mp3')
        tts.save(temp_mp3)
        # Output the path to stdout
        print(temp_mp3)
    except Exception as e:
        print(f"Error: {e}", file=sys.stderr)
        sys.exit(1)

if __name__ == "__main__":
    main()
