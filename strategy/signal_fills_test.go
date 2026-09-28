package strategy

import (
	"errors"
	"math"
	"sync"
	"testing"
	"time"

	"quantmesh/position"
	"quantmesh/storage"
	"quantmesh/utils"
)

func TestSignalFillsCumulativeCloseAndCancel(t *testing.T) {
	active := &Order{OrderID: 1, Symbol: "BTCUSDT", Quantity: 2, Price: 100}
	action := signalActionOpenLong
	var holding *Position
	entry := 0.0
	stats := &StrategyStatistics{}
	apply := func(id int64, status string, qty, price float64) {
		applySignalOrderUpdate(&active, &action, &holding, &entry, stats, nil, nil, &position.OrderUpdate{OrderID: id, Status: status, ExecutedQty: qty, AvgPrice: price})
	}
	apply(1, "PARTIALLY_FILLED", 1, 100)
	apply(1, "PARTIALLY_FILLED", 0.5, 90)
	apply(1, "FILLED", 2, 110)
	if holding.Size != 2 || entry != 110 || active != nil {
		t.Fatalf("holding=%+v entry=%v active=%v", holding, entry, active)
	}
	active = &Order{OrderID: 2, Symbol: "BTCUSDT", Quantity: 2, Price: 120}
	action = signalActionCloseLong
	apply(2, "PARTIALLY_FILLED", 0.5, 120)
	apply(2, "PARTIALLY_FILLED", 0.5, 120)
	apply(2, "CANCELED", 1, 130)
	apply(2, "CANCELED", 1, 130)
	if holding.Size != 1 || stats.TotalPnL != 20 || stats.TotalTrades != 1 || active != nil {
		t.Fatalf("holding=%+v stats=%+v active=%+v", holding, stats, active)
	}
}

func TestSignalInvalidTerminalFillRetainsReconciliationState(t *testing.T) {
	active := &Order{OrderID: 1, Symbol: "BTCUSDT", Quantity: 2, Price: 100}
	action := signalActionOpenLong
	var holding *Position
	var entry float64
	stats := &StrategyStatistics{}
	applySignalOrderUpdate(&active, &action, &holding, &entry, stats, nil, nil, &position.OrderUpdate{
		OrderID: 1, Status: "PARTIALLY_FILLED", ExecutedQty: 1, AvgPrice: 100,
	})
	// Two units at cumulative average 40 contradict the previously booked 100.
	applySignalOrderUpdate(&active, &action, &holding, &entry, stats, nil, nil, &position.OrderUpdate{
		OrderID: 1, Status: "CANCELED", ExecutedQty: 2, AvgPrice: 40,
	})
	if active == nil || active.Status != position.OrderStatusUnknown || action == "" || holding.Size != 1 || entry != 100 {
		t.Fatalf("invalid terminal erased reconciliation state: active=%+v holding=%+v", active, holding)
	}
	applySignalOrderUpdate(&active, &action, &holding, &entry, stats, nil, nil, &position.OrderUpdate{
		OrderID: 1, Status: "CANCELED", ExecutedQty: 2, AvgPrice: 110,
	})
	if active != nil || holding.Size != 2 || entry != 110 {
		t.Fatalf("corrected cumulative report could not recover: active=%v holding=%+v", active, holding)
	}
}

func TestSignalFilledWithoutCumulativeQuantityRetainsOrder(t *testing.T) {
	active := &Order{OrderID: 8, Symbol: "BTCUSDT", Quantity: 2, Price: 100}
	action := signalActionOpenLong
	var holding *Position
	var entry float64
	stats := &StrategyStatistics{}
	applySignalOrderUpdate(&active, &action, &holding, &entry, stats, nil, nil, &position.OrderUpdate{OrderID: 8, Status: "FILLED"})
	if active == nil || active.Status != position.OrderStatusUnknown || action != signalActionOpenLong || holding != nil || stats.TotalVolume != 0 {
		t.Fatalf("missing fill quantity was inferred as executed: active=%+v action=%q holding=%+v stats=%+v", active, action, holding, stats)
	}
}

func TestSignalOverfillsRemainPendingForReconciliation(t *testing.T) {
	tests := []struct {
		name   string
		order  *Order
		action string
		held   float64
		filled float64
	}{
		{name: "exchange fill exceeds submitted quantity", order: &Order{OrderID: 81, Symbol: "BTCUSDT", Quantity: 1, Price: 100}, action: signalActionOpenLong, filled: 1.1},
		{name: "close exceeds strategy inventory", order: &Order{OrderID: 82, Symbol: "BTCUSDT", Quantity: 1, Price: 110}, action: signalActionCloseLong, held: 0.5, filled: 0.75},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			active, action := tt.order, tt.action
			var holding *Position
			if tt.held > 0 {
				holding = &Position{Symbol: "BTCUSDT", Size: tt.held, EntryPrice: 100}
			}
			entry := 0.0
			stats := &StrategyStatistics{}
			applySignalOrderUpdate(&active, &action, &holding, &entry, stats, nil, nil, &position.OrderUpdate{
				OrderID: tt.order.OrderID, Status: "PARTIALLY_FILLED", ExecutedQty: tt.filled, AvgPrice: 110,
			})
			if active == nil || active.Status != position.OrderStatusUnknown || action == "" || stats.TotalVolume != 0 {
				t.Fatalf("unreconciled overfill was consumed: active=%+v action=%q stats=%+v", active, action, stats)
			}
			if (tt.held == 0 && holding != nil) || (holding != nil && holding.Size != tt.held) {
				t.Fatalf("local inventory changed despite mismatch: %+v", holding)
			}
		})
	}
}

func TestSignalStaleTerminalFillRequiresReconciliation(t *testing.T) {
	active := &Order{OrderID: 83, Symbol: "BTCUSDT", Quantity: 1, Price: 100}
	action := signalActionOpenLong
	var holding *Position
	entry := 0.0
	stats := &StrategyStatistics{}
	apply := func(status string, quantity float64) {
		applySignalOrderUpdate(&active, &action, &holding, &entry, stats, nil, nil, &position.OrderUpdate{
			OrderID: 83, Status: status, ExecutedQty: quantity, AvgPrice: 100,
		})
	}
	apply("PARTIALLY_FILLED", 0.5)
	apply("NEW", 0.25)
	if active == nil || active.Status != "PARTIALLY_FILLED" || holding == nil || holding.Size != 0.5 {
		t.Fatalf("stale acknowledgement rolled signal state backward: active=%+v holding=%+v", active, holding)
	}
	apply("CANCELED", 0.25)
	if active == nil || active.Status != position.OrderStatusUnknown || holding == nil || holding.Size != 0.5 || action == "" {
		t.Fatalf("stale terminal report cleared active fill: active=%+v action=%q holding=%+v", active, action, holding)
	}
	apply("CANCELED", 0.5)
	if active != nil || holding == nil || holding.Size != 0.5 || action != "" {
		t.Fatalf("corrected terminal report did not reconcile: active=%+v action=%q holding=%+v", active, action, holding)
	}
}

func TestSignalFillWithoutAveragePriceRetainsOrder(t *testing.T) {
	active := &Order{OrderID: 9, Symbol: "BTCUSDT", Quantity: 2, Price: 100}
	action := signalActionOpenLong
	var holding *Position
	var entry float64
	stats := &StrategyStatistics{}
	applySignalOrderUpdate(&active, &action, &holding, &entry, stats, nil, nil, &position.OrderUpdate{OrderID: 9, Status: "PARTIALLY_FILLED", ExecutedQty: 1, Price: 100})
	if active == nil || active.Status != position.OrderStatusUnknown || holding != nil || stats.TotalVolume != 0 {
		t.Fatalf("missing average fill price was replaced with order price: active=%+v holding=%+v stats=%+v", active, holding, stats)
	}
	applySignalOrderUpdate(&active, &action, &holding, &entry, stats, nil, nil, &position.OrderUpdate{OrderID: 9, Status: "PARTIALLY_FILLED", ExecutedQty: 1, AvgPrice: 101})
	if active == nil || holding == nil || holding.Size != 1 || entry != 101 {
		t.Fatalf("corrected fill price did not recover the order: active=%+v holding=%+v entry=%v", active, holding, entry)
	}
}

func TestSignalNetProfitIncludesOpeningFeeAndValuedCloseFee(t *testing.T) {
	active := &Order{OrderID: 30, Symbol: "BTCUSDT", Side: "BUY", Quantity: 1, Price: 100}
	action := signalActionOpenLong
	var holding *Position
	var entry float64
	stats := &StrategyStatistics{}
	exchange := &hedgeExchange{}
	applySignalOrderUpdate(&active, &action, &holding, &entry, stats, exchange, nil, &position.OrderUpdate{
		OrderID: 30, Status: "FILLED", ExecutedQty: 1, AvgPrice: 100,
		Commission: 0.2, CommissionAsset: "USDT",
	})
	if holding == nil || holding.OpeningFee != 0.2 {
		t.Fatalf("opening fee was not attached to inventory: %+v", holding)
	}
	active = &Order{OrderID: 31, Symbol: "BTCUSDT", Side: "SELL", Quantity: 1, Price: 110}
	action = signalActionCloseLong
	applySignalOrderUpdate(&active, &action, &holding, &entry, stats, exchange, nil, &position.OrderUpdate{
		OrderID: 31, Status: "FILLED", ExecutedQty: 1, AvgPrice: 110,
		Commission: 0.1, CommissionAsset: "USDT",
	})
	if holding != nil || math.Abs(stats.TotalPnL-9.7) > 1e-9 {
		t.Fatalf("net PnL should be gross 10 less 0.2 entry and 0.1 exit fee: holding=%+v stats=%+v", holding, stats)
	}
}

func TestSignalUnknownFeeCurrencyRetainsFillForReconciliation(t *testing.T) {
	active := &Order{OrderID: 32, Symbol: "BTCUSDT", Side: "BUY", Quantity: 1, Price: 100}
	action := signalActionOpenLong
	var holding *Position
	var entry float64
	stats := &StrategyStatistics{}
	applySignalOrderUpdate(&active, &action, &holding, &entry, stats, &hedgeExchange{}, nil, &position.OrderUpdate{
		OrderID: 32, Status: "PARTIALLY_FILLED", ExecutedQty: 0.5, AvgPrice: 100,
		Commission: 0.01, CommissionAsset: "BNB",
	})
	if active == nil || active.Status != position.OrderStatusUnknown || active.FillProgress.Quantity != 0 || holding != nil {
		t.Fatalf("unknown fee currency must retain the unbooked fill: active=%+v holding=%+v", active, holding)
	}
}

func TestSignalClientIDsConcurrentAndBrokerSafe(t *testing.T) {
	var wg sync.WaitGroup
	var ids sync.Map
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 1000; i++ {
				id := signalClientOrderID("very_long_strategy_name", signalActionOpenLong)
				if _, exists := ids.LoadOrStore(id, true); exists {
					t.Errorf("duplicate ID")
				}
				if len(id) != 26 || len(utils.AddBrokerPrefix("binance", id)) > 36 || len(utils.AddBrokerPrefix("gate", id)) > 30 {
					t.Errorf("invalid ID length")
				}
			}
		}()
	}
	wg.Wait()
}

type dcaFillRecorder struct {
	pnls, fees []float64
	failures   int
	keys       map[string]bool
}

func (r *dcaFillRecorder) SaveTradeIdempotent(trade *storage.Trade) error {
	if r.keys == nil {
		r.keys = make(map[string]bool)
	}
	if r.keys[trade.ExecutionKey] {
		return nil
	}
	if err := r.SaveTrade(trade.BuyOrderID, trade.SellOrderID, trade.Exchange, trade.Symbol,
		trade.BuyPrice, trade.SellPrice, trade.Quantity, trade.PnL, trade.Fee, trade.FeeAsset, trade.CreatedAt, trade.BotID); err != nil {
		return err
	}
	r.keys[trade.ExecutionKey] = true
	return nil
}

func (r *dcaFillRecorder) SaveTrade(_, _ int64, _, _ string, _, _, _, pnl, fee float64, _ string, _ time.Time, _ string) error {
	if r.failures > 0 {
		r.failures--
		return errors.New("injected trade ledger failure")
	}
	r.pnls = append(r.pnls, pnl)
	r.fees = append(r.fees, fee)
	return nil
}

func TestDCACloseLedgerFailureRetainsFillForRetry(t *testing.T) {
	s := newR3DCA(t, &hedgeOrderExecutor{}, nil)
	defer s.Stop()
	s.layers = []*DCALayer{{Price: 100, Quantity: 1, Cost: 100, Status: entryStatusFilled}}
	s.updateTotals()
	recorder := &dcaFillRecorder{failures: 1}
	s.SetTradeStorage(recorder)
	if err := s.closeAllPositions(110, "test"); err != nil {
		t.Fatal(err)
	}
	update := &position.OrderUpdate{OrderID: s.closeOrderID, ClientOrderID: "dca-close", Status: "PARTIALLY_FILLED", ExecutedQty: 0.5, AvgPrice: 110}
	if err := s.OnOrderUpdate(update); err != nil {
		t.Fatal(err)
	}
	if !s.isClosing || s.closeProgress.Quantity != 0 || math.Abs(s.totalQty-1) > 1e-9 || s.stats.TotalTrades != 0 {
		t.Fatalf("ledger failure consumed fill: closing=%v progress=%+v qty=%v stats=%+v", s.isClosing, s.closeProgress, s.totalQty, s.stats)
	}
	if err := s.OnOrderUpdate(update); err != nil {
		t.Fatal(err)
	}
	if !s.isClosing || math.Abs(s.closeProgress.Quantity-0.5) > 1e-9 || math.Abs(s.totalQty-0.5) > 1e-9 || s.stats.TotalTrades != 1 || len(recorder.pnls) != 1 {
		t.Fatalf("replayed fill not committed exactly once: closing=%v progress=%+v qty=%v stats=%+v rows=%d", s.isClosing, s.closeProgress, s.totalQty, s.stats, len(recorder.pnls))
	}
}

func TestDCACloseRecordsOnlyIncrementalExecutionsAndActualFees(t *testing.T) {
	s := newR3DCA(t, &hedgeOrderExecutor{}, nil)
	defer s.Stop()
	s.layers = []*DCALayer{{Price: 100, Quantity: 2, Cost: 200, OpeningFee: 0.4, Status: entryStatusFilled}}
	s.updateTotals()
	recorder := &dcaFillRecorder{}
	s.SetTradeStorage(recorder)
	if err := s.closeAllPositions(130, "止盈"); err != nil {
		t.Fatal(err)
	}
	if len(recorder.pnls) != 0 || s.stats.TotalPnL != 0 {
		t.Fatal("unfilled order booked profit")
	}
	id := s.closeOrderID
	if err := s.OnOrderUpdate(&position.OrderUpdate{OrderID: id, Status: "FILLED"}); err != nil {
		t.Fatal(err)
	}
	if !s.isClosing || s.closeOrderID != id || s.totalQty != 2 || len(recorder.pnls) != 0 {
		t.Fatal("FILLED without executed quantity cleared the close or changed the ledger")
	}
	if err := s.OnOrderUpdate(&position.OrderUpdate{OrderID: id, Status: "PARTIALLY_FILLED", ExecutedQty: 0.5, Price: 110}); err != nil {
		t.Fatal(err)
	}
	if !s.isClosing || s.totalQty != 2 || len(recorder.pnls) != 0 {
		t.Fatal("fill without average price changed close state or PnL")
	}
	update := &position.OrderUpdate{OrderID: id, Status: "PARTIALLY_FILLED", ExecutedQty: 0.5, AvgPrice: 110, Commission: 0.2, CommissionAsset: "USDT"}
	if err := s.OnOrderUpdate(update); err != nil {
		t.Fatal(err)
	}
	if err := s.OnOrderUpdate(update); err != nil {
		t.Fatal(err)
	}
	update.Status, update.ExecutedQty, update.AvgPrice, update.Commission = "CANCELED", 1, 120, 0.3
	if err := s.OnOrderUpdate(update); err != nil {
		t.Fatal(err)
	}
	if err := s.OnOrderUpdate(update); err != nil {
		t.Fatal(err)
	}
	if s.totalQty != 1 || s.totalCost != 100 || math.Abs(s.layers[0].OpeningFee-0.2) > 1e-9 {
		t.Fatalf("remaining qty=%v cost=%v fee=%v", s.totalQty, s.totalCost, s.layers[0].OpeningFee)
	}
	if len(recorder.pnls) != 2 || recorder.pnls[0]+recorder.pnls[1] != 20 || math.Abs(recorder.fees[0]+recorder.fees[1]-0.7) > 1e-9 || math.Abs(s.stats.TotalPnL-19.3) > 1e-9 {
		t.Fatalf("pnl=%v fee=%v stats=%+v", recorder.pnls, recorder.fees, s.stats)
	}
}
