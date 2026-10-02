package strategy

import (
	"context"
	"testing"

	"quantmesh/position"
)

type comboNilBatchResultExecutor struct{ comboAdmissionTestExecutor }

type comboNilContextBatchResultExecutor struct{ comboNilBatchResultExecutor }

func (e *comboNilContextBatchResultExecutor) BatchPlaceOrdersWithDetailsContext(ctx context.Context, orders []*position.OrderRequest) *position.BatchPlaceOrdersResult {
	return e.BatchPlaceOrdersWithDetails(orders)
}

func (e *comboNilBatchResultExecutor) BatchPlaceOrdersWithDetails(orders []*position.OrderRequest) *position.BatchPlaceOrdersResult {
	e.comboAdmissionTestExecutor.BatchPlaceOrdersWithDetails(orders)
	return nil
}

func TestComboNilBatchResultRetainsOnlySubmittedOrderUncertainty(t *testing.T) {
	combo := &ComboStrategy{strategyCfg: &ComboConfig{TotalCapital: 100, MaxExposure: 0.8}, riskExposureReady: true}
	next := &comboNilBatchResultExecutor{}
	gate := &comboExposureAdmissionExecutor{combo: combo, next: next}
	gate.beginChildAdmission()
	defer gate.endChildAdmission()
	orders := []*position.OrderRequest{
		{Side: "BUY", Price: 90, Quantity: 1, ClientOrderID: "rejected-open"},
		{Side: "SELL", Price: 100, Quantity: 1, ReduceOnly: true, ClientOrderID: "submitted-close"},
		{Side: "SELL", Price: 100, Quantity: 1, ReduceOnly: true},
	}
	result := gate.BatchPlaceOrdersWithDetails(orders)
	if !result.HasAdmissionError || result.AdmissionErrors["rejected-open"] == "" {
		t.Fatalf("known local rejection was lost: %+v", result)
	}
	if !result.UnknownOrders["submitted-close"] || !result.UnknownOrders["batch-index:2"] || result.UnknownOrders["rejected-open"] {
		t.Fatalf("nil result must preserve submitted uncertainty, not locally rejected orders: %+v", result)
	}
	if len(result.PlacedOrders) != 0 || result.HasMarginError || len(next.batches) != 1 || len(next.batches[0]) != 2 {
		t.Fatalf("nil result invented success/margin error or submitted a rejected order: %+v batches=%v", result, next.batches)
	}
}

func TestComboNilContextBatchResultPreservesOpeningUncertaintyAndReservation(t *testing.T) {
	combo := &ComboStrategy{strategyCfg: &ComboConfig{TotalCapital: 100, MaxExposure: 0.8}, riskExposureReady: true}
	next := &comboNilContextBatchResultExecutor{}
	gate := &comboExposureAdmissionExecutor{combo: combo, next: next}
	gate.beginChildAdmission()
	defer gate.endChildAdmission()
	result := gate.BatchPlaceOrdersWithDetailsContext(context.Background(), []*position.OrderRequest{
		{Side: "BUY", Price: 45, Quantity: 1, ClientOrderID: "unknown-open"},
	})
	if !result.UnknownOrders["unknown-open"] || result.HasAdmissionError || result.HasMarginError || len(result.PlacedOrders) != 0 {
		t.Fatalf("nil contextual result lost uncertainty or invented a known rejection: %+v", result)
	}
	if _, err := gate.PlaceOrder(&position.OrderRequest{Side: "BUY", Price: 45, Quantity: 1, ClientOrderID: "next-open"}); err == nil || next.places != 0 {
		t.Fatalf("unknown submitted notional was released within callback: err=%v places=%d", err, next.places)
	}
}
