package indicators

import (
	"fmt"
	"strings"
)

// VolatilityPreset 波动率预设配置
type VolatilityPreset struct {
	Name              string
	Symbols           []string // 匹配的交易对符号（如 BTCUSDT, *USDT 等）
	ShortPeriod       int      // 短期周期（小时）
	MediumPeriod      int      // 中期周期（小时）
	LongPeriod        int      // 长期周期（小时）
	LowThreshold      float64  // 低波动阈值（%）
	NormalThreshold   float64  // 正常波动上限（%）
	HighThreshold     float64  // 高波动上限（%）
	ExtremeThreshold  float64  // 极端波动阈值（%）
	PriceRangePeriod  int      // 价格范围检测周期（小时）
	PriceRangeThreshold float64 // 价格范围阈值（%）
}

// 内置波动率预设

// btcPreset BTC 波动率预设（高波动品种）
var btcPreset = VolatilityPreset{
	Name:             "BTC",
	Symbols:          []string{"BTCUSDT", "BUSD-BTC", "BTCBUSD"},
	ShortPeriod:      24,
	MediumPeriod:     72,
	LongPeriod:       168,
	LowThreshold:     1.5,   // BTC 日常波动较大，1.5% 以下才算低波动
	NormalThreshold:  4.0,   // 4% 以下算正常
	HighThreshold:    7.0,   // 7% 以上算高波动
	ExtremeThreshold: 15.0,  // 15% 以上算极端
	PriceRangePeriod:      72,
	PriceRangeThreshold:   2.0, // 3天内价格在2%范围内算低波动
}

// ethPreset ETH 波动率预设（高波动品种）
var ethPreset = VolatilityPreset{
	Name:             "ETH",
	Symbols:          []string{"ETHUSDT", "BUSD-ETH", "ETHBUSD"},
	ShortPeriod:      24,
	MediumPeriod:     72,
	LongPeriod:       168,
	LowThreshold:     1.5,
	NormalThreshold:  4.0,
	HighThreshold:    7.0,
	ExtremeThreshold: 15.0,
	PriceRangePeriod:      72,
	PriceRangeThreshold:   2.0,
}

// stablecoinPreset 稳定币波动率预设（低波动品种）
var stablecoinPreset = VolatilityPreset{
	Name:             "Stablecoin",
	Symbols:          []string{"USDTUSDT", "BUSDUSDT", "USDCUSDT", "DAIUSDT", "TUSDUSDT"},
	ShortPeriod:      24,
	MediumPeriod:     72,
	LongPeriod:       168,
	LowThreshold:     0.1,   // 稳定币0.1%以下算低波动
	NormalThreshold:  0.3,   // 0.3%以下算正常
	HighThreshold:    0.8,   // 0.8%以上算高波动
	ExtremeThreshold: 2.0,   // 2%以上算极端
	PriceRangePeriod:      72,
	PriceRangeThreshold:   0.3,
}

// goldPreset 黄金波动率预设（中等波动品种）
var goldPreset = VolatilityPreset{
	Name:             "Gold",
	Symbols:          []string{"PAXGUSDT", "XAUTUSDT", "BTGUSDT", "DGXUSDT"},
	ShortPeriod:      24,
	MediumPeriod:     72,
	LongPeriod:       168,
	LowThreshold:     0.5,   // 黄金相对稳定，0.5%以下算低波动
	NormalThreshold:  1.5,   // 1.5%以下算正常
	HighThreshold:    3.0,   // 3%以上算高波动
	ExtremeThreshold: 6.0,   // 6%以上算极端
	PriceRangePeriod:      72,
	PriceRangeThreshold:   1.0,
}

// altcoinPreset 山寨币波动率预设（极高波动品种）
var altcoinPreset = VolatilityPreset{
	Name:             "Altcoin",
	Symbols:          []string{"*USDT"}, // 通配符，匹配所有USDT交易对（优先级最低）
	ShortPeriod:      24,
	MediumPeriod:     72,
	LongPeriod:       168,
	LowThreshold:     2.0,   // 山寨币波动大
	NormalThreshold:  5.0,
	HighThreshold:    10.0,
	ExtremeThreshold: 20.0,
	PriceRangePeriod:      72,
	PriceRangeThreshold:   3.0,
}

// defiPreset DeFi 代币波动率预设（极高波动）
var defiPreset = VolatilityPreset{
	Name:             "DeFi",
	Symbols:          []string{"UNIUSDT", "AAVEUSDT", "SNXUSDT", "CRVUSDT", "COMPUSDT", "YFIUSDT", "MKRUSDT"},
	ShortPeriod:      24,
	MediumPeriod:     72,
	LongPeriod:       168,
	LowThreshold:     3.0,
	NormalThreshold:  7.0,
	HighThreshold:    12.0,
	ExtremeThreshold: 25.0,
	PriceRangePeriod:      72,
	PriceRangeThreshold:   4.0,
}

// memePreset Meme 币波动率预设（极度波动）
var memePreset = VolatilityPreset{
	Name:             "Meme",
	Symbols:          []string{"DOGEUSDT", "SHIBUSDT", "PEPEUSDT", "FLOKIUSDT", "BONKUSDT"},
	ShortPeriod:      24,
	MediumPeriod:     72,
	LongPeriod:       168,
	LowThreshold:     5.0,
	NormalThreshold:  10.0,
	HighThreshold:    20.0,
	ExtremeThreshold: 40.0,
	PriceRangePeriod:      72,
	PriceRangeThreshold:   8.0,
}

// forexPreset 外汇波动率预设
var forexPreset = VolatilityPreset{
	Name:             "Forex",
	Symbols:          []string{"EURUSDT", "GBPUSDT", "JPYUSDT", "AUDUSDT", "CADUSDT"},
	ShortPeriod:      24,
	MediumPeriod:     72,
	LongPeriod:       168,
	LowThreshold:     0.3,
	NormalThreshold:  0.8,
	HighThreshold:    1.5,
	ExtremeThreshold: 3.0,
	PriceRangePeriod:      72,
	PriceRangeThreshold:   0.6,
}

// indexPreset 指数波动率预设
var indexPreset = VolatilityPreset{
	Name:             "Index",
	Symbols:          []string{"BTCUSDT", "ETHUSDT"}, // 这里的指数指币圈指数
	ShortPeriod:      24,
	MediumPeriod:     72,
	LongPeriod:       168,
	LowThreshold:     1.0,
	NormalThreshold:  3.0,
	HighThreshold:    6.0,
	ExtremeThreshold: 12.0,
	PriceRangePeriod:      72,
	PriceRangeThreshold:   1.5,
}

// 所有预设（按优先级排序，优先级高的在前）
var volatilityPresets = []VolatilityPreset{
	memePreset,      // 最高优先级：Meme 币
	defiPreset,      // 高优先级：DeFi 代币
	goldPreset,      // 中高优先级：黄金
	stablecoinPreset, // 中高优先级：稳定币
	btcPreset,       // 中优先级：BTC
	ethPreset,       // 中优先级：ETH
	forexPreset,     // 中低优先级：外汇
	altcoinPreset,   // 低优先级：山寨币通配符
}

// GetVolatilityPreset 根据交易对符号获取波动率预设
func GetVolatilityPreset(symbol string) VolatilityPreset {
	// 标准化符号
	symbol = strings.ToUpper(strings.TrimSuffix(symbol, "PERP")) // 去掉永续合约后缀
	symbol = strings.ReplaceAll(symbol, "-", "") // 去掉分隔符

	// 按优先级遍历预设
	for _, preset := range volatilityPresets {
		for _, pattern := range preset.Symbols {
			if matchSymbol(symbol, pattern) {
				return preset
			}
		}
	}

	// 没有匹配到，返回默认预设（山寨币）
	return altcoinPreset
}

// matchSymbol 检查符号是否匹配模式
func matchSymbol(symbol, pattern string) bool {
	if strings.HasPrefix(pattern, "*") {
		// 通配符匹配
		suffix := strings.TrimPrefix(pattern, "*")
		return strings.HasSuffix(symbol, suffix)
	}

	if strings.HasSuffix(pattern, "*") {
		// 前缀匹配
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(symbol, prefix)
	}

	// 精确匹配
	return symbol == pattern
}

// GetPresetForSymbol 根据交易对获取预设配置的友好描述
func GetPresetForSymbol(symbol string) string {
	preset := GetVolatilityPreset(symbol)
	return fmt.Sprintf("%s预设: 低波动<%.1f%%, 正常<%.1f%%, 高波动<%.1f%%, 极端≥%.1f%%",
		preset.Name,
		preset.LowThreshold,
		preset.NormalThreshold,
		preset.HighThreshold,
		preset.ExtremeThreshold)
}

// ListAllPresets 列出所有可用的预设
func ListAllPresets() []string {
	var result []string
	for _, preset := range volatilityPresets {
		desc := fmt.Sprintf("📊 %s: %s (阈值: %.1f%% / %.1f%% / %.1f%% / %.1f%%)",
			preset.Name,
			strings.Join(preset.Symbols, ", "),
			preset.LowThreshold,
			preset.NormalThreshold,
			preset.HighThreshold,
			preset.ExtremeThreshold)
		result = append(result, desc)
	}
	return result
}

// ConvertToConfig 将预设转换为 VolatilityRegimeConfig
func (vp VolatilityPreset) ConvertToConfig() VolatilityRegimeConfig {
	return VolatilityRegimeConfig{
		ShortPeriod:          vp.ShortPeriod,
		MediumPeriod:         vp.MediumPeriod,
		LongPeriod:           vp.LongPeriod,
		LowThreshold:         vp.LowThreshold,
		NormalThreshold:      vp.NormalThreshold,
		HighThreshold:        vp.HighThreshold,
		ExtremeThreshold:     vp.ExtremeThreshold,
		PriceRangePeriod:     vp.PriceRangePeriod,
		PriceRangeThreshold:  vp.PriceRangeThreshold,
		ConsecutivePeriods:   2,
	}
}

// CustomPreset 允许用户创建自定义预设
func CustomPreset(name string, thresholds map[string]float64) VolatilityPreset {
	return VolatilityPreset{
		Name:             name,
		Symbols:          []string{},
		ShortPeriod:      24,
		MediumPeriod:     72,
		LongPeriod:       168,
		LowThreshold:     thresholds["low"],
		NormalThreshold:  thresholds["normal"],
		HighThreshold:    thresholds["high"],
		ExtremeThreshold: thresholds["extreme"],
		PriceRangePeriod:      72,
		PriceRangeThreshold:   thresholds["price_range"],
	}
}
