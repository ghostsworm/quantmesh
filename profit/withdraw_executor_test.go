package profit

import (
	"context"
	"errors"
	"math"
	"strconv"
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

func TestExecuteWithdrawRejectsMissingExchangeGetter(t *testing.T) {
	e := NewWithdrawExecutor(context.Background(), nil, nil)
	now := time.Now()
	err := e.executeWithdraw(&storage.ProfitWithdrawRule{ID: "rule-a", ExchangeID: "binance"}, "claim-a", 1, now.Add(-time.Minute), now, 1)
	if err == nil || !strings.Contains(err.Error(), "exchange lookup is unavailable") {
		t.Fatalf("executeWithdraw error=%v, want missing exchange lookup error", err)
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
	ids := make([]string, 0, len(f.rules)+1)
	seen := make(map[string]struct{})
	if f.rules == nil {
		return []string{f.rule.AccountID}, nil
	}
	for _, rule := range f.rules {
		if rule == nil {
			continue
		}
		if _, ok := seen[rule.AccountID]; ok {
			continue
		}
		seen[rule.AccountID] = struct{}{}
		ids = append(ids, rule.AccountID)
	}
	return ids, nil
}
func (f *fakeWithdrawStorage) ListProfitWithdrawRules(accountID string) ([]*storage.ProfitWithdrawRule, error) {
	if f.rules != nil {
		out := make([]*storage.ProfitWithdrawRule, 0, len(f.rules))
		for _, rule := range f.rules {
			if rule == nil || rule.AccountID != accountID {
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
			strings.EqualFold(r.StrategyID, symbol) && (r.CreatedAt.After(since) || (r.CreatedAt.Equal(since) && r.Type == "manual")) &&
			r.Status != "failed" && r.Status != "cancelled" {
			total += r.Amount
		}
	}
	return total, nil
}
func (f *fakeWithdrawStorage) SaveWithdrawRecord(r *storage.ProfitWithdrawRecord) error {
	f.records = append(f.records, r)
	return nil
}
func (f *fakeWithdrawStorage) SaveWithdrawRecordForClaim(r *storage.ProfitWithdrawRecord, windowStart time.Time, verifiedBudget float64) error {
	if r == nil || r.ClaimID == "" || f.claimID != r.ClaimID {
		return errors.New("withdrawal claim was lost")
	}
	reserved, err := f.SumReservedWithdrawAmountForStream(r.AccountID, r.AccountScope, r.ExchangeID, r.StrategyID, windowStart)
	if err != nil || reserved+r.Amount > verifiedBudget {
		return errors.New("withdrawal accounting changed during validation")
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
	accountScope    string
	amounts         []float64
	err             error
	emptyTransferID bool
	account         *exchange.Account
	accountErr      error
	ledgerEntries   []accounting.Entry
	ledgerErr       error
}

func (f *fakeTransferExchange) WithdrawalAccountScope() string { return f.accountScope }

func (f *fakeTransferExchange) GetAccountFresh(context.Context) (*exchange.Account, error) {
	return f.account, f.accountErr
}

func (f *fakeTransferExchange) ReadVerifiedWithdrawalEvidence(_ context.Context, since time.Time) (accounting.Snapshot, error) {
	if f.ledgerErr != nil {
		return accounting.Snapshot{}, f.ledgerErr
	}
	now := time.Now().UTC()
	entries := append([]accounting.Entry(nil), f.ledgerEntries...)
	if f.st != nil && f.ledgerEntries == nil {
		for index, event := range f.st.events {
			if !event.at.After(since) || event.at.After(now) {
				continue
			}
			entries = append(entries, accounting.Entry{ID: "fake-pnl-" + strconv.Itoa(index), Kind: "realized_pnl", Currency: "USDT",
				Amount: strconv.FormatFloat(event.pnl, 'f', -1, 64), Symbol: "BTCUSDT", At: event.at})
		}
	}
	return accounting.Snapshot{Currency: "USDT", ObservedAt: now,
		Wallet: accounting.Wallet{Balance: "1000", From: since, Through: now, ObservedAt: now}, Entries: entries}, nil
}

type candidateOnlyTransferExchange struct{ exchange.IExchange }

func (candidateOnlyTransferExchange) WithdrawalAccountScope() string { return "scope-a" }
func (candidateOnlyTransferExchange) ReadAccountEvidence(context.Context, time.Time) (accounting.Snapshot, error) {
	return accounting.Snapshot{Currency: "USDT"}, nil
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

func TestValidateTransferSafetyRequiresSameSymbolAttribution(t *testing.T) {
	now := time.Now().UTC()
	windowStart, windowEnd := now.Add(-10*time.Minute), now.Add(-time.Minute)
	for _, tt := range []struct {
		name         string
		symbol       string
		accountScope string
		wantOK       bool
	}{
		{name: "exact stream", symbol: "BTCUSDT", accountScope: "scope-a", wantOK: true},
		{name: "different symbol", symbol: "ETHUSDT", accountScope: "scope-a"},
		{name: "missing symbol", accountScope: "scope-a"},
		{name: "different credential scope", symbol: "BTCUSDT", accountScope: "scope-b"},
		{name: "unattested credential scope", symbol: "BTCUSDT"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			ex := &fakeTransferExchange{
				accountScope:  tt.accountScope,
				account:       &exchange.Account{BalanceAsset: "USDT", AvailableBalance: 100, MaxWithdrawAmount: 100},
				ledgerEntries: []accounting.Entry{{ID: "income-1", Kind: "realized_pnl", Currency: "USDT", Amount: "10", Symbol: tt.symbol, At: now.Add(-5 * time.Minute)}},
			}
			err := ValidateTransferSafety(context.Background(), ex, "BTCUSDT", "scope-a", 5, windowStart, windowEnd)
			if (err == nil) != tt.wantOK {
				t.Fatalf("ValidateTransferSafety() error=%v, wantOK=%v", err, tt.wantOK)
			}
		})
	}
}

func TestValidateTransferSafetyRejectsCandidateEvidenceWithoutDurableReconciliation(t *testing.T) {
	now := time.Now().UTC()
	err := ValidateTransferSafety(context.Background(), candidateOnlyTransferExchange{}, "BTCUSDT", "scope-a", 1,
		now.Add(-time.Minute), now)
	if err == nil || !strings.Contains(err.Error(), "durably reconciled") {
		t.Fatalf("candidate-only ledger evidence should be rejected: %v", err)
	}
}

func TestValidateTransferSafetyCapsAmountAtFreshExchangeLedgerProfit(t *testing.T) {
	now := time.Now().UTC()
	windowStart, windowEnd := now.Add(-10*time.Minute), now.Add(-time.Minute)
	tests := []struct {
		name    string
		entries []accounting.Entry
		amount  float64
		wantErr bool
	}{
		{name: "amount within fresh net realized profit", entries: []accounting.Entry{
			{ID: "pnl-1", Kind: "realized_pnl", Currency: "USDT", Amount: "10", Symbol: "BTCUSDT", At: now.Add(-5 * time.Minute)},
			{ID: "fee-1", Kind: "fee", Currency: "USDT", Amount: "-2", Symbol: "BTCUSDT", At: now.Add(-4 * time.Minute)},
		}, amount: 8},
		{name: "late fee makes requested amount exceed fresh profit", entries: []accounting.Entry{
			{ID: "pnl-2", Kind: "realized_pnl", Currency: "USDT", Amount: "10", Symbol: "BTCUSDT", At: now.Add(-5 * time.Minute)},
			{ID: "fee-2", Kind: "fee", Currency: "USDT", Amount: "-6", Symbol: "BTCUSDT", At: now.Add(-4 * time.Minute)},
		}, amount: 5, wantErr: true},
		{name: "duplicate transaction identity", entries: []accounting.Entry{
			{ID: "same", Kind: "realized_pnl", Currency: "USDT", Amount: "10", Symbol: "BTCUSDT", At: now.Add(-5 * time.Minute)},
			{ID: "same", Kind: "funding", Currency: "USDT", Amount: "10", Symbol: "BTCUSDT", At: now.Add(-4 * time.Minute)},
		}, amount: 5, wantErr: true},
		{name: "unvalued commission asset", entries: []accounting.Entry{
			{ID: "pnl-3", Kind: "realized_pnl", Currency: "USDT", Amount: "10", Symbol: "BTCUSDT", At: now.Add(-5 * time.Minute)},
			{ID: "fee-3", Kind: "fee", Currency: "BNB", Amount: "-0.01", Symbol: "BTCUSDT", At: now.Add(-4 * time.Minute)},
		}, amount: 5, wantErr: true},
		{name: "positive fee lacks rebate classification", entries: []accounting.Entry{
			{ID: "pnl-4", Kind: "realized_pnl", Currency: "USDT", Amount: "10", Symbol: "BTCUSDT", At: now.Add(-5 * time.Minute)},
			{ID: "fee-4", Kind: "fee", Currency: "USDT", Amount: "1", Symbol: "BTCUSDT", At: now.Add(-4 * time.Minute)},
		}, amount: 5, wantErr: true},
		{name: "entry without timestamp", entries: []accounting.Entry{
			{ID: "pnl-5", Kind: "realized_pnl", Currency: "USDT", Amount: "10", Symbol: "BTCUSDT", At: now.Add(-5 * time.Minute)},
			{ID: "unknown-time-fee", Kind: "fee", Currency: "USDT", Amount: "-9"},
		}, amount: 5, wantErr: true},
		{name: "unreserved outgoing transfer reduces withdrawal budget", entries: []accounting.Entry{
			{ID: "pnl-transfer-out", Kind: "realized_pnl", Currency: "USDT", Amount: "10", Symbol: "BTCUSDT", At: now.Add(-5 * time.Minute)},
			{ID: "transfer-out-1", Kind: "transfer_out", Currency: "USDT", Amount: "-3", At: now.Add(-4 * time.Minute)},
		}, amount: 8, wantErr: true},
		{name: "withdrawal within profit net of outgoing transfer", entries: []accounting.Entry{
			{ID: "pnl-transfer-out-allowed", Kind: "realized_pnl", Currency: "USDT", Amount: "10", Symbol: "BTCUSDT", At: now.Add(-5 * time.Minute)},
			{ID: "transfer-out-allowed", Kind: "transfer_out", Currency: "USDT", Amount: "-3", At: now.Add(-4 * time.Minute)},
		}, amount: 7},
		{name: "incoming transfer is not profit", entries: []accounting.Entry{
			{ID: "pnl-transfer-in", Kind: "realized_pnl", Currency: "USDT", Amount: "10", Symbol: "BTCUSDT", At: now.Add(-5 * time.Minute)},
			{ID: "transfer-in-1", Kind: "transfer_in", Currency: "USDT", Amount: "100", At: now.Add(-4 * time.Minute)},
		}, amount: 11, wantErr: true},
		{name: "outgoing transfer requires a negative amount", entries: []accounting.Entry{
			{ID: "pnl-transfer-sign", Kind: "realized_pnl", Currency: "USDT", Amount: "10", Symbol: "BTCUSDT", At: now.Add(-5 * time.Minute)},
			{ID: "transfer-out-positive", Kind: "transfer_out", Currency: "USDT", Amount: "3", At: now.Add(-4 * time.Minute)},
		}, amount: 5, wantErr: true},
		{name: "incoming transfer requires USDT denomination", entries: []accounting.Entry{
			{ID: "pnl-transfer-asset", Kind: "realized_pnl", Currency: "USDT", Amount: "10", Symbol: "BTCUSDT", At: now.Add(-5 * time.Minute)},
			{ID: "transfer-in-foreign", Kind: "transfer_in", Currency: "BTC", Amount: "1", At: now.Add(-4 * time.Minute)},
		}, amount: 5, wantErr: true},
		{name: "transfer requires stable identity", entries: []accounting.Entry{
			{ID: "pnl-transfer-id", Kind: "realized_pnl", Currency: "USDT", Amount: "10", Symbol: "BTCUSDT", At: now.Add(-5 * time.Minute)},
			{Kind: "transfer_out", Currency: "USDT", Amount: "-1", At: now.Add(-4 * time.Minute)},
		}, amount: 5, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ex := &fakeTransferExchange{accountScope: "scope-a", account: &exchange.Account{BalanceAsset: "USDT", AvailableBalance: 100, MaxWithdrawAmount: 100}, ledgerEntries: tt.entries}
			err := ValidateTransferSafety(context.Background(), ex, "BTCUSDT", "scope-a", tt.amount, windowStart, windowEnd)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ValidateTransferSafety() error=%v, wantErr=%v", err, tt.wantErr)
			}
		})
	}
}

func TestAutomaticWithdrawRejectsUnallocatedAccountExpenses(t *testing.T) {
	base := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	for _, kind := range []string{"insurance_clear", "unallocated_fee", "interest"} {
		t.Run(kind, func(t *testing.T) {
			st := &fakeWithdrawStorage{rule: &storage.ProfitWithdrawRule{
				ID: "cashflow-rule", AccountID: "cashflow-account", AccountScope: "scope-a", ExchangeID: "binance", StrategyID: "BTCUSDT",
				Enabled: true, TriggerAmount: 10, WithdrawRatio: 0.5, Frequency: frequencyImmediate, CreatedAt: base.Add(-time.Hour),
			}, coverageFrom: base.Add(-24 * time.Hour), coverageUntil: base.Add(24 * time.Hour),
				events: []pnlEvent{{at: base.Add(time.Minute), pnl: 100}}}
			ex := &fakeTransferExchange{st: st, accountScope: "scope-a", account: &exchange.Account{BalanceAsset: "USDT", AvailableBalance: 1000, MaxWithdrawAmount: 1000},
				ledgerEntries: []accounting.Entry{{ID: "unallocated-1", Kind: kind, Currency: "USDT", Amount: "-1", At: base.Add(time.Minute)}}}
			e := NewWithdrawExecutor(context.Background(), st, func(string) exchange.IExchange { return ex })
			e.now = func() time.Time { return base.Add(2 * time.Minute) }
			if err := e.processRule(st.rule); err == nil {
				t.Fatal("unallocated account expense must disable automatic withdrawal")
			}
			if len(ex.amounts) != 0 || len(st.records) != 1 || st.records[0].Status != "failed" {
				t.Fatalf("unallocated account expense must release its preflight reservation without transferring: amounts=%v records=%+v", ex.amounts, st.records)
			}
		})
	}
}

func TestAutomaticWithdrawRejectsStoredProfitAboveFreshExchangeLedger(t *testing.T) {
	base := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	st := &fakeWithdrawStorage{rule: &storage.ProfitWithdrawRule{
		ID: "stale-profit-rule", AccountID: "stale-profit-account", AccountScope: "scope-a", ExchangeID: "binance", StrategyID: "BTCUSDT",
		Enabled: true, TriggerAmount: 10, WithdrawRatio: 0.5, Frequency: frequencyImmediate, CreatedAt: base.Add(-time.Hour),
	}, coverageFrom: base.Add(-24 * time.Hour), coverageUntil: base.Add(24 * time.Hour),
		events: []pnlEvent{{at: base.Add(time.Minute), pnl: 100}}}
	ex := &fakeTransferExchange{st: st, accountScope: "scope-a", account: &exchange.Account{BalanceAsset: "USDT", AvailableBalance: 1000, MaxWithdrawAmount: 1000},
		ledgerEntries: []accounting.Entry{{ID: "fresh-pnl", Kind: "realized_pnl", Currency: "USDT", Amount: "20", Symbol: "BTCUSDT", At: base.Add(time.Minute)}}}
	e := NewWithdrawExecutor(context.Background(), st, func(string) exchange.IExchange { return ex })
	e.now = func() time.Time { return base.Add(2 * time.Minute) }
	if err := e.processRule(st.rule); err == nil {
		t.Fatal("stored profit exceeding fresh exchange-ledger profit must not be transferred")
	}
	if len(ex.amounts) != 0 || len(st.records) != 1 || st.records[0].Status != "failed" {
		t.Fatalf("stale stored profit must fail before transfer: amounts=%v records=%+v", ex.amounts, st.records)
	}
}

func TestAutomaticWithdrawRejectsCredentialScopeMismatchBeforeReservation(t *testing.T) {
	base := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	st := &fakeWithdrawStorage{rule: &storage.ProfitWithdrawRule{
		ID: "rotated-key-rule", AccountID: "rotated-key-account", AccountScope: "old-scope", ExchangeID: "binance", StrategyID: "BTCUSDT",
		Enabled: true, TriggerAmount: 10, WithdrawRatio: 0.5, Frequency: frequencyImmediate, CreatedAt: base.Add(-time.Hour),
	}, coverageFrom: base.Add(-24 * time.Hour), coverageUntil: base.Add(24 * time.Hour),
		events: []pnlEvent{{at: base.Add(time.Minute), pnl: 100}}}
	ex := &fakeTransferExchange{st: st, accountScope: "new-scope", account: &exchange.Account{BalanceAsset: "USDT", AvailableBalance: 1000, MaxWithdrawAmount: 1000}}
	e := NewWithdrawExecutor(context.Background(), st, func(string) exchange.IExchange { return ex })
	e.now = func() time.Time { return base.Add(2 * time.Minute) }
	if err := e.processRule(st.rule); err == nil {
		t.Fatal("old profit rule must not transfer funds using another credential scope")
	}
	if len(ex.amounts) != 0 || len(st.records) != 1 || st.records[0].Status != "failed" {
		t.Fatalf("scope mismatch must release its preflight reservation without transferring: amounts=%v records=%+v", ex.amounts, st.records)
	}
}

func TestAutomaticWithdrawUsesUnscopedCompletedWithdrawalAsCheckpoint(t *testing.T) {
	base := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	completedAt := base
	st := &fakeWithdrawStorage{rule: &storage.ProfitWithdrawRule{
		ID: "legacy-scope-rule", AccountID: "legacy-scope-account", AccountScope: "scope-a", ExchangeID: "binance", StrategyID: "BTCUSDT",
		Enabled: true, TriggerAmount: 10, WithdrawRatio: 0.5, Frequency: frequencyImmediate, CreatedAt: base.Add(-time.Hour),
	}, coverageFrom: base.Add(-24 * time.Hour), coverageUntil: base.Add(24 * time.Hour),
		events:  []pnlEvent{{at: base.Add(time.Minute), pnl: 100}},
		records: []*storage.ProfitWithdrawRecord{{AccountID: "legacy-scope-account", ExchangeID: "binance", StrategyID: "ETHUSDT", Status: "completed", CreatedAt: base.Add(-time.Minute), CompletedAt: &completedAt}},
	}
	ex := &fakeTransferExchange{st: st, accountScope: "scope-a", account: &exchange.Account{BalanceAsset: "USDT", AvailableBalance: 1000, MaxWithdrawAmount: 1000}}
	e := NewWithdrawExecutor(context.Background(), st, func(string) exchange.IExchange { return ex })
	e.now = func() time.Time { return base.Add(2 * time.Minute) }
	if err := e.processRule(st.rule); err != nil {
		t.Fatalf("completed legacy withdrawal should establish a global checkpoint: %v", err)
	}
	if len(ex.amounts) != 1 || len(st.records) != 2 {
		t.Fatalf("post-checkpoint profit should remain withdrawable: amounts=%v records=%+v", ex.amounts, st.records)
	}
}

func TestIsReconciledWithdrawalFailureRequiresLedgerEvidence(t *testing.T) {
	tests := []struct {
		name   string
		record *storage.ProfitWithdrawRecord
		want   bool
	}{
		{name: "evidence-confirmed failure", record: &storage.ProfitWithdrawRecord{Status: "failed", TransferID: "ledger-ref-1", FailedReason: "人工核账确认交易所流水未发生划转：逐笔核实金额、币种及账户流水。"}, want: true},
		{name: "legacy failed status without evidence", record: &storage.ProfitWithdrawRecord{Status: "failed", FailedReason: "transfer error"}},
		{name: "cancelled without verified ledger", record: &storage.ProfitWithdrawRecord{Status: "cancelled"}},
		{name: "failure without reference", record: &storage.ProfitWithdrawRecord{Status: "failed", FailedReason: "人工核账确认交易所流水未发生划转：逐笔核实。"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsReconciledWithdrawalFailure(tt.record); got != tt.want {
				t.Fatalf("IsReconciledWithdrawalFailure()=%v, want %v", got, tt.want)
			}
		})
	}
}

func TestLegacyWithdrawalCheckpointUsesConfirmedCompletionTime(t *testing.T) {
	createdAt := time.Date(2026, 9, 17, 9, 0, 0, 0, time.UTC)
	completedAt := createdAt.Add(2 * time.Minute)
	checkpoint, err := LegacyWithdrawalCheckpoint([]*storage.ProfitWithdrawRecord{{
		ExchangeID: "binance", StrategyID: "ETHUSDT", Status: "completed", CreatedAt: createdAt, CompletedAt: &completedAt,
	}}, "BINANCE")
	if err != nil {
		t.Fatal(err)
	}
	if !checkpoint.Equal(completedAt) {
		t.Fatalf("checkpoint=%v, want confirmed completion time %v", checkpoint, completedAt)
	}
	if _, err := LegacyWithdrawalCheckpoint([]*storage.ProfitWithdrawRecord{{
		ExchangeID: "binance", Status: "completed", CreatedAt: createdAt,
	}}, "binance"); err == nil {
		t.Fatal("completed legacy withdrawal without a completion time must fail closed")
	}
}

func TestAutomaticWithdrawRejectsUnknownAccountCashFlow(t *testing.T) {
	base := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	st := &fakeWithdrawStorage{rule: &storage.ProfitWithdrawRule{
		ID: "unknown-flow-rule", AccountID: "unknown-flow-account", AccountScope: "scope-a", ExchangeID: "binance", StrategyID: "BTCUSDT",
		Enabled: true, TriggerAmount: 10, WithdrawRatio: 0.5, Frequency: frequencyImmediate, CreatedAt: base.Add(-time.Hour),
	}, coverageFrom: base.Add(-24 * time.Hour), coverageUntil: base.Add(24 * time.Hour),
		events: []pnlEvent{{at: base.Add(time.Minute), pnl: 100}}}
	ex := &fakeTransferExchange{st: st, accountScope: "scope-a", account: &exchange.Account{BalanceAsset: "USDT", AvailableBalance: 1000, MaxWithdrawAmount: 1000},
		ledgerEntries: []accounting.Entry{{ID: "future-income-kind", Kind: "new_exchange_adjustment", Currency: "USDT", Amount: "-1", At: base.Add(time.Minute)}}}
	e := NewWithdrawExecutor(context.Background(), st, func(string) exchange.IExchange { return ex })
	e.now = func() time.Time { return base.Add(2 * time.Minute) }
	if err := e.processRule(st.rule); err == nil {
		t.Fatal("unknown account cash-flow kinds must disable automatic withdrawal")
	}
	if len(ex.amounts) != 0 || len(st.records) != 1 || st.records[0].Status != "failed" {
		t.Fatalf("unknown account cash flow must release its preflight reservation without transferring: amounts=%v records=%+v", ex.amounts, st.records)
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
				ID: "balance-rule", AccountID: "balance-account", AccountScope: "scope-a", ExchangeID: "binance", StrategyID: "BTCUSDT", Enabled: true,
				TriggerAmount: 10, WithdrawRatio: 0.5, Frequency: frequencyImmediate, CreatedAt: base.Add(-time.Hour),
			}, coverageFrom: base.Add(-24 * time.Hour), coverageUntil: base.Add(24 * time.Hour)}
			st.events = append(st.events, pnlEvent{at: base.Add(time.Minute), pnl: 100})
			ex := &fakeTransferExchange{st: st, accountScope: "scope-a", account: test.account, accountErr: test.accountErr}
			e := NewWithdrawExecutor(context.Background(), st, func(string) exchange.IExchange { return ex })
			e.now = func() time.Time { return base.Add(2 * time.Minute) }
			err := e.processRule(st.rule)
			if (err != nil) != test.wantErr {
				t.Fatalf("processRule err=%v, wantErr=%v", err, test.wantErr)
			}
			if test.wantErr && (len(ex.amounts) != 0 || len(st.records) != 1 || st.records[0].Status != "failed") {
				t.Fatalf("unsafe balance must release its preflight reservation without transferring: amounts=%v records=%+v", ex.amounts, st.records)
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
	ex := &fakeTransferExchange{st: st, accountScope: "scope-a", account: &exchange.Account{BalanceAsset: "USDT", AvailableBalance: 1000, MaxWithdrawAmount: 1000}}
	e := NewWithdrawExecutor(context.Background(), st, func(string) exchange.IExchange { return ex })
	e.now = func() time.Time { return base.Add(2 * time.Minute) }
	e.processRules(frequencyImmediate)
	if len(ex.amounts) != 0 || len(st.records) != 0 {
		t.Fatalf("overlapping rules must not transfer the same accounting stream twice: amounts=%v records=%+v", ex.amounts, st.records)
	}
}

func TestAutomaticWithdrawFailsClosedForDuplicatesAcrossAccountPartitions(t *testing.T) {
	base := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	newRule := func(id, accountID string) *storage.ProfitWithdrawRule {
		return &storage.ProfitWithdrawRule{ID: id, AccountID: accountID, AccountScope: "same-credential-scope", ExchangeID: "binance",
			StrategyID: "BTCUSDT", Enabled: true, TriggerAmount: 10, WithdrawRatio: 0.5,
			Frequency: frequencyImmediate, CreatedAt: base.Add(-time.Hour)}
	}
	st := &fakeWithdrawStorage{
		rule: newRule("account-a-rule", "account-a"),
		rules: []*storage.ProfitWithdrawRule{
			newRule("account-a-rule", "account-a"),
			newRule("account-b-rule", "account-b"),
		},
		coverageFrom: base.Add(-24 * time.Hour), coverageUntil: base.Add(24 * time.Hour),
		events: []pnlEvent{{at: base.Add(time.Minute), pnl: 100}},
	}
	ex := &fakeTransferExchange{st: st, accountScope: "same-credential-scope",
		account: &exchange.Account{BalanceAsset: "USDT", AvailableBalance: 1000, MaxWithdrawAmount: 1000}}
	e := NewWithdrawExecutor(context.Background(), st, func(string) exchange.IExchange { return ex })
	e.now = func() time.Time { return base.Add(2 * time.Minute) }
	e.processRules(frequencyImmediate)
	if len(ex.amounts) != 0 || len(st.records) != 0 {
		t.Fatalf("duplicate scoped rules across account partitions must both be blocked: amounts=%v records=%+v", ex.amounts, st.records)
	}
}

func TestAutomaticWithdrawAmbiguousTransferErrorIsNeverRetried(t *testing.T) {
	base := time.Date(2026, 9, 17, 10, 0, 0, 0, time.UTC)
	st := &fakeWithdrawStorage{rule: &storage.ProfitWithdrawRule{
		ID: "r1", AccountID: "acc", AccountScope: "scope-a", ExchangeID: "binance", StrategyID: "BTCUSDT", Enabled: true,
		TriggerAmount: 10, WithdrawRatio: 0.5, Frequency: frequencyImmediate, CreatedAt: base.Add(-time.Hour),
	}, coverageFrom: base.Add(-24 * time.Hour), coverageUntil: base.Add(24 * time.Hour)}
	st.events = append(st.events, pnlEvent{at: base.Add(time.Minute), pnl: 100})
	ex := &fakeTransferExchange{st: st, accountScope: "scope-a", err: errors.New("request timeout"), account: &exchange.Account{BalanceAsset: "USDT", AvailableBalance: 1000, MaxWithdrawAmount: 1000}}
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
		ID: "empty-transfer-id-rule", AccountID: "empty-transfer-id-account", AccountScope: "scope-a", ExchangeID: "binance", StrategyID: "BTCUSDT", Enabled: true,
		TriggerAmount: 10, WithdrawRatio: 0.5, Frequency: frequencyImmediate, CreatedAt: base.Add(-time.Hour),
	}, coverageFrom: base.Add(-24 * time.Hour), coverageUntil: base.Add(24 * time.Hour)}
	st.events = append(st.events, pnlEvent{at: base.Add(time.Minute), pnl: 100})
	ex := &fakeTransferExchange{st: st, accountScope: "scope-a", emptyTransferID: true,
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
		ExchangeID: "binance", StrategyID: "BTCUSDT", Enabled: true, TriggerAmount: 1, CreatedAt: base.Add(-time.Hour),
		WithdrawRatio: 1, Frequency: frequencyImmediate,
	}, coverageFrom: base.Add(-24 * time.Hour), coverageUntil: base.Add(24 * time.Hour)}
	st.events = append(st.events, pnlEvent{at: base.Add(time.Minute), pnl: 100})
	ex := &fakeTransferExchange{st: st, accountScope: "scope-a", account: &exchange.Account{BalanceAsset: "USDT", AvailableBalance: 1000, MaxWithdrawAmount: 1000}}
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
			ID: "coverage-rule", AccountID: "coverage-account", AccountScope: "scope-a", ExchangeID: "binance", StrategyID: "BTCUSDT",
			Enabled: true, TriggerAmount: 10, WithdrawRatio: 0.5, Frequency: frequencyImmediate, CreatedAt: base.Add(-time.Hour),
		},
		coverageFrom: base.Add(-24 * time.Hour), coverageUntil: coverageEnd,
		events: []pnlEvent{
			{at: base.Add(time.Minute), pnl: 100},
			{at: base.Add(3 * time.Minute), pnl: 1000}, // Not yet covered by the funding-history snapshot.
		},
	}
	ex := &fakeTransferExchange{st: st, accountScope: "scope-a", account: &exchange.Account{BalanceAsset: "USDT", AvailableBalance: 1000, MaxWithdrawAmount: 1000}}
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
					ID: "r1", AccountID: "acc", AccountScope: "scope-a", ExchangeID: "binance", StrategyID: "BTCUSDT", Enabled: true,
					TriggerAmount: 10, WithdrawRatio: 0.5, Frequency: "immediate", CreatedAt: base.Add(-time.Hour),
				},
				coverageFrom: base.Add(-24 * time.Hour), coverageUntil: base.Add(24 * time.Hour),
				failLastTrig: tt.failLastTrig,
			}
			ex := &fakeTransferExchange{st: st, accountScope: "scope-a", account: &exchange.Account{BalanceAsset: "USDT", AvailableBalance: 1000, MaxWithdrawAmount: 1000}}
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
