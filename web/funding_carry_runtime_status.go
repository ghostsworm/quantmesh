package web

// FundingCarryRuntimeStatus separates a managed runtime from its trading loop.
// Remaining quantities are historical accounting, never spendable inventory.
type FundingCarryRuntimeStatus struct {
	TradingRunning         bool `json:"trading_running"`
	ReconciliationRequired bool `json:"reconciliation_required"`
}

func fundingCarryDashboardStatus(bot BotResponse) string {
	if !bot.Running {
		return "stopped"
	}
	if bot.FundingCarryRuntime == nil {
		return "unknown"
	}
	if bot.FundingCarryRuntime.ReconciliationRequired {
		return "reconciliation_required"
	}
	if bot.FundingCarryRuntime.TradingRunning {
		return "running"
	}
	return "stopped"
}
