package storage

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestProfitWithdrawRulePersistsImmutableAccountScope(t *testing.T) {
	st, err := NewSQLStorage(t.TempDir() + "/withdraw-rule-scope.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })

	rule := &ProfitWithdrawRule{
		ID: "rule-scope", ExchangeID: "binance", AccountScope: "opaque-scope-a",
		Enabled: true, TriggerAmount: 10, WithdrawRatio: 0.5,
		Frequency: "immediate", Destination: "account",
	}
	if err := st.UpsertProfitWithdrawRule("same-prefix", rule); err != nil {
		t.Fatalf("upsert rule: %v", err)
	}
	rules, err := st.ListProfitWithdrawRules("same-prefix")
	if err != nil {
		t.Fatalf("list rules: %v", err)
	}
	if len(rules) != 1 || rules[0].AccountScope != "opaque-scope-a" {
		t.Fatalf("stored scope mismatch: %+v", rules)
	}

	rule.AccountScope = ""
	rule.ID = "rule-missing-scope"
	if err := st.UpsertProfitWithdrawRule("same-prefix", rule); err == nil {
		t.Fatal("rule without immutable account scope must be rejected")
	}
}

func TestClaimProfitWithdrawRuleIsAtomicAcrossCallers(t *testing.T) {
	st, err := NewSQLStorage(t.TempDir() + "/withdraw-claim.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.UpsertProfitWithdrawRule("acct", &ProfitWithdrawRule{
		ID: "claim-rule", ExchangeID: "binance", AccountScope: "scope-a", Enabled: true,
		WithdrawRatio: 0.5, Frequency: "immediate", Destination: "account",
	}); err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	type claimResult struct {
		token string
		won   bool
		err   error
	}
	results := make(chan claimResult, 2)
	var wg sync.WaitGroup
	for _, token := range []string{"claim-a", "claim-b"} {
		wg.Add(1)
		go func(token string) {
			defer wg.Done()
			<-start
			won, err := st.ClaimProfitWithdrawRule("claim-rule", token)
			results <- claimResult{token: token, won: won, err: err}
		}(token)
	}
	close(start)
	wg.Wait()
	close(results)
	winner := ""
	wins := 0
	for result := range results {
		if result.err != nil {
			t.Fatal(result.err)
		}
		if result.won {
			winner = result.token
			wins++
		}
	}
	if wins != 1 {
		t.Fatalf("claim winners=%d, want exactly one", wins)
	}
	loser := "claim-a"
	if winner == loser {
		loser = "claim-b"
	}
	if err := st.ReleaseProfitWithdrawRuleClaim("claim-rule", loser); err == nil {
		t.Fatal("non-owner must not release durable claim")
	}
	if err := st.UpsertProfitWithdrawRule("acct", &ProfitWithdrawRule{
		ID: "claim-rule", ExchangeID: "binance", AccountScope: "scope-a", Enabled: true,
		WithdrawRatio: 0.5, Frequency: "immediate", Destination: "account",
	}); err == nil {
		t.Fatal("claimed rule must not be edited")
	}
	if err := st.DeleteProfitWithdrawRule("acct", "claim-rule"); err == nil {
		t.Fatal("claimed rule must not be deleted")
	}
	if err := st.ReplaceProfitWithdrawRules("acct", nil); err == nil {
		t.Fatal("account rules must not be replaced while one has an active claim")
	}
	if err := st.ReleaseProfitWithdrawRuleClaim("claim-rule", winner); err != nil {
		t.Fatalf("winner release: %v", err)
	}
}

func TestSumReservedWithdrawAmountHasNoHistoryPageLimit(t *testing.T) {
	st, err := NewSQLStorage(t.TempDir() + "/withdraw-reservations.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	since := time.Now().UTC().Add(-time.Hour)
	for i := 0; i < 1005; i++ {
		record := &ProfitWithdrawRecord{
			ID: fmt.Sprintf("wd-%d", i), RuleID: "rule-a", AccountID: "acct",
			ExchangeID: "binance", Amount: 1, NetAmount: 1, Currency: "USDT",
			Type: "auto", Status: "pending", Destination: "account",
			CreatedAt: since.Add(time.Duration(i+1) * time.Second),
		}
		if i == 1004 {
			record.Status = "failed"
		}
		if err := st.SaveWithdrawRecord(record); err != nil {
			t.Fatalf("save reservation %d: %v", i, err)
		}
	}
	got, err := st.SumReservedWithdrawAmount("acct", "rule-a", since)
	if err != nil {
		t.Fatal(err)
	}
	if got != 1004 {
		t.Fatalf("reserved amount=%v, want 1004 across >1000 rows excluding failed", got)
	}
}

func TestResolvePendingWithdrawReleasesOnlyMatchingClaimAndAuditsEvidence(t *testing.T) {
	st, err := NewSQLStorage(t.TempDir() + "/withdraw-resolution.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.UpsertProfitWithdrawRule("acct", &ProfitWithdrawRule{
		ID: "rule-resolve", ExchangeID: "binance", AccountScope: "scope-a",
		WithdrawRatio: 0.5, Frequency: "immediate", Destination: "account",
	}); err != nil {
		t.Fatal(err)
	}
	claimed, err := st.ClaimProfitWithdrawRule("rule-resolve", "claim-resolve")
	if err != nil || !claimed {
		t.Fatalf("claim=(%v, %v)", claimed, err)
	}
	if err := st.SaveWithdrawRecord(&ProfitWithdrawRecord{
		ID: "record-resolve", RuleID: "rule-resolve", AccountID: "acct", AccountScope: "scope-a",
		ClaimID: "claim-resolve", ExchangeID: "binance", Amount: 25, NetAmount: 25,
		Currency: "USDT", Type: "auto", Status: "pending", Destination: "account", CreatedAt: time.Now().Add(-time.Minute),
	}); err != nil {
		t.Fatal(err)
	}
	evidence := "Checked the exchange transfer ledger for the exact time, account, asset and amount; no duplicate transfer exists."
	if err := st.ResolvePendingWithdrawRecord("acct", "record-resolve", "completed", "exchange-transfer-123", evidence); err != nil {
		t.Fatal(err)
	}
	record, err := st.GetWithdrawRecord("acct", "record-resolve")
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != "completed" || record.TransferID != "exchange-transfer-123" || !strings.Contains(record.Note, evidence) {
		t.Fatalf("resolved record lacks durable audit evidence: %+v", record)
	}
	rules, err := st.ListProfitWithdrawRules("acct")
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 {
		t.Fatalf("rules=%d, want 1", len(rules))
	}
	if claimed, err := st.ClaimProfitWithdrawRule("rule-resolve", "claim-next"); err != nil || !claimed {
		t.Fatalf("resolved claim was not released: (%v, %v)", claimed, err)
	}
	if err := st.ResolvePendingWithdrawRecord("acct", "record-resolve", "failed", "ref-2", evidence); err == nil {
		t.Fatal("a second resolution must not rewrite the audited decision")
	}
}
