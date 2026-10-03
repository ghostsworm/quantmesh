package strategy

import "fmt"

type HedgeRecoveryBinding struct {
	BotID, StrategyName, GroupID, Symbol, BaseAsset string
}

// These order journals do not serialize settled inventory. Empty pending
// cursors prove only journal settlement, not live inventory or zero margin debt.
func VerifySpotLongRecoveryConfigState(version int, payload string, binding HedgeRecoveryBinding) error {
	if version != spotLongRuntimeStateSchemaVersion || !recoveryBindingComplete(binding.BotID, binding.StrategyName, binding.Symbol, binding.BaseAsset) {
		return fmt.Errorf("%w: SpotLong schema or independent binding unavailable", ErrRecoveryConfigUnverified)
	}
	var state spotLongRuntimeState
	if err := verifyRecoveryJSON(payload, &state, "bot_id", "strategy", "group_id", "symbol", "base_asset", "pending_orders"); err != nil {
		return fmt.Errorf("%w: %w", ErrRecoveryConfigUnverified, err)
	}
	if state.BotID != binding.BotID || state.Strategy != binding.StrategyName || state.GroupID != binding.GroupID || state.Symbol != binding.Symbol || state.BaseAsset != binding.BaseAsset {
		return fmt.Errorf("%w: SpotLong owner identity mismatch", ErrRecoveryConfigUnverified)
	}
	if len(state.PendingOrders) != 0 || len(state.PendingIntents) != 0 {
		return fmt.Errorf("%w: SpotLong order journal is unresolved", ErrRecoveryConfigRequired)
	}
	return nil
}

func VerifySpotShortRecoveryConfigState(version int, payload string, binding HedgeRecoveryBinding) error {
	if version != spotShortRuntimeStateSchemaVersion || !recoveryBindingComplete(binding.BotID, binding.StrategyName, binding.Symbol, binding.BaseAsset) {
		return fmt.Errorf("%w: SpotShort schema or independent binding unavailable", ErrRecoveryConfigUnverified)
	}
	var state spotShortRuntimeState
	if err := verifyRecoveryJSON(payload, &state, "bot_id", "strategy", "group_id", "symbol", "base_asset", "pending_repay", "consumed_repay_transfers"); err != nil {
		return fmt.Errorf("%w: %w", ErrRecoveryConfigUnverified, err)
	}
	if state.BotID != binding.BotID || state.Strategy != binding.StrategyName || state.GroupID != binding.GroupID || state.Symbol != binding.Symbol || state.BaseAsset != binding.BaseAsset {
		return fmt.Errorf("%w: SpotShort owner identity mismatch", ErrRecoveryConfigUnverified)
	}
	for transferID, orderID := range state.ConsumedRepayTransfers {
		if transferID <= 0 || orderID <= 0 {
			return fmt.Errorf("%w: SpotShort consumed repayment identity invalid", ErrRecoveryConfigUnverified)
		}
	}
	if len(state.PendingBorrow) != 0 || len(state.PendingBuy) != 0 || len(state.PendingRepay) != 0 {
		return fmt.Errorf("%w: SpotShort borrow/buy/repay journal is unresolved", ErrRecoveryConfigRequired)
	}
	return nil
}

func VerifyFuturesHedgeRecoveryConfigState(version int, payload string, binding HedgeRecoveryBinding) error {
	if version != futuresHedgeRuntimeStateVersion || !recoveryBindingComplete(binding.BotID, binding.StrategyName, binding.Symbol) {
		return fmt.Errorf("%w: futures hedge schema or independent binding unavailable", ErrRecoveryConfigUnverified)
	}
	var state futuresHedgeRuntimeState
	if err := verifyRecoveryJSON(payload, &state, "bot_id", "strategy", "group_id", "symbol"); err != nil {
		return fmt.Errorf("%w: %w", ErrRecoveryConfigUnverified, err)
	}
	if state.BotID != binding.BotID || state.Strategy != binding.StrategyName || state.GroupID != binding.GroupID || state.Symbol != binding.Symbol {
		return fmt.Errorf("%w: futures hedge owner identity mismatch", ErrRecoveryConfigUnverified)
	}
	if state.Pending != nil {
		return fmt.Errorf("%w: futures hedge order journal is unresolved", ErrRecoveryConfigRequired)
	}
	return nil
}

// Parent high-water evidence alone never certifies the Combo child journals.
func verifyComboRecoveryConfigParent(version int, payload string, binding SingleLegRecoveryBinding) error {
	if version != comboRuntimeStateSchemaVersion || !recoveryBindingComplete(binding.BotID, binding.StrategyName, binding.Symbol) {
		return fmt.Errorf("%w: Combo schema or independent binding unavailable", ErrRecoveryConfigUnverified)
	}
	var state comboRuntimeState
	if err := verifyRecoveryJSON(payload, &state, "bot_id", "strategy_name", "symbol", "peak_equity"); err != nil {
		return fmt.Errorf("%w: %w", ErrRecoveryConfigUnverified, err)
	}
	if state.BotID != binding.BotID || state.StrategyName != binding.StrategyName || state.Symbol != binding.Symbol || !finiteNumber(state.PeakEquity) || state.PeakEquity < 0 {
		return fmt.Errorf("%w: Combo parent identity or high-water invalid", ErrRecoveryConfigUnverified)
	}
	return nil
}
