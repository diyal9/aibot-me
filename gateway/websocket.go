package main

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"
)

// DeviceWebSocketHandler 处理设备 WebSocket 连接
// 用于远程控制电脑、IoT 指令下发等场景
func DeviceWebSocketHandler(w http.ResponseWriter, r *http.Request) {
	deviceID := chi.URLParam(r, "id")
	
	log.Info().Str("device_id", deviceID).Msg("WebSocket connection attempt")

	// TODO: 在此处实现 WebSocket 握手
	// 1. 升级连接
	// 2. 鉴权 (从 Query 或 Header 中获取 Token)
	// 3. 建立与 device-svc 的双向通道
	// 4. 转发消息

	// 暂时返回占位响应
	w.WriteHeader(http.StatusSwitchingProtocols)
	w.Write([]byte("WebSocket placeholder - implement with gorilla/websocket"))
}
