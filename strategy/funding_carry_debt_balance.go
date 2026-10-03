package strategy

import (
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
)

const fundingCarryPrincipalRoundoffULPs = 4

// Retained events span completed cycles. Interest cannot offset principal.
func validateFundingCarryDebtPrincipalBalance(state fundingCarryRuntimeState) error {
	asset := ""
	balance := new(big.Rat)
	activeBorrow := false
	for _, event := range state.MarginDebtEvents {
		currentAsset := strings.ToUpper(strings.TrimSpace(event.Asset))
		if asset == "" {
			asset = currentAsset
		}
		if currentAsset != asset {
			return fmt.Errorf("margin principal ledger mixes assets")
		}
		previous, _ := balance.Float64()
		principal := fundingCarryDecimalPrincipal(event.Principal)
		if event.Action == "borrow" {
			balance.Add(balance, principal)
			activeBorrow = event.TransferID == state.MarginBorrowTransferID && event.OccurredAt.Equal(state.MarginBorrowedAt)
		} else {
			if event.Principal > 0 && balance.Sign() <= 0 {
				return fmt.Errorf("margin principal repayment has no preceding borrowing")
			}
			balance.Sub(balance, principal)
		}
		current, _ := balance.Float64()
		if math.IsNaN(current) || math.IsInf(current, 0) {
			return fmt.Errorf("margin principal ledger overflow")
		}
		if balance.Sign() < 0 {
			if -current > fundingCarryPrincipalRoundoff(math.Max(previous, event.Principal)) {
				return fmt.Errorf("margin principal repayment precedes or exceeds borrowing")
			}
			balance.SetInt64(0)
		}
	}
	current, _ := balance.Float64()
	difference, _ := new(big.Rat).Sub(balance, fundingCarryDecimalPrincipal(state.MarginDebt)).Float64()
	if math.Abs(difference) > fundingCarryPrincipalRoundoff(math.Max(math.Abs(current), state.MarginDebt)) {
		return fmt.Errorf("margin principal ledger does not match owned debt")
	}
	if state.MarginDebt > 0 && (state.Direction != DirectionReverse || !activeBorrow) {
		return fmt.Errorf("margin principal debt lacks active borrow identity")
	}
	return nil
}

// Sum the decimals preserved by JSON instead of accumulating binary drift
// across completed cycles. Inputs have already passed finite/nonnegative checks.
func fundingCarryDecimalPrincipal(value float64) *big.Rat {
	amount, _ := new(big.Rat).SetString(strconv.FormatFloat(value, 'f', -1, 64))
	return amount
}

func fundingCarryPrincipalRoundoff(value float64) float64 {
	return fundingCarryPrincipalRoundoffULPs * (value - math.Nextafter(value, 0))
}

func validateFundingCarryDebtAsset(state fundingCarryRuntimeState, baseAsset string) error {
	if state.MarginCoverIntent != nil && !strings.EqualFold(state.MarginCoverIntent.Asset, strings.TrimSpace(baseAsset)) {
		return fmt.Errorf("margin cover intent asset does not match exchange base asset")
	}
	for _, record := range state.MarginCoverOrders {
		if strings.TrimSpace(baseAsset) == "" || !strings.EqualFold(record.Asset, strings.TrimSpace(baseAsset)) {
			return fmt.Errorf("margin cover journal asset does not match exchange base asset")
		}
	}
	for _, event := range state.MarginDebtEvents {
		if strings.TrimSpace(baseAsset) == "" || !strings.EqualFold(strings.TrimSpace(event.Asset), strings.TrimSpace(baseAsset)) {
			return fmt.Errorf("margin debt ledger asset does not match exchange base asset")
		}
	}
	return nil
}
