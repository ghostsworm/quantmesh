package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"quantmesh/accounting"
	"quantmesh/config"
	"quantmesh/storage"
)

const fundingRecoveryReleaseResumeTestPath = "/api/capital/funding-recovery/episode-resume/release/resume"

type fundingRecoveryReleaseResumeContextKey struct{}

func newFundingRecoveryReleaseResumeTestRouter(t *testing.T, service *OrderReconciliationService) (*gin.Engine, *Session, *Session) {
	t.Helper()
	originalMode := gin.Mode()
	gin.SetMode(gin.TestMode)
	originalStorage := storageProvider
	originalService := currentOrderReconciliationService()
	originalSessionManager := SwapSessionManager(NewEphemeralSessionManager())
	t.Cleanup(func() {
		SetStorageProvider(originalStorage)
		SetOrderReconciliationService(originalService)
		if originalSessionManager == nil {
			originalSessionManager = NewEphemeralSessionManager()
		}
		SetSessionManager(originalSessionManager)
		gin.SetMode(originalMode)
	})
	SetStorageProvider(installedRecoveryAuthSettingsProvider{})
	SetOrderReconciliationService(service)
	admin, err := GetSessionManager().CreateSession("resume-auditor", "admin", "192.0.2.10", "release-resume-test")
	if err != nil {
		t.Fatal(err)
	}
	operator, err := GetSessionManager().CreateSession("operator", "user", "192.0.2.10", "release-resume-test")
	if err != nil {
		t.Fatal(err)
	}
	router := gin.New()
	router.Use(gin.Recovery())
	cfg := &config.Config{}
	cfg.Web.SharedDir = t.TempDir()
	SetupRoutesWithConfig(router, cfg)
	return router, admin, operator
}

func fundingRecoveryReleaseResumeTestBody(t *testing.T) string {
	t.Helper()
	body, err := json.Marshal(map[string]interface{}{
		"operation_id": "resume-1", "release_operation_id": "release-1",
		"release_request_hash": strings.Repeat("a", 64), "audit_ref": "incident-resume",
		"evidence_hash": strings.Repeat("b", 64), "expected_owner_generation": "owner-before",
		"owner_generation": "owner-current", "account_scopes": []string{strings.Repeat("c", 64), strings.Repeat("d", 64)},
		"expected_episode_revision": 8, "expected_hold_revision": 4, "recovery_generation": 2, "confirmed": true,
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(body)
}

func performFundingRecoveryReleaseResumeRequest(router http.Handler, method, path, body string, session *Session) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	// 使用非本機位址，確保測試經過正式認證而非本機開發捷徑。
	req.RemoteAddr = "192.0.2.10:1234"
	req.Header.Set("Content-Type", "application/json")
	if session != nil {
		req.AddCookie(&http.Cookie{Name: "session_id", Value: session.SessionID})
	}
	req = req.WithContext(context.WithValue(req.Context(), fundingRecoveryReleaseResumeContextKey{}, "request-context"))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, req)
	return response
}

func TestFundingRecoveryReleaseResumeRequiresSessionAdmin(t *testing.T) {
	calls := 0
	service := &OrderReconciliationService{
		RecoveryReleaseResumePreview: func(context.Context, string, string) (FundingRecoveryReleaseResumePreview, error) {
			calls++
			return FundingRecoveryReleaseResumePreview{}, nil
		},
		RecoveryReleaseResumeApply: func(context.Context, accounting.RecoveryReleaseResumeAuthorization) (FundingRecoveryReleaseResumeReadback, error) {
			calls++
			return FundingRecoveryReleaseResumeReadback{}, nil
		},
	}
	router, _, operator := newFundingRecoveryReleaseResumeTestRouter(t, service)
	for _, endpoint := range []struct {
		name, method, path string
	}{
		{"preview", http.MethodGet, fundingRecoveryReleaseResumeTestPath + "/preview?release_operation_id=release-1"},
		{"apply", http.MethodPost, fundingRecoveryReleaseResumeTestPath},
	} {
		for _, identity := range []struct {
			name    string
			session *Session
			status  int
		}{
			{"unauthenticated", nil, http.StatusUnauthorized},
			{"non_admin", operator, http.StatusForbidden},
		} {
			t.Run(endpoint.name+"/"+identity.name, func(t *testing.T) {
				response := performFundingRecoveryReleaseResumeRequest(router, endpoint.method, endpoint.path, fundingRecoveryReleaseResumeTestBody(t), identity.session)
				if response.Code != identity.status || calls != 0 {
					t.Fatalf("status=%d calls=%d body=%s", response.Code, calls, response.Body.String())
				}
			})
		}
	}
}

func TestFundingRecoveryReleaseResumeRejectsInvalidJSON(t *testing.T) {
	calls := 0
	router, admin, _ := newFundingRecoveryReleaseResumeTestRouter(t, &OrderReconciliationService{
		RecoveryReleaseResumeApply: func(context.Context, accounting.RecoveryReleaseResumeAuthorization) (FundingRecoveryReleaseResumeReadback, error) {
			calls++
			return FundingRecoveryReleaseResumeReadback{}, nil
		},
	})
	body := fundingRecoveryReleaseResumeTestBody(t)
	for _, test := range []struct {
		name, body string
	}{
		{"client_actor", strings.TrimSuffix(body, "}") + `,"actor":"forged-admin"}`},
		{"unknown_field", strings.TrimSuffix(body, "}") + `,"execution_replayed":true}`},
		{"trailing_object", body + `{}`},
		{"trailing_null", body + `null`},
		{"trailing_garbage", body + `!`},
		{"oversized_body", strings.Replace(body, "incident-resume", strings.Repeat("x", fundingRecoveryReleaseResumeMaxBodyBytes), 1)},
		{"oversized_trailing_whitespace", body + strings.Repeat(" ", fundingRecoveryReleaseResumeMaxBodyBytes)},
	} {
		t.Run(test.name, func(t *testing.T) {
			response := performFundingRecoveryReleaseResumeRequest(router, http.MethodPost, fundingRecoveryReleaseResumeTestPath, test.body, admin)
			if response.Code != http.StatusBadRequest || calls != 0 {
				t.Fatalf("status=%d calls=%d body=%s", response.Code, calls, response.Body.String())
			}
		})
	}
}

func TestFundingRecoveryReleaseResumeRequiresCompleteAuthorization(t *testing.T) {
	calls := 0
	router, admin, _ := newFundingRecoveryReleaseResumeTestRouter(t, &OrderReconciliationService{
		RecoveryReleaseResumeApply: func(context.Context, accounting.RecoveryReleaseResumeAuthorization) (FundingRecoveryReleaseResumeReadback, error) {
			calls++
			return FundingRecoveryReleaseResumeReadback{}, nil
		},
	})
	for _, field := range []string{
		"confirmed", "operation_id", "release_operation_id", "release_request_hash", "audit_ref", "evidence_hash",
		"expected_owner_generation", "owner_generation", "account_scopes", "expected_episode_revision",
		"expected_hold_revision", "recovery_generation",
	} {
		t.Run("missing_"+field, func(t *testing.T) {
			var request map[string]json.RawMessage
			if err := json.Unmarshal([]byte(fundingRecoveryReleaseResumeTestBody(t)), &request); err != nil {
				t.Fatal(err)
			}
			delete(request, field)
			body, err := json.Marshal(request)
			if err != nil {
				t.Fatal(err)
			}
			response := performFundingRecoveryReleaseResumeRequest(router, http.MethodPost, fundingRecoveryReleaseResumeTestPath, string(body), admin)
			if response.Code != http.StatusConflict || calls != 0 {
				t.Fatalf("status=%d calls=%d body=%s", response.Code, calls, response.Body.String())
			}
		})
	}
	for _, test := range []struct {
		name, old, replacement string
	}{
		{"not_confirmed", `"confirmed":true`, `"confirmed":false`},
		{"invalid_release_hash", strings.Repeat("a", 64), strings.Repeat("z", 64)},
		{"invalid_evidence_hash", strings.Repeat("b", 64), strings.Repeat("z", 64)},
		{"empty_scope", strings.Repeat("c", 64), ""},
		{"duplicate_scope", strings.Repeat("c", 64), strings.Repeat("d", 64)},
	} {
		t.Run(test.name, func(t *testing.T) {
			body := strings.Replace(fundingRecoveryReleaseResumeTestBody(t), test.old, test.replacement, 1)
			response := performFundingRecoveryReleaseResumeRequest(router, http.MethodPost, fundingRecoveryReleaseResumeTestPath, body, admin)
			if response.Code != http.StatusConflict || calls != 0 {
				t.Fatalf("status=%d calls=%d body=%s", response.Code, calls, response.Body.String())
			}
		})
	}
}

func TestFundingRecoveryReleaseResumePreviewBindsRequestAndFlattensEvidence(t *testing.T) {
	calls := 0
	router, admin, _ := newFundingRecoveryReleaseResumeTestRouter(t, &OrderReconciliationService{
		RecoveryReleaseResumePreview: func(ctx context.Context, episodeID, releaseOperationID string) (FundingRecoveryReleaseResumePreview, error) {
			calls++
			if episodeID != "episode-resume" || releaseOperationID != "release-1" || ctx.Value(fundingRecoveryReleaseResumeContextKey{}) != "request-context" {
				t.Fatalf("preview callback: episode=%q release=%q context=%v", episodeID, releaseOperationID, ctx.Value(fundingRecoveryReleaseResumeContextKey{}))
			}
			return FundingRecoveryReleaseResumePreview{
				FundingRecoveryReleaseResumePreview: storage.FundingRecoveryReleaseResumePreview{
					Episode:        accounting.RecoveryEpisode{ID: episodeID, Revision: 8},
					Hold:           accounting.RecoveryHold{EpisodeID: episodeID, Revision: 4, State: "released"},
					ReleaseReceipt: accounting.RecoveryReceipt{OperationID: releaseOperationID, RequestHash: strings.Repeat("a", 64)},
					EvidenceHash:   strings.Repeat("b", 64), OwnerGeneration: "owner-current",
				},
				GateSources: []string{"recovery:episode-resume", "manual-pause"}, RecoveryGateSource: "recovery:episode-resume",
			}, nil
		},
	})
	for _, query := range []string{"", "?release_operation_id=%20"} {
		response := performFundingRecoveryReleaseResumeRequest(router, http.MethodGet, fundingRecoveryReleaseResumeTestPath+"/preview"+query, "", admin)
		if response.Code != http.StatusConflict || calls != 0 {
			t.Fatalf("missing release operation: status=%d calls=%d body=%s", response.Code, calls, response.Body.String())
		}
	}
	response := performFundingRecoveryReleaseResumeRequest(router, http.MethodGet, fundingRecoveryReleaseResumeTestPath+"/preview?release_operation_id=release-1", "", admin)
	if response.Code != http.StatusOK || calls != 1 {
		t.Fatalf("status=%d calls=%d body=%s", response.Code, calls, response.Body.String())
	}
	var envelope struct {
		Preview struct {
			Episode            accounting.RecoveryEpisode `json:"episode"`
			Hold               accounting.RecoveryHold    `json:"hold"`
			ReleaseReceipt     accounting.RecoveryReceipt `json:"release_receipt"`
			EvidenceHash       string                     `json:"evidence_hash"`
			OwnerGeneration    string                     `json:"owner_generation"`
			GateSources        []string                   `json:"gate_sources"`
			RecoveryGateSource string                     `json:"recovery_gate_source"`
		} `json:"preview"`
		ExecutionReplayed *bool `json:"execution_replayed"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
		t.Fatal(err)
	}
	preview := envelope.Preview
	if preview.Episode.ID != "episode-resume" || preview.Hold.Revision != 4 || preview.ReleaseReceipt.OperationID != "release-1" ||
		preview.ReleaseReceipt.RequestHash != strings.Repeat("a", 64) || preview.EvidenceHash != strings.Repeat("b", 64) ||
		preview.OwnerGeneration != "owner-current" || preview.RecoveryGateSource != "recovery:episode-resume" ||
		!reflect.DeepEqual(preview.GateSources, []string{"recovery:episode-resume", "manual-pause"}) ||
		envelope.ExecutionReplayed == nil || *envelope.ExecutionReplayed {
		t.Fatalf("preview response lost evidence or replay flag: %s", response.Body.String())
	}
}

func TestFundingRecoveryReleaseResumeBindsAuthorizationAndReturnsBothReceipts(t *testing.T) {
	calls := 0
	verified := true
	router, admin, _ := newFundingRecoveryReleaseResumeTestRouter(t, &OrderReconciliationService{
		RecoveryReleaseResumeApply: func(ctx context.Context, auth accounting.RecoveryReleaseResumeAuthorization) (FundingRecoveryReleaseResumeReadback, error) {
			calls++
			want := accounting.RecoveryReleaseResumeAuthorization{
				EpisodeID: "episode-resume", OperationID: "resume-1", ReleaseOperationID: "release-1",
				ReleaseRequestHash: strings.Repeat("a", 64), Actor: "resume-auditor", AuditRef: "incident-resume",
				EvidenceHash: strings.Repeat("b", 64), ExpectedOwnerGeneration: "owner-before", OwnerGeneration: "owner-current",
				AccountScopes:           []string{strings.Repeat("c", 64), strings.Repeat("d", 64)},
				ExpectedEpisodeRevision: 8, ExpectedHoldRevision: 4, RecoveryGeneration: 2, Confirmed: true,
			}
			if !reflect.DeepEqual(auth, want) || ctx.Value(fundingRecoveryReleaseResumeContextKey{}) != "request-context" {
				t.Fatalf("callback authorization=%+v context=%v", auth, ctx.Value(fundingRecoveryReleaseResumeContextKey{}))
			}
			return FundingRecoveryReleaseResumeReadback{
				FundingRecoveryReleaseReadback: FundingRecoveryReleaseReadback{
					Episode:     accounting.RecoveryEpisode{ID: auth.EpisodeID, Revision: 9},
					Hold:        accounting.RecoveryHold{EpisodeID: auth.EpisodeID, State: "released"},
					GateSources: []string{"manual-pause"}, ReleasedSource: "recovery:episode-resume",
					Receipt: accounting.RecoveryReceipt{OperationID: "resume-1", Actor: auth.Actor}, Verified: verified,
				},
				ReleaseReceipt: accounting.RecoveryReceipt{OperationID: "release-1", RequestHash: auth.ReleaseRequestHash},
				Replayed:       true,
			}, nil
		},
	})
	for _, wantVerified := range []bool{true, false} {
		t.Run(fmt.Sprintf("verified_%t", wantVerified), func(t *testing.T) {
			verified = wantVerified
			response := performFundingRecoveryReleaseResumeRequest(router, http.MethodPost, fundingRecoveryReleaseResumeTestPath, fundingRecoveryReleaseResumeTestBody(t), admin)
			if response.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			var envelope struct {
				Readback struct {
					Episode        accounting.RecoveryEpisode `json:"episode"`
					Hold           accounting.RecoveryHold    `json:"hold"`
					GateSources    []string                   `json:"gate_sources"`
					ReleasedSource string                     `json:"released_source"`
					Receipt        accounting.RecoveryReceipt `json:"receipt"`
					ReleaseReceipt accounting.RecoveryReceipt `json:"release_receipt"`
					Verified       bool                       `json:"verified"`
					Replayed       bool                       `json:"replayed"`
				} `json:"readback"`
				HoldReleased      *bool `json:"hold_released"`
				ExecutionReplayed *bool `json:"execution_replayed"`
			}
			if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
				t.Fatal(err)
			}
			readback := envelope.Readback
			if readback.Episode.ID != "episode-resume" || readback.Hold.State != "released" ||
				readback.Receipt.OperationID != "resume-1" || readback.Receipt.Actor != "resume-auditor" ||
				readback.ReleaseReceipt.OperationID != "release-1" || readback.ReleaseReceipt.RequestHash != strings.Repeat("a", 64) ||
				!reflect.DeepEqual(readback.GateSources, []string{"manual-pause"}) || readback.ReleasedSource != "recovery:episode-resume" ||
				!readback.Replayed || readback.Verified != wantVerified || envelope.HoldReleased == nil || *envelope.HoldReleased != wantVerified ||
				envelope.ExecutionReplayed == nil || *envelope.ExecutionReplayed {
				t.Fatalf("resume response lost receipts, durable state or replay distinction: %s", response.Body.String())
			}
		})
	}
	if calls != 2 {
		t.Fatalf("apply callback calls=%d want=2", calls)
	}
}

func TestFundingRecoveryReleaseResumePreservesDurableErrorState(t *testing.T) {
	var callbackErr error
	service := &OrderReconciliationService{
		RecoveryReleaseResumePreview: func(context.Context, string, string) (FundingRecoveryReleaseResumePreview, error) {
			return FundingRecoveryReleaseResumePreview{}, callbackErr
		},
		RecoveryReleaseResumeApply: func(context.Context, accounting.RecoveryReleaseResumeAuthorization) (FundingRecoveryReleaseResumeReadback, error) {
			return FundingRecoveryReleaseResumeReadback{}, callbackErr
		},
	}
	router, admin, _ := newFundingRecoveryReleaseResumeTestRouter(t, service)
	for _, endpoint := range []struct {
		name, method, path string
	}{
		{"preview", http.MethodGet, fundingRecoveryReleaseResumeTestPath + "/preview?release_operation_id=release-1"},
		{"apply", http.MethodPost, fundingRecoveryReleaseResumeTestPath},
	} {
		for _, test := range []struct {
			name         string
			err          error
			status       int
			errorCode    string
			holdReleased bool
			gateCleared  bool
		}{
			{"hold_already_released", fmt.Errorf("resume readback: %w", &FundingRecoveryReleaseStateError{HoldReleased: true, GateCleared: true, Cause: errors.New("injected readback failure")}), http.StatusConflict, "funding_recovery_release_unverified", true, true},
			{"gate_cleanup_failed", &FundingRecoveryReleaseStateError{HoldReleased: true, Cause: errors.New("injected gate cleanup failure")}, http.StatusConflict, "funding_recovery_release_unverified", true, false},
			{"conflict", accounting.ErrRecoveryConflict, http.StatusConflict, "funding_recovery_release_conflict", false, false},
			{"operation_conflict", accounting.ErrRecoveryOperationConflict, http.StatusConflict, "funding_recovery_release_conflict", false, false},
			{"not_found", storage.ErrFundingRecoveryNotFound, http.StatusNotFound, "funding_recovery_episode_not_found", false, false},
		} {
			t.Run(endpoint.name+"/"+test.name, func(t *testing.T) {
				callbackErr = test.err
				response := performFundingRecoveryReleaseResumeRequest(router, endpoint.method, endpoint.path, fundingRecoveryReleaseResumeTestBody(t), admin)
				var envelope struct {
					Error             string `json:"error"`
					HoldReleased      *bool  `json:"hold_released"`
					GateCleared       bool   `json:"recovery_gate_cleared"`
					ExecutionReplayed *bool  `json:"execution_replayed"`
				}
				if err := json.Unmarshal(response.Body.Bytes(), &envelope); err != nil {
					t.Fatal(err)
				}
				if response.Code != test.status || envelope.Error != test.errorCode || envelope.HoldReleased == nil || *envelope.HoldReleased != test.holdReleased || envelope.GateCleared != test.gateCleared {
					t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
				}
				if test.holdReleased && (envelope.ExecutionReplayed == nil || *envelope.ExecutionReplayed) {
					t.Fatalf("state error lost execution replay fact: %s", response.Body.String())
				}
			})
		}
	}
}

func TestFundingRecoveryReleaseResumeUnavailableWithoutCallback(t *testing.T) {
	router, admin, _ := newFundingRecoveryReleaseResumeTestRouter(t, nil)
	for _, test := range []struct {
		name    string
		service *OrderReconciliationService
	}{
		{"no_service", nil},
		{"no_callback", &OrderReconciliationService{Recovery: &storage.SQLStorage{}}},
	} {
		SetOrderReconciliationService(test.service)
		for _, endpoint := range []struct {
			name, method, path string
		}{
			{"preview", http.MethodGet, fundingRecoveryReleaseResumeTestPath + "/preview?release_operation_id=release-1"},
			{"apply", http.MethodPost, fundingRecoveryReleaseResumeTestPath},
		} {
			t.Run(test.name+"/"+endpoint.name, func(t *testing.T) {
				response := performFundingRecoveryReleaseResumeRequest(router, endpoint.method, endpoint.path, fundingRecoveryReleaseResumeTestBody(t), admin)
				if response.Code != http.StatusServiceUnavailable {
					t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
				}
			})
		}
	}
}
