package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"quantmesh/arbitrage"
	"quantmesh/config"
	"quantmesh/event"
	"quantmesh/exchange"
	"quantmesh/exchange/binance"
	"quantmesh/execution"
	"quantmesh/feerate"
	"quantmesh/lock"
	"quantmesh/logger"
	"quantmesh/monitor"
	"quantmesh/order"
	"quantmesh/position"
	"quantmesh/safety"
	"quantmesh/storage"
	"quantmesh/strategy"
	ordersync "quantmesh/sync"
	"quantmesh/utils"
	"quantmesh/web"
)

// SymbolManager 管理多個 SymbolRuntime（委託給 BotManager 實現）
type SymbolManager struct {
	botManager    *BotManager
	legacyCloseMu sync.Mutex // serializes the legacy account-level close API
}

// NewSymbolManager 創建管理器（內部創建 BotManager，需傳入完整依賴）。primaryYAMLPath 為主配置 YAML 路徑（無則空），用於啟動 Bot 前與主庫同步刷新費率等。
func NewSymbolManager(cfg *config.Config, eventBus *event.EventBus, storageService *storage.StorageService, distributedLock lock.DistributedLock, primaryYAMLPath string) *SymbolManager {
	return &SymbolManager{
		botManager: NewBotManager(cfg, eventBus, storageService, distributedLock, primaryYAMLPath),
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
	// OpeningGate covers specialized runtimes that do not use the grid position manager.
	OpeningGate              *execution.OpeningGate
	CancelOpeningOrders      func(context.Context) error
	PrepareShutdown          func(context.Context, bool) error
	CloseForShutdown         func(context.Context) error
	VerifyShutdownClose      func(context.Context) error
	CloseForManual           func(context.Context, config.ClosePositionConfig) (*position.ClosePositionRecord, error)
	UpdateOpenControl        func(config.OpenPositionControl) error
	GetOpenControl           func() config.OpenPositionControl
	StopWithError            func() error
	OpeningController        *position.OpeningController
	OrderCleaner             *safety.OrderCleaner
	Reconciler               *safety.Reconciler
	TrendDetector            *strategy.TrendDetector
	DynamicAdjuster          *strategy.DynamicAdjuster
	StrategyManager          *strategy.StrategyManager
	ExchangeExecutor         *order.ExchangeOrderExecutor
	ExecutorAdapter          *exchangeExecutorAdapter
	ExchangeAdapter          *positionExchangeAdapter
	EventBus                 *event.EventBus
	StorageService           *storage.StorageService
	AccountID                string  // 账戶標识
	AccountScope             string  // immutable non-secret digest of exchange/environment/credential identity
	AccountMarketType        string  // immutable valuation scope; do not read mutable Config while sampling
	verifiedCapitalBudget    float64 // immutable startup-verified gross notional ceiling for this Bot
	capitalReservationStore  storage.AccountWalletCapitalReservationStore
	capitalReservationBotID  string
	capitalReservationClaims []storage.AccountWalletCapitalClaim
	capitalReservationStop   func()
	ClampOpenControl         func(config.OpenPositionControl) (config.OpenPositionControl, error)
	Stop                     func()
	fundingIncomeCancel      context.CancelFunc
	shutdownContextMu        sync.RWMutex
	shutdownContext          context.Context
	closeManagerMu           sync.Mutex
	closeManager             *position.ClosePositionManager
	specialCloseRecords      []*position.ClosePositionRecord

	// shutdownCloseHandled 非空表示退出流程中本 Bot 的持倉已由其他路徑（進程級 close_positions_on_exit）平倉，
	// 值為原因；Stop 中的 close_on_stop 見到後跳過，避免重複提交平倉單。
	shutdownCloseHandled atomic.Pointer[string]
	// Separate from success: an uncertain process close prevents independent
	// close_on_stop retries even if a later position snapshot happens to be flat.
	shutdownCloseUnverified atomic.Pointer[string]
}

func (rt *SymbolRuntime) recordSpecializedClose(record *position.ClosePositionRecord) {
	if rt == nil || record == nil {
		return
	}
	copy := *record
	rt.closeManagerMu.Lock()
	rt.specialCloseRecords = append(rt.specialCloseRecords, &copy)
	rt.closeManagerMu.Unlock()
}

func (rt *SymbolRuntime) specializedCloseRecords() []*position.ClosePositionRecord {
	if rt == nil {
		return nil
	}
	rt.closeManagerMu.Lock()
	defer rt.closeManagerMu.Unlock()
	result := make([]*position.ClosePositionRecord, 0, len(rt.specialCloseRecords))
	for _, record := range rt.specialCloseRecords {
		if record != nil {
			copy := *record
			result = append(result, &copy)
		}
	}
	return result
}

// singleLegStrategyPositionSides 單腿對沖策略的固定持倉方向（策略名 -> LONG/SHORT）
var singleLegStrategyPositionSides = map[string]string{
	"spot_short":    position.PositionSideShort,
	"futures_short": position.PositionSideShort,
	"spot_long":     position.PositionSideLong,
	"futures_long":  position.PositionSideLong,
}

// isOrderSkippedError 判斷下單錯誤是否為「價格位鎖被其他實例持有，本次未提交」。
// 這類錯誤不是失敗：不應釋放/計數為失敗，等待下一個 tick 重試。
func isOrderSkippedError(err error) bool {
	return errors.Is(err, order.ErrLockNotAcquired)
}

func orderStreamStartupError(exchangeName, symbol string, err error) error {
	if errors.Is(err, binance.ErrHedgePositionMode) {
		return fmt.Errorf("啟動訂單流失败(%s:%s)，請將賬戶切換為單向持倉模式: %w", exchangeName, symbol, err)
	}
	return fmt.Errorf("啟動訂單流失败(%s:%s)，為避免無法核實成交而拒絕啟動 Bot: %w", exchangeName, symbol, err)
}

// logAdjustOrdersError 記錄 AdjustOrders 錯誤：鎖未取得按跳過處理（Debug），其餘按失敗處理（Error）
func logAdjustOrdersError(ctx context.Context, symbol string, err error) {
	if isOrderSkippedError(err) {
		logger.DebugCtx(ctx, "🔒 [%s] 調整订單跳過（價格位被其他實例鎖定），下一輪重試: %v", symbol, err)
		return
	}
	logger.ErrorCtx(ctx, "❌ [%s] 調整订單失败: %v", symbol, err)
}

// fetchExchangeFeeRates 從交易所接口拉取 maker/taker 費率（測試可替換）
var fetchExchangeFeeRates = feerate.FetchFromExchangeAPI

func validResolvedGridFeeRates(maker, taker float64) bool {
	return !math.IsNaN(maker) && !math.IsInf(maker, 0) && maker >= -1 && maker <= 1 &&
		!math.IsNaN(taker) && !math.IsInf(taker, 0) && taker > 0 && taker <= 1
}

const (
	gridFeeRateUnverifiedBlock                  = "grid_fee_rate_unverified"
	gridFeeRateRetryInterval                    = time.Minute
	capitalBalanceUnverifiedBlock               = "capital_balance_unverified"
	accountWalletCapitalReservationPendingBlock = "account_wallet_capital_reservation_pending"
)

func gridFeeRatesRequired(cfg *config.Config) bool {
	return cfg != nil && cfg.Trading.FeeAwareSpread.IsEnabled() && !config.ShouldSkipInitialGridAdjustOrders(cfg)
}

func supportsGridFeeRateAPI(symCfg config.SymbolConfig) bool {
	if symCfg.GetMarketType() != "futures" {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(symCfg.Exchange)) {
	case "binance", "bitget":
		return true
	default:
		return false
	}
}

// gridFeeRateSource 網格費率來源（日誌用）
const (
	gridFeeRateSourceExchange = "exchange_api"
	gridFeeRateSourceConfig   = "config_fee_rate"
)

// resolveGridFeeRates 計算網格費率感知利差使用的 maker/taker：
// 合約且未跳過時優先交易所接口（feerate 目前只支持合約費率端點）；失敗或現貨時回退配置 fee_rate（maker 保守地取同值）。
// 返回 taker <= 0 表示無可用費率。
func resolveGridFeeRates(cfg *config.Config, symCfg config.SymbolConfig, configFeeRate float64, allowExchange bool) (maker, taker float64, source string) {
	maker, taker, source = configFeeRate, configFeeRate, gridFeeRateSourceConfig
	if !allowExchange || cfg == nil || config.IsSpotMarketType(symCfg.GetMarketType()) {
		return maker, taker, source
	}
	m, t, err := fetchExchangeFeeRates(cfg, symCfg.Exchange, symCfg.Symbol)
	if err != nil || !validResolvedGridFeeRates(m, t) {
		logger.Info("ℹ️ [%s:%s] 從交易所拉取 maker/taker 費率失敗，使用配置 fee_rate=%.4f%%: %v",
			symCfg.Exchange, symCfg.Symbol, configFeeRate*100, err)
		return maker, taker, source
	}
	return m, t, gridFeeRateSourceExchange
}

// applyGridFeeRates 啟動時為倉位管理器注入費率（遵守 timing.skip_exchange_fee_on_bot_start）
func applyGridFeeRates(ctx context.Context, cfg *config.Config, symCfg config.SymbolConfig, configFeeRate float64, spm *position.SuperPositionManager) {
	if spm == nil {
		return
	}
	maker, taker, source := resolveGridFeeRates(cfg, symCfg, configFeeRate, cfg != nil && !cfg.Timing.SkipExchangeFeeOnBotStart)
	if !validResolvedGridFeeRates(maker, taker) {
		if gridFeeRatesRequired(cfg) {
			spm.OpeningGate().Block(gridFeeRateUnverifiedBlock)
			logger.ErrorCtx(ctx, "🚨 [%s] 無法核實 maker 手續費率，已封鎖網格新開倉；請配置 exchanges.%s.fee_rate 或恢復交易所費率查詢", symCfg.Symbol, symCfg.Exchange)
		} else {
			spm.OpeningGate().Unblock(gridFeeRateUnverifiedBlock)
			logger.WarnCtx(ctx, "⚠️ [%s] 無可用手續費率，費率感知最小利差不生效", symCfg.Symbol)
		}
		return
	}
	spm.SetFeeRates(maker, taker)
	spm.OpeningGate().Unblock(gridFeeRateUnverifiedBlock)
	logger.InfoCtx(ctx, "💳 [%s] 網格費率來源: %s (maker %.4f%% / taker %.4f%%)", symCfg.Symbol, source, maker*100, taker*100)
}

func refreshGridFeeRates(cfg *config.Config, symCfg config.SymbolConfig, configFeeRate float64, spm *position.SuperPositionManager) {
	if spm == nil || cfg == nil {
		return
	}
	maker, taker, _ := resolveGridFeeRates(cfg, symCfg, configFeeRate, true)
	if !validResolvedGridFeeRates(maker, taker) {
		return
	}
	spm.SetFeeRates(maker, taker)
	spm.OpeningGate().Unblock(gridFeeRateUnverifiedBlock)
}

// startGridFeeRateRefresh 按 timing.fee_rate_refresh_minutes 定期刷新倉位管理器費率；返回停止函數（可重複調用）
func startGridFeeRateRefresh(ctx context.Context, cfg *config.Config, symCfg config.SymbolConfig, configFeeRate float64, spm *position.SuperPositionManager) func() {
	if spm == nil || cfg == nil {
		return func() {}
	}
	interval := time.Duration(cfg.Timing.FeeRateRefreshMinutes) * time.Minute
	if interval <= 0 {
		_, _, hasFees := spm.GetFeeRates()
		if !gridFeeRatesRequired(cfg) || hasFees || !supportsGridFeeRateAPI(symCfg) {
			return func() {}
		}
		interval = gridFeeRateRetryInterval
	}
	done := make(chan struct{})
	var once sync.Once
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case <-ticker.C:
				refreshGridFeeRates(cfg, symCfg, configFeeRate, spm)
			}
		}
	}()
	return func() { once.Do(func() { close(done) }) }
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
func (sm *SymbolManager) StopAll() error {
	return sm.botManager.StopAll()
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
	startupPauseHolders []storage.OpeningPauseHolder,
) (*SymbolRuntime, error) {
	if symCfg.GetMarketType() == config.MarketTypeFundingCarry {
		return startFundingCarrySymbolRuntime(ctx, baseCfg, symCfg, eventBus, storageService, distributedLock, onRequestStop, startupPauseHolders)
	}
	if symCfg.GetMarketType() == config.MarketTypeFundingPerpSpread {
		return startFundingPerpSpreadSymbolRuntime(ctx, baseCfg, symCfg, eventBus, storageService, distributedLock, onRequestStop, startupPauseHolders)
	}
	if err := validateLegacyFundingArbitrageReadiness(baseCfg, symCfg.GetMarketType()); err != nil {
		return nil, fmt.Errorf("期現套利配置無法安全啟動(%s:%s): %w", symCfg.Exchange, symCfg.Symbol, err)
	}

	// 按 Bot 覆蓋的 R5 配置（trading_overrides）與全局合併後校驗；非法時只拒絕本 Bot 啟動
	if err := config.ValidateBotTradingOverrides(baseCfg, symCfg.TradingOverrides); err != nil {
		return nil, fmt.Errorf("Bot 配置無效(%s:%s): %w", symCfg.Exchange, symCfg.Symbol, err)
	}
	// auto_rebuild / slot_filter / close_on_stop_config 校驗，非法時拒絕啟動
	if err := validateBotRuntimeExtras(symCfg); err != nil {
		return nil, fmt.Errorf("Bot 配置無效(%s:%s): %w", symCfg.Exchange, symCfg.Symbol, err)
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
	var ownershipGate atomic.Pointer[execution.OpeningGate]
	var ownershipExecutor atomic.Pointer[order.ExchangeOrderExecutor]
	var ownershipRuntime atomic.Pointer[SymbolRuntime]
	ownershipLease, err := acquireRuntimeOwnershipLease(ctx, distributedLock, runtimeOwnershipScope(
		equityAccountScopeID(symCfg.Exchange, localCfg.Exchanges[symCfg.Exchange]), symCfg.Exchange, symCfg.GetMarketType(), symCfg.Symbol,
	), runtimeOwnershipLeaseTTL, func(renewErr error) {
		logger.ErrorCtx(ctx, "[%s] Bot 运行所有权租约续期失败，停止后续提交并封锁开仓: %v", botID, renewErr)
		if gate := ownershipGate.Load(); gate != nil {
			gate.Block("runtime_ownership_unverified")
		}
		if executor := ownershipExecutor.Load(); executor != nil {
			executor.BeginShutdown()
		}
		if runtime := ownershipRuntime.Load(); runtime != nil {
			runtime.markShutdownCloseUnverified("Bot 运行所有权租约丢失，禁止独立追加平仓")
		}
	})
	if err != nil {
		return nil, fmt.Errorf("Bot %s 无法取得唯一运行所有权: %w", botID, err)
	}
	ownershipLeaseTransferred := false
	defer func() {
		if !ownershipLeaseTransferred {
			if releaseErr := ownershipLease.Release(); releaseErr != nil {
				logger.WarnCtx(ctx, "[%s] 初始化失败后释放运行所有权租约: %v", botID, releaseErr)
			}
		}
	}()
	localCfg.Trading.BotID = botID
	ctx = logger.WithBotID(ctx, botID)
	localCfg.Trading.Symbol = symCfg.Symbol
	localCfg.Trading.MarketType = symCfg.GetMarketType()
	localCfg.Trading.PriceInterval = symCfg.PriceInterval
	localCfg.Trading.ProfitSpread = symCfg.ProfitSpread
	localCfg.Trading.OrderQuantity = symCfg.OrderQuantity
	localCfg.Trading.MinOrderValue = symCfg.MinOrderValue
	localCfg.Trading.BuyWindowSize = symCfg.BuyWindowSize
	localCfg.Trading.SellWindowSize = symCfg.SellWindowSize
	localCfg.Trading.ShortOpenWindowSize = symCfg.ShortOpenWindowSize
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
	localCfg.Trading.SpotInventoryPolicy = config.NormalizeSpotInventoryPolicy(symCfg.SpotInventoryPolicy)
	config.ApplyBotRiskControls(&localCfg, symCfg)
	localCfg.Trading.SmartOrder = symCfg.SmartOrder
	if symCfg.SmartOrder.Enabled && symCfg.SmartOrder.MaxOpenOrders <= 0 {
		localCfg.Trading.SmartOrder.MaxOpenOrders = 3
	}
	if symCfg.SmartOrder.OpenOrderDistance <= 0 && symCfg.SmartOrder.Enabled {
		localCfg.Trading.SmartOrder.OpenOrderDistance = 5
	}

	// 將 Bot 上的 strategies 合并到本交易对 localCfg，使实盘与 API 策略类型（如 trend_following）一致
	if err := config.ApplyBotStrategiesToLocalConfig(&localCfg, &symCfg); err != nil {
		return nil, fmt.Errorf("应用 Bot 策略配置失败: %w", err)
	}
	// Bot 級 R5 覆蓋（fee_aware_spread / regime_filter / inventory_skew / funding_rate.pricing_enabled 等），未設置項沿用全局
	config.ApplyBotTradingOverrides(&localCfg, symCfg.TradingOverrides)

	// 創建交易所實例（根據交易對配置的市场類型：spot / futures）
	// 如果之前创建了临时实例，重用它；否则创建新实例
	var ex exchange.IExchange
	if tempEx != nil {
		ex = tempEx
		logger.InfoCtx(ctx, "✅ [%s] 重用交易所實例 (symbol=%s)", ex.GetName(), symCfg.Symbol)
	} else {
		ex, err = exchange.NewExchange(&localCfg, symCfg.Exchange, symCfg.Symbol, symCfg.GetMarketType())
		if err != nil {
			return nil, fmt.Errorf("創建交易所實例失败(%s:%s): %w", symCfg.Exchange, symCfg.Symbol, err)
		}
		logger.InfoCtx(ctx, "✅ [%s] 交易所實例已創建 (symbol=%s)", ex.GetName(), symCfg.Symbol)
	}

	requestedCapital := symCfg.TotalAllocatedCapital
	if requestedCapital == 0 {
		requestedCapital = localCfg.Strategies.CapitalAllocation.TotalCapital
	}
	quoteAsset := strings.TrimSpace(ex.GetQuoteAsset())
	availableBalance := 0.0
	availableBalanceObservedAt := time.Time{}
	availableBalanceObservationSequence := int64(0)
	var balanceErr error
	if quoteAsset == "" {
		balanceErr = fmt.Errorf("exchange quote asset is unavailable")
	} else {
		balanceCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		walletExchange := strings.TrimSpace(symCfg.Exchange)
		if walletExchange == "" {
			walletExchange = localCfg.App.CurrentExchange
		}
		walletKey, walletErr := accountWalletCapitalKey(baseCfg, walletExchange, ex.GetMarketType(), quoteAsset)
		if walletErr != nil {
			balanceErr = walletErr
		} else {
			availableBalanceObservationSequence, balanceErr = beginAccountWalletBalanceObservation(balanceCtx, storageService, walletKey)
		}
		availableBalanceObservedAt = time.Now().UTC()
		if balanceErr == nil {
			availableBalance, balanceErr = readAccountWalletCapitalValue(balanceCtx, ex, quoteAsset, symCfg.Symbol)
		}
		cancel()
	}
	botCapitalBudget, capitalErr := capStrategyCapitalLimit(requestedCapital, availableBalance)
	var capitalClaim storage.AccountWalletCapitalClaim
	capitalClaimReady := false
	if balanceErr != nil {
		capitalErr = fmt.Errorf("read verified %s account equity: %w", quoteAsset, balanceErr)
	}
	accountCapitalTotal, accountCapitalErr := configuredAccountCapitalTotalForQuote(baseCfg, symCfg, quoteAsset)
	if capitalErr == nil && accountCapitalErr != nil {
		capitalErr = accountCapitalErr
	}
	if capitalErr == nil && accountCapitalTotal > availableBalance {
		capitalErr = fmt.Errorf("configured Bot allocations %.2f %s exceed verified account equity %.2f %s",
			accountCapitalTotal, quoteAsset, availableBalance, quoteAsset)
	}
	if capitalErr == nil {
		capitalErr = applyBotCapitalLimit(&localCfg.Trading.OpenPositionControl, botCapitalBudget)
	}
	if capitalErr == nil {
		localCfg.Strategies.CapitalAllocation.TotalCapital = botCapitalBudget
	}
	if capitalErr == nil {
		walletExchange := strings.TrimSpace(symCfg.Exchange)
		if walletExchange == "" {
			walletExchange = localCfg.App.CurrentExchange
		}
		claim, claimErr := buildAccountWalletCapitalClaimFromObservation(baseCfg, walletExchange, ex.GetMarketType(), quoteAsset, botCapitalBudget, availableBalance, availableBalanceObservedAt)
		if claimErr != nil {
			capitalErr = claimErr
		} else {
			claim.ObservationSequence = availableBalanceObservationSequence
			claim.Exchange = walletExchange
			claim.Market = ex.GetMarketType()
			claim.QuoteAsset = quoteAsset
			claim.Symbol = symCfg.Symbol
			capitalClaim = claim
			capitalClaimReady = true
		}
	}

	// K 線 regime 檢測器（trading.regime_filter / adaptive_interval / upper_bound_freeze）：
	// 在啟動任何後台組件前校驗配置，配置非法時直接拒絕啟動
	gridRegime, err := newGridRegimeRuntime(&localCfg, symCfg.Symbol, ex)
	if err != nil {
		return nil, fmt.Errorf("K 線 regime 配置無效(%s): %w", botID, err)
	}
	logLegacyTrendFilterDeprecation(ctx, &localCfg, symCfg.Symbol)

	// API 权限安全检测
	logger.InfoCtx(ctx, "🔐 [%s:%s] 开始检测 API 权限...", symCfg.Exchange, symCfg.Symbol)
	permCheckCtx, permCheckCancel := context.WithTimeout(ctx, 10*time.Second)
	defer permCheckCancel()

	if checker, ok := ex.(exchange.PermissionChecker); ok {
		permissions, err := checker.CheckAPIPermissions(permCheckCtx)
		if err != nil {
			return nil, fmt.Errorf("API permission verification failed for %s:%s: %w", symCfg.Exchange, symCfg.Symbol, err)
		} else {
			// 检查是否安全
			if !permissions.IsSecure() {
				logger.ErrorCtx(ctx, "🚨 [%s:%s] API 密钥存在安全风險！", symCfg.Exchange, symCfg.Symbol)
				if permissions != nil {
					for _, warning := range permissions.GetWarnings() {
						logger.ErrorCtx(ctx, "   %s", warning)
					}
				}
				// 权限不安全时中止启动，避免风险 API Key 进入交易运行态。
				return nil, fmt.Errorf("API permissions are not safe for trading %s:%s", symCfg.Exchange, symCfg.Symbol)
			} else {
				logger.InfoCtx(ctx, "✅ [%s:%s] API 权限检测通過 (安全评分: %d/100, 风險等级: %s)",
					symCfg.Exchange, symCfg.Symbol, permissions.SecurityScore, permissions.RiskLevel)

				// 显示建议
				warnings := permissions.GetWarnings()
				if len(warnings) > 0 {
					for _, warning := range warnings {
						logger.InfoCtx(ctx, "   %s", warning)
					}
				}
			}
		}
	} else {
		logger.InfoCtx(ctx, "ℹ️ [%s:%s] 該交易所暫不支援自动权限检测，请手动确认 API 权限設置", symCfg.Exchange, symCfg.Symbol)
	}

	// 價格監控
	priceMonitor := monitor.NewPriceMonitor(
		ex,
		symCfg.Symbol,
		localCfg.Timing.PriceSendInterval,
	)

	logger.InfoCtx(ctx, "🔗 [%s] 啟动 WebSocket 價格流...", symCfg.Symbol)
	if err := priceMonitor.Start(); err != nil {
		return nil, fmt.Errorf("啟動價格流失败(%s:%s): %w", symCfg.Exchange, symCfg.Symbol, err)
	}

	// 等待初始價格
	pollInterval := time.Duration(localCfg.Timing.PricePollInterval) * time.Millisecond
	if pollInterval <= 0 {
		// 零值保护：避免配置未经校验时 time.Sleep(0) 退化为 CPU 空转
		pollInterval = 500 * time.Millisecond
	}
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
	logger.InfoCtx(ctx, "ℹ️ [%s] 精度 - 價格:%d 數量:%d", symCfg.Symbol, priceDecimals, quantityDecimals)

	// 使用之前获取的手续费率（已在创建交易所实例前获取）
	if symCfg.Exchange == "binance" && feeRate == 0 {
		logger.InfoCtx(ctx, "💳 [%s] 配置文件未設置手续费率，使用币安期货默认Taker费率: %.4f%%", symCfg.Symbol, feeRate*100)
		logger.InfoCtx(ctx, "ℹ️ [%s] 提示：币安期货實際费率取决於您的VIP等级，请在配置文件中設置准确的费率", symCfg.Symbol)
	} else {
		logger.InfoCtx(ctx, "💳 [%s] 使用配置文件中的手续费率: %.4f%%", symCfg.Symbol, feeRate*100)
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
	logger.InfoCtx(ctx, "✅ [%s] 持倉安全性检查通過", botID)

	// 核心组件
	// Legacy Account 字段也使用不可逆摘要；不可把 API Key 片段落入账本或统计数据。
	accountID := ""
	if exCfg, ok := baseCfg.Exchanges[symCfg.Exchange]; ok && strings.TrimSpace(exCfg.APIKey) != "" {
		accountID = equityAccountScopeID(symCfg.Exchange, exCfg)
	}

	exchangeExecutor := order.NewExchangeOrderExecutor(
		ex,
		symCfg.Symbol,
		localCfg.Timing.RateLimitRetryDelay,
		localCfg.Timing.OrderRetryDelay,
		distributedLock,
		botID,
	)
	executorAdapter := &exchangeExecutorAdapter{
		executor:  exchangeExecutor,
		eventBus:  eventBus,
		symbol:    symCfg.Symbol,
		exchange:  symCfg.Exchange,
		accountID: accountID,
	}
	exchangeAdapter := &positionExchangeAdapter{exchange: ex}

	// PostOnly 被拒時重定價重掛（永不降級 GTC）
	exchangeExecutor.SetPostOnlyRepriceMaxAttempts(localCfg.Trading.PostOnlyRepriceMaxAttempts)

	superPositionManager := position.NewSuperPositionManager(&localCfg, executorAdapter, exchangeAdapter, priceDecimals, quantityDecimals)
	applyStartupOpeningPauseHolders(superPositionManager.OpeningGate(), startupPauseHolders)
	if capitalErr == nil {
		if err := superPositionManager.SetVerifiedCapitalLimit(botCapitalBudget); err != nil {
			return nil, fmt.Errorf("install verified Bot capital ceiling: %w", err)
		}
	}
	// Initial pause is now owned by the shared gate. Static strategy config must
	// not retain an initial flag that an explicit runtime resume cannot clear.
	localCfg.Trading.OpenPositionControl.PauseOpening = false
	if localCfg.Trading.OpenPositionControl.BotRiskControl != nil {
		localCfg.Trading.OpenPositionControl.BotRiskControl.PauseOpening = false
	}
	exchangeExecutor.SetOpeningGate(superPositionManager.OpeningGate(), localCfg.Trading.Direction)
	ownershipGate.Store(superPositionManager.OpeningGate())
	ownershipExecutor.Store(exchangeExecutor)
	if ownershipLease.Lost() {
		superPositionManager.OpeningGate().Block("runtime_ownership_unverified")
		exchangeExecutor.BeginShutdown()
	}
	if capitalErr != nil {
		superPositionManager.OpeningGate().Block(capitalBalanceUnverifiedBlock)
		logger.ErrorCtx(ctx, "🚨 [%s] Bot 資金上限无法核实，已封锁所有新開倉: %v", botID, capitalErr)
	} else {
		// The persistent claim is intentionally acquired immediately before the
		// first order-capable initialization. Earlier startup failures must not
		// strand a reservation for a Bot that never reached trading admission.
		superPositionManager.OpeningGate().Block(accountWalletCapitalReservationPendingBlock)
		if requestedCapital > botCapitalBudget {
			logger.WarnCtx(ctx, "⚠️ [%s] 配置資金上限 %.2f %s 超過已核實帳戶權益 %.2f %s，Bot 总名义敞口已下調至該權益",
				botID, requestedCapital, quoteAsset, botCapitalBudget, quoteAsset)
		} else {
			logger.InfoCtx(ctx, "💰 [%s] 同账户已配置 Bot 资金预算合计 %.2f %s，已核實帳戶權益 %.2f %s",
				botID, accountCapitalTotal, quoteAsset, availableBalance, quoteAsset)
		}
	}
	exposureBook, err := configureRuntimeExposure(exchangeExecutor, priceMonitor.GetQuoteEvidence)
	if err != nil {
		return nil, fmt.Errorf("configure runtime exposure: %w", err)
	}
	superPositionManager.SetRiskControls(config.RiskControls{Open: localCfg.Trading.OpenPositionControl, Grid: localCfg.Trading.GridRiskControl})
	var intentBackend runtimeIntentBackend
	if storageService != nil {
		intentBackend, _ = storageService.GetStorage().(runtimeIntentBackend)
	}
	intentScope := execution.IntentScope{Account: equityAccountScopeID(symCfg.Exchange, localCfg.Exchanges[symCfg.Exchange]),
		Exchange: ex.GetName(), Market: ex.GetMarketType(), Symbol: symCfg.Symbol, Bot: botID}
	if storageService != nil {
		runtimeStateKey, scopeErr := intentScope.Key()
		if scopeErr != nil {
			return nil, fmt.Errorf("grid runtime state owner scope: %w", scopeErr)
		}
		superPositionManager.SetGridRuntimeStateStore(&scopedGridRuntimeStateAdapter{
			base:        &strategyRuntimeStateAdapter{storageService: storageService, botID: botID},
			strategyKey: "grid-" + runtimeStateKey,
		})
		if restored, restoreErr := superPositionManager.RestoreGridRuntimeState(); restoreErr != nil {
			return nil, fmt.Errorf("restore grid runtime state for %s: %w", botID, restoreErr)
		} else if restored {
			logger.InfoCtx(ctx, "[%s] 已加载网格运行态快照；仍需交易所持仓、挂单和执行意图核对后才允许开仓", botID)
			if !superPositionManager.GridRuntimeStateIsVerifiedEmpty() {
				superPositionManager.OpeningGate().Block("grid_runtime_state_reconciliation")
			}
		}
		tradeStorageAdapter := &tradeStorageAdapter{
			storageService: storageService,
			accountID:      accountID,
			accountScope:   equityAccountScopeID(symCfg.Exchange, localCfg.Exchanges[symCfg.Exchange]),
			botID:          botID,
			marketType:     strings.ToLower(strings.TrimSpace(ex.GetMarketType())),
			pnlAsset:       strings.ToUpper(strings.TrimSpace(quoteAsset)),
		}
		superPositionManager.SetTradeStorage(tradeStorageAdapter)
		exchangeExecutor.SetTradeLedgerRecoveryHandler(tradeStorageAdapter.ReplayPendingGridTrade)
		pendingCorrections, correctionErr := tradeStorageAdapter.CountPendingTradeFeeCorrections(symCfg.Exchange, symCfg.Symbol)
		if restorePendingTradeFeeCorrectionHold(superPositionManager.OpeningGate(), pendingCorrections, correctionErr) && correctionErr != nil {
			logger.ErrorCtx(ctx, "🚨 [%s] 無法核實持久化手續費更正狀態，已封鎖新開倉: %v", botID, correctionErr)
		} else if pendingCorrections > 0 {
			logger.ErrorCtx(ctx, "🚨 [%s] 發現 %d 筆尚未核賬的持久化手續費更正，已封鎖新開倉", botID, pendingCorrections)
		}
	}
	var signalInventory []execution.ExposurePosition
	var signalStateRestored bool
	var signalStateLoadErr error
	var signalStrategyNames []string
	for _, name := range []string{"trend", "mean_reversion", "momentum"} {
		if strategyCfg, exists := localCfg.Strategies.Configs[name]; exists && strategyCfg.Enabled {
			signalStrategyNames = append(signalStrategyNames, name)
		}
	}
	if len(signalStrategyNames) > 0 {
		stateStore := &strategyRuntimeStateAdapter{storageService: storageService, botID: botID}
		signalInventory, signalStateRestored, signalStateLoadErr = strategy.LoadSignalRuntimeExposureInventory(stateStore, &localCfg, exchangeAdapter, symCfg.Symbol, signalStrategyNames...)
		if signalStateLoadErr != nil {
			logger.ErrorCtx(ctx, "[%s] signal strategy exposure recovery incomplete; new opening remains blocked: %v", botID, signalStateLoadErr)
			superPositionManager.OpeningGate().Block(runtimeExposureBootstrapBlock)
		}
	}
	if dcaCfg, exists := localCfg.Strategies.Configs["dca"]; exists && dcaCfg.Enabled && signalStateLoadErr == nil {
		stateStore := &strategyRuntimeStateAdapter{storageService: storageService, botID: botID}
		dcaInventory, dcaStateRestored, dcaErr := strategy.LoadDCAExposureInventory(stateStore, &localCfg, exchangeAdapter, symCfg.Symbol, dcaCfg.Config)
		if dcaErr != nil {
			signalStateLoadErr = dcaErr
			logger.ErrorCtx(ctx, "[%s] DCA exposure recovery incomplete; new opening remains blocked: %v", botID, dcaErr)
			superPositionManager.OpeningGate().Block(runtimeExposureBootstrapBlock)
		} else {
			signalInventory = append(signalInventory, dcaInventory...)
			signalStateRestored = signalStateRestored || dcaStateRestored
		}
	}
	if martingaleCfg, exists := localCfg.Strategies.Configs["martingale"]; exists && martingaleCfg.Enabled && signalStateLoadErr == nil {
		stateStore := &strategyRuntimeStateAdapter{storageService: storageService, botID: botID}
		martingaleInventory, martingaleStateRestored, martingaleErr := strategy.LoadMartingaleExposureInventory(stateStore, &localCfg, exchangeAdapter, symCfg.Symbol, martingaleCfg.Config)
		if martingaleErr != nil {
			signalStateLoadErr = martingaleErr
			logger.ErrorCtx(ctx, "[%s] martingale exposure recovery incomplete; new opening remains blocked: %v", botID, martingaleErr)
			superPositionManager.OpeningGate().Block(runtimeExposureBootstrapBlock)
		} else {
			signalInventory = append(signalInventory, martingaleInventory...)
			signalStateRestored = signalStateRestored || martingaleStateRestored
		}
	}
	if signalStateLoadErr == nil {
		if err := bootstrapRuntimeExposure(ctx, exchangeExecutor, superPositionManager.OpeningGate(), ex, intentBackend, intentScope, exposureBook, superPositionManager, signalInventory, signalStateRestored); err != nil {
			logger.ErrorCtx(ctx, "[%s] execution recovery incomplete; new opening remains blocked: %v", botID, err)
		} else {
			superPositionManager.MarkGridRuntimeVenueFlatVerified()
		}
	}
	if localCfg.CircuitBreaker.Enabled && localCfg.CircuitBreaker.Triggers.MaxDrawdown.Enabled {
		superPositionManager.SetEquityRiskPaused(true) // no opening before the first verified equity sample
	}
	if err := superPositionManager.ConfigureProtectiveLiquidation(ctx, superPositionManager.NewLiquidationVenue(ex), nil); err != nil {
		return nil, fmt.Errorf("configure protective liquidation: %w", err)
	}
	exchangeExecutor.SetUnknownOrderHandler(func(req order.OrderRequest) {
		logger.ErrorCtx(ctx, "[%s] 訂單 %s 結果 UNKNOWN：已保留資金並停止新增風險，需核實成交與策略倉位", botID, req.ClientOrderID)
		if eventBus != nil {
			eventBus.Publish(&event.Event{Type: event.EventTypeRiskTriggered, Data: map[string]interface{}{
				"bot_id": botID, "symbol": req.Symbol, "exchange": symCfg.Exchange,
				"reason": "order_outcome_unknown", "client_order_id": req.ClientOrderID,
				"strategy_name": req.StrategyName, "requires_reconciliation": true,
			}})
		}
	})
	// 費率感知最小利差：注入 maker/taker 費率（交易所接口優先，失敗回退配置 fee_rate）
	applyGridFeeRates(ctx, &localCfg, symCfg, feeRate, superPositionManager)
	// 配置的槽位過濾在首輪掛單（Initialize / AdjustOrders）之前生效
	applyConfiguredSlotFilter(ctx, symCfg, superPositionManager)
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
				logger.WarnCtx(ctx, "⚠️ 創建現貨交易所失敗，跳過期現套利: %v", err)
			} else {
				// 創建交易所適配器
				futuresAdapter := &arbitrageExchangeAdapter{exchange: ex}
				spotAdapter := &arbitrageExchangeAdapter{exchange: spotEx}

				arbitrageManager = arbitrage.NewFundingArbitrageManager(&localCfg, futuresAdapter, spotAdapter, fundingMonitor, symCfg.Symbol)

				// 連接套利管理器到網格管理器
				superPositionManager.SetArbitrageManager(arbitrageManager)
				logger.InfoCtx(ctx, "💱 期現套利管理器已創建並連接到網格管理器")
			}
		}

		logger.InfoCtx(ctx, "💰 資金費率監控器已創建")
	}

	reconciler := safety.NewReconciler(&localCfg, exchangeAdapter, superPositionManager, distributedLock)
	reconciler.SetOpenOrderOwnershipVerifier(exchangeExecutor.OwnsOpenOrder)
	reconciler.SetPauseChecker(func() bool {
		// 检查市场异动风控或深度风控是否触发
		return riskMonitor.IsTriggered() || depthMonitor.IsTriggered()
	})
	if storageService != nil {
		reconciler.SetStorage(&reconciliationStorageAdapter{
			storageService: storageService,
			accountID:      accountID,
			accountScope:   equityAccountScopeID(symCfg.Exchange, localCfg.Exchanges[symCfg.Exchange]),
			marketType:     symCfg.GetMarketType(),
			botID:          botID,
			exchange:       symCfg.Exchange,
		})
	}

	// 提前宣告，供訂單流回調在成交/取消時通知策略並釋放預留資金（閉包可引用）
	var strategyManager *strategy.StrategyManager
	var multiExecutor *strategy.MultiStrategyExecutor
	fillCapture := newRuntimeFillCapture()
	var fillWriter interface {
		SaveOrderFill(*storage.OrderFill) error
	}
	accountScope := equityAccountScopeID(symCfg.Exchange, localCfg.Exchanges[symCfg.Exchange])
	if storageService != nil {
		fillWriter, _ = storageService.GetStorage().(interface {
			SaveOrderFill(*storage.OrderFill) error
		})
	}

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
			logger.DebugCtx(ctx, "⏭️ [订單過滤] 跳過其他交易對的订單: Symbol=%s (當前交易對: %s), ClientOID=%s",
				posUpdate.Symbol, symCfg.Symbol, posUpdate.ClientOrderID)
			return
		}
		if !observeOwnedRuntimeOrder(exchangeExecutor, posUpdate) {
			logger.DebugCtx(ctx, "[%s] ignore order update without verified Bot intent ownership", botID)
			return
		}
		if terminalOrderUpdate(posUpdate.Status) && posUpdate.ExecutedQty > 0 {
			blockReason := fmt.Sprintf("execution_ledger_unverified:%d", posUpdate.OrderID)
			// Capture is asynchronous. Close the admission window before starting
			// it; only a complete, durably persisted fill history may reopen it.
			superPositionManager.OpeningGate().Block(blockReason)
			fillCapture.Observe(ctx, ex, fillWriter, *posUpdate, ex.GetName(), ex.GetMarketType(), accountScope, accountID, botID,
				func(err error) {
					superPositionManager.OpeningGate().Block(blockReason)
					logger.ErrorCtx(ctx, "[%s] 成交执行账本无法核实，已阻止新开仓: %v", botID, err)
					if eventBus != nil {
						eventBus.Publish(&event.Event{Type: event.EventTypeRiskTriggered, Data: map[string]interface{}{
							"bot_id": botID, "symbol": symCfg.Symbol, "exchange": symCfg.Exchange,
							"reason": "execution_ledger_unverified", "order_id": posUpdate.OrderID,
							"requires_reconciliation": true,
						}})
					}
				}, func() {
					superPositionManager.OpeningGate().Unblock(blockReason)
				})
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

		gridZeroFillAccounted := superPositionManager.OnOrderUpdate(*posUpdate)
		// 通知策略層訂單更新（DCA/馬丁等），並在成交或取消時釋放當時預留的資金，避免「可用」只減不增
		routedStrategy := ""
		strategyAccountingVerified := false
		if strategyManager != nil {
			if multiExecutor != nil {
				routedStrategy = multiExecutor.GetStrategyByOrderID(posUpdate.OrderID)
				if routedStrategy == "" {
					routedStrategy = multiExecutor.GetStrategyByClientOrderID(posUpdate.ClientOrderID)
				}
			}
			if err := strategyManager.ApplyOrderUpdateForStrategy(routedStrategy, posUpdate); err != nil {
				superPositionManager.OpeningGate().Block("strategy_accounting_unverified")
				logger.ErrorCtx(ctx, "[%s] 策略成交账未能确认，已封锁 Bot 新开仓: %v", botID, err)
				if eventBus != nil {
					eventBus.Publish(&event.Event{Type: event.EventTypeRiskTriggered, Data: map[string]interface{}{
						"bot_id": botID, "symbol": symCfg.Symbol, "exchange": symCfg.Exchange,
						"reason": "strategy_accounting_unverified", "requires_reconciliation": true,
					}})
				}
			} else if routedStrategy != "" {
				strategyAccountingVerified = true
			}
		}
		if multiExecutor != nil {
			// D5：開倉成交轉為持倉占用、平倉成交按比例釋放、撤單/拒單/過期釋放未成交預留
			multiExecutor.OnOrderUpdate(posUpdate)
		}
		if strategyAccountingVerified {
			if err := settleVerifiedStrategyIntent(ctx, exchangeExecutor, superPositionManager.OpeningGate(), routedStrategy, posUpdate); err != nil {
				logger.ErrorCtx(ctx, "[%s] 策略终态订单的执行意图尚未核实结算，已暂停新开仓并保留恢复阻断状态: order_id=%d error=%v", botID, posUpdate.OrderID, err)
				if eventBus != nil {
					eventBus.Publish(&event.Event{Type: event.EventTypeRiskTriggered, Data: map[string]interface{}{
						"bot_id": botID, "symbol": symCfg.Symbol, "exchange": symCfg.Exchange,
						"reason": strategyIntentSettlementBlock, "requires_reconciliation": true,
					}})
				}
			}
		}
		settleVerifiedGridZeroFill(exchangeExecutor, superPositionManager.OpeningGate(), posUpdate, gridZeroFillAccounted)
	}); err != nil {
		priceMonitor.Stop()
		return nil, orderStreamStartupError(symCfg.Exchange, symCfg.Symbol, err)
	}
	if storageService != nil && (ex.GetMarketType() == "futures" || ex.GetMarketType() == "spot") {
		type adapterGetter interface{ GetAdapter() interface{} }
		if getter, ok := ex.(adapterGetter); ok {
			if _, ok := getter.GetAdapter().(interface {
				GetUserTradesFromID(ctx context.Context, symbol string, startTime, endTime, fromID int64, limit int) ([]*binance.UserTrade, error)
			}); ok {
				orderSyncService := ordersync.NewOrderSyncService(ex, storageService.GetStorage(), symCfg.Symbol, accountID, ex.GetName(), 5*time.Minute)
				orderSyncService.SetTradeScope(ex.GetMarketType(), accountScope)
				orderSyncService.Start(ctx)
				logger.InfoCtx(ctx, "[%s] Binance %s 成交历史后台补偿已启动（5 分钟间隔）", symCfg.Symbol, ex.GetMarketType())
			}
		}
	}

	// Publish risk admission before Initialize/first grid placement/StartAll.
	dynamicAdjuster := strategy.NewDynamicAdjuster(&localCfg, priceMonitor, superPositionManager)
	if err := dynamicAdjuster.SetVolatilityHistoryLoader(runtimeVolatilityHistoryLoader(ex, symCfg.Symbol)); err != nil {
		return nil, err
	}
	if capitalErr == nil {
		if !capitalClaimReady {
			return nil, fmt.Errorf("verified Bot capital claim is unavailable before order initialization")
		}
		if err := reserveRuntimeAccountWalletCapital(ctx, baseCfg, storageService, distributedLock, botID, []storage.AccountWalletCapitalClaim{capitalClaim}, superPositionManager.OpeningGate()); err != nil {
			return nil, fmt.Errorf("reserve Bot account wallet capital before order initialization: %w", err)
		}
	}
	dynamicAdjuster.StartWithExternalPrices()
	dynamicOwnedByRuntime := false
	defer func() {
		if !dynamicOwnedByRuntime {
			dynamicAdjuster.Stop()
		}
	}()

	initializationErr := superPositionManager.Initialize(currentPrice, currentPriceStr)
	if initializationErr != nil {
		logger.ErrorCtx(ctx, "[%s] 网格初始化/首批订单未核实，已封锁新开仓并撤销已登记开仓单；运行时保留供安全停止与对账: %v", botID, initializationErr)
		superPositionManager.CancelAllOpenOrders()
		if eventBus != nil {
			eventBus.Publish(&event.Event{Type: event.EventTypeRiskTriggered, Data: map[string]interface{}{
				"bot_id": botID, "symbol": symCfg.Symbol, "exchange": symCfg.Exchange,
				"reason": "strategy_startup_unverified", "requires_reconciliation": true,
			}})
		}
	}

	// 🔥 如果啟动時已有持倉（满倉或接近满倉），立即調用 AdjustOrders 初始化賣單
	// 避免等待價格變化才触发订單調整，确保满倉状態下也能立即开始交易
	// 純趋势/动量等非網格多策略模式跳过首輪網格挂单，避免误挂
	if initializationErr != nil {
		// Initialize may have returned after partial acknowledgements. Keep the
		// startup gate sealed; the stop path will reconcile live positions/orders.
	} else if config.ShouldSkipInitialGridAdjustOrders(&localCfg) {
		logger.InfoCtx(ctx, "⏭️ [%s] 啟动時跳过網格订單初始化（當前為非網格多策略模式）", symCfg.Symbol)
	} else if err := superPositionManager.AdjustOrders(currentPrice); err != nil {
		if isOrderSkippedError(err) {
			logger.InfoCtx(ctx, "🔒 [%s] 啟动時部分價格位被其他實例鎖定，下一輪重試: %v", symCfg.Symbol, err)
		} else {
			logger.WarnCtx(ctx, "⚠️ [%s] 啟动時初始化订單失败: %v", symCfg.Symbol, err)
		}
	} else {
		logger.InfoCtx(ctx, "✅ [%s] 啟动時订單初始化完成（如有持倉已自动挂賣單）", symCfg.Symbol)
	}

	if storageService != nil {
		if st := storageService.GetStorage(); st != nil {
			restoreAdapter := &reconciliationRestoreAdapter{
				storage: st, accountID: accountID,
				accountScope: equityAccountScopeID(symCfg.Exchange, localCfg.Exchanges[symCfg.Exchange]),
				marketType:   symCfg.GetMarketType(), botID: botID,
			}
			if err := superPositionManager.RestoreReconciliationStats(restoreAdapter, symCfg.Exchange, symCfg.Symbol); err != nil {
				logger.WarnCtx(ctx, "⚠️ [%s] 恢複對账统计失败: %v", symCfg.Symbol, err)
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

	// K 線 regime：注入倉位管理器並啟動檢測器 + 間隔控制循環（stopFn 中停止）
	if err := gridRegime.start(ctx, superPositionManager); err != nil {
		logger.ErrorCtx(ctx, "❌ [%s] K 線 regime 啟動失敗，本 Bot 以原有網格邏輯運行: %v", symCfg.Symbol, err)
		gridRegime = nil
	}

	var trendDetector *strategy.TrendDetector
	// 舊 tick 級趨勢檢測器：regime_filter 啟用時不再為趨勢過濾創建（smart_position 仍需要）
	legacyTrendFilter := localCfg.Trading.GridRiskControl.TrendFilterEnabled && !localCfg.Trading.RegimeFilter.Enabled
	if localCfg.Trading.SmartPosition.Enabled || legacyTrendFilter {
		trendDetector = strategy.NewTrendDetector(&localCfg, priceMonitor)
		trendDetector.Start()
		// 將趋势检测器注入 SuperPositionManager
		superPositionManager.SetTrendDetector(trendDetector)
	}

	if localCfg.Strategies.Enabled {
		strategyManager = strategy.NewStrategyManager(&localCfg, botCapitalBudget)
		strategyManager.SetOrderUpdateErrorHandler(func(strategyName string, updateErr error) {
			superPositionManager.OpeningGate().Block("strategy_accounting_unverified")
			logger.ErrorCtx(ctx, "[%s] 策略 %s 成交账未能确认，已封锁 Bot 新开仓: %v", botID, strategyName, updateErr)
			if eventBus != nil {
				eventBus.Publish(&event.Event{Type: event.EventTypeRiskTriggered, Data: map[string]interface{}{
					"bot_id": botID, "symbol": symCfg.Symbol, "exchange": symCfg.Exchange,
					"reason": "strategy_accounting_unverified", "strategy_name": strategyName,
					"requires_reconciliation": true,
				}})
			}
		})
		// 設置事件總線
		if eventBus != nil {
			strategyManager.SetEventBus(eventBus)
		}
		multiExecutor = strategy.NewMultiStrategyExecutor(exchangeExecutor, strategyManager.GetCapitalAllocator())
		// S9：顯式聲明單腿對沖策略的持倉方向，用於判斷開/平倉（不再按策略名稱猜測）
		for strategyName, positionSide := range singleLegStrategyPositionSides {
			if err := multiExecutor.SetStrategyPositionSide(strategyName, positionSide); err != nil {
				logger.WarnCtx(ctx, "⚠️ [%s] 設置策略持倉方向失败: %v", symCfg.Symbol, err)
			}
		}

		if gridCfg, exists := localCfg.Strategies.Configs["grid"]; exists && gridCfg.Enabled {
			gridStrategy := strategy.NewGridStrategy("grid", &localCfg, executorAdapter, exchangeAdapter, superPositionManager)
			fixedPool := 0.0
			if pool, ok := gridCfg.Config["capital_pool"].(float64); ok {
				fixedPool = pool
			}
			strategyManager.RegisterStrategy("grid", gridStrategy, gridCfg.Weight, fixedPool)
			logger.InfoCtx(ctx, "✅ [%s] 網格策略已注册", symCfg.Symbol)
		}

		if trendCfg, exists := localCfg.Strategies.Configs["trend"]; exists && trendCfg.Enabled {
			trendExecutor := strategy.NewMultiStrategyExecutorAdapter(multiExecutor, "trend")
			trendStrategy := strategy.NewTrendFollowingStrategy("trend", &localCfg, trendExecutor, exchangeAdapter, trendCfg.Config)
			if storageService != nil {
				trendStrategy.SetRuntimeStateStore(&strategyRuntimeStateAdapter{storageService: storageService, botID: botID})
			}
			fixedPool := 0.0
			if pool, ok := trendCfg.Config["capital_pool"].(float64); ok {
				fixedPool = pool
			}
			strategyManager.RegisterStrategy("trend", trendStrategy, trendCfg.Weight, fixedPool)
			logger.InfoCtx(ctx, "✅ [%s] 趋势策略已注册", symCfg.Symbol)
		}

		if meanCfg, exists := localCfg.Strategies.Configs["mean_reversion"]; exists && meanCfg.Enabled {
			meanExecutor := strategy.NewMultiStrategyExecutorAdapter(multiExecutor, "mean_reversion")
			meanStrategy := strategy.NewMeanReversionStrategy("mean_reversion", &localCfg, meanExecutor, exchangeAdapter, meanCfg.Config)
			if storageService != nil {
				meanStrategy.SetRuntimeStateStore(&strategyRuntimeStateAdapter{storageService: storageService, botID: botID})
			}
			fixedPool := 0.0
			if pool, ok := meanCfg.Config["capital_pool"].(float64); ok {
				fixedPool = pool
			}
			strategyManager.RegisterStrategy("mean_reversion", meanStrategy, meanCfg.Weight, fixedPool)
			logger.InfoCtx(ctx, "✅ [%s] 均值回归策略已注册", symCfg.Symbol)
		}

		if momentumCfg, exists := localCfg.Strategies.Configs["momentum"]; exists && momentumCfg.Enabled {
			momentumExecutor := strategy.NewMultiStrategyExecutorAdapter(multiExecutor, "momentum")
			momentumStrategy := strategy.NewMomentumStrategy("momentum", &localCfg, momentumExecutor, exchangeAdapter, momentumCfg.Config)
			if storageService != nil {
				momentumStrategy.SetRuntimeStateStore(&strategyRuntimeStateAdapter{storageService: storageService, botID: botID})
			}
			fixedPool := 0.0
			if pool, ok := momentumCfg.Config["capital_pool"].(float64); ok {
				fixedPool = pool
			}
			strategyManager.RegisterStrategy("momentum", momentumStrategy, momentumCfg.Weight, fixedPool)
			logger.InfoCtx(ctx, "✅ [%s] 动量策略已注册", symCfg.Symbol)
		}

		if martinCfg, exists := localCfg.Strategies.Configs["martingale"]; exists && martinCfg.Enabled {
			martinExecutor := strategy.NewMultiStrategyExecutorAdapter(multiExecutor, "martingale")
			martinStrategy := strategy.NewMartingaleStrategy("martingale", symCfg.Symbol, &localCfg, martinExecutor, exchangeAdapter, martinCfg.Config)
			if storageService != nil {
				martinStrategy.SetRuntimeStateStore(&strategyRuntimeStateAdapter{storageService: storageService, botID: botID})
			}
			fixedPool := 0.0
			if pool, ok := martinCfg.Config["capital_pool"].(float64); ok {
				fixedPool = pool
			}
			strategyManager.RegisterStrategy("martingale", martinStrategy, martinCfg.Weight, fixedPool)
			logger.InfoCtx(ctx, "✅ [%s] 马丁格尔策略已注册", symCfg.Symbol)
		}

		// DCA 策略（普通 DCA，使用 DCAEnhancedStrategy 實現）
		if dcaCfg, exists := localCfg.Strategies.Configs["dca"]; exists && dcaCfg.Enabled {
			dcaExecutor := strategy.NewMultiStrategyExecutorAdapter(multiExecutor, "dca")
			dcaStrategy := strategy.NewDCAEnhancedStrategy("dca", symCfg.Symbol, &localCfg, dcaExecutor, exchangeAdapter, dcaCfg.Config)
			if storageService != nil {
				dcaStrategy.SetRuntimeStateStore(&strategyRuntimeStateAdapter{storageService: storageService, botID: botID})
			}
			// 🔥 設置交易存儲，用於保存止损单的交易記錄
			if storageService != nil {
				tradeStorageAdapter := &tradeStorageAdapter{
					storageService: storageService,
					accountID:      accountID,
					accountScope:   equityAccountScopeID(symCfg.Exchange, localCfg.Exchanges[symCfg.Exchange]),
					botID:          botID,
					marketType:     strings.ToLower(strings.TrimSpace(symCfg.GetMarketType())),
					pnlAsset:       strings.ToUpper(strings.TrimSpace(exchangeAdapter.GetQuoteAsset())),
				}
				dcaStrategy.SetTradeStorage(tradeStorageAdapter)
			}
			fixedPool := 0.0
			if pool, ok := dcaCfg.Config["capital_pool"].(float64); ok {
				fixedPool = pool
			}
			strategyManager.RegisterStrategy("dca", dcaStrategy, dcaCfg.Weight, fixedPool)
			logger.InfoCtx(ctx, "✅ [%s] DCA 定投策略已注册", symCfg.Symbol)
		}

		// DCA Enhanced 策略（增強型 DCA）
		if dcaEnhancedCfg, exists := localCfg.Strategies.Configs["dca_enhanced"]; exists && dcaEnhancedCfg.Enabled {
			dcaEnhancedExecutor := strategy.NewMultiStrategyExecutorAdapter(multiExecutor, "dca_enhanced")
			dcaEnhancedStrategy := strategy.NewDCAEnhancedStrategy("dca_enhanced", symCfg.Symbol, &localCfg, dcaEnhancedExecutor, exchangeAdapter, dcaEnhancedCfg.Config)
			if storageService != nil {
				dcaEnhancedStrategy.SetRuntimeStateStore(&strategyRuntimeStateAdapter{storageService: storageService, botID: botID})
			}
			// 🔥 設置交易存儲，用於保存止损单的交易記錄
			if storageService != nil {
				tradeStorageAdapter := &tradeStorageAdapter{
					storageService: storageService,
					accountID:      accountID,
					accountScope:   equityAccountScopeID(symCfg.Exchange, localCfg.Exchanges[symCfg.Exchange]),
					botID:          botID,
					marketType:     strings.ToLower(strings.TrimSpace(symCfg.GetMarketType())),
					pnlAsset:       strings.ToUpper(strings.TrimSpace(exchangeAdapter.GetQuoteAsset())),
				}
				dcaEnhancedStrategy.SetTradeStorage(tradeStorageAdapter)
			}
			fixedPool := 0.0
			if pool, ok := dcaEnhancedCfg.Config["capital_pool"].(float64); ok {
				fixedPool = pool
			}
			strategyManager.RegisterStrategy("dca_enhanced", dcaEnhancedStrategy, dcaEnhancedCfg.Weight, fixedPool)
			logger.InfoCtx(ctx, "✅ [%s] 增强型 DCA 策略已注册", symCfg.Symbol)
		}

		if comboCfg, exists := localCfg.Strategies.Configs["combo"]; exists && comboCfg.Enabled {
			comboExecutor := strategy.NewMultiStrategyExecutorAdapter(multiExecutor, "combo")
			comboStrategy := strategy.NewComboStrategy("combo", symCfg.Symbol, &localCfg, comboExecutor, exchangeAdapter, comboCfg.Config)
			stateReady := true
			if storageService != nil {
				if err := comboStrategy.SetRuntimeStateStore(&strategyRuntimeStateAdapter{storageService: storageService, botID: botID}); err != nil {
					stateReady = false
					superPositionManager.OpeningGate().Block("combo_runtime_state_unverified")
					superPositionManager.CancelAllOpenOrders()
					logger.ErrorCtx(ctx, "[%s] Combo 运行态恢复失败，已封锁 Bot 开仓并跳过 Combo 策略注册；运行时保留供安全停止与对账: %v", botID, err)
					if eventBus != nil {
						eventBus.Publish(&event.Event{Type: event.EventTypeRiskTriggered, Data: map[string]interface{}{
							"bot_id": botID, "symbol": symCfg.Symbol, "exchange": symCfg.Exchange,
							"reason": "combo_runtime_state_unverified", "strategy_name": "combo",
							"requires_reconciliation": true,
						}})
					}
				}
			}
			if stateReady {
				comboStrategy.SetRuntimeStateErrorHandler(func(stateErr error) {
					if stateErr != nil {
						superPositionManager.OpeningGate().Block("combo_runtime_state_unverified")
						logger.ErrorCtx(ctx, "[%s] Combo 回撤高水位持久化失败，已封锁 Bot 开仓: %v", botID, stateErr)
						return
					}
					superPositionManager.OpeningGate().Unblock("combo_runtime_state_unverified")
				})
				fixedPool := 0.0
				if pool, ok := comboCfg.Config["capital_pool"].(float64); ok {
					fixedPool = pool
				}
				strategyManager.RegisterStrategy("combo", comboStrategy, comboCfg.Weight, fixedPool)
				logger.InfoCtx(ctx, "✅ [%s] 组合策略已注册", symCfg.Symbol)
			}
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
					if storageService != nil {
						spotShortStrategy.SetRuntimeStateStore(&strategyRuntimeStateAdapter{storageService: storageService, botID: botID})
					}
					spotShortStrategy.SetRuntimeStateErrorHandler(func(stateErr error) {
						superPositionManager.OpeningGate().Block("strategy_accounting_unverified")
						logger.ErrorCtx(ctx, "[%s] SpotShort 运行态未能持久化，已封锁 Bot 新开仓: %v", botID, stateErr)
						if eventBus != nil {
							eventBus.Publish(&event.Event{Type: event.EventTypeRiskTriggered, Data: map[string]interface{}{
								"bot_id": botID, "symbol": symCfg.Symbol, "exchange": symCfg.Exchange,
								"reason": "strategy_accounting_unverified", "strategy_name": "spot_short",
								"requires_reconciliation": true,
							}})
						}
					})
					spotShortStrategy.SetUnresolvedDebtHandler(func(debtErr error) {
						superPositionManager.OpeningGate().Block("strategy_accounting_unverified")
						logger.ErrorCtx(ctx, "[%s] SpotShort 借贷结果/补偿还款未核实，已封锁 Bot 新开仓: %v", botID, debtErr)
						if eventBus != nil {
							eventBus.Publish(&event.Event{Type: event.EventTypeRiskTriggered, Data: map[string]interface{}{
								"bot_id": botID, "symbol": symCfg.Symbol, "exchange": symCfg.Exchange,
								"reason": "strategy_accounting_unverified", "strategy_name": "spot_short",
								"requires_reconciliation": true,
							}})
						}
					})
					strategyManager.RegisterStrategy("spot_short", spotShortStrategy, si.Weight, 0)
					logger.InfoCtx(ctx, "✅ [%s] 現貨做空策略已注册 (group=%v)", symCfg.Symbol, spotShortCfg["group_id"])
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
					if storageService != nil {
						spotLongStrategy.SetRuntimeStateStore(&strategyRuntimeStateAdapter{storageService: storageService, botID: botID})
					}
					spotLongStrategy.SetRuntimeStateErrorHandler(func(stateErr error) {
						superPositionManager.OpeningGate().Block("strategy_accounting_unverified")
						logger.ErrorCtx(ctx, "[%s] SpotLong 运行态未能持久化，已封锁 Bot 新开仓: %v", botID, stateErr)
						if eventBus != nil {
							eventBus.Publish(&event.Event{Type: event.EventTypeRiskTriggered, Data: map[string]interface{}{
								"bot_id": botID, "symbol": symCfg.Symbol, "exchange": symCfg.Exchange,
								"reason": "strategy_accounting_unverified", "strategy_name": "spot_long",
								"requires_reconciliation": true,
							}})
						}
					})
					strategyManager.RegisterStrategy("spot_long", spotLongStrategy, si.Weight, 0)
					logger.InfoCtx(ctx, "✅ [%s] 現貨做多對沖策略已注册 (group=%v)", symCfg.Symbol, spotLongCfg["group_id"])
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
					futuresShortStrategy.SetRuntimeStateStore(&strategyRuntimeStateAdapter{storageService: storageService, botID: botID})
					strategyManager.RegisterStrategy("futures_short", futuresShortStrategy, si.Weight, 0)
					logger.InfoCtx(ctx, "✅ [%s] 合約做空對沖策略已注册 (group=%v)", symCfg.Symbol, futuresShortCfg["group_id"])
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
					futuresLongStrategy.SetRuntimeStateStore(&strategyRuntimeStateAdapter{storageService: storageService, botID: botID})
					strategyManager.RegisterStrategy("futures_long", futuresLongStrategy, si.Weight, 0)
					logger.InfoCtx(ctx, "✅ [%s] 合約做多對沖策略已注册 (group=%v)", symCfg.Symbol, futuresLongCfg["group_id"])
					break
				}
			}
		}

		// Routes come only from validated account/Bot-scoped intent records.
		// A restored route is not proof that strategy capital/fills are settled.
		for _, route := range exchangeExecutor.RecoveredOrderRoutes() {
			multiExecutor.RestoreOrderRoute(route.OrderID, route.ClientOrderID, route.StrategyName)
		}

		if err := startStrategiesWithFailClosedGate(strategyManager.StartAll, superPositionManager.OpeningGate()); err != nil {
			logger.ErrorCtx(ctx, "❌ [%s] 啟动策略管理器失败: %v", symCfg.Symbol, err)
		} else {
			logger.InfoCtx(ctx, "✅ [%s] 多策略系统已啟动", symCfg.Symbol)
		}
	}

	// 價格变动处理
	go watchPriceFeedHealth(ctx, priceFeedCheckEvery, func() bool {
		return priceMonitor.IsStale(priceFeedStaleAfter)
	}, func(stale bool) {
		superPositionManager.SetPriceFeedStale(stale)
		if stale {
			logger.ErrorCtx(ctx, "[%s] 價格 WebSocket 超過 %s 無有效推送：封鎖新開倉並撤銷本 Bot 開倉委託", symCfg.Symbol, priceFeedStaleAfter)
			superPositionManager.CancelAllOpenOrders()
			go superPositionManager.CancelResidualOpeningOrders()
			return
		}
		logger.InfoCtx(ctx, "[%s] 價格 WebSocket 已恢復；僅解除行情過期開倉封鎖", symCfg.Symbol)
	})

	go func() {
		defer func() {
			if r := recover(); r != nil {
				logger.ErrorCtx(ctx, "❌ [%s] 價格變化处理协程 panic: %v", symCfg.Symbol, r)
			}
		}()

		priceCh := priceMonitor.Subscribe()
		var lastTriggered bool

		for {
			select {
			case <-ctx.Done():
				logger.DebugCtx(ctx, "⏹️ [%s] 價格變化处理协程已停止", symCfg.Symbol)
				return
			case priceChange, ok := <-priceCh:
				if !ok {
					// channel 已关闭
					logger.DebugCtx(ctx, "⏹️ [%s] 價格變化 channel 已关闭", symCfg.Symbol)
					return
				}
				if err := exchangeExecutor.ObserveExposureMark(priceChange.NewPrice, priceChange.Timestamp); err != nil {
					logger.DebugCtx(ctx, "[%s] exposure mark rejected; new openings remain subject to quote readiness: %v", symCfg.Symbol, err)
				}

				// Update risk before any strategy receives this same tick. A second
				// monitor subscriber would steal ticks from the trading consumer.
				dynamicAdjuster.OnPriceChange(priceChange)
				isTriggered := riskMonitor.IsTriggered() || depthMonitor.IsTriggered()
				if isTriggered != lastTriggered {
					superPositionManager.SetMarketRiskPaused(isTriggered)
				}
				if isTriggered {
					if !lastTriggered {
						logger.WarnCtx(ctx, "🚨 [%s][风控触发] 暂停新開倉並撤销開倉單，继续持倉保护", symCfg.Symbol)
						// 按方向撤銷開倉單（LONG 撤買單、SHORT 撤賣單），避免做空時誤撤平倉單
						superPositionManager.CancelAllOpenOrders()
						go superPositionManager.CancelResidualOpeningOrders()
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
				}

				if lastTriggered && !isTriggered {
					logger.InfoCtx(ctx, "✅ [%s][风控解除] 恢複自动交易", symCfg.Symbol)
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
						logAdjustOrdersError(ctx, symCfg.Symbol, err)
					}
					localCfg.Trading.BuyWindowSize = origBuy
					localCfg.Trading.SellWindowSize = origSell
				} else {
					if strategyManager == nil || !localCfg.Strategies.Enabled {
						if err := superPositionManager.AdjustOrders(priceChange.NewPrice); err != nil {
							logAdjustOrdersError(ctx, symCfg.Symbol, err)
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
					logger.ErrorCtx(ctx, "❌ [%s] 配置档案切换协程 panic: %v", symCfg.Symbol, r)
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
					logger.DebugCtx(ctx, "⏹️ [%s] 配置档案切换协程已停止", symCfg.Symbol)
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
						logger.WarnCtx(ctx, "⚠️ [%s] 获取最新配置失败，跳过 profile 切换检查: %v", symCfg.Symbol, err)
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
						logger.InfoCtx(ctx, "🔄 [%s:%s] 检测到费率变化，建议从配置档案 '%s' 切换到 '%s' (资金费率: %.6f%%, 手续费率: %.6f%%)",
							symCfg.Exchange, symCfg.Symbol, currentProfileName, newProfileName,
							currentFundingRate*100, currentFeeRate*100)

						// 记录新配置参数
						if newProfileName != "default" {
							logger.InfoCtx(ctx, "📋 [%s:%s] 新配置档案参数: price_interval=%.2f, order_quantity=%.2f, buy_window=%d, sell_window=%d",
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
		Config:                symCfg,
		Exchange:              ex,
		PriceMonitor:          priceMonitor,
		RiskMonitor:           riskMonitor,
		DepthMonitor:          depthMonitor,
		FundingMonitor:        fundingMonitor,
		ArbitrageManager:      arbitrageManager,
		SuperPositionManager:  superPositionManager,
		OrderCleaner:          orderCleaner,
		Reconciler:            reconciler,
		TrendDetector:         trendDetector,
		DynamicAdjuster:       dynamicAdjuster,
		StrategyManager:       strategyManager,
		ExchangeExecutor:      exchangeExecutor,
		ExecutorAdapter:       executorAdapter,
		ExchangeAdapter:       exchangeAdapter,
		EventBus:              eventBus,
		StorageService:        storageService,
		AccountID:             accountID,
		AccountScope:          equityAccountScopeID(symCfg.Exchange, localCfg.Exchanges[symCfg.Exchange]),
		AccountMarketType:     symCfg.GetMarketType(),
		verifiedCapitalBudget: botCapitalBudget,
	}
	ownershipRuntime.Store(rt)
	if ownershipLease.Lost() {
		rt.markShutdownCloseUnverified("Bot 运行所有权租约丢失，禁止独立追加平仓")
	}
	rt.ClampOpenControl = func(control config.OpenPositionControl) (config.OpenPositionControl, error) {
		control = config.CloneOpenPositionControl(control)
		if rt.verifiedCapitalBudget <= 0 {
			return config.OpenPositionControl{}, fmt.Errorf("verified Bot capital budget is unavailable")
		}
		if err := applyBotCapitalLimit(&control, rt.verifiedCapitalBudget); err != nil {
			return config.OpenPositionControl{}, err
		}
		return control, nil
	}

	// 開倉控制器（限倉、定時、週期規則）
	openingController := position.NewOpeningController(superPositionManager, &rt.Config)
	openingController.Start()
	rt.OpeningController = openingController

	// 按 timing.fee_rate_refresh_minutes 定期刷新費率（放在所有可能提前返回的初始化步驟之後，避免協程洩漏）
	stopFeeRefresh := startGridFeeRateRefresh(ctx, &localCfg, symCfg, feeRate, superPositionManager)
	// 網格自動重建同樣放在所有提前返回之後；停止時先停它，避免平倉過程中重新錨定網格
	stopAutoRebuild := startConfiguredAutoRebuild(ctx, symCfg, superPositionManager, !config.ShouldSkipInitialGridAdjustOrders(&localCfg))

	var stopOnce sync.Once
	var stopErr error
	stopFn := func() error {
		stopOnce.Do(func() {
			var stopErrors []error
			sealRuntimeShutdown(rt)
			if ownershipLease.Lost() {
				rt.markShutdownCloseUnverified("Bot 运行所有权租约丢失，禁止独立追加平仓")
			}
			stopFeeRefresh()
			stopAutoRebuild()
			if rt.fundingIncomeCancel != nil {
				rt.fundingIncomeCancel()
			}
			if rt.capitalReservationStop != nil {
				rt.capitalReservationStop()
			}
			protectiveSettled := true
			shutdownCtx := rt.stopContext(ctx)
			liquidationStopCtx, liquidationStopCancel := context.WithTimeout(shutdownCtx, runtimeShutdownPrepareTimeout)
			if err := prepareRuntimeShutdown(liquidationStopCtx, rt, symCfg.CloseOnStop); err != nil {
				protectiveSettled = false
				rt.markShutdownCloseUnverified(err.Error())
				stopErrors = append(stopErrors, fmt.Errorf("停止前订单/保护性平仓准备未核实: %w", err))
				logger.ErrorCtx(ctx, "[%s] 停止時保護性平倉未核實，保留待對賬阻斷: %v", symCfg.Symbol, err)
			}
			liquidationStopCancel()
			// 終止時平倉：close_on_stop=true 時按 close_on_stop_config 平倉，未配置則全平（見 runCloseOnStop）
			if protectiveSettled && !ownershipLease.Lost() {
				if err := closeOnStopForRuntime(shutdownCtx, symCfg, rt); err != nil {
					stopErrors = append(stopErrors, err)
				}
			} else if ownershipLease.Lost() {
				rt.markShutdownCloseUnverified("Bot 运行所有权租约丢失，停止时不执行独立平仓")
				stopErrors = append(stopErrors, fmt.Errorf("Bot 运行所有权租约已丢失，停止平仓无法安全执行"))
			}
			logger.InfoCtx(ctx, "⏹️ [%s] 停止開倉控制器...", symCfg.Symbol)
			if rt.OpeningController != nil {
				rt.OpeningController.Stop()
			}
			logger.InfoCtx(ctx, "⏹️ [%s] 停止價格監控...", symCfg.Symbol)
			if priceMonitor != nil {
				priceMonitor.Stop()
			}
			logger.InfoCtx(ctx, "⏹️ [%s] 停止訂單流...", symCfg.Symbol)
			ex.StopOrderStream()
			logger.InfoCtx(ctx, "⏹️ [%s] 停止风控監視器...", symCfg.Symbol)
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
			gridRegime.stop()
			if strategyManager != nil {
				if err := strategyManager.StopAllWithError(); err != nil {
					rt.markShutdownCloseUnverified(err.Error())
					stopErrors = append(stopErrors, fmt.Errorf("策略停止未核实: %w", err))
				}
			}
			if len(rt.capitalReservationClaims) > 0 {
				if len(stopErrors) != 0 || ownershipLease.Lost() {
					logger.ErrorCtx(ctx, "[%s] 停止链路或运行租约未核实，保留账户钱包资金预留", botID)
				} else {
					capitalReleaseCtx, capitalReleaseCancel := context.WithTimeout(shutdownCtx, 20*time.Second)
					releaseErr := verifyAndReleaseAccountWalletCapitalGuarded(capitalReleaseCtx, rt.capitalReservationStore,
						rt.capitalReservationBotID, rt.capitalReservationClaims,
						func(verifyCtx context.Context) error {
							if strings.EqualFold(rt.AccountMarketType, "spot") {
								return verifyStandardSpotRuntimeFlat(verifyCtx, rt.Exchange, symCfg.Symbol, func() error {
									return verifyStandardSpotBotInventoryFlat(rt.SuperPositionManager, rt.StrategyManager, symCfg.Symbol)
								})
							}
							if strings.EqualFold(rt.AccountMarketType, "spot_margin") {
								return verifyStandardSpotMarginRuntimeFlat(verifyCtx, rt.Exchange, func() error {
									return verifyStandardSpotBotInventoryFlat(rt.SuperPositionManager, rt.StrategyManager, symCfg.Symbol)
								})
							}
							return verifyStandardRuntimeFlat(verifyCtx, rt.Exchange, rt.AccountMarketType, symCfg.Symbol)
						}, func() error {
							if ownershipLease.Lost() {
								return errors.New("runtime ownership lease lost during flatness verification")
							}
							return nil
						})
					capitalReleaseCancel()
					if releaseErr != nil {
						rt.markShutdownCloseUnverified(releaseErr.Error())
						stopErrors = append(stopErrors, fmt.Errorf("普通 Bot 资金预留未能核实释放: %w", releaseErr))
						logger.ErrorCtx(ctx, "[%s] 资金 claim 保留，仓位/委托平仓证据不足: %v", botID, releaseErr)
					} else {
						logger.InfoCtx(ctx, "[%s] 期货仓位与活动委托已核实为空，账户钱包资金 claim 已释放", botID)
					}
				}
			}
			if len(stopErrors) == 0 {
				if reason := rt.shutdownCloseUnverifiedReason(); reason != "" {
					stopErrors = append(stopErrors, fmt.Errorf("停止状态仍需对账: %s", reason))
				}
			}
			released, releaseErr := releaseRuntimeOwnershipLeaseAfterVerifiedStop(ownershipLease, stopErrors, rt.shutdownCloseUnverifiedReason())
			if releaseErr != nil {
				logger.WarnCtx(ctx, "[%s] 安全核实停止或释放 Bot 运行所有权租约失败: %v", botID, releaseErr)
				stopErrors = append(stopErrors, fmt.Errorf("释放 Bot 运行所有权租约前核实失败: %w", releaseErr))
			} else if !released {
				logger.ErrorCtx(ctx, "[%s] 停止状态未核实；继续持有运行所有权租约，阻止其他实例接管", botID)
			} else {
				logger.InfoCtx(ctx, "[%s] 停止已核实，运行所有权租约已释放", botID)
			}
			stopErr = errors.Join(stopErrors...)
		})
		return stopErr
	}
	rt.StopWithError = stopFn
	rt.Stop = func() { _ = stopFn() }
	if symCfg.GetMarketType() == "futures" && storageService != nil && storageService.GetStorage() != nil {
		fundingSyncCtx, cancelFundingSync := context.WithCancel(ctx)
		rt.fundingIncomeCancel = cancelFundingSync
		go startFundingIncomeSync(fundingSyncCtx, storageService.GetStorage(), ex,
			symCfg.Exchange, symCfg.Symbol, accountID, symCfg.GetMarketType(), rt.AccountScope)
	}
	if capitalClaimReady && storageService != nil && storageService.GetStorage() != nil {
		if reservationStore, ok := storageService.GetStorage().(storage.AccountWalletCapitalReservationStore); ok {
			rt.capitalReservationStore = reservationStore
			rt.capitalReservationBotID = botID
			rt.capitalReservationClaims = []storage.AccountWalletCapitalClaim{capitalClaim}
			if checker, supported := reservationStore.(storage.AccountWalletCapitalAdmissionChecker); supported {
				exchangeExecutor.SetOpeningAdmissionGuard(func(guardCtx context.Context) error {
					return checker.CheckAccountWalletCapitalAdmission(guardCtx, []string{capitalClaim.WalletKey}, 2*accountWalletCapitalRefreshInterval)
				})
			} else {
				exchangeExecutor.SetOpeningAdmissionGuard(func(context.Context) error {
					return fmt.Errorf("persistent storage does not support shared wallet opening admission checks")
				})
			}
			rt.capitalReservationStop = startRuntimeAccountWalletCapitalRevalidation(ctx, baseCfg, storageService,
				distributedLock, botID, rt.capitalReservationClaims,
				[]accountWalletBalanceReader{accountWalletBalanceReaderForClaim(capitalClaim, ex, storageService)}, superPositionManager.OpeningGate(),
				func(cancelCtx context.Context) error { return exchangeExecutor.CancelOwnedOpeningOrders(cancelCtx) })
		}
	}
	ownershipLeaseTransferred = true
	dynamicOwnedByRuntime = true

	return rt, nil
}

func applyStartupOpeningPauseHolders(gate *execution.OpeningGate, holders []storage.OpeningPauseHolder) {
	if gate == nil {
		return
	}
	for _, holder := range holders {
		if holder.Source != "" {
			gate.Block(holder.Source)
		}
	}
}

func validateLegacyFundingArbitrageReadiness(cfg *config.Config, marketType string) error {
	if cfg == nil || marketType != "futures" || !cfg.FundingRate.Enabled || !cfg.FundingRate.ArbitrageEnabled {
		return nil
	}
	return fmt.Errorf("funding_rate.arbitrage_enabled is unsupported: spot orders are not wired to the managed execution journal and risk admission; disable it until the managed spot hedge path is implemented")
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
	commissionKnown := false // Missing metadata is not proof of a zero commission.
	if field := v.FieldByName("CommissionKnown"); field.IsValid() && field.Kind() == reflect.Bool {
		commissionKnown = field.Bool()
	}
	commissionIncomplete := false
	if field := v.FieldByName("CommissionIncomplete"); field.IsValid() && field.Kind() == reflect.Bool {
		commissionIncomplete = field.Bool()
	}

	return &position.OrderUpdate{
		OrderID:              getInt64Field("OrderID"),
		ClientOrderID:        getStringField("ClientOrderID"),
		Symbol:               getStringField("Symbol"),
		Status:               getStringField("Status"),
		ExecutedQty:          getFloat64Field("ExecutedQty"),
		Price:                getFloat64Field("Price"),
		AvgPrice:             getFloat64Field("AvgPrice"),
		Side:                 getStringField("Side"),
		Type:                 getStringField("Type"),
		UpdateTime:           getInt64Field("UpdateTime"),
		Commission:           getFloat64Field("Commission"),
		CommissionAsset:      getStringField("CommissionAsset"),
		CommissionKnown:      commissionKnown,
		CommissionIncomplete: commissionIncomplete,
		RealizedPnL:          getFloat64Field("RealizedPnL"),
		BaseFeeQty:           getFloat64Field("BaseFeeQty"),
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
