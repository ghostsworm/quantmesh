package storage

import (
	"context"
	"path/filepath"
	"testing"
)

func TestAccountWalletRefreshRequiresCurrentGenerationAcrossAllWallets(t *testing.T) {
	store, err := NewSQLStorage(filepath.Join(t.TempDir(), "wallet-generation-refresh.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	verifyWalletGenerationRefresh(t, context.Background(), store, "sqlite-wallet-generation")
}

func verifyWalletGenerationRefresh(t *testing.T, ctx context.Context, store *SQLStorage, identity string) {
	t.Helper()
	claims := []AccountWalletCapitalClaim{
		{WalletKey: mysqlCapitalTestWalletKey(identity + "-a"), ReservationToken: mysqlCapitalTestWalletKey(identity + "-old-a"), Amount: 50, Available: 100},
		{WalletKey: mysqlCapitalTestWalletKey(identity + "-b"), ReservationToken: mysqlCapitalTestWalletKey(identity + "-b-token"), Amount: 50, Available: 100},
	}
	if err := store.ReserveAccountWalletCapital(ctx, identity, claims); err != nil {
		t.Fatal(err)
	}
	replacement := claims[0]
	replacement.ReservationToken = mysqlCapitalTestWalletKey(identity + "-new-a")
	if err := store.ReserveAccountWalletCapital(ctx, identity, []AccountWalletCapitalClaim{replacement}); err != nil {
		t.Fatal(err)
	}
	current := []AccountWalletCapitalClaim{replacement, claims[1]}
	t.Cleanup(func() {
		if err := store.ReleaseAccountWalletCapital(context.Background(), identity, current); err != nil {
			t.Errorf("clean own generation fixture: %v", err)
		}
	})
	stale := append([]AccountWalletCapitalClaim(nil), claims...)
	stale[0].Amount, stale[1].Amount = 60, 60
	if err := store.RefreshAccountWalletCapital(ctx, identity, stale); err == nil {
		t.Fatal("stale generation refreshed replacement")
	}
	for index, claim := range current {
		var token string
		var amount float64
		if err := store.db.QueryRowContext(ctx, `SELECT reservation_token, amount FROM funding_spread_capital_reservations WHERE wallet_key = ? AND bot_key = ?`, claim.WalletKey, fundingSpreadBotKey(identity)).Scan(&token, &amount); err != nil {
			t.Fatal(err)
		}
		if token != claim.ReservationToken || amount != 50 {
			t.Fatalf("failed multi-wallet refresh changed wallet %d", index)
		}
	}
	current[0].Amount, current[1].Amount = 60, 60
	if err := store.RefreshAccountWalletCapital(ctx, identity, current); err != nil {
		t.Fatalf("current generation rejected: %v", err)
	}
	if err := store.ReleaseAccountWalletCapital(ctx, identity, []AccountWalletCapitalClaim{current[0]}); err != nil {
		t.Fatal(err)
	}
	if err := store.RefreshAccountWalletCapital(ctx, identity, current); err == nil {
		t.Fatal("refresh resurrected a released reservation")
	}
	var count int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM funding_spread_capital_reservations WHERE wallet_key = ? AND bot_key = ?`, current[0].WalletKey, fundingSpreadBotKey(identity)).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatal("released reservation was recreated")
	}
}
