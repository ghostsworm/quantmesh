package position

import (
	"testing"

	"quantmesh/storage"
)

func TestGridLedgerRejectsStorageWithoutIdempotentWriter(t *testing.T) {
	spm := &SuperPositionManager{}
	err := spm.saveGridTradeIdempotently(&storage.Trade{ExecutionKey: "grid-close-1"})
	if err == nil {
		t.Fatal("grid ledger must reject storage without idempotent writes")
	}
}

type tradeLedgerHoldTestExecutor struct {
	MockExecutor
	ledgerCalls int
	orderCalls  int
}

func (e *tradeLedgerHoldTestExecutor) MarkTradeLedgerReconciliationRequired(int64, string, string) error {
	e.ledgerCalls++
	return nil
}

func (e *tradeLedgerHoldTestExecutor) MarkOrderReconciliationRequired(int64, string, string) error {
	e.orderCalls++
	return nil
}

func TestGridMissingCostBasisKeepsVenueOrderKnownAndBlocksOpenings(t *testing.T) {
	spm := newFillFeeSPM(t, "futures", nil)
	executor := &tradeLedgerHoldTestExecutor{}
	spm.executor = executor
	spm.SetTradeStorage(&auditTradeRecorder{})
	slot := fillSlot(spm, fillFeeTestPrice, 1, 0, "")
	cid := spm.generateClientOrderID(fillFeeTestPrice, "SELL", "")
	spm.OnOrderUpdate(OrderUpdate{OrderID: 907, ClientOrderID: cid, Symbol: "ETHUSDT", Status: "NEW", Side: "SELL", Price: fillFeeTestPrice})
	spm.OnOrderUpdate(OrderUpdate{OrderID: 907, ClientOrderID: cid, Symbol: "ETHUSDT", Status: "FILLED", Side: "SELL", ExecutedQty: 1, AvgPrice: fillFeeTestPrice + 10})

	if executor.ledgerCalls != 1 || executor.orderCalls != 0 {
		t.Fatalf("known terminal venue result must create one economic hold, not an unknown-order hold: ledger=%d order=%d", executor.ledgerCalls, executor.orderCalls)
	}
	if !spm.OpeningGate().HasBlock("trade_ledger_unverified") {
		t.Fatal("missing cost basis must block new openings")
	}
	slot.mu.RLock()
	defer slot.mu.RUnlock()
	if slot.PositionStatus != PositionStatusEmpty || slot.OrderID != 0 || slot.lastFilledClientOID != cid {
		t.Fatalf("known filled close should settle physical order state while retaining fill identity: position=%s order=%d lastCID=%q", slot.PositionStatus, slot.OrderID, slot.lastFilledClientOID)
	}
}
