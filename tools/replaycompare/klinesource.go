package main

import (
	"time"

	"quantmesh/backtest/replay"
	"quantmesh/exchange"
)

// K 線聚合與無未來函數 K 線源已移到 backtest/replay（Web 回測任務共用），這裡保留原名供工具內部與測試使用。

// ClosedKlineSource 見 replay.ClosedKlineSource
type ClosedKlineSource = replay.ClosedKlineSource

// AggregateCandles 見 replay.AggregateCandles
func AggregateCandles(candles []*exchange.Candle, intervalMs int64) ([]*exchange.Candle, error) {
	return replay.AggregateCandles(candles, intervalMs)
}

// NewClosedKlineSource 見 replay.NewClosedKlineSource
func NewClosedKlineSource(symbol, interval string, bars []*exchange.Candle, now func() time.Time) (*ClosedKlineSource, error) {
	return replay.NewClosedKlineSource(symbol, interval, bars, now)
}
