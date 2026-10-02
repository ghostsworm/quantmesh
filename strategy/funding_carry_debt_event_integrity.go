package strategy

import (
	"fmt"
)

func validateFundingCarryDebtEventIntegrity(event fundingCarryMarginDebtEvent, accountScope string) error {
	components := event.Principal + event.InterestPaid
	if !validRuntimeAmount(event.Amount) || event.Amount <= 0 || !validRuntimeAmount(components) || components <= 0 ||
		!validRuntimeAmount(event.Principal) || !validRuntimeAmount(event.InterestPaid) ||
		!fundingCarryFinancialAmountsMatch(components, event.Amount) ||
		event.AccountScope != accountScope || event.OccurredAt.UnixMilli() <= 0 {
		return fmt.Errorf("funding_carry margin debt event has invalid financial components or account scope")
	}
	if event.Action == "borrow" && (event.InterestPaid != 0 || !fundingCarryFinancialAmountsMatch(event.Principal, event.Amount)) {
		return fmt.Errorf("funding_carry borrow event cannot contain paid repayment interest")
	}
	return nil
}
