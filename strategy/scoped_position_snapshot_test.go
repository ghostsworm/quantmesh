package strategy

import (
	"context"
	"errors"
	"testing"

	"quantmesh/exchange"
)

type cancelAfterPositionRead struct {
	exchange.IExchange
	cancel context.CancelFunc
	calls  int
}

func (e *cancelAfterPositionRead) GetPositions(context.Context, string) ([]*exchange.Position, error) {
	e.calls++
	e.cancel()
	return []*exchange.Position{}, nil
}

func TestReadScopedPositionSnapshotRejectsEndedContext(t *testing.T) {
	t.Run("already canceled does not query", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		ex := &cancelAfterPositionRead{cancel: cancel}
		_, err := readScopedPositionSnapshot(ctx, ex, "BTCUSDT")
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context canceled", err)
		}
		var unavailable *positionSnapshotQueryError
		if !errors.As(err, &unavailable) {
			t.Error("canceled-before-read lacks unavailable evidence classification")
		}
		if ex.calls != 0 {
			t.Fatalf("exchange queried %d times after cancellation", ex.calls)
		}
	})

	t.Run("canceled during query rejects returned snapshot", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		ex := &cancelAfterPositionRead{cancel: cancel}
		positions, err := readScopedPositionSnapshot(ctx, ex, "BTCUSDT")
		if !errors.Is(err, context.Canceled) || positions != nil {
			t.Fatalf("positions=%v error=%v, want canceled context and no evidence", positions, err)
		}
		var unavailable *positionSnapshotQueryError
		if !errors.As(err, &unavailable) {
			t.Error("canceled-after-read lacks unavailable evidence classification")
		}
	})
}
