package main

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/execution"
	"quantmesh/lock"
	"quantmesh/position"
	"quantmesh/storage"
	"quantmesh/strategy"
)

type accountWalletCapitalReleaseSpy struct {
	releases int
	err      error
}

type capitalFlatVerifierExchange struct {
	exchange.IExchange
	positions      []*exchange.Position
	orders         []*exchange.Order
	positionsErr   error
	ordersErr      error
	baseAsset      string
	totalInventory float64
	inventoryErr   error
}

type capitalSpotWithoutTotalInventory struct {
	exchange.IExchange
	orders []*exchange.Order
}

func (capitalSpotWithoutTotalInventory) GetBaseAsset() string { return "BTC" }
func (f capitalSpotWithoutTotalInventory) GetOpenOrders(context.Context, string) ([]*exchange.Order, error) {
	return f.orders, nil
}

func (f capitalFlatVerifierExchange) GetPositions(context.Context, string) ([]*exchange.Position, error) {
	return f.positions, f.positionsErr
}

func (f capitalFlatVerifierExchange) GetOpenOrders(context.Context, string) ([]*exchange.Order, error) {
	return f.orders, f.ordersErr
}
func (f capitalFlatVerifierExchange) GetBaseAsset() string { return f.baseAsset }
func (f capitalFlatVerifierExchange) SpotInventoryQty(context.Context) (float64, error) {
	return f.totalInventory, f.inventoryErr
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

func TestRuntimeAccountWalletCapitalKeepsOpeningBlockedUntilReservationSucceeds(t *testing.T) {
	cfg := &config.Config{}
	cfg.Storage.Enabled = true
	cfg.Storage.Type = "sqlite"
	cfg.Storage.Path = filepath.Join(t.TempDir(), "runtime-wallet-capital.db")
	cfg.Storage.BufferSize = 1
	cfg.Storage.BatchSize = 1
	storageService, err := storage.NewStorageService(cfg, context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer storageService.Stop()
	gate := &execution.OpeningGate{}
	gate.Block(accountWalletCapitalReservationPendingBlock)
	claim := storage.AccountWalletCapitalClaim{WalletKey: strings.Repeat("b", 64), Amount: 10, Available: 100}

	cfg.Instance.Total = 2
	err = reserveRuntimeAccountWalletCapital(context.Background(), cfg, storageService, lock.NewNopLock(), "bot-a", []storage.AccountWalletCapitalClaim{claim}, gate)
	if err == nil {
		t.Fatal("runtime reservation accepted SQLite for multiple instances")
	}
	if !gate.HasBlock(accountWalletCapitalReservationPendingBlock) {
		t.Fatal("failed reservation released the opening gate")
	}

	cfg.Instance.Total = 1
	if err := reserveRuntimeAccountWalletCapital(context.Background(), cfg, storageService, lock.NewNopLock(), "bot-a", []storage.AccountWalletCapitalClaim{claim}, gate); err != nil {
		t.Fatalf("single-instance reservation: %v", err)
	}
	if gate.HasBlock(accountWalletCapitalReservationPendingBlock) {
		t.Fatal("successful reservation retained the pending gate")
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

func TestAccountWalletCapitalReleaseRechecksOwnershipImmediatelyBeforeDelete(t *testing.T) {
	claim := storage.AccountWalletCapitalClaim{WalletKey: strings.Repeat("c", 64), Amount: 10, Available: 100}
	store := &accountWalletCapitalReleaseSpy{}
	guardErr := errors.New("ownership lease lost")
	err := verifyAndReleaseAccountWalletCapitalGuarded(context.Background(), store, "bot-a",
		[]storage.AccountWalletCapitalClaim{claim}, func(context.Context) error { return nil }, func() error { return guardErr })
	if !errors.Is(err, guardErr) {
		t.Fatalf("guarded release error = %v, want ownership guard error", err)
	}
	if store.releases != 0 {
		t.Fatalf("reservation releases = %d after ownership guard failure, want 0", store.releases)
	}
}

func TestAccountWalletCapitalReleaseRefusesLeaseLostDuringFlatnessVerification(t *testing.T) {
	claim := storage.AccountWalletCapitalClaim{WalletKey: strings.Repeat("d", 64), Amount: 10, Available: 100}
	store := &accountWalletCapitalReleaseSpy{}
	leaseLost := false
	err := verifyAndReleaseAccountWalletCapitalGuarded(context.Background(), store, "bot-a",
		[]storage.AccountWalletCapitalClaim{claim}, func(context.Context) error {
			leaseLost = true
			return nil
		}, func() error {
			if leaseLost {
				return errors.New("runtime ownership lease lost during verification")
			}
			return nil
		})
	if err == nil || !strings.Contains(err.Error(), "lease lost during verification") {
		t.Fatalf("release error = %v, want lease loss to prevent deletion", err)
	}
	if store.releases != 0 {
		t.Fatalf("reservation releases = %d after lease loss during verification, want 0", store.releases)
	}
}

func TestVerifyStandardRuntimeFlatRequiresFuturesFlatnessAndNoOpenOrders(t *testing.T) {
	t.Run("flat futures wallet accepted", func(t *testing.T) {
		venue := capitalFlatVerifierExchange{positions: []*exchange.Position{}, orders: []*exchange.Order{}}
		if err := verifyStandardRuntimeFlat(context.Background(), venue, "futures", "BTCUSDT"); err != nil {
			t.Fatalf("flat futures verification failed: %v", err)
		}
	})

	t.Run("residual position rejected", func(t *testing.T) {
		venue := capitalFlatVerifierExchange{positions: []*exchange.Position{{Symbol: "BTCUSDT", Size: 0.001}}, orders: []*exchange.Order{}}
		if err := verifyStandardRuntimeFlat(context.Background(), venue, "futures", "BTCUSDT"); err == nil {
			t.Fatal("residual futures position was accepted as flat")
		}
	})

	t.Run("open order rejected", func(t *testing.T) {
		venue := capitalFlatVerifierExchange{positions: []*exchange.Position{}, orders: []*exchange.Order{{OrderID: 123, Symbol: "BTCUSDT", Status: exchange.OrderStatusNew}}}
		if err := verifyStandardRuntimeFlat(context.Background(), venue, "futures", "BTCUSDT"); err == nil {
			t.Fatal("active futures order was accepted as flat")
		}
	})

	t.Run("spot inventory is never inferred flat", func(t *testing.T) {
		venue := capitalFlatVerifierExchange{positions: []*exchange.Position{}, orders: []*exchange.Order{}}
		if err := verifyStandardRuntimeFlat(context.Background(), venue, "spot", "BTCUSDT"); err == nil {
			t.Fatal("spot wallet was released without an authoritative inventory verifier")
		}
	})

	t.Run("nil positions response is not an empty snapshot", func(t *testing.T) {
		venue := capitalFlatVerifierExchange{orders: []*exchange.Order{}}
		if err := verifyStandardRuntimeFlat(context.Background(), venue, "futures", "BTCUSDT"); err == nil {
			t.Fatal("nil positions response was accepted as an authoritative flat snapshot")
		}
	})

	t.Run("nil open-orders response is not an empty snapshot", func(t *testing.T) {
		venue := capitalFlatVerifierExchange{positions: []*exchange.Position{}}
		if err := verifyStandardRuntimeFlat(context.Background(), venue, "futures", "BTCUSDT"); err == nil {
			t.Fatal("nil open-orders response was accepted as an authoritative empty snapshot")
		}
	})

	t.Run("position query error blocks release", func(t *testing.T) {
		venue := capitalFlatVerifierExchange{positionsErr: errors.New("position API unavailable"), orders: []*exchange.Order{}}
		if err := verifyStandardRuntimeFlat(context.Background(), venue, "futures", "BTCUSDT"); err == nil {
			t.Fatal("position query error was accepted as flat")
		}
	})
}

func TestVerifyStandardSpotRuntimeFlatRequiresOwnedLedgerBalanceAndOrders(t *testing.T) {
	flatInventory := func() error { return nil }
	for _, tc := range []struct {
		name      string
		venue     capitalFlatVerifierExchange
		inventory func() error
		wantErr   bool
	}{
		{name: "fully verified empty", venue: capitalFlatVerifierExchange{baseAsset: "BTC", orders: []*exchange.Order{}, totalInventory: 0}, inventory: flatInventory},
		{name: "missing inventory proof", venue: capitalFlatVerifierExchange{baseAsset: "BTC", orders: []*exchange.Order{}}, inventory: func() error { return errors.New("grid snapshot unverified") }, wantErr: true},
		{name: "total inventory remains", venue: capitalFlatVerifierExchange{baseAsset: "BTC", orders: []*exchange.Order{}, totalInventory: 0.001}, inventory: flatInventory, wantErr: true},
		{name: "nil order snapshot", venue: capitalFlatVerifierExchange{baseAsset: "BTC", totalInventory: 0}, inventory: flatInventory, wantErr: true},
		{name: "active order remains", venue: capitalFlatVerifierExchange{baseAsset: "BTC", orders: []*exchange.Order{{OrderID: 9, Symbol: "BTCUSDT"}}, totalInventory: 0}, inventory: flatInventory, wantErr: true},
		{name: "total inventory query fails", venue: capitalFlatVerifierExchange{baseAsset: "BTC", orders: []*exchange.Order{}, inventoryErr: errors.New("total inventory unavailable")}, inventory: flatInventory, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := verifyStandardSpotRuntimeFlat(context.Background(), tc.venue, "BTCUSDT", tc.inventory)
			if (err != nil) != tc.wantErr {
				t.Fatalf("spot flatness error = %v, wantErr=%v", err, tc.wantErr)
			}
		})
	}
	if err := verifyStandardSpotRuntimeFlat(context.Background(), capitalSpotWithoutTotalInventory{orders: []*exchange.Order{}}, "BTCUSDT", flatInventory); err == nil {
		t.Fatal("spot exchange without authoritative total inventory was accepted")
	}
}

func TestVerifyStandardSpotBotInventoryRequiresVenueBootstrapAndStrategyFlatness(t *testing.T) {
	grid := &position.SuperPositionManager{}
	manager := strategy.NewStrategyManager(&config.Config{}, 100)
	if err := verifyStandardSpotBotInventoryFlat(grid, manager, "BTCUSDT"); err == nil {
		t.Fatal("grid inventory without startup venue proof was accepted")
	}
	grid.MarkGridRuntimeVenueFlatVerified()
	if err := verifyStandardSpotBotInventoryFlat(grid, manager, "BTCUSDT"); err != nil {
		t.Fatalf("verified empty spot Bot should pass inventory check: %v", err)
	}
	if err := verifyStandardSpotBotInventoryFlat(grid, nil, "BTCUSDT"); err == nil {
		t.Fatal("missing strategy inventory manager was accepted")
	}
}
