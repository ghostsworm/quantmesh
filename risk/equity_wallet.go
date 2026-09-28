package risk

import (
	"fmt"
	"math/big"
	"time"

	"quantmesh/exchange/accounting"
)

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

func exactFlow(f EquityCashFlow, currency string) (*big.Rat, error) {
	if _, err := f.externalAmount(currency); err != nil {
		return nil, err
	}
	if f.Account == "" {
		return nil, fmt.Errorf("ledger account missing")
	}
	exact, err := accounting.Decimal(f.ExactAmount)
	if err != nil {
		return nil, err
	}
	approx, _ := exact.Float64()
	if approx != f.Amount {
		return nil, fmt.Errorf("ledger amount differs from exact amount")
	}
	return exact, nil
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
		amount, err := exactFlow(flow, s.Currency)
		if err != nil {
			return 0, err
		}
		if !flow.At.After(base.Through) {
			continue
		}
		sums[flow.Account].Add(sums[flow.Account], amount)
		switch flow.Kind {
		case "deposit", "withdrawal", "transfer_in", "transfer_out":
			external.Add(external, amount)
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
	var oldest time.Time
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
		canonical, err := accounting.CanonicalDecimal(wallet.Balance)
		if err != nil {
			return nil, err
		}
		wallet.Balance = canonical
		wallets[account] = wallet
	}
	if !oldest.Equal(o.ObservedAt) {
		return nil, fmt.Errorf("aggregate equity time must match oldest account capture")
	}
	return wallets, nil
}

func sameWalletReceipt(a, b EquityCashFlow) bool {
	return a.ID == b.ID && a.Account == b.Account && a.Kind == b.Kind && a.Currency == b.Currency && a.Amount == b.Amount && a.ExactAmount == b.ExactAmount && a.At.Equal(b.At)
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
		if _, err := exactFlow(flow, o.Currency); err != nil {
			return s, err
		}
		flow.ExactAmount, err = accounting.CanonicalDecimal(flow.ExactAmount)
		if err != nil {
			return s, err
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
