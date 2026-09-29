package exchange

import (
	"context"
	"testing"
	"time"

	"quantmesh/exchange/accounting"
	"quantmesh/exchange/bybit"
	"quantmesh/exchange/okx"
)

func TestBitgetWrapperAccountEvidenceUnavailableWithoutAdapter(t *testing.T) {
	var source accounting.Source = (*bitgetWrapper)(nil)
	if _, err := source.ReadAccountEvidence(context.Background(), time.Time{}); err == nil {
		t.Fatal("nil Bitget adapter must fail closed")
	}
	if _, err := (&bitgetWrapper{}).ReadAccountEvidence(context.Background(), time.Time{}); err == nil {
		t.Fatal("missing Bitget adapter must fail closed")
	}
}

// TestOKXBybitInternalConstantsMirror okx/bybit 包因循環匯入鏡像了內部常量，這裡保證兩邊一致
func TestOKXBybitInternalConstantsMirror(t *testing.T) {
	pairs := []struct{ mirror, canonical string }{
		{okx.InternalSideBuy, string(SideBuy)}, {okx.InternalSideSell, string(SideSell)},
		{okx.InternalOrderTypeLimit, string(OrderTypeLimit)}, {okx.InternalOrderTypeMarket, string(OrderTypeMarket)},
		{okx.InternalStatusNew, string(OrderStatusNew)}, {okx.InternalStatusPartiallyFilled, string(OrderStatusPartiallyFilled)},
		{okx.InternalStatusFilled, string(OrderStatusFilled)}, {okx.InternalStatusCanceled, string(OrderStatusCanceled)},
		{okx.InternalStatusRejected, string(OrderStatusRejected)}, {okx.InternalStatusExpired, string(OrderStatusExpired)},
		{okx.InternalTimeInForceGTX, string(TimeInForceGTX)},
		{bybit.InternalSideBuy, string(SideBuy)}, {bybit.InternalSideSell, string(SideSell)},
		{bybit.InternalOrderTypeLimit, string(OrderTypeLimit)}, {bybit.InternalOrderTypeMarket, string(OrderTypeMarket)},
		{bybit.InternalStatusNew, string(OrderStatusNew)}, {bybit.InternalStatusPartiallyFilled, string(OrderStatusPartiallyFilled)},
		{bybit.InternalStatusFilled, string(OrderStatusFilled)}, {bybit.InternalStatusCanceled, string(OrderStatusCanceled)},
		{bybit.InternalStatusRejected, string(OrderStatusRejected)}, {bybit.InternalStatusExpired, string(OrderStatusExpired)},
		{bybit.InternalTimeInForceGTX, string(TimeInForceGTX)},
	}
	for _, p := range pairs {
		if p.mirror != p.canonical {
			t.Fatalf("鏡像常量 %q 與 exchange 常量 %q 不一致", p.mirror, p.canonical)
		}
	}
}

func TestOKXWrapperOrderConversion(t *testing.T) {
	req, err := toOKXOrderRequest(&OrderRequest{Side: SideBuy, Type: OrderTypeLimit, TimeInForce: TimeInForceGTX})
	if err != nil || req.Side != okx.SideBuy || req.Type != okx.OrderTypeLimit || !req.PostOnly {
		t.Fatalf("toOKXOrderRequest=%+v,%v", req, err)
	}
	if _, err := toOKXOrderRequest(&OrderRequest{Side: "HOLD", Type: OrderTypeLimit}); err == nil {
		t.Fatal("未知方向應報錯")
	}

	order, err := fromOKXOrder(&okx.Order{Side: okx.SideSell, Type: okx.OrderTypePostOnly, Status: okx.OrderStatusFilled})
	if err != nil || order.Side != SideSell || order.Type != OrderTypeLimit || order.Status != OrderStatusFilled {
		t.Fatalf("fromOKXOrder=%+v,%v", order, err)
	}
	if _, err := fromOKXOrder(&okx.Order{Side: okx.SideSell, Type: okx.OrderTypeLimit, Status: "unknown"}); err == nil {
		t.Fatal("未知狀態應報錯")
	}
}

func TestBybitWrapperOrderConversion(t *testing.T) {
	req, err := toBybitOrderRequest(&OrderRequest{Side: SideSell, Type: OrderTypeLimit, PostOnly: true})
	if err != nil || req.Side != bybit.SideSell || req.Type != bybit.OrderTypeLimit || req.TimeInForce != bybit.TimeInForcePO || !req.PostOnly {
		t.Fatalf("toBybitOrderRequest=%+v,%v", req, err)
	}
	order, err := fromBybitOrder(&bybit.Order{Side: bybit.SideBuy, Type: bybit.OrderTypeMarket, Status: bybit.OrderStatusCanceled})
	if err != nil || order.Side != SideBuy || order.Type != OrderTypeMarket || order.Status != OrderStatusCanceled {
		t.Fatalf("fromBybitOrder=%+v,%v", order, err)
	}
	if _, err := fromBybitOrder(&bybit.Order{Side: "BUY", Type: bybit.OrderTypeLimit, Status: bybit.OrderStatusNew}); err == nil {
		t.Fatal("非原生方向應報錯")
	}
}

func TestSpotWrapperOrderConversion(t *testing.T) {
	okxReq, err := toOKXSpotOrderRequest(&OrderRequest{Side: SideSell, Type: OrderTypeLimit, PostOnly: true, ReduceOnly: true})
	if err != nil || okxReq.Side != okx.SideSell || okxReq.Type != okx.OrderTypeLimit || !okxReq.PostOnly || okxReq.ReduceOnly {
		t.Fatalf("toOKXSpotOrderRequest=%+v,%v", okxReq, err)
	}
	if _, err := toOKXSpotOrderRequest(&OrderRequest{Side: SideBuy, Type: "STOP"}); err == nil {
		t.Fatal("OKX 現貨未知類型應報錯")
	}
	order, err := fromOKXOrder(&okx.Order{Side: okx.SideBuy, Type: okx.OrderTypeMarket, Status: okx.OrderStatusPartiallyFilled})
	if err != nil || order.Side != SideBuy || order.Type != OrderTypeMarket || order.Status != OrderStatusPartiallyFilled {
		t.Fatalf("fromOKXOrder=%+v,%v", order, err)
	}

	bybitReq, err := toBybitSpotOrderRequest(&OrderRequest{Side: SideBuy, Type: OrderTypeLimit, TimeInForce: TimeInForceGTX, ReduceOnly: true})
	if err != nil || bybitReq.Side != bybit.SideBuy || bybitReq.TimeInForce != bybit.TimeInForcePO || bybitReq.ReduceOnly {
		t.Fatalf("toBybitSpotOrderRequest=%+v,%v", bybitReq, err)
	}
	if _, err := toBybitSpotOrderRequest(&OrderRequest{Side: "HOLD", Type: OrderTypeLimit}); err == nil {
		t.Fatal("Bybit 現貨未知方向應報錯")
	}
	if _, err := fromBybitOrder(&bybit.Order{Side: bybit.SideSell, Type: bybit.OrderTypeLimit, Status: "Unknown"}); err == nil {
		t.Fatal("Bybit 未知狀態應報錯而非透傳")
	}
}
