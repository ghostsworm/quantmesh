package storage

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"
)

// TestMySQLProfitWithdrawRules requires a disposable, empty MySQL schema in
// QUANTMESH_TEST_MYSQL_DSN. It exercises the production migration and lock path.
func TestMySQLProfitWithdrawRules(t *testing.T) {
	dsn := os.Getenv("QUANTMESH_TEST_MYSQL_DSN")
	if dsn == "" {
		t.Skip("set QUANTMESH_TEST_MYSQL_DSN to a disposable MySQL schema")
	}
	st, err := NewStorage("mysql", dsn)
	if err != nil {
		t.Fatalf("initialize MySQL storage and migrations: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if _, err := st.db.Exec(`ALTER TABLE profit_withdraw_rules DROP COLUMN claim_started_at`); err != nil {
		t.Fatalf("prepare legacy MySQL withdrawal rules schema: %v", err)
	}
	if err := migrateProfitWithdrawRulesTableMySQL(st.db); err != nil {
		t.Fatalf("migrate legacy MySQL withdrawal rules schema: %v", err)
	}

	accountID := fmt.Sprintf("codex-profit-readiness-%d", time.Now().UnixNano())
	start := make(chan struct{})
	results := make(chan error, 2)
	ids := []string{accountID + "-a", accountID + "-b"}
	var workers sync.WaitGroup
	for _, id := range ids {
		workers.Add(1)
		go func(id string) {
			defer workers.Done()
			<-start
			results <- st.UpsertProfitWithdrawRule(accountID, &ProfitWithdrawRule{
				ID: id, AccountScope: "mysql-scope", ExchangeID: "Binance", StrategyID: "btcusdt",
				Enabled: true, WithdrawRatio: 0.5, Frequency: "immediate", Destination: "account",
			})
		}(id)
	}
	close(start)
	workers.Wait()
	first, second := <-results, <-results
	if (first == nil) == (second == nil) {
		t.Fatalf("concurrent MySQL upserts must have exactly one winner: first=%v second=%v", first, second)
	}
	loser := first
	if loser == nil {
		loser = second
	}
	if !errors.Is(loser, ErrOverlappingProfitWithdrawRule) {
		t.Fatalf("concurrent MySQL loser should report duplicate stream: %v", loser)
	}
	rules, err := st.ListProfitWithdrawRules(accountID)
	if err != nil || len(rules) != 1 {
		t.Fatalf("list concurrent MySQL rules: count=%d err=%v", len(rules), err)
	}
	rule := rules[0]
	rule.WithdrawRatio = 0.25
	if err := st.UpsertProfitWithdrawRule(accountID, rule); err != nil {
		t.Fatalf("MySQL duplicate-key update: %v", err)
	}
	foreignAttempt := *rule
	foreignAttempt.AccountScope = "another-scope"
	if err := st.UpsertProfitWithdrawRule(accountID+"-other", &foreignAttempt); err == nil {
		t.Fatal("MySQL upsert must reject another account's rule ID")
	}
	if err := st.DeleteProfitWithdrawRule(accountID, rule.ID); err != nil {
		t.Fatalf("delete MySQL withdrawal rule: %v", err)
	}
	rules, err = st.ListProfitWithdrawRules(accountID)
	if err != nil || len(rules) != 0 {
		t.Fatalf("MySQL rule remains after delete: count=%d err=%v", len(rules), err)
	}

	since := time.Now().UTC().Add(-time.Hour)
	startManual := make(chan struct{})
	manualResults := make(chan error, 2)
	var manualWorkers sync.WaitGroup
	for _, id := range []string{accountID + "-manual-a", accountID + "-manual-b"} {
		manualWorkers.Add(1)
		go func(id string) {
			defer manualWorkers.Done()
			<-startManual
			manualResults <- st.ReserveManualWithdrawRecord(&ProfitWithdrawRecord{
				ID: id, AccountID: accountID, AccountScope: "mysql-scope", ExchangeID: "binance", StrategyID: "BTCUSDT",
				Amount: 40, NetAmount: 40, Currency: "USDT", Type: "manual", Status: "processing", Destination: "account", CreatedAt: time.Now().UTC(),
			}, since, "", 50)
		}(id)
	}
	close(startManual)
	manualWorkers.Wait()
	manualA, manualB := <-manualResults, <-manualResults
	if (manualA == nil) == (manualB == nil) {
		t.Fatalf("concurrent MySQL manual reservations must have exactly one winner: first=%v second=%v", manualA, manualB)
	}
	reserved, err := st.SumReservedWithdrawAmountForStream(accountID, "mysql-scope", "BINANCE", "btcusdt", since)
	if err != nil || reserved != 40 {
		t.Fatalf("MySQL manual stream reservation=%v err=%v, want 40", reserved, err)
	}

	claimAccount := accountID + "-claim-recovery"
	claimRuleID := claimAccount + "-rule"
	if err := st.UpsertProfitWithdrawRule(claimAccount, &ProfitWithdrawRule{
		ID: claimRuleID, AccountScope: "mysql-claim-scope", ExchangeID: "binance", StrategyID: "ETHUSDT",
		Enabled: true, WithdrawRatio: 0.5, Frequency: "immediate", Destination: "account",
	}); err != nil {
		t.Fatal(err)
	}
	if claimed, err := st.ClaimProfitWithdrawRule(claimRuleID, "mysql-old-claim"); err != nil || !claimed {
		t.Fatalf("claim MySQL rule: claimed=%v err=%v", claimed, err)
	}
	if _, err := st.db.Exec(`UPDATE profit_withdraw_rules SET claim_started_at = ? WHERE id = ?`, time.Now().Add(-time.Hour), claimRuleID); err != nil {
		t.Fatal(err)
	}
	recovered, err := st.RecoverAbandonedProfitWithdrawRuleClaims(time.Now().UTC())
	if err != nil || recovered != 1 {
		t.Fatalf("recover stale MySQL claim: count=%d err=%v", recovered, err)
	}
	if claimed, err := st.ClaimProfitWithdrawRule(claimRuleID, "mysql-current-claim"); err != nil || !claimed {
		t.Fatalf("reclaim MySQL rule: claimed=%v err=%v", claimed, err)
	}
	record := &ProfitWithdrawRecord{ID: claimAccount + "-stale-reservation", RuleID: claimRuleID,
		AccountID: claimAccount, AccountScope: "mysql-claim-scope", ClaimID: "mysql-old-claim",
		ExchangeID: "binance", StrategyID: "ETHUSDT", Amount: 2, NetAmount: 2, Currency: "USDT",
		Type: "auto", Status: "processing", Destination: "account", CreatedAt: time.Now().UTC()}
	if err := st.SaveWithdrawRecordForClaim(record); err == nil {
		t.Fatal("stale MySQL worker must not create a reservation after claim recovery")
	}
	record.ID = claimAccount + "-current-reservation"
	record.ClaimID = "mysql-current-claim"
	if err := st.SaveWithdrawRecordForClaim(record); err != nil {
		t.Fatalf("save current MySQL claim reservation: %v", err)
	}
}
