package web

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/gin-gonic/gin"
	"quantmesh/config"
	"quantmesh/logger"
)

// BotResponse Bot 列表項
type BotResponse struct {
	BotID                  string  `json:"bot_id"`
	Name                   string  `json:"name"`
	Exchange               string  `json:"exchange"`
	Symbol                 string  `json:"symbol"`
	MarketType             string  `json:"market_type"`
	Running                bool    `json:"running"`
	CurrentPrice           float64 `json:"current_price,omitempty"`
	TotalPnL               float64 `json:"total_pnl,omitempty"`
	TotalTrades            int     `json:"total_trades,omitempty"`
	RiskTriggered          bool    `json:"risk_triggered,omitempty"`
	Uptime                 int64   `json:"uptime,omitempty"`
	PriceInterval          float64 `json:"price_interval,omitempty"`           // 價格間隔
	ProfitSpread           float64 `json:"profit_spread,omitempty"`             // 利潤間距
	OrderQuantity          float64 `json:"order_quantity,omitempty"`            // 每單金額
	TotalAllocatedCapital  float64 `json:"total_allocated_capital,omitempty"`    // 總投入資金
	Strategies             []BotStrategyInfo `json:"strategies,omitempty"`       // 該 Bot 配置的策略列表
	Leverage               float64 `json:"leverage,omitempty"`                 // 杠杆倍數
	MaxCapitalRatio        float64 `json:"max_capital_ratio,omitempty"`        // 最大資金占用比例 (0.1-1.0)
	BuyWindowSize          int     `json:"buy_window_size,omitempty"`          // 買窗大小（用於計算平倉價）
}

// BotStrategyInfo Bot 策略信息（用于列表显示）
type BotStrategyInfo struct {
	Type   string  `json:"type"`   // 策略类型，如 grid, dca, martingale
	Weight float64 `json:"weight"` // 策略权重（资金分配比例）
	Name   string  `json:"name"`   // 策略显示名称
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
	EnableBot(botID string) error  // 啟用 Bot（從數據庫移除禁用標記）
}

var botManagerProvider BotManagerProvider

// findGroupNameByBotID 若 botID 屬於某個 BotGroup，返回該組名稱；否則返回空字串
func findGroupNameByBotID(cfg *config.Config, botID string) string {
	if cfg == nil || cfg.BotGroups == nil {
		return ""
	}
	for _, g := range cfg.BotGroups {
		for _, id := range g.BotIDs {
			if id == botID {
				return g.Name
			}
		}
	}
	return ""
}

// RegisterBotManagerProvider 註冊 Bot 管理提供者
func RegisterBotManagerProvider(provider BotManagerProvider) {
	botManagerProvider = provider
}

// CreateBotRequest Bot 創建請求（含策略配置）
type CreateBotRequest struct {
	Name                  string                     `json:"name"`
	Exchange              string                     `json:"exchange"`
	Symbol                string                     `json:"symbol"`
	MarketType            string                     `json:"market_type"`
	Testnet               bool                       `json:"testnet"`
	Strategies            []config.StrategyInstance   `json:"strategies"`
	TotalAllocatedCapital float64                    `json:"total_allocated_capital"`
	PriceInterval         float64                    `json:"price_interval"`
	ProfitSpread          float64                    `json:"profit_spread"`
	OrderQuantity         float64                    `json:"order_quantity"`
	MinOrderValue         float64                    `json:"min_order_value"`
	BuyWindowSize         int                        `json:"buy_window_size"`
	SellWindowSize        int                        `json:"sell_window_size"`
	ReconcileInterval     int                        `json:"reconcile_interval"`
	OrderCleanupThreshold int                        `json:"order_cleanup_threshold"`
	CleanupBatchSize      int                        `json:"cleanup_batch_size"`
	MarginLockDurationSec int                        `json:"margin_lock_duration_seconds"`
	PositionSafetyCheck   int                        `json:"position_safety_check"`
	Direction             string                     `json:"direction"`
	PriceLow              float64                    `json:"price_low"`
	PriceHigh             float64                    `json:"price_high"`
	TriggerPrice          float64                    `json:"trigger_price"`
	GridMode              string                     `json:"grid_mode"`
	GridShiftEnabled      bool                       `json:"grid_shift_enabled"`
	GridShiftStep         float64                    `json:"grid_shift_step"`
	CloseOnStop           bool                       `json:"close_on_stop"`
	// 網格風控配置（創建時填寫，建好後在 Bot 詳情可見）
	GridRiskControlEnabled          bool    `json:"grid_risk_control_enabled"`
	GridRiskControlStopLossRatio    float64 `json:"grid_risk_control_stop_loss_ratio"`
	GridRiskControlTakeProfitRatio  float64 `json:"grid_risk_control_take_profit_trigger_ratio"`
	GridRiskControlTrailingRatio    float64 `json:"grid_risk_control_trailing_take_profit_ratio"`
	GridRiskControlTrendFilter      bool    `json:"grid_risk_control_trend_filter_enabled"`
	GridRiskControlMaxGridLayers    int     `json:"grid_risk_control_max_grid_layers"`
	GridRiskControlMaxOpenOrdersCap int     `json:"grid_risk_control_max_open_orders_at_cap"`
}

// buildGridRiskControlFromRequest 從創建請求構建 GridRiskControl
func buildGridRiskControlFromRequest(req CreateBotRequest) config.GridRiskControl {
	grc := config.GridRiskControl{
		Enabled:                 req.GridRiskControlEnabled,
		StopLossRatio:           req.GridRiskControlStopLossRatio,
		TakeProfitTriggerRatio:  req.GridRiskControlTakeProfitRatio,
		TrailingTakeProfitRatio: req.GridRiskControlTrailingRatio,
		TrendFilterEnabled:      req.GridRiskControlTrendFilter,
		MaxGridLayers:           req.GridRiskControlMaxGridLayers,
		MaxOpenOrdersAtCap:      req.GridRiskControlMaxOpenOrdersCap,
	}
	return grc
}

// postBotCreate 創建 Bot
// POST /api/bots/create
func postBotCreate(c *gin.Context) {
	if configManager == nil {
		respondError(c, http.StatusServiceUnavailable, "error.config_manager_unavailable")
		return
	}
	var req CreateBotRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "error.invalid_request", err)
		return
	}
	if req.Exchange == "" || req.Symbol == "" {
		respondError(c, http.StatusBadRequest, "error.invalid_bot_config")
		return
	}
	mt := req.MarketType
	if mt == "" {
		mt = "futures"
	}
	if mt != "spot" && mt != "futures" {
		respondError(c, http.StatusBadRequest, "error.invalid_market_type")
		return
	}

	cfg, err := GetLatestConfig()
	if err != nil || cfg == nil {
		respondError(c, http.StatusInternalServerError, "error.config_load_failed")
		return
	}

	// 1. 檢查對沖組占用：若該交易對已被對沖組占用，拒絕
	symbolKey := config.GenerateBotID(req.Exchange, req.Symbol, mt)
	for _, b := range cfg.Bots {
		id := b.ID
		if id == "" {
			id = config.GenerateBotID(b.Exchange, b.Symbol, b.GetMarketType())
		}
		existingKey := config.GenerateBotID(b.Exchange, b.Symbol, b.GetMarketType())
		if existingKey == symbolKey {
			if groupName := findGroupNameByBotID(cfg, id); groupName != "" {
				logger.Warn("⚠️ [Bot創建] 衝突：%s 已被對沖組「%s」占用", symbolKey, groupName)
				c.JSON(http.StatusConflict, gin.H{
					"error":      "symbol_used_by_hedge_group",
					"error_key":  "error.bot_symbol_used_by_hedge_group",
					"group_name": groupName,
				})
				return
			}
		}
	}

	// 2. 檢查運行中衝突：若同一交易對已有 Bot 在運行，拒絕（同一交易對只能有一個運行）
	if botManagerProvider != nil {
		for _, resp := range botManagerProvider.ListBots() {
			if resp.Running &&
				strings.EqualFold(resp.Exchange, req.Exchange) &&
				strings.EqualFold(resp.Symbol, req.Symbol) &&
				strings.EqualFold(resp.MarketType, mt) {
				logger.Warn("⚠️ [Bot創建] 衝突：%s 已有 Bot [%s] 在運行，請先停止或刪除", symbolKey, resp.BotID)
				c.JSON(http.StatusConflict, gin.H{
					"error":     "symbol_running",
					"error_key": "error.bot_symbol_running",
					"bot_id":    resp.BotID,
				})
				return
			}
		}
	}

	// 3. 使用 UUID 作為新 Bot 的唯一 ID，支持同一交易對多個 Bot 配置（僅一個可運行）
	botID := config.GenerateUniqueBotID()

	// 構建 BotConfig
	name := req.Name
	if name == "" {
		if mt == "spot" {
			name = req.Symbol + " (spot)"
		} else {
			name = req.Symbol + " (futures)"
		}
	}
	bc := config.BotConfig{
		ID:                    botID,
		Name:                  name,
		Exchange:              req.Exchange,
		Symbol:                req.Symbol,
		MarketType:            mt,
		Testnet:               req.Testnet,
		Enabled:               config.BoolPtr(true),
		Strategies:            req.Strategies,
		TotalAllocatedCapital: req.TotalAllocatedCapital,
		PriceInterval:         req.PriceInterval,
		ProfitSpread:          req.ProfitSpread,
		OrderQuantity:          req.OrderQuantity,
		MinOrderValue:         req.MinOrderValue,
		BuyWindowSize:         req.BuyWindowSize,
		SellWindowSize:        req.SellWindowSize,
		ReconcileInterval:     req.ReconcileInterval,
		OrderCleanupThreshold: req.OrderCleanupThreshold,
		CleanupBatchSize:      req.CleanupBatchSize,
		MarginLockDurationSec: req.MarginLockDurationSec,
		PositionSafetyCheck:   req.PositionSafetyCheck,
		Direction:             req.Direction,
		PriceLow:              req.PriceLow,
		PriceHigh:             req.PriceHigh,
		TriggerPrice:          req.TriggerPrice,
		GridMode:              req.GridMode,
		GridShiftEnabled:      req.GridShiftEnabled,
		GridShiftStep:         req.GridShiftStep,
		CloseOnStop:           req.CloseOnStop,
		GridRiskControl:       buildGridRiskControlFromRequest(req),
	}
	if bc.ReconcileInterval <= 0 {
		bc.ReconcileInterval = 60
	}
	if bc.OrderCleanupThreshold <= 0 {
		bc.OrderCleanupThreshold = 50
	}
	if bc.CleanupBatchSize <= 0 {
		bc.CleanupBatchSize = 10
	}
	if bc.MarginLockDurationSec <= 0 {
		bc.MarginLockDurationSec = 10
	}
	if bc.PositionSafetyCheck <= 0 {
		bc.PositionSafetyCheck = 100
	}
	if bc.Direction == "" {
		bc.Direction = "LONG"
	}
	if bc.MinOrderValue <= 0 {
		bc.MinOrderValue = 20
	}
	// 若無策略，預設 grid
	if len(bc.Strategies) == 0 {
		bc.Strategies = []config.StrategyInstance{{Type: "grid", Weight: 1.0, Config: map[string]interface{}{}}}
	}

	cfg.Bots = append(cfg.Bots, bc)
	if err := fileConfigManager.UpdateConfig(cfg); err != nil {
		respondError(c, http.StatusInternalServerError, "error.config_save_failed", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "bot_id": botID})
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

// postBotStart 啟動 Bot（異步執行，避免長時間阻塞導致請求超時）
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
	// 已在運行則直接返回成功
	if bot, ok := botManagerProvider.GetBot(botID); ok && bot.Running {
		c.JSON(http.StatusOK, gin.H{"ok": true, "bot_id": botID})
		return
	}
	// 異步啟動，避免 WebSocket 連接、價格獲取等耗時操作阻塞請求導致超時
	bc := *botCfg
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Error("❌ [%s] Bot 啟動時發生 panic: %v", botID, r)
			}
		}()
		ctx := context.Background()
		if err := botManagerProvider.StartBot(ctx, bc); err != nil {
			logger.Error("❌ [%s] Bot 異步啟動失敗: %v", botID, err)
		}
	}()
	c.JSON(http.StatusAccepted, gin.H{
		"ok":      true,
		"bot_id":  botID,
		"status":  "starting",
		"message": "Bot 正在啟動，請稍後刷新狀態",
	})
}

// deleteBot 刪除 Bot（若屬於對沖組則禁止）
// DELETE /api/bots/:id
func deleteBot(c *gin.Context) {
	botID := c.Param("id")
	if botID == "" {
		respondError(c, http.StatusBadRequest, "error.invalid_bot_id")
		return
	}
	if configManager == nil {
		respondError(c, http.StatusServiceUnavailable, "error.config_manager_unavailable")
		return
	}
	cfg, err := GetLatestConfig()
	if err != nil || cfg == nil {
		respondError(c, http.StatusInternalServerError, "error.config_load_failed")
		return
	}
	if groupName := findGroupNameByBotID(cfg, botID); groupName != "" {
		logger.Warn("⚠️ [Bot刪除] 拒絕：%s 屬於對沖組「%s」，禁止單獨刪除", botID, groupName)
		c.JSON(http.StatusForbidden, gin.H{
			"error":      "bot_in_hedge_group",
			"error_key":  "error.bot_in_hedge_group_cannot_delete",
			"group_name": groupName,
		})
		return
	}
	var found bool
	var newBots []config.BotConfig
	for _, b := range cfg.Bots {
		id := b.ID
		if id == "" {
			id = config.GenerateBotID(b.Exchange, b.Symbol, b.GetMarketType())
		}
		if id == botID {
			found = true
			continue
		}
		newBots = append(newBots, b)
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "Bot not found"})
		return
	}
	cfg.Bots = newBots
	if err := fileConfigManager.UpdateConfig(cfg); err != nil {
		respondError(c, http.StatusInternalServerError, "error.config_save_failed", err)
		return
	}
	if botManagerProvider != nil {
		_ = botManagerProvider.StopBot(botID)
	}
	logger.Info("✅ [Bot刪除] 已移除 %s", botID)
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

// postBotEnable 啟用 Bot（從數據庫移除禁用標記）
// POST /api/bots/:id/enable
func postBotEnable(c *gin.Context) {
	botID := c.Param("id")
	if botID == "" {
		respondError(c, http.StatusBadRequest, "error.invalid_bot_id")
		return
	}
	if botManagerProvider == nil {
		respondError(c, http.StatusServiceUnavailable, "error.bot_manager_unavailable")
		return
	}
	if err := botManagerProvider.EnableBot(botID); err != nil {
		respondError(c, http.StatusInternalServerError, "error.bot_enable_failed", err)
		return
	}
	logger.Info("✅ [Bot啟用] 已從數據庫移除禁用標記 %s", botID)
	c.JSON(http.StatusOK, gin.H{"ok": true, "bot_id": botID})
}

// CreateBotGroupRequest Bot 組創建請求（對沖模式）
type CreateBotGroupRequest struct {
	Name        string                 `json:"name"`
	Type        string                 `json:"type"` // futures_spot_hedge, long_short_hedge
	HedgeConfig config.HedgeConfig     `json:"hedge_config"`
	FuturesBot  CreateBotRequest       `json:"futures_bot"`
	SpotBot     CreateBotRequest       `json:"spot_bot"`
}

// getBotGroups 獲取 Bot 組列表
// GET /api/bot-groups
func getBotGroups(c *gin.Context) {
	if configManager == nil {
		c.JSON(http.StatusOK, gin.H{"bot_groups": []config.BotGroup{}})
		return
	}
	cfg, err := GetLatestConfig()
	if err != nil || cfg == nil {
		c.JSON(http.StatusOK, gin.H{"bot_groups": []config.BotGroup{}})
		return
	}
	groups := cfg.BotGroups
	if groups == nil {
		groups = []config.BotGroup{}
	}
	c.JSON(http.StatusOK, gin.H{"bot_groups": groups})
}

// getBotGroupByID 獲取 Bot 組詳情
// GET /api/bot-groups/:id
func getBotGroupByID(c *gin.Context) {
	groupID := c.Param("id")
	if groupID == "" {
		respondError(c, http.StatusBadRequest, "error.invalid_group_id")
		return
	}
	if configManager == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Config manager not available"})
		return
	}
	cfg, err := GetLatestConfig()
	if err != nil || cfg == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Config load failed"})
		return
	}
	for _, g := range cfg.BotGroups {
		if g.ID == groupID {
			c.JSON(http.StatusOK, gin.H{"bot_group": g})
			return
		}
	}
	c.JSON(http.StatusNotFound, gin.H{"error": "Bot group not found"})
}

// postBotGroupCreate 創建 Bot 組（原子化創建 futures+spot 兩個 Bot）
// POST /api/bot-groups
func postBotGroupCreate(c *gin.Context) {
	if configManager == nil {
		respondError(c, http.StatusServiceUnavailable, "error.config_manager_unavailable")
		return
	}
	var req CreateBotGroupRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "error.invalid_request", err)
		return
	}
	if req.Type == "" {
		req.Type = "futures_spot_hedge"
	}
	if req.Type != "futures_spot_hedge" && req.Type != "long_short_hedge" {
		respondError(c, http.StatusBadRequest, "error.invalid_group_type")
		return
	}
	if req.FuturesBot.Exchange == "" || req.FuturesBot.Symbol == "" ||
		req.SpotBot.Exchange == "" || req.SpotBot.Symbol == "" {
		respondError(c, http.StatusBadRequest, "error.invalid_bot_config")
		return
	}
	req.FuturesBot.MarketType = "futures"
	req.SpotBot.MarketType = "spot"

	cfg, err := GetLatestConfig()
	if err != nil || cfg == nil {
		respondError(c, http.StatusInternalServerError, "error.config_load_failed")
		return
	}

	futuresID := config.GenerateBotID(req.FuturesBot.Exchange, req.FuturesBot.Symbol, "futures")
	spotID := config.GenerateBotID(req.SpotBot.Exchange, req.SpotBot.Symbol, "spot")
	for _, b := range cfg.Bots {
		id := b.ID
		if id == "" {
			id = config.GenerateBotID(b.Exchange, b.Symbol, b.GetMarketType())
		}
		if id == futuresID || id == spotID {
			if groupName := findGroupNameByBotID(cfg, id); groupName != "" {
				logger.Warn("⚠️ [對沖組創建] 衝突：%s 已被對沖組「%s」占用", id, groupName)
				c.JSON(http.StatusConflict, gin.H{
					"error":      "symbol_used_by_hedge_group",
					"error_key":  "error.bot_symbol_used_by_hedge_group",
					"group_name": groupName,
				})
				return
			}
			logger.Warn("⚠️ [對沖組創建] 衝突：%s 已存在（普通衝突）", id)
			c.JSON(http.StatusConflict, gin.H{"error": "symbol_conflict", "error_key": "error.bot_symbol_conflict"})
			return
		}
	}

	groupID := "bg-" + uuid.New().String()[:8]
	groupName := req.Name
	if groupName == "" {
		groupName = req.FuturesBot.Symbol + " Hedge"
	}

	// 構建兩個 BotConfig
	futuresName := req.FuturesBot.Name
	if futuresName == "" {
		futuresName = req.FuturesBot.Symbol + " (futures)"
	}
	spotName := req.SpotBot.Name
	if spotName == "" {
		spotName = req.SpotBot.Symbol + " (spot)"
	}

	bcFutures := config.BotConfig{
		ID:                    futuresID,
		Name:                  futuresName,
		Exchange:              req.FuturesBot.Exchange,
		Symbol:                req.FuturesBot.Symbol,
		MarketType:            "futures",
		Testnet:               req.FuturesBot.Testnet,
		Enabled:               config.BoolPtr(true),
		Strategies:            req.FuturesBot.Strategies,
		TotalAllocatedCapital: req.FuturesBot.TotalAllocatedCapital,
		PriceInterval:         req.FuturesBot.PriceInterval,
		ProfitSpread:          req.FuturesBot.ProfitSpread,
		OrderQuantity:         req.FuturesBot.OrderQuantity,
		MinOrderValue:         req.FuturesBot.MinOrderValue,
		BuyWindowSize:         req.FuturesBot.BuyWindowSize,
		SellWindowSize:        req.FuturesBot.SellWindowSize,
		ReconcileInterval:     req.FuturesBot.ReconcileInterval,
		OrderCleanupThreshold: req.FuturesBot.OrderCleanupThreshold,
		CleanupBatchSize:      req.FuturesBot.CleanupBatchSize,
		MarginLockDurationSec: req.FuturesBot.MarginLockDurationSec,
		PositionSafetyCheck:   req.FuturesBot.PositionSafetyCheck,
		Direction:             req.FuturesBot.Direction,
		PriceLow:              req.FuturesBot.PriceLow,
		PriceHigh:             req.FuturesBot.PriceHigh,
		TriggerPrice:          req.FuturesBot.TriggerPrice,
		GridMode:              req.FuturesBot.GridMode,
		GridShiftEnabled:      req.FuturesBot.GridShiftEnabled,
		GridShiftStep:         req.FuturesBot.GridShiftStep,
		CloseOnStop:           req.FuturesBot.CloseOnStop,
		GridRiskControl:       buildGridRiskControlFromRequest(req.FuturesBot),
	}
	if len(bcFutures.Strategies) == 0 {
		bcFutures.Strategies = []config.StrategyInstance{{Type: "grid", Weight: 1.0, Config: map[string]interface{}{}}}
	}
	applyBotDefaults(&bcFutures)

	bcSpot := config.BotConfig{
		ID:                    spotID,
		Name:                  spotName,
		Exchange:              req.SpotBot.Exchange,
		Symbol:                req.SpotBot.Symbol,
		MarketType:            "spot",
		Testnet:               req.SpotBot.Testnet,
		Enabled:               config.BoolPtr(true),
		Strategies:            req.SpotBot.Strategies,
		TotalAllocatedCapital: req.SpotBot.TotalAllocatedCapital,
		PriceInterval:         req.SpotBot.PriceInterval,
		ProfitSpread:          req.SpotBot.ProfitSpread,
		OrderQuantity:         req.SpotBot.OrderQuantity,
		MinOrderValue:         req.SpotBot.MinOrderValue,
		BuyWindowSize:         req.SpotBot.BuyWindowSize,
		SellWindowSize:        req.SpotBot.SellWindowSize,
		ReconcileInterval:     req.SpotBot.ReconcileInterval,
		OrderCleanupThreshold: req.SpotBot.OrderCleanupThreshold,
		CleanupBatchSize:      req.SpotBot.CleanupBatchSize,
		MarginLockDurationSec: req.SpotBot.MarginLockDurationSec,
		PositionSafetyCheck:   req.SpotBot.PositionSafetyCheck,
		Direction:             req.SpotBot.Direction,
		PriceLow:              req.SpotBot.PriceLow,
		PriceHigh:             req.SpotBot.PriceHigh,
		TriggerPrice:          req.SpotBot.TriggerPrice,
		GridMode:              req.SpotBot.GridMode,
		GridShiftEnabled:      req.SpotBot.GridShiftEnabled,
		GridShiftStep:         req.SpotBot.GridShiftStep,
		CloseOnStop:           req.SpotBot.CloseOnStop,
		GridRiskControl:       buildGridRiskControlFromRequest(req.SpotBot),
	}
	if len(bcSpot.Strategies) == 0 {
		bcSpot.Strategies = []config.StrategyInstance{{Type: "grid", Weight: 1.0, Config: map[string]interface{}{}}}
	}
	applyBotDefaults(&bcSpot)

	hedgeCfg := req.HedgeConfig
	if hedgeCfg.HedgeRatio <= 0 {
		hedgeCfg.HedgeRatio = 0.5
	}
	if hedgeCfg.RebalanceInterval <= 0 {
		hedgeCfg.RebalanceInterval = 3600
	}

	group := config.BotGroup{
		ID:          groupID,
		Name:        groupName,
		Type:        req.Type,
		BotIDs:      []string{futuresID, spotID},
		HedgeConfig: hedgeCfg,
	}

	if cfg.BotGroups == nil {
		cfg.BotGroups = []config.BotGroup{}
	}
	cfg.BotGroups = append(cfg.BotGroups, group)
	cfg.Bots = append(cfg.Bots, bcFutures, bcSpot)

	if err := fileConfigManager.UpdateConfig(cfg); err != nil {
		respondError(c, http.StatusInternalServerError, "error.config_save_failed", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "group_id": groupID, "bot_ids": []string{futuresID, spotID}})
}

func applyBotDefaults(bc *config.BotConfig) {
	if bc.ReconcileInterval <= 0 {
		bc.ReconcileInterval = 60
	}
	if bc.OrderCleanupThreshold <= 0 {
		bc.OrderCleanupThreshold = 50
	}
	if bc.CleanupBatchSize <= 0 {
		bc.CleanupBatchSize = 10
	}
	if bc.MarginLockDurationSec <= 0 {
		bc.MarginLockDurationSec = 10
	}
	if bc.PositionSafetyCheck <= 0 {
		bc.PositionSafetyCheck = 100
	}
	if bc.Direction == "" {
		bc.Direction = "LONG"
	}
	if bc.MinOrderValue <= 0 {
		bc.MinOrderValue = 20
	}
}

// deleteBotGroup 刪除 Bot 組
// DELETE /api/bot-groups/:id
func deleteBotGroup(c *gin.Context) {
	groupID := c.Param("id")
	if groupID == "" {
		respondError(c, http.StatusBadRequest, "error.invalid_group_id")
		return
	}
	if configManager == nil {
		respondError(c, http.StatusServiceUnavailable, "error.config_manager_unavailable")
		return
	}
	cfg, err := GetLatestConfig()
	if err != nil || cfg == nil {
		respondError(c, http.StatusInternalServerError, "error.config_load_failed")
		return
	}
	var newGroups []config.BotGroup
	var botIDsToRemove []string
	for _, g := range cfg.BotGroups {
		if g.ID != groupID {
			newGroups = append(newGroups, g)
		} else {
			botIDsToRemove = g.BotIDs
		}
	}
	if len(botIDsToRemove) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "Bot group not found"})
		return
	}
	removeSet := make(map[string]bool)
	for _, id := range botIDsToRemove {
		removeSet[id] = true
	}
	var newBots []config.BotConfig
	for _, b := range cfg.Bots {
		id := b.ID
		if id == "" {
			id = config.GenerateBotID(b.Exchange, b.Symbol, b.GetMarketType())
		}
		if !removeSet[id] {
			newBots = append(newBots, b)
		}
	}
	cfg.BotGroups = newGroups
	cfg.Bots = newBots
	if err := fileConfigManager.UpdateConfig(cfg); err != nil {
		respondError(c, http.StatusInternalServerError, "error.config_save_failed", err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"ok": true, "group_id": groupID})
}

// UpdateBotStrategyRequest 更新 Bot 策略請求
type UpdateBotStrategyRequest struct {
	Strategies     []config.StrategyInstance `json:"strategies"`     // 策略列表
	PriceInterval  *float64                  `json:"price_interval,omitempty"`  // 價格間隔
	ProfitSpread   *float64                  `json:"profit_spread,omitempty"`   // 利潤間距
	OrderQuantity  *float64                  `json:"order_quantity,omitempty"`  // 每單金額
	PriceLow       *float64                  `json:"price_low,omitempty"`        // 網格價格下限
	PriceHigh      *float64                  `json:"price_high,omitempty"`       // 網格價格上限
	Direction      *string                   `json:"direction,omitempty"`       // 交易方向：LONG/SHORT/BOTH
}

// putBotStrategy 更新 Bot 策略配置
// PUT /api/bots/:id/strategy
func putBotStrategy(c *gin.Context) {
	botID := c.Param("id")
	if botID == "" {
		respondError(c, http.StatusBadRequest, "error.invalid_bot_id")
		return
	}
	if configManager == nil {
		respondError(c, http.StatusServiceUnavailable, "error.config_manager_unavailable")
		return
	}
	var req UpdateBotStrategyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "error.invalid_request", err)
		return
	}

	cfg, err := GetLatestConfig()
	if err != nil || cfg == nil {
		respondError(c, http.StatusInternalServerError, "error.config_load_failed")
		return
	}

	// 查找並更新 Bot
	found := false
	var originalStrategyType string
	for i := range cfg.Bots {
		bc := &cfg.Bots[i]
		id := bc.ID
		if id == "" {
			id = config.GenerateBotID(bc.Exchange, bc.Symbol, bc.GetMarketType())
		}
		if id == botID {
			// 檢查 Bot 是否正在運行（只對策略類型切換有限制，參數修改允許）
			isRunning := false
			if botManagerProvider != nil {
				if bot, ok := botManagerProvider.GetBot(botID); ok {
					isRunning = bot.Running
				}
			}

			// 記錄原始策略類型
			if len(bc.Strategies) > 0 {
				originalStrategyType = bc.Strategies[0].Type
			}

			// 驗證策略變更：只能在相關策略類型之間切換
			// 網格策略 (grid) 可以切換到: grid+trend, trend, momentum
			// DCA 策略 (dca) 只能在 dca 相關策略內切換
			gridRelatedStrategies := map[string]bool{
				"grid":              true,
				"trend_following":   true,
				"momentum":          true,
				"mean_reversion":    true,
			}
			dcaRelatedStrategies := map[string]bool{
				"dca":        true,
				"martingale": true,
			}

			// 只有在修改策略類型時才進行驗證
			if len(req.Strategies) > 0 {
				newStrategyType := req.Strategies[0].Type
				isOriginalGrid := gridRelatedStrategies[originalStrategyType]
				isOriginalDCA := dcaRelatedStrategies[originalStrategyType]
				isNewGrid := gridRelatedStrategies[newStrategyType]
				isNewDCA := dcaRelatedStrategies[newStrategyType]

				// 運行中不允許切換策略類型
				if isRunning && newStrategyType != originalStrategyType {
					c.JSON(http.StatusConflict, gin.H{
						"error":     "bot_running",
						"error_key": "error.bot_running_cannot_change_strategy_type",
						"message":   "Cannot change strategy type while bot is running. Parameters can be modified, but strategy type changes require stopping the bot first.",
					})
					return
				}

				// 不允許從 DCA 切換到網格類策略，或反之
				if (isOriginalDCA && isNewGrid) || (isOriginalGrid && isNewDCA) {
					respondError(c, http.StatusBadRequest, "error.invalid_strategy_change")
					return
				}
			}

			// 更新策略
			if len(req.Strategies) > 0 {
				bc.Strategies = req.Strategies
			}

			// 更新價格間隔
			if req.PriceInterval != nil {
				bc.PriceInterval = *req.PriceInterval
			}
			// 更新利潤間距
			if req.ProfitSpread != nil {
				bc.ProfitSpread = *req.ProfitSpread
			}
			// 更新每單金額
			if req.OrderQuantity != nil {
				bc.OrderQuantity = *req.OrderQuantity
			}
			// 更新網格價格下限
			if req.PriceLow != nil {
				bc.PriceLow = *req.PriceLow
			}
			// 更新網格價格上限
			if req.PriceHigh != nil {
				bc.PriceHigh = *req.PriceHigh
			}
			// 更新交易方向
			if req.Direction != nil {
				bc.Direction = *req.Direction
			}

			found = true
			break
		}
	}

	if !found {
		respondError(c, http.StatusNotFound, "error.bot_not_found")
		return
	}

	if err := fileConfigManager.UpdateConfig(cfg); err != nil {
		respondError(c, http.StatusInternalServerError, "error.config_save_failed", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ok":      true,
		"bot_id":  botID,
		"message": "Strategy updated successfully",
	})
}
