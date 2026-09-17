package main

import (
	"archive/zip"
	"context"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"quantmesh/backtest"
	"quantmesh/exchange"
	"quantmesh/strategy/regime"
)

const (
	fixtureDay    = int64(1767225600000) // 2026-01-01 00:00 UTC
	fixtureSymbol = "TESTUSDT"
)

// zipFixture 把 testdata 中的 CSV 打包到臨時目錄的 zip 中
func zipFixture(t *testing.T, src, name string) string {
	t.Helper()
	dir := t.TempDir()
	zpath := filepath.Join(dir, strings.TrimSuffix(name, ".csv")+".zip")
	fh, err := os.Create(zpath)
	if err != nil {
		t.Fatalf("create zip: %v", err)
	}
	zw := zip.NewWriter(fh)
	w, err := zw.Create(name)
	if err != nil {
		t.Fatalf("zip entry: %v", err)
	}
	in, err := os.Open(src)
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer in.Close()
	if _, err := io.Copy(w, in); err != nil {
		t.Fatalf("copy fixture: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip writer: %v", err)
	}
	if err := fh.Close(); err != nil {
		t.Fatalf("close zip file: %v", err)
	}
	return dir
}

func TestLoadKlines(t *testing.T) {
	csvName := "TESTUSDT-1m-2026-01-01.csv"
	zipDir := zipFixture(t, filepath.Join("testdata", "klines", csvName), csvName)
	tests := []struct {
		name      string
		dir       string
		start     int64
		end       int64
		wantTs    []int64
		wantFirst [4]float64
	}{
		{name: "csv with header", dir: filepath.Join("testdata", "klines"), start: fixtureDay, end: fixtureDay + DayMs,
			wantTs: []int64{fixtureDay, fixtureDay + MinuteMs, fixtureDay + 2*MinuteMs}, wantFirst: [4]float64{100, 101, 99.5, 100.5}},
		{name: "zip", dir: zipDir, start: fixtureDay, end: fixtureDay + DayMs,
			wantTs: []int64{fixtureDay, fixtureDay + MinuteMs, fixtureDay + 2*MinuteMs}, wantFirst: [4]float64{100, 101, 99.5, 100.5}},
		{name: "no header, unsorted input is sorted", dir: filepath.Join("testdata", "klines_noheader"), start: fixtureDay, end: fixtureDay + DayMs,
			wantTs: []int64{fixtureDay, fixtureDay + 2*MinuteMs}, wantFirst: [4]float64{100, 101, 99.5, 100.5}},
		{name: "range filter end exclusive", dir: filepath.Join("testdata", "klines"), start: fixtureDay + MinuteMs, end: fixtureDay + 2*MinuteMs,
			wantTs: []int64{fixtureDay + MinuteMs}, wantFirst: [4]float64{100.5, 102, 100, 101.5}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := LoadKlines(tc.dir, fixtureSymbol, tc.start, tc.end)
			if err != nil {
				t.Fatalf("LoadKlines: %v", err)
			}
			if len(got) != len(tc.wantTs) {
				t.Fatalf("got %d candles, want %d", len(got), len(tc.wantTs))
			}
			for i, c := range got {
				if c.Timestamp != tc.wantTs[i] {
					t.Errorf("candle %d ts=%d want %d", i, c.Timestamp, tc.wantTs[i])
				}
				if c.Symbol != fixtureSymbol || !c.IsClosed {
					t.Errorf("candle %d symbol=%q closed=%v", i, c.Symbol, c.IsClosed)
				}
			}
			f := got[0]
			if [4]float64{f.Open, f.High, f.Low, f.Close} != tc.wantFirst {
				t.Errorf("first OHLC = %v/%v/%v/%v, want %v", f.Open, f.High, f.Low, f.Close, tc.wantFirst)
			}
		})
	}
}

func TestLoadKlinesErrors(t *testing.T) {
	if _, err := LoadKlines(filepath.Join("testdata", "klines_bad"), fixtureSymbol, 0, math.MaxInt64); err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("want parse error with line number, got %v", err)
	}
	if _, err := LoadKlines(filepath.Join("testdata", "does_not_exist"), fixtureSymbol, 0, math.MaxInt64); err == nil {
		t.Fatal("want error for missing dir")
	}
	if _, err := LoadKlines(t.TempDir(), fixtureSymbol, 0, math.MaxInt64); err == nil {
		t.Fatal("want error for empty dir")
	}
}

func TestLoadFunding(t *testing.T) {
	pts, err := LoadFunding(filepath.Join("testdata", "funding"))
	if err != nil {
		t.Fatalf("LoadFunding: %v", err)
	}
	if len(pts) != 2 || pts[0].Timestamp != fixtureDay+1 || pts[0].Rate != 0.0001 || pts[1].Rate != 0.00005 {
		t.Fatalf("unexpected funding points: %+v", pts)
	}
	none, err := LoadFunding(filepath.Join("testdata", "no_funding_here"))
	if err != nil || none != nil {
		t.Fatalf("missing funding dir should be (nil, nil), got %v, %v", none, err)
	}
}

func TestNormalizeTimestampMs(t *testing.T) {
	for _, tc := range []struct{ in, want int64 }{
		{1767225600, fixtureDay},
		{fixtureDay, fixtureDay},
		{fixtureDay * 1000, fixtureDay},
	} {
		if got := normalizeTimestampMs(tc.in); got != tc.want {
			t.Errorf("normalizeTimestampMs(%d)=%d want %d", tc.in, got, tc.want)
		}
	}
}

func TestComputeCoverage(t *testing.T) {
	candles := []*exchange.Candle{{Timestamp: fixtureDay, Open: 1, High: 1, Low: 1, Close: 1}}
	cov := ComputeCoverage(fixtureSymbol, candles, fixtureDay, fixtureDay+2*DayMs)
	if cov.ExpectedBars != 2880 || len(cov.MissingDays) != 1 || cov.MissingDays[0] != "2026-01-02" || len(cov.IncompleteDays) != 1 {
		t.Fatalf("unexpected coverage: %+v", cov)
	}
}

// minuteCandles 生成 n 根連續 1m K 線，第 i 根收盤價 = 100+i
func minuteCandles(start int64, n int) []*exchange.Candle {
	out := make([]*exchange.Candle, n)
	for i := range out {
		p := 100 + float64(i)
		out[i] = &exchange.Candle{Symbol: fixtureSymbol, Timestamp: start + int64(i)*MinuteMs, Open: p - 0.5, High: p + 1, Low: p - 1, Close: p, Volume: 1, IsClosed: true}
	}
	return out
}

func TestAggregateCandles(t *testing.T) {
	bars, err := AggregateCandles(minuteCandles(fixtureDay, 150), hourMs)
	if err != nil {
		t.Fatalf("AggregateCandles: %v", err)
	}
	if len(bars) != 3 {
		t.Fatalf("got %d hourly bars, want 3", len(bars))
	}
	h0 := bars[0]
	if h0.Timestamp != fixtureDay || h0.Open != 99.5 || h0.Close != 159 || h0.High != 160 || h0.Low != 99 || h0.Volume != 60 {
		t.Errorf("hour 0 = %+v", *h0)
	}
	if bars[2].Timestamp != fixtureDay+2*hourMs || bars[2].Volume != 30 {
		t.Errorf("partial last hour = %+v", *bars[2])
	}
	if _, err := AggregateCandles([]*exchange.Candle{{Timestamp: 2}, {Timestamp: 1}}, hourMs); err == nil {
		t.Error("want error for unsorted input")
	}
}

func TestClosedKlineSourceNoLookahead(t *testing.T) {
	hourly, err := AggregateCandles(minuteCandles(fixtureDay, 5*60), hourMs) // 5 根 1h：00..04
	if err != nil {
		t.Fatal(err)
	}
	now := time.UnixMilli(fixtureDay)
	src, err := NewClosedKlineSource(fixtureSymbol, "1h", hourly, func() time.Time { return now })
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	tests := []struct {
		name     string
		now      int64
		limit    int
		wantOpen []int64
	}{
		{name: "before first close", now: fixtureDay + hourMs - 1, limit: 10, wantOpen: nil},
		{name: "exactly at first close", now: fixtureDay + hourMs, limit: 10, wantOpen: []int64{fixtureDay}},
		{name: "mid third hour excludes forming bar", now: fixtureDay + 2*hourMs + 30*MinuteMs, limit: 10, wantOpen: []int64{fixtureDay, fixtureDay + hourMs}},
		{name: "limit keeps most recent", now: fixtureDay + 4*hourMs, limit: 2, wantOpen: []int64{fixtureDay + 2*hourMs, fixtureDay + 3*hourMs}},
		{name: "after all data", now: fixtureDay + 10*hourMs, limit: 100, wantOpen: []int64{fixtureDay, fixtureDay + hourMs, fixtureDay + 2*hourMs, fixtureDay + 3*hourMs, fixtureDay + 4*hourMs}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			now = time.UnixMilli(tc.now)
			got, err := src.GetHistoricalKlines(ctx, fixtureSymbol, "1h", tc.limit)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tc.wantOpen) {
				t.Fatalf("got %d bars, want %d", len(got), len(tc.wantOpen))
			}
			for i, b := range got {
				if b.Timestamp != tc.wantOpen[i] {
					t.Errorf("bar %d open=%d want %d", i, b.Timestamp, tc.wantOpen[i])
				}
				if b.Timestamp+hourMs > tc.now {
					t.Errorf("lookahead: bar open=%d closes after now=%d", b.Timestamp, tc.now)
				}
			}
		})
	}
	// 返回副本：修改結果不影響數據源
	now = time.UnixMilli(fixtureDay + 10*hourMs)
	got, _ := src.GetHistoricalKlines(ctx, fixtureSymbol, "1h", 1)
	got[0].Close = -1
	again, _ := src.GetHistoricalKlines(ctx, fixtureSymbol, "1h", 1)
	if again[0].Close == -1 {
		t.Error("source returned shared pointers")
	}
	if _, err := src.GetHistoricalKlines(ctx, "OTHER", "1h", 1); err == nil {
		t.Error("want error for wrong symbol")
	}
	if _, err := src.GetHistoricalKlines(ctx, fixtureSymbol, "4h", 1); err == nil {
		t.Error("want error for wrong interval")
	}
}

// TestClosedKlineSourceDetectorNoLookahead 檢測器在 t 時刻的快照只依賴 t 前已收盤的 K 線：
// 把 t 之後的數據改成極端值，快照不變。
func TestClosedKlineSourceDetectorNoLookahead(t *testing.T) {
	const hours = 200
	mk := func(spikeFrom int) []*exchange.Candle {
		bars := make([]*exchange.Candle, hours)
		for i := range bars {
			p := 100 + 5*math.Sin(float64(i)/7)
			if i >= spikeFrom {
				p *= 3
			}
			bars[i] = &exchange.Candle{Symbol: fixtureSymbol, Timestamp: fixtureDay + int64(i)*hourMs, Open: p, High: p * 1.01, Low: p * 0.99, Close: p, Volume: 1, IsClosed: true}
		}
		return bars
	}
	evalAt := 150
	snapAt := func(bars []*exchange.Candle) regime.Snapshot {
		now := time.UnixMilli(fixtureDay + int64(evalAt)*hourMs)
		clock := func() time.Time { return now }
		src, err := NewClosedKlineSource(fixtureSymbol, "1h", bars, clock)
		if err != nil {
			t.Fatal(err)
		}
		det, err := regime.NewDetector(fixtureSymbol, regime.RegimeConfig{Enabled: true, KlineInterval: "1h"}.WithDefaults(), src, regime.WithClock(clock))
		if err != nil {
			t.Fatal(err)
		}
		if err := det.Refresh(context.Background()); err != nil {
			t.Fatal(err)
		}
		return det.Snapshot()
	}
	clean := snapAt(mk(hours))
	spiked := snapAt(mk(evalAt)) // 第 evalAt 根（在 now 時刻尚未收盤）及之後被篡改
	if !clean.Ready {
		t.Fatalf("detector not ready after %d bars", evalAt)
	}
	if clean.ATR != spiked.ATR || clean.ADX != spiked.ADX || clean.Close != spiked.Close || !clean.BarOpenTime.Equal(spiked.BarOpenTime) {
		t.Fatalf("snapshot depends on future bars: clean=%+v spiked=%+v", clean, spiked)
	}
	if want := time.UnixMilli(fixtureDay + int64(evalAt-1)*hourMs); !clean.BarOpenTime.Equal(want) {
		t.Errorf("last processed bar = %v, want %v", clean.BarOpenTime, want)
	}
}

func TestDailySharpeLike(t *testing.T) {
	var curve []backtest.EquityPoint
	eq := 1000.0
	for d := 0; d < 5; d++ {
		for h := 0; h < 24; h += 12 {
			curve = append(curve, backtest.EquityPoint{Timestamp: fixtureDay + int64(d)*DayMs + int64(h)*hourMs, Equity: eq})
		}
		eq *= 1 + 0.001*float64(d%2+1)
	}
	got, days := DailySharpeLike(curve)
	if days != 5 || got <= 0 || math.IsNaN(got) {
		t.Fatalf("DailySharpeLike = %v over %d days", got, days)
	}
	if flat, _ := DailySharpeLike([]backtest.EquityPoint{{Timestamp: 0, Equity: 1}, {Timestamp: DayMs, Equity: 1}, {Timestamp: 2 * DayMs, Equity: 1}}); flat != 0 {
		t.Errorf("flat curve sharpe = %v, want 0", flat)
	}
}

func TestMaxNoFillGapHours(t *testing.T) {
	trades := []backtest.Trade{{Timestamp: 5 * hourMs}, {Timestamp: hourMs}}
	if got := MaxNoFillGapHours(trades, 0, 6*hourMs); got != 4 {
		t.Errorf("gap = %v, want 4", got)
	}
	if got := MaxNoFillGapHours(nil, 0, 3*hourMs); got != 3 {
		t.Errorf("no trades gap = %v, want 3", got)
	}
}

func TestSplitSegments(t *testing.T) {
	segs := SplitSegments(fixtureDay, fixtureDay+89*DayMs, 30, 3)
	if len(segs) != 4 || segs[0].Name != "full" || segs[3].EndMs != fixtureDay+89*DayMs || segs[2].StartMs != fixtureDay+30*DayMs {
		t.Fatalf("unexpected segments: %+v", segs)
	}
}
