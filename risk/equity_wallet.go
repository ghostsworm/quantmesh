package risk

import (
	"fmt"
	"math/big"
	"strings"
	"time"

	"quantmesh/exchange/accounting"
)

func walletCurrency(w accounting.Wallet, fallback string) string {
	if w.Currency != "" {
		return w.Currency
	}
	return fallback
}

func validateWallet(w accounting.Wallet) error {
	if _, err := accounting.Decimal(w.Balance); err != nil {
		return err
	}
	if w.From.IsZero() || w.Through.IsZero() || w.From.After(w.Through) || w.ObservedAt.IsZero() {
		return fmt.Errorf("invalid account wallet coverage")
	}
	if w.Through.Sub(w.ObservedAt) > accounting.MaxClockSkew || w.ObservedAt.Sub(w.Through) > accounting.MaxClockSkew+accounting.MaxCaptureDuration+time.Millisecond {
		return fmt.Errorf("account wallet cursor is inconsistent with capture time")
	}
	return nil
}

func exactFlow(f EquityCashFlow, currency, expectedWalletCurrency string) (*big.Rat, error) {
	if _, err := f.externalAmount(currency); err != nil {
		return nil, err
	}
	if f.Account == "" {
		return nil, fmt.Errorf("ledger account missing")
	}
	flowCurrency := f.WalletCurrency
	if flowCurrency == "" {
		flowCurrency = f.Currency
	}
	if flowCurrency != expectedWalletCurrency {
		return nil, fmt.Errorf("ledger wallet currency mismatch")
	}
	exact, err := accounting.Decimal(f.ExactAmount)
	if err != nil {
		return nil, err
	}
	rateText := f.ValuationRate
	if rateText == "" {
		if expectedWalletCurrency != currency {
			return nil, fmt.Errorf("non-valuation wallet currency lacks a verified conversion rate")
		}
		rateText = "1"
	}
	rate, err := accounting.Decimal(rateText)
	if err != nil || rate.Sign() <= 0 {
		return nil, fmt.Errorf("invalid ledger valuation rate")
	}
	if expectedWalletCurrency == currency {
		if rate.Cmp(big.NewRat(1, 1)) != 0 {
			return nil, fmt.Errorf("same-currency ledger valuation rate must equal one")
		}
		if f.ValuationSource != "" || !f.ValuationAt.IsZero() {
			return nil, fmt.Errorf("same-currency ledger has unexpected conversion provenance")
		}
	} else if strings.TrimSpace(f.ValuationSource) == "" || f.ValuationAt.IsZero() || f.ValuationAt.After(f.At) || f.At.Sub(f.ValuationAt) > time.Minute {
		return nil, fmt.Errorf("non-valuation ledger amount lacks timely conversion provenance")
	}
	valued := new(big.Rat).Mul(exact, rate)
	approx, _ := valued.Float64()
	if approx != f.Amount {
		return nil, fmt.Errorf("valued ledger amount differs from exact amount and rate")
	}
	return exact, nil
}

func exactValuedFlow(f EquityCashFlow) (*big.Rat, error) {
	amount, err := accounting.Decimal(f.ExactAmount)
	if err != nil {
		return nil, err
	}
	rate := f.ValuationRate
	if rate == "" {
		rate = "1"
	}
	parsedRate, err := accounting.Decimal(rate)
	if err != nil || parsedRate.Sign() <= 0 {
		return nil, fmt.Errorf("invalid ledger valuation rate")
	}
	return new(big.Rat).Mul(amount, parsedRate), nil
}

// Validate the entire durable journal against its immutable per-account wallet
// baseline. Checking only external transfers would miss unrecorded fees/funding.
func (s EquityCheckpoint) walletLedgerTotal() (float64, error) {
	if !s.CashFlowAdjusted || len(s.Wallets) == 0 || len(s.BaseWallets) != len(s.Wallets) {
		return 0, fmt.Errorf("invalid wallet checkpoint scope")
	}
	sums := make(map[string]*big.Rat, len(s.Wallets))
	var baseObserved, lastObserved time.Time
	for account, wallet := range s.Wallets {
		base, ok := s.BaseWallets[account]
		if account == "" || !ok {
			return 0, fmt.Errorf("wallet checkpoint account changed")
		}
		currency := walletCurrency(wallet, s.Currency)
		if currency == "" || walletCurrency(base, s.Currency) != currency {
			return 0, fmt.Errorf("wallet currency missing")
		}
		if err := validateWallet(wallet); err != nil {
			return 0, err
		}
		if err := validateWallet(base); err != nil {
			return 0, err
		}
		if wallet.Through.Before(base.Through) || wallet.From.Before(base.From) || wallet.ObservedAt.Before(base.ObservedAt) {
			return 0, fmt.Errorf("wallet checkpoint cursor regressed")
		}
		if baseObserved.IsZero() || base.ObservedAt.Before(baseObserved) {
			baseObserved = base.ObservedAt
		}
		if lastObserved.IsZero() || wallet.ObservedAt.Before(lastObserved) {
			lastObserved = wallet.ObservedAt
		}
		sums[account] = new(big.Rat)
	}
	if !baseObserved.Equal(s.BaseAt) || !lastObserved.Equal(s.LastAt) {
		return 0, fmt.Errorf("wallet checkpoint observation identity changed")
	}
	external := new(big.Rat)
	for id, flow := range s.Receipts {
		wallet, ok := s.Wallets[flow.Account]
		base := s.BaseWallets[flow.Account]
		if !ok || id != flow.ID || flow.At.Before(base.From) || flow.At.After(wallet.Through) {
			return 0, fmt.Errorf("wallet receipt outside account coverage")
		}
		walletCurrency := walletCurrency(wallet, s.Currency)
		amount, err := exactFlow(flow, s.Currency, walletCurrency)
		if err != nil {
			return 0, err
		}
		if !flow.At.After(base.Through) {
			continue
		}
		sums[flow.Account].Add(sums[flow.Account], amount)
		switch flow.Kind {
		case "deposit", "withdrawal", "transfer_in", "transfer_out":
			valued, err := exactValuedFlow(flow)
			if err != nil {
				return 0, err
			}
			external.Add(external, valued)
		}
	}
	for account, wallet := range s.Wallets {
		balance, _ := accounting.Decimal(wallet.Balance)
		base, _ := accounting.Decimal(s.BaseWallets[account].Balance)
		if balance.Sub(balance, base).Cmp(sums[account]) != 0 {
			return 0, fmt.Errorf("wallet delta does not reconcile with complete ledger")
		}
	}
	total, _ := external.Float64()
	if !finiteEquity(total) {
		return 0, fmt.Errorf("non-finite external capital total")
	}
	return total, nil
}

func cloneWallets(wallets map[string]accounting.Wallet) map[string]accounting.Wallet {
	copy := make(map[string]accounting.Wallet, len(wallets))
	for account, wallet := range wallets {
		copy[account] = wallet
	}
	return copy
}

func walletObservation(o EquityObservation, now time.Time, maxAge time.Duration) (map[string]accounting.Wallet, error) {
	if !o.CashFlowComplete {
		return nil, fmt.Errorf("wallet ledger coverage is incomplete")
	}
	wallets := cloneWallets(o.Wallets)
	var oldest, newest time.Time
	for account, wallet := range wallets {
		if account == "" {
			return nil, fmt.Errorf("empty wallet account identity")
		}
		if err := validateWallet(wallet); err != nil {
			return nil, err
		}
		if wallet.ObservedAt.After(now) || now.Sub(wallet.ObservedAt) > maxAge {
			return nil, fmt.Errorf("stale or future account capture")
		}
		if oldest.IsZero() || wallet.ObservedAt.Before(oldest) {
			oldest = wallet.ObservedAt
		}
		if newest.IsZero() || wallet.ObservedAt.After(newest) {
			newest = wallet.ObservedAt
		}
		canonical, err := accounting.CanonicalDecimal(wallet.Balance)
		if err != nil {
			return nil, err
		}
		wallet.Balance = canonical
		wallet.Currency = walletCurrency(wallet, o.Currency)
		wallets[account] = wallet
	}
	if !oldest.Equal(o.ObservedAt) {
		return nil, fmt.Errorf("aggregate equity time must match oldest account capture")
	}
	if newest.Sub(oldest) > accounting.MaxCaptureDuration {
		return nil, fmt.Errorf("account equity captures exceed maximum aggregate window")
	}
	return wallets, nil
}

func sameWalletReceipt(a, b EquityCashFlow) bool {
	return a.ID == b.ID && a.Account == b.Account && a.Kind == b.Kind && a.Currency == b.Currency && a.WalletCurrency == b.WalletCurrency && a.ValuationRate == b.ValuationRate && a.ValuationSource == b.ValuationSource && a.ValuationAt.Equal(b.ValuationAt) && a.Amount == b.Amount && a.ExactAmount == b.ExactAmount && a.At.Equal(b.At)
}

func nextWalletEquityCheckpoint(previous *EquityCheckpoint, o EquityObservation, now, reset time.Time, maxAge time.Duration) (EquityCheckpoint, error) {
	wallets, err := walletObservation(o, now, maxAge)
	if err != nil {
		return EquityCheckpoint{}, err
	}
	s := EquityCheckpoint{Version: equityStateVersion, Scope: o.Scope, Currency: o.Currency, CashFlowAdjusted: true, BaseAt: o.ObservedAt, ResetAt: reset,
		HighWater: o.Equity, Receipts: make(map[string]EquityCashFlow), BaseWallets: cloneWallets(wallets), Wallets: wallets}
	baseline := previous == nil
	if previous != nil {
		if err := previous.validate(); err != nil {
			return s, err
		}
		if previous.Scope != o.Scope || previous.Currency != o.Currency || o.ObservedAt.Before(previous.LastAt) {
			return s, fmt.Errorf("wallet scope changed or observation regressed")
		}
		baseline = reset.After(previous.ResetAt)
		if len(previous.Wallets) > 0 {
			if len(wallets) != len(previous.Wallets) {
				return s, fmt.Errorf("wallet account membership changed")
			}
			for account, wallet := range wallets {
				old, ok := previous.Wallets[account]
				if !ok || wallet.Through.Before(old.Through) || wallet.ObservedAt.Before(old.ObservedAt) {
					return s, fmt.Errorf("wallet account changed or cursor regressed")
				}
			}
		} else if !baseline {
			return s, fmt.Errorf("wallet accounting mode requires explicit reconciliation/reset")
		}
		s.Revision = previous.Revision
		if !baseline {
			s = *previous
			s.Wallets = wallets
			s.BaseWallets = cloneWallets(previous.BaseWallets)
			s.Receipts = make(map[string]EquityCashFlow, len(previous.Receipts))
			for id, receipt := range previous.Receipts {
				s.Receipts[id] = receipt
			}
			if o.ObservedAt.Equal(previous.LastAt) && o.Equity != previous.LastEquity {
				return s, fmt.Errorf("equity changed without new observation")
			}
			for account, wallet := range wallets {
				from := maxTime(previous.BaseWallets[account].From, previous.Wallets[account].Through.Add(-realizedCursorOverlap))
				if wallet.From.After(from) || wallet.From.Before(previous.BaseWallets[account].From) {
					return s, fmt.Errorf("wallet ledger overlap has a gap or regressed before baseline coverage")
				}
			}
		}
	}
	observed := make(map[string]EquityCashFlow, len(o.Flows))
	for _, flow := range o.Flows {
		wallet, ok := wallets[flow.Account]
		if !ok || flow.At.Before(wallet.From) || flow.At.After(wallet.Through) {
			return s, fmt.Errorf("ledger receipt outside account observation")
		}
		if _, err := exactFlow(flow, o.Currency, walletCurrency(wallet, s.Currency)); err != nil {
			return s, err
		}
		flow.ExactAmount, err = accounting.CanonicalDecimal(flow.ExactAmount)
		if err != nil {
			return s, err
		}
		if flow.ValuationRate != "" {
			flow.ValuationRate, err = accounting.CanonicalDecimal(flow.ValuationRate)
			if err != nil {
				return s, err
			}
		}
		if old, ok := observed[flow.ID]; ok && !sameWalletReceipt(old, flow) {
			return s, fmt.Errorf("conflicting wallet receipt identity")
		}
		observed[flow.ID] = flow
		if old, ok := s.Receipts[flow.ID]; ok {
			if !sameWalletReceipt(old, flow) {
				return s, fmt.Errorf("durable wallet receipt changed; reconciliation required")
			}
			continue
		}
		if !baseline && !flow.At.After(previous.Wallets[flow.Account].Through) {
			return s, fmt.Errorf("late wallet receipt requires historical reconciliation")
		}
		s.Receipts[flow.ID] = flow
	}
	if !baseline {
		for id, flow := range previous.Receipts {
			if !flow.At.Before(wallets[flow.Account].From) {
				if _, ok := observed[id]; !ok {
					return s, fmt.Errorf("durable wallet receipt missing from complete overlap")
				}
			}
		}
	}
	s.LastAt = o.ObservedAt
	s.ExternalFlows, err = s.walletLedgerTotal()
	if err != nil {
		return s, err
	}
	return finishEquityCheckpoint(s, o)
}
