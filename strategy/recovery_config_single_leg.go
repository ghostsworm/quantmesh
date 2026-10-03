package strategy

import (
	"encoding/json"
	"fmt"
)

// Binding must come from the owner/configuration record, not the same payload
// being checked. Namespaced Combo children use their underlying child name.
type SingleLegRecoveryBinding struct {
	BotID, StrategyName, Symbol string
	Direction                   string // Mandatory for martingale; LONG or SHORT.
}

func VerifyDCARecoveryConfigState(version int, payload string, binding SingleLegRecoveryBinding) error {
	if version != dcaRuntimeStateSchemaVersion || !recoveryBindingComplete(binding.BotID, binding.StrategyName, binding.Symbol) {
		return fmt.Errorf("%w: DCA schema or independent binding unavailable", ErrRecoveryConfigUnverified)
	}
	var state dcaRuntimeState
	err := verifyRecoveryJSONWithCollections(payload, &state, map[string]bool{"layers": true}, "bot_id", "strategy_name", "symbol", "layers", "total_cost", "total_qty", "avg_entry_price", "current_layer", "dynamic_interval", "highest_profit", "take_profit_triggered", "is_closing", "close_order_id", "close_client_order_id", "close_layer_index", "close_progress", "close_fee_verified_qty", "close_base_fee_qty", "close_requested_qty", "close_limit_price", "stats")
	if err != nil {
		return fmt.Errorf("%w: %w", ErrRecoveryConfigUnverified, err)
	}
	if state.BotID != binding.BotID || state.StrategyName != binding.StrategyName || state.Symbol != binding.Symbol {
		return fmt.Errorf("%w: DCA owner identity mismatch", ErrRecoveryConfigUnverified)
	}
	if err := verifySingleLegNestedEvidence(payload, "stats", true); err != nil {
		return err
	}
	if !finiteNumber(state.DynamicInterval) || !finiteNumber(state.HighestProfit) || !recoveryStatisticsValid(state.Stats) {
		return fmt.Errorf("%w: DCA statistics or interval invalid", ErrRecoveryConfigUnverified)
	}
	if err := verifyDCAReleaseFlat(state); err != nil {
		return fmt.Errorf("%w: %w", ErrRecoveryConfigRequired, err)
	}
	return nil
}

func VerifyMartingaleRecoveryConfigState(version int, payload string, binding SingleLegRecoveryBinding) error {
	if version != martingaleRuntimeStateSchemaVersion || !recoveryBindingComplete(binding.BotID, binding.StrategyName, binding.Symbol) || (binding.Direction != "LONG" && binding.Direction != "SHORT") {
		return fmt.Errorf("%w: martingale schema or independent binding unavailable", ErrRecoveryConfigUnverified)
	}
	var state martingaleRuntimeState
	err := verifyRecoveryJSONWithCollections(payload, &state, map[string]bool{"entries": true}, "bot_id", "strategy_name", "symbol", "direction", "entries", "total_cost", "total_qty", "avg_entry_price", "current_level", "is_closing", "close_order_id", "close_requested_qty", "close_progress", "close_realized_pnl", "stats")
	if err != nil {
		return fmt.Errorf("%w: %w", ErrRecoveryConfigUnverified, err)
	}
	if state.BotID != binding.BotID || state.StrategyName != binding.StrategyName || state.Symbol != binding.Symbol || state.Direction != binding.Direction {
		return fmt.Errorf("%w: martingale owner identity mismatch", ErrRecoveryConfigUnverified)
	}
	if err := verifySingleLegNestedEvidence(payload, "stats", true); err != nil {
		return err
	}
	if !recoveryStatisticsValid(state.Stats) {
		return fmt.Errorf("%w: martingale statistics invalid", ErrRecoveryConfigUnverified)
	}
	if err := verifyMartingaleReleaseFlat(state); err != nil {
		return fmt.Errorf("%w: %w", ErrRecoveryConfigRequired, err)
	}
	return nil
}

// A saved position/order is never discarded here, even when its attributed
// quantity is zero. Full recovery validation is still required to act on it.
func VerifySignalRecoveryConfigState(version int, payload string, binding SingleLegRecoveryBinding) error {
	if version != signalRuntimeStateSchemaVersion || !recoveryBindingComplete(binding.BotID, binding.StrategyName, binding.Symbol) {
		return fmt.Errorf("%w: signal schema or independent binding unavailable", ErrRecoveryConfigUnverified)
	}
	var state signalRuntimeState
	if err := verifyRecoveryJSON(payload, &state, "bot_id", "strategy_name", "symbol", "entry_price", "statistics", "is_paused"); err != nil {
		return fmt.Errorf("%w: %w", ErrRecoveryConfigUnverified, err)
	}
	if state.BotID != binding.BotID || state.StrategyName != binding.StrategyName || state.Symbol != binding.Symbol {
		return fmt.Errorf("%w: signal owner identity mismatch", ErrRecoveryConfigUnverified)
	}
	if err := verifySingleLegNestedEvidence(payload, "statistics", false); err != nil {
		return err
	}
	if !recoveryStatisticsValid(state.Statistics) {
		return fmt.Errorf("%w: signal statistics invalid", ErrRecoveryConfigUnverified)
	}
	if err := verifySignalReleaseFlat(state); err != nil {
		return fmt.Errorf("%w: %w", ErrRecoveryConfigRequired, err)
	}
	return nil
}

func recoveryStatisticsValid(stats StrategyStatistics) bool {
	return stats.TotalTrades >= 0 && finiteNumber(stats.TotalPnL) && finiteNumber(stats.TotalVolume) && stats.TotalVolume >= 0 && finiteNumber(stats.WinRate) && stats.WinRate >= 0 && stats.WinRate <= 1
}

func verifySingleLegNestedEvidence(payload, statisticsKey string, hasCloseProgress bool) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(payload), &fields); err != nil {
		return fmt.Errorf("%w: %w", ErrRecoveryConfigUnverified, err)
	}
	var stats StrategyStatistics
	if err := verifyRecoveryJSON(string(fields[statisticsKey]), &stats, "TotalTrades", "WinRate", "TotalPnL", "TotalVolume"); err != nil {
		return fmt.Errorf("%w: incomplete statistics evidence", ErrRecoveryConfigUnverified)
	}
	if hasCloseProgress {
		var progress struct{ Quantity, Notional float64 }
		if err := verifyRecoveryJSON(string(fields["close_progress"]), &progress, "Quantity", "Notional"); err != nil {
			return fmt.Errorf("%w: incomplete close execution evidence", ErrRecoveryConfigUnverified)
		}
	}
	return nil
}
