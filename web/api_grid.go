package web

import (
	"net/http"
	"reflect"
	"strconv"

	"github.com/gin-gonic/gin"
	"quantmesh/logger"
	"quantmesh/position"
)

// postGridShiftUp 網格上移
// POST /api/grid/shift-up?exchange=xxx&symbol=xxx&step=可选
func postGridShiftUp(c *gin.Context) {
	exchange := c.Query("exchange")
	symbol := c.Query("symbol")
	if exchange == "" || symbol == "" {
		respondError(c, http.StatusBadRequest, "error.exchange_symbol_required")
		return
	}
	step := parseFloatQuery(c, "step", 0)

	spm, ok := getGridSPM(c, exchange, symbol)
	if !ok {
		return
	}
	spm.ShiftGrid("up", step)
	logger.Info("📈 [網格] 上移 [%s:%s] step=%.2f", exchange, symbol, step)
	c.JSON(http.StatusOK, gin.H{"message": "網格已上移", "direction": "up"})
}

// postGridShiftDown 網格下移
// POST /api/grid/shift-down?exchange=xxx&symbol=xxx&step=可选
func postGridShiftDown(c *gin.Context) {
	exchange := c.Query("exchange")
	symbol := c.Query("symbol")
	if exchange == "" || symbol == "" {
		respondError(c, http.StatusBadRequest, "error.exchange_symbol_required")
		return
	}
	step := parseFloatQuery(c, "step", 0)

	spm, ok := getGridSPM(c, exchange, symbol)
	if !ok {
		return
	}
	spm.ShiftGrid("down", step)
	logger.Info("📉 [網格] 下移 [%s:%s] step=%.2f", exchange, symbol, step)
	c.JSON(http.StatusOK, gin.H{"message": "網格已下移", "direction": "down"})
}

// getGridSPM 獲取指定交易對的 SuperPositionManager
func getGridSPM(c *gin.Context, exchange, symbol string) (*position.SuperPositionManager, bool) {
	if symbolManagerProvider == nil {
		respondError(c, http.StatusServiceUnavailable, "error.symbol_manager_unavailable")
		return nil, false
	}
	marketType := c.DefaultQuery("market_type", "futures")
	if marketType != "spot" && marketType != "futures" {
		marketType = "futures"
	}
	rtInterface, exists := symbolManagerProvider.GetEx(exchange, symbol, marketType)
	if !exists {
		respondError(c, http.StatusNotFound, "error.symbol_not_found")
		return nil, false
	}
	rtVal := reflect.ValueOf(rtInterface)
	if rtVal.Kind() == reflect.Ptr {
		rtVal = rtVal.Elem()
	}
	spmField := rtVal.FieldByName("SuperPositionManager")
	if !spmField.IsValid() || spmField.IsNil() {
		respondError(c, http.StatusInternalServerError, "error.position_manager_unavailable")
		return nil, false
	}
	spm, ok := spmField.Interface().(*position.SuperPositionManager)
	if !ok || spm == nil {
		respondError(c, http.StatusInternalServerError, "error.position_manager_unavailable")
		return nil, false
	}
	return spm, true
}

func parseFloatQuery(c *gin.Context, key string, defaultVal float64) float64 {
	s := c.Query(key)
	if s == "" {
		return defaultVal
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return defaultVal
	}
	return f
}
