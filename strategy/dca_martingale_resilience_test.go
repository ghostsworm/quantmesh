package strategy

import (
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/position"
)

type nilOrderExecutor struct{}

func (e *nilOrderExecutor) PlaceOrder(req *position.OrderRequest) (*position.Order, error) {
	return nil, nil
}

func (e *nilOrderExecutor) BatchPlaceOrders(orders []*position.OrderRequest) ([]*position.Order, bool) {
	return nil, false
}

func (e *nilOrderExecutor) BatchPlaceOrdersWithDetails(orders []*position.OrderRequest) *position.BatchPlaceOrdersResult {
	return &position.BatchPlaceOrdersResult{ReduceOnlyErrors: map[string]bool{}}
}

func (e *nilOrderExecutor) BatchCancelOrders(orderIDs []int64) error {
	return nil
}

func TestDCAEnhancedPauseExpires(t *testing.T) {
	cfg := &config.Config{}
	executor := &hedgeOrderExecutor{}
	strategy := NewDCAEnhancedStrategy("dca", "BTCUSDT", cfg, executor, &hedgeExchange{price: 50000}, map[string]interface{}{
		"trend_filter_enabled": false,
		"cascade_protection":   false,
	})
	if err := strategy.Start(t.Context()); err != nil {
		t.Fatalf("Start() error=%v", err)
	}

	strategy.mu.Lock()
	strategy.isPaused = true
	strategy.pauseUntil = time.Now().Add(-time.Second)
	strategy.mu.Unlock()

	if err := strategy.OnPriceChange(50000); err != nil {
		t.Fatalf("OnPriceChange() error=%v", err)
	}
	if strategy.IsPaused() {
		t.Fatal("暂停到期后应自动恢复")
	}
	if len(executor.orders) != 1 {
		t.Fatalf("恢复后应允许开基础单，实际订单数 %d", len(executor.orders))
	}
}

func TestDCAEnhancedManualPauseWithoutDeadlineStaysPaused(t *testing.T) {
	cfg := &config.Config{}
	executor := &hedgeOrderExecutor{}
	strategy := NewDCAEnhancedStrategy("dca", "BTCUSDT", cfg, executor, &hedgeExchange{price: 50000}, map[string]interface{}{
		"trend_filter_enabled": false,
		"cascade_protection":   false,
	})
	if err := strategy.Start(t.Context()); err != nil {
		t.Fatalf("Start() error=%v", err)
	}

	strategy.mu.Lock()
	strategy.isPaused = true
	strategy.pauseUntil = time.Time{}
	strategy.mu.Unlock()

	if err := strategy.OnPriceChange(50000); err != nil {
		t.Fatalf("OnPriceChange() error=%v", err)
	}
	if !strategy.IsPaused() {
		t.Fatal("无到期时间的暂停应保持暂停")
	}
	if len(executor.orders) != 0 {
		t.Fatalf("手动暂停期间不应下单，实际订单数 %d", len(executor.orders))
	}
}

func TestDCAAndMartingaleSkipNilOrdersWithoutStateMutation(t *testing.T) {
	cfg := &config.Config{}
	ex := &hedgeExchange{price: 50000}

	dca := NewDCAEnhancedStrategy("dca", "BTCUSDT", cfg, &nilOrderExecutor{}, ex, map[string]interface{}{
		"trend_filter_enabled": false,
		"cascade_protection":   false,
	})
	if err := dca.Start(t.Context()); err != nil {
		t.Fatalf("DCA Start() error=%v", err)
	}
	if err := dca.OnPriceChange(50000); err != nil {
		t.Fatalf("DCA OnPriceChange() error=%v", err)
	}
	if len(dca.layers) != 0 || dca.currentLayer != 0 || dca.totalQty != 0 {
		t.Fatalf("nil 订单不应改变 DCA 仓位状态: layers=%d layer=%d qty=%.8f", len(dca.layers), dca.currentLayer, dca.totalQty)
	}

	martin := NewMartingaleStrategy("martin", "BTCUSDT", cfg, &nilOrderExecutor{}, ex, map[string]interface{}{
		"trend_filter": false,
	})
	if err := martin.Start(t.Context()); err != nil {
		t.Fatalf("Martingale Start() error=%v", err)
	}
	if err := martin.OnPriceChange(50000); err != nil {
		t.Fatalf("Martingale OnPriceChange() error=%v", err)
	}
	if len(martin.entries) != 0 || martin.currentLevel != 0 || martin.totalQty != 0 {
		t.Fatalf("nil 订单不应改变马丁仓位状态: entries=%d level=%d qty=%.8f", len(martin.entries), martin.currentLevel, martin.totalQty)
	}
}

func TestDCACloseStateClearsOnlyAfterCloseOrderFilled(t *testing.T) {
	cfg := &config.Config{}
	executor := &hedgeOrderExecutor{}
	strategy := NewDCAEnhancedStrategy("dca", "BTCUSDT", cfg, executor, &hedgeExchange{price: 51000}, map[string]interface{}{
		"trend_filter_enabled": false,
		"cascade_protection":   false,
	})
	if err := strategy.Start(t.Context()); err != nil {
		t.Fatalf("Start() error=%v", err)
	}
	strategy.layers = []*DCALayer{{Index: 0, Price: 50000, Quantity: 1, Cost: 50000, Status: "filled"}}
	strategy.currentLayer = 1
	strategy.updateTotals()

	if err := strategy.closeAllPositions(51000, "止盈"); err != nil {
		t.Fatalf("closeAllPositions() error=%v", err)
	}
	if !strategy.isClosing || strategy.closeOrderID == 0 {
		t.Fatalf("平仓下单后应进入 closing 状态: closing=%v order=%d", strategy.isClosing, strategy.closeOrderID)
	}
	if len(strategy.layers) == 0 || strategy.totalQty == 0 {
		t.Fatal("平仓单未成交前不应清空 DCA 仓位状态")
	}

	if err := strategy.OnPriceChange(52000); err != nil {
		t.Fatalf("OnPriceChange() error=%v", err)
	}
	if len(executor.orders) != 1 {
		t.Fatalf("closing 期间不应重复开仓/平仓，实际订单数 %d", len(executor.orders))
	}

	closeID := strategy.closeOrderID
	if err := strategy.OnOrderUpdate(&position.OrderUpdate{OrderID: closeID, Status: position.OrderStatusCanceled}); err != nil {
		t.Fatalf("OnOrderUpdate(cancel) error=%v", err)
	}
	if strategy.isClosing || len(strategy.layers) == 0 || strategy.totalQty == 0 {
		t.Fatal("平仓单取消后应保留原仓位并退出 closing")
	}

	if err := strategy.closeAllPositions(51000, "止盈"); err != nil {
		t.Fatalf("second closeAllPositions() error=%v", err)
	}
	closeID = strategy.closeOrderID
	if err := strategy.OnOrderUpdate(&position.OrderUpdate{OrderID: closeID, Status: position.OrderStatusFilled}); err != nil {
		t.Fatalf("OnOrderUpdate(fill) error=%v", err)
	}
	if strategy.isClosing || len(strategy.layers) != 0 || strategy.totalQty != 0 || strategy.currentLayer != 0 {
		t.Fatalf("平仓成交后应清空状态: closing=%v layers=%d qty=%.8f layer=%d", strategy.isClosing, len(strategy.layers), strategy.totalQty, strategy.currentLayer)
	}
}

func TestMartingaleCloseStateClearsOnlyAfterCloseOrderFilled(t *testing.T) {
	cfg := &config.Config{}
	executor := &hedgeOrderExecutor{}
	strategy := NewMartingaleStrategy("martin", "BTCUSDT", cfg, executor, &hedgeExchange{price: 51000}, map[string]interface{}{
		"trend_filter": false,
	})
	if err := strategy.Start(t.Context()); err != nil {
		t.Fatalf("Start() error=%v", err)
	}
	strategy.entries = []*MartingaleEntry{{Level: 0, Price: 50000, Quantity: 1, Cost: 50000, Status: "filled"}}
	strategy.currentLevel = 1
	strategy.updateTotals()

	if err := strategy.closeAllPositions(51000, "止盈"); err != nil {
		t.Fatalf("closeAllPositions() error=%v", err)
	}
	if !strategy.isClosing || strategy.closeOrderID == 0 {
		t.Fatalf("平仓下单后应进入 closing 状态: closing=%v order=%d", strategy.isClosing, strategy.closeOrderID)
	}
	if len(strategy.entries) == 0 || strategy.totalQty == 0 {
		t.Fatal("平仓单未成交前不应清空马丁仓位状态")
	}

	if err := strategy.OnPriceChange(52000); err != nil {
		t.Fatalf("OnPriceChange() error=%v", err)
	}
	if len(executor.orders) != 1 {
		t.Fatalf("closing 期间不应重复开仓/平仓，实际订单数 %d", len(executor.orders))
	}

	closeID := strategy.closeOrderID
	if err := strategy.OnOrderUpdate(&position.OrderUpdate{OrderID: closeID, Status: position.OrderStatusCanceled}); err != nil {
		t.Fatalf("OnOrderUpdate(cancel) error=%v", err)
	}
	if strategy.isClosing || len(strategy.entries) == 0 || strategy.totalQty == 0 {
		t.Fatal("平仓单取消后应保留原仓位并退出 closing")
	}

	if err := strategy.closeAllPositions(51000, "止盈"); err != nil {
		t.Fatalf("second closeAllPositions() error=%v", err)
	}
	closeID = strategy.closeOrderID
	if err := strategy.OnOrderUpdate(&position.OrderUpdate{OrderID: closeID, Status: position.OrderStatusFilled}); err != nil {
		t.Fatalf("OnOrderUpdate(fill) error=%v", err)
	}
	if strategy.isClosing || len(strategy.entries) != 0 || strategy.totalQty != 0 || strategy.currentLevel != 0 {
		t.Fatalf("平仓成交后应清空状态: closing=%v entries=%d qty=%.8f level=%d", strategy.isClosing, len(strategy.entries), strategy.totalQty, strategy.currentLevel)
	}
}

func TestStopLossCloseOrdersAreNotPostOnly(t *testing.T) {
	cfg := &config.Config{}

	dcaExecutor := &hedgeOrderExecutor{}
	dca := NewDCAEnhancedStrategy("dca", "BTCUSDT", cfg, dcaExecutor, &hedgeExchange{price: 49000}, map[string]interface{}{
		"trend_filter_enabled": false,
		"cascade_protection":   false,
	})
	if err := dca.Start(t.Context()); err != nil {
		t.Fatalf("DCA Start() error=%v", err)
	}
	dca.layers = []*DCALayer{{Index: 0, Price: 50000, Quantity: 1, Cost: 50000, Status: "filled"}}
	dca.currentLayer = 1
	dca.updateTotals()
	if err := dca.closeAllPositions(49000, "止损"); err != nil {
		t.Fatalf("DCA closeAllPositions() error=%v", err)
	}
	if len(dcaExecutor.orders) != 1 {
		t.Fatalf("DCA 应下发止损平仓单，实际订单数 %d", len(dcaExecutor.orders))
	}
	if dcaExecutor.orders[0].PostOnly || !dcaExecutor.orders[0].ReduceOnly || dcaExecutor.orders[0].OrderSource != "stop_loss" {
		t.Fatalf("DCA 止损单应为非 PostOnly 的 reduce-only: postOnly=%v reduceOnly=%v source=%s",
			dcaExecutor.orders[0].PostOnly, dcaExecutor.orders[0].ReduceOnly, dcaExecutor.orders[0].OrderSource)
	}

	martinExecutor := &hedgeOrderExecutor{}
	martin := NewMartingaleStrategy("martin", "BTCUSDT", cfg, martinExecutor, &hedgeExchange{price: 49000}, map[string]interface{}{
		"trend_filter": false,
	})
	if err := martin.Start(t.Context()); err != nil {
		t.Fatalf("Martingale Start() error=%v", err)
	}
	martin.entries = []*MartingaleEntry{{Level: 0, Price: 50000, Quantity: 1, Cost: 50000, Status: "filled"}}
	martin.currentLevel = 1
	martin.updateTotals()
	if err := martin.closeAllPositions(49000, "止损"); err != nil {
		t.Fatalf("Martingale closeAllPositions() error=%v", err)
	}
	if len(martinExecutor.orders) != 1 {
		t.Fatalf("Martingale 应下发止损平仓单，实际订单数 %d", len(martinExecutor.orders))
	}
	if martinExecutor.orders[0].PostOnly || !martinExecutor.orders[0].ReduceOnly || martinExecutor.orders[0].OrderSource != "stop_loss" {
		t.Fatalf("Martingale 止损单应为非 PostOnly 的 reduce-only: postOnly=%v reduceOnly=%v source=%s",
			martinExecutor.orders[0].PostOnly, martinExecutor.orders[0].ReduceOnly, martinExecutor.orders[0].OrderSource)
	}
}
