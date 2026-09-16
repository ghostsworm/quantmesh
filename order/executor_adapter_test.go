package order

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"quantmesh/exchange"
	"quantmesh/lock"
)

func TestNewExchangeOrderExecutorTrimsBotID(t *testing.T) {
	var ex exchange.IExchange
	oe := NewExchangeOrderExecutor(ex, "BTCUSDT", 1, 100, nil, "  bid-1  ")
	if oe.botID != "bid-1" {
		t.Fatalf("botID=%q", oe.botID)
	}
}

func TestIsMarginInsufficientError_OKX51008(t *testing.T) {
	if !isMarginInsufficientError("下單失败: 51008 - insufficient balance") {
		t.Fatal("expected 51008 to be margin insufficient")
	}
	if isMarginInsufficientError("unknown") {
		t.Fatal("unexpected")
	}
}

type fakeOrderExchange struct {
	exchange.IExchange
	placed          []*exchange.OrderRequest
	cancelled       []int64
	batchCancelled  [][]int64
	batchCancelErr  error
	cancelErr       error
	order           *exchange.Order
	openOrders      []*exchange.Order
	quantityDecimal int
	placeErr        error
}

func (f *fakeOrderExchange) GetName() string { return "fake" }
func (f *fakeOrderExchange) PlaceOrder(ctx context.Context, req *exchange.OrderRequest) (*exchange.Order, error) {
	f.placed = append(f.placed, req)
	if f.placeErr != nil {
		return nil, f.placeErr
	}
	return &exchange.Order{
		OrderID:       int64(len(f.placed)),
		ClientOrderID: req.ClientOrderID,
		Status:        exchange.OrderStatusFilled,
		ExecutedQty:   req.Quantity,
	}, nil
}
func (f *fakeOrderExchange) CancelOrder(ctx context.Context, symbol string, orderID int64) error {
	f.cancelled = append(f.cancelled, orderID)
	return f.cancelErr
}
func (f *fakeOrderExchange) BatchCancelOrders(ctx context.Context, symbol string, orderIDs []int64) error {
	f.batchCancelled = append(f.batchCancelled, orderIDs)
	return f.batchCancelErr
}
func (f *fakeOrderExchange) GetOrder(ctx context.Context, symbol string, orderID int64) (*exchange.Order, error) {
	return f.order, nil
}
func (f *fakeOrderExchange) GetOpenOrders(ctx context.Context, symbol string) ([]*exchange.Order, error) {
	return f.openOrders, nil
}
func (f *fakeOrderExchange) GetQuantityDecimals() int { return f.quantityDecimal }
func (f *fakeOrderExchange) EstimateFinalOrderAmount(symbol string, price, quantity float64, reduceOnly bool) float64 {
	if reduceOnly {
		return price * quantity
	}
	return price*quantity + 1
}

type denyOrderLock struct {
	lock.DistributedLock
}

func (d denyOrderLock) TryLock(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	return false, nil
}

func TestExchangeOrderExecutorPlaceCancelAndQueryPaths(t *testing.T) {
	ex := &fakeOrderExchange{
		order:           &exchange.Order{Status: exchange.OrderStatusFilled, ExecutedQty: 0.5},
		openOrders:      []*exchange.Order{{OrderID: 7}},
		quantityDecimal: 3,
	}
	oe := NewExchangeOrderExecutor(ex, "BTCUSDT", 0, 0, lock.NewNopLock(), "bot-1")

	placed, err := oe.PlaceOrder(&OrderRequest{
		Symbol:        "BTCUSDT",
		Side:          "BUY",
		Price:         50000,
		Quantity:      0.01,
		PriceDecimals: 2,
		PostOnly:      true,
		ClientOrderID: "cid-1",
		StrategyName:  "grid",
		StrategyType:  "grid",
	})
	if err != nil {
		t.Fatalf("PlaceOrder returned error: %v", err)
	}
	if placed.OrderID != 1 || placed.ClientOrderID != "cid-1" || len(ex.placed) != 1 {
		t.Fatalf("unexpected placed order: %#v placed=%#v", placed, ex.placed)
	}
	if !ex.placed[0].PostOnly || ex.placed[0].StrategyName != "grid" {
		t.Fatalf("exchange request not populated: %#v", ex.placed[0])
	}

	skippedExecutor := NewExchangeOrderExecutor(ex, "BTCUSDT", 0, 0, denyOrderLock{}, "")
	skipped, err := skippedExecutor.PlaceOrder(&OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Price: 50000, Quantity: 0.01})
	if !errors.Is(err, ErrLockNotAcquired) || skipped != nil {
		t.Fatalf("locked PlaceOrder = %#v err=%v, want nil ErrLockNotAcquired", skipped, err)
	}

	if err := oe.CancelOrder(99); err != nil {
		t.Fatalf("CancelOrder returned error: %v", err)
	}
	ex.cancelErr = errors.New("Unknown order sent")
	if err := oe.CancelOrder(100); err != nil {
		t.Fatalf("unknown order cancel should be ignored: %v", err)
	}

	status, executed, err := oe.CheckOrderStatus(7)
	if err != nil || status != string(exchange.OrderStatusFilled) || executed != 0.5 {
		t.Fatalf("CheckOrderStatus = %q %f err=%v", status, executed, err)
	}
	open, err := oe.GetOpenOrders()
	if err != nil || len(open) != 1 {
		t.Fatalf("GetOpenOrders = %#v err=%v", open, err)
	}
	if oe.GetQuantityDecimals() != 3 {
		t.Fatalf("quantity decimals = %d, want 3", oe.GetQuantityDecimals())
	}
	if got := oe.RoundQuantity(1.2349); got != 1.234 {
		t.Fatalf("RoundQuantity = %f, want 1.234", got)
	}
	if got := oe.EstimateFinalOrderAmount("BTCUSDT", 10, 2, false); got != 21 {
		t.Fatalf("EstimateFinalOrderAmount = %f, want 21", got)
	}
	if oe.GetSymbol() != "BTCUSDT" {
		t.Fatalf("GetSymbol = %s", oe.GetSymbol())
	}
}

func TestExchangeOrderExecutorBatchPaths(t *testing.T) {
	ex := &fakeOrderExchange{quantityDecimal: 2}
	oe := NewExchangeOrderExecutor(ex, "BTCUSDT", 0, 0, lock.NewNopLock(), "")

	orders, hasMargin := oe.BatchPlaceOrders([]*OrderRequest{
		{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1},
		{Symbol: "BTCUSDT", Side: "SELL", Price: 101, Quantity: 1},
	})
	if hasMargin || len(orders) != 2 {
		t.Fatalf("BatchPlaceOrders = len %d margin %v", len(orders), hasMargin)
	}

	if err := oe.BatchCancelOrders(nil); err != nil {
		t.Fatalf("empty BatchCancelOrders returned error: %v", err)
	}
	ex.batchCancelErr = errors.New("batch unavailable")
	if err := oe.BatchCancelOrders([]int64{1, 2}); err != nil {
		t.Fatalf("fallback BatchCancelOrders returned error: %v", err)
	}
	if len(ex.batchCancelled) != 1 || len(ex.cancelled) != 2 {
		t.Fatalf("unexpected cancel calls: batch=%#v single=%#v", ex.batchCancelled, ex.cancelled)
	}
}

func TestOrderErrorClassifiers(t *testing.T) {
	if !isPostOnlyError(errors.New("Post Only order will be rejected")) {
		t.Fatal("expected post-only error")
	}
	if isPostOnlyError(nil) || isPostOnlyError(errors.New("other")) {
		t.Fatal("unexpected post-only classification")
	}
	if !isReduceOnlyError(errors.New("-2022 ReduceOnly Order is rejected")) {
		t.Fatal("expected reduce-only error")
	}
	if isReduceOnlyError(nil) || isReduceOnlyError(errors.New("-4164 reduce only notional too small")) {
		t.Fatal("unexpected reduce-only classification")
	}
}

type errOrderLock struct {
	lock.DistributedLock
}

func (e errOrderLock) TryLock(ctx context.Context, key string, ttl time.Duration) (bool, error) {
	return false, errors.New("redis down")
}

// TestPlaceOrderLockOutcomes 鎖未獲取返回哨兵錯誤、鎖服務異常 fail closed，均不向交易所提交
func TestPlaceOrderLockOutcomes(t *testing.T) {
	tests := []struct {
		name         string
		lock         lock.DistributedLock
		wantSentinel bool
	}{
		{name: "lock held by other instance", lock: denyOrderLock{}, wantSentinel: true},
		{name: "lock backend error fails closed", lock: errOrderLock{}, wantSentinel: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ex := &fakeOrderExchange{}
			oe := NewExchangeOrderExecutor(ex, "BTCUSDT", 0, 0, tt.lock, "")
			got, err := oe.PlaceOrder(&OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1})
			if err == nil || got != nil {
				t.Fatalf("PlaceOrder = %#v, %v; want nil order and error", got, err)
			}
			if errors.Is(err, ErrLockNotAcquired) != tt.wantSentinel {
				t.Fatalf("errors.Is(ErrLockNotAcquired) = %v, want %v (err=%v)", !tt.wantSentinel, tt.wantSentinel, err)
			}
			if len(ex.placed) != 0 {
				t.Fatalf("exchange PlaceOrder called %d times, want 0", len(ex.placed))
			}
		})
	}
}

// TestBatchPlaceOrdersSkipsLockedWithoutNil 批量下單時被鎖跳過的订單不得以 nil 形式加入結果
func TestBatchPlaceOrdersSkipsLockedWithoutNil(t *testing.T) {
	oe := NewExchangeOrderExecutor(&fakeOrderExchange{}, "BTCUSDT", 0, 0, denyOrderLock{}, "")
	res := oe.BatchPlaceOrdersWithDetails([]*OrderRequest{
		{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1, ClientOrderID: "a"},
		{Symbol: "BTCUSDT", Side: "BUY", Price: 90, Quantity: 1, ClientOrderID: "b"},
	})
	if len(res.PlacedOrders) != 0 || res.HasMarginError {
		t.Fatalf("result = %#v, want no placed orders", res)
	}
	for _, o := range res.PlacedOrders {
		if o == nil {
			t.Fatal("nil order in PlacedOrders")
		}
	}
}

// TestPlaceOrderAmbiguousFailureLooksUpClientOrderID 重試耗盡後按 ClientOrderID 回查，已受理則視為成功
func TestPlaceOrderAmbiguousFailureLooksUpClientOrderID(t *testing.T) {
	tests := []struct {
		name       string
		cid        string
		openOrders []*exchange.Order
		wantFound  bool
		wantID     int64
	}{
		{name: "exact cid found", cid: "cid-1", openOrders: []*exchange.Order{{OrderID: 42, ClientOrderID: "cid-1", Status: exchange.OrderStatusNew}}, wantFound: true, wantID: 42},
		{name: "other cid not matched", cid: "cid-1", openOrders: []*exchange.Order{{OrderID: 43, ClientOrderID: "cid-2"}}, wantFound: false},
		{name: "empty cid skips lookup", cid: "", openOrders: []*exchange.Order{{OrderID: 44, ClientOrderID: ""}}, wantFound: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ex := &fakeOrderExchange{placeErr: errors.New("context deadline exceeded"), openOrders: tt.openOrders}
			oe := NewExchangeOrderExecutor(ex, "BTCUSDT", 0, 0, lock.NewNopLock(), "")
			got, err := oe.PlaceOrder(&OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1, ClientOrderID: tt.cid})
			if tt.wantFound {
				if err != nil || got == nil || got.OrderID != tt.wantID {
					t.Fatalf("PlaceOrder = %#v, %v; want order %d", got, err, tt.wantID)
				}
				return
			}
			if err == nil || got != nil {
				t.Fatalf("PlaceOrder = %#v, %v; want failure", got, err)
			}
		})
	}
}

// fakeCIDQuerierExchange 實現 exchange.OrderByClientIDQuerier 的假交易所
type fakeCIDQuerierExchange struct {
	*fakeOrderExchange
	byCID       map[string]*exchange.Order
	queryErr    error
	queryCalls  int
	openScanned bool
}

func (f *fakeCIDQuerierExchange) GetOrderByClientOrderID(ctx context.Context, symbol, clientOrderID string) (*exchange.Order, error) {
	f.queryCalls++
	if f.queryErr != nil {
		return nil, f.queryErr
	}
	return f.byCID[clientOrderID], nil
}

func (f *fakeCIDQuerierExchange) GetOpenOrders(ctx context.Context, symbol string) ([]*exchange.Order, error) {
	f.openScanned = true
	return f.fakeOrderExchange.GetOpenOrders(ctx, symbol)
}

// TestFindOrderByClientOrderIDUsesQuerier 交易所支持按 ClientOrderID 查單時優先直查（可找回已成交訂單），出錯才掃描挂單
func TestFindOrderByClientOrderIDUsesQuerier(t *testing.T) {
	tests := []struct {
		name         string
		byCID        map[string]*exchange.Order
		queryErr     error
		openOrders   []*exchange.Order
		wantID       int64
		wantFound    bool
		wantOpenScan bool
	}{
		{name: "直查找到已成交訂單", byCID: map[string]*exchange.Order{"cid-1": {OrderID: 9, ClientOrderID: "cid-1", Status: exchange.OrderStatusFilled}}, wantID: 9, wantFound: true},
		{name: "直查確認不存在不再掃描", byCID: map[string]*exchange.Order{}, openOrders: []*exchange.Order{{OrderID: 5, ClientOrderID: "cid-1"}}},
		{name: "直查出錯回退掃描挂單", queryErr: errors.New("timeout"), openOrders: []*exchange.Order{{OrderID: 5, ClientOrderID: "cid-1"}}, wantID: 5, wantFound: true, wantOpenScan: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ex := &fakeCIDQuerierExchange{
				fakeOrderExchange: &fakeOrderExchange{openOrders: tt.openOrders},
				byCID:             tt.byCID,
				queryErr:          tt.queryErr,
			}
			oe := NewExchangeOrderExecutor(ex, "BTCUSDT", 0, 0, lock.NewNopLock(), "")
			got := oe.findOrderByClientOrderID(&OrderRequest{Symbol: "BTCUSDT", Side: "BUY", ClientOrderID: "cid-1"})
			if ex.queryCalls != 1 {
				t.Fatalf("queryCalls = %d, want 1", ex.queryCalls)
			}
			if (got != nil) != tt.wantFound || (got != nil && got.OrderID != tt.wantID) {
				t.Fatalf("found = %#v, want id %d found %v", got, tt.wantID, tt.wantFound)
			}
			if ex.openScanned != tt.wantOpenScan {
				t.Fatalf("openScanned = %v, want %v", ex.openScanned, tt.wantOpenScan)
			}
		})
	}
}

// perOrderCancelExchange 按訂單返回撤單錯誤
type perOrderCancelExchange struct {
	*fakeOrderExchange
	cancelErrs map[int64]error
}

func (p *perOrderCancelExchange) CancelOrder(ctx context.Context, symbol string, orderID int64) error {
	p.cancelled = append(p.cancelled, orderID)
	return p.cancelErrs[orderID]
}

// TestBatchCancelOrdersReturnsAggregatedErrors 批量撤單失敗後逐個撤單，不存在視為成功，其餘錯誤匯總返回
func TestBatchCancelOrdersReturnsAggregatedErrors(t *testing.T) {
	boom := errors.New("boom")
	tests := []struct {
		name       string
		cancelErrs map[int64]error
		wantErr    bool
	}{
		{name: "逐個全部成功", cancelErrs: map[int64]error{}},
		{name: "不存在視為成功", cancelErrs: map[int64]error{1: errors.New("-2011 Unknown order sent")}},
		{name: "真實失敗匯總返回", cancelErrs: map[int64]error{2: boom}, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ex := &perOrderCancelExchange{
				fakeOrderExchange: &fakeOrderExchange{batchCancelErr: errors.New("batch down")},
				cancelErrs:        tt.cancelErrs,
			}
			oe := NewExchangeOrderExecutor(ex, "BTCUSDT", 0, 0, lock.NewNopLock(), "")
			err := oe.BatchCancelOrders([]int64{1, 2})
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if tt.wantErr && (!errors.Is(err, boom) || !strings.Contains(err.Error(), "order 2")) {
				t.Fatalf("錯誤應包含訂單 2 的原因並可 errors.Is: %v", err)
			}
		})
	}
}
