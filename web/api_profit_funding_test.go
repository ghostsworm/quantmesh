package web

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/storage"

	"github.com/gin-gonic/gin"
)

type marginInterestProfitReaderStub struct {
	from, through time.Time
	cost          float64
	unvalued      int64
	calls         int
}

type fundingIncomeCoverageFixture struct {
	streams        []*storage.PnLBySymbol
	fundingSymbols []string
	coverage       map[string][2]time.Time
}

type orderFillCoverageFixture struct {
	streams        []*storage.PnLBySymbol
	rawStreams     []storage.OrderFillStream
	invalidRawRows int64
	coverage       map[string]*storage.OrderFillCoverage
	pending        map[string]int
	integrity      map[string]storage.OrderFillLedgerIntegrity
}

func (f orderFillCoverageFixture) GetPnLByAccountScopeAndAsset(_, _, _ string, _, _ time.Time) ([]*storage.PnLBySymbol, error) {
	return f.streams, nil
}

func (f orderFillCoverageFixture) GetOrderFillCoverage(exchange, marketType, symbol, scope string) (*storage.OrderFillCoverage, error) {
	return f.coverage[strings.ToLower(exchange)+"/"+strings.ToLower(marketType)+"/"+strings.ToUpper(symbol)+"/"+scope], nil
}

func (f orderFillCoverageFixture) CountPendingTradeFeeCorrectionsForAccount(exchange, marketType, symbol, scope string) (int, error) {
	return f.pending[strings.ToLower(exchange)+"/"+strings.ToLower(marketType)+"/"+strings.ToUpper(symbol)+"/"+scope], nil
}

func (f orderFillCoverageFixture) ListOrderFillStreamsByAccountScope(_, _ string) ([]storage.OrderFillStream, int64, error) {
	return f.rawStreams, f.invalidRawRows, nil
}

func (f orderFillCoverageFixture) GetOrderFillLedgerIntegrity(exchange, marketType, symbol, scope, _ string) (storage.OrderFillLedgerIntegrity, error) {
	key := strings.ToLower(exchange) + "/" + strings.ToLower(marketType) + "/" + strings.ToUpper(symbol) + "/" + scope
	result, exists := f.integrity[key]
	if !exists {
		result.FeeTotalsMatch = true
	}
	return result, nil
}

func TestSumDailyProfitInRangeIncludesPriorMonthWeekDaysOnly(t *testing.T) {
	daily := map[string]float64{
		"2026-09-28": 10,
		"2026-09-29": 20,
		"2026-10-01": 30,
		"2026-10-04": 40,
	}
	week, err := sumDailyProfitInRange(daily, "2026-09-28", "2026-10-04")
	if err != nil || week != 100 {
		t.Fatalf("cross-month week total=%v err=%v, want 100", week, err)
	}
	month, err := sumDailyProfitInRange(daily, "2026-10-01", "2026-10-04")
	if err != nil || month != 70 {
		t.Fatalf("month total=%v err=%v, want 70", month, err)
	}
}

func TestProfitCompletenessRequiresDailyEquityReconciliation(t *testing.T) {
	if profitPeriodVerified(true, true, true, true, false) {
		t.Fatal("complete transaction ledgers without daily equity/cash-flow reconciliation must not be declared net-profit verified")
	}
	if !profitPeriodVerified(true, true, true, true, true) {
		t.Fatal("fully reconciled account and ledgers should pass the completeness gate")
	}
}

func TestReadScopedOrderFillCoverageWindowsFailsClosed(t *testing.T) {
	start := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 10, 19, 0, 0, 0, 0, time.UTC)
	window := []profitCoverageWindow{{start: start, end: end}}
	scopes := []profitAccountScope{{exchange: "binance", scope: "scope-a", futuresSymbols: []string{"BTCUSDT"}}}
	completeCoverage := func(market, symbol string) *storage.OrderFillCoverage {
		return &storage.OrderFillCoverage{Exchange: "binance", MarketType: market, Symbol: symbol, AccountScope: "scope-a", CoveredFrom: start.Add(-time.Hour), CoveredThrough: end.Add(time.Hour)}
	}
	fixture := orderFillCoverageFixture{
		streams:    []*storage.PnLBySymbol{{Exchange: "binance", MarketType: "spot", Symbol: "ETHUSDT", PnLAsset: "USDT"}},
		rawStreams: []storage.OrderFillStream{{Exchange: "binance", MarketType: "spot", Symbol: "ETHUSDT"}},
		coverage: map[string]*storage.OrderFillCoverage{
			"binance/futures/BTCUSDT/scope-a": completeCoverage("futures", "BTCUSDT"),
			"binance/spot/ETHUSDT/scope-a":    completeCoverage("spot", "ETHUSDT"),
		},
		pending: map[string]int{},
	}
	results, err := readScopedOrderFillCoverageWindows(fixture, scopes, window)
	if err != nil || len(results) != 1 || !results[0] {
		t.Fatalf("complete execution history=%v err=%v", results, err)
	}
	delete(fixture.coverage, "binance/spot/ETHUSDT/scope-a")
	results, err = readScopedOrderFillCoverageWindows(fixture, scopes, window)
	if err != nil || results[0] {
		t.Fatalf("missing spot execution coverage must be incomplete: %v err=%v", results, err)
	}
	fixture.coverage["binance/spot/ETHUSDT/scope-a"] = completeCoverage("spot", "ETHUSDT")
	fixture.streams = append(fixture.streams, &storage.PnLBySymbol{Exchange: "binance", MarketType: "unknown", Symbol: "XRPUSDT", PnLAsset: "USDT"})
	results, err = readScopedOrderFillCoverageWindows(fixture, scopes, window)
	if err != nil || results[0] {
		t.Fatalf("unknown market stream must fail closed: %v err=%v", results, err)
	}
	fixture.streams = fixture.streams[:1]
	fixture.rawStreams = append(fixture.rawStreams, storage.OrderFillStream{Exchange: "binance", MarketType: "futures", Symbol: "SOLUSDT"})
	fixture.coverage["binance/futures/SOLUSDT/scope-a"] = completeCoverage("futures", "SOLUSDT")
	results, err = readScopedOrderFillCoverageWindows(fixture, scopes, window)
	if err != nil || !results[0] {
		t.Fatalf("raw-only stream with coverage should be discovered and verified: %v err=%v", results, err)
	}
	fixture.integrity = map[string]storage.OrderFillLedgerIntegrity{
		"binance/futures/SOLUSDT/scope-a": {UnpairedOrderCount: 1, FeeTotalsMatch: false},
	}
	results, err = readScopedOrderFillCoverageWindows(fixture, scopes, window)
	if err != nil || results[0] {
		t.Fatalf("orphan raw fill must block complete profit claims: %v err=%v", results, err)
	}
	fixture.integrity["binance/futures/SOLUSDT/scope-a"] = storage.OrderFillLedgerIntegrity{MissingFillOrderCount: 1, FeeTotalsMatch: true}
	results, err = readScopedOrderFillCoverageWindows(fixture, scopes, window)
	if err != nil || results[0] {
		t.Fatalf("paired trade missing raw fills must block complete profit claims: %v err=%v", results, err)
	}
	fixture.rawStreams = fixture.rawStreams[:1]
	fixture.integrity = nil
	fixture.pending["binance/spot/ETHUSDT/scope-a"] = 1
	results, err = readScopedOrderFillCoverageWindows(fixture, scopes, window)
	if err != nil || results[0] {
		t.Fatalf("unresolved fee correction must make execution history incomplete: %v err=%v", results, err)
	}
}

func TestAppendRetiredProfitAccountScopesKeepsOldHistoryAndFailsClosed(t *testing.T) {
	payload := []byte(`{"version":1,"revision":7,"accounts":[{"exchange":"BINANCE","market_type":"futures","scope":"old-scope","status":"pending_verification"},{"exchange":"binance","market_type":"spot","scope":"current-scope","status":"ready_for_explicit_reset"},{"exchange":"okx","market_type":"spot_margin","scope":"other-scope","status":"pending_verification"}]}`)
	current := []profitAccountScope{{exchange: "binance", scope: "current-scope"}}
	got, complete, err := appendRetiredProfitAccountScopes(current, "binance", payload, 7)
	if err != nil || !complete || len(got) != 2 || got[1].scope != "old-scope" {
		t.Fatalf("archived scope history not retained safely: scopes=%+v complete=%t err=%v", got, complete, err)
	}
	got, complete, err = appendRetiredProfitAccountScopes(current, "", payload, 7)
	if err != nil || !complete || len(got) != 3 {
		t.Fatalf("all exchange archives not retained for aggregate report: scopes=%+v complete=%t err=%v", got, complete, err)
	}
	if !got[2].unsupportedMarginInterestUntracked {
		t.Fatal("retired unsupported spot-margin interest was incorrectly treated as tracked")
	}
	for _, tc := range []struct {
		name     string
		payload  []byte
		revision int64
	}{
		{name: "revision mismatch", payload: payload, revision: 8},
		{name: "invalid archive", payload: []byte(`{"version":1}`), revision: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, complete, err := appendRetiredProfitAccountScopes(current, "", tc.payload, tc.revision); err == nil || complete {
				t.Fatalf("invalid retired scope archive was attested complete: complete=%t err=%v", complete, err)
			}
		})
	}
}

func TestAccountExecutionScopeInventoryRequiresEveryStoredScopeToBeKnown(t *testing.T) {
	known := []profitAccountScope{{exchange: "binance", scope: "current"}, {exchange: "binance", scope: "archived"}}
	if !accountExecutionScopesCovered(known, []storage.ExecutionAccountScope{{Exchange: "binance", Scope: "current"}, {Exchange: "BINANCE", Scope: "archived"}}, 0) {
		t.Fatal("current and explicitly archived account scopes should be covered")
	}
	if accountExecutionScopesCovered(known[:1], []storage.ExecutionAccountScope{{Exchange: "binance", Scope: "unarchived-old"}}, 0) {
		t.Fatal("an unarchived database scope must not be treated as complete account history")
	}
	if accountExecutionScopesCovered(known, nil, 1) {
		t.Fatal("unattributed executions must keep account history incomplete")
	}
}

func TestProfitAccountHistoryRejectsUnarchivedDatabaseScope(t *testing.T) {
	store, err := storage.NewSQLStorage(t.TempDir() + "/unarchived-profit-scope.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	previousConfig := fileConfigManager
	fcm := NewFileConfigManager("")
	cfg := config.CreateMinimalConfig()
	cfg.Exchanges = map[string]config.ExchangeConfig{"binance": {APIKey: "scope-inventory-test", SecretKey: "scope-inventory-test-secret"}}
	if err := fcm.SetRuntimeConfig(cfg); err != nil {
		t.Fatal(err)
	}
	SetFileConfigManager(fcm)
	t.Cleanup(func() { SetFileConfigManager(previousConfig) })
	if err := store.SaveTrade(&storage.Trade{BuyOrderID: 1, SellOrderID: 2, Exchange: "binance", MarketType: "futures", PnLAsset: "USDT", AccountScope: "not-in-config-or-archive", Symbol: "BTCUSDT", Quantity: 1, PnL: 1, CreatedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	_, complete, err := resolveProfitAccountScopesWithHistory(context.Background(), "binance", store)
	if err != nil {
		t.Fatal(err)
	}
	if complete {
		t.Fatal("unarchived historical scope in durable trades must make account history incomplete")
	}
}

func TestFundingRecoveryReleaseErrorReportsDurableHoldState(t *testing.T) {
	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)
	writeFundingRecoveryReleaseError(ctx, &FundingRecoveryReleaseStateError{HoldReleased: true, Cause: errors.New("injected Gate readback failure")})
	var payload map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusConflict || payload["hold_released"] != true || payload["recovery_gate_cleared"] != false || payload["execution_replayed"] != false {
		t.Fatalf("release API contradicted durable facts or implied replay: status=%d payload=%v", response.Code, payload)
	}
}

func (f fundingIncomeCoverageFixture) GetFundingIncomeCoverage(exchange, symbol, marketType, scope string) (time.Time, time.Time, error) {
	coverage := f.coverage[strings.ToLower(exchange)+"/"+strings.ToUpper(symbol)+"/"+strings.ToLower(marketType)+"/"+scope]
	return coverage[0], coverage[1], nil
}

func (f fundingIncomeCoverageFixture) GetPnLByAccountScopeAndAsset(_, _, _ string, _, _ time.Time) ([]*storage.PnLBySymbol, error) {
	return f.streams, nil
}

func (f fundingIncomeCoverageFixture) GetFundingPaymentSymbolsByAccountScope(_, _, _ string, _, _ time.Time) ([]string, error) {
	return f.fundingSymbols, nil
}

func TestReadScopedFundingIncomeCoverageRequiresEveryConfiguredAndHistoricalFuturesSymbol(t *testing.T) {
	start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	end := start.Add(24 * time.Hour)
	covered := [2]time.Time{start.Add(-time.Hour), end.Add(time.Hour)}
	full := fundingIncomeCoverageFixture{
		streams: []*storage.PnLBySymbol{
			{Exchange: "binance", MarketType: "futures", Symbol: "ETHUSDT", PnLAsset: "USDT"},
			{Exchange: "binance", MarketType: "spot", Symbol: "BNBUSDT", PnLAsset: "USDT"},
		},
		fundingSymbols: []string{"DOGEUSDT"},
		coverage: map[string][2]time.Time{
			"binance/BTCUSDT/futures/scope-a":  covered,
			"binance/ETHUSDT/futures/scope-a":  covered,
			"binance/DOGEUSDT/futures/scope-a": covered,
		},
	}
	scopes := []profitAccountScope{{exchange: "binance", scope: "scope-a", futuresSymbols: []string{"BTCUSDT"}}}
	complete, err := readScopedFundingIncomeCoverage(full, scopes, start, end)
	if err != nil || !complete {
		t.Fatalf("complete configured and historical streams must be verified: complete=%v err=%v", complete, err)
	}
	delete(full.coverage, "binance/ETHUSDT/futures/scope-a")
	complete, err = readScopedFundingIncomeCoverage(full, scopes, start, end)
	if err != nil || complete {
		t.Fatalf("missing historical-symbol coverage must be incomplete: complete=%v err=%v", complete, err)
	}
	delete(full.coverage, "binance/DOGEUSDT/futures/scope-a")
	complete, err = readScopedFundingIncomeCoverage(full, scopes, start, end)
	if err != nil || complete {
		t.Fatalf("funding-ledger symbols without PnL must still require coverage: complete=%v err=%v", complete, err)
	}
	full.coverage["binance/ETHUSDT/futures/scope-a"] = covered
	full.coverage["binance/DOGEUSDT/futures/scope-a"] = covered
	full.streams = append(full.streams, &storage.PnLBySymbol{Exchange: "binance", MarketType: "unknown", Symbol: "XRPUSDT", PnLAsset: "USDT"})
	complete, err = readScopedFundingIncomeCoverage(full, scopes, start, end)
	if err != nil || complete {
		t.Fatalf("unknown market classification must fail closed: complete=%v err=%v", complete, err)
	}
}

func TestConfiguredFundingSymbolsForExchangeIncludesBothPerpSpreadLegs(t *testing.T) {
	cfg := &config.Config{Bots: []config.BotConfig{{
		MarketType: config.MarketTypeFundingPerpSpread,
		FundingPerpSpread: &config.FundingPerpSpreadConfig{
			LegA: config.FundingPerpLeg{Exchange: "BINANCE", Symbol: "BTCUSDT"},
			LegB: config.FundingPerpLeg{Exchange: "okx", Symbol: "BTC-USDT-SWAP"},
		},
	}}}
	got := configuredFundingSymbolsForExchange("okx", cfg)
	if len(got) != 1 || got[0] != "BTC-USDT-SWAP" {
		t.Fatalf("configuredFundingSymbolsForExchange()=%v, want second perp leg", got)
	}
}

func TestConfiguredProfitTradeStreamsIncludesPerpSpreadLegsAndSpotSymbols(t *testing.T) {
	cfg := &config.Config{
		Bots: []config.BotConfig{
			{Exchange: "binance", MarketType: config.MarketTypeFundingPerpSpread,
				FundingPerpSpread: &config.FundingPerpSpreadConfig{
					LegA: config.FundingPerpLeg{Exchange: "binance", Symbol: "BTCUSDT"},
					LegB: config.FundingPerpLeg{Exchange: "okx", Symbol: "ETH-USDT-SWAP"},
				}},
			{Exchange: "binance", Symbol: "SOLUSDT", MarketType: config.MarketTypeFundingCarry},
		},
	}
	cfg.Trading.Symbols = []config.SymbolConfig{{Exchange: "binance", Symbol: "ETHUSDT", MarketType: "spot"}}
	got := configuredProfitTradeStreamsForExchange("binance", cfg)
	want := map[profitTradeStream]bool{
		{market: "futures", symbol: "BTCUSDT"}: true,
		{market: "spot", symbol: "ETHUSDT"}:    true,
		{market: "spot", symbol: "SOLUSDT"}:    true,
		{market: "futures", symbol: "SOLUSDT"}: true,
	}
	if len(got) != len(want) {
		t.Fatalf("configured trade streams=%+v, want %+v", got, want)
	}
	for _, stream := range got {
		if !want[stream] {
			t.Fatalf("unexpected configured trade stream: %+v", stream)
		}
	}
}

func TestReadScopedFundingIncomeCoverageWindowsVerifiesPeriodsIndependently(t *testing.T) {
	today := time.Date(2026, 10, 2, 0, 0, 0, 0, time.UTC)
	now := today.Add(12 * time.Hour)
	fixture := fundingIncomeCoverageFixture{
		coverage: map[string][2]time.Time{
			"binance/BTCUSDT/futures/scope-a": {today, now},
		},
	}
	results, err := readScopedFundingIncomeCoverageWindows(fixture, []profitAccountScope{
		{exchange: "binance", scope: "scope-a", futuresSymbols: []string{"BTCUSDT"}},
	}, []profitCoverageWindow{
		{start: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC), end: now},
		{start: today, end: now},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || results[0] || !results[1] {
		t.Fatalf("coverage completeness by period=%v, want [false true]", results)
	}
}

func TestExchangeUsesSpotMarginAcrossCurrentAndLegacyConfig(t *testing.T) {
	legacyConfig := &config.Config{}
	legacyConfig.App.CurrentExchange = "bybit"
	legacyConfig.Trading.Symbols = []config.SymbolConfig{{MarketType: "spot", UseSpotMargin: true}}
	tests := []struct {
		name     string
		cfg      *config.Config
		exchange string
		want     bool
	}{
		{name: "bot spot margin", cfg: &config.Config{Bots: []config.BotConfig{{Exchange: "okx", MarketType: "spot", UseSpotMargin: true}}}, exchange: "OKX", want: true},
		{name: "legacy symbol spot margin", cfg: legacyConfig, exchange: "BYBIT", want: true},
		{name: "plain spot", cfg: &config.Config{Bots: []config.BotConfig{{Exchange: "okx", MarketType: "spot"}}}, exchange: "okx", want: false},
		{name: "other exchange", cfg: &config.Config{Bots: []config.BotConfig{{Exchange: "okx", MarketType: "spot", UseSpotMargin: true}}}, exchange: "bybit", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := exchangeUsesSpotMargin(tt.exchange, tt.cfg); got != tt.want {
				t.Fatalf("exchangeUsesSpotMargin()=%v, want %v", got, tt.want)
			}
		})
	}
}

func TestReadScopedMarginInterestProfitTotalsMarksUnsupportedMarginVenueIncomplete(t *testing.T) {
	end := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	start := end.Add(-24 * time.Hour)
	totals, err := readScopedMarginInterestProfitTotals(nil, []profitAccountScope{{exchange: "okx", scope: "scope-a", unsupportedMarginInterestUntracked: true}}, start, end)
	if err != nil || totals.Total != 0 || totals.Complete {
		t.Fatalf("untracked spot-margin interest must invalidate net-profit completeness: totals=%+v err=%v", totals, err)
	}
	plainFutures, err := readScopedMarginInterestProfitTotals(nil, []profitAccountScope{{exchange: "okx", scope: "scope-a"}}, start, end)
	if err != nil || !plainFutures.Complete {
		t.Fatalf("unsupported interest coverage must not affect a non-margin venue: totals=%+v err=%v", plainFutures, err)
	}
}

func (s *marginInterestProfitReaderStub) GetMarginInterestCoverage(_, _, _ string) (time.Time, time.Time, error) {
	return s.from, s.through, nil
}

func (s *marginInterestProfitReaderStub) GetMarginInterestValuationTotalsByAccountScope(_, _ string, _, _ time.Time) (float64, int64, error) {
	s.calls++
	return s.cost, s.unvalued, nil
}

func TestReadScopedMarginInterestProfitTotalsDeductsKnownCostAndFlagsGaps(t *testing.T) {
	end := time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)
	start := end.Add(-24 * time.Hour)
	partialReader := &marginInterestProfitReaderStub{from: end.Add(-6 * time.Hour), through: end.Add(-5 * time.Minute), cost: 12.5}
	partial, err := readScopedMarginInterestProfitTotals(partialReader, []profitAccountScope{
		{exchange: "binance", scope: "scope-a"}, {exchange: "okx", scope: "scope-b"},
	}, start, end)
	if err != nil || partial.Total != 12.5 || partial.Complete || partialReader.calls != 1 {
		t.Fatalf("partial interest coverage total=%+v calls=%d err=%v", partial, partialReader.calls, err)
	}
	completeReader := &marginInterestProfitReaderStub{from: start, through: end, cost: 2.25}
	complete, err := readScopedMarginInterestProfitTotals(completeReader, []profitAccountScope{{exchange: "binance", scope: "scope-a"}}, start, end)
	if err != nil || complete.Total != 2.25 || !complete.Complete {
		t.Fatalf("complete interest coverage total=%+v err=%v", complete, err)
	}
	unvaluedReader := &marginInterestProfitReaderStub{from: start, through: end, cost: 1, unvalued: 1}
	unvalued, err := readScopedMarginInterestProfitTotals(unvaluedReader, []profitAccountScope{{exchange: "binance", scope: "scope-a"}}, start, end)
	if err != nil || unvalued.Total != 1 || unvalued.Complete {
		t.Fatalf("unvalued margin charge should mark net profit incomplete: total=%+v err=%v", unvalued, err)
	}
	missing, err := readScopedMarginInterestProfitTotals(nil, []profitAccountScope{{exchange: "binance", scope: "scope-a"}}, start, end)
	if err != nil || missing.Total != 0 || missing.Complete {
		t.Fatalf("missing interest reader must not assert complete net profit: total=%+v err=%v", missing, err)
	}
}

func TestProfitTrendDeclaresItsIncompleteIncomeBasis(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store, err := storage.NewSQLStorage(t.TempDir() + "/profit-trend-basis.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	previousStorage, previousConfigManager := storageServiceProvider, fileConfigManager
	SetStorageServiceProvider(&testStorageProvider{st: store})
	fcm := NewFileConfigManager("")
	cfg := config.CreateMinimalConfig()
	cfg.Exchanges = map[string]config.ExchangeConfig{"binance": {APIKey: "profit-trend-basis-test", SecretKey: "profit-trend-basis-secret"}}
	if err := fcm.SetRuntimeConfig(cfg); err != nil {
		t.Fatal(err)
	}
	SetFileConfigManager(fcm)
	t.Cleanup(func() {
		SetStorageServiceProvider(previousStorage)
		SetFileConfigManager(previousConfigManager)
	})
	request := httptest.NewRequest(http.MethodGet, "/api/profit/trend?period=7d&exchange_id=binance", nil)
	response := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(response)
	ctx.Request = request
	getProfitTrendHandler(ctx)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var payload struct {
		IncomeBasis              string `json:"income_basis"`
		ExecutionHistoryComplete bool   `json:"execution_history_complete"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	const expectedBasis = "account_scoped_paired_trade_pnl_net_of_recorded_fees_excludes_funding_and_margin_interest"
	if payload.IncomeBasis != expectedBasis {
		t.Fatalf("profit trend income basis=%q, want %q", payload.IncomeBasis, expectedBasis)
	}
	if payload.ExecutionHistoryComplete {
		t.Fatal("profit trend must report incomplete execution history when no configured stream coverage exists")
	}
}

func TestFundingCarryDashboardBotRowsNeverExposeAccountIncomeAsBotIncome(t *testing.T) {
	encoded, err := json.Marshal(fundingCarryDashboardSymbolInfo{Symbol: "BTCUSDT", BotID: "bot-1", Status: "running", Capital: 250})
	if err != nil {
		t.Fatal(err)
	}
	var row map[string]interface{}
	if err := json.Unmarshal(encoded, &row); err != nil {
		t.Fatal(err)
	}
	if row["bot_id"] != "bot-1" || row["symbol"] != "BTCUSDT" || row["capital"] != float64(250) {
		t.Fatalf("dashboard bot identity/capital fields are incomplete: %s", encoded)
	}
	for _, forbidden := range []string{"income_24h", "income_7d", "net_income", "net_profit"} {
		if _, exists := row[forbidden]; exists {
			t.Fatalf("dashboard Bot row must not publish unverified strategy income field %q: %s", forbidden, encoded)
		}
	}
}

type fundingTotalsStub struct {
	calls    int
	failCall int
}

type fundingCarrySumStub struct {
	total float64
	err   error
	calls int
}

func (s *fundingCarrySumStub) GetFundingPaymentsSumByScope(_, _, _, _, _ string, _, _ time.Time) (float64, error) {
	s.calls++
	return s.total, s.err
}

func TestReadFundingCarrySumFailsClosed(t *testing.T) {
	now := time.Now().UTC()
	tests := []struct {
		name  string
		total float64
		err   error
	}{
		{name: "non-finite", total: math.NaN()},
		{name: "query error", total: 10, err: errors.New("database unavailable")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reader := &fundingCarrySumStub{total: tt.total, err: tt.err}
			if _, err := readFundingCarrySum(reader, "binance", "BTCUSDT", "scope-a", now.Add(-time.Hour), now); err == nil {
				t.Fatal("invalid funding income was accepted")
			}
			if reader.calls != 1 {
				t.Fatalf("query calls=%d, want 1", reader.calls)
			}
		})
	}
	reader := &fundingCarrySumStub{total: -1.25}
	if got, err := readFundingCarrySum(reader, "binance", "BTCUSDT", "scope-a", now.Add(-time.Hour), now); err != nil || got != -1.25 {
		t.Fatalf("valid negative funding income=%v err=%v", got, err)
	}
	if _, err := readFundingCarrySum(reader, "binance", "BTCUSDT", "", now.Add(-time.Hour), now); err == nil {
		t.Fatal("empty account scope was accepted")
	}
}

func TestMergeFundingCarryDailyTotalsRejectsInvalidRowsAndOverflow(t *testing.T) {
	tests := []struct {
		name   string
		start  map[string]float64
		source map[string]float64
	}{
		{name: "invalid date", source: map[string]float64{"yesterday": 1}},
		{name: "non-finite amount", source: map[string]float64{"2026-10-01": math.Inf(1)}},
		{name: "overflow", start: map[string]float64{"2026-10-01": math.MaxFloat64}, source: map[string]float64{"2026-10-01": math.MaxFloat64}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := mergeFundingCarryDailyTotals(tt.start, tt.source); err == nil {
				t.Fatal("invalid daily funding aggregate was accepted")
			}
		})
	}
	totals := map[string]float64{}
	if err := mergeFundingCarryDailyTotals(totals, map[string]float64{"2026-10-01": 1.5}); err != nil || totals["2026-10-01"] != 1.5 {
		t.Fatalf("valid daily total=%v err=%v", totals, err)
	}
}

func TestFundingPaymentItemFromRecordRequiresExactScopeAndFiniteAmount(t *testing.T) {
	valid := &storage.FundingPayment{
		Exchange: "Binance", Symbol: "BTCUSDT", AccountScope: "scope-a", Asset: "USDT",
		Income: 1.123456789, TradeTime: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
	}
	item, err := fundingPaymentItemFromRecord(valid, "scope-a", "binance")
	if err != nil || item.Income != 1.12345679 {
		t.Fatalf("valid scoped funding row=%+v err=%v", item, err)
	}
	tests := []struct {
		name    string
		payment *storage.FundingPayment
	}{
		{name: "nil row"},
		{name: "wrong scope", payment: &storage.FundingPayment{Exchange: "binance", AccountScope: "scope-b", Asset: "USDT"}},
		{name: "wrong exchange", payment: &storage.FundingPayment{Exchange: "okx", AccountScope: "scope-a", Asset: "USDT"}},
		{name: "missing asset", payment: &storage.FundingPayment{Exchange: "binance", AccountScope: "scope-a"}},
		{name: "nan amount", payment: &storage.FundingPayment{Exchange: "binance", AccountScope: "scope-a", Asset: "USDT", Income: math.NaN()}},
		{name: "infinite amount", payment: &storage.FundingPayment{Exchange: "binance", AccountScope: "scope-a", Asset: "USDT", Income: math.Inf(1)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := fundingPaymentItemFromRecord(tt.payment, "scope-a", "binance"); err == nil {
				t.Fatal("invalid or out-of-scope funding row was accepted")
			}
		})
	}
}

func (s *fundingTotalsStub) GetFundingPaymentsSum(_, _ string, _, _ time.Time) (float64, error) {
	s.calls++
	if s.calls == s.failCall {
		return 0, errors.New("database unavailable")
	}
	return float64(s.calls), nil
}

func TestReadFundingProfitTotalsRejectsAnyFailedWindow(t *testing.T) {
	for failCall := 1; failCall <= 4; failCall++ {
		t.Run(string(rune('0'+failCall)), func(t *testing.T) {
			reader := &fundingTotalsStub{failCall: failCall}
			now := time.Now()
			_, err := readFundingProfitTotals(reader, "account", "binance", now.AddDate(-6, 0, 0), now.Add(-time.Hour), now.Add(-24*time.Hour), now.Add(-30*24*time.Hour), now)
			if err == nil {
				t.Fatalf("expected error for failed funding query %d", failCall)
			}
			if reader.calls != failCall {
				t.Fatalf("query calls=%d, want stop at failed query %d", reader.calls, failCall)
			}
		})
	}
}

func TestReadFundingProfitTotalsReturnsAllWindows(t *testing.T) {
	reader := &fundingTotalsStub{}
	now := time.Now()
	got, err := readFundingProfitTotals(reader, "account", "binance", now.AddDate(-6, 0, 0), now.Add(-time.Hour), now.Add(-24*time.Hour), now.Add(-30*24*time.Hour), now)
	if err != nil {
		t.Fatal(err)
	}
	if got.Total != 1 || got.Today != 2 || got.Week != 3 || got.Month != 4 {
		t.Fatalf("funding totals=%+v", got)
	}
}

func TestAddFiniteProfitValuesRejectsNonFiniteAndOverflow(t *testing.T) {
	tests := []struct {
		name   string
		values []float64
	}{
		{name: "nan", values: []float64{math.NaN()}},
		{name: "positive infinity", values: []float64{math.Inf(1)}},
		{name: "negative infinity", values: []float64{math.Inf(-1)}},
		{name: "sum overflow", values: []float64{math.MaxFloat64, math.MaxFloat64}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got, ok := addFiniteProfitValues(tt.values...); ok {
				t.Fatalf("addFiniteProfitValues() = %v, true; want rejected value", got)
			}
		})
	}
	if got, ok := addFiniteProfitValues(-3, 1.25); !ok || got != -1.75 {
		t.Fatalf("finite negative-profit sum = %v, %v; want -1.75, true", got, ok)
	}
	if got, ok := roundProfitToCents(math.MaxFloat64); !ok || got != math.MaxFloat64 {
		t.Fatalf("rounding a finite extreme must not overflow: %v, %v", got, ok)
	}
	if got, ok := roundProfitToCents(math.NaN()); ok || got != 0 {
		t.Fatalf("rounding NaN must fail closed: %v, %v", got, ok)
	}
	if got, ok := roundProfitToPrecision(1.123456789, 1e8); !ok || got != 1.12345679 {
		t.Fatalf("8-decimal precision rounding = %v, %v; want 1.12345679, true", got, ok)
	}
	if got, ok := roundProfitToPrecision(math.MaxFloat64, 1e8); !ok || got != math.MaxFloat64 {
		t.Fatalf("extreme finite funding amount must not overflow: %v, %v", got, ok)
	}
	if _, ok := roundProfitToPrecision(1, 0); ok {
		t.Fatal("zero precision was accepted")
	}
}

func TestAddFiniteProfitToMap(t *testing.T) {
	totals := map[string]float64{}
	if err := addFiniteProfitToMap(totals, "2026-10-01", 1.25); err != nil {
		t.Fatal(err)
	}
	if err := addFiniteProfitToMap(totals, "2026-10-01", 2.5); err != nil {
		t.Fatal(err)
	}
	if totals["2026-10-01"] != 3.75 {
		t.Fatalf("finite daily total=%v, want 3.75", totals["2026-10-01"])
	}
	for _, value := range []float64{math.NaN(), math.Inf(1)} {
		if err := addFiniteProfitToMap(totals, "2026-10-01", value); err == nil {
			t.Fatalf("non-finite or overflowing value %v was accepted", value)
		}
	}
	extremeTotals := map[string]float64{"2026-10-01": math.MaxFloat64}
	if err := addFiniteProfitToMap(extremeTotals, "2026-10-01", math.MaxFloat64); err == nil {
		t.Fatal("overflowing daily total was accepted")
	}
	if err := addFiniteProfitToMap(nil, "2026-10-01", 1); err == nil {
		t.Fatal("missing trend totals map was accepted")
	}
}

func TestMergeProfitStatisticsRejectsInvalidOrOverflowWithoutPartialMutation(t *testing.T) {
	tests := []struct {
		name   string
		source *storage.Statistics
	}{
		{name: "nil summary"},
		{name: "nan pnl", source: &storage.Statistics{TotalPnL: math.NaN()}},
		{name: "infinite volume", source: &storage.Statistics{TotalVolume: math.Inf(1)}},
		{name: "invalid win rate", source: &storage.Statistics{WinRate: 1.1}},
		{name: "volume overflow", source: &storage.Statistics{TotalVolume: math.MaxFloat64}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			target := &storage.Statistics{TotalVolume: math.MaxFloat64, TotalPnL: 4}
			before := *target
			err := mergeProfitStatistics(target, tt.source)
			if err == nil {
				t.Fatal("invalid statistics were accepted")
			}
			if *target != before {
				t.Fatalf("failed merge partially mutated target: before=%+v after=%+v", before, *target)
			}
		})
	}
}

func TestValidateProfitStatisticsSnapshot(t *testing.T) {
	valid := &storage.Statistics{TotalTrades: 1, TotalVolume: 2, TotalPnL: -1, TotalFee: 0.1, WinRate: 0.5}
	if err := validateProfitStatisticsSnapshot(valid); err != nil {
		t.Fatalf("valid summary rejected: %v", err)
	}
	invalid := []*storage.Statistics{
		nil,
		{TotalTrades: -1},
		{TotalVolume: math.NaN()},
		{TotalPnL: math.Inf(1)},
		{WinRate: 1.1},
	}
	for i, summary := range invalid {
		if err := validateProfitStatisticsSnapshot(summary); err == nil {
			t.Fatalf("invalid summary %d accepted", i)
		}
	}
}

func TestValidateStrategyPnLStreamRejectsCorruptFinancialRows(t *testing.T) {
	base := storage.PnLBySymbol{Exchange: "binance", MarketType: "futures", Symbol: "BTCUSDT", PnLAsset: "USDT", TotalTrades: 2, TotalVolume: 10, WinRate: .5, ExchangeWinRate: .25}
	tests := []struct {
		name   string
		mutate func(*storage.PnLBySymbol)
	}{
		{name: "nil row"},
		{name: "nan net pnl", mutate: func(row *storage.PnLBySymbol) { row.TotalPnL = math.NaN() }},
		{name: "infinite volume", mutate: func(row *storage.PnLBySymbol) { row.TotalVolume = math.Inf(1) }},
		{name: "invalid win rate", mutate: func(row *storage.PnLBySymbol) { row.WinRate = 1.1 }},
		{name: "negative trades", mutate: func(row *storage.PnLBySymbol) { row.TotalTrades = -1 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var row *storage.PnLBySymbol
			if tt.mutate != nil {
				copy := base
				tt.mutate(&copy)
				row = &copy
			}
			if err := validateStrategyPnLStream(row); err == nil {
				t.Fatal("corrupt PnL row was accepted")
			}
		})
	}
	if err := validateStrategyPnLStream(&base); err != nil {
		t.Fatalf("valid PnL row rejected: %v", err)
	}
}

func TestRoundStrategyProfitAmountsAvoidsOverflowAndRejectsNonFiniteValues(t *testing.T) {
	profit := &StrategyProfit{TotalProfit: math.MaxFloat64, WithdrawnProfit: -math.MaxFloat64}
	if err := roundStrategyProfitAmounts(profit); err != nil {
		t.Fatalf("finite extreme strategy amounts must not overflow during cents rounding: %v", err)
	}
	if profit.TotalProfit != math.MaxFloat64 || profit.WithdrawnProfit != -math.MaxFloat64 {
		t.Fatalf("extreme finite values changed unexpectedly: %+v", profit)
	}
	profit.ExchangeTotalProfit = math.NaN()
	if err := roundStrategyProfitAmounts(profit); err == nil {
		t.Fatal("non-finite strategy amount was accepted")
	}
}
