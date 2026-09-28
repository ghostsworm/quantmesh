package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"time"

	"quantmesh/exchange/accounting"
	"quantmesh/risk"
)

func observeAccountEvidence(ctx context.Context, observation risk.EquityObservation, keys []string, providers map[string]accounting.Source, cursors map[string]time.Time) (risk.EquityObservation, error) {
	observation.Wallets = make(map[string]accounting.Wallet, len(keys))
	observation.ObservedAt = time.Time{}
	for _, key := range keys {
		qctx, cancel := context.WithTimeout(ctx, riskEquityQueryTimeout)
		snapshot, err := providers[key].ReadAccountEvidence(qctx, cursors[key])
		if err == nil {
			err = qctx.Err()
		}
		cancel()
		if err != nil {
			return risk.EquityObservation{}, fmt.Errorf("read account evidence: %w", err)
		}
		if snapshot.Currency != observation.Currency || math.IsNaN(snapshot.Equity) || math.IsInf(snapshot.Equity, 0) || snapshot.ObservedAt.IsZero() || snapshot.ObservedAt.After(time.Now()) || !snapshot.Wallet.ObservedAt.Equal(snapshot.ObservedAt) {
			return risk.EquityObservation{}, fmt.Errorf("invalid account evidence valuation or capture identity")
		}
		if observation.ObservedAt.IsZero() || observation.ObservedAt.After(snapshot.ObservedAt) {
			observation.ObservedAt = snapshot.ObservedAt
		}
		observation.Equity += snapshot.Equity
		observation.Wallets[key] = snapshot.Wallet
		for _, entry := range snapshot.Entries {
			if entry.ID == "" {
				return risk.EquityObservation{}, fmt.Errorf("account ledger receipt identity missing")
			}
			amount, err := accounting.Decimal(entry.Amount)
			if err != nil {
				return risk.EquityObservation{}, err
			}
			approx, _ := amount.Float64()
			id, err := json.Marshal([]string{key, entry.ID})
			if err != nil {
				return risk.EquityObservation{}, err
			}
			observation.Flows = append(observation.Flows, risk.EquityCashFlow{ID: string(id), Account: key, ExactAmount: entry.Amount, Kind: entry.Kind, Currency: entry.Currency, Amount: approx, At: entry.At})
		}
	}
	if math.IsNaN(observation.Equity) || math.IsInf(observation.Equity, 0) {
		return risk.EquityObservation{}, fmt.Errorf("non-finite total account equity")
	}
	// Pagination is only a candidate completeness assertion. nextEquityCheckpoint
	// performs exact per-account reconciliation before this can become healthy.
	observation.CashFlowComplete = true
	return observation, nil
}

var _ risk.AccountEquitySource = (*runtimeEquitySource)(nil)
