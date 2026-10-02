package bitget

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"quantmesh/exchange/accounting"
)

const (
	bitgetSpotBillsPageSize = 500
	bitgetSpotBillsMaxPages = 120
	bitgetSpotBillsChunk    = 30 * 24 * time.Hour
)

type bitgetSpotBill struct {
	ID           string `json:"billId"`
	Coin         string `json:"coin"`
	GroupType    string `json:"groupType"`
	BusinessType string `json:"businessType"`
	Size         string `json:"size"`
	Balance      string `json:"balance"`
	Fees         string `json:"fees"`
	Time         string `json:"cTime"`
}

func bitgetSpotBillEvidence(row *bitgetSpotBill, valuationCurrency string, fetchRate func(context.Context, string, time.Time) (string, string, time.Time, error), ctx context.Context) (accounting.Entry, error) {
	if row == nil || strings.TrimSpace(row.ID) == "" || strings.TrimSpace(row.Coin) == "" {
		return accounting.Entry{}, fmt.Errorf("Bitget Spot bill identity or currency missing")
	}
	sequence, ok := new(big.Int).SetString(row.ID, 10)
	if !ok || sequence.Sign() <= 0 {
		return accounting.Entry{}, fmt.Errorf("Bitget Spot bill sequence is invalid")
	}
	atMS, err := strconv.ParseInt(row.Time, 10, 64)
	if err != nil || atMS <= 0 {
		return accounting.Entry{}, fmt.Errorf("Bitget Spot bill timestamp is invalid")
	}
	at := time.UnixMilli(atMS).UTC()
	if _, err := accounting.Decimal(row.Size); err != nil {
		return accounting.Entry{}, fmt.Errorf("Bitget Spot bill size is invalid")
	}
	postBalance, err := accounting.Decimal(row.Balance)
	if err != nil || postBalance.Sign() < 0 {
		return accounting.Entry{}, fmt.Errorf("Bitget Spot post-bill balance is invalid")
	}
	if strings.TrimSpace(row.Fees) == "" {
		return accounting.Entry{}, fmt.Errorf("Bitget Spot bill fee is missing")
	}
	fees, err := accounting.Decimal(row.Fees)
	if err != nil {
		return accounting.Entry{}, fmt.Errorf("Bitget Spot bill fee is invalid")
	}
	kind, err := bitgetSpotBillKind(row.GroupType, row.BusinessType)
	if err != nil {
		return accounting.Entry{}, err
	}
	if isExternalSpotCapitalFlow(kind) && fees.Sign() != 0 {
		return accounting.Entry{}, fmt.Errorf("Bitget Spot capital flow with a nonzero bundled fee is unsupported")
	}
	coin := strings.ToUpper(strings.TrimSpace(row.Coin))
	entry := accounting.Entry{ID: row.ID, Sequence: sequence.String(), Kind: kind, Currency: coin,
		BalanceAfter: row.Balance, At: at}
	if isExternalSpotCapitalFlow(kind) && coin != strings.ToUpper(valuationCurrency) {
		if fetchRate == nil {
			return accounting.Entry{}, fmt.Errorf("Bitget Spot external flow lacks a historical valuation source")
		}
		rate, source, valuedAt, err := fetchRate(ctx, coin+strings.ToUpper(valuationCurrency), at)
		if err != nil {
			return accounting.Entry{}, err
		}
		parsedRate, err := accounting.Decimal(rate)
		if err != nil || parsedRate.Sign() <= 0 || source == "" || valuedAt.IsZero() || valuedAt.After(at) || at.Sub(valuedAt) >= time.Minute {
			return accounting.Entry{}, fmt.Errorf("Bitget Spot external flow valuation is invalid or stale")
		}
		entry.ValuationRate, entry.ValuationSource, entry.ValuationAt = parsedRate.FloatString(18), source, valuedAt
	}
	return entry, nil
}

func bitgetSpotBillKind(groupType, businessType string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(groupType)) {
	case "deposit":
		if businessType == "DEPOSIT" {
			return "deposit", nil
		}
	case "withdraw":
		if businessType == "WITHDRAW" {
			return "withdrawal", nil
		}
	case "transfer":
		switch businessType {
		case "TRANSFER_IN":
			return "transfer_in", nil
		case "TRANSFER_OUT":
			return "transfer_out", nil
		}
	case "transaction":
		switch businessType {
		case "BUY", "SELL":
			return "realized_pnl", nil
		case "DEDUCTION_HANDLING_FEE":
			return "fee", nil
		}
	}
	return "", fmt.Errorf("unclassified Bitget Spot bill type %q/%q", groupType, businessType)
}

func isExternalSpotCapitalFlow(kind string) bool {
	switch kind {
	case "deposit", "withdrawal", "transfer_in", "transfer_out":
		return true
	default:
		return false
	}
}

func (b *BitgetSpotAdapter) readSpotAssets(ctx context.Context) ([]bitgetSpotAccountAsset, int64, error) {
	if b == nil || b.client == nil {
		return nil, 0, fmt.Errorf("Bitget Spot client unavailable")
	}
	response, err := b.client.DoRequest(ctx, "GET", "/api/v2/spot/account/assets", nil)
	if err != nil {
		return nil, 0, err
	}
	var assets []bitgetSpotAccountAsset
	if err := json.Unmarshal(response.Data, &assets); err != nil || assets == nil || response.ReqTime <= 0 {
		return nil, 0, fmt.Errorf("invalid Bitget Spot account asset snapshot")
	}
	if _, err := spotWalletBalances(assets); err != nil {
		return nil, 0, err
	}
	return assets, response.ReqTime, nil
}

func spotWalletBalances(assets []bitgetSpotAccountAsset) (map[string]string, error) {
	if assets == nil {
		return nil, fmt.Errorf("Bitget Spot asset snapshot missing")
	}
	wallets := make(map[string]string, len(assets))
	for _, asset := range assets {
		coin := strings.ToUpper(strings.TrimSpace(asset.Coin))
		if coin == "" {
			return nil, fmt.Errorf("Bitget Spot asset currency missing")
		}
		if _, exists := wallets[coin]; exists {
			return nil, fmt.Errorf("duplicate Bitget Spot asset %s", coin)
		}
		available, err := accounting.Decimal(asset.Available)
		if err != nil || available.Sign() < 0 {
			return nil, fmt.Errorf("invalid Bitget Spot available balance for %s", coin)
		}
		locked, err := accounting.Decimal(asset.Locked)
		if err != nil || locked.Sign() < 0 {
			return nil, fmt.Errorf("invalid Bitget Spot locked balance for %s", coin)
		}
		frozen, err := accounting.Decimal(asset.Frozen)
		if err != nil || frozen.Sign() < 0 {
			return nil, fmt.Errorf("invalid Bitget Spot frozen balance for %s", coin)
		}
		wallets[coin] = new(big.Rat).Add(new(big.Rat).Add(available, locked), frozen).FloatString(18)
	}
	return wallets, nil
}

func (b *BitgetSpotAdapter) readSpotBills(ctx context.Context, from, through time.Time, valuationCurrency string) ([]accounting.Entry, error) {
	if from.IsZero() || through.Before(from) || from.Before(through.AddDate(0, 0, -90)) {
		return nil, fmt.Errorf("Bitget Spot bill interval invalid or exceeds the documented 90-day window")
	}
	seen := make(map[string]accounting.Entry)
	type cachedValuation struct {
		rate, source string
		at           time.Time
		err          error
	}
	rateCache := make(map[string]cachedValuation)
	fetchRate := func(ctx context.Context, symbol string, at time.Time) (string, string, time.Time, error) {
		minute := at.UTC().Truncate(time.Minute)
		key := strings.ToUpper(symbol) + ":" + strconv.FormatInt(minute.Unix(), 10)
		if cached, ok := rateCache[key]; ok {
			return cached.rate, cached.source, cached.at, cached.err
		}
		rate, source, valuedAt, err := b.spotHistoricalRate(ctx, symbol, at)
		rateCache[key] = cachedValuation{rate: rate, source: source, at: valuedAt, err: err}
		return rate, source, valuedAt, err
	}
	for start := from; !start.After(through); {
		end := start.Add(bitgetSpotBillsChunk)
		if end.After(through) {
			end = through
		}
		cursor := ""
		for page := 0; page < bitgetSpotBillsMaxPages; page++ {
			query := url.Values{"startTime": {strconv.FormatInt(start.UnixMilli(), 10)}, "endTime": {strconv.FormatInt(end.UnixMilli(), 10)}, "limit": {strconv.Itoa(bitgetSpotBillsPageSize)}}
			if cursor != "" {
				query.Set("idLessThan", cursor)
			}
			response, err := b.client.DoRequest(ctx, "GET", "/api/v2/spot/account/bills?"+query.Encode(), nil)
			if err != nil {
				return nil, err
			}
			var rows []*bitgetSpotBill
			if err := json.Unmarshal(response.Data, &rows); err != nil || rows == nil || len(rows) > bitgetSpotBillsPageSize {
				return nil, fmt.Errorf("invalid Bitget Spot bill page")
			}
			newRows := 0
			var previousPageID *big.Int
			for _, row := range rows {
				if row == nil {
					return nil, fmt.Errorf("Bitget Spot bill page contains a missing row")
				}
				pageID, validID := new(big.Int).SetString(strings.TrimSpace(row.ID), 10)
				if !validID || pageID.Sign() <= 0 || (previousPageID != nil && pageID.Cmp(previousPageID) >= 0) {
					return nil, fmt.Errorf("Bitget Spot bill IDs are not strictly descending within the page")
				}
				previousPageID = pageID
				entry, err := bitgetSpotBillEvidence(row, valuationCurrency, fetchRate, ctx)
				if err != nil {
					return nil, err
				}
				if entry.At.Before(start) || entry.At.After(end) {
					return nil, fmt.Errorf("Bitget Spot bill outside requested time range")
				}
				if old, exists := seen[entry.ID]; exists {
					if old != entry {
						return nil, fmt.Errorf("conflicting Bitget Spot bill identity")
					}
					continue
				}
				seen[entry.ID] = entry
				newRows++
			}
			if len(rows) < bitgetSpotBillsPageSize {
				break
			}
			last := rows[len(rows)-1]
			if newRows == 0 || last == nil || strings.TrimSpace(last.ID) == "" {
				return nil, fmt.Errorf("Bitget Spot bill pagination made no progress")
			}
			lastID, lastValid := new(big.Int).SetString(strings.TrimSpace(last.ID), 10)
			if !lastValid || lastID.Sign() <= 0 || cursor == last.ID {
				return nil, fmt.Errorf("Bitget Spot bill pagination made no progress")
			}
			if cursor != "" {
				previousID, previousValid := new(big.Int).SetString(cursor, 10)
				if !previousValid || lastID.Cmp(previousID) >= 0 {
					return nil, fmt.Errorf("Bitget Spot bill cursor did not advance toward older IDs")
				}
			}
			cursor = last.ID
			if page == bitgetSpotBillsMaxPages-1 {
				return nil, fmt.Errorf("Bitget Spot bill pagination budget exhausted")
			}
		}
		if end.Equal(through) {
			break
		}
		start = end.Add(time.Millisecond)
	}
	entries := make([]accounting.Entry, 0, len(seen))
	for _, entry := range seen {
		entries = append(entries, entry)
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].At.Equal(entries[j].At) {
			left, _ := new(big.Int).SetString(entries[i].Sequence, 10)
			right, _ := new(big.Int).SetString(entries[j].Sequence, 10)
			return left.Cmp(right) < 0
		}
		return entries[i].At.Before(entries[j].At)
	})
	return entries, nil
}

func (b *BitgetSpotAdapter) spotHistoricalRate(ctx context.Context, symbol string, at time.Time) (string, string, time.Time, error) {
	valuationAt := at.UTC().Truncate(time.Minute)
	openAt := valuationAt.Add(-time.Minute)
	query := url.Values{"symbol": {symbol}, "granularity": {"1min"}, "startTime": {strconv.FormatInt(openAt.UnixMilli(), 10)}, "endTime": {strconv.FormatInt(valuationAt.UnixMilli(), 10)}, "limit": {"10"}}
	response, err := b.client.DoRequest(ctx, "GET", "/api/v2/spot/market/candles?"+query.Encode(), nil)
	if err != nil {
		return "", "", time.Time{}, err
	}
	var rows [][]string
	if err := json.Unmarshal(response.Data, &rows); err != nil || rows == nil {
		return "", "", time.Time{}, fmt.Errorf("invalid Bitget Spot historical candle response")
	}
	var closePrice string
	for _, row := range rows {
		if len(row) < 5 || row[0] != strconv.FormatInt(openAt.UnixMilli(), 10) {
			continue
		}
		if closePrice != "" {
			return "", "", time.Time{}, fmt.Errorf("duplicate Bitget Spot historical candle")
		}
		price, parseErr := accounting.Decimal(row[4])
		if parseErr != nil || price.Sign() <= 0 {
			return "", "", time.Time{}, fmt.Errorf("invalid Bitget Spot historical close")
		}
		closePrice = price.FloatString(18)
	}
	if closePrice == "" {
		return "", "", time.Time{}, fmt.Errorf("Bitget Spot historical candle unavailable")
	}
	return closePrice, "bitget_spot_1m_completed_close:" + symbol, valuationAt, nil
}

func (b *BitgetSpotAdapter) ReadAccountEvidence(ctx context.Context, since time.Time) (accounting.Snapshot, error) {
	var empty accounting.Snapshot
	if b == nil || b.client == nil {
		return empty, errors.New("Bitget Spot account evidence source unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, bitgetEquityReadTimeout)
	defer cancel()
	started := time.Now()
	first, firstAt, err := b.readSpotAssets(ctx)
	if err != nil {
		return empty, err
	}
	firstBalances, err := spotWalletBalances(first)
	if err != nil {
		return empty, err
	}
	second, secondAt, err := b.readSpotAssets(ctx)
	if err != nil {
		return empty, err
	}
	secondBalances, err := spotWalletBalances(second)
	if err != nil {
		return empty, err
	}
	if len(firstBalances) != len(secondBalances) || time.Since(started) > bitgetEquityMaxCapture || secondAt < firstAt {
		return empty, fmt.Errorf("Bitget Spot wallet changed during capture; reconciliation required")
	}
	for coin, balance := range firstBalances {
		if secondBalances[coin] != balance {
			return empty, fmt.Errorf("Bitget Spot wallet changed during capture; reconciliation required")
		}
	}
	from := since.UTC().Truncate(time.Millisecond)
	if since.IsZero() {
		from = time.UnixMilli(firstAt).Add(-bitgetEquityOverlap)
	}
	through := time.UnixMilli(firstAt).Add(-time.Millisecond)
	if from.After(through) {
		return empty, fmt.Errorf("Bitget Spot bill cursor is newer than the captured wallet")
	}
	entries, err := b.readSpotBills(ctx, from, through, "USDT")
	if err != nil {
		return empty, err
	}
	for _, entry := range entries {
		if !entry.At.Before(time.UnixMilli(firstAt)) {
			return empty, fmt.Errorf("Bitget Spot bill changed during capture; reconciliation required")
		}
	}
	var tickers []struct{ Symbol, LastPr string }
	tickerResponse, err := b.client.DoRequest(ctx, "GET", "/api/v2/spot/market/tickers", nil)
	if err != nil {
		return empty, err
	}
	if err := json.Unmarshal(tickerResponse.Data, &tickers); err != nil || tickers == nil || tickerResponse.ReqTime < firstAt || time.Since(started) > bitgetEquityMaxCapture {
		return empty, fmt.Errorf("invalid or stale Bitget Spot valuation ticker snapshot")
	}
	prices := make(map[string][]string, len(tickers))
	for _, ticker := range tickers {
		key := strings.ToUpper(strings.TrimSpace(ticker.Symbol))
		if key != "" {
			prices[key] = append(prices[key], ticker.LastPr)
		}
	}
	balances := second
	equity, err := valueBitgetSpotBalancesUSDT(ctx, balances, func(_ context.Context, symbol string) (float64, error) {
		matches := prices[strings.ToUpper(symbol)]
		if len(matches) != 1 {
			return 0, fmt.Errorf("missing or duplicate Bitget Spot ticker")
		}
		price, err := strconv.ParseFloat(matches[0], 64)
		if err != nil || price <= 0 || math.IsNaN(price) || math.IsInf(price, 0) {
			return 0, fmt.Errorf("invalid Bitget Spot ticker")
		}
		return price, nil
	})
	if err != nil {
		return empty, fmt.Errorf("cannot value Bitget Spot equity: %w", err)
	}
	if math.IsNaN(equity) || math.IsInf(equity, 0) {
		return empty, fmt.Errorf("Bitget Spot equity is non-finite")
	}
	observedAt := time.Now().UTC()
	wallets := make(map[string]accounting.Wallet, len(secondBalances))
	for coin, balance := range secondBalances {
		wallets[coin] = accounting.Wallet{Currency: coin, Balance: balance, From: from, Through: through, ObservedAt: observedAt}
	}
	return accounting.Snapshot{Currency: "USDT", Equity: equity, ObservedAt: observedAt, Wallets: wallets, Entries: entries}, nil
}

var _ accounting.Source = (*BitgetSpotAdapter)(nil)
