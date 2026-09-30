package bybit

import (
	"context"
	"fmt"
	"hash/fnv"
	"math"
	"strconv"
	"strings"
	"time"

	"quantmesh/exchange/income"
)

const bybitTransactionLogMaxWindowMs int64 = 7*24*60*60*1000 - 1

// GetIncomeHistory returns funding cash flows from Bybit's authenticated unified transaction log.
func (b *BybitAdapter) GetIncomeHistory(ctx context.Context, symbol, incomeType string, startTime, endTime int64) ([]*income.Income, error) {
	if b == nil || b.client == nil || strings.TrimSpace(symbol) == "" {
		return nil, fmt.Errorf("Bybit income query requires adapter, client, and symbol")
	}
	if incomeType != "" && !strings.EqualFold(strings.TrimSpace(incomeType), "FUNDING_FEE") {
		return nil, fmt.Errorf("Bybit income history does not support type %q", incomeType)
	}
	market := strings.ToUpper(strings.TrimSpace(symbol))
	if !strings.EqualFold(market, strings.TrimSpace(b.symbol)) {
		return nil, fmt.Errorf("Bybit income symbol %q does not match configured symbol %q", symbol, b.symbol)
	}
	if startTime <= 0 || endTime < startTime {
		return nil, fmt.Errorf("Bybit income query requires a valid time range")
	}

	result := make([]*income.Income, 0)
	seenIDs := make(map[int64]struct{})
	for windowStart := startTime; windowStart <= endTime; {
		windowEnd := endTime
		if endTime-windowStart > bybitTransactionLogMaxWindowMs {
			windowEnd = windowStart + bybitTransactionLogMaxWindowMs
		}
		windowRows, err := b.getFundingHistoryWindow(ctx, market, windowStart, windowEnd, seenIDs)
		if err != nil {
			return nil, err
		}
		result = append(result, windowRows...)
		if windowEnd == endTime {
			break
		}
		windowStart = windowEnd + 1
	}
	return result, nil
}

func (b *BybitAdapter) getFundingHistoryWindow(ctx context.Context, symbol string, startTime, endTime int64, seenIDs map[int64]struct{}) ([]*income.Income, error) {
	var result []*income.Income
	cursor := ""
	seenCursors := make(map[string]struct{})
	for {
		rows, nextCursor, err := b.client.GetTransactionLogPage(ctx, "linear", startTime, endTime, cursor)
		if err != nil {
			return nil, fmt.Errorf("fetch Bybit funding history for %s [%d,%d]: %w", symbol, startTime, endTime, err)
		}
		if len(rows) > bybitTransactionLogPageSize || (nextCursor != "" && len(rows) == 0) {
			return nil, fmt.Errorf("Bybit transaction-log returned an invalid page (rows=%d next_cursor=%t)", len(rows), nextCursor != "")
		}
		for _, row := range rows {
			entry, relevant, err := normalizeBybitFundingEntry(row, symbol, startTime, endTime, seenIDs)
			if err != nil {
				return nil, err
			}
			if relevant {
				result = append(result, entry)
			}
		}
		if nextCursor == "" {
			return result, nil
		}
		if _, duplicate := seenCursors[nextCursor]; duplicate || nextCursor == cursor {
			return nil, fmt.Errorf("Bybit transaction-log returned a repeated pagination cursor")
		}
		seenCursors[nextCursor] = struct{}{}
		cursor = nextCursor
	}
}

func normalizeBybitFundingEntry(row BybitTransactionLog, symbol string, startTime, endTime int64, seenIDs map[int64]struct{}) (*income.Income, bool, error) {
	if !strings.EqualFold(row.Category, "linear") {
		return nil, false, fmt.Errorf("Bybit transaction-log returned unexpected category %q", row.Category)
	}
	if !strings.EqualFold(row.Symbol, symbol) || !strings.EqualFold(row.Type, "SETTLEMENT") || strings.TrimSpace(row.Funding) == "" {
		return nil, false, nil
	}
	tradeTime, timeErr := strconv.ParseInt(row.TransactionTime, 10, 64)
	amount, amountErr := strconv.ParseFloat(row.Funding, 64)
	asset := strings.ToUpper(strings.TrimSpace(row.Currency))
	if strings.TrimSpace(row.ID) == "" || timeErr != nil || tradeTime < startTime || tradeTime > endTime ||
		amountErr != nil || math.IsNaN(amount) || math.IsInf(amount, 0) || asset == "" {
		return nil, false, fmt.Errorf("Bybit funding entry for %s contains invalid identity, range, or financial fields", symbol)
	}
	// The exchange row ID is the stable identity. Financial payload fields must
	// not participate in the hash: if Bybit corrects an amount/time/currency,
	// storage must see the same transaction ID and reject conflicting economics.
	identity := strings.TrimSpace(row.ID)
	hasher := fnv.New64a()
	_, _ = hasher.Write([]byte(identity))
	transactionID := int64(hasher.Sum64() & (^uint64(0) >> 1))
	if transactionID == 0 {
		transactionID = 1
	}
	if _, duplicate := seenIDs[transactionID]; duplicate {
		return nil, false, fmt.Errorf("Bybit funding history contains duplicate transaction identity %q", row.ID)
	}
	seenIDs[transactionID] = struct{}{}
	return &income.Income{Symbol: symbol, IncomeType: "FUNDING_FEE", Income: amount, Asset: asset,
		Info: "bybit_transaction_id=" + row.ID, TransactionID: transactionID, TradeTime: time.UnixMilli(tradeTime).UTC()}, true, nil
}
