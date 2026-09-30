package main

import (
	"strings"
	"testing"

	"quantmesh/config"
)

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
