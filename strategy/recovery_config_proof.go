package strategy

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// These errors distinguish unresolved economics from invalid evidence. Neither
// outcome authorizes discarding recovery configuration or releasing capital.
var (
	ErrRecoveryConfigUnverified = errors.New("recovery configuration evidence is unverified")
	ErrRecoveryConfigRequired   = errors.New("recovery configuration requires reconciliation")
)

type FundingCarryRecoveryBinding struct {
	FuturesExchange, SpotExchange, Symbol, BaseAsset, MarginAccountScope string
}

type FundingPerpSpreadRecoveryBinding struct {
	LegAExchange, LegASymbol, LegBExchange, LegBSymbol string
}

// VerifyFundingCarryRecoveryConfigState proves only the saved journal is flat,
// not live inventory, spendability, account ownership or atomic admission.
func VerifyFundingCarryRecoveryConfigState(version int, payload string, binding FundingCarryRecoveryBinding) error {
	if version != fundingCarryRuntimeStateVersion || !recoveryBindingComplete(binding.FuturesExchange, binding.SpotExchange, binding.Symbol, binding.BaseAsset, binding.MarginAccountScope) {
		return fmt.Errorf("%w: funding_carry schema or independent binding unavailable", ErrRecoveryConfigUnverified)
	}
	var raw fundingCarryRuntimeState
	if err := verifyRecoveryJSON(payload, &raw, "strategy", "futures_exchange", "spot_exchange", "symbol", "ownership_ready", "intent_in_flight", "exposure_unknown", "direction", "owned_spot", "owned_futures", "margin_debt"); err != nil {
		return fmt.Errorf("%w: %w", ErrRecoveryConfigUnverified, err)
	}
	state, err := decodeFundingCarryRuntimeStateForRecovery(version, payload, binding.FuturesExchange, binding.SpotExchange, binding.Symbol, true)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrRecoveryConfigUnverified, err)
	}
	if state.MarginAccountScope != binding.MarginAccountScope {
		return fmt.Errorf("%w: funding_carry margin scope mismatch", ErrRecoveryConfigUnverified)
	}
	if err := validateFundingCarryDebtAsset(state, binding.BaseAsset); err != nil {
		return fmt.Errorf("%w: %w", ErrRecoveryConfigUnverified, err)
	}
	if state.IntentInFlight || state.ExposureUnknown || state.Direction != DirectionNone || state.OwnedSpot != 0 || state.OwnedFutures != 0 || state.MarginDebt != 0 || state.MarginBorrowTransferID != 0 || !state.MarginBorrowedAt.IsZero() || state.MarginRepayIntent != nil || state.MarginCoverIntent != nil {
		return fmt.Errorf("%w: funding_carry has pending operations or exposure", ErrRecoveryConfigRequired)
	}
	remaining, err := fundingCarryCoverRemaining(state, binding.BaseAsset)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrRecoveryConfigUnverified, err)
	}
	if remaining.Sign() != 0 {
		return fmt.Errorf("%w: funding_carry has historical remaining assets", ErrRecoveryConfigRequired)
	}
	return nil
}

func VerifyFundingPerpSpreadRecoveryConfigState(version int, payload string, binding FundingPerpSpreadRecoveryBinding) error {
	if version != fundingPerpSpreadRuntimeStateVersion || !recoveryBindingComplete(binding.LegAExchange, binding.LegASymbol, binding.LegBExchange, binding.LegBSymbol) {
		return fmt.Errorf("%w: funding_perp_spread schema or independent binding unavailable", ErrRecoveryConfigUnverified)
	}
	var raw fundingPerpSpreadRuntimeState
	if err := verifyRecoveryJSON(payload, &raw, "strategy", "leg_a_exchange", "leg_a_symbol", "leg_b_exchange", "leg_b_symbol", "ownership_ready", "intent_in_flight", "exposure_unknown", "owned_a", "owned_b"); err != nil {
		return fmt.Errorf("%w: %w", ErrRecoveryConfigUnverified, err)
	}
	state, err := decodeFundingPerpSpreadRuntimeState(version, payload, binding.LegAExchange, binding.LegASymbol, binding.LegBExchange, binding.LegBSymbol)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrRecoveryConfigUnverified, err)
	}
	if state.IntentInFlight || state.PendingOrder != nil || state.ExposureUnknown || state.ExecutionLedgerUnverified || state.EmergencyCloseRequired || len(state.PendingExecutions) != 0 || state.OwnedA != 0 || state.OwnedB != 0 {
		return fmt.Errorf("%w: funding_perp_spread has pending execution or exposure", ErrRecoveryConfigRequired)
	}
	return nil
}

func recoveryBindingComplete(values ...string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return false
		}
	}
	return true
}

// The arbitrage serializers never emit null. Other serializers may explicitly
// declare a nullable top-level empty collection; null entries remain invalid.
func verifyRecoveryJSON(payload string, target interface{}, required ...string) error {
	return verifyRecoveryJSONWithCollections(payload, target, nil, required...)
}

func verifyRecoveryJSONWithCollections(payload string, target interface{}, nullableCollections map[string]bool, required ...string) error {
	decoder := json.NewDecoder(strings.NewReader(payload))
	if err := verifyRecoveryJSONValue(decoder, 0, "", nullableCollections); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return fmt.Errorf("recovery state has trailing data")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(payload), &fields); err != nil {
		return err
	}
	for _, key := range required {
		if _, found := fields[key]; !found {
			return fmt.Errorf("recovery state missing %s", key)
		}
	}
	decoder = json.NewDecoder(strings.NewReader(payload))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func verifyRecoveryJSONValue(decoder *json.Decoder, depth int, field string, nullableCollections map[string]bool) error {
	if depth > 128 {
		return fmt.Errorf("recovery state nesting exceeds limit")
	}
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	if token == nil {
		if depth == 1 && nullableCollections[field] {
			return nil
		}
		return fmt.Errorf("recovery state contains null evidence")
	}
	delimiter, container := token.(json.Delim)
	if !container {
		return nil
	}
	seen := make(map[string]bool)
	for decoder.More() {
		childField := ""
		if delimiter == '{' {
			key, err := decoder.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			name = strings.ToLower(name)
			if !ok || seen[name] {
				return fmt.Errorf("recovery state contains duplicate or invalid key")
			}
			seen[name] = true
			childField = name
		}
		if err := verifyRecoveryJSONValue(decoder, depth+1, childField, nullableCollections); err != nil {
			return err
		}
	}
	_, err = decoder.Token()
	return err
}
