package config

import "strings"

// DefaultBinanceVisionDir is the gitignored directory of checksum-verified
// data.binance.vision USD-M 1m files used by grid replay.
const DefaultBinanceVisionDir = "data/kline/binance_vision"

// ApplyBacktestDataDefaults fills the verified-data directory when unset.
func ApplyBacktestDataDefaults(cfg *Config) {
	if cfg == nil {
		return
	}
	if strings.TrimSpace(cfg.BacktestData.BinanceVisionDir) == "" {
		cfg.BacktestData.BinanceVisionDir = DefaultBinanceVisionDir
	}
}
