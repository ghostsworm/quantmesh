package strategy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	"quantmesh/storage"
)

// The strategy is wired to real SQLite storage and its generation-fenced
// writer. Only the lost acknowledgement boundary is injected after the SQL
// transaction has durably committed.
type commitConfirmedCanceledRuntimeStore struct {
	db                  *storage.SQLStorage
	generation          storage.FundingCarryRuntimeGeneration
	mode                string
	saveCalls           int
	readCalls           int
	casCalls            int
	botID               string
	readFailureAfterCAS bool
	committedPayload    string
	conflictPayload     string
	expectedContext     context.Context
	cancelCaller        context.CancelFunc
	contextWriterCalls  int
	legacyWriteCalls    int
	callerContextSeen   bool
}

func (s *commitConfirmedCanceledRuntimeStore) SaveRuntimeState(_ string, version int, payload string) error {
	s.legacyWriteCalls++
	if s.saveCalls == 0 {
		return errors.New("legacy SaveRuntimeState used; caller context was not propagated")
	}
	return s.commitSnapshot(version, payload)
}

func (s *commitConfirmedCanceledRuntimeStore) SaveRuntimeStateContext(ctx context.Context, _ string, version int, payload string) error {
	s.contextWriterCalls++
	s.callerContextSeen = ctx == s.expectedContext && ctx.Value(fundingCarryIntentContextKey{}) == "caller-context"
	if err := s.commitSnapshot(version, payload); err != nil {
		return err
	}
	if s.mode == "unknown_dispatch" {
		if s.contextWriterCalls == 1 {
			return nil // beginRuntimeIntent's prepared checkpoint is acknowledged
		}
		return errors.Join(storage.ErrFundingCarryRuntimeStateCommitOutcomeUnknown, errors.New("injected dispatch-marker acknowledgement loss"))
	}
	// Model cancellation after the durable SQL COMMIT, before the write
	// acknowledgement reaches the strategy caller.
	s.cancelCaller()
	if s.mode == "unknown" {
		return errors.Join(storage.ErrFundingCarryRuntimeStateCommitOutcomeUnknown, ctx.Err())
	}
	return errors.Join(storage.ErrFundingCarryRuntimeStateCommitConfirmedCanceled, ctx.Err())
}

func (s *commitConfirmedCanceledRuntimeStore) commitSnapshot(version int, payload string) error {
	s.saveCalls++
	state := &storage.StrategyRuntimeState{BotID: s.botID, StrategyName: "funding_carry", SchemaVersion: version, Payload: payload}
	if err := s.db.SetFundingCarryRuntimeState(context.Background(), s.generation, state); err != nil {
		return err
	}
	if s.saveCalls == 1 {
		s.committedPayload = payload
	}
	return nil
}

func (s *commitConfirmedCanceledRuntimeStore) LoadRuntimeState(name string) (int, string, bool, error) {
	return s.LoadRuntimeStateContext(context.Background(), name)
}

func (s *commitConfirmedCanceledRuntimeStore) LoadRuntimeStateContext(ctx context.Context, name string) (int, string, bool, error) {
	s.readCalls++
	if err := ctx.Err(); err != nil {
		return 0, "", false, err
	}
	if s.mode == "read_error" || s.readFailureAfterCAS {
		return 0, "", false, errors.New("injected durable read failure")
	}
	if s.mode == "changed_snapshot" && s.readCalls == 1 {
		current, err := s.db.GetStrategyRuntimeStateContext(ctx, s.botID, name)
		if err != nil {
			return 0, "", false, err
		}
		current.Payload = " " + current.Payload // semantically equal, but not the exact S1 receipt.
		s.conflictPayload = current.Payload
		if err := s.db.SetFundingCarryRuntimeState(ctx, s.generation, current); err != nil {
			return 0, "", false, err
		}
	}
	current, err := s.db.GetStrategyRuntimeStateContext(ctx, s.botID, name)
	if err != nil {
		return 0, "", false, err
	}
	if current == nil {
		return 0, "", false, nil
	}
	return current.SchemaVersion, current.Payload, true, nil
}

func (s *commitConfirmedCanceledRuntimeStore) CompareAndSwapRuntimeState(ctx context.Context, name string, expectedVersion int, expectedPayload string, nextVersion int, nextPayload string) (bool, error) {
	s.casCalls++
	if s.mode == "owner_takeover" {
		_, err := s.db.ClaimFundingCarryRuntimeGeneration(ctx, []string{fmt.Sprintf("%064x", 1)})
		if err != nil {
			return false, err
		}
	}
	if s.mode == "cas_conflict" {
		current, err := s.db.GetStrategyRuntimeStateContext(ctx, s.botID, name)
		if err != nil {
			return false, err
		}
		current.Payload = " " + current.Payload
		s.conflictPayload = current.Payload
		if err := s.db.SetFundingCarryRuntimeState(ctx, s.generation, current); err != nil {
			return false, err
		}
	}
	saved, err := s.db.CompareAndSwapFundingCarryRuntimeState(ctx, s.generation,
		&storage.StrategyRuntimeState{BotID: s.botID, StrategyName: name, SchemaVersion: nextVersion, Payload: nextPayload},
		expectedVersion, expectedPayload)
	if err != nil {
		return false, err
	}
	if s.mode == "cas_ack_unknown" && saved {
		return false, errors.Join(storage.ErrFundingCarryRuntimeStateCommitConfirmedCanceled, context.Canceled)
	}
	if s.mode == "cas_ack_outcome_unknown" && saved {
		return false, errors.Join(storage.ErrFundingCarryRuntimeStateCommitOutcomeUnknown, errors.New("injected rollback acknowledgement loss"))
	}
	if s.mode == "cas_ack_unknown_read_failure" && saved {
		s.readFailureAfterCAS = true
		return false, errors.Join(storage.ErrFundingCarryRuntimeStateCommitConfirmedCanceled, context.Canceled)
	}
	return saved, nil
}

func TestFundingCarryCommitConfirmedCanceledBeforeTransferNeverSubmitsOrPoisonsState(t *testing.T) {
	tests := []struct {
		name          string
		caller        string
		mode          string
		wantRecovered bool
		wantCAS       bool
	}{
		{name: "collateral transfer exact receipt permits only pre-submit rollback", caller: "margin_transfer", wantRecovered: true, wantCAS: true},
		{name: "profit harvest exact receipt permits only pre-submit rollback", caller: "profit_harvest", wantRecovered: true, wantCAS: true},
		{name: "profit harvest snapshot conflict preserves durable evidence", caller: "profit_harvest", mode: "changed_snapshot", wantCAS: false},
		{name: "profit harvest CAS conflict preserves newer durable evidence", caller: "profit_harvest", mode: "cas_conflict", wantCAS: true},
		{name: "unknown commit with exact readback permits only pre-submit rollback", caller: "margin_transfer", mode: "unknown", wantRecovered: true, wantCAS: true},
		{name: "durable read failure remains blocked", caller: "margin_transfer", mode: "read_error"},
		{name: "changed snapshot remains blocked", caller: "margin_transfer", mode: "changed_snapshot"},
		{name: "owner takeover remains blocked", caller: "margin_transfer", mode: "owner_takeover", wantCAS: true},
		{name: "condition CAS conflict remains blocked", caller: "margin_transfer", mode: "cas_conflict", wantCAS: true},
		{name: "rollback lost acknowledgement with exact readback is resolved", caller: "margin_transfer", mode: "cas_ack_unknown", wantRecovered: true, wantCAS: true},
		{name: "rollback unknown outcome with exact readback is resolved", caller: "margin_transfer", mode: "cas_ack_outcome_unknown", wantRecovered: true, wantCAS: true},
		{name: "rollback lost acknowledgement without readback remains blocked", caller: "margin_transfer", mode: "cas_ack_unknown_read_failure", wantCAS: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			futuresBalance := float64(0)
			if tt.caller == "profit_harvest" {
				futuresBalance = 300
			}
			strategy, futures, spot := newFundingCarryBudgetStrategy(true, 0, futuresBalance, 500)
			strategy.strategySpotKnown = true
			if tt.caller == "profit_harvest" {
				strategy.profitHarvestEnabled = true
				strategy.profitHarvestMin = 10
			}
			store := newCommitConfirmedCanceledRuntimeStore(t, tt.mode)
			strategy.SetRuntimeStateStore(store)
			callerCtx, cancelCaller := context.WithCancel(context.WithValue(context.Background(), fundingCarryIntentContextKey{}, "caller-context"))
			store.expectedContext, store.cancelCaller = callerCtx, cancelCaller
			initialPayload, err := json.Marshal(strategy.runtimeStateSnapshotLocked())
			if err != nil {
				t.Fatal(err)
			}
			initial := &storage.StrategyRuntimeState{BotID: store.botID, StrategyName: "funding_carry", SchemaVersion: fundingCarryRuntimeStateVersion, Payload: string(initialPayload)}
			if err := store.db.SetFundingCarryRuntimeState(t.Context(), store.generation, initial); err != nil {
				t.Fatal("seed exact S0:", err)
			}

			var callErr error
			if tt.caller == "profit_harvest" {
				strategy.harvestProfitUnderWalletLock(callerCtx)
			} else {
				callErr = strategy.ensureFuturesMargin(callerCtx, 200, 0)
				if callErr == nil {
					t.Fatal("canceled pre-submit intent was reported as successful")
				}
			}
			if store.contextWriterCalls != 1 || !store.callerContextSeen || !errors.Is(callerCtx.Err(), context.Canceled) {
				t.Fatalf("caller context did not reach the storage writer and cancel after commit: calls=%d same_context=%v ctx_err=%v", store.contextWriterCalls, store.callerContextSeen, callerCtx.Err())
			}
			transferCalls := spot.transferCalls
			if tt.caller == "profit_harvest" {
				transferCalls = futures.transferCalls
			}
			if transferCalls != 0 {
				t.Fatalf("submitted %d transfer RPCs after beginRuntimeIntent failed", transferCalls)
			}
			var committed fundingCarryRuntimeState
			if err := json.Unmarshal([]byte(store.committedPayload), &committed); err != nil || !committed.IntentInFlight {
				t.Fatalf("fixture did not commit the pre-submit S1 intent: state=%+v err=%v", committed, err)
			}
			if (store.casCalls > 0) != tt.wantCAS {
				t.Fatalf("conditional rollback attempted=%v, want %v (calls=%d)", store.casCalls > 0, tt.wantCAS, store.casCalls)
			}
			if tt.wantRecovered {
				if strategy.intentInFlight || strategy.unownedExposure {
					t.Fatalf("confirmed pre-submit rollback left memory blocked: intent=%v unknown=%v", strategy.intentInFlight, strategy.unownedExposure)
				}
				if store.casCalls != 1 {
					t.Fatalf("conditional rollback calls=%d, want one", store.casCalls)
				}
				current, err := store.db.GetStrategyRuntimeStateContext(t.Context(), store.botID, "funding_carry")
				if err != nil || current == nil || current.SchemaVersion != initial.SchemaVersion || current.Payload != initial.Payload {
					t.Fatalf("durable state does not equal exact S0: state=%+v err=%v", current, err)
				}
				memoryPayload, err := json.Marshal(strategy.runtimeStateSnapshotLocked())
				if err != nil || string(memoryPayload) != initial.Payload {
					t.Fatalf("strategy memory differs from restored S0: payload=%s err=%v", memoryPayload, err)
				}
			} else {
				if !strategy.intentInFlight || !strategy.unownedExposure {
					t.Fatal("uncertain recovery did not remain fail-closed in memory")
				}
				if tt.mode == "owner_takeover" {
					current, err := store.db.GetStrategyRuntimeStateContext(t.Context(), store.botID, "funding_carry")
					if err != nil || current == nil || !strings.Contains(current.Payload, `"intent_in_flight":true`) {
						t.Fatalf("takeover lost durable pending intent: state=%+v err=%v", current, err)
					}
				}
				if tt.mode == "changed_snapshot" || tt.mode == "cas_conflict" {
					current, err := store.db.GetStrategyRuntimeStateContext(t.Context(), store.botID, "funding_carry")
					if err != nil || current == nil || current.Payload != store.conflictPayload {
						t.Fatalf("outer error handler overwrote conflicting durable evidence: state=%+v expected_payload=%q err=%v", current, store.conflictPayload, err)
					}
				}
			}
		})
	}
}

func TestFundingCarryDispatchMarkerUnknownCommitOutcomeRollsBackBeforeTransfer(t *testing.T) {
	strategy, _, spot := newFundingCarryBudgetStrategy(true, 0, 0, 500)
	strategy.symbol = "BTCUSDT"
	strategy.strategySpotKnown = true
	store := newCommitConfirmedCanceledRuntimeStore(t, "unknown_dispatch")
	strategy.SetRuntimeStateStore(store)

	err := strategy.ensureFuturesMargin(t.Context(), 200, 0)
	if err == nil || !errors.Is(err, storage.ErrFundingCarryRuntimeStateCommitOutcomeUnknown) {
		t.Fatalf("unknown dispatch-marker outcome was not returned: %v", err)
	}
	if spot.transferCalls != 0 {
		t.Fatalf("dispatch marker uncertainty submitted %d transfer RPCs", spot.transferCalls)
	}
	if strategy.intentInFlight || strategy.intentPhase != "" || strategy.unownedExposure {
		t.Fatalf("exact fenced rollback did not restore in-memory S0: in_flight=%v phase=%q unknown=%v", strategy.intentInFlight, strategy.intentPhase, strategy.unownedExposure)
	}
	current, err := store.db.GetStrategyRuntimeStateContext(t.Context(), store.botID, "funding_carry")
	if err != nil || current == nil {
		t.Fatalf("read dispatch rollback checkpoint: state=%+v err=%v", current, err)
	}
	var state fundingCarryRuntimeState
	if err := json.Unmarshal([]byte(current.Payload), &state); err != nil {
		t.Fatal(err)
	}
	if state.IntentInFlight || state.IntentPhase != "" || state.ExposureUnknown || store.casCalls != 1 {
		t.Fatalf("durable rollback is not exact S0: state=%+v CAS=%d", state, store.casCalls)
	}
}

func newCommitConfirmedCanceledRuntimeStore(t *testing.T, mode string) *commitConfirmedCanceledRuntimeStore {
	t.Helper()
	db, err := storage.NewSQLStorage(t.TempDir() + "/funding-carry-intent-cancel.db")
	if err != nil {
		t.Fatal("open isolated SQLite storage:", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	generation, err := db.ClaimFundingCarryRuntimeGeneration(t.Context(), []string{fmt.Sprintf("%064x", 1)})
	if err != nil {
		t.Fatal("claim runtime generation:", err)
	}
	return &commitConfirmedCanceledRuntimeStore{db: db, generation: generation, mode: mode, botID: "funding-carry-intent-cancel-test"}
}

type fundingCarryIntentContextKey struct{}
