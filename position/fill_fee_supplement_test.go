package position

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"sync"
	"testing"
	"time"

	"quantmesh/storage"
)

// detailedFill 與 exchange.OrderFill 字段名一致（上層按字段名反射讀取）
type detailedFill struct {
	Price           float64
	Quantity        float64
	Commission      float64
	CommissionAsset string
	BaseFeeQty      float64
}

// gatedFillsExchange 補查在 release 關閉前阻塞，用於模擬 REST 延遲返回
type gatedFillsExchange struct {
	MockExchange
	mu      sync.Mutex
	fills   map[int64][]*detailedFill
	release chan struct{}
	started chan struct{}
}

type failedFeeLookupExchange struct{ MockExchange }

func (failedFeeLookupExchange) GetOrderFills(context.Context, string, int64) (interface{}, error) {
	return nil, errors.New("fee history unavailable")
}

func newGatedFillsExchange(gated bool) *gatedFillsExchange {
	g := &gatedFillsExchange{fills: map[int64][]*detailedFill{}, release: make(chan struct{}), started: make(chan struct{}, 1)}
	if !gated {
		close(g.release)
	}
	return g
}

func TestOpeningFeeLookupFailureKeepsPnLUnverifiedAndBlocksOpenings(t *testing.T) {
	spm := newFillFeeSPM(t, "futures", &failedFeeLookupExchange{})
	executor := &tradeLedgerHoldTestExecutor{}
	spm.executor = executor
	spm.SetTradeStorage(&auditTradeRecorder{})
	clientOID := openBuy(spm, 71)
	spm.OnOrderUpdate(OrderUpdate{OrderID: 71, ClientOrderID: clientOID, Symbol: "ETHUSDT", Status: "FILLED", Side: "BUY",
		ExecutedQty: 0.5, AvgPrice: fillFeeTestPrice})
	waitFor(t, func() bool {
		slot := spm.getOrCreateSlot(fillFeeTestPrice)
		slot.mu.RLock()
		defer slot.mu.RUnlock()
		return slot.pendingFeeSupplementCount == 0 && slot.feeValuationUnknown
	})
	if _, verified := spm.calculateUnrealizedPnLVerified(fillFeeTestPrice * 1.1); verified {
		t.Fatal("PnL was trusted after fee lookup failure")
	}
	if !spm.OpeningGate().HasBlock("trade_ledger_unverified") || executor.ledgerCalls == 0 {
		t.Fatal("fee lookup failure did not persist an economic reconciliation hold")
	}
}

func (g *gatedFillsExchange) setFills(orderID int64, fills ...*detailedFill) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.fills[orderID] = fills
}

func (g *gatedFillsExchange) GetOrderFills(ctx context.Context, symbol string, orderID int64) (interface{}, error) {
	select {
	case g.started <- struct{}{}:
	default:
	}
	select {
	case <-g.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.fills[orderID], nil
}

type synchronizedGridStateStore struct {
	mu       sync.Mutex
	version  int
	payload  string
	found    bool
	failNext bool
}

func (s *synchronizedGridStateStore) LoadRuntimeState(string) (int, string, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.version, s.payload, s.found, nil
}

func (s *synchronizedGridStateStore) SaveRuntimeState(_ string, version int, payload string) error {
	s.mu.Lock()
	if s.failNext {
		s.failNext = false
		s.mu.Unlock()
		return errors.New("injected grid state write failure")
	}
	s.version, s.payload, s.found = version, payload, true
	s.mu.Unlock()
	return nil
}

func (s *synchronizedGridStateStore) failNextSave() {
	s.mu.Lock()
	s.failNext = true
	s.mu.Unlock()
}

// eventTradeStorage 記錄交易與更正事件
type eventTradeStorage struct {
	mu     sync.Mutex
	fees   []float64
	events []map[string]interface{}
	keys   map[string]bool
}

func (s *eventTradeStorage) SaveTradeIdempotent(trade *storage.Trade) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.keys == nil {
		s.keys = make(map[string]bool)
	}
	if s.keys[trade.ExecutionKey] {
		return nil
	}
	s.keys[trade.ExecutionKey] = true
	s.fees = append(s.fees, trade.Fee)
	return nil
}

func (s *eventTradeStorage) SaveTrade(buyOrderID, sellOrderID int64, exchange, symbol string, buyPrice, sellPrice, quantity, pnl, fee float64, feeAsset string, createdAt time.Time, botID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fees = append(s.fees, fee)
	return nil
}

func (s *eventTradeStorage) SaveTradeWithDeviation(buyOrderID, sellOrderID int64, exchange, symbol string, buyPrice, sellPrice, quantity, pnl, fee float64, feeAsset string, buyPriceDeviation, sellPriceDeviation float64, createdAt time.Time, botID string) error {
	return s.SaveTrade(buyOrderID, sellOrderID, exchange, symbol, buyPrice, sellPrice, quantity, pnl, fee, feeAsset, createdAt, botID)
}

func (s *eventTradeStorage) SaveTradeWithExchangePnL(buyOrderID, sellOrderID int64, exchange, symbol string, buyPrice, sellPrice, quantity, pnl, exchangePnL, fee float64, feeAsset string, buyPriceDeviation, sellPriceDeviation float64, createdAt time.Time, botID string) error {
	return s.SaveTrade(buyOrderID, sellOrderID, exchange, symbol, buyPrice, sellPrice, quantity, pnl, fee, feeAsset, createdAt, botID)
}

func (s *eventTradeStorage) SaveEvent(eventType string, data map[string]interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if eventType == tradeFeeCorrectionEvent {
		s.events = append(s.events, data)
	}
	return nil
}

func (s *eventTradeStorage) eventCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.events)
}

func fillOrder(spm *SuperPositionManager, orderID int64, side string, qty, fee, baseFee float64) {
	coid := spm.generateClientOrderID(fillFeeTestPrice, side, "")
	spm.OnOrderUpdate(OrderUpdate{OrderID: orderID, ClientOrderID: coid, Symbol: "ETHUSDT", Status: "NEW", Side: side, Price: fillFeeTestPrice})
	spm.OnOrderUpdate(OrderUpdate{OrderID: orderID, ClientOrderID: coid, Symbol: "ETHUSDT", Status: "FILLED", Side: side,
		ExecutedQty: qty, AvgPrice: fillFeeTestPrice, Commission: fee, CommissionAsset: "USDT", BaseFeeQty: baseFee})
}

func slotState(spm *SuperPositionManager) (qty, buyFee, avg float64, held bool) {
	slot := spm.getOrCreateSlot(fillFeeTestPrice)
	slot.mu.RLock()
	defer slot.mu.RUnlock()
	return slot.PositionQty, slot.BuyFee, slot.AvgBuyPrice, !slot.feeSupplementUntil.IsZero()
}

func sellOrdersPlaced(exec *MockExecutor) []*OrderRequest {
	var out []*OrderRequest
	for _, req := range exec.PlacedOrders {
		if req.Side == "SELL" {
			out = append(out, req)
		}
	}
	return out
}

// Bybit 現貨：推送無手續費 → 補查返回基礎幣手續費 → 扣減持倉；補查在途期間暫緩平倉單，返回後按淨數量掛單
func TestSupplementCommission_SpotBaseFeeDeductedAndCloseUsesNetQty(t *testing.T) {
	ex := newGatedFillsExchange(true)
	ex.setFills(10,
		&detailedFill{Price: fillFeeTestPrice, Quantity: 0.3, Commission: 0.36, CommissionAsset: "USDT", BaseFeeQty: 0.00012},
		&detailedFill{Price: fillFeeTestPrice, Quantity: 0.2, Commission: 0.24, CommissionAsset: "USDT", BaseFeeQty: 0.00008})
	exec := &MockExecutor{}
	spm := newFillFeeSPM(t, "spot", ex)
	spm.executor = exec
	fillOrder(spm, 10, "BUY", 0.5, 0, 0)

	if _, _, _, held := slotState(spm); !held {
		t.Fatal("補查在途時應暫緩平倉單")
	}
	spm.setAnchorPrice(fillFeeTestPrice)
	if err := spm.AdjustOrders(fillFeeTestPrice + 0.4); err != nil {
		t.Fatalf("AdjustOrders: %v", err)
	}
	if got := sellOrdersPlaced(exec); len(got) != 0 {
		t.Fatalf("補查在途不應掛平倉單: %+v", got[0])
	}

	close(ex.release)
	waitFor(t, func() bool { _, _, _, held := slotState(spm); return !held })
	qty, fee, avg, _ := slotState(spm)
	// 0.5 − 0.0002 = 0.4998 → 向下取整 3 位 = 0.499
	if math.Abs(qty-0.499) > fillFeeEps {
		t.Fatalf("PositionQty=%v want 0.499", qty)
	}
	if math.Abs(fee-0.6) > fillFeeEps {
		t.Fatalf("BuyFee=%v want 0.6", fee)
	}
	if math.Abs(avg-fillFeeTestPrice) > fillFeeEps {
		t.Fatalf("AvgBuyPrice=%v want %v", avg, fillFeeTestPrice)
	}

	spm.markAdjustDirty()
	if err := spm.AdjustOrders(fillFeeTestPrice + 0.4); err != nil {
		t.Fatalf("AdjustOrders: %v", err)
	}
	sells := sellOrdersPlaced(exec)
	if len(sells) == 0 {
		t.Fatalf("補查返回後應掛平倉單: %+v", exec.PlacedOrders)
	}
	for _, req := range sells {
		if req.Quantity > 0.499+fillFeeEps {
			t.Fatalf("平倉數量 %v 超過淨持倉 0.499", req.Quantity)
		}
	}
}

func TestFeeSupplementMarkerIsPersistedBeforeRESTLookupStarts(t *testing.T) {
	ex := newGatedFillsExchange(true)
	ex.setFills(30, &detailedFill{Price: fillFeeTestPrice, Quantity: 0.5, Commission: 0.6, CommissionAsset: "USDT"})
	spm := newFillFeeSPM(t, "futures", ex)
	spm.botID = "fee-pending-bot"
	spm.setAnchorPrice(fillFeeTestPrice)
	store := &synchronizedGridStateStore{}
	spm.SetGridRuntimeStateStore(store)

	fillOrder(spm, 30, "BUY", 0.5, 0, 0)
	select {
	case <-ex.started:
	case <-time.After(time.Second):
		t.Fatal("fee REST lookup did not start after the grid snapshot was persisted")
	}
	_, payload, found, err := store.LoadRuntimeState(gridRuntimeStateName)
	if err != nil || !found {
		t.Fatalf("load persisted grid state: found=%v err=%v", found, err)
	}
	var snapshot gridRuntimeStateSnapshot
	if err := json.Unmarshal([]byte(payload), &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Slots) != 1 || snapshot.Slots[0].PendingFeeSupplementCount != 1 {
		t.Fatalf("REST lookup started without a durable pending marker: %+v", snapshot.Slots)
	}

	close(ex.release)
	waitFor(t, func() bool {
		_, payload, found, _ := store.LoadRuntimeState(gridRuntimeStateName)
		if !found || json.Unmarshal([]byte(payload), &snapshot) != nil || len(snapshot.Slots) != 1 {
			return false
		}
		return snapshot.Slots[0].PendingFeeSupplementCount == 0
	})
	if spm.GridRuntimeStateIsVerifiedEmpty() {
		t.Fatal("a filled slot cannot be considered empty after fee supplement")
	}
}

func TestFeeSupplementDoesNotStartUntilPendingMarkerWriteSucceeds(t *testing.T) {
	ex := newGatedFillsExchange(true)
	ex.setFills(31, &detailedFill{Price: fillFeeTestPrice, Quantity: 0.5, Commission: 0.6, CommissionAsset: "USDT"})
	spm := newFillFeeSPM(t, "futures", ex)
	spm.botID = "fee-write-failure-bot"
	spm.setAnchorPrice(fillFeeTestPrice)
	store := &synchronizedGridStateStore{}
	spm.SetGridRuntimeStateStore(store)

	cid := spm.generateClientOrderID(fillFeeTestPrice, "BUY", "")
	spm.OnOrderUpdate(OrderUpdate{OrderID: 31, ClientOrderID: cid, Symbol: "ETHUSDT", Status: "NEW", Side: "BUY", Price: fillFeeTestPrice})
	store.failNextSave()
	spm.OnOrderUpdate(OrderUpdate{OrderID: 31, ClientOrderID: cid, Symbol: "ETHUSDT", Status: "FILLED", Side: "BUY",
		ExecutedQty: 0.5, AvgPrice: fillFeeTestPrice})
	select {
	case <-ex.started:
		t.Fatal("REST fee lookup started although its pending marker write failed")
	default:
	}
	if !spm.openingGate.Blocked() {
		t.Fatal("failed grid state persistence did not keep opening blocked")
	}

	// A duplicate terminal event retries persistence; only after that durable
	// write succeeds may the queued lookup start.
	spm.OnOrderUpdate(OrderUpdate{OrderID: 31, ClientOrderID: cid, Symbol: "ETHUSDT", Status: "FILLED", Side: "BUY",
		ExecutedQty: 0.5, AvgPrice: fillFeeTestPrice})
	select {
	case <-ex.started:
	case <-time.After(time.Second):
		t.Fatal("queued REST lookup did not start after durable marker retry")
	}
	close(ex.release)
}

// 推送已帶基礎幣手續費（但 Commission 為 0 仍觸發補查）時，補查不得重複扣減持倉；合約不扣減
func TestSupplementCommission_NoDoubleBaseFeeDeduction(t *testing.T) {
	tests := []struct {
		name    string
		market  string
		wsBase  float64
		wantQty float64
	}{
		// 推送扣 0.0002 → 0.4998 → 0.499；若補查再扣 0.0002 會變成 0.498
		{name: "現貨推送已扣", market: "spot", wsBase: 0.0002, wantQty: 0.499},
		{name: "現貨推送未扣由補查扣", market: "spot", wsBase: 0, wantQty: 0.499},
		{name: "合約不扣", market: "futures", wsBase: 0, wantQty: 0.5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ex := newGatedFillsExchange(false)
			ex.setFills(11, &detailedFill{Price: fillFeeTestPrice, Quantity: 0.5, Commission: 0.6, CommissionAsset: "USDT", BaseFeeQty: 0.0002})
			spm := newFillFeeSPM(t, tt.market, ex)
			fillOrder(spm, 11, "BUY", 0.5, 0, tt.wsBase)
			waitFor(t, func() bool { _, fee, _, _ := slotState(spm); return math.Abs(fee-0.6) < fillFeeEps })
			waitFor(t, func() bool { _, _, _, held := slotState(spm); return !held })
			if qty, _, _, _ := slotState(spm); math.Abs(qty-tt.wantQty) > fillFeeEps {
				t.Fatalf("PositionQty=%v want %v", qty, tt.wantQty)
			}
		})
	}
}

// 補查返回時原持倉週期已平倉並開啟新週期：手續費不得寫入新週期 BuyFee/持倉，改記更正記錄
func TestSupplementCommission_StaleCycleRoutedToCorrection(t *testing.T) {
	ex := newGatedFillsExchange(true)
	ex.setFills(20, &detailedFill{Price: fillFeeTestPrice, Quantity: 0.5, Commission: 0.6, CommissionAsset: "USDT", BaseFeeQty: 0.0002})
	store := &eventTradeStorage{}
	spm := newFillFeeSPM(t, "spot", ex)
	executor := &tradeLedgerHoldTestExecutor{}
	spm.executor = executor
	spm.SetTradeStorage(store)

	fillOrder(spm, 20, "BUY", 0.5, 0, 0)    // 推送無手續費 → 補查阻塞在途
	fillOrder(spm, 21, "SELL", 0.5, 0.5, 0) // 平倉完成 → 週期結束
	fillOrder(spm, 22, "BUY", 0.4, 0.48, 0) // 新週期開倉，推送帶手續費

	close(ex.release)
	waitFor(t, func() bool { return store.eventCount() == 1 })
	if !spm.OpeningGate().HasBlock("trade_ledger_unverified") || executor.ledgerCalls != 1 {
		t.Fatalf("stale open-leg fee correction must persist an economic reconciliation hold: gate=%v calls=%d", spm.OpeningGate().HasBlock("trade_ledger_unverified"), executor.ledgerCalls)
	}
	time.Sleep(20 * time.Millisecond)

	qty, fee, _, _ := slotState(spm)
	if math.Abs(qty-0.4) > fillFeeEps {
		t.Fatalf("新週期 PositionQty=%v want 0.4（不得被舊週期基礎幣手續費扣減）", qty)
	}
	if math.Abs(fee-0.48) > fillFeeEps {
		t.Fatalf("新週期 BuyFee=%v want 0.48（不得混入舊週期補查手續費）", fee)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	ev := store.events[0]
	if ev["leg"] != "open" || ev["order_id"] != int64(20) || math.Abs(ev["fee"].(float64)-0.6) > fillFeeEps {
		t.Fatalf("更正記錄內容錯誤: %+v", ev)
	}
}

// 平倉單推送無手續費：補查結果寫入更正記錄（交易記錄已保存，存儲無按訂單更新手續費的接口）
func TestSupplementCommission_CloseLegWritesCorrection(t *testing.T) {
	ex := newGatedFillsExchange(false)
	ex.setFills(31, &detailedFill{Price: fillFeeTestPrice + 1, Quantity: 0.5, Commission: 0.75, CommissionAsset: "USDT"})
	store := &eventTradeStorage{}
	spm := newFillFeeSPM(t, "futures", ex)
	executor := &tradeLedgerHoldTestExecutor{}
	spm.executor = executor
	spm.SetTradeStorage(store)

	fillOrder(spm, 30, "BUY", 0.5, 0.6, 0)
	fillOrder(spm, 31, "SELL", 0.5, 0, 0)
	waitFor(t, func() bool { return store.eventCount() == 1 })
	if !spm.OpeningGate().HasBlock("trade_ledger_unverified") || executor.ledgerCalls != 1 {
		t.Fatalf("close-leg fee correction must persist an economic reconciliation hold: gate=%v calls=%d", spm.OpeningGate().HasBlock("trade_ledger_unverified"), executor.ledgerCalls)
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	ev := store.events[0]
	if ev["leg"] != "close" || ev["order_id"] != int64(31) || math.Abs(ev["fee"].(float64)-0.75) > fillFeeEps {
		t.Fatalf("更正記錄內容錯誤: %+v", ev)
	}
	if len(store.fees) != 1 || math.Abs(store.fees[0]-0.6) > fillFeeEps {
		t.Fatalf("交易記錄手續費=%v want [0.6]", store.fees)
	}
	if _, fee, _, _ := slotState(spm); fee != 0 {
		t.Fatalf("平倉完成後 BuyFee=%v want 0", fee)
	}
}

func TestSummarizeFills(t *testing.T) {
	sum, n := summarizeFills([]*detailedFill{
		{Price: 100, Quantity: 1, Commission: 0.1, CommissionAsset: "USDT", BaseFeeQty: 0.001},
		nil,
		{Price: 200, Quantity: 1, Commission: 0.2, CommissionAsset: "USDT"},
	})
	if n != 2 || math.Abs(sum.commission-0.3) > fillFeeEps || math.Abs(sum.baseFeeQty-0.001) > fillFeeEps ||
		math.Abs(sum.notional/sum.qty-150) > fillFeeEps || sum.asset != "USDT" {
		t.Fatalf("unexpected %+v n=%d", sum, n)
	}
	sum, n = summarizeFills([]interface{}{map[string]interface{}{"Commission": "0.5", "CommissionAsset": "BNB", "BaseFeeQty": 0.002, "Price": 10.0, "Quantity": 2.0}})
	if n != 1 || sum.commission != 0 || sum.valuationKnown || sum.asset != "BNB" || sum.baseFeeQty != 0.002 || sum.notional != 20 {
		t.Fatalf("map fill unexpected %+v", sum)
	}
}
