package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"quantmesh/accounting"
)

func TestMySQLFundingRecoveryAnomalyDifferentAdjustmentsHaveSingleWinner(t *testing.T) {
	dsn := os.Getenv("QUANTMESH_MYSQL_TEST_DSN")
	if dsn == "" {
		t.Skip("requires QUANTMESH_MYSQL_TEST_DSN and explicit disposable-schema opt-in; MySQL anomaly race not run")
	}
	allowDestructive := os.Getenv("QUANTMESH_MYSQL_TEST_ALLOW_DESTRUCTIVE_SCHEMA") == "1"
	baseConfig, err := orderReconciliationMySQLFixtureConfig(dsn, allowDestructive)
	if err != nil {
		t.Skipf("MySQL anomaly race refused unsafe fixture DSN: %v", err)
	}
	if !allowDestructive {
		t.Skip("requires QUANTMESH_MYSQL_TEST_ALLOW_DESTRUCTIVE_SCHEMA=1; MySQL anomaly race not run")
	}

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	admin, err := sql.Open("mysql", baseConfig.FormatDSN())
	if err != nil {
		t.Fatal("open MySQL anomaly fixture administrator connection:", err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	if err := admin.PingContext(ctx); err != nil {
		t.Fatal("connect MySQL anomaly fixture:", err)
	}
	var serverVersion string
	if err := admin.QueryRowContext(ctx, `SELECT VERSION()`).Scan(&serverVersion); err != nil {
		t.Fatal("read MySQL server version:", err)
	}
	if !strings.HasPrefix(serverVersion, "8.") {
		t.Fatalf("MySQL anomaly concurrency requires MySQL 8, got %q", serverVersion)
	}

	schema := fmt.Sprintf("quantmesh_audit_funding_anomaly_%d", time.Now().UTC().UnixNano())
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE `"+schema+"` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
		t.Fatal("create isolated MySQL anomaly schema:", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := admin.ExecContext(cleanupCtx, "DROP DATABASE `"+schema+"`"); err != nil {
			t.Errorf("drop isolated MySQL anomaly schema %q: %v", schema, err)
		}
	})

	config := *baseConfig
	config.DBName = schema
	stores := make([]*SQLStorage, 2)
	connectionIDs := make([]int64, 2)
	for index := range stores {
		db, err := sql.Open("mysql", config.FormatDSN())
		if err != nil {
			t.Fatalf("open independent MySQL storage connection %d: %v", index, err)
		}
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)
		t.Cleanup(func() { _ = db.Close() })
		if err := db.PingContext(ctx); err != nil {
			t.Fatalf("connect independent MySQL storage connection %d: %v", index, err)
		}
		if err := db.QueryRowContext(ctx, `SELECT CONNECTION_ID()`).Scan(&connectionIDs[index]); err != nil {
			t.Fatalf("read MySQL connection ID for independent pool %d: %v", index, err)
		}
		stores[index] = &SQLStorage{db: db, dbType: "mysql", commitProbe: db}
	}
	if connectionIDs[0] == connectionIDs[1] {
		t.Fatalf("race participants do not use independent MySQL connections: both use connection %d", connectionIDs[0])
	}

	if err := migrateFundingCarryRuntimeGeneration(stores[0].db, "mysql"); err != nil {
		t.Fatal("migrate MySQL funding runtime generation:", err)
	}
	if err := stores[0].MigrateFundingRecovery(ctx); err != nil {
		t.Fatal("migrate MySQL funding recovery:", err)
	}
	if err := stores[0].SetRecoverySourceWindowVerifier(testRecoverySourceWindowVerifier{}); err != nil {
		t.Fatal("configure trusted test source-window verifier:", err)
	}
	if err := stores[1].SetRecoverySourceWindowVerifier(testRecoverySourceWindowVerifier{}); err != nil {
		t.Fatal("configure second trusted test source-window verifier:", err)
	}

	scope := fmt.Sprintf("%064x", time.Now().UTC().UnixNano())
	generation, err := stores[0].ClaimFundingCarryRuntimeGeneration(ctx, []string{scope})
	if err != nil {
		t.Fatal("claim isolated MySQL recovery generation:", err)
	}
	episode := accounting.RecoveryEpisode{
		ID: "mysql-anomaly-race-" + scope[:20], Bot: "bot-mysql-anomaly-race", Strategy: "FundingCarry",
		AccountScopes: []string{scope}, OwnerGeneration: generation.ID(), Revision: 1, RecoveryGeneration: 1,
		NextActionOrdinal: 1, State: accounting.RecoveryPrepared,
	}
	if _, err := stores[0].CreateRecoveryEpisode(ctx, generation, episode); err != nil {
		t.Fatal("create held MySQL recovery episode:", err)
	}

	from := time.Date(2026, 10, 12, 0, 0, 0, 0, time.UTC)
	through := from.Add(time.Hour)
	window := sourceWindow(scope, from, through, 0, 1)
	window.Source = accounting.RecoverySourceBinanceFuturesIncomeV1
	window.InitialFrom = from
	window.HistoricalEvidenceHash = strings.Repeat("c", 64)
	seed := fundingOperation(episode, scope, "mysql-anomaly-race-seed", 1, window, "income-transaction-original", "-1.25")
	if _, replayed, err := stores[0].ApplyRecoveryOperation(ctx, generation, seed); err != nil || replayed {
		t.Fatalf("seed durable source cursor: replayed=%t err=%v", replayed, err)
	}
	projectSourceCursorOperation(t, stores[0], generation, episode.ID, seed.OperationID)
	original := seed.Events[0]
	observed := original
	observed.ID = "observed-income-transaction"
	observed.Amount = "-1.5"
	observed.SourceRevision = "mysql-late-history-revision"
	observed.SourceHash = strings.Repeat("d", 64)
	delta, err := accounting.RecoveryFundingAmountDelta(&original, &observed)
	if err != nil {
		t.Fatal("calculate exact observed anomaly delta:", err)
	}
	identity := window.Identity()
	anomaly := accounting.RecoverySourceAnomaly{
		EpisodeID: episode.ID, RecoveryGeneration: episode.RecoveryGeneration, EpisodeRevision: 3,
		Identity: identity, CursorRevision: 1, InitialFrom: from, Through: through,
		ExpectedHistoryHash: window.HistoricalEvidenceHash, ObservedHistoryHash: strings.Repeat("e", 64),
		Facts: []accounting.RecoverySourceAnomalyFact{{
			Kind: accounting.RecoverySourceAnomalyChanged, ExternalID: original.ExternalID,
			OriginalEvent: &original, ObservedEvent: &observed, DeltaAmount: delta,
		}},
		Revision: 1, State: accounting.RecoverySourceAnomalyOpen,
		DetectedActor: "qa-mysql-race", DetectedAuditRef: "rc1186-mysql-anomaly-detection",
		DetectedAt: from.Add(2 * time.Hour),
	}
	anomaly.ID, err = anomaly.Fingerprint()
	if err != nil {
		t.Fatal("fingerprint durable source anomaly:", err)
	}
	if _, replayed, err := stores[0].RecordRecoverySourceAnomaly(ctx, generation, anomaly); err != nil || replayed {
		t.Fatalf("record durable source anomaly: replayed=%t err=%v", replayed, err)
	}
	initialCursor, found, err := stores[0].GetRecoverySourceCursor(ctx, identity)
	if err != nil || !found || initialCursor.Revision != 1 {
		t.Fatalf("read seeded source cursor: cursor=%+v found=%t err=%v", initialCursor, found, err)
	}

	operations := []accounting.RecoveryOperation{
		mysqlAnomalyAdjustmentOperation(episode, identity, anomaly, original, observed, "mysql-anomaly-adjustment-a", "operator-a", "audit-mysql-adjustment-a", strings.Repeat("a", 64)),
		mysqlAnomalyAdjustmentOperation(episode, identity, anomaly, original, observed, "mysql-anomaly-adjustment-b", "operator-b", "audit-mysql-adjustment-b", strings.Repeat("b", 64)),
	}
	start := make(chan struct{})
	type result struct {
		receipt  accounting.RecoveryReceipt
		replayed bool
		err      error
	}
	results := make([]result, len(operations))
	var workers sync.WaitGroup
	for index, store := range stores {
		workers.Add(1)
		go func(index int, store *SQLStorage) {
			defer workers.Done()
			<-start
			callCtx, callCancel := context.WithTimeout(ctx, 15*time.Second)
			defer callCancel()
			results[index].receipt, results[index].replayed, results[index].err = store.ApplyRecoveryOperation(callCtx, generation, operations[index])
		}(index, store)
	}
	close(start)
	workers.Wait()

	winner := -1
	loser := -1
	for index, got := range results {
		if got.err == nil && !got.replayed {
			if winner != -1 {
				t.Fatalf("different MySQL adjustment operations both committed: results=%+v", results)
			}
			winner = index
			continue
		}
		if got.err == nil || got.replayed {
			t.Fatalf("losing MySQL adjustment operation was treated as successful: index=%d replayed=%t receipt=%+v err=%v", index, got.replayed, got.receipt, got.err)
		}
		if !errors.Is(got.err, accounting.ErrRecoveryConflict) {
			t.Fatalf("losing MySQL adjustment operation returned non-CAS error: index=%d err=%v", index, got.err)
		}
		loser = index
	}
	if winner < 0 || loser < 0 {
		t.Fatalf("MySQL anomaly race did not produce exactly one winner and one CAS loser: results=%+v", results)
	}

	winningOperation := operations[winner]
	winningResult := results[winner]
	losingOperation := operations[loser]
	if receipt, found, err := stores[1].GetRecoveryReceipt(ctx, episode.ID, winningOperation.OperationID); err != nil || !found || receipt.RequestHash != winningResult.receipt.RequestHash {
		t.Fatalf("winner receipt readback mismatch: receipt=%+v found=%t err=%v", receipt, found, err)
	}
	if _, found, err := stores[1].GetRecoveryReceipt(ctx, episode.ID, losingOperation.OperationID); err != nil || found {
		t.Fatalf("loser has durable receipt: found=%t err=%v", found, err)
	}
	if outbox, found, err := stores[1].GetRecoveryOutbox(ctx, episode.ID, winningOperation.OperationID); err != nil || !found || outbox.ProjectionHash != winningResult.receipt.ProjectionHash {
		t.Fatalf("winner outbox readback mismatch: outbox=%+v found=%t err=%v", outbox, found, err)
	}
	if _, found, err := stores[1].GetRecoveryOutbox(ctx, episode.ID, losingOperation.OperationID); err != nil || found {
		t.Fatalf("loser has durable outbox: found=%t err=%v", found, err)
	}
	var operationCount, outboxCount, adjustmentCount int
	if err := stores[1].db.QueryRowContext(ctx, `SELECT COUNT(*) FROM funding_recovery_operations WHERE episode_id=?`, episode.ID).Scan(&operationCount); err != nil {
		t.Fatal("count durable MySQL operation receipts:", err)
	}
	if err := stores[1].db.QueryRowContext(ctx, `SELECT COUNT(*) FROM funding_recovery_outbox WHERE episode_id=?`, episode.ID).Scan(&outboxCount); err != nil {
		t.Fatal("count durable MySQL operation outboxes:", err)
	}
	if err := stores[1].db.QueryRowContext(ctx, `SELECT COUNT(*) FROM funding_recovery_events WHERE episode_id=?`, episode.ID).Scan(&adjustmentCount); err != nil {
		t.Fatal("count durable MySQL ledger events:", err)
	}
	if operationCount != 2 || outboxCount != 2 || adjustmentCount != 2 {
		t.Fatalf("MySQL anomaly race wrote duplicate or partial state: operation receipts=%d outboxes=%d ledger events=%d; want seed plus one adjustment", operationCount, outboxCount, adjustmentCount)
	}
	events, err := stores[1].ListRecoveryEvents(ctx, episode.ID)
	if err != nil || len(events) != 2 {
		t.Fatalf("read MySQL ledger events: events=%+v err=%v", events, err)
	}
	originalPreserved, winningAdjustmentFound := false, false
	for _, event := range events {
		if event.ID == original.ID && event.ExternalID == original.ExternalID && event.Amount == original.Amount && event.SourceHash == original.SourceHash {
			originalPreserved = true
		}
		if event.ID == winningOperation.Events[0].ID && event.Amount == delta && event.RelatedExternalID == original.ExternalID &&
			event.FundingAdjustment != nil && event.FundingAdjustment.AnomalyID == anomaly.ID &&
			event.FundingAdjustment.OriginalExternalID == original.ExternalID && event.FundingAdjustment.OriginalSourceHash == original.SourceHash &&
			event.FundingAdjustment.ObservedSourceHash == observed.SourceHash && event.FundingAdjustment.OriginalAmountRaw == original.Amount &&
			event.FundingAdjustment.ObservedAmountRaw == observed.Amount && event.FundingAdjustment.DeltaAmountRaw == delta &&
			event.FundingAdjustment.EvidenceHash == winningOperation.EvidenceHash && event.FundingAdjustment.Actor == winningOperation.Actor &&
			event.FundingAdjustment.AuditRef == winningOperation.AuditRef {
			winningAdjustmentFound = true
		}
	}
	if !originalPreserved || !winningAdjustmentFound {
		t.Fatalf("MySQL ledger did not preserve original fact and append exactly the winning adjustment: events=%+v", events)
	}
	readbackAnomaly, found, err := stores[1].GetRecoverySourceAnomaly(ctx, episode.ID, anomaly.ID)
	if err != nil || !found || readbackAnomaly.State != accounting.RecoverySourceAnomalyAdjustmentApplied || readbackAnomaly.Revision != 2 ||
		readbackAnomaly.AdjustmentOperationID != winningOperation.OperationID || readbackAnomaly.AdjustmentActor != winningOperation.Actor ||
		readbackAnomaly.AdjustmentAuditRef != winningOperation.AuditRef || readbackAnomaly.AdjustmentEvidenceHash != winningOperation.EvidenceHash ||
		len(readbackAnomaly.Facts) != len(anomaly.Facts) || !reflect.DeepEqual(readbackAnomaly.Facts, anomaly.Facts) {
		t.Fatalf("MySQL anomaly audit readback mismatch: anomaly=%+v found=%t err=%v", readbackAnomaly, found, err)
	}
	currentCursor, found, err := stores[1].GetRecoverySourceCursor(ctx, identity)
	if err != nil || !found || currentCursor.Revision != initialCursor.Revision || currentCursor.EvidenceHash != initialCursor.EvidenceHash ||
		!currentCursor.Through.Equal(initialCursor.Through) || currentCursor.HistoricalEvidenceHash != initialCursor.HistoricalEvidenceHash {
		t.Fatalf("manual anomaly adjustment changed source cursor: cursor=%+v found=%t err=%v", currentCursor, found, err)
	}
	hold, found, err := stores[1].GetRecoveryHold(ctx, episode.ID, episode.RecoveryGeneration)
	if err != nil || !found || hold.State != "held" {
		t.Fatalf("MySQL anomaly adjustment released recovery hold: hold=%+v found=%t err=%v", hold, found, err)
	}
	activeHolds, err := stores[1].ListActiveRecoveryHolds(ctx)
	if err != nil || len(activeHolds) != 1 || activeHolds[0].EpisodeID != episode.ID || activeHolds[0].State != "held" {
		t.Fatalf("MySQL active recovery hold readback mismatch: holds=%+v err=%v", activeHolds, err)
	}
	currentEpisode, found, err := stores[1].GetRecoveryEpisode(ctx, episode.ID)
	if err != nil || !found || currentEpisode.Revision != winningResult.receipt.EpisodeRevision || currentEpisode.State != accounting.RecoveryLedgerCommitted {
		t.Fatalf("MySQL episode revision/state differs from winner receipt: episode=%+v found=%t err=%v", currentEpisode, found, err)
	}

	replayedReceipt, replayed, err := stores[1].ApplyRecoveryOperation(ctx, generation, winningOperation)
	if err != nil || !replayed || replayedReceipt.RequestHash != winningResult.receipt.RequestHash || replayedReceipt.ProjectionHash != winningResult.receipt.ProjectionHash {
		t.Fatalf("winner retry was not an idempotent receipt readback: receipt=%+v replayed=%t err=%v", replayedReceipt, replayed, err)
	}
	if _, replayed, err := stores[1].ApplyRecoveryOperation(ctx, generation, losingOperation); !errors.Is(err, accounting.ErrRecoveryConflict) || replayed {
		t.Fatalf("loser retry was treated as a successful/idempotent adjustment: replayed=%t err=%v", replayed, err)
	}
	if _, found, err := stores[1].GetRecoveryReceipt(ctx, episode.ID, losingOperation.OperationID); err != nil || found {
		t.Fatalf("loser retry created a durable receipt: found=%t err=%v", found, err)
	}
	if events, err := stores[1].ListRecoveryEvents(ctx, episode.ID); err != nil || len(events) != 2 {
		t.Fatalf("winner retry duplicated MySQL adjustment ledger event: events=%+v err=%v", events, err)
	}
	if _, found, err := stores[1].GetRecoveryOutbox(ctx, episode.ID, losingOperation.OperationID); err != nil || found {
		t.Fatalf("loser retry created an outbox: found=%t err=%v", found, err)
	}
	var finalOutboxCount int
	if err := stores[1].db.QueryRowContext(ctx, `SELECT COUNT(*) FROM funding_recovery_outbox WHERE episode_id=?`, episode.ID).Scan(&finalOutboxCount); err != nil || finalOutboxCount != 2 {
		t.Fatalf("loser retry changed durable outbox count=%d err=%v; want seed plus winner", finalOutboxCount, err)
	}
	finalAnomaly, found, err := stores[1].GetRecoverySourceAnomaly(ctx, episode.ID, anomaly.ID)
	if err != nil || !found || finalAnomaly.Revision != 2 || finalAnomaly.AdjustmentOperationID != winningOperation.OperationID ||
		finalAnomaly.AdjustmentAuditRef != winningOperation.AuditRef || finalAnomaly.AdjustmentEvidenceHash != winningOperation.EvidenceHash {
		t.Fatalf("loser retry changed durable anomaly state: anomaly=%+v found=%t err=%v", finalAnomaly, found, err)
	}
	finalCursor, found, err := stores[1].GetRecoverySourceCursor(ctx, identity)
	if err != nil || !found || finalCursor.Revision != initialCursor.Revision || finalCursor.EvidenceHash != initialCursor.EvidenceHash {
		t.Fatalf("loser retry changed durable source cursor: cursor=%+v found=%t err=%v", finalCursor, found, err)
	}
	finalHold, found, err := stores[1].GetRecoveryHold(ctx, episode.ID, episode.RecoveryGeneration)
	if err != nil || !found || finalHold.State != "held" {
		t.Fatalf("loser retry changed durable hold: hold=%+v found=%t err=%v", finalHold, found, err)
	}
}

func mysqlAnomalyAdjustmentOperation(
	episode accounting.RecoveryEpisode,
	identity accounting.RecoverySourceIdentity,
	anomaly accounting.RecoverySourceAnomaly,
	original, observed accounting.RecoveryEvent,
	operationID, actor, auditRef, evidenceHash string,
) accounting.RecoveryOperation {
	confirmedAt := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	evidence := accounting.RecoveryFundingAdjustmentEvidence{
		AnomalyID: anomaly.ID, OriginalExternalID: original.ExternalID,
		OriginalSourceHash: original.SourceHash, ObservedSourceHash: observed.SourceHash,
		OriginalAmountRaw: original.Amount, ObservedAmountRaw: observed.Amount,
		ObservedRowAt: observed.ObservedAt, DeltaAmountRaw: anomaly.Facts[0].DeltaAmount,
		EvidenceHash: evidenceHash, Actor: actor, AuditRef: auditRef, ConfirmedAt: confirmedAt,
	}
	adjustment := accounting.RecoveryEvent{
		ID: "mysql-manual-adjustment-" + operationID, Source: accounting.RecoveryManualFundingAdjustmentSource,
		Exchange: identity.Exchange, Market: identity.Market, AccountScope: identity.AccountScope, Symbol: identity.Symbol,
		Kind: accounting.RecoveryFundingEvent, ExternalID: "adjustment:" + anomaly.ID + ":" + operationID,
		RelatedExternalID: original.ExternalID, EpisodeID: episode.ID, Asset: identity.Asset,
		Amount: anomaly.Facts[0].DeltaAmount, SourceRevision: anomaly.ID + ":1", SourceHash: evidenceHash,
		ObservedAt: confirmedAt, FundingAdjustment: &evidence,
	}
	return accounting.RecoveryOperation{
		EpisodeID: episode.ID, OperationID: operationID, HashVersion: 1,
		ExpectedEpisodeRevision: 3, ExpectedRuntimeRevision: "mysql-runtime-anomaly-race-3",
		ExpectedChildRevisions: map[string]uint64{}, ExpectedEventRevisions: map[string]uint64{},
		Events: []accounting.RecoveryEvent{adjustment}, SourceAnomalyID: anomaly.ID, ExpectedAnomalyRevision: 1,
		Actor: actor, AuditRef: auditRef, EvidenceHash: evidenceHash, EvidenceReadAt: anomaly.DetectedAt,
	}
}
