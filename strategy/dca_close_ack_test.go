package strategy

import (
	"context"
	"encoding/json"
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
	strategy := NewDCAEnhancedStrategy("dca", "BTCUSDT", dcaTestConfig(), executor, &hedgeExchange{price: 110}, nil)
	setTestRuntimeStateStore(t, strategy)
	strategy.layers = []*DCALayer{{Index: 0, Price: 100, Quantity: 1, Cost: 100, RequestedQuantity: 1,
		FillProgress: position.FillProgress{Quantity: 1, Notional: 100}, Status: entryStatusFilled}}
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

func TestDCAProfitAndLossTriggersUseFeeAdjustedReturn(t *testing.T) {
	t.Run("take profit waits until estimated net target", func(t *testing.T) {
		executor := &hedgeOrderExecutor{}
		cfg := dcaTestConfig()
		cfg.Exchanges = map[string]config.ExchangeConfig{"mock": {FeeRate: 0.005}}
		strategy := NewDCAEnhancedStrategy("dca", "BTCUSDT", cfg, executor, &hedgeExchange{price: 100}, map[string]interface{}{
			"first_order_take_profit": 0.5, "total_take_profit": 5, "trailing_activation": 5, "stop_loss": 50,
		})
		setTestRuntimeStateStore(t, strategy)
		strategy.layers = []*DCALayer{{Index: 0, Price: 100, Quantity: 1, Cost: 100, OpeningFee: 0.2, Status: entryStatusFilled}}
		strategy.totalQty, strategy.totalCost, strategy.avgEntryPrice = 1, 100, 100

		if err := strategy.checkTakeProfitStopLoss(101); err != nil {
			t.Fatalf("checkTakeProfitStopLoss() error = %v", err)
		}
		if len(executor.orders) != 0 {
			t.Fatal("gross-profit threshold must not trigger while estimated net return is below target")
		}
		if err := strategy.checkTakeProfitStopLoss(101.21); err != nil {
			t.Fatalf("checkTakeProfitStopLoss() at net target error = %v", err)
		}
		if len(executor.orders) != 1 {
			t.Fatalf("expected one close after estimated net target, got %d", len(executor.orders))
		}
	})

	t.Run("stop loss includes already paid opening fee", func(t *testing.T) {
		executor := &hedgeOrderExecutor{}
		cfg := dcaTestConfig()
		cfg.Exchanges = map[string]config.ExchangeConfig{"mock": {FeeRate: 0}}
		strategy := NewDCAEnhancedStrategy("dca", "BTCUSDT", cfg, executor, &hedgeExchange{price: 100}, map[string]interface{}{
			"first_order_take_profit": 10, "total_take_profit": 10, "trailing_activation": 10, "stop_loss": 0.1,
		})
		setTestRuntimeStateStore(t, strategy)
		strategy.layers = []*DCALayer{{Index: 0, Price: 100, Quantity: 1, Cost: 100, OpeningFee: 0.2, Status: entryStatusFilled}}
		strategy.totalQty, strategy.totalCost, strategy.avgEntryPrice = 1, 100, 100

		if err := strategy.checkTakeProfitStopLoss(100); err != nil {
			t.Fatalf("checkTakeProfitStopLoss() error = %v", err)
		}
		if len(executor.orders) != 1 || executor.orders[0].OrderSource != "stop_loss" {
			t.Fatalf("expected fee-adjusted stop loss order, got %+v", executor.orders)
		}
	})
}

func TestDCATrailingTakeProfitStatePersistsAndRestores(t *testing.T) {
	store := &memoryRuntimeStateStore{}
	strategy := NewDCAEnhancedStrategy("dca", "BTCUSDT", dcaTestConfig(), &hedgeOrderExecutor{}, &hedgeExchange{}, map[string]interface{}{
		"first_order_take_profit": 10, "total_take_profit": 10, "trailing_activation": 0.5,
		"trailing_take_profit": 1, "stop_loss": 50,
	})
	strategy.SetRuntimeStateStore(store)
	strategy.layers = []*DCALayer{{Index: 0, Price: 100, Quantity: 1, Cost: 100, RequestedQuantity: 1,
		FillProgress: position.FillProgress{Quantity: 1, Notional: 100}, Status: entryStatusFilled}}
	strategy.totalQty, strategy.totalCost, strategy.avgEntryPrice = 1, 100, 100
	if err := strategy.checkTakeProfitStopLoss(101); err != nil {
		t.Fatalf("checkTakeProfitStopLoss() error = %v", err)
	}
	var persisted dcaRuntimeState
	if err := json.Unmarshal([]byte(store.payload), &persisted); err != nil {
		t.Fatal(err)
	}
	if !persisted.TakeProfitTriggered || persisted.HighestProfit < 0.99 {
		t.Fatalf("trailing activation and high-water mark were not persisted: %+v", persisted)
	}

	restarted := NewDCAEnhancedStrategy("dca", "BTCUSDT", dcaTestConfig(), &hedgeOrderExecutor{}, &hedgeExchange{}, map[string]interface{}{
		"first_order_take_profit": 10, "total_take_profit": 10, "trailing_activation": 0.5,
		"trailing_take_profit": 1, "stop_loss": 50,
	})
	restarted.SetRuntimeStateStore(store)
	if err := restarted.Start(context.Background()); err != nil {
		t.Fatalf("Start() restoring trailing state error = %v", err)
	}
	defer restarted.Stop()
	if !restarted.takeProfitTriggered || restarted.highestProfit < 0.99 {
		t.Fatalf("trailing state lost across restart: active=%v peak=%.4f", restarted.takeProfitTriggered, restarted.highestProfit)
	}
}

func TestDCAOverfilledCloseRetainsInventoryAndRequiresReconciliation(t *testing.T) {
	executor := &dcaReconciliationExecutor{hedgeOrderExecutor: &hedgeOrderExecutor{}}
	strategy := NewDCAEnhancedStrategy("dca", "BTCUSDT", dcaTestConfig(), executor, &hedgeExchange{price: 110}, nil)
	setTestRuntimeStateStore(t, strategy)
	strategy.layers = []*DCALayer{{Index: 0, Price: 100, Quantity: 1, Cost: 100, Status: entryStatusFilled}}
	strategy.totalQty, strategy.totalCost, strategy.avgEntryPrice = 1, 100, 100
	strategy.SetTradeStorage(&dcaFillRecorder{})
	if err := strategy.closeAllPositions(110, "take profit"); err != nil {
		t.Fatal(err)
	}
	orderID := strategy.closeOrderID
	if err := strategy.OnOrderUpdate(&position.OrderUpdate{
		OrderID: orderID, Status: "PARTIALLY_FILLED", ExecutedQty: 1.1, AvgPrice: 110, CommissionAsset: "USDT", CommissionKnown: true,
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
			strategy := NewDCAEnhancedStrategy("dca", "BTCUSDT", dcaTestConfig(), executor, &hedgeExchange{price: 110}, nil)
			setTestRuntimeStateStore(t, strategy)
			strategy.layers = []*DCALayer{{Index: 0, Price: 100, Quantity: 1, Cost: 100, Status: entryStatusFilled}}
			strategy.totalQty, strategy.totalCost, strategy.avgEntryPrice = 1, 100, 100
			strategy.SetTradeStorage(&dcaFillRecorder{})
			if err := strategy.closeAllPositions(110, "take profit"); err != nil {
				t.Fatal(err)
			}
			orderID := strategy.closeOrderID
			if err := strategy.OnOrderUpdate(&position.OrderUpdate{
				OrderID: orderID, Status: "FILLED", ExecutedQty: tt.qty, AvgPrice: tt.price, CommissionAsset: "USDT", CommissionKnown: true,
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

func TestDCATerminalEntryWithRegressedCumulativeNotionalRequiresReconciliation(t *testing.T) {
	executor := &dcaReconciliationExecutor{hedgeOrderExecutor: &hedgeOrderExecutor{}}
	strategy := NewDCAEnhancedStrategy("dca", "BTCUSDT", dcaTestConfig(), executor, &hedgeExchange{price: 100}, nil)
	setTestRuntimeStateStore(t, strategy)
	layer := &DCALayer{Index: 0, Price: 100, Quantity: 1, Cost: 100, RequestedQuantity: 1, OrderID: 91,
		Status: entryStatusPending}
	strategy.layers = []*DCALayer{layer}

	if err := strategy.OnOrderUpdate(&position.OrderUpdate{OrderID: 91, Status: "PARTIALLY_FILLED", ExecutedQty: 0.5,
		AvgPrice: 100, Commission: 0, CommissionAsset: "USDT", CommissionKnown: true}); err != nil {
		t.Fatal(err)
	}
	if err := strategy.OnOrderUpdate(&position.OrderUpdate{OrderID: 91, Status: "FILLED", ExecutedQty: 1,
		AvgPrice: 40, Commission: 0, CommissionAsset: "USDT", CommissionKnown: true}); err != nil {
		t.Fatal(err)
	}
	if executor.marked != 1 || layer.Status != entryStatusPartiallyFilled || layer.FillProgress.Quantity != 0.5 || strategy.totalQty != 0.5 {
		t.Fatalf("regressed cumulative notional must preserve partial inventory and require reconciliation: marks=%d layer=%+v qty=%v",
			executor.marked, layer, strategy.totalQty)
	}
}

func TestDCATerminalCloseWithRegressedCumulativeNotionalRetainsIntent(t *testing.T) {
	executor := &dcaReconciliationExecutor{hedgeOrderExecutor: &hedgeOrderExecutor{}}
	strategy := NewDCAEnhancedStrategy("dca", "BTCUSDT", dcaTestConfig(), executor, &hedgeExchange{price: 110}, nil)
	setTestRuntimeStateStore(t, strategy)
	strategy.layers = []*DCALayer{{Index: 0, Price: 100, Quantity: 1, Cost: 100, Status: entryStatusFilled}}
	strategy.totalQty, strategy.totalCost, strategy.avgEntryPrice = 1, 100, 100
	strategy.SetTradeStorage(&dcaFillRecorder{})
	if err := strategy.closeAllPositions(110, "take profit"); err != nil {
		t.Fatal(err)
	}
	orderID := strategy.closeOrderID
	for _, update := range []*position.OrderUpdate{
		{OrderID: orderID, Status: "PARTIALLY_FILLED", ExecutedQty: 0.5, AvgPrice: 110, CommissionAsset: "USDT", CommissionKnown: true},
		{OrderID: orderID, Status: "FILLED", ExecutedQty: 1, AvgPrice: 50, CommissionAsset: "USDT", CommissionKnown: true},
	} {
		if err := strategy.OnOrderUpdate(update); err != nil {
			t.Fatal(err)
		}
	}
	if executor.marked != 1 || !strategy.isClosing || strategy.closeOrderID != orderID || strategy.closeProgress.Quantity != 0.5 || strategy.totalQty != 0.5 {
		t.Fatalf("regressed cumulative notional must retain close intent and reconciled inventory: marks=%d closing=%v order=%d progress=%+v qty=%v",
			executor.marked, strategy.isClosing, strategy.closeOrderID, strategy.closeProgress, strategy.totalQty)
	}
}

func TestDCACloseWaitsForPendingEntryCancellationTerminal(t *testing.T) {
	executor := &cancelRecordingExecutor{}
	strategy := NewDCAEnhancedStrategy("dca", "BTCUSDT", dcaTestConfig(), executor, &hedgeExchange{price: 110}, nil)
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
	strategy := NewDCAEnhancedStrategy("dca", "BTCUSDT", dcaTestConfig(), executor, &hedgeExchange{price: 110}, nil)
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
	strategy := NewDCAEnhancedStrategy("dca", "BTCUSDT", dcaTestConfig(), &hedgeOrderExecutor{}, &hedgeExchange{}, nil)
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
	strategy := NewDCAEnhancedStrategy("dca", "BTCUSDT", dcaTestConfig(), &hedgeOrderExecutor{}, &hedgeExchange{}, nil)
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

func TestDCARejectsNonFiniteCommissionBeforeAccounting(t *testing.T) {
	tests := []struct {
		name       string
		commission float64
		asset      string
		price      float64
	}{
		{name: "nan quote fee", commission: math.NaN(), asset: "USDT", price: 100},
		{name: "infinite quote fee", commission: math.Inf(1), asset: "USDT", price: 100},
		{name: "base fee conversion overflow", commission: math.MaxFloat64, asset: "BTC", price: 2},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			executor := &dcaReconciliationExecutor{hedgeOrderExecutor: &hedgeOrderExecutor{}}
			strategy := NewDCAEnhancedStrategy("dca", "BTCUSDT", dcaTestConfig(), executor, &hedgeExchange{}, nil)
			layer := &DCALayer{OrderID: 70, Quantity: 1, RequestedQuantity: 1, Status: entryStatusPending}
			strategy.layers = []*DCALayer{layer}
			strategy.handleLayerOrderUpdate(layer, &position.OrderUpdate{
				OrderID: 70, Status: "PARTIALLY_FILLED", ExecutedQty: 0.5, AvgPrice: tt.price,
				Commission: tt.commission, CommissionAsset: tt.asset, CommissionKnown: true,
			})
			if executor.marked != 1 || layer.FillProgress.Quantity != 0 || layer.OpeningFee != 0 || strategy.totalQty != 0 {
				t.Fatalf("non-finite fee mutated accounting: marked=%d layer=%+v total=%v", executor.marked, layer, strategy.totalQty)
			}
		})
	}
}

func TestDCASpotEntryBaseFeeUsesNetInventoryAndQuoteFee(t *testing.T) {
	cfg := dcaTestConfig()
	cfg.Trading.MarketType = "spot"
	strategy := NewDCAEnhancedStrategy("dca", "BTCUSDT", cfg, &hedgeOrderExecutor{}, &hedgeExchange{}, nil)
	layer := &DCALayer{Index: 0, OrderID: 96, Status: entryStatusPending, RequestedQuantity: 1}
	strategy.layers = []*DCALayer{layer}
	strategy.handleLayerOrderUpdate(layer, &position.OrderUpdate{
		OrderID: 96, Side: "BUY", Status: "FILLED", ExecutedQty: 1, AvgPrice: 100,
		Commission: 0.001, CommissionAsset: "BTC", BaseFeeQty: 0.001, CommissionKnown: true,
	})
	if layer.Quantity != 0.999 || layer.Cost != 99.9 || math.Abs(layer.OpeningFee-0.1) > 1e-12 ||
		layer.EntryBaseFeeQty != 0.001 || layer.FillProgress.Quantity != 1 || strategy.totalQty != 0.999 {
		t.Fatalf("spot base fee did not preserve gross cursor and net inventory accounting: layer=%+v total=%v", layer, strategy.totalQty)
	}
}

func TestDCARejectsUnsupportedEntryAndCloseBaseFees(t *testing.T) {
	executor := &dcaReconciliationExecutor{hedgeOrderExecutor: &hedgeOrderExecutor{}}
	strategy := NewDCAEnhancedStrategy("dca", "BTCUSDT", dcaTestConfig(), executor, &hedgeExchange{}, nil)
	layer := &DCALayer{Index: 0, OrderID: 97, Status: entryStatusPending, RequestedQuantity: 1}
	strategy.layers = []*DCALayer{layer}
	strategy.handleLayerOrderUpdate(layer, &position.OrderUpdate{
		OrderID: 97, Side: "BUY", Status: "PARTIALLY_FILLED", ExecutedQty: 1, AvgPrice: 100,
		Commission: 0.001, CommissionAsset: "BTC", BaseFeeQty: 0.001, CommissionKnown: true,
	})
	if executor.marked != 1 || layer.FillProgress.Quantity != 0 || strategy.totalQty != 0 {
		t.Fatalf("unsupported entry base fee was not held for reconciliation: marks=%d layer=%+v qty=%v", executor.marked, layer, strategy.totalQty)
	}

	strategy.isClosing, strategy.closeOrderID, strategy.closeRequestedQty = true, 98, 1
	strategy.closeProgress = position.FillProgress{}
	strategy.totalQty, strategy.totalCost = 1, 100
	strategy.layers = []*DCALayer{{Index: 0, Price: 100, Quantity: 1, Cost: 100, Status: entryStatusFilled}}
	strategy.handleCloseOrderUpdate(&position.OrderUpdate{
		OrderID: 98, Side: "SELL", Status: "PARTIALLY_FILLED", ExecutedQty: 0.5, AvgPrice: 110, BaseFeeQty: 0.001,
	})
	if executor.marked != 2 || strategy.closeProgress.Quantity != 0 || strategy.totalQty != 1 {
		t.Fatalf("close base fee mutated inventory before reconciliation: marks=%d progress=%+v qty=%v", executor.marked, strategy.closeProgress, strategy.totalQty)
	}
}

func TestDCARejectsBaseCommissionWithoutBaseFeeQuantity(t *testing.T) {
	cfg := dcaTestConfig()
	cfg.Trading.MarketType = "spot"
	executor := &dcaReconciliationExecutor{hedgeOrderExecutor: &hedgeOrderExecutor{}}
	strategy := NewDCAEnhancedStrategy("dca", "BTCUSDT", cfg, executor, &hedgeExchange{}, nil)
	layer := &DCALayer{Index: 0, OrderID: 99, Status: entryStatusPending, RequestedQuantity: 1}
	strategy.layers = []*DCALayer{layer}
	strategy.handleLayerOrderUpdate(layer, &position.OrderUpdate{
		OrderID: 99, Side: "BUY", Status: "PARTIALLY_FILLED", ExecutedQty: 0.5, AvgPrice: 100,
		Commission: 0.0005, CommissionAsset: "BTC", CommissionKnown: true,
	})
	if executor.marked != 1 || layer.FillProgress.Quantity != 0 || layer.Quantity != 0 || strategy.totalQty != 0 {
		t.Fatalf("unmapped base commission was accepted as gross inventory: marks=%d layer=%+v total=%v", executor.marked, layer, strategy.totalQty)
	}
}
