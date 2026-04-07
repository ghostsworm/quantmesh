package whitebit

import (
	"testing"
	"time"
)

func TestParseWhiteBITNextFundingTimestamp(t *testing.T) {
	ts := time.Date(2026, 4, 8, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		in   string
		want time.Time
	}{
		{ts.Format(time.RFC3339), ts},
		{"1775606400000", time.UnixMilli(1775606400000).UTC()},
		{"1775606400", time.Unix(1775606400, 0).UTC()},
	}
	for _, c := range cases {
		got, err := parseWhiteBITNextFundingTimestamp(c.in)
		if err != nil {
			t.Fatalf("parse %q: %v", c.in, err)
		}
		if !got.Equal(c.want) {
			t.Errorf("parse %q: got %v want %v", c.in, got, c.want)
		}
	}
}
