package strategy

import (
	"encoding/json"
	"fmt"
	"math"
	"time"

	"quantmesh/position"
)

const martingaleRuntimeStateSchemaVersion = 1

type martingaleRuntimeState struct {
	BotID              string                `json:"bot_id"`
	StrategyName       string                `json:"strategy_name"`
	Symbol             string                `json:"symbol"`
	Direction          string                `json:"direction"`
	Entries            []*MartingaleEntry    `json:"entries"`
	TotalCost          float64               `json:"total_cost"`
	TotalQty           float64               `json:"total_qty"`
	AvgEntryPrice      float64               `json:"avg_entry_price"`
	CurrentLevel       int                   `json:"current_level"`
	IsPaused           bool                  `json:"is_paused"`
	IsClosing          bool                  `json:"is_closing"`
	CloseOrderID       int64                 `json:"close_order_id"`
	CloseClientOrderID string                `json:"close_client_order_id,omitempty"`
	CloseReason        string                `json:"close_reason,omitempty"`
	CloseRequestedQty  float64               `json:"close_requested_qty"`
	CloseProgress      position.FillProgress `json:"close_progress"`
	CloseRealizedPnL   float64               `json:"close_realized_pnl"`
	PendingCloseReason string                `json:"pending_close_reason,omitempty"`
	Stats              StrategyStatistics    `json:"stats"`
	UpdatedAt          time.Time             `json:"updated_at"`
}

func (s *MartingaleStrategy) SetRuntimeStateStore(store RuntimeStateStore) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runtimeStateStore = store
}

func (s *MartingaleStrategy) runtimeStateSnapshotLocked() martingaleRuntimeState {
	state := martingaleRuntimeState{
		BotID: s.effectiveBotID(), StrategyName: s.name, Symbol: s.strategyCfg.Symbol,
		Direction: s.direction, TotalCost: s.totalCost, TotalQty: s.totalQty,
		AvgEntryPrice: s.avgEntryPrice, CurrentLevel: s.currentLevel,
		IsPaused: s.isPaused, IsClosing: s.isClosing, CloseOrderID: s.closeOrderID,
		CloseClientOrderID: s.closeClientOrderID,
		CloseReason:        s.closeReason,
		CloseRequestedQty:  s.closeRequestedQty, CloseProgress: s.closeProgress,
		CloseRealizedPnL: s.closeRealizedPnL, PendingCloseReason: s.pendingCloseReason, UpdatedAt: time.Now().UTC(),
	}
	if s.stats != nil {
		state.Stats = *s.stats
	}
	for _, entry := range s.entries {
		if entry != nil {
			copyEntry := *entry
			state.Entries = append(state.Entries, &copyEntry)
		}
	}
	return state
}

func (s *MartingaleStrategy) persistRuntimeStateLocked() error {
	if s.runtimeStateStore == nil {
		s.runtimeStateErr = fmt.Errorf("martingale runtime state store is unavailable")
		return s.runtimeStateErr
	}
	payload, err := json.Marshal(s.runtimeStateSnapshotLocked())
	if err != nil {
		s.runtimeStateErr = fmt.Errorf("encode martingale runtime state: %w", err)
		return s.runtimeStateErr
	}
	if err := s.runtimeStateStore.SaveRuntimeState(s.name, martingaleRuntimeStateSchemaVersion, string(payload)); err != nil {
		s.runtimeStateErr = fmt.Errorf("persist martingale runtime state: %w", err)
		return s.runtimeStateErr
	}
	s.runtimeStateErr = nil
	return nil
}

func (s *MartingaleStrategy) restoreRuntimeState() error {
	if s.runtimeStateStore == nil {
		return nil
	}
	version, payload, found, err := s.runtimeStateStore.LoadRuntimeState(s.name)
	if err != nil {
		return fmt.Errorf("load martingale runtime state: %w", err)
	}
	if !found {
		return nil
	}
	if version != martingaleRuntimeStateSchemaVersion {
		return fmt.Errorf("unsupported martingale runtime state schema version %d", version)
	}
	var state martingaleRuntimeState
	if err := json.Unmarshal([]byte(payload), &state); err != nil {
		return fmt.Errorf("decode martingale runtime state: %w", err)
	}
	if state.BotID != s.effectiveBotID() || state.StrategyName != s.name || state.Symbol != s.strategyCfg.Symbol || state.Direction != s.direction {
		return fmt.Errorf("martingale runtime state identity mismatch")
	}
	if state.TotalCost < 0 || state.TotalQty < 0 || !finiteNumber(state.TotalCost) || !finiteNumber(state.TotalQty) || !finiteNumber(state.AvgEntryPrice) || len(state.Entries) > s.strategyCfg.MaxLevels {
		return fmt.Errorf("martingale runtime state contains invalid inventory")
	}
	if state.CurrentLevel < 0 || state.CurrentLevel > s.strategyCfg.MaxLevels ||
		state.CloseOrderID < 0 || state.CloseRequestedQty < 0 || state.CloseProgress.Quantity < 0 || state.CloseProgress.Notional < 0 ||
		!finiteNumber(state.CloseRequestedQty) || !finiteNumber(state.CloseProgress.Quantity) || !finiteNumber(state.CloseProgress.Notional) || !finiteNumber(state.CloseRealizedPnL) {
		return fmt.Errorf("martingale runtime state contains invalid close progress")
	}
	var totalQty, totalCost float64
	seenLevels := make(map[int]struct{}, len(state.Entries))
	seenActiveOrderIDs := make(map[int64]struct{}, len(state.Entries))
	for _, entry := range state.Entries {
		if entry == nil || entry.Level < 0 || entry.Level >= s.strategyCfg.MaxLevels || entry.Quantity < 0 || entry.Cost < 0 || entry.OpeningFee < 0 || entry.RequestedQuantity < 0 || entry.FillProgress.Quantity < 0 || entry.FillProgress.Notional < 0 || !finiteNumber(entry.Price) || !finiteNumber(entry.Quantity) || !finiteNumber(entry.Cost) || !finiteNumber(entry.OpeningFee) || !finiteNumber(entry.RequestedQuantity) || !finiteNumber(entry.FillProgress.Quantity) || !finiteNumber(entry.FillProgress.Notional) || entry.OrderID < 0 {
			return fmt.Errorf("martingale runtime state contains invalid entry")
		}
		if _, exists := seenLevels[entry.Level]; exists {
			return fmt.Errorf("martingale runtime state contains duplicate entry levels")
		}
		seenLevels[entry.Level] = struct{}{}
		switch entry.Status {
		case entryStatusPending, entryStatusPartiallyFilled, entryStatusFilled, position.OrderStatusUnknown:
		default:
			return fmt.Errorf("martingale runtime state contains unsupported entry status %q", entry.Status)
		}
		if entry.RequestedQuantity > 0 && (entry.Quantity > entry.RequestedQuantity+entryQtyEpsilon || entry.FillProgress.Quantity > entry.RequestedQuantity+entryQtyEpsilon) {
			return fmt.Errorf("martingale runtime state entry execution exceeds requested quantity")
		}
		if entry.Status == entryStatusPending && (entry.Quantity > 0 || entry.Cost > 0 || entry.FillProgress.Quantity > 0) {
			return fmt.Errorf("martingale pending entry contains attributed fills")
		}
		if entry.Status == entryStatusPartiallyFilled && (entry.Quantity <= 0 || entry.FillProgress.Quantity <= 0) {
			return fmt.Errorf("martingale partially filled entry is missing fill progress")
		}
		if (entry.Status == entryStatusPending || entry.Status == entryStatusPartiallyFilled) && entry.OrderID > 0 {
			if _, exists := seenActiveOrderIDs[entry.OrderID]; exists {
				return fmt.Errorf("martingale runtime state contains duplicate active entry order IDs")
			}
			seenActiveOrderIDs[entry.OrderID] = struct{}{}
		}
		if martingaleEntryHasAttributedFill(entry) {
			totalQty += entry.Quantity
			totalCost += entry.Cost
		}
	}
	if math.Abs(totalQty-state.TotalQty) > entryQtyEpsilon || math.Abs(totalCost-state.TotalCost) > math.Max(1e-8, math.Abs(state.TotalCost)*1e-8) {
		return fmt.Errorf("martingale runtime state inventory totals do not reconcile")
	}
	if state.TotalQty == 0 {
		if state.AvgEntryPrice != 0 {
			return fmt.Errorf("martingale runtime state has an average entry price without inventory")
		}
	} else {
		expectedAverage := state.TotalCost / state.TotalQty
		if expectedAverage <= 0 || math.Abs(state.AvgEntryPrice-expectedAverage) > math.Max(1e-8, math.Abs(expectedAverage)*1e-8) {
			return fmt.Errorf("martingale runtime state average entry price does not match inventory cost")
		}
	}
	if state.IsClosing && state.CloseOrderID <= 0 {
		return fmt.Errorf("martingale close state is missing its order identity")
	}
	if state.CloseClientOrderID != "" && (state.CloseRequestedQty <= 0 || state.PendingCloseReason != "" || state.CloseReason == "") {
		return fmt.Errorf("martingale close submission intent is inconsistent")
	}
	if state.CloseClientOrderID == "" && state.CloseReason != "" {
		return fmt.Errorf("martingale runtime state has a close reason without a close order identity")
	}
	if state.IsClosing && (state.CloseRequestedQty <= 0 || state.CloseProgress.Quantity > state.CloseRequestedQty+entryQtyEpsilon || state.CloseProgress.Notional < 0) {
		return fmt.Errorf("martingale close state contains inconsistent execution progress")
	}
	if !state.IsClosing && (state.CloseOrderID != 0 || state.CloseProgress.Quantity != 0 || state.CloseProgress.Notional != 0 || state.CloseRealizedPnL != 0 || state.CloseRequestedQty != 0 && state.CloseClientOrderID == "") {
		return fmt.Errorf("martingale runtime state has close progress without an active close order")
	}
	if state.IsClosing && state.PendingCloseReason != "" {
		return fmt.Errorf("martingale runtime state has both an active close order and a pending close intent")
	}
	if state.PendingCloseReason != "" && state.TotalQty <= 0 {
		return fmt.Errorf("martingale pending close intent has no attributed inventory")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = state.Entries
	s.totalCost, s.totalQty, s.avgEntryPrice = state.TotalCost, state.TotalQty, state.AvgEntryPrice
	s.currentLevel = state.CurrentLevel
	s.isPaused, s.isClosing, s.closeOrderID = state.IsPaused, state.IsClosing, state.CloseOrderID
	s.closeClientOrderID = state.CloseClientOrderID
	s.closeReason = state.CloseReason
	s.closeRequestedQty, s.closeProgress, s.closeRealizedPnL = state.CloseRequestedQty, state.CloseProgress, state.CloseRealizedPnL
	s.pendingCloseReason = state.PendingCloseReason
	s.stats = &state.Stats
	return nil
}
