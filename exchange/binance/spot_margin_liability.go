package binance

import (
	"context"
	"fmt"
	"math"
	"strings"

	binancesdk "github.com/adshao/go-binance/v2"
)

// Debt is independent of inventory. Never treat bought-back base assets as
// proof that debt vanished, nor require zero inventory to read liabilities.
func (b *BinanceSpotMarginAdapter) GetMarginLiability(ctx context.Context, asset string) (float64, float64, error) {
	if ctx == nil {
		return 0, 0, fmt.Errorf("margin liability requires context")
	}
	if strings.TrimSpace(asset) == "" || !strings.EqualFold(asset, b.baseAsset) {
		return 0, 0, fmt.Errorf("margin liability asset does not match configured base")
	}
	var account *binancesdk.MarginAccount
	err := b.withRateLimit(ctx, func() error {
		var err error
		account, err = b.client.NewGetMarginAccountService().Do(ctx)
		return err
	})
	if err != nil {
		return 0, 0, err
	}
	if err := ctx.Err(); err != nil {
		return 0, 0, err
	}
	if account == nil || account.UserAssets == nil {
		return 0, 0, fmt.Errorf("margin liability has no authoritative asset list")
	}
	var found *binancesdk.UserAsset
	for i := range account.UserAssets {
		row := &account.UserAssets[i]
		if !strings.EqualFold(row.Asset, asset) {
			continue
		}
		if found != nil {
			return 0, 0, fmt.Errorf("margin liability asset is duplicated")
		}
		found = row
	}
	if found == nil {
		return 0, 0, fmt.Errorf("margin liability asset is missing")
	}
	_, principal, interest, _, err := parseMarginUserAsset(*found)
	if err != nil {
		return 0, 0, err
	}
	if math.IsInf(principal+interest, 0) {
		return 0, 0, fmt.Errorf("margin liability total overflows")
	}
	return principal, interest, nil
}
