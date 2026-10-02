package strategy

import "fmt"

func validateFundingCarryDebtCover(filled, requested, totalDebt float64) error {
	if !validRuntimeAmount(filled) || !validRuntimeAmount(requested) || !validRuntimeAmount(totalDebt) || requested <= 0 || totalDebt <= 0 {
		return fmt.Errorf("margin debt cover contains invalid quantities")
	}
	if filled > requested && !fundingCarryFinancialAmountsMatch(filled, requested) {
		return fmt.Errorf("margin debt cover fill exceeds submitted quantity")
	}
	if filled < totalDebt && !fundingCarryFinancialAmountsMatch(filled, totalDebt) {
		return fmt.Errorf("margin debt cover %.12g does not cover principal and interest %.12g", filled, totalDebt)
	}
	return nil
}
