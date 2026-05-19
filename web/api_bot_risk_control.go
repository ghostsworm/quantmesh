package web

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"quantmesh/config"
	"quantmesh/logger"

	"github.com/gin-gonic/gin"
)

// BotRiskControlRequest 风控配置请求
type BotRiskControlRequest struct {
	Enabled             *bool                 `json:"enabled"`
	MaxPositionQuantity *float64              `json:"max_position_quantity"`
	MaxPositionValue    *float64              `json:"max_position_value"`
	MaxPositionLayers   *int                  `json:"max_position_layers"`
	MaxOpenOrders       *int                  `json:"max_open_orders"`     // 最多開倉掛單數，0=不限制
	OpenOrderDistance   *float64              `json:"open_order_distance"` // 開倉單距離當前價的最大間隔數
	StopLossRatio       *float64              `json:"stop_loss_ratio"`
	TakeProfitRatio     *float64              `json:"take_profit_ratio"`
	TrailingStopRatio   *float64              `json:"trailing_stop_ratio"`
	TrendFilterEnabled  *bool                 `json:"trend_filter_enabled"`
	GridRiskControl     *GridRiskControlPatch `json:"grid_risk_control,omitempty"` // 網格風控（止損、止盈、回撤等）
}

type GridRiskControlPatch struct {
	Enabled                    *bool    `json:"enabled"`
	MaxGridLayers              *int     `json:"max_grid_layers"`
	MaxOpenOrdersAtCap         *int     `json:"max_open_orders_at_cap"`
	StopLossRatio              *float64 `json:"stop_loss_ratio"`
	TakeProfitTriggerRatio     *float64 `json:"take_profit_trigger_ratio"`
	TrailingTakeProfitRatio    *float64 `json:"trailing_take_profit_ratio"`
	TrendFilterEnabled         *bool    `json:"trend_filter_enabled"`
	CloseConditionEnabled      *bool    `json:"close_condition_enabled"`
	CloseConditionProfitTarget *float64 `json:"close_condition_profit_target"`
	CloseConditionLossLimit    *float64 `json:"close_condition_loss_limit"`
}

// PauseOpeningRequest 暂停开仓请求
type PauseOpeningRequest struct {
	Reason        string `json:"reason"`
	AutoResumeSec *int   `json:"auto_resume_sec"` // 自动恢复时间（秒），0=不自动恢复
}

func validateBotRiskControlRequest(req *BotRiskControlRequest) error {
	if req.MaxPositionQuantity != nil && !validNonNegativeFinite(*req.MaxPositionQuantity) {
		return fmt.Errorf("max_position_quantity must be a finite number >= 0")
	}
	if req.MaxPositionValue != nil && !validNonNegativeFinite(*req.MaxPositionValue) {
		return fmt.Errorf("max_position_value must be a finite number >= 0")
	}
	if req.MaxPositionLayers != nil && *req.MaxPositionLayers < 0 {
		return fmt.Errorf("max_position_layers must be >= 0")
	}
	if req.MaxOpenOrders != nil && *req.MaxOpenOrders < 0 {
		return fmt.Errorf("max_open_orders must be >= 0")
	}
	if req.OpenOrderDistance != nil && !validNonNegativeFinite(*req.OpenOrderDistance) {
		return fmt.Errorf("open_order_distance must be a finite number >= 0")
	}
	if req.StopLossRatio != nil && !validRatio(*req.StopLossRatio) {
		return fmt.Errorf("stop_loss_ratio must be between 0 and 1")
	}
	if req.TakeProfitRatio != nil && !validRatio(*req.TakeProfitRatio) {
		return fmt.Errorf("take_profit_ratio must be between 0 and 1")
	}
	if req.TrailingStopRatio != nil && !validRatio(*req.TrailingStopRatio) {
		return fmt.Errorf("trailing_stop_ratio must be between 0 and 1")
	}
	if req.GridRiskControl != nil {
		if err := validateGridRiskControlPatch(*req.GridRiskControl); err != nil {
			return err
		}
	}
	return nil
}

func validateGridRiskControlPatch(grc GridRiskControlPatch) error {
	if grc.MaxGridLayers != nil && *grc.MaxGridLayers < 0 {
		return fmt.Errorf("grid_risk_control.max_grid_layers must be >= 0")
	}
	if grc.MaxOpenOrdersAtCap != nil && *grc.MaxOpenOrdersAtCap < 0 {
		return fmt.Errorf("grid_risk_control.max_open_orders_at_cap must be >= 0")
	}
	if grc.StopLossRatio != nil && !validRatio(*grc.StopLossRatio) {
		return fmt.Errorf("grid_risk_control.stop_loss_ratio must be between 0 and 1")
	}
	if grc.TakeProfitTriggerRatio != nil && !validRatio(*grc.TakeProfitTriggerRatio) {
		return fmt.Errorf("grid_risk_control.take_profit_trigger_ratio must be between 0 and 1")
	}
	if grc.TrailingTakeProfitRatio != nil && !validRatio(*grc.TrailingTakeProfitRatio) {
		return fmt.Errorf("grid_risk_control.trailing_take_profit_ratio must be between 0 and 1")
	}
	if grc.CloseConditionProfitTarget != nil && !validRatio(*grc.CloseConditionProfitTarget) {
		return fmt.Errorf("grid_risk_control.close_condition_profit_target must be between 0 and 1")
	}
	if grc.CloseConditionLossLimit != nil && !validRatio(*grc.CloseConditionLossLimit) {
		return fmt.Errorf("grid_risk_control.close_condition_loss_limit must be between 0 and 1")
	}
	return nil
}

func applyGridRiskControlPatch(dst config.GridRiskControl, patch *GridRiskControlPatch) config.GridRiskControl {
	if patch == nil {
		return dst
	}
	if patch.Enabled != nil {
		dst.Enabled = *patch.Enabled
	}
	if patch.MaxGridLayers != nil {
		dst.MaxGridLayers = *patch.MaxGridLayers
	}
	if patch.MaxOpenOrdersAtCap != nil {
		dst.MaxOpenOrdersAtCap = *patch.MaxOpenOrdersAtCap
	}
	if patch.StopLossRatio != nil {
		dst.StopLossRatio = *patch.StopLossRatio
	}
	if patch.TakeProfitTriggerRatio != nil {
		dst.TakeProfitTriggerRatio = *patch.TakeProfitTriggerRatio
	}
	if patch.TrailingTakeProfitRatio != nil {
		dst.TrailingTakeProfitRatio = *patch.TrailingTakeProfitRatio
	}
	if patch.TrendFilterEnabled != nil {
		dst.TrendFilterEnabled = *patch.TrendFilterEnabled
	}
	if patch.CloseConditionEnabled != nil {
		dst.CloseConditionEnabled = *patch.CloseConditionEnabled
	}
	if patch.CloseConditionProfitTarget != nil {
		dst.CloseConditionProfitTarget = *patch.CloseConditionProfitTarget
	}
	if patch.CloseConditionLossLimit != nil {
		dst.CloseConditionLossLimit = *patch.CloseConditionLossLimit
	}
	return dst
}

func validNonNegativeFinite(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0
}

func validRatio(v float64) bool {
	return validNonNegativeFinite(v) && v <= 1
}

// mergeBotRiskControlRequest 將請求中非空字段合入 dst（dst 非 nil）
func mergeBotRiskControlRequest(dst *config.BotRiskControl, req *BotRiskControlRequest) {
	if req.Enabled != nil {
		dst.Enabled = *req.Enabled
	}
	if req.MaxPositionQuantity != nil {
		dst.MaxPositionQuantity = *req.MaxPositionQuantity
	}
	if req.MaxPositionValue != nil {
		dst.MaxPositionValue = *req.MaxPositionValue
	}
	if req.MaxPositionLayers != nil {
		dst.MaxPositionLayers = *req.MaxPositionLayers
	}
	if req.MaxOpenOrders != nil {
		dst.MaxOpenOrders = *req.MaxOpenOrders
	}
	if req.OpenOrderDistance != nil {
		dst.OpenOrderDistance = *req.OpenOrderDistance
	}
	if req.StopLossRatio != nil {
		dst.StopLossRatio = *req.StopLossRatio
	}
	if req.TakeProfitRatio != nil {
		dst.TakeProfitRatio = *req.TakeProfitRatio
	}
	if req.TrailingStopRatio != nil {
		dst.TrailingStopRatio = *req.TrailingStopRatio
	}
	if req.TrendFilterEnabled != nil {
		dst.TrendFilterEnabled = *req.TrendFilterEnabled
	}
}

// getBotRiskControl 获取 Bot 风控配置（运行中从运行时取，已停止从配置取）
// 返回包含 open_position_control 與 grid_risk_control 的合併結構
func getBotRiskControl(c *gin.Context) {
	botID := c.Param("id")
	bot, ok := botExtendedProvider.GetBot(botID)
	if ok {
		rc := bot.GetBotRiskControl()
		if rc == nil {
			rc = &config.BotRiskControl{}
		}
		grc := bot.GetGridRiskControl()
		c.JSON(http.StatusOK, gin.H{
			"enabled":               rc.Enabled,
			"max_position_quantity": rc.MaxPositionQuantity,
			"max_position_value":    rc.MaxPositionValue,
			"max_position_layers":   rc.MaxPositionLayers,
			"max_open_orders":       rc.MaxOpenOrders,
			"open_order_distance":   rc.OpenOrderDistance,
			"stop_loss_ratio":       rc.StopLossRatio,
			"take_profit_ratio":     rc.TakeProfitRatio,
			"trailing_stop_ratio":   rc.TrailingStopRatio,
			"trend_filter_enabled":  rc.TrendFilterEnabled,
			"pause_opening":         rc.PauseOpening,
			"pause_opening_reason":  rc.PauseOpeningReason,
			"auto_resume_after":     rc.AutoResumeAfter,
			"grid_risk_control":     grc,
		})
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
			grc := cfg.Bots[i].GridRiskControl
			c.JSON(http.StatusOK, gin.H{
				"enabled":               rc.Enabled,
				"max_position_quantity": rc.MaxPositionQuantity,
				"max_position_value":    rc.MaxPositionValue,
				"max_position_layers":   rc.MaxPositionLayers,
				"max_open_orders":       rc.MaxOpenOrders,
				"open_order_distance":   rc.OpenOrderDistance,
				"stop_loss_ratio":       rc.StopLossRatio,
				"take_profit_ratio":     rc.TakeProfitRatio,
				"trailing_stop_ratio":   rc.TrailingStopRatio,
				"trend_filter_enabled":  rc.TrendFilterEnabled,
				"pause_opening":         rc.PauseOpening,
				"pause_opening_reason":  rc.PauseOpeningReason,
				"auto_resume_after":     rc.AutoResumeAfter,
				"grid_risk_control":     grc,
			})
			return
		}
	}
	c.JSON(http.StatusNotFound, gin.H{"error": "Bot not found"})
}

// updateBotRiskControl 更新 Bot 风控配置
func updateBotRiskControl(c *gin.Context) {
	botID := c.Param("id")

	var req BotRiskControlRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := validateBotRiskControlRequest(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if botExtendedProvider == nil {
		updateBotRiskControlWhenStopped(c, botID, &req)
		return
	}
	bot, ok := botExtendedProvider.GetBot(botID)
	if !ok {
		updateBotRiskControlWhenStopped(c, botID, &req)
		return
	}

	// 获取当前风控配置
	currentRiskControl := bot.GetBotRiskControl()
	if currentRiskControl == nil {
		currentRiskControl = &config.BotRiskControl{}
	}
	mergeBotRiskControlRequest(currentRiskControl, &req)

	// 应用更新后的配置
	if err := bot.SetBotRiskControl(currentRiskControl); err != nil {
		logger.Error("❌ [%s] 更新风控配置失败: %v", botID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if err := persistBotRiskControlToConfig(botID, *currentRiskControl); err != nil {
		logger.Warn("⚠️ [%s] Bot 風控持久化失敗（運行時已更新）: %v", botID, err)
	}

	// 更新網格風控（若請求中包含）
	if req.GridRiskControl != nil {
		nextGridRiskControl := applyGridRiskControlPatch(bot.GetGridRiskControl(), req.GridRiskControl)
		if err := bot.SetGridRiskControl(nextGridRiskControl); err != nil {
			logger.Error("❌ [%s] 更新網格風控失敗: %v", botID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		// 持久化到配置文件
		if err := persistGridRiskControlToConfig(botID, nextGridRiskControl); err != nil {
			logger.Warn("⚠️ [%s] 網格風控持久化失敗（運行時已更新）: %v", botID, err)
		}
	}

	logger.Info("✅ [%s] 风控配置已更新: %+v", botID, currentRiskControl)
	resp := gin.H{
		"enabled":               currentRiskControl.Enabled,
		"max_position_quantity": currentRiskControl.MaxPositionQuantity,
		"max_position_value":    currentRiskControl.MaxPositionValue,
		"max_position_layers":   currentRiskControl.MaxPositionLayers,
		"max_open_orders":       currentRiskControl.MaxOpenOrders,
		"open_order_distance":   currentRiskControl.OpenOrderDistance,
		"stop_loss_ratio":       currentRiskControl.StopLossRatio,
		"take_profit_ratio":     currentRiskControl.TakeProfitRatio,
		"trailing_stop_ratio":   currentRiskControl.TrailingStopRatio,
		"trend_filter_enabled":  currentRiskControl.TrendFilterEnabled,
		"grid_risk_control":     bot.GetGridRiskControl(),
		"persisted":             true,
	}
	c.JSON(http.StatusOK, resp)
}

// updateBotRiskControlWhenStopped Bot 未運行時：與 GET 一致從主庫/快照載入配置並寫回（避免誤回 404）
func updateBotRiskControlWhenStopped(c *gin.Context, botID string, req *BotRiskControlRequest) {
	bcf, err := resolveBotConfigFileFromUnifiedOrMain(botID)
	if err != nil {
		logger.Error("載入 Bot 配置失敗 [%s]: %v", botID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "config_load_failed"})
		return
	}
	if bcf == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Bot not found"})
		return
	}

	rc := bcf.RiskControl.OpenPositionControl.BotRiskControl
	if rc == nil {
		rc = &config.BotRiskControl{}
	}
	mergeBotRiskControlRequest(rc, req)
	bcf.RiskControl.OpenPositionControl.BotRiskControl = rc
	if req.GridRiskControl != nil {
		bcf.RiskControl.GridRiskControl = applyGridRiskControlPatch(bcf.RiskControl.GridRiskControl, req.GridRiskControl)
	}
	bcf.UpdatedAt = time.Now().Format(time.RFC3339)

	if botConfigStorageReady() {
		if err := saveBotConfigUnified(bcf, "web", "put_bot_risk_control_stopped"); err != nil {
			logger.Error("❌ [%s] 保存風控到 bot_configs 失敗: %v", botID, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}
	if fileConfigManager != nil {
		if err := persistBotRiskControlToConfig(botID, *rc); err != nil {
			logger.Warn("⚠️ [%s] 主配置風控同步失敗: %v", botID, err)
		}
		if req.GridRiskControl != nil {
			if err := persistGridRiskControlToConfig(botID, bcf.RiskControl.GridRiskControl); err != nil {
				logger.Warn("⚠️ [%s] 主配置網格風控同步失敗: %v", botID, err)
			}
		}
	}

	logger.Info("✅ [%s] 風控配置已更新（Bot 未運行）: %+v", botID, rc)
	c.JSON(http.StatusOK, gin.H{
		"enabled":               rc.Enabled,
		"max_position_quantity": rc.MaxPositionQuantity,
		"max_position_value":    rc.MaxPositionValue,
		"max_position_layers":   rc.MaxPositionLayers,
		"max_open_orders":       rc.MaxOpenOrders,
		"open_order_distance":   rc.OpenOrderDistance,
		"stop_loss_ratio":       rc.StopLossRatio,
		"take_profit_ratio":     rc.TakeProfitRatio,
		"trailing_stop_ratio":   rc.TrailingStopRatio,
		"trend_filter_enabled":  rc.TrendFilterEnabled,
		"grid_risk_control":     bcf.RiskControl.GridRiskControl,
		"persisted":             true,
	})
}

// persistBotRiskControlToConfig 將 Bot 風控寫入主配置文件
func persistBotRiskControlToConfig(botID string, rc config.BotRiskControl) error {
	if fileConfigManager == nil {
		return nil
	}
	cfg, err := GetLatestConfig()
	if err != nil || cfg == nil {
		return err
	}
	for i := range cfg.Bots {
		id := cfg.Bots[i].ID
		if id == "" {
			id = config.GenerateBotID(cfg.Bots[i].Exchange, cfg.Bots[i].Symbol, cfg.Bots[i].GetMarketType())
		}
		if id == botID {
			if cfg.Bots[i].OpenPositionControl.BotRiskControl == nil {
				cfg.Bots[i].OpenPositionControl.BotRiskControl = &config.BotRiskControl{}
			}
			*cfg.Bots[i].OpenPositionControl.BotRiskControl = rc
			if err := fileConfigManager.UpdateConfigWithBotHistorySource(cfg, "put_bot_risk_control_running"); err != nil {
				return err
			}
			return nil
		}
	}
	return nil
}

// persistGridRiskControlToConfig 將網格風控寫入主配置文件
func persistGridRiskControlToConfig(botID string, grc config.GridRiskControl) error {
	if fileConfigManager == nil {
		return nil
	}
	cfg, err := GetLatestConfig()
	if err != nil || cfg == nil {
		return err
	}
	for i := range cfg.Bots {
		id := cfg.Bots[i].ID
		if id == "" {
			id = config.GenerateBotID(cfg.Bots[i].Exchange, cfg.Bots[i].Symbol, cfg.Bots[i].GetMarketType())
		}
		if id == botID {
			cfg.Bots[i].GridRiskControl = grc
			if err := fileConfigManager.UpdateConfigWithBotHistorySource(cfg, "put_bot_risk_control_running"); err != nil {
				return err
			}
			return nil
		}
	}
	return nil
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
		} else if riskControl != nil {
			if err := persistBotRiskControlToConfig(botID, *riskControl); err != nil {
				logger.Warn("⚠️ [%s] 保存自动恢复时间到配置失败: %v", botID, err)
			}
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
				"stopped":              true,
				"total_position_qty":   0,
				"total_position_value": 0,
				"position_layers":      0,
				"current_price":        0,
				"paused":               false,
				"reached_limit_qty":    false,
				"reached_limit_value":  false,
				"reached_limit_layers": false,
				"should_stop_opening":  false,
			})
			return
		}
	}
	c.JSON(http.StatusNotFound, gin.H{"error": "Bot not found"})
}

// BotRiskControlEventJSON Bot 風控事件 API 響應項
type BotRiskControlEventJSON struct {
	ID        int64  `json:"id"`
	EventType string `json:"event_type"`
	Reason    string `json:"reason"`
	Source    string `json:"source"`
	CreatedAt string `json:"created_at"`
}

// getBotRiskControlEvents 分頁查詢 Bot 開倉風控暫停/恢復事件
// GET /api/v2/bots/:id/risk-control/events?page=1&page_size=20
func getBotRiskControlEvents(c *gin.Context) {
	botID := c.Param("id")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 500 {
		pageSize = 20
	}

	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusOK, gin.H{
			"events":     []BotRiskControlEventJSON{},
			"total":      int64(0),
			"page":       page,
			"page_size":  pageSize,
			"total_page": int64(0),
		})
		return
	}
	st := storageProv.GetStorage()
	if st == nil {
		c.JSON(http.StatusOK, gin.H{
			"events":     []BotRiskControlEventJSON{},
			"total":      int64(0),
			"page":       page,
			"page_size":  pageSize,
			"total_page": int64(0),
		})
		return
	}

	total, err := st.CountBotRiskControlEvents(botID)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "error.storage_query")
		return
	}
	offset := (page - 1) * pageSize
	records, err := st.QueryBotRiskControlEvents(botID, pageSize, offset)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "error.storage_query")
		return
	}

	out := make([]BotRiskControlEventJSON, 0, len(records))
	for _, r := range records {
		if r == nil {
			continue
		}
		out = append(out, BotRiskControlEventJSON{
			ID:        r.ID,
			EventType: r.EventType,
			Reason:    r.Reason,
			Source:    r.Source,
			CreatedAt: r.CreatedAt.UTC().Format(time.RFC3339),
		})
	}

	var totalPage int64
	if total > 0 {
		totalPage = (total + int64(pageSize) - 1) / int64(pageSize)
	}

	c.JSON(http.StatusOK, gin.H{
		"events":     out,
		"total":      total,
		"page":       page,
		"page_size":  pageSize,
		"total_page": totalPage,
	})
}

// exportBotRiskControlEvents 導出 Bot 風控事件為 CSV（UTF-8 BOM）
// GET /api/v2/bots/:id/risk-control/events/export
func exportBotRiskControlEvents(c *gin.Context) {
	botID := c.Param("id")
	limit := 50000
	if ls := strings.TrimSpace(c.Query("limit")); ls != "" {
		if n, err := strconv.Atoi(ls); err == nil && n > 0 && n <= 100000 {
			limit = n
		}
	}

	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "storage_unavailable"})
		return
	}
	st := storageProv.GetStorage()
	if st == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "storage_unavailable"})
		return
	}

	records, err := st.QueryBotRiskControlEvents(botID, limit, 0)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "error.storage_query")
		return
	}

	var buf bytes.Buffer
	buf.WriteString("\xef\xbb\xbf")
	w := csv.NewWriter(&buf)
	_ = w.Write([]string{"id", "event_type", "reason", "source", "created_at"})
	for _, r := range records {
		if r == nil {
			continue
		}
		_ = w.Write([]string{
			fmt.Sprintf("%d", r.ID),
			r.EventType,
			r.Reason,
			r.Source,
			r.CreatedAt.UTC().Format(time.RFC3339),
		})
	}
	w.Flush()
	if err := w.Error(); err != nil {
		respondError(c, http.StatusInternalServerError, "error.export_csv")
		return
	}

	safeName := strings.Map(func(r rune) rune {
		if r <= 32 || strings.ContainsRune(`\/:*?"<>|`, r) {
			return '_'
		}
		return r
	}, botID)
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="bot_risk_events_%s.csv"`, safeName))
	c.Data(http.StatusOK, "text/csv; charset=utf-8", buf.Bytes())
}
