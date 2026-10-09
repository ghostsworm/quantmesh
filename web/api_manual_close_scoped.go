package web

import (
	"context"
	"errors"
	"net/http"
	"reflect"
	"strings"

	"github.com/gin-gonic/gin"
)

var ErrManualCloseScopeAmbiguous = errors.New("manual close scope ambiguous")
var ErrManualCloseScopeUnavailable = errors.New("manual close scope unavailable")

type scopedManualCloser interface {
	ClosePositionsScoped(context.Context, string, string, string, string) (*ClosePositionsResponse, error)
}

func closeAllPositionsScoped(c *gin.Context) {
	exchange, symbol := strings.TrimSpace(c.Query("exchange")), strings.TrimSpace(c.Query("symbol"))
	if exchange == "" || symbol == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "manual_close_scope_incomplete"})
		return
	}
	closer, ok := symbolManagerProvider.(scopedManualCloser)
	if !ok || closer == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "manual_close_scoped_provider_unavailable"})
		return
	}
	if rv := reflect.ValueOf(closer); rv.Kind() == reflect.Ptr && rv.IsNil() {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "manual_close_scoped_provider_unavailable"})
		return
	}
	result, err := closer.ClosePositionsScoped(c.Request.Context(), exchange, symbol, strings.TrimSpace(c.Query("market_type")), strings.TrimSpace(c.Query("bot_id")))
	if err != nil {
		status, code := http.StatusInternalServerError, "manual_close_unverified"
		if errors.Is(err, ErrManualCloseScopeAmbiguous) {
			status, code = http.StatusConflict, "manual_close_scope_ambiguous"
		}
		if errors.Is(err, ErrManualCloseScopeUnavailable) {
			status, code = http.StatusServiceUnavailable, "manual_close_scope_unavailable"
		}
		c.JSON(status, gin.H{"error": code, "requires_reconciliation": true})
		return
	}
	if result == nil || result.SuccessCount < 0 || result.FailCount != 0 {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "manual_close_result_unverified", "requires_reconciliation": true})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success_count": result.SuccessCount, "fail_count": 0, "message": "manual_close_verified"})
}
