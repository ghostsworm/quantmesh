package safety

import (
	"context"
	"testing"
	"time"

	"quantmesh/config"
)

func TestFundingRateMonitorNilExchangeDoesNotStart(t *testing.T) {
	cfg := &config.Config{}
	cfg.FundingRate.Enabled = true

	monitor := NewFundingRateMonitor(cfg, nil, "BTCUSDT")
	monitor.Start(nil)

	monitor.mu.RLock()
	running := monitor.running
	monitor.mu.RUnlock()
	if running {
		t.Fatal("交易所为空时资金费率监控不应启动")
	}
}

func TestCompositeRiskControllerCanRestartAfterContextCancel(t *testing.T) {
	cfg := &config.Config{}
	cfg.CompositeRisk.Enabled = true
	cfg.CompositeRisk.EvaluateInterval = 1
	controller := NewCompositeRiskController(cfg)
	ctx, cancel := context.WithCancel(context.Background())

	controller.Start(ctx)
	cancel()

	deadline := time.After(time.Second)
	for {
		controller.mu.RLock()
		running := controller.running
		controller.mu.RUnlock()
		if !running {
			break
		}

		select {
		case <-deadline:
			t.Fatal("context 取消后复合风控应自动标记为停止")
		default:
			time.Sleep(10 * time.Millisecond)
		}
	}

	controller.Start(nil)
	controller.mu.RLock()
	running := controller.running
	controller.mu.RUnlock()
	if !running {
		t.Fatal("复合风控应允许使用空 context 安全重启")
	}
	controller.Stop()
}
