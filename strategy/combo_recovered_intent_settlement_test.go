package strategy

import (
	"context"
	"errors"
	"testing"
)

type comboIntentSettlerExecutor struct {
	comboAdmissionTestExecutor
	gotContext  context.Context
	gotClientID string
	err         error
}

func (e *comboIntentSettlerExecutor) SettleRecoveredIntent(ctx context.Context, clientOrderID string) error {
	e.gotContext = ctx
	e.gotClientID = clientOrderID
	return e.err
}

func TestComboExposureExecutorForwardsRecoveredIntentSettlement(t *testing.T) {
	type contextKey struct{}
	ctx := context.WithValue(context.Background(), contextKey{}, "recovery")
	wantErr := errors.New("underlying settlement rejected")
	next := &comboIntentSettlerExecutor{err: wantErr}
	gate := &comboExposureAdmissionExecutor{next: next}

	err := gate.SettleRecoveredIntent(ctx, "combo-recovered-cid")
	if !errors.Is(err, wantErr) {
		t.Fatalf("settlement error was not propagated: got %v, want %v", err, wantErr)
	}
	if next.gotContext != ctx || next.gotClientID != "combo-recovered-cid" {
		t.Fatalf("settlement call changed its scope: context=%v clientOrderID=%q", next.gotContext, next.gotClientID)
	}
}

func TestComboExposureExecutorRejectsUnsupportedRecoveredIntentSettlement(t *testing.T) {
	gate := &comboExposureAdmissionExecutor{next: &comboAdmissionTestExecutor{}}
	if err := gate.SettleRecoveredIntent(context.Background(), "combo-recovered-cid"); err == nil {
		t.Fatal("expected unsupported settlement to fail closed")
	}
}
