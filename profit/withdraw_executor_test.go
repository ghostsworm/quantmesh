package profit

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"quantmesh/exchange"
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
	events        []pnlEvent
	records       []*storage.ProfitWithdrawRecord
	failLastTrig  bool
	transferCalls int
}

func (f *fakeWithdrawStorage) ListAccountIDsWithProfitRules() ([]string, error) {
	return []string{f.rule.AccountID}, nil
}
func (f *fakeWithdrawStorage) ListProfitWithdrawRules(accountID string) ([]*storage.ProfitWithdrawRule, error) {
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
func (f *fakeWithdrawStorage) GetWithdrawRecords(accountID string, limit int) ([]*storage.ProfitWithdrawRecord, error) {
	return f.records, nil
}
func (f *fakeWithdrawStorage) SaveWithdrawRecord(r *storage.ProfitWithdrawRecord) error {
	f.records = append(f.records, r)
	return nil
}
func (f *fakeWithdrawStorage) UpdateWithdrawRecordStatus(id, status, transferID, failedReason string) error {
	for _, r := range f.records {
		if r.ID == id {
			r.Status = status
			r.TransferID = transferID
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

type fakeTransferExchange struct {
	exchange.IExchange
	st      *fakeWithdrawStorage
	amounts []float64
}

func (f *fakeTransferExchange) InternalTransfer(ctx context.Context, from, to, asset string, amount float64) (string, error) {
	f.amounts = append(f.amounts, amount)
	return "tx", nil
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
					ID: "r1", AccountID: "acc", ExchangeID: "binance", Enabled: true,
					TriggerAmount: 10, WithdrawRatio: 0.5, Frequency: "immediate",
				},
				failLastTrig: tt.failLastTrig,
			}
			ex := &fakeTransferExchange{st: st}
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
