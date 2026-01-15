# Changelog

所有重要的项目更新都会记录在这个文件中。

## [Unreleased]

### Added
- **内置异步 AI 任务系统**: 集成完整的异步 AI 调用能力，无需依赖外部 `go-gemini-proxy` 服务
  - 新增 `AsyncTask` 模型支持异步任务持久化（支持 SQLite/PostgreSQL/MySQL）
  - 新增 `TaskService` 提供任务 CRUD 操作
  - 新增 `AIService` 封装 Gemini API 直接调用
  - 新增 `TaskProcessor` 后台任务处理器，支持并发控制和重试机制
- **新手风险检查 API**: 新增 `/api/risk/newbie-check` 接口，提供配置安全性评估

### Changed
- **统一 AI 访问方式**: 移除 `native`/`proxy` 访问模式选择，统一使用内置异步系统
  - `GeminiClient` 重构为 `AsyncGeminiClient`，内部自动处理任务创建和轮询
  - 配置文件移除 `access_mode` 和 `proxy` 相关配置项
  - 前端移除 AI 访问方式选择 UI 和代理配置表单
- **简化 API 接口**: `/api/ai/generate-config` 接口移除访问模式和代理相关参数

### Security
- **API Token 保护**: AI 调用完全在本地完成，敏感的 API Key 不再发送到外部服务

### Removed
- 移除对外部 `gemini.facev.app` 代理服务的依赖
- 移除 `ProxyGeminiClient` 和 `NativeGeminiClient` 双模式实现
- 移除配置中的 `ai.access_mode`、`ai.proxy.base_url`、`ai.proxy.username`、`ai.proxy.password` 字段
