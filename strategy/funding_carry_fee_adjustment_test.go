package strategy

import (
	"testing"

	"quantmesh/accounting"
	"quantmesh/exchange"
	"quantmesh/execution"
)

func TestFundingCarryManualFeeAdjustmentCASReplayAndCorrectionKeepsHold(t *testing.T) {
	fill := validFundingRecoveryFill("13", "0.1", false)
	fixture := newFundingRecoveryFillFixture(t, []*exchange.OrderFill{fill}, true, false, true)
	fillPreview, err := fixture.strategy.PreviewFundingRecoveryFills(fixture.ctx, fixture.episode.ID, fixture.child.ID)
	if err != nil {
		t.Fatal(err)
	}
	fillApply := ApplyFundingRecoveryFillsRequest{EpisodeID: fixture.episode.ID, ChildID: fixture.child.ID, OperationID: "fee-fill-op-171",
		ExpectedEpisodeRevision: fillPreview.EpisodeRevision, ExpectedChildRevision: fillPreview.ChildRevision,
		PreviewSourceRevision: fillPreview.Batch.SourceRevision, Actor: "operator", AuditRef: "incident-fee-171", Confirmed: true}
	if result, err := fixture.strategy.ApplyFundingRecoveryFills(fixture.ctx, fillApply); err != nil || result.FeesComplete || !result.ManualFeeAdjustmentRequired || !result.HoldRetained {
		t.Fatalf("unknown-fee fill apply should remain held and request manual fee evidence: result=%+v err=%v", result, err)
	}
	feePreview, err := fixture.strategy.PreviewFundingRecoveryFeeAdjustment(fixture.ctx, fixture.episode.ID, fixture.child.ID, "13")
	if err != nil || feePreview.FillEvent.FillEvidence == nil || feePreview.FillEvent.FillEvidence.CommissionKnown || feePreview.SourceRevision != fillPreview.Batch.SourceRevision {
		t.Fatalf("unknown fee preview mismatch: preview=%+v err=%v", feePreview, err)
	}
	// Positive deltas add fee cost; negative deltas append a correction/rebate.
	request := ApplyFundingRecoveryFeeAdjustmentRequest{EpisodeID: fixture.episode.ID, ChildID: fixture.child.ID, TradeID: "13",
		OperationID: "fee-adjustment-op-171", AdjustmentID: "statement-line-171", AmountDeltaRaw: "0.0001200", Asset: "bnb",
		EvidenceHash: "0000000000000000000000000000000000000000000000000000000000000171", SourceRevision: feePreview.SourceRevision,
		ExpectedEpisodeRevision: feePreview.EpisodeRevision, ExpectedChildRevision: feePreview.ChildRevision,
		Actor: "operator", AuditRef: "incident-fee-171", Confirmed: true}
	result, err := fixture.strategy.ApplyFundingRecoveryFeeAdjustment(fixture.ctx, request)
	if err != nil || !result.Projected || !result.FeesComplete || len(result.UncoveredFeeTradeIDs) != 0 || !result.HoldRetained {
		t.Fatalf("manual fee adjustment did not persist/read back while held: result=%+v err=%v", result, err)
	}
	if !fixture.gate.HasBlock(execution.RecoveryEpisodeGateSource(fixture.episode.ID, fixture.episode.RecoveryGeneration)) {
		t.Fatal("manual fee adjustment released the recovery Gate source")
	}
	events, err := fixture.store.ListRecoveryEvents(fixture.ctx, fixture.episode.ID)
	if err != nil {
		t.Fatal(err)
	}
	var feeEvent *accounting.RecoveryEvent
	for index := range events {
		if events[index].Kind == accounting.RecoveryFeeEvent {
			feeEvent = &events[index]
		}
	}
	if len(events) != 2 || feeEvent == nil || feeEvent.RelatedExternalID != "13" || feeEvent.Amount != "0.0001200" ||
		feeEvent.Asset != "BNB" || feeEvent.SourceHash != request.EvidenceHash || feeEvent.CursorSequence != 0 {
		t.Fatalf("raw fee adjustment was not persisted as an independent linked event: %+v", events)
	}
	replay, err := fixture.strategy.ApplyFundingRecoveryFeeAdjustment(fixture.ctx, request)
	if err != nil || !replay.Replayed || !replay.Projected || !replay.HoldRetained || lenMustRecoveryEvents(t, fixture) != 2 {
		t.Fatalf("same fee operation did not replay idempotently: result=%+v err=%v", replay, err)
	}
	conflicting := request
	conflicting.AmountDeltaRaw = "-0.00013"
	if _, err := fixture.strategy.ApplyFundingRecoveryFeeAdjustment(fixture.ctx, conflicting); err == nil {
		t.Fatal("same operation ID accepted different fee facts")
	}

	correctionPreview, err := fixture.strategy.PreviewFundingRecoveryFeeAdjustment(fixture.ctx, fixture.episode.ID, fixture.child.ID, "13")
	if err != nil || len(correctionPreview.ExistingAdjustments) != 1 {
		t.Fatalf("fee correction preview failed: preview=%+v err=%v", correctionPreview, err)
	}
	correction := request
	correction.OperationID = "fee-correction-op-171"
	correction.AdjustmentID = "statement-correction-171"
	correction.AmountDeltaRaw = "-0.00000001"
	correction.EvidenceHash = "0000000000000000000000000000000000000000000000000000000000000172"
	correction.SourceRevision = correctionPreview.SourceRevision
	correction.ExpectedEpisodeRevision = correctionPreview.EpisodeRevision
	correction.ExpectedChildRevision = correctionPreview.ChildRevision
	corrected, err := fixture.strategy.ApplyFundingRecoveryFeeAdjustment(fixture.ctx, correction)
	if err != nil || !corrected.Projected || !corrected.FeesComplete || !corrected.HoldRetained || lenMustRecoveryEvents(t, fixture) != 3 {
		t.Fatalf("append-only fee correction failed: result=%+v err=%v", corrected, err)
	}
}

func lenMustRecoveryEvents(t *testing.T, fixture fundingRecoveryFillFixture) int {
	t.Helper()
	events, err := fixture.store.ListRecoveryEvents(fixture.ctx, fixture.episode.ID)
	if err != nil {
		t.Fatal(err)
	}
	return len(events)
}

func TestFundingCarryManualFeePreviewRefusesWhenVenueFeeBecomesKnown(t *testing.T) {
	fill := validFundingRecoveryFill("14", "0.1", false)
	fixture := newFundingRecoveryFillFixture(t, []*exchange.OrderFill{fill}, true, false, true)
	fillPreview, err := fixture.strategy.PreviewFundingRecoveryFills(fixture.ctx, fixture.episode.ID, fixture.child.ID)
	if err != nil {
		t.Fatal(err)
	}
	request := ApplyFundingRecoveryFillsRequest{EpisodeID: fixture.episode.ID, ChildID: fixture.child.ID, OperationID: "fee-fill-op-172",
		ExpectedEpisodeRevision: fillPreview.EpisodeRevision, ExpectedChildRevision: fillPreview.ChildRevision,
		PreviewSourceRevision: fillPreview.Batch.SourceRevision, Actor: "operator", AuditRef: "incident-fee-172", Confirmed: true}
	if _, err := fixture.strategy.ApplyFundingRecoveryFills(fixture.ctx, request); err != nil {
		t.Fatal(err)
	}
	fixture.venue.batch.Fills[0].CommissionKnown = true
	fixture.venue.batch.Fills[0].CommissionRaw = "0.0001"
	fixture.venue.batch.Fills[0].CommissionAsset = "USDT"
	if _, err := fixture.strategy.PreviewFundingRecoveryFeeAdjustment(fixture.ctx, fixture.episode.ID, fixture.child.ID, "14"); err == nil {
		t.Fatal("manual fee preview accepted after authoritative venue fee became available")
	}
}

func TestFundingCarryManualFeeApplyRefusesWhenVenueFeeBecomesKnown(t *testing.T) {
	fill := validFundingRecoveryFill("15", "0.1", false)
	fixture := newFundingRecoveryFillFixture(t, []*exchange.OrderFill{fill}, true, false, true)
	fillPreview, err := fixture.strategy.PreviewFundingRecoveryFills(fixture.ctx, fixture.episode.ID, fixture.child.ID)
	if err != nil {
		t.Fatal(err)
	}
	fillApply := ApplyFundingRecoveryFillsRequest{EpisodeID: fixture.episode.ID, ChildID: fixture.child.ID, OperationID: "fee-fill-op-173",
		ExpectedEpisodeRevision: fillPreview.EpisodeRevision, ExpectedChildRevision: fillPreview.ChildRevision,
		PreviewSourceRevision: fillPreview.Batch.SourceRevision, Actor: "operator", AuditRef: "incident-fee-173", Confirmed: true}
	if _, err := fixture.strategy.ApplyFundingRecoveryFills(fixture.ctx, fillApply); err != nil {
		t.Fatal(err)
	}
	feePreview, err := fixture.strategy.PreviewFundingRecoveryFeeAdjustment(fixture.ctx, fixture.episode.ID, fixture.child.ID, "15")
	if err != nil {
		t.Fatal(err)
	}
	request := ApplyFundingRecoveryFeeAdjustmentRequest{EpisodeID: fixture.episode.ID, ChildID: fixture.child.ID, TradeID: "15",
		OperationID: "fee-adjustment-op-173", AdjustmentID: "statement-line-173", AmountDeltaRaw: "0.0001", Asset: "USDT",
		EvidenceHash: "0000000000000000000000000000000000000000000000000000000000000173", SourceRevision: feePreview.SourceRevision,
		ExpectedEpisodeRevision: feePreview.EpisodeRevision, ExpectedChildRevision: feePreview.ChildRevision,
		Actor: "operator", AuditRef: "incident-fee-173", Confirmed: true}
	fixture.venue.batch.Fills[0].CommissionKnown = true
	fixture.venue.batch.Fills[0].CommissionRaw = "0.0001"
	fixture.venue.batch.Fills[0].CommissionAsset = "USDT"
	if _, err := fixture.strategy.ApplyFundingRecoveryFeeAdjustment(fixture.ctx, request); err == nil {
		t.Fatal("manual fee apply accepted after authoritative venue fee became available")
	}
	if lenMustRecoveryEvents(t, fixture) != 1 {
		t.Fatal("rejected manual fee apply wrote a fee event")
	}
}
