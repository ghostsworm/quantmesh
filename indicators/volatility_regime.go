package indicators

import (
	"fmt"
	"math"
	"sync"
	"time"
)

// VolatilityRegime 波动率区间类型
type VolatilityRegime int

const (
	// RegimeLow 低波动区间 - 适合网格策略
	RegimeLow VolatilityRegime = iota
	// RegimeNormal 正常波动区间
	RegimeNormal
	// RegimeHigh 高波动区间 - 需要谨慎
	RegimeHigh
	// RegimeExtreme 极端波动区间 - 危险
	RegimeExtreme
)

// String 返回区间描述
func (vr VolatilityRegime) String() string {
	switch vr {
	case RegimeLow:
		return "LOW"
	case RegimeNormal:
		return "NORMAL"
	case RegimeHigh:
		return "HIGH"
	case RegimeExtreme:
		return "EXTREME"
	default:
		return "UNKNOWN"
	}
}

// Color 返回区间对应的颜色（用于UI显示）
func (vr VolatilityRegime) Color() string {
	switch vr {
	case RegimeLow:
		return "green"    // 绿色 - 安全
	case RegimeNormal:
		return "blue"     // 蓝色 - 正常
	case RegimeHigh:
		return "orange"   // 橙色 - 警告
	case RegimeExtreme:
		return "red"      // 红色 - 危险
	default:
		return "gray"
	}
}

// VolatilityRegimeConfig 波动率区间检测配置
type VolatilityRegimeConfig struct {
	// 检测周期（单位：小时）
	ShortPeriod int  // 短期波动率周期（默认 24 小时）
	MediumPeriod int // 中期波动率周期（默认 3 天 = 72 小时）
	LongPeriod int   // 长期波动率周期（默认 7 天 = 168 小时）

	// 波动率阈值（百分比）
	LowThreshold     float64 // 低波动阈值（默认 1%）
	NormalThreshold  float64 // 正常波动上限（默认 3%）
	HighThreshold    float64 // 高波动上限（默认 5%）
	ExtremeThreshold float64 // 极端波动阈值（默认 10%）

	// 价格范围检测（用于检测低波动）
	PriceRangePeriod   int     // 价格范围检测周期（默认 3 天）
	PriceRangeThreshold float64 // 价格范围阈值（默认 1.5%）

	// 波动率突变检测
	SuddenIncreaseThreshold float64 // 突然增加阈值（默认 2倍，即波动率翻倍）
	SuddenDecreaseThreshold float64 // 突然减少阈值（默认 0.5倍，即波动率减半）

	// 连续性检测
	ConsecutivePeriods int // 连续几个周期超出阈值才触发（默认 2）
}

// DefaultVolatilityRegimeConfig 返回默认配置
func DefaultVolatilityRegimeConfig() VolatilityRegimeConfig {
	return VolatilityRegimeConfig{
		ShortPeriod:              24,
		MediumPeriod:             72,  // 3天
		LongPeriod:               168, // 7天
		LowThreshold:             1.0,
		NormalThreshold:          3.0,
		HighThreshold:            5.0,
		ExtremeThreshold:         10.0,
		PriceRangePeriod:         72,  // 3天
		PriceRangeThreshold:      1.5,
		SuddenIncreaseThreshold:  2.0,
		SuddenDecreaseThreshold:  0.5,
		ConsecutivePeriods:       2,
	}
}

// VolatilityRegimeDetector 波动率区间检测器
type VolatilityRegimeDetector struct {
	config VolatilityRegimeConfig
	mu     sync.RWMutex

	// 历史数据存储
	priceHistory      []PricePoint
	volatilityHistory []VolatilityPoint

	// 当前状态
	currentRegime     VolatilityRegime
	previousRegime    VolatilityRegime
	consecutiveCount  int

	// 检测结果回调
	onRegimeChange    func(VolatilityRegimeEvent)
}

// PricePoint 价格数据点
type PricePoint struct {
	Timestamp time.Time
	Price     float64
	High      float64
	Low       float64
	Volume    float64
}

// VolatilityPoint 波动率数据点
type VolatilityPoint struct {
	Timestamp       time.Time
	ShortVolatility float64 // 短期波动率
	MediumVolatility float64 // 中期波动率
	LongVolatility  float64 // 长期波动率
	PriceRange      float64 // 价格范围百分比
	Regime          VolatilityRegime
}

// VolatilityRegimeEvent 波动率区间变化事件
type VolatilityRegimeEvent struct {
	Timestamp       time.Time
	OldRegime       VolatilityRegime
	NewRegime       VolatilityRegime
	TriggerReason   string
	ShortVolatility float64
	MediumVolatility float64
	LongVolatility  float64
	PriceRange      float64
	Recommendations []string
	Severity        string // "info", "warning", "critical"
}

// NewVolatilityRegimeDetector 创建波动率区间检测器
func NewVolatilityRegimeDetector(config VolatilityRegimeConfig) *VolatilityRegimeDetector {
	if config.ShortPeriod == 0 {
		config = DefaultVolatilityRegimeConfig()
	}

	return &VolatilityRegimeDetector{
		config:         config,
		priceHistory:   make([]PricePoint, 0, 1000),
		volatilityHistory: make([]VolatilityPoint, 0, 1000),
		currentRegime:  RegimeNormal,
		previousRegime: RegimeNormal,
	}
}

// SetRegimeChangeCallback 设置区间变化回调
func (vrd *VolatilityRegimeDetector) SetRegimeChangeCallback(callback func(VolatilityRegimeEvent)) {
	vrd.mu.Lock()
	defer vrd.mu.Unlock()
	vrd.onRegimeChange = callback
}

// UpdatePrice 更新价格数据
func (vrd *VolatilityRegimeDetector) UpdatePrice(price, high, low, volume float64) {
	vrd.mu.Lock()
	defer vrd.mu.Unlock()

	now := time.Now()
	point := PricePoint{
		Timestamp: now,
		Price:     price,
		High:      high,
		Low:       low,
		Volume:    volume,
	}

	vrd.priceHistory = append(vrd.priceHistory, point)

	// 限制历史数据长度
	maxLength := vrd.config.LongPeriod * 2 // 保留2倍长期周期的数据
	if len(vrd.priceHistory) > maxLength {
		vrd.priceHistory = vrd.priceHistory[len(vrd.priceHistory)-maxLength:]
	}

	// 检测波动率区间
	vrd.detectRegime(now)
}

// detectRegime 检测波动率区间
func (vrd *VolatilityRegimeDetector) detectRegime(timestamp time.Time) {
	if len(vrd.priceHistory) < vrd.config.ShortPeriod {
		return
	}

	// 计算各周期波动率
	shortVol := vrd.calculateVolatility(vrd.config.ShortPeriod)
	mediumVol := vrd.calculateVolatility(vrd.config.MediumPeriod)
	longVol := vrd.calculateVolatility(vrd.config.LongPeriod)
	priceRange := vrd.calculatePriceRange(vrd.config.PriceRangePeriod)

	// 判断当前区间
	newRegime := vrd.classifyRegime(shortVol, mediumVol, longVol, priceRange)

	// 记录波动率数据
	volPoint := VolatilityPoint{
		Timestamp:        timestamp,
		ShortVolatility:  shortVol,
		MediumVolatility: mediumVol,
		LongVolatility:   longVol,
		PriceRange:       priceRange,
		Regime:           newRegime,
	}
	vrd.volatilityHistory = append(vrd.volatilityHistory, volPoint)

	// 检查是否需要触发区间变化事件
	if newRegime != vrd.currentRegime {
		if newRegime == vrd.previousRegime {
			// 确认变化
			vrd.consecutiveCount++
			if vrd.consecutiveCount >= vrd.config.ConsecutivePeriods {
				vrd.triggerRegimeChange(volPoint)
			}
		} else {
			// 首次检测到变化
			vrd.previousRegime = vrd.currentRegime
			vrd.consecutiveCount = 1
		}
	} else {
		vrd.consecutiveCount = 0
		vrd.previousRegime = vrd.currentRegime
	}
}

// classifyRegime 分类波动率区间
func (vrd *VolatilityRegimeDetector) classifyRegime(shortVol, mediumVol, longVol, priceRange float64) VolatilityRegime {
	// 优先检查极端情况
	if shortVol > vrd.config.ExtremeThreshold {
		return RegimeExtreme
	}

	// 检查低波动情况（价格在很小范围内波动）
	if priceRange < vrd.config.PriceRangeThreshold && mediumVol < vrd.config.LowThreshold {
		return RegimeLow
	}

	// 根据短期波动率分类
	if shortVol > vrd.config.HighThreshold {
		return RegimeHigh
	}

	if shortVol < vrd.config.LowThreshold {
		return RegimeLow
	}

	return RegimeNormal
}

// triggerRegimeChange 触发区间变化事件
func (vrd *VolatilityRegimeDetector) triggerRegimeChange(volPoint VolatilityPoint) {
	oldRegime := vrd.currentRegime
	newRegime := volPoint.Regime

	// 构建事件
	event := VolatilityRegimeEvent{
		Timestamp:        volPoint.Timestamp,
		OldRegime:        oldRegime,
		NewRegime:        newRegime,
		ShortVolatility:  volPoint.ShortVolatility,
		MediumVolatility: volPoint.MediumVolatility,
		LongVolatility:   volPoint.LongVolatility,
		PriceRange:       volPoint.PriceRange,
	}

	// 确定触发原因和严重程度
	event.TriggerReason = vrd.buildTriggerReason(oldRegime, newRegime, volPoint)
	event.Severity = vrd.determineSeverity(oldRegime, newRegime)
	event.Recommendations = vrd.generateRecommendations(oldRegime, newRegime, volPoint)

	// 更新当前区间
	vrd.currentRegime = newRegime

	// 触发回调
	if vrd.onRegimeChange != nil {
		go vrd.onRegimeChange(event)
	}
}

// buildTriggerReason 构建触发原因说明
func (vrd *VolatilityRegimeDetector) buildTriggerReason(oldRegime, newRegime VolatilityRegime, volPoint VolatilityPoint) string {
	reason := fmt.Sprintf("波动率区间从 %s 变为 %s", oldRegime, newRegime)

	// 添加详细原因
	switch newRegime {
	case RegimeLow:
		reason += fmt.Sprintf("。中期波动率 %.2f%% 低于低波动阈值 %.2f%%，价格范围 %.2f%% 低于阈值 %.2f%%",
			volPoint.MediumVolatility, vrd.config.LowThreshold,
			volPoint.PriceRange, vrd.config.PriceRangeThreshold)

	case RegimeHigh:
		reason += fmt.Sprintf("。短期波动率 %.2f%% 超过高波动阈值 %.2f%%",
			volPoint.ShortVolatility, vrd.config.HighThreshold)

	case RegimeExtreme:
		reason += fmt.Sprintf("。⚠️ 短期波动率 %.2f%% 超过极端阈值 %.2f%%，市场极其不稳定！",
			volPoint.ShortVolatility, vrd.config.ExtremeThreshold)
	}

	return reason
}

// determineSeverity 确定事件严重程度
func (vrd *VolatilityRegimeDetector) determineSeverity(oldRegime, newRegime VolatilityRegime) string {
	// 从低波动到高波动是危险的
	if oldRegime == RegimeLow && (newRegime == RegimeHigh || newRegime == RegimeExtreme) {
		return "critical"
	}

	// 从正常到极端波动是危险的
	if (oldRegime == RegimeNormal || oldRegime == RegimeLow) && newRegime == RegimeExtreme {
		return "critical"
	}

	// 进入高波动需要警告
	if newRegime == RegimeHigh {
		return "warning"
	}

	// 进入低波动是信息性通知
	if newRegime == RegimeLow {
		return "info"
	}

	return "info"
}

// generateRecommendations 生成策略建议
func (vrd *VolatilityRegimeDetector) generateRecommendations(oldRegime, newRegime VolatilityRegime, volPoint VolatilityPoint) []string {
	recommendations := []string{}

	switch newRegime {
	case RegimeLow:
		recommendations = append(recommendations, "✅ 低波动区间，适合网格策略运行")
		recommendations = append(recommendations, "💡 可以考虑缩小网格间距以提高收益")
		recommendations = append(recommendations, "💡 可以适当增加仓位以提高资金利用率")

	case RegimeNormal:
		recommendations = append(recommendations, "ℹ️ 正常波动区间，策略正常运行")

	case RegimeHigh:
		recommendations = append(recommendations, "⚠️ 高波动区间，需要谨慎")
		recommendations = append(recommendations, "📊 建议扩大网格间距以减少频繁交易")
		recommendations = append(recommendations, "📉 建议减少新开仓，降低仓位")
		if oldRegime == RegimeLow {
			recommendations = append(recommendations, "🚨 特别注意：从低波动突然进入高波动，建议暂停策略")
		}

	case RegimeExtreme:
		recommendations = append(recommendations, "🚨 极端波动区间，市场极其不稳定！")
		recommendations = append(recommendations, "⛔ 强烈建议暂停策略")
		recommendations = append(recommendations, "🛡️ 建议设置更宽的止损或及时止盈")
		recommendations = append(recommendations, "📉 建议大幅减少仓位或平仓保护")
	}

	return recommendations
}

// calculateVolatility 计算指定周期的波动率（使用标准差）
func (vrd *VolatilityRegimeDetector) calculateVolatility(period int) float64 {
	if len(vrd.priceHistory) < period {
		return 0
	}

	// 获取最近period个数据点
	start := len(vrd.priceHistory) - period
	points := vrd.priceHistory[start:]

	// 计算收益率
	returns := make([]float64, len(points)-1)
	for i := 0; i < len(points)-1; i++ {
		if points[i].Price > 0 {
			returns[i] = (points[i+1].Price - points[i].Price) / points[i].Price * 100
		}
	}

	if len(returns) == 0 {
		return 0
	}

	// 计算平均收益率
	var sum float64
	for _, r := range returns {
		sum += r
	}
	mean := sum / float64(len(returns))

	// 计算标准差
	var variance float64
	for _, r := range returns {
		diff := r - mean
		variance += diff * diff
	}

	stdDev := math.Sqrt(variance / float64(len(returns)))

	// 返回绝对值作为波动率
	return math.Abs(stdDev)
}

// calculatePriceRange 计算指定周期内的价格范围百分比
func (vrd *VolatilityRegimeDetector) calculatePriceRange(period int) float64 {
	if len(vrd.priceHistory) < period {
		return 0
	}

	start := len(vrd.priceHistory) - period
	points := vrd.priceHistory[start:]

	// 找出最高价和最低价
	var maxHigh, minLow float64
	for i, p := range points {
		if i == 0 {
			maxHigh = p.High
			minLow = p.Low
		} else {
			if p.High > maxHigh {
				maxHigh = p.High
			}
			if p.Low < minLow {
				minLow = p.Low
			}
		}
	}

	if minLow == 0 {
		return 0
	}

	// 计算价格范围百分比
	rangePercent := (maxHigh - minLow) / minLow * 100
	return rangePercent
}

// DetectSuddenChange 检测波动率突变
func (vrd *VolatilityRegimeDetector) DetectSuddenChange() (bool, string) {
	vrd.mu.RLock()
	defer vrd.mu.RUnlock()

	if len(vrd.volatilityHistory) < 2 {
		return false, ""
	}

	// 比较最新的两个波动率数据点
	latest := vrd.volatilityHistory[len(vrd.volatilityHistory)-1]
	previous := vrd.volatilityHistory[len(vrd.volatilityHistory)-2]

	// 检测突然增加
	if previous.ShortVolatility > 0 {
		ratio := latest.ShortVolatility / previous.ShortVolatility
		if ratio >= vrd.config.SuddenIncreaseThreshold {
			return true, fmt.Sprintf("⚠️ 波动率突然增加 %.1f倍：%.2f%% -> %.2f%%",
				ratio, previous.ShortVolatility, latest.ShortVolatility)
		}
	}

	// 检测突然减少
	if previous.ShortVolatility > 0 {
		ratio := latest.ShortVolatility / previous.ShortVolatility
		if ratio <= vrd.config.SuddenDecreaseThreshold {
			return true, fmt.Sprintf("ℹ️ 波动率突然减少 %.1f倍：%.2f%% -> %.2f%%",
				1/ratio, previous.ShortVolatility, latest.ShortVolatility)
		}
	}

	return false, ""
}

// GetCurrentRegime 获取当前波动率区间
func (vrd *VolatilityRegimeDetector) GetCurrentRegime() VolatilityRegime {
	vrd.mu.RLock()
	defer vrd.mu.RUnlock()
	return vrd.currentRegime
}

// GetLatestVolatility 获取最新波动率数据
func (vrd *VolatilityRegimeDetector) GetLatestVolatility() *VolatilityPoint {
	vrd.mu.RLock()
	defer vrd.mu.RUnlock()

	if len(vrd.volatilityHistory) == 0 {
		return nil
	}

	latest := vrd.volatilityHistory[len(vrd.volatilityHistory)-1]
	return &latest
}

// GetVolatilityHistory 获取波动率历史
func (vrd *VolatilityRegimeDetector) GetVolatilityHistory(count int) []VolatilityPoint {
	vrd.mu.RLock()
	defer vrd.mu.RUnlock()

	if count <= 0 || count > len(vrd.volatilityHistory) {
		count = len(vrd.volatilityHistory)
	}

	start := len(vrd.volatilityHistory) - count
	if start < 0 {
		start = 0
	}

	result := make([]VolatilityPoint, count)
	copy(result, vrd.volatilityHistory[start:])
	return result
}

// IsGridFriendly 判断当前是否适合网格策略
func (vrd *VolatilityRegimeDetector) IsGridFriendly() bool {
	vrd.mu.RLock()
	defer vrd.mu.RUnlock()

	// 低波动和正常波动都适合网格策略
	return vrd.currentRegime == RegimeLow || vrd.currentRegime == RegimeNormal
}

// GetRiskLevel 获取当前风险等级（0-10）
func (vrd *VolatilityRegimeDetector) GetRiskLevel() int {
	vrd.mu.RLock()
	defer vrd.mu.RUnlock()

	switch vrd.currentRegime {
	case RegimeLow:
		return 1
	case RegimeNormal:
		return 3
	case RegimeHigh:
		return 7
	case RegimeExtreme:
		return 10
	default:
		return 5
	}
}
