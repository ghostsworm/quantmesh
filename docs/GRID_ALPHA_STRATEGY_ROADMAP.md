# 網格策略競爭優勢路線圖

> 在 BTCUSDT 合約網格同質化競爭中，如何建立不對稱優勢

## 一、QuantMesh 現有優勢盤點

### ✅ 已實現且領先

| 能力 | 狀態 | 說明 |
|-----|------|------|
| 資金費率策略整合 | ✅ 完成 | `FundingRateMonitor` + `FundingArbitrageManager`，費率偏向買入、期現套利 |
| 波動率自適應網格 | ✅ 完成 | `DynamicAdjuster` 根據波動率動態調整價格間隔、窗口大小 |
| 趨勢過濾 | ✅ 完成 | `TrendDetector` + `GridRiskControl.TrendFilterEnabled`，下跌趨勢暫停買入 |
| 網格風控 | ✅ 完成 | 硬止損、動態止盈、層數限制 |
| 訂單簿深度監控 | ✅ 完成 | `DepthMonitor` 深度異常時暫停交易 |
| 多交易所支援 | ✅ 完成 | Binance/OKX/Bybit/Gate/Bitget 等 |

### ⚠️ 部分實現 / 可強化

| 能力 | 狀態 | 差距 |
|-----|------|------|
| 動態單筆金額 | 配置存在，未啟用 | `DynamicAdjustment.OrderQuantity` 有結構，但 `adjustOrderQuantityLoop` 未在 `Start()` 中調用 |
| 訂單簿優化掛單 | 僅用於風控 | `DepthMonitor` 只做深度下降告警，未用於優化掛單位置 |
| 鏈上數據 | 無 | 鯨魚轉賬、大額流入流出預警尚未接入 |

---

## 二、競爭優勢矩陣

```
                    可行性
                   低 ←→ 高
                   ┌────┬────┐
                   │ 鏈上 │ 波動率 │
        潛在收益   │ 預警 │ 自適應 │  ← 已有
           高      ├────┼────┤
                   │ 跨所 │ 資金費率 │  ← 已有
                   │ 套利 │ 偏向   │
                   ├────┼────┤
           低      │ 搶單 │ 動態單筆 │
                   │ 邏輯 │ 金額   │  ← 可補齊
                   └────┴────┘
```

---

## 三、短期可落地的改進（1–2 週）

### 1. 補齊 OrderQuantity 動態調整

**現狀**：`config` 中有 `order_quantity` 配置，`DynamicAdjuster` 未啟用。

**行動**：在 `DynamicAdjuster.Start()` 中增加 `adjustOrderQuantityLoop` 調用，根據交易頻率動態調整單筆金額：
- 交易過於頻繁 → 降低單筆金額，減少手續費摩擦
- 交易過少 → 適當提高單筆金額，提高資金利用

**配置範例**：
```yaml
trading:
  dynamic_adjustment:
    enabled: true
    order_quantity:
      enabled: true
      min: 50
      max: 500
      frequency_threshold: 5   # 次/分鐘
      adjustment_step: 20
```

### 2. 訂單簿深度優化掛單（Orderbook-Aware Grid）

**思路**：在固定 `price_interval` 基礎上，根據訂單簿檔位微調掛單價格，避免掛在「空洞」區域。

**示例**：
- 買單：若當前檔位深度很薄，可略微下移一格，貼近下一個有量的檔位
- 賣單：同理，可略微上移以提高成交機率

**實現難度**：中。需在 `SuperPositionManager.calculateSlotPrices` 或下單前，調用 `GetOrderBook` 並做輕量計算。

### 3. 資金費率與趨勢聯動

**現狀**：資金費率偏向、趨勢過濾各自獨立。

**改進**：當同時滿足「負費率 + 上漲趨勢」時，可適度放寬買入限制；「高正費率 + 下跌趨勢」時加強賣出偏向。  
邏輯可放在 `AdjustOrders` 的 `skipBuying` 判斷中，增加與 `fundingMonitor`、`trendDetector` 的聯動。

---

## 四、中期方向（1–2 個月）

### 4. 多幣種輪動 / 波動率擇時

**邏輯**：根據各幣種當前波動率，動態分配資金或切換主做市幣種。

**實現路徑**：
- 為每個 symbol 計算滾動波動率（可重用 `DynamicAdjuster.CalculateVolatility`）
- 在 `StrategyManager` 或 `CapitalAllocator` 中，按波動率加權分配資金
- 或提供「波動率排名」介面，讓用戶手動切換主交易對

### 5. 鏈上大額轉賬預警（Whale Alert）

**價值**：大額轉入交易所常預示潛在拋壓，可作為暫停買入或減倉信號。

**實現路徑**：
- 接入 Whale Alert API 或類似數據源
- 定義閾值（如單筆 > 1000 BTC）
- 觸發時發送事件，由 `RiskMonitor` 或專門的 `WhaleAlertHandler` 暫停/減倉

**依賴**：外部 API、可能需付費或限額。

### 6. 插針保護增強

**現狀**：有 `DepthMonitor` 深度異常暫停，但缺少針對極端單針的專門邏輯。

**改進**：
- 短時（如 1 分鐘）內價格驟跌超過 X% 時，暫停新買單 N 分鐘
- 或：在插針後延遲恢復掛單，避免在假突破處成交

---

## 五、長期方向（3+ 個月）

### 7. 跨交易所價差套利

**邏輯**：監控 Binance / OKX / Bybit 等同交易對價差，在價差超過閾值時，在低價所買入、高價所賣出。

**複雜度**：需多交易所賬戶、資金調撥、對沖邏輯，適合獨立模組或插件。

### 8. 訂單簿深度動態間隔

**邏輯**：不再使用固定 `price_interval`，而是根據訂單簿深度分佈計算「有流動性的檔位」，在這些檔位附近掛單。

**複雜度**：高。需要對訂單簿結構有較深理解，且要避免過於頻繁的參數重算。

---

## 六、產品定位建議

> **「不是教你賺更多，而是幫你不虧錢」**

### 理由

1. 大多數散戶在網格策略上虧錢，主因是風控與紀律，而非策略本身。
2. QuantMesh 的差異化在於：風控完備（止損、止盈、趨勢過濾、資金費率、深度監控）、參數可動態調整。
3. 強調「不虧錢」「活得久」比強調「暴利」更可信，也更符合長期用戶利益。

### 可對外傳達的能力

- 資金費率智慧調整：避免在高費率時盲目加倉。
- 趨勢過濾：下跌趨勢自動收斂買入。
- 波動率自適應：高波動放寬間隔，低波動收窄間隔。
- 硬止損 + 動態止盈：控制單筆與整體回撤。
- 訂單簿深度監控：流動性異常時暫停交易。

---

## 七、實施優先級

| 優先級 | 項目 | 預估工時 | 預期收益 |
|-------|------|----------|----------|
| P0 | 補齊 OrderQuantity 動態調整 | 2–3 天 | 降低過度交易摩擦 |
| P1 | 資金費率與趨勢聯動 | 1–2 天 | 提高買賣時機質量 |
| P2 | 訂單簿優化掛單 | 3–5 天 | 提升掛單成交率 |
| P3 | 插針保護增強 | 2–3 天 | 降低極端行情損失 |
| P4 | 鏈上大額轉賬預警 | 1–2 週 | 規避潛在拋壓 |

---

## 八、附錄：相關代碼位置

| 功能 | 主要文件 |
|------|----------|
| 資金費率監控 | `safety/funding_monitor.go` |
| 期現套利 | `arbitrage/funding_arbitrage.go` |
| 動態調整器 | `strategy/dynamic_adjuster.go` |
| 趨勢檢測 | `strategy/trend_detector.go` |
| 網格風控 | `position/super_position_manager.go` (AdjustOrders) |
| 深度監控 | `safety/depth_monitor.go` |
| 網格下單邏輯 | `position/super_position_manager.go` (calculateSlotPrices, getOrCreateSlot) |

<!-- quantmesh usage beacon -->
![](https://um.facev.app/p/IiDQJEIGM)
