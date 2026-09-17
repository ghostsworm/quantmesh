package position

import (
	"context"
	"fmt"
	"math"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	liqTestSymbol     = "BTCUSDT"
	liqTestBid        = 49990.0
	liqTestAsk        = 50010.0
	liqTestLast       = 50000.0
	liqTestTimeout    = 10 * time.Second
	liqTestWallBudget = 2 * time.Second
	liqTestEps        = 1e-9
)

// autoClock 測試時鐘：After/Sleep 立即推進模擬時間並觸發，使輪詢等待不消耗牆鐘
type autoClock struct {
	mu  sync.Mutex
	now time.Time
}

func newAutoClock() *autoClock {
	return &autoClock{now: time.Date(2026, 9, 17, 0, 0, 0, 0, time.UTC)}
}

func (c *autoClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *autoClock) Sleep(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

func (c *autoClock) After(d time.Duration) <-chan time.Time {
	c.Sleep(d)
	ch := make(chan time.Time, 1)
	ch <- c.Now()
	return ch
}

func (c *autoClock) NewTicker(d time.Duration) Ticker { return RealClock().NewTicker(d) }

type liqFakeOrder struct {
	id         int64
	side       string
	typ        string
	qty        float64
	executed   float64
	status     string
	reduceOnly bool
	price      float64
}

// liqFakeVenue 同時扮演執行器、交易所（盤口）與 LiquidationVenue 的內存撮合
type liqFakeVenue struct {
	MockExchange
	mu sync.Mutex

	positions []float64
	orders    map[int64]*liqFakeOrder
	nextID    int64
	bid, ask  float64

	limitFillRatio float64 // 限價平倉單下單時立即成交的比例
	marketNoFill   bool    // 市價單不成交（模擬持倉無法平掉）
	uncancellable  map[int64]bool

	limitReqs  []*OrderRequest
	marketReqs []*liqFakeOrder
}

func newLiqFakeVenue(positions ...float64) *liqFakeVenue {
	return &liqFakeVenue{
		positions:     positions,
		orders:        make(map[int64]*liqFakeOrder),
		nextID:        1000,
		bid:           liqTestBid,
		ask:           liqTestAsk,
		uncancellable: make(map[int64]bool),
	}
}

// applyFill 按方向減少對應持倉條目（ReduceOnly 語義，不反向開倉）
func (v *liqFakeVenue) applyFill(side string, qty float64) float64 {
	done := 0.0
	for i, size := range v.positions {
		if qty-done < liqTestEps {
			break
		}
		if side == "SELL" && size > 0 {
			d := math.Min(size, qty-done)
			v.positions[i] -= d
			done += d
		}
		if side == "BUY" && size < 0 {
			d := math.Min(-size, qty-done)
			v.positions[i] += d
			done += d
		}
	}
	return done
}

func (v *liqFakeVenue) addOrder(o *liqFakeOrder) *liqFakeOrder {
	v.nextID++
	o.id = v.nextID
	v.orders[o.id] = o
	return o
}

// restingOrder 預置一筆與平倉無關的掛單
func (v *liqFakeVenue) restingOrder() int64 {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.addOrder(&liqFakeOrder{side: "BUY", typ: "LIMIT", qty: 1, status: "NEW"}).id
}

// ---- OrderExecutorInterface ----

func (v *liqFakeVenue) PlaceOrder(req *OrderRequest) (*Order, error) {
	res := v.BatchPlaceOrdersWithDetails([]*OrderRequest{req})
	if len(res.PlacedOrders) == 0 {
		return nil, fmt.Errorf("place failed")
	}
	return res.PlacedOrders[0], nil
}

func (v *liqFakeVenue) BatchPlaceOrders(orders []*OrderRequest) ([]*Order, bool) {
	res := v.BatchPlaceOrdersWithDetails(orders)
	return res.PlacedOrders, false
}

func (v *liqFakeVenue) BatchPlaceOrdersWithDetails(orders []*OrderRequest) *BatchPlaceOrdersResult {
	v.mu.Lock()
	defer v.mu.Unlock()
	res := &BatchPlaceOrdersResult{}
	for _, req := range orders {
		v.limitReqs = append(v.limitReqs, req)
		o := v.addOrder(&liqFakeOrder{side: req.Side, typ: "LIMIT", qty: req.Quantity, status: "NEW", reduceOnly: req.ReduceOnly, price: req.Price})
		if v.limitFillRatio > 0 {
			o.executed = v.applyFill(req.Side, req.Quantity*v.limitFillRatio)
			switch {
			case o.executed >= o.qty-liqTestEps:
				o.status = "FILLED"
			case o.executed > 0:
				o.status = "PARTIALLY_FILLED"
			}
		}
		res.PlacedOrders = append(res.PlacedOrders, &Order{
			OrderID: o.id, ClientOrderID: req.ClientOrderID, Symbol: req.Symbol,
			Side: req.Side, Price: req.Price, Quantity: req.Quantity, Status: o.status,
		})
	}
	return res
}

func (v *liqFakeVenue) BatchCancelOrders(orderIDs []int64) error {
	for _, id := range orderIDs {
		_ = v.CancelOrder(context.Background(), liqTestSymbol, id)
	}
	return nil
}

// ---- IExchange（盤口） ----

func (v *liqFakeVenue) GetOrderBook(ctx context.Context, symbol string, limit int) (*OrderBook, error) {
	return &OrderBook{
		Symbol: symbol,
		Bids:   []OrderBookLevel{{Price: v.bid, Quantity: 100}},
		Asks:   []OrderBookLevel{{Price: v.ask, Quantity: 100}},
	}, nil
}

// ---- LiquidationVenue ----

func (v *liqFakeVenue) GetOrderState(ctx context.Context, symbol string, orderID int64) (LiquidationOrderState, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	o, ok := v.orders[orderID]
	if !ok {
		return LiquidationOrderState{}, fmt.Errorf("order %d not found", orderID)
	}
	return LiquidationOrderState{Status: o.status, ExecutedQty: o.executed}, nil
}

func (v *liqFakeVenue) CancelOrder(ctx context.Context, symbol string, orderID int64) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	o, ok := v.orders[orderID]
	if !ok {
		return fmt.Errorf("order %d not found", orderID)
	}
	if v.uncancellable[orderID] {
		return fmt.Errorf("order %d cancel rejected", orderID)
	}
	if !isLiquidationTerminalStatus(o.status) {
		o.status = OrderStatusCanceled
	}
	return nil
}

func (v *liqFakeVenue) PlaceMarketOrder(ctx context.Context, symbol, side string, qty float64, reduceOnly bool) (int64, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	o := v.addOrder(&liqFakeOrder{side: side, typ: "MARKET", qty: qty, status: "NEW", reduceOnly: reduceOnly})
	v.marketReqs = append(v.marketReqs, o)
	if !v.marketNoFill {
		o.executed = v.applyFill(side, qty)
		o.status = "FILLED"
	}
	return o.id, nil
}

func (v *liqFakeVenue) GetPositionSizes(ctx context.Context, symbol string) ([]float64, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	return append([]float64(nil), v.positions...), nil
}

func (v *liqFakeVenue) GetOpenOrderIDs(ctx context.Context, symbol string) ([]int64, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	var ids []int64
	for id, o := range v.orders {
		if !isLiquidationTerminalStatus(o.status) {
			ids = append(ids, id)
		}
	}
	return ids, nil
}

func (v *liqFakeVenue) CancelAllOrders(ctx context.Context, symbol string) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	for id, o := range v.orders {
		if !v.uncancellable[id] && !isLiquidationTerminalStatus(o.status) {
			o.status = OrderStatusCanceled
		}
	}
	return nil
}

func (v *liqFakeVenue) flat() bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	return len(nonZeroSizes(v.positions)) == 0
}

func (v *liqFakeVenue) openCount() int {
	ids, _ := v.GetOpenOrderIDs(context.Background(), liqTestSymbol)
	return len(ids)
}

func newLiqTestSPM(t *testing.T, direction string, venue *liqFakeVenue) (*SuperPositionManager, *autoClock) {
	t.Helper()
	spm := newDirectionTestSPM(t, direction, venue)
	spm.exchange = venue
	spm.lastMarketPrice.Store(liqTestLast)
	clk := newAutoClock()
	spm.SetClock(clk)
	return spm, clk
}

// runVerified 執行並檢查牆鐘耗時（等待必須走注入時鐘）
func runVerified(t *testing.T, spm *SuperPositionManager, venue *liqFakeVenue) error {
	t.Helper()
	start := time.Now()
	err := spm.LiquidateAllVerified(context.Background(), venue, liqTestTimeout)
	if elapsed := time.Since(start); elapsed > liqTestWallBudget {
		t.Fatalf("LiquidateAllVerified took %s wall time with injected clock", elapsed)
	}
	return err
}

func TestLiquidateAllVerified_UnfilledLimitCancelledThenMarketResidual(t *testing.T) {
	venue := newLiqFakeVenue(1)
	venue.bid = 49000 // 盤口大幅低於現價：現價-1% 上限 49500 不穿價，必須壓到買一
	unrelated := venue.restingOrder()
	spm, clk := newLiqTestSPM(t, "LONG", venue)
	fillSlot(spm, 50000, 1, 0, "")
	simStart := clk.Now()

	if err := runVerified(t, spm, venue); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(venue.limitReqs) != 1 {
		t.Fatalf("limit close orders = %d, want 1", len(venue.limitReqs))
	}
	lim := venue.limitReqs[0]
	if lim.Side != "SELL" || !lim.ReduceOnly || lim.Price > venue.bid+liqTestEps {
		t.Fatalf("limit close must be marketable SELL ≤ bid %.2f: %+v", venue.bid, lim)
	}
	if got := clk.Now().Sub(simStart); got < liqTestTimeout/liquidationLimitPhaseDivisor {
		t.Fatalf("limit phase must wait on injected clock, advanced only %s", got)
	}
	if len(venue.marketReqs) != 1 || venue.marketReqs[0].side != "SELL" || !venue.marketReqs[0].reduceOnly ||
		math.Abs(venue.marketReqs[0].qty-1) > liqTestEps {
		t.Fatalf("want one MARKET reduceOnly SELL 1, got %+v", venue.marketReqs)
	}
	if !venue.flat() || venue.openCount() != 0 {
		t.Fatalf("flat=%v open=%d, want flat and no open orders", venue.flat(), venue.openCount())
	}
	if venue.orders[unrelated].status != OrderStatusCanceled {
		t.Fatalf("unrelated resting order must be cleaned up, status=%s", venue.orders[unrelated].status)
	}
}

func TestLiquidateAllVerified_PartialFill(t *testing.T) {
	venue := newLiqFakeVenue(1)
	venue.limitFillRatio = 0.4
	spm, _ := newLiqTestSPM(t, "LONG", venue)
	fillSlot(spm, 50000, 1, 0, "")

	if err := runVerified(t, spm, venue); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(venue.marketReqs) != 1 || math.Abs(venue.marketReqs[0].qty-0.6) > liqTestEps {
		t.Fatalf("market residual must be 0.6, got %+v", venue.marketReqs)
	}
	if !venue.flat() || venue.openCount() != 0 {
		t.Fatalf("flat=%v open=%d", venue.flat(), venue.openCount())
	}
}

func TestLiquidateAllVerified_FullFillNoMarket(t *testing.T) {
	venue := newLiqFakeVenue(1)
	venue.limitFillRatio = 1
	spm, clk := newLiqTestSPM(t, "LONG", venue)
	fillSlot(spm, 50000, 1, 0, "")
	simStart := clk.Now()

	if err := runVerified(t, spm, venue); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(venue.marketReqs) != 0 {
		t.Fatalf("no market residual expected, got %+v", venue.marketReqs)
	}
	if got := clk.Now().Sub(simStart); got >= liqTestTimeout/liquidationLimitPhaseDivisor {
		t.Fatalf("filled orders must not wait the full limit phase, advanced %s", got)
	}
}

func TestLiquidateAllVerified_Short(t *testing.T) {
	venue := newLiqFakeVenue(-1)
	spm, _ := newLiqTestSPM(t, "SHORT", venue)
	fillSlot(spm, 50000, 1, 0, "")

	if err := runVerified(t, spm, venue); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(venue.limitReqs) != 1 || venue.limitReqs[0].Side != "BUY" || venue.limitReqs[0].Price < venue.ask-liqTestEps {
		t.Fatalf("limit close must be marketable BUY ≥ ask: %+v", venue.limitReqs)
	}
	if len(venue.marketReqs) != 1 || venue.marketReqs[0].side != "BUY" || !venue.marketReqs[0].reduceOnly {
		t.Fatalf("want MARKET reduceOnly BUY, got %+v", venue.marketReqs)
	}
	if !venue.flat() || venue.openCount() != 0 {
		t.Fatalf("flat=%v open=%d", venue.flat(), venue.openCount())
	}
}

func TestLiquidateAllVerified_BothLegsClosedSeparately(t *testing.T) {
	venue := newLiqFakeVenue(2, -1) // 交易所按腿分別返回持倉條目
	spm, _ := newLiqTestSPM(t, "BOTH", venue)
	fillSlot(spm, 50000, 2, 0, PositionLegLong)
	fillSlot(spm, 50100, 1, 0, PositionLegShort)

	if err := runVerified(t, spm, venue); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var sell, buy float64
	for _, m := range venue.marketReqs {
		if !m.reduceOnly {
			t.Fatalf("market residual must be reduceOnly: %+v", m)
		}
		if m.side == "SELL" {
			sell += m.qty
		} else {
			buy += m.qty
		}
	}
	if len(venue.marketReqs) != 2 || math.Abs(sell-2) > liqTestEps || math.Abs(buy-1) > liqTestEps {
		t.Fatalf("want separate SELL 2 and BUY 1, got %+v", venue.marketReqs)
	}
	if !venue.flat() || venue.openCount() != 0 {
		t.Fatalf("flat=%v open=%d", venue.flat(), venue.openCount())
	}
}

func TestLiquidateAllVerified_BothNetPositionCappedByLeg(t *testing.T) {
	venue := newLiqFakeVenue(1) // 單向淨持倉：多 2 空 1 → 淨 +1
	spm, _ := newLiqTestSPM(t, "BOTH", venue)
	fillSlot(spm, 50000, 2, 0, PositionLegLong)
	fillSlot(spm, 50100, 1, 0, PositionLegShort)

	if err := runVerified(t, spm, venue); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(venue.marketReqs) != 1 || venue.marketReqs[0].side != "SELL" || math.Abs(venue.marketReqs[0].qty-1) > liqTestEps {
		t.Fatalf("net +1 must be closed by one SELL 1, got %+v", venue.marketReqs)
	}
	if !venue.flat() {
		t.Fatal("position must be flat")
	}
}

func TestLiquidateAllVerified_TimeoutReportsResidual(t *testing.T) {
	venue := newLiqFakeVenue(1.5)
	venue.marketNoFill = true
	stuck := venue.restingOrder()
	venue.uncancellable[stuck] = true
	spm, clk := newLiqTestSPM(t, "LONG", venue)
	fillSlot(spm, 50000, 1.5, 0, "")
	simStart := clk.Now()

	err := runVerified(t, spm, venue)
	if err == nil {
		t.Fatal("residual position must return error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "1.50000000") || !strings.Contains(msg, fmt.Sprintf("%d", stuck)) {
		t.Fatalf("error must list residual qty and stuck order id %d: %v", stuck, err)
	}
	if got := clk.Now().Sub(simStart); got < liqTestTimeout {
		t.Fatalf("verification must poll until timeout on injected clock, advanced %s", got)
	}
}

func TestLiquidateAllVerified_DoesNotCloseOtherBotsPosition(t *testing.T) {
	venue := newLiqFakeVenue(3) // 同交易對其他 Bot 持有 2
	spm, _ := newLiqTestSPM(t, "LONG", venue)
	fillSlot(spm, 50000, 1, 0, "")

	err := runVerified(t, spm, venue)
	if len(venue.marketReqs) != 1 || math.Abs(venue.marketReqs[0].qty-1) > liqTestEps {
		t.Fatalf("market residual must be capped to this bot's 1, got %+v", venue.marketReqs)
	}
	if err == nil || !strings.Contains(err.Error(), "2.00000000") {
		t.Fatalf("remaining exchange position must be reported, got %v", err)
	}
}

func TestLiquidateAllVerified_NilVenue(t *testing.T) {
	spm, _ := newLiqTestSPM(t, "LONG", newLiqFakeVenue())
	if err := spm.LiquidateAllVerified(context.Background(), nil, liqTestTimeout); err == nil {
		t.Fatal("nil venue must return error")
	}
}

func TestMarketableLiquidationPrice(t *testing.T) {
	book := liquidationBookPrices{bestBid: 100, bestAsk: 101}
	cases := []struct {
		side     string
		px, want float64
	}{
		{"SELL", 102, 100},
		{"SELL", 99, 99},
		{"BUY", 99, 101},
		{"BUY", 103, 103},
	}
	for _, tc := range cases {
		if got := marketableLiquidationPrice(tc.side, tc.px, book); got != tc.want {
			t.Fatalf("%s px=%v got %v want %v", tc.side, tc.px, got, tc.want)
		}
	}
	if got := marketableLiquidationPrice("SELL", 102, liquidationBookPrices{}); got != 102 {
		t.Fatalf("no book must keep price, got %v", got)
	}
}
