package backtest

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBinanceDownloaderURLsAndTimeRangeParsing(t *testing.T) {
	t.Parallel()

	d := NewBinanceDownloader(t.TempDir(), "btcusdt", "1m")
	if d.Symbol != "BTCUSDT" {
		t.Fatalf("symbol = %q, want uppercase", d.Symbol)
	}
	if got := d.getKlinesURL(2026, 6); !strings.Contains(got, "/klines/BTCUSDT/1m/BTCUSDT-1m-2026-06.zip") {
		t.Fatalf("klines URL = %q", got)
	}
	if got := d.getFundingRateURL(2026, 6); !strings.Contains(got, "/fundingRate/BTCUSDT/BTCUSDT-fundingRate-2026-06.zip") {
		t.Fatalf("funding URL = %q", got)
	}
	aggDate := time.Date(2026, 6, 3, 0, 0, 0, 0, time.UTC)
	if got := d.getAggTradesURL(aggDate); !strings.Contains(got, "/aggTrades/BTCUSDT/BTCUSDT-aggTrades-2026-06-03.zip") {
		t.Fatalf("aggTrades URL = %q", got)
	}

	start, end, err := ParseTimeRange("2026-05", "2026-06")
	if err != nil {
		t.Fatalf("ParseTimeRange month range returned error: %v", err)
	}
	if start.Format("2006-01-02") != "2026-05-01" || end.Format("2006-01-02 15:04:05") != "2026-06-30 23:59:59" {
		t.Fatalf("parsed range = %s -> %s", start, end)
	}

	start, end, err = ParseTimeRange("2026-05-02", "2026-05-03")
	if err != nil {
		t.Fatalf("ParseTimeRange date range returned error: %v", err)
	}
	if start.Format("2006-01-02") != "2026-05-02" || end.Format("2006-01-02") != "2026-05-03" {
		t.Fatalf("parsed date range = %s -> %s", start, end)
	}
	if _, _, err := ParseTimeRange("bad", "2026-05"); err == nil {
		t.Fatal("expected invalid start error")
	}
	if _, _, err := ParseTimeRange("2026-05", "bad"); err == nil {
		t.Fatal("expected invalid end error")
	}
}

func TestBinanceDownloaderDownloadExtractAndAvailability(t *testing.T) {
	t.Parallel()

	zipData := testZipBytes(t, "payload.csv", "time,price\n1,2\n")
	client := &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		status := http.StatusOK
		body := io.NopCloser(bytes.NewReader(zipData))
		if strings.Contains(req.URL.String(), "missing") {
			status = http.StatusNotFound
			body = io.NopCloser(strings.NewReader(""))
		}
		return &http.Response{
			StatusCode: status,
			Header:     make(http.Header),
			Body:       body,
			Request:    req,
		}, nil
	})}

	d := NewBinanceDownloader(t.TempDir(), "ETHUSDT", "5m")
	d.HTTPClient = client

	zipPath := filepath.Join(d.DataDir, "manual", "test.zip")
	if err := d.downloadFile("https://example.test/data.zip", zipPath); err != nil {
		t.Fatalf("downloadFile returned error: %v", err)
	}
	if err := d.downloadFile("https://example.test/data.zip", zipPath); err != nil {
		t.Fatalf("downloadFile should skip existing file: %v", err)
	}

	extractDir := filepath.Join(d.DataDir, "extract")
	if err := d.extractZip(zipPath, extractDir); err != nil {
		t.Fatalf("extractZip returned error: %v", err)
	}
	if data, err := os.ReadFile(filepath.Join(extractDir, "payload.csv")); err != nil || !strings.Contains(string(data), "time,price") {
		t.Fatalf("extracted data = %q, err = %v", data, err)
	}
	if err := d.downloadFile("https://example.test/missing.zip", filepath.Join(d.DataDir, "missing.zip")); err == nil {
		t.Fatal("expected 404 download error")
	}

	klines, funding, err := d.CheckAvailability()
	if err != nil {
		t.Fatalf("CheckAvailability returned error: %v", err)
	}
	if len(klines) == 0 || len(funding) == 0 {
		t.Fatalf("availability maps should be populated: %#v %#v", klines, funding)
	}
	klinesTime, fundingTime, err := d.GetLatestDataTime()
	if err != nil {
		t.Fatalf("GetLatestDataTime returned error: %v", err)
	}
	if klinesTime.IsZero() || fundingTime.IsZero() {
		t.Fatalf("latest times should be set: %s %s", klinesTime, fundingTime)
	}
	if !d.CheckAggTradesAvailability(time.Date(2026, 6, 3, 0, 0, 0, 0, time.UTC)) {
		t.Fatal("expected aggTrades availability")
	}
}

func TestBinanceDownloaderLocalDataInfo(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	d := NewBinanceDownloader(root, "BTCUSDT", "1h")
	writeBacktestFixture(t, filepath.Join(root, "klines", "BTCUSDT", "1h", "BTCUSDT-1h-2026-04.csv"), "a,b\n")
	writeBacktestFixture(t, filepath.Join(root, "klines", "BTCUSDT", "1h", "BTCUSDT-1h-2026-06.csv"), "a,b\n")
	writeBacktestFixture(t, filepath.Join(root, "funding_rate", "BTCUSDT", "BTCUSDT-fundingRate-2026-05.csv"), "a,b\n")
	writeBacktestFixture(t, filepath.Join(root, "funding_rate", "BTCUSDT", "BTCUSDT-fundingRate-2026-06.csv"), "a,b\n")
	writeBacktestFixture(t, filepath.Join(root, "aggtrades", "BTCUSDT", "BTCUSDT-aggTrades-2026-06-01.csv"), strings.Repeat("x", 200))
	writeBacktestFixture(t, filepath.Join(root, "aggtrades", "BTCUSDT", "BTCUSDT-aggTrades-2026-06-03.csv"), strings.Repeat("x", 200))

	info, err := d.GetDataInfo()
	if err != nil {
		t.Fatalf("GetDataInfo returned error: %v", err)
	}
	if len(info.KlinesFiles) != 2 || len(info.FundingFiles) != 2 {
		t.Fatalf("unexpected data files: %#v %#v", info.KlinesFiles, info.FundingFiles)
	}
	if info.EarliestKline.Format("2006-01") != "2026-04" || info.LatestKline.Format("2006-01") != "2026-06" {
		t.Fatalf("kline range = %s -> %s", info.EarliestKline, info.LatestKline)
	}
	if info.EarliestFunding.Format("2006-01") != "2026-05" || info.LatestFunding.Format("2006-01") != "2026-06" {
		t.Fatalf("funding range = %s -> %s", info.EarliestFunding, info.LatestFunding)
	}

	aggInfo, err := d.GetAggTradesInfo()
	if err != nil {
		t.Fatalf("GetAggTradesInfo returned error: %v", err)
	}
	if len(aggInfo.Files) != 2 || aggInfo.DateRange != "2026-06-01 ~ 2026-06-03" {
		t.Fatalf("agg info = %#v", aggInfo)
	}
	if aggInfo.TotalTrades == 0 {
		t.Fatal("expected estimated trade count")
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func testZipBytes(t *testing.T, name, content string) []byte {
	t.Helper()

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create(name)
	if err != nil {
		t.Fatalf("create zip entry: %v", err)
	}
	if _, err := w.Write([]byte(content)); err != nil {
		t.Fatalf("write zip entry: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("close zip: %v", err)
	}
	return buf.Bytes()
}

func writeBacktestFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("mkdir fixture dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("write fixture %s: %v", path, err)
	}
}
