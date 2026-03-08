# 更新日志 (Changelog)

本文档记录 OpenSQT Market Maker 项目的所有重要功能变更、算法调整和版本更新。

## 版本规范

- 每次功能变更或算法调整都会打一个 Git Tag
- 版本号格式：`v{major}.{minor}.{patch}` (例如：v1.0.0)
- 每个版本记录包含：版本号、Git Tag、更新时间、变更内容

---

## v3.65.0-rc7 - 2026年03月09日

**Git Tag**: `v3.65.0-rc7`

### 修復 (Fixed)

- **做空网格回测零平仓 bug**：修復 `buildGridLevelsBySpacingFromHigh` 配合 `maxCount` 限制時，20 個檔位全部集中在自動推導的極端高價區（priceHigh 可能是一次性尖峰），導致正常交易價格遠低於所有檔位 → 開倉後永遠觸不到檔位來平倉。改用 `buildGridLevelsByCenteredSpacing` 以首根 K 線收盤價為中心向兩側展開檔位，確保覆蓋正常交易區間
- **grid_count 默認值過度限制**：當用戶指定了 `grid_spacing` 但未指定 `grid_count` 時，原默認 20 檔會截斷大範圍為極小子集；改為 `grid_spacing` 存在且 `grid_count` 未指定時默認不限制檔位數（0=全區間）

---

## v3.65.0-rc5 - 2026年03月09日

**Git Tag**: `v3.65.0-rc5`

### 修復 (Fixed)

- **配置保存類型錯誤**：修復前端 DecimalNumberInput 返回字符串（如 `"70.000000"`）導致後端 YAML 反序列化 float64 字段失敗的問題；後端 `bindConfigFromJSONMap` 增加遞歸數值規範化防禦，前端 `normalizeConfigForSave` 擴展為遞歸處理所有數值字段

---

## v3.65.0-rc1 - 2026年03月09日

**Git Tag**: `v3.65.0-rc1`

### 新增 (Added)

- **回測報告方向感知術語**：根據 `direction` 參數（LONG/SHORT/BOTH）自動切換報告中的「買入/賣出」與「開倉/平倉」；單向做多時顯示開倉/平倉，單向做空時賣出=開倉、買入=平倉，雙向時保持買入/賣出；成對交易表頭與交易明細類型列同步更新

### 變更 (Changed)

- **回測報告參數合併**：若任務 params 無 `direction` 但策略配置有，從首個策略合併到報告參數，確保開倉/平倉標籤正確顯示

---

## v3.56.0-rc1 - 2026年03月01日

**Git Tag**: `v3.56.0-rc1`

### 變更 (Changed)

- **资金管理重构**：资金管理页面改为主打「查看」的仪表盘，展示各交易所总余额、可用资金、未实现盈亏；每个交易所下按 Bot 列出委托资金（挂单占用）、持仓占用、合计占用及占比；新增 API `GET /api/capital/usage`
- **SuperPositionManager**：新增 `GetPendingBuyOrderValueUSDT()` 方法，用于统计挂单买单占用的资金

---

## v3.43.0-rc1 - 2026年02月06日

**Git Tag**: `v3.43.0-rc1`

### 新增 (Added)

- **複合風控引擎（Composite Risk Controller）**：五個風控因子（AI 新聞、均線趨勢、資金費率、市場深度、K 線異常）加權聚合，輸出複合評分與級別（normal/caution/reduce_position/pause_buying/stop_trading），並在持倉管理中根據級別調整買入或撤單；配置塊 `composite_risk`、API `GET /api/composite-risk`

---

## v3.42.0-rc1 - 2026年02月06日

**Git Tag**: `v3.42.0-rc1`

### 修复 (Fixed)

- **CI/CD 前端构建失败**：GitHub Actions 和 Dockerfile 中前端构建从 npm 迁移到 yarn (Yarn 4 Berry)，添加 `setup-node` 和 `corepack enable` 步骤
- **posthog-js 版本错误**：修正 `posthog-js` 依赖版本从不存在的 `^2.0.0` 改为 `^1.341.0`
- **pnpm overrides 改为 yarn resolutions**：`package.json` 中 `pnpm.overrides` 替换为 `resolutions`
- **yarn.lock 纳入版本控制**：从 `.gitignore` 中移除 `webui/yarn.lock`，确保 CI 可用 `--immutable` 安装

---

## v3.29.0 - 2026年02月03日

**Git Tag**: `v3.29.0`

### 新增 (Added)

- **回測報告保存為圖片**：回測結果彈窗新增「保存為圖片」按鈕，可將完整報告（含 K 線走勢圖表）導出為 PNG 高清圖片下載
- **回測風控參數可配置**：網格策略回測時支持在開始回測前指定風控模擬器參數（帶默認值，可微調）
- **帶風控回測對比**：網格策略回測時自動執行兩次（無風控 + 有風控），在同一份報告中呈現對比數據
- **期末持倉**：報告與彈窗新增「期末持倉（幣的數量）」「期末持倉市值」顯示
- **交易指標**：新增「買入次數」「賣出次數」顯示
- **回測報告**：新增成對交易（前50筆）、未成對交易（前50筆）明細表
- **K 線走勢圖**：回測報告彈窗內新增期間 K 線收盤價走勢圖，拆為 4 段折線圖便於查看
- **當前持倉按交易所、币种、策略列出**：概覽頁「當前持倉」現支持按交易所、币种、策略維度展示
- **服務狀態頁**：新增服務狀態監控頁面

### 變更 (Changed)

- **回測報告彈窗展示**：點擊「查看」時改為以 Dialog（Modal）形式展示回測結果，便於專注閱讀；支持下載報告
- **回測報告小數格式統一**：統計值和專用指標改為小數點後 4 位

### 修復 (Fixed)

- **K 線走勢圖不顯示**：修復回測報告彈窗中「期間 K 線走勢」四段圖表框架存在但折線不渲染的問題
- **回測報告首次加載卡住**：修復第一次點擊新產生的回測報告時一直顯示「載入中」的問題
- **回測報告 Markdown 渲染**：報告內容現正確渲染 Markdown
- **回測任務列表高亮**：任務列表中當前查看的任務行增加背景高亮
- **回測無需 Binance API 配置**：回測獲取歷史 K 線時，若未配置 Binance API，自動使用公開數據適配器
- **K 線圖異常插針**：新增通用 `exchange.ClipKlineSpikes`，在所有交易所的 K 線出口統一裁剪插針

---

## v3.28.11-rc5 - 2026年02月03日

**Git Tag**: `v3.28.11-rc5`

### 新增 (Added)

- **期末持倉**：報告與彈窗新增「期末持倉（幣的數量）」「期末持倉市值」顯示

---

## v3.28.11-rc4 - 2026年02月03日

**Git Tag**: `v3.28.11-rc4`

### 新增 (Added)

- **交易指標**：新增「買入次數」「賣出次數」顯示
- **回測報告**：新增成對交易（前50筆）、未成對交易（前50筆）明細表
- **K 線走勢圖**：回測報告彈窗內新增期間 K 線收盤價走勢圖，拆為 4 段折線圖

---

## v3.28.11-rc3 - 2026年02月03日

**Git Tag**: `v3.28.11-rc3`

### 变更 (Changed)

#### 回测报告弹窗展示

- **功能描述**: 点击「查看」时改为以 Dialog（Modal）形式展示回测结果，便于专注阅读。
- **影响范围**: `webui/src/components/BacktestMenu.tsx`

---

## v3.28.11-rc2 - 2026年02月03日

**Git Tag**: `v3.28.11-rc2`

### 修复 (Fixed)

#### 回测报告 Markdown 渲染

- **问题描述**: 回测任务列表点击「查看」时，报告以原始 Markdown 文本显示，标题、表格等未渲染。
- **解决方案**: 使用 react-markdown、remark-gfm 对报告内容进行渲染。
- **影响范围**: `webui/src/components/BacktestMenu.tsx`

---

## v3.28.11-rc1 - 2026年02月03日

**Git Tag**: `v3.28.11-rc1`

### 修复 (Fixed)

#### 回测任务列表高亮

- **问题描述**: 任务列表中无法直观看出当前查看的是哪条任务的详情。
- **解决方案**: 为当前选中的任务行增加背景高亮（浅蓝/深色模式适配）。
- **影响范围**: `webui/src/components/BacktestMenu.tsx`

#### 回测无需 Binance API 配置

- **问题描述**: 未配置 Binance API 时回测失败，报错「获取历史数据失败: 创建 Binance adapter 失败: Binance API 配置不完整」。
- **解决方案**: 新增 `NewBinanceAdapterForPublicData`，在 API 为空时使用占位密钥；Binance K 线为公开接口，无需认证即可拉取历史数据。
- **影响范围**: `exchange/binance/adapter.go`、`backtest/data_fetcher.go`

#### 回测使用已配置的 Binance API

- **问题描述**: 已配置 demo/正式 Binance API，但回测仍报「Binance API 配置不完整」，因 TaskManager 创建时使用了硬编码的空配置。
- **解决方案**: 新增 `buildBinanceConfigForBacktest`，从 cfg 中读取 Binance 配置并传入 TaskManager；两处 TaskManager 创建均改用实际配置。
- **影响范围**: `main.go`

---

## v3.28.11 - 2026年02月03日

**Git Tag**: `v3.28.11`

### 新增 (Added)

#### 當前持倉按交易所、币种、策略列出

- **功能描述**: 概覽頁「當前持倉」現支持按交易所、币种、策略維度展示。
- **單幣種概覽**: 持倉卡片標題顯示 交易所 · 币种 · 策略（如 BINANCE · BTCUSDT · 网格）。
- **全局概覽**: 新增「當前持倉」表格，列出所有有持倉的交易對，按交易所、币种、策略分列，可點擊行切換到該幣種詳情。
- **新增 API**: `GET /api/positions/summary/all` 獲取所有交易對持倉彙總。
- **影響範圍**: `web/api.go`、`web/server.go`、`webui/src/components/Dashboard.tsx`、`webui/src/components/GlobalDashboard.tsx`、`webui/src/services/api.ts`、i18n

---

## v3.28.10-rc2 - 2026年02月03日

**Git Tag**: `v3.28.10-rc2`

### 修复 (Fixed)

#### 回测参数重复

- **问题描述**: 回测页策略参数中包含 `total_capital`，同时又有固定的「总投入资金」输入框，导致重复显示。
- **解决方案**: 回测页渲染策略参数时过滤 `total_capital`，只保留底部的总投入资金输入。
- **影响范围**: `webui/src/components/BacktestMenu.tsx`

#### 回测参数提示

- **问题描述**: 用户不清楚网格策略的「价格上限/下限」应如何填写。
- **解决方案**: 为「价格上限/下限」新增填写提示，解释参考区间和压力/支撑位。
- **影响范围**: `backtest/strategy_params.go`、`webui/src/components/BacktestMenu.tsx`

#### 回测参数校验提示

- **问题描述**: 用户误以为价格上/下限填 0 可表示不设限，实际会导致回测失败。
- **解决方案**: 在提示中明确上/下限必须大于 0，且上限需大于下限。
- **影响范围**: `backtest/strategy_params.go`

---

## v3.28.8 - 2026年02月02日

**Git Tag**: `v3.28.8`

### 功能 (Added)

#### 事件中心：補齊 Storage 寫入的舊事件顯示

- **問題描述**: 由 Storage 寫入的事件（僅 `event_type`、`data`）在 Web 事件詳情中顯示為空（事件來源、事件類型、事件標題、事件消息均為空）。
- **解決方案**: 讀取事件時，若 `type`/`source`/`title`/`message` 為空但 `event_type`、`data` 有值，則從 `event_type` 推導 severity/source/title，並從 `data` JSON 構建 message 再返回前端。
- **影響範圍**: `database/interface.go`（EventRecord 新增 EventTypeRaw/DataRaw）、`event/enrich.go`（BuildMessageFromData）、`web/api_events.go`（enrichEventRecord）

---

## v3.28.6 - 2026年02月02日

**Git Tag**: `v3.28.6`

### 修復 (Fixed)

#### PWA / Service Worker 與 API 延遲

- **問題描述**: 登入頁或首屏載入時 `/api/version` 等請求經 Service Worker 攔截後變慢（約 3 秒），Chrome 有時一直載入；內嵌瀏覽器可正常打開。
- **根本原因**: SW 攔截請求會多一跳與 JS 執行；SW 冷啟動時需載入 sw.js 與 workbox，耗時可達數秒；另存在雙重註冊導致衝突。
- **解決方案**:
  - 不再為 `/api`、`/ws` 註冊 runtimeCaching，請求直接走瀏覽器網絡
  - 移除 main.tsx 中手動 Service Worker 註冊，僅保留 vite-plugin-pwa 自動註冊
  - 在 vite.config.js 中補充註釋說明不讓 SW 攔截 API 的原因
- **影響範圍**: `webui/vite.config.js`、`webui/src/main.tsx`

本版亦包含 3.28.5-rc1～rc5 的修復（新聞分析格式、新聞收集顯示、登入頁重複請求、策略配比 Hooks、收益日曆今天焦點、DCA 資金釋放等）。

---

## v3.28.1 - 2026年02月02日

**Git Tag**: `v3.28.1`

### 修復 (Fixed)

#### logs.db 路徑調整

- **問題描述**: systemd 配置 `ProtectSystem=strict` + `ReadWritePaths` 時，`logs.db` 位於根目錄導致無法寫入，報錯 "attempt to write a readonly database"
- **解決方案**: 將 `logs.db` 移至 `./data/logs.db`，與其他數據庫文件（quantmesh.db, auth.db, webauthn.db）保持一致
- **影響範圍**:
  - `main.go`: 預設路徑改為 `./data/logs.db`
  - `tools/diagnose_events.go`: 診斷工具路徑更新
  - `scripts/backup.sh`, `scripts/restore.sh`, `scripts/reset_data.sh`, `scripts/deploy.sh`: 備份恢復腳本路徑更新
  - 文檔更新：`docs/BACKUP_RECOVERY.md`, `rdocs/部署指南.md`

#### Binance 現貨測試網 WebSocket 端點修正

- **問題描述**: 現貨測試網 WebSocket 連接失敗，報錯 "websocket: bad handshake"
- **根本原因**: 測試網 WebSocket 端點使用了錯誤的域名 `testnet.binance.vision`
- **解決方案**: 修正為正確的測試網 WebSocket 端點 `stream.testnet.binance.vision`

**升級注意事項**:
- 升級後若舊版 `./logs.db` 存在，需手動遷移：`mv ./logs.db ./data/logs.db`
- systemd 服務的 `ReadWritePaths` 不再需要單獨指定 `logs.db`，只需包含 `/opt/quantmesh/data` 即可

---

## v3.24.0-rc1 - 2026年02月01日

**Git Tag**: `v3.24.0-rc1`

### 修復 (Fixed)

#### 參數優化頁面超時問題

- **問題描述**: 點擊「開始優化」後長時間等待，最終提示 "failed to fetch"
- **根本原因**: 獲取歷史 K 線數據在 HTTP 請求處理過程中同步執行，當下載時間過長時導致代理/瀏覽器超時
- **解決方案**:
  - 將歷史數據獲取移至後台任務異步執行
  - API 立即返回任務 ID，不再阻塞等待數據下載
  - 新增 `loading_data` 任務狀態
  - 前端顯示「正在從交易所下載歷史K線數據...」提示

#### TaskManager.GetResult 方法缺失

- 補齊 `backtest/task_manager.go` 中缺失的 `GetResult` 方法，修復編譯錯誤

---

## v3.24.0 - 2026年02月01日

**Git Tag**: `v3.24.0`

### 新增 (Added)

#### 幣安現貨 WebSocket 價格流支援

- **現貨價格流實現**: 為 Binance Spot 適配器實現 WebSocket 價格流
  - 新增 `exchange/binance/spot_websocket.go`：現貨 WebSocket 管理器
  - 使用 aggTrade 流獲取實時成交價格
  - 支援主網（stream.binance.com）與測試網（testnet.binance.vision）
  - 現貨交易對（如 PAXGUSDT）現可正常啟動交易

### 修復 (Fixed)

- **現貨交易啟動失敗**: 修復「啟動價格流失敗（WebSocket 是唯一價格來源）：現貨價格流暫未實現」錯誤

---

## v3.22.1 - 2026年02月01日

**Git Tag**: `v3.22.1`

### 修復 (Fixed)

#### 運行日誌為空問題排查與改進

- **日誌存儲診斷**: 當批量寫入日誌失敗時輸出錯誤至標準錯誤，便於排查運行日誌頁面無數據問題
- **日誌寫入 panic 可視化**: logger 寫入 SQLite 時若發生 panic 會輸出至標準錯誤，不再靜默忽略
- **日誌 channel 滿時告警**: 日誌隊列滿丟棄消息時輸出警告，避免無聲丟失
- **日誌處理協程生命週期日誌**: processLogs 啟動/退出時輸出 DEBUG 日誌，便於確認協程是否正常運行
- **診斷工具增強**: `tools/diagnose_events.go` 新增對 `logs.db`（運行日誌數據庫）的檢查，可同時診斷運行日誌與事件中心兩套數據

---

## v3.22.0 - 2026年02月01日

**Git Tag**: `v3.22.0`

### 新增 (Added)

#### 資料匯出功能

- **資料匯出頁面**: 新增 `/data-export` 頁面，支援匯出各類交易資料
- **匯出類型**:
  - 當前配置（已脫敏，不含 API 金鑰）
  - 交易歷史、訂單歷史、持倉歷史
  - 統計資料、對賬歷史、風控檢查歷史
  - 系統監控資料、應用日誌、審計日誌
  - 全量資料匯出（ZIP 打包）
- **匯出選項**: 支援 JSON/CSV 格式選擇和時間範圍篩選
- **側邊欄入口**: 在主導航欄新增「資料匯出」菜單項
- **多語言支援**: 新增資料匯出相關的中文簡體、中文繁體、英文翻譯

---

## v3.21.0 - 2026年02月01日

**Git Tag**: `v3.21.0`

### 新增 (Added)

#### 回測介面優化 - 三級聯動選擇與參數預填

- **交易所選擇**: 回測頁面新增交易所選擇器，支持 Binance、Bitget、OKX、Bybit、Gate 等交易所
- **市場類型選擇**: 根據交易所動態顯示支援的市場類型（現貨/合約）
- **交易對選擇**: 根據交易所和市場類型過濾並顯示可用交易對
  - 已配置的交易對會優先顯示並標記「(已配置)」
  - 預置常用交易對列表（BTCUSDT、ETHUSDT、SOLUSDT 等）
- **策略參數預填**: 選擇策略後自動從 config 中讀取已配置的參數進行預填
  - 支持網格策略的價格間隔、單筆金額、窗口大小等參數
  - 支持 DCA、馬丁格爾等策略的相關參數
  - 自動設置總資金為配置中的分配資金

#### 後端 API 新增

- `GET /api/backtest/exchanges` - 獲取支援回測的交易所列表及市場類型
- `GET /api/backtest/symbols` - 根據交易所和市場類型獲取交易對列表
- `GET /api/backtest/config-params` - 獲取已配置交易對的策略參數（用於預填）

---

## v3.20.3-rc3 - 2026年02月01日

**Git Tag**: `v3.20.3-rc3`

### 修復 (Fixed)

#### 安裝配置錯誤修復
- 修復 systemd 服務文件中官網 URL 錯誤：將 `quantmesh.com` 更正為 `quantmesh.io`
- 確認安裝腳本正確創建 quantmesh 用戶並設置 `/opt/quantmesh` 目錄權限

---

## v3.20.3 - 2026年02月01日

**Git Tag**: `v3.20.3`

### 修复 (Fixed)

#### WebAuthn 指紋登錄 - RPID 配置錯誤修复

- **問題**: WebAuthn 指紋注册時出現 "The relying party ID is not a registrable domain suffix" 錯誤
- **原因**: RPID 硬編碼為 `localhost`，與實際訪問域名不匹配
- **修复**: 
  - 重構 RPID 配置邏輯，支持動態配置
  - 優先級：環境變數 `DOMAIN` → 配置文件 `web.domain` → `web.host` → `localhost`
  - 自動检测 HTTPS 並生成正确的 Origin
  - 新增 TLS 配置支持（`web.tls.enabled/cert_file/key_file`）
- **相關文件**: `main.go`、`config/config.go`、`config.example.yaml`

#### 工具與文档

- **新增**: WebAuthn 診斷工具 (`tools/webauthn_diagnose.go`)
- **新增**: 快速修复脚本 (`scripts/fix_webauthn_rpid.sh`)  
- **新增**: 修复指南文档 (`docs/WEBAUTHN_RPID_FIX.md`)
- **新增**: 針對性配置示例 (`config.webauthn-fix.yaml`)

---

## v3.19.1 - 2026年02月01日

**Git Tag**: `v3.19.1`

### 新增 (Added)

#### 配置管理 - 動態調整 UI

- **單筆金額動態調整配置**：在配置管理 → 交易參數頁籤新增「動態調整」區塊，可透過 UI 管理：
  - 啟用動態調整總開關
  - 啟用單筆金額動態調整
  - 單筆金額上下限 (min/max)
  - 頻率閾值 (次/分鐘)
  - 調整步長
  - 檢查間隔 (秒)
  - 涉及文件：`webui/src/components/Configuration.tsx`、`webui/src/i18n/locales/*.json`、`webui/src/services/config.ts`

---

## v3.19.0 - 2026年02月01日

**Git Tag**: `v3.19.0`

### 新增 (Added)

#### 網格策略 - 單筆金額動態調整 (P0)

- **OrderQuantity 動態調整**：
  - 根據過去 1 分鐘內的成交頻率，自動調整每筆訂單金額
  - 交易過於頻繁時降低單筆金額，減少手續費摩擦
  - 交易過少時適當提高單筆金額，提高資金利用
  - 配置項：`trading.dynamic_adjustment.order_quantity`（enabled, min, max, frequency_threshold, adjustment_step, check_interval）
  - 涉及文件：`strategy/dynamic_adjuster.go`、`position/super_position_manager.go`、`config/config.go`

#### 文檔

- **P1/P2 規格詳情**：新增 `docs/GRID_ALPHA_P1_P2_SPEC.md`，定義資金費率與趨勢聯動（P1）、訂單簿優化掛單（P2）的實現規格

---

## v3.18.1 - 2026年02月01日

**Git Tag**: `v3.18.1`

### 修復 (Fixed)
- **價差監控現貨-合約價差模塊非 BTCUSDT 合約價錯誤**：
  - 問題：價差監控頁面中，僅 BTCUSDT 的現貨/合約價格正確，ETHUSDT、BNBUSDT、SOLUSDT 等交易對的合約價格均顯示為 BTC 的合約價（約 $78,802），導致價差百分比異常
  - 原因：Binance 適配器 `GetLatestPrice(ctx, symbol)` 未使用參數 `symbol`，僅從單一 WebSocket 訂閱（firstRuntime 的 BTCUSDT）緩存讀取，導致所有交易對的合約價都返回同一值
  - 修復：當請求的 symbol 與適配器訂閱的 b.symbol 不同或 WebSocket 無數據時，改為通過 REST `GET /fapi/v1/premiumIndex` 獲取該 symbol 的標記價格，確保每個交易對顯示對應的合約價格
  - 涉及文件：`exchange/binance/adapter.go`

---

## v3.8.1 - 2026年01月31日

**Git Tag**: `v3.8.1`

### 新增 (Added)

#### 前端 - 策略配比页面增强
- **策略配比页面可编辑功能** (`StrategyAllocation.tsx`)：
  - 将原本只读的策略配比页面改造为可编辑页面
  - 支持通过滑块或输入框直接调整各策略的资金权重比例
  - 实时显示调整后的预估分配金额
  - 支持按交易所独立配置策略比例（每个交易所可设置不同的策略权重）
  - 添加"保存更改"和"取消"按钮，用户可预览更改后再提交
  - 添加"资金再平衡"功能按钮，支持一键按权重重新分配资金

- **用户提示增强**：
  - 在页面顶部添加醒目提示：不同交易所/币种可以配置不同的策略比例
  - 示例说明：在 Binance 可配置 70% 网格 + 30% 马丁格尔，在 OKX 可配置 50% 网格 + 50% DCA
  - 在"全部交易所"视图下显示只读提示，引导用户选择具体交易所进行调整
  - 显示总权重状态，当权重不等于100%时提示用户建议调整

- **UI/UX 优化**：
  - 每个策略卡片显示：策略名称、类型、状态、当前权重、已使用资金、使用率
  - 支持百分比模式切换
  - 切换交易所时自动清空待保存的更改，避免误操作
  - 资金分配比例可视化展示

---

## v3.4.2 - 2026年01月14日

**Git Tag**: `v3.4.2`

### 修复 (Fixed)
- **修复启动交易对时找不到配置的问题**：
  - 问题：当通过 Web 界面添加新的交易对配置后，启动交易对时系统报错"未找到交易对配置"，即使配置文件中已存在该交易对
  - 原因：`symbolManagerWebAdapter` 在创建时保存了配置的引用，当配置文件更新后，`StartSymbol` 方法仍使用启动时的旧配置查找交易对，导致找不到新添加的交易对
  - 解决方案：
    - 在 `web/api_config.go` 中新增 `GetLatestConfig()` 函数，用于获取最新配置
    - 修改 `main.go` 中的 `StartSymbol` 方法，从配置管理器获取最新配置，而不是使用启动时的配置
    - 如果获取最新配置失败，回退到使用启动时的配置（向后兼容）
  - 技术细节：
    - `GetLatestConfig()` 函数通过配置管理器获取最新配置，支持配置热更新后的实时读取
    - `StartSymbol` 方法在查找交易对配置前先获取最新配置，确保能找到新添加的交易对
    - 涉及的文件：`main.go`, `web/api_config.go`

---

## v3.4.1 - 2026年01月11日

**Git Tag**: `v3.4.1`

### 新增 (Added)

#### 后端 - 策略多样化扩展
- **增强型 DCA 策略** (`strategy/dca_enhanced.go`)：
  - ATR 动态间距：根据市场波动率自动调整加仓间距
  - 三重止盈机制：固定止盈 + 追踪止盈 + 时间止盈
  - 50层仓位管理：支持多达50层金字塔加仓
  - 瀑布保护：极端行情下的批量止损机制
  - 趋势过滤：基于均线判断趋势方向，避免逆势加仓

- **马丁格尔策略** (`strategy/martingale.go`)：
  - 正向/反向马丁格尔支持
  - 递减风控：随着层数增加降低加仓倍数
  - 多方向支持：做多/做空/双向
  - 最大层数限制和资金保护

- **组合策略模块** (`strategy/combo_strategy.go`)：
  - 多策略组合运行
  - 动态权重调整
  - 市场自适应切换
  - 策略间资金隔离

#### 后端 - 技术指标库
- **新增完整技术指标包** (`indicators/`)：
  - 趋势指标：MACD、ADX、Parabolic SAR、Ichimoku Cloud、Aroon、SuperTrend
  - 波动率指标：ATR、Bollinger Bands、Keltner Channel、Donchian Channel、NATR
  - 动量指标：RSI、Stochastic、CCI、Williams %R、MFI、ROC、TRIX、Ultimate Oscillator
  - 成交量指标：OBV、VWAP、CMF、ADL、Force Index、Chaikin Oscillator

#### 后端 - AI 风险评估
- **AI 风险评估器** (`ai/risk_assessor.go`)：
  - 四维评估：资金安全、风险控制、策略适配、市场环境
  - 智能评分：0-100分综合风险评估
  - 风险因素识别：自动识别高风险配置
  - 优化建议：AI生成的参数优化建议

#### 后端 - 经纪商返佣系统
- **返佣服务** (`saas/broker_rebate.go`)：
  - 多交易所支持
  - 邀请链接生成
  - 返佣追踪统计
  - RESTful API 接口

#### 前端 - 策略管理系统
- **策略市场页面** (`StrategyMarket.tsx`)：
  - 所有可用策略浏览
  - 分类筛选（网格、DCA、马丁格尔、趋势、均值回归、组合）
  - 策略搜索功能
  - 免费/付费策略区分
  - 风险等级标识

- **策略组件** (`components/strategy/`)：
  - `StrategyCard.tsx`：策略卡片展示
  - `StrategyDetailModal.tsx`：策略详情弹窗
  - `StrategyGrid.tsx`：策略网格布局
  - `PremiumBadge.tsx`：付费策略标识

#### 前端 - 资金管理系统
- **资金管理页面** (`CapitalManagement.tsx`)：
  - 账户总览统计
  - 多策略资金分配
  - 可视化分配图表
  - 资金再平衡功能

- **资金组件** (`components/capital/`)：
  - `CapitalSlider.tsx`：资金分配滑块
  - `AllocationChart.tsx`：资金分配图表
  - `RebalanceButton.tsx`：再平衡功能

#### 前端 - 盈利管理系统
- **盈利管理页面** (`ProfitManagement.tsx`)：
  - 盈利趋势图表
  - 按策略盈利统计
  - 手动提取功能
  - 自动提取规则配置

- **盈利组件** (`components/profit/`)：
  - `ProfitChart.tsx`：盈利趋势图表
  - `WithdrawDialog.tsx`：提取对话框
  - `WithdrawRuleForm.tsx`：自动提取规则表单

#### 前端 - API 服务和类型
- **新增 API 服务**：
  - `services/strategy.ts`：策略 API 服务
  - `services/capital.ts`：资金管理 API 服务
  - `services/profit.ts`：盈利管理 API 服务

- **TypeScript 类型定义** (`types/`)：
  - `strategy.ts`：策略相关类型
  - `capital.ts`：资金管理类型
  - `profit.ts`：盈利管理类型

### 变更 (Changed)
- 更新 `App.tsx`：添加策略市场、资金管理、盈利管理路由
- 更新 `Sidebar.tsx`：添加新页面导航入口
- 更新国际化文件：添加中英文翻译（`zh-CN.json`, `en-US.json`）
- 修复 `positionExchangeAdapter` 缺少 `GetAccount` 方法的问题

### 修复 (Fixed)
- 修复后端编译错误：`positionExchangeAdapter` 实现 `position.IExchange` 接口
- 添加 `test_logger.go` 构建忽略标签，避免 main 函数重复声明

---

## v3.3.8 - 2026年01月11日

**Git Tag**: `v3.3.8`

### 说明
- 本版本为 v3.4.1 之前的稳定版本快照
- 包含 v3.3.3 至 v3.3.8-rc8 的所有功能和修复

---

## v3.3.3 - 2026年01月08日

**Git Tag**: `v3.3.3`

### 修复 (Fixed)
- **修复配置完整时系统监控数据提供者未设置的问题**：
  - 问题：当配置完整时（`configComplete && firstRuntime != nil`），系统监控数据提供者（SystemMetricsProvider）未被设置，导致API返回空数据，系统监控页面一直显示"暂无数据"
  - 解决方案：在配置完整的分支中也添加SystemMetricsProvider的设置逻辑，确保无论配置是否完整，系统监控功能都能正常工作
  - 涉及的文件：`main.go`
- **修复启动时满仓状态无法立即开始交易的问题**：
  - 问题：程序启动时如果已有持仓（满仓或接近满仓），虽然会恢复持仓槽位，但不会立即挂卖单，需要等待价格变化才会触发订单调整，导致长时间没有交易
  - 解决方案：在仓位管理器初始化完成后，立即调用一次 `AdjustOrders` 来初始化卖单，确保满仓状态下也能立即开始交易
  - 技术细节：
    - 在 `symbol_manager.go` 的 `startSymbolRuntime` 函数中，`Initialize` 完成后立即调用 `AdjustOrders(currentPrice)`
    - 这样即使启动时已有持仓，也会立即在合适的价格挂卖单，无需等待价格变化
    - 涉及的文件：`symbol_manager.go`

---

## v3.3.2 - 2025年12月27日

**Git Tag**: `v3.3.2`

### 变更 (Changed)
- **时区处理统一**：所有时间统一使用东8区（UTC+8）返回给客户端，数据库存储统一使用UTC时间
  - 程序启动时自动设置时区为东8区（Asia/Shanghai）
  - 创建时间工具包（utils/timezone.go），提供UTC和UTC+8时间转换函数
  - API返回的所有时间字段自动转换为UTC+8时区
  - 数据库存储时自动将时间转换为UTC时区
  - 确保时间数据的一致性和准确性
  - 技术细节：
    - 新增 `utils.ToUTC8()` 函数：将UTC时间转换为东8区时间（用于API返回）
    - 新增 `utils.ToUTC()` 函数：将时间转换为UTC时间（用于数据库存储）
    - 新增 `utils.NowUTC()` 和 `utils.NowUTC8()` 函数：获取当前UTC或UTC+8时间
    - 修改所有API返回函数，在返回前将时间字段转换为UTC+8
    - 修改所有数据库存储函数，在存储前将时间字段转换为UTC
    - 涉及的文件：`main.go`, `web/api.go`, `web/system_metrics_provider.go`, `storage/sqlite.go`, `storage/storage.go`, `storage/log_storage.go`

---

## [未发布] - Unreleased

### 新增
- **修复统计页面胜率计算问题**，改为从 trades 表实时计算（而不是使用 AVG(win_rate)）
- **新增日历视图功能**，在统计页面显示当前月份的每日交易统计（总盈利、胜率、盈利/亏损交易数）
- 新增 `QueryDailyStatisticsFromTrades()` 方法，从 trades 表按日期查询统计（包含盈利/亏损交易数）
- 统计 API 支持混合模式：优先使用 statistics 表，缺失的日期（特别是今天）从 trades 表实时补充
- **可配置系统时区支持**：
  - 支持在配置文件中指定系统时区（如 `Asia/Shanghai`, `UTC`, `America/New_York`）
  - 全局统一时区处理：系统内部所有时间展示和统计均遵循配置的时区
  - Web 界面支持：在系统配置页面新增“系统时区”设置项，支持通过界面修改并即时备份
  - 变更重启提示：修改时区后会提示用户需要重启系统以确保所有组件同步应用
  - 技术细节：
    - 在 `Config` 结构体中新增 `System.Timezone` 字段
    - 升级 `utils/timezone.go`，将硬编码的东8区改为动态加载的全局时区
    - 改造 `logger/logger.go`，支持按配置时区生成日志文件名和日志时间戳
    - 升级 `web/api.go` 中的统计查询逻辑，使用配置时区计算日期范围
    - 保持向后兼容：保留 `ToUTC8` 和 `NowUTC8` 函数作为别名，实际按配置时区处理
    - 涉及的文件：`config/config.go`, `utils/timezone.go`, `logger/logger.go`, `main.go`, `web/api.go`, `webui/src/components/Configuration.tsx`, `webui/src/services/config.ts`, `config/diff.go`

- **自动化单元测试第一阶段实现**：
  - 为系统的基础组件和核心逻辑添加了首批单元测试，提高了代码的可靠性和可维护性
  - 基础工具测试：`utils/orderid_test.go`，验证了订单ID的生成与解析逻辑
  - 配置管理测试：`config/config_test.go`，验证了配置验证、差异对比和热更新逻辑
  - 存储层测试：`storage/sqlite_test.go`，验证了SQLite数据库的CRUD操作和变动存储逻辑
  - 仓位管理初步测试：`position/super_position_manager_test.go`，实现了Mock执行器和交易所，验证了管理器的初始化和成交回调逻辑
  - 技术细节：
    - 修复了 `config/backup.go` 中备份文件名识别和时间戳解析的 bug
    - 规范了存储层测试中的时区处理（统一使用 UTC）
    - 涉及的文件：`utils/orderid_test.go`, `config/config_test.go`, `storage/sqlite_test.go`, `position/super_position_manager_test.go`, `config/backup.go`
- **自动化单元测试第二、三阶段实现**：
  - 风控与安全测试：
    - 账户安全检查：`safety/safety_test.go`，验证了余额不足、杠杆过高、已有持仓跳过检查以及利润无法覆盖手续费等场景。
    - 主动风控监控：`safety/risk_monitor_test.go`，验证了市场异常触发熔断和行情恢复解除风控的逻辑。
    - 持仓对账：`safety/reconciler_test.go`，通过 Mock 模拟本地与交易所状态差异，验证了对账流程的正确性。
  - 策略逻辑测试：
    - 趋势检测：`strategy/trend_detector_test.go`，验证了 MA/EMA 计算逻辑以及根据趋势调整买卖窗口的准确性。
    - 动态参数调整：`strategy/dynamic_adjuster_test.go`，验证了基于波动率动态调整价格间隔以及基于资金利用率调整窗口大小的逻辑。
    - 网格执行包装：`strategy/grid_strategy_test.go`，验证了策略层对底层仓位管理器的调用和回调分发逻辑。
  - 技术细节：
    - 使用反射和 Mock 接口解决了跨包接口依赖和循环导入导致的测试难题。
    - 涉及的文件：`safety/safety_test.go`, `safety/risk_monitor_test.go`, `safety/reconciler_test.go`, `strategy/trend_detector_test.go`, `strategy/dynamic_adjuster_test.go`, `strategy/grid_strategy_test.go`
- **Web界面配置管理系统**：
  - 新增配置编辑页面（`/config`），用户可通过Web界面修改配置，无需直接编辑YAML文件
  - 支持配置变更预览：保存前显示所有变更项及前后对比，提示哪些配置需要重启
  - 自动配置备份：每次保存配置前自动备份，保留最近50个版本，支持恢复和删除备份
  - 配置热更新：自动检测并应用可热更新的配置（交易参数、风控参数等），无需重启
  - 配置文件监控：监控配置文件外部修改，自动重新加载并应用热更新
  - 涉及的文件：
    - 后端：`config/backup.go`, `config/diff.go`, `config/hot_reload.go`, `config/watcher.go`, `web/api_config.go`, `config/config.go`
    - 前端：`webui/src/components/Configuration.tsx`, `webui/src/services/config.ts`
    - 集成：`main.go`, `web/server.go`, `webui/src/App.tsx`
  - 技术细节：
    - 配置備份存儲在 `config.yaml` 同級的 `backups/` 目錄，文件名格式：`config.yaml.backup.{timestamp}.yaml`
    - 热更新判断规则：交易所切换、Web端口、存储路径等需要重启；交易参数、风控参数等可热更新
    - 使用 `fsnotify` 监控配置文件变化，支持外部编辑自动生效
    - API端点：`GET /api/config`, `GET /api/config/json`, `POST /api/config/preview`, `POST /api/config/update`, `GET /api/config/backups`, `POST /api/config/restore/:backup_id`, `DELETE /api/config/backup/:backup_id`
- **退出时自动平仓功能**：
  - 新增配置项 `system.close_positions_on_exit`（默认 `false`，需用户主动开启）
  - 程序退出时，如果启用此选项，会自动查询所有持仓并下 ReduceOnly 平仓单
  - 支持多仓和空仓的自动平仓，使用当前价格或标记价格下单
  - 平仓操作在撤单之后、停止组件之前执行，确保顺序正确
  - 提供详细的平仓日志，包括成功/失败统计
  - 涉及的文件：`main.go`, `config/config.go`, `config.example.yaml`
  - 技术细节：
    - 实现 `closeAllPositions()` 函数，查询持仓并使用 ReduceOnly 订单平仓
    - 多仓（Size > 0）下 SELL 单平仓，空仓（Size < 0）下 BUY 单平仓
    - 价格优先级：当前价格 > 标记价格 > 开仓价格
    - 平仓超时时间：30秒，每个订单间隔100ms避免请求过快
- **集成 Chakra UI v2 现代化 UI 组件库**：
  - 安装 Chakra UI v2.10.9 及相关依赖（@emotion/react、@emotion/styled、framer-motion、@chakra-ui/icons）
  - 创建自定义主题配置（`webui/src/theme.ts`），定义品牌色、字体等设计令牌
  - 配置 ChakraProvider 包裹整个应用，提供全局主题支持
  - 改造核心页面使用 Chakra UI 组件：
    - 登录页面：使用 Box、Container、VStack、Input、Button、Alert 等组件
    - 仪表盘：使用 SimpleGrid、Card、Stat、Badge、Toast 等组件展示系统状态和统计数据
    - 订单管理：使用 Tabs、Table、Badge 等组件优化订单列表展示
    - 持仓管理：使用 Card、Stat、Table、Skeleton 等组件展示持仓信息
    - 系统监控：使用 Select、Progress、Stat 等组件展示系统指标
  - 改造应用主布局：使用 Box、Flex、Container 等布局组件替代传统 CSS
  - 技术细节：
    - Chakra UI 与 Tailwind CSS 共存，Chakra 主要用于组件，Tailwind 用于自定义布局
    - 使用 CSS-in-JS 方案（@emotion），支持主题定制和响应式设计
    - 所有组件内置 ARIA 支持，提升可访问性
    - 涉及的文件：
      - 配置：`webui/src/theme.ts`、`webui/src/App.tsx`、`webui/package.json`
      - 核心页面：`webui/src/components/Login.tsx`、`webui/src/components/Dashboard.tsx`、`webui/src/components/Orders.tsx`、`webui/src/components/Positions.tsx`、`webui/src/components/SystemMonitor.tsx`

### 变更
- **许可证变更**：从 MIT 许可证变更为 AGPL-3.0 双许可模式
  - 开源版本采用 AGPL-3.0 (GNU Affero General Public License v3.0)
  - 要求所有衍生作品必须开源并在 AGPL-3.0 下发布
  - 即使通过网络服务使用，也必须提供源代码
  - 修改后的代码必须回馈给社区
  - 新增商业许可选项，允许在专有应用中使用而无需开源修改
  - 商业许可持有者可获得优先技术支持和技术更新
  - 详细信息请参阅 LICENSE 文件和 README.md 中的许可证说明部分

### 新增
- **Polymarket预测市场信号集成**：
  - 支持从Polymarket获取预测市场数据作为交易信号
  - PolymarketSignalAnalyzer模块：自动获取相关市场数据，生成交易信号
  - 智能市场筛选：根据关键词、流动性、交易量、到期时间筛选有效市场
  - 信号生成逻辑：将预测概率转换为买入/卖出/持有信号
  - 信号聚合：多个市场信号加权聚合，生成综合交易信号
  - 相关性判断：自动判断市场与加密货币的相关性（高/中/低）
  - 集成到决策引擎：预测市场信号与其他AI模块（市场分析、情绪分析、风险分析）综合决策
  - 配置扩展：新增Polymarket配置结构，支持自定义API地址、关键词、筛选条件、信号阈值等
  - 技术细节：
    - 使用GraphQL API获取市场数据
    - 数据缓存机制（避免频繁API调用）
    - 错误处理和降级策略
    - 异步分析（不阻塞主流程）
    - 涉及的文件：`ai/polymarket_signal.go`，`ai/data_sources.go`（扩展），`ai/decision_engine.go`（集成），`config/config.go`，`main.go`
- **AI交易系统集成**：
  - 支持Gemini和OpenAI两种AI服务提供商
  - AI市场分析模块：分析K线数据、技术指标、价格趋势，提供交易信号
  - AI参数优化模块：自动优化网格策略参数（价格间隔、窗口大小、订单金额）
  - AI风险分析模块：智能风险评估和预警，提供风险评分和建议
  - AI市场情绪分析模块：分析新闻RSS和恐慌贪婪指数，评估市场情绪
  - AI决策引擎：整合所有AI模块，支持建议模式、执行模式和混合模式
  - 外部数据源获取：支持RSS新闻源（CoinDesk、CoinTelegraph等）和恐慌贪婪指数API
  - **开箱即用数据源**：AI情绪分析模块内置默认数据源，无需配置即可使用
    - 默认RSS新闻源：CoinDesk、CoinTelegraph、CryptoNews
    - 默认恐慌贪婪指数API：alternative.me（免费，无需API Key）
    - **Reddit API集成**：支持从Reddit获取加密货币相关帖子作为情绪分析数据源
      - 使用Reddit公开JSON API（无需API Key）
      - 默认监控子版块：Bitcoin、ethereum、CryptoCurrency、CryptoMarkets
      - 支持自定义子版块列表和帖子数量限制
      - 按帖子分数排序，优先分析热门内容
      - 数据缓存10分钟，减少API调用
    - 启动时自动显示数据源配置信息
  - 配置扩展：新增AI配置结构，支持各模块独立开关和参数配置
  - 技术细节：
    - 使用工厂模式创建AI服务实例
    - 统一的请求/响应格式
    - 数据缓存机制（避免频繁API调用）
    - 错误处理和降级策略
    - 异步调用AI服务（不阻塞主流程）
    - 涉及的文件：`ai/`目录下的所有文件，`config/config.go`，`main.go`
- **UI 美化与布局优化**：采用 Apple 设计语言重新设计界面
  - 顶部 Header：毛玻璃效果（backdrop-filter）、优雅的阴影、精致的间距和排版
  - 导航栏：流畅的 hover 动画、清晰的活跃状态指示（蓝色下划线）、响应式设计
  - 底部 Footer：新增版权信息和免责声明，采用简洁专业的排版
  - 全局动画：添加 fadeIn、slideIn、scaleIn 等流畅的过渡动画效果
  - 响应式优化：支持移动端和不同屏幕尺寸的适配
- **盈利图交互式提示功能**：鼠标悬停在数据点上时显示详细信息（对账时间、预计盈利、实际盈利、持仓等），提升用户体验
- **对账页面新增仓位走势图**：在对账页面（/reconciliation）新增历史仓位走势图，展示各时间点的本地持仓和交易所持仓变化趋势
  - 同时展示本地持仓和交易所持仓两条曲线，便于对比分析
  - 支持鼠标悬停显示详细信息（对账时间、本地持仓、交易所持仓、持仓差异、挂单数量等）
  - 使用SVG绘制专业图表，与盈利趋势图保持一致的视觉风格
  - 涉及的文件：`webui/src/components/Reconciliation.tsx`
- 创建 CHANGELOG.md 文件，建立版本管理规范
- 添加 ReduceOnly 订单错误自动处理机制
- 新增 `BatchPlaceOrdersWithDetails()` 方法，返回详细的订单执行结果
- 新增 `isReduceOnlyError()` 错误检测函数
- 添加存储服务停止状态检查机制
- 添加 SQLite 数据库关闭状态标记
- **新增个人资料页面**，支持修改密码和管理 WebAuthn 凭证
- 新增修改密码 API (`POST /api/auth/password/change`)
- **引入 Tailwind CSS 现代化 UI 框架**，提升前端开发效率和界面美观度
- **新增亏损率显示功能**，在持仓概览页面显示相对于持仓成本的盈亏百分比
- **新增K线图页面**，支持查看当前交易币种的K线数据和成交量，支持时间周期切换（1m/5m/15m/30m/1h/4h/1d），使用 lightweight-charts 库渲染专业级K线图表
- 新增K线数据API (`GET /api/klines`)，支持查询历史K线数据
- **盈利图交互式提示功能**：鼠标悬停在数据点上时显示详细信息（对账时间、预计盈利、实际盈利、持仓等），提升用户体验

### 修复
- **修复 Gate.io 包装器资产信息获取问题**：修复了 `gateWrapper` 中 `GetBaseAsset()`、`GetQuoteAsset()` 和 `GetPriceDecimals()` 等方法未正确委托给底层适配器的问题。此前 `GetBaseAsset()` 始终返回空字符串，导致前端显示和对账报告中的仓位币种缺失。
- **修复版本号不匹配问题**：修复 `main.go` 中 `Version` 变量与 CHANGELOG.md 中记录的版本号不一致的问题，将版本号从 `v3.3.1` 更正为 `v3.3.2`，确保版本信息准确性
- **修复对账历史表迁移问题**：修复 `reconciliation_history` 表的迁移函数，现在会同时检查和添加 `actual_profit` 和 `created_at` 两个字段，避免旧数据库在插入操作时因缺少 `created_at` 列而失败
- **修复实际盈利总是显示为0的问题**：
  - 原因：系统从未调用 `SaveTrade` 保存交易记录到 `trades` 表，导致 `GetActualProfitBySymbol` 查询时表为空，实际盈利始终为0
  - 解决方案：
    - 在 `position` 包中新增 `TradeStorage` 接口，用于保存交易记录（避免循环导入）
    - 在 `SuperPositionManager` 中添加 `tradeStorage` 字段和 `SetTradeStorage` 方法
    - 在卖单成交时（`OnOrderUpdate` 函数中），计算盈亏并调用 `SaveTrade` 保存交易记录
    - 在 `main.go` 中创建 `tradeStorageAdapter` 适配器，将存储服务注入到 `SuperPositionManager`
  - 技术细节：买入价格使用槽位价格（`slot.Price`），卖出价格使用成交均价，盈亏计算为 `(卖出价格 - 买入价格) * 数量`，买入订单ID暂时设为0（因为无法追溯历史订单）
- 修复 ReduceOnly 订单被拒绝时持续重试的问题（币安 API 错误码 -2022）
- 修复本地槽位持仓状态与交易所实际持仓不同步的问题
- **修复退出时数据库写入失败的问题**（`sql: database is closed` 错误）
- 修复首次设置密码后未自动登录的问题
- 修复注册指纹时提示"未登录"的问题
- **修复首次登录设置密码后反复要求设置密码的问题**
- 修复日志页面缺少实时订阅函数导致 `/logs` 页面报错的问题
- **修复 session_id Cookie 因 Base64 填充符导致会话查找失败的问题**
- **修复前端命名遮蔽导致设置密码请求未发送的问题**
- **修复 DataSourceManager.getCached 方法的并发安全问题**：
  - 问题：`getCached` 方法使用读锁（RLock）但调用了 `delete()` 操作，这会导致运行时panic（"fatal error: concurrent map modification"）
  - 解决方案：修改为先释放读锁检查过期状态，如需删除则获取写锁执行删除操作，使用双重检查确保线程安全
  - 涉及的文件：`ai/data_sources.go`
- **修复 AI 模块关闭时资源泄漏问题**：
  - 问题：AI模块（MarketAnalyzer、ParameterOptimizer、RiskAnalyzer、SentimentAnalyzer、PolymarketSignalAnalyzer）启动后，在程序关闭时未调用 `Stop()` 方法，导致goroutine泄漏和资源泄漏
  - 解决方案：在程序关闭序列中添加所有AI模块的 `Stop()` 方法调用，确保优雅关闭
  - 涉及的文件：`main.go`
- **实现 WebAuthn 注册完成功能**
- **修复前端密码设置请求未发送的问题（state setter 覆盖了 API 方法）**
- **修复会话 ID 在 Cookie 中被转义导致无法识别的问题（去除 Base64 填充）**
- 从 Git 版本控制中移除 `.opensqt.pid` 文件（运行时临时文件，不应被跟踪）
- **修复 K 线图页面交易币种为空导致报错的问题**：前端直接读取 `/api/status` 返回的扁平字段 `symbol`，避免使用不存在的 `status.symbol`
- **修复盈利图负盈利显示问题**：改进 Y 轴范围计算逻辑，确保当所有盈利值为负数时也能正确显示，添加 0 线作为参考
- **修复 Tailwind CSS v4 PostCSS 配置问题**，安装 `@tailwindcss/postcss` 包并更新配置以适配 Tailwind CSS v4 的新架构
- **修复 WebAuthn 注册失败问题**：
  - 前端：将 ArrayBuffer 数组格式改为 base64url 字符串格式，符合 go-webauthn 库的期望格式
  - 后端：添加 `normalizeWebAuthnResponse` 函数，自动将数组格式转换为 base64url 字符串格式，兼容旧版本前端代码
- **增强 WebAuthn 注册流程的日志记录**：在后端添加详细的调试日志，包括请求体内容、响应结构、会话数据等，便于诊断注册失败问题
- **修复已注册设备列表显示问题**：
  - 后端：处理 `device_name` 字段可能为 NULL 的情况，为空时显示"未命名设备"
  - 前端：添加日期格式化函数，正确处理日期显示，避免显示 "Invalid Date"
- **增强 WebAuthn 凭证保存和查询的日志记录**：
  - 在 `SaveCredential` 函数中添加详细的保存日志，包括凭证ID、设备名称、影响行数等
  - 在 `ListCredentials` 函数中添加查询日志，记录查询结果和找到的凭证数量
  - 便于诊断凭证保存和查询问题
- **修复系统监控页面无法显示CPU和内存数据的问题**：
  - 修复前端API接口定义，将字段名改为与后端匹配（cpu_percent, memory_mb, memory_percent, process_id）
  - 修复SystemMonitor组件中使用了未定义的`api`变量，改为使用正确的API函数（getCurrentSystemMetrics, getSystemMetrics, getDailySystemMetrics）
  - 修复API返回数据结构解析，后端`/api/system/metrics/current`直接返回SystemMetrics对象，而不是包装在metrics字段中
  - 更新getSystemMetrics函数支持查询参数（start_time, end_time, granularity）

### 变更
- 改进订单执行器错误处理逻辑，ReduceOnly 错误不再重试
- 增强仓位管理器自动修复能力，检测到 ReduceOnly 错误时自动清空槽位状态
- 优化批量下单接口，支持返回 ReduceOnly 错误详情
- **优化系统退出流程**，调整组件关闭顺序，确保数据完整性
- 改进存储服务关闭逻辑，防止在数据库关闭后继续写入
- 首次设置密码后自动创建会话并登录
- **优化首次设置流程**，使用 sessionStorage 跟踪设置状态，确保密码设置后能继续 WebAuthn 注册
- **将会话 Cookie 的 SameSite 模式从 Strict 改为 Lax**，提高同站请求的兼容性
- **前端 API 基址改为同源绝对地址**，避免代理/扩展劫持相对路径导致设置密码请求未发送
- **优化K线图数据拉取性能**：
  - 根据时间周期（interval）动态调整刷新间隔：1m(30s)、5m(2min)、15m(5min)、30m(10min)、1h(15min)、4h(30min)、1d(1h)
  - 根据时间周期动态调整数据量（limit）：1m(500条)、5m(300条)、15m/30m/1h(200条)、4h(150条)、1d(100条)
  - 添加防抖机制（300ms），避免快速切换interval时频繁请求
  - 添加请求取消机制（AbortController），避免并发重复请求和资源浪费
  - 优化API函数支持请求取消（signal参数）
- **进一步优化K线图前端渲染性能**：
  - 实现增量更新机制：只更新变更的K线数据，而不是全量替换，大幅减少图表重绘开销
  - 添加数据缓存机制：缓存已加载的K线数据，用于增量更新判断
  - 优化图表视图调整：只在首次加载时调用 `fitContent()`，后续更新不再重置用户视图
  - 使用 `useMemo` 缓存数据转换结果，避免重复计算
  - 优化窗口resize事件处理：添加150ms节流，减少不必要的图表重绘
  - 使用 `React.memo` 优化按钮组件渲染，避免不必要的重渲染
  - 优化loading状态显示：只在初始加载时显示，后续增量更新不显示loading

### 技术细节
- 修改文件：
  - `order/executor_adapter.go`: 添加 ReduceOnly 错误检测和处理
  - `position/super_position_manager.go`: 自动清空无效持仓槽位
  - `strategy/multi_strategy_executor.go`: 支持详细错误结果传递
  - `strategy/executor_adapter.go`: 适配新接口
  - `main.go`: 优化退出流程，调整组件关闭顺序
  - `storage/storage.go`: 添加停止状态检查，改进 Stop/Save/batchSave 方法
  - `storage/sqlite.go`: 添加关闭状态标记，防止重复关闭
  - `web/api_auth.go`: 设置密码后自动创建会话
  - `web/session_manager.go`: 将 Cookie SameSite 模式改为 Lax，添加延迟确保 Cookie 处理
  - `web/session_manager.go`: SessionID 使用 RawURLEncoding（无 '=' 填充），避免 Cookie 转义导致会话查找失败
  - `webui/src/components/FirstTimeSetup.tsx`: 使用 sessionStorage 跟踪设置流程状态，添加延迟确保 Cookie 被浏览器处理，修复密码 state setter 覆盖 API 方法的问题
  - `webui/src/App.tsx`: 改进路由逻辑，支持首次设置流程中的状态跟踪
  - `webui/src/services/auth.ts`: API 基址改为同源绝对地址，禁用缓存，确保设置密码请求必发出；改进错误处理，非 2xx 响应会抛出详细错误
  - `webui/src/services/api.ts`: API 基址改为同源绝对地址，避免代理/扩展对相对路径的劫持
  - `webui/src/services/api.ts`: 新增日志 WebSocket 订阅函数 `subscribeLogs`，用于实时接收日志流
  - `webui/src/components/Logs.tsx`: 引入订阅函数，恢复日志页面实时显示能力
  - `webui/src/components/FirstTimeSetup.tsx`: 修复本地 state setter 遮蔽 API 函数的问题，改进错误处理逻辑
  - `web/session_manager.go`: 改用 RawURLEncoding 生成 sessionID，避免 Base64 填充符在 Cookie 中被转义
  - `web/api_webauthn.go`: 实现 `finishWebAuthnRegistration` 函数，完成 WebAuthn 注册流程
  - `webui/src/components/Profile.tsx`: 新增个人资料页面组件，支持修改密码和管理 WebAuthn 凭证
  - `webui/src/components/Profile.css`: 个人资料页面样式
  - `webui/src/App.tsx`: 添加个人资料路由和导航链接
  - `web/api_auth.go`: 新增 `changePassword` 函数，实现修改密码功能
  - `web/server.go`: 添加修改密码路由
  - `webui/src/services/auth.ts`: 新增 `changePassword` 函数
  - `webui/package.json`: 添加 Tailwind CSS、PostCSS 和 Autoprefixer 依赖
  - `webui/tailwind.config.js`: 创建 Tailwind CSS 配置文件，配置内容扫描路径
  - `webui/postcss.config.js`: 创建 PostCSS 配置文件，集成 Tailwind 和 Autoprefixer
  - `webui/src/index.css`: 添加 Tailwind CSS 指令（@tailwind base/components/utilities），保留现有基础样式
  - `webui/src/components/KlineChart.tsx`: 
    - 优化K线图数据拉取性能，添加动态刷新间隔、防抖、请求取消等机制
    - 实现增量更新机制，只更新变更的K线数据，减少图表重绘
    - 添加数据缓存和智能更新逻辑
    - 优化resize事件处理（节流）和组件渲染（React.memo）
  - `webui/src/services/api.ts`: `getKlines` 函数支持可选的 `signal` 参数，用于请求取消
- 新增文档：
  - `rdocs/ReduceOnly错误处理说明.md`
  - `rdocs/退出流程优化说明.md`

### 退出流程优化详情
1. **新的关闭顺序**：
   - 第一优先级：撤销所有订单
   - 第二优先级：优雅停止各个组件（价格监控、订单流、风控等）
   - 第三优先级：取消 context（停止事件处理协程）
   - 等待 500ms 让事件队列处理完毕
   - 第四优先级：停止存储服务（关闭数据库）
   - 等待 200ms 让最后的写入完成

2. **存储服务改进**：
   - 添加 `stopped` 状态标记，防止在停止后接受新事件
   - `Stop()` 方法先取消 context，等待事件处理完，再关闭数据库
   - `Save()` 方法检查服务状态，停止后直接返回
   - `batchSave()` 方法检测数据库关闭错误并优雅处理

3. **数据完整性保证**：
   - 确保所有事件都被正确保存到数据库
   - 防止数据库关闭后继续写入导致的错误
   - 优雅处理关闭过程中的异常情况

---

## 版本历史

### 示例格式

```markdown
## [v1.0.0] - 2025-12-26

**Git Tag:** `v1.0.0`  
**发布时间:** 2025年12月26日

### 新增 (Added)
- 新功能描述

### 变更 (Changed)
- 功能调整描述
- 算法优化描述

### 修复 (Fixed)
- Bug 修复描述

### 移除 (Removed)
- 移除的功能描述

### 安全 (Security)
- 安全相关更新
```

---

## 变更类型说明

- **新增 (Added)**: 新增的功能
- **变更 (Changed)**: 对现有功能的变更或算法调整
- **弃用 (Deprecated)**: 即将移除的功能
- **移除 (Removed)**: 已移除的功能
- **修复 (Fixed)**: Bug 修复
- **安全 (Security)**: 安全相关的修复或更新

---

## 注意事项

1. 每次发布新版本前，将 `[未发布]` 部分的内容移动到新版本记录中
2. 确保每个版本都有对应的 Git Tag
3. 记录时间格式：YYYY年MM月DD日
4. 重要的算法调整需要详细说明调整原因和预期效果
5. 破坏性变更需要特别标注 **[BREAKING CHANGE]**

