package main

import (
	"context"
	"errors"
	"testing"

	"quantmesh/config"
	"quantmesh/event"
	"quantmesh/exchange"
	"quantmesh/exchange/income"
	"quantmesh/position"
)

type adapterFakeExchange struct {
	name        string
	positions   []*exchange.Position
	orders      []*exchange.Order
	orderBook   *exchange.OrderBook
	account     *exchange.Account
	price       float64
	balance     float64
	cancelled   []int64
	cancelAll   bool
	klines      []*exchange.Candle
	funding     float64
	priceDec    int
	quantityDec int
}

func (f *adapterFakeExchange) GetName() string {
	if f.name == "" {
		return "fake"
	}
	return f.name
}
func (f *adapterFakeExchange) GetMarketType() string { return "futures" }
func (f *adapterFakeExchange) PlaceOrder(ctx context.Context, req *exchange.OrderRequest) (*exchange.Order, error) {
	return &exchange.Order{OrderID: 99, ClientOrderID: req.ClientOrderID, Symbol: req.Symbol, Side: req.Side, Price: req.Price, Quantity: req.Quantity, Status: exchange.OrderStatusNew}, nil
}
func (f *adapterFakeExchange) BatchPlaceOrders(ctx context.Context, orders []*exchange.OrderRequest) ([]*exchange.Order, bool) {
	out := make([]*exchange.Order, 0, len(orders))
	for _, req := range orders {
		ord, _ := f.PlaceOrder(ctx, req)
		out = append(out, ord)
	}
	return out, false
}
func (f *adapterFakeExchange) CancelOrder(ctx context.Context, symbol string, orderID int64) error {
	f.cancelled = append(f.cancelled, orderID)
	return nil
}
func (f *adapterFakeExchange) BatchCancelOrders(ctx context.Context, symbol string, orderIDs []int64) error {
	f.cancelled = append(f.cancelled, orderIDs...)
	return nil
}
func (f *adapterFakeExchange) CancelAllOrders(ctx context.Context, symbol string) error {
	f.cancelAll = true
	return nil
}
func (f *adapterFakeExchange) GetOrder(ctx context.Context, symbol string, orderID int64) (*exchange.Order, error) {
	for _, order := range f.orders {
		if order.OrderID == orderID {
			return order, nil
		}
	}
	return nil, errors.New("not found")
}
func (f *adapterFakeExchange) GetOpenOrders(ctx context.Context, symbol string) ([]*exchange.Order, error) {
	return f.orders, nil
}
func (f *adapterFakeExchange) GetOrderFills(ctx context.Context, symbol string, orderID int64) ([]*exchange.OrderFill, error) {
	return []*exchange.OrderFill{{OrderID: orderID, Commission: 0.1}}, nil
}
func (f *adapterFakeExchange) GetAccount(ctx context.Context) (*exchange.Account, error) {
	if f.account != nil {
		return f.account, nil
	}
	return &exchange.Account{TotalMarginBalance: 1234}, nil
}
func (f *adapterFakeExchange) GetPositions(ctx context.Context, symbol string) ([]*exchange.Position, error) {
	return f.positions, nil
}
func (f *adapterFakeExchange) GetBalance(ctx context.Context, asset string) (float64, error) {
	return f.balance, nil
}
func (f *adapterFakeExchange) StartOrderStream(ctx context.Context, callback func(interface{})) error {
	callback(exchange.OrderUpdate{OrderID: 1})
	return nil
}
func (f *adapterFakeExchange) StopOrderStream() error { return nil }
func (f *adapterFakeExchange) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	return f.price, nil
}
func (f *adapterFakeExchange) StartPriceStream(ctx context.Context, symbol string, callback func(price float64)) error {
	callback(f.price)
	return nil
}
func (f *adapterFakeExchange) StartKlineStream(ctx context.Context, symbols []string, interval string, callback exchange.CandleUpdateCallback) error {
	callback(&exchange.Candle{Symbol: symbols[0], Close: f.price})
	return nil
}
func (f *adapterFakeExchange) StopKlineStream() error { return nil }
func (f *adapterFakeExchange) GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*exchange.Candle, error) {
	return f.klines, nil
}
func (f *adapterFakeExchange) GetPriceDecimals() int {
	if f.priceDec == 0 {
		return 2
	}
	return f.priceDec
}
func (f *adapterFakeExchange) GetQuantityDecimals() int {
	if f.quantityDec == 0 {
		return 4
	}
	return f.quantityDec
}
func (f *adapterFakeExchange) GetBaseAsset() string  { return "BTC" }
func (f *adapterFakeExchange) GetQuoteAsset() string { return "USDT" }
func (f *adapterFakeExchange) EstimateFinalOrderAmount(symbol string, price, quantity float64, reduceOnly bool) float64 {
	return price * quantity
}
func (f *adapterFakeExchange) GetFundingRate(ctx context.Context, symbol string) (float64, error) {
	return f.funding, nil
}
func (f *adapterFakeExchange) GetIncomeHistory(ctx context.Context, symbol, incomeType string, startTime, endTime int64) ([]*income.Income, error) {
	return []*income.Income{{Symbol: symbol, IncomeType: incomeType}}, nil
}
func (f *adapterFakeExchange) GetSpotPrice(ctx context.Context, symbol string) (float64, error) {
	return f.price - 1, nil
}
func (f *adapterFakeExchange) GetOrderBook(ctx context.Context, symbol string, limit int) (*exchange.OrderBook, error) {
	return f.orderBook, nil
}
func (f *adapterFakeExchange) GetFundingInfo(ctx context.Context, symbol string) (*exchange.FundingInfo, error) {
	return &exchange.FundingInfo{Symbol: symbol, Rate: f.funding}, nil
}
func (f *adapterFakeExchange) InternalTransfer(ctx context.Context, fromAccount, toAccount, asset string, amount float64) (string, error) {
	return "transfer-1", nil
}

func TestPositionExchangeAdapterDelegatesAndConverts(t *testing.T) {
	ctx := context.Background()
	ex := &adapterFakeExchange{
		positions: []*exchange.Position{{Symbol: "BTCUSDT", Size: 0.5}},
		orders:    []*exchange.Order{{OrderID: 7, Symbol: "BTCUSDT", Side: exchange.SideBuy}},
		orderBook: &exchange.OrderBook{
			Symbol: "BTCUSDT",
			Bids:   []exchange.OrderBookLevel{{Price: 100, Quantity: 1}},
			Asks:   []exchange.OrderBookLevel{{Price: 101, Quantity: 2}},
		},
		price:   100.5,
		balance: 42,
	}
	adapter := &positionExchangeAdapter{exchange: ex}

	rawPositions, err := adapter.GetPositions(ctx, "BTCUSDT")
	if err != nil {
		t.Fatalf("GetPositions() error = %v", err)
	}
	positions := rawPositions.([]*position.PositionInfo)
	if len(positions) != 1 || positions[0].Size != 0.5 {
		t.Fatalf("converted positions = %+v", positions)
	}
	if got, _ := adapter.GetOpenOrders(ctx, "BTCUSDT"); len(got.([]*exchange.Order)) != 1 {
		t.Fatalf("GetOpenOrders() = %+v", got)
	}
	if got, err := adapter.GetOrderForReconciler(ctx, "BTCUSDT", 7); err != nil || got.OrderID != 7 {
		t.Fatalf("GetOrderForReconciler() = %+v/%v", got, err)
	}
	if got, _ := adapter.GetLatestPrice(ctx, "BTCUSDT"); got != 100.5 {
		t.Fatalf("GetLatestPrice() = %.2f", got)
	}
	ob, err := adapter.GetOrderBook(ctx, "BTCUSDT", 5)
	if err != nil || ob.Symbol != "BTCUSDT" || ob.Bids[0].Price != 100 || ob.Asks[0].Quantity != 2 {
		t.Fatalf("GetOrderBook() = %+v/%v", ob, err)
	}
	if adapter.GetBaseAsset() != "BTC" || adapter.GetQuoteAsset() != "USDT" || adapter.GetPriceDecimals() != 2 || adapter.GetQuantityDecimals() != 4 {
		t.Fatalf("asset/precision accessors mismatch")
	}
	if got, _ := adapter.GetBalance(ctx, "USDT"); got != 42 {
		t.Fatalf("GetBalance() = %.2f", got)
	}
	if err := adapter.CancelAllOrders(ctx, "BTCUSDT"); err != nil || !ex.cancelAll {
		t.Fatalf("CancelAllOrders() error/called = %v/%v", err, ex.cancelAll)
	}
}

func TestExchangeProviderAndCapitalAdapters(t *testing.T) {
	cfg := &config.Config{}
	cfg.Exchanges = map[string]config.ExchangeConfig{"binance": {}}
	cfg.Strategies.Configs = map[string]config.StrategyConfig{"grid": {Enabled: true}}
	ex1 := &adapterFakeExchange{name: "ex1", klines: []*exchange.Candle{{Close: 100}}, funding: 0.001}
	ex2 := &adapterFakeExchange{name: "ex1"}
	manager := NewSymbolManager(cfg, event.NewEventBus(1), nil, nil, "")
	manager.Add(&SymbolRuntime{Exchange: ex1, Config: config.SymbolConfig{Exchange: "binance", Symbol: "BTCUSDT"}})
	manager.Add(&SymbolRuntime{Exchange: ex2, Config: config.SymbolConfig{Exchange: "binance", Symbol: "ETHUSDT"}})

	capital := &capitalDataSourceAdapter{manager: manager, cfg: cfg}
	if got := capital.GetExchanges(); len(got) != 1 {
		t.Fatalf("GetExchanges() length = %d, want deduped 1", len(got))
	}
	if !capital.GetStrategyConfigs()["grid"].Enabled || capital.GetConfig() != cfg {
		t.Fatalf("strategy config/config accessors mismatch")
	}
	if got := capital.GetPositionManagers(); len(got) != 2 {
		t.Fatalf("GetPositionManagers() length = %d, want 2", len(got))
	}

	provider := &exchangeProviderAdapter{exchange: ex1}
	klines, err := provider.GetHistoricalKlines(context.Background(), "BTCUSDT", "1m", 1)
	if err != nil || len(klines) != 1 {
		t.Fatalf("GetHistoricalKlines() = %+v/%v", klines, err)
	}
	if rate, _ := provider.GetFundingRate(context.Background(), "BTCUSDT"); rate != 0.001 {
		t.Fatalf("GetFundingRate() = %.4f", rate)
	}
	if positions, _ := provider.GetPositions(context.Background(), "BTCUSDT"); positions != nil {
		t.Fatalf("GetPositions() = %+v, want nil default", positions)
	}
}

func TestMiscWebAdapters(t *testing.T) {
	cfg := &config.Config{}
	cfg.Exchanges = map[string]config.ExchangeConfig{
		"binance": {APIKey: "ak", SecretKey: "sk", Testnet: true},
	}
	binanceCfg := buildBinanceConfigForBacktest(cfg)
	if binanceCfg["api_key"] != "ak" || binanceCfg["secret_key"] != "sk" || binanceCfg["testnet"] != "true" {
		t.Fatalf("buildBinanceConfigForBacktest() = %+v", binanceCfg)
	}
	if got := buildBinanceConfigForBacktest(nil); got["testnet"] != "false" {
		t.Fatalf("nil buildBinanceConfigForBacktest() = %+v", got)
	}

	plan := position.NewPlanManager(nil, nil, nil)
	if got := (&planManagerProviderAdapter{planManager: plan}).GetPlanManager(); got != plan {
		t.Fatalf("GetPlanManager() mismatch")
	}
	poly := &polymarketSignalAdapter{}
	if poly.GetLastAnalysis() != nil || !poly.GetLastAnalysisTime().IsZero() || poly.PerformAnalysis() == nil {
		t.Fatalf("nil polymarketSignalAdapter guards failed")
	}
	restore := &reconciliationRestoreAdapter{}
	if history, err := restore.GetLatestReconciliationHistory("binance", "BTCUSDT"); history != nil || err != nil {
		t.Fatalf("nil restore history = %+v/%v", history, err)
	}
	if count, err := restore.GetReconciliationCount("binance", "BTCUSDT"); count != 0 || err != nil {
		t.Fatalf("nil restore count = %d/%v", count, err)
	}
}

func TestSnapshotRuntimeAdapter(t *testing.T) {
	ex := &adapterFakeExchange{account: &exchange.Account{TotalWalletBalance: 900, TotalMarginBalance: 0}}
	rt := &SymbolRuntime{Config: config.SymbolConfig{Exchange: "binance", Symbol: "BTCUSDT"}, AccountID: "acct", Exchange: ex}
	adapter := &snapshotRuntimeAdapter{rt: rt}
	if adapter.Exchange() != "binance" || adapter.Symbol() != "BTCUSDT" || adapter.Account() != "acct" {
		t.Fatalf("snapshot identity mismatch")
	}
	price, pnl, value := adapter.CurrentSnapshot()
	if price != 0 || pnl != 0 || value != 0 {
		t.Fatalf("CurrentSnapshot() without monitors = %.2f/%.2f/%.2f", price, pnl, value)
	}
	if equity, ok := adapter.AccountEquityUSDT(context.Background()); !ok || equity != 900 {
		t.Fatalf("AccountEquityUSDT() = %.2f/%v", equity, ok)
	}
	if equity, ok := (&snapshotRuntimeAdapter{}).AccountEquityUSDT(context.Background()); ok || equity != 0 {
		t.Fatalf("nil AccountEquityUSDT() = %.2f/%v", equity, ok)
	}

}
