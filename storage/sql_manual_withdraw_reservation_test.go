package storage

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestReserveManualWithdrawRecordSerializesAccountingWindow(t *testing.T) {
	st, err := NewSQLStorage(t.TempDir() + "/manual-withdraw-reserve.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	since := time.Now().UTC().Add(-time.Hour)
	var succeeded atomic.Int32
	var wg sync.WaitGroup
	for _, id := range []string{"manual-a", "manual-b"} {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			record := &ProfitWithdrawRecord{
				ID: id, AccountID: "acct", AccountScope: "scope-a", ExchangeID: "binance", StrategyID: "BTCUSDT",
				Amount: 40, NetAmount: 40, Currency: "USDT", Type: "manual", Status: "processing",
				Destination: "account", CreatedAt: time.Now().UTC(),
			}
			if err := st.ReserveManualWithdrawRecord(record, since, "", 50); err == nil {
				succeeded.Add(1)
			}
		}(id)
	}
	wg.Wait()
	if got := succeeded.Load(); got != 1 {
		t.Fatalf("successful concurrent reservations=%d, want exactly one", got)
	}
	records, err := st.GetWithdrawRecords("acct", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(records) != 1 || records[0].Status != "processing" {
		t.Fatalf("persisted reservations=%+v", records)
	}
}

func TestWithdrawalReservationsSerializeAcrossSymbolsInOneAccountScope(t *testing.T) {
	st, err := NewSQLStorage(t.TempDir() + "/account-withdraw-reserve.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	since := time.Now().UTC().Add(-time.Hour)
	manual := &ProfitWithdrawRecord{ID: "manual-btc", AccountID: "acct", AccountScope: "scope-a", ExchangeID: "binance", StrategyID: "BTCUSDT",
		Amount: 40, NetAmount: 40, Currency: "USDT", Type: "manual", Status: "processing", Destination: "account", CreatedAt: time.Now().UTC()}
	if err := st.ReserveManualWithdrawRecord(manual, since, "", 50); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertProfitWithdrawRule("acct", &ProfitWithdrawRule{ID: "eth-rule", AccountScope: "scope-a", ExchangeID: "binance", StrategyID: "ETHUSDT",
		Enabled: true, WithdrawRatio: 0.5, Frequency: "immediate", Destination: "account"}); err != nil {
		t.Fatal(err)
	}
	if claimed, err := st.ClaimProfitWithdrawRule("eth-rule", "eth-claim"); err != nil || !claimed {
		t.Fatalf("claim separate-symbol auto rule: claimed=%v err=%v", claimed, err)
	}
	auto := &ProfitWithdrawRecord{ID: "auto-eth", RuleID: "eth-rule", AccountID: "acct", AccountScope: "scope-a", ClaimID: "eth-claim",
		ExchangeID: "binance", StrategyID: "ETHUSDT", Amount: 5, NetAmount: 5, Currency: "USDT", Type: "auto", Status: "processing",
		Destination: "account", CreatedAt: time.Now().UTC()}
	if err := st.SaveWithdrawRecordForClaim(auto, since, 50); err == nil {
		t.Fatal("automatic reservation for another symbol must wait for the unresolved account transfer")
	}
	manualOtherSymbol := *manual
	manualOtherSymbol.ID = "manual-eth"
	manualOtherSymbol.StrategyID = "ETHUSDT"
	if err := st.ReserveManualWithdrawRecord(&manualOtherSymbol, since, "", 50); err == nil {
		t.Fatal("manual reservation for another symbol must wait for the unresolved account transfer")
	}
}

func TestManualReservationBlocksUnknownHistoricalStatusInAccountScope(t *testing.T) {
	st, err := NewSQLStorage(t.TempDir() + "/unknown-withdraw-status.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.SaveWithdrawRecord(&ProfitWithdrawRecord{ID: "unknown-old-transfer", AccountID: "acct", AccountScope: "scope-a",
		ExchangeID: "binance", StrategyID: "BTCUSDT", Amount: 20, NetAmount: 20, Currency: "USDT", Type: "manual",
		Status: "exchange_unknown", Destination: "account", CreatedAt: time.Now().UTC().Add(-24 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	manual := &ProfitWithdrawRecord{ID: "new-transfer", AccountID: "acct", AccountScope: "scope-a", ExchangeID: "binance", StrategyID: "ETHUSDT",
		Amount: 5, NetAmount: 5, Currency: "USDT", Type: "manual", Status: "processing", Destination: "account", CreatedAt: time.Now().UTC()}
	if err := st.ReserveManualWithdrawRecord(manual, time.Now().UTC().Add(-time.Hour), "", 10); err == nil {
		t.Fatal("unknown non-terminal historical status must lock the account withdrawal scope")
	}
}

func TestWithdrawalFencesSpanAccountPartitionsForSameCredentialScope(t *testing.T) {
	st, err := NewSQLStorage(t.TempDir() + "/cross-account-scope-withdraw.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.UpsertProfitWithdrawRule("old-account", &ProfitWithdrawRule{
		ID: "old-account-rule", AccountScope: "credential-scope", ExchangeID: "binance", StrategyID: "BTCUSDT",
		Enabled: true, WithdrawRatio: 0.5, Frequency: "immediate", Destination: "account",
	}); err != nil {
		t.Fatal(err)
	}
	manual := &ProfitWithdrawRecord{ID: "new-account-manual", AccountID: "new-account", AccountScope: "credential-scope",
		ExchangeID: "binance", StrategyID: "BTCUSDT", Amount: 5, NetAmount: 5, Currency: "USDT", Type: "manual",
		Status: "processing", Destination: "account", CreatedAt: time.Now().UTC()}
	if err := st.ReserveManualWithdrawRecord(manual, time.Now().UTC().Add(-time.Hour), "", 10); err == nil {
		t.Fatal("manual reservation in a second account partition must see the enabled scoped rule")
	}
	if err := st.UpsertProfitWithdrawRule("new-account", &ProfitWithdrawRule{
		ID: "new-account-rule", AccountScope: "credential-scope", ExchangeID: "binance", StrategyID: "BTCUSDT",
		Enabled: true, WithdrawRatio: 0.5, Frequency: "immediate", Destination: "account",
	}); err == nil {
		t.Fatal("active automatic rules in separate account partitions must share one scoped stream")
	}
	if err := st.ReplaceProfitWithdrawRules("new-account", []*ProfitWithdrawRule{{
		ID: "new-account-replaced-rule", AccountScope: "credential-scope", ExchangeID: "binance", StrategyID: "BTCUSDT",
		Enabled: true, WithdrawRatio: 0.5, Frequency: "immediate", Destination: "account",
	}}); err == nil {
		t.Fatal("batch replacement must detect active automatic rules in other account partitions")
	}

	if err := st.DeleteProfitWithdrawRule("old-account", "old-account-rule"); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveWithdrawRecord(&ProfitWithdrawRecord{ID: "old-account-pending", AccountID: "old-account", AccountScope: "credential-scope",
		ExchangeID: "binance", StrategyID: "ETHUSDT", Amount: 5, NetAmount: 5, Currency: "USDT", Type: "manual",
		Status: "processing", Destination: "account", CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	manual.ID = "new-account-manual-after-pending"
	if err := st.ReserveManualWithdrawRecord(manual, time.Now().UTC().Add(-time.Hour), "", 10); err == nil {
		t.Fatal("unresolved transfer in another account partition must block a new scoped transfer")
	}
}

func TestAutomaticReservationBlocksUnknownHistoricalStatusInAccountScope(t *testing.T) {
	st, err := NewSQLStorage(t.TempDir() + "/auto-unknown-withdraw-status.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	if err := st.SaveWithdrawRecord(&ProfitWithdrawRecord{ID: "unknown-old-transfer", AccountID: "acct", AccountScope: "scope-a",
		ExchangeID: "binance", StrategyID: "BTCUSDT", Amount: 20, NetAmount: 20, Currency: "USDT", Type: "manual",
		Status: "exchange_unknown", Destination: "account", CreatedAt: time.Now().UTC().Add(-24 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := st.UpsertProfitWithdrawRule("acct", &ProfitWithdrawRule{ID: "auto-rule", AccountScope: "scope-a", ExchangeID: "binance",
		StrategyID: "ETHUSDT", Enabled: true, TriggerAmount: 10, WithdrawRatio: 0.5, Frequency: "immediate", Destination: "account"}); err != nil {
		t.Fatal(err)
	}
	if claimed, err := st.ClaimProfitWithdrawRule("auto-rule", "claim-auto"); err != nil || !claimed {
		t.Fatalf("claim=(%v,%v)", claimed, err)
	}
	auto := &ProfitWithdrawRecord{ID: "auto-transfer", RuleID: "auto-rule", ClaimID: "claim-auto", AccountID: "acct", AccountScope: "scope-a",
		ExchangeID: "binance", StrategyID: "ETHUSDT", Amount: 5, NetAmount: 5, Currency: "USDT", Type: "auto", Status: "processing",
		Destination: "account", CreatedAt: time.Now().UTC()}
	if err := st.SaveWithdrawRecordForClaim(auto, time.Now().UTC().Add(-time.Hour), 10); err == nil {
		t.Fatal("unknown non-terminal historical status must lock automatic withdrawal reservation")
	}
}

func TestUpdateWithdrawRecordStatusOnlyAllowsProcessingTransitions(t *testing.T) {
	st, err := NewSQLStorage(t.TempDir() + "/withdraw-status-transition.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	newProcessing := func(id string) {
		t.Helper()
		if err := st.SaveWithdrawRecord(&ProfitWithdrawRecord{ID: id, AccountID: "acct", AccountScope: "scope-a", ExchangeID: "binance",
			StrategyID: "BTCUSDT", Amount: 5, NetAmount: 5, Currency: "USDT", Type: "manual", Status: "processing", Destination: "account",
			CreatedAt: time.Now().UTC()}); err != nil {
			t.Fatal(err)
		}
	}
	newProcessing("pending-record")
	if err := st.UpdateWithdrawRecordStatus("pending-record", "pending", "", "unknown transfer outcome"); err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateWithdrawRecordStatus("pending-record", "completed", "late-id", ""); err == nil {
		t.Fatal("generic status update must not resolve a pending transfer")
	}
	newProcessing("completed-record")
	if err := st.UpdateWithdrawRecordStatus("completed-record", "completed", "", ""); err == nil {
		t.Fatal("completed withdrawal requires a verifiable transfer ID")
	}
	if err := st.UpdateWithdrawRecordStatus("completed-record", "completed", "venue-transfer-1", ""); err != nil {
		t.Fatal(err)
	}
	if err := st.UpdateWithdrawRecordStatus("completed-record", "failed", "", "overwrite"); err == nil {
		t.Fatal("generic status update must not overwrite a terminal transfer")
	}
	if err := st.UpdateWithdrawRecordStatus("missing-record", "failed", "", "no-op must not look successful"); err == nil {
		t.Fatal("updating an absent withdrawal record must return an error")
	}
	if err := st.UpdateWithdrawRecordStatus("pending-record", "cancelled", "", ""); err == nil {
		t.Fatal("unsupported status transition must be rejected")
	}
	got, err := st.GetWithdrawRecord("acct", "pending-record")
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != "pending" {
		t.Fatalf("pending reconciliation lock was overwritten: status=%s", got.Status)
	}
}

func TestReserveManualWithdrawRecordRejectsUnsafeCases(t *testing.T) {
	st, err := NewSQLStorage(t.TempDir() + "/manual-withdraw-reject.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	since := time.Now().UTC().Add(-time.Hour)
	newRecord := func(id string) *ProfitWithdrawRecord {
		return &ProfitWithdrawRecord{ID: id, AccountID: "acct", AccountScope: "scope-a", ExchangeID: "binance", StrategyID: "BTCUSDT",
			Amount: 25, NetAmount: 25, Currency: "USDT", Type: "manual", Status: "processing", Destination: "account", CreatedAt: time.Now().UTC()}
	}
	if err := st.ReserveManualWithdrawRecord(newRecord("over-profit"), since, "", 10); err == nil {
		t.Fatal("reservation above verified profit must fail")
	}
	if err := st.UpsertProfitWithdrawRule("acct", &ProfitWithdrawRule{ID: "auto-rule", AccountScope: "scope-a", ExchangeID: "binance", StrategyID: "BTCUSDT",
		Enabled: true, WithdrawRatio: 0.5, Frequency: "immediate", Destination: "account"}); err != nil {
		t.Fatal(err)
	}
	if err := st.ReserveManualWithdrawRecord(newRecord("auto-conflict"), since, "", 50); err == nil {
		t.Fatal("manual reservation must not race an enabled automatic rule")
	}
	if records, err := st.GetWithdrawRecords("acct", 10); err != nil || len(records) != 0 {
		t.Fatalf("unsafe reservation persisted records=%+v err=%v", records, err)
	}
}

func TestReplaceProfitWithdrawRulesRejectsManualWithdrawalInFlight(t *testing.T) {
	for _, status := range []string{"pending", "processing"} {
		t.Run(status, func(t *testing.T) {
			st, err := NewSQLStorage(t.TempDir() + "/replace-manual-withdraw-" + status + ".db")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = st.Close() })
			original := &ProfitWithdrawRule{ID: "existing-rule", AccountScope: "scope-a", ExchangeID: "binance", StrategyID: "BTCUSDT",
				Enabled: false, WithdrawRatio: 0.5, Frequency: "immediate", Destination: "account"}
			if err := st.UpsertProfitWithdrawRule("acct", original); err != nil {
				t.Fatal(err)
			}
			if err := st.SaveWithdrawRecord(&ProfitWithdrawRecord{ID: "manual-" + status, AccountID: "acct", AccountScope: "scope-a",
				ExchangeID: "BINANCE", StrategyID: "btcusdt", Amount: 20, Currency: "USDT", Type: "manual", Status: status,
				Destination: "account", CreatedAt: time.Now().UTC()}); err != nil {
				t.Fatal(err)
			}
			requested := &ProfitWithdrawRule{ID: "replacement-rule", AccountScope: "scope-a", ExchangeID: "binance", StrategyID: "BTCUSDT",
				Enabled: true, WithdrawRatio: 0.5, Frequency: "immediate", Destination: "account"}
			if err := st.ReplaceProfitWithdrawRules("acct", []*ProfitWithdrawRule{requested}); err == nil {
				t.Fatal("replacement must not enable automatic withdrawal while a manual transfer is unresolved")
			}
			rules, err := st.ListProfitWithdrawRules("acct")
			if err != nil {
				t.Fatal(err)
			}
			if len(rules) != 1 || rules[0].ID != original.ID || rules[0].Enabled {
				t.Fatalf("rejected replacement must preserve original rule: %+v", rules)
			}
		})
	}
}

func TestSumReservedWithdrawAmountForStreamIncludesManualAndAutomatic(t *testing.T) {
	st, err := NewSQLStorage(t.TempDir() + "/manual-withdraw-stream.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = st.Close() })
	since := time.Now().UTC().Add(-time.Hour)
	for _, record := range []*ProfitWithdrawRecord{
		{ID: "auto-reserved", RuleID: "rule-a", AccountID: "acct", AccountScope: "scope-a", ExchangeID: "binance", StrategyID: "BTCUSDT", Amount: 12, Currency: "USDT", Type: "auto", Status: "completed", Destination: "account", CreatedAt: time.Now().UTC()},
		{ID: "manual-reserved", AccountID: "acct", AccountScope: "scope-a", ExchangeID: "binance", StrategyID: "BTCUSDT", Amount: 8, Currency: "USDT", Type: "manual", Status: "pending", Destination: "account", CreatedAt: time.Now().UTC()},
		{ID: "manual-at-checkpoint", AccountID: "acct", AccountScope: "scope-a", ExchangeID: "binance", StrategyID: "BTCUSDT", Amount: 5, Currency: "USDT", Type: "manual", Status: "completed", Destination: "account", CreatedAt: since},
		{ID: "auto-at-checkpoint", RuleID: "rule-a", AccountID: "acct", AccountScope: "scope-a", ExchangeID: "binance", StrategyID: "BTCUSDT", Amount: 3, Currency: "USDT", Type: "auto", Status: "completed", Destination: "account", CreatedAt: since},
		{ID: "other-scope", AccountID: "acct", AccountScope: "scope-b", ExchangeID: "binance", StrategyID: "BTCUSDT", Amount: 200, Currency: "USDT", Type: "manual", Status: "completed", Destination: "account", CreatedAt: time.Now().UTC()},
	} {
		if err := st.SaveWithdrawRecord(record); err != nil {
			t.Fatal(err)
		}
	}
	got, err := st.SumReservedWithdrawAmountForStream("acct", "scope-a", "binance", "BTCUSDT", since)
	if err != nil {
		t.Fatal(err)
	}
	if got != 25 {
		t.Fatalf("stream reservations=%v, want 25 (including same-checkpoint manual withdrawal, excluding prior auto withdrawal)", got)
	}
}
