package kucoin

import (
	"testing"
	"time"
)

func TestParseKuCoinFundingGranularity(t *testing.T) {
	for _, tc := range []struct {
		input int64
		want  time.Duration
		bad   bool
	}{{3600000, time.Hour, false}, {14400000, 4 * time.Hour, false}, {28800000, 8 * time.Hour, false}, {0, 0, true}, {-1, 0, true}, {90000000, 0, true}} {
		got, err := parseKuCoinFundingGranularity(tc.input)
		if (err != nil) != tc.bad || (!tc.bad && got != tc.want) {
			t.Errorf("parseKuCoinFundingGranularity(%d) = %s, %v; want %s, bad=%v", tc.input, got, err, tc.want, tc.bad)
		}
	}
}
