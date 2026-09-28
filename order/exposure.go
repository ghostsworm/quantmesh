package order

import (
	"context"
	"fmt"
	"strings"
	"time"

	"quantmesh/execution"
	"quantmesh/logger"
)

const ExposureLimitBlock = "exposure_limit_reduction"

// SetExposureBook must run before submissions start. A book is deliberately not
// auto-seeded: zero local orders cannot prove that a restarted owner is flat.
func (oe *ExchangeOrderExecutor) SetExposureBook(book *execution.ExposureBook) {
	oe.exposureBook = book
}

func (oe *ExchangeOrderExecutor) ExposureSnapshot() *execution.ExposureSnapshot {
	if oe.exposureBook == nil {
		return nil
	}
	oe.refreshExposureMark()
	snapshot := oe.exposureBook.Snapshot(time.Now())
	return &snapshot
}

// SetExposureMarkProvider is configured before runtime submissions, like the book.
func (oe *ExchangeOrderExecutor) SetExposureMarkProvider(provider func() (float64, time.Time)) {
	oe.exposureMarkProvider = provider
}

func (oe *ExchangeOrderExecutor) refreshExposureMark() {
	if oe.exposureBook != nil && oe.exposureMarkProvider != nil {
		price, at := oe.exposureMarkProvider()
		_ = oe.ObserveExposureMark(price, at) // readiness records a rejected quote
		oe.reconcileExposureLimitBlock()
	}
}

func (oe *ExchangeOrderExecutor) exposureRequest(req *OrderRequest) (execution.ExposureRequest, error) {
	opening := oe.isOpeningOrder(req)
	leg := "LONG"
	if (opening && req.Side == "SELL") || (!opening && req.Side == "BUY") {
		leg = "SHORT"
	}
	if explicit := strings.ToUpper(strings.TrimSpace(req.PositionSide)); explicit != "" && explicit != leg {
		return execution.ExposureRequest{}, fmt.Errorf("exposure side and inventory leg disagree")
	}
	group := strings.TrimSpace(req.StrategyName)
	if group == "" {
		group = "grid"
	}
	if req.BotWideClose && (opening || req.StrategyName != "manual_close" || req.OrderSource != "liquidation") {
		return execution.ExposureRequest{}, fmt.Errorf("bot-wide inventory allocation requires an explicit managed close")
	}
	return execution.ExposureRequest{ID: req.ClientOrderID, Group: group, Lot: req.ExposureKey, Leg: leg, Opening: opening, Quantity: req.Quantity, Price: req.Price, BotWideClose: req.BotWideClose}, nil
}

func (oe *ExchangeOrderExecutor) SetExposureLimits(limits execution.ExposureLimits) error {
	if oe.exposureBook == nil {
		return nil
	}
	if err := oe.exposureBook.SetLimits(limits); err != nil {
		return err
	}
	oe.reconcileExposureLimitBlock()
	return nil
}

func (oe *ExchangeOrderExecutor) reconcileExposureLimitBlock() {
	if oe.exposureBook == nil || oe.openingGate == nil {
		return
	}
	if !oe.exposureBook.OverLimits() {
		oe.openingGate.Unblock(ExposureLimitBlock)
		return
	}
	wasBlocked := oe.openingGate.HasBlock(ExposureLimitBlock)
	oe.openingGate.Block(ExposureLimitBlock)
	if !wasBlocked {
		go oe.cancelOpeningsAfterLimitReduction()
	}
}

func (oe *ExchangeOrderExecutor) cancelOpeningsAfterLimitReduction() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := oe.CancelOwnedOpeningOrders(ctx); err != nil {
		logger.ErrorCtx(oe.logCtx(), "exposure limit reduction requires reconciliation; owned opening cancellation incomplete: %v", err)
	}
	oe.reconcileExposureLimitBlock()
}

func (oe *ExchangeOrderExecutor) ObserveExposureMark(price float64, at time.Time) error {
	if oe.exposureBook == nil {
		return nil
	}
	return oe.exposureBook.ObserveMark(price, at, time.Now())
}

func (oe *ExchangeOrderExecutor) InvalidateExposure(reason string) {
	if oe.exposureBook != nil {
		oe.exposureBook.RequireReconciliation(reason)
	}
}

func (oe *ExchangeOrderExecutor) observeExposureLocked(cid string, order *Order) error {
	if oe.exposureBook == nil || order == nil {
		return nil
	}
	return oe.exposureBook.Observe(cid, execution.ExposureUpdate{CumulativeQty: order.ExecutedQty, OrderQty: order.Quantity, Price: order.Price, Status: order.Status})
}

func (oe *ExchangeOrderExecutor) markExposureUnknownLocked(cid string) {
	if oe.exposureBook != nil {
		oe.exposureBook.MarkUnknown(cid)
	}
	if oe.openingGate != nil {
		oe.openingGate.Block("unknown_orders")
	}
}
