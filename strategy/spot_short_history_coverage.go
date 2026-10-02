package strategy

import (
	"context"
	"fmt"

	"quantmesh/exchange"
)

const (
	spotShortHistoryPageSize   = 100
	spotShortHistoryMaxRecords = 10000
)

type spotShortHistoryPageReader func(context.Context, int, int) ([]exchange.MarginBorrowRecord, int64, error)

// A unique amount match is not proof if records were omitted or duplicated.
func readCompleteSpotShortHistory(ctx context.Context, read spotShortHistoryPageReader) ([]exchange.MarginBorrowRecord, error) {
	var records []exchange.MarginBorrowRecord
	seen := make(map[int64]struct{})
	var expectedTotal int64 = -1
	for page := 1; ; page++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		rows, total, err := read(ctx, page, spotShortHistoryPageSize)
		if err != nil {
			return nil, fmt.Errorf("read margin history page %d: %w", page, err)
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if total < 0 || total > spotShortHistoryMaxRecords || (expectedTotal >= 0 && total != expectedTotal) {
			return nil, fmt.Errorf("margin history total is invalid, exceeds limit, or changed during pagination")
		}
		expectedTotal = total
		remaining := total - int64(len(records))
		want := min(remaining, int64(spotShortHistoryPageSize))
		if remaining < 0 || int64(len(rows)) != want {
			return nil, fmt.Errorf("margin history page %d is incomplete or inconsistent with total", page)
		}
		for _, row := range rows {
			if row.TransferID <= 0 {
				return nil, fmt.Errorf("margin history has invalid transaction identity")
			}
			if _, exists := seen[row.TransferID]; exists {
				return nil, fmt.Errorf("margin history repeats transaction identity")
			}
			seen[row.TransferID] = struct{}{}
		}
		records = append(records, rows...)
		if int64(len(records)) == expectedTotal {
			return records, nil
		}
	}
}
