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
	"quantmesh/exchange"
	"quantmesh/exchange/accounting"
	"quantmesh/risk"
)

type equityScopeSnapshot struct {
	scope      string
	revision   uint64
	configured bool
	err        string
	accounts   []equityAccountEvidenceConfig
}

type equityAccountEvidenceConfig struct {
	Exchange   string
	MarketType string
	Scope      string
	APIKey     string
	SecretKey  string
	Passphrase string
	Testnet    bool
}

func (a equityAccountEvidenceConfig) identity() string {
	identity, _ := json.Marshal([]string{a.MarketType, a.Scope})
	return string(identity)
}

func (a equityAccountEvidenceConfig) credentialVersion() string {
	encoded, _ := json.Marshal(a)
	digest := sha256.Sum256(encoded)
	return fmt.Sprintf("%x", digest[:])
}

func equityAccountCredentialVersion(exchangeName, marketType string, cfg config.ExchangeConfig) string {
	exchangeName = strings.TrimSpace(exchangeName)
	marketType = strings.ToLower(strings.TrimSpace(marketType))
	account := equityAccountEvidenceConfig{
		Exchange: strings.ToLower(exchangeName), MarketType: marketType, Scope: equityAccountScopeID(exchangeName, cfg),
		APIKey: cfg.APIKey, SecretKey: cfg.SecretKey, Passphrase: cfg.Passphrase, Testnet: cfg.Testnet,
	}
	return account.credentialVersion()
}

func sameEquityAccountEvidenceConfigs(a, b []equityAccountEvidenceConfig) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func buildEquityScopeSnapshot(cfg *config.Config) equityScopeSnapshot {
	if cfg == nil {
		return equityScopeSnapshot{}
	}
	accounts := make(map[string]equityAccountEvidenceConfig)
	marketTypes := make(map[string]struct{})
	add := func(exchangeName, marketType string) error {
		exchangeName = strings.TrimSpace(exchangeName)
		if exchangeName == "" {
			exchangeName = strings.TrimSpace(cfg.App.CurrentExchange)
		}
		if exchangeName == "" {
			return fmt.Errorf("enabled equity Bot has no exchange identity")
		}
		marketType = strings.ToLower(strings.TrimSpace(marketType))
		if marketType != "futures" && (marketType != "spot" || !strings.EqualFold(exchangeName, "bitget")) {
			return fmt.Errorf("enabled Bot market %q is not supported by equity reconciliation", marketType)
		}
		exchangeConfig, ok := cfg.Exchanges[exchangeName]
		if !ok {
			for configuredExchange, candidate := range cfg.Exchanges {
				if strings.EqualFold(configuredExchange, exchangeName) {
					exchangeConfig, ok = candidate, true
					break
				}
			}
		}
		if !ok || strings.TrimSpace(exchangeConfig.APIKey) == "" {
			return fmt.Errorf("enabled equity Bot %s has no configured account credentials", exchangeName)
		}
		accountScope := equityAccountScopeID(exchangeName, exchangeConfig)
		identity, err := json.Marshal([]string{marketType, accountScope})
		if err != nil {
			return err
		}
		accounts[string(identity)] = equityAccountEvidenceConfig{Exchange: strings.ToLower(exchangeName), MarketType: marketType,
			Scope: accountScope, APIKey: exchangeConfig.APIKey, SecretKey: exchangeConfig.SecretKey,
			Passphrase: exchangeConfig.Passphrase, Testnet: exchangeConfig.Testnet}
		marketTypes[marketType] = struct{}{}
		return nil
	}
	if len(cfg.Bots) > 0 {
		for _, bot := range cfg.Bots {
			var err error
			switch bot.GetMarketType() {
			case config.MarketTypeFundingCarry:
				err = add(bot.Exchange, "futures")
				if err == nil {
					err = add(bot.Exchange, "spot")
				}
			case config.MarketTypeFundingPerpSpread:
				if bot.FundingPerpSpread == nil {
					err = fmt.Errorf("funding_perp_spread account legs are missing")
				} else {
					err = add(bot.FundingPerpSpread.LegA.Exchange, "futures")
					if err == nil {
						err = add(bot.FundingPerpSpread.LegB.Exchange, "futures")
					}
				}
			default:
				err = add(bot.Exchange, bot.GetMarketType())
			}
			if err != nil {
				return equityScopeSnapshot{configured: true, err: err.Error()}
			}
		}
	} else {
		for _, symbol := range cfg.Trading.Symbols {
			var err error
			switch symbol.GetMarketType() {
			case config.MarketTypeFundingCarry:
				err = add(symbol.Exchange, "futures")
				if err == nil {
					err = add(symbol.Exchange, "spot")
				}
			case config.MarketTypeFundingPerpSpread:
				if symbol.FundingPerpSpread == nil {
					err = fmt.Errorf("funding_perp_spread account legs are missing")
				} else {
					err = add(symbol.FundingPerpSpread.LegA.Exchange, "futures")
					if err == nil {
						err = add(symbol.FundingPerpSpread.LegB.Exchange, "futures")
					}
				}
			default:
				err = add(symbol.Exchange, symbol.GetMarketType())
			}
			if err != nil {
				return equityScopeSnapshot{configured: true, err: err.Error()}
			}
		}
	}
	keys := make([]string, 0, len(accounts))
	for key := range accounts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	accountConfigs := make([]equityAccountEvidenceConfig, 0, len(keys))
	for _, key := range keys {
		accountConfigs = append(accountConfigs, accounts[key])
	}
	identity, err := json.Marshal(keys)
	if err != nil {
		return equityScopeSnapshot{configured: true, err: err.Error()}
	}
	return equityScopeSnapshot{configured: true, scope: equityScopePrefix(marketTypes) + fmt.Sprintf("%x", sha256.Sum256(identity)), accounts: accountConfigs}
}

func equityScopePrefix(marketTypes map[string]struct{}) string {
	if len(marketTypes) > 1 {
		return "mixed:"
	}
	if _, ok := marketTypes["spot"]; ok {
		return "spot:"
	}
	return "futures:"
}

type spotEquityReconciliationSupport interface {
	SupportsSpotEquityReconciliation() bool
}

type configuredEquityEvidenceSourceResult struct {
	source     accounting.Source
	marketType string
}

func equityAccountScopeID(name string, cfg config.ExchangeConfig) string {
	return config.AccountScopeID(name, cfg)
}

func configuredEquityEvidenceSource(ctx context.Context, account equityAccountEvidenceConfig) (accounting.Source, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return exchange.NewAccountEvidenceSource(account.Exchange, account.MarketType, config.ExchangeConfig{
		APIKey: account.APIKey, SecretKey: account.SecretKey, Passphrase: account.Passphrase, Testnet: account.Testnet,
	})
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
	runtimes = equityEvidenceRuntimes(runtimes)
	activeAccounts := make(map[string]struct{})
	staleRuntimeAccounts := make(map[string]struct{})
	if len(runtimes) > 0 {
		active, _, _, err := runtimeEquityAccounts(runtimes)
		if err != nil {
			return risk.EquityObservation{}, err
		}
		for identity := range active {
			activeAccounts[identity] = struct{}{}
		}
		configuredByIdentity := make(map[string]equityAccountEvidenceConfig, len(configuredScope.accounts))
		for _, account := range configuredScope.accounts {
			configuredByIdentity[account.identity()] = account
		}
		for _, runtime := range runtimes {
			identity := equityRuntimeAccountKey(strings.ToLower(strings.TrimSpace(runtime.AccountMarketType)), runtime.AccountScope)
			account, configured := configuredByIdentity[identity]
			if configured && runtime.accountCredentialVersion != account.credentialVersion() {
				staleRuntimeAccounts[identity] = struct{}{}
			}
		}
	}
	equityRuntimes := make([]*SymbolRuntime, 0, len(runtimes))
	for _, runtime := range runtimes {
		identity := equityRuntimeAccountKey(strings.ToLower(strings.TrimSpace(runtime.AccountMarketType)), runtime.AccountScope)
		if _, stale := staleRuntimeAccounts[identity]; !stale {
			equityRuntimes = append(equityRuntimes, runtime)
		}
	}
	factory := s.accountEvidenceSourceFactory
	if factory == nil {
		factory = configuredEquityEvidenceSource
	}
	idleSources := make(map[string]configuredEquityEvidenceSourceResult)
	for _, account := range configuredScope.accounts {
		identity := account.identity()
		_, active := activeAccounts[identity]
		_, stale := staleRuntimeAccounts[identity]
		if active && !stale {
			continue
		}
		source, err := factory(ctx, account)
		if err != nil {
			return risk.EquityObservation{}, fmt.Errorf("create read-only account evidence source for %s/%s: %w", account.Exchange, account.MarketType, err)
		}
		if source == nil {
			return risk.EquityObservation{}, fmt.Errorf("read-only account evidence source for %s/%s is unavailable", account.Exchange, account.MarketType)
		}
		idleSources[identity] = configuredEquityEvidenceSourceResult{source: source, marketType: account.MarketType}
	}
	observation, err := observeRuntimeEquityCursorsWithIdleSources(ctx, equityRuntimes, cursors, idleSources)
	if err != nil {
		return risk.EquityObservation{}, err
	}
	currentActive, err := runtimeEquityAccountKeys(s.manager.List())
	if err != nil {
		return risk.EquityObservation{}, err
	}
	if !sameStringSet(activeAccounts, currentActive) {
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

func runtimeEquityAccountKeys(runtimes []*SymbolRuntime) (map[string]struct{}, error) {
	keys := make(map[string]struct{})
	for _, rt := range runtimes {
		if isCompositeEquityRuntime(rt) {
			continue
		}
		if rt == nil || rt.Exchange == nil || strings.TrimSpace(rt.AccountScope) == "" {
			return nil, fmt.Errorf("equity runtime is incomplete")
		}
		keys[equityRuntimeAccountKey(strings.ToLower(strings.TrimSpace(rt.AccountMarketType)), rt.AccountScope)] = struct{}{}
	}
	return keys, nil
}

func sameStringSet(a, b map[string]struct{}) bool {
	if len(a) != len(b) {
		return false
	}
	for value := range a {
		if _, ok := b[value]; !ok {
			return false
		}
	}
	return true
}

func observeRuntimeEquity(ctx context.Context, runtimes []*SymbolRuntime) (risk.EquityObservation, error) {
	return observeRuntimeEquityCursors(ctx, runtimes, nil)
}

// Only dedicated account evidence providers can supply candidate adjusted
// equity. The feeder must reconcile wallet deltas and persist before publishing.
// Unsupported providers remain explicitly raw; nil income APIs are not proof.
func observeRuntimeEquityCursors(ctx context.Context, runtimes []*SymbolRuntime, cursors map[string]time.Time) (risk.EquityObservation, error) {
	return observeRuntimeEquityCursorsWithIdleSources(ctx, runtimes, cursors, nil)
}

func observeRuntimeEquityCursorsWithIdleSources(ctx context.Context, runtimes []*SymbolRuntime, cursors map[string]time.Time, idleSources map[string]configuredEquityEvidenceSourceResult) (risk.EquityObservation, error) {
	observation := risk.EquityObservation{Currency: "USDT", ObservedAt: time.Now()}
	accounts := make(map[string]*SymbolRuntime)
	marketTypes := make(map[string]struct{})
	for _, rt := range runtimes {
		if isCompositeEquityRuntime(rt) {
			continue
		}
		if rt == nil || rt.Exchange == nil {
			return observation, fmt.Errorf("equity runtime is incomplete")
		}
		market := strings.ToLower(strings.TrimSpace(rt.AccountMarketType))
		spotSupport, spotOK := rt.Exchange.(spotEquityReconciliationSupport)
		if (market != "futures" && (market != "spot" || !spotOK || !spotSupport.SupportsSpotEquityReconciliation())) || rt.AccountScope == "" {
			return observation, fmt.Errorf("spot account valuation is not reconciled: %w", risk.ErrEquityUnavailable)
		}
		key := equityRuntimeAccountKey(market, rt.AccountScope)
		accounts[key] = rt
		marketTypes[market] = struct{}{}
	}
	for key, source := range idleSources {
		if source.source == nil {
			return observation, risk.ErrEquityUnavailable
		}
		if _, exists := accounts[key]; exists {
			return observation, fmt.Errorf("duplicate active and configured equity account")
		}
		accounts[key] = nil
		marketTypes[strings.ToLower(strings.TrimSpace(source.marketType))] = struct{}{}
	}
	if len(accounts) == 0 {
		return observation, risk.ErrEquityUnavailable
	}
	keys := make([]string, 0, len(accounts))
	accountKeys := make(map[string]struct{}, len(accounts))
	for key := range accounts {
		keys = append(keys, key)
		accountKeys[key] = struct{}{}
	}
	sort.Strings(keys)
	identity, err := json.Marshal(keys)
	if err != nil {
		return observation, err
	}
	observation.Scope = equityScopePrefix(marketTypes) + fmt.Sprintf("%x", sha256.Sum256(identity))
	accountCursors, err := cursorsByAccountKeys(cursors, accountKeys)
	if err != nil {
		return observation, err
	}
	providers := make(map[string]accounting.Source, len(accounts))
	containsSpot := false
	for _, key := range keys {
		if strings.EqualFold(marketTypesForAccount(key, accounts, idleSources), "spot") {
			containsSpot = true
		}
		if rt := accounts[key]; rt != nil {
			if provider, ok := rt.Exchange.(accounting.Source); ok {
				providers[key] = provider
			}
		} else if provider := idleSources[key].source; provider != nil {
			providers[key] = provider
		}
	}
	if containsSpot && len(providers) != len(accounts) {
		return observation, fmt.Errorf("Spot equity scope requires complete account evidence from every configured account: %w", risk.ErrEquityUnavailable)
	}
	if len(providers) == len(accounts) {
		return observeAccountEvidence(ctx, observation, keys, providers, accountCursors)
	}
	if len(idleSources) > 0 {
		return observation, fmt.Errorf("configured idle account lacks complete cash-flow evidence: %w", risk.ErrEquityUnavailable)
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

func marketTypesForAccount(key string, accounts map[string]*SymbolRuntime, idleSources map[string]configuredEquityEvidenceSourceResult) string {
	if rt := accounts[key]; rt != nil {
		return rt.AccountMarketType
	}
	return idleSources[key].marketType
}

func runtimeEquityAccounts(runtimes []*SymbolRuntime) (map[string]*SymbolRuntime, []string, string, error) {
	accounts := make(map[string]*SymbolRuntime)
	for _, rt := range runtimes {
		if isCompositeEquityRuntime(rt) {
			continue
		}
		if rt == nil || rt.Exchange == nil {
			return nil, nil, "", fmt.Errorf("equity runtime is incomplete")
		}
		marketType := strings.ToLower(strings.TrimSpace(rt.AccountMarketType))
		spotSupport, spotOK := rt.Exchange.(spotEquityReconciliationSupport)
		if (marketType != "futures" && (marketType != "spot" || !spotOK || !spotSupport.SupportsSpotEquityReconciliation())) || rt.AccountScope == "" {
			return nil, nil, "", fmt.Errorf("spot account valuation is not reconciled: %w", risk.ErrEquityUnavailable)
		}
		accounts[equityRuntimeAccountKey(marketType, rt.AccountScope)] = rt
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
	marketTypes := make(map[string]struct{})
	for _, account := range accounts {
		marketTypes[strings.ToLower(strings.TrimSpace(account.AccountMarketType))] = struct{}{}
	}
	return accounts, keys, equityScopePrefix(marketTypes) + fmt.Sprintf("%x", sha256.Sum256(identity)), nil
}

func isCompositeEquityRuntime(rt *SymbolRuntime) bool {
	if rt == nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(rt.AccountMarketType)) {
	case config.MarketTypeFundingCarry, config.MarketTypeFundingPerpSpread:
		return true
	default:
		return false
	}
}

func equityEvidenceRuntimes(runtimes []*SymbolRuntime) []*SymbolRuntime {
	filtered := make([]*SymbolRuntime, 0, len(runtimes))
	for _, rt := range runtimes {
		if !isCompositeEquityRuntime(rt) {
			filtered = append(filtered, rt)
		}
	}
	return filtered
}

func equityRuntimeAccountKey(marketType, accountScope string) string {
	identity, _ := json.Marshal([]string{marketType, accountScope})
	return string(identity)
}

// Wallet checkpoints may contain one cursor per currency. Current exchange
// sources accept one lower-bound cursor per account, so use the oldest durable
// cursor while retaining each wallet's own overlap validation downstream.
func cursorsByAccount(cursors map[string]time.Time, accounts map[string]*SymbolRuntime) (map[string]time.Time, error) {
	accountKeys := make(map[string]struct{}, len(accounts))
	for account := range accounts {
		accountKeys[account] = struct{}{}
	}
	return cursorsByAccountKeys(cursors, accountKeys)
}

func cursorsByAccountKeys(cursors map[string]time.Time, accounts map[string]struct{}) (map[string]time.Time, error) {
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
