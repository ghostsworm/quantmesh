package web

import (
	"context"
	"net/http"
	"strings"

	"quantmesh/config"

	"github.com/gin-gonic/gin"
)

// BotResponse Bot 列表項
type BotResponse struct {
	BotID         string  `json:"bot_id"`
	Name          string  `json:"name"`
	Exchange      string  `json:"exchange"`
	Symbol        string  `json:"symbol"`
	MarketType    string  `json:"market_type"`
	Running       bool    `json:"running"`
	CurrentPrice  float64 `json:"current_price,omitempty"`
	TotalPnL      float64 `json:"total_pnl,omitempty"`
	TotalTrades   int     `json:"total_trades,omitempty"`
	RiskTriggered bool    `json:"risk_triggered,omitempty"`
	Uptime        int64   `json:"uptime,omitempty"`
}

// BotDetailResponse Bot 詳情（含持倉、訂單等）
type BotDetailResponse struct {
	BotResponse
	Config *config.BotConfig `json:"config,omitempty"`
}

// BotManagerProvider Bot 管理提供者（由 main 注入）
type BotManagerProvider interface {
	ListBots() []BotResponse
	GetBot(botID string) (*BotDetailResponse, bool)
	StartBot(ctx context.Context, botCfg config.BotConfig) error
	StopBot(botID string) error
}

var botManagerProvider BotManagerProvider

// RegisterBotManagerProvider 註冊 Bot 管理提供者
func RegisterBotManagerProvider(provider BotManagerProvider) {
	botManagerProvider = provider
}

// getBots 獲取 Bot 列表
// GET /api/bots
func getBots(c *gin.Context) {
	if botManagerProvider == nil {
		c.JSON(http.StatusOK, gin.H{"bots": []BotResponse{}})
		return
	}
	bots := botManagerProvider.ListBots()
	c.JSON(http.StatusOK, gin.H{"bots": bots})
}

// getBotByID 獲取 Bot 詳情
// GET /api/bots/:id
func getBotByID(c *gin.Context) {
	botID := c.Param("id")
	if botID == "" {
		respondError(c, http.StatusBadRequest, "error.invalid_bot_id")
		return
	}
	if botManagerProvider == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Bot manager not available"})
		return
	}
	bot, ok := botManagerProvider.GetBot(botID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "Bot not found"})
		return
	}
	c.JSON(http.StatusOK, bot)
}

// postBotStart 啟動 Bot
// POST /api/bots/:id/start
func postBotStart(c *gin.Context) {
	botID := c.Param("id")
	if botID == "" {
		respondError(c, http.StatusBadRequest, "error.invalid_bot_id")
		return
	}
	if botManagerProvider == nil {
		respondError(c, http.StatusServiceUnavailable, "error.bot_manager_unavailable")
		return
	}
	cfg, err := GetLatestConfig()
	if err != nil || cfg == nil {
		respondError(c, http.StatusInternalServerError, "error.config_load_failed")
		return
	}
	var botCfg *config.BotConfig
	for i := range cfg.Bots {
		id := cfg.Bots[i].ID
		if id == "" {
			id = config.GenerateBotID(cfg.Bots[i].Exchange, cfg.Bots[i].Symbol, cfg.Bots[i].GetMarketType())
		}
		if id == botID {
			botCfg = &cfg.Bots[i]
			break
		}
	}
	if botCfg == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Bot config not found"})
		return
	}
	if err := botManagerProvider.StartBot(c.Request.Context(), *botCfg); err != nil {
		// 區分衝突錯誤和其他錯誤，衝突返回 409 Conflict
		if strings.Contains(err.Error(), "symbol_conflict") {
			c.JSON(http.StatusConflict, gin.H{"error": err.Error(), "error_key": "error.bot_symbol_conflict"})
			return
		}
		respondError(c, http.StatusInternalServerError, "error.bot_start_failed", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "bot_id": botID})
}

// postBotStop 停止 Bot
// POST /api/bots/:id/stop
func postBotStop(c *gin.Context) {
	botID := c.Param("id")
	if botID == "" {
		respondError(c, http.StatusBadRequest, "error.invalid_bot_id")
		return
	}
	if botManagerProvider == nil {
		respondError(c, http.StatusServiceUnavailable, "error.bot_manager_unavailable")
		return
	}
	if err := botManagerProvider.StopBot(botID); err != nil {
		respondError(c, http.StatusInternalServerError, "error.bot_stop_failed", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "bot_id": botID})
}
