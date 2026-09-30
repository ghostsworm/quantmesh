package strategy

import (
	"math"
	"testing"

	"quantmesh/config"
	"quantmesh/position"
)

func TestMultiStrategyExecutorClassifiesReduceAndOpenOrders(t *testing.T) {
	tests := []struct {
		name         string
		direction    string
		strategyName string
		registered   string // SetStrategyPositionSide
		req          *position.OrderRequest
		wantReduce   bool
	}{
		{
			name:         "long sell is close by default",
			direction:    "LONG",
			strategyName: "grid-long",
			req:          &position.OrderRequest{Side: "SELL"},
			wantReduce:   true,
		},
		{
			name:         "short sell opens position",
			direction:    "SHORT",
			strategyName: "grid-short",
			req:          &position.OrderRequest{Side: "SELL"},
			wantReduce:   false,
		},
		{
			name:         "short buy closes position",
			direction:    "SHORT",
			strategyName: "dca",
			req:          &position.OrderRequest{Side: "BUY"},
			wantReduce:   true,
		},
		{
			name:         "both sell can open short leg",
			direction:    "BOTH",
			strategyName: "grid-both",
			req:          &position.OrderRequest{Side: "SELL"},
			wantReduce:   false,
		},
		{
			name:         "reduce only sell always closes",
			direction:    "BOTH",
			strategyName: "grid-both",
			req:          &position.OrderRequest{Side: "SELL", ReduceOnly: true},
			wantReduce:   true,
		},
		{
			name:         "S9 strategy name containing short no longer decides",
			direction:    "LONG",
			strategyName: "futures_short",
			req:          &position.OrderRequest{Side: "SELL"},
			wantReduce:   true,
		},
		{
			name:         "registered short strategy sell opens",
			direction:    "LONG",
			strategyName: "combo",
			registered:   position.PositionSideShort,
			req:          &position.OrderRequest{Side: "SELL"},
			wantReduce:   false,
		},
		{
			name:         "registered short strategy non-reduce-only buy closes",
			direction:    "LONG",
			strategyName: "spot_short",
			registered:   position.PositionSideShort,
			req:          &position.OrderRequest{Side: "BUY"},
			wantReduce:   true,
		},
		{
			name:         "explicit request position side overrides direction",
			direction:    "LONG",
			strategyName: "combo",
			req:          &position.OrderRequest{Side: "SELL", PositionSide: position.PositionSideShort},
			wantReduce:   false,
		},
		{
			name:         "buy opens unless explicitly reduce only",
			direction:    "LONG",
			strategyName: "grid-long",
			req:          &position.OrderRequest{Side: "BUY"},
			wantReduce:   false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Trading.Direction = tc.direction
			mse := NewMultiStrategyExecutor(nil, NewCapitalAllocator(cfg, 1000))
			if tc.registered != "" {
				if err := mse.SetStrategyPositionSide(tc.strategyName, tc.registered); err != nil {
					t.Fatalf("SetStrategyPositionSide: %v", err)
				}
			}

			if got := mse.isReducePositionOrder(tc.strategyName, tc.req); got != tc.wantReduce {
				t.Fatalf("isReducePositionOrder()=%v want %v", got, tc.wantReduce)
			}
		})
	}
}

func TestSetStrategyPositionSideRejectsInvalid(t *testing.T) {
	mse := NewMultiStrategyExecutor(nil, NewCapitalAllocator(&config.Config{}, 1000))
	if err := mse.SetStrategyPositionSide("x", "BOTH"); err == nil {
		t.Fatalf("BOTH should be rejected")
	}
}

// trackTestOrder 模擬 PlaceOrder 成功後的記賬（不依賴真實交易所執行器）
func trackTestOrder(t *testing.T, mse *MultiStrategyExecutor, strategyName string, req *position.OrderRequest, orderID int64, amount float64) {
	t.Helper()
	leg, opening := mse.classifyOrder(strategyName, req)
	if opening && !mse.allocator.Reserve(strategyName, amount) {
		t.Fatalf("reserve %.2f failed", amount)
	}
	if !opening {
		amount = 0
	}
	rec := &orderCapital{strategy: strategyName, leg: leg, opening: opening, reserved: amount, quantity: req.Quantity}
	mse.mu.Lock()
	mse.bindOrderRouteLocked(orderID, req.ClientOrderID, strategyName)
	mse.trackOrderLocked(rec, orderID, req.ClientOrderID)
	mse.mu.Unlock()
}

func newCapitalTestExecutor(t *testing.T, direction string) (*MultiStrategyExecutor, *CapitalAllocator) {
	t.Helper()
	cfg := &config.Config{}
	cfg.Trading.Direction = direction
	allocator := NewCapitalAllocator(cfg, 1000)
	allocator.RegisterStrategy("dca", 1, 0)
	allocator.Allocate()
	return NewMultiStrategyExecutor(nil, allocator), allocator
}

func assertStrategyUsed(t *testing.T, allocator *CapitalAllocator, want float64) {
	t.Helper()
	if got := allocator.GetUsed("dca"); math.Abs(got-want) > 1e-9 {
		t.Fatalf("used=%.4f want %.4f", got, want)
	}
}

// D5：開倉成交後資金仍占用，DCA 多次加倉的累計敞口受分配額度約束，平倉成交後才釋放
func TestOnOrderUpdateKeepsCapitalForFilledOpeningUntilClose(t *testing.T) {
	tests := []struct {
		direction string
		openSide  string
		closeSide string
	}{
		{"LONG", "BUY", "SELL"},
		{"SHORT", "SELL", "BUY"},
	}
	for _, tc := range tests {
		t.Run(tc.direction, func(t *testing.T) {
			mse, allocator := newCapitalTestExecutor(t, tc.direction)

			trackTestOrder(t, mse, "dca", &position.OrderRequest{Side: tc.openSide, Quantity: 1, ClientOrderID: "open-1"}, 1, 100)
			mse.OnOrderUpdate(&position.OrderUpdate{OrderID: 1, ClientOrderID: "open-1", Status: "FILLED", ExecutedQty: 1})
			assertStrategyUsed(t, allocator, 100)

			trackTestOrder(t, mse, "dca", &position.OrderRequest{Side: tc.openSide, Quantity: 1, ClientOrderID: "open-2"}, 2, 100)
			mse.OnOrderUpdate(&position.OrderUpdate{OrderID: 2, ClientOrderID: "open-2", Status: "FILLED", ExecutedQty: 1})
			assertStrategyUsed(t, allocator, 200)
			if got := mse.GetPositionCapital("dca", map[string]string{"LONG": "LONG", "SHORT": "SHORT"}[tc.direction]); got != 200 {
				t.Fatalf("position capital=%.2f want 200", got)
			}
			// 重複 FILLED 推送不重複記賬
			mse.OnOrderUpdate(&position.OrderUpdate{OrderID: 2, ClientOrderID: "open-2", Status: "FILLED", ExecutedQty: 1})
			assertStrategyUsed(t, allocator, 200)

			// 平一半
			trackTestOrder(t, mse, "dca", &position.OrderRequest{Side: tc.closeSide, Quantity: 1, ReduceOnly: true, ClientOrderID: "close-1"}, 3, 0)
			mse.OnOrderUpdate(&position.OrderUpdate{OrderID: 3, ClientOrderID: "close-1", Status: "FILLED", ExecutedQty: 1})
			assertStrategyUsed(t, allocator, 100)

			// 平剩餘（分兩筆成交）
			trackTestOrder(t, mse, "dca", &position.OrderRequest{Side: tc.closeSide, Quantity: 1, ReduceOnly: true, ClientOrderID: "close-2"}, 4, 0)
			mse.OnOrderUpdate(&position.OrderUpdate{OrderID: 4, ClientOrderID: "close-2", Status: "PARTIALLY_FILLED", ExecutedQty: 0.5})
			assertStrategyUsed(t, allocator, 50)
			mse.OnOrderUpdate(&position.OrderUpdate{OrderID: 4, ClientOrderID: "close-2", Status: "FILLED", ExecutedQty: 1})
			assertStrategyUsed(t, allocator, 0)
		})
	}
}

func TestOnOrderUpdateReleasesUnfilledOnCancel(t *testing.T) {
	for _, status := range []string{"CANCELED", "EXPIRED", "REJECTED"} {
		t.Run(status, func(t *testing.T) {
			mse, allocator := newCapitalTestExecutor(t, "LONG")
			trackTestOrder(t, mse, "dca", &position.OrderRequest{Side: "BUY", Quantity: 2, ClientOrderID: "open-1"}, 10, 100)

			mse.OnOrderUpdate(&position.OrderUpdate{OrderID: 10, ClientOrderID: "open-1", Status: "PARTIALLY_FILLED", ExecutedQty: 0.5})
			assertStrategyUsed(t, allocator, 100)

			mse.OnOrderUpdate(&position.OrderUpdate{OrderID: 10, ClientOrderID: "open-1", Status: status})
			// 成交 1/4 → 保留 25 作持倉占用，釋放 75
			assertStrategyUsed(t, allocator, 25)

			// 重複撤單推送（按 OrderID 或 ClientOrderID）不重複釋放
			mse.OnOrderUpdate(&position.OrderUpdate{OrderID: 10, Status: status})
			mse.OnOrderUpdate(&position.OrderUpdate{ClientOrderID: "open-1", Status: status})
			assertStrategyUsed(t, allocator, 25)
			if mse.GetStrategyByOrderID(10) != "" || mse.GetStrategyByClientOrderID("open-1") != "" {
				t.Fatalf("routes should be cleared after terminal status")
			}
		})
	}
}

func TestOnOrderUpdateCloseCancelDoesNotRelease(t *testing.T) {
	mse, allocator := newCapitalTestExecutor(t, "LONG")
	trackTestOrder(t, mse, "dca", &position.OrderRequest{Side: "BUY", Quantity: 1, ClientOrderID: "open-1"}, 1, 100)
	mse.OnOrderUpdate(&position.OrderUpdate{OrderID: 1, ClientOrderID: "open-1", Status: "FILLED", ExecutedQty: 1})

	trackTestOrder(t, mse, "dca", &position.OrderRequest{Side: "SELL", Quantity: 1, ReduceOnly: true, ClientOrderID: "close-1"}, 2, 0)
	mse.OnOrderUpdate(&position.OrderUpdate{OrderID: 2, ClientOrderID: "close-1", Status: "CANCELED"})
	assertStrategyUsed(t, allocator, 100)
}

func TestOnOrderUpdateRejectsInvalidCumulativeFill(t *testing.T) {
	tests := []struct {
		name string
		qty  float64
	}{
		{name: "nan", qty: math.NaN()},
		{name: "positive infinity", qty: math.Inf(1)},
		{name: "negative", qty: -0.1},
		{name: "exceeds requested", qty: 1.1},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			mse, allocator := newCapitalTestExecutor(t, "LONG")
			trackTestOrder(t, mse, "dca", &position.OrderRequest{Side: "BUY", Quantity: 1, ClientOrderID: "open-invalid"}, 20, 100)

			mse.OnOrderUpdate(&position.OrderUpdate{OrderID: 20, ClientOrderID: "open-invalid", Status: "PARTIALLY_FILLED", ExecutedQty: tc.qty})

			assertStrategyUsed(t, allocator, 100)
			if got := mse.GetPositionCapital("dca", "LONG"); got != 0 {
				t.Fatalf("position capital=%v want 0", got)
			}
			if mse.GetStrategyByOrderID(20) != "dca" || mse.GetStrategyByClientOrderID("open-invalid") != "dca" {
				t.Fatal("invalid update must retain order routes for reconciliation")
			}
		})
	}
}

func TestOnOrderUpdateZeroQuantityFilledKeepsOpeningReservation(t *testing.T) {
	mse, allocator := newCapitalTestExecutor(t, "LONG")
	trackTestOrder(t, mse, "dca", &position.OrderRequest{Side: "BUY", Quantity: 1, ClientOrderID: "open-zero"}, 21, 100)

	mse.OnOrderUpdate(&position.OrderUpdate{OrderID: 21, ClientOrderID: "open-zero", Status: "FILLED", ExecutedQty: 0})

	assertStrategyUsed(t, allocator, 100)
	if got := mse.GetPositionCapital("dca", "LONG"); got != 0 {
		t.Fatalf("position capital=%v want 0", got)
	}
	if mse.GetStrategyByOrderID(21) != "dca" || mse.GetStrategyByClientOrderID("open-zero") != "dca" {
		t.Fatal("zero-quantity FILLED update must retain order routes for reconciliation")
	}
}

func TestOnOrderUpdateRegressiveFilledKeepsCapitalForReconciliation(t *testing.T) {
	mse, allocator := newCapitalTestExecutor(t, "LONG")
	trackTestOrder(t, mse, "dca", &position.OrderRequest{Side: "BUY", Quantity: 1, ClientOrderID: "open-regressive-filled"}, 22, 100)

	mse.OnOrderUpdate(&position.OrderUpdate{OrderID: 22, ClientOrderID: "open-regressive-filled", Status: "PARTIALLY_FILLED", ExecutedQty: 0.5})
	mse.OnOrderUpdate(&position.OrderUpdate{OrderID: 22, ClientOrderID: "open-regressive-filled", Status: "FILLED", ExecutedQty: 0.4})

	assertStrategyUsed(t, allocator, 100)
	if got := mse.GetPositionCapital("dca", "LONG"); got != 50 {
		t.Fatalf("position capital=%v want 50 (existing confirmed partial only)", got)
	}
	if mse.GetStrategyByOrderID(22) != "dca" || mse.GetStrategyByClientOrderID("open-regressive-filled") != "dca" {
		t.Fatal("regressive FILLED update must retain order routes for reconciliation")
	}
}
