package web

import (
	"context"
	"net/http"
	"quantmesh/config"
	"quantmesh/position"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// BotExtendedProvider 擴展的 Bot 管理提供者接口
type BotExtendedProvider interface {
	GetBot(botID string) (*BotExtended, bool)
}

// BotExtended 擴展的 Bot 接口
type BotExtended interface {
	ClosePositions(ctx context.Context, cfg config.ClosePositionConfig) (*position.ClosePositionRecord, error)
	GetCloseRecords() []*position.ClosePositionRecord
	GetSlotFilter() *config.SlotFilterConfig
	SetSlotFilter(filter *config.SlotFilterConfig)
	GetSlots() []map[string]interface{}

	// Bot 风控相关方法
	GetBotRiskControl() *config.BotRiskControl
	SetBotRiskControl(riskControl *config.BotRiskControl) error
	PauseOpening(reason string)
	ResumeOpening()
	GetPositionStatus() map[string]interface{}
}

var botExtendedProvider BotExtendedProvider

// RegisterBotExtendedProvider 註冊擴展的 Bot 管理提供者
func RegisterBotExtendedProvider(provider BotExtendedProvider) {
	botExtendedProvider = provider
}

// ClosePositionsV2Request 平倉請求
type ClosePositionsV2Request struct {
	Method      string  `json:"method" binding:"required"`      // market/limit
	PriceOffset float64 `json:"price_offset,omitempty"`         // 限價偏移（%）
	TimeoutSec  int     `json:"timeout_sec,omitempty"`         // 超時時間（秒）
	AutoRetry   bool    `json:"auto_retry,omitempty"`          // 是否自動重試
}

// ClosePositionsV2Response 平倉響應
type ClosePositionsV2Response struct {
	Success      bool   `json:"success"`
	RecordID     string `json:"record_id,omitempty"`
	OrderID      int64  `json:"order_id,omitempty"`
	Status       string `json:"status,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
}

// ClosePositionRecordResponse 平倉記錄響應
type ClosePositionRecordResponse struct {
	RecordID     string  `json:"record_id"`
	BotID        string  `json:"bot_id"`
	Symbol       string  `json:"symbol"`
	Side         string  `json:"side"`
	TargetQty    float64 `json:"target_qty"`
	FilledQty    float64 `json:"filled_qty"`
	Method       string  `json:"method"`
	Price        float64 `json:"price"`
	OrderID      int64   `json:"order_id"`
	Status       string  `json:"status"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`
	TimeoutAt    string  `json:"timeout_at,omitempty"`
	RetryCount   int     `json:"retry_count"`
	ErrorMessage string  `json:"error_message,omitempty"`
}

// closePositionsV2 平倉API V2（支持市價/限價）
// POST /api/v2/bots/:id/close-positions
func closePositionsV2(c *gin.Context) {
	botID := c.Param("id")
	var req ClosePositionsV2Request
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 驗證 method
	if req.Method != "market" && req.Method != "limit" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "method must be 'market' or 'limit'"})
		return
	}

	// 獲取Bot實例
	if botExtendedProvider == nil {
		respondError(c, http.StatusServiceUnavailable, "error.bot_manager_unavailable")
		return
	}

	bot, ok := botExtendedProvider.GetBot(botID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "Bot not found"})
		return
	}

	// 調用平倉
	cfg := config.ClosePositionConfig{
		Method:      req.Method,
		PriceOffset: req.PriceOffset,
		TimeoutSec:  req.TimeoutSec,
		AutoRetry:   req.AutoRetry,
		MaxRetries:  3,
	}

	result, err := bot.ClosePositions(context.Background(), cfg)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ClosePositionsV2Response{
			Success:      false,
			ErrorMessage: err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, ClosePositionsV2Response{
		Success:  true,
		RecordID: result.RecordID,
		OrderID:  result.OrderID,
		Status:   result.Status,
	})
}

// getClosePositionRecords 獲取平倉記錄
// GET /api/v2/bots/:id/close-records
func getClosePositionRecords(c *gin.Context) {
	botID := c.Param("id")

	if botExtendedProvider == nil {
		respondError(c, http.StatusServiceUnavailable, "error.bot_manager_unavailable")
		return
	}

	bot, ok := botExtendedProvider.GetBot(botID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "Bot not found"})
		return
	}

	records := bot.GetCloseRecords()

	// 轉換為響應格式
	responseRecords := make([]ClosePositionRecordResponse, len(records))
	for i, r := range records {
		responseRecords[i] = ClosePositionRecordResponse{
			RecordID:     r.RecordID,
			BotID:        r.BotID,
			Symbol:       r.Symbol,
			Side:         r.Side,
			TargetQty:    r.TargetQty,
			FilledQty:    r.FilledQty,
			Method:       string(r.Method),
			Price:        r.Price,
			OrderID:      r.OrderID,
			Status:       r.Status,
			CreatedAt:    r.CreatedAt.Format(time.RFC3339),
			UpdatedAt:    r.UpdatedAt.Format(time.RFC3339),
			TimeoutAt:    r.TimeoutAt.Format(time.RFC3339),
			RetryCount:   r.RetryCount,
			ErrorMessage: r.ErrorMessage,
		}
	}

	c.JSON(http.StatusOK, gin.H{"records": responseRecords})
}

// retryClosePosition 重試平倉（手動觸發）
// POST /api/v2/close-records/:record_id/retry
func retryClosePosition(c *gin.Context) {
	recordID := c.Param("record_id")

	var req struct {
		Method string `json:"method" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Method != "market" && req.Method != "limit" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "method must be 'market' or 'limit'"})
		return
	}

	// 通過全局管理器獲取記錄
	// 注意：這需要在 web/server.go 中註冊全局的 closePositionManager

	// 這裡需要實際的實現，暫時返回未實現
	c.JSON(http.StatusNotImplemented, gin.H{"error": "Not implemented yet"})
}
