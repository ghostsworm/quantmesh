package strategy

import (
	"math"
	"math/big"
)

func fundingCarryFinancialAmountsMatch(actual, expected float64) bool {
	if !validRuntimeAmount(actual) || !validRuntimeAmount(expected) {
		return false
	}
	if actual == 0 || expected == 0 {
		return actual == expected
	}
	difference, _ := new(big.Rat).Sub(fundingCarryDecimalPrincipal(actual), fundingCarryDecimalPrincipal(expected)).Float64()
	return math.Abs(difference) <= fundingCarryPrincipalRoundoff(math.Max(actual, expected))
}
