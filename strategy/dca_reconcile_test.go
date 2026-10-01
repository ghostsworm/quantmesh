package strategy

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"sync"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/position"
)

type dcaRecoveryExchange struct {
	*hedgeExchange
	order                *exchange.Order
	lookupOrder          *exchange.Order
	lookupErr            error
	lookupClientOrderIDs []string
	fills                []*exchange.OrderFill
	orderErr             error
	fillsErr             error
	fillsFn              func(orderID int64) (interface{}, error)
}

func (e *dcaRecoveryExchange) GetOrderByClientOrderID(_ context.Context, _, clientOrderID string) (*exchange.Order, error) {
	e.lookupClientOrderIDs = append(e.lookupClientOrderIDs, clientOrderID)
	return e.lookupOrder, e.lookupErr
}

func (e *dcaRecoveryExchange) GetOrder(context.Context, string, int64) (interface{}, error) {
	return e.order, e.orderErr
}

func (e *dcaRecoveryExchange) GetOrderFills(_ context.Context, _ string, orderID int64) (interface{}, error) {
	if e.fillsFn != nil {
		return e.fillsFn(orderID)
	}
	return e.fills, e.fillsErr
}

func newPersistedDCAStrategy(t *testing.T, ex *dcaRecoveryExchange, state dcaRuntimeState) *DCAEnhancedStrategy {
	return newPersistedDCAStrategyWithConfig(t, ex, state, dcaTestConfig())
}

func newPersistedDCAStrategyWithConfig(t *testing.T, ex *dcaRecoveryExchange, state dcaRuntimeState, cfg *config.Config) *DCAEnhancedStrategy {
	t.Helper()
	if len(cfg.Exchanges) == 0 {
		cfg.Exchanges = dcaTestConfig().Exchanges
	}
	s := NewDCAEnhancedStrategy("dca", "BTCUSDT", cfg, &hedgeOrderExecutor{}, ex, nil)
	state.BotID = s.effectiveBotID()
	payload, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	s.SetRuntimeStateStore(&memoryRuntimeStateStore{version: dcaRuntimeStateSchemaVersion, payload: string(payload), found: true})
	return s
}

func TestDCAStartReplaysSpotEntryBaseFeeAsNetInventoryAndQuoteCost(t *testing.T) {
	ex := &dcaRecoveryExchange{hedgeExchange: &hedgeExchange{}, order: &exchange.Order{
		OrderID: 94, Symbol: "BTCUSDT", Side: exchange.SideBuy, Quantity: 1, ExecutedQty: 1,
		AvgPrice: 100, Status: exchange.OrderStatusFilled,
	}, fills: []*exchange.OrderFill{{
		OrderID: 94, TradeID: "trade-94", Symbol: "BTCUSDT", Side: exchange.SideBuy,
		Price: 100, Quantity: 1, Commission: 0.001, CommissionAsset: "BTC", BaseFeeQty: 0.001,
	}}}
	state := dcaRuntimeState{StrategyName: "dca", Symbol: "BTCUSDT", CurrentLayer: 1, CloseLayerIndex: -1,
		Layers: []*DCALayer{{Index: 0, Price: 100, OrderID: 94, Status: entryStatusPending, RequestedQuantity: 1}},
	}
	cfg := dcaTestConfig()
	cfg.Trading.MarketType = "spot"
	s := newPersistedDCAStrategyWithConfig(t, ex, state, cfg)
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start() failed to replay spot base-asset fee: %v", err)
	}
	defer s.Stop()
	layer := s.layers[0]
	if layer.Quantity != 0.999 || layer.Cost != 99.9 || math.Abs(layer.OpeningFee-0.1) > 1e-12 ||
		layer.EntryBaseFeeQty != 0.001 || layer.FillProgress.Quantity != 1 || layer.FillProgress.Notional != 100 {
		t.Fatalf("spot base fee was not reconciled into net inventory and quote accounting: %+v", layer)
	}
}

func TestDCAStartRejectsBaseFeePrefixMismatch(t *testing.T) {
	ex := &dcaRecoveryExchange{hedgeExchange: &hedgeExchange{}, order: &exchange.Order{
		OrderID: 95, Symbol: "BTCUSDT", Side: exchange.SideBuy, Quantity: 1, ExecutedQty: 1,
		AvgPrice: 100, Status: exchange.OrderStatusFilled,
	}, fills: []*exchange.OrderFill{{
		OrderID: 95, TradeID: "trade-95", Symbol: "BTCUSDT", Side: exchange.SideBuy,
		Price: 100, Quantity: 0.5, Commission: 0.0005, CommissionAsset: "BTC", BaseFeeQty: 0.0005,
	}, {
		OrderID: 95, TradeID: "trade-96", Symbol: "BTCUSDT", Side: exchange.SideBuy,
		Price: 100, Quantity: 0.5, Commission: 0.0005, CommissionAsset: "BTC", BaseFeeQty: 0.0005,
	}}}
	state := dcaRuntimeState{StrategyName: "dca", Symbol: "BTCUSDT", CurrentLayer: 1, CloseLayerIndex: -1,
		TotalCost: 49.95, TotalQty: 0.4995, AvgEntryPrice: 100,
		Layers: []*DCALayer{{Index: 0, Price: 100, Quantity: 0.4995, Cost: 49.95, EntryBaseFeeQty: 0.0004, FeeVerifiedQty: 0.5,
			OrderID: 95, Status: entryStatusPartiallyFilled, RequestedQuantity: 1,
			FillProgress: position.FillProgress{Quantity: 0.5, Notional: 50}}},
	}
	cfg := dcaTestConfig()
	cfg.Trading.MarketType = "spot"
	s := newPersistedDCAStrategyWithConfig(t, ex, state, cfg)
	if err := s.Start(context.Background()); err == nil {
		t.Fatal("Start() succeeded despite persisted base-fee cursor mismatch")
	}
	if s.IsRunning() || s.totalQty != 0.4995 || s.layers[0].FillProgress.Quantity != 0.5 {
		t.Fatalf("failed recovery mutated persisted inventory or cursor: running=%v layer=%+v", s.IsRunning(), s.layers[0])
	}
}

func TestDCAStartReplaysOnlySpotBaseFeeSuffixAfterMatchingPrefix(t *testing.T) {
	ex := &dcaRecoveryExchange{hedgeExchange: &hedgeExchange{}, order: &exchange.Order{
		OrderID: 96, Symbol: "BTCUSDT", Side: exchange.SideBuy, Quantity: 1, ExecutedQty: 0.5,
		AvgPrice: 101, Status: exchange.OrderStatusPartiallyFilled,
	}, fills: []*exchange.OrderFill{
		{OrderID: 96, TradeID: "trade-96a", Symbol: "BTCUSDT", Side: exchange.SideBuy,
			Price: 100, Quantity: 0.25, Commission: 0.00025, CommissionAsset: "BTC", BaseFeeQty: 0.00025, TradeTime: 1_700_000_000_000},
		{OrderID: 96, TradeID: "trade-96b", Symbol: "BTCUSDT", Side: exchange.SideBuy,
			Price: 102, Quantity: 0.25, Commission: 0.00026, CommissionAsset: "BTC", BaseFeeQty: 0.00026, TradeTime: 1_700_000_000_001},
	}}
	state := dcaRuntimeState{StrategyName: "dca", Symbol: "BTCUSDT", CurrentLayer: 1, CloseLayerIndex: -1,
		TotalCost: 24.975, TotalQty: 0.24975, AvgEntryPrice: 100,
		Layers: []*DCALayer{{Index: 0, Price: 100, Quantity: 0.24975, Cost: 24.975, OpeningFee: 0.025, FeeVerifiedQty: 0.25,
			EntryBaseFeeQty: 0.00025, OrderID: 96, Status: entryStatusPartiallyFilled, RequestedQuantity: 1,
			FillProgress: position.FillProgress{Quantity: 0.25, Notional: 25}}},
	}
	cfg := dcaTestConfig()
	cfg.Trading.MarketType = "spot"
	s := newPersistedDCAStrategyWithConfig(t, ex, state, cfg)
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start() failed to replay verified base-fee suffix: %v", err)
	}
	defer s.Stop()
	layer := s.layers[0]
	if math.Abs(layer.Quantity-0.49949) > 1e-12 || math.Abs(layer.Cost-50.44848) > 1e-10 ||
		math.Abs(layer.OpeningFee-0.05152) > 1e-12 || math.Abs(layer.EntryBaseFeeQty-0.00051) > 1e-12 ||
		layer.FillProgress.Quantity != 0.5 || layer.FillProgress.Notional != 50.5 {
		t.Fatalf("recovery did not apply only the spot base-fee suffix: %+v", layer)
	}
}

func TestDCAStartRejectsBaseCommissionWithoutBaseFeeQuantity(t *testing.T) {
	ex := &dcaRecoveryExchange{hedgeExchange: &hedgeExchange{}, order: &exchange.Order{
		OrderID: 97, Symbol: "BTCUSDT", Side: exchange.SideBuy, Quantity: 1, ExecutedQty: 0.5,
		AvgPrice: 100, Status: exchange.OrderStatusPartiallyFilled,
	}, fills: []*exchange.OrderFill{{
		OrderID: 97, TradeID: "trade-97", Symbol: "BTCUSDT", Side: exchange.SideBuy,
		Price: 100, Quantity: 0.5, Commission: 0.0005, CommissionAsset: "BTC",
	}}}
	state := dcaRuntimeState{StrategyName: "dca", Symbol: "BTCUSDT", CurrentLayer: 1, CloseLayerIndex: -1,
		Layers: []*DCALayer{{Index: 0, Price: 100, OrderID: 97, Status: entryStatusPending, RequestedQuantity: 1}},
	}
	cfg := dcaTestConfig()
	cfg.Trading.MarketType = "spot"
	s := newPersistedDCAStrategyWithConfig(t, ex, state, cfg)
	if err := s.Start(context.Background()); err == nil {
		t.Fatal("Start() accepted base-asset commission with unknown inventory fee quantity")
	}
	if s.IsRunning() || s.totalQty != 0 || s.layers[0].FillProgress.Quantity != 0 {
		t.Fatalf("failed recovery changed strategy economics: running=%v layer=%+v", s.IsRunning(), s.layers[0])
	}
}

func TestDCAOnOrderUpdateVerifiesUnreportedSpotFeeBeforeAccounting(t *testing.T) {
	ex := &dcaRecoveryExchange{hedgeExchange: &hedgeExchange{}, fills: []*exchange.OrderFill{{
		OrderID: 98, TradeID: "trade-98", Symbol: "BTCUSDT", Side: exchange.SideBuy,
		Price: 100, Quantity: 0.5, Commission: 0.05, CommissionAsset: "USDT",
	}}}
	cfg := dcaTestConfig()
	cfg.Trading.MarketType = "spot"
	s := NewDCAEnhancedStrategy("dca", "BTCUSDT", cfg, &hedgeOrderExecutor{}, ex, nil)
	setTestRuntimeStateStore(t, s)
	s.layers = []*DCALayer{{Index: 0, OrderID: 98, Status: entryStatusPending, RequestedQuantity: 1}}
	if err := s.OnOrderUpdate(&position.OrderUpdate{
		OrderID: 98, Side: "BUY", Status: "PARTIALLY_FILLED", ExecutedQty: 0.5, AvgPrice: 100,
		CommissionAsset: "USDT", CommissionKnown: false,
	}); err != nil {
		t.Fatalf("OnOrderUpdate() failed to verify fee evidence: %v", err)
	}
	layer := s.layers[0]
	if layer.Quantity != 0.5 || layer.Cost != 50 || layer.OpeningFee != 0.05 || layer.FillProgress.Quantity != 0.5 {
		t.Fatalf("DCA did not account using verified fill fees: %+v", layer)
	}
}

func TestDCAConcurrentUnverifiedUpdatesReconcileAgainstLatestFillCursor(t *testing.T) {
	firstFillQuery := make(chan struct{})
	secondFillQuery := make(chan struct{})
	releaseFullHistory := make(chan struct{})
	var queryMu sync.Mutex
	queryCount := 0
	ex := &dcaRecoveryExchange{hedgeExchange: &hedgeExchange{}}
	ex.fillsFn = func(int64) (interface{}, error) {
		queryMu.Lock()
		queryCount++
		currentQuery := queryCount
		queryMu.Unlock()
		if currentQuery == 1 {
			close(firstFillQuery)
			<-releaseFullHistory
			return []*exchange.OrderFill{
				{OrderID: 120, TradeID: "cursor-a", Symbol: "BTCUSDT", Side: exchange.SideBuy, Price: 100, Quantity: 0.5,
					Commission: 0.05, CommissionAsset: "USDT", TradeTime: 1_700_000_000_000},
				{OrderID: 120, TradeID: "cursor-b", Symbol: "BTCUSDT", Side: exchange.SideBuy, Price: 100, Quantity: 0.5,
					Commission: 0.05, CommissionAsset: "USDT", TradeTime: 1_700_000_000_001},
			}, nil
		}
		close(secondFillQuery)
		return []*exchange.OrderFill{{OrderID: 120, TradeID: "cursor-a", Symbol: "BTCUSDT", Side: exchange.SideBuy, Price: 100,
			Quantity: 0.5, Commission: 0.05, CommissionAsset: "USDT", TradeTime: 1_700_000_000_000}}, nil
	}
	s := NewDCAEnhancedStrategy("dca", "BTCUSDT", dcaTestConfig(), &hedgeOrderExecutor{}, ex, nil)
	s.SetRuntimeStateStore(&memoryRuntimeStateStore{})
	s.layers = []*DCALayer{{Index: 0, OrderID: 120, Status: entryStatusPartiallyFilled, RequestedQuantity: 1}}
	fullUpdateDone := make(chan error, 1)
	partialUpdateDone := make(chan error, 1)
	go func() {
		fullUpdateDone <- s.OnOrderUpdate(&position.OrderUpdate{OrderID: 120, Symbol: "BTCUSDT", Side: "BUY",
			Status: position.OrderStatusFilled, ExecutedQty: 1, AvgPrice: 100, CommissionKnown: false})
	}()
	<-firstFillQuery // The full-order update has captured the empty persisted cursor before blocking on fills.
	go func() {
		partialUpdateDone <- s.OnOrderUpdate(&position.OrderUpdate{OrderID: 120, Symbol: "BTCUSDT", Side: "BUY",
			Status: position.OrderStatusPartiallyFilled, ExecutedQty: 0.5, AvgPrice: 100, CommissionKnown: false})
	}()
	partialFinished := false
	select {
	case <-secondFillQuery:
		if err := <-partialUpdateDone; err != nil {
			t.Fatal(err)
		}
		partialFinished = true
	case <-time.After(250 * time.Millisecond):
		// A serialized callback waits here until the first order update is applied.
	}
	close(releaseFullHistory)
	if err := <-fullUpdateDone; err != nil {
		t.Fatal(err)
	}
	if !partialFinished {
		select {
		case err := <-partialUpdateDone:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(time.Second):
			t.Fatal("concurrent partial update did not complete")
		}
	}
	if got := s.layers[0]; got.FillProgress.Quantity != 1 || got.Quantity != 1 || got.Status != entryStatusFilled || math.Abs(got.OpeningFee-0.1) > 1e-12 {
		t.Fatalf("concurrent DCA fee cursor was double-counted: %+v", got)
	}
}

func TestDCAOnOrderUpdateVerifiesUnreportedFuturesFeeBeforeAccounting(t *testing.T) {
	ex := &dcaRecoveryExchange{hedgeExchange: &hedgeExchange{}, fills: []*exchange.OrderFill{{
		OrderID: 108, TradeID: "trade-108", Symbol: "BTCUSDT", Side: exchange.SideBuy,
		Price: 100, Quantity: 0.5, Commission: 0.05, CommissionAsset: "USDT",
	}}}
	cfg := dcaTestConfig()
	cfg.Trading.MarketType = "futures"
	s := NewDCAEnhancedStrategy("dca", "BTCUSDT", cfg, &hedgeOrderExecutor{}, ex, nil)
	setTestRuntimeStateStore(t, s)
	s.layers = []*DCALayer{{Index: 0, OrderID: 108, Status: entryStatusPending, RequestedQuantity: 1}}
	if err := s.OnOrderUpdate(&position.OrderUpdate{
		OrderID: 108, Side: "BUY", Status: "PARTIALLY_FILLED", ExecutedQty: 0.5, AvgPrice: 100,
		CommissionAsset: "USDT", CommissionKnown: false,
	}); err != nil {
		t.Fatalf("OnOrderUpdate() failed to verify futures fee evidence: %v", err)
	}
	layer := s.layers[0]
	if layer.Quantity != 0.5 || layer.Cost != 50 || layer.OpeningFee != 0.05 || layer.FillProgress.Quantity != 0.5 {
		t.Fatalf("DCA futures accounting did not include verified fees: %+v", layer)
	}
}

func TestDCAFuturesDoesNotAccountFillWhenFeeEvidenceIsUnavailable(t *testing.T) {
	ex := &dcaRecoveryExchange{hedgeExchange: &hedgeExchange{}, fillsErr: errors.New("fills unavailable")}
	cfg := dcaTestConfig()
	cfg.Trading.MarketType = "futures"
	executor := &dcaReconciliationExecutor{hedgeOrderExecutor: &hedgeOrderExecutor{}}
	s := NewDCAEnhancedStrategy("dca", "BTCUSDT", cfg, executor, ex, nil)
	setTestRuntimeStateStore(t, s)
	layer := &DCALayer{Index: 0, OrderID: 109, Status: entryStatusPending, RequestedQuantity: 1}
	s.layers = []*DCALayer{layer}
	err := s.OnOrderUpdate(&position.OrderUpdate{
		OrderID: 109, Side: "BUY", Status: "PARTIALLY_FILLED", ExecutedQty: 0.5, AvgPrice: 100,
		CommissionAsset: "USDT", CommissionKnown: false,
	})
	if err == nil || executor.marked != 1 || layer.Quantity != 0 || layer.FillProgress.Quantity != 0 {
		t.Fatalf("unverified futures fee changed DCA state: err=%v marks=%d layer=%+v", err, executor.marked, layer)
	}
}

func TestDCAOnOrderUpdatePreservesStateWhenSpotFeeEvidenceIsUnavailable(t *testing.T) {
	ex := &dcaRecoveryExchange{hedgeExchange: &hedgeExchange{}, fillsErr: errors.New("fills unavailable")}
	cfg := dcaTestConfig()
	cfg.Trading.MarketType = "spot"
	executor := &dcaReconciliationExecutor{hedgeOrderExecutor: &hedgeOrderExecutor{}}
	s := NewDCAEnhancedStrategy("dca", "BTCUSDT", cfg, executor, ex, nil)
	setTestRuntimeStateStore(t, s)
	layer := &DCALayer{Index: 0, OrderID: 99, Status: entryStatusPending, RequestedQuantity: 1}
	s.layers = []*DCALayer{layer}
	err := s.OnOrderUpdate(&position.OrderUpdate{
		OrderID: 99, Side: "BUY", Status: "PARTIALLY_FILLED", ExecutedQty: 0.5, AvgPrice: 100,
		CommissionAsset: "USDT", CommissionKnown: false,
	})
	if err == nil || executor.marked != 1 || layer.Quantity != 0 || layer.FillProgress.Quantity != 0 {
		t.Fatalf("unverified spot fee changed DCA state: err=%v marks=%d layer=%+v", err, executor.marked, layer)
	}
}

func TestDCAStartRejectsLegacySpotSnapshotWithUnverifiedFeeCursor(t *testing.T) {
	state := dcaRuntimeState{StrategyName: "dca", Symbol: "BTCUSDT", TotalCost: 100, TotalQty: 1, AvgEntryPrice: 100,
		CurrentLayer: 1, CloseLayerIndex: -1,
		Layers: []*DCALayer{{Index: 0, Price: 100, Quantity: 1, Cost: 100, OrderID: 100,
			Status: entryStatusFilled, RequestedQuantity: 1,
			FillProgress: position.FillProgress{Quantity: 1, Notional: 100}}},
	}
	cfg := dcaTestConfig()
	cfg.Trading.MarketType = "spot"
	s := newPersistedDCAStrategyWithConfig(t, &dcaRecoveryExchange{hedgeExchange: &hedgeExchange{}}, state, cfg)
	if err := s.Start(context.Background()); err == nil {
		t.Fatal("Start() accepted a legacy spot snapshot whose execution fee history was never verified")
	}
	if s.IsRunning() || s.totalQty != 0 {
		t.Fatalf("legacy snapshot refusal applied unverified economics: running=%v qty=%v", s.IsRunning(), s.totalQty)
	}
}

func TestDCAUnverifiedZeroFillCancellationDoesNotRequireFeeHistory(t *testing.T) {
	cfg := dcaTestConfig()
	cfg.Trading.MarketType = "spot"
	s := NewDCAEnhancedStrategy("dca", "BTCUSDT", cfg, &hedgeOrderExecutor{}, &hedgeExchange{}, nil)
	setTestRuntimeStateStore(t, s)
	layer := &DCALayer{Index: 0, OrderID: 101, Status: entryStatusPending, RequestedQuantity: 1}
	s.layers = []*DCALayer{layer}
	if err := s.OnOrderUpdate(&position.OrderUpdate{OrderID: 101, Status: "CANCELED", CommissionKnown: false}); err != nil {
		t.Fatalf("zero-fill cancellation should not require fee query: %v", err)
	}
	if len(s.layers) != 0 {
		t.Fatalf("zero-fill terminal order was not released: %+v", s.layers)
	}
}

func TestDCAStartReplaysVerifiedCloseFillMissedWhileOffline(t *testing.T) {
	ex := &dcaRecoveryExchange{hedgeExchange: &hedgeExchange{}, order: &exchange.Order{
		OrderID: 90, Symbol: "BTCUSDT", Side: exchange.SideSell, Quantity: 1, ExecutedQty: 1,
		AvgPrice: 110, Status: exchange.OrderStatusFilled,
	}, fills: []*exchange.OrderFill{{
		OrderID: 90, TradeID: "trade-90", Symbol: "BTCUSDT", Side: exchange.SideSell,
		Price: 110, Quantity: 1, Commission: 0.11, CommissionAsset: "USDT",
	}}}
	state := dcaRuntimeState{
		StrategyName: "dca", Symbol: "BTCUSDT", TotalCost: 100, TotalQty: 1, AvgEntryPrice: 100,
		CurrentLayer: 1, IsClosing: true, CloseOrderID: 90, CloseLayerIndex: -1,
		CloseRequestedQty: 1, CloseLimitPrice: 110,
		Layers: []*DCALayer{{Index: 0, Price: 100, Quantity: 1, Cost: 100, OpeningFee: 0.2,
			RequestedQuantity: 1, FillProgress: position.FillProgress{Quantity: 1, Notional: 100}, Status: entryStatusFilled}},
	}
	s := newPersistedDCAStrategy(t, ex, state)
	ledger := &dcaFillRecorder{}
	s.SetTradeStorage(ledger)
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start() failed to replay verified close: %v", err)
	}
	defer s.Stop()
	if s.isClosing || s.totalQty != 0 || len(ledger.fees) != 1 || ledger.fees[0] != 0.31 || ledger.feeAssets[0] != "USDT" {
		t.Fatalf("missed close was not reconciled exactly once: closing=%v qty=%v fees=%v assets=%v",
			s.isClosing, s.totalQty, ledger.fees, ledger.feeAssets)
	}
}

func TestDCAStartFailsClosedWhenOrderOrFillEvidenceIsMissing(t *testing.T) {
	tests := []struct {
		name string
		ex   *dcaRecoveryExchange
	}{
		{name: "order missing", ex: &dcaRecoveryExchange{hedgeExchange: &hedgeExchange{}}},
		{name: "fills unavailable", ex: &dcaRecoveryExchange{hedgeExchange: &hedgeExchange{}, order: &exchange.Order{
			OrderID: 91, Symbol: "BTCUSDT", Side: exchange.SideSell, Quantity: 1, ExecutedQty: 1,
			AvgPrice: 110, Status: exchange.OrderStatusFilled,
		}, fillsErr: errors.New("history unavailable")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := dcaRuntimeState{StrategyName: "dca", Symbol: "BTCUSDT", TotalCost: 100, TotalQty: 1, AvgEntryPrice: 100,
				CurrentLayer: 1, IsClosing: true, CloseOrderID: 91, CloseLayerIndex: -1, CloseRequestedQty: 1,
				Layers: []*DCALayer{{Index: 0, Price: 100, Quantity: 1, Cost: 100, RequestedQuantity: 1,
					FillProgress: position.FillProgress{Quantity: 1, Notional: 100}, Status: entryStatusFilled}},
			}
			s := newPersistedDCAStrategy(t, tt.ex, state)
			if err := s.Start(context.Background()); err == nil {
				t.Fatal("Start() succeeded without complete venue evidence")
			}
			if s.IsRunning() || s.totalQty != 1 || !s.isClosing {
				t.Fatalf("failed recovery did not preserve stopped/blocked state: running=%v qty=%v closing=%v", s.IsRunning(), s.totalQty, s.isClosing)
			}
		})
	}
}

func TestDCAStartReplaysVerifiedEntryFillMissedWhileOffline(t *testing.T) {
	ex := &dcaRecoveryExchange{hedgeExchange: &hedgeExchange{}, order: &exchange.Order{
		OrderID: 92, Symbol: "BTCUSDT", Side: exchange.SideBuy, Quantity: 1, ExecutedQty: 0.5,
		AvgPrice: 100, Status: exchange.OrderStatusPartiallyFilled,
	}, fills: []*exchange.OrderFill{{
		OrderID: 92, TradeID: "trade-92", Symbol: "BTCUSDT", Side: exchange.SideBuy,
		Price: 100, Quantity: 0.5, Commission: 0.05, CommissionAsset: "USDT",
	}}}
	state := dcaRuntimeState{StrategyName: "dca", Symbol: "BTCUSDT", CurrentLayer: 1, CloseLayerIndex: -1,
		Layers: []*DCALayer{{Index: 0, Price: 100, OrderID: 92, Status: entryStatusPending, RequestedQuantity: 1}}}
	s := newPersistedDCAStrategy(t, ex, state)
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start() failed to replay verified entry: %v", err)
	}
	defer s.Stop()
	if s.totalQty != 0.5 || s.totalCost != 50 || s.layers[0].OpeningFee != 0.05 || s.layers[0].Status != entryStatusPartiallyFilled {
		t.Fatalf("missed entry fill was not reconciled: qty=%v cost=%v fee=%v layer=%+v", s.totalQty, s.totalCost, s.layers[0].OpeningFee, s.layers[0])
	}
}

func TestDCAStartReplaysOnlyFillDeltaAfterPersistedCursor(t *testing.T) {
	ex := &dcaRecoveryExchange{hedgeExchange: &hedgeExchange{}, order: &exchange.Order{
		OrderID: 93, Symbol: "BTCUSDT", Side: exchange.SideBuy, Quantity: 1, ExecutedQty: 0.5,
		AvgPrice: 101, Status: exchange.OrderStatusPartiallyFilled,
	}, fills: []*exchange.OrderFill{
		{OrderID: 93, TradeID: "trade-93a", Symbol: "BTCUSDT", Side: exchange.SideBuy, Price: 100, Quantity: 0.25, Commission: 0.025, CommissionAsset: "USDT", TradeTime: 1_700_000_000_000},
		{OrderID: 93, TradeID: "trade-93b", Symbol: "BTCUSDT", Side: exchange.SideBuy, Price: 102, Quantity: 0.25, Commission: 0.026, CommissionAsset: "USDT", TradeTime: 1_700_000_000_001},
	}}
	state := dcaRuntimeState{StrategyName: "dca", Symbol: "BTCUSDT", CurrentLayer: 1, CloseLayerIndex: -1,
		TotalCost: 25, TotalQty: 0.25, AvgEntryPrice: 100,
		Layers: []*DCALayer{{Index: 0, Price: 100, Quantity: 0.25, Cost: 25, OpeningFee: 0.025, OrderID: 93,
			Status: entryStatusPartiallyFilled, RequestedQuantity: 1,
			FillProgress: position.FillProgress{Quantity: 0.25, Notional: 25}}},
	}
	s := newPersistedDCAStrategy(t, ex, state)
	if err := s.Start(context.Background()); err != nil {
		t.Fatalf("Start() failed to replay the unpersisted fill suffix: %v", err)
	}
	defer s.Stop()
	layer := s.layers[0]
	if layer.Quantity != 0.5 || layer.Cost != 50.5 || math.Abs(layer.OpeningFee-0.051) > 1e-12 || layer.FillProgress.Quantity != 0.5 {
		t.Fatalf("recovery did not apply only the new fill suffix: %+v", layer)
	}
}

func TestDCAStartRejectsUntimedFillWithPersistedCursor(t *testing.T) {
	ex := &dcaRecoveryExchange{hedgeExchange: &hedgeExchange{}, order: &exchange.Order{
		OrderID: 109, Symbol: "BTCUSDT", Side: exchange.SideBuy, Quantity: 1, ExecutedQty: 0.5,
		AvgPrice: 101, Status: exchange.OrderStatusPartiallyFilled,
	}, fills: []*exchange.OrderFill{
		{OrderID: 109, TradeID: "trade-109a", Symbol: "BTCUSDT", Side: exchange.SideBuy, Price: 100, Quantity: 0.25, Commission: 0.025, CommissionAsset: "USDT", TradeTime: 1_700_000_000_000},
		{OrderID: 109, TradeID: "trade-109b", Symbol: "BTCUSDT", Side: exchange.SideBuy, Price: 102, Quantity: 0.25, Commission: 0.026, CommissionAsset: "USDT"},
	}}
	state := dcaRuntimeState{StrategyName: "dca", Symbol: "BTCUSDT", CurrentLayer: 1, CloseLayerIndex: -1,
		TotalCost: 25, TotalQty: 0.25, AvgEntryPrice: 100,
		Layers: []*DCALayer{{Index: 0, Price: 100, Quantity: 0.25, Cost: 25, OpeningFee: 0.025, OrderID: 109,
			Status: entryStatusPartiallyFilled, RequestedQuantity: 1,
			FillProgress: position.FillProgress{Quantity: 0.25, Notional: 25}}},
	}
	s := newPersistedDCAStrategy(t, ex, state)
	if err := s.Start(context.Background()); err == nil {
		t.Fatal("Start() accepted an untimed fill while reconstructing a persisted fill prefix")
	}
	if s.IsRunning() || s.totalQty != 0.25 || s.layers[0].FillProgress.Quantity != 0.25 {
		t.Fatalf("failed recovery mutated persisted inventory or cursor: running=%v layer=%+v", s.IsRunning(), s.layers[0])
	}
}

func TestDCAStartRejectsCursorSplittingSameTimestampTrades(t *testing.T) {
	const tradeTime = int64(1_700_000_000_000)
	ex := &dcaRecoveryExchange{hedgeExchange: &hedgeExchange{}, order: &exchange.Order{
		OrderID: 110, Symbol: "BTCUSDT", Side: exchange.SideBuy, Quantity: 1, ExecutedQty: 0.5,
		AvgPrice: 100, Status: exchange.OrderStatusPartiallyFilled,
	}, fills: []*exchange.OrderFill{
		{OrderID: 110, TradeID: "trade-110a", Symbol: "BTCUSDT", Side: exchange.SideBuy, Price: 100, Quantity: 0.25, Commission: 0.025, CommissionAsset: "USDT", TradeTime: tradeTime},
		{OrderID: 110, TradeID: "trade-110b", Symbol: "BTCUSDT", Side: exchange.SideBuy, Price: 100, Quantity: 0.25, Commission: 0.025, CommissionAsset: "USDT", TradeTime: tradeTime},
	}}
	state := dcaRuntimeState{StrategyName: "dca", Symbol: "BTCUSDT", CurrentLayer: 1, CloseLayerIndex: -1,
		TotalCost: 25, TotalQty: 0.25, AvgEntryPrice: 100,
		Layers: []*DCALayer{{Index: 0, Price: 100, Quantity: 0.25, Cost: 25, OpeningFee: 0.025, OrderID: 110,
			Status: entryStatusPartiallyFilled, RequestedQuantity: 1,
			FillProgress: position.FillProgress{Quantity: 0.25, Notional: 25}}},
	}
	s := newPersistedDCAStrategy(t, ex, state)
	if err := s.Start(context.Background()); err == nil {
		t.Fatal("Start() accepted a cursor that splits trades sharing one timestamp")
	}
	if s.IsRunning() || s.totalQty != 0.25 || s.layers[0].FillProgress.Quantity != 0.25 {
		t.Fatalf("failed recovery mutated persisted inventory or cursor: running=%v layer=%+v", s.IsRunning(), s.layers[0])
	}
}
