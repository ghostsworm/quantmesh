package strategy

import (
	"context"
	"math"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/indicators"
	"quantmesh/position"
)

const r3FloatTolerance = 1e-9

func r3AlmostEqual(a, b float64) bool {
	return math.Abs(a-b) < r3FloatTolerance
}

type cancelRecordingExecutor struct {
	hedgeOrderExecutor
	canceled []int64
}

func (e *cancelRecordingExecutor) BatchCancelOrders(orderIDs []int64) error {
	e.canceled = append(e.canceled, orderIDs...)
	return nil
}

func newR3DCA(t *testing.T, executor position.OrderExecutorInterface, extra map[string]interface{}) *DCAEnhancedStrategy {
	t.Helper()
	params := map[string]interface{}{
		"trend_filter_enabled": false,
		"cascade_protection":   false,
	}
	for k, v := range extra {
		params[k] = v
	}
	s := NewDCAEnhancedStrategy("dca", "BTCUSDT", &config.Config{}, executor, &hedgeExchange{price: 100}, params)
	if err := s.Start(t.Context()); err != nil {
		t.Fatalf("Start() error=%v", err)
	}
	return s
}

// S1：瀑布保护暂停期间止损仍需执行
func TestDCAStopLossRunsWhileCascadePaused(t *testing.T) {
	executor := &hedgeOrderExecutor{}
	s := newR3DCA(t, executor, map[string]interface{}{"cascade_protection": true})
	s.layers = []*DCALayer{{Index: 0, Price: 100, Quantity: 1, Cost: 100, Status: entryStatusFilled}}
	s.currentLayer = 1
	s.updateTotals()
	s.isPaused = true
	s.pauseUntil = time.Now().Add(time.Hour)

	if err := s.OnPriceChange(85); err != nil {
		t.Fatalf("OnPriceChange() error=%v", err)
	}
	if len(executor.orders) != 1 {
		t.Fatalf("暂停期间亏损超过止损应下平仓单，实际订单数 %d", len(executor.orders))
	}
	req := executor.orders[0]
	if req.Side != "SELL" || !req.ReduceOnly || req.OrderSource != "stop_loss" {
		t.Fatalf("应为止损平仓单: %+v", req)
	}
	if !s.IsPaused() {
		t.Fatal("暂停状态不应被止损检查清除")
	}
}

// S1：暂停期间不开新倉
func TestDCACascadePauseBlocksOpeningOnly(t *testing.T) {
	executor := &hedgeOrderExecutor{}
	s := newR3DCA(t, executor, nil)
	s.isPaused = true
	s.pauseUntil = time.Now().Add(time.Hour)
	if err := s.OnPriceChange(100); err != nil {
		t.Fatalf("OnPriceChange() error=%v", err)
	}
	if len(executor.orders) != 0 {
		t.Fatalf("暂停期间不应开倉，实际订单数 %d", len(executor.orders))
	}
}

// S2：尾單止盈只平最后一层
func TestDCATailTakeProfitClosesOnlyLastLayer(t *testing.T) {
	executor := &hedgeOrderExecutor{}
	s := newR3DCA(t, executor, nil)
	first := &DCALayer{Index: 0, Price: 100, Quantity: 1, Cost: 100, Status: entryStatusFilled}
	last := &DCALayer{Index: 1, Price: 90, Quantity: 1, Cost: 90, Status: entryStatusFilled}
	s.layers = []*DCALayer{first, last}
	s.currentLayer = 2
	s.updateTotals()

	// 尾單 +0.56%，整体 -4.7%：只应平尾单
	if err := s.OnPriceChange(90.5); err != nil {
		t.Fatalf("OnPriceChange() error=%v", err)
	}
	if len(executor.orders) != 1 {
		t.Fatalf("应下 1 张尾单止盈单，实际 %d", len(executor.orders))
	}
	req := executor.orders[0]
	if req.Side != "SELL" || !req.ReduceOnly || !r3AlmostEqual(req.Quantity, 1) || req.PositionSide != position.PositionSideLong {
		t.Fatalf("尾单止盈单数量应为最后一层数量 1: %+v", req)
	}
	if !s.isClosing || s.closeLayer != last {
		t.Fatal("应进入只平尾层的 closing 状态")
	}

	if err := s.OnOrderUpdate(&position.OrderUpdate{OrderID: s.closeOrderID, Status: position.OrderStatusFilled, ExecutedQty: 1, AvgPrice: 90.5}); err != nil {
		t.Fatalf("OnOrderUpdate() error=%v", err)
	}
	if s.isClosing || len(s.layers) != 1 || s.layers[0] != first || s.currentLayer != 1 {
		t.Fatalf("成交后应只移除尾层: closing=%v layers=%d currentLayer=%d", s.isClosing, len(s.layers), s.currentLayer)
	}
	if !r3AlmostEqual(s.totalQty, 1) || !r3AlmostEqual(s.totalCost, 100) || !r3AlmostEqual(s.avgEntryPrice, 100) {
		t.Fatalf("总计应只剩首层: qty=%.6f cost=%.2f avg=%.2f", s.totalQty, s.totalCost, s.avgEntryPrice)
	}
}

// S3：DCA 限价单挂单后为 pending，成交回报后按实际数量计入；未成交撤单回滚
func TestDCAEntryPendingUntilFilledAndRollbackOnCancel(t *testing.T) {
	executor := &hedgeOrderExecutor{}
	s := newR3DCA(t, executor, nil)

	if err := s.OnPriceChange(100); err != nil {
		t.Fatalf("OnPriceChange() error=%v", err)
	}
	if len(executor.orders) != 1 || executor.orders[0].PositionSide != position.PositionSideLong {
		t.Fatalf("应下 1 张带 LONG 持仓腿的基础单: %+v", executor.orders)
	}
	if len(s.layers) != 1 || s.layers[0].Status != entryStatusPending || s.totalQty != 0 {
		t.Fatalf("挂单后应为 pending 且不计入持仓: status=%s qty=%.6f", s.layers[0].Status, s.totalQty)
	}
	if err := s.OnPriceChange(50); err != nil {
		t.Fatalf("OnPriceChange() error=%v", err)
	}
	if len(executor.orders) != 1 {
		t.Fatalf("存在 pending 开仓单时不应再下单，实际 %d", len(executor.orders))
	}

	orderID := s.layers[0].OrderID
	if err := s.OnOrderUpdate(&position.OrderUpdate{OrderID: orderID, Status: position.OrderStatusPartiallyFilled, ExecutedQty: 0.4, AvgPrice: 99}); err != nil {
		t.Fatalf("OnOrderUpdate(partial) error=%v", err)
	}
	if s.layers[0].Status != entryStatusPartiallyFilled || !r3AlmostEqual(s.totalQty, 0.4) || !r3AlmostEqual(s.totalCost, 39.6) {
		t.Fatalf("部分成交应按实际数量计入: status=%s qty=%.6f cost=%.4f", s.layers[0].Status, s.totalQty, s.totalCost)
	}
	if err := s.OnOrderUpdate(&position.OrderUpdate{OrderID: orderID, Status: position.OrderStatusFilled, ExecutedQty: 1, AvgPrice: 99.5}); err != nil {
		t.Fatalf("OnOrderUpdate(fill) error=%v", err)
	}
	if s.layers[0].Status != entryStatusFilled || !r3AlmostEqual(s.totalQty, 1) || !r3AlmostEqual(s.avgEntryPrice, 99.5) {
		t.Fatalf("全部成交应按实际均价计入: qty=%.6f avg=%.4f", s.totalQty, s.avgEntryPrice)
	}

	// 未成交即撤单：回滚
	executor2 := &hedgeOrderExecutor{}
	s2 := newR3DCA(t, executor2, nil)
	if err := s2.OnPriceChange(100); err != nil {
		t.Fatalf("OnPriceChange() error=%v", err)
	}
	if err := s2.OnOrderUpdate(&position.OrderUpdate{OrderID: s2.layers[0].OrderID, Status: "EXPIRED"}); err != nil {
		t.Fatalf("OnOrderUpdate(expired) error=%v", err)
	}
	if len(s2.layers) != 0 || s2.currentLayer != 0 || s2.totalQty != 0 {
		t.Fatalf("未成交过期应回滚: layers=%d layer=%d qty=%.6f", len(s2.layers), s2.currentLayer, s2.totalQty)
	}
	if err := s2.OnPriceChange(100); err != nil {
		t.Fatalf("OnPriceChange() error=%v", err)
	}
	if len(executor2.orders) != 2 {
		t.Fatalf("回滚后应允许重新开基础单，实际订单数 %d", len(executor2.orders))
	}
}

// S3：平仓单被拒/过期必须退出 closing；部分成交后被撤按比例缩减
func TestDCACloseOrderRejectedOrExpiredReleasesClosing(t *testing.T) {
	for _, status := range []string{"REJECTED", "EXPIRED", "CANCELLED"} {
		executor := &hedgeOrderExecutor{}
		s := newR3DCA(t, executor, nil)
		s.layers = []*DCALayer{{Index: 0, Price: 100, Quantity: 2, Cost: 200, Status: entryStatusFilled}}
		s.currentLayer = 1
		s.updateTotals()
		if err := s.closeAllPositions(90, "止损"); err != nil {
			t.Fatalf("closeAllPositions() error=%v", err)
		}
		if err := s.OnOrderUpdate(&position.OrderUpdate{OrderID: s.closeOrderID, Status: status, ExecutedQty: 0.5}); err != nil {
			t.Fatalf("OnOrderUpdate(%s) error=%v", status, err)
		}
		if s.isClosing || s.closeOrderID != 0 {
			t.Fatalf("%s 后应退出 closing", status)
		}
		if !r3AlmostEqual(s.totalQty, 1.5) || !r3AlmostEqual(s.totalCost, 150) {
			t.Fatalf("%s 部分成交后应按比例缩减: qty=%.6f cost=%.4f", status, s.totalQty, s.totalCost)
		}
	}
}

// S3 + R2 遗留：做空马丁开仓单带 SHORT 持仓腿；pending/回滚/平仓拒单
func TestShortMartingaleEntryLifecycle(t *testing.T) {
	executor := &cancelRecordingExecutor{}
	s := NewMartingaleStrategy("short_martingale", "BTCUSDT", &config.Config{}, executor, &hedgeExchange{price: 50000}, map[string]interface{}{
		"trend_filter": false,
		"direction":    "short",
	})
	if err := s.Start(t.Context()); err != nil {
		t.Fatalf("Start() error=%v", err)
	}

	if err := s.OnPriceChange(50000); err != nil {
		t.Fatalf("OnPriceChange() error=%v", err)
	}
	if len(executor.orders) != 1 {
		t.Fatalf("应下初始单，实际 %d", len(executor.orders))
	}
	open := executor.orders[0]
	if open.Side != "SELL" || open.PositionSide != position.PositionSideShort || open.ReduceOnly {
		t.Fatalf("做空开仓单应为 SELL + PositionSide=SHORT: %+v", open)
	}
	if s.totalQty != 0 || s.entries[0].Status != entryStatusPending {
		t.Fatal("下单后不应计入持仓")
	}

	if err := s.OnOrderUpdate(&position.OrderUpdate{OrderID: s.entries[0].OrderID, Status: "REJECTED"}); err != nil {
		t.Fatalf("OnOrderUpdate(rejected) error=%v", err)
	}
	if len(s.entries) != 0 || s.currentLevel != 0 {
		t.Fatalf("拒单应回滚: entries=%d level=%d", len(s.entries), s.currentLevel)
	}

	if err := s.OnPriceChange(50000); err != nil {
		t.Fatalf("OnPriceChange() error=%v", err)
	}
	entryID := s.entries[0].OrderID
	if err := s.OnOrderUpdate(&position.OrderUpdate{OrderID: entryID, Status: position.OrderStatusPartiallyFilled, ExecutedQty: 0.001, AvgPrice: 50010}); err != nil {
		t.Fatalf("OnOrderUpdate(partial) error=%v", err)
	}
	if !r3AlmostEqual(s.totalQty, 0.001) || !r3AlmostEqual(s.avgEntryPrice, 50010) {
		t.Fatalf("部分成交应计入: qty=%.6f avg=%.2f", s.totalQty, s.avgEntryPrice)
	}

	if err := s.closeAllPositions(49000, "止盈"); err != nil {
		t.Fatalf("closeAllPositions() error=%v", err)
	}
	if len(executor.canceled) != 1 || executor.canceled[0] != entryID {
		t.Fatalf("平仓前应撤掉未完全成交的开仓单: %v", executor.canceled)
	}
	closeReq := executor.orders[len(executor.orders)-1]
	if closeReq.Side != "BUY" || !closeReq.ReduceOnly || closeReq.PositionSide != position.PositionSideShort {
		t.Fatalf("做空平仓单应为 BUY + ReduceOnly + SHORT: %+v", closeReq)
	}
	if err := s.OnOrderUpdate(&position.OrderUpdate{OrderID: s.closeOrderID, Status: "EXPIRED"}); err != nil {
		t.Fatalf("OnOrderUpdate(expired) error=%v", err)
	}
	if s.isClosing {
		t.Fatal("平仓单过期后不应永久卡在 closing")
	}
}

// 马丁精度暂停时止损仍执行
func TestMartingalePausedStillRunsStopLoss(t *testing.T) {
	executor := &hedgeOrderExecutor{}
	s := NewMartingaleStrategy("martin", "BTCUSDT", &config.Config{}, executor, &hedgeExchange{price: 50000}, map[string]interface{}{
		"trend_filter": false,
	})
	if err := s.Start(t.Context()); err != nil {
		t.Fatalf("Start() error=%v", err)
	}
	s.entries = []*MartingaleEntry{{Level: 0, Price: 100, Quantity: 1, Cost: 100, Status: entryStatusFilled}}
	s.currentLevel = 1
	s.updateTotals()
	s.isPaused = true
	if err := s.OnPriceChange(80); err != nil {
		t.Fatalf("OnPriceChange() error=%v", err)
	}
	if len(executor.orders) != 1 || executor.orders[0].OrderSource != "stop_loss" {
		t.Fatalf("暂停时仍应止损: %+v", executor.orders)
	}
}

// S11：GetStatistics 返回副本
func TestDCAAndMartingaleStatisticsAreCopies(t *testing.T) {
	dca := newR3DCA(t, &hedgeOrderExecutor{}, nil)
	dca.stats.TotalTrades = 3
	got := dca.GetStatistics()
	got.TotalTrades = 99
	if dca.stats.TotalTrades != 3 {
		t.Fatal("DCA GetStatistics 不应返回内部指针")
	}

	martin := NewMartingaleStrategy("martin", "BTCUSDT", &config.Config{}, &hedgeOrderExecutor{}, &hedgeExchange{price: 1}, nil)
	martin.stats.TotalPnL = 5
	gotM := martin.GetStatistics()
	gotM.TotalPnL = -1
	if martin.stats.TotalPnL != 5 {
		t.Fatal("马丁 GetStatistics 不应返回内部指针")
	}
}

type riskOnlyFakeSubStrategy struct {
	fakeComboSubStrategy
	riskOnlyPrices []float64
}

func (f *riskOnlyFakeSubStrategy) OnPriceChangeRiskOnly(price float64) error {
	f.riskOnlyPrices = append(f.riskOnlyPrices, price)
	return nil
}

func newR3Combo(ctx context.Context, cancel context.CancelFunc, cfg *ComboConfig, subs ...Strategy) *ComboStrategy {
	names := make([]string, 0, len(subs))
	weights := make([]float64, 0, len(subs))
	for _, sub := range subs {
		names = append(names, sub.Name())
		weights = append(weights, 1)
	}
	return &ComboStrategy{
		name:          "combo",
		strategies:    subs,
		strategyNames: names,
		weights:       weights,
		marketState:   MarketSideways,
		priceHistory:  make([]float64, 0, 200),
		candles:       make([]indicators.Candle, 0, 200),
		ctx:           ctx,
		cancel:        cancel,
		strategyCfg:   cfg,
		stats:         &StrategyStatistics{},
	}
}

// S4：市况门控只禁止开仓，止盈止损仍通过 risk-only 通道执行
func TestComboMarketGatingStillRunsRiskChecks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	gated := &riskOnlyFakeSubStrategy{fakeComboSubStrategy: fakeComboSubStrategy{name: "gated", stats: &StrategyStatistics{}}}
	flatNoHandler := &fakeComboSubStrategy{name: "flat", stats: &StrategyStatistics{}}
	combo := newR3Combo(ctx, cancel, &ComboConfig{
		Strategies: []StrategyConfig{
			{Name: "gated", PreferredMarket: []MarketState{MarketBullish}},
			{Name: "flat", PreferredMarket: []MarketState{MarketBullish}},
		},
	}, gated, flatNoHandler)

	if err := combo.OnPriceChange(100); err != nil {
		t.Fatalf("OnPriceChange() error=%v", err)
	}
	if len(gated.prices) != 0 || len(gated.riskOnlyPrices) != 1 {
		t.Fatalf("门控子策略应只走 risk-only: full=%v riskOnly=%v", gated.prices, gated.riskOnlyPrices)
	}
	if len(flatNoHandler.prices) != 0 {
		t.Fatal("无持仓且不支持 risk-only 的子策略被门控时不应收到价格")
	}
}

// MaxExposure / MaxDrawdown：超限只禁止开新仓
func TestComboExposureAndDrawdownLimitsBlockOpening(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sub := &riskOnlyFakeSubStrategy{fakeComboSubStrategy: fakeComboSubStrategy{
		name:      "sub",
		stats:     &StrategyStatistics{},
		positions: []*Position{{Symbol: "BTCUSDT", Size: 1, CurrentPrice: 600}},
	}}
	combo := newR3Combo(ctx, cancel, &ComboConfig{TotalCapital: 1000, MaxExposure: 0.5, Strategies: []StrategyConfig{{Name: "sub"}}}, sub)
	if err := combo.OnPriceChange(600); err != nil {
		t.Fatalf("OnPriceChange() error=%v", err)
	}
	if len(sub.prices) != 0 || len(sub.riskOnlyPrices) != 1 {
		t.Fatalf("敞口 0.6 > 0.5 应禁止开仓: full=%v riskOnly=%v", sub.prices, sub.riskOnlyPrices)
	}
	sub.positions[0].Size = 0.1
	if err := combo.OnPriceChange(600); err != nil {
		t.Fatalf("OnPriceChange() error=%v", err)
	}
	if len(sub.prices) != 1 {
		t.Fatal("敞口回落后应恢复正常转发")
	}

	ddSub := &riskOnlyFakeSubStrategy{fakeComboSubStrategy: fakeComboSubStrategy{
		name:      "dd",
		stats:     &StrategyStatistics{},
		positions: []*Position{{Symbol: "BTCUSDT", Size: 0.01, CurrentPrice: 100, PnL: 0}},
	}}
	ddCombo := newR3Combo(ctx, cancel, &ComboConfig{TotalCapital: 1000, MaxDrawdown: 5, Strategies: []StrategyConfig{{Name: "dd"}}}, ddSub)
	if err := ddCombo.OnPriceChange(100); err != nil {
		t.Fatalf("OnPriceChange() error=%v", err)
	}
	ddSub.positions[0].PnL = -60
	if err := ddCombo.OnPriceChange(100); err != nil {
		t.Fatalf("OnPriceChange() error=%v", err)
	}
	if len(ddSub.prices) != 1 || len(ddSub.riskOnlyPrices) != 1 {
		t.Fatalf("回撤 6%% 超过 5%% 后应只走 risk-only: full=%v riskOnly=%v", ddSub.prices, ddSub.riskOnlyPrices)
	}
}

// GetInfo 不再递归持有读锁
func TestComboGetInfoWithConcurrentWriter(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sub := &fakeComboSubStrategy{name: "sub", stats: &StrategyStatistics{}, positions: []*Position{{Symbol: "BTCUSDT"}}}
	combo := newR3Combo(ctx, cancel, &ComboConfig{}, sub)

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 200; i++ {
			_ = combo.GetInfo()
		}
	}()
	for i := 0; i < 200; i++ {
		combo.mu.Lock()
		combo.marketState = MarketBullish
		combo.mu.Unlock()
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("GetInfo 与写锁并发时发生死锁")
	}
}
