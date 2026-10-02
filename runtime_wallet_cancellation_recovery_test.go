package main

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/execution"
	"quantmesh/lock"
	"quantmesh/order"
	"quantmesh/storage"
)

type walletCancellationRecoveryVenue struct {
	*runtimeJournalVenue
	ackOnly bool
	cancels int
}

func (v *walletCancellationRecoveryVenue) CancelOrder(ctx context.Context, symbol string, id int64) error {
	v.cancels++
	if v.ackOnly {
		return nil
	}
	return v.runtimeJournalVenue.CancelOrder(ctx, symbol, id)
}

func TestRuntimeWalletRecoveryRechecksUnverifiedOpeningCancellation(t *testing.T) {
	ctx := context.Background()
	cfg := &config.Config{}
	cfg.Storage.Enabled = true
	cfg.Storage.Type = "sqlite"
	cfg.Storage.Path = filepath.Join(t.TempDir(), "wallet-cancellation-recovery.db")
	cfg.Storage.BufferSize, cfg.Storage.BatchSize = 1, 1
	service, err := storage.NewStorageService(cfg, ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { service.Stop() })
	store := service.GetStorage().(storage.AccountWalletCapitalReservationStore)
	issuer := service.GetStorage().(storage.AccountWalletBalanceObservationIssuer)
	claim := storage.AccountWalletCapitalClaim{WalletKey: fmt.Sprintf("%064x", 912), ReservationToken: fmt.Sprintf("%064x", 1912), Amount: 70, Available: 100}
	claim.ObservationSequence, err = issuer.BeginAccountWalletBalanceObservation(ctx, claim.WalletKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ReserveAccountWalletCapital(ctx, "wallet-recovery", []storage.AccountWalletCapitalClaim{claim}); err != nil {
		t.Fatal(err)
	}
	venue := &walletCancellationRecoveryVenue{runtimeJournalVenue: &runtimeJournalVenue{}, ackOnly: true}
	executor := order.NewExchangeOrderExecutor(venue, "BTCUSDT", 0, 0, lock.NewNopLock(), "")
	gate := &execution.OpeningGate{}
	executor.SetOpeningGate(gate, "LONG")
	if _, err := executor.PlaceOrder(&order.OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1, ClientOrderID: "pending-wallet-opening"}); err != nil {
		t.Fatal(err)
	}
	venue.liveOrders[99] = &exchange.Order{OrderID: 99, ClientOrderID: "foreign-opening", Symbol: "BTCUSDT", Side: exchange.SideBuy, Status: exchange.OrderStatusNew, Quantity: 1}
	available := 60.0
	reader := accountWalletBalanceReader{walletKey: claim.WalletKey, read: func(readCtx context.Context) (accountWalletBalanceObservation, error) {
		sequence, err := issuer.BeginAccountWalletBalanceObservation(readCtx, claim.WalletKey)
		return accountWalletBalanceObservation{Available: available, RequestedAt: time.Now().UTC(), ObservationSequence: sequence}, err
	}}
	revalidate := func() error {
		return revalidateRuntimeAccountWalletCapital(ctx, cfg, service, lock.NewNopLock(), "wallet-recovery", []storage.AccountWalletCapitalClaim{claim}, []accountWalletBalanceReader{reader}, gate, executor.CancelOwnedOpeningOrders)
	}
	if err := revalidate(); err == nil {
		t.Fatal("low wallet balance and ACK-only cancellation reported success")
	}
	if !gate.HasBlock(execution.UnverifiedCancellationBlock) || venue.cancels != 1 {
		t.Fatalf("missing unverified cancellation protection: sources=%v cancels=%d", gate.Sources(), venue.cancels)
	}
	gate.Block("manual-review")
	available = 100
	if err := revalidate(); err == nil {
		t.Fatal("recovered wallet ignored still-unverified opening cancellation")
	}
	if !gate.HasBlock(execution.UnverifiedCancellationBlock) || venue.cancels != 2 {
		t.Fatalf("recovery did not retry cancellation exactly once: sources=%v cancels=%d", gate.Sources(), venue.cancels)
	}
	venue.ackOnly = false
	if err := revalidate(); err != nil {
		t.Fatalf("verified terminal cancellation did not recover: %v", err)
	}
	if venue.cancels != 3 || gate.HasBlock(execution.UnverifiedCancellationBlock) || gate.HasBlock(accountWalletBalanceUnverifiedBlock) || gate.HasBlock(accountWalletCapitalReservationPendingBlock) || !gate.HasBlock("manual-review") {
		t.Fatalf("recovery changed independent protection or left own blocks: sources=%v cancels=%d", gate.Sources(), venue.cancels)
	}
	gate.Unblock("manual-review")
	if venue.liveOrders[99].Status != exchange.OrderStatusNew {
		t.Fatal("wallet recovery swept an unrelated account order")
	}
	if _, err := executor.PlaceOrder(&order.OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1, ClientOrderID: "verified-recovery-opening"}); err != nil {
		t.Fatalf("verified recovery did not permit a new opening: %v", err)
	}
}

func TestWalletCancellationRecoveryRejectsMissingProofAndExpiredContext(t *testing.T) {
	for _, name := range []string{"missing-verifier", "unverified-ack", "cancelled-context"} {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			gate := &execution.OpeningGate{}
			gate.Block(execution.UnverifiedCancellationBlock)
			gate.Block("manual-review")
			var verify func(context.Context) error
			if name != "missing-verifier" {
				verify = func(context.Context) error { return nil }
			}
			if name == "cancelled-context" {
				cancel()
			}
			if err := verifyRecoveredWalletOpeningCancellation(ctx, gate, verify); err == nil {
				t.Fatal("missing, ACK-only or cancelled proof reported recovery")
			}
			if !gate.HasBlock(execution.UnverifiedCancellationBlock) || !gate.HasBlock("manual-review") {
				t.Fatal("failed proof removed independent protection")
			}
		})
	}
}
