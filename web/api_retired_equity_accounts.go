package web

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"quantmesh/logger"
)

type RetiredEquityAccountStatus struct {
	ID                 string `json:"id"`
	Exchange           string `json:"exchange"`
	MarketType         string `json:"market_type"`
	AccountScope       string `json:"account_scope"`
	Status             string `json:"status"`
	RetiredAt          string `json:"retired_at"`
	LastObservedAt     string `json:"last_observed_at,omitempty"`
	LastFlatAt         string `json:"last_flat_at,omitempty"`
	FlatEvidenceCount  int    `json:"flat_evidence_count"`
	LastEvidenceResult string `json:"last_evidence_result,omitempty"`
}

type RetiredEquityAccountStatusReader func(context.Context) ([]RetiredEquityAccountStatus, error)

type RetiredEquityAccountResetOperation struct {
	ID                string    `json:"id"`
	Actor             string    `json:"actor"`
	TargetScope       string    `json:"target_scope"`
	StartedAt         time.Time `json:"started_at"`
	CompletedAt       time.Time `json:"completed_at,omitempty"`
	BaselineRevision  int64     `json:"baseline_revision,omitempty"`
	RetiredAccountIDs []string  `json:"retired_account_ids"`
}

type RetiredEquityAccountResetHistoryReader func(context.Context) ([]RetiredEquityAccountResetOperation, error)
type RetiredEquityAccountResetter func(context.Context, string) (RetiredEquityAccountResetOperation, error)

var retiredEquityAccountStatusState struct {
	sync.RWMutex
	reader        RetiredEquityAccountStatusReader
	historyReader RetiredEquityAccountResetHistoryReader
	resetter      RetiredEquityAccountResetter
}

func SetRetiredEquityAccountStatusReader(reader RetiredEquityAccountStatusReader) {
	retiredEquityAccountStatusState.Lock()
	retiredEquityAccountStatusState.reader = reader
	retiredEquityAccountStatusState.Unlock()
}

func SetRetiredEquityAccountResetHistoryReader(reader RetiredEquityAccountResetHistoryReader) {
	retiredEquityAccountStatusState.Lock()
	retiredEquityAccountStatusState.historyReader = reader
	retiredEquityAccountStatusState.Unlock()
}

func SetRetiredEquityAccountResetter(resetter RetiredEquityAccountResetter) {
	retiredEquityAccountStatusState.Lock()
	retiredEquityAccountStatusState.resetter = resetter
	retiredEquityAccountStatusState.Unlock()
}

func getRetiredEquityAccountStatusesHandler(c *gin.Context) {
	if !requireRetiredEquityAdmin(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin_required"})
		return
	}
	retiredEquityAccountStatusState.RLock()
	reader := retiredEquityAccountStatusState.reader
	retiredEquityAccountStatusState.RUnlock()
	if reader == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "retired_equity_account_status_unavailable"})
		return
	}
	statuses, err := reader(c.Request.Context())
	if err != nil {
		logger.ErrorCtx(c.Request.Context(), "read retired equity account statuses: %v", err)
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "retired_equity_account_status_unavailable"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"accounts": statuses})
}

func getRetiredEquityAccountResetHistoryHandler(c *gin.Context) {
	if !requireRetiredEquityAdmin(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin_required"})
		return
	}
	retiredEquityAccountStatusState.RLock()
	reader := retiredEquityAccountStatusState.historyReader
	retiredEquityAccountStatusState.RUnlock()
	if reader == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "retired_equity_account_reset_history_unavailable"})
		return
	}
	history, err := reader(c.Request.Context())
	if err != nil {
		logger.ErrorCtx(c.Request.Context(), "read retired equity account reset history: %v", err)
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "retired_equity_account_reset_history_unavailable"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"history": history})
}

func resetRetiredEquityAccountsHandler(c *gin.Context) {
	if !requireRetiredEquityAdmin(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin_required"})
		return
	}
	session, _ := c.Get("session")
	actor := session.(*Session).Username
	retiredEquityAccountStatusState.RLock()
	resetter := retiredEquityAccountStatusState.resetter
	retiredEquityAccountStatusState.RUnlock()
	if resetter == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "retired_equity_account_reset_unavailable"})
		return
	}
	operation, err := resetter(c.Request.Context(), actor)
	if err != nil {
		logger.ErrorCtx(c.Request.Context(), "admin %q retired-account reset failed: %v", actor, err)
		response := gin.H{"error": "retired_equity_account_reset_not_completed"}
		if operation.ID != "" {
			response["operation"] = operation
		}
		c.JSON(http.StatusConflict, response)
		return
	}
	c.JSON(http.StatusOK, gin.H{"operation": operation, "status": "completed"})
}

func requireRetiredEquityAdmin(c *gin.Context) bool {
	sessionValue, ok := c.Get("session")
	session, isSession := sessionValue.(*Session)
	return !c.GetBool("local_dev_mode") && ok && isSession && session.Role == "admin" && session.Username != ""
}
