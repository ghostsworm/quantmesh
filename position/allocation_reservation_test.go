package position

import (
	"context"
	"math"
	"sync/atomic"
	"testing"

	"quantmesh/config"
)

const (
	reservationTestPrice = 50000.0
	reservationTestQty   = 0.01 // 名義價值 500 USDT
	reservationTestLimit = 1000.0
)

// countingExchange 統計 REST 調用次數，用於驗證 WS 回調不做網絡請求（E4）
type countingExchange struct {
	MockExchange
	accountCalls   atomic.Int64
	positionsCalls atomic.Int64
}

func (c *countingExchange) GetAccount(ctx context.Context) (interface{}, error) {
	c.accountCalls.Add(1)
	return nil, nil
}

func (c *countingExchange) GetPositions(ctx context.Context, symbol string) (interface{}, error) {
	c.positionsCalls.Add(1)
	return nil, nil
}

func newReservationTestSPM(t *testing.T, direction string) (*SuperPositionManager, *countingExchange) {
	t.Helper()
	cfg := &config.Config{}
	cfg.Trading.Symbol = "BTCUSDT"
	cfg.Trading.Direction = direction
	cfg.Trading.MarketType = "futures"
	cfg.Trading.PriceInterval = 100
	cfg.Trading.OrderQuantity = 500
	cfg.PositionAllocation.Enabled = true
	cfg.PositionAllocation.Allocations = []config.SymbolAllocation{{
		Exchange:      "binance",
		Symbol:        "BTCUSDT",
		MaxAmountUSDT: reservationTestLimit,
		MaxPercentage: 100,
	}}
	ex := &countingExchange{}
	return NewSuperPositionManager(cfg, &MockExecutor{}, ex, 2, 3), ex
}

func usedAmount(t *testing.T, spm *SuperPositionManager) float64 {
	t.Helper()
	status := spm.allocationManager.GetStatus("binance", "BTCUSDT")
	if status == nil {
		t.Fatalf("allocation status missing")
	}
	return status.UsedAmount
}

func assertUsed(t *testing.T, spm *SuperPositionManager, want float64) {
	t.Helper()
	if got := usedAmount(t, spm); math.Abs(got-want) > 1e-6 {
		t.Fatalf("UsedAmount=%.6f want %.6f", got, want)
	}
}

// placeOpening 預留並模擬 WS NEW 推送
func placeOpening(t *testing.T, spm *SuperPositionManager, side string, orderID int64) string {
	t.Helper()
	coid := spm.generateClientOrderID(reservationTestPrice, side, "")
	req := &OrderRequest{Symbol: "BTCUSDT", Side: side, Price: reservationTestPrice, Quantity: reservationTestQty, ClientOrderID: coid}
	if _, err := spm.reserveOrderAllocation(req, 1, 0); err != nil {
		t.Fatalf("reserve %s: %v", side, err)
	}
	spm.OnOrderUpdate(OrderUpdate{OrderID: orderID, ClientOrderID: coid, Symbol: "BTCUSDT", Status: "NEW", Side: side, Price: reservationTestPrice})
	return coid
}

func TestAllocationReservation_CancelReleasesExactlyPerDirection(t *testing.T) {
	tests := []struct {
		direction string
		side      string
	}{
		{"LONG", "BUY"},
		{"SHORT", "SELL"},
		{"BOTH", "BUY"},
		{"BOTH", "SELL"},
	}
	for _, tc := range tests {
		for _, status := range []string{"CANCELED", "EXPIRED", "REJECTED"} {
			t.Run(tc.direction+"_"+tc.side+"_"+status, func(t *testing.T) {
				spm, ex := newReservationTestSPM(t, tc.direction)
				coid := placeOpening(t, spm, tc.side, 1)
				assertUsed(t, spm, 500)

				spm.OnOrderUpdate(OrderUpdate{OrderID: 1, ClientOrderID: coid, Symbol: "BTCUSDT", Status: status, Side: tc.side})
				assertUsed(t, spm, 0)

				slot := spm.getOrCreateSlot(reservationTestPrice)
				if slot.PositionStatus != PositionStatusEmpty || slot.SlotStatus != SlotStatusFree {
					t.Fatalf("slot after cancel: position=%s slot=%s", slot.PositionStatus, slot.SlotStatus)
				}
				// 重複的撤單推送不能重複釋放
				spm.OnOrderUpdate(OrderUpdate{OrderID: 1, ClientOrderID: coid, Symbol: "BTCUSDT", Status: status, Side: tc.side})
				assertUsed(t, spm, 0)
				if n := ex.accountCalls.Load() + ex.positionsCalls.Load(); n != 0 {
					t.Fatalf("WS callbacks made %d REST calls, want 0", n)
				}
			})
		}
	}
}

func TestAllocationReservation_CloseOrdersNotBlocked(t *testing.T) {
	tests := []struct {
		direction  string
		closeSide  string
		reduceOnly bool
	}{
		{"LONG", "SELL", true},
		{"SHORT", "BUY", true},
		{"LONG", "SELL", false}, // 現貨式非 ReduceOnly 平倉單，按方向識別
	}
	for _, tc := range tests {
		t.Run(tc.direction+"_"+tc.closeSide, func(t *testing.T) {
			spm, _ := newReservationTestSPM(t, tc.direction)
			spm.allocationManager.SetUsedAmount("binance", "BTCUSDT", reservationTestLimit)
			req := &OrderRequest{Symbol: "BTCUSDT", Side: tc.closeSide, Price: reservationTestPrice, Quantity: reservationTestQty,
				ReduceOnly: tc.reduceOnly, ClientOrderID: spm.generateClientOrderID(reservationTestPrice, tc.closeSide, "")}
			if _, err := spm.reserveOrderAllocation(req, 1, 0); err != nil {
				t.Fatalf("close order blocked by allocation: %v", err)
			}
			assertUsed(t, spm, reservationTestLimit)
		})
	}
}

func TestAllocationReservation_OpenOrderBlockedAtLimit(t *testing.T) {
	spm, _ := newReservationTestSPM(t, "SHORT")
	spm.allocationManager.SetUsedAmount("binance", "BTCUSDT", 800)
	req := &OrderRequest{Symbol: "BTCUSDT", Side: "SELL", Price: reservationTestPrice, Quantity: reservationTestQty,
		ClientOrderID: spm.generateClientOrderID(reservationTestPrice, "SELL", "")}
	if _, err := spm.reserveOrderAllocation(req, 1, 0); err == nil {
		t.Fatalf("SHORT opening order above limit should be rejected")
	}
	// 被拒的預留不會留下記錄，撤單釋放為 0
	if released := spm.releaseOrderReservation(req.ClientOrderID); released != 0 {
		t.Fatalf("released=%.2f want 0", released)
	}
	assertUsed(t, spm, 800)
}

func TestAllocationReservation_PartialFillCancelThenClose(t *testing.T) {
	tests := []struct {
		direction string
		openSide  string
		closeSide string
	}{
		{"LONG", "BUY", "SELL"},
		{"SHORT", "SELL", "BUY"},
		{"BOTH", "SELL", "BUY"},
	}
	for _, tc := range tests {
		t.Run(tc.direction, func(t *testing.T) {
			spm, ex := newReservationTestSPM(t, tc.direction)
			coid := placeOpening(t, spm, tc.openSide, 7)

			spm.OnOrderUpdate(OrderUpdate{OrderID: 7, ClientOrderID: coid, Symbol: "BTCUSDT", Status: "PARTIALLY_FILLED",
				Side: tc.openSide, ExecutedQty: 0.004, AvgPrice: reservationTestPrice, Commission: 0.01})
			slot := spm.getOrCreateSlot(reservationTestPrice)
			if math.Abs(slot.AllocatedMargin-200) > 1e-6 {
				t.Fatalf("AllocatedMargin=%.4f want 200", slot.AllocatedMargin)
			}
			assertUsed(t, spm, 500)

			spm.OnOrderUpdate(OrderUpdate{OrderID: 7, ClientOrderID: coid, Symbol: "BTCUSDT", Status: "CANCELED", Side: tc.openSide})
			assertUsed(t, spm, 200)
			if slot.PositionStatus != PositionStatusFilled || slot.SlotStatus != SlotStatusFree {
				t.Fatalf("partial-fill cancel should keep position: %s/%s", slot.PositionStatus, slot.SlotStatus)
			}

			closeCOID := spm.generateClientOrderID(reservationTestPrice, tc.closeSide, "")
			spm.OnOrderUpdate(OrderUpdate{OrderID: 8, ClientOrderID: closeCOID, Symbol: "BTCUSDT", Status: "NEW", Side: tc.closeSide})
			spm.OnOrderUpdate(OrderUpdate{OrderID: 8, ClientOrderID: closeCOID, Symbol: "BTCUSDT", Status: "PARTIALLY_FILLED",
				Side: tc.closeSide, ExecutedQty: 0.002, AvgPrice: reservationTestPrice, Commission: 0.01})
			assertUsed(t, spm, 100)
			spm.OnOrderUpdate(OrderUpdate{OrderID: 8, ClientOrderID: closeCOID, Symbol: "BTCUSDT", Status: "FILLED",
				Side: tc.closeSide, ExecutedQty: 0.004, AvgPrice: reservationTestPrice, Commission: 0.01})
			assertUsed(t, spm, 0)
			if slot.PositionStatus != PositionStatusEmpty || slot.AllocatedMargin != 0 {
				t.Fatalf("after close: position=%s margin=%.4f", slot.PositionStatus, slot.AllocatedMargin)
			}
			if n := ex.accountCalls.Load() + ex.positionsCalls.Load(); n != 0 {
				t.Fatalf("WS callbacks made %d REST calls, want 0", n)
			}
		})
	}
}

func TestAllocationReservation_FullFillKeepsUsageUntilClose(t *testing.T) {
	spm, _ := newReservationTestSPM(t, "SHORT")
	coid := placeOpening(t, spm, "SELL", 9)
	spm.OnOrderUpdate(OrderUpdate{OrderID: 9, ClientOrderID: coid, Symbol: "BTCUSDT", Status: "FILLED",
		Side: "SELL", ExecutedQty: reservationTestQty, AvgPrice: reservationTestPrice, Commission: 0.01})
	// 成交後持倉仍占用额度（不再像舊實現那樣在成交時釋放）
	assertUsed(t, spm, 500)
	if released := spm.releaseOrderReservation(coid); released != 0 {
		t.Fatalf("reservation should be consumed on fill, released=%.2f", released)
	}

	closeCOID := spm.generateClientOrderID(reservationTestPrice, "BUY", "")
	spm.OnOrderUpdate(OrderUpdate{OrderID: 10, ClientOrderID: closeCOID, Symbol: "BTCUSDT", Status: "NEW", Side: "BUY"})
	spm.OnOrderUpdate(OrderUpdate{OrderID: 10, ClientOrderID: closeCOID, Symbol: "BTCUSDT", Status: "FILLED",
		Side: "BUY", ExecutedQty: reservationTestQty, AvgPrice: reservationTestPrice - 100, Commission: 0.01})
	assertUsed(t, spm, 0)
}

func TestOnOrderUpdate_CancelLegAwareForShort(t *testing.T) {
	spm, _ := newReservationTestSPM(t, "SHORT")

	// SHORT 開倉 SELL 未成交被撤：槽位重置為空
	coid := placeOpening(t, spm, "SELL", 11)
	spm.OnOrderUpdate(OrderUpdate{OrderID: 11, ClientOrderID: coid, Symbol: "BTCUSDT", Status: "CANCELED", Side: "SELL"})
	slot := spm.getOrCreateSlot(reservationTestPrice)
	if slot.PositionStatus != PositionStatusEmpty || slot.PostOnlyFailCount != 0 {
		t.Fatalf("SHORT open cancel: position=%s postOnlyFail=%d", slot.PositionStatus, slot.PostOnlyFailCount)
	}

	// SHORT 平倉 BUY 被撤：保留空頭持倉並累計 PostOnly 失敗
	fillSlot(spm, reservationTestPrice, reservationTestQty, reservationTestPrice, "")
	closeCOID := spm.generateClientOrderID(reservationTestPrice, "BUY", "")
	spm.OnOrderUpdate(OrderUpdate{OrderID: 12, ClientOrderID: closeCOID, Symbol: "BTCUSDT", Status: "NEW", Side: "BUY"})
	spm.OnOrderUpdate(OrderUpdate{OrderID: 12, ClientOrderID: closeCOID, Symbol: "BTCUSDT", Status: "CANCELED", Side: "BUY"})
	if slot.PositionStatus != PositionStatusFilled || slot.PositionQty != reservationTestQty || slot.PostOnlyFailCount != 1 {
		t.Fatalf("SHORT close cancel: position=%s qty=%.4f postOnlyFail=%d", slot.PositionStatus, slot.PositionQty, slot.PostOnlyFailCount)
	}
}

func TestOnOrderUpdate_CancelLegAwareForBoth(t *testing.T) {
	spm, _ := newReservationTestSPM(t, "BOTH")
	// 空腿持倉，BUY 為平倉腿
	fillSlot(spm, reservationTestPrice, reservationTestQty, reservationTestPrice, PositionLegShort)
	closeCOID := spm.generateClientOrderID(reservationTestPrice, "BUY", "")
	spm.OnOrderUpdate(OrderUpdate{OrderID: 13, ClientOrderID: closeCOID, Symbol: "BTCUSDT", Status: "NEW", Side: "BUY"})
	spm.OnOrderUpdate(OrderUpdate{OrderID: 13, ClientOrderID: closeCOID, Symbol: "BTCUSDT", Status: "CANCELED", Side: "BUY"})
	slot := spm.getOrCreateSlot(reservationTestPrice)
	if slot.PositionStatus != PositionStatusFilled || slot.PositionLeg != PositionLegShort || slot.PostOnlyFailCount != 1 {
		t.Fatalf("BOTH short-leg close cancel: position=%s leg=%s postOnlyFail=%d", slot.PositionStatus, slot.PositionLeg, slot.PostOnlyFailCount)
	}
}

func TestHandleReduceOnlyRejection_KeepsPositionAndCoolsDown(t *testing.T) {
	for _, direction := range []string{"LONG", "SHORT", "BOTH"} {
		t.Run(direction, func(t *testing.T) {
			spm, _ := newReservationTestSPM(t, direction)
			slot := fillSlot(spm, reservationTestPrice, reservationTestQty, reservationTestPrice, PositionLegShort)
			slot.mu.Lock()
			slot.SlotStatus = SlotStatusPending
			slot.mu.Unlock()

			spm.handleReduceOnlyRejection(reservationTestPrice, "BUY", spm.generateClientOrderID(reservationTestPrice, "BUY", ""))

			if slot.PositionStatus != PositionStatusFilled || slot.PositionQty != reservationTestQty {
				t.Fatalf("reduce-only rejection must not zero local position: %s qty=%.4f", slot.PositionStatus, slot.PositionQty)
			}
			if slot.SlotStatus != SlotStatusFree {
				t.Fatalf("slot lock not released: %s", slot.SlotStatus)
			}
			if !spm.isReduceOnlyCooldown(reservationTestPrice) {
				t.Fatalf("cooldown not set")
			}
		})
	}
}

type leverageAccount struct {
	AvailableBalance float64
	AccountLeverage  int
}

type leveragePosition struct {
	Leverage int
}

type leverageExchange struct {
	countingExchange
}

func (l *leverageExchange) GetPositions(ctx context.Context, symbol string) (interface{}, error) {
	l.positionsCalls.Add(1)
	return []*leveragePosition{{Leverage: 5}}, nil
}

func TestResolveLeverage_CachesAndCallbacksUseCache(t *testing.T) {
	cfg := &config.Config{}
	cfg.Trading.Symbol = "BTCUSDT"
	cfg.Trading.MarketType = "futures"
	ex := &leverageExchange{}
	spm := NewSuperPositionManager(cfg, &MockExecutor{}, ex, 2, 3)

	if got := spm.getActualMargin(1000); got != 1000 {
		t.Fatalf("uncached margin=%.2f want 1000 (leverage 1)", got)
	}
	if ex.positionsCalls.Load() != 0 {
		t.Fatalf("getActualMargin must not call REST")
	}

	if lev := spm.resolveLeverage(context.Background(), &leverageAccount{AccountLeverage: 10}); lev != 10 {
		t.Fatalf("account leverage=%d want 10", lev)
	}
	if ex.positionsCalls.Load() != 0 {
		t.Fatalf("account leverage available, positions should not be queried")
	}
	if got := spm.getActualMargin(1000); got != 100 {
		t.Fatalf("cached margin=%.2f want 100", got)
	}
	// 緩存新鮮時，帳戶無槓桿信息也不查持倉
	if lev := spm.resolveLeverage(context.Background(), nil); lev != 10 || ex.positionsCalls.Load() != 0 {
		t.Fatalf("fresh cache: lev=%d positionsCalls=%d", lev, ex.positionsCalls.Load())
	}
	// 緩存過期後才查持倉刷新
	spm.leverage.mu.Lock()
	spm.leverage.updatedAt = spm.leverage.updatedAt.Add(-2 * leverageCacheRefreshInterval)
	spm.leverage.mu.Unlock()
	if lev := spm.resolveLeverage(context.Background(), nil); lev != 5 || ex.positionsCalls.Load() != 1 {
		t.Fatalf("stale cache: lev=%d positionsCalls=%d", lev, ex.positionsCalls.Load())
	}
}
