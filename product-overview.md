# QuantMesh 产品概览

> 当前版本：`3.111.0-rc1188`（2026-10-13，开发中，未发布）

RC1187 将新 FundingRecovery outbox 的 `ExpectedRuntimeRevision` 纳入投影哈希，并在 receipt/outbox 配对和生产 runtime CAS 前交叉核对。旧格式 payload 仍可读作审计证据，但因缺少该 revision 绑定而禁止自动投影/重放，转人工核账。RC1188 修正 MySQL outbox 测试将十六进制哈希误 CAST 为 JSON 的夹具错误；生产校验逻辑不变，MySQL 与完整 CI 仍须验收。

FundingCarry 管理员 Binance funding-income 核账现支持 durable receipt/outbox 重试；投影失败仅允许精确 operation 的 pending receipt/outbox 恢复；游标推进后重试旧 operation 按 outbox 目标版本校验并返回自身历史 cursor snapshot，不重新查询来源、不修改当前 cursor。生产 service/router 故障注入回归覆盖 pending 恢复、CAS 已写后响应丢失、新 runtime generation 显式 takeover、连续窗口推进及旧操作重放，并确认 Gate/hold 保持。该 restart simulation 使用同一隔离 SQLite StorageService；真实 OS 进程重启、独立多实例 storage handles、MySQL、同 SHA CI 尚待验收。RC1185 新增耐久历史异常记录和管理员追加式 funding adjustment API：原始行不覆盖，迟到/改写/删除差异保存精确差额及审计依据；调整经 operation/CAS 与 receipt/outbox 持久投影，之后须完整来源回扫一致才推进 cursor/标记 verified，hold 仍须独立显式解锁。MySQL、多进程及同 SHA CI 尚待验收。零金额行可留作来源事实，不代表收益；双扫描摘要并非交易所独立签名证明，因此来源完整性仍未闭环。另修复管理员期初 snapshot 时间早于录入时间被错误拒绝的问题。

RC1166 本地集成测试覆盖隔离 SQLite StorageService、生产核账 service/router、显式 owner takeover、原 client order ID venue 查询、attempt 与 provisional child 同步 CAS 绑定及 hold 保持。RC1171 增加经 SymbolManager/BotManager 和 FundingCarry 真实构造函数建立并登记 retained recovery runtime，再由管理员 HTTP 执行 takeover/found 的模拟组合测试；策略循环、订单重放、账务投影和 Gate 解锁均保持关闭。venue 为测试 fixture，MySQL、真实多进程、费用/借贷入账及持久 Gate 解锁仍待验证。

历史阶段里程碑（RC1173）：在 RC1172 的 fill/cursor 基础上，为完整权威 fill 中 commission unknown 的交易增加管理员人工 fee adjustment API。费用记录以 append-only 独立 event 保存，要求准确匹配 episode/child/trade、source revision 和 CAS revision，原始签名 decimal 与币种不折算；凭证以 SHA-256 摘要绑定审计记录，actor 由管理员 session 注入。durable receipt/outbox 与 retained runtime CAS 读回支持响应丢失重试和幂等重复请求；测试中 Gate/hold 保持。正数为新增费用、负数为更正/返佣；人工输入和外部凭证摘要不构成交易所自动核验。

RC1173 阶段限制记录：当时的 permit、Gate 来源约束、recovery outbox 与 takeover 基础能力仍受此前验收边界约束；RC1173 只增加受限人工费用记录能力，不提供借贷/还款/利息/资金费账本、跨币估值、持久 Gate 解锁或自动订单重放。MySQL DSN/多进程竞争、完整真实重启链、正式启动恢复装配、远端同 SHA CI、真实账户与盈利证据尚未验收。以上内容是历史阶段记录；当前状态以本页版本头与 Goal Teams 最新验收记录为准。

## 主要功能

- 多交易所网格 / 趋势 / 套利策略引擎（spot + perpetual）
- 可视化配置 + 多 Bot 并行管理 + 实时风控
- 回测 / 优化器 + 历史数据导出
- 多 AI 上游（OpenAI 兼容 / Claude 原生 / Gemini，含 DeepSeek / Moonshot / 智谱 / Ollama / OneAPI / Poe 等自定义 base_url）的市场解读、参数建议、新闻分析
- WebUI（React + Chakra）+ REST API + WebSocket 推送 + Telegram / 邮件 / Webhook 告警
- **PostHog / Sentry 可观测性错误上报（用户主动配置后启用）**
- **内嵌 MCP streamable HTTP 服务，可被 Claude Desktop / Cursor / 自研 Agent 直接调用 Bot、持仓、订单、PNL、对账、风控、系统健康与诊断工具**
- CI 会执行 Go 全仓单元测试、覆盖率摘要与 WebUI Vitest 单测，当前 Go 全仓语句覆盖率已推进到 `33.2%`

## 设计思路

- **单进程多 Bot**：每个 Bot 是 `(exchange, symbol, market_type)` 三元组，独立运行时与配置，统一存储
- **存储优先 SQL**：SQLite 默认，可切 MySQL / Postgres；配置文件作为可导入/导出的源
- **配置即数据**：YAML 与数据库双源，热更新；首次启动会自动迁移
- **WebUI 完全独立于后端**：通过 `/api/*` 通信，i18n 走 react-i18next（**禁止硬编码文案**）
- **AI 协议无关执行层**：`ai/service` 的 transport 层（`transport_{gemini,openai,claude}.go`）将「统一中立请求 → provider 协议请求/响应 → 统一结果」三步抽象为 `providerTransport` 接口；所有 provider 统一走异步任务队列 + 轮询，共享重试 / 超时 / token 统计。新增 provider 只需加一个 transport 适配器，业务层与配置层无感。配置通过 `provider + model + api_key + base_url` 任意组合上游（含命名上游 `ai.upstreams` 与模块级 `upstream_ref`）
- **可观测性**：
  - 日志写文件 + SQLite，前端可查询
  - Prometheus `/metrics` 端点
  - HTTP 响应统一带 `X-App-Version` / `X-Server-Version` 头
  - 错误自动上报到 17push（用户提供 API Key 后即生效，无 key 时整套静默）
  - 可选 PostHog / Sentry 错误上报（用户提供 Project API Key / DSN 后即生效）

## 当前待办

- [ ] MCP stdio 适配（目前仅 streamable HTTP，部分老 agent 客户端需要 stdio）
- [ ] 可观测性上报范围扩展：策略/下单失败、交易所 WS 异常断连
- [ ] 移动端响应式适配优化
- [ ] **偶发 CPU 100% busy-loop 现场确认**（社区 issue，rc14 已修复疑似根因，待真实 dump 验证）：现象为运行中偶发某 goroutine 空转占满 CPU、重启又正常。已定位高可能根因——`dynamic_adjuster`/`trend_detector` 读价格订阅 channel 漏判 `ok`，PriceMonitor 停止关闭 channel 后空转（rc14 已修）。另已审计 WebSocket（重连均有退避、出错即 return）并加固 KuCoin ping ticker、PricePollInterval 零值隐患（rc13）。已加入 `SIGUSR1` goroutine 栈快照工具（`monitor/stackdump.go`）用于抓现场，待用户回传一次真实 dump 做最终确认。

## 集成手册

### 17push 错误上报

1. 注册 [17push.com](https://17push.com) 并生成 API Key
2. 进入「全局设置 → 错误自动上报」，粘贴 Key 并启用
3. 点「测试连接」确认通；之后所有 `logger.Error` / 5xx / panic 会自动上报

### PostHog / Sentry 可观测性

1. 进入「全局设置 → 可观测性上报」
2. 填入 PostHog Project API Key 或 Sentry DSN，按需设置环境名
3. 点对应「测试」按钮确认通；之后 `logger.Error` / 5xx / panic 会同步上报到已启用的平台

### MCP 服务

1. 「全局设置 → MCP 服务」生成 Token
2. 复制下方 JSON（按 agent 类型切换）粘贴到客户端配置文件
3. Agent 即可调用 `qm_list_positions`、`qm_reconciliation_latest`、`qm_order_pnl_audit`、`qm_risk_events` 等工具核对数据；写工具默认关闭，需在 UI 显式开启
