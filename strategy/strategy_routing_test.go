package strategy

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/position"
)

type startOutcomeStrategy struct {
	routingTestStrategy
	startErr error
	stopped  bool
}

func (s *startOutcomeStrategy) Start(context.Context) error { return s.startErr }
func (s *startOutcomeStrategy) Stop() error {
	s.stopped = true
	return nil
}

type routingTestStrategy struct {
	name string
	hit  atomic.Int64
}

type dynamicPerformanceTestStrategy struct {
	routingTestStrategy
	stats StrategyStatistics
}

func (s *dynamicPerformanceTestStrategy) GetStatistics() *StrategyStatistics {
	stats := s.stats
	return &stats
}

type failingOrderUpdateStrategy struct {
	routingTestStrategy
	err error
}

func (s *failingOrderUpdateStrategy) OnOrderUpdate(*position.OrderUpdate) error { return s.err }

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

func TestStrategyManagerFeedsOnlyRealizedPerformanceDeltasToDynamicAllocator(t *testing.T) {
	cfg := &config.Config{}
	cfg.Strategies.CapitalAllocation.DynamicAllocation.Enabled = true
	sm := NewStrategyManager(cfg, 1000)
	dca := &dynamicPerformanceTestStrategy{
		routingTestStrategy: routingTestStrategy{name: "dca"},
		stats:               StrategyStatistics{TotalTrades: 2, WinRate: 0.5, TotalPnL: 100},
	}
	grid := &dynamicPerformanceTestStrategy{
		routingTestStrategy: routingTestStrategy{name: "grid"},
		stats:               StrategyStatistics{TotalPnL: 1000}, // grid statistics are unrealized and have no closed trades
	}
	sm.RegisterStrategy("dca", dca, 0.5, 0)
	sm.RegisterStrategy("grid", grid, 0.5, 0)

	sm.syncDynamicPerformance()
	sm.syncDynamicPerformance()
	dcaStats := sm.dynamicAllocator.GetPerformance("dca")
	if dcaStats == nil || dcaStats.TotalTrades != 2 || dcaStats.WinningTrades != 1 || dcaStats.TotalPnL != 100 {
		t.Fatalf("first cumulative sample was duplicated or misread: %+v", dcaStats)
	}
	gridStats := sm.dynamicAllocator.GetPerformance("grid")
	if gridStats == nil || gridStats.TotalTrades != 0 || gridStats.TotalPnL != 0 {
		t.Fatalf("unrealized grid statistics affected allocation: %+v", gridStats)
	}

	dca.stats = StrategyStatistics{TotalTrades: 3, WinRate: 2.0 / 3.0, TotalPnL: 145}
	sm.syncDynamicPerformance()
	dcaStats = sm.dynamicAllocator.GetPerformance("dca")
	if dcaStats.TotalTrades != 3 || dcaStats.WinningTrades != 2 || dcaStats.TotalPnL != 145 {
		t.Fatalf("incremental cumulative sample was not applied exactly once: %+v", dcaStats)
	}
}

type failedOrderUpdateStrategy struct {
	routingTestStrategy
	err error
}

func (s *failedOrderUpdateStrategy) OnOrderUpdate(*position.OrderUpdate) error { return s.err }

type failedStopStrategy struct {
	routingTestStrategy
	err     error
	stopped atomic.Bool
}

func (s *failedStopStrategy) Stop() error {
	s.stopped.Store(true)
	return s.err
}

func TestStrategyManagerStopAllWithErrorReportsCloseFailure(t *testing.T) {
	want := errors.New("spread hedge close is unresolved")
	sm := NewStrategyManager(&config.Config{}, 1000)
	strategy := &failedStopStrategy{routingTestStrategy: routingTestStrategy{name: "funding_perp_spread"}, err: want}
	sm.RegisterStrategy(strategy.Name(), strategy, 1, 0)

	err := sm.StopAllWithError()
	if !errors.Is(err, want) {
		t.Fatalf("StopAllWithError() = %v, want wrapped strategy stop failure", err)
	}
	if !strategy.stopped.Load() {
		t.Fatal("StopAllWithError() did not attempt to stop the failing strategy")
	}
}

func TestApplyOrderUpdateForStrategyIsSynchronousAndReturnsPersistenceErrors(t *testing.T) {
	cfg := &config.Config{}
	cfg.Strategies.Configs = map[string]config.StrategyConfig{"trend": {Enabled: true}}
	sm := NewStrategyManager(cfg, 1000)
	want := errors.New("runtime state persistence failed")
	trend := &failedOrderUpdateStrategy{routingTestStrategy: routingTestStrategy{name: "trend"}, err: want}
	sm.RegisterStrategy("trend", trend, 1, 0)
	if err := sm.ApplyOrderUpdateForStrategy("trend", &position.OrderUpdate{OrderID: 1002, Status: "FILLED"}); !errors.Is(err, want) {
		t.Fatalf("expected synchronous accounting error, got %v", err)
	}
	if got := trend.hit.Load(); got != 0 {
		t.Fatalf("failing implementation should not run embedded success handler, got %d", got)
	}
}

func TestApplyOrderUpdateForStrategyRejectsMissingRoute(t *testing.T) {
	sm := NewStrategyManager(&config.Config{}, 1000)
	err := sm.ApplyOrderUpdateForStrategy("missing", &position.OrderUpdate{OrderID: 1003, Status: "FILLED"})
	if err == nil || !strings.Contains(err.Error(), "unavailable strategy") {
		t.Fatalf("expected missing strategy route error, got %v", err)
	}
}

func TestStrategyManagerReportsOrderUpdateAccountingFailure(t *testing.T) {
	cfg := &config.Config{}
	cfg.Strategies.Configs = map[string]config.StrategyConfig{"trend": {Enabled: true}}
	sm := NewStrategyManager(cfg, 1000)
	wantErr := errors.New("runtime state persistence failed")
	sm.RegisterStrategy("trend", &failingOrderUpdateStrategy{
		routingTestStrategy: routingTestStrategy{name: "trend"}, err: wantErr,
	}, 1, 0)
	got := make(chan error, 1)
	sm.SetOrderUpdateErrorHandler(func(name string, err error) {
		if name != "trend" {
			got <- errors.New("unexpected strategy name")
			return
		}
		got <- err
	})
	sm.OnOrderUpdateForStrategy("trend", &position.OrderUpdate{OrderID: 1002, Status: "FILLED"})
	select {
	case err := <-got:
		if !errors.Is(err, wantErr) {
			t.Fatalf("reported error=%v, want wrapped persistence error", err)
		}
	case <-time.After(time.Second):
		t.Fatal("order update failure was not propagated to runtime handler")
	}
}

func TestStrategyManagerReportsUnavailableOwnedOrderRoute(t *testing.T) {
	cfg := &config.Config{}
	sm := NewStrategyManager(cfg, 1000)
	got := make(chan error, 1)
	sm.SetOrderUpdateErrorHandler(func(name string, err error) { got <- err })
	sm.OnOrderUpdateForStrategy("missing", &position.OrderUpdate{OrderID: 1003, Status: "FILLED"})
	select {
	case err := <-got:
		if err == nil || !strings.Contains(err.Error(), "unavailable strategy") {
			t.Fatalf("unexpected route error: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("unavailable owned route did not trigger reconciliation handler")
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

func TestStrategyManagerStartAllReturnsFailureAndStopsEarlierStrategies(t *testing.T) {
	cfg := &config.Config{}
	cfg.Strategies.Configs = map[string]config.StrategyConfig{
		"a": {Enabled: true},
		"b": {Enabled: true},
	}
	sm := NewStrategyManager(cfg, 1000)
	first := &startOutcomeStrategy{routingTestStrategy: routingTestStrategy{name: "a"}}
	failing := &startOutcomeStrategy{routingTestStrategy: routingTestStrategy{name: "b"}, startErr: errors.New("state snapshot is invalid")}
	sm.RegisterStrategy("a", first, 1, 0)
	sm.RegisterStrategy("b", failing, 1, 0)

	err := sm.StartAll()
	if err == nil || !strings.Contains(err.Error(), "strategy b") {
		t.Fatalf("StartAll error=%v, want failed strategy context", err)
	}
	if !first.stopped {
		t.Fatal("previously started strategy was not stopped after startup failure")
	}
	if !failing.stopped {
		t.Fatal("strategy that returned a start error was not stopped")
	}
}
