package backtest

// StrategyParamDefinition 策略参数定义
type StrategyParamDefinition struct {
	StrategyType string       `json:"strategy_type"`
	Name         string       `json:"name"`
	Description  string       `json:"description"`
	Params       []ParamField `json:"params"`
}

// ParamField 参数字段
type ParamField struct {
	Name     string      `json:"name"`
	Label    string      `json:"label"`
	Type     string      `json:"type"` // number, select, range, boolean
	Required bool        `json:"required"`
	Default  interface{} `json:"default,omitempty"`
	Min      *float64    `json:"min,omitempty"`
	Max      *float64    `json:"max,omitempty"`
	Options  []Option    `json:"options,omitempty"`
	Unit     string      `json:"unit,omitempty"`
	Hint     string      `json:"hint,omitempty"`
}

// Option 下拉选项
type Option struct {
	Value interface{} `json:"value"`
	Label string     `json:"label"`
}

// SymbolBacktestPreset 交易对回测预设（基于 docs/K线选择要求.md）
type SymbolBacktestPreset struct {
	Symbol               string   `json:"symbol"`
	VolatilityType       string   `json:"volatility_type"`        // high, medium_high, medium, low
	RecommendedDays      []int    `json:"recommended_days"`       // 推荐回测天数
	RecommendedInterval string   `json:"recommended_interval"`   // 推荐 K 线周期
	GridGapRange         string   `json:"grid_gap_range"`         // 网格间距建议
	IntervalOptions      []string `json:"interval_options"`      // 可选 K 线周期
}

// 内置交易对预设（基于 K线选择要求.md 推荐矩阵）
var defaultSymbolPresets = map[string]SymbolBacktestPreset{
	"BTCUSDT": {
		Symbol:               "BTCUSDT",
		VolatilityType:       "high",
		RecommendedDays:      []int{7, 14, 30},
		RecommendedInterval: "1m",
		GridGapRange:         "0.1%~0.3%",
		IntervalOptions:      []string{"1m", "3m", "5m"},
	},
	"ETHUSDT": {
		Symbol:               "ETHUSDT",
		VolatilityType:       "medium_high",
		RecommendedDays:      []int{30, 60},
		RecommendedInterval: "5m",
		GridGapRange:         "0.3%~0.6%",
		IntervalOptions:      []string{"5m", "15m", "1h"},
	},
	"SOLUSDT": {
		Symbol:               "SOLUSDT",
		VolatilityType:       "high",
		RecommendedDays:      []int{7, 14, 30},
		RecommendedInterval: "1m",
		GridGapRange:         "0.1%~0.3%",
		IntervalOptions:      []string{"1m", "5m", "15m"},
	},
	"DOGEUSDT": {
		Symbol:               "DOGEUSDT",
		VolatilityType:       "high",
		RecommendedDays:      []int{7, 14, 30},
		RecommendedInterval: "1m",
		GridGapRange:         "0.1%~0.3%",
		IntervalOptions:      []string{"1m", "5m", "15m"},
	},
	"BNBUSDT": {
		Symbol:               "BNBUSDT",
		VolatilityType:       "medium_high",
		RecommendedDays:      []int{30, 60},
		RecommendedInterval: "5m",
		GridGapRange:         "0.3%~0.6%",
		IntervalOptions:      []string{"5m", "15m", "1h"},
	},
	"ADAUSDT": {
		Symbol:               "ADAUSDT",
		VolatilityType:       "medium",
		RecommendedDays:      []int{60, 120},
		RecommendedInterval: "15m",
		GridGapRange:         "0.5%~1.0%",
		IntervalOptions:      []string{"15m", "30m", "1h"},
	},
	"LTCUSDT": {
		Symbol:               "LTCUSDT",
		VolatilityType:       "medium",
		RecommendedDays:      []int{60, 120},
		RecommendedInterval: "15m",
		GridGapRange:         "0.5%~1.0%",
		IntervalOptions:      []string{"15m", "30m", "1h"},
	},
	"PAXGUSDT": {
		Symbol:               "PAXGUSDT",
		VolatilityType:       "low",
		RecommendedDays:      []int{90, 180},
		RecommendedInterval: "1h",
		GridGapRange:         "1%~2%",
		IntervalOptions:      []string{"1h", "4h"},
	},
	"XAUUSDT": {
		Symbol:               "XAUUSDT",
		VolatilityType:       "low",
		RecommendedDays:      []int{90, 180},
		RecommendedInterval: "1h",
		GridGapRange:         "1%~2%",
		IntervalOptions:      []string{"1h", "4h"},
	},
}

// GetSymbolPreset 获取交易对预设，无则返回通用默认
func GetSymbolPreset(symbol string) SymbolBacktestPreset {
	if p, ok := defaultSymbolPresets[symbol]; ok {
		return p
	}
	return SymbolBacktestPreset{
		Symbol:               symbol,
		VolatilityType:       "medium",
		RecommendedDays:      []int{30, 60, 90},
		RecommendedInterval: "15m",
		GridGapRange:         "0.5%~1.0%",
		IntervalOptions:      []string{"5m", "15m", "30m", "1h"},
	}
}

// GetAllStrategyDefinitions 返回所有策略的参数定义
func GetAllStrategyDefinitions() []StrategyParamDefinition {
	return []StrategyParamDefinition{
		GetGridStrategyDefinition(),
		GetMomentumStrategyDefinition(),
		GetMeanReversionStrategyDefinition(),
		GetTrendFollowingStrategyDefinition(),
		GetDCAStrategyDefinition(),
		GetMartingaleStrategyDefinition(),
		GetComboStrategyDefinition(),
	}
}

// GetStrategyDefinition 按策略类型获取参数定义
func GetStrategyDefinition(strategyType string) *StrategyParamDefinition {
	for _, d := range GetAllStrategyDefinitions() {
		if d.StrategyType == strategyType {
			return &d
		}
	}
	return nil
}

// GetGridStrategyDefinition 网格策略参数
func GetGridStrategyDefinition() StrategyParamDefinition {
	minZero := 0.0
	minGap := 0.01
	maxGap := 10.0
	return StrategyParamDefinition{
		StrategyType: "grid",
		Name:         "网格策略",
		Description:  "在价格区间内按网格挂单，低买高卖。",
		Params: []ParamField{
			{Name: "price_low", Label: "价格下限", Type: "number", Required: true, Default: 0, Unit: "USDT", Hint: "网格区间下限"},
			{Name: "price_high", Label: "价格上限", Type: "number", Required: true, Default: 0, Unit: "USDT", Hint: "网格区间上限"},
			{Name: "grid_count", Label: "格子数量", Type: "number", Required: false, Default: 0, Min: &minZero, Hint: "0 表示无限网格（按间距铺满）"},
			{Name: "order_quantity", Label: "单笔订单大小", Type: "number", Required: true, Default: 100, Min: &minGap, Unit: "USDT"},
			{Name: "total_capital", Label: "总投入资金", Type: "number", Required: true, Default: 10000, Min: &minGap, Unit: "USDT"},
			{Name: "fee_rate", Label: "手续费率", Type: "number", Required: false, Default: 0.0004, Min: &minZero, Max: &maxGap, Hint: "如 0.0004 表示 0.04%"},
		},
	}
}

// GetMomentumStrategyDefinition 动量策略参数
func GetMomentumStrategyDefinition() StrategyParamDefinition {
	minVal := 1.0
	maxVal := 100.0
	return StrategyParamDefinition{
		StrategyType: "momentum",
		Name:         "动量策略",
		Description:  "基于 RSI 的超买超卖策略。",
		Params: []ParamField{
			{Name: "rsi_period", Label: "RSI 周期", Type: "number", Required: false, Default: 14, Min: &minVal, Max: &maxVal},
			{Name: "total_capital", Label: "总投入资金", Type: "number", Required: true, Default: 10000, Min: &minVal, Unit: "USDT"},
			{Name: "fee_rate", Label: "手续费率", Type: "number", Required: false, Default: 0.0004},
		},
	}
}

// GetMeanReversionStrategyDefinition 均值回归策略参数
func GetMeanReversionStrategyDefinition() StrategyParamDefinition {
	minVal := 1.0
	return StrategyParamDefinition{
		StrategyType: "mean_reversion",
		Name:         "均值回归策略",
		Description:  "基于布林带的均值回归。",
		Params: []ParamField{
			{Name: "period", Label: "周期", Type: "number", Required: false, Default: 20, Min: &minVal},
			{Name: "total_capital", Label: "总投入资金", Type: "number", Required: true, Default: 10000, Min: &minVal, Unit: "USDT"},
			{Name: "fee_rate", Label: "手续费率", Type: "number", Required: false, Default: 0.0004},
		},
	}
}

// GetTrendFollowingStrategyDefinition 趋势跟踪策略参数
func GetTrendFollowingStrategyDefinition() StrategyParamDefinition {
	minVal := 1.0
	return StrategyParamDefinition{
		StrategyType: "trend_following",
		Name:         "趋势跟踪策略",
		Description:  "双均线金叉死叉。",
		Params: []ParamField{
			{Name: "fast_period", Label: "快线周期", Type: "number", Required: false, Default: 10, Min: &minVal},
			{Name: "slow_period", Label: "慢线周期", Type: "number", Required: false, Default: 30, Min: &minVal},
			{Name: "total_capital", Label: "总投入资金", Type: "number", Required: true, Default: 10000, Min: &minVal, Unit: "USDT"},
			{Name: "fee_rate", Label: "手续费率", Type: "number", Required: false, Default: 0.0004},
		},
	}
}

// GetDCAStrategyDefinition DCA 策略参数
func GetDCAStrategyDefinition() StrategyParamDefinition {
	minVal := 1.0
	return StrategyParamDefinition{
		StrategyType: "dca",
		Name:         "定投策略",
		Description:  "定期定额买入。",
		Params: []ParamField{
			{Name: "interval_days", Label: "定投间隔(天)", Type: "number", Required: true, Default: 7, Min: &minVal},
			{Name: "amount_per_trade", Label: "每次投入金额", Type: "number", Required: true, Default: 100, Min: &minVal, Unit: "USDT"},
			{Name: "total_capital", Label: "总投入资金上限", Type: "number", Required: true, Default: 10000, Min: &minVal, Unit: "USDT"},
			{Name: "fee_rate", Label: "手续费率", Type: "number", Required: false, Default: 0.0004},
		},
	}
}

// GetMartingaleStrategyDefinition 马丁格尔策略参数
func GetMartingaleStrategyDefinition() StrategyParamDefinition {
	minVal := 1.0
	return StrategyParamDefinition{
		StrategyType: "martingale",
		Name:         "马丁格尔策略",
		Description:  "亏损加倍下注（高风险）。",
		Params: []ParamField{
			{Name: "base_amount", Label: "基础下单金额", Type: "number", Required: true, Default: 100, Min: &minVal, Unit: "USDT"},
			{Name: "multiplier", Label: "加倍倍数", Type: "number", Required: false, Default: 2.0, Min: &minVal},
			{Name: "total_capital", Label: "总投入资金上限", Type: "number", Required: true, Default: 10000, Min: &minVal, Unit: "USDT"},
			{Name: "fee_rate", Label: "手续费率", Type: "number", Required: false, Default: 0.0004},
		},
	}
}

// GetComboStrategyDefinition 组合策略参数
func GetComboStrategyDefinition() StrategyParamDefinition {
	minVal := 1.0
	return StrategyParamDefinition{
		StrategyType: "combo",
		Name:         "组合策略",
		Description:  "多策略组合。",
		Params: []ParamField{
			{Name: "total_capital", Label: "总投入资金", Type: "number", Required: true, Default: 10000, Min: &minVal, Unit: "USDT"},
			{Name: "fee_rate", Label: "手续费率", Type: "number", Required: false, Default: 0.0004},
		},
	}
}
