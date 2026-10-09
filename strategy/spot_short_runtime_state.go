package strategy

import (
	"encoding/json"
	"fmt"
	"math"

	"quantmesh/config"
)

const spotShortRuntimeStateSchemaVersion = 10

type spotShortPendingRepay struct {
	ClientOrderID            string  `json:"client_order_id,omitempty"`
	OrderQuantity            float64 `json:"order_quantity"`
	ExecutedQty              float64 `json:"executed_qty"`
	BaseFeeQty               float64 `json:"base_fee_qty"`
	RepayUncertain           bool    `json:"repay_uncertain"`
	RepayPrepared            bool    `json:"repay_prepared,omitempty"`
	RepayTransferID          int64   `json:"repay_transfer_id,omitempty"`
	RepayAmount              float64 `json:"repay_amount,omitempty"`
	RepayStartedAtUnixMilli  int64   `json:"repay_started_at_unix_milli,omitempty"`
	RepayExpectedExecutedQty float64 `json:"repay_expected_executed_qty,omitempty"`
	RepayExpectedBaseFeeQty  float64 `json:"repay_expected_base_fee_qty,omitempty"`
}

type spotShortPendingBorrow struct {
	Amount              float64 `json:"amount"`
	Phase               string  `json:"phase"`
	BorrowTransferID    int64   `json:"borrow_transfer_id,omitempty"`
	CreatedAtUnixMilli  int64   `json:"created_at_unix_milli"`
	TerminalOrderID     int64   `json:"terminal_order_id,omitempty"`
	TerminalStatus      string  `json:"terminal_status,omitempty"`
	TerminalExecutedQty float64 `json:"terminal_executed_qty,omitempty"`
}

type spotShortPendingBuy struct {
	Quantity           float64 `json:"quantity"`
	CreatedAtUnixMilli int64   `json:"created_at_unix_milli"`
}

type spotShortRuntimeState struct {
	BotID                  string                            `json:"bot_id"`
	Strategy               string                            `json:"strategy"`
	GroupID                string                            `json:"group_id"`
	Symbol                 string                            `json:"symbol"`
	BaseAsset              string                            `json:"base_asset"`
	PendingRepay           map[int64]spotShortPendingRepay   `json:"pending_repay"`
	ConsumedRepayTransfers map[int64]int64                   `json:"consumed_repay_transfers"`
	PendingBorrow          map[string]spotShortPendingBorrow `json:"pending_borrow,omitempty"`
	PendingBuy             map[string]spotShortPendingBuy    `json:"pending_buy,omitempty"`
}

func (s *SpotShortStrategy) persistRuntimeStateLocked() error {
	if s.runtimeStateStore == nil {
		return fmt.Errorf("spot short runtime state store is unavailable")
	}
	state := spotShortRuntimeState{
		BotID: spotShortBotID(s.cfg), Strategy: s.name, GroupID: s.groupID,
		Symbol: s.symbol, BaseAsset: s.baseAsset, PendingRepay: make(map[int64]spotShortPendingRepay, len(s.pendingRepay)),
		PendingBorrow:          make(map[string]spotShortPendingBorrow, len(s.pendingBorrow)),
		PendingBuy:             make(map[string]spotShortPendingBuy, len(s.pendingBuy)),
		ConsumedRepayTransfers: make(map[int64]int64, len(s.consumedRepayTransfers)),
	}
	for id, pending := range s.pendingRepay {
		state.PendingRepay[id] = pending
	}
	for transferID, orderID := range s.consumedRepayTransfers {
		state.ConsumedRepayTransfers[transferID] = orderID
	}
	for clientOrderID, pending := range s.pendingBorrow {
		state.PendingBorrow[clientOrderID] = pending
	}
	for clientOrderID, pending := range s.pendingBuy {
		state.PendingBuy[clientOrderID] = pending
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
	if version != 3 && version != 4 && version != 5 && version != 6 && version != 7 && version != 8 && version != 9 && version != spotShortRuntimeStateSchemaVersion {
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
	if state.PendingBuy == nil {
		state.PendingBuy = make(map[string]spotShortPendingBuy)
	}
	if state.ConsumedRepayTransfers == nil {
		state.ConsumedRepayTransfers = make(map[int64]int64)
	}
	for transferID, orderID := range state.ConsumedRepayTransfers {
		if version < 9 || transferID <= 0 || orderID <= 0 {
			return fmt.Errorf("spot short runtime state contains invalid consumed repayment evidence")
		}
	}
	for id, amount := range state.PendingRepay {
		if amount.RepayPrepared && (version < 7 || !amount.RepayUncertain || amount.RepayTransferID != 0) {
			return fmt.Errorf("spot short order %d has invalid prepared repayment evidence", id)
		}
		if id <= 0 || math.IsNaN(amount.OrderQuantity) || math.IsInf(amount.OrderQuantity, 0) || amount.OrderQuantity <= 0 ||
			math.IsNaN(amount.ExecutedQty) || math.IsInf(amount.ExecutedQty, 0) || amount.ExecutedQty < 0 || amount.ExecutedQty > amount.OrderQuantity ||
			math.IsNaN(amount.BaseFeeQty) || math.IsInf(amount.BaseFeeQty, 0) || amount.BaseFeeQty < 0 || amount.BaseFeeQty > amount.ExecutedQty {
			return fmt.Errorf("spot short runtime state contains invalid pending repayment")
		}
		if amount.RepayTransferID < 0 || math.IsNaN(amount.RepayAmount) || math.IsInf(amount.RepayAmount, 0) || amount.RepayAmount < 0 ||
			amount.RepayStartedAtUnixMilli < 0 || math.IsNaN(amount.RepayExpectedExecutedQty) || math.IsInf(amount.RepayExpectedExecutedQty, 0) || amount.RepayExpectedExecutedQty < 0 ||
			math.IsNaN(amount.RepayExpectedBaseFeeQty) || math.IsInf(amount.RepayExpectedBaseFeeQty, 0) || amount.RepayExpectedBaseFeeQty < 0 {
			return fmt.Errorf("spot short runtime state contains invalid repayment reconciliation intent")
		}
		if amount.RepayUncertain && (amount.RepayStartedAtUnixMilli <= 0 || amount.RepayAmount <= 0 ||
			amount.RepayExpectedExecutedQty <= amount.ExecutedQty || amount.RepayExpectedExecutedQty > amount.OrderQuantity ||
			amount.RepayExpectedBaseFeeQty < amount.BaseFeeQty || amount.RepayExpectedBaseFeeQty > amount.RepayExpectedExecutedQty ||
			math.Abs((amount.RepayExpectedExecutedQty-amount.ExecutedQty)-(amount.RepayExpectedBaseFeeQty-amount.BaseFeeQty)-amount.RepayAmount) > math.Max(1e-10, amount.RepayAmount*1e-8)) {
			return fmt.Errorf("spot short order %d has an uncertain repayment without sufficient persisted transaction evidence", id)
		}
	}
	for clientOrderID, pending := range state.PendingBorrow {
		if clientOrderID == "" || math.IsNaN(pending.Amount) || math.IsInf(pending.Amount, 0) || pending.Amount <= 0 || pending.CreatedAtUnixMilli <= 0 ||
			(pending.Phase != "prepared" && pending.Phase != "borrowed" && pending.Phase != "unsubmitted" && pending.Phase != "settlement_pending") || pending.BorrowTransferID < 0 ||
			((pending.Phase != "borrowed" && pending.Phase != "settlement_pending") && pending.BorrowTransferID != 0) || ((pending.Phase == "borrowed" || pending.Phase == "settlement_pending") && pending.BorrowTransferID <= 0) ||
			(pending.Phase == "unsubmitted" && version < 8) ||
			(pending.Phase == "settlement_pending" && (version < 10 || pending.TerminalOrderID <= 0 || !isSpotShortTerminalOrderStatus(pending.TerminalStatus) || math.IsNaN(pending.TerminalExecutedQty) || math.IsInf(pending.TerminalExecutedQty, 0) || pending.TerminalExecutedQty <= 0 || pending.TerminalExecutedQty > pending.Amount+math.Max(1e-10, pending.Amount*1e-8))) {
			return fmt.Errorf("spot short runtime state contains invalid pending borrow intent")
		}
	}
	for clientOrderID, pending := range state.PendingBuy {
		if clientOrderID == "" || math.IsNaN(pending.Quantity) || math.IsInf(pending.Quantity, 0) || pending.Quantity <= 0 || pending.CreatedAtUnixMilli <= 0 {
			return fmt.Errorf("spot short runtime state contains invalid pending buy intent")
		}
	}
	s.pendingRepay = state.PendingRepay
	s.consumedRepayTransfers = state.ConsumedRepayTransfers
	s.pendingBorrow = state.PendingBorrow
	s.pendingBuy = state.PendingBuy
	if version != spotShortRuntimeStateSchemaVersion {
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
