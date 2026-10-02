package strategy

import (
	"context"
	"errors"
	"testing"
	"time"

	"quantmesh/position"
)

type spotShortCancelableExecutor struct {
	signalTestExecutor
	entered chan struct{}
}

func (*spotShortCancelableExecutor) PlaceOrder(*position.OrderRequest) (*position.Order, error) {
	return nil, errors.New("legacy placement detached the account operation context")
}

func (e *spotShortCancelableExecutor) PlaceOrderContext(ctx context.Context, _ *position.OrderRequest) (*position.Order, error) {
	e.entered <- struct{}{}
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestSpotShortCoordinatedPlacementCancellationRetainsIntent(t *testing.T) {
	for _, side := range []string{"SELL", "BUY"} {
		t.Run(side, func(t *testing.T) {
			executor := &spotShortCancelableExecutor{entered: make(chan struct{}, 1)}
			margin := &mockMarginExchange{}
			s := newSpotShortForTest(executor, &signalTestExchange{}, margin)
			if err := s.SetAccountWalletCoordinationLock(&walletCoordinationTestLock{}, "funding_carry_wallet:"+t.Name()); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := make(chan error, 1)
			go func() {
				if side == "SELL" {
					done <- s.increaseShort(ctx, 0.25)
				} else {
					done <- s.decreaseShort(ctx, 0.25)
				}
			}()
			select {
			case <-executor.entered:
			case err := <-done:
				t.Fatalf("placement did not reach context-aware executor: %v", err)
			case <-time.After(5 * time.Second):
				t.Fatal("placement did not reach context-aware executor")
			}
			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("placement lost operation cancellation: %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("placement continued after its account operation was canceled")
			}
			if side == "SELL" && (len(s.pendingBorrow) != 1 || len(margin.borrowed) != 1) {
				t.Fatalf("canceled sell lost confirmed borrow intent: %+v", s.pendingBorrow)
			}
			if side == "BUY" && len(s.pendingBuy) != 1 {
				t.Fatalf("canceled buy lost durable order intent: %+v", s.pendingBuy)
			}
		})
	}
}

func TestSpotShortCanceledAfterBorrowDoesNotSubmitSell(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	executor := &spotShortCancelableExecutor{entered: make(chan struct{}, 1)}
	margin := &mockMarginExchange{beforeBorrow: cancel}
	s := newSpotShortForTest(executor, &signalTestExchange{}, margin)
	if err := s.SetAccountWalletCoordinationLock(&walletCoordinationTestLock{}, "funding_carry_wallet:"+t.Name()); err != nil {
		t.Fatal(err)
	}
	if err := s.increaseShort(ctx, 0.25); !errors.Is(err, context.Canceled) {
		t.Fatalf("expected canceled sell after confirmed borrow: %v", err)
	}
	if len(executor.entered) != 0 || len(s.pendingBorrow) != 1 {
		t.Fatalf("canceled operation submitted sell or lost borrow: entered=%d pending=%v", len(executor.entered), s.pendingBorrow)
	}
	for _, intent := range s.pendingBorrow {
		if intent.Phase != "borrowed" || intent.BorrowTransferID <= 0 {
			t.Fatalf("confirmed borrow identity was lost: %+v", intent)
		}
	}
}

func TestSpotShortCoordinatedBorrowRejectsExecutorWithoutContext(t *testing.T) {
	margin := &mockMarginExchange{}
	s := newSpotShortForTest(&signalTestExecutor{}, &signalTestExchange{}, margin)
	if err := s.SetAccountWalletCoordinationLock(&walletCoordinationTestLock{}, "funding_carry_wallet:"+t.Name()); err != nil {
		t.Fatal(err)
	}
	if err := s.increaseShort(context.Background(), 0.25); err == nil {
		t.Fatal("borrow proceeded without a cancellable sell executor")
	}
	if len(margin.borrowed) != 0 || len(s.pendingBorrow) != 0 {
		t.Fatal("executor capability check happened after the borrowing side effect")
	}
}
