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
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
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

// Supertonic TTS configuration
const (
	SUPERTONIC_URL = "http://127.0.0.1:7788" // Supertonic local server
)

func ttsHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", 405)
		return
	}

	var req struct {
		Text     string  `json:"text"`
		Voice    string  `json:"voice"`
		Language string  `json:"language"`
		Speed    float64 `json:"speed"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Bad request: "+err.Error(), 400)
		return
	}

	if req.Text == "" {
		http.Error(w, "Empty text", 400)
		return
	}

	voice := "M1"
	if req.Voice != "" {
		voice = req.Voice
	}
	lang := "na" // language-agnostic
	if req.Language != "" {
		lang = req.Language
	}
	speed := 1.0
	if req.Speed > 0 {
		speed = req.Speed
	}

	// Call Supertonic OpenAI-compatible endpoint
	ttsReqBody, _ := json.Marshal(map[string]interface{}{
		"model":           "supertonic",
		"input":           req.Text,
		"voice":           voice,
		"response_format": "wav",
		"speed":           speed,
		"language":        lang,
	})

	ttsReq, err := http.NewRequest("POST", SUPERTONIC_URL+"/v1/audio/speech", bytes.NewReader(ttsReqBody))
	if err != nil {
		// Fallback: return JSON telling client to use speechSynthesis
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "fallback",
			"reason":  "supertonic_unavailable",
			"text":    req.Text,
			"message": "Using client-side TTS",
		})
		return
	}
	ttsReq.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30}
	resp, err := client.Do(ttsReq)
	if err != nil {
		// Fallback
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "fallback",
			"reason":  "supertonic_unavailable",
			"text":    req.Text,
			"message": "Using client-side TTS",
		})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		// Fallback
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"status":  "fallback",
			"reason":  fmt.Sprintf("supertonic_error_%d", resp.StatusCode),
			"text":    req.Text,
			"message": "Using client-side TTS",
		})
		return
	}

	// Return WAV audio directly
	w.Header().Set("Content-Type", "audio/wav")
	w.Header().Set("Cache-Control", "no-cache")
	io.Copy(w, resp.Body)
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
	// 静态文件服务 (指向项目 frontend 目录)
	fs := http.FileServer(http.Dir("/root/aibot-me/frontend/"))
	http.Handle("/", fs)
	
	// API 路由
	http.HandleFunc("/api/chat", corsMiddleware(chatHandler))
	http.HandleFunc("/api/storyboard", corsMiddleware(storyboardHandler))
	http.HandleFunc("/api/blogs", corsMiddleware(blogsHandler))
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
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
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

func storyboardHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", 405)
		return
	}

	var req struct {
		Topic string `json:"topic"`
		Mode  string `json:"mode"` // "math" or "blog"
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), 400)
		return
	}

	topic := req.Topic
	if topic == "" {
		topic = "勾股定理"
	}

	var sysPrompt string
	if req.Mode == "blog" {
		sysPrompt = fmt.Sprintf(`你是 AI 讲师。请为主题 "%s" 生成一个 4 步的演示分镜脚本 (Storyboard)。
严格输出 JSON 数组，不要包含 Markdown 格式。数组包含 4 个对象。
每个对象包含:
- "text": 讲师要说的口语化中文 (简短)。
- "emotion": 情绪 (happy, focus, excited, calm)。
- "svg_action": 黑板动作代码 (draw_line, draw_circle, write_text, highlight)。
- "svg_data": 动作参数 (JSON 字符串，包含坐标/颜色/文字等)。

示例:
[
  {"text": "大家好，今天我们聊聊 %s", "emotion": "happy", "svg_action": "write_text", "svg_data": "{\"text\":\"%s\",\"x\":150,\"y\":50,\"color\":\"#fff\"}"},
  {"text": "核心在于三个支柱...", "emotion": "focus", "svg_action": "draw_line", "svg_data": "{\"x1\":100,\"y1\":100,\"x2\":300,\"y2\":100,\"color\":\"#00f2ff\"}"},
  {"text": "特别是这里...", "emotion": "excited", "svg_action": "highlight", "svg_data": "{\"x\":200,\"y\":100,\"text\":\"重点\"}"},
  {"text": "总结来说...", "emotion": "calm", "svg_action": "draw_circle", "svg_data": "{\"cx\":200,\"cy\":150,\"r\":50,\"color\":\"#ffd700\"}"}
]`, topic, topic, topic)
	} else {
		sysPrompt = fmt.Sprintf(`你是数学辅导老师。请为数学题 "%s" 生成一个 4 步的几何证明分镜脚本。
严格输出 JSON 数组。
每个对象包含:
- "text": 讲解词。
- "emotion": 情绪 (focus, happy, excited)。
- "svg_action": 几何动作 (draw_triangle, draw_square, label_point, highlight_eq, finish)。
- "svg_data": 参数 (JSON 字符串)。

示例:
[
  {"text": "同学们好，我们来看这道题", "emotion": "happy", "svg_action": "write_text", "svg_data": "{\"text\":\"题目\",\"x\":150,\"y\":30,\"color\":\"#fff\"}"},
  {"text": "首先画一个直角三角形", "emotion": "focus", "svg_action": "draw_triangle", "svg_data": "{\"x1\":50,\"y1\":250,\"x2\":200,\"y2\":250,\"x3\":50,\"y3\":100}"},
  {"text": "做辅助线...", "emotion": "focus", "svg_action": "draw_line", "svg_data": "{\"x1\":125,\"y1\":175,\"x2\":200,\"y2\":250,\"color\":\"#ff4444\",\"dashed\":true}"},
  {"text": "证毕！", "emotion": "excited", "svg_action": "highlight", "svg_data": "{\"x\":150,\"y\":280,\"text\":\"a²+b²=c²\",\"color\":\"#00ff88\"}"}
]`, topic)
	}

	reqBody, _ := json.Marshal(map[string]interface{}{
		"model":    "qwen-plus",
		"messages": []map[string]string{
			{"role": "system", "content": sysPrompt},
			{"role": "user", "content": "请生成 JSON"},
		},
	})

	llmReq, _ := http.NewRequest("POST", LLM_API_URL, bytes.NewReader(reqBody))
	llmReq.Header.Set("Content-Type", "application/json")
	llmReq.Header.Set("Authorization", "Bearer "+LLM_API_KEY)

	client := &http.Client{Timeout: 30}
	resp, err := client.Do(llmReq)
	if err != nil {
		http.Error(w, err.Error(), 500)
		return
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var llmResult struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(body, &llmResult); err != nil {
		http.Error(w, "Failed to parse LLM response", 500)
		return
	}

	if len(llmResult.Choices) == 0 {
		http.Error(w, "No content from LLM", 500)
		return
	}

	content := llmResult.Choices[0].Message.Content

	if strings.HasPrefix(content, "```json") {
		content = strings.TrimPrefix(content, "```json")
		content = strings.TrimSuffix(content, "```")
	}
	content = strings.TrimSpace(content)

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(content))
}

// Blog Item structure
type BlogItem struct {
	Title   string    `json:"title"`
	Date    string    `json:"date"`
	Summary string    `json:"summary"`
	RawDate time.Time `json:"-"`
}

// blogsHandler: Reads Markdown files from tcloudblog, extracts frontmatter, returns top 5 newest
func blogsHandler(w http.ResponseWriter, r *http.Request) {
	dir := "/root/aispace/tcloudblog/content/posts/"
	
	files, err := os.ReadDir(dir)
	if err != nil {
		http.Error(w, "Failed to read blog dir: "+err.Error(), 500)
		return
	}

	var blogs []BlogItem
	// Regex for extracting simple YAML frontmatter fields
	titleRe := regexp.MustCompile(`(?m)^title:\s*["']?(.+?)["']?\s*$`)
	dateRe := regexp.MustCompile(`(?m)^date:\s*["']?(.+?)["']?\s*$`)

	for _, f := range files {
		if f.IsDir() || !strings.HasSuffix(f.Name(), ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, f.Name()))
		if err != nil { continue }

		content := string(data)
		
		// Extract Title
		title := f.Name()
		if m := titleRe.FindStringSubmatch(content); len(m) > 1 {
			title = m[1]
		}

		// Extract Date
		dateStr := "1970-01-01"
		if m := dateRe.FindStringSubmatch(content); len(m) > 1 {
			dateStr = m[1]
		}
		// Try parsing date (supports YYYY-MM-DD and YYYY-MM-DDTHH:mm:ss)
		parsedDate, err := time.Parse("2006-01-02", dateStr)
		if err != nil {
			parsedDate, _ = time.Parse("2006-01-02T15:04:05", dateStr)
		}

		// Extract Summary (first 500 chars of text body, strip markdown)
		// Find end of frontmatter (split by ---)
		parts := strings.SplitN(content, "---", 3)
		body := ""
		if len(parts) > 2 {
			body = strings.TrimSpace(parts[2])
		} else {
			body = content // Fallback if no frontmatter
		}
		
		// Remove markdown links/images for cleaner text
		reMd := regexp.MustCompile(`\[(.*?)\]\(.*?\)`)
		body = reMd.ReplaceAllString(body, "$1")
		reImg := regexp.MustCompile(`!\[.*?\]\(.*?\)`)
		body = reImg.ReplaceAllString(body, "")
		reCode := regexp.MustCompile("`[^`]+`")
		body = reCode.ReplaceAllString(body, "")
		
		if len(body) > 600 {
			body = body[:600] + "..."
		}

		blogs = append(blogs, BlogItem{
			Title:   title,
			Date:    dateStr,
			Summary: body,
			RawDate: parsedDate,
		})
	}

	// Sort by date desc (newest first)
	sort.Slice(blogs, func(i, j int) bool {
		return blogs[i].RawDate.After(blogs[j].RawDate)
	})

	// Take top 5
	if len(blogs) > 5 {
		blogs = blogs[:5]
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(blogs)
}

