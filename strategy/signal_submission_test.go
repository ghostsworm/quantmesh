package strategy

import (
	"encoding/json"
	"errors"
	"math"
	"testing"

	"quantmesh/config"
	"quantmesh/execution"
	"quantmesh/position"
	"quantmesh/utils"
)

type unknownSignalExecutor struct {
	signalTestExecutor
	err error
}

func (e *unknownSignalExecutor) PlaceOrder(req *position.OrderRequest) (*position.Order, error) {
	copy := *req
	e.orders = append(e.orders, &copy)
	return nil, e.err
}

type brokerSignalExchange struct{ signalTestExchange }

func (*brokerSignalExchange) GetName() string { return "binance" }

func TestSignalUnknownRetainsActionAndAcceptsLateBrokerFill(t *testing.T) {
	for _, kind := range []string{"trend", "mean_reversion", "momentum"} {
		for _, action := range []string{signalActionOpenLong, signalActionCloseLong} {
			t.Run(kind+"/"+action, func(t *testing.T) {
				cfg := &config.Config{}
				cfg.Trading.Symbol = "BTCUSDT"
				executor := &unknownSignalExecutor{err: execution.ErrOrderUnknown}
				var s Strategy
				var submit func(string, float64) error
				if kind == "trend" {
					v := NewTrendFollowingStrategy(kind, cfg, executor, &brokerSignalExchange{}, nil)
					if action == signalActionCloseLong {
						v.position = &Position{Symbol: "BTCUSDT", Size: 1, EntryPrice: 100}
					}
					s, submit = v, v.placeSignalOrder
				} else if kind == "mean_reversion" {
					v := NewMeanReversionStrategy(kind, cfg, executor, &brokerSignalExchange{}, nil)
					if action == signalActionCloseLong {
						v.position = &Position{Symbol: "BTCUSDT", Size: 1, EntryPrice: 100}
					}
					s, submit = v, v.placeSignalOrder
				} else {
					v := NewMomentumStrategy(kind, cfg, executor, &brokerSignalExchange{}, nil)
					if action == signalActionCloseLong {
						v.position = &Position{Symbol: "BTCUSDT", Size: 1, EntryPrice: 100}
					}
					s, submit = v, v.placeSignalOrder
				}
				if err := submit(action, 110); !errors.Is(err, execution.ErrOrderUnknown) {
					t.Fatal(err)
				}
				if len(s.GetOrders()) != 1 || s.GetOrders()[0].Status != position.OrderStatusUnknown {
					t.Fatal("strategy forgot an uncertain economic action")
				}
				if err := submit(action, 110); !errors.Is(err, execution.ErrIntentPending) || len(executor.orders) != 1 {
					t.Fatalf("duplicate uncertain action submitted: %v", err)
				}
				cid := utils.AddBrokerPrefix("binance", executor.orders[0].ClientOrderID)
				update := &position.OrderUpdate{OrderID: 42, ClientOrderID: cid, Symbol: "BTCUSDT", Status: "PARTIALLY_FILLED", ExecutedQty: 0.4, AvgPrice: 110, CommissionKnown: true}
				if err := s.OnOrderUpdate(update); err != nil {
					t.Fatal(err)
				}
				if got := s.GetOrders()[0].OrderID; got != 42 {
					t.Fatalf("late order identity not bound: %v", got)
				}
				update.Status = "CANCELED"
				if err := s.OnOrderUpdate(update); err != nil {
					t.Fatal(err)
				}
				want := 0.4
				if action == signalActionCloseLong {
					want = 0.6
				}
				holdings := s.GetPositions()
				if len(holdings) != 1 || math.Abs(holdings[0].Size-want) > 1e-9 || len(s.GetOrders()) != 0 {
					t.Fatalf("late fill lost: positions=%v orders=%v", holdings, s.GetOrders())
				}
			})
		}
	}
}

type partialAckSignalExecutor struct{ signalTestExecutor }

func (e *partialAckSignalExecutor) PlaceOrder(req *position.OrderRequest) (*position.Order, error) {
	ord, err := e.signalTestExecutor.PlaceOrder(req)
	ord.Status, ord.ExecutedQty, ord.AvgPrice = "CANCELED", 0.4, 105
	return ord, err
}

type persistedIntentSignalExecutor struct {
	signalTestExecutor
	store *memoryRuntimeStateStore
}

func (e *persistedIntentSignalExecutor) PlaceOrder(req *position.OrderRequest) (*position.Order, error) {
	var state signalRuntimeState
	if !e.store.found || json.Unmarshal([]byte(e.store.payload), &state) != nil || state.ActiveOrder == nil || state.ActiveOrder.ClientOrderID != req.ClientOrderID {
		return nil, errors.New("signal intent was not durable before order submission")
	}
	return e.signalTestExecutor.PlaceOrder(req)
}

func TestSignalStrategyPersistsIntentBeforeSubmitAndFillBeforeRestart(t *testing.T) {
	store := &memoryRuntimeStateStore{}
	executor := &persistedIntentSignalExecutor{store: store}
	cfg := &config.Config{}
	cfg.Trading.BotID, cfg.Trading.Symbol = "bot-signal-persist", "BTCUSDT"
	venue := &brokerSignalExchange{}
	trend := NewTrendFollowingStrategy("trend", cfg, executor, venue, nil)
	trend.SetRuntimeStateStore(store)
	trend.mu.Lock()
	err := trend.placeSignalOrder(signalActionOpenLong, 100)
	trend.mu.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	var pending signalRuntimeState
	if err := json.Unmarshal([]byte(store.payload), &pending); err != nil || pending.ActiveOrder == nil || pending.ActiveOrder.OrderID != 1 {
		t.Fatalf("acknowledged order state not persisted: state=%+v err=%v", pending, err)
	}
	if err := trend.OnOrderUpdate(&position.OrderUpdate{
		OrderID: 1, ClientOrderID: pending.ActiveOrder.ClientOrderID, Symbol: "BTCUSDT", Side: "BUY",
		Status: "FILLED", ExecutedQty: pending.ActiveOrder.Quantity, AvgPrice: 100, Commission: 0.02, CommissionAsset: "USDT", CommissionKnown: true,
	}); err != nil {
		t.Fatal(err)
	}
	var filled signalRuntimeState
	if err := json.Unmarshal([]byte(store.payload), &filled); err != nil || filled.Position == nil || filled.Position.OpeningFee != 0.02 || filled.ActiveOrder != nil {
		t.Fatalf("filled state not durably recorded: state=%+v err=%v", filled, err)
	}
}

func TestSignalSubmissionAccountsTerminalPartialAcknowledgement(t *testing.T) {
	var active *Order
	var holding *Position
	var action string
	var entry float64
	stats := &StrategyStatistics{}
	err := submitSignalOrder(&partialAckSignalExecutor{}, "fake", &position.OrderRequest{
		Symbol: "BTCUSDT", Side: "BUY", Quantity: 1, Price: 110, ClientOrderID: "partial-ack",
	}, signalActionOpenLong, &active, &action, &holding, &entry, stats, &hedgeExchange{}, nil)
	if err != nil || active == nil || active.Status != position.OrderStatusUnknown || holding != nil {
		t.Fatalf("placement acknowledgement without fee data must await authoritative update: active=%v holding=%+v entry=%v err=%v", active, holding, entry, err)
	}
	applySignalOrderUpdate(&active, &action, &holding, &entry, stats, &hedgeExchange{}, nil, &position.OrderUpdate{
		OrderID: active.OrderID, ClientOrderID: active.ClientOrderID, Symbol: active.Symbol, Side: active.Side,
		Status: "CANCELED", ExecutedQty: 0.4, AvgPrice: 105, Commission: 0.01, CommissionAsset: "USDT", CommissionKnown: true,
	})
	if active != nil || holding == nil || holding.Size != 0.4 || entry != 105 || holding.OpeningFee != 0.01 {
		t.Fatalf("authoritative partial fill not booked: active=%v holding=%+v entry=%v err=%v", active, holding, entry, err)
	}
}

func TestSignalDeterministicRefusalDoesNotLeavePhantomPending(t *testing.T) {
	var active *Order
	var holding *Position
	var action string
	var entry float64
	err := submitSignalOrder(&unknownSignalExecutor{err: execution.ErrOpeningPaused}, "fake", &position.OrderRequest{
		Symbol: "BTCUSDT", Side: "BUY", Quantity: 1, Price: 100, ClientOrderID: "rejected",
	}, signalActionOpenLong, &active, &action, &holding, &entry, &StrategyStatistics{}, nil, nil)
	if !errors.Is(err, execution.ErrOpeningPaused) || active != nil || action != "" || holding != nil {
		t.Fatalf("deterministic refusal retained intent: active=%v action=%s err=%v", active, action, err)
	}
}
