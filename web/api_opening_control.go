package web

import (
	"net/http"
	"reflect"

	"github.com/gin-gonic/gin"
	"quantmesh/config"
	"quantmesh/logger"
	"quantmesh/position"
)

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

	rtInterface, oc, ok := getOpeningControlRuntimeAndController(c, exchange, symbol)
	if !ok {
		return
	}

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

	// 持久化到配置文件
	if globalConfig != nil {
		cfg := globalConfig
		if cfg != nil {
			for i := range cfg.Trading.Symbols {
				sym := &cfg.Trading.Symbols[i]
				if sym.Exchange == exchange && sym.Symbol == symbol {
					sym.OpenPositionControl.MaxPositionValue = req.MaxPositionValue
					sym.OpenPositionControl.MaxPositionLayers = req.MaxPositionLayers
					sym.OpenPositionControl.ScheduleRules = req.ScheduleRules
					sym.OpenPositionControl.PeriodicRule = req.PeriodicRule
					if err := fileConfigManager.UpdateConfig(cfg); err != nil {
						logger.Warn("⚠️ [開倉管理] 配置持久化失敗: %v", err)
					}
					break
				}
			}
		}
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
	rtInterface, exists := symbolManagerProvider.GetEx(exchange, symbol, marketType)
	if !exists {
		respondError(c, http.StatusNotFound, "error.symbol_not_found")
		return nil, nil, false
	}

	rtVal := reflect.ValueOf(rtInterface)
	if rtVal.Kind() == reflect.Ptr {
		rtVal = rtVal.Elem()
	}

	ocField := rtVal.FieldByName("OpeningController")
	if ocField.IsValid() && !ocField.IsNil() {
		if o, _ := ocField.Interface().(*position.OpeningController); o != nil {
			oc = o
		}
	}

	return rtInterface, oc, true
}
