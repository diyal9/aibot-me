package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"

	"github.com/rs/zerolog/log"
)

// STTHandler proxies audio transcription to DashScope.
func STTHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}

		// 1. Parse Audio File
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			http.Error(w, "Parse form failed", http.StatusBadRequest)
			return
		}
		file, header, err := r.FormFile("audio")
		if err != nil {
			http.Error(w, "No audio file", http.StatusBadRequest)
			return
		}
		defer file.Close()

		audioData, err := io.ReadAll(file)
		if err != nil {
			http.Error(w, "Read audio failed", http.StatusInternalServerError)
			return
		}

		// 2. Construct Multipart Request to DashScope
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		part, err := writer.CreateFormFile("file", header.Filename)
		if err != nil {
			http.Error(w, "Create form file failed", http.StatusInternalServerError)
			return
		}
		part.Write(audioData)
		writer.WriteField("model", cfg.STTModel)
		writer.Close()

		req, _ := http.NewRequest("POST", cfg.STTAPIURL, &body)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.Header.Set("Authorization", "Bearer "+cfg.LLMAPIKey)

		// 3. Execute Request
		client := &http.Client{Timeout: 30000000000} // 30s
		resp, err := client.Do(req)
		if err != nil {
			log.Error().Err(err).Msg("STT API request failed")
			http.Error(w, "STT Service Error", http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		respBody, _ := io.ReadAll(resp.Body)

		if resp.StatusCode != http.StatusOK {
			log.Error().Bytes("response", respBody).Msg("STT API non-200 response")
			http.Error(w, fmt.Sprintf("STT API error: %d", resp.StatusCode), resp.StatusCode)
			return
		}

		// 4. Parse & Return Result
		var result struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal(respBody, &result); err != nil {
			http.Error(w, "Parse STT response failed", http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]string{"text": result.Text})
	}
}
