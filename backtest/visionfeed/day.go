package visionfeed

import (
	"fmt"
	"path/filepath"
)

// CompleteUTCDay reports whether a verified 1m file holds every minute of its
// own UTC day. The day comes from the official file name, not from the caller.
// A header row is ignored. A candle outside that day, a duplicate timestamp, a
// missing minute, or a row the loader cannot parse fails the check. Funding
// files are not checked here: replay reads every settlement row it finds and
// does not require one per minute.
func CompleteUTCDay(path, symbol string) error {
	name := filepath.Base(path)
	match := dayFilePattern.FindStringSubmatch(name)
	if match == nil || match[1] != symbol {
		return fmt.Errorf("visionfeed: coverage: %s is not a %s 1m day file", name, symbol)
	}
	day, err := utcDayMillis(match[2])
	if err != nil {
		return fmt.Errorf("visionfeed: coverage: %s: %w", name, err)
	}
	records, err := readCSVRecords(path, maxKlineCSVBytes)
	if err != nil {
		return fmt.Errorf("visionfeed: parse %s: %w", name, err)
	}
	seen := map[int64]bool{}
	for i, record := range records {
		if isHeaderRow(record) {
			continue
		}
		candle, err := parseKlineRecord(record, symbol)
		if err != nil {
			return fmt.Errorf("visionfeed: parse %s line %d: %w", name, i+1, err)
		}
		if candle.Timestamp < day || candle.Timestamp >= day+DayMs {
			return fmt.Errorf("visionfeed: coverage: %s line %d is outside %s", name, i+1, dayLabel(day))
		}
		if candle.Timestamp%MinuteMs != 0 {
			return fmt.Errorf("visionfeed: coverage: %s line %d is not minute aligned", name, i+1)
		}
		if seen[candle.Timestamp] {
			return fmt.Errorf("visionfeed: coverage: duplicate candle at %d in %s", candle.Timestamp, name)
		}
		seen[candle.Timestamp] = true
	}
	for ts := day; ts < day+DayMs; ts += MinuteMs {
		if !seen[ts] {
			return fmt.Errorf("visionfeed: coverage: %s is missing 1m candle at %d", name, ts)
		}
	}
	return nil
}
