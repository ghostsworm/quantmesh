package app

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"quantmesh/accounting"
	"quantmesh/config"
	"quantmesh/execution"
	"quantmesh/storage"
	"quantmesh/strategy"
	"quantmesh/web"
)

func testInterruptedFundingRecoveryRelease(t *testing.T, cfg *config.Config, original *storage.StorageService, originalGeneration storage.FundingCarryRuntimeGeneration, auth accounting.RecoveryReleaseAuthorization) {
	t.Helper()
	ctx := context.Background()
	oldStore := original.GetStorage().(*storage.SQLStorage)
	pending, err := oldStore.PrepareRecoveryRelease(ctx, originalGeneration, auth)
	if err != nil || pending.State != accounting.RecoveryReleasePending {
		t.Fatalf("prepare durable release before interruption: episode=%+v err=%v", pending, err)
	}
	original.Stop()

	// 重新開啟 SQLite 連線並建立全新的 runtime，模擬提交 prepare 後程序退出。
	restarted, err := storage.NewStorageService(cfg, ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(restarted.Stop)
	store := restarted.GetStorage().(*storage.SQLStorage)
	generation, err := store.ClaimFundingCarryRuntimeGeneration(ctx, pending.AccountScopes)
	if err != nil {
		t.Fatal(err)
	}
	carry := strategy.NewFundingCarryStrategy("funding_carry", nil, config.SymbolConfig{Symbol: "BTCUSDT"}, nil, nil, nil, nil)
	carry.SetRuntimeStateStore(&fundingCarryRuntimeStateAdapter{
		strategyRuntimeStateAdapter: &strategyRuntimeStateAdapter{storageService: restarted, botID: pending.Bot},
		generation:                  generation,
	})
	gate := &execution.OpeningGate{}
	gate.Block(execution.RecoveryEpisodeGateSource(pending.ID, pending.RecoveryGeneration))
	gate.Block("manual_operator_hold")
	carry.SetOpeningGate(gate)
	manager := strategy.NewStrategyManager(cfg, 0)
	manager.RegisterStrategy("funding_carry", carry, 1, 0)
	bots := &BotManager{runtimes: map[string]*BotRuntime{pending.Bot: {
		BotID: pending.Bot, Inner: &SymbolRuntime{fundingCarryReconciliationOnly: true, StrategyManager: manager},
	}}}
	service, err := newProductionOrderReconciliationService(store, bots)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := service.RecoveryOwnerTakeover(ctx, pending.ID, "ordinary-takeover", "new-admin", "restart-incident", pending.Revision, true); err == nil {
		t.Fatal("ordinary takeover must reject a pending release")
	} else {
		t.Logf("reproduced ordinary takeover rejection after fresh connection/runtime: %v", err)
	}
	if _, err := service.RecoveryReleaseApply(ctx, auth); err == nil {
		t.Fatal("ordinary release must reject an episode owned by a previous runtime")
	} else {
		t.Logf("reproduced ordinary release rejection after fresh connection/runtime: %v", err)
	}
	hold, found, err := store.GetRecoveryHold(ctx, pending.ID, pending.RecoveryGeneration)
	if err != nil || !found || hold.State != "held" || !gate.HasBlock(execution.RecoveryEpisodeGateSource(pending.ID, pending.RecoveryGeneration)) {
		t.Fatalf("interrupted release lost its hold or exact Gate: hold=%+v found=%t err=%v", hold, found, err)
	}
	originalReceipt, found, err := store.GetRecoveryReceipt(ctx, pending.ID, auth.OperationID)
	if err != nil || !found {
		t.Fatal("missing original release receipt", err)
	}
	web.SetOrderReconciliationService(service)
	router := gin.New()
	web.SetupRoutesWithConfig(router, cfg)
	admin, err := web.GetSessionManager().CreateSession("resume-admin", "admin", "192.0.2.31", "release-resume")
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.RemoteAddr = "192.0.2.31:1234"
		req.AddCookie(&http.Cookie{Name: "session_id", Value: admin.SessionID})
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		return response
	}
	path := "/api/capital/funding-recovery/" + pending.ID + "/release/resume"
	response := request(http.MethodGet, path+"/preview?release_operation_id="+auth.OperationID, "")
	if response.Code != http.StatusOK {
		t.Fatalf("resume preview after restart: status=%d body=%s", response.Code, response.Body.String())
	}
	var envelope struct {
		Preview web.FundingRecoveryReleaseResumePreview `json:"preview"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	preview := envelope.Preview
	resume := accounting.RecoveryReleaseResumeAuthorization{EpisodeID: pending.ID, OperationID: "resume-after-restart", ReleaseOperationID: auth.OperationID,
		ReleaseRequestHash: preview.ReleaseReceipt.RequestHash, Actor: admin.Username, AuditRef: "incident-resume", EvidenceHash: preview.EvidenceHash,
		ExpectedOwnerGeneration: preview.Episode.OwnerGeneration, OwnerGeneration: preview.OwnerGeneration, AccountScopes: preview.Episode.AccountScopes,
		ExpectedEpisodeRevision: preview.Episode.Revision, ExpectedHoldRevision: preview.Hold.Revision, RecoveryGeneration: preview.Episode.RecoveryGeneration, Confirmed: true}
	encodeRequest := func(value accounting.RecoveryReleaseResumeAuthorization) string {
		t.Helper()
		raw, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			t.Fatal(err)
		}
		delete(fields, "actor")
		delete(fields, "episode_id")
		raw, err = json.Marshal(fields)
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}
	for _, test := range []struct {
		name   string
		mutate func(*accounting.RecoveryReleaseResumeAuthorization)
	}{
		{"evidence", func(a *accounting.RecoveryReleaseResumeAuthorization) { a.EvidenceHash = strings.Repeat("0", 64) }},
		{"release receipt", func(a *accounting.RecoveryReleaseResumeAuthorization) { a.ReleaseRequestHash = strings.Repeat("0", 64) }},
		{"old owner", func(a *accounting.RecoveryReleaseResumeAuthorization) { a.ExpectedOwnerGeneration = "wrong-owner" }},
		{"scope", func(a *accounting.RecoveryReleaseResumeAuthorization) {
			a.AccountScopes = []string{strings.Repeat("0", 64)}
		}},
		{"revision", func(a *accounting.RecoveryReleaseResumeAuthorization) { a.ExpectedEpisodeRevision++ }},
	} {
		t.Run(test.name, func(t *testing.T) {
			changed := resume
			test.mutate(&changed)
			response := request(http.MethodPost, path, encodeRequest(changed))
			if response.Code != http.StatusConflict && response.Code != http.StatusBadRequest {
				t.Fatalf("invalid resume accepted: status=%d body=%s", response.Code, response.Body.String())
			}
			unchanged, _, err := store.GetRecoveryHold(ctx, pending.ID, pending.RecoveryGeneration)
			if err != nil || unchanged.State != "held" || unchanged.Revision != hold.Revision {
				t.Fatalf("invalid resume changed hold: %+v err=%v", unchanged, err)
			}
		})
	}
	body := encodeRequest(resume)
	// Gate 清理失敗發生在 durable 提交之後，必須回報真實釋放狀態並允許精確重試。
	carry.SetOpeningGate(nil)
	failedGate := request(http.MethodPost, path, body)
	if failedGate.Code != http.StatusConflict || !strings.Contains(failedGate.Body.String(), `"hold_released":true`) || !gate.HasBlock(preview.RecoveryGateSource) {
		t.Fatalf("Gate cleanup failure lost durable facts: status=%d body=%s", failedGate.Code, failedGate.Body.String())
	}
	carry.SetOpeningGate(gate)
	var readback web.FundingRecoveryReleaseResumeReadback
	for i := 0; i < 2; i++ {
		response := request(http.MethodPost, path, body)
		if response.Code != http.StatusOK {
			t.Fatalf("resume retry %d: status=%d body=%s", i, response.Code, response.Body.String())
		}
		var result struct {
			Readback web.FundingRecoveryReleaseResumeReadback `json:"readback"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &result); err != nil {
			t.Fatal(err)
		}
		readback = result.Readback
		if !readback.Verified || !readback.Replayed || readback.ExecutionReplayed || readback.Hold.ReleasedByOperation != auth.OperationID ||
			readback.Episode.OwnerGeneration != generation.ID() || readback.Receipt.Actor != admin.Username || readback.ReleaseReceipt.Actor != auth.Actor ||
			readback.ReleaseReceipt.RequestHash != originalReceipt.RequestHash || readback.ReleaseReceipt.OwnerGeneration != originalGeneration.ID() ||
			readback.Hold.Revision != hold.Revision+1 || gate.HasBlock(preview.RecoveryGateSource) || !gate.HasBlock("manual_operator_hold") {
			t.Fatalf("resume readback lost original authorization/new-owner audit/exact Gate semantics: %+v", readback)
		}
	}
	if _, _, _, _, err := store.ResumeRecoveryRelease(ctx, originalGeneration, resume); err == nil {
		t.Fatal("old owner bypassed generation fence on retry")
	}
	if _, err := service.RecoveryReleaseApply(ctx, auth); !errors.Is(err, accounting.ErrRecoveryConflict) {
		t.Fatalf("original authorization bypassed explicit resume after ownership changed: %v", err)
	}
	changed := resume
	changed.AuditRef = "different-incident"
	if _, err := service.RecoveryReleaseResumeApply(ctx, changed); !errors.Is(err, accounting.ErrRecoveryOperationConflict) {
		t.Fatalf("same operation with different audit accepted: %v", err)
	}
	assertReleasedRecoveryResumeAfterSecondRestart(t, cfg, restarted, resume, readback.Hold.Revision)
}

func assertReleasedRecoveryResumeAfterSecondRestart(t *testing.T, cfg *config.Config, previous *storage.StorageService, prior accounting.RecoveryReleaseResumeAuthorization, holdRevision uint64) {
	t.Helper()
	previous.Stop()
	ctx := context.Background()
	reopened, err := storage.NewStorageService(cfg, ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(reopened.Stop)
	store := reopened.GetStorage().(*storage.SQLStorage)
	generation, err := store.ClaimFundingCarryRuntimeGeneration(ctx, prior.AccountScopes)
	if err != nil {
		t.Fatal(err)
	}
	preview, err := store.PreviewRecoveryReleaseResume(ctx, generation, prior.EpisodeID, prior.ReleaseOperationID)
	if err != nil {
		t.Fatal(err)
	}
	next := prior
	next.OperationID = "resume-cleanup-after-second-restart"
	next.ExpectedOwnerGeneration = preview.Episode.OwnerGeneration
	next.OwnerGeneration = generation.ID()
	next.ExpectedEpisodeRevision = preview.Episode.Revision
	next.ExpectedHoldRevision = preview.Hold.Revision
	next.EvidenceHash = preview.EvidenceHash
	episode, hold, receipt, replayed, err := store.ResumeRecoveryRelease(ctx, generation, next)
	if err != nil || replayed || episode.State != accounting.RecoveryReleased || episode.OwnerGeneration != generation.ID() ||
		hold.Revision != holdRevision || hold.ReleasedByOperation != prior.ReleaseOperationID || receipt.PreviousOwnerGeneration != prior.OwnerGeneration {
		t.Fatalf("released cleanup responsibility resume changed original hold: episode=%+v hold=%+v receipt=%+v replayed=%t err=%v", episode, hold, receipt, replayed, err)
	}
	if _, _, _, _, err := store.ResumeRecoveryRelease(ctx, generation, prior); err == nil {
		t.Fatal("previous runtime request was accepted by new generation")
	}
}
