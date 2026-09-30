package bitget

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math"
	"math/big"
	"net/url"
	"strconv"
	"strings"
	"time"

	"quantmesh/exchange/accounting"
	"quantmesh/exchange/income"
)

const bitgetIncomeRetention = 90 * 24 * time.Hour

// GetIncomeHistory returns contract funding cash flows from Bitget's signed bill ledger.
func (b *BitgetAdapter) GetIncomeHistory(ctx context.Context, symbol, incomeType string, startTime, endTime int64) ([]*income.Income, error) {
	if b == nil || b.client == nil || strings.TrimSpace(symbol) == "" || strings.TrimSpace(b.productType) == "" {
		return nil, fmt.Errorf("Bitget income query requires client, product type, and symbol")
	}
	if incomeType != "" && !strings.EqualFold(strings.TrimSpace(incomeType), "FUNDING_FEE") {
		return nil, fmt.Errorf("Bitget income history does not support type %q", incomeType)
	}
	market := strings.ToUpper(strings.TrimSpace(symbol))
	if !strings.EqualFold(market, strings.TrimSpace(b.symbol)) {
		return nil, fmt.Errorf("Bitget income symbol %q does not match configured symbol %q", symbol, b.symbol)
	}
	start := time.UnixMilli(startTime).UTC()
	end := time.UnixMilli(endTime).UTC()
	if startTime <= 0 || endTime < startTime || start.Before(end.Add(-bitgetIncomeRetention)) {
		return nil, fmt.Errorf("Bitget income interval must be valid and within the 90-day bill retention")
	}
	productType := strings.ToUpper(strings.TrimSpace(b.productType))
	switch productType {
	case "USDT-FUTURES", "USDC-FUTURES", "COIN-FUTURES":
	default:
		return nil, fmt.Errorf("Bitget income history has unsupported product type %q", b.productType)
	}
	return b.readFundingIncomeBills(ctx, market, productType, start, end)
}

func (b *BitgetAdapter) readFundingIncomeBills(ctx context.Context, symbol, productType string, from, through time.Time) ([]*income.Income, error) {
	seenBills := make(map[string]*income.Income)
	seenTransactionIDs := make(map[int64]string)
	var result []*income.Income
	for chunkStart := from; !chunkStart.After(through); {
		chunkEnd := chunkStart.Add(bitgetBillChunk)
		if chunkEnd.After(through) {
			chunkEnd = through
		}
		chunk, err := b.readFundingIncomeChunk(ctx, symbol, productType, chunkStart, chunkEnd, seenBills, seenTransactionIDs)
		if err != nil {
			return nil, err
		}
		result = append(result, chunk...)
		if chunkEnd.Equal(through) {
			return result, nil
		}
		chunkStart = chunkEnd
	}
	return result, nil
}

func (b *BitgetAdapter) readFundingIncomeChunk(ctx context.Context, symbol, productType string, start, end time.Time, seenBills map[string]*income.Income, seenTransactionIDs map[int64]string) ([]*income.Income, error) {
	cursor := ""
	var result []*income.Income
	for pageNumber := 0; pageNumber < bitgetBillMaxPages; pageNumber++ {
		query := url.Values{
			"productType": {productType}, "onlyFunding": {"yes"},
			"startTime": {strconv.FormatInt(start.UnixMilli(), 10)},
			"endTime":   {strconv.FormatInt(end.UnixMilli(), 10)},
			"limit":     {strconv.Itoa(bitgetBillPageSize)},
		}
		if cursor != "" {
			query.Set("idLessThan", cursor)
		}
		response, err := b.client.DoRequest(ctx, "GET", "/api/v2/mix/account/bill?"+query.Encode(), nil)
		if err != nil {
			return nil, fmt.Errorf("query Bitget funding bills for %s: %w", symbol, err)
		}
		var page bitgetBillPage
		if err := json.Unmarshal(response.Data, &page); err != nil || page.Bills == nil || len(page.Bills) > bitgetBillPageSize {
			return nil, fmt.Errorf("Bitget returned an invalid funding bill page for %s", symbol)
		}
		for _, row := range page.Bills {
			entry, relevant, err := normalizeBitgetFundingBill(row, symbol, start, end, productType, seenTransactionIDs)
			if err != nil {
				return nil, err
			}
			if !relevant {
				continue
			}
			if previous, exists := seenBills[row.ID]; exists {
				if *previous != *entry {
					return nil, fmt.Errorf("Bitget funding bill ID %q has conflicting accounting data", row.ID)
				}
				continue
			}
			seenBills[row.ID] = entry
			result = append(result, entry)
		}
		if len(page.Bills) > 0 {
			last := page.Bills[len(page.Bills)-1]
			if last == nil || page.EndID == "" || page.EndID != last.ID {
				return nil, fmt.Errorf("Bitget funding bill page endId does not match its final record for %s", symbol)
			}
		}
		if len(page.Bills) < bitgetBillPageSize {
			return result, nil
		}
		last := page.Bills[len(page.Bills)-1]
		if last == nil || page.EndID == "" || page.EndID != last.ID || page.EndID == cursor {
			return nil, fmt.Errorf("Bitget funding bill pagination did not advance for %s", symbol)
		}
		cursor = page.EndID
		if pageNumber == bitgetBillMaxPages-1 {
			return nil, fmt.Errorf("Bitget funding bill pagination budget exhausted for %s", symbol)
		}
	}
	return nil, fmt.Errorf("Bitget funding bill pagination ended unexpectedly for %s", symbol)
}

func normalizeBitgetFundingBill(row *bitgetBill, symbol string, start, end time.Time, productType string, seenIDs map[int64]string) (*income.Income, bool, error) {
	if row == nil {
		return nil, false, fmt.Errorf("Bitget returned a nil funding bill")
	}
	if !strings.EqualFold(strings.TrimSpace(row.BusinessType), "contract_settle_fee") {
		return nil, false, nil
	}
	if !strings.EqualFold(strings.TrimSpace(row.Symbol), symbol) {
		if strings.TrimSpace(row.Symbol) == "" {
			return nil, false, fmt.Errorf("Bitget funding bill %q is missing its symbol", row.ID)
		}
		return nil, false, nil
	}
	if strings.TrimSpace(row.ID) == "" || strings.TrimSpace(row.Coin) == "" {
		return nil, false, fmt.Errorf("Bitget funding bill for %s is missing its ID or settlement currency", symbol)
	}
	tradeTimeMs, err := strconv.ParseInt(strings.TrimSpace(row.Time), 10, 64)
	if err != nil || tradeTimeMs <= 0 {
		return nil, false, fmt.Errorf("Bitget funding bill %q has invalid timestamp", row.ID)
	}
	tradeTime := time.UnixMilli(tradeTimeMs).UTC()
	if tradeTime.Before(start) || tradeTime.After(end) {
		return nil, false, fmt.Errorf("Bitget funding bill %q is outside its query interval", row.ID)
	}
	amount, err := accounting.Decimal(strings.TrimSpace(row.Amount))
	if err != nil {
		return nil, false, fmt.Errorf("Bitget funding bill %q has invalid amount: %w", row.ID, err)
	}
	feeText := strings.TrimSpace(row.Fee)
	if feeText == "" {
		return nil, false, fmt.Errorf("Bitget funding bill %q is missing fee amount", row.ID)
	}
	fee, err := accounting.Decimal(feeText)
	if err != nil {
		return nil, false, fmt.Errorf("Bitget funding bill %q has invalid fee: %w", row.ID, err)
	}
	if couponText := strings.TrimSpace(row.FeeByCoupon); couponText != "" {
		coupon, parseErr := accounting.Decimal(couponText)
		if parseErr != nil || coupon.Sign() != 0 {
			return nil, false, fmt.Errorf("Bitget funding bill %q includes unaccounted coupon fee", row.ID)
		}
	}
	net := new(big.Rat).Add(amount, fee)
	value, _ := net.Float64()
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return nil, false, fmt.Errorf("Bitget funding bill %q amount exceeds supported numeric range", row.ID)
	}
	identity := productType + ":" + strings.ToUpper(row.Coin) + ":" + symbol + ":" + row.ID
	hasher := fnv.New64a()
	_, _ = hasher.Write([]byte(identity))
	transactionID := int64(hasher.Sum64() & (^uint64(0) >> 1))
	if transactionID == 0 {
		transactionID = 1
	}
	if previousID, exists := seenIDs[transactionID]; exists && previousID != row.ID {
		return nil, false, fmt.Errorf("Bitget bill IDs %q and %q collide after local ID mapping", previousID, row.ID)
	}
	seenIDs[transactionID] = row.ID
	return &income.Income{Symbol: symbol, IncomeType: "FUNDING_FEE", Income: value, Asset: strings.ToUpper(row.Coin),
		Info: "bitget_bill_id=" + row.ID, TransactionID: transactionID, TradeTime: tradeTime}, true, nil
}
