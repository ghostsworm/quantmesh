package optimizer

import (
	"context"

	"quantmesh/backtest"
	"quantmesh/exchange"
)

// Range 浮点参數範圍 (min, max, step)
type Range struct {
	Min  float64 `json:"min"`
	Max  float64 `json:"max"`
	Step float64 `json:"step"`
}

// IntRange 整數参數範圍
type IntRange struct {
	Min  int `json:"min"`
	Max  int `json:"max"`
	Step int `json:"step"`
}

// OptimSearchSpace 参數搜索空间
type OptimSearchSpace struct {
	PriceLowRange   Range   `json:"price_low_range"`
	PriceHighRange  Range   `json:"price_high_range"`
	GridCountRange  IntRange `json:"grid_count_range"`
	OrderQtyRange   Range   `json:"order_qty_range"`
}

// OptimConfig 优化配置
type OptimConfig struct {
	Method        string  `json:"method"`         // "grid", "bayesian", "genetic"
	Lambda        float64 `json:"lambda"`         // 风險权重 0.3-0.7
	MaxIterations int     `json:"max_iterations"` // 最大迭代次數
	Tolerance     float64 `json:"tolerance"`     // 收敛容差
	Parallelism   int     `json:"parallelism"`   // 並行度，0 表示 NumCPU
}

// Optimizer 优化器接口
type Optimizer interface {
	// Run 執行优化，返回最优結果；ctx 用於取消
	Run(ctx context.Context, symbol string, candles []*exchange.Candle, space OptimSearchSpace, config OptimConfig, initialCapital float64) (*OptimResult, error)
}

// BacktestRunner 單次回测執行器，供各优化器調用
func BacktestRunner(symbol string, candles []*exchange.Candle, params backtest.GridBacktestParams, initialCapital float64) (*backtest.BacktestResult, error) {
	return backtest.RunGridBacktest(symbol, candles, params, initialCapital)
}

// ParamsFromSpace 從搜索空间生成單组回测参數（用於固定 FeeRate 等）
func ParamsFromSpace(priceLow, priceHigh float64, gridCount int, orderQty, totalCapital, feeRate, slippage float64) backtest.GridBacktestParams {
	if feeRate <= 0 {
		feeRate = 0.0004
	}
	if slippage <= 0 {
		slippage = 0.0003
	}
	return backtest.GridBacktestParams{
		PriceLow:       priceLow,
		PriceHigh:      priceHigh,
		GridCount:      gridCount,
		OrderQuantity:  orderQty,
		TotalCapital:   totalCapital,
		FeeRate:        feeRate,
		SlippageRatio:  slippage,
	}
}

// DefaultOptimConfig 返回默认优化配置
func DefaultOptimConfig() OptimConfig {
	return OptimConfig{
		Method:        "grid",
		Lambda:        0.5,
		MaxIterations: 100,
		Tolerance:     1e-4,
		Parallelism:   0,
	}
}

// ValidateSearchSpace 校驗搜索空间合法性
func ValidateSearchSpace(space OptimSearchSpace) error {
	if space.PriceLowRange.Min >= space.PriceLowRange.Max {
		return errInvalidRange
	}
	if space.PriceHighRange.Min >= space.PriceHighRange.Max {
		return errInvalidRange
	}
	if space.PriceLowRange.Step <= 0 || space.PriceHighRange.Step <= 0 {
		return errInvalidRange
	}
	if space.GridCountRange.Min <= 0 || space.GridCountRange.Max < space.GridCountRange.Min {
		return errInvalidRange
	}
	if space.OrderQtyRange.Min <= 0 || space.OrderQtyRange.Max < space.OrderQtyRange.Min {
		return errInvalidRange
	}
	return nil
}
