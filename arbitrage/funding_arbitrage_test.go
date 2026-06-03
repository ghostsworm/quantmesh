package arbitrage

import (
	"context"
	"errors"
	"testing"
	"time"

	"quantmesh/config"
)

type fakeArbitrageExchange struct {
	name        string
	marketType  string
	baseAsset   string
	quoteAsset  string
	latestPrice float64
	balance     float64
	positionErr error
	priceErr    error
	orderErr    error
	orders      []*OrderRequest
}

func (f *fakeArbitrageExchange) GetName() string       { return f.name }
func (f *fakeArbitrageExchange) GetMarketType() string { return f.marketType }
func (f *fakeArbitrageExchange) GetBaseAsset() string  { return f.baseAsset }
func (f *fakeArbitrageExchange) GetQuoteAsset() string { return f.quoteAsset }

func (f *fakeArbitrageExchange) PlaceOrder(ctx context.Context, req interface{}) (interface{}, error) {
	if f.orderErr != nil {
		return nil, f.orderErr
	}
	orderReq, ok := req.(*OrderRequest)
	if !ok {
		return nil, errors.New("unexpected order request")
	}
	f.orders = append(f.orders, orderReq)
	return &Order{OrderID: int64(len(f.orders)), Symbol: orderReq.Symbol, Side: orderReq.Side}, nil
}

func (f *fakeArbitrageExchange) CancelOrder(ctx context.Context, symbol string, orderID int64) error {
	return nil
}

func (f *fakeArbitrageExchange) GetPositions(ctx context.Context, symbol string) (interface{}, error) {
	if f.positionErr != nil {
		return nil, f.positionErr
	}
	return []*Position{{Symbol: symbol, Size: 1}}, nil
}

func (f *fakeArbitrageExchange) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	if f.priceErr != nil {
		return 0, f.priceErr
	}
	return f.latestPrice, nil
}

func (f *fakeArbitrageExchange) GetBalance(ctx context.Context, asset string) (float64, error) {
	return f.balance, nil
}

type fakeFundingMonitor struct {
	rate float64
}

func (f fakeFundingMonitor) GetBuyBias() float64           { return 0 }
func (f fakeFundingMonitor) IsHighRate() bool              { return f.rate > 0 }
func (f fakeFundingMonitor) GetCurrentRate() float64       { return f.rate }
func (f fakeFundingMonitor) ShouldPauseBuying() bool       { return false }
func (f fakeFundingMonitor) GetNextFundingTime() time.Time { return time.Time{} }

func TestParsePositionSize(t *testing.T) {
	tests := []struct {
		name      string
		positions []*Position
		want      float64
	}{
		{name: "nil positions", positions: nil, want: 0},
		{name: "empty positions", positions: []*Position{}, want: 0},
		{
			name: "sums long and short positions",
			positions: []*Position{
				{Symbol: "BTCUSDT", Size: 1.25},
				{Symbol: "BTCUSDT", Size: -0.5},
				{Symbol: "ETHUSDT", Size: 2},
			},
			want: 2.75,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parsePositionSize(tt.positions); got != tt.want {
				t.Fatalf("parsePositionSize() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestFundingArbitrageSyncStatusAndHedgeStatus(t *testing.T) {
	ctx := context.Background()
	cfg := &config.Config{}
	cfg.FundingRate.ArbitrageEnabled = true
	cfg.FundingRate.HedgeRateThreshold = 0.001
	futures := &fakeArbitrageExchange{baseAsset: "BTC", quoteAsset: "USDT"}
	spot := &fakeArbitrageExchange{baseAsset: "BTC", quoteAsset: "USDT", balance: 1.5}
	manager := NewFundingArbitrageManager(cfg, futures, spot, fakeFundingMonitor{rate: 0.002}, "BTCUSDT")

	if err := manager.SyncHedge(ctx); err != nil {
		t.Fatalf("SyncHedge() error = %v", err)
	}
	status := manager.GetHedgeStatus()
	if !status.Enabled || status.SpotPosition != 1.5 || status.SyncStatus != "synced" {
		t.Fatalf("GetHedgeStatus() = %+v, want synced enabled spot balance", status)
	}
	if status.CurrentRate != 0.002 {
		t.Fatalf("CurrentRate = %v, want monitor rate", status.CurrentRate)
	}

	futures.positionErr = errors.New("positions unavailable")
	if err := manager.SyncHedge(ctx); err == nil {
		t.Fatalf("SyncHedge() expected position error")
	}
	if got := manager.GetHedgeStatus().SyncStatus; got != "error" {
		t.Fatalf("SyncStatus = %q, want error", got)
	}
}

func TestFundingArbitrageSpreadAndSpotOrders(t *testing.T) {
	ctx := context.Background()
	cfg := &config.Config{}
	cfg.FundingRate.ArbitrageEnabled = true
	cfg.FundingRate.HedgeRateThreshold = 0.001
	cfg.FundingRate.HedgeMinPosition = 100
	cfg.FundingRate.MaxSpreadPercent = 1

	spot := &fakeArbitrageExchange{latestPrice: 100, baseAsset: "BTC", quoteAsset: "USDT"}
	manager := NewFundingArbitrageManager(cfg, nil, spot, fakeFundingMonitor{rate: 0.002}, "BTCUSDT")

	if !manager.shouldHedge() {
		t.Fatalf("shouldHedge() = false, want true")
	}
	if !manager.checkSpread(ctx, 100.5) {
		t.Fatalf("checkSpread() = false for spread within threshold")
	}
	if manager.checkSpread(ctx, 103) {
		t.Fatalf("checkSpread() = true for spread above threshold")
	}

	manager.adjustHedgeForBuy(ctx, 2, 100)
	if manager.hedgedAmount != 2 || manager.spotPosition != 2 {
		t.Fatalf("after buy hedge amount=%v spot=%v, want 2/2", manager.hedgedAmount, manager.spotPosition)
	}
	if len(spot.orders) != 1 || spot.orders[0].Side != "BUY" || spot.orders[0].Price != 100.1 {
		t.Fatalf("buy order = %+v", spot.orders)
	}

	manager.adjustHedgeForSell(ctx, 0.75, 100)
	if manager.hedgedAmount != 1.25 || manager.spotPosition != 1.25 {
		t.Fatalf("after sell hedge amount=%v spot=%v, want 1.25/1.25", manager.hedgedAmount, manager.spotPosition)
	}
	if len(spot.orders) != 2 || spot.orders[1].Side != "SELL" || spot.orders[1].Price != 99.9 {
		t.Fatalf("sell order = %+v", spot.orders)
	}

	status := manager.GetHedgeStatus()
	if status.EstimatedSavings != 0.002*1.25 {
		t.Fatalf("EstimatedSavings = %v, want %v", status.EstimatedSavings, 0.002*1.25)
	}
}

func TestFundingArbitragePositionChangesAndCloseAll(t *testing.T) {
	ctx := context.Background()
	cfg := &config.Config{}
	cfg.FundingRate.ArbitrageEnabled = true
	cfg.FundingRate.HedgeRateThreshold = 0.001
	cfg.FundingRate.HedgeMinPosition = 50
	cfg.FundingRate.MaxSpreadPercent = 1

	spot := &fakeArbitrageExchange{latestPrice: 100, baseAsset: "BTC", quoteAsset: "USDT"}
	manager := NewFundingArbitrageManager(cfg, nil, spot, fakeFundingMonitor{rate: 0.002}, "BTCUSDT")

	manager.handlePositionChange(ctx, PositionChangeEvent{Symbol: "BTCUSDT", Delta: 1, Price: 100})
	if manager.futuresPosition != 1 || manager.hedgedAmount != 1 {
		t.Fatalf("buy position change futures=%v hedge=%v, want 1/1", manager.futuresPosition, manager.hedgedAmount)
	}

	manager.handlePositionChange(ctx, PositionChangeEvent{Symbol: "BTCUSDT", Delta: -0.4, Price: 100})
	if manager.futuresPosition != 0.6 || manager.hedgedAmount != 0.6 {
		t.Fatalf("sell position change futures=%v hedge=%v, want 0.6/0.6", manager.futuresPosition, manager.hedgedAmount)
	}

	if err := manager.CloseAllHedge(ctx); err != nil {
		t.Fatalf("CloseAllHedge() error = %v", err)
	}
	if manager.hedgedAmount != 0 {
		t.Fatalf("hedgedAmount = %v, want 0 after CloseAllHedge", manager.hedgedAmount)
	}

	beforeOrders := len(spot.orders)
	if err := manager.CloseAllHedge(ctx); err != nil {
		t.Fatalf("CloseAllHedge() without hedge error = %v", err)
	}
	if len(spot.orders) != beforeOrders {
		t.Fatalf("CloseAllHedge() placed order without hedge")
	}
}

func TestFundingArbitrageNoopAndErrorBranches(t *testing.T) {
	ctx := context.Background()
	cfg := &config.Config{}
	cfg.FundingRate.ArbitrageEnabled = true
	cfg.FundingRate.HedgeRateThreshold = 0.01
	cfg.FundingRate.HedgeMinPosition = 100

	manager := NewFundingArbitrageManager(cfg, nil, nil, fakeFundingMonitor{rate: 0.002}, "BTCUSDT")
	if manager.shouldHedge() {
		t.Fatalf("shouldHedge() = true below threshold")
	}
	if manager.checkSpread(ctx, 100) {
		t.Fatalf("checkSpread() = true without spot exchange")
	}
	if err := manager.buySpot(ctx, 1, 100); err == nil {
		t.Fatalf("buySpot() expected missing exchange error")
	}
	if err := manager.sellSpot(ctx, 1, 100); err == nil {
		t.Fatalf("sellSpot() expected missing exchange error")
	}

	manager.OnGridPositionChange(1, 100)
	if len(manager.positionChangeCh) != 1 {
		t.Fatalf("OnGridPositionChange() channel length = %d, want 1", len(manager.positionChangeCh))
	}

	cfg.FundingRate.ArbitrageEnabled = false
	manager.OnGridPositionChange(1, 100)
	if len(manager.positionChangeCh) != 1 {
		t.Fatalf("disabled OnGridPositionChange() enqueued event")
	}
}
