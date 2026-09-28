package position

import (
	"encoding/json"
	"errors"
	"math"
	"testing"
)

type gridRuntimeStateTestStore struct {
	version int
	payload string
	err     error
}

func (s *gridRuntimeStateTestStore) LoadRuntimeState(string) (int, string, bool, error) {
	return 0, "", false, nil
}

func (s *gridRuntimeStateTestStore) SaveRuntimeState(_ string, version int, payload string) error {
	s.version, s.payload = version, payload
	return s.err
}

func TestPersistGridRuntimeStateCapturesCompleteSlotAccountingCursor(t *testing.T) {
	spm, _ := newStateTestSPM("LONG", "futures")
	spm.botID = "bot-1"
	spm.setAnchorPrice(100)
	spm.totalBuyQty.Store(2.0)
	spm.totalSellQty.Store(0.5)
	spm.lastMarketPrice.Store(101.0)
	slot := spm.getOrCreateSlot(99)
	slot.mu.Lock()
	slot.PositionStatus, slot.PositionQty = PositionStatusFilled, 1.5
	slot.OrderID, slot.ClientOID, slot.OrderSide = 7, "owned-cid", "SELL"
	slot.OrderStatus, slot.OrderPrice = OrderStatusPartiallyFilled, 102
	slot.OrderFilledQty, slot.OrderFilledNotional = 0.25, 25.5
	slot.BuyFee, slot.FeeAsset = 0.03, "USDT"
	slot.feeClientOID, slot.orderCommission, slot.orderBaseFeeQty = "owned-cid", 0.01, 0.0001
	slot.feeValuationUnknown, slot.cycleGen = true, 4
	slot.lastFilledClientOID = "previous-cid"
	slot.lastTerminalFill = FillProgress{Quantity: 0.1, Notional: 9.9}
	slot.AvgBuyPrice, slot.AllocatedMargin = 98.5, 147.75
	slot.PositionLeg, slot.StrategyName, slot.StrategyType = PositionLegLong, "Grid-BTCUSDT", "grid"
	slot.mu.Unlock()
	store := &gridRuntimeStateTestStore{}
	spm.SetGridRuntimeStateStore(store)

	if err := spm.PersistGridRuntimeState(); err != nil {
		t.Fatal(err)
	}
	if store.version != gridRuntimeStateSchemaVersion {
		t.Fatalf("schema version = %d", store.version)
	}
	var got gridRuntimeStateSnapshot
	if err := json.Unmarshal([]byte(store.payload), &got); err != nil {
		t.Fatal(err)
	}
	if got.BotID != "bot-1" || got.Exchange != "binance" || got.MarketType != "futures" || got.Symbol != "BTCUSDT" ||
		got.AnchorPrice != 100 || got.TotalBuyQty != 2 || got.TotalSellQty != .5 || len(got.Slots) != 1 {
		t.Fatalf("snapshot scope/aggregate mismatch: %+v", got)
	}
	state := got.Slots[0]
	if state.PositionQty != 1.5 || state.ClientOID != "owned-cid" || state.OrderFilledQty != .25 || state.OrderFilledNotional != 25.5 ||
		state.FeeClientOID != "owned-cid" || state.OrderCommission != .01 || state.OrderBaseFeeQty != .0001 ||
		!state.FeeValuationUnknown || state.CycleGen != 4 || state.LastFilledClientOID != "previous-cid" ||
		state.LastTerminalFill.Quantity != .1 || state.LastTerminalFill.Notional != 9.9 || state.AvgBuyPrice != 98.5 || state.AllocatedMargin != 147.75 {
		t.Fatalf("snapshot lost grid accounting cursor: %+v", state)
	}
}

func TestPersistGridRuntimeStateRejectsNonFiniteEconomics(t *testing.T) {
	spm, _ := newStateTestSPM("LONG", "futures")
	spm.totalBuyQty.Store(math.NaN())
	store := &gridRuntimeStateTestStore{}
	spm.SetGridRuntimeStateStore(store)
	if err := spm.PersistGridRuntimeState(); err == nil {
		t.Fatal("non-finite total quantity must not be persisted")
	}
	if store.payload != "" {
		t.Fatal("invalid snapshot reached storage")
	}
}

type gridRuntimeStateHoldExecutor struct {
	MockExecutor
	marked int
}

func (e *gridRuntimeStateHoldExecutor) MarkOrderReconciliationRequired(int64, string, string) error {
	e.marked++
	return nil
}

func TestGridSnapshotPersistenceFailureBlocksOpeningsAndPersistsOrderHold(t *testing.T) {
	spm, _ := newStateTestSPM("LONG", "futures")
	executor := &gridRuntimeStateHoldExecutor{}
	spm.executor = executor
	spm.SetGridRuntimeStateStore(&gridRuntimeStateTestStore{err: errors.New("database unavailable")})
	cid := spm.generateClientOrderID(100, "BUY", "")
	spm.OnOrderUpdate(OrderUpdate{OrderID: 42, ClientOrderID: cid, Symbol: "BTCUSDT", Side: "BUY", Status: "NEW", Price: 100})
	if !spm.OpeningGate().HasBlock("grid_runtime_state_unverified") || executor.marked != 1 {
		t.Fatalf("snapshot failure was not durably held: blocked=%v marked=%d", spm.OpeningGate().HasBlock("grid_runtime_state_unverified"), executor.marked)
	}
}
