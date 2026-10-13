package strategy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"reflect"
	"sort"
	"strings"
	"time"

	"quantmesh/accounting"
	"quantmesh/storage"
)

type ApplyFundingRecoveryOpeningBalanceRequest struct {
	EpisodeID               string                                    `json:"episode_id"`
	OperationID             string                                    `json:"operation_id"`
	ExpectedEpisodeRevision uint64                                    `json:"expected_episode_revision"`
	Snapshot                accounting.RecoveryOpeningBalanceSnapshot `json:"snapshot"`
	EvidenceHash            string                                    `json:"evidence_hash"`
	Actor                   string                                    `json:"actor"`
	AuditRef                string                                    `json:"audit_ref"`
	Confirmed               bool                                      `json:"confirmed"`
}

type ApplyFundingRecoveryOpeningBalanceResult struct {
	Receipt      accounting.RecoveryReceipt `json:"receipt"`
	Events       []accounting.RecoveryEvent `json:"events"`
	Replayed     bool                       `json:"replayed"`
	Projected    bool                       `json:"projected"`
	HoldRetained bool                       `json:"hold_retained"`
}

// ApplyFundingRecoveryOpeningBalance records an operator-attested, hashed
// opening snapshot against an already-held FundingCarry episode. It only
// establishes a ledger baseline; it never releases a hold or places orders.
func (s *FundingCarryStrategy) ApplyFundingRecoveryOpeningBalance(ctx context.Context, request ApplyFundingRecoveryOpeningBalanceRequest) (ApplyFundingRecoveryOpeningBalanceResult, error) {
	if ctx == nil || s == nil || !request.Confirmed || strings.TrimSpace(request.EpisodeID) == "" ||
		strings.TrimSpace(request.OperationID) == "" || request.ExpectedEpisodeRevision == 0 ||
		strings.TrimSpace(request.Actor) == "" || strings.TrimSpace(request.AuditRef) == "" {
		return ApplyFundingRecoveryOpeningBalanceResult{}, errors.New("incomplete explicit opening-balance authorization")
	}
	snapshotHash, err := request.Snapshot.CanonicalHash()
	if err != nil {
		return ApplyFundingRecoveryOpeningBalanceResult{}, err
	}
	if request.EvidenceHash != snapshotHash {
		return ApplyFundingRecoveryOpeningBalanceResult{}, errors.New("opening balance evidence hash does not match canonical snapshot")
	}
	binding, err := resolveFundingCarryRecoveryBindingFromStrategy(s)
	if err != nil {
		return ApplyFundingRecoveryOpeningBalanceResult{}, err
	}
	if binding.botID == "" || binding.generation.ID() == "" {
		return ApplyFundingRecoveryOpeningBalanceResult{}, ErrFundingCarryDurableRecoveryUnavailable
	}
	if result, replayed, retryErr := s.readFundingRecoveryOpeningBalanceRetry(ctx, binding, request, snapshotHash); retryErr != nil || replayed {
		return result, retryErr
	}
	episode, found, err := binding.store.GetRecoveryEpisode(ctx, request.EpisodeID)
	if err != nil {
		return ApplyFundingRecoveryOpeningBalanceResult{}, err
	}
	if !found {
		return ApplyFundingRecoveryOpeningBalanceResult{}, storage.ErrFundingRecoveryNotFound
	}
	if episode.Bot != binding.botID || episode.Strategy != "funding_carry" || episode.OwnerGeneration != binding.generation.ID() ||
		episode.Revision != request.ExpectedEpisodeRevision ||
		(episode.State != accounting.RecoveryBlocked && episode.State != accounting.RecoveryProjected) {
		return ApplyFundingRecoveryOpeningBalanceResult{}, accounting.ErrRecoveryConflict
	}
	if !sameFundingRecoveryScopeForOpeningBalance(binding.scopeKeys, episode.AccountScopes, request.Snapshot.AccountScope) {
		return ApplyFundingRecoveryOpeningBalanceResult{}, storage.ErrFundingRecoveryGenerationScopeMismatch
	}
	hold, held, err := binding.store.GetRecoveryHold(ctx, episode.ID, episode.RecoveryGeneration)
	if err != nil {
		return ApplyFundingRecoveryOpeningBalanceResult{}, err
	}
	if !held || hold.State != "held" || hold.Bot != episode.Bot || hold.Strategy != episode.Strategy || hold.Generation != episode.RecoveryGeneration {
		return ApplyFundingRecoveryOpeningBalanceResult{}, accounting.ErrRecoveryConflict
	}
	reader, ok := binding.stateStore.(RuntimeStateContextReader)
	if !ok {
		return ApplyFundingRecoveryOpeningBalanceResult{}, errors.New("opening balance requires context-aware runtime revision read")
	}
	version, payload, found, err := reader.LoadRuntimeStateContext(ctx, "funding_carry")
	if err != nil || !found || version != fundingCarryRuntimeStateVersion {
		return ApplyFundingRecoveryOpeningBalanceResult{}, errors.Join(errors.New("read current FundingCarry runtime revision"), err)
	}
	events := makeFundingRecoveryOpeningBalanceEvents(request, snapshotHash)
	operation := accounting.RecoveryOperation{
		EpisodeID: episode.ID, OperationID: request.OperationID, HashVersion: 1,
		ExpectedEpisodeRevision: episode.Revision, ExpectedRuntimeRevision: fundingCarryRuntimeRevision(version, payload),
		ExpectedEventRevisions: make(map[string]uint64, len(events)), Events: events,
		Actor: request.Actor, AuditRef: request.AuditRef, EvidenceHash: snapshotHash, EvidenceReadAt: time.Now().UTC(),
	}
	for _, event := range events {
		operation.ExpectedEventRevisions[event.ID] = 0
	}
	receipt, replayed, err := binding.store.ApplyRecoveryOperation(ctx, binding.generation, operation)
	if err != nil {
		return ApplyFundingRecoveryOpeningBalanceResult{}, err
	}
	result := ApplyFundingRecoveryOpeningBalanceResult{Receipt: receipt, Events: events, Replayed: replayed, HoldRetained: true}
	if err := s.projectFundingRecoveryFillOperation(ctx, binding, episode.ID, request.OperationID); err != nil {
		return result, fmt.Errorf("opening balance was durably recorded; runtime projection remains pending and hold is retained: %w", err)
	}
	result.Projected = true
	return result, nil
}

func (s *FundingCarryStrategy) readFundingRecoveryOpeningBalanceRetry(ctx context.Context, binding fundingCarryRecoveryBinding, request ApplyFundingRecoveryOpeningBalanceRequest, snapshotHash string) (ApplyFundingRecoveryOpeningBalanceResult, bool, error) {
	receipt, found, err := binding.store.GetRecoveryReceipt(ctx, request.EpisodeID, request.OperationID)
	if err != nil || !found {
		return ApplyFundingRecoveryOpeningBalanceResult{}, false, err
	}
	if !openingBalanceReceiptMatchesExpectedRevision(receipt, request.ExpectedEpisodeRevision) ||
		receipt.Actor != request.Actor || receipt.AuditRef != request.AuditRef || receipt.EvidenceHash != snapshotHash || receipt.RequestHash == "" {
		return ApplyFundingRecoveryOpeningBalanceResult{}, false, accounting.ErrRecoveryOperationConflict
	}
	events := makeFundingRecoveryOpeningBalanceEvents(request, snapshotHash)
	stored, err := binding.store.ListRecoveryEvents(ctx, request.EpisodeID)
	if err != nil {
		return ApplyFundingRecoveryOpeningBalanceResult{}, false, err
	}
	byID := make(map[string]accounting.RecoveryEvent, len(stored))
	for _, event := range stored {
		byID[event.ID] = event
	}
	for _, event := range events {
		if actual, ok := byID[event.ID]; !ok || !reflect.DeepEqual(actual, event) {
			return ApplyFundingRecoveryOpeningBalanceResult{}, false, accounting.ErrRecoveryOperationConflict
		}
	}
	result := ApplyFundingRecoveryOpeningBalanceResult{Receipt: receipt, Events: events, Replayed: true, HoldRetained: true}
	if err := s.projectFundingRecoveryFillOperation(ctx, binding, request.EpisodeID, request.OperationID); err != nil {
		return result, true, fmt.Errorf("read back recorded opening balance; projection remains pending and hold is retained: %w", err)
	}
	result.Projected = true
	return result, true, nil
}

func openingBalanceReceiptMatchesExpectedRevision(receipt accounting.RecoveryReceipt, expected uint64) bool {
	switch receipt.State {
	case accounting.RecoveryLedgerCommitted:
		return expected < ^uint64(0) && receipt.EpisodeRevision == expected+1
	case accounting.RecoveryProjected:
		return expected <= ^uint64(0)-2 && receipt.EpisodeRevision == expected+2
	default:
		return false
	}
}

func makeFundingRecoveryOpeningBalanceEvents(request ApplyFundingRecoveryOpeningBalanceRequest, snapshotHash string) []accounting.RecoveryEvent {
	assets := append([]accounting.RecoveryOpeningBalanceEntry(nil), request.Snapshot.Assets...)
	sort.Slice(assets, func(i, j int) bool { return assets[i].Asset < assets[j].Asset })
	events := make([]accounting.RecoveryEvent, 0, len(assets))
	for _, entry := range assets {
		asset := strings.ToUpper(strings.TrimSpace(entry.Asset))
		identity := request.EpisodeID + "\x00" + request.OperationID + "\x00" + asset
		idDigest := sha256.Sum256([]byte(identity))
		snapshotAt := request.Snapshot.SnapshotAt.UTC()
		events = append(events, accounting.RecoveryEvent{
			ID: "opening_balance:" + hex.EncodeToString(idDigest[:]), Source: accounting.RecoveryManualOpeningBalanceSource,
			Exchange: strings.ToLower(strings.TrimSpace(request.Snapshot.Exchange)), Market: strings.ToLower(strings.TrimSpace(request.Snapshot.Market)),
			AccountScope: request.Snapshot.AccountScope, Kind: accounting.RecoveryOpeningBalanceEvent,
			ExternalID: request.OperationID + ":" + asset, EpisodeID: request.EpisodeID, Asset: asset, Amount: entry.AmountRaw,
			SourceRevision: "manual_snapshot_v1", SourceHash: snapshotHash, ObservedAt: snapshotAt,
			OpeningBalance: &accounting.RecoveryOpeningBalanceEvidence{
				Actor: request.Actor, AuditRef: request.AuditRef, SnapshotHash: snapshotHash, SnapshotAt: snapshotAt,
			},
		})
	}
	return events
}

func sameFundingRecoveryScopeForOpeningBalance(ownerScopes, episodeScopes []string, target string) bool {
	owner, err := normalizeFundingCarryRecoveryScopes(ownerScopes)
	if err != nil {
		return false
	}
	episode, err := normalizeFundingCarryRecoveryScopes(episodeScopes)
	if err != nil || !equalFundingCarryRecoveryScopes(owner, episode) {
		return false
	}
	for _, scope := range owner {
		if scope == target {
			return true
		}
	}
	return false
}
