package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/big"
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
		if snapshot.Currency != observation.Currency || math.IsNaN(snapshot.Equity) || math.IsInf(snapshot.Equity, 0) || snapshot.ObservedAt.IsZero() || snapshot.ObservedAt.After(time.Now()) {
			return risk.EquityObservation{}, fmt.Errorf("invalid account evidence valuation or capture identity")
		}
		if observation.ObservedAt.IsZero() || observation.ObservedAt.After(snapshot.ObservedAt) {
			observation.ObservedAt = snapshot.ObservedAt
		}
		observation.Equity += snapshot.Equity
		wallets := snapshot.Wallets
		if len(wallets) == 0 {
			wallets = map[string]accounting.Wallet{snapshot.Currency: snapshot.Wallet}
		}
		walletIDs := make(map[string]string, len(wallets))
		for currency, wallet := range wallets {
			if currency == "" {
				return risk.EquityObservation{}, fmt.Errorf("account evidence wallet currency missing")
			}
			if wallet.Currency != "" && wallet.Currency != currency {
				return risk.EquityObservation{}, fmt.Errorf("account evidence wallet currency conflicts with identity")
			}
			if !wallet.ObservedAt.Equal(snapshot.ObservedAt) {
				return risk.EquityObservation{}, fmt.Errorf("account evidence wallet capture time mismatch")
			}
			id, err := json.Marshal([]string{key, currency})
			if err != nil {
				return risk.EquityObservation{}, err
			}
			walletID := string(id)
			wallet.Currency = currency
			observation.Wallets[walletID] = wallet
			walletIDs[currency] = walletID
		}
		for _, entry := range snapshot.Entries {
			if entry.ID == "" || entry.Currency == "" {
				return risk.EquityObservation{}, fmt.Errorf("account ledger receipt identity missing")
			}
			walletID, ok := walletIDs[entry.Currency]
			if !ok {
				return risk.EquityObservation{}, fmt.Errorf("account ledger entry currency has no observed wallet")
			}
			amount, err := accounting.Decimal(entry.Amount)
			if err != nil {
				return risk.EquityObservation{}, err
			}
			rateText := entry.ValuationRate
			if rateText == "" {
				if entry.Currency != snapshot.Currency {
					switch entry.Kind {
					case "deposit", "withdrawal", "transfer_in", "transfer_out":
						return risk.EquityObservation{}, fmt.Errorf("external account entry lacks conversion evidence")
					default:
						// Wallet reconciliation uses the exact native-currency amount;
						// only capital flows need valuation in the observation currency.
					}
				} else {
					rateText = "1"
				}
			}
			var approx float64
			if rateText != "" {
				rate, err := accounting.Decimal(rateText)
				if err != nil || rate.Sign() <= 0 {
					return risk.EquityObservation{}, fmt.Errorf("invalid account entry valuation rate")
				}
				valued := new(big.Rat).Mul(amount, rate)
				approx, _ = valued.Float64()
			}
			id, err := json.Marshal([]string{key, entry.ID})
			if err != nil {
				return risk.EquityObservation{}, err
			}
			observation.Flows = append(observation.Flows, risk.EquityCashFlow{ID: string(id), Account: walletID, ExactAmount: entry.Amount,
				WalletCurrency: entry.Currency, Symbol: entry.Symbol, ValuationRate: rateText, ValuationSource: entry.ValuationSource, ValuationAt: entry.ValuationAt,
				Kind: entry.Kind, Currency: snapshot.Currency, Amount: approx, At: entry.At})
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
