package strategy

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

const fundingPerpSpreadRuntimeStateVersion = 6

type fundingPerpSpreadOrderIntent struct {
	ClientOrderID  string  `json:"client_order_id"`
	LegExchange    string  `json:"leg_exchange"`
	Symbol         string  `json:"symbol"`
	Side           string  `json:"side"`
	Quantity       float64 `json:"quantity"`
	PositionBefore float64 `json:"position_before"`
}

type fundingPerpSpreadPendingExecutionState struct {
	Exchange      string  `json:"exchange"`
	Symbol        string  `json:"symbol"`
	ClientOrderID string  `json:"client_order_id"`
	OrderID       int64   `json:"order_id,omitempty"`
	Side          string  `json:"side"`
	Quantity      float64 `json:"quantity"`
}

type fundingPerpSpreadRuntimeState struct {
	Strategy                  string                                   `json:"strategy"`
	LegAExchange              string                                   `json:"leg_a_exchange"`
	LegASymbol                string                                   `json:"leg_a_symbol"`
	LegBExchange              string                                   `json:"leg_b_exchange"`
	LegBSymbol                string                                   `json:"leg_b_symbol"`
	OwnershipReady            bool                                     `json:"ownership_ready"`
	IntentInFlight            bool                                     `json:"intent_in_flight"`
	PendingOrder              *fundingPerpSpreadOrderIntent            `json:"pending_order,omitempty"`
	ExposureUnknown           bool                                     `json:"exposure_unknown"`
	ExecutionLedgerUnverified bool                                     `json:"execution_ledger_unverified,omitempty"`
	EmergencyCloseRequired    bool                                     `json:"emergency_close_required,omitempty"`
	PendingExecutions         []fundingPerpSpreadPendingExecutionState `json:"pending_executions,omitempty"`
	OwnedA                    float64                                  `json:"owned_a"`
	OwnedB                    float64                                  `json:"owned_b"`
}

func (s *FundingPerpSpreadStrategy) runtimeStateSnapshotLocked() fundingPerpSpreadRuntimeState {
	return fundingPerpSpreadRuntimeState{
		Strategy: "funding_perp_spread", LegAExchange: s.legA.GetName(), LegASymbol: s.symA,
		LegBExchange: s.legB.GetName(), LegBSymbol: s.symB, OwnershipReady: s.ownershipReady,
		IntentInFlight: s.intentInFlight, PendingOrder: cloneFundingPerpSpreadOrderIntent(s.pendingOrder),
		ExposureUnknown: s.exposureUnknown, ExecutionLedgerUnverified: s.executionLedgerUnverified,
		EmergencyCloseRequired: s.emergencyCloseRequired,
		PendingExecutions:      cloneFundingPerpSpreadPendingExecutions(s.pendingExecutions),
		OwnedA:                 s.ownedA, OwnedB: s.ownedB,
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
	if version < 1 || version > fundingPerpSpreadRuntimeStateVersion {
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
	if version < 3 && state.IntentInFlight {
		return fundingPerpSpreadRuntimeState{}, fmt.Errorf("legacy funding_perp_spread order intent lacks the durable pre-submit position snapshot required for safe recovery")
	}
	if version >= 3 {
		if state.IntentInFlight != (state.PendingOrder != nil) {
			return fundingPerpSpreadRuntimeState{}, fmt.Errorf("funding_perp_spread pending order identity does not match intent state")
		}
		if intent := state.PendingOrder; intent != nil {
			validLeg := (strings.EqualFold(intent.LegExchange, legAExchange) && strings.EqualFold(intent.Symbol, legASymbol)) ||
				(strings.EqualFold(intent.LegExchange, legBExchange) && strings.EqualFold(intent.Symbol, legBSymbol))
			if strings.TrimSpace(intent.ClientOrderID) == "" || len(intent.ClientOrderID) > 64 || !validLeg ||
				(intent.Side != "BUY" && intent.Side != "SELL") || math.IsNaN(intent.Quantity) || math.IsInf(intent.Quantity, 0) || intent.Quantity <= 0 ||
				math.IsNaN(intent.PositionBefore) || math.IsInf(intent.PositionBefore, 0) {
				return fundingPerpSpreadRuntimeState{}, fmt.Errorf("funding_perp_spread pending order identity is invalid")
			}
		}
	}
	for _, pending := range state.PendingExecutions {
		validLeg := (strings.EqualFold(pending.Exchange, legAExchange) && strings.EqualFold(pending.Symbol, legASymbol)) ||
			(strings.EqualFold(pending.Exchange, legBExchange) && strings.EqualFold(pending.Symbol, legBSymbol))
		if !validLeg || strings.TrimSpace(pending.ClientOrderID) == "" || len(pending.ClientOrderID) > 64 || pending.OrderID < 0 ||
			(pending.Side != "BUY" && pending.Side != "SELL") || math.IsNaN(pending.Quantity) || math.IsInf(pending.Quantity, 0) || pending.Quantity <= 0 {
			return fundingPerpSpreadRuntimeState{}, fmt.Errorf("funding_perp_spread pending execution identity is invalid")
		}
	}
	if len(state.PendingExecutions) > 0 && !state.ExecutionLedgerUnverified {
		return fundingPerpSpreadRuntimeState{}, fmt.Errorf("funding_perp_spread pending executions require an unverified ledger block")
	}
	if !state.OwnershipReady || (state.ExposureUnknown && !state.IntentInFlight) ||
		math.IsNaN(state.OwnedA) || math.IsInf(state.OwnedA, 0) ||
		math.IsNaN(state.OwnedB) || math.IsInf(state.OwnedB, 0) {
		return fundingPerpSpreadRuntimeState{}, fmt.Errorf("funding_perp_spread runtime state is unresolved")
	}
	return state, nil
}

func cloneFundingPerpSpreadPendingExecutions(src []fundingPerpSpreadPendingExecutionState) []fundingPerpSpreadPendingExecutionState {
	if len(src) == 0 {
		return nil
	}
	return append([]fundingPerpSpreadPendingExecutionState(nil), src...)
}

func cloneFundingPerpSpreadOrderIntent(src *fundingPerpSpreadOrderIntent) *fundingPerpSpreadOrderIntent {
	if src == nil {
		return nil
	}
	copy := *src
	return &copy
}
