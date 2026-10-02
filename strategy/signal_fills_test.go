package strategy

import (
	"context"
	"errors"
	"math"
	"sync"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/exchange"
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
		applySignalOrderUpdate(&active, &action, &holding, &entry, stats, nil, nil, &position.OrderUpdate{OrderID: id, Status: status, ExecutedQty: qty, AvgPrice: price, CommissionKnown: true})
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

func TestSignalStrategiesSerializeFillFeeVerificationAndAccounting(t *testing.T) {
	tests := []struct {
		name string
		new  func(position.IExchange) (func(*position.OrderUpdate) error, func() (*Position, float64, *Order))
	}{
		{name: "mean reversion", new: func(ex position.IExchange) (func(*position.OrderUpdate) error, func() (*Position, float64, *Order)) {
			s := NewMeanReversionStrategy("mean", &config.Config{}, &hedgeOrderExecutor{}, ex, nil)
			s.SetRuntimeStateStore(&memoryRuntimeStateStore{})
			s.activeOrder, s.pendingAction = &Order{OrderID: 122, Symbol: "BTCUSDT", Side: "BUY", Quantity: 1,
				Status: position.OrderStatusPartiallyFilled}, signalActionOpenLong
			return s.OnOrderUpdate, func() (*Position, float64, *Order) { return s.position, s.entryPrice, s.activeOrder }
		}},
		{name: "momentum", new: func(ex position.IExchange) (func(*position.OrderUpdate) error, func() (*Position, float64, *Order)) {
			s := NewMomentumStrategy("momentum", &config.Config{}, &hedgeOrderExecutor{}, ex, nil)
			s.SetRuntimeStateStore(&memoryRuntimeStateStore{})
			s.activeOrder, s.pendingAction = &Order{OrderID: 122, Symbol: "BTCUSDT", Side: "BUY", Quantity: 1,
				Status: position.OrderStatusPartiallyFilled}, signalActionOpenLong
			return s.OnOrderUpdate, func() (*Position, float64, *Order) { return s.position, s.entryPrice, s.activeOrder }
		}},
		{name: "trend following", new: func(ex position.IExchange) (func(*position.OrderUpdate) error, func() (*Position, float64, *Order)) {
			s := NewTrendFollowingStrategy("trend", &config.Config{}, &hedgeOrderExecutor{}, ex, nil)
			s.SetRuntimeStateStore(&memoryRuntimeStateStore{})
			s.activeOrder, s.pendingAction = &Order{OrderID: 122, Symbol: "BTCUSDT", Side: "BUY", Quantity: 1,
				Status: position.OrderStatusPartiallyFilled}, signalActionOpenLong
			return s.OnOrderUpdate, func() (*Position, float64, *Order) { return s.position, s.entryPrice, s.activeOrder }
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
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
						{OrderID: 122, TradeID: "signal-cursor-a", Symbol: "BTCUSDT", Side: exchange.SideBuy, Price: 100, Quantity: 0.5,
							Commission: 0.05, CommissionAsset: "USDT", TradeTime: 1_700_000_000_000},
						{OrderID: 122, TradeID: "signal-cursor-b", Symbol: "BTCUSDT", Side: exchange.SideBuy, Price: 100, Quantity: 0.5,
							Commission: 0.05, CommissionAsset: "USDT", TradeTime: 1_700_000_000_001},
					}, nil
				}
				close(secondFillQuery)
				return []*exchange.OrderFill{{OrderID: 122, TradeID: "signal-cursor-a", Symbol: "BTCUSDT", Side: exchange.SideBuy, Price: 100,
					Quantity: 0.5, Commission: 0.05, CommissionAsset: "USDT", TradeTime: 1_700_000_000_000}}, nil
			}
			onUpdate, state := tt.new(ex)
			fullUpdateDone := make(chan error, 1)
			partialUpdateDone := make(chan error, 1)
			go func() {
				fullUpdateDone <- onUpdate(&position.OrderUpdate{OrderID: 122, Symbol: "BTCUSDT", Side: "BUY",
					Status: position.OrderStatusFilled, ExecutedQty: 1, AvgPrice: 100, CommissionKnown: false})
			}()
			<-firstFillQuery
			go func() {
				partialUpdateDone <- onUpdate(&position.OrderUpdate{OrderID: 122, Symbol: "BTCUSDT", Side: "BUY",
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
				// A serialized callback waits until the first update advances or closes the order.
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
					t.Fatal("concurrent signal strategy update did not complete")
				}
			}
			holding, entryPrice, active := state()
			if holding == nil || holding.Size != 1 || math.Abs(holding.OpeningFee-0.1) > 1e-12 || entryPrice != 100 || active != nil {
				t.Fatalf("concurrent signal fill was misaccounted: holding=%+v entry=%v active=%+v", holding, entryPrice, active)
			}
		})
	}
}

func TestSignalInvalidTerminalFillRetainsReconciliationState(t *testing.T) {
	active := &Order{OrderID: 1, Symbol: "BTCUSDT", Quantity: 2, Price: 100}
	action := signalActionOpenLong
	var holding *Position
	var entry float64
	stats := &StrategyStatistics{}
	applySignalOrderUpdate(&active, &action, &holding, &entry, stats, nil, nil, &position.OrderUpdate{
		OrderID: 1, Status: "PARTIALLY_FILLED", ExecutedQty: 1, AvgPrice: 100, CommissionKnown: true,
	})
	// Two units at cumulative average 40 contradict the previously booked 100.
	applySignalOrderUpdate(&active, &action, &holding, &entry, stats, nil, nil, &position.OrderUpdate{
		OrderID: 1, Status: "CANCELED", ExecutedQty: 2, AvgPrice: 40, CommissionKnown: true,
	})
	if active == nil || active.Status != position.OrderStatusUnknown || action == "" || holding.Size != 1 || entry != 100 {
		t.Fatalf("invalid terminal erased reconciliation state: active=%+v holding=%+v", active, holding)
	}
	applySignalOrderUpdate(&active, &action, &holding, &entry, stats, nil, nil, &position.OrderUpdate{
		OrderID: 1, Status: "CANCELED", ExecutedQty: 2, AvgPrice: 110, CommissionKnown: true,
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

func TestSignalUnderfilledFilledRetainsOrderAndRecoversAfterVerification(t *testing.T) {
	for _, actionName := range []string{signalActionOpenLong, signalActionCloseLong} {
		t.Run(actionName, func(t *testing.T) {
			active := &Order{OrderID: 84, Symbol: "BTCUSDT", Quantity: 1, Price: 100}
			action := actionName
			var holding *Position
			if actionName == signalActionCloseLong {
				holding = &Position{Symbol: "BTCUSDT", Size: 1, EntryPrice: 90}
			}
			entry := 0.0
			stats := &StrategyStatistics{}
			executor := &signalReconciliationExecutor{}
			apply := func(quantity float64) {
				applySignalOrderUpdate(&active, &action, &holding, &entry, stats, nil, executor, &position.OrderUpdate{
					OrderID: 84, Status: "FILLED", ExecutedQty: quantity, AvgPrice: 100, CommissionKnown: true,
				})
			}

			apply(0.4)
			if active == nil || active.Status != position.OrderStatusUnknown || action != actionName || active.FillProgress.Quantity != 0.4 || executor.calls != 1 {
				t.Fatalf("underfilled FILLED erased order state: active=%+v action=%q", active, action)
			}
			if actionName == signalActionOpenLong && (holding == nil || holding.Size != 0.4) {
				t.Fatalf("verified partial opening fill was not accounted: %+v", holding)
			}
			if actionName == signalActionCloseLong && (holding == nil || math.Abs(holding.Size-0.6) > entryQtyEpsilon) {
				t.Fatalf("verified partial closing fill was not accounted: %+v", holding)
			}

			apply(1)
			if active != nil || action != "" || executor.calls != 1 {
				t.Fatalf("fully verified cumulative fill did not settle: active=%+v action=%q", active, action)
			}
			if actionName == signalActionOpenLong && (holding == nil || holding.Size != 1) {
				t.Fatalf("opening fill remainder not applied: %+v", holding)
			}
			if actionName == signalActionCloseLong && holding != nil {
				t.Fatalf("closing fill remainder not applied: %+v", holding)
			}
		})
	}
}

type signalReconciliationExecutor struct {
	position.OrderExecutorInterface
	calls int
}

func (e *signalReconciliationExecutor) MarkOrderReconciliationRequired(int64, string, string) error {
	e.calls++
	return nil
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
				OrderID: tt.order.OrderID, Status: "PARTIALLY_FILLED", ExecutedQty: tt.filled, AvgPrice: 110, CommissionKnown: true,
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
			OrderID: 83, Status: status, ExecutedQty: quantity, AvgPrice: 100, CommissionKnown: true,
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
	applySignalOrderUpdate(&active, &action, &holding, &entry, stats, nil, nil, &position.OrderUpdate{OrderID: 9, Status: "PARTIALLY_FILLED", ExecutedQty: 1, Price: 100, CommissionKnown: true})
	if active == nil || active.Status != position.OrderStatusUnknown || holding != nil || stats.TotalVolume != 0 {
		t.Fatalf("missing average fill price was replaced with order price: active=%+v holding=%+v stats=%+v", active, holding, stats)
	}
	applySignalOrderUpdate(&active, &action, &holding, &entry, stats, nil, nil, &position.OrderUpdate{OrderID: 9, Status: "PARTIALLY_FILLED", ExecutedQty: 1, AvgPrice: 101, CommissionKnown: true})
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
		Commission: 0.2, CommissionAsset: "USDT", CommissionKnown: true,
	})
	if holding == nil || holding.OpeningFee != 0.2 {
		t.Fatalf("opening fee was not attached to inventory: %+v", holding)
	}
	active = &Order{OrderID: 31, Symbol: "BTCUSDT", Side: "SELL", Quantity: 1, Price: 110}
	action = signalActionCloseLong
	applySignalOrderUpdate(&active, &action, &holding, &entry, stats, exchange, nil, &position.OrderUpdate{
		OrderID: 31, Status: "FILLED", ExecutedQty: 1, AvgPrice: 110,
		Commission: 0.1, CommissionAsset: "USDT", CommissionKnown: true,
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

func TestSignalZeroFeePlaceholderDoesNotBookFill(t *testing.T) {
	active := &Order{OrderID: 33, Symbol: "BTCUSDT", Side: "BUY", Quantity: 1, Price: 100}
	action := signalActionOpenLong
	var holding *Position
	entry := 0.0
	stats := &StrategyStatistics{}
	applySignalOrderUpdate(&active, &action, &holding, &entry, stats, &hedgeExchange{}, nil, &position.OrderUpdate{
		OrderID: 33, Status: "PARTIALLY_FILLED", ExecutedQty: 0.5, AvgPrice: 100,
	})
	if active == nil || active.Status != position.OrderStatusUnknown || active.FillProgress.Quantity != 0 || holding != nil || stats.TotalVolume != 0 {
		t.Fatalf("unauthenticated zero-fee placeholder was booked: active=%+v holding=%+v stats=%+v", active, holding, stats)
	}
}

type signalFeeEvidenceExchange struct {
	signalTestExchange
	fills []*exchange.OrderFill
	err   error
}

func (e *signalFeeEvidenceExchange) GetOrderFills(context.Context, string, int64) (interface{}, error) {
	return e.fills, e.err
}

func TestSignalLiveFillRequiresAndUsesAuthoritativeFillHistory(t *testing.T) {
	venue := &signalFeeEvidenceExchange{fills: []*exchange.OrderFill{{
		OrderID: 42, TradeID: "trade-42", Symbol: "BTCUSDT", Side: exchange.SideBuy,
		Price: 100, Quantity: 0.5, CommissionQuoteKnown: true, CommissionQuote: 0,
	}}}
	strategy := NewTrendFollowingStrategy("trend", &config.Config{}, &signalTestExecutor{}, venue, nil)
	setTestRuntimeStateStore(t, strategy)
	strategy.activeOrder = &Order{OrderID: 42, ClientOrderID: "signal-42", Symbol: "BTCUSDT", Side: "BUY", Quantity: 1, Price: 100, Status: position.OrderStatusUnknown}
	strategy.pendingAction = signalActionOpenLong
	update := &position.OrderUpdate{OrderID: 42, ClientOrderID: "signal-42", Symbol: "BTCUSDT", Side: "BUY",
		Status: "PARTIALLY_FILLED", ExecutedQty: 0.5, AvgPrice: 100}
	if err := strategy.OnOrderUpdate(update); err != nil {
		t.Fatalf("authoritative fill history was not applied: %v", err)
	}
	if !update.CommissionKnown || update.CommissionAsset != "USDT" || strategy.position == nil ||
		strategy.position.Size != 0.5 || strategy.activeOrder == nil || strategy.activeOrder.FillProgress.Quantity != 0.5 {
		t.Fatalf("verified fill was not accounted exactly once: update=%+v position=%+v active=%+v",
			update, strategy.position, strategy.activeOrder)
	}
}

func TestSignalMissingFeeHistoryRetainsAndLocksLiveFill(t *testing.T) {
	venue := &signalFeeEvidenceExchange{}
	executor := &signalReconciliationExecutor{}
	strategy := NewTrendFollowingStrategy("trend", &config.Config{}, executor, venue, nil)
	setTestRuntimeStateStore(t, strategy)
	strategy.activeOrder = &Order{OrderID: 43, ClientOrderID: "signal-43", Symbol: "BTCUSDT", Side: "BUY", Quantity: 1, Price: 100}
	strategy.pendingAction = signalActionOpenLong
	err := strategy.OnOrderUpdate(&position.OrderUpdate{OrderID: 43, ClientOrderID: "signal-43", Symbol: "BTCUSDT", Side: "BUY",
		Status: "PARTIALLY_FILLED", ExecutedQty: 0.5, AvgPrice: 100})
	if err == nil || executor.calls != 1 || strategy.activeOrder == nil || strategy.activeOrder.Status != position.OrderStatusUnknown ||
		strategy.activeOrder.FillProgress.Quantity != 0 || strategy.position != nil {
		t.Fatalf("missing fee evidence did not fail closed: err=%v marked=%v active=%+v position=%+v",
			err, executor.calls, strategy.activeOrder, strategy.position)
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
	feeAssets  []string
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

func (r *dcaFillRecorder) SaveTrade(_, _ int64, _, _ string, _, _, _, pnl, fee float64, feeAsset string, _ time.Time, _ string) error {
	if r.failures > 0 {
		r.failures--
		return errors.New("injected trade ledger failure")
	}
	r.pnls = append(r.pnls, pnl)
	r.fees = append(r.fees, fee)
	r.feeAssets = append(r.feeAssets, feeAsset)
	return nil
}

func TestDCACloseBaseCommissionWithoutInventoryFeeQuantityFailsClosed(t *testing.T) {
	s := newR3DCA(t, &hedgeOrderExecutor{}, nil)
	defer s.Stop()
	s.layers = []*DCALayer{{Price: 100, Quantity: 1, Cost: 100, OpeningFee: 0.2, Status: entryStatusFilled}}
	s.updateTotals()
	recorder := &dcaFillRecorder{}
	s.SetTradeStorage(recorder)
	if err := s.closeAllPositions(110, "test"); err != nil {
		t.Fatal(err)
	}
	if err := s.OnOrderUpdate(&position.OrderUpdate{
		OrderID: s.closeOrderID, Status: "PARTIALLY_FILLED", ExecutedQty: 1, AvgPrice: 110,
		Commission: 0.001, CommissionAsset: "BTC", CommissionKnown: true,
	}); err != nil {
		t.Fatal(err)
	}
	if len(recorder.fees) != 0 || s.totalQty != 1 || s.closeProgress.Quantity != 0 || !s.isClosing {
		t.Fatalf("unmapped base fee must not settle a close: fees=%v qty=%v progress=%+v closing=%v",
			recorder.fees, s.totalQty, s.closeProgress, s.isClosing)
	}
	if len(recorder.feeAssets) != 0 {
		t.Fatalf("unmapped base fee must not be recorded in the trade ledger: %v", recorder.feeAssets)
	}
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
	update := &position.OrderUpdate{OrderID: s.closeOrderID, ClientOrderID: s.closeClientOrderID, Status: "PARTIALLY_FILLED", ExecutedQty: 0.5, AvgPrice: 110, CommissionKnown: true}
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
	update := &position.OrderUpdate{OrderID: id, Status: "PARTIALLY_FILLED", ExecutedQty: 0.5, AvgPrice: 110, Commission: 0.2, CommissionAsset: "USDT", CommissionKnown: true}
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
