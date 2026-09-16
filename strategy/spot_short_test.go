package strategy

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"quantmesh/config"
	"quantmesh/position"
)

// mockMarginExchange 記錄借還調用
type mockMarginExchange struct {
	mockFCExchange
	marginMu sync.Mutex
	borrowed []float64
	repaid   []float64
	repayErr error
}

func (m *mockMarginExchange) Borrow(ctx context.Context, asset string, amount float64) (int64, error) {
	m.marginMu.Lock()
	defer m.marginMu.Unlock()
	m.borrowed = append(m.borrowed, amount)
	return 1, nil
}

func (m *mockMarginExchange) Repay(ctx context.Context, asset string, amount float64) (int64, error) {
	m.marginMu.Lock()
	defer m.marginMu.Unlock()
	m.repaid = append(m.repaid, amount)
	return 1, m.repayErr
}

type failingPriceExchange struct {
	signalTestExchange
	err error
}

func (e *failingPriceExchange) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	return 0, e.err
}

type failingOrderExecutor struct {
	signalTestExecutor
	err error
}

func (e *failingOrderExecutor) PlaceOrder(req *position.OrderRequest) (*position.Order, error) {
	return nil, e.err
}

func newSpotShortForTest(executor position.OrderExecutorInterface, ex position.IExchange, margin *mockMarginExchange) *SpotShortStrategy {
	cfg := &config.Config{}
	cfg.Trading.Symbol = "BTCUSDT"
	return NewSpotShortStrategy("spot_short", cfg, executor, ex, margin, map[string]interface{}{})
}

func TestSpotShortIncreaseShortRepaysOnFailure(t *testing.T) {
	errPrice := errors.New("price feed down")
	errSell := errors.New("insufficient balance")

	cases := []struct {
		name     string
		executor position.OrderExecutorInterface
		ex       position.IExchange
		wantErr  error
	}{
		{name: "price fetch fails", executor: &signalTestExecutor{}, ex: &failingPriceExchange{err: errPrice}, wantErr: errPrice},
		{name: "sell order fails", executor: &failingOrderExecutor{err: errSell}, ex: &signalTestExchange{}, wantErr: errSell},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			margin := &mockMarginExchange{}
			s := newSpotShortForTest(tc.executor, tc.ex, margin)

			err := s.increaseShort(context.Background(), 0.5)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err=%v, want wrapped %v", err, tc.wantErr)
			}
			if len(margin.borrowed) != 1 || len(margin.repaid) != 1 || margin.repaid[0] != margin.borrowed[0] {
				t.Fatalf("borrowed=%v repaid=%v, want borrowed amount repaid", margin.borrowed, margin.repaid)
			}
		})
	}
}

func TestSpotShortIncreaseShortReportsRepayFailure(t *testing.T) {
	errSell := errors.New("sell rejected")
	margin := &mockMarginExchange{repayErr: errors.New("repay rejected")}
	s := newSpotShortForTest(&failingOrderExecutor{err: errSell}, &signalTestExchange{}, margin)

	err := s.increaseShort(context.Background(), 0.5)
	if !errors.Is(err, errSell) || !strings.Contains(err.Error(), "repay rejected") {
		t.Fatalf("err=%v, want sell error with repay failure context", err)
	}
}

func TestSpotShortIncreaseShortSuccessDoesNotRepay(t *testing.T) {
	margin := &mockMarginExchange{}
	executor := &signalTestExecutor{}
	s := newSpotShortForTest(executor, &signalTestExchange{}, margin)

	if err := s.increaseShort(context.Background(), 0.5); err != nil {
		t.Fatalf("increaseShort: %v", err)
	}
	if len(margin.repaid) != 0 || len(executor.orders) != 1 || executor.orders[0].Side != "SELL" {
		t.Fatalf("repaid=%v orders=%d", margin.repaid, len(executor.orders))
	}
}
