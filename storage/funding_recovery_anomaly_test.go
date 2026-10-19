package storage

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mattn/go-sqlite3"
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

func TestSQLiteFundingRecoverySourceAnomalyDifferentAdjustmentsAcrossHandlesHaveSingleCommit(t *testing.T) {
	databasePath := filepath.Join(t.TempDir(), "source-anomaly-competing-adjustments.db")
	firstStore, err := NewSQLStorage(databasePath)
	if err != nil {
		t.Fatal("open first SQL handle:", err)
	}
	t.Cleanup(func() { _ = firstStore.Close() })
	secondStore, err := NewSQLStorage(databasePath)
	if err != nil {
		t.Fatal("open second SQL handle:", err)
	}
	t.Cleanup(func() { _ = secondStore.Close() })
	ctx := t.Context()
	for index, store := range []*SQLStorage{firstStore, secondStore} {
		if err := store.MigrateFundingRecovery(ctx); err != nil {
			t.Fatalf("migrate independent SQL handle %d: %v", index, err)
		}
		if err := store.SetRecoverySourceWindowVerifier(testRecoverySourceWindowVerifier{}); err != nil {
			t.Fatalf("configure independent SQL handle %d verifier: %v", index, err)
		}
	}
	scope := strings.Repeat("a", 64)
	generation, err := firstStore.ClaimFundingCarryRuntimeGeneration(ctx, []string{scope})
	if err != nil {
		t.Fatal("claim shared runtime generation:", err)
	}
	episode := accounting.RecoveryEpisode{ID: "episode-competing-adjustments", Bot: "bot-competing-adjustments", Strategy: "FundingCarry",
		AccountScopes: []string{scope}, OwnerGeneration: generation.ID(), Revision: 1, RecoveryGeneration: 1, State: accounting.RecoveryPrepared}
	if _, err := firstStore.CreateRecoveryEpisode(ctx, generation, episode); err != nil {
		t.Fatal("create held recovery episode:", err)
	}
	from := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	window := sourceWindow(scope, from, from.Add(time.Hour), 0, 1)
	window.Source = accounting.RecoverySourceBinanceFuturesIncomeV1
	window.InitialFrom = from
	window.HistoricalEvidenceHash = strings.Repeat("c", 64)
	seed := fundingOperation(episode, scope, "anomaly-adjustment-seed", 1, window, "funding-original", "-1.25")
	if _, replayed, err := firstStore.ApplyRecoveryOperation(ctx, generation, seed); err != nil || replayed {
		t.Fatalf("seed durable source cursor: replayed=%t err=%v", replayed, err)
	}
	projectSourceCursorOperation(t, firstStore, generation, episode.ID, seed.OperationID)
	original := seed.Events[0]
	observed := original
	observed.ID, observed.Amount, observed.SourceRevision, observed.SourceHash = "funding-observed", "-1.50", "source-revision-late", strings.Repeat("b", 64)
	observed.ObservedAt = from.Add(30 * time.Minute)
	delta, err := accounting.RecoveryFundingAmountDelta(&original, &observed)
	if err != nil {
		t.Fatal("calculate exact anomaly adjustment:", err)
	}
	identity := window.Identity()
	anomaly := accounting.RecoverySourceAnomaly{EpisodeID: episode.ID, RecoveryGeneration: episode.RecoveryGeneration, EpisodeRevision: 3,
		Identity: identity, CursorRevision: 1, InitialFrom: from, Through: window.Through,
		ExpectedHistoryHash: window.HistoricalEvidenceHash, ObservedHistoryHash: strings.Repeat("d", 64),
		Facts: []accounting.RecoverySourceAnomalyFact{{Kind: accounting.RecoverySourceAnomalyChanged, ExternalID: original.ExternalID,
			OriginalEvent: &original, ObservedEvent: &observed, DeltaAmount: delta}},
		Revision: 1, State: accounting.RecoverySourceAnomalyOpen, DetectedActor: "auditor", DetectedAuditRef: "ticket-anomaly-race", DetectedAt: from.Add(2 * time.Hour)}
	anomaly.ID, err = anomaly.Fingerprint()
	if err != nil {
		t.Fatal("fingerprint durable source anomaly:", err)
	}
	if _, replayed, err := firstStore.RecordRecoverySourceAnomaly(ctx, generation, anomaly); err != nil || replayed {
		t.Fatalf("record durable source anomaly: replayed=%t err=%v", replayed, err)
	}
	assertRecoverySourceAnomalyBlocksRelease(t, firstStore, generation, episode.ID, seed.OperationID)

	makeAdjustment := func(operationID, auditRef, eventID, externalID string, evidenceByte byte) accounting.RecoveryOperation {
		evidenceHash := strings.Repeat(string(evidenceByte), 64)
		adjustmentEvidence := accounting.RecoveryFundingAdjustmentEvidence{AnomalyID: anomaly.ID, OriginalExternalID: original.ExternalID,
			OriginalSourceHash: original.SourceHash, ObservedSourceHash: observed.SourceHash, OriginalAmountRaw: original.Amount,
			ObservedAmountRaw: observed.Amount, ObservedRowAt: observed.ObservedAt, DeltaAmountRaw: delta, EvidenceHash: evidenceHash,
			Actor: "auditor", AuditRef: auditRef, ConfirmedAt: from.Add(3 * time.Hour)}
		adjustmentEvent := accounting.RecoveryEvent{ID: eventID, Source: accounting.RecoveryManualFundingAdjustmentSource,
			Exchange: identity.Exchange, Market: identity.Market, AccountScope: identity.AccountScope, Symbol: identity.Symbol,
			Kind: accounting.RecoveryFundingEvent, ExternalID: externalID, RelatedExternalID: original.ExternalID,
			EpisodeID: episode.ID, Asset: identity.Asset, Amount: delta, SourceRevision: anomaly.ID + ":1",
			SourceHash: evidenceHash, ObservedAt: adjustmentEvidence.ConfirmedAt, FundingAdjustment: &adjustmentEvidence}
		return accounting.RecoveryOperation{EpisodeID: episode.ID, OperationID: operationID, HashVersion: 1,
			ExpectedEpisodeRevision: 3, ExpectedRuntimeRevision: "runtime-adjustment-race", ExpectedChildRevisions: map[string]uint64{},
			ExpectedEventRevisions: map[string]uint64{}, Events: []accounting.RecoveryEvent{adjustmentEvent}, SourceAnomalyID: anomaly.ID,
			ExpectedAnomalyRevision: 1, Actor: "auditor", AuditRef: auditRef, EvidenceHash: evidenceHash, EvidenceReadAt: anomaly.DetectedAt}
	}
	operations := []accounting.RecoveryOperation{
		makeAdjustment("anomaly-adjustment-op-a", "ticket-adjustment-a", "adjustment-event-a", "adjustment:anomaly:op-a", 'e'),
		makeAdjustment("anomaly-adjustment-op-b", "ticket-adjustment-b", "adjustment-event-b", "adjustment:anomaly:op-b", 'f'),
	}
	stores := make([]*SQLStorage, 2)
	for index := range stores {
		armed := &atomic.Bool{}
		armed.Store(true)
		writerDB := sql.OpenDB(&fundingRecoveryAmbiguousCommitConnector{
			dsn: databasePath + "?_journal_mode=WAL&_synchronous=NORMAL", armed: armed,
		})
		writerDB.SetMaxOpenConns(1)
		t.Cleanup(func() { _ = writerDB.Close() })
		stores[index] = &SQLStorage{db: writerDB, commitProbe: firstStore.commitProbe, dbType: "sqlite"}
	}
	start := make(chan struct{})
	type applyResult struct {
		index    int
		receipt  accounting.RecoveryReceipt
		replayed bool
		err      error
	}
	results := make([]applyResult, len(operations))
	var workers sync.WaitGroup
	for index := range operations {
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			<-start
			results[index].index = index
			results[index].receipt, results[index].replayed, results[index].err = stores[index].ApplyRecoveryOperation(ctx, generation, operations[index])
		}(index)
	}
	close(start)
	workers.Wait()

	winner := -1
	for index, got := range results {
		if got.err == nil {
			if winner != -1 {
				t.Fatalf("two distinct adjustment operations committed: %+v", results)
			}
			if !got.replayed {
				t.Fatalf("operation %s committed despite the injected ambiguous commit outcome without receipt readback", operations[index].OperationID)
			}
			winner = index
			continue
		}
		if !errors.Is(got.err, accounting.ErrRecoveryConflict) {
			t.Fatalf("losing operation %s returned replay=%t err=%v; want durable CAS conflict", operations[index].OperationID, got.replayed, got.err)
		}
	}
	if winner == -1 {
		t.Fatalf("no adjustment operation committed: %+v", results)
	}
	winnerOp, loserOp := operations[winner], operations[1-winner]
	winningStore, losingStore := stores[winner], stores[1-winner]

	// The driver committed the winning transaction and returned an ambiguous error;
	// ApplyRecoveryOperation confirmed it through the independent receipt probe.
	// A subsequent ordinary retry must still return exactly that durable receipt.
	retriedReceipt, replayed, err := winningStore.ApplyRecoveryOperation(ctx, generation, winnerOp)
	if err != nil || !replayed || retriedReceipt.RequestHash != results[winner].receipt.RequestHash || retriedReceipt.OperationID != winnerOp.OperationID {
		t.Fatalf("same-operation response-loss retry = %+v replayed=%t err=%v", retriedReceipt, replayed, err)
	}
	if _, replayed, err := losingStore.ApplyRecoveryOperation(ctx, generation, loserOp); !errors.Is(err, accounting.ErrRecoveryConflict) || replayed {
		t.Fatalf("losing operation retry replayed=%t err=%v; want conflict", replayed, err)
	}

	readback, found, err := losingStore.GetRecoverySourceAnomaly(ctx, episode.ID, anomaly.ID)
	if err != nil || !found || readback.State != accounting.RecoverySourceAnomalyAdjustmentApplied || readback.Revision != 2 ||
		readback.AdjustmentOperationID != winnerOp.OperationID || readback.AdjustmentAuditRef != winnerOp.AuditRef || readback.Facts[0].OriginalEvent.Amount != original.Amount {
		t.Fatalf("durable anomaly winner/original-fact readback = %+v found=%t err=%v", readback, found, err)
	}
	if _, found, err := losingStore.GetRecoveryReceipt(ctx, episode.ID, loserOp.OperationID); err != nil || found {
		t.Fatalf("losing operation created a durable receipt: found=%t err=%v", found, err)
	}
	winnerReceipt, found, err := losingStore.GetRecoveryReceipt(ctx, episode.ID, winnerOp.OperationID)
	if err != nil || !found || winnerReceipt.RequestHash != results[winner].receipt.RequestHash {
		t.Fatalf("winner receipt readback = %+v found=%t err=%v", winnerReceipt, found, err)
	}
	var operationCount, outboxCount int
	if err := losingStore.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM funding_recovery_operations WHERE episode_id=?`, episode.ID).Scan(&operationCount); err != nil || operationCount != 2 {
		t.Fatalf("durable operation count=%d err=%v; want seed and one winner", operationCount, err)
	}
	if err := losingStore.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM funding_recovery_outbox WHERE episode_id=?`, episode.ID).Scan(&outboxCount); err != nil || outboxCount != 2 {
		t.Fatalf("durable outbox count=%d err=%v; want seed and one winner", outboxCount, err)
	}
	events, err := losingStore.ListRecoveryEvents(ctx, episode.ID)
	if err != nil || len(events) != 2 {
		t.Fatalf("durable funding facts = %+v err=%v; want original plus one adjustment", events, err)
	}
	adjustmentCount := 0
	for _, event := range events {
		if event.Source == accounting.RecoveryManualFundingAdjustmentSource {
			adjustmentCount++
			if event.ID != winnerOp.Events[0].ID || event.Amount != delta || event.FundingAdjustment == nil || event.FundingAdjustment.AuditRef != winnerOp.AuditRef {
				t.Fatalf("persisted adjustment differs from winning exact audit event: %+v", event)
			}
		} else if event.ID != original.ID || event.Amount != original.Amount {
			t.Fatalf("original funding fact was mutated: %+v", event)
		}
	}
	if adjustmentCount != 1 {
		t.Fatalf("persisted adjustment count=%d; want exactly one", adjustmentCount)
	}
	currentEpisode, found, err := losingStore.GetRecoveryEpisode(ctx, episode.ID)
	if err != nil || !found || currentEpisode.Revision != 4 {
		t.Fatalf("episode after atomic ledger commit = %+v found=%t err=%v; want revision 4", currentEpisode, found, err)
	}
	cursor, found, err := losingStore.GetRecoverySourceCursor(ctx, identity)
	if err != nil || !found || cursor.Revision != 1 || cursor.HistoricalEvidenceHash != window.HistoricalEvidenceHash {
		t.Fatalf("competing adjustment advanced or changed source cursor: %+v found=%t err=%v", cursor, found, err)
	}
	hold, found, err := losingStore.GetRecoveryHold(ctx, episode.ID, episode.RecoveryGeneration)
	if err != nil || !found || hold.State != "held" {
		t.Fatalf("competing adjustment released or changed recovery hold: %+v found=%t err=%v", hold, found, err)
	}
	assertRecoverySourceAnomalyBlocksRelease(t, losingStore, generation, episode.ID, winnerOp.OperationID)
}

type fundingRecoveryAmbiguousCommitConnector struct {
	dsn   string
	armed *atomic.Bool
}

func (c *fundingRecoveryAmbiguousCommitConnector) Connect(ctx context.Context) (driver.Conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	conn, err := (&sqlite3.SQLiteDriver{}).Open(c.dsn)
	if err != nil {
		return nil, err
	}
	return &fundingRecoveryAmbiguousCommitConn{Conn: conn, armed: c.armed}, nil
}

func (c *fundingRecoveryAmbiguousCommitConnector) Driver() driver.Driver {
	return &sqlite3.SQLiteDriver{}
}

type fundingRecoveryAmbiguousCommitConn struct {
	driver.Conn
	armed *atomic.Bool
}

func (c *fundingRecoveryAmbiguousCommitConn) Begin() (driver.Tx, error) {
	tx, err := c.Conn.Begin()
	if err != nil {
		return nil, err
	}
	return &fundingRecoveryAmbiguousCommitTx{Tx: tx, armed: c.armed}, nil
}

func (c *fundingRecoveryAmbiguousCommitConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	beginner, ok := c.Conn.(driver.ConnBeginTx)
	if !ok {
		return nil, errors.New("SQLite connection does not support BeginTx")
	}
	tx, err := beginner.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &fundingRecoveryAmbiguousCommitTx{Tx: tx, armed: c.armed}, nil
}

type fundingRecoveryAmbiguousCommitTx struct {
	driver.Tx
	armed *atomic.Bool
}

func (tx *fundingRecoveryAmbiguousCommitTx) Commit() error {
	if err := tx.Tx.Commit(); err != nil {
		return err
	}
	if tx.armed.CompareAndSwap(true, false) {
		return errors.New("injected ambiguous commit response after durable SQLite commit")
	}
	return nil
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
		Actor: "admin", AuditRef: "incident-unresolved-anomaly", EvidenceHash: strings.Repeat("a", 64), ExposureClearanceHash: strings.Repeat("b", 64),
		ExposureClearanceConfirmed: true, ProjectionHash: outbox.ProjectionHash, RuntimeRevision: outbox.ExpectedRuntimeRevision,
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
