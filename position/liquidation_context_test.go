package position

import (
	"context"
	"errors"
	"testing"
	"time"
)

type cancellationLiquidationExecutor struct {
	*liqFakeVenue
	cancel     context.CancelFunc
	stage      string
	batchCalls int
}

func (v *cancellationLiquidationExecutor) GetOrderBook(ctx context.Context, symbol string, depth int) (*OrderBook, error) {
	if v.stage == "book" {
		v.cancel()
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return v.liqFakeVenue.GetOrderBook(ctx, symbol, depth)
}

func (v *cancellationLiquidationExecutor) BatchPlaceOrdersWithDetailsContext(ctx context.Context, reqs []*OrderRequest) *BatchPlaceOrdersResult {
	v.batchCalls++
	v.cancel()
	<-ctx.Done()
	return &BatchPlaceOrdersResult{}
}

func TestLiquidationContextReachesBookAndBatchSubmission(t *testing.T) {
	for _, stage := range []string{"book", "batch"} {
		t.Run(stage, func(t *testing.T) {
			base := newLiqFakeVenue(1)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			v := &cancellationLiquidationExecutor{liqFakeVenue: base, cancel: cancel, stage: stage}
			spm, _ := newLiqTestSPM(t, "LONG", base)
			spm.executor, spm.exchange = v, v
			slot := fillSlot(spm, liqTestLast, 1, liqTestLast, "")
			err := spm.LiquidateAllVerified(ctx, base, time.Second)
			if !errors.Is(err, context.Canceled) || len(base.marketReqs) != 0 || len(base.limitReqs) != 0 || !spm.IsOpeningPaused() {
				t.Fatalf("cancelled protective flow submitted or reported success: %v", err)
			}
			if slot.SlotStatus == SlotStatusPending || slot.PositionQty != 1 {
				t.Fatal("unsent request left pending slot or lost inventory")
			}
			if stage == "book" && v.batchCalls != 0 {
				t.Fatal("continued submission after book cancellation")
			}
		})
	}
}

func TestLiquidationCannotFallBackToNonContextExecutor(t *testing.T) {
	base := newLiqFakeVenue(1)
	spm, _ := newLiqTestSPM(t, "LONG", base)
	legacy := &MockExecutor{}
	spm.executor = legacy
	fillSlot(spm, liqTestLast, 1, liqTestLast, "")
	if err := spm.LiquidateAllVerified(t.Context(), base, time.Second); err == nil {
		t.Fatal("unbounded executor accepted for verified liquidation")
	}
	if len(legacy.PlacedOrders) != 0 || len(base.marketReqs) != 0 {
		t.Fatal("missing context capability caused untracked submission")
	}
}
