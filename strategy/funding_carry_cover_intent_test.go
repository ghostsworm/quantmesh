package strategy

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"quantmesh/exchange"
)

type fundingCarryCoverIntentVenue struct {
	*fundingCarryRepayIntentExchange
	store    *memoryRuntimeStateStore
	t        *testing.T
	noACK    bool
	wrongCID bool
}

func (v *fundingCarryCoverIntentVenue) PlaceOrder(ctx context.Context, req *exchange.OrderRequest) (*exchange.Order, error) {
	var state fundingCarryRuntimeState
	if err := json.Unmarshal([]byte(v.store.payload), &state); err != nil {
		v.t.Fatal(err)
	}
	i := state.MarginCoverIntent
	if i == nil || req.ClientOrderID == "" || i.ClientOrderID != req.ClientOrderID || i.Quantity != req.Quantity || i.Price != req.Price || i.Symbol != req.Symbol || i.AccountScope != "scope-a" || !state.IntentInFlight {
		v.t.Fatal("RPC entered without matching durable CID request")
	}
	if v.noACK {
		return nil, errors.New("injected accepted request without ACK")
	}
	order, err := v.mockFCExchange.PlaceOrder(ctx, req)
	order.ClientOrderID = req.ClientOrderID
	if v.wrongCID {
		order.ClientOrderID = "foreign-request"
	}
	return order, err
}

func TestFundingCarryCoverIntentIsSavedBeforeRPC(t *testing.T) {
	for _, mode := range []string{"ack", "no_ack", "wrong_cid"} {
		t.Run(mode, func(t *testing.T) {
			s, margin, store := newFundingCarryRepayIntentFixture()
			venue := &fundingCarryCoverIntentVenue{fundingCarryRepayIntentExchange: margin, store: store, t: t, noACK: mode == "no_ack", wrongCID: mode == "wrong_cid"}
			s.marginEx = venue
			if err := s.closeReverse(context.Background(), mode); err == nil {
				t.Fatal("expected injected unresolved outcome")
			}
			var state fundingCarryRuntimeState
			if err := json.Unmarshal([]byte(store.payload), &state); err != nil {
				t.Fatal(err)
			}
			if mode == "ack" {
				if state.MarginCoverIntent != nil || len(state.MarginCoverOrders) != 1 || state.MarginCoverOrders[0].ClientOrderID == "" || margin.repayCalls != 1 {
					t.Fatal("ACK not converted to exact durable order record")
				}
			} else {
				if state.MarginCoverIntent == nil || !state.IntentInFlight || !state.ExposureUnknown || margin.repayCalls != 0 {
					t.Fatal("uncertain submission intent lost or repaid")
				}
				if err := s.beginRuntimeIntent(context.Background()); err == nil {
					t.Fatal("new operation overwrote uncertain request")
				}
			}
		})
	}
}

func TestFundingCarryCoverIntentSaveFailureNeverSubmits(t *testing.T) {
	s, margin, store := newFundingCarryRepayIntentFixture()
	s.intentInFlight = true
	store.err = errors.New("injected pre-RPC save failure")
	req := &exchange.OrderRequest{Symbol: "BTCUSDT", Side: exchange.SideBuy, Type: exchange.OrderTypeLimit, Quantity: 0.4, Price: 50000}
	if err := s.prepareMarginCoverIntent(context.Background(), req, 0.4); err == nil {
		t.Fatal("save failure ignored")
	}
	if len(margin.placedOrders) != 0 || s.marginCoverIntent == nil || !s.unownedExposure {
		t.Fatal("failed WAL allowed submission or lost local request")
	}
}
