# P1/P2 規格詳情：資金費率趨勢聯動與訂單簿優化掛單

> 本文件定義後續待實現的 P1、P2 級別改進的詳細規格，供開發參考。

---

## P1: 資金費率與趨勢聯動

### 目標

將資金費率偏向策略與趨勢過濾聯動，在「負費率 + 上漲趨勢」時放寬買入限制，在「高正費率 + 下跌趨勢」時加強賣出偏向，提高買賣時機質量。

### 修改位置

- 檔案：[`position/super_position_manager.go`](position/super_position_manager.go)
- 函數：`AdjustOrders`（約 669-721 行）
- 區塊：`skipBuying` 與 `allowedNewBuyOrders` 的計算邏輯

### 邏輯規格

| 情境 | 條件 | 行為 |
|------|------|------|
| 放寬買入 | `buyBias > 1`（負費率）且 `trend == "up"`（上漲趨勢） | 放寬趨勢過濾：`skipBuying` 不因趨勢為 down 而設為 true；或將 `allowedNewBuyOrders` 乘以 `min(1.2, buyBias)` 等係數 |
| 加強賣出偏向 | `buyBias < 1`（高正費率）且 `trend == "down"`（下跌趨勢） | 維持或強化：`skipBuying = true`；或將 `allowedNewBuyOrders` 進一步乘以 `buyBias` 甚至設為 0 |
| 預設 | 其他組合 | 維持現有邏輯：趨勢過濾與資金費率偏向各自獨立計算後合併 |

### 偽代碼

```go
// 現有: 趨勢過濾
if trendDetector != nil && trend == "down" {
    skipBuying = true
}
// 現有: 資金費率偏向調整 allowedNewBuyOrders

// 新增: 聯動邏輯 (當 funding_rate.trend_sync_enabled 為 true)
if cfg.FundingRate.TrendSyncEnabled && fundingMonitor != nil && trendDetector != nil {
    buyBias := fundingMonitor.GetBuyBias()
    trend := trendDetector.GetCurrentTrend()
    if buyBias > 1 && trend == "up" {
        // 負費率 + 上漲：允許在趨勢過濾時仍少量買入
        if skipBuying {
            skipBuying = false  // 或改為 allowedNewBuyOrders = 1
        }
        allowedNewBuyOrders = int(float64(allowedNewBuyOrders) * math.Min(1.2, buyBias))
    } else if buyBias < 1 && trend == "down" {
        // 高費率 + 下跌：強制 skipBuying 或將 allowedNewBuyOrders 設為 0
        skipBuying = true
        allowedNewBuyOrders = 0
    }
}
```

### 依賴

- `spm.fundingMonitor`：已注入
- `spm.trendDetector`：已注入
- 需確保 `BiasEnabled` 與 `TrendFilterEnabled` 皆啟用時，聯動才生效

### 配置

新增至 `FundingRateConfig`：

```yaml
funding_rate:
  enabled: true
  bias_enabled: true
  trend_sync_enabled: true   # 新增：是否啟用費率與趨勢聯動，預設 true
```

### 測試要點

- 負費率 + 上漲趨勢：應允許買入或提高 `allowedNewBuyOrders`
- 高正費率 + 下跌趨勢：應 `skipBuying = true` 或 `allowedNewBuyOrders = 0`
- 聯動關閉時：行為與現有一致

---

## P2: 訂單簿優化掛單 (Orderbook-Aware Grid)

### 目標

根據訂單簿深度微調網格掛單價格，避免掛在「空洞」區域，提高成交機率。

### 修改位置

- 檔案：[`position/super_position_manager.go`](position/super_position_manager.go)
- 相關函數：`calculateSlotPrices`、下單前的價格計算，或新增 `optimizeSlotPricesWithOrderBook`

### 實現思路

1. 在下單前取得 `GetOrderBook(ctx, symbol, depthLevels)`
2. 對每個候選價格 `p`：
   - **買單**：檢查 `p` 附近的 ask 檔位深度；若 N 檔內總量 < `min_depth_usdt`，則將價格微調至下一個有量的 ask 檔位（略下移）
   - **賣單**：檢查 `p` 附近的 bid 檔位深度；若 N 檔內總量 < `min_depth_usdt`，則將價格微調至下一個有量的 bid 檔位（略上移）
3. 微調後價格需落在 `price_interval` 的合理範圍內，且不超越買賣窗口邊界

### 技術要點

| 項目 | 說明 |
|------|------|
| 取得 OrderBook | `SuperPositionManager` 需能取得 `IExchange`，可透過構造函數注入或從 `SymbolRuntime` 傳入 |
| 深度計算 | 每個檔位：`depth_usdt = price * quantity`；N 檔內總量為各檔 `depth_usdt` 之和 |
| 「空洞」定義 | 候選價上下 N 檔的累計深度 < `min_depth_usdt` 時視為空洞 |
| 微調方向 | 買單下移（略低於原價）、賣單上移（略高於原價），步長可為 tick size 或 `price_interval` 的 10% |
| 呼叫時機 | 每次 `AdjustOrders` 時若啟用，在計算 `slotPrices` 後、下單前呼叫優化函數；可設為每 M 秒優化一次以降低 API 呼叫頻率 |

### 配置

新增至 `Trading`：

```yaml
trading:
  orderbook_optimization:
    enabled: false                    # 預設關閉
    depth_levels: 20                  # 取得訂單簿檔位數
    min_depth_usdt: 5000              # 低於此視為空洞，需微調
    lookback_levels: 3                # 檢查候選價前後 N 檔
    optimization_interval: 30         # 優化間隔（秒），0 表示每次 AdjustOrders 都優化
```

### 邊界處理

- 若 `GetOrderBook` 失敗，則使用原價格，不影響現有下單邏輯
- 微調後價格不得超出 `buy_window` / `sell_window` 範圍
- 微調幅度應小於 `price_interval`，避免破壞網格結構

### 測試要點

- 深度正常：價格不變或微調幅度很小
- 深度薄：價格應向有量檔位偏移
- API 失敗：回退到原價格，無異常
