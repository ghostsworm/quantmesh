package main

import (
	"context"
	"encoding/json"
	"fmt"

	"quantmesh/risk"
	"quantmesh/storage"
)

const circuitEquityCheckpointKey = "global-circuit-equity-v1"

type riskCheckpointBackend interface {
	LoadRiskCheckpoint(context.Context, string) ([]byte, int64, error)
	SaveRiskCheckpoint(context.Context, string, int64, []byte) error
}

type persistedEquityState struct{ backend riskCheckpointBackend }

func equityStateStore(service *storage.StorageService) risk.EquityStateStore {
	if service == nil {
		return nil
	}
	backend, ok := service.GetStorage().(riskCheckpointBackend)
	if !ok {
		return nil
	}
	return &persistedEquityState{backend: backend}
}

func (s *persistedEquityState) LoadEquityState(ctx context.Context) (*risk.EquityCheckpoint, error) {
	payload, revision, err := s.backend.LoadRiskCheckpoint(ctx, circuitEquityCheckpointKey)
	if err != nil {
		return nil, err
	}
	if payload == nil && revision == 0 {
		return nil, nil
	}
	var state risk.EquityCheckpoint
	if err := json.Unmarshal(payload, &state); err != nil {
		return nil, fmt.Errorf("decode equity checkpoint: %w", err)
	}
	if state.Revision != revision {
		return nil, fmt.Errorf("equity checkpoint revision mismatch")
	}
	return &state, nil
}

func (s *persistedEquityState) SaveEquityState(ctx context.Context, expected int64, state risk.EquityCheckpoint) error {
	if state.Revision != expected+1 {
		return fmt.Errorf("invalid equity checkpoint revision advance")
	}
	payload, err := json.Marshal(state)
	if err != nil {
		return err
	}
	return s.backend.SaveRiskCheckpoint(ctx, circuitEquityCheckpointKey, expected, payload)
}
