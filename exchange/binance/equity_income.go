package binance

import (
	"context"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"time"

	"quantmesh/exchange/accounting"
)

const (
	equityIncomePageSize = 1000
	equityMaxPages       = 40
	equityPageInterval   = 100 * time.Millisecond
)

type equityIncomeWire struct {
	Type          string `json:"incomeType"`
	Amount        string `json:"income"`
	Asset         string `json:"asset"`
	Time          int64  `json:"time"`
	TransactionID int64  `json:"tranId"`
}

func incomeEvidence(row *equityIncomeWire) (accounting.Entry, error) {
	if row == nil || row.TransactionID <= 0 || row.Time <= 0 || row.Asset != "USDT" {
		return accounting.Entry{}, fmt.Errorf("income identity or valuation unsupported")
	}
	amount, err := accounting.CanonicalDecimal(row.Amount)
	if err != nil {
		return accounting.Entry{}, err
	}
	r, err := accounting.Decimal(amount)
	if err != nil {
		return accounting.Entry{}, err
	}
	kind := ""
	switch row.Type {
	case "TRANSFER", "INTERNAL_TRANSFER":
		kind = "transfer_in"
		if r.Sign() < 0 {
			kind = "transfer_out"
		}
	case "FUNDING_FEE":
		kind = "funding"
	case "REALIZED_PNL":
		kind = "realized_pnl"
	case "INSURANCE_CLEAR":
		kind = "insurance_clear"
	case "COMMISSION":
		kind = "fee"
		if r.Sign() > 0 {
			return accounting.Entry{}, fmt.Errorf("positive commission requires explicit rebate classification")
		}
	case "POSITION_LIMIT_INCREASE_FEE":
		kind = "unallocated_fee"
	case "WELCOME_BONUS", "REFERRAL_KICKBACK", "COMMISSION_REBATE", "API_REBATE", "CONTEST_REWARD", "FEE_RETURN", "BFUSD_REWARD":
		if r.Sign() < 0 {
			return accounting.Entry{}, fmt.Errorf("negative rebate requires explicit clawback classification")
		}
		kind = "rebate"
	default:
		return accounting.Entry{}, fmt.Errorf("unclassified Binance income type")
	}
	// tranId is unique only within one incomeType, never globally.
	return accounting.Entry{ID: row.Type + ":" + strconv.FormatInt(row.TransactionID, 10), Kind: kind, Currency: row.Asset, Amount: amount, At: time.UnixMilli(row.Time).UTC()}, nil
}

func (b *BinanceAdapter) readEquityIncome(ctx context.Context, from, through, server time.Time) ([]accounting.Entry, error) {
	if from.IsZero() || from.After(through) || through.After(server) || from.Before(server.AddDate(0, -3, 1)) {
		return nil, fmt.Errorf("income interval invalid or outside reliable retention window")
	}
	seen := make(map[string]accounting.Entry)
	localAnchor := time.Now()
	for page := 1; page <= equityMaxPages; page++ {
		if page > 1 {
			timer := time.NewTimer(equityPageInterval)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
		}
		values := url.Values{"startTime": {strconv.FormatInt(from.UnixMilli(), 10)}, "endTime": {strconv.FormatInt(through.UnixMilli(), 10)}, "page": {strconv.Itoa(page)}, "limit": {strconv.Itoa(equityIncomePageSize)}}
		var rows []*equityIncomeWire
		// Advance the signing time, but keep the ledger interval fixed for paging.
		if err := b.equityGET(ctx, "/fapi/v1/income", values, server.Add(time.Since(localAnchor)).UnixMilli(), &rows); err != nil {
			return nil, err
		}
		if rows == nil || len(rows) > equityIncomePageSize {
			return nil, fmt.Errorf("invalid income page shape")
		}
		added := 0
		for _, row := range rows {
			entry, err := incomeEvidence(row)
			if err != nil {
				return nil, err
			}
			if entry.At.Before(from) || entry.At.After(through) {
				return nil, fmt.Errorf("income outside requested interval")
			}
			if old, exists := seen[entry.ID]; exists {
				if old != entry {
					return nil, fmt.Errorf("conflicting income identity across pages")
				}
				continue
			}
			seen[entry.ID] = entry
			added++
		}
		if len(rows) > 0 && added == 0 {
			return nil, fmt.Errorf("income pagination made no progress")
		}
		if len(rows) < equityIncomePageSize {
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
	}
	return nil, fmt.Errorf("income pagination budget exhausted; coverage incomplete")
}
