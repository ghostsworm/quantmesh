package position

import (
	"context"
	"math"
	"testing"
	"time"

	"quantmesh/utils"
)

func TestLiquidationRESTTerminalBooksLimitFillOnce(t *testing.T) {
	for _, ratio := range []float64{0.4, 1} {
		v := newLiqFakeVenue(1)
		v.limitFillRatio = ratio
		spm, clk := newLiqTestSPM(t, "LONG", v)
		slot := fillSlot(spm, liqTestLast, 1, liqTestLast, "")
		st := &auditTradeRecorder{}
		spm.SetTradeStorage(st)
		sub := spm.submitLiquidationClosesContext(t.Context(), true)
		if len(sub.placed) != 1 || sub.err != nil {
			t.Fatalf("submit: %+v", sub)
		}
		filled, _, err := spm.settleLiquidationLimitOrders(t.Context(), v, liqTestSymbol, sub.placed, clk.Now().Add(time.Second))
		if err != nil || math.Abs(filled-ratio) > 1e-9 || math.Abs(slot.PositionQty-(1-ratio)) > 1e-9 || slot.OrderID != 0 {
			t.Fatalf("REST terminal not booked: fill=%v inventory=%v order=%v err=%v", filled, slot.PositionQty, slot.OrderID, err)
		}
		if len(st.pnl) != 1 || math.Abs(st.pnl[0]-(v.limitReqs[0].Price-liqTestLast)*ratio) > 1e-9 {
			t.Fatalf("actual fill not in ledger: %v", st.pnl)
		}
		p := sub.placed[0]
		state, _ := v.GetOrderState(t.Context(), liqTestSymbol, p.orderID)
		if err := spm.applyLiquidationFill(p, state); err != nil {
			t.Fatal(err)
		}
		spm.OnOrderUpdate(OrderUpdate{OrderID: p.orderID, ClientOrderID: utils.AddBrokerPrefix("binance", p.clientOID), Symbol: liqTestSymbol,
			Side: p.side, Status: state.Status, ExecutedQty: state.ExecutedQty, AvgPrice: state.AvgPrice})
		if len(st.pnl) != 1 || math.Abs(slot.PositionQty-(1-ratio)) > 1e-9 {
			t.Fatal("REST/WS replay booked twice")
		}
	}
}

type earlyFillLiquidationExecutor struct {
	*liqFakeVenue
	spm    *SuperPositionManager
	broker bool
}

func (v *earlyFillLiquidationExecutor) BatchPlaceOrdersWithDetailsContext(ctx context.Context, reqs []*OrderRequest) *BatchPlaceOrdersResult {
	result := v.liqFakeVenue.BatchPlaceOrdersWithDetailsContext(ctx, reqs)
	for _, ord := range result.PlacedOrders {
		state, _ := v.GetOrderState(ctx, ord.Symbol, ord.OrderID)
		cid := ord.ClientOrderID
		if v.broker {
			cid = utils.AddBrokerPrefix("binance", cid)
			ord.ClientOrderID = cid
		}
		v.spm.OnOrderUpdate(OrderUpdate{OrderID: ord.OrderID, ClientOrderID: cid, Symbol: ord.Symbol, Side: ord.Side,
			Status: state.Status, ExecutedQty: state.ExecutedQty, AvgPrice: state.AvgPrice})
		ord.Status = "NEW" // deliberately stale acknowledgement, after WS
	}
	return result
}

func TestLiquidationEarlyWSIsNotResetByLateREST(t *testing.T) {
	for _, ratio := range []float64{0.4, 1} {
		for _, broker := range []bool{false, true} {
			base := newLiqFakeVenue(1)
			base.limitFillRatio = ratio
			spm, clk := newLiqTestSPM(t, "LONG", base)
			spm.executor = &earlyFillLiquidationExecutor{liqFakeVenue: base, spm: spm, broker: broker}
			slot := fillSlot(spm, liqTestLast, 1, liqTestLast, "")
			st := &auditTradeRecorder{}
			spm.SetTradeStorage(st)
			sub := spm.submitLiquidationClosesContext(t.Context(), true)
			if sub.err != nil || len(sub.placed) != 1 {
				t.Fatalf("submit: %+v", sub)
			}
			if math.Abs(slot.PositionQty-(1-ratio)) > 1e-9 {
				t.Fatal("early fill not booked")
			}
			if ratio == 1 && (slot.OrderID != 0 || slot.ClientOID != "" || slot.SlotStatus != SlotStatusFree) {
				t.Fatal("late ACK resurrected terminal order")
			}
			if ratio < 1 && (slot.OrderFilledQty != ratio || slot.OrderStatus != OrderStatusPartiallyFilled) {
				t.Fatal("late ACK reset partial cursor")
			}
			if _, _, err := spm.settleLiquidationLimitOrders(t.Context(), base, liqTestSymbol, sub.placed, clk.Now().Add(time.Second)); err != nil {
				t.Fatal(err)
			}
			if len(st.pnl) != 1 || math.Abs(slot.PositionQty-(1-ratio)) > 1e-9 {
				t.Fatal("REST terminal repeated early WS accounting")
			}
		}
	}
}

func TestLiquidationMissingActualAverageCannotInventExecution(t *testing.T) {
	v := newLiqFakeVenue(1)
	spm, _ := newLiqTestSPM(t, "LONG", v)
	slot := fillSlot(spm, liqTestLast, 1, liqTestLast, "")
	sub := spm.submitLiquidationClosesContext(t.Context(), true)
	for _, avg := range []float64{0, math.NaN(), math.Inf(1), -1} {
		if err := spm.applyLiquidationFill(sub.placed[0], LiquidationOrderState{Status: "FILLED", ExecutedQty: 1, AvgPrice: avg, Price: liqTestBid}); err == nil {
			t.Fatalf("invalid average %v accepted", avg)
		}
		if slot.PositionQty != 1 || slot.ClientOID == "" {
			t.Fatal("lost inventory/ownership on invalid execution")
		}
	}
}

func TestGridInvalidTerminalKeepsOwnershipAndCapital(t *testing.T) {
	for _, status := range []string{"FILLED", "CANCELED", "EXPIRED", "REJECTED"} {
		for _, qty := range []float64{0, -1, math.NaN(), math.Inf(1)} {
			spm, _ := newReservationTestSPM(t, "LONG")
			cid := placeOpening(t, spm, "BUY", 7)
			spm.OnOrderUpdate(OrderUpdate{OrderID: 7, ClientOrderID: cid, Symbol: "BTCUSDT", Side: "BUY", Status: "PARTIALLY_FILLED", ExecutedQty: 0.004, AvgPrice: reservationTestPrice})
			spm.OnOrderUpdate(OrderUpdate{OrderID: 7, ClientOrderID: cid, Symbol: "BTCUSDT", Side: "BUY", Status: status, ExecutedQty: qty, AvgPrice: reservationTestPrice})
			slot := spm.getOrCreateSlot(reservationTestPrice)
			if slot.ClientOID != cid || slot.OrderID != 7 || slot.OrderStatus != OrderStatusUnknown || slot.SlotStatus != SlotStatusLocked || slot.PositionQty != 0.004 || !spm.IsOpeningPaused() {
				t.Fatalf("invalid terminal released economic state: %s qty=%v", status, qty)
			}
			assertUsed(t, spm, 500)
		}
	}
}
