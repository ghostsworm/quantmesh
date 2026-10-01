package bitget

import (
	"testing"
	"time"
)

func TestParseBitgetFundingRateInterval(t *testing.T) {
	for _, tc := range []struct {
		input string
		want  time.Duration
		bad   bool
	}{{"1", time.Hour, false}, {"4", 4 * time.Hour, false}, {"8", 8 * time.Hour, false}, {"", 0, true}, {"0", 0, true}, {"25", 0, true}, {"8h", 0, true}} {
		got, err := parseBitgetFundingRateInterval(tc.input)
		if (err != nil) != tc.bad || (!tc.bad && got != tc.want) {
			t.Errorf("parseBitgetFundingRateInterval(%q) = %s, %v; want %s, bad=%v", tc.input, got, err, tc.want, tc.bad)
		}
	}
}
