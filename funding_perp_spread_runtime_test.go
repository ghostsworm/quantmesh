package main

import (
	"context"
	"strings"
	"testing"

	"quantmesh/config"
	"quantmesh/exchange"
)

type fundingPerpSpreadOrderSyncTestClient struct {
	exchange.IExchange
	name string
}

func (c fundingPerpSpreadOrderSyncTestClient) GetName() string { return c.name }

type fundingPerpSpreadOrderHistoryTestClient struct {
	fundingPerpSpreadOrderSyncTestClient
}

func (fundingPerpSpreadOrderHistoryTestClient) GetOrderHistoryPage(context.Context, string, int64, int64, string, int) (exchange.OrderHistoryPage, error) {
	return exchange.OrderHistoryPage{}, nil
}

func TestFundingPerpSpreadOrderSyncPlansKeepSupportedLegsAndScope(t *testing.T) {
	targets := []fundingPerpSpreadIncomeTarget{
		{Exchange: "binance", Symbol: "BTCUSDT", AccountID: "scope-a", AccountScope: "scope-a"},
		{Exchange: "bybit", Symbol: "BTCUSDT", AccountID: "scope-b", AccountScope: "scope-b"},
	}
	plans, unsupported, err := fundingPerpSpreadOrderSyncPlans(targets,
		fundingPerpSpreadOrderHistoryTestClient{fundingPerpSpreadOrderSyncTestClient{name: "binance"}},
		fundingPerpSpreadOrderSyncTestClient{name: "bybit"},
	)
	if err != nil {
		t.Fatalf("build plans: %v", err)
	}
	if len(plans) != 1 || len(unsupported) != 1 || unsupported[0] != "bybit:BTCUSDT" {
		t.Fatalf("plans=%+v unsupported=%v; want one supported plan and bybit gap", plans, unsupported)
	}
	if plans[0].target.AccountScope != "scope-a" || plans[0].target.AccountID != "scope-a" {
		t.Fatalf("plan lost account scope: %+v", plans[0].target)
	}
}

func TestFundingPerpSpreadOrderSyncPlansRejectMismatchedClients(t *testing.T) {
	target := fundingPerpSpreadIncomeTarget{Exchange: "binance", Symbol: "BTCUSDT", AccountID: "scope", AccountScope: "scope"}
	_, _, err := fundingPerpSpreadOrderSyncPlans([]fundingPerpSpreadIncomeTarget{target}, fundingPerpSpreadOrderSyncTestClient{name: "bybit"})
	if err == nil {
		t.Fatal("expected mismatched exchange client to be rejected")
	}
}

func TestFundingPerpSpreadStateScopeIsolatesCredentialsWithoutLeakingThem(t *testing.T) {
	fp := &config.FundingPerpSpreadConfig{
		LegA: config.FundingPerpLeg{Exchange: "account-a", Symbol: "BTCUSDT"},
		LegB: config.FundingPerpLeg{Exchange: "account-b", Symbol: "ETHUSDT"},
	}
	cfgA := &config.Config{Exchanges: map[string]config.ExchangeConfig{
		"account-a": {APIKey: "key-a", SecretKey: "secret-a"},
		"account-b": {APIKey: "key-b", SecretKey: "secret-b"},
	}}
	cfgB := &config.Config{Exchanges: map[string]config.ExchangeConfig{
		"account-a": {APIKey: "key-a2", SecretKey: "secret-a2"},
		"account-b": {APIKey: "key-b", SecretKey: "secret-b"},
	}}
	scopeA := fundingPerpSpreadStateScope("bot-id", cfgA, fp)
	scopeB := fundingPerpSpreadStateScope("bot-id", cfgB, fp)
	if scopeA == scopeB {
		t.Fatal("different exchange credentials shared a durable state scope")
	}
	for _, secret := range []string{"key-a", "secret-a", "key-b", "secret-b"} {
		if strings.Contains(scopeA, secret) {
			t.Fatalf("state scope leaked credential material %q", secret)
		}
	}
}

func TestFundingPerpSpreadCapitalClaimsUseCredentialScopedWallets(t *testing.T) {
	cfg := &config.Config{Exchanges: map[string]config.ExchangeConfig{
		"binance": {APIKey: "binance-api-key", SecretKey: "binance-secret"},
		"bybit":   {APIKey: "bybit-api-key", SecretKey: "bybit-secret"},
	}}
	fp := &config.FundingPerpSpreadConfig{
		LegA: config.FundingPerpLeg{Exchange: "binance", Symbol: "BTCUSDT"},
		LegB: config.FundingPerpLeg{Exchange: "bybit", Symbol: "BTCUSDT"},
	}
	claims, err := fundingPerpSpreadCapitalClaims(cfg, fp, 100, 80, 90)
	if err != nil {
		t.Fatalf("build cross-wallet claims: %v", err)
	}
	if len(claims) != 2 {
		t.Fatalf("claim count = %d, want 2", len(claims))
	}
	for _, claim := range claims {
		if claim.Amount != 50 || claim.Available != 80 && claim.Available != 90 {
			t.Fatalf("unexpected leg claim: %+v", claim)
		}
		for _, secret := range []string{"binance-api-key", "binance-secret", "bybit-api-key", "bybit-secret"} {
			if strings.Contains(claim.WalletKey, secret) {
				t.Fatalf("wallet claim leaked credential %q", secret)
			}
		}
	}

	fp.LegB.Exchange = "binance"
	combined, err := fundingPerpSpreadCapitalClaims(cfg, fp, 100, 80, 70)
	if err != nil {
		t.Fatalf("combine same-wallet legs: %v", err)
	}
	if len(combined) != 1 || combined[0].Amount != 100 || combined[0].Available != 70 {
		t.Fatalf("same wallet legs were not combined conservatively: %+v", combined)
	}
}

func TestFundingPerpSpreadIncomeTargetsCoverBothLegsByCredentialScope(t *testing.T) {
	tests := []struct {
		name       string
		legB       config.FundingPerpLeg
		wantScopes int
	}{
		{
			name:       "different credential accounts",
			legB:       config.FundingPerpLeg{Exchange: "bybit", Symbol: "BTCUSDT"},
			wantScopes: 2,
		},
		{
			name:       "same account different symbols",
			legB:       config.FundingPerpLeg{Exchange: "binance", Symbol: "ETHUSDT"},
			wantScopes: 1,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{Exchanges: map[string]config.ExchangeConfig{
				"binance": {APIKey: "binance-api-key", SecretKey: "binance-secret"},
				"bybit":   {APIKey: "bybit-api-key", SecretKey: "bybit-secret"},
			}}
			fp := &config.FundingPerpSpreadConfig{
				LegA: config.FundingPerpLeg{Exchange: "binance", Symbol: "BTCUSDT"},
				LegB: tt.legB,
			}
			targets, err := fundingPerpSpreadIncomeTargets(cfg, fp)
			if err != nil {
				t.Fatalf("build income sync targets: %v", err)
			}
			if len(targets) != 2 {
				t.Fatalf("target count = %d, want 2", len(targets))
			}
			if targets[0].Exchange != "binance" || targets[0].Symbol != "BTCUSDT" ||
				targets[1].Exchange != tt.legB.Exchange || targets[1].Symbol != tt.legB.Symbol {
				t.Fatalf("both configured legs must be synchronized independently: %+v", targets)
			}
			if targets[0].AccountScope == "" || targets[1].AccountScope == "" ||
				targets[0].AccountID != targets[0].AccountScope || targets[1].AccountID != targets[1].AccountScope {
				t.Fatalf("income targets must use exact immutable account scopes: %+v", targets)
			}
			scopes := map[string]struct{}{}
			for _, target := range targets {
				scopes[target.AccountScope] = struct{}{}
				for _, secret := range []string{"binance-api-key", "binance-secret", "bybit-api-key", "bybit-secret"} {
					if strings.Contains(target.AccountScope, secret) {
						t.Fatalf("account scope leaked credential material %q", secret)
					}
				}
			}
			if len(scopes) != tt.wantScopes {
				t.Fatalf("distinct account scopes = %d, want %d", len(scopes), tt.wantScopes)
			}
		})
	}
}

func TestValidateFundingPerpSpreadLegBases(t *testing.T) {
	tests := []struct {
		name    string
		baseA   string
		baseB   string
		wantErr bool
	}{
		{name: "same asset ignoring case and spaces", baseA: " btc ", baseB: "BTC"},
		{name: "different assets", baseA: "BTC", baseB: "ETH", wantErr: true},
		{name: "missing asset", baseA: "BTC", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateFundingPerpSpreadLegBases(tt.baseA, tt.baseB)
			if (err != nil) != tt.wantErr {
				t.Fatalf("error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
