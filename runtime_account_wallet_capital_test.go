package main

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"quantmesh/config"
	"quantmesh/lock"
	"quantmesh/storage"
)

type accountWalletCapitalReleaseSpy struct {
	releases int
	err      error
}

func (*accountWalletCapitalReleaseSpy) ReserveAccountWalletCapital(context.Context, string, []storage.AccountWalletCapitalClaim) error {
	return nil
}

func (s *accountWalletCapitalReleaseSpy) ReleaseAccountWalletCapital(context.Context, string, []storage.AccountWalletCapitalClaim) error {
	s.releases++
	return s.err
}

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

func TestAccountWalletCapitalRejectsMultiInstanceSQLite(t *testing.T) {
	cfg := &config.Config{}
	cfg.Instance.Total = 2
	cfg.Storage.Enabled = true
	cfg.Storage.Type = "sqlite"
	cfg.Storage.Path = filepath.Join(t.TempDir(), "wallet-capital.db")
	cfg.Storage.BufferSize = 1
	cfg.Storage.BatchSize = 1
	storageService, err := storage.NewStorageService(cfg, context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer storageService.Stop()

	err = reserveAccountWalletCapital(context.Background(), cfg, storageService, lock.NewNopLock(), "bot-a", []storage.AccountWalletCapitalClaim{{
		WalletKey: strings.Repeat("a", 64), Amount: 10, Available: 100,
	}})
	if err == nil || !strings.Contains(err.Error(), "require shared MySQL storage") {
		t.Fatalf("multi-instance SQLite reservation error = %v, want shared-MySQL rejection", err)
	}
}

func TestAccountWalletCapitalReleaseRequiresVerifiedFlatness(t *testing.T) {
	claim := storage.AccountWalletCapitalClaim{WalletKey: strings.Repeat("a", 64), Amount: 10, Available: 100}
	verifyErr := errors.New("open orders remain")
	store := &accountWalletCapitalReleaseSpy{}
	err := verifyAndReleaseAccountWalletCapital(context.Background(), store, "bot-a", []storage.AccountWalletCapitalClaim{claim}, func(context.Context) error {
		return verifyErr
	})
	if !errors.Is(err, verifyErr) {
		t.Fatalf("release error = %v, want verification error", err)
	}
	if store.releases != 0 {
		t.Fatalf("reservation releases = %d after failed verification, want 0", store.releases)
	}

	if err := verifyAndReleaseAccountWalletCapital(context.Background(), store, "bot-a", []storage.AccountWalletCapitalClaim{claim}, func(context.Context) error {
		return nil
	}); err != nil {
		t.Fatalf("verified release: %v", err)
	}
	if store.releases != 1 {
		t.Fatalf("reservation releases = %d after verified flatness, want 1", store.releases)
	}
}
