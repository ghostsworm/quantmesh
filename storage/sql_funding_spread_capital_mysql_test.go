package storage

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"
)

// TestMySQLAccountWalletCapitalReservations requires QUANTMESH_MYSQL_TEST_DSN
// to point at a disposable schema. It exercises the production InnoDB lock path.
func TestMySQLAccountWalletCapitalReservations(t *testing.T) {
	dsn := os.Getenv("QUANTMESH_MYSQL_TEST_DSN")
	if dsn == "" {
		t.Skip("requires QUANTMESH_MYSQL_TEST_DSN pointing to a disposable MySQL schema")
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal("open isolated MySQL test database:", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		t.Fatal("connect to isolated MySQL test database:", err)
	}
	if err := migrateFundingSpreadCapitalTablesMySQL(db); err != nil {
		t.Fatal("apply account wallet capital migrations:", err)
	}
	store := &SQLStorage{db: db, dbType: "mysql"}
	unique := fmt.Sprintf("mysql-capital-%d", time.Now().UTC().UnixNano())
	walletKey := mysqlCapitalTestWalletKey(unique + "-concurrent")
	claims := []AccountWalletCapitalClaim{{WalletKey: walletKey, ReservationToken: mysqlCapitalTestWalletKey(unique + "-generation"), Amount: 60, Available: 100}}
	botIDs := []string{unique + "-a", unique + "-b"}
	t.Cleanup(func() {
		for _, botID := range botIDs {
			_ = store.ReleaseAccountWalletCapital(context.Background(), botID, claims)
		}
	})

	start := make(chan struct{})
	results := make(chan error, len(botIDs))
	var workers sync.WaitGroup
	for _, botID := range botIDs {
		workers.Add(1)
		go func(botID string) {
			defer workers.Done()
			<-start
			results <- store.ReserveAccountWalletCapital(ctx, botID, claims)
		}(botID)
	}
	close(start)
	workers.Wait()
	close(results)
	winners := 0
	for err := range results {
		if err == nil {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("concurrent MySQL wallet reservation winners=%d, want exactly one", winners)
	}
	var rows int
	var reserved float64
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*), COALESCE(SUM(amount), 0)
		FROM funding_spread_capital_reservations WHERE wallet_key = ?`, walletKey).Scan(&rows, &reserved); err != nil {
		t.Fatal("read concurrent MySQL reservation result:", err)
	}
	if rows != 1 || reserved != 60 {
		t.Fatalf("concurrent MySQL reservations rows=%d amount=%v, want 1 row/60", rows, reserved)
	}

	firstWallet := mysqlCapitalTestWalletKey(unique + "-first")
	secondWallet := mysqlCapitalTestWalletKey(unique + "-second")
	blockerID, atomicBotID := unique+"-blocker", unique+"-atomic"
	blockerClaims := []AccountWalletCapitalClaim{{WalletKey: secondWallet, ReservationToken: mysqlCapitalTestWalletKey(unique + "-blocker-generation"), Amount: 60, Available: 100}}
	atomicToken := mysqlCapitalTestWalletKey(unique + "-atomic-generation")
	t.Cleanup(func() {
		_ = store.ReleaseAccountWalletCapital(context.Background(), blockerID, blockerClaims)
		_ = store.ReleaseAccountWalletCapital(context.Background(), atomicBotID, []AccountWalletCapitalClaim{
			{WalletKey: firstWallet, ReservationToken: atomicToken}, {WalletKey: secondWallet, ReservationToken: atomicToken},
		})
	})
	if err := store.ReserveAccountWalletCapital(ctx, blockerID, blockerClaims); err != nil {
		t.Fatal("reserve blocker wallet capacity:", err)
	}
	atomicClaims := []AccountWalletCapitalClaim{
		{WalletKey: firstWallet, ReservationToken: atomicToken, Amount: 70, Available: 100},
		{WalletKey: secondWallet, ReservationToken: atomicToken, Amount: 70, Available: 100},
	}
	if err := store.ReserveAccountWalletCapital(ctx, atomicBotID, atomicClaims); err == nil {
		t.Fatal("overcommitted MySQL two-wallet reservation succeeded")
	}
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM funding_spread_capital_reservations
		WHERE wallet_key = ? AND bot_key = ?`, firstWallet, fundingSpreadBotKey(atomicBotID)).Scan(&rows); err != nil {
		t.Fatal("verify MySQL atomic rollback:", err)
	}
	if rows != 0 {
		t.Fatalf("failed MySQL two-wallet claim left %d partial rows", rows)
	}
}

func mysqlCapitalTestWalletKey(seed string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(seed)))
}
