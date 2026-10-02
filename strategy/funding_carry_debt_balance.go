package strategy

import (
	"fmt"
	"math"
	"strings"
)

// Retained events span completed cycles. Interest cannot offset principal.
func validateFundingCarryDebtPrincipalBalance(state fundingCarryRuntimeState) error {
	asset, balance := "", 0.0
	activeBorrow := false
	for _, event := range state.MarginDebtEvents {
		currentAsset := strings.ToUpper(strings.TrimSpace(event.Asset))
		if asset == "" {
			asset = currentAsset
		}
		if currentAsset != asset {
			return fmt.Errorf("margin principal ledger mixes assets")
		}
		if event.Action == "borrow" {
			balance += event.Principal
			activeBorrow = event.TransferID == state.MarginBorrowTransferID && event.OccurredAt.Equal(state.MarginBorrowedAt)
		} else {
			balance -= event.Principal
		}
		if math.IsNaN(balance) || math.IsInf(balance, 0) {
			return fmt.Errorf("margin principal ledger overflow")
		}
		if balance < -math.Max(1e-10, event.Principal*1e-8) {
			return fmt.Errorf("margin principal repayment precedes or exceeds borrowing")
		}
	}
	if math.Abs(balance-state.MarginDebt) > math.Max(1e-10, math.Max(math.Abs(balance), state.MarginDebt)*1e-8) {
		return fmt.Errorf("margin principal ledger does not match owned debt")
	}
	if state.MarginDebt > 0 && (state.Direction != DirectionReverse || !activeBorrow) {
		return fmt.Errorf("margin principal debt lacks active borrow identity")
	}
	return nil
}

func validateFundingCarryDebtAsset(state fundingCarryRuntimeState, baseAsset string) error {
	for _, event := range state.MarginDebtEvents {
		if strings.TrimSpace(baseAsset) == "" || !strings.EqualFold(strings.TrimSpace(event.Asset), strings.TrimSpace(baseAsset)) {
			return fmt.Errorf("margin debt ledger asset does not match exchange base asset")
		}
	}
	return nil
}
