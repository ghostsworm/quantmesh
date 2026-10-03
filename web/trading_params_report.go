package web

import "quantmesh/config"

// Codes, not raw provider errors, cross the public API boundary.
type TradingParamsUpdateReport struct {
	Verified   bool              `json:"verified"`
	Applied    []string          `json:"applied"`
	Failed     map[string]string `json:"failed"`
	NotRunning []string          `json:"not_running"`
}

type TradingParamsReportUpdater interface {
	UpdateTradingParamsWithReport(*config.Config) TradingParamsUpdateReport
}

func applyTradingParamsWithReport(cfg *config.Config) TradingParamsUpdateReport {
	if updater, ok := symbolManagerProvider.(TradingParamsReportUpdater); ok {
		return updater.UpdateTradingParamsWithReport(cfg)
	}
	report := TradingParamsUpdateReport{Applied: []string{}, Failed: map[string]string{}, NotRunning: []string{}}
	// Retain legacy dispatch, but IDs alone cannot prove absence of failures.
	if updater, ok := symbolManagerProvider.(TradingParamsUpdater); ok {
		updater.UpdateTradingParams(cfg)
	}
	return report
}
