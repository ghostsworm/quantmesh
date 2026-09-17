package position

import (
	"context"
	"math"
	"sync/atomic"
	"testing"
	"time"

	"quantmesh/config"
)

const (
	fillFeeTestPrice = 3000.0
	fillFeeEps       = 1e-9
)

type fakeFill struct {
	Commission      float64
	CommissionAsset string
}

// fillsExchange 返回具體類型切片（與真實適配層 []*exchange.OrderFill 一致）並統計補查次數
type fillsExchange struct {
	MockExchange
	fills []*fakeFill
	calls atomic.Int64
}

func (f *fillsExchange) GetOrderFills(ctx context.Context, symbol string, orderID int64) (interface{}, error) {
	f.calls.Add(1)
	return f.fills, nil
}

func newFillFeeSPM(t *testing.T, marketType string, ex IExchange) *SuperPositionManager {
	t.Helper()
	cfg := &config.Config{}
	cfg.Trading.Symbol = "ETHUSDT"
	cfg.Trading.Direction = "LONG"
	cfg.Trading.MarketType = marketType
	cfg.Trading.PriceInterval = 1
	cfg.Trading.ProfitSpread = 1
	cfg.Trading.SellWindowSize = 5
	cfg.Trading.OrderQuantity = 100
	if ex == nil {
		ex = &MockExchange{}
	}
	return NewSuperPositionManager(cfg, &MockExecutor{}, ex, 2, 3)
}

func openBuy(spm *SuperPositionManager, orderID int64) string {
	coid := spm.generateClientOrderID(fillFeeTestPrice, "BUY", "")
	spm.OnOrderUpdate(OrderUpdate{OrderID: orderID, ClientOrderID: coid, Symbol: "ETHUSDT", Status: "NEW", Side: "BUY", Price: fillFeeTestPrice})
	return coid
}

func slotSnapshot(spm *SuperPositionManager) (qty, buyFee float64) {
	slot := spm.getOrCreateSlot(fillFeeTestPrice)
	slot.mu.RLock()
	defer slot.mu.RUnlock()
	return slot.PositionQty, slot.BuyFee
}

func TestOnOrderUpdate_PartialFillCommissionAccumulates(t *testing.T) {
	for _, market := range []string{"futures", "spot"} {
		t.Run(market, func(t *testing.T) {
			spm := newFillFeeSPM(t, market, nil)
			coid := openBuy(spm, 1)
			up := func(status string, exec, fee float64) {
				spm.OnOrderUpdate(OrderUpdate{OrderID: 1, ClientOrderID: coid, Symbol: "ETHUSDT", Status: status, Side: "BUY",
					ExecutedQty: exec, AvgPrice: fillFeeTestPrice, Commission: fee, CommissionAsset: "USDT"})
			}
			up("PARTIALLY_FILLED", 0.1, 0.01)
			up("PARTIALLY_FILLED", 0.1, 0.01) // 重複推送：增量為 0，不得重複累加
			up("PARTIALLY_FILLED", 0.2, 0.02)
			up("PARTIALLY_FILLED", 0.3, 0.03)
			up("FILLED", 0.5, 0.04)
			up("FILLED", 0.5, 0.04) // 完全成交後的重放推送：持倉與手續費都不得再記

			qty, fee := slotSnapshot(spm)
			if math.Abs(qty-0.5) > fillFeeEps {
				t.Fatalf("PositionQty=%v want 0.5", qty)
			}
			if want := 0.01 + 0.02 + 0.03 + 0.04; math.Abs(fee-want) > fillFeeEps {
				t.Fatalf("BuyFee=%v want %v", fee, want)
			}
			if got := spm.totalBuyQty.Load().(float64); math.Abs(got-0.5) > fillFeeEps {
				t.Fatalf("totalBuyQty=%v want 0.5", got)
			}
		})
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met before deadline")
}

// Bybit 路徑：推送不帶手續費 → 訂單結束時補查一次；推送已帶手續費 → 不補查，避免重複累加
func TestOnOrderUpdate_SupplementCommissionOnlyWhenPushHasNone(t *testing.T) {
	t.Run("推送無手續費時補查一次", func(t *testing.T) {
		ex := &fillsExchange{fills: []*fakeFill{{Commission: 0.2, CommissionAsset: "USDT"}, nil, {Commission: 0.3, CommissionAsset: "USDT"}}}
		spm := newFillFeeSPM(t, "spot", ex)
		coid := openBuy(spm, 2)
		for _, s := range []struct {
			status string
			exec   float64
		}{{"PARTIALLY_FILLED", 0.2}, {"PARTIALLY_FILLED", 0.4}, {"FILLED", 0.5}} {
			spm.OnOrderUpdate(OrderUpdate{OrderID: 2, ClientOrderID: coid, Symbol: "ETHUSDT", Status: s.status, Side: "BUY",
				ExecutedQty: s.exec, AvgPrice: fillFeeTestPrice, CommissionAsset: "USDT"})
		}
		waitFor(t, func() bool { _, fee := slotSnapshot(spm); return math.Abs(fee-0.5) < fillFeeEps })
		time.Sleep(20 * time.Millisecond)
		if n := ex.calls.Load(); n != 1 {
			t.Fatalf("GetOrderFills calls=%d want 1", n)
		}
	})

	t.Run("推送已帶手續費不補查", func(t *testing.T) {
		ex := &fillsExchange{fills: []*fakeFill{{Commission: 9, CommissionAsset: "USDT"}}}
		spm := newFillFeeSPM(t, "spot", ex)
		coid := openBuy(spm, 3)
		spm.OnOrderUpdate(OrderUpdate{OrderID: 3, ClientOrderID: coid, Symbol: "ETHUSDT", Status: "PARTIALLY_FILLED", Side: "BUY",
			ExecutedQty: 0.2, AvgPrice: fillFeeTestPrice, Commission: 0.1, CommissionAsset: "USDT"})
		// 最後一筆推送手續費為 0（例如返佣/精度），不應觸發按整單匯總的補查
		spm.OnOrderUpdate(OrderUpdate{OrderID: 3, ClientOrderID: coid, Symbol: "ETHUSDT", Status: "FILLED", Side: "BUY",
			ExecutedQty: 0.5, AvgPrice: fillFeeTestPrice, CommissionAsset: "USDT"})
		time.Sleep(50 * time.Millisecond)
		if n := ex.calls.Load(); n != 0 {
			t.Fatalf("GetOrderFills calls=%d want 0", n)
		}
		if _, fee := slotSnapshot(spm); math.Abs(fee-0.1) > fillFeeEps {
			t.Fatalf("BuyFee=%v want 0.1", fee)
		}
	})
}

func TestOnOrderUpdate_SpotBaseFeeReducesPosition(t *testing.T) {
	tests := []struct {
		name    string
		market  string
		wantQty float64
	}{
		// 0.3 + 0.2 成交，基礎幣手續費 0.0003 + 0.0002 → 0.4995，向下取整到 3 位 → 0.499
		{name: "現貨扣除基礎幣手續費並向下取整", market: "spot", wantQty: 0.499},
		{name: "合約不受影響", market: "futures", wantQty: 0.5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exec := &MockExecutor{}
			spm := newFillFeeSPM(t, tt.market, nil)
			spm.executor = exec
			coid := openBuy(spm, 4)
			spm.OnOrderUpdate(OrderUpdate{OrderID: 4, ClientOrderID: coid, Symbol: "ETHUSDT", Status: "PARTIALLY_FILLED", Side: "BUY",
				ExecutedQty: 0.3, AvgPrice: fillFeeTestPrice, Commission: 0.9, CommissionAsset: "USDT", BaseFeeQty: 0.0003})
			spm.OnOrderUpdate(OrderUpdate{OrderID: 4, ClientOrderID: coid, Symbol: "ETHUSDT", Status: "FILLED", Side: "BUY",
				ExecutedQty: 0.5, AvgPrice: fillFeeTestPrice, Commission: 0.6, CommissionAsset: "USDT", BaseFeeQty: 0.0002})

			qty, fee := slotSnapshot(spm)
			if math.Abs(qty-tt.wantQty) > fillFeeEps {
				t.Fatalf("PositionQty=%v want %v", qty, tt.wantQty)
			}
			if math.Abs(fee-1.5) > fillFeeEps {
				t.Fatalf("BuyFee=%v want 1.5（計價幣手續費仍全額計入）", fee)
			}

			spm.setAnchorPrice(fillFeeTestPrice)
			if err := spm.AdjustOrders(fillFeeTestPrice + 0.4); err != nil {
				t.Fatalf("AdjustOrders: %v", err)
			}
			var found bool
			for _, req := range exec.PlacedOrders {
				if req.Side != "SELL" {
					continue
				}
				found = true
				if req.Quantity > tt.wantQty+fillFeeEps {
					t.Fatalf("close order qty %v exceeds held qty %v", req.Quantity, tt.wantQty)
				}
			}
			if !found {
				t.Fatalf("no close order placed: %+v", exec.PlacedOrders)
			}
		})
	}
}

func TestNetReceivedQtyAndFloor(t *testing.T) {
	spot := newFillFeeSPM(t, "spot", nil)
	fut := newFillFeeSPM(t, "futures", nil)
	tests := []struct {
		name string
		spm  *SuperPositionManager
		side string
		d, f float64
		want float64
	}{
		{"spot buy", spot, "BUY", 1, 0.001, 0.999},
		{"spot sell ignores", spot, "SELL", 1, 0.001, 1},
		{"futures ignores", fut, "BUY", 1, 0.001, 1},
		{"fee over qty clamps", spot, "BUY", 0.001, 0.002, 0},
		{"negative fee ignored", spot, "BUY", 1, -0.1, 1},
	}
	for _, tt := range tests {
		if got := tt.spm.netReceivedQty(tt.side, tt.d, tt.f); math.Abs(got-tt.want) > fillFeeEps {
			t.Errorf("%s: got %v want %v", tt.name, got, tt.want)
		}
	}
	if got := floorToDecimals(0.009, 3); got != 0.009 {
		t.Errorf("floor(0.009,3)=%v", got)
	}
	if got := floorToDecimals(0.4995, 3); got != 0.499 {
		t.Errorf("floor(0.4995,3)=%v", got)
	}
	if got := len(interfaceSliceOf([]*fakeFill{{}, nil})); got != 1 {
		t.Errorf("interfaceSliceOf skips nil ptr: got %d", got)
	}
	if interfaceSliceOf("x") != nil {
		t.Errorf("non-slice should be nil")
	}
}
