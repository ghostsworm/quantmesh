package position

import (
	"context"
	"errors"
	"quantmesh/exchange"
	"quantmesh/execution"
	"testing"
)

type ownedCloseReadVenue struct {
	exchange.IExchange
	state *exchange.Order
}

func (*ownedCloseReadVenue) GetName() string       { return "fake" }
func (*ownedCloseReadVenue) GetPriceDecimals() int { return 2 }
func (v *ownedCloseReadVenue) GetOrder(context.Context, string, int64) (*exchange.Order, error) {
	return v.state, nil
}

type ownedCloseSubmitter struct {
	req *OrderRequest
	err error
}

func (s *ownedCloseSubmitter) PlaceOrderContext(_ context.Context, r *OrderRequest) (*Order, error) {
	s.req = r
	return &Order{OrderID: 1, Symbol: r.Symbol, Side: r.Side, ClientOrderID: r.ClientOrderID, Quantity: r.Quantity, ExecutedQty: 0.25, AvgPrice: 100, Status: "PARTIALLY_FILLED"}, s.err
}

func TestOwnedCloseWrapperPreservesCumulativeFillAndIntentOptions(t *testing.T) {
	for _, side := range []string{"BUY", "SELL"} {
		s := &ownedCloseSubmitter{}
		w := NewOwnedExchangeAdapterWrapper(&ownedCloseReadVenue{}, s, func(*exchange.Order) bool { return true })
		r := &ExchangeOrderRequest{Symbol: "BTCUSDT", Side: side, Type: "MARKET", Quantity: 1, ReduceOnly: true, ClientOrderID: "stable"}
		o, err := w.PlaceOrder(t.Context(), r)
		if err != nil || o.ExecutedQty != 0.25 || o.Quantity != 1 {
			t.Fatalf("quantity fabricated as fill: %+v %v", o, err)
		}
		leg := PositionSideLong
		if side == "BUY" {
			leg = PositionSideShort
		}
		if s.req.ClientOrderID != "stable" || s.req.PositionSide != leg || s.req.Type != "MARKET" || !s.req.ReduceOnly || s.req.StrategyName != "manual_close" || !s.req.BotWideClose {
			t.Fatalf("intent lost: %+v", s.req)
		}
		s.err = execution.ErrOrderUnknown
		if o, err := w.PlaceOrder(t.Context(), r); o == nil || !errors.Is(err, execution.ErrOrderUnknown) {
			t.Fatal("uncertain acknowledgement discarded")
		}
	}
}

func TestUnownedCloseWrapperCannotSubmitAndNilQueryIsNotFilled(t *testing.T) {
	w := NewExchangeAdapterWrapper(&ownedCloseReadVenue{})
	if _, err := w.PlaceOrder(t.Context(), &ExchangeOrderRequest{}); err == nil {
		t.Fatal("raw venue submission allowed")
	}
	if _, err := w.GetOrder(t.Context(), "BTCUSDT", 1); err == nil {
		t.Fatal("nil query accepted")
	}
}
