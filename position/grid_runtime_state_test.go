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
	slot.baseFeeReconciliationRequired = true
	slot.PositionEntryOrderID, slot.PositionEntryOrderAmbiguous = 7001, false
	slot.pendingFeeSupplementCount = 2
	slot.lastFilledClientOID = "previous-cid"
	slot.lastTerminalFill = FillProgress{Quantity: 0.1, Notional: 9.9}
	slot.AvgBuyPrice, slot.AllocatedMargin = 98.5, 147.75
	slot.CostBasisUnverified = true
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
		!state.FeeValuationUnknown || !state.BaseFeeReconciliationRequired || state.CycleGen != 4 || state.PendingFeeSupplementCount != 2 || state.LastFilledClientOID != "previous-cid" ||
		state.LastTerminalFill.Quantity != .1 || state.LastTerminalFill.Notional != 9.9 || state.AvgBuyPrice != 98.5 ||
		!state.CostBasisUnverified || state.AllocatedMargin != 147.75 || state.PositionEntryOrderID != 7001 || state.PositionEntryOrderUnknown {
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
	if !restored.OpeningGate().HasBlock("unknown_orders") {
		t.Fatal("restored base-fee reconciliation latch did not block opening")
	}
	restoredSlot, ok := restored.slots.Load(99.0)
	if !ok {
		t.Fatal("restored slot not found")
	}
	restoredSlot.(*InventorySlot).mu.RLock()
	defer restoredSlot.(*InventorySlot).mu.RUnlock()
	if restoredSlot.(*InventorySlot).PositionQty != 1.5 || restoredSlot.(*InventorySlot).feeClientOID != "owned-cid" ||
		!restoredSlot.(*InventorySlot).baseFeeReconciliationRequired ||
		restoredSlot.(*InventorySlot).lastTerminalFill.Quantity != .1 || restoredSlot.(*InventorySlot).pendingFeeSupplementCount != 2 ||
		restoredSlot.(*InventorySlot).PositionEntryOrderID != 7001 || restoredSlot.(*InventorySlot).PositionEntryOrderAmbiguous ||
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

func TestCompleteReconciliationClearsOnlyVerifiedRuntimeRestoreHold(t *testing.T) {
	spm, _ := newStateTestSPM("LONG", "futures")
	spm.OpeningGate().Block("grid_runtime_state_reconciliation")
	spm.OpeningGate().Block("unknown_orders")
	spm.OpeningGate().Block("operator_pause")

	spm.CompleteReconciliation()

	if spm.OpeningGate().HasBlock("grid_runtime_state_reconciliation") {
		t.Fatal("successful complete reconciliation left the restored-runtime hold active")
	}
	if !spm.OpeningGate().HasBlock("unknown_orders") || !spm.OpeningGate().HasBlock("operator_pause") {
		t.Fatal("complete reconciliation cleared an unrelated opening hold")
	}
}

func TestCompleteReconciliationPersistsBeforeClearingRuntimeStateFailureHold(t *testing.T) {
	spm, _ := newStateTestSPM("LONG", "futures")
	spm.botID = "bot-persist-recovery"
	spm.setAnchorPrice(100)
	store := &gridRuntimeStateTestStore{err: errors.New("temporary storage failure")}
	spm.SetGridRuntimeStateStore(store)
	spm.OpeningGate().Block("grid_runtime_state_unverified")
	spm.OpeningGate().Block("unknown_orders")

	spm.CompleteReconciliation()
	if !spm.OpeningGate().HasBlock("grid_runtime_state_unverified") {
		t.Fatal("runtime-state persistence failure hold cleared despite failed durable checkpoint")
	}

	store.err = nil
	spm.CompleteReconciliation()
	if spm.OpeningGate().HasBlock("grid_runtime_state_unverified") || !store.found {
		t.Fatal("runtime-state hold did not clear after a successful durable checkpoint")
	}
	if !spm.OpeningGate().HasBlock("unknown_orders") {
		t.Fatal("durable checkpoint cleared unrelated UNKNOWN-order hold")
	}
}

func TestRestoreGridRuntimeStateMigratesUnprovenLegacyCostBasis(t *testing.T) {
	spm, _ := newStateTestSPM("LONG", "futures")
	spm.botID = "bot-migrate"
	legacy := gridRuntimeStateSnapshot{
		Version: 2, BotID: "bot-migrate", Exchange: "binance", MarketType: "futures",
		Symbol: "BTCUSDT", Direction: "LONG", AnchorPrice: 100,
		Slots: []gridRuntimeSlotSnapshot{{
			Price: 110, PositionStatus: PositionStatusFilled, PositionQty: 0.25,
			OrderStatus: OrderStatusNotPlaced, SlotStatus: SlotStatusFree, AvgBuyPrice: 110,
		}},
	}
	payload, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	store := &gridRuntimeStateTestStore{version: 2, payload: string(payload), found: true}
	spm.SetGridRuntimeStateStore(store)
	if restored, err := spm.RestoreGridRuntimeState(); err != nil || !restored {
		t.Fatalf("legacy restore: restored=%v err=%v", restored, err)
	}
	if store.version != gridRuntimeStateSchemaVersion {
		t.Fatalf("migrated snapshot version = %d, want %d", store.version, gridRuntimeStateSchemaVersion)
	}
	slot := spm.getOrCreateSlot(110)
	slot.mu.RLock()
	defer slot.mu.RUnlock()
	if !slot.CostBasisUnverified || !slot.PositionEntryOrderAmbiguous || slot.PositionEntryOrderID != 0 || !spm.OpeningGate().HasBlock("grid_cost_basis_unverified") {
		t.Fatal("legacy position without terminal fill evidence was trusted")
	}
}

func TestRestoreGridRuntimeStateMigratesUnknownSpotSellToFeeReconciliationLatch(t *testing.T) {
	spm, _ := newStateTestSPM("LONG", "spot")
	spm.botID = "bot-v4-spot-unknown"
	snapshot := gridRuntimeStateSnapshot{
		Version: 4, BotID: spm.botID, Exchange: "binance", MarketType: "spot", Symbol: "BTCUSDT", Direction: "LONG", AnchorPrice: 100,
		Slots: []gridRuntimeSlotSnapshot{{Price: 100, PositionStatus: PositionStatusFilled, PositionQty: 0.5,
			OrderID: 88, ClientOID: "sell-88", OrderSide: "SELL", OrderStatus: OrderStatusUnknown,
			OrderPrice: 110, SlotStatus: SlotStatusLocked, AvgBuyPrice: 90}},
	}
	payload, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	store := &gridRuntimeStateTestStore{version: 4, payload: string(payload), found: true}
	spm.SetGridRuntimeStateStore(store)
	if restored, err := spm.RestoreGridRuntimeState(); err != nil || !restored {
		t.Fatalf("restore v4 spot unknown state: restored=%v err=%v", restored, err)
	}
	if store.version != gridRuntimeStateSchemaVersion || !spm.OpeningGate().HasBlock("unknown_orders") {
		t.Fatalf("v4 migration failed to persist/activate reconciliation latch: version=%d blocks=%v", store.version, spm.OpeningGate().Sources())
	}
	slot := spm.getOrCreateSlot(100)
	slot.mu.RLock()
	defer slot.mu.RUnlock()
	if !slot.baseFeeReconciliationRequired {
		t.Fatal("v4 unknown spot SELL lost conservative fee-reconciliation latch")
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
