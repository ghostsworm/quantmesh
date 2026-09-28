package position

import (
	"context"
	"errors"
	"testing"

	"quantmesh/config"
)

type shutdownCancelExecutor struct {
	MockExecutor
	err   error
	calls int
}

func (e *shutdownCancelExecutor) CancelOwnedShutdownOrders(context.Context) error {
	e.calls++
	return e.err
}

type shutdownCancelExchange struct {
	MockExchange
	accountWideCancels int
}

func (e *shutdownCancelExchange) CancelAllOrders(context.Context, string) error {
	e.accountWideCancels++
	return nil
}

func TestCancelAllOrdersUsesOwnedShutdownCancellation(t *testing.T) {
	for _, test := range []struct {
		name        string
		err         error
		wantBlocked bool
	}{
		{name: "verified"},
		{name: "uncertain", err: errors.New("terminal state unavailable"), wantBlocked: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.Trading.Symbol = "BTCUSDT"
			executor := &shutdownCancelExecutor{err: test.err}
			exchange := &shutdownCancelExchange{}
			spm := NewSuperPositionManager(cfg, executor, exchange, 2, 3)
			spm.CancelAllOrders()
			if executor.calls != 1 {
				t.Fatalf("owned shutdown cancel calls = %d, want 1", executor.calls)
			}
			if exchange.accountWideCancels != 0 {
				t.Fatalf("account-wide cancel was called %d times", exchange.accountWideCancels)
			}
			if got := spm.OpeningGate().HasBlock("shutdown_orders_unverified"); got != test.wantBlocked {
				t.Fatalf("shutdown block = %v, want %v", got, test.wantBlocked)
			}
		})
	}
}
