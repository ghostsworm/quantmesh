package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"quantmesh/config"
	"quantmesh/exchange/accounting"
	"quantmesh/risk"
)

type equityScopeSnapshot struct {
	scope      string
	revision   uint64
	configured bool
	err        string
}

func buildEquityScopeSnapshot(cfg *config.Config) equityScopeSnapshot {
	if cfg == nil {
		return equityScopeSnapshot{}
	}
	accounts := make(map[string]struct{})
	add := func(exchangeName, marketType string, enabled bool) error {
		if !enabled {
			return nil
		}
		exchangeName = strings.TrimSpace(exchangeName)
		if exchangeName == "" {
			exchangeName = strings.TrimSpace(cfg.App.CurrentExchange)
		}
		if exchangeName == "" {
			return fmt.Errorf("enabled equity Bot has no exchange identity")
		}
		if marketType != "futures" {
			return fmt.Errorf("enabled Bot market %q is not supported by equity reconciliation", marketType)
		}
		exchangeConfig, ok := cfg.Exchanges[exchangeName]
		if !ok || strings.TrimSpace(exchangeConfig.APIKey) == "" {
			return fmt.Errorf("enabled equity Bot %s has no configured account credentials", exchangeName)
		}
		identity, err := json.Marshal([]string{marketType, equityAccountScopeID(exchangeName, exchangeConfig)})
		if err != nil {
			return err
		}
		accounts[string(identity)] = struct{}{}
		return nil
	}
	if len(cfg.Bots) > 0 {
		for _, bot := range cfg.Bots {
			if err := add(bot.Exchange, bot.GetMarketType(), bot.IsEnabled()); err != nil {
				return equityScopeSnapshot{configured: true, err: err.Error()}
			}
		}
	} else {
		for _, symbol := range cfg.Trading.Symbols {
			if err := add(symbol.Exchange, symbol.GetMarketType(), symbol.IsEnabled()); err != nil {
				return equityScopeSnapshot{configured: true, err: err.Error()}
			}
		}
	}
	keys := make([]string, 0, len(accounts))
	for key := range accounts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	identity, err := json.Marshal(keys)
	if err != nil {
		return equityScopeSnapshot{configured: true, err: err.Error()}
	}
	return equityScopeSnapshot{configured: true, scope: fmt.Sprintf("futures:%x", sha256.Sum256(identity))}
}

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
	configuredScope := s.manager.botManager.equityScopeSnapshot()
	if configuredScope.err != "" {
		return risk.EquityObservation{}, fmt.Errorf("configured equity account scope is unsupported: %s", configuredScope.err)
	}
	runtimes := s.manager.List()
	observation, err := observeRuntimeEquityCursors(ctx, runtimes, cursors)
	if err != nil {
		return risk.EquityObservation{}, err
	}
	_, _, currentScope, err := runtimeEquityAccounts(s.manager.List())
	if err != nil {
		return risk.EquityObservation{}, err
	}
	if currentScope != observation.Scope {
		return risk.EquityObservation{}, fmt.Errorf("equity account membership changed during observation; retry required")
	}
	latestConfiguredScope := s.manager.botManager.equityScopeSnapshot()
	if configuredScope.revision != latestConfiguredScope.revision {
		return risk.EquityObservation{}, fmt.Errorf("configured equity account scope changed during observation; retry required")
	}
	if latestConfiguredScope.err != "" {
		return risk.EquityObservation{}, fmt.Errorf("configured equity account scope is unsupported: %s", latestConfiguredScope.err)
	}
	if latestConfiguredScope.configured && latestConfiguredScope.scope != observation.Scope {
		return risk.EquityObservation{}, fmt.Errorf("running equity accounts do not cover all enabled configured Bots; reconciliation required")
	}
	return observation, nil
}

func observeRuntimeEquity(ctx context.Context, runtimes []*SymbolRuntime) (risk.EquityObservation, error) {
	return observeRuntimeEquityCursors(ctx, runtimes, nil)
}

// Only dedicated account evidence providers can supply candidate adjusted
// equity. The feeder must reconcile wallet deltas and persist before publishing.
// Unsupported providers remain explicitly raw; nil income APIs are not proof.
func observeRuntimeEquityCursors(ctx context.Context, runtimes []*SymbolRuntime, cursors map[string]time.Time) (risk.EquityObservation, error) {
	observation := risk.EquityObservation{Currency: "USDT", ObservedAt: time.Now()}
	accounts, keys, scope, err := runtimeEquityAccounts(runtimes)
	if err != nil {
		return observation, err
	}
	observation.Scope = scope
	accountCursors, err := cursorsByAccount(cursors, accounts)
	if err != nil {
		return observation, err
	}
	providers := make(map[string]accounting.Source, len(accounts))
	for _, key := range keys {
		if provider, ok := accounts[key].Exchange.(accounting.Source); ok {
			providers[key] = provider
		}
	}
	if len(providers) == len(accounts) {
		return observeAccountEvidence(ctx, observation, keys, providers, accountCursors)
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

func runtimeEquityAccounts(runtimes []*SymbolRuntime) (map[string]*SymbolRuntime, []string, string, error) {
	accounts := make(map[string]*SymbolRuntime)
	for _, rt := range runtimes {
		if rt == nil || rt.Exchange == nil {
			return nil, nil, "", fmt.Errorf("equity runtime is incomplete")
		}
		if rt.AccountMarketType != "futures" || rt.AccountScope == "" {
			return nil, nil, "", fmt.Errorf("spot account valuation is not reconciled: %w", risk.ErrEquityUnavailable)
		}
		identity, err := json.Marshal([]string{rt.AccountMarketType, rt.AccountScope})
		if err != nil {
			return nil, nil, "", err
		}
		accounts[string(identity)] = rt
	}
	if len(accounts) == 0 {
		return nil, nil, "", risk.ErrEquityUnavailable
	}
	keys := make([]string, 0, len(accounts))
	for key := range accounts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	identity, err := json.Marshal(keys)
	if err != nil {
		return nil, nil, "", err
	}
	return accounts, keys, fmt.Sprintf("futures:%x", sha256.Sum256(identity)), nil
}

// Wallet checkpoints may contain one cursor per currency. Current exchange
// sources accept one lower-bound cursor per account, so use the oldest durable
// cursor while retaining each wallet's own overlap validation downstream.
func cursorsByAccount(cursors map[string]time.Time, accounts map[string]*SymbolRuntime) (map[string]time.Time, error) {
	result := make(map[string]time.Time, len(accounts))
	for identity, cursor := range cursors {
		account := identity
		var composite []string
		if err := json.Unmarshal([]byte(identity), &composite); err == nil {
			if len(composite) == 2 && composite[0] != "" && composite[1] != "" {
				if _, ok := accounts[composite[0]]; ok {
					account = composite[0]
				}
			}
		}
		if _, ok := accounts[account]; !ok {
			return nil, fmt.Errorf("equity account identity changed; reconciliation required")
		}
		oldest, ok := result[account]
		if !ok || cursor.Before(oldest) {
			result[account] = cursor
		}
	}
	if len(cursors) > 0 && len(result) != len(accounts) {
		return nil, fmt.Errorf("equity account membership changed; reconciliation required")
	}
	return result, nil
}
