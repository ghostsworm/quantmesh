package exchange

import (
	"context"
	"errors"
	"testing"
	"time"

	"quantmesh/exchange/income"
)

type fakeExchange struct {
	openOrdersErr error
	order         *Order
}

func (f *fakeExchange) GetName() string { return "fake" }
func (f *fakeExchange) GetMarketType() string {
	return "futures"
}
func (f *fakeExchange) PlaceOrder(context.Context, *OrderRequest) (*Order, error) {
	return nil, errors.New("write should be intercepted")
}
func (f *fakeExchange) BatchPlaceOrders(context.Context, []*OrderRequest) ([]*Order, bool) {
	return nil, true
}
func (f *fakeExchange) CancelOrder(context.Context, string, int64) error { return nil }
func (f *fakeExchange) BatchCancelOrders(context.Context, string, []int64) error {
	return nil
}
func (f *fakeExchange) CancelAllOrders(context.Context, string) error { return nil }
func (f *fakeExchange) GetOrder(context.Context, string, int64) (*Order, error) {
	if f.order != nil {
		return f.order, nil
	}
	return &Order{OrderID: 42, Status: OrderStatusFilled}, nil
}
func (f *fakeExchange) GetOpenOrders(context.Context, string) ([]*Order, error) {
	if f.openOrdersErr != nil {
		return nil, f.openOrdersErr
	}
	return []*Order{{OrderID: 7, Symbol: "BTCUSDT", Status: OrderStatusNew}}, nil
}
func (f *fakeExchange) GetOrderFills(context.Context, string, int64) ([]*OrderFill, error) {
	return []*OrderFill{{TradeID: "fill-1"}}, nil
}
func (f *fakeExchange) GetAccount(context.Context) (*Account, error) {
	return &Account{AvailableBalance: 100}, nil
}
func (f *fakeExchange) GetPositions(context.Context, string) ([]*Position, error) {
	return []*Position{{Symbol: "BTCUSDT", Size: 1}}, nil
}
func (f *fakeExchange) GetBalance(context.Context, string) (float64, error) { return 99, nil }
func (f *fakeExchange) StartOrderStream(context.Context, func(interface{})) error {
	return nil
}
func (f *fakeExchange) StopOrderStream() error { return nil }
func (f *fakeExchange) GetLatestPrice(context.Context, string) (float64, error) {
	return 123.45, nil
}
func (f *fakeExchange) StartPriceStream(context.Context, string, func(float64)) error {
	return nil
}
func (f *fakeExchange) StartKlineStream(context.Context, []string, string, CandleUpdateCallback) error {
	return nil
}
func (f *fakeExchange) StopKlineStream() error { return nil }
func (f *fakeExchange) GetHistoricalKlines(context.Context, string, string, int) ([]*Candle, error) {
	return []*Candle{{Symbol: "BTCUSDT", Open: 1, High: 2, Low: 1, Close: 2}}, nil
}
func (f *fakeExchange) GetPriceDecimals() int    { return 2 }
func (f *fakeExchange) GetQuantityDecimals() int { return 3 }
func (f *fakeExchange) GetBaseAsset() string     { return "BTC" }
func (f *fakeExchange) GetQuoteAsset() string    { return "USDT" }
func (f *fakeExchange) EstimateFinalOrderAmount(string, float64, float64, bool) float64 {
	return 88
}
func (f *fakeExchange) GetFundingRate(context.Context, string) (float64, error) {
	return 0.0001, nil
}
func (f *fakeExchange) GetIncomeHistory(context.Context, string, string, int64, int64) ([]*income.Income, error) {
	return []*income.Income{{Symbol: "BTCUSDT"}}, nil
}
func (f *fakeExchange) GetSpotPrice(context.Context, string) (float64, error) {
	return 122.22, nil
}
func (f *fakeExchange) GetOrderBook(context.Context, string, int) (*OrderBook, error) {
	return &OrderBook{Symbol: "BTCUSDT"}, nil
}
func (f *fakeExchange) GetFundingInfo(context.Context, string) (*FundingInfo, error) {
	return &FundingInfo{Symbol: "BTCUSDT", Rate: 0.0001}, nil
}
func (f *fakeExchange) InternalTransfer(context.Context, string, string, string, float64) (string, error) {
	return "transfer-1", nil
}

type fakeFixedFundingPricer struct {
	rateErr error
}

func (f fakeFixedFundingPricer) GetFundingRate(context.Context) (float64, error) {
	return 0.0002, f.rateErr
}

func (f fakeFixedFundingPricer) GetLatestPrice(context.Context, string) (float64, error) {
	return 321, nil
}

func TestDryRunWrapperInterceptsWritesAndForwardsReads(t *testing.T) {
	ctx := context.Background()
	wrapped := &fakeExchange{}
	dry := NewDryRunWrapper(wrapped)

	if dry.GetName() != "fake [DryRun]" || dry.GetMarketType() != "futures" {
		t.Fatalf("unexpected wrapper identity")
	}

	order, err := dry.PlaceOrder(ctx, &OrderRequest{
		Symbol:        "BTCUSDT",
		Side:          SideBuy,
		Type:          OrderTypeLimit,
		Price:         100,
		Quantity:      0.5,
		ClientOrderID: "cid-1",
	})
	if err != nil || order.OrderID <= 1_000_000 || order.Status != OrderStatusNew || order.ClientOrderID != "cid-1" {
		t.Fatalf("dry order=%#v err=%v", order, err)
	}
	orders, marginErr := dry.BatchPlaceOrders(ctx, []*OrderRequest{
		{Symbol: "BTCUSDT", Side: SideBuy, Type: OrderTypeLimit, Price: 101, Quantity: 0.1},
		{Symbol: "ETHUSDT", Side: SideSell, Type: OrderTypeLimit, Price: 202, Quantity: 0.2},
	})
	if marginErr || len(orders) != 2 {
		t.Fatalf("batch orders=%#v margin=%v", orders, marginErr)
	}

	if got, err := dry.GetOrder(ctx, "BTCUSDT", order.OrderID); err != nil || got.OrderID != order.OrderID {
		t.Fatalf("get simulated order=%#v err=%v", got, err)
	}
	if got, err := dry.GetOrder(ctx, "BTCUSDT", 42); err != nil || got.OrderID != 42 {
		t.Fatalf("get real order=%#v err=%v", got, err)
	}

	open, err := dry.GetOpenOrders(ctx, "BTCUSDT")
	if err != nil || len(open) != 3 {
		t.Fatalf("open orders len=%d err=%v", len(open), err)
	}
	if err := dry.CancelOrder(ctx, "BTCUSDT", order.OrderID); err != nil {
		t.Fatalf("cancel simulated order: %v", err)
	}
	if got, _ := dry.GetOrder(ctx, "BTCUSDT", order.OrderID); got.Status != OrderStatusCanceled {
		t.Fatalf("canceled order status = %s", got.Status)
	}
	if err := dry.BatchCancelOrders(ctx, "ETHUSDT", []int64{orders[1].OrderID}); err != nil {
		t.Fatalf("batch cancel: %v", err)
	}
	if err := dry.CancelAllOrders(ctx, "ETHUSDT"); err != nil {
		t.Fatalf("cancel all: %v", err)
	}

	wrapped.openOrdersErr = errors.New("boom")
	open, err = dry.GetOpenOrders(ctx, "BTCUSDT")
	if err != nil || len(open) != 1 {
		t.Fatalf("open fallback len=%d err=%v", len(open), err)
	}

	if acct, _ := dry.GetAccount(ctx); acct.AvailableBalance != 100 {
		t.Fatalf("account not forwarded: %#v", acct)
	}
	if pos, _ := dry.GetPositions(ctx, "BTCUSDT"); len(pos) != 1 {
		t.Fatalf("positions not forwarded: %#v", pos)
	}
	if balance, _ := dry.GetBalance(ctx, "USDT"); balance != 99 {
		t.Fatalf("balance = %v", balance)
	}
	if price, _ := dry.GetLatestPrice(ctx, "BTCUSDT"); price != 123.45 {
		t.Fatalf("latest price = %v", price)
	}
	if spot, _ := dry.GetSpotPrice(ctx, "BTCUSDT"); spot != 122.22 {
		t.Fatalf("spot price = %v", spot)
	}
	if dry.GetPriceDecimals() != 2 || dry.GetQuantityDecimals() != 3 || dry.GetBaseAsset() != "BTC" || dry.GetQuoteAsset() != "USDT" {
		t.Fatalf("precision/assets not forwarded")
	}
	if dry.EstimateFinalOrderAmount("BTCUSDT", 1, 2, false) != 88 {
		t.Fatalf("estimate not forwarded")
	}
	if rate, _ := dry.GetFundingRate(ctx, "BTCUSDT"); rate != 0.0001 {
		t.Fatalf("funding rate = %v", rate)
	}
	if info, _ := dry.GetFundingInfo(ctx, "BTCUSDT"); info.Symbol != "BTCUSDT" {
		t.Fatalf("funding info = %#v", info)
	}
	if fills, _ := dry.GetOrderFills(ctx, "BTCUSDT", order.OrderID); len(fills) != 1 {
		t.Fatalf("fills = %#v", fills)
	}
	if incomes, _ := dry.GetIncomeHistory(ctx, "BTCUSDT", "", 1, 2); len(incomes) != 1 {
		t.Fatalf("income = %#v", incomes)
	}
	if book, _ := dry.GetOrderBook(ctx, "BTCUSDT", 5); book.Symbol != "BTCUSDT" {
		t.Fatalf("book = %#v", book)
	}
	if klines, _ := dry.GetHistoricalKlines(ctx, "BTCUSDT", "1m", 1); len(klines) != 1 {
		t.Fatalf("klines = %#v", klines)
	}
	if _, err := dry.InternalTransfer(ctx, "a", "b", "USDT", 1); !errors.Is(err, ErrNotImplemented) {
		t.Fatalf("dry transfer error = %v", err)
	}
	if dry.GetWrappedExchange() != wrapped {
		t.Fatalf("wrapped exchange mismatch")
	}
	if len(dry.GetSimulatedOrders()) != 3 {
		t.Fatalf("simulated orders copy mismatch")
	}
	dry.ClearSimulatedOrders()
	if len(dry.GetSimulatedOrders()) != 0 {
		t.Fatalf("simulated orders should be cleared")
	}
}

func TestExchangeFundingPermissionsCandlesAndSpikeFilter(t *testing.T) {
	cases := []struct {
		now  time.Time
		want int
	}{
		{time.Date(2026, 6, 4, 1, 0, 0, 0, time.UTC), 8},
		{time.Date(2026, 6, 4, 9, 0, 0, 0, time.UTC), 16},
		{time.Date(2026, 6, 4, 17, 0, 0, 0, time.UTC), 0},
	}
	for _, tc := range cases {
		got := EstimateNextFundingUTC8h(tc.now)
		if got.Hour() != tc.want {
			t.Fatalf("next funding for %v = %v", tc.now, got)
		}
	}
	if got := EstimateNextFundingKrakenHourlyUTC(time.Date(2026, 6, 4, 9, 30, 0, 0, time.UTC)); got.Hour() != 10 || got.Minute() != 0 {
		t.Fatalf("kraken hourly funding = %v", got)
	}

	info := FundingInfoFromRateEstimate("BTCUSDT", 0.001, 100, 99)
	if info.Symbol != "BTCUSDT" || info.Rate != 0.001 || info.MarkPrice != 100 || info.IndexPrice != 99 {
		t.Fatalf("estimated funding info = %#v", info)
	}
	fallback, err := FundingInfoFallbackFromRate(context.Background(), "BTCUSDT", &fakeExchange{})
	if err != nil || fallback.Rate != 0.0001 || fallback.MarkPrice != 123.45 {
		t.Fatalf("funding fallback = %#v err=%v", fallback, err)
	}
	fixed, err := FundingInfoFallbackFromRateFixedSymbol(context.Background(), "BTCUSDT", fakeFixedFundingPricer{})
	if err != nil || fixed.Rate != 0.0002 || fixed.MarkPrice != 321 {
		t.Fatalf("fixed funding fallback = %#v err=%v", fixed, err)
	}
	_, err = FundingInfoFallbackFromRateFixedSymbol(context.Background(), "BTCUSDT", fakeFixedFundingPricer{rateErr: errors.New("rate")})
	if err == nil {
		t.Fatalf("fixed funding error should propagate")
	}

	perms := &APIPermissions{CanTrade: true, CanWithdraw: true, CanTransfer: true}
	perms.CalculateSecurityScore()
	if perms.SecurityScore != 0 || perms.RiskLevel != "high" || perms.IsSecure() {
		t.Fatalf("dangerous permissions = %#v", perms)
	}
	if warnings := perms.GetWarnings(); len(warnings) != 3 {
		t.Fatalf("warnings = %#v", warnings)
	}
	perms = &APIPermissions{CanTrade: true, IPRestricted: true}
	perms.CalculateSecurityScore()
	if perms.SecurityScore != 100 || perms.RiskLevel != "low" || !perms.IsSecure() {
		t.Fatalf("safe permissions = %#v", perms)
	}
	perms = &APIPermissions{CanRead: true}
	if warnings := perms.GetWarnings(); len(warnings) != 2 {
		t.Fatalf("read-only warnings = %#v", warnings)
	}

	valid := &Candle{Open: 10, High: 12, Low: 9, Close: 11}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid candle failed: %v", err)
	}
	for _, candle := range []*Candle{
		{Open: 0, High: 1, Low: 1, Close: 1},
		{Open: 1, High: 0.5, Low: 2, Close: 1},
		{Open: 1, High: 1, Low: 0.5, Close: 2},
		{Open: 1, High: 2, Low: 0.5, Close: 1, Volume: -1},
	} {
		if err := candle.Validate(); err == nil {
			t.Fatalf("invalid candle should fail: %#v", candle)
		}
	}

	candles := []*Candle{
		{Symbol: "BTCUSDT", Open: 100, High: 500, Low: 1, Close: 101, Timestamp: 1},
		{Symbol: "BTCUSDT", Open: 101, High: 102, Low: 100, Close: 101, Timestamp: 2},
	}
	clipped := ClipKlineSpikes(candles, 0.05)
	if clipped[0].High > 106.1 || clipped[0].Low < 95 {
		t.Fatalf("spike not clipped: %#v", clipped[0])
	}
	if ClipKlineSpikes(nil, 0.05) != nil {
		t.Fatalf("nil candles should stay nil")
	}
}
