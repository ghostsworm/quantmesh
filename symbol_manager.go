package main

import (
	"context"
	"fmt"
	"reflect"
	"time"

	"quantmesh/config"
	"quantmesh/event"
	"quantmesh/exchange"
	"quantmesh/lock"
	"quantmesh/logger"
	"quantmesh/monitor"
	"quantmesh/order"
	"quantmesh/position"
	"quantmesh/safety"
	"quantmesh/storage"
	"quantmesh/strategy"
)

// SymbolRuntime 代表单个交易所/交易对的运行时组件集合
type SymbolRuntime struct {
	Config               config.SymbolConfig
	Exchange             exchange.IExchange
	PriceMonitor         *monitor.PriceMonitor
	RiskMonitor          *safety.RiskMonitor
	SuperPositionManager *position.SuperPositionManager
	OrderCleaner         *safety.OrderCleaner
	Reconciler           *safety.Reconciler
	TrendDetector        *strategy.TrendDetector
	DynamicAdjuster      *strategy.DynamicAdjuster
	StrategyManager      *strategy.StrategyManager
	ExchangeExecutor     *order.ExchangeOrderExecutor
	ExecutorAdapter      *exchangeExecutorAdapter
	ExchangeAdapter      *positionExchangeAdapter
	EventBus             *event.EventBus
	StorageService       *storage.StorageService
	Stop                 func()
}

// SymbolManager 管理多个 SymbolRuntime
type SymbolManager struct {
	cfg      *config.Config
	runtimes map[string]*SymbolRuntime
}

// NewSymbolManager 创建管理器
func NewSymbolManager(cfg *config.Config) *SymbolManager {
	return &SymbolManager{
		cfg:      cfg,
		runtimes: make(map[string]*SymbolRuntime),
	}
}

// runtimeKey 生成唯一键（exchange:symbol）
func runtimeKey(exchangeName, symbol string) string {
	return fmt.Sprintf("%s:%s", exchangeName, symbol)
}

// Add 注册运行时
func (sm *SymbolManager) Add(rt *SymbolRuntime) {
	key := runtimeKey(rt.Config.Exchange, rt.Config.Symbol)
	sm.runtimes[key] = rt
}

// Get 获取运行时
func (sm *SymbolManager) Get(exchangeName, symbol string) (*SymbolRuntime, bool) {
	key := runtimeKey(exchangeName, symbol)
	rt, ok := sm.runtimes[key]
	return rt, ok
}

// List 列出所有运行时
func (sm *SymbolManager) List() []*SymbolRuntime {
	list := make([]*SymbolRuntime, 0, len(sm.runtimes))
	for _, rt := range sm.runtimes {
		list = append(list, rt)
	}
	return list
}

// StopAll 停止所有运行时（如退出时调用）
func (sm *SymbolManager) StopAll() {
	for _, rt := range sm.runtimes {
		if rt != nil && rt.Stop != nil {
			rt.Stop()
		}
	}
}

// startSymbolRuntime 启动单个交易对的核心组件
func startSymbolRuntime(
	ctx context.Context,
	baseCfg *config.Config,
	symCfg config.SymbolConfig,
	eventBus *event.EventBus,
	storageService *storage.StorageService,
	distributedLock lock.DistributedLock,
) (*SymbolRuntime, error) {
	// 为该交易对构造局部配置（避免修改全局 cfg）
	localCfg := *baseCfg
	localCfg.App.CurrentExchange = symCfg.Exchange
	localCfg.Trading.Symbol = symCfg.Symbol
	localCfg.Trading.PriceInterval = symCfg.PriceInterval
	localCfg.Trading.OrderQuantity = symCfg.OrderQuantity
	localCfg.Trading.MinOrderValue = symCfg.MinOrderValue
	localCfg.Trading.BuyWindowSize = symCfg.BuyWindowSize
	localCfg.Trading.SellWindowSize = symCfg.SellWindowSize
	localCfg.Trading.ReconcileInterval = symCfg.ReconcileInterval
	localCfg.Trading.OrderCleanupThreshold = symCfg.OrderCleanupThreshold
	localCfg.Trading.CleanupBatchSize = symCfg.CleanupBatchSize
	localCfg.Trading.MarginLockDurationSec = symCfg.MarginLockDurationSec
	localCfg.Trading.PositionSafetyCheck = symCfg.PositionSafetyCheck

	// 创建交易所实例
	ex, err := exchange.NewExchange(&localCfg, symCfg.Exchange, symCfg.Symbol)
	if err != nil {
		return nil, fmt.Errorf("创建交易所实例失败(%s:%s): %w", symCfg.Exchange, symCfg.Symbol, err)
	}
	logger.Info("✅ [%s] 交易所实例已创建 (symbol=%s)", ex.GetName(), symCfg.Symbol)

	// API 权限安全检测
	logger.Info("🔐 [%s:%s] 开始检测 API 权限...", symCfg.Exchange, symCfg.Symbol)
	permCheckCtx, permCheckCancel := context.WithTimeout(ctx, 10*time.Second)
	defer permCheckCancel()

	if checker, ok := ex.(exchange.PermissionChecker); ok {
		permissions, err := checker.CheckAPIPermissions(permCheckCtx)
		if err != nil {
			logger.Warn("⚠️ [%s:%s] API 权限检测失败: %v (将继续启动)", symCfg.Exchange, symCfg.Symbol, err)
		} else {
			// 检查是否安全
			if !permissions.IsSecure() {
				logger.Error("🚨 [%s:%s] API 密钥存在安全风险！", symCfg.Exchange, symCfg.Symbol)
				warnings := permissions.GetWarnings()
				for _, warning := range warnings {
					logger.Error("   %s", warning)
				}
				// 可以选择是否继续启动，这里我们记录错误但继续
				logger.Warn("⚠️ [%s:%s] 尽管存在安全风险，系统仍将继续启动。强烈建议修改 API 权限设置！", symCfg.Exchange, symCfg.Symbol)
			} else {
				logger.Info("✅ [%s:%s] API 权限检测通过 (安全评分: %d/100, 风险等级: %s)",
					symCfg.Exchange, symCfg.Symbol, permissions.SecurityScore, permissions.RiskLevel)

				// 显示建议
				warnings := permissions.GetWarnings()
				if len(warnings) > 0 {
					for _, warning := range warnings {
						logger.Info("   %s", warning)
					}
				}
			}
		}
	} else {
		logger.Info("ℹ️ [%s:%s] 该交易所暂不支持自动权限检测，请手动确认 API 权限设置", symCfg.Exchange, symCfg.Symbol)
	}

	// 价格监控
	priceMonitor := monitor.NewPriceMonitor(
		ex,
		symCfg.Symbol,
		localCfg.Timing.PriceSendInterval,
	)

	logger.Info("🔗 [%s] 启动 WebSocket 价格流...", symCfg.Symbol)
	if err := priceMonitor.Start(); err != nil {
		return nil, fmt.Errorf("启动价格流失败(%s:%s): %w", symCfg.Exchange, symCfg.Symbol, err)
	}

	// 等待初始价格
	pollInterval := time.Duration(localCfg.Timing.PricePollInterval) * time.Millisecond
	currentPrice := 0.0
	currentPriceStr := ""
	for i := 0; i < 10; i++ {
		currentPrice = priceMonitor.GetLastPrice()
		currentPriceStr = priceMonitor.GetLastPriceString()
		if currentPrice > 0 {
			break
		}
		time.Sleep(pollInterval)
	}
	if currentPrice <= 0 {
		return nil, fmt.Errorf("无法获取初始价格(%s:%s)", symCfg.Exchange, symCfg.Symbol)
	}

	// 精度
	priceDecimals := ex.GetPriceDecimals()
	quantityDecimals := ex.GetQuantityDecimals()
	logger.Info("ℹ️ [%s] 精度 - 价格:%d 数量:%d", symCfg.Symbol, priceDecimals, quantityDecimals)

	// 获取交易手续费率
	// 币安期货API不提供获取手续费率的接口，因此使用以下策略：
	// 1. 如果配置文件中设置了费率且不为0，使用配置值
	// 2. 否则使用币安期货默认Taker费率（0.04%）作为保守估计
	configFeeRate := baseCfg.Exchanges[symCfg.Exchange].FeeRate
	feeRate := configFeeRate

	if symCfg.Exchange == "binance" {
		// 币安期货默认费率：Maker 0.02%, Taker 0.04%
		// 网格策略使用限价单，通常作为Maker成交，但为保守起见使用Taker费率
		defaultBinanceTakerFee := 0.0004 // 0.04%

		if configFeeRate == 0 {
			// 配置文件中未设置或设置为0，使用默认Taker费率
			feeRate = defaultBinanceTakerFee
			logger.Info("💳 [%s] 配置文件未设置手续费率，使用币安期货默认Taker费率: %.4f%%", symCfg.Symbol, feeRate*100)
		} else {
			// 使用配置文件中的费率
			logger.Info("💳 [%s] 使用配置文件中的手续费率: %.4f%%", symCfg.Symbol, feeRate*100)
		}
		logger.Info("ℹ️ [%s] 提示：币安期货实际费率取决于您的VIP等级，请在配置文件中设置准确的费率", symCfg.Symbol)
	} else {
		logger.Info("💳 [%s] 使用配置文件中的手续费率: %.4f%%", symCfg.Symbol, feeRate*100)
	}

	// 持仓安全性检查
	maxLeverage := baseCfg.RiskControl.MaxLeverage
	if err := safety.CheckAccountSafety(
		ex,
		symCfg.Symbol,
		currentPrice,
		symCfg.OrderQuantity,
		symCfg.PriceInterval,
		feeRate,
		symCfg.PositionSafetyCheck,
		priceDecimals,
		maxLeverage,
	); err != nil {
		return nil, fmt.Errorf("持仓安全性检查失败(%s:%s): %w", symCfg.Exchange, symCfg.Symbol, err)
	}
	logger.Info("✅ [%s] 持仓安全性检查通过", symCfg.Symbol)

	// 核心组件
	exchangeExecutor := order.NewExchangeOrderExecutor(
		ex,
		symCfg.Symbol,
		localCfg.Timing.RateLimitRetryDelay,
		localCfg.Timing.OrderRetryDelay,
		distributedLock,
	)
	executorAdapter := &exchangeExecutorAdapter{
		executor: exchangeExecutor,
		eventBus: eventBus,
		symbol:   symCfg.Symbol,
	}
	exchangeAdapter := &positionExchangeAdapter{exchange: ex}

	superPositionManager := position.NewSuperPositionManager(&localCfg, executorAdapter, exchangeAdapter, priceDecimals, quantityDecimals)
	if storageService != nil {
		tradeStorageAdapter := &tradeStorageAdapter{storageService: storageService}
		superPositionManager.SetTradeStorage(tradeStorageAdapter)
	}
	// 设置事件总线（用于发送告警）
	if eventBus != nil {
		superPositionManager.SetEventBus(eventBus)
	}

	riskMonitor := safety.NewRiskMonitor(&localCfg, ex)
	if storageService != nil {
		riskMonitor.SetStorage(storageService.GetStorage())
	}

	reconciler := safety.NewReconciler(&localCfg, exchangeAdapter, superPositionManager, distributedLock)
	reconciler.SetPauseChecker(func() bool {
		return riskMonitor.IsTriggered()
	})
	if storageService != nil {
		reconciler.SetStorage(&reconciliationStorageAdapter{storageService: storageService})
	}

	// 订单流
	if err := ex.StartOrderStream(ctx, func(updateInterface interface{}) {
		posUpdate := toPositionOrderUpdate(updateInterface)
		if posUpdate == nil {
			return
		}

		// 🔥 关键修复：过滤掉不属于当前交易对的订单更新
		// 币安的 WebSocket 订单流是全局的，会推送所有交易对的订单
		// 必须检查 Symbol 是否匹配，避免不同交易对的订单互相干扰
		if posUpdate.Symbol != symCfg.Symbol {
			logger.Debug("⏭️ [订单过滤] 跳过其他交易对的订单: Symbol=%s (当前交易对: %s), ClientOID=%s",
				posUpdate.Symbol, symCfg.Symbol, posUpdate.ClientOrderID)
			return
		}

		// 发布订单事件
		if eventBus != nil && posUpdate.Symbol != "" {
			var eventType event.EventType
			switch posUpdate.Status {
			case "FILLED":
				eventType = event.EventTypeOrderFilled
			case "CANCELED":
				eventType = event.EventTypeOrderCanceled
			}
			if eventType != "" {
				eventBus.Publish(&event.Event{
					Type: eventType,
					Data: map[string]interface{}{
						"order_id":        posUpdate.OrderID,
						"client_order_id": posUpdate.ClientOrderID,
						"symbol":          posUpdate.Symbol,
						"side":            posUpdate.Side,
						"price":           posUpdate.Price,
						"executed_qty":    posUpdate.ExecutedQty,
						"status":          posUpdate.Status,
					},
				})
			}
		}

		superPositionManager.OnOrderUpdate(*posUpdate)
	}); err != nil {
		logger.Warn("⚠️ [%s] 启动订单流失败: %v", symCfg.Symbol, err)
	}

	if err := superPositionManager.Initialize(currentPrice, currentPriceStr); err != nil {
		return nil, fmt.Errorf("初始化仓位管理器失败(%s:%s): %w", symCfg.Exchange, symCfg.Symbol, err)
	}

	if storageService != nil {
		if st := storageService.GetStorage(); st != nil {
			restoreAdapter := &reconciliationRestoreAdapter{storage: st}
			if err := superPositionManager.RestoreReconciliationStats(restoreAdapter, symCfg.Symbol); err != nil {
				logger.Warn("⚠️ [%s] 恢复对账统计失败: %v", symCfg.Symbol, err)
			}
		}
	}

	reconciler.Start(ctx)

	orderCleaner := safety.NewOrderCleaner(&localCfg, exchangeExecutor, superPositionManager)
	orderCleaner.Start(ctx)

	go riskMonitor.Start(ctx)

	// 可选组件
	var dynamicAdjuster *strategy.DynamicAdjuster
	if localCfg.Trading.DynamicAdjustment.Enabled {
		dynamicAdjuster = strategy.NewDynamicAdjuster(&localCfg, priceMonitor, superPositionManager)
		dynamicAdjuster.Start()
	}

	var trendDetector *strategy.TrendDetector
	if localCfg.Trading.SmartPosition.Enabled || localCfg.Trading.GridRiskControl.TrendFilterEnabled {
		trendDetector = strategy.NewTrendDetector(&localCfg, priceMonitor)
		trendDetector.Start()
		// 将趋势检测器注入 SuperPositionManager
		superPositionManager.SetTrendDetector(trendDetector)
	}

	var strategyManager *strategy.StrategyManager
	var multiExecutor *strategy.MultiStrategyExecutor
	if localCfg.Strategies.Enabled {
		totalCapital := localCfg.Strategies.CapitalAllocation.TotalCapital
		if totalCapital <= 0 {
			balance, err := ex.GetBalance(ctx, "USDT")
			if err == nil && balance > 0 {
				totalCapital = balance
				logger.Info("💰 [%s] 从账户获取总资金: %.2f USDT", symCfg.Symbol, totalCapital)
			} else {
				totalCapital = 5000
				logger.Warn("⚠️ [%s] 无法获取账户余额，使用默认总资金: %.2f USDT", symCfg.Symbol, totalCapital)
			}
		}

		strategyManager = strategy.NewStrategyManager(&localCfg, totalCapital)
		multiExecutor = strategy.NewMultiStrategyExecutor(exchangeExecutor, strategyManager.GetCapitalAllocator())

		if gridCfg, exists := localCfg.Strategies.Configs["grid"]; exists && gridCfg.Enabled {
			gridStrategy := strategy.NewGridStrategy("grid", &localCfg, executorAdapter, exchangeAdapter, superPositionManager)
			fixedPool := 0.0
			if pool, ok := gridCfg.Config["capital_pool"].(float64); ok {
				fixedPool = pool
			}
			strategyManager.RegisterStrategy("grid", gridStrategy, gridCfg.Weight, fixedPool)
			logger.Info("✅ [%s] 网格策略已注册", symCfg.Symbol)
		}

		if trendCfg, exists := localCfg.Strategies.Configs["trend"]; exists && trendCfg.Enabled {
			trendExecutor := strategy.NewMultiStrategyExecutorAdapter(multiExecutor, "trend")
			trendStrategy := strategy.NewTrendFollowingStrategy("trend", &localCfg, trendExecutor, exchangeAdapter, trendCfg.Config)
			fixedPool := 0.0
			if pool, ok := trendCfg.Config["capital_pool"].(float64); ok {
				fixedPool = pool
			}
			strategyManager.RegisterStrategy("trend", trendStrategy, trendCfg.Weight, fixedPool)
			logger.Info("✅ [%s] 趋势策略已注册", symCfg.Symbol)
		}

		if meanCfg, exists := localCfg.Strategies.Configs["mean_reversion"]; exists && meanCfg.Enabled {
			meanExecutor := strategy.NewMultiStrategyExecutorAdapter(multiExecutor, "mean_reversion")
			meanStrategy := strategy.NewMeanReversionStrategy("mean_reversion", &localCfg, meanExecutor, exchangeAdapter, meanCfg.Config)
			fixedPool := 0.0
			if pool, ok := meanCfg.Config["capital_pool"].(float64); ok {
				fixedPool = pool
			}
			strategyManager.RegisterStrategy("mean_reversion", meanStrategy, meanCfg.Weight, fixedPool)
			logger.Info("✅ [%s] 均值回归策略已注册", symCfg.Symbol)
		}

		if momentumCfg, exists := localCfg.Strategies.Configs["momentum"]; exists && momentumCfg.Enabled {
			momentumExecutor := strategy.NewMultiStrategyExecutorAdapter(multiExecutor, "momentum")
			momentumStrategy := strategy.NewMomentumStrategy("momentum", &localCfg, momentumExecutor, exchangeAdapter, momentumCfg.Config)
			fixedPool := 0.0
			if pool, ok := momentumCfg.Config["capital_pool"].(float64); ok {
				fixedPool = pool
			}
			strategyManager.RegisterStrategy("momentum", momentumStrategy, momentumCfg.Weight, fixedPool)
			logger.Info("✅ [%s] 动量策略已注册", symCfg.Symbol)
		}

		if err := strategyManager.StartAll(); err != nil {
			logger.Error("❌ [%s] 启动策略管理器失败: %v", symCfg.Symbol, err)
		} else {
			logger.Info("✅ [%s] 多策略系统已启动", symCfg.Symbol)
		}
	}

	// 价格变动处理
	go func() {
		priceCh := priceMonitor.Subscribe()
		var lastTriggered bool
		for priceChange := range priceCh {
			isTriggered := riskMonitor.IsTriggered()
			if isTriggered {
				if !lastTriggered {
					logger.Warn("🚨 [%s][风控触发] 撤销所有买单并暂停交易...", symCfg.Symbol)
					superPositionManager.CancelAllBuyOrders()
					lastTriggered = true
					if eventBus != nil {
						eventBus.Publish(&event.Event{
							Type: event.EventTypeRiskTriggered,
							Data: map[string]interface{}{
								"symbol": symCfg.Symbol,
								"price":  priceChange.NewPrice,
							},
						})
					}
				}
				continue
			}

			if lastTriggered {
				logger.Info("✅ [%s][风控解除] 恢复自动交易", symCfg.Symbol)
				lastTriggered = false
				if eventBus != nil {
					eventBus.Publish(&event.Event{
						Type: event.EventTypeRiskRecovered,
						Data: map[string]interface{}{
							"symbol": symCfg.Symbol,
							"price":  priceChange.NewPrice,
						},
					})
				}
			}

			if strategyManager != nil {
				strategyManager.OnPriceChange(priceChange.NewPrice)
			}

			if trendDetector != nil && localCfg.Trading.SmartPosition.WindowAdjustment.Enabled {
				buyWindow, sellWindow := trendDetector.AdjustWindows()
				origBuy, origSell := localCfg.Trading.BuyWindowSize, localCfg.Trading.SellWindowSize
				localCfg.Trading.BuyWindowSize = buyWindow
				localCfg.Trading.SellWindowSize = sellWindow
				if err := superPositionManager.AdjustOrders(priceChange.NewPrice); err != nil {
					logger.Error("❌ [%s] 调整订单失败: %v", symCfg.Symbol, err)
				}
				localCfg.Trading.BuyWindowSize = origBuy
				localCfg.Trading.SellWindowSize = origSell
			} else {
				if strategyManager == nil || !localCfg.Strategies.Enabled {
					if err := superPositionManager.AdjustOrders(priceChange.NewPrice); err != nil {
						logger.Error("❌ [%s] 调整订单失败: %v", symCfg.Symbol, err)
					}
				}
			}
		}
	}()

	// 定期打印持仓
	go func() {
		statusInterval := time.Duration(localCfg.Timing.StatusPrintInterval) * time.Minute
		ticker := time.NewTicker(statusInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !riskMonitor.IsTriggered() {
					superPositionManager.PrintPositions()
				}
			}
		}
	}()

	stopFn := func() {
		logger.Info("⏹️ [%s] 停止价格监控...", symCfg.Symbol)
		if priceMonitor != nil {
			priceMonitor.Stop()
		}
		logger.Info("⏹️ [%s] 停止订单流...", symCfg.Symbol)
		ex.StopOrderStream()
		logger.Info("⏹️ [%s] 停止风控监视器...", symCfg.Symbol)
		if riskMonitor != nil {
			riskMonitor.Stop()
		}
		if dynamicAdjuster != nil {
			dynamicAdjuster.Stop()
		}
		if trendDetector != nil {
			trendDetector.Stop()
		}
		if strategyManager != nil {
			strategyManager.StopAll()
		}
	}

	return &SymbolRuntime{
		Config:               symCfg,
		Exchange:             ex,
		PriceMonitor:         priceMonitor,
		RiskMonitor:          riskMonitor,
		SuperPositionManager: superPositionManager,
		OrderCleaner:         orderCleaner,
		Reconciler:           reconciler,
		TrendDetector:        trendDetector,
		DynamicAdjuster:      dynamicAdjuster,
		StrategyManager:      strategyManager,
		ExchangeExecutor:     exchangeExecutor,
		ExecutorAdapter:      executorAdapter,
		ExchangeAdapter:      exchangeAdapter,
		EventBus:             eventBus,
		StorageService:       storageService,
		Stop:                 stopFn,
	}, nil
}

// toPositionOrderUpdate 提取订单更新为 position.OrderUpdate
func toPositionOrderUpdate(updateInterface interface{}) *position.OrderUpdate {
	v := reflect.ValueOf(updateInterface)
	if !v.IsValid() || v.Kind() != reflect.Struct {
		logger.Warn("⚠️ [symbol_manager] 订单更新不是结构体类型: %T", updateInterface)
		return nil
	}

	getInt64Field := func(name string) int64 {
		field := v.FieldByName(name)
		if field.IsValid() && field.CanInt() {
			return field.Int()
		}
		return 0
	}

	getStringField := func(name string) string {
		field := v.FieldByName(name)
		if field.IsValid() && field.Kind() == reflect.String {
			return field.String()
		}
		return ""
	}

	getFloat64Field := func(name string) float64 {
		field := v.FieldByName(name)
		if field.IsValid() && field.CanFloat() {
			return field.Float()
		}
		return 0.0
	}

	return &position.OrderUpdate{
		OrderID:       getInt64Field("OrderID"),
		ClientOrderID: getStringField("ClientOrderID"),
		Symbol:        getStringField("Symbol"),
		Status:        getStringField("Status"),
		ExecutedQty:   getFloat64Field("ExecutedQty"),
		Price:         getFloat64Field("Price"),
		AvgPrice:      getFloat64Field("AvgPrice"),
		Side:          getStringField("Side"),
		Type:          getStringField("Type"),
		UpdateTime:    getInt64Field("UpdateTime"),
	}
}
