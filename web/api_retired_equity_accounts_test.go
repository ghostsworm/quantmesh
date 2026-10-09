package web

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestRetiredEquityAccountStatusRequiresAdminAndDoesNotExposeCredentials(t *testing.T) {
	gin.SetMode(gin.TestMode)
	oldReader := retiredEquityAccountStatusState.reader
	t.Cleanup(func() { SetRetiredEquityAccountStatusReader(oldReader) })
	SetRetiredEquityAccountStatusReader(func(context.Context) ([]RetiredEquityAccountStatus, error) {
		return []RetiredEquityAccountStatus{{ID: "hashed-account-id", Exchange: "binance", MarketType: "futures",
			AccountScope: "scope-hash", Status: "pending_verification", RetiredAt: "2026-10-09T00:00:00Z",
			FlatEvidenceCount: 1, LastEvidenceResult: "flat"}}, nil
	})
	for _, tc := range []struct {
		name       string
		role       string
		localDev   bool
		wantStatus int
	}{{name: "anonymous", wantStatus: http.StatusForbidden}, {name: "non-admin", role: "user", wantStatus: http.StatusForbidden},
		{name: "local development", role: "admin", localDev: true, wantStatus: http.StatusForbidden}, {name: "admin", role: "admin", wantStatus: http.StatusOK}} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/api/capital/retired-equity-accounts", nil)
			if tc.role != "" {
				c.Set("session", &Session{Username: "operator", Role: tc.role})
			}
			if tc.localDev {
				c.Set("local_dev_mode", true)
			}
			getRetiredEquityAccountStatusesHandler(c)
			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d: %s", w.Code, tc.wantStatus, w.Body.String())
			}
			if tc.wantStatus == http.StatusOK {
				for _, forbidden := range []string{"api_key", "secret_key", "passphrase", "account-key", "account-secret"} {
					if strings.Contains(strings.ToLower(w.Body.String()), forbidden) {
						t.Fatalf("status response exposed credential material: %s", w.Body.String())
					}
				}
				if !strings.Contains(w.Body.String(), "pending_verification") || !strings.Contains(w.Body.String(), "flat_evidence_count") {
					t.Fatalf("status response omitted operator-visible evidence state: %s", w.Body.String())
				}
			}
		})
	}
}

func TestRetiredEquityAccountStatusFailsClosedWhenReaderUnavailable(t *testing.T) {
	oldReader := retiredEquityAccountStatusState.reader
	t.Cleanup(func() { SetRetiredEquityAccountStatusReader(oldReader) })
	for _, tc := range []struct {
		name   string
		reader RetiredEquityAccountStatusReader
	}{{name: "missing reader"}, {name: "storage error", reader: func(context.Context) ([]RetiredEquityAccountStatus, error) {
		return nil, errors.New("storage unavailable")
	}}} {
		t.Run(tc.name, func(t *testing.T) {
			SetRetiredEquityAccountStatusReader(tc.reader)
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/api/capital/retired-equity-accounts", nil)
			c.Set("session", &Session{Username: "operator", Role: "admin"})
			getRetiredEquityAccountStatusesHandler(c)
			if w.Code != http.StatusServiceUnavailable {
				t.Fatalf("status = %d, want %d", w.Code, http.StatusServiceUnavailable)
			}
		})
	}
}

func TestRetiredEquityResetIsAdminOnlyAndReturnsAuditableOperation(t *testing.T) {
	oldResetter := retiredEquityAccountStatusState.resetter
	t.Cleanup(func() { SetRetiredEquityAccountResetter(oldResetter) })
	calls := 0
	SetRetiredEquityAccountResetter(func(_ context.Context, actor string) (RetiredEquityAccountResetOperation, error) {
		calls++
		if actor != "operator" {
			t.Fatalf("reset actor = %q", actor)
		}
		return RetiredEquityAccountResetOperation{ID: "op-123", Actor: actor, TargetScope: "scope-hash",
			StartedAt: time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC), CompletedAt: time.Date(2026, 10, 9, 12, 1, 0, 0, time.UTC),
			BaselineRevision: 9, RetiredAccountIDs: []string{"account-hash"}}, nil
	})
	for _, tc := range []struct {
		name       string
		role       string
		localDev   bool
		wantStatus int
	}{{name: "anonymous", wantStatus: http.StatusForbidden}, {name: "non-admin", role: "user", wantStatus: http.StatusForbidden},
		{name: "local development", role: "admin", localDev: true, wantStatus: http.StatusForbidden},
		{name: "admin", role: "admin", wantStatus: http.StatusOK}} {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodPost, "/api/capital/retired-equity-accounts/reset", nil)
			if tc.role != "" {
				c.Set("session", &Session{Username: "operator", Role: tc.role})
			}
			if tc.localDev {
				c.Set("local_dev_mode", true)
			}
			resetRetiredEquityAccountsHandler(c)
			if w.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d: %s", w.Code, tc.wantStatus, w.Body.String())
			}
			if tc.name == "admin" {
				for _, expected := range []string{"op-123", "scope-hash", "baseline_revision", "completed_at"} {
					if !strings.Contains(w.Body.String(), expected) {
						t.Fatalf("reset response omitted %q: %s", expected, w.Body.String())
					}
				}
			}
		})
	}
	if calls != 1 {
		t.Fatalf("reset executor calls = %d, want exactly one authorized call", calls)
	}
}

func TestRetiredEquityResetHistoryIsAdminOnlyAndFailClosed(t *testing.T) {
	oldReader := retiredEquityAccountStatusState.historyReader
	t.Cleanup(func() { SetRetiredEquityAccountResetHistoryReader(oldReader) })
	SetRetiredEquityAccountResetHistoryReader(func(context.Context) ([]RetiredEquityAccountResetOperation, error) {
		return []RetiredEquityAccountResetOperation{{ID: "op-123", Actor: "operator", TargetScope: "scope-hash",
			BaselineRevision: 9, RetiredAccountIDs: []string{"account-hash"}}}, nil
	})
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/capital/retired-equity-accounts/reset-history", nil)
	c.Set("session", &Session{Username: "operator", Role: "admin"})
	getRetiredEquityAccountResetHistoryHandler(c)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "op-123") || !strings.Contains(w.Body.String(), "baseline_revision") {
		t.Fatalf("admin reset history response = %d %s", w.Code, w.Body.String())
	}
}
