package main

import (
	"math"
	"testing"

	"quantmesh/config"
)

func TestCapStrategyCapitalLimit(t *testing.T) {
	tests := []struct {
		name       string
		configured float64
		available  float64
		want       float64
		wantErr    bool
	}{
		{name: "configured below available", configured: 200, available: 500, want: 200},
		{name: "configured above available", configured: 5000, available: 200, want: 200},
		{name: "zero configuration uses verified available", configured: 0, available: 200, want: 200},
		{name: "no available funds", configured: 200, available: 0, wantErr: true},
		{name: "negative configured cap", configured: -1, available: 200, wantErr: true},
		{name: "non-finite configured cap", configured: math.NaN(), available: 200, wantErr: true},
		{name: "non-finite available balance", configured: 200, available: math.Inf(1), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := capStrategyCapitalLimit(tt.configured, tt.available)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && got != tt.want {
				t.Fatalf("capStrategyCapitalLimit() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCapTwoLegStrategyCapitalLimit(t *testing.T) {
	tests := []struct {
		name       string
		configured float64
		legA       float64
		legB       float64
		want       float64
		wantErr    bool
	}{
		{name: "configured within both legs", configured: 150, legA: 100, legB: 100, want: 150},
		{name: "limited by lower balance", configured: 500, legA: 400, legB: 100, want: 200},
		{name: "zero configured cap rejected", configured: 0, legA: 100, legB: 100, wantErr: true},
		{name: "missing leg balance rejected", configured: 100, legA: 100, legB: 0, wantErr: true},
		{name: "non-finite leg balance rejected", configured: 100, legA: 100, legB: math.NaN(), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := capTwoLegStrategyCapitalLimit(tt.configured, tt.legA, tt.legB)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && got != tt.want {
				t.Fatalf("capTwoLegStrategyCapitalLimit() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestApplyBotCapitalLimit(t *testing.T) {
	control := &config.OpenPositionControl{
		MaxPositionValue: 1000,
		BotRiskControl:   &config.BotRiskControl{Enabled: true, MaxPositionValue: 800},
	}
	if err := applyBotCapitalLimit(control, 500); err != nil {
		t.Fatal(err)
	}
	if control.MaxPositionValue != 500 || control.BotRiskControl.MaxPositionValue != 500 {
		t.Fatalf("capital limit not applied to all active controls: %+v", control)
	}

	control = &config.OpenPositionControl{MaxPositionValue: 250}
	if err := applyBotCapitalLimit(control, 500); err != nil {
		t.Fatal(err)
	}
	if control.MaxPositionValue != 250 {
		t.Fatalf("stricter user limit was relaxed: %+v", control)
	}
}

func TestConfiguredAccountCapitalTotal(t *testing.T) {
	cfg := &config.Config{
		Exchanges: map[string]config.ExchangeConfig{
			"binance": {APIKey: "account-a", Testnet: false},
			"okx":     {APIKey: "account-b", Testnet: false},
		},
	}
	cfg.App.CurrentExchange = "binance"
	cfg.Strategies.CapitalAllocation.TotalCapital = 1000
	cfg.Bots = []config.BotConfig{
		{ID: "btc", Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures", TotalAllocatedCapital: 700},
		{ID: "eth", Exchange: "binance", Symbol: "ETHUSDT", MarketType: "futures", TotalAllocatedCapital: 300},
		{ID: "spot", Exchange: "binance", Symbol: "BTCUSDT", MarketType: "spot", TotalAllocatedCapital: 900},
		{ID: "other-account", Exchange: "okx", Symbol: "BTCUSDT", MarketType: "futures", TotalAllocatedCapital: 2000},
	}
	candidate := config.SymbolConfig{ID: "btc", Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures", TotalAllocatedCapital: 500}
	got, err := configuredAccountCapitalTotal(cfg, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if got != 800 {
		t.Fatalf("configured account capital = %v, want candidate replacement + same-account futures Bot = 800", got)
	}
}

func TestConfiguredAccountCapitalTotalUsesFallbackAndRejectsUnknown(t *testing.T) {
	cfg := &config.Config{Exchanges: map[string]config.ExchangeConfig{"binance": {APIKey: "account-a"}}}
	cfg.Strategies.CapitalAllocation.TotalCapital = 250
	cfg.Bots = []config.BotConfig{{ID: "other", Exchange: "binance", Symbol: "ETHUSDT", MarketType: "futures"}}
	candidate := config.SymbolConfig{ID: "btc", Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures"}
	got, err := configuredAccountCapitalTotal(cfg, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if got != 500 {
		t.Fatalf("fallback account capital = %v, want 500", got)
	}

	cfg.Exchanges["binance"] = config.ExchangeConfig{}
	if _, err := configuredAccountCapitalTotal(cfg, candidate); err == nil {
		t.Fatal("missing account identity unexpectedly accepted")
	}
}

func TestConfiguredAccountWalletCapitalCountsPairedStrategyLegs(t *testing.T) {
	cfg := &config.Config{
		Exchanges: map[string]config.ExchangeConfig{
			"binance": {APIKey: "shared-a"},
			"okx":     {APIKey: "shared-b"},
		},
	}
	cfg.App.CurrentExchange = "binance"
	cfg.Strategies.CapitalAllocation.TotalCapital = 1000
	cfg.Bots = []config.BotConfig{
		{ID: "candidate", Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures", TotalAllocatedCapital: 300},
		{ID: "standard", Exchange: "binance", Symbol: "ETHUSDT", MarketType: "futures", TotalAllocatedCapital: 200},
		{ID: "carry", Exchange: "binance", Symbol: "SOLUSDT", MarketType: config.MarketTypeFundingCarry, TotalAllocatedCapital: 800},
		{ID: "spread", MarketType: config.MarketTypeFundingPerpSpread, TotalAllocatedCapital: 600,
			FundingPerpSpread: &config.FundingPerpSpreadConfig{
				LegA: config.FundingPerpLeg{Exchange: "binance", Symbol: "XRPUSDT"},
				LegB: config.FundingPerpLeg{Exchange: "okx", Symbol: "XRPUSDT"},
			}},
		{ID: "other", Exchange: "okx", Symbol: "DOGEUSDT", MarketType: "futures", TotalAllocatedCapital: 900},
	}
	candidate := config.SymbolConfig{ID: "candidate", Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures", TotalAllocatedCapital: 500}
	got, err := configuredAccountWalletCapitalTotal(cfg, candidate, "binance", "futures")
	if err != nil {
		t.Fatal(err)
	}
	if got != 1400 { // candidate 500 + standard 200 + carry 400 + spread leg 300
		t.Fatalf("binance futures wallet commitment=%v, want 1400", got)
	}
	got, err = configuredAccountWalletCapitalTotal(cfg, candidate, "binance", "spot")
	if err != nil {
		t.Fatal(err)
	}
	if got != 400 {
		t.Fatalf("binance spot wallet commitment=%v, want carry half 400", got)
	}
	got, err = configuredAccountWalletCapitalTotal(cfg, candidate, "binance", "spot_margin")
	if err != nil {
		t.Fatal(err)
	}
	if got != 400 {
		t.Fatalf("binance spot-margin wallet commitment=%v, want carry half 400", got)
	}
	got, err = configuredAccountWalletCapitalTotal(cfg, candidate, "okx", "futures")
	if err != nil {
		t.Fatal(err)
	}
	if got != 1200 { // other 900 + spread leg 300
		t.Fatalf("okx futures wallet commitment=%v, want 1200", got)
	}
}

func TestConfiguredAccountWalletCapitalCountsBothSpreadLegsOnSameWallet(t *testing.T) {
	cfg := &config.Config{Exchanges: map[string]config.ExchangeConfig{"binance": {APIKey: "shared-a"}}}
	cfg.Strategies.CapitalAllocation.TotalCapital = 500
	spread := &config.FundingPerpSpreadConfig{
		LegA: config.FundingPerpLeg{Exchange: "binance", Symbol: "BTCUSDT"},
		LegB: config.FundingPerpLeg{Exchange: "binance", Symbol: "ETHUSDT"},
	}
	candidate := config.SymbolConfig{ID: "spread", Exchange: "binance", MarketType: config.MarketTypeFundingPerpSpread,
		TotalAllocatedCapital: 700, FundingPerpSpread: spread}
	got, err := configuredAccountWalletCapitalTotal(cfg, candidate, "binance", "futures")
	if err != nil {
		t.Fatal(err)
	}
	if got != 700 {
		t.Fatalf("same-wallet two-leg spread commitment=%v, want full 700", got)
	}
}

func TestConfiguredAccountWalletCapitalRejectsDuplicateBotIdentity(t *testing.T) {
	cfg := &config.Config{
		Exchanges: map[string]config.ExchangeConfig{"binance": {APIKey: "shared-a"}},
	}
	cfg.Strategies.CapitalAllocation.TotalCapital = 500
	cfg.Bots = []config.BotConfig{
		{ID: "duplicate", Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures", TotalAllocatedCapital: 200},
		{ID: " duplicate ", Exchange: "binance", Symbol: "ETHUSDT", MarketType: "futures", TotalAllocatedCapital: 300},
	}
	candidate := config.SymbolConfig{ID: "candidate", Exchange: "binance", Symbol: "SOLUSDT", MarketType: "futures", TotalAllocatedCapital: 100}
	if _, err := configuredAccountWalletCapitalTotal(cfg, candidate, "binance", "futures"); err == nil {
		t.Fatal("duplicate Bot identity unexpectedly allowed an unverified wallet commitment")
	}
}

func TestConfiguredAccountWalletCapitalMatchesLegacyBotUsingCurrentExchange(t *testing.T) {
	cfg := &config.Config{
		Exchanges: map[string]config.ExchangeConfig{"binance": {APIKey: "shared-a"}},
	}
	cfg.App.CurrentExchange = "binance"
	cfg.Bots = []config.BotConfig{{Symbol: "BTCUSDT", MarketType: "futures", TotalAllocatedCapital: 300}}
	candidate := config.SymbolConfig{Symbol: "BTCUSDT", MarketType: "futures", TotalAllocatedCapital: 500}

	got, err := configuredAccountWalletCapitalTotal(cfg, candidate, "binance", "futures")
	if err != nil {
		t.Fatal(err)
	}
	if got != 500 {
		t.Fatalf("legacy candidate allocation was not replaced by its canonical Bot identity: got %.2f, want 500", got)
	}
}

func TestQuoteAssetFromConfiguredSymbol(t *testing.T) {
	tests := []struct {
		symbol  string
		want    string
		wantErr bool
	}{
		{symbol: "BTCUSDT", want: "USDT"},
		{symbol: "btc/usdc", want: "USDC"},
		{symbol: "BTC-USDT-SWAP", want: "USDT"},
		{symbol: "ETH_USDC_PERP", want: "USDC"},
		{symbol: "FOOBARXYZ", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.symbol, func(t *testing.T) {
			got, err := quoteAssetFromConfiguredSymbol(tt.symbol)
			if (err != nil) != tt.wantErr {
				t.Fatalf("quoteAssetFromConfiguredSymbol() error = %v, wantErr %v", err, tt.wantErr)
			}
			if err == nil && got != tt.want {
				t.Fatalf("quoteAssetFromConfiguredSymbol() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestConfiguredAccountWalletCapitalPartitionsQuoteAssets(t *testing.T) {
	cfg := &config.Config{
		Exchanges: map[string]config.ExchangeConfig{"binance": {APIKey: "shared-a"}},
		Bots: []config.BotConfig{
			{ID: "btc-usdt", Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures", TotalAllocatedCapital: 200},
			{ID: "eth-usdc", Exchange: "binance", Symbol: "ETHUSDC", MarketType: "futures", TotalAllocatedCapital: 900},
		},
	}
	usdtCandidate := config.SymbolConfig{ID: "candidate-usdt", Exchange: "binance", Symbol: "SOLUSDT", MarketType: "futures", TotalAllocatedCapital: 400}
	got, err := configuredAccountWalletCapitalForQuote(cfg, usdtCandidate, "binance", "futures", "USDT")
	if err != nil {
		t.Fatal(err)
	}
	if got != 600 {
		t.Fatalf("USDT wallet commitment = %v, want 600", got)
	}

	usdcCandidate := config.SymbolConfig{ID: "candidate-usdc", Exchange: "binance", Symbol: "ADAUSDC", MarketType: "futures", TotalAllocatedCapital: 250}
	got, err = configuredAccountWalletCapitalForQuote(cfg, usdcCandidate, "binance", "futures", "USDC")
	if err != nil {
		t.Fatal(err)
	}
	if got != 1150 {
		t.Fatalf("USDC wallet commitment = %v, want 1150", got)
	}
}

func TestConfiguredAccountWalletCapitalFailsClosedOnUnknownQuoteAsset(t *testing.T) {
	cfg := &config.Config{
		Exchanges: map[string]config.ExchangeConfig{"binance": {APIKey: "shared-a"}},
		Bots:      []config.BotConfig{{ID: "unknown", Exchange: "binance", Symbol: "FOOBARXYZ", MarketType: "futures", TotalAllocatedCapital: 300}},
	}
	candidate := config.SymbolConfig{ID: "candidate", Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures", TotalAllocatedCapital: 200}
	if _, err := configuredAccountWalletCapitalForQuote(cfg, candidate, "binance", "futures", "USDT"); err == nil {
		t.Fatal("unknown same-wallet quote asset unexpectedly allowed commitment verification")
	}
}
