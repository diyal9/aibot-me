package main

import (
	"net/http"
	"net/url"

	"github.com/go-chi/chi/v5"
)

// NewRouter initializes the Gateway routing table
func NewRouter() *chi.Mux {
	r := chi.NewRouter()

	// Global Middleware
	r.Use(RecoveryMiddleware)
	r.Use(LoggingMiddleware)
	r.Use(CORSMiddleware)

	// Health Check
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("OK"))
	})

	// --- AI Service Routing ---
	// The new AI Service runs on port 8002
	aiTarget, _ := url.Parse("http://localhost:8002")
	
	// Routes: /api/chat, /stt, /llm/chat
	r.Handle("/api/chat*", NewAIProxy(aiTarget))
	r.Handle("/stt*", NewAIProxy(aiTarget))
	r.Handle("/llm*", NewAIProxy(aiTarget))

	// Note: We removed generic legacy routes. 
	// Specific routes for User/Social/Commerce would be added here as those services come online.

	return r
}
