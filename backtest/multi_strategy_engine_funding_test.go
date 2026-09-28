package backtest

import (
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fundingPriceTimingStrategy struct {
	account          *BacktestAccount
	positionAtSecond float64
	entryAtSecond    float64
}

func (s *fundingPriceTimingStrategy) OnInit(account *BacktestAccount, _ interface{}) error {
	s.account = account
	return nil
}

func (s *fundingPriceTimingStrategy) OnKline(kline TickKline, _ int64) ([]TickOrder, error) {
	if kline.Timestamp == 1000 {
		return []TickOrder{{OrderID: "open-long", Side: "buy", Price: 99, Size: 1}}, nil
	}
	s.positionAtSecond = s.account.PositionSize
	s.entryAtSecond = s.account.PositionEntryPrice
	return nil, nil
}

func (*fundingPriceTimingStrategy) OnTrade(TickTrade) {}
func (*fundingPriceTimingStrategy) GetName() string   { return "funding-price-timing" }
func (*fundingPriceTimingStrategy) GetType() string   { return "test" }
func (*fundingPriceTimingStrategy) GetConfig() map[string]interface{} {
	return map[string]interface{}{"total_capital": 1000}
}

func writeFundingBacktestKlines(t *testing.T, dataDir string) {
	t.Helper()
	dir := filepath.Join(dataDir, "BTCUSDT", "1m")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	contents := "open_time,open,high,low,close,volume,close_time,quote_volume,trades\n" +
		"1000,100,101,99,100,1,59999,100,1\n" +
		"61000,100,102,99,101,1,119999,101,1\n"
	if err := os.WriteFile(filepath.Join(dir, "klines.csv"), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func fundingBacktestEngine(dataDir string) *MultiStrategyEngine {
	cfg := validEngineConfigForValidation()
	cfg.EnableFunding = true
	cfg.DataDir = dataDir
	return NewMultiStrategyEngine(cfg)
}

func TestFundingEnabledBacktestFailsWhenFundingEvidenceIsMissing(t *testing.T) {
	dataDir := t.TempDir()
	writeFundingBacktestKlines(t, dataDir)

	engine := fundingBacktestEngine(dataDir)
	err := engine.LoadData()
	if err == nil || !strings.Contains(err.Error(), "funding costs are enabled") {
		t.Fatalf("LoadData() error = %v, want missing funding evidence error", err)
	}
	if len(engine.FundingRates) != 0 {
		t.Fatalf("missing funding data unexpectedly left %d records loaded", len(engine.FundingRates))
	}
}

func TestProcessFundingRateRespectsPositionDirection(t *testing.T) {
	tests := []struct {
		name         string
		positionSize float64
		fundingRate  float64
		wantCost     float64
		wantBalance  float64
	}{
		{name: "long pays positive rate", positionSize: 2, fundingRate: 0.01, wantCost: 2, wantBalance: 998},
		{name: "short receives positive rate", positionSize: -2, fundingRate: 0.01, wantCost: -2, wantBalance: 1002},
		{name: "long receives negative rate", positionSize: 2, fundingRate: -0.01, wantCost: -2, wantBalance: 1002},
		{name: "short pays negative rate", positionSize: -2, fundingRate: -0.01, wantCost: 2, wantBalance: 998},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			engine := &MultiStrategyEngine{
				FundingRates: []FundingRateRow{{FundingTime: 1000, FundingRate: tt.fundingRate}},
			}
			runtime := &StrategyRuntime{
				account: &BacktestAccount{Balance: 1000, PositionSize: tt.positionSize, PositionEntryPrice: 150, lastPrice: 100},
				stats:   &StrategyStats{},
			}

			if err := engine.processFundingRate(runtime, 1000); err != nil {
				t.Fatalf("processFundingRate() error = %v", err)
			}

			if runtime.account.Balance != tt.wantBalance {
				t.Errorf("balance = %v, want %v", runtime.account.Balance, tt.wantBalance)
			}
			if runtime.totalFunding != tt.wantCost || engine.totalFunding != tt.wantCost || runtime.stats.FundingCost != tt.wantCost {
				t.Errorf("funding totals runtime=%v engine=%v stats=%v, want %v", runtime.totalFunding, engine.totalFunding, runtime.stats.FundingCost, tt.wantCost)
			}
			if runtime.fundingIdx != 1 {
				t.Errorf("fundingIdx = %d, want 1", runtime.fundingIdx)
			}
		})
	}
}

func TestProcessFundingRateFailsClosedWithoutPriorPrice(t *testing.T) {
	engine := &MultiStrategyEngine{FundingRates: []FundingRateRow{{FundingTime: 1000, FundingRate: 0.01}}}
	runtime := &StrategyRuntime{
		account: &BacktestAccount{Balance: 1000, PositionSize: 2, PositionEntryPrice: 100},
		stats:   &StrategyStats{},
	}
	if err := engine.processFundingRate(runtime, 1000); err == nil {
		t.Fatal("processFundingRate() accepted an open position without a prior settlement price")
	}
	if runtime.fundingIdx != 0 || runtime.account.Balance != 1000 {
		t.Fatalf("failed settlement mutated state: index=%d balance=%v", runtime.fundingIdx, runtime.account.Balance)
	}
}

func TestRunSettlesFundingUsingPreviousKlinePrice(t *testing.T) {
	cfg := validEngineConfigForValidation()
	cfg.EnableFunding = true
	engine := NewMultiStrategyEngine(cfg)
	engine.Klines = []TickKline{
		{Timestamp: 1000, Open: 100, High: 105, Low: 98, Close: 110, Volume: 100},
		{Timestamp: 61000, Open: 190, High: 205, Low: 185, Close: 200, Volume: 100},
	}
	engine.FundingRates = []FundingRateRow{{FundingTime: 0}, {FundingTime: 61000, FundingRate: 0.01}}
	strategy := &fundingPriceTimingStrategy{}
	if err := engine.AddStrategy(strategy); err != nil {
		t.Fatalf("AddStrategy() error = %v", err)
	}

	result, err := engine.Run()
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if strategy.positionAtSecond == 0 || strategy.entryAtSecond == 0 {
		t.Fatal("test strategy did not open its expected position")
	}
	want := strategy.positionAtSecond * 110 * 0.01
	if math.Abs(result.TotalFunding-want) > 1e-9 {
		t.Fatalf("TotalFunding = %v, want %v using prior Kline close (entry=%v, settlement Kline close=200)", result.TotalFunding, want, strategy.entryAtSecond)
	}
}

func TestRunSettlesFundingUsingRecordedMarkPrice(t *testing.T) {
	cfg := validEngineConfigForValidation()
	cfg.EnableFunding = true
	engine := NewMultiStrategyEngine(cfg)
	engine.Klines = []TickKline{{Timestamp: 1000, Open: 100, High: 105, Low: 98, Close: 110, Volume: 100}, {Timestamp: 61000, Open: 190, High: 205, Low: 185, Close: 200, Volume: 100}}
	engine.FundingRates = []FundingRateRow{{FundingTime: 0}, {FundingTime: 61000, FundingRate: 0.01, MarkPrice: 150, HasMarkPrice: true}}
	strategy := &fundingPriceTimingStrategy{}
	if err := engine.AddStrategy(strategy); err != nil {
		t.Fatal(err)
	}
	result, err := engine.Run()
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	want := strategy.positionAtSecond * 150 * 0.01
	if math.Abs(result.TotalFunding-want) > 1e-9 {
		t.Fatalf("TotalFunding = %v, want %v using recorded settlement mark price", result.TotalFunding, want)
	}
}

func TestRunCannotBypassFundingEvidenceValidation(t *testing.T) {
	tests := []struct {
		name  string
		rates []FundingRateRow
	}{
		{name: "missing", rates: nil},
		{name: "non-finite", rates: []FundingRateRow{{FundingTime: 1000, FundingRate: math.NaN()}}},
		{name: "duplicate", rates: []FundingRateRow{{FundingTime: 1000}, {FundingTime: 1000}}},
		{name: "invalid mark price", rates: []FundingRateRow{{FundingTime: 1000, MarkPrice: math.Inf(1), HasMarkPrice: true}}},
		{name: "out of order", rates: []FundingRateRow{{FundingTime: 2000}, {FundingTime: 1000}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := validEngineConfigForValidation()
			cfg.EnableFunding = true
			engine := NewMultiStrategyEngine(cfg)
			engine.FundingRates = tt.rates
			if _, err := engine.Run(); err == nil {
				t.Fatal("Run() accepted missing or invalid funding evidence")
			}
		})
	}
}

func TestRunRejectsFundingSeriesOutsideBacktestRange(t *testing.T) {
	cfg := validEngineConfigForValidation()
	cfg.EnableFunding = true
	engine := NewMultiStrategyEngine(cfg)
	engine.Klines = []TickKline{{Timestamp: 1000, Open: 100, High: 100, Low: 100, Close: 100}, {Timestamp: 61000, Open: 100, High: 100, Low: 100, Close: 100}}
	engine.FundingRates = []FundingRateRow{{FundingTime: 61000, FundingRate: 0.001}}
	if _, err := engine.Run(); err == nil {
		t.Fatal("Run() accepted funding observations without a left-side range boundary")
	}
}

func TestStrictFundingLoaderRejectsEmptyCorruptAndDuplicateEvidence(t *testing.T) {
	tests := []struct {
		name  string
		files map[string]string
	}{
		{name: "no csv files", files: map[string]string{}},
		{name: "header only", files: map[string]string{"rates.csv": "funding_time,funding_rate\n"}},
		{name: "malformed row", files: map[string]string{"rates.csv": "funding_time,funding_rate\n1000,0.001\nbad,0.002\n"}},
		{name: "non finite rate", files: map[string]string{"rates.csv": "funding_time,funding_rate\n1000,NaN\n"}},
		{name: "duplicate timestamp", files: map[string]string{
			"a.csv": "funding_time,funding_rate\n1000,0.001\n",
			"b.csv": "funding_time,funding_rate\n1000,-0.001\n",
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dataDir := t.TempDir()
			fundingDir := filepath.Join(dataDir, "funding_rate", "BTCUSDT")
			if err := os.MkdirAll(fundingDir, 0o755); err != nil {
				t.Fatal(err)
			}
			for name, contents := range tt.files {
				if err := os.WriteFile(filepath.Join(fundingDir, name), []byte(contents), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			loader := NewDataLoader(dataDir, "BTCUSDT")
			if rates, err := loader.LoadFundingRatesFromDirStrict(); err == nil {
				t.Fatalf("strict loader accepted invalid evidence: %#v", rates)
			}
		})
	}
}

func TestStrictFundingLoaderMergesAndSortsCompleteFiles(t *testing.T) {
	dataDir := t.TempDir()
	fundingDir := filepath.Join(dataDir, "funding_rate", "BTCUSDT")
	if err := os.MkdirAll(fundingDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, contents := range map[string]string{
		"later.csv":   "funding_time,funding_rate\n2000,-0.001\n",
		"earlier.csv": "funding_time,funding_rate\n1000,0.001\n",
	} {
		if err := os.WriteFile(filepath.Join(fundingDir, name), []byte(contents), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	loader := NewDataLoader(dataDir, "BTCUSDT")
	rates, err := loader.LoadFundingRatesFromDirStrict()
	if err != nil {
		t.Fatalf("LoadFundingRatesFromDirStrict() error = %v", err)
	}
	if len(rates) != 2 || rates[0].FundingTime != 1000 || rates[1].FundingTime != 2000 {
		t.Fatalf("strict funding observations = %#v, want ordered timestamps [1000 2000]", rates)
	}
}

func TestStrictFundingLoaderParsesIntervalAwareBinanceSchema(t *testing.T) {
	tests := []struct {
		name      string
		contents  string
		wantTime  int64
		wantRate  float64
		wantHours int64
		wantMark  float64
	}{
		{
			name:      "named dynamic interval columns",
			contents:  "calc_time,funding_interval_hours,last_funding_rate\n1000,1,0.0003\n",
			wantTime:  1000,
			wantRate:  0.0003,
			wantHours: 1,
		},
		{
			name:      "headerless dynamic interval columns",
			contents:  "1000,4,-0.0002\n",
			wantTime:  1000,
			wantRate:  -0.0002,
			wantHours: 4,
		},
		{
			name:     "named fundingTime export",
			contents: "symbol,fundingTime,fundingRate,markPrice\nBTCUSDT,1000,0.0001,101.5\n",
			wantTime: 1000,
			wantRate: 0.0001,
			wantMark: 101.5,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dataDir := t.TempDir()
			fundingDir := filepath.Join(dataDir, "funding_rate", "BTCUSDT")
			if err := os.MkdirAll(fundingDir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(fundingDir, "rates.csv"), []byte(tt.contents), 0o600); err != nil {
				t.Fatal(err)
			}
			rates, err := NewDataLoader(dataDir, "BTCUSDT").LoadFundingRatesFromDirStrict()
			if err != nil {
				t.Fatalf("LoadFundingRatesFromDirStrict() error = %v", err)
			}
			if len(rates) != 1 || rates[0].FundingTime != tt.wantTime || rates[0].FundingRate != tt.wantRate || rates[0].FundingIntervalHours != tt.wantHours || rates[0].MarkPrice != tt.wantMark || rates[0].HasMarkPrice != (tt.wantMark > 0) {
				t.Fatalf("funding rows = %#v, want time=%d rate=%v interval=%d mark=%v", rates, tt.wantTime, tt.wantRate, tt.wantHours, tt.wantMark)
			}
		})
	}
}

func TestStrictFundingLoaderMatchesReplayCompareFixture(t *testing.T) {
	loader := NewDataLoader(t.TempDir(), "TESTUSDT")
	fixturePath := filepath.Join("..", "tools", "replaycompare", "testdata", "funding", "TESTUSDT-fundingRate-2026-01.csv")
	rates, err := loader.loadFundingRatesFromCSV(fixturePath, true)
	if err != nil {
		t.Fatalf("strict CSV load of repository fixture error = %v", err)
	}
	if len(rates) != 2 {
		t.Fatalf("repository fixture produced %d funding rows, want 2", len(rates))
	}
	if rates[0].FundingTime != 1767254400000 || rates[0].FundingRate != 0.00005 || rates[0].FundingIntervalHours != 8 {
		t.Fatalf("first parsed fixture rate = %+v", rates[0])
	}
	if rates[1].FundingTime != 1767225600001 || rates[1].FundingRate != 0.0001 || rates[1].FundingIntervalHours != 8 {
		t.Fatalf("second parsed fixture rate = %+v", rates[1])
	}
}

func TestStrictFundingLoaderRejectsAmbiguousMultiColumnRows(t *testing.T) {
	dataDir := t.TempDir()
	fundingDir := filepath.Join(dataDir, "funding_rate", "BTCUSDT")
	if err := os.MkdirAll(fundingDir, 0o755); err != nil {
		t.Fatal(err)
	}
	contents := "1000,8,0.001,unexpected\n"
	if err := os.WriteFile(filepath.Join(fundingDir, "rates.csv"), []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	if rates, err := NewDataLoader(dataDir, "BTCUSDT").LoadFundingRatesFromDirStrict(); err == nil {
		t.Fatalf("strict loader accepted ambiguous extra columns: %#v", rates)
	}
}

func TestValidateFundingCoverageRequiresRangeBracketingAndNoDeclaredGaps(t *testing.T) {
	const hour int64 = 60 * 60 * 1000
	tests := []struct {
		name    string
		rates   []FundingRateRow
		start   int64
		end     int64
		wantErr bool
	}{
		{name: "bracketed", rates: []FundingRateRow{{FundingTime: 1000}, {FundingTime: 2 * hour}, {FundingTime: 4 * hour}}, start: 2 * hour, end: 3 * hour},
		{name: "missing left boundary", rates: []FundingRateRow{{FundingTime: 2 * hour}, {FundingTime: 4 * hour}}, start: hour, end: 3 * hour, wantErr: true},
		{name: "missing right boundary", rates: []FundingRateRow{{FundingTime: 1000}, {FundingTime: 2 * hour}}, start: hour, end: 3 * hour, wantErr: true},
		{name: "gap exceeds declared interval", rates: []FundingRateRow{{FundingTime: 1000, FundingIntervalHours: 1}, {FundingTime: hour, FundingIntervalHours: 1}, {FundingTime: 4 * hour, FundingIntervalHours: 1}}, start: 1000, end: 4 * hour, wantErr: true},
		{name: "unrepresentable interval", rates: []FundingRateRow{{FundingTime: 1000, FundingIntervalHours: int64(^uint64(0) >> 1)}, {FundingTime: 2 * hour}}, start: 1000, end: 2 * hour, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateFundingCoverage(tt.rates, tt.start, tt.end)
			if (err != nil) != tt.wantErr {
				t.Fatalf("validateFundingCoverage() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
