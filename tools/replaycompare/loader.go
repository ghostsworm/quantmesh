package main

import (
	"archive/zip"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"quantmesh/backtest/replay"
	"quantmesh/exchange"
)

// Binance data.binance.vision CSV 列位置
const (
	klineColOpenTime = 0
	klineColOpen     = 1
	klineColHigh     = 2
	klineColLow      = 3
	klineColClose    = 4
	klineColVolume   = 5
	klineMinColumns  = 6

	fundingColCalcTime = 0
	fundingColRate     = 2
	fundingMinColumns  = 3

	// secondsTimestampLimit 小於此值的時間戳視為秒；大於 microsecondsLimit 視為微秒（Binance 2025 起現貨數據為微秒）
	secondsTimestampLimit      = int64(1e12)
	microsecondsTimestampLimit = int64(1e15)
	msPerSecond                = int64(1000)
	microsPerMs                = int64(1000)

	// MinuteMs 1 分鐘毫秒數
	MinuteMs = int64(60 * 1000)
	// DayMs 1 天毫秒數
	DayMs = 24 * 60 * MinuteMs
)

// normalizeTimestampMs 把秒/毫秒/微秒時間戳統一為毫秒
func normalizeTimestampMs(ts int64) int64 {
	switch {
	case ts < secondsTimestampLimit:
		return ts * msPerSecond
	case ts >= microsecondsTimestampLimit:
		return ts / microsPerMs
	default:
		return ts
	}
}

// readCSVRecords 讀取 .zip（取其中所有 .csv）或 .csv 文件的全部記錄
func readCSVRecords(path string) ([][]string, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".zip":
		zr, err := zip.OpenReader(path)
		if err != nil {
			return nil, fmt.Errorf("open zip %s: %w", path, err)
		}
		defer zr.Close()
		var out [][]string
		for _, f := range zr.File {
			if !strings.EqualFold(filepath.Ext(f.Name), ".csv") {
				continue
			}
			rc, err := f.Open()
			if err != nil {
				return nil, fmt.Errorf("open %s in zip %s: %w", f.Name, path, err)
			}
			recs, err := parseCSV(rc)
			rc.Close()
			if err != nil {
				return nil, fmt.Errorf("parse %s in zip %s: %w", f.Name, path, err)
			}
			out = append(out, recs...)
		}
		return out, nil
	case ".csv":
		fh, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("open csv %s: %w", path, err)
		}
		defer fh.Close()
		recs, err := parseCSV(fh)
		if err != nil {
			return nil, fmt.Errorf("parse csv %s: %w", path, err)
		}
		return recs, nil
	default:
		return nil, fmt.Errorf("unsupported data file extension: %s", path)
	}
}

func parseCSV(r io.Reader) ([][]string, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	cr.ReuseRecord = false
	return cr.ReadAll()
}

// isHeaderRow 首列不是數字即視為表頭（新版數據帶表頭，舊版不帶）
func isHeaderRow(rec []string) bool {
	if len(rec) == 0 {
		return true
	}
	_, err := strconv.ParseInt(strings.TrimSpace(rec[0]), 10, 64)
	return err != nil
}

// listDataFiles 列出目錄下的 .zip/.csv 文件；同名 .zip 與 .csv 並存時只取 .csv（已解壓）
func listDataFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read data dir %s: %w", dir, err)
	}
	byStem := map[string]string{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		ext := strings.ToLower(filepath.Ext(name))
		if ext != ".zip" && ext != ".csv" {
			continue
		}
		stem := strings.TrimSuffix(name, filepath.Ext(name))
		if prev, ok := byStem[stem]; ok && strings.EqualFold(filepath.Ext(prev), ".csv") {
			continue
		}
		byStem[stem] = filepath.Join(dir, name)
	}
	files := make([]string, 0, len(byStem))
	for _, p := range byStem {
		files = append(files, p)
	}
	sort.Strings(files)
	return files, nil
}

// LoadKlines 讀取目錄下所有 K 線文件，返回 [startMs, endMs) 內按時間升序、去重的 K 線。
// 單行解析失敗返回錯誤（帶文件與行號），不靜默跳過。
func LoadKlines(dir, symbol string, startMs, endMs int64) ([]*exchange.Candle, error) {
	files, err := listDataFiles(dir)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("no kline files in %s", dir)
	}
	byTs := map[int64]*exchange.Candle{}
	for _, f := range files {
		recs, err := readCSVRecords(f)
		if err != nil {
			return nil, err
		}
		for i, rec := range recs {
			if isHeaderRow(rec) {
				continue
			}
			c, err := parseKlineRecord(rec, symbol)
			if err != nil {
				return nil, fmt.Errorf("%s line %d: %w", f, i+1, err)
			}
			if c.Timestamp < startMs || c.Timestamp >= endMs {
				continue
			}
			byTs[c.Timestamp] = c
		}
	}
	out := make([]*exchange.Candle, 0, len(byTs))
	for _, c := range byTs {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Timestamp < out[j].Timestamp })
	return out, nil
}

func parseKlineRecord(rec []string, symbol string) (*exchange.Candle, error) {
	if len(rec) < klineMinColumns {
		return nil, fmt.Errorf("kline record has %d columns, want >= %d", len(rec), klineMinColumns)
	}
	ts, err := strconv.ParseInt(strings.TrimSpace(rec[klineColOpenTime]), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("parse open_time %q: %w", rec[klineColOpenTime], err)
	}
	vals := make([]float64, 0, 5)
	for _, col := range []int{klineColOpen, klineColHigh, klineColLow, klineColClose, klineColVolume} {
		v, err := strconv.ParseFloat(strings.TrimSpace(rec[col]), 64)
		if err != nil {
			return nil, fmt.Errorf("parse column %d %q: %w", col, rec[col], err)
		}
		vals = append(vals, v)
	}
	c := &exchange.Candle{
		Symbol: symbol, Timestamp: normalizeTimestampMs(ts),
		Open: vals[0], High: vals[1], Low: vals[2], Close: vals[3], Volume: vals[4], IsClosed: true,
	}
	if c.Open <= 0 || c.High <= 0 || c.Low <= 0 || c.Close <= 0 || c.High < c.Low {
		return nil, fmt.Errorf("invalid OHLC at ts=%d: %v/%v/%v/%v", c.Timestamp, c.Open, c.High, c.Low, c.Close)
	}
	return c, nil
}

// LoadFunding 讀取 fundingRate 文件（calc_time,funding_interval_hours,last_funding_rate），按時間升序
func LoadFunding(dir string) ([]replay.FundingPoint, error) {
	files, err := listDataFiles(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	byTs := map[int64]float64{}
	for _, f := range files {
		recs, err := readCSVRecords(f)
		if err != nil {
			return nil, err
		}
		for i, rec := range recs {
			if isHeaderRow(rec) {
				continue
			}
			if len(rec) < fundingMinColumns {
				return nil, fmt.Errorf("%s line %d: funding record has %d columns, want >= %d", f, i+1, len(rec), fundingMinColumns)
			}
			ts, err := strconv.ParseInt(strings.TrimSpace(rec[fundingColCalcTime]), 10, 64)
			if err != nil {
				return nil, fmt.Errorf("%s line %d: parse calc_time %q: %w", f, i+1, rec[fundingColCalcTime], err)
			}
			rate, err := strconv.ParseFloat(strings.TrimSpace(rec[fundingColRate]), 64)
			if err != nil {
				return nil, fmt.Errorf("%s line %d: parse funding rate %q: %w", f, i+1, rec[fundingColRate], err)
			}
			byTs[normalizeTimestampMs(ts)] = rate
		}
	}
	out := make([]replay.FundingPoint, 0, len(byTs))
	for ts, r := range byTs {
		out = append(out, replay.FundingPoint{Timestamp: ts, Rate: r})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Timestamp < out[j].Timestamp })
	return out, nil
}

// Coverage 數據覆蓋情況
type Coverage struct {
	Symbol         string   `json:"symbol"`
	FirstBar       string   `json:"first_bar"`
	LastBar        string   `json:"last_bar"`
	Bars           int      `json:"bars"`
	ExpectedBars   int      `json:"expected_bars"`
	MissingDays    []string `json:"missing_days"`    // 整天無數據
	IncompleteDays []string `json:"incomplete_days"` // 有數據但不足 1440 根
	FundingPoints  int      `json:"funding_points"`
	FundingLast    string   `json:"funding_last,omitempty"`
}

// ComputeCoverage 按 UTC 天統計 [startMs, endMs) 內的 1m K 線覆蓋
func ComputeCoverage(symbol string, candles []*exchange.Candle, startMs, endMs int64) Coverage {
	perDay := map[int64]int{}
	for _, c := range candles {
		perDay[c.Timestamp-c.Timestamp%DayMs]++
	}
	cov := Coverage{Symbol: symbol, Bars: len(candles), ExpectedBars: int((endMs - startMs) / MinuteMs)}
	if len(candles) > 0 {
		cov.FirstBar = fmtTime(candles[0].Timestamp)
		cov.LastBar = fmtTime(candles[len(candles)-1].Timestamp)
	}
	minutesPerDay := int(DayMs / MinuteMs)
	for d := startMs - startMs%DayMs; d < endMs; d += DayMs {
		n := perDay[d]
		day := time.UnixMilli(d).UTC().Format("2006-01-02")
		switch {
		case n == 0:
			cov.MissingDays = append(cov.MissingDays, day)
		case n < minutesPerDay:
			cov.IncompleteDays = append(cov.IncompleteDays, fmt.Sprintf("%s(%d)", day, n))
		}
	}
	return cov
}

func fmtTime(ms int64) string {
	return time.UnixMilli(ms).UTC().Format("2006-01-02 15:04")
}
