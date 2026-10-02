package storage

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func fundingSpreadTestClaim(wallet int, amount, available float64) FundingSpreadCapitalClaim {
	return FundingSpreadCapitalClaim{WalletKey: fmt.Sprintf("%064x", wallet), ReservationToken: fmt.Sprintf("%064x", wallet+1000), Amount: amount, Available: available}
}

func TestAccountWalletReservationRejectsDelayedStaleHighBalance(t *testing.T) {
	store, err := NewSQLStorage(filepath.Join(t.TempDir(), "stale-wallet-balance.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	walletKey := fmt.Sprintf("%064x", 77)
	base := time.Now().UTC().Truncate(time.Millisecond)
	initialSequence, err := store.BeginAccountWalletBalanceObservation(ctx, walletKey)
	if err != nil {
		t.Fatal(err)
	}
	initial := AccountWalletCapitalClaim{
		WalletKey: walletKey, ReservationToken: fmt.Sprintf("%064x", 1077), Amount: 40, Available: 100, ObservedAt: base, ObservationSequence: initialSequence,
	}
	if err := store.ReserveAccountWalletCapital(ctx, "bot-initial", []AccountWalletCapitalClaim{initial}); err != nil {
		t.Fatalf("store initial wallet claim: %v", err)
	}
	staleSequence, err := store.BeginAccountWalletBalanceObservation(ctx, walletKey)
	if err != nil {
		t.Fatal(err)
	}
	newerSequence, err := store.BeginAccountWalletBalanceObservation(ctx, walletKey)
	if err != nil {
		t.Fatal(err)
	}
	supersededInFlight := AccountWalletCapitalClaim{
		WalletKey: walletKey, ReservationToken: fmt.Sprintf("%064x", 2577), Amount: 20, Available: 100,
		ObservedAt: base.Add(30 * time.Second), ObservationSequence: staleSequence,
	}
	if err := store.ReserveAccountWalletCapital(ctx, "bot-stale-in-flight", []AccountWalletCapitalClaim{supersededInFlight}); err == nil {
		t.Fatal("older balance request reserved capital while a newer balance request was still in flight")
	}
	newerLow := AccountWalletCapitalClaim{
		WalletKey: walletKey, ReservationToken: fmt.Sprintf("%064x", 2077), Amount: 20, Available: 50, ObservedAt: base.Add(-time.Hour), ObservationSequence: newerSequence,
	}
	if err := store.ReserveAccountWalletCapital(ctx, "bot-new-low", []AccountWalletCapitalClaim{newerLow}); err == nil {
		t.Fatal("overcommitted reservation unexpectedly succeeded after the wallet balance fell")
	}
	var available float64
	var observedAt int64
	if err := store.db.QueryRow(`SELECT available, observed_at_ns FROM funding_spread_wallet_balances WHERE wallet_key = ?`, walletKey).Scan(&available, &observedAt); err != nil {
		t.Fatal(err)
	}
	if available != 50 || observedAt != base.Add(-time.Hour).UnixNano() {
		t.Fatalf("rejected claim rolled back latest balance observation: available=%v observedAt=%d", available, observedAt)
	}
	if _, err := store.db.Exec(`UPDATE funding_spread_wallet_balances SET available = ?, observed_at_ns = ? WHERE wallet_key = ?`, 100, base.Add(30*time.Second).UnixNano(), walletKey); err != nil {
		t.Fatal("simulate a legacy process updating its compatibility balance row:", err)
	}
	readTx, err := store.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		t.Fatal(err)
	}
	authoritativeAvailable, err := readLatestWalletBalance(ctx, readTx, walletKey)
	_ = readTx.Rollback()
	if err != nil || authoritativeAvailable != 50 {
		t.Fatalf("legacy compatibility-table write changed authoritative balance=%v, err=%v; want 50", authoritativeAvailable, err)
	}
	staleHigh := AccountWalletCapitalClaim{
		WalletKey: walletKey, ReservationToken: fmt.Sprintf("%064x", 3077), Amount: 20, Available: 100, ObservedAt: base.Add(30 * time.Second), ObservationSequence: staleSequence,
	}
	if err := store.ReserveAccountWalletCapital(ctx, "bot-stale-high", []AccountWalletCapitalClaim{staleHigh}); err == nil {
		t.Fatal("delayed stale high balance observation allowed aggregate reservations above the persisted newer low balance")
	}
	if err := store.db.QueryRow(`SELECT available, observed_at_ns FROM funding_spread_wallet_balances WHERE wallet_key = ?`, walletKey).Scan(&available, &observedAt); err != nil {
		t.Fatal(err)
	}
	if available != 100 || observedAt != base.Add(30*time.Second).UnixNano() {
		t.Fatalf("stale request unexpectedly rewrote compatibility balance: available=%v observedAt=%d", available, observedAt)
	}
}

func TestFundingSpreadCapitalReservationsAreAtomicAcrossWallets(t *testing.T) {
	store, err := NewSQLStorage(filepath.Join(t.TempDir(), "capital-reservations.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()

	if err := store.ReserveFundingSpreadCapital(ctx, "spread-a", []FundingSpreadCapitalClaim{
		fundingSpreadTestClaim(1, 70, 100), fundingSpreadTestClaim(2, 70, 100),
	}); err != nil {
		t.Fatalf("reserve first two-wallet budget: %v", err)
	}
	if err := store.ReserveFundingSpreadCapital(ctx, "spread-b", []FundingSpreadCapitalClaim{
		fundingSpreadTestClaim(1, 40, 100), fundingSpreadTestClaim(2, 40, 100),
	}); err == nil {
		t.Fatal("overcommitted two-wallet reservation succeeded")
	}
	for _, wallet := range []int{1, 2} {
		if err := store.ReserveFundingSpreadCapital(ctx, fmt.Sprintf("spread-c-%d", wallet), []FundingSpreadCapitalClaim{fundingSpreadTestClaim(wallet, 30, 100)}); err != nil {
			t.Fatalf("failed two-wallet claim left a partial reservation on wallet %d: %v", wallet, err)
		}
	}
}

func TestFundingSpreadCapitalReservationsSerializeConcurrentBots(t *testing.T) {
	store, err := NewSQLStorage(filepath.Join(t.TempDir(), "capital-race.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, botID := range []string{"spread-a", "spread-b"} {
		wg.Add(1)
		go func(botID string) {
			defer wg.Done()
			<-start
			results <- store.ReserveFundingSpreadCapital(context.Background(), botID, []FundingSpreadCapitalClaim{fundingSpreadTestClaim(3, 60, 100)})
		}(botID)
	}
	close(start)
	wg.Wait()
	close(results)
	twins := 0
	for err := range results {
		if err == nil {
			twins++
		}
	}
	if twins != 1 {
		t.Fatalf("successful concurrent claims = %d, want exactly one", twins)
	}
}

func TestFundingSpreadCapitalReservationRejectsUnsafeRestartAndRetainsClaim(t *testing.T) {
	store, err := NewSQLStorage(filepath.Join(t.TempDir(), "capital-retained.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()

	if err := store.ReserveFundingSpreadCapital(ctx, "spread-owner", []FundingSpreadCapitalClaim{fundingSpreadTestClaim(4, 60, 100)}); err != nil {
		t.Fatal(err)
	}
	if err := store.ReserveFundingSpreadCapital(ctx, "spread-other", []FundingSpreadCapitalClaim{fundingSpreadTestClaim(4, 30, 100)}); err != nil {
		t.Fatal(err)
	}
	if err := store.ReserveFundingSpreadCapital(ctx, "spread-owner", []FundingSpreadCapitalClaim{fundingSpreadTestClaim(4, 40, 80)}); err == nil {
		t.Fatal("same-Bot restart bypassed other Bot reservations after available balance fell")
	}
	rows, err := store.ListAccountWalletCapitalReservations(ctx, "", "", 10)
	retained := 0.0
	for _, row := range rows {
		retained += row.Amount
	}
	if err != nil || len(rows) != 2 || retained != 90 {
		t.Fatalf("rejected restart must retain all previous reservations: rows=%+v err=%v", rows, err)
	}
	if err := store.ReserveFundingSpreadCapital(ctx, "spread-new", []FundingSpreadCapitalClaim{fundingSpreadTestClaim(4, 11, 100)}); err == nil {
		t.Fatal("another Bot was allowed to consume capacity still reserved by the owners")
	}
	if err := store.ReleaseFundingSpreadCapital(ctx, "spread-owner", []FundingSpreadCapitalClaim{fundingSpreadTestClaim(4, 0, 0)}); err != nil {
		t.Fatalf("release verified flat reservation: %v", err)
	}
	if err := store.ReserveFundingSpreadCapital(ctx, "spread-new", []FundingSpreadCapitalClaim{fundingSpreadTestClaim(4, 70, 100)}); err != nil {
		t.Fatalf("capacity was not released: %v", err)
	}
}

func TestAccountWalletCapitalReservationIsSharedAcrossStrategies(t *testing.T) {
	store, err := NewSQLStorage(filepath.Join(t.TempDir(), "shared-wallet-capital.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	claim := AccountWalletCapitalClaim{WalletKey: fmt.Sprintf("%064x", 9), ReservationToken: fmt.Sprintf("%064x", 1009), Amount: 65, Available: 100}
	if err := store.ReserveAccountWalletCapital(ctx, "grid-bot", []AccountWalletCapitalClaim{claim}); err != nil {
		t.Fatal(err)
	}
	claim.Amount = 40
	if err := store.ReserveAccountWalletCapital(ctx, "funding-carry-bot", []AccountWalletCapitalClaim{claim}); err == nil {
		t.Fatal("different strategy bypassed an account wallet reservation")
	}
	claim.Amount = 35
	if err := store.ReserveAccountWalletCapital(ctx, "funding-carry-bot", []AccountWalletCapitalClaim{claim}); err != nil {
		t.Fatalf("account-wide capacity should be shared across strategy types: %v", err)
	}
}

func TestAccountWalletCapitalReservationAuditMapsNewClaimsAndKeepsLegacyVisible(t *testing.T) {
	store, err := NewSQLStorage(filepath.Join(t.TempDir(), "reservation-audit.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()

	mapped := AccountWalletCapitalClaim{WalletKey: fmt.Sprintf("%064x", 10), ReservationToken: fmt.Sprintf("%064x", 1010), Amount: 25, Available: 100,
		Exchange: "binance", Market: "futures", QuoteAsset: "USDT", Symbol: "BTCUSDT"}
	mapped.ObservationSequence, err = store.BeginAccountWalletBalanceObservation(ctx, mapped.WalletKey)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.ReserveAccountWalletCapital(ctx, "bot-mapped", []AccountWalletCapitalClaim{mapped}); err != nil {
		t.Fatal(err)
	}
	legacy := fundingSpreadTestClaim(11, 15, 100)
	if err := store.ReserveAccountWalletCapital(ctx, "bot-legacy", []AccountWalletCapitalClaim{legacy}); err != nil {
		t.Fatal(err)
	}

	reservations, err := store.ListAccountWalletCapitalReservations(ctx, "", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(reservations) != 2 {
		t.Fatalf("first audit page plus lookahead count = %d, want 2", len(reservations))
	}
	if !reservations[0].Mapped || reservations[0].Exchange != "binance" || reservations[0].Market != "futures" ||
		reservations[0].QuoteAsset != "USDT" || reservations[0].Symbol != "BTCUSDT" || reservations[0].Amount != 25 {
		t.Fatalf("mapped reservation = %+v", reservations[0])
	}
	if reservations[1].Mapped || reservations[1].Exchange != "" || reservations[1].Amount != 15 {
		t.Fatalf("legacy reservation should remain visible as unmapped: %+v", reservations[1])
	}
	reservations, err = store.ListAccountWalletCapitalReservations(ctx, reservations[0].WalletKey, reservations[0].BotKey, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(reservations) != 1 || reservations[0].Mapped {
		t.Fatalf("second audit page should contain the legacy reservation: %+v", reservations)
	}
	if _, err := store.ListAccountWalletCapitalReservations(ctx, reservations[0].WalletKey, reservations[0].BotKey, 1); err != nil {
		t.Fatalf("empty audit page should be valid: %v", err)
	}
	if err := store.ReleaseAccountWalletCapital(ctx, "bot-mapped", []AccountWalletCapitalClaim{mapped}); err != nil {
		t.Fatal(err)
	}
	reservations, err = store.ListAccountWalletCapitalReservations(ctx, "", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(reservations) != 1 || reservations[0].Mapped {
		t.Fatalf("release should atomically remove mapped reservation and metadata: %+v", reservations)
	}
}

func TestAccountWalletCapitalReleaseCannotDeleteReplacementGeneration(t *testing.T) {
	store, err := NewSQLStorage(filepath.Join(t.TempDir(), "reservation-generation.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	oldGeneration := fundingSpreadTestClaim(14, 20, 100)
	newGeneration := oldGeneration
	newGeneration.ReservationToken = fmt.Sprintf("%064x", 2014)
	newGeneration.Amount = 30
	if err := store.ReserveAccountWalletCapital(ctx, "same-bot", []AccountWalletCapitalClaim{oldGeneration}); err != nil {
		t.Fatal(err)
	}
	if err := migrateFundingSpreadCapitalTables(store.db); err != nil {
		t.Fatalf("re-running startup migrations must be idempotent: %v", err)
	}
	if err := store.ReserveAccountWalletCapital(ctx, "same-bot", []AccountWalletCapitalClaim{newGeneration}); err != nil {
		t.Fatal(err)
	}
	if err := store.ReleaseAccountWalletCapital(ctx, "same-bot", []AccountWalletCapitalClaim{oldGeneration}); err == nil {
		t.Fatal("stale runtime generation released its replacement's claim")
	}
	rows, err := store.ListAccountWalletCapitalReservations(ctx, "", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Amount != newGeneration.Amount {
		t.Fatalf("replacement claim after stale release attempt = %+v", rows)
	}
	if err := store.ReleaseAccountWalletCapital(ctx, "same-bot", []AccountWalletCapitalClaim{newGeneration}); err != nil {
		t.Fatalf("current runtime could not release its own claim: %v", err)
	}
}

func TestAccountWalletCapitalReservationBotChecker(t *testing.T) {
	store, err := NewSQLStorage(filepath.Join(t.TempDir(), "reservation-bot-check.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	claim := fundingSpreadTestClaim(25, 30, 100)
	if err := store.ReserveAccountWalletCapital(ctx, "bot-check", []AccountWalletCapitalClaim{claim}); err != nil {
		t.Fatal(err)
	}
	reserved, err := store.HasAccountWalletCapitalReservation(ctx, "bot-check")
	if err != nil || !reserved {
		t.Fatalf("claim check for owning Bot = %v, err=%v; want true", reserved, err)
	}
	reserved, err = store.HasAccountWalletCapitalReservation(ctx, "different-bot")
	if err != nil || reserved {
		t.Fatalf("claim check for other Bot = %v, err=%v; want false", reserved, err)
	}
	if err := store.ReleaseAccountWalletCapital(ctx, "bot-check", []AccountWalletCapitalClaim{claim}); err != nil {
		t.Fatal(err)
	}
	reserved, err = store.HasAccountWalletCapitalReservation(ctx, "bot-check")
	if err != nil || reserved {
		t.Fatalf("claim check after release = %v, err=%v; want false", reserved, err)
	}
}

func TestFundingSpreadCapitalTokenMigrationPreservesLegacyReservations(t *testing.T) {
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "legacy-capital-reservation.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, version := range []string{"2026093001", "2026093002"} {
		data, err := fundingSpreadCapitalMigrations.ReadFile("migrations/" + version + "_funding_spread_capital_sqlite.up.sql")
		if err != nil {
			t.Fatal(err)
		}
		for _, statement := range strings.Split(string(data), ";") {
			if statement = strings.TrimSpace(statement); statement != "" {
				if _, err := db.Exec(statement); err != nil {
					t.Fatalf("apply legacy migration %s: %v", version, err)
				}
			}
		}
	}
	walletKey, botKey := fmt.Sprintf("%064x", 21), fundingSpreadBotKey("legacy-bot")
	if _, err := db.Exec(`INSERT INTO funding_spread_capital_reservations (wallet_key, bot_key, amount) VALUES (?, ?, ?)`, walletKey, botKey, 42); err != nil {
		t.Fatal(err)
	}
	if err := migrateFundingSpreadCapitalTables(db); err != nil {
		t.Fatalf("upgrade legacy reservation schema: %v", err)
	}
	var amount float64
	var token string
	if err := db.QueryRow(`SELECT amount, reservation_token FROM funding_spread_capital_reservations WHERE wallet_key = ? AND bot_key = ?`, walletKey, botKey).Scan(&amount, &token); err != nil {
		t.Fatal(err)
	}
	if amount != 42 || token != "" {
		t.Fatalf("legacy reservation after generation migration amount=%v token=%q, want preserved amount and empty legacy generation", amount, token)
	}
	if err := migrateFundingSpreadCapitalTables(db); err != nil {
		t.Fatalf("re-running upgraded schema migrations: %v", err)
	}
}

func TestAccountWalletCapitalReservationAuditBoundsPageSize(t *testing.T) {
	store, err := NewSQLStorage(filepath.Join(t.TempDir(), "reservation-audit-page-bounds.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	for _, tc := range []struct {
		afterWalletKey string
		afterBotKey    string
		limit          int
	}{{afterWalletKey: "abc", afterBotKey: "", limit: 10}, {afterWalletKey: fmt.Sprintf("%064x", 1), limit: 10}, {limit: 0}, {limit: AccountWalletCapitalReservationAuditPageSize + 1}} {
		if _, err := store.ListAccountWalletCapitalReservations(context.Background(), tc.afterWalletKey, tc.afterBotKey, tc.limit); err == nil {
			t.Fatalf("invalid audit page cursor=%q/%q limit=%d was accepted", tc.afterWalletKey, tc.afterBotKey, tc.limit)
		}
	}
}

func TestAccountWalletCapitalReservationAuditCursorSurvivesClaimUpdates(t *testing.T) {
	store, err := NewSQLStorage(filepath.Join(t.TempDir(), "reservation-audit-cursor-update.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()
	firstClaim := fundingSpreadTestClaim(1, 10, 100)
	secondClaim := fundingSpreadTestClaim(2, 10, 100)
	if err := store.ReserveAccountWalletCapital(ctx, "bot-first", []AccountWalletCapitalClaim{firstClaim}); err != nil {
		t.Fatal(err)
	}
	if err := store.ReserveAccountWalletCapital(ctx, "bot-second", []AccountWalletCapitalClaim{secondClaim}); err != nil {
		t.Fatal(err)
	}

	firstPage, err := store.ListAccountWalletCapitalReservations(ctx, "", "", 1)
	if err != nil || len(firstPage) != 2 {
		t.Fatalf("first audit page = %+v, err=%v; want row plus lookahead", firstPage, err)
	}
	firstSeen := firstPage[0]
	firstClaim.Amount = 20
	if err := store.ReserveAccountWalletCapital(ctx, "bot-first", []AccountWalletCapitalClaim{firstClaim}); err != nil {
		t.Fatal(err)
	}

	secondPage, err := store.ListAccountWalletCapitalReservations(ctx, firstSeen.WalletKey, firstSeen.BotKey, 1)
	if err != nil || len(secondPage) != 1 {
		t.Fatalf("second audit page after an earlier claim update = %+v, err=%v; want one remaining claim", secondPage, err)
	}
	if secondPage[0].WalletKey != secondClaim.WalletKey {
		t.Fatalf("cursor repeated or skipped a claim after update: first=%+v second=%+v", firstSeen, secondPage[0])
	}
}

func TestAccountWalletCapitalReservationRejectsUnsafeMetadata(t *testing.T) {
	store, err := NewSQLStorage(filepath.Join(t.TempDir(), "reservation-audit-invalid.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	claim := AccountWalletCapitalClaim{WalletKey: fmt.Sprintf("%064x", 12), ReservationToken: fmt.Sprintf("%064x", 1012), Amount: 10, Available: 100,
		Exchange: "binance\nsecret", Market: "futures", QuoteAsset: "USDT", Symbol: "BTCUSDT"}
	if err := store.ReserveAccountWalletCapital(context.Background(), "bot-invalid", []AccountWalletCapitalClaim{claim}); err == nil {
		t.Fatal("metadata containing control characters was accepted")
	}
	items, err := store.ListAccountWalletCapitalReservations(context.Background(), "", "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 0 {
		t.Fatalf("invalid metadata transaction left reservation behind: %+v", items)
	}
}
