package strategy

import (
	"encoding/json"
	"fmt"
	"math"

	"quantmesh/config"
)

const spotShortRuntimeStateSchemaVersion = 4

type spotShortPendingRepay struct {
	OrderQuantity  float64 `json:"order_quantity"`
	ExecutedQty    float64 `json:"executed_qty"`
	BaseFeeQty     float64 `json:"base_fee_qty"`
	RepayUncertain bool    `json:"repay_uncertain"`
}

type spotShortPendingBorrow struct {
	Amount             float64 `json:"amount"`
	Phase              string  `json:"phase"`
	BorrowTransferID   int64   `json:"borrow_transfer_id,omitempty"`
	CreatedAtUnixMilli int64   `json:"created_at_unix_milli"`
}

type spotShortRuntimeState struct {
	BotID         string                            `json:"bot_id"`
	Strategy      string                            `json:"strategy"`
	GroupID       string                            `json:"group_id"`
	Symbol        string                            `json:"symbol"`
	BaseAsset     string                            `json:"base_asset"`
	PendingRepay  map[int64]spotShortPendingRepay   `json:"pending_repay"`
	PendingBorrow map[string]spotShortPendingBorrow `json:"pending_borrow,omitempty"`
}

func (s *SpotShortStrategy) persistRuntimeStateLocked() error {
	if s.runtimeStateStore == nil {
		return fmt.Errorf("spot short runtime state store is unavailable")
	}
	state := spotShortRuntimeState{
		BotID: spotShortBotID(s.cfg), Strategy: s.name, GroupID: s.groupID,
		Symbol: s.symbol, BaseAsset: s.baseAsset, PendingRepay: make(map[int64]spotShortPendingRepay, len(s.pendingRepay)),
		PendingBorrow: make(map[string]spotShortPendingBorrow, len(s.pendingBorrow)),
	}
	for id, pending := range s.pendingRepay {
		state.PendingRepay[id] = pending
	}
	for clientOrderID, pending := range s.pendingBorrow {
		state.PendingBorrow[clientOrderID] = pending
	}
	payload, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("encode spot short runtime state: %w", err)
	}
	if err := s.runtimeStateStore.SaveRuntimeState(s.name, spotShortRuntimeStateSchemaVersion, string(payload)); err != nil {
		wrapped := fmt.Errorf("persist spot short runtime state: %w", err)
		if s.runtimeStateErrorHandler != nil {
			s.runtimeStateErrorHandler(wrapped)
		}
		return wrapped
	}
	return nil
}

func (s *SpotShortStrategy) restoreRuntimeStateLocked() error {
	if s.runtimeStateStore == nil {
		return fmt.Errorf("spot short runtime state store is unavailable; refusing to start without durable debt/order recovery")
	}
	version, payload, found, err := s.runtimeStateStore.LoadRuntimeState(s.name)
	if err != nil {
		return fmt.Errorf("load spot short runtime state: %w", err)
	}
	if !found {
		return nil
	}
	if version != 3 && version != spotShortRuntimeStateSchemaVersion {
		return fmt.Errorf("unsupported spot short runtime state schema version %d", version)
	}
	var state spotShortRuntimeState
	if err := json.Unmarshal([]byte(payload), &state); err != nil {
		return fmt.Errorf("decode spot short runtime state: %w", err)
	}
	if state.BotID != spotShortBotID(s.cfg) || state.Strategy != s.name || state.GroupID != s.groupID ||
		state.Symbol != s.symbol || state.BaseAsset != s.baseAsset {
		return fmt.Errorf("spot short runtime state identity mismatch")
	}
	if state.PendingRepay == nil {
		state.PendingRepay = make(map[int64]spotShortPendingRepay)
	}
	if state.PendingBorrow == nil {
		state.PendingBorrow = make(map[string]spotShortPendingBorrow)
	}
	for id, amount := range state.PendingRepay {
		if id <= 0 || math.IsNaN(amount.OrderQuantity) || math.IsInf(amount.OrderQuantity, 0) || amount.OrderQuantity <= 0 ||
			math.IsNaN(amount.ExecutedQty) || math.IsInf(amount.ExecutedQty, 0) || amount.ExecutedQty < 0 || amount.ExecutedQty > amount.OrderQuantity ||
			math.IsNaN(amount.BaseFeeQty) || math.IsInf(amount.BaseFeeQty, 0) || amount.BaseFeeQty < 0 || amount.BaseFeeQty > amount.ExecutedQty {
			return fmt.Errorf("spot short runtime state contains invalid pending repayment")
		}
		if amount.RepayUncertain {
			return fmt.Errorf("spot short order %d repayment outcome is uncertain; exchange reconciliation is required before restart", id)
		}
	}
	for clientOrderID, pending := range state.PendingBorrow {
		if clientOrderID == "" || math.IsNaN(pending.Amount) || math.IsInf(pending.Amount, 0) || pending.Amount <= 0 || pending.CreatedAtUnixMilli <= 0 ||
			(pending.Phase != "prepared" && pending.Phase != "borrowed") || pending.BorrowTransferID < 0 ||
			(pending.Phase == "prepared" && pending.BorrowTransferID != 0) || (pending.Phase == "borrowed" && pending.BorrowTransferID <= 0) {
			return fmt.Errorf("spot short runtime state contains invalid pending borrow intent")
		}
	}
	s.pendingRepay = state.PendingRepay
	s.pendingBorrow = state.PendingBorrow
	if len(s.pendingBorrow) > 0 {
		return fmt.Errorf("spot short has %d unresolved borrow intent(s); reconcile the borrow account and client order id before restart", len(s.pendingBorrow))
	}
	if version == 3 {
		if err := s.persistRuntimeStateLocked(); err != nil {
			return fmt.Errorf("upgrade spot short runtime state schema: %w", err)
		}
	}
	return nil
}

func spotShortBotID(cfg *config.Config) string {
	if cfg == nil {
		return ""
	}
	return cfg.Trading.BotID
}
