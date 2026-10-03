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
	principal, interest, _, err := b.GetMarginRepaymentFunds(ctx, asset)
	return principal, interest, err
}

func (b *BinanceSpotMarginAdapter) GetMarginRepaymentFunds(ctx context.Context, asset string) (float64, float64, float64, error) {
	if !strings.EqualFold(asset, b.baseAsset) || strings.TrimSpace(asset) == "" {
		return 0, 0, 0, fmt.Errorf("margin liability asset does not match configured base")
	}
	row, err := b.readMarginAsset(ctx, asset)
	if err != nil {
		return 0, 0, 0, err
	}
	free, principal, interest, _, err := parseMarginUserAsset(*row)
	if err != nil {
		return 0, 0, 0, err
	}
	if math.IsInf(principal+interest, 0) {
		return 0, 0, 0, fmt.Errorf("margin liability total overflows")
	}
	return principal, interest, free, nil
}

// Override the embedded spot method: margin funds never come from /api/v3/account.
func (b *BinanceSpotMarginAdapter) GetBalance(ctx context.Context, asset string) (float64, error) {
	row, err := b.readMarginAsset(ctx, asset)
	if err != nil {
		return 0, err
	}
	free, _, _, _, err := parseMarginUserAsset(*row)
	return free, err
}

func (b *BinanceSpotMarginAdapter) readMarginAsset(ctx context.Context, asset string) (*binancesdk.UserAsset, error) {
	if ctx == nil {
		return nil, fmt.Errorf("margin asset requires context")
	}
	if strings.TrimSpace(asset) == "" {
		return nil, fmt.Errorf("margin asset identity is empty")
	}
	var account *binancesdk.MarginAccount
	err := b.withRateLimit(ctx, func() error {
		var err error
		account, err = b.client.NewGetMarginAccountService().Do(ctx)
		return err
	})
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if account == nil || account.UserAssets == nil {
		return nil, fmt.Errorf("margin asset has no authoritative asset list")
	}
	var found *binancesdk.UserAsset
	for i := range account.UserAssets {
		row := &account.UserAssets[i]
		if !strings.EqualFold(row.Asset, asset) {
			continue
		}
		if found != nil {
			return nil, fmt.Errorf("margin asset is duplicated")
		}
		found = row
	}
	if found == nil {
		return nil, fmt.Errorf("margin asset is missing")
	}
	return found, nil
}
