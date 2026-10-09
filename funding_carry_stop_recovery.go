package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"

	"quantmesh/config"
	"quantmesh/exchange"
	"quantmesh/exchange/binance"
	"quantmesh/execution"
	"quantmesh/lock"
	"quantmesh/storage"
	"quantmesh/strategy"
)

func (bm *BotManager) recoverFundingCarryStop(ctx context.Context, cfg *config.Config, botCfg config.BotConfig, journal *botStopJournal) error {
	if bm == nil || ctx == nil || cfg == nil || journal == nil || journal.State == nil {
		return errors.New("FundingCarry stop recovery requires manager, context, configuration, and journal")
	}
	if journal.Complete || journal.State.BotID == "" || config.BotIDOrGenerate(botCfg) != journal.State.BotID {
		return errors.New("FundingCarry stop recovery identity or journal phase is invalid")
	}
	symCfg := config.BotConfigToSymbolConfig(botCfg)
	if symCfg.GetMarketType() != config.MarketTypeFundingCarry || !strings.EqualFold(symCfg.Exchange, "binance") || strings.TrimSpace(symCfg.Symbol) == "" {
		return errors.New("incomplete stop journal is not an eligible Binance FundingCarry Bot")
	}
	claims, err := fundingCarryClaimsFromStopJournal(cfg, botCfg, journal)
	if err != nil {
		return err
	}
	if bm.storageService == nil || bm.storageService.GetStorage() == nil {
		return errors.New("FundingCarry stop recovery requires durable strategy and capital storage")
	}
	capitalStore, ok := bm.storageService.GetStorage().(storage.AccountWalletCapitalReservationStore)
	if !ok {
		return errors.New("storage does not support generation-scoped account capital release")
	}
	if bm.distributedLock == nil {
		return errors.New("FundingCarry stop recovery requires distributed ownership fencing")
	}
	if _, disabled := bm.distributedLock.(*lock.NopLock); disabled {
		return errors.New("FundingCarry stop recovery refuses a no-op distributed lock")
	}
	leases, err := bm.fundingCarryRecoveryLeases(ctx, journal.State.BotID, cfg, symCfg)
	if err != nil {
		return err
	}
	guard := func() error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := validateRuntimeOwnershipLeases(ctx, leases); err != nil {
			return fmt.Errorf("FundingCarry stop recovery lost current runtime ownership: %w", err)
		}
		return nil
	}
	if err := guard(); err != nil {
		return err
	}
	if strings.TrimSpace(cfg.Exchanges[symCfg.Exchange].APIKey) == "" || strings.TrimSpace(cfg.Exchanges[symCfg.Exchange].SecretKey) == "" {
		return errors.New("FundingCarry recovery account credentials are incomplete")
	}
	credential := cfg.Exchanges[symCfg.Exchange]
	futuresEvidence, err := binance.NewBinanceFuturesStopEvidenceAdapter(ctx, credential.APIKey, credential.SecretKey, credential.Testnet, symCfg.Symbol)
	if err != nil {
		return fmt.Errorf("construct read-only futures stop evidence: %w", err)
	}
	spotEvidence, err := binance.NewBinanceSpotStopEvidenceAdapter(ctx, credential.APIKey, credential.SecretKey, credential.Testnet, symCfg.Symbol)
	if err != nil {
		return fmt.Errorf("construct read-only spot stop evidence: %w", err)
	}
	marginEvidence, err := binance.NewBinanceSpotMarginStopEvidenceAdapter(ctx, credential.APIKey, credential.SecretKey, credential.Testnet, symCfg.Symbol)
	if err != nil {
		return fmt.Errorf("construct read-only margin stop evidence: %w", err)
	}
	if !futuresEvidence.IsStopEvidenceOnly() || !spotEvidence.IsStopEvidenceOnly() || !marginEvidence.IsStopEvidenceOnly() {
		return errors.New("FundingCarry stop recovery refused an exchange adapter without mutation guards")
	}
	if err := guard(); err != nil {
		return err
	}
	marginExchange, ok := any(marginEvidence).(exchange.ISpotMarginExchange)
	if !ok {
		return errors.New("Binance margin stop evidence lacks the required margin exchange contract")
	}
	localCfg := *cfg
	localCfg.Trading.BotID = journal.State.BotID
	localCfg.Trading.Symbol = symCfg.Symbol
	localCfg.Trading.MarketType = config.MarketTypeFundingCarry
	mergeFundingCarryStrategyConfig(&localCfg, symCfg)
	futuresExchange := exchange.NewBinanceFuturesEvidenceExchange(futuresEvidence)
	spotExchange := exchange.NewBinanceSpotEvidenceExchange(spotEvidence)
	if futuresExchange == nil || spotExchange == nil {
		return errors.New("FundingCarry stop recovery could not adapt read-only Binance evidence exchanges")
	}
	carry := strategy.NewFundingCarryStrategy("funding_carry", &localCfg, symCfg, futuresExchange, spotExchange, marginExchange, localCfg.Strategies.Configs["funding_carry"].Config)
	accountScope := equityAccountScopeID(symCfg.Exchange, credential)
	if err := carry.SetMarginAccountScope(accountScope); err != nil {
		return err
	}
	if err := carry.SetAccountWalletCoordinationLock(bm.distributedLock, "funding_carry_wallet:"+accountScope); err != nil {
		return err
	}
	carry.SetRuntimeStateStore(&strategyRuntimeStateAdapter{storageService: bm.storageService, botID: journal.State.BotID})
	carry.SetOpeningGate(&execution.OpeningGate{})

	if err := carry.ReconcilePersistedStoppedFlat(ctx, guard); err != nil {
		return fmt.Errorf("verify persisted FundingCarry stopped flat state: %w", err)
	}
	if err := verifyAndReleaseAccountWalletCapitalGuarded(ctx, capitalStore, journal.State.BotID, claims,
		func(verifyCtx context.Context) error { return carry.VerifyPersistedStoppedFlat(verifyCtx, guard) }, guard); err != nil {
		return fmt.Errorf("release exact stopped FundingCarry capital claims: %w", err)
	}
	if err := guard(); err != nil {
		return err
	}
	var releaseErr error
	for i := len(leases) - 1; i >= 0; i-- {
		if err := guard(); err != nil {
			releaseErr = errors.Join(releaseErr, err)
			break
		}
		released, err := releaseRuntimeOwnershipLeaseAfterVerifiedStop(leases[i], nil, "")
		if err == nil && !released {
			err = errors.New("FundingCarry runtime ownership lease remains held")
		}
		releaseErr = errors.Join(releaseErr, err)
	}
	if releaseErr != nil {
		return fmt.Errorf("release verified FundingCarry runtime ownership: %w", releaseErr)
	}
	bm.stopRecoveryMu.Lock()
	delete(bm.stopRecoveryLeases, journal.State.BotID)
	bm.stopRecoveryMu.Unlock()
	return nil
}

func fundingCarryClaimsFromStopJournal(cfg *config.Config, botCfg config.BotConfig, journal *botStopJournal) ([]storage.AccountWalletCapitalClaim, error) {
	if cfg == nil || journal == nil || journal.State == nil || len(journal.CapitalClaims) < 2 {
		return nil, errors.New("FundingCarry stop journal lacks exact futures and spot capital claims")
	}
	exchangeName := strings.TrimSpace(botCfg.Exchange)
	symbol := strings.ToUpper(strings.TrimSpace(botCfg.Symbol))
	seen := make(map[string]struct{}, len(journal.CapitalClaims))
	claims := make([]storage.AccountWalletCapitalClaim, 0, len(journal.CapitalClaims))
	hasFutures, hasSpot := false, false
	for _, persisted := range journal.CapitalClaims {
		market := strings.ToLower(strings.TrimSpace(persisted.Market))
		quote := strings.ToUpper(strings.TrimSpace(persisted.QuoteAsset))
		if !strings.EqualFold(strings.TrimSpace(persisted.Exchange), exchangeName) || strings.ToUpper(strings.TrimSpace(persisted.Symbol)) != symbol || quote != "USDT" {
			return nil, errors.New("FundingCarry stop claim does not match the current exchange, symbol, and quote scope")
		}
		if market != "futures" && market != "spot" && market != "spot_margin" {
			return nil, fmt.Errorf("unsupported FundingCarry stop claim market %q", market)
		}
		if _, duplicate := seen[market]; duplicate {
			return nil, fmt.Errorf("duplicate FundingCarry stop claim for %s", market)
		}
		seen[market] = struct{}{}
		expectedWallet, err := accountWalletCapitalKey(cfg, exchangeName, market, quote)
		if err != nil {
			return nil, err
		}
		if persisted.WalletKey != expectedWallet || math.IsNaN(persisted.Amount) || math.IsInf(persisted.Amount, 0) || persisted.Amount <= 0 {
			return nil, fmt.Errorf("FundingCarry stop claim wallet generation does not match current account configuration")
		}
		hasFutures = hasFutures || market == "futures"
		hasSpot = hasSpot || market == "spot"
		claims = append(claims, storage.AccountWalletCapitalClaim{
			WalletKey: persisted.WalletKey, ReservationToken: persisted.ReservationToken, Amount: persisted.Amount,
			Exchange: exchangeName, Market: market, QuoteAsset: quote, Symbol: symbol,
		})
	}
	if !hasFutures || !hasSpot {
		return nil, errors.New("FundingCarry stop journal must retain both futures and spot capital generations")
	}
	return claims, nil
}

func (bm *BotManager) fundingCarryRecoveryLeases(ctx context.Context, botID string, cfg *config.Config, symCfg config.SymbolConfig) ([]*runtimeOwnershipLease, error) {
	bm.stopRecoveryMu.Lock()
	if leases := bm.stopRecoveryLeases[botID]; len(leases) > 0 {
		bm.stopRecoveryMu.Unlock()
		if fundingCarryRuntimeOwnershipLeaseLost(leases) {
			return nil, errors.New("retained FundingCarry recovery lease was lost; refusing takeover")
		}
		return leases, nil
	}
	bm.stopRecoveryMu.Unlock()
	leases, err := acquireFundingCarryRuntimeOwnershipLeases(ctx, bm.distributedLock, cfg, symCfg.Exchange, symCfg.Symbol, true, nil)
	if err != nil {
		return nil, err
	}
	bm.stopRecoveryMu.Lock()
	if bm.stopRecoveryLeases == nil {
		bm.stopRecoveryLeases = make(map[string][]*runtimeOwnershipLease)
	}
	bm.stopRecoveryLeases[botID] = leases
	bm.stopRecoveryMu.Unlock()
	return leases, nil
}
