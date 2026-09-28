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
