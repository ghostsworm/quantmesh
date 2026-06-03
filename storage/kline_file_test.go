package storage

import (
	"path/filepath"
	"testing"
	"time"
)

func TestParseTimeFieldAndKlineFileCRUD(t *testing.T) {
	now := time.Unix(1700000000, 0).UTC()
	if !parseTimeField(nil).IsZero() || !parseTimeField(struct{}{}).IsZero() || !parseTimeField("bad").IsZero() {
		t.Fatalf("invalid time fields should parse to zero")
	}
	for _, input := range []interface{}{int64(1700000000), float64(1700000000), now, "2023-11-14 22:13:20", "2023-11-14T22:13:20Z", "2023-11-14T22:13:20+00:00"} {
		if parseTimeField(input).IsZero() {
			t.Fatalf("time field parsed zero for %#v", input)
		}
	}

	store, err := NewSQLStorage(filepath.Join(t.TempDir(), "kline.db"))
	if err != nil {
		t.Fatalf("new sql storage: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	end := now.Add(time.Hour)
	kf := &KlineFile{
		Filename: "binance_BTCUSDT_1m.csv", Exchange: "binance", Symbol: "BTCUSDT", Interval: "1m",
		StartTime: now, EndTime: &end, Status: "collecting", HasDepth: true, CandleCount: 10, FileSize: 2048, Source: "collector",
	}
	if err := store.CreateKlineFile(kf); err != nil {
		t.Fatalf("create kline file: %v", err)
	}
	if kf.ID == 0 {
		t.Fatalf("expected kline file id")
	}
	loaded, err := store.GetKlineFile(kf.ID)
	if err != nil || loaded == nil || loaded.Filename != kf.Filename || !loaded.HasDepth {
		t.Fatalf("loaded kline file=%#v err=%v", loaded, err)
	}
	byName, err := store.GetKlineFileByFilename(kf.Filename)
	if err != nil || byName == nil || byName.ID != kf.ID {
		t.Fatalf("by name=%#v err=%v", byName, err)
	}

	kf.Status = "completed"
	kf.CandleCount = 20
	kf.FileSize = 4096
	if err := store.UpdateKlineFile(kf); err != nil {
		t.Fatalf("update kline file: %v", err)
	}
	if err := store.UpdateKlineFileStatus(kf.Filename, "completed", &end, 30, 8192); err != nil {
		t.Fatalf("update kline file status: %v", err)
	}
	list, err := store.ListKlineFiles(&KlineFileFilter{Exchange: "binance", Symbol: "BTCUSDT", Interval: "1m", Status: "completed", Source: "collector", Limit: 10})
	if err != nil || len(list) != 1 {
		t.Fatalf("list=%#v err=%v", list, err)
	}
	completed, err := store.GetCompletedKlineFiles("binance", "BTCUSDT", "1m")
	if err != nil || len(completed) != 1 {
		t.Fatalf("completed=%#v err=%v", completed, err)
	}
	inRange, err := store.GetKlineFilesInTimeRange("binance", "BTCUSDT", "1m", now.Add(-time.Minute), end.Add(time.Minute))
	if err != nil || len(inRange) != 1 {
		t.Fatalf("in range=%#v err=%v", inRange, err)
	}
	if missing, err := store.GetKlineFile(9999); err != nil || missing != nil {
		t.Fatalf("missing=%#v err=%v", missing, err)
	}
	if err := store.DeleteKlineFile(kf.ID); err != nil {
		t.Fatalf("delete by id: %v", err)
	}

	kf.ID = 0
	kf.Filename = "delete-by-name.csv"
	if err := store.CreateKlineFile(kf); err != nil {
		t.Fatalf("create second kline file: %v", err)
	}
	if err := store.DeleteKlineFileByFilename(kf.Filename); err != nil {
		t.Fatalf("delete by filename: %v", err)
	}
}
