# QuantMesh 产品概览

> 当前版本：`3.106.0-rc1`（2026-05-29）

## 主要功能

- 多交易所网格 / 趋势 / 套利策略引擎（spot + perpetual）
- 可视化配置 + 多 Bot 并行管理 + 实时风控
- 回测 / 优化器 + 历史数据导出
- 多 AI 上游（Gemini / OpenAI / 自托管 LLM）的市场解读、参数建议、新闻分析
- WebUI（React + Chakra）+ REST API + WebSocket 推送 + Telegram / 邮件 / Webhook 告警
- **错误自动上报到 17push（通过 aipipe-go-sdk）**
- **内嵌 MCP 服务，可被 Claude Desktop / Cursor / 自研 Agent 直接调用**

## 设计思路

- **单进程多 Bot**：每个 Bot 是 `(exchange, symbol, market_type)` 三元组，独立运行时与配置，统一存储
- **存储优先 SQL**：SQLite 默认，可切 MySQL / Postgres；配置文件作为可导入/导出的源
- **配置即数据**：YAML 与数据库双源，热更新；首次启动会自动迁移
- **WebUI 完全独立于后端**：通过 `/api/*` 通信，i18n 走 react-i18next（**禁止硬编码文案**）
- **可观测性**：
  - 日志写文件 + SQLite，前端可查询
  - Prometheus `/metrics` 端点
  - HTTP 响应统一带 `X-App-Version` / `X-Server-Version` 头
  - 错误自动上报到 17push（用户提供 API Key 后即生效，无 key 时整套静默）

## 当前待办

- [ ] MCP stdio 适配（目前仅 streamable HTTP，部分老 agent 客户端需要 stdio）
- [ ] aipipe 上报范围扩展：策略/下单失败、交易所 WS 异常断连
- [ ] 移动端响应式适配优化

## 集成手册

### 17push 错误上报

1. 注册 [17push.com](https://17push.com) 并生成 API Key
2. 进入「全局设置 → 错误自动上报」，粘贴 Key 并启用
3. 点「测试连接」确认通；之后所有 `logger.Error` / 5xx / panic 会自动上报

### MCP 服务

1. 「全局设置 → MCP 服务」生成 Token
2. 复制下方 JSON（按 agent 类型切换）粘贴到客户端配置文件
3. Agent 即可调用 `qm_list_positions` 等工具；写工具默认关闭，需在 UI 显式开启
