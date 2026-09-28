package strategy

import (
	"testing"

	"quantmesh/config"
	"quantmesh/position"
)

type filledAckExecutor struct{ *hedgeOrderExecutor }

func (e *filledAckExecutor) PlaceOrder(req *position.OrderRequest) (*position.Order, error) {
	e.orders = append(e.orders, req)
	return &position.Order{OrderID: int64(len(e.orders)), Side: req.Side, Quantity: req.Quantity, Price: req.Price, Status: "FILLED"}, nil
}

func TestDCAPlacementAckCannotSettleCloseWithoutActualFillAndFee(t *testing.T) {
	executor := &filledAckExecutor{hedgeOrderExecutor: &hedgeOrderExecutor{}}
	strategy := NewDCAEnhancedStrategy("dca", "BTCUSDT", &config.Config{}, executor, &hedgeExchange{price: 110}, nil)
	setTestRuntimeStateStore(t, strategy)
	strategy.layers = []*DCALayer{{Index: 0, Price: 100, Quantity: 1, Cost: 100, Status: entryStatusFilled}}
	strategy.totalQty, strategy.totalCost, strategy.avgEntryPrice = 1, 100, 100

	if err := strategy.closeAllPositions(110, "take profit"); err != nil {
		t.Fatalf("closeAllPositions() error = %v", err)
	}
	if !strategy.isClosing || strategy.closeProgress.Quantity != 0 || strategy.totalQty != 1 {
		t.Fatalf("placement FILLED label must wait for authoritative fill update: closing=%v progress=%+v qty=%v", strategy.isClosing, strategy.closeProgress, strategy.totalQty)
	}
	if len(executor.orders) != 1 || executor.orders[0].Quantity != 1 {
		t.Fatalf("unexpected close order: %+v", executor.orders)
	}
}

func TestDCACommissionMustBeConvertedToQuoteBeforeAccounting(t *testing.T) {
	strategy := NewDCAEnhancedStrategy("dca", "BTCUSDT", &config.Config{}, &hedgeOrderExecutor{}, &hedgeExchange{}, nil)
	if got, ok := strategy.commissionInQuote(0.25, "USDT", 100); !ok || got != 0.25 {
		t.Fatalf("quote commission conversion = %v, %v", got, ok)
	}
	if got, ok := strategy.commissionInQuote(0.001, "BTC", 100); !ok || got != 0.1 {
		t.Fatalf("base commission conversion = %v, %v", got, ok)
	}
	if got, ok := strategy.commissionInQuote(0.01, "BNB", 100); ok || got != 0 {
		t.Fatalf("unsupported third-asset commission must remain unvalued: %v, %v", got, ok)
	}
	if got, ok := strategy.commissionInQuote(-0.25, "USDT", 100); !ok || got != -0.25 {
		t.Fatalf("quote-asset commission rebate must remain negative: %v, %v", got, ok)
	}
	if got, ok := strategy.commissionInQuote(-0.001, "BTC", 100); !ok || got != -0.1 {
		t.Fatalf("base-asset commission rebate must retain signed quote value: %v, %v", got, ok)
	}
}

func TestDCAUnvaluedOpenFeeDoesNotConsumeFillProgress(t *testing.T) {
	strategy := NewDCAEnhancedStrategy("dca", "BTCUSDT", &config.Config{}, &hedgeOrderExecutor{}, &hedgeExchange{}, nil)
	layer := &DCALayer{OrderID: 7, Quantity: 1, Status: entryStatusPending}
	strategy.layers = []*DCALayer{layer}
	strategy.handleLayerOrderUpdate(layer, &position.OrderUpdate{
		OrderID: 7, Status: "PARTIALLY_FILLED", ExecutedQty: 0.5,
		AvgPrice: 100, Commission: 0.01, CommissionAsset: "BNB",
	})
	if layer.FillProgress.Quantity != 0 || layer.Quantity != 1 || strategy.totalQty != 0 {
		t.Fatalf("unvalued fee consumed fill or changed inventory: layer=%+v total=%v", layer, strategy.totalQty)
	}
}
