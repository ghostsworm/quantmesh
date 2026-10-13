package execution

import (
	"context"
	"errors"
	"reflect"
	"sort"
	"sync"
	"testing"
	"time"

	"quantmesh/accounting"
)

func TestOpeningGateSourcesAndDrain(t *testing.T) {
	var gate OpeningGate
	release, err := gate.Begin()
	if err != nil {
		t.Fatal(err)
	}
	gate.Block("manual")
	gate.Block("market")
	gate.Unblock("market")
	if !gate.Blocked() {
		t.Fatal("market recovery cleared manual pause")
	}
	if _, err := gate.Begin(); !errors.Is(err, ErrOpeningPaused) {
		t.Fatalf("blocked admission: %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := gate.Drain(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("in-flight order must prevent drain: %v", err)
	}
	release()
	release() // idempotent even with duplicate caller cleanup
	if err := gate.Drain(t.Context()); err != nil {
		t.Fatal(err)
	}
	gate.Unblock("manual")
	release, err = gate.Begin()
	if err != nil {
		t.Fatal(err)
	}
	release()
}

func TestOpeningGateRejectsZeroValuePhysicalLease(t *testing.T) {
	var gate OpeningGate
	if _, err := gate.AuthorizeOpeningSubmission(OpeningLease{}); !errors.Is(err, ErrOpeningPaused) {
		t.Fatalf("zero-value lease authorized a physical submission: %v", err)
	}
}

func TestOpeningGateBlockRevokesQueuedLeaseBeforePhysicalSubmit(t *testing.T) {
	var gate OpeningGate
	lease, err := gate.BeginLease()
	if err != nil {
		t.Fatal(err)
	}
	gate.Block("manual")
	if _, err := gate.AuthorizeOpeningSubmission(lease); !errors.Is(err, ErrOpeningPaused) {
		t.Fatalf("queued lease survived a pause: %v", err)
	}
	gate.ReleaseLease(lease.ID)
	if err := gate.Drain(t.Context()); err != nil {
		t.Fatalf("revoked lease prevented drain: %v", err)
	}
}

func TestOpeningGateBlockLinearizesAgainstPhysicalSubmit(t *testing.T) {
	var gate OpeningGate
	lease, err := gate.BeginLease()
	if err != nil {
		t.Fatal(err)
	}
	started := make(chan struct{})
	type authorizationResult struct {
		finish func()
		err    error
	}
	finish := make(chan authorizationResult, 1)
	go func() {
		close(started)
		releaseAttempt, authErr := gate.AuthorizeOpeningSubmission(lease)
		finish <- authorizationResult{finish: releaseAttempt, err: authErr}
	}()
	<-started
	gate.Block("risk")
	result := <-finish
	if result.err == nil {
		// If authorization won the gate mutex first, this in-flight attempt is
		// allowed to finish, but Drain must continue waiting for its lease.
		canceled, cancel := context.WithCancel(t.Context())
		cancel()
		if err := gate.Drain(canceled); !errors.Is(err, context.Canceled) {
			t.Fatalf("drain passed while an authorized physical attempt was still active: %v", err)
		}
		result.finish()
	} else if !errors.Is(result.err, ErrOpeningPaused) {
		t.Fatalf("race produced an unexpected authorization error: %v", result.err)
	}
	gate.ReleaseLease(lease.ID)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := gate.Drain(ctx); err != nil {
		t.Fatalf("drain did not settle after authorized attempt ended: %v", err)
	}
}

func TestOpeningGateRecoveryHoldRevokesOrdinaryLeasesAndScopesPermit(t *testing.T) {
	var gate OpeningGate
	ordinary, err := gate.BeginLease()
	if err != nil {
		t.Fatal(err)
	}
	permit := accounting.OneShotPermit{
		ID: "permit-1", EpisodeID: "episode-1", Generation: 1, ActionID: "action-1",
		ActionOrdinal: 1, Kind: accounting.RecoveryActionBorrow, AccountScope: "acct-1", OwnerGeneration: "owner-1",
		LeaseID: 2, ExpectedRevision: 1, State: accounting.PermitAuthorized,
	}
	recovery, err := gate.BeginRecoveryActionWithHold(permit)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := gate.AuthorizeOpeningSubmission(ordinary); !errors.Is(err, ErrOpeningPaused) {
		t.Fatalf("episode hold failed to revoke ordinary lease: %v", err)
	}
	consumed := permit
	consumed.State = accounting.PermitConsumed
	if err := gate.ConsumeEpisodeActionLease(recovery, consumed); err != nil {
		t.Fatal(err)
	}
	finishAttempt, err := gate.AuthorizeOpeningSubmission(recovery)
	if err != nil {
		t.Fatalf("exact recovery action did not pass its own hold: %v", err)
	}
	finishAttempt()
	if _, err := gate.AuthorizeOpeningSubmission(recovery); !errors.Is(err, ErrOpeningPaused) {
		t.Fatalf("one-shot episode action was reusable: %v", err)
	}
	if _, err := gate.BeginRecoveryActionWithHold(permit); !errors.Is(err, ErrOpeningPaused) {
		t.Fatalf("same durable permit created a second lease: %v", err)
	}
	gate.ReleaseLease(ordinary.ID)
	gate.ReleaseLease(recovery.ID)
	if _, err := gate.BeginRecoveryActionWithHold(permit); !errors.Is(err, ErrOpeningPaused) {
		t.Fatalf("released lease made a consumed permit reusable: %v", err)
	}
}

func TestOpeningGateRecoveryLeaseMustMatchDurableLeaseID(t *testing.T) {
	var gate OpeningGate
	permit := accounting.OneShotPermit{
		ID: "permit-mismatch", EpisodeID: "episode-mismatch", Generation: 1, ActionID: "action-mismatch",
		ActionOrdinal: 1, Kind: accounting.RecoveryActionBorrow, AccountScope: "acct-1", OwnerGeneration: "owner-1",
		LeaseID: 1, ExpectedRevision: 1, State: accounting.PermitAuthorized,
	}
	lease, err := gate.BeginRecoveryActionWithHold(permit)
	if err != nil {
		t.Fatal(err)
	}
	consumed := permit
	consumed.State = accounting.PermitConsumed
	consumed.LeaseID++
	if err := gate.ConsumeEpisodeActionLease(lease, consumed); !errors.Is(err, ErrOpeningPaused) {
		t.Fatalf("permit with a different durable lease ID was consumed: %v", err)
	}
	gate.ReleaseLease(lease.ID)
}

func TestOpeningGateRecoveryPermitAllowsOnlyExactPersistedSourceSet(t *testing.T) {
	const intentRecovery = "execution_intent_recovery"
	const episodeExposure = "funding_carry_exposure_unverified"
	const reconciliationRequired = "funding_carry_reconciliation_required"
	permit := accounting.OneShotPermit{
		ID: "permit-source-set", EpisodeID: "episode-source-set", Generation: 2, ActionID: "action-source-set",
		ActionOrdinal: 1, Kind: accounting.RecoveryActionBorrow, AccountScope: "acct-1", OwnerGeneration: "owner-1",
		LeaseID: 7, ExpectedRevision: 3, State: accounting.PermitAuthorized,
		GateSourceExceptions: []string{intentRecovery, episodeExposure, reconciliationRequired},
	}
	var gate OpeningGate
	gate.Block(intentRecovery)
	gate.Block(episodeExposure)
	gate.Block(reconciliationRequired)
	lease, err := gate.BeginRecoveryActionWithHold(permit)
	if err != nil {
		t.Fatalf("approved exact source set was rejected: %v", err)
	}
	consumed := permit
	consumed.State = accounting.PermitConsumed
	if err := gate.ConsumeEpisodeActionLease(lease, consumed); err != nil {
		t.Fatalf("exact persisted source set did not consume: %v", err)
	}

	// A newly-arriving independent source between durable consume and RPC must
	// deny submission, and the gate must retain every source unchanged.
	gate.Block("manual")
	if _, err := gate.AuthorizeOpeningSubmission(lease); !errors.Is(err, ErrOpeningPaused) {
		t.Fatalf("late manual source did not deny the RPC: %v", err)
	}
	want := []string{episodeExposure, intentRecovery, reconciliationRequired, "manual", RecoveryEpisodeGateSource(permit.EpisodeID, permit.Generation)}
	sort.Strings(want)
	if got := gate.Sources(); !reflect.DeepEqual(got, want) {
		t.Fatalf("permit path changed gate sources: got %v, want %v", got, want)
	}
}

func TestOpeningGateRecoveryPermitRequiresEveryPersistedExceptionSource(t *testing.T) {
	permit := accounting.OneShotPermit{
		ID: "permit-missing-source", EpisodeID: "episode-missing-source", Generation: 1, ActionID: "action-missing-source",
		ActionOrdinal: 1, Kind: accounting.RecoveryActionBorrow, AccountScope: "acct-1", OwnerGeneration: "owner-1",
		LeaseID: 1, ExpectedRevision: 1, State: accounting.PermitAuthorized,
		GateSourceExceptions: []string{"execution_intent_recovery"},
	}
	var gate OpeningGate
	if _, err := gate.BeginRecoveryActionWithHold(permit); !errors.Is(err, ErrOpeningPaused) {
		t.Fatalf("permit proceeded while one persisted exception source was absent: %v", err)
	}
	if want, got := RecoveryEpisodeGateSource(permit.EpisodeID, permit.Generation), gate.Sources(); len(got) != 1 || got[0] != want {
		t.Fatalf("failed admission did not retain only its recovery hold: got %v, want [%s]", got, want)
	}
}

func TestOpeningGateRecoveryPermitCannotBypassUnlistedOrUnsafeSource(t *testing.T) {
	permit := accounting.OneShotPermit{
		ID: "permit-no-bypass", EpisodeID: "episode-no-bypass", Generation: 1, ActionID: "action-no-bypass",
		ActionOrdinal: 1, Kind: accounting.RecoveryActionBorrow, AccountScope: "acct-1", OwnerGeneration: "owner-1",
		LeaseID: 1, ExpectedRevision: 1, State: accounting.PermitAuthorized,
		GateSourceExceptions: []string{"execution_intent_recovery"},
	}
	for _, source := range []string{"manual", "risk", "unknown_orders"} {
		t.Run(source, func(t *testing.T) {
			var gate OpeningGate
			gate.Block("execution_intent_recovery")
			gate.Block(source)
			before := gate.Sources()
			if _, err := gate.BeginRecoveryActionWithHold(permit); !errors.Is(err, ErrOpeningPaused) {
				t.Fatalf("unlisted source %q did not deny permit: %v", source, err)
			}
			if got := gate.Sources(); !reflect.DeepEqual(got, before) {
				t.Fatalf("denied permit changed sources: got %v, want %v", got, before)
			}
		})
	}
	permit.GateSourceExceptions = []string{"manual"}
	if err := permit.Validate(); err == nil {
		t.Fatal("unsafe source was accepted in durable permit")
	}
}

func TestOpeningGateExistingEpisodeActionPermitUsesPersistedSourceSet(t *testing.T) {
	const episode = "recovery_episode:episode-existing:3"
	permit := accounting.OneShotPermit{
		ID: "permit-existing", EpisodeID: "episode-existing", Generation: 3, ActionID: "action-2",
		ActionOrdinal: 2, Kind: accounting.RecoveryActionBorrow, AccountScope: "acct-1", OwnerGeneration: "owner-1",
		LeaseID: 9, ExpectedRevision: 4, State: accounting.PermitAuthorized,
		GateSourceExceptions: []string{"execution_intent_recovery"},
	}
	var gate OpeningGate
	gate.Block(episode)
	gate.Block("execution_intent_recovery")
	lease, err := gate.BeginWithEpisodeActionPermit(permit)
	if err != nil {
		t.Fatalf("exact durable source set did not admit next action: %v", err)
	}
	consumed := permit
	consumed.State = accounting.PermitConsumed
	if err := gate.ConsumeEpisodeActionLease(lease, consumed); err != nil {
		t.Fatalf("durable source set did not pass consume check: %v", err)
	}
	finish, err := gate.AuthorizeOpeningSubmission(lease)
	if err != nil {
		t.Fatalf("durable source set did not pass pre-RPC check: %v", err)
	}
	finish()
}

func TestOpeningGateOperationLeaseOnlyExemptsPairedLedgerSource(t *testing.T) {
	var gate OpeningGate
	operation, err := gate.BeginOperationLease()
	if err != nil {
		t.Fatal(err)
	}
	if err := gate.BlockForOperation(PairedExecutionLedgerGateSource, operation); err != nil {
		t.Fatal(err)
	}
	if err := gate.BlockForOperation("manual", operation); !errors.Is(err, ErrOpeningPaused) {
		t.Fatalf("operation lease bound a non-ledger hold: %v", err)
	}
	child, err := gate.BeginChildOpeningLease(operation)
	if err != nil {
		t.Fatalf("admitted pair could not obtain its scoped child lease: %v", err)
	}
	finish, err := gate.AuthorizeOpeningSubmission(child)
	if err != nil {
		t.Fatalf("admitted pair could not finish under its ledger hold: %v", err)
	}
	finish()
	gate.ReleaseLease(child.ID)
	gate.Block("manual")
	if _, err := gate.BeginChildOpeningLease(operation); !errors.Is(err, ErrOpeningPaused) {
		t.Fatalf("pair bypassed an independent manual hold: %v", err)
	}
	gate.ReleaseLease(operation.ID)
}

func TestOpeningGateConcurrentAdmissionsAndPause(t *testing.T) {
	var gate OpeningGate
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if release, err := gate.Begin(); err == nil {
				release()
			} else if !errors.Is(err, ErrOpeningPaused) {
				t.Error(err)
			}
		}()
	}
	gate.Block("risk")
	wg.Wait()
	if err := gate.Drain(t.Context()); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		if _, err := gate.Begin(); !errors.Is(err, ErrOpeningPaused) {
			t.Fatalf("post-pause admission: %v", err)
		}
	}
}
