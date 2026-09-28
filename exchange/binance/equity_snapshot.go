package binance

import (
	"context"
	"fmt"
	"math"
	"math/big"
	"time"

	"quantmesh/exchange/accounting"
)

const (
	equityClockTolerance = accounting.MaxClockSkew
	equityCaptureLimit   = accounting.MaxCaptureDuration
	equityReadLimit      = 15 * time.Second
	equityInitialOverlap = 5 * time.Minute
)

type equityAssetWire struct {
	Asset         string `json:"asset"`
	Wallet        string `json:"walletBalance"`
	Available     string `json:"availableBalance"`
	MaxWithdraw   string `json:"maxWithdrawAmount"`
	Unrealized    string `json:"unrealizedProfit"`
	Margin        string `json:"marginBalance"`
	InitialMargin string `json:"initialMargin"`
	UpdatedAt     *int64 `json:"updateTime"`
}

type equityAccountWire struct {
	MultiAsset *bool             `json:"multiAssetsMargin"`
	Wallet     string            `json:"totalWalletBalance"`
	Unrealized string            `json:"totalUnrealizedProfit"`
	Margin     string            `json:"totalMarginBalance"`
	Assets     []equityAssetWire `json:"assets"`
}

type equityAccountSample struct {
	wallet      string
	equity      float64
	available   float64
	maxWithdraw float64
	updatedAt   int64
}

func accountAmounts(wallet, unrealized, margin string) (*big.Rat, *big.Rat, error) {
	w, err := accounting.Decimal(wallet)
	if err != nil {
		return nil, nil, err
	}
	u, err := accounting.Decimal(unrealized)
	if err != nil {
		return nil, nil, err
	}
	m, err := accounting.Decimal(margin)
	if err != nil {
		return nil, nil, err
	}
	if new(big.Rat).Add(w, u).Cmp(m) != 0 {
		return nil, nil, fmt.Errorf("inconsistent account balance arithmetic")
	}
	return w, m, nil
}

func (a equityAccountWire) sample() (equityAccountSample, error) {
	if a.MultiAsset == nil || *a.MultiAsset {
		return equityAccountSample{}, fmt.Errorf("equity evidence requires explicit single-asset mode")
	}
	topWallet, topMargin, err := accountAmounts(a.Wallet, a.Unrealized, a.Margin)
	if err != nil {
		return equityAccountSample{}, err
	}
	seen := make(map[string]bool)
	var sample equityAccountSample
	for _, asset := range a.Assets {
		if asset.Asset == "" || seen[asset.Asset] {
			return sample, fmt.Errorf("missing or duplicate account asset")
		}
		seen[asset.Asset] = true
		wallet, margin, err := accountAmounts(asset.Wallet, asset.Unrealized, asset.Margin)
		if err != nil {
			return sample, err
		}
		initial, err := accounting.Decimal(asset.InitialMargin)
		if err != nil || initial.Sign() < 0 {
			return sample, fmt.Errorf("invalid account initial margin")
		}
		available, err := accounting.Decimal(asset.Available)
		if err != nil || available.Sign() < 0 || available.Cmp(margin) > 0 {
			return sample, fmt.Errorf("invalid account available balance")
		}
		maxWithdraw, err := accounting.Decimal(asset.MaxWithdraw)
		if err != nil || maxWithdraw.Sign() < 0 || maxWithdraw.Cmp(available) > 0 {
			return sample, fmt.Errorf("invalid account maximum withdrawal amount")
		}
		if asset.Asset != "USDT" {
			if wallet.Sign() != 0 || margin.Sign() != 0 || initial.Sign() != 0 {
				return sample, fmt.Errorf("non-USDT account exposure requires valuation")
			}
			continue
		}
		if asset.UpdatedAt == nil || *asset.UpdatedAt < 0 || wallet.Cmp(topWallet) != 0 || margin.Cmp(topMargin) != 0 {
			return sample, fmt.Errorf("inconsistent USDT account totals or update cursor")
		}
		equity, _ := margin.Float64()
		if math.IsNaN(equity) || math.IsInf(equity, 0) {
			return sample, fmt.Errorf("non-finite account equity")
		}
		availableFloat, _ := available.Float64()
		if math.IsNaN(availableFloat) || math.IsInf(availableFloat, 0) {
			return sample, fmt.Errorf("non-finite account available balance")
		}
		maxWithdrawFloat, _ := maxWithdraw.Float64()
		if math.IsNaN(maxWithdrawFloat) || math.IsInf(maxWithdrawFloat, 0) {
			return sample, fmt.Errorf("non-finite account maximum withdrawal amount")
		}
		sample = equityAccountSample{wallet: wallet.FloatString(18), equity: equity, available: availableFloat,
			maxWithdraw: maxWithdrawFloat, updatedAt: *asset.UpdatedAt}
	}
	if !seen["USDT"] {
		return sample, fmt.Errorf("USDT account asset missing")
	}
	return sample, nil
}

func (b *BinanceAdapter) readEquityAccount(ctx context.Context, signedAt time.Time) (equityAccountSample, error) {
	var wire equityAccountWire
	if err := b.equityGET(ctx, "/fapi/v2/account", nil, signedAt.UnixMilli(), &wire); err != nil {
		return equityAccountSample{}, err
	}
	return wire.sample()
}

// ReadAccountEvidence bypasses GetAccount's cache. This produces a candidate
// sample, NOT independent proof of complete financial accounting. The consumer
// must reconcile exact wallet deltas against all ledger entries and durable
// per-account cursors before clearing an equity-health gate.
func (b *BinanceAdapter) ReadAccountEvidence(ctx context.Context, since time.Time) (accounting.Snapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, equityReadLimit)
	defer cancel()
	var empty accounting.Snapshot
	start, err := b.equityServerTime(ctx)
	if err != nil {
		return empty, err
	}
	localAnchor := time.Now()
	first, err := b.readEquityAccount(ctx, start)
	if err != nil {
		return empty, err
	}
	observedAt := time.Now().UTC()
	last, err := b.readEquityAccount(ctx, start.Add(time.Since(localAnchor)))
	if err != nil {
		return empty, err
	}
	end, err := b.equityServerTime(ctx)
	if err != nil {
		return empty, err
	}
	if end.Before(start) || end.Sub(start) > equityCaptureLimit || time.Since(localAnchor) > equityCaptureLimit {
		return empty, fmt.Errorf("account capture clock moved or capture expired")
	}
	if first.wallet != last.wallet || first.updatedAt != last.updatedAt || last.updatedAt >= start.UnixMilli() {
		return empty, fmt.Errorf("account wallet changed during capture; reconciliation required")
	}
	from := since.UTC().Truncate(time.Millisecond)
	if since.IsZero() {
		from = start.Add(-equityInitialOverlap)
	}
	if from.After(start.Add(-time.Millisecond)) {
		return empty, fmt.Errorf("ledger cursor is newer than account capture")
	}
	entries, err := b.readEquityIncome(ctx, from, end, end)
	if err != nil {
		return empty, err
	}
	for _, entry := range entries {
		if !entry.At.Before(start) {
			return empty, fmt.Errorf("account ledger changed during capture; reconciliation required")
		}
	}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	return accounting.Snapshot{Currency: "USDT", Equity: last.equity, ObservedAt: observedAt,
		Wallet: accounting.Wallet{Balance: last.wallet, From: from, Through: start.Add(-time.Millisecond), ObservedAt: observedAt}, Entries: entries}, nil
}

// GetAccountFresh bypasses the general account cache and only reports a
// transferable balance when Binance confirms single-asset USDT mode.
func (b *BinanceAdapter) GetAccountFresh(ctx context.Context) (*Account, error) {
	if b == nil || b.client == nil {
		return nil, fmt.Errorf("Binance account adapter unavailable")
	}
	ctx, cancel := context.WithTimeout(ctx, equityReadLimit)
	defer cancel()
	serverNow, err := b.equityServerTime(ctx)
	if err != nil {
		return nil, err
	}
	sample, err := b.readEquityAccount(ctx, serverNow)
	if err != nil {
		return nil, err
	}
	wallet, err := accounting.Decimal(sample.wallet)
	if err != nil {
		return nil, fmt.Errorf("parse fresh account wallet balance: %w", err)
	}
	walletFloat, _ := wallet.Float64()
	if math.IsNaN(walletFloat) || math.IsInf(walletFloat, 0) {
		return nil, fmt.Errorf("fresh account wallet balance is non-finite")
	}
	return &Account{TotalWalletBalance: walletFloat, TotalMarginBalance: sample.equity,
		AvailableBalance: sample.available, MaxWithdrawAmount: sample.maxWithdraw, BalanceAsset: "USDT"}, nil
}

var _ accounting.Source = (*BinanceAdapter)(nil)
