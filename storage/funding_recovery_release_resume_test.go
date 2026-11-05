package storage

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"quantmesh/accounting"
)

type releaseResumeFixture struct {
	stores          [2]*SQLStorage
	oldOwner, owner FundingCarryRuntimeGeneration
	auth            accounting.RecoveryReleaseResumeAuthorization
	release         accounting.RecoveryReleaseAuthorization
	preview         FundingRecoveryReleaseResumePreview
}

func newSQLiteReleaseResumeStores(t *testing.T) [2]*SQLStorage {
	t.Helper()
	path := filepath.Join(t.TempDir(), "resume.db")
	var stores [2]*SQLStorage
	for i := range stores {
		db, err := sql.Open("sqlite3", path+"?_busy_timeout=10000&_journal_mode=WAL")
		if err != nil {
			t.Fatal(err)
		}
		db.SetMaxOpenConns(1)
		t.Cleanup(func() { _ = db.Close() })
		stores[i] = &SQLStorage{db: db, dbType: "sqlite"}
	}
	if err := migrateFundingCarryRuntimeGeneration(stores[0].db, "sqlite"); err != nil {
		t.Fatal(err)
	}
	if err := stores[0].MigrateFundingRecovery(t.Context()); err != nil {
		t.Fatal(err)
	}
	return stores
}

func prepareReleaseResumeFixture(t *testing.T, stores [2]*SQLStorage, released bool) releaseResumeFixture {
	t.Helper()
	ctx, store := t.Context(), stores[0]
	scope := fmt.Sprintf("%064x", 9901)
	old, err := store.ClaimFundingCarryRuntimeGeneration(ctx, []string{scope})
	if err != nil {
		t.Fatal(err)
	}
	ep := accounting.RecoveryEpisode{ID: "release-resume-episode", Bot: "resume-bot", Strategy: "FundingCarry", AccountScopes: []string{scope},
		OwnerGeneration: old.ID(), Revision: 1, RecoveryGeneration: 1, NextActionOrdinal: 1, State: accounting.RecoveryPrepared,
		ActionPlan: []accounting.RecoveryAction{{ID: "unused-action", Ordinal: 1, Kind: accounting.RecoveryActionBorrow, Role: "borrow", AccountScope: scope, Asset: "BTC", Amount: "0.01", OwnerGeneration: old.ID(), Status: "authorized"}}}
	if _, err = store.CreateRecoveryEpisode(ctx, old, ep); err != nil {
		t.Fatal(err)
	}
	if _, err = store.CreateRecoveryPermit(ctx, old, accounting.OneShotPermit{ID: "unused-permit", EpisodeID: ep.ID, Generation: 1,
		ActionID: "unused-action", ActionOrdinal: 1, Kind: accounting.RecoveryActionBorrow, AccountScope: scope, OwnerGeneration: old.ID(),
		LeaseID: 1, ExpectedRevision: 1, State: accounting.PermitAuthorized}); err != nil {
		t.Fatal(err)
	}
	if err = store.SetRecoverySourceWindowVerifier(testRecoverySourceWindowVerifier{}); err != nil {
		t.Fatal(err)
	}
	from := time.Date(2026, 10, 20, 0, 0, 0, 0, time.UTC)
	window := sourceWindowForSymbol(scope, "BTCUSDT", from, from.Add(time.Hour), 0, 1)
	op := fundingOperation(ep, scope, "project-before-release", 1, window, "resume-income", "-1.25")
	if _, _, err = store.ApplyRecoveryOperation(ctx, old, op); err != nil {
		t.Fatal(err)
	}
	outbox, found, err := store.GetRecoveryOutbox(ctx, ep.ID, op.OperationID)
	if err != nil || !found {
		t.Fatalf("outbox: found=%v err=%v", found, err)
	}
	outbox.State = accounting.RecoveryProjected
	outbox.AppliedOperationID, outbox.AppliedRevision, outbox.AppliedHash = outbox.OperationID, outbox.TargetEpisodeRevision, outbox.ProjectionHash
	if err = store.MarkRecoveryProjected(ctx, old, outbox); err != nil {
		t.Fatal(err)
	}
	ep, _, err = store.GetRecoveryEpisode(ctx, ep.ID)
	if err != nil {
		t.Fatal(err)
	}
	hold, _, err := store.GetRecoveryHold(ctx, ep.ID, ep.RecoveryGeneration)
	if err != nil {
		t.Fatal(err)
	}
	release := accounting.RecoveryReleaseAuthorization{EpisodeID: ep.ID, OperationID: "original-release", Actor: "original-admin", AuditRef: "original-audit",
		EvidenceHash: strings.Repeat("a", 64), ExposureClearanceHash: strings.Repeat("b", 64), ProjectionHash: outbox.ProjectionHash,
		RuntimeRevision: outbox.ExpectedRuntimeRevision, OwnerGeneration: old.ID(), ExpectedEpisodeRevision: ep.Revision,
		RecoveryGeneration: ep.RecoveryGeneration, ExpectedHoldRevision: hold.Revision, Confirmed: true, ExposureClearanceConfirmed: true}
	if _, err = store.PrepareRecoveryRelease(ctx, old, release); err != nil {
		t.Fatal(err)
	}
	if released {
		if _, err = store.ReleaseRecoveryHold(ctx, old, release); err != nil {
			t.Fatal(err)
		}
	}
	owner, err := stores[1].ClaimFundingCarryRuntimeGeneration(ctx, []string{scope})
	if err != nil {
		t.Fatal(err)
	}
	preview, err := store.PreviewRecoveryReleaseResume(ctx, owner, ep.ID, release.OperationID)
	if err != nil {
		t.Fatalf("preview resume: %v", err)
	}
	auth := releaseResumeAuthFromPreview(preview, "resume-release")
	return releaseResumeFixture{stores: stores, oldOwner: old, owner: owner, auth: auth, release: release, preview: preview}
}

func releaseResumeAuthFromPreview(p FundingRecoveryReleaseResumePreview, operation string) accounting.RecoveryReleaseResumeAuthorization {
	return accounting.RecoveryReleaseResumeAuthorization{EpisodeID: p.Episode.ID, OperationID: operation, ReleaseOperationID: p.ReleaseReceipt.OperationID,
		ReleaseRequestHash: p.ReleaseReceipt.RequestHash, Actor: "resume-admin", AuditRef: "resume-audit", EvidenceHash: p.EvidenceHash,
		ExpectedOwnerGeneration: p.Episode.OwnerGeneration, OwnerGeneration: p.OwnerGeneration, AccountScopes: append([]string(nil), p.Episode.AccountScopes...),
		ExpectedEpisodeRevision: p.Episode.Revision, ExpectedHoldRevision: p.Hold.Revision, RecoveryGeneration: p.Episode.RecoveryGeneration, Confirmed: true}
}

func TestSQLiteFundingRecoveryReleaseResumeAtomicAndReplay(t *testing.T) {
	for _, released := range []bool{false, true} {
		t.Run(fmt.Sprintf("released=%t", released), func(t *testing.T) {
			f := prepareReleaseResumeFixture(t, newSQLiteReleaseResumeStores(t), released)
			testReleaseResumeAtomicAndReplay(t, f)
		})
	}
}

func testReleaseResumeAtomicAndReplay(t *testing.T, f releaseResumeFixture) {
	t.Helper()
	ctx, store := t.Context(), f.stores[0]
	if _, _, err := store.TakeOverRecoveryEpisode(ctx, f.owner, accounting.RecoveryOwnerTakeoverAuthorization{EpisodeID: f.auth.EpisodeID,
		OperationID: "ordinary-takeover", Actor: "admin", AuditRef: "audit", ExpectedOwnerGeneration: f.oldOwner.ID(), ExpectedEpisodeRevision: f.preview.Episode.Revision, Confirmed: true}); err == nil {
		t.Fatal("ordinary takeover accepted release state")
	}
	if _, err := store.ReleaseRecoveryHold(ctx, f.owner, f.release); err == nil {
		t.Fatal("ordinary release accepted new owner")
	}
	ep, hold, receipt, replay, err := store.ResumeRecoveryRelease(ctx, f.owner, f.auth)
	if err != nil || replay {
		t.Fatalf("resume: replay=%v err=%v", replay, err)
	}
	if ep.Revision != f.preview.Episode.Revision+1 || ep.OwnerGeneration != f.owner.ID() || ep.State != accounting.RecoveryReleased ||
		hold.State != "released" || hold.ReleasedByOperation != f.release.OperationID || len(ep.ReleaseResumptions) != 1 || len(ep.OwnerTakeovers) != 0 ||
		receipt.OperationID != f.auth.OperationID || receipt.ReleaseOperationID != f.release.OperationID || receipt.ReleaseRequestHash != f.preview.ReleaseReceipt.RequestHash {
		t.Fatalf("invalid resume result: episode=%+v hold=%+v receipt=%+v", ep, hold, receipt)
	}
	expectedOriginal := f.preview.ReleaseReceipt
	if f.preview.Episode.State == accounting.RecoveryReleasePending {
		expectedOriginal.State, expectedOriginal.EpisodeRevision = accounting.RecoveryReleased, ep.Revision
		if hold.Revision != f.preview.Hold.Revision+1 {
			t.Fatal("pending resume did not release hold exactly once")
		}
	} else if !reflect.DeepEqual(hold, f.preview.Hold) {
		t.Fatal("released cleanup transfer rewrote hold")
	}
	original, found, err := store.GetRecoveryReceipt(ctx, f.auth.EpisodeID, f.release.OperationID)
	if err != nil || !found || original != expectedOriginal {
		t.Fatalf("original release identity changed: %+v, err=%v", original, err)
	}
	before := releaseResumeDurableState(t, store)
	for range 2 {
		gotEP, gotHold, gotReceipt, replayed, retryErr := f.stores[1].ResumeRecoveryRelease(ctx, f.owner, f.auth)
		if retryErr != nil || !replayed || !reflect.DeepEqual(gotEP, ep) || !reflect.DeepEqual(gotHold, hold) || gotReceipt != receipt {
			t.Fatalf("retry: replay=%v err=%v", replayed, retryErr)
		}
	}
	if after := releaseResumeDurableState(t, store); !reflect.DeepEqual(before, after) {
		t.Fatal("retry wrote durable data")
	}
	changed := f.auth
	changed.Actor = "different-admin"
	if _, _, _, _, err = store.ResumeRecoveryRelease(ctx, f.owner, changed); !errors.Is(err, accounting.ErrRecoveryOperationConflict) {
		t.Fatalf("changed request=%v", err)
	}
	if _, _, _, _, err = store.ResumeRecoveryRelease(ctx, f.oldOwner, f.auth); !errors.Is(err, ErrFundingCarryRuntimeGenerationLost) {
		t.Fatalf("old capability=%v", err)
	}
	// A later restart can transfer cleanup responsibility again. The original
	// release and hold remain byte-for-byte business-identical.
	next, err := f.stores[1].ClaimFundingCarryRuntimeGeneration(ctx, f.auth.AccountScopes)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err = store.ResumeRecoveryRelease(ctx, f.owner, f.auth); !errors.Is(err, ErrFundingCarryRuntimeGenerationLost) {
		t.Fatalf("stale retry=%v", err)
	}
	preview, err := store.PreviewRecoveryReleaseResume(ctx, next, ep.ID, f.release.OperationID)
	if err != nil {
		t.Fatal("preview later restart:", err)
	}
	_, nextHold, _, replayed, err := store.ResumeRecoveryRelease(ctx, next, releaseResumeAuthFromPreview(preview, "resume-second-restart"))
	if err != nil || replayed || !reflect.DeepEqual(nextHold, hold) {
		t.Fatalf("later restart resume: replay=%v err=%v", replayed, err)
	}
	original, _, err = store.GetRecoveryReceipt(ctx, ep.ID, f.release.OperationID)
	if err != nil || original != expectedOriginal {
		t.Fatal("later restart rewrote original release receipt")
	}
}

func releaseResumeDurableState(t *testing.T, store *SQLStorage) map[string][]string {
	t.Helper()
	result := map[string][]string{}
	queries := map[string]string{
		"episodes": `SELECT payload_json FROM funding_recovery_episodes ORDER BY episode_id`,
		"holds":    `SELECT payload_json FROM funding_recovery_holds ORDER BY hold_id`,
		"receipts": `SELECT receipt_json FROM funding_recovery_operations ORDER BY episode_id,operation_id`,
		"permits":  `SELECT payload_json FROM funding_recovery_permits ORDER BY permit_id`,
	}
	for name, query := range queries {
		rows, err := store.db.QueryContext(t.Context(), query)
		if err != nil {
			t.Fatal(err)
		}
		for rows.Next() {
			var raw string
			if err = rows.Scan(&raw); err != nil {
				t.Fatal(err)
			}
			result[name] = append(result[name], raw)
		}
		if err = rows.Err(); err != nil {
			t.Fatal(err)
		}
		if err = rows.Close(); err != nil {
			t.Fatal(err)
		}
	}
	return result
}

func TestSQLiteFundingRecoveryReleaseResumeAuthorizationBoundaries(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*accounting.RecoveryReleaseResumeAuthorization)
	}{
		{"confirmation", func(a *accounting.RecoveryReleaseResumeAuthorization) { a.Confirmed = false }},
		{"actor", func(a *accounting.RecoveryReleaseResumeAuthorization) { a.Actor = " " }},
		{"audit", func(a *accounting.RecoveryReleaseResumeAuthorization) { a.AuditRef = "" }},
		{"same operation", func(a *accounting.RecoveryReleaseResumeAuthorization) { a.OperationID = a.ReleaseOperationID }},
		{"bad release hash", func(a *accounting.RecoveryReleaseResumeAuthorization) { a.ReleaseRequestHash = "invalid" }},
		{"wrong release hash", func(a *accounting.RecoveryReleaseResumeAuthorization) { a.ReleaseRequestHash = strings.Repeat("f", 64) }},
		{"bad evidence", func(a *accounting.RecoveryReleaseResumeAuthorization) { a.EvidenceHash = "invalid" }},
		{"stale evidence", func(a *accounting.RecoveryReleaseResumeAuthorization) { a.EvidenceHash = strings.Repeat("f", 64) }},
		{"old owner", func(a *accounting.RecoveryReleaseResumeAuthorization) { a.ExpectedOwnerGeneration = "other-owner" }},
		{"unbound new owner", func(a *accounting.RecoveryReleaseResumeAuthorization) { a.OwnerGeneration = "other-owner" }},
		{"same owner", func(a *accounting.RecoveryReleaseResumeAuthorization) { a.OwnerGeneration = a.ExpectedOwnerGeneration }},
		{"episode CAS", func(a *accounting.RecoveryReleaseResumeAuthorization) { a.ExpectedEpisodeRevision++ }},
		{"hold CAS", func(a *accounting.RecoveryReleaseResumeAuthorization) { a.ExpectedHoldRevision++ }},
		{"recovery generation", func(a *accounting.RecoveryReleaseResumeAuthorization) { a.RecoveryGeneration++ }},
		{"episode overflow", func(a *accounting.RecoveryReleaseResumeAuthorization) {
			a.ExpectedEpisodeRevision = accounting.MaxDurableRecoveryRevision
		}},
		{"hold overflow", func(a *accounting.RecoveryReleaseResumeAuthorization) { a.ExpectedHoldRevision = ^uint64(0) }},
		{"empty scopes", func(a *accounting.RecoveryReleaseResumeAuthorization) { a.AccountScopes = nil }},
		{"other scope", func(a *accounting.RecoveryReleaseResumeAuthorization) {
			a.AccountScopes = []string{strings.Repeat("e", 64)}
		}},
		{"duplicate scopes", func(a *accounting.RecoveryReleaseResumeAuthorization) {
			a.AccountScopes = append(a.AccountScopes, a.AccountScopes[0])
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := prepareReleaseResumeFixture(t, newSQLiteReleaseResumeStores(t), false)
			before := releaseResumeDurableState(t, f.stores[0])
			tc.mutate(&f.auth)
			if _, _, _, _, err := f.stores[0].ResumeRecoveryRelease(t.Context(), f.owner, f.auth); err == nil {
				t.Fatal("accepted invalid authorization")
			}
			if !reflect.DeepEqual(before, releaseResumeDurableState(t, f.stores[0])) {
				t.Fatal("rejection changed durable state")
			}
		})
	}
}

func TestSQLiteFundingRecoveryReleaseResumeEvidenceBoundaries(t *testing.T) {
	cases := []struct {
		name, query string
		args        []any
	}{
		{"episode scalar", `UPDATE funding_recovery_episodes SET revision=revision+1 WHERE episode_id=?`, nil},
		{"hold scalar", `UPDATE funding_recovery_holds SET hold_revision=hold_revision+1 WHERE episode_id=?`, nil},
		{"scope payload", `UPDATE funding_recovery_episodes SET payload_json=json_set(payload_json,'$.account_scopes[0]',?) WHERE episode_id=?`, []any{strings.Repeat("d", 64)}},
		{"hold scope", `UPDATE funding_recovery_holds SET payload_json=json_set(payload_json,'$.account_scopes[0]',?) WHERE episode_id=?`, []any{strings.Repeat("d", 64)}},
		{"release scalar hash", `UPDATE funding_recovery_operations SET request_hash=? WHERE operation_id='original-release' AND episode_id=?`, []any{strings.Repeat("f", 64)}},
		{"release phase", `UPDATE funding_recovery_operations SET phase='blocked' WHERE operation_id='original-release' AND episode_id=?`, nil},
		{"release actor", `UPDATE funding_recovery_operations SET receipt_json=json_set(receipt_json,'$.actor','tampered') WHERE operation_id='original-release' AND episode_id=?`, nil},
		{"release evidence", `UPDATE funding_recovery_operations SET receipt_json=json_set(receipt_json,'$.evidence_hash',?) WHERE operation_id='original-release' AND episode_id=?`, []any{strings.Repeat("f", 64)}},
		{"release exposure", `UPDATE funding_recovery_operations SET receipt_json=json_set(receipt_json,'$.exposure_clearance_hash',?) WHERE operation_id='original-release' AND episode_id=?`, []any{strings.Repeat("f", 64)}},
		{"release owner", `UPDATE funding_recovery_operations SET receipt_json=json_set(receipt_json,'$.owner_generation','tampered') WHERE operation_id='original-release' AND episode_id=?`, nil},
		{"release projection", `UPDATE funding_recovery_operations SET receipt_json=json_set(receipt_json,'$.projection_hash',?) WHERE operation_id='original-release' AND episode_id=?`, []any{strings.Repeat("f", 64)}},
		{"event", `UPDATE funding_recovery_events SET payload_json=json_set(payload_json,'$.amount','-2.5') WHERE episode_id=?`, nil},
		{"cursor", `UPDATE funding_recovery_source_cursors SET revision=revision+1 WHERE episode_id=?`, nil},
		{"pending outbox scalar", `UPDATE funding_recovery_outbox SET state='projection_pending' WHERE episode_id=?`, nil},
		{"outbox payload", `UPDATE funding_recovery_outbox SET payload_json=json_set(payload_json,'$.projection_payload.events[0].amount','-3.5') WHERE episode_id=?`, nil},
		{"unverified anomaly", `INSERT INTO funding_recovery_source_anomalies(anomaly_id,source_identity_hash,cursor_revision,observed_history_hash,state,revision,payload_json,episode_id) VALUES('unverified','source',1,'hash','open',1,'{}',?)`, nil},
		{"unknown child", `INSERT INTO funding_recovery_children(child_id,revision,payload_json,episode_id) VALUES('unknown',1,'{"state":"unknown"}',?)`, nil},
		{"unknown attempt", `INSERT INTO funding_recovery_submission_attempts(permit_id,recovery_generation,action_id,revision,state,client_order_id,intent_hash,payload_json,episode_id) VALUES('unknown',1,'action',1,'outcome_unknown','cid','hash','{}',?)`, nil},
		{"authorized permit", `INSERT INTO funding_recovery_permits(permit_id,recovery_generation,action_id,lease_id,state,payload_json,episode_id) VALUES('unused',1,'action',1,'authorized','{}',?)`, nil},
		{"reopened", `UPDATE funding_recovery_episodes SET state='reopened',recovery_generation=recovery_generation+1,payload_json=json_set(payload_json,'$.state','reopened','$.recovery_generation',recovery_generation+1) WHERE episode_id=?`, nil},
	}
	for _, afterCommit := range []bool{false, true} {
		for _, tc := range cases {
			t.Run(fmt.Sprintf("afterCommit=%t/%s", afterCommit, tc.name), func(t *testing.T) {
				f := prepareReleaseResumeFixture(t, newSQLiteReleaseResumeStores(t), false)
				if afterCommit {
					if _, _, _, _, err := f.stores[0].ResumeRecoveryRelease(t.Context(), f.owner, f.auth); err != nil {
						t.Fatal(err)
					}
				}
				args := append(append([]any(nil), tc.args...), f.auth.EpisodeID)
				if _, err := f.stores[0].db.ExecContext(t.Context(), tc.query, args...); err != nil {
					t.Fatal(err)
				}
				before := releaseResumeDurableState(t, f.stores[0])
				if _, _, _, _, err := f.stores[0].ResumeRecoveryRelease(t.Context(), f.owner, f.auth); err == nil {
					t.Fatal("accepted changed evidence")
				}
				if !reflect.DeepEqual(before, releaseResumeDurableState(t, f.stores[0])) {
					t.Fatal("rejection changed durable state")
				}
			})
		}
	}
}

func TestSQLiteFundingRecoveryReleaseResumeConcurrentPools(t *testing.T) {
	for _, sameOperation := range []bool{true, false} {
		t.Run(fmt.Sprintf("sameOperation=%t", sameOperation), func(t *testing.T) {
			f := prepareReleaseResumeFixture(t, newSQLiteReleaseResumeStores(t), false)
			testReleaseResumeConcurrentPools(t, f, sameOperation)
		})
	}
}

func testReleaseResumeConcurrentPools(t *testing.T, f releaseResumeFixture, sameOperation bool) {
	t.Helper()
	if f.stores[0].db == f.stores[1].db {
		t.Fatal("concurrency fixture must use independent pools")
	}
	var wg sync.WaitGroup
	start := make(chan struct{})
	type result struct {
		replay bool
		err    error
	}
	results := make(chan result, 2)
	for i, store := range f.stores {
		wg.Add(1)
		go func(i int, store *SQLStorage) {
			defer wg.Done()
			auth := f.auth
			if !sameOperation && i == 1 {
				auth.OperationID = "competing-resume"
			}
			<-start
			_, _, _, replay, err := store.ResumeRecoveryRelease(t.Context(), f.owner, auth)
			results <- result{replay, err}
		}(i, store)
	}
	close(start)
	wg.Wait()
	close(results)
	writes, replays, conflicts := 0, 0, 0
	for result := range results {
		if result.err != nil {
			if !errors.Is(result.err, accounting.ErrRecoveryConflict) {
				t.Fatalf("concurrent resume: %v", result.err)
			}
			conflicts++
		} else if result.replay {
			replays++
		} else {
			writes++
		}
	}
	if writes != 1 || (sameOperation && (replays != 1 || conflicts != 0)) || (!sameOperation && (replays != 0 || conflicts != 1)) {
		t.Fatalf("writers=%d replays=%d conflicts=%d", writes, replays, conflicts)
	}
}

func TestSQLiteFundingRecoveryReleaseResumeLateCASRollsBack(t *testing.T) {
	f := prepareReleaseResumeFixture(t, newSQLiteReleaseResumeStores(t), false)
	// SQLite RAISE(IGNORE) makes the last episode CAS affect zero rows after
	// the hold and original receipt writes; the transaction must roll all back.
	if _, err := f.stores[0].db.ExecContext(t.Context(), `CREATE TRIGGER fail_resume_episode BEFORE UPDATE ON funding_recovery_episodes BEGIN SELECT RAISE(IGNORE); END`); err != nil {
		t.Fatal(err)
	}
	before := releaseResumeDurableState(t, f.stores[0])
	if _, _, _, _, err := f.stores[0].ResumeRecoveryRelease(t.Context(), f.owner, f.auth); !errors.Is(err, accounting.ErrRecoveryConflict) {
		t.Fatalf("late CAS: %v", err)
	}
	if !reflect.DeepEqual(before, releaseResumeDurableState(t, f.stores[0])) {
		t.Fatal("late CAS failure left a partial release")
	}
}

func TestSQLiteFundingRecoveryReleaseResumeAuditOnlyEvidenceChangeRejectsReplay(t *testing.T) {
	f := prepareReleaseResumeFixture(t, newSQLiteReleaseResumeStores(t), false)
	if _, _, _, _, err := f.stores[0].ResumeRecoveryRelease(t.Context(), f.owner, f.auth); err != nil {
		t.Fatal(err)
	}
	// Metadata changes remain well-formed and pass projection business checks;
	// the result snapshot must still reject this retry.
	if _, err := f.stores[0].db.ExecContext(t.Context(), `UPDATE funding_recovery_episodes SET payload_json=json_set(payload_json,'$.updated_at',?) WHERE episode_id=?`, time.Now().UTC().Format(time.RFC3339Nano), f.auth.EpisodeID); err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err := f.stores[0].ResumeRecoveryRelease(t.Context(), f.owner, f.auth); err == nil {
		t.Fatal("accepted changed committed snapshot")
	}
}

func TestSQLiteFundingRecoveryReleaseResumeHistoricalReceiptTampering(t *testing.T) {
	f := prepareReleaseResumeFixture(t, newSQLiteReleaseResumeStores(t), false)
	store, ctx := f.stores[0], t.Context()
	if _, _, _, _, err := store.ResumeRecoveryRelease(ctx, f.owner, f.auth); err != nil {
		t.Fatal(err)
	}
	next, err := f.stores[1].ClaimFundingCarryRuntimeGeneration(ctx, f.auth.AccountScopes)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := store.PreviewRecoveryReleaseResume(ctx, next, f.auth.EpisodeID, f.release.OperationID)
	if err != nil {
		t.Fatal(err)
	}
	second := releaseResumeAuthFromPreview(preview, "resume-second")
	if _, _, _, _, err = store.ResumeRecoveryRelease(ctx, next, second); err != nil {
		t.Fatal(err)
	}
	if _, _, _, replayed, err := store.ResumeRecoveryRelease(ctx, next, second); err != nil || !replayed {
		t.Fatalf("legitimate second resume replay: replay=%v err=%v", replayed, err)
	}
	// Completed history receipts stay bound to later retries: changing only an
	// older resumption's stored receipt must invalidate the current replay.
	if _, err = store.db.ExecContext(ctx, `UPDATE funding_recovery_operations SET receipt_json=json_set(receipt_json,'$.actor','tampered') WHERE operation_id=? AND episode_id=?`,
		f.auth.OperationID, f.auth.EpisodeID); err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, err = store.ResumeRecoveryRelease(ctx, next, second); err == nil {
		t.Fatal("accepted tampered historical resumption receipt on replay")
	}
}

func TestFundingRecoveryReleaseResumePreviewJSONContract(t *testing.T) {
	raw, err := json.Marshal(FundingRecoveryReleaseResumePreview{})
	if err != nil {
		t.Fatal(err)
	}
	var object map[string]json.RawMessage
	if err = json.Unmarshal(raw, &object); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"episode", "hold", "release_receipt", "evidence_hash", "owner_generation"} {
		if _, ok := object[key]; !ok {
			t.Fatalf("missing preview JSON field %s", key)
		}
	}
}

// Compile-time method contracts kept local: adapters can opt into this narrow
// capability without expanding FundingRecoveryStore and all existing mocks.
var _ interface {
	PreviewRecoveryReleaseResume(context.Context, FundingCarryRuntimeGeneration, string, string) (FundingRecoveryReleaseResumePreview, error)
	ResumeRecoveryRelease(context.Context, FundingCarryRuntimeGeneration, accounting.RecoveryReleaseResumeAuthorization) (accounting.RecoveryEpisode, accounting.RecoveryHold, accounting.RecoveryReceipt, bool, error)
} = (*SQLStorage)(nil)
