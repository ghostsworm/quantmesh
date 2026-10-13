package web

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"quantmesh/accounting"
	"quantmesh/storage"
	"quantmesh/strategy"
)

type FundingRecoveryStatus struct {
	Episode   accounting.RecoveryEpisode             `json:"episode"`
	Children  []accounting.RecoveryChild             `json:"children"`
	Events    []accounting.RecoveryEvent             `json:"events"`
	Anomalies []accounting.RecoverySourceAnomaly     `json:"source_anomalies"`
	Attempts  []accounting.RecoverySubmissionAttempt `json:"submission_attempts"`
	Hold      accounting.RecoveryHold                `json:"hold"`
	Outbox    []accounting.RecoveryOutbox            `json:"pending_outbox"`
}

type FundingRecoveryFillApplyReadback struct {
	Result   strategy.ApplyFundingRecoveryFillsResult `json:"result"`
	Receipt  accounting.RecoveryReceipt               `json:"receipt"`
	Outbox   accounting.RecoveryOutbox                `json:"outbox"`
	Episode  accounting.RecoveryEpisode               `json:"episode"`
	Hold     accounting.RecoveryHold                  `json:"hold"`
	Verified bool                                     `json:"verified"`
}

type FundingRecoveryFeeAdjustmentReadback struct {
	Result   strategy.ApplyFundingRecoveryFeeAdjustmentResult `json:"result"`
	Receipt  accounting.RecoveryReceipt                       `json:"receipt"`
	Outbox   accounting.RecoveryOutbox                        `json:"outbox"`
	Episode  accounting.RecoveryEpisode                       `json:"episode"`
	Hold     accounting.RecoveryHold                          `json:"hold"`
	FeeEvent accounting.RecoveryEvent                         `json:"fee_event"`
	Verified bool                                             `json:"verified"`
}

type FundingRecoveryOpeningBalanceReadback struct {
	Result                      strategy.ApplyFundingRecoveryOpeningBalanceResult `json:"result"`
	Receipt                     accounting.RecoveryReceipt                        `json:"receipt"`
	Outbox                      accounting.RecoveryOutbox                         `json:"outbox"`
	Episode                     accounting.RecoveryEpisode                        `json:"episode"`
	Hold                        accounting.RecoveryHold                           `json:"hold"`
	Events                      []accounting.RecoveryEvent                        `json:"events"`
	DurableReadbackVerified     bool                                              `json:"durable_readback_verified"`
	OperatorAttestedOnly        bool                                              `json:"operator_attested_only"`
	AuthoritativeSourceVerified bool                                              `json:"authoritative_source_verified"`
}

type FundingRecoveryIncomeReadback struct {
	Result   strategy.ApplyFundingRecoveryIncomeResult `json:"result"`
	Receipt  accounting.RecoveryReceipt                `json:"receipt"`
	Outbox   accounting.RecoveryOutbox                 `json:"outbox"`
	Cursor   accounting.RecoverySourceCursor           `json:"cursor"`
	Episode  accounting.RecoveryEpisode                `json:"episode"`
	Hold     accounting.RecoveryHold                   `json:"hold"`
	Verified bool                                      `json:"verified"`
}

type FundingRecoveryAnomalyAdjustmentReadback struct {
	Result   FundingRecoveryAnomalyAdjustmentResult `json:"result"`
	Receipt  accounting.RecoveryReceipt             `json:"receipt"`
	Outbox   accounting.RecoveryOutbox              `json:"outbox"`
	Episode  accounting.RecoveryEpisode             `json:"episode"`
	Hold     accounting.RecoveryHold                `json:"hold"`
	Anomaly  accounting.RecoverySourceAnomaly       `json:"anomaly"`
	Verified bool                                   `json:"verified"`
}

type FundingRecoveryAnomalyAdjustmentResult = strategy.ApplyFundingRecoveryAnomalyAdjustmentResult

// ResolveFundingRecoveryRuntime resolves the currently retained, held runtime
// for a recovery episode. It fails closed when the production runtime binding
// is unavailable and never claims a new generation.
func (s *OrderReconciliationService) ResolveFundingRecoveryRuntime(ctx context.Context, episode accounting.RecoveryEpisode) (storage.FundingCarryRuntimeGeneration, error) {
	if s == nil || s.RecoveryRuntimeResolver == nil || ctx == nil {
		return storage.FundingCarryRuntimeGeneration{}, storage.ErrFundingRecoveryUnavailable
	}
	return s.RecoveryRuntimeResolver(ctx, episode)
}

// ReadFundingRecoveryStatus is a read-only service API. It never treats client
// supplied assertions as accounting evidence and cannot release a hold.
func (s *OrderReconciliationService) ReadFundingRecoveryStatus(ctx context.Context, episodeID string) (FundingRecoveryStatus, error) {
	if s == nil || s.Recovery == nil || strings.TrimSpace(episodeID) == "" {
		return FundingRecoveryStatus{}, storage.ErrFundingRecoveryUnavailable
	}
	store := s.Recovery
	episode, found, err := store.GetRecoveryEpisode(ctx, episodeID)
	if err != nil {
		return FundingRecoveryStatus{}, err
	}
	if !found {
		return FundingRecoveryStatus{}, storage.ErrFundingRecoveryNotFound
	}
	children, err := store.ListRecoveryChildren(ctx, episodeID)
	if err != nil {
		return FundingRecoveryStatus{}, err
	}
	events, err := store.ListRecoveryEvents(ctx, episodeID)
	if err != nil {
		return FundingRecoveryStatus{}, err
	}
	attempts, err := store.ListRecoverySubmissionAttempts(ctx, episodeID)
	if err != nil {
		return FundingRecoveryStatus{}, err
	}
	hold, found, err := store.GetRecoveryHold(ctx, episodeID, episode.RecoveryGeneration)
	if err != nil {
		return FundingRecoveryStatus{}, err
	}
	if !found {
		return FundingRecoveryStatus{}, storage.ErrFundingRecoveryNotFound
	}
	outboxes, err := store.ListPendingRecoveryOutbox(ctx)
	if err != nil {
		return FundingRecoveryStatus{}, err
	}
	anomalies, err := store.ListRecoverySourceAnomalies(ctx, episodeID)
	if err != nil {
		return FundingRecoveryStatus{}, err
	}
	pending := make([]accounting.RecoveryOutbox, 0)
	for _, item := range outboxes {
		if item.EpisodeID == episodeID {
			pending = append(pending, item)
		}
	}
	return FundingRecoveryStatus{Episode: episode, Children: children, Events: events, Anomalies: anomalies, Attempts: attempts, Hold: hold, Outbox: pending}, nil
}

func (s *OrderReconciliationService) PreviewFundingRecoveryFill(ctx context.Context, episodeID, childID string) (strategy.FundingRecoveryFillPreview, error) {
	if s == nil || s.Recovery == nil || s.RecoveryFillPreview == nil || ctx == nil || strings.TrimSpace(episodeID) == "" || strings.TrimSpace(childID) == "" {
		return strategy.FundingRecoveryFillPreview{}, storage.ErrFundingRecoveryUnavailable
	}
	episode, found, err := s.Recovery.GetRecoveryEpisode(ctx, episodeID)
	if err != nil {
		return strategy.FundingRecoveryFillPreview{}, err
	}
	if !found {
		return strategy.FundingRecoveryFillPreview{}, storage.ErrFundingRecoveryNotFound
	}
	if episode.Strategy != "funding_carry" {
		return strategy.FundingRecoveryFillPreview{}, accounting.ErrRecoveryConflict
	}
	return s.RecoveryFillPreview(ctx, episodeID, childID)
}

func (s *OrderReconciliationService) ApplyFundingRecoveryFill(ctx context.Context, request strategy.ApplyFundingRecoveryFillsRequest) (FundingRecoveryFillApplyReadback, error) {
	if s == nil || s.Recovery == nil || s.RecoveryFillApply == nil || ctx == nil || strings.TrimSpace(request.EpisodeID) == "" || strings.TrimSpace(request.ChildID) == "" {
		return FundingRecoveryFillApplyReadback{}, storage.ErrFundingRecoveryUnavailable
	}
	episode, found, err := s.Recovery.GetRecoveryEpisode(ctx, request.EpisodeID)
	if err != nil {
		return FundingRecoveryFillApplyReadback{}, err
	}
	if !found {
		return FundingRecoveryFillApplyReadback{}, storage.ErrFundingRecoveryNotFound
	}
	if episode.Strategy != "funding_carry" {
		return FundingRecoveryFillApplyReadback{}, accounting.ErrRecoveryConflict
	}
	result, err := s.RecoveryFillApply(ctx, request)
	if err != nil {
		return FundingRecoveryFillApplyReadback{}, err
	}
	receipt, receiptFound, err := s.Recovery.GetRecoveryReceipt(ctx, request.EpisodeID, request.OperationID)
	if err != nil {
		return FundingRecoveryFillApplyReadback{}, err
	}
	if !receiptFound {
		return FundingRecoveryFillApplyReadback{}, errors.New("FundingCarry fill operation returned without a durable receipt")
	}
	outbox, outboxFound, err := s.Recovery.GetRecoveryOutbox(ctx, request.EpisodeID, request.OperationID)
	if err != nil {
		return FundingRecoveryFillApplyReadback{}, err
	}
	if !outboxFound {
		return FundingRecoveryFillApplyReadback{}, errors.New("FundingCarry fill receipt has no durable outbox")
	}
	status, err := s.ReadFundingRecoveryStatus(ctx, request.EpisodeID)
	if err != nil {
		return FundingRecoveryFillApplyReadback{}, err
	}
	verified := result.Projected && receipt.OperationID == request.OperationID && outbox.OperationID == request.OperationID &&
		outbox.State == accounting.RecoveryProjected && status.Hold.State == "held" &&
		(status.Episode.State == accounting.RecoveryBlocked || status.Episode.State == accounting.RecoveryProjected)
	if !verified {
		return FundingRecoveryFillApplyReadback{}, errors.New("FundingCarry fill operation readback is incomplete; hold remains active")
	}
	return FundingRecoveryFillApplyReadback{Result: result, Receipt: receipt, Outbox: outbox, Episode: status.Episode, Hold: status.Hold, Verified: true}, nil
}

func (s *OrderReconciliationService) PreviewFundingRecoveryFeeAdjustment(ctx context.Context, episodeID, childID, tradeID string) (strategy.FundingRecoveryFeeAdjustmentPreview, error) {
	if s == nil || s.Recovery == nil || s.RecoveryFeePreview == nil || ctx == nil || strings.TrimSpace(episodeID) == "" ||
		strings.TrimSpace(childID) == "" || strings.TrimSpace(tradeID) == "" {
		return strategy.FundingRecoveryFeeAdjustmentPreview{}, storage.ErrFundingRecoveryUnavailable
	}
	episode, found, err := s.Recovery.GetRecoveryEpisode(ctx, episodeID)
	if err != nil {
		return strategy.FundingRecoveryFeeAdjustmentPreview{}, err
	}
	if !found {
		return strategy.FundingRecoveryFeeAdjustmentPreview{}, storage.ErrFundingRecoveryNotFound
	}
	if episode.Strategy != "funding_carry" {
		return strategy.FundingRecoveryFeeAdjustmentPreview{}, accounting.ErrRecoveryConflict
	}
	return s.RecoveryFeePreview(ctx, episodeID, childID, tradeID)
}

func (s *OrderReconciliationService) ApplyFundingRecoveryFeeAdjustment(ctx context.Context, request strategy.ApplyFundingRecoveryFeeAdjustmentRequest) (FundingRecoveryFeeAdjustmentReadback, error) {
	if s == nil || s.Recovery == nil || s.RecoveryFeeApply == nil || ctx == nil || strings.TrimSpace(request.EpisodeID) == "" ||
		strings.TrimSpace(request.ChildID) == "" {
		return FundingRecoveryFeeAdjustmentReadback{}, storage.ErrFundingRecoveryUnavailable
	}
	episode, found, err := s.Recovery.GetRecoveryEpisode(ctx, request.EpisodeID)
	if err != nil {
		return FundingRecoveryFeeAdjustmentReadback{}, err
	}
	if !found {
		return FundingRecoveryFeeAdjustmentReadback{}, storage.ErrFundingRecoveryNotFound
	}
	if episode.Strategy != "funding_carry" {
		return FundingRecoveryFeeAdjustmentReadback{}, accounting.ErrRecoveryConflict
	}
	result, err := s.RecoveryFeeApply(ctx, request)
	if err != nil {
		return FundingRecoveryFeeAdjustmentReadback{}, err
	}
	receipt, receiptFound, err := s.Recovery.GetRecoveryReceipt(ctx, request.EpisodeID, request.OperationID)
	if err != nil {
		return FundingRecoveryFeeAdjustmentReadback{}, err
	}
	if !receiptFound {
		return FundingRecoveryFeeAdjustmentReadback{}, errors.New("FundingCarry fee adjustment returned without a durable receipt")
	}
	outbox, outboxFound, err := s.Recovery.GetRecoveryOutbox(ctx, request.EpisodeID, request.OperationID)
	if err != nil {
		return FundingRecoveryFeeAdjustmentReadback{}, err
	}
	if !outboxFound {
		return FundingRecoveryFeeAdjustmentReadback{}, errors.New("FundingCarry fee adjustment receipt has no durable outbox")
	}
	status, err := s.ReadFundingRecoveryStatus(ctx, request.EpisodeID)
	if err != nil {
		return FundingRecoveryFeeAdjustmentReadback{}, err
	}
	var feeEvent accounting.RecoveryEvent
	feeEventCount := 0
	for _, event := range status.Events {
		if event.Kind == accounting.RecoveryFeeEvent && event.ChildID == request.ChildID && event.ExternalID == request.AdjustmentID {
			feeEvent = event
			feeEventCount++
		}
	}
	verified := result.Projected && receipt.OperationID == request.OperationID && outbox.OperationID == request.OperationID &&
		receipt.EpisodeID == request.EpisodeID && receipt.Actor == request.Actor && receipt.AuditRef == request.AuditRef &&
		receipt.EvidenceHash == request.EvidenceHash && outbox.EpisodeID == request.EpisodeID &&
		outbox.State == accounting.RecoveryProjected && status.Hold.State == "held" &&
		(status.Episode.State == accounting.RecoveryBlocked || status.Episode.State == accounting.RecoveryProjected) &&
		feeEventCount == 1 && feeEvent.EpisodeID == request.EpisodeID && feeEvent.ChildID == request.ChildID &&
		feeEvent.ExternalID == request.AdjustmentID && feeEvent.RelatedExternalID == request.TradeID &&
		feeEvent.SourceHash == request.EvidenceHash && feeEvent.Amount == request.AmountDeltaRaw &&
		feeEvent.Asset == strings.ToUpper(strings.TrimSpace(request.Asset))
	if !verified {
		return FundingRecoveryFeeAdjustmentReadback{}, errors.New("FundingCarry fee adjustment readback is incomplete or hold changed")
	}
	return FundingRecoveryFeeAdjustmentReadback{Result: result, Receipt: receipt, Outbox: outbox, Episode: status.Episode,
		Hold: status.Hold, FeeEvent: feeEvent, Verified: true}, nil
}

func (s *OrderReconciliationService) ApplyFundingRecoveryOpeningBalance(ctx context.Context, request strategy.ApplyFundingRecoveryOpeningBalanceRequest) (FundingRecoveryOpeningBalanceReadback, error) {
	if s == nil || s.Recovery == nil || s.RecoveryOpeningBalanceApply == nil || ctx == nil ||
		strings.TrimSpace(request.EpisodeID) == "" || strings.TrimSpace(request.OperationID) == "" || !request.Confirmed {
		return FundingRecoveryOpeningBalanceReadback{}, storage.ErrFundingRecoveryUnavailable
	}
	snapshotHash, err := request.Snapshot.CanonicalHash()
	if err != nil {
		return FundingRecoveryOpeningBalanceReadback{}, err
	}
	if request.EvidenceHash != snapshotHash {
		return FundingRecoveryOpeningBalanceReadback{}, errors.New("opening balance snapshot digest mismatch")
	}
	episode, found, err := s.Recovery.GetRecoveryEpisode(ctx, request.EpisodeID)
	if err != nil {
		return FundingRecoveryOpeningBalanceReadback{}, err
	}
	if !found {
		return FundingRecoveryOpeningBalanceReadback{}, storage.ErrFundingRecoveryNotFound
	}
	if episode.Strategy != "funding_carry" {
		return FundingRecoveryOpeningBalanceReadback{}, accounting.ErrRecoveryConflict
	}
	scopeFound := false
	for _, scope := range episode.AccountScopes {
		if scope == request.Snapshot.AccountScope {
			scopeFound = true
			break
		}
	}
	if !scopeFound {
		return FundingRecoveryOpeningBalanceReadback{}, storage.ErrFundingRecoveryGenerationScopeMismatch
	}
	result, err := s.RecoveryOpeningBalanceApply(ctx, request)
	if err != nil {
		return FundingRecoveryOpeningBalanceReadback{}, err
	}
	receipt, receiptFound, err := s.Recovery.GetRecoveryReceipt(ctx, request.EpisodeID, request.OperationID)
	if err != nil {
		return FundingRecoveryOpeningBalanceReadback{}, err
	}
	if !receiptFound {
		return FundingRecoveryOpeningBalanceReadback{}, errors.New("opening balance operation returned without a durable receipt")
	}
	outbox, outboxFound, err := s.Recovery.GetRecoveryOutbox(ctx, request.EpisodeID, request.OperationID)
	if err != nil {
		return FundingRecoveryOpeningBalanceReadback{}, err
	}
	if !outboxFound {
		return FundingRecoveryOpeningBalanceReadback{}, errors.New("opening balance receipt has no durable outbox")
	}
	status, err := s.ReadFundingRecoveryStatus(ctx, request.EpisodeID)
	if err != nil {
		return FundingRecoveryOpeningBalanceReadback{}, err
	}
	storedByID := make(map[string]accounting.RecoveryEvent, len(status.Events))
	for _, event := range status.Events {
		storedByID[event.ID] = event
	}
	eventsMatch := len(result.Events) == len(request.Snapshot.Assets)
	for _, event := range result.Events {
		stored, exists := storedByID[event.ID]
		if !exists || !reflect.DeepEqual(event, stored) || event.Kind != accounting.RecoveryOpeningBalanceEvent || event.SourceHash != snapshotHash {
			eventsMatch = false
			break
		}
	}
	verified := result.Projected && eventsMatch && receipt.OperationID == request.OperationID && receipt.EpisodeID == request.EpisodeID &&
		receipt.Actor == request.Actor && receipt.AuditRef == request.AuditRef && receipt.EvidenceHash == snapshotHash &&
		outbox.OperationID == request.OperationID && outbox.EpisodeID == request.EpisodeID && outbox.State == accounting.RecoveryProjected &&
		status.Hold.State == "held" && (status.Episode.State == accounting.RecoveryBlocked || status.Episode.State == accounting.RecoveryProjected)
	if !verified {
		return FundingRecoveryOpeningBalanceReadback{}, errors.New("opening balance durable readback is incomplete; hold remains active")
	}
	return FundingRecoveryOpeningBalanceReadback{Result: result, Receipt: receipt, Outbox: outbox, Episode: status.Episode,
		Hold: status.Hold, Events: result.Events, DurableReadbackVerified: true, OperatorAttestedOnly: true,
		AuthoritativeSourceVerified: false}, nil
}

func (s *OrderReconciliationService) ApplyFundingRecoveryIncome(ctx context.Context, request strategy.ApplyFundingRecoveryIncomeRequest) (FundingRecoveryIncomeReadback, error) {
	if s == nil || s.Recovery == nil || s.RecoveryFundingIncomeApply == nil || ctx == nil ||
		strings.TrimSpace(request.EpisodeID) == "" || strings.TrimSpace(request.OperationID) == "" || !request.Confirmed {
		return FundingRecoveryIncomeReadback{}, storage.ErrFundingRecoveryUnavailable
	}
	result, err := s.RecoveryFundingIncomeApply(ctx, request)
	if err != nil {
		return FundingRecoveryIncomeReadback{}, err
	}
	receipt, receiptFound, err := s.Recovery.GetRecoveryReceipt(ctx, request.EpisodeID, request.OperationID)
	if err != nil || !receiptFound {
		return FundingRecoveryIncomeReadback{}, errors.Join(errors.New("funding-income durable receipt readback failed"), err)
	}
	outbox, outboxFound, err := s.Recovery.GetRecoveryOutbox(ctx, request.EpisodeID, request.OperationID)
	if err != nil || !outboxFound || outbox.State != accounting.RecoveryProjected || outbox.AppliedOperationID != request.OperationID ||
		outbox.AppliedRevision != outbox.TargetEpisodeRevision || outbox.AppliedHash != outbox.ProjectionHash {
		return FundingRecoveryIncomeReadback{}, errors.Join(errors.New("funding-income projected outbox readback failed"), err)
	}
	status, err := s.ReadFundingRecoveryStatus(ctx, request.EpisodeID)
	if err != nil {
		return FundingRecoveryIncomeReadback{}, err
	}
	var cursor accounting.RecoverySourceCursor
	if result.Replayed {
		var takeoverReceipt accounting.RecoveryReceipt
		if receipt.EpisodeRevision > outbox.TargetEpisodeRevision && receipt.EpisodeRevision-outbox.TargetEpisodeRevision == 2 {
			if len(status.Episode.OwnerTakeovers) == 0 {
				return FundingRecoveryIncomeReadback{}, errors.New("funding-income replay has no matching takeover receipt")
			}
			takeover := status.Episode.OwnerTakeovers[len(status.Episode.OwnerTakeovers)-1]
			var takeoverFound bool
			takeoverReceipt, takeoverFound, err = s.Recovery.GetRecoveryReceipt(ctx, status.Episode.ID, takeover.OperationID)
			if err != nil || !takeoverFound {
				return FundingRecoveryIncomeReadback{}, errors.Join(errors.New("funding-income replay takeover receipt readback failed"), err)
			}
		}
		cursor, err = fundingIncomeReplayCursorSnapshot(outbox, receipt, takeoverReceipt, request, result, status.Episode)
		if err != nil {
			return FundingRecoveryIncomeReadback{}, err
		}
	} else {
		currentCursor, cursorFound, cursorErr := s.Recovery.GetRecoverySourceCursor(ctx, result.SourceWindow.Identity())
		if cursorErr != nil || !cursorFound || currentCursor.EpisodeID != request.EpisodeID {
			return FundingRecoveryIncomeReadback{}, errors.Join(errors.New("funding-income current source cursor readback failed"), cursorErr)
		}
		if currentCursor.Revision != result.SourceWindow.ExpectedRevision+1 ||
			currentCursor.EvidenceHash != result.SourceWindow.EvidenceHash || !currentCursor.InitialFrom.Equal(result.SourceWindow.InitialFrom) ||
			currentCursor.HistoricalEvidenceHash != result.SourceWindow.HistoricalEvidenceHash || !currentCursor.From.Equal(result.SourceWindow.From) || !currentCursor.Through.Equal(result.SourceWindow.Through) {
			return FundingRecoveryIncomeReadback{}, errors.New("funding-income source cursor readback failed")
		}
		cursor = currentCursor
	}
	stored := make(map[string]accounting.RecoveryEvent, len(status.Events))
	for _, event := range status.Events {
		stored[event.ID] = event
	}
	eventsMatch := uint64(len(result.Events)) == result.SourceWindow.Rows
	for _, event := range result.Events {
		if saved, found := stored[event.ID]; !found || !reflect.DeepEqual(saved, event) || event.Kind != accounting.RecoveryFundingEvent || event.Source != result.SourceWindow.Source {
			eventsMatch = false
			break
		}
	}
	verified := result.Projected && result.HoldRetained && receipt.EpisodeID == request.EpisodeID && receipt.OperationID == request.OperationID &&
		receipt.Actor != "" && receipt.AuditRef != "" && receipt.EvidenceHash != "" && eventsMatch && status.Hold.State == "held" &&
		(status.Episode.State == accounting.RecoveryBlocked || status.Episode.State == accounting.RecoveryProjected)
	if !verified {
		return FundingRecoveryIncomeReadback{}, errors.New("funding-income exact facts/cursor/runtime projection readback incomplete; hold remains active")
	}
	return FundingRecoveryIncomeReadback{Result: result, Receipt: receipt, Outbox: outbox, Cursor: cursor, Episode: status.Episode, Hold: status.Hold, Verified: true}, nil
}

func (s *OrderReconciliationService) ApplyFundingRecoveryAnomalyAdjustment(ctx context.Context, request strategy.ApplyFundingRecoveryAnomalyAdjustmentRequest) (FundingRecoveryAnomalyAdjustmentReadback, error) {
	if s == nil || s.Recovery == nil || s.RecoveryAnomalyAdjustmentApply == nil || ctx == nil || strings.TrimSpace(request.EpisodeID) == "" || !request.Confirmed {
		return FundingRecoveryAnomalyAdjustmentReadback{}, storage.ErrFundingRecoveryUnavailable
	}
	episode, found, err := s.Recovery.GetRecoveryEpisode(ctx, request.EpisodeID)
	if err != nil || !found {
		return FundingRecoveryAnomalyAdjustmentReadback{}, errors.Join(storage.ErrFundingRecoveryNotFound, err)
	}
	if episode.Strategy != "funding_carry" {
		return FundingRecoveryAnomalyAdjustmentReadback{}, accounting.ErrRecoveryConflict
	}
	result, err := s.RecoveryAnomalyAdjustmentApply(ctx, request)
	if err != nil {
		return FundingRecoveryAnomalyAdjustmentReadback{}, err
	}
	receipt, receiptFound, err := s.Recovery.GetRecoveryReceipt(ctx, request.EpisodeID, request.OperationID)
	if err != nil || !receiptFound {
		return FundingRecoveryAnomalyAdjustmentReadback{}, errors.Join(errors.New("funding anomaly adjustment receipt readback failed"), err)
	}
	outbox, outboxFound, err := s.Recovery.GetRecoveryOutbox(ctx, request.EpisodeID, request.OperationID)
	if err != nil || !outboxFound || outbox.State != accounting.RecoveryProjected || outbox.AppliedHash != outbox.ProjectionHash {
		return FundingRecoveryAnomalyAdjustmentReadback{}, errors.Join(errors.New("funding anomaly adjustment projection readback failed"), err)
	}
	status, err := s.ReadFundingRecoveryStatus(ctx, request.EpisodeID)
	if err != nil {
		return FundingRecoveryAnomalyAdjustmentReadback{}, err
	}
	var anomaly accounting.RecoverySourceAnomaly
	for _, candidate := range status.Anomalies {
		if candidate.ID == request.AnomalyID {
			anomaly = candidate
			break
		}
	}
	eventsMatch := len(result.Events) == len(anomaly.Facts)
	for _, event := range result.Events {
		matched := false
		for _, stored := range status.Events {
			if stored.ID == event.ID && reflect.DeepEqual(stored, event) {
				matched = true
				break
			}
		}
		eventsMatch = eventsMatch && matched
	}
	verified := result.Projected && result.HoldRetained && anomaly.State == accounting.RecoverySourceAnomalyAdjustmentApplied &&
		anomaly.AdjustmentOperationID == request.OperationID && receipt.Actor == request.Actor && receipt.AuditRef == request.AuditRef &&
		receipt.EvidenceHash == request.EvidenceHash && receipt.OperationID == request.OperationID && outbox.OperationID == request.OperationID &&
		status.Hold.State == "held" && eventsMatch
	if !verified {
		return FundingRecoveryAnomalyAdjustmentReadback{}, errors.New("funding anomaly adjustment durable readback incomplete; hold remains active")
	}
	return FundingRecoveryAnomalyAdjustmentReadback{Result: result, Receipt: receipt, Outbox: outbox, Episode: status.Episode, Hold: status.Hold,
		Anomaly: anomaly, Verified: true}, nil
}

func fundingIncomeReplayCursorSnapshot(outbox accounting.RecoveryOutbox, receipt, takeoverReceipt accounting.RecoveryReceipt,
	request strategy.ApplyFundingRecoveryIncomeRequest, result strategy.ApplyFundingRecoveryIncomeResult,
	currentEpisode accounting.RecoveryEpisode) (accounting.RecoverySourceCursor, error) {
	if request.ExpectedEpisodeRevision >= accounting.MaxDurableRecoveryRevision-1 || result.SourceWindow.ExpectedRevision == ^uint64(0) ||
		result.SourceWindow.Validate() != nil || outbox.OperationID != request.OperationID || outbox.EpisodeID != request.EpisodeID ||
		outbox.TargetEpisodeRevision != request.ExpectedEpisodeRevision+1 || receipt.State != accounting.RecoveryProjected {
		return accounting.RecoverySourceCursor{}, errors.New("funding-income replay receipt/outbox revision does not match the original request")
	}
	if storage.VerifyFundingRecoveryProjectionHash(outbox.ProjectionPayload, outbox.ProjectionHash) != nil || outbox.State != accounting.RecoveryProjected {
		return accounting.RecoverySourceCursor{}, errors.New("funding-income replay outbox payload hash does not match its receipt")
	}
	var snapshot struct {
		Episode      accounting.RecoveryEpisode       `json:"episode"`
		Events       []accounting.RecoveryEvent       `json:"events"`
		SourceCursor *accounting.RecoverySourceCursor `json:"source_cursor"`
	}
	if err := json.Unmarshal(outbox.ProjectionPayload, &snapshot); err != nil {
		return accounting.RecoverySourceCursor{}, err
	}
	if snapshot.SourceCursor == nil || snapshot.Episode.ID != request.EpisodeID || snapshot.Episode.Revision != outbox.TargetEpisodeRevision ||
		receipt.OwnerGeneration != snapshot.Episode.OwnerGeneration || !fundingIncomeReplayReceiptRevisionMatches(receipt, outbox, snapshot.Episode, currentEpisode) ||
		!reflect.DeepEqual(snapshot.Events, result.Events) || snapshot.SourceCursor.EpisodeID != request.EpisodeID ||
		!reflect.DeepEqual(snapshot.SourceCursor.RecoverySourceWindow, result.SourceWindow) ||
		snapshot.SourceCursor.Revision != result.SourceWindow.ExpectedRevision+1 || snapshot.SourceCursor.Identity().Validate() != nil ||
		snapshot.SourceCursor.RecoverySourceWindow.Validate() != nil {
		return accounting.RecoverySourceCursor{}, errors.New("funding-income replay outbox snapshot does not match its receipt and source facts")
	}
	var pairErr error
	if receipt.EpisodeRevision-outbox.TargetEpisodeRevision == 2 {
		pairErr = storage.VerifyFundingRecoveryReceiptOutboxPairAfterVerifiedTakeover(receipt, takeoverReceipt, outbox, currentEpisode)
	} else {
		pairErr = storage.VerifyFundingRecoveryReceiptOutboxPair(receipt, outbox)
	}
	if pairErr != nil {
		return accounting.RecoverySourceCursor{}, fmt.Errorf("funding-income replay receipt/outbox mismatch: %w", pairErr)
	}
	return *snapshot.SourceCursor, nil
}

func fundingIncomeReplayReceiptRevisionMatches(receipt accounting.RecoveryReceipt, outbox accounting.RecoveryOutbox,
	projectedEpisode, currentEpisode accounting.RecoveryEpisode) bool {
	if outbox.TargetEpisodeRevision >= accounting.MaxDurableRecoveryRevision {
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
			takeover.AuditRef != "" && validFundingRecoveryTakeoverEvidenceHash(takeover.EvidenceHash) {
			return true
		}
	}
	return false
}

func validFundingRecoveryTakeoverEvidenceHash(value string) bool {
	if len(value) != sha256.Size*2 || strings.ToLower(value) != value {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size
}

func getFundingRecoveryStatusHandler(c *gin.Context) {
	if !requireRetiredEquityAdmin(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin_required"})
		return
	}
	s := currentOrderReconciliationService()
	if s == nil {
		reconciliationUnavailable(c)
		return
	}
	status, err := s.ReadFundingRecoveryStatus(c.Request.Context(), c.Param("episode_id"))
	if err != nil {
		if errors.Is(err, storage.ErrFundingRecoveryNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "funding_recovery_episode_not_found"})
			return
		}
		reconciliationUnavailable(c)
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": status})
}

func getFundingRecoveryReceiptHandler(c *gin.Context) {
	if !requireRetiredEquityAdmin(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin_required"})
		return
	}
	s := currentOrderReconciliationService()
	if s == nil || s.Recovery == nil {
		reconciliationUnavailable(c)
		return
	}
	receipt, found, err := s.Recovery.GetRecoveryReceipt(c.Request.Context(), c.Param("episode_id"), c.Param("operation_id"))
	if err != nil {
		reconciliationUnavailable(c)
		return
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "funding_recovery_receipt_not_found"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"receipt": receipt})
}

type fundingRecoveryAdjudicationRequest struct {
	OperationID             string `json:"operation_id"`
	PermitID                string `json:"permit_id"`
	ExpectedAttemptRevision uint64 `json:"expected_attempt_revision"`
	ExpectedEpisodeRevision uint64 `json:"expected_episode_revision"`
	AuditRef                string `json:"audit_ref"`
	Confirmed               bool   `json:"confirmed"`
}

func adjudicateFundingRecoverySubmissionHandler(c *gin.Context) {
	if !requireRetiredEquityAdmin(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin_required"})
		return
	}
	s := currentOrderReconciliationService()
	if s == nil || s.Recovery == nil || s.RecoverySubmissionAdjudicator == nil {
		reconciliationUnavailable(c)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	var request fundingRecoveryAdjudicationRequest
	if err := decoder.Decode(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_funding_recovery_adjudication"})
		return
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_funding_recovery_adjudication"})
		return
	}
	session, _ := c.Get("session")
	admin, ok := session.(*Session)
	if !ok || strings.TrimSpace(admin.Username) == "" {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin_required"})
		return
	}
	adjudication := accounting.RecoverySubmissionAdjudication{
		EpisodeID: c.Param("episode_id"), OperationID: request.OperationID, PermitID: request.PermitID,
		ExpectedAttemptRevision: request.ExpectedAttemptRevision, ExpectedEpisodeRevision: request.ExpectedEpisodeRevision,
		Decision: accounting.RecoverySubmissionFound, Actor: admin.Username, AuditRef: request.AuditRef, Confirmed: request.Confirmed,
	}
	attempt, replayed, err := s.RecoverySubmissionAdjudicator(c.Request.Context(), adjudication.EpisodeID, adjudication)
	if err != nil {
		if errors.Is(err, accounting.ErrRecoveryConflict) || errors.Is(err, accounting.ErrRecoveryOperationConflict) {
			c.JSON(http.StatusConflict, gin.H{"error": "funding_recovery_adjudication_conflict"})
			return
		}
		if errors.Is(err, storage.ErrFundingRecoveryNotFound) {
			c.JSON(http.StatusNotFound, gin.H{"error": "funding_recovery_attempt_not_found"})
			return
		}
		c.JSON(http.StatusConflict, gin.H{"error": "funding_recovery_evidence_unverified", "outcome_remains": accounting.RecoverySubmissionUnknown})
		return
	}
	c.JSON(http.StatusOK, gin.H{"attempt": attempt, "replayed": replayed, "hold_released": false})
}

type fundingRecoveryTakeoverRequest struct {
	OperationID             string `json:"operation_id"`
	ExpectedEpisodeRevision uint64 `json:"expected_episode_revision"`
	AuditRef                string `json:"audit_ref"`
	Confirmed               bool   `json:"confirmed"`
}

func takeOverFundingRecoveryEpisodeHandler(c *gin.Context) {
	if !requireRetiredEquityAdmin(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin_required"})
		return
	}
	s := currentOrderReconciliationService()
	if s == nil || s.Recovery == nil || s.RecoveryOwnerTakeover == nil {
		reconciliationUnavailable(c)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	var request fundingRecoveryTakeoverRequest
	if err := decoder.Decode(&request); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_funding_recovery_takeover"})
		return
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_funding_recovery_takeover"})
		return
	}
	session, _ := c.Get("session")
	admin, ok := session.(*Session)
	if !ok || strings.TrimSpace(admin.Username) == "" {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin_required"})
		return
	}
	episode, replayed, err := s.RecoveryOwnerTakeover(c.Request.Context(), c.Param("episode_id"), request.OperationID, admin.Username, request.AuditRef, request.ExpectedEpisodeRevision, request.Confirmed)
	if err != nil {
		if errors.Is(err, accounting.ErrRecoveryConflict) || errors.Is(err, accounting.ErrRecoveryOperationConflict) {
			c.JSON(http.StatusConflict, gin.H{"error": "funding_recovery_takeover_conflict"})
			return
		}
		c.JSON(http.StatusConflict, gin.H{"error": "funding_recovery_takeover_unavailable"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"episode": episode, "replayed": replayed, "hold_released": false})
}

func previewFundingRecoveryFillsHandler(c *gin.Context) {
	if !requireRetiredEquityAdmin(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin_required"})
		return
	}
	s := currentOrderReconciliationService()
	if s == nil {
		reconciliationUnavailable(c)
		return
	}
	preview, err := s.PreviewFundingRecoveryFill(c.Request.Context(), c.Param("episode_id"), c.Param("child_id"))
	if err != nil {
		writeFundingRecoveryFillError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"preview": preview, "read_only": true, "hold_released": false})
}

type fundingRecoveryFillApplyRequest struct {
	OperationID             string `json:"operation_id"`
	ExpectedEpisodeRevision uint64 `json:"expected_episode_revision"`
	ExpectedChildRevision   uint64 `json:"expected_child_revision"`
	SourceRevision          string `json:"source_revision"`
	AuditRef                string `json:"audit_ref"`
	Confirmed               bool   `json:"confirmed"`
}

func applyFundingRecoveryFillsHandler(c *gin.Context) {
	if !requireRetiredEquityAdmin(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin_required"})
		return
	}
	s := currentOrderReconciliationService()
	if s == nil || s.RecoveryFillApply == nil || s.Recovery == nil {
		reconciliationUnavailable(c)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	var input fundingRecoveryFillApplyRequest
	if err := decoder.Decode(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_funding_recovery_fill_apply"})
		return
	}
	var trailing any
	if !errors.Is(decoder.Decode(&trailing), io.EOF) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_funding_recovery_fill_apply"})
		return
	}
	session, _ := c.Get("session")
	admin, ok := session.(*Session)
	if !ok || strings.TrimSpace(admin.Username) == "" {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin_required"})
		return
	}
	readback, err := s.ApplyFundingRecoveryFill(c.Request.Context(), strategy.ApplyFundingRecoveryFillsRequest{
		EpisodeID: c.Param("episode_id"), ChildID: c.Param("child_id"), OperationID: input.OperationID,
		ExpectedEpisodeRevision: input.ExpectedEpisodeRevision, ExpectedChildRevision: input.ExpectedChildRevision,
		PreviewSourceRevision: input.SourceRevision, Actor: admin.Username, AuditRef: input.AuditRef, Confirmed: input.Confirmed,
	})
	if err != nil {
		writeFundingRecoveryFillError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"readback": readback, "hold_released": false, "execution_replayed": false})
}

func previewFundingRecoveryFeeAdjustmentHandler(c *gin.Context) {
	if !requireRetiredEquityAdmin(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin_required"})
		return
	}
	s := currentOrderReconciliationService()
	if s == nil {
		reconciliationUnavailable(c)
		return
	}
	preview, err := s.PreviewFundingRecoveryFeeAdjustment(c.Request.Context(), c.Param("episode_id"), c.Param("child_id"), c.Param("trade_id"))
	if err != nil {
		writeFundingRecoveryFillError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"preview": preview, "read_only": true, "hold_released": false})
}

type fundingRecoveryFeeAdjustmentApplyRequest struct {
	OperationID             string `json:"operation_id"`
	AdjustmentID            string `json:"adjustment_id"`
	AmountDeltaRaw          string `json:"amount_delta_raw"`
	Asset                   string `json:"asset"`
	EvidenceHash            string `json:"evidence_hash"`
	SourceRevision          string `json:"source_revision"`
	ExpectedEpisodeRevision uint64 `json:"expected_episode_revision"`
	ExpectedChildRevision   uint64 `json:"expected_child_revision"`
	AuditRef                string `json:"audit_ref"`
	Confirmed               bool   `json:"confirmed"`
}

func applyFundingRecoveryFeeAdjustmentHandler(c *gin.Context) {
	if !requireRetiredEquityAdmin(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin_required"})
		return
	}
	s := currentOrderReconciliationService()
	if s == nil || s.RecoveryFeeApply == nil || s.Recovery == nil {
		reconciliationUnavailable(c)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	var input fundingRecoveryFeeAdjustmentApplyRequest
	if err := decoder.Decode(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_funding_recovery_fee_adjustment"})
		return
	}
	var trailing any
	if !errors.Is(decoder.Decode(&trailing), io.EOF) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_funding_recovery_fee_adjustment"})
		return
	}
	session, _ := c.Get("session")
	admin, ok := session.(*Session)
	if !ok || strings.TrimSpace(admin.Username) == "" {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin_required"})
		return
	}
	readback, err := s.ApplyFundingRecoveryFeeAdjustment(c.Request.Context(), strategy.ApplyFundingRecoveryFeeAdjustmentRequest{
		EpisodeID: c.Param("episode_id"), ChildID: c.Param("child_id"), TradeID: c.Param("trade_id"), OperationID: input.OperationID,
		AdjustmentID: input.AdjustmentID, AmountDeltaRaw: input.AmountDeltaRaw, Asset: input.Asset, EvidenceHash: input.EvidenceHash,
		SourceRevision: input.SourceRevision, ExpectedEpisodeRevision: input.ExpectedEpisodeRevision,
		ExpectedChildRevision: input.ExpectedChildRevision, Actor: admin.Username, AuditRef: input.AuditRef, Confirmed: input.Confirmed,
	})
	if err != nil {
		writeFundingRecoveryFillError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"readback": readback, "hold_released": false, "execution_replayed": false})
}

type fundingRecoveryOpeningBalanceRequest struct {
	OperationID             string                                    `json:"operation_id"`
	ExpectedEpisodeRevision uint64                                    `json:"expected_episode_revision"`
	AuditRef                string                                    `json:"audit_ref"`
	Snapshot                accounting.RecoveryOpeningBalanceSnapshot `json:"snapshot"`
	Confirmed               bool                                      `json:"confirmed"`
}

func applyFundingRecoveryOpeningBalanceHandler(c *gin.Context) {
	if !requireRetiredEquityAdmin(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin_required"})
		return
	}
	s := currentOrderReconciliationService()
	if s == nil || s.Recovery == nil || s.RecoveryOpeningBalanceApply == nil {
		reconciliationUnavailable(c)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	var input fundingRecoveryOpeningBalanceRequest
	if err := decoder.Decode(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_funding_recovery_opening_balance"})
		return
	}
	var trailing any
	if !errors.Is(decoder.Decode(&trailing), io.EOF) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_funding_recovery_opening_balance"})
		return
	}
	session, _ := c.Get("session")
	admin, ok := session.(*Session)
	if !ok || strings.TrimSpace(admin.Username) == "" {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin_required"})
		return
	}
	snapshotHash, err := input.Snapshot.CanonicalHash()
	if err != nil || strings.TrimSpace(input.AuditRef) == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_funding_recovery_opening_balance_snapshot"})
		return
	}
	readback, err := s.ApplyFundingRecoveryOpeningBalance(c.Request.Context(), strategy.ApplyFundingRecoveryOpeningBalanceRequest{
		EpisodeID: c.Param("episode_id"), OperationID: input.OperationID, ExpectedEpisodeRevision: input.ExpectedEpisodeRevision,
		Snapshot: input.Snapshot, EvidenceHash: snapshotHash, Actor: admin.Username, AuditRef: input.AuditRef, Confirmed: input.Confirmed,
	})
	if err != nil {
		writeFundingRecoveryFillError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"readback": readback, "hold_released": false, "execution_replayed": false})
}

type fundingRecoveryIncomeRequest struct {
	OperationID             string    `json:"operation_id"`
	ExpectedEpisodeRevision uint64    `json:"expected_episode_revision"`
	From                    time.Time `json:"from"`
	AuditRef                string    `json:"audit_ref"`
	Confirmed               bool      `json:"confirmed"`
}

func applyFundingRecoveryIncomeHandler(c *gin.Context) {
	if !requireRetiredEquityAdmin(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin_required"})
		return
	}
	s := currentOrderReconciliationService()
	if s == nil || s.Recovery == nil || s.RecoveryFundingIncomeApply == nil {
		reconciliationUnavailable(c)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 32<<10)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	var input fundingRecoveryIncomeRequest
	if err := decoder.Decode(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_funding_recovery_income_request"})
		return
	}
	var trailing any
	if !errors.Is(decoder.Decode(&trailing), io.EOF) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_funding_recovery_income_request"})
		return
	}
	session, _ := c.Get("session")
	admin, ok := session.(*Session)
	if !ok || strings.TrimSpace(admin.Username) == "" || strings.TrimSpace(input.AuditRef) == "" || !input.Confirmed ||
		input.From.IsZero() || input.From.Nanosecond()%int(time.Millisecond) != 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "incomplete_funding_recovery_income_confirmation"})
		return
	}
	readback, err := s.ApplyFundingRecoveryIncome(c.Request.Context(), strategy.ApplyFundingRecoveryIncomeRequest{
		EpisodeID: c.Param("episode_id"), OperationID: input.OperationID, ExpectedEpisodeRevision: input.ExpectedEpisodeRevision,
		From: input.From.UTC(), Actor: admin.Username, AuditRef: input.AuditRef, Confirmed: input.Confirmed,
	})
	if err != nil {
		writeFundingRecoveryFillError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"readback": readback, "hold_released": false, "execution_replayed": false})
}

type fundingRecoveryAnomalyAdjustmentRequest struct {
	OperationID             string `json:"operation_id"`
	ExpectedEpisodeRevision uint64 `json:"expected_episode_revision"`
	ExpectedAnomalyRevision uint64 `json:"expected_anomaly_revision"`
	EvidenceHash            string `json:"evidence_hash"`
	AuditRef                string `json:"audit_ref"`
	Confirmed               bool   `json:"confirmed"`
}

func applyFundingRecoveryAnomalyAdjustmentHandler(c *gin.Context) {
	if !requireRetiredEquityAdmin(c) {
		c.JSON(http.StatusForbidden, gin.H{"error": "admin_required"})
		return
	}
	s := currentOrderReconciliationService()
	if s == nil || s.Recovery == nil || s.RecoveryAnomalyAdjustmentApply == nil {
		reconciliationUnavailable(c)
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	var input fundingRecoveryAnomalyAdjustmentRequest
	if err := decoder.Decode(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_funding_recovery_anomaly_adjustment"})
		return
	}
	var trailing any
	if !errors.Is(decoder.Decode(&trailing), io.EOF) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid_funding_recovery_anomaly_adjustment"})
		return
	}
	session, _ := c.Get("session")
	admin, ok := session.(*Session)
	if !ok || strings.TrimSpace(admin.Username) == "" || strings.TrimSpace(input.AuditRef) == "" || !input.Confirmed {
		c.JSON(http.StatusBadRequest, gin.H{"error": "incomplete_funding_recovery_anomaly_confirmation"})
		return
	}
	readback, err := s.ApplyFundingRecoveryAnomalyAdjustment(c.Request.Context(), strategy.ApplyFundingRecoveryAnomalyAdjustmentRequest{
		EpisodeID: c.Param("episode_id"), AnomalyID: c.Param("anomaly_id"), OperationID: input.OperationID,
		ExpectedEpisodeRevision: input.ExpectedEpisodeRevision, ExpectedAnomalyRevision: input.ExpectedAnomalyRevision,
		EvidenceHash: input.EvidenceHash, Actor: admin.Username, AuditRef: input.AuditRef, Confirmed: input.Confirmed,
	})
	if err != nil {
		writeFundingRecoveryFillError(c, err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"readback": readback, "hold_released": false, "execution_replayed": false})
}

func writeFundingRecoveryFillError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, storage.ErrFundingRecoveryNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "funding_recovery_record_not_found"})
	case errors.Is(err, accounting.ErrRecoveryConflict), errors.Is(err, accounting.ErrRecoveryOperationConflict):
		c.JSON(http.StatusConflict, gin.H{"error": "funding_recovery_fill_conflict", "hold_released": false})
	default:
		c.JSON(http.StatusConflict, gin.H{"error": "funding_recovery_fill_evidence_unverified", "hold_released": false})
	}
}
