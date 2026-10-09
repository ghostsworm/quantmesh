package strategy

import (
	"context"
	"encoding/json"
	"fmt"
)

type fundingCarryRecoveryCheckpointKey struct{}

// One serialized strategy operation owns this advancing source snapshot.
// Each successful conditional write becomes the source of its next write.
type fundingCarryRecoveryCheckpoint struct {
	store   RuntimeStateStore
	version int
	payload string
}

// Caller holds s.mu. Ordinary live financial operations keep their persistence
// contract; startup recovery must never use unconditional writes.
func (s *FundingCarryStrategy) persistRecoveryCheckpointLocked(ctx context.Context) error {
	source, ok := ctx.Value(fundingCarryRecoveryCheckpointKey{}).(*fundingCarryRecoveryCheckpoint)
	if !ok {
		return s.persistRuntimeStateLocked()
	}
	if s.runtimeStateStore != source.store {
		return fmt.Errorf("financial recovery checkpoint store changed")
	}
	writer, ok := source.store.(RuntimeStateConditionalWriter)
	if !ok {
		return fmt.Errorf("financial recovery requires atomic conditional checkpoint writes")
	}
	encoded, err := json.Marshal(s.runtimeStateSnapshotLocked())
	if err != nil {
		return fmt.Errorf("encode financial recovery checkpoint: %w", err)
	}
	saved, err := writer.CompareAndSwapRuntimeState(ctx, "funding_carry", source.version, source.payload, fundingCarryRuntimeStateVersion, string(encoded))
	if err != nil {
		return fmt.Errorf("conditional financial recovery checkpoint: %w", err)
	}
	if !saved {
		return fmt.Errorf("financial checkpoint changed during recovery")
	}
	source.version, source.payload = fundingCarryRuntimeStateVersion, string(encoded)
	s.runtimeStateErr = nil
	return nil
}
