package bitget

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
	"net/url"
	"strconv"
	"strings"
)

// AccountEquityUSDT values every non-zero Spot asset balance using its direct
// ASSETUSDT market. It rejects partial or ambiguous snapshots instead of
// treating quantities in different currencies as interchangeable.
func (b *BitgetSpotAdapter) AccountEquityUSDT(ctx context.Context) (float64, bool) {
	if b == nil || b.client == nil {
		return 0, false
	}
	resp, err := b.client.DoRequest(ctx, "GET", "/api/v2/spot/account/assets", nil)
	if err != nil {
		return 0, false
	}
	var balances []bitgetSpotAccountAsset
	if err := json.Unmarshal(resp.Data, &balances); err != nil || balances == nil {
		return 0, false
	}
	value, err := valueBitgetSpotBalancesUSDT(ctx, balances, func(ctx context.Context, symbol string) (float64, error) {
		path := "/api/v2/spot/market/tickers?symbol=" + url.QueryEscape(symbol)
		response, requestErr := b.client.DoRequest(ctx, "GET", path, nil)
		if requestErr != nil {
			return 0, requestErr
		}
		var tickers []struct {
			Symbol string `json:"symbol"`
			LastPr string `json:"lastPr"`
		}
		if decodeErr := json.Unmarshal(response.Data, &tickers); decodeErr != nil || len(tickers) != 1 || !strings.EqualFold(tickers[0].Symbol, symbol) {
			return 0, fmt.Errorf("missing or mismatched ticker for %s", symbol)
		}
		price, parseErr := strconv.ParseFloat(tickers[0].LastPr, 64)
		if parseErr != nil || price <= 0 || math.IsNaN(price) || math.IsInf(price, 0) {
			return 0, fmt.Errorf("invalid ticker price for %s", symbol)
		}
		return price, nil
	})
	if err != nil || math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return 0, false
	}
	return value, true
}

func valueBitgetSpotBalancesUSDT(ctx context.Context, balances []bitgetSpotAccountAsset, fetchPrice func(context.Context, string) (float64, error)) (float64, error) {
	if balances == nil || fetchPrice == nil {
		return 0, fmt.Errorf("Bitget Spot balances or price source unavailable")
	}
	total := new(big.Rat)
	seen := make(map[string]struct{}, len(balances))
	for _, balance := range balances {
		asset := strings.ToUpper(strings.TrimSpace(balance.Coin))
		if asset == "" {
			return 0, fmt.Errorf("Bitget Spot balance has empty asset")
		}
		if _, exists := seen[asset]; exists {
			return 0, fmt.Errorf("duplicate Bitget Spot balance asset %s", asset)
		}
		seen[asset] = struct{}{}
		available, ok := new(big.Rat).SetString(strings.TrimSpace(balance.Available))
		if !ok || available.Sign() < 0 {
			return 0, fmt.Errorf("invalid Bitget Spot available balance for %s", asset)
		}
		locked, ok := new(big.Rat).SetString(strings.TrimSpace(balance.Locked))
		if !ok || locked.Sign() < 0 {
			return 0, fmt.Errorf("invalid Bitget Spot locked balance for %s", asset)
		}
		quantity := new(big.Rat).Add(available, locked)
		if quantity.Sign() == 0 {
			continue
		}
		price := 1.0
		if asset != "USDT" {
			var err error
			price, err = fetchPrice(ctx, asset+"USDT")
			if err != nil || price <= 0 || math.IsNaN(price) || math.IsInf(price, 0) {
				return 0, fmt.Errorf("cannot value Bitget Spot asset %s in USDT", asset)
			}
		}
		priceRat, ok := new(big.Rat).SetString(strconv.FormatFloat(price, 'g', -1, 64))
		if !ok {
			return 0, fmt.Errorf("invalid Bitget Spot price for %s", asset)
		}
		total.Add(total, new(big.Rat).Mul(quantity, priceRat))
	}
	value, _ := total.Float64()
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, fmt.Errorf("Bitget Spot equity is not finite")
	}
	return value, nil
}
