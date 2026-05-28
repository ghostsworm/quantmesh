package web

import (
	"net/http"
	"strconv"
	"sync"

	"quantmesh/storage"

	"github.com/gin-gonic/gin"
)

// ==================== 價差監控 API ====================

// BasisMonitorProvider 價差監控提供者接口
type BasisMonitorProvider interface {
	GetCurrentBasis(symbol string) (*storage.BasisData, error)
	GetAllCurrentBasis() []*storage.BasisData
	GetBasisHistory(symbol string, limit int) ([]*storage.BasisData, error)
	GetBasisStatistics(symbol string, hours int) (*storage.BasisStats, error)
}

var (
	basisMonitorProvider BasisMonitorProvider
	basisMonitorMu       sync.RWMutex
)

// SetBasisMonitorProvider 設置價差監控提供者
func SetBasisMonitorProvider(provider BasisMonitorProvider) {
	basisMonitorMu.Lock()
	defer basisMonitorMu.Unlock()
	basisMonitorProvider = provider
}

// getBasisMonitorProvider 獲取價差監控提供者
func getBasisMonitorProvider() BasisMonitorProvider {
	basisMonitorMu.RLock()
	defer basisMonitorMu.RUnlock()
	return basisMonitorProvider
}

// getBasisCurrent 獲取當前價差數據
// GET /api/basis/current?symbol=BTCUSDT
func getBasisCurrent(c *gin.Context) {
	provider := getBasisMonitorProvider()
	if provider == nil {
		respondError(c, http.StatusServiceUnavailable, "errors.service_unavailable")
		return
	}

	symbol := c.Query("symbol")
	if symbol == "" {
		// 如果没有指定交易對，返回所有交易對的當前價差
		allBasis := provider.GetAllCurrentBasis()
		c.JSON(http.StatusOK, gin.H{
			"data":  allBasis,
			"count": len(allBasis),
		})
		return
	}

	// 獲取指定交易對的價差
	data, err := provider.GetCurrentBasis(symbol)
	if err != nil {
		respondError(c, http.StatusNotFound, "errors.not_found", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": data})
}

// getBasisHistory 獲取價差历史數據
// GET /api/basis/history?symbol=BTCUSDT&limit=100
func getBasisHistory(c *gin.Context) {
	provider := getBasisMonitorProvider()
	if provider == nil {
		respondError(c, http.StatusServiceUnavailable, "errors.service_unavailable")
		return
	}

	symbol := c.Query("symbol")
	if symbol == "" {
		respondError(c, http.StatusBadRequest, "errors.missing_parameter",
			map[string]interface{}{"param": "symbol"})
		return
	}

	limit := 100
	if limitStr := c.Query("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	history, err := provider.GetBasisHistory(symbol, limit)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "errors.internal_error", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":  history,
		"count": len(history),
	})
}

// getBasisStatistics 獲取價差统计數據
// GET /api/basis/statistics?symbol=BTCUSDT&hours=24
func getBasisStatistics(c *gin.Context) {
	provider := getBasisMonitorProvider()
	if provider == nil {
		respondError(c, http.StatusServiceUnavailable, "errors.service_unavailable")
		return
	}

	symbol := c.Query("symbol")
	if symbol == "" {
		respondError(c, http.StatusBadRequest, "errors.missing_parameter",
			map[string]interface{}{"param": "symbol"})
		return
	}

	hours := 24
	if hoursStr := c.Query("hours"); hoursStr != "" {
		if h, err := strconv.Atoi(hoursStr); err == nil && h > 0 {
			hours = h
		}
	}

	stats, err := provider.GetBasisStatistics(symbol, hours)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "errors.internal_error", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": stats})
}
