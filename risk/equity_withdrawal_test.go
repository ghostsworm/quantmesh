package risk

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"quantmesh/exchange/accounting"
)

type withdrawalEvidenceSource struct {
	observation EquityObservation
	cursors     map[string]time.Time
}

func (s *withdrawalEvidenceSource) TotalEquity(context.Context) (float64, error) {
	return s.observation.Equity, nil
}

func (s *withdrawalEvidenceSource) ObserveAccountEquity(_ context.Context, cursors map[string]time.Time) (EquityObservation, error) {
	s.cursors = make(map[string]time.Time, len(cursors))
	for key, value := range cursors {
		s.cursors[key] = value
	}
	return s.observation, nil
}

func TestRefreshWithdrawalEvidenceRequiresAndPersistsExactWalletReconciliation(t *testing.T) {
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	accountIdentityBytes, _ := json.Marshal([]string{"futures", "scope-a"})
	accountIdentity := string(accountIdentityBytes)
	walletIDBytes, _ := json.Marshal([]string{accountIdentity, "USDT"})
	walletID := string(walletIDBytes)
	baseFrom := now.Add(-time.Hour)
	source := &withdrawalEvidenceSource{observation: EquityObservation{
		Scope: "scope-a", Currency: "USDT", Equity: 100, ObservedAt: now, CashFlowComplete: true,
		Wallets: map[string]accounting.Wallet{walletID: {Currency: "USDT", Balance: "100", From: baseFrom, Through: now, ObservedAt: now}},
	}}
	store := &memoryEquityStore{}
	options := MetricsFeederOptions{Now: func() time.Time { return now }, EquityStore: store,
		RequirePersistence: true, RequireCashFlowReconciliation: true, MaxEquityAge: 2 * time.Minute}
	feeder := NewMetricsFeeder(&fakeSink{}, nil, source, nil, options)
	if _, err := feeder.Tick(t.Context()); err != nil {
		t.Fatalf("establish durable equity baseline: %v", err)
	}

	since := now.Add(-30 * time.Second)
	now = now.Add(time.Minute)
	observationTime := now
	flow := EquityCashFlow{ID: "withdrawal-flow-1", Account: walletID, Kind: "realized_pnl", Currency: "USDT",
		WalletCurrency: "USDT", ExactAmount: "10", Symbol: "BTCUSDT", Amount: 10, At: now.Add(-time.Second)}
	wallet := accounting.Wallet{Currency: "USDT", Balance: "110", From: baseFrom, Through: observationTime, ObservedAt: observationTime}
	// The verifier requests at least the persisted overlap cursor, which predates
	// the withdrawal window and is necessary to detect late ledger receipts.
	source.observation = EquityObservation{Scope: "scope-a", Currency: "USDT", Equity: 110, ObservedAt: observationTime,
		CashFlowComplete: true, Wallets: map[string]accounting.Wallet{walletID: wallet}, Flows: []EquityCashFlow{flow}}
	verified, err := feeder.RefreshWithdrawalEvidence(t.Context(), accountIdentity, since)
	if err != nil {
		t.Fatalf("refresh reconciled withdrawal evidence: %v", err)
	}
	if verified.Wallets[walletID].Balance != "110" || store.revision != 2 {
		t.Fatalf("verified evidence was not durably advanced: wallet=%+v revision=%d", verified.Wallets[walletID], store.revision)
	}
	if cursor, ok := source.cursors[accountIdentity]; !ok || cursor.After(since) {
		t.Fatalf("refresh cursor did not cover withdrawal interval and overlap: cursor=%v present=%v since=%v", cursor, ok, since)
	}

	bad := source.observation
	bad.ObservedAt = now.Add(time.Minute)
	bad.Wallets = map[string]accounting.Wallet{walletID: {
		Currency: "USDT", Balance: "120", From: bad.Wallets[walletID].From,
		Through: bad.ObservedAt, ObservedAt: bad.ObservedAt,
	}}
	bad.Flows = nil
	source.observation = bad
	if _, err := feeder.RefreshWithdrawalEvidence(t.Context(), accountIdentity, since); err == nil {
		t.Fatal("wallet delta without matching ledger receipt was accepted")
	}
	if store.revision != 2 || feeder.equityState.Revision != 2 {
		t.Fatalf("failed reconciliation changed durable state: store=%d feeder=%d", store.revision, feeder.equityState.Revision)
	}
}
