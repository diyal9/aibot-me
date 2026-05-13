package main

import (
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/rs/zerolog/log"
)

// NewAIProxy 创建针对 LLM/SSE 优化的反向代理
// 特点：禁用缓冲，直接透传流式数据
func NewAIProxy(target *url.URL) http.Handler {
	proxy := httputil.NewSingleHostReverseProxy(target)

	// Director: 修改请求头以适配目标服务
	proxy.Director = func(req *http.Request) {
		req.Header = req.Header.Clone()
		req.Host = target.Host
		req.URL.Scheme = target.Scheme
		req.URL.Host = target.Host
		
		// 移除可能导致缓冲的头
		req.Header.Del("Accept-Encoding") 
	}

	// ModifyResponse: 确保响应头不被篡改
	proxy.ModifyResponse = func(resp *http.Response) error {
		// 保持 SSE 的 Content-Type
		if resp.Header.Get("Content-Type") == "text/event-stream" {
			resp.Header.Set("X-AI-Proxy", "stream-optimized")
			resp.Header.Del("Content-Encoding") // 禁止压缩，保证实时性
		}
		return nil
	}

	// ErrorHandler
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Error().Err(err).Str("service", "ai-svc").Msg("AI Proxy error")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		w.Write([]byte(`{"error": "AI Service unavailable"}`))
	}

	return proxy
}
