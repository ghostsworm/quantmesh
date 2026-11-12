// Package visionfeed reads checksum-verified data.binance.vision USD-M 1m
// klines and funding rates from a local directory. It never downloads, never
// skips a file that fails verification, and never fabricates a missing candle.
package visionfeed

import (
	"archive/zip"
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"quantmesh/backtest/replay"
	"quantmesh/exchange"
)

const (
	// SourceBinanceVisionUM1m names the only dataset this package reads.
	SourceBinanceVisionUM1m = "binance_vision_um_1m"
	// MinuteMs is one minute in milliseconds.
	MinuteMs int64 = 60 * 1000
	// DayMs is one UTC day in milliseconds.
	DayMs int64 = 24 * 60 * MinuteMs
	// DefaultRoot is the gitignored directory holding verified vision files.
	DefaultRoot = "data/kline/binance_vision"

	minutesPerDay = 1440

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

	secondsTimestampLimit      = int64(1e12)
	microsecondsTimestampLimit = int64(1e15)
)

var (
	dayFilePattern     = regexp.MustCompile(`^([A-Z0-9]{2,32})-1m-(\d{4}-\d{2}-\d{2})\.(csv|zip)$`)
	fundingFilePattern = regexp.MustCompile(`^([A-Z0-9]{2,32})-fundingRate-(\d{4}-\d{2})\.(csv|zip)$`)
	checksumLine       = regexp.MustCompile(`^[0-9a-f]{64}  \S+$`)

	monthLengths = [12]int{31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}
)

// FileDigest is the SHA-256 of one verified data file's bytes.
type FileDigest struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
}

// Provenance records exactly which verified files a load read.
type Provenance struct {
	Source       string       `json:"source"`
	Files        []FileDigest `json:"files"`
	FundingFiles []FileDigest `json:"funding_files"`
}

// LoadRequest selects one symbol's verified 1m window.
type LoadRequest struct {
	Root     string
	Symbol   string
	Interval string
	FirstMs  int64
	EndMs    int64
}

// LoadResult is the verified minute series plus settlement funding, if present.
// Funding points never carry a mark price.
type LoadResult struct {
	Candles     []*exchange.Candle
	Funding     []replay.FundingPoint
	FundingUsed bool
	Provenance  Provenance
}

// Load reads <root>/<symbol>/1m and, when present, <root>/<symbol>/fundingRate.
// A missing day, a bad checksum, or a gap fails the whole load.
func Load(ctx context.Context, req LoadRequest) (LoadResult, error) {
	if err := ctx.Err(); err != nil {
		return LoadResult{}, err
	}
	if req.FirstMs < 0 || req.EndMs <= req.FirstMs || (req.EndMs-req.FirstMs)%MinuteMs != 0 {
		return LoadResult{}, fmt.Errorf("visionfeed: coverage: window [%d,%d) is not a positive whole-minute span", req.FirstMs, req.EndMs)
	}
	klineDir := filepath.Join(req.Root, req.Symbol, "1m")
	candles, files, err := loadMinuteKlines(ctx, klineDir, req.Symbol, req.FirstMs, req.EndMs)
	if err != nil {
		return LoadResult{}, err
	}
	fundingDir := filepath.Join(req.Root, req.Symbol, "fundingRate")
	funding, fundingFiles, used, err := loadFunding(ctx, fundingDir, req.Symbol)
	if err != nil {
		return LoadResult{}, err
	}
	return LoadResult{
		Candles:     candles,
		Funding:     funding,
		FundingUsed: used,
		Provenance:  Provenance{Source: SourceBinanceVisionUM1m, Files: files, FundingFiles: fundingFiles},
	}, nil
}

// loadMinuteKlines returns every 1m candle in [firstMs, endMs). Each UTC day
// overlapping that window must have its own verified file.
func loadMinuteKlines(ctx context.Context, dir, symbol string, firstMs, endMs int64) ([]*exchange.Candle, []FileDigest, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, fmt.Errorf("visionfeed: coverage: read %s: %w", dir, err)
	}
	type dayFile struct {
		name string
		day  int64
	}
	var found []dayFile
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		match := dayFilePattern.FindStringSubmatch(entry.Name())
		if match == nil || match[1] != symbol {
			continue
		}
		day, err := utcDayMillis(match[2])
		if err != nil {
			return nil, nil, fmt.Errorf("visionfeed: coverage: %s: %w", entry.Name(), err)
		}
		found = append(found, dayFile{name: entry.Name(), day: day})
	}

	byDay := map[int64][]dayFile{}
	for _, file := range found {
		byDay[file.day] = append(byDay[file.day], file)
	}
	var wanted []dayFile
	for day := firstMs - firstMs%DayMs; day < endMs; day += DayMs {
		files := byDay[day]
		switch len(files) {
		case 0:
			return nil, nil, fmt.Errorf("visionfeed: coverage: missing 1m file for %s", dayLabel(day))
		case 1:
			wanted = append(wanted, files[0])
		default:
			names := make([]string, len(files))
			for i, file := range files {
				names[i] = file.name
			}
			sort.Strings(names)
			return nil, nil, fmt.Errorf("visionfeed: duplicate: %s has both %s", dayLabel(day), strings.Join(names, " and "))
		}
	}

	byTs := map[int64]*exchange.Candle{}
	var digests []FileDigest
	for _, file := range wanted {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		path := filepath.Join(dir, file.name)
		digest, err := verifyChecksum(path)
		if err != nil {
			return nil, nil, err
		}
		if err := CompleteUTCDay(path, symbol); err != nil {
			return nil, nil, err
		}
		records, err := readCSVRecords(path)
		if err != nil {
			return nil, nil, fmt.Errorf("visionfeed: parse %s: %w", path, err)
		}
		for _, record := range records {
			if isHeaderRow(record) {
				continue
			}
			candle, err := parseKlineRecord(record, symbol)
			if err != nil {
				return nil, nil, err
			}
			byTs[candle.Timestamp] = candle
		}
		digests = append(digests, FileDigest{Name: relName(symbol, "1m", file.name), SHA256: digest})
	}

	out := make([]*exchange.Candle, 0, (endMs-firstMs)/MinuteMs)
	for ts := firstMs; ts < endMs; ts += MinuteMs {
		candle, ok := byTs[ts]
		if !ok {
			return nil, nil, fmt.Errorf("visionfeed: coverage: missing 1m candle at %d", ts)
		}
		out = append(out, candle)
	}
	sort.Slice(digests, func(i, j int) bool { return digests[i].Name < digests[j].Name })
	return out, digests, nil
}

// loadFunding reads verified fundingRate files. A missing directory, or one
// with no data file, reports funding as unused rather than inventing a series.
func loadFunding(ctx context.Context, dir, symbol string) ([]replay.FundingPoint, []FileDigest, bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, false, nil
		}
		return nil, nil, false, fmt.Errorf("visionfeed: read %s: %w", dir, err)
	}
	var names []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		match := fundingFilePattern.FindStringSubmatch(entry.Name())
		if match == nil || match[1] != symbol {
			continue
		}
		names = append(names, entry.Name())
	}
	if len(names) == 0 {
		return nil, nil, false, nil
	}
	sort.Strings(names)

	byTs := map[int64]replay.FundingPoint{}
	var digests []FileDigest
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return nil, nil, false, err
		}
		path := filepath.Join(dir, name)
		digest, err := verifyChecksum(path)
		if err != nil {
			return nil, nil, false, err
		}
		records, err := readCSVRecords(path)
		if err != nil {
			return nil, nil, false, fmt.Errorf("visionfeed: parse %s: %w", path, err)
		}
		for i, record := range records {
			if isHeaderRow(record) {
				continue
			}
			if len(record) < fundingMinColumns {
				return nil, nil, false, fmt.Errorf("visionfeed: parse %s line %d: funding record has %d columns, want >= %d", path, i+1, len(record), fundingMinColumns)
			}
			ts, err := strconv.ParseInt(strings.TrimSpace(record[fundingColCalcTime]), 10, 64)
			if err != nil {
				return nil, nil, false, fmt.Errorf("visionfeed: parse %s line %d: calc_time %q: %w", path, i+1, record[fundingColCalcTime], err)
			}
			rate, err := strconv.ParseFloat(strings.TrimSpace(record[fundingColRate]), 64)
			if err != nil {
				return nil, nil, false, fmt.Errorf("visionfeed: parse %s line %d: funding rate %q: %w", path, i+1, record[fundingColRate], err)
			}
			stamp := normalizeTimestampMs(ts)
			if _, exists := byTs[stamp]; exists {
				return nil, nil, false, fmt.Errorf("visionfeed: parse %s line %d: duplicate calc_time %d", path, i+1, stamp)
			}
			byTs[stamp] = replay.FundingPoint{Timestamp: stamp, Rate: rate}
		}
		digests = append(digests, FileDigest{Name: relName(symbol, "fundingRate", name), SHA256: digest})
	}
	out := make([]replay.FundingPoint, 0, len(byTs))
	for _, point := range byTs {
		out = append(out, point)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Timestamp < out[j].Timestamp })
	sort.Slice(digests, func(i, j int) bool { return digests[i].Name < digests[j].Name })
	return out, digests, true, nil
}

// verifyChecksum checks the GNU sha256sum sidecar next to the data file.
// The digest covers the file bytes actually read, zip or csv.
func verifyChecksum(path string) (string, error) {
	body, err := os.ReadFile(path)
	if err != nil {
		return "", fmt.Errorf("visionfeed: checksum: read %s: %w", path, err)
	}
	sum := sha256.Sum256(body)
	got := hex.EncodeToString(sum[:])
	sidecar := path + ".CHECKSUM"
	file, err := os.Open(sidecar)
	if err != nil {
		return "", fmt.Errorf("visionfeed: checksum: %s: %w", sidecar, err)
	}
	defer file.Close()
	base := filepath.Base(path)
	scanner := bufio.NewScanner(file)
	matched := false
	for scanner.Scan() {
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !checksumLine.MatchString(line) {
			return "", fmt.Errorf("visionfeed: checksum: %s: malformed line %q", sidecar, line)
		}
		fields := strings.SplitN(line, "  ", 2)
		if fields[1] != base {
			continue
		}
		matched = true
		if fields[0] != got {
			return "", fmt.Errorf("visionfeed: checksum: %s: hash mismatch for %s", sidecar, base)
		}
	}
	if err := scanner.Err(); err != nil {
		return "", fmt.Errorf("visionfeed: checksum: read %s: %w", sidecar, err)
	}
	if !matched {
		return "", fmt.Errorf("visionfeed: checksum: %s: basename %s not listed", sidecar, base)
	}
	return got, nil
}

func readCSVRecords(path string) ([][]string, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".zip":
		reader, err := zip.OpenReader(path)
		if err != nil {
			return nil, fmt.Errorf("open zip: %w", err)
		}
		defer reader.Close()
		var out [][]string
		for _, file := range reader.File {
			if !strings.EqualFold(filepath.Ext(file.Name), ".csv") {
				continue
			}
			rc, err := file.Open()
			if err != nil {
				return nil, fmt.Errorf("open %s in zip: %w", file.Name, err)
			}
			records, err := parseCSV(rc)
			rc.Close()
			if err != nil {
				return nil, fmt.Errorf("parse %s in zip: %w", file.Name, err)
			}
			out = append(out, records...)
		}
		if len(out) == 0 {
			return nil, fmt.Errorf("zip contains no csv")
		}
		return out, nil
	case ".csv":
		file, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("open csv: %w", err)
		}
		defer file.Close()
		return parseCSV(file)
	default:
		return nil, fmt.Errorf("unsupported extension")
	}
}

func parseCSV(r io.Reader) ([][]string, error) {
	reader := csv.NewReader(r)
	reader.FieldsPerRecord = -1
	reader.ReuseRecord = false
	return reader.ReadAll()
}

func isHeaderRow(record []string) bool {
	if len(record) == 0 {
		return true
	}
	_, err := strconv.ParseInt(strings.TrimSpace(record[0]), 10, 64)
	return err != nil
}

func parseKlineRecord(record []string, symbol string) (*exchange.Candle, error) {
	if len(record) < klineMinColumns {
		return nil, fmt.Errorf("kline record has %d columns, want >= %d", len(record), klineMinColumns)
	}
	ts, err := strconv.ParseInt(strings.TrimSpace(record[klineColOpenTime]), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("open_time %q: %w", record[klineColOpenTime], err)
	}
	values := make([]float64, 0, 5)
	for _, col := range []int{klineColOpen, klineColHigh, klineColLow, klineColClose, klineColVolume} {
		value, err := strconv.ParseFloat(strings.TrimSpace(record[col]), 64)
		if err != nil {
			return nil, fmt.Errorf("column %d %q: %w", col, record[col], err)
		}
		values = append(values, value)
	}
	candle := &exchange.Candle{
		Symbol: symbol, Timestamp: normalizeTimestampMs(ts), IsClosed: true,
		Open: values[0], High: values[1], Low: values[2], Close: values[3], Volume: values[4],
	}
	if candle.Open <= 0 || candle.High <= 0 || candle.Low <= 0 || candle.Close <= 0 || candle.High < candle.Low {
		return nil, fmt.Errorf("invalid OHLC at %d: %v/%v/%v/%v", candle.Timestamp, candle.Open, candle.High, candle.Low, candle.Close)
	}
	return candle, nil
}

func normalizeTimestampMs(ts int64) int64 {
	switch {
	case ts < secondsTimestampLimit:
		return ts * 1000
	case ts >= microsecondsTimestampLimit:
		return ts / 1000
	default:
		return ts
	}
}

func utcDayMillis(label string) (int64, error) {
	parts := strings.Split(label, "-")
	year, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, fmt.Errorf("bad date %s", label)
	}
	month, err := strconv.Atoi(parts[1])
	if err != nil || month < 1 || month > 12 {
		return 0, fmt.Errorf("bad date %s", label)
	}
	day, err := strconv.Atoi(parts[2])
	if err != nil || day < 1 || day > daysInMonth(year, month) {
		return 0, fmt.Errorf("bad date %s", label)
	}
	days := int64(0)
	for y := 1970; y < year; y++ {
		days += int64(daysInYear(y))
	}
	for m := 1; m < month; m++ {
		days += int64(daysInMonth(year, m))
	}
	days += int64(day - 1)
	return days * DayMs, nil
}

func daysInYear(year int) int {
	if isLeap(year) {
		return 366
	}
	return 365
}

func daysInMonth(year, month int) int {
	if month == 2 && isLeap(year) {
		return 29
	}
	return monthLengths[month-1]
}

func isLeap(year int) bool {
	return year%4 == 0 && (year%100 != 0 || year%400 == 0)
}

func dayLabel(ms int64) string {
	days := int(ms / DayMs)
	year := 1970
	for days >= daysInYear(year) {
		days -= daysInYear(year)
		year++
	}
	month := 1
	for days >= daysInMonth(year, month) {
		days -= daysInMonth(year, month)
		month++
	}
	return fmt.Sprintf("%04d-%02d-%02d", year, month, days+1)
}

func relName(symbol, kind, name string) string {
	return symbol + "/" + kind + "/" + name
}
