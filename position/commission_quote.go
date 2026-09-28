package position

import (
	"math"
	"strings"
)

func (spm *SuperPositionManager) commissionInQuote(update OrderUpdate, fillPrice float64) (float64, bool) {
	commission := update.Commission
	if commission == 0 {
		return 0, true
	}
	if math.IsNaN(commission) || math.IsInf(commission, 0) || spm.exchange == nil {
		return 0, false
	}
	asset := strings.ToUpper(strings.TrimSpace(update.CommissionAsset))
	quote := strings.ToUpper(strings.TrimSpace(spm.exchange.GetQuoteAsset()))
	if asset == "" || quote == "" {
		return 0, false
	}
	if asset == quote {
		return commission, true
	}
	base := strings.ToUpper(strings.TrimSpace(spm.exchange.GetBaseAsset()))
	if asset == base && base != "" && fillPrice > 0 && !math.IsNaN(fillPrice) && !math.IsInf(fillPrice, 0) {
		value := commission * fillPrice
		if !math.IsNaN(value) && !math.IsInf(value, 0) {
			return value, true
		}
	}
	return 0, false
}
