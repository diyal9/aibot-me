package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/rs/zerolog/log"
)

// ChatRequest defines the expected JSON body from the client
type ChatRequest struct {
	Message string `json:"message"`
}

// ChatHandler proxies the LLM request to DashScope and streams the response back.
func ChatHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		// 1. Decode Client Request
		var req ChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}
		if req.Message == "" {
			http.Error(w, "Message is required", http.StatusBadRequest)
			return
		}

		// 2. Construct DashScope Request
		payload := map[string]interface{}{
			"model": cfg.LLMModel,
			"messages": []map[string]string{
				{"role": "system", "content": cfg.SystemPrompt},
				{"role": "user", "content": req.Message},
			},
			"stream": true,
		}

		payloadBytes, err := json.Marshal(payload)
		if err != nil {
			log.Error().Err(err).Msg("Failed to marshal LLM payload")
			http.Error(w, "Internal Error", http.StatusInternalServerError)
			return
		}

		llmReq, err := http.NewRequest("POST", cfg.LLMAPIURL, strings.NewReader(string(payloadBytes)))
		if err != nil {
			http.Error(w, "Request creation failed", http.StatusInternalServerError)
			return
		}
		llmReq.Header.Set("Content-Type", "application/json")
		llmReq.Header.Set("Authorization", "Bearer "+cfg.LLMAPIKey)

		// 3. Execute LLM Request
		client := &http.Client{}
		resp, err := client.Do(llmReq)
		if err != nil {
			log.Error().Err(err).Msg("LLM API request failed")
			http.Error(w, "LLM API unreachable", http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		// 4. Setup Streaming Response
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Connection", "keep-alive")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "Streaming unsupported", http.StatusInternalServerError)
			return
		}

		// 5. Stream Loop
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data: ") {
				payloadStr := line[6:]
				if payloadStr == "[DONE]" {
					fmt.Fprintf(w, "data: [DONE]\n\n")
					flusher.Flush()
					return
				}

				var chunk struct {
					Choices []struct {
						Delta struct {
							Content string `json:"content"`
						} `json:"delta"`
					} `json:"choices"`
				}
				if err := json.Unmarshal([]byte(payloadStr), &chunk); err == nil {
					if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
						fmt.Fprintf(w, "data: %s\n\n", chunk.Choices[0].Delta.Content)
						flusher.Flush()
					}
				}
			}
		}
	}
}
