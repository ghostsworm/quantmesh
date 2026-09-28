package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"time"

	"quantmesh/config"
	"quantmesh/exchange/accounting"
	"quantmesh/risk"
)

func equityAccountScopeID(name string, cfg config.ExchangeConfig) string {
	identity, _ := json.Marshal([]interface{}{name, cfg.Testnet, cfg.APIKey})
	return fmt.Sprintf("%x", sha256.Sum256(identity))
}

func (s *runtimeEquitySource) ObserveEquity(ctx context.Context, since time.Time) (risk.EquityObservation, error) {
	if !since.IsZero() {
		return risk.EquityObservation{}, fmt.Errorf("runtime equity requires per-account exchange cursors")
	}
	return s.ObserveAccountEquity(ctx, nil)
}

func (s *runtimeEquitySource) ObserveAccountEquity(ctx context.Context, cursors map[string]time.Time) (risk.EquityObservation, error) {
	if s == nil || s.manager == nil {
		return risk.EquityObservation{}, risk.ErrEquityUnavailable
	}
	return observeRuntimeEquityCursors(ctx, s.manager.List(), cursors)
}

func observeRuntimeEquity(ctx context.Context, runtimes []*SymbolRuntime) (risk.EquityObservation, error) {
	return observeRuntimeEquityCursors(ctx, runtimes, nil)
}

// Only dedicated account evidence providers can supply candidate adjusted
// equity. The feeder must reconcile wallet deltas and persist before publishing.
// Unsupported providers remain explicitly raw; nil income APIs are not proof.
func observeRuntimeEquityCursors(ctx context.Context, runtimes []*SymbolRuntime, cursors map[string]time.Time) (risk.EquityObservation, error) {
	observation := risk.EquityObservation{Currency: "USDT", ObservedAt: time.Now()}
	accounts := make(map[string]*SymbolRuntime)
	for _, rt := range runtimes {
		if rt == nil || rt.Exchange == nil {
			return observation, fmt.Errorf("equity runtime is incomplete")
		}
		if rt.AccountMarketType != "futures" || rt.AccountScope == "" {
			return observation, fmt.Errorf("spot account valuation is not reconciled: %w", risk.ErrEquityUnavailable)
		}
		identity, err := json.Marshal([]string{rt.AccountMarketType, rt.AccountScope})
		if err != nil {
			return observation, err
		}
		accounts[string(identity)] = rt
	}
	if len(accounts) == 0 {
		return observation, risk.ErrEquityUnavailable
	}
	keys := make([]string, 0, len(accounts))
	for key := range accounts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	identity, err := json.Marshal(keys)
	if err != nil {
		return observation, err
	}
	observation.Scope = fmt.Sprintf("futures:%x", sha256.Sum256(identity))
	if len(cursors) > 0 {
		if len(cursors) != len(accounts) {
			return observation, fmt.Errorf("equity account membership changed; reconciliation required")
		}
		for key := range cursors {
			if _, ok := accounts[key]; !ok {
				return observation, fmt.Errorf("equity account identity changed; reconciliation required")
			}
		}
	}
	providers := make(map[string]accounting.Source, len(accounts))
	for _, key := range keys {
		if provider, ok := accounts[key].Exchange.(accounting.Source); ok {
			providers[key] = provider
		}
	}
	if len(providers) == len(accounts) {
		return observeAccountEvidence(ctx, observation, keys, providers, cursors)
	}
	for _, key := range keys {
		rt := accounts[key]
		qctx, cancel := context.WithTimeout(ctx, riskEquityQueryTimeout)
		account, err := rt.Exchange.GetAccount(qctx)
		cancel()
		if err != nil {
			return observation, fmt.Errorf("query account equity: %w", err)
		}
		if account == nil || account.BalanceAsset != observation.Currency || math.IsNaN(account.TotalMarginBalance) || math.IsInf(account.TotalMarginBalance, 0) || account.TotalMarginBalance < 0 {
			return observation, fmt.Errorf("invalid account equity response")
		}
		observation.Equity += account.TotalMarginBalance
	}
	return observation, nil
}
