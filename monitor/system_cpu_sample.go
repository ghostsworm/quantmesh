package monitor

import (
	"fmt"
	"time"
)

func readProcessCPUSample(read func() (float64, error), logicalCPUs int) (*SystemMetrics, error) {
	if read == nil {
		return nil, fmt.Errorf("process CPU sample requires a reader")
	}
	var raw float64
	capacity, err := readProcessCPUCapacityPercent(func() (float64, error) {
		var err error
		raw, err = read()
		return raw, err
	}, logicalCPUs)
	if err != nil {
		return nil, err
	}
	return &SystemMetrics{CPUPercent: capacity, processCPUPercent: &raw}, nil
}

// Legacy in-memory callers retain their original units. New production
// samples preserve the same-read process units; persisted history is not
// reconstructed or used to infer process CPU usage.
func (m *SystemMetrics) cpuThresholdPercent() float64 {
	if m.processCPUPercent != nil {
		return *m.processCPUPercent
	}
	return m.CPUPercent
}

func findOldestCPUInWindow(history []*SystemMetrics, current *SystemMetrics, minutes int) *SystemMetrics {
	windowStart := current.Timestamp.Add(-time.Duration(minutes) * time.Minute)
	var oldest *SystemMetrics
	for _, sample := range history {
		if sample == nil || (sample.processCPUPercent == nil) != (current.processCPUPercent == nil) {
			continue
		}
		if sample.Timestamp.After(windowStart) && sample.Timestamp.Before(current.Timestamp) && (oldest == nil || sample.Timestamp.Before(oldest.Timestamp)) {
			oldest = sample
		}
	}
	return oldest
}
