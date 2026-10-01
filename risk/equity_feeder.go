package risk

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// RefreshWithdrawalEvidence performs a fresh, durable, whole-account ledger
// reconciliation before a transfer. accountIdentity is the serialized
// [marketType, accountScope] identity used by runtimeEquitySource.
func (f *MetricsFeeder) RefreshWithdrawalEvidence(ctx context.Context, accountIdentity string, since time.Time) (EquityObservation, error) {
	if f == nil || ctx == nil || strings.TrimSpace(accountIdentity) == "" || since.IsZero() {
		return EquityObservation{}, fmt.Errorf("withdrawal evidence refresh requires a feeder, account identity, and start time")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.opts.EquityStore == nil || !f.opts.RequirePersistence || !f.opts.RequireCashFlowReconciliation {
		return EquityObservation{}, fmt.Errorf("withdrawal requires persisted, cash-flow-reconciled account equity")
	}
	if !f.equityLoaded {
		state, err := f.opts.EquityStore.LoadEquityState(ctx)
		if err != nil {
			return EquityObservation{}, fmt.Errorf("load withdrawal equity checkpoint: %w", err)
		}
		if state == nil {
			return EquityObservation{}, fmt.Errorf("withdrawal requires an established account equity baseline")
		}
		if err := state.validate(); err != nil {
			return EquityObservation{}, fmt.Errorf("validate withdrawal equity checkpoint: %w", err)
		}
		f.equityState = state
		f.equityLoaded = true
	}
	if f.equityState == nil {
		return EquityObservation{}, fmt.Errorf("withdrawal requires an established account equity baseline")
	}
	source, ok := f.equity.(AccountEquitySource)
	if !ok {
		return EquityObservation{}, fmt.Errorf("withdrawal requires per-account exchange-clock ledger evidence")
	}
	cursors := make(map[string]time.Time, len(f.equityState.Wallets))
	targetFound := false
	for walletID, wallet := range f.equityState.Wallets {
		var identity []string
		if err := json.Unmarshal([]byte(walletID), &identity); err != nil || len(identity) != 2 || identity[0] == "" || identity[1] == "" {
			return EquityObservation{}, fmt.Errorf("withdrawal equity checkpoint has an invalid wallet identity")
		}
		base, ok := f.equityState.BaseWallets[walletID]
		if !ok {
			return EquityObservation{}, fmt.Errorf("withdrawal equity checkpoint lacks a wallet baseline")
		}
		cursor := maxTime(base.From, wallet.Through.Add(-realizedCursorOverlap))
		if identity[0] == accountIdentity && strings.EqualFold(identity[1], "USDT") {
			targetFound = true
			if since.Before(base.From) {
				return EquityObservation{}, fmt.Errorf("withdrawal interval predates the durable account wallet baseline")
			}
			if since.Before(cursor) {
				cursor = since
			}
		}
		if previous, exists := cursors[identity[0]]; !exists || cursor.Before(previous) {
			cursors[identity[0]] = cursor
		}
	}
	if !targetFound {
		return EquityObservation{}, fmt.Errorf("withdrawal account is not present in the durable equity checkpoint")
	}
	observation, err := source.ObserveAccountEquity(ctx, cursors)
	if err != nil {
		return EquityObservation{}, fmt.Errorf("refresh withdrawal account ledger: %w", err)
	}
	if observation.Scope != f.equityState.Scope {
		return EquityObservation{}, fmt.Errorf("withdrawal account scope changed; reconciliation required")
	}
	next, err := nextEquityCheckpoint(f.equityState, observation, f.opts.Now(), time.Time{}, f.opts.MaxEquityAge, true)
	if err != nil {
		return EquityObservation{}, fmt.Errorf("reconcile withdrawal account ledger: %w", err)
	}
	if err := f.opts.EquityStore.SaveEquityState(ctx, next.Revision-1, next); err != nil {
		f.equityLoaded = false
		return EquityObservation{}, fmt.Errorf("persist withdrawal equity reconciliation: %w", err)
	}
	f.equityState = &next
	return observation, nil
}

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
