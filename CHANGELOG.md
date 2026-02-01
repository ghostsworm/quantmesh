# Changelog

所有重要的專案更新都會記錄在此檔案中。

## [Unreleased]

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
