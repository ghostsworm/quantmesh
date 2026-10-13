package strategy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"time"

	"quantmesh/accounting"
	"quantmesh/exchange"
	"quantmesh/execution"
	"quantmesh/storage"
)

// FundingRecoveryFillPreview contains venue data for a human to inspect. It is
// deliberately not accepted back by ApplyFundingRecoveryFills.
type FundingRecoveryFillPreview struct {
	EpisodeID       string                               `json:"episode_id"`
	ChildID         string                               `json:"child_id"`
	EpisodeRevision uint64                               `json:"episode_revision"`
	ChildRevision   uint64                               `json:"child_revision"`
	Cursor          accounting.Cursor                    `json:"cursor"`
	Attempt         accounting.RecoverySubmissionAttempt `json:"attempt"`
	Batch           exchange.OrderFillBatch              `json:"batch"`
	FeesComplete    bool                                 `json:"fees_complete"`
	NewTradeIDs     []string                             `json:"new_trade_ids"`
}

// ApplyFundingRecoveryFillsRequest contains authorization and CAS coordinates
// only. Client-supplied fill, fee, or economic facts are not representable.
type ApplyFundingRecoveryFillsRequest struct {
	EpisodeID               string `json:"episode_id"`
	ChildID                 string `json:"child_id"`
	OperationID             string `json:"operation_id"`
	ExpectedEpisodeRevision uint64 `json:"expected_episode_revision"`
	ExpectedChildRevision   uint64 `json:"expected_child_revision"`
	PreviewSourceRevision   string `json:"preview_source_revision"`
	Actor                   string `json:"actor"`
	AuditRef                string `json:"audit_ref"`
	Confirmed               bool   `json:"confirmed"`
}

type ApplyFundingRecoveryFillsResult struct {
	Receipt                     accounting.RecoveryReceipt `json:"receipt"`
	Replayed                    bool                       `json:"replayed"`
	FeesComplete                bool                       `json:"fees_complete"`
	ManualFeeAdjustmentRequired bool                       `json:"manual_fee_adjustment_required"`
	HoldRetained                bool                       `json:"hold_retained"`
	Projected                   bool                       `json:"projected"`
}

type fundingRecoveryFillSnapshot struct {
	binding fundingCarryRecoveryBinding
	episode accounting.RecoveryEpisode
	child   accounting.RecoveryChild
	attempt accounting.RecoverySubmissionAttempt
	venue   exchange.IExchange
	source  exchange.OrderFillBatchSource
	batch   *exchange.OrderFillBatch
	fills   []*exchange.OrderFill
}

func (s *FundingCarryStrategy) previewFundingRecoveryFills(ctx context.Context, episodeID, childID string) (FundingRecoveryFillPreview, error) {
	if ctx == nil || s == nil || strings.TrimSpace(episodeID) == "" || strings.TrimSpace(childID) == "" {
		return FundingRecoveryFillPreview{}, errors.New("preview requires context, episode ID and child ID")
	}
	snapshot, err := s.readFundingRecoveryFillSnapshot(ctx, episodeID, childID)
	if err != nil {
		return FundingRecoveryFillPreview{}, err
	}
	existing, err := snapshot.binding.store.ListRecoveryEvents(ctx, episodeID)
	if err != nil {
		return FundingRecoveryFillPreview{}, fmt.Errorf("read durable fill events: %w", err)
	}
	newFills, err := fundingRecoveryFillSuffix(snapshot, existing)
	if err != nil {
		return FundingRecoveryFillPreview{}, err
	}
	ids := make([]string, len(newFills))
	for i, fill := range newFills {
		ids[i] = fill.TradeID
	}
	return FundingRecoveryFillPreview{
		EpisodeID: episodeID, ChildID: childID, EpisodeRevision: snapshot.episode.Revision,
		ChildRevision: snapshot.child.Revision, Cursor: snapshot.child.FillCursor,
		Attempt: snapshot.attempt, Batch: cloneOrderFillBatch(snapshot.batch),
		FeesComplete: snapshot.batch.FeesComplete, NewTradeIDs: ids,
	}, nil
}

func (s *FundingCarryStrategy) readFundingRecoveryFillSnapshot(ctx context.Context, episodeID, childID string) (fundingRecoveryFillSnapshot, error) {
	binding, err := resolveFundingCarryRecoveryBindingFromStrategy(s)
	if err != nil {
		return fundingRecoveryFillSnapshot{}, err
	}
	episode, found, err := binding.store.GetRecoveryEpisode(ctx, episodeID)
	if err != nil || !found {
		return fundingRecoveryFillSnapshot{}, errors.Join(errors.New("read FundingCarry recovery episode"), err)
	}
	if episode.Bot != binding.botID || episode.Strategy != "funding_carry" || episode.OwnerGeneration != binding.generation.ID() ||
		(episode.State != accounting.RecoveryBlocked && episode.State != accounting.RecoveryProjected) || !validFundingCarryRetainableEpisode(episode) {
		return fundingRecoveryFillSnapshot{}, accounting.ErrRecoveryConflict
	}
	scopes, err := normalizeFundingCarryRecoveryScopes(episode.AccountScopes)
	if err != nil || !equalFundingCarryRecoveryScopes(scopes, binding.scopeKeys) {
		return fundingRecoveryFillSnapshot{}, storage.ErrFundingRecoveryGenerationScopeMismatch
	}
	hold, held, err := binding.store.GetRecoveryHold(ctx, episode.ID, episode.RecoveryGeneration)
	if err != nil || !held || hold.State != "held" || hold.Bot != episode.Bot || hold.Strategy != episode.Strategy ||
		hold.Generation != episode.RecoveryGeneration || hold.Source != execution.RecoveryEpisodeGateSource(episode.ID, episode.RecoveryGeneration) {
		return fundingRecoveryFillSnapshot{}, errors.Join(errors.New("exact active recovery hold is required"), err)
	}
	holdScopes, err := normalizeFundingCarryRecoveryScopes(hold.AccountScopes)
	if err != nil || !equalFundingCarryRecoveryScopes(holdScopes, binding.scopeKeys) {
		return fundingRecoveryFillSnapshot{}, storage.ErrFundingRecoveryGenerationScopeMismatch
	}
	s.mu.RLock()
	gate := s.openingGate
	s.mu.RUnlock()
	if gate == nil || !gate.HasBlock(execution.RecoveryEpisodeGateSource(episode.ID, episode.RecoveryGeneration)) {
		return fundingRecoveryFillSnapshot{}, errors.New("exact recovery episode Gate source is not active")
	}
	children, err := binding.store.ListRecoveryChildren(ctx, episode.ID)
	if err != nil {
		return fundingRecoveryFillSnapshot{}, fmt.Errorf("list recovery children: %w", err)
	}
	var child accounting.RecoveryChild
	childCount := 0
	for _, candidate := range children {
		if candidate.ID == childID {
			child, childCount = candidate, childCount+1
		}
	}
	if childCount != 1 || child.EpisodeID != episode.ID || child.State != accounting.ChildBound || child.Revision == 0 ||
		child.Bot != binding.botID || child.StrategyName != "funding_carry" || child.StrategyType != "funding_carry" ||
		!containsFundingCarryScope(binding.scopeKeys, child.AccountScope) || child.AccountScope == "" || child.VenueOrderID == "" ||
		child.ClientOrderID == "" || child.Symbol == "" || (child.Side != "BUY" && child.Side != "SELL") || !validFundingRecoveryIntentHash(child.OrderIntentHash) {
		return fundingRecoveryFillSnapshot{}, errors.New("exact bound FundingCarry child identity is invalid")
	}
	attempts, err := binding.store.ListRecoverySubmissionAttempts(ctx, episode.ID)
	if err != nil {
		return fundingRecoveryFillSnapshot{}, fmt.Errorf("list recovery submission attempts: %w", err)
	}
	var attempt accounting.RecoverySubmissionAttempt
	attemptCount := 0
	for _, candidate := range attempts {
		if candidate.ChildID == child.ID {
			attempt, attemptCount = candidate, attemptCount+1
		}
	}
	if attemptCount != 1 || attempt.State != accounting.RecoverySubmissionFound || attempt.Revision == 0 ||
		attempt.EpisodeID != episode.ID || attempt.Generation != episode.RecoveryGeneration ||
		!fundingRecoveryEpisodeOwnsGeneration(episode, attempt.OwnerGeneration) ||
		attempt.AccountScope != child.AccountScope || attempt.ChildID != child.ID || attempt.IntentHash != child.OrderIntentHash ||
		attempt.VenueOrderID != child.VenueOrderID || attempt.Intent.ClientOrderID != child.ClientOrderID ||
		attempt.Intent.Symbol != child.Symbol || attempt.Intent.Side != child.Side || attempt.Intent.Exchange != child.Exchange || attempt.Intent.Market != child.Market ||
		attempt.Actor == "" || attempt.AuditRef == "" {
		return fundingRecoveryFillSnapshot{}, errors.New("unique durable found submission attempt does not match bound child")
	}
	venue, err := s.recoveryVenue(attempt.Intent)
	if err != nil {
		return fundingRecoveryFillSnapshot{}, err
	}
	source, ok := venue.(exchange.OrderFillBatchSource)
	if !ok || source == nil {
		return fundingRecoveryFillSnapshot{}, errors.New("FundingCarry venue does not support authoritative complete per-order fill batches")
	}
	orderID, err := strconv.ParseInt(child.VenueOrderID, 10, 64)
	if err != nil || orderID <= 0 {
		return fundingRecoveryFillSnapshot{}, errors.New("bound venue order ID is not a positive numeric identifier")
	}
	batch, err := source.GetOrderFillBatch(ctx, child.Symbol, orderID)
	if err != nil || batch == nil {
		return fundingRecoveryFillSnapshot{}, errors.Join(errors.New("read exact venue order fill batch"), err)
	}
	if err := validateFundingRecoveryFillBatch(batch, child, attempt); err != nil {
		return fundingRecoveryFillSnapshot{}, err
	}
	return fundingRecoveryFillSnapshot{binding: binding, episode: episode, child: child, attempt: attempt, venue: venue, source: source, batch: batch, fills: sortedFundingRecoveryFills(batch.Fills)}, nil
}

func fundingRecoveryEpisodeOwnsGeneration(episode accounting.RecoveryEpisode, generation string) bool {
	if generation == "" || episode.OwnerGeneration == "" || len(episode.OwnerTakeovers) > 0 && !episode.HasCurrentOwnerTakeover() {
		return false
	}
	if len(episode.OwnerTakeovers) > 0 {
		owner := episode.OwnerTakeovers[0].PreviousOwnerGeneration
		if owner == "" {
			return false
		}
		for _, takeover := range episode.OwnerTakeovers {
			if takeover.PreviousOwnerGeneration != owner || takeover.OwnerGeneration == "" || takeover.OwnerGeneration == owner {
				return false
			}
			owner = takeover.OwnerGeneration
		}
		if owner != episode.OwnerGeneration {
			return false
		}
	}
	if generation == episode.OwnerGeneration {
		return true
	}
	for _, takeover := range episode.OwnerTakeovers {
		if generation == takeover.PreviousOwnerGeneration || generation == takeover.OwnerGeneration {
			return true
		}
	}
	return false
}

func resolveFundingCarryRecoveryBindingFromStrategy(s *FundingCarryStrategy) (fundingCarryRecoveryBinding, error) {
	if s == nil {
		return fundingCarryRecoveryBinding{}, ErrFundingCarryDurableRecoveryUnavailable
	}
	s.mu.RLock()
	stateStore := s.runtimeStateStore
	s.mu.RUnlock()
	provider, ok := stateStore.(FundingCarryRecoveryProvider)
	if !ok || provider == nil {
		return fundingCarryRecoveryBinding{}, ErrFundingCarryDurableRecoveryUnavailable
	}
	binding, err := resolveFundingCarryRecoveryBinding(stateStore, provider)
	if err != nil {
		return fundingCarryRecoveryBinding{}, err
	}
	return binding, nil
}

func validateFundingRecoveryFillBatch(batch *exchange.OrderFillBatch, child accounting.RecoveryChild, attempt accounting.RecoverySubmissionAttempt) error {
	if batch.OrderID <= 0 || strconv.FormatInt(batch.OrderID, 10) != child.VenueOrderID || batch.ClientOrderID != child.ClientOrderID ||
		batch.Symbol != child.Symbol || string(batch.Side) != child.Side || batch.SourceRevision == "" || batch.OrderUpdateTime <= 0 ||
		batch.TerminalStatus != exchange.OrderStatusFilled && batch.TerminalStatus != exchange.OrderStatusCanceled &&
			batch.TerminalStatus != exchange.OrderStatusRejected && batch.TerminalStatus != exchange.OrderStatusExpired {
		return errors.New("fill batch identity, source revision, or terminal status differs from bound order")
	}
	if !batch.Complete || !batch.FillHistoryComplete {
		return errors.New("venue fill batch is partial or cannot prove complete fill history")
	}
	executed, ok := new(big.Rat).SetString(batch.ExecutedQuantityRaw)
	if !ok || executed.Sign() < 0 {
		return errors.New("fill batch has invalid raw executed quantity")
	}
	fillTotal := new(big.Rat)
	for _, fill := range batch.Fills {
		if fill == nil || fill.QuantityRaw == "" {
			return errors.New("fill batch contains missing raw quantity")
		}
		quantity, valid := new(big.Rat).SetString(fill.QuantityRaw)
		if !valid || quantity.Sign() <= 0 {
			return errors.New("fill batch contains invalid raw quantity")
		}
		fillTotal.Add(fillTotal, quantity)
	}
	if fillTotal.Cmp(executed) != 0 {
		return errors.New("complete fill batch quantities do not equal authoritative executed quantity")
	}
	intended, ok := new(big.Rat).SetString(attempt.Intent.Quantity)
	if !ok || intended.Sign() <= 0 || executed.Cmp(intended) > 0 {
		return errors.New("fill batch executed quantity is inconsistent with immutable intent")
	}
	return nil
}

func sortedFundingRecoveryFills(fills []*exchange.OrderFill) []*exchange.OrderFill {
	out := append([]*exchange.OrderFill(nil), fills...)
	sort.Slice(out, func(i, j int) bool {
		if out[i] == nil {
			return out[j] != nil
		}
		if out[j] == nil {
			return false
		}
		left, leftErr := strconv.ParseUint(out[i].TradeID, 10, 64)
		right, rightErr := strconv.ParseUint(out[j].TradeID, 10, 64)
		if leftErr != nil || rightErr != nil {
			return out[i].TradeID < out[j].TradeID
		}
		return left < right
	})
	return out
}

func fundingRecoveryFillSuffix(snapshot fundingRecoveryFillSnapshot, existing []accounting.RecoveryEvent) ([]*exchange.OrderFill, error) {
	batchFills := snapshot.fills
	if len(batchFills) != len(snapshot.batch.Fills) {
		return nil, errors.New("fill batch contains nil or unordered fill identity")
	}
	seen := make(map[string]struct{}, len(batchFills))
	for _, fill := range batchFills {
		id, err := strconv.ParseUint(fill.TradeID, 10, 64)
		if err != nil || fill.TradeID == "" {
			return nil, errors.New("venue trade ID must be a positive numeric value")
		}
		if _, duplicate := seen[fill.TradeID]; duplicate {
			return nil, errors.New("venue fill batch contains duplicate trade ID")
		}
		seen[fill.TradeID] = struct{}{}
		if id == 0 || fill.OrderID != snapshot.batch.OrderID || fill.Symbol != snapshot.child.Symbol || string(fill.Side) != snapshot.child.Side || fill.TradeTime <= 0 {
			return nil, errors.New("fill identity or TradeTime is incomplete")
		}
		if !validFundingRecoveryRawDecimal(fill.PriceRaw, true) || !validFundingRecoveryRawDecimal(fill.QuantityRaw, true) ||
			(fill.QuoteQuantityRaw != "" && !validFundingRecoveryRawDecimal(fill.QuoteQuantityRaw, true)) ||
			(fill.CommissionKnown && (!validFundingRecoveryRawDecimal(fill.CommissionRaw, false) || strings.TrimSpace(fill.CommissionAsset) == "")) ||
			!fill.CommissionKnown && (fill.CommissionRaw != "" || fill.CommissionAsset != "") ||
			(fill.RealizedPnLKnown && (!validFundingRecoverySignedRawDecimal(fill.RealizedPnLRaw) || strings.TrimSpace(fill.RealizedPnLAsset) == "")) ||
			(!fill.RealizedPnLKnown && (fill.RealizedPnLRaw != "" || fill.RealizedPnLAsset != "")) {
			return nil, errors.New("fill raw decimal or commission evidence is malformed")
		}
	}
	feesComplete := true
	for _, fill := range batchFills {
		if !fill.CommissionKnown {
			feesComplete = false
		}
	}
	if snapshot.batch.FeesComplete != feesComplete {
		return nil, errors.New("venue FeesComplete flag disagrees with per-fill commission evidence")
	}
	byTrade := make(map[string]*exchange.OrderFill, len(batchFills))
	for _, fill := range batchFills {
		byTrade[fill.TradeID] = fill
	}
	var stored []accounting.RecoveryEvent
	terminalSeen := false
	for _, event := range existing {
		if event.ChildID == snapshot.child.ID && event.Kind == accounting.RecoveryFillEvent {
			stored = append(stored, event)
		} else if event.ChildID == snapshot.child.ID && event.Kind == accounting.RecoveryTerminalEvent {
			terminalSeen = true
		}
	}
	if terminalSeen {
		return nil, errors.New("child already has terminal-zero evidence; a later fill requires an explicit audited reopen, not this fill adapter")
	}
	sort.Slice(stored, func(i, j int) bool { return stored[i].CursorSequence < stored[j].CursorSequence })
	if snapshot.child.FillCursor.Sequence != uint64(len(stored)) || snapshot.child.FillCursor.Validate() != nil {
		return nil, errors.New("durable child cursor and stored fill events are not a complete prefix")
	}
	for index, event := range stored {
		if index >= len(batchFills) || event.CursorSequence != uint64(index+1) || event.ExternalID != batchFills[index].TradeID ||
			snapshot.child.FillCursor.Sequence < event.CursorSequence || snapshot.child.FillCursor.TradeID != batchFills[len(stored)-1].TradeID {
			return nil, errors.New("stored fill events are not the numeric-order prefix of the complete source batch")
		}
		fill := byTrade[event.ExternalID]
		if fill == nil || !fundingRecoveryFillEventMatches(event, fill, snapshot, uint64(index+1)) {
			return nil, errors.New("stored fill event changed or disappeared; any fee correction requires a separate explicit manual fee event, never replacement or zero-fee inference")
		}
	}
	for _, event := range existing {
		if event.ChildID == snapshot.child.ID && event.Kind != accounting.RecoveryFillEvent && event.Kind != accounting.RecoveryTerminalEvent {
			return nil, errors.New("child has an unexpected non-fill accounting event")
		}
	}
	return batchFills[len(stored):], nil
}

func fundingRecoveryFillEventMatches(event accounting.RecoveryEvent, fill *exchange.OrderFill, snapshot fundingRecoveryFillSnapshot, sequence uint64) bool {
	if event.Validate() != nil {
		return false
	}
	baseAsset := strings.TrimSpace(snapshot.venue.GetBaseAsset())
	quoteAsset := ""
	if fill.QuoteQuantityRaw != "" {
		quoteAsset = strings.TrimSpace(snapshot.venue.GetQuoteAsset())
		if quoteAsset == "" {
			return false
		}
	}
	evidence := accounting.RecoveryFillEvidence{
		ClientOrderID: snapshot.child.ClientOrderID, VenueOrderID: snapshot.child.VenueOrderID,
		Symbol: snapshot.child.Symbol, OrderIntentHash: snapshot.child.OrderIntentHash, Side: snapshot.child.Side,
		BaseAsset: baseAsset, PriceRaw: fill.PriceRaw, QuantityRaw: fill.QuantityRaw,
		QuoteAsset: quoteAsset, QuoteQuantityRaw: fill.QuoteQuantityRaw,
		CommissionKnown: fill.CommissionKnown, CommissionRaw: fill.CommissionRaw, CommissionAsset: fill.CommissionAsset,
	}
	if !fill.CommissionKnown {
		evidence.CommissionRaw, evidence.CommissionAsset = "", ""
	}
	if fill.RealizedPnLKnown {
		evidence.RealizedPnLKnown, evidence.RealizedPnLRaw, evidence.RealizedPnLAsset = true, fill.RealizedPnLRaw, fill.RealizedPnLAsset
	}
	request := ApplyFundingRecoveryFillsRequest{EpisodeID: snapshot.episode.ID, ChildID: snapshot.child.ID}
	expected := fundingRecoveryFillEvent(request, snapshot, fill, evidence, time.UnixMilli(fill.TradeTime).UTC(), sequence)
	return reflect.DeepEqual(event, expected)
}

func (s *FundingCarryStrategy) applyFundingRecoveryFills(ctx context.Context, request ApplyFundingRecoveryFillsRequest) (ApplyFundingRecoveryFillsResult, error) {
	if ctx == nil || !request.Confirmed || strings.TrimSpace(request.EpisodeID) == "" || strings.TrimSpace(request.ChildID) == "" ||
		strings.TrimSpace(request.OperationID) == "" || request.ExpectedEpisodeRevision == 0 || request.ExpectedChildRevision == 0 ||
		strings.TrimSpace(request.PreviewSourceRevision) == "" || strings.TrimSpace(request.Actor) == "" || strings.TrimSpace(request.AuditRef) == "" {
		return ApplyFundingRecoveryFillsResult{}, errors.New("incomplete explicit FundingCarry fill reconciliation authorization")
	}
	if result, found, err := s.readFundingRecoveryFillOperationRetry(ctx, request); err != nil || found {
		return result, err
	}
	snapshot, err := s.readFundingRecoveryFillSnapshot(ctx, request.EpisodeID, request.ChildID)
	if err != nil {
		return ApplyFundingRecoveryFillsResult{}, err
	}
	if snapshot.episode.Revision != request.ExpectedEpisodeRevision || snapshot.child.Revision != request.ExpectedChildRevision ||
		snapshot.batch.SourceRevision != request.PreviewSourceRevision {
		return ApplyFundingRecoveryFillsResult{}, accounting.ErrRecoveryConflict
	}
	if !snapshot.batch.Complete {
		return ApplyFundingRecoveryFillsResult{}, errors.New("incomplete current venue snapshot; no accounting writes made")
	}
	existing, err := snapshot.binding.store.ListRecoveryEvents(ctx, request.EpisodeID)
	if err != nil {
		return ApplyFundingRecoveryFillsResult{}, err
	}
	newFills, err := fundingRecoveryFillSuffix(snapshot, existing)
	if err != nil {
		return ApplyFundingRecoveryFillsResult{}, err
	}
	executed, _ := new(big.Rat).SetString(snapshot.batch.ExecutedQuantityRaw)
	if len(newFills) == 0 && !(len(snapshot.fills) == 0 && snapshot.child.FillCursor.Sequence == 0 && executed != nil && executed.Sign() == 0) {
		return ApplyFundingRecoveryFillsResult{}, errors.New("no new venue fills to reconcile")
	}
	reader, ok := snapshot.binding.stateStore.(RuntimeStateContextReader)
	if !ok {
		return ApplyFundingRecoveryFillsResult{}, errors.New("durable fill reconciliation requires context-aware runtime revision read")
	}
	version, payload, found, err := reader.LoadRuntimeStateContext(ctx, "funding_carry")
	if err != nil || !found || version != fundingCarryRuntimeStateVersion {
		return ApplyFundingRecoveryFillsResult{}, errors.Join(errors.New("read current FundingCarry runtime revision"), err)
	}
	operation, err := buildFundingRecoveryFillOperation(request, snapshot, newFills, fundingCarryRuntimeRevision(version, payload))
	if err != nil {
		return ApplyFundingRecoveryFillsResult{}, err
	}
	receipt, replayed, err := snapshot.binding.store.ApplyRecoveryOperation(ctx, snapshot.binding.generation, operation)
	if err != nil {
		return ApplyFundingRecoveryFillsResult{}, err
	}
	result := ApplyFundingRecoveryFillsResult{Receipt: receipt, Replayed: replayed, FeesComplete: snapshot.batch.FeesComplete,
		ManualFeeAdjustmentRequired: !snapshot.batch.FeesComplete, HoldRetained: true}
	if err := s.projectFundingRecoveryFillOperation(ctx, snapshot.binding, request.EpisodeID, request.OperationID); err != nil {
		return result, fmt.Errorf("durable fill receipt committed; outbox projection remains pending and hold is retained: %w", err)
	}
	result.Projected = true
	return result, nil
}

func buildFundingRecoveryFillOperation(request ApplyFundingRecoveryFillsRequest, snapshot fundingRecoveryFillSnapshot, fills []*exchange.OrderFill, runtimeRevision string) (accounting.RecoveryOperation, error) {
	baseAsset := strings.TrimSpace(snapshot.venue.GetBaseAsset())
	if baseAsset == "" {
		return accounting.RecoveryOperation{}, errors.New("retained venue does not expose authoritative base asset")
	}
	events := make([]accounting.RecoveryEvent, 0, len(fills)+1)
	var maxObservedAt time.Time
	for index, fill := range fills {
		if fill.TradeTime <= 0 {
			return accounting.RecoveryOperation{}, errors.New("fill TradeTime must be positive; no accounting writes made")
		}
		observedAt := time.UnixMilli(fill.TradeTime).UTC()
		if observedAt.After(maxObservedAt) {
			maxObservedAt = observedAt
		}
		evidence := accounting.RecoveryFillEvidence{
			ClientOrderID: snapshot.child.ClientOrderID, VenueOrderID: snapshot.child.VenueOrderID,
			Symbol: snapshot.child.Symbol, OrderIntentHash: snapshot.child.OrderIntentHash, Side: snapshot.child.Side,
			BaseAsset: baseAsset, PriceRaw: fill.PriceRaw, QuantityRaw: fill.QuantityRaw,
			QuoteQuantityRaw: fill.QuoteQuantityRaw,
			CommissionKnown:  fill.CommissionKnown, CommissionRaw: fill.CommissionRaw, CommissionAsset: fill.CommissionAsset,
		}
		if !evidence.CommissionKnown {
			evidence.CommissionRaw, evidence.CommissionAsset = "", ""
		}
		if fill.QuoteQuantityRaw != "" {
			quoteAsset := strings.TrimSpace(snapshot.venue.GetQuoteAsset())
			if quoteAsset == "" {
				return accounting.RecoveryOperation{}, errors.New("fill source provides quote quantity but retained venue quote asset is unknown")
			}
			evidence.QuoteAsset = quoteAsset
		}
		if fill.RealizedPnLKnown {
			if !validFundingRecoverySignedRawDecimal(fill.RealizedPnLRaw) || strings.TrimSpace(fill.RealizedPnLAsset) == "" {
				return accounting.RecoveryOperation{}, errors.New("known realized PnL is missing valid raw decimal or authoritative asset")
			}
			evidence.RealizedPnLKnown = true
			evidence.RealizedPnLRaw = fill.RealizedPnLRaw
			evidence.RealizedPnLAsset = fill.RealizedPnLAsset
		}
		event := fundingRecoveryFillEvent(request, snapshot, fill, evidence, observedAt, snapshot.child.FillCursor.Sequence+uint64(index)+1)
		if err := event.Validate(); err != nil {
			return accounting.RecoveryOperation{}, fmt.Errorf("validate venue fill %q: %w", fill.TradeID, err)
		}
		events = append(events, event)
	}
	if maxObservedAt.IsZero() {
		maxObservedAt = time.UnixMilli(snapshot.batch.OrderUpdateTime).UTC()
	}
	child := snapshot.child
	child.FillCursor = snapshot.child.FillCursor
	for _, fill := range fills {
		sequence := child.FillCursor.Sequence + 1
		child.FillCursor = accounting.Cursor{Sequence: sequence, TradeID: fill.TradeID}
	}
	executed, _ := new(big.Rat).SetString(snapshot.batch.ExecutedQuantityRaw)
	if len(snapshot.fills) == 0 && executed != nil && executed.Sign() == 0 && snapshot.child.FillCursor.Sequence == 0 {
		events = append(events, fundingRecoveryZeroTerminalEvent(request, snapshot, maxObservedAt))
	}
	evidenceDigest, err := sha256JSON(struct {
		EpisodeID, ChildID, SourceRevision string
		Events                             []accounting.RecoveryEvent
	}{request.EpisodeID, request.ChildID, snapshot.batch.SourceRevision, events})
	if err != nil {
		return accounting.RecoveryOperation{}, err
	}
	return accounting.RecoveryOperation{
		EpisodeID: request.EpisodeID, OperationID: request.OperationID, HashVersion: 1,
		ExpectedEpisodeRevision: request.ExpectedEpisodeRevision, ExpectedRuntimeRevision: runtimeRevision,
		ExpectedChildRevisions: map[string]uint64{request.ChildID: request.ExpectedChildRevision}, ExpectedEventRevisions: map[string]uint64{},
		Children: []accounting.RecoveryChild{child}, Events: events, Actor: request.Actor, AuditRef: request.AuditRef,
		EvidenceHash: evidenceDigest, EvidenceReadAt: maxObservedAt,
	}, nil
}

func fundingRecoveryFillEvent(request ApplyFundingRecoveryFillsRequest, snapshot fundingRecoveryFillSnapshot, fill *exchange.OrderFill, evidence accounting.RecoveryFillEvidence, observedAt time.Time, sequence uint64) accounting.RecoveryEvent {
	evidenceHash, _ := sha256JSON(evidence)
	idHash := sha256.Sum256([]byte(request.EpisodeID + "\x00" + snapshot.child.ID + "\x00" + fill.TradeID))
	return accounting.RecoveryEvent{ID: "fill:" + hex.EncodeToString(idHash[:]), Source: "venue_order_fill_batch", Exchange: snapshot.child.Exchange,
		Market: snapshot.child.Market, AccountScope: snapshot.child.AccountScope, Kind: accounting.RecoveryFillEvent,
		ExternalID: fill.TradeID, EpisodeID: request.EpisodeID, ChildID: snapshot.child.ID, CursorSequence: sequence,
		Asset: evidence.BaseAsset, Amount: fill.QuantityRaw, SourceRevision: snapshot.batch.SourceRevision,
		SourceHash: evidenceHash, ObservedAt: observedAt, FillEvidence: &evidence}
}

func fundingRecoveryZeroTerminalEvent(request ApplyFundingRecoveryFillsRequest, snapshot fundingRecoveryFillSnapshot, observedAt time.Time) accounting.RecoveryEvent {
	hash, _ := sha256JSON(struct{ Episode, Child, Revision string }{request.EpisodeID, request.ChildID, snapshot.batch.SourceRevision})
	return accounting.RecoveryEvent{ID: "terminal_zero:" + hash, Source: "venue_order_fill_batch", Exchange: snapshot.child.Exchange,
		Market: snapshot.child.Market, AccountScope: snapshot.child.AccountScope, Kind: accounting.RecoveryTerminalEvent,
		ExternalID: "terminal_zero:" + hash, EpisodeID: request.EpisodeID, ChildID: snapshot.child.ID, Asset: snapshot.venue.GetBaseAsset(),
		Amount: "0", SourceRevision: snapshot.batch.SourceRevision, SourceHash: hash, ObservedAt: observedAt, CompletenessMarker: "fill_batch_complete"}
}

func (s *FundingCarryStrategy) projectFundingRecoveryFillOperation(ctx context.Context, binding fundingCarryRecoveryBinding, episodeID, operationID string) error {
	outbox, found, err := binding.store.GetRecoveryOutbox(ctx, episodeID, operationID)
	if err != nil || !found {
		return errors.Join(errors.New("read committed FundingCarry fill outbox"), err)
	}
	episode, found, err := binding.store.GetRecoveryEpisode(ctx, episodeID)
	if err != nil || !found {
		return errors.Join(errors.New("read committed FundingCarry fill episode"), err)
	}
	if outbox.State == accounting.RecoveryProjected {
		return s.verifyFundingRecoveryFillProjection(ctx, binding, episode, outbox)
	}
	if err := s.projectFundingCarryRecoveryOutbox(ctx, binding, episode, outbox); err != nil {
		return err
	}
	readback, found, err := binding.store.GetRecoveryOutbox(ctx, episodeID, operationID)
	if err != nil || !found || readback.State != accounting.RecoveryProjected || readback.AppliedOperationID != operationID || readback.AppliedHash != outbox.ProjectionHash {
		return errors.Join(errors.New("FundingCarry fill outbox projection readback mismatch"), err)
	}
	return nil
}

func (s *FundingCarryStrategy) verifyFundingRecoveryFillProjection(ctx context.Context, binding fundingCarryRecoveryBinding, episode accounting.RecoveryEpisode, outbox accounting.RecoveryOutbox) error {
	if outbox.State != accounting.RecoveryProjected || outbox.AppliedOperationID != outbox.OperationID || outbox.AppliedRevision != outbox.TargetEpisodeRevision || outbox.AppliedHash != outbox.ProjectionHash {
		return errors.New("projected FundingCarry fill outbox readback does not match its immutable receipt")
	}
	reader, ok := binding.stateStore.(RuntimeStateContextReader)
	if !ok {
		return errors.New("projected FundingCarry fill outbox requires runtime readback")
	}
	version, payload, found, err := reader.LoadRuntimeStateContext(ctx, "funding_carry")
	if err != nil || !found || version != fundingCarryRuntimeStateVersion {
		return errors.Join(errors.New("read projected FundingCarry fill runtime state"), err)
	}
	state, err := decodeFundingCarryRuntimeStateForRecovery(version, payload, s.fut.GetName(), s.spot.GetName(), s.symbol, true)
	if err != nil {
		return err
	}
	var projection fundingCarryRecoveryProjectionPayload
	if err := json.Unmarshal(outbox.ProjectionPayload, &projection); err != nil {
		return err
	}
	marker := fundingCarryRecoveryProjectionMarker{EpisodeID: episode.ID, EpisodeRevision: outbox.TargetEpisodeRevision,
		OperationID: outbox.OperationID, ProjectionHash: outbox.ProjectionHash, ExpectedRuntimeRevision: outbox.ExpectedRuntimeRevision}
	if !fundingCarryRecoveryProjectionPresent(state, marker, projection.Events, projection.SourceCursor) {
		return errors.New("projected FundingCarry fill runtime state is missing the exact operation marker/events")
	}
	return nil
}

func (s *FundingCarryStrategy) readFundingRecoveryFillOperationRetry(ctx context.Context, request ApplyFundingRecoveryFillsRequest) (ApplyFundingRecoveryFillsResult, bool, error) {
	binding, err := resolveFundingCarryRecoveryBindingFromStrategy(s)
	if err != nil {
		return ApplyFundingRecoveryFillsResult{}, false, err
	}
	receipt, found, err := binding.store.GetRecoveryReceipt(ctx, request.EpisodeID, request.OperationID)
	if err != nil || !found {
		return ApplyFundingRecoveryFillsResult{}, false, err
	}
	if receipt.Actor != request.Actor || receipt.AuditRef != request.AuditRef || receipt.RequestHash == "" {
		return ApplyFundingRecoveryFillsResult{}, false, accounting.ErrRecoveryOperationConflict
	}
	outbox, found, err := binding.store.GetRecoveryOutbox(ctx, request.EpisodeID, request.OperationID)
	if err != nil || !found {
		return ApplyFundingRecoveryFillsResult{}, false, errors.Join(errors.New("response-loss retry receipt has no matching outbox"), err)
	}
	var projection fundingCarryRecoveryProjectionPayload
	if err := json.Unmarshal(outbox.ProjectionPayload, &projection); err != nil || projection.Episode.ID != request.EpisodeID || len(projection.Children) != 1 {
		return ApplyFundingRecoveryFillsResult{}, false, errors.Join(errors.New("response-loss retry outbox payload is invalid"), err)
	}
	child := projection.Children[0]
	if child.ID != request.ChildID || child.Revision != request.ExpectedChildRevision || outbox.TargetEpisodeRevision != request.ExpectedEpisodeRevision+1 {
		return ApplyFundingRecoveryFillsResult{}, false, accounting.ErrRecoveryOperationConflict
	}
	var sourceRevision string
	for _, event := range projection.Events {
		if event.ChildID != request.ChildID {
			return ApplyFundingRecoveryFillsResult{}, false, accounting.ErrRecoveryOperationConflict
		}
		if sourceRevision == "" {
			sourceRevision = event.SourceRevision
		} else if sourceRevision != event.SourceRevision {
			return ApplyFundingRecoveryFillsResult{}, false, accounting.ErrRecoveryOperationConflict
		}
	}
	if sourceRevision == "" || sourceRevision != request.PreviewSourceRevision {
		return ApplyFundingRecoveryFillsResult{}, false, accounting.ErrRecoveryOperationConflict
	}
	var observedAt time.Time
	for _, event := range projection.Events {
		if event.ObservedAt.After(observedAt) {
			observedAt = event.ObservedAt
		}
	}
	if observedAt.IsZero() {
		return ApplyFundingRecoveryFillsResult{}, false, accounting.ErrRecoveryOperationConflict
	}
	op := accounting.RecoveryOperation{EpisodeID: request.EpisodeID, OperationID: request.OperationID, HashVersion: 1,
		ExpectedEpisodeRevision: request.ExpectedEpisodeRevision, ExpectedRuntimeRevision: outbox.ExpectedRuntimeRevision,
		ExpectedChildRevisions: map[string]uint64{request.ChildID: child.Revision}, ExpectedEventRevisions: map[string]uint64{},
		Children: []accounting.RecoveryChild{child}, Events: projection.Events, Actor: request.Actor, AuditRef: request.AuditRef,
		EvidenceHash: receipt.EvidenceHash, EvidenceReadAt: observedAt}
	hash, err := op.CanonicalHash()
	if err != nil || hash != receipt.RequestHash {
		return ApplyFundingRecoveryFillsResult{}, false, errors.Join(fmt.Errorf("reconstructed recovery operation does not match its durable receipt: %w", accounting.ErrRecoveryOperationConflict), err)
	}
	result := ApplyFundingRecoveryFillsResult{Receipt: receipt, Replayed: true, HoldRetained: true, FeesComplete: true}
	for _, event := range projection.Events {
		if event.Kind == accounting.RecoveryFillEvent && event.FillEvidence != nil && !event.FillEvidence.CommissionKnown {
			result.FeesComplete = false
		}
	}
	result.ManualFeeAdjustmentRequired = !result.FeesComplete
	if outbox.State != accounting.RecoveryProjected {
		if err := s.projectFundingRecoveryFillOperation(ctx, binding, request.EpisodeID, request.OperationID); err != nil {
			return result, true, fmt.Errorf("original receipt verified; outbox remains pending and hold is retained: %w", err)
		}
	}
	result.Projected = true
	return result, true, nil
}

func sha256JSON(value any) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(encoded)
	return hex.EncodeToString(hash[:]), nil
}

func validFundingRecoveryIntentHash(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func validFundingRecoveryRawDecimal(value string, positive bool) bool {
	if value == "" || strings.TrimSpace(value) != value || strings.ContainsAny(value, "eE+-/") {
		return false
	}
	digits, dots := 0, 0
	for _, r := range value {
		switch {
		case r >= '0' && r <= '9':
			digits++
		case r == '.':
			dots++
		default:
			return false
		}
	}
	if digits == 0 || dots > 1 || value[0] == '.' || value[len(value)-1] == '.' {
		return false
	}
	amount, ok := new(big.Rat).SetString(value)
	return ok && ((positive && amount.Sign() > 0) || (!positive && amount.Sign() >= 0))
}

func validFundingRecoverySignedRawDecimal(value string) bool {
	if strings.HasPrefix(value, "-") {
		return validFundingRecoveryRawDecimal(strings.TrimPrefix(value, "-"), false)
	}
	return validFundingRecoveryRawDecimal(value, false)
}

func cloneOrderFillBatch(batch *exchange.OrderFillBatch) exchange.OrderFillBatch {
	copy := *batch
	copy.Fills = make([]*exchange.OrderFill, len(batch.Fills))
	for i, fill := range batch.Fills {
		if fill != nil {
			item := *fill
			copy.Fills[i] = &item
		}
	}
	return copy
}
