# AIBot Me - V2 Microservices Architecture

本项目采用自研 Go 网关 + 微服务架构，专为 AI 交互应用设计。

## 🏗️ 架构概览

```
Client (Web/App)
      │
      ▼
┌─────────────────┐
│   Gateway (:8080)│  <-- 自研 Go 网关 (路由/鉴权/流式优化)
└────────┬────────┘
         │
    ┌────┴────┐
    ▼         ▼
┌────────┐ ┌──────────┐ ┌──────────┐ ┌───────────┐
│User Svc│ │ AI Svc   │ │Social Svc│ │Commerce Svc│
│ (:8001)│ │ (:8002)  │ │ (:8003)  │ │  (:8004)   │
└────────┘ └──────────┘ └──────────┘ └───────────┘
```

## 📂 目录结构

```
├── gateway/           # 自研网关：负责路由转发、SSE 优化、JWT 鉴权
├── services/          # 微服务集群
│   ├── user-svc/      # 用户服务：注册/登录/权限
│   ├── ai-svc/        # AI 服务：LLM/STT/TTS 聚合 (原 backend 逻辑迁移至此)
│   ├── social-svc/    # 社交服务：好友/动态/群组
│   └── commerce-svc/  # 商业服务：支付/订阅/商品
├── shared/            # 公共代码：Protobuf, Types, Utils
├── web/               # 前端门户 (Next.js, 待开发)
├── infra/             # Docker 编排与部署脚本
└── backend/           # [Legacy] 旧版单体后端 (保留备份)
```

## 🚀 快速开始

### 1. 环境要求
- Go 1.22+
- Docker & Docker Compose
- Node.js 20+ (前端开发)

### 2. 本地运行 (Gateway + Services)
```bash
# 启动所有基础设施和服务
cd infra/docker
docker compose up -d

# 或者单独运行网关 (开发模式)
cd gateway
go mod tidy
go run .
```

### 3. 关键特性
- **SSE 优化**: AI 网关代理针对 LLM 流式输出进行了缓冲禁用处理。
- **旧版兼容**: `/llm/*` 和 `/stt/*` 路由已配置转发至旧版后端 (`:8085`)，确保平滑迁移。
- **WebSocket**: 原生支持设备控制长连接。

## 📝 开发规范
- 每个微服务独立 `go.mod` (推荐 Go Workspaces)。
- 服务间通信优先使用 gRPC (定义在 `shared/proto`)。
- 外部 API 统一通过 Gateway 暴露。
