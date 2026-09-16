# ADR 2026-09-17：K 線 regime / 自適應間隔 / 邊界冻结 / 庫存偏斜 / 資金費定價接入網格

- 狀態：已接受（R5 第二段，全部默認關閉）
- 關聯：`docs/audits/2026-09-17-full-audit.md` 第五節第 4、5、6、7、8 條；`docs/decisions/2026-09-17-kline-regime-filter.md`
- 代碼：`position/adjust_regime.go`、`position/adjust_plan.go`、`position/inventory_skew.go`、`position/funding_pricing.go`、
  `symbol_manager_regime.go`、`config/grid_regime.go`、`safety/funding_monitor.go`

## 決策

### 1. 配置：鏡像而非嵌入

`strategy/regime → exchange → config` 已存在依賴，`config` 嵌入 `regime.RegimeConfig` 會形成循環。
因此 `config.RegimeFilterConfig` / `config.AdaptiveIntervalConfig` 逐字段鏡像（yaml 標籤一致），
`position.RegimeConfigFromConfig` 用 Go 結構體類型轉換（字段不一致即編譯失敗），並有反射測試校驗 yaml 標籤。
默認值仍由 `regime.*.WithDefaults()` 唯一定義；`regime.NewDetector` 在 Bot 啟動早期校驗，非法配置拒絕啟動。

`config/config.go` 已超過 3000 行，本輪把交易風控/開倉控制類型原樣移到 `config/trading_controls.go`（無行為變化）。

### 2. 每 tick 的開倉計劃（`planOpeningLeg`，持有 `spm.mu`）

按腿（LONG/SHORT；BOTH 分兩腿）依次合併：

1. regime：`Effective() != Unknown` 且 `regime_filter.enabled` 時應用 `PolicyFor`：窗口 `max(1, floor(w × EntryWindowScale))`，記錄 `FreezeFavorableBound`。
2. 庫存偏斜：窗口 × `(1 − inv×s)`（≤0 停止開倉，否則至少 1 檔），開倉價外移 `inv×s×間隔`，平倉利差係數 `1 − 0.5×inv×s`。
3. 資金費：付費方開倉價外移、平倉價外移 `rate×price×h/8`（封頂 0.5×間隔）；結算前 N 分鐘暫停付費方開倉。
4. 開倉價總外移封頂 0.9×間隔（不越過相鄰槽位）。

開倉委託價外移時 **槽位鍵不變**（ClientOrderID 仍編碼槽位價），成交後 `AvgBuyPrice` 記錄實際成交價；
LONG 平倉基準取 `max(槽位, 均價)`，因此外移只會增加每格利潤。偏斜後的平倉利差取 `max(槽位利差×係數, 手續費下界)` 且不超過未偏斜值。

### 3. 與 ADR 默認表的偏差：Unknown 保持原有行為

原 ADR 建議 Unknown 時窗口減半並冻结邊界。接入時按整改要求改為 **Unknown/過期 → 原有行為**（不縮窗、不冻结、間隔回到 base），
理由：啟用 regime 不應在 K 線預熱（默認 114 根）或數據源故障期間悄悄改變已驗證的網格行為；K 線源故障另有日誌告警。

### 4. 順勢邊界冻结

進入冻结時記錄當時開倉窗口的邊界（LONG 取最高槽位、SHORT 取最低槽位），此後過濾越界的開倉槽位，直到有效狀態離開順勢（含變為 Unknown）。
平倉單不受影響。`upper_bound_freeze.enabled` 另提供 ATR 自動邊界 `EMA ± k×ATR`，與手動 `price_high/price_low` 取更嚴格者並沿用原軟限制語義。

### 5. 間隔：後台循環 + UpdateTradingParams

AdjustOrders 持有 `spm.mu`，不能在其中調用加鎖的 `UpdateTradingParams`；且間隔只應隨已收盤 K 線變化。
因此 `RunRegimeControlLoop` 每 30s（或 Detector `OnChange` 通知）讀快照，**僅當 BarOpenTime 或有效狀態變化** 時計算：

```
adaptive = Adaptive.Next(adaptiveCurrent, ATR, base)      // adaptiveCurrent 只保存未放大的值（防自激）
target   = QuantizeInterval(adaptive × IntervalScale, base) // BOTH 取兩腿較大倍數
```

- `profit_spread > 0` 時按 `target/base` 等比縮放，保持「平倉價 = 相鄰槽位」的網格語義。
- 外部（Web 熱更新、動態調整器）改了間隔 → 以新值為 base；`dynamic_adjustment.price_interval` 同時啟用時停用自適應，避免互相覆蓋。
- 等比/三級火箭網格不支持間隔調整（窗口與冻结仍生效）。

鎖順序：`spm.mu → regimeControl.mu`；間隔循環在不持有 `spm.mu` 時調用 `UpdateTradingParams`。

### 6. 舊趨勢過濾退役路徑

`regime_filter.enabled` 時：不再為趨勢過濾創建 `strategy.TrendDetector`（smart_position 仍會創建），AdjustOrders 跳過舊判斷，
資金費-趨勢聯動改用 regime 趨勢。未啟用 regime 但開了 `trend_filter_enabled` 時啟動日誌提示廢棄。

### 7. 資金費結算時間

交易所接口只返回費率；`FundingRateMonitor.fetchFundingRate` 在下次結算時間未設置或已過期時按 UTC 00/08/16 估算
（`EstimateNextFundingTime`）。非 8 小時結算的品種會估算偏差，影響偏移大小與暫停窗口；未知時按整周期 8 小時計（偏保守）。

### 8. bot_manager 費率拉取不刪除

`BotManager.applyExchangeFeeFromAPIForBot` 寫全局 `exchanges.<ex>.fee_rate`（taker），被持倉安全檢查、profile 切換規則、
Web 參數建議使用；`applyGridFeeRates` 只注入倉位管理器且以前者為回退。兩者職責不同，保留並在代碼中注明。

## 後果

- 全部默認關閉，未開啟時 AdjustOrders 行為與 R5 第一段一致（已有測試全部通過）。
- 參數缺乏與實盤同構的回測驗證（R6），開啟需小倉位觀察。
- 已知限制：配置只讀全局 `trading.*`，Bot 級覆蓋未支持；庫存偏斜的平倉利差縮小在 ReduceOnly 回執解析失敗的兜底路徑中未考慮（僅影響日誌中的槽位反推）。
