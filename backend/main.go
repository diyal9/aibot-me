package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"strings"
)

const (
	LLM_API_URL   = "https://dashscope.aliyuncs.com/compatible-mode/v1/chat/completions"
	STT_API_URL   = "https://dashscope.aliyuncs.com/compatible-mode/v1/audio/transcriptions"
	LLM_MODEL     = "qwen-turbo"
	STT_MODEL     = "paraformer-realtime-v1"
	LLM_API_KEY   = "sk-4a2f4f92b72341ea994c7becdf65da7d"
)

// System prompt for personality
const SYSTEM_PROMPT = `你是一个幽默、亲切的AI助手，说话风格轻松自然。回答尽量简短，保持在50字以内。`

func ttsHandler(w http.ResponseWriter, r *http.Request) {
	// TTS 由前端浏览器 speechSynthesis API 处理
	http.Error(w, "TTS is handled by browser speechSynthesis", 200)
}

// STT handler: receives audio blob, sends to Aliyun DashScope for transcription
func sttHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", 405)
		return
	}

	// Parse multipart form (audio file)
	r.ParseMultipartForm(32 << 20) // 32MB max
	file, header, err := r.FormFile("audio")
	if err != nil {
		http.Error(w, "No audio file: "+err.Error(), 400)
		return
	}
	defer file.Close()

	// Read audio data
	audioData, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "Failed to read audio: "+err.Error(), 500)
		return
	}

	// Create multipart request to DashScope
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", header.Filename)
	if err != nil {
		http.Error(w, "Failed to create form: "+err.Error(), 500)
		return
	}
	part.Write(audioData)

	// Add model field
	writer.WriteField("model", STT_MODEL)
	writer.Close()

	// Send to DashScope STT API
	req, _ := http.NewRequest("POST", STT_API_URL, &body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+LLM_API_KEY)

	client := &http.Client{Timeout: 30}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "STT request failed: "+err.Error(), 500)
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != 200 {
		http.Error(w, fmt.Sprintf("STT API error: %s", string(respBody)), resp.StatusCode)
		return
	}

	// Parse response and extract text
	var result struct {
		Text string `json:"text"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		http.Error(w, "Failed to parse STT response", 500)
		return
	}

	if result.Text == "" {
		http.Error(w, "No speech detected", 200)
		return
	}

	// Return recognized text
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"text": result.Text})
}

func main() {
	// 静态文件服务 (兜底)
	fs := http.FileServer(http.Dir("/usr/share/nginx/"))
	http.Handle("/", fs)
	
	// API 路由
	http.HandleFunc("/api/chat", corsMiddleware(chatHandler))
	http.HandleFunc("/tts", corsMiddleware(ttsHandler))
	http.HandleFunc("/stt", corsMiddleware(sttHandler))

	port := ":8085"
	fmt.Printf("Digital Human LLM Gateway running on %s\n", port)
	if err := http.ListenAndServe(port, nil); err != nil {
		fmt.Println("Server failed:", err)
		os.Exit(1)
	}
}

// CORS 中间件，允许跨域
func corsMiddleware(next http.HandlerFunc) http.HandlerFunc {
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

func chatHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", 405)
		return
	}

	var req struct {
		Message string `json:"message"`
	}
	
	// 解析请求体
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	// 构造 LLM 请求体
	reqBody, _ := json.Marshal(map[string]interface{}{
		"model": LLM_MODEL,
		"messages": []map[string]string{
			{"role": "system", "content": SYSTEM_PROMPT},
			{"role": "user", "content": req.Message},
		},
		"stream": true,
	})

	// 发起请求
	llmReq, _ := http.NewRequest("POST", LLM_API_URL, strings.NewReader(string(reqBody)))
	llmReq.Header.Set("Content-Type", "application/json")
	llmReq.Header.Set("Authorization", "Bearer "+LLM_API_KEY)

	client := &http.Client{}
	resp, err := client.Do(llmReq)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer resp.Body.Close()

	// 设置 SSE 响应头
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported", 500)
		return
	}

	// 逐行转发流
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			payload := line[6:]
			if payload == "[DONE]" {
				fmt.Fprintf(w, "data: [DONE]\n\n")
				flusher.Flush()
				return
			}
			
			// 提取 content 并转发
			var chunk struct {
				Choices []struct {
					Delta struct {
						Content string `json:"content"`
					} `json:"delta"`
				} `json:"choices"`
			}
			if err := json.Unmarshal([]byte(payload), &chunk); err == nil {
				if len(chunk.Choices) > 0 && chunk.Choices[0].Delta.Content != "" {
					fmt.Fprintf(w, "data: %s\n\n", chunk.Choices[0].Delta.Content)
					flusher.Flush()
				}
			}
		}
	}
}
