package web

// FundingCarryRuntimeStatus separates a managed runtime from its trading loop.
// Remaining quantities are historical accounting, never spendable inventory.
type FundingCarryRuntimeStatus struct {
	Type                      string      `json:"type"`
	TradingRunning            bool        `json:"trading_running"`
	ReconciliationRequired    bool        `json:"reconciliation_required"`
	ReconciliationReasons     []string    `json:"reconciliation_reasons"`
	Direction                 string      `json:"direction"`
	SpotQty                   float64     `json:"spot_qty"`
	FuturesQty                float64     `json:"futures_qty"`
	MarginDebt                float64     `json:"margin_debt"`
	MarginDebtBasis           string      `json:"margin_debt_basis"`
	MarginBorrowTransferID    int64       `json:"margin_borrow_transfer_id"`
	MarginBorrowedAt          string      `json:"margin_borrowed_at"`
	MarginCoverRemainingQty   interface{} `json:"margin_cover_remaining_qty"`
	MarginCoverRemainingKnown bool        `json:"margin_cover_remaining_known"`
	MarginCoverRemainingBasis string      `json:"margin_cover_remaining_basis"`
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
