package main

import (
	"testing"

	"quantmesh/config"
)

func TestAccountWalletCapitalClaimIsolatesCredentialMarketAndQuoteAsset(t *testing.T) {
	cfg := &config.Config{Exchanges: map[string]config.ExchangeConfig{
		"binance": {APIKey: "account-a", SecretKey: "secret-a"},
	}}
	claim, err := buildAccountWalletCapitalClaim(cfg, "binance", " FUTURES ", "usdt", 50, 100)
	if err != nil {
		t.Fatal(err)
	}
	if claim.Amount != 50 || claim.Available != 100 || len(claim.WalletKey) != 64 {
		t.Fatalf("unexpected normalized wallet claim: %+v", claim)
	}
	cases := []struct {
		name      string
		market    string
		quote     string
		apiKey    string
		wantMatch bool
	}{
		{name: "same wallet normalized", market: "futures", quote: "USDT", apiKey: "account-a", wantMatch: true},
		{name: "different market", market: "spot", quote: "USDT", apiKey: "account-a"},
		{name: "different quote", market: "futures", quote: "USDC", apiKey: "account-a"},
		{name: "different credential", market: "futures", quote: "USDT", apiKey: "account-b"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.apiKey == "" {
				tc.apiKey = "account-a"
			}
			cfg.Exchanges["binance"] = config.ExchangeConfig{APIKey: tc.apiKey, SecretKey: "secret-a"}
			got, err := buildAccountWalletCapitalClaim(cfg, "binance", tc.market, tc.quote, 1, 2)
			if err != nil {
				t.Fatal(err)
			}
			matches := got.WalletKey == claim.WalletKey
			if matches != tc.wantMatch {
				t.Fatalf("wallet key match = %v, want %v", matches, tc.wantMatch)
			}
		})
	}
}
