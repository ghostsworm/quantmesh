package web

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"quantmesh/storage"
)

type dailyBreakdownFailingStorage struct {
	storage.Storage
	failure string
}

func (s dailyBreakdownFailingStorage) GetDailyTradesSummary(string, string, string, string) (int, float64, float64, error) {
	return 0, 0, 0, errors.New(s.failure)
}

func (s dailyBreakdownFailingStorage) QueryDailyPnLTrades(string, string, string, string, string, string, time.Time, time.Time) (int, float64, float64, []*storage.Trade, []*storage.Trade, error) {
	return 0, 0, 0, nil, nil, errors.New(s.failure)
}

func (s dailyBreakdownFailingStorage) QueryDailyOrderFillsByScope(string, string, string, string, string, time.Time, time.Time) (storage.DailyOrderFillSummary, error) {
	return storage.DailyOrderFillSummary{}, errors.New(s.failure)
}

func (dailyBreakdownFailingStorage) GetOrderFillCoverage(exchange, marketType, symbol, accountScope string) (*storage.OrderFillCoverage, error) {
	return &storage.OrderFillCoverage{Exchange: exchange, MarketType: marketType, Symbol: symbol, AccountScope: accountScope,
		CoveredFrom: time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC), CoveredThrough: time.Date(2026, 9, 29, 0, 0, 0, 0, time.UTC)}, nil
}

type dailyBreakdownStorageProvider struct{ store storage.Storage }

func (p dailyBreakdownStorageProvider) GetStorage() storage.Storage { return p.store }

func TestDailyPnLBreakdownDoesNotReportStorageFailureAsZeroProfit(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousProvider := storageServiceProvider
	previousStatus := currentStatus
	storageServiceProvider = dailyBreakdownStorageProvider{store: dailyBreakdownFailingStorage{failure: "injected database failure"}}
	currentStatus = &SystemStatus{Exchange: "binance", Symbol: "BTCUSDT", MarketType: "spot", QuoteAsset: "USDT", AccountScope: "scope-a"}
	t.Cleanup(func() {
		storageServiceProvider = previousProvider
		currentStatus = previousStatus
	})

	router := gin.New()
	router.GET("/api/statistics/daily/breakdown", getDailyPnLBreakdown)
	request := httptest.NewRequest("GET", "/api/statistics/daily/breakdown?date=2026-09-26&market_type=spot", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != 500 {
		t.Fatalf("database error must not become a successful zero-profit response: status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestDailyPnLBreakdownFailsClosedWithoutAccountScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousProvider := storageServiceProvider
	previousStatus := currentStatus
	storageServiceProvider = dailyBreakdownStorageProvider{store: dailyBreakdownFailingStorage{failure: "must not query"}}
	currentStatus = nil
	t.Cleanup(func() {
		storageServiceProvider = previousProvider
		currentStatus = previousStatus
	})

	router := gin.New()
	router.GET("/api/statistics/daily/breakdown", getDailyPnLBreakdown)
	request := httptest.NewRequest("GET", "/api/statistics/daily/breakdown?date=2026-09-27&exchange=binance&symbol=BTCUSDT&market_type=spot", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != 503 {
		t.Fatalf("missing account credential scope must fail closed: status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestDailyPnLBreakdownFailsClosedWithoutFullDayExecutionCoverage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousProvider := storageServiceProvider
	previousStatus := currentStatus
	storageServiceProvider = dailyBreakdownStorageProvider{store: dailyBreakdownCoverageMissingStorage{Storage: dailyBreakdownFailingStorage{failure: "must not query"}}}
	currentStatus = &SystemStatus{Exchange: "binance", Symbol: "BTCUSDT", MarketType: "spot", AccountScope: "scope-a"}
	t.Cleanup(func() { storageServiceProvider, currentStatus = previousProvider, previousStatus })
	router := gin.New()
	router.GET("/api/statistics/daily/breakdown", getDailyPnLBreakdown)
	request := httptest.NewRequest("GET", "/api/statistics/daily/breakdown?date=2026-09-26&market_type=spot", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("incomplete execution coverage must fail closed: status=%d body=%s", response.Code, response.Body.String())
	}
}

type dailyBreakdownFeeAssetStorage struct {
	dailyBreakdownFailingStorage
	fees map[string]float64
}

func (s dailyBreakdownFeeAssetStorage) QueryDailyOrderFillsByScope(string, string, string, string, string, time.Time, time.Time) (storage.DailyOrderFillSummary, error) {
	return storage.DailyOrderFillSummary{FeesByAsset: s.fees}, nil
}

func TestDailyPnLBreakdownRejectsUnconvertedFeeAsset(t *testing.T) {
	gin.SetMode(gin.TestMode)
	previousProvider, previousStatus := storageServiceProvider, currentStatus
	base := dailyBreakdownFailingStorage{failure: "must not continue after fee validation"}
	storageServiceProvider = dailyBreakdownStorageProvider{store: dailyBreakdownFeeAssetStorage{dailyBreakdownFailingStorage: base, fees: map[string]float64{"BNB": 0.01}}}
	currentStatus = &SystemStatus{Exchange: "binance", Symbol: "BTCUSDT", MarketType: "spot", QuoteAsset: "USDT", AccountScope: "scope-a"}
	t.Cleanup(func() { storageServiceProvider, currentStatus = previousProvider, previousStatus })
	router := gin.New()
	router.GET("/api/statistics/daily/breakdown", getDailyPnLBreakdown)
	request := httptest.NewRequest("GET", "/api/statistics/daily/breakdown?date=2026-09-26&market_type=spot", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("unconverted BNB fees must not be combined with USDT PnL: status=%d body=%s", response.Code, response.Body.String())
	}
}

type dailyBreakdownCoverageMissingStorage struct{ storage.Storage }

func (dailyBreakdownCoverageMissingStorage) GetOrderFillCoverage(string, string, string, string) (*storage.OrderFillCoverage, error) {
	return nil, nil
}
