package binance

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	binancesdk "github.com/adshao/go-binance/v2"
)

const marginPositionEvidenceTolerance = 1e-12
const marginPositionRelativeTolerance = 1e-8

func verifiedMarginShortDebt(account *binancesdk.MarginAccount, base string) (float64, float64, error) {
	if account == nil || account.UserAssets == nil {
		return 0, 0, fmt.Errorf("Binance margin position evidence has no asset list")
	}
	if len(account.UserAssets) == 0 {
		return 0, 0, verifyEmptyMarginPositionSummary(account)
	}
	var found *binancesdk.UserAsset
	for i := range account.UserAssets {
		asset := &account.UserAssets[i]
		if !strings.EqualFold(asset.Asset, base) {
			continue
		}
		if found != nil {
			return 0, 0, fmt.Errorf("Binance margin position evidence has duplicate %s assets", base)
		}
		found = asset
	}
	if found == nil {
		return 0, 0, fmt.Errorf("Binance margin position evidence is missing asset %s", base)
	}
	free, borrowed, interest, net, err := parseMarginUserAsset(*found)
	if err != nil {
		return 0, 0, fmt.Errorf("parse Binance margin debt for %s: %w", base, err)
	}
	locked, err := strconv.ParseFloat(found.Locked, 64)
	if err != nil || math.IsNaN(locked) || math.IsInf(locked, 0) || locked < 0 {
		return 0, 0, fmt.Errorf("parse Binance margin locked balance for %s: invalid amount", base)
	}
	if free > 0 || locked > 0 {
		return 0, 0, fmt.Errorf("Binance margin account has unowned %s inventory (free=%.12g locked=%.12g); SpotShort requires a zero base-asset balance to attribute borrowed exposure safely", base, free, locked)
	}
	liability := borrowed + interest
	tolerance := math.Max(marginPositionEvidenceTolerance, liability*marginPositionRelativeTolerance)
	if math.IsInf(liability, 0) || math.IsNaN(liability) || math.Abs(net+liability) > tolerance {
		return 0, 0, fmt.Errorf("Binance margin %s net exposure contradicts principal and interest", base)
	}
	return borrowed, interest, nil
}

func verifyEmptyMarginPositionSummary(account *binancesdk.MarginAccount) error {
	for _, field := range []struct{ name, raw string }{
		{"total assets", account.TotalAssetOfBTC},
		{"total liability", account.TotalLiabilityOfBTC},
		{"total net assets", account.TotalNetAssetOfBTC},
	} {
		value, err := strconv.ParseFloat(field.raw, 64)
		if err != nil || value != 0 {
			return fmt.Errorf("Binance margin empty asset list requires verified zero %s", field.name)
		}
	}
	return nil
}
