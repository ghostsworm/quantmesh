package web

import (
	"net/http"
	"time"

	"quantmesh/config"
	"quantmesh/logger"

	"github.com/gin-gonic/gin"
)

// BotRiskControlRequest 风控配置请求
type BotRiskControlRequest struct {
	Enabled             *bool    `json:"enabled"`
	MaxPositionQuantity *float64 `json:"max_position_quantity"`
	MaxPositionValue    *float64 `json:"max_position_value"`
	MaxPositionLayers   *int     `json:"max_position_layers"`
	MaxOpenOrders       *int     `json:"max_open_orders"`       // 最多開倉掛單數，0=不限制
	OpenOrderDistance   *float64 `json:"open_order_distance"`  // 開倉單距離當前價的最大間隔數
	StopLossRatio       *float64 `json:"stop_loss_ratio"`
	TakeProfitRatio     *float64 `json:"take_profit_ratio"`
	TrailingStopRatio   *float64 `json:"trailing_stop_ratio"`
	TrendFilterEnabled  *bool    `json:"trend_filter_enabled"`
}

// PauseOpeningRequest 暂停开仓请求
type PauseOpeningRequest struct {
	Reason        string `json:"reason"`
	AutoResumeSec *int   `json:"auto_resume_sec"` // 自动恢复时间（秒），0=不自动恢复
}

// getBotRiskControl 获取 Bot 风控配置（运行中从运行时取，已停止从配置取）
func getBotRiskControl(c *gin.Context) {
	botID := c.Param("id")
	bot, ok := botExtendedProvider.GetBot(botID)
	if ok {
		riskControl := bot.GetBotRiskControl()
		c.JSON(http.StatusOK, riskControl)
		return
	}
	// 已停止的 Bot：从配置读取风控
	cfg, err := GetLatestConfig()
	if err != nil || cfg == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Bot not found"})
		return
	}
	for i := range cfg.Bots {
		id := cfg.Bots[i].ID
		if id == "" {
			id = config.GenerateBotID(cfg.Bots[i].Exchange, cfg.Bots[i].Symbol, cfg.Bots[i].GetMarketType())
		}
		if id == botID {
			rc := cfg.Bots[i].OpenPositionControl.BotRiskControl
			if rc == nil {
				rc = &config.BotRiskControl{}
			}
			c.JSON(http.StatusOK, rc)
			return
		}
	}
	c.JSON(http.StatusNotFound, gin.H{"error": "Bot not found"})
}

// updateBotRiskControl 更新 Bot 风控配置
func updateBotRiskControl(c *gin.Context) {
	botID := c.Param("id")
	bot, ok := botExtendedProvider.GetBot(botID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "Bot not found"})
		return
	}

	var req BotRiskControlRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 获取当前风控配置
	currentRiskControl := bot.GetBotRiskControl()
	if currentRiskControl == nil {
		currentRiskControl = &config.BotRiskControl{}
	}

	// 更新非空字段
	if req.Enabled != nil {
		currentRiskControl.Enabled = *req.Enabled
	}
	if req.MaxPositionQuantity != nil {
		currentRiskControl.MaxPositionQuantity = *req.MaxPositionQuantity
	}
	if req.MaxPositionValue != nil {
		currentRiskControl.MaxPositionValue = *req.MaxPositionValue
	}
	if req.MaxPositionLayers != nil {
		currentRiskControl.MaxPositionLayers = *req.MaxPositionLayers
	}
	if req.MaxOpenOrders != nil {
		currentRiskControl.MaxOpenOrders = *req.MaxOpenOrders
	}
	if req.OpenOrderDistance != nil {
		currentRiskControl.OpenOrderDistance = *req.OpenOrderDistance
	}
	if req.StopLossRatio != nil {
		currentRiskControl.StopLossRatio = *req.StopLossRatio
	}
	if req.TakeProfitRatio != nil {
		currentRiskControl.TakeProfitRatio = *req.TakeProfitRatio
	}
	if req.TrailingStopRatio != nil {
		currentRiskControl.TrailingStopRatio = *req.TrailingStopRatio
	}
	if req.TrendFilterEnabled != nil {
		currentRiskControl.TrendFilterEnabled = *req.TrendFilterEnabled
	}

	// 应用更新后的配置
	if err := bot.SetBotRiskControl(currentRiskControl); err != nil {
		logger.Error("❌ [%s] 更新风控配置失败: %v", botID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	logger.Info("✅ [%s] 风控配置已更新: %+v", botID, currentRiskControl)
	c.JSON(http.StatusOK, currentRiskControl)
}

// pauseBotOpening 暂停 Bot 开仓
func pauseBotOpening(c *gin.Context) {
	botID := c.Param("id")
	bot, ok := botExtendedProvider.GetBot(botID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "Bot not found"})
		return
	}

	var req PauseOpeningRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	// 获取当前风控配置
	riskControl := bot.GetBotRiskControl()

	// 如果设置了自动恢复时间，也更新到风控配置中
	if req.AutoResumeSec != nil && *req.AutoResumeSec > 0 {
		if riskControl == nil {
			riskControl = &config.BotRiskControl{}
		}
		riskControl.AutoResumeAfter = *req.AutoResumeSec
		// 保存风控配置
		if err := bot.SetBotRiskControl(riskControl); err != nil {
			logger.Warn("⚠️ [%s] 保存自动恢复时间失败: %v", botID, err)
		}
		resumeAt := time.Now().Add(time.Duration(*req.AutoResumeSec) * time.Second)
		logger.Info("⏸️ [%s] 暂停开仓（原因: %s），将在 %s 自动恢复", botID, req.Reason, resumeAt.Format("15:04:05"))
	} else {
		logger.Info("⏸️ [%s] 暂停开仓（原因: %s）", botID, req.Reason)
	}

	// 设置暂停状态和原因
	bot.PauseOpening(req.Reason)

	c.JSON(http.StatusOK, gin.H{
		"status":         "paused",
		"reason":         req.Reason,
		"auto_resume_at": req.AutoResumeSec,
	})
}

// resumeBotOpening 恢复 Bot 开仓
func resumeBotOpening(c *gin.Context) {
	botID := c.Param("id")
	bot, ok := botExtendedProvider.GetBot(botID)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"error": "Bot not found"})
		return
	}

	bot.ResumeOpening()
	logger.Info("▶️ [%s] 恢复开仓", botID)

	c.JSON(http.StatusOK, gin.H{
		"status": "resumed",
	})
}

// getBotPositionStatus 获取 Bot 仓位状态（包括是否达到限制）
// 已停止的 Bot 返回 stopped: true 的空状态，避免前端报错
func getBotPositionStatus(c *gin.Context) {
	botID := c.Param("id")
	bot, ok := botExtendedProvider.GetBot(botID)
	if ok {
		status := bot.GetPositionStatus()
		c.JSON(http.StatusOK, status)
		return
	}
	// 已停止的 Bot：返回空仓位状态，供前端友好展示
	cfg, err := GetLatestConfig()
	if err != nil || cfg == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Bot not found"})
		return
	}
	for i := range cfg.Bots {
		id := cfg.Bots[i].ID
		if id == "" {
			id = config.GenerateBotID(cfg.Bots[i].Exchange, cfg.Bots[i].Symbol, cfg.Bots[i].GetMarketType())
		}
		if id == botID {
			c.JSON(http.StatusOK, gin.H{
				"stopped":             true,
				"total_position_qty":  0,
				"total_position_value": 0,
				"position_layers":     0,
				"current_price":       0,
				"paused":              false,
				"reached_limit_qty":   false,
				"reached_limit_value":  false,
				"reached_limit_layers": false,
				"should_stop_opening": false,
			})
			return
		}
	}
	c.JSON(http.StatusNotFound, gin.H{"error": "Bot not found"})
}
