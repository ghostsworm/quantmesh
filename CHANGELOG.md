# Changelog

所有重要的專案更新都會記錄在此檔案中。

## [3.105.0-rc38] - 2026-05-20

### Fixed
- **PostgreSQL/Supabase 配置存储**：新增基于 GORM 的配置中心实现，`database.type: postgres/postgresql` 可使用 Postgres/Supabase DSN 初始化 `config_entries` 与 `config_history`，避免原配置中心只识别 SQLite/MySQL。
- **远端 SQL 默认值**：`storage.type` 未填但主库配置为 MySQL/PostgreSQL 时会跟随主库类型；PostgreSQL/Supabase 与 MySQL 一样允许 `storage.path` 留空并使用 `database.dsn`，避免被错误补成 SQLite 文件路径。
- **启动引导**：未指定 YAML 时可通过 `QUANTMESH_DATABASE_DSN=postgresql://...` 读取 PostgreSQL/Supabase `app_config` 快照，迁移模式也不会把远端 SQL 的空 `storage.path` 覆盖成本地 SQLite。
- **前端与状态可观测性**：数据存储设置增加 PostgreSQL/Supabase 选项与 DSN 提示，服务状态接口按 Postgres DSN 判定配置完整性。
- **部署文档**：HA 与通用配置示例补齐 `storage.type: postgres` / 远端 SQL 留空 `path` 的写法，减少 Supabase/PostgreSQL 部署时的配置误导。

---

## [3.105.0-rc37] - 2026-05-19

### Fixed
- **密码找回闭环**：新增一次性恢复码机制，已登录用户可生成恢复码；忘记密码时可用恢复码重置密码，恢复码只明文展示一次，落库仅保存哈希。
- **认证风险收敛**：恢复码使用后会自动失效，并在密码恢复成功后清理该用户旧会话，降低遗失密码后的会话残留风险。
- **前端易用性**：登录页增加忘记密码入口，个人资料页增加恢复码生成与保存提示，相关文案走 i18n。

---

## [3.105.0-rc36] - 2026-05-19

### Fixed
- **WebAuthn 免密登录闭环**：指纹/Passkey 登录完成接口不再要求额外输入密码，前端移除二次密码弹窗，避免“看起来是指纹登录、实际还要密码”的误导。
- **凭证持久化可靠性**：WebAuthn 新凭证改为保存完整 credential 记录，并兼容读取旧版仅保存公钥的记录，避免重启或长期使用后公钥解析失败导致免密登录不可用。
- **登录会话与响应兼容**：WebAuthn 临时 challenge 会话改用加密随机 key，登录响应统一支持 base64url 与旧数组格式，降低并发登录、浏览器差异和重放窗口带来的认证风险。

---

## [3.105.0-rc35] - 2026-05-19

### Fixed
- **市场情报链路落地**：Polymarket Gamma 客户端解析真实 `outcomePrices` / `yes_probability`，并把概率透出给市场情报 API 与 AI 提示词，避免模型只靠题目和成交量猜概率。
- **AI 输出与凭据防护**：Polymarket 信号分析增加并发保护、防御性缓存拷贝和 signal/strength/confidence/probability 边界归一化；异步 Gemini 任务入库前脱敏 API key，真实 key 仅通过运行期内存引用取回。
- **NewsAPI 与长期任务稳定性**：NewsAPI 查询统一限长、去重、压缩空白并修复旧监控路径的非法 `language=zh,en`；宏观事件拉取器补齐空 context、无效间隔、Stop 后重启和 Gamma 响应体限流保护。

---

## [3.105.0-rc34] - 2026-05-19

### Fixed
- **多交易所订单链路安全性**：修复 BitMEX、Phemex 字符串型订单 ID 被错误转成 rune 的撤单/查单问题，修复 AscendEX、Poloniex 撤单/查单误传交易对而非订单 ID 的问题；Deribit、MEXC、BingX、WOO X、Crypto.com、BTCC、CoinEx、Bitrue、XT.COM 等批量撤单/全撤不再吞掉单笔失败，避免上层误判为全部成功。
- **Polymarket 情报质量**：Gamma 客户端解析并透出 `outcomePrices` / `yes_probability`，AI Polymarket 信号提示词改为使用真实 Yes 概率，避免模型只靠题目和成交量猜概率。
- **AI 情报输出防护**：Polymarket 信号分析增加并发保护、防御性缓存拷贝，以及 signal/strength/confidence/probability 的边界归一化，降低异常 LLM 输出污染市场判断的风险。
- **NewsAPI 长期运行稳定性**：NewsAPI 查询统一去重、压缩空白、限制关键词数和查询长度，修复旧监控路径使用 `language=zh,en` 导致请求失败的问题，并限制错误/响应体读取大小。
- **Gemini 调用成本与质量**：异步 Gemini 任务不再把完整 prompt 同时作为 `system_instruction` 重复提交，改用简短系统指令，减少无效 token 消耗并降低提示词自我干扰。
- **Gemini 密钥落库防护**：异步 AI 任务请求入库前会脱敏 `gemini_api_key`，运行期通过内存引用取回真实 key，降低任务表或本地数据库泄露后的凭据风险。
- **宏观事件拉取器生命周期**：Polymarket 宏观事件定时拉取支持空 context 兜底、无效间隔兜底、Stop 后重建停止通道，并限制 Gamma 响应体大小，降低长期 goroutine 静默失效或 panic 风险。

---

## [3.105.0-rc33] - 2026-05-19

### Fixed
- **信号策略自动下单**：趋势跟踪、均值回归、动量策略从 signal-only 补齐为自动交易模式，信号触发后通过策略执行器下单，并在成交回报后确认/清理本地仓位，避免未成交订单污染策略状态。
- **买卖方向与平仓保护**：三类策略统一按多头逻辑执行，开仓使用 `BUY`，平仓使用 `SELL`；合约市场平仓会带 `ReduceOnly`，降低误把平仓变成反向开仓的风险。
- **策略订单可观测性**：三类策略会记录待成交订单、pending action、单笔金额和真实统计，前端/API 可看到自动交易状态与当前挂起动作。

---

## [3.105.0-rc32] - 2026-05-19

### Fixed
- **策略运行态不再误报**：策略状态接口优先读取策略真实运行态，网格、DCA、马丁、组合以及信号型策略会在 Start/Stop 后同步运行状态，避免 UI/API 把“已启用但未运行”的策略误显示为运行中。
- **策略管理器锁安全**：策略广播与启动路径不再在持读锁时再次读取策略启用状态，降低长期运行中状态变更与行情回调交错造成锁等待卡住的风险。
- **信号型策略长期运行安全**：当时尚未自动执行的趋势跟踪、均值回归、动量策略明确标记为 signal-only，不再暗示会自动下单；均值回归和趋势策略的可视化/信号路径拆除嵌套锁，避免行情触发特定分支后策略卡死。

---

## [3.105.0-rc31] - 2026-05-19

### Fixed
- **多交易所公開 K 線入口**：`NewExchangeForPublicKlines` 不再只支持 Binance，已覆盖工厂声明的 Bitget、Bybit、Gate、OKX、Huobi、KuCoin、Kraken、Bitfinex、MEXC、BingX、Deribit、BitMEX、Phemex、WOO X、CoinEx、Bitrue、XT.COM、BTCC、AscendEX、Poloniex、Crypto.com、WhiteBIT、Bitkub、Coins.ph 等接入，未启动 Bot 时也能优先尝试对应交易所的公开行情/K 线适配器。
- **交易所 live 检查**：新增默认跳过的 `TestLivePublicExchangeMarketData`，设置 `QUANTMESH_LIVE_EXCHANGE_TESTS=1` 后可逐个调用已接入交易所公开行情接口，便于持续排查实际 API 可用性。

---

## [3.105.0-rc30] - 2026-05-19

### Fixed
- **风控 API 参数硬校验**：Bot 风控更新会拒绝负仓位、负挂单数、负距离以及超过 100% 的止损/止盈/追踪比例，避免异常前端输入或脚本调用直接污染运行时风控。
- **网格风控 Patch 语义**：`grid_risk_control` 部分更新不再把未传字段清零，避免只切换开关时误清空止损、止盈、追踪止盈和层数限制。
- **动态止损 API 不再假成功**：手动调整动态止损接口在未实现前返回 `501`，并校验 Bot ID 与止损比例范围，避免前端误以为止损已经更新。
- **前端错误文案与日志**：认证与初始化服务移除调试日志，错误兜底文案改走 i18n，提升 API 对接失败时的可读性。

---

## [3.105.0-rc29] - 2026-05-19

### Fixed
- **资金分配硬防护**：固定资金池总额超过账户资金时按比例缩放，权重策略不再获得负资金；负权重、负固定池、负预留和负释放统一归零或无操作，避免异常配置导致可用资金被反向放大。
- **止损平仓执行性**：DCA 与马丁策略的止损平仓单不再强制 `PostOnly`，继续保持 `ReduceOnly`，降低止损单因 maker-only 限制被拒或长期不成交的风险。

---

## [3.105.0-rc28] - 2026-05-19

### Fixed
- **组合策略定时器兜底**：自适应权重再平衡间隔为 0 或负数时使用 1 小时默认值，避免配置覆盖默认值后触发 `time.NewTicker` panic。

---

## [3.105.0-rc27] - 2026-05-19

### Fixed
- **定时风控兜底**：订单清理与深度监控在间隔配置为 0 或负数时会使用安全默认值，避免上游配置漏填导致 `time.NewTicker` panic；空 context 会兜底为后台 context。

---

## [3.105.0-rc26] - 2026-05-19

### Fixed
- **长期任务生命周期**：订单同步、资金费率监控、复合风控和开仓控制器支持更安全的停止/重启；context 取消后会回落运行状态，Stop 后重建停止通道，避免定时 goroutine 静默退出或因重复停止/重启导致任务失效。

---

## [3.105.0-rc25] - 2026-05-19

### Fixed
- **订单轮询生命周期自愈**：轮询启动时允许空 context 安全兜底；后台轮询因 context 取消退出后会自动标记为停止，避免服务已退出但 `isRunning` 仍为 true 导致无法重启。

---

## [3.105.0-rc24] - 2026-05-19

### Fixed
- **订单轮询回账可靠性**：订单状态轮询现在能正确读取 `int64` 订单 ID 和指针槽位，避免本地活跃订单无法被轮询；轮询服务支持停止后安全重启，并对 0 或负数轮询间隔使用默认值，降低外部成交/撤单漏回账风险。

---

## [3.105.0-rc23] - 2026-05-19

### Fixed
- **动态止损配置兜底**：波动率检查与盈利追踪启用但间隔配置为 0 或负数时，会使用安全默认间隔，不再因 `time.NewTicker(0)` 导致进程 panic；Bot 提供者为空时会跳过检查，避免风控模块因依赖缺失崩溃。

---

## [3.105.0-rc22] - 2026-05-19

### Fixed
- **动态止损生命周期**：`Stop()` 后再次 `Start()` 会创建新的停止通道，避免复用已关闭通道导致检查器立即退出；活跃止损槽位查询改为返回防御性拷贝，避免外部调用方篡改内部止损状态。

---

## [3.105.0-rc21] - 2026-05-19

### Fixed
- **全局熔断状态机**：熔断已触发时会忽略重复触发，避免后台检查或人工操作反复执行撤单/平仓；恢复逻辑不再持有状态锁调用外部 Bot，并尊重 `manual_required`，避免手动恢复场景被自动恢复绕过。

---

## [3.105.0-rc20] - 2026-05-19

### Fixed
- **紧急减仓兜底**：紧急中心的 `reduce_position` 不再返回“待实现”但标记成功；当前缺少按比例减仓接口时，会保守执行全平保护并在结果中明确说明，避免大额亏损场景误以为风险已经降下来。

---

## [3.105.0-rc19] - 2026-05-19

### Fixed
- **DCA / 马丁平仓状态机**：平仓单发出后不再立即清空内部仓位，而是进入 closing 状态；只有收到平仓成交回报后才清理层级/持仓状态，平仓取消则保留原仓位并允许下一轮重新平仓，避免限价平仓未成交时策略误判空仓并再次开仓。

---

## [3.105.0-rc18] - 2026-05-19

### Fixed
- **DCA 暂停恢复**：限时瀑布保护暂停到期后会自动恢复，不再因 `isPaused` 提前返回而永久停止加仓/开仓；无到期时间的手动或精度保护暂停仍保持暂停。
- **DCA / 马丁下单跳过保护**：当执行器因分布式锁等原因返回空订单并跳过下单时，策略不再解引用空订单，也不会把未成交请求写入仓位层级状态，避免多实例长期运行时 panic 或状态漂移。

---

## [3.105.0-rc17] - 2026-05-19

### Fixed
- **多策略卖开空资金审核**：`MultiStrategyExecutor` 不再把所有 **SELL** 都视为减仓；在 **SHORT / BOTH** 或策略名包含 short 的场景下，卖单会按开仓委托预估并预留策略资金，避免空头策略、双向网格长期运行时绕过资金池限制。
- **对冲策略归零**：`HedgeCoordinator` 与现货/合约对冲策略现在会发送并执行目标仓位为 **0** 的对冲信号，避免主策略低于触发层数或完全平仓后，对冲腿残留旧仓位。

---

## [3.105.0-rc16] - 2026-05-19

### Fixed
- **BOTH 雙向網格智能掛單**：`SmartOrderManager` 現在會把 **賣開空 SELL** 也識別為開倉委託，並按空側距離規則撤銷過遠/方向異常的掛單，避免雙向網格長期運行時只管理買開多、忽略空側開倉單。

---

## [3.105.0-rc15] - 2026-05-19

### Fixed
- **交易方向一致性**：`SymbolConfig.GetDirection()` 與舊版單交易對配置遷移現在保留 **`BOTH`**，避免雙向網格被狀態接口或配置轉換誤顯示/誤歸一為 **`LONG`**。
- **BOTH 雙向網格開倉配額**：同一輪同時生成買開多與賣開空時，新增總量保護，避免突破 `order_cleanup_threshold`；補充多腿/空腿平倉方向測試，確認多腿平倉為 **ReduceOnly SELL**、空腿平倉為 **ReduceOnly BUY**。

---

## [3.105.0-rc14] - 2026-05-19

### Fixed
- **Web 公開行情 API 穩定性**：`/api/market/ticker`、`/api/config/param-advisor` 與 `/api/optimizer/price` 的外部行情請求改用共享 HTTP client，增加 **8 秒超時**、請求上下文取消、HTTP 狀態碼校驗、響應體大小限制與統一 JSON 解析，避免交易所公共 API 卡住時拖慢 Web 入口。

---

## [3.105.0-rc13] - 2026-05-03

### Fixed
- **Bitget V2 收尾**：資金費率改為 **`GET /api/v2/mix/market/current-fund-rate`**（路徑與官方遷移表一致）；`productType` 使用適配器實際 **`usdt-futures` / `coin-futures` 等**，不再硬編碼。
- **Bitget 實時费率拉取**：**`GET /api/v2/common/trade-rate`** 改為官方 **Base64 HMAC** 簽名；**`businessType=mix`**（合約）；按 **`code=00000`** 解析 **`data`**。
- **`live_server/bitget`**：合約 **`productType` 查詢參數** 統一為小寫 **`usdt-futures`**（與主程序適配器一致）。
- **CHANGELOG**：舊條目內 Bitget 劃轉路徑更新為 V2。

---

## [3.105.0-rc12] - 2026-05-03

### Fixed
- **Bitget**：V1 REST 已下線（錯誤碼 **30032**）。合約訂單簿改為 **GET `/api/v2/mix/market/merge-depth`**；內部劃轉改為 **POST `/api/v2/spot/wallet/transfer`**，帳戶類型 **mix_usdt/mix_usdc** 改為官方 V2 的 **usdt_futures/usdc_futures**。

---

## [3.105.0-rc11] - 2026-04-21

### Fixed
- **配置**：`storage.type: mysql` 且 **`path` 留空**（依 `database.dsn`）時，預設 **`storage.enabled: true`**，避免誤以為未啟用存儲。
- **文檔**：`docs/config/examples/config-mysql8-example.yaml` 中 **`storage.type` 誤寫為 `database`**，已改為 **`mysql`** 並註明與 `database.dsn` 的關係。

---

## [3.105.0-rc10] - 2026-04-21

### Fixed
- **日志庫 SQLite**：連接 DSN 增加 **`_busy_timeout=15000`**；**`batchInsert`** 對 `database is locked` / busy 類錯誤做短重試，減輕高併發下「批量写入日志失败: database is locked」。

---

## [3.105.0-rc9] - 2026-04-21

### Fixed
- **`VolatilityAlertService.Stop`**：改為指針接收者並調用 **`cancel`**，避免按值傳遞複製 **`sync.RWMutex`**（`go vet` 報錯）。
- **`GetStatistics`**：在已持有讀鎖時不再調用 **`GetUnacknowledgedAlerts`**（會再次 **`RLock`**），消除同 goroutine 死鎖風險。
- **`DynamicAdjuster.Stop`**：停止時調用波動預警服務的 **`Stop`**，釋放上下文。

---

## [3.105.0-rc8] - 2026-04-21

### Fixed
- **收益統計日曆**：後端未返回某日記錄時（當日無成交等）改為顯示 **+0.00** 與 **0.0%** 等零值，不再整月顯示「無數據」；當月日期格均可點擊進入單日詳情。

---

## [3.105.0-rc7] - 2026-04-17

### Changed
- **訂單管理**：歷史訂單預設時間範圍由最近 **24 小時** 改為 **72 小時**；**`GET /api/orders/history`** 在未帶時間參數時預設同為最近 **72 小時**。

---

## [3.105.0-rc6] - 2026-04-15

### Fixed
- **體系外持久化**：**`storage.SaveAppConfigSnapshot`** 在寫入 **`app_config`** 後同步 **`cfg.Bots` → `bot_configs`**；**`SaveAppConfigSnapshotWithBotSource`** 可單獨指定 **`bot_config_history.source`**（與 **`app_config_history`** 的 `file_config_update` 區分審計）。**`main.go` 首次快照**、**`--migrate-app-config`**（原本已在一事務內寫入 YAML 目錄 Bot，行為不變）與 Web **`UpdateConfigWithBotHistorySource`** 路徑一致。
- **Web**：移除重複的 **`sync*`** 輔助函數，改由 **`persistAppConfigToDB`** 統一觸發存儲層同步。

### Added
- **`storage` 單元測試**：`TestSaveAppConfigSnapshotSyncsBotConfigs`。

---

## [3.105.0-rc5] - 2026-04-15

### Fixed
- **主庫 `app_config` 與 `bot_configs` 一致性**：除 **`PUT /api/bots/:id/strategy`** 外，補齊僅寫入主快照而未同步 Bot 文檔表的路徑——**創建/刪除 Bot、對沖組、批量 funding_carry、整體配置 JSON/YAML、AI 應用配置、新人風控一鍵加固、運行中風控持久化、開倉管理持久化** 等；刪除 Bot/組時 **`removeBotConfigSnapshotBestEffort`** 清理 `bot_configs` 行。

---

## [3.105.0-rc4] - 2026-04-15

### Fixed
- **`PUT /api/bots/:id/strategy`**：先前僅 **`fileConfigManager.UpdateConfig`** 寫入 **`app_config`**，未同步 **`bot_configs`**，導致主庫快照與 Bot 文檔表不一致（例如方向改為 LONG 後，啟動仍讀舊 **`bot_configs`**）。保存主快照後即 **`syncBotConfigSnapshotFromMainBot`** 寫入 **`bot_configs`**。
- **`ConvertFromBotConfig` / `ConvertToBotConfig`**：補齊 **`Grid.AutoRebuild`** 往返，避免同步時丟失自動重建配置。

---

## [3.105.0-rc3] - 2026-04-15

### Fixed
- **Web i18n（zh-TW）**：補齊 **`botCreate.direction` / `directionHint`**、**`backtest` 網格方向說明**（與 zh-CN / en-US 語義一致，繁體文案）。

---

## [3.105.0-rc2] - 2026-04-15

### Fixed
- **`POST /api/bots/create`**：`1b` 對主配置快照中**任意**同腿 Bot 一律 `BotsConflict` 拒絕，導致「僅有已停止 Bot」時仍 **409**；**2** 僅在 `cfg.Bots` 內找到運行中 ID 才擋，運行中 Bot **未寫回快照**時漏擋。改為：**1b** 在運行時可確認該 Bot **未在跑**（或已停用）時不阻擋；**2** 用 **`ListBots` + `GetBot`/最小腿信息** 做 `BotsConflict`。
- **Web i18n（en-US）**：`botCreate.directionHint` 與中文語義對齊（單向淨持倉 BOTH、現貨降級說明）。

---

## [3.105.0-rc1] - 2026-04-15

### Added
- **單向淨持倉雙向網格（`direction: BOTH`）**：合約網格下方買開多、上方賣開空，槽位 **`PositionLeg`** 區分多/空腿；平倉仍 **`reduce_only`**。新增可選 **`short_open_window_size`**（未設時繼承 `sell_window_size` / `buy_window_size`）。現貨選 BOTH 時啟動時自動降級為 LONG。`SuperPositionManager` 新增 **`adjustOrdersBoth`**、PnL/全平/撤開倉單等路徑已適配。

---

## [3.104.0-rc8] - 2026-04-15

### Fixed
- **`PUT /api/v2/bots/:id/risk-control`**：先前僅在 Bot **運行中**（內存實例存在）時可更新，已停止時一律 **404**，與 **GET**（可從主庫/快照讀）不一致。改為 Bot 未運行時與 **`resolveBotConfigFileFromUnifiedOrMain`** 一致載入配置、寫入 **`bot_configs`**（若可用）並同步 **`app_config` 快照**，並補單元測試。

---

## [3.104.0-rc7] - 2026-04-15

### Fixed
- **Web 保存主配置**：`FileConfigManager.UpdateConfig` 原在持有全局配置寫鎖時同步調用 `notifyNewsMonitorRuntimeSync` → `NewsMonitor.ApplyRuntimeConfig` → `stopInternalLocked` 會 **`<-analysisLoopDone` 阻塞數秒～十餘秒**，導致 `PUT /api/bots/:id/strategy` 等寫庫 API 整體變慢（journal 出現 `[GIN_SLOW]` 10～17s）。改為 **先釋放鎖再同步新聞監控**，避免拖住所有依賴配置鎖的請求。

---

## [3.104.0-rc6] - 2026-04-15

### Fixed
- **期權對沖 API**（`GET/POST /api/v2/bots/:id/option-hedge/*`）：先前僅用 `LoadBotConfig` 讀本地 `bots/<id>/config.yaml`，主庫 **`bot_configs`** 已有快照、但無磁盤文件時會載入失敗並回 **404**。改為與 **`loadBotConfigUnified`** 一致（主庫 → 本地 YAML → **`GetLatestConfig` 中對應 Bot**），並補單元測試。

---

## [3.104.0-rc5] - 2026-04-15

### Fixed
- **GET /api/exchanges**：先前僅讀啟動時的 `globalConfig`，Web 保存 `app_config` 後 `FileConfigManager` 已更新但 `globalConfig` 未同步，導致新建 Bot 下拉里看不到剛配置的交易所（如 Bitget）。改為優先使用 `GetConfig()`（與 `/api/config/json` 一致）。

---

## [3.104.0-rc4] - 2026-04-15

### Fixed
- **MEXC / AscendEX WebSocket**：外層重連時每次 `go heartbeat()` 未隨連線結束，導致多個 ping 协程泄漏；改為每條連線 **`hbStop` + `heartbeat(hbStop)`**，`readMessages` 返回後 `close(hbStop)` 並關閉連線；**`connect`** 增加 **`ctx.Done()`** 退出。
- **Coins.ph 價格 WebSocket**：讀取失敗即退出、無自動重連；改為 **`runPriceLoop`**（退避重連、每連線獨立 keep-alive）、**`StartPriceStream`** 重複調用時 **`priceCancel`** 取消上一路。
- **Bitfinex WebSocket**：重連前未釋放舊連線；讀錯後 **`w.conn` 仍非 nil** 時 **`StartPriceStream`** 僅訂閱導致無法恢復。新增 **`closeConn()`**，**`handleMessages`** 讀取前取鎖內連線副本、錯誤時先 **`closeConn()`** 再 **`reconnect`**；**`authenticate` / `subscribeTicker`** 經鎖取 `conn` 寫入。
- **Kraken Futures WebSocket**：同上 **`closeConn()`** 與讀取路徑；**`ping`** 改為 **`startPingLoop` + `pingWorker`**（`pingCancel` 與連線綁定），避免重連疊加多個 ping 协程。

---

## [3.104.0-rc3] - 2026-04-15

### Fixed
- **Bybit 公共行情 WebSocket**：與已修復之 OKX 類似，舊實作在單次 `readPriceMessages` 断線後不重連，導致 `lastPrice` 可長期卡死；重複啟動價格流時未取消上一路。改為 **`runPublicPriceLoop`**（可取消、断線重連、讀取超時），**`handlePriceMessage`** 校驗 **`topic`** 為 `tickers.{symbol}`；**`Stop`** 時一併取消公共行情协程。補 **`TestHandlePriceMessageFiltersTopic`**。

---

## [3.104.0-rc2] - 2026-04-15

### Fixed
- **OKX 公共行情 WebSocket**：`tickers` 断線後自動重連（含讀取超時），避免 `last` 長期卡死在舊價；重複 `StartPriceStream` 時取消上一路协程，避免多連接競寫 `lastPrice`；解析推送時按 **`instId`** 過濾，避免多標的批次推送時誤用 `data[0]`。補 **`TestHandlePriceMessageFiltersByInstId`**。
- **Gate.io 現貨訂單 WebSocket**：與 K 線流一致每 **15s** 發送 **`spot.ping`**，降低長連線被服務端斷開（如 **1006 abnormal closure**）後頻繁重連的情況。

---

## [3.104.0-rc1] - 2026-04-13

### Added
- **OKX 現貨網格庫存策略 `spot_inventory_policy`**：預設 **`conservative`**（啟動／對賬不自動將交易所基礎幣收編為網格庫存）；可選 **`adopt_all`**（重啟後一次性接管基礎幣餘額至賣單槽位）。現貨買單按 **quote 可用餘額** 裁剪；統一將 OKX **`51008`** 歸入餘額不足語義。Web 創建／Bot 詳情策略面板可編輯，**中／英** 文案；補 **`safety` 對賬** 與既有單元測試。

---

## [3.103.0-rc5] - 2026-04-13

### Changed
- **持倉安全檢查失敗提示**：餘額不足時除原有說明外，附 **當前可用餘額**、**滿足 N 倉估算所需可用餘額**（依每倉金額與杠杆）、以及 **合約名義約** 便於對照；補充單元測試。

---

## [3.103.0-rc4] - 2026-04-13

### Fixed
- **Bot 啟動仍用舊交易參數**：`StartBot` 在刷新主庫 `app_config` 後仍沿用調用入口傳入的舊 `botCfg`，導致持倉安全檢查等使用過期的 `order_quantity` 等。啟動前改為 **`resolveLatestStartConfig`**：對齊 `bm.cfg.Bots` 最新快照，並在存在 `bot_configs` 時以 Bot 專屬快照覆蓋；補充單元測試。

---

## [3.103.0-rc3] - 2026-04-13

### Fixed
- **開倉管理誤顯「Bot 未運行」**：運行中 Bot 使用 **UUID 主鍵** 時，`/api/opening-control/*` 仍用 `exchange:symbol:market_type` 推導舊式鍵查找運行時，導致與 Bot 詳情「運行中」不一致。現支援查詢參數 **`bot_id`**（前端在 Bot 工作區自動帶上 URL 中的 Bot ID），並在持久化時優先按 **`bot_id`** 寫回 `cfg.Bots`；補充 **`SymbolManagerProvider.GetByBotID`** 與單元測試 **`TestOpeningControlRuntimeMatchesQuery`**。

---

## [3.103.0-rc2] - 2026-04-13

### Removed
- **PostHog / 第三方後端遙測**：移除 `posthog-js` 與前端初始化；`utils/telemetry.go` 導出函數改為空實現；安裝腳本不再向分析端點發送請求；刪除 PostHog 相關腳本與冗餘 TELEMETRY_* 文檔，保留簡要說明於 `docs/TELEMETRY_GUIDE.md`。

---

## [3.103.0-rc1] - 2026-04-13

### Added
- **Bot 詳情「概覽」交易所帳戶餘額**：新增 **`GET /api/bots/:id/account-balances`**，依 Bot 的交易所／交易對／市場類型查詢：現貨返回標的 **基礎幣／計價幣可用餘額**，合約返回 **錢包／可用／保證金**（計價）；**資金費期現套利** 分現貨腿與合約腿；**雙永续跨所** 分腿展示。前端概覽頂部區塊約 **20 秒** 輪詢，並補 **中／英** 文案。

---

## [3.102.0-rc14] - 2026-04-13

### Fixed
- **Bot 詳情「實時日誌」不刷新**：`BotDetail` 新增 5 秒輪詢（靜默刷新），避免頁面停留在首次拉取結果造成長時間顯示 0 條。

---

## [3.102.0-rc13] - 2026-04-13

### Changed
- **下單失敗日誌**：`[Gate Spot]` 批量下單警告與 `ExchangeOrderExecutor` 批量下單 **ERROR** 一併輸出 **`qty`（基礎幣數量）**，後者另附 **`名义≈USDT`**（`price×qty`，便於對照配置的每單 USDT 金額）。

---

## [3.102.0-rc12] - 2026-04-13

### Fixed
- **Gate 現貨交易對列表為空**：`/api/v4/spot/currency_pairs` 回傳 **`quote` 為大寫 `USDT`**、可交易狀態在 **`trade_status`**（如 `tradable`），舊邏輯誤用 **`usdt` + `tradeable` 布爾**，導致篩選結果永遠為空；已改為 **`EqualFold` + `trade_status`** 解析。

---

## [3.102.0-rc11] - 2026-04-13

### Added
- **Gemini 用量持久化**：新增主庫表 **`gemini_usage`**，每次 `geminiusage.Record` 異步寫入；**`GET /api/ai/gemini/usage`** 支援 **`limit` / `offset` / `start_time` / `end_time`**（RFC3339），優先讀庫並返回 **`total` / `source`**。
- **Gemini 用量主頁**：前端路由 **`/gemini-usage`**（側欄「全局 · AI」），頂欄改為鏈接至該頁；原下拉浮層組件已移除。

---

## [3.102.0-rc10] - 2026-04-13

### Fixed
- **新聞監控配置保存後仍按舊任務運行**：`FileConfigManager.UpdateConfig` 成功寫入主庫後觸發 **`NewsMonitor.ApplyRuntimeConfig`**，替換運行時 `*Config` 指針並重啟/停止定時收集與分析；同步重啟 **價格歷史記錄器**、**預測驗證器**，並在重新啟用時向各 **`RiskMonitor`** 重新注入 **`SetNewsMonitor`**。
- **差異預覽**：`news_monitor` 節任意字段變更標記 **`requires_restart`**（與運行時同步並行，便於 UI 提示兜底）。

---

## [3.102.0-rc9] - 2026-04-12

### Fixed
- **Bot 對賬頁「預計盈利」異常偏大**：後端已支援 `bot_id` 僅統計該 Bot 配對成交；前端 **`Reconciliation`** 在 **`/api/reconciliation/status` / `history` / `aggregated`** 請求中帶上 **`bot_id`**（並納入 `useEffect` 依賴），換 Bot 時會重新拉數。

---

## [3.102.0-rc8] - 2026-04-12

### Changed
- **Bot 詳情實時日誌**：`fetchLogs` 僅依賴 **`bot_id`**（及級別/條數），移除對 `symbol` 的前置判斷與無關 `useCallback` 依賴，與「只按 `bot_id` 精準篩選」一致。

---

## [3.102.0-rc7] - 2026-04-12

### Fixed
- **Bot 詳情「實時日誌」長期 0 條**：`/api/logs` 對 `exchange` / `market_type` / `keyword` 皆為 **message 子串 AND**；OKX 日誌常為 **BTC-USDT**（不含 **BTCUSDT**）、正文未必含英文 **spot**，與 **bot_id** 聯用時被篩成空。詳情頁改為 **僅傳 `bot_id`（+ 可選級別）**；並更新中/英/繁說明文案。

---

## [3.102.0-rc6] - 2026-04-12

### Fixed
- **OKX 現貨/合約訂單 WebSocket**：`state` 為 **`canceled` / `filled` / `partially_filled` / `live`**（小寫+下劃線），與 SPM 內 **`CANCELED` / `FILLED` 等**不一致，導致 **`OnOrderUpdate` 無法進入對應分支**；撤單後槽位長期 **`CANCEL_REQUESTED` + `LOCKED`**，風控解除後也不會重新掛單。新增 **`normalizeOrderStatus`** 在處理前歸一化。

---

## [3.102.0-rc5] - 2026-04-12

### Changed
- **Bot 日誌方向修正**：以 **寫入時帶 `bot_id`** 為準——**`ExchangeOrderExecutor`** 構造參數注入 **`botID`**，內部訂單/撤單相關日誌統一 **`logger.WithBotID` + `*Ctx`**，使 SQLite **`logs.bot_id`** 有值；**Bot 詳情「實時日誌」** 恢復請求 **`bot_id` 精確篩選**（不再靠去掉 `bot_id` 查詢湊合）。
- **Cursor 規則**：新增 **`.cursor/rules/logging-bot-id.mdc`**，約束「禁止用查詢去掉 `bot_id` 替代寫入」。

---

## [3.102.0-rc4] - 2026-04-12

### Fixed
- **Bot 詳情「實時日誌」為空**：曾暫時去掉前端 **`bot_id` 精確匹配**（見 rc5 已改為寫入側注入並恢復查詢）。訂單執行器失敗日誌補上 **交易對符號**，便於命中 `keyword`。

---

## [3.102.0-rc3] - 2026-04-12

### Fixed
- **OKX clOrdId**：API v5 要求 **僅字母數字**、長度 ≤32；原 `GenerateOrderID` 含下劃線（含緊湊格式 `c…_B_…`）觸發 **51000 Parameter clOrdId error**。對 **`exchangeName == okx`** 改用 **`GenerateOrderIDWithSourceOKX`**（無下劃線；止損為末尾 **`SL`**），並擴展 **`ParseOrderID` / `ParseOrderSource`** 解析該格式。
- **OKX K線 WebSocket**：忽略非 JSON 帧（如純文本 **ping/pong**），避免 `invalid character 'p' looking for beginning of value` 告警。

---

## [3.102.0-rc2] - 2026-04-12

### Fixed
- **OKX 現貨下單**：依 `instruments` 的 **tickSz / lotSz / minSz** 對 **px、sz** 做對齊與最小量校驗（公開接口顯示 BTC-USDT 現貨 **minSz=0.00001** 與 **lotSz** 不同）；限價使用適配器 **priceDecimals** 格式化，避免與 **sz** 精度不一致；REST 外層錯誤時附帶 **data** 原文便於排查。
- **訂單執行器**：重試耗盡後的失敗日誌由 **WARN** 改為 **ERROR**（與嚴重度一致）。

---

## [3.102.0-rc1] - 2026-04-12

### Added
- **網格配對成交表 `trades.bot_id`**：SQLite / MySQL `qm_paired_trades` 遷移新增欄位與索引；寫入時由運行時 Bot 注入；可選從 `orders` 回填歷史列。
- **統計 API**：`GET /api/statistics`、`GET /api/statistics/daily` 支援可選查詢參數 **`bot_id`**；帶 `bot_id` 時僅聚合該 Bot 的配對成交與訂單已實現盈虧，且不採用全局 `statistics` 日表、不展示賬戶級資金費序列。
- **`GET /api/statistics/daily/breakdown`**：支援 **`bot_id`**；`GetDailyTradesSummary` 等存儲層方法同步支援 Bot 維度。
- **Web UI**：Bot 路由下統計與日盈虧拆解請求附帶 **`bot_id`**；統計頁增加說明文案（中/英）。

---

## [3.101.0-rc12] - 2026-04-12

### Added
- **SQLite 應用日誌表 `logs`**：新增 **`bot_id` 列**（可空）及索引；啟動時自動 `ALTER TABLE` 遷移舊庫。
- **`logger.WithBotID` / `InfoCtx` 等**：寫入日誌庫時可帶 **bot_id**；`startSymbolRuntime`、資金費套利／雙永续運行時主路徑已接入。
- **API `/api/logs` 響應** 增加 **`bot_id` 欄位**（與查詢參數一致時按列精確篩選）。

---

## [3.101.0-rc11] - 2026-04-12

### Added
- **`GET /api/logs`**：支援可選查詢參數 **`exchange`、`symbol`、`market_type`、`bot_id`**（對 `message` 子串匹配，與 `keyword` 等條件為 **AND**）；日誌表仍無獨立欄位，僅縮小文本結果集。
- **Web UI Bot 詳情「實時日誌」**：請求帶上 **`bot_id`、交易所、`market_type`**，與 `keyword`（交易對）一併縮小，避免多 Bot / 多所日誌混在一起。

---

## [3.101.0-rc10] - 2026-04-12

### Fixed
- **Web API `PickPriceProvider`**：`priceProviders` 映射缺失時，改從 **SymbolManager**（`GetEx` + `List` 匹配配置）解析运行時 `PriceMonitor`，避免回退到默認所價格源；**`registerWebSymbolProvidersForRuntime`** 在「已註冊 Status」提前返回時仍 **Upsert** 價格/持倉適配器，修復僅有 Status 無 Price 映射時日誌出現 `no provider found for key=okx:...` 並誤用默認 Provider 的問題。
- **對账恢複**：`RestoreReconciliationStats` 對 `*storage.ReconciliationHistory` 改為 **循環解引用** 至 struct，避免多層指針時誤報「對账記錄類型錯误」。

---

## [3.101.0-rc9] - 2026-04-12

### Fixed
- **Web UI**：儀表板、Bot 詳情、訂單／持倉／統計／槽位等 API 請求補上 **`market_type` 查詢參數**（與當前選中或 Bot 的 `exchange` / `symbol` 對齊），避免後端 `resolveSymbolKey` 落到預設交易對而出現錯誤 Provider 或日誌警告。

---

## [3.101.0-rc8] - 2026-04-12

### Fixed
- **OKX 訂單簿**：`GetOrderBook` 對 `request()` 已剝離的 `data` 誤解為帶 `data` 鍵的對象，實際為數組，導致 `json: cannot unmarshal array into Go struct`；深度監控與依賴訂單簿的邏輯無法解析（`exchange/okx/client.go`）。新增回歸測試 `TestOrderBookDataArrayUnmarshal`。

---

## [3.101.0-rc7] - 2026-04-12

### Added
- **Bot 啟動**：`StartBot` 前先按與 main 相同順序刷新內存主配置（命令行 YAML 若存在 → `app_config` 覆蓋 → 環境變量覆蓋），持倉安全檢查使用主庫中最新的 `fee_rate`。
- **手續費**：預設在啟動前調用交易所接口（Binance / Bitget）拉取 **Taker** 並寫入內存 `exchanges.<name>.fee_rate`（可用 `timing.skip_exchange_fee_on_bot_start: true` 關閉）；邏輯抽至 `feerate` 包並與 Web「拉取費率」接口共用。
- **定時同步**：`timing.fee_rate_refresh_minutes`（>0 時）按間隔刷新主配置並拉取各交易所 Taker 至內存（不強制寫回數據庫）。

### Changed
- `NewSymbolManager` / `NewBotManager` 增加參數 `primaryYAMLPath`（無則傳空字串）。

---

## [3.101.0-rc6] - 2026-04-12

### Fixed
- **Bot 獨立配置 API（`/api/bots/:id/config-file` 等）**：改為優先讀寫主庫 **`bot_configs` 文檔表**（與主配置 `app_config` 同庫），不再依賴可寫入的 `bots/<id>/config.yaml` 目錄；唯讀檔案系統（如容器根目錄）下可正常保存。
- **storage**：新增 `GetBotConfigDocument`、`SaveBotConfigSnapshot`、`DeleteBotConfigSnapshot`；單元測試 `TestSaveBotConfigSnapshotRoundTrip`。

### Changed
- **Web 文案**：Bot 風控「啟動前持倉安全」按鈕改為「保存」，說明改為主庫文檔表（zh-CN / en-US / zh-TW）。

---

## [3.101.0-rc5] - 2026-04-12

### Changed
- **Web Bot 專屬配置**：將「持倉安全檢查（position_safety_check）」自「交易參數」分頁移至「風險控制」分頁，並新增區塊標題「啟動與資金風控」。
### Added
- **Web Bot 詳情 → 風控**：新增「啟動前持倉安全檢查」卡片（讀寫 Bot 獨立設定檔 `advanced.position_safety_check`）；運行中 Bot 僅可檢視、停止後可儲存，並提供前往 Bot 專屬配置頁的連結。

---

## [3.101.0-rc4] - 2026-04-12

### Changed
- **Web 全局配置頁（/config）**：頂部增加說明——「交易參數」分頁僅在 **Bot 專屬配置**（`/bots/:botId/config`），避免與全局標籤混淆。

---

## [3.101.0-rc3] - 2026-04-12

### Added
- **Web 配置頁**：「交易參數」分頁新增 **持倉安全檢查（position_safety_check）** 表單欄位（與交易對管理頁一致），並附 zh-CN / en-US / zh-TW 文案。

---

## [3.101.0-rc2] - 2026-04-12

### Changed
- **持倉安全檢查預設值**：`position_safety_check` 預設由 100 改為 **5**（`config.DefaultPositionSafetyCheck`）；Web、API 回退與一鍵加固邏輯與之對齊；新手體檢「資金護城河」分級改為以 50／20 為界。

---

## [3.101.0-rc1] - 2026-04-12

### Added
- **Huobi（HTX）**：`InternalTransfer` 走現貨 `POST /v2/account/transfer`（`spot` ↔ `linear-swap`，全倉 `margin-account: USDT`）。
- **Kraken**：`InternalTransfer` 現貨→期貨 `POST /0/private/WalletTransfer`；期貨→現貨 `POST /derivatives/api/v3/withdrawal`。
- **劃轉映射單元測試**：`exchange/huobi`、`exchange/kraken` 與 KuCoin/Gate 風格一致之帳戶標籤（`UMFUTURE`/`SPOT`/`MAIN`）。

---

## [3.100.0-rc1] - 2026-04-12

### Added
- **Kraken / Deribit / MEXC / Huobi / Bitfinex**：`GetSpotPrice` 與 `GetOrderBook`（公共 REST／JSON-RPC）；對應 wrapper 轉為通用 `OrderBook`。
- **KuCoin 合約**：`InternalTransfer`（`POST /api/v3/transfer-out`、`POST /api/v1/transfer-in`，主帳 ↔ 合約）及劃轉方向單元測試。
- **長尾所**：`GetSpotPrice` 委託 `GetLatestPrice`（BingX、BitMEX、CoinEx、XT、WOO X、Phemex、Poloniex、Bitrue、AscendEX、BTCC、Crypto.com）。

### Fixed
- **Bybit / Binance / Bitget / OKX**：合約信息拉取失敗時從交易對符号推斷 base/quote，避免測試或離線環境下 `GetBaseAsset` 為空。
- **公開 K 線工廠測試**：遇 `Forbidden` 時跳過（與離線／沙箱環境一致）。

---

## [3.99.0-rc1] - 2026-04-12

### Added
- **Gate.io / Bybit / Bitget**：`InternalTransfer` 實作（Gate `POST /wallet/transfers`；Bybit `POST /v5/asset/transfer`；Bitget `POST /api/v2/spot/wallet/transfer`），並附帳戶標籤映射單元測試。
- **KuCoin 合約**：`GetSpotPrice` / `GetOrderBook` 走公共 ticker 與 level2 深度；`wrapper_kucoin` 轉為通用 `OrderBook`。
- **WhiteBIT**：`GetOrderFills`（`/api/v4/trade-account/order` 成交明细）與 wrapper 轉換；K 線流明確回傳不支援錯誤。

---

## [3.98.0-rc1] - 2026-04-12

### Added
- **OKX / Bybit / Gate.io / Bitget 現貨**：實作訂單流與 K 線 WebSocket（OKX 私有 `orders` SPOT + 公共 `candle`；Bybit 私有 `order` + 現貨公共 `kline`；Gate `spot.orders` + `spot.candlesticks`；Bitget 私有 `SPOT orders` + 公共 `SPOT candle`）。
- **現貨成交 REST**：上述四家 `GetOrderFills`（REST）及對應 `wrapper_*_spot` 轉換為通用 `OrderFill`。
- **OKX 內部轉帳**：`InternalTransfer` 支援資金帳戶與交易帳戶互轉（`/api/v5/asset/transfer`），合約與現貨適配器共用邏輯。

---

## [3.97.0-rc1] - 2026-04-12

### Added
- **Bybit / Gate.io / Bitget 現貨**：實作公共 WebSocket 價格流（Bybit `v5/public/spot` tickers；Gate `spot.tickers`；Bitget `SPOT` ticker）。
- **幣安現貨**：實作 User Data Stream 訂單流（`executionReport`）與現貨 K 線 combined stream；`GetLatestPrice` 優先讀取 miniTicker 緩存。

---

## [3.96.0-rc2] - 2026-04-12

### Fixed
- **OKX 現貨價格流**：實作 `StartPriceStream`（公共 WS `tickers`），此前 `3.96.0-rc1` 變更日誌已列但程式仍回傳「暂未實現」。

### Tests
- `exchange/okx`：`symbolToSpotInstId` 單元測試。

---

## [3.96.0-rc1] - 2026-04-12

### Added
- **Web 控制台**：全局設定「交易所 API」分頁中，未填寫 API Key / Secret（且無測試網、非零手續費等）的交易所預設摺疊為單行，點擊展開；展開後可收合。已填寫憑證的交易所仍完整顯示。

### Tests
- `exchangeConfigUi`：`isExchangeApiSlotVisuallyEmpty` 單元測試。

---

## [3.95.0-rc3] - 2026-04-12

### Fixed
- **Agent 聊天**：刪除當前會話後不再自動建立新會話，避免側邊欄仍顯示一條「空聊天」而誤以為刪除失敗；無活躍會話時輸入框提示先點「新建聊天」。刪除請求非成功狀態時改為提示失敗。

---

## [3.95.0-rc2] - 2026-04-12

### Added
- **Gemini 流式**：`agent/llm` 的 `GenerateStream` 在讀完 SSE 後依末帧（及中間帧）的 `usageMetadata` 寫入用量日誌，來源標記為 `agent_llm_stream`。

### Tests
- `agent/llm`：`streamChunkUsage` 根級與候選補全單元測試。

---

## [3.95.0-rc1] - 2026-04-12

### Added
- **Gemini 用量**：後端在每次 `AIService.GenerateContent` 與 Agent `GeminiClient` 非流式調用成功後，記錄時間、模型、來源、輸入/輸出 token、耗時（進程內環形緩衝，約 300 條）。
- **API**：`GET /api/ai/gemini/usage`（需登入），回傳 `entries` 與緩衝區內 `summary` 聚合。
- **Web 控制台**：頂欄全局「Gemini 用量」下拉選單，展示上述記錄（中英繁文案）。

### Tests
- `ai/geminiusage`：環形緩衝順序與聚合單元測試。

---

## [3.94.1-rc4] - 2026-04-12

### Fixed
- **Web 控制台**：`GlobalDashboard` 為 `findBotIdForSymbol` 增加顯式別名 `normalizeExchange`（等同 `normalizeExchangeName`），避免生產包中仍出現 `ReferenceError: normalizeExchange is not defined`。

---

## [3.94.1-rc3] - 2026-04-12

### Fixed
- **Web 控制台**：`GlobalDashboard` 交易所卡片誤傳未定義的 `normalizeExchange`，改為傳入 `normalizeExchangeName`，修復首頁白屏與 `ReferenceError`。
- **開發環境**：`yarn dev` 下不再註冊 Service Worker（`/sw.js` 不存在會被 Vite 回成 HTML，導致 MIME 錯誤與控制台刷屏）。

---

## [3.94.1-rc2] - 2026-04-12

### Fixed
- **Web 控制台**：修復部分環境（含 Cursor 內嵌瀏覽器）下首頁 `/` **白屏**：`PageWrapper` 不再使用 `motion` 的 `initial opacity:0` 進場動畫；移除包裹 `Routes` 的 `AnimatePresence`（與 RR6 組合易導致首屏不可見）。
- **語言選擇器**：當 `i18n.language` 為瀏覽器簡碼（如 `en`）與下拉選項（`en-US`）不一致時，對齊到已列舉值，避免 Chakra `Select` 異常。

---

## [3.94.1-rc1] - 2026-04-12

### Changed
- **Web 控制台**：全局 `/config` 與 **Bot 詳情** 主分頁在**窄屏**（小於 `md`）下，**未選中**分頁僅顯示圖標（`Tooltip` 顯示完整標題），**當前選中**分頁顯示圖標與完整文案；分頁列可橫向滾動，避免標籤文字豎排換行。Bot **訂單**分頁在未選中且窄屏時，未平倉單數量以圖標角標顯示。

---

## [3.94.0-rc1] - 2026-04-12

### Added
- **Web 控制台**：`/config` 全局設置中 **Gemini API Key** 與 **各交易所 API** 下方新增「測試」按鈕；**Gemini** 以輕量問答驗證金鑰與回傳格式；**交易所** 以臨時憑證呼叫 `GetAccount` 讀取帳戶權益（僅讀取、不下單）。
- **API**：`POST /api/config/test-gemini`、`POST /api/config/test-exchange`（需登入）。

### Tests
- `web`：`defaultTestSymbolAndMarket` 單元測試（Bitkub/Coins.ph 等現貨預設 symbol）。

---

## [3.93.0-rc1] - 2026-04-12

### Changed
- **Web 控制台**：`/config` 全局設置將 **交易所 API**、**AI 助手**（Gemini / 上游 / AI 配置助手）、**新聞分析**（NewsAPI、新聞用 AI Provider、資產列表等）拆分為獨立分頁；文案區分「全局 AI 配置助手」與「新聞分析專用」密鑰語義。

---

## [3.92.0-rc2] - 2026-04-12

### Fixed
- **收益統計 / 按日帳戶淨值曲線**：日統計 API 在 `daily_snapshots` 未帶 `account_equity` 時，改從 **hourly_equity_records** 按日取最後一條非空權益回填，便於畫出藍色淨值線。
- **Web**：淨值圖表改為 **灰色虛線始終表示本地累計盈虧**，**藍線僅表示交易所帳戶權益**（避免無淨值數據時藍線與圖例語義不符）。

---

## [3.92.0-rc1] - 2026-04-12

### Added
- **Web 控制台**：Bot 詳情「實時日誌」預設拉取 **500** 條（可選 200 / 500 / 1000 / 2000），支援按 **DEBUG / INFO / WARN / ERROR / FATAL** 篩選，並顯示符合條件總數與本頁條數說明。
- **API**：`GET /api/logs` 單次 `limit` 上限由 1000 提升至 **2000**（與 `storage.LogStorage.GetLogs` 一致）。

---

## [3.91.0-rc1] - 2026-04-12

### Added
- **Web 控制台**：全局看板「交易所概览」中的交易对卡片支持点击跳转到对应 Bot 详情页（`/bots/:botId`）；启动/停止与一键平仓按钮仍会优先响应且不会误触跳转。

### Tests
- `webui`：`findBotIdForSymbol`（按交易所+交易对+市场类型解析 `bot_id`）单元测试。

---

## [3.90.0-rc6] - 2026-04-11

### Fixed
- **Web 控制台**：语言选择器使用主题 `xs` 字号与正文字重（`fontWeight: normal`），避免较粗/视觉偏大、与正文不一致。

---

## [3.90.0-rc5] - 2026-04-11

### Fixed
- **Web 控制台**：`/config` 配置页在 `BrowserRouter` 下误用 `useBlocker`（仅支持 data router）导致运行时抛错白屏；已移除并对未保存提示保留 `beforeunload`。
- **Web 控制台**：窄屏顶栏中间状态区与品牌/操作区挤压换行错位；顶栏改为响应式 Grid，全局状态条使用 `Wrap` + `whiteSpace="nowrap"`。

### Tests
- `webui`：`reactRouterDataApi` 约定说明单测。

---

## [3.90.0-rc4] - 2026-04-11

### Changed
- **倉庫佈局**：`ARCHITECTURE.md`、`CONTRIBUTING.md`、`SECURITY_ALERT.md`、`K线文件统一管理功能说明.md`、`test_new_features.md` 移至 `docs/`，根目錄首屏更易聚焦 `README.md`；各語言 README 與內部鏈接已同步。

---

## [3.90.0-rc3] - 2026-04-11

### Changed
- **倉庫佈局**：根目錄示例與場景 **YAML** 統一移至 `docs/config/examples/`；`Dockerfile`、發佈打包、`install.sh`、`.gitignore` 與文檔中路徑已同步。
- **工具**：`analyze_market_data.go` 移至 `tools/`，默認讀當前目錄 `config.yaml`（可設 `QUANTMESH_CONFIG_YAML`）；`cfgmgr` 生成簡化 YAML 的路徑改為 `docs/config/examples/config.minimal.yaml`。

---

## [3.90.0-rc2] - 2026-04-11

### Changed
- **文檔**：主 `README.md` 改為用戶/營銷視角（價值表、適合人群、快速開始）；強化 Star 提示。
- **倉庫佈局**：根目錄 `dev.sh` / `start.sh` / `stop.sh` / `restart.sh` / `restart_api.sh` 移至 `scripts/local/`，根目錄更易掃到說明與核心檔案；`Makefile` 與文檔中路徑已同步。

---

## [3.90.0-rc1] - 2026-04-11

### Added
- **Web 控制台**：側欄左下角開源更新提示；後台請求 GitHub `releases/latest` 與本地 `/api/version` 比對，若有新版本可一鍵在新分頁打開倉庫（`VITE_GITHUB_REPO` / `VITE_DISABLE_OPEN_SOURCE_UPDATE` 可選）。

### Tests
- `webui`：`semverCompare` 單元測試。

---

## [3.89.0-rc2] - 2026-04-08

### Changed
- **部署腳本**：`scripts/deploy-git.sh` 新建 systemd 單元時 `ExecStart` 改為無參數（與主庫 `app_config` SSOT 一致），不再默認附加 `config.yaml`。
- **配置**：`SaveConfig` / `SaveConfigWithoutValidation` 註釋標明僅供 CLI/遷移/工具，Web 保存走數據庫。

---

## [3.89.0-rc1] - 2026-04-08

### Added
- **市場情報**：恐慌貪婪指數區塊置頂；RSS 區提供「AI 新聞簡報」按鈕（`GET /api/market-intelligence/news-digest`，後端 Gemini + 15 分鐘服務端緩存，需全局 Gemini API Key）。
- **市場情報客戶端緩存**：按數據源分層 TTL（如恐慌指數 10 分鐘、RSS/Reddit 2 分鐘等），「全部」視圖並行拉取各來源並合併；宏觀事件緩存獨立鍵。
- **配置頁**：新增「宏觀事件（Gamma）」卡片，可開啟 `macro_event.enabled` 與調整 `fetch_interval`（保存後需重啟進程）。

### Tests
- `webui`：`marketIntelligenceCache` 單測更新（分源 TTL、合併）。

---

## [3.88.0-rc7] - 2026-04-08

### Added
- **市場情報頁**：`sessionStorage` 短期客戶端緩存（默認 90 秒，介於 1～2 分鐘），相同搜索條件下刷新或返回頁面可減少重複請求；手動「刷新」與提交搜索仍強制拉取接口。宏觀事件/impact 與主列表共用 TTL。

### Tests
- `webui`：`marketIntelligenceCache` 單測。

---

## [3.88.0-rc6] - 2026-04-08

### Fixed
- **SQLite `order_placed` / SaveOrder**：`ON CONFLICT(exchange, account, symbol, order_id)` 必須與複合 UNIQUE 索引列完全一致。舊庫可能僅有 `(exchange, order_id)` 唯一約束，或同名 `idx_orders_exchange_account_symbol_order_id` 列序錯誤；`IF NOT EXISTS` 不會覆寫已定義索引。遷移時用 `PRAGMA index_info` 校驗，不符則 `DROP` 並重建。初始 `createTables` 不再在尚無 `account` 等列的舊表上提前建複合索引。修復校驗時未關閉首個 `Query` 結果集導致的 SQLite 單連接死鎖。

### Tests
- `storage`：`TestSaveOrderWrongCompositeIndexRepaired`。

---

## [3.88.0-rc5] - 2026-04-08

### Added
- **Web 配置頁**：偵測表單或 YAML 編輯器未保存變更時顯示頂部提醒、將「保存更改」按鈕高亮為橙色；離開頁面（路由或關閉分頁）時提示確認。

### Tests
- `webui`：`mergeFeeRateInputsIntoConfig` 單測。

---

## [3.88.0-rc4] - 2026-04-08

### Fixed
- **市場情報 HTTP 500**：`stripHTMLTags` 曾使用含 `\1` 回溯引用的正則；Go `regexp`（RE2）不支持該語法，`MustCompile` 在處理 RSS 描述等路徑時 panic，導致聚合接口返回 500。改為分別匹配 `<script>` / `<style>`，並用 `(?is)` 支持跨行內容。

### Tests
- `web`：`stripHTMLTags` 單測（含多行 script）。

---

## [3.88.0-rc3] - 2026-04-08

### Fixed
- **全局持倉**：定時刷新（每 10s）時合併已加載的開/平倉委託緩存，並用規範化行鍵（exchange/symbol 小寫）避免展開行與委託列表被清空或展開態丟失。

### Tests
- `webui`：`positionRowKey` / `mergePositionRowsForRefresh` 單測。

---

## [3.88.0-rc2] - 2026-04-08

### Fixed
- **CI / 倉庫完整性**：補提交 `ai/polymarket_signal.go`、`polymarket/` 與 Web 內置數據源 Gamma 相關改動，修復 `main.go` 引用 `PolymarketSignalAnalyzer` 時遠端編譯 `undefined` 的問題。

---

## [3.88.0-rc1] - 2026-04-08

### Added
- **配置界面 Polymarket 一鍵預填**：全域「市場風控」與單 Bot「AI 策略」中新增開關；開啟時合併 `docs/config/examples/config.example.yaml` 對應的 `ai.modules.polymarket_signal` 預設（Gamma URL、間隔、關鍵詞、信號閾值等），並補齊 `macro_event.gamma_api_url` / `fetch_interval`（不自動開啟 `macro_event.enabled`）。

### Tests
- `webui`：`polymarketConfigDefaults` 合併邏輯單測。

---

## [3.87.0-rc2] - 2026-04-08

### Fixed
- **Polymarket / Gamma 缺省值**：`ApplyGammaRelatedDefaults` 在舊庫存 `app_config` 缺字段時補齊 Gamma URL、分析間隔與 `signal_generation` 閾值；`LoadConfigFromBytes` 與 YAML/JSON 路徑一致應用缺省。

### Tests
- `config`：`ApplyGammaRelatedDefaults` 單測。

---

## [3.87.0-rc1] - 2026-04-08

### Added
- **Polymarket 市場情报**：內置數據源從 Gamma REST（`https://gamma-api.polymarket.com`）拉取活躍預測市場，無需登錄 token；支援 `macro_event.gamma_api_url` / `ai.modules.polymarket_signal.api_url` 覆寫；`ApplyDataSourcePolymarketConfig` 在路由初始化時應用。
- **Polymarket + LLM 信號**：新增 `ai.PolymarketSignalAnalyzer`，在 `ai.modules.polymarket_signal.enabled` 且 AI 上游有效時註冊 Web API，定時拉取 Gamma 並用 LLM 輸出綜合信號與市場級解讀；註冊不依賴「當前有無運行中的交易對」。
- **`polymarket` 包**：`FetchActiveMarkets` 供 Web 與 AI 共用。

### Tests
- `polymarket`：Gamma 解析與關鍵詞過濾單測。
- `ai`：`parsePolymarketJSON` 單測。

---

## [3.86.0-rc2] - 2026-04-08

### Fixed
- **每日統計 API**：合併日期鍵時納入資金費、交易所已實現盈虧與日快照；僅有資金費/交易所數據而無網格成交時仍返回當日記錄，避免統計頁日曆「中間缺一日」。
- **統計日曆**：月份過濾改用 `YYYY-MM-DD` 字串解析，避免 `new Date('YYYY-MM-DD')` 在部分時區導致月份錯位。

### Tests
- `web`：`collectDailyStatDateKeysInRange` 單測。
- `webui`：`calendarMonthMatchesDateStr` 單測。

---

## [3.86.0-rc1] - 2026-04-08

### Added
- **統計淨值曲線**：小時任務調用交易所 `GetAccount` 採樣帳戶權益（USDT）寫入 `hourly_equity_records` / `daily_snapshots`；`/api/statistics/daily` 返回 `account_equity` 與 `market_type`；Web 曲線實心藍線為交易所帳戶權益、灰虚線為本地累計盈虧對照；每日明細表可選展示「帳戶權益」列。

### Tests
- `webui`：`buildDailyEquityChartPoints` 含 `account_equity` 映射單測。

---

## [3.85.0-rc1] - 2026-04-08

### Added
- **Bot 策略与实盘对齐**：`ApplyBotStrategiesToLocalConfig` 将 `SymbolConfig.strategies` 合并到运行时的 `localCfg.Strategies`（`trend_following`→`trend`、`grid+trend` 展开为 grid+trend 双策略、快慢线参数别名）；纯网格单策略仍走 legacy SPM；`ShouldSkipInitialGridAdjustOrders` 在非网格多策略启动时跳过首轮网格挂单。
- **Web Bot 详情**：策略参数面板按类型分支——网格类显示网格/智能挂单/三级火箭；趋势/动量/均值回归/DCA/马丁及 `grid+trend` 显示可编辑专属参数并写入 `strategies[].config`。

### Tests
- `config`：`ApplyBotStrategiesToLocalConfig` / `ShouldSkipInitialGridAdjustOrders` 单测。
- `web`：`UpdateBotStrategyRequest` 嵌套 `strategies[].config` JSON 解析单测。

---

## [3.84.0-rc3] - 2026-04-08

### Added
- **Bot 詳情（雙永续）**：`funding_perp_spread` Bot 展示兩腿 U 本位永續行情（標記價、最新價、24h 高低）、中間價基差；概覽「當前價」改為兩腿標記價中間價；隱藏不適用的網格強平估算；`parseFundingPerpSpread` 單測。

---

## [3.84.0-rc2] - 2026-04-08

### Added
- **Web 創建 Bot**：策略類型新增「雙永续跨所資金费」；表單填寫腿 A/B 交易所與合約、`funding_perp_spread` 閾值與分配資金；`CreateBotRequest` 類型擴展。

---

## [3.84.0-rc1] - 2026-04-08

### Added
- **雙永续跨所資金費差套利**：新 `market_type`：`funding_perp_spread`；配置 `funding_perp_spread` 兩腿（各所合約符號）；策略 `FundingPerpSpreadStrategy` 對比兩腿資金費率，高費率腿做空、低費率腿做多；運行時 `startFundingPerpSpreadSymbolRuntime`；Bot 衝突檢測改為 `BotsConflict`（期貨腿重疊 + 資金費期現套利規則）；API 創建 Bot 支援兩腿與單策略 `funding_perp_spread` 校驗。

---

## [3.83.0-rc2] - 2026-04-08

### Added
- **Kraken GetFundingInfo**：`GetPerpetualTicker` 取費率與標記/指數價；下次結算 REST 無欄位，按每整點 UTC（與官方 hourly historical funding 一致），並在 `exchange` 增加 `EstimateNextFundingKrakenHourlyUTC` 供共用說明與單測。
- **WhiteBIT GetFundingInfo**：`GetFuturesMarketByTicker` 讀取 `next_funding_rate_timestamp`、`funding_rate`、`index_price` / `last_price`；解析失敗時回退原 `GetFundingRate` + 8h 估算。

---

## [3.83.0-rc1] - 2026-04-08

### Added
- **KuCoin GetFundingInfo**：透過期貨公開接口 `GET /api/v1/contracts/{symbol}` 解析 `nextFundingRateDateTime`、標記/指數價與 `fundingFeeRate`；`BTC-USDT` 映射為合約代碼 `XBTUSDTM`；失敗時仍回退 `GetFundingRate` + UTC 8h 估算。

---

## [3.82.0-rc2] - 2026-04-08

### Added
- **交易所 GetFundingInfo**：除幣安外，為 OKX、Huobi、Bybit、Gate、Bitget 在適配層實現完整資金費詳情（費率 + 下次結算時間 + 標記/指數價，依各所公開 API）；其餘合約所透過 `GetFundingRate` + `GetLatestPrice` 與 UTC 8h 估算結算時間後備；新增 `exchange/funding_estimate.go` 共用邏輯與單測。

---

## [3.82.0-rc1] - 2026-04-08

### Added
- **Bot 詳情 / 風控**：持久化「開倉暫停 / 恢復」事件（含原因與來源：配置層、開倉控制器、自動恢復計時器等），SQLite/MySQL 表 `bot_risk_control_events`；API `GET/GET export /api/v2/bots/:id/risk-control/events`；前端「風控」標籤頁內「風控記錄」分頁列表與 CSV 下載。

---

## [3.81.2-rc2] - 2026-04-07

### Changed
- **Web**：Bot 详情风控面板顶部增加说明，链向全局「市场风控」；微调「独立风控」开关的文案，避免与全市场 K 线/深度配置混淆。

---

## [3.81.2-rc1] - 2026-04-07

### Added
- **Web**：全局「设置」(`/config`) 新增「市场风控」标签页，完整暴露 `risk_control`（K 线监控交易对、周期、成交量倍数、均线窗口、恢复阈值、最大杠杆）与 `depth_monitor`（深度监控）表单；单 Bot 配置页「风险控制」中重复的引擎项改为说明并链向全局，避免与 Bot 详情「独立风控」语义混淆。

---

## [3.81.1-rc1] - 2026-04-07

### Fixed
- **Web**：收益统计区间下拉增加「最近 365 天」，与后端拉取的一年数据对齐。

---

## [3.81.0] - 2026-04-07

### Added
- **Web**：收益统计页（全局与单 Bot `/bots/:id/statistics`）新增「按日累计净值曲线」：折线为后端「累计盈亏」，柱状为当日「盈亏 + 资金费」；「最近 7/30/90 天」筛选同时作用于曲线与下方每日明细表。

---

## [3.80.0] - 2026-04-07

### Added（Funding Carry Pro 五大增強特性）
- **結算時間感知**：`IExchange` 接口新增 `GetFundingInfo` 方法，策略自動追蹤 `nextSettlement` 時間，結算臨近時暫停開倉（可配 `settlement_buffer_min`），結算後自動觸發利潤歸集。
- **資金自動劃轉**：開倉前 `ensureFuturesMargin` 自動從現貨劃轉 USDT 到合約帳戶；結算後 `harvestProfit` 自動歸集合約盈餘到現貨帳戶。可配 `auto_transfer_enabled`、`transfer_reserve_spot`、`profit_harvest_enabled`、`profit_harvest_min`。
- **負費率反向套利**：當費率持續為負時自動借幣賣出現貨 + 合約做多，收取負費率。需 `ISpotMarginExchange`（目前僅 Binance）。可配 `reverse_enabled`、`reverse_min_funding_rate`、`reverse_exit_funding_rate`、`margin_interest_max`。
- **多幣種並行**：每個 `funding_carry` Bot 獨立同步資金費收入（不再依賴全局 `firstRuntime`）；新增 `POST /api/funding-carry/batch-create` 批量創建 API，支援多幣種均分資金。
- **歷史收益面板**：新增 `GET /api/funding-carry/dashboard` 和 `GET /api/funding-carry/income-history` API；前端 `FundingCarryDashboard.tsx` 組件含總收益卡片、每日收益圖表（柱狀 + 累計折線）、各幣種狀態表格；側邊欄新增入口。

### Changed
- `FundingCarryStrategy` 構造函數新增 `marginEx exchange.ISpotMarginExchange` 參數（可 nil）。
- 策略持倉狀態從單一 `spotQty/futQty` 擴展為含 `direction`（Forward/Reverse/None）、`marginDebt` 的完整狀態機。
- `funding_carry_runtime.go` 啟動時嘗試建立 `spot_margin` 連線，失敗不阻塞但禁用反向。

---

## [3.79.14-rc2] - 2026-04-07

### Fixed（資金費套利策略生產級加固）
- **開倉原子性**：現貨限價單先下 → 輪詢等待成交確認 → 再用實際成交數量開合約空；任一步驟失敗自動撤單回滾，杜絕單腿裸多/裸空。
- **停止時平倉**：`Stop()` 先嘗試雙腿平倉（合約市價平空 + 現貨激進限價賣出），失敗則發 `risk_triggered` 通知。
- **事件通知**：開倉成功 → `position_opened`；平倉 → `position_closed`；下單失敗/腿不對齊/連續錯誤 → `order_failed` / `risk_triggered`，經 `EventCenter` → Telegram/Webhook。
- **持倉同步**：每次 tick 從交易所拉真實持倉（`GetPositions` + `GetBalance`），不再依賴內存布爾值；重啟後狀態自動恢復。
- **平倉滑價**：現貨賣出由 0.3% 調為 1%（激進限價），合約始終市價平空。
- **腿不平衡檢測**：tick 中自動比對 spotQty vs futQty，異常時發 `risk_triggered` 告警。
- **連續錯誤告警**：tick 連續 5 次失敗自動發 critical 級通知。

---

## [3.79.14] - 2026-04-07

### Added
- **資金費率套利 Bot（funding_carry）**：獨立 `market_type` 與單策略 `funding_carry`；與同交易所同幣種其它 Bot 互斥；幣安雙市場預檢 API `POST /api/bots/preflight-funding`；運行時雙連線（UM 合約 + 現貨）與 `FundingCarryStrategy`。
- **Web**：Bot 創建向導與 `CreateBotRequest` 類型支援 `funding_carry`；`preflightFundingCarry` 客戶端封裝；中英文 `error.funding_carry_single_strategy` / `error.bot_symbol_market_conflict`。

### Fixed
- **`config/config_test.go`**：移除未使用 import，恢復 `go test ./config/...` 可編譯。

---

## [3.79.13] - 2026-04-06

### Added
- **Web Bot 工作区导航**：在 Bot 模式侧边栏与移动端底部栏增加「机器人详情」入口，指向 `/bots/:botId`（策略/风控等 BotDetail 页），与「交易面板」`/dashboard` 并列，避免从列表进入详情后无法在交易子页返回该页；路径高亮使用 `isBotWorkspaceRoot` 避免与 `/dashboard` 冲突。

---

## [3.79.12] - 2026-04-05

### Changed
- **README**：改寫語氣，減少套話式排版與營銷腔；收斂大表與 emoji 標題；保留核心事實與鏈接；頁腳版本與 `main.go` 對齊為 **3.79.12**；版權年份 2026。
- **docs(i18n)**：`docs/i18n/README.en.md` 與根目錄中文版結構與語氣對齊；移除舊版大表與 AI 營銷條目；版本與版權年同步。
- **存儲命名**：`storage/sqlite.go` 更名為 `storage/sql_storage.go`（實際為 SQLite/MySQL 共用的 `database/sql` 實現）；類型 `SQLiteStorage`、構造函數 `NewSQLiteStorage` 分別更名為 `SQLStorage`、`NewSQLStorage`。文檔中舊路徑已同步替換。

---

## [3.79.11] - 2026-04-05

### Fixed
- **订单管理「历史订单」时间筛选**：原先用 `created_at`（下单时间）过滤，网格单常早挂单、晚成交，选「最近 24 小时」时列表与统计易显示为 0。已改为按 `updated_at` 筛选并与 `total_count` 时间范围一致；「今日订单数」按更新日期统计。Web 订单页增加简短说明文案（i18n）。

---

## [3.79.10] - 2026-04-04

### Added
- **Bot 風控頁展示觸發原因**：API 增加 `risk_trigger_message`（K 線風控與深度風控 lastMsg 合併）；`/api/bots/:id/risk-control` 返回 `pause_opening_reason`。Web 風控標籤頂部展示市場/深度說明與暫停開倉原因；Bot 詳情每 15s 刷新以同步狀態。

---

## [3.79.9] - 2026-04-04

### Fixed
- **Web `/api/status` 與 Bot 詳情不一致**：進程啟動後再通過 UI/API 啟動的 Bot 此前未掛接 `statusBySymbol`，導致儀表盤顯示「幣種未運行」而 Bot 頁為「运行中」。現於 `StartBot` 成功後注册 Web 狀態與 provider，於 `StopBot` 時注销；服務啟動時若已在 `StartBot` 中注册則跳過重複注册，並修正 `SetStatusProvider` 使用含 `market_type` 的鍵。

---

## [3.79.8] - 2026-04-04

### Release
- **穩定版**：與 **`3.79.8-rc26`** 代碼一致；主配置以主庫 **`app_config`** 為權威來源；文檔、Web 多語言與 **`ghostsworm/quantmesh`** 鏈接已對齊。細項見下方 **3.79.8-rc\*** 歷史條目。

---

## [3.79.8-rc26] - 2026-04-03

### Docs / scripts
- **主倉庫鏈接**：**`plugin/`**、**`docs/reports/IMPLEMENTATION_SUMMARY.md`**、**`docs/i18n/README.{pt,es,fr}.md`**、**`docs/PRODUCTION_DEPLOYMENT.md`**、**`scripts/create_plugin.sh`** 等處的 clone / Issues 統一為 **`ghostsworm/quantmesh`**，目錄名 **`quantmesh`**（移除過時的 **`quantmesh_market_maker`** / 錯誤 org）。
- **路徑拼寫**：示例與部署腳本中的 **`/root/quntmesh`** 更正為 **`/root/quantmesh`**（**`docs/SSH_TUNNEL_ACCESS.md`**、**`docs/CONFIG_TIERED_LIMITS.md`**、**`scripts/DEPLOY_GIT_README.md`**、**`scripts/deploy-git.sh`**、**`scripts/emergency_security_check.sh`**）；**`CHANGELOG`** 舊條目改為說明歷史誤拼 **`quntmesh`** 的遷移語義。

---

## [3.79.8-rc25] - 2026-04-03

### Docs / i18n
- **`rdocs/articles/zh/07-常见问题.md`**：GitHub Issues 鏈接改為 **`ghostsworm/quantmesh`**。
- **Web UI 多語言**：移除已下線「磁盤備份 / 配置歷史 API」相關且無代碼引用的 **`configuration.backupManagement*`**、**`loadBackupListFailed`**、**`globalTabs.history`**、**`configHistory`** 等鍵（24 個 locale JSON），避免與 **`app_config` SSOT** 敘事混淆。

---

## [3.79.8-rc24] - 2026-04-03

### Docs
- **`rdocs/`**（快速入門、FAQ、部署指南、測試網/槓桿率等）：與 **`app_config` + `--migrate-app-config` / 首參 YAML** 敘事對齊；倉庫克隆地址改為 **`ghostsworm/quantmesh`**。
- **`plugin/*.md`、`scripts/*README.md`、`docs/ARCHITECTURE.md`、`docs/SECURITY_ALERT.md`、`docs/HIGH_AVAILABILITY.md`**：去掉不存在的 **`--config` / `--check-config` / `--standby`** 示例；**`rdocs/CHANGELOG.md`** 頂部增加歷史條目免責說明。

---

## [3.79.8-rc23] - 2026-04-03

### Docs
- **`API_REFERENCE.md`**（及 **`docs/i18n/en`**、**`docs/i18n/zh-TW`**）：刪除已下線的 **`/api/config/backups`**、**`/api/config/history*`**、**`/api/export/config/history/*`**；補齊現有 **`/api/config/*`**（param-advisor、security、test-notification 等）與 **`/api/export/backtest-reports`**；註明主配置 **`app_config`**。
- **`docs/reports/CONFIG_TEST_REPORT.md`**：與上述 API/前端變更對齊，標註過時項。

---

## [3.79.8-rc22] - 2026-04-03

### Docs
- **第二輪掃描 `docs/`**：補齊運維/故障/內存/MySQL/PWA/許可/WebAuthn 等零散文檔中仍以 **`config.yaml` 為唯一敘事** 的段落；`OPERATIONS_SUMMARY`、`PRODUCTION_DEPLOYMENT`（多實例）、`CICD_GUIDE`、`HA_QUICKSTART`、`DISTRIBUTED_LOCK`、`CONFIG_TIERED_LIMITS`、`SECURITY_FIX_SETUP_AUTH`、`reports/*` 等與 **`app_config` SSOT** 對齊；Docker 示例補 **首參**或 **migrate** 說明；刪除/替換不存在的 **`--config` / `--check-config` / `--port`** 旗標示例。

---

## [3.79.8-rc21] - 2026-04-03

### Docs
- **`docs/` 全量清查**：與 **zh / en** 基準對齊——`README.*`、`config-database-design.md`、`CONFIGURATION_REDUNDANCY_*`、`BACKUP_RECOVERY`、`PRODUCTION_DEPLOYMENT`、`guides/README_SCRIPTS`、`AI_UPSTREAM_PROFILES`、新聞監控文檔等統一為 **主庫 `app_config` + 導入 YAML / `--migrate-app-config`** 敘事；移除已刪模組（如 `config/backup.go`）引用；修正不存在的 `--config` / `--check-config` 示例。

---

## [3.79.8-rc20] - 2026-04-03

### Breaking
- **主配置持久化**：移除磁盤 **`config.yaml` 作為權威來源**、**`backups/`**、**`config_history.db`** 及相關 REST（`/api/config/backups`、`/api/config/history*`、`GET /api/export/config/history/:version` 等）。主配置僅經 **`app_config` / `app_config_history`**（主庫）持久化；Web 仍支援 **YAML 編輯**（序列化自內存，不落盤為固定文件名）。
- **啟動參數**：未傳命令行 YAML 路徑時，僅從主庫或引導加載；**不再自動寫入最小 `config.yaml`**。可選第一參數為一次性 YAML 路徑；`--migrate-app-config` 需 **`QUANTMESH_IMPORT_YAML`**、命令行路徑或當前目錄存在 **`config.yaml`**。

### Added
- **`storage.SaveAppConfigSnapshotFromJSON`**：寫入含擴展鍵（如 `security`）的完整 JSON 快照。

### Docs
- **`README.md` / `docs/config-database-design.md` / `rdocs/部署指南.md`**：改為「範例 YAML + 環境變量 + 主庫 `app_config`」敘事；systemd / supervisor 示例不再強制 `config.yaml` 參數。

---

## [3.79.8-rc19] - 2026-04-03

### Changed
- **啟動 / 無 config.yaml**：`LoadConfigFromAppConfigDBIfExists` 在本地 SQLite 無有效 `app_config` 快照時，若已設置 **`QUANTMESH_DATABASE_DSN`**（MySQL），會再從 **MySQL `app_config`** 加載，便於純 RDS 部署、不再依賴 `./data/quantmesh.db` 做啟動引導

---

## [3.79.8-rc18] - 2026-04-03

### Added
- **API / Bot 列表與詳情**：響應增加 **`testnet`**，取值與當前 `exchanges[exchange].testnet` 一致（無交易所條目時回退 Bot 記錄）；`Config.EffectiveTestnetForExchange` 統一邏輯
- **Web / Bot 列表與詳情**：展示 **測試網 / 實盤** 標籤；i18n 鍵 `botList.envTestnet`、`botList.envLive`（全語言）

---

## [3.79.8-rc17] - 2026-04-03

### Fixed
- **Web / 創建 Bot**：啟動時將主進程已加載的配置注入 `FileConfigManager`，使 `GET /api/config/json` 含完整交易所密鑰（例如僅存於 `app_config` 時），`/bots/create` 可正常拉取交易對列表
- **Web / 創建 Bot**：`exchanges` 鍵名不區分大小寫匹配；拉取失敗或缺少 API Key 時顯示明確提示（i18n zh-CN / en-US）

---

## [3.79.8-rc16] - 2026-04-03

### Fixed
- **Binance 合約/現貨**：自動將常見拼寫錯誤 **`USTD` → `USDT`**（如 `BTCUSTD` → `BTCUSDT`），避免 WebSocket 訂閱無效交易對導致「等待首個價格超時（10秒）」

---

## [3.79.8-rc15] - 2026-04-03

### Changed
- **`restart.sh`**：默認改為**開發模式**（`go run` + `webui` Vite 熱更新）；生產模式需顯式傳 **`--prod` / `-p`**，可選 `config.yaml`；無 `--prod` 時若誤傳配置文件路徑會提示並忽略

---

## [3.79.8-rc14] - 2026-04-03

### Fixed
- **Web / 全局看板**：`setLoading(false)` 移入 `finally`，避免 try 內處理數據時拋錯導致永遠轉圈；`symbols` 缺省為空數組；`normalizeExchangeName` 防禦空值；`Promise.all` 增加 60s 總超時；輪詢 `useEffect` 改為僅掛載綁定（避免依賴 `toast` 反覆卸載 interval）；錯誤提示經 `toastRef`/`tRef` 保證最新 i18n

---

## [3.79.8-rc13] - 2026-04-03

### Changed
- **Web / 首次配置**：保存成功、失败及表单校验改用 Chakra **Toast**（右下角 `bottom-right`，与全站统一），并抽取 `DEFAULT_APP_TOAST_OPTIONS`

---

## [3.79.8-rc12] - 2026-04-03

### Added
- **Web / 首次配置 (`/config-setup`)**：交易对改为下拉选择，默认 **BTCUSDT**；主流币种带推荐价格间隔、订单金额、窗口等预设；最小订单价值支持小数（`step=0.01`）
- **Web**：`configSetupSymbolPresets` 与单元测试

---

## [3.79.8-rc11] - 2026-04-03

### Changed
- **启动脚本**：`start.sh` / `restart.sh` 不再强制要求磁盘上存在 `config.yaml`；缺失时与主程序一致，依赖 SQLite 主库 `app_config`（配合 `.env` 中 `QUANTMESH_SQLITE_PATH`）
- 新增仓库内 **`.env.example`**（本机 SQLite 默认值说明）

---

## [3.79.8-rc10] - 2026-04-03

### Fixed
- **Binance 合約 / 持倉安全檢查**：進一步處理 U 本位多資產下 **帳戶級 `availableBalance` 為 0** 但 **`totalMarginBalance` / `totalWalletBalance` 仍為正** 的情況，依序回退為保證金餘額、錢包餘額；若 WebSocket `v2/account.status` 合併後仍全 0，再請求 REST `/fapi/v2/account` 補全，避免誤判餘額為 0

---

## [3.79.8-rc9] - 2026-04-03

### Fixed
- **Binance 合約 / 持倉安全檢查**：U 本位**多資產保證金**模式下，僅累加各資產行的 `availableBalance`（USDT/USDC 等）可能為 0，但 REST/WS 帳戶根部的 `availableBalance` 仍正確；`GetAccount` 在資產行合計為 0 時回退帳戶級餘額，避免誤報「餘額 0」無法啟動 Bot

---

## [3.79.8-rc8] - 2026-04-03

### Fixed
- **Storage / MySQL / kline_files**：列名 `interval` 在 MySQL 中為保留字，對 `kline_files` 的 SQL **始終**使用反引號 `` `interval` ``（不再僅依 `dbType == "mysql"` 分支），避免 `Error 1064`（`near ', start_time, ...'`）及 K 線文件同步失敗

---

## [3.79.8-rc7] - 2026-04-03

### Added
- **API**：`GET /api/statistics/pnl/exchange` 對查詢區間設上限（90 天）；超出時將 `start_time` 截斷為 `end_time` 前 90 天，並在 JSON 中返回 `range_clamped`、`effective_start_time`、`effective_end_time`
- **i18n**：`error.invalid_time_range`（結束時間早於開始時間）

### Changed
- **Web UI**：全局儀表板與頂欄不再請求「自 2020 年 / 近 365 天」的按交易所盈亏，改為與後端一致的 **最近 90 天**（`constants/pnl.ts`）
- **Events**：`GetEventStats` 改為單次 SQL 聚合 severity 計數，減少往返；`/api/events/stats` 上下文超時由 10s 調整為 25s

---

## [3.79.8-rc6] - 2026-04-03

### Fixed
- **Storage / MySQL**：`system_settings` 與 `kline_files` 查詢中未對保留字 `key`、`value`、`type`、`interval` 加反引號，導致 MariaDB/MySQL 報 `Error 1064`（與 facev.app 日誌中「同步文件到數據庫失敗」「讀取 local_dev_mode 失敗」一致）；MySQL 路徑下改為正確引用列名
- **API**：補齊配置安全相關路由 `GET /api/config/security/status`、`POST /api/config/security/generate-key`（對應 Web UI 配置頁「安全」分頁與 `getSecurityStatus` / `generateMasterKey`），修復部署環境下 404

### Added
- **Web / 可觀測性**：Gin 中間件對單次請求處理耗時 ≥2s 額外輸出 `[GIN_SLOW]`（`logger.Warn` + Web 日志文件），便於對照瀏覽器 Network 中「等待服務器響應」與後端實際耗時

---

## [3.79.8-rc5] - 2026-04-03

### Changed
- **Web UI / i18n**：與 `zh-CN` 相同，將 `en-US` 靜態打入主包並加入 `resources`，離線（PWA）時中文、英文均可顯示；其餘語言仍按需加載，並以 `import.meta.glob` 排除 `zh-CN` / `en-US`，避免與靜態導入重複打入 lazy chunk

---

## [3.79.8-rc4] - 2026-04-03

### Changed
- **Web UI / PWA**：Workbox precache 排除動態語言包 chunk（`assets/xx-YY-*.js`），避免 Service Worker 安裝時批量預取所有語言；運行時仍按需加載當前語言（與 `i18n/config` 中 `resourcesToBackend` 一致）

---

## [3.79.8-rc3] - 2026-04-02

### Fixed
- **Storage / MySQL**：啟動時遷移 `system_metrics`、`daily_system_metrics`（原僅 SQLite `createTables` 會建表，MySQL 未建導致 `Table 'qt.system_metrics' doesn't exist`）；`SaveDailySystemMetrics` 在 MySQL 使用 `ON DUPLICATE KEY UPDATE` 替代 `INSERT OR REPLACE`

---

## [3.79.8-rc2] - 2026-04-02

### Fixed
- **Storage / MySQL**：網格買賣配對成交與 GORM `trades`（逐筆成交、`pn_l`）同庫時表名衝突，導致 `Unknown column 'pnl'`；MySQL 使用獨立表 `qm_paired_trades` 並補齊 `QueryDailyStatisticsByExchange`、`GetPnLBySymbol`、`GetPnLByTimeRange`、`GetActualProfitBySymbol`、`GetTotalBuySellQty` 等查詢走 `tradesTbl()`

---

## [3.79.8-rc1] - 2026-04-02

### Added
- **回測指標**：`CalculateMetrics` 依權益曲線採樣間隔推斷年化期數，修正波動率與夏普/索提諾的年化口徑；無成交紀錄時仍計算基於權益的收益與回撤類指標
- **網格優化器**：`OptimConfig` 支持 `validation_ratio`（時間序列末尾樣本外）、`fee_rate` / `slippage_ratio`；`OptimResult` 帶出 `hold_out_enabled`、`fee_rate_used`、`slippage_used` 與訓練/驗證指標；網格/贝叶斯/遗传均統一評估邏輯（GP/GA 用訓練集得分，最終最優按驗證集）
- **API**：`POST /api/optimizer/run` 校驗 `validation_ratio`

### Changed
- **GridStrategy**：`GetPositions` / `GetOrders` / `GetStatistics` 從 `SuperPositionManager` 聚合槽位與成交量

---

## [3.79.7-rc1] - 2026-04-02

### Added
- **配置 / AI**：支持多套「命名上游」`ai.upstreams` 与可选 `ai.default_upstream`；`news_monitor.ai_provider.upstream_ref`、`inspector.ai.upstream_ref`、各 `ai.modules.*.upstream_ref` 可引用命名上游；集中解析 `ResolveGlobalAI` / `ResolveInspectorAI` 等，Web 与智子巡检、市场解读等路径已接入；加密与导出脱敏覆盖 upstreams 内 api_key
- **Web UI**：配置页增加默认上游名与新闻分析 `upstream_ref` 表单项（中英 i18n）

### Changed
- **文档**：`docs/AI_UPSTREAM_PROFILES.md` 第 11 节与实现同步

---

## [3.79.6-rc21] - 2026-04-02

### Added
- **Web UI / Agent 聊天**：消息内图片支持点击全屏预览并用关闭按钮退出；生成视频支持放大模态播放、画中画按钮与下载（跨域失败时提示并新开标签页）

---

## [3.79.6-rc20] - 2026-04-02

### Changed
- **文档**：`docs/i18n/README.en.md` 与中文版结构对齐（三句话、对比表、简介与风险提示、完整特性与模组概览、Docker/源码快速开始、遥测与隐私、精简授权与联络）；移除过时 OpenSQT 起源与致谢段落；开发示例改为 `yarn dev`

---

## [3.79.6-rc19] - 2026-04-02

### Changed
- **文档**：根目录 `README.md` 强化价值主张（三句话、与常见方案对比表）、项目简介与风险提示措辞；去除授权段落重复句

---

## [3.79.6-rc18] - 2026-04-02

### Fixed
- **Web UI**：遥测开关读取 `localStorage` 时加入 try/catch，避免隐私模式或存储不可用导致整段初始化抛错；修正 PostHog 配置项注释

---

## [3.79.6-rc17] - 2026-03-29

### Added
- **统计**：在仓库主要 Markdown（README、docs、CONTRIBUTING 等）末尾增加阅读量像素；Web UI 在应用初始化时加载同源像素（与 `VITE_DISABLE_TELEMETRY` / `QUANTMESH_DISABLE_TELEMETRY` 一致，关闭遥测时亦不加载）

---

## [3.79.6-rc16] - 2026-03-26

### Changed
- **Web UI**：补全 Telegram Chat ID 说明文案的波兰语、泰语、乌克兰语、波斯语、乌尔都语、孟加拉语、他加禄语翻译（此前为英文占位）

---

## [3.79.6-rc15] - 2026-03-26

### Added
- **Web UI**：配置页 Telegram 区块在 Chat ID 输入框下增加多语言说明（如何发消息、`getUpdates` 取 `chat.id`、@userinfobot、群组 ID 等）

---

## [3.79.6-rc14] - 2026-03-26

### Fixed
- **通知**：Telegram 發送請求超時由 3 秒調整為 30 秒，避免跨網或官方 API 較慢時消息已送達但「測試連接」仍報 `context deadline exceeded`

---

## [3.79.6-rc13] - 2026-03-26

### Added
- **Web UI**：在 `/bots/:botId/config` 單 Bot 設置頁頂部增加說明條，提示當前僅配置該 Bot，並提供前往全局設置 `/config` 的鏈接；文案支持多語言

---

## [3.79.6-rc12] - 2026-03-26

### Fixed
- **Bot 啟動**：異步啟動失敗（如持倉安全檢查餘額不足）時，除飛書等通知外，`GET /api/bots/:id` 與列表接口返回 `last_start_error` / `last_start_error_at`；前端輪詢可立即結束並 Toast 展示原因，詳情頁頂部顯示錯誤條
- **日誌**：異步啟動失敗的應用日誌帶上交易對符號，便於「實時日誌」按關鍵字篩選命中

---

## [3.79.6-rc11] - 2026-03-24

### Changed
- **文檔**：根目錄 `README.md` 改為簡體中文；移除「專案來源／基於其他開源專案」之說明與 OpenSQT 致謝段落；繁體版移至 `docs/i18n/README.zh-TW.md`；同步多語 README 的語言導航連結

---

## [3.79.6-rc10] - 2026-03-24

### Fixed
- **存儲**：`storage.type: mysql` 時若 `storage.path` 誤填為 SQLite 路徑（如 `./data/quantmesh.db`），改為自動回退使用 `database.dsn`，避免將路徑當成 MySQL DSN 導致 `default addr for network './data' unknown`；服務狀態 API 同步按「有效 DSN」判斷是否已配置

---

## [3.79.6-rc9] - 2026-03-24

### Added
- **配置**：`ApplyDatabaseDSNFromEnv` — 啟動時在 `LoadDotEnvIfPresent` 之後應用 `QUANTMESH_DATABASE_DSN`（可選 `QUANTMESH_DATABASE_TYPE`）覆蓋 `database.dsn`，並在雙 sqlite 時同步 `storage.path`；自動生成的 `.env` 模板補充說明

---

## [3.79.6-rc8] - 2026-03-24

### Fixed
- **存儲**：`app_config` / `bot_configs` 文檔表缺失時自動幂等補建（`GetAppConfigDocument` 自癒、`MigrateYAMLToAppConfigDB` 前強制確保）；新增 `./quantmesh --repair-app-config-tables config.yaml` 與 `scripts/sql/sqlite_app_config_document_tables.sql` 便於線上手動修復

---

## [3.79.6-rc7] - 2026-03-23

### Fixed
- **測試**：NewsAPI 相關用例改為僅在 `NEWSAPI_INTEGRATION_TEST=1` 且設置 `NEWSAPI_TEST_KEY` 時執行，默認跳過；移除硬編碼 key，避免 CI/無外網環境隨機失敗

---

## [3.79.6-rc6] - 2026-03-23

### Fixed
- **Deribit**：`sendRequest` 改為官方要求的 `POST {base}/api/v2`（method 僅在 JSON-RPC 體內），避免錯誤拼成 `/api/v2/public/public/...` 導致 `11050 bad_request`
- **測試**：`TestSendRequestUsesSingleJSONRPCEndpoint` 用 httptest 鎖定路徑為 `/api/v2`

---

## [3.79.6-rc5] - 2026-03-23

### Fixed
- **交易所測試**：各 `client_test` 中數量精度改為「不得為負」（合約整數張時可為 0）；Kraken 測試用合法 base64 secret、`ContractInfo` 數值字段與 API 對齊；KuCoin/MEXC/Phemex 等適配器與前述輪次一致
- **Bitfinex**：`GetName` 顯示為 `Bitfinex`
- **CoinEx**：僅在 API 返回有效精度/幣種時覆蓋默認值
- **Deribit**：`NewAdapter` 不再強制先 `Authenticate`（公共 `get_instruments` 無需密鑰）
- **Huobi**：`contract_size` JSON 兼容數值；獲取合約失敗時從 `contractCode` 補全基礎/報價資產

---

## [3.79.6-rc4] - 2026-03-23

### Fixed
- **XT.COM**：`/v4/public/symbol` 的 `result` 為 `{ symbols: [...] }`，修正 `GetSymbol` 解析；適配器僅在 API 返回有效精度/幣種時覆蓋默認值

---

## [3.79.6-rc3] - 2026-03-23

### Fixed
- **測試**：`MockGridExchange` 實現 `position.IExchange` 全量存根，避免嵌入 nil 接口導致 `GetAccount` 等調用 panic（`TestGridStrategy_Delegation`）
- **測試**：`MockRiskExchange` 覆蓋 `GetName` / `StopKlineStream`，避免嵌入 nil `exchange.IExchange` 時 panic（`TestRiskMonitor_IsTriggered`）

---

## [3.79.6-rc2] - 2026-03-23

### Fixed
- **零參與自動遷移後二次啟動**：`config.yaml` 已歸檔且磁盤上無 YAML 時，啟動時先按 `QUANTMESH_SQLITE_PATH`（默認 `./data/quantmesh.db`）從主庫 `app_config` 加載配置，避免誤走「最小化向導」路徑
- **測試**：`MigrateYAMLToAppConfigDB` 簽名變更後更新 `storage` 單元測試

### Changed
- **.gitignore**：忽略 `config.yaml.migrated.*.bak` 歸檔文件

---

## [3.79.6-rc1] - 2026-03-23

### Added
- **主配置數據庫化（Phase A）**：主庫 SQLite/MySQL 新增 `app_config`、`app_config_history`、`bot_configs`、`bot_config_history` 表；`--migrate-app-config` 將 `config.yaml` 與 `bots/*/config.yaml` 寫入庫；啟動時若 `app_config` 有快照則優先 `LoadConfigFromJSON` 覆蓋（`QUANTMESH_USE_APP_CONFIG=0` 可禁用；重遷移需 `QUANTMESH_MIGRATE_APP_CONFIG_FORCE=1`）
- **config**：`DecryptSensitiveFields`、`LoadConfigFromJSON` 供 DB 快照與 YAML 共用解密與校驗

---

## [3.79.5-rc6] - 2026-03-23

### Added
- **文档**：新增 [主配置数据库化设计文档](docs/config-database-design.md)（`app_config` JSON 文档模型、数组与迁移策略、与 `system_settings` 过渡期关系）

---

## [3.79.5-rc5] - 2026-03-22

### Changed
- **i18n**：为 K 线周期选择器 `intervalSelector` 补全 21 种语言的翻译

---

## [3.79.5-rc4] - 2026-03-22

### Fixed
- **K线周期选择器选中态不明显**：K线深度页时间按钮（1m/5m/15m 等）选中后样式不够明显。现采用 Apple HIG 风格的 Segmented Control：胶囊轨道 + 白底阴影选中态，字重与颜色层次清晰可辨

---

## [3.79.5-rc3] - 2026-03-22

### Fixed
- **存储不可用时 Bot 无法启动**：当 storage 服务未初始化时，`isBotEnabledInDB` 直接返回「已禁用」且不读文件，导致 EnableBot 写入 `bot_states.json` 后 StartBot 仍拒绝启动。现改为 storage 为 nil 时也尝试 `isBotEnabledFromFile`，与 `saveBotStateToDB` 的写入逻辑对应。

---

## [3.79.5-rc2] - 2026-03-22

### Removed
- **CD 飞书通知**：移除 GitHub Actions CD workflow 中的 Release 成功后飞书 Webhook 通知

---

## [3.79.5-rc1] - 2026-03-22

### Added
- **Bot 列表按交易所、币种筛选**：`/bots` 列表页新增交易所、币种下拉筛选，可快速定位指定交易所或交易对的 Bot

---

## [3.79.4-rc2] - 2026-03-22

### Fixed
- **bots/create 返回 503**：當存儲服務（storageService）初始化失敗時，配置管理器（configManager）未設置到 Web，導致 `POST /api/bots/create` 返回 503。現改為在 Web 服務器啟動時即設置 configManager，與 storageService 脫鉤，避免該問題。

---

## [3.79.4-rc1] - 2026-03-22

### Added
- **K 線 API 無 Bot 支持**：`GET /api/klines` 在未啟動對應交易對 Bot 時，可通過 `exchange`+`symbol` 參數使用公開 API 拉取主流幣 K 線（如 Binance 無需 API 密鑰）
- **全局 K 線深度菜單**：側欄新增「K線深度」入口，全局模式下可先選幣種再查看 K 線圖表，無需進入某個 Bot

### Changed
- **exchange**：新增 `NewExchangeForPublicKlines`，支持 Binance 公開 K 線查詢
- **web**：`getKlines` 在 provider 為空時嘗試創建公開數據適配器

---

## [3.79.3-rc2] - 2026-03-22

### Changed
- **配置保存选项弹框逻辑**：仅在修改交易参数（网格、方向、手数等）时弹出「取消委托/平仓」选项；修改全局配置（交易所 key、app、ai 等）时直接保存，不再弹框
  - 通过 `previewConfig` 的 diff 判断变更路径，仅 `trading.*` 变更才展示保存选项模态框
  - 新增 `hasTradingParamChanges` 工具及单元测试

---

## [3.79.3-rc1] - 2026-03-22

### Added
- **價差監控 UI 啟用與配置**：價差監控可在頁面直接啟用，無需編輯 config.yaml
  - 新增 `GET/PUT /api/basis/config`，配置寫入數據庫並覆蓋 config.yaml
  - 支持動態啟停，無需重啟服務
  - 價差監控頁在服務未啟用時展示配置表單（啟用開關、檢查間隔、監控交易對），可一鍵保存並生效

### Changed
- **系統設置提供者接入**：存儲適配器現實現 SystemSettingsProvider，供價差監控等模組從數據庫讀寫配置
- **價差監控初始化**：改用 BasisMonitorController，啟動時合併數據庫與 config.yaml 的配置

---

## [3.79.2-rc4] - 2026-03-22

### Fixed
- **K线文件页面 503 错误**：当 K 线收集器未初始化时，`GET /api/kline-files` 不再返回 503
  - 新增 `monitor.ListKlineFilesFromDir`，在收集器未启动时从 `./data/kline` 目录扫描并返回文件列表
  - 下载接口在收集器为 nil 时使用默认目录 `./data/kline` 提供文件下载
  - 需 storage 服务可用；若两者皆未就绪则仍返回 503 并提示「K线收集器未初始化且存储服务未就绪」

---

## [3.79.2-rc1] - 2026-03-21

### Added
- **Bot 列表卡片显示创建与停止时间**：每个 Bot 卡片展示创建时间；若当前为已停止状态，则额外展示停止时间
  - 后端 `BotResponse` 新增 `stopped_at` 字段，从 `bot_states` 表或 `bot_states.json` 文件读取
  - `BotManager.GetStoppedAt` 支持数据库与文件 fallback

### Changed
- **Bot 列表页 Apple 风格重设计**：按苹果设计总监标准重构卡片与布局
  - 卡片：圆角 2xl、柔和阴影、悬停微动效
  - 时间信息：创建时间与停止时间独立展示，层次清晰
  - 总投入汇总区：圆角 xl、浅色背景
  - 强平价文案国际化（`botList.liquidationPrice`）

---

## [3.79.1-rc4] - 2026-03-21

### Fixed
- **配置界面无法添加 OKX 等交易所**：修复配置页交易所 API 选项卡仅显示 6 个交易所（binance/bitget/bybit/gate/edgex/bit）的问题
  - 新增 `webui/src/constants/exchanges.ts` 集中维护与后端 factory 一致的 25 个交易所列表
  - Configuration、SymbolManager 改用完整交易所列表，支持 OKX、Huobi、KuCoin、Kraken 等
  - 为 Bitget/OKX/KuCoin 在配置页增加 Passphrase 输入框
  - 补全 zh-CN、en-US 的交易所名称 i18n

---

## [3.79.1-rc3] - 2026-03-21

### Changed
- **配置页 UX 优化**：底部新增「保存更改」按钮，方便长页面快速保存；保存成功通知改为右上角可关闭的 toast

---

## [3.79.1-rc2] - 2026-03-21

### Fixed
- **市場情報 500 錯誤**：修復恐慌貪婪指數 API 解析失敗導致 HTTP 500
  - Alternative.me API 返回 `data` 為數組、`value`/`timestamp` 為字串，原代碼按單對象和數字解析導致 JSON 解碼失敗
  - 調整 `GetFearGreedIndex` 以正確解析實際 API 格式，新增單元測試驗證

---

## [3.79.0-rc1] - 2026-03-14

### Fixed
- **修復開倉管理雙重響應 Bug**：Bot 未運行時，`getOpeningControlStatus` API 不再同時返回 404 錯誤和降級數據
  - `getOpeningControlRuntimeAndController` 在找不到運行時時不再發送 HTTP 響應，僅返回 false
  - 允許調用者決定如何處理（例如從配置文件讀取降級數據）

### Added
- **震荡分析页面**：新增主菜单「震荡分析」页面，根据所选币种与时间范围（12h/24h/72h/7d）计算震荡强度指数与网格友好指数
  - 震荡强度指数（Shake Strength）：衡量价格偏离中线的程度，>1.5% 波动大、0.5%~1.5% 适合网格、<0.5% 太死水
  - 网格友好指数（Grid Friendly）：衡量上下面积对称性，≥0.7 优质震荡、0.4~0.7 一般、≤0.4 假震荡
  - 基于约 100 根 K 线计算，给出网格适合度建议与参考价格间隔
  - 支持 zh-CN、en-US、zh-TW 国际化

---

## [3.78.0-rc1] - 2026-03-14

### Added
- **存储类型选择（SQLite / MySQL）**：支持在设置中切换数据存储类型
  - 选择 SQLite 时显示「数据库文件路径」输入框
  - 选择 MySQL 时显示「MySQL 连接字符串 (DSN)」输入框
  - 线上环境推荐使用 MySQL，无需 SQLite 文件
  - MySQL 可留空 DSN 以使用 Database 配置中的 DSN

### Changed
- 服务状态页「数据存储」标签改为通用显示（不再硬编码 SQLite）
- 配置默认值：MySQL 类型时 storage.path 可留空

---

## [3.77.0-rc2] - 2026-03-13

### Added
- **做空网格 + Call 期权对冲**：期权对冲现支持做空网格场景
  - 适配器 `FetchPositions` 按网格方向拉取：LONG 拉 Put，SHORT 拉 Call
  - 覆盖率计算支持 Call 多头（delta 为正，对冲短仓）
  - 展期建议：做空时推荐 OTM Call（行权价高于现价），做多时推荐 OTM Put（行权价低于现价）
  - 配置支持 `option_hedge.direction` 或从 `grid.direction` 自动推断
  - 前端期权对冲面板根据方向显示「Put 保护」或「Call 保护」

---

## [3.77.0-rc1] - 2026-03-13

### Added
- **外部期权对冲模式（Put + 单向做多网格）**：支持用户在币安/Deribit 等平台买入看跌期权，在 QuantMesh 运行单向做多网格，实现保护性对冲
  - 新增 `option` 包：`OptionHedgePosition`、`CoverageSnapshot`、`RollSuggestion` 数据模型
  - 新增 Binance/Deribit 期权 API 适配器，统一拉取 Put 仓位
  - 新增覆盖率计算引擎（名义覆盖率、Delta 覆盖率、DTE 告警）
  - 新增展期建议引擎与执行记录 API
  - 新增 `GET/POST /api/v2/bots/:id/option-hedge/*` 接口
  - 风控配置新增 `option_hedge` 段：`target_coverage_ratio`、`min_coverage_ratio`、`dte_warning_days`
  - 前端 Bot 风控页新增「期权对冲」面板，支持同步仓位、查看覆盖率、加载展期建议、记录展期
  - 中英文 i18n 支持

---

## [3.76.0-rc27] - 2026-03-13

### Fixed
- **持倉安全性檢查失敗（API 空響應）**：當 Binance API 返回空響應（`<APIError> rsp= `）時，錯誤信息現附加排查建議（網絡/地區限制、API Key 與 testnet 不匹配、IP 白名單等）
- **常見問題文檔**：新增 Q22「持倉安全性检查失败，獲取帳戶信息失败」的故障排查說明

### Changed
- **safety 測試**：MockExchange 補全 `GetMarketType` 實現，新增 `TestAccountAPIErrorHint` 單元測試

---

## [3.76.0-rc26] - 2026-03-13

### Added
- **Bot 列表標籤與信息**：Bot 列表卡片新增創建時間、是否對沖、網格方向等標籤
  - 創建時間：顯示 Bot 創建時間（新建 Bot 自動記錄，舊 Bot 無則不顯示）
  - 對沖組：若 Bot 屬於對沖組，顯示「對沖組「組名」」標籤
  - 網格方向：顯示做多/做空/雙向標籤（LONG/SHORT/BOTH）
  - 後端 BotResponse 新增 `created_at`、`hedge_group_name`、`direction` 字段
  - BotConfig 新增 `created_at` 字段，創建 Bot 時自動寫入

---

## [3.76.0-rc25] - 2026-03-13

### Fixed
- **單 Bot 創建「該交易對已存在 Bot」**：僅當同交易對有**運行中**的 Bot 時拒絕創建；若僅有已停止的 Bot，允許創建新 Bot（同一交易對可存在多個 Bot 配置，僅一個可運行）
- 更新 `error.bot_symbol_conflict` 文案，明確為「已有 Bot 在運行時不能創建」
- 更新 `MigrateToBots` 註釋，說明允許多 Bot 配置、僅運行時衝突才拒絕

---

## [3.76.0-rc24] - 2026-03-13

### Fixed
- **對沖組創建「該交易對已存在 Bot」**：僅當同交易對有**運行中**的 Bot 時拒絕創建；若僅有已停止的 Bot，允許創建對沖組（會替換同 ID 的舊 Bot）

---

## [3.76.0-rc23] - 2026-03-13

### Added
- **已停止 Bot 的概覽持倉**：停止的 Bot 概覽頁仍顯示該交易對在交易所的持倉（當前價、未實現盈虧、持倉量、倉位價值），並備註「可能非本 Bot 開倉」
  - 後端新增 `GET /api/positions/exchange-summary`，不依賴運行中的 Bot，直接從交易所查詢持倉
  - 前端 BotDetail 概覽在 Bot 停止時調用該 API 展示持倉，每 10 秒刷新

---

## [3.76.0-rc22] - 2026-03-13

### Changed
- **風控表單佈局**：Bot 創建嚮導、回測參數中，將「止損比例」「止盈觸發比例」緊挨「啟用風控」放置，三者作為一組；「趨勢過濾」移至其後獨立顯示

---

## [3.76.0-rc21] - 2026-03-13

### Added
- **Bot 創建頁利潤間距**：bots/create 頁面新增利潤間距（profit_spread）表單欄位，可與價格間隔解耦配置；不填或填 0 時使用價格間隔

---

## [3.76.0-rc20] - 2026-03-13

### Added
- **現貨網格做空+合約做多對沖**：支援現貨網格做空 + 合約做多對沖
  - 新增 `FuturesLongStrategy`：訂閱 `target_futures_long` 信號，在合約側開多/平多以對沖現貨網格做空持倉
  - `HedgeCoordinator` 支援 `PrimaryLeg=spot` 且 `Direction=SHORT`：監聽現貨做空持倉，發送 `target_futures_long`
  - 新增模板 `spot_grid_short_futures_long_hedge`，創建時 BotIDs=[spotID, futuresID]，現貨腿啟用 UseSpotMargin
  - 前端：對沖策略選擇器新增 `futures_long` 選項，支援「現貨網格做空+合約做多對沖」創建流程

---

## [3.76.0-rc19] - 2026-03-13

### Added
- **現貨網格+合約對沖**：支援現貨網格做多 + 合約做空對沖
  - 新增 `FuturesShortStrategy`：訂閱 `target_futures_short` 信號，在合約側開空/平空以對沖現貨網格持倉
  - `HedgeCoordinator` 支援 `PrimaryLeg=spot`：監聽現貨腿持倉變化，向合約腿發送 `target_futures_short`
  - `HedgeConfig` 新增 `PrimaryLeg` 欄位（futures/spot）
  - 新增模板 `spot_grid_futures_hedge`，創建時 BotIDs=[spotID, futuresID]
  - 前端：對沖策略選擇器新增 `futures_short` 選項，支援「現貨網格+合約對沖」創建流程

---

## [3.76.0-rc18] - 2026-03-12

### Added
- **做空網格現貨做多對沖**：支援合約做空網格 + 現貨買入持倉對沖
  - 新增 `SpotLongStrategy`：訂閱 `target_spot_long` 信號，買入/賣出現貨以對沖做空網格的上漲風險
  - `HedgeCoordinator` 根據 `HedgeConfig.Direction`：LONG 發 `target_spot_short`，SHORT 發 `target_spot_long`
  - `HedgeConfig` 新增 `Direction` 欄位，從合約腿繼承
  - 前端：對沖策略選擇器新增 `spot_long` 選項，新增「網格+現貨做多對沖」模板
  - 選擇 `spot_long` 時自動將方向設為做空，並顯示對應參數配置

---

## [3.76.0-rc17] - 2026-03-12

### Changed
- **對沖 Bot 創建流程優化**：創建「網格+現貨做空對沖」時
  - 將「交易方向」（做多/做空/雙向）移至網格參數最前面，先選方向再配網格
  - 做空或雙向網格時：對沖參數標題改為「僅網格做多時生效」，並顯示橙色提示說明現貨做空對沖僅適用於做多網格
  - 雙向網格時：提示對沖行為可能不符合預期

---

## [3.76.0-rc16] - 2026-03-12

### Fixed
- **存儲初始化失敗 no such column: bot_id**：修復舊版 `risk_check_history` 表（無 bot_id 列）導致 SQLite 存儲初始化失敗
  - `createTables` 中移除對 `bot_id` 的 CREATE INDEX，改由 `migrateRiskCheckHistoryTable` 在添加列後創建，避免舊表時報錯
  - 修復後 `storageServiceProvider` 可正常設置，統計接口可查詢數據庫

---

## [3.76.0-rc15] - 2026-03-12

### Fixed
- **開倉管理 symbol_not_found**：Bot 未運行時開倉管理頁面報 404
  - 狀態/配置 API 現支援從配置文件讀取（Bots 或 Trading.Symbols），Bot 停止時可查看與編輯開倉控制配置
  - 配置更新時同時持久化到 Bots 與 Trading.Symbols，兼容新舊配置結構
  - 前端：Bot 未運行時顯示「Bot 未运行」提示並禁用暫停/恢復開關

---

## [3.76.0-rc14] - 2026-03-12

### Added
- **關閉條件**：當滿足條件時自動平倉並停止 Bot
  - 目標盈利率：盈利率達到設定值時平倉並停止
  - 虧損限制：虧損率達到設定值時平倉並停止
  - Bot 風控面板新增「關閉條件」區塊，可獨立啟用並配置
  - 觸發時發布事件、寫入停止原因（關閉條件觸發）

---

## [3.76.0-rc13] - 2026-03-12

### Changed
- **趋势过滤与风控解耦**：启用趋势过滤（下跌趋势暂停开仓）可与启用风控分开设置，修改时无依赖关系
  - Bot 创建向导、回测参数、配置页：趋势过滤开关独立于风控显示
  - 文案调整：启用风控提示改为「启用后可设置止损、止盈」；趋势检测改名为「趋势过滤」以便明确区分

---

## [3.76.0-rc12] - 2026-03-12

### Fixed
- **已停止 Bot 重啟後自動運行**：修復重大 bug，停止過的 Bot 在系統重啟後不再自動啟動
  - 存儲不可用或查詢失敗時保守返回「禁用」，避免誤啟動
  - 存儲未啟用時新增 `./data/bot_states.json` 文件 fallback，確保停止狀態持久化
  - MySQL 用戶：新增 `bot_states` 表遷移（此前僅 SQLite 有），`SetBotState` 支援 MySQL 語法

### Test
- **bot_manager_test.go**：新增 `TestBotManagerIsBotEnabledInDB_StorageUnavailable`、`TestBotManagerBotStateFileFallback`

---

## [3.76.0-rc11] - 2026-03-12

### Fixed
- **TestGetBackups panic**：當 backups 為 nil 時避免 type assertion panic

---

## [3.76.0-rc10] - 2026-03-12

### Changed
- **Binance 合約 GetAccount 優先 WebSocket**：賬戶查詢優先使用 WebSocket API（v2/account.status），失敗時回退 REST，進一步降低 REST 調用

---

## [3.76.0-rc9] - 2026-03-12

### Fixed
- **Binance API 限流優化**：GetAccount/GetPositions 等賬戶類 REST 調用加限流與緩存，降低 -1003 Too many requests 觸發
  - 合約適配器：GetAccount 加 200ms 限流、5 秒緩存，WebSocket ACCOUNT_UPDATE 時失效
  - 現貨/槓桿適配器：GetAccount、GetPositions、GetBalance 加限流
  - 資金概覽 API：3 秒內重複請求返回緩存

---

## [3.76.0-rc8] - 2026-03-12

### Fixed
- **Bot 列表页停止行为**：列表页点击停止时与 Bot 详情页保持一致，弹出确认对话框询问「仅停止」或「停止并平仓」，避免误操作

---

## [3.76.0-rc7] - 2026-03-12

### Fixed
- **创建对冲组 503**：`POST /api/bot-groups` 在 `configManager` 未初始化时返回 503；现改为检查 `fileConfigManager`（与配置读写一致），在 Web 服务就绪后即可创建对冲组

### Added
- **网格+现货做空对冲参数**：创建「网格+现货做空对冲」时，步骤 3 新增现货腿参数配置：做空名义敞口比例、触发对冲阈值、再平衡间隔，支持用户自定义对冲行为

### Test
- **web/api_bots_test.go**：新增 `TestPostBotGroupCreateWorksWithFileConfigManagerOnly`，验证仅 `fileConfigManager` 时创建对冲组成功

---

## [3.76.0-rc6] - 2026-03-12

### Added
- **对冲单腿主动处置**：`BotManager` 新增单腿运行超时自动暂停开仓机制（默认 30 秒），避免对冲组长时间半边裸奔
- **对冲风险事件增强**：单腿超时触发 `risk_triggered` 事件（`issue=single_leg_running`、`action=pause_opening`），便于告警系统联动

### Test
- **bot_manager_test.go**：新增 `TestBotManagerAutoPausesSingleLegAfterGrace`，验证单腿超时后自动暂停开仓

---

## [3.76.0-rc5] - 2026-03-12

### Added
- **FIX 配置化（P2）**：`config.fix.enabled` 开关（預設 true），关闭时所有 FIX API 返回 503；`config.fix.heartbeat_timeout_sec` 心跳超时秒数（預設 120），支持按环境调整

### Test
- **web/api_fix_test.go**：`TestFixDisabledReturns503` 验证 FIX 关闭时 503；`TestFixHeartbeatTimeoutConfig` 验证可配置超时生效

---

## [3.76.0-rc4] - 2026-03-12

### Added
- **FIX 登出 API**：`POST /api/fix/sessions/logout` 支持主动登出 FIX 会话，将 `is_logged_on=false` 并清空内存绑定
- **FIX 管理页**：Web UI 新增 FIX 管理（`/fix`），会话列表与订单列表双 Tab，支持查看会话状态、最后心跳、主动登出；订单列表展示 ClOrdID、交易对、方向、状态、内部订单映射
- **FIX 管理 i18n**：`fixManagement` 与 `sidebar.fixManagement` 文案已接入中英等多语言

---

## [3.76.0-rc3] - 2026-03-12

### Added
- **FIX 会话绑定持久化**：`fixLogonSession` 将 `bot_id` 写入 `FixSessionState`，进程重启后可从存储恢复绑定，`resolveFixExecutionContext` 优先使用存储字段
- **FIX 会话超时失活**：心跳超过 120 秒未更新则标记 `is_logged_on=false` 并拒单，返回 `session heartbeat timeout`
- **FIX 审计与指标**：登录/新单/撤单/改单/超时均记录 logger 与 Prometheus 指标（`quantmesh_fix_session_logon_total`、`quantmesh_fix_order_total`、`quantmesh_fix_session_timeout_total`）

### Test
- **web/api_fix_test.go**：`TestFixLogonAndHeartbeat` 增加 BotID 持久化断言；新增 `TestFixSessionTimeout` 验证超时拒单与会话失活

---

## [3.76.0-rc2] - 2026-03-12

### Added
- **FIX 执行链路（单会话单 Bot）**：新增 FIX 登录/心跳/新单/撤单/改单 API，支持会话绑定 `bot_id` 并直接路由到运行中 Bot 的交易所实例执行
- **FIX 会话绑定管理**：新增会话到 `bot_id` 的绑定管理逻辑，支持同会话后续报文走同一执行 Bot

### Test
- **web/api_fix_test.go**：新增 `TestFixLogonAndHeartbeat`，覆盖 FIX 会话登录绑定与心跳更新

---

## [3.76.0-rc1] - 2026-03-12

### Added
- **FIX 持久化基础能力**：新增 `fix_session_states` 与 `fix_order_links` 表，用于 FIX 会话序号恢复与主订单到内部订单映射，支持后续 Acceptor/Initiator 双模式接入
- **FIX 存储接口扩展**：`storage.Storage` 新增 FIX 会话与订单映射读写接口（upsert/query/list），便于协议层与 API 层复用
- **FIX 查询 API**：新增 `GET /api/fix/sessions` 与 `GET /api/fix/orders`，支持按会话与订单状态查看 FIX 运行轨迹

### Test
- **storage/sqlite_test.go**：新增 `TestFixSessionStateCRUD`、`TestFixOrderLinkCRUD`，覆盖 FIX 会话状态与订单映射的增改查列表

---

## [3.75.0-rc4] - 2026-03-12

### Added
- **对冲组一致性巡检与状态输出**：`GET /api/bot-groups` 与 `GET /api/bot-groups/:id` 返回新增 `consistency` 字段（`status`、`alert`、运行/停止腿列表），支持识别 `single_leg_running` 场景

### Fixed
- **单腿运行事件告警**：当对冲组从双腿变为单腿运行时，`BotManager` 自动发布告警事件并记录 warning 日志；双腿恢复后发布恢复事件，降低“半边仓位裸奔”风险

### Test
- **bot_manager_test.go**：新增 `TestBotManagerWarnsOnSingleLegRunning`，验证单腿告警与恢复事件
- **web/api_bots_test.go**：新增 `TestGetBotGroupByIDIncludesConsistency`，验证组详情返回一致性巡检结果

---

## [3.75.0-rc3] - 2026-03-12

### Fixed
- **风控历史归属增强**：`risk_check_history` 新增 `bot_id`、`exchange`、`market_type` 字段并补充索引，风控检查记录可明确归属到 Bot，避免多 Bot 同交易对时历史混淆
- **风险历史查询支持 Bot 过滤**：`GET /api/risk/history` 与导出 `GET /api/export/risk-checks` 新增 `bot_id` 过滤参数，支持按 Bot 精准查看风控轨迹

### Test
- **storage/sqlite_test.go**：新增 `TestQueryRiskCheckHistoryByBotID` 验证按 `bot_id` 过滤
- **web/api_risk_history_test.go**：新增 `TestGetRiskCheckHistoryWithBotIDFilter` 验证 API 过滤生效

---

## [3.75.0-rc2] - 2026-03-12

### Fixed
- **Bot 归属与隔离增强**：订单存储新增 `bot_id` 并改为按 `(exchange, order_id)` 做唯一约束，修复同 `order_id` 在不同交易所/不同 Bot 场景下互相覆盖的问题
- **BotManager 并发安全**：为运行时 `runtimes` 读写加锁，修复并发启停/查询时可能触发的 map 竞态与崩溃风险
- **Bot Group 删除安全**：删除组前先逐个停止组内 Bot，避免配置删除后出现“孤儿腿”继续交易

### Test
- **bot_manager_test.go**：新增 `TestBotManagerConcurrentAccessNoPanic`，覆盖并发读写稳定性
- **storage/sqlite_test.go**：新增 `TestSaveOrderKeepIsolationByExchangeAndBotID`，验证同 `order_id` 跨交易所/Bot 隔离
- **web/api_bots_test.go**：新增 `TestDeleteBotGroupStopsRunningBotsBeforeRemove`，验证删除组时先停机

---

## [3.75.0-rc1] - 2026-03-12

### Fixed
- **PostOnly/ReduceOnly 全覆蓋**：修復策略層與平倉路徑未正確傳遞 PostOnly/ReduceOnly 的問題
  - `strategy/martingale.go`：開倉/加倉/平倉單補全 PostOnly、平倉補全 ReduceOnly
  - `strategy/dca_enhanced.go`：開倉/加倉/平倉單補全 PostOnly、平倉補全 ReduceOnly
  - `strategy/spot_short.go`：賣出/買回單補全 PostOnly
  - `position/close_manager.go`：限價平倉單啟用 PostOnly
  - `position/exchange_wrapper.go`：移除 PostOnly 硬編碼，改為透傳 req.PostOnly

---

## [3.75.0] - 2026-03-12

### Added
- **Bot Detail 当前委托 Tab**：在机器人详情页新增「当前委托」标签页，直接从交易所实时查询该交易对的全部开放委托，并标注哪些是本机器人管理的委托（`is_mine`）vs 未知来源委托
- **一键清理交易所委托**：新增「一键清理全部委托」功能按钮，一次性取消该交易对在交易所上的所有挂单，方便在异常情况下快速清盘
- **后端 API**：新增 `GET /api/orders/exchange-open` 直接查交易所开放委托；`POST /api/orders/cancel-all-exchange` 一键取消全部

---

## [3.74.7] - 2026-03-12

### Added
- **VolatilityIndicator 波動率指標組件**：新增 K 線圖波動率面板，展示 24h ATR%、上行/下行偏差、多空偏向及波動強度（Calm/Normal/Active/Extreme）；支持 24 種語言國際化

### Fixed
- **平倉委託價格保護（LONG + SHORT 雙向）**：波動/滑點導致實際成交價偏離網格價時，平倉委託價格現統一以實際開倉均價為基準計算，防止虧損出場
  - LONG：賣出價 = `max(slotPrice, AvgBuyPrice) + spread`
  - SHORT：買回價 = `min(slotPrice, AvgOpenPrice) - spread`

---

## [3.74.6-rc4] - 2026-03-12

### Fixed
- **SHORT 方向平倉買單價格未保證低於實際開空均價**：SHORT 波動/滑點時，若實際賣出價低於網格檔位價，原邏輯仍用 `slotPrice - spread` 計算買回價，可能高於成本導致虧損；現改為以 `min(slotPrice, AvgOpenPrice) - spread` 為基準，與 LONG 方向修復對稱

### Test
- **position/super_position_manager_test.go**：新增 `TestCloseOrderPriceBelowAvgOpenPriceForShort` 驗證 SHORT 方向修復

## [3.74.6-rc3] - 2026-03-12

### Fixed
- **平倉賣單價格未保證高於實際買入價（LONG）**：波動/滑點時，若實際成交價高於網格檔位價，原邏輯僅用 `slotPrice + spread` 計算賣單價，可能低於成本導致虧損；現改為以 `max(slotPrice, AvgBuyPrice) + spread` 為基準，確保賣出價始終高於實際買入均價

### Test
- **position/super_position_manager_test.go**：新增 `TestCloseOrderPriceAboveAvgBuyPrice` 驗證修復

## [3.74.6-rc2] - 2026-03-12

### Fixed
- **Bot 風控更新 400 Bad Request**：網格風控 `stop_loss_ratio` 等比例欄位由 `DecimalNumberInput` 傳入 string（如 "15"）時未轉為 number，Go 後端無法 unmarshal；現抽取 `normalizeGridRiskControlPayload` 統一將 string 轉為 float64，並正確處理 % 與 0-1 比例

### Test
- **gridRiskControlPayload.test.ts**：新增 `toRatio`、`normalizeGridRiskControlPayload` 單元測試

## [3.74.6-rc1] - 2026-03-12

### Added
- **通知渠道獨立開關 UI**：配置頁面為 Telegram、Webhook、郵件、釘釘、企業微信、Slack 各渠道新增啟用/關閉開關，與飛書一致；支持 24 種語言的國際化文案

## [3.74.5-rc1] - 2026-03-12

### Added
- **Bot 詳情風控標籤 - 網格風控展示與編輯**：Bot Detail 風控標籤新增「網格風控」區塊，展示並可編輯 stop_loss_ratio、take_profit_trigger_ratio、trailing_take_profit_ratio、max_grid_layers、trend_filter_enabled；支持運行時熱更新並持久化到配置文件
- **網格風控觸發止損時發送通知**：`super_position_manager` 觸發硬止損時發布 `EventTypeStopLoss` 事件，會觸發飛書/郵件通知（需在 `notifications.rules.stop_loss` 啟用）

## [3.74.4-rc2] - 2026-03-12

### Fixed
- **線上更改配置時 HTTP 500「配置管理器未初始化」**：`previewConfigHandler`、`updateConfigHandler`、`restoreBackupHandler`、`restoreConfigHistoryHandler`、`updateConfigYAMLHandler` 錯誤地檢查 `configManager`（cfgmgr），而該變量僅在 configStorage 初始化後才注入；實際配置讀寫使用 `fileConfigManager`，現改為檢查 `fileConfigManager`，與 `getConfigHandler` 一致

## [3.74.4-rc1] - 2026-03-12

### Added
- **Bot 詳情頁面優化**：持倉數量、當前倉位、實際占用資金等移至概覽標籤；風控標籤「啟用 Bot 獨立風控」區塊預設展開；新增「止損、止盈」標籤匯總展示 stop_loss/take_profit 訂單；實時日誌標籤進入時自動加載最新 50 條

## [3.74.3-rc1] - 2026-03-12

### Added
- **ClientOrderID 編碼訂單來源**：止損平倉單的 ClientOrderID 追加 `_SL` 後綴，幣安等交易所會原樣存儲並返回，可從訂單歷史/WebSocket 中解析 order_source；`utils.ParseOrderSource` 支持從任意 ClientOrderID 解析；WebSocket 訂單更新與 strategyMap 未命中時自動從 ClientOrderID 回填 order_source

### Test
- **utils/orderid_test.go**：新增 TestGenerateOrderIDWithSource、TestParseOrderSource、TestParseOrderIDWithSLSuffix

## [3.74.2-rc3] - 2026-03-12

### Fixed
- **storage 構建失敗 no required module provides package github.com/lib/pq**：移除 PostgreSQL (lib/pq) 依賴，PostgreSQL 存儲類型暫不支持，配置時返回明確錯誤提示使用 sqlite 或 mysql

### Test
- **storage/sqlite_test.go**：新增 TestPostgresUnsupported 驗證 postgres 配置時錯誤訊息

## [3.74.2-rc2] - 2026-03-12

### Fixed
- **Binance API 限流導致 quantmesh panic 崩潰**：當 GetAccount 因限流（-1003 Too many requests）失敗時，accountResult 可能為 `(*T)(nil)` 轉 interface{}，對其 reflect.Elem() 後調用 FieldByName 會 panic；現增加 IsValid/IsNil/Kind 檢查，避免對 zero Value 做 reflect 操作
- **position/super_position_manager.go**：獲取 AccountLeverage 時增加 IsNil() 與 Struct 類型檢查，避免 API 失敗時 reflect panic

### Test
- **position/super_position_manager_test.go**：新增 TestAdjustOrders_GetAccountFails_NoPanic、TestAdjustOrders_GetAccountReturnsNil_NoPanic 驗證修復

## [3.74.2-rc1] - 2026-03-12

### Changed
- **Bot 詳情實時日誌優化**：時間列改為簡短格式（HH:mm:ss）節省空間；日誌內容懸停顯示完整 tooltip；雙擊行可複製整條日誌到剪貼板

## [3.74.1-rc3] - 2026-03-12

### Fixed
- **CI 構建失敗 undefined: RocketTieredGridConfig**：將 RocketTieredGridConfig、RocketTier 類型定義從 bot_config.go 移至 config.go，解決 linux/amd64 CGO 構建時符號未定義錯誤

## [3.74.1-rc2] - 2026-03-11

### Changed
- 版本號升級

## [3.74.1-rc1] - 2026-03-11

### Added
- **網格+現貨做空對沖 Bot**：合約網格做多 + 現貨借幣做空對沖策略
  - 合約腿：單邊做多網格；現貨腿：現貨借幣做空（Binance Spot Margin）
  - 倉位關係：現貨做空 ≈ 網格名義敞口 × ShortNotionalRatio（預設 25%）
  - 觸發：網格滿 N 格（HedgeTriggerLayers，預設 3）後才開現貨空倉
  - HedgeCoordinator 監聽 OrderFilled 事件，計算目標空倉並發送 EventTypeHedgeSignal
  - spot_short 策略訂閱對沖信號，執行借幣/賣出或買回/還幣
  - 前端：對沖策略選擇器新增 spot_short 選項，新增 grid_spot_short_hedge 模板
  - 配置：`config.HedgeConfig` 支援 ShortNotionalRatio、HedgeTriggerLayers；BotConfig 支援 UseSpotMargin

### Test
- **strategy/hedge_coordinator_test.go**：新增 GetTargetSpotPosition、getFloat64、getInt、getString 單元測試

## [3.74.0-rc8] - 2026-03-11

### Fixed
- **智能開倉掛單 max_open_orders 未生效**：設置 `smart_order.max_open_orders=3` 後仍出現 10 個開倉委託；現每次 AdjustOrders 與 SmartOrderManager 定期檢查時，若開倉單數超過上限則動態撤銷最遠的委託單（做多撤高價買單、做空撤低價賣單）
- **FilterSlotsByMaxOpenOrders 做空取錯槽位**：做空時原取高價（最遠）槽位，現改為取低價（最接近當前價）槽位
- **SmartOrder 配置未傳遞到運行時**：SymbolConfig 新增 SmartOrder 欄位，BotConfigToSymbolConfig 與 symbol_manager 正確傳遞 SmartOrder 到 Trading 配置，確保 Bot 創建時設定的智能掛單參數生效
- **grid_auto_rebuild 日誌格式錯誤**：fmt.Sprintf 參數順序與格式符不匹配導致編譯失敗

## [3.74.0-rc7] - 2026-03-11

### Fixed
- **order_placed 仍報「ON CONFLICT clause does not match any PRIMARY KEY or UNIQUE constraint」**：根因是 createTables 曾創建 `CREATE INDEX idx_orders_order_id`（非 UNIQUE），遷移時因索引已存在而跳過創建唯一索引；現遷移會檢測並刪除非唯一/partial 索引後重建，createTables 改為直接創建 `CREATE UNIQUE INDEX`

### Test
- **storage/sqlite_test.go**：新增 `TestSaveOrderWithNonUniqueIndexMigration`，驗證非唯一索引遷移場景

## [3.74.0-rc6] - 2026-03-11

### Changed
- 版本號升級，用於發佈標籤

## [3.74.0-rc5] - 2026-03-11

### Fixed
- **Bot 啟動失敗「ON CONFLICT clause does not match any PRIMARY KEY or UNIQUE constraint」**：`order_placed` 等事件寫入 orders 表時，舊版遷移創建的 partial unique index 無法被 `ON CONFLICT(order_id)` 匹配；現遷移改為創建完整唯一索引，並在啟動時檢測並替換已有的 partial 索引，確保 SaveOrder upsert 正常

### Test
- **storage/sqlite_test.go**：新增 `TestSaveOrderUpsert`、`TestSaveOrderWithPartialIndexMigration`，驗證訂單 upsert 與 partial 索引遷移

## [3.74.0-rc4] - 2026-03-11

### Fixed
- **Bot Detail 保存參數時 500「配置管理器未初始化」**：`GET /api/config/json` 錯誤地檢查 `configManager`（cfgmgr），而該變量僅在 configStorage 初始化後才注入；實際配置數據來自 `fileConfigManager`，現改為檢查 `fileConfigManager`，與 `getConfigHandler` 一致

## [3.74.0-rc3] - 2026-03-11

### Fixed
- **Bot Detail 啟動失敗（bot_disabled_in_database）**：用戶通過 Web UI 停止後再次點擊啟動時，後端因數據庫中禁用標記導致 StartBot 失敗，但 API 已返回 202，前端輪詢 60 秒無果；現在 postBotStart 中先調用 EnableBot 清除禁用標記再異步啟動，確保啟動流程正常

### Test
- **api_bots_test.go**：新增 `TestPostBotStartCallsEnableBotBeforeStart`，驗證 postBotStart 在啟動前會調用 EnableBot

## [3.74.0-rc2] - 2026-03-11

### Fixed
- **MySQL 配置存儲初始化失敗導致 panic**：當 MySQL 連接失敗（如 Error 1449 definer 不存在）時，原邏輯會導致 nil 指針 panic；現增加防禦性 nil 檢查，並在 MySQL 失敗時自動回退到 SQLite，確保服務可正常啟動

### Test
- **config_mysql_test.go**：新增 `TestMySQLConfigStorage_InitializeConfigs_NilDB`，驗證 db 為 nil 時返回錯誤而非 panic

## [3.74.0] - 2026-03-11

### Added
- **全局持倉管理頁**：全局側邊欄新增「全局持倉」入口（`/global-positions`），匯總展示所有 Bot 的持倉情況、未實現盈虧；支持展開查看每個持倉的開倉委託與平倉委託；頂部卡片顯示總浮盈、總倉位價值、持倉對數；每 10 秒自動刷新
- **分批止盈止損規則**：每個持倉提供「止盈止損」入口，可同時設置多條條件規則；每條規則可配置觸發價格、委託價格（留空則市價）、賣出比例（全倉/1/2/1/3/1/4）、方向（止盈/止損）、平倉方式（限價/市價）；規則以 localStorage 持久化，前端每 5 秒輪詢當前價格，觸發後自動調用 closePositionsV2 執行平倉
- **後端部分平倉支持**：`ClosePositionConfig` 與 `ClosePositionsV2Request` 新增 `quantity_ratio` 參數（0~1），支持按比例平倉
- **positions/summary/all 補充 bot_id**：全局持倉彙總 API 響應中補充 `bot_id` 字段，方便前端直接跳轉至對應 Bot 看板

## [3.73.3] - 2026-03-11

### Added
- **訂單管理總價格與資金占用**：待成交與歷史訂單表格新增「總價格」（價格×數量）與「資金占用」（名義價值÷槓桿，即實際保證金）列；後端 API 返回 `leverage` 供前端計算；資金占用列標題帶 tooltip 說明

### Fixed
- **訂單管理「取消全部」報交易所不存在**：當無運行中的 bot 時，`exchangeGetterFunc` 返回 nil 導致「交易所 binance 不存在」。現改為優先從運行中 bot 獲取交易所，若無則從 `globalConfig` 按需創建，支持在 bot 未運行時也能取消訂單；批量/單筆取消 API 新增 `market_type` 參數

## [3.73.2] - 2026-03-11

### Fixed
- **智能掛單開關刷新後還原**：啟用 smartOrderEnabled 後刷新頁面又變關閉；根因是策略更新 API 未將新配置推送到運行中的 Bot，且 UpdateRuntimeTradingParams 僅在交易參數變更時才同步 Config。現策略更新後立即調用 UpdateTradingParams 推送配置，且運行時始終同步 Config（含 smart_order、風控等），確保刷新後正確顯示
- **智能掛單配置未多語系化**：補全 botDetail.strategy 下 smartOrderConfig、smartOrderDescription、smartOrderEnabled、maxOpenOrders、maxOpenOrdersHint、openOrderDistance、openOrderDistanceHint、smartOrderEffect 的 zh-CN / en-US 翻譯；保存策略後自動刷新 Bot 詳情

## [3.71.7] - 2026-03-09

### Fixed
- **對账歷史卡片與列表不一致**：對账頁面頂部「對账歷史」卡片原顯示運行時對账次數（重啟後歸零），與下方列表（數據庫記錄）不一致；現改為顯示數據庫中的對账歷史記錄數，與列表一致
- **對账 API 未傳 market_type**：對账狀態/歷史/聚合 API 請求現補充 `market_type` 參數，確保合約/現貨交易對能正確解析到對應的 PositionProvider，修復「本地持倉為 0 但對账記錄有差異」時 provider 錯配導致的顯示問題

## [3.71.3] - 2026-03-09

### Fixed
- **歷史訂單賣出單不顯示**：根因是 `orders` 表部分訂單 `exchange` 字段為空，前端篩選 binance 時被排除；`order_placed` 事件現補充 `exchange` 字段，新下單從創建時即正確寫入；線上已對 PAXGUSDT 等補全 `exchange='binance'`

## [3.71.0] - 2026-03-09

### Changed
- **前後端版本號同步**：main.go 與 package.json 統一為 3.71.0
- **Footer 版本號樣式**：版本號與版權聲明使用相同字體、顏色與字重
- **Footer 版本號 i18n**：版本資訊改為多語系 key `footer.versionInfo`，支援各語系

## [3.67.0-rc3] - 2026-03-09

### Fixed
- **Bot 啟動請求超時**：啟動 Bot 時需連接 WebSocket、獲取價格等耗時操作，同步阻塞導致請求超時（「Failed to load resource: the server」）；現改為異步啟動，POST 立即返回 202 Accepted，前端輪詢狀態直到運行或超時，避免長時間阻塞

## [3.67.0-rc2] - 2026-03-09

### Fixed
- **創建 Bot 後風控參數不可見**：創建 Bot 時填寫的網格風控（止損、止盈、趨勢檢測等）未寫入配置，導致建好後詳情頁看不到；現後端 `postBotCreate` 與 `postBotGroupCreate` 正確解析並保存 `grid_risk_control_*` 欄位到 BotConfig.GridRiskControl
- **Bot 詳情風控狀態未翻譯**：趨勢檢測、風控狀態的「啟用/禁用」顯示為 `COMMON.DISABLED` 等原始 key；為 `t('common.enabled')`、`t('common.disabled')` 增加 `defaultValue` 兜底，確保任何語言環境下均顯示正確譯文

## [3.67.0-rc1] - 2026-03-09

### Fixed
- **創建 Bot 頁價格/數量等無法輸入小數**：bots/create 頁面的價格間隔、訂單數量、買賣窗口、止損比例、止盈觸發比例等欄位改用 DecimalNumberInput，修復 Chakra NumberInput 輸入 "3." 時小數點丟失的問題；StrategyParamForm 策略參數的 number 類型同樣改用 DecimalNumberInput

## [3.65.0-rc10] - 2026-03-09

### Added
- **回測期末結算明細**：多策略回測報告新增「期末結算明細」章節，後端結構化欄位 `end_settlement`（liquidated、liquidation_price、liquidation_qty、liquidation_amount），一眼區分「估值收官」與「強平收官」；強平觸發時記錄價格/數量/金額供報告展示

## [3.65.0-rc9] - 2026-03-09

### Added
- **網格回測報告完整參數註明**：報告回測配置表現註明間距、格子數、單筆訂單大小、手續費率、風控-成交量倍數、風控-均線窗口、利潤間距（止盈）等；參數鍵轉為可讀中文標籤，手續費率以百分比顯示；網格策略（含風控對比）自動合併實際使用的參數

## [3.65.0-rc8] - 2026-03-09

### Added
- **回測報告做空持倉與期末净價值**：做空時持倉為負債（欠基幣），報告與前端現顯示「期末欠（基幣）」「倉位負債價值」；新增「期末净價值」（做多=USDT+持倉市值，做空=USDT-負債）；`computeEndPosition` 支援 SHORT 方向負持倉

## [3.65.0-rc6] - 2026-03-09

### Fixed
- **回測交易記錄時間戳穿越**：單策略交易記錄的 Timestamp 為毫秒，但 API 層用 `time.Unix` 當秒處理，導致頁面顯示「58116-06-07」等未來時間；修正為 `time.UnixMilli`，同步修正測試數據

## [3.65.0-rc4] - 2026-03-09

### Fixed
- **做空網格回測零交易**：`buildGridLevelsBySpacing` 從最低價往上構建檔位並受 `maxCount` 截斷，導致 SHORT 方向檔位全部集中在價格低位區間（如 60222–61552），而實際交易價格在 63000–67000，永遠不會上穿這些檔位開空。新增 `buildGridLevelsBySpacingFromHigh` 從最高價往下構建，確保做空檔位覆蓋高位區間；BOTH 方向不限制數量以覆蓋全區間
- **做空風控觸發方向反轉**：風控模擬器原固定以「價格低於均價 + 放量」觸發（針對做多），做空時應以「價格高於均價 + 放量」觸發（看漲對空頭不利）。`RiskSimulatorConfig` 新增 `Direction` 欄位，`Check` 方法根據方向翻轉觸發條件

## [3.65.0-rc3] - 2026-03-09

### Fixed
- **單向做空網格回測方向未生效**：前端 `normalizeParamsForApi` 僅保留數字參數，導致 `direction: "SHORT"` 被過濾；後端 `grid_adapter` 未支援 Direction、`gridParamsFromTask` 未傳遞。現前端保留字串/布林參數，後端新增 Direction 支援做空邏輯（價格上漲開空、下跌平空），報告正確顯示網格方向

## [3.65.0-rc2] - 2026-03-09

### Fixed
- **Bot 详情策略类型仅显示第一个**：列表页展示「网格 70% + DCA 30%」等多策略组合，详情页仅显示「网格交易策略」；现多策略时详情页以标签形式展示全部策略及权重，与列表一致

## [3.64.12] - 2026-03-09

### Added
- **回測風控對比顯示網格方向**：網格策略回測的風控對比區塊新增「網格方向」標示（開多/開空/雙向網格），便於區分不同方向的回測結果；前端與 Markdown 報告均支援

## [3.64.11-rc2] - 2026-03-09

### Added
- **回測交易記錄顯示交易後狀態**：每筆交易後記錄並展示當前持倉量、剩餘資金、持倉方向（LONG/SHORT/空）；線上查看、CSV/JSON 導出均含新欄位

## [3.64.11-rc1] - 2026-03-09

### Fixed
- **回測單向做多仍出現 SHORT 交易**：`shouldGenerateOrder` 原對所有方向均返回 true，未實際過濾；現 LONG 模式下無多頭持倉時不生成賣單、SHORT 模式下無空頭持倉時不生成買單；引擎層增加 PositionMode 防護，禁止平多後剩餘部分開空、平空後剩餘部分開多

## [3.64.10] - 2026-03-09

### Fixed
- **網格回測零交易**：原邏輯僅用相鄰 K 線收盤價判斷穿越，1 分鐘內很少波動 130+ USDT 導致零交易；現改為利用 K 線 High/Low 檢測檔位穿越，與實盤行為一致

## [3.64.9] - 2026-03-09

### Added
- **回测期末强制平仓**：多策略回测结束时强制平仓所有剩余仓位，确保最终权益为已实现盈亏；权益曲线追加平仓后终点；报告中明确标注「期末已强制平仓」；导出 CSV 时强制平仓交易标注为 `[期末强制平仓]`

## [3.64.8] - 2026-03-09

### Fixed
- **多策略回测交易导出 PnL 全为 0**：多策略模式导出时原使用 TickTrade（单笔成交），无 PnL 字段；现优先使用 CompletedTrades（成对平仓记录）导出，包含真实 PnL 与 Fee
- **策略权重显示 1% 而非 100%**：权重为 0-1 区间时现正确转为百分比显示（1 → 100%）；单策略时默认显示 100%
- **各策略胜率与总览不一致**：各策略表现表中的 WinRate、MaxDrawdown 原未从 RiskMetrics 同步，现正确填充
- **策略配置显示 map[]**：配置改为可读格式（如 grid_count=20, price_interval=0.5），空配置显示「（無）」

## [3.62.4-rc4] - 2026-03-08

### Fixed
- **回测交易记录下载无反应**：多策略/组合回测结果仅存于 `multi_result`，原导出接口只读 `Result.Trades` 导致 404；现支持单策略与多策略两种结果格式导出 CSV/JSON
- **各策略表现表格渲染异常**：多策略报告中表格行间多余空行导致 GFM 解析失败，表体显示为原始 markdown 文本；模板使用 `{{range .Strategies -}}` 去除空行
- **下载失败无提示**：导出失败时增加 toast 错误提示

## [3.62.4] - 2026-03-08

正式发布，整合 3.62.4-rc1 ~ rc3 的修复。

### Fixed
- **回测报告标签错误**：多策略回测结果中「手续费率」实际显示的是总手续费（USDT），标签改为「总手续费」
- **全面支持小数输入**：所有需小数的 NumberInput 改为 DecimalNumberInput
- **回测参数无法输入小数**：网格间距、单笔订单大小等支持小数点输入
- **Bot 详情跳转回测参数未完全填入**：grid_spacing、order_quantity、grid_count 正确预填

## [3.62.4-rc2] - 2026-03-08

### Fixed
- **全面支持小数输入**：所有需小数的 NumberInput 改为 DecimalNumberInput，确保能正常输入小数点
  - HybridStrategyConfig：子策略权重 (0–1)
  - OptimizerPage：初始资金、价格区间、订单量、lambda、最大迭代等
  - Configuration 网格风控：止损比例、止盈触发比例、追踪止盈比例
  - 保存前将百分比字符串转为数值

## [3.62.4-rc1] - 2026-03-08

### Fixed
- **回测参数无法输入小数**：网格间距、单笔订单大小等数字输入框无法输入小数点（如 3.5）
  - 后端为 grid_spacing、order_quantity 添加 Step: 0.01 支持小数
  - 前端 NumberInput 在 onChange 中保留字符串中间态（如 "3."），避免输入小数时丢失小数点
  - 提交前将字符串参数转为数字

## [3.62.3] - 2026-03-08

### Fixed
- **Bot 详情跳转回测参数未完全填入**：从 Bot 详情「前往回测」跳转时，URL 携带的 grid_spacing、order_quantity、grid_count 未正确填入表单
  - BotDetail.buildBacktestUrl 补充 grid_count 参数（来自 max_position_layers 或 buy_window_size + sell_window_size）
  - BacktestMenu 解析并应用 url grid_count 参数
  - loadConfigParams 改为合并而非覆盖，保留 URL 预填参数优先；API 返回的 price_interval 映射为 grid_spacing

## [3.62.1-rc1] - 2026-03-08

### Added
- **MySQL 8 支持**：新增 MySQL 8 数据库兼容性
  - 添加 MySQL 8 配置指南和验证报告
  - 更新 database/gorm.go 以支持 MySQL 8
  - 新增 docs/config/examples/config-mysql8-example.yaml 配置示例

### Fixed
- **登录认证流程**：修复登录页面 401 错误导致的刷新循环
- **回测系统**：改进回测报告生成器和任务管理器
- **应用安全**：添加运行时守卫 (appRuntimeGuards)
- **K线文件迁移**：优化 K线文件迁移逻辑

### Changed
- 清理旧的回测缓存文件
- 更新 WebUI 构建产物

## [3.61.3-rc1] - 2026-03-08

### Added
- **回测多策略组合**：新建回测时「多策略组合」可正常选中并保持
  - 修复切换多策略时只添加一个策略导致单选立刻回退到单策略的 bug，改为至少添加两个策略
- **多策略下每策略参数**：多策略模式下每个策略卡片内展示该策略参数（如网格的网格间距、格子数等）
  - 参数写入各策略的 config，与后端 TaskStrategy.Config 一致；单策略时仍使用底部全局参数表单

## [3.61.2-rc4] - 2026-03-08

### Fixed
- **登录页反复刷新**：为 `/login`、`/setup`、`/config-setup`、`/wizard` 增加运行时守卫，避免认证流程被 Service Worker 和后端探测抢跑干扰
  - 在登录类页面进入时主动注销已有 Service Worker，减少旧缓存和自动更新导致的反复 reload
  - 恢复 ConnectionStatusBanner 在认证流程页的首次后端探测延迟，避免与登录页初始化请求叠加
  - 开发环境禁用 PWA Service Worker，避免本地调试时登录页被 dev SW 反复接管
  - 新增 `appRuntimeGuards` 单元测试，覆盖认证路由识别与探测延迟逻辑

## [3.61.2-rc3] - 2026-03-07

### Fixed
- **回测页 i18n 不全**：修复 en-US/zh-CN 中重复的 `backtest` 键导致后者覆盖前者、BacktestMenu 翻译丢失的问题
  - 合并两处 backtest 对象，补全 BotBacktestDialog 所需键（executionCompleted、config、feeRate、fundingRate 等）
  - 为 de-DE 补全缺失的 backtest 键

## [3.61.2-rc2] - 2026-03-07

### Fixed
- **install.sh 静默模式**：修复 `select_language` 未正确接收 `--silent` 等参数的问题

### Changed
- 移除 footer 版本号显示

## [3.61.2] - 2026-03-07

### Changed
- **Bot 详情回测区重构**：移除弹窗式回测，改为跳转全局回测页并预填参数
  - 补全 botDetail 回测区 i18n（maxPositionValue、priceInterval、goToBacktest 等）
  - 展示更多参数：策略类型、利润间距、价格上下限
  - 点击「前往回测」跳转 `/backtest`，URL 携带 exchange、symbol、strategy、total_capital、grid_spacing、order_quantity 等
  - BacktestMenu 支持从 URL 参数预填表单

## [3.61.1-rc2] - 2026-03-07

### Fixed
- **停止并平仓顺序修复**：修复在停止 Bot 后再调用平仓接口导致 `Bot not found` 的问题
  - 调整 `Dashboard` 与 `BotDetail` 的流程为先平仓再停止，避免 runtime 被提前移除
- **策略运行态接口错误语义修复**：`/api/strategies/runtime` 在运行态不存在时不再返回 500
  - 对“未找到”场景返回 200 + 空策略列表，减少前端轮询期间的错误噪音

## [3.61.1-rc1] - 2026-03-07

### Added
- **install.sh 静默安装选项**：支持 `--silent` / `-s` / `--silent-upgrade` 参数
  - 跳过语言选择、配置确认等交互
  - 保留现有配置文件，升级二进制并自动重启服务
  - 适用于 CI/CD 或自动更新场景

## [3.61.0-rc4] - 2026-03-07

### Changed
- **版本号同步**：统一 main.go 与 webui/package.json 版本号
- **组件与 UI 改进**：Dashboard、Orders、Reconciliation、RiskMonitor、Logs、MarketIntelligence、KlineFilesManager、AITaskManager、Configuration、EventDetailModal、SystemMonitor 等组件优化

## [3.61.0-rc3] - 2026-03-07

### Fixed
- **Bot 详情页显示文案和判断逻辑**：修复显示"仓位价值"而非"实际占用资金"的问题
  - 修改 `GetPositionStatus` 返回 `total_actual_margin` 和 `leverage` 字段
  - 判断逻辑使用实际占用资金（保证金）而非仓位价值
  - Bot 详情页显示改为"当前实际占用资金"，仓位价值作为参考信息小字显示
  - 开仓管理表单标签改为"占用资金上限"，说明文字改为"限制实际占用资金（保证金）"

### Added
- **Bot 详情页新增回测 Tab**：在 Bot 详情页添加回测功能入口
  - 复用 `BotBacktestDialog` 组件，自动带入当前 Bot 的配置参数
  - 显示 Bot 的关键参数（占用资金上限、层数上限、价格间隔、每单金额）
  - 支持中英文翻译

## [3.61.0-rc1] - 2026-03-07

### Fixed
- **开仓管理资金限制逻辑**：修复限仓检查使用仓位价值而非实际占用资金的问题
  - 修改 `checkPositionLimit` 使用 `实际占用资金 = 仓位价值 / 杠杆倍数` 进行判断
  - 日志输出清晰显示实际占用资金、仓位价值和杠杆倍数
  - API 新增 `current_actual_margin_usdt` 和 `current_leverage` 字段
- **开仓管理层数配置丢失**：修复重启后 `MaxPositionLayers` 配置被重置为默认值的问题
  - 在 `normalizeSymbol` 中添加 `OpenPositionControl` 的继承和默认值处理
  - 未配置时从全局继承，全局未配置时默认为 8 层

## [3.60.0-rc5] - 2026-03-07

### Fixed
- **持仓对账以交易所为准**：当本地持仓 < 交易所持仓时，自动以交易所为准补齐本地持仓差额
  - 新增 `fillDeficitPositions`：将差额分配到距离当前价格最近的已填充槽位
  - 若本地无任何持仓槽位，触发完整持仓恢复（`initializeSellSlotsFromPosition`）
  - 不再仅记录警告并建议重启，实现真正的「以交易所为准」同步

## [3.60.0-rc3] - 2026-03-06

### Changed
- **i18n**：补全 `backtest.*` 等 56 个键的多语言翻译（24 种语言）

## [3.60.0-rc2] - 2026-03-06

### Added
- **停止确认对话框**：在 Bot 详情页和 Dashboard 点击「停止」时，弹出确认对话框
  - 可选「仅停止」或「停止并平仓」
  - 若选择平仓，支持市价/限价方式，限价可配置偏移、超时及超时自动重试为市价
  - 限价单提示「可能无法成交，需自行关注」

## [3.60.0-rc1] - 2026-03-06

### Added
- **Bot 列表卡片信息增强**：卡片展示间距（price_interval）、利润间距（profit_spread）、每单金额（order_quantity）、投入资金（total_allocated_capital）
- **总投入汇总**：Bot 列表顶部展示当前筛选下所有 Bot 的总投入金额

### Fixed
- **Bot 回测「无效的 Bot ID」**：修复路由参数名不一致（`:id` vs `botId`）导致 botID 为空的问题
- **回测弹窗关闭按钮**：回测运行中时「关闭」按钮不再禁用，可随时关闭弹窗

### Changed
- **i18n**：补全 `botList.priceInterval`、`profitSpread`、`orderQuantity`、`totalCapital`、`totalInvestment` 多语言（24 种语言）

## [3.59.0-rc3] - 2026-03-06

### Changed
- **Bot 唯一标识与同交易对多 Bot**：
  - 新创建的 Bot 使用 UUID 作为唯一 ID，URL 形如 `/bots/550e8400-e29b-41d4-a716-446655440000`
  - 允许同一交易对存在多个 Bot 配置（如旧的已停止 + 新建的），仅当有运行中的同交易对 Bot 时拒绝创建
  - 创建成功后直接跳转到新 Bot 详情页

### Fixed
- 修复「该交易对已存在 Bot」在旧 Bot 已停止时仍无法创建新 Bot 的问题
- **Bot 列表 UI**：已停止的 Bot 卡片使用灰色背景区分，运行中保持默认背景
- **i18n**：补全 `botList.backtest` 多语言翻译（24 种语言）

## [3.59.0-rc2] - 2026-03-06

### Added
- **Bot 创建向导增强**：
  - 第二步（交易对）、第三步（参数配置）展示当前标记价格与 24h 波峰/波谷，便于设置网格区间
  - 第四步（确认）展示完整参数：策略参数（网格数量、上下限、总金额等）、买单/卖单窗口
  - 新增 `GET /api/market/ticker` 市场行情 API（标记价、24h 高低），支持 Binance/Bitget/Bybit/OKX

## [3.59.0-rc1] - 2026-03-06

### Added
- **日志清理配置化**：系统级配置中新增 `log_cleanup` 设置
  - 可配置启用/禁用、执行时间（HH:MM）、保留天数、要清理的级别（默认 INFO/WARN）
  - 保留 ERROR/DEBUG 便于排查，清理后自动 VACUUM 回收空间
  - 在设置页「系统基础配置」中暴露，支持国际化

## [3.59.0] - 2026-03-05

### Added
- **Bot 回測系統**：實現完整的基於 paxg 專案專業級回測功能
  - **Tick 級別訂單撮合引擎**：實現基於價格路徑穿越檢測的訂單撮合（`backtest/tick_matcher.go`）
  - **多策略組合回測**：支援單 Bot 包含多個策略組合回測（`backtest/multi_strategy_engine.go`）
  - **歷史數據下載器**：整合幣安歷史數據下載器，支援從 data.binance.vision 下載 K 線與資金費率數據（`backtest/binance_downloader.go`）
  - **CSV 數據加載器**：支援 gzip 壓縮格式的 CSV 歷史數據加載（`backtest/data_loader.go`）
  - **策略實現**：實現 5 種回測策略（網格/DCA/馬丁格爾/趨勢/組合）（`backtest/strategies.go`）
  - **API 端點**：
    - `POST /api/v2/bots/:id/backtest` - 建立回測任務
    - `GET /api/v2/bot/backtest/:taskId` - 查詢任務狀態
    - `GET /api/v2/bot/backtest/:taskId/result` - 獲取回測結果
    - `DELETE /api/v2/bot/backtest/:taskId` - 刪除任務
    - `GET /api/v2/bots/:id/backtest/tasks` - 列出 Bot 的所有回測任務
    - `POST /api/v2/backtest/data/download` - 下載歷史數據
    - `GET /api/v2/backtest/data/info` - 獲取數據信息
    - `GET /api/v2/backtest/data/availability` - 檢查數據可用性
  - **UI 組件**：使用 Chakra UI 實現的 `BotBacktestDialog` 組件，支援實時進度顯示、權益曲線圖表、策略統計表格
  - **i18n**：補全中英文回測相關翻譯鍵

### Fixed
- **API 請求解析邏輯**：修復空請求體與無效 JSON 的區分邏輯
- **空策略列表檢查**：添加策略配置為空時的檢查與錯誤提示
- **進度回調不報告 100%**：修復回測完成時進度回調不報告最終進度的問題
- **除零保護**：添加 `CalculateEffectiveSpread` 函數的除零保護

## [3.58.0-rc4] - 2026-03-06

### Fixed
- **內存持續增長**：修復多處導致內存泄漏的問題
  - 事件處理循環：`Subscribe()` 在循環內每次調用會創建新 channel 導致嚴重泄漏，改為循環外訂閱一次
  - 程序退出時關閉 EventBus，釋放訂閱者 channel 與 dedup goroutine
  - SmartParamsService：價格/波動率緩存無過期淘汰，新增 `evictExpiredCache` 與訪問時刪除過期條目

## [3.58.0-rc3] - 2026-03-05

### Added
- **macro 包與相關文件**：修復 CI 構建，補齊 macro/、safety/factor_macro.go、web/api_macro.go
- **宏觀事件完善**：配置項、API 路由、前端 MarketIntelligence 組件與 i18n

## [3.58.0-rc2] - 2026-03-05

### Fixed
- **ReduceOnly 錯誤頻率優化**：降低「無持倉需清空槽位」重複告警
  - 新增槽位冷却期（2 分鐘）：同一槽位 ReduceOnly 失敗後短期內不再嘗試平倉，避免 5 分鐘內重複觸發
  - 解析失敗 fallback：`parseClientOrderID` 失敗時從 `ordersToPlace` 反推槽位價格並清空，避免因解析失敗導致槽位無法清空、持續重試
  - 日誌降級：ReduceOnly 錯誤從 ERROR 改為 WARN（系統會自動清空槽位，減少 AIPipe 告警噪音）

## [3.58.0-rc1] - 2026-03-04

### Added
- **宏觀事件預測市場信號**：接入 Polymarket Gamma REST API，追蹤戰爭、利率、匯率、監管、經濟衰退等宏觀事件
  - 新增 `macro` 包：`MacroEventFetcher` 定時拉取、`EventImpactClassifier` 事件分類與影響映射
  - 新增 `safety/factor_macro.go`：`MacroEventRiskFactor` 風控因子，可註冊到複合風控引擎
  - 配置 `macro_event`：啟用開關、拉取間隔、分類關鍵詞、過濾條件
  - API：`GET /api/macro/events`、`GET /api/macro/impact`
  - 市場情報頁新增「宏觀事件」標籤，展示預測概率與影響評估
  - `GetPolymarketMarkets` 優先使用 Gamma REST API（無需認證），回退 GraphQL

## [3.57.0-rc3] - 2026-03-04

### Fixed
- **啟動失敗錯誤提示**：BotList、BotDetail 的 handleStart 失敗時使用 API 返回的 errorKey 展示具體原因（如交易對衝突）
- **刪除按鈕**：對沖組內 Bot 懸停刪除按鈕時顯示 tooltip 提示「請先刪除對沖組」；點擊時 stopPropagation 避免觸發卡片導航
- **後端日誌**：Bot 創建/對沖組創建衝突、刪除被拒、刪除成功時增加 logger 記錄，便於排查

## [3.57.0-rc2] - 2026-03-04

### Fixed (P0)
- **衝突檢測區分**：創建 Bot 時，若交易對已被對沖組占用，返回明確錯誤 `error.bot_symbol_used_by_hedge_group` 並提示組名，不再僅顯示「交易對衝突」
- **創建對沖組衝突**：同邏輯，創建對沖組時若 futures/spot 已被另一對沖組占用，返回明確提示
- **BotList 對沖組標籤**：Bot 列表並行拉取 `getBotGroups`，對屬於對沖組的 Bot 顯示紫色「Hedge」badge
- **單個 Bot 刪除**：新增 `DELETE /api/bots/:id`，若 Bot 屬於對沖組則返回 403 禁止單獨刪除；前端 BotList 增加刪除按鈕與確認對話框
- **i18n**：所有 21 個 locale 補全 `error.bot_symbol_conflict`、`error.bot_symbol_used_by_hedge_group`、`error.bot_in_hedge_group_cannot_delete` 及 botList 刪除相關 key

## [3.57.0-rc1] - 2026-03-04

### Added
- **Bot 創建流程策略選擇重構**：Bot 創建嚮導從 3 步擴展為 5 步，支持策略選擇與配置
  - 後端：新增 `POST /api/bots/create` API，接受含策略配置的 BotConfig，替代前端直接改 config
  - 後端：config.go 新增 `BotGroup`/`HedgeConfig` 類型，Config 增加 `BotGroups` 字段
  - 後端：新增 BotGroup CRUD API（`POST/GET/DELETE /api/bot-groups`），支持跨市場對沖
  - 後端：新增 `strategy/hedge_coordinator.go` 和 `bot_group_manager.go`，實現對沖協調邏輯
  - 後端：`GET /api/strategies/templates` 返回預設組合模板（grid_trend/dca_martingale/futures_spot_hedge）
  - 前端：新增 `StrategyTypeSelector`、`StrategyPicker`、`StrategyParamForm` 組件
  - 前端：支持基礎策略、組合策略（多策略+權重）、對沖策略（合約+現貨 Bot 組）
  - i18n：所有 locale 新增策略選擇相關 key（~50 個）

## [3.56.3-rc2] - 2026-03-04

### Changed
- **啟動優化**：提前啟動 Web 服務（在事件中心後即啟動，不再等待 K 線收集器與插件），Web API 可更快就緒
- **K 線收集器按需創建**：僅為 Bots/Trading.Symbols 中實際使用的交易所創建適配器，減少無用交易所客戶端初始化
- **啟動耗時日誌**：新增 `⏱️ [啟動]` 階段耗時日誌（存儲、事件中心、Web API、K 線收集器、插件、總耗時），便於排查瓶頸

## [3.56.3-rc1] - 2026-03-03

### Added
- **盈亏诊断**：Statistics 页面新增「盈亏诊断」按钮，弹窗展示网格盈亏 vs 交易所盈亏对比、差异说明及订单统计；后端 `GET /api/statistics/pnl/diagnosis` 增强返回 `pnl_comparison`
- **待实现盈亏展示**：Statistics 汇总卡片与 Dashboard 总盈亏卡片增加「待实现盈亏」展示；后端 `GET /api/statistics` 新增 `unrealized_pnl` 字段
- **rdocs**：新增《网格盈亏与交易所盈亏的差异说明》，手续费文档增加相关链接

## [3.56.2-rc3] - 2026-03-03

### Fixed
- **i18n 补全**：补全 21 个 locale 的 525 个缺失键（aiInterpret、botRiskControl、backtest.paramHints、globalDashboard.closePositions/slotManager/smartOrder、newsAnalysis 等），en-US 补全 108 个键，zh-TW 补全 282 个键

## [3.56.2-rc2] - 2026-03-02

### Fixed
- **statistics.totalPnL 未翻译**：BotDetail 中 `t('statistics.totalPnl')` 改为 `t('statistics.totalPnL')`，与 locale 键一致
- **statistics 多语言补全**：21 个 locale 补全 `modeGlobal`、`modeSingleBot`、`exchangePnl`、`exchangePnlShort`、`exchangePnlTooltip` 等缺失键

## [3.56.2-rc1] - 2026-03-02

### Changed
- **restart_backend.sh → restart_api.sh**：重命名为 `restart_api.sh`，仅重启 API 后端，不编译前端（前端由 Vite 单独服务）；`start.sh` 新增 `--api-only` 选项

## [3.56.1-rc1] - 2026-03-02

### Added
- **保存配置前可选操作**：修改机器人/网格策略参数并保存时，弹出「保存前可选操作」对话框，支持勾选「撤回当前委托单」「平掉当前仓位」，按用户选择执行后再保存配置；确保撤单、平仓指令实际执行

## [3.56.0-rc2] - 2026-03-02

### Fixed
- **已停止 Bot 的仓位/风控**：后端 `getBotPositionStatus`、`getBotRiskControl` 支持已停止的 Bot，返回 200 而非 404；前端停止 Bot 时显示「停止交易的 Bot，没有仓位」，不再弹「获取仓位失败」toast

### Changed
- **策略参数提示**：改为「策略参数与资金分配在左侧菜单的策略配比中设置」
- **盈利管理、风控监控移至全局**：从 Bot 侧边栏移到全局菜单（`/risk`、`/profit-management`），旧链接 `/bots/:id/risk`、`/bots/:id/profit-management` 重定向到全局
- **Footer**：移除服务条款与隐私政策之间的竖线分隔符
- **新增**：`restart_backend.sh` 脚本，支持生产/开发模式快速重启后端

## [3.55.0-rc2] - 2026-03-01

### Changed
- **Bot 列表 UX 優化**：整張卡片可點擊進入詳情，Bot 名稱顯示為藍色鏈接樣式；啟動/停止按鈕點擊時不觸發導航

## [3.55.0-rc1] - 2026-02-15

### Added
- **Bot 概念重構**：系統從「交易對驅動」轉向「Bot 驅動」
  - 後端：新增 `BotConfig` 結構，`MigrateToBots()` 將舊 `symbols` 配置平滑遷移至 `bots`
  - 後端：新增 `BotRuntime`、`BotManager`，按 BotID 進行生命週期管理
  - 後端：新增 `/api/bots` 系列 API（列表、詳情、啟動、停止）
  - 前端：新增 Bot 列表頁 `/bots`、Bot 詳情頁 `/bots/:id`
  - 前端：頂部移除交易所/幣種選擇器，改為 StatusBar（全局視圖、返回全局）
  - 前端：側邊欄新增 Bot 列表入口，點擊 Bot 可進入工作區

## [3.54.2-rc1] - 2026-02-15

### Added
- **策略總覽優先**：導航改為策略維度優先，進入系統先看到所有策略運行概況
  - 新增 `GET /api/strategies/runtime/all` 聚合 API，返回所有幣種下所有策略的運行狀態
  - 新增策略總覽頁 `/strategy-overview`，展示所有策略卡片（狀態/PnL/持倉/資金），支持篩選與搜索
  - 新增策略詳情頁 `/strategy-detail`，展示單策略的持倉、訂單、統計與可視化
  - 側邊欄「策略總覽」置於全局監控首位
- **修復持倉 strategy 硬編碼**：`getPositionsSummaryAll` 從策略運行時獲取實際策略名稱，不再硬編碼 `"grid"`
- **Dashboard 去網格中心化**：Slots 矩陣與價格偏差僅在有 grid 策略時顯示

## [3.54.1-rc5] - 2026-02-12

### Fixed
- **401 未登錄時自動跳轉登錄頁**：API 返回 401 時自動跳轉至 `/login`，避免頁面顯示錯誤提示；`fetchWithAuth` 及 backtest/optimizer 共用此邏輯

## [3.54.1-rc4] - 2026-02-10

### Fixed
- **修復現貨交易對開倉管理/網格報 symbol_not_found**：開倉管理、網格上移/下移 API 現支援 `market_type` 查詢參數，按 `exchange:symbol:market_type` 精確查找運行時；前端開倉管理、配置頁網格操作傳入 `selectedMarketType`，現貨交易對（如 PAXGUSDT）可正常使用

## [3.54.1-rc3] - 2026-02-10

### Fixed
- **修復概覽頁「未運行」與「已啟動」狀態不一致**：判斷當前是否已啟動時區分 spot/future，避免選中現貨時顯示未運行但點擊啟動報「已在使用中」
  - 後端 `GET /api/status` 支援 `market_type` 查詢參數，按 `exchange:symbol:market_type` 精確查詢運行狀態；配置匹配時亦按 market_type 過濾
  - 後端 `GET /api/statuses` 列表 key 改為 `exchange:symbol:market_type`，現貨與合約不再互相覆蓋
  - 前端 `getSystemStatus` 傳入 `marketType`；Dashboard 拉狀態與匹配 isTrading 時均比較 `market_type`；GlobalDashboard 兜底拉單條狀態時傳入 `sym.market_type`

## [3.54.1-rc2] - 2026-02-10

### Fixed
- **修復現貨啟動誤啟合約**：`StartSymbol` 新增 `marketType` 參數，當前端傳入 `market_type=spot` 時優先啟動現貨配置，避免點擊現貨「啟動」時誤啟合約
- **修復現貨杠杆檢查錯誤**：`CheckAccountSafety` 對現貨市場跳過杠杆倍數檢查（恒為 1x），避免現貨啟動時因誤用合約 API 的杠杆數據而報「杠杆倍率太高」錯誤

## [3.54.1-rc1] - 2026-02-10

### Added
- **頂部菜單交易對選擇標明現貨/合約**：選擇交易所後的交易對下拉中每項顯示「現貨」或「合約」標籤，避免同名交易對混淆
  - SymbolContext 新增 `selectedMarketType` 並持久化；交易對選項使用複合 value（symbol::market_type）區分現貨與合約
  - Dashboard 起停交易、Orders/Positions/Reconciliation/Slots 的當前交易對匹配均按 exchange+symbol+market_type 精確匹配

## [3.54.0-rc8] - 2026-02-10

### Fixed
- **修復現貨 WebSocket 消息解析失敗**：`spot_websocket.go` 改用 `map[string]interface{}` 解析消息，兼容 Binance 推送的不同消息格式（miniTicker `e` 為字串、心跳/控制消息 `e` 為數字）。之前嚴格的 struct 解析導致所有消息都失敗，價格永遠收不到

## [3.54.0-rc7] - 2026-02-10

### Fixed
- **修復啟動/停止交易互相覆蓋 bug**：`SetSymbolEnabled` 新增 `marketType` 參數，按 `exchange + symbol + market_type` 精確匹配，避免啟動現貨時把合約的 enabled 狀態覆蓋（反之亦然）
  - `StartSymbol` / `StopSymbol` 調用 `SetSymbolEnabled` 時傳入 `marketType`
  - `StartSymbol` 第二次配置查找加入 `market_type` 精確匹配

## [3.54.0-rc6] - 2026-02-10

### Fixed
- **修復啟動/停止交易 API 未傳 market_type**：前端 `startTrading`/`stopTrading` 函數新增 `marketType` 參數；後端 handler 讀取 `market_type` query param 並用於精確查找 `statusBySymbol`
- **修復同名交易對啟動衝突**：`StartSymbol` 改為遍歷所有同名候選配置，自動啟動尚未運行的那個（如 BTCUSDT 合約已運行，再啟動 BTCUSDT 現貨不會報錯）
- 前端 `GlobalDashboard.handleToggleTrading` 傳入 `sym.market_type`

## [3.54.0-rc5] - 2026-02-10

### Fixed
- **修復啟動/停止交易對缺少 market_type 判斷**：`SymbolManager.runtimeKey` 從 `exchange:symbol` 改為 `exchange:symbol:market_type`，解決同名交易對（如 BTCUSDT 合約已運行時，BTCUSDT 現貨無法啟動）互相衝突的問題
  - `SymbolManager.Get`/`Add`/`Remove` 均支持 `market_type` 參數
  - `StartSymbol` 在找到配置後用 `market_type` 精確判斷是否已在運行
  - `StopSymbol`/`ClosePositions` 使用 `resolveMarketType` 推導正確的 market_type
  - `UpdateRuntimeTradingParams` 使用含 market_type 的 key 匹配運行時

## [3.54.0-rc4] - 2026-02-10

### Fixed
- **修復概覽頁同名交易對去重 bug**：同一交易所的 BTCUSDT 現貨和 BTCUSDT 合約會被錯誤合併為一個，因為 key 只用了 `exchange:symbol`
  - 後端：`makeSymbolKey` 改為 `exchange:symbol:market_type` 三段式 key
  - 後端：`SystemStatus` 新增 `MarketType` 欄位
  - 後端：`RegisterSymbolProviders` 支持傳入 `marketType`
  - 後端：`resolveSymbolKey` 支持 `market_type` 查詢參數，含向後兼容 fallback
  - 前端：`GlobalDashboard` 所有 key 更新為含 `market_type`，`pnlInfo` 查找加入 `market_type` 匹配

## [3.54.0-rc3] - 2026-02-10

### Fixed
- **修復現貨 WebSocket 連接失敗**：Binance 現貨 WebSocket 端點從 `stream.binance.com:9443` 改為 `stream.binance.com:443`（標準 HTTPS 端口），解決因 9443 端口被防火牆/代理阻擋導致 BTCU 等現貨交易對價格流啟動超時的問題

## [3.54.0-rc2] - 2026-02-10

### Fixed
- **修復交易對加載 bug**：切換市場類型（現貨/合約）後，`loadAvailableSymbols` 使用的是舊的 `market_type`（React state 異步更新問題），導致選「現貨」時仍拉取合約交易對，U 計價交易對無法顯示
- **交易對按計價幣分組**：Select 組件使用 `<optgroup>` 按計價幣（U / USDT 等）分組顯示，方便快速定位 U 計價零手續費交易對

## [3.54.0-rc1] - 2026-02-10

### Added
- **支持 United Stables (U) 計價幣**：新增對幣安 United Stables (U) 穩定幣的支持，可交易 BTC/U、ETH/U、SOL/U 等零手續費（Maker）交易對
  - 後端：`getBinanceSpotSymbols` 現貨交易對拉取支持 USDT + U 雙計價幣過濾
  - 後端：Binance Spot/Futures 適配器餘額檢測支持 U 資產
  - 後端：`symbol_manager` 餘額獲取改為動態 quote currency（使用 `GetQuoteAsset()`）
  - 後端：回測報告 `baseAssetFromSymbol` 支持 U 後綴解析
  - 後端：參數顧問 quote currency 校驗支持 U
  - 後端：Inspector 賬戶摘要 Currency 字段改為動態取值
  - 前端：新增 `utils/symbol.ts` 工具函數（`getQuoteAsset`/`getBaseAsset`）
  - 前端：Dashboard 所有貨幣單位顯示改為動態（不再硬編碼 USDT）
  - 前端：AIConfigWizard 餘額查詢支持 U/USDT/USDC/BUSD 多計價幣

## [3.53.1-rc1] - 2026-02-10

### Fixed
- **CI 修复**：移除 GitHub Actions CI 中的 ARM64 构建目标，修复因 ubuntu-latest (AMD64) 无法交叉编译 CGO ARM64 代码导致的 CI 失败问题

## [3.53.0] - 2026-02-10

### Added
- **網格策略增強（對標幣安）**：
  - **網格風控 UI**：配置頁交易對參數下新增「網格風控」區塊，可配置啟用、止損比例、止盈觸發比例、回撤止盈比例、最大持倉層數、預警時最多開倉單數、趨勢過濾；後端風控邏輯已存在，此前無 UI 暴露。
  - **價格範圍（軟限制）**：支持配置價格下限/上限；超出範圍時暫停新開倉，保留已有倉位的平倉單；槽位價格裁剪到 [price_low, price_high] 範圍內。
  - **觸發價格**：可設置觸發價，達到後才啟動網格（做多：當前價 ≤ 觸發價啟動；做空：當前價 ≥ 觸發價啟動）。
  - **網格模式**：支持等差（arithmetic）與等比（geometric）兩種網格間距；等比時 price_interval 為比例（如 0.005 表示 0.5%）。
  - **網格上移/下移**：配置項 grid_shift_step；新增 API `POST /api/grid/shift-up`、`POST /api/grid/shift-down`；配置頁提供上移/下移按鈕，可手動整體移動網格錨點並撤銷開倉委託。
  - **終止時全部平倉**：配置項 close_on_stop；策略停止時若啟用則自動執行全平倉。
- **後端**：`config.SymbolConfig` 與 `Config.Trading` 新增 price_low、price_high、trigger_price、grid_mode、grid_shift_enabled、grid_shift_step、close_on_stop；symbol_manager 合併上述字段到 localCfg；super_position_manager 實現觸發價檢查、價格範圍軟限制、等比網格計算、ShiftGrid 方法；symbol_manager 停止時若 close_on_stop 則調用 LiquidateAll。
- **前端**：TypeScript 類型補全（GridRiskControl、SymbolConfig 新字段）；配置頁新增價格範圍、觸發價、網格模式、終止時平倉、網格步長及上移/下移按鈕、網格風控卡片；24 語言 i18n 新增對應 key。

## [3.53.0-rc1] - 2026-02-10

### Added
- **交易面板/概览显示开仓暂停状态**：在交易面板（Dashboard）和概览（GlobalDashboard）页面中展示「暂停开仓」的醒目提示。
  - 后端 `SystemStatus` 新增 `opening_paused` 和 `pause_reason` 字段，`/api/status` 和 `/api/statuses` 接口自动返回开仓控制状态
  - 交易面板：当所选交易对处于暂停开仓状态时，显示橙色警告横幅，标明暂停原因（手动/定时/周期/限仓），并提供跳转至「开仓管理」的快捷入口
  - 概览页：在全局汇总区域下方显示暂停开仓的交易对计数提示，每个交易对卡片上也会显示橙色「已暂停开仓」标识
  - 支持全部 24 种语言的国际化翻译

## [3.52.2-rc4] - 2026-02-07

### Fixed
- **翻译修复**：修复编辑交易对时 `configuration.profitSpread` 和 `configuration.profitSpreadHint` 未翻译的问题，已在所有 24 个语言文件中添加相应翻译。

## [3.52.2-rc3] - 2026-02-07

### Fixed
- **订单管理交易对筛选修复**：修复订单管理页面在选择特定交易所和交易对时，仍显示其他交易所或交易对订单的问题。
  - 后端新增 `QueryOrdersWithFilter` 和 `CountOrdersWithFilter` 方法，支持按 `exchange` 和 `symbol` 筛选订单
  - 历史订单 API (`/api/orders/history`) 现在正确应用交易所和交易对筛选条件
  - 待成交订单 API (`/api/orders/pending`) 返回的订单信息中增加 `exchange` 和 `symbol` 字段

## [3.52.2-rc2] - 2026-02-07

### Fixed
- **持仓列表显示优化**：修复未持仓币种（如 BNB）不显示的问题。现在即使持仓为 0、盈利为 0，配置的币种也会显示在持仓列表中，保持表格高度一致，提升用户体验。

## [3.52.2-rc1] - 2026-02-07

### Added
- **交易对列表显示优化**：在交易对列表表格中新增「持仓安全检查阈值」列，显示每个交易对的 `position_safety_check` 参数值，方便用户查看和调整该参数。

### Changed
- **订单查询筛选增强**：订单历史查询和待成交订单列表支持按交易所（exchange）和交易对（symbol）进行筛选，提升查询灵活性。

### Fixed
- **開倉管理撤單增強**：修復暫停開倉時開倉委託單未撤銷的問題。在 `PauseOpening` 中增加異步強制撤單邏輯，直接從交易所獲取所有掛單並撤銷開倉方向的委託，解決本地狀態同步延遲導致的撤單不徹底問題。

## [3.52.1-rc2] - 2026-02-07

### Fixed
- **開倉管理翻譯修復**：補全 `common.delete` 翻譯（24 個語言）；將開倉管理中的 scheduleCard/addScheduleRule 術語由「定時規則」改為「重複規則」（Cron Rules），與實際功能（類似 crontab 的定時重複執行）一致。

## [3.52.1-rc1] - 2026-02-07

### Added
- **新聞分析品種開關（配置頁）**：在配置 → 新聞監控下新增「新聞分析品種」區塊，七個品種（BTC、國際金價、白銀、美股、ETH、SOL、DOGE）各有一個啟用開關；開啟的品種會按「AI 分析間隔」定時調用 AI 做預測，未開啟的品種可在新聞分析頁手動觸發。i18n：`newsAnalysisAssets`、`newsAnalysisAssetsDesc`（簡中/繁中/英文）。

### Fixed
- **新聞分析按品種返回**：後端 `GET /api/news/analysis?asset_type=` 補全 asset_type → symbol 映射（crypto_btc、commodity_gold、commodity_silver、stock_us、crypto_eth、crypto_sol、crypto_doge），切換白銀/黃金/ETH 等品種時正確返回該品種的 Analysis Summary 與評估結果。

### Changed
- **新聞分析頁**：品種按鈕文案改為 i18n（btc、eth、sol、doge 等）；無該品種分析結果時展示提示「該品種暫無分析結果，請手動觸發或啟用定時分析」。

## [3.52.0] - 2026-02-07

### Added
- **通知測試連接**：在通知配置頁面的每個通知渠道（Telegram、Webhook、Email、飛書、釘釘、企業微信、Slack）下方新增「測試連接」按鈕
  - 點擊按鈕會使用當前頁面上的配置向對應渠道發送一條測試通知
  - 發送成功/失敗會通過 Toast 即時反饋
  - 按鈕在必要配置項未填時自動禁用
  - 前端：`testNotification` API、i18n（中英文）、`Configuration.tsx` 測試按鈕 UI
  - 後端：復用已有 `POST /api/config/test-notification?channel=` 端點

## [3.51.0] - 2026-02-07

### Added
- **日盈虧拆解頁**：統計日曆支持點擊有數據的日期進入當日盈虧拆解
  - 後端：新增 `GET /api/statistics/daily/breakdown?date=YYYY-MM-DD&exchange=&symbol=`，按配置時區查詢當日訂單/成交/資金費/小時權益/每日快照，返回核心數據、計算步驟、最終等式、小時權益曲線、Top 成交
  - 存儲：`GetDailyTradesSummary`、`GetFilledOrderQtySumBeforeTime` 支持按日與時區聚合
  - 前端：日盈虧拆解頁 `/statistics/daily/:date`（核心數據表、四步計算卡片、最終等式、日內權益曲線、Top Trades），日曆格點擊導航；完整 i18n（dailyBreakdown.*，中英文）

## [3.50.0] - 2026-02-07

### Added
- **訂單來源標記**：在歷史訂單列表中新增「訂單來源」列，區分正常限價委託和止損平倉訂單
  - 正常委託（normal）：網格策略按價格間距正常下的限價單，顯示綠色標籤
  - 止損平倉（stop_loss）：風控觸發硬止損或策略止損時的平倉單，顯示紅色標籤
  - 後端：`storage.Order`、`position.OrderRequest`、`order.OrderRequest` 新增 `OrderSource` 字段
  - 數據庫：`orders` 表自動遷移新增 `order_source` 列，UPSERT 兼容舊數據
  - API：`/api/orders/history` 返回 `order_source` 字段
  - 前端：歷史訂單表格新增「訂單來源」列，完整 i18n 支持（中英文）

## [3.49.0] - 2026-02-07

### Added
- **開倉管理**：新增開倉管理模組，支持手動暫停開倉、限倉、定時規則、週期規則
  - 手動暫停開倉：一鍵暫停/恢復開倉，暫停時自動撤銷當前開倉委託（做多撤買單、做空撤賣單）
  - 限倉功能：當倉位價值或持倉層數達到上限時，自動暫停開倉並撤銷開倉委託
  - 定時規則：可配置在指定 UTC 時間執行暫停/恢復開倉，支持星期篩選
  - 週期規則：可配置開倉持續 N 分鐘、關倉持續 M 分鐘的週期性開關倉
  - 後端：`config.OpenPositionControl`、`position.OpeningController`、`web/api_opening_control.go`
  - 前端：開倉管理頁面（`/opening-control`）、完整 i18n 支持（中英文）

## [3.48.5-rc2] - 2026-02-07

### Fixed
- **修复非 Binance 交易所订单盈亏数据丢失**：交易所返回的已实现盈亏（RealizedPnL）未被正确捕获和存入数据库
  - **根因**：OKX、Gate.io、Bybit、Bitget 等交易所的 `OrderUpdate` 结构体缺少 `RealizedPnL` 字段，WebSocket 返回的盈亏数据在解析阶段即被丢弃
  - **二次丢失**：各适配器 `StartOrderStream` 中创建通用匿名结构体时也未包含 `RealizedPnL`、`Commission`、`CommissionAsset` 字段
  - 修复范围：Gate.io（`realised_pnl`）、Bitget（`totalProfits`）、OKX（`pnl`）、Bybit（`closedPnl`）均已正确解析
  - 所有交易所适配器包装层已补齐 `RealizedPnL`、`Commission`、`CommissionAsset` 字段传递

## [3.48.5-rc1] - 2026-02-07

### Added
- **AI 解读持久化与历史**：资金费率与价差监控页的「AI 解读」支持记住状态并列出历史
  - 点击「AI 解读」后离开页面再返回，自动恢复显示上次进行中或已完成的解读；进行中任务会继续轮询直到完成
  - 新增「历史解读」折叠列表，可按页面类型查看过往解读记录，点击可查看该次结果
  - 后端：市场解读任务写入 SQLite（`market_interpret_tasks` 表），新增 `GET /api/ai/market-interpret/latest?page_type=`、`GET /api/ai/market-interpret/history?page_type=&limit=`
  - 前端：挂载时请求最新一条并恢复展示；历史列表与选中查看结果；中英文 i18n（historyTitle、noHistory、status.*）

## [3.48.4] - 2026-02-07

### Added
- **当前持仓页「撤销所有委托买单」**：在持仓页标题旁新增按钮，可一键撤销当前交易对下所有委托买单；完整 i18n 支持（简体中文 / 繁体中文 / 英文 / 日文 / 韩文等）

## [3.48.3] - 2026-02-07

### Added
- **概览与交易面板显示当前币种杠杆**：在已选交易所和币种时展示杠杆倍数
  - 交易面板（Dashboard）：头部与币种/交易所/方向并列显示「杠杆 Nx」Badge，数据来自持仓汇总或交易所
  - 概览（GlobalDashboard）：当前持仓表格新增「杠杆」列，每行显示该币种杠杆
  - 新增 i18n：`dashboard.leverage`、`dashboard.leverageTimes`（简体中文 / 繁体中文 / 英文）

## [3.48.2-rc2] - 2026-02-07

### Changed
- **买单/卖单窗口滑块显示当前值**：滑块右侧显示当前选中的数值，便于确认配置

## [3.48.2] - 2026-02-07

### Changed
- **买单/卖单窗口大小改为滑块**：配置管理界面中，买单窗口大小、卖单窗口大小由数字输入框改为 1–100 范围滑块，并增加 10、20、30、50、100 快捷按钮

## [3.48.1] - 2026-02-07

### Added
- **新增利润间距（ProfitSpread）配置**：将网格间距和平仓利润目标解耦
  - 新增 `profit_spread` 配置字段，控制卖出价与买入价的价差
  - 不填或填 0 时默认使用 `price_interval`（完全向后兼容）
  - 例如：`price_interval=80`（网格间距 80），`profit_spread=100`（卖出价 = 买入价 + 100）
  - 支持主配置、Profile 配置、初始化向导中配置
  - 前端配置界面、交易对管理弹窗同步新增输入框
  - 完整 i18n 支持（简体中文 / 繁体中文 / 英文）

## [3.48.0-rc11] - 2026-02-07

### Fixed
- **修复交易参数热更新不生效的严重 Bug**：修改 `price_interval`、`order_quantity`、`buy_window_size`、`sell_window_size` 等参数后，运行中的 `SuperPositionManager` 内存中的数据不会同步更新，导致后续下单仍使用旧参数（如间隔从 200 改为 80 后，委托单仍按 200 间隔挂单）
  - 根因：`symbol_manager.go` 启动时通过 `localCfg := *baseCfg` 创建值拷贝，`SuperPositionManager` 持有此拷贝的指针，配置更新只修改了 `configManager.currentConfig`，未推送到运行时
  - 新增 `SuperPositionManager.UpdateTradingParams()` 方法，支持运行时更新交易参数
  - 新增 `SymbolManager.UpdateRuntimeTradingParams()` 方法，遍历所有运行时并推送最新配置
  - 在 `updateConfigHandler` 保存配置后自动调用热更新推送，确保参数立即生效
  - API 响应新增 `hot_updated` 字段，返回已热更新的交易对列表

### Added
- **实时价格范围计算**：配置页面新增「实时价格范围」面板
  - 根据当前市场价格、价格间隔和窗口大小自动计算买单/卖单的价格上下限
  - 显示当前价格、网格价格、锚点价格
  - 支持从运行时实时获取或从配置文件静态计算
  - 保存配置后自动刷新价格范围
  - 新增后端 API：`GET /api/config/price-range`
  - 完整 i18n 支持（简体中文 / 繁体中文 / 英文）

## [3.48.0-rc10] - 2026-02-07

### Fixed
- 修复 `symbol_manager.go` 中 `selectedProfile` 变量声明但未使用导致编译失败的问题

## [3.48.0-rc9] - 2026-02-06

### Added
- **配置页面新增「参数建议助手」**：在交易对参数设置区域新增可折叠的智能参数建议面板
  - 支持手动输入 Maker / Taker 手续费率，实时显示百分比换算
  - 支持「一键获取交易所费率」按钮，自动从交易所 API 拉取实际费率（Binance / Bitget，需配置 API Key）
  - 未配置 API Key 时自动回退到配置文件中的费率或行业默认值
  - 根据当前价格和手续费率自动计算：盈亏平衡间距、推荐的 price_interval 范围（最小/推荐/最大）、推荐的单笔订单金额范围
  - 显示当前已配置值是否在建议范围内（绿/橙/红标签提示）
  - 一键「应用推荐值」直接填入配置
  - 支持 Binance / Bitget / Bybit / OKX 多交易所价格获取
  - 新增后端 API：`GET /api/config/param-advisor`（参数建议）、`GET /api/config/exchange-fees`（交易所费率获取）
  - 完整的 i18n 支持（简体中文 / 繁体中文 / 英文）

## [3.48.0-rc8] - 2026-02-06

### Added
- **价差监控 & 资金费率页面新增「AI 解读」功能**：在现货-合约价差监控和资金费率页面底部新增 AI 市场解读按钮
  - 点击后自动收集当前页面数据（价差/资金费率）+ 最近 30 根 1 分钟 K 线 + 最近 16 根 15 分钟 K 线
  - 通过 Gemini API（启用 Google Search grounding）结合最新市场新闻和宏观经济形势进行综合分析
  - 使用异步任务机制，前端轮询获取结果，避免长时间阻塞
  - AI 输出 Markdown 格式的专业分析报告，包含市场概况、技术面分析、价差/费率分析、风险提示和操作建议
  - 新增后端 API：`POST /api/ai/market-interpret`（创建任务）、`GET /api/ai/market-interpret/:task_id`（查询状态）
  - 新增 `AIMarketInterpret` 通用组件，支持进度条、折叠/展开、错误提示
  - 完整的 i18n 支持（中文/英文）

## [3.48.0-rc7] - 2026-02-06

### Fixed
- **修复平仓委托总量超出实际持仓的 Bug**：当本地槽位状态与交易所实际仓位不一致时（存在"幻影"槽位），系统会为每个本地 FILLED 槽位都挂平仓委托，导致委托总量超出实际持仓。
  - 增强 `ForceSyncPositions`：新增 `trimExcessPositions` 方法，支持在交易所仍有持仓（非零）的情况下，修剪距离当前价格最远的多余幻影槽位，将本地持仓对齐交易所
  - 增强对账逻辑：当检测到 `本地持仓 > 交易所持仓 > 0` 时，自动调用 `ForceSyncPositions` 修剪多余槽位，而非仅记录警告
  - 此前系统仅在交易所仓位为 0 时才会同步清空本地状态，非零不一致只打日志不修复，导致幻影槽位长期残留

## [3.48.0-rc6] - 2026-02-06

### Added
- **对账页面新增「每日盈利增量」柱状图**：在聚合数据视图中新增一个直观的柱状图，展示每天/每周/每月的盈利变化量
  - 蓝色柱子表示每日预计盈利增量，绿色柱子表示每日实际盈利增量
  - 正值显示在零线上方，负值显示在下方，一目了然
  - 原有的累计盈利趋势图保留，标题改为「累计盈利趋势」以区分
  - 新增 i18n 翻译 key：dailyProfitChange, dailyEstimatedProfit, dailyActualProfit, cumulativeProfit, profitAmount

## [3.48.0-rc5] - 2026-02-06

### Fixed
- **历史订单统计数字不准确**：修复订单管理页"总订单数"和"今日订单数"始终受 API 返回条数限制（最多 200 条）的问题
  - 后端 `/api/orders/history` 新增 `total_count`（数据库真实总数）和 `today_count`（今日订单数）字段
  - 后端新增 `CountOrders` 方法，通过 `SELECT COUNT(*)` 从数据库获取真实订单总数
  - 前端统计卡片（今日订单数、总订单数）改为使用后端返回的真实数据，而非前端数组长度
  - 前端查询 limit 从默认 100 提升至 500，确保列表显示更多记录
- **修复多个 Go 编译错误**：补充缺失的包导入并修复变量遮蔽问题
  - `web/api_capital.go`：添加缺失的 `os` 包导入
  - `web/api_config.go`：添加缺失的 `time` 包导入
  - `web/api_news.go`：添加缺失的 `os` 和 `fmt` 包导入
  - `web/api_risk_check.go`：添加缺失的 `os` 包导入
  - `web/api_strategy.go`：修复局部变量 `config` 遮蔽 `config` 包名导致的类型错误，移除不存在的 `Priority` 字段引用

## [3.48.0-rc4] - 2026-02-06

### Fixed
- **修复 NewsAnalysisHistory 构建错误**：修复了 `NewsAnalysisHistory.tsx` 的两个问题导致 Vite 构建失败：
  - 缺少 `Button` 组件的导入（分页按钮使用了但未从 `@chakra-ui/react` 导入）
  - 多余的 `</VStack>` 闭合标签导致 Modal 成为第二个根元素，触发 JSX 解析错误 `Expected ")" but found "isOpen"`

## [3.48.0-rc3] - 2026-02-06

### Fixed
- **网格同价买卖单去重**：修复了网格策略在同一价格同时挂买单和卖单的 Bug。
  - 场景：LONG 模式下，空仓槽位 P 挂买单，同时已持仓槽位 (P - interval) 的平仓价也是 P，导致同价位同时出现买单和卖单，毫无意义还浪费手续费。
  - 修复：在下单前增加去重检查，若同一价格同时有开仓单和平仓单，移除开仓单（平仓优先），并重置对应槽位状态。
  - 同时适用于 SHORT 模式的对称场景。

## [3.48.0-rc2] - 2026-02-06

### Added
- **通知配置测试功能**：在配置管理页面为每个通知渠道（Telegram, Webhook, Email, 飞书, 钉钉, 企业微信, Slack）添加了“测试通知”按钮。
  - 后端：新增 `POST /api/config/test-notification` 接口，支持使用当前未保存的配置实时测试通知发送。
  - 前端：在通知设置卡片中集成了测试按钮，支持实时反馈测试结果。
  - 国际化：补全了中、英、日、繁体中文的测试相关翻译。
- **新闻分析多资产支持扩展**：新闻分析功能新增支持美股、白银、ETH、SOL、DOGE 等多种资产
  - 新增资产类型：美股（S&P 500）、白银（XAGUSDT）、以太坊（ETHUSDT）、Solana（SOLUSDT）、Dogecoin（DOGEUSDT）
  - 每个资产类型都有专门的关键词配置和 AI 分析 Prompt
  - 黄金分析改为预测国际金价，使用 PAXG/USDT 核对价格走势
  - 每个资产独立调用 Gemini 进行分析，避免一次性分析所有资产导致 API 限制
- **NewsAPI 单元测试**：新增 `monitor/news_collector_test.go` 单元测试文件
  - 测试 NewsAPI 新闻获取功能
  - 验证新闻项的基本字段完整性
  - 测试多关键词合并和收集功能
  - 测试上下文取消处理

### Fixed
- **统一配置历史记录逻辑**：确保通过 Web UI（表单编辑器、资金分配、策略管理、新手加固等）修改配置时，也能正确记录到“配置历史”中，而不仅仅是 YAML 编辑器。
  - 后端：在 `ConfigManager.UpdateConfig` 中统一注入历史记录保存逻辑。
  - 后端：修复 `SetSymbolEnabled`、`updateCapitalAllocationHandler`、`putNewsKeywords` 等直接调用 `SaveConfig` 的地方，补全历史记录。
  - 变更描述：自动生成更详细的变更描述（如“通过 Web UI 修改了 N 项配置”）。

## [3.48.0-rc1] - 2026-02-06

### Added
- **新闻分析多资产支持扩展**：新闻分析功能新增支持美股、白银、ETH、SOL、DOGE 等多种资产
  - 新增资产类型：美股（S&P 500）、白银（XAGUSDT）、以太坊（ETHUSDT）、Solana（SOLUSDT）、Dogecoin（DOGEUSDT）
  - 每个资产类型都有专门的关键词配置和 AI 分析 Prompt
  - 黄金分析改为预测国际金价，使用 PAXG/USDT 核对价格走势
  - 每个资产独立调用 Gemini 进行分析，避免一次性分析所有资产导致 API 限制
  - 后端：`config/config.go` 新增各资产的默认关键词函数（`DefaultSilverKeywords`、`DefaultStockKeywords`、`DefaultEthKeywords`、`DefaultSolKeywords`、`DefaultDogeKeywords`）
  - 后端：`monitor/news_monitor.go` 扩展 `SymbolToAssetType` 映射支持新资产
  - 后端：`monitor/gemini_news_analyzer.go` 优化 Prompt，为不同资产类型提供针对性分析指令
  - 前端：`NewsAnalysis.tsx` 增加新资产选项，支持切换不同资产查看分析结果
  - 国际化：新增资产相关翻译（goldInternational、silver、usStock）
- **NewsAPI 单元测试**：新增 `monitor/news_collector_test.go` 单元测试文件
  - 测试 NewsAPI 新闻获取功能
  - 验证新闻项的基本字段完整性
  - 测试多关键词合并和收集功能
  - 测试上下文取消处理

## [3.47.0-rc1] - 2026-02-06

### Changed
- **新闻分析页面 Tab 化改造**：将新闻分析页面重构为 Tab 布局
  - 将"最新分析"、"历史记录"、"预测准确率"整合到同一个页面的三个 Tab 中
  - 移除了原有的路由跳转按钮，统一使用 Tab 切换
  - 优化了页面结构，移除了重复的标题和返回按钮
  - 国际化支持：确保 Tab 标签翻译（tabAnalysis、tabHistory、tabPrediction）

## [3.46.2-rc1] - 2026-02-06

### Added
- **新闻分析波动曲线**：在新闻分析页面增加“风险评分”和“大跌概率”的历史波动曲线图
  - 支持选择时间段（最近 7 天、30 天、90 天）
  - 后端 API `GET /api/news/history` 增强，支持返回风险评分和大跌概率，并支持日期格式参数
  - 前端新增 `NewsTrendChart` 组件，使用 recharts 展示双曲线波动趋势
  - 国际化支持：新增波动趋势相关翻译文案

## [3.46.1-rc1] - 2026-02-06

### Added
- **历史订单时间范围限制**：历史订单查询必须携带时间范围，默认最近24小时（滚动），最大7天
  - 前端历史订单页面新增时间范围选择器（开始时间/结束时间）
  - 前端自动校验时间范围（结束时间必须晚于开始时间，跨度不超过7天）
  - 后端强制校验时间范围参数，缺失时默认最近24小时
  - 存储层 `QueryOrdersWithTimeRange` 支持时间范围过滤
  - 所有历史订单查询均应用时间范围限制，提升查询性能
  - 国际化支持：新增时间范围相关翻译文案（zh-CN/zh-TW/en-US/ja-JP）

## [3.46.0-rc1] - 2026-02-06

### Added
- **历史订单时间范围限制**：历史订单查询必须携带时间范围，默认最近24小时（滚动），最大7天
  - 前端历史订单页面新增时间范围选择器（开始时间/结束时间）
  - 前端自动校验时间范围（结束时间必须晚于开始时间，跨度不超过7天）
  - 后端强制校验时间范围参数，缺失时默认最近24小时
  - 存储层 `QueryOrdersWithTimeRange` 支持时间范围过滤
  - 所有历史订单查询均应用时间范围限制，提升查询性能
  - 国际化支持：新增时间范围相关翻译文案（zh-CN/zh-TW/en-US/ja-JP）

## [3.45.0] - 2026-02-06

### Added
- **多套配置自动切换功能**：根据资金费率和手续费率自动切换配置档案
  - 支持为每个交易对配置多个配置档案（如正费率/负费率）
  - 每个配置档案可独立设置价格间隔、订单数量、买卖窗口大小等参数
  - 支持基于资金费率阈值和手续费率阈值的自动切换规则
  - 可配置切换冷却时间，避免频繁切换
  - 后端实现：
    - `config.SymbolConfig` 新增 `Profiles` 和 `SwitchRules` 字段
    - `symbol_manager.go` 实现配置档案选择和应用逻辑
    - 启动时自动选择合适配置档案，运行时定期检查并自动切换
    - 配置切换时发布 `config_switched` 事件
  - 前端实现：
    - 配置页面新增「多套配置自动切换」卡片
    - 支持编辑正费率/负费率配置档案参数
    - 支持配置切换规则（资金费率阈值、手续费率阈值、冷却时间）
  - 国际化支持：新增相关翻译文案（zh-CN/zh-TW/en-US/ja-JP）

## [3.44.1-rc9] - 2026-02-06

### Fixed
- **订单策略归属记录**：修复历史订单页面「策略」列始终显示 `-` 的问题
  - `Order` 模型新增 `StrategyName` / `StrategyType` 字段
  - 数据库 `orders` 表自动迁移添加 `strategy_name` / `strategy_type` 列
  - 下单事件（`order_placed`）携带策略信息并持久化到数据库
  - 历史订单 API 返回 `strategy_name` / `strategy_type` 字段
  - 使用 UPSERT + COALESCE 确保后续状态更新（成交/取消）不会覆盖已有策略信息

## [3.44.1-rc8] - 2026-02-06

### Added
- **Gemini 分析间隔扩展**：在新闻监控配置中新增 2小时、4小时、8小时、24小时分析间隔选项
- **国际化完善**：补全了 zh-CN、zh-TW、en-US 中新增间隔选项的翻译文案

## [3.44.1-rc7] - 2026-02-06

### Added
- **交易所盈亏统计全覆盖**：在概览页(Dashboard)、收益统计页(Statistics)的汇总卡片、日历视图、每日统计表格中同步展示交易所已实现盈亏
  - 后端新增 `GetExchangePnLTotal` / `GetDailyExchangePnL`，从 `orders` 表聚合 `realized_pnl`
  - `/api/statistics` 返回 `exchange_pnl`（总计）；`/api/statistics/daily` 每日条目返回 `exchange_pnl`
  - Dashboard 总盈亏卡片下方显示交易所盈亏（带 Tooltip 说明计算差异）
  - Statistics 汇总区新增「交易所盈亏」卡片；每日统计表新增「交易所盈亏」列（蓝色区分）
  - 日历格子中在网格盈亏下方显示交易所盈亏行
  - 计算逻辑说明：网格 PnL = `(卖出价−买入价)×数量`（按买卖配对）；交易所 PnL = 交易所按加权平均成本法计算的已实现盈亏
- 多语言支持：`dashboard.exchangePnl`、`statistics.exchangePnl`/`exchangePnlShort`/`exchangePnlTooltip`（zh-CN/zh-TW/en-US/ja-JP/ko-KR）

## [3.44.1-rc6] - 2026-02-06

### Fixed
- **修复多处翻译缺失问题**：
  - 修复 zh-CN 中 dashboard 价格偏差统计区域 13 个 key 显示英文的问题（buyPriceDeviation、sellPriceDeviation、totalDeviationLoss 等）
  - 修复 zh-CN 中 configSetup 安全设置区域 10 个 key 显示英文的问题（enableEncryption、securitySettings 等）
  - 修复 Slots 页面 order_side（BUY/SELL）和 order_status 未翻译的问题
  - 修复 StrategyRuntimeStatus 组件订单方向和状态未翻译的问题
  - 修复 Dashboard 组件订单方向翻译 key 引用缺少命名空间前缀的问题
  - 同步更新全部 22 个语言文件的翻译（新增 slotsPage/strategyRuntime 下的订单方向与状态 key，修复 dashboard 价格偏差翻译）

## [3.44.1-rc5] - 2026-02-06

### Fixed
- **修复订单数量显示为 0.0000 的 Bug**：`order_filled` 事件字段名 `executed_qty` 与 `saveOrderFromMap` 读取的 `quantity` 不匹配，导致入库 quantity=0
- **修复 SaveOrder 覆盖问题**：从 `INSERT OR REPLACE` 改为 `INSERT ... ON CONFLICT DO UPDATE`，仅在新值非零/非空时更新，避免后续同步覆盖已有正确数据
- **事件补全字段**：`order_filled` 事件新增 `quantity`、`type`、`realized_pnl`、`exchange` 字段

### Added
- **双盈亏展示（网格 PnL + 交易所 PnL）**：
  - 订单历史页面同时展示「盈利(网格)」和「盈利(交易所)」两列
  - 网格 PnL = `(卖出价 - 买入价) × 数量`，按网格槽位买卖配对计算，反映每个网格周期的利润
  - 交易所 PnL = 币安等交易所按加权平均成本法计算的已实现盈亏（`RealizedPnL` 字段），反映持仓整体的会计核算
  - 两种计算方法的差异说明：网格策略的 PnL 始终为正（低买高卖），而交易所 PnL 可能为负（当整体仓位均价高于卖出价时）
  - 鼠标悬停交易所 PnL 有 Tooltip 说明计算方法差异
- **orders 表新增列**：`filled_qty`（已成交数量）、`exchange`（交易所）、`type`（订单类型）、`realized_pnl`（交易所已实现盈亏）
- **订单同步增强**：`order_sync` 同步时传递 `RealizedPnL`、`FilledQty`、`Exchange` 字段
- **API 增强**：`/api/orders/history` 返回 `filled_qty`、`exchange`、`type`、`exchange_pnl` 字段
- **多语言支持**：新增 `gridPnl`、`exchangePnl`、`exchangePnlTooltip` 国际化键（zh-CN/zh-TW/en-US/ja-JP/ko-KR）

## [3.44.1-rc4] - 2026-02-06

### Fixed
- 修复 main.go 中 Version 变量重复声明的编译错误

## [3.44.1-rc3] - 2026-02-06

### Fixed
- 修复 ConnectionStatusBanner 组件中 keyframes 导入错误（应从 @emotion/react 导入而非 @chakra-ui/react）

## [3.44.1-rc2] - 2026-02-06

### Fixed
- 修复 GitHub Actions 构建失败问题：为 Corepack 下载 Yarn 添加重试机制（应对 repo.yarnpkg.com 临时故障）

## [3.44.1-rc1] - 2026-02-06

### Fixed
- 修复 Dashboard.tsx、Slots.tsx、StrategyRuntimeStatus.tsx 中订单方向（BUY/SELL）和订单状态未翻译的问题
- 完善国际化翻译，确保所有订单相关显示都使用翻译键

## [3.44.0]

### Added
- 新增乌克兰语、孟加拉语、乌尔都语、他加禄语（菲律宾）4 种语言支持

## [3.44.0-rc1] - 2026-02-06

### Added
- 新增波斯语（fa-IR）到语言选择器

## [3.43.2-rc3] - 2026-02-06

### Improved
- 连接状态提示从全宽横幅改为右下角紧凑浮动胶囊，大幅减少屏幕占用
- 后端连通性检测从 REST API 轮询（/api/version 每 10 秒）改为 WebSocket 长连接，实时感知断连/恢复，支持指数退避自动重连

## [3.43.2-rc2] - 2026-02-06

### Fixed
- 修复 ServiceStatusPage.tsx 语法错误导致构建失败
- 优化前端构建分包：Monaco Editor、ECharts、PostHog、Markdown/Diff、html2canvas 独立 chunk
- 增大 PWA 预缓存文件大小上限至 5MB

## [3.43.2-rc1] - 2026-02-06

### Added
- 複合風控引擎擴展與回測/深度風控相關改進；前端與多語言更新

## [3.43.1-rc1] - 2026-02-06

### Fixed
- **修复交易记录重复写入 Bug**：`GridStrategy.OnOrderUpdate()` 重复转发导致每笔订单被处理两次（PnL 膨胀 2 倍、持仓翻倍）
- **修复订单数量被清零**：`SaveOrder` 从 `INSERT OR REPLACE` 改为 `INSERT ... ON CONFLICT DO UPDATE`，保留原始委托量
- **补全 filled_qty 字段**：`order_filled` 事件正确映射 `executed_qty`，补充 `exchange`/`type`
- **数据库三轮去重**：清理精确重复、合计重复、翻倍重复，624 → 324 条
- **修复历史数据**：780 个 FILLED 订单的 quantity/filled_qty 恢复为正确值

### Added
- **净盈亏展示**：Dashboard 卡片分解显示：已完成利润 + 持仓浮亏 - 手续费
- **`/api/statistics` 新增字段**：`unrealized_pnl`、`net_pnl`
- **成交明细弹窗**：订单列表点击盈利值查看委托详情和所有部分成交记录
- **`/api/trades/by-order/:order_id` 接口**：查询卖单的全部成交明细、手续费分摊、汇总
- **`/api/orders/history` 补全字段**：`filled_qty`、`exchange`、`type`

## [3.43.0-rc1] - 2026-02-06

### Added
- **複合風控引擎（Composite Risk Controller）**：
  - 新增 `safety/composite_risk.go`：風控因子接口、加權聚合、級別映射、定時評估
  - 五個風控因子：AI 新聞、均線趨勢、資金費率、市場深度、K 線異常（`factor_news.go`、`factor_trend.go`、`factor_funding.go`、`factor_depth.go`、`factor_kline.go`）
  - 配置項 `composite_risk`：啟用開關、評估間隔、閾值（caution/reduce_position/pause_buying/stop_trading）、各因子權重
  - `SuperPositionManager` 集成：根據複合風控結果調整買入（RiskStopTrading 撤買單並返回、RiskPauseBuying 暫停買入、RiskReducePosition/RiskCaution 縮減買單數量）
  - `symbol_manager` 初始化複合風控並注入新聞監控（main 中設置 NewsMonitor 後寫入各 runtime）
  - API `GET /api/composite-risk`：返回當前複合風控狀態與各因子評分

## [3.42.0-rc2] - 2026-02-06

### Fixed
- **修复前端构建错误**：在 `config.ts` 中补充导出 `getSecurityStatus` 和 `generateMasterKey` 函数，解决 Configuration.tsx 导入失败导致的构建失败

## [3.42.0] - 2026-02-06

### Added
- **配置加密自动支持明文和密文**：
  - `LoadConfig` 函数自动检测并解密敏感字段（API Key、Secret Key、Passphrase、AI API Key）
  - 支持明文配置（无加密前缀）：直接使用，无需主密钥
  - 支持密文配置（有加密前缀如 `AKIA...`）：自动解密，需要主密钥
  - 新增 `LoadMasterKey` 函数：仅在主密钥存在时加载，不会自动生成
  - 向后兼容：现有明文配置无需修改即可正常工作

## [3.41.0] - 2026-02-06

### Added
- **遙測（Telemetry）**：可選匿名使用統計（PostHog），含文檔與腳本（TELEMETRY_*.md、scripts/verify_telemetry.sh 等）
- **配置加密**：`config/encryption.go` 支援敏感配置加密存儲
- **多語言**：新增 i18n 語言 bn-BD、fa-IR、ja-JP、pl-PL、th-TH、tl-PH、uk-UA、ur-PK（後端 toml + 前端 JSON）
- **文檔**：推廣與安全相關文檔（PAID_PROMOTION_GUIDE、TELEGRAM_PROMOTION_QUICKSTART、security-enhancements 等）、sync 模塊

## [3.40.0-rc1] - 2026-02-05

### Added
- **新聞風控 AI Provider 和多時間間隔配置**：
  - 支援多種 AI Provider：Gemini、OpenAI、Claude、Poe
  - 每個 Provider 支援多個模型選擇（如 gpt-4、claude-3-opus 等）
  - 可配置分析時間間隔：5分鐘、15分鐘、30分鐘、1小時、2小時、4小時、8小時、24小時
  - 前端配置頁面新增 AI Provider 選擇器、模型選擇器、API Key 輸入框和 Base URL 輸入框
  - 後端新增統一的 AI 客戶端接口（`AIClient`），支援動態切換 Provider
  - Google Search 功能僅 Gemini 原生支援，其他 Provider 通過 prompt 增強
  - 保持向後兼容，現有配置無需修改即可工作

## [3.39.0-rc1] - 2026-02-05

### Fixed
- **一键平仓交易对匹配失败**：统一交易对键的大小写规范（exchange 小写、symbol 大写），避免大小写不一致导致找不到运行时

## [3.39.0] - 2026-02-05

### Added
- **方向顯示與切換確認**：
  - 訂單、持倉、槽位、對賬、Dashboard、全局概覽、策略運行狀態、交易對管理列表等處顯示做多/做空 Badge
  - 切換交易對方向時彈出確認，確認後依次執行：撤單 → 平倉 → 停止交易 → 更新配置 → 重啟交易
  - 新增交易對默認做多，可編輯
  - 交易對管理卡片新增「默認交易方向」選擇，新交易對未單獨設置時使用

## [3.38.0] - 2026-02-05

### Added
- **交易方向配置**：支援按交易對設置做多/做空方向（`direction: LONG/SHORT`，預設 LONG）
  - 配置：`SymbolConfig` 新增 `direction` 字段，前端配置頁交易對參數新增方向下拉
  - 網格策略：根據方向調整開倉/平倉與槽位價格（LONG：買低賣高；SHORT：賣高買低）
  - 持倉恢復：做空時從負數持倉恢復買單平倉槽位
- **新聞分析功能開關**：新增 `news_monitor.enable_analysis` 配置項，可單獨控制是否啟用新聞分析功能（Gemini 分析）
  - 關閉後僅收集新聞，不會定時調用 Gemini 對 BTC 市場做出預判
  - 前端配置頁面新增「啟用新聞分析」開關，僅在啟用新聞監控時顯示

### Fixed
- **NewsAPI 查詢字符串過長問題**：修復新聞收集時查詢字符串超過 NewsAPI 500 字符限制的問題
  - 自動限制關鍵詞數量，最多使用 15 個最重要的關鍵詞
  - 優先使用用戶配置的關鍵詞和核心關鍵詞（如 bitcoin, btc, cryptocurrency）
  - 每個資產最多取前 10 個關鍵詞，確保查詢字符串符合 NewsAPI 要求

## [3.37.0] - 2026-02-04

### Added
- **價格偏差統計功能**：新增價格偏差（slippage）統計，幫助了解因委託價格與實際成交價格差異導致的損失
  - 數據庫：`trades` 表新增 `buy_price_deviation` 和 `sell_price_deviation` 字段，記錄每筆交易的價格偏差
  - 後端：統計 API 返回買入/賣出價格偏差總和，Dashboard API 返回價格偏差損失
  - 前端：Dashboard 頁面新增價格偏差統計卡片，顯示買入偏差、賣出偏差和總損失
  - 回測系統：回測報告新增「價格偏差（slippage）累計損失」統計，顯示回測期間因價格偏差導致的累計損失
  - 實盤交易：`SuperPositionManager` 自動計算並記錄每筆交易的價格偏差，當理論盈利轉為實際虧損時記錄警告

## [3.36.0] - 2026-02-04

### Added
- **订单管理历史订单盈利列**：历史订单表格新增「盈利」列，已成交的卖单显示对应盈亏（从 trades 表关联），买入订单及无匹配交易的卖单显示「-」

## [3.35.0-rc4] - 2026-02-04

### Fixed
- **持倉模式 -4061 不再導致進程退出**：遇幣安「雙向持倉/單向持倉」錯誤碼 -4061 時改為記錄錯誤並返回，進程繼續運行，避免服務反覆重啟；用戶在交易所改為單向持倉後可手動重試

## [3.35.0-rc3] - 2026-02-04

### Fixed
- **持倉彙總頁未啟動交易時報錯**：修復未啟動交易時 `GET /api/positions` 返回 `{ "positions": [] }` 與前端期望的 `{ "summary": { "positions": [], ... } }` 不一致，導致「Invalid response format. Response keys: positions」報錯；無 provider 時改為返回與有 provider 時一致的 summary 結構

## [3.35.0-rc2] - 2026-02-04

### Fixed
- **交易所手续费率支持小数输入**：修复配置页「手续费率」输入框无法输入小数点的问题，可正常填写 0.0004 等费率

## [3.35.0-rc1] - 2026-02-04

### Improved
- **参数优化结果标题补全**：标题展示交易对、策略、K 线类型、风控参与（含订单深度有/无）及 K 线起止时间；当结果缺少任务上下文时自动使用任务信息补全

## [3.34.0-rc13] - 2026-02-04

### Fixed
- **概览页网格统计与槽位矩阵一致**：修复资金分配中「已用/已预留」显示为 0% 而槽位矩阵有大量 FILLED 的不一致问题
  - 原因：持仓恢复时只更新了仓位层 AllocationManager，策略层 CapitalAllocator 的 Used 未同步
  - 策略资金分配 API 汇总时改为优先使用仓位层（AllocationManager）的已用金额，与槽位 FILLED 状态一致

## [3.34.0-rc12] - 2026-02-04

### Added
- **参数优化结果弹窗标题增强**：查看任务结果时，标题展示交易对、策略、K 线类型（tick 线/1m 线等）、风控参与说明（成交量风控、是否订单深度）、K 线起始与结束时间，便于区分不同任务
  - 后端：优化结果 JSON 增加 `symbol`、`interval`、`start_time`、`end_time`、`use_order_depth`、`risk_info` 等任务上下文字段
  - 前端：`OptimResultModal` 根据结果中的任务上下文拼接标题；旧结果文件无上下文时仍显示「参数优化结果」

## [3.34.0-rc11] - 2026-02-04

### Added
- **收益统计增强**：每日 0 点记录未实现盈利快照；日历视图与每日统计表新增「未实现盈亏」「账面盈亏（已平仓+未实现）」列，便于查看真实账面值

## [3.34.0-rc10] - 2026-02-04

### Fixed
- **盈利管理翻译缺失**：补全 `profitManagement.netProfit`（净盈利）在所有 16 种语言中的翻译，修复繁体中文等语言下显示为 key 的问题

## [3.34.0-rc9] - 2026-02-04

### Fixed
- **回测配置支持小数输入**：修复新建回测时「风控-成交量倍数」等参数只能输入整数的问题，现在支持 2.5、3.5 等小数值
- **实盘配置支持小数输入**：同步优化实盘配置中「成交量倍数」输入框，增加精度支持

## [3.34.0-rc8] - 2026-02-04

### Fixed
- **关键空指针崩溃修复**：修复参数优化功能中导致服务崩溃的多个空指针问题
  - 修复 `backtest/data_fetcher.go:460` 中 `os.Stat` 错误未检查导致的空指针访问
  - 修复 `backtest/optimizer/universal_optimizer.go:269` 中 `res` 为 nil 时仍然访问 `res.Metrics` 的问题
  - 修复 `backtest/optimrun/manager.go:111` 中传入 nil context 导致的空指针问题
  - 修复 `tools/diagnose_events.go:48` 中类似的 `os.Stat` 空指针问题
  - 增强了参数优化器的错误处理和空值检查机制

## [3.34.0-rc7] - 2026-02-04

### Fixed
- **K 线文件加载失败修复**：修复回测页面切换到「K 线文件」数据来源时显示「载入 K 线文件列表失败」的问题
  - 修复 `storage/kline_file.go` 中时间字段读取错误（`created_at`、`updated_at` 需支持字符串和 Unix 时间戳两种格式）
  - 新增 `monitor/kline_collector.go` 启动时自动同步所有现有 K 线文件到数据库的功能
  - 现在服务启动时会自动将 `./data/kline` 目录下的所有 CSV 文件同步到 `kline_files` 表

## [3.34.0-rc6] - 2026-02-04

### Improved
- **回测报告记录数量优化**：将风控介入记录、成对交易、未成对交易的显示数量从 30/50 条减少为 10 条，报告更简洁

### Fixed
- **回测配置显示正确的初始资金**：修复回测报告配置表中 `total_capital` 显示错误的问题，现在使用任务级别的初始资金而非 params 中的值

## [3.34.0-rc5] - 2026-02-04

### Improved
- **回测风控对比优化**：将回测结果中「无风控」和「有风控」的各项指标改为表格对比形式，同一项指标在同一行显示，方便快速对比

## [3.34.0-rc4] - 2026-02-04

### Fixed
- **回测任务列表显示缓存数据源的币种信息**：修复使用 K 线缓存创建回测任务时，任务列表中交易对、周期、时间范围等字段为空的问题
  - 前端：创建缓存任务时从缓存元信息中提取 symbol、interval、start、end 并传递给后端
  - 后端：修复 `updateCacheIndex` 缓存名解析逻辑，支持 4 段（`BTCUSDT_1m_start_end`）和 5 段（`binance_BTCUSDT_1m_start_end`）两种格式
  - 后端：`ListCache` 增加自愈逻辑，对于旧索引中 symbol/interval/start/end 为空的条目，自动从缓存名解析填充

## [3.34.0-rc2] - 2026-02-04

### Fixed
- **回测页面 K 线文件列表加载失败提示**：回测页面加载时，若 K 线收集器未初始化不再弹出错误提示，改为静默处理；用户手动刷新时才显示警告

## [3.34.0-rc1] - 2026-02-04

### Fixed
- **回测参数输入框支持小数**：修复风控-成交量倍数等参数无法输入小数点的问题，现在可以输入 2.5、3.5 等小数值

## [3.34.0] - 2026-02-03

### Added
- **回测支持 K 线文件与深度风控**：回测可选择数据来源（交易所拉取 / K 线文件 / 缓存），支持从本地 K 线文件或回测缓存加载数据；深度风控支持配置档位与比例，在有深度数据时自动启用
  - 后端：`BacktestTask` 新增 `DataSource`、`KlineFile`、`CacheName` 字段；`backtest/data_source.go` 支持从 K 线文件和缓存加载；`backtest/risk_simulator.go` 支持深度风控参数与判断
  - 前端：回测菜单增加数据来源选择，可选「交易所」「K 线文件」「缓存」，并展示文件时间范围、K 线数量、是否带深度等信息
- **K 线文件统一管理**：K 线文件元信息统一入库管理，便于回测与收集器协同
  - 新增 `storage/kline_files` 表及 CRUD；迁移脚本扫描 `./data/kline` 与回测缓存目录导入 `kline_files`
  - `monitor/kline_collector.go` 写入 K 线时同步更新 `kline_files`，并定期更新文件状态
  - 新增 `GET /api/kline-files/available` 返回 `status=completed` 的可用文件列表；创建回测任务时校验所选文件为 completed
  - 回测菜单中 K 线文件选项仅展示已完成文件，并显示时间范围、深度、数据来源等

## [3.33.0] - 2026-02-03

### Added
- **策略可视化功能**：为DCA、趋势跟踪、均值回归、网格等主要策略添加可视化展示
  - 在Dashboard首页为每个有资金分配的策略显示可视化卡片
  - DCA策略可视化：显示分层持仓、ATR动态间距、止盈止损线、决策依据
  - 趋势跟踪策略可视化：显示快慢均线、金叉/死叉信号、趋势方向、持仓状态
  - 均值回归策略可视化：显示布林带上下中轨、价格在布林带中的位置、买入/卖出信号
  - 网格策略可视化：显示网格价格区间、槽位状态、填充率
  - 后端：扩展 `StrategyRuntimeStatusResponse` 添加 `visualizationData` 字段，为各策略实现 `GetVisualizationData()` 方法
  - 前端：新增 `strategy-visualization` 组件库，包含 `PriceChart` 共享组件和各策略专用可视化组件
  - API：`/api/strategies/runtime` 接口现在返回策略可视化数据
  - 实时更新：可视化数据随策略状态每5秒自动刷新

## [3.32.1] - 2026-02-03

### Fixed
- **修复"释放"按钮404错误**：修复概览页面点击策略资金"释放"按钮时返回404的问题
  - 原因：`/api/strategies/:name/release-capital` 与 `/api/strategies/:id` 路由冲突
  - 解决：将 release-capital 路由移入 strategies 路由组，统一使用 `:id` 参数
  - 影响文件：`web/server.go`、`web/api.go`

## [3.32.0] - 2026-02-03

### Added
- **K线数据自动收集与管理**：新增K线数据自动收集、存储和管理功能
  - 自动收集tick级K线数据（最新24小时，每分钟更新）
  - 自动收集分钟级K线数据（带订单深度，每分钟更新）
  - 自动收集小时级K线数据（带订单深度，每小时更新）
  - 支持前5大交易所（binance, okx, bybit, bitget, gate）和主要币种
  - CSV文件存储到 `./data/kline` 目录
  - 7天自动清理未保护的文件
  - 文件保护机制：用户可保护重要文件不被自动删除
  - 后端：`monitor/kline_collector.go`、`web/api_kline_files.go`、`storage/sql_storage.go`（新增 `protected_kline_files` 表）
  - 前端：`webui/src/components/KlineFilesManager.tsx`、`webui/src/services/klineFiles.ts`
  - API：`GET /api/kline-files`、`POST /api/kline-files/:filename/protect`、`DELETE /api/kline-files/:filename/protect`、`GET /api/kline-files/:filename/download`

- **回测报告导出功能**：在数据导出页面新增回测报告导出功能
  - 后端：`GET /api/export/backtest-reports` 接口，打包所有回测报告（.md和.csv文件）为ZIP
  - 前端：导出页面新增回测报告导出选项
  - 支持导出所有回测报告文件（Markdown报告和CSV权益曲线）

## [3.31.0] - 2026-02-03

### Added
- **每日统计增加资金费用**：每日统计表格和日历组件现在显示资金费率（Funding Fee）
  - 后端：新增 `GetDailyFundingPayments` 接口，按日期汇总资金费用
  - 每日统计 API 返回新增 `funding_fee` 字段
  - 日历组件：每日格子中显示资金费用（正数绿色，负数橙色）
  - 表格：新增「资金费用」列，显示每日资金费净额
  - 帮助排查交易盈利但账户资金减少的问题（资金费率支出）

- **参数自动优化**：新增「参数优化」功能，可自动遍历参数组合寻找最优解
  - 在回测菜单新增「参数优化」Tab，选定策略、交易对、日期范围、K 线周期与初始资金后，程序自动遍历不同参数组合回测
  - 支持策略：网格(grid)、动量(momentum)、均值回归(mean_reversion)、趋势跟踪(trend_following)、定投(dca)、马丁格尔(martingale)
  - 各策略有默认参数搜索范围（如网格：间距 100~500、订单金额 50~200、风控倍数 2~5）
  - 结果表格支持筛选（最大回撤≤X%、收益率≥X%、交易次数≥X）与排序（按收益率、夏普比率、最大回撤、胜率）
  - 异步执行，可查看进行中任务进度与历史完成任务
  - 后端：`backtest/optimizer/universal_optimizer.go`、`backtest/optimrun/`、`storage/optim_task.go`、`/api/backtest/optim/*` API
  - 移除侧边栏旧的「参数优化」入口，功能整合至回测菜单

### Fixed
- **界面语言修复**：修复选择简体中文时部分界面仍显示繁体中文的问题
  - BacktestMenu 组件：约 50 处繁体→简体（任務→任务、結果→结果、優化→优化、顯示→显示、載入→载入等）
  - OptimResultModal 组件：10 处繁体→简体（參數優化結果→参数优化结果、導出→导出、勝率→胜率等）

## [3.30.0] - 2026-02-03

*版本跳过，功能已整合到 3.31.0*

## [3.29.4] - 2026-02-03

### Added
- **订单取消功能**：待成交订单列表新增取消按钮，支持单个取消和批量取消全部待成交订单
  - 后端：新增 `POST /api/orders/:id/cancel` 和 `POST /api/orders/cancel` 接口
  - 前端：订单列表每行新增取消按钮，顶部新增「取消全部」按钮

- **资金概览增强**：Dashboard 资金分配卡片现在显示策略预留资金详情
  - 显示每个策略的：已分配、已预留（锁定）、可用资金
  - 使用进度条和颜色标签直观展示资金占用率

- **释放锁定资金功能**：支持手动释放被错误锁定的策略资金
  - 后端：新增 `POST /api/strategies/:name/release-capital` 和 `POST /api/strategies/release-all-capital` 接口
  - 前端：资金分配卡片中每个有锁定资金的策略旁新增「释放」按钮，底部新增「释放全部锁定资金」按钮

## [3.29.3] - 2026-02-03

### Added
- **订单列表显示策略来源**：待成交订单列表新增「策略」列，显示每个订单来自哪个策略（如 Grid-BTCUSDT、DCA 等）
  - 后端：`InventorySlot` 和 `SlotInfo` 结构新增 `StrategyName`、`StrategyType` 字段
  - 前端：订单管理页面待成交 Tab 新增策略列，使用颜色标签区分策略类型

## [3.29.2] - 2026-02-03

### Added
- **服務條款與隱私政策擴充**：服務條款與隱私政策內容擴充為更長、更正式的專業版本，適用金融/量化場景
  - 服務條款：14 條（條款接受、定義、服務描述、使用資格與合規、風險披露與金融免責、非投資建議、用戶責任、禁止用途、知識產權、責任限制、賠償、終止、適用法律與爭議解決、聯繫我們）
  - 隱私政策：11 條（引言與適用範圍、我們可能涉及的信息、使用目的、數據存儲與安全、第三方與交易所 API、Cookie 與會話、數據保留、您的權利、國際與本地部署、政策變更、聯繫我們）
  - 聯繫方式統一為 contact@quantmesh.io
  - 簡體中文、繁體中文、英文、德文為完整翻譯；其餘語言目前使用英文正文，頁面標題與導航仍為各語言

## [3.29.1-rc4] - 2026-02-03

### Fixed
- **交易所概覽默認展開**：修復交易所概覽區塊在數據加載後不會自動展開的問題，現在所有交易所默認展開顯示

## [3.29.1-rc3] - 2026-02-03

### Fixed
- **日曆視圖時區修正**：修復統計日曆按 UTC 時間分組導致日期錯位的問題，現按配置時區（如 Asia/Shanghai）正確分組每日統計數據
  - 涉及：`storage/sql_storage.go`（`QueryDailyStatisticsByExchange` 使用時區偏移）、`utils/timezone.go`（新增 `GetTimezoneOffsetSeconds`）

## [3.29.1-rc2] - 2026-02-03

### Changed
- **回測報告風控介入記錄分類顯示**：「風控介入記錄」區塊現分為「有跳過買入的介入」和「無跳過買入的介入」兩個表格，各顯示前 30 條，便於查看重點介入事件

## [3.29.1-rc1] - 2026-02-03

### Fixed
- **回測風控參數支持小數**：「風控-成交量倍數」和「手续费率」參數現在支持輸入小數值（如 2.5、0.0004），後端新增 `step` 欄位定義步長

## [3.29.1] - 2026-02-03

### Added
- **回測報告顯示配置與參數**：回測報告新增「回测配置」區塊，包含 K 線周期、回測時間範圍、所有回測參數（含策略參數與風控參數）以表格形式呈現
  - 涉及：`backtest/report_generator.go`（新增 `ReportMeta`、`ReportParamRow`）、`backtest/task_manager.go`

### Fixed
- **回測報告保存為圖片優化**：修復保存為圖片時使用克隆元素避免修改原始 DOM，改善截圖完整性

## [3.29.0] - 2026-02-03

### Added
- **回測報告保存為圖片**：回測結果彈窗新增「保存為圖片」按鈕，可將完整報告（含 K 線走勢圖表）導出為 PNG 高清圖片下載
  - 使用 `html2canvas` 庫將模態框內容渲染為圖片
  - 自動展開滾動區域確保完整截圖
- **回測風控參數可配置**：網格策略回測時支持在開始回測前指定風控模擬器參數（帶默認值，可微調）
  - `風控-成交量倍數`：成交量超過均量的倍數觸發風控（默認 3.0，越小越敏感）
  - `風控-均線窗口`：計算均價/均量的 K 線數量（默認 20）
- **帶風控回測對比**：網格策略回測時自動執行兩次（無風控 + 有風控），在同一份報告中呈現對比數據
  - 風控模擬邏輯與實盤 RiskMonitor 對齊（價格低於均線且成交量放大時觸發，跳過買入信號）
  - 報告包含：無風控 vs 有風控的收益率、最大回撤、交易次數等對比
  - 風控介入記錄表：時間、原因、類型、持續 K 線數、跳過的買入數
  - 風控效果分析：說明風控是否起到保護作用
- **期末持倉**：報告與彈窗新增「期末持倉（幣的數量）」「期末持倉市值」顯示
- **交易指標**：新增「買入次數」「賣出次數」顯示
- **回測報告**：新增成對交易（前50筆）、未成對交易（前50筆）明細表
- **K 線走勢圖**：回測報告彈窗內新增期間 K 線收盤價走勢圖，拆為 4 段折線圖便於查看
  - 新增 API：`GET /api/backtest/tasks/:id/klines`
- **當前持倉按交易所、币种、策略列出**：概覽頁「當前持倉」現支持按交易所、币种、策略維度展示
  - 單幣種概覽：持倉卡片標題顯示 交易所 · 币种 · 策略（如 BINANCE · BTCUSDT · 网格）
  - 全局概覽：新增「當前持倉」表格，列出所有有持倉的交易對，按交易所、币种、策略分列，可點擊行切換到該幣種詳情
  - 新增 API：`GET /api/positions/summary/all` 獲取所有交易對持倉彙總
- **服務狀態頁**：新增服務狀態監控頁面（ServiceStatusPage）

### Changed
- **回測報告彈窗展示**：點擊「查看」時改為以 Dialog（Modal）形式展示回測結果，便於專注閱讀；支持下載報告
- **回測報告小數格式統一**：統計值和專用指標改為小數點後 4 位（收益率、夏普比率、回撤率、勝率、利潤因子等），價格和數量保持原有格式

### Fixed
- **K 線走勢圖不顯示**：修復回測報告彈窗中「期間 K 線走勢」四段圖表框架存在但折線不渲染的問題
- **回測報告首次加載卡住**：修復第一次點擊新產生的回測報告時一直顯示「載入中」的問題
- **回測報告 Markdown 渲染**：回測任務列表點擊「查看」時，報告內容現正確渲染 Markdown（標題、表格、代碼塊等），不再顯示原始文本
- **回測任務列表高亮**：任務列表中當前查看的任務行增加背景高亮，便於辨識
- **回測無需 Binance API 配置**：回測獲取歷史 K 線時，若未配置 Binance API，自動使用公開數據適配器
- **回測使用已配置的 Binance API**：TaskManager 創建時從配置中讀取 Binance API，不再硬編碼為空
- **回測參數重複**：移除回測頁策略參數中的 `total_capital` 顯示，避免與「總投入資金」重複
- **回測參數提示**：補充網格策略「價格上/下限」填寫提示，協助理解區間含義
- **K 線圖異常插針**：新增通用 `exchange.ClipKlineSpikes`，在所有交易所的 K 線出口統一裁剪插針（High/Low 限制在鄰近價格 ±3% 內）

## [3.28.11] - 2026-02-03

### Added
- **當前持倉按交易所、币种、策略列出**：概覽頁「當前持倉」現支持按交易所、币种、策略維度展示
  - 單幣種概覽：持倉卡片標題顯示 交易所 · 币种 · 策略（如 BINANCE · BTCUSDT · 网格）
  - 全局概覽：新增「當前持倉」表格，列出所有有持倉的交易對，按交易所、币种、策略分列，可點擊行切換到該幣種詳情
  - 新增 API：`GET /api/positions/summary/all` 獲取所有交易對持倉彙總
  - 涉及：`web/api.go`、`web/server.go`、`webui/src/components/Dashboard.tsx`、`webui/src/components/GlobalDashboard.tsx`、`webui/src/services/api.ts`、i18n

## [3.28.10-rc2] - 2026-02-03

### Fixed
- **回測參數重複**：移除回測頁策略參數中的 `total_capital` 顯示，避免與「總投入資金」重複
  - 涉及：`webui/src/components/BacktestMenu.tsx`
- **回測參數提示**：補充網格策略「價格上/下限」填寫提示，協助理解區間含義
  - 涉及：`backtest/strategy_params.go`、`webui/src/components/BacktestMenu.tsx`
- **回測參數校驗提示**：明確「價格上/下限」需大於 0，且上限需大於下限
  - 涉及：`backtest/strategy_params.go`

## [3.28.9] - 2026-02-03

### Fixed
- **K 線圖異常插針**：本地 K 線圖出現超過 8 萬的異常上影線（插針），與 24h 內實際價格不符
  - 原因：交易所歷史 K 線接口可能返回壞 tick/異常數據，且原先僅打日志、未裁剪
  - 改動：新增通用 `exchange.ClipKlineSpikes`，在 **所有交易所** 的 K 線出口統一裁剪插針（High/Low 限制在鄰近價格 ±3% 內）；Binance 僅保留 detectPriceSpikes 日誌
  - 涉及：`exchange/spike_filter.go`（新增）、`web/api.go`（getKlines、日 K 線統計處調用 ClipKlineSpikes）、`exchange/binance/adapter.go`（移除重複裁剪）
- **文檔**：新增 `docs/KLINE_DATA_ANOMALIES.md`，說明源頭數據為何會出現插針（壞 tick、流動性枯竭、數據聚合問題等）及本專案處理方式

## [3.28.7] - 2026-02-02

### Added
- **网格参数优化页交易所与交易对来自 API**: 交易所下拉改为通过 `/api/backtest/exchanges` 获取全部支持的交易所（Binance、Bitget、OKX、Bybit、Gate），交易对通过 `/api/backtest/symbols` 按所选交易所与市场类型获取，不再写死为 Binance/Bitget 与少量交易对
  - 涉及：`webui/src/components/OptimizerPage.tsx`

## [3.28.6-rc1] - 2026-02-02

### Fixed
- **編譯錯誤修復**: 修復 CI 編譯失敗
  - Spot wrapper 的 `GetOrderFills` receiver 寫錯：`*okx_spotWrapper` 等改為正確的 `*okxSpotWrapper`（okx/gate/bybit/bitget 四個 spot wrapper）
  - `positionExchangeAdapter` 補上 `GetOrderFills` 方法以實現 `position.IExchange`
  - 涉及：`exchange/wrapper_okx_spot.go`、`exchange/wrapper_gate_spot.go`、`exchange/wrapper_bybit_spot.go`、`exchange/wrapper_bitget_spot.go`、`main.go`

## [3.28.6] - 2026-02-02

### Fixed
- **PWA / Service Worker 與 API 延遲**: 正式版包含 rc4/rc5 相關修復
  - 不再為 `/api`、`/ws` 註冊 Service Worker 的 runtimeCaching，請求直接走瀏覽器網絡，避免 SW 攔截導致冷啟動與延遲（如 `/api/version` 需 3 秒）
  - 移除 main.tsx 中重複的 Service Worker 註冊，僅保留 vite-plugin-pwa 自動註冊，避免雙重註冊導致 Chrome 載入卡住
  - 在 `vite.config.js` 中補充註釋說明為何不讓 SW 攔截 API
  - 涉及：`webui/vite.config.js`、`webui/src/main.tsx`
- **新聞分析價格預測格式**、**新聞收集顯示**、**登入頁重複請求**、**策略配比 Hooks**、**收益日曆今天焦點**、**DCA 資金釋放**等修復已包含於 3.28.5-rc1～rc5，本版統一發佈為 3.28.6。

## [3.28.5-rc5] - 2026-02-02

### Fixed
- **新聞分析價格預測格式修復**: 修復 Gemini 返回的價格預測格式不正確的問題（如返回 `2hcount_down_5_percent_...` 而非結構化 JSON）
  - 在 prompt 中添加明確的 JSON 示例，指導 AI 輸出正確格式
  - 強化 JSON Schema 的 enum 和 description 約束
  - 添加 `normalizeTimeframe` 函數，自動修正異常的時間窗口格式
  - 添加 probability 自動校正（如百分比轉小數）
  - 涉及：`monitor/gemini_news_analyzer.go`

- **新聞收集顯示修復**: 修復「已收集新聞」一直顯示「暫無」的問題
  - 原因：NewsAPI 返回的新聞通常超過 2 小時，被過濾後緩存為空
  - 改動：緩存保留 24 小時內的新聞，前端顯示所有緩存新聞；AI 分析仍使用最近 2 小時的新聞
  - 添加更多日誌幫助排查收集問題
  - 涉及：`monitor/news_collector.go`、`monitor/news_monitor.go`、`webui/src/components/NewsAnalysis.tsx`

## [3.28.5-rc4] - 2026-02-02

### Fixed
- **登入頁刷新變慢 / 請求長時間 Pending**: 讓 API 與 WebSocket 請求不再經由 Service Worker，直接走瀏覽器網絡，避免 SW 攔截導致請求排隊、刷新變慢
  - 涉及：`webui/vite.config.js`（移除 `/api`、`/ws` 的 runtimeCaching）
- **登入頁減少重複請求**: ConnectionStatusBanner 在登入頁延遲 3 秒再執行首次後端檢查，避免與登入頁的 `/api/version` 同時發送
  - 涉及：`webui/src/components/ConnectionStatusBanner.tsx`

## [3.28.5-rc3] - 2026-02-02

### Fixed
- **策略配比頁面 Hooks 規則修復**: 將 `useMemo` 置於條件 return 之前，遵守 React Hooks 調用順序
  - 涉及：`webui/src/components/StrategyAllocation.tsx`

## [3.28.5-rc2] - 2026-02-02

### Fixed
- **收益統計日曆「今天」焦點修復**: 修復日曆視圖在非 UTC 時區下「今天」高亮錯位的問題
  - 原因：使用 `toISOString().split('T')[0]` 得到的是 UTC 日期，在 UTC+8 等時區會導致焦點仍停留在昨日
  - 改動：改為使用本地時區的年/月/日組今日期字串，與日曆格子一致
  - 涉及：`webui/src/components/StatisticsCalendar.tsx`

## [3.28.5-rc1] - 2026-02-02

### Fixed
- **DCA/多策略資金釋放修復**: 修復 DCA 等策略配置固定資金池後仍報「資金不足」的問題
  - 原因：下單時會預留資金（Reserve），但訂單成交或取消後未釋放（Release），導致「可用」只減不增
  - 改動：MultiStrategyExecutor 記錄每筆訂單的預留金額，訂單 FILLED/CANCELED 時按 orderID 釋放；訂單流回調中通知策略層並調用釋放
  - 涉及：`strategy/multi_strategy_executor.go`、`symbol_manager.go`

## [3.28.5] - 2026-02-02

### Fixed
- **Binance 現貨價格流首價超時修復**: 修復 PAXGUSDT 等低流動性交易對啟動時「等待首個價格超時（10秒）」的問題
  - 原因：現貨價格流使用 aggTrade，僅在有成交時推送；低流動性對可能長時間無成交導致收不到首價
  - 改動：現貨改為使用 miniTicker 流（每 1 秒推送最新價），可穩定取得首價；首價等待超時由 10 秒改為 15 秒
  - 涉及：`exchange/binance/spot_websocket.go`、`exchange/binance/spot_adapter.go`

## [3.28.4] - 2026-02-02

### Fixed
- **Binance 杠杆倍數獲取修復**: 修復資金分配檢查時杠杆顯示錯誤為 1x 的問題
  - 問題原因：Binance adapter 的 `Account` 結構體缺少 `AccountLeverage` 欄位，導致資金分配計算時使用錯誤的杠杆值
  - 修復內容：
    - `binance/adapter.go`：新增 `AccountLeverage` 欄位，從持倉資訊中提取杠杆倍數
    - `wrapper_binance.go`：傳遞 `AccountLeverage` 值到通用 Account 結構
    - `super_position_manager.go`：修復從 `GetPositions` 獲取杠杆的反射邏輯，正確處理 `[]*Position` 類型
  - 影響：資金分配檢查現在會使用正確的杠杆計算實際保證金（訂單價值 / 杠杆）

## [3.28.3] - 2026-02-02

### Fixed
- **AI 任務超時處理**: 修復 AI 任務長時間顯示「運行中」不超時的問題
  - 問題：HTTP 未響應 context 取消時，任務可運行 99 分鐘仍顯示運行中
  - 新增 `GetStaleRunningTasks`：定期查出已超過 `TimeoutSeconds` 仍為 running 的任務並標記為 timeout
  - 處理器每輪詢（2 秒）先標記超時任務，再處理 pending
  - `context.DeadlineExceeded` 時將狀態設為 timeout 而非 failed
  - 預設任務超時由 15 分鐘改為 5 分鐘（`gemini_client` 與 DB 預設）

## [3.28.2] - 2026-02-02

### Fixed
- **DCA 策略平倉資金檢查修復**: 修復 DCA 策略觸發止損/止盈時因資金檢查導致平倉失敗的問題
  - 問題原因：`MultiStrategyExecutor.PlaceOrder()` 對所有訂單都進行資金充足檢查，但平倉（賣出）操作是釋放資金而非消耗資金
  - 修復方案：當 `ReduceOnly=true` 或 `Side=SELL` 時跳過資金檢查和預留
  - 影響範圍：`PlaceOrder()` 和 `BatchPlaceOrdersWithDetails()` 兩個方法

## [3.28.0] - 2026-02-02

### Added
- **智子巡檢 (Sophon Inspector)**: 智能中控/巡檢系統，彙總交易、風控、市場與新聞數據，並支援定時與緊急通知
  - 新增 `inspector/` 模塊：數據收集器、AI 分析引擎、事件監測器、報告生成器、調度器、黃金專項分析
  - 定時彙總報告：可配置常規間隔（預設 1h）、靜默時段（如 23:00–07:00 改為 4h 間隔）
  - 緊急事件立即通知：風控觸發/恢復、新聞風險評分突變、資金費率異常、賬戶餘額變動、黃金與 BTC 相關性突變
  - 報告內容：資金概覽、持倉狀態、盈虧統計、風控狀態、新聞風險、黃金專項（價格、與 BTC 相關性、避險情緒）
  - 可選 Gemini AI 分析：一句話總結、重要發現、操作建議、需關注幣種、黃金洞察
  - 配置節 `inspector`：啟用、名稱、調度、閾值、關注交易對、AI、報告格式
  - 通知：新增事件類型 `EventTypeInspectorReport`，支援 Telegram/Email 等渠道發送報告正文
  - 存儲：新增 `inspection_reports` 表與 `SaveInspectionReport` 接口，歷史報告可查
- **新聞監控擴展**: `news_monitor.custom_rss_feeds` 支援用戶自定義 RSS 源，與現有 `rss_feeds` 合併使用

### Changed
- 配置預設：`notifications.rules.inspector_report` 預設為 true（智子巡檢報告可通過現有通知渠道發送）

## [3.27.0] - 2026-02-02

### Security
- **認證數據丟失保護**: 新增 `.installed` 標記文件機制，防止數據庫被刪除後繞過認證
  - 首次設置密碼時自動創建 `data/.installed` 標記文件
  - 系統啟動時檢查：如果 `.installed` 存在但 `auth.db` 中無密碼記錄，阻止重新設置密碼
  - 前端顯示安全警告頁面，提示管理員檢查數據目錄
  - 新增 `IsSecurityCompromised()` 方法檢測安全隱患
  - 新增 `security_compromised` 字段在認證狀態 API 中返回
- **防止 Docker 部署數據丟失**: 當容器重新部署時未掛載 `data` 目錄，不再允許繞過認證重新設置密碼

### Added
- `PasswordManager.IsInstalled()`: 檢查系統是否已完成首次設置
- `PasswordManager.IsSecurityCompromised()`: 檢查認證數據是否丟失
- `PasswordManager.createInstalledMarker()`: 創建安裝標記文件
- 前端 `securityCompromised` 狀態：在 AuthContext 中追蹤安全隱患
- 前端安全警告頁面：當檢測到數據丟失時顯示詳細說明和處理建議

## [3.26.0] - 2026-02-02

### Added
- **新聞監控配置 UI**: 在配置管理頁面添加新聞監控配置項
  - 在全局視圖「API 配置」選項卡新增「新聞監控配置」卡片
  - 支援啟用/禁用新聞監控開關
  - 支援 NewsAPI Key 配置（密碼形式）
  - 支援 Gemini 實時搜索開關
  - 支援新聞收集間隔和 AI 分析間隔配置
  - 未配置 API Key 時顯示警告提示
- **Config 類型定義**: 前端新增 `news_monitor` 完整類型定義

### Fixed
- **NewsAPI 請求失敗**: 修復 NewsAPI 返回 HTTP 400 錯誤的問題
  - 原因：`language` 參數不支援逗號分隔的多語言格式（`zh,en`）
  - 解決：改為使用單一語言 `en` 以獲取更多結果
  - 改進：添加詳細錯誤信息輸出便於調試

## [3.25.0] - 2026-02-01

### Added
- **智能參數推薦服務**: 根據當前市場價格和波動率自動生成最優回測參數
  - 新增 `backtest/smart_params.go`：智能參數推薦核心服務
  - 根據實時價格、7/30日波動率、平均日振幅計算最優參數
  - 支援網格(grid)、動量(momentum)、均值回歸(mean_reversion)、趨勢跟蹤(trend_following)、定投(dca)、馬丁格爾(martingale)策略
  - 每個推薦包含置信度評分和詳細推薦理由
- **自動回測調度器**: 後台自動運行預計算回測，用戶進入頁面即可看到結果
  - 新增 `backtest/auto_scheduler.go`：自動回測調度核心
  - 定時（預設6小時）對配置的交易對運行回測
  - 預計算結果帶有市場分析報告和收益預測
  - 支援配置啟用/禁用、調度間隔、並行任務數等
- **前端智能推薦界面**: 回測頁面新增智能推薦區域
  - 頁面頂部展示預計算回測結果卡片（按收益率排序）
  - 顯示每個推薦的收益率、夏普比率、回撤、勝率、置信度
  - 一鍵應用推薦參數到回測表單
  - 策略選擇後可獲取基於當前市場的智能推薦
- **新增 API 端點**:
  - `GET /api/backtest/smart-params`：獲取智能參數推薦
  - `POST /api/backtest/smart-params`：獲取智能參數推薦（POST版本）
  - `GET /api/backtest/smart-params/multiple`：獲取多策略推薦
  - `GET /api/backtest/precomputed`：獲取預計算回測結果
  - `GET /api/backtest/precomputed/:symbol/:strategy`：獲取特定預計算結果
  - `POST /api/backtest/precomputed/trigger`：手動觸發預計算
  - `GET /api/backtest/scheduler/status`：獲取自動調度器狀態
- **配置項**: 新增 `auto_backtest` 配置區塊支援自動回測
  - `enabled`：是否啟用自動回測
  - `schedule_interval_hours`：調度間隔（小時）
  - `max_concurrent_tasks`：最大並行任務數
  - `default_capital`：預設回測資金
  - `symbols`：要自動回測的交易對和策略列表

## [3.24.0-rc1] - 2026-02-01

### Fixed
- **參數優化頁面超時**: 修復「開始優化」點擊後長時間等待最終 "failed to fetch" 的問題
  - 根本原因：獲取歷史 K 線數據是在 HTTP 請求處理過程中同步執行的，當下載時間過長時導致代理/瀏覽器超時
  - 解決方案：將歷史數據獲取移至後台任務異步執行，API 立即返回任務 ID
  - 新增 `loading_data` 任務狀態，前端顯示「正在從交易所下載歷史K線數據...」提示
- **TaskManager.GetResult 缺失**: 補齊 `backtest/task_manager.go` 中缺失的 GetResult 方法

## [3.24.0] - 2026-02-01

### Added
- **幣安現貨價格流**: 實現 Binance Spot WebSocket 價格流支援
  - 新增 `exchange/binance/spot_websocket.go`：現貨 WebSocket 管理器
  - 支援現貨交易對（如 PAXGUSDT）的實時價格推送
  - 使用 aggTrade 流獲取最新成交價格
  - 支援主網與測試網環境

### Fixed
- **現貨交易啟動失敗**: 修復「啟動價格流失敗（WebSocket 是唯一價格來源）：現貨價格流暫未實現」錯誤

## [3.23.0] - 2026-02-01

### Added
- **多語言翻譯優化**: 補齊並優化各語系 i18n
  - WebUI：以 en-US 為基準合併缺失鍵至所有語系，缺譯處暫以英文顯示，避免介面出現裸 key
  - 新增 `webui/scripts/merge-locales.js` 用於補齊鍵
  - 新增 `webui/scripts/zh-cn-to-zh-tw.js`：以 OpenCC 由 zh-CN 生成完整 zh-TW（無需 API）
  - 新增 `webui/scripts/translate-locales.js`：以 Gemini API 批量翻譯各語系（需 GEMINI_API_KEY，可選）
- **文檔**: 新增 `docs/I18N_LOCALES.md` 說明後端 TOML / 前端 JSON 維護流程與腳本用法

### Changed
- **後端 zh-TW.toml**: 統一繁體用詞（儲存/資料/模組/載入/登入等）
- **依賴**: webui 新增 devDependency `opencc-js` 用於簡繁轉換

## [3.22.0] - 2026-02-01

### Added
- **資料匯出功能**: 新增資料匯出頁面 `/data-export`，支援匯出各類交易資料
  - 匯出當前配置（已脫敏，不含 API 金鑰）
  - 匯出交易歷史、訂單歷史、持倉歷史
  - 匯出統計資料、對賬歷史、風控檢查歷史
  - 匯出系統監控資料、應用日誌、審計日誌
  - 全量資料匯出（ZIP 打包）
  - 支援 JSON/CSV 格式選擇和時間範圍篩選
- **側邊欄入口**: 在主導航欄新增「資料匯出」菜單項
- **多語言支援**: 新增資料匯出相關的中文簡體、中文繁體、英文翻譯

## [3.21.0] - 2026-02-01

### Added
- **策略運行狀態面板**: 新增策略運行狀態實時展示功能
  - 可查看每個策略的運行狀態（運行中/已啟用/未啟用）
  - 顯示策略資金分配：已分配、已使用、可用資金
  - 顯示策略統計：交易次數、勝率、總盈虧、交易量
  - 顯示策略持倉和訂單列表
  - 自動每 10 秒刷新數據
- **API**: 新增 `/api/strategies/runtime` 獲取所有策略運行狀態
- **API**: 新增 `/api/strategies/runtime/:id` 獲取單個策略運行狀態
- **CapitalAllocator**: 新增 `GetAllocated()` 方法獲取已分配資金

### Fixed
- **DCA 策略註冊**: 修復 `dca` 策略配置無法被註冊和執行的問題，現在支持 `dca` 和 `dca_enhanced` 兩種配置鍵
- **Systemd 配置**: 修復 `ReadWritePaths` 缺少 `config.yaml` 導致 Web UI 無法保存配置的問題

## [3.20.2] - 2026-02-01

### Fixed
- **CI 編譯**: `positionExchangeAdapter` 補齊 `GetOrderBook` 實作，滿足 `position.IExchange` 介面，修復 GitHub Actions Build (darwin-arm64) 失敗

## [3.20.1] - 2026-02-01

### Added
- **文檔**: 新增 P1/P2 進階功能指南 `docs/GRID_STRATEGY_ADVANCED_FEATURES.md`
- **文檔**: 新增現貨交易指南 `docs/SPOT_TRADING_GUIDE.md`
- **文檔**: 新增風控系統使用指南 `docs/RISK_CONTROL_GUIDE.md`
- **文檔**: 新增 API 參考 `docs/API_REFERENCE.md`
- **文檔**: 新增配置冗餘與遷移說明 `docs/CONFIGURATION_REDUNDANCY_AND_MIGRATION.md`

### Changed
- **配置**: SQLite 路徑統一：當 `database` 與 `storage` 均為 sqlite 時，以 `database.dsn` 為準，自動同步 `storage.path`，避免雙文件冗餘
- **README**: 更新核心特性與功能模組（多策略、技術指標、AI、回測、監控、事件與新聞）
- **README**: 更新模組架構圖，加入 strategy、indicators、ai、backtest、monitor、event、metrics、plugin、webui 等模組說明
- **README**: 新增功能模組概覽表與相關文檔連結

## [3.20.0] - 2026-02-01

### Added
- **P1: 資金費率與趨勢聯動**: 實現資金費率偏向策略與趨勢過濾的智能聯動功能
  - 新增 `funding_rate.trend_sync_enabled` 配置選項，預設為 true
  - 負費率 + 上漲趨勢：放寬趨勢過濾限制，允許少量買入並增加買單數量
  - 高正費率 + 下跌趨勢：強化賣出偏向，強制暫停買入
  - 提升買賣時機質量，優化網格策略效率
- **P2: 訂單簿優化掛單**: 實現基於訂單簿深度的智能掛單價格優化
  - 新增 `trading.orderbook_optimization` 配置模組，支持深度檔位數、最小深度閾值等設定
  - 自動檢測「空洞」區域（深度不足5000 USDT），微調掛單價格至有量檔位
  - 買單向下微調至ask檔位，賣單向上微調至bid檔位，提高成交機率
  - 支持優化間隔控制，降低API調用頻率
  - 微調幅度限制在price_interval的10%內，保持網格結構完整

### Changed
- **配置結構**: `FundingRateConfig` 新增 `TrendSyncEnabled` 欄位
- **配置結構**: `Trading` 新增 `OrderbookOptimization` 配置模組
- **網格策略**: `SuperPositionManager` 增強，支持費率趨勢聯動和訂單簿優化功能

### Fixed
- **P1 費率趨勢聯動**: 修復雙重放大問題，聯動時不再重複乘係數
- **P2 訂單簿優化**: 深度計算改為按前 N 檔累加（符合規格），微調方向邏輯簡化
- **P2 訂單簿優化**: 持倉恢複時賣單槽位也應用訂單簿優化

## [3.18.0] - 2026-02-01

### Added
- **每日統計盈利/止損交易量區分**: 收益統計頁面「每日統計」模組新增「盈利交易量」「止損交易量」兩欄
  - 盈利交易量：pnl>0 的交易數量總和（綠色）
  - 止損交易量：pnl<=0 的交易數量總和（紅色）
- **交易量說明 Tooltip**: 「交易量」表頭懸停提示：當日交易量少通常表示市場震盪較小，成交條件較少達成

### Changed
- **Storage**: `DailyStatisticsWithTradeCount` 新增 `VolumeProfit`、`VolumeStopLoss` 欄位
- **API**: 每日統計回應新增 `volume_profit`、`volume_stop_loss` 欄位

## [3.17.0] - 2026-02-01

### Added
- **監控每日快照** (`monitor/daily_snapshot.go`): 新增每日快照功能，便於營運與排查
- **繁體中文語系** (`i18n/locales/zh-TW.toml`): 新增繁體中文在地化檔
- **文檔 i18n**: 新增 `docs/i18n/README.en.md`、`docs/i18n/README.zh-Hans.md` 多語說明
- **交易所現貨適配器**: 補齊 Binance、Bitget、Bybit、Gate、OKX 現貨適配器與對應 wrapper（`spot_adapter.go`、`wrapper_*_spot.go`）
- **腳本** (`scripts/s2t_comments.py`): 註釋簡繁轉換輔助腳本

### Changed
- **註釋與文檔統一**: 專案內註釋統一為繁體中文或英文，符合專案規範
- **代碼與依賴**: 多處模組小幅調整與依賴更新，保持前後端版本號一致（3.17.0）

## [3.16.4] - 2026-02-01

### Fixed
- **配置備份只讀錯誤**: 修復保存配置時「read-only file system」錯誤
  - 備份目錄改為 `config.yaml` 同級的 `backups/`（不再使用 `config_backups/`）
  - 與 install.sh 的 ReadWritePaths 一致，確保 systemd 下可寫入

### Changed
- **配置備份目錄統一**: 查看、新建備份均使用 `backups/` 目錄
- **install.sh**: 創建 `backups` 目錄並設置 0776 權限，確保運行用戶可寫入

## [3.16.3] - 2026-02-01

### Added
- **概覽頁風控與交易幣種展示**: 在 Overview 頁面新增主要交易幣種列表與風控狀態
  - 全局概覽：顯示主要交易幣種（如 BTCUSDT, ETHUSDT...）、風控狀態（風控暫停交易 / 正常交易）
  - 單一幣種概覽：新增風控狀態卡片，即時顯示是否暫停或正常
  - 交易所下的幣種卡片：若該幣種風控觸發則顯示紅色標籤

## [3.16.2] - 2026-02-01

### Fixed
- **事件中心風控觸發詳情**: 風控觸發事件現在會顯示具體觸發原因
  - 市場異常：顯示「X/Y 幣種異常」及具體條件（如價格低於均線、成交量放大倍數）
  - 深度監控：顯示「深度風控觸發: 交易對 深度 X USDT」
  - 事件詳情中的 JSON 會包含 `reason` 欄位

### Improved
- **風控行為說明**: 風控觸發後會暫停買單與新開倉，待市場/深度恢復後會自動恢復交易（非永久停止）

## [3.16.1] - 2026-02-01

### Fixed
- **現貨交易代碼優化**: 修復多個現貨交易相關的技術問題
  - 修復 Binance Spot PlaceOrder 的 AvgPrice 計算錯誤
  - 修復 OKX Spot GetAccount 的 break 邏輯錯誤  
  - 修復 Gate Spot GetOrderBook 的時間戳處理問題
  - 修復 Bitget Spot 價格精度錯誤處理
  - 在 config.go 添加 MarketType 有效值驗證（"spot"/"futures"）

### Enhanced
- **現貨交易 UI 改進**: 顯著提升現貨交易的用戶體驗
  - SuperPositionManager 已完整支援現貨倉位處理（無 ReduceOnly，槓桿固定為 1）
  - 交易所工廠函數增加不支援現貨的交易所友好提示
  - SymbolManager 新增現貨/合約 Tab 分組視圖，支援按市場類型篩選
  - SymbolMultiSelect 增強市場類型視覺區分（現貨🟢綠色，合約📈紫色）
  - 新手一鍵設置功能支援選擇市場類型（現貨/合約）
  - Web API SymbolItem 新增 market_type 欄位以支援前端市場類型顯示

### Improved
- **交易所現貨支援狀態**: 清晰標示各交易所現貨支援情況
  - 支援現貨: Binance、Bitget、Bybit、Gate.io、OKX
  - 暫不支援: Huobi、KuCoin、Kraken、BitMEX、Phemex、WOO X、CoinEx、Bitrue、XT.COM、BTCC、AscendEX、Poloniex、Crypto.com
  - 選擇不支援的交易所時會顯示友好錯誤提示

## [3.16.0] - 2026-02-01

### Added
- **現貨交易支援**: 系統現已支援現貨交易，不再侷限於合約交易
  - 配置層：`SymbolConfig` 新增 `market_type` 欄位，支援 `"spot"` 或 `"futures"`（預設）
  - 交易所適配器：為 Binance、OKX、Bybit、Bitget、Gate 實現了現貨適配器
  - 倉位管理：現貨模式下自動跳過 `ReduceOnly`，槓桿固定為 1
  - Web API：交易對介面支援 `market_type` 參數區分現貨/合約
  - 前端 UI：交易對管理頁新增市場類型選擇器，可選擇現貨或合約交易
  - 範例交易對：PAXG/USDT（黃金代幣）等現貨交易對

### API
- `GET /api/exchanges/{exchange}/symbols?market_type=spot` - 獲取現貨交易對列表
- `GET /api/exchanges/{exchange}/symbols?market_type=futures` - 獲取合約交易對列表（預設）

## [3.15.0-rc1] - 2026-02-01

### Fixed
- **最大回撤計算修復**: 修復統計頁面最大回撤百分比可能超過 100% 的問題
  - 原邏輯使用累計盈虧直接計算回撤，當從小正值跌到負值時會導致回撤超過 100%（如 189%）
  - 新邏輯使用淨值（虛擬初始本金 + 累計盈虧）計算，確保回撤百分比始終在 0-100% 範圍內

## [3.15.0] - 2026-02-01

### Added
- **多資產預測與驗證**
  - 擴展新聞監控支援多資產：BTC、黃金（PAXGUSDT）
  - 黃金分析：專用 Prompt 與關鍵詞（美聯儲、美元指數、通脹、地緣政治等）
  - 價格歷史記錄：每 5 分鐘寫入 price_history 表
  - 預測驗證：prediction_verification 表，校驗預測方向正確性
  - PAXGUSDT 風控：使用黃金分析結果
  - API: `GET /api/predictions/accuracy`、`GET /api/predictions/history`
  - 前端：資產切換（BTC/黃金）、準確率統計、預測驗證歷史頁
- **參數優化器增強**
  - 交易所選擇器：支援 Binance、Bitget，先選交易所再選交易對
  - K 線週期擴展：支援 1m、5m、15m、30m、1h、4h、1d
  - 常用交易對快捷列表：BTCUSDT、ETHUSDT、BNBUSDT、SOLUSDT、PAXGUSDT、XRPUSDT、DOGEUSDT、ADAUSDT
  - 搜尋空間自動初始化：根據所選交易對當前價格自動設定價格上下限與步長（85%-95% / 105%-120%）

### API
- `GET /api/optimizer/price` - 獲取交易對當前價格
- 優化器 run 請求新增 `exchange` 參數

## [3.14.0] - 2026-02-01

### Added
- **新聞驅動風控**: 基於新聞與 Gemini 的智慧風控系統
  - NewsAPI 每 5 分鐘靜默收集，維護 2 小時新聞快取
  - Gemini 即時搜尋 + 歷史新聞彙總，每 30 分鐘分析一次
  - 預測各時間視窗（2h/4h/6h/12h/24h）價格波動機率，輸出建議（正常/謹慎/減倉/暫停交易）
  - 風控模組根據預測機率自動觸發暫停交易或減倉
  - 手動觸發分析，支援指定焦點事件（如「伊朗大爆炸」）
  - 關鍵詞可在 UI 修改，預設 40+ 條影響幣價的關鍵詞
  - 配置項：NewsAPI Key、Gemini 搜尋開關、分析間隔、風險閾值
  - 新聞分析頁面：當前分析、價格預測、已收集新聞、歷史記錄
  - Prometheus 指標：`quantmesh_news_prediction_probability`、`quantmesh_news_recommendation` 等

### API
- `GET /api/news/analysis` - 最新分析結果
- `POST /api/news/analyze` - 手動觸發分析（支援 focus_event）
- `GET /api/news/collected` - 已收集新聞
- `GET/PUT /api/news/keywords` - 關鍵詞管理
- `GET /api/news/history` - 分析歷史

## [3.13.2] - 2026-02-01

### Fixed
- **首次設定頁面版本號修復**: 修復版本號顯示錯誤
  - 之前顯示的是動態時間戳 `v2.0.{timestamp}`，導致每次打字時版本號都會變化
  - 現在改為建構時從 `package.json` 注入的正確版本號
  - 新增 `vite-env.d.ts` 類型宣告，在 `vite.config.js` 中定義 `__APP_VERSION__` 和 `__BUILD_TIME__`
- **Footer 版本號硬編碼修復**: 頁面底部版本號之前是硬編碼的 v3.8.3，現改為使用 `__APP_VERSION__` 動態讀取

## [3.13.1] - 2026-02-01

### Fixed
- **CGO 編譯修復**: CI/CD 建構時正確啟用 CGO 並安裝 SQLite 依賴
  - 修復 Linux 建構時 `go-sqlite3` 因 CGO 未啟用導致的執行時錯誤
  - 安裝 `build-essential` 和 `libsqlite3-dev` 確保 SQLite 正確連結
  - 解決「密碼管理器未初始化」問題

## [3.13.0] - 2026-02-01

### Added
- **盈利趨勢曲線圖增強**: 盈利管理頁面圖表升級
  - 柱狀圖改為曲線圖，使用 recharts 雙軸展示
  - 新增幣種選擇器，支援選擇交易對
  - 選擇幣種後可疊加顯示該幣種每日價格變化（收盤價 - 開盤價）
  - 雙 Y 軸：左軸盈利金額，右軸價格變化，便於對比盈利與市場表現

## [3.12.0] - 2026-01-31

### Added
- **利潤自動提取定時任務**: 新增利潤提取執行器
  - Immediate 任務：每 5 分鐘檢查規則條件
  - Daily 任務：每天凌晨 2 點執行
  - Weekly 任務：每週一凌晨 2 點執行
  - 根據已實現盈利與規則觸發金額/比例執行內部轉帳
- **交易所內部轉帳**: 交易所介面新增 `InternalTransfer` 方法（幣安已實現，期貨帳戶轉現貨）
- **提取記錄儲存**: 新增 `profit_withdraw_records` 表及儲存介面
  - 支援儲存/更新提取記錄狀態、按帳戶查詢提取歷史
- **手動提取完善**: 手動提取 API 呼叫交易所內部轉帳並持久化記錄
- **提取歷史查詢 API**: `GET /api/profit/history` 回傳真實提取記錄（支援 `limit` 參數）

### Changed
- 提取規則表新增 `last_triggered_at` 欄位，用於 daily/weekly 頻率防重複執行

## [3.11.3] - 2026-01-31

### Changed
- **CI/CD 簡化**: 移除伺服器部署步驟，改用 GitHub Container Registry (ghcr.io) 發布 Docker 映像
- Docker 映像現在發布到 `ghcr.io/ghostsworm/quantmesh`

## [3.11.2] - 2026-01-31

### Fixed
- **CI/CD 修復**: 重構 workflow 架構，先建構所有平台產物再統一建立 release，徹底解決 immutable release 問題

## [3.11.1] - 2026-01-31

### Fixed
- **CI/CD 修復**: 升級 `softprops/action-gh-release` 到 v2，修復並行建構時 release asset 上傳失敗的問題

## [3.11.0] - 2026-01-31

### Added
- **資料匯出功能**: 新增完善的資料匯出策略
  - 配置檔案匯出：下載當前配置與歷史版本（API Key 等敏感資訊自動脫敏）
  - 交易資料匯出：訂單歷史、交易歷史、持倉歷史（支援 CSV/JSON）
  - 統計與對帳：每日統計、對帳歷史、風控檢查歷史
  - 系統資料：系統監控指標匯出
  - 日誌匯出：應用日誌、合規審計交易日誌
  - 一鍵全量匯出：ZIP 打包所有資料
  - 匯出 API 支援時間範圍、交易所、交易對等過濾參數

### Added (API)
- `GET /api/export/config` - 下載當前配置（脫敏）
- `GET /api/export/config/history/:version` - 下載歷史配置
- `GET /api/export/trades` - 匯出交易歷史
- `GET /api/export/orders` - 匯出訂單歷史
- `GET /api/export/positions` - 匯出持倉歷史
- `GET /api/export/statistics` - 匯出統計資料
- `GET /api/export/reconciliation` - 匯出對帳歷史
- `GET /api/export/risk-checks` - 匯出風控檢查歷史
- `GET /api/export/system-metrics` - 匯出系統監控
- `GET /api/export/logs` - 匯出應用日誌
- `GET /api/export/audit-logs` - 匯出審計日誌（ZIP）
- `GET /api/export/all` - 全量資料匯出（ZIP）

## [3.10.0] - 2026-01-31

### Added
- **每日統計增強**: 新增市場漲跌幅與最大回撤指標
  - 每日統計表格新增「開盤/收盤」列，顯示當日標的開盤價與收盤價
  - 每日統計表格新增「漲跌幅」列，顯示當日價格變化百分比
  - 每日統計表格新增「累計盈虧」列，顯示累計盈虧金額
  - 關鍵指標卡片新增「最大回撤」指標，顯示回撤百分比與金額
  - 回撤超過 10% 時自動高亮警示

### Changed
- 統計卡片版面調整為更緊湊的 5 列展示

## [3.9.0] - 2026-01-31

### Added
- **線上 YAML 配置編輯器**: 新增完整的線上配置編輯功能
  - 使用 Monaco Editor 提供語法高亮與程式碼摺疊
  - 即時 YAML 語法錯誤提示
  - 儲存前 Diff 預覽確認，支援並排對比與統一視圖
  - 使用 react-diff-viewer 渲染差異

- **配置歷史版本管理**: 新增配置版本控制功能
  - 歷史版本儲存到資料庫（SQLite），永久保存
  - 本機磁碟備份保留最近 10 個版本
  - 支援檢視任意歷史版本的完整內容
  - 支援任意兩個版本之間的 Diff 對比（包含與當前版本對比）
  - 一鍵還原到指定歷史版本，還原前自動備份當前配置
  - 配置修改後自動記錄歷史版本

- **後端 API**: 新增配置歷史相關介面
  - `GET /api/config/history` - 獲取歷史版本列表
  - `GET /api/config/history/:version` - 獲取指定版本內容
  - `POST /api/config/history/:version/restore` - 還原到指定版本
  - `POST /api/config/history/diff` - 對比任意兩個版本
  - `POST /api/config/validate-yaml` - 驗證 YAML 配置
  - `POST /api/config/update-yaml` - 更新 YAML 配置

### Changed
- **配置備份數量**: 本機磁碟備份從 50 個減少到 10 個
- **配置頁面**: 全域視圖新增「YAML 編輯器」與「歷史版本」兩個 Tab

## [3.8.3] - 2026-01-31

### Added
- **安裝腳本**: 新增 `install.sh` 自動安裝腳本，支援一鍵部署
  - 自動安裝二進位檔到 `/opt/quantmesh`
  - 自動設定 systemd 服務
  - 智慧處理配置檔案（保留/覆蓋/備份）
  - 支援從舊路徑遷移資料（歷史誤拼目錄名 `quntmesh` 亦會處理）
  - 建立 quantmesh 使用者並設定安全權限

### Changed
- **Release 打包優化**: GitHub Actions 建構的 Linux 發行包現在包含完整的安裝工具
  - 包含 `install.sh` 安裝腳本
  - 包含 `quantmesh.service` systemd 服務檔
  - 包含 `backup.sh` 與 `restore.sh` 輔助腳本
  - 解壓後可直接執行 `sudo ./install.sh` 完成安裝

### Fixed
- **版本號同步**: 統一前後端版本號

## [3.8.2] - 2026-01-31

### Fixed
- **交易對配置自動儲存**: 修復新增/編輯/刪除交易對後重新整理頁面配置遺失的問題
  - 之前 `SymbolManager` 元件的 `onUpdate` 回呼只更新了本機 React 狀態，未提交到後端
  - 現在交易對變更後會自動呼叫 `updateConfig` API 儲存到後端
  - 新增儲存成功/失敗的 Toast 提示

## [3.4.5] - 2026-01-24

### Improved
- **概覽頁預設展開交易所**: 概覽頁面預設展開所有交易所下的交易對列表，無需手動點擊
  - 使用 `defaultIndex` 屬性自動展開所有 Accordion 項目
  - 優化使用者體驗，快速檢視所有交易對狀態

## [3.4.4] - 2025-01-01

### Fixed
- **CI 建構修復**: 修復 GitHub Actions 建構失敗問題
  - 確保 `web/dist` 目錄至少有一個檔案，避免 `go:embed dist/*` 報錯
  - 改進前端建構步驟，新增更好的錯誤處理與降級方案
  - 當前端建構失敗時，建立佔位檔案以確保 Go 建構可以繼續

## [3.4.3] - 2025-01-01

### Added
- **內建非同步 AI 任務系統**: 整合完整的非同步 AI 呼叫能力，無需依賴外部 `go-gemini-proxy` 服務
  - 新增 `AsyncTask` 模型支援非同步任務持久化（支援 SQLite/PostgreSQL/MySQL）
  - 新增 `TaskService` 提供任務 CRUD 操作
  - 新增 `AIService` 封裝 Gemini API 直接呼叫
  - 新增 `TaskProcessor` 背景任務處理器，支援並行控制與重試機制
- **多交易對管理與資金分配增強**: 完善交易對管理與資金分配系統
  - 支援批次新增常用交易對（BTC/ETH/SOL 等）
  - 策略資金配置持久化，支援最大資金限額、佔比、儲備金比例等
  - 即時資金分配視圖，顯示各策略的已分配/已使用/可用資金
  - 交易對啟用/停用控制
- **交易所盈虧診斷 API**: 新增 `/api/exchange/profit-diagnosis` 介面，提供交易所盈虧分析
- **精度調整優化**: Binance 適配器支援 TickSize/StepSize，自動從交易所獲取最小變動單位

### Changed
- **統一 AI 存取方式**: 移除 `native`/`proxy` 存取模式選擇，統一使用內建非同步系統
  - `GeminiClient` 重構為 `AsyncGeminiClient`，內部自動處理任務建立與輪詢
  - 配置檔案移除 `access_mode` 與 `proxy` 相關配置項
  - 前端移除 AI 存取方式選擇 UI 與代理配置表單
- **簡化 API 介面**: `/api/ai/generate-config` 介面移除存取模式與代理相關參數
- **網格參數優化**: 修復網格參數價格間隔單位，從百分比改為 USDT 絕對值
- **專案檔案結構優化**: 整理專案檔案結構，移動測試檔、腳本與文件到專屬目錄
- **README 優化**: 新增徽章、效能指標與對比表

### Fixed
- **價差監控修復**: 修復價差監控中所有幣種使用同一個合約價格的問題
  - 修改 13 個交易所適配器的 `GetLatestPrice` 方法簽名，新增 symbol 參數
  - 現在能正確顯示每個幣種（SOL/ETH/BNB/BTC）的實際合約價格
- **首頁 Dashboard 修復**: 
  - 資料口徑統一：P&L/成交量改為讀取 `/api/statistics` 累計資料
  - 修復 uptime=0 時 trades/hour 計算導致的 NaN 顯示
  - 新增價格回退機制，當 status.current_price 不可用時使用 positionsSummary 中的價格
- **國際化完善**: 修復首頁 Dashboard 的國際化問題，補全 zh-CN/zh-TW 翻譯
- **對帳校驗頁面修復**: 修復預計盈利曲線鋸齒跳動問題

### Security
- **API Token 保護**: AI 呼叫完全在本機完成，敏感的 API Key 不再傳送到外部服務

### Removed
- 移除對外部 `gemini.facev.app` 代理服務的依賴
- 移除 `ProxyGeminiClient` 與 `NativeGeminiClient` 雙模式實作
- 移除配置中的 `ai.access_mode`、`ai.proxy.base_url`、`ai.proxy.username`、`ai.proxy.password` 欄位
