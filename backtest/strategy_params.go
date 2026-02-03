package backtest

// StrategyParamDefinition 策略参數定义
type StrategyParamDefinition struct {
	StrategyType string       `json:"strategy_type"`
	Name         string       `json:"name"`
	Description  string       `json:"description"`
	Params       []ParamField `json:"params"`
}

// ParamField 参數字段
type ParamField struct {
	Name     string      `json:"name"`
	Label    string      `json:"label"`
	Type     string      `json:"type"` // number, select, range, boolean
	Required bool        `json:"required"`
	Default  interface{} `json:"default,omitempty"`
	Min      *float64    `json:"min,omitempty"`
	Max      *float64    `json:"max,omitempty"`
	Step     *float64    `json:"step,omitempty"` // 步長，如 0.1 表示允許小數
	Options  []Option    `json:"options,omitempty"`
	Unit     string      `json:"unit,omitempty"`
	Hint     string      `json:"hint,omitempty"`
}

// Option 下拉选项
type Option struct {
	Value interface{} `json:"value"`
	Label string      `json:"label"`
}

// SymbolBacktestPreset 交易對回测預設（基於 docs/K線选擇要求.md）
type SymbolBacktestPreset struct {
	Symbol              string   `json:"symbol"`
	VolatilityType      string   `json:"volatility_type"`      // high, medium_high, medium, low
	RecommendedDays     []int    `json:"recommended_days"`     // 推荐回测天數
	RecommendedInterval string   `json:"recommended_interval"` // 推荐 K 線周期
	GridGapRange        string   `json:"grid_gap_range"`       // 网格间距建议
	IntervalOptions     []string `json:"interval_options"`     // 可選 K 線周期
}

// 内置交易對預設（基於 K線选擇要求.md 推荐矩阵）
var defaultSymbolPresets = map[string]SymbolBacktestPreset{
	"BTCUSDT": {
		Symbol:              "BTCUSDT",
		VolatilityType:      "high",
		RecommendedDays:     []int{7, 14, 30},
		RecommendedInterval: "1m",
		GridGapRange:        "0.1%~0.3%",
		IntervalOptions:     []string{"1m", "3m", "5m"},
	},
	"ETHUSDT": {
		Symbol:              "ETHUSDT",
		VolatilityType:      "medium_high",
		RecommendedDays:     []int{30, 60},
		RecommendedInterval: "5m",
		GridGapRange:        "0.3%~0.6%",
		IntervalOptions:     []string{"5m", "15m", "1h"},
	},
	"SOLUSDT": {
		Symbol:              "SOLUSDT",
		VolatilityType:      "high",
		RecommendedDays:     []int{7, 14, 30},
		RecommendedInterval: "1m",
		GridGapRange:        "0.1%~0.3%",
		IntervalOptions:     []string{"1m", "5m", "15m"},
	},
	"DOGEUSDT": {
		Symbol:              "DOGEUSDT",
		VolatilityType:      "high",
		RecommendedDays:     []int{7, 14, 30},
		RecommendedInterval: "1m",
		GridGapRange:        "0.1%~0.3%",
		IntervalOptions:     []string{"1m", "5m", "15m"},
	},
	"BNBUSDT": {
		Symbol:              "BNBUSDT",
		VolatilityType:      "medium_high",
		RecommendedDays:     []int{30, 60},
		RecommendedInterval: "5m",
		GridGapRange:        "0.3%~0.6%",
		IntervalOptions:     []string{"5m", "15m", "1h"},
	},
	"ADAUSDT": {
		Symbol:              "ADAUSDT",
		VolatilityType:      "medium",
		RecommendedDays:     []int{60, 120},
		RecommendedInterval: "15m",
		GridGapRange:        "0.5%~1.0%",
		IntervalOptions:     []string{"15m", "30m", "1h"},
	},
	"LTCUSDT": {
		Symbol:              "LTCUSDT",
		VolatilityType:      "medium",
		RecommendedDays:     []int{60, 120},
		RecommendedInterval: "15m",
		GridGapRange:        "0.5%~1.0%",
		IntervalOptions:     []string{"15m", "30m", "1h"},
	},
	"PAXGUSDT": {
		Symbol:              "PAXGUSDT",
		VolatilityType:      "low",
		RecommendedDays:     []int{90, 180},
		RecommendedInterval: "1h",
		GridGapRange:        "1%~2%",
		IntervalOptions:     []string{"1h", "4h"},
	},
	"XAUUSDT": {
		Symbol:              "XAUUSDT",
		VolatilityType:      "low",
		RecommendedDays:     []int{90, 180},
		RecommendedInterval: "1h",
		GridGapRange:        "1%~2%",
		IntervalOptions:     []string{"1h", "4h"},
	},
}

// GetSymbolPreset 獲取交易對預設，無则返回通用默认
func GetSymbolPreset(symbol string) SymbolBacktestPreset {
	if p, ok := defaultSymbolPresets[symbol]; ok {
		return p
	}
	return SymbolBacktestPreset{
		Symbol:              symbol,
		VolatilityType:      "medium",
		RecommendedDays:     []int{30, 60, 90},
		RecommendedInterval: "15m",
		GridGapRange:        "0.5%~1.0%",
		IntervalOptions:     []string{"5m", "15m", "30m", "1h"},
	}
}

// GetAllStrategyDefinitions 返回所有策略的参數定义
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

// GetStrategyDefinition 按策略類型獲取参數定义
func GetStrategyDefinition(strategyType string) *StrategyParamDefinition {
	for _, d := range GetAllStrategyDefinitions() {
		if d.StrategyType == strategyType {
			return &d
		}
	}
	return nil
}

// GetGridStrategyDefinition 網格策略参數
func GetGridStrategyDefinition() StrategyParamDefinition {
	minZero := 0.0
	minGap := 0.01
	minOne := 1.0
	maxGap := 10.0
	maxTen := 10.0
	max200 := 200.0
	stepPoint1 := 0.1
	stepPoint0001 := 0.0001
	return StrategyParamDefinition{
		StrategyType: "grid",
		Name:         "網格策略",
		Description:  "在價格区间内按网格挂單，低買高賣。",
		Params: []ParamField{
			// 回测時價格區間從 K 線自動推導，不再顯示 price_low / price_high
			{Name: "grid_spacing", Label: "網格間距", Type: "number", Required: false, Default: 200, Min: &minGap, Unit: "USDT", Hint: "每檔價格差，如 200 表示每檔相差 200 USDT。不填則按格子數量均分區間"},
			{Name: "grid_count", Label: "格子數量", Type: "number", Required: false, Default: 20, Min: &minZero, Hint: "最多幾檔。填了間距時表示最多 N 檔；未填間距時表示將區間均分為 N 檔"},
			{Name: "order_quantity", Label: "單笔订單大小", Type: "number", Required: true, Default: 100, Min: &minGap, Unit: "USDT"},
			{Name: "total_capital", Label: "總投入资金", Type: "number", Required: true, Default: 10000, Min: &minGap, Unit: "USDT"},
			{Name: "fee_rate", Label: "手续费率", Type: "number", Required: false, Default: 0.0004, Min: &minZero, Max: &maxGap, Step: &stepPoint0001, Hint: "如 0.0004 表示 0.04%"},
			{Name: "risk_volume_multiplier", Label: "風控-成交量倍數", Type: "number", Required: false, Default: 3.0, Min: &minOne, Max: &maxTen, Step: &stepPoint1, Hint: "成交量超過均量的倍數觸發風控（越小越敏感）"},
			{Name: "risk_average_window", Label: "風控-均線窗口", Type: "number", Required: false, Default: 20, Min: &minOne, Max: &max200, Hint: "計算均價/均量的K線數量"},
		},
	}
}

// GetMomentumStrategyDefinition 动量策略参數
func GetMomentumStrategyDefinition() StrategyParamDefinition {
	minVal := 1.0
	maxVal := 100.0
	return StrategyParamDefinition{
		StrategyType: "momentum",
		Name:         "动量策略",
		Description:  "基於 RSI 的超買超賣策略。",
		Params: []ParamField{
			{Name: "rsi_period", Label: "RSI 周期", Type: "number", Required: false, Default: 14, Min: &minVal, Max: &maxVal},
			{Name: "total_capital", Label: "總投入资金", Type: "number", Required: true, Default: 10000, Min: &minVal, Unit: "USDT"},
			{Name: "fee_rate", Label: "手续费率", Type: "number", Required: false, Default: 0.0004},
		},
	}
}

// GetMeanReversionStrategyDefinition 均值回归策略参數
func GetMeanReversionStrategyDefinition() StrategyParamDefinition {
	minVal := 1.0
	return StrategyParamDefinition{
		StrategyType: "mean_reversion",
		Name:         "均值回归策略",
		Description:  "基於布林带的均值回归。",
		Params: []ParamField{
			{Name: "period", Label: "周期", Type: "number", Required: false, Default: 20, Min: &minVal},
			{Name: "total_capital", Label: "總投入资金", Type: "number", Required: true, Default: 10000, Min: &minVal, Unit: "USDT"},
			{Name: "fee_rate", Label: "手续费率", Type: "number", Required: false, Default: 0.0004},
		},
	}
}

// GetTrendFollowingStrategyDefinition 趋势跟踪策略参數
func GetTrendFollowingStrategyDefinition() StrategyParamDefinition {
	minVal := 1.0
	return StrategyParamDefinition{
		StrategyType: "trend_following",
		Name:         "趋势跟踪策略",
		Description:  "双均線金叉死叉。",
		Params: []ParamField{
			{Name: "fast_period", Label: "快線周期", Type: "number", Required: false, Default: 10, Min: &minVal},
			{Name: "slow_period", Label: "慢線周期", Type: "number", Required: false, Default: 30, Min: &minVal},
			{Name: "total_capital", Label: "總投入资金", Type: "number", Required: true, Default: 10000, Min: &minVal, Unit: "USDT"},
			{Name: "fee_rate", Label: "手续费率", Type: "number", Required: false, Default: 0.0004},
		},
	}
}

// GetDCAStrategyDefinition DCA 策略参數
func GetDCAStrategyDefinition() StrategyParamDefinition {
	minVal := 1.0
	return StrategyParamDefinition{
		StrategyType: "dca",
		Name:         "定投策略",
		Description:  "定期定額買入。",
		Params: []ParamField{
			{Name: "interval_days", Label: "定投间隔(天)", Type: "number", Required: true, Default: 7, Min: &minVal},
			{Name: "amount_per_trade", Label: "每次投入金額", Type: "number", Required: true, Default: 100, Min: &minVal, Unit: "USDT"},
			{Name: "total_capital", Label: "總投入资金上限", Type: "number", Required: true, Default: 10000, Min: &minVal, Unit: "USDT"},
			{Name: "fee_rate", Label: "手续费率", Type: "number", Required: false, Default: 0.0004},
		},
	}
}

// GetMartingaleStrategyDefinition 马丁格尔策略参數
func GetMartingaleStrategyDefinition() StrategyParamDefinition {
	minVal := 1.0
	return StrategyParamDefinition{
		StrategyType: "martingale",
		Name:         "马丁格尔策略",
		Description:  "亏损加倍下注（高风險）。",
		Params: []ParamField{
			{Name: "base_amount", Label: "基础下單金額", Type: "number", Required: true, Default: 100, Min: &minVal, Unit: "USDT"},
			{Name: "multiplier", Label: "加倍倍數", Type: "number", Required: false, Default: 2.0, Min: &minVal},
			{Name: "total_capital", Label: "總投入资金上限", Type: "number", Required: true, Default: 10000, Min: &minVal, Unit: "USDT"},
			{Name: "fee_rate", Label: "手续费率", Type: "number", Required: false, Default: 0.0004},
		},
	}
}

// GetComboStrategyDefinition 组合策略参數
func GetComboStrategyDefinition() StrategyParamDefinition {
	minVal := 1.0
	return StrategyParamDefinition{
		StrategyType: "combo",
		Name:         "组合策略",
		Description:  "多策略组合。",
		Params: []ParamField{
			{Name: "total_capital", Label: "總投入资金", Type: "number", Required: true, Default: 10000, Min: &minVal, Unit: "USDT"},
			{Name: "fee_rate", Label: "手续费率", Type: "number", Required: false, Default: 0.0004},
		},
	}
}
