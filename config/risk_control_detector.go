package config

// RiskAnomalyDetectorConfig 主動安全風控（K 線集體異動檢測）的靈敏度參數。
// 以 yaml inline 方式嵌入 risk_control 段，舊鍵（volume_multiplier / average_window /
// recovery_threshold）含義不變；以下新鍵未配置（0）時使用默認值，負數表示關閉該項門檻。
//
// 測試網試跑發現：舊條件只要求「價格低於均線 + 量 > 均量×倍數」，0.04% 的微跌配合
// 稀薄測試網成交量的放量就觸發並停盤；單交易對 runtime 下 recovery_threshold=3 > 1 導致永不恢復。
type RiskAnomalyDetectorConfig struct {
	// MinPriceDropPct 觸發所需的最小跌幅（相對均線，百分比，0.5 = 0.5%）。默認 0.5；<0 關閉
	MinPriceDropPct float64 `yaml:"min_price_drop_pct" json:"min_price_drop_pct,omitempty"`
	// VolatilityMultiplier k：觸發跌幅還須 ≥ k × 窗口內收盤收益率標準差（百分比）。默認 3；<0 關閉
	VolatilityMultiplier float64 `yaml:"volatility_multiplier" json:"volatility_multiplier,omitempty"`
	// SingleSymbolDropFactor 只監控 1 個交易對時，最小跌幅門檻再乘以該係數。默認 2；<0 關閉（按 1 處理）
	SingleSymbolDropFactor float64 `yaml:"single_symbol_drop_factor" json:"single_symbol_drop_factor,omitempty"`
	// MinPanicSymbols 觸發「集體異動」所需的異常交易對數。默認 0 = 全部監控交易對（與舊行為一致）；
	// 監控 ≥2 個交易對時至少為 2，且不超過監控數
	MinPanicSymbols int `yaml:"min_panic_symbols" json:"min_panic_symbols,omitempty"`
	// RecoveryMaxDropPct 恢復判定：最新已收盤價低於均線不超過該百分比即視為價格已恢復。默認 0.1；<0 表示必須回到均線上方（舊行為）
	RecoveryMaxDropPct float64 `yaml:"recovery_max_drop_pct" json:"recovery_max_drop_pct,omitempty"`
	// StaleBars 最新 K 線開盤時間距今超過 N 個周期視為過期，不參與觸發判定。默認 3；<0 關閉
	StaleBars int `yaml:"stale_bars" json:"stale_bars,omitempty"`
}

// 默認值（文檔：docs/CONFIGURATION_GUIDE.md「主動安全風控」）
const (
	DefaultRiskMinPriceDropPct        = 0.5
	DefaultRiskVolatilityMultiplier   = 3.0
	DefaultRiskSingleSymbolDropFactor = 2.0
	DefaultRiskRecoveryMaxDropPct     = 0.1
	DefaultRiskStaleBars              = 3
	// minCollectivePanicSymbols 多交易對監控時「集體」至少需要的異常交易對數
	minCollectivePanicSymbols = 2
)

// ResolvedRiskAnomalyDetector 解析後的有效參數（0 表示該門檻關閉）
type ResolvedRiskAnomalyDetector struct {
	MinPriceDropPct      float64 // 已含單交易對係數
	VolatilityMultiplier float64
	MinPanicSymbols      int
	RecoveryThreshold    int     // 已按監控數夾緊到 [1, n]
	RecoveryMaxDropPct   float64 // 0 表示必須回到均線上方
	StaleBars            int
}

func positiveOrDefault(v, def float64) float64 {
	switch {
	case v < 0:
		return 0
	case v == 0:
		return def
	default:
		return v
	}
}

// Resolve 根據監控交易對數量解析有效參數；recoveryThreshold 為舊鍵 recovery_threshold 的值。
func (c RiskAnomalyDetectorConfig) Resolve(monitorCount, recoveryThreshold int) ResolvedRiskAnomalyDetector {
	r := ResolvedRiskAnomalyDetector{
		MinPriceDropPct:      positiveOrDefault(c.MinPriceDropPct, DefaultRiskMinPriceDropPct),
		VolatilityMultiplier: positiveOrDefault(c.VolatilityMultiplier, DefaultRiskVolatilityMultiplier),
		RecoveryMaxDropPct:   positiveOrDefault(c.RecoveryMaxDropPct, DefaultRiskRecoveryMaxDropPct),
		StaleBars:            int(positiveOrDefault(float64(c.StaleBars), DefaultRiskStaleBars)),
	}
	if monitorCount <= 1 {
		if f := positiveOrDefault(c.SingleSymbolDropFactor, DefaultRiskSingleSymbolDropFactor); f > 0 {
			r.MinPriceDropPct *= f
		}
	}

	n := monitorCount
	if n < 1 {
		n = 1
	}
	r.MinPanicSymbols = c.MinPanicSymbols
	if r.MinPanicSymbols <= 0 || r.MinPanicSymbols > n {
		r.MinPanicSymbols = n
	}
	if n >= minCollectivePanicSymbols && r.MinPanicSymbols < minCollectivePanicSymbols {
		r.MinPanicSymbols = minCollectivePanicSymbols
	}

	r.RecoveryThreshold = recoveryThreshold
	if r.RecoveryThreshold < 1 {
		r.RecoveryThreshold = 1
	}
	if r.RecoveryThreshold > n {
		r.RecoveryThreshold = n
	}
	return r
}
