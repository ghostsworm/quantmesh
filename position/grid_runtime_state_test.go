package position

import (
	"encoding/json"
	"errors"
	"math"
	"strings"
	"testing"
)

type gridRuntimeStateTestStore struct {
	version int
	payload string
	err     error
	found   bool
}

func (s *gridRuntimeStateTestStore) LoadRuntimeState(string) (int, string, bool, error) {
	return s.version, s.payload, s.found, s.err
}

func (s *gridRuntimeStateTestStore) SaveRuntimeState(_ string, version int, payload string) error {
	s.version, s.payload, s.found = version, payload, true
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
	slot.pendingFeeSupplementCount = 2
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
		!state.FeeValuationUnknown || state.CycleGen != 4 || state.PendingFeeSupplementCount != 2 || state.LastFilledClientOID != "previous-cid" ||
		state.LastTerminalFill.Quantity != .1 || state.LastTerminalFill.Notional != 9.9 || state.AvgBuyPrice != 98.5 || state.AllocatedMargin != 147.75 {
		t.Fatalf("snapshot lost grid accounting cursor: %+v", state)
	}

	restored, _ := newStateTestSPM("LONG", "futures")
	restored.botID = "bot-1"
	restored.SetGridRuntimeStateStore(store)
	found, err := restored.RestoreGridRuntimeState()
	if err != nil || !found || !restored.gridRuntimeStateRestored.Load() {
		t.Fatalf("restore snapshot: found=%v restored=%v err=%v", found, restored.gridRuntimeStateRestored.Load(), err)
	}
	if restored.GridRuntimeStateIsVerifiedEmpty() {
		t.Fatal("non-empty restored inventory was misclassified as a safe empty bootstrap")
	}
	restoredSlot, ok := restored.slots.Load(99.0)
	if !ok {
		t.Fatal("restored slot not found")
	}
	restoredSlot.(*InventorySlot).mu.RLock()
	defer restoredSlot.(*InventorySlot).mu.RUnlock()
	if restoredSlot.(*InventorySlot).PositionQty != 1.5 || restoredSlot.(*InventorySlot).feeClientOID != "owned-cid" ||
		restoredSlot.(*InventorySlot).lastTerminalFill.Quantity != .1 || restoredSlot.(*InventorySlot).pendingFeeSupplementCount != 2 ||
		restored.anchorPrice() != 100 {
		t.Fatalf("restored accounting state mismatch: %+v", restoredSlot.(*InventorySlot))
	}
	if err := restored.Initialize(120, "120"); err != nil {
		t.Fatal(err)
	}
	if restored.anchorPrice() != 100 || restoredSlot.(*InventorySlot).PositionQty != 1.5 {
		t.Fatal("grid initialization overwrote restored anchor or inventory cost state")
	}
}

func TestGridRuntimeStateAllowsEmptyBootstrapOnlyForEmptySnapshot(t *testing.T) {
	spm, _ := newStateTestSPM("LONG", "futures")
	spm.botID = "bot-empty"
	snapshot := gridRuntimeStateSnapshot{
		Version: gridRuntimeStateSchemaVersion, BotID: "bot-empty", Exchange: "binance",
		MarketType: "futures", Symbol: "BTCUSDT", Direction: "LONG", AnchorPrice: 100,
		Slots: []gridRuntimeSlotSnapshot{{Price: 100, PositionStatus: PositionStatusEmpty, OrderStatus: OrderStatusNotPlaced, SlotStatus: SlotStatusFree}},
	}
	payload, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	store := &gridRuntimeStateTestStore{version: gridRuntimeStateSchemaVersion, payload: string(payload), found: true}
	spm.SetGridRuntimeStateStore(store)
	if restored, err := spm.RestoreGridRuntimeState(); err != nil || !restored {
		t.Fatalf("restore empty snapshot: restored=%v err=%v", restored, err)
	}
	if !spm.GridRuntimeStateIsVerifiedEmpty() {
		t.Fatal("strictly empty grid snapshot should use authoritative empty-account bootstrap")
	}
	slot, _ := spm.slots.Load(100.0)
	slot.(*InventorySlot).mu.Lock()
	slot.(*InventorySlot).ClientOID = "pending"
	slot.(*InventorySlot).mu.Unlock()
	if spm.GridRuntimeStateIsVerifiedEmpty() {
		t.Fatal("snapshot with an order identity must remain blocked")
	}
	slot.(*InventorySlot).mu.Lock()
	slot.(*InventorySlot).ClientOID = ""
	slot.(*InventorySlot).SlotStatus = SlotStatusLocked
	slot.(*InventorySlot).mu.Unlock()
	if spm.GridRuntimeStateIsVerifiedEmpty() {
		t.Fatal("empty snapshot with a locked slot must remain blocked")
	}
	slot.(*InventorySlot).mu.Lock()
	slot.(*InventorySlot).SlotStatus = SlotStatusFree
	slot.(*InventorySlot).pendingFeeSupplementCount = 1
	slot.(*InventorySlot).mu.Unlock()
	if spm.GridRuntimeStateIsVerifiedEmpty() {
		t.Fatal("empty snapshot with pending fee accounting must remain blocked")
	}

	snapshot.Slots[0].PendingFeeSupplementCount = 1
	pendingPayload, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	restarted, _ := newStateTestSPM("LONG", "futures")
	restarted.botID = "bot-empty"
	restarted.SetGridRuntimeStateStore(&gridRuntimeStateTestStore{
		version: gridRuntimeStateSchemaVersion, payload: string(pendingPayload), found: true,
	})
	if restored, err := restarted.RestoreGridRuntimeState(); err != nil || !restored {
		t.Fatalf("restore pending fee snapshot: restored=%v err=%v", restored, err)
	}
	if restarted.GridRuntimeStateIsVerifiedEmpty() {
		t.Fatal("restart classified a persisted pending fee supplement as verified empty")
	}
}

func TestRestoreGridRuntimeStateRejectsForeignOwnerAndSchemaMismatch(t *testing.T) {
	spm, _ := newStateTestSPM("LONG", "futures")
	spm.botID = "bot-expected"
	payload, err := json.Marshal(gridRuntimeStateSnapshot{Version: gridRuntimeStateSchemaVersion, BotID: "bot-other", Exchange: "binance", MarketType: "futures", Symbol: "BTCUSDT", Direction: "LONG"})
	if err != nil {
		t.Fatal(err)
	}
	store := &gridRuntimeStateTestStore{version: gridRuntimeStateSchemaVersion, payload: string(payload), found: true}
	spm.SetGridRuntimeStateStore(store)
	if _, err := spm.RestoreGridRuntimeState(); err == nil {
		t.Fatal("foreign Bot snapshot must be rejected")
	}
	store.payload = strings.ReplaceAll(string(payload), `"bot-other"`, `"bot-expected"`)
	store.version = gridRuntimeStateSchemaVersion + 1
	if _, err := spm.RestoreGridRuntimeState(); err == nil {
		t.Fatal("storage and payload schema mismatch must be rejected")
	}
}

func TestRestoreGridRuntimeStateRejectsLegacySnapshotWithoutFeePendingEvidence(t *testing.T) {
	spm, _ := newStateTestSPM("LONG", "futures")
	spm.botID = "bot-legacy"
	legacy := gridRuntimeStateSnapshot{
		Version: 1, BotID: "bot-legacy", Exchange: "binance", MarketType: "futures",
		Symbol: "BTCUSDT", Direction: "LONG", AnchorPrice: 100,
		Slots: []gridRuntimeSlotSnapshot{{
			Price: 100, PositionStatus: PositionStatusEmpty, OrderStatus: OrderStatusNotPlaced, SlotStatus: SlotStatusFree,
		}},
	}
	payload, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	spm.SetGridRuntimeStateStore(&gridRuntimeStateTestStore{version: 1, payload: string(payload), found: true})
	if restored, err := spm.RestoreGridRuntimeState(); err == nil || !restored {
		t.Fatalf("legacy snapshot must be found and rejected: restored=%v err=%v", restored, err)
	}
	if spm.gridRuntimeStateRestored.Load() || spm.GridRuntimeStateIsVerifiedEmpty() {
		t.Fatal("legacy snapshot without pending-fee evidence was treated as restored/empty")
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
	spm.setAnchorPrice(100)
	executor := &gridRuntimeStateHoldExecutor{}
	spm.executor = executor
	spm.SetGridRuntimeStateStore(&gridRuntimeStateTestStore{err: errors.New("database unavailable")})
	cid := spm.generateClientOrderID(100, "BUY", "")
	spm.OnOrderUpdate(OrderUpdate{OrderID: 42, ClientOrderID: cid, Symbol: "BTCUSDT", Side: "BUY", Status: "NEW", Price: 100})
	if !spm.OpeningGate().HasBlock("grid_runtime_state_unverified") || executor.marked != 1 {
		t.Fatalf("snapshot failure was not durably held: blocked=%v marked=%d", spm.OpeningGate().HasBlock("grid_runtime_state_unverified"), executor.marked)
	}
}
