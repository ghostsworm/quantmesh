package strategy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"quantmesh/accounting"
	"quantmesh/exchange"
	"quantmesh/storage"
)

const fundingRecoveryManualFeeSource = accounting.RecoveryManualFeeAdjustmentSource

type FundingRecoveryFeeAdjustmentPreview struct {
	EpisodeID            string                     `json:"episode_id"`
	ChildID              string                     `json:"child_id"`
	TradeID              string                     `json:"trade_id"`
	EpisodeRevision      uint64                     `json:"episode_revision"`
	ChildRevision        uint64                     `json:"child_revision"`
	SourceRevision       string                     `json:"source_revision"`
	FillEvent            accounting.RecoveryEvent   `json:"fill_event"`
	ExistingAdjustments  []accounting.RecoveryEvent `json:"existing_adjustments"`
	UncoveredFeeTradeIDs []string                   `json:"uncovered_fee_trade_ids"`
	HoldRetained         bool                       `json:"hold_retained"`
}

// ApplyFundingRecoveryFeeAdjustmentRequest contains an operator-attested raw
// fee delta and CAS/evidence references only. It never modifies fill evidence
// or releases the recovery hold.
type ApplyFundingRecoveryFeeAdjustmentRequest struct {
	EpisodeID               string `json:"episode_id"`
	ChildID                 string `json:"child_id"`
	TradeID                 string `json:"trade_id"`
	OperationID             string `json:"operation_id"`
	AdjustmentID            string `json:"adjustment_id"`
	AmountDeltaRaw          string `json:"amount_delta_raw"`
	Asset                   string `json:"asset"`
	EvidenceHash            string `json:"evidence_hash"`
	SourceRevision          string `json:"source_revision"`
	ExpectedEpisodeRevision uint64 `json:"expected_episode_revision"`
	ExpectedChildRevision   uint64 `json:"expected_child_revision"`
	Actor                   string `json:"actor"`
	AuditRef                string `json:"audit_ref"`
	Confirmed               bool   `json:"confirmed"`
}

type ApplyFundingRecoveryFeeAdjustmentResult struct {
	Receipt              accounting.RecoveryReceipt `json:"receipt"`
	Replayed             bool                       `json:"replayed"`
	FeesComplete         bool                       `json:"fees_complete"`
	UncoveredFeeTradeIDs []string                   `json:"uncovered_fee_trade_ids"`
	HoldRetained         bool                       `json:"hold_retained"`
	Projected            bool                       `json:"projected"`
}

func (s *FundingCarryStrategy) PreviewFundingRecoveryFeeAdjustment(ctx context.Context, episodeID, childID, tradeID string) (FundingRecoveryFeeAdjustmentPreview, error) {
	if ctx == nil || s == nil || strings.TrimSpace(episodeID) == "" || strings.TrimSpace(childID) == "" || strings.TrimSpace(tradeID) == "" {
		return FundingRecoveryFeeAdjustmentPreview{}, errors.New("fee preview requires context, episode, child, and trade identities")
	}
	snapshot, err := s.readFundingRecoveryFillSnapshot(ctx, episodeID, childID)
	if err != nil {
		return FundingRecoveryFeeAdjustmentPreview{}, err
	}
	if !snapshot.batch.Complete || !snapshot.batch.FillHistoryComplete {
		return FundingRecoveryFeeAdjustmentPreview{}, errors.New("fee adjustment requires a currently complete authoritative order fill batch")
	}
	events, err := snapshot.binding.store.ListRecoveryEvents(ctx, episodeID)
	if err != nil {
		return FundingRecoveryFeeAdjustmentPreview{}, fmt.Errorf("read durable fee and fill events: %w", err)
	}
	fillEvent, err := fundingRecoveryUnknownFeeFillEvent(events, childID, tradeID)
	if err != nil {
		return FundingRecoveryFeeAdjustmentPreview{}, err
	}
	venueFill, err := fundingRecoveryFindVenueFill(snapshot.fills, tradeID)
	if err != nil {
		return FundingRecoveryFeeAdjustmentPreview{}, err
	}
	if venueFill.CommissionKnown {
		return FundingRecoveryFeeAdjustmentPreview{}, errors.New("authoritative venue fee is now available; refresh fill reconciliation instead of manually entering it")
	}
	if !fundingRecoveryFeeFillFactsMatch(fillEvent, venueFill, snapshot) {
		return FundingRecoveryFeeAdjustmentPreview{}, errors.New("current venue fill economic identity differs from stored fill; fee adjustment refused")
	}
	adjustments := fundingRecoveryFeeAdjustments(events, childID, tradeID)
	uncovered, err := fundingRecoveryUncoveredFeeTradeIDs(events)
	if err != nil {
		return FundingRecoveryFeeAdjustmentPreview{}, err
	}
	return FundingRecoveryFeeAdjustmentPreview{EpisodeID: episodeID, ChildID: childID, TradeID: tradeID,
		EpisodeRevision: snapshot.episode.Revision, ChildRevision: snapshot.child.Revision, SourceRevision: snapshot.batch.SourceRevision,
		FillEvent: fillEvent, ExistingAdjustments: adjustments, UncoveredFeeTradeIDs: uncovered, HoldRetained: true}, nil
}

func (s *FundingCarryStrategy) ApplyFundingRecoveryFeeAdjustment(ctx context.Context, request ApplyFundingRecoveryFeeAdjustmentRequest) (ApplyFundingRecoveryFeeAdjustmentResult, error) {
	if ctx == nil || s == nil || !request.Confirmed || strings.TrimSpace(request.EpisodeID) == "" || strings.TrimSpace(request.ChildID) == "" ||
		strings.TrimSpace(request.TradeID) == "" || strings.TrimSpace(request.OperationID) == "" || !validFundingFeeAdjustmentID(request.AdjustmentID) ||
		!validFundingRecoverySignedRawDecimal(request.AmountDeltaRaw) || !validFundingFeeAsset(request.Asset) ||
		!validFundingRecoveryCanonicalHash(request.EvidenceHash) || strings.TrimSpace(request.SourceRevision) == "" ||
		request.ExpectedEpisodeRevision == 0 || request.ExpectedEpisodeRevision == ^uint64(0) || request.ExpectedChildRevision == 0 ||
		strings.TrimSpace(request.Actor) == "" || strings.TrimSpace(request.AuditRef) == "" {
		return ApplyFundingRecoveryFeeAdjustmentResult{}, errors.New("incomplete explicit FundingCarry fee adjustment authorization")
	}
	if result, found, err := s.readFundingRecoveryFeeAdjustmentRetry(ctx, request); err != nil || found {
		return result, err
	}
	snapshot, err := s.readFundingRecoveryFillSnapshot(ctx, request.EpisodeID, request.ChildID)
	if err != nil {
		return ApplyFundingRecoveryFeeAdjustmentResult{}, err
	}
	if snapshot.episode.Revision != request.ExpectedEpisodeRevision || snapshot.child.Revision != request.ExpectedChildRevision ||
		snapshot.batch.SourceRevision != request.SourceRevision || !snapshot.batch.Complete || !snapshot.batch.FillHistoryComplete {
		return ApplyFundingRecoveryFeeAdjustmentResult{}, accounting.ErrRecoveryConflict
	}
	events, err := snapshot.binding.store.ListRecoveryEvents(ctx, request.EpisodeID)
	if err != nil {
		return ApplyFundingRecoveryFeeAdjustmentResult{}, err
	}
	fillEvent, err := fundingRecoveryUnknownFeeFillEvent(events, request.ChildID, request.TradeID)
	if err != nil {
		return ApplyFundingRecoveryFeeAdjustmentResult{}, err
	}
	venueFill, err := fundingRecoveryFindVenueFill(snapshot.fills, request.TradeID)
	if err != nil {
		return ApplyFundingRecoveryFeeAdjustmentResult{}, err
	}
	if venueFill.CommissionKnown {
		return ApplyFundingRecoveryFeeAdjustmentResult{}, errors.New("authoritative venue fee is now available; refresh fill reconciliation instead of manually entering it")
	}
	if !fundingRecoveryFeeFillFactsMatch(fillEvent, venueFill, snapshot) {
		return ApplyFundingRecoveryFeeAdjustmentResult{}, errors.New("current venue fill economic identity differs from stored fill; fee adjustment refused")
	}
	reader, ok := snapshot.binding.stateStore.(RuntimeStateContextReader)
	if !ok {
		return ApplyFundingRecoveryFeeAdjustmentResult{}, errors.New("durable fee adjustment requires context-aware runtime revision read")
	}
	version, payload, found, err := reader.LoadRuntimeStateContext(ctx, "funding_carry")
	if err != nil || !found || version != fundingCarryRuntimeStateVersion {
		return ApplyFundingRecoveryFeeAdjustmentResult{}, errors.Join(errors.New("read current FundingCarry runtime revision"), err)
	}
	observedAt := time.Now().UTC()
	event := fundingRecoveryFeeAdjustmentEvent(request, snapshot, observedAt)
	if err := event.Validate(); err != nil {
		return ApplyFundingRecoveryFeeAdjustmentResult{}, err
	}
	child := snapshot.child
	op := accounting.RecoveryOperation{EpisodeID: request.EpisodeID, OperationID: request.OperationID, HashVersion: 1,
		ExpectedEpisodeRevision: request.ExpectedEpisodeRevision, ExpectedRuntimeRevision: fundingCarryRuntimeRevision(version, payload),
		ExpectedChildRevisions: map[string]uint64{request.ChildID: request.ExpectedChildRevision}, ExpectedEventRevisions: map[string]uint64{},
		Children: []accounting.RecoveryChild{child}, Events: []accounting.RecoveryEvent{event}, Actor: request.Actor, AuditRef: request.AuditRef,
		EvidenceHash: request.EvidenceHash, EvidenceReadAt: observedAt}
	receipt, replayed, err := snapshot.binding.store.ApplyRecoveryOperation(ctx, snapshot.binding.generation, op)
	if err != nil {
		return ApplyFundingRecoveryFeeAdjustmentResult{}, err
	}
	result := ApplyFundingRecoveryFeeAdjustmentResult{Receipt: receipt, Replayed: replayed, HoldRetained: true}
	if err := s.projectFundingRecoveryFillOperation(ctx, snapshot.binding, request.EpisodeID, request.OperationID); err != nil {
		return result, fmt.Errorf("fee adjustment committed; outbox projection remains pending and hold is retained: %w", err)
	}
	result.Projected = true
	allEvents, err := snapshot.binding.store.ListRecoveryEvents(ctx, request.EpisodeID)
	if err != nil {
		return result, fmt.Errorf("fee adjustment projected; read back fee ledger: %w", err)
	}
	result.UncoveredFeeTradeIDs, err = fundingRecoveryUncoveredFeeTradeIDs(allEvents)
	if err != nil {
		return result, err
	}
	result.FeesComplete = len(result.UncoveredFeeTradeIDs) == 0
	return result, nil
}

func (s *FundingCarryStrategy) readFundingRecoveryFeeAdjustmentRetry(ctx context.Context, request ApplyFundingRecoveryFeeAdjustmentRequest) (ApplyFundingRecoveryFeeAdjustmentResult, bool, error) {
	binding, err := resolveFundingCarryRecoveryBindingFromStrategy(s)
	if err != nil {
		return ApplyFundingRecoveryFeeAdjustmentResult{}, false, err
	}
	receipt, found, err := binding.store.GetRecoveryReceipt(ctx, request.EpisodeID, request.OperationID)
	if err != nil || !found {
		return ApplyFundingRecoveryFeeAdjustmentResult{}, false, err
	}
	if receipt.Actor != request.Actor || receipt.AuditRef != request.AuditRef || receipt.EvidenceHash != request.EvidenceHash {
		return ApplyFundingRecoveryFeeAdjustmentResult{}, false, accounting.ErrRecoveryOperationConflict
	}
	outbox, found, err := binding.store.GetRecoveryOutbox(ctx, request.EpisodeID, request.OperationID)
	if err != nil || !found {
		return ApplyFundingRecoveryFeeAdjustmentResult{}, false, errors.Join(errors.New("fee adjustment receipt has no outbox"), err)
	}
	if err := storage.VerifyFundingRecoveryReceiptOutboxPair(receipt, outbox); err != nil {
		return ApplyFundingRecoveryFeeAdjustmentResult{}, false, fmt.Errorf("verify funding fee response-loss outbox receipt/outbox: %w", err)
	}
	if outbox.State == accounting.RecoveryProjected && receipt.EpisodeRevision != outbox.TargetEpisodeRevision+1 {
		return ApplyFundingRecoveryFeeAdjustmentResult{}, false, accounting.ErrRecoveryOperationConflict
	}
	var projection fundingCarryRecoveryProjectionPayload
	if err := storage.VerifyFundingRecoveryProjectionHash(outbox.ProjectionPayload, outbox.ProjectionHash); err != nil {
		return ApplyFundingRecoveryFeeAdjustmentResult{}, false, fmt.Errorf("verify funding fee response-loss outbox: %w", err)
	}
	if err := json.Unmarshal(outbox.ProjectionPayload, &projection); err != nil || len(projection.Children) != 1 || len(projection.Events) != 1 ||
		outbox.TargetEpisodeRevision != request.ExpectedEpisodeRevision+1 {
		return ApplyFundingRecoveryFeeAdjustmentResult{}, false, errors.Join(errors.New("fee adjustment response-loss outbox is invalid"), err)
	}
	child, event := projection.Children[0], projection.Events[0]
	if child.ID != request.ChildID || child.Revision != request.ExpectedChildRevision || event != fundingRecoveryFeeAdjustmentEvent(request,
		fundingRecoveryFillSnapshot{child: child}, event.ObservedAt) {
		return ApplyFundingRecoveryFeeAdjustmentResult{}, false, accounting.ErrRecoveryOperationConflict
	}
	op := accounting.RecoveryOperation{EpisodeID: request.EpisodeID, OperationID: request.OperationID, HashVersion: 1,
		ExpectedEpisodeRevision: request.ExpectedEpisodeRevision, ExpectedRuntimeRevision: outbox.ExpectedRuntimeRevision,
		ExpectedChildRevisions: map[string]uint64{request.ChildID: child.Revision}, ExpectedEventRevisions: map[string]uint64{},
		Children: []accounting.RecoveryChild{child}, Events: projection.Events, Actor: request.Actor, AuditRef: request.AuditRef,
		EvidenceHash: request.EvidenceHash, EvidenceReadAt: event.ObservedAt}
	hash, err := op.CanonicalHash()
	if err != nil || hash != receipt.RequestHash {
		return ApplyFundingRecoveryFeeAdjustmentResult{}, false, errors.Join(fmt.Errorf("fee adjustment retry hash does not match durable receipt: %w", accounting.ErrRecoveryOperationConflict), err)
	}
	if err := s.projectFundingRecoveryFillOperation(ctx, binding, request.EpisodeID, request.OperationID); err != nil {
		return ApplyFundingRecoveryFeeAdjustmentResult{}, true, err
	}
	events, err := binding.store.ListRecoveryEvents(ctx, request.EpisodeID)
	if err != nil {
		return ApplyFundingRecoveryFeeAdjustmentResult{}, true, err
	}
	uncovered, err := fundingRecoveryUncoveredFeeTradeIDs(events)
	if err != nil {
		return ApplyFundingRecoveryFeeAdjustmentResult{}, true, err
	}
	return ApplyFundingRecoveryFeeAdjustmentResult{Receipt: receipt, Replayed: true, FeesComplete: len(uncovered) == 0,
		UncoveredFeeTradeIDs: uncovered, HoldRetained: true, Projected: true}, true, nil
}

func fundingRecoveryFeeAdjustmentEvent(request ApplyFundingRecoveryFeeAdjustmentRequest, snapshot fundingRecoveryFillSnapshot, observedAt time.Time) accounting.RecoveryEvent {
	asset := strings.ToUpper(strings.TrimSpace(request.Asset))
	idDigest := sha256.Sum256([]byte(request.EpisodeID + "\x00" + request.ChildID + "\x00" + request.TradeID + "\x00" + request.AdjustmentID))
	return accounting.RecoveryEvent{ID: "fee_adjustment:" + hex.EncodeToString(idDigest[:]), Source: fundingRecoveryManualFeeSource,
		Exchange: snapshot.child.Exchange, Market: snapshot.child.Market, AccountScope: snapshot.child.AccountScope,
		Kind: accounting.RecoveryFeeEvent, ExternalID: request.AdjustmentID, RelatedExternalID: request.TradeID,
		EpisodeID: request.EpisodeID, ChildID: request.ChildID, Asset: asset, Amount: request.AmountDeltaRaw,
		SourceRevision: request.SourceRevision, SourceHash: request.EvidenceHash, ObservedAt: observedAt.UTC()}
}

func fundingRecoveryUnknownFeeFillEvent(events []accounting.RecoveryEvent, childID, tradeID string) (accounting.RecoveryEvent, error) {
	var matched accounting.RecoveryEvent
	count := 0
	for _, event := range events {
		if event.ChildID == childID && event.Kind == accounting.RecoveryFillEvent && event.ExternalID == tradeID {
			matched = event
			count++
		}
	}
	if count != 1 || matched.Validate() != nil || matched.FillEvidence == nil || matched.FillEvidence.CommissionKnown {
		return accounting.RecoveryEvent{}, errors.New("fee adjustment requires exactly one persisted fill with unknown commission")
	}
	return matched, nil
}

func fundingRecoveryFindVenueFill(fills []*exchange.OrderFill, tradeID string) (*exchange.OrderFill, error) {
	var matched *exchange.OrderFill
	for _, fill := range fills {
		if fill != nil && fill.TradeID == tradeID {
			if matched != nil {
				return nil, errors.New("venue fill batch repeats the requested trade ID")
			}
			matched = fill
		}
	}
	if matched == nil {
		return nil, errors.New("requested fee trade is absent from the complete venue batch")
	}
	return matched, nil
}

func fundingRecoveryFeeFillFactsMatch(event accounting.RecoveryEvent, fill *exchange.OrderFill, snapshot fundingRecoveryFillSnapshot) bool {
	evidence := event.FillEvidence
	return evidence != nil && fill != nil && event.ChildID == snapshot.child.ID && event.ExternalID == fill.TradeID &&
		evidence.ClientOrderID == snapshot.child.ClientOrderID && evidence.VenueOrderID == snapshot.child.VenueOrderID &&
		evidence.Symbol == fill.Symbol && evidence.Symbol == snapshot.child.Symbol && evidence.Side == string(fill.Side) && evidence.Side == snapshot.child.Side &&
		evidence.OrderIntentHash == snapshot.child.OrderIntentHash && evidence.PriceRaw == fill.PriceRaw && evidence.QuantityRaw == fill.QuantityRaw &&
		evidence.QuoteQuantityRaw == fill.QuoteQuantityRaw && evidence.RealizedPnLKnown == fill.RealizedPnLKnown &&
		evidence.RealizedPnLRaw == fill.RealizedPnLRaw && evidence.RealizedPnLAsset == fill.RealizedPnLAsset
}

func fundingRecoveryFeeAdjustments(events []accounting.RecoveryEvent, childID, tradeID string) []accounting.RecoveryEvent {
	adjustments := make([]accounting.RecoveryEvent, 0)
	for _, event := range events {
		if event.Kind == accounting.RecoveryFeeEvent && event.ChildID == childID && event.RelatedExternalID == tradeID {
			adjustments = append(adjustments, event)
		}
	}
	sort.Slice(adjustments, func(i, j int) bool { return adjustments[i].ExternalID < adjustments[j].ExternalID })
	return adjustments
}

func fundingRecoveryUncoveredFeeTradeIDs(events []accounting.RecoveryEvent) ([]string, error) {
	covered := make(map[string]struct{})
	unknown := make(map[string]struct{})
	for _, event := range events {
		key := event.ChildID + "\x00" + event.ExternalID
		switch event.Kind {
		case accounting.RecoveryFillEvent:
			if event.FillEvidence == nil {
				return nil, errors.New("durable fill is missing fee-known evidence")
			}
			if !event.FillEvidence.CommissionKnown {
				unknown[key] = struct{}{}
			}
		case accounting.RecoveryFeeEvent:
			covered[event.ChildID+"\x00"+event.RelatedExternalID] = struct{}{}
		}
	}
	out := make([]string, 0)
	for key := range unknown {
		if _, ok := covered[key]; !ok {
			out = append(out, strings.SplitN(key, "\x00", 2)[1])
		}
	}
	sort.Strings(out)
	return out, nil
}

func validFundingRecoveryCanonicalHash(value string) bool {
	return len(value) == sha256.Size*2 && strings.ToLower(value) == value && validFundingRecoveryIntentHash(value)
}

func validFundingFeeAdjustmentID(value string) bool {
	if len(value) == 0 || len(value) > 128 || strings.TrimSpace(value) != value {
		return false
	}
	for _, char := range value {
		if !(char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || strings.ContainsRune("._:-", char)) {
			return false
		}
	}
	return true
}

func validFundingFeeAsset(value string) bool {
	value = strings.ToUpper(strings.TrimSpace(value))
	if len(value) == 0 || len(value) > 32 {
		return false
	}
	for _, char := range value {
		if !(char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || strings.ContainsRune("._-", char)) {
			return false
		}
	}
	return true
}
