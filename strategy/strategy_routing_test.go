package strategy

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/position"
)

type routingTestStrategy struct {
	name string
	hit  atomic.Int64
}

func (s *routingTestStrategy) Name() string { return s.name }
func (s *routingTestStrategy) Initialize(cfg *config.Config, executor position.OrderExecutorInterface, exchange position.IExchange) error {
	return nil
}
func (s *routingTestStrategy) OnPriceChange(price float64) error { return nil }
func (s *routingTestStrategy) OnOrderUpdate(update *position.OrderUpdate) error {
	s.hit.Add(1)
	return nil
}
func (s *routingTestStrategy) GetPositions() []*Position                    { return nil }
func (s *routingTestStrategy) GetOrders() []*Order                          { return nil }
func (s *routingTestStrategy) GetStatistics() *StrategyStatistics           { return nil }
func (s *routingTestStrategy) Start(ctx context.Context) error              { return nil }
func (s *routingTestStrategy) Stop() error                                  { return nil }
func (s *routingTestStrategy) SetEventBus(bus EventBus)                     {}
func (s *routingTestStrategy) GetVisualizationData() map[string]interface{} { return nil }

type runningStatusStrategy struct {
	routingTestStrategy
	running atomic.Int64
}

func (s *runningStatusStrategy) IsRunning() bool {
	return s.running.Load() == 1
}

func TestStrategyManagerOnOrderUpdateForStrategy(t *testing.T) {
	cfg := &config.Config{}
	cfg.Strategies.Configs = map[string]config.StrategyConfig{
		"dca":        {Enabled: true},
		"martingale": {Enabled: true},
	}

	sm := NewStrategyManager(cfg, 1000)
	dca := &routingTestStrategy{name: "dca"}
	martingale := &routingTestStrategy{name: "martingale"}
	sm.RegisterStrategy("dca", dca, 1, 0)
	sm.RegisterStrategy("martingale", martingale, 1, 0)

	sm.OnOrderUpdateForStrategy("dca", &position.OrderUpdate{OrderID: 1001, Status: "FILLED"})
	time.Sleep(20 * time.Millisecond)

	if got := dca.hit.Load(); got != 1 {
		t.Fatalf("dca 策略应收到 1 次回调，实际 %d", got)
	}
	if got := martingale.hit.Load(); got != 0 {
		t.Fatalf("martingale 策略不应收到回调，实际 %d", got)
	}
}

func TestStrategyManagerStatusUsesRuntimeState(t *testing.T) {
	cfg := &config.Config{}
	cfg.Strategies.Configs = map[string]config.StrategyConfig{
		"grid": {Enabled: true, Type: "grid", Weight: 1},
	}

	sm := NewStrategyManager(cfg, 1000)
	strategy := &runningStatusStrategy{routingTestStrategy: routingTestStrategy{name: "grid"}}
	sm.RegisterStrategy("grid", strategy, 1, 0)

	status := sm.GetStrategyStatus("grid")
	if status == nil {
		t.Fatal("应返回策略状态")
	}
	if status.IsRunning {
		t.Fatal("策略尚未成功启动时不应仅因 enabled=true 就显示 running=true")
	}

	strategy.running.Store(1)
	status = sm.GetStrategyStatus("grid")
	if status == nil || !status.IsRunning {
		t.Fatal("策略运行态为 true 时应显示 running=true")
	}

	cfg.Strategies.Configs["grid"] = config.StrategyConfig{Enabled: false, Type: "grid", Weight: 1}
	status = sm.GetStrategyStatus("grid")
	if status == nil {
		t.Fatal("应返回策略状态")
	}
	if status.IsRunning {
		t.Fatal("策略配置被禁用时不应显示 running=true")
	}
}
