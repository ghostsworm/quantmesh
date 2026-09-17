package main

import (
	"time"

	"quantmesh/backtest/replay"
	"quantmesh/exchange"
)

// 功能注入（regime 檢測器、自適應間隔、上沿凍結、資金費定價）已移到 backtest/replay/features.go，
// 與 Web 回測任務（replay.RunGridTask）共用；這裡只保留工具側的薄封裝。

// regimeControlCheckInterval 見 replay.RegimeControlCheckInterval
const regimeControlCheckInterval = replay.RegimeControlCheckInterval

// installHooks 為需要 K 線 regime 或資金費監控的變體設置 cfg.Setup / cfg.OnTick。
// hourly 為包含段前歷史的 1h K 線（校準工具的 regime K 線周期固定為 regimeKlineInterval）。
func installHooks(cfg *replay.Config, symbol string, hourly []*exchange.Candle, funding []replay.FundingPoint) (*replay.FeatureStats, error) {
	// 序列之前（無已結算費率）按 0 處理：不偏移
	return replay.InstallFeatureHooks(cfg, replay.FeatureOptions{Symbol: symbol, RegimeKlines: hourly, FundingSeries: funding})
}

// advancePast 見 replay.AdvancePast
func advancePast(next, now time.Time, every time.Duration, first bool) time.Time {
	return replay.AdvancePast(next, now, every, first)
}
