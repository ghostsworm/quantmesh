package strategy

import (
	"math"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/position"
)

type dcaReconciliationExecutor struct {
	*hedgeOrderExecutor
	marked int
	reason string
}

func (e *dcaReconciliationExecutor) MarkOrderReconciliationRequired(_ int64, _ string, reason string) error {
	e.marked++
	e.reason = reason
	return nil
}

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

func TestDCAOverfilledCloseRetainsInventoryAndRequiresReconciliation(t *testing.T) {
	executor := &dcaReconciliationExecutor{hedgeOrderExecutor: &hedgeOrderExecutor{}}
	strategy := NewDCAEnhancedStrategy("dca", "BTCUSDT", &config.Config{}, executor, &hedgeExchange{price: 110}, nil)
	setTestRuntimeStateStore(t, strategy)
	strategy.layers = []*DCALayer{{Index: 0, Price: 100, Quantity: 1, Cost: 100, Status: entryStatusFilled}}
	strategy.totalQty, strategy.totalCost, strategy.avgEntryPrice = 1, 100, 100
	strategy.SetTradeStorage(&dcaFillRecorder{})
	if err := strategy.closeAllPositions(110, "take profit"); err != nil {
		t.Fatal(err)
	}
	orderID := strategy.closeOrderID
	if err := strategy.OnOrderUpdate(&position.OrderUpdate{
		OrderID: orderID, Status: "PARTIALLY_FILLED", ExecutedQty: 1.1, AvgPrice: 110, CommissionAsset: "USDT",
	}); err != nil {
		t.Fatal(err)
	}
	if executor.marked != 1 || executor.reason == "" {
		t.Fatalf("overfill must request executor-level reconciliation: %+v", executor)
	}
	if !strategy.isClosing || strategy.closeOrderID != orderID || strategy.closeProgress.Quantity != 0 || strategy.totalQty != 1 {
		t.Fatalf("overfill must preserve close intent and attributed inventory: closing=%v order=%d progress=%+v qty=%v",
			strategy.isClosing, strategy.closeOrderID, strategy.closeProgress, strategy.totalQty)
	}
	if got := strategy.GetStatistics().TotalTrades; got != 0 {
		t.Fatalf("unreconciled overfill must not affect trade statistics, got %d trades", got)
	}
}

func TestDCAMalformedTerminalCloseFillCannotClearIntent(t *testing.T) {
	tests := []struct {
		name  string
		qty   float64
		price float64
	}{
		{name: "NaN quantity", qty: math.NaN(), price: 110},
		{name: "infinite quantity", qty: math.Inf(1), price: 110},
		{name: "NaN average price", qty: 1, price: math.NaN()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executor := &dcaReconciliationExecutor{hedgeOrderExecutor: &hedgeOrderExecutor{}}
			strategy := NewDCAEnhancedStrategy("dca", "BTCUSDT", &config.Config{}, executor, &hedgeExchange{price: 110}, nil)
			setTestRuntimeStateStore(t, strategy)
			strategy.layers = []*DCALayer{{Index: 0, Price: 100, Quantity: 1, Cost: 100, Status: entryStatusFilled}}
			strategy.totalQty, strategy.totalCost, strategy.avgEntryPrice = 1, 100, 100
			strategy.SetTradeStorage(&dcaFillRecorder{})
			if err := strategy.closeAllPositions(110, "take profit"); err != nil {
				t.Fatal(err)
			}
			orderID := strategy.closeOrderID
			if err := strategy.OnOrderUpdate(&position.OrderUpdate{
				OrderID: orderID, Status: "FILLED", ExecutedQty: tt.qty, AvgPrice: tt.price, CommissionAsset: "USDT",
			}); err != nil {
				t.Fatal(err)
			}
			if executor.marked != 1 || !strategy.isClosing || strategy.closeOrderID != orderID || strategy.totalQty != 1 {
				t.Fatalf("malformed terminal fill must preserve intent and inventory: marked=%d closing=%v order=%d qty=%v",
					executor.marked, strategy.isClosing, strategy.closeOrderID, strategy.totalQty)
			}
		})
	}
}

func TestDCACloseWaitsForPendingEntryCancellationTerminal(t *testing.T) {
	executor := &cancelRecordingExecutor{}
	strategy := NewDCAEnhancedStrategy("dca", "BTCUSDT", &config.Config{}, executor, &hedgeExchange{price: 110}, nil)
	setTestRuntimeStateStore(t, strategy)
	filled := &DCALayer{Index: 0, Price: 100, Quantity: 1, Cost: 100, Status: entryStatusFilled}
	pending := &DCALayer{Index: 1, Price: 90, Quantity: 1, Cost: 90, OrderID: 77, Status: entryStatusPartiallyFilled}
	strategy.layers = []*DCALayer{filled, pending}
	strategy.updateTotals()

	if err := strategy.closeAllPositions(110, "stop loss"); err != nil {
		t.Fatal(err)
	}
	if len(executor.canceled) != 1 || executor.canceled[0] != 77 || len(executor.orders) != 0 {
		t.Fatalf("cancel ACK must not permit close submission: canceled=%v orders=%+v", executor.canceled, executor.orders)
	}
	if err := strategy.closeAllPositions(110, "stop loss"); err != nil {
		t.Fatal(err)
	}
	if len(executor.canceled) != 1 {
		t.Fatalf("cancel retry must be throttled while awaiting terminal update: %v", executor.canceled)
	}
	pending.CancelRequestedAt = time.Now().Add(-dcaPendingCancelRetryInterval)
	if err := strategy.closeAllPositions(110, "stop loss"); err != nil {
		t.Fatal(err)
	}
	if len(executor.canceled) != 2 || len(executor.orders) != 0 {
		t.Fatalf("unconfirmed cancellation should retry but continue blocking close: canceled=%v orders=%+v", executor.canceled, executor.orders)
	}
	if err := strategy.OnOrderUpdate(&position.OrderUpdate{OrderID: 77, Status: "CANCELED"}); err != nil {
		t.Fatal(err)
	}
	if err := strategy.closeAllPositions(110, "stop loss"); err != nil {
		t.Fatal(err)
	}
	if len(executor.orders) != 1 || !executor.orders[0].ReduceOnly || executor.orders[0].Quantity != 1 {
		t.Fatalf("after terminal cancel, close should use reconciled filled inventory only: %+v", executor.orders)
	}
}

func TestDCATailCloseWaitsForPendingEntryCancellationTerminal(t *testing.T) {
	executor := &cancelRecordingExecutor{}
	strategy := NewDCAEnhancedStrategy("dca", "BTCUSDT", &config.Config{}, executor, &hedgeExchange{price: 110}, nil)
	setTestRuntimeStateStore(t, strategy)
	last := &DCALayer{Index: 2, Price: 100, Quantity: 0.5, Cost: 50, Status: entryStatusFilled}
	pending := &DCALayer{Index: 3, Price: 90, Quantity: 0.5, Cost: 45, OrderID: 78, Status: entryStatusPending}
	strategy.layers = []*DCALayer{last, pending}
	strategy.updateTotals()
	if err := strategy.closeLastLayer(last, 110); err != nil {
		t.Fatal(err)
	}
	if len(executor.canceled) != 1 || len(executor.orders) != 0 {
		t.Fatalf("tail close must wait for opening-order terminal status: canceled=%v orders=%+v", executor.canceled, executor.orders)
	}
	if err := strategy.OnOrderUpdate(&position.OrderUpdate{OrderID: 78, Status: "CANCELED"}); err != nil {
		t.Fatal(err)
	}
	if err := strategy.closeLastLayer(last, 110); err != nil {
		t.Fatal(err)
	}
	if len(executor.orders) != 1 || executor.orders[0].Quantity != 0.5 || !executor.orders[0].ReduceOnly {
		t.Fatalf("tail close should submit only after pending entry terminal: %+v", executor.orders)
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
