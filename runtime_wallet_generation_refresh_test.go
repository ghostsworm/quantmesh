package main

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/execution"
	"quantmesh/lock"
	"quantmesh/storage"
)

func TestRuntimeWalletRevalidationCannotReclaimReplacementGeneration(t *testing.T) {
	ctx := context.Background()
	cfg := &config.Config{}
	cfg.Storage.Enabled, cfg.Storage.Type = true, "sqlite"
	cfg.Storage.Path = filepath.Join(t.TempDir(), "wallet-generation-refresh.db")
	cfg.Storage.BufferSize, cfg.Storage.BatchSize = 1, 1
	service, err := storage.NewStorageService(cfg, ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { service.Stop() })
	store := service.GetStorage().(storage.AccountWalletCapitalReservationStore)
	issuer := service.GetStorage().(storage.AccountWalletBalanceObservationIssuer)
	old := storage.AccountWalletCapitalClaim{WalletKey: fmt.Sprintf("%064x", 913), ReservationToken: fmt.Sprintf("%064x", 1913), Amount: 70, Available: 100}
	old.ObservationSequence, err = issuer.BeginAccountWalletBalanceObservation(ctx, old.WalletKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ReserveAccountWalletCapital(ctx, "generation-bot", []storage.AccountWalletCapitalClaim{old}); err != nil {
		t.Fatal(err)
	}
	replacement := old
	replacement.ReservationToken = fmt.Sprintf("%064x", 2913)
	replacement.ObservationSequence, err = issuer.BeginAccountWalletBalanceObservation(ctx, old.WalletKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ReserveAccountWalletCapital(ctx, "generation-bot", []storage.AccountWalletCapitalClaim{replacement}); err != nil {
		t.Fatal(err)
	}
	reads, cancellations := 0, 0
	cancelOpenings := func(context.Context) error { cancellations++; return nil }
	reader := accountWalletBalanceReader{walletKey: old.WalletKey, read: func(readCtx context.Context) (accountWalletBalanceObservation, error) {
		reads++
		sequence, err := issuer.BeginAccountWalletBalanceObservation(readCtx, old.WalletKey)
		return accountWalletBalanceObservation{Available: 100, RequestedAt: time.Now().UTC(), ObservationSequence: sequence}, err
	}}
	oldGate := &execution.OpeningGate{}
	oldGate.Block("runtime_ownership_unverified")
	if err := revalidateRuntimeAccountWalletCapital(ctx, cfg, service, lock.NewNopLock(), "generation-bot", []storage.AccountWalletCapitalClaim{old}, []accountWalletBalanceReader{reader}, oldGate, cancelOpenings); err == nil {
		t.Fatal("stale runtime revalidation reclaimed its replacement generation")
	}
	if reads != 0 || cancellations != 0 {
		t.Fatalf("known lost owner still performed wallet side effects: reads=%d cancellations=%d", reads, cancellations)
	}
	// The database fence must also reject an old generation without relying on
	// the in-memory ownership pause.
	oldGate.Unblock("runtime_ownership_unverified")
	if err := revalidateRuntimeAccountWalletCapital(ctx, cfg, service, lock.NewNopLock(), "generation-bot", []storage.AccountWalletCapitalClaim{old}, []accountWalletBalanceReader{reader}, oldGate, cancelOpenings); err == nil {
		t.Fatal("missing ownership pause bypassed generation fence")
	}
	rows, err := service.GetStorage().(storage.AccountWalletCapitalReservationReader).ListAccountWalletCapitalReservations(ctx, "", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite3", cfg.Storage.Path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	var token string
	if err := db.QueryRowContext(ctx, `SELECT reservation_token FROM funding_spread_capital_reservations WHERE wallet_key = ?`, old.WalletKey).Scan(&token); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || token != replacement.ReservationToken || rows[0].Amount != 70 {
		t.Fatalf("replacement reservation changed: %+v", rows)
	}
	newGate := &execution.OpeningGate{}
	if err := revalidateRuntimeAccountWalletCapital(ctx, cfg, service, lock.NewNopLock(), "generation-bot", []storage.AccountWalletCapitalClaim{replacement}, []accountWalletBalanceReader{reader}, newGate, nil); err != nil || newGate.Blocked() {
		t.Fatalf("current runtime could not refresh: sources=%v err=%v", newGate.Sources(), err)
	}
	lostDuringRead := &execution.OpeningGate{}
	delayed := replacement
	delayed.Amount = 80
	lossReader := reader
	lossReader.read = func(readCtx context.Context) (accountWalletBalanceObservation, error) {
		observation, err := reader.read(readCtx)
		lostDuringRead.Block("runtime_ownership_unverified")
		return observation, err
	}
	beforeCancellation := cancellations
	if err := revalidateRuntimeAccountWalletCapital(ctx, cfg, service, lock.NewNopLock(), "generation-bot", []storage.AccountWalletCapitalClaim{delayed}, []accountWalletBalanceReader{lossReader}, lostDuringRead, cancelOpenings); err == nil {
		t.Fatal("lease loss during observation allowed refresh")
	}
	var amount float64
	if err := db.QueryRowContext(ctx, `SELECT amount FROM funding_spread_capital_reservations WHERE wallet_key = ?`, old.WalletKey).Scan(&amount); err != nil {
		t.Fatal(err)
	}
	if amount != 70 || cancellations != beforeCancellation || !lostDuringRead.HasBlock("runtime_ownership_unverified") {
		t.Fatalf("lost owner changed reservation or cancelled orders: amount=%v cancellations=%d", amount, cancellations)
	}
}

func TestRuntimeWalletObservationStopsRemainingWalletsAfterOwnershipLoss(t *testing.T) {
	gate := &execution.OpeningGate{}
	claims := []storage.AccountWalletCapitalClaim{{WalletKey: "wallet-a"}, {WalletKey: "wallet-b"}}
	secondReads := 0
	readers := []accountWalletBalanceReader{
		{walletKey: "wallet-a", read: func(context.Context) (accountWalletBalanceObservation, error) {
			gate.Block("runtime_ownership_unverified")
			return accountWalletBalanceObservation{Available: 100, RequestedAt: time.Now().UTC(), ObservationSequence: 1}, nil
		}},
		{walletKey: "wallet-b", read: func(context.Context) (accountWalletBalanceObservation, error) {
			secondReads++
			return accountWalletBalanceObservation{Available: 100, RequestedAt: time.Now().UTC(), ObservationSequence: 1}, nil
		}},
	}
	observations, err := observeRuntimeWalletCapitalClaims(context.Background(), claims, readers, gate)
	if err == nil || observations != nil || secondReads != 0 {
		t.Fatalf("ownership loss read another wallet or returned partial proof: observations=%v reads=%d err=%v", observations, secondReads, err)
	}
}
