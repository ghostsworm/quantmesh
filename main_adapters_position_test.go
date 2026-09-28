package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"quantmesh/exchange"
	"quantmesh/lock"
	"quantmesh/order"
	"quantmesh/position"
)

type lockedExecutorExchange struct {
	exchange.IExchange
	placed int
}

func (f *lockedExecutorExchange) GetName() string       { return "fake" }
func (f *lockedExecutorExchange) GetMarketType() string { return "futures" }
func (f *lockedExecutorExchange) PlaceOrder(ctx context.Context, req *exchange.OrderRequest) (*exchange.Order, error) {
	f.placed++
	return &exchange.Order{OrderID: int64(f.placed), ClientOrderID: req.ClientOrderID, Status: exchange.OrderStatusNew}, nil
}

type alwaysBusyLock struct {
	lock.DistributedLock
}

func (alwaysBusyLock) TryLock(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	return false, nil
}

// TestExchangeExecutorAdapterLockSkippedIsNilSafe 分布式鎖未獲取時適配器不得解引用 nil 订單
func TestExchangeExecutorAdapterLockSkippedIsNilSafe(t *testing.T) {
	tests := []struct {
		name string
		lock lock.DistributedLock
		want int // 期望成功订單數
	}{
		{name: "lock busy", lock: alwaysBusyLock{}, want: 0},
		// The first CID was already submitted above; batching it again must not
		// create another physical order. Only the second, new CID is accepted.
		{name: "lock free", lock: lock.NewNopLock(), want: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ex := &lockedExecutorExchange{}
			a := &exchangeExecutorAdapter{
				executor: order.NewExchangeOrderExecutor(ex, "BTCUSDT", 0, 0, tt.lock, ""),
				symbol:   "BTCUSDT",
				exchange: "fake",
			}
			reqs := []*position.OrderRequest{
				{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1, ClientOrderID: "a"},
				{Symbol: "BTCUSDT", Side: "BUY", Price: 50, Quantity: 1, ClientOrderID: "b"},
			}

			ord, err := a.PlaceOrder(reqs[0])
			if tt.want == 0 {
				if ord != nil || !errors.Is(err, order.ErrLockNotAcquired) {
					t.Fatalf("PlaceOrder = %#v, %v; want nil, ErrLockNotAcquired", ord, err)
				}
			} else if err != nil || ord == nil {
				t.Fatalf("PlaceOrder = %#v, %v; want order", ord, err)
			}

			res := a.BatchPlaceOrdersWithDetails(reqs)
			if len(res.PlacedOrders) != tt.want {
				t.Fatalf("PlacedOrders len = %d, want %d", len(res.PlacedOrders), tt.want)
			}
			for i, o := range res.PlacedOrders {
				if o == nil {
					t.Fatalf("PlacedOrders[%d] is nil", i)
				}
			}
			if tt.want > 0 && ex.placed != 2 {
				t.Fatalf("duplicate CID reached venue: %d physical orders, want 2", ex.placed)
			}
		})
	}
}
