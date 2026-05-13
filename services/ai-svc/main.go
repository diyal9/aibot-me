package main

import (
	"net/http"

	"github.com/rs/zerolog/log"
)

func main() {
	cfg := LoadConfig()
	log.Info().Str("port", cfg.Port).Str("model", cfg.LLMModel).Msg("Starting AI Service")

	mux := http.NewServeMux()

	// CORS Middleware wrapper
	cors := func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusOK)
				return
			}
			next(w, r)
		}
	}

	// Routes
	// /api/chat matches the frontend's original call
	mux.HandleFunc("/api/chat", cors(ChatHandler(cfg)))
	mux.HandleFunc("/stt", cors(STTHandler(cfg)))
	
	// /llm/chat matches the Nginx proxy path if it rewrites /llm/ -> /api/chat or similar
	// We handle both to be safe
	mux.HandleFunc("/llm/chat", cors(ChatHandler(cfg)))

	// Health check
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("OK"))
	})

	log.Info().Msg("AI Service ready")
	if err := http.ListenAndServe(":"+cfg.Port, mux); err != nil {
		log.Fatal().Err(err).Msg("AI Service failed")
	}
}
