package main

import (
	"context"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/exchange/accounting"
)

func TestBuildEquityScopeSnapshotExpandsCompositeBotAccounts(t *testing.T) {
	bitgetCredentials := config.ExchangeConfig{APIKey: "bitget-key", SecretKey: "bitget-secret", Passphrase: "bitget-pass", Testnet: true}
	binanceCredentials := config.ExchangeConfig{APIKey: "binance-key", SecretKey: "binance-secret", Testnet: true}
	cfg := &config.Config{
		Exchanges: map[string]config.ExchangeConfig{"bitget": bitgetCredentials, "binance": binanceCredentials},
		Bots: []config.BotConfig{
			{Exchange: "bitget", Symbol: "BTCUSDT", MarketType: config.MarketTypeFundingCarry},
			{MarketType: config.MarketTypeFundingPerpSpread, FundingPerpSpread: &config.FundingPerpSpreadConfig{
				LegA: config.FundingPerpLeg{Exchange: "BINANCE", Symbol: "BTCUSDT"},
				LegB: config.FundingPerpLeg{Exchange: "bitget", Symbol: "ETHUSDT"},
			}},
		},
	}

	snapshot := buildEquityScopeSnapshot(cfg)
	if snapshot.err != "" {
		t.Fatalf("composite account scope rejected: %s", snapshot.err)
	}
	if len(snapshot.accounts) != 3 {
		t.Fatalf("composite scope accounts=%+v, want Bitget futures, Bitget spot, and Binance futures", snapshot.accounts)
	}
	seen := make(map[string]bool)
	for _, account := range snapshot.accounts {
		seen[account.Exchange+"/"+account.MarketType] = true
	}
	for _, want := range []string{"bitget/futures", "bitget/spot", "binance/futures"} {
		if !seen[want] {
			t.Errorf("composite equity scope missing %s: %+v", want, snapshot.accounts)
		}
	}

	cfg.Exchanges["binance"] = binanceCredentials
	cfg.Bots[0].Exchange = "binance"
	unsupported := buildEquityScopeSnapshot(cfg)
	if unsupported.err == "" || unsupported.scope != "" {
		t.Fatalf("unsupported composite spot venue did not fail closed: %+v", unsupported)
	}
}

func TestRuntimeEquityUsesConfiguredUnderlyingSourcesForCompositeRuntimes(t *testing.T) {
	now := time.Now().UTC().Add(-time.Second).Truncate(time.Millisecond)
	bitgetCredentials := config.ExchangeConfig{APIKey: "bitget-key", SecretKey: "bitget-secret", Passphrase: "bitget-pass", Testnet: true}
	binanceCredentials := config.ExchangeConfig{APIKey: "binance-key", SecretKey: "binance-secret", Testnet: true}
	cfg := &config.Config{
		Exchanges: map[string]config.ExchangeConfig{"bitget": bitgetCredentials, "binance": binanceCredentials},
		Bots: []config.BotConfig{
			{Exchange: "bitget", Symbol: "BTCUSDT", MarketType: config.MarketTypeFundingCarry},
			{MarketType: config.MarketTypeFundingPerpSpread, FundingPerpSpread: &config.FundingPerpSpreadConfig{
				LegA: config.FundingPerpLeg{Exchange: "binance", Symbol: "BTCUSDT"},
				LegB: config.FundingPerpLeg{Exchange: "bitget", Symbol: "ETHUSDT"},
			}},
		},
	}
	manager := &SymbolManager{botManager: NewBotManager(cfg, nil, nil, nil, "")}
	manager.botManager.AddRuntime(&BotRuntime{BotID: "carry", Inner: &SymbolRuntime{
		Exchange: &equityLedgerExchange{}, AccountScope: "composite-carry", AccountMarketType: config.MarketTypeFundingCarry,
	}})
	manager.botManager.AddRuntime(&BotRuntime{BotID: "spread", Inner: &SymbolRuntime{
		Exchange: &equityLedgerExchange{}, AccountScope: "composite-spread", AccountMarketType: config.MarketTypeFundingPerpSpread,
	}})

	accounts := make(map[string]*equityLedgerExchange)
	factoryCalls := 0
	source := &runtimeEquitySource{manager: manager, accountEvidenceSourceFactory: func(_ context.Context, account equityAccountEvidenceConfig) (accounting.Source, error) {
		factoryCalls++
		key := account.identity()
		provider := &equityLedgerExchange{snapshot: runtimeWalletFixture(now, "100", 100)}
		accounts[key] = provider
		return provider, nil
	}}
	observation, err := source.ObserveAccountEquity(t.Context(), nil)
	if err != nil {
		t.Fatalf("composite account reconciliation: %v", err)
	}
	if factoryCalls != 3 || observation.Equity != 300 || !observation.CashFlowComplete || len(observation.Wallets) != 3 {
		t.Fatalf("composite observation=%+v source_calls=%d", observation, factoryCalls)
	}
	for identity, provider := range accounts {
		if provider.evidenceCalls != 1 {
			t.Errorf("underlying account %q evidence reads=%d, want exactly one", identity, provider.evidenceCalls)
		}
	}
}
