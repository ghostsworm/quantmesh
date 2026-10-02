package storage

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestAccountWalletAdmissionRejectsUnfinishedObservationAndRecovers(t *testing.T) {
	store, err := NewSQLStorage(filepath.Join(t.TempDir(), "wallet-observation-admission.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	verifyWalletObservationAdmission(t, context.Background(), store, "sqlite-observation-admission")
}

func verifyWalletObservationAdmission(t *testing.T, ctx context.Context, store *SQLStorage, identity string) {
	t.Helper()
	wallet := mysqlCapitalTestWalletKey(identity)
	bot := identity + "-bot"
	claim := AccountWalletCapitalClaim{WalletKey: wallet, ReservationToken: mysqlCapitalTestWalletKey(identity + "-token"), Amount: 70, Available: 100}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := store.ReleaseAccountWalletCapital(cleanupCtx, bot, []AccountWalletCapitalClaim{claim}); err != nil {
			t.Errorf("clean own wallet reservation: %v", err)
		}
	})
	observe := func() int64 {
		t.Helper()
		sequence, err := store.BeginAccountWalletBalanceObservation(ctx, wallet)
		if err != nil {
			t.Fatal(err)
		}
		return sequence
	}
	claim.ObservationSequence = observe()
	if err := store.ReserveAccountWalletCapital(ctx, bot, []AccountWalletCapitalClaim{claim}); err != nil {
		t.Fatal(err)
	}
	if err := store.CheckAccountWalletCapitalAdmission(ctx, []string{wallet}, time.Minute); err != nil {
		t.Fatalf("initial verified balance rejected: %v", err)
	}
	previous := claim
	claim.ObservationSequence = observe()
	if err := store.CheckAccountWalletCapitalAdmission(ctx, []string{wallet}, time.Minute); err == nil {
		t.Fatal("unfinished newer observation admitted an opening using the prior high balance")
	}
	failedCtx, cancel := context.WithCancel(ctx)
	cancel()
	if err := store.ReserveAccountWalletCapital(failedCtx, bot, []AccountWalletCapitalClaim{claim}); err == nil {
		t.Fatal("cancelled observation unexpectedly restored balance evidence")
	}
	if err := store.CheckAccountWalletCapitalAdmission(ctx, []string{wallet}, time.Minute); err == nil {
		t.Fatal("failed newer observation restored prior balance admission")
	}
	if err := store.ReserveAccountWalletCapital(ctx, bot, []AccountWalletCapitalClaim{previous}); err == nil {
		t.Fatal("superseded balance observation unexpectedly committed")
	}
	if err := store.CheckAccountWalletCapitalAdmission(ctx, []string{wallet}, time.Minute); err == nil {
		t.Fatal("late superseded response restored admission while newer evidence was missing")
	}
	claim.Available = 60
	if err := store.ReserveAccountWalletCapital(ctx, bot, []AccountWalletCapitalClaim{claim}); err == nil {
		t.Fatal("low balance allowed an overcommitted reservation")
	}
	if err := store.CheckAccountWalletCapitalAdmission(ctx, []string{wallet}, time.Minute); err == nil {
		t.Fatal("low authoritative balance admitted an opening")
	}
	replayed := claim
	replayed.Available = 100
	replayed.ObservedAt = time.Now().UTC()
	if err := store.ReserveAccountWalletCapital(ctx, bot, []AccountWalletCapitalClaim{replayed}); err == nil {
		t.Fatal("same-generation replay replaced low balance with high balance")
	}
	if err := store.CheckAccountWalletCapitalAdmission(ctx, []string{wallet}, time.Minute); err == nil {
		t.Fatal("same-generation replay restored opening admission")
	}
	claim.ObservationSequence = observe()
	claim.Available = 100
	claim.ObservedAt = time.Now().UTC()
	if err := store.ReserveAccountWalletCapital(ctx, bot, []AccountWalletCapitalClaim{claim}); err != nil {
		t.Fatalf("verified recovered balance rejected: %v", err)
	}
	if err := store.CheckAccountWalletCapitalAdmission(ctx, []string{wallet}, time.Minute); err != nil {
		t.Fatalf("admission did not recover after valid newer evidence: %v", err)
	}
	var amount float64
	if err := store.db.QueryRowContext(ctx, `SELECT amount FROM funding_spread_capital_reservations WHERE wallet_key = ? AND bot_key = ?`, wallet, fundingSpreadBotKey(bot)).Scan(&amount); err != nil {
		t.Fatal(err)
	}
	if amount != claim.Amount {
		t.Fatalf("observation refresh changed reserved amount: %v", amount)
	}
	claim.ObservationSequence = observe()
	claim.ObservedAt = time.Now().UTC().Add(-2 * time.Minute)
	if err := store.ReserveAccountWalletCapital(ctx, bot, []AccountWalletCapitalClaim{claim}); err != nil {
		t.Fatal(err)
	}
	replayed = claim
	replayed.ObservedAt = time.Now().UTC()
	if err := store.ReserveAccountWalletCapital(ctx, bot, []AccountWalletCapitalClaim{replayed}); err != nil {
		t.Fatalf("idempotent solvent reservation rejected: %v", err)
	}
	if err := store.CheckAccountWalletCapitalAdmission(ctx, []string{wallet}, time.Minute); err == nil {
		t.Fatal("same-generation replay refreshed stale observation time")
	}
	for _, query := range []string{
		`SELECT observed_at_ns FROM funding_spread_wallet_observation_sequences WHERE wallet_key = ?`,
		`SELECT observed_at_ns FROM funding_spread_wallet_balances WHERE wallet_key = ?`,
	} {
		var observedAt int64
		if err := store.db.QueryRowContext(ctx, query, wallet).Scan(&observedAt); err != nil {
			t.Fatal(err)
		}
		if observedAt != claim.ObservedAt.UnixNano() {
			t.Fatalf("replay changed stored observation time: %d", observedAt)
		}
	}
	claim.ObservationSequence = observe()
	claim.ObservedAt = time.Now().UTC()
	if err := store.ReserveAccountWalletCapital(ctx, bot, []AccountWalletCapitalClaim{claim}); err != nil {
		t.Fatal(err)
	}
	if err := store.CheckAccountWalletCapitalAdmission(ctx, []string{wallet}, time.Minute); err != nil {
		t.Fatalf("fresh observation did not recover stale-evidence admission: %v", err)
	}
}
