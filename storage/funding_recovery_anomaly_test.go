package storage

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"quantmesh/accounting"
)

func TestSQLiteRecoverySourceAnomalyCaptureIsFencedDurableAndIdempotent(t *testing.T) {
	store, generation, episode, scope := newSourceCursorFixture(t, "episode-source-anomaly")
	ctx := context.Background()
	from := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	window := sourceWindow(scope, from, from.Add(time.Hour), 0, 1)
	window.Source = accounting.RecoverySourceBinanceFuturesIncomeV1
	window.InitialFrom = from
	window.HistoricalEvidenceHash = strings.Repeat("c", 64)
	op := fundingOperation(episode, scope, "source-anomaly-seed", 1, window, "transaction-1", "-1.25")
	if _, replayed, err := store.ApplyRecoveryOperation(ctx, generation, op); err != nil || replayed {
		t.Fatalf("seed accepted source cursor: replayed=%t err=%v", replayed, err)
	}
	projectSourceCursorOperation(t, store, generation, episode.ID, op.OperationID)
	original := op.Events[0]
	observed := original
	observed.ID, observed.Amount, observed.SourceRevision, observed.SourceHash = "observed-event", "-1.5", "late-source-revision", strings.Repeat("d", 64)
	identity := window.Identity()
	delta, err := accounting.RecoveryFundingAmountDelta(&original, &observed)
	if err != nil {
		t.Fatal("calculate exact anomaly delta:", err)
	}
	anomaly := accounting.RecoverySourceAnomaly{EpisodeID: episode.ID, RecoveryGeneration: episode.RecoveryGeneration, EpisodeRevision: 3,
		Identity: identity, CursorRevision: 1, InitialFrom: from, Through: window.Through,
		ExpectedHistoryHash: window.HistoricalEvidenceHash, ObservedHistoryHash: strings.Repeat("e", 64),
		Facts: []accounting.RecoverySourceAnomalyFact{{Kind: accounting.RecoverySourceAnomalyChanged, ExternalID: original.ExternalID,
			OriginalEvent: &original, ObservedEvent: &observed, DeltaAmount: delta}},
		Revision: 1, State: accounting.RecoverySourceAnomalyOpen, DetectedActor: "auditor", DetectedAuditRef: "ticket-anomaly-1", DetectedAt: from.Add(2 * time.Hour)}
	anomaly.ID, err = anomaly.Fingerprint()
	if err != nil {
		t.Fatal("fingerprint source anomaly:", err)
	}
	saved, replayed, err := store.RecordRecoverySourceAnomaly(ctx, generation, anomaly)
	if err != nil || replayed || saved.ID != anomaly.ID {
		t.Fatalf("record source anomaly = %+v replayed=%t err=%v", saved, replayed, err)
	}
	duplicate := anomaly
	duplicate.DetectedActor, duplicate.DetectedAuditRef, duplicate.DetectedAt = "other-auditor", "ticket-retry", from.Add(3*time.Hour)
	if got, replayed, err := store.RecordRecoverySourceAnomaly(ctx, generation, duplicate); err != nil || !replayed || got.DetectedAuditRef != anomaly.DetectedAuditRef {
		t.Fatalf("same complete scan did not return its first durable observation: anomaly=%+v replayed=%t err=%v", got, replayed, err)
	}
	readback, found, err := store.GetRecoverySourceAnomaly(ctx, episode.ID, anomaly.ID)
	if err != nil || !found || readback.State != accounting.RecoverySourceAnomalyOpen || readback.Facts[0].DeltaAmount != "-0.25" {
		t.Fatalf("durable anomaly readback = %+v found=%t err=%v", readback, found, err)
	}
	assertRecoverySourceAnomalyBlocksRelease(t, store, generation, episode.ID, op.OperationID)
	adjustmentEvidence := accounting.RecoveryFundingAdjustmentEvidence{AnomalyID: anomaly.ID, OriginalExternalID: original.ExternalID,
		OriginalSourceHash: original.SourceHash, ObservedSourceHash: observed.SourceHash, OriginalAmountRaw: original.Amount,
		ObservedAmountRaw: observed.Amount, ObservedRowAt: observed.ObservedAt, DeltaAmountRaw: "-0.25", EvidenceHash: strings.Repeat("f", 64),
		Actor: "auditor", AuditRef: "ticket-adjustment-1", ConfirmedAt: from.Add(4 * time.Hour)}
	adjustment := accounting.RecoveryEvent{ID: "manual-funding-adjustment-1", Source: accounting.RecoveryManualFundingAdjustmentSource,
		Exchange: identity.Exchange, Market: identity.Market, AccountScope: identity.AccountScope, Symbol: identity.Symbol, Kind: accounting.RecoveryFundingEvent,
		ExternalID: "adjustment:" + anomaly.ID, RelatedExternalID: original.ExternalID, EpisodeID: episode.ID, Asset: identity.Asset,
		Amount: "-0.25", SourceRevision: anomaly.ID + ":1", SourceHash: adjustmentEvidence.EvidenceHash,
		ObservedAt: adjustmentEvidence.ConfirmedAt, FundingAdjustment: &adjustmentEvidence}
	adjustmentOp := accounting.RecoveryOperation{EpisodeID: episode.ID, OperationID: "source-anomaly-adjustment-1", HashVersion: 1,
		ExpectedEpisodeRevision: 3, ExpectedRuntimeRevision: "runtime-adjust-3", ExpectedChildRevisions: map[string]uint64{},
		ExpectedEventRevisions: map[string]uint64{}, Events: []accounting.RecoveryEvent{adjustment}, SourceAnomalyID: anomaly.ID,
		ExpectedAnomalyRevision: 1, Actor: "auditor", AuditRef: adjustmentEvidence.AuditRef, EvidenceHash: adjustmentEvidence.EvidenceHash,
		EvidenceReadAt: anomaly.DetectedAt}
	if _, replayed, err := store.ApplyRecoveryOperation(ctx, generation, adjustmentOp); err != nil || replayed {
		t.Fatalf("apply exact append-only adjustment: replayed=%t err=%v", replayed, err)
	}
	adjusted, found, err := store.GetRecoverySourceAnomaly(ctx, episode.ID, anomaly.ID)
	if err != nil || !found || adjusted.State != accounting.RecoverySourceAnomalyAdjustmentApplied || adjusted.Facts[0].OriginalEvent.Amount != original.Amount ||
		adjusted.AdjustmentOperationID != adjustmentOp.OperationID {
		t.Fatalf("adjustment did not retain original source fact and audit: %+v found=%t err=%v", adjusted, found, err)
	}
	assertRecoverySourceAnomalyBlocksRelease(t, store, generation, episode.ID, adjustmentOp.OperationID)
	ledgerEvents, err := store.ListRecoveryEvents(ctx, episode.ID)
	if err != nil || len(ledgerEvents) != 2 || ledgerEvents[0].ID != original.ID || ledgerEvents[0].Amount != original.Amount {
		t.Fatalf("adjustment overwrote original ledger fact: events=%+v err=%v", ledgerEvents, err)
	}
	projectSourceCursorOperation(t, store, generation, episode.ID, adjustmentOp.OperationID)
	projectedAnomaly, found, err := store.GetRecoverySourceAnomaly(ctx, episode.ID, anomaly.ID)
	if err != nil || !found || projectedAnomaly.State != accounting.RecoverySourceAnomalyAdjustmentApplied {
		t.Fatalf("projected anomaly state = %+v found=%t err=%v", projectedAnomaly, found, err)
	}
	anomalies, err := store.ListRecoverySourceAnomalies(ctx, episode.ID)
	if err != nil || len(anomalies) != 1 || anomalies[0].ID != anomaly.ID {
		t.Fatalf("durable anomaly list = %+v err=%v", anomalies, err)
	}
	cursor, found, err := store.GetRecoverySourceCursor(ctx, identity)
	if err != nil || !found || cursor.Revision != 1 || cursor.HistoricalEvidenceHash != window.HistoricalEvidenceHash {
		t.Fatalf("anomaly capture advanced or damaged source cursor: %+v found=%t err=%v", cursor, found, err)
	}
	holds, err := store.ListActiveRecoveryHolds(ctx)
	if err != nil || len(holds) != 1 || holds[0].State != "held" {
		t.Fatalf("anomaly capture lost durable hold: %+v err=%v", holds, err)
	}
	newGeneration, err := store.ClaimFundingCarryRuntimeGeneration(ctx, []string{scope})
	if err != nil {
		t.Fatal("claim replacement generation:", err)
	}
	if _, _, err := store.RecordRecoverySourceAnomaly(ctx, generation, anomaly); err == nil {
		t.Fatal("stale generation recorded a source anomaly")
	}
	if _, _, err := store.RecordRecoverySourceAnomaly(ctx, newGeneration, anomaly); err == nil {
		t.Fatal("replacement generation recorded an anomaly against a stale episode revision")
	}
}

func assertRecoverySourceAnomalyBlocksRelease(t *testing.T, store *SQLStorage, generation FundingCarryRuntimeGeneration, episodeID, projectionOperationID string) {
	t.Helper()
	episode, found, err := store.GetRecoveryEpisode(context.Background(), episodeID)
	if err != nil || !found {
		t.Fatalf("read episode for release guard: found=%t err=%v", found, err)
	}
	hold, held, err := store.GetRecoveryHold(context.Background(), episodeID, episode.RecoveryGeneration)
	if err != nil || !held {
		t.Fatalf("read hold for release guard: held=%t err=%v", held, err)
	}
	outbox, found, err := store.GetRecoveryOutbox(context.Background(), episodeID, projectionOperationID)
	if err != nil || !found {
		t.Fatalf("read projected outbox for release guard: found=%t err=%v", found, err)
	}
	authorization := accounting.RecoveryReleaseAuthorization{EpisodeID: episodeID, OperationID: "release-with-unresolved-source-anomaly",
		Actor: "admin", EvidenceHash: strings.Repeat("a", 64), ProjectionHash: outbox.ProjectionHash, RuntimeRevision: outbox.ExpectedRuntimeRevision,
		OwnerGeneration: generation.ID(), ExpectedEpisodeRevision: episode.Revision, RecoveryGeneration: episode.RecoveryGeneration,
		ExpectedHoldRevision: hold.Revision, Confirmed: true}
	if _, err := store.PrepareRecoveryRelease(context.Background(), generation, authorization); !errors.Is(err, accounting.ErrRecoveryConflict) {
		t.Fatalf("prepare release with unresolved source anomaly error=%v, want conflict", err)
	}
	if _, err := store.ReleaseRecoveryHold(context.Background(), generation, authorization); !errors.Is(err, accounting.ErrRecoveryConflict) {
		t.Fatalf("release hold with unresolved source anomaly error=%v, want conflict", err)
	}
}

func TestSQLiteFundingRecoveryAnomalyMigrationIsIdempotentAndRetainsAuditRows(t *testing.T) {
	store, _, _, _ := newSourceCursorFixture(t, "episode-source-anomaly-migration")
	var count int
	if err := store.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='funding_recovery_source_anomalies'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("anomaly table count=%d err=%v", count, err)
	}
	if err := store.MigrateFundingRecovery(t.Context()); err != nil {
		t.Fatal("repeat anomaly migration:", err)
	}
	if err := store.db.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='funding_recovery_source_anomalies'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("repeated migration removed anomaly table: count=%d err=%v", count, err)
	}
	for _, path := range []string{"migrations/2026101205_funding_recovery_sqlite.down.sql", "migrations/2026101205_funding_recovery_sqlite.up.sql"} {
		body, err := fundingRecoveryMigrations.ReadFile(path)
		if err != nil {
			t.Fatal("read anomaly migration", path, err)
		}
		if strings.TrimSpace(string(body)) == "" {
			t.Fatalf("empty migration %s", path)
		}
	}
}

func TestMySQLFundingRecoveryAnomalyMigrationDDLIsEmbedded(t *testing.T) {
	for _, path := range []string{"migrations/2026101205_funding_recovery_mysql.up.sql", "migrations/2026101205_funding_recovery_mysql.down.sql"} {
		body, err := fundingRecoveryMigrations.ReadFile(path)
		if err != nil || strings.TrimSpace(string(body)) == "" || (strings.HasSuffix(path, ".up.sql") && !strings.Contains(string(body), "funding_recovery_source_anomalies")) || strings.Contains(strings.ToUpper(string(body)), "DROP TABLE") {
			t.Fatalf("read MySQL anomaly migration %s: %v", path, err)
		}
	}
}
