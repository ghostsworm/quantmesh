# API 參考

所有 HTTP API 皆以 `/api` 為前綴。成功回應通常為 `200`。伺服器會在每個 API 回應中加入 `X-App-Version` 供除錯。

**Base URL**：`http://<host>:<port>/api`（預設埠見 `web.port`，如 28888）

**認證**：多數業務 API 需認證（Session Cookie 或設定的 API Key）。公開端點：`/api/version`、`/api/auth/*`（登入／設定）、`/api/setup/*`。

---

## 版本

| 方法 | 路徑 | 認證 | 說明 |
|--------|------|------|-------------|
| GET | `/api/version` | 否 | 回傳 `{ "version": "<semver>" }`；回應頭含 `X-App-Version`。 |

---

## 認證

| 方法 | 路徑 | 認證 | 說明 |
|--------|------|------|-------------|
| GET | `/api/auth/status` | 否 | 當前認證狀態 |
| POST | `/api/auth/password/set` | 否 | 設定初始密碼（引導） |
| POST | `/api/auth/password/verify` | 否 | 驗證密碼（登入） |
| POST | `/api/auth/logout` | 否 | 登出 |
| POST | `/api/auth/password/change` | 是 | 變更密碼 |

---

## 設定引導

| 方法 | 路徑 | 認證 | 說明 |
|--------|------|------|-------------|
| GET | `/api/setup/status` | 否 | 設定完成狀態 |
| POST | `/api/setup/init` | 否 | 初始化設定 |
| POST | `/api/setup/exchange-symbols` | 否 | 取得交易所交易對 |

---

## WebAuthn

| 方法 | 路徑 | 認證 | 說明 |
|--------|------|------|-------------|
| POST | `/api/webauthn/register/begin` | 是 | 開始 WebAuthn 註冊 |
| POST | `/api/webauthn/register/finish` | 是 | 完成 WebAuthn 註冊 |
| POST | `/api/webauthn/login/begin` | 否 | 開始 WebAuthn 登入 |
| POST | `/api/webauthn/login/finish` | 否 | 完成 WebAuthn 登入 |
| GET | `/api/webauthn/credentials` | 是 | 列出 WebAuthn 憑證 |
| POST | `/api/webauthn/credentials/delete` | 是 | 刪除憑證 |

---

## 狀態與交易

| 方法 | 路徑 | 認證 | 說明 |
|--------|------|------|-------------|
| GET | `/api/status` | 是 | 單一實例狀態 |
| GET | `/api/statuses` | 是 | 多實例狀態 |
| GET | `/api/symbols` | 是 | 交易對列表 |
| GET | `/api/exchanges` | 是 | 交易所列表 |
| GET | `/api/positions` | 是 | 持倉 |
| GET | `/api/positions/summary` | 是 | 持倉摘要 |
| GET | `/api/orders` | 是 | 訂單 |
| GET | `/api/orders/history` | 是 | 訂單歷史 |
| GET | `/api/orders/pending` | 是 | 未成交訂單 |
| POST | `/api/trading/start` | 是 | 開始交易 |
| POST | `/api/trading/stop` | 是 | 停止交易 |
| POST | `/api/trading/close-positions` | 是 | 平掉所有持倉 |
| POST | `/api/grid/shift-up` | 是 | 網格上移（查參：`exchange`, `symbol`, 可選 `step`） |
| POST | `/api/grid/shift-down` | 是 | 網格下移（查參：`exchange`, `symbol`, 可選 `step`） |

---

## 統計

| 方法 | 路徑 | 認證 | 說明 |
|--------|------|------|-------------|
| GET | `/api/statistics` | 是 | 統計 |
| GET | `/api/statistics/daily` | 是 | 每日統計 |
| GET | `/api/statistics/trades` | 是 | 成交統計 |
| GET | `/api/statistics/pnl/symbol` | 是 | 依交易對盈虧 |
| GET | `/api/statistics/pnl/time-range` | 是 | 依時間區間盈虧 |
| GET | `/api/statistics/pnl/exchange` | 是 | 依交易所盈虧 |
| GET | `/api/statistics/pnl/diagnosis` | 是 | 交易所盈虧診斷 |
| GET | `/api/statistics/anomalous-trades` | 是 | 異常成交 |

---

## 分配與計劃

| 方法 | 路徑 | 認證 | 說明 |
|--------|------|------|-------------|
| GET | `/api/allocation/status` | 是 | 分配狀態 |
| GET | `/api/allocation/status/:exchange/:symbol` | 是 | 依交易所／交易對分配 |
| GET | `/api/position-plans/check` | 是 | 持倉計劃檢查 |
| GET | `/api/position-plans` | 是 | 持倉計劃列表 |
| GET | `/api/position-plans/:id` | 是 | 取得持倉計劃 |
| POST | `/api/position-plans` | 是 | 建立持倉計劃 |
| PUT | `/api/position-plans/:id` | 是 | 更新持倉計劃 |
| DELETE | `/api/position-plans/:id` | 是 | 取消持倉計劃 |

---

## 對帳與風控

| 方法 | 路徑 | 認證 | 說明 |
|--------|------|------|-------------|
| GET | `/api/reconciliation/status` | 是 | 對帳狀態 |
| GET | `/api/reconciliation/history` | 是 | 對帳歷史 |
| GET | `/api/reconciliation/aggregated` | 是 | 對帳彙總 |
| GET | `/api/risk/status` | 是 | 風控狀態 |
| GET | `/api/risk/monitor` | 是 | 風控監控資料 |
| GET | `/api/risk/history` | 是 | 風控檢查歷史 |
| GET | `/api/risk/newbie-check` | 是 | 新手風控檢查 |
| POST | `/api/risk/newbie-check/apply` | 是 | 套用新手安全設定 |

---

## 新聞與預測

| 方法 | 路徑 | 認證 | 說明 |
|--------|------|------|-------------|
| GET | `/api/news/analysis` | 是 | 新聞分析 |
| GET | `/api/news/predictions` | 是 | 新聞預測 |
| POST | `/api/news/analyze` | 是 | 觸發新聞分析 |
| GET | `/api/news/collected` | 是 | 已收集新聞 |
| GET | `/api/news/keywords` | 是 | 新聞關鍵詞 |
| PUT | `/api/news/keywords` | 是 | 更新新聞關鍵詞 |
| GET | `/api/news/history` | 是 | 新聞歷史 |
| GET | `/api/news/history/:id` | 是 | 依 ID 新聞歷史 |
| GET | `/api/predictions/accuracy` | 是 | 預測準確度 |
| GET | `/api/predictions/history` | 是 | 預測歷史 |

---

## 配置

| 方法 | 路徑 | 認證 | 說明 |
|--------|------|------|-------------|
| GET | `/api/config` | 是 | 取得配置 |
| GET | `/api/config/json` | 是 | 取得配置 JSON |
| POST | `/api/config/validate` | 是 | 驗證配置 |
| POST | `/api/config/validate-yaml` | 是 | 驗證 YAML 配置 |
| POST | `/api/config/preview` | 是 | 預覽配置 |
| POST | `/api/config/update` | 是 | 更新配置 |
| POST | `/api/config/update-yaml` | 是 | 以 YAML 更新配置 |
| GET | `/api/config/backups` | 是 | 備份列表 |
| POST | `/api/config/restore/:backup_id` | 是 | 還原備份 |
| DELETE | `/api/config/backup/:backup_id` | 是 | 刪除備份 |
| GET | `/api/config/history` | 是 | 配置歷史列表 |
| GET | `/api/config/history/:version` | 是 | 指定版本配置 |
| POST | `/api/config/history/:version/restore` | 是 | 還原該版本 |
| POST | `/api/config/history/diff` | 是 | 配置版本差異 |

---

## 匯出

| 方法 | 路徑 | 認證 | 說明 |
|--------|------|------|-------------|
| GET | `/api/export/config` | 是 | 匯出配置 |
| GET | `/api/export/config/history/:version` | 是 | 匯出指定版本配置 |
| GET | `/api/export/trades` | 是 | 匯出成交 |
| GET | `/api/export/orders` | 是 | 匯出訂單 |
| GET | `/api/export/positions` | 是 | 匯出持倉 |
| GET | `/api/export/statistics` | 是 | 匯出統計 |
| GET | `/api/export/reconciliation` | 是 | 匯出對帳 |
| GET | `/api/export/risk-checks` | 是 | 匯出風控檢查 |
| GET | `/api/export/system-metrics` | 是 | 匯出系統指標 |
| GET | `/api/export/logs` | 是 | 匯出日誌 |
| GET | `/api/export/audit-logs` | 是 | 匯出審計日誌 |
| GET | `/api/export/all` | 是 | 匯出全部 |

---

## 回測

| 方法 | 路徑 | 認證 | 說明 |
|--------|------|------|-------------|
| POST | `/api/backtest/run` | 是 | 執行回測 |
| GET | `/api/backtest/strategies` | 是 | 回測策略 |
| GET | `/api/backtest/presets/:symbol` | 是 | 交易對預設 |
| POST | `/api/backtest/cache/generate` | 是 | 產生快取 |
| GET | `/api/backtest/cache/status` | 是 | 快取狀態 |
| GET | `/api/backtest/cache/stats` | 是 | 快取統計 |
| GET | `/api/backtest/cache/list` | 是 | 快取列表 |
| DELETE | `/api/backtest/cache/:key` | 是 | 刪除快取項目 |
| DELETE | `/api/backtest/cache` | 是 | 清空快取 |
| POST | `/api/backtest/tasks` | 是 | 建立回測任務 |
| GET | `/api/backtest/tasks` | 是 | 回測任務列表 |
| GET | `/api/backtest/tasks/:id` | 是 | 取得任務 |
| GET | `/api/backtest/tasks/:id/result` | 是 | 任務結果 |
| GET | `/api/backtest/tasks/:id/report` | 是 | 任務報告 |
| DELETE | `/api/backtest/tasks/:id` | 是 | 刪除任務 |

---

## 優化器

| 方法 | 路徑 | 認證 | 說明 |
|--------|------|------|-------------|
| GET | `/api/optimizer/price` | 是 | 優化器價格 |
| POST | `/api/optimizer/run` | 是 | 執行優化器 |
| GET | `/api/optimizer/status/:id` | 是 | 優化器狀態 |
| GET | `/api/optimizer/result/:id` | 是 | 優化器結果 |
| POST | `/api/optimizer/stop/:id` | 是 | 停止優化器 |

---

## 支付（加密貨幣）

| 方法 | 路徑 | 認證 | 說明 |
|--------|------|------|-------------|
| GET | `/api/payment/crypto/currencies` | 是 | 支援的加密貨幣 |
| POST | `/api/payment/crypto/coinbase/create` | 是 | 建立 Coinbase 支付 |
| POST | `/api/payment/crypto/direct/create` | 是 | 建立直接支付 |
| GET | `/api/payment/crypto/list` | 是 | 使用者支付列表 |
| GET | `/api/payment/crypto/:id` | 是 | 支付狀態 |
| POST | `/api/payment/crypto/:id/submit-tx` | 是 | 提交交易雜湊 |
| POST | `/api/payment/crypto/:id/confirm` | 是 | 確認直接支付（管理員） |

---

## 系統與日誌

| 方法 | 路徑 | 認證 | 說明 |
|--------|------|------|-------------|
| GET | `/api/system/metrics` | 是 | 系統指標 |
| GET | `/api/system/metrics/current` | 是 | 當前系統指標 |
| GET | `/api/system/metrics/daily` | 是 | 每日系統指標 |
| GET | `/api/logs` | 是 | 日誌 |
| POST | `/api/logs/clean` | 是 | 清理日誌 |
| GET | `/api/logs/stats` | 是 | 日誌統計 |
| POST | `/api/logs/vacuum` | 是 | 日誌壓縮 |

---

## 槽位、策略、K 線、資金費率

| 方法 | 路徑 | 認證 | 說明 |
|--------|------|------|-------------|
| GET | `/api/slots` | 是 | 槽位資料 |
| GET | `/api/strategies/allocation` | 是 | 策略分配 |
| GET | `/api/klines` | 是 | K 線資料 |
| GET | `/api/funding/current` | 是 | 當前資金費率 |
| GET | `/api/funding/history` | 是 | 資金費率歷史 |

---

## AI

| 方法 | 路徑 | 認證 | 說明 |
|--------|------|------|-------------|
| GET | `/api/ai/status` | 是 | AI 分析狀態 |
| GET | `/api/ai/analysis/market` | 是 | 市場分析 |
| GET | `/api/ai/analysis/parameter` | 是 | 參數優化 |
| GET | `/api/ai/analysis/risk` | 是 | 風險分析 |
| GET | `/api/ai/analysis/sentiment` | 是 | 情緒分析 |
| GET | `/api/ai/analysis/polymarket` | 是 | Polymarket 信號 |
| POST | `/api/ai/analysis/trigger/:module` | 是 | 觸發 AI 模組 |
| GET | `/api/ai/prompts` | 是 | AI 提示詞 |
| POST | `/api/ai/prompts` | 是 | 更新 AI 提示詞 |
| POST | `/api/ai/generate-config` | 是 | 產生 AI 配置 |
| GET | `/api/ai/task/:task_id` | 是 | AI 任務狀態 |
| GET | `/api/ai/tasks` | 是 | AI 任務列表 |
| GET | `/api/ai/tasks/stats` | 是 | AI 任務統計 |
| POST | `/api/ai/apply-config` | 是 | 套用 AI 配置 |

---

## 價差監控

| 方法 | 路徑 | 認證 | 說明 |
|--------|------|------|-------------|
| GET | `/api/basis/current` | 是 | 當前價差 |
| GET | `/api/basis/history` | 是 | 價差歷史 |
| GET | `/api/basis/statistics` | 是 | 價差統計 |

---

## 市場情報、權限、審計

| 方法 | 路徑 | 認證 | 說明 |
|--------|------|------|-------------|
| GET | `/api/market-intelligence` | 是 | 市場情報 |
| GET | `/api/permissions/check` | 是 | API 權限檢查 |
| GET | `/api/audit/logs` | 是 | 審計日誌 |

---

## 策略管理

| 方法 | 路徑 | 認證 | 說明 |
|--------|------|------|-------------|
| GET | `/api/strategies` | 是 | 策略列表 |
| GET | `/api/strategies/types` | 是 | 策略類型 |
| GET | `/api/strategies/configs` | 是 | 策略配置 |
| GET | `/api/strategies/enabled` | 是 | 已啟用策略 |
| POST | `/api/strategies/batch-update` | 是 | 批次更新策略 |
| GET | `/api/strategies/:id` | 是 | 策略詳情 |
| POST | `/api/strategies/:id/enable` | 是 | 啟用策略 |
| POST | `/api/strategies/:id/disable` | 是 | 停用策略 |
| GET | `/api/strategies/:id/license` | 是 | 策略授權 |
| PUT | `/api/strategies/:id/config` | 是 | 更新策略配置 |
| POST | `/api/strategies/:id/purchase` | 是 | 購買策略 |

---

## 盈利

| 方法 | 路徑 | 認證 | 說明 |
|--------|------|------|-------------|
| GET | `/api/profit/summary` | 是 | 盈利摘要 |
| GET | `/api/profit/by-strategy` | 是 | 依策略盈利 |
| GET | `/api/profit/by-strategy/:id` | 是 | 策略盈利詳情 |
| GET | `/api/profit/withdraw-rules` | 是 | 提領規則 |
| PUT | `/api/profit/withdraw-rules` | 是 | 更新提領規則 |
| POST | `/api/profit/withdraw-rules/upsert` | 是 | 新增或更新提領規則 |
| DELETE | `/api/profit/withdraw-rules/:id` | 是 | 刪除提領規則 |
| POST | `/api/profit/withdraw` | 是 | 提領盈利 |
| GET | `/api/profit/history` | 是 | 提領歷史 |
| GET | `/api/profit/trend` | 是 | 盈利趨勢 |
| POST | `/api/profit/withdraw/estimate` | 是 | 估提領手續費 |
| POST | `/api/profit/withdraw/:id/cancel` | 是 | 取消提領 |
| GET | `/api/profit/withdraw/:id` | 是 | 提領詳情 |

---

## 資金

| 方法 | 路徑 | 認證 | 說明 |
|--------|------|------|-------------|
| GET | `/api/capital/overview` | 是 | 資金總覽 |
| GET | `/api/capital/allocation` | 是 | 資金分配 |
| PUT | `/api/capital/allocation` | 是 | 更新分配 |
| GET | `/api/capital/allocation/:id` | 是 | 策略資金詳情 |
| PUT | `/api/capital/allocation/:id` | 是 | 更新策略資金 |
| POST | `/api/capital/allocation/:id/lock` | 是 | 鎖定策略資金 |
| POST | `/api/capital/rebalance` | 是 | 再平衡資金 |
| GET | `/api/capital/history` | 是 | 資金歷史 |
| PUT | `/api/capital/reserve` | 是 | 設定保留資金 |

---

## Webhooks（無認證；驗簽）

| 方法 | 路徑 | 說明 |
|--------|------|-------------|
| POST | `/api/billing/webhook/stripe` | Stripe Webhook |
| POST | `/api/payment/crypto/webhook/coinbase` | Coinbase Webhook |

---

## WebSocket

| 路徑 | 說明 |
|------|-------------|
| GET | `/ws`：WebSocket 連線，即時更新（如價格、訂單）。 |

---

## 其他端點

- **Prometheus**：`GET /metrics`（不在 `/api` 下；無認證）。
- **pprof**：`/debug/pprof/*`，需在 `web.pprof.enabled` 為 true 時啟用；可能需認證或 IP 白名單。

---

## 錯誤

- `401`：未認證或認證無效。
- `403`：禁止（如 pprof IP 未允許）。
- `404`：找不到。
- `500`：伺服器錯誤；請查回應內容與日誌。

錯誤回應主體通常為 JSON，例如 `{ "error": "訊息" }`。

<!-- quantmesh usage beacon -->
![](https://um.facev.app/p/IiDQJEIGM)
