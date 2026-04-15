package web

import (
	"net/http"
	"reflect"
	"strings"

	"github.com/gin-gonic/gin"
	"quantmesh/config"
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
	mt := strings.TrimSpace(strings.ToLower(marketType))
	if mt != "spot" && mt != "futures" {
		mt = "futures"
	}
	if id := strings.TrimSpace(preferredBotID); id != "" {
		for i := range cfg.Bots {
			b := &cfg.Bots[i]
			if strings.EqualFold(strings.TrimSpace(b.ID), id) {
				return &b.OpenPositionControl
			}
		}
	}
	// 優先從 Bots 查找
	for i := range cfg.Bots {
		b := &cfg.Bots[i]
		bmt := b.GetMarketType()
		if bmt == "spot_margin" {
			bmt = "spot"
		}
		if strings.EqualFold(b.Exchange, exchange) && strings.EqualFold(b.Symbol, symbol) && bmt == mt {
			return &b.OpenPositionControl
		}
	}
	// 兼容舊配置：從 Trading.Symbols 查找
	for i := range cfg.Trading.Symbols {
		s := &cfg.Trading.Symbols[i]
		symMT := s.GetMarketType()
		if symMT == "" {
			symMT = "futures"
		}
		if symMT == "spot_margin" {
			symMT = "spot"
		}
		if strings.EqualFold(s.Exchange, exchange) && strings.EqualFold(s.Symbol, symbol) && symMT == mt {
			return &s.OpenPositionControl
		}
	}
	return nil
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

	spm, cfg, ok := getOpeningControlComponents(c, exchange, symbol)
	if !ok {
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
				"config": gin.H{
					"max_position_value":  cfgFallback.MaxPositionValue,
					"max_position_layers": cfgFallback.MaxPositionLayers,
					"schedule_rules":      cfgFallback.ScheduleRules,
					"periodic_rule":       cfgFallback.PeriodicRule,
				},
			})
			return
		}
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
		"exchange":                      exchange,
		"symbol":                        symbol,
		"opening_paused":                spm.IsOpeningPaused(),
		"pause_reason":                  spm.GetOpeningPauseReason(),
		"current_position_value_usdt":   totalValue,      // 倉位價值（供參考）
		"current_actual_margin_usdt":    actualMargin,    // 實際占用資金
		"current_leverage":              leverage,         // 槓桿倍數
		"current_layers":                layers,
		"config": gin.H{
			"max_position_value":  cfg.OpenPositionControl.MaxPositionValue,
			"max_position_layers": cfg.OpenPositionControl.MaxPositionLayers,
			"schedule_rules":      cfg.OpenPositionControl.ScheduleRules,
			"periodic_rule":       cfg.OpenPositionControl.PeriodicRule,
		},
	})
}

// pauseOpening 手動暫停開倉
// POST /api/opening-control/pause?exchange=xxx&symbol=xxx
func pauseOpening(c *gin.Context) {
	exchange := c.Query("exchange")
	symbol := c.Query("symbol")
	if exchange == "" || symbol == "" {
		respondError(c, http.StatusBadRequest, "error.exchange_symbol_required")
		return
	}

	spm, _, ok := getOpeningControlComponents(c, exchange, symbol)
	if !ok {
		return
	}

	spm.PauseOpening("manual")
	logger.Info("🔄 [開倉管理] 手動暫停開倉 [%s:%s]", exchange, symbol)
	c.JSON(http.StatusOK, gin.H{"message": "開倉已暫停", "opening_paused": true})
}

// resumeOpening 手動恢復開倉
// POST /api/opening-control/resume?exchange=xxx&symbol=xxx
func resumeOpening(c *gin.Context) {
	exchange := c.Query("exchange")
	symbol := c.Query("symbol")
	if exchange == "" || symbol == "" {
		respondError(c, http.StatusBadRequest, "error.exchange_symbol_required")
		return
	}

	spm, _, ok := getOpeningControlComponents(c, exchange, symbol)
	if !ok {
		return
	}

	spm.ResumeOpening()
	logger.Info("🔄 [開倉管理] 手動恢復開倉 [%s:%s]", exchange, symbol)
	c.JSON(http.StatusOK, gin.H{"message": "開倉已恢復", "opening_paused": false})
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

	_, cfg, ok := getOpeningControlComponents(c, exchange, symbol)
	if !ok {
		marketType := c.DefaultQuery("market_type", "futures")
		if marketType != "spot" && marketType != "futures" {
			marketType = "futures"
		}
		cfgFallback := findOpeningControlConfigFromConfig(exchange, symbol, marketType, c.Query("bot_id"))
		if cfgFallback != nil {
			c.JSON(http.StatusOK, cfgFallback)
			return
		}
		return
	}

	c.JSON(http.StatusOK, cfg.OpenPositionControl)
}

// putOpeningControlConfig 更新開倉控制配置
// PUT /api/opening-control/config?exchange=xxx&symbol=xxx
func putOpeningControlConfig(c *gin.Context) {
	exchange := c.Query("exchange")
	symbol := c.Query("symbol")
	if exchange == "" || symbol == "" {
		respondError(c, http.StatusBadRequest, "error.exchange_symbol_required")
		return
	}

	var req config.OpenPositionControl
	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "error.invalid_request", err.Error())
		return
	}

	marketType := c.DefaultQuery("market_type", "futures")
	if marketType != "spot" && marketType != "futures" {
		marketType = "futures"
	}

	rtInterface, oc, ok := getOpeningControlRuntimeAndController(c, exchange, symbol)
	if ok {
		// 更新運行時 Config 中的 OpenPositionControl（不包含 PauseOpening 運行時狀態）
		rtVal := reflect.ValueOf(rtInterface)
		if rtVal.Kind() == reflect.Ptr {
			rtVal = rtVal.Elem()
		}
		configField := rtVal.FieldByName("Config")
		if configField.IsValid() && configField.CanSet() {
			cfg := configField.Addr().Interface().(*config.SymbolConfig)
			cfg.OpenPositionControl.MaxPositionValue = req.MaxPositionValue
			cfg.OpenPositionControl.MaxPositionLayers = req.MaxPositionLayers
			cfg.OpenPositionControl.ScheduleRules = req.ScheduleRules
			cfg.OpenPositionControl.PeriodicRule = req.PeriodicRule
		}

		// 更新 OpeningController 的配置指針（Config 已在上方更新，oc 持有 &rt.Config 會自動看到）
		if oc != nil {
			rtVal := reflect.ValueOf(rtInterface)
			if rtVal.Kind() == reflect.Ptr {
				rtVal = rtVal.Elem()
			}
			configField := rtVal.FieldByName("Config")
			if configField.IsValid() {
				cfgPtr := configField.Addr().Interface().(*config.SymbolConfig)
				oc.UpdateConfig(cfgPtr)
			}
		}
	}

	// 持久化到配置文件（Bot 運行與否都需持久化，從 GetLatestConfig 獲取最新配置）
	cfg, err := GetLatestConfig()
	if err != nil || cfg == nil || fileConfigManager == nil {
		if !ok {
			respondError(c, http.StatusNotFound, "error.symbol_not_found")
			return
		}
		logger.Warn("⚠️ [開倉管理] 配置持久化跳過（配置管理器不可用）")
		c.JSON(http.StatusOK, gin.H{"message": "配置已更新"})
		return
	}

	persisted := false
	var syncedBotID string
	botIDQ := strings.TrimSpace(c.Query("bot_id"))
	if botIDQ != "" {
		for i := range cfg.Bots {
			b := &cfg.Bots[i]
			if strings.EqualFold(strings.TrimSpace(b.ID), botIDQ) {
				b.OpenPositionControl.MaxPositionValue = req.MaxPositionValue
				b.OpenPositionControl.MaxPositionLayers = req.MaxPositionLayers
				b.OpenPositionControl.ScheduleRules = req.ScheduleRules
				b.OpenPositionControl.PeriodicRule = req.PeriodicRule
				syncedBotID = b.ID
				if syncedBotID == "" {
					syncedBotID = config.GenerateBotID(b.Exchange, b.Symbol, b.GetMarketType())
				}
				persisted = true
				break
			}
		}
	}
	// 更新 Bots 中的開倉控制配置（無 bot_id 或按 ID 未命中時按交易對匹配）
	if !persisted {
		for i := range cfg.Bots {
			b := &cfg.Bots[i]
			bmt := b.GetMarketType()
			if bmt == "spot_margin" {
				bmt = "spot"
			}
			if strings.EqualFold(b.Exchange, exchange) && strings.EqualFold(b.Symbol, symbol) && bmt == marketType {
				b.OpenPositionControl.MaxPositionValue = req.MaxPositionValue
				b.OpenPositionControl.MaxPositionLayers = req.MaxPositionLayers
				b.OpenPositionControl.ScheduleRules = req.ScheduleRules
				b.OpenPositionControl.PeriodicRule = req.PeriodicRule
				syncedBotID = b.ID
				if syncedBotID == "" {
					syncedBotID = config.GenerateBotID(b.Exchange, b.Symbol, b.GetMarketType())
				}
				persisted = true
				break
			}
		}
	}
	// 兼容舊配置：更新 Trading.Symbols
	if !persisted {
		for i := range cfg.Trading.Symbols {
			sym := &cfg.Trading.Symbols[i]
			symMT := sym.GetMarketType()
			if symMT == "" {
				symMT = "futures"
			}
			if symMT == "spot_margin" {
				symMT = "spot"
			}
			if strings.EqualFold(sym.Exchange, exchange) && strings.EqualFold(sym.Symbol, symbol) && symMT == marketType {
				sym.OpenPositionControl.MaxPositionValue = req.MaxPositionValue
				sym.OpenPositionControl.MaxPositionLayers = req.MaxPositionLayers
				sym.OpenPositionControl.ScheduleRules = req.ScheduleRules
				sym.OpenPositionControl.PeriodicRule = req.PeriodicRule
				persisted = true
				break
			}
		}
	}

	if persisted {
		if syncedBotID != "" {
			if err := fileConfigManager.UpdateConfigWithBotHistorySource(cfg, "put_opening_control"); err != nil {
				logger.Warn("⚠️ [開倉管理] 配置持久化失敗: %v", err)
			}
		} else if err := fileConfigManager.UpdateConfig(cfg); err != nil {
			logger.Warn("⚠️ [開倉管理] 配置持久化失敗: %v", err)
		}
	} else if !ok {
		respondError(c, http.StatusNotFound, "error.symbol_not_found")
		return
	}

	logger.Info("🔄 [開倉管理] 配置已更新 [%s:%s]", exchange, symbol)
	c.JSON(http.StatusOK, gin.H{"message": "配置已更新"})
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

	return spm, cfg, true
}

// getOpeningControlRuntimeAndController 獲取 SymbolRuntime 和 OpeningController
func getOpeningControlRuntimeAndController(c *gin.Context, exchange, symbol string) (rtInterface interface{}, oc *position.OpeningController, ok bool) {
	if symbolManagerProvider == nil {
		respondError(c, http.StatusServiceUnavailable, "error.symbol_manager_unavailable")
		return nil, nil, false
	}

	marketType := c.DefaultQuery("market_type", "futures")
	if marketType != "spot" && marketType != "futures" {
		marketType = "futures"
	}

	// 帶 UUID 的 Bot 運行時鍵為 bot_id，GetEx 仍按 exchange:symbol:mt 生成鍵查找，會誤判為「未運行」
	if botID := strings.TrimSpace(c.Query("bot_id")); botID != "" {
		if rtByID, idOK := symbolManagerProvider.GetByBotID(botID); idOK && rtByID != nil {
			if openingControlRuntimeMatchesQuery(rtByID, exchange, symbol, marketType) {
				return extractOpeningControllerFromRuntime(rtByID)
			}
		}
	}

	rtInterface, exists := symbolManagerProvider.GetEx(exchange, symbol, marketType)
	if !exists {
		// 不發送 HTTP 響應，僅返回 false，讓調用者決定如何處理（例如從配置文件讀取）
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
