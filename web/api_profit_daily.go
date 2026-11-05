package web

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"quantmesh/accounting/daily"
)

// getProfitDailyReconciliationHandler exposes only an already-published daily
// report. It never computes or widens a report on read, and a missing report is
// not presented as a verified zero.
func getProfitDailyReconciliationHandler(c *gin.Context) {
	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "error": "daily_reconciliation_storage_unavailable"})
		return
	}
	st := storageProv.GetStorage()
	if st == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "error": "daily_reconciliation_storage_unavailable"})
		return
	}
	date := strings.TrimSpace(c.Query("date"))
	exchange := strings.ToLower(strings.TrimSpace(c.Query("exchange_id")))
	scope := strings.TrimSpace(c.Query("account_scope"))
	if date == "" || exchange == "" || scope == "" {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "daily_reconciliation_requires_date_exchange_and_scope"})
		return
	}
	if _, _, err := daily.DayBounds(date); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "daily_reconciliation_date_invalid"})
		return
	}
	authorized, _, err := resolveProfitAccountScopesWithHistory(c.Request.Context(), exchange, st)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "error": "daily_reconciliation_scope_unavailable"})
		return
	}
	granted := false
	for _, candidate := range authorized {
		if strings.EqualFold(candidate.exchange, exchange) && candidate.scope == scope {
			granted = true
			break
		}
	}
	if !granted {
		c.JSON(http.StatusForbidden, gin.H{"success": false, "error": "daily_reconciliation_scope_not_authorized"})
		return
	}
	repo, ok := st.(daily.Repository)
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "error": "daily_reconciliation_unsupported_storage"})
		return
	}
	result, err := daily.NewService(repo).Read(c.Request.Context(), daily.Query{Exchange: exchange, AccountScope: scope, Date: date}, time.Now().UTC())
	if err != nil {
		switch {
		case errors.Is(err, daily.ErrNotFound):
			c.JSON(http.StatusNotFound, gin.H{"success": false, "error": "daily_reconciliation_not_found"})
		case errors.Is(err, daily.ErrInvalid):
			c.JSON(http.StatusBadRequest, gin.H{"success": false, "error": "daily_reconciliation_invalid"})
		default:
			c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "error": "daily_reconciliation_unavailable"})
		}
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "dailyReconciliation": result})
}
