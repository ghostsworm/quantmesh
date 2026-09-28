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
	if got != 20 {
		t.Fatalf("stream reservations=%v, want 20", got)
	}
}
