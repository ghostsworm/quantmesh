# 現貨交易指南

QuantMesh 同時支援**合約**（U 本位）與**現貨**市場。本指南說明如何啟用與使用現貨交易。

## 現貨與合約差異

| 項目 | 現貨 | 合約 |
|--------|------|---------|
| 槓桿 | 無（1x） | 可配置（如 1x–20x） |
| 做空 | 否（僅能賣出持倉） | 是 |
| 資金費率 | 不適用 | 有（週期性資金費率） |
| ReduceOnly | 不適用 | 合約支援 |
| 常見用途 | 買入持有、網格低買高賣 | 方向性與槓桿網格 |

## 為交易對啟用現貨

在該交易對的配置中將 `market_type` 設為 `spot`。多交易對使用 `trading.symbols[].market_type`。

### 範例：單一交易對（舊版寫法）

若使用舊版單一交易對配置，請設定：

```yaml
trading:
  symbol: BTCUSDT
  market_type: spot   # 使用現貨市場（預設為合約）
  price_interval: 150
  order_quantity: 150
  # ...
```

### 範例：多交易對（建議）

```yaml
trading:
  symbols:
    - enabled: true
      exchange: binance
      symbol: BTCUSDT
      market_type: spot    # 此交易對為現貨
      total_allocated_capital: 5000
      strategies:
        - type: grid
          weight: 1.0
          config: {}
      price_interval: 150
      order_quantity: 150
      # ...
    - enabled: true
      exchange: binance
      symbol: ETHUSDT
      market_type: futures # 此交易對為合約
      # ...
```

- 未填 `market_type` 時預設為 `futures`（向後相容）。
- 合法值：`spot`、`futures`。

## 支援的交易所

凡適配器實作現貨 API 的交易所皆可（如 Binance 現貨、Gate 現貨）。同一適配器可能依交易對與端點同時支援現貨與合約。

- **Binance**：現貨與 USDT 本位合約；依交易對設定 `market_type`。
- **Gate、OKX、Bybit 等**：依各適配器現貨／合約支援情況設定 `market_type`。

請參考各交易所適配器與文檔確認符號格式（如現貨與合約是否皆為 `BTCUSDT`）。

## 現貨注意事項

1. **無槓桿**：現貨恆為 1x；交易所配置中的 `leverage` 對現貨交易對無效。
2. **無 ReduceOnly**：現貨為一般買賣；ReduceOnly 為合約概念，現貨不使用。
3. **持倉意義**：現貨「持倉」為淨基礎資產餘額（買入減賣出），非獨立合約倉位。
4. **資金費率**：資金費率及相關功能（如 P1 趨勢聯動）僅作用於合約交易對；現貨會略過。

## 策略相容性

- **網格**：適用現貨（低買高賣；無做空）。
- **DCA／馬丁格爾／均值回歸／動量／趨勢跟蹤**：一般同時支援現貨與合約；行為可能不同（如現貨無做空）。

## 配置檢查清單

- [ ] 在 `trading.symbols`（或舊版單一交易對的 `trading`）中為現貨交易對設定 `market_type: spot`。
- [ ] 使用在該交易對上支援現貨的交易所。
- [ ] 現貨交易對勿依賴資金費率或 ReduceOnly 邏輯。
- [ ] 確認 `price_interval`、`order_quantity` 符合現貨流動性與手續費。

多交易對與驗證說明見 [CONFIGURATION_GUIDE.md](../../CONFIGURATION_GUIDE.md)、[CONFIGURATION_REDUNDANCY_AND_MIGRATION.md](../../CONFIGURATION_REDUNDANCY_AND_MIGRATION.md)。

<!-- quantmesh usage beacon -->
![](https://um.facev.app/p/IiDQJEIGM)
