package risk

import (
	"context"
	"fmt"
	"time"
)

// drawdown uses observed account equity on EVERY tick. No local trade/PnL
// reconstruction can hide fees, funding, interest, or other account debits.
func (f *MetricsFeeder) drawdown(ctx context.Context, now time.Time, marks MetricsResetMarks) (float64, bool, error) {
	if f.equity == nil {
		if f.opts.RequireCashFlowReconciliation || f.opts.RequirePersistence {
			return 0, false, fmt.Errorf("account equity source required")
		}
		return 0, false, nil
	}
	if f.opts.RequirePersistence && f.opts.EquityStore == nil {
		return 0, false, fmt.Errorf("equity checkpoint storage required")
	}
	if !f.equityLoaded {
		if f.opts.EquityStore != nil {
			state, err := f.opts.EquityStore.LoadEquityState(ctx)
			if err != nil {
				return 0, false, fmt.Errorf("load equity checkpoint: %w", err)
			}
			if state != nil {
				if err := state.validate(); err != nil {
					return 0, false, err
				}
				f.equityState = state
			} else if f.equityState != nil {
				return 0, false, fmt.Errorf("previously existing equity checkpoint disappeared")
			}
		}
		f.equityLoaded = true
	}
	var observation EquityObservation
	var err error
	if source, ok := f.equity.(AccountEquitySource); ok {
		cursors := make(map[string]time.Time)
		if f.equityState != nil {
			for account, wallet := range f.equityState.Wallets {
				cursors[account] = maxTime(f.equityState.BaseWallets[account].From, wallet.Through.Add(-realizedCursorOverlap))
			}
		}
		observation, err = source.ObserveAccountEquity(ctx, cursors)
		if err == nil && observation.CashFlowComplete && len(observation.Wallets) == 0 {
			err = fmt.Errorf("account equity source omitted wallet reconciliation evidence")
		}
	} else if source, ok := f.equity.(ReconciledEquitySource); ok {
		var since time.Time
		if f.equityState != nil {
			since = maxTime(f.equityState.BaseAt, f.equityState.LastAt.Add(-realizedCursorOverlap))
		}
		observation, err = source.ObserveEquity(ctx, since)
	} else {
		observation = EquityObservation{Scope: "legacy-account-scope", Currency: "USDT", ObservedAt: now}
		observation.Equity, err = f.equity.TotalEquity(ctx)
	}
	if err != nil {
		return 0, false, fmt.Errorf("observe account equity: %w", err)
	}
	next, err := nextEquityCheckpoint(f.equityState, observation, f.opts.Now(), marks.All, f.opts.MaxEquityAge, f.opts.RequireCashFlowReconciliation)
	if err != nil {
		return 0, false, err
	}
	if f.opts.EquityStore != nil {
		if err := f.opts.EquityStore.SaveEquityState(ctx, next.Revision-1, next); err != nil {
			// Reload authoritative state on retry (also handles an uncertain DB ACK).
			f.equityLoaded = false
			return 0, false, fmt.Errorf("save equity checkpoint: %w", err)
		}
	}
	f.equityState = &next
	return next.DrawdownPct, true, nil
}
