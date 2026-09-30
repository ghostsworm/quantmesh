package storage

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestProfitWithdrawRulesRejectOverlappingEnabledStreams(t *testing.T) {
	st, err := NewSQLStorage(t.TempDir() + "/withdraw-duplicate-stream.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	first := &ProfitWithdrawRule{ID: "rule-a", AccountScope: "scope-a", ExchangeID: "Binance", StrategyID: "btcusdt",
		Enabled: true, WithdrawRatio: 0.5, Frequency: "immediate", Destination: "account"}
	if err := st.UpsertProfitWithdrawRule("acct", first); err != nil {
		t.Fatal(err)
	}
	second := &ProfitWithdrawRule{ID: "rule-b", AccountScope: "scope-a", ExchangeID: "binance", StrategyID: "BTCUSDT",
		Enabled: true, WithdrawRatio: 0.5, Frequency: "daily", Destination: "account"}
	if err := st.UpsertProfitWithdrawRule("acct", second); !errors.Is(err, ErrOverlappingProfitWithdrawRule) {
		t.Fatalf("upsert duplicate stream error=%v, want overlap sentinel", err)
	}
	if err := st.ReplaceProfitWithdrawRules("acct", []*ProfitWithdrawRule{first, second}); !errors.Is(err, ErrOverlappingProfitWithdrawRule) {
		t.Fatalf("replace duplicate stream error=%v, want overlap sentinel", err)
	}
	rules, err := st.ListProfitWithdrawRules("acct")
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 || rules[0].ID != "rule-a" {
		t.Fatalf("rejected replacement must preserve existing rule: %+v", rules)
	}
}

func TestProfitWithdrawRulesRejectUnsafeAmountsBeforePersistence(t *testing.T) {
	st, err := NewSQLStorage(t.TempDir() + "/withdraw-invalid-amounts.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	base := &ProfitWithdrawRule{ID: "valid-rule", AccountScope: "scope-a", ExchangeID: "binance", StrategyID: "BTCUSDT",
		Enabled: true, TriggerAmount: 10, WithdrawRatio: 0.5, Frequency: "immediate", Destination: "account"}
	invalid := []*ProfitWithdrawRule{
		{ID: "ratio-over-one", Enabled: true, WithdrawRatio: 1.01},
		{ID: "ratio-nan", WithdrawRatio: math.NaN()},
		{ID: "trigger-infinite", TriggerAmount: math.Inf(1)},
		{ID: "minimum-negative", MinWithdrawAmount: -1},
		{ID: "maximum-infinite", MaxWithdrawAmount: floatPointer(math.Inf(1))},
		{ID: "minimum-over-maximum", MinWithdrawAmount: 5, MaxWithdrawAmount: floatPointer(4)},
	}
	for _, candidate := range invalid {
		candidate.AccountScope, candidate.ExchangeID, candidate.StrategyID = base.AccountScope, base.ExchangeID, base.StrategyID
		candidate.Enabled, candidate.Frequency, candidate.Destination = true, base.Frequency, base.Destination
		if candidate.ID != "ratio-over-one" && candidate.ID != "ratio-nan" {
			candidate.WithdrawRatio = base.WithdrawRatio
		}
		if err := st.UpsertProfitWithdrawRule("acct", candidate); err == nil {
			t.Errorf("unsafe upsert %q was accepted", candidate.ID)
		}
		if err := st.ReplaceProfitWithdrawRules("acct", []*ProfitWithdrawRule{candidate}); err == nil {
			t.Errorf("unsafe replacement %q was accepted", candidate.ID)
		}
	}
	if err := st.UpsertProfitWithdrawRule("acct", base); err != nil {
		t.Fatalf("valid rule rejected: %v", err)
	}
	rules, err := st.ListProfitWithdrawRules("acct")
	if err != nil || len(rules) != 1 || rules[0].ID != base.ID {
		t.Fatalf("invalid rules reached persistence: rules=%+v err=%v", rules, err)
	}
}

func TestProfitWithdrawRulesCannotTakeOverUnknownManualReservation(t *testing.T) {
	st, err := NewSQLStorage(t.TempDir() + "/withdraw-unknown-manual-status.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.SaveWithdrawRecord(&ProfitWithdrawRecord{ID: "unknown-manual", AccountID: "acct", AccountScope: "scope-a",
		ExchangeID: "binance", StrategyID: "BTCUSDT", Amount: 10, NetAmount: 10, Currency: "USDT", Type: "manual",
		Status: "exchange_unknown", Destination: "account", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	rule := &ProfitWithdrawRule{ID: "auto-rule", AccountScope: "scope-a", ExchangeID: "binance", StrategyID: "BTCUSDT",
		Enabled: true, TriggerAmount: 10, WithdrawRatio: 0.5, Frequency: "immediate", Destination: "account"}
	if err := st.UpsertProfitWithdrawRule("acct", rule); err == nil {
		t.Fatal("unknown manual reservation must block enabling an overlapping automatic rule")
	}
	if err := st.ReplaceProfitWithdrawRules("acct", []*ProfitWithdrawRule{rule}); err == nil {
		t.Fatal("unknown manual reservation must block replacing in an overlapping automatic rule")
	}
}

func TestListProfitWithdrawRulesFailsOnMalformedPersistedRow(t *testing.T) {
	st, err := NewSQLStorage(t.TempDir() + "/withdraw-malformed-rule.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	rule := &ProfitWithdrawRule{ID: "broken-rule", AccountScope: "scope-a", ExchangeID: "binance", StrategyID: "BTCUSDT",
		Enabled: true, WithdrawRatio: 0.5, Frequency: "immediate", Destination: "account"}
	if err := st.UpsertProfitWithdrawRule("acct", rule); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(`UPDATE profit_withdraw_rules SET trigger_amount = 'not-a-number' WHERE id = ?`, rule.ID); err != nil {
		t.Fatal(err)
	}
	got, err := st.ListProfitWithdrawRules("acct")
	if err == nil {
		t.Fatalf("malformed persisted rule was silently accepted: %+v", got)
	}
	if got != nil {
		t.Fatalf("malformed query must not return a partial rule set: %+v", got)
	}
}

func TestReplaceProfitWithdrawRulesRejectsNilWithoutDeletingExistingRules(t *testing.T) {
	st, err := NewSQLStorage(t.TempDir() + "/withdraw-nil-replacement-rule.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	existing := &ProfitWithdrawRule{ID: "existing-rule", AccountScope: "scope-a", ExchangeID: "binance", StrategyID: "BTCUSDT",
		Enabled: true, WithdrawRatio: 0.5, Frequency: "daily", Destination: "account"}
	if err := st.UpsertProfitWithdrawRule("acct", existing); err != nil {
		t.Fatal(err)
	}
	if err := st.ReplaceProfitWithdrawRules("acct", []*ProfitWithdrawRule{nil}); err == nil {
		t.Fatal("replacement with a nil rule was accepted")
	}
	rules, err := st.ListProfitWithdrawRules("acct")
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 || rules[0].ID != existing.ID {
		t.Fatalf("rejected replacement must preserve existing rules: %+v", rules)
	}
}

func TestConcurrentUpsertCannotCreateOverlappingWithdrawStreams(t *testing.T) {
	st, err := NewSQLStorage(t.TempDir() + "/withdraw-concurrent-rules.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, id := range []string{"concurrent-a", "concurrent-b"} {
		go func(id string) {
			<-start
			results <- st.UpsertProfitWithdrawRule("acct", &ProfitWithdrawRule{
				ID: id, AccountScope: "scope-a", ExchangeID: "binance", StrategyID: "BTCUSDT",
				Enabled: true, WithdrawRatio: 0.5, Frequency: "immediate", Destination: "account",
			})
		}(id)
	}
	close(start)
	first, second := <-results, <-results
	if (first == nil) == (second == nil) {
		t.Fatalf("concurrent upserts must have exactly one winner: first=%v second=%v", first, second)
	}
	loser := first
	if loser == nil {
		loser = second
	}
	if !errors.Is(loser, ErrOverlappingProfitWithdrawRule) {
		t.Fatalf("losing upsert should report duplicate accounting stream, got %v", loser)
	}
	rules, err := st.ListProfitWithdrawRules("acct")
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 || !rules[0].Enabled {
		t.Fatalf("stored overlapping rules=%+v, want exactly one enabled rule", rules)
	}
}

func TestProfitWithdrawUpsertUsesDatabaseDialect(t *testing.T) {
	sqliteSQL := profitWithdrawUpsertSQL("sqlite")
	mysqlSQL := profitWithdrawUpsertSQL("mysql")
	if !strings.Contains(sqliteSQL, "ON CONFLICT(id)") || strings.Contains(sqliteSQL, "ON DUPLICATE KEY") {
		t.Fatalf("unexpected SQLite upsert statement: %s", sqliteSQL)
	}
	if !strings.Contains(mysqlSQL, "ON DUPLICATE KEY UPDATE") || strings.Contains(mysqlSQL, "excluded.") {
		t.Fatalf("unexpected MySQL upsert statement: %s", mysqlSQL)
	}
}

func TestUpsertProfitWithdrawRuleCannotTakeOverAnotherAccountID(t *testing.T) {
	st, err := NewSQLStorage(t.TempDir() + "/withdraw-rule-owner.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	owned := &ProfitWithdrawRule{ID: "owned-rule", AccountScope: "scope-b", ExchangeID: "binance", StrategyID: "BTCUSDT",
		Enabled: true, WithdrawRatio: 0.5, Frequency: "immediate", Destination: "account"}
	if err := st.UpsertProfitWithdrawRule("account-b", owned); err != nil {
		t.Fatal(err)
	}
	attack := &ProfitWithdrawRule{ID: "owned-rule", AccountScope: "scope-a", ExchangeID: "bybit", StrategyID: "ETHUSDT",
		Enabled: true, WithdrawRatio: 1, Frequency: "immediate", Destination: "wallet", WalletAddress: "attacker-controlled"}
	if err := st.UpsertProfitWithdrawRule("account-a", attack); err == nil {
		t.Fatal("cross-account rule ID takeover must fail")
	}
	rules, err := st.ListProfitWithdrawRules("account-b")
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 || rules[0].ExchangeID != "binance" || rules[0].AccountScope != "scope-b" || rules[0].WalletAddress != "" {
		t.Fatalf("account-b rule changed after rejected takeover: %+v", rules)
	}
}

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

func TestAbandonedWithdrawalClaimCanRecoverOnlyBeforeReservation(t *testing.T) {
	st, err := NewSQLStorage(t.TempDir() + "/withdraw-abandoned-claim.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	rule := &ProfitWithdrawRule{ID: "recover-rule", AccountScope: "scope-a", ExchangeID: "binance", StrategyID: "BTCUSDT",
		Enabled: true, WithdrawRatio: 0.5, Frequency: "immediate", Destination: "account"}
	if err := st.UpsertProfitWithdrawRule("acct", rule); err != nil {
		t.Fatal(err)
	}
	if claimed, err := st.ClaimProfitWithdrawRule(rule.ID, "old-claim"); err != nil || !claimed {
		t.Fatalf("initial claim failed: claimed=%v err=%v", claimed, err)
	}
	cutoff := time.Now().UTC()
	if _, err := st.db.Exec(`UPDATE profit_withdraw_rules SET claim_started_at = ? WHERE id = ?`, cutoff.Add(-time.Hour), rule.ID); err != nil {
		t.Fatal(err)
	}
	recovered, err := st.RecoverAbandonedProfitWithdrawRuleClaims(cutoff)
	if err != nil || recovered != 1 {
		t.Fatalf("orphaned claim should be recovered: recovered=%d err=%v", recovered, err)
	}
	if claimed, err := st.ClaimProfitWithdrawRule(rule.ID, "new-claim"); err != nil || !claimed {
		t.Fatalf("recovered rule should be claimable: claimed=%v err=%v", claimed, err)
	}
	record := &ProfitWithdrawRecord{ID: "stale-reservation", RuleID: rule.ID, AccountID: "acct", AccountScope: "scope-a",
		ClaimID: "old-claim", ExchangeID: "binance", StrategyID: "BTCUSDT", Amount: 1, NetAmount: 1,
		Currency: "USDT", Type: "auto", Status: "processing", Destination: "account", CreatedAt: time.Now().UTC()}
	windowStart := record.CreatedAt.Add(-time.Minute)
	boundaryManual := &ProfitWithdrawRecord{ID: "same-checkpoint-manual", AccountID: "acct", AccountScope: "scope-a",
		ExchangeID: "binance", StrategyID: "BTCUSDT", Amount: 0.5, NetAmount: 0.5, Currency: "USDT",
		Type: "manual", Status: "completed", CreatedAt: windowStart}
	if err := st.SaveWithdrawRecord(boundaryManual); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveWithdrawRecordForClaim(record, windowStart, 10); err == nil {
		t.Fatal("stale worker must not create a transfer reservation after its claim is recovered")
	}
	record.ID = "current-reservation"
	record.ClaimID = "new-claim"
	if err := st.SaveWithdrawRecordForClaim(record, windowStart, 1.5); err != nil {
		t.Fatalf("current claim should atomically create its reservation: %v", err)
	}
	recovered, err = st.RecoverAbandonedProfitWithdrawRuleClaims(time.Now().Add(time.Hour))
	if err != nil || recovered != 0 {
		t.Fatalf("claim with an existing transfer reservation must not be recovered: recovered=%d err=%v", recovered, err)
	}
	if err := st.UpdateWithdrawRecordStatus(record.ID, "completed", "confirmed-transfer", ""); err != nil {
		t.Fatal(err)
	}
	recovered, err = st.RecoverAbandonedProfitWithdrawRuleClaims(time.Now().Add(time.Hour))
	if err != nil || recovered != 1 {
		t.Fatalf("stale claim with a terminal, confirmed transfer should be released: recovered=%d err=%v", recovered, err)
	}
	reserved, err := st.SumReservedWithdrawAmountForStream("acct", "scope-a", "binance", "BTCUSDT", record.CreatedAt.Add(-time.Minute))
	if err != nil || reserved != record.Amount+boundaryManual.Amount {
		t.Fatalf("confirmed transfer and same-checkpoint manual withdrawal must remain reserved after releasing its claim: reserved=%v err=%v", reserved, err)
	}
	if claimed, err := st.ClaimProfitWithdrawRule(rule.ID, "after-completion"); err != nil || !claimed {
		t.Fatalf("completed transfer claim should be reclaimable: claimed=%v err=%v", claimed, err)
	}
	retry := *record
	retry.ID = "over-budget-reservation"
	retry.ClaimID = "after-completion"
	retry.Amount = 0.01
	retry.NetAmount = retry.Amount
	if err := st.SaveWithdrawRecordForClaim(&retry, windowStart, 1.5); err == nil {
		t.Fatal("completed transfer plus a new reservation must not exceed the verified window budget")
	}
}

func TestWithdrawalClaimRecoveryPreservesLegacyClaimsWithoutTimestamp(t *testing.T) {
	st, err := NewSQLStorage(t.TempDir() + "/withdraw-legacy-claim.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.UpsertProfitWithdrawRule("acct", &ProfitWithdrawRule{ID: "legacy-claim-rule", AccountScope: "scope-a",
		ExchangeID: "binance", StrategyID: "BTCUSDT", Enabled: true, WithdrawRatio: 0.5,
		Frequency: "immediate", Destination: "account"}); err != nil {
		t.Fatal(err)
	}
	if claimed, err := st.ClaimProfitWithdrawRule("legacy-claim-rule", "legacy-claim"); err != nil || !claimed {
		t.Fatalf("initial claim failed: claimed=%v err=%v", claimed, err)
	}
	if _, err := st.db.Exec(`UPDATE profit_withdraw_rules SET claim_started_at = NULL WHERE id = ?`, "legacy-claim-rule"); err != nil {
		t.Fatal(err)
	}
	recovered, err := st.RecoverAbandonedProfitWithdrawRuleClaims(time.Now().Add(24 * time.Hour))
	if err != nil || recovered != 0 {
		t.Fatalf("claim with unknown legacy age must remain protected: recovered=%d err=%v", recovered, err)
	}
	if claimed, err := st.ClaimProfitWithdrawRule("legacy-claim-rule", "replacement"); err != nil || claimed {
		t.Fatalf("recovery must not steal a legacy claim: claimed=%v err=%v", claimed, err)
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
		} else if i == 1003 {
			record.Status = "exchange_unknown"
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

func TestClaimProfitWithdrawRuleDoesNotClaimDisabledRule(t *testing.T) {
	st, err := NewSQLStorage(t.TempDir() + "/withdraw-disabled-claim.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.UpsertProfitWithdrawRule("acct", &ProfitWithdrawRule{
		ID: "disabled-rule", ExchangeID: "binance", AccountScope: "scope-a", Enabled: false,
		WithdrawRatio: 0.5, Frequency: "immediate", Destination: "account",
	}); err != nil {
		t.Fatal(err)
	}
	claimed, err := st.ClaimProfitWithdrawRule("disabled-rule", "claim-disabled")
	if err != nil {
		t.Fatal(err)
	}
	if claimed {
		t.Fatal("scheduler must not claim a rule disabled after its in-memory rule snapshot")
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
		Enabled: true, WithdrawRatio: 0.5, Frequency: "immediate", Destination: "account",
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

func TestResolveUnknownWithdrawalStatusWithOperatorEvidence(t *testing.T) {
	st, err := NewSQLStorage(t.TempDir() + "/resolve-unknown-withdraw-status.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.SaveWithdrawRecord(&ProfitWithdrawRecord{ID: "unknown-record", AccountID: "acct", AccountScope: "scope-a",
		ExchangeID: "binance", StrategyID: "BTCUSDT", Amount: 12, NetAmount: 12, Currency: "USDT", Type: "manual",
		Status: "exchange_unknown", Destination: "account", CreatedAt: time.Now().UTC().Add(-time.Minute)}); err != nil {
		t.Fatal(err)
	}
	evidence := "Reviewed the exact exchange account ledger, asset, amount and time window; no transfer occurred."
	if err := st.ResolvePendingWithdrawRecord("acct", "unknown-record", "failed", "ledger-reference-unknown", evidence); err != nil {
		t.Fatal(err)
	}
	record, err := st.GetWithdrawRecord("acct", "unknown-record")
	if err != nil {
		t.Fatal(err)
	}
	if record.Status != "failed" || record.TransferID != "ledger-reference-unknown" || !strings.Contains(record.FailedReason, evidence) {
		t.Fatalf("operator reconciliation did not durably resolve unknown status: %+v", record)
	}
}
