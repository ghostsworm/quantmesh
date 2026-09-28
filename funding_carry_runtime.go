package main

import (
	"context"
	"fmt"
	"strings"
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

	futEx, err := exchange.NewExchange(&localCfg, symCfg.Exchange, symCfg.Symbol, "futures")
	if err != nil {
		return nil, fmt.Errorf("創建合約連線失敗: %w", err)
	}
	spotEx, err := exchange.NewExchange(&localCfg, symCfg.Exchange, symCfg.Symbol, "spot")
	if err != nil {
		return nil, fmt.Errorf("創建現貨連線失敗: %w", err)
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
	openingGate := &execution.OpeningGate{}
	if symCfg.OpenPositionControl.PauseOpening || (symCfg.OpenPositionControl.BotRiskControl != nil && symCfg.OpenPositionControl.BotRiskControl.PauseOpening) {
		openingGate.Block("manual")
	}
	futuresOrderExecutor := newFundingCarryOrderExecutor(futEx, symCfg.Symbol, botID, localCfg, distributedLock, openingGate, "BOTH")
	spotOrderExecutor := newFundingCarryOrderExecutor(spotEx, symCfg.Symbol, botID, localCfg, distributedLock, openingGate, "LONG")
	var marginOrderExecutor *fundingCarryOrderExecutor
	if marginEx != nil {
		marginOrderExecutor = newFundingCarryOrderExecutor(marginEx, symCfg.Symbol, botID, localCfg, distributedLock, openingGate, "SHORT")
	}
	accountScope := equityAccountScopeID(symCfg.Exchange, localCfg.Exchanges[symCfg.Exchange])
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
	if err := strategyManager.StartAll(); err != nil {
		return nil, err
	}

	accountID := ""
	if exCfg, ok := baseCfg.Exchanges[symCfg.Exchange]; ok && len(exCfg.APIKey) > 0 {
		if len(exCfg.APIKey) > 8 {
			accountID = exCfg.APIKey[:8]
		} else {
			accountID = exCfg.APIKey
		}
	}

	// 每個 funding_carry bot 獨立同步資金費收入
	if storageService != nil {
		go startFundingIncomeSync(ctx, storageService.GetStorage(), futEx,
			symCfg.Exchange, symCfg.Symbol, accountID, symCfg.GetMarketType(), equityAccountScopeID(symCfg.Exchange, baseCfg.Exchanges[symCfg.Exchange]))
	}

	rt := &SymbolRuntime{
		Config:               symCfg,
		Exchange:             futEx,
		PriceMonitor:         priceMonitor,
		StrategyManager:      strategyManager,
		EventBus:             eventBus,
		StorageService:       storageService,
		AccountID:            accountID,
		AccountScope:         accountScope,
		AccountMarketType:    config.MarketTypeFundingCarry,
		SuperPositionManager: nil,
		OpeningGate:          openingGate,
		ExchangeExecutor:     nil,
		ExecutorAdapter:      nil,
		ExchangeAdapter:      nil,
	}
	executors := []*order.ExchangeOrderExecutor{futuresOrderExecutor.executor, spotOrderExecutor.executor}
	if marginOrderExecutor != nil {
		executors = append(executors, marginOrderExecutor.executor)
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
	rt.GetOpenControl = fc.OpenPositionControl

	rt.Stop = func() {
		logger.InfoCtx(ctx, "⏹️ [%s] 停止資金費套利運行時（策略 Stop 會自動嘗試平倉）", symCfg.Symbol)
		shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), processShutdownTotalTimeout)
		sealRuntimeShutdown(rt)
		if err := rt.PrepareShutdown(shutdownCtx, true); err != nil {
			rt.markShutdownCloseUnverified(fmt.Sprintf("資金費套利停止準備未核實: %v", err))
			logger.ErrorCtx(ctx, "[%s] 资金费套利停止准备失败，保留敞口待核对: %v", symCfg.Symbol, err)
		} else if err := rt.CloseForShutdown(shutdownCtx); err != nil {
			rt.markShutdownCloseUnverified(fmt.Sprintf("資金費套利策略平倉未核實: %v", err))
			logger.ErrorCtx(ctx, "[%s] 资金费套利配对平仓失败，保留敞口待核对: %v", symCfg.Symbol, err)
		}
		cancelShutdown()
		if strategyManager != nil {
			strategyManager.StopAll()
		}
		if priceMonitor != nil {
			priceMonitor.Stop()
		}
		futEx.StopOrderStream()
		spotEx.StopOrderStream()
		if marginEx != nil {
			marginEx.StopOrderStream()
		}
	}
	runtimeReady = true

	return rt, nil
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
