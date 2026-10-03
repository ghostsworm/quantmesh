package main

import (
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
	report := web.TradingParamsUpdateReport{Applied: []string{}, Failed: map[string]string{}, NotRunning: []string{}}
	if latestCfg == nil {
		return report
	}
	report.Verified = true
	bm.registerEquityScopeConfig(latestCfg)
	for _, botCfg := range latestCfg.Bots {
		botID := config.BotIDOrGenerate(botCfg)
		bm.runtimesMu.RLock()
		br := bm.runtimes[botID]
		bm.runtimesMu.RUnlock()
		if br == nil {
			report.NotRunning = append(report.NotRunning, botID)
			continue
		}
		if br.Inner == nil {
			report.Failed[botID] = "runtime_uninitialized"
			continue
		}
		if err := br.applyRuntimeTradingParams(botCfg); err != nil {
			report.Failed[botID] = "risk_apply_failed"
			logger.Error("[%s] 运行时热参数应用失败，未发布交易参数", botID)
			continue
		}
		report.Applied = append(report.Applied, botID)
	}
	return report
}

// Validate and apply risk before publishing dependent trading parameters.
// Specialized runtimes use their UpdateOpenControl callback, without an SPM.
func (br *BotRuntime) applyRuntimeTradingParams(botCfg config.BotConfig) error {
	br.configMu.Lock()
	defer br.configMu.Unlock()
	previous := br.Config
	br.Config = botCfg
	br.Config.OpenPositionControl = config.CloneOpenPositionControl(botCfg.OpenPositionControl)
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
