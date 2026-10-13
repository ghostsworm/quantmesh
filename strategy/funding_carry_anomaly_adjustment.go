package strategy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"quantmesh/accounting"
	"quantmesh/execution"
	"quantmesh/storage"
)

type ApplyFundingRecoveryAnomalyAdjustmentRequest struct {
	EpisodeID               string `json:"episode_id"`
	AnomalyID               string `json:"anomaly_id"`
	OperationID             string `json:"operation_id"`
	ExpectedEpisodeRevision uint64 `json:"expected_episode_revision"`
	ExpectedAnomalyRevision uint64 `json:"expected_anomaly_revision"`
	EvidenceHash            string `json:"evidence_hash"`
	Actor                   string `json:"actor"`
	AuditRef                string `json:"audit_ref"`
	Confirmed               bool   `json:"confirmed"`
}

type ApplyFundingRecoveryAnomalyAdjustmentResult struct {
	Receipt      accounting.RecoveryReceipt       `json:"receipt"`
	Anomaly      accounting.RecoverySourceAnomaly `json:"anomaly"`
	Events       []accounting.RecoveryEvent       `json:"events"`
	Replayed     bool                             `json:"replayed"`
	Projected    bool                             `json:"projected"`
	HoldRetained bool                             `json:"hold_retained"`
}

// ApplyFundingRecoveryAnomalyAdjustment appends exact, operator-audited
// adjustment facts. It does not rewrite original source facts or release a hold.
func (s *FundingCarryStrategy) ApplyFundingRecoveryAnomalyAdjustment(ctx context.Context, request ApplyFundingRecoveryAnomalyAdjustmentRequest) (ApplyFundingRecoveryAnomalyAdjustmentResult, error) {
	if ctx == nil || s == nil || !request.Confirmed || strings.TrimSpace(request.EpisodeID) == "" || strings.TrimSpace(request.AnomalyID) == "" ||
		strings.TrimSpace(request.OperationID) == "" || request.ExpectedEpisodeRevision == 0 || request.ExpectedAnomalyRevision == 0 ||
		!validFundingRecoveryCanonicalHash(request.EvidenceHash) || strings.TrimSpace(request.Actor) == "" || strings.TrimSpace(request.AuditRef) == "" {
		return ApplyFundingRecoveryAnomalyAdjustmentResult{}, errors.New("incomplete explicit source-anomaly adjustment authorization")
	}
	binding, err := resolveFundingCarryRecoveryBindingFromStrategy(s)
	if err != nil {
		return ApplyFundingRecoveryAnomalyAdjustmentResult{}, err
	}
	if result, found, err := s.readFundingRecoveryAnomalyAdjustmentRetry(ctx, binding, request); err != nil || found {
		return result, err
	}
	episode, found, err := binding.store.GetRecoveryEpisode(ctx, request.EpisodeID)
	if err != nil || !found {
		return ApplyFundingRecoveryAnomalyAdjustmentResult{}, errors.Join(errors.New("read funding recovery episode"), err)
	}
	if episode.Revision != request.ExpectedEpisodeRevision || episode.Bot != binding.botID || episode.Strategy != "funding_carry" ||
		episode.OwnerGeneration != binding.generation.ID() || !episode.HasCurrentOwnerTakeover() ||
		(episode.State != accounting.RecoveryBlocked && episode.State != accounting.RecoveryProjected) {
		return ApplyFundingRecoveryAnomalyAdjustmentResult{}, accounting.ErrRecoveryConflict
	}
	hold, held, err := binding.store.GetRecoveryHold(ctx, episode.ID, episode.RecoveryGeneration)
	if err != nil || !held || hold.State != "held" || hold.Bot != episode.Bot || hold.Strategy != episode.Strategy || hold.Generation != episode.RecoveryGeneration {
		return ApplyFundingRecoveryAnomalyAdjustmentResult{}, errors.Join(errors.New("exact durable recovery hold is required"), err)
	}
	scopes, err := normalizeFundingCarryRecoveryScopes(episode.AccountScopes)
	if err != nil || !equalFundingCarryRecoveryScopes(scopes, binding.scopeKeys) {
		return ApplyFundingRecoveryAnomalyAdjustmentResult{}, errors.New("episode account ownership scope changed")
	}
	s.mu.RLock()
	gate := s.openingGate
	s.mu.RUnlock()
	if gate == nil || !gate.HasBlock(execution.RecoveryEpisodeGateSource(episode.ID, episode.RecoveryGeneration)) {
		return ApplyFundingRecoveryAnomalyAdjustmentResult{}, errors.New("exact recovery episode Gate source is not active")
	}
	anomaly, found, err := binding.store.GetRecoverySourceAnomaly(ctx, episode.ID, request.AnomalyID)
	if err != nil || !found {
		return ApplyFundingRecoveryAnomalyAdjustmentResult{}, errors.Join(errors.New("read durable source anomaly"), err)
	}
	if anomaly.RecoveryGeneration != episode.RecoveryGeneration || anomaly.Revision != request.ExpectedAnomalyRevision ||
		anomaly.State != accounting.RecoverySourceAnomalyOpen || anomaly.Identity.AccountScope == "" ||
		!containsFundingCarryRecoveryScope(binding.scopeKeys, anomaly.Identity.AccountScope) {
		return ApplyFundingRecoveryAnomalyAdjustmentResult{}, accounting.ErrRecoveryConflict
	}
	reader, ok := binding.stateStore.(RuntimeStateContextReader)
	if !ok {
		return ApplyFundingRecoveryAnomalyAdjustmentResult{}, errors.New("anomaly adjustment requires context-aware runtime revision read")
	}
	version, payload, runtimeFound, err := reader.LoadRuntimeStateContext(ctx, "funding_carry")
	if err != nil || !runtimeFound || version != fundingCarryRuntimeStateVersion {
		return ApplyFundingRecoveryAnomalyAdjustmentResult{}, errors.Join(errors.New("read current FundingCarry runtime revision"), err)
	}
	now := time.Now().UTC()
	events := make([]accounting.RecoveryEvent, 0, len(anomaly.Facts))
	for _, fact := range anomaly.Facts {
		var originalAmount, originalHash, observedAmount, observedHash string
		var observedAt time.Time
		if fact.OriginalEvent != nil {
			originalAmount, originalHash = fact.OriginalEvent.Amount, fact.OriginalEvent.SourceHash
		}
		if fact.ObservedEvent != nil {
			observedAmount, observedHash, observedAt = fact.ObservedEvent.Amount, fact.ObservedEvent.SourceHash, fact.ObservedEvent.ObservedAt
		}
		evidence := accounting.RecoveryFundingAdjustmentEvidence{AnomalyID: anomaly.ID, OriginalExternalID: fact.ExternalID,
			OriginalSourceHash: originalHash, ObservedSourceHash: observedHash, OriginalAmountRaw: originalAmount, ObservedAmountRaw: observedAmount,
			ObservedRowAt: observedAt, DeltaAmountRaw: fact.DeltaAmount, EvidenceHash: request.EvidenceHash, Actor: request.Actor,
			AuditRef: request.AuditRef, ConfirmedAt: now}
		if err := evidence.Validate(); err != nil {
			return ApplyFundingRecoveryAnomalyAdjustmentResult{}, err
		}
		idHash := sha256.Sum256([]byte(anomaly.ID + "\x00" + fact.ExternalID))
		event := accounting.RecoveryEvent{ID: "funding-adjustment:" + hex.EncodeToString(idHash[:]), Source: accounting.RecoveryManualFundingAdjustmentSource,
			Exchange: anomaly.Identity.Exchange, Market: anomaly.Identity.Market, AccountScope: anomaly.Identity.AccountScope,
			Symbol: anomaly.Identity.Symbol, Kind: accounting.RecoveryFundingEvent, ExternalID: "adjustment:" + hex.EncodeToString(idHash[:]),
			RelatedExternalID: fact.ExternalID, EpisodeID: episode.ID, Asset: anomaly.Identity.Asset, Amount: fact.DeltaAmount,
			SourceRevision: fmt.Sprintf("%s:%d", anomaly.ID, anomaly.Revision), SourceHash: request.EvidenceHash, ObservedAt: now,
			FundingAdjustment: &evidence}
		if err := event.Validate(); err != nil {
			return ApplyFundingRecoveryAnomalyAdjustmentResult{}, err
		}
		events = append(events, event)
	}
	op := accounting.RecoveryOperation{EpisodeID: episode.ID, OperationID: request.OperationID, HashVersion: 1,
		ExpectedEpisodeRevision: episode.Revision, ExpectedRuntimeRevision: fundingCarryRuntimeRevision(version, payload),
		ExpectedChildRevisions: map[string]uint64{}, ExpectedEventRevisions: map[string]uint64{}, Events: events,
		SourceAnomalyID: anomaly.ID, ExpectedAnomalyRevision: anomaly.Revision, Actor: request.Actor, AuditRef: request.AuditRef,
		EvidenceHash: request.EvidenceHash, EvidenceReadAt: anomaly.DetectedAt}
	receipt, replayed, err := binding.store.ApplyRecoveryOperation(ctx, binding.generation, op)
	if err != nil {
		return ApplyFundingRecoveryAnomalyAdjustmentResult{}, err
	}
	result := ApplyFundingRecoveryAnomalyAdjustmentResult{Receipt: receipt, Events: events, Replayed: replayed, HoldRetained: true}
	if err := s.projectFundingRecoveryFillOperation(ctx, binding, episode.ID, request.OperationID); err != nil {
		return result, fmt.Errorf("funding adjustment committed; projection remains pending and hold is retained: %w", err)
	}
	result.Anomaly, found, err = binding.store.GetRecoverySourceAnomaly(ctx, episode.ID, anomaly.ID)
	if err != nil || !found || result.Anomaly.State != accounting.RecoverySourceAnomalyAdjustmentApplied {
		return result, errors.Join(errors.New("funding anomaly adjustment readback is incomplete"), err)
	}
	currentHold, held, err := binding.store.GetRecoveryHold(ctx, episode.ID, episode.RecoveryGeneration)
	if err != nil || !held || currentHold.State != "held" {
		return result, errors.Join(errors.New("funding anomaly adjustment lost its durable hold"), err)
	}
	result.Projected = true
	return result, nil
}

func (s *FundingCarryStrategy) readFundingRecoveryAnomalyAdjustmentRetry(ctx context.Context, binding fundingCarryRecoveryBinding,
	request ApplyFundingRecoveryAnomalyAdjustmentRequest) (ApplyFundingRecoveryAnomalyAdjustmentResult, bool, error) {
	receipt, found, err := binding.store.GetRecoveryReceipt(ctx, request.EpisodeID, request.OperationID)
	if err != nil || !found {
		return ApplyFundingRecoveryAnomalyAdjustmentResult{}, false, err
	}
	if receipt.Actor != request.Actor || receipt.AuditRef != request.AuditRef || receipt.EvidenceHash != request.EvidenceHash {
		return ApplyFundingRecoveryAnomalyAdjustmentResult{}, false, accounting.ErrRecoveryOperationConflict
	}
	outbox, found, err := binding.store.GetRecoveryOutbox(ctx, request.EpisodeID, request.OperationID)
	if err != nil || !found {
		return ApplyFundingRecoveryAnomalyAdjustmentResult{}, false, errors.Join(errors.New("anomaly adjustment receipt has no outbox"), err)
	}
	if err := storage.VerifyFundingRecoveryReceiptOutboxPair(receipt, outbox); err != nil {
		return ApplyFundingRecoveryAnomalyAdjustmentResult{}, false, fmt.Errorf("verify funding anomaly response-loss outbox receipt/outbox: %w", err)
	}
	if outbox.State == accounting.RecoveryProjected && receipt.EpisodeRevision != outbox.TargetEpisodeRevision+1 {
		return ApplyFundingRecoveryAnomalyAdjustmentResult{}, false, accounting.ErrRecoveryOperationConflict
	}
	var projection fundingCarryRecoveryProjectionPayload
	if err := storage.VerifyFundingRecoveryProjectionHash(outbox.ProjectionPayload, outbox.ProjectionHash); err != nil {
		return ApplyFundingRecoveryAnomalyAdjustmentResult{}, false, fmt.Errorf("verify funding anomaly response-loss outbox: %w", err)
	}
	if err := json.Unmarshal(outbox.ProjectionPayload, &projection); err != nil || outbox.TargetEpisodeRevision != request.ExpectedEpisodeRevision+1 {
		return ApplyFundingRecoveryAnomalyAdjustmentResult{}, false, errors.Join(errors.New("anomaly adjustment response-loss outbox is invalid"), err)
	}
	anomaly, found, err := binding.store.GetRecoverySourceAnomaly(ctx, request.EpisodeID, request.AnomalyID)
	if err != nil || !found || anomaly.AdjustmentOperationID != request.OperationID || anomaly.AdjustmentEvidenceHash != request.EvidenceHash {
		return ApplyFundingRecoveryAnomalyAdjustmentResult{}, false, errors.Join(accounting.ErrRecoveryOperationConflict, err)
	}
	projectionPairValid := receipt.State == accounting.RecoveryProjected && outbox.State == accounting.RecoveryProjected ||
		receipt.State == accounting.RecoveryLedgerCommitted && outbox.State == accounting.RecoveryProjectionPending
	if outbox.ExpectedRuntimeRevision == "" || !projectionPairValid || anomaly.Revision != request.ExpectedAnomalyRevision+1 ||
		anomaly.State != accounting.RecoverySourceAnomalyAdjustmentApplied {
		return ApplyFundingRecoveryAnomalyAdjustmentResult{}, false, accounting.ErrRecoveryOperationConflict
	}
	op := accounting.RecoveryOperation{EpisodeID: request.EpisodeID, OperationID: request.OperationID, HashVersion: 1,
		ExpectedEpisodeRevision: request.ExpectedEpisodeRevision, ExpectedRuntimeRevision: outbox.ExpectedRuntimeRevision,
		ExpectedChildRevisions: map[string]uint64{}, ExpectedEventRevisions: map[string]uint64{}, Events: projection.Events,
		SourceAnomalyID: request.AnomalyID, ExpectedAnomalyRevision: request.ExpectedAnomalyRevision, Actor: request.Actor,
		AuditRef: request.AuditRef, EvidenceHash: request.EvidenceHash, EvidenceReadAt: anomaly.DetectedAt}
	hash, err := op.CanonicalHash()
	if err != nil || hash != receipt.RequestHash {
		return ApplyFundingRecoveryAnomalyAdjustmentResult{}, false, errors.Join(accounting.ErrRecoveryOperationConflict, err)
	}
	result := ApplyFundingRecoveryAnomalyAdjustmentResult{Receipt: receipt, Anomaly: anomaly, Events: projection.Events, Replayed: true, Projected: true, HoldRetained: true}
	if err := s.projectFundingRecoveryFillOperation(ctx, binding, request.EpisodeID, request.OperationID); err != nil {
		return result, true, err
	}
	return result, true, nil
}
