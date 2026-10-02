package strategy

import (
	"context"
	"math"
	"testing"

	"quantmesh/position"
)

func TestMultiStrategyBatchReportsKnownLocalRejection(t *testing.T) {
	for _, reason := range []string{"capital", "cancelled", "invalid notional", "nan notional", "infinite notional", "bot wide close"} {
		t.Run(reason, func(t *testing.T) {
			venue := &brokerCapitalVenue{}
			executor := newBrokerCapitalExecutor(venue)
			ctx := context.Background()
			req := &position.OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Price: 2000, Quantity: 1, ClientOrderID: "local-reject"}
			if reason == "cancelled" {
				cancelCtx, cancel := context.WithCancel(ctx)
				cancel()
				ctx = cancelCtx
			}
			if reason == "invalid notional" {
				req.Price = 0
			}
			if reason == "nan notional" {
				req.Price = math.NaN()
			}
			if reason == "infinite notional" {
				req.Price = math.Inf(1)
			}
			if reason == "bot wide close" {
				req.BotWideClose = true
			}
			result := executor.BatchPlaceOrdersWithDetailsContext(ctx, "dca", []*position.OrderRequest{req})
			if !result.HasAdmissionError || result.AdmissionErrors[req.ClientOrderID] == "" || result.HasMarginError || len(result.UnknownOrders) != 0 {
				t.Fatalf("known pre-submission rejection disappeared or was misclassified: %+v", result)
			}
			if venue.calls != 0 || len(result.PlacedOrders) != 0 || executor.allocator.GetAvailable("dca") != 1000 {
				t.Fatalf("rejected request reached venue or reserved capital: calls=%d result=%+v", venue.calls, result)
			}
		})
	}
}

type comboAdmissionFlagExecutor struct{ comboAdmissionTestExecutor }

func (e *comboAdmissionFlagExecutor) BatchPlaceOrdersWithDetails([]*position.OrderRequest) *position.BatchPlaceOrdersResult {
	return &position.BatchPlaceOrdersResult{HasAdmissionError: true}
}

func TestComboPreservesDownstreamAdmissionFlagWithoutDetailMap(t *testing.T) {
	gate := &comboExposureAdmissionExecutor{next: &comboAdmissionFlagExecutor{}}
	reqs := []*position.OrderRequest{{Side: "SELL", ReduceOnly: true, ClientOrderID: "downstream-rejected"}}
	result := gate.BatchPlaceOrdersWithDetails(reqs)
	if !result.HasAdmissionError || result.HasMarginError || len(result.UnknownOrders) != 0 {
		t.Fatalf("downstream admission flag was erased or misclassified: %+v", result)
	}
	if _, rejected := gate.BatchPlaceOrders(reqs); !rejected {
		t.Fatal("legacy batch API reported downstream rejection as success")
	}
}

func TestMultiStrategyLegacyBatchReportsLocalAdmissionFailure(t *testing.T) {
	venue := &brokerCapitalVenue{}
	executor := newBrokerCapitalExecutor(venue)
	orders, rejected := executor.BatchPlaceOrders("dca", []*position.OrderRequest{
		{Symbol: "BTCUSDT", Side: "BUY", Price: 2000, Quantity: 1, ClientOrderID: "capital-rejected"},
	})
	if !rejected || len(orders) != 0 || venue.calls != 0 {
		t.Fatalf("legacy batch API lost known rejection: orders=%v rejected=%v sends=%d", orders, rejected, venue.calls)
	}
}

func TestMultiStrategyBatchPreservesTrackedIntentUncertainty(t *testing.T) {
	venue := &brokerCapitalVenue{unknown: true}
	executor := newBrokerCapitalExecutor(venue)
	req := &position.OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1, ClientOrderID: "already-pending"}
	executor.BatchPlaceOrdersWithDetails("dca", []*position.OrderRequest{req})
	available := executor.allocator.GetAvailable("dca")
	result := executor.BatchPlaceOrdersWithDetails("dca", []*position.OrderRequest{req})
	if !result.UnknownOrders[req.ClientOrderID] || result.HasAdmissionError || venue.calls != 1 || executor.allocator.GetAvailable("dca") != available {
		t.Fatalf("tracked intent retry lost uncertainty or changed original reservation: %+v calls=%d", result, venue.calls)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result = executor.BatchPlaceOrdersWithDetailsContext(ctx, "dca", []*position.OrderRequest{req})
	if !result.UnknownOrders[req.ClientOrderID] || result.HasAdmissionError || venue.calls != 1 || executor.allocator.GetAvailable("dca") != available {
		t.Fatalf("cancelled retry erased the original unknown intent: %+v calls=%d", result, venue.calls)
	}
}
