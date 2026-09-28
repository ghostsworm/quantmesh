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

type memoryRuntimeStateStore struct {
	version int
	payload string
	found   bool
	err     error
}

func setTestRuntimeStateStore(t *testing.T, strategy interface{ SetRuntimeStateStore(RuntimeStateStore) }) {
	t.Helper()
	strategy.SetRuntimeStateStore(&memoryRuntimeStateStore{})
}

func (m *memoryRuntimeStateStore) LoadRuntimeState(string) (int, string, bool, error) {
	return m.version, m.payload, m.found, m.err
}

func (m *memoryRuntimeStateStore) SaveRuntimeState(_ string, version int, payload string) error {
	if m.err != nil {
		return m.err
	}
	m.version, m.payload, m.found = version, payload, true
	return nil
}

func TestDCAOrderFillPersistsAndRestoresFeeBearingInventory(t *testing.T) {
	store := &memoryRuntimeStateStore{}
	cfg := &config.Config{}
	first := NewDCAEnhancedStrategy("dca", "BTCUSDT", cfg, &hedgeOrderExecutor{}, &hedgeExchange{}, nil)
	first.SetRuntimeStateStore(store)
	if err := first.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	first.layers = []*DCALayer{{Index: 0, Quantity: 0.5, RequestedQuantity: 0.5, OrderID: 77, Status: entryStatusPending}}
	if err := first.OnOrderUpdate(&position.OrderUpdate{
		OrderID: 77, Status: "PARTIALLY_FILLED", ExecutedQty: 0.5, AvgPrice: 100,
		Commission: 0.1, CommissionAsset: "USDT",
	}); err != nil {
		t.Fatal(err)
	}
	if !store.found {
		t.Fatal("fill transition was not durably stored")
	}

	second := NewDCAEnhancedStrategy("dca", "BTCUSDT", cfg, &hedgeOrderExecutor{}, &hedgeExchange{}, nil)
	second.SetRuntimeStateStore(store)
	if err := second.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	second.lastPrice = 110
	positions := second.GetPositions()
	if len(positions) != 1 || positions[0].Size != 0.5 || positions[0].OpeningFee != 0.1 || math.Abs(positions[0].PnL-4.9) > 1e-9 {
		t.Fatalf("restored inventory mismatch: %+v", positions)
	}
}

func TestDCAInvalidPersistedIdentityBlocksStart(t *testing.T) {
	store := &memoryRuntimeStateStore{version: dcaRuntimeStateSchemaVersion, payload: `{ "bot_id": "wrong" }`, found: true}
	dca := NewDCAEnhancedStrategy("dca", "BTCUSDT", &config.Config{}, &hedgeOrderExecutor{}, &hedgeExchange{}, nil)
	dca.SetRuntimeStateStore(store)
	if err := dca.Start(context.Background()); err == nil {
		t.Fatal("expected corrupt/mismatched state to block startup")
	}
	if dca.IsRunning() {
		t.Fatal("strategy started despite invalid persisted state")
	}
}

func TestDCAAndMartingaleRequireDurableRuntimeState(t *testing.T) {
	dca := NewDCAEnhancedStrategy("dca", "BTCUSDT", &config.Config{}, &hedgeOrderExecutor{}, &hedgeExchange{}, nil)
	if err := dca.Start(context.Background()); err == nil {
		t.Fatal("DCA started without durable runtime state")
	}
	if dca.IsRunning() {
		t.Fatal("DCA marked itself running without durable runtime state")
	}

	martin := NewMartingaleStrategy("martingale", "BTCUSDT", &config.Config{}, &hedgeOrderExecutor{}, &hedgeExchange{}, nil)
	if err := martin.Start(context.Background()); err == nil {
		t.Fatal("martingale started without durable runtime state")
	}
	if martin.IsRunning() {
		t.Fatal("martingale marked itself running without durable runtime state")
	}
}

func TestMartingaleReversePendingOrderDoesNotCountRequestedInventory(t *testing.T) {
	executor := &hedgeOrderExecutor{}
	martin := NewMartingaleStrategy("martingale", "BTCUSDT", &config.Config{}, executor, &hedgeExchange{}, map[string]interface{}{
		"reverse_multiplier": 1.5,
		"price_step":         2.0,
	})
	setTestRuntimeStateStore(t, martin)
	martin.entries = []*MartingaleEntry{{Level: 0, Price: 100, Quantity: 1, Cost: 100, Status: entryStatusFilled}}
	martin.currentLevel = 1
	martin.updateTotals()
	if err := martin.checkReverseMartingale(110); err != nil {
		t.Fatal(err)
	}
	if len(martin.entries) != 2 {
		t.Fatalf("entries=%d want 2", len(martin.entries))
	}
	pending := martin.entries[1]
	if pending.Quantity != 0 || pending.Cost != 0 || pending.RequestedQuantity <= 0 {
		t.Fatalf("pending order was pre-counted as filled inventory: %+v", pending)
	}
	martin.handleEntryOrderUpdate(pending, &position.OrderUpdate{
		OrderID: pending.OrderID, Status: "PARTIALLY_FILLED", ExecutedQty: 0.5,
		AvgPrice: 110, Commission: 0.05, CommissionAsset: "USDT",
	})
	if math.Abs(martin.totalQty-1.5) > 1e-9 || math.Abs(martin.totalCost-155) > 1e-9 {
		t.Fatalf("inventory counted requested amount instead of fill: qty=%v cost=%v", martin.totalQty, martin.totalCost)
	}
}

func TestMartingaleRuntimeStateLoadFailureBlocksStart(t *testing.T) {
	store := &memoryRuntimeStateStore{err: errors.New("database unavailable")}
	martin := NewMartingaleStrategy("martingale", "BTCUSDT", &config.Config{}, &hedgeOrderExecutor{}, &hedgeExchange{}, nil)
	martin.SetRuntimeStateStore(store)
	if err := martin.Start(context.Background()); err == nil {
		t.Fatal("expected state load failure to prevent strategy startup")
	}
	if martin.IsRunning() {
		t.Fatal("strategy started despite state load failure")
	}
}

func TestSpotShortPendingRepaymentRestoresBeforeStart(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.BotID = "bot-spot-short"
	cfg.Trading.Symbol = "BTCUSDT"
	store := &memoryRuntimeStateStore{}
	first := newSpotShortForTest(&signalTestExecutor{}, &signalTestExchange{}, &mockMarginExchange{})
	first.cfg = cfg
	first.SetRuntimeStateStore(store)
	if err := first.decreaseShort(context.Background(), 0.25); err != nil {
		t.Fatal(err)
	}
	if !store.found {
		t.Fatal("buy order was accepted without persisting its pending repayment")
	}

	second := NewSpotShortStrategy("spot_short", cfg, &signalTestExecutor{}, &spotShortReconcileExchange{
		order: &exchange.Order{OrderID: 1, Symbol: "BTCUSDT", Side: exchange.SideBuy, Status: exchange.OrderStatusNew, Quantity: 0.25},
	}, &mockMarginExchange{}, map[string]interface{}{})
	second.SetRuntimeStateStore(store)
	if err := second.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	if second.pendingRepay[1].OrderQuantity != 0.25 {
		t.Fatalf("pending repayment not restored: %+v", second.pendingRepay)
	}
}

func TestSpotShortRefusesStartWithoutDurableStateStore(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.Symbol = "BTCUSDT"
	s := NewSpotShortStrategy("spot_short", cfg, &signalTestExecutor{}, &signalTestExchange{}, &mockMarginExchange{}, nil)
	if err := s.Start(context.Background()); err == nil {
		t.Fatal("spot short started without durable debt/order recovery")
	}
}

func TestDCAStateSnapshotRoundTripsCloseOrderLayerIdentity(t *testing.T) {
	dca := NewDCAEnhancedStrategy("dca", "BTCUSDT", &config.Config{}, nil, nil, nil)
	dca.layers = []*DCALayer{{Index: 0, Quantity: 1, Cost: 100, Status: entryStatusFilled}}
	dca.totalQty, dca.totalCost, dca.avgEntryPrice = 1, 100, 100
	dca.closeLayer = dca.layers[0]
	dca.isClosing, dca.closeOrderID = true, 99
	state := dca.runtimeStateSnapshotLocked()
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	store := &memoryRuntimeStateStore{version: dcaRuntimeStateSchemaVersion, payload: string(data), found: true}
	copy := NewDCAEnhancedStrategy("dca", "BTCUSDT", &config.Config{}, nil, nil, nil)
	copy.SetRuntimeStateStore(store)
	if err := copy.restoreRuntimeState(); err != nil {
		t.Fatal(err)
	}
	if copy.closeLayer == nil || copy.closeLayer.Index != 0 {
		t.Fatalf("close layer identity was not restored: %+v", copy.closeLayer)
	}
}

func TestSignalStrategiesRestoreFeeBearingPositionAndActiveOrder(t *testing.T) {
	for _, strategyName := range []string{"trend", "mean_reversion", "momentum"} {
		t.Run(strategyName, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Trading.BotID = "bot-signal-restore"
			cfg.Trading.Symbol = "BTCUSDT"
			ex := &hedgeExchange{}
			store := &memoryRuntimeStateStore{}
			state := signalRuntimeState{
				BotID: cfg.Trading.BotID, StrategyName: strategyName, Symbol: "BTCUSDT",
				Position:   &Position{Symbol: "BTCUSDT", Size: 0.5, EntryPrice: 100, OpeningFee: 0.1, CurrentPrice: 110, PnL: 4.9},
				EntryPrice: 100, PendingAction: signalActionCloseLong,
				ActiveOrder: &Order{OrderID: 83, ClientOrderID: "close-83", Symbol: "BTCUSDT", Side: "SELL", Quantity: 0.5, Price: 110, Status: position.OrderStatusUnknown},
				Statistics:  StrategyStatistics{TotalPnL: 12, TotalVolume: 210, TotalTrades: 1, WinRate: 1},
			}
			payload, err := json.Marshal(state)
			if err != nil {
				t.Fatal(err)
			}
			store.version, store.payload, store.found = signalRuntimeStateSchemaVersion, string(payload), true

			var s Strategy
			switch strategyName {
			case "trend":
				v := NewTrendFollowingStrategy(strategyName, cfg, nil, ex, nil)
				v.SetRuntimeStateStore(store)
				s = v
			case "mean_reversion":
				v := NewMeanReversionStrategy(strategyName, cfg, nil, ex, nil)
				v.SetRuntimeStateStore(store)
				s = v
			case "momentum":
				v := NewMomentumStrategy(strategyName, cfg, nil, ex, nil)
				v.SetRuntimeStateStore(store)
				s = v
			}
			if err := s.Start(context.Background()); err != nil {
				t.Fatal(err)
			}
			positions, orders := s.GetPositions(), s.GetOrders()
			if len(positions) != 1 || positions[0].Size != 0.5 || positions[0].OpeningFee != 0.1 || len(orders) != 1 || orders[0].OrderID != 83 {
				t.Fatalf("restored position/order mismatch: positions=%+v orders=%+v", positions, orders)
			}
			if !signalOrderMatches(orders[0], &position.OrderUpdate{ClientOrderID: "close-83"}) {
				t.Fatal("restored active order lost its matching identity")
			}
		})
	}
}
