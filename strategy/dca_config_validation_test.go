package strategy

import (
	"context"
	"math"
	"strings"
	"testing"
)

func TestNewDCAEnhancedStrategyRejectsUnsafeConfigurationBeforeStart(t *testing.T) {
	tests := []struct {
		name   string
		config map[string]interface{}
		want   string
	}{
		{name: "negative safety order capacity", config: map[string]interface{}{"max_safety_orders": -1}, want: "max_safety_orders"},
		{name: "excessive safety order capacity", config: map[string]interface{}{"max_safety_orders": 51}, want: "max_safety_orders"},
		{name: "fractional safety order capacity", config: map[string]interface{}{"max_safety_orders": 2.5}, want: "max_safety_orders"},
		{name: "malformed safety order capacity", config: map[string]interface{}{"max_safety_orders": "5"}, want: "max_safety_orders"},
		{name: "non-finite base amount", config: map[string]interface{}{"base_order_amount": math.NaN()}, want: "base_order_amount"},
		{name: "infinite stop loss", config: map[string]interface{}{"stop_loss": math.Inf(1)}, want: "stop_loss"},
		{name: "non-positive ATR period", config: map[string]interface{}{"atr_period": 0}, want: "atr_period"},
		{name: "inverted price step range", config: map[string]interface{}{"min_price_step": 4.0, "max_price_step": 2.0}, want: "min_price_step"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			strategy := NewDCAEnhancedStrategy("dca", "BTCUSDT", nil, nil, nil, test.config)
			if strategy.configErr == nil || !strings.Contains(strategy.configErr.Error(), test.want) {
				t.Fatalf("config error = %v, want error mentioning %q", strategy.configErr, test.want)
			}
			if len(strategy.layers) != 0 || strategy.maxLayers < 1 || strategy.maxLayers > 51 {
				t.Fatalf("unsafe constructor allocation: layers=%d maxLayers=%d", len(strategy.layers), strategy.maxLayers)
			}
			if err := strategy.Initialize(nil, nil, nil); err == nil {
				t.Fatal("Initialize accepted invalid DCA configuration")
			}
			if err := strategy.Start(context.Background()); err == nil {
				t.Fatal("Start accepted invalid DCA configuration")
			}
		})
	}
}

func TestValidateDCAEnhancedConfigAcceptsZeroSafetyLayers(t *testing.T) {
	cfg := defaultDCAEnhancedConfig()
	cfg.MaxSafetyOrders = 0
	if err := validateDCAEnhancedConfig(cfg); err != nil {
		t.Fatalf("zero safety layers should allow a base-only DCA config: %v", err)
	}
}
