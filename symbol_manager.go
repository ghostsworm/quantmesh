package main

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"quantmesh/arbitrage"
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
	"quantmesh/utils"
	"quantmesh/web"
)

// SymbolManager 管理多個 SymbolRuntime（委託給 BotManager 實現）
type SymbolManager struct {
	botManager *BotManager
}

// NewSymbolManager 創建管理器（內部創建 BotManager，需傳入完整依賴）
func NewSymbolManager(cfg *config.Config, eventBus *event.EventBus, storageService *storage.StorageService, distributedLock lock.DistributedLock) *SymbolManager {
	return &SymbolManager{
		botManager: NewBotManager(cfg, eventBus, storageService, distributedLock),
	}
}

// SymbolRuntime 代表單個交易所/交易對的运行時组件集合
type SymbolRuntime struct {
	Config               config.SymbolConfig
	Exchange             exchange.IExchange
	PriceMonitor         *monitor.PriceMonitor
	RiskMonitor          *safety.RiskMonitor
	DepthMonitor         *safety.DepthMonitor
	FundingMonitor       *safety.FundingRateMonitor
	ArbitrageManager     *arbitrage.FundingArbitrageManager
	SuperPositionManager *position.SuperPositionManager
	OpeningController    *position.OpeningController
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
	AccountID            string // 账戶標识
	Stop                 func()
}

// runtimeKey 生成唯一键（exchange:symbol:market_type）
func runtimeKey(exchangeName, symbol string, marketType ...string) string {
	mt := "futures"
	if len(marketType) > 0 && marketType[0] != "" {
		mt = marketType[0]
	}
	return config.GenerateBotID(exchangeName, symbol, mt)
}

// Add 注册运行時（包裝為 BotRuntime 後加入 BotManager）
func (sm *SymbolManager) Add(rt *SymbolRuntime) {
	exCfg, _ := sm.botManager.cfg.Exchanges[rt.Config.Exchange]
	botCfg := config.SymbolConfigToBotConfig(rt.Config, exCfg.Testnet)
	// 使用 botCfg.ID，优先使用配置中的自定义 ID（如 UUID），否则使用生成的标准格式 ID
	botID := botCfg.ID
	if botID == "" {
		botID = config.GenerateBotID(rt.Config.Exchange, rt.Config.Symbol, rt.Config.GetMarketType())
	}
	br := &BotRuntime{Config: botCfg, BotID: botID, Inner: rt, EventBus: sm.botManager.eventBus}
	sm.botManager.AddRuntime(br)
}

// Get 獲取运行時（委託 BotManager）
func (sm *SymbolManager) Get(exchangeName, symbol string, marketType ...string) (*SymbolRuntime, bool) {
	br, ok := sm.botManager.GetByExchangeSymbol(exchangeName, symbol, marketType...)
	if !ok || br.Inner == nil {
		return nil, false
	}
	return br.Inner, true
}

// List 列出所有运行時（委託 BotManager）
func (sm *SymbolManager) List() []*SymbolRuntime {
	return sm.botManager.ListSymbolRuntimes()
}

// Remove 從管理器中移除运行時（委託 BotManager）
func (sm *SymbolManager) Remove(exchangeName, symbol string, marketType ...string) {
	botID := runtimeKey(exchangeName, symbol, marketType...)
	sm.botManager.Remove(botID)
}

// StopAll 停止所有运行時（委託 BotManager）
func (sm *SymbolManager) StopAll() {
	sm.botManager.StopAll()
}

// UpdateRuntimeTradingParams 更新运行中的交易對的交易参數（热更新，委託 BotManager）
func (sm *SymbolManager) UpdateRuntimeTradingParams(latestCfg *config.Config) (updatedSymbols []string) {
	return sm.botManager.UpdateRuntimeTradingParams(latestCfg)
}

// StartBot 啟動指定 Bot（委託 BotManager）
func (sm *SymbolManager) StartBot(ctx context.Context, botCfg config.BotConfig) (*BotRuntime, error) {
	return sm.botManager.StartBot(ctx, botCfg)
}

// GetBotManager 返回底層 BotManager（供 Web API 等使用）
func (sm *SymbolManager) GetBotManager() *BotManager {
	return sm.botManager
}

// selectProfile 根据资金费率和手续费率选择配置档案
func selectProfile(ctx context.Context, symCfg config.SymbolConfig, ex exchange.IExchange, feeRate float64, storageService *storage.StorageService) (config.ProfileConfig, string) {
	// 如果没有配置 profiles，返回空配置（使用主配置）
	if len(symCfg.Profiles) == 0 {
		return config.ProfileConfig{}, ""
	}

	// 获取资金费率（仅对合约有效）
	var fundingRate float64
	if symCfg.GetMarketType() == "futures" {
		// 优先从存储服务获取最新资金费率
		if storageService != nil {
			if st := storageService.GetStorage(); st != nil {
				if latestRate, err := st.GetLatestFundingRate(symCfg.Symbol, symCfg.Exchange); err == nil {
					fundingRate = latestRate
				}
			}
		}
		// 如果存储中没有，尝试从交易所实时获取
		if fundingRate == 0 && ex != nil {
			rateCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
			if rate, err := ex.GetFundingRate(rateCtx, symCfg.Symbol); err == nil {
				fundingRate = rate
			}
			cancel()
		}
	}

	// 根据切换规则选择 profile
	// 优先级：资金费率 > 手续费率
	if symCfg.SwitchRules.FundingRate.Threshold != 0 && symCfg.GetMarketType() == "futures" {
		if fundingRate >= symCfg.SwitchRules.FundingRate.Threshold {
			// 资金费率为正，选择 positive profile
			if profile, exists := symCfg.Profiles["positive"]; exists {
				return profile, "positive"
			}
		} else if fundingRate < -symCfg.SwitchRules.FundingRate.Threshold {
			// 资金费率为负，选择 negative profile
			if profile, exists := symCfg.Profiles["negative"]; exists {
				return profile, "negative"
			}
		}
	}

	if symCfg.SwitchRules.FeeRate.Threshold != 0 {
		if feeRate >= symCfg.SwitchRules.FeeRate.Threshold {
			// 手续费率较高，选择 positive profile
			if profile, exists := symCfg.Profiles["positive"]; exists {
				return profile, "positive"
			}
		} else if feeRate < symCfg.SwitchRules.FeeRate.Threshold {
			// 手续费率较低，选择 negative profile
			if profile, exists := symCfg.Profiles["negative"]; exists {
				return profile, "negative"
			}
		}
	}

	// 默认使用主配置（返回空 profile）
	return config.ProfileConfig{}, ""
}

// applyProfile 应用选中的 profile 到配置
func applyProfile(symCfg config.SymbolConfig, profile config.ProfileConfig) config.SymbolConfig {
	result := symCfg
	if profile.PriceInterval > 0 {
		result.PriceInterval = profile.PriceInterval
	}
	if profile.ProfitSpread > 0 {
		result.ProfitSpread = profile.ProfitSpread
	}
	if profile.OrderQuantity > 0 {
		result.OrderQuantity = profile.OrderQuantity
	}
	if profile.BuyWindowSize > 0 {
		result.BuyWindowSize = profile.BuyWindowSize
	}
	if profile.SellWindowSize > 0 {
		result.SellWindowSize = profile.SellWindowSize
	}
	if profile.MinOrderValue > 0 {
		result.MinOrderValue = profile.MinOrderValue
	}
	if profile.ProfitSpread > 0 {
		result.ProfitSpread = profile.ProfitSpread
	}
	return result
}

// startSymbolRuntime 啟动單個交易對的核心组件
// onRequestStop: 當關閉條件觸發時調用，用於自動停止 Bot
func startSymbolRuntime(
	ctx context.Context,
	baseCfg *config.Config,
	symCfg config.SymbolConfig,
	eventBus *event.EventBus,
	storageService *storage.StorageService,
	distributedLock lock.DistributedLock,
	onRequestStop func(botID string),
) (*SymbolRuntime, error) {
	if symCfg.GetMarketType() == config.MarketTypeFundingCarry {
		return startFundingCarrySymbolRuntime(ctx, baseCfg, symCfg, eventBus, storageService, distributedLock, onRequestStop)
	}
	if symCfg.GetMarketType() == config.MarketTypeFundingPerpSpread {
		return startFundingPerpSpreadSymbolRuntime(ctx, baseCfg, symCfg, eventBus, storageService, distributedLock, onRequestStop)
	}

	// 獲取交易手续费率（在创建交易所实例之前）
	configFeeRate := baseCfg.Exchanges[symCfg.Exchange].FeeRate
	feeRate := configFeeRate
	if symCfg.Exchange == "binance" && configFeeRate == 0 {
		feeRate = 0.0004 // 币安期货默认Taker费率
	}

	// 创建临时交易所实例用于获取资金费率（如果配置了 profiles）
	var tempEx exchange.IExchange
	if len(symCfg.Profiles) > 0 {
		var err error
		tempEx, err = exchange.NewExchange(baseCfg, symCfg.Exchange, symCfg.Symbol, symCfg.GetMarketType())
		if err != nil {
			logger.Warn("⚠️ [%s:%s] 创建临时交易所实例失败，将使用主配置: %v", symCfg.Exchange, symCfg.Symbol, err)
		} else {
			// 根据费率和手续费率选择 profile
			profile, profileName := selectProfile(ctx, symCfg, tempEx, feeRate, storageService)
			if profileName != "" {
				logger.Info("🔄 [%s:%s] 自动切换到配置档案: %s", symCfg.Exchange, symCfg.Symbol, profileName)
				symCfg = applyProfile(symCfg, profile)
			}
		}
	}

	// 為該交易對構造局部配置（避免修改全局 cfg）
	localCfg := *baseCfg
	localCfg.App.CurrentExchange = symCfg.Exchange
	botID := symCfg.ID
	if botID == "" {
		botID = config.GenerateBotID(symCfg.Exchange, symCfg.Symbol, symCfg.GetMarketType())
	}
	localCfg.Trading.BotID = botID
	localCfg.Trading.Symbol = symCfg.Symbol
	localCfg.Trading.MarketType = symCfg.GetMarketType()
	localCfg.Trading.PriceInterval = symCfg.PriceInterval
	localCfg.Trading.ProfitSpread = symCfg.ProfitSpread
	localCfg.Trading.OrderQuantity = symCfg.OrderQuantity
	localCfg.Trading.MinOrderValue = symCfg.MinOrderValue
	localCfg.Trading.BuyWindowSize = symCfg.BuyWindowSize
	localCfg.Trading.SellWindowSize = symCfg.SellWindowSize
	localCfg.Trading.ReconcileInterval = symCfg.ReconcileInterval
	localCfg.Trading.OrderCleanupThreshold = symCfg.OrderCleanupThreshold
	localCfg.Trading.CleanupBatchSize = symCfg.CleanupBatchSize
	localCfg.Trading.MarginLockDurationSec = symCfg.MarginLockDurationSec
	localCfg.Trading.PositionSafetyCheck = symCfg.PositionSafetyCheck
	localCfg.Trading.Direction = symCfg.GetDirection()
	localCfg.Trading.PriceLow = symCfg.PriceLow
	localCfg.Trading.PriceHigh = symCfg.PriceHigh
	localCfg.Trading.TriggerPrice = symCfg.TriggerPrice
	localCfg.Trading.GridMode = symCfg.GridMode
	if localCfg.Trading.GridMode == "" {
		localCfg.Trading.GridMode = "arithmetic"
	}
	localCfg.Trading.GridShiftEnabled = symCfg.GridShiftEnabled
	localCfg.Trading.GridShiftStep = symCfg.GridShiftStep
	localCfg.Trading.RocketTieredGrid = symCfg.RocketTieredGrid
	localCfg.Trading.CloseOnStop = symCfg.CloseOnStop
	localCfg.Trading.GridRiskControl = symCfg.GridRiskControl
	localCfg.Trading.SmartOrder = symCfg.SmartOrder
	if symCfg.SmartOrder.Enabled && symCfg.SmartOrder.MaxOpenOrders <= 0 {
		localCfg.Trading.SmartOrder.MaxOpenOrders = 3
	}
	if symCfg.SmartOrder.OpenOrderDistance <= 0 && symCfg.SmartOrder.Enabled {
		localCfg.Trading.SmartOrder.OpenOrderDistance = 5
	}

	// 將 Bot 上的 strategies 合并到本交易对 localCfg，使实盘与 API 策略类型（如 trend_following）一致
	config.ApplyBotStrategiesToLocalConfig(&localCfg, &symCfg)

	// 創建交易所實例（根據交易對配置的市场類型：spot / futures）
	// 如果之前创建了临时实例，重用它；否则创建新实例
	var ex exchange.IExchange
	var err error
	if tempEx != nil {
		ex = tempEx
		logger.Info("✅ [%s] 重用交易所實例 (symbol=%s)", ex.GetName(), symCfg.Symbol)
	} else {
		ex, err = exchange.NewExchange(&localCfg, symCfg.Exchange, symCfg.Symbol, symCfg.GetMarketType())
		if err != nil {
			return nil, fmt.Errorf("創建交易所實例失败(%s:%s): %w", symCfg.Exchange, symCfg.Symbol, err)
		}
		logger.Info("✅ [%s] 交易所實例已創建 (symbol=%s)", ex.GetName(), symCfg.Symbol)
	}

	// API 权限安全检测
	logger.Info("🔐 [%s:%s] 开始检测 API 权限...", symCfg.Exchange, symCfg.Symbol)
	permCheckCtx, permCheckCancel := context.WithTimeout(ctx, 10*time.Second)
	defer permCheckCancel()

	if checker, ok := ex.(exchange.PermissionChecker); ok {
		permissions, err := checker.CheckAPIPermissions(permCheckCtx)
		if err != nil {
			logger.Warn("⚠️ [%s:%s] API 权限检测失败: %v (將继续啟动)", symCfg.Exchange, symCfg.Symbol, err)
		} else {
			// 检查是否安全
			if !permissions.IsSecure() {
				logger.Error("🚨 [%s:%s] API 密钥存在安全风險！", symCfg.Exchange, symCfg.Symbol)
				warnings := permissions.GetWarnings()
				for _, warning := range warnings {
					logger.Error("   %s", warning)
				}
				// 可以选擇是否继续啟动，这里我们記錄錯误但继续
				logger.Warn("⚠️ [%s:%s] 尽管存在安全风險，系统仍將继续啟动。强烈建议修改 API 权限設置！", symCfg.Exchange, symCfg.Symbol)
			} else {
				logger.Info("✅ [%s:%s] API 权限检测通過 (安全评分: %d/100, 风險等级: %s)",
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
		logger.Info("ℹ️ [%s:%s] 該交易所暫不支援自动权限检测，请手动确认 API 权限設置", symCfg.Exchange, symCfg.Symbol)
	}

	// 價格監控
	priceMonitor := monitor.NewPriceMonitor(
		ex,
		symCfg.Symbol,
		localCfg.Timing.PriceSendInterval,
	)

	logger.Info("🔗 [%s] 啟动 WebSocket 價格流...", symCfg.Symbol)
	if err := priceMonitor.Start(); err != nil {
		return nil, fmt.Errorf("啟動價格流失败(%s:%s): %w", symCfg.Exchange, symCfg.Symbol, err)
	}

	// 等待初始價格
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
		return nil, fmt.Errorf("無法獲取初始價格(%s:%s)", symCfg.Exchange, symCfg.Symbol)
	}

	// 精度
	priceDecimals := ex.GetPriceDecimals()
	quantityDecimals := ex.GetQuantityDecimals()
	logger.Info("ℹ️ [%s] 精度 - 價格:%d 數量:%d", symCfg.Symbol, priceDecimals, quantityDecimals)

	// 使用之前获取的手续费率（已在创建交易所实例前获取）
	if symCfg.Exchange == "binance" && feeRate == 0 {
		logger.Info("💳 [%s] 配置文件未設置手续费率，使用币安期货默认Taker费率: %.4f%%", symCfg.Symbol, feeRate*100)
		logger.Info("ℹ️ [%s] 提示：币安期货實際费率取决於您的VIP等级，请在配置文件中設置准确的费率", symCfg.Symbol)
	} else {
		logger.Info("💳 [%s] 使用配置文件中的手续费率: %.4f%%", symCfg.Symbol, feeRate*100)
	}

	// 持倉安全性检查
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
		return nil, fmt.Errorf("持倉安全性检查失败(%s): %w", botID, err)
	}
	logger.Info("✅ [%s] 持倉安全性检查通過", botID)

	// 核心组件
	// 生成账戶標识（使用 API Key 的前 8 位）
	accountID := ""
	if exCfg, ok := baseCfg.Exchanges[symCfg.Exchange]; ok {
		if len(exCfg.APIKey) > 8 {
			accountID = exCfg.APIKey[:8]
		} else {
			accountID = exCfg.APIKey
		}
	}

	exchangeExecutor := order.NewExchangeOrderExecutor(
		ex,
		symCfg.Symbol,
		localCfg.Timing.RateLimitRetryDelay,
		localCfg.Timing.OrderRetryDelay,
		distributedLock,
	)
	executorAdapter := &exchangeExecutorAdapter{
		executor:  exchangeExecutor,
		eventBus:  eventBus,
		symbol:    symCfg.Symbol,
		exchange:  symCfg.Exchange,
		accountID: accountID,
	}
	exchangeAdapter := &positionExchangeAdapter{exchange: ex}

	superPositionManager := position.NewSuperPositionManager(&localCfg, executorAdapter, exchangeAdapter, priceDecimals, quantityDecimals)
	if storageService != nil {
		tradeStorageAdapter := &tradeStorageAdapter{
			storageService: storageService,
			accountID:      accountID,
		}
		superPositionManager.SetTradeStorage(tradeStorageAdapter)
	}
	// 設置事件總線（用於发送告警）
	if eventBus != nil {
		superPositionManager.SetEventBus(eventBus)
	}
	// 設置關閉條件觸發時的回調（滿足盈利率目標或虧損限制時自動停止 Bot）
	if onRequestStop != nil {
		superPositionManager.SetRequestStopFunc(func() {
			go onRequestStop(botID)
		})
	}

	// 風控監控默認綁定到當前 runtime 的交易對，避免多 Bot/多交易對互相影響
	runtimeRiskCfg := localCfg
	runtimeRiskCfg.RiskControl.MonitorSymbols = []string{symCfg.Symbol}
	riskMonitor := safety.NewRiskMonitor(&runtimeRiskCfg, ex)
	if storageService != nil {
		riskMonitor.SetStorage(storageService.GetStorage())
	}

	// 創建深度監控器
	depthMonitor := safety.NewDepthMonitor(&runtimeRiskCfg, ex)

	// 創建資金費率監控器（僅對合約啟用）
	var fundingMonitor *safety.FundingRateMonitor
	var arbitrageManager *arbitrage.FundingArbitrageManager
	if ex.GetMarketType() == "futures" && localCfg.FundingRate.Enabled {
		fundingMonitor = safety.NewFundingRateMonitor(&localCfg, ex, symCfg.Symbol)

		// 設置資金費率監控器到網格管理器
		superPositionManager.SetFundingMonitor(fundingMonitor)

		// 如果啟用了期現套利，創建套利管理器
		if localCfg.FundingRate.ArbitrageEnabled {
			// 創建現貨交易所實例
			spotEx, err := exchange.NewExchange(&localCfg, symCfg.Exchange, symCfg.Symbol, "spot")
			if err != nil {
				logger.Warn("⚠️ 創建現貨交易所失敗，跳過期現套利: %v", err)
			} else {
				// 創建交易所適配器
				futuresAdapter := &arbitrageExchangeAdapter{exchange: ex}
				spotAdapter := &arbitrageExchangeAdapter{exchange: spotEx}

				arbitrageManager = arbitrage.NewFundingArbitrageManager(&localCfg, futuresAdapter, spotAdapter, fundingMonitor, symCfg.Symbol)

				// 連接套利管理器到網格管理器
				superPositionManager.SetArbitrageManager(arbitrageManager)
				logger.Info("💱 期現套利管理器已創建並連接到網格管理器")
			}
		}

		logger.Info("💰 資金費率監控器已創建")
	}

	reconciler := safety.NewReconciler(&localCfg, exchangeAdapter, superPositionManager, distributedLock)
	reconciler.SetPauseChecker(func() bool {
		// 检查市场异动风控或深度风控是否触发
		return riskMonitor.IsTriggered() || depthMonitor.IsTriggered()
	})
	if storageService != nil {
		reconciler.SetStorage(&reconciliationStorageAdapter{
			storageService: storageService,
			accountID:      accountID,
			exchange:       symCfg.Exchange,
		})
	}

	// 提前宣告，供訂單流回調在成交/取消時通知策略並釋放預留資金（閉包可引用）
	var strategyManager *strategy.StrategyManager
	var multiExecutor *strategy.MultiStrategyExecutor

	// 訂單流
	if err := ex.StartOrderStream(ctx, func(updateInterface interface{}) {
		posUpdate := toPositionOrderUpdate(updateInterface)
		if posUpdate == nil {
			return
		}

		// 🔥 关键修複：過滤掉不属於當前交易對的订單更新
		// 币安的 WebSocket 訂單流是全局的，會推送所有交易對的订單
		// 必須检查 Symbol 是否匹配，避免不同交易對的订單互相干扰
		if posUpdate.Symbol != symCfg.Symbol {
			logger.Debug("⏭️ [订單過滤] 跳過其他交易對的订單: Symbol=%s (當前交易對: %s), ClientOID=%s",
				posUpdate.Symbol, symCfg.Symbol, posUpdate.ClientOrderID)
			return
		}

		// 发布订單事件
		if eventBus != nil && posUpdate.Symbol != "" {
			var eventType event.EventType
			switch posUpdate.Status {
			case "FILLED":
				eventType = event.EventTypeOrderFilled
			case "CANCELED":
				eventType = event.EventTypeOrderCanceled
			}
			if eventType != "" {
				evtData := map[string]interface{}{
					"order_id":        posUpdate.OrderID,
					"client_order_id": posUpdate.ClientOrderID,
					"symbol":          posUpdate.Symbol,
					"side":            posUpdate.Side,
					"price":           posUpdate.Price,
					"quantity":        posUpdate.ExecutedQty, // FILLED 时 quantity == executed_qty
					"executed_qty":    posUpdate.ExecutedQty,
					"status":          posUpdate.Status,
					"type":            posUpdate.Type,
					"realized_pnl":    posUpdate.RealizedPnL,
					"exchange":        symCfg.Exchange,
					"account":         accountID,
					"bot_id":          localCfg.Trading.BotID,
					"market_type":     localCfg.Trading.MarketType,
					"order_source":    utils.ParseOrderSource(posUpdate.ClientOrderID), // 從 ClientOrderID 解析（如 _SL=止損）
				}
				if eventType == event.EventTypeOrderFilled && superPositionManager != nil {
					evtData["position"] = superPositionManager.GetTotalBuyQty() - superPositionManager.GetTotalSellQty()
					evtData["filled_layers"] = superPositionManager.GetActiveLayers()
				}
				eventBus.Publish(&event.Event{
					Type: eventType,
					Data: evtData,
				})
			}
		}

		superPositionManager.OnOrderUpdate(*posUpdate)
		// 通知策略層訂單更新（DCA/馬丁等），並在成交或取消時釋放當時預留的資金，避免「可用」只減不增
		if strategyManager != nil {
			routedStrategy := ""
			if multiExecutor != nil {
				routedStrategy = multiExecutor.GetStrategyByOrderID(posUpdate.OrderID)
				if routedStrategy == "" {
					routedStrategy = multiExecutor.GetStrategyByClientOrderID(posUpdate.ClientOrderID)
				}
			}
			strategyManager.OnOrderUpdateForStrategy(routedStrategy, posUpdate)
		}
		if multiExecutor != nil && (posUpdate.Status == "FILLED" || posUpdate.Status == "CANCELED") {
			multiExecutor.ReleaseOrderCapitalByClientOrderID(posUpdate.ClientOrderID)
			multiExecutor.ReleaseOrderCapitalByOrderID(posUpdate.OrderID)
		}
	}); err != nil {
		logger.Warn("⚠️ [%s] 啟動訂單流失败: %v", symCfg.Symbol, err)
	}

	if err := superPositionManager.Initialize(currentPrice, currentPriceStr); err != nil {
		return nil, fmt.Errorf("初始化倉位管理器失败(%s:%s): %w", symCfg.Exchange, symCfg.Symbol, err)
	}

	// 🔥 如果啟动時已有持倉（满倉或接近满倉），立即調用 AdjustOrders 初始化賣單
	// 避免等待價格變化才触发订單調整，确保满倉状態下也能立即开始交易
	// 純趋势/动量等非網格多策略模式跳过首輪網格挂单，避免误挂
	if config.ShouldSkipInitialGridAdjustOrders(&localCfg) {
		logger.Info("⏭️ [%s] 啟动時跳过網格订單初始化（當前為非網格多策略模式）", symCfg.Symbol)
	} else if err := superPositionManager.AdjustOrders(currentPrice); err != nil {
		logger.Warn("⚠️ [%s] 啟动時初始化订單失败: %v", symCfg.Symbol, err)
	} else {
		logger.Info("✅ [%s] 啟动時订單初始化完成（如有持倉已自动挂賣單）", symCfg.Symbol)
	}

	if storageService != nil {
		if st := storageService.GetStorage(); st != nil {
			restoreAdapter := &reconciliationRestoreAdapter{storage: st}
			if err := superPositionManager.RestoreReconciliationStats(restoreAdapter, symCfg.Exchange, symCfg.Symbol); err != nil {
				logger.Warn("⚠️ [%s] 恢複對账统计失败: %v", symCfg.Symbol, err)
			}
		}
	}

	reconciler.Start(ctx)

	orderCleaner := safety.NewOrderCleaner(&localCfg, exchangeExecutor, superPositionManager)
	orderCleaner.Start(ctx)

	go riskMonitor.Start(ctx)
	go depthMonitor.Start(ctx)

	// 啟動資金費率監控器和套利管理器
	if fundingMonitor != nil {
		go fundingMonitor.Start(ctx)
	}
	if arbitrageManager != nil {
		go arbitrageManager.Start(ctx)
	}

	// 可選组件
	var dynamicAdjuster *strategy.DynamicAdjuster
	if localCfg.Trading.DynamicAdjustment.Enabled {
		dynamicAdjuster = strategy.NewDynamicAdjuster(&localCfg, priceMonitor, superPositionManager)
		dynamicAdjuster.Start()
	}

	var trendDetector *strategy.TrendDetector
	if localCfg.Trading.SmartPosition.Enabled || localCfg.Trading.GridRiskControl.TrendFilterEnabled {
		trendDetector = strategy.NewTrendDetector(&localCfg, priceMonitor)
		trendDetector.Start()
		// 將趋势检测器注入 SuperPositionManager
		superPositionManager.SetTrendDetector(trendDetector)
	}

	if localCfg.Strategies.Enabled {
		totalCapital := localCfg.Strategies.CapitalAllocation.TotalCapital
		if totalCapital <= 0 {
			quoteAsset := ex.GetQuoteAsset()
			if quoteAsset == "" {
				quoteAsset = "USDT"
			}
			balance, err := ex.GetBalance(ctx, quoteAsset)
			if err == nil && balance > 0 {
				totalCapital = balance
				logger.Info("💰 [%s] 從账戶獲取總资金: %.2f %s", symCfg.Symbol, totalCapital, quoteAsset)
			} else {
				totalCapital = 5000
				logger.Warn("⚠️ [%s] 無法獲取帳戶餘額，使用默认總资金: %.2f %s", symCfg.Symbol, totalCapital, quoteAsset)
			}
		}

		strategyManager = strategy.NewStrategyManager(&localCfg, totalCapital)
		// 設置事件總線
		if eventBus != nil {
			strategyManager.SetEventBus(eventBus)
		}
		multiExecutor = strategy.NewMultiStrategyExecutor(exchangeExecutor, strategyManager.GetCapitalAllocator())

		if gridCfg, exists := localCfg.Strategies.Configs["grid"]; exists && gridCfg.Enabled {
			gridStrategy := strategy.NewGridStrategy("grid", &localCfg, executorAdapter, exchangeAdapter, superPositionManager)
			fixedPool := 0.0
			if pool, ok := gridCfg.Config["capital_pool"].(float64); ok {
				fixedPool = pool
			}
			strategyManager.RegisterStrategy("grid", gridStrategy, gridCfg.Weight, fixedPool)
			logger.Info("✅ [%s] 網格策略已注册", symCfg.Symbol)
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

		if martinCfg, exists := localCfg.Strategies.Configs["martingale"]; exists && martinCfg.Enabled {
			martinExecutor := strategy.NewMultiStrategyExecutorAdapter(multiExecutor, "martingale")
			martinStrategy := strategy.NewMartingaleStrategy("martingale", symCfg.Symbol, &localCfg, martinExecutor, exchangeAdapter, martinCfg.Config)
			fixedPool := 0.0
			if pool, ok := martinCfg.Config["capital_pool"].(float64); ok {
				fixedPool = pool
			}
			strategyManager.RegisterStrategy("martingale", martinStrategy, martinCfg.Weight, fixedPool)
			logger.Info("✅ [%s] 马丁格尔策略已注册", symCfg.Symbol)
		}

		// DCA 策略（普通 DCA，使用 DCAEnhancedStrategy 實現）
		if dcaCfg, exists := localCfg.Strategies.Configs["dca"]; exists && dcaCfg.Enabled {
			dcaExecutor := strategy.NewMultiStrategyExecutorAdapter(multiExecutor, "dca")
			dcaStrategy := strategy.NewDCAEnhancedStrategy("dca", symCfg.Symbol, &localCfg, dcaExecutor, exchangeAdapter, dcaCfg.Config)
			// 🔥 設置交易存儲，用於保存止损单的交易記錄
			if storageService != nil {
				tradeStorageAdapter := &tradeStorageAdapter{
					storageService: storageService,
					accountID:      accountID,
				}
				dcaStrategy.SetTradeStorage(tradeStorageAdapter)
			}
			fixedPool := 0.0
			if pool, ok := dcaCfg.Config["capital_pool"].(float64); ok {
				fixedPool = pool
			}
			strategyManager.RegisterStrategy("dca", dcaStrategy, dcaCfg.Weight, fixedPool)
			logger.Info("✅ [%s] DCA 定投策略已注册", symCfg.Symbol)
		}

		// DCA Enhanced 策略（增強型 DCA）
		if dcaEnhancedCfg, exists := localCfg.Strategies.Configs["dca_enhanced"]; exists && dcaEnhancedCfg.Enabled {
			dcaEnhancedExecutor := strategy.NewMultiStrategyExecutorAdapter(multiExecutor, "dca_enhanced")
			dcaEnhancedStrategy := strategy.NewDCAEnhancedStrategy("dca_enhanced", symCfg.Symbol, &localCfg, dcaEnhancedExecutor, exchangeAdapter, dcaEnhancedCfg.Config)
			// 🔥 設置交易存儲，用於保存止损单的交易記錄
			if storageService != nil {
				tradeStorageAdapter := &tradeStorageAdapter{
					storageService: storageService,
					accountID:      accountID,
				}
				dcaEnhancedStrategy.SetTradeStorage(tradeStorageAdapter)
			}
			fixedPool := 0.0
			if pool, ok := dcaEnhancedCfg.Config["capital_pool"].(float64); ok {
				fixedPool = pool
			}
			strategyManager.RegisterStrategy("dca_enhanced", dcaEnhancedStrategy, dcaEnhancedCfg.Weight, fixedPool)
			logger.Info("✅ [%s] 增强型 DCA 策略已注册", symCfg.Symbol)
		}

		if comboCfg, exists := localCfg.Strategies.Configs["combo"]; exists && comboCfg.Enabled {
			comboExecutor := strategy.NewMultiStrategyExecutorAdapter(multiExecutor, "combo")
			comboStrategy := strategy.NewComboStrategy("combo", symCfg.Symbol, &localCfg, comboExecutor, exchangeAdapter, comboCfg.Config)
			fixedPool := 0.0
			if pool, ok := comboCfg.Config["capital_pool"].(float64); ok {
				fixedPool = pool
			}
			strategyManager.RegisterStrategy("combo", comboStrategy, comboCfg.Weight, fixedPool)
			logger.Info("✅ [%s] 组合策略已注册", symCfg.Symbol)
		}

		// spot_short：現貨借幣做空策略（僅 spot/spot_margin，對沖組現貨腿，做多網格用）
		if symCfg.GetMarketType() == "spot" || symCfg.GetMarketType() == "spot_margin" {
			for _, si := range symCfg.Strategies {
				if si.Type == "spot_short" {
					spotShortCfg := map[string]interface{}{}
					if si.Config != nil {
						spotShortCfg = si.Config
					}
					spotShortExecutor := strategy.NewMultiStrategyExecutorAdapter(multiExecutor, "spot_short")
					spotShortStrategy := strategy.NewSpotShortStrategy("spot_short", &localCfg, spotShortExecutor, exchangeAdapter, ex, spotShortCfg)
					strategyManager.RegisterStrategy("spot_short", spotShortStrategy, si.Weight, 0)
					logger.Info("✅ [%s] 現貨做空策略已注册 (group=%v)", symCfg.Symbol, spotShortCfg["group_id"])
					break
				}
			}
		}
		// spot_long：現貨做多對沖策略（僅 spot，對沖組現貨腿，做空網格用）
		if symCfg.GetMarketType() == "spot" {
			for _, si := range symCfg.Strategies {
				if si.Type == "spot_long" {
					spotLongCfg := map[string]interface{}{}
					if si.Config != nil {
						spotLongCfg = si.Config
					}
					spotLongExecutor := strategy.NewMultiStrategyExecutorAdapter(multiExecutor, "spot_long")
					spotLongStrategy := strategy.NewSpotLongStrategy("spot_long", &localCfg, spotLongExecutor, exchangeAdapter, spotLongCfg)
					strategyManager.RegisterStrategy("spot_long", spotLongStrategy, si.Weight, 0)
					logger.Info("✅ [%s] 現貨做多對沖策略已注册 (group=%v)", symCfg.Symbol, spotLongCfg["group_id"])
					break
				}
			}
		}
		// futures_short：合約做空對沖策略（僅 futures，對沖組合約腿，現貨網格做多時用）
		if symCfg.GetMarketType() == "futures" {
			for _, si := range symCfg.Strategies {
				if si.Type == "futures_short" {
					futuresShortCfg := map[string]interface{}{}
					if si.Config != nil {
						futuresShortCfg = si.Config
					}
					futuresShortExecutor := strategy.NewMultiStrategyExecutorAdapter(multiExecutor, "futures_short")
					futuresShortStrategy := strategy.NewFuturesShortStrategy("futures_short", &localCfg, futuresShortExecutor, exchangeAdapter, futuresShortCfg)
					strategyManager.RegisterStrategy("futures_short", futuresShortStrategy, si.Weight, 0)
					logger.Info("✅ [%s] 合約做空對沖策略已注册 (group=%v)", symCfg.Symbol, futuresShortCfg["group_id"])
					break
				}
			}
		}
		// futures_long：合約做多對沖策略（僅 futures，對沖組合約腿，現貨網格做空時用）
		if symCfg.GetMarketType() == "futures" {
			for _, si := range symCfg.Strategies {
				if si.Type == "futures_long" {
					futuresLongCfg := map[string]interface{}{}
					if si.Config != nil {
						futuresLongCfg = si.Config
					}
					futuresLongExecutor := strategy.NewMultiStrategyExecutorAdapter(multiExecutor, "futures_long")
					futuresLongStrategy := strategy.NewFuturesLongStrategy("futures_long", &localCfg, futuresLongExecutor, exchangeAdapter, futuresLongCfg)
					strategyManager.RegisterStrategy("futures_long", futuresLongStrategy, si.Weight, 0)
					logger.Info("✅ [%s] 合約做多對沖策略已注册 (group=%v)", symCfg.Symbol, futuresLongCfg["group_id"])
					break
				}
			}
		}

		// 重啟恢復：從持久化訂單恢復 orderID/clientOrderID -> strategy 路由，避免回報廣播錯配
		if storageService != nil {
			if st := storageService.GetStorage(); st != nil {
				restoreStatuses := []string{"NEW", "PARTIALLY_FILLED"}
				for _, restoreStatus := range restoreStatuses {
					orders, err := st.QueryOrdersWithFilter(2000, 0, restoreStatus, symCfg.Exchange, symCfg.Symbol, nil, nil)
					if err != nil {
						logger.Warn("⚠️ [%s] 恢復策略路由失敗(status=%s): %v", symCfg.Symbol, restoreStatus, err)
						continue
					}
					for _, o := range orders {
						if o == nil || o.StrategyName == "" {
							continue
						}
						multiExecutor.RestoreOrderRoute(o.OrderID, o.ClientOrderID, o.StrategyName)
					}
				}
			}
		}

		if err := strategyManager.StartAll(); err != nil {
			logger.Error("❌ [%s] 啟动策略管理器失败: %v", symCfg.Symbol, err)
		} else {
			logger.Info("✅ [%s] 多策略系统已啟动", symCfg.Symbol)
		}
	}

	// 價格变动处理
	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.Error("❌ [%s] 價格變化处理协程 panic: %v", symCfg.Symbol, r)
			}
		}()

		priceCh := priceMonitor.Subscribe()
		var lastTriggered bool

		for {
			select {
			case <-ctx.Done():
				logger.Debug("⏹️ [%s] 價格變化处理协程已停止", symCfg.Symbol)
				return
			case priceChange, ok := <-priceCh:
				if !ok {
					// channel 已关闭
					logger.Debug("⏹️ [%s] 價格變化 channel 已关闭", symCfg.Symbol)
					return
				}

				isTriggered := riskMonitor.IsTriggered() || depthMonitor.IsTriggered()
				if isTriggered {
					if !lastTriggered {
						logger.Warn("🚨 [%s][风控触发] 撤销所有買單並暂停交易...", symCfg.Symbol)
						superPositionManager.CancelAllBuyOrders()
						lastTriggered = true
						if eventBus != nil {
							// 匯集觸發原因，便於事件中心展示
							var reasons []string
							if riskMonitor.IsTriggered() {
								if msg := riskMonitor.GetLastMsg(); msg != "" {
									reasons = append(reasons, msg)
								}
							}
							if depthMonitor.IsTriggered() {
								if msg := depthMonitor.GetLastMsg(); msg != "" {
									reasons = append(reasons, msg)
								}
							}
							reason := ""
							if len(reasons) > 0 {
								reason = reasons[0]
								for i := 1; i < len(reasons); i++ {
									reason += "; " + reasons[i]
								}
							}
							eventData := map[string]interface{}{
								"symbol": symCfg.Symbol,
								"price":  priceChange.NewPrice,
							}
							if reason != "" {
								eventData["reason"] = reason
							}
							eventBus.Publish(&event.Event{
								Type: event.EventTypeRiskTriggered,
								Data: eventData,
							})
						}
					}
					continue
				}

				if lastTriggered {
					logger.Info("✅ [%s][风控解除] 恢複自动交易", symCfg.Symbol)
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
						logger.Error("❌ [%s] 調整订單失败: %v", symCfg.Symbol, err)
					}
					localCfg.Trading.BuyWindowSize = origBuy
					localCfg.Trading.SellWindowSize = origSell
				} else {
					if strategyManager == nil || !localCfg.Strategies.Enabled {
						if err := superPositionManager.AdjustOrders(priceChange.NewPrice); err != nil {
							logger.Error("❌ [%s] 調整订單失败: %v", symCfg.Symbol, err)
						}
					}
				}
			}
		}
	}()

	// 定期检查并自动切换配置档案（如果配置了 profiles）
	var lastProfileSwitchTime time.Time
	var currentProfileName string
	if len(symCfg.Profiles) > 0 {
		// 初始化当前 profile 名称
		_, currentProfileName = selectProfile(ctx, symCfg, ex, feeRate, storageService)
		if currentProfileName != "" {
			lastProfileSwitchTime = time.Now()
		}

		go func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Error("❌ [%s] 配置档案切换协程 panic: %v", symCfg.Symbol, r)
				}
			}()

			// 每5分钟检查一次（可配置）
			checkInterval := 5 * time.Minute
			if symCfg.SwitchRules.CooldownSeconds > 0 {
				checkInterval = time.Duration(symCfg.SwitchRules.CooldownSeconds) * time.Second
			}
			ticker := time.NewTicker(checkInterval)
			defer ticker.Stop()

			// 保存 feeRate 的副本供协程使用
			currentFeeRate := feeRate

			for {
				select {
				case <-ctx.Done():
					logger.Debug("⏹️ [%s] 配置档案切换协程已停止", symCfg.Symbol)
					return
				case <-ticker.C:
					// 检查冷却时间
					if symCfg.SwitchRules.CooldownSeconds > 0 {
						if time.Since(lastProfileSwitchTime) < time.Duration(symCfg.SwitchRules.CooldownSeconds)*time.Second {
							continue
						}
					}

					// 重新获取最新配置（可能已更新）
					latestCfg, err := web.GetLatestConfig()
					if err != nil {
						logger.Warn("⚠️ [%s] 获取最新配置失败，跳过 profile 切换检查: %v", symCfg.Symbol, err)
						continue
					}

					// 查找当前交易对的配置
					var latestSymCfg *config.SymbolConfig
					for i := range latestCfg.Trading.Symbols {
						if strings.EqualFold(latestCfg.Trading.Symbols[i].Exchange, symCfg.Exchange) &&
							strings.EqualFold(latestCfg.Trading.Symbols[i].Symbol, symCfg.Symbol) {
							latestSymCfg = &latestCfg.Trading.Symbols[i]
							break
						}
					}

					if latestSymCfg == nil || len(latestSymCfg.Profiles) == 0 {
						continue
					}

					// 获取当前资金费率
					var currentFundingRate float64
					if symCfg.GetMarketType() == "futures" {
						if storageService != nil {
							if st := storageService.GetStorage(); st != nil {
								if latestRate, err := st.GetLatestFundingRate(symCfg.Symbol, symCfg.Exchange); err == nil {
									currentFundingRate = latestRate
								}
							}
						}
						if currentFundingRate == 0 && ex != nil {
							rateCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
							if rate, err := ex.GetFundingRate(rateCtx, symCfg.Symbol); err == nil {
								currentFundingRate = rate
							}
							cancel()
						}
					}

					// 选择新的 profile
					newProfile, newProfileName := selectProfile(ctx, *latestSymCfg, ex, currentFeeRate, storageService)
					if newProfileName == "" {
						// 使用主配置
						newProfileName = "default"
					}

					// 如果 profile 发生变化，记录日志（实际切换需要在重启时生效）
					if newProfileName != currentProfileName {
						logger.Info("🔄 [%s:%s] 检测到费率变化，建议从配置档案 '%s' 切换到 '%s' (资金费率: %.6f%%, 手续费率: %.6f%%)",
							symCfg.Exchange, symCfg.Symbol, currentProfileName, newProfileName,
							currentFundingRate*100, currentFeeRate*100)

						// 记录新配置参数
						if newProfileName != "default" {
							logger.Info("📋 [%s:%s] 新配置档案参数: price_interval=%.2f, order_quantity=%.2f, buy_window=%d, sell_window=%d",
								symCfg.Exchange, symCfg.Symbol, newProfile.PriceInterval, newProfile.OrderQuantity,
								newProfile.BuyWindowSize, newProfile.SellWindowSize)
						}

						oldProfileName := currentProfileName
						currentProfileName = newProfileName
						lastProfileSwitchTime = time.Now()

						// 发布事件
						if eventBus != nil {
							eventBus.Publish(&event.Event{
								Type: event.EventTypeConfigSwitched,
								Data: map[string]interface{}{
									"exchange":     symCfg.Exchange,
									"symbol":       symCfg.Symbol,
									"old_profile":  oldProfileName,
									"new_profile":  newProfileName,
									"funding_rate": currentFundingRate,
									"fee_rate":     currentFeeRate,
									"message":      fmt.Sprintf("配置档案切换建议: %s -> %s", oldProfileName, newProfileName),
								},
							})
						}
					}
				}
			}
		}()
	}

	// 定期打印持倉
	go func() {
		statusInterval := time.Duration(localCfg.Timing.StatusPrintInterval) * time.Minute
		ticker := time.NewTicker(statusInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if !riskMonitor.IsTriggered() && !depthMonitor.IsTriggered() {
					superPositionManager.PrintPositions()
				}
			}
		}
	}()

	rt := &SymbolRuntime{
		Config:               symCfg,
		Exchange:             ex,
		PriceMonitor:         priceMonitor,
		RiskMonitor:          riskMonitor,
		DepthMonitor:         depthMonitor,
		FundingMonitor:       fundingMonitor,
		ArbitrageManager:     arbitrageManager,
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
		AccountID:            accountID,
	}

	// 開倉控制器（限倉、定時、週期規則）
	openingController := position.NewOpeningController(superPositionManager, &rt.Config)
	openingController.Start()
	rt.OpeningController = openingController

	stopFn := func() {
		// 終止時全部平倉：若配置了 close_on_stop，先執行全平倉
		if symCfg.CloseOnStop && superPositionManager != nil {
			logger.Info("🔄 [%s] 終止時全部平倉 (close_on_stop=true)...", symCfg.Symbol)
			superPositionManager.LiquidateAll()
		}
		logger.Info("⏹️ [%s] 停止開倉控制器...", symCfg.Symbol)
		if rt.OpeningController != nil {
			rt.OpeningController.Stop()
		}
		logger.Info("⏹️ [%s] 停止價格監控...", symCfg.Symbol)
		if priceMonitor != nil {
			priceMonitor.Stop()
		}
		logger.Info("⏹️ [%s] 停止訂單流...", symCfg.Symbol)
		ex.StopOrderStream()
		logger.Info("⏹️ [%s] 停止风控監視器...", symCfg.Symbol)
		if riskMonitor != nil {
			riskMonitor.Stop()
		}
		if fundingMonitor != nil {
			fundingMonitor.Stop()
		}
		if arbitrageManager != nil {
			arbitrageManager.Stop()
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
	rt.Stop = stopFn

	return rt, nil
}

// toPositionOrderUpdate 提取订單更新為 position.OrderUpdate
func toPositionOrderUpdate(updateInterface interface{}) *position.OrderUpdate {
	v := reflect.ValueOf(updateInterface)
	if !v.IsValid() || v.Kind() != reflect.Struct {
		logger.Warn("⚠️ [symbol_manager] 订單更新不是結構体類型: %T", updateInterface)
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
		OrderID:         getInt64Field("OrderID"),
		ClientOrderID:   getStringField("ClientOrderID"),
		Symbol:          getStringField("Symbol"),
		Status:          getStringField("Status"),
		ExecutedQty:     getFloat64Field("ExecutedQty"),
		Price:           getFloat64Field("Price"),
		AvgPrice:        getFloat64Field("AvgPrice"),
		Side:            getStringField("Side"),
		Type:            getStringField("Type"),
		UpdateTime:      getInt64Field("UpdateTime"),
		Commission:      getFloat64Field("Commission"),
		CommissionAsset: getStringField("CommissionAsset"),
		RealizedPnL:     getFloat64Field("RealizedPnL"),
	}
}

// arbitrageExchangeAdapter 適配器，將 exchange.IExchange 適配為 arbitrage.IExchange
type arbitrageExchangeAdapter struct {
	exchange exchange.IExchange
}

func (a *arbitrageExchangeAdapter) GetName() string {
	return a.exchange.GetName()
}

func (a *arbitrageExchangeAdapter) GetMarketType() string {
	return a.exchange.GetMarketType()
}

func (a *arbitrageExchangeAdapter) PlaceOrder(ctx context.Context, req interface{}) (interface{}, error) {
	// 這裡需要將 interface{} 轉換為 exchange.OrderRequest
	// 簡化處理，暫時返回錯誤
	return nil, fmt.Errorf("PlaceOrder not implemented in adapter")
}

func (a *arbitrageExchangeAdapter) CancelOrder(ctx context.Context, symbol string, orderID int64) error {
	return a.exchange.CancelOrder(ctx, symbol, orderID)
}

func (a *arbitrageExchangeAdapter) GetPositions(ctx context.Context, symbol string) (interface{}, error) {
	positions, err := a.exchange.GetPositions(ctx, symbol)
	return positions, err
}

func (a *arbitrageExchangeAdapter) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	return a.exchange.GetLatestPrice(ctx, symbol)
}

func (a *arbitrageExchangeAdapter) GetBalance(ctx context.Context, asset string) (float64, error) {
	return a.exchange.GetBalance(ctx, asset)
}

func (a *arbitrageExchangeAdapter) GetBaseAsset() string {
	return a.exchange.GetBaseAsset()
}

func (a *arbitrageExchangeAdapter) GetQuoteAsset() string {
	return a.exchange.GetQuoteAsset()
}
