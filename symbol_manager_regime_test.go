package main

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/position"
)

// fakeKlineSource 不發網絡請求的 K 線源
type fakeKlineSource struct {
	calls atomic.Int32
}

func (f *fakeKlineSource) GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*exchange.Candle, error) {
	f.calls.Add(1)
	return nil, errors.New("offline")
}

func TestNewGridRegimeRuntime(t *testing.T) {
	tests := []struct {
		name         string
		mutate       func(c *config.Config)
		wantNil      bool
		wantErr      bool
		wantAdaptive bool
		wantFilter   bool
	}{
		{name: "全部關閉不創建", mutate: func(c *config.Config) {}, wantNil: true},
		{name: "只開 regime_filter", mutate: func(c *config.Config) { c.Trading.RegimeFilter.Enabled = true }, wantFilter: true},
		{name: "只開 upper_bound_freeze 也需要檢測器", mutate: func(c *config.Config) { c.Trading.UpperBoundFreeze.Enabled = true }},
		{name: "非法 K 線周期拒絕", mutate: func(c *config.Config) {
			c.Trading.RegimeFilter.Enabled = true
			c.Trading.RegimeFilter.KlineInterval = "1w"
		}, wantErr: true},
		{name: "與 dynamic_adjustment.price_interval 衝突時停用自適應", mutate: func(c *config.Config) {
			c.Trading.AdaptiveInterval.Enabled = true
			c.Trading.DynamicAdjustment.Enabled = true
			c.Trading.DynamicAdjustment.PriceInterval.Enabled = true
		}},
		{name: "自適應間隔", mutate: func(c *config.Config) { c.Trading.AdaptiveInterval.Enabled = true }, wantAdaptive: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			tt.mutate(cfg)
			rt, err := newGridRegimeRuntime(cfg, "ETHUSDT", &fakeKlineSource{})
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if (rt == nil) != tt.wantNil {
				t.Fatalf("runtime nil = %v, want %v", rt == nil, tt.wantNil)
			}
			if rt == nil {
				return
			}
			if rt.opts.Adaptive.Enabled != tt.wantAdaptive || rt.opts.FilterEnabled != tt.wantFilter {
				t.Fatalf("opts = %+v", rt.opts)
			}
		})
	}
}

func TestGridRegimeRuntimeStartStop(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.Symbol = "ETHUSDT"
	cfg.Trading.Direction = "LONG"
	cfg.Trading.PriceInterval = 1
	cfg.Trading.RegimeFilter.Enabled = true
	src := &fakeKlineSource{}
	rt, err := newGridRegimeRuntime(cfg, "ETHUSDT", src)
	if err != nil || rt == nil {
		t.Fatalf("newGridRegimeRuntime: rt=%v err=%v", rt, err)
	}
	spm := position.NewSuperPositionManager(cfg, nil, nil, 2, 3)
	if err := rt.start(context.Background(), spm); err != nil {
		t.Fatalf("start: %v", err)
	}
	rt.stop()
	rt.stop() // 冪等
	if cfg.Trading.PriceInterval != 1 {
		t.Fatalf("unready detector must not change interval, got %v", cfg.Trading.PriceInterval)
	}

	var nilRT *gridRegimeRuntime
	if err := nilRT.start(context.Background(), spm); err != nil {
		t.Fatalf("nil runtime start: %v", err)
	}
	nilRT.stop()
}
