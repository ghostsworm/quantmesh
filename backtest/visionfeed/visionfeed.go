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
	"errors"
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
	// maxKlineCSVBytes bounds one official 1m day: 1440 rows plus a header, with
	// room for the extra columns an official kline row carries. A larger entry is
	// refused before it is inflated.
	maxKlineCSVBytes int64 = 2 << 20
	// maxFundingCSVBytes bounds one official funding month. Settlements are a few
	// dozen rows, so this stays far below a day of klines.
	maxFundingCSVBytes int64 = 256 << 10
	// maxChecksumSidecarBytes keeps a malformed checksum manifest bounded.
	maxChecksumSidecarBytes int64 = 64 << 10
	// directoryReadBatchSize bounds each cancellable directory scan batch.
	directoryReadBatchSize = 128

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
	funding, fundingFiles, used, err := loadFunding(ctx, fundingDir, req.Symbol, req.FirstMs, req.EndMs)
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

func walkDirContext(ctx context.Context, dir string, visit func(os.DirEntry) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	info, err := os.Stat(dir)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ctxErr
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("not a directory")
	}
	directory, err := os.Open(dir)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return errors.Join(err, directory.Close())
	}
	defer directory.Close()

	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		entries, readErr := directory.ReadDir(directoryReadBatchSize)
		for _, entry := range entries {
			if err := ctx.Err(); err != nil {
				return err
			}
			if err := visit(entry); err != nil {
				return err
			}
		}
		if errors.Is(readErr, io.EOF) {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}

func openRegularFile(ctx context.Context, path string) (*os.File, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	info, err := os.Stat(path)
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("not a regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, errors.Join(err, file.Close())
	}
	return file, nil
}

// loadMinuteKlines returns every 1m candle in [firstMs, endMs). Each UTC day
// overlapping that window must have its own verified file.
func loadMinuteKlines(ctx context.Context, dir, symbol string, firstMs, endMs int64) ([]*exchange.Candle, []FileDigest, error) {
	type dayFile struct {
		name string
	}
	firstDay := firstMs - firstMs%DayMs
	wantedDays := make(map[int64]struct{})
	for day := firstDay; day < endMs; day += DayMs {
		wantedDays[day] = struct{}{}
	}
	byDay := make(map[int64]dayFile, len(wantedDays))
	err := walkDirContext(ctx, dir, func(entry os.DirEntry) error {
		if entry.IsDir() {
			return nil
		}
		match := dayFilePattern.FindStringSubmatch(entry.Name())
		if match == nil || match[1] != symbol {
			return nil
		}
		day, err := utcDayMillis(match[2])
		if err != nil {
			return fmt.Errorf("visionfeed: coverage: %s: %w", entry.Name(), err)
		}
		if _, needed := wantedDays[day]; !needed {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		info, err := os.Stat(filepath.Join(dir, entry.Name()))
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if err != nil {
			return hideCause(fmt.Sprintf("visionfeed: coverage: stat %s: %s", entry.Name(), ioReason(err)), err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("visionfeed: coverage: %s is not a regular file", entry.Name())
		}
		if existing, ok := byDay[day]; ok {
			names := []string{existing.name, entry.Name()}
			sort.Strings(names)
			return fmt.Errorf("visionfeed: duplicate: %s has both %s", dayLabel(day), strings.Join(names, " and "))
		}
		byDay[day] = dayFile{name: entry.Name()}
		return nil
	})
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, nil, ctxErr
	}
	if err != nil {
		return nil, nil, hideCause(fmt.Sprintf("visionfeed: coverage: read %s/1m: %s", symbol, ioReason(err)), err)
	}

	var wanted []dayFile
	for day := firstDay; day < endMs; day += DayMs {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		file, ok := byDay[day]
		if !ok {
			return nil, nil, fmt.Errorf("visionfeed: coverage: missing 1m file for %s", dayLabel(day))
		}
		wanted = append(wanted, file)
	}

	byTs := map[int64]*exchange.Candle{}
	var digests []FileDigest
	for _, file := range wanted {
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		path := filepath.Join(dir, file.name)
		digest, err := verifyChecksumContext(ctx, path, maxKlineCSVBytes)
		if err != nil {
			return nil, nil, err
		}
		if err := CompleteUTCDayContext(ctx, path, symbol); err != nil {
			return nil, nil, err
		}
		records, err := readCSVRecordsContext(ctx, path, maxKlineCSVBytes)
		if err != nil {
			return nil, nil, fmt.Errorf("visionfeed: parse %s: %w", filepath.Base(path), err)
		}
		for _, record := range records {
			if err := ctx.Err(); err != nil {
				return nil, nil, err
			}
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
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
		candle, ok := byTs[ts]
		if !ok {
			return nil, nil, fmt.Errorf("visionfeed: coverage: missing 1m candle at %d", ts)
		}
		out = append(out, candle)
	}
	sort.Slice(digests, func(i, j int) bool { return digests[i].Name < digests[j].Name })
	return out, digests, nil
}

// loadFunding reads the verified funding files for each UTC month overlapping
// [firstMs, endMs). No file for any of those months leaves funding unused, so the
// caller keeps its fixed rate; files for other months are ignored. Once one month
// in the window is present, every month must be present. A gap fails the load
// instead of becoming a stretch of zero funding.
func loadFunding(ctx context.Context, dir, symbol string, firstMs, endMs int64) ([]replay.FundingPoint, []FileDigest, bool, error) {
	months, err := fundingMonths(firstMs, endMs)
	if err != nil {
		return nil, nil, false, err
	}
	wantedMonths := make(map[string]struct{}, len(months))
	for _, month := range months {
		wantedMonths[month.label] = struct{}{}
	}
	byMonth := make(map[string]string, len(months))
	err = walkDirContext(ctx, dir, func(entry os.DirEntry) error {
		if entry.IsDir() {
			return nil
		}
		match := fundingFilePattern.FindStringSubmatch(entry.Name())
		if match == nil || match[1] != symbol {
			return nil
		}
		month := match[2]
		if _, needed := wantedMonths[month]; !needed {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		info, err := os.Stat(filepath.Join(dir, entry.Name()))
		if ctxErr := ctx.Err(); ctxErr != nil {
			return ctxErr
		}
		if err != nil {
			return hideCause(fmt.Sprintf("visionfeed: funding: stat %s: %s", entry.Name(), ioReason(err)), err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("visionfeed: funding: %s is not a regular file", entry.Name())
		}
		if existing, ok := byMonth[month]; ok {
			names := []string{existing, entry.Name()}
			sort.Strings(names)
			return fmt.Errorf("visionfeed: duplicate: %s has both %s", month, strings.Join(names, " and "))
		}
		byMonth[month] = entry.Name()
		return nil
	})
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, nil, false, ctxErr
	}
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil, false, nil
		}
		return nil, nil, false, hideCause(fmt.Sprintf("visionfeed: read %s/fundingRate: %s", symbol, ioReason(err)), err)
	}
	if len(byMonth) == 0 {
		return nil, nil, false, nil
	}

	byTs := map[int64]replay.FundingPoint{}
	var digests []FileDigest
	for _, month := range months {
		if err := ctx.Err(); err != nil {
			return nil, nil, false, err
		}
		name, ok := byMonth[month.label]
		if !ok {
			return nil, nil, false, fmt.Errorf("visionfeed: coverage: missing funding file for %s", month.label)
		}
		path := filepath.Join(dir, name)
		digest, err := verifyChecksumContext(ctx, path, maxFundingCSVBytes)
		if err != nil {
			return nil, nil, false, err
		}
		records, err := readCSVRecordsContext(ctx, path, maxFundingCSVBytes)
		if err != nil {
			return nil, nil, false, fmt.Errorf("visionfeed: parse %s: %w", filepath.Base(path), err)
		}
		points := 0
		for i, record := range records {
			if err := ctx.Err(); err != nil {
				return nil, nil, false, err
			}
			if isHeaderRow(record) {
				continue
			}
			if len(record) < fundingMinColumns {
				return nil, nil, false, fmt.Errorf("visionfeed: parse %s line %d: funding record has %d columns, want >= %d", filepath.Base(path), i+1, len(record), fundingMinColumns)
			}
			ts, err := strconv.ParseInt(strings.TrimSpace(record[fundingColCalcTime]), 10, 64)
			if err != nil {
				return nil, nil, false, fmt.Errorf("visionfeed: parse %s line %d: calc_time %q: %w", filepath.Base(path), i+1, record[fundingColCalcTime], err)
			}
			rate, err := strconv.ParseFloat(strings.TrimSpace(record[fundingColRate]), 64)
			if err != nil {
				return nil, nil, false, fmt.Errorf("visionfeed: parse %s line %d: funding rate %q: %w", filepath.Base(path), i+1, record[fundingColRate], err)
			}
			stamp := normalizeTimestampMs(ts)
			if stamp < month.start || stamp >= month.end {
				return nil, nil, false, fmt.Errorf("visionfeed: coverage: %s line %d: calc_time %d is outside %s", filepath.Base(path), i+1, stamp, month.label)
			}
			if _, exists := byTs[stamp]; exists {
				return nil, nil, false, fmt.Errorf("visionfeed: parse %s line %d: duplicate calc_time %d", filepath.Base(path), i+1, stamp)
			}
			byTs[stamp] = replay.FundingPoint{Timestamp: stamp, Rate: rate}
			points++
		}
		if points == 0 {
			return nil, nil, false, fmt.Errorf("visionfeed: coverage: %s has no funding settlement", filepath.Base(path))
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

type fundingMonth struct {
	label      string
	start, end int64
}

// fundingMonths lists each UTC month overlapping [firstMs, endMs). The month
// that starts at endMs is excluded.
func fundingMonths(firstMs, endMs int64) ([]fundingMonth, error) {
	label := dayLabel(firstMs)
	year, err := strconv.Atoi(label[:4])
	if err != nil {
		return nil, fmt.Errorf("visionfeed: coverage: bad window start %d", firstMs)
	}
	month, err := strconv.Atoi(label[5:7])
	if err != nil {
		return nil, fmt.Errorf("visionfeed: coverage: bad window start %d", firstMs)
	}
	var out []fundingMonth
	for {
		start, err := utcDayMillis(fmt.Sprintf("%04d-%02d-01", year, month))
		if err != nil {
			return nil, err
		}
		nextYear, nextMonth := year, month+1
		if nextMonth == 13 {
			nextYear, nextMonth = year+1, 1
		}
		next, err := utcDayMillis(fmt.Sprintf("%04d-%02d-01", nextYear, nextMonth))
		if err != nil {
			return nil, err
		}
		if start >= endMs {
			break
		}
		out = append(out, fundingMonth{label: fmt.Sprintf("%04d-%02d", year, month), start: start, end: next})
		if next >= endMs {
			break
		}
		year, month = nextYear, nextMonth
	}
	return out, nil
}

// visibleError is safe to show. Its cause is kept for the server log and may
// name a filesystem path, so Error leaves that cause out.
type visibleError struct {
	text  string
	cause error
}

func (e *visibleError) Error() string { return e.text }
func (e *visibleError) Unwrap() error { return e.cause }

func hideCause(text string, cause error) error {
	return &visibleError{text: text, cause: cause}
}

func ioReason(err error) string {
	switch {
	case os.IsNotExist(err):
		return "not found"
	case os.IsPermission(err):
		return "permission denied"
	default:
		return "io error"
	}
}

// ErrorDetail adds the hidden cause so a server log can name the file.
// The result can contain a filesystem path and must not be shown to a client.
func ErrorDetail(err error) string {
	var hidden *visibleError
	if errors.As(err, &hidden) && hidden.cause != nil {
		return err.Error() + ": " + hidden.cause.Error()
	}
	return err.Error()
}

// verifyChecksum keeps the existing helper source-compatible for publish paths.
// Replay loads use verifyChecksumContext with the narrower file-specific bound.
func verifyChecksum(path string) (string, error) {
	maxBytes := maxKlineCSVBytes
	if fundingFilePattern.MatchString(filepath.Base(path)) {
		maxBytes = maxFundingCSVBytes
	}
	return verifyChecksumContext(context.Background(), path, maxBytes)
}

// verifyChecksumContext checks the GNU sha256sum sidecar while streaming no more
// than maxBytes+1 data bytes, so growth after any metadata check stays bounded.
func verifyChecksumContext(ctx context.Context, path string, maxBytes int64) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if maxBytes < 0 {
		return "", fmt.Errorf("visionfeed: checksum: invalid size limit %d", maxBytes)
	}
	base := filepath.Base(path)
	file, err := openRegularFile(ctx, path)
	if err != nil {
		return "", hideCause(fmt.Sprintf("visionfeed: checksum: read %s: %s", base, ioReason(err)), err)
	}
	defer file.Close()
	hasher := sha256.New()
	read, err := io.Copy(hasher, &io.LimitedReader{
		R: &contextReader{ctx: ctx, reader: file},
		N: maxBytes + 1,
	})
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		return "", hideCause(fmt.Sprintf("visionfeed: checksum: read %s: %s", base, ioReason(err)), err)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if read > maxBytes {
		return "", fmt.Errorf("visionfeed: checksum: %s exceeds %d-byte limit", base, maxBytes)
	}
	got := hex.EncodeToString(hasher.Sum(nil))
	sidecar := path + ".CHECKSUM"
	checksumFile, err := openRegularFile(ctx, sidecar)
	if err != nil {
		return "", hideCause(fmt.Sprintf("visionfeed: checksum: %s: %s", filepath.Base(sidecar), ioReason(err)), err)
	}
	defer checksumFile.Close()
	sidecarReader := &countingReader{reader: &contextReader{ctx: ctx, reader: checksumFile}}
	scanner := bufio.NewScanner(&io.LimitedReader{R: sidecarReader, N: maxChecksumSidecarBytes + 1})
	scanner.Buffer(make([]byte, 1024), int(maxChecksumSidecarBytes))
	matched := false
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		if !checksumLine.MatchString(line) {
			return "", fmt.Errorf("visionfeed: checksum: %s: malformed line %q", filepath.Base(sidecar), line)
		}
		fields := strings.SplitN(line, "  ", 2)
		if fields[1] != base {
			continue
		}
		matched = true
		if fields[0] != got {
			return "", fmt.Errorf("visionfeed: checksum: %s: hash mismatch for %s", filepath.Base(sidecar), base)
		}
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if sidecarReader.bytesRead > maxChecksumSidecarBytes {
		return "", fmt.Errorf("visionfeed: checksum: %s exceeds %d-byte limit", filepath.Base(sidecar), maxChecksumSidecarBytes)
	}
	if err := scanner.Err(); err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return "", ctxErr
		}
		return "", hideCause(fmt.Sprintf("visionfeed: checksum: read %s: %s", filepath.Base(sidecar), ioReason(err)), err)
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !matched {
		return "", fmt.Errorf("visionfeed: checksum: %s: basename %s not listed", filepath.Base(sidecar), base)
	}
	return got, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.reader.Read(p)
	if ctxErr := r.ctx.Err(); ctxErr != nil {
		return n, ctxErr
	}
	return n, err
}

type countingReader struct {
	reader    io.Reader
	bytesRead int64
}

func (r *countingReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.bytesRead += int64(n)
	return n, err
}

// readCSVRecords preserves the original helper for non-replay package callers.
func readCSVRecords(path string, maxBytes int64) ([][]string, error) {
	return readCSVRecordsContext(context.Background(), path, maxBytes)
}

// readCSVRecordsContext reads one csv, or the single csv stored inside a zip.
// Both entries and plain csv files are limited while reading to close stat/read
// races; replay loads also inherit cancellation during parsing.
func readCSVRecordsContext(ctx context.Context, path string, maxBytes int64) ([][]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if maxBytes < 0 {
		return nil, fmt.Errorf("csv has invalid size limit %d", maxBytes)
	}
	switch strings.ToLower(filepath.Ext(path)) {
	case ".zip":
		archiveFile, err := openRegularFile(ctx, path)
		if err != nil {
			return nil, hideCause(fmt.Sprintf("open zip %s: %s", filepath.Base(path), ioReason(err)), err)
		}
		defer archiveFile.Close()
		info, err := archiveFile.Stat()
		if err != nil {
			return nil, hideCause(fmt.Sprintf("stat zip %s: %s", filepath.Base(path), ioReason(err)), err)
		}
		if info.Size() > maxBytes {
			return nil, fmt.Errorf("zip is %d bytes, limit %d", info.Size(), maxBytes)
		}
		reader, err := zip.NewReader(archiveFile, info.Size())
		if err != nil {
			return nil, hideCause(fmt.Sprintf("open zip %s: %s", filepath.Base(path), ioReason(err)), err)
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		want := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)) + ".csv"
		var found *zip.File
		for _, file := range reader.File {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if !strings.EqualFold(filepath.Ext(file.Name), ".csv") {
				continue
			}
			if found != nil {
				return nil, fmt.Errorf("zip holds more than one csv")
			}
			found = file
		}
		if found == nil {
			return nil, fmt.Errorf("zip contains no csv")
		}
		if err := checkZipEntryName(found.Name, want); err != nil {
			return nil, err
		}
		if found.UncompressedSize64 > uint64(maxBytes) {
			return nil, fmt.Errorf("csv in zip claims %d bytes, limit %d", found.UncompressedSize64, maxBytes)
		}
		rc, err := found.Open()
		if err != nil {
			return nil, hideCause(fmt.Sprintf("open %s in zip: %s", found.Name, ioReason(err)), err)
		}
		defer rc.Close()
		records, err := parseCSV(newLimitedCSV(&contextReader{ctx: ctx, reader: rc}, maxBytes))
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return records, err
	case ".csv":
		file, err := openRegularFile(ctx, path)
		if err != nil {
			return nil, hideCause(fmt.Sprintf("open csv %s: %s", filepath.Base(path), ioReason(err)), err)
		}
		defer file.Close()
		records, err := parseCSV(newLimitedCSV(&contextReader{ctx: ctx, reader: file}, maxBytes))
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return records, err
	default:
		return nil, fmt.Errorf("unsupported extension")
	}
}

// checkZipEntryName accepts only the csv whose name is the zip's own name with
// the extension swapped. A separator, "..", or an absolute name is refused, so
// the entry name can never name another file.
func checkZipEntryName(name, want string) error {
	if name != want || strings.ContainsAny(name, "/\\") || strings.Contains(name, "..") {
		return fmt.Errorf("zip entry %q is not %s", name, want)
	}
	return nil
}

// limitedCSV reads at most max bytes and then reports that the csv is larger
// than its limit instead of returning the short prefix as a complete file.
type limitedCSV struct {
	reader io.Reader
	max    int64
	read   int64
}

func newLimitedCSV(r io.Reader, max int64) *limitedCSV {
	return &limitedCSV{reader: r, max: max}
}

func (l *limitedCSV) Read(p []byte) (int, error) {
	if l.read > l.max {
		return 0, fmt.Errorf("csv exceeds its size limit")
	}
	remaining := l.max - l.read + 1
	if int64(len(p)) > remaining {
		p = p[:remaining]
	}
	n, err := l.reader.Read(p)
	l.read += int64(n)
	if l.read > l.max {
		return n, fmt.Errorf("csv exceeds its size limit")
	}
	return n, err
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
