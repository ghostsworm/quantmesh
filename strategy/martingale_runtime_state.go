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
		CloseRequestedQty: s.closeRequestedQty, CloseProgress: s.closeProgress,
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
	var totalQty, totalCost float64
	for _, entry := range state.Entries {
		if entry == nil || entry.Quantity < 0 || entry.Cost < 0 || entry.OpeningFee < 0 || entry.RequestedQuantity < 0 || !finiteNumber(entry.Quantity) || !finiteNumber(entry.Cost) || !finiteNumber(entry.OpeningFee) || !finiteNumber(entry.FillProgress.Quantity) || !finiteNumber(entry.FillProgress.Notional) {
			return fmt.Errorf("martingale runtime state contains invalid entry")
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
	if state.IsClosing && state.PendingCloseReason != "" {
		return fmt.Errorf("martingale runtime state has both an active close order and a pending close intent")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.entries = state.Entries
	s.totalCost, s.totalQty, s.avgEntryPrice = state.TotalCost, state.TotalQty, state.AvgEntryPrice
	s.currentLevel = state.CurrentLevel
	s.isPaused, s.isClosing, s.closeOrderID = state.IsPaused, state.IsClosing, state.CloseOrderID
	s.closeRequestedQty, s.closeProgress, s.closeRealizedPnL = state.CloseRequestedQty, state.CloseProgress, state.CloseRealizedPnL
	s.pendingCloseReason = state.PendingCloseReason
	s.stats = &state.Stats
	return nil
}
