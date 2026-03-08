# Changelog

所有重要的專案更新都會記錄在此檔案中。

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
  - 新增 config-mysql8-example.yaml 配置示例

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
  - 后端：`monitor/kline_collector.go`、`web/api_kline_files.go`、`storage/sqlite.go`（新增 `protected_kline_files` 表）
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
  - 涉及：`storage/sqlite.go`（`QueryDailyStatisticsByExchange` 使用時區偏移）、`utils/timezone.go`（新增 `GetTimezoneOffsetSeconds`）

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
  - 支援從舊路徑 `/root/quntmesh` 遷移資料
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
