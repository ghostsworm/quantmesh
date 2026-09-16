package position

import (
	"math"
	"testing"

	"quantmesh/config"
)

func newDirectionTestSPM(t *testing.T, direction string, executor OrderExecutorInterface) *SuperPositionManager {
	t.Helper()
	cfg := &config.Config{}
	cfg.Trading.Symbol = "BTCUSDT"
	cfg.Trading.Direction = direction
	cfg.Trading.MarketType = "futures"
	cfg.Trading.PriceInterval = 100
	cfg.Trading.OrderQuantity = 100
	if executor == nil {
		executor = &MockExecutor{}
	}
	return NewSuperPositionManager(cfg, executor, &MockExchange{}, 2, 3)
}

func fillSlot(spm *SuperPositionManager, price, qty, avg float64, leg string) *InventorySlot {
	slot := spm.getOrCreateSlot(price)
	slot.mu.Lock()
	slot.PositionStatus = PositionStatusFilled
	slot.PositionQty = qty
	slot.AvgBuyPrice = avg
	slot.PositionLeg = leg
	slot.SlotStatus = SlotStatusFree
	slot.mu.Unlock()
	return slot
}

func TestCalculateUnrealizedPnL_DirectionSign(t *testing.T) {
	tests := []struct {
		name      string
		direction string
		slotPrice float64
		avg       float64
		leg       string
		current   float64
		want      float64
	}{
		{"LONG 上漲盈利", "LONG", 50000, 0, "", 51000, 1000},
		{"LONG 下跌虧損", "LONG", 50000, 0, "", 49000, -1000},
		{"LONG 優先使用均價", "LONG", 50000, 49500, "", 50000, 500},
		{"SHORT 上漲虧損", "SHORT", 50000, 0, "", 51000, -1000},
		{"SHORT 下跌盈利", "SHORT", 50000, 0, "", 49000, 1000},
		{"SHORT 優先使用開空均價", "SHORT", 50000, 50200, "", 50000, 200},
		{"BOTH 空腿下跌盈利", "BOTH", 50000, 0, PositionLegShort, 49000, 1000},
		{"BOTH 多腿下跌虧損", "BOTH", 50000, 0, PositionLegLong, 49000, -1000},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			spm := newDirectionTestSPM(t, tc.direction, nil)
			fillSlot(spm, tc.slotPrice, 1, tc.avg, tc.leg)
			got := spm.calculateUnrealizedPnL(tc.current)
			if math.Abs(got-tc.want) > 1e-9 {
				t.Fatalf("pnl=%.4f want %.4f", got, tc.want)
			}
		})
	}
}

// failingExecutor 下單全部失敗（不返回任何成功訂單）
type failingExecutor struct {
	MockExecutor
}

func (f *failingExecutor) BatchPlaceOrdersWithDetails(orders []*OrderRequest) *BatchPlaceOrdersResult {
	f.PlacedOrders = append(f.PlacedOrders, orders...)
	return &BatchPlaceOrdersResult{}
}

func TestLiquidateAll_CloseSideByDirection(t *testing.T) {
	tests := []struct {
		direction string
		leg       string
		wantSide  string
		wantPx    float64
	}{
		{"LONG", "", "SELL", 49500},
		{"SHORT", "", "BUY", 50500},
		{"BOTH", PositionLegShort, "BUY", 50500},
		{"BOTH", PositionLegLong, "SELL", 49500},
	}
	for _, tc := range tests {
		t.Run(tc.direction+"_"+tc.leg, func(t *testing.T) {
			executor := &MockExecutor{}
			spm := newDirectionTestSPM(t, tc.direction, executor)
			spm.lastMarketPrice.Store(50000.0)
			fillSlot(spm, 50000, 0.5, 0, tc.leg)

			spm.LiquidateAll()

			if len(executor.PlacedOrders) != 1 {
				t.Fatalf("placed %d orders, want 1", len(executor.PlacedOrders))
			}
			req := executor.PlacedOrders[0]
			if req.Side != tc.wantSide || !req.ReduceOnly || req.PostOnly {
				t.Fatalf("unexpected close order: %+v", req)
			}
			if math.Abs(req.Price-tc.wantPx) > 1e-9 || req.Quantity != 0.5 {
				t.Fatalf("price=%.2f qty=%.4f want price=%.2f qty=0.5", req.Price, req.Quantity, tc.wantPx)
			}
		})
	}
}

func TestLiquidateAll_DoesNotCancelCloseOrdersAsOpeningOrders(t *testing.T) {
	// SHORT：BUY 為平倉單，不應被當作開倉單撤銷（開倉單為 SELL）
	executor := &MockExecutor{}
	spm := newDirectionTestSPM(t, "SHORT", executor)
	spm.lastMarketPrice.Store(50000.0)
	slot := fillSlot(spm, 50000, 1, 0, "")
	slot.mu.Lock()
	slot.OrderID = 777
	slot.OrderSide = "BUY"
	slot.OrderStatus = OrderStatusPlaced
	slot.mu.Unlock()

	spm.LiquidateAll()

	// 僅由 LiquidateAll 對持倉槽位撤一次現有平倉單，不應再經 CancelAllOpenOrders 重複撤銷
	if len(executor.CancelledOrderIDs) != 1 || executor.CancelledOrderIDs[0] != 777 {
		t.Fatalf("cancelled=%v, want [777] once", executor.CancelledOrderIDs)
	}
}

func TestLiquidateAll_RollbackPendingOnPlaceFailure(t *testing.T) {
	executor := &failingExecutor{}
	spm := newDirectionTestSPM(t, "SHORT", executor)
	spm.lastMarketPrice.Store(50000.0)
	slot := fillSlot(spm, 50000, 1, 0, "")

	spm.LiquidateAll()

	slot.mu.RLock()
	defer slot.mu.RUnlock()
	if slot.SlotStatus != SlotStatusFree {
		t.Fatalf("slot status=%s, want FREE after failed close order", slot.SlotStatus)
	}
	if slot.PositionStatus != PositionStatusFilled || slot.PositionQty != 1 {
		t.Fatalf("position must be kept: %s %.4f", slot.PositionStatus, slot.PositionQty)
	}
}

func TestIsPlacedOrderAlreadyHandled(t *testing.T) {
	tests := []struct {
		name      string
		direction string
		side      string
		slot      *InventorySlot
		want      bool
	}{
		{"LONG 開倉秒成交", "LONG", "BUY", &InventorySlot{PositionStatus: PositionStatusFilled}, true},
		{"LONG 平倉秒成交", "LONG", "SELL", &InventorySlot{PositionStatus: PositionStatusEmpty, SlotStatus: SlotStatusFree}, true},
		{"SHORT 開倉(SELL)秒成交", "SHORT", "SELL", &InventorySlot{PositionStatus: PositionStatusFilled}, true},
		{"SHORT 平倉(BUY)秒成交", "SHORT", "BUY", &InventorySlot{PositionStatus: PositionStatusEmpty, SlotStatus: SlotStatusFree}, true},
		{"SHORT 開倉未成交", "SHORT", "SELL", &InventorySlot{PositionStatus: PositionStatusEmpty, SlotStatus: SlotStatusPending}, false},
		{"SHORT 平倉未成交", "SHORT", "BUY", &InventorySlot{PositionStatus: PositionStatusFilled, SlotStatus: SlotStatusPending}, false},
		{"秒撤已被 WS 處理", "SHORT", "SELL", &InventorySlot{PositionStatus: PositionStatusEmpty, OrderStatus: OrderStatusCanceled, OrderSide: "SELL", SlotStatus: SlotStatusFree}, true},
		{"已有 OrderID 的撤單請求不算", "LONG", "BUY", &InventorySlot{OrderID: 5, OrderStatus: OrderStatusCanceled, SlotStatus: SlotStatusPending}, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			spm := newDirectionTestSPM(t, tc.direction, nil)
			if got := spm.isPlacedOrderAlreadyHandled(tc.side, tc.slot); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

type fakeDirectionalFunding struct {
	buy, sell float64
}

func (f fakeDirectionalFunding) GetBuyBias() float64     { return f.buy }
func (f fakeDirectionalFunding) GetSellBias() float64    { return f.sell }
func (f fakeDirectionalFunding) IsHighRate() bool        { return false }
func (f fakeDirectionalFunding) GetCurrentRate() float64 { return 0 }
func (f fakeDirectionalFunding) ShouldPauseBuying() bool { return f.buy == 0 }

func TestOpeningFundingBiasAndTrendMirror(t *testing.T) {
	tests := []struct {
		direction     string
		wantBias      float64
		wantFavorable string
		wantAdverse   string
	}{
		{"LONG", 0.3, "up", "down"},
		{"SHORT", 1.2, "down", "up"},
	}
	for _, tc := range tests {
		t.Run(tc.direction, func(t *testing.T) {
			spm := newDirectionTestSPM(t, tc.direction, nil)
			if got := spm.openingFundingBias(); got != 1.0 {
				t.Fatalf("nil monitor bias=%.2f want 1.0", got)
			}
			spm.SetFundingMonitor(fakeDirectionalFunding{buy: 0.3, sell: 1.2})
			if got := spm.openingFundingBias(); got != tc.wantBias {
				t.Fatalf("bias=%.2f want %.2f", got, tc.wantBias)
			}
			if spm.favorableTrend() != tc.wantFavorable || spm.adverseTrend() != tc.wantAdverse {
				t.Fatalf("trend favorable=%s adverse=%s", spm.favorableTrend(), spm.adverseTrend())
			}
		})
	}
}

func TestGetNetPositionQty(t *testing.T) {
	long := newDirectionTestSPM(t, "LONG", nil)
	fillSlot(long, 50000, 0.3, 0, "")
	fillSlot(long, 49900, 0.2, 0, "")
	if got := long.GetNetPositionQty(); math.Abs(got-0.5) > 1e-12 {
		t.Fatalf("LONG net=%.4f want 0.5", got)
	}
	short := newDirectionTestSPM(t, "SHORT", nil)
	fillSlot(short, 50000, 0.3, 0, "")
	if got := short.GetNetPositionQty(); math.Abs(got+0.3) > 1e-12 {
		t.Fatalf("SHORT net=%.4f want -0.3", got)
	}
	both := newDirectionTestSPM(t, "BOTH", nil)
	fillSlot(both, 49900, 0.5, 0, PositionLegLong)
	fillSlot(both, 50100, 0.2, 0, PositionLegShort)
	if got := both.GetNetPositionQty(); math.Abs(got-0.3) > 1e-12 {
		t.Fatalf("BOTH net=%.4f want 0.3", got)
	}
}

func TestPlanCloseOrder(t *testing.T) {
	tests := []struct {
		name        string
		botNet      float64
		exNet       float64
		hasExchange bool
		ratio       float64
		decimals    int
		wantSide    string
		wantQty     float64
		wantErr     bool
	}{
		{"多頭全平", 0.5, 0.5, true, 0, 3, "SELL", 0.5, false},
		{"空頭全平", -0.5, -0.5, true, 1, 3, "BUY", 0.5, false},
		{"按比例平一半並向下取整", 0.333, 0.333, true, 0.5, 3, "SELL", 0.166, false},
		{"交易所持倉較少時封頂", 1.0, 0.4, true, 0, 3, "SELL", 0.4, false},
		{"無交易所數據時按本地", -0.25, 0, false, 0, 3, "BUY", 0.25, false},
		{"方向不一致拒絕", 0.5, -0.5, true, 0, 3, "", 0, true},
		{"交易所無持倉拒絕", 0.5, 0, true, 0, 3, "", 0, true},
		{"無持倉", 0, 0, false, 0, 3, "", 0, true},
		{"比例非法", 0.5, 0.5, true, 1.5, 3, "", 0, true},
		{"取整後為 0", 0.0004, 0.0004, true, 0, 3, "", 0, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := PlanCloseOrder(tc.botNet, tc.exNet, tc.hasExchange, tc.ratio, tc.decimals)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error, got plan %+v", plan)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if plan.Side != tc.wantSide || math.Abs(plan.Quantity-tc.wantQty) > 1e-12 {
				t.Fatalf("plan=%+v want side=%s qty=%.4f", plan, tc.wantSide, tc.wantQty)
			}
		})
	}
}
