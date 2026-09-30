package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/storage"

	"github.com/gin-gonic/gin"
)

type dailyFundingFixture struct {
	from, through time.Time
	coverageErr   error
	payments      map[string]map[string]float64
}

func (f dailyFundingFixture) GetFundingIncomeCoverage(string, string, string, string) (time.Time, time.Time, error) {
	return f.from, f.through, f.coverageErr
}

func (f dailyFundingFixture) GetDailyFundingPaymentsByScope(_, _, _, _, _ string, start, _ time.Time) (map[string]float64, error) {
	if f.payments == nil {
		return nil, nil
	}
	return f.payments[start.Add(8*time.Hour).Format("2006-01-02")], nil
}

func TestQueryVerifiedDailyFundingRequiresCompleteCoveredDayAndSingleQuoteAsset(t *testing.T) {
	location := time.FixedZone("UTC+8", 8*60*60)
	start := time.Date(2026, 9, 28, 0, 0, 0, 0, location)
	end := start.AddDate(0, 0, 3)
	now := end.Add(12 * time.Hour)
	fixture := dailyFundingFixture{
		from: start.UTC(), through: end.UTC(),
		payments: map[string]map[string]float64{
			"2026-09-28": {"USDT": 1.25},
			"2026-09-29": {"USDC": 2},
			"2026-09-30": {"USDT": 1, "USDC": 2},
		},
	}
	got, assets := queryVerifiedDailyFunding(fixture, "acct", "binance", "futures", "BTCUSDT", "scope-a", "usdt", start, end, now, location)
	if len(got) != 1 || got["2026-09-28"] != 1.25 || assets["2026-09-28"] != "USDT" {
		t.Fatalf("expected only fully covered quote-asset funding, got amounts=%v assets=%v", got, assets)
	}
	noPayments := dailyFundingFixture{from: start.UTC(), through: end.UTC()}
	zeroAmounts, _ := queryVerifiedDailyFunding(noPayments, "acct", "binance", "futures", "BTCUSDT", "scope-a", "USDT", start, end, now, location)
	if len(zeroAmounts) != 3 || zeroAmounts["2026-09-28"] != 0 {
		t.Fatalf("a complete covered day with no payments should be a verified zero, got %v", zeroAmounts)
	}
	partial := dailyFundingFixture{from: start.UTC(), through: start.Add(23 * time.Hour).UTC(), payments: fixture.payments}
	if partialAmounts, _ := queryVerifiedDailyFunding(partial, "acct", "binance", "futures", "BTCUSDT", "scope-a", "USDT", start, end, now, location); len(partialAmounts) != 0 {
		t.Fatalf("partial day coverage must not publish a daily total: %v", partialAmounts)
	}
	failed := dailyFundingFixture{coverageErr: errors.New("db unavailable")}
	if failedAmounts, _ := queryVerifiedDailyFunding(failed, "acct", "binance", "futures", "BTCUSDT", "scope-a", "USDT", start, end, now, location); len(failedAmounts) != 0 {
		t.Fatalf("coverage read failure must fail closed: %v", failedAmounts)
	}
}

func TestStatisticsDoesNotPublishOrderPnLWithoutAssetProof(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store, err := storage.NewSQLStorage(t.TempDir() + "/statistics-profit-safety.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })

	orderPnL := 321.75
	if err := store.SaveOrder(&storage.Order{
		OrderID: 42, Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures",
		Status: "FILLED", RealizedPnL: &orderPnL, CreatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}

	previousStatus, previousStorage := currentStatus, storageServiceProvider
	currentStatus = &SystemStatus{Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures"}
	SetStorageServiceProvider(&testStorageProvider{st: store})
	t.Cleanup(func() {
		currentStatus = previousStatus
		SetStorageServiceProvider(previousStorage)
	})

	request := httptest.NewRequest(http.MethodGet, "/api/statistics?exchange=binance&symbol=BTCUSDT&market_type=futures", nil)
	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)
	ctx.Request = request
	getStatistics(ctx)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["exchange_pnl"] != nil || payload["exchange_pnl_verified"] != false ||
		payload["today_exchange_pnl"] != nil || payload["today_exchange_pnl_verified"] != false {
		t.Fatalf("unverified order PnL escaped statistics API: %s", response.Body.String())
	}
}

func TestStatisticsUsesExactMarketScopeAndPnLAsset(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store, err := storage.NewSQLStorage(t.TempDir() + "/statistics-dimension.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	now := time.Now().UTC()
	for _, trade := range []storage.Trade{
		{Account: "rotated-label", AccountScope: "scope-a", Exchange: "binance", MarketType: "futures", PnLAsset: "USDT", FeeAsset: "USDT", Symbol: "BTCUSDT", Quantity: 1, PnL: 10, Fee: 2, CreatedAt: now},
		{Account: "rotated-label", AccountScope: "scope-a", Exchange: "binance", MarketType: "spot", PnLAsset: "USDT", FeeAsset: "USDT", Symbol: "BTCUSDT", Quantity: 10, PnL: 100, Fee: 1, CreatedAt: now},
		{Account: "other", AccountScope: "scope-b", Exchange: "binance", MarketType: "futures", PnLAsset: "USDT", FeeAsset: "USDT", Symbol: "BTCUSDT", Quantity: 100, PnL: 1000, Fee: 10, CreatedAt: now},
	} {
		item := trade
		if err := store.SaveTrade(&item); err != nil {
			t.Fatal(err)
		}
	}
	previousStatus, previousStorage := currentStatus, storageServiceProvider
	currentStatus = &SystemStatus{Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures", QuoteAsset: "USDT", AccountScope: "scope-a"}
	SetStorageServiceProvider(&testStorageProvider{st: store})
	t.Cleanup(func() {
		currentStatus = previousStatus
		SetStorageServiceProvider(previousStorage)
	})
	request := httptest.NewRequest(http.MethodGet, "/api/statistics?exchange=binance&symbol=BTCUSDT&market_type=futures", nil)
	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)
	ctx.Request = request
	getStatistics(ctx)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if payload["pnl_verified"] != true || payload["pnl_asset"] != "USDT" || payload["total_pnl"] != float64(8) || payload["total_trades"] != float64(1) {
		t.Fatalf("statistics must be filtered by exact market/scope/asset: %s", response.Body.String())
	}
}

func TestDailyStatisticsUsesExactMarketScopeAndSuppressesUnverifiedSnapshotPnL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store, err := storage.NewSQLStorage(t.TempDir() + "/daily-statistics-dimension.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	now := time.Now().UTC()
	for _, trade := range []storage.Trade{
		{Account: "rotated-label", AccountScope: "scope-a", Exchange: "binance", MarketType: "futures", PnLAsset: "USDT", FeeAsset: "USDT", Symbol: "BTCUSDT", Quantity: 1, PnL: 10, Fee: 2, CreatedAt: now},
		{Account: "rotated-label", AccountScope: "scope-a", Exchange: "binance", MarketType: "spot", PnLAsset: "USDT", FeeAsset: "USDT", Symbol: "BTCUSDT", Quantity: 10, PnL: 100, Fee: 1, CreatedAt: now},
		{Account: "other", AccountScope: "scope-b", Exchange: "binance", MarketType: "futures", PnLAsset: "USDT", FeeAsset: "USDT", Symbol: "BTCUSDT", Quantity: 100, PnL: 1000, Fee: 10, CreatedAt: now},
	} {
		item := trade
		if err := store.SaveTrade(&item); err != nil {
			t.Fatal(err)
		}
	}
	previousStatus, previousStorage := currentStatus, storageServiceProvider
	currentStatus = &SystemStatus{Exchange: "binance", Symbol: "BTCUSDT", MarketType: "futures", QuoteAsset: "USDT", AccountScope: "scope-a"}
	SetStorageServiceProvider(&testStorageProvider{st: store})
	t.Cleanup(func() {
		currentStatus = previousStatus
		SetStorageServiceProvider(previousStorage)
	})
	request := httptest.NewRequest(http.MethodGet, "/api/statistics/daily?exchange=binance&symbol=BTCUSDT&market_type=futures&days=2", nil)
	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)
	ctx.Request = request
	getDailyStatistics(ctx)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		Statistics []map[string]interface{} `json:"statistics"`
		Verified   bool                     `json:"pnl_verified"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.Verified || len(payload.Statistics) != 1 {
		t.Fatalf("expected one verified scoped daily row: %s", response.Body.String())
	}
	row := payload.Statistics[0]
	if row["pnl_verified"] != true || row["total_pnl"] != float64(8) || row["pnl_asset"] != "USDT" || row["funding_fee"] != nil || row["unrealized_pnl"] != nil || row["book_value_pnl"] != nil {
		t.Fatalf("daily API leaked mixed or unverified PnL: %s", response.Body.String())
	}
}

func TestPnLTimeRangeOmitsSnapshotUnrealizedPnLWithoutAssetMetadata(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store, err := storage.NewSQLStorage(t.TempDir() + "/pnl-time-range-snapshot.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	now := time.Now().UTC().Truncate(time.Second)
	accountID := GetCurrentAccountID()
	if accountID == "" {
		accountID = "default"
	}
	fcm := NewFileConfigManager("")
	cfg := config.CreateMinimalConfig()
	cfg.Exchanges = map[string]config.ExchangeConfig{"binance": {APIKey: "pnl-snapshot-test-key", SecretKey: "pnl-snapshot-test-secret"}}
	cfg.App.CurrentExchange = "binance"
	if err := fcm.SetRuntimeConfig(cfg); err != nil {
		t.Fatal(err)
	}
	previousConfigManager := fileConfigManager
	SetFileConfigManager(fcm)
	t.Cleanup(func() { SetFileConfigManager(previousConfigManager) })
	scope := accountScopeForExchange("binance")
	trade := storage.Trade{Account: accountID, AccountScope: scope, Exchange: "binance", MarketType: "futures", PnLAsset: "USDT", FeeAsset: "USDT", Symbol: "BTCUSDT", Quantity: 1, PnL: 3, CreatedAt: now}
	if err := store.SaveTrade(&trade); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveDailySnapshot(&storage.DailySnapshot{Exchange: "binance", MarketType: "futures", AccountScope: scope, Symbol: "BTCUSDT", Account: accountID, Date: now, SnapshotTime: now, UnrealizedPnL: 987.65}); err != nil {
		t.Fatal(err)
	}
	previousStorage := storageServiceProvider
	SetStorageServiceProvider(&testStorageProvider{st: store})
	t.Cleanup(func() { SetStorageServiceProvider(previousStorage) })
	request := httptest.NewRequest(http.MethodGet, "/api/statistics/pnl/time-range?start_time="+now.Add(-time.Hour).Format(time.RFC3339)+"&end_time="+now.Add(time.Hour).Format(time.RFC3339), nil)
	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)
	ctx.Request = request
	getPnLByTimeRange(ctx)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		PnLBySymbol []map[string]interface{} `json:"pnl_by_symbol"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if len(payload.PnLBySymbol) != 1 {
		t.Fatalf("expected one scoped PnL row: %s", response.Body.String())
	}
	if _, leaked := payload.PnLBySymbol[0]["unrealized_pnl"]; leaked {
		t.Fatalf("snapshot PnL without denomination escaped time-range API: %s", response.Body.String())
	}
}

func TestDailyBreakdownSnapshotPnLDefaultsToNullAndUnverified(t *testing.T) {
	payload, err := json.Marshal(DailyPnLBreakdownSummary{})
	if err != nil {
		t.Fatal(err)
	}
	var summary map[string]interface{}
	if err := json.Unmarshal(payload, &summary); err != nil {
		t.Fatal(err)
	}
	if summary["unrealized_pnl_start"] != nil || summary["unrealized_pnl_end"] != nil || summary["unrealized_pnl_start_verified"] != false || summary["unrealized_pnl_end_verified"] != false {
		t.Fatalf("unverified snapshot PnL must serialize as null with an explicit false verification flag: %s", payload)
	}
}
