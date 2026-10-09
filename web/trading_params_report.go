package web

import (
	"context"
	"quantmesh/config"
	"reflect"
)

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

// The guard must run after the runtime's lifecycle lock is acquired, not merely
// before dispatch, since a newer save can complete while application is waiting.
type TradingParamsGuardedReportUpdater interface {
	UpdateTradingParamsWithGuardedReport(*config.Config, func() bool) TradingParamsUpdateReport
}

type TradingParamsContextReportUpdater interface {
	UpdateTradingParamsWithContext(context.Context, *config.Config, func() bool) TradingParamsUpdateReport
}

func applyTradingParamsWithReport(cfg *config.Config) TradingParamsUpdateReport {
	return applyTradingParamsWithContext(context.Background(), cfg)
}

func applyTradingParamsWithContext(ctx context.Context, cfg *config.Config) TradingParamsUpdateReport {
	if updater, ok := symbolManagerProvider.(TradingParamsContextReportUpdater); ok {
		return updater.UpdateTradingParamsWithContext(ctx, cfg, func() bool {
			current, err := GetLatestConfig()
			return err == nil && current != nil && reflect.DeepEqual(current, cfg)
		})
	}
	if updater, ok := symbolManagerProvider.(TradingParamsGuardedReportUpdater); ok {
		return updater.UpdateTradingParamsWithGuardedReport(cfg, func() bool {
			current, err := GetLatestConfig()
			return err == nil && current != nil && reflect.DeepEqual(current, cfg)
		})
	}
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
