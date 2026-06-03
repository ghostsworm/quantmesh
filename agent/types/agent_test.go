package types

import "testing"

func TestSecurityLevelString(t *testing.T) {
	tests := []struct {
		level SecurityLevel
		want  string
	}{
		{level: SecurityLevelNone, want: "none"},
		{level: SecurityLevelLow, want: "low"},
		{level: SecurityLevelMedium, want: "medium"},
		{level: SecurityLevelHigh, want: "high"},
		{level: SecurityLevelCritical, want: "critical"},
		{level: SecurityLevel(99), want: "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.level.String(); got != tt.want {
				t.Fatalf("SecurityLevel(%d).String() = %q, want %q", tt.level, got, tt.want)
			}
		})
	}
}

func TestSecurityLevelColor(t *testing.T) {
	tests := []struct {
		level SecurityLevel
		want  string
	}{
		{level: SecurityLevelNone, want: "green"},
		{level: SecurityLevelLow, want: "green"},
		{level: SecurityLevelMedium, want: "yellow"},
		{level: SecurityLevelHigh, want: "orange"},
		{level: SecurityLevelCritical, want: "red"},
		{level: SecurityLevel(99), want: "gray"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if got := tt.level.Color(); got != tt.want {
				t.Fatalf("SecurityLevel(%d).Color() = %q, want %q", tt.level, got, tt.want)
			}
		})
	}
}
