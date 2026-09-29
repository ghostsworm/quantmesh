package web

import (
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"quantmesh/storage"
)

type pendingTradeFeeCorrectionStorage interface {
	GetPendingTradeFeeCorrections(exchange, marketType, symbol, accountScope, botID string) ([]*storage.TradeFeeCorrection, error)
}

type tradeFeeCorrectionReconciler interface {
	ResolveTradeFeeCorrection(correctionID, exchange, marketType, symbol, accountScope, botID, evidence string, resolvedAt time.Time) error
}

type pendingTradeFeeCorrectionResponse struct {
	CorrectionID  string    `json:"correction_id"`
	BotID         string    `json:"bot_id"`
	Exchange      string    `json:"exchange"`
	MarketType    string    `json:"market_type"`
	Symbol        string    `json:"symbol"`
	OrderID       int64     `json:"order_id"`
	ClientOrderID string    `json:"client_order_id,omitempty"`
	Leg           string    `json:"leg"`
	Side          string    `json:"side"`
	Fee           float64   `json:"fee"`
	FeeAsset      string    `json:"fee_asset,omitempty"`
	ExecutedQty   float64   `json:"executed_qty"`
	Reason        string    `json:"reason"`
	LegacyEvent   bool      `json:"legacy_unscoped_event"`
	CanApply      bool      `json:"basic_apply_eligibility"`
	CreatedAt     time.Time `json:"created_at"`
}

// getPendingTradeFeeCorrectionsHandler lists unresolved fee-ledger holds only
// for the currently selected runtime scope. It never resolves or releases them.
func getPendingTradeFeeCorrectionsHandler(c *gin.Context) {
	exchange := strings.TrimSpace(c.Query("exchange"))
	symbol := strings.TrimSpace(c.Query("symbol"))
	marketType := strings.TrimSpace(c.Query("market_type"))
	botID := strings.TrimSpace(c.Query("bot_id"))
	if exchange == "" || symbol == "" || marketType == "" || botID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "exchange, symbol, market_type, and bot_id are required"})
		return
	}

	status := pickStatus(c)
	if status == nil || !strings.EqualFold(status.Exchange, exchange) || !strings.EqualFold(status.Symbol, symbol) ||
		!strings.EqualFold(status.MarketType, marketType) || strings.TrimSpace(status.AccountScope) == "" {
		c.JSON(http.StatusConflict, gin.H{"error": "requested scope does not match a verified active runtime"})
		return
	}

	storageProvider := PickStorageProvider(c)
	if storageProvider == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "fee reconciliation storage is unavailable"})
		return
	}
	store := storageProvider.GetStorage()
	if store == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "fee reconciliation storage is unavailable"})
		return
	}
	correctionStorage, ok := store.(pendingTradeFeeCorrectionStorage)
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "fee reconciliation storage is unsupported"})
		return
	}
	corrections, err := correctionStorage.GetPendingTradeFeeCorrections(exchange, marketType, symbol, status.AccountScope, botID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to query pending fee reconciliations"})
		return
	}

	response := make([]pendingTradeFeeCorrectionResponse, 0, len(corrections))
	for _, correction := range corrections {
		if correction == nil {
			continue
		}
		response = append(response, pendingTradeFeeCorrectionResponse{
			CorrectionID: correction.CorrectionID, BotID: correction.BotID, Exchange: correction.Exchange,
			MarketType: correction.MarketType, Symbol: correction.Symbol, OrderID: correction.OrderID,
			ClientOrderID: correction.ClientOrderID, Leg: correction.Leg, Side: correction.Side,
			Fee: correction.Fee, FeeAsset: correction.FeeAsset, ExecutedQty: correction.ExecutedQty,
			Reason: correction.Reason, LegacyEvent: correction.LegacyEvent,
			CanApply:  !correction.LegacyEvent && correction.Fee > 0 && correction.FeeAsset != "" && correction.ExecutedQty > 0 && correction.BaseFeeQty == 0,
			CreatedAt: correction.CreatedAt,
		})
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "pending": response, "resolution_supported": true})
}

type reconcileTradeFeeCorrectionRequest struct {
	BotID      string `json:"bot_id" binding:"required"`
	Exchange   string `json:"exchange" binding:"required"`
	MarketType string `json:"market_type" binding:"required"`
	Symbol     string `json:"symbol" binding:"required"`
	Evidence   string `json:"evidence" binding:"required"`
	Confirm    bool   `json:"confirm_apply"`
}

// reconcileTradeFeeCorrectionHandler applies only a full, exact, quote-asset
// fee allocation. The running opening gate stays held until a restart reloads
// the persisted pending-correction set.
func reconcileTradeFeeCorrectionHandler(c *gin.Context) {
	correctionID := strings.TrimSpace(c.Param("id"))
	var request reconcileTradeFeeCorrectionRequest
	if correctionID == "" || c.ShouldBindJSON(&request) != nil || !request.Confirm || len(strings.TrimSpace(request.Evidence)) < 12 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "valid correction identity, evidence, and explicit confirmation are required"})
		return
	}
	status := pickStatus(c)
	if status == nil || !strings.EqualFold(status.Exchange, request.Exchange) || !strings.EqualFold(status.Symbol, request.Symbol) ||
		!strings.EqualFold(status.MarketType, request.MarketType) || strings.TrimSpace(status.AccountScope) == "" {
		c.JSON(http.StatusConflict, gin.H{"error": "requested scope does not match a verified active runtime"})
		return
	}
	storageProvider := PickStorageProvider(c)
	if storageProvider == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "fee reconciliation storage is unavailable"})
		return
	}
	store := storageProvider.GetStorage()
	if store == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "fee reconciliation storage is unavailable"})
		return
	}
	reconciler, ok := store.(tradeFeeCorrectionReconciler)
	if !ok {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "fee reconciliation storage is unsupported"})
		return
	}
	if err := reconciler.ResolveTradeFeeCorrection(correctionID, request.Exchange, request.MarketType,
		request.Symbol, status.AccountScope, strings.TrimSpace(request.BotID), strings.TrimSpace(request.Evidence), time.Now().UTC()); err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "fee correction could not be applied; the opening hold remains active"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "restart_required": true})
}
