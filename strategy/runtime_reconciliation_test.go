package strategy

import (
	"context"
	"testing"
	"time"
)

func TestRuntimeReconciliationChecksPendingOrdersOnly(t *testing.T) {
	dca := &DCAEnhancedStrategy{layers: []*DCALayer{{Status: entryStatusPending}}}
	if !dca.hasPendingDCAOrderReconciliation() {
		t.Fatal("DCA pending entry should be reconciled")
	}
	dca.layers[0].Status = "CANCELED"
	if dca.hasPendingDCAOrderReconciliation() {
		t.Fatal("terminal DCA entry should not be reconciled")
	}

	martingale := &MartingaleStrategy{entries: []*MartingaleEntry{{Status: entryStatusPartiallyFilled}}}
	if !martingale.hasPendingMartingaleEntryReconciliation() {
		t.Fatal("partially-filled martingale entry should be reconciled")
	}
	martingale.entries[0].Status = entryStatusFilled
	if martingale.hasPendingMartingaleEntryReconciliation() {
		t.Fatal("terminal martingale entry should not be reconciled")
	}
	if martingale.hasPendingMartingaleCloseReconciliation() {
		t.Fatal("martingale without a close intent should not reconcile a close")
	}
	martingale.closeClientOrderID = "close-1"
	if !martingale.hasPendingMartingaleCloseReconciliation() {
		t.Fatal("persisted martingale close intent should be reconciled")
	}
	spotShort := &SpotShortStrategy{pendingBorrow: map[string]spotShortPendingBorrow{"borrow": {Amount: 1}},
		pendingRepay: map[int64]spotShortPendingRepay{}, pendingBuy: map[string]spotShortPendingBuy{}}
	if !spotShort.hasPendingRuntimeReconciliation() {
		t.Fatal("spot short borrow intent should be reconciled")
	}
	spotShort.pendingBorrow = nil
	spotShort.pendingBuy = map[string]spotShortPendingBuy{"buy": {Quantity: 1}}
	if !spotShort.hasPendingRuntimeReconciliation() {
		t.Fatal("spot short buy intent should be reconciled")
	}
	spotShort.pendingBuy = nil
	spotShort.pendingRepay = map[int64]spotShortPendingRepay{1: {OrderQuantity: 1}}
	if !spotShort.hasPendingRuntimeReconciliation() {
		t.Fatal("spot short repayment intent should be reconciled")
	}
}

func TestRuntimeOrderReconciliationStopsWithContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	finished := make(chan struct{})
	dca := &DCAEnhancedStrategy{}
	go func() {
		dca.runOrderReconciliation(ctx)
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("DCA reconciliation loop did not stop after context cancellation")
	}

	finished = make(chan struct{})
	martingale := &MartingaleStrategy{}
	go func() {
		martingale.runEntryOrderReconciliation(ctx)
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("martingale reconciliation loop did not stop after context cancellation")
	}

	finished = make(chan struct{})
	spotShort := &SpotShortStrategy{}
	go func() {
		spotShort.runRuntimeReconciliation(ctx)
		close(finished)
	}()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("spot short reconciliation loop did not stop after context cancellation")
	}
}
