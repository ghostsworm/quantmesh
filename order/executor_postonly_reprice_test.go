package order

import (
	"context"
	"errors"
	"strings"
	"testing"

	"quantmesh/exchange"
	"quantmesh/lock"
)

// postOnlyRejectExchange 前 rejects 次下單返回 PostOnly 拒單，其後成功
type postOnlyRejectExchange struct {
	*fakeOrderExchange
	rejects int
	calls   int
}

func (p *postOnlyRejectExchange) PlaceOrder(ctx context.Context, req *exchange.OrderRequest) (*exchange.Order, error) {
	p.calls++
	cp := *req
	p.placed = append(p.placed, &cp)
	if p.calls <= p.rejects {
		return nil, errors.New("code=-5022, msg=Due to the order could not be executed as maker, the Post Only order will be rejected")
	}
	return &exchange.Order{OrderID: int64(p.calls), ClientOrderID: req.ClientOrderID, Status: exchange.OrderStatusNew}, nil
}

func (p *postOnlyRejectExchange) GetPriceDecimals() int { return 2 }

func TestRepricePostOnly(t *testing.T) {
	tests := []struct {
		name     string
		price    float64
		side     string
		decimals int
		want     float64
		wantOK   bool
	}{
		{name: "BUY 下移一個 tick", price: 100.00, side: "BUY", decimals: 2, want: 99.99, wantOK: true},
		{name: "SELL 上移一個 tick", price: 100.00, side: "SELL", decimals: 2, want: 100.01, wantOK: true},
		{name: "整數精度", price: 3000, side: "sell", decimals: 0, want: 3001, wantOK: true},
		{name: "BUY 不可降到非正數", price: 0.01, side: "BUY", decimals: 2, want: 0.01, wantOK: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := repricePostOnly(tt.price, tt.side, tt.decimals)
			if ok != tt.wantOK || got != tt.want {
				t.Fatalf("repricePostOnly(%v,%s,%d) = %v,%v want %v,%v", tt.price, tt.side, tt.decimals, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}

// TestPlaceOrderPostOnlyRepricesNeverGTC PostOnly 被拒時逐 tick 遠離盤口重掛，始終保持 PostOnly，超過次數返回錯誤
func TestPlaceOrderPostOnlyRepricesNeverGTC(t *testing.T) {
	tests := []struct {
		name        string
		side        string
		rejects     int
		maxAttempts int
		wantErr     bool
		wantPrices  []float64
	}{
		{name: "BUY 被拒兩次後成功", side: "BUY", rejects: 2, maxAttempts: 3, wantPrices: []float64{100, 99.99, 99.98}},
		{name: "SELL 被拒一次後成功", side: "SELL", rejects: 1, maxAttempts: 3, wantPrices: []float64{100, 100.01}},
		{name: "超過重定價次數返回錯誤", side: "SELL", rejects: 10, maxAttempts: 2, wantErr: true, wantPrices: []float64{100, 100.01, 100.02}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ex := &postOnlyRejectExchange{fakeOrderExchange: &fakeOrderExchange{}, rejects: tt.rejects}
			oe := NewExchangeOrderExecutor(ex, "BTCUSDT", 0, 0, lock.NewNopLock(), "")
			oe.SetPostOnlyRepriceMaxAttempts(tt.maxAttempts)
			got, err := oe.PlaceOrder(&OrderRequest{Symbol: "BTCUSDT", Side: tt.side, Price: 100, Quantity: 1, PriceDecimals: 2, PostOnly: true, ClientOrderID: "cid"})
			if tt.wantErr {
				if err == nil || got != nil || !isPostOnlyError(err) || !strings.Contains(err.Error(), "PostOnly 重定價") {
					t.Fatalf("PlaceOrder = %#v, %v; want PostOnly error", got, err)
				}
			} else if err != nil || got == nil || got.Price != tt.wantPrices[len(tt.wantPrices)-1] {
				t.Fatalf("PlaceOrder = %#v, %v; want price %v", got, err, tt.wantPrices[len(tt.wantPrices)-1])
			}
			if len(ex.placed) != len(tt.wantPrices) {
				t.Fatalf("placed %d times, want %d", len(ex.placed), len(tt.wantPrices))
			}
			for i, req := range ex.placed {
				if !req.PostOnly {
					t.Fatalf("attempt %d downgraded to GTC", i)
				}
				if req.Price != tt.wantPrices[i] {
					t.Fatalf("attempt %d price = %v, want %v", i, req.Price, tt.wantPrices[i])
				}
			}
		})
	}
}
