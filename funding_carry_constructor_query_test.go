package main

import (
	"encoding/json"
	"testing"
	"time"

	"quantmesh/exchange"
	"quantmesh/storage"
)

// The oracle checks the first-query SQL write, not merely equality to whatever
// payload the constructor happened to save. Later stop/retry checks retain it.
func assertConstructorQueriedCoverEvidence(t *testing.T, store storage.StrategyRuntimeStateStore, botID, scope string) string {
	t.Helper()
	saved, err := store.GetStrategyRuntimeState(botID, "funding_carry")
	if err != nil || saved == nil || saved.SchemaVersion != 8 {
		t.Fatalf("queried cover was not durably saved as schema8: %v", err)
	}
	var state struct {
		MarginDebt      float64 `json:"margin_debt"`
		BorrowID        int64   `json:"margin_borrow_transfer_id"`
		IntentInFlight  bool    `json:"intent_in_flight"`
		ExposureUnknown bool    `json:"exposure_unknown"`
		AccountScope    string  `json:"margin_account_scope"`
		Orders          []struct {
			OrderID   int64                 `json:"order_id"`
			CID       string                `json:"client_order_id"`
			Asset     string                `json:"asset"`
			Scope     string                `json:"account_scope"`
			Price     float64               `json:"request_price"`
			Prepared  time.Time             `json:"prepared_at"`
			Requested float64               `json:"requested"`
			Debt      float64               `json:"debt_to_cover"`
			Gross     float64               `json:"gross"`
			Net       float64               `json:"net"`
			Verified  bool                  `json:"verified"`
			Terminal  exchange.OrderStatus  `json:"terminal_status"`
			Consumed  float64               `json:"consumed"`
			RepayID   int64                 `json:"repay_transfer_id"`
			Fills     []*exchange.OrderFill `json:"fills"`
		} `json:"margin_cover_orders"`
	}
	if err := json.Unmarshal([]byte(saved.Payload), &state); err != nil {
		t.Fatal(err)
	}
	if state.MarginDebt != 0.4 || state.BorrowID != 42 || !state.IntentInFlight || !state.ExposureUnknown || state.AccountScope != scope || len(state.Orders) != 1 {
		t.Fatal("first query rewrote unresolved debt or attribution")
	}
	r := state.Orders[0]
	if r.OrderID != 7 || r.CID != "partial-7" || r.Asset != "BTC" || r.Scope != scope || r.Price != 50000 || !r.Prepared.Equal(time.UnixMilli(1000)) || r.Requested != 0.401 || r.Debt != 0.4 || r.Gross != 0.2 || r.Net != 0.1995 || !r.Verified || r.Terminal != exchange.OrderStatusCanceled || r.Consumed != 0 || r.RepayID != 0 || len(r.Fills) != 1 || r.Fills[0] == nil {
		t.Fatal("SQL cover evidence lost exact request, partial net, or terminal identity")
	}
	f := r.Fills[0]
	if f.OrderID != 7 || f.TradeID != "partial-7" || f.Symbol != "BTCUSDT" || f.Side != exchange.SideBuy || f.Price != 50000 || f.Quantity != 0.2 || f.CommissionAsset != "BTC" || f.Commission != 0.0005 || f.BaseFeeQty != 0.0005 || f.TradeTime != 1500 {
		t.Fatal("SQL fill evidence did not preserve raw fee and exact trade")
	}
	return saved.Payload
}
