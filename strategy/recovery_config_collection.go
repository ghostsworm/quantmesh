package strategy

import (
	"context"
	"fmt"

	"quantmesh/storage"
)

// Types and bindings must be resolved independently of the payload, including
// disabled/old records. Unknown records are not inferred from their JSON shape.
type RecoveryConfigStateBinding struct {
	StateKey, StrategyType, ComboParent string
	SingleLeg                           SingleLegRecoveryBinding
	Hedge                               HedgeRecoveryBinding
	Carry                               FundingCarryRecoveryBinding
	Spread                              FundingPerpSpreadRecoveryBinding
}

// Only call with a COMPLETE Bot-scoped read; nil means unavailable, not empty.
// This is a journal/configuration proof, not capital release or atomic fencing.
func VerifyBotRecoveryConfigStates(ctx context.Context, botID string, states []*storage.StrategyRuntimeState, bindings []RecoveryConfigStateBinding) error {
	if ctx == nil || !recoveryBindingComplete(botID) || states == nil {
		return fmt.Errorf("%w: complete Bot state evidence unavailable", ErrRecoveryConfigUnverified)
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%w: %w", ErrRecoveryConfigUnverified, err)
	}
	byKey := make(map[string]RecoveryConfigStateBinding, len(bindings))
	for _, binding := range bindings {
		if !recoveryBindingComplete(binding.StateKey) {
			return fmt.Errorf("%w: binding key unavailable", ErrRecoveryConfigUnverified)
		}
		if _, duplicate := byKey[binding.StateKey]; duplicate {
			return fmt.Errorf("%w: duplicate binding key", ErrRecoveryConfigUnverified)
		}
		byKey[binding.StateKey] = binding
	}
	seen := make(map[string]bool, len(states))
	for _, state := range states {
		if state == nil || state.BotID != botID || !recoveryBindingComplete(state.StrategyName) || seen[state.StrategyName] {
			return fmt.Errorf("%w: missing, cross-Bot or duplicate state identity", ErrRecoveryConfigUnverified)
		}
		seen[state.StrategyName] = true
	}
	for _, state := range states {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("%w: %w", ErrRecoveryConfigUnverified, err)
		}
		binding, found := byKey[state.StrategyName]
		if !found {
			return fmt.Errorf("%w: historical strategy binding unavailable", ErrRecoveryConfigUnverified)
		}
		if err := verifyRecoveryBindingKey(botID, binding, byKey, seen); err != nil {
			return err
		}
		if err := verifyRecoveryBoundState(state, binding); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("%w: %w", ErrRecoveryConfigUnverified, err)
	}
	return nil
}

func verifyRecoveryBindingKey(botID string, binding RecoveryConfigStateBinding, bindings map[string]RecoveryConfigStateBinding, records map[string]bool) error {
	name := binding.SingleLeg.StrategyName
	switch binding.StrategyType {
	case "funding_carry":
		name = "funding_carry"
	case "funding_perp_spread":
		name = "funding_perp_spread"
	case "spot_long", "spot_short", "futures_long", "futures_short":
		name = binding.Hedge.StrategyName
		if binding.Hedge.BotID != botID {
			return fmt.Errorf("%w: hedge binding owner mismatch", ErrRecoveryConfigUnverified)
		}
	default:
		if binding.SingleLeg.BotID != botID {
			return fmt.Errorf("%w: strategy binding owner mismatch", ErrRecoveryConfigUnverified)
		}
	}
	expectedKey := name
	if binding.ComboParent != "" {
		switch binding.StrategyType {
		case "dca", "dca_enhanced", "martingale", "trend", "mean_reversion", "momentum":
		default:
			return fmt.Errorf("%w: unsupported Combo child type", ErrRecoveryConfigUnverified)
		}
		parent, found := bindings[binding.ComboParent]
		if !found || parent.StrategyType != "combo" || parent.ComboParent != "" || parent.SingleLeg.BotID != botID || parent.SingleLeg.Symbol != binding.SingleLeg.Symbol || !records[binding.ComboParent] {
			return fmt.Errorf("%w: Combo parent evidence unavailable or mismatched", ErrRecoveryConfigUnverified)
		}
		key, err := comboChildRuntimeStateKey(parent.SingleLeg.StrategyName, name)
		if err != nil {
			return fmt.Errorf("%w: %w", ErrRecoveryConfigUnverified, err)
		}
		expectedKey = key
	}
	if binding.StateKey != expectedKey || !recoveryBindingComplete(name) {
		return fmt.Errorf("%w: storage namespace binding mismatch", ErrRecoveryConfigUnverified)
	}
	return nil
}

func verifyRecoveryBoundState(state *storage.StrategyRuntimeState, binding RecoveryConfigStateBinding) error {
	switch binding.StrategyType {
	case "dca", "dca_enhanced":
		return VerifyDCARecoveryConfigState(state.SchemaVersion, state.Payload, binding.SingleLeg)
	case "martingale":
		return VerifyMartingaleRecoveryConfigState(state.SchemaVersion, state.Payload, binding.SingleLeg)
	case "trend", "mean_reversion", "momentum":
		return VerifySignalRecoveryConfigState(state.SchemaVersion, state.Payload, binding.SingleLeg)
	case "spot_long":
		return VerifySpotLongRecoveryConfigState(state.SchemaVersion, state.Payload, binding.Hedge)
	case "spot_short":
		return VerifySpotShortRecoveryConfigState(state.SchemaVersion, state.Payload, binding.Hedge)
	case "futures_long", "futures_short":
		return VerifyFuturesHedgeRecoveryConfigState(state.SchemaVersion, state.Payload, binding.Hedge)
	case "funding_carry":
		return VerifyFundingCarryRecoveryConfigState(state.SchemaVersion, state.Payload, binding.Carry)
	case "funding_perp_spread":
		return VerifyFundingPerpSpreadRecoveryConfigState(state.SchemaVersion, state.Payload, binding.Spread)
	case "combo":
		return verifyComboRecoveryConfigParent(state.SchemaVersion, state.Payload, binding.SingleLeg)
	default:
		return fmt.Errorf("%w: unsupported historical strategy type", ErrRecoveryConfigUnverified)
	}
}
