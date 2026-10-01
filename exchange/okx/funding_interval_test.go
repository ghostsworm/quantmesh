package okx

import (
	"testing"
	"time"
)

func TestParseOKXFundingInterval(t *testing.T) {
	for _, tc := range []struct {
		name string
		from string
		to   string
		want time.Duration
		bad  bool
	}{{"eight hours", "1000", "28801000", 8 * time.Hour, false}, {"one hour", "1000", "3601000", time.Hour, false}, {"missing next", "1000", "", 0, true}, {"reversed", "2000", "1000", 0, true}, {"over day", "1000", "90001000", 0, true}} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := parseOKXFundingInterval(tc.from, tc.to)
			if (err != nil) != tc.bad || (!tc.bad && got != tc.want) {
				t.Fatalf("parseOKXFundingInterval(%q, %q) = %s, %v; want %s, bad=%v", tc.from, tc.to, got, err, tc.want, tc.bad)
			}
		})
	}
}
