package backtest

import (
	"compress/gzip"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestDataLoaderLoadKlinesFromCSVAndDir(t *testing.T) {
	tempDir := t.TempDir()
	symbolDir := filepath.Join(tempDir, "BTCUSDT", "1m")
	if err := os.MkdirAll(symbolDir, 0755); err != nil {
		t.Fatalf("failed to create symbol dir: %v", err)
	}

	csvPath := filepath.Join(symbolDir, "sample.csv")
	csvData := "open_time,open,high,low,close,volume,close_time,quote_volume,trades\n" +
		"2000,101,103,100,102,2.5,2999,0,12\n" +
		"1000,100,102,99,101,1.5,1999,0,10\n" +
		"bad,100,102,99,101,1.5,1999,0,10\n"
	if err := os.WriteFile(csvPath, []byte(csvData), 0644); err != nil {
		t.Fatalf("failed to write csv: %v", err)
	}

	loader := NewDataLoader(tempDir, "BTCUSDT")
	klines, err := loader.LoadKlinesFromCSV(csvPath)
	if err != nil {
		t.Fatalf("LoadKlinesFromCSV failed: %v", err)
	}
	if len(klines) != 2 {
		t.Fatalf("expected 2 valid klines, got %d", len(klines))
	}
	if klines[0].NumTrades != 12 {
		t.Fatalf("NumTrades = %d, want 12", klines[0].NumTrades)
	}

	fromDir, err := loader.LoadKlinesFromDir()
	if err != nil {
		t.Fatalf("LoadKlinesFromDir failed: %v", err)
	}
	if len(fromDir) != 2 || fromDir[0].OpenTime != 1000 || fromDir[1].OpenTime != 2000 {
		t.Fatalf("expected sorted klines from dir, got %#v", fromDir)
	}
}

func TestDataLoaderLoadGzipFundingRatesAndMissingDirs(t *testing.T) {
	tempDir := t.TempDir()
	loader := NewDataLoader(tempDir, "BTCUSDT")

	if rates, err := loader.LoadFundingRatesFromDir(); err != nil || len(rates) != 0 {
		t.Fatalf("missing funding dir should return empty slice, got rates=%#v err=%v", rates, err)
	}

	fundingDir := filepath.Join(tempDir, "funding_rate", "BTCUSDT")
	if err := os.MkdirAll(fundingDir, 0755); err != nil {
		t.Fatalf("failed to create funding dir: %v", err)
	}
	gzPath := filepath.Join(fundingDir, "rates.csv.gz")
	writeGzipFile(t, gzPath, "funding_time,funding_rate\n1000,0.0001\nbad,0.2\n2000,0.0002\n")

	rates, err := loader.LoadFundingRatesFromCSV(gzPath)
	if err != nil {
		t.Fatalf("LoadFundingRatesFromCSV failed: %v", err)
	}
	if len(rates) != 2 || rates[1].FundingRate != 0.0002 {
		t.Fatalf("unexpected rates: %#v", rates)
	}

	fromDir, err := loader.LoadFundingRatesFromDir()
	if err != nil {
		t.Fatalf("LoadFundingRatesFromDir failed: %v", err)
	}
	if len(fromDir) != 2 {
		t.Fatalf("expected 2 rates from dir, got %d", len(fromDir))
	}
}

func TestDataLoaderTransformsStatsValidationAndResample(t *testing.T) {
	klines := []KlineRow{
		{OpenTime: 0, Open: 100, High: 102, Low: 99, Close: 101, Volume: 1, CloseTime: 59999, NumTrades: 3},
		{OpenTime: 60000, Open: 101, High: 105, Low: 100, Close: 104, Volume: 2, CloseTime: 119999, NumTrades: 4},
		{OpenTime: 120000, Open: 104, High: 106, Low: 103, Close: 105, Volume: 3, CloseTime: 179999, NumTrades: 5},
	}

	if err := ValidateKlines(klines); err != nil {
		t.Fatalf("ValidateKlines failed: %v", err)
	}
	if err := ValidateKlines(nil); err == nil {
		t.Fatal("expected empty kline validation to fail")
	}
	invalid := append([]KlineRow{}, klines...)
	invalid[1].High = 90
	if err := ValidateKlines(invalid); err == nil {
		t.Fatal("expected invalid high/low to fail")
	}

	stats := GetDataStats(klines)
	if stats.TotalKlines != 3 || stats.TotalVolume != 6 || stats.AvgVolume != 2 {
		t.Fatalf("unexpected stats: %#v", stats)
	}
	if stats.PriceChange != 5 || stats.PriceChangePct != 5 {
		t.Fatalf("unexpected price change stats: %#v", stats)
	}
	if empty := GetDataStats(nil); empty.TotalKlines != 0 {
		t.Fatalf("empty stats = %#v", empty)
	}

	ticks := ConvertToTickKlines(klines)
	if len(ticks) != 3 || ticks[0].Timestamp != 0 || ticks[0].Close != 101 {
		t.Fatalf("unexpected tick klines: %#v", ticks)
	}
	if single := KlineToTickKline(klines[0]); single.Volume != 1 || single.High != 102 {
		t.Fatalf("unexpected single tick kline: %#v", single)
	}

	resampled := ResampleToInterval(klines, 2)
	if len(resampled) != 2 {
		t.Fatalf("expected 2 resampled klines, got %d", len(resampled))
	}
	if resampled[0].Open != 100 || resampled[0].High != 105 || resampled[0].Low != 99 || resampled[0].Close != 104 {
		t.Fatalf("unexpected first resampled kline: %#v", resampled[0])
	}
	if same := ResampleToInterval(klines, 0); len(same) != len(klines) {
		t.Fatalf("invalid interval should return original klines, got %d", len(same))
	}

	filtered := NewDataLoader("", "").FilterByTimeRange(
		klines,
		time.UnixMilli(60000),
		time.UnixMilli(180000),
	)
	if len(filtered) != 2 || filtered[0].OpenTime != 60000 {
		t.Fatalf("unexpected filtered klines: %#v", filtered)
	}
}

func TestParseKlineRowErrors(t *testing.T) {
	if _, err := parseKlineRow([]string{"bad", "1", "2", "3", "4", "5", "6"}); err == nil {
		t.Fatal("expected invalid open time to fail")
	}
	if _, err := parseKlineRow([]string{"1", "bad", "2", "3", "4", "5", "6"}); err == nil {
		t.Fatal("expected invalid open to fail")
	}
}

func TestAggTradeLoaderLoadConvertStatsAndResample(t *testing.T) {
	tempDir := t.TempDir()
	tradesDir := filepath.Join(tempDir, "aggtrades", "BTCUSDT")
	if err := os.MkdirAll(tradesDir, 0755); err != nil {
		t.Fatalf("failed to create trades dir: %v", err)
	}
	csvPath := filepath.Join(tradesDir, "BTCUSDT-aggTrades-2026-06-03.csv")
	csvData := "agg_trade_id,price,quantity,first_trade_id,last_trade_id,timestamp,is_buyer_maker\n" +
		"2,101,0.5,20,21,1780480001000,true\n" +
		"1,100,1.5,10,11,1780480000000,false\n" +
		"bad,100,1,1,1,1780480000000,false\n"
	if err := os.WriteFile(csvPath, []byte(csvData), 0644); err != nil {
		t.Fatalf("failed to write agg trades: %v", err)
	}

	loader := NewAggTradeLoader(tempDir, "btcusdt")
	trades, err := loader.LoadAggTradesFromCSV(csvPath)
	if err != nil {
		t.Fatalf("LoadAggTradesFromCSV failed: %v", err)
	}
	if len(trades) != 2 {
		t.Fatalf("expected 2 valid trades, got %d", len(trades))
	}

	fromDir, err := loader.LoadAggTradesFromDir(time.Date(2026, 6, 3, 0, 0, 0, 0, time.UTC), time.Date(2026, 6, 3, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("LoadAggTradesFromDir failed: %v", err)
	}
	if len(fromDir) != 2 || fromDir[0].AggTradeID != 1 {
		t.Fatalf("expected sorted trades from dir, got %#v", fromDir)
	}

	ticks := loader.ConvertToTickTrades(fromDir)
	if len(ticks) != 2 || ticks[0].Side != "buy" || ticks[1].Side != "sell" {
		t.Fatalf("unexpected tick trades: %#v", ticks)
	}

	stats := loader.GetStats(fromDir)
	if stats.TotalTrades != 2 || stats.TotalVolume != 2 || stats.BuyVolume != 1.5 || stats.SellVolume != 0.5 {
		t.Fatalf("unexpected stats: %#v", stats)
	}
	if stats.WeightedAvgPrice != 100.25 {
		t.Fatalf("WeightedAvgPrice = %v, want 100.25", stats.WeightedAvgPrice)
	}
	if empty := loader.GetStats(nil); empty.TotalTrades != 0 {
		t.Fatalf("empty stats = %#v", empty)
	}

	klines := loader.ResampleToKline(fromDir, time.Minute)
	if len(klines) != 1 || klines[0].Open != 100 || klines[0].Close != 101 || klines[0].Volume != 2 {
		t.Fatalf("unexpected resampled agg trade kline: %#v", klines)
	}
	if got := loader.ResampleToKline(nil, time.Minute); got != nil {
		t.Fatalf("empty resample = %#v, want nil", got)
	}
}

func TestParseAggTradeRowErrorsAndBoolFallbacks(t *testing.T) {
	trade, err := parseAggTradeRow([]string{"1", "100", "2", "10", "11", "1000", "1"})
	if err != nil {
		t.Fatalf("parseAggTradeRow bool fallback failed: %v", err)
	}
	if !trade.IsBuyerMaker {
		t.Fatal("expected numeric bool fallback to set IsBuyerMaker")
	}

	if _, err := parseAggTradeRow([]string{"bad", "100", "2", "10", "11", "1000", "false"}); err == nil {
		t.Fatal("expected invalid aggTradeId to fail")
	}
	if _, err := parseAggTradeRow([]string{"1", "100", "2", "10", "11", "1000", "maybe"}); err == nil {
		t.Fatal("expected invalid isBuyerMaker to fail")
	}
}

func writeGzipFile(t *testing.T, path string, content string) {
	t.Helper()
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("failed to create gzip file: %v", err)
	}
	defer file.Close()

	writer := gzip.NewWriter(file)
	if _, err := writer.Write([]byte(content)); err != nil {
		t.Fatalf("failed to write gzip content: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("failed to close gzip writer: %v", err)
	}
}
