package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"quantmesh/config"
	"quantmesh/event"
	"quantmesh/exchange"
	"quantmesh/execution"
	"quantmesh/lock"
	"quantmesh/logger"
	"quantmesh/monitor"
	"quantmesh/order"
	"quantmesh/position"
	"quantmesh/storage"
	"quantmesh/strategy"
)

// startFundingCarrySymbolRuntime 資金費套利專用運行時（現貨+合約雙連線，不跑網格）
func startFundingCarrySymbolRuntime(
	ctx context.Context,
	baseCfg *config.Config,
	symCfg config.SymbolConfig,
	eventBus *event.EventBus,
	storageService *storage.StorageService,
	distributedLock lock.DistributedLock,
	onRequestStop func(botID string),
	startupPauseHolders []storage.OpeningPauseHolder,
) (*SymbolRuntime, error) {
	if strings.ToLower(symCfg.Exchange) != "binance" {
		return nil, fmt.Errorf("資金費套利當前僅支援 binance，當前: %s", symCfg.Exchange)
	}

	permCtx, cancelPerm := context.WithTimeout(ctx, 15*time.Second)
	defer cancelPerm()
	perm, err := exchange.CheckFundingCarrySetup(permCtx, baseCfg, symCfg.Exchange, symCfg.Symbol)
	if err != nil {
		return nil, fmt.Errorf("資金費套利預檢失敗: %w", err)
	}
	if perm == nil {
		return nil, fmt.Errorf("資金費套利預檢結果為空")
	}
	if !perm.OK {
		return nil, fmt.Errorf("資金費套利 API 未就緒: 現貨=%v 合約=%v (%s / %s)",
			perm.SpotOK, perm.FuturesOK, perm.SpotMessage, perm.FuturesMessage)
	}

	localCfg := *baseCfg
	botID := symCfg.ID
	if botID == "" {
		botID = config.GenerateBotID(symCfg.Exchange, symCfg.Symbol, symCfg.GetMarketType())
	}
	localCfg.Trading.BotID = botID
	ctx = logger.WithBotID(ctx, botID)
	localCfg.Trading.Symbol = symCfg.Symbol
	localCfg.Trading.MarketType = config.MarketTypeFundingCarry
	mergeFundingCarryStrategyConfig(&localCfg, symCfg)
	openingGate := &execution.OpeningGate{}
	applyStartupOpeningPauseHolders(openingGate, startupPauseHolders)
	var ownershipStrategy atomic.Pointer[strategy.FundingCarryStrategy]
	if symCfg.OpenPositionControl.PauseOpening || (symCfg.OpenPositionControl.BotRiskControl != nil && symCfg.OpenPositionControl.BotRiskControl.PauseOpening) {
		openingGate.Block("manual")
	}
	ownershipLeases, err := acquireFundingCarryRuntimeOwnershipLeases(ctx, distributedLock, baseCfg,
		symCfg.Exchange, symCfg.Symbol, fundingCarryReverseEnabled(symCfg), func(leaseErr error) {
			openingGate.Block("runtime_ownership_unverified")
			if current := ownershipStrategy.Load(); current != nil {
				current.MarkExecutionUnknown(leaseErr)
			}
			logger.ErrorCtx(ctx, "[%s] funding_carry 运行所有权租约丢失，已封锁所有新开仓: %v", botID, leaseErr)
		})
	if err != nil {
		return nil, err
	}
	ownershipTransferred := false
	retainOwnershipOnFailure := false
	defer func() {
		if ownershipTransferred || retainOwnershipOnFailure {
			return
		}
		if releaseErr := releaseFundingCarryRuntimeOwnershipLeases(ownershipLeases); releaseErr != nil {
			logger.ErrorCtx(ctx, "[%s] funding_carry 初始化失败后释放运行所有权租约失败: %v", botID, releaseErr)
		}
	}()

	futEx, err := exchange.NewExchange(&localCfg, symCfg.Exchange, symCfg.Symbol, "futures")
	if err != nil {
		return nil, fmt.Errorf("創建合約連線失敗: %w", err)
	}
	spotEx, err := exchange.NewExchange(&localCfg, symCfg.Exchange, symCfg.Symbol, "spot")
	if err != nil {
		return nil, fmt.Errorf("創建現貨連線失敗: %w", err)
	}
	if err := validateFundingCarryPairAssets(spotEx.GetBaseAsset(), spotEx.GetQuoteAsset(), futEx.GetBaseAsset(), futEx.GetQuoteAsset()); err != nil {
		return nil, fmt.Errorf("資金費套利現貨/合約資產不匹配: %w", err)
	}
	var futuresAccountCapital, spotAccountCapital, futuresAvailable, spotAvailable float64
	var futuresAvailableAt, spotAvailableAt time.Time
	for _, wallet := range []struct {
		market string
		ex     exchange.IExchange
	}{{market: "futures", ex: futEx}, {market: "spot", ex: spotEx}} {
		allocated, err := configuredAccountWalletCapitalForQuote(baseCfg, symCfg, symCfg.Exchange, wallet.market, "USDT")
		if err != nil {
			return nil, fmt.Errorf("計算同帳戶 %s 錢包配置資金: %w", wallet.market, err)
		}
		balanceCtx, cancelBalance := context.WithTimeout(ctx, 10*time.Second)
		observedAt := time.Now().UTC()
		available, balanceErr := wallet.ex.GetBalance(balanceCtx, "USDT")
		cancelBalance()
		if balanceErr != nil {
			return nil, fmt.Errorf("讀取 %s USDT 可用餘額: %w", wallet.market, balanceErr)
		}
		if math.IsNaN(available) || math.IsInf(available, 0) || available <= 0 || allocated > available {
			return nil, fmt.Errorf("同帳戶 %s 配置資金 %.2f USDT 超過或無法核實可用餘額 %.2f USDT", wallet.market, allocated, available)
		}
		if wallet.market == "futures" {
			futuresAccountCapital = allocated
			futuresAvailable = available
			futuresAvailableAt = observedAt
		} else {
			spotAccountCapital = allocated
			spotAvailable = available
			spotAvailableAt = observedAt
		}
	}

	// 嘗試建立保證金帳戶連線（反向套利用，失敗不阻塞啟動）
	var marginEx exchange.ISpotMarginExchange
	marginRaw, marginErr := exchange.NewExchange(&localCfg, symCfg.Exchange, symCfg.Symbol, "spot_margin")
	if marginErr == nil {
		if me, ok := marginRaw.(exchange.ISpotMarginExchange); ok {
			marginEx = me
			logger.InfoCtx(ctx, "✅ [%s] 保證金帳戶已連線（支援反向套利）", symCfg.Symbol)
		}
	} else {
		logger.InfoCtx(ctx, "ℹ️ [%s] 保證金帳戶不可用（%v），反向套利已禁用", symCfg.Symbol, marginErr)
	}
	var marginAvailable float64
	var marginAvailableAt time.Time
	if fundingCarryReverseEnabled(symCfg) && marginEx != nil {
		if err := validateFundingCarryPairAssets(spotEx.GetBaseAsset(), spotEx.GetQuoteAsset(), marginEx.GetBaseAsset(), marginEx.GetQuoteAsset()); err != nil {
			return nil, fmt.Errorf("資金費套利現貨/槓桿錢包資產不匹配: %w", err)
		}
		allocated, allocationErr := configuredAccountWalletCapitalForQuote(baseCfg, symCfg, symCfg.Exchange, "spot_margin", "USDT")
		if allocationErr != nil {
			return nil, fmt.Errorf("計算同帳戶 spot_margin 錢包配置資金: %w", allocationErr)
		}
		balanceCtx, cancelBalance := context.WithTimeout(ctx, 10*time.Second)
		marginAvailableAt = time.Now().UTC()
		available, balanceErr := marginEx.GetBalance(balanceCtx, "USDT")
		cancelBalance()
		if balanceErr != nil {
			return nil, fmt.Errorf("讀取 spot_margin USDT 可用餘額: %w", balanceErr)
		}
		if math.IsNaN(available) || math.IsInf(available, 0) || available <= 0 || allocated > available {
			return nil, fmt.Errorf("同帳戶 spot_margin 配置資金 %.2f USDT 超過或無法核實可用餘額 %.2f USDT", allocated, available)
		}
		marginAvailable = available
	}

	priceMonitor := monitor.NewPriceMonitor(
		futEx,
		symCfg.Symbol,
		localCfg.Timing.PriceSendInterval,
	)
	if err := priceMonitor.Start(); err != nil {
		return nil, fmt.Errorf("價格流: %w", err)
	}
	runtimeReady := false
	defer func() {
		if !runtimeReady {
			priceMonitor.Stop()
			futEx.StopOrderStream()
			spotEx.StopOrderStream()
			if marginEx != nil {
				marginEx.StopOrderStream()
			}
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
		return nil, fmt.Errorf("無法獲取初始價格")
	}

	totalCap := symCfg.TotalAllocatedCapital
	if totalCap <= 0 {
		totalCap = symCfg.OrderQuantity
	}
	if totalCap <= 0 {
		return nil, fmt.Errorf("total_allocated_capital 必須大於 0")
	}

	strategyManager := strategy.NewStrategyManager(&localCfg, totalCap)
	if eventBus != nil {
		strategyManager.SetEventBus(eventBus)
	}
	var fcCfg map[string]interface{}
	for _, si := range symCfg.Strategies {
		if si.Type == "funding_carry" {
			fcCfg = si.Config
			break
		}
	}
	fc := strategy.NewFundingCarryStrategy("funding_carry", &localCfg, symCfg, futEx, spotEx, marginEx, fcCfg)
	ownershipStrategy.Store(fc)
	if fundingCarryRuntimeOwnershipLeaseLost(ownershipLeases) {
		fc.MarkExecutionUnknown(fmt.Errorf("runtime ownership lease lost during Funding Carry initialization"))
	}
	accountScope := equityAccountScopeID(symCfg.Exchange, localCfg.Exchanges[symCfg.Exchange])
	if err := fc.SetMarginAccountScope(accountScope); err != nil {
		return nil, fmt.Errorf("configure funding_carry margin account scope: %w", err)
	}
	if err := fc.SetAccountWalletCoordinationLock(distributedLock, "funding_carry_wallet:"+accountScope); err != nil {
		return nil, fmt.Errorf("configure funding_carry account wallet coordination: %w", err)
	}
	ownCapital := symCfg.TotalAllocatedCapital
	if ownCapital <= 0 {
		ownCapital = symCfg.OrderQuantity
	}
	if ownCapital <= 0 {
		ownCapital = baseCfg.Strategies.CapitalAllocation.TotalCapital
	}
	ownLegCapital := ownCapital / 2
	futuresExternalCapital := math.Max(0, futuresAccountCapital-ownLegCapital)
	spotExternalCapital := math.Max(0, spotAccountCapital-ownLegCapital)
	if err := fc.SetAccountCapitalReserves(futuresAccountCapital, futuresExternalCapital, spotAccountCapital, spotExternalCapital); err != nil {
		return nil, fmt.Errorf("configure account-level funding_carry capital reserves: %w", err)
	}
	if storageService != nil {
		fc.SetRuntimeStateStore(&strategyRuntimeStateAdapter{storageService: storageService, botID: botID})
	}
	if storageService == nil || storageService.GetStorage() == nil {
		return nil, fmt.Errorf("funding_carry requires durable runtime and execution-intent storage")
	}
	intentBackend, ok := storageService.GetStorage().(runtimeIntentBackend)
	if !ok {
		return nil, fmt.Errorf("funding_carry storage backend does not support durable execution intents")
	}
	capitalClaims := make([]storage.AccountWalletCapitalClaim, 0, 3)
	for _, wallet := range []struct {
		market    string
		available float64
	}{{"futures", futuresAvailable}, {"spot", spotAvailable}} {
		observedAt := futuresAvailableAt
		if wallet.market == "spot" {
			observedAt = spotAvailableAt
		}
		claim, claimErr := buildAccountWalletCapitalClaimFromObservation(baseCfg, symCfg.Exchange, wallet.market, "USDT", ownLegCapital, wallet.available, observedAt)
		if claimErr != nil {
			return nil, fmt.Errorf("build funding_carry %s capital reservation: %w", wallet.market, claimErr)
		}
		claim.Exchange, claim.Market, claim.QuoteAsset, claim.Symbol = symCfg.Exchange, wallet.market, "USDT", symCfg.Symbol
		capitalClaims = append(capitalClaims, claim)
	}
	if marginAvailable > 0 {
		claim, claimErr := buildAccountWalletCapitalClaimFromObservation(baseCfg, symCfg.Exchange, "spot_margin", "USDT", ownLegCapital, marginAvailable, marginAvailableAt)
		if claimErr != nil {
			return nil, fmt.Errorf("build funding_carry spot_margin capital reservation: %w", claimErr)
		}
		claim.Exchange, claim.Market, claim.QuoteAsset, claim.Symbol = symCfg.Exchange, "spot_margin", "USDT", symCfg.Symbol
		capitalClaims = append(capitalClaims, claim)
	}
	if err := reserveAccountWalletCapital(ctx, baseCfg, storageService, distributedLock, botID, capitalClaims); err != nil {
		return nil, fmt.Errorf("reserve funding_carry account wallet capital: %w", err)
	}
	capitalStore, ok := storageService.GetStorage().(storage.AccountWalletCapitalReservationStore)
	if !ok {
		return nil, fmt.Errorf("funding_carry account wallet reservation storage disappeared after claim")
	}
	reservationTransferred := false
	strategyStartAttempted := false
	defer func() {
		if reservationTransferred {
			return
		}
		if fundingCarryRuntimeOwnershipLeaseLost(ownershipLeases) {
			fc.MarkExecutionUnknown(fmt.Errorf("runtime ownership lease lost during Funding Carry initialization"))
			if strategyStartAttempted {
				if stopErr := strategyManager.StopAllWithError(); stopErr != nil {
					logger.WarnCtx(ctx, "[%s] Funding Carry lease-loss startup freeze retained unresolved strategy state: %v", botID, stopErr)
				}
			}
			retainOwnershipOnFailure = true
			logger.ErrorCtx(ctx, "[%s] funding_carry 初始化失敗時運行租約已丟失，保留資金 claim 等待對帳", botID)
			return
		}
		if strategyStartAttempted {
			if stopErr := strategyManager.StopAllWithError(); stopErr != nil {
				retainOwnershipOnFailure = true
				logger.ErrorCtx(ctx, "[%s] funding_carry 初始化失敗後策略停止未核實，保留資金 claim 與運行租約: %v", botID, stopErr)
				return
			}
		}
		releaseCtx, cancelRelease := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancelRelease()
		releaseErr := verifyAndReleaseAccountWalletCapitalGuarded(releaseCtx, capitalStore, botID, capitalClaims, fc.VerifyFlat, func() error {
			if fundingCarryRuntimeOwnershipLeaseLost(ownershipLeases) {
				return fmt.Errorf("funding_carry runtime ownership lease was lost")
			}
			return nil
		})
		if releaseErr != nil {
			retainOwnershipOnFailure = true
			logger.ErrorCtx(ctx, "[%s] funding_carry 初始化失敗後无法核实并释放 claim，保留资金预留与运行租约: %v", botID, releaseErr)
		}
	}()
	futuresOrderExecutor := newFundingCarryOrderExecutor(futEx, symCfg.Symbol, botID, localCfg, distributedLock, openingGate, "BOTH")
	spotOrderExecutor := newFundingCarryOrderExecutor(spotEx, symCfg.Symbol, botID, localCfg, distributedLock, openingGate, "LONG")
	var marginOrderExecutor *fundingCarryOrderExecutor
	if marginEx != nil {
		marginOrderExecutor = newFundingCarryOrderExecutor(marginEx, symCfg.Symbol, botID, localCfg, distributedLock, openingGate, "SHORT")
	}
	for _, leg := range []struct {
		exchange exchange.IExchange
		executor *fundingCarryOrderExecutor
	}{{futEx, futuresOrderExecutor}, {spotEx, spotOrderExecutor}} {
		scope := execution.IntentScope{Account: accountScope, Exchange: leg.exchange.GetName(), Market: leg.exchange.GetMarketType(), Symbol: symCfg.Symbol, Bot: botID}
		if err := leg.executor.executor.ConfigureIntentJournal(ctx, intentBackend, scope); err != nil {
			return nil, fmt.Errorf("configure funding_carry %s execution journal: %w", leg.exchange.GetMarketType(), err)
		}
	}
	if marginOrderExecutor != nil {
		scope := execution.IntentScope{Account: accountScope, Exchange: marginEx.GetName(), Market: marginEx.GetMarketType(), Symbol: symCfg.Symbol, Bot: botID}
		if err := marginOrderExecutor.executor.ConfigureIntentJournal(ctx, intentBackend, scope); err != nil {
			return nil, fmt.Errorf("configure funding_carry margin execution journal: %w", err)
		}
	}
	fc.SetOrderExecutors(futuresOrderExecutor, spotOrderExecutor, marginOrderExecutor)
	fc.SetOpeningBlocker(openingGate.Block)
	fc.SetOpeningGate(openingGate)
	futuresOrderExecutor.executor.SetUnknownOrderHandler(func(req order.OrderRequest) {
		fc.MarkExecutionUnknown(fmt.Errorf("futures order %s has unresolved outcome", req.ClientOrderID))
	})
	spotOrderExecutor.executor.SetUnknownOrderHandler(func(req order.OrderRequest) {
		fc.MarkExecutionUnknown(fmt.Errorf("spot order %s has unresolved outcome", req.ClientOrderID))
	})
	if marginOrderExecutor != nil {
		marginOrderExecutor.executor.SetUnknownOrderHandler(func(req order.OrderRequest) {
			fc.MarkExecutionUnknown(fmt.Errorf("margin order %s has unresolved outcome", req.ClientOrderID))
		})
	}
	strategyManager.RegisterStrategy("funding_carry", fc, 1.0, 0)
	strategyStartAttempted = true
	if err := strategyManager.StartAll(); err != nil {
		return nil, err
	}
	if fundingCarryRuntimeOwnershipLeaseLost(ownershipLeases) {
		openingGate.Block("runtime_ownership_unverified")
		logger.ErrorCtx(ctx, "[%s] funding_carry 啟動期間運行租約丟失；保留受管運行時與資金 claim，等待對帳", botID)
	}

	accountID := ""
	if exCfg, ok := baseCfg.Exchanges[symCfg.Exchange]; ok && strings.TrimSpace(exCfg.APIKey) != "" {
		accountID = equityAccountScopeID(symCfg.Exchange, exCfg)
	}

	// 每個 funding_carry bot 獨立同步資金費收入
	if storageService != nil {
		go startFundingIncomeSync(ctx, storageService.GetStorage(), futEx,
			symCfg.Exchange, symCfg.Symbol, accountID, symCfg.GetMarketType(), equityAccountScopeID(symCfg.Exchange, baseCfg.Exchanges[symCfg.Exchange]))
		if fundingCarryReverseEnabled(symCfg) && marginEx != nil {
			go startMarginInterestSync(ctx, storageService.GetStorage(), marginEx, symCfg.Exchange,
				accountID, equityAccountScopeID(symCfg.Exchange, baseCfg.Exchanges[symCfg.Exchange]))
		}
	}

	rt := &SymbolRuntime{
		Config:                symCfg,
		Exchange:              futEx,
		PriceMonitor:          priceMonitor,
		StrategyManager:       strategyManager,
		EventBus:              eventBus,
		StorageService:        storageService,
		AccountID:             accountID,
		AccountScope:          accountScope,
		AccountMarketType:     config.MarketTypeFundingCarry,
		SuperPositionManager:  nil,
		verifiedCapitalBudget: ownCapital,
		OpeningGate:           openingGate,
		ExchangeExecutor:      nil,
		ExecutorAdapter:       nil,
		ExchangeAdapter:       nil,
	}
	executors := []*order.ExchangeOrderExecutor{futuresOrderExecutor.executor, spotOrderExecutor.executor}
	if marginOrderExecutor != nil {
		executors = append(executors, marginOrderExecutor.executor)
	}
	rt.CancelOpeningOrders = func(cancelCtx context.Context) error {
		var cancelErrors []error
		for _, executor := range executors {
			if err := executor.CancelOwnedOpeningOrders(cancelCtx); err != nil {
				cancelErrors = append(cancelErrors, err)
			}
		}
		return errors.Join(cancelErrors...)
	}
	rt.PrepareShutdown = func(shutdownCtx context.Context, cancelOrders bool) error {
		for _, executor := range executors {
			executor.BeginShutdown()
		}
		for _, executor := range executors {
			if err := executor.DrainShutdown(shutdownCtx); err != nil {
				return fmt.Errorf("drain funding_carry order submissions: %w", err)
			}
		}
		if cancelOrders {
			for _, executor := range executors {
				if err := executor.CancelOwnedShutdownOrders(shutdownCtx); err != nil {
					return fmt.Errorf("cancel/verify funding_carry owned orders: %w", err)
				}
			}
		}
		return nil
	}
	rt.CloseForShutdown = func(shutdownCtx context.Context) error {
		closeCtx := shutdownCtx
		for _, executor := range executors {
			var err error
			closeCtx, err = executor.ShutdownCloseContext(closeCtx)
			if err != nil {
				return fmt.Errorf("grant funding_carry shutdown-close permit: %w", err)
			}
		}
		return fc.StopContext(closeCtx)
	}
	rt.VerifyShutdownClose = fc.VerifyFlat
	rt.CloseForManual = func(closeCtx context.Context, closeCfg config.ClosePositionConfig) (*position.ClosePositionRecord, error) {
		if openingGate.HasBlock(order.RuntimeShutdownBlock) {
			return nil, order.ErrRuntimeStopping
		}
		if closeCfg.QuantityRatio != 0 && closeCfg.QuantityRatio != 1 {
			return nil, fmt.Errorf("funding_carry manual close currently supports full close only")
		}
		if closeCfg.Method != "" && closeCfg.Method != string(position.CloseMethodMarket) {
			return nil, fmt.Errorf("funding_carry manual close uses the strategy's verified paired-leg execution, market/limit override is unavailable")
		}
		qty, closeErr := fc.CloseOwned(closeCtx)
		status := position.CloseStatusFilled
		message := "strategy-owned paired positions closed and verified"
		if closeErr != nil {
			status = position.CloseStatusFailed
			message = closeErr.Error()
		}
		filled := qty
		if closeErr != nil {
			filled = 0
		}
		now := time.Now()
		record := &position.ClosePositionRecord{RecordID: fmt.Sprintf("funding-carry-%d", now.UnixNano()), BotID: botID,
			Symbol: symCfg.Symbol, TargetQty: qty, FilledQty: filled, Method: position.CloseMethodMarket,
			Status: status, CreatedAt: now, UpdatedAt: now, ErrorMessage: message}
		return record, closeErr
	}
	rt.UpdateOpenControl = func(control config.OpenPositionControl) error {
		fc.UpdateOpenPositionControl(control)
		return nil
	}
	rt.ClampOpenControl = func(control config.OpenPositionControl) (config.OpenPositionControl, error) {
		control = config.CloneOpenPositionControl(control)
		if err := applyBotCapitalLimit(&control, ownCapital); err != nil {
			return config.OpenPositionControl{}, err
		}
		return control, nil
	}
	rt.GetOpenControl = fc.OpenPositionControl

	var stopOnce sync.Once
	var ownershipLossStopOnce sync.Once
	var stopMu sync.Mutex
	var stopErr error
	stopRuntime := func() error {
		if rt.capitalReservationStop != nil {
			rt.capitalReservationStop()
		}
		if fundingCarryRuntimeOwnershipLeaseLost(ownershipLeases) {
			if rt.OpeningGate != nil {
				rt.OpeningGate.Block("runtime_ownership_unverified")
			}
			fc.MarkExecutionUnknown(fmt.Errorf("funding_carry runtime ownership lease was lost"))
			ownershipLossStopOnce.Do(func() {
				if stopErr := strategyManager.StopAllWithError(); stopErr != nil {
					logger.WarnCtx(ctx, "[%s] ownership-loss freeze stopped without closing potentially unowned exposure: %v", botID, stopErr)
				}
				priceMonitor.Stop()
				futEx.StopOrderStream()
				spotEx.StopOrderStream()
				if marginEx != nil {
					marginEx.StopOrderStream()
				}
			})
			return fmt.Errorf("funding_carry runtime ownership lease was lost; retain runtime and capital reservation for reconciliation")
		}
		stopMu.Lock()
		defer stopMu.Unlock()
		stopOnce.Do(func() {
			logger.InfoCtx(ctx, "⏹️ [%s] 停止資金費套利運行時（平倉後將獨立核驗再釋放預留）", symCfg.Symbol)
			shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), processShutdownTotalTimeout)
			defer cancelShutdown()
			sealRuntimeShutdown(rt)
			var stopErrors []error
			if err := rt.PrepareShutdown(shutdownCtx, true); err != nil {
				rt.markShutdownCloseUnverified(fmt.Sprintf("資金費套利停止準備未核實: %v", err))
				stopErrors = append(stopErrors, fmt.Errorf("prepare funding_carry shutdown: %w", err))
			} else if fundingCarryRuntimeOwnershipLeaseLost(ownershipLeases) {
				rt.markShutdownCloseUnverified("資金費套利運行所有權租約丟失，禁止獨立平倉")
				stopErrors = append(stopErrors, fmt.Errorf("funding_carry runtime ownership lease was lost before close"))
			} else if err := rt.CloseForShutdown(shutdownCtx); err != nil {
				rt.markShutdownCloseUnverified(fmt.Sprintf("資金費套利策略平倉未核實: %v", err))
				stopErrors = append(stopErrors, fmt.Errorf("close funding_carry strategy-owned exposure: %w", err))
			}
			if strategyManager != nil {
				if err := strategyManager.StopAllWithError(); err != nil {
					stopErrors = append(stopErrors, fmt.Errorf("stop funding_carry strategy manager: %w", err))
				}
			}
			if priceMonitor != nil {
				priceMonitor.Stop()
			}
			futEx.StopOrderStream()
			spotEx.StopOrderStream()
			if marginEx != nil {
				marginEx.StopOrderStream()
			}
			stopErr = errors.Join(stopErrors...)
		})
		if stopErr != nil {
			return stopErr
		}
		if fundingCarryRuntimeOwnershipLeaseLost(ownershipLeases) {
			if rt.OpeningGate != nil {
				rt.OpeningGate.Block("runtime_ownership_unverified")
			}
			return fmt.Errorf("funding_carry runtime ownership lease was lost; retain capital reservation for reconciliation")
		}
		verifyCtx, cancelVerify := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancelVerify()
		if err := verifyAndReleaseAccountWalletCapitalGuarded(verifyCtx, capitalStore, botID, capitalClaims, fc.VerifyFlat, func() error {
			if fundingCarryRuntimeOwnershipLeaseLost(ownershipLeases) {
				return fmt.Errorf("funding_carry runtime ownership lease was lost")
			}
			return nil
		}); err != nil {
			rt.markShutdownCloseUnverified(err.Error())
			if rt.OpeningGate != nil {
				rt.OpeningGate.Block("capital_reservation_unverified")
			}
			return fmt.Errorf("funding_carry capital reservation retained because flatness/release is unverified: %w", err)
		}
		return releaseFundingCarryRuntimeOwnershipLeases(ownershipLeases)
	}
	rt.StopWithError = stopRuntime
	rt.Stop = func() {
		if err := stopRuntime(); err != nil {
			logger.ErrorCtx(ctx, "[%s] 资金费套利停止或资金预留释放未核实，保留预留等待核账: %v", symCfg.Symbol, err)
		}
	}
	runtimeReady = true
	if capitalStore, ok := storageService.GetStorage().(storage.AccountWalletCapitalReservationStore); ok {
		readers := make([]accountWalletBalanceReader, 0, len(capitalClaims))
		for _, claim := range capitalClaims {
			var client exchange.IExchange
			switch strings.ToLower(strings.TrimSpace(claim.Market)) {
			case "futures":
				client = futEx
			case "spot":
				client = spotEx
			case "spot_margin":
				client = marginEx
			}
			readers = append(readers, accountWalletBalanceReaderForClaim(claim, client))
		}
		rt.capitalReservationStore = capitalStore
		rt.capitalReservationBotID = botID
		rt.capitalReservationClaims = append([]storage.AccountWalletCapitalClaim(nil), capitalClaims...)
		rt.capitalReservationStop = startRuntimeAccountWalletCapitalRevalidation(ctx, baseCfg, storageService,
			distributedLock, botID, capitalClaims, readers, openingGate, rt.CancelOpeningOrders)
	}
	reservationTransferred = true
	ownershipTransferred = true

	return rt, nil
}

func validateFundingCarryPairAssets(spotBase, spotQuote, hedgeBase, hedgeQuote string) error {
	spotBase = strings.ToUpper(strings.TrimSpace(spotBase))
	spotQuote = strings.ToUpper(strings.TrimSpace(spotQuote))
	hedgeBase = strings.ToUpper(strings.TrimSpace(hedgeBase))
	hedgeQuote = strings.ToUpper(strings.TrimSpace(hedgeQuote))
	if spotBase == "" || hedgeBase == "" || spotQuote == "" || hedgeQuote == "" {
		return fmt.Errorf("交易所未提供完整基礎幣/報價幣身份")
	}
	if spotBase != hedgeBase {
		return fmt.Errorf("基礎幣不一致：現貨 %s、對沖腿 %s", spotBase, hedgeBase)
	}
	if spotQuote != "USDT" || hedgeQuote != "USDT" {
		return fmt.Errorf("目前資金預算僅核對 USDT 錢包，報價幣必須均為 USDT：現貨 %s、對沖腿 %s", spotQuote, hedgeQuote)
	}
	return nil
}

func fundingCarryReverseEnabled(symCfg config.SymbolConfig) bool {
	for _, instance := range symCfg.Strategies {
		if instance.Type != "funding_carry" || instance.Config == nil {
			continue
		}
		enabled, _ := instance.Config["reverse_enabled"].(bool)
		return enabled
	}
	return false
}

type fundingCarryOrderExecutor struct {
	executor *order.ExchangeOrderExecutor
	market   string
}

func newFundingCarryOrderExecutor(ex exchange.IExchange, symbol, botID string, cfg config.Config, distributedLock lock.DistributedLock, gate *execution.OpeningGate, direction string) *fundingCarryOrderExecutor {
	executor := order.NewExchangeOrderExecutor(ex, symbol, cfg.Timing.RateLimitRetryDelay, cfg.Timing.OrderRetryDelay, distributedLock, botID)
	executor.SetPostOnlyRepriceMaxAttempts(cfg.Trading.PostOnlyRepriceMaxAttempts)
	executor.SetOpeningGate(gate, direction)
	return &fundingCarryOrderExecutor{executor: executor, market: ex.GetMarketType()}
}

func (a *fundingCarryOrderExecutor) PlaceOrderContext(ctx context.Context, request *exchange.OrderRequest) (*exchange.Order, error) {
	if a == nil || a.executor == nil || request == nil {
		return nil, fmt.Errorf("funding_carry order executor or request is nil")
	}
	positionSide := ""
	if strings.EqualFold(a.market, "spot") {
		positionSide = "LONG"
	} else if strings.EqualFold(a.market, "spot_margin") {
		positionSide = "SHORT"
	}
	placed, err := a.executor.PlaceOrderContext(ctx, &order.OrderRequest{
		Symbol: request.Symbol, Side: string(request.Side), Type: string(request.Type), TimeInForce: string(request.TimeInForce),
		Price: request.Price, Quantity: request.Quantity, PriceDecimals: request.PriceDecimals, ReduceOnly: request.ReduceOnly,
		PositionSide: positionSide, StrategyName: "funding_carry", StrategyType: request.StrategyType,
	})
	if err != nil || placed == nil {
		if err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("funding_carry executor returned an empty order acknowledgement")
	}
	return &exchange.Order{
		OrderID: placed.OrderID, ClientOrderID: placed.ClientOrderID, Symbol: placed.Symbol,
		Side: exchange.Side(placed.Side), Type: exchange.OrderType(request.Type), Price: placed.Price,
		Quantity: placed.Quantity, ExecutedQty: placed.ExecutedQty, AvgPrice: placed.AvgPrice,
		Status: exchange.OrderStatus(placed.Status), CreatedAt: placed.CreatedAt,
	}, nil
}

func (a *fundingCarryOrderExecutor) SettleIntent(ctx context.Context, clientOrderID string) error {
	if a == nil || a.executor == nil {
		return fmt.Errorf("funding_carry order executor is nil")
	}
	return a.executor.SettleIntent(ctx, clientOrderID)
}

func (a *fundingCarryOrderExecutor) CancelOrderContext(ctx context.Context, orderID int64) error {
	if a == nil || a.executor == nil {
		return fmt.Errorf("funding_carry order executor is nil")
	}
	return a.executor.CancelOrderContext(ctx, orderID)
}

func mergeFundingCarryStrategyConfig(localCfg *config.Config, symCfg config.SymbolConfig) {
	localCfg.Strategies.Enabled = true
	if localCfg.Strategies.Configs == nil {
		localCfg.Strategies.Configs = make(map[string]config.StrategyConfig)
	}
	var cfg map[string]interface{}
	weight := 1.0
	for _, si := range symCfg.Strategies {
		if si.Type == "funding_carry" {
			cfg = si.Config
			if si.Weight > 0 {
				weight = si.Weight
			}
			break
		}
	}
	localCfg.Strategies.Configs["funding_carry"] = config.StrategyConfig{
		Enabled: true,
		Type:    "funding_carry",
		Weight:  weight,
		Config:  cfg,
	}
}
