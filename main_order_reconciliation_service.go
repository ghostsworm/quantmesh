package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"quantmesh/accounting"
	"quantmesh/execution"
	"quantmesh/storage"
	"quantmesh/strategy"
	"quantmesh/web"
)

// buildProductionOrderReconciliationService accepts the storage abstraction
// exposed by StorageService but only enables the durable SQL implementation.
// Schema migration remains owned by StorageService startup.
func buildProductionOrderReconciliationService(backend storage.Storage, bots *BotManager) (*web.OrderReconciliationService, error) {
	store, ok := backend.(*storage.SQLStorage)
	if !ok || store == nil {
		return nil, fmt.Errorf("order reconciliation service: initialized SQL storage is required")
	}
	return newProductionOrderReconciliationService(store, bots)
}

// newProductionOrderReconciliationService binds the already-initialized
// durable case/audit repository and read-only runtime evidence provider.
// StorageService owns schema migration; Accounting and Gate intentionally
// remain unset until independently verified implementations exist.
func newProductionOrderReconciliationService(store *storage.SQLStorage, bots *BotManager) (*web.OrderReconciliationService, error) {
	if store == nil {
		return nil, fmt.Errorf("order reconciliation service: SQL storage is required")
	}
	if bots == nil {
		return nil, fmt.Errorf("order reconciliation service: Bot manager is required")
	}
	service := &web.OrderReconciliationService{
		Repository: store,
		Evidence:   newRuntimeTrustedOrderEvidenceProvider(bots),
		Recovery:   store,
		RecoveryRuntimeResolver: func(ctx context.Context, episode accounting.RecoveryEpisode) (storage.FundingCarryRuntimeGeneration, error) {
			var generation storage.FundingCarryRuntimeGeneration
			err := bots.WithBotRuntimeOperationContext(ctx, episode.Bot, func(operationCtx context.Context, _ *BotRuntime) error {
				_, resolved, resolveErr := resolveHeldFundingCarryStrategyForFill(operationCtx, store, bots, episode, false)
				generation = resolved
				return resolveErr
			})
			return generation, err
		},
		RecoveryOwnerTakeover: func(ctx context.Context, episodeID, operationID, actor, auditRef string, expectedRevision uint64, confirmed bool) (accounting.RecoveryEpisode, bool, error) {
			episode, found, err := store.GetRecoveryEpisode(ctx, episodeID)
			if err != nil {
				return accounting.RecoveryEpisode{}, false, err
			}
			if !found {
				return accounting.RecoveryEpisode{}, false, storage.ErrFundingRecoveryNotFound
			}
			var result accounting.RecoveryEpisode
			var replayed bool
			err = bots.WithBotRuntimeOperationContext(ctx, episode.Bot, func(operationCtx context.Context, _ *BotRuntime) error {
				current, stillFound, readErr := store.GetRecoveryEpisode(operationCtx, episodeID)
				if readErr != nil {
					return readErr
				}
				if !stillFound {
					return storage.ErrFundingRecoveryNotFound
				}
				carry, generation, resolveErr := resolveHeldFundingCarryStrategy(operationCtx, store, bots, current, true)
				if resolveErr != nil {
					return resolveErr
				}
				expectedOwner := current.OwnerGeneration
				if current.Revision != expectedRevision {
					if len(current.OwnerTakeovers) == 0 {
						return accounting.ErrRecoveryConflict
					}
					last := current.OwnerTakeovers[len(current.OwnerTakeovers)-1]
					if last.OperationID != operationID || last.OwnerGeneration != generation.ID() || last.EpisodeRevision == 0 || last.EpisodeRevision-1 != expectedRevision {
						return accounting.ErrRecoveryConflict
					}
					expectedOwner = last.PreviousOwnerGeneration
				}
				result, replayed, resolveErr = store.TakeOverRecoveryEpisode(operationCtx, generation, accounting.RecoveryOwnerTakeoverAuthorization{
					EpisodeID: current.ID, OperationID: operationID, Actor: actor, AuditRef: auditRef,
					ExpectedOwnerGeneration: expectedOwner, ExpectedEpisodeRevision: expectedRevision, Confirmed: confirmed,
				})
				if resolveErr != nil {
					return resolveErr
				}
				if _, projectionErr := carry.ProjectRecoveryOutboxAfterTakeover(operationCtx, current.ID, operationID); projectionErr != nil {
					return fmt.Errorf("episode takeover was persisted; existing recovery outbox remains held for retry: %w", projectionErr)
				}
				return nil
			})
			return result, replayed, err
		},
		RecoverySubmissionAdjudicator: func(ctx context.Context, episodeID string, adjudication accounting.RecoverySubmissionAdjudication) (accounting.RecoverySubmissionAttempt, bool, error) {
			episode, found, err := store.GetRecoveryEpisode(ctx, episodeID)
			if err != nil {
				return accounting.RecoverySubmissionAttempt{}, false, err
			}
			if !found {
				return accounting.RecoverySubmissionAttempt{}, false, storage.ErrFundingRecoveryNotFound
			}
			var result accounting.RecoverySubmissionAttempt
			var replayed bool
			err = bots.WithBotRuntimeOperationContext(ctx, episode.Bot, func(operationCtx context.Context, _ *BotRuntime) error {
				current, stillFound, readErr := store.GetRecoveryEpisode(operationCtx, episodeID)
				if readErr != nil {
					return readErr
				}
				if !stillFound {
					return storage.ErrFundingRecoveryNotFound
				}
				carry, _, resolveErr := resolveHeldFundingCarryStrategy(operationCtx, store, bots, current, false)
				if resolveErr != nil {
					return resolveErr
				}
				result, replayed, resolveErr = carry.AdjudicateRecoverySubmissionFound(operationCtx, adjudication)
				return resolveErr
			})
			return result, replayed, err
		},
		RecoveryFillPreview: func(ctx context.Context, episodeID, childID string) (strategy.FundingRecoveryFillPreview, error) {
			episode, found, err := store.GetRecoveryEpisode(ctx, episodeID)
			if err != nil {
				return strategy.FundingRecoveryFillPreview{}, err
			}
			if !found {
				return strategy.FundingRecoveryFillPreview{}, storage.ErrFundingRecoveryNotFound
			}
			var preview strategy.FundingRecoveryFillPreview
			err = bots.WithBotRuntimeOperationContext(ctx, episode.Bot, func(operationCtx context.Context, _ *BotRuntime) error {
				current, stillFound, readErr := store.GetRecoveryEpisode(operationCtx, episodeID)
				if readErr != nil {
					return readErr
				}
				if !stillFound {
					return storage.ErrFundingRecoveryNotFound
				}
				carry, _, resolveErr := resolveHeldFundingCarryStrategyForFill(operationCtx, store, bots, current, false)
				if resolveErr != nil {
					return resolveErr
				}
				preview, resolveErr = carry.PreviewFundingRecoveryFills(operationCtx, episodeID, childID)
				if resolveErr == nil && preview.EpisodeRevision != current.Revision {
					return accounting.ErrRecoveryConflict
				}
				return resolveErr
			})
			return preview, err
		},
		RecoveryFillApply: func(ctx context.Context, request strategy.ApplyFundingRecoveryFillsRequest) (strategy.ApplyFundingRecoveryFillsResult, error) {
			episode, found, err := store.GetRecoveryEpisode(ctx, request.EpisodeID)
			if err != nil {
				return strategy.ApplyFundingRecoveryFillsResult{}, err
			}
			if !found {
				return strategy.ApplyFundingRecoveryFillsResult{}, storage.ErrFundingRecoveryNotFound
			}
			var result strategy.ApplyFundingRecoveryFillsResult
			err = bots.WithBotRuntimeOperationContext(ctx, episode.Bot, func(operationCtx context.Context, _ *BotRuntime) error {
				current, stillFound, readErr := store.GetRecoveryEpisode(operationCtx, request.EpisodeID)
				if readErr != nil {
					return readErr
				}
				if !stillFound {
					return storage.ErrFundingRecoveryNotFound
				}
				carry, _, resolveErr := resolveHeldFundingCarryStrategyForFill(operationCtx, store, bots, current, true)
				if resolveErr != nil {
					return resolveErr
				}
				result, resolveErr = carry.ApplyFundingRecoveryFills(operationCtx, request)
				if resolveErr != nil {
					return resolveErr
				}
				if !result.Projected {
					return fmt.Errorf("funding fill operation was not projected; durable hold remains active")
				}
				outbox, outboxFound, readErr := store.GetRecoveryOutbox(operationCtx, request.EpisodeID, request.OperationID)
				if readErr != nil {
					return fmt.Errorf("read back projected funding fill outbox: %w", readErr)
				}
				if !outboxFound || outbox.State != accounting.RecoveryProjected {
					return fmt.Errorf("projected funding fill outbox is missing or not projected")
				}
				runtimeState, readErr := store.GetStrategyRuntimeStateContext(operationCtx, episode.Bot, "funding_carry")
				if readErr != nil {
					return fmt.Errorf("read back projected FundingCarry runtime state: %w", readErr)
				}
				if runtimeState == nil || !fundingCarryProjectionMarkerReadbackMatches(runtimeState.Payload, request.EpisodeID, request.OperationID, outbox) {
					return fmt.Errorf("projected FundingCarry runtime marker is missing")
				}
				hold, held, readErr := store.GetRecoveryHold(operationCtx, request.EpisodeID, current.RecoveryGeneration)
				if readErr != nil {
					return fmt.Errorf("read back FundingCarry recovery hold: %w", readErr)
				}
				if !held || hold.State != "held" {
					return fmt.Errorf("FundingCarry recovery hold changed during fill projection")
				}
				return nil
			})
			return result, err
		},
		RecoveryFeePreview: func(ctx context.Context, episodeID, childID, tradeID string) (strategy.FundingRecoveryFeeAdjustmentPreview, error) {
			episode, found, err := store.GetRecoveryEpisode(ctx, episodeID)
			if err != nil {
				return strategy.FundingRecoveryFeeAdjustmentPreview{}, err
			}
			if !found {
				return strategy.FundingRecoveryFeeAdjustmentPreview{}, storage.ErrFundingRecoveryNotFound
			}
			var preview strategy.FundingRecoveryFeeAdjustmentPreview
			err = bots.WithBotRuntimeOperationContext(ctx, episode.Bot, func(operationCtx context.Context, _ *BotRuntime) error {
				current, stillFound, readErr := store.GetRecoveryEpisode(operationCtx, episodeID)
				if readErr != nil {
					return readErr
				}
				if !stillFound {
					return storage.ErrFundingRecoveryNotFound
				}
				carry, _, resolveErr := resolveHeldFundingCarryStrategyForFill(operationCtx, store, bots, current, false)
				if resolveErr != nil {
					return resolveErr
				}
				preview, resolveErr = carry.PreviewFundingRecoveryFeeAdjustment(operationCtx, episodeID, childID, tradeID)
				if resolveErr == nil && preview.EpisodeRevision != current.Revision {
					return accounting.ErrRecoveryConflict
				}
				return resolveErr
			})
			return preview, err
		},
		RecoveryFeeApply: func(ctx context.Context, request strategy.ApplyFundingRecoveryFeeAdjustmentRequest) (strategy.ApplyFundingRecoveryFeeAdjustmentResult, error) {
			episode, found, err := store.GetRecoveryEpisode(ctx, request.EpisodeID)
			if err != nil {
				return strategy.ApplyFundingRecoveryFeeAdjustmentResult{}, err
			}
			if !found {
				return strategy.ApplyFundingRecoveryFeeAdjustmentResult{}, storage.ErrFundingRecoveryNotFound
			}
			var result strategy.ApplyFundingRecoveryFeeAdjustmentResult
			err = bots.WithBotRuntimeOperationContext(ctx, episode.Bot, func(operationCtx context.Context, _ *BotRuntime) error {
				current, stillFound, readErr := store.GetRecoveryEpisode(operationCtx, request.EpisodeID)
				if readErr != nil {
					return readErr
				}
				if !stillFound {
					return storage.ErrFundingRecoveryNotFound
				}
				carry, _, resolveErr := resolveHeldFundingCarryStrategyForFill(operationCtx, store, bots, current, true)
				if resolveErr != nil {
					return resolveErr
				}
				result, resolveErr = carry.ApplyFundingRecoveryFeeAdjustment(operationCtx, request)
				if resolveErr != nil {
					return resolveErr
				}
				if !result.Projected {
					return fmt.Errorf("fee adjustment operation was not projected; durable hold remains active")
				}
				outbox, outboxFound, readErr := store.GetRecoveryOutbox(operationCtx, request.EpisodeID, request.OperationID)
				if readErr != nil || !outboxFound || outbox.State != accounting.RecoveryProjected {
					return errors.Join(fmt.Errorf("fee adjustment outbox is not projected"), readErr)
				}
				runtimeState, readErr := store.GetStrategyRuntimeStateContext(operationCtx, episode.Bot, "funding_carry")
				if readErr != nil || runtimeState == nil || !fundingCarryProjectionMarkerReadbackMatches(runtimeState.Payload, request.EpisodeID, request.OperationID, outbox) {
					return errors.Join(fmt.Errorf("fee adjustment runtime marker readback failed"), readErr)
				}
				hold, held, readErr := store.GetRecoveryHold(operationCtx, request.EpisodeID, current.RecoveryGeneration)
				if readErr != nil || !held || hold.State != "held" {
					return errors.Join(fmt.Errorf("fee adjustment changed or lost the active hold"), readErr)
				}
				return nil
			})
			return result, err
		},
		RecoveryOpeningBalanceApply: func(ctx context.Context, request strategy.ApplyFundingRecoveryOpeningBalanceRequest) (strategy.ApplyFundingRecoveryOpeningBalanceResult, error) {
			episode, found, err := store.GetRecoveryEpisode(ctx, request.EpisodeID)
			if err != nil {
				return strategy.ApplyFundingRecoveryOpeningBalanceResult{}, err
			}
			if !found {
				return strategy.ApplyFundingRecoveryOpeningBalanceResult{}, storage.ErrFundingRecoveryNotFound
			}
			var result strategy.ApplyFundingRecoveryOpeningBalanceResult
			err = bots.WithBotRuntimeOperationContext(ctx, episode.Bot, func(operationCtx context.Context, _ *BotRuntime) error {
				current, stillFound, readErr := store.GetRecoveryEpisode(operationCtx, request.EpisodeID)
				if readErr != nil {
					return readErr
				}
				if !stillFound {
					return storage.ErrFundingRecoveryNotFound
				}
				carry, _, resolveErr := resolveHeldFundingCarryStrategyForFill(operationCtx, store, bots, current, true)
				if resolveErr != nil {
					return resolveErr
				}
				result, resolveErr = carry.ApplyFundingRecoveryOpeningBalance(operationCtx, request)
				if resolveErr != nil {
					return resolveErr
				}
				if !result.Projected {
					return errors.New("opening balance projection is incomplete; hold remains active")
				}
				outbox, outboxFound, readErr := store.GetRecoveryOutbox(operationCtx, request.EpisodeID, request.OperationID)
				if readErr != nil || !outboxFound || outbox.State != accounting.RecoveryProjected {
					return errors.Join(errors.New("opening balance outbox readback failed"), readErr)
				}
				runtimeState, readErr := store.GetStrategyRuntimeStateContext(operationCtx, episode.Bot, "funding_carry")
				if readErr != nil || runtimeState == nil || !fundingCarryProjectionMarkerReadbackMatches(runtimeState.Payload, request.EpisodeID, request.OperationID, outbox) {
					return errors.Join(errors.New("opening balance runtime marker readback failed"), readErr)
				}
				hold, held, readErr := store.GetRecoveryHold(operationCtx, request.EpisodeID, current.RecoveryGeneration)
				if readErr != nil || !held || hold.State != "held" {
					return errors.Join(errors.New("opening balance changed or lost the active hold"), readErr)
				}
				return nil
			})
			return result, err
		},
		RecoveryFundingIncomeApply: func(ctx context.Context, request strategy.ApplyFundingRecoveryIncomeRequest) (strategy.ApplyFundingRecoveryIncomeResult, error) {
			episode, found, err := store.GetRecoveryEpisode(ctx, request.EpisodeID)
			if err != nil {
				return strategy.ApplyFundingRecoveryIncomeResult{}, err
			}
			if !found {
				return strategy.ApplyFundingRecoveryIncomeResult{}, storage.ErrFundingRecoveryNotFound
			}
			var result strategy.ApplyFundingRecoveryIncomeResult
			err = bots.WithBotRuntimeOperationContext(ctx, episode.Bot, func(operationCtx context.Context, _ *BotRuntime) error {
				current, stillFound, readErr := store.GetRecoveryEpisode(operationCtx, request.EpisodeID)
				if readErr != nil {
					return readErr
				}
				if !stillFound {
					return storage.ErrFundingRecoveryNotFound
				}
				allowPendingProjectionRetry := false
				if current.State == accounting.RecoveryLedgerCommitted {
					receipt, receiptFound, receiptErr := store.GetRecoveryReceipt(operationCtx, request.EpisodeID, request.OperationID)
					outbox, outboxFound, outboxErr := store.GetRecoveryOutbox(operationCtx, request.EpisodeID, request.OperationID)
					if receiptErr != nil || outboxErr != nil {
						return errors.Join(errors.New("read exact funding-income pending projection receipt"), receiptErr, outboxErr)
					}
					if receiptFound && outboxFound && fundingIncomePendingProjectionMatches(receipt, outbox, request) {
						allowPendingProjectionRetry = true
					} else {
						return accounting.ErrRecoveryOperationConflict
					}
				}
				carry, _, resolveErr := resolveHeldFundingCarryStrategyForFill(operationCtx, store, bots, current, allowPendingProjectionRetry)
				if resolveErr != nil {
					return resolveErr
				}
				result, resolveErr = carry.ApplyFundingRecoveryIncome(operationCtx, request)
				if resolveErr != nil {
					return resolveErr
				}
				if !result.Projected || !result.HoldRetained {
					return errors.New("funding-income facts are not fully projected or hold is missing")
				}
				return nil
			})
			return result, err
		},
		RecoveryAnomalyAdjustmentApply: func(ctx context.Context, request strategy.ApplyFundingRecoveryAnomalyAdjustmentRequest) (strategy.ApplyFundingRecoveryAnomalyAdjustmentResult, error) {
			episode, found, err := store.GetRecoveryEpisode(ctx, request.EpisodeID)
			if err != nil {
				return strategy.ApplyFundingRecoveryAnomalyAdjustmentResult{}, err
			}
			if !found {
				return strategy.ApplyFundingRecoveryAnomalyAdjustmentResult{}, storage.ErrFundingRecoveryNotFound
			}
			var result strategy.ApplyFundingRecoveryAnomalyAdjustmentResult
			err = bots.WithBotRuntimeOperationContext(ctx, episode.Bot, func(operationCtx context.Context, _ *BotRuntime) error {
				current, stillFound, readErr := store.GetRecoveryEpisode(operationCtx, request.EpisodeID)
				if readErr != nil {
					return readErr
				}
				if !stillFound {
					return storage.ErrFundingRecoveryNotFound
				}
				allowPendingProjectionRetry := false
				if current.State == accounting.RecoveryLedgerCommitted {
					receipt, receiptFound, receiptErr := store.GetRecoveryReceipt(operationCtx, request.EpisodeID, request.OperationID)
					outbox, outboxFound, outboxErr := store.GetRecoveryOutbox(operationCtx, request.EpisodeID, request.OperationID)
					if receiptErr != nil || outboxErr != nil {
						return errors.Join(errors.New("read source-anomaly adjustment projection receipt"), receiptErr, outboxErr)
					}
					if receiptFound && outboxFound && receipt.State == accounting.RecoveryLedgerCommitted && outbox.State == accounting.RecoveryProjectionPending &&
						outbox.TargetEpisodeRevision == request.ExpectedEpisodeRevision+1 && outbox.ProjectionHash == receipt.ProjectionHash {
						allowPendingProjectionRetry = true
					} else {
						return accounting.ErrRecoveryOperationConflict
					}
				}
				carry, _, resolveErr := resolveHeldFundingCarryStrategyForFill(operationCtx, store, bots, current, allowPendingProjectionRetry)
				if resolveErr != nil {
					return resolveErr
				}
				result, resolveErr = carry.ApplyFundingRecoveryAnomalyAdjustment(operationCtx, request)
				if resolveErr != nil {
					return resolveErr
				}
				if !result.Projected || !result.HoldRetained {
					return errors.New("source-anomaly adjustment is not projected or hold is missing")
				}
				return nil
			})
			return result, err
		},
		// Generic order-accounting release remains unset. FundingCarry has a
		// separate evidence-bound durable release path below.
		Accounting: nil,
		Gate:       nil,
	}
	service.RecoveryGateSourceReader = func(ctx context.Context, episode accounting.RecoveryEpisode) ([]string, error) {
		var sources []string
		err := bots.WithBotRuntimeOperationContext(ctx, episode.Bot, func(operationCtx context.Context, _ *BotRuntime) error {
			current, found, err := store.GetRecoveryEpisode(operationCtx, episode.ID)
			if err != nil || !found {
				return errors.Join(storage.ErrFundingRecoveryNotFound, err)
			}
			carry, _, err := resolveHeldFundingCarryStrategyForFill(operationCtx, store, bots, current, false)
			if err != nil {
				return err
			}
			sources = carry.RecoveryOpeningGateSources()
			return nil
		})
		return sources, err
	}
	service.RecoveryReleaseApply = func(ctx context.Context, auth accounting.RecoveryReleaseAuthorization) (web.FundingRecoveryReleaseReadback, error) {
		episode, found, err := store.GetRecoveryEpisode(ctx, auth.EpisodeID)
		if err != nil || !found {
			return web.FundingRecoveryReleaseReadback{}, errors.Join(storage.ErrFundingRecoveryNotFound, err)
		}
		var readback web.FundingRecoveryReleaseReadback
		err = bots.WithBotRuntimeOperationContext(ctx, episode.Bot, func(operationCtx context.Context, _ *BotRuntime) error {
			current, stillFound, readErr := store.GetRecoveryEpisode(operationCtx, auth.EpisodeID)
			if readErr != nil || !stillFound {
				return errors.Join(storage.ErrFundingRecoveryNotFound, readErr)
			}
			carry, generation, resolveErr := resolveHeldFundingCarryStrategyForRelease(operationCtx, store, bots, current)
			if resolveErr != nil {
				return resolveErr
			}
			if current.State != accounting.RecoveryReleasePending && current.State != accounting.RecoveryReleased {
				status, statusErr := service.ReadFundingRecoveryStatus(operationCtx, current.ID)
				outboxes, outboxErr := store.ListRecoveryOutboxes(operationCtx, current.ID)
				gates := carry.RecoveryOpeningGateSources()
				if statusErr != nil || outboxErr != nil || len(outboxes) == 0 {
					return errors.Join(errors.New("read current FundingCarry release evidence"), statusErr, outboxErr)
				}
				evidenceHash, hashErr := web.FundingRecoveryReleaseEvidenceHash(status, outboxes, generation.ID(), gates)
				latest := outboxes[len(outboxes)-1]
				if hashErr != nil || evidenceHash != auth.EvidenceHash || current.Revision != auth.ExpectedEpisodeRevision ||
					current.RecoveryGeneration != auth.RecoveryGeneration || current.OwnerGeneration != auth.OwnerGeneration ||
					status.Hold.Revision != auth.ExpectedHoldRevision || latest.State != accounting.RecoveryProjected ||
					latest.AppliedHash != auth.ProjectionHash || latest.ExpectedRuntimeRevision != auth.RuntimeRevision {
					return errors.Join(accounting.ErrRecoveryConflict, hashErr)
				}
			}
			if _, err := store.PrepareRecoveryRelease(operationCtx, generation, auth); err != nil {
				return err
			}
			hold, err := store.ReleaseRecoveryHold(operationCtx, generation, auth)
			if err != nil {
				return err
			}
			if err := carry.ReleaseFundingRecoveryGate(operationCtx, auth.EpisodeID); err != nil {
				return &web.FundingRecoveryReleaseStateError{HoldReleased: hold.State == "released", Cause: fmt.Errorf("durable FundingCarry release recorded but exact Gate source readback failed: %w", err)}
			}
			updated, found, err := store.GetRecoveryEpisode(operationCtx, auth.EpisodeID)
			if err != nil || !found {
				return &web.FundingRecoveryReleaseStateError{HoldReleased: hold.State == "released", GateCleared: true, Cause: errors.Join(errors.New("read released FundingCarry episode"), err)}
			}
			receipt, receiptFound, err := store.GetRecoveryReceipt(operationCtx, auth.EpisodeID, auth.OperationID)
			if err != nil || !receiptFound {
				return &web.FundingRecoveryReleaseStateError{HoldReleased: hold.State == "released", GateCleared: true, Cause: errors.Join(errors.New("read durable FundingCarry release receipt"), err)}
			}
			sources := carry.RecoveryOpeningGateSources()
			source := execution.RecoveryEpisodeGateSource(auth.EpisodeID, auth.RecoveryGeneration)
			verified := updated.State == accounting.RecoveryReleased && hold.State == "released" && !containsFundingRecoveryGateSource(sources, source) &&
				receipt.State == accounting.RecoveryReleased && receipt.Actor == auth.Actor && receipt.AuditRef == auth.AuditRef && receipt.EvidenceHash == auth.EvidenceHash &&
				receipt.ExposureClearanceHash == auth.ExposureClearanceHash
			if !verified {
				return &web.FundingRecoveryReleaseStateError{HoldReleased: hold.State == "released", GateCleared: !containsFundingRecoveryGateSource(sources, source),
					Cause: errors.New("FundingCarry durable release/Gate readback mismatch; no order was replayed")}
			}
			readback = web.FundingRecoveryReleaseReadback{Episode: updated, Hold: hold, GateSources: sources, ReleasedSource: source,
				Receipt: receipt, ExecutionReplayed: false, Verified: true}
			return nil
		})
		return readback, err
	}
	return service, nil
}

func containsFundingRecoveryGateSource(sources []string, source string) bool {
	for _, candidate := range sources {
		if candidate == source {
			return true
		}
	}
	return false
}

func fundingIncomePendingProjectionMatches(receipt accounting.RecoveryReceipt, outbox accounting.RecoveryOutbox,
	request strategy.ApplyFundingRecoveryIncomeRequest) bool {
	return receipt.EpisodeID == request.EpisodeID && receipt.OperationID == request.OperationID &&
		receipt.State == accounting.RecoveryLedgerCommitted && outbox.EpisodeID == request.EpisodeID && outbox.OperationID == request.OperationID &&
		outbox.State == accounting.RecoveryProjectionPending && outbox.TargetEpisodeRevision == receipt.EpisodeRevision &&
		outbox.ProjectionHash != "" && outbox.ProjectionHash == receipt.ProjectionHash &&
		request.ExpectedEpisodeRevision < accounting.MaxDurableRecoveryRevision-1 && outbox.TargetEpisodeRevision == request.ExpectedEpisodeRevision+1
}

func resolveHeldFundingCarryStrategy(ctx context.Context, store *storage.SQLStorage, bots *BotManager, episode accounting.RecoveryEpisode, takeover bool) (*strategy.FundingCarryStrategy, storage.FundingCarryRuntimeGeneration, error) {
	stateAllowed := episode.State == accounting.RecoveryBlocked
	if takeover && episode.State != accounting.RecoveryReleasePending && episode.State != accounting.RecoveryReleased {
		stateAllowed = true
	}
	if ctx == nil || store == nil || bots == nil || episode.ID == "" || episode.Bot == "" || episode.Strategy != "funding_carry" || !stateAllowed {
		return nil, storage.FundingCarryRuntimeGeneration{}, fmt.Errorf("funding recovery episode is not eligible for held-runtime resolution")
	}
	bot, found := bots.Get(episode.Bot)
	if !found || bot == nil || bot.Inner == nil || bot.stopTransitionPending() || !bot.Inner.fundingCarryReconciliationOnly || bot.Inner.StrategyManager == nil {
		return nil, storage.FundingCarryRuntimeGeneration{}, fmt.Errorf("retained FundingCarry reconciliation runtime is unavailable")
	}
	carry, ok := bot.Inner.StrategyManager.GetStrategy("funding_carry").(*strategy.FundingCarryStrategy)
	if !ok || carry == nil {
		return nil, storage.FundingCarryRuntimeGeneration{}, fmt.Errorf("retained FundingCarry strategy is unavailable")
	}
	boundStore, generation, botID, scopes, err := carry.CurrentFundingCarryRecoveryBinding()
	if err != nil {
		return nil, storage.FundingCarryRuntimeGeneration{}, fmt.Errorf("retained FundingCarry generation binding is unavailable: %w", err)
	}
	if boundStore != store || botID != episode.Bot || generation.ID() == "" {
		return nil, storage.FundingCarryRuntimeGeneration{}, fmt.Errorf("retained FundingCarry generation binding does not match production service")
	}
	sort.Strings(scopes)
	episodeScopes, err := normalizeFundingRecoveryScopes(episode.AccountScopes)
	if err != nil || !sameFundingRecoveryScopes(scopes, episodeScopes) {
		return nil, storage.FundingCarryRuntimeGeneration{}, storage.ErrFundingRecoveryGenerationScopeMismatch
	}
	if episode.OwnerGeneration != generation.ID() {
		if !takeover {
			return nil, storage.FundingCarryRuntimeGeneration{}, fmt.Errorf("episode has not been explicitly taken over by the retained generation")
		}
	}
	hold, held, err := store.GetRecoveryHold(ctx, episode.ID, episode.RecoveryGeneration)
	if err != nil {
		return nil, storage.FundingCarryRuntimeGeneration{}, fmt.Errorf("read durable FundingCarry recovery hold: %w", err)
	}
	if !held || hold.State != "held" || hold.Bot != episode.Bot || hold.Strategy != episode.Strategy || hold.Generation != episode.RecoveryGeneration {
		return nil, storage.FundingCarryRuntimeGeneration{}, fmt.Errorf("durable FundingCarry recovery hold is not active")
	}
	holdScopes, err := normalizeFundingRecoveryScopes(hold.AccountScopes)
	if err != nil || !sameFundingRecoveryScopes(holdScopes, episodeScopes) {
		return nil, storage.FundingCarryRuntimeGeneration{}, storage.ErrFundingRecoveryGenerationScopeMismatch
	}
	return carry, generation, nil
}

// resolveHeldFundingCarryStrategyForFill is the sole production path that may
// operate on a RecoveryProjected episode. It does not claim/rotate ownership;
// the standard resolver still verifies the exact generation, account scopes,
// reconciliation-only runtime, and durable active hold.
func resolveHeldFundingCarryStrategyForFill(ctx context.Context, store *storage.SQLStorage, bots *BotManager, episode accounting.RecoveryEpisode, allowLedgerCommitted bool) (*strategy.FundingCarryStrategy, storage.FundingCarryRuntimeGeneration, error) {
	stateAllowed := episode.State == accounting.RecoveryBlocked || episode.State == accounting.RecoveryProjected ||
		(allowLedgerCommitted && episode.State == accounting.RecoveryLedgerCommitted)
	if !stateAllowed {
		return nil, storage.FundingCarryRuntimeGeneration{}, fmt.Errorf("fill reconciliation requires a blocked, projected, or retryable ledger-committed episode")
	}
	blockedView := episode
	blockedView.State = accounting.RecoveryBlocked
	// The strict resolver proves that the retained runtime generation already
	// owns this episode and its exact account scopes/hold. A takeover audit is
	// required only when ownership was actually transferred; native current-owner
	// blocked episodes need no synthetic takeover operation.
	return resolveHeldFundingCarryStrategy(ctx, store, bots, blockedView, false)
}

func resolveHeldFundingCarryStrategyForRelease(ctx context.Context, store *storage.SQLStorage, bots *BotManager, episode accounting.RecoveryEpisode) (*strategy.FundingCarryStrategy, storage.FundingCarryRuntimeGeneration, error) {
	if episode.State == accounting.RecoveryProjected || episode.State == accounting.RecoveryReleasePending {
		blockedView := episode
		blockedView.State = accounting.RecoveryBlocked
		return resolveHeldFundingCarryStrategy(ctx, store, bots, blockedView, false)
	}
	if ctx == nil || store == nil || bots == nil || episode.State != accounting.RecoveryReleased || episode.Strategy != "funding_carry" {
		return nil, storage.FundingCarryRuntimeGeneration{}, errors.New("FundingCarry release requires a projected or previously released episode")
	}
	bot, found := bots.Get(episode.Bot)
	if !found || bot == nil || bot.Inner == nil || bot.stopTransitionPending() || !bot.Inner.fundingCarryReconciliationOnly || bot.Inner.StrategyManager == nil {
		return nil, storage.FundingCarryRuntimeGeneration{}, errors.New("retained FundingCarry reconciliation runtime is unavailable")
	}
	carry, ok := bot.Inner.StrategyManager.GetStrategy("funding_carry").(*strategy.FundingCarryStrategy)
	if !ok || carry == nil {
		return nil, storage.FundingCarryRuntimeGeneration{}, errors.New("retained FundingCarry strategy is unavailable")
	}
	boundStore, generation, botID, scopes, err := carry.CurrentFundingCarryRecoveryBinding()
	if err != nil || boundStore != store || botID != episode.Bot || generation.ID() != episode.OwnerGeneration {
		return nil, storage.FundingCarryRuntimeGeneration{}, errors.Join(errors.New("released episode runtime owner mismatch"), err)
	}
	episodeScopes, err := normalizeFundingRecoveryScopes(episode.AccountScopes)
	if err != nil {
		return nil, storage.FundingCarryRuntimeGeneration{}, err
	}
	sort.Strings(scopes)
	if !sameFundingRecoveryScopes(scopes, episodeScopes) {
		return nil, storage.FundingCarryRuntimeGeneration{}, storage.ErrFundingRecoveryGenerationScopeMismatch
	}
	return carry, generation, nil
}

func fundingCarryProjectionMarkerReadbackMatches(payload, episodeID, operationID string, outbox accounting.RecoveryOutbox) bool {
	var state struct {
		RecoveryProjectionSchema  int `json:"recovery_projection_schema"`
		RecoveryProjectionMarkers []struct {
			EpisodeID               string `json:"episode_id"`
			EpisodeRevision         uint64 `json:"episode_revision"`
			OperationID             string `json:"operation_id"`
			ProjectionHash          string `json:"projection_hash"`
			ExpectedRuntimeRevision string `json:"expected_runtime_revision"`
		} `json:"recovery_projection_markers"`
	}
	if json.Unmarshal([]byte(payload), &state) != nil || state.RecoveryProjectionSchema != 1 {
		return false
	}
	for _, marker := range state.RecoveryProjectionMarkers {
		if marker.EpisodeID == episodeID && marker.OperationID == operationID && marker.EpisodeRevision == outbox.TargetEpisodeRevision &&
			marker.ProjectionHash == outbox.ProjectionHash && marker.ExpectedRuntimeRevision == outbox.ExpectedRuntimeRevision {
			return true
		}
	}
	return false
}

func normalizeFundingRecoveryScopes(scopes []string) ([]string, error) {
	if len(scopes) == 0 {
		return nil, fmt.Errorf("funding recovery runtime has no account scopes")
	}
	result := append([]string(nil), scopes...)
	sort.Strings(result)
	for i := 1; i < len(result); i++ {
		if result[i] == result[i-1] {
			return nil, fmt.Errorf("funding recovery runtime has duplicate account scopes")
		}
	}
	return result, nil
}

func sameFundingRecoveryScopes(left, right []string) bool {
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

var _ storage.FundingRecoveryStore = (*storage.SQLStorage)(nil)
