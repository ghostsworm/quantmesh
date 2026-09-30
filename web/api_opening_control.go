package web

import (
	"errors"
	"net/http"
	"reflect"
	"strings"

	"github.com/gin-gonic/gin"
	"quantmesh/config"
	"quantmesh/execution"
	"quantmesh/logger"
	"quantmesh/position"
)

// openingControlRuntimeMatchesQuery 校驗運行時交易對與請求參數一致（防止 bot_id 與 exchange/symbol 參數錯配）
func openingControlRuntimeMatchesQuery(rtInterface interface{}, exchange, symbol, marketType string) bool {
	rtVal := reflect.ValueOf(rtInterface)
	if rtVal.Kind() == reflect.Ptr {
		rtVal = rtVal.Elem()
	}
	configField := rtVal.FieldByName("Config")
	if !configField.IsValid() {
		return false
	}
	cfg := configField.Addr().Interface().(*config.SymbolConfig)
	cfgMT := cfg.GetMarketType()
	if cfgMT == "spot_margin" {
		cfgMT = "spot"
	}
	if cfgMT != "spot" && cfgMT != "futures" {
		cfgMT = "futures"
	}
	reqMT := strings.TrimSpace(strings.ToLower(marketType))
	if reqMT != "spot" && reqMT != "futures" {
		reqMT = "futures"
	}
	return strings.EqualFold(cfg.Exchange, exchange) &&
		strings.EqualFold(cfg.Symbol, symbol) &&
		cfgMT == reqMT
}

// findOpeningControlConfigFromConfig 從配置文件中查找開倉控制配置（Bot 未運行時使用）
// preferredBotID 非空時優先匹配該 Bot（與運行時 UUID 鍵一致），避免同交易對多條配置時誤用他條
func findOpeningControlConfigFromConfig(exchange, symbol, marketType, preferredBotID string) *config.OpenPositionControl {
	cfg, err := GetLatestConfig()
	if err != nil || cfg == nil {
		return nil
	}
	target, _, err := openingControlTarget(cfg, strings.TrimSpace(preferredBotID), exchange, symbol, marketType)
	if err != nil {
		return nil
	}
	copy := config.CloneOpenPositionControl(*target)
	return &copy
}

func openingLimitStatus(control config.OpenPositionControl) gin.H {
	quantity, value, layers := control.PositionLimits()
	overridden := control.BotRiskControl != nil && control.BotRiskControl.Enabled
	return gin.H{
		"max_position_value":                control.MaxPositionValue,
		"max_position_layers":               control.MaxPositionLayers,
		"effective_max_position_quantity":   quantity,
		"effective_max_position_value":      value,
		"effective_max_position_layers":     layers,
		"bot_risk_control_overrides_limits": overridden,
		"schedule_rules":                    control.ScheduleRules,
		"periodic_rule":                     control.PeriodicRule,
	}
}

func getSpecializedOpeningRuntime(c *gin.Context, exchange, symbol string) (*execution.OpeningGate, config.OpenPositionControl, bool) {
	rt, _, ok := getOpeningControlRuntimeAndController(c, exchange, symbol)
	if !ok || c.Writer.Written() {
		return nil, config.OpenPositionControl{}, false
	}
	value := reflect.ValueOf(rt)
	if value.Kind() == reflect.Ptr {
		value = value.Elem()
	}
	manager := value.FieldByName("SuperPositionManager")
	gateField := value.FieldByName("OpeningGate")
	if !manager.IsValid() || !manager.IsNil() || !gateField.IsValid() || gateField.IsNil() {
		return nil, config.OpenPositionControl{}, false
	}
	gate, _ := gateField.Interface().(*execution.OpeningGate)
	control := config.OpenPositionControl{}
	getter := value.FieldByName("GetOpenControl")
	if getter.IsValid() && !getter.IsNil() {
		if get, ok := getter.Interface().(func() config.OpenPositionControl); ok {
			control = get()
		}
	}
	return gate, control, gate != nil
}

// getOpeningControlStatus 獲取開倉控制狀態
// GET /api/opening-control/status?exchange=xxx&symbol=xxx
func getOpeningControlStatus(c *gin.Context) {
	exchange := c.Query("exchange")
	symbol := c.Query("symbol")
	if exchange == "" || symbol == "" {
		respondError(c, http.StatusBadRequest, "error.exchange_symbol_required")
		return
	}
	gate, control, specialized := getSpecializedOpeningRuntime(c, exchange, symbol)
	if c.Writer.Written() {
		return
	}
	if specialized {
		reason := ""
		for _, source := range []string{"manual", "position_limit", "schedule", "periodic", "runtime_shutdown", "funding_carry_exposure_unverified"} {
			if gate.HasBlock(source) {
				reason = source
				break
			}
		}
		c.JSON(http.StatusOK, gin.H{"exchange": exchange, "symbol": symbol, "opening_paused": gate.Blocked(),
			"pause_reason": reason, "valuation_available": false, "position_managed_by_strategy": true,
			"current_position_value_usdt": 0.0, "current_actual_margin_usdt": 0.0, "current_leverage": 1,
			"current_layers": 0, "config": openingLimitStatus(control)})
		return
	}

	spm, cfg, ok := getOpeningControlComponents(c, exchange, symbol)
	if !ok {
		if c.Writer.Written() {
			return
		}
		// Bot 未運行時，從配置返回降級狀態（僅配置，無實時倉位數據）
		marketType := c.DefaultQuery("market_type", "futures")
		if marketType != "spot" && marketType != "futures" {
			marketType = "futures"
		}
		cfgFallback := findOpeningControlConfigFromConfig(exchange, symbol, marketType, c.Query("bot_id"))
		if cfgFallback != nil {
			c.JSON(http.StatusOK, gin.H{
				"exchange":                    exchange,
				"symbol":                      symbol,
				"opening_paused":              true,
				"pause_reason":                "bot_stopped",
				"current_position_value_usdt": 0.0,
				"current_actual_margin_usdt":  0.0,
				"current_leverage":            1,
				"current_layers":              0,
				"config":                      openingLimitStatus(*cfgFallback),
			})
			return
		}
		respondError(c, http.StatusNotFound, "error.symbol_not_found")
		return
	}

	currentPrice := spm.GetLastMarketPrice()
	totalValue := 0.0
	actualMargin := 0.0 // 實際占用資金（保證金）
	leverage := 1
	if currentPrice > 0 {
		totalValue = spm.GetTotalPositionValueAtPrice(currentPrice)
		leverage = spm.GetLeverage()
		if leverage <= 0 {
			leverage = 1
		}
		actualMargin = totalValue / float64(leverage)
	}
	layers := spm.GetActiveLayers()

	c.JSON(http.StatusOK, gin.H{
		"exchange":                    exchange,
		"symbol":                      symbol,
		"opening_paused":              spm.IsOpeningPaused(),
		"pause_reason":                spm.GetOpeningPauseReason(),
		"protective_liquidation":      spm.GetProtectiveLiquidationStatus(),
		"current_position_value_usdt": totalValue,   // 倉位價值（供參考）
		"current_actual_margin_usdt":  actualMargin, // 實際占用資金
		"current_leverage":            leverage,     // 槓桿倍數
		"current_layers":              layers,
		"config":                      openingLimitStatus(cfg.OpenPositionControl),
	})
}

// pauseOpening 手動暫停開倉
// POST /api/opening-control/pause?exchange=xxx&symbol=xxx
func pauseOpening(c *gin.Context) {
	riskControlUpdateMu.Lock()
	defer riskControlUpdateMu.Unlock()
	exchange := c.Query("exchange")
	symbol := c.Query("symbol")
	if exchange == "" || symbol == "" {
		respondError(c, http.StatusBadRequest, "error.exchange_symbol_required")
		return
	}
	gate, _, specialized := getSpecializedOpeningRuntime(c, exchange, symbol)
	if c.Writer.Written() {
		return
	}
	if specialized {
		if err := persistOpeningPause(strings.TrimSpace(c.Query("bot_id")), exchange, symbol, c.DefaultQuery("market_type", "futures"), true); err != nil {
			respondError(c, http.StatusInternalServerError, "error.opening_pause_persist_failed")
			return
		}
		gate.Block("manual")
		c.JSON(http.StatusOK, gin.H{"message": "開倉已暫停", "opening_paused": true})
		return
	}

	spm, _, ok := getOpeningControlComponents(c, exchange, symbol)
	if !ok {
		if !c.Writer.Written() {
			c.JSON(http.StatusConflict, gin.H{"error": "opening_bot_stopped"})
		}
		return
	}
	if err := persistOpeningPause(strings.TrimSpace(c.Query("bot_id")), exchange, symbol, c.DefaultQuery("market_type", "futures"), true); err != nil {
		respondError(c, http.StatusInternalServerError, "error.opening_pause_persist_failed")
		return
	}

	spm.PauseOpening("manual")
	logger.Info("🔄 [開倉管理] 手動暫停開倉 [%s:%s]", exchange, symbol)
	c.JSON(http.StatusOK, gin.H{"message": "開倉已暫停", "opening_paused": true})
}

// resumeOpening 手動恢復開倉
// POST /api/opening-control/resume?exchange=xxx&symbol=xxx
func resumeOpening(c *gin.Context) {
	riskControlUpdateMu.Lock()
	defer riskControlUpdateMu.Unlock()
	exchange := c.Query("exchange")
	symbol := c.Query("symbol")
	if exchange == "" || symbol == "" {
		respondError(c, http.StatusBadRequest, "error.exchange_symbol_required")
		return
	}
	gate, _, specialized := getSpecializedOpeningRuntime(c, exchange, symbol)
	if c.Writer.Written() {
		return
	}
	if specialized {
		allowed, err := runRecoveryIfRiskUnheld(func() error {
			if err := persistOpeningPause(strings.TrimSpace(c.Query("bot_id")), exchange, symbol, c.DefaultQuery("market_type", "futures"), false); err != nil {
				return err
			}
			gate.Unblock("manual")
			return nil
		})
		if err != nil {
			if errors.Is(err, errOpeningPauseCoordinatorUnavailable) {
				c.JSON(http.StatusServiceUnavailable, gin.H{"error": "risk_pause_coordinator_unavailable", "opening_paused": true})
				return
			}
			c.JSON(http.StatusInternalServerError, gin.H{"error": "opening_resume_persist_failed", "opening_paused": true})
			return
		}
		if !allowed {
			c.JSON(http.StatusConflict, gin.H{"error": "risk_pause_active", "opening_paused": true})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "恢復請求已處理", "opening_paused": gate.Blocked()})
		return
	}

	spm, _, ok := getOpeningControlComponents(c, exchange, symbol)
	if !ok {
		if !c.Writer.Written() {
			c.JSON(http.StatusConflict, gin.H{"error": "opening_bot_stopped"})
		}
		return
	}

	persistenceFailed := false
	allowed, err := runRecoveryIfRiskUnheld(func() error {
		if err := spm.ResumeOpeningManually(); err != nil {
			return err
		}
		if err := persistOpeningPause(strings.TrimSpace(c.Query("bot_id")), exchange, symbol, c.DefaultQuery("market_type", "futures"), false); err != nil {
			spm.PauseOpening("manual")
			persistenceFailed = true
			return err
		}
		return nil
	})
	if errors.Is(err, errOpeningPauseCoordinatorUnavailable) {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "risk_pause_coordinator_unavailable", "opening_paused": true})
		return
	}
	if !allowed {
		c.JSON(http.StatusConflict, gin.H{"error": "risk_pause_active", "opening_paused": true})
		return
	}
	if err != nil && persistenceFailed {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "opening_resume_persist_failed", "opening_paused": true})
		return
	}
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": err.Error(), "opening_paused": spm.IsOpeningPaused(), "protective_liquidation": spm.GetProtectiveLiquidationStatus()})
		return
	}
	logger.Info("🔄 [開倉管理] 手動恢復開倉 [%s:%s]", exchange, symbol)
	c.JSON(http.StatusOK, gin.H{"message": "恢復請求已處理", "opening_paused": spm.IsOpeningPaused(), "pause_reason": spm.GetOpeningPauseReason()})
}

// getOpeningControlConfig 獲取開倉控制配置
// GET /api/opening-control/config?exchange=xxx&symbol=xxx
func getOpeningControlConfig(c *gin.Context) {
	exchange := c.Query("exchange")
	symbol := c.Query("symbol")
	if exchange == "" || symbol == "" {
		respondError(c, http.StatusBadRequest, "error.exchange_symbol_required")
		return
	}
	_, control, specialized := getSpecializedOpeningRuntime(c, exchange, symbol)
	if c.Writer.Written() {
		return
	}
	if specialized {
		c.JSON(http.StatusOK, control)
		return
	}

	_, cfg, ok := getOpeningControlComponents(c, exchange, symbol)
	if !ok {
		if c.Writer.Written() {
			return
		}
		marketType := c.DefaultQuery("market_type", "futures")
		if marketType != "spot" && marketType != "futures" {
			marketType = "futures"
		}
		cfgFallback := findOpeningControlConfigFromConfig(exchange, symbol, marketType, c.Query("bot_id"))
		if cfgFallback != nil {
			c.JSON(http.StatusOK, cfgFallback)
			return
		}
		respondError(c, http.StatusNotFound, "error.symbol_not_found")
		return
	}

	c.JSON(http.StatusOK, cfg.OpenPositionControl)
}

// putOpeningControlConfig 更新開倉控制配置
// PUT /api/opening-control/config?exchange=xxx&symbol=xxx
func putOpeningControlConfig(c *gin.Context) {
	riskControlUpdateMu.Lock()
	defer riskControlUpdateMu.Unlock()
	exchange, symbol := strings.TrimSpace(c.Query("exchange")), strings.TrimSpace(c.Query("symbol"))
	market := c.DefaultQuery("market_type", "futures")
	if exchange == "" || symbol == "" || (market != "spot" && market != "futures") {
		respondError(c, http.StatusBadRequest, "error.invalid_request")
		return
	}
	var payload struct {
		MaxPositionValue  *float64              `json:"max_position_value"`
		MaxPositionLayers *int                  `json:"max_position_layers"`
		ScheduleRules     []config.ScheduleRule `json:"schedule_rules"`
		PeriodicRule      *config.PeriodicRule  `json:"periodic_rule"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil || payload.MaxPositionValue == nil || payload.MaxPositionLayers == nil {
		respondError(c, http.StatusBadRequest, "error.invalid_request")
		return
	}
	req := config.OpenPositionControl{MaxPositionValue: *payload.MaxPositionValue, MaxPositionLayers: *payload.MaxPositionLayers,
		ScheduleRules: payload.ScheduleRules, PeriodicRule: payload.PeriodicRule}
	if err := validateOpeningControl(req); err != nil {
		respondError(c, http.StatusBadRequest, "error.invalid_request")
		return
	}
	// Do not persist a live change that the runtime cannot apply. This matters
	// for strategy-owned runtimes without the grid OpeningController.
	var rt interface{}
	var running bool
	var specializedUpdate func(config.OpenPositionControl) error
	if symbolManagerProvider != nil {
		rt, _, running = getOpeningControlRuntimeAndController(c, exchange, symbol)
		if c.Writer.Written() {
			return
		}
	}
	var controller *position.OpeningController
	if running && rt != nil {
		_, controller, _ = extractOpeningControllerFromRuntime(rt)
		if controller == nil {
			specializedUpdate = specializedOpeningControlUpdater(rt)
		}
		if controller == nil && specializedUpdate == nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "opening_runtime_unavailable", "persisted": false, "applied": false})
			return
		}
	}
	var clamp func(config.OpenPositionControl) (config.OpenPositionControl, error)
	if running && rt != nil {
		clamp = openingControlClamp(rt)
	}
	control, _, err := persistOpeningControl(strings.TrimSpace(c.Query("bot_id")), exchange, symbol, market, req, clamp)
	if err != nil {
		if clamp != nil {
			c.JSON(http.StatusConflict, gin.H{"error": "opening_capital_limit_unavailable", "persisted": false, "applied": false})
			return
		}
		code := http.StatusInternalServerError
		if err == errOpeningTarget {
			code = http.StatusConflict
		}
		c.JSON(code, gin.H{"error": "opening_config_save_failed", "persisted": false, "applied": false})
		return
	}
	if running && rt != nil {
		if specializedUpdate != nil {
			if err := specializedUpdate(control); err != nil {
				c.JSON(http.StatusServiceUnavailable, gin.H{"error": "opening_runtime_unavailable", "persisted": true, "applied": false})
				return
			}
			c.JSON(http.StatusOK, gin.H{"persisted": true, "applied": true})
			return
		}
		// OpeningController clones the value and publishes the SPM immutable
		// controls; never write the runtime's shared Config via reflection.
		value := reflect.ValueOf(rt)
		if value.Kind() == reflect.Ptr {
			value = value.Elem()
		}
		copy := *value.FieldByName("Config").Addr().Interface().(*config.SymbolConfig)
		copy.OpenPositionControl = control
		controller.UpdateConfig(&copy)
	}
	c.JSON(http.StatusOK, gin.H{"persisted": true, "applied": running && rt != nil})
}

func openingControlClamp(rt interface{}) func(config.OpenPositionControl) (config.OpenPositionControl, error) {
	value := reflect.ValueOf(rt)
	if value.Kind() == reflect.Ptr {
		value = value.Elem()
	}
	field := value.FieldByName("ClampOpenControl")
	if !field.IsValid() || field.IsNil() {
		return nil
	}
	clamp, _ := field.Interface().(func(config.OpenPositionControl) (config.OpenPositionControl, error))
	return clamp
}

func specializedOpeningControlUpdater(rt interface{}) func(config.OpenPositionControl) error {
	value := reflect.ValueOf(rt)
	if value.Kind() == reflect.Ptr {
		value = value.Elem()
	}
	field := value.FieldByName("UpdateOpenControl")
	if !field.IsValid() || field.IsNil() {
		return nil
	}
	if updater, ok := field.Interface().(func(config.OpenPositionControl) error); ok {
		return updater
	}
	return nil
}

// getOpeningControlComponents 獲取 SuperPositionManager 和 SymbolConfig
func getOpeningControlComponents(c *gin.Context, exchange, symbol string) (*position.SuperPositionManager, *config.SymbolConfig, bool) {
	rtInterface, _, ok := getOpeningControlRuntimeAndController(c, exchange, symbol)
	if !ok {
		return nil, nil, false
	}

	rtVal := reflect.ValueOf(rtInterface)
	if rtVal.Kind() == reflect.Ptr {
		rtVal = rtVal.Elem()
	}

	spmField := rtVal.FieldByName("SuperPositionManager")
	if !spmField.IsValid() || spmField.IsNil() {
		respondError(c, http.StatusInternalServerError, "error.position_manager_unavailable")
		return nil, nil, false
	}
	spm, _ := spmField.Interface().(*position.SuperPositionManager)
	if spm == nil {
		respondError(c, http.StatusInternalServerError, "error.position_manager_unavailable")
		return nil, nil, false
	}

	configField := rtVal.FieldByName("Config")
	if !configField.IsValid() {
		respondError(c, http.StatusInternalServerError, "error.config_unavailable")
		return nil, nil, false
	}
	cfg := configField.Addr().Interface().(*config.SymbolConfig)

	copy := *cfg
	copy.OpenPositionControl = spm.GetRiskControls().Open
	return spm, &copy, true
}

// getOpeningControlRuntimeAndController 獲取 SymbolRuntime 和 OpeningController
func getOpeningControlRuntimeAndController(c *gin.Context, exchange, symbol string) (rtInterface interface{}, oc *position.OpeningController, ok bool) {
	market := c.DefaultQuery("market_type", "futures")
	if market != "spot" && market != "futures" {
		respondError(c, http.StatusBadRequest, "error.invalid_request")
		return nil, nil, false
	}
	if symbolManagerProvider == nil {
		respondError(c, http.StatusServiceUnavailable, "error.symbol_manager_unavailable")
		return nil, nil, false
	}
	cfg, err := GetLatestConfig()
	if err != nil || cfg == nil {
		respondError(c, http.StatusServiceUnavailable, "error.config_unavailable")
		return nil, nil, false
	}
	_, id, err := openingControlTarget(cfg, strings.TrimSpace(c.Query("bot_id")), exchange, symbol, market)
	if err != nil {
		c.JSON(http.StatusConflict, gin.H{"error": "opening_target_unavailable"})
		return nil, nil, false
	}
	var exists bool
	if id != "" {
		rtInterface, exists = symbolManagerProvider.GetByBotID(id)
	} else {
		rtInterface, exists = symbolManagerProvider.GetEx(exchange, symbol, market)
	}
	if !exists || rtInterface == nil {
		// A resolved but stopped target permits configuration-only GET responses.
		return nil, nil, false
	}
	if !openingControlRuntimeMatchesQuery(rtInterface, exchange, symbol, market) {
		c.JSON(http.StatusConflict, gin.H{"error": "opening_runtime_mismatch"})
		return nil, nil, false
	}
	return extractOpeningControllerFromRuntime(rtInterface)
}

func extractOpeningControllerFromRuntime(rtInterface interface{}) (interface{}, *position.OpeningController, bool) {
	rtVal := reflect.ValueOf(rtInterface)
	if rtVal.Kind() == reflect.Ptr {
		rtVal = rtVal.Elem()
	}
	var oc *position.OpeningController
	ocField := rtVal.FieldByName("OpeningController")
	if ocField.IsValid() && !ocField.IsNil() {
		if o, _ := ocField.Interface().(*position.OpeningController); o != nil {
			oc = o
		}
	}
	return rtInterface, oc, true
}
