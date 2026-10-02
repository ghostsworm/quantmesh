package strategy

import (
	"context"
	"fmt"
	"math/big"
	"strings"

	"quantmesh/exchange"
)

func (s *FundingCarryStrategy) verifyMarginNetDebtCover(ctx context.Context, orderID int64, gross, requested, debt float64) error {
	if ctx == nil || ctx.Err() != nil || orderID <= 0 {
		return fmt.Errorf("margin net cover requires live context and order identity")
	}
	fills, err := s.marginEx.GetOrderFills(ctx, s.symbol, orderID)
	if err != nil {
		return fmt.Errorf("query margin debt cover fills: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	base := strings.TrimSpace(s.spot.GetBaseAsset())
	if base == "" || len(fills) == 0 {
		return fmt.Errorf("margin debt cover has no complete fill evidence")
	}
	seen := make(map[string]struct{}, len(fills))
	total, fees := new(big.Rat), new(big.Rat)
	for _, fill := range fills {
		if fill == nil || fill.OrderID != orderID || strings.TrimSpace(fill.TradeID) == "" || !strings.EqualFold(strings.TrimSpace(fill.Symbol), s.symbol) || fill.Side != exchange.SideBuy || fill.TradeTime <= 0 || !validRuntimeAmount(fill.Price) || fill.Price <= 0 || !validRuntimeAmount(fill.Quantity) || fill.Quantity <= 0 || !validRuntimeAmount(fill.Commission) || !validRuntimeAmount(fill.BaseFeeQty) || fill.BaseFeeQty > fill.Quantity || strings.TrimSpace(fill.CommissionAsset) == "" {
			return fmt.Errorf("margin debt cover contains invalid fill or fee identity")
		}
		if _, exists := seen[fill.TradeID]; exists {
			return fmt.Errorf("margin debt cover repeats a trade identity")
		}
		seen[fill.TradeID] = struct{}{}
		if err := validateFundingCarryCoverFee(fill, base); err != nil {
			return err
		}
		total.Add(total, fundingCarryDecimalPrincipal(fill.Quantity))
		fees.Add(fees, fundingCarryDecimalPrincipal(fill.BaseFeeQty))
	}
	quantity, _ := total.Float64()
	if !fundingCarryFinancialAmountsMatch(quantity, gross) {
		return fmt.Errorf("margin debt cover fills do not cover cumulative execution")
	}
	net, _ := new(big.Rat).Sub(total, fees).Float64()
	return validateFundingCarryDebtCover(net, requested, debt)
}
