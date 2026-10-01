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

func TestValidateDCAEnhancedConfigRejectsOverflowingSafetyOrderProgression(t *testing.T) {
	cfg := defaultDCAEnhancedConfig()
	cfg.SafetyOrderAmount = math.MaxFloat64 * 0.75
	cfg.SafetyOrderScale = 2
	cfg.MaxSafetyOrders = 2
	if err := validateDCAEnhancedConfig(cfg); err == nil || !strings.Contains(err.Error(), "progression") {
		t.Fatalf("expected overflowing safety-order progression to be rejected, got %v", err)
	}
}

func TestDCARoundQuantityDownRejectsInvalidInputs(t *testing.T) {
	tests := []struct {
		name     string
		quantity float64
		decimals int
	}{
		{name: "nan quantity", quantity: math.NaN(), decimals: 2},
		{name: "infinite quantity", quantity: math.Inf(1), decimals: 2},
		{name: "scaled overflow", quantity: math.MaxFloat64, decimals: 1},
		{name: "negative precision", quantity: 1, decimals: -1},
		{name: "excessive precision", quantity: 1, decimals: 19},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if quantity, err := dcaRoundQuantityDown(test.quantity, test.decimals); err == nil {
				t.Fatalf("expected invalid quantity to be rejected, got %v", quantity)
			}
		})
	}
}

func TestDCAOnPriceRejectsInvalidMarketPricesWithoutMutatingState(t *testing.T) {
	prices := []float64{math.NaN(), math.Inf(1), math.Inf(-1), 0, -1}
	for _, price := range prices {
		strategy := NewDCAEnhancedStrategy("dca", "BTCUSDT", nil, nil, nil, nil)
		if err := strategy.onPrice(price, true); err == nil {
			t.Fatalf("onPrice(%v) should reject invalid market price", price)
		}
		if len(strategy.priceHistory) != 0 || len(strategy.layers) != 0 || strategy.totalQty != 0 {
			t.Fatalf("onPrice(%v) mutated state: prices=%d layers=%d quantity=%v", price, len(strategy.priceHistory), len(strategy.layers), strategy.totalQty)
		}
	}
}
