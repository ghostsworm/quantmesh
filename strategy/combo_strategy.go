package strategy

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"quantmesh/config"
	"quantmesh/indicators"
	"quantmesh/logger"
	"quantmesh/position"
)

// ComboStrategy 组合策略
// 特点：
// 1. 多空對冲：同時运行多头和空头策略
// 2. 市况自适应：根據市场状態自动切换策略权重
// 3. 策略组合：支援任意策略组合（如马丁+DCA）
// 4. 全時况覆盖：上涨、下跌、震荡行情均可盈利
type ComboStrategy struct {
	name        string
	cfg         *config.Config
	executor    position.OrderExecutorInterface
	exchange    position.IExchange
	strategyCfg *ComboConfig
	initErr     error

	// 子策略
	strategies    []Strategy
	strategyNames []string
	weights       []float64 // 各策略权重

	// 市场状態检测
	marketState  MarketState
	priceHistory []float64
	candles      []indicators.Candle
	lastPrice    float64

	// 状態
	mu        sync.RWMutex
	ctx       context.Context
	cancel    context.CancelFunc
	isRunning bool
	isPaused  bool // 暂停標志

	// 组合风控：持久化的权益高水位及其恢复状态
	peakEquity               float64
	runtimeStateStore        RuntimeStateStore
	runtimeStateErrorHandler func(error)
	runtimeStateDirty        bool
	runtimeStateFound        bool

	// 统计
	stats *StrategyStatistics

	// 事件總線
	eventBus EventBus
}

// MarketState 市场状態
type MarketState string

const (
	MarketBullish  MarketState = "bullish"  // 牛市（上涨趋势）
	MarketBearish  MarketState = "bearish"  // 熊市（下跌趋势）
	MarketSideways MarketState = "sideways" // 震荡市
	MarketVolatile MarketState = "volatile" // 高波动
)

// riskOnlyPriceHandler 子策略可選實現：只跑止盈止损、不开新倉。
// 组合策略在市况門控 / 敞口或回撤超限時調用，保证已有持倉的保护不被跳过（S4）。
type riskOnlyPriceHandler interface {
	OnPriceChangeRiskOnly(price float64) error
}

// comboPercentBase 百分比换算基數
const comboPercentBase = 100.0

// ComboConfig 组合策略配置
type ComboConfig struct {
	// 基础配置
	Symbol     string           `yaml:"symbol"`
	Strategies []StrategyConfig `yaml:"strategies"` // 子策略配置

	// 市况检测
	MarketDetection     bool    `yaml:"market_detection"`     // 啟用市况检测
	TrendPeriod         int     `yaml:"trend_period"`         // 趋势周期
	VolatilityPeriod    int     `yaml:"volatility_period"`    // 波动率周期
	VolatilityThreshold float64 `yaml:"volatility_threshold"` // 高波动阈值

	// 权重調整
	AdaptiveWeights   bool `yaml:"adaptive_weights"`   // 自适应权重
	RebalanceInterval int  `yaml:"rebalance_interval"` // 再平衡间隔（秒）

	// 對冲設置
	HedgeEnabled bool    `yaml:"hedge_enabled"` // 啟用對冲
	HedgeRatio   float64 `yaml:"hedge_ratio"`   // 對冲比例 (0.0-1.0)
	MaxDrawdown  float64 `yaml:"max_drawdown"`  // 最大回撤触发對冲

	// 风控
	TotalCapital float64 `yaml:"total_capital"` // 總资金
	MaxExposure  float64 `yaml:"max_exposure"`  // 最大敞口比例
}

// StrategyConfig 子策略配置
type StrategyConfig struct {
	Name       string                 `yaml:"name"`
	Type       string                 `yaml:"type"`      // dca/martingale/grid/trend
	Weight     float64                `yaml:"weight"`    // 权重
	Direction  string                 `yaml:"direction"` // LONG/SHORT/BOTH
	Parameters map[string]interface{} `yaml:"parameters"`

	// 市况适配
	PreferredMarket []MarketState `yaml:"preferred_market"` // 适合的市况
}

// NewComboStrategy 創建组合策略
func NewComboStrategy(
	name string,
	symbol string,
	cfg *config.Config,
	executor position.OrderExecutorInterface,
	exchange position.IExchange,
	strategyCfg map[string]interface{},
) *ComboStrategy {
	ctx, cancel := context.WithCancel(context.Background())

	comboCfg := parseComboConfig(strategyCfg)
	if symbol != "" {
		comboCfg.Symbol = symbol
	}

	combo := &ComboStrategy{
		name:          name,
		cfg:           cfg,
		executor:      executor,
		exchange:      exchange,
		strategyCfg:   comboCfg,
		strategies:    make([]Strategy, 0),
		strategyNames: make([]string, 0),
		weights:       make([]float64, 0),
		priceHistory:  make([]float64, 0, 200),
		candles:       make([]indicators.Candle, 0, 200),
		marketState:   MarketSideways,
		ctx:           ctx,
		cancel:        cancel,
		stats: &StrategyStatistics{
			TotalTrades: 0,
			WinRate:     0,
			TotalPnL:    0,
			TotalVolume: 0,
		},
	}

	// 創建子策略
	combo.initErr = combo.initializeStrategies()

	return combo
}

// parseComboConfig 解析组合配置
func parseComboConfig(cfg map[string]interface{}) *ComboConfig {
	comboCfg := &ComboConfig{
		Symbol:              "BTCUSDT",
		MarketDetection:     true,
		TrendPeriod:         20,
		VolatilityPeriod:    14,
		VolatilityThreshold: 3.0,
		AdaptiveWeights:     true,
		RebalanceInterval:   3600,
		HedgeEnabled:        true,
		HedgeRatio:          0.3,
		MaxDrawdown:         5.0,
		TotalCapital:        10000,
		MaxExposure:         0.8,
		Strategies:          make([]StrategyConfig, 0),
	}

	if cfg == nil {
		return comboCfg
	}

	// 辅助函數：安全地從 map 中獲取 float64
	getFloat := func(key string, defaultValue float64) float64 {
		if v, ok := cfg[key]; ok {
			switch val := v.(type) {
			case float64:
				return val
			case int:
				return float64(val)
			case int64:
				return float64(val)
			}
		}
		return defaultValue
	}

	// 辅助函數：安全地從 map 中獲取 int
	getInt := func(key string, defaultValue int) int {
		if v, ok := cfg[key]; ok {
			switch val := v.(type) {
			case int:
				return val
			case float64:
				return int(val)
			case int64:
				return int(val)
			}
		}
		return defaultValue
	}

	if v, ok := cfg["symbol"].(string); ok {
		comboCfg.Symbol = v
	}

	comboCfg.MarketDetection = getBoolParam(cfg, "market_detection", comboCfg.MarketDetection)
	comboCfg.TrendPeriod = getInt("trend_period", comboCfg.TrendPeriod)
	comboCfg.VolatilityPeriod = getInt("volatility_period", comboCfg.VolatilityPeriod)
	comboCfg.VolatilityThreshold = getFloat("volatility_threshold", comboCfg.VolatilityThreshold)
	comboCfg.AdaptiveWeights = getBoolParam(cfg, "adaptive_weights", comboCfg.AdaptiveWeights)
	comboCfg.RebalanceInterval = getInt("rebalance_interval", comboCfg.RebalanceInterval)
	comboCfg.HedgeEnabled = getBoolParam(cfg, "hedge_enabled", comboCfg.HedgeEnabled)
	comboCfg.HedgeRatio = getFloat("hedge_ratio", comboCfg.HedgeRatio)
	comboCfg.MaxDrawdown = getFloat("max_drawdown", comboCfg.MaxDrawdown)
	comboCfg.TotalCapital = getFloat("total_capital", comboCfg.TotalCapital)
	comboCfg.MaxExposure = getFloat("max_exposure", comboCfg.MaxExposure)

	// 解析子策略配置
	if strategies, ok := cfg["strategies"].([]interface{}); ok {
		for _, s := range strategies {
			if stratMap, ok := s.(map[string]interface{}); ok {
				stratCfg := StrategyConfig{
					Name:       getStringParam(stratMap, "name", ""),
					Type:       getStringParam(stratMap, "type", "dca"),
					Weight:     getFloatParamCombo(stratMap, "weight", 1.0),
					Direction:  getStringParam(stratMap, "direction", "LONG"),
					Parameters: make(map[string]interface{}),
				}
				if params, ok := stratMap["parameters"].(map[string]interface{}); ok {
					stratCfg.Parameters = params
				}
				if preferred, ok := stratMap["preferred_market"].([]interface{}); ok {
					for _, p := range preferred {
						if ps, ok := p.(string); ok {
							stratCfg.PreferredMarket = append(stratCfg.PreferredMarket, MarketState(ps))
						}
					}
				}
				comboCfg.Strategies = append(comboCfg.Strategies, stratCfg)
			}
		}
	}

	// 如果沒有配置策略，新增預設組合
	if len(comboCfg.Strategies) == 0 {
		comboCfg.Strategies = []StrategyConfig{
			{
				Name:            "long_dca",
				Type:            "dca",
				Weight:          0.5,
				Direction:       "LONG",
				Parameters:      map[string]interface{}{"base_order_amount": 100.0},
				PreferredMarket: []MarketState{MarketBullish, MarketSideways},
			},
			{
				Name:            "short_martingale",
				Type:            "martingale",
				Weight:          0.3,
				Direction:       "SHORT",
				Parameters:      map[string]interface{}{"initial_amount": 50.0, "direction": "SHORT"},
				PreferredMarket: []MarketState{MarketBearish},
			},
			{
				Name:            "hedge_martingale",
				Type:            "martingale",
				Weight:          0.2,
				Direction:       "LONG",
				Parameters:      map[string]interface{}{"initial_amount": 30.0, "reverse_martingale": true},
				PreferredMarket: []MarketState{MarketBullish, MarketVolatile},
			},
		}
	}

	return comboCfg
}

func getStringParam(m map[string]interface{}, key, defaultVal string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return defaultVal
}

func getBoolParam(m map[string]interface{}, key string, defaultVal bool) bool {
	if v, ok := m[key].(bool); ok {
		return v
	}
	return defaultVal
}

func getFloatParamCombo(m map[string]interface{}, key string, defaultVal float64) float64 {
	if v, ok := m[key].(float64); ok {
		return v
	}
	if v, ok := m[key].(int); ok {
		return float64(v)
	}
	return defaultVal
}

// initializeStrategies 初始化子策略
func (s *ComboStrategy) initializeStrategies() error {
	for _, stratCfg := range s.strategyCfg.Strategies {
		var strategy Strategy

		// 添加符号到参數
		if stratCfg.Parameters == nil {
			stratCfg.Parameters = make(map[string]interface{})
		}
		stratCfg.Parameters["symbol"] = s.strategyCfg.Symbol

		switch stratCfg.Type {
		case "dca":
			if strings.EqualFold(strings.TrimSpace(stratCfg.Direction), position.PositionSideShort) {
				logger.Warn("⚠️ [%s] 子策略 %s 為 DCA，只支持做多，配置的 SHORT 方向將被忽略", s.name, stratCfg.Name)
			}
			strategy = NewDCAEnhancedStrategy(
				stratCfg.Name,
				s.strategyCfg.Symbol,
				s.cfg,
				s.executor,
				s.exchange,
				stratCfg.Parameters,
			)
		case "martingale":
			stratCfg.Parameters["direction"] = stratCfg.Direction
			strategy = NewMartingaleStrategy(
				stratCfg.Name,
				s.strategyCfg.Symbol,
				s.cfg,
				s.executor,
				s.exchange,
				stratCfg.Parameters,
			)
		case "trend":
			strategy = NewTrendFollowingStrategy(
				stratCfg.Name,
				s.cfg,
				s.executor,
				s.exchange,
				stratCfg.Parameters,
			)
		case "mean_reversion":
			strategy = NewMeanReversionStrategy(
				stratCfg.Name,
				s.cfg,
				s.executor,
				s.exchange,
				stratCfg.Parameters,
			)
		default:
			return fmt.Errorf("combo sub-strategy %q has unsupported type %q; refusing partial strategy startup", stratCfg.Name, stratCfg.Type)
		}

		if strategy != nil {
			s.strategies = append(s.strategies, strategy)
			s.strategyNames = append(s.strategyNames, stratCfg.Name)
			s.weights = append(s.weights, stratCfg.Weight)
		}
	}
	if len(s.strategies) == 0 {
		return errors.New("combo requires at least one supported sub-strategy")
	}
	return nil
}

// Name 回傳策略名稱
func (s *ComboStrategy) Name() string {
	return s.name
}

// Initialize 初始化策略
func (s *ComboStrategy) Initialize(cfg *config.Config, executor position.OrderExecutorInterface, exchange position.IExchange) error {
	s.cfg = cfg
	s.executor = executor
	s.exchange = exchange
	return nil
}

// SetRuntimeStateStore injects Bot-scoped durable storage into every stateful
// Combo child. Child keys are namespaced so sibling and top-level strategies
// cannot overwrite each other's recovery checkpoints.
func (s *ComboStrategy) SetRuntimeStateStore(store RuntimeStateStore) error {
	if store == nil {
		return fmt.Errorf("combo runtime state store is unavailable")
	}
	seen := make(map[string]struct{}, len(s.strategies))
	for i, child := range s.strategies {
		name := strings.TrimSpace(child.Name())
		if name == "" {
			return fmt.Errorf("combo child strategy name is empty")
		}
		if _, exists := seen[name]; exists {
			return fmt.Errorf("duplicate combo child strategy name %q", name)
		}
		seen[name] = struct{}{}
		if _, ok := child.(interface{ SetRuntimeStateStore(RuntimeStateStore) }); !ok {
			return fmt.Errorf("combo child strategy %q does not support durable runtime state", s.strategyNames[i])
		}
	}
	for _, child := range s.strategies {
		scoped := comboChildRuntimeStateStore{store: store, comboName: s.name, childName: child.Name()}
		child.(interface{ SetRuntimeStateStore(RuntimeStateStore) }).SetRuntimeStateStore(scoped)
	}
	s.mu.Lock()
	s.runtimeStateStore = store
	s.mu.Unlock()
	return nil
}

func (s *ComboStrategy) SetRuntimeStateErrorHandler(handler func(error)) {
	s.mu.Lock()
	s.runtimeStateErrorHandler = handler
	s.mu.Unlock()
}

// SetEventBus 設置事件總線
func (s *ComboStrategy) SetEventBus(bus EventBus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.eventBus = bus

	// 同步给所有子策略
	for _, strategy := range s.strategies {
		strategy.SetEventBus(bus)
	}
}

// Start 啟动策略
func (s *ComboStrategy) Start(ctx context.Context) error {
	if s.initErr != nil {
		return fmt.Errorf("initialize combo sub-strategies: %w", s.initErr)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if s.requiresRiskOnlyPriceHandling() {
		for i, strategy := range s.strategies {
			if _, ok := strategy.(riskOnlyPriceHandler); !ok {
				name := strategy.Name()
				if i < len(s.strategyNames) && s.strategyNames[i] != "" {
					name = s.strategyNames[i]
				}
				return fmt.Errorf("combo sub-strategy %s does not support risk-only price handling required by market/risk gates", name)
			}
		}
	}
	if err := s.restoreRuntimeState(); err != nil {
		return fmt.Errorf("restore combo runtime state: %w", err)
	}
	needsBaseline := s.strategyCfg != nil && s.strategyCfg.TotalCapital > 0 && s.strategyCfg.MaxDrawdown > 0 && !s.runtimeStateFound
	if needsBaseline {
		missingStateErr := errors.New("combo drawdown checkpoint is missing; validating child recovery before creating a baseline")
		if s.runtimeStateErrorHandler == nil {
			return fmt.Errorf("combo drawdown checkpoint is missing and no shared opening gate is available")
		}
		s.reportRuntimeStateError(missingStateErr)
	}
	s.mu.Lock()
	s.ctx = ctx
	s.isRunning = true
	s.mu.Unlock()

	// 啟动所有子策略
	for i, strategy := range s.strategies {
		if err := strategy.Start(ctx); err != nil {
			failures := []error{fmt.Errorf("start combo sub-strategy %s: %w", s.strategyNames[i], err)}
			if stopErr := strategy.Stop(); stopErr != nil {
				failures = append(failures, fmt.Errorf("stop failed sub-strategy %s: %w", s.strategyNames[i], stopErr))
			}
			for started := i - 1; started >= 0; started-- {
				if stopErr := s.strategies[started].Stop(); stopErr != nil {
					failures = append(failures, fmt.Errorf("rollback combo sub-strategy %s: %w", s.strategyNames[started], stopErr))
				}
			}
			s.mu.Lock()
			s.isRunning = false
			s.mu.Unlock()
			return errors.Join(failures...)
		}
	}
	if needsBaseline {
		if s.hasPriorEconomicActivity() {
			stateErr := errors.New("combo drawdown checkpoint is missing while child strategy history or exposure exists; reconciliation required")
			s.stopStartedChildren(len(s.strategies) - 1)
			s.mu.Lock()
			s.isRunning = false
			s.mu.Unlock()
			s.reportRuntimeStateError(stateErr)
			return stateErr
		}
		s.mu.Lock()
		s.peakEquity = s.strategyCfg.TotalCapital
		s.runtimeStateDirty = true
		s.mu.Unlock()
		if err := s.persistRuntimeState(); err != nil {
			s.stopStartedChildren(len(s.strategies) - 1)
			s.mu.Lock()
			s.isRunning = false
			s.mu.Unlock()
			s.reportRuntimeStateError(err)
			return fmt.Errorf("initialize combo drawdown checkpoint: %w", err)
		}
		s.reportRuntimeStateError(nil)
	}

	// 啟动市况检测循环
	if s.strategyCfg.MarketDetection {
		go s.marketDetectionLoop()
	}

	// 啟动权重再平衡循环
	if s.strategyCfg.AdaptiveWeights {
		go s.rebalanceLoop()
	}

	s.warnUnsupportedRiskConfig()

	logger.Info("✅ [%s] 组合策略已啟动，子策略數量: %d", s.name, len(s.strategies))
	for i, name := range s.strategyNames {
		logger.Info("   - %s (权重: %.2f)", name, s.weights[i])
	}

	return nil
}

func (s *ComboStrategy) hasPriorEconomicActivity() bool {
	for _, child := range s.strategies {
		if len(child.GetOrders()) > 0 || len(child.GetPositions()) > 0 {
			return true
		}
		stats := child.GetStatistics()
		if stats != nil && (stats.TotalTrades > 0 || stats.TotalPnL != 0 || stats.TotalVolume != 0) {
			return true
		}
	}
	return false
}

func (s *ComboStrategy) stopStartedChildren(last int) {
	for i := last; i >= 0; i-- {
		if err := s.strategies[i].Stop(); err != nil {
			logger.Error("❌ [%s] Combo 启动回滚时停止子策略 %s 失败: %v", s.name, s.strategyNames[i], err)
		}
	}
}

// Stop 停止策略
func (s *ComboStrategy) Stop() error {
	s.mu.Lock()
	s.isRunning = false
	s.mu.Unlock()

	// 停止所有子策略
	for i, strategy := range s.strategies {
		if err := strategy.Stop(); err != nil {
			logger.Error("❌ [%s] 子策略 %s 停止失败: %v", s.name, s.strategyNames[i], err)
		}
	}

	if s.cancel != nil {
		s.cancel()
	}

	logger.Info("⏹️ [%s] 组合策略已停止", s.name)
	return nil
}

// IsRunning 回傳策略是否已成功啟动
func (s *ComboStrategy) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.isRunning
}

// OnPriceChange 價格變化处理
func (s *ComboStrategy) OnPriceChange(price float64) error {
	s.mu.Lock()

	if s.isPaused {
		s.mu.Unlock()
		return nil
	}

	// 更新價格历史
	s.priceHistory = append(s.priceHistory, price)
	if len(s.priceHistory) > 200 {
		// 使用 copy 而不是切片截取，避免記憶體泄漏
		newHistory := make([]float64, 200)
		copy(newHistory, s.priceHistory[len(s.priceHistory)-200:])
		s.priceHistory = newHistory
	}
	s.lastPrice = price

	// 更新 K線
	s.updateCandle(price)

	s.mu.Unlock()

	// 组合级风控（敞口 / 回撤）只限制开新倉
	riskAllowOpen, riskReason := s.checkComboRiskLimits(price)

	// 傳遞给所有子策略
	for i, strategy := range s.strategies {
		// 市况門控與组合风控只决定能否开新倉；不能开倉時仍要跑已有持倉的止盈止损（S4）
		if riskAllowOpen && s.shouldExecuteStrategy(i) {
			if err := strategy.OnPriceChange(price); err != nil {
				logger.Warn("⚠️ [%s] 子策略 %s 处理價格變化失败: %v",
					s.name, s.strategyNames[i], err)
			}
			continue
		}
		if err := s.runRiskOnly(strategy, price); err != nil {
			logger.Warn("⚠️ [%s] 子策略 %s 止盈止损检查失败 (门控原因: %s): %v",
				s.name, s.strategyNames[i], riskReason, err)
		}
	}

	return nil
}

// runRiskOnly 子策略被門控時只执行退出/風險邏輯，不允許回退到完整交易路徑。
func (s *ComboStrategy) runRiskOnly(strategy Strategy, price float64) error {
	if handler, ok := strategy.(riskOnlyPriceHandler); ok {
		return handler.OnPriceChangeRiskOnly(price)
	}
	if len(strategy.GetPositions()) > 0 {
		return fmt.Errorf("sub-strategy %s has positions but does not support risk-only price handling; full price dispatch is blocked", strategy.Name())
	}
	return nil
}

func (s *ComboStrategy) requiresRiskOnlyPriceHandling() bool {
	if s.strategyCfg == nil {
		return false
	}
	if s.strategyCfg.MarketDetection || s.strategyCfg.MaxExposure != 0 || s.strategyCfg.MaxDrawdown != 0 {
		return true
	}
	for _, child := range s.strategyCfg.Strategies {
		if len(child.PreferredMarket) > 0 {
			return true
		}
	}
	return false
}

// checkComboRiskLimits 组合级敞口与回撤限制：超限時只禁止开新倉，返回 (是否允许开倉, 原因)。
// MaxExposure：子策略持倉名义价值之和 / TotalCapital；MaxDrawdown：(權益高水位 - 當前權益) / 高水位 (%)，
// 當前權益 = TotalCapital + 子策略已實現盈亏 + 未實現盈亏。TotalCapital<=0 時两项均不生效。
func (s *ComboStrategy) checkComboRiskLimits(price float64) (bool, string) {
	if s.strategyCfg == nil {
		return true, ""
	}
	maxExposure := s.strategyCfg.MaxExposure
	maxDrawdown := s.strategyCfg.MaxDrawdown
	if !finiteNumber(maxExposure) || !finiteNumber(maxDrawdown) || maxExposure < 0 || maxDrawdown < 0 || maxDrawdown > comboPercentBase {
		return false, "组合风险配置无效"
	}
	if maxExposure == 0 && maxDrawdown == 0 {
		return true, ""
	}
	capital := s.strategyCfg.TotalCapital
	if !finiteNumber(capital) || capital <= 0 {
		return false, "组合风险资本基线无效"
	}
	if !finiteNumber(price) || price <= 0 {
		return false, "组合风险价格证据无效"
	}

	notional := 0.0
	unrealized := 0.0
	realized := 0.0
	for _, strategy := range s.strategies {
		for _, pos := range strategy.GetPositions() {
			if pos == nil {
				return false, "组合持仓证据包含空项目"
			}
			markPrice := pos.CurrentPrice
			if !finiteNumber(markPrice) || !finiteNumber(pos.Size) || !finiteNumber(pos.PnL) {
				return false, "组合持仓或未实现盈亏包含非有限数值"
			}
			if markPrice <= 0 {
				markPrice = price
			}
			notional += math.Abs(pos.Size) * markPrice
			unrealized += pos.PnL
			if !finiteNumber(notional) || !finiteNumber(unrealized) {
				return false, "组合敞口或未实现盈亏累计溢出"
			}
		}
		stats := strategy.GetStatistics()
		if maxDrawdown > 0 && stats == nil {
			return false, "组合回撤统计证据不可用"
		}
		if stats != nil {
			if !finiteNumber(stats.TotalPnL) {
				return false, "组合已实现盈亏包含非有限数值"
			}
			realized += stats.TotalPnL
			if !finiteNumber(realized) {
				return false, "组合已实现盈亏累计溢出"
			}
		}
	}

	if maxExposure > 0 {
		exposure := notional / capital
		if !finiteNumber(exposure) {
			return false, "组合敞口比例无效"
		}
		if exposure >= maxExposure {
			return false, fmt.Sprintf("敞口 %.2f 已达上限 %.2f", exposure, maxExposure)
		}
	}

	if maxDrawdown > 0 {
		equity := capital + realized + unrealized
		if !finiteNumber(equity) {
			return false, "组合权益计算无效"
		}
		s.mu.Lock()
		if equity > s.peakEquity {
			s.peakEquity = equity
			s.runtimeStateDirty = true
		}
		peak := s.peakEquity
		dirty := s.runtimeStateDirty
		s.mu.Unlock()
		if peak <= 0 || !finiteNumber(peak) {
			return false, "组合回撤高水位基线无效"
		}
		if dirty {
			if err := s.persistRuntimeState(); err != nil {
				s.reportRuntimeStateError(err)
				return false, "组合回撤高水位未能持久化"
			}
			s.reportRuntimeStateError(nil)
		}
		if peak > 0 {
			drawdown := (peak - equity) / peak * comboPercentBase
			if !finiteNumber(drawdown) {
				return false, "组合回撤计算无效"
			}
			if drawdown >= maxDrawdown {
				return false, fmt.Sprintf("回撤 %.2f%% 已达上限 %.2f%%", drawdown, maxDrawdown)
			}
		}
	}

	return true, ""
}

// warnUnsupportedRiskConfig 對已解析但未實現的配置給出啟动告警
func (s *ComboStrategy) warnUnsupportedRiskConfig() {
	if s.strategyCfg == nil {
		return
	}
	if s.strategyCfg.HedgeEnabled {
		logger.Warn("⚠️ [%s] hedge_enabled/hedge_ratio 暂未實現（不会自动開對沖倉），max_drawdown 仅用于回撤超限時停止开新倉", s.name)
	}
	if s.strategyCfg.TotalCapital <= 0 && (s.strategyCfg.MaxExposure > 0 || s.strategyCfg.MaxDrawdown > 0) {
		logger.Warn("⚠️ [%s] total_capital 未配置，max_exposure / max_drawdown 不生效", s.name)
	}
}

// updateCandle 更新 K線
func (s *ComboStrategy) updateCandle(price float64) {
	now := time.Now().Unix()

	if len(s.candles) == 0 {
		s.candles = append(s.candles, indicators.Candle{
			Time:   now,
			Open:   price,
			High:   price,
			Low:    price,
			Close:  price,
			Volume: 1,
		})
		return
	}

	last := &s.candles[len(s.candles)-1]
	if now-last.Time >= 60 {
		s.candles = append(s.candles, indicators.Candle{
			Time:   now,
			Open:   price,
			High:   price,
			Low:    price,
			Close:  price,
			Volume: 1,
		})
		if len(s.candles) > 200 {
			// 使用 copy 而不是切片截取，避免記憶體泄漏
			newCandles := make([]indicators.Candle, 200)
			copy(newCandles, s.candles[len(s.candles)-200:])
			s.candles = newCandles
		}
	} else {
		last.Close = price
		if price > last.High {
			last.High = price
		}
		if price < last.Low {
			last.Low = price
		}
		last.Volume++
	}
}

// shouldExecuteStrategy 判断是否应該執行策略
func (s *ComboStrategy) shouldExecuteStrategy(index int) bool {
	if index >= len(s.strategyCfg.Strategies) {
		return true
	}

	stratCfg := s.strategyCfg.Strategies[index]

	// 如果没有指定首选市况，總是執行
	if len(stratCfg.PreferredMarket) == 0 {
		return true
	}

	// 检查當前市况是否匹配
	s.mu.RLock()
	currentMarket := s.marketState
	s.mu.RUnlock()

	for _, preferred := range stratCfg.PreferredMarket {
		if preferred == currentMarket {
			return true
		}
	}

	return false
}

// marketDetectionLoop 市况检测循环
func (s *ComboStrategy) marketDetectionLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.detectMarketState()
		}
	}
}

// detectMarketState 检测市场状態
func (s *ComboStrategy) detectMarketState() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.priceHistory) < s.strategyCfg.TrendPeriod*2 {
		return
	}

	prices := s.priceHistory

	// 计算趋势
	shortPeriod := s.strategyCfg.TrendPeriod
	longPeriod := s.strategyCfg.TrendPeriod * 2

	shortMA := indicators.SMA(prices, shortPeriod)
	longMA := indicators.SMA(prices, longPeriod)

	if shortMA == nil || longMA == nil || len(shortMA) == 0 || len(longMA) == 0 {
		return
	}

	shortValue := shortMA[len(shortMA)-1]
	longValue := longMA[len(longMA)-1]

	// 计算波动率
	var volatility float64
	if len(s.candles) > s.strategyCfg.VolatilityPeriod {
		atr := indicators.NewATR(s.strategyCfg.VolatilityPeriod)
		atrValue := atr.CurrentATR(s.candles)
		if s.lastPrice > 0 {
			volatility = atrValue / s.lastPrice * 100
		}
	}

	// 判断市场状態
	previousState := s.marketState

	if volatility > s.strategyCfg.VolatilityThreshold {
		s.marketState = MarketVolatile
	} else if shortValue > longValue*1.02 { // 上涨趋势
		s.marketState = MarketBullish
	} else if shortValue < longValue*0.98 { // 下跌趋势
		s.marketState = MarketBearish
	} else {
		s.marketState = MarketSideways
	}

	if s.marketState != previousState {
		logger.Info("📊 [%s] 市场状態变化: %s -> %s (波动率: %.2f%%)",
			s.name, previousState, s.marketState, volatility)
	}
}

// rebalanceLoop 权重再平衡循环
func (s *ComboStrategy) rebalanceLoop() {
	interval := time.Duration(s.strategyCfg.RebalanceInterval) * time.Second
	if interval <= 0 {
		interval = time.Hour
		logger.Warn("⚠️ [%s] 组合策略再平衡间隔配置无效，使用默认值 %v", s.name, interval)
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.rebalanceWeights()
		}
	}
}

// rebalanceWeights 根據市况調整权重
func (s *ComboStrategy) rebalanceWeights() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.strategies) == 0 {
		return
	}

	// 根據當前市况調整权重
	for i, stratCfg := range s.strategyCfg.Strategies {
		if i >= len(s.weights) {
			break
		}

		baseWeight := stratCfg.Weight
		adjustedWeight := baseWeight

		// 检查策略是否适合當前市况
		isPreferred := false
		for _, preferred := range stratCfg.PreferredMarket {
			if preferred == s.marketState {
				isPreferred = true
				break
			}
		}

		if isPreferred {
			adjustedWeight = baseWeight * 1.5 // 增加权重
		} else if len(stratCfg.PreferredMarket) > 0 {
			adjustedWeight = baseWeight * 0.5 // 减少权重
		}

		// 限制权重範圍
		if adjustedWeight > 1.0 {
			adjustedWeight = 1.0
		}
		if adjustedWeight < 0.1 {
			adjustedWeight = 0.1
		}

		if s.weights[i] != adjustedWeight {
			logger.Info("⚖️ [%s] 調整策略 %s 权重: %.2f -> %.2f (市况: %s)",
				s.name, s.strategyNames[i], s.weights[i], adjustedWeight, s.marketState)
			s.weights[i] = adjustedWeight
		}
	}
}

// OnOrderUpdate 订單更新处理
func (s *ComboStrategy) OnOrderUpdate(update *position.OrderUpdate) error {
	// 傳遞给所有子策略
	var updateErrors []error
	for _, strategy := range s.strategies {
		updateCopy := *update
		if err := strategy.OnOrderUpdate(&updateCopy); err != nil {
			logger.Warn("⚠️ [%s] 子策略处理订單更新失败: %v", s.name, err)
			updateErrors = append(updateErrors, fmt.Errorf("combo sub-strategy %s order update: %w", strategy.Name(), err))
		}
	}
	return errors.Join(updateErrors...)
}

// GetPositions 獲取所有持倉
func (s *ComboStrategy) GetPositions() []*Position {
	s.mu.RLock()
	defer s.mu.RUnlock()

	positions := make([]*Position, 0)
	for _, strategy := range s.strategies {
		positions = append(positions, strategy.GetPositions()...)
	}
	return positions
}

// GetOrders 獲取所有订單
func (s *ComboStrategy) GetOrders() []*Order {
	s.mu.RLock()
	defer s.mu.RUnlock()

	orders := make([]*Order, 0)
	for _, strategy := range s.strategies {
		orders = append(orders, strategy.GetOrders()...)
	}
	return orders
}

// GetStatistics 獲取统计
func (s *ComboStrategy) GetStatistics() *StrategyStatistics {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// 彙總所有子策略统计
	totalStats := &StrategyStatistics{}
	for _, strategy := range s.strategies {
		subStats := strategy.GetStatistics()
		totalStats.TotalTrades += subStats.TotalTrades
		totalStats.TotalPnL += subStats.TotalPnL
		totalStats.TotalVolume += subStats.TotalVolume
	}

	if totalStats.TotalTrades > 0 {
		// 计算總胜率（加权平均）
		totalWins := 0.0
		for _, strategy := range s.strategies {
			subStats := strategy.GetStatistics()
			totalWins += subStats.WinRate * float64(subStats.TotalTrades)
		}
		totalStats.WinRate = totalWins / float64(totalStats.TotalTrades)
	}

	return totalStats
}

// GetMarketState 獲取當前市场状態
func (s *ComboStrategy) GetMarketState() MarketState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.marketState
}

// GetStrategyWeights 獲取策略权重
func (s *ComboStrategy) GetStrategyWeights() map[string]float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	weights := make(map[string]float64)
	for i, name := range s.strategyNames {
		if i < len(s.weights) {
			weights[name] = s.weights[i]
		}
	}
	return weights
}

// GetInfo 獲取组合策略信息
func (s *ComboStrategy) GetInfo() string {
	s.mu.RLock()
	marketState := s.marketState
	strategyCount := len(s.strategies)
	s.mu.RUnlock()

	// GetPositions 自己會加讀鎖，不能在持有讀鎖時調用（遞歸 RLock 遇到寫者等待會死鎖）
	return fmt.Sprintf("市场状態: %s, 子策略數: %d, 總持倉: %d",
		marketState, strategyCount, len(s.GetPositions()))
}

// GetVisualizationData 獲取策略可视化數據
func (s *ComboStrategy) GetVisualizationData() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// 组合策略的可视化数据包含所有子策略的可视化数据
	data := make(map[string]interface{})
	subStrategiesData := make(map[string]interface{})

	for i, strategy := range s.strategies {
		if i < len(s.strategyNames) {
			name := s.strategyNames[i]
			subStrategiesData[name] = strategy.GetVisualizationData()
		}
	}

	data["subStrategies"] = subStrategiesData
	data["marketState"] = string(s.marketState)
	data["strategyCount"] = len(s.strategies)

	return data
}
