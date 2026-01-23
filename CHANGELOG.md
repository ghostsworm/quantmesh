# Changelog

所有重要的项目更新都会记录在这个文件中。

## [Unreleased]

## [3.4.5] - 2026-01-24

### Improved
- **概览页默认展开交易所**: 概览页面默认展开所有交易所下的交易对列表，无需手动点击
  - 使用 `defaultIndex` 属性自动展开所有 Accordion 项
  - 优化用户体验，快速查看所有交易对状态

## [3.4.4] - 2025-01-01

### Fixed
- **CI 构建修复**: 修复 GitHub Actions 构建失败问题
  - 确保 `web/dist` 目录至少有一个文件，避免 `go:embed dist/*` 报错
  - 改进前端构建步骤，添加更好的错误处理和降级方案
  - 当前端构建失败时，创建占位文件以确保 Go 构建可以继续

## [3.4.3] - 2025-01-01

### Added
- **内置异步 AI 任务系统**: 集成完整的异步 AI 调用能力，无需依赖外部 `go-gemini-proxy` 服务
  - 新增 `AsyncTask` 模型支持异步任务持久化（支持 SQLite/PostgreSQL/MySQL）
  - 新增 `TaskService` 提供任务 CRUD 操作
  - 新增 `AIService` 封装 Gemini API 直接调用
  - 新增 `TaskProcessor` 后台任务处理器，支持并发控制和重试机制
- **多交易对管理和资金分配增强**: 完善交易对管理和资金分配系统
  - 支持批量添加常用交易对（BTC/ETH/SOL 等）
  - 策略资金配置持久化，支持最大资金限额、占比、储备金比例等
  - 实时资金分配视图，显示各策略的已分配/已使用/可用资金
  - 交易对启用/禁用控制
- **交易所盈亏诊断 API**: 新增 `/api/exchange/profit-diagnosis` 接口，提供交易所盈亏分析
- **精度调整优化**: Binance 适配器支持 TickSize/StepSize，自动从交易所获取最小变动单位

### Changed
- **统一 AI 访问方式**: 移除 `native`/`proxy` 访问模式选择，统一使用内置异步系统
  - `GeminiClient` 重构为 `AsyncGeminiClient`，内部自动处理任务创建和轮询
  - 配置文件移除 `access_mode` 和 `proxy` 相关配置项
  - 前端移除 AI 访问方式选择 UI 和代理配置表单
- **简化 API 接口**: `/api/ai/generate-config` 接口移除访问模式和代理相关参数
- **网格参数优化**: 修复网格参数价格间隔单位，从百分比改为 USDT 绝对值
- **项目文件结构优化**: 整理项目文件结构，移动测试文件、脚本和文档到专属目录
- **README 优化**: 添加徽章、性能指标和对比表

### Fixed
- **价差监控修复**: 修复价差监控中所有币种使用同一个合约价格的问题
  - 修改 13 个交易所适配器的 `GetLatestPrice` 方法签名，添加 symbol 参数
  - 现在能正确显示每个币种（SOL/ETH/BNB/BTC）的实际合约价格
- **首页 Dashboard 修复**: 
  - 数据口径统一：P&L/成交量改为读取 `/api/statistics` 累计数据
  - 修复 uptime=0 时 trades/hour 计算导致的 NaN 显示
  - 添加价格回退机制，当 status.current_price 不可用时使用 positionsSummary 中的价格
- **国际化完善**: 修复首页 Dashboard 的国际化问题，补全 zh-CN/zh-TW 翻译
- **对账校验页面修复**: 修复预计盈利曲线锯齿跳动问题

### Security
- **API Token 保护**: AI 调用完全在本地完成，敏感的 API Key 不再发送到外部服务

### Removed
- 移除对外部 `gemini.facev.app` 代理服务的依赖
- 移除 `ProxyGeminiClient` 和 `NativeGeminiClient` 双模式实现
- 移除配置中的 `ai.access_mode`、`ai.proxy.base_url`、`ai.proxy.username`、`ai.proxy.password` 字段
