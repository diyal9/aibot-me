package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
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
	STT_MODEL     = "paraformer-v2"
	LLM_API_KEY   = "sk-4a2f4f92b72341ea994c7becdf65da7d"
)

// System prompt for personality (Supertonic Enhanced)
const SYSTEM_PROMPT = `你是一个幽默、亲切的数学 AI 讲师“小星老师”。
你的听众是深圳三年级的小学生。讲课风格要生动、有耐心。
为了让语音听起来像真人，请在回复中巧妙使用以下 Supertonic 语音标签：
- <breath>：表示自然换气或短暂停顿（建议句间使用）
- <laugh>：表示轻松、鼓励或开心的语气（如学生做对题时）
- <sigh>：表示思考或引导（如“嗯...我们来看看..."）

示例：“这道题很简单哦<laugh><breath>我们先看个位<breath>5 加 8 等于 13<breath>写 3 进 1..."
回答请尽量简短，保持在 80 字以内。`

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
	speed := 0.9 // 稍微慢一点，适合教学场景
	if req.Speed > 0 {
		speed = req.Speed
	}

	// Call TTS endpoint (supports both Supertonic WAV/MP3 and Edge TTS MP3)
	ttsPayload := map[string]interface{}{
		"model":           "supertonic",
		"input":           req.Text,
		"voice":           voice,
		"speed":           speed,
		"language":        lang,
	}
	ttsReqJSON, _ := json.Marshal(ttsPayload)

	fmt.Printf("[TTS] Sending request to %s (payload: %s)\n", SUPERTONIC_URL, string(ttsReqJSON))

	ttsClient := &http.Client{Timeout: 30 * time.Second}
	resp, err := ttsClient.Post(SUPERTONIC_URL+"/v1/audio/speech", "application/json", strings.NewReader(string(ttsReqJSON)))
	if err != nil {
		fmt.Printf("[TTS] Supertonic request failed: %v\n", err)
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

	// Return audio directly (WAV or MP3)
	w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
	w.Header().Set("Cache-Control", "no-cache")
	io.Copy(w, resp.Body)
}

// STT handler: Upload → get OSS URL → async transcription → poll
func sttHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method Not Allowed", 405)
		return
	}

	r.ParseMultipartForm(32 << 20)
	file, header, err := r.FormFile("audio")
	if err != nil {
		http.Error(w, "No audio file: "+err.Error(), 400)
		return
	}
	defer file.Close()

	audioData, err := io.ReadAll(file)
	if err != nil {
		http.Error(w, "Failed to read audio: "+err.Error(), 500)
		return
	}

	// Debug: save received audio for inspection
	debugPath := "/tmp/received_stt_debug.wav"
	os.WriteFile(debugPath, audioData, 0644)
	log.Printf("[STT] Received audio: %s, size: %d bytes. Saved to %s", header.Filename, len(audioData), debugPath)

	client := &http.Client{Timeout: 60 * time.Second}
	authHeader := "Bearer " + LLM_API_KEY

	// Step 1: Upload file to DashScope
	var fileBody bytes.Buffer
	fileWriter := multipart.NewWriter(&fileBody)
	part, _ := fileWriter.CreateFormFile("file", header.Filename)
	part.Write(audioData)
	fileWriter.WriteField("purpose", "file-extract")
	fileWriter.Close()

	fileReq, _ := http.NewRequest("POST", "https://dashscope.aliyuncs.com/api/v1/files", &fileBody)
	fileReq.Header.Set("Content-Type", fileWriter.FormDataContentType())
	fileReq.Header.Set("Authorization", authHeader)

	fileResp, err := client.Do(fileReq)
	if err != nil {
		log.Printf("[STT] Upload failed: %v", err)
		http.Error(w, "File upload failed", 500)
		return
	}
	fileRespBody, _ := io.ReadAll(fileResp.Body)
	fileResp.Body.Close()

	if fileResp.StatusCode != 200 {
		log.Printf("[STT] Upload error: %s", string(fileRespBody))
		http.Error(w, "File upload error", 500)
		return
	}

	var uploadResp struct {
		Data struct {
			UploadedFiles []struct {
				FileID string `json:"file_id"`
			} `json:"uploaded_files"`
		} `json:"data"`
	}
	if err := json.Unmarshal(fileRespBody, &uploadResp); err != nil || len(uploadResp.Data.UploadedFiles) == 0 {
		log.Printf("[STT] Upload parse error: %s", string(fileRespBody))
		http.Error(w, "Upload parse error", 500)
		return
	}
	fileID := uploadResp.Data.UploadedFiles[0].FileID
	log.Printf("[STT] File uploaded, file_id: %s", fileID)

	// Step 2: Get file details to retrieve OSS URL (with retry for rate limiting)
	var detailsData []byte
	var detailsStatusCode int
	for attempt := 0; attempt < 3; attempt++ {
		detailsReq, _ := http.NewRequest("GET", "https://dashscope.aliyuncs.com/api/v1/files/"+fileID, nil)
		detailsReq.Header.Set("Authorization", authHeader)

		detailsResp, err := client.Do(detailsReq)
		if err != nil {
			log.Printf("[STT] Get details failed: %v", err)
			http.Error(w, "Get file details failed", 500)
			return
		}
		detailsData, _ = io.ReadAll(detailsResp.Body)
		detailsStatusCode = detailsResp.StatusCode
		detailsResp.Body.Close()

		if detailsStatusCode == 200 {
			break
		}

		// Check if it's a throttling error
		var errResp struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		json.Unmarshal(detailsData, &errResp)
		if errResp.Code == "Throttling.RateQuota" {
			log.Printf("[STT] Throttled on attempt %d, retrying in 2s...", attempt+1)
			time.Sleep(2 * time.Second)
			continue
		}

		log.Printf("[STT] Get details non-200: status=%d body=%s", detailsStatusCode, string(detailsData))
		http.Error(w, "Get file details failed: "+string(detailsData), 500)
		return
	}

	if detailsStatusCode != 200 {
		log.Printf("[STT] Get details failed after retries: status=%d", detailsStatusCode)
		http.Error(w, "Get file details failed after retries", 500)
		return
	}

	var detailsResult struct {
		Data struct {
			URL string `json:"url"`
		} `json:"data"`
	}
	if err := json.Unmarshal(detailsData, &detailsResult); err != nil || detailsResult.Data.URL == "" {
		log.Printf("[STT] Details parse error: status=%d body=%s", detailsStatusCode, string(detailsData))
		http.Error(w, "Get file URL failed", 500)
		return
	}
	fileURL := detailsResult.Data.URL
	log.Printf("[STT] File URL: %s...", fileURL[:80])

	// Step 3: Submit transcription task with OSS URL
	// 强制指定 16k 采样率，确保与前端录制一致
	taskPayload := map[string]interface{}{
		"model": "paraformer-v2",
		"input": map[string]interface{}{
			"file_urls": []string{fileURL},
		},
		"parameters": map[string]interface{}{
			"language_hints": []string{"zh"},
			// 移除 sample_rate，让 DashScope 自动从 WAV 头部检测
		},
	}
	taskJSON, _ := json.Marshal(taskPayload)
	taskReq, _ := http.NewRequest("POST", "https://dashscope.aliyuncs.com/api/v1/services/audio/asr/transcription", bytes.NewBuffer(taskJSON))
	taskReq.Header.Set("Content-Type", "application/json")
	taskReq.Header.Set("Authorization", authHeader)
	taskReq.Header.Set("X-DashScope-Async", "enable")

	taskResp, err := client.Do(taskReq)
	if err != nil {
		log.Printf("[STT] Task submit failed: %v", err)
		http.Error(w, "Task submit failed", 500)
		return
	}
	taskRespBody, _ := io.ReadAll(taskResp.Body)
	taskResp.Body.Close()

	var taskResult struct {
		Output struct {
			TaskID     string `json:"task_id"`
			TaskStatus string `json:"task_status"`
		} `json:"output"`
	}
	if err := json.Unmarshal(taskRespBody, &taskResult); err != nil || taskResult.Output.TaskID == "" {
		log.Printf("[STT] Task error: %s", string(taskRespBody))
		http.Error(w, "Task submit error", 500)
		return
	}
	taskID := taskResult.Output.TaskID
	log.Printf("[STT] Task submitted, task_id: %s", taskID)

	// Step 4: Poll for result (up to 15 times, 3s apart = ~45s max)
	var text string
	var errorCode, errorMsg string
	for i := 0; i < 15; i++ {
		time.Sleep(3 * time.Second)

		pollReq, _ := http.NewRequest("GET", "https://dashscope.aliyuncs.com/api/v1/tasks/"+taskID, nil)
		pollReq.Header.Set("Authorization", authHeader)

		pollResp, err := client.Do(pollReq)
		if err != nil {
			continue
		}
		pollBody, _ := io.ReadAll(pollResp.Body)
		pollResp.Body.Close()

		var pollRes struct {
			Output struct {
				TaskStatus string `json:"task_status"`
				Code       string `json:"code"`
				Message    string `json:"message"`
				Results    []struct {
					Text string `json:"text"`
				} `json:"results"`
			} `json:"output"`
		}
		json.Unmarshal(pollBody, &pollRes)

		log.Printf("[STT] Poll %d: status=%s", i+1, pollRes.Output.TaskStatus)

		if pollRes.Output.TaskStatus == "SUCCEEDED" {
			log.Printf("[STT] SUCCEEDED. Raw response: %s", string(pollBody))
			if len(pollRes.Output.Results) > 0 {
				text = pollRes.Output.Results[0].Text
			}
			break
		} else if pollRes.Output.TaskStatus == "FAILED" {
			errorCode = pollRes.Output.Code
			errorMsg = pollRes.Output.Message
			log.Printf("[STT] Task FAILED: code=%s message=%s", errorCode, errorMsg)
			break
		}
	}

	if text == "" {
		if errorCode != "" {
			// 特殊处理 DashScope 的 "SUCCESS_WITH_NO_VALID_FRAGMENT" 错误
			if errorCode == "SUCCESS_WITH_NO_VALID_FRAGMENT" {
				log.Printf("[STT] No valid speech detected in audio.")
				w.Header().Set("Content-Type", "application/json")
				json.NewEncoder(w).Encode(map[string]interface{}{"text": "", "warning": "未检测到有效语音，请大声一点或靠近麦克风"})
				return
			}
			http.Error(w, "Transcription failed: "+errorCode, 500)
		} else {
			log.Printf("[STT] Empty result. Raw response structure available.")
			// Return the raw DashScope output for debugging
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{"text": "", "warning": "未识别到语音 (DashScope returned empty text)"})
			return
		}
		return
	}

	log.Printf("[STT] Success: %s", text)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{"text": text})
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

