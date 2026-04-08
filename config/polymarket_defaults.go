package config

import "strings"

// Gamma / Polymarket 相關缺省（與 config.example.yaml、web ApplyDataSourcePolymarketConfig 對齊）。
const (
	// DefaultGammaAPIURL Polymarket Gamma REST 根地址（無需 token）。
	DefaultGammaAPIURL = "https://gamma-api.polymarket.com"
	// DefaultPolymarketAnalysisIntervalSec LLM 綜合信號輪詢間隔（秒）；0 表示未設置，由 ApplyGammaRelatedDefaults 填此值。
	DefaultPolymarketAnalysisIntervalSec = 300
)

// ApplyGammaRelatedDefaults 在從 JSON/YAML 加載後補齊零值，避免舊 app_config 快照缺少新字段時 Gamma URL、間隔為空。
// 僅在「明顯未設置」時填寫：空字串、非正間隔；不覆蓋用戶顯式寫入的 0（流動性等篩選仍表示不限制）。
func ApplyGammaRelatedDefaults(cfg *Config) {
	if cfg == nil {
		return
	}
	if strings.TrimSpace(cfg.MacroEvent.GammaAPIURL) == "" {
		cfg.MacroEvent.GammaAPIURL = DefaultGammaAPIURL
	}
	if cfg.MacroEvent.Enabled && cfg.MacroEvent.FetchInterval <= 0 {
		cfg.MacroEvent.FetchInterval = 300
	}
	ps := &cfg.AI.Modules.PolymarketSignal
	if strings.TrimSpace(ps.APIURL) == "" {
		ps.APIURL = DefaultGammaAPIURL
	}
	if ps.AnalysisInterval <= 0 {
		ps.AnalysisInterval = DefaultPolymarketAnalysisIntervalSec
	}
	sg := &ps.SignalGeneration
	if sg.BuyThreshold == 0 && sg.SellThreshold == 0 && sg.MinSignalStrength == 0 && sg.MinConfidence == 0 {
		sg.BuyThreshold = 0.65
		sg.SellThreshold = 0.35
		sg.MinSignalStrength = 0.3
		sg.MinConfidence = 0.5
	}
}
