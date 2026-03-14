package web

import (
	"net/http"
	"strings"

	"quantmesh/config"
	"quantmesh/option"

	"github.com/gin-gonic/gin"
)

var optionStore = option.NewStore()

// getOptionHedgeStatus 获取期权对冲状态
// GET /api/bots/:id/option-hedge/status
func getOptionHedgeStatus(c *gin.Context) {
	botID := c.Param("id")
	if botID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bot_id required"})
		return
	}

	bcf, err := botConfigManager.LoadBotConfig(botID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "bot not found"})
		return
	}

	cfg := getOptionHedgeConfigFromBot(bcf)
	if cfg == nil || !cfg.Enabled {
		c.JSON(http.StatusOK, gin.H{
			"bot_id":   botID,
			"enabled":  false,
			"message":  "option_hedge_disabled",
		})
		return
	}

	hedgeType := getOptionHedgeType(bcf, cfg)

	st := optionStore.GetState(botID)
	resp := option.OptionHedgeStatus{
		BotID:     botID,
		Enabled:   true,
		HedgeType: hedgeType,
	}
	if st != nil {
		resp.Positions = st.Positions
		resp.Coverage = st.Coverage
		if st.Coverage != nil && st.Coverage.HedgeType != "" {
			resp.HedgeType = st.Coverage.HedgeType
		}
		resp.SyncStatus = st.SyncStatus
		resp.LastSyncAt = st.LastSyncAt
		if st.LastError != "" {
			resp.Alerts = append(resp.Alerts, st.LastError)
		}
		if st.Coverage != nil {
			if st.Coverage.BelowMinCoverage {
				resp.Alerts = append(resp.Alerts, "below_min_coverage")
			}
			if st.Coverage.DTEWarning {
				resp.Alerts = append(resp.Alerts, "dte_warning")
			}
		}
	}
	c.JSON(http.StatusOK, resp)
}

// postOptionHedgeSync 同步期权仓位
// POST /api/bots/:id/option-hedge/sync
func postOptionHedgeSync(c *gin.Context) {
	botID := c.Param("id")
	if botID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bot_id required"})
		return
	}

	bcf, err := botConfigManager.LoadBotConfig(botID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "bot not found"})
		return
	}

	cfg := getOptionHedgeConfigFromBot(bcf)
	if cfg == nil || !cfg.Enabled {
		c.JSON(http.StatusBadRequest, gin.H{"error": "option_hedge_disabled"})
		return
	}

	adapter := getOptionAdapter(cfg, bcf.Exchange, bcf.Testnet)
	if adapter == nil {
		optionStore.UpsertPositions(botID, nil, "failed", "adapter_not_available")
		c.JSON(http.StatusInternalServerError, gin.H{"error": "exchange_not_supported"})
		return
	}

	direction := getOptionHedgeDirection(bcf, cfg)
	hedgeType := getOptionHedgeType(bcf, cfg)

	positions, err := adapter.FetchPositions(c.Request.Context(), option.FetchParams{
		Symbol:    bcf.Symbol,
		Direction: direction,
	})
	syncStatus := "ok"
	errMsg := ""
	if err != nil {
		syncStatus = "failed"
		errMsg = err.Error()
	}
	optionStore.UpsertPositions(botID, positions, syncStatus, errMsg)

	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"bot_id":      botID,
			"sync_status": syncStatus,
			"error":       errMsg,
			"positions":   positions,
		})
		return
	}

	// 计算覆盖率（需要网格持仓）
	eng := option.NewEngine(option.OptionHedgeConfig{
		Enabled:             cfg.Enabled,
		TargetCoverageRatio: cfg.TargetCoverageRatio,
		MinCoverageRatio:    cfg.MinCoverageRatio,
		DTEWarningDays:      cfg.DTEWarningDays,
	})
	gridNotional, gridQty := getGridPositionForBot(botID)
	cov := eng.ComputeCoverage(botID, gridNotional, gridQty, positions, hedgeType)
	optionStore.SetCoverage(botID, cov)

	c.JSON(http.StatusOK, gin.H{
		"bot_id":      botID,
		"sync_status": syncStatus,
		"hedge_type":  hedgeType,
		"positions":   positions,
		"coverage":    cov,
	})
}

// getOptionHedgeRollSuggestions 获取展期建议
// GET /api/bots/:id/option-hedge/roll-suggestions
func getOptionHedgeRollSuggestions(c *gin.Context) {
	botID := c.Param("id")
	if botID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bot_id required"})
		return
	}

	st := optionStore.GetState(botID)
	if st == nil || st.Coverage == nil {
		c.JSON(http.StatusOK, gin.H{"suggestions": []option.RollSuggestion{}})
		return
	}

	bcf, _ := botConfigManager.LoadBotConfig(botID)
	cfg := getOptionHedgeConfigFromBot(bcf)
	if cfg == nil {
		cfg = &config.OptionHedgeConfig{TargetCoverageRatio: 0.25, MinCoverageRatio: 0.15, DTEWarningDays: 7}
	}
	eng := option.NewEngine(option.OptionHedgeConfig{
		Enabled:             cfg.Enabled,
		TargetCoverageRatio: cfg.TargetCoverageRatio,
		MinCoverageRatio:    cfg.MinCoverageRatio,
		DTEWarningDays:      cfg.DTEWarningDays,
	})

	currentPrice := 0.0
	gridQtyAbs := st.Coverage.GridPositionQty
	if gridQtyAbs < 0 {
		gridQtyAbs = -gridQtyAbs
	}
	if st.Coverage.GridNotional > 0 && gridQtyAbs > 0 {
		currentPrice = st.Coverage.GridNotional / gridQtyAbs
	}
	suggestions := eng.SuggestRolls(st.Coverage, currentPrice)
	c.JSON(http.StatusOK, gin.H{"suggestions": suggestions})
}

// postOptionHedgeExecuteRoll 执行展期（记录审计日志，实际下单由用户在其他平台完成）
// POST /api/bots/:id/option-hedge/execute-roll
func postOptionHedgeExecuteRoll(c *gin.Context) {
	botID := c.Param("id")
	if botID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "bot_id required"})
		return
	}

	var body struct {
		FromInstrument string `json:"from_instrument"`
		ToInstrument   string `json:"to_instrument"`
		Action         string `json:"action"` // roll_executed / roll_skipped
		Details        string `json:"details"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_body"})
		return
	}
	if body.Action == "" {
		body.Action = "roll_executed"
	}

	optionStore.AppendRollLog(botID, option.RollLogEntry{
		BotID:    botID,
		Action:   body.Action,
		FromInst: body.FromInstrument,
		ToInst:   body.ToInstrument,
		Details:  body.Details,
	})

	c.JSON(http.StatusOK, gin.H{
		"bot_id":   botID,
		"action":   body.Action,
		"recorded": true,
	})
}

func getOptionHedgeConfigFromBot(bcf *config.BotConfigFile) *config.OptionHedgeConfig {
	if bcf == nil || bcf.RiskControl.OptionHedge == nil {
		return nil
	}
	return bcf.RiskControl.OptionHedge
}

// getOptionHedgeDirection 从 option_hedge.direction 或 grid.direction 推断对冲方向
func getOptionHedgeDirection(bcf *config.BotConfigFile, cfg *config.OptionHedgeConfig) string {
	if cfg != nil && strings.ToUpper(cfg.Direction) == "SHORT" {
		return "SHORT"
	}
	if bcf != nil && bcf.Grid.Direction != "" && strings.ToUpper(bcf.Grid.Direction) == "SHORT" {
		return "SHORT"
	}
	return "LONG"
}

// getOptionHedgeType 返回 hedge_type：LONG 网格用 PUT，SHORT 网格用 CALL
func getOptionHedgeType(bcf *config.BotConfigFile, cfg *config.OptionHedgeConfig) string {
	if getOptionHedgeDirection(bcf, cfg) == "SHORT" {
		return "CALL"
	}
	return "PUT"
}

func getOptionAdapter(cfg *config.OptionHedgeConfig, exchange string, testnet bool) option.Adapter {
	if cfg == nil {
		return nil
	}
	ex := cfg.Exchange
	if ex == "" {
		ex = exchange
	}
	apiKey, secretKey := getExchangeCredentials(ex)
	if apiKey == "" || secretKey == "" {
		return nil
	}
	switch ex {
	case "deribit":
		return option.NewDeribitAdapter(apiKey, secretKey, testnet)
	case "binance":
		return option.NewBinanceAdapter(apiKey, secretKey, testnet)
	default:
		return nil
	}
}

func getExchangeCredentials(exchange string) (apiKey, secretKey string) {
	cfg := GetConfig()
	if cfg == nil || cfg.Exchanges == nil {
		return "", ""
	}
	ec, ok := cfg.Exchanges[exchange]
	if !ok {
		return "", ""
	}
	return ec.APIKey, ec.SecretKey
}

func getGridPositionForBot(botID string) (notional, qty float64) {
	bot, ok := botExtendedProvider.GetBot(botID)
	if !ok {
		return 0, 0
	}
	m := bot.GetPositionStatus()
	if m == nil {
		return 0, 0
	}
	if v, ok := m["total_position_qty"]; ok {
		if f, ok := v.(float64); ok {
			qty = f
		}
	}
	if v, ok := m["total_position_value"]; ok {
		if f, ok := v.(float64); ok {
			notional = f
		}
	}
	if notional == 0 && qty > 0 {
		if v, ok := m["current_price"]; ok {
			if f, ok := v.(float64); ok && f > 0 {
				notional = qty * f
			}
		}
	}
	return notional, qty
}
