package storage

import (
	"testing"
	"time"
)

func TestGeminiUsageSaveAndQuery(t *testing.T) {
	s, err := NewStorage("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	now := time.Now().UTC().Truncate(time.Millisecond)
	rec := &GeminiUsageRecord{
		CalledAt:     now,
		Model:        "gemini-2.0-flash",
		Source:       "test",
		InputTokens:  10,
		OutputTokens: 20,
		DurationMs:   100,
	}
	if err := s.SaveGeminiUsageRecord(rec); err != nil {
		t.Fatal(err)
	}
	if rec.ID < 1 {
		t.Fatalf("expected ID set, got %d", rec.ID)
	}

	list, total, err := s.QueryGeminiUsageRecords(nil, nil, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(list) != 1 {
		t.Fatalf("total=%d len=%d", total, len(list))
	}
	cnt, inTok, outTok, err := s.AggregateGeminiUsageTotals(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if cnt != 1 || inTok != 10 || outTok != 20 {
		t.Fatalf("agg cnt=%d in=%d out=%d", cnt, inTok, outTok)
	}
}
