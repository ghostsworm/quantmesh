package main

import (
	"quantmesh/config"
	"quantmesh/logger"
)

// The legacy result contains only runtimes whose application succeeded, not
// every saved configuration. Per-Bot error propagation to HTTP remains separate.
func (bm *BotManager) UpdateRuntimeTradingParams(latestCfg *config.Config) (updatedBotIDs []string) {
	if latestCfg == nil {
		return nil
	}
	bm.registerEquityScopeConfig(latestCfg)
	for _, botCfg := range latestCfg.Bots {
		botID := config.BotIDOrGenerate(botCfg)
		bm.runtimesMu.RLock()
		br := bm.runtimes[botID]
		bm.runtimesMu.RUnlock()
		if br == nil || br.Inner == nil {
			continue
		}
		if err := br.applyRuntimeTradingParams(botCfg); err != nil {
			logger.Error("[%s] 运行时热参数应用失败，未发布交易参数", botID)
			continue
		}
		updatedBotIDs = append(updatedBotIDs, botID)
	}
	return
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
