package web

import (
	"context"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"quantmesh/exchange"
	"quantmesh/storage"
)

func TestSymbolProviderRegistrationSelectionAndFallbacks(t *testing.T) {
	gin.SetMode(gin.TestMode)
	resetWebProviderStateForTest()

	defaultPrice := &testPriceProvider{price: 11}
	defaultExchange := &testExchangeProvider{}
	defaultPosition := &testPositionProvider{leverage: 3}
	defaultRisk := &testRiskProvider{}
	defaultStorage := &providerSelectionStorageProvider{}
	defaultFunding := &testFundingProvider{}
	SetPriceProvider(defaultPrice)
	SetExchangeProvider(defaultExchange)
	SetPositionManagerProvider(defaultPosition)
	SetRiskMonitorProvider(defaultRisk)
	SetStorageServiceProvider(defaultStorage)
	SetFundingMonitorProvider(defaultFunding)
	SetStatusProvider(&SystemStatus{Exchange: "default", Symbol: "ETHUSDT"})
	SetDefaultSymbolKey("binance", "BTCUSDT")

	price := &testPriceProvider{price: 99}
	exchangeProv := &testExchangeProvider{}
	position := &testPositionProvider{leverage: 8}
	risk := &testRiskProvider{triggered: true}
	storageProv := &providerSelectionStorageProvider{}
	funding := &testFundingProvider{rates: map[string]float64{"BTCUSDT": 0.0001}}
	status := &SystemStatus{Running: true, Exchange: "binance", Symbol: "BTCUSDT", MarketType: "spot"}

	RegisterSymbolProviders("Binance", "BTCUSDT", &SymbolScopedProviders{
		Status: status, Price: price, Exchange: exchangeProv, Position: position,
		Risk: risk, Storage: storageProv, Funding: funding,
	}, "spot")

	if !IsSymbolStatusRegistered("binance", "btcusdt", "spot") {
		t.Fatalf("symbol status should be registered")
	}
	if got, ok := GetRegisteredSystemStatus("binance", "btcusdt", "spot"); !ok || got != status {
		t.Fatalf("registered status=%#v ok=%v", got, ok)
	}

	ctx := testGinContextWithQuery("/api/status?exchange=binance&symbol=btcusdt&market_type=spot")
	if got := pickStatus(ctx); got != status {
		t.Fatalf("pick status=%#v", got)
	}
	if got := PickPriceProvider(ctx); got != price {
		t.Fatalf("pick price provider did not use symbol scoped provider")
	}
	if got := pickExchangeProvider(ctx); got != exchangeProv {
		t.Fatalf("pick exchange provider did not use symbol scoped provider")
	}
	if got := PickPositionProvider(ctx); got != position {
		t.Fatalf("pick position provider did not use symbol scoped provider")
	}
	if got := PickRiskProvider(ctx); got != risk {
		t.Fatalf("pick risk provider did not use symbol scoped provider")
	}
	if got := PickStorageProvider(ctx); got != storageProv {
		t.Fatalf("pick storage provider did not use symbol scoped provider")
	}
	if got := PickFundingProvider(ctx); got != funding {
		t.Fatalf("pick funding provider did not use symbol scoped provider")
	}

	compatKey := makeSymbolKeyCompat("binance", "btcusdt")
	statusMu.Lock()
	statusBySymbol[compatKey] = status
	statusMu.Unlock()
	compatCtx := testGinContextWithQuery("/api/status?exchange=binance&symbol=btcusdt&market_type=margin")
	if key := resolveSymbolKey(compatCtx); key != compatKey {
		t.Fatalf("compat key=%s", key)
	}

	UpsertPriceProviderForKey("binance", "ethusdt", "futures", price)
	UpsertPriceProviderForKey("binance", "ethusdt", "futures", (*testPriceProvider)(nil))
	UpsertPositionProviderForKey("binance", "ethusdt", "futures", position)
	UpsertPositionProviderForKey("binance", "ethusdt", "futures", (*testPositionProvider)(nil))
	ethCtx := testGinContextWithQuery("/api/status?exchange=binance&symbol=ethusdt")
	if got := PickPriceProvider(ethCtx); got != price {
		t.Fatalf("upserted price provider not selected")
	}
	if got := PickPositionProvider(ethCtx); got != position {
		t.Fatalf("upserted position provider not selected")
	}

	unknownCtx := testGinContextWithQuery("/api/status?exchange=kraken&symbol=SOLUSDT&market_type=futures")
	if PickPriceProvider(unknownCtx) != defaultPrice || PickPositionProvider(unknownCtx) != defaultPosition ||
		PickRiskProvider(unknownCtx) != defaultRisk || PickStorageProvider(unknownCtx) != defaultStorage ||
		PickFundingProvider(unknownCtx) != defaultFunding || pickExchangeProvider(unknownCtx) != defaultExchange {
		t.Fatalf("unknown symbol should use default providers")
	}

	UnregisterSymbolProviders("binance", "btcusdt", "spot")
	if IsSymbolStatusRegistered("binance", "btcusdt", "spot") {
		t.Fatalf("symbol status should be unregistered")
	}
	if status.Running {
		t.Fatalf("unregister should mark status stopped")
	}

	RegisterSymbolProviders("noop", "nil", nil, "spot")
	RegisterFundingProvider("binance", "adausdt", nil)
	RegisterFundingProvider("binance", "adausdt", funding)
	adaCtx := testGinContextWithQuery("/api/funding?exchange=binance&symbol=adausdt")
	if got := PickFundingProvider(adaCtx); got != funding {
		t.Fatalf("registered funding provider not selected")
	}
}

func testGinContextWithQuery(target string) *gin.Context {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("GET", target, nil)
	return c
}

func resetWebProviderStateForTest() {
	statusMu.Lock()
	currentStatus = nil
	statusBySymbol = make(map[string]*SystemStatus)
	defaultSymbolKey = ""
	statusMu.Unlock()

	providersMu.Lock()
	priceProviders = make(map[string]PriceProvider)
	exchangeProviders = make(map[string]ExchangeProvider)
	positionProviders = make(map[string]PositionManagerProvider)
	riskProviders = make(map[string]RiskMonitorProvider)
	storageProviders = make(map[string]StorageServiceProvider)
	fundingProviders = make(map[string]FundingMonitorProvider)
	providersMu.Unlock()

	priceProvider = nil
	exchangeProvider = nil
	positionManagerProvider = nil
	riskMonitorProvider = nil
	storageServiceProvider = nil
	fundingMonitorProvider = nil
	symbolManagerProvider = nil
}

type testPriceProvider struct{ price float64 }

func (p *testPriceProvider) GetLastPrice() float64 { return p.price }

type testExchangeProvider struct{}

func (p *testExchangeProvider) GetHistoricalKlines(context.Context, string, string, int) ([]*exchange.Candle, error) {
	return nil, nil
}

func (p *testExchangeProvider) GetFundingRate(context.Context, string) (float64, error) {
	return 0.01, nil
}

func (p *testExchangeProvider) GetPositions(context.Context, string) ([]*exchange.Position, error) {
	return nil, nil
}

type testPositionProvider struct{ leverage int }

func (p *testPositionProvider) GetAllSlots() []SlotInfo { return nil }
func (p *testPositionProvider) GetSlotCount() int       { return 0 }
func (p *testPositionProvider) GetReconcileCount() int64 {
	return 0
}
func (p *testPositionProvider) GetLastReconcileTime() time.Time { return time.Time{} }
func (p *testPositionProvider) GetTotalBuyQty() float64         { return 0 }
func (p *testPositionProvider) GetTotalSellQty() float64        { return 0 }
func (p *testPositionProvider) GetPriceInterval() float64       { return 0 }
func (p *testPositionProvider) GetProfitSpread() float64        { return 0 }
func (p *testPositionProvider) GetLeverage() int                { return p.leverage }

type testRiskProvider struct{ triggered bool }

func (p *testRiskProvider) IsTriggered() bool                       { return p.triggered }
func (p *testRiskProvider) GetTriggeredTime() time.Time             { return time.Unix(1, 0) }
func (p *testRiskProvider) GetRecoveredTime() time.Time             { return time.Unix(2, 0) }
func (p *testRiskProvider) GetMonitorSymbols() []string             { return []string{"BTCUSDT"} }
func (p *testRiskProvider) GetSymbolData(symbol string) interface{} { return symbol }

type providerSelectionStorageProvider struct{}

func (p *providerSelectionStorageProvider) GetStorage() storage.Storage { return nil }

type testFundingProvider struct{ rates map[string]float64 }

func (p *testFundingProvider) GetCurrentFundingRates() (map[string]float64, error) {
	if p.rates == nil {
		return map[string]float64{}, nil
	}
	return p.rates, nil
}
