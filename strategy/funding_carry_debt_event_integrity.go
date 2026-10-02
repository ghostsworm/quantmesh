package strategy

import (
	"fmt"
	"math"
)

func validateFundingCarryDebtEventIntegrity(event fundingCarryMarginDebtEvent, accountScope string) error {
	tolerance := math.Max(1e-10, event.Amount*1e-8)
	components := event.Principal + event.InterestPaid
	if !validRuntimeAmount(event.Amount) || event.Amount <= 0 || !validRuntimeAmount(components) || components <= 0 ||
		!validRuntimeAmount(event.Principal) || !validRuntimeAmount(event.InterestPaid) ||
		math.Abs(event.Principal+event.InterestPaid-event.Amount) > tolerance ||
		event.AccountScope != accountScope || event.OccurredAt.UnixMilli() <= 0 {
		return fmt.Errorf("funding_carry margin debt event has invalid financial components or account scope")
	}
	if event.Action == "borrow" && (event.InterestPaid != 0 || math.Abs(event.Principal-event.Amount) > tolerance) {
		return fmt.Errorf("funding_carry borrow event cannot contain paid repayment interest")
	}
	return nil
}
