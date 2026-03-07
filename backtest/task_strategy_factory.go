package backtest

import (
	"fmt"

	"quantmesh/exchange"
)

type StrategyExecutionContext struct {
	Symbol       string
	TotalCapital float64
}

// AdapterBacktestStrategy bridges StrategyAdapter-based strategies into MultiStrategyEngine.
type AdapterBacktestStrategy struct {
	name         string
	strategyType string
	adapter      StrategyAdapter
	totalCapital float64
	account      *BacktestAccount
	positionSize float64
}

func (s *AdapterBacktestStrategy) OnInit(account *BacktestAccount, cfg interface{}) error {
	s.account = account
	return nil
}

func (s *AdapterBacktestStrategy) OnKline(kline TickKline, timestamp int64) ([]TickOrder, error) {
	if s.adapter == nil {
		return nil, fmt.Errorf("strategy adapter is nil")
	}
	signal := s.adapter.OnCandle(&exchange.Candle{
		Symbol:    s.account.Symbol,
		Open:      kline.Open,
		High:      kline.High,
		Low:       kline.Low,
		Close:     kline.Close,
		Volume:    kline.Volume,
		Timestamp: kline.Timestamp,
		IsClosed:  true,
	})
	switch signal.Action {
	case "buy":
		if s.account == nil || s.positionSize > 0 {
			return nil, nil
		}
		if s.totalCapital <= 0 || kline.Close <= 0 {
			return nil, nil
		}
		size := (s.totalCapital * 0.95 * s.account.Leverage) / kline.Close
		if size <= 0 {
			return nil, nil
		}
		return []TickOrder{{
			OrderID:  fmt.Sprintf("%s_buy_%d", s.name, timestamp),
			Side:     "buy",
			Price:    kline.Close,
			Size:     size,
			Strategy: s.name,
		}}, nil
	case "sell":
		if s.account == nil || s.positionSize <= 0 {
			return nil, nil
		}
		return []TickOrder{{
			OrderID:  fmt.Sprintf("%s_sell_%d", s.name, timestamp),
			Side:     "sell",
			Price:    kline.Close,
			Size:     s.positionSize,
			Strategy: s.name,
		}}, nil
	default:
		return nil, nil
	}
}

func (s *AdapterBacktestStrategy) OnTrade(trade TickTrade) {
	if trade.Strategy != s.name {
		return
	}
	switch trade.Side {
	case "buy":
		s.positionSize += trade.Size
	case "sell":
		s.positionSize -= trade.Size
		if s.positionSize < 0 {
			s.positionSize = 0
		}
	}
}

func (s *AdapterBacktestStrategy) GetName() string {
	return s.name
}

func (s *AdapterBacktestStrategy) GetType() string {
	return s.strategyType
}

func (s *AdapterBacktestStrategy) GetConfig() map[string]interface{} {
	return map[string]interface{}{
		"total_capital": s.totalCapital,
	}
}

func NormalizeTaskStrategies(strategies []TaskStrategy) []TaskStrategy {
	if len(strategies) == 0 {
		return nil
	}
	normalized := make([]TaskStrategy, len(strategies))
	copy(normalized, strategies)

	totalWeight := 0.0
	for i := range normalized {
		if normalized[i].Weight <= 0 {
			normalized[i].Weight = 1
		}
		totalWeight += normalized[i].Weight
	}
	if totalWeight <= 0 {
		even := 1.0 / float64(len(normalized))
		for i := range normalized {
			normalized[i].Weight = even
		}
		return normalized
	}
	for i := range normalized {
		normalized[i].Weight = normalized[i].Weight / totalWeight
	}
	return normalized
}

func CreateTaskBacktestStrategy(strategy TaskStrategy, ctx StrategyExecutionContext) (BacktestStrategy, error) {
	totalCapital := ctx.TotalCapital * strategy.Weight
	switch strategy.Type {
	case "grid":
		gridCount := getIntParam(strategy.Config, "grid_count", 50)
		gridSpacing := getFloatParam(strategy.Config, "grid_spacing", 0.0025)
		gridLeverage := getIntParam(strategy.Config, "grid_leverage", 5)
		return NewGridBacktestStrategy(
			fmt.Sprintf("grid_%s", ctx.Symbol),
			ctx.Symbol,
			gridCount,
			gridSpacing,
			float64(gridLeverage),
			totalCapital,
		), nil
	case "dca", "dca_enhanced":
		baseOrderAmount := getFloatParam(strategy.Config, "base_order_amount", 30.0)
		maxOrders := getIntParam(strategy.Config, "max_orders", 10)
		return NewDCABacktestStrategy(
			fmt.Sprintf("dca_%s", ctx.Symbol),
			ctx.Symbol,
			baseOrderAmount,
			float64(maxOrders),
			totalCapital,
		), nil
	case "martingale":
		baseOrderAmount := getFloatParam(strategy.Config, "base_order_amount", 30.0)
		multiplier := getFloatParam(strategy.Config, "multiplier", 2.0)
		maxOrders := getIntParam(strategy.Config, "max_orders", 7)
		return NewMartingaleBacktestStrategy(
			fmt.Sprintf("martingale_%s", ctx.Symbol),
			ctx.Symbol,
			baseOrderAmount,
			multiplier,
			float64(maxOrders),
			totalCapital,
		), nil
	case "trend", "trend_following":
		return &AdapterBacktestStrategy{
			name:         fmt.Sprintf("trend_following_%s", ctx.Symbol),
			strategyType: "trend_following",
			adapter:      NewTrendFollowingAdapterWithParams(strategy.Config),
			totalCapital: totalCapital,
		}, nil
	case "momentum":
		return &AdapterBacktestStrategy{
			name:         fmt.Sprintf("momentum_%s", ctx.Symbol),
			strategyType: "momentum",
			adapter:      NewMomentumAdapterWithParams(strategy.Config),
			totalCapital: totalCapital,
		}, nil
	case "mean_reversion":
		return &AdapterBacktestStrategy{
			name:         fmt.Sprintf("mean_reversion_%s", ctx.Symbol),
			strategyType: "mean_reversion",
			adapter:      NewMeanReversionAdapterWithParams(strategy.Config),
			totalCapital: totalCapital,
		}, nil
	case "combo":
		subStrategiesCfg, _ := strategy.Config["strategies"].([]interface{})
		if len(subStrategiesCfg) == 0 {
			return nil, fmt.Errorf("combo strategy requires sub-strategies configuration")
		}
		subStrategies := make([]TaskStrategy, 0, len(subStrategiesCfg))
		for _, subCfg := range subStrategiesCfg {
			subStrategyInstance, ok := subCfg.(map[string]interface{})
			if !ok {
				continue
			}
			subStrategies = append(subStrategies, TaskStrategy{
				Type:   getStringParam(subStrategyInstance, "type"),
				Weight: getFloatParam(subStrategyInstance, "weight", 0),
				Config: subStrategyInstance,
			})
		}
		subStrategies = NormalizeTaskStrategies(subStrategies)
		if len(subStrategies) == 0 {
			return nil, fmt.Errorf("no valid sub-strategies created")
		}
		created := make([]BacktestStrategy, 0, len(subStrategies))
		weights := make([]float64, 0, len(subStrategies))
		for _, subStrategy := range subStrategies {
			bs, err := CreateTaskBacktestStrategy(subStrategy, StrategyExecutionContext{
				Symbol:       ctx.Symbol,
				TotalCapital: totalCapital,
			})
			if err != nil {
				return nil, err
			}
			created = append(created, bs)
			weights = append(weights, subStrategy.Weight)
		}
		return NewComboBacktestStrategy(
			fmt.Sprintf("combo_%s", ctx.Symbol),
			ctx.Symbol,
			totalCapital,
			created,
			weights,
		), nil
	default:
		return nil, fmt.Errorf("unsupported strategy type: %s", strategy.Type)
	}
}

func RunMultiStrategyTask(task *BacktestTask, candles []*exchange.Candle) (*MultiStrategyResult, error) {
	if task == nil {
		return nil, fmt.Errorf("task is nil")
	}
	if len(task.Strategies) == 0 {
		return nil, fmt.Errorf("task has no strategies")
	}
	if len(candles) == 0 {
		return nil, fmt.Errorf("candles data is empty")
	}

	engine := NewMultiStrategyEngine(&EngineConfig{
		Symbol:         task.Symbol,
		InitialCapital: task.TotalCapital,
		StartDate:      task.StartTime,
		EndDate:        task.EndTime,
		MatcherConfig:  DefaultMatcherConfig(),
		Leverage:       1,
	})
	engine.Klines = make([]TickKline, 0, len(candles))
	for _, candle := range candles {
		if candle == nil {
			continue
		}
		engine.Klines = append(engine.Klines, TickKline{
			Timestamp: candle.Timestamp,
			Open:      candle.Open,
			High:      candle.High,
			Low:       candle.Low,
			Close:     candle.Close,
			Volume:    candle.Volume,
		})
	}

	ctx := StrategyExecutionContext{
		Symbol:       task.Symbol,
		TotalCapital: task.TotalCapital,
	}
	for _, strategy := range NormalizeTaskStrategies(task.Strategies) {
		bs, err := CreateTaskBacktestStrategy(strategy, ctx)
		if err != nil {
			return nil, err
		}
		if err := engine.AddStrategy(bs); err != nil {
			return nil, err
		}
	}

	return engine.Run()
}

func getIntParam(params map[string]interface{}, key string, defaultValue int) int {
	if params == nil {
		return defaultValue
	}
	if val, ok := params[key]; ok {
		switch v := val.(type) {
		case int:
			return v
		case int32:
			return int(v)
		case int64:
			return int(v)
		case float64:
			return int(v)
		case float32:
			return int(v)
		}
	}
	return defaultValue
}

func getFloatParam(params map[string]interface{}, key string, defaultValue float64) float64 {
	if params == nil {
		return defaultValue
	}
	if val, ok := params[key]; ok {
		switch v := val.(type) {
		case float64:
			return v
		case float32:
			return float64(v)
		case int:
			return float64(v)
		case int32:
			return float64(v)
		case int64:
			return float64(v)
		}
	}
	return defaultValue
}

func getStringParam(params map[string]interface{}, key string) string {
	if params == nil {
		return ""
	}
	if val, ok := params[key]; ok {
		if str, ok := val.(string); ok {
			return str
		}
	}
	return ""
}
