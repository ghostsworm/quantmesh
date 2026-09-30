package coinex

import (
	"context"
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"
	"time"

	"quantmesh/exchange/income"
)

// GetIncomeHistory returns authenticated, position-level futures funding entries.
func (a *Adapter) GetIncomeHistory(ctx context.Context, symbol, incomeType string, startTime, endTime int64) ([]*income.Income, error) {
	if a == nil || a.client == nil || strings.TrimSpace(symbol) == "" {
		return nil, fmt.Errorf("CoinEx income query requires adapter, client, and symbol")
	}
	if incomeType != "" && !strings.EqualFold(strings.TrimSpace(incomeType), "FUNDING_FEE") {
		return nil, fmt.Errorf("CoinEx income history does not support type %q", incomeType)
	}
	market := strings.ToUpper(strings.TrimSpace(symbol))
	if market != strings.ToUpper(strings.TrimSpace(a.market)) {
		return nil, fmt.Errorf("CoinEx income symbol %q does not match configured market %q", symbol, a.market)
	}
	rows, err := a.client.GetPositionFundingHistoryV2(ctx, market, startTime, endTime)
	if err != nil {
		return nil, err
	}
	result := make([]*income.Income, 0, len(rows))
	seenIDs := make(map[int64]struct{}, len(rows))
	for _, row := range rows {
		positionID, positionErr := strconv.ParseInt(row.PositionID.String(), 10, 64)
		createdAt, timeErr := strconv.ParseInt(row.CreatedAt.String(), 10, 64)
		fundingRate, rateErr := strconv.ParseFloat(row.FundingRate, 64)
		fundingValue, valueErr := strconv.ParseFloat(row.FundingValue, 64)
		asset := strings.ToUpper(strings.TrimSpace(row.Currency))
		if positionErr != nil || positionID <= 0 || timeErr != nil || createdAt < startTime || createdAt > endTime ||
			rateErr != nil || valueErr != nil || !finiteCoinExValue(fundingRate) || !finiteCoinExValue(fundingValue) ||
			fundingValue < 0 || asset == "" || !strings.EqualFold(row.Market, market) || !strings.EqualFold(row.MarketType, "FUTURES") {
			return nil, fmt.Errorf("CoinEx funding entry for %s contains invalid identity, range, or financial fields", market)
		}
		side := strings.ToLower(strings.TrimSpace(row.Side))
		if side != "long" && side != "short" {
			return nil, fmt.Errorf("CoinEx funding entry for %s has unsupported position side %q", market, row.Side)
		}
		amount := fundingValue
		if (side == "long" && fundingRate > 0) || (side == "short" && fundingRate < 0) {
			amount = -amount
		}
		identity := market + ":" + strconv.FormatInt(positionID, 10) + ":" + side + ":" + strconv.FormatInt(createdAt, 10)
		hasher := fnv.New64a()
		_, _ = hasher.Write([]byte(identity))
		transactionID := int64(hasher.Sum64() & (^uint64(0) >> 1))
		if transactionID == 0 {
			transactionID = 1
		}
		if _, duplicate := seenIDs[transactionID]; duplicate {
			return nil, fmt.Errorf("CoinEx funding entries produced a duplicate transaction identity for %s", market)
		}
		seenIDs[transactionID] = struct{}{}
		result = append(result, &income.Income{Symbol: market, IncomeType: "FUNDING_FEE", Income: amount, Asset: asset,
			Info:          fmt.Sprintf("position_id=%d side=%s funding_rate=%s", positionID, side, row.FundingRate),
			TransactionID: transactionID, TradeTime: time.UnixMilli(createdAt).UTC()})
	}
	return result, nil
}
