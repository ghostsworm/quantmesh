package strategy

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

const fundingPerpSpreadRuntimeStateVersion = 1

type fundingPerpSpreadRuntimeState struct {
	Strategy        string  `json:"strategy"`
	LegAExchange    string  `json:"leg_a_exchange"`
	LegASymbol      string  `json:"leg_a_symbol"`
	LegBExchange    string  `json:"leg_b_exchange"`
	LegBSymbol      string  `json:"leg_b_symbol"`
	OwnershipReady  bool    `json:"ownership_ready"`
	IntentInFlight  bool    `json:"intent_in_flight"`
	ExposureUnknown bool    `json:"exposure_unknown"`
	OwnedA          float64 `json:"owned_a"`
	OwnedB          float64 `json:"owned_b"`
}

func (s *FundingPerpSpreadStrategy) runtimeStateSnapshotLocked() fundingPerpSpreadRuntimeState {
	return fundingPerpSpreadRuntimeState{
		Strategy: "funding_perp_spread", LegAExchange: s.legA.GetName(), LegASymbol: s.symA,
		LegBExchange: s.legB.GetName(), LegBSymbol: s.symB, OwnershipReady: s.ownershipReady,
		IntentInFlight: s.intentInFlight, ExposureUnknown: s.exposureUnknown,
		OwnedA: s.ownedA, OwnedB: s.ownedB,
	}
}

func (s *FundingPerpSpreadStrategy) persistRuntimeStateLocked() error {
	if s.runtimeStateStore == nil {
		return fmt.Errorf("funding_perp_spread runtime state store is unavailable")
	}
	payload, err := json.Marshal(s.runtimeStateSnapshotLocked())
	if err != nil {
		return fmt.Errorf("encode funding_perp_spread runtime state: %w", err)
	}
	if err := s.runtimeStateStore.SaveRuntimeState("funding_perp_spread", fundingPerpSpreadRuntimeStateVersion, string(payload)); err != nil {
		return fmt.Errorf("persist funding_perp_spread runtime state: %w", err)
	}
	return nil
}

func decodeFundingPerpSpreadRuntimeState(version int, payload string, legAExchange, legASymbol, legBExchange, legBSymbol string) (fundingPerpSpreadRuntimeState, error) {
	if version != fundingPerpSpreadRuntimeStateVersion {
		return fundingPerpSpreadRuntimeState{}, fmt.Errorf("unsupported funding_perp_spread runtime state schema %d", version)
	}
	var state fundingPerpSpreadRuntimeState
	if err := json.Unmarshal([]byte(payload), &state); err != nil {
		return fundingPerpSpreadRuntimeState{}, fmt.Errorf("decode funding_perp_spread runtime state: %w", err)
	}
	if state.Strategy != "funding_perp_spread" ||
		!strings.EqualFold(state.LegAExchange, legAExchange) || !strings.EqualFold(state.LegASymbol, legASymbol) ||
		!strings.EqualFold(state.LegBExchange, legBExchange) || !strings.EqualFold(state.LegBSymbol, legBSymbol) {
		return fundingPerpSpreadRuntimeState{}, fmt.Errorf("funding_perp_spread runtime state identity mismatch")
	}
	if !state.OwnershipReady || state.IntentInFlight || state.ExposureUnknown ||
		math.IsNaN(state.OwnedA) || math.IsInf(state.OwnedA, 0) ||
		math.IsNaN(state.OwnedB) || math.IsInf(state.OwnedB, 0) {
		return fundingPerpSpreadRuntimeState{}, fmt.Errorf("funding_perp_spread runtime state is unresolved")
	}
	return state, nil
}
