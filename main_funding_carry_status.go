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
	reasons, _ := status.VisualizationData["reconciliation_reasons"].([]string)
	direction, _ := status.VisualizationData["direction"].(string)
	spotQty, _ := status.VisualizationData["spot_qty"].(float64)
	futuresQty, _ := status.VisualizationData["futures_qty"].(float64)
	marginDebt, _ := status.VisualizationData["margin_debt"].(float64)
	marginDebtBasis, _ := status.VisualizationData["margin_debt_basis"].(string)
	borrowTransferID, _ := status.VisualizationData["margin_borrow_transfer_id"].(int64)
	borrowedAt, _ := status.VisualizationData["margin_borrowed_at"].(string)
	coverRemainingQty := status.VisualizationData["margin_cover_remaining_qty"]
	coverRemainingKnown, _ := status.VisualizationData["margin_cover_remaining_known"].(bool)
	coverRemainingBasis, _ := status.VisualizationData["margin_cover_remaining_basis"].(string)
	resp.FundingCarryRuntime = &web.FundingCarryRuntimeStatus{
		Type: "funding_carry", TradingRunning: status.IsRunning, ReconciliationRequired: required,
		ReconciliationReasons: append([]string(nil), reasons...), Direction: direction, SpotQty: spotQty,
		FuturesQty: futuresQty, MarginDebt: marginDebt, MarginDebtBasis: marginDebtBasis,
		MarginBorrowTransferID: borrowTransferID, MarginBorrowedAt: borrowedAt,
		MarginCoverRemainingQty: coverRemainingQty, MarginCoverRemainingKnown: coverRemainingKnown,
		MarginCoverRemainingBasis: coverRemainingBasis,
	}
}
