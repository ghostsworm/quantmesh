package main

import "quantmesh/web"

func attachFundingCarryRuntimeStatus(inner *SymbolRuntime, resp *web.BotResponse) {
	if inner == nil || inner.StrategyManager == nil || resp == nil {
		return
	}
	statuses := inner.StrategyManager.GetAllStrategyStatus()
	// The dedicated runtime must contain exactly one Funding Carry strategy.
	if len(statuses) != 1 || statuses[0].Type != "funding_carry" {
		return
	}
	status := statuses[0]
	required, known := status.VisualizationData["reconciliation_required"].(bool)
	if !known {
		return
	}
	resp.FundingCarryRuntime = &web.FundingCarryRuntimeStatus{
		TradingRunning: status.IsRunning, ReconciliationRequired: required,
	}
}
