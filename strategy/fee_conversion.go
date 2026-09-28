package strategy

import (
	"math"
	"strings"

	"quantmesh/position"
)

func commissionInQuote(exchange position.IExchange, commission float64, asset string, fillPrice float64) (float64, bool) {
	if commission == 0 {
		return 0, true
	}
	if exchange == nil || math.IsNaN(commission) || math.IsInf(commission, 0) ||
		fillPrice <= 0 || math.IsNaN(fillPrice) || math.IsInf(fillPrice, 0) {
		return 0, false
	}
	asset = strings.ToUpper(strings.TrimSpace(asset))
	quoteAsset := strings.ToUpper(strings.TrimSpace(exchange.GetQuoteAsset()))
	if asset == "" || quoteAsset == "" {
		return 0, false
	}
	if asset == quoteAsset {
		return commission, true
	}
	baseAsset := strings.ToUpper(strings.TrimSpace(exchange.GetBaseAsset()))
	if baseAsset != "" && asset == baseAsset {
		return commission * fillPrice, true
	}
	return 0, false
}
