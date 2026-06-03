package monitor

import (
	"context"
	"runtime"
	"testing"
	"time"

	"quantmesh/config"
)

func testWatchdogConfig() *config.Config {
	cfg := &config.Config{}
	cfg.Watchdog.Notifications.FixedThreshold.Enabled = true
	cfg.Watchdog.Notifications.FixedThreshold.CPUPercent = 80
	cfg.Watchdog.Notifications.FixedThreshold.MemoryMB = 512
	cfg.Watchdog.Notifications.RateThreshold.Enabled = true
	cfg.Watchdog.Notifications.RateThreshold.WindowMinutes = 5
	cfg.Watchdog.Notifications.RateThreshold.CPUIncrease = 20
	cfg.Watchdog.Notifications.RateThreshold.MemoryIncreaseMB = 128
	return cfg
}

func TestThresholdCheckerFixedAndRateThresholds(t *testing.T) {
	cfg := testWatchdogConfig()
	checker := NewThresholdChecker(cfg)
	now := time.Now()
	current := &SystemMetrics{
		Timestamp:  now,
		CPUPercent: 85,
		MemoryMB:   700,
	}

	if !checker.CheckFixedThreshold(current) {
		t.Fatalf("expected fixed threshold to trigger")
	}

	cfg.Watchdog.Notifications.FixedThreshold.Enabled = false
	if checker.CheckFixedThreshold(current) {
		t.Fatalf("disabled fixed threshold should not trigger")
	}
	cfg.Watchdog.Notifications.FixedThreshold.Enabled = true

	history := []*SystemMetrics{
		{Timestamp: now.Add(-10 * time.Minute), CPUPercent: 1, MemoryMB: 1},
		{Timestamp: now.Add(-4 * time.Minute), CPUPercent: 50, MemoryMB: 500},
		{Timestamp: now.Add(-2 * time.Minute), CPUPercent: 70, MemoryMB: 600},
	}
	if !checker.CheckRateThreshold(current, history, 5, 20) {
		t.Fatalf("expected CPU rate threshold to trigger")
	}
	if checker.CheckRateThreshold(current, history, 5, 40) {
		t.Fatalf("CPU increase below threshold should not trigger")
	}
	if checker.CheckRateThreshold(current, history, 5, 0) {
		t.Fatalf("non-positive CPU threshold should not trigger")
	}
	if checker.CheckRateThreshold(current, nil, 5, 20) {
		t.Fatalf("missing history should not trigger CPU rate threshold")
	}

	if !checker.CheckMemoryRateThreshold(current, history, 5, 128) {
		t.Fatalf("expected memory rate threshold to trigger")
	}
	if checker.CheckMemoryRateThreshold(current, history, 5, 256) {
		t.Fatalf("memory increase below threshold should not trigger")
	}
	if checker.CheckMemoryRateThreshold(current, history, 5, 0) {
		t.Fatalf("non-positive memory threshold should not trigger")
	}

	cfg.Watchdog.Notifications.RateThreshold.Enabled = false
	if checker.CheckRateThreshold(current, history, 5, 20) {
		t.Fatalf("disabled rate threshold should not trigger CPU threshold")
	}
	if checker.CheckMemoryRateThreshold(current, history, 5, 128) {
		t.Fatalf("disabled rate threshold should not trigger memory threshold")
	}
}

func TestWatchdogDefaultsCacheNotificationsAndHelpers(t *testing.T) {
	cfg := testWatchdogConfig()
	w := NewWatchdog(cfg, nil, nil, nil)

	if w.sampleInterval != 120*time.Second {
		t.Fatalf("default sample interval = %v, want 120s", w.sampleInterval)
	}
	if w.cooldownDuration != 30*time.Minute {
		t.Fatalf("default cooldown = %v, want 30m", w.cooldownDuration)
	}
	if w.maxHistory <= 0 {
		t.Fatalf("max history should be positive")
	}

	if err := w.Start(context.Background()); err != nil {
		t.Fatalf("disabled watchdog start returned error: %v", err)
	}
	if err := w.saveMetrics(&SystemMetrics{Timestamp: time.Now()}); err != nil {
		t.Fatalf("nil storage save should be a no-op: %v", err)
	}
	if latest := w.GetLatestMetrics(); latest != nil {
		t.Fatalf("empty cache latest = %#v, want nil", latest)
	}

	w.maxHistory = 2
	first := &SystemMetrics{Timestamp: time.Now().Add(-3 * time.Minute), CPUPercent: 10}
	second := &SystemMetrics{Timestamp: time.Now().Add(-2 * time.Minute), CPUPercent: 20}
	third := &SystemMetrics{Timestamp: time.Now().Add(-time.Minute), CPUPercent: 30}
	w.updateHistoryCache(first)
	w.updateHistoryCache(second)
	w.updateHistoryCache(third)

	if len(w.historyCache) != 2 {
		t.Fatalf("history cache len = %d, want 2", len(w.historyCache))
	}
	if latest := w.GetLatestMetrics(); latest != third {
		t.Fatalf("latest metrics = %#v, want third sample", latest)
	}

	if !w.shouldNotify("fixed_cpu") {
		t.Fatalf("first notification should be allowed")
	}
	w.updateNotificationTime("fixed_cpu")
	if w.shouldNotify("fixed_cpu") {
		t.Fatalf("notification inside cooldown should be suppressed")
	}
	w.mu.Lock()
	w.lastNotificationTime["fixed_cpu"] = time.Now().Add(-31 * time.Minute)
	w.mu.Unlock()
	if !w.shouldNotify("fixed_cpu") {
		t.Fatalf("notification after cooldown should be allowed")
	}

	if got := findOldestInWindow([]*SystemMetrics{first, second, third}, time.Now(), 3); got != second {
		t.Fatalf("oldest in window = %#v, want second sample", got)
	}

	for input, want := range map[string][2]int{
		"23:45": {23, 45},
		"99:70": {0, 0},
		"-1:-2": {0, 0},
	} {
		hour, minute := parseSchedule(input)
		if hour != want[0] || minute != want[1] {
			t.Fatalf("parseSchedule(%q) = %02d:%02d, want %02d:%02d", input, hour, minute, want[0], want[1])
		}
	}

	w.Stop()
}

func TestMemoryManagerStatsAndChecks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mm := NewMemoryManager(&config.Config{}, ctx)

	if mm.gcInterval != 5*time.Minute {
		t.Fatalf("gc interval = %v, want 5m", mm.gcInterval)
	}
	if mm.cleanupInterval != 30*time.Minute {
		t.Fatalf("cleanup interval = %v, want 30m", mm.cleanupInterval)
	}

	stats := mm.GetMemoryStats()
	for _, key := range []string{
		"alloc_mb",
		"sys_mb",
		"num_gc",
		"goroutines",
		"heap_alloc_mb",
		"heap_sys_mb",
		"heap_idle_mb",
		"heap_inuse_mb",
		"next_gc_mb",
		"gc_cpu_fraction",
	} {
		if _, ok := stats[key]; !ok {
			t.Fatalf("memory stats missing key %q", key)
		}
	}

	mm.forceGC()
	mm.mu.RLock()
	lastGC := mm.lastGCStats.NumGC
	mm.mu.RUnlock()
	if lastGC == 0 {
		t.Fatalf("forceGC should update last GC stats")
	}

	mm.mu.Lock()
	mm.lastGCStats = runtime.MemStats{Alloc: ^uint64(0)}
	mm.mu.Unlock()
	mm.checkMemoryUsage()
	mm.Stop()
}
