package position

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"quantmesh/execution"
	"quantmesh/utils"
)

type marketFillLiquidationExecutor struct {
	*liqFakeVenue
	spm              *SuperPositionManager
	unknown, earlyWS bool
	marketIntents    []*OrderRequest
}

func (v *marketFillLiquidationExecutor) PlaceOrderContext(ctx context.Context, req *OrderRequest) (*Order, error) {
	v.marketIntents = append(v.marketIntents, req)
	if v.unknown {
		return nil, execution.ErrOrderUnknown
	}
	ord, err := v.liqFakeVenue.PlaceOrderContext(ctx, req)
	if err == nil && v.earlyWS {
		cid := utils.AddBrokerPrefix("binance", req.ClientOrderID)
		v.spm.OnOrderUpdate(OrderUpdate{OrderID: ord.OrderID, ClientOrderID: cid, Symbol: req.Symbol,
			Side: req.Side, Status: ord.Status, ExecutedQty: ord.ExecutedQty, AvgPrice: ord.AvgPrice})
		ord.Status, ord.ClientOrderID = "NEW", cid
	}
	return ord, err
}

func TestLiquidationMarketBooksEachSlotAndPreservesForeignInventory(t *testing.T) {
	for _, early := range []bool{false, true} {
		for _, market := range []string{"futures", "spot"} {
			base := newLiqFakeVenue(4) // our 1.5, foreign 2.5
			base.limitFillRatio = 0.4
			spm, _ := newLiqTestSPM(t, "LONG", base)
			spm.config.Trading.MarketType = market
			v := &marketFillLiquidationExecutor{liqFakeVenue: base, spm: spm, earlyWS: early}
			spm.executor = v
			first := fillSlot(spm, 50000, 1, 50000, "")
			second := fillSlot(spm, 50100, 0.5, 50100, "")
			st := &auditTradeRecorder{}
			spm.SetTradeStorage(st)
			if err := runVerified(t, spm, base); err != nil {
				t.Fatalf("early=%v market=%s: %v", early, market, err)
			}
			if first.PositionQty > 1e-9 || second.PositionQty > 1e-9 || first.ClientOID != "" || second.ClientOID != "" {
				t.Fatal("market execution did not settle both slot books")
			}
			if len(v.marketIntents) != 2 || len(st.pnl) != 4 {
				t.Fatalf("market intents=%d trade records=%d", len(v.marketIntents), len(st.pnl))
			}
			seen := map[string]bool{}
			for _, req := range v.marketIntents {
				price, side, ok := spm.parseClientOrderID(req.ClientOrderID)
				if !ok || (price != 50000 && price != 50100) || side != "SELL" || req.Type != "MARKET" || req.PositionSide != PositionLegLong || seen[req.ClientOrderID] {
					t.Fatalf("market identity lost slot: %+v", req)
				}
				seen[req.ClientOrderID] = true
			}
			if math.Abs(base.positions[0]-2.5) > 1e-9 {
				t.Fatalf("foreign position changed: %v", base.positions)
			}
			// Full gross PnL: 1*(49990-50000) + .5*(49990-50100).
			var total float64
			for _, pnl := range st.pnl {
				total += pnl
			}
			if math.Abs(total-(-65)) > 1e-8 {
				t.Fatalf("missing/duplicate gross PnL: %v", total)
			}
		}
	}
}

func TestLiquidationUnknownMarketRetainsSlotAndCannotRetry(t *testing.T) {
	base := newLiqFakeVenue(1)
	spm, _ := newLiqTestSPM(t, "LONG", base)
	v := &marketFillLiquidationExecutor{liqFakeVenue: base, spm: spm, unknown: true}
	spm.executor = v
	slot := fillSlot(spm, liqTestLast, 1, liqTestLast, "")
	if err := runVerified(t, spm, base); err == nil {
		t.Fatal("unknown market close reported complete")
	}
	if len(v.marketIntents) != 1 || slot.ClientOID != v.marketIntents[0].ClientOrderID || slot.OrderStatus != OrderStatusUnknown || slot.PositionQty != 1 || !spm.IsOpeningPaused() {
		t.Fatal("unknown market lost slot/ownership")
	}
	spm.ResumeOpening()
	spm.LiquidateAll()
	if err := spm.LiquidateAllVerified(t.Context(), base, time.Second); err == nil || len(v.marketIntents) != 1 {
		t.Fatal("unreconciled market close retried")
	}
}

func TestLiquidationReceiptMustAgreeWithEarlierTerminal(t *testing.T) {
	base := newLiqFakeVenue(1)
	base.limitFillRatio = 1
	spm, clk := newLiqTestSPM(t, "LONG", base)
	fillSlot(spm, liqTestLast, 1, liqTestLast, "")
	sub := spm.submitLiquidationClosesContext(t.Context(), true)
	if _, _, err := spm.settleLiquidationLimitOrders(t.Context(), base, liqTestSymbol, sub.placed, clk.Now()); err != nil {
		t.Fatal(err)
	}
	p := sub.placed[0]
	state, _ := base.GetOrderState(t.Context(), liqTestSymbol, p.orderID)
	state.AvgPrice += 1
	if err := spm.applyLiquidationFill(p, state); err == nil {
		t.Fatal("conflicting terminal notional silently accepted")
	}
	state.Status, state.ExecutedQty = "CANCELED", 0.5
	if err := spm.applyLiquidationFill(p, state); err == nil {
		t.Fatal("conflicting terminal quantity silently accepted")
	}
}

func TestSlotMarketCancellationBeforeSendDoesNotAcquireIntent(t *testing.T) {
	base := newLiqFakeVenue(1)
	spm, _ := newLiqTestSPM(t, "LONG", base)
	slot := fillSlot(spm, liqTestLast, 1, liqTestLast, "")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := spm.submitSlotLiquidationMarket(ctx, base, liquidationSlotResidual{price: liqTestLast, qty: 1, side: "SELL", leg: PositionLegLong})
	if !errors.Is(err, context.Canceled) || slot.ClientOID != "" || slot.SlotStatus == SlotStatusPending || len(base.marketReqs) != 0 {
		t.Fatalf("cancelled slot acquired intent: %v", err)
	}
}
