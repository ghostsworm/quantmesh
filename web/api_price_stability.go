package web

import (
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"quantmesh/logger"

	"github.com/gin-gonic/gin"
)

// ========== 价格稳定性分析 API ==========

// getPriceStability 获取价格稳定性分析
// GET /api/price/stability?symbol=BNTUSDT&hours=1
func getPriceStability(c *gin.Context) {
	symbol := c.Query("symbol")
	if symbol == "" {
		respondError(c, http.StatusBadRequest, "errors.missing_parameter",
			map[string]interface{}{"param": "symbol"})
		return
	}

	// 解析时间范围（默认1小时）
	hours := 1
	if hoursStr := c.Query("hours"); hoursStr != "" {
		if h, err := strconv.Atoi(hoursStr); err == nil && h > 0 {
			hours = h
		}
	}

	// 获取存储服务
	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		storageProv = storageServiceProvider
	}
	if storageProv == nil {
		respondError(c, http.StatusServiceUnavailable, "errors.service_unavailable")
		return
	}

	storage := storageProv.GetStorage()
	if storage == nil {
		respondError(c, http.StatusServiceUnavailable, "errors.service_unavailable")
		return
	}

	// 计算时间范围
	endTime := time.Now().UTC()
	startTime := endTime.Add(-time.Duration(hours) * time.Hour)

	// 尝试从资产配置中获取 asset_type
	assetType := ""
	if globalConfig != nil {
		cfg := globalConfig
		if cfg != nil {
			for _, asset := range cfg.NewsMonitor.Assets {
				if asset.Symbol == symbol {
					assetType = asset.AssetType
					break
				}
			}
		}
	}

	// 如果没有找到 asset_type，尝试推断
	if assetType == "" {
		// 根据符号推断资产类型
		if strings.Contains(symbol, "PAXG") {
			assetType = "commodity_gold"
		} else if strings.Contains(symbol, "BTC") {
			assetType = "crypto_btc"
		} else if strings.Contains(symbol, "ETH") {
			assetType = "crypto_eth"
		} else if strings.Contains(symbol, "BNB") {
			assetType = "crypto_bnb"
		} else {
			assetType = "crypto_other"
		}
	}

	// 获取价格历史
	history, err := storage.GetPriceHistory(assetType, symbol, startTime, endTime, 1000)
	if err != nil {
		logger.Warn("⚠️ 获取价格历史失败: %v", err)
		respondError(c, http.StatusInternalServerError, "errors.internal_error", err)
		return
	}

	if len(history) < 2 {
		c.JSON(http.StatusOK, gin.H{
			"symbol":      symbol,
			"hours":       hours,
			"data_points": len(history),
			"message":     "数据点不足，无法计算稳定性",
		})
		return
	}

	// 计算价格统计
	var prices []float64
	var minPrice, maxPrice float64 = history[0].Price, history[0].Price
	var sum float64

	for _, h := range history {
		if h.Price > 0 {
			prices = append(prices, h.Price)
			sum += h.Price
			if h.Price < minPrice {
				minPrice = h.Price
			}
			if h.Price > maxPrice {
				maxPrice = h.Price
			}
		}
	}

	if len(prices) < 2 {
		c.JSON(http.StatusOK, gin.H{
			"symbol":      symbol,
			"hours":       hours,
			"data_points": len(prices),
			"message":     "有效数据点不足",
		})
		return
	}

	// 计算平均价格
	avgPrice := sum / float64(len(prices))

	// 计算标准差和波动率
	var variance float64
	for _, p := range prices {
		diff := p - avgPrice
		variance += diff * diff
	}
	variance /= float64(len(prices))
	stdDev := math.Sqrt(variance)
	volatility := (stdDev / avgPrice) * 100

	// 计算价格范围
	priceRange := maxPrice - minPrice
	priceRangePercent := (priceRange / avgPrice) * 100

	// 计算收益率序列的标准差（更准确的波动率）
	var returns []float64
	for i := 1; i < len(prices); i++ {
		if prices[i-1] > 0 {
			ret := (prices[i] - prices[i-1]) / prices[i-1]
			returns = append(returns, ret)
		}
	}

	var returnStdDev float64
	if len(returns) > 0 {
		var returnSum float64
		for _, r := range returns {
			returnSum += r
		}
		returnMean := returnSum / float64(len(returns))

		var returnVariance float64
		for _, r := range returns {
			returnVariance += math.Pow(r-returnMean, 2)
		}
		returnVariance /= float64(len(returns))
		returnStdDev = math.Sqrt(returnVariance) * 100 // 转换为百分比
	}

	// 判断稳定性等级
	stabilityLevel := "high"
	if volatility > 2.0 {
		stabilityLevel = "low"
	} else if volatility > 0.5 {
		stabilityLevel = "medium"
	}

	c.JSON(http.StatusOK, gin.H{
		"symbol":              symbol,
		"hours":               hours,
		"data_points":         len(prices),
		"current_price":       prices[len(prices)-1],
		"average_price":       avgPrice,
		"min_price":           minPrice,
		"max_price":           maxPrice,
		"price_range":         priceRange,
		"price_range_percent": priceRangePercent,
		"volatility":          volatility,
		"return_volatility":   returnStdDev,
		"std_dev":             stdDev,
		"stability_level":     stabilityLevel,
		"start_time":          startTime.Format(time.RFC3339),
		"end_time":            endTime.Format(time.RFC3339),
	})
}
