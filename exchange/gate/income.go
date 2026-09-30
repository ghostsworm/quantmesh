package gate

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

const gateContractLedgerFilterStartMs int64 = 1698710400000 // 2023-10-31 UTC; older records may lack contract identity.

// GetIncomeHistory returns contract-scoped funding cash flows from Gate's signed account book.
func (g *GateAdapter) GetIncomeHistory(ctx context.Context, symbol, incomeType string, startTime, endTime int64) ([]*income.Income, error) {
	if g == nil || g.client == nil || strings.TrimSpace(symbol) == "" || strings.TrimSpace(g.settle) == "" {
		return nil, fmt.Errorf("Gate income query requires adapter, client, settlement currency, and symbol")
	}
	if incomeType != "" && !strings.EqualFold(strings.TrimSpace(incomeType), "FUNDING_FEE") {
		return nil, fmt.Errorf("Gate income history does not support type %q", incomeType)
	}
	if !strings.EqualFold(strings.TrimSpace(symbol), strings.TrimSpace(g.symbol)) {
		return nil, fmt.Errorf("Gate income symbol %q does not match configured symbol %q", symbol, g.symbol)
	}
	switch strings.ToLower(strings.TrimSpace(g.settle)) {
	case "btc", "usdt", "usd1":
	default:
		return nil, fmt.Errorf("Gate income history has unsupported settlement currency %q", g.settle)
	}
	if startTime < gateContractLedgerFilterStartMs || endTime < startTime {
		return nil, fmt.Errorf("Gate funding history requires a valid range starting on or after 2023-10-31 UTC so contract identity can be verified")
	}
	return g.fetchFundingAccountBook(ctx, strings.ToUpper(strings.TrimSpace(symbol)), startTime, endTime)
}

func (g *GateAdapter) fetchFundingAccountBook(ctx context.Context, symbol string, startTime, endTime int64) ([]*income.Income, error) {
	contract := strings.ToUpper(strings.TrimSpace(g.gateSymbol))
	if contract == "" {
		return nil, fmt.Errorf("Gate funding history requires a configured contract")
	}
	fromSeconds := startTime / 1000
	toSeconds := endTime / 1000
	if endTime%1000 != 0 {
		toSeconds++
	}
	var result []*income.Income
	seenRecordIDs := make(map[string]struct{})
	seenTransactionIDs := make(map[int64]string)
	for offset := 0; ; {
		rows, err := g.client.GetFuturesAccountBookPage(ctx, g.settle, contract, fromSeconds, toSeconds, offset)
		if err != nil {
			return nil, fmt.Errorf("fetch Gate funding account book for %s: %w", contract, err)
		}
		if len(rows) > gateFuturesAccountBookPageSize {
			return nil, fmt.Errorf("Gate account book returned %d rows above page limit", len(rows))
		}
		for _, row := range rows {
			entry, relevant, err := normalizeGateFundingEntry(row, symbol, contract, g.settle, startTime, endTime, seenRecordIDs, seenTransactionIDs)
			if err != nil {
				return nil, err
			}
			if relevant {
				result = append(result, entry)
			}
		}
		if len(rows) < gateFuturesAccountBookPageSize {
			return result, nil
		}
		offset += len(rows)
	}
}

func normalizeGateFundingEntry(row FuturesAccountBookEntry, symbol, contract, settle string, startTime, endTime int64, seenIDs map[string]struct{}, seenTransactionIDs map[int64]string) (*income.Income, bool, error) {
	if !strings.EqualFold(strings.TrimSpace(row.Type), "fund") {
		return nil, false, fmt.Errorf("Gate account-book query returned unexpected type %q", row.Type)
	}
	if !strings.EqualFold(strings.TrimSpace(row.Contract), contract) {
		return nil, false, fmt.Errorf("Gate funding record %q has missing or mismatched contract identity %q", row.ID, row.Contract)
	}
	if strings.TrimSpace(row.ID) == "" || math.IsNaN(row.Time) || math.IsInf(row.Time, 0) || row.Time <= 0 || row.Time > float64(math.MaxInt64)/float64(time.Second) {
		return nil, false, fmt.Errorf("Gate funding record for %s contains invalid identity or time", contract)
	}
	tradeTime := time.Unix(0, int64(math.Round(row.Time*float64(time.Second)))).UTC()
	if tradeTime.Before(time.UnixMilli(startTime)) || tradeTime.After(time.UnixMilli(endTime)) {
		return nil, false, fmt.Errorf("Gate funding record %q is outside the requested time range", row.ID)
	}
	amount, err := strconv.ParseFloat(strings.TrimSpace(row.Change), 64)
	if err != nil || math.IsNaN(amount) || math.IsInf(amount, 0) {
		return nil, false, fmt.Errorf("Gate funding record %q contains invalid change %q", row.ID, row.Change)
	}
	if _, duplicate := seenIDs[row.ID]; duplicate {
		return nil, false, fmt.Errorf("Gate account book contains duplicate record ID %q", row.ID)
	}
	seenIDs[row.ID] = struct{}{}
	hasher := fnv.New64a()
	_, _ = hasher.Write([]byte(row.ID))
	transactionID := int64(hasher.Sum64() & (^uint64(0) >> 1))
	if transactionID == 0 {
		transactionID = 1
	}
	if existingID, collision := seenTransactionIDs[transactionID]; collision {
		return nil, false, fmt.Errorf("Gate account-book IDs %q and %q collide after local ID mapping", existingID, row.ID)
	}
	seenTransactionIDs[transactionID] = row.ID
	return &income.Income{Symbol: symbol, IncomeType: "FUNDING_FEE", Income: amount, Asset: strings.ToUpper(settle),
		Info: "gate_account_book_id=" + row.ID, TransactionID: transactionID, TradeTime: tradeTime}, true, nil
}
