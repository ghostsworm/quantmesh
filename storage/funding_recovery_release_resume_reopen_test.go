package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"quantmesh/accounting"
)

func TestSQLiteFundingRecoveryReleaseResumeReopenPreservesOwnerHistory(t *testing.T) {
	f := prepareReleaseResumeFixture(t, newSQLiteReleaseResumeStores(t), false)
	store, ctx := f.stores[0], t.Context()
	episode, _, _, _, err := store.ResumeRecoveryRelease(ctx, f.owner, f.auth)
	if err != nil {
		t.Fatal(err)
	}
	owner := f.owner
	for cycle := 1; cycle <= 2; cycle++ {
		late := accounting.RecoveryEvent{ID: fmt.Sprintf("late-fee-%d", cycle), Source: "venue_late_fee", Exchange: "binance", Market: "futures",
			AccountScope: f.auth.AccountScopes[0], Kind: accounting.RecoveryFeeEvent, ExternalID: fmt.Sprintf("late-external-%d", cycle),
			EpisodeID: episode.ID, Asset: "USDT", Amount: "-0.01", SourceRevision: "1", SourceHash: strings.Repeat("c", 64), ObservedAt: time.Now().UTC()}
		hold, reopenErr := store.ReopenRecoveryEpisode(ctx, owner, episode.ID, episode.Revision, late, fmt.Sprintf("late-reopen-%d", cycle))
		if reopenErr != nil {
			t.Fatal("reopen resumed episode:", reopenErr)
		}
		episode, _, err = store.GetRecoveryEpisode(ctx, episode.ID)
		if err != nil || episode.Validate() != nil || !episode.KnowsOwnerGeneration(f.oldOwner.ID()) || !episode.KnowsOwnerGeneration(owner.ID()) || episode.KnowsOwnerGeneration("unknown-owner") {
			t.Fatalf("reopened owner history: episode=%+v err=%v validate=%v", episode, err, episode.Validate())
		}
		// The old release's historical receipt cannot clear a new generation.
		if _, _, _, _, err = store.ResumeRecoveryRelease(ctx, owner, f.auth); err == nil {
			t.Fatal("old resume crossed reopened generation")
		}
		permit := accounting.OneShotPermit{ID: fmt.Sprintf("no-new-permit-%d", cycle), EpisodeID: episode.ID, Generation: episode.RecoveryGeneration,
			ActionID: "unused-action", ActionOrdinal: 1, Kind: accounting.RecoveryActionBorrow, AccountScope: f.auth.AccountScopes[0], OwnerGeneration: owner.ID(),
			LeaseID: uint64(cycle + 1), ExpectedRevision: episode.Revision, State: accounting.PermitAuthorized}
		if _, err = store.CreateRecoveryPermit(ctx, owner, permit); !errors.Is(err, accounting.ErrRecoveryConflict) {
			t.Fatalf("reopened resume allowed execution permit: %v", err)
		}
		// Even an already stored authorization cannot be consumed after a resume.
		raw, marshalErr := json.Marshal(permit)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		if _, err = store.db.ExecContext(ctx, `INSERT INTO funding_recovery_permits(permit_id,episode_id,recovery_generation,action_id,lease_id,state,payload_json) VALUES(?,?,?,?,?,?,?)`,
			permit.ID, episode.ID, permit.Generation, permit.ActionID, permit.LeaseID, string(permit.State), raw); err != nil {
			t.Fatal(err)
		}
		if _, maySubmit, consumeErr := store.ConsumeRecoveryPermit(ctx, owner, permit); !errors.Is(consumeErr, accounting.ErrRecoveryConflict) || maySubmit {
			t.Fatalf("reopened resume consumed execution permit: maySubmit=%v err=%v", maySubmit, consumeErr)
		}
		nextOwner, claimErr := f.stores[1].ClaimFundingCarryRuntimeGeneration(ctx, f.auth.AccountScopes)
		if claimErr != nil {
			t.Fatal(claimErr)
		}
		episode, _, err = store.TakeOverRecoveryEpisode(ctx, nextOwner, accounting.RecoveryOwnerTakeoverAuthorization{EpisodeID: episode.ID,
			OperationID: fmt.Sprintf("takeover-reopened-%d", cycle), Actor: "reopen-admin", AuditRef: "reopen-audit", ExpectedOwnerGeneration: owner.ID(),
			ExpectedEpisodeRevision: episode.Revision, Confirmed: true})
		if err != nil || episode.Validate() != nil {
			t.Fatalf("ordinary takeover after resume/reopen: %v validation=%v", err, episode.Validate())
		}
		owner = nextOwner
		from := time.Date(2026, 10, 20, cycle, 0, 0, 0, time.UTC)
		window := sourceWindowForSymbol(f.auth.AccountScopes[0], "BTCUSDT", from, from.Add(time.Hour), uint64(cycle), 1)
		op := fundingOperation(episode, f.auth.AccountScopes[0], fmt.Sprintf("account-new-generation-%d", cycle), episode.Revision, window, fmt.Sprintf("new-income-%d", cycle), "-1.25")
		if _, _, err = store.ApplyRecoveryOperation(ctx, owner, op); err != nil {
			t.Fatal("account reopened generation:", err)
		}
		outbox, _, readErr := store.GetRecoveryOutbox(ctx, episode.ID, op.OperationID)
		if readErr != nil {
			t.Fatal(readErr)
		}
		outbox.State, outbox.AppliedOperationID, outbox.AppliedRevision, outbox.AppliedHash = accounting.RecoveryProjected, outbox.OperationID, outbox.TargetEpisodeRevision, outbox.ProjectionHash
		if err = store.MarkRecoveryProjected(ctx, owner, outbox); err != nil {
			t.Fatal(err)
		}
		episode, _, err = store.GetRecoveryEpisode(ctx, episode.ID)
		if err != nil {
			t.Fatal(err)
		}
		release := f.release
		release.OperationID = fmt.Sprintf("release-new-generation-%d", cycle)
		release.OwnerGeneration, release.ExpectedEpisodeRevision, release.RecoveryGeneration = owner.ID(), episode.Revision, episode.RecoveryGeneration
		release.ExpectedHoldRevision, release.ProjectionHash, release.RuntimeRevision = hold.Revision, outbox.ProjectionHash, outbox.ExpectedRuntimeRevision
		if _, err = store.PrepareRecoveryRelease(ctx, owner, release); err != nil {
			t.Fatal("prepare next generation release:", err)
		}
		if cycle == 2 {
			if _, err = store.ReleaseRecoveryHold(ctx, owner, release); err != nil {
				t.Fatal(err)
			}
		}
		newOwner, claimErr := store.ClaimFundingCarryRuntimeGeneration(ctx, f.auth.AccountScopes)
		if claimErr != nil {
			t.Fatal(claimErr)
		}
		preview, previewErr := store.PreviewRecoveryReleaseResume(ctx, newOwner, episode.ID, release.OperationID)
		if previewErr != nil {
			t.Fatal("preview current generation with historical resume:", previewErr)
		}
		newAuth := releaseResumeAuthFromPreview(preview, fmt.Sprintf("resume-new-generation-%d", cycle))
		episode, _, _, _, err = store.ResumeRecoveryRelease(ctx, newOwner, newAuth)
		if err != nil {
			t.Fatal("resume new generation:", err)
		}
		if len(episode.ReleaseResumptions) != cycle+1 || len(episode.OwnerTakeovers) != cycle || episode.ValidateOwnerHistory() != nil {
			t.Fatalf("lost interleaved owner history: %+v", episode)
		}
		if _, _, _, replayed, retryErr := store.ResumeRecoveryRelease(ctx, newOwner, newAuth); retryErr != nil || !replayed {
			t.Fatalf("retry new generation: replay=%v err=%v", replayed, retryErr)
		}
		if _, _, _, _, err = store.ResumeRecoveryRelease(ctx, newOwner, f.auth); err == nil {
			t.Fatal("historical resume accepted after new generation release")
		}
		owner = newOwner
	}
	for _, mutate := range []func(*accounting.RecoveryEpisode){
		func(e *accounting.RecoveryEpisode) { e.OwnerTakeovers[0].PreviousOwnerGeneration = "unknown" },
		func(e *accounting.RecoveryEpisode) { e.ReleaseResumptions[1].PreviousOwnerGeneration = "unknown" },
		func(e *accounting.RecoveryEpisode) {
			e.ReleaseResumptions[1].EpisodeRevision = e.OwnerTakeovers[0].EpisodeRevision
		},
		func(e *accounting.RecoveryEpisode) { e.OwnerGeneration = "unknown" },
	} {
		raw, marshalErr := json.Marshal(episode)
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		var invalid accounting.RecoveryEpisode
		if err = json.Unmarshal(raw, &invalid); err != nil {
			t.Fatal(err)
		}
		mutate(&invalid)
		if invalid.ValidateOwnerHistory() == nil || invalid.KnowsOwnerGeneration(f.oldOwner.ID()) {
			t.Fatal("accepted disconnected owner history")
		}
	}
}
