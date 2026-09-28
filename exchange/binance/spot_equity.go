package binance

import (
	"context"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"

	binancesdk "github.com/adshao/go-binance/v2"
)

// AccountEquityUSDT values every non-zero spot balance into USDT. It refuses
// partial valuation rather than summing token quantities as if they shared a
// currency or silently dropping an asset without a direct USDT market.
func (b *BinanceSpotAdapter) AccountEquityUSDT(ctx context.Context) (float64, bool) {
	if b == nil || b.client == nil {
		return 0, false
	}
	var account *binancesdk.Account
	if err := b.withRateLimit(ctx, func() error {
		var err error
		account, err = b.client.NewGetAccountService().Do(ctx)
		return err
	}); err != nil || account == nil {
		return 0, false
	}
	fetchPrice := b.feePriceFetcher
	if fetchPrice == nil {
		fetchPrice = b.fetchTickerPrice
	}
	value, err := valueSpotBalancesUSDT(ctx, account.Balances, fetchPrice)
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return 0, false
	}
	return value, true
}

func valueSpotBalancesUSDT(ctx context.Context, balances []binancesdk.Balance, fetchPrice func(context.Context, string) (float64, error)) (float64, error) {
	if balances == nil || fetchPrice == nil {
		return 0, fmt.Errorf("spot balances or USDT price source unavailable")
	}
	total := new(big.Rat)
	seen := make(map[string]struct{}, len(balances))
	for _, balance := range balances {
		asset := strings.ToUpper(strings.TrimSpace(balance.Asset))
		if asset == "" {
			return 0, fmt.Errorf("spot balance has empty asset")
		}
		if _, duplicate := seen[asset]; duplicate {
			return 0, fmt.Errorf("duplicate spot balance asset %s", asset)
		}
		seen[asset] = struct{}{}
		free, ok := new(big.Rat).SetString(strings.TrimSpace(balance.Free))
		if !ok || free.Sign() < 0 {
			return 0, fmt.Errorf("invalid free balance for %s", asset)
		}
		locked, ok := new(big.Rat).SetString(strings.TrimSpace(balance.Locked))
		if !ok || locked.Sign() < 0 {
			return 0, fmt.Errorf("invalid locked balance for %s", asset)
		}
		quantity := new(big.Rat).Add(free, locked)
		if quantity.Sign() == 0 {
			continue
		}
		price := 1.0
		if asset != "USDT" {
			var err error
			price, err = fetchPrice(ctx, asset+"USDT")
			if err != nil {
				return 0, fmt.Errorf("value spot asset %s: %w", asset, err)
			}
			if price <= 0 || math.IsNaN(price) || math.IsInf(price, 0) {
				return 0, fmt.Errorf("invalid USDT price for spot asset %s", asset)
			}
		}
		priceRat, ok := new(big.Rat).SetString(strconv.FormatFloat(price, 'g', -1, 64))
		if !ok {
			return 0, fmt.Errorf("cannot represent USDT price for spot asset %s", asset)
		}
		total.Add(total, new(big.Rat).Mul(quantity, priceRat))
	}
	value, _ := total.Float64()
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, fmt.Errorf("spot account equity is not finite")
	}
	return value, nil
}
