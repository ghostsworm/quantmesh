package inspector

import (
	"testing"
	"time"
)

func TestDefaultSchedulerConfig(t *testing.T) {
	cfg := DefaultSchedulerConfig()

	if cfg.RegularInterval != time.Hour {
		t.Fatalf("RegularInterval = %s, want %s", cfg.RegularInterval, time.Hour)
	}
	if cfg.QuietInterval != 4*time.Hour {
		t.Fatalf("QuietInterval = %s, want %s", cfg.QuietInterval, 4*time.Hour)
	}
	if cfg.QuietHoursStart != 23 || cfg.QuietHoursEnd != 7 {
		t.Fatalf("quiet hours = %d-%d, want 23-7", cfg.QuietHoursStart, cfg.QuietHoursEnd)
	}
}

func TestNewSchedulerNormalizesIntervals(t *testing.T) {
	scheduler := NewScheduler(SchedulerConfig{
		RegularInterval: 0,
		QuietInterval:   -time.Second,
	})

	if scheduler.cfg.RegularInterval != time.Hour {
		t.Fatalf("RegularInterval = %s, want default %s", scheduler.cfg.RegularInterval, time.Hour)
	}
	if scheduler.cfg.QuietInterval != 4*time.Hour {
		t.Fatalf("QuietInterval = %s, want default %s", scheduler.cfg.QuietInterval, 4*time.Hour)
	}
}

func TestSchedulerQuietHoursAcrossMidnight(t *testing.T) {
	scheduler := NewScheduler(SchedulerConfig{
		RegularInterval: 30 * time.Minute,
		QuietHoursStart: 23,
		QuietHoursEnd:   7,
		QuietInterval:   3 * time.Hour,
	})

	tests := []struct {
		name  string
		hour  int
		quiet bool
	}{
		{name: "before quiet", hour: 22, quiet: false},
		{name: "start quiet", hour: 23, quiet: true},
		{name: "after midnight", hour: 2, quiet: true},
		{name: "end quiet", hour: 7, quiet: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := scheduler.inQuietHours(tt.hour); got != tt.quiet {
				t.Fatalf("inQuietHours(%d) = %v, want %v", tt.hour, got, tt.quiet)
			}
		})
	}
}

func TestSchedulerIntervalAt(t *testing.T) {
	scheduler := NewScheduler(SchedulerConfig{
		RegularInterval: 15 * time.Minute,
		QuietHoursStart: 1,
		QuietHoursEnd:   3,
		QuietInterval:   2 * time.Hour,
	})

	quietTime := time.Date(2026, 6, 3, 2, 0, 0, 0, time.UTC)
	if got := scheduler.intervalAt(quietTime); got != 2*time.Hour {
		t.Fatalf("quiet interval = %s, want %s", got, 2*time.Hour)
	}

	regularTime := time.Date(2026, 6, 3, 4, 0, 0, 0, time.UTC)
	if got := scheduler.intervalAt(regularTime); got != 15*time.Minute {
		t.Fatalf("regular interval = %s, want %s", got, 15*time.Minute)
	}
}
