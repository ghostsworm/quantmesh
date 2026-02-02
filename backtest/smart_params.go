package backtest

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"sort"
	"sync"
	"time"

	"quantmesh/exchange"
	"quantmesh/logger"
)

// SmartParamsService 智能參數推薦服務
// 根據當前市場價格和波動率自動生成最優回測參數
type SmartParamsService struct {
	mu sync.RWMutex

	// 價格緩存 (symbol -> price info)
	priceCache map[string]*PriceInfo

	// 波動率緩存 (symbol -> volatility info)
	volatilityCache map[string]*VolatilityInfo

	// 交易所適配器工廠
	exchangeFactory func(exchange, marketType string) (exchange.IExchange, error)

	// 配置
	config SmartParamsConfig
}

// PriceInfo 價格信息
type PriceInfo struct {
	Symbol       string    `json:"symbol"`
	CurrentPrice float64   `json:"current_price"`
	High24h      float64   `json:"high_24h"`
	Low24h       float64   `json:"low_24h"`
	UpdatedAt    time.Time `json:"updated_at"`
}

// VolatilityInfo 波動率信息
type VolatilityInfo struct {
	Symbol         string    `json:"symbol"`
	Volatility7d   float64   `json:"volatility_7d"`   // 7天波動率 (%)
	Volatility30d  float64   `json:"volatility_30d"`  // 30天波動率 (%)
	AverageRange   float64   `json:"average_range"`   // 平均日振幅 (%)
	TrendDirection string    `json:"trend_direction"` // up, down, sideways
	UpdatedAt      time.Time `json:"updated_at"`
}

// SmartParamsConfig 智能參數服務配置
type SmartParamsConfig struct {
	// 價格緩存過期時間
	PriceCacheTTL time.Duration
	// 波動率緩存過期時間
	VolatilityCacheTTL time.Duration
}

// SmartParamsRecommendation 智能參數推薦結果
type SmartParamsRecommendation struct {
	Symbol       string                 `json:"symbol"`
	Exchange     string                 `json:"exchange"`
	MarketType   string                 `json:"market_type"`
	Strategy     string                 `json:"strategy"`
	CurrentPrice float64                `json:"current_price"`
	Volatility   *VolatilityInfo        `json:"volatility"`
	Params       map[string]interface{} `json:"params"`
	Reasoning    string                 `json:"reasoning"`
	Confidence   float64                `json:"confidence"` // 0-100 置信度
	GeneratedAt  time.Time              `json:"generated_at"`
}

// NewSmartParamsService 創建智能參數推薦服務
func NewSmartParamsService(
	exchangeFactory func(exchange, marketType string) (exchange.IExchange, error),
	config SmartParamsConfig,
) *SmartParamsService {
	if config.PriceCacheTTL == 0 {
		config.PriceCacheTTL = 5 * time.Minute
	}
	if config.VolatilityCacheTTL == 0 {
		config.VolatilityCacheTTL = 1 * time.Hour
	}

	return &SmartParamsService{
		priceCache:      make(map[string]*PriceInfo),
		volatilityCache: make(map[string]*VolatilityInfo),
		exchangeFactory: exchangeFactory,
		config:          config,
	}
}

// GetRecommendation 獲取智能參數推薦
func (s *SmartParamsService) GetRecommendation(
	ctx context.Context,
	exchangeName string,
	marketType string,
	symbol string,
	strategy string,
	totalCapital float64,
) (*SmartParamsRecommendation, error) {
	// 1. 獲取當前價格
	priceInfo, err := s.getCurrentPrice(ctx, exchangeName, marketType, symbol)
	if err != nil {
		return nil, fmt.Errorf("獲取當前價格失敗: %w", err)
	}

	// 2. 獲取波動率信息
	volatilityInfo, err := s.getVolatilityInfo(ctx, exchangeName, symbol)
	if err != nil {
		logger.Warn("獲取波動率信息失敗 (使用默認值): %v", err)
		volatilityInfo = s.getDefaultVolatility(symbol)
	}

	// 3. 根據策略類型生成推薦參數
	var params map[string]interface{}
	var reasoning string
	var confidence float64

	switch strategy {
	case "grid":
		params, reasoning, confidence = s.generateGridParams(priceInfo, volatilityInfo, totalCapital)
	case "momentum":
		params, reasoning, confidence = s.generateMomentumParams(priceInfo, volatilityInfo, totalCapital)
	case "mean_reversion":
		params, reasoning, confidence = s.generateMeanReversionParams(priceInfo, volatilityInfo, totalCapital)
	case "trend_following":
		params, reasoning, confidence = s.generateTrendFollowingParams(priceInfo, volatilityInfo, totalCapital)
	case "dca":
		params, reasoning, confidence = s.generateDCAParams(priceInfo, volatilityInfo, totalCapital)
	case "martingale":
		params, reasoning, confidence = s.generateMartingaleParams(priceInfo, volatilityInfo, totalCapital)
	default:
		return nil, fmt.Errorf("不支援的策略類型: %s", strategy)
	}

	return &SmartParamsRecommendation{
		Symbol:       symbol,
		Exchange:     exchangeName,
		MarketType:   marketType,
		Strategy:     strategy,
		CurrentPrice: priceInfo.CurrentPrice,
		Volatility:   volatilityInfo,
		Params:       params,
		Reasoning:    reasoning,
		Confidence:   confidence,
		GeneratedAt:  time.Now(),
	}, nil
}

// getCurrentPrice 獲取當前價格（帶緩存）
func (s *SmartParamsService) getCurrentPrice(
	ctx context.Context,
	exchangeName string,
	marketType string,
	symbol string,
) (*PriceInfo, error) {
	cacheKey := fmt.Sprintf("%s:%s:%s", exchangeName, marketType, symbol)

	// 檢查緩存
	s.mu.RLock()
	if cached, ok := s.priceCache[cacheKey]; ok {
		if time.Since(cached.UpdatedAt) < s.config.PriceCacheTTL {
			s.mu.RUnlock()
			return cached, nil
		}
	}
	s.mu.RUnlock()

	// 獲取新價格
	if s.exchangeFactory == nil {
		// 簡化模式：使用 Binance 公開 API 獲取價格（無需配置交易所）
		price, err := fetchPriceFromBinancePublic(ctx, symbol)
		if err != nil {
			return nil, fmt.Errorf("無法從 Binance 公開 API 獲取價格，請在設置中配置交易所後重試: %w", err)
		}
		priceInfo := &PriceInfo{
			Symbol:       symbol,
			CurrentPrice: price,
			High24h:      price * 1.02,
			Low24h:       price * 0.98,
			UpdatedAt:    time.Now(),
		}
		s.mu.Lock()
		s.priceCache[cacheKey] = priceInfo
		s.mu.Unlock()
		return priceInfo, nil
	}

	ex, err := s.exchangeFactory(exchangeName, marketType)
	if err != nil {
		return nil, fmt.Errorf("創建交易所適配器失敗: %w", err)
	}

	price, err := ex.GetLatestPrice(ctx, symbol)
	if err != nil {
		return nil, fmt.Errorf("獲取最新價格失敗: %w", err)
	}

	// 嘗試獲取24小時高低價
	high24h, low24h := price*1.02, price*0.98 // 默認 ±2%
	if candles, err := ex.GetHistoricalKlines(ctx, symbol, "1h", 24); err == nil && len(candles) > 0 {
		high24h = candles[0].High
		low24h = candles[0].Low
		for _, c := range candles {
			if c.High > high24h {
				high24h = c.High
			}
			if c.Low < low24h {
				low24h = c.Low
			}
		}
	}

	priceInfo := &PriceInfo{
		Symbol:       symbol,
		CurrentPrice: price,
		High24h:      high24h,
		Low24h:       low24h,
		UpdatedAt:    time.Now(),
	}

	// 更新緩存
	s.mu.Lock()
	s.priceCache[cacheKey] = priceInfo
	s.mu.Unlock()

	return priceInfo, nil
}

// fetchPriceFromBinancePublic 從 Binance 公開 API 獲取現貨價格（無需 API Key）
func fetchPriceFromBinancePublic(ctx context.Context, symbol string) (float64, error) {
	u := "https://api.binance.com/api/v3/ticker/price?symbol=" + url.QueryEscape(symbol)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, err
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("Binance API 返回 %d", resp.StatusCode)
	}
	var data struct {
		Symbol string `json:"symbol"`
		Price  string `json:"price"`
	}
	if err := json.Unmarshal(body, &data); err != nil {
		return 0, fmt.Errorf("解析價格失敗: %w", err)
	}
	var price float64
	if _, err := fmt.Sscanf(data.Price, "%f", &price); err != nil || price <= 0 {
		return 0, fmt.Errorf("無效價格: %s", data.Price)
	}
	return price, nil
}

// getVolatilityInfo 獲取波動率信息（帶緩存）
func (s *SmartParamsService) getVolatilityInfo(
	ctx context.Context,
	exchangeName string,
	symbol string,
) (*VolatilityInfo, error) {
	cacheKey := fmt.Sprintf("%s:%s", exchangeName, symbol)

	// 檢查緩存
	s.mu.RLock()
	if cached, ok := s.volatilityCache[cacheKey]; ok {
		if time.Since(cached.UpdatedAt) < s.config.VolatilityCacheTTL {
			s.mu.RUnlock()
			return cached, nil
		}
	}
	s.mu.RUnlock()

	// 計算波動率
	if s.exchangeFactory == nil {
		return nil, fmt.Errorf("交易所適配器工廠未配置")
	}

	// 使用現貨市場計算波動率（更穩定）
	ex, err := s.exchangeFactory(exchangeName, "spot")
	if err != nil {
		// 回退到合約市場
		ex, err = s.exchangeFactory(exchangeName, "futures")
		if err != nil {
			return nil, fmt.Errorf("創建交易所適配器失敗: %w", err)
		}
	}

	// 獲取30天日線數據
	candles, err := ex.GetHistoricalKlines(ctx, symbol, "1d", 30)
	if err != nil {
		return nil, fmt.Errorf("獲取歷史K線失敗: %w", err)
	}

	if len(candles) < 7 {
		return nil, fmt.Errorf("歷史數據不足")
	}

	// 計算日收益率的標準差作為波動率
	returns := make([]float64, 0, len(candles)-1)
	dailyRanges := make([]float64, 0, len(candles))

	for i := 1; i < len(candles); i++ {
		if candles[i-1].Close > 0 {
			ret := (candles[i].Close - candles[i-1].Close) / candles[i-1].Close
			returns = append(returns, ret)
		}
		if candles[i].Low > 0 {
			rangePercent := (candles[i].High - candles[i].Low) / candles[i].Low * 100
			dailyRanges = append(dailyRanges, rangePercent)
		}
	}

	// 計算7天和30天波動率
	volatility7d := s.calculateStdDev(returns[max(0, len(returns)-7):]) * math.Sqrt(365) * 100
	volatility30d := s.calculateStdDev(returns) * math.Sqrt(365) * 100

	// 計算平均日振幅
	var avgRange float64
	if len(dailyRanges) > 0 {
		for _, r := range dailyRanges {
			avgRange += r
		}
		avgRange /= float64(len(dailyRanges))
	}

	// 判斷趨勢方向
	trend := "sideways"
	if len(candles) >= 7 {
		recent := candles[len(candles)-7:]
		firstPrice := recent[0].Close
		lastPrice := recent[len(recent)-1].Close
		changePercent := (lastPrice - firstPrice) / firstPrice * 100
		if changePercent > 5 {
			trend = "up"
		} else if changePercent < -5 {
			trend = "down"
		}
	}

	volatilityInfo := &VolatilityInfo{
		Symbol:         symbol,
		Volatility7d:   volatility7d,
		Volatility30d:  volatility30d,
		AverageRange:   avgRange,
		TrendDirection: trend,
		UpdatedAt:      time.Now(),
	}

	// 更新緩存
	s.mu.Lock()
	s.volatilityCache[cacheKey] = volatilityInfo
	s.mu.Unlock()

	return volatilityInfo, nil
}

// getDefaultVolatility 獲取默認波動率（基於預設）
func (s *SmartParamsService) getDefaultVolatility(symbol string) *VolatilityInfo {
	preset := GetSymbolPreset(symbol)

	var volatility float64
	var avgRange float64

	switch preset.VolatilityType {
	case "high":
		volatility = 80
		avgRange = 4.0
	case "medium_high":
		volatility = 60
		avgRange = 3.0
	case "medium":
		volatility = 40
		avgRange = 2.0
	case "low":
		volatility = 20
		avgRange = 1.0
	default:
		volatility = 50
		avgRange = 2.5
	}

	return &VolatilityInfo{
		Symbol:         symbol,
		Volatility7d:   volatility,
		Volatility30d:  volatility,
		AverageRange:   avgRange,
		TrendDirection: "sideways",
		UpdatedAt:      time.Now(),
	}
}

// generateGridParams 生成網格策略參數
func (s *SmartParamsService) generateGridParams(
	price *PriceInfo,
	volatility *VolatilityInfo,
	totalCapital float64,
) (map[string]interface{}, string, float64) {
	currentPrice := price.CurrentPrice

	// 根據波動率確定網格範圍
	// 高波動性：較寬的網格範圍
	// 低波動性：較窄的網格範圍
	rangePercent := volatility.AverageRange * 3 // 使用3倍平均日振幅
	if rangePercent < 2 {
		rangePercent = 2
	}
	if rangePercent > 20 {
		rangePercent = 20
	}

	priceLow := currentPrice * (1 - rangePercent/100)
	priceHigh := currentPrice * (1 + rangePercent/100)

	// 根據資金規模確定網格數量
	// 每格約 100-500 USDT
	orderQuantity := 100.0
	if totalCapital >= 50000 {
		orderQuantity = 500
	} else if totalCapital >= 10000 {
		orderQuantity = 200
	}

	// 計算合理的網格數量
	gridCount := int(totalCapital / orderQuantity)
	if gridCount < 5 {
		gridCount = 5
	}
	if gridCount > 200 {
		gridCount = 200
	}

	// 計算網格間距百分比
	gridGapPercent := rangePercent * 2 / float64(gridCount)

	params := map[string]interface{}{
		"price_low":      math.Round(priceLow*100) / 100,
		"price_high":     math.Round(priceHigh*100) / 100,
		"grid_count":     gridCount,
		"order_quantity": orderQuantity,
		"total_capital":  totalCapital,
		"fee_rate":       0.0004,
	}

	reasoning := fmt.Sprintf(
		"當前價格 %.2f，7日波動率 %.1f%%，平均日振幅 %.2f%%。"+
			"建議網格範圍 %.2f - %.2f (±%.1f%%)，共 %d 格，每格間距約 %.2f%%，單筆 %.0f USDT。",
		currentPrice, volatility.Volatility7d, volatility.AverageRange,
		priceLow, priceHigh, rangePercent, gridCount, gridGapPercent, orderQuantity,
	)

	// 計算置信度
	confidence := 70.0
	if volatility.TrendDirection == "sideways" {
		confidence += 20 // 震盪市場更適合網格
	}
	if volatility.Volatility7d > 30 && volatility.Volatility7d < 100 {
		confidence += 10 // 中等波動率最適合
	}

	return params, reasoning, min(confidence, 100)
}

// generateMomentumParams 生成動量策略參數
func (s *SmartParamsService) generateMomentumParams(
	price *PriceInfo,
	volatility *VolatilityInfo,
	totalCapital float64,
) (map[string]interface{}, string, float64) {
	// RSI 週期根據波動率調整
	rsiPeriod := 14
	if volatility.Volatility7d > 60 {
		rsiPeriod = 10 // 高波動用較短週期
	} else if volatility.Volatility7d < 30 {
		rsiPeriod = 21 // 低波動用較長週期
	}

	params := map[string]interface{}{
		"rsi_period":    rsiPeriod,
		"total_capital": totalCapital,
		"fee_rate":      0.0004,
	}

	reasoning := fmt.Sprintf(
		"當前價格 %.2f，7日波動率 %.1f%%，趨勢方向 %s。"+
			"建議 RSI 週期 %d（%s波動率適用）。",
		price.CurrentPrice, volatility.Volatility7d, volatility.TrendDirection,
		rsiPeriod, s.getVolatilityLevel(volatility.Volatility7d),
	)

	confidence := 60.0
	if volatility.TrendDirection != "sideways" {
		confidence += 20 // 有趨勢時動量策略更有效
	}

	return params, reasoning, min(confidence, 100)
}

// generateMeanReversionParams 生成均值回歸策略參數
func (s *SmartParamsService) generateMeanReversionParams(
	price *PriceInfo,
	volatility *VolatilityInfo,
	totalCapital float64,
) (map[string]interface{}, string, float64) {
	// 布林帶週期根據波動率調整
	period := 20
	if volatility.Volatility7d > 60 {
		period = 14
	} else if volatility.Volatility7d < 30 {
		period = 30
	}

	params := map[string]interface{}{
		"period":        period,
		"total_capital": totalCapital,
		"fee_rate":      0.0004,
	}

	reasoning := fmt.Sprintf(
		"當前價格 %.2f，7日波動率 %.1f%%，趨勢方向 %s。"+
			"建議布林帶週期 %d（%s波動率適用）。均值回歸策略在震盪市場表現較好。",
		price.CurrentPrice, volatility.Volatility7d, volatility.TrendDirection,
		period, s.getVolatilityLevel(volatility.Volatility7d),
	)

	confidence := 60.0
	if volatility.TrendDirection == "sideways" {
		confidence += 25 // 震盪市場均值回歸更有效
	}

	return params, reasoning, min(confidence, 100)
}

// generateTrendFollowingParams 生成趨勢跟蹤策略參數
func (s *SmartParamsService) generateTrendFollowingParams(
	price *PriceInfo,
	volatility *VolatilityInfo,
	totalCapital float64,
) (map[string]interface{}, string, float64) {
	// 均線週期根據波動率調整
	fastPeriod := 10
	slowPeriod := 30

	if volatility.Volatility7d > 60 {
		fastPeriod = 5
		slowPeriod = 20
	} else if volatility.Volatility7d < 30 {
		fastPeriod = 15
		slowPeriod = 50
	}

	params := map[string]interface{}{
		"fast_period":   fastPeriod,
		"slow_period":   slowPeriod,
		"total_capital": totalCapital,
		"fee_rate":      0.0004,
	}

	reasoning := fmt.Sprintf(
		"當前價格 %.2f，7日波動率 %.1f%%，趨勢方向 %s。"+
			"建議快線週期 %d，慢線週期 %d。趨勢跟蹤策略在明確趨勢市場表現較好。",
		price.CurrentPrice, volatility.Volatility7d, volatility.TrendDirection,
		fastPeriod, slowPeriod,
	)

	confidence := 55.0
	if volatility.TrendDirection != "sideways" {
		confidence += 30 // 有趨勢時效果更好
	}

	return params, reasoning, min(confidence, 100)
}

// generateDCAParams 生成定投策略參數
func (s *SmartParamsService) generateDCAParams(
	price *PriceInfo,
	volatility *VolatilityInfo,
	totalCapital float64,
) (map[string]interface{}, string, float64) {
	// 定投間隔根據波動率調整
	intervalDays := 7
	if volatility.Volatility7d > 60 {
		intervalDays = 3 // 高波動時更頻繁定投
	} else if volatility.Volatility7d < 30 {
		intervalDays = 14 // 低波動時間隔更長
	}

	// 每次投入金額
	amountPerTrade := totalCapital / 20 // 假設20次定投完成
	if amountPerTrade < 50 {
		amountPerTrade = 50
	}

	params := map[string]interface{}{
		"interval_days":    intervalDays,
		"amount_per_trade": amountPerTrade,
		"total_capital":    totalCapital,
		"fee_rate":         0.0004,
	}

	reasoning := fmt.Sprintf(
		"當前價格 %.2f，7日波動率 %.1f%%。"+
			"建議每 %d 天定投 %.0f USDT，分散買入降低時機風險。",
		price.CurrentPrice, volatility.Volatility7d,
		intervalDays, amountPerTrade,
	)

	confidence := 75.0 // 定投策略相對穩定

	return params, reasoning, confidence
}

// generateMartingaleParams 生成馬丁格爾策略參數
func (s *SmartParamsService) generateMartingaleParams(
	price *PriceInfo,
	volatility *VolatilityInfo,
	totalCapital float64,
) (map[string]interface{}, string, float64) {
	// 基礎金額（總資金的 1%）
	baseAmount := totalCapital * 0.01
	if baseAmount < 10 {
		baseAmount = 10
	}

	// 加倍倍數根據波動率調整
	multiplier := 2.0
	if volatility.Volatility7d > 60 {
		multiplier = 1.5 // 高波動時降低倍數
	}

	params := map[string]interface{}{
		"base_amount":   baseAmount,
		"multiplier":    multiplier,
		"total_capital": totalCapital,
		"fee_rate":      0.0004,
	}

	reasoning := fmt.Sprintf(
		"當前價格 %.2f，7日波動率 %.1f%%。"+
			"⚠️ 馬丁格爾策略風險較高！建議基礎金額 %.0f USDT，加倍倍數 %.1f。"+
			"請確保總資金足以承受連續虧損。",
		price.CurrentPrice, volatility.Volatility7d,
		baseAmount, multiplier,
	)

	confidence := 40.0 // 馬丁格爾風險高，置信度較低

	return params, reasoning, confidence
}

// calculateStdDev 計算標準差
func (s *SmartParamsService) calculateStdDev(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}

	var sum float64
	for _, v := range values {
		sum += v
	}
	mean := sum / float64(len(values))

	var variance float64
	for _, v := range values {
		variance += (v - mean) * (v - mean)
	}
	variance /= float64(len(values))

	return math.Sqrt(variance)
}

// getVolatilityLevel 獲取波動率等級描述
func (s *SmartParamsService) getVolatilityLevel(volatility float64) string {
	if volatility > 80 {
		return "極高"
	} else if volatility > 60 {
		return "高"
	} else if volatility > 40 {
		return "中等"
	} else if volatility > 20 {
		return "較低"
	}
	return "低"
}

// GetMultipleRecommendations 批量獲取多個策略的推薦
func (s *SmartParamsService) GetMultipleRecommendations(
	ctx context.Context,
	exchangeName string,
	marketType string,
	symbol string,
	strategies []string,
	totalCapital float64,
) ([]*SmartParamsRecommendation, error) {
	recommendations := make([]*SmartParamsRecommendation, 0, len(strategies))

	for _, strategy := range strategies {
		rec, err := s.GetRecommendation(ctx, exchangeName, marketType, symbol, strategy, totalCapital)
		if err != nil {
			logger.Warn("獲取 %s 策略推薦失敗: %v", strategy, err)
			continue
		}
		recommendations = append(recommendations, rec)
	}

	// 按置信度排序
	sort.Slice(recommendations, func(i, j int) bool {
		return recommendations[i].Confidence > recommendations[j].Confidence
	})

	return recommendations, nil
}

// ClearCache 清除緩存
func (s *SmartParamsService) ClearCache() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.priceCache = make(map[string]*PriceInfo)
	s.volatilityCache = make(map[string]*VolatilityInfo)
}
