package position

import (
	"encoding/json"
	"fmt"
	"math"
	"sort"
)

const gridRuntimeStateName = "grid"

// PersistGridRuntimeState stores the full in-memory grid accounting cursor so
// a later recovery workflow can compare it with venue evidence. It deliberately
// does not settle execution intents or authorize startup by itself.
func (spm *SuperPositionManager) PersistGridRuntimeState() error {
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

func validateGridRuntimeSnapshot(snapshot gridRuntimeStateSnapshot) error {
	if snapshot.Version != gridRuntimeStateSchemaVersion || snapshot.BotID == "" || snapshot.Exchange == "" ||
		snapshot.MarketType == "" || snapshot.Symbol == "" || snapshot.Direction == "" {
		return fmt.Errorf("grid runtime snapshot has incomplete owner or schema identity")
	}
	if !finiteGridValue(snapshot.AnchorPrice) || !finiteGridValue(snapshot.LastMarket) ||
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
	}
	return nil
}

func finiteGridValue(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }
