package strategy

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"testing"

	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/position"
)

type dcaRecoveryExchange struct {
	*hedgeExchange
	order    *exchange.Order
	fills    []*exchange.OrderFill
	orderErr error
	fillsErr error
}

func (e *dcaRecoveryExchange) GetOrder(context.Context, string, int64) (interface{}, error) {
	return e.order, e.orderErr
}

func (e *dcaRecoveryExchange) GetOrderFills(context.Context, string, int64) (interface{}, error) {
	return e.fills, e.fillsErr
}

func newPersistedDCAStrategy(t *testing.T, ex *dcaRecoveryExchange, state dcaRuntimeState) *DCAEnhancedStrategy {
	t.Helper()
	s := NewDCAEnhancedStrategy("dca", "BTCUSDT", &config.Config{}, &hedgeOrderExecutor{}, ex, nil)
	state.BotID = s.effectiveBotID()
	payload, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	s.SetRuntimeStateStore(&memoryRuntimeStateStore{version: dcaRuntimeStateSchemaVersion, payload: string(payload), found: true})
	return s
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
		{OrderID: 93, TradeID: "trade-93a", Symbol: "BTCUSDT", Side: exchange.SideBuy, Price: 100, Quantity: 0.25, Commission: 0.025, CommissionAsset: "USDT"},
		{OrderID: 93, TradeID: "trade-93b", Symbol: "BTCUSDT", Side: exchange.SideBuy, Price: 102, Quantity: 0.25, Commission: 0.026, CommissionAsset: "USDT"},
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
