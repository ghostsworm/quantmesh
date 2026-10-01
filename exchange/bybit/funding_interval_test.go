package bybit

import (
	"testing"
	"time"
)

func TestParseBybitFundingIntervalHours(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    time.Duration
		wantErr bool
	}{
		{name: "one hour", input: "1", want: time.Hour},
		{name: "eight hours", input: "8", want: 8 * time.Hour},
		{name: "daily", input: "24", want: 24 * time.Hour},
		{name: "missing", input: "", wantErr: true},
		{name: "zero", input: "0", wantErr: true},
		{name: "over one day", input: "25", wantErr: true},
		{name: "malformed", input: "8h", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseBybitFundingIntervalHours(tt.input)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseBybitFundingIntervalHours(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			}
			if err == nil && got != tt.want {
				t.Fatalf("parseBybitFundingIntervalHours(%q) = %s, want %s", tt.input, got, tt.want)
			}
		})
	}
}
