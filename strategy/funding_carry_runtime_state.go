package strategy

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

const fundingCarryRuntimeStateVersion = 1

type fundingCarryRuntimeState struct {
	Strategy        string         `json:"strategy"`
	FuturesExchange string         `json:"futures_exchange"`
	SpotExchange    string         `json:"spot_exchange"`
	Symbol          string         `json:"symbol"`
	OwnershipReady  bool           `json:"ownership_ready"`
	IntentInFlight  bool           `json:"intent_in_flight"`
	ExposureUnknown bool           `json:"exposure_unknown"`
	Direction       CarryDirection `json:"direction"`
	OwnedSpot       float64        `json:"owned_spot"`
	OwnedFutures    float64        `json:"owned_futures"`
	MarginDebt      float64        `json:"margin_debt"`
}

func (s *FundingCarryStrategy) runtimeStateSnapshotLocked() fundingCarryRuntimeState {
	return fundingCarryRuntimeState{
		Strategy: "funding_carry", FuturesExchange: s.fut.GetName(), SpotExchange: s.spot.GetName(),
		Symbol: s.symbol, OwnershipReady: s.strategySpotKnown, IntentInFlight: s.intentInFlight,
		ExposureUnknown: s.unownedExposure, Direction: s.direction, OwnedSpot: s.strategySpotQty,
		OwnedFutures: s.futQty, MarginDebt: s.marginDebt,
	}
}

func (s *FundingCarryStrategy) persistRuntimeStateLocked() error {
	if s.runtimeStateStore == nil {
		return fmt.Errorf("funding_carry runtime state store is unavailable")
	}
	payload, err := json.Marshal(s.runtimeStateSnapshotLocked())
	if err != nil {
		return fmt.Errorf("encode funding_carry runtime state: %w", err)
	}
	if err := s.runtimeStateStore.SaveRuntimeState("funding_carry", fundingCarryRuntimeStateVersion, string(payload)); err != nil {
		return fmt.Errorf("persist funding_carry runtime state: %w", err)
	}
	s.runtimeStateErr = nil
	return nil
}

func (s *FundingCarryStrategy) restoreRuntimeState() error {
	s.mu.RLock()
	store := s.runtimeStateStore
	s.mu.RUnlock()
	if store == nil {
		return fmt.Errorf("durable runtime state store is required")
	}
	version, payload, found, err := store.LoadRuntimeState("funding_carry")
	if err != nil {
		return fmt.Errorf("load runtime state: %w", err)
	}
	if !found {
		return nil
	}
	state, err := decodeFundingCarryRuntimeState(version, payload, s.fut.GetName(), s.spot.GetName(), s.symbol)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.direction = state.Direction
	s.strategySpotQty = state.OwnedSpot
	s.spotQty = state.OwnedSpot
	s.futQty = state.OwnedFutures
	s.marginDebt = state.MarginDebt
	s.strategySpotKnown = true
	s.unownedExposure = false
	s.intentInFlight = false
	s.mu.Unlock()
	return nil
}

func decodeFundingCarryRuntimeState(version int, payload, futuresExchange, spotExchange, symbol string) (fundingCarryRuntimeState, error) {
	if version != fundingCarryRuntimeStateVersion {
		return fundingCarryRuntimeState{}, fmt.Errorf("unsupported funding_carry runtime state schema %d", version)
	}
	var state fundingCarryRuntimeState
	if err := json.Unmarshal([]byte(payload), &state); err != nil {
		return fundingCarryRuntimeState{}, fmt.Errorf("decode funding_carry runtime state: %w", err)
	}
	if state.Strategy != "funding_carry" || !strings.EqualFold(state.FuturesExchange, futuresExchange) ||
		!strings.EqualFold(state.SpotExchange, spotExchange) || !strings.EqualFold(state.Symbol, symbol) {
		return fundingCarryRuntimeState{}, fmt.Errorf("funding_carry runtime state identity mismatch")
	}
	if !state.OwnershipReady || state.IntentInFlight || state.ExposureUnknown ||
		state.Direction < DirectionNone || state.Direction > DirectionReverse ||
		!validRuntimeAmount(state.OwnedSpot) || !validRuntimeAmount(state.OwnedFutures) || !validRuntimeAmount(state.MarginDebt) {
		return fundingCarryRuntimeState{}, fmt.Errorf("funding_carry runtime state is unresolved or invalid")
	}
	if state.Direction == DirectionNone && (state.OwnedSpot > 0 || state.OwnedFutures > 0 || state.MarginDebt > 0) {
		return fundingCarryRuntimeState{}, fmt.Errorf("funding_carry flat state contains owned exposure")
	}
	return state, nil
}

func validRuntimeAmount(value float64) bool {
	return value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

func (s *FundingCarryStrategy) beginRuntimeIntent() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.runtimeStateErr != nil {
		s.unownedExposure = true
		return fmt.Errorf("previous runtime state persistence failed: %w", s.runtimeStateErr)
	}
	s.intentInFlight = true
	if err := s.persistRuntimeStateLocked(); err != nil {
		s.unownedExposure = true
		return err
	}
	return nil
}

func (s *FundingCarryStrategy) finishRuntimeIntent(success bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.intentInFlight = false
	if !success {
		s.unownedExposure = true
	}
	if err := s.persistRuntimeStateLocked(); err != nil {
		s.unownedExposure = true
		s.runtimeStateErr = err
		return err
	}
	return nil
}
