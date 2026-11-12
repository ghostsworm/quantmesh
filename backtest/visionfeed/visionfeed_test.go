package visionfeed

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"quantmesh/exchange"
)

const fixtureSymbol = "TESTUSDT"

// fixtureDay is 2026-01-01 00:00 UTC.
const fixtureDay int64 = 1767225600000

func TestLoadReadsCSVAndZip(t *testing.T) {
	root := t.TempDir()
	writeDayCSV(t, root, fixtureSymbol, fixtureDay, false)
	writeFunding(t, root, fixtureSymbol, "2026-01", []string{"calc_time,funding_interval_hours,last_funding_rate", "1767225600000,8,0.0001"})
	zipDay := fixtureDay + DayMs
	writeDayZip(t, root, fixtureSymbol, zipDay)

	loaded, err := Load(context.Background(), LoadRequest{
		Root: root, Symbol: fixtureSymbol, Interval: "1m",
		FirstMs: fixtureDay + 2*MinuteMs, EndMs: zipDay + 2*MinuteMs,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(loaded.Candles) != 1440 {
		t.Fatalf("candles = %d, want 1440", len(loaded.Candles))
	}
	if loaded.Candles[0].Timestamp != fixtureDay+2*MinuteMs || loaded.Candles[0].Open != 102 {
		t.Fatalf("first candle = %+v", loaded.Candles[0])
	}
	if loaded.Provenance.Source != SourceBinanceVisionUM1m || len(loaded.Provenance.Files) != 2 {
		t.Fatalf("provenance = %+v", loaded.Provenance)
	}
	if !loaded.FundingUsed {
		t.Fatal("funding file was not used")
	}
	if len(loaded.Funding) != 1 || loaded.Funding[0].Rate != 0.0001 || loaded.Funding[0].MarkPrice != nil {
		t.Fatalf("funding = %+v", loaded.Funding)
	}
}

func TestLoadAcceptsHeaderlessAndCoarseTimestamps(t *testing.T) {
	cases := []struct {
		name   string
		scale  int64
		header bool
	}{
		{"milliseconds with header", 1, true},
		{"seconds without header", 1000, false},
		{"microseconds", 1, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeDayCSV(t, root, fixtureSymbol, fixtureDay, !tc.header)
			if tc.scale != 1 {
				rewriteTimestamps(t, dayPath(root, fixtureSymbol, fixtureDay, "csv"), tc.scale)
			}
			loaded, err := Load(context.Background(), LoadRequest{
				Root: root, Symbol: fixtureSymbol, FirstMs: fixtureDay, EndMs: fixtureDay + 3*MinuteMs,
			})
			if err != nil {
				t.Fatal(err)
			}
			if len(loaded.Candles) != 3 || loaded.Candles[2].Timestamp != fixtureDay+2*MinuteMs {
				t.Fatalf("candles = %+v", loaded.Candles)
			}
		})
	}
}

func TestLoadRejectsBadChecksums(t *testing.T) {
	cases := []struct {
		name string
		edit func(string)
	}{
		{"missing sidecar", func(path string) { os.Remove(path + ".CHECKSUM") }},
		{"malformed line", func(path string) { os.WriteFile(path+".CHECKSUM", []byte("not a checksum\n"), 0o644) }},
		{"hash mismatch", func(path string) {
			os.WriteFile(path+".CHECKSUM", []byte(strings.Repeat("ab", 32)+"  "+filepath.Base(path)+"\n"), 0o644)
		}},
		{"basename not listed", func(path string) {
			body, _ := os.ReadFile(path)
			sum := sha256.Sum256(body)
			os.WriteFile(path+".CHECKSUM", []byte(hex.EncodeToString(sum[:])+"  other.csv\n"), 0o644)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			writeDayCSV(t, root, fixtureSymbol, fixtureDay, false)
			tc.edit(dayPath(root, fixtureSymbol, fixtureDay, "csv"))
			_, err := Load(context.Background(), LoadRequest{Root: root, Symbol: fixtureSymbol, FirstMs: fixtureDay, EndMs: fixtureDay + MinuteMs})
			if err == nil || !strings.Contains(err.Error(), "visionfeed: checksum") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestLoadRejectsDuplicateDayFiles(t *testing.T) {
	root := t.TempDir()
	writeDayCSV(t, root, fixtureSymbol, fixtureDay, false)
	writeDayZip(t, root, fixtureSymbol, fixtureDay)
	_, err := Load(context.Background(), LoadRequest{Root: root, Symbol: fixtureSymbol, FirstMs: fixtureDay, EndMs: fixtureDay + MinuteMs})
	if err == nil || !strings.Contains(err.Error(), "visionfeed: duplicate") {
		t.Fatalf("error = %v", err)
	}
}

func TestLoadRejectsCoverageGaps(t *testing.T) {
	cases := []struct {
		name string
		make func(string)
	}{
		{"missing day", func(root string) {}},
		{"short day", func(root string) { writeShortDay(t, root, fixtureSymbol, fixtureDay, 10) }},
		{"duplicate timestamp", func(root string) {
			writeDayCSV(t, root, fixtureSymbol, fixtureDay, false)
			appendDuplicate(t, dayPath(root, fixtureSymbol, fixtureDay, "csv"))
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			tc.make(root)
			_, err := Load(context.Background(), LoadRequest{Root: root, Symbol: fixtureSymbol, FirstMs: fixtureDay, EndMs: fixtureDay + DayMs})
			if err == nil || !strings.Contains(err.Error(), "visionfeed: coverage") {
				t.Fatalf("error = %v", err)
			}
		})
	}
}

func TestLoadReportsParseLocation(t *testing.T) {
	root := t.TempDir()
	writeDayCSV(t, root, fixtureSymbol, fixtureDay, false)
	path := dayPath(root, fixtureSymbol, fixtureDay, "csv")
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	broken := strings.Replace(string(body), ",100,101,99,100,10\n", ",100,101,99,nope,10\n", 1)
	if err := os.WriteFile(path, []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}
	writeChecksum(t, path)
	_, err = Load(context.Background(), LoadRequest{Root: root, Symbol: fixtureSymbol, FirstMs: fixtureDay, EndMs: fixtureDay + MinuteMs})
	if err == nil || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "line 2") {
		t.Fatalf("error = %v", err)
	}
}

func TestLoadFundingIsOptionalAndOrdered(t *testing.T) {
	root := t.TempDir()
	writeDayCSV(t, root, fixtureSymbol, fixtureDay, false)
	loaded, err := Load(context.Background(), LoadRequest{Root: root, Symbol: fixtureSymbol, FirstMs: fixtureDay, EndMs: fixtureDay + MinuteMs})
	if err != nil {
		t.Fatal(err)
	}
	if loaded.FundingUsed || loaded.Funding != nil {
		t.Fatalf("missing funding dir reported as used: %+v", loaded.Funding)
	}

	empty := filepath.Join(root, fixtureSymbol, "fundingRate")
	if err := os.MkdirAll(empty, 0o755); err != nil {
		t.Fatal(err)
	}
	loaded, err = Load(context.Background(), LoadRequest{Root: root, Symbol: fixtureSymbol, FirstMs: fixtureDay, EndMs: fixtureDay + MinuteMs})
	if err != nil || loaded.FundingUsed {
		t.Fatalf("empty funding dir: used=%v err=%v", loaded.FundingUsed, err)
	}
}

func TestLoadRejectsAFundingMonthGap(t *testing.T) {
	root := t.TempDir()
	for day := int64(0); day < 32; day++ {
		writeDayCSV(t, root, fixtureSymbol, fixtureDay+day*DayMs, false)
	}
	writeFunding(t, root, fixtureSymbol, "2026-01", []string{
		"calc_time,funding_interval_hours,last_funding_rate",
		"1767225600000,8,0.0001",
	})
	_, err := Load(context.Background(), LoadRequest{
		Root: root, Symbol: fixtureSymbol, FirstMs: fixtureDay, EndMs: fixtureDay + 32*DayMs,
	})
	if err == nil || !strings.Contains(err.Error(), "missing funding file for 2026-02") {
		t.Fatalf("error = %v", err)
	}
}

func TestLoadIgnoresFundingOutsideTheWindow(t *testing.T) {
	root := t.TempDir()
	writeDayCSV(t, root, fixtureSymbol, fixtureDay, false)
	writeFunding(t, root, fixtureSymbol, "2025-12", []string{
		"calc_time,funding_interval_hours,last_funding_rate",
		"1764547200000,8,0.0009",
	})
	loaded, err := Load(context.Background(), LoadRequest{
		Root: root, Symbol: fixtureSymbol, FirstMs: fixtureDay, EndMs: fixtureDay + MinuteMs,
	})
	if err != nil {
		t.Fatal(err)
	}
	if loaded.FundingUsed || loaded.Funding != nil {
		t.Fatalf("funding outside the window was used: %+v", loaded.Funding)
	}
}

func TestLoadRejectsFundingOutsideItsNamedMonth(t *testing.T) {
	root := t.TempDir()
	writeDayCSV(t, root, fixtureSymbol, fixtureDay, false)
	writeFunding(t, root, fixtureSymbol, "2026-01", []string{
		"calc_time,funding_interval_hours,last_funding_rate",
		"1769904000000,8,0.0001",
	})
	_, err := Load(context.Background(), LoadRequest{
		Root: root, Symbol: fixtureSymbol, FirstMs: fixtureDay, EndMs: fixtureDay + MinuteMs,
	})
	if err == nil || !strings.Contains(err.Error(), "outside 2026-01") {
		t.Fatalf("error = %v", err)
	}
}

func TestLoadRejectsAZipWhoseCSVNameDoesNotMatch(t *testing.T) {
	root := t.TempDir()
	writeDayCSV(t, root, fixtureSymbol, fixtureDay, false)
	zipDay := fixtureDay + DayMs
	for _, entryName := range []string{
		"../" + fixtureSymbol + "-1m-" + dayLabel(zipDay) + ".csv",
		"/tmp/" + fixtureSymbol + "-1m-" + dayLabel(zipDay) + ".csv",
		fixtureSymbol + "-1m.csv",
	} {
		writeDayZipEntry(t, root, fixtureSymbol, zipDay, entryName)
		_, err := Load(context.Background(), LoadRequest{
			Root: root, Symbol: fixtureSymbol, FirstMs: fixtureDay, EndMs: zipDay + MinuteMs,
		})
		if err == nil || !strings.Contains(err.Error(), "zip entry") {
			t.Fatalf("entry %s error = %v", entryName, err)
		}
	}
}

func TestLoadRejectsAZipWithTwoCSVs(t *testing.T) {
	root := t.TempDir()
	writeDayCSV(t, root, fixtureSymbol, fixtureDay, false)
	zipDay := fixtureDay + DayMs
	name := fixtureSymbol + "-1m-" + dayLabel(zipDay) + ".csv"
	path := dayPath(root, fixtureSymbol, zipDay, "zip")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	for _, entryName := range []string{name, "second.csv"} {
		entry, err := writer.Create(entryName)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte("open_time,open,high,low,close,volume\n")); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	writeChecksum(t, path)
	_, err = Load(context.Background(), LoadRequest{
		Root: root, Symbol: fixtureSymbol, FirstMs: fixtureDay, EndMs: zipDay + MinuteMs,
	})
	if err == nil || !strings.Contains(err.Error(), "more than one csv") {
		t.Fatalf("error = %v", err)
	}
}

func TestLoadRejectsAZipEntryThatClaimsToBeTooLarge(t *testing.T) {
	root := t.TempDir()
	writeDayCSV(t, root, fixtureSymbol, fixtureDay, false)
	zipDay := fixtureDay + DayMs
	name := fixtureSymbol + "-1m-" + dayLabel(zipDay) + ".csv"
	path := dayPath(root, fixtureSymbol, zipDay, "zip")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	payload := []byte("open_time,open,high,low,close,volume\n")
	header := &zip.FileHeader{Name: name, Method: zip.Store}
	header.SetMode(0o644)
	header.CRC32 = crc32.ChecksumIEEE(payload)
	header.CompressedSize64 = uint64(len(payload))
	header.UncompressedSize64 = uint64(maxKlineCSVBytes) + 1
	entry, err := writer.CreateRaw(header)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	writeChecksum(t, path)
	_, err = Load(context.Background(), LoadRequest{
		Root: root, Symbol: fixtureSymbol, FirstMs: fixtureDay, EndMs: zipDay + MinuteMs,
	})
	if err == nil || !strings.Contains(err.Error(), "claims") {
		t.Fatalf("error = %v", err)
	}
}

func TestLoadRejectsAPlainCSVOverTheDayLimit(t *testing.T) {
	root := t.TempDir()
	path := dayPath(root, fixtureSymbol, fixtureDay, "csv")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(maxKlineCSVBytes + 1); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	writeChecksum(t, path)
	_, err = Load(context.Background(), LoadRequest{
		Root: root, Symbol: fixtureSymbol, FirstMs: fixtureDay, EndMs: fixtureDay + MinuteMs,
	})
	if err == nil || !strings.Contains(err.Error(), "limit") {
		t.Fatalf("error = %v", err)
	}
}

func TestLoadRejectsDuplicateFundingFiles(t *testing.T) {
	root := t.TempDir()
	writeDayCSV(t, root, fixtureSymbol, fixtureDay, false)
	lines := []string{"calc_time,funding_interval_hours,last_funding_rate", "1767225600000,8,0.0001"}
	writeFunding(t, root, fixtureSymbol, "2026-01", lines)
	dir := filepath.Join(root, fixtureSymbol, "fundingRate")
	zipPath := filepath.Join(dir, fixtureSymbol+"-fundingRate-2026-01.zip")
	file, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	entry, err := writer.Create(fixtureSymbol + "-fundingRate-2026-01.csv")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte(strings.Join(lines, "\n") + "\n")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	writeChecksum(t, zipPath)
	_, err = Load(context.Background(), LoadRequest{
		Root: root, Symbol: fixtureSymbol, FirstMs: fixtureDay, EndMs: fixtureDay + MinuteMs,
	})
	if err == nil || !strings.Contains(err.Error(), "visionfeed: duplicate") {
		t.Fatalf("error = %v", err)
	}
}

func TestLoadReadsFundingAcrossCompleteMonths(t *testing.T) {
	root := t.TempDir()
	for day := int64(0); day < 32; day++ {
		writeDayCSV(t, root, fixtureSymbol, fixtureDay+day*DayMs, false)
	}
	writeFunding(t, root, fixtureSymbol, "2026-01", []string{
		"calc_time,funding_interval_hours,last_funding_rate",
		"1769903999999,8,0.0001",
	})
	writeFunding(t, root, fixtureSymbol, "2026-02", []string{
		"calc_time,funding_interval_hours,last_funding_rate",
		"1769904000000,8,0.0002",
	})
	loaded, err := Load(context.Background(), LoadRequest{
		Root: root, Symbol: fixtureSymbol, FirstMs: fixtureDay, EndMs: fixtureDay + 32*DayMs,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !loaded.FundingUsed || len(loaded.Funding) != 2 || len(loaded.Provenance.FundingFiles) != 2 {
		t.Fatalf("funding = %+v files = %+v", loaded.Funding, loaded.Provenance.FundingFiles)
	}
	if loaded.Funding[0].Rate != 0.0001 || loaded.Funding[1].Rate != 0.0002 {
		t.Fatalf("rates = %+v", loaded.Funding)
	}
}
func TestLoadRejectsDuplicateFundingTime(t *testing.T) {
	root := t.TempDir()
	writeDayCSV(t, root, fixtureSymbol, fixtureDay, false)
	writeFunding(t, root, fixtureSymbol, "2026-01", []string{
		"calc_time,funding_interval_hours,last_funding_rate",
		"1767225600000,8,0.0001",
		"1767225600000,8,0.0002",
	})
	_, err := Load(context.Background(), LoadRequest{Root: root, Symbol: fixtureSymbol, FirstMs: fixtureDay, EndMs: fixtureDay + MinuteMs})
	if err == nil || !strings.Contains(err.Error(), "duplicate calc_time") {
		t.Fatalf("error = %v", err)
	}
}

func TestAggregateExactBuckets(t *testing.T) {
	minutes := make([]*exchange.Candle, 24*60)
	for i := range minutes {
		price := 100 + float64(i%90)
		minutes[i] = &exchange.Candle{Symbol: fixtureSymbol, Timestamp: fixtureDay + int64(i)*MinuteMs, Open: price, High: price + 2, Low: price - 1, Close: price + 1, Volume: 1}
	}
	for _, interval := range []int64{60 * MinuteMs, 4 * 60 * MinuteMs, DayMs} {
		out, err := AggregateExact(minutes, interval, fixtureDay, fixtureDay+DayMs)
		if err != nil {
			t.Fatal(err)
		}
		if len(out) != int(DayMs/interval) {
			t.Fatalf("interval %d produced %d candles", interval, len(out))
		}
		first := out[0]
		wantClose := 100 + float64((interval/MinuteMs-1)%90) + 1
		if first.Open != 100 || first.Close != wantClose || first.Volume != float64(interval/MinuteMs) {
			t.Fatalf("interval %d first bucket = %+v", interval, first)
		}
		if out[len(out)-1].Timestamp != fixtureDay+DayMs-interval {
			t.Fatalf("last bucket timestamp = %d", out[len(out)-1].Timestamp)
		}
	}
	minutes[30] = minutes[29]
	if _, err := AggregateExact(minutes, 60*MinuteMs, fixtureDay, fixtureDay+DayMs); err == nil || !strings.Contains(err.Error(), "visionfeed: aggregate") {
		t.Fatalf("duplicate sub-candle error = %v", err)
	}
}

func TestDayLabelRoundTrip(t *testing.T) {
	for _, label := range []string{"2024-02-29", "2026-01-01", "2026-12-31", "1972-03-01"} {
		ms, err := utcDayMillis(label)
		if err != nil {
			t.Fatal(err)
		}
		if got := dayLabel(ms); got != label {
			t.Fatalf("dayLabel(%d) = %s, want %s", ms, got, label)
		}
		if got := time.UnixMilli(ms).UTC().Format("2006-01-02"); got != label {
			t.Fatalf("unix date of %d = %s, want %s", ms, got, label)
		}
	}
}

func writeDayCSV(t *testing.T, root, symbol string, day int64, headerless bool) {
	t.Helper()
	lines := make([]string, 0, minutesPerDay+1)
	if !headerless {
		lines = append(lines, "open_time,open,high,low,close,volume")
	}
	for i := 0; i < minutesPerDay; i++ {
		price := 100 + i
		lines = append(lines, line(day+int64(i)*MinuteMs, price))
	}
	path := dayPath(root, symbol, day, "csv")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeChecksum(t, path)
}

func writeDayZip(t *testing.T, root, symbol string, day int64) {
	t.Helper()
	var body strings.Builder
	body.WriteString("open_time,open,high,low,close,volume\n")
	for i := 0; i < minutesPerDay; i++ {
		body.WriteString(line(day+int64(i)*MinuteMs, 200+i) + "\n")
	}
	path := dayPath(root, symbol, day, "zip")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	entry, err := writer.Create(symbol + "-1m-" + dayLabel(day) + ".csv")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte(body.String())); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	writeChecksum(t, path)
}

func writeDayZipEntry(t *testing.T, root, symbol string, day int64, entryName string) {
	t.Helper()
	path := dayPath(root, symbol, day, "zip")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	writer := zip.NewWriter(file)
	entry, err := writer.Create(entryName)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := entry.Write([]byte("open_time,open,high,low,close,volume\n")); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	writeChecksum(t, path)
}

func writeShortDay(t *testing.T, root, symbol string, day int64, count int) {
	t.Helper()
	lines := []string{"open_time,open,high,low,close,volume"}
	for i := 0; i < count; i++ {
		lines = append(lines, line(day+int64(i)*MinuteMs, 100+i))
	}
	path := dayPath(root, symbol, day, "csv")
	os.MkdirAll(filepath.Dir(path), 0o755)
	os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
	writeChecksum(t, path)
}

func appendDuplicate(t *testing.T, path string) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(line(fixtureDay, 100) + "\n"); err != nil {
		t.Fatal(err)
	}
	file.Close()
	writeChecksum(t, path)
}

func rewriteTimestamps(t *testing.T, path string, divisor int64) {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, row := range strings.Split(strings.TrimRight(string(body), "\n"), "\n") {
		fields := strings.Split(row, ",")
		if len(fields) == 6 && fields[0] != "open_time" {
			stamp, err := strconv.ParseInt(fields[0], 10, 64)
			if err != nil {
				t.Fatal(err)
			}
			if divisor == 1 {
				fields[0] = strconv.FormatInt(stamp*1000, 10)
			} else {
				fields[0] = strconv.FormatInt(stamp/divisor, 10)
			}
		}
		out = append(out, strings.Join(fields, ","))
	}
	if err := os.WriteFile(path, []byte(strings.Join(out, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeChecksum(t, path)
}

func writeFunding(t *testing.T, root, symbol, month string, lines []string) {
	t.Helper()
	dir := filepath.Join(root, symbol, "fundingRate")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, symbol+"-fundingRate-"+month+".csv")
	if _, err := os.Stat(path); err == nil {
		return
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeChecksum(t, path)
}

func writeChecksum(t *testing.T, path string) {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(body)
	line := hex.EncodeToString(sum[:]) + "  " + filepath.Base(path) + "\n"
	if err := os.WriteFile(path+".CHECKSUM", []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}
}

func dayPath(root, symbol string, day int64, ext string) string {
	return filepath.Join(root, symbol, "1m", symbol+"-1m-"+dayLabel(day)+"."+ext)
}

func line(ts int64, price int) string {
	return fmt.Sprintf("%d,%d,%d,%d,%d,10", ts, price, price+1, price-1, price)
}
