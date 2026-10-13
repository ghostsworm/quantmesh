package strategy

import (
	"context"
	"fmt"
	"sort"

	"quantmesh/execution"
	"quantmesh/storage"
)

// Only produced after complete historical accounting import and successful
// wallet cleanup. It is not proof of current inventory or permission to trade.
type FundingCarryReconciliationRequiredError struct{ message string }

func (e *FundingCarryReconciliationRequiredError) Error() string { return e.message }

// FundingCarryDurableRecoveryRequiredError is emitted only when every retained
// hold is backed by an exact, valid nonterminal episode. It grants no trading
// or gate-release capability; it only admits a stopped reconciliation runtime.
type FundingCarryDurableRecoveryRequiredError struct {
	Episodes []FundingCarryDurableRecoveryEpisode
}

type FundingCarryDurableRecoveryEpisode struct {
	ID         string
	Generation uint64
}

func (e *FundingCarryDurableRecoveryRequiredError) Error() string {
	return fmt.Sprintf("%d durable FundingCarry recovery hold(s) require human reconciliation", len(e.Episodes))
}

func (s *FundingCarryStrategy) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.started && s.ctx != nil && s.ctx.Err() == nil
}

func (s *FundingCarryStrategy) VerifyRemainingReconciliation(ctx context.Context, gate *execution.OpeningGate) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if err := s.verifyDebtCommitLocked(ctx); err != nil {
		return err
	}
	if gate == nil || s.openingGate != gate || s.started || !s.strategySpotKnown || !s.unownedExposure || !s.intentInFlight ||
		s.runtimeStateErr != nil || s.marginRepayIntent != nil || s.marginCoverIntent != nil || s.marginAccountScope == "" {
		return fmt.Errorf("funding_carry is not a verified stopped remaining-asset reconciliation")
	}
	if err := validateFundingCarryDebtPrincipalBalance(s.runtimeStateSnapshotLocked()); err != nil {
		return err
	}
	remaining, err := fundingCarryCoverRemaining(s.coverRemainingStateLocked(), s.spot.GetBaseAsset())
	if err != nil {
		return err
	}
	if remaining.Sign() <= 0 && !(s.marginDebt > 0 && hasTerminalPartialFundingCarryCover(s.marginCoverOrders)) {
		return fmt.Errorf("funding_carry has no verified remaining-asset reconciliation")
	}
	return nil
}

// VerifyDurableRecovery re-reads the exact nonterminal episode, active hold and
// any persisted attempts after startup producers have been quiesced. It never
// clears a Gate source.
func (s *FundingCarryStrategy) VerifyDurableRecovery(ctx context.Context, gate *execution.OpeningGate, expected []FundingCarryDurableRecoveryEpisode) error {
	if ctx == nil || gate == nil || len(expected) == 0 {
		return fmt.Errorf("durable FundingCarry recovery verification requires context, gate and episodes")
	}
	s.mu.RLock()
	if s.openingGate != gate || s.started || s.ctx != nil || s.runtimeStateErr != nil || s.marginRepayIntent != nil || s.marginCoverIntent != nil {
		s.mu.RUnlock()
		return fmt.Errorf("FundingCarry runtime is not quiesced for durable recovery")
	}
	if err := s.verifyDebtCommitLocked(ctx); err != nil {
		s.mu.RUnlock()
		return err
	}
	s.mu.RUnlock()

	store, generation, botID, scopes, err := s.CurrentFundingCarryRecoveryBinding()
	if err != nil || nilFundingRecoveryStore(store) || generation.ID() == "" || botID == "" {
		return ErrFundingCarryDurableRecoveryUnavailable
	}
	currentScopes, err := normalizeFundingCarryRecoveryScopes(scopes)
	if err != nil {
		return err
	}
	want := append([]FundingCarryDurableRecoveryEpisode(nil), expected...)
	sort.Slice(want, func(i, j int) bool { return want[i].ID < want[j].ID })
	for i, ref := range want {
		if ref.ID == "" || ref.Generation == 0 || (i > 0 && want[i-1].ID == ref.ID) {
			return fmt.Errorf("invalid or duplicate durable recovery episode reference")
		}
		episode, found, err := store.GetRecoveryEpisode(ctx, ref.ID)
		if err != nil || !found || !validFundingCarryRetainableEpisode(episode) || episode.Bot != botID || episode.Strategy != "funding_carry" || episode.RecoveryGeneration != ref.Generation {
			return fmt.Errorf("durable recovery episode identity changed")
		}
		episodeScopes, err := normalizeFundingCarryRecoveryScopes(episode.AccountScopes)
		if err != nil || !equalFundingCarryRecoveryScopes(currentScopes, episodeScopes) {
			return storage.ErrFundingRecoveryGenerationScopeMismatch
		}
		hold, held, err := store.GetRecoveryHold(ctx, episode.ID, episode.RecoveryGeneration)
		if err != nil || !held || hold.State != "held" || hold.Bot != botID || hold.Strategy != "funding_carry" || hold.Generation != ref.Generation {
			return fmt.Errorf("durable recovery hold is no longer active")
		}
		holdScopes, err := normalizeFundingCarryRecoveryScopes(hold.AccountScopes)
		if err != nil || !equalFundingCarryRecoveryScopes(currentScopes, holdScopes) {
			return storage.ErrFundingRecoveryGenerationScopeMismatch
		}
		attempts, err := store.ListRecoverySubmissionAttempts(ctx, episode.ID)
		if err != nil {
			return err
		}
		for _, attempt := range attempts {
			if attempt.Validate() != nil || attempt.EpisodeID != episode.ID || attempt.Generation != episode.RecoveryGeneration {
				return fmt.Errorf("durable recovery attempt identity is invalid")
			}
			if !containsFundingCarryRecoveryScope(currentScopes, attempt.AccountScope) {
				return storage.ErrFundingRecoveryGenerationScopeMismatch
			}
		}
		if !gate.HasBlock(execution.RecoveryEpisodeGateSource(episode.ID, episode.RecoveryGeneration)) {
			return fmt.Errorf("exact durable recovery hold or Gate source is missing")
		}
	}
	return nil
}
