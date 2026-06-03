package backtest

import (
	"strings"
	"testing"
)

func TestOptimTaskSearchSpaceJSONRoundTrip(t *testing.T) {
	task := &OptimTask{
		SearchSpace: OptimSearchSpace{
			Strategy: "grid",
			Ranges: map[string]OptimParamRange{
				"grid_count": {Min: 5, Max: 20, Step: 5},
				"spacing":    {Min: 0.01, Max: 0.03, Step: 0.01},
			},
		},
	}

	encoded, err := task.SearchSpaceToJSON()
	if err != nil {
		t.Fatalf("SearchSpaceToJSON() error = %v", err)
	}
	if !strings.Contains(encoded, `"strategy":"grid"`) {
		t.Fatalf("SearchSpaceToJSON() = %s, want strategy", encoded)
	}

	var decoded OptimTask
	if err := decoded.SearchSpaceFromJSON(encoded); err != nil {
		t.Fatalf("SearchSpaceFromJSON() error = %v", err)
	}
	if decoded.SearchSpace.Strategy != "grid" || len(decoded.SearchSpace.Ranges) != 2 {
		t.Fatalf("decoded search space = %+v", decoded.SearchSpace)
	}
	if err := decoded.SearchSpaceFromJSON("{"); err == nil {
		t.Fatalf("SearchSpaceFromJSON(invalid) expected error")
	}
}

func TestExchangeStyleMetricsString(t *testing.T) {
	metrics := ExchangeStyleMetrics{
		AverageCost:            100.12345,
		CurrentPosition:        2.5,
		AverageCostPnL:         12.34,
		AverageCostRealizedPnL: -5.67,
		ExchangeWinRate:        66.666,
		ExchangeProfitFactor:   1.5,
		ExchangeTotalProfit:    3,
		ExchangeTotalLoss:      2,
		CurrentPositionValue:   250,
	}

	got := metrics.String()
	for _, want := range []string{"平均成本: 100.1235", "当前持仓: 2.5000", "未实现盈亏: 12.34", "已实现盈亏: -5.67", "交易所胜率: 66.67%"} {
		if !strings.Contains(got, want) {
			t.Fatalf("String() = %q, missing %q", got, want)
		}
	}
}
