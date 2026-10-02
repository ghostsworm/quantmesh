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
}
