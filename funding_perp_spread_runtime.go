package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"quantmesh/config"
	"quantmesh/event"
	"quantmesh/exchange"
	"quantmesh/execution"
	"quantmesh/lock"
	"quantmesh/logger"
	"quantmesh/monitor"
	"quantmesh/storage"
	"quantmesh/strategy"
)

// startFundingPerpSpreadSymbolRuntime 雙永续跨所資金費差專用運行時
func startFundingPerpSpreadSymbolRuntime(
	ctx context.Context,
	baseCfg *config.Config,
	symCfg config.SymbolConfig,
	eventBus *event.EventBus,
	storageService *storage.StorageService,
	distributedLock lock.DistributedLock,
	onRequestStop func(botID string),
) (*SymbolRuntime, error) {
	fp := symCfg.FundingPerpSpread
	if fp == nil {
		return nil, fmt.Errorf("funding_perp_spread 缺少 funding_perp_spread 配置")
	}
	if err := config.ValidateFundingPerpSpread(fp); err != nil {
		return nil, err
	}

	localCfg := *baseCfg
	botID := symCfg.ID
	if botID == "" {
		botID = config.GenerateBotIDFundingPerpSpread(fp)
	}
	localCfg.Trading.BotID = botID
	ctx = logger.WithBotID(ctx, botID)
	localCfg.Trading.Symbol = symCfg.Symbol
	localCfg.Trading.MarketType = config.MarketTypeFundingPerpSpread
	mergeFundingPerpSpreadStrategyConfig(&localCfg, symCfg)

	legAEx, err := exchange.NewExchange(&localCfg, strings.TrimSpace(fp.LegA.Exchange), strings.TrimSpace(fp.LegA.Symbol), "futures")
	if err != nil {
		return nil, fmt.Errorf("創建 leg_a 合約連線失敗: %w", err)
	}
	legBEx, err := exchange.NewExchange(&localCfg, strings.TrimSpace(fp.LegB.Exchange), strings.TrimSpace(fp.LegB.Symbol), "futures")
	if err != nil {
		return nil, fmt.Errorf("創建 leg_b 合約連線失敗: %w", err)
	}
	baseA, baseB := legAEx.GetBaseAsset(), legBEx.GetBaseAsset()
	if err := validateFundingPerpSpreadLegBases(baseA, baseB); err != nil {
		return nil, err
	}
	const spreadCapitalAsset = "USDT"
	quoteA := strings.ToUpper(strings.TrimSpace(legAEx.GetQuoteAsset()))
	quoteB := strings.ToUpper(strings.TrimSpace(legBEx.GetQuoteAsset()))
	if quoteA != spreadCapitalAsset || quoteB != spreadCapitalAsset {
		return nil, fmt.Errorf("funding_perp_spread currently requires USDT-quoted legs; got %q and %q", quoteA, quoteB)
	}
	requestedCapital := symCfg.TotalAllocatedCapital
	if requestedCapital <= 0 {
		requestedCapital = symCfg.OrderQuantity
	}
	var legBBalance float64
	balanceCtx, cancelBalance := context.WithTimeout(ctx, 10*time.Second)
	legABalance, balanceErr := legAEx.GetBalance(balanceCtx, spreadCapitalAsset)
	if balanceErr == nil {
		legBBalance, balanceErr = legBEx.GetBalance(balanceCtx, spreadCapitalAsset)
		if balanceErr == nil {
			verifiedCapital, capErr := capTwoLegStrategyCapitalLimit(requestedCapital, legABalance, legBBalance)
			if capErr != nil {
				balanceErr = capErr
			} else {
				walletCandidate := symCfg
				walletCandidate.TotalAllocatedCapital = verifiedCapital
				for _, wallet := range []struct {
					exchange string
					balance  float64
				}{{exchange: fp.LegA.Exchange, balance: legABalance}, {exchange: fp.LegB.Exchange, balance: legBBalance}} {
					allocated, allocationErr := configuredAccountWalletCapitalForQuote(baseCfg, walletCandidate, wallet.exchange, "futures", spreadCapitalAsset)
					if allocationErr != nil {
						balanceErr = fmt.Errorf("calculate configured futures-wallet capital for %s: %w", wallet.exchange, allocationErr)
						break
					}
					if allocated > wallet.balance {
						balanceErr = fmt.Errorf("configured futures-wallet capital %.2f USDT exceeds available balance %.2f USDT on %s", allocated, wallet.balance, wallet.exchange)
						break
					}
				}
			}
			if balanceErr == nil {
				if verifiedCapital < requestedCapital {
					logger.WarnCtx(ctx, "funding_perp_spread budget capped from %.2f to %.2f USDT by verified leg balances", requestedCapital, verifiedCapital)
				}
				symCfg.TotalAllocatedCapital = verifiedCapital
			}
		}
	}
	cancelBalance()
	if balanceErr != nil {
		return nil, fmt.Errorf("verify funding_perp_spread USDT balance on both legs: %w", balanceErr)
	}

	totalCap := symCfg.TotalAllocatedCapital
	strategyManager := strategy.NewStrategyManager(&localCfg, totalCap)
	if eventBus != nil {
		strategyManager.SetEventBus(eventBus)
	}
	var stratCfg map[string]interface{}
	for _, si := range symCfg.Strategies {
		if si.Type == "funding_perp_spread" {
			stratCfg = si.Config
			break
		}
	}
	openingGate := &execution.OpeningGate{}
	if localCfg.Trading.OpenPositionControl.PauseOpening || (localCfg.Trading.OpenPositionControl.BotRiskControl != nil && localCfg.Trading.OpenPositionControl.BotRiskControl.PauseOpening) {
		openingGate.Block("manual")
	}
	st := strategy.NewFundingPerpSpreadStrategy("funding_perp_spread", &localCfg, symCfg, legAEx, legBEx, fp, stratCfg)
	st.SetOpeningGate(openingGate)
	stateBotID := fundingPerpSpreadStateScope(botID, baseCfg, fp)
	st.SetRuntimeStateStore(&strategyRuntimeStateAdapter{storageService: storageService, botID: stateBotID})
	st.SetCoordinationLock(distributedLock)
	ownershipLeases, err := acquireFundingPerpSpreadRuntimeOwnershipLeases(ctx, distributedLock, baseCfg, fp, func(leaseErr error) {
		openingGate.Block("runtime_ownership_unverified")
		if stateErr := st.MarkOwnershipUnverified(); stateErr != nil {
			logger.ErrorCtx(ctx, "[%s] 運行所有權丟失後持久化阻斷狀態失敗: %v", botID, stateErr)
		}
		logger.ErrorCtx(ctx, "[%s] funding_perp_spread 雙腿運行所有權租約丟失，已阻斷策略動作: %v", botID, leaseErr)
	})
	if err != nil {
		return nil, fmt.Errorf("acquire funding_perp_spread two-leg runtime ownership: %w", err)
	}
	ownershipLeasesTransferred := false
	defer func() {
		if ownershipLeasesTransferred {
			return
		}
		if releaseErr := releaseFundingPerpSpreadRuntimeOwnershipLeases(ownershipLeases); releaseErr != nil {
			logger.WarnCtx(ctx, "[%s] 初始化失敗後釋放雙腿運行所有權租約失敗: %v", botID, releaseErr)
		}
	}()
	if distributedLock == nil {
		return nil, fmt.Errorf("funding_perp_spread requires its configured leg coordination lock")
	}
	claims, err := fundingPerpSpreadCapitalClaims(baseCfg, fp, totalCap, legABalance, legBBalance)
	if err != nil {
		return nil, fmt.Errorf("build funding_perp_spread wallet capital claims: %w", err)
	}
	if err := reserveAccountWalletCapital(ctx, baseCfg, storageService, distributedLock, botID, claims); err != nil {
		return nil, fmt.Errorf("reserve funding_perp_spread wallet capital: %w", err)
	}
	reservationStore, ok := storageService.GetStorage().(storage.AccountWalletCapitalReservationStore)
	if !ok {
		return nil, fmt.Errorf("funding_perp_spread account wallet reservation storage disappeared after claim")
	}
	runtimeOwnsReservation := false
	defer func() {
		if runtimeOwnsReservation {
			return
		}
		releaseErr := verifyAndReleaseFundingPerpSpreadCapital(context.Background(), reservationStore, botID, claims, st.VerifyFlat, ownershipLeases)
		if releaseErr != nil {
			logger.ErrorCtx(ctx, "[%s] 啟動失敗後釋放已核實平倉的資金預留失敗: %v", botID, releaseErr)
		}
	}()

	// 價格監控掛在 leg_a（主顯示）
	priceMonitor := monitor.NewPriceMonitor(
		legAEx,
		fp.LegA.Symbol,
		localCfg.Timing.PriceSendInterval,
	)
	if err := priceMonitor.Start(); err != nil {
		return nil, fmt.Errorf("價格流: %w", err)
	}
	priceMonitorTransferred := false
	defer func() {
		if !priceMonitorTransferred {
			priceMonitor.Stop()
		}
	}()
	pollInterval := time.Duration(localCfg.Timing.PricePollInterval) * time.Millisecond
	if pollInterval <= 0 {
		// 零值保护：避免配置未经校验时 time.Sleep(0) 退化为 CPU 空转
		pollInterval = 500 * time.Millisecond
	}
	for i := 0; i < 10; i++ {
		if priceMonitor.GetLastPrice() > 0 {
			break
		}
		time.Sleep(pollInterval)
	}
	if priceMonitor.GetLastPrice() <= 0 {
		return nil, fmt.Errorf("無法獲取初始價格（leg_a）")
	}

	strategyManager.RegisterStrategy("funding_perp_spread", st, 1.0, 0)
	if err := strategyManager.StartAll(); err != nil {
		return nil, err
	}
	if fundingPerpSpreadRuntimeOwnershipLeaseLost(ownershipLeases) {
		shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 20*time.Second)
		shutdownErr := st.PrepareShutdown(shutdownCtx)
		cancelShutdown()
		return nil, errors.Join(fmt.Errorf("funding_perp_spread runtime ownership was lost during startup"), shutdownErr)
	}

	accountID := ""
	if fp != nil {
		legAExchange := strings.TrimSpace(fp.LegA.Exchange)
		if exCfg, ok := baseCfg.Exchanges[legAExchange]; ok && strings.TrimSpace(exCfg.APIKey) != "" {
			accountID = equityAccountScopeID(legAExchange, exCfg)
		}
	}

	rt := &SymbolRuntime{
		Config:               symCfg,
		Exchange:             legAEx,
		PriceMonitor:         priceMonitor,
		StrategyManager:      strategyManager,
		EventBus:             eventBus,
		StorageService:       storageService,
		AccountID:            accountID,
		AccountScope:         stateBotID,
		AccountMarketType:    "futures",
		OpeningGate:          openingGate,
		SuperPositionManager: nil,
		ExchangeExecutor:     nil,
		ExecutorAdapter:      nil,
		ExchangeAdapter:      nil,
	}
	rt.PrepareShutdown = func(shutdownCtx context.Context, _ bool) error {
		return st.PrepareShutdown(shutdownCtx)
	}
	rt.CloseForShutdown = st.CloseForShutdown
	rt.VerifyShutdownClose = st.VerifyFlat

	stopRuntime := func() error {
		logger.InfoCtx(ctx, "⏹️ [%s] 停止雙永续跨所資金費運行時", botID)
		if fundingPerpSpreadRuntimeOwnershipLeaseLost(ownershipLeases) {
			openingGate.Block("runtime_ownership_unverified")
			shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 20*time.Second)
			shutdownErr := st.PrepareShutdown(shutdownCtx)
			cancelShutdown()
			if shutdownErr != nil {
				return fmt.Errorf("funding_perp_spread lost runtime ownership and could not quiesce safely: %w", shutdownErr)
			}
		} else if err := strategyManager.StopAllWithError(); err != nil {
			openingGate.Block("strategy_stop_unverified")
			return fmt.Errorf("funding_perp_spread stop/close is unverified: %w", err)
		}
		if releaseErr := verifyAndReleaseFundingPerpSpreadCapital(context.Background(), reservationStore, botID, claims, st.VerifyFlat, ownershipLeases); releaseErr != nil {
			openingGate.Block("strategy_stop_unverified")
			return fmt.Errorf("funding_perp_spread flatness, ownership, or capital reservation release is unverified: %w", releaseErr)
		}
		if leaseErr := releaseFundingPerpSpreadRuntimeOwnershipLeases(ownershipLeases); leaseErr != nil {
			logger.WarnCtx(ctx, "[%s] 已核實平倉，但釋放雙腿運行所有權租約失敗（租約將到期）: %v", botID, leaseErr)
		}
		openingGate.Unblock("capital_reservation_unverified")
		openingGate.Unblock("strategy_stop_unverified")
		if priceMonitor != nil {
			priceMonitor.Stop()
		}
		legAEx.StopOrderStream()
		legBEx.StopOrderStream()
		return nil
	}
	rt.StopWithError = stopRuntime
	rt.Stop = func() {
		if err := stopRuntime(); err != nil {
			logger.ErrorCtx(ctx, "[%s] 停止雙永續資金費運行時失敗，保留運行時供核賬/重試: %v", botID, err)
		}
	}
	runtimeOwnsReservation = true
	priceMonitorTransferred = true
	ownershipLeasesTransferred = true

	return rt, nil
}

func verifyAndReleaseFundingPerpSpreadCapital(ctx context.Context, store storage.AccountWalletCapitalReservationStore, botID string, claims []storage.AccountWalletCapitalClaim, verifyFlat func(context.Context) error, ownershipLeases []*runtimeOwnershipLease) error {
	return verifyAndReleaseAccountWalletCapitalGuarded(ctx, store, botID, claims, verifyFlat, func() error {
		if fundingPerpSpreadRuntimeOwnershipLeaseLost(ownershipLeases) {
			return fmt.Errorf("funding_perp_spread runtime ownership lease was lost")
		}
		return nil
	})
}

func fundingPerpSpreadRuntimeOwnershipScopes(cfg *config.Config, fp *config.FundingPerpSpreadConfig) ([]execution.IntentScope, error) {
	if cfg == nil || fp == nil {
		return nil, fmt.Errorf("funding_perp_spread ownership requires config and both legs")
	}
	byKey := make(map[string]execution.IntentScope, 2)
	for _, leg := range []config.FundingPerpLeg{fp.LegA, fp.LegB} {
		exchangeName := strings.TrimSpace(leg.Exchange)
		exchangeCfg, ok := cfg.Exchanges[exchangeName]
		if !ok || strings.TrimSpace(exchangeCfg.APIKey) == "" {
			return nil, fmt.Errorf("runtime ownership account identity unavailable for %s", exchangeName)
		}
		scope := runtimeOwnershipScope(equityAccountScopeID(exchangeName, exchangeCfg), exchangeName, "futures", leg.Symbol)
		key, err := scope.Key()
		if err != nil {
			return nil, fmt.Errorf("build %s futures ownership scope: %w", exchangeName, err)
		}
		byKey[key] = scope
	}
	keys := make([]string, 0, len(byKey))
	for key := range byKey {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	scopes := make([]execution.IntentScope, 0, len(keys))
	for _, key := range keys {
		scopes = append(scopes, byKey[key])
	}
	return scopes, nil
}

func acquireFundingPerpSpreadRuntimeOwnershipLeases(ctx context.Context, distributedLock lock.DistributedLock, cfg *config.Config, fp *config.FundingPerpSpreadConfig, onLost func(error)) ([]*runtimeOwnershipLease, error) {
	scopes, err := fundingPerpSpreadRuntimeOwnershipScopes(cfg, fp)
	if err != nil {
		return nil, err
	}
	leases := make([]*runtimeOwnershipLease, 0, len(scopes))
	for _, scope := range scopes {
		lease, err := acquireRuntimeOwnershipLease(ctx, distributedLock, scope, runtimeOwnershipLeaseTTL, onLost)
		if err != nil {
			return nil, errors.Join(err, releaseFundingPerpSpreadRuntimeOwnershipLeases(leases))
		}
		leases = append(leases, lease)
	}
	return leases, nil
}

func releaseFundingPerpSpreadRuntimeOwnershipLeases(leases []*runtimeOwnershipLease) error {
	var releaseErr error
	for i := len(leases) - 1; i >= 0; i-- {
		releaseErr = errors.Join(releaseErr, leases[i].Release())
	}
	return releaseErr
}

func fundingPerpSpreadRuntimeOwnershipLeaseLost(leases []*runtimeOwnershipLease) bool {
	for _, lease := range leases {
		if lease.Lost() {
			return true
		}
	}
	return false
}

func fundingPerpSpreadCapitalClaims(cfg *config.Config, fp *config.FundingPerpSpreadConfig, capital, balanceA, balanceB float64) ([]storage.FundingSpreadCapitalClaim, error) {
	if cfg == nil || fp == nil || math.IsNaN(capital) || math.IsInf(capital, 0) || capital <= 0 ||
		math.IsNaN(balanceA) || math.IsInf(balanceA, 0) || balanceA <= 0 || math.IsNaN(balanceB) || math.IsInf(balanceB, 0) || balanceB <= 0 {
		return nil, fmt.Errorf("verified config, capital, and both positive balances are required")
	}
	perLeg := capital / 2
	byWallet := make(map[string]storage.FundingSpreadCapitalClaim, 2)
	for _, leg := range []struct {
		exchange string
		symbol   string
		balance  float64
	}{{fp.LegA.Exchange, fp.LegA.Symbol, balanceA}, {fp.LegB.Exchange, fp.LegB.Symbol, balanceB}} {
		exchangeName := strings.TrimSpace(leg.exchange)
		exchangeCfg, ok := cfg.Exchanges[exchangeName]
		if !ok || strings.TrimSpace(exchangeCfg.APIKey) == "" {
			return nil, fmt.Errorf("verified account identity is unavailable for %s", exchangeName)
		}
		accountScope := equityAccountScopeID(exchangeName, exchangeCfg)
		walletMaterial := accountScope + "|futures|USDT"
		walletKey := fmt.Sprintf("%x", sha256.Sum256([]byte(walletMaterial)))
		claim, exists := byWallet[walletKey]
		if exists {
			if claim.Amount > math.MaxFloat64-perLeg {
				return nil, fmt.Errorf("combined wallet capital overflows")
			}
			claim.Amount += perLeg
			if leg.balance < claim.Available {
				claim.Available = leg.balance
			}
			if !strings.Contains(claim.Symbol, leg.symbol) {
				claim.Symbol += "," + leg.symbol
			}
		} else {
			reservationToken, tokenErr := newAccountWalletCapitalReservationToken()
			if tokenErr != nil {
				return nil, tokenErr
			}
			claim = storage.FundingSpreadCapitalClaim{WalletKey: walletKey, Amount: perLeg, Available: leg.balance,
				ReservationToken: reservationToken, Exchange: exchangeName, Market: "futures", QuoteAsset: "USDT", Symbol: leg.symbol}
		}
		byWallet[walletKey] = claim
	}
	claims := make([]storage.FundingSpreadCapitalClaim, 0, len(byWallet))
	for _, claim := range byWallet {
		claims = append(claims, claim)
	}
	return claims, nil
}

func validateFundingPerpSpreadLegBases(baseA, baseB string) error {
	baseA = strings.ToUpper(strings.TrimSpace(baseA))
	baseB = strings.ToUpper(strings.TrimSpace(baseB))
	if baseA == "" || baseB == "" || baseA != baseB {
		return fmt.Errorf("funding_perp_spread legs must use the same verified base asset; got %q and %q", baseA, baseB)
	}
	return nil
}

// fundingPerpSpreadStateScope isolates durable strategy state by bot, both legs,
// and credential identity without persisting or logging credential material.
func fundingPerpSpreadStateScope(botID string, cfg *config.Config, fp *config.FundingPerpSpreadConfig) string {
	h := sha256.New()
	write := func(value string) {
		_, _ = h.Write([]byte(value))
		_, _ = h.Write([]byte{0})
	}
	write(botID)
	for _, leg := range []config.FundingPerpLeg{fp.LegA, fp.LegB} {
		write(strings.ToLower(strings.TrimSpace(leg.Exchange)))
		write(strings.ToUpper(strings.TrimSpace(leg.Symbol)))
		exCfg, ok := cfg.Exchanges[strings.TrimSpace(leg.Exchange)]
		if !ok {
			write("missing-exchange-config")
			continue
		}
		write(exCfg.APIKey)
		write(exCfg.SecretKey)
		write(exCfg.Passphrase)
		write(fmt.Sprintf("testnet=%t", exCfg.Testnet))
	}
	return "funding_perp_spread:" + hex.EncodeToString(h.Sum(nil))
}

func mergeFundingPerpSpreadStrategyConfig(localCfg *config.Config, symCfg config.SymbolConfig) {
	localCfg.Strategies.Enabled = true
	if localCfg.Strategies.Configs == nil {
		localCfg.Strategies.Configs = make(map[string]config.StrategyConfig)
	}
	var cfg map[string]interface{}
	weight := 1.0
	for _, si := range symCfg.Strategies {
		if si.Type == "funding_perp_spread" {
			cfg = si.Config
			if si.Weight > 0 {
				weight = si.Weight
			}
			break
		}
	}
	localCfg.Strategies.Configs["funding_perp_spread"] = config.StrategyConfig{
		Enabled: true,
		Type:    "funding_perp_spread",
		Weight:  weight,
		Config:  cfg,
	}
}
