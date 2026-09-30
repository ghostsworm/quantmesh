package strategy

import (
	"encoding/json"
	"fmt"
	"math"

	"quantmesh/config"
)

const spotLongRuntimeStateSchemaVersion = 2

type spotLongPendingOrder struct {
	ClientOrderID string  `json:"client_order_id,omitempty"`
	Side          string  `json:"side"`
	Quantity      float64 `json:"quantity"`
	ExecutedQty   float64 `json:"executed_qty"`
}

type spotLongPendingIntent struct {
	Side               string  `json:"side"`
	Quantity           float64 `json:"quantity"`
	CreatedAtUnixMilli int64   `json:"created_at_unix_milli"`
}

type spotLongRuntimeState struct {
	BotID          string                           `json:"bot_id"`
	Strategy       string                           `json:"strategy"`
	GroupID        string                           `json:"group_id"`
	Symbol         string                           `json:"symbol"`
	BaseAsset      string                           `json:"base_asset"`
	PendingOrders  map[int64]spotLongPendingOrder   `json:"pending_orders"`
	PendingIntents map[string]spotLongPendingIntent `json:"pending_intents,omitempty"`
}

func (s *SpotLongStrategy) persistRuntimeStateLocked() error {
	if s.runtimeStateStore == nil {
		return fmt.Errorf("spot long runtime state store is unavailable")
	}
	state := spotLongRuntimeState{
		BotID: spotLongBotID(s.cfg), Strategy: s.name, GroupID: s.groupID,
		Symbol: s.symbol, BaseAsset: s.baseAsset,
		PendingOrders:  make(map[int64]spotLongPendingOrder, len(s.pendingOrders)),
		PendingIntents: make(map[string]spotLongPendingIntent, len(s.pendingIntents)),
	}
	for id, order := range s.pendingOrders {
		state.PendingOrders[id] = order
	}
	for clientOrderID, intent := range s.pendingIntents {
		state.PendingIntents[clientOrderID] = intent
	}
	payload, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("encode spot long runtime state: %w", err)
	}
	if err := s.runtimeStateStore.SaveRuntimeState(s.name, spotLongRuntimeStateSchemaVersion, string(payload)); err != nil {
		wrapped := fmt.Errorf("persist spot long runtime state: %w", err)
		if s.runtimeStateErrorHandler != nil {
			s.runtimeStateErrorHandler(wrapped)
		}
		return wrapped
	}
	return nil
}

func (s *SpotLongStrategy) restoreRuntimeStateLocked() error {
	if s.runtimeStateStore == nil {
		return fmt.Errorf("spot long runtime state store is unavailable; refusing to start without durable order recovery")
	}
	version, payload, found, err := s.runtimeStateStore.LoadRuntimeState(s.name)
	if err != nil {
		return fmt.Errorf("load spot long runtime state: %w", err)
	}
	if !found {
		return nil
	}
	if version != 1 && version != spotLongRuntimeStateSchemaVersion {
		return fmt.Errorf("unsupported spot long runtime state schema version %d", version)
	}
	var state spotLongRuntimeState
	if err := json.Unmarshal([]byte(payload), &state); err != nil {
		return fmt.Errorf("decode spot long runtime state: %w", err)
	}
	if state.BotID != spotLongBotID(s.cfg) || state.Strategy != s.name || state.GroupID != s.groupID ||
		state.Symbol != s.symbol || state.BaseAsset != s.baseAsset {
		return fmt.Errorf("spot long runtime state identity mismatch")
	}
	if state.PendingOrders == nil {
		state.PendingOrders = make(map[int64]spotLongPendingOrder)
	}
	if state.PendingIntents == nil {
		state.PendingIntents = make(map[string]spotLongPendingIntent)
	}
	for id, order := range state.PendingOrders {
		if id <= 0 || (order.Side != "BUY" && order.Side != "SELL") || order.Quantity <= 0 ||
			math.IsNaN(order.Quantity) || math.IsInf(order.Quantity, 0) || order.ExecutedQty < 0 ||
			order.ExecutedQty > order.Quantity || math.IsNaN(order.ExecutedQty) || math.IsInf(order.ExecutedQty, 0) {
			return fmt.Errorf("spot long runtime state contains invalid pending order")
		}
	}
	for clientOrderID, intent := range state.PendingIntents {
		if clientOrderID == "" || (intent.Side != "BUY" && intent.Side != "SELL") || !finiteNumber(intent.Quantity) || intent.Quantity <= 0 || intent.CreatedAtUnixMilli <= 0 {
			return fmt.Errorf("spot long runtime state contains invalid pending order intent")
		}
	}
	s.pendingOrders = state.PendingOrders
	s.pendingIntents = state.PendingIntents
	if version != spotLongRuntimeStateSchemaVersion {
		if err := s.persistRuntimeStateLocked(); err != nil {
			return fmt.Errorf("upgrade spot long runtime state schema: %w", err)
		}
	}
	return nil
}

func spotLongBotID(cfg *config.Config) string {
	if cfg == nil {
		return ""
	}
	return cfg.Trading.BotID
}
