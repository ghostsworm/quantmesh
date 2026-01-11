package web

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"quantmesh/ai"
	"quantmesh/exchange"
	qmi18n "quantmesh/i18n"
	"quantmesh/logger"
	"quantmesh/position"
	"quantmesh/storage"
	"quantmesh/utils"
)

// respondError 返回翻译后的错误响应
func respondError(c *gin.Context, status int, messageKey string, args ...interface{}) {
	lang := GetLanguage(c)

	var data map[string]interface{}
	var errObj error

	// 解析参数
	for _, arg := range args {
		if err, ok := arg.(error); ok {
			errObj = err
		} else if m, ok := arg.(map[string]interface{}); ok {
			data = m
		}
	}

	// 翻译错误消息
	message := qmi18n.TWithLang(lang, messageKey, data)

	// 如果有实际的错误对象，添加详细信息（仅在开发模式）
	if errObj != nil && status >= 500 {
		// 在生产环境可能需要隐藏详细错误信息
		message = fmt.Sprintf("%s: %v", message, errObj)
	}

	c.JSON(status, gin.H{"error": message})
}

// SystemStatus 系统状态
type SystemStatus struct {
	Running       bool    `json:"running"`
	Exchange      string  `json:"exchange"`
	Symbol        string  `json:"symbol"`
	CurrentPrice  float64 `json:"current_price"`
	TotalPnL      float64 `json:"total_pnl"`
	TotalTrades   int     `json:"total_trades"`
	RiskTriggered bool    `json:"risk_triggered"`
	Uptime        int64   `json:"uptime"` // 运行时间（秒）
}

var (
	// 全局状态（需要从 main.go 注入）
	currentStatus *SystemStatus
	// 多交易对状态（key: exchange:symbol）
	statusBySymbol   = make(map[string]*SystemStatus)
	defaultSymbolKey string
	// 保护 statusBySymbol 的读写锁
	statusMu sync.RWMutex
	// 版本号（需要从 main.go 注入）
	appVersion string
)

// SymbolScopedProviders 组合一个交易对的所有依赖
type SymbolScopedProviders struct {
	Status   *SystemStatus
	Price    PriceProvider
	Exchange ExchangeProvider
	Position PositionManagerProvider
	Risk     RiskMonitorProvider
	Storage  StorageServiceProvider
	Funding  FundingMonitorProvider
}

func makeSymbolKey(exchange, symbol string) string {
	return strings.ToLower(fmt.Sprintf("%s:%s", exchange, symbol))
}

// SetStatusProvider 设置状态提供者
func SetStatusProvider(status *SystemStatus) {
	currentStatus = status
}

// SetVersion 设置版本号
func SetVersion(version string) {
	appVersion = version
}

// RegisterSymbolProviders 注册单个交易对的提供者集合
func RegisterSymbolProviders(exchange, symbol string, providers *SymbolScopedProviders) {
	if providers == nil {
		return
	}
	key := makeSymbolKey(exchange, symbol)

	logger.Info("[DEBUG] RegisterSymbolProviders - registering key=%s, hasPosition=%v, hasPrice=%v",
		key, providers.Position != nil, providers.Price != nil)

	// 使用写锁保护并发写入
	statusMu.Lock()
	statusBySymbol[key] = providers.Status
	statusMu.Unlock()

	providersMu.Lock()
	if providers.Price != nil {
		priceProviders[key] = providers.Price
		logger.Info("[DEBUG] RegisterSymbolProviders - registered price provider for key=%s", key)
	}
	if providers.Exchange != nil {
		exchangeProviders[key] = providers.Exchange
	}
	if providers.Position != nil {
		positionProviders[key] = providers.Position
		logger.Info("[DEBUG] RegisterSymbolProviders - registered position provider for key=%s", key)
	}
	if providers.Risk != nil {
		riskProviders[key] = providers.Risk
	}
	if providers.Storage != nil {
		storageProviders[key] = providers.Storage
	}
	if providers.Funding != nil {
		fundingProviders[key] = providers.Funding
	}
	providersMu.Unlock()
}

// RegisterFundingProvider 单独注册资金费率提供者
func RegisterFundingProvider(exchange, symbol string, provider FundingMonitorProvider) {
	if provider == nil {
		return
	}
	key := makeSymbolKey(exchange, symbol)

	// 使用写锁保护并发写入
	providersMu.Lock()
	fundingProviders[key] = provider
	providersMu.Unlock()
}

// SetDefaultSymbolKey 设置默认交易对（兼容旧接口）
func SetDefaultSymbolKey(exchange, symbol string) {
	defaultSymbolKey = makeSymbolKey(exchange, symbol)
}

// resolveSymbolKey 根据查询参数获取 key
func resolveSymbolKey(c *gin.Context) string {
	ex := c.Query("exchange")
	sym := c.Query("symbol")
	if ex != "" && sym != "" {
		key := makeSymbolKey(ex, sym)
		logger.Info("[DEBUG] resolveSymbolKey - ex=%s, sym=%s, key=%s", ex, sym, key)
		return key
	}
	logger.Info("[DEBUG] resolveSymbolKey - no params, returning defaultSymbolKey=%s", defaultSymbolKey)
	return defaultSymbolKey
}

// === Provider 映射 ===
var (
	priceProviders    = make(map[string]PriceProvider)
	exchangeProviders = make(map[string]ExchangeProvider)
	positionProviders = make(map[string]PositionManagerProvider)
	riskProviders     = make(map[string]RiskMonitorProvider)
	storageProviders  = make(map[string]StorageServiceProvider)
	fundingProviders  = make(map[string]FundingMonitorProvider)
	// 保护所有 provider 映射的读写锁
	providersMu sync.RWMutex
)

func pickStatus(c *gin.Context) *SystemStatus {
	if key := resolveSymbolKey(c); key != "" {
		statusMu.RLock()
		st, ok := statusBySymbol[key]
		statusMu.RUnlock()
		if ok && st != nil {
			return st
		}
	}
	return currentStatus
}

func pickPriceProvider(c *gin.Context) PriceProvider {
	if key := resolveSymbolKey(c); key != "" {
		providersMu.RLock()
		p, ok := priceProviders[key]
		providersMu.RUnlock()
		if ok && p != nil {
			logger.Info("[DEBUG] pickPriceProvider - found provider for key=%s", key)
			return p
		}
		logger.Warn("⚠️ [pickPriceProvider] no provider found for key=%s, falling back to default", key)
	}
	logger.Info("[DEBUG] pickPriceProvider - using default priceProvider")
	return priceProvider
}

func pickExchangeProvider(c *gin.Context) ExchangeProvider {
	if key := resolveSymbolKey(c); key != "" {
		providersMu.RLock()
		p, ok := exchangeProviders[key]
		providersMu.RUnlock()
		if ok && p != nil {
			return p
		}
	}
	return exchangeProvider
}

func pickPositionProvider(c *gin.Context) PositionManagerProvider {
	key := resolveSymbolKey(c)
	logger.Info("[DEBUG] pickPositionProvider - resolvedKey=%s", key)

	if key != "" {
		providersMu.RLock()
		p, ok := positionProviders[key]
		providersMu.RUnlock()

		logger.Info("[DEBUG] pickPositionProvider - found in map: %v, provider!=nil: %v", ok, p != nil)

		if ok && p != nil {
			return p
		}
	}

	logger.Info("[DEBUG] pickPositionProvider - returning default provider")
	return positionManagerProvider
}

func pickRiskProvider(c *gin.Context) RiskMonitorProvider {
	if key := resolveSymbolKey(c); key != "" {
		providersMu.RLock()
		p, ok := riskProviders[key]
		providersMu.RUnlock()
		if ok && p != nil {
			return p
		}
	}
	return riskMonitorProvider
}

func pickStorageProvider(c *gin.Context) StorageServiceProvider {
	if key := resolveSymbolKey(c); key != "" {
		providersMu.RLock()
		p, ok := storageProviders[key]
		providersMu.RUnlock()
		if ok && p != nil {
			return p
		}
	}
	return storageServiceProvider
}

func pickFundingProvider(c *gin.Context) FundingMonitorProvider {
	if key := resolveSymbolKey(c); key != "" {
		providersMu.RLock()
		p, ok := fundingProviders[key]
		providersMu.RUnlock()
		if ok && p != nil {
			return p
		}
	}
	return fundingMonitorProvider
}

func getStatus(c *gin.Context) {
	status := pickStatus(c)
	if status == nil {
		c.JSON(http.StatusOK, &SystemStatus{
			Running: false,
		})
		return
	}
	c.JSON(http.StatusOK, status)
}

// SymbolItem 用于返回可用的交易所/交易对列表
type SymbolItem struct {
	Exchange     string  `json:"exchange"`
	Symbol       string  `json:"symbol"`
	IsActive     bool    `json:"is_active"`
	CurrentPrice float64 `json:"current_price"`
}

// getSymbols 返回可用的交易对列表
func getSymbols(c *gin.Context) {
	// 使用 map 来去重，key 为 exchange:symbol
	symbolMap := make(map[string]*SymbolItem)
	activeList := make([]SymbolItem, 0)
	inactiveList := make([]SymbolItem, 0)

	// 首先从配置文件中读取所有配置的交易对
	if configManager != nil {
		cfg, err := configManager.GetConfig()
		if err == nil && cfg != nil {
			// 从交易对配置中读取
			for _, sym := range cfg.Trading.Symbols {
				if sym.Symbol == "" {
					continue
				}
				exchange := sym.Exchange
				if exchange == "" {
					exchange = cfg.App.CurrentExchange
				}
				if exchange == "" {
					continue
				}
				key := strings.ToLower(fmt.Sprintf("%s:%s", exchange, sym.Symbol))
				if _, exists := symbolMap[key]; !exists {
					symbolMap[key] = &SymbolItem{
						Exchange:     strings.ToLower(exchange),
						Symbol:       sym.Symbol,
						IsActive:     false, // 默认未运行，后面会更新
						CurrentPrice: 0,
					}
				}
			}
			// 如果只有单交易对配置
			if len(cfg.Trading.Symbols) == 0 && cfg.Trading.Symbol != "" {
				exchange := cfg.App.CurrentExchange
				if exchange != "" {
					key := strings.ToLower(fmt.Sprintf("%s:%s", exchange, cfg.Trading.Symbol))
					if _, exists := symbolMap[key]; !exists {
						symbolMap[key] = &SymbolItem{
							Exchange:     strings.ToLower(exchange),
							Symbol:       cfg.Trading.Symbol,
							IsActive:     false,
							CurrentPrice: 0,
						}
					}
				}
			}
		}
	}

	// 然后从运行状态中更新（确保正在运行的交易对状态正确）
	statusMu.RLock()
	for _, st := range statusBySymbol {
		if st == nil {
			continue
		}
		key := strings.ToLower(fmt.Sprintf("%s:%s", st.Exchange, st.Symbol))
		if item, exists := symbolMap[key]; exists {
			// 更新已存在的交易对状态
			item.IsActive = st.Running
			item.CurrentPrice = st.CurrentPrice
		} else {
			// 添加新的运行中的交易对
			symbolMap[key] = &SymbolItem{
				Exchange:     strings.ToLower(st.Exchange),
				Symbol:       st.Symbol,
				IsActive:     st.Running,
				CurrentPrice: st.CurrentPrice,
			}
		}
	}
	statusMu.RUnlock()

	// 向后兼容：如果没有多交易对数据，使用旧的单交易对状态
	if len(symbolMap) == 0 && currentStatus != nil {
		key := strings.ToLower(fmt.Sprintf("%s:%s", currentStatus.Exchange, currentStatus.Symbol))
		symbolMap[key] = &SymbolItem{
			Exchange:     strings.ToLower(currentStatus.Exchange),
			Symbol:       currentStatus.Symbol,
			IsActive:     currentStatus.Running,
			CurrentPrice: currentStatus.CurrentPrice,
		}
	}

	// 转换为列表并分组
	for _, item := range symbolMap {
		if item.IsActive {
			activeList = append(activeList, *item)
		} else {
			inactiveList = append(inactiveList, *item)
		}
	}

	// 活跃的交易对排在前面
	list := make([]SymbolItem, 0)
	list = append(list, activeList...)
	list = append(list, inactiveList...)

	c.JSON(http.StatusOK, gin.H{"symbols": list})
}

// getVersion 返回版本号（不需要认证）
func getVersion(c *gin.Context) {
	version := appVersion
	if version == "" {
		version = "unknown"
	}
	c.JSON(http.StatusOK, gin.H{"version": version})
}

// getExchanges 返回所有配置的交易所列表
func getExchanges(c *gin.Context) {
	exchangeSet := make(map[string]bool)

	// 首先从配置文件中读取所有配置的交易所
	if configManager != nil {
		cfg, err := configManager.GetConfig()
		if err == nil && cfg != nil {
			// 从配置的 exchanges 中读取
			for ex := range cfg.Exchanges {
				if ex != "" {
					exchangeSet[strings.ToLower(ex)] = true
				}
			}
			// 从交易对配置中读取交易所
			for _, sym := range cfg.Trading.Symbols {
				if sym.Exchange != "" {
					exchangeSet[strings.ToLower(sym.Exchange)] = true
				} else if cfg.App.CurrentExchange != "" {
					exchangeSet[strings.ToLower(cfg.App.CurrentExchange)] = true
				}
			}
			// 如果只有单交易对配置
			if len(cfg.Trading.Symbols) == 0 && cfg.Trading.Symbol != "" {
				if cfg.App.CurrentExchange != "" {
					exchangeSet[strings.ToLower(cfg.App.CurrentExchange)] = true
				}
			}
		}
	}

	// 然后从运行状态中读取（确保正在运行的交易所也在列表中）
	statusMu.RLock()
	for _, st := range statusBySymbol {
		if st == nil {
			continue
		}
		exchangeSet[strings.ToLower(st.Exchange)] = true
	}
	statusMu.RUnlock()

	// 向后兼容
	if len(exchangeSet) == 0 && currentStatus != nil {
		exchangeSet[strings.ToLower(currentStatus.Exchange)] = true
	}

	exchanges := make([]string, 0, len(exchangeSet))
	for ex := range exchangeSet {
		exchanges = append(exchanges, ex)
	}

	// 排序交易所列表（可选，但有助于一致性）
	sort.Strings(exchanges)

	c.JSON(http.StatusOK, gin.H{"exchanges": exchanges})
}

// PositionSummary 持仓汇总信息
type PositionSummary struct {
	TotalQuantity float64        `json:"total_quantity"` // 总持仓数量
	TotalValue    float64        `json:"total_value"`    // 总持仓价值（当前价格 * 数量）
	PositionCount int            `json:"position_count"` // 持仓槽位数
	AveragePrice  float64        `json:"average_price"`  // 平均持仓价格
	CurrentPrice  float64        `json:"current_price"`  // 当前市场价格
	UnrealizedPnL float64        `json:"unrealized_pnl"` // 未实现盈亏
	PnlPercentage float64        `json:"pnl_percentage"` // 盈亏百分比
	Positions     []PositionInfo `json:"positions"`      // 持仓列表
}

// PositionInfo 单个持仓信息
type PositionInfo struct {
	Price         float64 `json:"price"`          // 持仓价格
	Quantity      float64 `json:"quantity"`       // 持仓数量
	Value         float64 `json:"value"`          // 持仓价值
	UnrealizedPnL float64 `json:"unrealized_pnl"` // 未实现盈亏
}

var (
	// 价格提供者（需要从main.go注入）
	priceProvider PriceProvider
)

// PriceProvider 价格提供者接口
type PriceProvider interface {
	GetLastPrice() float64
}

// SetPriceProvider 设置价格提供者
func SetPriceProvider(provider PriceProvider) {
	priceProvider = provider
}

var (
	// 交易所提供者（需要从main.go注入）
	exchangeProvider ExchangeProvider
)

// ExchangeProvider 交易所提供者接口
type ExchangeProvider interface {
	GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*exchange.Candle, error)
}

// SetExchangeProvider 设置交易所提供者
func SetExchangeProvider(provider ExchangeProvider) {
	exchangeProvider = provider
}

// getPositions 获取持仓列表（从槽位数据筛选）
func getPositions(c *gin.Context) {
	// 调试：记录接收到的参数
	exchange := c.Query("exchange")
	symbol := c.Query("symbol")
	resolvedKey := resolveSymbolKey(c)
	logger.Info("[DEBUG] getPositions called - exchange=%s, symbol=%s, resolvedKey=%s", exchange, symbol, resolvedKey)

	pmProvider := pickPositionProvider(c)
	priceProv := pickPriceProvider(c)

	if pmProvider == nil {
		c.JSON(http.StatusOK, gin.H{"positions": []interface{}{}})
		return
	}

	slots := pmProvider.GetAllSlots()
	logger.Info("[DEBUG] getPositions - got %d slots for key=%s", len(slots), resolvedKey)
	var positions []PositionInfo
	currentPrice := 0.0
	if priceProv != nil {
		currentPrice = priceProv.GetLastPrice()
		logger.Info("[DEBUG] getPositions - [%s:%s] resolvedKey=%s, priceProvider!=nil, currentPrice=%.2f",
			exchange, symbol, resolvedKey, currentPrice)
	} else {
		logger.Warn("⚠️ [getPositions] [%s:%s] resolvedKey=%s, priceProvider is nil!",
			exchange, symbol, resolvedKey)
	}

	totalQuantity := 0.0
	totalValue := 0.0
	positionCount := 0

	// 筛选有持仓的槽位
	for _, slot := range slots {
		// 🔥 添加价格验证：确保槽位价格有效（大于0且合理）
		if slot.PositionStatus == "FILLED" && slot.PositionQty > 0.000001 && slot.Price > 0.000001 {
			positionCount++
			totalQuantity += slot.PositionQty

			// 计算持仓价值（使用当前价格）
			value := slot.PositionQty * currentPrice
			if currentPrice == 0 {
				// 如果当前价格不可用，使用持仓价格
				value = slot.PositionQty * slot.Price
			}
			totalValue += value

			// 计算未实现盈亏
			unrealizedPnL := 0.0
			if currentPrice > 0 && slot.Price > 0 {
				// 🔥 添加价格合理性检查：如果当前价格相对于持仓价格偏差过大，可能是价格异常
				priceDeviation := (currentPrice - slot.Price) / slot.Price

				// 检查是否是单位问题（比如当前价格是持仓价格的100倍或0.01倍）
				priceRatio := currentPrice / slot.Price
				adjustedCurrentPrice := currentPrice
				if priceRatio > 50 {
					// 当前价格可能是持仓价格的100倍，尝试除以100
					adjustedPrice := currentPrice / 100
					if math.Abs(adjustedPrice-slot.Price)/slot.Price < 0.1 {
						logger.Warn("⚠️ [getPositions] [%s:%s] 检测到价格单位问题（当前价格可能是持仓价格的100倍），已自动修正: %.2f -> %.2f",
							exchange, symbol, currentPrice, adjustedPrice)
						adjustedCurrentPrice = adjustedPrice
					}
				} else if priceRatio < 0.02 {
					// 当前价格可能是持仓价格的0.01倍，尝试乘以100
					adjustedPrice := currentPrice * 100
					if math.Abs(adjustedPrice-slot.Price)/slot.Price < 0.1 {
						logger.Warn("⚠️ [getPositions] [%s:%s] 检测到价格单位问题（当前价格可能是持仓价格的0.01倍），已自动修正: %.2f -> %.2f",
							exchange, symbol, currentPrice, adjustedPrice)
						adjustedCurrentPrice = adjustedPrice
					}
				}

				// 重新计算价格偏差
				priceDeviation = (adjustedCurrentPrice - slot.Price) / slot.Price
				if priceDeviation > 0.5 || priceDeviation < -0.5 {
					// 价格偏差仍然过大，使用持仓价格（未实现盈亏为0）
					logger.Warn("⚠️ [getPositions] [%s:%s] 价格偏差过大，使用持仓价格计算（未实现盈亏设为0）: currentPrice=%.2f, slotPrice=%.2f, 偏差=%.2f%%, resolvedKey=%s",
						exchange, symbol, adjustedCurrentPrice, slot.Price, priceDeviation*100, resolvedKey)
					adjustedCurrentPrice = slot.Price
				}

				unrealizedPnL = (adjustedCurrentPrice - slot.Price) * slot.PositionQty
			}

			positions = append(positions, PositionInfo{
				Price:         slot.Price,
				Quantity:      slot.PositionQty,
				Value:         value,
				UnrealizedPnL: unrealizedPnL,
			})
		}
	}

	// 计算平均持仓价格
	averagePrice := 0.0
	if totalQuantity > 0 {
		totalCost := 0.0
		for _, pos := range positions {
			totalCost += pos.Price * pos.Quantity
		}
		averagePrice = totalCost / totalQuantity
	}

	// 计算总未实现盈亏
	totalUnrealizedPnL := 0.0
	if currentPrice > 0 {
		for _, pos := range positions {
			totalUnrealizedPnL += pos.UnrealizedPnL
		}
	}

	// 计算总持仓成本
	totalCost := 0.0
	for _, pos := range positions {
		totalCost += pos.Price * pos.Quantity
	}

	// 计算亏损率（相对于持仓成本的百分比）
	pnlPercentage := 0.0
	if totalCost > 0 {
		pnlPercentage = (totalUnrealizedPnL / totalCost) * 100.0
	}

	summary := PositionSummary{
		TotalQuantity: totalQuantity,
		TotalValue:    totalValue,
		PositionCount: positionCount,
		AveragePrice:  averagePrice,
		CurrentPrice:  currentPrice,
		UnrealizedPnL: totalUnrealizedPnL,
		PnlPercentage: pnlPercentage,
		Positions:     positions,
	}

	// 调试：在响应中包含请求的交易对信息
	c.JSON(http.StatusOK, gin.H{
		"summary": summary,
		"_debug": gin.H{
			"exchange":    exchange,
			"symbol":      symbol,
			"resolvedKey": resolvedKey,
			"slotCount":   len(slots),
		},
	})
}

// getPositionsSummary 获取持仓汇总
// GET /api/positions/summary
func getPositionsSummary(c *gin.Context) {
	pmProvider := pickPositionProvider(c)
	priceProv := pickPriceProvider(c)

	if pmProvider == nil {
		c.JSON(http.StatusOK, gin.H{
			"total_quantity": 0,
			"total_value":    0,
			"position_count": 0,
			"average_price":  0,
			"current_price":  0,
			"unrealized_pnl": 0,
			"pnl_percentage": 0,
		})
		return
	}

	slots := pmProvider.GetAllSlots()
	currentPrice := 0.0
	if priceProv != nil {
		currentPrice = priceProv.GetLastPrice()
	}

	totalQuantity := 0.0
	totalValue := 0.0
	positionCount := 0
	totalCost := 0.0

	// 筛选有持仓的槽位
	for _, slot := range slots {
		// 🔥 添加价格验证：确保槽位价格有效（大于0且合理）
		if slot.PositionStatus == "FILLED" && slot.PositionQty > 0.000001 && slot.Price > 0.000001 {
			positionCount++
			totalQuantity += slot.PositionQty
			totalCost += slot.Price * slot.PositionQty

			// 计算持仓价值（使用当前价格）
			if currentPrice > 0 {
				totalValue += slot.PositionQty * currentPrice
			} else {
				// 如果当前价格不可用，使用持仓价格
				totalValue += slot.PositionQty * slot.Price
			}
		}
	}

	// 计算平均持仓价格
	averagePrice := 0.0
	if totalQuantity > 0 {
		averagePrice = totalCost / totalQuantity
	}

	// 计算总未实现盈亏
	unrealizedPnL := 0.0
	if currentPrice > 0 && totalQuantity > 0 && averagePrice > 0 {
		// 🔥 添加价格合理性检查：如果当前价格相对于平均价格偏差过大（超过50%），可能是价格异常
		priceDeviation := (currentPrice - averagePrice) / averagePrice

		// 检查是否是单位问题（比如当前价格是平均价格的100倍或0.01倍）
		priceRatio := currentPrice / averagePrice
		if priceRatio > 50 || priceRatio < 0.02 {
			// 可能是单位问题，尝试修正
			if priceRatio > 50 {
				// 当前价格可能是平均价格的100倍，尝试除以100
				adjustedPrice := currentPrice / 100
				if math.Abs(adjustedPrice-averagePrice)/averagePrice < 0.1 {
					logger.Warn("⚠️ [getPositionsSummary] 检测到价格单位问题（当前价格可能是平均价格的100倍），已自动修正: %.2f -> %.2f",
						currentPrice, adjustedPrice)
					currentPrice = adjustedPrice
				}
			} else if priceRatio < 0.02 {
				// 当前价格可能是平均价格的0.01倍，尝试乘以100
				adjustedPrice := currentPrice * 100
				if math.Abs(adjustedPrice-averagePrice)/averagePrice < 0.1 {
					logger.Warn("⚠️ [getPositionsSummary] 检测到价格单位问题（当前价格可能是平均价格的0.01倍），已自动修正: %.2f -> %.2f",
						currentPrice, adjustedPrice)
					currentPrice = adjustedPrice
				}
			}
		}

		// 重新计算价格偏差
		priceDeviation = (currentPrice - averagePrice) / averagePrice
		if priceDeviation > 0.5 || priceDeviation < -0.5 {
			// 价格偏差仍然过大，记录详细警告并使用平均价格
			logger.Warn("⚠️ [getPositionsSummary] 当前价格异常: currentPrice=%.2f, averagePrice=%.2f, 偏差=%.2f%%, totalQuantity=%.4f",
				currentPrice, averagePrice, priceDeviation*100, totalQuantity)
			logger.Warn("⚠️ [getPositionsSummary] 价格偏差过大，使用平均价格计算（未实现盈亏设为0）")
			currentPrice = averagePrice // 使用平均价格，使未实现盈亏为0
		}

		unrealizedPnL = (currentPrice - averagePrice) * totalQuantity

		// 🔥 添加未实现盈亏合理性检查：如果未实现盈亏相对于持仓成本过大（超过100%），记录警告
		if totalCost > 0 {
			pnlRatio := unrealizedPnL / totalCost
			if pnlRatio > 1.0 || pnlRatio < -1.0 {
				logger.Warn("⚠️ [getPositionsSummary] 未实现盈亏异常: unrealizedPnL=%.2f, totalCost=%.2f, 比例=%.2f%%, currentPrice=%.2f, averagePrice=%.2f",
					unrealizedPnL, totalCost, pnlRatio*100, currentPrice, averagePrice)
			}
		}
	}

	// 计算亏损率（相对于持仓成本的百分比）
	pnlPercentage := 0.0
	if totalCost > 0 {
		pnlPercentage = (unrealizedPnL / totalCost) * 100.0
	}

	c.JSON(http.StatusOK, gin.H{
		"total_quantity": totalQuantity,
		"total_value":    totalValue,
		"position_count": positionCount,
		"average_price":  averagePrice,
		"current_price":  currentPrice,
		"unrealized_pnl": unrealizedPnL,
		"pnl_percentage": pnlPercentage,
	})
}

// getOrders 获取订单列表（历史订单）
// GET /api/orders
func getOrders(c *gin.Context) {
	storageProv := pickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusOK, gin.H{"orders": []interface{}{}})
		return
	}

	storage := storageProv.GetStorage()
	if storage == nil {
		c.JSON(http.StatusOK, gin.H{"orders": []interface{}{}})
		return
	}

	// 解析参数
	limitStr := c.DefaultQuery("limit", "100")
	offsetStr := c.DefaultQuery("offset", "0")
	status := c.Query("status")

	limit := 100
	offset := 0
	if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
		limit = l
	}
	if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
		offset = o
	}

	orders, err := storage.QueryOrders(limit, offset, status)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "error.query_orders_failed", err)
		return
	}

	// 转换时间为UTC+8
	ordersResponse := make([]map[string]interface{}, len(orders))
	for i, order := range orders {
		ordersResponse[i] = map[string]interface{}{
			"order_id":        order.OrderID,
			"client_order_id": order.ClientOrderID,
			"symbol":          order.Symbol,
			"side":            order.Side,
			"price":           order.Price,
			"quantity":        order.Quantity,
			"status":          order.Status,
			"created_at":      utils.ToUTC8(order.CreatedAt),
			"updated_at":      utils.ToUTC8(order.UpdatedAt),
		}
	}

	c.JSON(http.StatusOK, gin.H{"orders": ordersResponse})
}

// getOrderHistory 获取订单历史
// GET /api/orders/history
func getOrderHistory(c *gin.Context) {
	storageProv := pickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusOK, gin.H{"orders": []interface{}{}})
		return
	}

	storage := storageProv.GetStorage()
	if storage == nil {
		c.JSON(http.StatusOK, gin.H{"orders": []interface{}{}})
		return
	}

	// 解析参数
	limitStr := c.DefaultQuery("limit", "100")
	offsetStr := c.DefaultQuery("offset", "0")

	limit := 100
	offset := 0
	if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
		limit = l
	}
	if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
		offset = o
	}

	// 只查询已完成或已取消的订单
	orders, err := storage.QueryOrders(limit, offset, "FILLED")
	if err != nil {
		// 如果查询失败，尝试查询所有状态的订单
		orders, err = storage.QueryOrders(limit, offset, "")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
	}

	// 也查询已取消的订单
	canceledOrders, err := storage.QueryOrders(limit, offset, "CANCELED")
	if err == nil {
		orders = append(orders, canceledOrders...)
	}

	// 转换时间为UTC+8并格式化返回数据
	ordersResponse := make([]map[string]interface{}, len(orders))
	for i, order := range orders {
		ordersResponse[i] = map[string]interface{}{
			"order_id":        order.OrderID,
			"client_order_id": order.ClientOrderID,
			"symbol":          order.Symbol,
			"side":            order.Side,
			"price":           order.Price,
			"quantity":        order.Quantity,
			"status":          order.Status,
			"created_at":      utils.ToUTC8(order.CreatedAt),
			"updated_at":      utils.ToUTC8(order.UpdatedAt),
		}
	}

	c.JSON(http.StatusOK, gin.H{"orders": ordersResponse})
}

var (
	// 存储服务提供者（需要从main.go注入）
	storageServiceProvider StorageServiceProvider
)

// StorageServiceProvider 存储服务提供者接口
type StorageServiceProvider interface {
	GetStorage() storage.Storage
}

// SetStorageServiceProvider 设置存储服务提供者
func SetStorageServiceProvider(provider StorageServiceProvider) {
	storageServiceProvider = provider
}

// storageServiceAdapter 存储服务适配器
type storageServiceAdapter struct {
	service *storage.StorageService
}

// NewStorageServiceAdapter 创建存储服务适配器
func NewStorageServiceAdapter(service *storage.StorageService) StorageServiceProvider {
	return &storageServiceAdapter{service: service}
}

// GetStorage 获取存储接口
func (a *storageServiceAdapter) GetStorage() storage.Storage {
	if a.service == nil {
		return nil
	}
	return a.service.GetStorage()
}

// getStatistics 获取统计数据
// GET /api/statistics
func getStatistics(c *gin.Context) {
	storageProv := pickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusOK, gin.H{
			"total_trades": 0,
			"total_volume": 0,
			"total_pnl":    0,
			"win_rate":     0,
		})
		return
	}

	storage := storageProv.GetStorage()
	if storage == nil {
		c.JSON(http.StatusOK, gin.H{
			"total_trades": 0,
			"total_volume": 0,
			"total_pnl":    0,
			"win_rate":     0,
		})
		return
	}

	// 从数据库获取统计汇总
	summary, err := storage.GetStatisticsSummary()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 如果数据库没有数据，尝试从 SuperPositionManager 计算
	pmProvider := pickPositionProvider(c)
	if summary.TotalTrades == 0 && pmProvider != nil {
		slots := pmProvider.GetAllSlots()
		totalBuyQty := 0.0
		totalSellQty := 0.0

		for _, slot := range slots {
			if slot.OrderSide == "BUY" && slot.OrderStatus == "FILLED" {
				totalBuyQty += slot.OrderFilledQty
			} else if slot.OrderSide == "SELL" && slot.OrderStatus == "FILLED" {
				totalSellQty += slot.OrderFilledQty
			}
		}

		// 估算交易数（买卖配对）
		totalTrades := int((totalBuyQty + totalSellQty) / 2)
		if totalTrades > 0 {
			summary.TotalTrades = totalTrades
			summary.TotalVolume = totalBuyQty + totalSellQty
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"total_trades": summary.TotalTrades,
		"total_volume": summary.TotalVolume,
		"total_pnl":    summary.TotalPnL,
		"win_rate":     summary.WinRate,
	})
}

// getDailyStatistics 获取每日统计（混合模式：优先使用 statistics 表，缺失的日期从 trades 表补充）
// GET /api/statistics/daily
func getDailyStatistics(c *gin.Context) {
	storageProv := pickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusOK, gin.H{"statistics": []interface{}{}})
		return
	}

	st := storageProv.GetStorage()
	if st == nil {
		c.JSON(http.StatusOK, gin.H{"statistics": []interface{}{}})
		return
	}

	// 解析参数
	daysStr := c.DefaultQuery("days", "30")
	days := 30
	if d, err := strconv.Atoi(daysStr); err == nil && d > 0 {
		days = d
	}

	startDate := utils.NowConfiguredTimezone().AddDate(0, 0, -days)
	endDate := utils.NowConfiguredTimezone()

	// 1. 先从 statistics 表查询
	statsFromTable, err := st.QueryStatistics(startDate, endDate)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 2. 构建日期映射（statistics 表中已有的日期）
	statsMap := make(map[string]*storage.Statistics)
	for _, stat := range statsFromTable {
		dateKey := stat.Date.Format("2006-01-02")
		statsMap[dateKey] = stat
	}

	// 3. 从 trades 表查询所有日期（包含缺失的日期和盈利/亏损交易数）
	tradesStatsMap := make(map[string]*storage.DailyStatisticsWithTradeCount)
	tradesStats, err2 := st.QueryDailyStatisticsFromTrades(startDate, endDate)
	if err2 == nil {
		for _, tradeStat := range tradesStats {
			dateKey := tradeStat.Date.Format("2006-01-02")
			tradesStatsMap[dateKey] = tradeStat
		}
	}

	// 4. 合并数据：优先使用 statistics 表的数据，缺失的日期使用 trades 表的数据
	// 构建最终结果
	var result []map[string]interface{}
	startDateStr := startDate.Format("2006-01-02")
	endDateStr := endDate.Format("2006-01-02")

	// 处理所有日期（包括 statistics 表和 trades 表中的日期）
	allDates := make(map[string]bool)
	for dateKey := range statsMap {
		allDates[dateKey] = true
	}
	for dateKey := range tradesStatsMap {
		allDates[dateKey] = true
	}

	// 转换为列表
	var dateList []string
	for dateKey := range allDates {
		if dateKey >= startDateStr && dateKey <= endDateStr {
			dateList = append(dateList, dateKey)
		}
	}

	// 按日期倒序排序
	for i := 0; i < len(dateList)-1; i++ {
		for j := i + 1; j < len(dateList); j++ {
			if dateList[i] < dateList[j] {
				dateList[i], dateList[j] = dateList[j], dateList[i]
			}
		}
	}

	// 构建结果
	for _, dateKey := range dateList {
		item := make(map[string]interface{})
		item["date"] = dateKey

		// 优先使用 statistics 表的数据
		if stat, exists := statsMap[dateKey]; exists {
			item["total_trades"] = stat.TotalTrades
			item["total_volume"] = stat.TotalVolume
			item["total_pnl"] = stat.TotalPnL
			item["win_rate"] = stat.WinRate
		} else if tradeStat, exists := tradesStatsMap[dateKey]; exists {
			// 使用 trades 表的数据
			item["total_trades"] = tradeStat.TotalTrades
			item["total_volume"] = tradeStat.TotalVolume
			item["total_pnl"] = tradeStat.TotalPnL
			item["win_rate"] = tradeStat.WinRate
			item["winning_trades"] = tradeStat.WinningTrades
			item["losing_trades"] = tradeStat.LosingTrades
		} else {
			continue
		}

		// 如果 statistics 表的数据存在，但从 trades 表可以获取盈利/亏损交易数，也添加进去
		if _, exists := statsMap[dateKey]; exists {
			if tradeStat, exists := tradesStatsMap[dateKey]; exists {
				item["winning_trades"] = tradeStat.WinningTrades
				item["losing_trades"] = tradeStat.LosingTrades
			}
		}

		result = append(result, item)
	}

	c.JSON(http.StatusOK, gin.H{"statistics": result})
}

// getTradeStatistics 获取交易统计
// GET /api/statistics/trades
func getTradeStatistics(c *gin.Context) {
	storageProv := pickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusOK, gin.H{"trades": []interface{}{}})
		return
	}

	storage := storageProv.GetStorage()
	if storage == nil {
		c.JSON(http.StatusOK, gin.H{"trades": []interface{}{}})
		return
	}

	// 解析参数
	limitStr := c.DefaultQuery("limit", "100")
	offsetStr := c.DefaultQuery("offset", "0")
	limit := 100
	offset := 0
	if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
		limit = l
	}
	if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
		offset = o
	}

	startTimeStr := c.Query("start_time")
	endTimeStr := c.Query("end_time")

	var startTime, endTime time.Time
	var err error

	if startTimeStr != "" {
		startTime, err = time.Parse(time.RFC3339, startTimeStr)
		if err != nil {
			respondError(c, http.StatusBadRequest, "error.invalid_start_time")
			return
		}
	} else {
		startTime = utils.NowConfiguredTimezone().AddDate(0, 0, -7) // 默认最近7天
	}

	if endTimeStr != "" {
		endTime, err = time.Parse(time.RFC3339, endTimeStr)
		if err != nil {
			respondError(c, http.StatusBadRequest, "error.invalid_end_time")
			return
		}
	} else {
		endTime = utils.NowConfiguredTimezone()
	}

	trades, err := storage.QueryTrades(startTime, endTime, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 转换时间为UTC+8
	tradesResponse := make([]map[string]interface{}, len(trades))
	for i, trade := range trades {
		tradesResponse[i] = map[string]interface{}{
			"buy_order_id":  trade.BuyOrderID,
			"sell_order_id": trade.SellOrderID,
			"symbol":        trade.Symbol,
			"buy_price":     trade.BuyPrice,
			"sell_price":    trade.SellPrice,
			"quantity":      trade.Quantity,
			"pnl":           trade.PnL,
			"created_at":    utils.ToUTC8(trade.CreatedAt),
		}
	}

	c.JSON(http.StatusOK, gin.H{"trades": tradesResponse})
}

// 这些函数已移动到 web/api_config.go
// 保留这些存根函数以保持向后兼容（如果其他地方有引用）
func getConfig(c *gin.Context) {
	getConfigHandler(c)
}

func updateConfig(c *gin.Context) {
	updateConfigHandler(c)
}

func startTrading(c *gin.Context) {
	exchange := c.Query("exchange")
	symbol := c.Query("symbol")

	if exchange == "" || symbol == "" {
		respondError(c, http.StatusBadRequest, "error.missing_exchange_or_symbol")
		return
	}

	if symbolManagerProvider == nil {
		respondError(c, http.StatusInternalServerError, "error.symbol_manager_unavailable")
		return
	}

	err := symbolManagerProvider.StartSymbol(exchange, symbol)
	if err != nil {
		logger.Error("❌ [%s:%s] 启动交易失败: %v", exchange, symbol, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 更新状态
	key := makeSymbolKey(exchange, symbol)
	statusMu.Lock()
	if status, ok := statusBySymbol[key]; ok {
		status.Running = true
	} else {
		statusBySymbol[key] = &SystemStatus{
			Running:  true,
			Exchange: exchange,
			Symbol:   symbol,
		}
	}
	statusMu.Unlock()

	logger.Info("✅ [%s:%s] 交易已启动", exchange, symbol)
	c.JSON(http.StatusOK, gin.H{"message": fmt.Sprintf("交易已启动: %s:%s", exchange, symbol)})
}

func stopTrading(c *gin.Context) {
	exchange := c.Query("exchange")
	symbol := c.Query("symbol")

	if exchange == "" || symbol == "" {
		respondError(c, http.StatusBadRequest, "error.missing_exchange_or_symbol")
		return
	}

	if symbolManagerProvider == nil {
		respondError(c, http.StatusInternalServerError, "error.symbol_manager_unavailable")
		return
	}

	err := symbolManagerProvider.StopSymbol(exchange, symbol)
	if err != nil {
		logger.Error("❌ [%s:%s] 停止交易失败: %v", exchange, symbol, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 更新状态
	key := makeSymbolKey(exchange, symbol)
	statusMu.Lock()
	if status, ok := statusBySymbol[key]; ok {
		status.Running = false
	}
	statusMu.Unlock()

	logger.Info("⏹️ [%s:%s] 交易已停止", exchange, symbol)
	c.JSON(http.StatusOK, gin.H{"message": fmt.Sprintf("交易已停止: %s:%s", exchange, symbol)})
}

// ClosePositionsResponse 平仓响应
type ClosePositionsResponse struct {
	SuccessCount int `json:"success_count"`
	FailCount    int `json:"fail_count"`
	Message      string `json:"message"`
}

func closeAllPositions(c *gin.Context) {
	exchange := c.Query("exchange")
	symbol := c.Query("symbol")

	if exchange == "" || symbol == "" {
		respondError(c, http.StatusBadRequest, "error.missing_exchange_or_symbol")
		return
	}

	if symbolManagerProvider == nil {
		respondError(c, http.StatusInternalServerError, "error.symbol_manager_unavailable")
		return
	}

	// 通过适配器调用 ClosePositions 方法
	adapter, ok := symbolManagerProvider.(interface {
		ClosePositions(exchange, symbol string) (*ClosePositionsResponse, error)
	})
	if !ok {
		respondError(c, http.StatusInternalServerError, "error.close_positions_not_supported")
		return
	}

	result, err := adapter.ClosePositions(exchange, symbol)
	if err != nil {
		logger.Error("❌ [%s:%s] 平仓失败: %v", exchange, symbol, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	logger.Info("📊 [%s:%s] 平仓完成: 成功=%d, 失败=%d", exchange, symbol, result.SuccessCount, result.FailCount)
	c.JSON(http.StatusOK, result)
}

// ========== 交易控制相关API ==========

var (
	// SymbolManager 提供者（需要从main.go注入）
	symbolManagerProvider SymbolManagerProvider
)

// SymbolManagerProvider SymbolManager 提供者接口
type SymbolManagerProvider interface {
	Get(exchange, symbol string) (interface{}, bool) // 返回 SymbolRuntime（使用 interface{} 避免循环依赖）
	List() []interface{}                             // 返回 SymbolRuntime 列表
	StartSymbol(exchange, symbol string) error       // 启动指定交易所/币种的交易
	StopSymbol(exchange, symbol string) error         // 停止指定交易所/币种的交易
}

// RegisterSymbolManager 注册 SymbolManager
func RegisterSymbolManager(provider SymbolManagerProvider) {
	symbolManagerProvider = provider
}

// ========== 系统监控相关API ==========

var (
	// 系统监控数据提供者（需要从main.go注入）
	systemMetricsProvider SystemMetricsProvider
)

// SystemMetricsProvider 系统监控数据提供者接口
type SystemMetricsProvider interface {
	GetCurrentMetrics() (*SystemMetricsResponse, error)
	GetMetrics(startTime, endTime time.Time, granularity string) ([]*SystemMetricsResponse, error)
	GetDailyMetrics(days int) ([]*DailySystemMetricsResponse, error)
}

// SystemMetricsResponse 系统监控数据响应
type SystemMetricsResponse struct {
	Timestamp     time.Time `json:"timestamp"`
	CPUPercent    float64   `json:"cpu_percent"`
	MemoryMB      float64   `json:"memory_mb"`
	MemoryPercent float64   `json:"memory_percent"`
	ProcessID     int       `json:"process_id"`
}

// DailySystemMetricsResponse 每日汇总数据响应
type DailySystemMetricsResponse struct {
	Date          time.Time `json:"date"`
	AvgCPUPercent float64   `json:"avg_cpu_percent"`
	MaxCPUPercent float64   `json:"max_cpu_percent"`
	MinCPUPercent float64   `json:"min_cpu_percent"`
	AvgMemoryMB   float64   `json:"avg_memory_mb"`
	MaxMemoryMB   float64   `json:"max_memory_mb"`
	MinMemoryMB   float64   `json:"min_memory_mb"`
	SampleCount   int       `json:"sample_count"`
}

// SetSystemMetricsProvider 设置系统监控数据提供者
func SetSystemMetricsProvider(provider SystemMetricsProvider) {
	systemMetricsProvider = provider
}

// getSystemMetrics 获取系统监控数据
// GET /api/system/metrics
// 参数：
//   - start_time: 开始时间（可选，ISO 8601格式，默认最近7天）
//   - end_time: 结束时间（可选，ISO 8601格式，默认当前时间）
//   - granularity: 粒度（detail/daily，默认detail）
func getSystemMetrics(c *gin.Context) {
	if systemMetricsProvider == nil {
		c.JSON(http.StatusOK, gin.H{"metrics": []interface{}{}})
		return
	}

	// 解析参数
	startTimeStr := c.Query("start_time")
	endTimeStr := c.Query("end_time")
	granularity := c.DefaultQuery("granularity", "detail")

	var startTime, endTime time.Time
	var err error

	if startTimeStr == "" {
		// 默认最近7天
		startTime = utils.NowConfiguredTimezone().Add(-7 * 24 * time.Hour)
	} else {
		startTime, err = time.Parse(time.RFC3339, startTimeStr)
		if err != nil {
			respondError(c, http.StatusBadRequest, "error.invalid_start_time")
			return
		}
	}

	if endTimeStr == "" {
		endTime = utils.NowConfiguredTimezone()
	} else {
		endTime, err = time.Parse(time.RFC3339, endTimeStr)
		if err != nil {
			respondError(c, http.StatusBadRequest, "error.invalid_end_time")
			return
		}
	}

	if granularity == "daily" {
		// 返回每日汇总数据
		days := int(endTime.Sub(startTime).Hours() / 24)
		if days <= 0 {
			days = 30 // 默认30天
		}
		dailyMetrics, err := systemMetricsProvider.GetDailyMetrics(days)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"metrics": dailyMetrics, "granularity": "daily"})
	} else {
		// 返回细粒度数据
		metrics, err := systemMetricsProvider.GetMetrics(startTime, endTime, "detail")
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"metrics": metrics, "granularity": "detail"})
	}
}

// getCurrentSystemMetrics 获取当前系统状态
// GET /api/system/metrics/current
func getCurrentSystemMetrics(c *gin.Context) {
	if systemMetricsProvider == nil {
		// 返回完整的对象结构，避免前端访问 undefined 字段
		c.JSON(http.StatusOK, &SystemMetricsResponse{
			Timestamp:     utils.ToUTC8(time.Now()),
			CPUPercent:    0,
			MemoryMB:      0,
			MemoryPercent: 0,
			ProcessID:     0,
		})
		return
	}

	metrics, err := systemMetricsProvider.GetCurrentMetrics()
	if err != nil {
		// 即使出错也返回完整的对象结构
		c.JSON(http.StatusOK, &SystemMetricsResponse{
			Timestamp:     utils.ToUTC8(time.Now()),
			CPUPercent:    0,
			MemoryMB:      0,
			MemoryPercent: 0,
			ProcessID:     0,
		})
		return
	}

	// 确保所有字段都有默认值
	if metrics == nil {
		metrics = &SystemMetricsResponse{
			Timestamp:     utils.ToUTC8(time.Now()),
			CPUPercent:    0,
			MemoryMB:      0,
			MemoryPercent: 0,
			ProcessID:     0,
		}
	}

	c.JSON(http.StatusOK, metrics)
}

// getDailySystemMetrics 获取每日汇总数据
// GET /api/system/metrics/daily
// 参数：
//   - days: 查询天数（默认30天）
func getDailySystemMetrics(c *gin.Context) {
	if systemMetricsProvider == nil {
		c.JSON(http.StatusOK, gin.H{"metrics": []interface{}{}})
		return
	}

	daysStr := c.DefaultQuery("days", "30")
	days := 30
	if d, err := strconv.Atoi(daysStr); err == nil && d > 0 {
		days = d
	}

	metrics, err := systemMetricsProvider.GetDailyMetrics(days)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"metrics": metrics})
}

// ========== 槽位数据相关API ==========

var (
	// 槽位数据提供者（需要从main.go注入）
	positionManagerProvider PositionManagerProvider
	// 订单金额配置（用于计算订单数量）
	orderQuantityConfig float64
)

// SetOrderQuantityConfig 设置订单金额配置
func SetOrderQuantityConfig(quantity float64) {
	orderQuantityConfig = quantity
}

// PositionManagerProvider 槽位数据提供者接口
type PositionManagerProvider interface {
	GetAllSlots() []SlotInfo
	GetSlotCount() int
	GetReconcileCount() int64
	GetLastReconcileTime() time.Time
	GetTotalBuyQty() float64
	GetTotalSellQty() float64
	GetPriceInterval() float64
}

// SlotInfo 槽位信息
type SlotInfo struct {
	Price          float64   `json:"price"`
	PositionStatus string    `json:"position_status"` // EMPTY/FILLED
	PositionQty    float64   `json:"position_qty"`
	OrderID        int64     `json:"order_id"`
	ClientOID      string    `json:"client_order_id"`
	OrderSide      string    `json:"order_side"`   // BUY/SELL
	OrderStatus    string    `json:"order_status"` // NOT_PLACED/PLACED/CONFIRMED/PARTIALLY_FILLED/FILLED/CANCELED
	OrderPrice     float64   `json:"order_price"`
	OrderFilledQty float64   `json:"order_filled_qty"`
	OrderCreatedAt time.Time `json:"order_created_at"`
	SlotStatus     string    `json:"slot_status"` // FREE/PENDING/LOCKED
}

// SetPositionManagerProvider 设置槽位数据提供者
func SetPositionManagerProvider(provider PositionManagerProvider) {
	positionManagerProvider = provider
}

// positionManagerAdapter 槽位管理器适配器
type positionManagerAdapter struct {
	manager *position.SuperPositionManager
}

// NewPositionManagerAdapter 创建槽位管理器适配器
func NewPositionManagerAdapter(manager *position.SuperPositionManager) PositionManagerProvider {
	return &positionManagerAdapter{manager: manager}
}

// GetAllSlots 获取所有槽位信息
func (a *positionManagerAdapter) GetAllSlots() []SlotInfo {
	detailedSlots := a.manager.GetAllSlotsDetailed()

	// 🔥 调试：打印管理器的交易对信息
	symbol := a.manager.GetSymbol()
	anchorPrice := a.manager.GetAnchorPrice()
	logger.Info("[DEBUG] GetAllSlots called - symbol=%s, anchorPrice=%.2f, slotsCount=%d",
		symbol, anchorPrice, len(detailedSlots))

	slots := make([]SlotInfo, len(detailedSlots))
	for i, ds := range detailedSlots {
		slots[i] = SlotInfo{
			Price:          ds.Price,
			PositionStatus: ds.PositionStatus,
			PositionQty:    ds.PositionQty,
			OrderID:        ds.OrderID,
			ClientOID:      ds.ClientOID,
			OrderSide:      ds.OrderSide,
			OrderStatus:    ds.OrderStatus,
			OrderPrice:     ds.OrderPrice,
			OrderFilledQty: ds.OrderFilledQty,
			OrderCreatedAt: utils.ToUTC8(ds.OrderCreatedAt),
			SlotStatus:     ds.SlotStatus,
		}
	}
	return slots
}

// GetSlotCount 获取槽位总数
func (a *positionManagerAdapter) GetSlotCount() int {
	return a.manager.GetSlotCount()
}

// GetReconcileCount 获取对账次数
func (a *positionManagerAdapter) GetReconcileCount() int64 {
	return a.manager.GetReconcileCount()
}

// GetLastReconcileTime 获取最后对账时间
func (a *positionManagerAdapter) GetLastReconcileTime() time.Time {
	return a.manager.GetLastReconcileTime()
}

// GetTotalBuyQty 获取累计买入数量
func (a *positionManagerAdapter) GetTotalBuyQty() float64 {
	return a.manager.GetTotalBuyQty()
}

// GetTotalSellQty 获取累计卖出数量
func (a *positionManagerAdapter) GetTotalSellQty() float64 {
	return a.manager.GetTotalSellQty()
}

// GetPriceInterval 获取价格间隔
func (a *positionManagerAdapter) GetPriceInterval() float64 {
	return a.manager.GetPriceInterval()
}

// getSlots 获取所有槽位信息
// GET /api/slots
func getSlots(c *gin.Context) {
	exchange := c.Query("exchange")
	symbol := c.Query("symbol")

	pmProvider := pickPositionProvider(c)
	if pmProvider == nil {
		c.JSON(http.StatusOK, gin.H{"slots": []interface{}{}, "count": 0})
		return
	}

	slots := pmProvider.GetAllSlots()
	count := pmProvider.GetSlotCount()

	// 🔥 调试：打印前3个槽位的价格
	if len(slots) > 0 {
		logger.Info("[DEBUG] getSlots - exchange=%s, symbol=%s, total=%d, first 3 prices: %.2f, %.2f, %.2f",
			exchange, symbol, len(slots),
			slots[0].Price,
			slots[min(1, len(slots)-1)].Price,
			slots[min(2, len(slots)-1)].Price)
	}

	c.JSON(http.StatusOK, gin.H{
		"slots": slots,
		"count": count,
	})
}

// ========== 策略资金分配相关API ==========

var (
	// 策略数据提供者（需要从main.go注入）
	strategyProvider StrategyProvider
)

// StrategyProvider 策略资金分配提供者接口
type StrategyProvider interface {
	GetCapitalAllocation() map[string]StrategyCapitalInfo
}

// StrategyCapitalInfo 策略资金信息
type StrategyCapitalInfo struct {
	Allocated float64 `json:"allocated"`  // 分配的资金
	Used      float64 `json:"used"`       // 已使用的资金（保证金）
	Available float64 `json:"available"`  // 可用资金
	Weight    float64 `json:"weight"`     // 权重
	FixedPool float64 `json:"fixed_pool"` // 固定资金池（如果指定）
}

// SetStrategyProvider 设置策略数据提供者
func SetStrategyProvider(provider StrategyProvider) {
	strategyProvider = provider
}

// strategyProviderAdapter 策略提供者适配器
type strategyProviderAdapter struct {
	getAllocationFunc func() map[string]StrategyCapitalInfo
}

// NewStrategyProviderAdapter 创建策略提供者适配器
func NewStrategyProviderAdapter(getAllocationFunc func() map[string]StrategyCapitalInfo) StrategyProvider {
	return &strategyProviderAdapter{getAllocationFunc: getAllocationFunc}
}

// GetCapitalAllocation 获取策略资金分配信息
func (a *strategyProviderAdapter) GetCapitalAllocation() map[string]StrategyCapitalInfo {
	return a.getAllocationFunc()
}

// getStrategyAllocation 获取策略资金分配信息
// GET /api/strategies/allocation
func getStrategyAllocation(c *gin.Context) {
	if strategyProvider == nil {
		c.JSON(http.StatusOK, gin.H{"allocation": map[string]interface{}{}})
		return
	}

	allocation := strategyProvider.GetCapitalAllocation()
	c.JSON(http.StatusOK, gin.H{"allocation": allocation})
}

// ========== 待成交订单相关API ==========

// getPendingOrders 获取待成交订单列表
// GET /api/orders/pending
func getPendingOrders(c *gin.Context) {
	pmProvider := pickPositionProvider(c)
	if pmProvider == nil {
		c.JSON(http.StatusOK, gin.H{"orders": []interface{}{}})
		return
	}

	slots := pmProvider.GetAllSlots()
	var pendingOrders []PendingOrderInfo

	for _, slot := range slots {
		// 筛选状态为 PLACED/CONFIRMED/PARTIALLY_FILLED 的订单
		if slot.OrderStatus == "PLACED" || slot.OrderStatus == "CONFIRMED" || slot.OrderStatus == "PARTIALLY_FILLED" {
			// 计算订单原始数量：使用配置的订单金额 / 订单价格
			var quantity float64
			if slot.OrderPrice > 0 && orderQuantityConfig > 0 {
				quantity = orderQuantityConfig / slot.OrderPrice
			} else if slot.OrderFilledQty > 0 {
				// 如果无法计算，使用已成交数量作为估算
				quantity = slot.OrderFilledQty
			}

			pendingOrders = append(pendingOrders, PendingOrderInfo{
				OrderID:        slot.OrderID,
				ClientOrderID:  slot.ClientOID,
				Price:          slot.OrderPrice,
				Quantity:       quantity,
				Side:           slot.OrderSide,
				Status:         slot.OrderStatus,
				FilledQuantity: slot.OrderFilledQty,
				CreatedAt:      utils.ToUTC8(slot.OrderCreatedAt),
				SlotPrice:      slot.Price,
			})
		}
	}

	c.JSON(http.StatusOK, gin.H{"orders": pendingOrders, "count": len(pendingOrders)})
}

// PendingOrderInfo 待成交订单信息
type PendingOrderInfo struct {
	OrderID        int64     `json:"order_id"`
	ClientOrderID  string    `json:"client_order_id"`
	Price          float64   `json:"price"`
	Quantity       float64   `json:"quantity"`
	Side           string    `json:"side"` // BUY/SELL
	Status         string    `json:"status"`
	FilledQuantity float64   `json:"filled_quantity"`
	CreatedAt      time.Time `json:"created_at"`
	SlotPrice      float64   `json:"slot_price"` // 槽位价格
}

// ========== 日志相关API ==========

var (
	// 日志存储提供者（需要从main.go注入）
	logStorageProvider LogStorageProvider
)

// LogStorageProvider 日志存储提供者接口
type LogStorageProvider interface {
	GetLogs(startTime, endTime time.Time, level, keyword string, limit, offset int) ([]*LogRecordResponse, int, error)
	CleanOldLogsByLevel(days int, levels []string) (int64, error)
	Vacuum() error
	GetLogStats() (map[string]interface{}, error)
}

// logStorageAdapter 日志存储适配器
type logStorageAdapter struct {
	storage *storage.LogStorage
}

// NewLogStorageAdapter 创建日志存储适配器
func NewLogStorageAdapter(ls *storage.LogStorage) LogStorageProvider {
	return &logStorageAdapter{storage: ls}
}

// GetLogs 实现 LogStorageProvider 接口
func (a *logStorageAdapter) GetLogs(startTime, endTime time.Time, level, keyword string, limit, offset int) ([]*LogRecordResponse, int, error) {
	params := storage.LogQueryParams{
		StartTime: startTime,
		EndTime:   endTime,
		Level:     level,
		Keyword:   keyword,
		Limit:     limit,
		Offset:    offset,
	}

	logs, total, err := a.storage.GetLogs(params)
	if err != nil {
		return nil, 0, err
	}

	// 转换为响应格式
	result := make([]*LogRecordResponse, len(logs))
	for i, log := range logs {
		result[i] = &LogRecordResponse{
			ID:        log.ID,
			Timestamp: utils.ToUTC8(log.Timestamp),
			Level:     log.Level,
			Message:   log.Message,
		}
	}

	return result, total, nil
}

// CleanOldLogsByLevel 实现 LogStorageProvider 接口
func (a *logStorageAdapter) CleanOldLogsByLevel(days int, levels []string) (int64, error) {
	return a.storage.CleanOldLogsByLevel(days, levels)
}

// Vacuum 实现 LogStorageProvider 接口
func (a *logStorageAdapter) Vacuum() error {
	return a.storage.Vacuum()
}

// GetLogStats 实现 LogStorageProvider 接口
func (a *logStorageAdapter) GetLogStats() (map[string]interface{}, error) {
	return a.storage.GetLogStats()
}

// LogRecordResponse 日志记录响应
type LogRecordResponse struct {
	ID        int64     `json:"id"`
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"`
	Message   string    `json:"message"`
}

// SetLogStorageProvider 设置日志存储提供者
func SetLogStorageProvider(provider LogStorageProvider) {
	logStorageProvider = provider
}

// getLogs 获取日志
// GET /api/logs
// 参数：
//   - start_time: 开始时间（可选，ISO 8601格式）
//   - end_time: 结束时间（可选，ISO 8601格式，默认当前时间）
//   - level: 日志级别（可选，DEBUG/INFO/WARN/ERROR/FATAL）
//   - keyword: 关键词搜索（可选）
//   - limit: 每页数量（可选，默认100，最大1000）
//   - offset: 偏移量（可选，默认0）
func getLogs(c *gin.Context) {
	if logStorageProvider == nil {
		c.JSON(http.StatusOK, gin.H{"logs": []interface{}{}, "total": 0})
		return
	}

	// 解析参数
	startTimeStr := c.Query("start_time")
	endTimeStr := c.Query("end_time")
	level := c.Query("level")
	keyword := c.Query("keyword")
	limitStr := c.DefaultQuery("limit", "100")
	offsetStr := c.DefaultQuery("offset", "0")

	var startTime, endTime time.Time
	var err error

	if startTimeStr != "" {
		startTime, err = time.Parse(time.RFC3339, startTimeStr)
		if err != nil {
			respondError(c, http.StatusBadRequest, "error.invalid_start_time")
			return
		}
	}

	if endTimeStr != "" {
		endTime, err = time.Parse(time.RFC3339, endTimeStr)
		if err != nil {
			respondError(c, http.StatusBadRequest, "error.invalid_end_time")
			return
		}
	} else {
		endTime = time.Now()
	}

	// 如果没有指定开始时间，默认最近7天
	if startTime.IsZero() {
		startTime = endTime.AddDate(0, 0, -7)
	}

	limit := 100
	if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
		limit = l
		if limit > 1000 {
			limit = 1000
		}
	}

	offset := 0
	if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
		offset = o
	}

	// 查询日志
	logs, total, err := logStorageProvider.GetLogs(startTime, endTime, level, keyword, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"logs":   logs,
		"total":  total,
		"limit":  limit,
		"offset": offset,
	})
}

// cleanLogs 清理日志
// POST /api/logs/clean
// 参数：
//   - days: 保留天数（默认7天）
//   - levels: 要清理的日志级别列表，如 ["INFO", "WARN"]（可选，默认清理所有级别）
func cleanLogs(c *gin.Context) {
	if logStorageProvider == nil {
		respondError(c, http.StatusServiceUnavailable, "日志存储未初始化")
		return
	}

	var req struct {
		Days   int      `json:"days"`
		Levels []string `json:"levels"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "invalid_request")
		return
	}

	if req.Days <= 0 {
		req.Days = 7 // 默认7天
	}

	var rowsAffected int64
	var err error

	if len(req.Levels) > 0 {
		// 清理指定级别的日志
		rowsAffected, err = logStorageProvider.CleanOldLogsByLevel(req.Days, req.Levels)
	} else {
		// 清理所有级别的日志
		rowsAffected, err = logStorageProvider.CleanOldLogsByLevel(req.Days, []string{"DEBUG", "INFO", "WARN", "ERROR", "FATAL"})
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":       true,
		"rows_affected": rowsAffected,
		"message":       fmt.Sprintf("已清理 %d 条日志", rowsAffected),
	})
}

// getLogStats 获取日志统计信息
// GET /api/logs/stats
func getLogStats(c *gin.Context) {
	if logStorageProvider == nil {
		respondError(c, http.StatusServiceUnavailable, "日志存储未初始化")
		return
	}

	stats, err := logStorageProvider.GetLogStats()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, stats)
}

// vacuumLogs 优化日志数据库
// POST /api/logs/vacuum
func vacuumLogs(c *gin.Context) {
	if logStorageProvider == nil {
		respondError(c, http.StatusServiceUnavailable, "日志存储未初始化")
		return
	}

	if err := logStorageProvider.Vacuum(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "数据库优化完成",
	})
}

// ReconciliationStatus 对账状态
type ReconciliationStatus struct {
	ReconcileCount    int64     `json:"reconcile_count"`     // 对账次数
	LastReconcileTime time.Time `json:"last_reconcile_time"` // 最后对账时间
	LocalPosition     float64   `json:"local_position"`      // 本地持仓
	TotalBuyQty       float64   `json:"total_buy_qty"`       // 累计买入
	TotalSellQty      float64   `json:"total_sell_qty"`      // 累计卖出
	EstimatedProfit   float64   `json:"estimated_profit"`    // 预计盈利
	ActualProfit      float64   `json:"actual_profit"`       // 实际盈利（来自 trades 表）
}

// ReconciliationHistoryInfo 对账历史信息
type ReconciliationHistoryInfo struct {
	ID               int64     `json:"id"`
	Symbol           string    `json:"symbol"`
	ReconcileTime    time.Time `json:"reconcile_time"`
	LocalPosition    float64   `json:"local_position"`
	ExchangePosition float64   `json:"exchange_position"`
	PositionDiff     float64   `json:"position_diff"`
	ActiveBuyOrders  int       `json:"active_buy_orders"`
	ActiveSellOrders int       `json:"active_sell_orders"`
	PendingSellQty   float64   `json:"pending_sell_qty"`
	TotalBuyQty      float64   `json:"total_buy_qty"`
	TotalSellQty     float64   `json:"total_sell_qty"`
	EstimatedProfit  float64   `json:"estimated_profit"`
	ActualProfit     float64   `json:"actual_profit"`
	CreatedAt        time.Time `json:"created_at"`
}

// getReconciliationStatus 获取对账状态
// GET /api/reconciliation/status
func getReconciliationStatus(c *gin.Context) {
	pmProvider := pickPositionProvider(c)
	if pmProvider == nil {
		c.JSON(http.StatusOK, gin.H{
			"reconcile_count":     0,
			"last_reconcile_time": time.Time{},
			"local_position":      0,
			"total_buy_qty":       0,
			"total_sell_qty":      0,
			"estimated_profit":    0,
			"actual_profit":       0,
		})
		return
	}

	// 从 PositionManager 获取对账统计
	reconcileCount := pmProvider.GetReconcileCount()
	lastReconcileTime := pmProvider.GetLastReconcileTime()
	totalBuyQty := pmProvider.GetTotalBuyQty()
	totalSellQty := pmProvider.GetTotalSellQty()
	priceInterval := pmProvider.GetPriceInterval()
	estimatedProfit := totalSellQty * priceInterval

	// 计算本地持仓
	slots := pmProvider.GetAllSlots()
	localPosition := 0.0
	for _, slot := range slots {
		if slot.PositionStatus == "FILLED" && slot.PositionQty > 0.000001 {
			localPosition += slot.PositionQty
		}
	}

	// 获取实际盈利
	actualProfit := 0.0
	symbol := c.Query("symbol")
	if symbol == "" {
		if st := pickStatus(c); st != nil {
			symbol = st.Symbol
		}
	}

	storageProv := pickStorageProvider(c)
	if symbol != "" && storageProv != nil && storageProv.GetStorage() != nil {
		// 查询截止到现在的累计实际盈利
		actualProfit, _ = storageProv.GetStorage().GetActualProfitBySymbol(symbol, time.Now().UTC())
	}

	status := ReconciliationStatus{
		ReconcileCount:    reconcileCount,
		LastReconcileTime: utils.ToUTC8(lastReconcileTime),
		LocalPosition:     localPosition,
		TotalBuyQty:       totalBuyQty,
		TotalSellQty:      totalSellQty,
		EstimatedProfit:   estimatedProfit,
		ActualProfit:      actualProfit,
	}

	c.JSON(http.StatusOK, status)
}

// getReconciliationHistory 获取对账历史
// GET /api/reconciliation/history
func getReconciliationHistory(c *gin.Context) {
	storageProv := pickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusOK, gin.H{"history": []interface{}{}})
		return
	}

	storage := storageProv.GetStorage()
	if storage == nil {
		c.JSON(http.StatusOK, gin.H{"history": []interface{}{}})
		return
	}

	// 解析参数
	symbol := c.Query("symbol")
	startTimeStr := c.Query("start_time")
	endTimeStr := c.Query("end_time")
	limitStr := c.DefaultQuery("limit", "100")
	offsetStr := c.DefaultQuery("offset", "0")

	var startTime, endTime time.Time
	var err error

	if startTimeStr != "" {
		startTime, err = time.Parse(time.RFC3339, startTimeStr)
		if err != nil {
			respondError(c, http.StatusBadRequest, "error.invalid_start_time")
			return
		}
	} else {
		// 默认最近7天
		startTime = time.Now().AddDate(0, 0, -7)
	}

	if endTimeStr != "" {
		endTime, err = time.Parse(time.RFC3339, endTimeStr)
		if err != nil {
			respondError(c, http.StatusBadRequest, "error.invalid_end_time")
			return
		}
	} else {
		endTime = time.Now()
	}

	limit := 100
	if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
		limit = l
	}

	offset := 0
	if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
		offset = o
	}

	// 查询对账历史
	histories, err := storage.QueryReconciliationHistory(symbol, startTime, endTime, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 转换为 API 响应格式
	result := make([]ReconciliationHistoryInfo, len(histories))
	for i, h := range histories {
		result[i] = ReconciliationHistoryInfo{
			ID:               h.ID,
			Symbol:           h.Symbol,
			ReconcileTime:    utils.ToUTC8(h.ReconcileTime),
			LocalPosition:    h.LocalPosition,
			ExchangePosition: h.ExchangePosition,
			PositionDiff:     h.PositionDiff,
			ActiveBuyOrders:  h.ActiveBuyOrders,
			ActiveSellOrders: h.ActiveSellOrders,
			PendingSellQty:   h.PendingSellQty,
			TotalBuyQty:      h.TotalBuyQty,
			TotalSellQty:     h.TotalSellQty,
			EstimatedProfit:  h.EstimatedProfit,
			ActualProfit:     h.ActualProfit,
			CreatedAt:        utils.ToUTC8(h.CreatedAt),
		}
	}

	c.JSON(http.StatusOK, gin.H{"history": result})
}

// PnLSummaryResponse 盈亏汇总响应
type PnLSummaryResponse struct {
	Symbol        string  `json:"symbol"`
	TotalPnL      float64 `json:"total_pnl"`
	TotalTrades   int     `json:"total_trades"`
	TotalVolume   float64 `json:"total_volume"`
	WinRate       float64 `json:"win_rate"`
	WinningTrades int     `json:"winning_trades"`
	LosingTrades  int     `json:"losing_trades"`
}

// getPnLBySymbol 按币种对查询盈亏数据
// GET /api/statistics/pnl/symbol
func getPnLBySymbol(c *gin.Context) {
	storageProv := pickStorageProvider(c)
	if storageProv == nil {
		respondError(c, http.StatusOK, "error.storage_unavailable")
		return
	}

	storage := storageProv.GetStorage()
	if storage == nil {
		respondError(c, http.StatusOK, "error.storage_unavailable")
		return
	}

	symbol := c.Query("symbol")
	if symbol == "" {
		respondError(c, http.StatusBadRequest, "error.missing_symbol_param")
		return
	}

	startTimeStr := c.Query("start_time")
	endTimeStr := c.Query("end_time")

	var startTime, endTime time.Time
	var err error

	if startTimeStr != "" {
		startTime, err = time.Parse(time.RFC3339, startTimeStr)
		if err != nil {
			respondError(c, http.StatusBadRequest, "error.invalid_start_time")
			return
		}
	} else {
		// 默认最近30天
		startTime = time.Now().AddDate(0, 0, -30)
	}

	if endTimeStr != "" {
		endTime, err = time.Parse(time.RFC3339, endTimeStr)
		if err != nil {
			respondError(c, http.StatusBadRequest, "error.invalid_end_time")
			return
		}
	} else {
		endTime = time.Now()
	}

	// 查询盈亏数据
	summary, err := storage.GetPnLBySymbol(symbol, startTime, endTime)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	response := PnLSummaryResponse{
		Symbol:        summary.Symbol,
		TotalPnL:      summary.TotalPnL,
		TotalTrades:   summary.TotalTrades,
		TotalVolume:   summary.TotalVolume,
		WinRate:       summary.WinRate,
		WinningTrades: summary.WinningTrades,
		LosingTrades:  summary.LosingTrades,
	}

	c.JSON(http.StatusOK, response)
}

// PnLBySymbolResponse 按币种对的盈亏数据
type PnLBySymbolResponse struct {
	Symbol      string  `json:"symbol"`
	TotalPnL    float64 `json:"total_pnl"`
	TotalTrades int     `json:"total_trades"`
	TotalVolume float64 `json:"total_volume"`
	WinRate     float64 `json:"win_rate"`
}

// getPnLByTimeRange 按时间区间查询盈亏数据（按币种对分组）
// GET /api/statistics/pnl/time-range
func getPnLByTimeRange(c *gin.Context) {
	storageProv := pickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusOK, gin.H{"pnl_by_symbol": []interface{}{}})
		return
	}

	storage := storageProv.GetStorage()
	if storage == nil {
		c.JSON(http.StatusOK, gin.H{"pnl_by_symbol": []interface{}{}})
		return
	}

	startTimeStr := c.Query("start_time")
	endTimeStr := c.Query("end_time")

	var startTime, endTime time.Time
	var err error

	if startTimeStr != "" {
		startTime, err = time.Parse(time.RFC3339, startTimeStr)
		if err != nil {
			respondError(c, http.StatusBadRequest, "error.invalid_start_time")
			return
		}
	} else {
		// 默认最近30天
		startTime = time.Now().AddDate(0, 0, -30)
	}

	if endTimeStr != "" {
		endTime, err = time.Parse(time.RFC3339, endTimeStr)
		if err != nil {
			respondError(c, http.StatusBadRequest, "error.invalid_end_time")
			return
		}
	} else {
		endTime = time.Now()
	}

	// 查询盈亏数据
	results, err := storage.GetPnLByTimeRange(startTime, endTime)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 转换为 API 响应格式
	response := make([]PnLBySymbolResponse, len(results))
	for i, r := range results {
		response[i] = PnLBySymbolResponse{
			Symbol:      r.Symbol,
			TotalPnL:    r.TotalPnL,
			TotalTrades: r.TotalTrades,
			TotalVolume: r.TotalVolume,
			WinRate:     r.WinRate,
		}
	}

	c.JSON(http.StatusOK, gin.H{"pnl_by_symbol": response})
}

// ExchangePnLResponse 按交易所分组的盈亏响应
type ExchangePnLResponse struct {
	Exchange    string              `json:"exchange"`
	TotalPnL    float64             `json:"total_pnl"`
	TotalTrades int                 `json:"total_trades"`
	TotalVolume float64             `json:"total_volume"`
	WinRate     float64             `json:"win_rate"`
	Symbols     []SymbolPnLInfo     `json:"symbols"`
}

// SymbolPnLInfo 币种盈亏信息
type SymbolPnLInfo struct {
	Symbol      string  `json:"symbol"`
	TotalPnL    float64 `json:"total_pnl"`
	TotalTrades int     `json:"total_trades"`
	TotalVolume float64 `json:"total_volume"`
	WinRate     float64 `json:"win_rate"`
}

// getPnLByExchange 按交易所分组查询盈亏数据
// GET /api/statistics/pnl/exchange
func getPnLByExchange(c *gin.Context) {
	storageProv := pickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusOK, gin.H{"exchanges": []interface{}{}})
		return
	}

	storage := storageProv.GetStorage()
	if storage == nil {
		c.JSON(http.StatusOK, gin.H{"exchanges": []interface{}{}})
		return
	}

	startTimeStr := c.Query("start_time")
	endTimeStr := c.Query("end_time")

	var startTime, endTime time.Time
	var err error

	if startTimeStr != "" {
		startTime, err = time.Parse(time.RFC3339, startTimeStr)
		if err != nil {
			respondError(c, http.StatusBadRequest, "error.invalid_start_time")
			return
		}
	} else {
		// 默认最近30天
		startTime = time.Now().AddDate(0, 0, -30)
	}

	if endTimeStr != "" {
		endTime, err = time.Parse(time.RFC3339, endTimeStr)
		if err != nil {
			respondError(c, http.StatusBadRequest, "error.invalid_end_time")
			return
		}
	} else {
		endTime = time.Now()
	}

	// 查询所有币种的盈亏数据（现在包含 exchange 字段）
	results, err := storage.GetPnLByTimeRange(startTime, endTime)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 按交易所分组（直接使用 exchange 字段）
	exchangeMap := make(map[string]*ExchangePnLResponse)
	for _, r := range results {
		exchange := strings.ToLower(r.Exchange)
		if exchange == "" {
			// 兼容旧数据：如果没有 exchange，默认为 binance
			exchange = "binance"
		}

		if _, exists := exchangeMap[exchange]; !exists {
			exchangeMap[exchange] = &ExchangePnLResponse{
				Exchange:    exchange,
				TotalPnL:    0,
				TotalTrades: 0,
				TotalVolume: 0,
				WinRate:     0,
				Symbols:     []SymbolPnLInfo{},
			}
		}

		exData := exchangeMap[exchange]
		exData.TotalPnL += r.TotalPnL
		exData.TotalTrades += r.TotalTrades
		exData.TotalVolume += r.TotalVolume

		// 添加币种信息
		exData.Symbols = append(exData.Symbols, SymbolPnLInfo{
			Symbol:      r.Symbol,
			TotalPnL:    r.TotalPnL,
			TotalTrades: r.TotalTrades,
			TotalVolume: r.TotalVolume,
			WinRate:     r.WinRate,
		})
	}

	// 计算每个交易所的胜率
	for _, exData := range exchangeMap {
		if exData.TotalTrades > 0 {
			winningTrades := 0
			for _, sym := range exData.Symbols {
				winningTrades += int(float64(sym.TotalTrades) * sym.WinRate)
			}
			exData.WinRate = float64(winningTrades) / float64(exData.TotalTrades)
		}
	}

	// 转换为列表
	response := make([]ExchangePnLResponse, 0, len(exchangeMap))
	for _, exData := range exchangeMap {
		response = append(response, *exData)
	}

	// 按交易所名称排序
	sort.Slice(response, func(i, j int) bool {
		return response[i].Exchange < response[j].Exchange
	})

	c.JSON(http.StatusOK, gin.H{"exchanges": response})
}

// getAnomalousTrades 检查异常交易记录（用于调试盈亏计算问题）
// GET /api/statistics/anomalous-trades
func getAnomalousTrades(c *gin.Context) {
	storageProv := pickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusOK, gin.H{"anomalous_trades": []interface{}{}})
		return
	}

	st := storageProv.GetStorage()
	if st == nil {
		c.JSON(http.StatusOK, gin.H{"anomalous_trades": []interface{}{}})
		return
	}

	symbol := c.Query("symbol")
	if symbol == "" {
		respondError(c, http.StatusBadRequest, "error.missing_symbol_param")
		return
	}

	// 查询所有交易记录
	trades, err := st.QueryTrades(time.Time{}, time.Now(), 1000, 0)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var anomalousTrades []map[string]interface{}
	for _, trade := range trades {
		if trade.Symbol != symbol {
			continue
		}

		// 计算订单金额
		orderAmount := trade.BuyPrice * trade.Quantity

		// 检查是否异常：盈亏超过订单金额的50%可能是错误的
		if orderAmount > 0 && math.Abs(trade.PnL) > orderAmount*0.5 {
			anomalousTrades = append(anomalousTrades, map[string]interface{}{
				"buy_order_id":  trade.BuyOrderID,
				"sell_order_id": trade.SellOrderID,
				"symbol":        trade.Symbol,
				"buy_price":     trade.BuyPrice,
				"sell_price":    trade.SellPrice,
				"quantity":      trade.Quantity,
				"pnl":           trade.PnL,
				"order_amount":  orderAmount,
				"pnl_rate":      (trade.PnL / orderAmount) * 100,
				"created_at":    utils.ToUTC8(trade.CreatedAt),
			})
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"anomalous_trades": anomalousTrades,
		"count":            len(anomalousTrades),
	})
}

// RiskMonitorProvider 风控监控提供者接口
type RiskMonitorProvider interface {
	IsTriggered() bool
	GetTriggeredTime() time.Time
	GetRecoveredTime() time.Time
	GetMonitorSymbols() []string
	GetSymbolData(symbol string) interface{}
}

var (
	riskMonitorProvider RiskMonitorProvider
)

// SetRiskMonitorProvider 设置风控监控提供者
func SetRiskMonitorProvider(provider RiskMonitorProvider) {
	riskMonitorProvider = provider
}

// RiskStatusResponse 风控状态响应
type RiskStatusResponse struct {
	Triggered      bool      `json:"triggered"`
	TriggeredTime  time.Time `json:"triggered_time"`
	RecoveredTime  time.Time `json:"recovered_time"`
	MonitorSymbols []string  `json:"monitor_symbols"`
}

// SymbolMonitorData 币种监控数据
type SymbolMonitorData struct {
	Symbol         string    `json:"symbol"`
	CurrentPrice   float64   `json:"current_price"`
	AveragePrice   float64   `json:"average_price"`
	PriceDeviation float64   `json:"price_deviation"`
	CurrentVolume  float64   `json:"current_volume"`
	AverageVolume  float64   `json:"average_volume"`
	VolumeRatio    float64   `json:"volume_ratio"`
	IsAbnormal     bool      `json:"is_abnormal"`
	LastUpdate     time.Time `json:"last_update"`
}

// getRiskStatus 获取风控状态
// GET /api/risk/status
func getRiskStatus(c *gin.Context) {
	riskProv := pickRiskProvider(c)
	if riskProv == nil {
		c.JSON(http.StatusOK, RiskStatusResponse{
			Triggered:      false,
			MonitorSymbols: []string{},
		})
		return
	}

	response := RiskStatusResponse{
		Triggered:      riskProv.IsTriggered(),
		TriggeredTime:  riskProv.GetTriggeredTime(),
		RecoveredTime:  riskProv.GetRecoveredTime(),
		MonitorSymbols: riskProv.GetMonitorSymbols(),
	}

	c.JSON(http.StatusOK, response)
}

// getRiskMonitorData 获取监控币种数据
// GET /api/risk/monitor
func getRiskMonitorData(c *gin.Context) {
	riskProv := pickRiskProvider(c)
	if riskProv == nil {
		c.JSON(http.StatusOK, gin.H{"symbols": []interface{}{}})
		return
	}

	symbols := riskProv.GetMonitorSymbols()
	var monitorData []SymbolMonitorData

	for _, symbol := range symbols {
		data := riskProv.GetSymbolData(symbol)
		if data == nil {
			continue
		}

		// 使用反射提取数据
		v := reflect.ValueOf(data)
		if v.Kind() == reflect.Ptr {
			v = v.Elem()
		}

		symbolData := SymbolMonitorData{
			Symbol: symbol,
		}

		// 提取字段
		if field := v.FieldByName("CurrentPrice"); field.IsValid() && field.CanFloat() {
			symbolData.CurrentPrice = field.Float()
		}
		if field := v.FieldByName("AveragePrice"); field.IsValid() && field.CanFloat() {
			symbolData.AveragePrice = field.Float()
		}
		if field := v.FieldByName("CurrentVolume"); field.IsValid() && field.CanFloat() {
			symbolData.CurrentVolume = field.Float()
		}
		if field := v.FieldByName("AverageVolume"); field.IsValid() && field.CanFloat() {
			symbolData.AverageVolume = field.Float()
		}
		if field := v.FieldByName("LastUpdate"); field.IsValid() {
			if t, ok := field.Interface().(time.Time); ok {
				symbolData.LastUpdate = t
			}
		}

		// 计算偏离度和比率
		if symbolData.AveragePrice > 0 {
			symbolData.PriceDeviation = (symbolData.CurrentPrice - symbolData.AveragePrice) / symbolData.AveragePrice * 100
		}
		if symbolData.AverageVolume > 0 {
			symbolData.VolumeRatio = symbolData.CurrentVolume / symbolData.AverageVolume
		}

		// 判断是否异常（简单判断）
		symbolData.IsAbnormal = math.Abs(symbolData.PriceDeviation) > 10 || symbolData.VolumeRatio > 3

		monitorData = append(monitorData, symbolData)
	}

	c.JSON(http.StatusOK, gin.H{"symbols": monitorData})
}

// RiskCheckHistoryResponse 风控检查历史响应
type RiskCheckHistoryResponse struct {
	CheckTime    time.Time             `json:"check_time"`
	Symbols      []RiskCheckSymbolInfo `json:"symbols"`
	HealthyCount int                   `json:"healthy_count"`
	TotalCount   int                   `json:"total_count"`
}

// RiskCheckSymbolInfo 风控检查币种信息
type RiskCheckSymbolInfo struct {
	Symbol         string  `json:"symbol"`
	IsHealthy      bool    `json:"is_healthy"`
	PriceDeviation float64 `json:"price_deviation"`
	VolumeRatio    float64 `json:"volume_ratio"`
	Reason         string  `json:"reason"`
}

// getRiskCheckHistory 获取风控检查历史
// GET /api/risk/history
// 参数：
//   - start_time: 开始时间（可选，ISO 8601格式，默认最近90天）
//   - end_time: 结束时间（可选，ISO 8601格式，默认当前时间）
func getRiskCheckHistory(c *gin.Context) {
	storageProv := pickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusOK, gin.H{"history": []interface{}{}})
		return
	}

	storage := storageProv.GetStorage()
	if storage == nil {
		c.JSON(http.StatusOK, gin.H{"history": []interface{}{}})
		return
	}

	// 解析参数
	startTimeStr := c.Query("start_time")
	endTimeStr := c.Query("end_time")
	limitStr := c.Query("limit")

	var startTime, endTime time.Time
	var err error
	limit := 500 // 默认限制500条

	if startTimeStr == "" {
		// 默认最近7天（减少默认数据量）
		startTime = time.Now().AddDate(0, 0, -7)
	} else {
		startTime, err = time.Parse(time.RFC3339, startTimeStr)
		if err != nil {
			respondError(c, http.StatusBadRequest, "error.invalid_start_time")
			return
		}
	}

	if endTimeStr == "" {
		endTime = time.Now()
	} else {
		endTime, err = time.Parse(time.RFC3339, endTimeStr)
		if err != nil {
			respondError(c, http.StatusBadRequest, "error.invalid_end_time")
			return
		}
	}

	if limitStr != "" {
		if parsedLimit, err := strconv.Atoi(limitStr); err == nil && parsedLimit > 0 {
			limit = parsedLimit
			// 最大限制为2000条
			if limit > 2000 {
				limit = 2000
			}
		}
	}

	// 查询历史数据
	histories, err := storage.QueryRiskCheckHistory(startTime, endTime, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 转换为 API 响应格式
	result := make([]RiskCheckHistoryResponse, len(histories))
	for i, h := range histories {
		symbols := make([]RiskCheckSymbolInfo, len(h.Symbols))
		for j, s := range h.Symbols {
			symbols[j] = RiskCheckSymbolInfo{
				Symbol:         s.Symbol,
				IsHealthy:      s.IsHealthy,
				PriceDeviation: s.PriceDeviation,
				VolumeRatio:    s.VolumeRatio,
				Reason:         s.Reason,
			}
		}
		result[i] = RiskCheckHistoryResponse{
			CheckTime:    utils.ToUTC8(h.CheckTime),
			Symbols:      symbols,
			HealthyCount: h.HealthyCount,
			TotalCount:   h.TotalCount,
		}
	}

	c.JSON(http.StatusOK, gin.H{"history": result})
}

// KlineData K线数据响应格式
type KlineData struct {
	Time   int64   `json:"time"` // 时间戳（秒）
	Open   float64 `json:"open"`
	High   float64 `json:"high"`
	Low    float64 `json:"low"`
	Close  float64 `json:"close"`
	Volume float64 `json:"volume"`
}

// getKlines 获取K线数据
// GET /api/klines
// 查询参数：
//   - interval: K线周期（1m/5m/15m/30m/1h/4h/1d等，默认1m）
//   - limit: 返回K线数量（默认500，最大1000）
func getKlines(c *gin.Context) {
	prov := pickExchangeProvider(c)
	if prov == nil {
		c.JSON(http.StatusOK, gin.H{"klines": []interface{}{}})
		return
	}

	// 获取当前交易币种（从系统状态）
	symbol := c.Query("symbol")
	if symbol == "" {
		if st := pickStatus(c); st != nil {
			symbol = st.Symbol
		}
	}
	if symbol == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无法获取交易币种"})
		return
	}

	// 解析查询参数
	interval := c.DefaultQuery("interval", "1m")
	limitStr := c.DefaultQuery("limit", "500")

	limit := 500
	if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
		limit = l
		if limit > 1000 {
			limit = 1000
		}
	}

	// 调用交易所接口获取K线数据
	ctx := c.Request.Context()
	candles, err := prov.GetHistoricalKlines(ctx, symbol, interval, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 转换为API响应格式
	klines := make([]KlineData, len(candles))
	for i, candle := range candles {
		// 将毫秒时间戳转换为秒（lightweight-charts使用秒级时间戳）
		klines[i] = KlineData{
			Time:   candle.Timestamp / 1000,
			Open:   candle.Open,
			High:   candle.High,
			Low:    candle.Low,
			Close:  candle.Close,
			Volume: candle.Volume,
		}
	}

	c.JSON(http.StatusOK, gin.H{"klines": klines, "symbol": symbol, "interval": interval})
}

// ========== 资金费率相关API ==========

var (
	// 资金费率监控提供者（需要从main.go注入）
	fundingMonitorProvider FundingMonitorProvider
)

// FundingMonitorProvider 资金费率监控提供者接口
type FundingMonitorProvider interface {
	GetCurrentFundingRates() (map[string]float64, error)
}

// SetFundingMonitorProvider 设置资金费率监控提供者
func SetFundingMonitorProvider(provider FundingMonitorProvider) {
	fundingMonitorProvider = provider
}

// getFundingRate 获取当前资金费率
// GET /api/funding/current
func getFundingRate(c *gin.Context) {
	fundingProv := pickFundingProvider(c)
	storageProv := pickStorageProvider(c)
	status := pickStatus(c)
	rates := make(map[string]interface{})

	// 从监控服务获取当前资金费率
	if fundingProv != nil {
		currentRates, err := fundingProv.GetCurrentFundingRates()
		if err == nil {
			for symbol, rate := range currentRates {
				rates[symbol] = map[string]interface{}{
					"rate":      rate,
					"rate_pct":  rate * 100, // 转换为百分比
					"timestamp": time.Now(),
				}
			}
		}
	}

	// 从数据库获取最新记录
	if storageProv != nil {
		storage := storageProv.GetStorage()
		if storage != nil {
			// 获取当前交易所名称
			exchangeName := ""
			if status != nil {
				exchangeName = status.Exchange
			}

			// 获取主流交易对的最新资金费率
			symbols := []string{"BTCUSDT", "ETHUSDT", "BNBUSDT", "SOLUSDT", "XRPUSDT"}
			for _, symbol := range symbols {
				latestRate, err := storage.GetLatestFundingRate(symbol, exchangeName)
				if err == nil {
					// 如果监控服务没有提供，使用数据库中的值
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

	c.JSON(http.StatusOK, gin.H{"rates": rates})
}

// getFundingRateHistory 获取资金费率历史
// GET /api/funding/history
// 查询参数：
//   - symbol: 交易对（可选）
//   - limit: 返回数量（默认100）
func getFundingRateHistory(c *gin.Context) {
	storageProv := pickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusOK, gin.H{"history": []interface{}{}})
		return
	}

	storage := storageProv.GetStorage()
	if storage == nil {
		c.JSON(http.StatusOK, gin.H{"history": []interface{}{}})
		return
	}

	// 解析查询参数
	symbol := c.Query("symbol")
	limitStr := c.DefaultQuery("limit", "100")
	limit := 100
	if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
		limit = l
		if limit > 1000 {
			limit = 1000 // 限制最大数量
		}
	}

	// 获取交易所名称
	exchangeName := ""
	if currentStatus != nil {
		exchangeName = currentStatus.Exchange
	}

	// 查询历史数据
	history, err := storage.GetFundingRateHistory(symbol, exchangeName, limit)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 转换为API响应格式
	response := make([]map[string]interface{}, len(history))
	for i, fr := range history {
		response[i] = map[string]interface{}{
			"id":         fr.ID,
			"symbol":     fr.Symbol,
			"exchange":   fr.Exchange,
			"rate":       fr.Rate,
			"rate_pct":   fr.Rate * 100, // 转换为百分比
			"timestamp":  fr.Timestamp,
			"created_at": fr.CreatedAt,
		}
	}

	c.JSON(http.StatusOK, gin.H{"history": response})
}

// ========== 市场情报数据源相关API ==========

var (
	// 数据源管理器提供者（需要从main.go注入）
	dataSourceProvider DataSourceProvider
)

// DataSourceProvider 数据源提供者接口
type DataSourceProvider interface {
	GetRSSFeeds() ([]RSSFeedInfo, error)
	GetFearGreedIndex() (*FearGreedIndexInfo, error)
	GetRedditPosts(subreddits []string, limit int) ([]RedditPostInfo, error)
	GetPolymarketMarkets(keywords []string) ([]PolymarketMarketInfo, error)
}

// RSSFeedInfo RSS源信息
type RSSFeedInfo struct {
	Title       string        `json:"title"`
	Description string        `json:"description"`
	URL         string        `json:"url"`
	Items       []RSSItemInfo `json:"items"`
	LastUpdate  time.Time     `json:"last_update"`
}

// RSSItemInfo RSS项信息
type RSSItemInfo struct {
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Link        string    `json:"link"`
	PubDate     time.Time `json:"pub_date"`
	Source      string    `json:"source"`
}

// FearGreedIndexInfo 恐慌贪婪指数信息
type FearGreedIndexInfo struct {
	Value          int       `json:"value"`
	Classification string    `json:"classification"`
	Timestamp      time.Time `json:"timestamp"`
}

// RedditPostInfo Reddit帖子信息
type RedditPostInfo struct {
	Title       string    `json:"title"`
	Content     string    `json:"content"`
	URL         string    `json:"url"`
	Subreddit   string    `json:"subreddit"`
	Score       int       `json:"score"`
	UpvoteRatio float64   `json:"upvote_ratio"`
	CreatedAt   time.Time `json:"created_at"`
	Author      string    `json:"author"`
}

// PolymarketMarketInfo Polymarket市场信息
type PolymarketMarketInfo struct {
	ID          string    `json:"id"`
	Question    string    `json:"question"`
	Description string    `json:"description"`
	EndDate     time.Time `json:"end_date"`
	Outcomes    []string  `json:"outcomes"`
	Volume      float64   `json:"volume"`
	Liquidity   float64   `json:"liquidity"`
}

// SetDataSourceProvider 设置数据源提供者
func SetDataSourceProvider(provider DataSourceProvider) {
	dataSourceProvider = provider
}

// dataSourceAdapter 数据源适配器
// 注意：这个适配器使用反射来调用方法，避免循环依赖
type dataSourceAdapter struct {
	dsm              interface{}
	rssFeeds         []string
	fearGreedAPIURL  string
	polymarketAPIURL string
}

// NewDataSourceAdapter 创建数据源适配器
// dsm 应该是 *ai.DataSourceManager 类型，但使用 interface{} 避免循环依赖
func NewDataSourceAdapter(dsm interface{}, rssFeeds []string, fearGreedAPIURL, polymarketAPIURL string) DataSourceProvider {
	return &dataSourceAdapter{
		dsm:              dsm,
		rssFeeds:         rssFeeds,
		fearGreedAPIURL:  fearGreedAPIURL,
		polymarketAPIURL: polymarketAPIURL,
	}
}

// GetRSSFeeds 获取RSS源
func (a *dataSourceAdapter) GetRSSFeeds() ([]RSSFeedInfo, error) {
	if a.dsm == nil {
		return nil, fmt.Errorf("数据源管理器未初始化")
	}

	// 使用反射调用方法（避免循环依赖）
	dsmValue := reflect.ValueOf(a.dsm)
	if !dsmValue.IsValid() {
		return nil, fmt.Errorf("无效的数据源管理器")
	}

	feeds := make([]RSSFeedInfo, 0)

	// 如果没有配置RSS源，使用默认源
	rssFeeds := a.rssFeeds
	if len(rssFeeds) == 0 {
		rssFeeds = []string{
			"https://www.coindesk.com/arc/outboundfeeds/rss/",
			"https://cointelegraph.com/rss",
			"https://cryptonews.com/news/feed/",
		}
	}

	for _, feedURL := range rssFeeds {
		method := dsmValue.MethodByName("FetchRSSFeed")
		if !method.IsValid() {
			continue
		}

		results := method.Call([]reflect.Value{reflect.ValueOf(feedURL)})
		if len(results) != 2 {
			continue
		}

		if !results[1].IsNil() {
			// 错误，跳过这个源
			continue
		}

		itemsValue := results[0]
		if itemsValue.IsNil() {
			continue
		}

		// 转换为[]NewsItem（ai包中的类型）
		items := itemsValue.Interface()
		itemsSlice := reflect.ValueOf(items)
		if itemsSlice.Kind() != reflect.Slice {
			continue
		}

		rssItems := make([]RSSItemInfo, 0)
		for i := 0; i < itemsSlice.Len(); i++ {
			item := itemsSlice.Index(i)
			if !item.IsValid() {
				continue
			}

			// 提取字段
			title := getFieldString(item, "Title")
			description := getFieldString(item, "Description")
			url := getFieldString(item, "URL")
			source := getFieldString(item, "Source")
			pubDate := getFieldTime(item, "PublishedAt")

			rssItems = append(rssItems, RSSItemInfo{
				Title:       title,
				Description: description,
				Link:        url,
				PubDate:     pubDate,
				Source:      source,
			})
		}

		if len(rssItems) > 0 {
			// 从URL提取源名称
			sourceName := extractSourceName(feedURL)
			feeds = append(feeds, RSSFeedInfo{
				Title:       sourceName,
				Description: fmt.Sprintf("来自 %s 的加密货币新闻", sourceName),
				URL:         feedURL,
				Items:       rssItems,
				LastUpdate:  time.Now(),
			})
		}
	}

	return feeds, nil
}

// GetFearGreedIndex 获取恐慌贪婪指数
func (a *dataSourceAdapter) GetFearGreedIndex() (*FearGreedIndexInfo, error) {
	if a.dsm == nil {
		return nil, fmt.Errorf("数据源管理器未初始化")
	}

	apiURL := a.fearGreedAPIURL
	if apiURL == "" {
		apiURL = "https://api.alternative.me/fng/"
	}

	dsmValue := reflect.ValueOf(a.dsm)
	method := dsmValue.MethodByName("FetchFearGreedIndex")
	if !method.IsValid() {
		return nil, fmt.Errorf("方法不存在")
	}

	results := method.Call([]reflect.Value{reflect.ValueOf(apiURL)})
	if len(results) != 2 {
		return nil, fmt.Errorf("返回值数量错误")
	}

	if !results[1].IsNil() {
		return nil, results[1].Interface().(error)
	}

	indexValue := results[0]
	if indexValue.IsNil() {
		return nil, fmt.Errorf("返回值为空")
	}

	index := indexValue.Elem()
	value := int(getFieldInt(index, "Value"))
	classification := getFieldString(index, "Classification")
	timestamp := getFieldTime(index, "Timestamp")

	return &FearGreedIndexInfo{
		Value:          value,
		Classification: classification,
		Timestamp:      timestamp,
	}, nil
}

// GetRedditPosts 获取Reddit帖子
func (a *dataSourceAdapter) GetRedditPosts(subreddits []string, limit int) ([]RedditPostInfo, error) {
	if a.dsm == nil {
		return nil, fmt.Errorf("数据源管理器未初始化")
	}

	if len(subreddits) == 0 {
		subreddits = []string{"Bitcoin", "ethereum", "CryptoCurrency", "CryptoMarkets"}
	}

	dsmValue := reflect.ValueOf(a.dsm)
	method := dsmValue.MethodByName("FetchRedditPosts")
	if !method.IsValid() {
		return nil, fmt.Errorf("方法不存在")
	}

	results := method.Call([]reflect.Value{
		reflect.ValueOf(subreddits),
		reflect.ValueOf(limit),
	})

	if len(results) != 2 {
		return nil, fmt.Errorf("返回值数量错误")
	}

	if !results[1].IsNil() {
		return nil, results[1].Interface().(error)
	}

	postsValue := results[0]
	if postsValue.IsNil() {
		return []RedditPostInfo{}, nil
	}

	postsSlice := reflect.ValueOf(postsValue.Interface())
	if postsSlice.Kind() != reflect.Slice {
		return []RedditPostInfo{}, nil
	}

	posts := make([]RedditPostInfo, 0)
	for i := 0; i < postsSlice.Len(); i++ {
		post := postsSlice.Index(i)
		if !post.IsValid() {
			continue
		}

		posts = append(posts, RedditPostInfo{
			Title:       getFieldString(post, "Title"),
			Content:     getFieldString(post, "Content"),
			URL:         getFieldString(post, "URL"),
			Subreddit:   getFieldString(post, "Subreddit"),
			Score:       int(getFieldInt(post, "Score")),
			UpvoteRatio: getFieldFloat(post, "UpvoteRatio"),
			CreatedAt:   getFieldTime(post, "CreatedAt"),
			Author:      getFieldString(post, "Author"),
		})
	}

	return posts, nil
}

// GetPolymarketMarkets 获取Polymarket市场
func (a *dataSourceAdapter) GetPolymarketMarkets(keywords []string) ([]PolymarketMarketInfo, error) {
	if a.dsm == nil {
		return nil, fmt.Errorf("数据源管理器未初始化")
	}

	apiURL := a.polymarketAPIURL
	if apiURL == "" {
		apiURL = "https://api.polymarket.com/graphql"
	}

	dsmValue := reflect.ValueOf(a.dsm)
	method := dsmValue.MethodByName("FetchPolymarketMarkets")
	if !method.IsValid() {
		return nil, fmt.Errorf("方法不存在")
	}

	results := method.Call([]reflect.Value{
		reflect.ValueOf(apiURL),
		reflect.ValueOf(keywords),
	})

	if len(results) != 2 {
		return nil, fmt.Errorf("返回值数量错误")
	}

	if !results[1].IsNil() {
		return nil, results[1].Interface().(error)
	}

	marketsValue := results[0]
	if marketsValue.IsNil() {
		return []PolymarketMarketInfo{}, nil
	}

	marketsSlice := reflect.ValueOf(marketsValue.Interface())
	if marketsSlice.Kind() != reflect.Slice {
		return []PolymarketMarketInfo{}, nil
	}

	markets := make([]PolymarketMarketInfo, 0)
	for i := 0; i < marketsSlice.Len(); i++ {
		market := marketsSlice.Index(i)
		if !market.IsValid() {
			continue
		}

		// 处理指针类型
		if market.Kind() == reflect.Ptr {
			market = market.Elem()
		}

		outcomesValue := market.FieldByName("Outcomes")
		outcomes := []string{}
		if outcomesValue.IsValid() && outcomesValue.Kind() == reflect.Slice {
			for j := 0; j < outcomesValue.Len(); j++ {
				outcomes = append(outcomes, outcomesValue.Index(j).String())
			}
		}

		markets = append(markets, PolymarketMarketInfo{
			ID:          getFieldString(market, "ID"),
			Question:    getFieldString(market, "Question"),
			Description: getFieldString(market, "Description"),
			EndDate:     getFieldTime(market, "EndDate"),
			Outcomes:    outcomes,
			Volume:      getFieldFloat(market, "Volume"),
			Liquidity:   getFieldFloat(market, "Liquidity"),
		})
	}

	return markets, nil
}

// 辅助函数：从反射值获取字符串字段
func getFieldString(v reflect.Value, fieldName string) string {
	field := v.FieldByName(fieldName)
	if !field.IsValid() {
		return ""
	}
	return field.String()
}

// 辅助函数：从反射值获取整数字段
func getFieldInt(v reflect.Value, fieldName string) int64 {
	field := v.FieldByName(fieldName)
	if !field.IsValid() {
		return 0
	}
	return field.Int()
}

// 辅助函数：从反射值获取浮点数字段
func getFieldFloat(v reflect.Value, fieldName string) float64 {
	field := v.FieldByName(fieldName)
	if !field.IsValid() {
		return 0
	}
	return field.Float()
}

// 辅助函数：从反射值获取时间字段
func getFieldTime(v reflect.Value, fieldName string) time.Time {
	field := v.FieldByName(fieldName)
	if !field.IsValid() {
		return time.Now()
	}
	if t, ok := field.Interface().(time.Time); ok {
		return t
	}
	return time.Now()
}

// 辅助函数：从URL提取源名称
func extractSourceName(url string) string {
	url = strings.TrimPrefix(url, "https://")
	url = strings.TrimPrefix(url, "http://")
	parts := strings.Split(url, "/")
	if len(parts) > 0 {
		return parts[0]
	}
	return url
}

// getMarketIntelligence 获取市场情报数据
// GET /api/market-intelligence
// 查询参数：
//   - source: 数据源类型（rss, fear_greed, reddit, polymarket，默认全部）
//   - keyword: 搜索关键词（可选）
//   - limit: 返回数量限制（默认50）
func getMarketIntelligence(c *gin.Context) {
	if dataSourceProvider == nil {
		c.JSON(http.StatusOK, gin.H{
			"rss_feeds":    []interface{}{},
			"fear_greed":   nil,
			"reddit_posts": []interface{}{},
			"polymarket":   []interface{}{},
		})
		return
	}

	source := c.Query("source")
	keyword := c.Query("keyword")
	limitStr := c.DefaultQuery("limit", "50")
	limit := 50
	if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
		limit = l
		if limit > 200 {
			limit = 200 // 最大限制200
		}
	}

	result := make(map[string]interface{})

	// 获取RSS新闻
	if source == "" || source == "rss" {
		rssFeeds, err := dataSourceProvider.GetRSSFeeds()
		if err == nil {
			// 如果有关键词，进行筛选
			if keyword != "" {
				filtered := make([]RSSFeedInfo, 0)
				keywordLower := strings.ToLower(keyword)
				for _, feed := range rssFeeds {
					filteredItems := make([]RSSItemInfo, 0)
					for _, item := range feed.Items {
						titleLower := strings.ToLower(item.Title)
						descLower := strings.ToLower(item.Description)
						if strings.Contains(titleLower, keywordLower) || strings.Contains(descLower, keywordLower) {
							filteredItems = append(filteredItems, item)
						}
					}
					if len(filteredItems) > 0 {
						feed.Items = filteredItems[:min(len(filteredItems), limit)]
						filtered = append(filtered, feed)
					}
				}
				result["rss_feeds"] = filtered
			} else {
				// 限制每个源的条目数
				for i := range rssFeeds {
					if len(rssFeeds[i].Items) > limit {
						rssFeeds[i].Items = rssFeeds[i].Items[:limit]
					}
				}
				result["rss_feeds"] = rssFeeds
			}
		} else {
			result["rss_feeds"] = []interface{}{}
		}
	}

	// 获取恐慌贪婪指数
	if source == "" || source == "fear_greed" {
		fearGreed, err := dataSourceProvider.GetFearGreedIndex()
		if err == nil {
			result["fear_greed"] = fearGreed
		} else {
			result["fear_greed"] = nil
		}
	}

	// 获取Reddit帖子
	if source == "" || source == "reddit" {
		// 默认子版块
		subreddits := []string{"Bitcoin", "ethereum", "CryptoCurrency", "CryptoMarkets"}
		redditPosts, err := dataSourceProvider.GetRedditPosts(subreddits, limit)
		if err == nil {
			// 如果有关键词，进行筛选
			if keyword != "" {
				filtered := make([]RedditPostInfo, 0)
				keywordLower := strings.ToLower(keyword)
				for _, post := range redditPosts {
					titleLower := strings.ToLower(post.Title)
					contentLower := strings.ToLower(post.Content)
					if strings.Contains(titleLower, keywordLower) || strings.Contains(contentLower, keywordLower) {
						filtered = append(filtered, post)
					}
				}
				result["reddit_posts"] = filtered[:min(len(filtered), limit)]
			} else {
				result["reddit_posts"] = redditPosts
			}
		} else {
			result["reddit_posts"] = []interface{}{}
		}
	}

	// 获取Polymarket市场
	if source == "" || source == "polymarket" {
		keywords := []string{}
		if keyword != "" {
			keywords = []string{keyword}
		}
		polymarketMarkets, err := dataSourceProvider.GetPolymarketMarkets(keywords)
		if err == nil {
			if len(polymarketMarkets) > limit {
				result["polymarket"] = polymarketMarkets[:limit]
			} else {
				result["polymarket"] = polymarketMarkets
			}
		} else {
			result["polymarket"] = []interface{}{}
		}
	}

	c.JSON(http.StatusOK, result)
}

// min 返回两个整数中的较小值
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ========== AI分析相关API ==========

var (
	// AI模块提供者（需要从main.go注入）
	aiMarketAnalyzerProvider     AIMarketAnalyzerProvider
	aiParameterOptimizerProvider AIParameterOptimizerProvider
	aiRiskAnalyzerProvider       AIRiskAnalyzerProvider
	aiSentimentAnalyzerProvider  AISentimentAnalyzerProvider
	aiPolymarketSignalProvider   AIPolymarketSignalProvider
	aiPromptManagerProvider      AIPromptManagerProvider
)

// AI提供者接口
type AIMarketAnalyzerProvider interface {
	GetLastAnalysis() interface{}
	GetLastAnalysisTime() time.Time
	PerformAnalysis() error
}

type AIParameterOptimizerProvider interface {
	GetLastOptimization() interface{}
	GetLastOptimizationTime() time.Time
	PerformOptimization() error
}

type AIRiskAnalyzerProvider interface {
	GetLastAnalysis() interface{}
	GetLastAnalysisTime() time.Time
	PerformAnalysis() error
}

type AISentimentAnalyzerProvider interface {
	GetLastAnalysis() interface{}
	GetLastAnalysisTime() time.Time
	PerformAnalysis() error
}

type AIPolymarketSignalProvider interface {
	GetLastAnalysis() interface{}
	GetLastAnalysisTime() time.Time
	PerformAnalysis() error
}

type AIPromptManagerProvider interface {
	GetAllPrompts() (map[string]interface{}, error)
	UpdatePrompt(module, template, systemPrompt string) error
}

// SetAIProviders 设置AI提供者
func SetAIMarketAnalyzerProvider(provider AIMarketAnalyzerProvider) {
	aiMarketAnalyzerProvider = provider
}

func SetAIParameterOptimizerProvider(provider AIParameterOptimizerProvider) {
	aiParameterOptimizerProvider = provider
}

func SetAIRiskAnalyzerProvider(provider AIRiskAnalyzerProvider) {
	aiRiskAnalyzerProvider = provider
}

func SetAISentimentAnalyzerProvider(provider AISentimentAnalyzerProvider) {
	aiSentimentAnalyzerProvider = provider
}

func SetAIPolymarketSignalProvider(provider AIPolymarketSignalProvider) {
	aiPolymarketSignalProvider = provider
}

func SetAIPromptManagerProvider(provider AIPromptManagerProvider) {
	aiPromptManagerProvider = provider
}

// getAIAnalysisStatus 获取AI系统状态
// GET /api/ai/status
func getAIAnalysisStatus(c *gin.Context) {
	status := map[string]interface{}{
		"enabled": true,
		"modules": map[string]interface{}{
			"market_analysis": map[string]interface{}{
				"enabled":     aiMarketAnalyzerProvider != nil,
				"last_update": nil,
				"has_data":    false,
			},
			"parameter_optimization": map[string]interface{}{
				"enabled":     aiParameterOptimizerProvider != nil,
				"last_update": nil,
				"has_data":    false,
			},
			"risk_analysis": map[string]interface{}{
				"enabled":     aiRiskAnalyzerProvider != nil,
				"last_update": nil,
				"has_data":    false,
			},
			"sentiment_analysis": map[string]interface{}{
				"enabled":     aiSentimentAnalyzerProvider != nil,
				"last_update": nil,
				"has_data":    false,
			},
			"polymarket_signal": map[string]interface{}{
				"enabled":     aiPolymarketSignalProvider != nil,
				"last_update": nil,
				"has_data":    false,
			},
		},
	}

	// 更新各模块状态
	if aiMarketAnalyzerProvider != nil {
		lastTime := aiMarketAnalyzerProvider.GetLastAnalysisTime()
		lastAnalysis := aiMarketAnalyzerProvider.GetLastAnalysis()
		status["modules"].(map[string]interface{})["market_analysis"].(map[string]interface{})["last_update"] = lastTime
		status["modules"].(map[string]interface{})["market_analysis"].(map[string]interface{})["has_data"] = lastAnalysis != nil
	}

	if aiParameterOptimizerProvider != nil {
		lastTime := aiParameterOptimizerProvider.GetLastOptimizationTime()
		lastOptimization := aiParameterOptimizerProvider.GetLastOptimization()
		status["modules"].(map[string]interface{})["parameter_optimization"].(map[string]interface{})["last_update"] = lastTime
		status["modules"].(map[string]interface{})["parameter_optimization"].(map[string]interface{})["has_data"] = lastOptimization != nil
	}

	if aiRiskAnalyzerProvider != nil {
		lastTime := aiRiskAnalyzerProvider.GetLastAnalysisTime()
		lastAnalysis := aiRiskAnalyzerProvider.GetLastAnalysis()
		status["modules"].(map[string]interface{})["risk_analysis"].(map[string]interface{})["last_update"] = lastTime
		status["modules"].(map[string]interface{})["risk_analysis"].(map[string]interface{})["has_data"] = lastAnalysis != nil
	}

	if aiSentimentAnalyzerProvider != nil {
		lastTime := aiSentimentAnalyzerProvider.GetLastAnalysisTime()
		lastAnalysis := aiSentimentAnalyzerProvider.GetLastAnalysis()
		status["modules"].(map[string]interface{})["sentiment_analysis"].(map[string]interface{})["last_update"] = lastTime
		status["modules"].(map[string]interface{})["sentiment_analysis"].(map[string]interface{})["has_data"] = lastAnalysis != nil
	}

	if aiPolymarketSignalProvider != nil {
		lastTime := aiPolymarketSignalProvider.GetLastAnalysisTime()
		lastAnalysis := aiPolymarketSignalProvider.GetLastAnalysis()
		status["modules"].(map[string]interface{})["polymarket_signal"].(map[string]interface{})["last_update"] = lastTime
		status["modules"].(map[string]interface{})["polymarket_signal"].(map[string]interface{})["has_data"] = lastAnalysis != nil
	}

	c.JSON(http.StatusOK, status)
}

// getAIMarketAnalysis 获取市场分析结果
// GET /api/ai/analysis/market
func getAIMarketAnalysis(c *gin.Context) {
	if aiMarketAnalyzerProvider == nil {
		c.JSON(http.StatusOK, gin.H{"error": "市场分析模块未启用"})
		return
	}

	analysis := aiMarketAnalyzerProvider.GetLastAnalysis()
	if analysis == nil {
		c.JSON(http.StatusOK, gin.H{"error": "暂无分析数据"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"analysis": analysis, "last_update": aiMarketAnalyzerProvider.GetLastAnalysisTime()})
}

// getAIParameterOptimization 获取参数优化结果
// GET /api/ai/analysis/parameter
func getAIParameterOptimization(c *gin.Context) {
	if aiParameterOptimizerProvider == nil {
		c.JSON(http.StatusOK, gin.H{"error": "参数优化模块未启用"})
		return
	}

	optimization := aiParameterOptimizerProvider.GetLastOptimization()
	if optimization == nil {
		c.JSON(http.StatusOK, gin.H{"error": "暂无优化数据"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"optimization": optimization, "last_update": aiParameterOptimizerProvider.GetLastOptimizationTime()})
}

// getAIRiskAnalysis 获取风险分析结果
// GET /api/ai/analysis/risk
func getAIRiskAnalysis(c *gin.Context) {
	if aiRiskAnalyzerProvider == nil {
		c.JSON(http.StatusOK, gin.H{"error": "风险分析模块未启用"})
		return
	}

	analysis := aiRiskAnalyzerProvider.GetLastAnalysis()
	if analysis == nil {
		c.JSON(http.StatusOK, gin.H{"error": "暂无分析数据"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"analysis": analysis, "last_update": aiRiskAnalyzerProvider.GetLastAnalysisTime()})
}

// getAISentimentAnalysis 获取情绪分析结果
// GET /api/ai/analysis/sentiment
func getAISentimentAnalysis(c *gin.Context) {
	if aiSentimentAnalyzerProvider == nil {
		c.JSON(http.StatusOK, gin.H{"error": "情绪分析模块未启用"})
		return
	}

	analysis := aiSentimentAnalyzerProvider.GetLastAnalysis()
	if analysis == nil {
		c.JSON(http.StatusOK, gin.H{"error": "暂无分析数据"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"analysis": analysis, "last_update": aiSentimentAnalyzerProvider.GetLastAnalysisTime()})
}

// getAIPolymarketSignal 获取Polymarket信号分析结果
// GET /api/ai/analysis/polymarket
func getAIPolymarketSignal(c *gin.Context) {
	if aiPolymarketSignalProvider == nil {
		c.JSON(http.StatusOK, gin.H{"error": "Polymarket信号模块未启用"})
		return
	}

	analysis := aiPolymarketSignalProvider.GetLastAnalysis()
	if analysis == nil {
		c.JSON(http.StatusOK, gin.H{"error": "暂无分析数据"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"analysis": analysis, "last_update": aiPolymarketSignalProvider.GetLastAnalysisTime()})
}

// triggerAIAnalysis 手动触发AI分析
// POST /api/ai/analysis/trigger/:module
func triggerAIAnalysis(c *gin.Context) {
	module := c.Param("module")
	var err error

	switch module {
	case "market":
		if aiMarketAnalyzerProvider != nil {
			err = aiMarketAnalyzerProvider.PerformAnalysis()
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"error": "市场分析模块未启用"})
			return
		}
	case "parameter":
		if aiParameterOptimizerProvider != nil {
			err = aiParameterOptimizerProvider.PerformOptimization()
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"error": "参数优化模块未启用"})
			return
		}
	case "risk":
		if aiRiskAnalyzerProvider != nil {
			err = aiRiskAnalyzerProvider.PerformAnalysis()
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"error": "风险分析模块未启用"})
			return
		}
	case "sentiment":
		if aiSentimentAnalyzerProvider != nil {
			err = aiSentimentAnalyzerProvider.PerformAnalysis()
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"error": "情绪分析模块未启用"})
			return
		}
	case "polymarket":
		if aiPolymarketSignalProvider != nil {
			err = aiPolymarketSignalProvider.PerformAnalysis()
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Polymarket信号模块未启用"})
			return
		}
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "未知的模块: " + module})
		return
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "分析已触发"})
}

// getAIPrompts 获取所有提示词模板
// GET /api/ai/prompts
func getAIPrompts(c *gin.Context) {
	if aiPromptManagerProvider == nil {
		c.JSON(http.StatusOK, gin.H{"prompts": map[string]interface{}{}})
		return
	}

	prompts, err := aiPromptManagerProvider.GetAllPrompts()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"prompts": prompts})
}

// updateAIPrompt 更新提示词模板
// POST /api/ai/prompts
func updateAIPrompt(c *gin.Context) {
	if aiPromptManagerProvider == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "提示词管理器未启用"})
		return
	}

	var req struct {
		Module       string `json:"module"`
		Template     string `json:"template"`
		SystemPrompt string `json:"system_prompt"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Module == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "模块名不能为空"})
		return
	}

	if req.Template == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "提示词模板不能为空"})
		return
	}

	if err := aiPromptManagerProvider.UpdatePrompt(req.Module, req.Template, req.SystemPrompt); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "提示词已更新"})
}

// AI模块适配器
type aiMarketAnalyzerAdapter struct {
	analyzer interface {
		GetLastAnalysis() interface{}
		GetLastAnalysisTime() time.Time
		PerformAnalysis() error
	}
}

func (a *aiMarketAnalyzerAdapter) GetLastAnalysis() interface{} {
	return a.analyzer.GetLastAnalysis()
}

func (a *aiMarketAnalyzerAdapter) GetLastAnalysisTime() time.Time {
	return a.analyzer.GetLastAnalysisTime()
}

func (a *aiMarketAnalyzerAdapter) PerformAnalysis() error {
	return a.analyzer.PerformAnalysis()
}

type aiParameterOptimizerAdapter struct {
	optimizer interface {
		GetLastOptimization() interface{}
		GetLastOptimizationTime() time.Time
		PerformOptimization() error
	}
}

func (a *aiParameterOptimizerAdapter) GetLastOptimization() interface{} {
	return a.optimizer.GetLastOptimization()
}

func (a *aiParameterOptimizerAdapter) GetLastOptimizationTime() time.Time {
	return a.optimizer.GetLastOptimizationTime()
}

func (a *aiParameterOptimizerAdapter) PerformOptimization() error {
	return a.optimizer.PerformOptimization()
}

type aiRiskAnalyzerAdapter struct {
	analyzer interface {
		GetLastAnalysis() interface{}
		GetLastAnalysisTime() time.Time
		PerformAnalysis() error
	}
}

func (a *aiRiskAnalyzerAdapter) GetLastAnalysis() interface{} {
	return a.analyzer.GetLastAnalysis()
}

func (a *aiRiskAnalyzerAdapter) GetLastAnalysisTime() time.Time {
	return a.analyzer.GetLastAnalysisTime()
}

func (a *aiRiskAnalyzerAdapter) PerformAnalysis() error {
	return a.analyzer.PerformAnalysis()
}

type aiSentimentAnalyzerAdapter struct {
	analyzer interface {
		GetLastAnalysis() interface{}
		GetLastAnalysisTime() time.Time
		PerformAnalysis() error
	}
}

func (a *aiSentimentAnalyzerAdapter) GetLastAnalysis() interface{} {
	return a.analyzer.GetLastAnalysis()
}

func (a *aiSentimentAnalyzerAdapter) GetLastAnalysisTime() time.Time {
	return a.analyzer.GetLastAnalysisTime()
}

func (a *aiSentimentAnalyzerAdapter) PerformAnalysis() error {
	return a.analyzer.PerformAnalysis()
}

type aiPolymarketSignalAdapter struct {
	analyzer interface {
		GetLastAnalysis() interface{}
		GetLastAnalysisTime() time.Time
		PerformAnalysis() error
	}
}

func (a *aiPolymarketSignalAdapter) GetLastAnalysis() interface{} {
	return a.analyzer.GetLastAnalysis()
}

func (a *aiPolymarketSignalAdapter) GetLastAnalysisTime() time.Time {
	return a.analyzer.GetLastAnalysisTime()
}

func (a *aiPolymarketSignalAdapter) PerformAnalysis() error {
	return a.analyzer.PerformAnalysis()
}

type aiPromptManagerAdapter struct {
	manager interface {
		GetAllPrompts() (map[string]interface{}, error)
		UpdatePrompt(module, template, systemPrompt string) error
	}
}

func (a *aiPromptManagerAdapter) GetAllPrompts() (map[string]interface{}, error) {
	return a.manager.GetAllPrompts()
}

func (a *aiPromptManagerAdapter) UpdatePrompt(module, template, systemPrompt string) error {
	return a.manager.UpdatePrompt(module, template, systemPrompt)
}

// ==================== 价差监控 API ====================

// BasisMonitorProvider 价差监控提供者接口
type BasisMonitorProvider interface {
	GetCurrentBasis(symbol string) (*storage.BasisData, error)
	GetAllCurrentBasis() []*storage.BasisData
	GetBasisHistory(symbol string, limit int) ([]*storage.BasisData, error)
	GetBasisStatistics(symbol string, hours int) (*storage.BasisStats, error)
}

var (
	basisMonitorProvider BasisMonitorProvider
	basisMonitorMu       sync.RWMutex
)

// SetBasisMonitorProvider 设置价差监控提供者
func SetBasisMonitorProvider(provider BasisMonitorProvider) {
	basisMonitorMu.Lock()
	defer basisMonitorMu.Unlock()
	basisMonitorProvider = provider
}

// getBasisMonitorProvider 获取价差监控提供者
func getBasisMonitorProvider() BasisMonitorProvider {
	basisMonitorMu.RLock()
	defer basisMonitorMu.RUnlock()
	return basisMonitorProvider
}

// getBasisCurrent 获取当前价差数据
// GET /api/basis/current?symbol=BTCUSDT
func getBasisCurrent(c *gin.Context) {
	provider := getBasisMonitorProvider()
	if provider == nil {
		respondError(c, http.StatusServiceUnavailable, "errors.service_unavailable")
		return
	}

	symbol := c.Query("symbol")
	if symbol == "" {
		// 如果没有指定交易对，返回所有交易对的当前价差
		allBasis := provider.GetAllCurrentBasis()
		c.JSON(http.StatusOK, gin.H{
			"data":  allBasis,
			"count": len(allBasis),
		})
		return
	}

	// 获取指定交易对的价差
	data, err := provider.GetCurrentBasis(symbol)
	if err != nil {
		respondError(c, http.StatusNotFound, "errors.not_found", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": data})
}

// getBasisHistory 获取价差历史数据
// GET /api/basis/history?symbol=BTCUSDT&limit=100
func getBasisHistory(c *gin.Context) {
	provider := getBasisMonitorProvider()
	if provider == nil {
		respondError(c, http.StatusServiceUnavailable, "errors.service_unavailable")
		return
	}

	symbol := c.Query("symbol")
	if symbol == "" {
		respondError(c, http.StatusBadRequest, "errors.missing_parameter",
			map[string]interface{}{"param": "symbol"})
		return
	}

	limit := 100
	if limitStr := c.Query("limit"); limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
			limit = l
		}
	}

	history, err := provider.GetBasisHistory(symbol, limit)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "errors.internal_error", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"data":  history,
		"count": len(history),
	})
}

// getBasisStatistics 获取价差统计数据
// GET /api/basis/statistics?symbol=BTCUSDT&hours=24
func getBasisStatistics(c *gin.Context) {
	provider := getBasisMonitorProvider()
	if provider == nil {
		respondError(c, http.StatusServiceUnavailable, "errors.service_unavailable")
		return
	}

	symbol := c.Query("symbol")
	if symbol == "" {
		respondError(c, http.StatusBadRequest, "errors.missing_parameter",
			map[string]interface{}{"param": "symbol"})
		return
	}

	hours := 24
	if hoursStr := c.Query("hours"); hoursStr != "" {
		if h, err := strconv.Atoi(hoursStr); err == nil && h > 0 {
			hours = h
		}
	}

	stats, err := provider.GetBasisStatistics(symbol, hours)
	if err != nil {
		respondError(c, http.StatusInternalServerError, "errors.internal_error", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{"data": stats})
}

// getAllocationStatus 获取资金分配状态
// GET /api/allocation/status
func getAllocationStatus(c *gin.Context) {
	if symbolManagerProvider == nil {
		respondError(c, http.StatusServiceUnavailable, "error.symbol_manager_unavailable")
		return
	}

	// 获取所有运行中的交易对
	runtimes := symbolManagerProvider.List()
	
	allStatuses := make([]map[string]interface{}, 0)
	
	for _, rt := range runtimes {
		// 使用反射获取 AllocationManager
		rtVal := reflect.ValueOf(rt)
		if rtVal.Kind() == reflect.Ptr {
			rtVal = rtVal.Elem()
		}
		
		// 尝试获取 PositionManager
		posManagerField := rtVal.FieldByName("PositionManager")
		if !posManagerField.IsValid() || posManagerField.IsNil() {
			continue
		}
		
		posManager := posManagerField.Interface()
		posManagerVal := reflect.ValueOf(posManager)
		if posManagerVal.Kind() == reflect.Ptr {
			posManagerVal = posManagerVal.Elem()
		}
		
		// 获取 allocationManager
		allocManagerField := posManagerVal.FieldByName("allocationManager")
		if !allocManagerField.IsValid() || allocManagerField.IsNil() {
			continue
		}
		
		// 调用 GetAllStatuses 方法
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

// getAllocationStatusBySymbol 获取指定交易对的资金分配状态
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
	
	// 获取指定的运行时
	rtInterface, exists := symbolManagerProvider.Get(exchange, symbol)
	if !exists {
		respondError(c, http.StatusNotFound, "error.symbol_not_found")
		return
	}
	
	// 使用反射获取 AllocationManager
	rtVal := reflect.ValueOf(rtInterface)
	if rtVal.Kind() == reflect.Ptr {
		rtVal = rtVal.Elem()
	}
	
	// 尝试获取 PositionManager
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
	
	// 获取 allocationManager
	allocManagerField := posManagerVal.FieldByName("allocationManager")
	if !allocManagerField.IsValid() || allocManagerField.IsNil() {
		respondError(c, http.StatusInternalServerError, "error.allocation_manager_unavailable")
		return
	}
	
	// 调用 GetStatus 方法
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

// SymbolCapitalRequest 币种资金配置请求
type SymbolCapitalRequest struct {
	Symbol  string  `json:"symbol"`
	Capital float64 `json:"capital"`
}

// generateAIConfig 生成 AI 配置建议
// POST /api/ai/generate-config
func generateAIConfig(c *gin.Context) {
	var req struct {
		Exchange       string                 `json:"exchange"`
		Symbols        []string               `json:"symbols"`
		TotalCapital   float64                `json:"total_capital"`
		SymbolCapitals []SymbolCapitalRequest `json:"symbol_capitals"`
		CapitalMode    string                 `json:"capital_mode"` // total 或 per_symbol
		RiskProfile    string                 `json:"risk_profile"`
		GeminiAPIKey   string                 `json:"gemini_api_key"` // 可选，前端传入的 API Key
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "error.invalid_request", err)
		return
	}

	// 获取 Gemini API Key
	// 优先使用请求中传入的 Key，否则使用配置文件中的 Key
	geminiAPIKey := req.GeminiAPIKey
	
	if geminiAPIKey == "" {
	if configManager == nil {
		respondError(c, http.StatusInternalServerError, "error.config_manager_unavailable")
		return
	}

	cfg, err := configManager.GetConfig()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "error.config_load_failed", err)
		return
	}

	// 获取 Gemini API Key（优先使用 gemini_api_key，否则使用 api_key）
		geminiAPIKey = cfg.AI.GeminiAPIKey
	if geminiAPIKey == "" {
		geminiAPIKey = cfg.AI.APIKey
	}
	}
	
	if geminiAPIKey == "" {
		respondError(c, http.StatusBadRequest, "error.gemini_api_key_not_configured")
		return
	}

	// 获取当前价格
	currentPrices := make(map[string]float64)
	if symbolManagerProvider != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// 尝试从运行中的交易对获取价格
		for _, symbol := range req.Symbols {
			rtInterface, exists := symbolManagerProvider.Get(req.Exchange, symbol)
			if exists {
				// 使用反射获取 PriceMonitor
				rtVal := reflect.ValueOf(rtInterface)
				if rtVal.Kind() == reflect.Ptr {
					rtVal = rtVal.Elem()
				}

				priceMonitorField := rtVal.FieldByName("PriceMonitor")
				if priceMonitorField.IsValid() && !priceMonitorField.IsNil() {
					priceMonitor := priceMonitorField.Interface()
					// 尝试调用 GetLastPrice 方法
					getPriceMethod := reflect.ValueOf(priceMonitor).MethodByName("GetLastPrice")
					if getPriceMethod.IsValid() {
						results := getPriceMethod.Call(nil)
						if len(results) > 0 {
							if price, ok := results[0].Interface().(float64); ok && price > 0 {
								currentPrices[symbol] = price
								continue
							}
						}
					}
				}

				// 如果 PriceMonitor 不可用，尝试从 Exchange 获取
				exchangeField := rtVal.FieldByName("Exchange")
				if exchangeField.IsValid() && !exchangeField.IsNil() {
					ex := exchangeField.Interface()
					if exchange, ok := ex.(exchange.IExchange); ok {
						if price, err := exchange.GetLatestPrice(ctx, symbol); err == nil && price > 0 {
							currentPrices[symbol] = price
							continue
						}
					}
				}
			}
		}
	}

	// 如果某些币种没有获取到价格，记录警告但不阻止继续
	if len(currentPrices) < len(req.Symbols) {
		logger.Warn("⚠️ 部分币种未能获取到价格，将使用默认值")
	}

	// 转换 SymbolCapitals 格式
	var symbolCapitals []ai.SymbolCapitalConfig
	for _, sc := range req.SymbolCapitals {
		symbolCapitals = append(symbolCapitals, ai.SymbolCapitalConfig{
			Symbol:  sc.Symbol,
			Capital: sc.Capital,
		})
	}

	// 确定资金模式，默认为 total
	capitalMode := req.CapitalMode
	if capitalMode == "" {
		capitalMode = "total"
	}

	// 调用 Gemini API
	geminiClient := ai.NewGeminiClient(geminiAPIKey)
	aiConfig, err := geminiClient.GenerateConfig(c.Request.Context(), &ai.GenerateConfigRequest{
		Exchange:       req.Exchange,
		Symbols:        req.Symbols,
		TotalCapital:   req.TotalCapital,
		SymbolCapitals: symbolCapitals,
		CapitalMode:    capitalMode,
		RiskProfile:    req.RiskProfile,
		CurrentPrices:  currentPrices,
	})

	if err != nil {
		logger.Error("❌ AI 配置生成失败: %v", err)
		respondError(c, http.StatusInternalServerError, "error.ai_generation_failed", err)
		return
	}

	// 计算总资金用于验证
	totalCapital := req.TotalCapital
	if capitalMode == "per_symbol" && len(symbolCapitals) > 0 {
		totalCapital = 0
		for _, sc := range symbolCapitals {
			totalCapital += sc.Capital
		}
	}

	// 验证配置
	configPath := configManager.GetConfigPath()
	configService := ai.NewConfigService(configPath)
	if err := configService.ValidateAIConfig(aiConfig, totalCapital); err != nil {
		respondError(c, http.StatusBadRequest, "error.invalid_ai_config", err)
		return
	}

	c.JSON(http.StatusOK, aiConfig)
}

// applyAIConfig 应用 AI 配置
// POST /api/ai/apply-config
func applyAIConfig(c *gin.Context) {
	var req ai.GenerateConfigResponse

	if err := c.ShouldBindJSON(&req); err != nil {
		respondError(c, http.StatusBadRequest, "error.invalid_request", err)
		return
	}

	if configManager == nil {
		respondError(c, http.StatusInternalServerError, "error.config_manager_unavailable")
		return
	}

	cfg, err := configManager.GetConfig()
	if err != nil {
		respondError(c, http.StatusInternalServerError, "error.config_load_failed", err)
		return
	}

	configPath := configManager.GetConfigPath()
	configService := ai.NewConfigService(configPath)
	if err := configService.ApplyAIConfig(&req, cfg); err != nil {
		logger.Error("❌ 应用 AI 配置失败: %v", err)
		respondError(c, http.StatusInternalServerError, "error.apply_config_failed", err)
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "配置已成功应用，请重启服务使配置生效",
	})
}
