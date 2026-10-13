package strategy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"quantmesh/accounting"
	"quantmesh/exchange"
	"quantmesh/exchange/income"
	"quantmesh/execution"
	"quantmesh/storage"
)

const fundingCarryIncomeSafetyLag = 2 * time.Minute

var fundingIncomeRawDecimalPattern = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?$`)

type fundingCarryIncomeScopeProvider interface {
	FundingCarryFundingIncomeScope() string
}

type ApplyFundingRecoveryIncomeRequest struct {
	EpisodeID               string    `json:"episode_id"`
	OperationID             string    `json:"operation_id"`
	ExpectedEpisodeRevision uint64    `json:"expected_episode_revision"`
	From                    time.Time `json:"from"`
	Actor                   string    `json:"actor"`
	AuditRef                string    `json:"audit_ref"`
	Confirmed               bool      `json:"confirmed"`
}

type ApplyFundingRecoveryIncomeResult struct {
	Receipt      accounting.RecoveryReceipt      `json:"receipt"`
	SourceWindow accounting.RecoverySourceWindow `json:"source_window"`
	Events       []accounting.RecoveryEvent      `json:"events"`
	Replayed     bool                            `json:"replayed"`
	Projected    bool                            `json:"projected"`
	HoldRetained bool                            `json:"hold_retained"`
}

type fundingIncomeContinuationSource interface {
	GetFundingIncomeContinuation(context.Context, string, time.Time, time.Time, time.Duration) (exchange.BinanceFundingIncomeContinuation, error)
}

// ApplyFundingRecoveryIncome performs an operator-triggered, read-only
// Binance double scan and durably records its exact funding rows with the
// source cursor/outbox CAS. It never releases a hold or invokes execution.
func (s *FundingCarryStrategy) ApplyFundingRecoveryIncome(ctx context.Context, request ApplyFundingRecoveryIncomeRequest) (ApplyFundingRecoveryIncomeResult, error) {
	if ctx == nil || s == nil || !request.Confirmed || strings.TrimSpace(request.EpisodeID) == "" ||
		strings.TrimSpace(request.OperationID) == "" || request.ExpectedEpisodeRevision == 0 || request.ExpectedEpisodeRevision >= accounting.MaxDurableRecoveryRevision-1 || request.From.IsZero() ||
		request.From.Nanosecond()%int(time.Millisecond) != 0 || strings.TrimSpace(request.Actor) == "" || strings.TrimSpace(request.AuditRef) == "" {
		return ApplyFundingRecoveryIncomeResult{}, errors.New("incomplete explicit FundingCarry funding-income reconciliation authorization")
	}
	binding, err := resolveFundingCarryRecoveryBindingFromStrategy(s)
	if err != nil {
		return ApplyFundingRecoveryIncomeResult{}, err
	}
	sourceScopeProvider, ok := binding.stateStore.(fundingCarryIncomeScopeProvider)
	if !ok {
		return ApplyFundingRecoveryIncomeResult{}, errors.New("runtime does not expose the exact futures ownership scope for income evidence")
	}
	sourceScope := sourceScopeProvider.FundingCarryFundingIncomeScope()
	if sourceScope == "" || !containsFundingCarryRecoveryScope(binding.scopeKeys, sourceScope) {
		return ApplyFundingRecoveryIncomeResult{}, storage.ErrFundingRecoveryGenerationScopeMismatch
	}
	if result, found, err := s.readFundingRecoveryIncomeRetry(ctx, binding, request); err != nil || found {
		return result, err
	}
	episode, found, err := binding.store.GetRecoveryEpisode(ctx, request.EpisodeID)
	if err != nil || !found {
		return ApplyFundingRecoveryIncomeResult{}, errors.Join(storage.ErrFundingRecoveryNotFound, err)
	}
	if episode.Bot != binding.botID || episode.Strategy != "funding_carry" || episode.OwnerGeneration != binding.generation.ID() ||
		episode.Revision != request.ExpectedEpisodeRevision ||
		(episode.State != accounting.RecoveryBlocked && episode.State != accounting.RecoveryProjected) || !episode.HasCurrentOwnerTakeover() {
		return ApplyFundingRecoveryIncomeResult{}, accounting.ErrRecoveryConflict
	}
	episodeScopes, err := normalizeFundingCarryRecoveryScopes(episode.AccountScopes)
	if err != nil || !equalFundingCarryRecoveryScopes(episodeScopes, binding.scopeKeys) {
		return ApplyFundingRecoveryIncomeResult{}, storage.ErrFundingRecoveryGenerationScopeMismatch
	}
	hold, held, err := binding.store.GetRecoveryHold(ctx, episode.ID, episode.RecoveryGeneration)
	if err != nil || !held || hold.State != "held" || hold.Bot != episode.Bot || hold.Strategy != episode.Strategy || hold.Generation != episode.RecoveryGeneration {
		return ApplyFundingRecoveryIncomeResult{}, errors.Join(errors.New("exact durable recovery hold is required"), err)
	}
	holdScopes, err := normalizeFundingCarryRecoveryScopes(hold.AccountScopes)
	if err != nil || !equalFundingCarryRecoveryScopes(holdScopes, binding.scopeKeys) {
		return ApplyFundingRecoveryIncomeResult{}, storage.ErrFundingRecoveryGenerationScopeMismatch
	}
	s.mu.RLock()
	gate, venue := s.openingGate, s.fut
	s.mu.RUnlock()
	if gate == nil || !gate.HasBlock(execution.RecoveryEpisodeGateSource(episode.ID, episode.RecoveryGeneration)) {
		return ApplyFundingRecoveryIncomeResult{}, errors.New("exact recovery episode Gate source is not active")
	}
	source, ok := venue.(exchange.FundingIncomeHistoryWindowSource)
	if !ok || venue == nil || !strings.EqualFold(venue.GetName(), "binance") || !strings.EqualFold(venue.GetMarketType(), "futures") {
		return ApplyFundingRecoveryIncomeResult{}, errors.New("retained futures venue lacks the bounded Binance funding-income source")
	}
	asset := strings.ToUpper(strings.TrimSpace(venue.GetQuoteAsset()))
	if asset == "" {
		return ApplyFundingRecoveryIncomeResult{}, errors.New("retained Binance futures venue has no authoritative settlement asset")
	}
	identity := accounting.RecoverySourceIdentity{Source: accounting.RecoverySourceBinanceFuturesIncomeV1,
		Exchange: "binance", Market: "futures", AccountScope: sourceScope, Symbol: strings.ToUpper(strings.TrimSpace(s.symbol)),
		Kind: accounting.RecoveryFundingEvent, Asset: asset}
	if err := identity.Validate(); err != nil {
		return ApplyFundingRecoveryIncomeResult{}, err
	}
	oldCursor, cursorFound, err := binding.store.GetRecoverySourceCursor(ctx, identity)
	if err != nil {
		return ApplyFundingRecoveryIncomeResult{}, err
	}
	if !cursorFound {
		if err := validateFundingIncomeInitialFrom(request.From, episode.CreatedAt); err != nil {
			return ApplyFundingRecoveryIncomeResult{}, err
		}
	}
	if cursorFound && (oldCursor.EpisodeID != episode.ID || request.From.UTC().Equal(oldCursor.Through.UTC()) == false) {
		return ApplyFundingRecoveryIncomeResult{}, accounting.ErrRecoveryConflict
	}
	initialFrom := request.From.UTC()
	var priorEvidenceHash string
	var historicalRows []*income.Income
	var venueWindow exchange.BinanceFundingIncomeWindow
	var continuation exchange.BinanceFundingIncomeContinuation
	if cursorFound {
		if oldCursor.InitialFrom.IsZero() || !validRecoveryEvidenceDigest(oldCursor.HistoricalEvidenceHash) {
			return ApplyFundingRecoveryIncomeResult{}, errors.New("prior Binance cursor lacks a durable initial watermark or cumulative history digest; cursor remains held for manual review")
		}
		initialFrom = oldCursor.InitialFrom.UTC()
		continuationSource, supported := venue.(fundingIncomeContinuationSource)
		if !supported {
			return ApplyFundingRecoveryIncomeResult{}, errors.New("retained Binance source cannot verify the full accepted funding history; hold remains active")
		}
		var continuationErr error
		continuation, continuationErr = continuationSource.GetFundingIncomeContinuation(ctx, identity.Symbol, initialFrom, oldCursor.Through, fundingCarryIncomeSafetyLag)
		if continuationErr != nil {
			return ApplyFundingRecoveryIncomeResult{}, fmt.Errorf("re-read full accepted funding history before cursor advance: %w", continuationErr)
		}
		if continuation.InitialFrom != initialFrom || !continuation.Window.From.Equal(oldCursor.Through) || continuation.Window.Through.IsZero() ||
			!continuation.Window.From.Before(continuation.Window.Through) || continuation.Window.ReadAt.Before(continuation.Window.Through) ||
			continuation.Window.CompletionMarker != "complete_stable_double_scan_v1" || continuation.Window.TotalRows != uint64(len(continuation.Window.Rows)) ||
			!validRecoveryEvidenceDigest(continuation.PriorEvidenceHash) || !validRecoveryEvidenceDigest(continuation.CumulativeEvidenceHash) {
			return ApplyFundingRecoveryIncomeResult{}, errors.New("Binance historical continuation is incomplete or mismatched; cursor not advanced and hold remains active")
		}
		priorEvidenceHash = continuation.CumulativeEvidenceHash
		historicalRows = continuation.HistoricalRows
		venueWindow = continuation.Window
	} else {
		if err := ctx.Err(); err != nil {
			return ApplyFundingRecoveryIncomeResult{}, err
		}
		venueWindow, err = source.GetFundingIncomeWindow(ctx, identity.Symbol, request.From.UTC(), fundingCarryIncomeSafetyLag)
		if err != nil {
			return ApplyFundingRecoveryIncomeResult{}, fmt.Errorf("collect stable Binance FUNDING_FEE window: %w", err)
		}
		initialFrom = venueWindow.From
		priorEvidenceHash = venueWindow.EvidenceHash
	}
	if venueWindow.Symbol != identity.Symbol || !venueWindow.From.Equal(request.From.UTC()) || venueWindow.Through.IsZero() ||
		!venueWindow.From.Before(venueWindow.Through) || venueWindow.ReadAt.Before(venueWindow.Through) ||
		venueWindow.CompletionMarker != "complete_stable_double_scan_v1" || venueWindow.TotalRows != uint64(len(venueWindow.Rows)) || !validRecoveryEvidenceDigest(venueWindow.EvidenceHash) {
		return ApplyFundingRecoveryIncomeResult{}, errors.New("Binance returned incomplete or mismatched funding-window evidence")
	}
	verifiedAnomalyIDs := make([]string, 0)
	if cursorFound {
		stored, listErr := binding.store.ListRecoveryEvents(ctx, episode.ID)
		if listErr != nil {
			return ApplyFundingRecoveryIncomeResult{}, fmt.Errorf("read prior durable funding facts before cursor advance: %w", listErr)
		}
		anomalies, anomalyErr := binding.store.ListRecoverySourceAnomalies(ctx, episode.ID)
		if anomalyErr != nil {
			return ApplyFundingRecoveryIncomeResult{}, anomalyErr
		}
		for _, anomaly := range anomalies {
			if anomaly.Identity == identity && anomaly.State == accounting.RecoverySourceAnomalyOpen {
				return ApplyFundingRecoveryIncomeResult{}, fmt.Errorf("funding source anomaly %s is durably open; apply an explicit audited adjustment before retry", anomaly.ID)
			}
		}
		facts, diffErr := diffFundingRecoveryIncomeHistory(episode.ID, identity, initialFrom, oldCursor.Through, historicalRows, stored)
		if diffErr != nil {
			return ApplyFundingRecoveryIncomeResult{}, fmt.Errorf("compare accepted funding history: %w", diffErr)
		}
		hasMatchingAdjustment := false
		for _, anomaly := range anomalies {
			if anomaly.Identity == identity && anomaly.CursorRevision == oldCursor.Revision && anomaly.ObservedHistoryHash == continuation.PriorEvidenceHash &&
				anomaly.State == accounting.RecoverySourceAnomalyAdjustmentApplied {
				hasMatchingAdjustment = true
			}
		}
		if len(facts) > 0 || (continuation.PriorEvidenceHash != oldCursor.HistoricalEvidenceHash && !hasMatchingAdjustment) {
			anomaly, captureErr := s.captureFundingRecoveryIncomeAnomaly(ctx, binding, episode, identity, oldCursor, continuation, facts, request)
			if captureErr != nil {
				return ApplyFundingRecoveryIncomeResult{}, captureErr
			}
			return ApplyFundingRecoveryIncomeResult{}, fmt.Errorf("funding source history anomaly %s captured; cursor remains unchanged and hold remains active", anomaly.ID)
		}
		if err := verifyFundingRecoveryIncomeHistory(episode.ID, identity, initialFrom, oldCursor.Through, historicalRows, stored); err != nil {
			return ApplyFundingRecoveryIncomeResult{}, fmt.Errorf("accepted funding history differs from durable ledger; cursor remains held: %w", err)
		}
		for _, anomaly := range anomalies {
			if anomaly.Identity == identity && anomaly.CursorRevision == oldCursor.Revision && anomaly.State == accounting.RecoverySourceAnomalyAdjustmentApplied {
				verifiedAnomalyIDs = append(verifiedAnomalyIDs, anomaly.ID)
			}
		}
	}
	events, err := makeFundingRecoveryIncomeEvents(episode.ID, identity, venueWindow.EvidenceHash, venueWindow.Rows)
	if err != nil {
		return ApplyFundingRecoveryIncomeResult{}, err
	}
	window := accounting.RecoverySourceWindow{Source: identity.Source, Exchange: identity.Exchange, Market: identity.Market,
		AccountScope: identity.AccountScope, Symbol: identity.Symbol, Kind: identity.Kind, Asset: identity.Asset,
		From: venueWindow.From.UTC(), Through: venueWindow.Through.UTC(), Rows: uint64(len(events)), TotalRows: venueWindow.TotalRows,
		CompletionMarker: accounting.RecoverySourceWindowCompleteV1, CollectorEvidenceHash: venueWindow.EvidenceHash,
		InitialFrom: initialFrom.UTC(), HistoricalEvidenceHash: priorEvidenceHash, ExpectedRevision: 0}
	if cursorFound {
		window.ExpectedRevision = oldCursor.Revision
	}
	window.EvidenceHash, err = accounting.RecoverySourceWindowEvidenceHash(window, events)
	if err != nil {
		return ApplyFundingRecoveryIncomeResult{}, fmt.Errorf("hash exact Binance funding source rows: %w", err)
	}
	reader, ok := binding.stateStore.(RuntimeStateContextReader)
	if !ok {
		return ApplyFundingRecoveryIncomeResult{}, errors.New("funding-income reconciliation requires context-aware runtime revision read")
	}
	version, payload, runtimeFound, err := reader.LoadRuntimeStateContext(ctx, "funding_carry")
	if err != nil || !runtimeFound || version != fundingCarryRuntimeStateVersion {
		return ApplyFundingRecoveryIncomeResult{}, errors.Join(errors.New("read current FundingCarry runtime revision"), err)
	}
	operationEvidence, err := sha256JSON(struct {
		VenueScanDigest string
		WindowDigest    string
		Rows            []accounting.RecoveryEvent
	}{venueWindow.EvidenceHash, window.EvidenceHash, events})
	if err != nil {
		return ApplyFundingRecoveryIncomeResult{}, err
	}
	operation := accounting.RecoveryOperation{EpisodeID: episode.ID, OperationID: request.OperationID, HashVersion: 1,
		ExpectedEpisodeRevision: episode.Revision, ExpectedRuntimeRevision: fundingCarryRuntimeRevision(version, payload),
		ExpectedChildRevisions: map[string]uint64{}, ExpectedEventRevisions: map[string]uint64{}, Events: events, SourceWindow: &window,
		Actor: request.Actor, AuditRef: request.AuditRef, EvidenceHash: operationEvidence, EvidenceReadAt: venueWindow.ReadAt.UTC()}
	operation.VerifiedAnomalyIDs = verifiedAnomalyIDs
	receipt, replayed, err := binding.store.ApplyRecoveryOperation(ctx, binding.generation, operation)
	if err != nil {
		return ApplyFundingRecoveryIncomeResult{}, err
	}
	result := ApplyFundingRecoveryIncomeResult{Receipt: receipt, SourceWindow: window, Events: events, Replayed: replayed, HoldRetained: true}
	if err := s.projectFundingRecoveryFillOperation(ctx, binding, episode.ID, request.OperationID); err != nil {
		return result, fmt.Errorf("funding facts committed; projection remains pending and hold is retained: %w", err)
	}
	currentHold, stillHeld, err := binding.store.GetRecoveryHold(ctx, episode.ID, episode.RecoveryGeneration)
	if err != nil || !stillHeld || currentHold.State != "held" {
		return result, errors.Join(errors.New("funding-income readback lost the recovery hold"), err)
	}
	result.Projected = true
	return result, nil
}

func (s *FundingCarryStrategy) readFundingRecoveryIncomeRetry(ctx context.Context, binding fundingCarryRecoveryBinding, request ApplyFundingRecoveryIncomeRequest) (ApplyFundingRecoveryIncomeResult, bool, error) {
	receipt, found, err := binding.store.GetRecoveryReceipt(ctx, request.EpisodeID, request.OperationID)
	if err != nil || !found {
		return ApplyFundingRecoveryIncomeResult{}, false, err
	}
	if receipt.Actor != request.Actor || receipt.AuditRef != request.AuditRef || receipt.OperationID != request.OperationID || receipt.EpisodeID != request.EpisodeID {
		return ApplyFundingRecoveryIncomeResult{}, true, accounting.ErrRecoveryOperationConflict
	}
	outbox, found, err := binding.store.GetRecoveryOutbox(ctx, request.EpisodeID, request.OperationID)
	if err != nil || !found || (outbox.State != accounting.RecoveryProjectionPending && outbox.State != accounting.RecoveryProjected) {
		return ApplyFundingRecoveryIncomeResult{}, true, errors.Join(errors.New("funding-income retry has no valid durable outbox"), err)
	}
	if err := storage.VerifyFundingRecoveryProjectionHash(outbox.ProjectionPayload, outbox.ProjectionHash); err != nil {
		return ApplyFundingRecoveryIncomeResult{}, true, fmt.Errorf("funding-income retry projection hash mismatch: %w", err)
	}
	var projection fundingCarryRecoveryProjectionPayload
	if err := json.Unmarshal(outbox.ProjectionPayload, &projection); err != nil {
		return ApplyFundingRecoveryIncomeResult{}, true, err
	}
	if projection.SourceCursor == nil || projection.Episode.ID != request.EpisodeID || projection.Episode.Bot != binding.botID ||
		projection.Episode.Strategy != "funding_carry" || projection.SourceCursor.EpisodeID != request.EpisodeID ||
		projection.SourceCursor.RecoverySourceWindow.Kind != accounting.RecoveryFundingEvent ||
		projection.SourceCursor.RecoverySourceWindow.Source != accounting.RecoverySourceBinanceFuturesIncomeV1 ||
		projection.Episode.Revision != outbox.TargetEpisodeRevision || !projection.SourceCursor.RecoverySourceWindow.From.Equal(request.From.UTC()) ||
		projection.SourceCursor.RecoverySourceWindow.InitialFrom.IsZero() || projection.SourceCursor.RecoverySourceWindow.InitialFrom.After(projection.SourceCursor.RecoverySourceWindow.From) ||
		!validRecoveryEvidenceDigest(projection.SourceCursor.RecoverySourceWindow.HistoricalEvidenceHash) {
		return ApplyFundingRecoveryIncomeResult{}, true, errors.New("funding-income retry outbox does not contain the exact source cursor")
	}
	currentEpisode, currentFound, err := binding.store.GetRecoveryEpisode(ctx, request.EpisodeID)
	if err != nil || !currentFound || currentEpisode.Bot != binding.botID || currentEpisode.Strategy != "funding_carry" ||
		currentEpisode.OwnerGeneration != binding.generation.ID() || !currentEpisode.HasCurrentOwnerTakeover() {
		return ApplyFundingRecoveryIncomeResult{}, true, errors.Join(accounting.ErrRecoveryConflict, err)
	}
	if err := validateFundingIncomeRetryRevision(receipt, outbox, request, projection.Episode, currentEpisode); err != nil {
		return ApplyFundingRecoveryIncomeResult{}, true, err
	}
	if receipt.EpisodeRevision-outbox.TargetEpisodeRevision == 2 {
		takeover := currentEpisode.OwnerTakeovers[len(currentEpisode.OwnerTakeovers)-1]
		takeoverReceipt, takeoverFound, takeoverErr := binding.store.GetRecoveryReceipt(ctx, currentEpisode.ID, takeover.OperationID)
		if takeoverErr != nil || !takeoverFound {
			return ApplyFundingRecoveryIncomeResult{}, true, errors.Join(errors.New("funding-income replay takeover receipt unavailable"), takeoverErr)
		}
		err = storage.VerifyFundingRecoveryReceiptOutboxPairAfterVerifiedTakeover(receipt, takeoverReceipt, outbox, currentEpisode)
	} else {
		err = storage.VerifyFundingRecoveryReceiptOutboxPair(receipt, outbox)
	}
	if err != nil {
		return ApplyFundingRecoveryIncomeResult{}, true, fmt.Errorf("funding-income retry receipt/outbox mismatch: %w", err)
	}
	currentScopes, scopeErr := normalizeFundingCarryRecoveryScopes(currentEpisode.AccountScopes)
	if scopeErr != nil || !equalFundingCarryRecoveryScopes(currentScopes, binding.scopeKeys) {
		return ApplyFundingRecoveryIncomeResult{}, true, storage.ErrFundingRecoveryGenerationScopeMismatch
	}
	s.mu.RLock()
	gate := s.openingGate
	s.mu.RUnlock()
	if gate == nil || !gate.HasBlock(execution.RecoveryEpisodeGateSource(currentEpisode.ID, currentEpisode.RecoveryGeneration)) {
		return ApplyFundingRecoveryIncomeResult{}, true, errors.New("exact recovery Gate is not active during source-operation retry")
	}
	if err := s.projectFundingRecoveryFillOperation(ctx, binding, request.EpisodeID, request.OperationID); err != nil {
		return ApplyFundingRecoveryIncomeResult{}, true, err
	}
	hold, held, err := binding.store.GetRecoveryHold(ctx, request.EpisodeID, projection.Episode.RecoveryGeneration)
	if err != nil || !held || hold.State != "held" {
		return ApplyFundingRecoveryIncomeResult{}, true, errors.Join(errors.New("funding-income retry could not verify held recovery state"), err)
	}
	return ApplyFundingRecoveryIncomeResult{Receipt: receipt, SourceWindow: projection.SourceCursor.RecoverySourceWindow,
		Events: append([]accounting.RecoveryEvent(nil), projection.Events...), Replayed: true, Projected: true, HoldRetained: true}, true, nil
}

func validateFundingIncomeInitialFrom(from, episodeCreatedAt time.Time) error {
	if from.IsZero() || episodeCreatedAt.IsZero() || from.UTC().After(episodeCreatedAt.UTC()) {
		return errors.New("initial funding-income coverage must begin no later than the recovery episode creation time")
	}
	return nil
}

func validateFundingIncomeRetryRevision(receipt accounting.RecoveryReceipt, outbox accounting.RecoveryOutbox, request ApplyFundingRecoveryIncomeRequest,
	projectedEpisode, currentEpisode accounting.RecoveryEpisode) error {
	if request.ExpectedEpisodeRevision >= accounting.MaxDurableRecoveryRevision-1 || outbox.TargetEpisodeRevision != request.ExpectedEpisodeRevision+1 ||
		receipt.EpisodeID != request.EpisodeID || receipt.OperationID != request.OperationID ||
		outbox.EpisodeID != request.EpisodeID || outbox.OperationID != request.OperationID || receipt.ProjectionHash == "" || receipt.ProjectionHash != outbox.ProjectionHash ||
		projectedEpisode.ID != request.EpisodeID || projectedEpisode.Revision != outbox.TargetEpisodeRevision || receipt.OwnerGeneration != projectedEpisode.OwnerGeneration ||
		(outbox.State == accounting.RecoveryProjectionPending && (receipt.State != accounting.RecoveryLedgerCommitted || receipt.EpisodeRevision != outbox.TargetEpisodeRevision)) ||
		(outbox.State == accounting.RecoveryProjected && !fundingIncomeProjectedReceiptRevisionMatches(receipt, outbox, projectedEpisode, currentEpisode)) {
		return accounting.ErrRecoveryOperationConflict
	}
	return nil
}

func fundingIncomeProjectedReceiptRevisionMatches(receipt accounting.RecoveryReceipt, outbox accounting.RecoveryOutbox,
	projectedEpisode, currentEpisode accounting.RecoveryEpisode) bool {
	if receipt.State != accounting.RecoveryProjected || outbox.TargetEpisodeRevision >= accounting.MaxDurableRecoveryRevision {
		return false
	}
	if receipt.EpisodeRevision == outbox.TargetEpisodeRevision+1 {
		return true
	}
	if outbox.TargetEpisodeRevision >= accounting.MaxDurableRecoveryRevision-1 ||
		receipt.EpisodeRevision != outbox.TargetEpisodeRevision+2 || currentEpisode.ID != projectedEpisode.ID {
		return false
	}
	for _, takeover := range currentEpisode.OwnerTakeovers {
		if takeover.EpisodeRevision == outbox.TargetEpisodeRevision+1 &&
			takeover.PreviousOwnerGeneration == projectedEpisode.OwnerGeneration && takeover.OwnerGeneration != "" &&
			takeover.OwnerGeneration == currentEpisode.OwnerGeneration && takeover.OwnerGeneration != takeover.PreviousOwnerGeneration &&
			takeover.OperationID != "" && takeover.Actor != "" &&
			takeover.AuditRef != "" && validRecoveryEvidenceDigest(takeover.EvidenceHash) {
			return true
		}
	}
	return false
}

func makeFundingRecoveryIncomeEvents(episodeID string, identity accounting.RecoverySourceIdentity, sourceRevision string, rows []*income.Income) ([]accounting.RecoveryEvent, error) {
	events := make([]accounting.RecoveryEvent, 0, len(rows))
	for _, row := range rows {
		if row == nil || row.TransactionID <= 0 || row.Symbol != identity.Symbol || row.IncomeType != "FUNDING_FEE" || row.Asset != identity.Asset || row.RawIncome == "" || row.TradeTime.IsZero() {
			return nil, errors.New("Binance funding row has incomplete or mismatched identity/asset")
		}
		if !fundingIncomeRawDecimalPattern.MatchString(row.RawIncome) {
			return nil, errors.New("Binance funding row has invalid raw decimal")
		}
		if _, ok := new(big.Rat).SetString(row.RawIncome); !ok {
			return nil, errors.New("invalid Binance funding amount; cursor not advanced")
		}
		rowBytes, marshalErr := sha256JSON(struct {
			TransactionID int64
			Symbol        string
			Type          string
			RawAmount     string
			Asset         string
			Info          string
			TradeTime     int64
		}{row.TransactionID, row.Symbol, row.IncomeType, row.RawIncome, row.Asset, row.Info, row.TradeTime.UnixMilli()})
		if marshalErr != nil {
			return nil, marshalErr
		}
		eventIdentity := strings.Join([]string{episodeID, identity.Source, identity.AccountScope, identity.Symbol, strconv.FormatInt(row.TransactionID, 10)}, "\x00")
		eventIDHash := sha256.Sum256([]byte(eventIdentity))
		events = append(events, accounting.RecoveryEvent{
			ID: "funding:" + hex.EncodeToString(eventIDHash[:]), Source: identity.Source, Exchange: identity.Exchange,
			Market: identity.Market, AccountScope: identity.AccountScope, Symbol: identity.Symbol,
			Kind: accounting.RecoveryFundingEvent, ExternalID: strconv.FormatInt(row.TransactionID, 10), EpisodeID: episodeID,
			Asset: identity.Asset, Amount: row.RawIncome, SourceRevision: sourceRevision, SourceHash: rowBytes,
			ObservedAt: row.TradeTime.UTC(),
		})
	}
	return events, nil
}

func verifyFundingRecoveryIncomeHistory(episodeID string, identity accounting.RecoverySourceIdentity, initialFrom, through time.Time, rows []*income.Income, events []accounting.RecoveryEvent) error {
	facts, err := diffFundingRecoveryIncomeHistory(episodeID, identity, initialFrom, through, rows, events)
	if err != nil {
		return err
	}
	if len(facts) > 0 {
		return fmt.Errorf("durable funding history still differs from complete Binance evidence in %d source rows", len(facts))
	}
	return nil
}

func diffFundingRecoveryIncomeHistory(episodeID string, identity accounting.RecoverySourceIdentity, initialFrom, through time.Time,
	rows []*income.Income, events []accounting.RecoveryEvent) ([]accounting.RecoverySourceAnomalyFact, error) {
	observedEvents, err := makeFundingRecoveryIncomeEvents(episodeID, identity, "historical-verification", rows)
	if err != nil {
		return nil, err
	}
	effective, err := effectiveFundingRecoveryHistoryEvents(episodeID, identity, initialFrom, through, events)
	if err != nil {
		return nil, err
	}
	observed := make(map[string]accounting.RecoveryEvent, len(observedEvents))
	for _, event := range observedEvents {
		if _, exists := observed[event.ExternalID]; exists {
			return nil, errors.New("complete source history contains duplicate funding transaction identities")
		}
		observed[event.ExternalID] = event
	}
	facts := make([]accounting.RecoverySourceAnomalyFact, 0)
	for externalID, venueEvent := range observed {
		prior, found := effective[externalID]
		if !found {
			delta, deltaErr := accounting.RecoveryFundingAmountDelta(nil, &venueEvent)
			if deltaErr != nil {
				return nil, deltaErr
			}
			facts = append(facts, accounting.RecoverySourceAnomalyFact{Kind: accounting.RecoverySourceAnomalyLate, ExternalID: externalID,
				ObservedEvent: &venueEvent, DeltaAmount: delta})
			continue
		}
		if prior.Amount == venueEvent.Amount && prior.SourceHash == venueEvent.SourceHash && prior.ObservedAt.Equal(venueEvent.ObservedAt) {
			delete(effective, externalID)
			continue
		}
		delta, deltaErr := accounting.RecoveryFundingAmountDelta(&prior, &venueEvent)
		if deltaErr != nil {
			return nil, deltaErr
		}
		kind := accounting.RecoverySourceAnomalyMetadata
		if prior.Amount != venueEvent.Amount {
			kind = accounting.RecoverySourceAnomalyChanged
		}
		facts = append(facts, accounting.RecoverySourceAnomalyFact{Kind: kind, ExternalID: externalID,
			OriginalEvent: &prior, ObservedEvent: &venueEvent, DeltaAmount: delta})
		delete(effective, externalID)
	}
	for externalID, prior := range effective {
		delta, deltaErr := accounting.RecoveryFundingAmountDelta(&prior, nil)
		if deltaErr != nil {
			return nil, deltaErr
		}
		facts = append(facts, accounting.RecoverySourceAnomalyFact{Kind: accounting.RecoverySourceAnomalyDeleted, ExternalID: externalID,
			OriginalEvent: &prior, DeltaAmount: delta})
	}
	sort.Slice(facts, func(i, j int) bool { return facts[i].ExternalID < facts[j].ExternalID })
	return facts, nil
}

func effectiveFundingRecoveryHistoryEvents(episodeID string, identity accounting.RecoverySourceIdentity, initialFrom, through time.Time,
	events []accounting.RecoveryEvent) (map[string]accounting.RecoveryEvent, error) {
	effective := make(map[string]accounting.RecoveryEvent)
	adjustments := make([]accounting.RecoveryEvent, 0)
	for _, event := range events {
		if event.EpisodeID != episodeID || event.Exchange != identity.Exchange || event.Market != identity.Market || event.AccountScope != identity.AccountScope ||
			event.Symbol != identity.Symbol || event.Asset != identity.Asset || event.Kind != accounting.RecoveryFundingEvent {
			continue
		}
		if event.Source == accounting.RecoveryManualFundingAdjustmentSource {
			adjustments = append(adjustments, event)
			continue
		}
		if event.Source != identity.Source || event.ObservedAt.Before(initialFrom) || !event.ObservedAt.Before(through) {
			continue
		}
		if _, exists := effective[event.ExternalID]; exists {
			return nil, errors.New("durable funding ledger contains duplicate original source identities")
		}
		effective[event.ExternalID] = event
	}
	sort.Slice(adjustments, func(i, j int) bool {
		left, right := adjustments[i].FundingAdjustment, adjustments[j].FundingAdjustment
		if left != nil && right != nil && !left.ConfirmedAt.Equal(right.ConfirmedAt) {
			return left.ConfirmedAt.Before(right.ConfirmedAt)
		}
		return adjustments[i].ID < adjustments[j].ID
	})
	for _, adjustment := range adjustments {
		evidence := adjustment.FundingAdjustment
		if evidence == nil || evidence.Validate() != nil || evidence.AnomalyID == "" || adjustment.RelatedExternalID != evidence.OriginalExternalID {
			return nil, errors.New("durable manual funding adjustment is incomplete or detached from its source fact")
		}
		current, found := effective[evidence.OriginalExternalID]
		if evidence.OriginalAmountRaw == "" {
			if found || evidence.ObservedAmountRaw == "" {
				return nil, accounting.ErrRecoveryOperationConflict
			}
			current = accounting.RecoveryEvent{ID: adjustment.ID, Source: identity.Source, Exchange: identity.Exchange, Market: identity.Market,
				AccountScope: identity.AccountScope, Symbol: identity.Symbol, Kind: accounting.RecoveryFundingEvent, ExternalID: evidence.OriginalExternalID,
				EpisodeID: episodeID, Asset: identity.Asset, Amount: evidence.ObservedAmountRaw, SourceRevision: "manual-adjustment-observation",
				SourceHash: evidence.ObservedSourceHash, ObservedAt: evidence.ObservedRowAt}
			effective[evidence.OriginalExternalID] = current
			continue
		}
		if !found || current.Amount != evidence.OriginalAmountRaw || current.SourceHash != evidence.OriginalSourceHash {
			return nil, accounting.ErrRecoveryOperationConflict
		}
		if evidence.ObservedAmountRaw == "" {
			delete(effective, evidence.OriginalExternalID)
			continue
		}
		current.Amount, current.SourceHash, current.ObservedAt = evidence.ObservedAmountRaw, evidence.ObservedSourceHash, evidence.ObservedRowAt
		effective[evidence.OriginalExternalID] = current
	}
	return effective, nil
}

func (s *FundingCarryStrategy) captureFundingRecoveryIncomeAnomaly(ctx context.Context, binding fundingCarryRecoveryBinding,
	episode accounting.RecoveryEpisode, identity accounting.RecoverySourceIdentity, cursor accounting.RecoverySourceCursor,
	continuation exchange.BinanceFundingIncomeContinuation, facts []accounting.RecoverySourceAnomalyFact, request ApplyFundingRecoveryIncomeRequest) (accounting.RecoverySourceAnomaly, error) {
	anomaly := accounting.RecoverySourceAnomaly{EpisodeID: episode.ID, RecoveryGeneration: episode.RecoveryGeneration, EpisodeRevision: episode.Revision,
		Identity: identity, CursorRevision: cursor.Revision, InitialFrom: cursor.InitialFrom, Through: cursor.Through,
		ExpectedHistoryHash: cursor.HistoricalEvidenceHash, ObservedHistoryHash: continuation.PriorEvidenceHash, Facts: facts,
		Revision: 1, State: accounting.RecoverySourceAnomalyOpen, DetectedActor: request.Actor, DetectedAuditRef: request.AuditRef,
		DetectedAt: continuation.Window.ReadAt.UTC()}
	if len(facts) == 0 {
		anomaly.UnclassifiedReason = "complete source history digest changed but no row-level difference could be materialized"
	}
	fingerprint, err := anomaly.Fingerprint()
	if err != nil {
		return accounting.RecoverySourceAnomaly{}, err
	}
	anomaly.ID = fingerprint
	saved, _, err := binding.store.RecordRecoverySourceAnomaly(ctx, binding.generation, anomaly)
	if err != nil {
		return accounting.RecoverySourceAnomaly{}, fmt.Errorf("persist detected funding source anomaly while retaining durable hold: %w", err)
	}
	return saved, nil
}

func validRecoveryEvidenceDigest(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && strings.ToLower(value) == value
}
