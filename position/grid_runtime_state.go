package position

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"strings"
)

const gridRuntimeStateName = "grid"

// PersistGridRuntimeState stores the full in-memory grid accounting cursor so
// a later recovery workflow can compare it with venue evidence. It deliberately
// does not settle execution intents or authorize startup by itself.
func (spm *SuperPositionManager) PersistGridRuntimeState() error {
	spm.gridRuntimeStateSaveMu.Lock()
	defer spm.gridRuntimeStateSaveMu.Unlock()
	spm.gridRuntimeStateMu.RLock()
	store := spm.gridRuntimeStateStore
	spm.gridRuntimeStateMu.RUnlock()
	if store == nil {
		return nil
	}

	snapshot := gridRuntimeStateSnapshot{
		Version:      gridRuntimeStateSchemaVersion,
		BotID:        spm.botID,
		Exchange:     spm.exchangeName,
		MarketType:   spm.config.Trading.MarketType,
		Symbol:       spm.config.Trading.Symbol,
		Direction:    spm.config.Trading.Direction,
		AnchorPrice:  spm.anchorPrice(),
		TotalBuyQty:  spm.totalBuyQty.Load().(float64),
		TotalSellQty: spm.totalSellQty.Load().(float64),
	}
	if market, ok := spm.lastMarketPrice.Load().(float64); ok {
		snapshot.LastMarket = market
	}
	spm.slots.Range(func(_, value any) bool {
		slot := value.(*InventorySlot)
		slot.mu.RLock()
		snapshot.Slots = append(snapshot.Slots, gridRuntimeSlotSnapshot{
			Price: slot.Price, PositionStatus: slot.PositionStatus, PositionQty: slot.PositionQty,
			OrderID: slot.OrderID, ClientOID: slot.ClientOID, OrderSide: slot.OrderSide, OrderStatus: slot.OrderStatus,
			OrderPrice: slot.OrderPrice, OrderFilledQty: slot.OrderFilledQty, OrderFilledNotional: slot.OrderFilledNotional,
			OrderCreatedAt: slot.OrderCreatedAt, SlotStatus: slot.SlotStatus, PostOnlyFailCount: slot.PostOnlyFailCount,
			BuyFee: slot.BuyFee, FeeAsset: slot.FeeAsset, FeeClientOID: slot.feeClientOID,
			OrderCommission: slot.orderCommission, FeeValuationUnknown: slot.feeValuationUnknown,
			OrderBaseFeeQty: slot.orderBaseFeeQty, CycleGen: slot.cycleGen, FeeSupplementUntil: slot.feeSupplementUntil,
			LastFilledClientOID: slot.lastFilledClientOID, LastTerminalFill: slot.lastTerminalFill,
			BaseFeeUnfloored: slot.baseFeeUnfloored, AvgBuyPrice: slot.AvgBuyPrice,
			AllocatedMargin: slot.AllocatedMargin, PositionLeg: slot.PositionLeg,
			StrategyName: slot.StrategyName, StrategyType: slot.StrategyType,
		})
		slot.mu.RUnlock()
		return true
	})
	sort.Slice(snapshot.Slots, func(i, j int) bool { return snapshot.Slots[i].Price < snapshot.Slots[j].Price })
	if err := validateGridRuntimeSnapshot(snapshot); err != nil {
		return err
	}
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("encode grid runtime state: %w", err)
	}
	if err := store.SaveRuntimeState(gridRuntimeStateName, gridRuntimeStateSchemaVersion, string(payload)); err != nil {
		return fmt.Errorf("persist grid runtime state for %s/%s: %w", spm.botID, spm.config.Trading.Symbol, err)
	}
	return nil
}

// RestoreGridRuntimeState loads a strictly scoped snapshot before grid
// initialization. Callers must still reconcile venue positions, open orders,
// and execution intents before opening is allowed.
func (spm *SuperPositionManager) RestoreGridRuntimeState() (bool, error) {
	spm.gridRuntimeStateMu.RLock()
	store := spm.gridRuntimeStateStore
	spm.gridRuntimeStateMu.RUnlock()
	if store == nil {
		return false, fmt.Errorf("grid runtime state storage is unavailable")
	}
	schemaVersion, payload, found, err := store.LoadRuntimeState(gridRuntimeStateName)
	if err != nil || !found {
		return found, err
	}
	var snapshot gridRuntimeStateSnapshot
	if err := json.Unmarshal([]byte(payload), &snapshot); err != nil {
		return true, fmt.Errorf("decode grid runtime state: %w", err)
	}
	if schemaVersion != snapshot.Version {
		return true, fmt.Errorf("grid runtime state storage/payload schema mismatch: %d/%d", schemaVersion, snapshot.Version)
	}
	if err := validateGridRuntimeSnapshot(snapshot); err != nil {
		return true, err
	}
	if snapshot.BotID != spm.botID || !strings.EqualFold(snapshot.Exchange, spm.exchangeName) ||
		!strings.EqualFold(snapshot.MarketType, spm.config.Trading.MarketType) ||
		!strings.EqualFold(snapshot.Symbol, spm.config.Trading.Symbol) ||
		!strings.EqualFold(snapshot.Direction, spm.config.Trading.Direction) {
		return true, fmt.Errorf("grid runtime snapshot owner/configuration mismatch")
	}
	if spm.isInitialized.Load() {
		return true, fmt.Errorf("cannot restore grid runtime state after initialization")
	}
	if err := spm.applyGridRuntimeSnapshot(snapshot); err != nil {
		return true, err
	}
	spm.gridRuntimeStateRestored.Store(true)
	return true, nil
}

func (spm *SuperPositionManager) applyGridRuntimeSnapshot(snapshot gridRuntimeStateSnapshot) error {
	hasExistingSlots := false
	spm.slots.Range(func(_, _ any) bool {
		hasExistingSlots = true
		return false
	})
	if hasExistingSlots {
		return fmt.Errorf("cannot restore grid runtime state after slots were created")
	}
	for _, state := range snapshot.Slots {
		slot := &InventorySlot{
			Price: state.Price, PositionStatus: state.PositionStatus, PositionQty: state.PositionQty,
			OrderID: state.OrderID, ClientOID: state.ClientOID, OrderSide: state.OrderSide, OrderStatus: state.OrderStatus,
			OrderPrice: state.OrderPrice, OrderFilledQty: state.OrderFilledQty, OrderFilledNotional: state.OrderFilledNotional,
			OrderCreatedAt: state.OrderCreatedAt, SlotStatus: state.SlotStatus, PostOnlyFailCount: state.PostOnlyFailCount,
			BuyFee: state.BuyFee, FeeAsset: state.FeeAsset, feeClientOID: state.FeeClientOID,
			orderCommission: state.OrderCommission, feeValuationUnknown: state.FeeValuationUnknown,
			orderBaseFeeQty: state.OrderBaseFeeQty, cycleGen: state.CycleGen, feeSupplementUntil: state.FeeSupplementUntil,
			lastFilledClientOID: state.LastFilledClientOID, lastTerminalFill: state.LastTerminalFill,
			baseFeeUnfloored: state.BaseFeeUnfloored, AvgBuyPrice: state.AvgBuyPrice,
			AllocatedMargin: state.AllocatedMargin, PositionLeg: state.PositionLeg,
			StrategyName: state.StrategyName, StrategyType: state.StrategyType,
		}
		spm.slots.Store(state.Price, slot)
	}
	spm.setAnchorPrice(snapshot.AnchorPrice)
	spm.lastMarketPrice.Store(snapshot.LastMarket)
	spm.totalBuyQty.Store(snapshot.TotalBuyQty)
	spm.totalSellQty.Store(snapshot.TotalSellQty)
	return nil
}

// GridRuntimeStateIsVerifiedEmpty permits only the existing empty-account
// bootstrap path. Any persisted inventory, active order, cost basis, or pending
// fee work requires full venue reconciliation before trading can resume.
func (spm *SuperPositionManager) GridRuntimeStateIsVerifiedEmpty() bool {
	if !spm.gridRuntimeStateRestored.Load() {
		return false
	}
	empty := true
	spm.slots.Range(func(_, value any) bool {
		slot := value.(*InventorySlot)
		slot.mu.RLock()
		clear := slot.PositionStatus == PositionStatusEmpty && slot.PositionQty == 0 &&
			slot.SlotStatus == SlotStatusFree &&
			slot.OrderID == 0 && slot.ClientOID == "" && slot.OrderFilledQty == 0 && slot.OrderFilledNotional == 0 &&
			(slot.OrderStatus == OrderStatusNotPlaced || slot.OrderStatus == OrderStatusCanceled) &&
			slot.BuyFee == 0 && slot.AllocatedMargin == 0 && slot.AvgBuyPrice == 0 &&
			slot.orderCommission == 0 && slot.orderBaseFeeQty == 0 && !slot.feeValuationUnknown &&
			slot.feeSupplementUntil.IsZero() && !slot.baseFeeUnfloored && slot.PositionLeg == PositionLegNone
		slot.mu.RUnlock()
		if !clear {
			empty = false
			return false
		}
		return true
	})
	return empty
}

func validateGridRuntimeSnapshot(snapshot gridRuntimeStateSnapshot) error {
	if snapshot.Version != gridRuntimeStateSchemaVersion || snapshot.BotID == "" || snapshot.Exchange == "" ||
		snapshot.MarketType == "" || snapshot.Symbol == "" || snapshot.Direction == "" {
		return fmt.Errorf("grid runtime snapshot has incomplete owner or schema identity")
	}
	if !finiteGridValue(snapshot.AnchorPrice) || snapshot.AnchorPrice <= 0 || !finiteGridValue(snapshot.LastMarket) ||
		!finiteGridValue(snapshot.TotalBuyQty) || !finiteGridValue(snapshot.TotalSellQty) ||
		snapshot.TotalBuyQty < 0 || snapshot.TotalSellQty < 0 {
		return fmt.Errorf("grid runtime snapshot contains invalid aggregate state")
	}
	for i, slot := range snapshot.Slots {
		values := []float64{slot.Price, slot.PositionQty, slot.OrderPrice, slot.OrderFilledQty,
			slot.OrderFilledNotional, slot.BuyFee, slot.OrderCommission, slot.OrderBaseFeeQty,
			slot.LastTerminalFill.Quantity, slot.LastTerminalFill.Notional, slot.AvgBuyPrice, slot.AllocatedMargin}
		for _, value := range values {
			if !finiteGridValue(value) {
				return fmt.Errorf("grid runtime snapshot slot %d contains non-finite economics", i)
			}
		}
		if slot.Price <= 0 || slot.PositionQty < 0 || slot.OrderFilledQty < 0 || slot.OrderFilledNotional < 0 ||
			slot.OrderBaseFeeQty < 0 || slot.PostOnlyFailCount < 0 || (i > 0 && snapshot.Slots[i-1].Price >= slot.Price) {
			return fmt.Errorf("grid runtime snapshot slot %d has invalid or duplicate identity/state", i)
		}
		if !validGridPositionStatus(slot.PositionStatus) || !validGridOrderStatus(slot.OrderStatus) ||
			!validGridSlotStatus(slot.SlotStatus) || (slot.OrderSide != "" && slot.OrderSide != "BUY" && slot.OrderSide != "SELL") ||
			(slot.PositionLeg != "" && slot.PositionLeg != PositionLegLong && slot.PositionLeg != PositionLegShort) {
			return fmt.Errorf("grid runtime snapshot slot %d has invalid status or side", i)
		}
	}
	return nil
}

func finiteGridValue(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }

func validGridPositionStatus(status string) bool {
	return status == PositionStatusEmpty || status == PositionStatusFilled
}

func validGridSlotStatus(status string) bool {
	return status == SlotStatusFree || status == SlotStatusPending || status == SlotStatusLocked
}

func validGridOrderStatus(status string) bool {
	switch status {
	case OrderStatusNotPlaced, OrderStatusPlaced, OrderStatusConfirmed, OrderStatusPartiallyFilled,
		OrderStatusFilled, OrderStatusCancelRequested, OrderStatusCanceled, OrderStatusUnknown,
		"EXPIRED", "REJECTED":
		return true
	default:
		return false
	}
}
