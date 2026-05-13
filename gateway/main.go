package main

import (
	"net/http"
	"os"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

func main() {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	log.Info().Msg("Starting AIBot Gateway...")

	router := NewRouter()

	// Gateway listens on 8085 to replace the old backend directly
	port := os.Getenv("GATEWAY_PORT")
	if port == "" {
		port = "8085"
	}

	log.Info().Str("port", port).Msg("Gateway listening")
	if err := http.ListenAndServe(":"+port, router); err != nil {
		log.Fatal().Err(err).Msg("Gateway failed")
	}
}
