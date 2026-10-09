package strategy

import (
	"fmt"
	"strings"

	"quantmesh/exchange"
)

func validateFundingCarryCoverFee(fill *exchange.OrderFill, base string) error {
	baseAsset := strings.EqualFold(strings.TrimSpace(fill.CommissionAsset), base)
	if !baseAsset {
		if fill.BaseFeeQty != 0 {
			return fmt.Errorf("margin cover non-base fee contains base deduction")
		}
		return nil
	}
	expected := fill.Commission // margin adapter preserves the raw asset amount
	if fill.CommissionQuoteKnown {
		if !validRuntimeAmount(fill.CommissionQuote) || !validRuntimeAmount(fill.CommissionQuoteRate) {
			return fmt.Errorf("margin cover fee conversion is invalid")
		}
		if fill.CommissionQuote == 0 {
			if fill.Commission != 0 {
				return fmt.Errorf("margin cover positive fee has zero quote value")
			}
		} else {
			if fill.CommissionQuoteRate <= 0 {
				return fmt.Errorf("margin cover base fee conversion has no positive rate")
			}
			if !fundingCarryFinancialAmountsMatch(fill.CommissionQuote/fill.CommissionQuoteRate, expected) {
				return fmt.Errorf("margin cover quote fee disagrees with raw base commission")
			}
		}
	}
	if !fundingCarryFinancialAmountsMatch(fill.BaseFeeQty, expected) {
		return fmt.Errorf("margin cover base deduction disagrees with fee evidence")
	}
	return nil
}
