package monitor

import (
	"errors"
	"math"
	"testing"
)

func TestProcessCPUCapacityPercentUsesOneSourceAndPreservesZero(t *testing.T) {
	for _, tc := range []struct {
		name    string
		raw     float64
		cpus    int
		want    float64
		invalid bool
	}{
		{"multicore", 300, 8, 37.5, false},
		{"observed_regression", 101.370776, 8, 12.671347, false},
		{"idle", 0, 8, 0, false},
		{"single_core", 80, 1, 80, false},
		{"full_capacity", 800, 8, 100, false},
		{"negative", -1, 8, 0, true},
		{"nan", math.NaN(), 8, 0, true},
		{"infinite", math.Inf(1), 8, 0, true},
		{"over_capacity", 801, 8, 0, true},
		{"missing_cpus", 80, 0, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			got, err := readProcessCPUCapacityPercent(func() (float64, error) { calls++; return tc.raw, nil }, tc.cpus)
			if (err != nil) != tc.invalid || math.Abs(got-tc.want) > 1e-9 {
				t.Fatalf("got=%v err=%v want=%v invalid=%v", got, err, tc.want, tc.invalid)
			}
			wantCalls := 1
			if tc.cpus < 1 {
				wantCalls = 0
			}
			if calls != wantCalls {
				t.Fatal("CPU read silently switched sources or repeated")
			}
		})
	}
	fixtureErr := errors.New("fixture process reader unavailable")
	if _, err := readProcessCPUCapacityPercent(func() (float64, error) { return 0, fixtureErr }, 8); !errors.Is(err, fixtureErr) {
		t.Fatal("reader failure hidden by host fallback", err)
	}
	if _, err := readProcessCPUCapacityPercent(nil, 8); err == nil {
		t.Fatal("missing reader accepted")
	}
}
