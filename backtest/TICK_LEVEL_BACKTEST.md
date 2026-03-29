# Tick 級回測系統 (Tick-Level Backtesting System)

## 概述

QuantMesh 現在支持三種回測撮合模式，包括使用 Binance 真實 aggTrade 數據進行 tick 級撮合回測。

## 撮合模式 (Match Modes)

### 1. MatchModeRealTick (真實 Tick 撮合)
- **描述**: 使用 Binance aggTrade 真實成交數據進行訂單撮合
- **優點**:
  - 最接近真實市場環境
  - 考慮真實市場流動性
  - 精確的成交價格和時間
- **缺點**:
  - 需要下載大量 tick 數據
  - 回測速度較慢
- **適用場景**: 最終驗證策略、生產環境前測試

### 2. MatchModeSimulated (模擬 Tick 撮合)
- **描述**: 使用 K 線數據模擬 tick 價格走勢進行撮合
- **優點**:
  - 無需下載額外數據
  - 回測速度適中
  - 考慮 K 線內價格波動
- **缺點**:
  - 模擬的 tick 可能與真實市場有差異
- **適用場景**: 策略開發階段測試

### 3. MatchModeKlineOnly (僅 K 線撮合)
- **描述**: 直接使用 K 線收盤價成交
- **優點**:
  - 回測速度最快
  - 數據需求最小
- **缺點**:
  - 不夠精確，假設所有訂單都以收盤價成交
- **適用場景**: 快速策略篩選、大批量參數優化

## 數據下載

### 使用 BinanceDownloader 下載 aggTrade 數據

```go
package main

import (
    "time"
    "quantmesh/backtest"
)

func main() {
    // 創建下載器
    downloader := backtest.NewBinanceDownloader(
        "./data",  // 數據目錄
        "BTCUSDT", // 交易對
        "1m",      // K線週期（僅用於 K 線下載）
    )

    // 下載指定日期範圍的 aggTrade 數據
    start, _ := time.Parse("2006-01-02", "2024-01-01")
    end, _ := time.Parse("2006-01-02", "2024-01-31")

    err := downloader.DownloadRangeAggTrades(start, end)
    if err != nil {
        panic(err)
    }
}
```

### 檢查數據可用性

```go
// 檢查特定日期的數據是否可用
date, _ := time.Parse("2006-01-02", "2024-01-01")
available := downloader.CheckAggTradesAvailability(date)
fmt.Printf("Data available: %v\n", available)

// 獲取已下載數據的統計信息
info, err := downloader.GetAggTradesInfo()
if err != nil {
    panic(err)
}
fmt.Printf("Files: %d\n", len(info.Files))
fmt.Printf("Date Range: %s\n", info.DateRange)
fmt.Printf("Total Size: %.2f MB\n", info.TotalSizeMB)
```

## Tick 級回測使用示例

### 基本使用

```go
package main

import (
    "fmt"
    "time"
    "quantmesh/backtest"
    "quantmesh/exchange"
)

// 實現策略适配器
type MyStrategy struct {
    name string
}

func (s *MyStrategy) OnCandle(candle *exchange.Candle) backtest.Signal {
    // 實現策略邏輯
    // 返回買賣信號
    return backtest.Signal{
        Action: "",   // "buy", "sell", 或 ""
        Price:  0,    // 委託價格
    }
}

func (s *MyStrategy) GetName() string {
    return s.name
}

func main() {
    // 1. 加載 aggTrade 數據
    loader := backtest.NewAggTradeLoader("./data", "BTCUSDT")
    aggTrades, err := loader.LoadAggTradesFromCSV("./data/aggtrades/BTCUSDT/BTCUSDT-aggTrades-2024-01-01.csv")
    if err != nil {
        panic(err)
    }

    // 2. 加載 K 線數據（用於策略信號）
    candles := loadCandles() // 從文件或 API 加載

    // 3. 創建回測引擎（使用真實 tick 撮合）
    backtester := backtest.NewTickBacktester(
        "BTCUSDT",
        10000.0,                          // 初始資金
        backtest.MatchModeRealTick,       // 使用真實 tick 撮合
    )

    backtester.SetAggTrades(aggTrades)
    backtester.SetCandles(candles)
    backtester.SetStrategy(&MyStrategy{name: "my_strategy"})

    // 4. 配置回測參數
    backtester.SetFeeConfig(0.0004, 0.0002) // taker/maker 手續費
    backtester.SetSlippage(1.0)            // 1 基點滑點

    // 5. 執行回測
    result, err := backtester.Run()
    if err != nil {
        panic(err)
    }

    // 6. 查看結果
    fmt.Printf("策略: %s\n", result.Strategy)
    fmt.Printf("初始資金: %.2f\n", result.InitialCapital)
    fmt.Printf("最終資金: %.2f\n", result.FinalCapital)
    fmt.Printf("總收益: %.2f%%\n", result.Metrics.TotalReturn)
    fmt.Printf("夏普比率: %.2f\n", result.Metrics.SharpeRatio)
    fmt.Printf("最大回撤: %.2f%%\n", result.Metrics.MaxDrawdown)
}
```

### 使用不同撮合模式

```go
// 真實 tick 撮合（最精確）
backtester1 := backtest.NewTickBacktester("BTCUSDT", 10000.0, backtest.MatchModeRealTick)

// 模擬 tick 撮合（中等速度）
backtester2 := backtest.NewTickBacktester("BTCUSDT", 10000.0, backtest.MatchModeSimulated)

// 僅 K 線撮合（最快）
backtester3 := backtest.NewTickBacktester("BTCUSDT", 10000.0, backtest.MatchModeKlineOnly)
```

## AggTrade 數據格式

Binance aggTrade CSV 格式：
```
aggTradeId,price,quantity,firstTradeId,lastTradeId,timestamp,isBuyerMaker
123456789,43250.50,0.001,123456700,123456800,1704067200000,true
...
```

字段說明：
- `aggTradeId`: 聚合交易 ID
- `price`: 成交價格
- `quantity`: 成交數量
- `firstTradeId`: 該聚合交易的第一筆交易 ID
- `lastTradeId`: 該聚合交易的最後一筆交易 ID
- `timestamp`: 成交時間戳（毫秒）
- `isBuyerMaker`: true=主動賣，false=主動買

## RealTickMatcher 撮合邏輯

### 訂單成交條件

```go
// 買單：市場價 <= 訂單價時成交
if order.Side == "buy" {
    return aggTrade.Price <= order.Price
}

// 賣單：市場價 >= 訂單價時成交
return aggTrade.Price >= order.Price
```

### 成交價格計算（含滑點）

```go
// 買單滑點向上（買得更貴）
if order.Side == "buy" {
    fillPrice = marketPrice * (1 + slippageBps/10000)
}
// 賣單滑點向下（賣得更便宜）
else {
    fillPrice = marketPrice * (2 - (1 + slippageBps/10000))
}
```

## 數據管理

### 目錄結構

```
data/
├── aggtrades/
│   └── BTCUSDT/
│       ├── BTCUSDT-aggTrades-2024-01-01.csv
│       ├── BTCUSDT-aggTrades-2024-01-02.csv
│       └── ...
├── klines/
│   └── BTCUSDT/
│       └── 1m/
│           ├── BTCUSDT-1m-2024-01.csv
│           └── ...
└── funding_rate/
    └── BTCUSDT/
        ├── BTCUSDT-fundingRate-2024-01.csv
        └── ...
```

### 數據統計

```go
// 獲取 aggTrade 統計信息
stats := loader.GetStats(aggTrades)

fmt.Printf("總成交筆數: %d\n", stats.TotalTrades)
fmt.Printf("總成交量: %.4f\n", stats.TotalVolume)
fmt.Printf("加權平均價: %.2f\n", stats.WeightedAvgPrice)
fmt.Printf("價格區間: %.2f - %.2f\n", stats.PriceRange.Min, stats.PriceRange.Max)
fmt.Printf("買盤量: %.4f\n", stats.BuyVolume)
fmt.Printf("賣盤量: %.4f\n", stats.SellVolume)
```

### K 線重採樣

可以從 aggTrade 數據重採樣生成 K 線：

```go
// 將 aggTrade 重採樣為 1 分鐘 K 線
klines := loader.ResampleToKline(aggTrades, time.Minute)

for _, k := range klines {
    fmt.Printf("%s O:%.2f H:%.2f L:%.2f C:%.2f V:%.4f\n",
        time.Unix(0, k.Timestamp).Format(time.RFC3339),
        k.Open, k.High, k.Low, k.Close, k.Volume)
}
```

## 性能建議

1. **數據緩存**: aggTrade 數據文件很大，建議下載後緩存到本地
2. **並行回測**: 可以使用 `RealTickMatcher.Clone()` 創建多個實例並行回測不同參數
3. **增量更新**: 每天增量下載最新數據，避免重複下載
4. **數據壓縮**: 使用 gzip 壓縮存儲歷史數據

## 注意事項

1. **數據量**: aggTrade 數據量非常大，單日可能有數十萬到數百萬筆交易
2. **內存使用**: 加載大量 tick 數據會消耗較多內存，建議分批處理
3. **回測時間**: 真實 tick 回測比 K 線回測慢 10-100 倍
4. **數據延遲**: Binance data.binance.vision 的數據通常有 1-2 天延遲

## 未來擴展

- 支持更多交易所的 tick 數據
- 實時 tick 數據流處理
- Tick 級風險管理
- 多策略組合回測
- 回測結果可視化

<!-- quantmesh usage beacon -->
![](https://um.facev.app/p/IiDQJEIGM)
