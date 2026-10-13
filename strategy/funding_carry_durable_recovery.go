package strategy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"quantmesh/accounting"
	"quantmesh/exchange"
	"quantmesh/execution"
	"quantmesh/storage"
)

var ErrFundingCarryDurableRecoveryUnavailable = errors.New("funding_carry durable recovery binding unavailable")

// FundingCarryRecoveryProvider is implemented structurally by the runtime
// state adapter. The generation is a bearer capability for storage mutations;
// ID is used only as a persisted, non-secret owner marker.
type FundingCarryRecoveryProvider interface {
	FundingCarryRecoveryBinding() (storage.FundingRecoveryStore, storage.FundingCarryRuntimeGeneration, string, []string, error)
}

// CurrentFundingCarryRecoveryBinding returns the binding already owned by this
// strategy runtime. It never claims or rotates a runtime generation.
func (s *FundingCarryStrategy) CurrentFundingCarryRecoveryBinding() (storage.FundingRecoveryStore, storage.FundingCarryRuntimeGeneration, string, []string, error) {
	if s == nil {
		return nil, storage.FundingCarryRuntimeGeneration{}, "", nil, ErrFundingCarryDurableRecoveryUnavailable
	}
	s.mu.RLock()
	stateStore := s.runtimeStateStore
	s.mu.RUnlock()
	provider, ok := stateStore.(FundingCarryRecoveryProvider)
	if !ok || provider == nil {
		return nil, storage.FundingCarryRuntimeGeneration{}, "", nil, ErrFundingCarryDurableRecoveryUnavailable
	}
	return provider.FundingCarryRecoveryBinding()
}

// PreviewFundingRecoveryFills returns the exact retained venue fill snapshot
// for a currently held, taken-over FundingCarry child. It performs no writes.
func (s *FundingCarryStrategy) PreviewFundingRecoveryFills(ctx context.Context, episodeID, childID string) (FundingRecoveryFillPreview, error) {
	return s.previewFundingRecoveryFills(ctx, episodeID, childID)
}

// ApplyFundingRecoveryFills records only venue-derived fills after explicit
// operator confirmation. It never releases a hold or invokes execution.
func (s *FundingCarryStrategy) ApplyFundingRecoveryFills(ctx context.Context, request ApplyFundingRecoveryFillsRequest) (ApplyFundingRecoveryFillsResult, error) {
	return s.applyFundingRecoveryFills(ctx, request)
}

// AdjudicateRecoverySubmissionFound reads the original durable client order ID
// from the exact venue attached to this strategy. A missing order or any query
// error is not negative evidence and therefore leaves the attempt UNKNOWN.
func (s *FundingCarryStrategy) AdjudicateRecoverySubmissionFound(ctx context.Context, adjudication accounting.RecoverySubmissionAdjudication) (accounting.RecoverySubmissionAttempt, bool, error) {
	if ctx == nil || s == nil {
		return accounting.RecoverySubmissionAttempt{}, false, errors.New("recovery adjudication must omit client-supplied venue facts")
	}
	if strings.TrimSpace(adjudication.EpisodeID) == "" || strings.TrimSpace(adjudication.OperationID) == "" || strings.TrimSpace(adjudication.PermitID) == "" ||
		adjudication.ExpectedAttemptRevision == 0 || adjudication.ExpectedEpisodeRevision == 0 || strings.TrimSpace(adjudication.Actor) == "" ||
		strings.TrimSpace(adjudication.AuditRef) == "" || !adjudication.Confirmed || adjudication.Decision != accounting.RecoverySubmissionFound ||
		adjudication.VenueOrderID != "" || adjudication.EvidenceHash != "" {
		return accounting.RecoverySubmissionAttempt{}, false, errors.New("incomplete or client-supplied recovery adjudication input")
	}
	store, generation, botID, scopes, err := s.CurrentFundingCarryRecoveryBinding()
	if err != nil || nilFundingRecoveryStore(store) || generation.ID() == "" || botID == "" {
		return accounting.RecoverySubmissionAttempt{}, false, ErrFundingCarryDurableRecoveryUnavailable
	}
	episode, found, err := store.GetRecoveryEpisode(ctx, adjudication.EpisodeID)
	if err != nil {
		return accounting.RecoverySubmissionAttempt{}, false, err
	}
	if !found || episode.Bot != botID || episode.Strategy != "funding_carry" || episode.OwnerGeneration != generation.ID() || episode.State != accounting.RecoveryBlocked || episode.Revision != adjudication.ExpectedEpisodeRevision {
		return accounting.RecoverySubmissionAttempt{}, false, accounting.ErrRecoveryConflict
	}
	currentScopes, err := normalizeFundingCarryRecoveryScopes(scopes)
	if err != nil {
		return accounting.RecoverySubmissionAttempt{}, false, err
	}
	episodeScopes, err := normalizeFundingCarryRecoveryScopes(episode.AccountScopes)
	if err != nil || !equalFundingCarryRecoveryScopes(currentScopes, episodeScopes) {
		return accounting.RecoverySubmissionAttempt{}, false, storage.ErrFundingRecoveryGenerationScopeMismatch
	}
	hold, held, err := store.GetRecoveryHold(ctx, episode.ID, episode.RecoveryGeneration)
	if err != nil {
		return accounting.RecoverySubmissionAttempt{}, false, err
	}
	if !held || hold.State != "held" || hold.Bot != episode.Bot || hold.Strategy != episode.Strategy || hold.Generation != episode.RecoveryGeneration {
		return accounting.RecoverySubmissionAttempt{}, false, accounting.ErrRecoveryConflict
	}
	holdScopes, err := normalizeFundingCarryRecoveryScopes(hold.AccountScopes)
	if err != nil || !equalFundingCarryRecoveryScopes(currentScopes, holdScopes) {
		return accounting.RecoverySubmissionAttempt{}, false, storage.ErrFundingRecoveryGenerationScopeMismatch
	}
	prior, replayed, err := store.ReadRecoverySubmissionAdjudication(ctx, adjudication)
	if err != nil || replayed {
		return prior, replayed, err
	}
	attempt, found, err := store.GetRecoverySubmissionAttempt(ctx, episode.ID, adjudication.PermitID)
	if err != nil {
		return accounting.RecoverySubmissionAttempt{}, false, err
	}
	if !found || attempt.Revision != adjudication.ExpectedAttemptRevision ||
		(attempt.State != accounting.RecoverySubmissionUnknown && attempt.State != accounting.RecoverySubmissionAcknowledged) {
		// A response-loss retry may read the exact result of this operation. Keep
		// the original expected revision; SQL idempotency decides whether the
		// operation ID and complete request hash are identical.
		if !found || attempt.State != accounting.RecoverySubmissionFound || attempt.Revision != adjudication.ExpectedAttemptRevision+1 ||
			attempt.Actor != adjudication.Actor || attempt.AuditRef != adjudication.AuditRef {
			return accounting.RecoverySubmissionAttempt{}, false, accounting.ErrRecoveryConflict
		}
	}
	venue, err := s.recoveryVenue(attempt.Intent)
	if err != nil {
		return accounting.RecoverySubmissionAttempt{}, false, err
	}
	querier, ok := venue.(exchange.OrderByClientIDQuerier)
	if !ok {
		return accounting.RecoverySubmissionAttempt{}, false, errors.New("FundingCarry venue does not support authoritative client-order lookup")
	}
	order, err := querier.GetOrderByClientOrderID(ctx, attempt.Intent.Symbol, attempt.Intent.ClientOrderID)
	if err != nil {
		return accounting.RecoverySubmissionAttempt{}, false, fmt.Errorf("query original recovery client order ID: %w", err)
	}
	if order == nil {
		return accounting.RecoverySubmissionAttempt{}, false, errors.New("venue lookup returned no order; submission outcome remains UNKNOWN")
	}
	if order.OrderID <= 0 || order.ClientOrderID != attempt.Intent.ClientOrderID || order.Symbol != attempt.Intent.Symbol || string(order.Side) != attempt.Intent.Side ||
		string(order.Type) != attempt.Intent.OrderType || !recoveryVenueDecimalMatches(order.Quantity, attempt.Intent.Quantity) ||
		(strings.HasPrefix(strings.ToUpper(attempt.Intent.OrderType), "LIMIT") && !recoveryVenueDecimalMatches(order.Price, attempt.Intent.Price)) {
		return accounting.RecoverySubmissionAttempt{}, false, errors.New("venue order identity or immutable intent does not exactly match; attempt remains UNKNOWN")
	}
	venueOrderID := strconv.FormatInt(order.OrderID, 10)
	if attempt.State == accounting.RecoverySubmissionAcknowledged && attempt.VenueOrderID != venueOrderID {
		return accounting.RecoverySubmissionAttempt{}, false, errors.New("venue order ID differs from the acknowledged order; hold remains active")
	}
	if attempt.State == accounting.RecoverySubmissionFound && attempt.VenueOrderID != venueOrderID {
		return accounting.RecoverySubmissionAttempt{}, false, errors.New("venue order identity changed after adjudication; hold remains active")
	}
	evidence, err := json.Marshal(struct {
		EpisodeID, PermitID, IntentHash, Scope, VenueOrderID, ClientOrderID, Symbol, Side, Status string
		ExecutedQty, AvgPrice                                                                     float64
		UpdateTime                                                                                int64
	}{
		EpisodeID: attempt.EpisodeID, PermitID: attempt.PermitID, IntentHash: attempt.IntentHash, Scope: attempt.AccountScope,
		VenueOrderID: strconv.FormatInt(order.OrderID, 10), ClientOrderID: order.ClientOrderID, Symbol: order.Symbol,
		Side: string(order.Side), Status: string(order.Status), ExecutedQty: order.ExecutedQty, AvgPrice: order.AvgPrice, UpdateTime: order.UpdateTime,
	})
	if err != nil {
		return accounting.RecoverySubmissionAttempt{}, false, err
	}
	evidenceHash := sha256.Sum256(evidence)
	adjudication.VenueOrderID = venueOrderID
	adjudication.EvidenceHash = hex.EncodeToString(evidenceHash[:])
	return store.AdjudicateRecoverySubmission(ctx, generation, adjudication)
}

func recoveryVenueDecimalMatches(actual float64, expected string) bool {
	if math.IsNaN(actual) || math.IsInf(actual, 0) {
		return false
	}
	intended, err := strconv.ParseFloat(expected, 64)
	if err != nil || math.IsNaN(intended) || math.IsInf(intended, 0) {
		return false
	}
	return actual == intended
}

func (s *FundingCarryStrategy) recoveryVenue(intent accounting.RecoveryOrderIntent) (exchange.IExchange, error) {
	s.mu.RLock()
	futures, spot, margin := s.fut, s.spot, s.marginEx
	s.mu.RUnlock()
	var venue exchange.IExchange
	switch strings.ToLower(intent.Market) {
	case "futures":
		venue = futures
	case "spot":
		venue = spot
	case "margin", "spot_margin":
		venue = margin
	default:
		return nil, fmt.Errorf("unsupported FundingCarry recovery market %q", intent.Market)
	}
	if venue == nil || !strings.EqualFold(venue.GetName(), intent.Exchange) || !fundingRecoveryMarketMatches(intent.Market, venue.GetMarketType()) {
		return nil, errors.New("durable recovery exchange/market does not match the retained runtime")
	}
	return venue, nil
}

func fundingRecoveryMarketMatches(intentMarket, venueMarket string) bool {
	intentMarket = strings.ToLower(strings.TrimSpace(intentMarket))
	venueMarket = strings.ToLower(strings.TrimSpace(venueMarket))
	switch intentMarket {
	case "futures":
		return venueMarket == "futures"
	case "spot":
		return venueMarket == "spot"
	case "margin", "spot_margin":
		return venueMarket == "margin" || venueMarket == "spot_margin"
	default:
		return false
	}
}

type fundingCarryRecoveryProjectionMarker struct {
	EpisodeID               string `json:"episode_id"`
	EpisodeRevision         uint64 `json:"episode_revision"`
	OperationID             string `json:"operation_id"`
	ProjectionHash          string `json:"projection_hash"`
	ExpectedRuntimeRevision string `json:"expected_runtime_revision"`
	RuntimeBaseRevision     string `json:"runtime_base_revision,omitempty"`
	RuntimeResultRevision   string `json:"runtime_result_revision,omitempty"`
}

type fundingCarryRecoveryProjectionPayload struct {
	Episode      accounting.RecoveryEpisode       `json:"episode"`
	Children     []accounting.RecoveryChild       `json:"children"`
	Events       []accounting.RecoveryEvent       `json:"events"`
	SourceCursor *accounting.RecoverySourceCursor `json:"source_cursor,omitempty"`
}

type fundingCarryRecoveryBinding struct {
	store      storage.FundingRecoveryStore
	generation storage.FundingCarryRuntimeGeneration
	botID      string
	scopeKeys  []string
	stateStore RuntimeStateStore
}

func (s *FundingCarryStrategy) reconcileDurableFundingCarryRecovery(ctx context.Context) error {
	s.mu.RLock()
	stateStore := s.runtimeStateStore
	hasRecoveryMarker := len(s.recoveryProjectionMarkers) > 0 || len(s.recoveryFacts) > 0
	s.mu.RUnlock()
	if stateStore == nil {
		if hasRecoveryMarker {
			s.blockFundingCarryRecovery(execution.RecoveryEpisodeGateSource("binding-unavailable", 1))
			return ErrFundingCarryDurableRecoveryUnavailable
		}
		return nil
	}
	provider, ok := stateStore.(FundingCarryRecoveryProvider)
	if !ok {
		if hasRecoveryMarker {
			s.blockFundingCarryRecovery(execution.RecoveryEpisodeGateSource("binding-unavailable", 1))
			return fmt.Errorf("%w: strategy runtime adapter does not implement FundingCarryRecoveryProvider", ErrFundingCarryDurableRecoveryUnavailable)
		}
		return nil
	}
	binding, err := resolveFundingCarryRecoveryBinding(stateStore, provider)
	if err != nil {
		s.blockFundingCarryRecovery(execution.RecoveryEpisodeGateSource("binding-invalid", 1))
		return err
	}

	holds, err := binding.store.ListActiveRecoveryHolds(ctx)
	if err != nil {
		s.blockFundingCarryRecovery(execution.RecoveryEpisodeGateSource("read-failed", 1))
		return fmt.Errorf("list durable recovery holds: %w", err)
	}
	pending, err := binding.store.ListPendingRecoveryOutbox(ctx)
	if err != nil {
		s.blockFundingCarryRecovery(execution.RecoveryEpisodeGateSource("outbox-read-failed", 1))
		return fmt.Errorf("list durable recovery outbox: %w", err)
	}

	matchingHolds := make([]accounting.RecoveryHold, 0)
	for _, hold := range holds {
		matches, identityErr := binding.classifyOwner(hold.Bot, hold.Strategy, hold.AccountScopes)
		if identityErr != nil {
			s.blockFundingCarryRecovery(execution.RecoveryEpisodeGateSource(hold.EpisodeID, hold.Generation))
			return identityErr
		}
		if matches {
			matchingHolds = append(matchingHolds, hold)
			s.blockFundingCarryRecovery(execution.RecoveryEpisodeGateSource(hold.EpisodeID, hold.Generation))
		}
	}

	for _, outbox := range pending {
		episode, found, readErr := binding.store.GetRecoveryEpisode(ctx, outbox.EpisodeID)
		if readErr != nil {
			s.blockFundingCarryRecovery(execution.RecoveryEpisodeGateSource(outbox.EpisodeID, 1))
			return fmt.Errorf("read recovery episode for outbox %q: %w", outbox.OperationID, readErr)
		}
		if !found {
			s.blockFundingCarryRecovery(execution.RecoveryEpisodeGateSource(outbox.EpisodeID, 1))
			return fmt.Errorf("recovery outbox %q has no parent episode", outbox.OperationID)
		}
		matches, identityErr := binding.classifyOwner(episode.Bot, episode.Strategy, episode.AccountScopes)
		if identityErr != nil {
			s.blockFundingCarryRecovery(execution.RecoveryEpisodeGateSource(episode.ID, episode.RecoveryGeneration))
			return identityErr
		}
		if !matches {
			continue
		}
		if episode.OwnerGeneration != binding.generation.ID() {
			s.blockFundingCarryRecovery(execution.RecoveryEpisodeGateSource(episode.ID, episode.RecoveryGeneration))
			heldByCurrentBot := false
			for _, hold := range matchingHolds {
				if hold.EpisodeID == episode.ID && hold.Generation == episode.RecoveryGeneration && hold.State == "held" {
					heldByCurrentBot = true
					break
				}
			}
			if !heldByCurrentBot {
				return fmt.Errorf("recovery episode %q belongs to a different runtime generation without a matching active hold", episode.ID)
			}
			if err := validateFundingCarryPendingOutboxForTakeover(binding, episode, outbox); err != nil {
				return fmt.Errorf("old-owner recovery outbox %q cannot be retained for explicit takeover: %w", outbox.OperationID, err)
			}
			continue
		}
		if err := s.projectFundingCarryRecoveryOutbox(ctx, binding, episode, outbox); err != nil {
			s.blockFundingCarryRecovery(execution.RecoveryEpisodeGateSource(episode.ID, episode.RecoveryGeneration))
			return err
		}
	}

	if len(matchingHolds) > 0 {
		refs, eligible := s.durableRecoveryRetentionEpisodes(ctx, binding, matchingHolds)
		if eligible {
			return &FundingCarryDurableRecoveryRequiredError{Episodes: refs}
		}
		return fmt.Errorf("%d active durable FundingCarry recovery hold(s) remain; human reconciliation and explicit authorization are required", len(matchingHolds))
	}
	return nil
}

// ProjectRecoveryOutboxAfterTakeover advances only the single pre-existing,
// hash-validated projection owned by the episode explicitly taken over by this
// runtime. It never queries a venue, writes fill/fee events, releases a hold,
// or issues an execution permit.
func (s *FundingCarryStrategy) ProjectRecoveryOutboxAfterTakeover(ctx context.Context, episodeID, takeoverOperationID string) (bool, error) {
	if ctx == nil || s == nil || strings.TrimSpace(episodeID) == "" || strings.TrimSpace(takeoverOperationID) == "" {
		return false, errors.New("FundingCarry recovery outbox takeover projection requires context, episode ID and takeover operation ID")
	}
	_, _, botID, _, err := s.CurrentFundingCarryRecoveryBinding()
	if err != nil {
		return false, err
	}
	s.mu.RLock()
	stateStore := s.runtimeStateStore
	s.mu.RUnlock()
	provider, ok := stateStore.(FundingCarryRecoveryProvider)
	if !ok || provider == nil {
		return false, ErrFundingCarryDurableRecoveryUnavailable
	}
	binding, err := resolveFundingCarryRecoveryBinding(stateStore, provider)
	if err != nil {
		return false, err
	}
	if binding.botID != botID {
		return false, storage.ErrFundingRecoveryGenerationScopeMismatch
	}
	episode, found, err := binding.store.GetRecoveryEpisode(ctx, episodeID)
	if err != nil || !found {
		return false, errors.Join(errors.New("read explicitly taken-over FundingCarry episode"), err)
	}
	if episode.Bot != binding.botID || episode.Strategy != "funding_carry" || episode.OwnerGeneration != binding.generation.ID() ||
		!validFundingCarryRetainableEpisode(episode) || !episode.HasCurrentOwnerTakeover() {
		return false, accounting.ErrRecoveryConflict
	}
	takeover := episode.OwnerTakeovers[len(episode.OwnerTakeovers)-1]
	if takeover.OperationID != takeoverOperationID || takeover.OwnerGeneration != binding.generation.ID() {
		return false, accounting.ErrRecoveryConflict
	}
	takeoverReceipt, receiptFound, err := binding.store.GetRecoveryReceipt(ctx, episode.ID, takeover.OperationID)
	if err != nil || !receiptFound || takeoverReceipt.State != accounting.RecoveryBlocked || takeoverReceipt.OwnerGeneration != takeover.OwnerGeneration ||
		takeoverReceipt.PreviousOwnerGeneration != takeover.PreviousOwnerGeneration || takeoverReceipt.EpisodeRevision != takeover.EpisodeRevision {
		return false, errors.Join(errors.New("audited owner takeover receipt does not match the current episode"), err)
	}
	episodeScopes, err := normalizeFundingCarryRecoveryScopes(episode.AccountScopes)
	if err != nil || !equalFundingCarryRecoveryScopes(binding.scopeKeys, episodeScopes) {
		return false, storage.ErrFundingRecoveryGenerationScopeMismatch
	}
	hold, held, err := binding.store.GetRecoveryHold(ctx, episode.ID, episode.RecoveryGeneration)
	if err != nil || !held || hold.State != "held" || hold.Bot != episode.Bot || hold.Strategy != episode.Strategy || hold.Generation != episode.RecoveryGeneration {
		return false, errors.Join(errors.New("explicit takeover projection requires the original active hold"), err)
	}
	holdScopes, err := normalizeFundingCarryRecoveryScopes(hold.AccountScopes)
	if err != nil || !equalFundingCarryRecoveryScopes(binding.scopeKeys, holdScopes) {
		return false, storage.ErrFundingRecoveryGenerationScopeMismatch
	}
	outboxes, err := binding.store.ListRecoveryOutboxes(ctx, episode.ID)
	if err != nil {
		return false, fmt.Errorf("list existing FundingCarry recovery outboxes after takeover: %w", err)
	}
	var candidate *accounting.RecoveryOutbox
	targetRevision := takeover.EpisodeRevision - 1
	for i := range outboxes {
		if outboxes[i].TargetEpisodeRevision != targetRevision {
			continue
		}
		if candidate != nil {
			return false, errors.New("multiple recovery outboxes bind the pre-takeover episode revision")
		}
		copy := outboxes[i]
		candidate = &copy
	}
	if candidate == nil {
		for _, outbox := range outboxes {
			if outbox.State == accounting.RecoveryProjectionPending {
				return false, errors.New("pending recovery outbox does not match the exact pre-takeover episode revision")
			}
		}
		return false, nil
	}
	projection, err := validateFundingCarryTakeoverOutboxProof(binding, takeover, *candidate)
	if err != nil {
		return false, err
	}
	switch candidate.State {
	case accounting.RecoveryProjectionPending:
		if episode.State != accounting.RecoveryBlocked || episode.Revision != takeover.EpisodeRevision {
			return false, accounting.ErrRecoveryConflict
		}
		if err = validateFundingCarryTakeoverOutboxCandidate(binding, episode, *candidate); err == nil {
			err = s.projectFundingCarryRecoveryOutbox(ctx, binding, episode, *candidate)
		}
		if err != nil {
			s.blockFundingCarryRecovery(execution.RecoveryEpisodeGateSource(episode.ID, episode.RecoveryGeneration))
			return false, err
		}
		return true, nil
	case accounting.RecoveryProjected:
		marker := fundingCarryRecoveryProjectionMarker{EpisodeID: episode.ID, EpisodeRevision: candidate.TargetEpisodeRevision,
			OperationID: candidate.OperationID, ProjectionHash: candidate.ProjectionHash, ExpectedRuntimeRevision: candidate.ExpectedRuntimeRevision}
		reader, ok := binding.stateStore.(RuntimeStateContextReader)
		if !ok {
			return false, errors.New("completed takeover projection requires runtime readback")
		}
		version, payload, found, readErr := reader.LoadRuntimeStateContext(ctx, "funding_carry")
		if readErr != nil || !found || version != fundingCarryRuntimeStateVersion {
			return false, errors.Join(errors.New("completed takeover projection runtime readback unavailable"), readErr)
		}
		state, decodeErr := decodeFundingCarryRuntimeStateForRecovery(version, payload, s.fut.GetName(), s.spot.GetName(), s.symbol, true)
		if decodeErr != nil || !fundingCarryRecoveryProjectionPresent(state, marker, projection.Events, projection.SourceCursor) {
			return false, errors.Join(errors.New("completed takeover projection marker readback mismatch"), decodeErr)
		}
		return false, nil
	default:
		return false, errors.New("pre-takeover recovery outbox is in an unsupported state")
	}
}

func validateFundingCarryTakeoverOutboxProof(binding fundingCarryRecoveryBinding, takeover accounting.RecoveryOwnerTakeoverRecord, outbox accounting.RecoveryOutbox) (fundingCarryRecoveryProjectionPayload, error) {
	if (outbox.State != accounting.RecoveryProjectionPending && outbox.State != accounting.RecoveryProjected) || outbox.EpisodeID == "" || outbox.OperationID == "" ||
		outbox.TargetEpisodeRevision == 0 || outbox.ExpectedRuntimeRevision == "" || outbox.ProjectionHash == "" || outbox.TargetEpisodeRevision+1 != takeover.EpisodeRevision {
		return fundingCarryRecoveryProjectionPayload{}, errors.New("pre-takeover outbox identity/revision does not match takeover audit")
	}
	if outbox.State == accounting.RecoveryProjected && (outbox.AppliedOperationID != outbox.OperationID || outbox.AppliedRevision != outbox.TargetEpisodeRevision || outbox.AppliedHash != outbox.ProjectionHash) {
		return fundingCarryRecoveryProjectionPayload{}, errors.New("completed outbox receipt does not match its immutable projection")
	}
	digest := sha256.Sum256(outbox.ProjectionPayload)
	if hex.EncodeToString(digest[:]) != outbox.ProjectionHash {
		return fundingCarryRecoveryProjectionPayload{}, errors.New("pre-takeover outbox payload hash mismatch")
	}
	var projection fundingCarryRecoveryProjectionPayload
	if err := json.Unmarshal(outbox.ProjectionPayload, &projection); err != nil {
		return projection, fmt.Errorf("decode pre-takeover outbox projection: %w", err)
	}
	prior := projection.Episode
	scopes, err := normalizeFundingCarryRecoveryScopes(prior.AccountScopes)
	if err != nil || !equalFundingCarryRecoveryScopes(binding.scopeKeys, scopes) {
		return projection, storage.ErrFundingRecoveryGenerationScopeMismatch
	}
	if prior.ID != outbox.EpisodeID || prior.Bot != binding.botID || prior.Strategy != "funding_carry" || prior.OwnerGeneration != takeover.PreviousOwnerGeneration ||
		prior.Revision != outbox.TargetEpisodeRevision || prior.State != accounting.RecoveryLedgerCommitted {
		return projection, errors.New("pre-takeover outbox parent identity does not match the audit record")
	}
	for _, event := range projection.Events {
		if event.Kind == accounting.RecoveryFillEvent || event.Kind == accounting.RecoveryFeeEvent || event.FillEvidence != nil {
			return projection, errors.New("takeover outbox projection containing fill or fee evidence requires the accounting workflow")
		}
	}
	if err := validateFundingCarryProjectedSourceCursor(projection.SourceCursor, prior.ID, binding.scopeKeys); err != nil {
		return projection, err
	}
	return projection, nil
}

func validateFundingCarryTakeoverOutboxCandidate(binding fundingCarryRecoveryBinding, current accounting.RecoveryEpisode, outbox accounting.RecoveryOutbox) error {
	if outbox.State != accounting.RecoveryProjectionPending || outbox.EpisodeID != current.ID || outbox.OperationID == "" || outbox.ExpectedRuntimeRevision == "" || outbox.TargetEpisodeRevision == 0 || outbox.ProjectionHash == "" {
		return errors.New("outbox identity or expected runtime revision is incomplete")
	}
	digest := sha256.Sum256(outbox.ProjectionPayload)
	if hex.EncodeToString(digest[:]) != outbox.ProjectionHash {
		return errors.New("outbox payload hash mismatch")
	}
	var projection fundingCarryRecoveryProjectionPayload
	if err := json.Unmarshal(outbox.ProjectionPayload, &projection); err != nil {
		return fmt.Errorf("decode outbox projection: %w", err)
	}
	prior := projection.Episode
	priorScopes, err := normalizeFundingCarryRecoveryScopes(prior.AccountScopes)
	if err != nil || !equalFundingCarryRecoveryScopes(binding.scopeKeys, priorScopes) {
		return storage.ErrFundingRecoveryGenerationScopeMismatch
	}
	if prior.ID != current.ID || prior.Bot != binding.botID || prior.Strategy != "funding_carry" || prior.OwnerGeneration == "" || prior.OwnerGeneration == binding.generation.ID() ||
		prior.Revision != outbox.TargetEpisodeRevision || prior.State != accounting.RecoveryLedgerCommitted || current.OwnerGeneration != binding.generation.ID() ||
		current.State != accounting.RecoveryBlocked || !current.HasCurrentOwnerTakeover() || current.Revision != prior.Revision+1 {
		return errors.New("outbox parent does not match the explicitly taken-over prior owner snapshot")
	}
	takeover := current.OwnerTakeovers[len(current.OwnerTakeovers)-1]
	if takeover.PreviousOwnerGeneration != prior.OwnerGeneration || takeover.OwnerGeneration != current.OwnerGeneration || takeover.EpisodeRevision != current.Revision {
		return errors.New("outbox parent owner is not the previous owner in the latest audited takeover")
	}
	// The immutable pre-takeover episode must match byte-decoded business state;
	// only owner, revision, state, timestamp, and the appended audit record differ.
	comparable := current
	comparable.OwnerGeneration = prior.OwnerGeneration
	comparable.Revision = prior.Revision
	comparable.State = prior.State
	comparable.UpdatedAt = prior.UpdatedAt
	comparable.OwnerTakeovers = append([]accounting.RecoveryOwnerTakeoverRecord(nil), current.OwnerTakeovers[:len(current.OwnerTakeovers)-1]...)
	if !reflect.DeepEqual(comparable, prior) {
		return errors.New("outbox parent business state changed after its hashed snapshot")
	}
	for _, event := range projection.Events {
		if event.Kind == accounting.RecoveryFillEvent || event.Kind == accounting.RecoveryFeeEvent || event.FillEvidence != nil {
			return errors.New("takeover outbox projection containing fill or fee evidence requires the accounting workflow")
		}
	}
	if err := validateFundingCarryProjectedSourceCursor(projection.SourceCursor, prior.ID, binding.scopeKeys); err != nil {
		return err
	}
	return nil
}

func validateFundingCarryPendingOutboxForTakeover(binding fundingCarryRecoveryBinding, episode accounting.RecoveryEpisode, outbox accounting.RecoveryOutbox) error {
	if outbox.State != accounting.RecoveryProjectionPending || outbox.EpisodeID != episode.ID || outbox.OperationID == "" || outbox.ExpectedRuntimeRevision == "" || outbox.TargetEpisodeRevision == 0 || outbox.ProjectionHash == "" {
		return errors.New("pending outbox identity or expected runtime revision is incomplete")
	}
	digest := sha256.Sum256(outbox.ProjectionPayload)
	if hex.EncodeToString(digest[:]) != outbox.ProjectionHash {
		return errors.New("pending outbox payload hash mismatch")
	}
	var projection fundingCarryRecoveryProjectionPayload
	if err := json.Unmarshal(outbox.ProjectionPayload, &projection); err != nil {
		return fmt.Errorf("decode pending outbox projection: %w", err)
	}
	prior := projection.Episode
	priorScopes, err := normalizeFundingCarryRecoveryScopes(prior.AccountScopes)
	if err != nil || !equalFundingCarryRecoveryScopes(binding.scopeKeys, priorScopes) {
		return storage.ErrFundingRecoveryGenerationScopeMismatch
	}
	if episode.State != accounting.RecoveryLedgerCommitted || episode.Revision != outbox.TargetEpisodeRevision || prior.ID != episode.ID || prior.Bot != binding.botID || prior.Strategy != "funding_carry" ||
		prior.OwnerGeneration != episode.OwnerGeneration || prior.Revision != episode.Revision || prior.State != accounting.RecoveryLedgerCommitted || !reflect.DeepEqual(prior, episode) {
		return errors.New("pending outbox does not match the complete persisted old-owner episode snapshot")
	}
	for _, event := range projection.Events {
		if event.Kind == accounting.RecoveryFillEvent || event.Kind == accounting.RecoveryFeeEvent || event.FillEvidence != nil {
			return errors.New("pending takeover outbox contains fill or fee evidence")
		}
	}
	if err := validateFundingCarryProjectedSourceCursor(projection.SourceCursor, prior.ID, binding.scopeKeys); err != nil {
		return err
	}
	return nil
}

func (s *FundingCarryStrategy) durableRecoveryRetentionEpisodes(ctx context.Context, binding fundingCarryRecoveryBinding, holds []accounting.RecoveryHold) ([]FundingCarryDurableRecoveryEpisode, bool) {
	refs := make([]FundingCarryDurableRecoveryEpisode, 0, len(holds))
	for _, hold := range holds {
		if hold.State != "held" || hold.ID == "" || hold.EpisodeID == "" || hold.Generation == 0 || hold.Bot != binding.botID || hold.Strategy != "funding_carry" {
			return nil, false
		}
		episode, found, err := binding.store.GetRecoveryEpisode(ctx, hold.EpisodeID)
		if err != nil || !found || !validFundingCarryRetainableEpisode(episode) || episode.ID != hold.EpisodeID || episode.Bot != binding.botID || episode.Strategy != "funding_carry" || episode.RecoveryGeneration != hold.Generation {
			return nil, false
		}
		episodeScopes, err := normalizeFundingCarryRecoveryScopes(episode.AccountScopes)
		if err != nil || !equalFundingCarryRecoveryScopes(binding.scopeKeys, episodeScopes) {
			return nil, false
		}
		holdScopes, err := normalizeFundingCarryRecoveryScopes(hold.AccountScopes)
		if err != nil || !equalFundingCarryRecoveryScopes(binding.scopeKeys, holdScopes) {
			return nil, false
		}
		attempts, err := binding.store.ListRecoverySubmissionAttempts(ctx, episode.ID)
		if err != nil {
			return nil, false
		}
		for _, attempt := range attempts {
			if attempt.Validate() != nil || attempt.EpisodeID != episode.ID || attempt.Generation != episode.RecoveryGeneration {
				return nil, false
			}
			if !containsFundingCarryScope(binding.scopeKeys, attempt.AccountScope) {
				return nil, false
			}
		}
		refs = append(refs, FundingCarryDurableRecoveryEpisode{ID: episode.ID, Generation: episode.RecoveryGeneration})
	}
	sort.Slice(refs, func(i, j int) bool { return refs[i].ID < refs[j].ID })
	return refs, len(refs) > 0
}

func validFundingCarryRetainableEpisode(episode accounting.RecoveryEpisode) bool {
	switch episode.State {
	case accounting.RecoveryPrepared, accounting.RecoveryBlocked, accounting.RecoveryReopened,
		accounting.RecoveryLedgerCommitted, accounting.RecoveryProjectionPending, accounting.RecoveryProjected, accounting.RecoveryStale:
	default:
		return false
	}
	if episode.ID == "" || episode.Bot == "" || episode.Strategy != "funding_carry" || episode.OwnerGeneration == "" || episode.Revision == 0 || episode.RecoveryGeneration == 0 {
		return false
	}
	// Validate the immutable identities/action plan while allowing durable
	// nonterminal states created after the initial prepared phase.
	validated := episode
	validated.State = accounting.RecoveryPrepared
	return validated.Validate() == nil
}

func resolveFundingCarryRecoveryBinding(stateStore RuntimeStateStore, provider FundingCarryRecoveryProvider) (fundingCarryRecoveryBinding, error) {
	store, generation, botID, scopes, err := provider.FundingCarryRecoveryBinding()
	if err != nil {
		return fundingCarryRecoveryBinding{}, fmt.Errorf("%w: %v", ErrFundingCarryDurableRecoveryUnavailable, err)
	}
	if nilFundingRecoveryStore(store) || botID == "" || generation.ID() == "" {
		return fundingCarryRecoveryBinding{}, ErrFundingCarryDurableRecoveryUnavailable
	}
	normalized, err := normalizeFundingCarryRecoveryScopes(scopes)
	if err != nil {
		return fundingCarryRecoveryBinding{}, fmt.Errorf("invalid current FundingCarry account scopes: %w", err)
	}
	generationScopes, err := normalizeFundingCarryRecoveryScopes(generation.ScopeKeys())
	if err != nil || !equalFundingCarryRecoveryScopes(normalized, generationScopes) {
		return fundingCarryRecoveryBinding{}, errors.New("FundingCarry provider scopes differ from runtime generation scopes")
	}
	return fundingCarryRecoveryBinding{store: store, generation: generation, botID: botID, scopeKeys: normalized, stateStore: stateStore}, nil
}

func nilFundingRecoveryStore(store storage.FundingRecoveryStore) bool {
	if store == nil {
		return true
	}
	value := reflect.ValueOf(store)
	return value.Kind() == reflect.Ptr && value.IsNil()
}

func normalizeFundingCarryRecoveryScopes(scopes []string) ([]string, error) {
	if len(scopes) == 0 {
		return nil, errors.New("FundingCarry recovery requires non-empty account scopes")
	}
	normalized := append([]string(nil), scopes...)
	for _, scope := range normalized {
		decoded, err := hex.DecodeString(scope)
		if err != nil || len(decoded) != sha256.Size || strings.ToLower(scope) != scope {
			return nil, fmt.Errorf("account scope %q is not a normalized hashed scope key", scope)
		}
	}
	sort.Strings(normalized)
	for i := 1; i < len(normalized); i++ {
		if normalized[i] == normalized[i-1] {
			return nil, errors.New("FundingCarry recovery contains duplicate account scopes")
		}
	}
	return normalized, nil
}

func equalFundingCarryRecoveryScopes(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func containsFundingCarryRecoveryScope(scopes []string, target string) bool {
	for _, scope := range scopes {
		if scope == target {
			return true
		}
	}
	return false
}

func (b fundingCarryRecoveryBinding) classifyOwner(botID, strategyName string, scopes []string) (bool, error) {
	if strategyName != "funding_carry" {
		return false, nil
	}
	ownerScopes, err := normalizeFundingCarryRecoveryScopes(scopes)
	if err != nil {
		return false, fmt.Errorf("invalid durable FundingCarry owner scopes: %w", err)
	}
	if botID == b.botID {
		if !equalFundingCarryRecoveryScopes(ownerScopes, b.scopeKeys) {
			return false, fmt.Errorf("FundingCarry recovery owner scope mismatch for bot %q", botID)
		}
		return true, nil
	}
	overlaps := false
	for _, ownerScope := range ownerScopes {
		if containsFundingCarryScope(b.scopeKeys, ownerScope) {
			overlaps = true
			break
		}
	}
	if !overlaps {
		return false, nil
	}
	return false, fmt.Errorf("FundingCarry recovery bot mismatch for overlapping account scopes")
}

func (s *FundingCarryStrategy) projectFundingCarryRecoveryOutbox(ctx context.Context, binding fundingCarryRecoveryBinding, episode accounting.RecoveryEpisode, outbox accounting.RecoveryOutbox) error {
	if outbox.State != accounting.RecoveryProjectionPending || outbox.EpisodeID != episode.ID || outbox.OperationID == "" || outbox.ExpectedRuntimeRevision == "" || outbox.TargetEpisodeRevision == 0 || outbox.ProjectionHash == "" {
		return errors.New("FundingCarry recovery outbox identity or expected runtime revision is incomplete")
	}
	projectionHash := sha256.Sum256(outbox.ProjectionPayload)
	if hex.EncodeToString(projectionHash[:]) != outbox.ProjectionHash {
		return errors.New("FundingCarry recovery outbox projection hash mismatch")
	}
	var projection fundingCarryRecoveryProjectionPayload
	if err := json.Unmarshal(outbox.ProjectionPayload, &projection); err != nil {
		return fmt.Errorf("decode FundingCarry recovery projection: %w", err)
	}
	projectionScopes, scopeErr := normalizeFundingCarryRecoveryScopes(projection.Episode.AccountScopes)
	if scopeErr != nil || !equalFundingCarryRecoveryScopes(binding.scopeKeys, projectionScopes) {
		return storage.ErrFundingRecoveryGenerationScopeMismatch
	}
	normalProjection := episode.Revision == outbox.TargetEpisodeRevision && episode.State == accounting.RecoveryLedgerCommitted &&
		projection.Episode.OwnerGeneration == binding.generation.ID() && projection.Episode.Revision == outbox.TargetEpisodeRevision
	takenOverProjection := episode.Revision == outbox.TargetEpisodeRevision+1 && episode.State == accounting.RecoveryBlocked && episode.HasCurrentOwnerTakeover() &&
		projection.Episode.Revision == outbox.TargetEpisodeRevision && validateFundingCarryTakeoverOutboxCandidate(binding, episode, outbox) == nil
	if projection.Episode.ID != episode.ID || projection.Episode.Bot != binding.botID || projection.Episode.Strategy != "funding_carry" || (!normalProjection && !takenOverProjection) {
		return errors.New("FundingCarry recovery projection parent identity/revision mismatch")
	}
	if err := validateFundingCarryRecoveryOutboxPredecessors(ctx, binding.store, outbox); err != nil {
		return err
	}
	for _, child := range projection.Children {
		if err := child.Validate(); err != nil {
			return fmt.Errorf("invalid FundingCarry recovery child %q: %w", child.ID, err)
		}
		if child.EpisodeID != episode.ID || child.Bot != binding.botID || child.StrategyName != "funding_carry" || !containsFundingCarryScope(binding.scopeKeys, child.AccountScope) {
			return fmt.Errorf("FundingCarry recovery child %q owner identity mismatch", child.ID)
		}
	}
	for _, event := range projection.Events {
		if err := event.Validate(); err != nil {
			return fmt.Errorf("invalid FundingCarry recovery event %q: %w", event.ID, err)
		}
		if event.EpisodeID != episode.ID || !containsFundingCarryScope(binding.scopeKeys, event.AccountScope) {
			return fmt.Errorf("FundingCarry recovery event %q owner identity mismatch", event.ID)
		}
	}

	reader, ok := binding.stateStore.(RuntimeStateContextReader)
	if !ok {
		return errors.New("FundingCarry recovery projection requires context-aware runtime readback")
	}
	writer, ok := binding.stateStore.(RuntimeStateConditionalWriter)
	if !ok {
		return errors.New("FundingCarry recovery projection requires conditional runtime CAS")
	}
	version, payload, found, err := reader.LoadRuntimeStateContext(ctx, "funding_carry")
	if err != nil {
		return fmt.Errorf("read FundingCarry runtime snapshot before projection: %w", err)
	}
	if !found || version != fundingCarryRuntimeStateVersion {
		return fmt.Errorf("FundingCarry recovery projection requires runtime schema %d; found=%v version=%d", fundingCarryRuntimeStateVersion, found, version)
	}
	state, err := decodeFundingCarryRuntimeStateForRecovery(version, payload, s.fut.GetName(), s.spot.GetName(), s.symbol, true)
	if err != nil {
		return fmt.Errorf("validate FundingCarry runtime snapshot before projection: %w", err)
	}
	if state.RecoveryProjectionSchema != fundingCarryRecoveryProjectionSchemaVersion {
		return errors.New("legacy FundingCarry runtime snapshot cannot accept durable recovery projection")
	}
	if state.MarginAccountScope != "" && !containsFundingCarryScope(binding.scopeKeys, state.MarginAccountScope) {
		return errors.New("FundingCarry runtime snapshot margin scope is outside the current owner generation")
	}
	marker := fundingCarryRecoveryProjectionMarker{EpisodeID: episode.ID, EpisodeRevision: outbox.TargetEpisodeRevision, OperationID: outbox.OperationID, ProjectionHash: outbox.ProjectionHash, ExpectedRuntimeRevision: outbox.ExpectedRuntimeRevision}
	if err := validateFundingCarryProjectedSourceCursor(projection.SourceCursor, episode.ID, binding.scopeKeys); err != nil {
		return err
	}
	const maxProjectionCASAttempts = 3
	projectedReadback := false
	for attempt := 0; attempt < maxProjectionCASAttempts; attempt++ {
		if fundingCarryRecoveryProjectionPresent(state, marker, projection.Events, projection.SourceCursor) {
			projectedReadback = true
			break
		}
		if err := validateFundingCarryRecoveryEpisodeFence(ctx, binding, episode, outbox, takenOverProjection); err != nil {
			return err
		}
		currentRevision := fundingCarryRuntimeRevision(version, payload)
		if currentRevision != outbox.ExpectedRuntimeRevision && !fundingCarryCanRebaseAcrossEpisodes(state, marker, currentRevision) {
			return errors.New("FundingCarry recovery outbox runtime revision changed without a verified different-episode projection")
		}
		if err := validateFundingCarryRecoveryOutboxPredecessors(ctx, binding.store, outbox); err != nil {
			return err
		}
		marker.RuntimeBaseRevision = currentRevision
		projected, mergeErr := mergeFundingCarryRecoveryProjection(state, marker, projection.Events, projection.SourceCursor)
		if mergeErr != nil {
			return mergeErr
		}
		nextPayload, encodeErr := json.Marshal(projected)
		if encodeErr != nil {
			return fmt.Errorf("encode FundingCarry recovery runtime projection: %w", encodeErr)
		}
		marker.RuntimeResultRevision = fundingCarryRuntimeRevision(version, string(nextPayload))
		for i := range projected.RecoveryProjectionMarkers {
			if projected.RecoveryProjectionMarkers[i].EpisodeID == marker.EpisodeID && projected.RecoveryProjectionMarkers[i].OperationID == marker.OperationID {
				projected.RecoveryProjectionMarkers[i] = marker
				break
			}
		}
		nextPayload, encodeErr = json.Marshal(projected)
		if encodeErr != nil {
			return fmt.Errorf("encode FundingCarry recovery runtime projection result marker: %w", encodeErr)
		}
		saved, saveErr := writer.CompareAndSwapRuntimeState(ctx, "funding_carry", version, payload, fundingCarryRuntimeStateVersion, string(nextPayload))
		if saveErr != nil {
			return fmt.Errorf("CAS FundingCarry recovery runtime projection: %w", saveErr)
		}
		if saved {
			projectedReadback = true
			break
		}
		version, payload, found, err = reader.LoadRuntimeStateContext(ctx, "funding_carry")
		if err != nil || !found || version != fundingCarryRuntimeStateVersion {
			return errors.Join(errors.New("FundingCarry recovery runtime projection CAS lost and readback unavailable"), err)
		}
		state, err = decodeFundingCarryRuntimeStateForRecovery(version, payload, s.fut.GetName(), s.spot.GetName(), s.symbol, true)
		if err != nil {
			return fmt.Errorf("validate FundingCarry runtime snapshot after projection CAS loss: %w", err)
		}
		if state.RecoveryProjectionSchema != fundingCarryRecoveryProjectionSchemaVersion ||
			(state.MarginAccountScope != "" && !containsFundingCarryScope(binding.scopeKeys, state.MarginAccountScope)) {
			return errors.New("FundingCarry runtime snapshot owner/schema fence changed during projection CAS")
		}
	}
	if !projectedReadback {
		return errors.New("FundingCarry recovery runtime projection CAS contention exceeded retry limit")
	}

	readVersion, readPayload, readFound, err := reader.LoadRuntimeStateContext(ctx, "funding_carry")
	if err != nil || !readFound || readVersion != fundingCarryRuntimeStateVersion {
		return errors.Join(errors.New("FundingCarry recovery runtime projection readback unavailable"), err)
	}
	readState, err := decodeFundingCarryRuntimeStateForRecovery(readVersion, readPayload, s.fut.GetName(), s.spot.GetName(), s.symbol, true)
	if err != nil || !fundingCarryRecoveryProjectionPresent(readState, marker, projection.Events, projection.SourceCursor) {
		return errors.Join(errors.New("FundingCarry recovery runtime projection readback mismatch"), err)
	}
	outbox.AppliedOperationID = outbox.OperationID
	outbox.AppliedRevision = outbox.TargetEpisodeRevision
	outbox.AppliedHash = outbox.ProjectionHash
	outbox.State = accounting.RecoveryProjected
	if markErr := binding.store.MarkRecoveryProjected(ctx, binding.generation, outbox); markErr != nil {
		readOutbox, found, readErr := binding.store.GetRecoveryOutbox(ctx, outbox.EpisodeID, outbox.OperationID)
		if readErr != nil || !found || readOutbox.State != accounting.RecoveryProjected || readOutbox.AppliedOperationID != marker.OperationID || readOutbox.AppliedRevision != marker.EpisodeRevision || readOutbox.AppliedHash != marker.ProjectionHash {
			return errors.Join(fmt.Errorf("mark FundingCarry recovery projection with current generation: %w", markErr), readErr)
		}
	}
	readOutbox, found, err := binding.store.GetRecoveryOutbox(ctx, outbox.EpisodeID, outbox.OperationID)
	if err != nil || !found || readOutbox.State != accounting.RecoveryProjected || readOutbox.AppliedOperationID != marker.OperationID || readOutbox.AppliedRevision != marker.EpisodeRevision || readOutbox.AppliedHash != marker.ProjectionHash {
		return errors.Join(errors.New("FundingCarry durable projection receipt readback mismatch"), err)
	}
	return nil
}

func validateFundingCarryRecoveryEpisodeFence(ctx context.Context, binding fundingCarryRecoveryBinding, expected accounting.RecoveryEpisode, outbox accounting.RecoveryOutbox, takenOver bool) error {
	current, found, err := binding.store.GetRecoveryEpisode(ctx, expected.ID)
	if err != nil || !found {
		return errors.Join(errors.New("read FundingCarry recovery episode before runtime projection CAS"), err)
	}
	currentScopes, scopeErr := normalizeFundingCarryRecoveryScopes(current.AccountScopes)
	if current.ID != expected.ID || current.Bot != binding.botID || current.Strategy != "funding_carry" || scopeErr != nil || !equalFundingCarryRecoveryScopes(currentScopes, binding.scopeKeys) {
		return errors.New("FundingCarry recovery episode owner/scope identity changed before runtime projection CAS")
	}
	if !takenOver {
		if current.Revision != outbox.TargetEpisodeRevision || current.State != accounting.RecoveryLedgerCommitted || current.OwnerGeneration != binding.generation.ID() {
			return errors.New("FundingCarry recovery episode projection proof changed before runtime CAS")
		}
		return nil
	}
	if current.Revision != outbox.TargetEpisodeRevision+1 || current.State != accounting.RecoveryBlocked ||
		!current.HasCurrentOwnerTakeover() || validateFundingCarryTakeoverOutboxCandidate(binding, current, outbox) != nil {
		return errors.New("FundingCarry explicit takeover projection proof changed before runtime CAS")
	}
	return nil
}

func mergeFundingCarryRecoveryProjection(state fundingCarryRuntimeState, marker fundingCarryRecoveryProjectionMarker, events []accounting.RecoveryEvent, cursor *accounting.RecoverySourceCursor) (fundingCarryRuntimeState, error) {
	for _, existing := range state.RecoveryProjectionMarkers {
		if existing.EpisodeID == marker.EpisodeID && existing.OperationID == marker.OperationID {
			if !sameFundingCarryRecoveryProjectionIdentity(existing, marker) || existing.RuntimeBaseRevision != marker.RuntimeBaseRevision ||
				existing.RuntimeResultRevision != marker.RuntimeResultRevision {
				return state, errors.New("FundingCarry recovery operation marker conflicts with runtime snapshot")
			}
			merged, err := appendFundingCarryRecoveryEvents(state, events)
			if err != nil {
				return state, err
			}
			return appendFundingCarryRecoveryCursor(merged, cursor)
		}
		if existing.EpisodeID == marker.EpisodeID && existing.EpisodeRevision >= marker.EpisodeRevision {
			return state, errors.New("FundingCarry recovery projection revision is stale or out of order")
		}
	}
	state, err := appendFundingCarryRecoveryEvents(state, events)
	if err != nil {
		return state, err
	}
	state, err = appendFundingCarryRecoveryCursor(state, cursor)
	if err != nil {
		return state, err
	}
	state.RecoveryProjectionMarkers = append(state.RecoveryProjectionMarkers, marker)
	sort.Slice(state.RecoveryProjectionMarkers, func(i, j int) bool {
		if state.RecoveryProjectionMarkers[i].EpisodeID != state.RecoveryProjectionMarkers[j].EpisodeID {
			return state.RecoveryProjectionMarkers[i].EpisodeID < state.RecoveryProjectionMarkers[j].EpisodeID
		}
		return state.RecoveryProjectionMarkers[i].EpisodeRevision < state.RecoveryProjectionMarkers[j].EpisodeRevision
	})
	return state, nil
}

func appendFundingCarryRecoveryEvents(state fundingCarryRuntimeState, events []accounting.RecoveryEvent) (fundingCarryRuntimeState, error) {
	byID := make(map[string]accounting.RecoveryEvent, len(state.RecoveryFacts))
	for _, event := range state.RecoveryFacts {
		byID[event.EpisodeID+"\x00"+event.ID] = event
	}
	for _, event := range events {
		key := event.EpisodeID + "\x00" + event.ID
		if old, exists := byID[key]; exists {
			oldJSON, _ := json.Marshal(old)
			newJSON, _ := json.Marshal(event)
			if string(oldJSON) != string(newJSON) {
				return state, fmt.Errorf("FundingCarry recovery event %q conflicts with runtime projection", event.ID)
			}
			continue
		}
		byID[key] = event
		state.RecoveryFacts = append(state.RecoveryFacts, event)
	}
	sort.Slice(state.RecoveryFacts, func(i, j int) bool {
		if state.RecoveryFacts[i].EpisodeID != state.RecoveryFacts[j].EpisodeID {
			return state.RecoveryFacts[i].EpisodeID < state.RecoveryFacts[j].EpisodeID
		}
		return state.RecoveryFacts[i].ID < state.RecoveryFacts[j].ID
	})
	return state, nil
}

func fundingCarryRecoveryProjectionPresent(state fundingCarryRuntimeState, marker fundingCarryRecoveryProjectionMarker, events []accounting.RecoveryEvent, cursor *accounting.RecoverySourceCursor) bool {
	for _, existing := range state.RecoveryProjectionMarkers {
		if sameFundingCarryRecoveryProjectionIdentity(existing, marker) && validFundingCarryRuntimeRevision(existing.RuntimeBaseRevision) &&
			validFundingCarryRuntimeRevision(existing.RuntimeResultRevision) {
			merged, err := appendFundingCarryRecoveryEvents(state, events)
			if err != nil || len(merged.RecoveryFacts) != len(state.RecoveryFacts) {
				return false
			}
			return fundingCarryRecoveryCursorProjectedAtLeast(state.RecoverySourceCursors, cursor)
		}
	}
	return false
}

func sameFundingCarryRecoveryProjectionIdentity(left, right fundingCarryRecoveryProjectionMarker) bool {
	return left.EpisodeID == right.EpisodeID && left.EpisodeRevision == right.EpisodeRevision &&
		left.OperationID == right.OperationID && left.ProjectionHash == right.ProjectionHash &&
		left.ExpectedRuntimeRevision == right.ExpectedRuntimeRevision
}

func fundingCarryCanRebaseAcrossEpisodes(state fundingCarryRuntimeState, marker fundingCarryRecoveryProjectionMarker, currentRevision string) bool {
	if currentRevision == marker.ExpectedRuntimeRevision || !validFundingCarryRuntimeRevision(marker.ExpectedRuntimeRevision) || !validFundingCarryRuntimeRevision(currentRevision) {
		return false
	}
	seen := make(map[string]struct{}, len(state.RecoveryProjectionMarkers))
	revision := marker.ExpectedRuntimeRevision
	for revision != currentRevision {
		if _, exists := seen[revision]; exists {
			return false
		}
		seen[revision] = struct{}{}
		var next *fundingCarryRecoveryProjectionMarker
		for i := range state.RecoveryProjectionMarkers {
			existing := &state.RecoveryProjectionMarkers[i]
			if existing.EpisodeID == marker.EpisodeID || existing.RuntimeBaseRevision != revision ||
				!validFundingCarryRuntimeRevision(existing.RuntimeResultRevision) {
				continue
			}
			if next != nil {
				return false
			}
			next = existing
		}
		if next == nil || next.RuntimeResultRevision == revision {
			return false
		}
		revision = next.RuntimeResultRevision
	}
	return true
}

func validFundingCarryRuntimeRevision(revision string) bool {
	versionText, hashText, found := strings.Cut(revision, ":")
	version, err := strconv.Atoi(versionText)
	if !found || err != nil || version != fundingCarryRuntimeStateVersion || versionText != strconv.Itoa(version) {
		return false
	}
	if hashText != strings.ToLower(hashText) {
		return false
	}
	hash, err := hex.DecodeString(hashText)
	return err == nil && len(hash) == sha256.Size
}

func validateFundingCarryRecoveryOutboxPredecessors(ctx context.Context, store storage.FundingRecoveryStore, outbox accounting.RecoveryOutbox) error {
	outboxes, err := store.ListRecoveryOutboxes(ctx, outbox.EpisodeID)
	if err != nil {
		return fmt.Errorf("list FundingCarry episode outboxes for predecessor fence: %w", err)
	}
	for _, earlier := range outboxes {
		if earlier.TargetEpisodeRevision >= outbox.TargetEpisodeRevision {
			continue
		}
		if earlier.State != accounting.RecoveryProjected || earlier.AppliedOperationID != earlier.OperationID ||
			earlier.AppliedRevision != earlier.TargetEpisodeRevision || earlier.AppliedHash != earlier.ProjectionHash {
			return fmt.Errorf("FundingCarry recovery outbox %q is blocked by unprojected predecessor %q", outbox.OperationID, earlier.OperationID)
		}
	}
	return nil
}

func fundingCarryRecoveryCursorProjectedAtLeast(cursors []accounting.RecoverySourceCursor, expected *accounting.RecoverySourceCursor) bool {
	if expected == nil {
		return true
	}
	identity, _ := json.Marshal(expected.RecoverySourceWindow.Identity())
	for _, current := range cursors {
		currentIdentity, _ := json.Marshal(current.RecoverySourceWindow.Identity())
		if string(currentIdentity) != string(identity) {
			continue
		}
		if current.EpisodeID != expected.EpisodeID || current.Revision < expected.Revision {
			return false
		}
		if current.Revision == expected.Revision {
			return current == *expected
		}
		return true
	}
	return false
}

func appendFundingCarryRecoveryCursor(state fundingCarryRuntimeState, cursor *accounting.RecoverySourceCursor) (fundingCarryRuntimeState, error) {
	if cursor == nil {
		return state, nil
	}
	identity := cursor.RecoverySourceWindow.Identity()
	encodedIdentity, _ := json.Marshal(identity)
	key := string(encodedIdentity)
	for index, existing := range state.RecoverySourceCursors {
		encodedExisting, _ := json.Marshal(existing.RecoverySourceWindow.Identity())
		if string(encodedExisting) != key {
			continue
		}
		if existing == *cursor {
			return state, nil
		}
		if existing.Revision >= cursor.Revision || existing.EpisodeID != cursor.EpisodeID || !existing.Through.Equal(cursor.From) || cursor.ExpectedRevision != existing.Revision {
			return state, errors.New("FundingCarry recovery source cursor conflicts, regresses, or skips a durable revision")
		}
		state.RecoverySourceCursors[index] = *cursor
		sort.Slice(state.RecoverySourceCursors, func(i, j int) bool {
			left, _ := json.Marshal(state.RecoverySourceCursors[i].RecoverySourceWindow.Identity())
			right, _ := json.Marshal(state.RecoverySourceCursors[j].RecoverySourceWindow.Identity())
			return string(left) < string(right)
		})
		return state, nil
	}
	if cursor.ExpectedRevision != 0 || cursor.Revision != 1 {
		return state, errors.New("FundingCarry recovery source cursor has no trusted initial revision")
	}
	state.RecoverySourceCursors = append(state.RecoverySourceCursors, *cursor)
	sort.Slice(state.RecoverySourceCursors, func(i, j int) bool {
		left, _ := json.Marshal(state.RecoverySourceCursors[i].RecoverySourceWindow.Identity())
		right, _ := json.Marshal(state.RecoverySourceCursors[j].RecoverySourceWindow.Identity())
		return string(left) < string(right)
	})
	return state, nil
}

func validateFundingCarryProjectedSourceCursor(cursor *accounting.RecoverySourceCursor, episodeID string, scopes []string) error {
	if cursor == nil {
		return nil
	}
	if cursor.EpisodeID != episodeID || cursor.Revision != cursor.ExpectedRevision+1 || cursor.RecoverySourceWindow.Validate() != nil ||
		!containsFundingCarryScope(scopes, cursor.AccountScope) {
		return errors.New("FundingCarry outbox source cursor identity, completeness, or account scope is invalid")
	}
	return nil
}

func containsFundingCarryScope(scopes []string, scope string) bool {
	index := sort.SearchStrings(scopes, scope)
	return index < len(scopes) && scopes[index] == scope
}

func fundingCarryRuntimeRevision(version int, payload string) string {
	var state fundingCarryRuntimeState
	if err := json.Unmarshal([]byte(payload), &state); err == nil && len(state.RecoveryProjectionMarkers) > 0 {
		for i := range state.RecoveryProjectionMarkers {
			state.RecoveryProjectionMarkers[i].RuntimeResultRevision = ""
		}
		if normalized, marshalErr := json.Marshal(state); marshalErr == nil {
			payload = string(normalized)
		}
	}
	hash := sha256.Sum256([]byte(payload))
	return fmt.Sprintf("%d:%x", version, hash[:])
}

func (s *FundingCarryStrategy) blockFundingCarryRecovery(source string) {
	if source == "" {
		source = execution.RecoveryEpisodeGateSource("recovery-unverified", 1)
	}
	s.mu.Lock()
	s.unownedExposure = true
	gate, blocker := s.openingGate, s.openingBlocker
	s.mu.Unlock()
	if gate != nil {
		gate.Block(source)
	}
	if blocker != nil {
		blocker(source)
	}
}
