package main

import (
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"

	"quantmesh/execution"
	"quantmesh/lock"
	"quantmesh/order"
	"quantmesh/storage"
)

func TestWalletAdmissionIsInstalledBeforeOrderCapableStartup(t *testing.T) {
	files := token.NewFileSet()
	file, err := parser.ParseFile(files, "symbol_manager.go", nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	var installed token.Pos
	var startup []token.Pos
	for _, declaration := range file.Decls {
		function, ok := declaration.(*ast.FuncDecl)
		if !ok || function.Name.Name != "startSymbolRuntime" {
			continue
		}
		ast.Inspect(function.Body, func(node ast.Node) bool {
			switch expression := node.(type) {
			case *ast.Ident:
				if expression.Name == "installRuntimeWalletOpeningAdmission" {
					installed = expression.Pos()
				}
			case *ast.SelectorExpr:
				if expression.Sel.Name == "SetOpeningAdmissionGuard" {
					installed = expression.Pos()
				}
				if expression.Sel.Name == "Initialize" || expression.Sel.Name == "StartAll" {
					startup = append(startup, expression.Pos())
				}
			}
			return true
		})
	}
	if installed == token.NoPos || len(startup) < 2 {
		t.Fatal("missing wallet admission installation or actual startup entrypoints")
	}
	for _, entry := range startup {
		if installed >= entry {
			t.Fatalf("wallet admission installed at %s after order-capable startup at %s", files.Position(installed), files.Position(entry))
		}
	}
}

func TestRuntimeWalletAdmissionProtectsFirstPhysicalOpeningAndRecovers(t *testing.T) {
	store, err := storage.NewSQLStorage(filepath.Join(t.TempDir(), "startup-wallet.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	wallet := fmt.Sprintf("%064x", 911)
	claim := storage.AccountWalletCapitalClaim{WalletKey: wallet, ReservationToken: fmt.Sprintf("%064x", 1911), Amount: 70, Available: 100}
	claim.ObservationSequence, err = store.BeginAccountWalletBalanceObservation(ctx, wallet)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ReserveAccountWalletCapital(ctx, "startup-bot", []storage.AccountWalletCapitalClaim{claim}); err != nil {
		t.Fatal(err)
	}
	venue := &runtimeJournalVenue{}
	executor := order.NewExchangeOrderExecutor(venue, "BTCUSDT", 0, 0, lock.NewNopLock(), "")
	executor.SetOpeningGate(&execution.OpeningGate{}, "LONG")
	installRuntimeWalletOpeningAdmission(executor, store, wallet)
	opening := &order.OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1, ClientOrderID: "first"}
	if _, err := executor.PlaceOrder(opening); err != nil || venue.sends != 1 {
		t.Fatalf("verified first opening rejected: sends=%d err=%v", venue.sends, err)
	}
	claim.ObservationSequence, err = store.BeginAccountWalletBalanceObservation(ctx, wallet)
	if err != nil {
		t.Fatal(err)
	}
	opening.ClientOrderID = "pending-proof"
	if _, err := executor.PlaceOrder(opening); err == nil || venue.sends != 1 {
		t.Fatalf("unfinished balance proof reached venue: sends=%d err=%v", venue.sends, err)
	}
	closing := &order.OrderRequest{Symbol: "BTCUSDT", Side: "SELL", Price: 100, Quantity: 1, ReduceOnly: true, ClientOrderID: "protect"}
	if _, err := executor.PlaceOrder(closing); err != nil || venue.sends != 2 {
		t.Fatalf("wallet proof blocked protective close: sends=%d err=%v", venue.sends, err)
	}
	if err := store.ReserveAccountWalletCapital(ctx, "startup-bot", []storage.AccountWalletCapitalClaim{claim}); err != nil {
		t.Fatal(err)
	}
	opening.ClientOrderID = "recovered-proof"
	if _, err := executor.PlaceOrder(opening); err != nil || venue.sends != 3 {
		t.Fatalf("new verified proof did not recover admission: sends=%d err=%v", venue.sends, err)
	}
}

func TestRuntimeWalletAdmissionMissingCapabilityRejectsOpeningsOnly(t *testing.T) {
	venue := &runtimeJournalVenue{}
	executor := order.NewExchangeOrderExecutor(venue, "BTCUSDT", 0, 0, lock.NewNopLock(), "")
	executor.SetOpeningGate(&execution.OpeningGate{}, "LONG")
	installRuntimeWalletOpeningAdmission(executor, nil, "")
	if _, err := executor.PlaceOrder(&order.OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1}); err == nil || venue.sends != 0 {
		t.Fatalf("missing capability allowed opening: sends=%d err=%v", venue.sends, err)
	}
	if _, err := executor.PlaceOrder(&order.OrderRequest{Symbol: "BTCUSDT", Side: "SELL", Price: 100, Quantity: 1, ReduceOnly: true}); err != nil || venue.sends != 1 {
		t.Fatalf("missing capability blocked close: sends=%d err=%v", venue.sends, err)
	}
}
