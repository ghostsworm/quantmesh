package monitor

import (
	"context"
	"testing"
	"time"

	"quantmesh/storage"
	"quantmesh/utils"
)

type marketSnapshotRuntime struct{ marketType string }

func (r marketSnapshotRuntime) Exchange() string                           { return "binance" }
func (r marketSnapshotRuntime) Symbol() string                             { return "BTCUSDT" }
func (r marketSnapshotRuntime) Account() string                            { return "acct" }
func (r marketSnapshotRuntime) MarketType() string                         { return r.marketType }
func (marketSnapshotRuntime) CurrentSnapshot() (float64, float64, float64) { return 100, 8, 200 }

type inventorySnapshotRuntime struct {
	marketSnapshotRuntime
	qty float64
}

type assetSnapshotRuntime struct{ marketSnapshotRuntime }

type unverifiedSnapshotRuntime struct{ marketSnapshotRuntime }

func (unverifiedSnapshotRuntime) CurrentSnapshotVerified() (float64, float64, float64, bool) {
	return 100, 0, 0, false
}

func (assetSnapshotRuntime) PnLAsset() string { return " usdt " }

func (r inventorySnapshotRuntime) SpotInventoryQty(context.Context) (float64, bool) {
	return r.qty, true
}

type accountMarketSnapshotRuntime struct {
	marketSnapshotRuntime
	calls *int
	scope string
}

func (r accountMarketSnapshotRuntime) AccountEquityUSDT(context.Context) (float64, bool) {
	*r.calls = *r.calls + 1
	return 1234, true
}
func (r accountMarketSnapshotRuntime) AccountScope() string { return r.scope }

type marketSnapshotStorage struct {
	storage.Storage
	hourly     []*storage.HourlyEquityRecord
	daily      []*storage.DailySnapshot
	account    []*storage.AccountEquityRecord
	queried    string
	queryStart time.Time
	queryEnd   time.Time
}

func (s *marketSnapshotStorage) SaveHourlyEquityRecord(record *storage.HourlyEquityRecord) error {
	s.hourly = append(s.hourly, record)
	return nil
}

func (s *marketSnapshotStorage) QueryHourlyEquityRecordsByMarketType(_, marketType, _, _ string, start, end time.Time) ([]*storage.HourlyEquityRecord, error) {
	s.queried = marketType
	s.queryStart, s.queryEnd = start, end
	return s.hourly, nil
}

func TestDailySnapshotRunnerUsesCalendarDayAcrossDST(t *testing.T) {
	previousLocation := utils.GlobalLocation
	t.Cleanup(func() { utils.GlobalLocation = previousLocation })
	if err := utils.SetLocation("America/New_York"); err != nil {
		t.Fatal(err)
	}
	store := &marketSnapshotStorage{hourly: []*storage.HourlyEquityRecord{{Timestamp: time.Date(2026, 3, 8, 23, 0, 0, 0, utils.GlobalLocation)}}}
	runner := &DailySnapshotRunner{storage: store, getRuntimes: func() []RuntimeSnapshotSource {
		return []RuntimeSnapshotSource{marketSnapshotRuntime{marketType: "spot"}}
	}}
	day := time.Date(2026, 3, 8, 0, 0, 0, 0, utils.GlobalLocation)
	runner.aggregateDaily(day)
	if got := store.queryEnd.Sub(store.queryStart); got != 23*time.Hour {
		t.Fatalf("DST spring-forward day query duration = %v, want 23h (%v to %v)", got, store.queryStart, store.queryEnd)
	}
	if len(store.daily) != 1 || !store.daily[0].SnapshotTime.Equal(store.hourly[0].Timestamp) {
		t.Fatalf("daily snapshot should include the final local-hour sample: %+v", store.daily)
	}
}

func (s *marketSnapshotStorage) SaveDailySnapshot(snapshot *storage.DailySnapshot) error {
	s.daily = append(s.daily, snapshot)
	return nil
}

func (s *marketSnapshotStorage) SaveAccountEquityRecord(record *storage.AccountEquityRecord) error {
	s.account = append(s.account, record)
	return nil
}

func (*marketSnapshotStorage) DeleteHourlyEquityRecordsBefore(time.Time) error { return nil }

func TestDailySnapshotRunnerKeepsMarketTypeThroughHourlyAndDailyAggregation(t *testing.T) {
	store := &marketSnapshotStorage{}
	runtime := marketSnapshotRuntime{marketType: "spot"}
	runner := &DailySnapshotRunner{
		storage:     store,
		getRuntimes: func() []RuntimeSnapshotSource { return []RuntimeSnapshotSource{runtime} },
	}
	ts := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	runner.recordHourlyForAll(ts)
	if len(store.hourly) != 1 || store.hourly[0].MarketType != "spot" {
		t.Fatalf("hourly sample lost market type: %+v", store.hourly)
	}
	runner.aggregateDaily(ts)
	if store.queried != "spot" || len(store.daily) != 1 || store.daily[0].MarketType != "spot" {
		t.Fatalf("daily aggregation crossed market boundary: query=%q snapshots=%+v", store.queried, store.daily)
	}
}

func TestDailySnapshotRunnerPersistsOnlyConsistentSnapshotPnLAsset(t *testing.T) {
	store := &marketSnapshotStorage{}
	runtime := assetSnapshotRuntime{marketSnapshotRuntime: marketSnapshotRuntime{marketType: "futures"}}
	runner := &DailySnapshotRunner{storage: store, getRuntimes: func() []RuntimeSnapshotSource { return []RuntimeSnapshotSource{runtime} }}
	ts := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	runner.recordHourlyForAll(ts)
	if len(store.hourly) != 1 || store.hourly[0].UnrealizedPnLAsset != "USDT" {
		t.Fatalf("hourly sample must normalize the explicit PnL denomination: %+v", store.hourly)
	}
	runner.aggregateDaily(ts)
	if len(store.daily) != 1 || store.daily[0].UnrealizedPnLAsset != "USDT" {
		t.Fatalf("daily aggregate must carry a consistent denomination: %+v", store.daily)
	}
	if got := consistentSnapshotPnLAsset([]*storage.HourlyEquityRecord{{UnrealizedPnLAsset: "USDT"}, {UnrealizedPnLAsset: "USDC"}}); got != "" {
		t.Fatalf("mixed hourly denominations must fail closed, got %q", got)
	}
}

func TestDailySnapshotRunnerSkipsUnverifiedRuntimeFinancialSnapshots(t *testing.T) {
	store := &marketSnapshotStorage{}
	runtime := unverifiedSnapshotRuntime{marketSnapshotRuntime: marketSnapshotRuntime{marketType: "futures"}}
	runner := &DailySnapshotRunner{storage: store, getRuntimes: func() []RuntimeSnapshotSource { return []RuntimeSnapshotSource{runtime} }}
	ts := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	runner.recordHourlyForAll(ts)
	runner.recordMidnightSnapshot(ts)
	if len(store.hourly) != 0 || len(store.daily) != 0 {
		t.Fatalf("unverified finance snapshot persisted: hourly=%+v daily=%+v", store.hourly, store.daily)
	}
}

func TestDailySnapshotRunnerPersistsExchangeSpotInventoryAndPrice(t *testing.T) {
	store := &marketSnapshotStorage{}
	runtime := inventorySnapshotRuntime{marketSnapshotRuntime: marketSnapshotRuntime{marketType: "spot"}, qty: 1.25}
	runner := &DailySnapshotRunner{storage: store, getRuntimes: func() []RuntimeSnapshotSource { return []RuntimeSnapshotSource{runtime} }}
	ts := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	runner.recordHourlyForAll(ts)
	if len(store.hourly) != 1 || store.hourly[0].MarketPrice != 100 || store.hourly[0].SpotPositionQty == nil || *store.hourly[0].SpotPositionQty != 1.25 {
		t.Fatalf("hourly snapshot must preserve actual account inventory and mark price: %+v", store.hourly)
	}
	runner.aggregateDaily(ts)
	if len(store.daily) != 1 || store.daily[0].SpotPositionQty == nil || *store.daily[0].SpotPositionQty != 1.25 || store.daily[0].ClosingPrice != 100 {
		t.Fatalf("daily snapshot must carry inventory boundary evidence: %+v", store.daily)
	}
}

func TestDailySnapshotRunnerSamplesAccountEquityOncePerExchangeMarketAccount(t *testing.T) {
	calls := 0
	store := &marketSnapshotStorage{}
	runtimes := []RuntimeSnapshotSource{
		accountMarketSnapshotRuntime{marketSnapshotRuntime: marketSnapshotRuntime{marketType: "spot"}, calls: &calls, scope: "scope-a"},
		accountMarketSnapshotRuntime{marketSnapshotRuntime: marketSnapshotRuntime{marketType: "futures"}, calls: &calls, scope: "scope-a"},
	}
	runner := &DailySnapshotRunner{storage: store, getRuntimes: func() []RuntimeSnapshotSource { return runtimes }}
	ts := time.Date(2026, 9, 27, 13, 0, 0, 0, time.UTC)
	runner.recordHourlyForAll(ts)
	if calls != 2 || len(store.account) != 2 {
		t.Fatalf("spot and futures wallets must be sampled independently: calls=%d records=%+v", calls, store.account)
	}
	if store.account[0].MarketType != "spot" || store.account[1].MarketType != "futures" {
		t.Fatalf("account equity records lost market identity: %+v", store.account)
	}
	if len(store.hourly) != 2 || store.hourly[0].AccountEquity != nil || store.hourly[1].AccountEquity != nil {
		t.Fatalf("account equity must not be duplicated across symbol rows: %+v", store.hourly)
	}
}

func TestDailySnapshotRunnerSeparatesCredentialsWithSameDisplayAccount(t *testing.T) {
	calls := 0
	store := &marketSnapshotStorage{}
	runtimes := []RuntimeSnapshotSource{
		accountMarketSnapshotRuntime{marketSnapshotRuntime: marketSnapshotRuntime{marketType: "futures"}, calls: &calls, scope: "credential-scope-a"},
		accountMarketSnapshotRuntime{marketSnapshotRuntime: marketSnapshotRuntime{marketType: "futures"}, calls: &calls, scope: "credential-scope-b"},
	}
	runner := &DailySnapshotRunner{storage: store, getRuntimes: func() []RuntimeSnapshotSource { return runtimes }}
	runner.recordHourlyForAll(time.Date(2026, 9, 27, 14, 0, 0, 0, time.UTC))
	if calls != 2 || len(store.account) != 2 || store.account[0].AccountScope == store.account[1].AccountScope {
		t.Fatalf("different credentials sharing an account label must be sampled separately: calls=%d records=%+v", calls, store.account)
	}
}
