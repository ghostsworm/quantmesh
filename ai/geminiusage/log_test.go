package geminiusage

import (
	"testing"
	"time"
)

func TestRecordAndSnapshotOrder(t *testing.T) {
	mu.Lock()
	entries = nil
	mu.Unlock()
	Record(Entry{At: time.Unix(1, 0), Model: "m1", Source: "a", InputTokens: 1, OutputTokens: 2, DurationMs: 10})
	Record(Entry{At: time.Unix(2, 0), Model: "m2", Source: "b", InputTokens: 3, OutputTokens: 4, DurationMs: 20})
	snap := Snapshot()
	if len(snap) != 2 {
		t.Fatalf("len=%d", len(snap))
	}
	if snap[0].Model != "m2" || snap[1].Model != "m1" {
		t.Fatalf("order: %+v", snap)
	}
	ag := Aggregate(snap)
	if ag.CallCount != 2 || ag.TotalInputTokens != 4 || ag.TotalOutputTokens != 6 {
		t.Fatalf("aggregate: %+v", ag)
	}
}
