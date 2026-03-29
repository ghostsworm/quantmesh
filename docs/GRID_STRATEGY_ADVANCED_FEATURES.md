# 網格策略進階功能（P1 / P2 與 3.53 增強）

本指南說明網格策略的進階功能：**3.53 網格增強（對標幣安）**、**P1（資金費率與趨勢聯動）**、**P2（訂單簿優化掛單）**，以及動態單筆金額調整。

## 3.53 網格增強（對標幣安）

自 3.53.0 起，網格策略支援以下進階參數，便於精細風控與策略行為控制。

| 功能 | 配置鍵 | 說明 |
|--------|-------------|-------------|
| 網格風控 | `trading.symbols[].grid_risk_control` | 止損比例、止盈觸發/回撤止盈、最大持倉層數、預警時最多開倉單數、趨勢過濾；Web 配置頁「網格風控」區塊可編輯 |
| 價格範圍 | `price_low` / `price_high` | 軟限制：超出範圍暫停新開倉，保留已有倉位平倉單；槽位價格裁剪到 [price_low, price_high] |
| 觸發價格 | `trigger_price` | 達到後才啟動網格（做多：當前價 ≤ 觸發價；做空：當前價 ≥ 觸發價）；0 表示立即啟動 |
| 網格模式 | `grid_mode` | `arithmetic` 等差（固定價差）或 `geometric` 等比（固定比例；等比時 `price_interval` 為比例，如 0.005 表示 0.5%） |
| 網格上移/下移 | `grid_shift_step` + API | 配置步長；`POST /api/grid/shift-up`、`POST /api/grid/shift-down` 可手動整體移動網格錨點並撤銷開倉委託 |
| 終止時全部平倉 | `close_on_stop` | 策略停止時若啟用則自動執行全平倉 |

### 網格風控（grid_risk_control）

- **enabled**：是否啟用網格風控
- **stop_loss_ratio**：單幣種最大浮虧比例（如 0.1 表示 10%），達到時全部平倉
- **take_profit_trigger_ratio**：盈利達到此比例後開啟回撤止盈（如 0.08 表示 8%）
- **trailing_take_profit_ratio**：盈利回撤比例（如 0.03 表示回撤 3% 止盈）
- **max_grid_layers**：最大持倉層數預警；達到後不再新開倉
- **max_open_orders_at_cap**：達到預警時最多允許的開倉單數；0 表示僅不新開倉不撤單
- **trend_filter_enabled**：趨勢過濾；下跌趨勢中可暫停買入

### 網格上移/下移 API

- `POST /api/grid/shift-up?exchange=xxx&symbol=xxx&step=可選`：網格上移（錨點 + step）
- `POST /api/grid/shift-down?exchange=xxx&symbol=xxx&step=可選`：網格下移（錨點 - step）  
未傳 `step` 時使用該交易對配置的 `grid_shift_step` 或 `price_interval`。

---

## 概覽（P1 / P2 / 動態）

| 功能 | 配置鍵 | 說明 |
|--------|-------------|-------------|
| P1 | `funding_rate.trend_sync_enabled` | 資金費率偏向與趨勢過濾聯動，改善買賣時機 |
| P2 | `trading.orderbook_optimization` | 依訂單簿深度微調掛單價格，避開流動性空洞 |
| 動態 | `trading.dynamic_adjustment.order_quantity` | 依交易頻率調整單筆下單金額 |

---

## P1：資金費率與趨勢聯動

### 目的

負費率且趨勢向上時放寬買入；高正費率且趨勢向下時加強賣出偏向，以提升進出場品質。

### 配置

```yaml
funding_rate:
  enabled: true
  bias_enabled: true
  trend_sync_enabled: true   # 啟用 P1：費率與趨勢聯動（預設 true）
  high_rate_threshold: 0.001
  pause_buy_threshold: 0.0015
```

### 行為

- **放寬買入**：`buyBias > 1`（負費率）且趨勢為上漲 → 趨勢過濾放寬；`allowedNewBuyOrders` 可能提高（受偏向係數上限）。
- **加強賣出**：`buyBias < 1`（高正費率）且趨勢為下跌 → `skipBuying = true` 或 `allowedNewBuyOrders` 降為 0。
- **其他情況**：維持既有邏輯；趨勢與費率照舊合併計算。

### 依賴

- `funding_rate.bias_enabled: true`
- 網格風控中 `trend_filter_enabled: true`（有使用趨勢時）
- `RiskMonitor` 與趨勢偵測（如 `TrendDetector`）須在 `SuperPositionManager` 中注入

### 調參

- 以 `high_rate_threshold`、`pause_buy_threshold` 定義「高費率」；可依交易對與資金費率週期調整。
- 若 P1 過於積極，可設 `trend_sync_enabled: false` 退回未聯動行為。

---

## P2：訂單簿優化掛單

### 目的

依訂單簿深度微調網格掛單價格，避免掛在「空洞」區域，提高成交機率。

### 配置

```yaml
trading:
  orderbook_optimization:
    enabled: true
    depth_levels: 20           # 取得訂單簿檔位數
    min_depth_usdt: 5000       # 低於此值（N 檔合計）視為空洞，需微調
    lookback_levels: 3          # 檢查候選價前後 N 檔
    optimization_interval: 30   # 優化間隔（秒）；0 表示每次 AdjustOrders 都優化
```

### 行為

1. 下單前以 `depth_levels` 取得訂單簿。
2. 對每個候選價：
   - **買單**：若該價下方 `lookback_levels` 檔的深度總和（USDT）小於 `min_depth_usdt`，則略向下微調至有量檔位。
   - **賣單**：若該價上方深度總和小於 `min_depth_usdt`，則略向上微調。
3. 微調後價格仍落在網格買賣視窗與 `price_interval` 內。

### 邊界

- 訂單簿取得失敗時使用原價，不影響既有網格邏輯。
- 微調幅度受限，以維持網格結構。

### 調參

- 大額或流動性較差者可提高 `min_depth_usdt`。
- 可提高 `optimization_interval` 以降低交易所 API 呼叫頻率。

---

## 動態單筆金額（P0）

### 目的

依近期成交頻率調整單筆下單金額（例如成交少時提高、成交多時降低）。

### 配置

```yaml
trading:
  dynamic_adjustment:
    enabled: true
    order_quantity:
      enabled: true
      min: 50
      max: 300
      frequency_threshold: 2    # 每分鐘成交次數閾值
      adjustment_step: 20
      check_interval: 60         # 檢查間隔（秒）
```

### 行為

- 系統週期性檢查成交頻率；低於 `frequency_threshold` 時可提高單筆金額（上限 `max`），高於時可降低（下限 `min`），步長為 `adjustment_step`。

### 介面

動態調整（含單筆金額）可在 Web 介面的網格／交易對設定中配置。

---

## 參數摘要

| 參數 | 區塊 | 預設 | 說明 |
|-----------|---------|---------|--------|
| `trend_sync_enabled` | `funding_rate` | true | P1 開關 |
| `orderbook_optimization.enabled` | `trading` | false | P2 開關 |
| `orderbook_optimization.depth_levels` | `trading` | 20 | 訂單簿檔位數 |
| `orderbook_optimization.min_depth_usdt` | `trading` | 5000 | 空洞閾值（USDT） |
| `orderbook_optimization.lookback_levels` | `trading` | 3 | 候選價前後檢查檔數 |
| `orderbook_optimization.optimization_interval` | `trading` | 30 | 優化間隔（秒）；0 為每次執行 |
| `dynamic_adjustment.order_quantity.*` | `trading` | — | P0 動態單筆金額 |

實作細節見 [GRID_ALPHA_P1_P2_SPEC.md](GRID_ALPHA_P1_P2_SPEC.md)。

<!-- quantmesh usage beacon -->
![](https://um.facev.app/p/IiDQJEIGM)
