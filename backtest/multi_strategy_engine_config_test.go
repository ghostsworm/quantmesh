package backtest

import (
	"math"
	"strings"
	"testing"
)

func validEngineConfigForValidation() *EngineConfig {
	return &EngineConfig{
		Symbol:          "BTCUSDT",
		InitialCapital:  1000,
		CommissionRate:  0.0004,
		Leverage:        1,
		MaxCapitalRatio: 1,
		MatcherConfig:   DefaultMatcherConfig(),
	}
}

func TestNewMultiStrategyEngineRejectsInvalidConfigBeforeRunning(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*EngineConfig)
		want   string
	}{
		{name: "negative commission", mutate: func(cfg *EngineConfig) { cfg.CommissionRate = -0.001 }, want: "commission rate"},
		{name: "non-finite commission", mutate: func(cfg *EngineConfig) { cfg.CommissionRate = math.NaN() }, want: "commission rate"},
		{name: "zero capital", mutate: func(cfg *EngineConfig) { cfg.InitialCapital = 0 }, want: "initial capital"},
		{name: "infinite leverage", mutate: func(cfg *EngineConfig) { cfg.Leverage = math.Inf(1) }, want: "leverage"},
		{name: "capital ratio above one", mutate: func(cfg *EngineConfig) { cfg.MaxCapitalRatio = 1.1 }, want: "max capital ratio"},
		{name: "negative buy slippage", mutate: func(cfg *EngineConfig) { cfg.MatcherConfig.BuySlippage = -1 }, want: "buy slippage"},
		{name: "sell rebate multiplier", mutate: func(cfg *EngineConfig) { cfg.MatcherConfig.SellSlippage = 1.01 }, want: "sell slippage"},
		{name: "volume ratio above one", mutate: func(cfg *EngineConfig) { cfg.MatcherConfig.MaxVolumeRatio = 1.01 }, want: "max volume ratio"},
		{name: "negative trade limit", mutate: func(cfg *EngineConfig) { cfg.MatcherConfig.MaxGridTradesPerMinute = -1 }, want: "max grid trades"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validEngineConfigForValidation()
			tt.mutate(cfg)
			engine := NewMultiStrategyEngine(cfg)
			_, err := engine.Run()
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Run() error = %v, want error containing %q", err, tt.want)
			}
		})
	}
}

func TestNewMultiStrategyEngineHandlesNilConfigWithoutPanic(t *testing.T) {
	engine := NewMultiStrategyEngine(nil)
	if _, err := engine.Run(); err == nil || !strings.Contains(err.Error(), "config is required") {
		t.Fatalf("Run() error = %v, want missing config error", err)
	}
}

func TestNewMultiStrategyEngineDoesNotMutateCallerConfig(t *testing.T) {
	cfg := validEngineConfigForValidation()
	cfg.CommissionRate = 0
	cfg.MatcherConfig = MatcherConfig{}
	_ = NewMultiStrategyEngine(cfg)
	if cfg.CommissionRate != 0 || cfg.MatcherConfig != (MatcherConfig{}) {
		t.Fatalf("constructor mutated caller config: %+v", cfg)
	}
}

func TestNewMultiStrategyEngineDefaultsUnspecifiedMatcherFieldsIndividually(t *testing.T) {
	cfg := validEngineConfigForValidation()
	cfg.MatcherConfig = MatcherConfig{BuySlippage: 1.002}
	engine := NewMultiStrategyEngine(cfg)
	if err := engine.validateConfig(); err != nil {
		t.Fatalf("validateConfig() error = %v", err)
	}
	if engine.Config.MatcherConfig.BuySlippage != 1.002 || engine.Config.MatcherConfig.SellSlippage != 0.9999 {
		t.Fatalf("unexpected normalized matcher config: %+v", engine.Config.MatcherConfig)
	}
}
