package main

import (
	"context"
	"errors"
	"quantmesh/config"
	"quantmesh/logger"
	"quantmesh/web"
)

// The legacy result contains only runtimes whose application succeeded, not
// every saved configuration. Per-Bot error propagation to HTTP remains separate.
func (bm *BotManager) UpdateRuntimeTradingParams(latestCfg *config.Config) (updatedBotIDs []string) {
	return bm.UpdateRuntimeTradingParamsWithReport(latestCfg).Applied
}

func (bm *BotManager) UpdateRuntimeTradingParamsWithReport(latestCfg *config.Config) web.TradingParamsUpdateReport {
	return bm.UpdateRuntimeTradingParamsWithGuardedReport(latestCfg, nil)
}

// Internal direct callers retain their existing semantics. Persisted HTTP
// callers supply a freshness check executed within each Bot lifecycle lock.
func (bm *BotManager) UpdateRuntimeTradingParamsWithGuardedReport(latestCfg *config.Config, current func() bool) web.TradingParamsUpdateReport {
	return bm.UpdateRuntimeTradingParamsWithContext(context.Background(), latestCfg, current)
}

func (bm *BotManager) UpdateRuntimeTradingParamsWithContext(ctx context.Context, latestCfg *config.Config, current func() bool) web.TradingParamsUpdateReport {
	report := web.TradingParamsUpdateReport{Applied: []string{}, Failed: map[string]string{}, NotRunning: []string{}}
	if latestCfg == nil {
		return report
	}
	report.Verified = true
	if ctx == nil || ctx.Err() != nil {
		for _, botCfg := range latestCfg.Bots {
			report.Failed[config.BotIDOrGenerate(botCfg)] = "runtime_application_cancelled"
		}
		return report
	}
	if current != nil && !current() {
		for _, botCfg := range latestCfg.Bots {
			report.Failed[config.BotIDOrGenerate(botCfg)] = "runtime_configuration_changed"
		}
		return report
	}
	bm.registerEquityScopeConfig(latestCfg)
	for _, botCfg := range latestCfg.Bots {
		botID := config.BotIDOrGenerate(botCfg)
		err := bm.WithBotStrategyConfigurationContext(ctx, botID, func(bool) error {
			if current != nil && !current() {
				report.Failed[botID] = "runtime_configuration_changed"
				return nil
			}
			bm.runtimesMu.RLock()
			br := bm.runtimes[botID]
			bm.runtimesMu.RUnlock()
			if br == nil {
				report.NotRunning = append(report.NotRunning, botID)
				return nil
			}
			if br.stopPersistencePending.Load() {
				report.Failed[botID] = "bot_stop_persistence_pending"
				return nil
			}
			if br.stopDrainPending.Load() {
				report.Failed[botID] = "bot_stop_drain_pending"
				return nil
			}
			if br.stopOwnershipPending.Load() {
				report.Failed[botID] = "bot_stop_ownership_pending"
				return nil
			}
			if br.stopVerificationPending.Load() {
				report.Failed[botID] = "bot_stop_verification_pending"
				return nil
			}
			if br.stopCleanupPending.Load() {
				report.Failed[botID] = "bot_stop_cleanup_pending"
				return nil
			}
			if br.stopReconciliationPending.Load() {
				report.Failed[botID] = "bot_stop_reconciliation_pending"
				return nil
			}
			if br.Inner == nil {
				report.Failed[botID] = "runtime_uninitialized"
				return nil
			}
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if err := br.applyRuntimeTradingParamsWithContext(ctx, botCfg); err != nil {
				if ctx.Err() != nil {
					return ctx.Err()
				}
				report.Failed[botID] = "risk_apply_failed"
				if errors.Is(err, errRuntimeConfigurationRequiresRestart) {
					report.Failed[botID] = "runtime_restart_required"
				}
				logger.Error("[%s] 运行时热参数应用失败，未发布交易参数", botID)
				return nil
			}
			report.Applied = append(report.Applied, botID)
			return nil
		})
		if err != nil {
			report.Failed[botID] = "runtime_lifecycle_unavailable"
			if ctx.Err() != nil {
				report.Failed[botID] = "runtime_application_cancelled"
			}
		}
	}
	return report
}

// Validate and apply risk before publishing dependent trading parameters.
// Specialized runtimes use their UpdateOpenControl callback, without an SPM.
func (br *BotRuntime) applyRuntimeTradingParams(botCfg config.BotConfig) error {
	return br.applyRuntimeTradingParamsWithContext(context.Background(), botCfg)
}

func (br *BotRuntime) applyRuntimeTradingParamsWithContext(ctx context.Context, botCfg config.BotConfig) error {
	br.configMu.Lock()
	defer br.configMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	previous := br.Config
	if !runtimeHotContractMatches(previous, botCfg) {
		return errRuntimeConfigurationRequiresRestart
	}
	if br.Inner.SuperPositionManager == nil &&
		(previous.PriceInterval != botCfg.PriceInterval || previous.ProfitSpread != botCfg.ProfitSpread ||
			previous.OrderQuantity != botCfg.OrderQuantity || previous.BuyWindowSize != botCfg.BuyWindowSize ||
			previous.SellWindowSize != botCfg.SellWindowSize || previous.GridRiskControl != botCfg.GridRiskControl) {
		// Specialized callbacks apply opening controls, not SPM grid parameters.
		return errRuntimeConfigurationRequiresRestart
	}
	br.Config = runtimeHotCandidate(previous, botCfg)
	if err := br.publishRiskControlsLocked(); err != nil {
		br.Config = previous
		return err
	}
	symCfg := config.BotConfigToSymbolConfig(br.Config)
	if spm := br.Inner.SuperPositionManager; spm != nil {
		spm.UpdateTradingParams(symCfg.PriceInterval, symCfg.ProfitSpread, symCfg.OrderQuantity, symCfg.BuyWindowSize, symCfg.SellWindowSize)
		spm.SetSpotInventoryPolicy(symCfg.SpotInventoryPolicy)
	}
	br.Inner.Config = symCfg
	return nil
}
