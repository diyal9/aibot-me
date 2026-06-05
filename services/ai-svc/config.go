package main

import (
	"os"

	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

type Config struct {
	LLMModel    string
	LLMAPIURL   string
	LLMAPIKey   string
	STTModel    string
	STTAPIURL   string
	SystemPrompt string
	Port        string
}

func LoadConfig() Config {
	zerolog.TimeFieldFormat = zerolog.TimeFormatUnix
	log.Logger = log.Output(zerolog.ConsoleWriter{Out: os.Stderr})

	// Use values from the original main.go if env vars are not set
	return Config{
		LLMModel:     getEnv("AI_LLM_MODEL", "qwen-turbo"),
		LLMAPIURL:    getEnv("AI_LLM_URL", "https://dashscope.aliyuncs.com/compatible-mode/v1/chat/completions"),
		LLMAPIKey:    getEnv("AI_LLM_API_KEY", "sk-4a2f4f92b72341ea994c7becdf65da7d"),
		STTModel:     getEnv("AI_STT_MODEL", "paraformer-v2"),
		STTAPIURL:    getEnv("AI_STT_URL", "https://dashscope.aliyuncs.com/compatible-mode/v1/audio/transcriptions"),
		SystemPrompt: getEnv("AI_SYSTEM_PROMPT", "你是一个幽默、亲切的AI助手，说话风格轻松自然。回答尽量简短，保持在50字以内。"),
		Port:         getEnv("AI_SERVICE_PORT", "8002"),
	}
}

func getEnv(key, fallback string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return fallback
}

// Mask API key for logging
func (c Config) SafeAPIKey() string {
	if len(c.LLMAPIKey) > 10 {
		return c.LLMAPIKey[:4] + "..." + c.LLMAPIKey[len(c.LLMAPIKey)-4:]
	}
	return "***"
}
