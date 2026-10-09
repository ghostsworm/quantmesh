package monitor

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"runtime"
	"strings"
	"testing"
	"time"

	"quantmesh/event"
)

func cpuContractSample(t *testing.T, raw float64, cpus int, timestamp time.Time) *SystemMetrics {
	t.Helper()
	sample, err := readProcessCPUSample(func() (float64, error) { return raw, nil }, cpus)
	if err != nil {
		t.Fatal(err)
	}
	sample.Timestamp = timestamp
	return sample
}

func TestWatchdogCPUThresholdPreservesProcessUnits(t *testing.T) {
	for _, cpus := range []int{1, 8, 32} {
		t.Run(fmt.Sprintf("%d_cores", cpus), func(t *testing.T) {
			cfg := testWatchdogConfig()
			cfg.Watchdog.Notifications.RateThreshold.Enabled = false
			cfg.Watchdog.Notifications.FixedThreshold.MemoryMB = 0
			current := cpuContractSample(t, 100, cpus, time.Now())
			if !NewThresholdChecker(cfg).CheckFixedThreshold(current) {
				t.Fatal("CPU normalization suppressed existing 80% process threshold")
			}
			w := NewWatchdog(cfg, nil, nil, nil)
			defer w.cancel()
			sender := &recordingAlertSender{}
			w.notifier = sender
			if err := w.checkThresholds(current); err != nil {
				t.Fatal(err)
			}
			if len(sender.events) != 1 || sender.events[0].Type != event.EventTypeSystemCPUHigh {
				t.Fatalf("missing actual CPU notification: %+v", sender.events)
			}
			if sender.events[0].Data["cpu_percent"] != "100.00" || !strings.Contains(sender.events[0].Data["message"].(string), "100.00%") {
				t.Fatalf("alert units differ from decision: %+v", sender.events[0].Data)
			}
			if sender.events[0].Data["cpu_percent_basis"] != "process_single_core_percent" || sender.events[0].Data["cpu_capacity_percent"] != fmt.Sprintf("%.2f", current.CPUPercent) {
				t.Fatalf("notification lost unit evidence: %+v", sender.events[0].Data)
			}
		})
	}
}

func TestProcessCPUSampleKeepsSameReadAlertAndDisplayUnits(t *testing.T) {
	calls := 0
	sample, err := readProcessCPUSample(func() (float64, error) { calls++; return 100, nil }, 8)
	if err != nil || calls != 1 || sample.CPUPercent != 12.5 || sample.cpuThresholdPercent() != 100 {
		t.Fatalf("sample sources or units diverged: %+v calls=%d err=%v", sample, calls, err)
	}
	encoded, err := json.Marshal(sample)
	if err != nil || strings.Contains(string(encoded), "processCPUPercent") || !strings.Contains(string(encoded), `"cpu_percent":12.5`) {
		t.Fatalf("internal alert sample changed JSON contract: %s %v", encoded, err)
	}
	cause := errors.New("fixture unavailable")
	if result, err := readProcessCPUSample(func() (float64, error) { return 100, cause }, 8); result != nil || !errors.Is(err, cause) {
		t.Fatalf("failed read became usable sample: %+v %v", result, err)
	}
	if result, err := readProcessCPUSample(nil, 8); result != nil || err == nil {
		t.Fatal("missing reader accepted")
	}
}

func TestWatchdogCPUDoesNotCompareMixedSampleUnits(t *testing.T) {
	cfg := testWatchdogConfig()
	now := time.Now()
	current := cpuContractSample(t, 100, 8, now)
	legacy := &SystemMetrics{Timestamp: now.Add(-time.Minute), CPUPercent: 0}
	checker := NewThresholdChecker(cfg)
	legacyCurrent := &SystemMetrics{Timestamp: now, CPUPercent: 100}
	typedOld := cpuContractSample(t, 0, 8, now.Add(-time.Minute))
	if checker.CheckRateThreshold(current, []*SystemMetrics{legacy}, 5, 20) || checker.CheckRateThreshold(legacyCurrent, []*SystemMetrics{typedOld}, 5, 20) {
		t.Fatal("mixed provenance became CPU rate evidence")
	}
}

func TestCollectSystemMetricsRetainsCPUAlertUnits(t *testing.T) {
	metrics, err := CollectSystemMetrics()
	if err != nil {
		t.Fatal(err)
	}
	if metrics == nil || metrics.processCPUPercent == nil || metrics.ProcessID == 0 || math.Abs(metrics.CPUPercent*float64(runtime.NumCPU())-metrics.cpuThresholdPercent()) > 1e-9 {
		t.Fatalf("actual collector lost same-read alert units: %+v", metrics)
	}
}

func TestWatchdogMemoryThresholdDoesNotInventCPUAlert(t *testing.T) {
	cfg := testWatchdogConfig()
	cfg.Watchdog.Notifications.RateThreshold.Enabled = false
	current := cpuContractSample(t, 0, 8, time.Now())
	current.MemoryMB = 900
	w := NewWatchdog(cfg, nil, nil, nil)
	defer w.cancel()
	sender := &recordingAlertSender{}
	w.notifier = sender
	if err := w.checkThresholds(current); err != nil {
		t.Fatal(err)
	}
	if len(sender.events) != 1 || sender.events[0].Type != event.EventTypeSystemMemoryHigh {
		t.Fatalf("memory failure became CPU failure: %+v", sender.events)
	}
}

func TestWatchdogCPURatePreservesProcessUnits(t *testing.T) {
	cfg := testWatchdogConfig()
	cfg.Watchdog.Notifications.FixedThreshold.Enabled = false
	cfg.Watchdog.Notifications.RateThreshold.MemoryIncreaseMB = 0
	now := time.Now()
	old := cpuContractSample(t, 50, 8, now.Add(-time.Minute))
	current := cpuContractSample(t, 75, 8, now)
	if !NewThresholdChecker(cfg).CheckRateThreshold(current, []*SystemMetrics{old}, 5, 20) {
		t.Fatal("CPU normalization suppressed 25-point process increase")
	}
	w := NewWatchdog(cfg, nil, nil, nil)
	defer w.cancel()
	sender := &recordingAlertSender{}
	w.notifier = sender
	w.updateHistoryCache(old)
	if err := w.checkThresholds(current); err != nil {
		t.Fatal(err)
	}
	if len(sender.events) != 1 || sender.events[0].Data["cpu_percent"] != "75.00" || !strings.Contains(sender.events[0].Data["message"].(string), "25.00%") {
		t.Fatalf("rate alert missing or wrong units: %+v", sender.events)
	}
}
