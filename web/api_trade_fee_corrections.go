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
	Reason        string    `json:"reason"`
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
			Fee: correction.Fee, FeeAsset: correction.FeeAsset, Reason: correction.Reason, CreatedAt: correction.CreatedAt,
		})
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "pending": response, "resolution_supported": false})
}
