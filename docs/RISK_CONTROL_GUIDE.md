# 風控系統指南

本指南說明 QuantMesh 的風控系統：主動市場風控監控、帳戶安全檢查、對帳與訂單清理。

## 組成

| 組件 | 配置區塊 | 用途 |
|-----------|-----------------|---------|
| RiskMonitor | `risk_control` | K 線成交量異常偵測；觸發時暫停交易 |
| 帳戶安全 | 啟動時／配置 | 交易前檢查餘額、槓桿與持倉安全 |
| Reconciler | `trading.reconcile_interval` | 同步本地與交易所（訂單、持倉） |
| 訂單清理 | `trading.order_cleanup_*` | 移除過多或過期訂單 |
| 深度監控 | `risk_control.depth_monitor` | 訂單簿深度監控（可選） |

---

## 1. 主動風控監控（K 線成交量）

### 角色

監控指定交易對的 K 線成交量。若當前成交量明顯高於近期平均（如暴量或異常），可暫停交易直至市場恢復正常。

### 配置

```yaml
risk_control:
  enabled: true
  monitor_symbols:
    - BTCUSDT
    - ETHUSDT
    - SOLUSDT
  interval: 1m              # K 線週期
  volume_multiplier: 3       # 觸發條件：成交量 > 平均 × 此倍數（如 3 倍）
  average_window: 20         # 用於計算平均的完結 K 線數量
  recovery_threshold: 3      # 至少幾個交易對「健康」才解除觸發（相對 monitor_symbols 數量）
  max_leverage: 3             # 最大允許槓桿（0 表示不限制）
  depth_monitor:
    enabled: false
    check_interval: 5
    depth_levels: 10
    drop_threshold: 0.5
    recovery_threshold: 0.7
    min_depth_usdt: 10000
```

### 行為

- **觸發**：任一監控交易對的當前（或最近一根完結）K 線成交量 > `average_window` 平均成交量 × `volume_multiplier` 時，該交易對標記為不健康。
- **暫停**：若健康交易對數少於 `recovery_threshold`，風控處於「已觸發」狀態，暫停交易（不下新單）。
- **恢復**：當至少 `recovery_threshold` 個交易對恢復健康時，解除觸發並恢復交易。

### 整合

- 實作於 [`safety/risk_monitor.go`](safety/risk_monitor.go)。
- 可與新聞驅動風控（如 NewsMonitor）搭配使用。

### API

- `GET /api/risk/status`：當前風控狀態（觸發／已恢復）與訊息。
- `GET /api/risk/monitor`：風控監控資料（如各交易對健康狀態）。
- `GET /api/risk/history`：風控檢查歷史。

---

## 2. 帳戶安全（啟動時）

交易開始前，系統可檢查：

- 帳戶餘額（是否足夠策略使用）。
- 槓桿（如不超過 `max_leverage`）。
- 持倉安全（如最少持倉數或類似規則）。

實作於 [`safety/safety.go`](safety/safety.go)。若帳戶已有持倉，部分檢查可能略過（視為使用者已知風險）。

---

## 3. 對帳

### 角色

週期性將本地訂單與持倉與交易所對齊，避免本地狀態偏離（例如手動撤單或外部成交後）。

### 配置

```yaml
trading:
  reconcile_interval: 60   # 對帳執行間隔（秒）
```

### 行為

- 從交易所取得未成交訂單與持倉。
- 更新本地狀態；必要時撤單或調整本地視圖。
- 有助於維持網格槽位與訂單數量正確。

### API

- `GET /api/reconciliation/status`：當前對帳狀態。
- `GET /api/reconciliation/history`：對帳歷史。
- `GET /api/reconciliation/aggregated`：對帳彙總資料。

---

## 4. 訂單清理

### 角色

當未成交訂單數超過閾值時，分批撤單，避免訂單過多（如過舊或冗餘訂單）。

### 配置

```yaml
trading:
  order_cleanup_threshold: 50   # 超過此數量即觸發清理
  cleanup_batch_size: 20         # 每次清理撤單數量
```

`timing.order_cleanup_interval` 控制清理任務執行頻率（預設 60 秒）。

### 行為

- 若該交易對（或全域）未成交訂單數超過 `order_cleanup_threshold`，系統會撤銷最多 `cleanup_batch_size` 筆訂單（最舊或依策略規則）。
- 實作於 [`safety/order_cleaner.go`](safety/order_cleaner.go)。

---

## 5. 深度監控（可選）

在 `risk_control.depth_monitor` 可啟用訂單簿深度檢查：

- 若深度（如前 N 檔 USDT 合計）低於閾值，可觸發風控。
- 深度恢復高於 `recovery_threshold` 時解除風控。

適用於希望在訂單簿過薄時暫停或限制交易的情境。

---

## 6. 多交易對與恢復

- **monitor_symbols**：每次 K 線更新都會檢查所列交易對；任一交易對都可計入「不健康」。
- **recovery_threshold**：須有幾個交易對健康才解除觸發。例：5 個交易對、閾值 3 → 至少 3 個健康才恢復。
- **新聞驅動風控**：若啟用 NewsMonitor 等，風控也可由新聞／AI 信號觸發；配置見新聞與 AI 文檔。

---

## 摘要

| 目標 | 配置／組件 |
|------|---------------------|
| 成交量暴量時暫停 | `risk_control` ＋ RiskMonitor |
| 限制槓桿 | `risk_control.max_leverage`、帳戶安全檢查 |
| 與交易所狀態同步 | `trading.reconcile_interval`、Reconciler |
| 限制未成交訂單數 | `order_cleanup_threshold`、`cleanup_batch_size`、訂單清理 |
| 訂單簿過薄時暫停 | `risk_control.depth_monitor` |

配置冗餘與遷移見 [CONFIGURATION_REDUNDANCY_AND_MIGRATION.md](CONFIGURATION_REDUNDANCY_AND_MIGRATION.md)；完整配置見 [CONFIGURATION_GUIDE.md](CONFIGURATION_GUIDE.md)。
