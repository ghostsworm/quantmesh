package main

import (
	"context"
	"sync"
	"testing"
	"time"

	"quantmesh/exchange"
)

// fakeCloseExchange 模擬退出平倉所需的交易所行為：
//   - 限價單：SELL 價 ≤ 市價 / BUY 價 ≥ 市價 且 !limitNeverFills 時立即成交，否則掛單（模擬 GTC 不成交）；
//   - 市價單：立即按剩餘持倉成交；
//   - 另可預置一個與平倉無關的殘留掛單（模擬網格重掛的止盈單）。
type fakeCloseExchange struct {
	exchange.IExchange
	mu              sync.Mutex
	market          float64
	position        float64
	limitNeverFills bool
	nextID          int64
	orders          map[int64]*exchange.Order
	placed          []exchange.OrderRequest
}

func newFakeCloseExchange(market, position float64) *fakeCloseExchange {
	return &fakeCloseExchange{market: market, position: position, nextID: 100, orders: map[int64]*exchange.Order{}}
}

func (f *fakeCloseExchange) GetName() string          { return "fake" }
func (f *fakeCloseExchange) GetPriceDecimals() int    { return 1 }
func (f *fakeCloseExchange) GetMarketType() string    { return "futures" }
func (f *fakeCloseExchange) GetQuantityDecimals() int { return 4 }

func (f *fakeCloseExchange) GetLatestPrice(context.Context, string) (float64, error) {
	return f.market, nil
}

func (f *fakeCloseExchange) GetPositions(_ context.Context, symbol string) ([]*exchange.Position, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return []*exchange.Position{{Symbol: symbol, Size: f.position, MarkPrice: f.market}}, nil
}

func (f *fakeCloseExchange) fill(side exchange.Side, qty float64) {
	if side == exchange.SideSell {
		f.position -= qty
	} else {
		f.position += qty
	}
}

func (f *fakeCloseExchange) PlaceOrder(_ context.Context, req *exchange.OrderRequest) (*exchange.Order, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.placed = append(f.placed, *req)
	f.nextID++
	ord := &exchange.Order{OrderID: f.nextID, ClientOrderID: req.ClientOrderID, Symbol: req.Symbol, Side: req.Side, Type: req.Type, Price: req.Price, Quantity: req.Quantity, Status: exchange.OrderStatusNew}
	marketable := req.Type == exchange.OrderTypeMarket ||
		(!f.limitNeverFills && ((req.Side == exchange.SideSell && req.Price <= f.market) || (req.Side == exchange.SideBuy && req.Price >= f.market)))
	if marketable {
		f.fill(req.Side, req.Quantity)
		ord.Status = exchange.OrderStatusFilled
		ord.ExecutedQty = req.Quantity
		ord.AvgPrice = f.market
	}
	f.orders[ord.OrderID] = ord
	return ord, nil
}

func (f *fakeCloseExchange) GetOrder(_ context.Context, _ string, id int64) (*exchange.Order, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	o := *f.orders[id]
	return &o, nil
}

func (f *fakeCloseExchange) CancelOrder(_ context.Context, _ string, id int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if o, ok := f.orders[id]; ok && o.Status == exchange.OrderStatusNew {
		o.Status = exchange.OrderStatusCanceled
	}
	return nil
}

func (f *fakeCloseExchange) CancelAllOrders(context.Context, string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, o := range f.orders {
		if o.Status == exchange.OrderStatusNew {
			o.Status = exchange.OrderStatusCanceled
		}
	}
	return nil
}

func (f *fakeCloseExchange) GetOpenOrders(context.Context, string) ([]*exchange.Order, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []*exchange.Order
	for _, o := range f.orders {
		if o.Status == exchange.OrderStatusNew {
			c := *o
			out = append(out, &c)
		}
	}
	return out, nil
}

func (f *fakeCloseExchange) GetOrderFills(context.Context, string, int64) ([]*exchange.OrderFill, error) {
	return nil, nil
}

func (*fakeCloseExchange) GetBaseAsset() string  { return "BTC" }
func (*fakeCloseExchange) GetQuoteAsset() string { return "USDT" }

func (*fakeCloseExchange) GetOrderBook(_ context.Context, symbol string, _ int) (*exchange.OrderBook, error) {
	return &exchange.OrderBook{Symbol: symbol,
		Bids: []exchange.OrderBookLevel{{Price: 99, Quantity: 10}},
		Asks: []exchange.OrderBookLevel{{Price: 101, Quantity: 10}}}, nil
}

func fastCloseOpts() shutdownCloseOptions {
	return shutdownCloseOptions{fillWait: 30 * time.Millisecond, pollInterval: 5 * time.Millisecond}
}

func TestMarketableClosePrice_NeverOutsideMarket(t *testing.T) {
	tests := []struct {
		name       string
		side       exchange.Side
		candidates []float64
		check      func(px float64) bool
	}{
		// 試跑現場：監控價過期 76363.9，市價 76348 → SELL 必須 ≤ 76348
		{"SELL 取最低參考價再下穿", exchange.SideSell, []float64{76348, 76363.9, 0}, func(px float64) bool { return px > 0 && px < 76348 }},
		{"BUY 取最高參考價再上穿", exchange.SideBuy, []float64{76348, 76363.9}, func(px float64) bool { return px > 76363.9 }},
		{"無參考價返回 0", exchange.SideSell, []float64{0, -1}, func(px float64) bool { return px == 0 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			px := marketableClosePrice(tt.side, 1, tt.candidates...)
			if !tt.check(px) {
				t.Fatalf("price=%v 不符合預期", px)
			}
		})
	}
}

func TestCloseAllPositionsMarketable(t *testing.T) {
	tests := []struct {
		name            string
		position        float64
		limitNeverFills bool
		staleMonitor    float64
		wantMarket      bool
	}{
		{"多倉：穿價限價直接成交，不需市價", 0.0014, false, 76363.9, false},
		{"空倉：穿價限價直接成交", -0.002, false, 76300, false},
		{"限價未成交：撤單後市價補平", 0.0014, true, 76363.9, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFakeCloseExchange(76348, tt.position)
			f.limitNeverFills = tt.limitNeverFills
			fail, err := closeAllPositionsMarketable(context.Background(), f, "BTCUSDT", tt.staleMonitor, fastCloseOpts())
			if err != nil || fail != 0 {
				t.Fatalf("fail=%d err=%v", fail, err)
			}
			if f.position != 0 {
				t.Fatalf("持倉未平: %v", f.position)
			}
			sawMarket := false
			for _, r := range f.placed {
				if !r.ReduceOnly {
					t.Fatalf("平倉單必須 ReduceOnly: %+v", r)
				}
				if r.Type == exchange.OrderTypeMarket {
					sawMarket = true
					continue
				}
				if r.Side == exchange.SideSell && r.Price > f.market {
					t.Fatalf("SELL 平倉價 %.2f 高於市價 %.2f", r.Price, f.market)
				}
				if r.Side == exchange.SideBuy && r.Price < f.market {
					t.Fatalf("BUY 平倉價 %.2f 低於市價 %.2f", r.Price, f.market)
				}
			}
			if sawMarket != tt.wantMarket {
				t.Fatalf("sawMarket=%v want %v", sawMarket, tt.wantMarket)
			}
			if open, _ := f.GetOpenOrders(context.Background(), "BTCUSDT"); len(open) != 0 {
				t.Fatalf("退出後仍有掛單: %d", len(open))
			}
		})
	}
}

func TestCloseAllPositionsMarketable_PreservesUnrelatedRestingOrders(t *testing.T) {
	f := newFakeCloseExchange(76348, 0.0014)
	// 未證明歸屬本次操作的掛單不能被平倉收尾掃掉。
	f.orders[1] = &exchange.Order{OrderID: 1, Side: exchange.SideSell, Price: 76796.58, Status: exchange.OrderStatusNew}
	if _, err := closeAllPositionsMarketable(context.Background(), f, "BTCUSDT", 0, fastCloseOpts()); err != nil {
		t.Fatal(err)
	}
	if open, _ := f.GetOpenOrders(context.Background(), "BTCUSDT"); len(open) != 1 || open[0].OrderID != 1 {
		t.Fatalf("無關掛單被修改: %+v", open)
	}
}

func TestCloseAllPositionsMarketable_NoPosition(t *testing.T) {
	f := newFakeCloseExchange(76348, 0)
	fail, err := closeAllPositionsMarketable(context.Background(), f, "BTCUSDT", 0, fastCloseOpts())
	if err != nil || fail != 0 || len(f.placed) != 0 {
		t.Fatalf("無持倉不應下單: fail=%d err=%v placed=%d", fail, err, len(f.placed))
	}
}
