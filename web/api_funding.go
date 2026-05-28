package web

import (
	"context"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// ========== 资金费率相关API ==========

var (
	// 资金费率監控提供者（需要從main.go注入）
	fundingMonitorProvider FundingMonitorProvider
)

// FundingMonitorProvider 资金费率監控提供者接口
type FundingMonitorProvider interface {
	GetCurrentFundingRates() (map[string]float64, error)
}

// SetFundingMonitorProvider 設置资金费率監控提供者
func SetFundingMonitorProvider(provider FundingMonitorProvider) {
	fundingMonitorProvider = provider
}

// getFundingRate 獲取當前资金费率
// GET /api/funding/current
func getFundingRate(c *gin.Context) {
	fundingProv := PickFundingProvider(c)
	storageProv := PickStorageProvider(c)
	status := pickStatus(c)
	exchangeProv := pickExchangeProvider(c)
	rates := make(map[string]interface{})

	// 從監控服務獲取當前资金费率
	if fundingProv != nil {
		currentRates, err := fundingProv.GetCurrentFundingRates()
		if err == nil {
			for symbol, rate := range currentRates {
				rates[symbol] = map[string]interface{}{
					"rate":      rate,
					"rate_pct":  rate * 100, // 轉换為百分比
					"timestamp": time.Now(),
				}
			}
		}
	}

	// 從數據库獲取最新記錄
	exchangeName := ""
	if status != nil {
		exchangeName = status.Exchange
	}

	// 獲取主流交易對的最新资金费率
	symbols := []string{"BTCUSDT", "ETHUSDT", "BNBUSDT", "SOLUSDT", "XRPUSDT"}
	if storageProv != nil {
		storage := storageProv.GetStorage()
		if storage != nil {
			for _, symbol := range symbols {
				latestRate, err := storage.GetLatestFundingRate(symbol, exchangeName)
				if err == nil {
					// 如果監控服務没有提供，使用數據库中的值
					if _, exists := rates[symbol]; !exists {
						rates[symbol] = map[string]interface{}{
							"rate":      latestRate,
							"rate_pct":  latestRate * 100,
							"timestamp": time.Now(),
						}
					}
				}
			}
		}
	}

	// 如果某些交易對没有數據，從交易所API實時獲取缺失的數據
	if exchangeProv != nil {
		ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
		defer cancel()

		for _, symbol := range symbols {
			// 如果該交易對已經有數據，跳過
			if _, exists := rates[symbol]; exists {
				continue
			}

			// 從交易所API實時獲取
			rate, err := exchangeProv.GetFundingRate(ctx, symbol)
			if err == nil {
				rates[symbol] = map[string]interface{}{
					"rate":      rate,
					"rate_pct":  rate * 100,
					"timestamp": time.Now(),
				}

				// 同時保存到數據库（如果存儲服務可用）
				if storageProv != nil {
					storage := storageProv.GetStorage()
					if storage != nil {
						_ = storage.SaveFundingRate(symbol, exchangeName, rate, time.Now().UTC())
					}
				}
			}
		}
		cancel()
	}

	c.JSON(http.StatusOK, gin.H{"rates": rates})
}

// getFundingRateHistory 獲取资金费率历史
// GET /api/funding/history
// 查詢参數：
//   - symbol: 交易對（可選）
//   - exchange: 交易所（可選，不傳則返回所有交易所）
//   - limit: 返回數量（預設 100）
func getFundingRateHistory(c *gin.Context) {
	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusOK, gin.H{"history": []interface{}{}})
		return
	}

	storage := storageProv.GetStorage()
	if storage == nil {
		c.JSON(http.StatusOK, gin.H{"history": []interface{}{}})
		return
	}

	// 解析查詢参數
	symbol := c.Query("symbol")
	exchangeParam := c.Query("exchange")
	limitStr := c.DefaultQuery("limit", "100")
	limit := 100
	if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
		limit = l
		if limit > 1000 {
			limit = 1000 // 限制最大數量
		}
	}

	// 交易所：exchange=all 表示所有交易所，不傳則用當前連接的交易所
	exchangeName := ""
	if exchangeParam == "all" {
		exchangeName = ""
	} else if exchangeParam != "" {
		exchangeName = exchangeParam
	} else if currentStatus != nil {
		exchangeName = currentStatus.Exchange
	}

	// 查詢历史數據
	history, err := storage.GetFundingRateHistory(symbol, exchangeName, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 轉换為API响应格式
	response := make([]map[string]interface{}, len(history))
	for i, fr := range history {
		response[i] = map[string]interface{}{
			"id":         fr.ID,
			"symbol":     fr.Symbol,
			"exchange":   fr.Exchange,
			"rate":       fr.Rate,
			"rate_pct":   fr.Rate * 100, // 轉换為百分比
			"timestamp":  fr.Timestamp,
			"created_at": fr.CreatedAt,
		}
	}

	c.JSON(http.StatusOK, gin.H{"history": response})
}
