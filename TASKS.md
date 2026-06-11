# v2-refactoring Tasks

## Phase 1: 基础设施与质量门禁
- [x] 创建 `scripts/verify.sh` 自动化检查脚本。
- [x] 修复 Supertonic HTTP 响应头（`Connection: close`）。
- [x] 统一前端入口逻辑 (`index.html`)。
- [x] 验证脚本通过 (5/5 Checks Passed)。

## Phase 2: 递进式加载与 TTS 同步引擎
- [ ] 优化 `LoadingOverlay` 骨架屏视觉。
- [ ] 确保 Three.js/VRM 加载不阻塞 UI。
- [ ] 新增 `Thinking` 状态机，填补 TTS 延迟空白。
- [ ] TTS 请求期间强制显示 Thinking 状态。

## Phase 3: 数学黑板重构 (核心体验)
- [ ] 重写 `MathController`: 分步触发。
- [ ] 实现 `SyncEngine`: 语音与绘图同步。

## Phase 4: 博客阅读模式
- [ ] 沉浸式阅读 UI。
- [ ] LLM 摘要集成。

## Phase 5: 全链路验收与部署
- [ ] 运行 `verify.sh` 生成 QA 报告。
- [ ] 部署至生产环境。
