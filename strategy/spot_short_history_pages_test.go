package strategy

import (
	"context"
	"errors"
	"testing"

	"quantmesh/exchange"
)

func TestSpotShortHistoryCoveragePages(t *testing.T) {
	for _, mode := range []string{"complete", "short", "negative", "excessive", "changed_total", "duplicate", "empty_tail", "oversized", "zero_identity", "zero_total_with_row"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			rows, err := readCompleteSpotShortHistory(context.Background(), func(_ context.Context, page, size int) ([]exchange.MarginBorrowRecord, int64, error) {
				calls++
				if size != spotShortHistoryPageSize {
					t.Fatal("unexpected page size")
				}
				total := int64(101)
				count := 100
				if page == 2 {
					count = 1
				}
				switch mode {
				case "short":
					count = 1
				case "negative":
					total = -1
				case "excessive":
					total = spotShortHistoryMaxRecords + 1
				case "changed_total":
					if page == 2 {
						total++
					}
				case "empty_tail":
					if page == 2 {
						count = 0
					}
				case "oversized":
					count = 101
				case "zero_total_with_row":
					total, count = 0, 1
				}
				result := make([]exchange.MarginBorrowRecord, count)
				for i := range result {
					result[i].TransferID = int64((page-1)*size + i + 1)
				}
				if mode == "duplicate" && page == 2 {
					result[0].TransferID = 1
				}
				if mode == "zero_identity" {
					result[0].TransferID = 0
				}
				return result, total, nil
			})
			if mode == "complete" {
				if err != nil || len(rows) != 101 || calls != 2 {
					t.Fatalf("complete coverage failed: rows=%d calls=%d err=%v", len(rows), calls, err)
				}
			} else if err == nil || rows != nil || calls > 2 {
				t.Fatalf("invalid history accepted or unbounded: calls=%d err=%v", calls, err)
			}
		})
	}
}

func TestSpotShortHistoryStopsCanceledReader(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	rows, err := readCompleteSpotShortHistory(ctx, func(context.Context, int, int) ([]exchange.MarginBorrowRecord, int64, error) {
		calls++
		cancel()
		return nil, 0, nil
	})
	if !errors.Is(err, context.Canceled) || rows != nil || calls != 1 {
		t.Fatal("canceled read yielded complete evidence")
	}
}
