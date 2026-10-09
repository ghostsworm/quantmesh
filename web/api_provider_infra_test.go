package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"quantmesh/exchange"
)

type providerInfraPrice struct {
	price float64
}

func (p *providerInfraPrice) GetLastPrice() float64 { return p.price }

type providerInfraPosition struct {
	slots []SlotInfo
}

func (p *providerInfraPosition) GetAllSlots() []SlotInfo         { return p.slots }
func (p *providerInfraPosition) GetSlotCount() int               { return len(p.slots) }
func (p *providerInfraPosition) GetReconcileCount() int64        { return 7 }
func (p *providerInfraPosition) GetLastReconcileTime() time.Time { return time.Unix(100, 0) }
func (p *providerInfraPosition) GetTotalBuyQty() float64         { return 1.2 }
func (p *providerInfraPosition) GetTotalSellQty() float64        { return 0.8 }
func (p *providerInfraPosition) GetPriceInterval() float64       { return 100 }
func (p *providerInfraPosition) GetProfitSpread() float64        { return 120 }
func (p *providerInfraPosition) GetLeverage() int                { return 3 }

type providerInfraExchange struct{}

func (providerInfraExchange) GetHistoricalKlines(context.Context, string, string, int) ([]*exchange.Candle, error) {
	return []*exchange.Candle{{Close: 100}}, nil
}
func (providerInfraExchange) GetFundingRate(context.Context, string) (float64, error) {
	return 0.0001, nil
}
func (providerInfraExchange) GetPositions(context.Context, string) ([]*exchange.Position, error) {
	return []*exchange.Position{{Symbol: "BTCUSDT"}}, nil
}

type providerInfraSystemMetrics struct {
	err bool
}

func (p providerInfraSystemMetrics) GetCurrentMetrics() (*SystemMetricsResponse, error) {
	if p.err {
		return nil, errors.New("boom")
	}
	return &SystemMetricsResponse{Timestamp: time.Unix(100, 0), CPUPercent: 11, MemoryMB: 22, MemoryPercent: 33, ProcessID: 44}, nil
}

func (p providerInfraSystemMetrics) GetMetrics(startTime, endTime time.Time, granularity string) ([]*SystemMetricsResponse, error) {
	if p.err {
		return nil, errors.New("boom")
	}
	return []*SystemMetricsResponse{{Timestamp: startTime, CPUPercent: 1, ProcessID: 2}}, nil
}

func (p providerInfraSystemMetrics) GetDailyMetrics(days int) ([]*DailySystemMetricsResponse, error) {
	if p.err {
		return nil, errors.New("boom")
	}
	return []*DailySystemMetricsResponse{{Date: time.Unix(0, 0), AvgCPUPercent: float64(days), SampleCount: days}}, nil
}

func resetProviderInfraGlobals(t *testing.T) {
	t.Helper()
	origStatus := currentStatus
	origDefaultKey := defaultSymbolKey
	origPrice := priceProvider
	origExchange := exchangeProvider
	origExchangeGetter := exchangeGetterFunc
	origPosition := positionManagerProvider
	origStorage := storageServiceProvider
	origFunding := fundingMonitorProvider
	origRisk := riskMonitorProvider
	origMetrics := systemMetricsProvider
	origStrategy := strategyProvider
	origOrderQuantity := orderQuantityConfig

	statusMu.Lock()
	origStatusBySymbol := statusBySymbol
	statusBySymbol = make(map[string]*SystemStatus)
	statusMu.Unlock()

	providersMu.Lock()
	origPriceProviders := priceProviders
	origExchangeProviders := exchangeProviders
	origPositionProviders := positionProviders
	origRiskProviders := riskProviders
	origStorageProviders := storageProviders
	origFundingProviders := fundingProviders
	priceProviders = make(map[string]PriceProvider)
	exchangeProviders = make(map[string]ExchangeProvider)
	positionProviders = make(map[string]PositionManagerProvider)
	riskProviders = make(map[string]RiskMonitorProvider)
	storageProviders = make(map[string]StorageServiceProvider)
	fundingProviders = make(map[string]FundingMonitorProvider)
	providersMu.Unlock()

	t.Cleanup(func() {
		currentStatus = origStatus
		defaultSymbolKey = origDefaultKey
		priceProvider = origPrice
		exchangeProvider = origExchange
		exchangeGetterFunc = origExchangeGetter
		positionManagerProvider = origPosition
		storageServiceProvider = origStorage
		fundingMonitorProvider = origFunding
		riskMonitorProvider = origRisk
		systemMetricsProvider = origMetrics
		strategyProvider = origStrategy
		orderQuantityConfig = origOrderQuantity

		statusMu.Lock()
		statusBySymbol = origStatusBySymbol
		statusMu.Unlock()

		providersMu.Lock()
		priceProviders = origPriceProviders
		exchangeProviders = origExchangeProviders
		positionProviders = origPositionProviders
		riskProviders = origRiskProviders
		storageProviders = origStorageProviders
		fundingProviders = origFundingProviders
		providersMu.Unlock()
	})
}

func newProviderInfraContext(path string) (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, path, nil)
	return c, w
}

func TestProviderRegistrationSelectionAndFallbacks(t *testing.T) {
	resetProviderInfraGlobals(t)
	status := &SystemStatus{Running: true, Exchange: "binance", Symbol: "BTCUSDT", MarketType: "spot"}
	price := &providerInfraPrice{price: 123}
	position := &providerInfraPosition{slots: []SlotInfo{{Price: 100, PositionStatus: "FILLED", PositionQty: 0.1}}}
	exchangeProv := providerInfraExchange{}

	SetStatusProvider(&SystemStatus{Running: false, Exchange: "fallback"})
	SetPriceProvider(&providerInfraPrice{price: 1})
	SetExchangeProvider(exchangeProv)
	SetPositionManagerProvider(&providerInfraPosition{})
	SetOrderQuantityConfig(88)
	SetExchangeGetter(func(exchangeID string) exchange.IExchange { return nil })

	RegisterSymbolProviders("binance", "BTCUSDT", &SymbolScopedProviders{
		Status:   status,
		Price:    price,
		Exchange: exchangeProv,
		Position: position,
	}, "spot")
	SetDefaultSymbolKey("binance", "BTCUSDT")

	if !IsSymbolStatusRegistered("binance", "BTCUSDT", "spot") {
		t.Fatal("status should be registered")
	}
	if got, ok := GetRegisteredSystemStatus("binance", "BTCUSDT", "spot"); !ok || got != status {
		t.Fatalf("unexpected registered status: %+v ok=%v", got, ok)
	}
	if got, ok := resolveStatusBySymbol("binance", "BTCUSDT", "spot"); !ok || got != status {
		t.Fatalf("resolveStatusBySymbol exact failed: %+v ok=%v", got, ok)
	}

	c, _ := newProviderInfraContext("/api/status?exchange=binance&symbol=BTCUSDT&market_type=spot")
	if got := pickStatus(c); got != status {
		t.Fatalf("pickStatus returned fallback: %+v", got)
	}
	if got := PickPriceProvider(c); got != price {
		t.Fatalf("PickPriceProvider returned %+v", got)
	}
	if got := pickExchangeProvider(c); got == nil {
		t.Fatal("pickExchangeProvider returned nil")
	}
	if got := PickPositionProvider(c); got != position {
		t.Fatalf("PickPositionProvider returned %+v", got)
	}

	UnregisterSymbolProviders("binance", "BTCUSDT", "spot")
	if status.Running {
		t.Fatal("unregister should mark status as stopped")
	}
	if IsSymbolStatusRegistered("binance", "BTCUSDT", "spot") {
		t.Fatal("status should be unregistered")
	}
}

func TestAdaptersAndStrategyProviderHelpers(t *testing.T) {
	resetProviderInfraGlobals(t)

	SetStorageServiceProvider(NewStorageServiceAdapter(nil))
	if storageServiceProvider.GetStorage() != nil {
		t.Fatal("nil storage service adapter should return nil storage")
	}

	released := ""
	strategy := NewStrategyProviderAdapter(
		func() map[string]StrategyCapitalInfo {
			return map[string]StrategyCapitalInfo{"grid": {Allocated: 100, Used: 25, Available: 75, Weight: 1}}
		},
		func(strategyName string) float64 {
			released = strategyName
			return 12
		},
		func() map[string]float64 { return map[string]float64{"grid": 12} },
	)
	SetStrategyProvider(strategy)
	if got := strategy.GetCapitalAllocation()["grid"].Available; got != 75 {
		t.Fatalf("allocation available = %v", got)
	}
	if got := strategy.ReleaseLockedCapital("grid"); got != 12 || released != "grid" {
		t.Fatalf("release one = %v released=%q", got, released)
	}
	if got := strategy.ReleaseAllLockedCapital()["grid"]; got != 12 {
		t.Fatalf("release all = %v", got)
	}

	emptyStrategy := NewStrategyProviderAdapter(func() map[string]StrategyCapitalInfo { return nil }, nil, nil)
	if emptyStrategy.ReleaseLockedCapital("missing") != 0 {
		t.Fatal("nil release function should return zero")
	}
	if len(emptyStrategy.ReleaseAllLockedCapital()) != 0 {
		t.Fatal("nil release all function should return empty map")
	}
}

func TestSystemMetricsHandlers(t *testing.T) {
	resetProviderInfraGlobals(t)
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/metrics", getSystemMetrics)
	r.GET("/metrics/current", getCurrentSystemMetrics)
	r.GET("/metrics/daily", getDailySystemMetrics)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("nil metrics status = %d", w.Code)
	}
	var nilResp map[string][]interface{}
	if err := json.Unmarshal(w.Body.Bytes(), &nilResp); err != nil {
		t.Fatalf("decode nil metrics: %v", err)
	}
	if len(nilResp["metrics"]) != 0 {
		t.Fatalf("nil metrics should be empty: %+v", nilResp)
	}

	SetSystemMetricsProvider(providerInfraSystemMetrics{})
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/metrics?start_time=2026-01-01T00:00:00Z&end_time=2026-01-02T00:00:00Z", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"granularity":"detail"`) {
		t.Fatalf("detail metrics status=%d body=%s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/metrics?granularity=daily&start_time=2026-01-01T00:00:00Z&end_time=2026-01-11T00:00:00Z", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"granularity":"daily"`) {
		t.Fatalf("daily metrics status=%d body=%s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/metrics/current", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"cpu_percent":11`) {
		t.Fatalf("current metrics status=%d body=%s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/metrics/daily?days=5", nil))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"sample_count":5`) {
		t.Fatalf("daily endpoint status=%d body=%s", w.Code, w.Body.String())
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/metrics?start_time=bad", nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid start status = %d body=%s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/metrics?start_time=2026-01-01T00:00:00Z&end_time=bad", nil))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("invalid end status = %d body=%s", w.Code, w.Body.String())
	}

	SetSystemMetricsProvider(providerInfraSystemMetrics{err: true})
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("metrics provider error status = %d", w.Code)
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/metrics/current", nil))
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), `"error":"system_metrics_unavailable"`) {
		t.Fatalf("current error fallback status=%d body=%s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/metrics/daily", nil))
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("daily provider error status = %d", w.Code)
	}
}

type marketTypeStopProbe struct {
	SymbolManagerProvider
	requestedMarketType string
	resolvedMarketType  string
	stopErr             error
}

type startFailureSymbolProbe struct {
	SymbolManagerProvider
	err error
}

func (p startFailureSymbolProbe) StartSymbol(string, string, string) error { return p.err }

func (p *marketTypeStopProbe) StopSymbol(string, string) error {
	return errors.New("legacy unscoped stop path must not be used")
}

func (p *marketTypeStopProbe) StopSymbolForMarket(_, _, marketType string) (string, error) {
	p.requestedMarketType = marketType
	return p.resolvedMarketType, p.stopErr
}

func TestStopTradingRoutesAndUpdatesExactMarketType(t *testing.T) {
	resetProviderInfraGlobals(t)
	previous := symbolManagerProvider
	probe := &marketTypeStopProbe{resolvedMarketType: "spot"}
	symbolManagerProvider = probe
	t.Cleanup(func() { symbolManagerProvider = previous })

	spotStatus := &SystemStatus{Running: true, Exchange: "binance", Symbol: "BTCUSDT", MarketType: "spot"}
	futuresStatus := &SystemStatus{Running: true, Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures"}
	statusMu.Lock()
	statusBySymbol = map[string]*SystemStatus{
		makeSymbolKey("binance", "BTCUSDT", "spot"):    spotStatus,
		makeSymbolKey("binance", "BTCUSDT", "futures"): futuresStatus,
	}
	statusMu.Unlock()

	c, response := newProviderInfraContext("/api/trading/stop?exchange=binance&symbol=BTCUSDT&market_type=spot")
	stopTrading(c)
	if response.Code != http.StatusOK {
		t.Fatalf("stop response status = %d, body=%s", response.Code, response.Body.String())
	}
	if probe.requestedMarketType != "spot" {
		t.Fatalf("stop routed market_type = %q, want spot", probe.requestedMarketType)
	}
	if spotStatus.Running || !futuresStatus.Running {
		t.Fatalf("stop updated wrong market status: spot=%v futures=%v", spotStatus.Running, futuresStatus.Running)
	}
	probe.stopErr = ErrStopMarketTypeRequired
	c, response = newProviderInfraContext("/api/trading/stop?exchange=binance&symbol=BTCUSDT")
	stopTrading(c)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("ambiguous stop response status = %d, want bad request; body=%s", response.Code, response.Body.String())
	}
}

func TestStartTradingDoesNotReturnUnderlyingDiagnostic(t *testing.T) {
	resetProviderInfraGlobals(t)
	previous := symbolManagerProvider
	symbolManagerProvider = startFailureSymbolProbe{err: errors.New("signed request failed: signature=private-token")}
	t.Cleanup(func() { symbolManagerProvider = previous })

	c, response := newProviderInfraContext("/api/trading/start?exchange=binance&symbol=BTCUSDT")
	startTrading(c)
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("start response status = %d, want internal error; body=%s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), "private-token") || strings.Contains(response.Body.String(), "signature=") {
		t.Fatalf("start response leaked underlying exchange diagnostic: %s", response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"error_key":"error.bot_start_failed_diagnostic_withheld"`) {
		t.Fatalf("start response lacks safe localized error key: %s", response.Body.String())
	}
}
