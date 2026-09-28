package profit

import (
	"context"
	"errors"
	"math"
	"strings"
	"sync"
	"testing"
	"time"

	"quantmesh/exchange"
	"quantmesh/exchange/accounting"
	"quantmesh/storage"
)

func TestWithdrawExecutorShouldExecute(t *testing.T) {
	executor := NewWithdrawExecutor(context.Background(), nil, nil)
	now := time.Now()
	yesterday := now.AddDate(0, 0, -1)
	lastWeek := now.AddDate(0, 0, -8)

	tests := []struct {
		name      string
		frequency string
		last      *time.Time
		want      bool
	}{
		{name: "immediate always executes", frequency: "immediate", last: &now, want: true},
		{name: "daily first run", frequency: "daily", last: nil, want: true},
		{name: "daily same day skipped", frequency: "daily", last: &now, want: false},
		{name: "daily previous day executes", frequency: "daily", last: &yesterday, want: true},
		{name: "weekly first run", frequency: "weekly", last: nil, want: true},
		{name: "weekly same week skipped", frequency: "weekly", last: &now, want: false},
		{name: "weekly previous week executes", frequency: "weekly", last: &lastWeek, want: true},
		{name: "unknown frequency skipped", frequency: "monthly", last: nil, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := &storage.ProfitWithdrawRule{LastTriggeredAt: tt.last}
			if got := executor.shouldExecute(rule, tt.frequency); got != tt.want {
				t.Fatalf("shouldExecute(%q) = %v, want %v", tt.frequency, got, tt.want)
			}
		})
	}
}

func TestCurrentPeriodStartAndNextSchedule(t *testing.T) {
	loc := time.FixedZone("UTC+8", 8*3600)
	at := func(y int, m time.Month, d, h, min int) time.Time { return time.Date(y, m, d, h, min, 0, 0, loc) }
	// 2026-09-14 為週一
	tests := []struct {
		name      string
		frequency string
		now       time.Time
		wantStart time.Time
		wantNext  time.Time
	}{
		{name: "daily 目標時刻前屬於前一週期", frequency: "daily", now: at(2026, 9, 17, 1, 59), wantStart: at(2026, 9, 16, 2, 0), wantNext: at(2026, 9, 17, 2, 0)},
		{name: "daily 恰好目標時刻", frequency: "daily", now: at(2026, 9, 17, 2, 0), wantStart: at(2026, 9, 17, 2, 0), wantNext: at(2026, 9, 18, 2, 0)},
		{name: "daily 啟動分鐘 >= 15 仍能排到下一次", frequency: "daily", now: at(2026, 9, 17, 2, 37), wantStart: at(2026, 9, 17, 2, 0), wantNext: at(2026, 9, 18, 2, 0)},
		{name: "weekly 週四", frequency: "weekly", now: at(2026, 9, 17, 10, 0), wantStart: at(2026, 9, 14, 2, 0), wantNext: at(2026, 9, 21, 2, 0)},
		{name: "weekly 週一目標前", frequency: "weekly", now: at(2026, 9, 14, 1, 0), wantStart: at(2026, 9, 7, 2, 0), wantNext: at(2026, 9, 14, 2, 0)},
		{name: "weekly 週日", frequency: "weekly", now: at(2026, 9, 20, 23, 0), wantStart: at(2026, 9, 14, 2, 0), wantNext: at(2026, 9, 21, 2, 0)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := currentPeriodStart(tt.frequency, tt.now); !got.Equal(tt.wantStart) {
				t.Fatalf("currentPeriodStart = %v, want %v", got, tt.wantStart)
			}
			if got := nextScheduleTime(tt.frequency, tt.now); !got.Equal(tt.wantNext) {
				t.Fatalf("nextScheduleTime = %v, want %v", got, tt.wantNext)
			}
		})
	}
}

func TestNextScheduledRetryWaitRetriesBeforeNextPeriod(t *testing.T) {
	loc := time.FixedZone("UTC+8", 8*3600)
	tests := []struct {
		name string
		now  time.Time
		want time.Duration
	}{
		{name: "daily overdue gets recovery retry", now: time.Date(2026, 9, 17, 3, 0, 0, 0, loc), want: immediateInterval},
		{name: "near target preserves target time", now: time.Date(2026, 9, 17, 1, 59, 0, 0, loc), want: time.Minute},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := nextScheduledRetryWait(frequencyDaily, tt.now); got != tt.want {
				t.Fatalf("retry wait=%s, want %s", got, tt.want)
			}
		})
	}
}

func TestShouldExecuteAtDedupesByPeriod(t *testing.T) {
	loc := time.FixedZone("UTC+8", 8*3600)
	at := func(d, h, min int) *time.Time { v := time.Date(2026, 9, d, h, min, 0, 0, loc); return &v }
	tests := []struct {
		name      string
		frequency string
		last      *time.Time
		now       time.Time
		want      bool
	}{
		{name: "daily 本週期已執行", frequency: "daily", last: at(17, 2, 0), now: *at(17, 3, 0), want: false},
		{name: "daily 昨天執行、今天目標已過", frequency: "daily", last: at(16, 2, 1), now: *at(17, 2, 1), want: true},
		{name: "daily 昨天執行、今天目標未到", frequency: "daily", last: at(16, 2, 1), now: *at(17, 1, 0), want: false},
		{name: "weekly 本週一已執行", frequency: "weekly", last: at(14, 2, 0), now: *at(17, 2, 0), want: false},
		{name: "weekly 上週執行", frequency: "weekly", last: at(8, 2, 0), now: *at(14, 2, 5), want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rule := &storage.ProfitWithdrawRule{LastTriggeredAt: tt.last}
			if got := shouldExecuteAt(rule, tt.frequency, tt.now); got != tt.want {
				t.Fatalf("shouldExecuteAt = %v, want %v", got, tt.want)
			}
		})
	}
}

type pnlEvent struct {
	at  time.Time
	pnl float64
}

// fakeWithdrawStorage 記憶體版存儲，僅實現利潤提取所需方法
type fakeWithdrawStorage struct {
	storage.Storage
	rule          *storage.ProfitWithdrawRule
	rules         []*storage.ProfitWithdrawRule
	events        []pnlEvent
	records       []*storage.ProfitWithdrawRecord
	coverageFrom  time.Time
	coverageUntil time.Time
	failLastTrig  bool
	transferCalls int
	claimID       string
}

func (f *fakeWithdrawStorage) GetFundingIncomeCoverage(string, string, string, string) (time.Time, time.Time, error) {
	return f.coverageFrom, f.coverageUntil, nil
}

func (f *fakeWithdrawStorage) ListAccountIDsWithProfitRules() ([]string, error) {
	return []string{f.rule.AccountID}, nil
}
func (f *fakeWithdrawStorage) ListProfitWithdrawRules(accountID string) ([]*storage.ProfitWithdrawRule, error) {
	if f.rules != nil {
		out := make([]*storage.ProfitWithdrawRule, 0, len(f.rules))
		for _, rule := range f.rules {
			if rule == nil {
				out = append(out, nil)
				continue
			}
			cp := *rule
			out = append(out, &cp)
		}
		return out, nil
	}
	cp := *f.rule
	return []*storage.ProfitWithdrawRule{&cp}, nil
}
func (f *fakeWithdrawStorage) GetPnLByTimeRange(account string, start, end time.Time) ([]*storage.PnLBySymbol, error) {
	var total float64
	for _, ev := range f.events {
		if ev.at.After(start) && !ev.at.After(end) {
			total += ev.pnl
		}
	}
	return []*storage.PnLBySymbol{{Symbol: "BTCUSDT", TotalPnL: total}}, nil
}
func (f *fakeWithdrawStorage) GetRealizedPnLForWithdrawal(_, _, _ string, start, end time.Time) (float64, error) {
	var total float64
	for _, ev := range f.events {
		if ev.at.After(start) && !ev.at.After(end) {
			total += ev.pnl
		}
	}
	return total, nil
}
func (f *fakeWithdrawStorage) GetWithdrawRecords(accountID string, limit int) ([]*storage.ProfitWithdrawRecord, error) {
	return f.records, nil
}
func (f *fakeWithdrawStorage) SumReservedWithdrawAmountForStream(accountID, accountScope, exchange, symbol string, since time.Time) (float64, error) {
	var total float64
	for _, r := range f.records {
		if r != nil && r.AccountID == accountID && r.AccountScope == accountScope && strings.EqualFold(r.ExchangeID, exchange) &&
			strings.EqualFold(r.StrategyID, symbol) && r.CreatedAt.After(since) && r.Status != "failed" && r.Status != "cancelled" {
			total += r.Amount
		}
	}
	return total, nil
}
func (f *fakeWithdrawStorage) SaveWithdrawRecord(r *storage.ProfitWithdrawRecord) error {
	f.records = append(f.records, r)
	return nil
}
func (f *fakeWithdrawStorage) SaveWithdrawRecordForClaim(r *storage.ProfitWithdrawRecord) error {
	if r == nil || r.ClaimID == "" || f.claimID != r.ClaimID {
		return errors.New("withdrawal claim was lost")
	}
	f.records = append(f.records, r)
	return nil
}
func (f *fakeWithdrawStorage) RecoverAbandonedProfitWithdrawRuleClaims(time.Time) (int64, error) {
	return 0, nil
}
func (f *fakeWithdrawStorage) UpdateWithdrawRecordStatus(id, status, transferID, failedReason string) error {
	for _, r := range f.records {
		if r.ID == id {
			r.Status = status
			r.TransferID = transferID
			r.FailedReason = failedReason
		}
	}
	return nil
}
func (f *fakeWithdrawStorage) UpdateRuleLastTriggeredAt(ruleID string, at time.Time) error {
	if f.failLastTrig {
		return errors.New("db down")
	}
	f.rule.LastTriggeredAt = &at
	return nil
}
func (f *fakeWithdrawStorage) ClaimProfitWithdrawRule(ruleID, claimID string) (bool, error) {
	if f.claimID != "" {
		return false, nil
	}
	f.claimID = claimID
	return true, nil
}
func (f *fakeWithdrawStorage) ReleaseProfitWithdrawRuleClaim(ruleID, claimID string) error {
	if f.claimID != claimID {
		return errors.New("claim identity mismatch")
	}
	f.claimID = ""
	return nil
}

type fakeTransferExchange struct {
	exchange.IExchange
	st              *fakeWithdrawStorage
	amounts         []float64
	err             error
	emptyTransferID bool
	account         *exchange.Account
	accountErr      error
	ledgerEntries   []accounting.Entry
	ledgerErr       error
}

func (f *fakeTransferExchange) GetAccountFresh(context.Context) (*exchange.Account, error) {
	return f.account, f.accountErr
}

func (f *fakeTransferExchange) ReadAccountEvidence(_ context.Context, since time.Time) (accounting.Snapshot, error) {
	if f.ledgerErr != nil {
		return accounting.Snapshot{}, f.ledgerErr
	}
	now := time.Now().UTC()
	return accounting.Snapshot{Currency: "USDT", ObservedAt: now,
		Wallet: accounting.Wallet{Balance: "1000", From: since, Through: now, ObservedAt: now}, Entries: f.ledgerEntries}, nil
}

func (f *fakeTransferExchange) InternalTransfer(ctx context.Context, from, to, asset string, amount float64) (string, error) {
	f.amounts = append(f.amounts, amount)
	if f.err != nil {
		return "", f.err
	}
	if f.emptyTransferID {
		return "", nil
	}
	return "tx", nil
}

func TestAutomaticWithdrawRejectsUnallocatedAccountExpenses(t *testing.T) {
	base := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	for _, kind := range []string{"insurance_clear", "unallocated_fee", "interest"} {
		t.Run(kind, func(t *testing.T) {
			st := &fakeWithdrawStorage{rule: &storage.ProfitWithdrawRule{
				ID: "cashflow-rule", AccountID: "cashflow-account", AccountScope: "scope-a", ExchangeID: "binance",
				Enabled: true, TriggerAmount: 10, WithdrawRatio: 0.5, Frequency: frequencyImmediate, CreatedAt: base.Add(-time.Hour),
			}, coverageFrom: base.Add(-24 * time.Hour), coverageUntil: base.Add(24 * time.Hour),
				events: []pnlEvent{{at: base.Add(time.Minute), pnl: 100}}}
			ex := &fakeTransferExchange{st: st, account: &exchange.Account{BalanceAsset: "USDT", AvailableBalance: 1000, MaxWithdrawAmount: 1000},
				ledgerEntries: []accounting.Entry{{ID: "unallocated-1", Kind: kind, Currency: "USDT", Amount: "-1", At: base.Add(time.Minute)}}}
			e := NewWithdrawExecutor(context.Background(), st, func(string) exchange.IExchange { return ex })
			e.now = func() time.Time { return base.Add(2 * time.Minute) }
			if err := e.processRule(st.rule); err == nil {
				t.Fatal("unallocated account expense must disable automatic withdrawal")
			}
			if len(ex.amounts) != 0 || len(st.records) != 0 {
				t.Fatalf("unallocated account expense must be rejected before transfer: amounts=%v records=%+v", ex.amounts, st.records)
			}
		})
	}
}

func TestAutomaticWithdrawRejectsUnknownAccountCashFlow(t *testing.T) {
	base := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	st := &fakeWithdrawStorage{rule: &storage.ProfitWithdrawRule{
		ID: "unknown-flow-rule", AccountID: "unknown-flow-account", AccountScope: "scope-a", ExchangeID: "binance",
		Enabled: true, TriggerAmount: 10, WithdrawRatio: 0.5, Frequency: frequencyImmediate, CreatedAt: base.Add(-time.Hour),
	}, coverageFrom: base.Add(-24 * time.Hour), coverageUntil: base.Add(24 * time.Hour),
		events: []pnlEvent{{at: base.Add(time.Minute), pnl: 100}}}
	ex := &fakeTransferExchange{st: st, account: &exchange.Account{BalanceAsset: "USDT", AvailableBalance: 1000, MaxWithdrawAmount: 1000},
		ledgerEntries: []accounting.Entry{{ID: "future-income-kind", Kind: "new_exchange_adjustment", Currency: "USDT", Amount: "-1", At: base.Add(time.Minute)}}}
	e := NewWithdrawExecutor(context.Background(), st, func(string) exchange.IExchange { return ex })
	e.now = func() time.Time { return base.Add(2 * time.Minute) }
	if err := e.processRule(st.rule); err == nil {
		t.Fatal("unknown account cash-flow kinds must disable automatic withdrawal")
	}
	if len(ex.amounts) != 0 || len(st.records) != 0 {
		t.Fatalf("unknown account cash flow must be rejected before transfer: amounts=%v records=%+v", ex.amounts, st.records)
	}
}

func TestAutomaticWithdrawRequiresFreshSufficientUSDTBalance(t *testing.T) {
	base := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	for _, test := range []struct {
		name       string
		account    *exchange.Account
		accountErr error
		wantErr    bool
	}{
		{name: "sufficient USDT", account: &exchange.Account{BalanceAsset: "USDT", AvailableBalance: 100, MaxWithdrawAmount: 100}},
		{name: "insufficient free margin", account: &exchange.Account{BalanceAsset: "USDT", AvailableBalance: 49, MaxWithdrawAmount: 49}, wantErr: true},
		{name: "exchange transfer cap", account: &exchange.Account{BalanceAsset: "USDT", AvailableBalance: 100, MaxWithdrawAmount: 5}, wantErr: true},
		{name: "unverified denomination", account: &exchange.Account{BalanceAsset: "BTC", AvailableBalance: 100, MaxWithdrawAmount: 100}, wantErr: true},
		{name: "missing account", account: nil, wantErr: true},
		{name: "account query error", accountErr: errors.New("offline"), wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			st := &fakeWithdrawStorage{rule: &storage.ProfitWithdrawRule{
				ID: "balance-rule", AccountID: "balance-account", AccountScope: "scope-a", ExchangeID: "binance", Enabled: true,
				TriggerAmount: 10, WithdrawRatio: 0.5, Frequency: frequencyImmediate, CreatedAt: base.Add(-time.Hour),
			}, coverageFrom: base.Add(-24 * time.Hour), coverageUntil: base.Add(24 * time.Hour)}
			st.events = append(st.events, pnlEvent{at: base.Add(time.Minute), pnl: 100})
			ex := &fakeTransferExchange{st: st, account: test.account, accountErr: test.accountErr}
			e := NewWithdrawExecutor(context.Background(), st, func(string) exchange.IExchange { return ex })
			e.now = func() time.Time { return base.Add(2 * time.Minute) }
			err := e.processRule(st.rule)
			if (err != nil) != test.wantErr {
				t.Fatalf("processRule err=%v, wantErr=%v", err, test.wantErr)
			}
			if test.wantErr && (len(ex.amounts) != 0 || len(st.records) != 0) {
				t.Fatalf("unsafe balance must be rejected before reserving or transferring: amounts=%v records=%+v", ex.amounts, st.records)
			}
			if !test.wantErr && (len(ex.amounts) != 1 || ex.amounts[0] != 50) {
				t.Fatalf("sufficient balance should permit the calculated transfer, got %v", ex.amounts)
			}
		})
	}
}

func TestAutomaticWithdrawFailsClosedForOverlappingEnabledRules(t *testing.T) {
	base := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	newRule := func(id, frequency string) *storage.ProfitWithdrawRule {
		return &storage.ProfitWithdrawRule{ID: id, AccountID: "duplicate-account", AccountScope: "scope-a", ExchangeID: "binance",
			StrategyID: "BTCUSDT", Enabled: true, TriggerAmount: 10, WithdrawRatio: 0.5,
			Frequency: frequency, CreatedAt: base.Add(-time.Hour)}
	}
	st := &fakeWithdrawStorage{
		rule:         newRule("immediate-rule", frequencyImmediate),
		rules:        []*storage.ProfitWithdrawRule{newRule("immediate-rule", frequencyImmediate), newRule("daily-rule", frequencyDaily)},
		coverageFrom: base.Add(-24 * time.Hour), coverageUntil: base.Add(24 * time.Hour),
		events: []pnlEvent{{at: base.Add(time.Minute), pnl: 100}},
	}
	ex := &fakeTransferExchange{st: st, account: &exchange.Account{BalanceAsset: "USDT", AvailableBalance: 1000, MaxWithdrawAmount: 1000}}
	e := NewWithdrawExecutor(context.Background(), st, func(string) exchange.IExchange { return ex })
	e.now = func() time.Time { return base.Add(2 * time.Minute) }
	e.processRules(frequencyImmediate)
	if len(ex.amounts) != 0 || len(st.records) != 0 {
		t.Fatalf("overlapping rules must not transfer the same accounting stream twice: amounts=%v records=%+v", ex.amounts, st.records)
	}
}

func TestAutomaticWithdrawAmbiguousTransferErrorIsNeverRetried(t *testing.T) {
	base := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	st := &fakeWithdrawStorage{rule: &storage.ProfitWithdrawRule{
		ID: "r1", AccountID: "acc", AccountScope: "scope-a", ExchangeID: "binance", Enabled: true,
		TriggerAmount: 10, WithdrawRatio: 0.5, Frequency: frequencyImmediate, CreatedAt: base.Add(-time.Hour),
	}, coverageFrom: base.Add(-24 * time.Hour), coverageUntil: base.Add(24 * time.Hour)}
	st.events = append(st.events, pnlEvent{at: base.Add(time.Minute), pnl: 100})
	ex := &fakeTransferExchange{st: st, err: errors.New("request timeout"), account: &exchange.Account{BalanceAsset: "USDT", AvailableBalance: 1000, MaxWithdrawAmount: 1000}}
	e := NewWithdrawExecutor(context.Background(), st, func(string) exchange.IExchange { return ex })
	e.now = func() time.Time { return base.Add(2 * time.Minute) }

	if err := e.processRule(st.rule); err == nil {
		t.Fatal("ambiguous transfer error must be surfaced")
	}
	if len(st.records) != 1 || st.records[0].Status != "pending" || st.records[0].FailedReason == "" {
		t.Fatalf("transfer outcome must remain reserved for reconciliation: %+v", st.records)
	}
	if st.claimID == "" {
		t.Fatal("unknown transfer outcome must retain the durable rule claim")
	}
	if err := e.processRule(st.rule); err != nil {
		t.Fatalf("pending record should suppress automatic retry without failing processing: %v", err)
	}
	if len(ex.amounts) != 1 {
		t.Fatalf("ambiguous transfer was retried %d times", len(ex.amounts))
	}
}

func TestAutomaticWithdrawEmptyTransferIDRemainsUnresolved(t *testing.T) {
	base := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	st := &fakeWithdrawStorage{rule: &storage.ProfitWithdrawRule{
		ID: "empty-transfer-id-rule", AccountID: "empty-transfer-id-account", AccountScope: "scope-a", ExchangeID: "binance", Enabled: true,
		TriggerAmount: 10, WithdrawRatio: 0.5, Frequency: frequencyImmediate, CreatedAt: base.Add(-time.Hour),
	}, coverageFrom: base.Add(-24 * time.Hour), coverageUntil: base.Add(24 * time.Hour)}
	st.events = append(st.events, pnlEvent{at: base.Add(time.Minute), pnl: 100})
	ex := &fakeTransferExchange{st: st, emptyTransferID: true,
		account: &exchange.Account{BalanceAsset: "USDT", AvailableBalance: 1000, MaxWithdrawAmount: 1000}}
	e := NewWithdrawExecutor(context.Background(), st, func(string) exchange.IExchange { return ex })
	e.now = func() time.Time { return base.Add(2 * time.Minute) }

	err := e.processRule(st.rule)
	if !errors.Is(err, ErrWithdrawOutcomeUnknown) {
		t.Fatalf("empty transfer ID must remain an unknown outcome, got %v", err)
	}
	if len(ex.amounts) != 1 || len(st.records) != 1 || st.records[0].Status != "pending" || st.records[0].FailedReason == "" {
		t.Fatalf("empty transfer ID must be reserved for reconciliation: calls=%d records=%+v", len(ex.amounts), st.records)
	}
	if st.claimID == "" {
		t.Fatal("unknown transfer result must retain the durable rule claim")
	}
	if st.rule.LastTriggeredAt != nil {
		t.Fatalf("unverified transfer must not advance accounting checkpoint: %v", st.rule.LastTriggeredAt)
	}
}

func TestConcurrentAutomaticWithdrawForSameRuleTransfersOnlyOnce(t *testing.T) {
	base := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	st := &fakeWithdrawStorage{rule: &storage.ProfitWithdrawRule{
		ID: "concurrent-rule", AccountID: "concurrent-account", AccountScope: "scope-a",
		ExchangeID: "binance", Enabled: true, TriggerAmount: 1, CreatedAt: base.Add(-time.Hour),
		WithdrawRatio: 1, Frequency: frequencyImmediate,
	}, coverageFrom: base.Add(-24 * time.Hour), coverageUntil: base.Add(24 * time.Hour)}
	st.events = append(st.events, pnlEvent{at: base.Add(time.Minute), pnl: 100})
	ex := &fakeTransferExchange{st: st, account: &exchange.Account{BalanceAsset: "USDT", AvailableBalance: 1000, MaxWithdrawAmount: 1000}}
	e := NewWithdrawExecutor(context.Background(), st, func(string) exchange.IExchange { return ex })
	e.now = func() time.Time { return base.Add(2 * time.Minute) }

	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := e.processRule(st.rule); err != nil {
				t.Errorf("process rule: %v", err)
			}
		}()
	}
	wg.Wait()
	if len(ex.amounts) != 1 || len(st.records) != 1 {
		t.Fatalf("concurrent rule produced %d transfers and %d reservations; want one each", len(ex.amounts), len(st.records))
	}
	if st.claimID != "" {
		t.Fatalf("confirmed successful transfer should release claim, got %q", st.claimID)
	}
}

func TestAutomaticWithdrawStopsAtLastCompleteFundingCoverage(t *testing.T) {
	base := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	coverageEnd := base.Add(2 * time.Minute)
	st := &fakeWithdrawStorage{
		rule: &storage.ProfitWithdrawRule{
			ID: "coverage-rule", AccountID: "coverage-account", AccountScope: "scope-a", ExchangeID: "binance",
			Enabled: true, TriggerAmount: 10, WithdrawRatio: 0.5, Frequency: frequencyImmediate, CreatedAt: base.Add(-time.Hour),
		},
		coverageFrom: base.Add(-24 * time.Hour), coverageUntil: coverageEnd,
		events: []pnlEvent{
			{at: base.Add(time.Minute), pnl: 100},
			{at: base.Add(3 * time.Minute), pnl: 1000}, // Not yet covered by the funding-history snapshot.
		},
	}
	ex := &fakeTransferExchange{st: st, account: &exchange.Account{BalanceAsset: "USDT", AvailableBalance: 1000, MaxWithdrawAmount: 1000}}
	e := NewWithdrawExecutor(context.Background(), st, func(string) exchange.IExchange { return ex })
	e.now = func() time.Time { return base.Add(10 * time.Minute) }
	if err := e.processRule(st.rule); err != nil {
		t.Fatal(err)
	}
	if len(ex.amounts) != 1 || ex.amounts[0] != 50 {
		t.Fatalf("transfer amounts=%v, want only 50 from covered window", ex.amounts)
	}
	if len(st.records) != 1 || !st.records[0].CreatedAt.Equal(coverageEnd) {
		t.Fatalf("withdrawal record must use verified coverage cutoff %s: %+v", coverageEnd, st.records)
	}
}

// TestImmediateWithdrawNoRepeatedTransfer 多次 tick 只提取新增利潤，不重複劃轉累計利潤
func TestImmediateWithdrawNoRepeatedTransfer(t *testing.T) {
	base := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	type tick struct {
		addPnL     float64 // 本 tick 前新增的已實現利潤
		wantAmount float64 // 0 表示不應劃轉
	}
	tests := []struct {
		name         string
		failLastTrig bool
		ticks        []tick
	}{
		{name: "正常更新 LastTriggeredAt", ticks: []tick{
			{addPnL: 100, wantAmount: 50},
			{addPnL: 0, wantAmount: 0},
			{addPnL: 0, wantAmount: 0},
			{addPnL: 40, wantAmount: 20},
			{addPnL: 5, wantAmount: 0}, // 低於 TriggerAmount
		}},
		{name: "LastTriggeredAt 更新失敗時扣減已提取額", failLastTrig: true, ticks: []tick{
			{addPnL: 100, wantAmount: 50},
			{addPnL: 0, wantAmount: 0},
			{addPnL: 40, wantAmount: 20},
			{addPnL: 0, wantAmount: 0},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			st := &fakeWithdrawStorage{
				rule: &storage.ProfitWithdrawRule{
					ID: "r1", AccountID: "acc", AccountScope: "scope-a", ExchangeID: "binance", Enabled: true,
					TriggerAmount: 10, WithdrawRatio: 0.5, Frequency: "immediate", CreatedAt: base.Add(-time.Hour),
				},
				coverageFrom: base.Add(-24 * time.Hour), coverageUntil: base.Add(24 * time.Hour),
				failLastTrig: tt.failLastTrig,
			}
			ex := &fakeTransferExchange{st: st, account: &exchange.Account{BalanceAsset: "USDT", AvailableBalance: 1000, MaxWithdrawAmount: 1000}}
			e := NewWithdrawExecutor(context.Background(), st, func(string) exchange.IExchange { return ex })
			clock := base
			e.now = func() time.Time { return clock }

			for i, tk := range tt.ticks {
				clock = clock.Add(5 * time.Minute)
				if tk.addPnL != 0 {
					st.events = append(st.events, pnlEvent{at: clock.Add(-time.Minute), pnl: tk.addPnL})
				}
				before := len(ex.amounts)
				e.processRules("immediate")
				got := 0.0
				if len(ex.amounts) > before {
					if len(ex.amounts)-before > 1 {
						t.Fatalf("tick %d: 劃轉 %d 次", i, len(ex.amounts)-before)
					}
					got = ex.amounts[len(ex.amounts)-1]
				}
				if math.Abs(got-tk.wantAmount) > 1e-9 {
					t.Fatalf("tick %d: 劃轉金額 = %v, want %v", i, got, tk.wantAmount)
				}
			}
		})
	}
}
