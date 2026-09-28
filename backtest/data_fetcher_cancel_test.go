package backtest

import (
	"context"
	"errors"
	"testing"
	"time"

	"quantmesh/exchange/binance"
)

type pageSourceFunc func(context.Context, string, string, int64, int) ([]*binance.Candle, error)

func (f pageSourceFunc) GetHistoricalKlinesFrom(ctx context.Context, symbol, interval string, start int64, limit int) ([]*binance.Candle, error) {
	return f(ctx, symbol, interval, start, limit)
}

func TestHistoricalPagesCancelDuringRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan struct{})
	source := pageSourceFunc(func(requestCtx context.Context, _, _ string, _ int64, _ int) ([]*binance.Candle, error) {
		close(entered)
		<-requestCtx.Done()
		return nil, requestCtx.Err()
	})
	done := make(chan error, 1)
	go func() {
		result, err := fetchHistoricalPages(ctx, source, "BTCUSDT", "1m", time.Unix(60, 0), time.Unix(600, 0))
		if result != nil {
			done <- errors.New("cancellation returned partial data")
			return
		}
		done <- err
	}()
	<-entered
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("parent cancellation did not reach page request")
	}
}

func TestHistoricalCanceledBeforeRequestDoesNotUseSourceOrCache(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	source := pageSourceFunc(func(context.Context, string, string, int64, int) ([]*binance.Candle, error) {
		t.Fatal("canceled fetch called source")
		return nil, nil
	})
	if _, err := fetchHistoricalPages(ctx, source, "BTCUSDT", "1m", time.Unix(60, 0), time.Unix(600, 0)); err != context.Canceled {
		t.Fatal(err)
	}
	if _, err := GetHistoricalDataContext(ctx, "BTCUSDT", "1m", time.Unix(60, 0), time.Unix(600, 0), nil); err != context.Canceled {
		t.Fatal(err)
	}
}

func TestHistoricalSourceCannotReturnSuccessAfterCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	source := pageSourceFunc(func(context.Context, string, string, int64, int) ([]*binance.Candle, error) {
		cancel()
		return []*binance.Candle{{Timestamp: 600000}}, nil
	})
	result, err := fetchHistoricalPages(ctx, source, "BTCUSDT", "1m", time.Unix(60, 0), time.Unix(600, 0))
	if result != nil || err != context.Canceled {
		t.Fatalf("result=%v error=%v", result, err)
	}
}

func TestHistoricalPagesRejectStalledAndNilResponses(t *testing.T) {
	for _, page := range [][]*binance.Candle{{nil}, {{Timestamp: 1000}}} {
		source := pageSourceFunc(func(context.Context, string, string, int64, int) ([]*binance.Candle, error) { return page, nil })
		if _, err := fetchHistoricalPages(context.Background(), source, "BTCUSDT", "1m", time.Unix(60, 0), time.Unix(600, 0)); err == nil {
			t.Fatal("accepted invalid page")
		}
	}
}
