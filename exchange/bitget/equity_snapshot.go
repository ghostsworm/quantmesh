package bitget

import (
	"context"
	"encoding/json"
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
	bitgetEquityReadTimeout = 15 * time.Second
	bitgetEquityMaxCapture  = accounting.MaxCaptureDuration
	bitgetEquityOverlap     = 5 * time.Minute
	bitgetBillPageSize      = 100
	bitgetBillMaxPages      = 120
	bitgetBillChunk         = 30 * 24 * time.Hour
)

type bitgetEquityAccount struct {
	MarginCoin       string            `json:"marginCoin"`
	Equity           string            `json:"accountEquity"`
	UnrealizedPL     string            `json:"unrealizedPL"`
	Coupon           string            `json:"coupon"`
	Grant            string            `json:"grant"`
	AssetMode        string            `json:"assetMode"`
	UnionTotalMargin string            `json:"unionTotalMargin"`
	UnionAvailable   string            `json:"unionAvailable"`
	UnionMaintenance string            `json:"unionMm"`
	UnionAssets      []json.RawMessage `json:"assetList"`
}

type bitgetBill struct {
	ID           string `json:"billId"`
	Symbol       string `json:"symbol"`
	Amount       string `json:"amount"`
	Fee          string `json:"fee"`
	FeeByCoupon  string `json:"feeByCoupon"`
	BusinessType string `json:"businessType"`
	Coin         string `json:"coin"`
	Time         string `json:"cTime"`
}

type bitgetBillPage struct {
	Bills []*bitgetBill `json:"bills"`
	EndID string        `json:"endId"`
}

func bitgetSingleUSDTWallet(account bitgetEquityAccount) (string, error) {
	if account.MarginCoin != "USDT" || account.AssetMode != "single" {
		return "", fmt.Errorf("Bitget equity requires single-asset USDT margin mode")
	}
	for field, raw := range map[string]string{
		"unionTotalMargin": account.UnionTotalMargin,
		"unionAvailable":   account.UnionAvailable,
		"unionMm":          account.UnionMaintenance,
	} {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		value, err := accounting.Decimal(strings.TrimSpace(raw))
		if err != nil || value.Sign() != 0 {
			return "", fmt.Errorf("Bitget single-asset account has unsupported %s", field)
		}
	}
	if len(account.UnionAssets) != 0 {
		return "", fmt.Errorf("Bitget single-asset account contains union asset rows")
	}
	equity, err := accounting.Decimal(account.Equity)
	if err != nil {
		return "", err
	}
	unrealized, err := accounting.Decimal(account.UnrealizedPL)
	if err != nil {
		return "", err
	}
	coupon, err := accounting.Decimal(account.Coupon)
	if err != nil || coupon.Sign() != 0 {
		return "", fmt.Errorf("Bitget futures coupon balance is unsupported")
	}
	grant, err := accounting.Decimal(account.Grant)
	if err != nil || grant.Sign() != 0 {
		return "", fmt.Errorf("Bitget futures grant balance is unsupported")
	}
	wallet := new(big.Rat).Sub(equity, unrealized)
	if wallet.Sign() < 0 {
		return "", fmt.Errorf("negative Bitget wallet balance")
	}
	return wallet.FloatString(18), nil
}

func bitgetBillEvidence(row *bitgetBill) (accounting.Entry, error) {
	if row == nil || strings.TrimSpace(row.ID) == "" || row.Coin != "USDT" || strings.TrimSpace(row.BusinessType) == "" {
		return accounting.Entry{}, fmt.Errorf("Bitget bill identity or currency unsupported")
	}
	ms, err := strconv.ParseInt(row.Time, 10, 64)
	if err != nil || ms <= 0 {
		return accounting.Entry{}, fmt.Errorf("invalid Bitget bill timestamp")
	}
	amount, err := accounting.Decimal(row.Amount)
	if err != nil {
		return accounting.Entry{}, err
	}
	fee, err := accounting.Decimal(row.Fee)
	if err != nil {
		return accounting.Entry{}, err
	}
	feeByCoupon := strings.TrimSpace(row.FeeByCoupon)
	if feeByCoupon != "" {
		coupon, parseErr := accounting.Decimal(feeByCoupon)
		if parseErr != nil || coupon.Sign() != 0 {
			return accounting.Entry{}, fmt.Errorf("Bitget coupon-paid fee is unsupported")
		}
	}
	kind, err := bitgetBillKind(row.BusinessType, amount)
	if err != nil {
		return accounting.Entry{}, err
	}
	net := new(big.Rat).Add(amount, fee)
	return accounting.Entry{ID: row.BusinessType + ":" + row.ID, Kind: kind, Currency: "USDT", Amount: net.FloatString(18), At: time.UnixMilli(ms).UTC()}, nil
}

func bitgetBillKind(businessType string, amount *big.Rat) (string, error) {
	switch businessType {
	case "trans_from_exchange", "trans_from_contract", "trans_from_otc", "trans_from_cross", "trans_from_isolated":
		if amount.Sign() < 0 {
			return "", fmt.Errorf("negative incoming Bitget transfer")
		}
		return "transfer_in", nil
	case "trans_to_exchange", "trans_to_contract", "trans_to_otc", "trans_to_cross", "trans_to_isolated":
		if amount.Sign() > 0 {
			return "", fmt.Errorf("positive outgoing Bitget transfer")
		}
		return "transfer_out", nil
	case "contract_settle_fee":
		return "funding", nil
	case "settle_interest":
		return "interest", nil
	case "cash_gift_issue", "bonus_issue":
		if amount.Sign() < 0 {
			return "", fmt.Errorf("negative Bitget promotional capital credit")
		}
		return "transfer_in", nil
	case "cash_gift_recycle", "bonus_recycle", "bonus_expired":
		if amount.Sign() > 0 {
			return "", fmt.Errorf("positive Bitget promotional capital debit")
		}
		return "transfer_out", nil
	case "open_long", "open_short", "close_long", "close_short", "force_close_long", "force_close_short",
		"burst_long_loss_query", "burst_short_loss_query", "buy", "sell", "force_buy", "force_sell", "burst_buy", "burst_sell",
		"delivery_long", "delivery_short", "adl_close_long", "adl_close_short", "adl_buy_in_single_side_mode", "adl_sell_in_single_side_mode",
		"tracking_follow_pay", "tracking_follow_back", "tracking_trader_income":
		return "realized_pnl", nil
	default:
		return "", fmt.Errorf("unclassified Bitget account bill type %q", businessType)
	}
}

func (b *BitgetAdapter) readEquityAccount(ctx context.Context) (bitgetEquityAccount, int64, error) {
	path := "/api/v2/mix/account/accounts?productType=USDT-FUTURES"
	response, err := b.client.DoRequest(ctx, "GET", path, nil)
	if err != nil {
		return bitgetEquityAccount{}, 0, err
	}
	var accounts []bitgetEquityAccount
	if err := json.Unmarshal(response.Data, &accounts); err != nil {
		return bitgetEquityAccount{}, 0, fmt.Errorf("decode Bitget equity accounts: %w", err)
	}
	if len(accounts) != 1 {
		return bitgetEquityAccount{}, 0, fmt.Errorf("Bitget USDT product returned %d account rows", len(accounts))
	}
	if _, err := bitgetSingleUSDTWallet(accounts[0]); err != nil {
		return bitgetEquityAccount{}, 0, err
	}
	if response.ReqTime <= 0 {
		return bitgetEquityAccount{}, 0, fmt.Errorf("Bitget equity response lacks exchange capture time")
	}
	return accounts[0], response.ReqTime, nil
}

func (b *BitgetAdapter) readEquityBills(ctx context.Context, from, through time.Time) ([]accounting.Entry, error) {
	if from.IsZero() || from.After(through) || from.Before(through.AddDate(0, 0, -90)) {
		return nil, fmt.Errorf("Bitget bill interval invalid or outside 90-day retention")
	}
	seen := make(map[string]accounting.Entry)
	start := from
	for start.Before(through) || start.Equal(through) {
		end := start.Add(bitgetBillChunk)
		if end.After(through) {
			end = through
		}
		cursor := ""
		for pageNumber := 0; pageNumber < bitgetBillMaxPages; pageNumber++ {
			query := url.Values{
				"productType": {"USDT-FUTURES"},
				// The accounting classifier requires trade and transfer bills too.
				// Bitget's onlyFunding=yes filters those non-funding records out.
				"onlyFunding": {"no"},
				"startTime":   {strconv.FormatInt(start.UnixMilli(), 10)},
				"endTime":     {strconv.FormatInt(end.UnixMilli(), 10)},
				"limit":       {strconv.Itoa(bitgetBillPageSize)},
			}
			if cursor != "" {
				query.Set("idLessThan", cursor)
			}
			response, err := b.client.DoRequest(ctx, "GET", "/api/v2/mix/account/bill?"+query.Encode(), nil)
			if err != nil {
				return nil, err
			}
			var result bitgetBillPage
			if err := json.Unmarshal(response.Data, &result); err != nil || result.Bills == nil || len(result.Bills) > bitgetBillPageSize {
				return nil, fmt.Errorf("invalid Bitget bill page")
			}
			added := 0
			for _, row := range result.Bills {
				entry, err := bitgetBillEvidence(row)
				if err != nil {
					return nil, err
				}
				if entry.At.Before(start) || entry.At.After(end) {
					return nil, fmt.Errorf("Bitget bill outside requested interval")
				}
				if old, exists := seen[entry.ID]; exists {
					if old != entry {
						return nil, fmt.Errorf("conflicting Bitget bill identity")
					}
					continue
				}
				seen[entry.ID] = entry
				added++
			}
			if len(result.Bills) == 0 || len(result.Bills) < bitgetBillPageSize {
				break
			}
			last := result.Bills[len(result.Bills)-1]
			if added == 0 || last == nil || result.EndID == "" || result.EndID != last.ID || result.EndID == cursor {
				return nil, fmt.Errorf("Bitget bill pagination made no progress")
			}
			cursor = result.EndID
			if pageNumber == bitgetBillMaxPages-1 {
				return nil, fmt.Errorf("Bitget bill pagination budget exhausted")
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
			return entries[i].ID < entries[j].ID
		}
		return entries[i].At.Before(entries[j].At)
	})
	return entries, nil
}

// ReadAccountEvidence emits only candidate single-USDT classic futures evidence.
// Wallet deltas still have to match every bill before the risk feeder can trust it.
func (b *BitgetAdapter) ReadAccountEvidence(ctx context.Context, since time.Time) (accounting.Snapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, bitgetEquityReadTimeout)
	defer cancel()
	var empty accounting.Snapshot
	localStart := time.Now()
	first, firstTime, err := b.readEquityAccount(ctx)
	if err != nil {
		return empty, err
	}
	firstWallet, err := bitgetSingleUSDTWallet(first)
	if err != nil {
		return empty, err
	}
	second, secondTime, err := b.readEquityAccount(ctx)
	if err != nil {
		return empty, err
	}
	secondWallet, err := bitgetSingleUSDTWallet(second)
	if err != nil {
		return empty, err
	}
	if firstWallet != secondWallet || secondTime < firstTime || time.Since(localStart) > bitgetEquityMaxCapture {
		return empty, fmt.Errorf("Bitget account wallet changed during capture; reconciliation required")
	}
	localObserved := time.Now().UTC()
	from := since.UTC().Truncate(time.Millisecond)
	if since.IsZero() {
		from = time.UnixMilli(firstTime).Add(-bitgetEquityOverlap)
	}
	through := time.UnixMilli(firstTime).Add(-time.Millisecond)
	if from.After(through) {
		return empty, fmt.Errorf("Bitget bill cursor is newer than account capture")
	}
	entries, err := b.readEquityBills(ctx, from, through)
	if err != nil {
		return empty, err
	}
	for _, entry := range entries {
		if !entry.At.Before(time.UnixMilli(firstTime)) {
			return empty, fmt.Errorf("Bitget account bill changed during capture; reconciliation required")
		}
	}
	equityRat, err := accounting.Decimal(second.Equity)
	if err != nil {
		return empty, err
	}
	equity, _ := equityRat.Float64()
	if math.IsNaN(equity) || math.IsInf(equity, 0) {
		return empty, fmt.Errorf("non-finite Bitget account equity")
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	return accounting.Snapshot{Currency: "USDT", Equity: equity, ObservedAt: localObserved,
		Wallet: accounting.Wallet{Balance: secondWallet, From: from, Through: through, ObservedAt: localObserved}, Entries: entries}, nil
}

var _ accounting.Source = (*BitgetAdapter)(nil)
