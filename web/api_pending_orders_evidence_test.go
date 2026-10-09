package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"quantmesh/config"
	"quantmesh/position"
)

type pendingOrdersFixture struct {
	testPositionProvider
	slots []SlotInfo
}

func (p *pendingOrdersFixture) GetAllSlots() []SlotInfo { return p.slots }

type pendingRuntimeFixtureProvider struct {
	SymbolManagerProvider
	runtime interface{}
}

func (p pendingRuntimeFixtureProvider) GetEx(string, string, string) (interface{}, bool) {
	return p.runtime, true
}
func (p pendingRuntimeFixtureProvider) List() []interface{} { return []interface{}{p.runtime} }

func pendingOrdersRequest(target string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, target, nil)
	getPendingOrders(c)
	return w
}

func TestPendingOrdersMissingProviderIsUnavailable(t *testing.T) {
	resetWebProviderStateForTest()
	t.Cleanup(resetWebProviderStateForTest)
	w := pendingOrdersRequest("/api/orders/pending")
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("missing provider represented as known empty: %d", w.Code)
	}
}

func TestPendingOrdersExplicitScopeCannotUseOtherDefault(t *testing.T) {
	resetWebProviderStateForTest()
	t.Cleanup(resetWebProviderStateForTest)
	SetPositionManagerProvider(&pendingOrdersFixture{slots: []SlotInfo{{OrderID: 42, OrderStatus: "PLACED"}}})
	w := pendingOrdersRequest("/api/orders/pending?exchange=binance&symbol=BTCUSDT&market_type=spot")
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("unrelated default leaked into scoped order evidence: %d", w.Code)
	}
}

func TestPendingOrdersPreservesUnknownAndCancelRequested(t *testing.T) {
	resetWebProviderStateForTest()
	t.Cleanup(resetWebProviderStateForTest)
	p := &pendingOrdersFixture{slots: []SlotInfo{
		{OrderID: 0, ClientOID: "fixture-unknown", OrderStatus: "UNKNOWN"},
		{OrderID: 42, OrderStatus: "CANCEL_REQUESTED"},
		{OrderID: 43, OrderStatus: "PARTIALLY_FILLED"},
		{OrderID: 44, OrderStatus: "CANCELED"},
	}}
	UpsertPositionProviderForKey("binance", "BTCUSDT", "spot", p)
	w := pendingOrdersRequest("/api/orders/pending?exchange=binance&symbol=BTCUSDT&market_type=spot")
	var body struct {
		Orders []PendingOrderInfo `json:"orders"`
		Count  int                `json:"count"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusOK || len(body.Orders) != 3 || body.Count != 3 || body.Orders[0].Status != "UNKNOWN" || body.Orders[1].Status != "CANCEL_REQUESTED" {
		t.Fatalf("uncertain in-flight orders were hidden: status=%d count=%d", w.Code, body.Count)
	}
}

func TestPendingOrdersKnownEmptyIsJSONArray(t *testing.T) {
	resetWebProviderStateForTest()
	t.Cleanup(resetWebProviderStateForTest)
	UpsertPositionProviderForKey("binance", "BTCUSDT", "spot", &pendingOrdersFixture{})
	w := pendingOrdersRequest("/api/orders/pending?exchange=binance&symbol=BTCUSDT&market_type=spot")
	var body map[string]json.RawMessage
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusOK || string(body["orders"]) != "[]" || string(body["count"]) != "0" {
		t.Fatal("known empty local snapshot must be an explicit array and zero count")
	}
}

func TestPendingOrdersRejectsIncompleteScopeAndWrongMarketFallback(t *testing.T) {
	resetWebProviderStateForTest()
	t.Cleanup(resetWebProviderStateForTest)
	RegisterSymbolProviders("binance", "BTCUSDT", &SymbolScopedProviders{Position: &pendingOrdersFixture{}}, "spot")
	for _, target := range []string{
		"/api/orders/pending?exchange=binance",
		"/api/orders/pending?symbol=BTCUSDT",
	} {
		if w := pendingOrdersRequest(target); w.Code != http.StatusBadRequest {
			t.Fatal("incomplete scope accepted")
		}
	}
	if w := pendingOrdersRequest("/api/orders/pending?exchange=binance&symbol=BTCUSDT&market_type=futures"); w.Code != http.StatusServiceUnavailable {
		t.Fatal("spot provider became futures evidence")
	}
}

func TestPendingOrdersNilImplementationsAreUnavailable(t *testing.T) {
	for _, provider := range []PositionManagerProvider{(*pendingOrdersFixture)(nil), NewPositionManagerAdapter(nil)} {
		resetWebProviderStateForTest()
		SetPositionManagerProvider(provider)
		if w := pendingOrdersRequest("/api/orders/pending"); w.Code != http.StatusServiceUnavailable {
			t.Fatal("uninitialized provider accepted")
		}
	}
	t.Cleanup(resetWebProviderStateForTest)
}

func TestPendingOrdersDefaultViewAndExplicitFuturesRemainAvailable(t *testing.T) {
	resetWebProviderStateForTest()
	t.Cleanup(resetWebProviderStateForTest)
	p := &pendingOrdersFixture{slots: []SlotInfo{{OrderID: 42, OrderStatus: "CONFIRMED"}}}
	SetPositionManagerProvider(p)
	if w := pendingOrdersRequest("/api/orders/pending"); w.Code != http.StatusOK {
		t.Fatal("legacy default view rejected")
	}
	UpsertPositionProviderForKey("binance", "BTCUSDT", "futures", p)
	if w := pendingOrdersRequest("/api/orders/pending?exchange=Binance&symbol=btcusdt"); w.Code != http.StatusOK {
		t.Fatal("default futures scoped view rejected")
	}
}

func TestPendingRuntimeScopeDoesNotConfuseSpotMarginOrDefaultFutures(t *testing.T) {
	rt := &struct{ Config config.SymbolConfig }{Config: config.SymbolConfig{Exchange: "binance", Symbol: "BTCUSDT", MarketType: "spot", UseSpotMargin: true}}
	if pendingRuntimeScopeMatches(rt, "binance", "BTCUSDT", "spot") || pendingRuntimeScopeMatches(rt, "binance", "BTCUSDT", "") {
		t.Fatal("margin runtime accepted as cash spot or default futures")
	}
	if !pendingRuntimeScopeMatches(rt, "Binance", "btcusdt", "spot_margin") {
		t.Fatal("exact margin scope rejected")
	}
	for _, invalid := range []interface{}{nil, 42, struct{}{}, (*struct{})(nil)} {
		if pendingRuntimeScopeMatches(invalid, "binance", "BTCUSDT", "spot_margin") {
			t.Fatal("unknown runtime identity accepted")
		}
	}
}

func TestPendingOrdersRuntimeFallbackUsesActualPositionAdapterOnlyForMatchingScope(t *testing.T) {
	resetWebProviderStateForTest()
	t.Cleanup(resetWebProviderStateForTest)
	cfg := &config.Config{}
	cfg.App.CurrentExchange = "binance"
	cfg.Trading.Symbol, cfg.Trading.MarketType = "BTCUSDT", "spot"
	spm := position.NewSuperPositionManager(cfg, nil, nil, 2, 3)
	runtime := &struct {
		Config               config.SymbolConfig
		SuperPositionManager *position.SuperPositionManager
	}{config.SymbolConfig{Exchange: "binance", Symbol: "BTCUSDT", MarketType: "spot"}, spm}
	symbolManagerProvider = pendingRuntimeFixtureProvider{runtime: runtime}
	if w := pendingOrdersRequest("/api/orders/pending?exchange=binance&symbol=BTCUSDT&market_type=spot"); w.Code != http.StatusOK {
		t.Fatal("actual matching runtime adapter was rejected")
	}
	if w := pendingOrdersRequest("/api/orders/pending?exchange=binance&symbol=BTCUSDT&market_type=futures"); w.Code != http.StatusServiceUnavailable {
		t.Fatal("GetEx returning wrong market bypassed scope verification")
	}
}
