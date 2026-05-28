package web

import (
	"net/http"
	"reflect"

	"quantmesh/position"

	"github.com/gin-gonic/gin"
)

// ========== 资金分配状態相关API ==========

// getAllocationStatus 獲取资金分配状態
// GET /api/allocation/status
func getAllocationStatus(c *gin.Context) {
	if symbolManagerProvider == nil {
		respondError(c, http.StatusServiceUnavailable, "error.symbol_manager_unavailable")
		return
	}

	// 獲取所有运行中的交易對
	runtimes := symbolManagerProvider.List()

	allStatuses := make([]map[string]interface{}, 0)

	for _, rt := range runtimes {
		// 使用反射獲取 AllocationManager
		rtVal := reflect.ValueOf(rt)
		if rtVal.Kind() == reflect.Ptr {
			rtVal = rtVal.Elem()
		}

		// 尝試獲取 PositionManager
		posManagerField := rtVal.FieldByName("PositionManager")
		if !posManagerField.IsValid() || posManagerField.IsNil() {
			continue
		}

		posManager := posManagerField.Interface()
		posManagerVal := reflect.ValueOf(posManager)
		if posManagerVal.Kind() == reflect.Ptr {
			posManagerVal = posManagerVal.Elem()
		}

		// 獲取 allocationManager
		allocManagerField := posManagerVal.FieldByName("allocationManager")
		if !allocManagerField.IsValid() || allocManagerField.IsNil() {
			continue
		}

		// 調用 GetAllStatuses 方法
		allocManager := allocManagerField.Interface()
		method := reflect.ValueOf(allocManager).MethodByName("GetAllStatuses")
		if !method.IsValid() {
			continue
		}

		results := method.Call(nil)
		if len(results) > 0 {
			statuses := results[0].Interface()
			if statusList, ok := statuses.([]*position.AllocationStatus); ok {
				for _, status := range statusList {
					allStatuses = append(allStatuses, map[string]interface{}{
						"exchange":         status.Exchange,
						"symbol":           status.Symbol,
						"max_amount":       status.MaxAmount,
						"used_amount":      status.UsedAmount,
						"available_amount": status.AvailableAmount,
						"usage_percentage": status.UsagePercentage,
					})
				}
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"allocations": allStatuses,
		"count":       len(allStatuses),
	})
}

// getAllocationStatusBySymbol 獲取指定交易對的资金分配状態
// GET /api/allocation/status/:exchange/:symbol
func getAllocationStatusBySymbol(c *gin.Context) {
	exchange := c.Param("exchange")
	symbol := c.Param("symbol")

	if exchange == "" || symbol == "" {
		respondError(c, http.StatusBadRequest, "error.missing_exchange_or_symbol")
		return
	}

	if symbolManagerProvider == nil {
		respondError(c, http.StatusServiceUnavailable, "error.symbol_manager_unavailable")
		return
	}

	// 獲取指定的运行時
	rtInterface, exists := symbolManagerProvider.Get(exchange, symbol)
	if !exists {
		respondError(c, http.StatusNotFound, "error.symbol_not_found")
		return
	}

	// 使用反射獲取 AllocationManager
	rtVal := reflect.ValueOf(rtInterface)
	if rtVal.Kind() == reflect.Ptr {
		rtVal = rtVal.Elem()
	}

	// 尝試獲取 PositionManager
	posManagerField := rtVal.FieldByName("PositionManager")
	if !posManagerField.IsValid() || posManagerField.IsNil() {
		respondError(c, http.StatusInternalServerError, "error.position_manager_unavailable")
		return
	}

	posManager := posManagerField.Interface()
	posManagerVal := reflect.ValueOf(posManager)
	if posManagerVal.Kind() == reflect.Ptr {
		posManagerVal = posManagerVal.Elem()
	}

	// 獲取 allocationManager
	allocManagerField := posManagerVal.FieldByName("allocationManager")
	if !allocManagerField.IsValid() || allocManagerField.IsNil() {
		respondError(c, http.StatusInternalServerError, "error.allocation_manager_unavailable")
		return
	}

	// 調用 GetStatus 方法
	allocManager := allocManagerField.Interface()
	method := reflect.ValueOf(allocManager).MethodByName("GetStatus")
	if !method.IsValid() {
		respondError(c, http.StatusInternalServerError, "error.method_unavailable")
		return
	}

	results := method.Call([]reflect.Value{
		reflect.ValueOf(exchange),
		reflect.ValueOf(symbol),
	})

	if len(results) > 0 && !results[0].IsNil() {
		status := results[0].Interface().(*position.AllocationStatus)
		c.JSON(http.StatusOK, gin.H{
			"exchange":         status.Exchange,
			"symbol":           status.Symbol,
			"max_amount":       status.MaxAmount,
			"used_amount":      status.UsedAmount,
			"available_amount": status.AvailableAmount,
			"usage_percentage": status.UsagePercentage,
		})
		return
	}

	respondError(c, http.StatusNotFound, "error.allocation_not_found")
}
