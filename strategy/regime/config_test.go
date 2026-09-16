package regime

import (
	"errors"
	"testing"
	"time"
)

func TestRegimeConfigWithDefaults(t *testing.T) {
	got := RegimeConfig{}.WithDefaults()
	want := RegimeConfig{
		KlineInterval:         DefaultKlineInterval,
		ADXPeriod:             DefaultADXPeriod,
		ADXEnterThreshold:     DefaultADXEnterThreshold,
		ADXExitThreshold:      DefaultADXExitThreshold,
		EMAPeriod:             DefaultEMAPeriod,
		EMASlopeLookback:      DefaultEMASlopeLookback,
		EMASlopeMinATR:        DefaultEMASlopeMinATR,
		MinDwellBars:          DefaultMinDwellBars,
		ATRPeriod:             DefaultATRPeriod,
		ATRPercentileLookback: DefaultATRPercentileLookback,
		BootstrapBars:         DefaultBootstrapBars,
		StaleMultiplier:       DefaultStaleMultiplier,
	}
	if got != want {
		t.Fatalf("defaults mismatch:\n got %+v\nwant %+v", got, want)
	}
	if err := got.Validate(); err != nil {
		t.Fatalf("defaults should validate: %v", err)
	}

	// 自定义值保留；进入阈值低于默认退出阈值时，退出阈值跟随进入阈值
	custom := RegimeConfig{Enabled: true, KlineInterval: "4h", ADXEnterThreshold: 18}.WithDefaults()
	if !custom.Enabled || custom.KlineInterval != "4h" || custom.ADXEnterThreshold != 18 || custom.ADXExitThreshold != 18 {
		t.Fatalf("custom values not preserved: %+v", custom)
	}
}

func TestRegimeConfigValidate(t *testing.T) {
	base := RegimeConfig{}.WithDefaults()
	tests := []struct {
		name    string
		mutate  func(*RegimeConfig)
		wantErr bool
	}{
		{name: "defaults ok", mutate: func(*RegimeConfig) {}},
		{name: "bad interval unit", mutate: func(c *RegimeConfig) { c.KlineInterval = "1w" }, wantErr: true},
		{name: "exit above enter", mutate: func(c *RegimeConfig) { c.ADXExitThreshold = 30 }, wantErr: true},
		{name: "zero dwell", mutate: func(c *RegimeConfig) { c.MinDwellBars = 0 }, wantErr: true},
		{name: "zero slope min", mutate: func(c *RegimeConfig) { c.EMASlopeMinATR = 0 }, wantErr: true},
		{name: "stale multiplier below 1", mutate: func(c *RegimeConfig) { c.StaleMultiplier = 0.5 }, wantErr: true},
		{name: "negative poll", mutate: func(c *RegimeConfig) { c.PollIntervalSeconds = -1 }, wantErr: true},
		{name: "bootstrap too small", mutate: func(c *RegimeConfig) { c.BootstrapBars = 100 }, wantErr: true},
		{name: "bootstrap too large", mutate: func(c *RegimeConfig) { c.BootstrapBars = 2000 }, wantErr: true},
		{name: "bootstrap exactly required", mutate: func(c *RegimeConfig) { c.BootstrapBars = c.RequiredBars() }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := base
			tt.mutate(&c)
			err := c.Validate()
			if (err != nil) != tt.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, tt.wantErr)
			}
			if err != nil && !errors.Is(err, ErrInvalidConfig) {
				t.Fatalf("error should wrap ErrInvalidConfig: %v", err)
			}
		})
	}
}

func TestRequiredBars(t *testing.T) {
	c := RegimeConfig{}.WithDefaults()
	// max(2*14, 50+5, 14+100)
	if got := c.RequiredBars(); got != 114 {
		t.Fatalf("RequiredBars=%d want 114", got)
	}
}

func TestParseKlineInterval(t *testing.T) {
	tests := []struct {
		in      string
		want    time.Duration
		wantErr bool
	}{
		{in: "1m", want: time.Minute},
		{in: "15m", want: 15 * time.Minute},
		{in: "1h", want: time.Hour},
		{in: " 4h ", want: 4 * time.Hour},
		{in: "1d", want: 24 * time.Hour},
		{in: "1w", wantErr: true},
		{in: "1M", wantErr: true},
		{in: "h", wantErr: true},
		{in: "0h", wantErr: true},
		{in: "-1h", wantErr: true},
		{in: "", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := ParseKlineInterval(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, tt.wantErr)
			}
			if got != tt.want {
				t.Fatalf("got %v want %v", got, tt.want)
			}
		})
	}
}

func TestAdaptiveIntervalConfigValidate(t *testing.T) {
	tests := []struct {
		name    string
		cfg     AdaptiveIntervalConfig
		wantErr bool
	}{
		{name: "defaults", cfg: AdaptiveIntervalConfig{}.WithDefaults()},
		{name: "zero multiplier", cfg: AdaptiveIntervalConfig{ChangeThresholdRatio: 0.1}, wantErr: true},
		{name: "min above max", cfg: AdaptiveIntervalConfig{ATRMultiplier: 1, MinInterval: 10, MaxInterval: 5}, wantErr: true},
		{name: "negative min", cfg: AdaptiveIntervalConfig{ATRMultiplier: 1, MinInterval: -1}, wantErr: true},
		{name: "negative ratio", cfg: AdaptiveIntervalConfig{ATRMultiplier: 1, ChangeThresholdRatio: -0.1}, wantErr: true},
		{name: "unbounded max ok", cfg: AdaptiveIntervalConfig{ATRMultiplier: 1, MinInterval: 10}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.cfg.Validate(); (err != nil) != tt.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, tt.wantErr)
			}
		})
	}
	d := AdaptiveIntervalConfig{}.WithDefaults()
	if d.ATRMultiplier != DefaultATRMultiplier || d.ChangeThresholdRatio != DefaultChangeThresholdRatio {
		t.Fatalf("unexpected defaults %+v", d)
	}
}
