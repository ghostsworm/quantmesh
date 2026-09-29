package position

import (
	"encoding/json"
	"errors"
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
	payload     []byte
}

func (e *tradeLedgerHoldTestExecutor) MarkTradeLedgerRecordReconciliationRequired(_ int64, _ string, _ string, payload []byte) error {
	e.ledgerCalls++
	e.payload = append([]byte(nil), payload...)
	return nil
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
	feeHistory := &fillsExchange{fills: []*fakeFill{{Price: fillFeeTestPrice + 10, Quantity: 1, CommissionAsset: "USDT"}}}
	spm := newFillFeeSPM(t, "futures", feeHistory)
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

func TestGridTradeWriteFailurePersistsExactReplayPayload(t *testing.T) {
	feeHistory := &fillsExchange{fills: []*fakeFill{{Price: fillFeeTestPrice + 10, Quantity: 1, CommissionAsset: "USDT"}}}
	spm := newFillFeeSPM(t, "futures", feeHistory)
	executor := &tradeLedgerHoldTestExecutor{}
	spm.executor = executor
	spm.SetTradeStorage(&auditTradeRecorder{err: errors.New("injected trade database failure")})
	fillSlot(spm, fillFeeTestPrice, 1, fillFeeTestPrice, "")
	cid := spm.generateClientOrderID(fillFeeTestPrice, "SELL", "")
	spm.OnOrderUpdate(OrderUpdate{OrderID: 908, ClientOrderID: cid, Symbol: "ETHUSDT", Status: "NEW", Side: "SELL", Price: fillFeeTestPrice + 10})
	spm.OnOrderUpdate(OrderUpdate{OrderID: 908, ClientOrderID: cid, Symbol: "ETHUSDT", Status: "FILLED", Side: "SELL", ExecutedQty: 1, AvgPrice: fillFeeTestPrice + 10})
	if executor.ledgerCalls != 1 || len(executor.payload) == 0 {
		t.Fatalf("trade write failure did not persist a replay payload: ledger_calls=%d payload=%s", executor.ledgerCalls, executor.payload)
	}
	var saved storage.Trade
	if err := json.Unmarshal(executor.payload, &saved); err != nil {
		t.Fatalf("decode replay payload: %v", err)
	}
	if saved.SellOrderID != 908 || saved.Quantity != 1 || saved.ExecutionKey == "" || saved.Symbol != "ETHUSDT" {
		t.Fatalf("replay payload lost realized trade economics or identity: %+v", saved)
	}
	if !spm.OpeningGate().HasBlock("trade_ledger_unverified") {
		t.Fatal("trade ledger failure must continue blocking new openings")
	}
}

func TestGridCloseOverfillKeepsInventoryAndPersistsReconciliationHold(t *testing.T) {
	for _, test := range []struct {
		name, direction, side string
	}{
		{name: "long sell close", direction: "LONG", side: "SELL"},
		{name: "short buy close", direction: "SHORT", side: "BUY"},
	} {
		t.Run(test.name, func(t *testing.T) {
			executor := &tradeLedgerHoldTestExecutor{}
			spm := newDirectionTestSPM(t, test.direction, executor)
			trades := &auditTradeRecorder{}
			spm.SetTradeStorage(trades)
			slot := fillSlot(spm, 100, 0.5, 100, "")
			cid := spm.generateClientOrderID(100, test.side, "")
			spm.OnOrderUpdate(OrderUpdate{OrderID: 909, ClientOrderID: cid, Symbol: "BTCUSDT", Status: "NEW", Side: test.side, Price: 90})
			spm.OnOrderUpdate(OrderUpdate{OrderID: 909, ClientOrderID: cid, Symbol: "BTCUSDT", Status: "FILLED", Side: test.side,
				ExecutedQty: 1, AvgPrice: 90, Commission: 0.01, CommissionAsset: "USDT"})

			if len(trades.trades) != 0 {
				t.Fatalf("over-close persisted a realized trade: %+v", trades.trades)
			}
			if executor.orderCalls != 1 {
				t.Fatalf("reconciliation holds=%d, want 1", executor.orderCalls)
			}
			if !spm.OpeningGate().HasBlock("unknown_orders") {
				t.Fatal("over-close did not block new openings")
			}
			slot.mu.RLock()
			defer slot.mu.RUnlock()
			if slot.PositionQty != 0.5 || slot.OrderFilledQty != 0 || slot.PositionStatus != PositionStatusFilled || slot.OrderStatus != OrderStatusUnknown || slot.SlotStatus != SlotStatusLocked {
				t.Fatalf("over-close mutated inventory/order state: qty=%v filled=%v position=%s order=%s slot=%s", slot.PositionQty, slot.OrderFilledQty, slot.PositionStatus, slot.OrderStatus, slot.SlotStatus)
			}
		})
	}
}
