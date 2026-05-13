package main

import (
	"net/http"
	"net/http/httputil"
	"net/url"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"
)

// NewRouter 初始化路由
func NewRouter() http.Handler {
	r := chi.NewRouter()

	// 全局中间件
	r.Use(RecoveryMiddleware)
	r.Use(LoggingMiddleware)
	r.Use(CORSMiddleware)

	// 健康检查
	r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("OK"))
	})

	// --- 路由定义 ---

	// 1. 用户服务 (User Service)
	// 转发到 localhost:8001
	userTarget, _ := url.Parse("http://localhost:8001")
	r.Handle("/api/user/*", NewProxy(userTarget))

	// 2. AI 服务 (AI Service) - 流式优化
	// 转发到 localhost:8002
	aiTarget, _ := url.Parse("http://localhost:8002")
	r.Handle("/api/ai/*", NewAIProxy(aiTarget))

	// 3. 社交服务 (Social Service)
	// 转发到 localhost:8003
	socialTarget, _ := url.Parse("http://localhost:8003")
	r.Handle("/api/social/*", NewProxy(socialTarget))

	// 4. 商业化服务 (Commerce Service)
	// 转发到 localhost:8004
	commerceTarget, _ := url.Parse("http://localhost:8004")
	r.Handle("/api/commerce/*", NewProxy(commerceTarget))

	// 5. 设备控制 WebSocket
	r.HandleFunc("/ws/device/{id}", DeviceWebSocketHandler)

	// 6. 旧版兼容 (Legacy Support)
	// 将 /llm/* 和 /stt/* 直接转发到旧后端，确保平滑迁移
	legacyTarget, _ := url.Parse("http://localhost:8085")
	r.Handle("/llm/*", NewProxy(legacyTarget))
	r.Handle("/stt/*", NewProxy(legacyTarget))

	return r
}

// NewProxy 创建标准反向代理
func NewProxy(target *url.URL) http.Handler {
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.Director = func(req *http.Request) {
		req.Header = req.Header.Clone()
		req.Host = target.Host
		req.URL.Scheme = target.Scheme
		req.URL.Host = target.Host
		// 保持原始路径，不修改
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		log.Error().Err(err).Str("target", target.String()).Msg("Proxy error")
		http.Error(w, "Service Unavailable", http.StatusBadGateway)
	}
	return proxy
}
