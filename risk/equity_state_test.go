package risk

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"testing"
	"time"
)

type fakeLedgerSource struct {
	fakeEquitySource
	observation EquityObservation
	since       time.Time
}

func (s *fakeLedgerSource) ObserveEquity(_ context.Context, since time.Time) (EquityObservation, error) {
	s.calls++
	s.since = since
	return s.observation, s.err
}

func testEquityObservation(now time.Time, equity float64) EquityObservation {
	return EquityObservation{Scope: "account-a:futures", Currency: "USDT", Equity: equity, ObservedAt: now,
		CashFlowComplete: true, FlowFrom: now.Add(-24 * time.Hour), FlowThrough: now}
}

type memoryEquityStore struct {
	data             []byte
	revision         int64
	loadErr, saveErr error
}

func (s *memoryEquityStore) LoadEquityState(context.Context) (*EquityCheckpoint, error) {
	if s.loadErr != nil {
		return nil, s.loadErr
	}
	if s.data == nil {
		return nil, nil
	}
	var checkpoint EquityCheckpoint
	err := json.Unmarshal(s.data, &checkpoint)
	return &checkpoint, err
}

func (s *memoryEquityStore) SaveEquityState(_ context.Context, revision int64, state EquityCheckpoint) error {
	if s.saveErr != nil {
		return s.saveErr
	}
	if revision != s.revision {
		return errors.New("revision conflict")
	}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	s.data, s.revision = data, state.Revision
	return nil
}

func TestEquityDrawdownPersistsHighWaterAndCashFlowReceipts(t *testing.T) {
	now := time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC)
	base := now
	source := &fakeLedgerSource{observation: testEquityObservation(now, 1000)}
	store := &memoryEquityStore{}
	opts := MetricsFeederOptions{Now: func() time.Time { return now }, EquityStore: store, RequirePersistence: true, RequireCashFlowReconciliation: true}
	feeder := NewMetricsFeeder(&fakeSink{}, nil, source, nil, opts)
	if _, err := feeder.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	source.observation = testEquityObservation(now, 1200)
	if _, err := feeder.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	deposit := EquityCashFlow{ID: "deposit", Kind: "deposit", Currency: "USDT", Amount: 500, At: now.Add(-10 * time.Second)}
	fee := EquityCashFlow{ID: "fee", Kind: "fee", Currency: "USDT", Amount: -100, At: now.Add(-5 * time.Second)}
	source.observation = testEquityObservation(now, 1600) // +500 capital, -100 expense
	source.observation.Flows = []EquityCashFlow{deposit, deposit, fee}
	snapshot, err := feeder.Tick(t.Context())
	if err != nil || !approxEqual(snapshot.MaxDrawdownPct, 100.0/12) || !snapshot.CashFlowAdjusted {
		t.Fatalf("cash-flow/fee accounting: %+v %v", snapshot, err)
	}
	feeder = NewMetricsFeeder(&fakeSink{}, nil, source, nil, opts) // actual JSON reload
	now = now.Add(time.Minute)
	source.observation = testEquityObservation(now, 1300)
	withdrawal := EquityCashFlow{ID: "withdrawal", Kind: "withdrawal", Currency: "USDT", Amount: -300, At: now.Add(-5 * time.Second)}
	source.observation.Flows = []EquityCashFlow{deposit, fee, withdrawal}
	snapshot, err = feeder.Tick(t.Context())
	if err != nil || !approxEqual(snapshot.MaxDrawdownPct, 100.0/12) {
		t.Fatalf("restart/withdrawal accounting: %+v %v", snapshot, err)
	}
	if feeder.equityState.HighWater != 1200 || feeder.equityState.ExternalFlows != 200 || !source.since.Equal(base) {
		t.Fatalf("lost high water/cursor: %+v since=%s", feeder.equityState, source.since)
	}
	// A loss that never appeared in trades is still captured after restart.
	now = now.Add(time.Minute)
	source.observation.Equity = 1100
	source.observation.ObservedAt, source.observation.FlowThrough = now, now
	snapshot, err = feeder.Tick(t.Context())
	if err != nil || !approxEqual(snapshot.MaxDrawdownPct, 25) {
		t.Fatalf("unbooked debit missed: %+v %v", snapshot, err)
	}
}

func TestEquityCheckpointRejectsUnverifiedObservations(t *testing.T) {
	base := time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC)
	previous, err := nextEquityCheckpoint(nil, testEquityObservation(base, 1000), base, time.Time{}, time.Minute, true)
	if err != nil {
		t.Fatal(err)
	}
	now := base.Add(time.Minute)
	for _, tc := range []struct {
		name   string
		mutate func(*EquityObservation)
	}{
		{"nan", func(o *EquityObservation) { o.Equity = math.NaN() }},
		{"inf", func(o *EquityObservation) { o.Equity = math.Inf(1) }},
		{"scope", func(o *EquityObservation) { o.Scope = "account-b" }},
		{"currency", func(o *EquityObservation) { o.Currency = "BTC" }},
		{"stale", func(o *EquityObservation) { o.ObservedAt = base.Add(-time.Hour) }},
		{"future", func(o *EquityObservation) { o.ObservedAt = now.Add(time.Second) }},
		{"regressed", func(o *EquityObservation) { o.ObservedAt = base.Add(-time.Second) }},
		{"same_time_changed_equity", func(o *EquityObservation) { o.ObservedAt = base }},
		{"missing_coverage", func(o *EquityObservation) { o.CashFlowComplete = false }},
		{"coverage_gap", func(o *EquityObservation) { o.FlowFrom = base.Add(time.Second) }},
		{"incomplete_tail", func(o *EquityObservation) { o.FlowThrough = now.Add(-time.Second) }},
		{"unclassified_flow", func(o *EquityObservation) {
			o.Flows = []EquityCashFlow{{ID: "x", Kind: "unknown", Currency: "USDT", At: now}}
		}},
		{"inverted_withdrawal", func(o *EquityObservation) {
			o.Flows = []EquityCashFlow{{ID: "x", Kind: "withdrawal", Amount: 1, Currency: "USDT", At: now}}
		}},
		{"unvalued_asset", func(o *EquityObservation) {
			o.Flows = []EquityCashFlow{{ID: "x", Kind: "funding", Currency: "BTC", At: now}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			o := testEquityObservation(now, 900)
			tc.mutate(&o)
			if _, err := nextEquityCheckpoint(&previous, o, now, time.Time{}, 2*time.Minute, true); err == nil {
				t.Fatal("accepted unverified observation")
			}
			if previous.HighWater != 1000 || previous.Revision != 1 || len(previous.Receipts) != 0 {
				t.Fatal("failure mutated checkpoint")
			}
		})
	}
}

func TestEquityCheckpointLateOrConflictingReceiptRequiresReconciliation(t *testing.T) {
	now := time.Date(2026, 9, 24, 0, 0, 0, 0, time.UTC)
	base, _ := nextEquityCheckpoint(nil, testEquityObservation(now, 1000), now, time.Time{}, time.Minute, true)
	now = now.Add(time.Minute)
	o := testEquityObservation(now, 1100)
	o.Flows = []EquityCashFlow{{ID: "flow", Kind: "deposit", Currency: "USDT", Amount: 100, At: now.Add(-time.Second)}}
	previous, err := nextEquityCheckpoint(&base, o, now, time.Time{}, time.Minute, true)
	if err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	o.ObservedAt, o.FlowThrough = now, now
	o.Flows[0].Amount = 200
	if _, err := nextEquityCheckpoint(&previous, o, now, time.Time{}, time.Minute, true); err == nil {
		t.Fatal("changed receipt accepted")
	}
	o.Flows[0].Amount = 100
	o.Flows[0].ID = "late"
	if _, err := nextEquityCheckpoint(&previous, o, now, time.Time{}, time.Minute, true); err == nil {
		t.Fatal("late flow silently rewrote historical risk")
	}
	o.Flows = nil
	if _, err := nextEquityCheckpoint(&previous, o, now, time.Time{}, time.Minute, true); err == nil {
		t.Fatal("incomplete overlap accepted as complete")
	}
}

func TestEquityFundingInterestAndFeesAreNotNeutralized(t *testing.T) {
	base := time.Now()
	previous, err := nextEquityCheckpoint(nil, testEquityObservation(base, 1000), base, time.Time{}, time.Minute, true)
	if err != nil {
		t.Fatal(err)
	}
	now := base.Add(time.Minute)
	o := testEquityObservation(now, 800)
	o.Flows = []EquityCashFlow{
		{ID: "funding", Kind: "funding", Currency: "USDT", Amount: -120, At: now.Add(-3 * time.Second)},
		{ID: "interest", Kind: "interest", Currency: "USDT", Amount: -60, At: now.Add(-2 * time.Second)},
		{ID: "fee", Kind: "fee", Currency: "USDT", Amount: -20, At: now.Add(-time.Second)},
	}
	next, err := nextEquityCheckpoint(&previous, o, now, time.Time{}, time.Minute, true)
	if err != nil || next.DrawdownPct != 20 || next.ExternalFlows != 0 {
		t.Fatalf("costs ignored or double-deducted: %+v %v", next, err)
	}
}

func TestEquityCheckpointZeroAndNegativeBalancesAreLosses(t *testing.T) {
	now := time.Now()
	base, _ := nextEquityCheckpoint(nil, testEquityObservation(now, 1000), now, time.Time{}, time.Minute, true)
	for _, balance := range []float64{0, -100} {
		o := testEquityObservation(now.Add(time.Second), balance)
		next, err := nextEquityCheckpoint(&base, o, o.ObservedAt, time.Time{}, time.Minute, true)
		if err != nil || !approxEqual(next.DrawdownPct, (1000-balance)/10) {
			t.Fatalf("balance=%v result=%+v err=%v", balance, next, err)
		}
	}
}

func TestEquityStorageFailureDoesNotResetOrPublishMetrics(t *testing.T) {
	now := time.Now()
	source := &fakeEquitySource{equity: 1000}
	store := &memoryEquityStore{}
	sink := &fakeSink{}
	feeder := NewMetricsFeeder(sink, nil, source, nil, MetricsFeederOptions{Now: func() time.Time { return now }, EquityStore: store, RequirePersistence: true})
	store.loadErr = errors.New("database unavailable")
	if _, err := feeder.Tick(t.Context()); err == nil || sink.writes != 0 {
		t.Fatal("load failure treated as empty baseline")
	}
	store.loadErr = nil
	if _, err := feeder.Tick(t.Context()); err != nil {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	source.equity = 800
	store.saveErr = errors.New("disk full")
	if _, err := feeder.Tick(t.Context()); err == nil || sink.writes != 1 || feeder.equityState.LastEquity != 1000 {
		t.Fatal("save failure advanced state or published metrics")
	}
	store.saveErr = nil
	snap, err := feeder.Tick(t.Context())
	if err != nil || snap.MaxDrawdownPct != 20 || sink.writes != 2 {
		t.Fatalf("retry: %+v %v", snap, err)
	}
	store.data = []byte(`{"version":999}`)
	feeder.equityLoaded = false
	if _, err := feeder.Tick(t.Context()); err == nil || sink.writes != 2 {
		t.Fatal("corrupt state reset baseline")
	}
}

func TestEquityStrictModeRequiresCashFlowsAndPersistence(t *testing.T) {
	for _, opts := range []MetricsFeederOptions{{RequireCashFlowReconciliation: true}, {RequirePersistence: true}} {
		sink := &fakeSink{}
		feeder := NewMetricsFeeder(sink, nil, &fakeEquitySource{equity: 1000}, nil, opts)
		if _, err := feeder.Tick(t.Context()); err == nil || sink.writes != 0 {
			t.Fatal("strict coverage requirement ignored")
		}
		feeder = NewMetricsFeeder(sink, nil, nil, nil, opts)
		if _, err := feeder.Tick(t.Context()); err == nil {
			t.Fatal("strict mode accepted missing equity source")
		}
	}
}

func TestEquityFeederRejectsNonfinitePnLBeforeSavingState(t *testing.T) {
	for _, value := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		sink, store := &fakeSink{}, &memoryEquityStore{}
		bots := &circuitBreakerMockProvider{bots: []BotController{&pnlBot{pnl: value}}}
		feeder := NewMetricsFeeder(sink, nil, &fakeEquitySource{equity: 1000}, bots, MetricsFeederOptions{EquityStore: store})
		if _, err := feeder.Tick(t.Context()); err == nil || sink.writes != 0 || store.revision != 0 {
			t.Fatal("nonfinite PnL advanced risk state")
		}
	}
}
