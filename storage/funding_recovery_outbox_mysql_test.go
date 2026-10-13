package storage

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"quantmesh/accounting"
)

func TestMySQLFundingRecoveryOutboxJSONRoundTripAndLegacyHashBoundProjection(t *testing.T) {
	fixture := newMySQLFundingRecoveryOutboxFixture(t)
	ctx := fixture.ctx
	from := time.Date(2026, 10, 13, 0, 0, 0, 0, time.UTC)
	episode, operation, identity := fixture.applyCursorOperation(t, "legacy-pending", "BTCUSDT", from)

	storedJSONType, storedPayload := readMySQLRecoveryOutboxJSON(t, ctx, fixture.store, episode.ID, operation.OperationID)
	if storedJSONType != "OBJECT" {
		t.Fatalf("payload_json native JSON type = %q, want OBJECT", storedJSONType)
	}
	outbox, found, err := fixture.store.GetRecoveryOutbox(ctx, episode.ID, operation.OperationID)
	if err != nil || !found {
		t.Fatalf("read pending outbox after native JSON round-trip: found=%t err=%v", found, err)
	}
	if !strings.Contains(string(storedPayload), `"source_cursor"`) {
		t.Fatalf("native JSON outbox lost the durable source cursor projection: %s", storedPayload)
	}
	assertMySQLProjectionCursorPayload(t, outbox.ProjectionPayload, identity, 1)

	// Model an older raw-byte hash over a projection that includes the current
	// runtime-revision binding. The hash encoding is legacy; the CAS value remains
	// authenticated by the projection.
	legacyPayload := append([]byte(" \n\t"), reorderProjectionJSONKeys(t, outbox.ProjectionPayload)...)
	if bytes.Equal(legacyPayload, outbox.ProjectionPayload) {
		t.Fatal("legacy pending fixture did not vary the serialized JSON representation")
	}
	legacyHash := legacyFundingRecoveryProjectionHash(t, outbox.ProjectionPayload)
	receipt, found, err := fixture.store.GetRecoveryReceipt(ctx, episode.ID, operation.OperationID)
	if err != nil || !found {
		t.Fatalf("read receipt before simulating legacy pending row: found=%t err=%v", found, err)
	}
	receipt.ProjectionHash = legacyHash
	receiptJSON, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal("marshal legacy recovery receipt:", err)
	}
	if _, err := fixture.store.db.ExecContext(ctx, `UPDATE funding_recovery_outbox SET projection_hash=?,payload_json=JSON_SET(payload_json,'$.projection_payload',CAST(? AS JSON),'$.projection_hash',CAST(? AS JSON)) WHERE episode_id=? AND operation_id=?`, legacyHash, string(legacyPayload), legacyHash, episode.ID, operation.OperationID); err != nil {
		t.Fatal("persist legacy pending payload through MySQL JSON:", err)
	}
	if _, err := fixture.store.db.ExecContext(ctx, `UPDATE funding_recovery_operations SET receipt_json=? WHERE episode_id=? AND operation_id=?`, receiptJSON, episode.ID, operation.OperationID); err != nil {
		t.Fatal("persist legacy receipt hash:", err)
	}
	legacyOutbox, found, err := fixture.store.GetRecoveryOutbox(ctx, episode.ID, operation.OperationID)
	if err != nil || !found || legacyOutbox.State != accounting.RecoveryProjectionPending || legacyOutbox.ProjectionHash != legacyHash {
		t.Fatalf("read historical pending outbox: outbox=%+v found=%t err=%v", legacyOutbox, found, err)
	}
	if bytes.Equal(legacyPayload, legacyOutbox.ProjectionPayload) {
		t.Fatal("MySQL native JSON did not normalize the historical nested projection representation")
	}
	fixture.markProjected(t, legacyOutbox)
	fixture.assertProjectedState(t, episode, operation, identity)
}

func TestMySQLFundingRecoveryOutboxReorderedLegacyProjectionWithOmittedNilChildrenFailsClosed(t *testing.T) {
	fixture := newMySQLFundingRecoveryOutboxFixture(t)
	ctx := fixture.ctx
	from := time.Date(2026, 10, 13, 1, 0, 0, 0, time.UTC)
	episode, operation, identity := fixture.applyCursorOperation(t, "legacy-no-children", "BTCUSDT", from)
	outbox, found, err := fixture.store.GetRecoveryOutbox(ctx, episode.ID, operation.OperationID)
	if err != nil || !found || len(operation.Children) != 0 {
		t.Fatalf("read nil-children pending outbox: found=%t children=%+v err=%v", found, operation.Children, err)
	}

	// Prior writers could omit a nil children slice entirely. Preserve the
	// historical raw-byte hash, then let MySQL's native JSON storage reorder
	// keys and normalize the nested payload as it would across a restart.
	var projection struct {
		Episode      accounting.RecoveryEpisode       `json:"episode"`
		Events       []accounting.RecoveryEvent       `json:"events"`
		SourceCursor *accounting.RecoverySourceCursor `json:"source_cursor"`
	}
	if err := json.Unmarshal(outbox.ProjectionPayload, &projection); err != nil || projection.SourceCursor == nil || projection.SourceCursor.Revision != 1 || len(projection.Events) != 1 {
		t.Fatalf("projection fixture is incomplete: projection=%+v err=%v", projection, err)
	}
	legacyPayload, err := json.Marshal(struct {
		Episode      accounting.RecoveryEpisode       `json:"episode"`
		Events       []accounting.RecoveryEvent       `json:"events"`
		SourceCursor *accounting.RecoverySourceCursor `json:"source_cursor,omitempty"`
	}{
		Episode:      projection.Episode,
		Events:       projection.Events,
		SourceCursor: projection.SourceCursor,
	})
	if err != nil {
		t.Fatal("marshal historical projection with source cursor:", err)
	}
	legacyHash := canonicalHash(legacyPayload)
	receipt, found, err := fixture.store.GetRecoveryReceipt(ctx, episode.ID, operation.OperationID)
	if err != nil || !found {
		t.Fatalf("read receipt before legacy payload mutation: found=%t err=%v", found, err)
	}
	receipt.ProjectionHash = legacyHash
	receiptJSON, err := json.Marshal(receipt)
	if err != nil {
		t.Fatal("marshal historical receipt:", err)
	}
	if _, err := fixture.store.db.ExecContext(ctx, `UPDATE funding_recovery_outbox SET projection_hash=?,payload_json=JSON_SET(payload_json,'$.projection_payload',CAST(? AS JSON),'$.projection_hash',CAST(? AS JSON)) WHERE episode_id=? AND operation_id=?`, legacyHash, string(legacyPayload), legacyHash, episode.ID, operation.OperationID); err != nil {
		t.Fatal("persist reordered historical projection through MySQL JSON:", err)
	}
	if _, err := fixture.store.db.ExecContext(ctx, `UPDATE funding_recovery_operations SET receipt_json=? WHERE episode_id=? AND operation_id=?`, receiptJSON, episode.ID, operation.OperationID); err != nil {
		t.Fatal("persist historical receipt hash:", err)
	}
	stored, found, err := fixture.store.GetRecoveryOutbox(ctx, episode.ID, operation.OperationID)
	if err != nil || !found || stored.State != accounting.RecoveryProjectionPending || stored.ProjectionHash != legacyHash {
		t.Fatalf("read legacy nil-children pending outbox: outbox=%+v found=%t err=%v", stored, found, err)
	}
	var storedFields map[string]json.RawMessage
	if err := json.Unmarshal(stored.ProjectionPayload, &storedFields); err != nil {
		t.Fatal("decode normalized legacy projection:", err)
	}
	if _, exists := storedFields["children"]; exists {
		t.Fatalf("MySQL JSON round-trip unexpectedly restored omitted nil children: %s", stored.ProjectionPayload)
	}
	if bytes.Equal(stored.ProjectionPayload, legacyPayload) {
		t.Fatal("MySQL native JSON did not reorder/normalize the historical payload bytes")
	}
	if err := VerifyFundingRecoveryProjectionHash(stored.ProjectionPayload, stored.ProjectionHash); err != nil {
		t.Fatalf("verify MySQL-normalized historical hash with omitted nil children: %v", err)
	}
	if err := VerifyFundingRecoveryProjectionHash(stored.ProjectionPayload, stored.ProjectionHash); err != nil {
		t.Fatalf("verify readable historical nil-children payload hash: %v", err)
	}
	fixture.prepareProjectionMarker(&stored)
	if err := fixture.store.MarkRecoveryProjected(ctx, fixture.generation, stored); err == nil || !strings.Contains(err.Error(), "manual reconciliation") {
		t.Fatalf("unbound historical runtime revision should require manual reconciliation: %v", err)
	}
	fixture.assertPendingState(t, episode, operation, identity)
}

func TestMySQLFundingRecoveryOutboxSemanticTamperingRejectedWithoutProjectionSideEffects(t *testing.T) {
	fixture := newMySQLFundingRecoveryOutboxFixture(t)
	ctx := fixture.ctx
	from := time.Date(2026, 10, 13, 2, 0, 0, 0, time.UTC)
	episode, operation, identity := fixture.applyCursorOperation(t, "tamper", "ETHUSDT", from)
	beforeEpisode, found, err := fixture.store.GetRecoveryEpisode(ctx, episode.ID)
	if err != nil || !found {
		t.Fatalf("read episode before tamper attempt: found=%t err=%v", found, err)
	}
	beforeReceipt, found, err := fixture.store.GetRecoveryReceipt(ctx, episode.ID, operation.OperationID)
	if err != nil || !found || beforeReceipt.State != accounting.RecoveryLedgerCommitted {
		t.Fatalf("read pending receipt before tamper attempt: receipt=%+v found=%t err=%v", beforeReceipt, found, err)
	}
	beforeCursor, found, err := fixture.store.GetRecoverySourceCursor(ctx, identity)
	if err != nil || !found || beforeCursor.Revision != 1 {
		t.Fatalf("read cursor before tamper attempt: cursor=%+v found=%t err=%v", beforeCursor, found, err)
	}
	beforeEvents, err := fixture.store.ListRecoveryEvents(ctx, episode.ID)
	if err != nil || len(beforeEvents) != 1 {
		t.Fatalf("read ledger before tamper attempt: events=%+v err=%v", beforeEvents, err)
	}
	beforeHold, found, err := fixture.store.GetRecoveryHold(ctx, episode.ID, episode.RecoveryGeneration)
	if err != nil || !found || beforeHold.State != "held" {
		t.Fatalf("read hold before tamper attempt: hold=%+v found=%t err=%v", beforeHold, found, err)
	}

	alteredAmount, _ := json.Marshal("-999.99")
	if _, err := fixture.store.db.ExecContext(ctx, `UPDATE funding_recovery_outbox SET payload_json=JSON_SET(payload_json,'$.projection_payload.events[0].amount',CAST(? AS JSON)) WHERE episode_id=? AND operation_id=?`, alteredAmount, episode.ID, operation.OperationID); err != nil {
		t.Fatal("tamper semantic ledger amount in native JSON payload:", err)
	}
	tampered, found, err := fixture.store.GetRecoveryOutbox(ctx, episode.ID, operation.OperationID)
	if err != nil || !found || tampered.State != accounting.RecoveryProjectionPending {
		t.Fatalf("read semantically tampered pending outbox: outbox=%+v found=%t err=%v", tampered, found, err)
	}
	var tamperedProjection struct {
		Events []accounting.RecoveryEvent `json:"events"`
	}
	if err := json.Unmarshal(tampered.ProjectionPayload, &tamperedProjection); err != nil || len(tamperedProjection.Events) != 1 || tamperedProjection.Events[0].Amount != "-999.99" {
		t.Fatalf("MySQL JSON semantic tamper was not observable in the projection: events=%+v err=%v", tamperedProjection.Events, err)
	}
	fixture.prepareProjectionMarker(&tampered)
	if err := fixture.store.MarkRecoveryProjected(ctx, fixture.generation, tampered); err == nil {
		t.Fatal("MarkRecoveryProjected accepted a semantically altered payload")
	}

	afterEpisode, found, err := fixture.store.GetRecoveryEpisode(ctx, episode.ID)
	if err != nil || !found || afterEpisode.Revision != beforeEpisode.Revision || afterEpisode.State != accounting.RecoveryLedgerCommitted {
		t.Fatalf("tamper rejection changed episode: before=%+v after=%+v found=%t err=%v", beforeEpisode, afterEpisode, found, err)
	}
	afterReceipt, found, err := fixture.store.GetRecoveryReceipt(ctx, episode.ID, operation.OperationID)
	if err != nil || !found || afterReceipt != beforeReceipt {
		t.Fatalf("tamper rejection changed receipt: before=%+v after=%+v found=%t err=%v", beforeReceipt, afterReceipt, found, err)
	}
	afterOutbox, found, err := fixture.store.GetRecoveryOutbox(ctx, episode.ID, operation.OperationID)
	if err != nil || !found || afterOutbox.State != accounting.RecoveryProjectionPending || afterOutbox.ProjectionHash != tampered.ProjectionHash {
		t.Fatalf("tamper rejection changed indexed outbox state/hash: outbox=%+v found=%t err=%v", afterOutbox, found, err)
	}
	afterCursor, found, err := fixture.store.GetRecoverySourceCursor(ctx, identity)
	if err != nil || !found || afterCursor.Revision != beforeCursor.Revision || afterCursor.EvidenceHash != beforeCursor.EvidenceHash {
		t.Fatalf("tamper rejection changed source cursor: before=%+v after=%+v found=%t err=%v", beforeCursor, afterCursor, found, err)
	}
	afterHold, found, err := fixture.store.GetRecoveryHold(ctx, episode.ID, episode.RecoveryGeneration)
	if err != nil || !found || afterHold.State != "held" || afterHold.Revision != beforeHold.Revision {
		t.Fatalf("tamper rejection changed or released recovery hold: before=%+v after=%+v found=%t err=%v", beforeHold, afterHold, found, err)
	}
	afterEvents, err := fixture.store.ListRecoveryEvents(ctx, episode.ID)
	if err != nil || len(afterEvents) != len(beforeEvents) || afterEvents[0].Amount != beforeEvents[0].Amount || afterEvents[0].ID != beforeEvents[0].ID {
		t.Fatalf("tamper rejection changed durable ledger: before=%+v after=%+v err=%v", beforeEvents, afterEvents, err)
	}
	var receiptCount, outboxCount, ledgerCount int
	if err := fixture.store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM funding_recovery_operations WHERE episode_id=?`, episode.ID).Scan(&receiptCount); err != nil {
		t.Fatal("count receipt after tamper rejection:", err)
	}
	if err := fixture.store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM funding_recovery_outbox WHERE episode_id=?`, episode.ID).Scan(&outboxCount); err != nil {
		t.Fatal("count outbox after tamper rejection:", err)
	}
	if err := fixture.store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM funding_recovery_events WHERE episode_id=?`, episode.ID).Scan(&ledgerCount); err != nil {
		t.Fatal("count ledger rows after tamper rejection:", err)
	}
	if receiptCount != 1 || outboxCount != 1 || ledgerCount != 1 {
		t.Fatalf("tamper rejection left partial/duplicate durable rows: receipts=%d outboxes=%d ledger=%d", receiptCount, outboxCount, ledgerCount)
	}
}

func TestMySQLFundingRecoveryOutboxCursorEpisodeAndUnknownFieldTamperingRejected(t *testing.T) {
	fixture := newMySQLFundingRecoveryOutboxFixture(t)
	from := time.Date(2026, 10, 13, 4, 0, 0, 0, time.UTC)
	cases := []struct {
		name   string
		symbol string
		path   string
		value  string
	}{
		{name: "source cursor evidence", symbol: "SOLUSDT", path: "$.projection_payload.source_cursor.evidence_hash", value: strings.Repeat("f", 64)},
		{name: "episode owner-visible identity", symbol: "XRPUSDT", path: "$.projection_payload.episode.bot_id", value: "tampered-bot"},
		{name: "unknown projection field", symbol: "ADAUSDT", path: "$.projection_payload.unrecognized_integrity_field", value: "must-not-be-ignored"},
	}
	for index, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			episode, operation, identity := fixture.applyCursorOperation(t, fmt.Sprintf("tamper-%d", index), testCase.symbol, from.Add(time.Duration(index)*2*time.Hour))
			assertMySQLTamperedProjectionRejected(t, fixture, episode, operation, identity, testCase.path, testCase.value)
		})
	}
}

func assertMySQLTamperedProjectionRejected(
	t *testing.T,
	fixture mysqlFundingRecoveryOutboxFixture,
	episode accounting.RecoveryEpisode,
	operation accounting.RecoveryOperation,
	identity accounting.RecoverySourceIdentity,
	jsonPath, replacement string,
) {
	t.Helper()
	ctx := fixture.ctx
	beforeEpisode, found, err := fixture.store.GetRecoveryEpisode(ctx, episode.ID)
	if err != nil || !found {
		t.Fatalf("read episode before projection tamper: found=%t err=%v", found, err)
	}
	beforeReceipt, found, err := fixture.store.GetRecoveryReceipt(ctx, episode.ID, operation.OperationID)
	if err != nil || !found || beforeReceipt.State != accounting.RecoveryLedgerCommitted {
		t.Fatalf("read receipt before projection tamper: receipt=%+v found=%t err=%v", beforeReceipt, found, err)
	}
	beforeCursor, found, err := fixture.store.GetRecoverySourceCursor(ctx, identity)
	if err != nil || !found || beforeCursor.Revision != 1 {
		t.Fatalf("read cursor before projection tamper: cursor=%+v found=%t err=%v", beforeCursor, found, err)
	}
	beforeHold, found, err := fixture.store.GetRecoveryHold(ctx, episode.ID, episode.RecoveryGeneration)
	if err != nil || !found || beforeHold.State != "held" {
		t.Fatalf("read hold before projection tamper: hold=%+v found=%t err=%v", beforeHold, found, err)
	}
	beforeEvents, err := fixture.store.ListRecoveryEvents(ctx, episode.ID)
	if err != nil || len(beforeEvents) != 1 {
		t.Fatalf("read ledger before projection tamper: events=%+v err=%v", beforeEvents, err)
	}
	valueJSON, err := json.Marshal(replacement)
	if err != nil {
		t.Fatal("marshal semantic replacement:", err)
	}
	query := `UPDATE funding_recovery_outbox SET payload_json=JSON_SET(payload_json,?,CAST(? AS JSON)) WHERE episode_id=? AND operation_id=?`
	if _, err := fixture.store.db.ExecContext(ctx, query, jsonPath, string(valueJSON), episode.ID, operation.OperationID); err != nil {
		t.Fatalf("tamper projection semantic field %s: %v", jsonPath, err)
	}
	tampered, found, err := fixture.store.GetRecoveryOutbox(ctx, episode.ID, operation.OperationID)
	if err != nil || !found || tampered.State != accounting.RecoveryProjectionPending {
		t.Fatalf("read tampered pending outbox: outbox=%+v found=%t err=%v", tampered, found, err)
	}
	if tampered.ProjectionHash != beforeReceipt.ProjectionHash {
		t.Fatalf("test changed trusted outbox hash while tampering payload: got=%s want=%s", tampered.ProjectionHash, beforeReceipt.ProjectionHash)
	}
	fixture.prepareProjectionMarker(&tampered)
	if err := fixture.store.MarkRecoveryProjected(ctx, fixture.generation, tampered); err == nil {
		t.Fatalf("MarkRecoveryProjected accepted tampered semantic field %s (unknown fields must not be silently ignored)", jsonPath)
	}

	afterEpisode, found, err := fixture.store.GetRecoveryEpisode(ctx, episode.ID)
	if err != nil || !found || afterEpisode.Revision != beforeEpisode.Revision || afterEpisode.State != beforeEpisode.State {
		t.Fatalf("rejected tamper changed episode: before=%+v after=%+v found=%t err=%v", beforeEpisode, afterEpisode, found, err)
	}
	afterReceipt, found, err := fixture.store.GetRecoveryReceipt(ctx, episode.ID, operation.OperationID)
	if err != nil || !found || afterReceipt != beforeReceipt {
		t.Fatalf("rejected tamper changed receipt: before=%+v after=%+v found=%t err=%v", beforeReceipt, afterReceipt, found, err)
	}
	afterOutbox, found, err := fixture.store.GetRecoveryOutbox(ctx, episode.ID, operation.OperationID)
	if err != nil || !found || afterOutbox.State != accounting.RecoveryProjectionPending || afterOutbox.ProjectionHash != beforeReceipt.ProjectionHash {
		t.Fatalf("rejected tamper changed outbox projection state/hash: outbox=%+v found=%t err=%v", afterOutbox, found, err)
	}
	afterCursor, found, err := fixture.store.GetRecoverySourceCursor(ctx, identity)
	if err != nil || !found || afterCursor.Revision != beforeCursor.Revision || afterCursor.EvidenceHash != beforeCursor.EvidenceHash {
		t.Fatalf("rejected tamper changed cursor: before=%+v after=%+v found=%t err=%v", beforeCursor, afterCursor, found, err)
	}
	afterHold, found, err := fixture.store.GetRecoveryHold(ctx, episode.ID, episode.RecoveryGeneration)
	if err != nil || !found || afterHold.State != "held" || afterHold.Revision != beforeHold.Revision {
		t.Fatalf("rejected tamper changed/released hold: before=%+v after=%+v found=%t err=%v", beforeHold, afterHold, found, err)
	}
	afterEvents, err := fixture.store.ListRecoveryEvents(ctx, episode.ID)
	if err != nil || len(afterEvents) != len(beforeEvents) || afterEvents[0] != beforeEvents[0] {
		t.Fatalf("rejected tamper changed ledger: before=%+v after=%+v err=%v", beforeEvents, afterEvents, err)
	}
}

type mysqlFundingRecoveryOutboxFixture struct {
	ctx        context.Context
	store      *SQLStorage
	generation FundingCarryRuntimeGeneration
	scope      string
}

func newMySQLFundingRecoveryOutboxFixture(t *testing.T) mysqlFundingRecoveryOutboxFixture {
	t.Helper()
	dsn := os.Getenv("QUANTMESH_MYSQL_TEST_DSN")
	if dsn == "" {
		t.Skip("requires QUANTMESH_MYSQL_TEST_DSN and explicit disposable-schema opt-in; MySQL outbox projection not run")
	}
	allowDestructive := os.Getenv("QUANTMESH_MYSQL_TEST_ALLOW_DESTRUCTIVE_SCHEMA") == "1"
	baseConfig, err := orderReconciliationMySQLFixtureConfig(dsn, allowDestructive)
	if err != nil {
		t.Skipf("MySQL outbox projection refused unsafe fixture DSN: %v", err)
	}
	if !allowDestructive {
		t.Skip("requires QUANTMESH_MYSQL_TEST_ALLOW_DESTRUCTIVE_SCHEMA=1; MySQL outbox projection not run")
	}

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	t.Cleanup(cancel)
	admin, err := sql.Open("mysql", baseConfig.FormatDSN())
	if err != nil {
		t.Fatal("open MySQL outbox fixture administrator connection:", err)
	}
	t.Cleanup(func() { _ = admin.Close() })
	if err := admin.PingContext(ctx); err != nil {
		t.Fatal("connect MySQL outbox fixture:", err)
	}
	var version string
	if err := admin.QueryRowContext(ctx, `SELECT VERSION()`).Scan(&version); err != nil {
		t.Fatal("read MySQL outbox server version:", err)
	}
	if !strings.HasPrefix(version, "8.") {
		t.Fatalf("MySQL outbox fixture requires MySQL 8, got %q", version)
	}
	schema := fmt.Sprintf("quantmesh_audit_funding_outbox_%d", time.Now().UTC().UnixNano())
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE `"+schema+"` CHARACTER SET utf8mb4 COLLATE utf8mb4_unicode_ci"); err != nil {
		t.Fatal("create isolated MySQL outbox schema:", err)
	}
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cleanupCancel()
		if _, err := admin.ExecContext(cleanupCtx, "DROP DATABASE `"+schema+"`"); err != nil {
			t.Errorf("drop isolated MySQL outbox schema %q: %v", schema, err)
		}
	})
	config := *baseConfig
	config.DBName = schema
	db, err := sql.Open("mysql", config.FormatDSN())
	if err != nil {
		t.Fatal("open MySQL outbox storage connection:", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	t.Cleanup(func() { _ = db.Close() })
	if err := db.PingContext(ctx); err != nil {
		t.Fatal("connect MySQL outbox storage:", err)
	}
	store := &SQLStorage{db: db, dbType: "mysql", commitProbe: db}
	if err := migrateFundingCarryRuntimeGeneration(db, "mysql"); err != nil {
		t.Fatal("migrate MySQL runtime generation:", err)
	}
	if err := store.MigrateFundingRecovery(ctx); err != nil {
		t.Fatal("migrate MySQL funding recovery:", err)
	}
	if err := store.SetRecoverySourceWindowVerifier(testRecoverySourceWindowVerifier{}); err != nil {
		t.Fatal("configure trusted source-window verifier:", err)
	}
	scope := fmt.Sprintf("%064x", time.Now().UTC().UnixNano())
	generation, err := store.ClaimFundingCarryRuntimeGeneration(ctx, []string{scope})
	if err != nil {
		t.Fatal("claim MySQL recovery generation:", err)
	}
	return mysqlFundingRecoveryOutboxFixture{ctx: ctx, store: store, generation: generation, scope: scope}
}

func (f mysqlFundingRecoveryOutboxFixture) applyCursorOperation(t *testing.T, label, symbol string, from time.Time) (accounting.RecoveryEpisode, accounting.RecoveryOperation, accounting.RecoverySourceIdentity) {
	t.Helper()
	episode := accounting.RecoveryEpisode{
		ID:  "mysql-outbox-" + label + "-" + fmt.Sprintf("%x", time.Now().UTC().UnixNano()),
		Bot: "bot-mysql-outbox-" + label, Strategy: "FundingCarry", AccountScopes: []string{f.scope},
		OwnerGeneration: f.generation.ID(), Revision: 1, RecoveryGeneration: 1,
		NextActionOrdinal: 1, State: accounting.RecoveryPrepared,
	}
	if _, err := f.store.CreateRecoveryEpisode(f.ctx, f.generation, episode); err != nil {
		t.Fatal("create MySQL recovery episode:", err)
	}
	window := sourceWindowForSymbol(f.scope, symbol, from, from.Add(time.Hour), 0, 1)
	window.Source = accounting.RecoverySourceBinanceFuturesIncomeV1
	window.InitialFrom = from
	window.HistoricalEvidenceHash = strings.Repeat("c", 64)
	operation := fundingOperation(episode, f.scope, "mysql-outbox-"+label, 1, window, "income-"+label, "-1.25")
	operation.Events[0].SourceHash = strings.Repeat("d", 64)
	if _, replayed, err := f.store.ApplyRecoveryOperation(f.ctx, f.generation, operation); err != nil || replayed {
		t.Fatalf("apply MySQL source cursor operation: replayed=%t err=%v", replayed, err)
	}
	return episode, operation, window.Identity()
}

func (f mysqlFundingRecoveryOutboxFixture) markProjected(t *testing.T, outbox accounting.RecoveryOutbox) {
	t.Helper()
	f.prepareProjectionMarker(&outbox)
	if err := f.store.MarkRecoveryProjected(f.ctx, f.generation, outbox); err != nil {
		t.Fatalf("project MySQL JSON-normalized pending outbox: %v", err)
	}
}

func (f mysqlFundingRecoveryOutboxFixture) prepareProjectionMarker(outbox *accounting.RecoveryOutbox) {
	outbox.AppliedOperationID = outbox.OperationID
	outbox.AppliedRevision = outbox.TargetEpisodeRevision
	outbox.AppliedHash = outbox.ProjectionHash
	outbox.State = accounting.RecoveryProjected
}

func (f mysqlFundingRecoveryOutboxFixture) assertProjectedState(t *testing.T, episode accounting.RecoveryEpisode, operation accounting.RecoveryOperation, identity accounting.RecoverySourceIdentity) {
	t.Helper()
	projectedEpisode, found, err := f.store.GetRecoveryEpisode(f.ctx, episode.ID)
	if err != nil || !found || projectedEpisode.State != accounting.RecoveryProjected || projectedEpisode.Revision != 3 {
		t.Fatalf("projected MySQL episode readback: episode=%+v found=%t err=%v", projectedEpisode, found, err)
	}
	receipt, found, err := f.store.GetRecoveryReceipt(f.ctx, episode.ID, operation.OperationID)
	if err != nil || !found || receipt.State != accounting.RecoveryProjected || receipt.EpisodeRevision != projectedEpisode.Revision {
		t.Fatalf("projected MySQL receipt readback: receipt=%+v found=%t err=%v", receipt, found, err)
	}
	outbox, found, err := f.store.GetRecoveryOutbox(f.ctx, episode.ID, operation.OperationID)
	if err != nil || !found || outbox.State != accounting.RecoveryProjected || outbox.AppliedOperationID != operation.OperationID ||
		outbox.AppliedRevision != 2 || outbox.AppliedHash != receipt.ProjectionHash || outbox.ProjectionHash != receipt.ProjectionHash {
		t.Fatalf("projected MySQL outbox readback: outbox=%+v found=%t err=%v", outbox, found, err)
	}
	cursor, found, err := f.store.GetRecoverySourceCursor(f.ctx, identity)
	if err != nil || !found || cursor.Revision != 1 || cursor.EpisodeID != episode.ID || cursor.EvidenceHash != operation.SourceWindow.EvidenceHash || cursor.HistoricalEvidenceHash != operation.SourceWindow.HistoricalEvidenceHash {
		t.Fatalf("projected MySQL source cursor readback: cursor=%+v found=%t err=%v", cursor, found, err)
	}
	hold, found, err := f.store.GetRecoveryHold(f.ctx, episode.ID, episode.RecoveryGeneration)
	if err != nil || !found || hold.State != "held" {
		t.Fatalf("projected MySQL recovery hold was not retained: hold=%+v found=%t err=%v", hold, found, err)
	}
	events, err := f.store.ListRecoveryEvents(f.ctx, episode.ID)
	if err != nil || len(events) != 1 || events[0].ExternalID != operation.Events[0].ExternalID || events[0].Amount != operation.Events[0].Amount || events[0].SourceHash != operation.Events[0].SourceHash {
		t.Fatalf("projected MySQL ledger readback: events=%+v err=%v", events, err)
	}
}

func (f mysqlFundingRecoveryOutboxFixture) assertPendingState(t *testing.T, episode accounting.RecoveryEpisode, operation accounting.RecoveryOperation, identity accounting.RecoverySourceIdentity) {
	t.Helper()
	pendingEpisode, found, err := f.store.GetRecoveryEpisode(f.ctx, episode.ID)
	if err != nil || !found || pendingEpisode.State != accounting.RecoveryLedgerCommitted || pendingEpisode.Revision != 2 {
		t.Fatalf("fail-closed legacy episode changed: episode=%+v found=%t err=%v", pendingEpisode, found, err)
	}
	receipt, found, err := f.store.GetRecoveryReceipt(f.ctx, episode.ID, operation.OperationID)
	if err != nil || !found || receipt.State != accounting.RecoveryLedgerCommitted || receipt.EpisodeRevision != 2 {
		t.Fatalf("fail-closed legacy receipt changed: receipt=%+v found=%t err=%v", receipt, found, err)
	}
	outbox, found, err := f.store.GetRecoveryOutbox(f.ctx, episode.ID, operation.OperationID)
	if err != nil || !found || outbox.State != accounting.RecoveryProjectionPending || outbox.AppliedOperationID != "" || outbox.AppliedRevision != 0 || outbox.AppliedHash != "" {
		t.Fatalf("fail-closed legacy outbox changed: outbox=%+v found=%t err=%v", outbox, found, err)
	}
	cursor, found, err := f.store.GetRecoverySourceCursor(f.ctx, identity)
	if err != nil || !found || cursor.Revision != 1 || cursor.EpisodeID != episode.ID || cursor.EvidenceHash != operation.SourceWindow.EvidenceHash {
		t.Fatalf("fail-closed legacy cursor changed: cursor=%+v found=%t err=%v", cursor, found, err)
	}
	hold, found, err := f.store.GetRecoveryHold(f.ctx, episode.ID, episode.RecoveryGeneration)
	if err != nil || !found || hold.State != "held" {
		t.Fatalf("fail-closed legacy hold changed: hold=%+v found=%t err=%v", hold, found, err)
	}
	events, err := f.store.ListRecoveryEvents(f.ctx, episode.ID)
	if err != nil || len(events) != 1 || events[0].ExternalID != operation.Events[0].ExternalID || events[0].Amount != operation.Events[0].Amount {
		t.Fatalf("fail-closed legacy ledger changed: events=%+v err=%v", events, err)
	}
}

func readMySQLRecoveryOutboxJSON(t *testing.T, ctx context.Context, store *SQLStorage, episodeID, operationID string) (string, []byte) {
	t.Helper()
	var jsonType string
	var raw []byte
	if err := store.db.QueryRowContext(ctx, `SELECT JSON_TYPE(payload_json),payload_json FROM funding_recovery_outbox WHERE episode_id=? AND operation_id=?`, episodeID, operationID).Scan(&jsonType, &raw); err != nil {
		t.Fatal("read native MySQL JSON outbox payload:", err)
	}
	return jsonType, raw
}

func reorderProjectionJSONKeys(t *testing.T, raw []byte) []byte {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal("decode projection fields for historical encoding:", err)
	}
	reordered, err := json.Marshal(fields)
	if err != nil {
		t.Fatal("marshal historical projection field order:", err)
	}
	return reordered
}

func legacyFundingRecoveryProjectionHash(t *testing.T, raw []byte) string {
	t.Helper()
	var projection struct {
		Episode      accounting.RecoveryEpisode       `json:"episode"`
		Children     []accounting.RecoveryChild       `json:"children"`
		Events       []accounting.RecoveryEvent       `json:"events"`
		SourceCursor *accounting.RecoverySourceCursor `json:"source_cursor,omitempty"`
	}
	if err := json.Unmarshal(raw, &projection); err != nil {
		t.Fatal("decode projection into historical release shape:", err)
	}
	legacyBytes, err := json.Marshal(projection)
	if err != nil {
		t.Fatal("marshal historical recovery projection:", err)
	}
	return canonicalHash(legacyBytes)
}

func assertMySQLProjectionCursorPayload(t *testing.T, raw []byte, identity accounting.RecoverySourceIdentity, expectedRevision uint64) {
	t.Helper()
	var projection struct {
		SourceCursor *accounting.RecoverySourceCursor `json:"source_cursor"`
	}
	if err := json.Unmarshal(raw, &projection); err != nil {
		t.Fatal("decode cursor projection from native JSON outbox:", err)
	}
	if projection.SourceCursor == nil || projection.SourceCursor.Revision != expectedRevision ||
		projection.SourceCursor.AccountScope != identity.AccountScope || projection.SourceCursor.Symbol != identity.Symbol ||
		projection.SourceCursor.Source != identity.Source || projection.SourceCursor.Asset != identity.Asset {
		t.Fatalf("native JSON round-trip cursor projection mismatch: %+v", projection.SourceCursor)
	}
}
