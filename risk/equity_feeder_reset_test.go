package risk

import (
	"context"
	"errors"
	"testing"
	"time"
)

type resetAccountEquitySource struct {
	observation EquityObservation
	err         error
	calls       int
}

func (s *resetAccountEquitySource) ObserveAccountEquity(context.Context, map[string]time.Time) (EquityObservation, error) {
	s.calls++
	return s.observation, s.err
}

func (s *resetAccountEquitySource) TotalEquity(context.Context) (float64, error) {
	return s.observation.Equity, s.err
}

func resetFixture(t *testing.T, now time.Time, source *resetAccountEquitySource, store *memoryEquityStore) *MetricsFeeder {
	t.Helper()
	oldObservation := testWalletObservation(now.Add(-time.Minute), 0, "1000", 1000, time.Time{})
	oldObservation.Scope = "old-scope"
	old, err := nextEquityCheckpoint(nil, oldObservation, now, time.Time{}, time.Hour, true)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SaveEquityState(context.Background(), 0, old); err != nil {
		t.Fatal(err)
	}
	return NewMetricsFeeder(&fakeSink{}, nil, source, nil, MetricsFeederOptions{
		Now: func() time.Time { return now }, MaxEquityAge: time.Hour, EquityStore: store,
		RequirePersistence: true, RequireCashFlowReconciliation: true,
	})
}

func TestResetEquityBaselineUsesFreshEvidenceCASAndIsIdempotent(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	observation := testWalletObservation(now, 0, "1250", 1250, time.Time{})
	observation.Scope = "new-scope"
	source, store := &resetAccountEquitySource{observation: observation}, &memoryEquityStore{}
	feeder := resetFixture(t, now, source, store)

	state, err := feeder.ResetEquityBaseline(t.Context(), "operator-reset-1", "new-scope")
	if err != nil {
		t.Fatalf("reset baseline: %v", err)
	}
	if state.Revision != 2 || state.ResetOperationRevision != 2 || state.Scope != "new-scope" || state.ResetOperationID != "operator-reset-1" ||
		state.LastEquity != 1250 || state.DrawdownPct != 0 || source.calls != 1 || feeder.equityState == nil || !feeder.equityLoaded {
		t.Fatalf("unexpected reset state: %+v calls=%d cache=%+v loaded=%v", state, source.calls, feeder.equityState, feeder.equityLoaded)
	}
	restartedFeeder := NewMetricsFeeder(&fakeSink{}, nil, source, nil, MetricsFeederOptions{
		Now: func() time.Time { return now }, MaxEquityAge: time.Hour, EquityStore: store,
		RequirePersistence: true, RequireCashFlowReconciliation: true,
	})
	retried, err := restartedFeeder.ResetEquityBaseline(t.Context(), "operator-reset-1", "new-scope")
	if err != nil || retried.Revision != state.Revision || source.calls != 1 {
		t.Fatalf("same operation retry was not idempotent: state=%+v calls=%d err=%v", retried, source.calls, err)
	}
}

func TestResetEquityBaselineRejectsIncompleteOrWrongScopeEvidence(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name   string
		mutate func(*EquityObservation)
	}{
		{name: "incomplete ledger", mutate: func(o *EquityObservation) { o.CashFlowComplete = false }},
		{name: "missing wallet evidence", mutate: func(o *EquityObservation) { o.Wallets = nil }},
		{name: "wrong scope", mutate: func(o *EquityObservation) { o.Scope = "different-scope" }},
		{name: "stale snapshot", mutate: func(o *EquityObservation) { o.ObservedAt = now.Add(-2 * time.Hour) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			observation := testWalletObservation(now, 0, "1250", 1250, time.Time{})
			observation.Scope = "new-scope"
			tc.mutate(&observation)
			source, store := &resetAccountEquitySource{observation: observation}, &memoryEquityStore{}
			feeder := resetFixture(t, now, source, store)
			if _, err := feeder.ResetEquityBaseline(t.Context(), "operator-reset-2", "new-scope"); err == nil {
				t.Fatal("unsafe target-scope observation was accepted")
			}
			if store.revision != 1 {
				t.Fatalf("rejected observation changed persisted revision: %d", store.revision)
			}
		})
	}
}

func TestResetEquityBaselineCredentialOnlyRotationPreservesPriorHighWater(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	baseObservation := testWalletObservation(now.Add(-2*time.Minute), 0, "1500", 1500, time.Time{})
	baseObservation.Scope = "same-account-scope"
	base, err := nextEquityCheckpoint(nil, baseObservation, now, time.Time{}, time.Hour, true)
	if err != nil {
		t.Fatal(err)
	}
	previousObservation := testWalletObservation(now.Add(-time.Minute), 0, "1200", 1200, base.BaseWallets["a"].From)
	previousObservation.Scope = base.Scope
	loss := testWalletFlow("loss", "realized_pnl", "-300", now.Add(-time.Minute-time.Second))
	previousObservation.Flows = []EquityCashFlow{loss}
	previous, err := nextEquityCheckpoint(&base, previousObservation, now, time.Time{}, time.Hour, true)
	if err != nil {
		t.Fatal(err)
	}
	observation := testWalletObservation(now, 0, "1300", 1300, base.BaseWallets["a"].From)
	observation.Scope = previous.Scope
	observation.Flows = []EquityCashFlow{loss, testWalletFlow("realized-pnl", "realized_pnl", "100", now.Add(-time.Second))}
	source, store := &resetAccountEquitySource{observation: observation}, &memoryEquityStore{}
	if err := store.SaveEquityState(context.Background(), 0, base); err != nil {
		t.Fatal(err)
	}
	// Persist the existing loss/high-water checkpoint rather than a new baseline.
	if err := store.SaveEquityState(context.Background(), 1, previous); err != nil {
		t.Fatal(err)
	}
	feeder := NewMetricsFeeder(&fakeSink{}, nil, source, nil, MetricsFeederOptions{
		Now: func() time.Time { return now }, MaxEquityAge: time.Hour, EquityStore: store,
		RequirePersistence: true, RequireCashFlowReconciliation: true,
	})
	state, err := feeder.ResetEquityBaseline(t.Context(), "credential-rotation-reset", previous.Scope)
	if err != nil {
		t.Fatalf("reconcile same account after credential rotation: %v", err)
	}
	if state.Scope != previous.Scope || state.HighWater != previous.HighWater || state.DrawdownPct >= previous.DrawdownPct ||
		state.ResetAt != previous.ResetAt || state.ResetOperationID != "credential-rotation-reset" || state.ResetOperationRevision != state.Revision {
		t.Fatalf("credential-only rotation reset performance history: previous=%+v next=%+v", previous, state)
	}
}

func TestResetEquityBaselineCASFailureKeepsCacheInvalid(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	observation := testWalletObservation(now, 0, "1250", 1250, time.Time{})
	observation.Scope = "new-scope"
	source := &resetAccountEquitySource{observation: observation}
	store := &memoryEquityStore{}
	feeder := resetFixture(t, now, source, store)
	store.saveErr = errors.New("injected CAS conflict")
	feeder.equityLoaded = true
	if _, err := feeder.ResetEquityBaseline(t.Context(), "operator-reset-3", "new-scope"); err == nil {
		t.Fatal("CAS failure was ignored")
	}
	if feeder.equityLoaded || feeder.equityState != nil || store.revision != 1 {
		t.Fatalf("CAS failure changed checkpoint cache or persistence: loaded=%v cache=%+v revision=%d", feeder.equityLoaded, feeder.equityState, store.revision)
	}
}
