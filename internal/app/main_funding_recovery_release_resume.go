package app

import (
	"context"
	"errors"
	"fmt"
	"sort"

	"quantmesh/accounting"
	"quantmesh/execution"
	"quantmesh/storage"
	"quantmesh/strategy"
	"quantmesh/web"
)

// 續辦只沿用已提交的釋放授權；普通 takeover 的狀態限制保持不變。
func wireFundingRecoveryReleaseResume(service *web.OrderReconciliationService, store *storage.SQLStorage, bots *BotManager) {
	service.RecoveryReleaseResumePreview = func(ctx context.Context, episodeID, releaseOperationID string) (web.FundingRecoveryReleaseResumePreview, error) {
		episode, found, err := store.GetRecoveryEpisode(ctx, episodeID)
		if err != nil || !found {
			return web.FundingRecoveryReleaseResumePreview{}, errors.Join(storage.ErrFundingRecoveryNotFound, err)
		}
		var preview web.FundingRecoveryReleaseResumePreview
		err = bots.WithBotRuntimeOperationContext(ctx, episode.Bot, func(operationCtx context.Context, _ *BotRuntime) error {
			carry, generation, resolveErr := resolveFundingCarryReleaseResumeRuntime(operationCtx, store, bots, episode)
			if resolveErr != nil {
				return resolveErr
			}
			durable, previewErr := store.PreviewRecoveryReleaseResume(operationCtx, generation, episodeID, releaseOperationID)
			if previewErr != nil {
				return previewErr
			}
			preview = web.FundingRecoveryReleaseResumePreview{FundingRecoveryReleaseResumePreview: durable,
				GateSources: carry.RecoveryOpeningGateSources(), RecoveryGateSource: execution.RecoveryEpisodeGateSource(episodeID, durable.Episode.RecoveryGeneration)}
			return nil
		})
		return preview, err
	}
	service.RecoveryReleaseResumeApply = func(ctx context.Context, auth accounting.RecoveryReleaseResumeAuthorization) (web.FundingRecoveryReleaseResumeReadback, error) {
		if err := auth.Validate(); err != nil {
			return web.FundingRecoveryReleaseResumeReadback{}, err
		}
		episode, found, err := store.GetRecoveryEpisode(ctx, auth.EpisodeID)
		if err != nil || !found {
			return web.FundingRecoveryReleaseResumeReadback{}, errors.Join(storage.ErrFundingRecoveryNotFound, err)
		}
		var readback web.FundingRecoveryReleaseResumeReadback
		err = bots.WithBotRuntimeOperationContext(ctx, episode.Bot, func(operationCtx context.Context, _ *BotRuntime) error {
			current, found, readErr := store.GetRecoveryEpisode(operationCtx, auth.EpisodeID)
			if readErr != nil || !found {
				return errors.Join(storage.ErrFundingRecoveryNotFound, readErr)
			}
			carry, generation, resolveErr := resolveFundingCarryReleaseResumeRuntime(operationCtx, store, bots, current)
			if resolveErr != nil {
				return resolveErr
			}
			_, hold, _, replayed, resumeErr := store.ResumeRecoveryRelease(operationCtx, generation, auth)
			if resumeErr != nil {
				return resumeErr
			}
			if gateErr := carry.ReleaseFundingRecoveryGate(operationCtx, auth.EpisodeID); gateErr != nil {
				return &web.FundingRecoveryReleaseStateError{HoldReleased: hold.State == "released", Cause: fmt.Errorf("resume committed; exact recovery Gate cleanup failed: %w", gateErr)}
			}
			updated, found, episodeErr := store.GetRecoveryEpisode(operationCtx, auth.EpisodeID)
			finalHold, holdFound, holdErr := store.GetRecoveryHold(operationCtx, auth.EpisodeID, auth.RecoveryGeneration)
			receipt, receiptFound, receiptErr := store.GetRecoveryReceipt(operationCtx, auth.EpisodeID, auth.OperationID)
			releaseReceipt, releaseFound, releaseErr := store.GetRecoveryReceipt(operationCtx, auth.EpisodeID, auth.ReleaseOperationID)
			sources := carry.RecoveryOpeningGateSources()
			source := execution.RecoveryEpisodeGateSource(auth.EpisodeID, auth.RecoveryGeneration)
			cleared := !containsFundingRecoveryGateSource(sources, source)
			if episodeErr != nil || holdErr != nil || receiptErr != nil || releaseErr != nil || !found || !holdFound || !receiptFound || !releaseFound ||
				updated.State != accounting.RecoveryReleased || updated.OwnerGeneration != generation.ID() || updated.RecoveryGeneration != auth.RecoveryGeneration ||
				finalHold.State != "released" || finalHold.ReleasedByOperation != auth.ReleaseOperationID ||
				receipt.Actor != auth.Actor || receipt.AuditRef != auth.AuditRef || receipt.OwnerGeneration != generation.ID() || receipt.EvidenceHash != auth.EvidenceHash ||
				receipt.ReleaseOperationID != auth.ReleaseOperationID || receipt.ReleaseRequestHash != auth.ReleaseRequestHash ||
				releaseReceipt.RequestHash != auth.ReleaseRequestHash || releaseReceipt.State != accounting.RecoveryReleased || !cleared {
				return &web.FundingRecoveryReleaseStateError{HoldReleased: hold.State == "released", GateCleared: cleared,
					Cause: errors.Join(errors.New("resumed FundingCarry release readback mismatch"), episodeErr, holdErr, receiptErr, releaseErr)}
			}
			readback = web.FundingRecoveryReleaseResumeReadback{FundingRecoveryReleaseReadback: web.FundingRecoveryReleaseReadback{
				Episode: updated, Hold: finalHold, Receipt: receipt, GateSources: sources, ReleasedSource: source, Verified: true, ExecutionReplayed: false,
			}, ReleaseReceipt: releaseReceipt, Replayed: replayed}
			return nil
		})
		return readback, err
	}
}

// 專用 resolver 不接管 owner；只有後續受 generation fence 與 CAS 保護的管理員事務可以接管。
func resolveFundingCarryReleaseResumeRuntime(ctx context.Context, store *storage.SQLStorage, bots *BotManager, episode accounting.RecoveryEpisode) (*strategy.FundingCarryStrategy, storage.FundingCarryRuntimeGeneration, error) {
	if ctx == nil || store == nil || bots == nil || episode.ID == "" || episode.Bot == "" || episode.Strategy != "funding_carry" ||
		(episode.State != accounting.RecoveryReleasePending && episode.State != accounting.RecoveryReleased) {
		return nil, storage.FundingCarryRuntimeGeneration{}, accounting.ErrRecoveryConflict
	}
	bot, found := bots.Get(episode.Bot)
	if !found || bot == nil || bot.Inner == nil || bot.stopTransitionPending() || !bot.Inner.fundingCarryReconciliationOnly || bot.Inner.StrategyManager == nil {
		return nil, storage.FundingCarryRuntimeGeneration{}, errors.New("retained FundingCarry reconciliation runtime is unavailable for release resume")
	}
	carry, ok := bot.Inner.StrategyManager.GetStrategy("funding_carry").(*strategy.FundingCarryStrategy)
	if !ok || carry == nil || carry.IsRunning() {
		return nil, storage.FundingCarryRuntimeGeneration{}, errors.New("stopped FundingCarry strategy is unavailable")
	}
	boundStore, generation, botID, scopes, err := carry.CurrentFundingCarryRecoveryBinding()
	if err != nil || boundStore != store || botID != episode.Bot || generation.ID() == "" {
		return nil, storage.FundingCarryRuntimeGeneration{}, errors.Join(storage.ErrFundingRecoveryGenerationScopeMismatch, err)
	}
	sort.Strings(scopes)
	episodeScopes, err := normalizeFundingRecoveryScopes(episode.AccountScopes)
	if err != nil || !sameFundingRecoveryScopes(scopes, episodeScopes) {
		return nil, storage.FundingCarryRuntimeGeneration{}, storage.ErrFundingRecoveryGenerationScopeMismatch
	}
	hold, found, err := store.GetRecoveryHold(ctx, episode.ID, episode.RecoveryGeneration)
	if err != nil || !found || hold.Bot != episode.Bot || hold.Strategy != episode.Strategy || hold.Generation != episode.RecoveryGeneration ||
		(episode.State == accounting.RecoveryReleasePending && hold.State != "held") || (episode.State == accounting.RecoveryReleased && hold.State != "released") {
		return nil, storage.FundingCarryRuntimeGeneration{}, errors.Join(accounting.ErrRecoveryConflict, err)
	}
	holdScopes, err := normalizeFundingRecoveryScopes(hold.AccountScopes)
	if err != nil || !sameFundingRecoveryScopes(scopes, holdScopes) {
		return nil, storage.FundingCarryRuntimeGeneration{}, storage.ErrFundingRecoveryGenerationScopeMismatch
	}
	return carry, generation, nil
}
