package safety

import (
	"context"
	"fmt"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/exchange/income"
)

func TestFundingRateMonitorFetchBiasStatusAndLifecycle(t *testing.T) {
	cfg := &config.Config{}
	cfg.FundingRate.Enabled = true
	cfg.FundingRate.BiasEnabled = true
	cfg.FundingRate.HighRateThreshold = 0.001
	cfg.FundingRate.PauseBuyThreshold = 0.0015
	cfg.FundingRate.AlertThreshold = 0.001
	ex := &fundingMonitorExchange{rate: 0.0001}
	monitor := NewFundingRateMonitor(cfg, ex, "BTCUSDT")

	disabledCfg := &config.Config{}
	disabled := NewFundingRateMonitor(disabledCfg, ex, "BTCUSDT")
	disabled.Start(context.Background())
	if disabled.IsRunning() {
		t.Fatalf("disabled monitor should not start")
	}
	noExchange := NewFundingRateMonitor(cfg, nil, "BTCUSDT")
	noExchange.Start(context.Background())
	if noExchange.IsRunning() {
		t.Fatalf("nil exchange monitor should not start")
	}

	if err := monitor.fetchFundingRate(context.Background()); err != nil {
		t.Fatalf("fetch funding: %v", err)
	}
	if monitor.GetCurrentRate() != 0.0001 || monitor.GetLastUpdate().IsZero() || len(monitor.GetRateHistory()) != 1 {
		t.Fatalf("funding state rate=%f last=%s history=%d", monitor.GetCurrentRate(), monitor.GetLastUpdate(), len(monitor.GetRateHistory()))
	}
	if err := monitor.FetchFundingInfo(context.Background()); err != nil {
		t.Fatalf("fetch funding info: %v", err)
	}
	if monitor.GetNextFundingTime().IsZero() {
		t.Fatalf("next funding time should be set")
	}
	next := time.Now().Add(10 * time.Minute)
	monitor.SetNextFundingTime(next)
	if monitor.TimeUntilNextFunding() <= 0 || !monitor.IsNearFundingTime(15) || monitor.IsNearFundingTime(1) {
		t.Fatalf("near funding calculations mismatch")
	}
	monitor.SetMarkPrice(101)
	monitor.SetIndexPrice(100)
	if monitor.GetMarkPrice() != 101 || monitor.GetIndexPrice() != 100 {
		t.Fatalf("price setters failed")
	}

	cases := []struct {
		rate  float64
		bias  float64
		high  bool
		neg   bool
		pause bool
	}{
		{-0.0001, 1.2, false, true, false},
		{0.0001, 1.0, false, false, false},
		{0.0008, 0.7, false, false, false},
		{0.0012, 0.3, true, false, false},
		{0.0020, 0.0, true, false, true},
	}
	for _, tc := range cases {
		monitor.currentRate = tc.rate
		if got := monitor.GetBuyBias(); got != tc.bias {
			t.Fatalf("bias rate=%f got=%f want=%f", tc.rate, got, tc.bias)
		}
		if monitor.IsHighRate() != tc.high || monitor.IsNegativeRate() != tc.neg || monitor.ShouldPauseBuying() != tc.pause {
			t.Fatalf("flags mismatch for rate=%f", tc.rate)
		}
		monitor.checkAlert()
		monitor.logCurrentRate()
	}
	cfg.FundingRate.BiasEnabled = false
	if monitor.GetBuyBias() != 1.0 {
		t.Fatalf("disabled bias should be neutral")
	}
	cfg.FundingRate.BiasEnabled = true

	status := monitor.GetStatus()
	if status.Symbol != "BTCUSDT" || !status.Enabled || status.MarkPrice != 101 || status.IndexPrice != 100 {
		t.Fatalf("status=%#v", status)
	}
	cancelCtx, cancel := context.WithCancel(context.Background())
	monitor.Start(cancelCtx)
	if !monitor.IsRunning() {
		t.Fatalf("monitor should be running")
	}
	monitor.Start(cancelCtx)
	cancel()
	time.Sleep(20 * time.Millisecond)
	monitor.Stop()
	monitor.Stop()

	ex.err = fmt.Errorf("rate error")
	if err := monitor.fetchFundingRate(context.Background()); err == nil {
		t.Fatalf("fetch error should bubble")
	}
	if err := monitor.FetchFundingInfo(context.Background()); err == nil {
		t.Fatalf("info error should bubble")
	}
}

type fundingMonitorExchange struct {
	rate float64
	err  error
}

func (e *fundingMonitorExchange) GetName() string       { return "fake" }
func (e *fundingMonitorExchange) GetMarketType() string { return "futures" }
func (e *fundingMonitorExchange) PlaceOrder(context.Context, *exchange.OrderRequest) (*exchange.Order, error) {
	return nil, nil
}
func (e *fundingMonitorExchange) BatchPlaceOrders(context.Context, []*exchange.OrderRequest) ([]*exchange.Order, bool) {
	return nil, false
}
func (e *fundingMonitorExchange) CancelOrder(context.Context, string, int64) error { return nil }
func (e *fundingMonitorExchange) BatchCancelOrders(context.Context, string, []int64) error {
	return nil
}
func (e *fundingMonitorExchange) CancelAllOrders(context.Context, string) error { return nil }
func (e *fundingMonitorExchange) GetOrder(context.Context, string, int64) (*exchange.Order, error) {
	return nil, nil
}
func (e *fundingMonitorExchange) GetOpenOrders(context.Context, string) ([]*exchange.Order, error) {
	return nil, nil
}
func (e *fundingMonitorExchange) GetOrderFills(context.Context, string, int64) ([]*exchange.OrderFill, error) {
	return nil, nil
}
func (e *fundingMonitorExchange) GetAccount(context.Context) (*exchange.Account, error) {
	return nil, nil
}
func (e *fundingMonitorExchange) GetPositions(context.Context, string) ([]*exchange.Position, error) {
	return nil, nil
}
func (e *fundingMonitorExchange) GetBalance(context.Context, string) (float64, error) { return 0, nil }
func (e *fundingMonitorExchange) StartOrderStream(context.Context, func(interface{})) error {
	return nil
}
func (e *fundingMonitorExchange) StopOrderStream() error { return nil }
func (e *fundingMonitorExchange) GetLatestPrice(context.Context, string) (float64, error) {
	return 0, nil
}
func (e *fundingMonitorExchange) StartPriceStream(context.Context, string, func(float64)) error {
	return nil
}
func (e *fundingMonitorExchange) StartKlineStream(context.Context, []string, string, exchange.CandleUpdateCallback) error {
	return nil
}
func (e *fundingMonitorExchange) StopKlineStream() error { return nil }
func (e *fundingMonitorExchange) GetHistoricalKlines(context.Context, string, string, int) ([]*exchange.Candle, error) {
	return nil, nil
}
func (e *fundingMonitorExchange) GetPriceDecimals() int    { return 2 }
func (e *fundingMonitorExchange) GetQuantityDecimals() int { return 3 }
func (e *fundingMonitorExchange) GetBaseAsset() string     { return "BTC" }
func (e *fundingMonitorExchange) GetQuoteAsset() string    { return "USDT" }
func (e *fundingMonitorExchange) EstimateFinalOrderAmount(string, float64, float64, bool) float64 {
	return 0
}
func (e *fundingMonitorExchange) GetFundingRate(context.Context, string) (float64, error) {
	if e.err != nil {
		return 0, e.err
	}
	return e.rate, nil
}
func (e *fundingMonitorExchange) GetIncomeHistory(context.Context, string, string, int64, int64) ([]*income.Income, error) {
	return nil, nil
}
func (e *fundingMonitorExchange) GetSpotPrice(context.Context, string) (float64, error) {
	return 0, nil
}
func (e *fundingMonitorExchange) GetOrderBook(context.Context, string, int) (*exchange.OrderBook, error) {
	return nil, nil
}
func (e *fundingMonitorExchange) GetFundingInfo(context.Context, string) (*exchange.FundingInfo, error) {
	return nil, exchange.ErrNotImplemented
}
func (e *fundingMonitorExchange) InternalTransfer(context.Context, string, string, string, float64) (string, error) {
	return "", exchange.ErrNotImplemented
}
