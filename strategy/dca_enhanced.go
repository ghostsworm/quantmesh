package strategy

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"quantmesh/config"
	"quantmesh/event"
	"quantmesh/indicators"
	"quantmesh/logger"
	"quantmesh/position"
	"quantmesh/storage"
)

// DCAEnhancedStrategy 增强型 DCA (定投) 策略
// 特点：
// 1. ATR 动態间距管理：根據市場波動率自动調整買入间距
// 2. 三重止盈机制：首單止盈、尾單止盈、全倉止盈
// 3. 50层精细化倉位管理：多达50层的加倉控制
// 4. 防瀑布式下跌保护：在极端下跌時暂停加倉
type DCAEnhancedStrategy struct {
	name        string
	cfg         *config.Config
	executor    position.OrderExecutorInterface
	exchange    position.IExchange
	strategyCfg *DCAEnhancedConfig

	// 價格數據
	priceHistory []float64
	candles      []indicators.Candle
	lastPrice    float64
	mu           sync.RWMutex

	// 倉位管理
	layers        []*DCALayer // 分层倉位
	totalCost     float64     // 總成本
	totalQty      float64     // 總持倉量
	avgEntryPrice float64     // 平均入场價
	maxLayers     int         // 最大层數
	currentLayer  int         // 當前层數

	// ATR 动態间距
	atr             *indicators.ATR
	baseInterval    float64 // 基础间距
	dynamicInterval float64 // 动態间距

	// 止盈追踪
	highestProfit       float64 // 最高盈利点
	takeProfitTriggered bool    // 是否触发止盈追踪

	// 状態
	ctx               context.Context
	cancel            context.CancelFunc
	isRunning         bool
	isPaused          bool // 暂停加倉（瀑布下跌保护）
	pauseUntil        time.Time
	isClosing         bool
	closeOrderID      int64
	closeLayer        *DCALayer // 非 nil 表示當前平倉單只平該层（尾單止盈），nil 表示全倉平倉
	closeProgress     position.FillProgress
	closeRequestedQty float64
	closeLimitPrice   float64

	// 统计
	stats *StrategyStatistics

	// 事件總線
	eventBus EventBus

	// 交易存儲（用於保存交易記錄）
	tradeStorage      TradeStorage
	runtimeStateStore RuntimeStateStore
	runtimeStateErr   error
}

const dcaRuntimeStateSchemaVersion = 1

const dcaPendingCancelRetryInterval = 3 * time.Second

type dcaRuntimeState struct {
	BotID               string                `json:"bot_id"`
	StrategyName        string                `json:"strategy_name"`
	Symbol              string                `json:"symbol"`
	Layers              []*DCALayer           `json:"layers"`
	TotalCost           float64               `json:"total_cost"`
	TotalQty            float64               `json:"total_qty"`
	AvgEntryPrice       float64               `json:"avg_entry_price"`
	CurrentLayer        int                   `json:"current_layer"`
	DynamicInterval     float64               `json:"dynamic_interval"`
	HighestProfit       float64               `json:"highest_profit"`
	TakeProfitTriggered bool                  `json:"take_profit_triggered"`
	IsPaused            bool                  `json:"is_paused"`
	PauseUntil          time.Time             `json:"pause_until"`
	IsClosing           bool                  `json:"is_closing"`
	CloseOrderID        int64                 `json:"close_order_id"`
	CloseLayerIndex     int                   `json:"close_layer_index"`
	CloseProgress       position.FillProgress `json:"close_progress"`
	CloseRequestedQty   float64               `json:"close_requested_qty"`
	CloseLimitPrice     float64               `json:"close_limit_price"`
	Stats               StrategyStatistics    `json:"stats"`
}

// DCAEnhancedConfig 增强型 DCA 配置
type DCAEnhancedConfig struct {
	// 基础配置
	Symbol            string  `yaml:"symbol"`
	BaseOrderAmount   float64 `yaml:"base_order_amount"`   // 基础订單金額 (USDT)
	SafetyOrderAmount float64 `yaml:"safety_order_amount"` // 安全订單金額 (USDT)
	MaxSafetyOrders   int     `yaml:"max_safety_orders"`   // 最大安全订單數 (最多50层)

	// ATR 动態间距
	ATRPeriod     int     `yaml:"atr_period"`     // ATR 周期
	ATRMultiplier float64 `yaml:"atr_multiplier"` // ATR 乘數
	MinPriceStep  float64 `yaml:"min_price_step"` // 最小價格间距 (%)
	MaxPriceStep  float64 `yaml:"max_price_step"` // 最大價格间距 (%)

	// 倉位遞增
	SafetyOrderScale float64 `yaml:"safety_order_scale"` // 安全订單遞增倍數 (1.0-2.0)
	SafetyOrderStep  float64 `yaml:"safety_order_step"`  // 安全订單间距遞增 (1.0-2.0)

	// 三重止盈
	FirstOrderTakeProfit float64 `yaml:"first_order_take_profit"` // 首單止盈比例 (%)
	LastOrderTakeProfit  float64 `yaml:"last_order_take_profit"`  // 尾單止盈比例 (%)
	TotalTakeProfit      float64 `yaml:"total_take_profit"`       // 全倉止盈比例 (%)
	TrailingTakeProfit   float64 `yaml:"trailing_take_profit"`    // 追踪止盈回撤比例 (%)
	TrailingActivation   float64 `yaml:"trailing_activation"`     // 追踪止盈激活阈值 (%)

	// 止损
	StopLoss         float64 `yaml:"stop_loss"`          // 止损比例 (%)
	TrailingStopLoss float64 `yaml:"trailing_stop_loss"` // 追踪止损比例 (%)

	// 防瀑布保护
	CascadeProtection    bool    `yaml:"cascade_protection"`     // 啟用瀑布保护
	CascadeDropThreshold float64 `yaml:"cascade_drop_threshold"` // 瀑布下跌阈值 (%)
	CascadePauseDuration int     `yaml:"cascade_pause_duration"` // 暂停時长 (秒)

	// 趨勢過濾
	TrendFilterEnabled bool   `yaml:"trend_filter_enabled"` // 啟用趨勢過濾
	TrendMethod        string `yaml:"trend_method"`         // 趋势判断方法 (ma/ema/macd)
	TrendPeriod        int    `yaml:"trend_period"`         // 趋势周期
}

// DCALayer 分层倉位
type DCALayer struct {
	Index             int       // 层级索引
	Price             float64   // 入场價格
	Quantity          float64   // 持倉數量
	Cost              float64   // 成本
	OrderID           int64     // 订單ID
	Status            string    // 状態: pending/filled/closed
	FilledAt          time.Time // 成交時间
	OpeningFee        float64   // 剩餘持倉應分攤的實際開倉手續費（計價幣）
	EntryBaseFeeQty   float64   // 累計以基礎幣扣收的開倉手續費數量；成交游標口徑，非剩餘庫存
	FillProgress      position.FillProgress
	RequestedQuantity float64
	CancelRequestedAt time.Time `json:"-"`
}

// NewDCAEnhancedStrategy 創建增强型 DCA 策略
func NewDCAEnhancedStrategy(
	name string,
	symbol string,
	cfg *config.Config,
	executor position.OrderExecutorInterface,
	exchange position.IExchange,
	strategyCfg map[string]interface{},
) *DCAEnhancedStrategy {
	ctx, cancel := context.WithCancel(context.Background())

	dcaCfg := parseDCAConfig(strategyCfg)
	if symbol != "" {
		dcaCfg.Symbol = symbol
	}

	strategy := &DCAEnhancedStrategy{
		name:         name,
		cfg:          cfg,
		executor:     executor,
		exchange:     exchange,
		strategyCfg:  dcaCfg,
		priceHistory: make([]float64, 0, 200),
		candles:      make([]indicators.Candle, 0, 200),
		layers:       make([]*DCALayer, 0, dcaCfg.MaxSafetyOrders+1),
		maxLayers:    dcaCfg.MaxSafetyOrders + 1,
		atr:          indicators.NewATR(dcaCfg.ATRPeriod),
		baseInterval: dcaCfg.MinPriceStep,
		ctx:          ctx,
		cancel:       cancel,
		stats: &StrategyStatistics{
			TotalTrades: 0,
			WinRate:     0,
			TotalPnL:    0,
			TotalVolume: 0,
		},
	}

	return strategy
}

// parseDCAConfig 解析 DCA 配置
func parseDCAConfig(cfg map[string]interface{}) *DCAEnhancedConfig {
	dcaCfg := &DCAEnhancedConfig{
		// 默认值
		Symbol:               "BTCUSDT",
		BaseOrderAmount:      100,
		SafetyOrderAmount:    200,
		MaxSafetyOrders:      50,
		ATRPeriod:            14,
		ATRMultiplier:        1.5,
		MinPriceStep:         1.0,
		MaxPriceStep:         5.0,
		SafetyOrderScale:     1.05,
		SafetyOrderStep:      1.0,
		FirstOrderTakeProfit: 1.0,
		LastOrderTakeProfit:  0.5,
		TotalTakeProfit:      2.0,
		TrailingTakeProfit:   0.5,
		TrailingActivation:   1.0,
		StopLoss:             10.0,
		TrailingStopLoss:     2.0,
		CascadeProtection:    true,
		CascadeDropThreshold: 5.0,
		CascadePauseDuration: 300,
		TrendFilterEnabled:   true,
		TrendMethod:          "ema",
		TrendPeriod:          20,
	}

	if cfg == nil {
		return dcaCfg
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

	// 從 map 中读取配置
	if v, ok := cfg["symbol"].(string); ok {
		dcaCfg.Symbol = v
	}

	dcaCfg.BaseOrderAmount = getFloat("base_order_amount", dcaCfg.BaseOrderAmount)
	dcaCfg.SafetyOrderAmount = getFloat("safety_order_amount", dcaCfg.SafetyOrderAmount)
	dcaCfg.MaxSafetyOrders = getInt("max_safety_orders", dcaCfg.MaxSafetyOrders)
	dcaCfg.ATRPeriod = getInt("atr_period", dcaCfg.ATRPeriod)
	dcaCfg.ATRMultiplier = getFloat("atr_multiplier", dcaCfg.ATRMultiplier)
	dcaCfg.MinPriceStep = getFloat("min_price_step", dcaCfg.MinPriceStep)
	dcaCfg.MaxPriceStep = getFloat("max_price_step", dcaCfg.MaxPriceStep)
	dcaCfg.SafetyOrderScale = getFloat("safety_order_scale", dcaCfg.SafetyOrderScale)
	dcaCfg.SafetyOrderStep = getFloat("safety_order_step", dcaCfg.SafetyOrderStep)
	dcaCfg.FirstOrderTakeProfit = getFloat("first_order_take_profit", dcaCfg.FirstOrderTakeProfit)
	dcaCfg.LastOrderTakeProfit = getFloat("last_order_take_profit", dcaCfg.LastOrderTakeProfit)
	dcaCfg.TotalTakeProfit = getFloat("total_take_profit", dcaCfg.TotalTakeProfit)
	dcaCfg.TrailingTakeProfit = getFloat("trailing_take_profit", dcaCfg.TrailingTakeProfit)
	dcaCfg.TrailingActivation = getFloat("trailing_activation", dcaCfg.TrailingActivation)
	dcaCfg.StopLoss = getFloat("stop_loss", dcaCfg.StopLoss)
	dcaCfg.TrailingStopLoss = getFloat("trailing_stop_loss", dcaCfg.TrailingStopLoss)

	if v, ok := cfg["cascade_protection"].(bool); ok {
		dcaCfg.CascadeProtection = v
	}
	dcaCfg.CascadeDropThreshold = getFloat("cascade_drop_threshold", dcaCfg.CascadeDropThreshold)
	dcaCfg.CascadePauseDuration = getInt("cascade_pause_duration", dcaCfg.CascadePauseDuration)

	if v, ok := cfg["trend_filter_enabled"].(bool); ok {
		dcaCfg.TrendFilterEnabled = v
	}
	if v, ok := cfg["trend_method"].(string); ok {
		dcaCfg.TrendMethod = v
	}
	dcaCfg.TrendPeriod = getInt("trend_period", dcaCfg.TrendPeriod)

	return dcaCfg
}

// Name 返回策略名称
func (s *DCAEnhancedStrategy) Name() string {
	return s.name
}

// logPrefix 返回日誌前綴，含 bot ID 便於區分同交易所同幣多實例
func (s *DCAEnhancedStrategy) logPrefix() string {
	if s.cfg != nil && s.cfg.Trading.BotID != "" {
		return s.cfg.Trading.BotID + " [" + s.name + "]"
	}
	return s.name
}

// Initialize 初始化策略
func (s *DCAEnhancedStrategy) Initialize(cfg *config.Config, executor position.OrderExecutorInterface, exchange position.IExchange) error {
	s.cfg = cfg
	s.executor = executor
	s.exchange = exchange
	return nil
}

// SetEventBus 設置事件總線
func (s *DCAEnhancedStrategy) SetEventBus(bus EventBus) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.eventBus = bus
}

// TradeStorage 交易存儲介面（避免循環匯入）
type TradeStorage interface {
	SaveTrade(buyOrderID, sellOrderID int64, exchange, symbol string, buyPrice, sellPrice, quantity, pnl, fee float64, feeAsset string, createdAt time.Time, botID string) error
}

// SetTradeStorage 設置交易存儲介面（用於保存交易記錄）
func (s *DCAEnhancedStrategy) SetTradeStorage(storage TradeStorage) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tradeStorage = storage
}

// SetRuntimeStateStore wires durable recovery before Start is called.
func (s *DCAEnhancedStrategy) SetRuntimeStateStore(store RuntimeStateStore) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runtimeStateStore = store
}

func (s *DCAEnhancedStrategy) runtimeStateSnapshotLocked() dcaRuntimeState {
	state := dcaRuntimeState{
		BotID: s.effectiveBotID(), StrategyName: s.name, Symbol: s.strategyCfg.Symbol,
		TotalCost: s.totalCost, TotalQty: s.totalQty, AvgEntryPrice: s.avgEntryPrice,
		CurrentLayer: s.currentLayer, DynamicInterval: s.dynamicInterval,
		HighestProfit: s.highestProfit, TakeProfitTriggered: s.takeProfitTriggered,
		IsPaused: s.isPaused, PauseUntil: s.pauseUntil, IsClosing: s.isClosing,
		CloseOrderID: s.closeOrderID, CloseLayerIndex: -1, CloseProgress: s.closeProgress,
		CloseRequestedQty: s.closeRequestedQty, CloseLimitPrice: s.closeLimitPrice,
	}
	if s.stats != nil {
		state.Stats = *s.stats
	}
	for _, layer := range s.layers {
		if layer != nil {
			copyLayer := *layer
			state.Layers = append(state.Layers, &copyLayer)
		}
	}
	if s.closeLayer != nil {
		state.CloseLayerIndex = s.closeLayer.Index
	}
	return state
}

func (s *DCAEnhancedStrategy) persistRuntimeStateLocked() error {
	if s.runtimeStateStore == nil {
		s.runtimeStateErr = fmt.Errorf("DCA runtime state store is unavailable")
		return s.runtimeStateErr
	}
	payload, err := json.Marshal(s.runtimeStateSnapshotLocked())
	if err != nil {
		s.runtimeStateErr = fmt.Errorf("encode DCA runtime state: %w", err)
		return s.runtimeStateErr
	}
	if err := s.runtimeStateStore.SaveRuntimeState(s.name, dcaRuntimeStateSchemaVersion, string(payload)); err != nil {
		s.runtimeStateErr = fmt.Errorf("persist DCA runtime state: %w", err)
		return s.runtimeStateErr
	}
	s.runtimeStateErr = nil
	return nil
}

func (s *DCAEnhancedStrategy) restoreRuntimeState() error {
	if s.runtimeStateStore == nil {
		return nil
	}
	version, payload, found, err := s.runtimeStateStore.LoadRuntimeState(s.name)
	if err != nil {
		return fmt.Errorf("load DCA runtime state: %w", err)
	}
	if !found {
		return nil
	}
	if version != dcaRuntimeStateSchemaVersion {
		return fmt.Errorf("unsupported DCA runtime state schema version %d", version)
	}
	var state dcaRuntimeState
	if err := json.Unmarshal([]byte(payload), &state); err != nil {
		return fmt.Errorf("decode DCA runtime state: %w", err)
	}
	if state.BotID != s.effectiveBotID() || state.StrategyName != s.name || state.Symbol != s.strategyCfg.Symbol {
		return fmt.Errorf("DCA runtime state identity mismatch")
	}
	if state.TotalCost < 0 || state.TotalQty < 0 || state.CurrentLayer < 0 || state.CurrentLayer > s.maxLayers ||
		state.CloseOrderID < 0 || state.CloseLayerIndex < -1 || state.CloseRequestedQty < 0 || state.CloseLimitPrice < 0 ||
		state.CloseProgress.Quantity < 0 || state.CloseProgress.Notional < 0 ||
		!finiteNumber(state.TotalCost) || !finiteNumber(state.TotalQty) || !finiteNumber(state.AvgEntryPrice) ||
		!finiteNumber(state.DynamicInterval) || !finiteNumber(state.HighestProfit) ||
		!finiteNumber(state.CloseRequestedQty) || !finiteNumber(state.CloseLimitPrice) ||
		!finiteNumber(state.CloseProgress.Quantity) || !finiteNumber(state.CloseProgress.Notional) ||
		!finiteNumber(state.Stats.TotalPnL) || !finiteNumber(state.Stats.TotalVolume) || !finiteNumber(state.Stats.WinRate) || len(state.Layers) > s.maxLayers {
		return fmt.Errorf("DCA runtime state contains invalid inventory")
	}
	var totalQty, totalCost float64
	closeLayerFound := state.CloseLayerIndex < 0
	layerIndexes := make(map[int]struct{}, len(state.Layers))
	activeOrderIDs := make(map[int64]struct{}, len(state.Layers)+1)
	var closeTargetLayer *DCALayer
	for _, layer := range state.Layers {
		if layer == nil || layer.Index < 0 || layer.OrderID < 0 || layer.Quantity < 0 || layer.Cost < 0 || layer.OpeningFee < 0 || layer.EntryBaseFeeQty < 0 || layer.RequestedQuantity < 0 ||
			layer.FillProgress.Quantity < 0 || layer.FillProgress.Notional < 0 ||
			!finiteNumber(layer.Price) || !finiteNumber(layer.Quantity) || !finiteNumber(layer.Cost) || !finiteNumber(layer.OpeningFee) || !finiteNumber(layer.EntryBaseFeeQty) ||
			!finiteNumber(layer.RequestedQuantity) || !finiteNumber(layer.FillProgress.Quantity) || !finiteNumber(layer.FillProgress.Notional) ||
			layer.FillProgress.Quantity > layer.RequestedQuantity+entryQtyEpsilon || layer.EntryBaseFeeQty > layer.FillProgress.Quantity+entryQtyEpsilon {
			return fmt.Errorf("DCA runtime state contains invalid layer")
		}
		if _, duplicate := layerIndexes[layer.Index]; duplicate {
			return fmt.Errorf("DCA runtime state contains duplicate layer index %d", layer.Index)
		}
		layerIndexes[layer.Index] = struct{}{}
		switch layer.Status {
		case entryStatusPending:
			if layer.Quantity != 0 || layer.Cost != 0 || layer.OpeningFee != 0 || layer.FillProgress.Quantity != 0 {
				return fmt.Errorf("DCA pending layer %d contains attributed fills", layer.Index)
			}
			if layer.OrderID <= 0 {
				return fmt.Errorf("DCA pending layer %d is missing its order identity", layer.Index)
			}
		case entryStatusPartiallyFilled, entryStatusFilled:
			if layer.Quantity <= 0 || layer.Cost <= 0 || layer.FillProgress.Quantity+entryQtyEpsilon < layer.Quantity {
				return fmt.Errorf("DCA filled layer %d has inconsistent inventory", layer.Index)
			}
			if layer.Status == entryStatusPartiallyFilled && layer.OrderID <= 0 {
				return fmt.Errorf("DCA partially filled layer %d is missing its order identity", layer.Index)
			}
		default:
			return fmt.Errorf("DCA runtime state contains unknown layer status %q", layer.Status)
		}
		if layer.Status == entryStatusPending || layer.Status == entryStatusPartiallyFilled {
			if _, duplicate := activeOrderIDs[layer.OrderID]; duplicate {
				return fmt.Errorf("DCA runtime state contains duplicate active order ID %d", layer.OrderID)
			}
			activeOrderIDs[layer.OrderID] = struct{}{}
		}
		if state.CloseLayerIndex == layer.Index {
			closeLayerFound = true
			closeTargetLayer = layer
		}
		if entryHasFill(layer.Status) {
			totalQty += layer.Quantity
			totalCost += layer.Cost
		}
	}
	if !closeLayerFound {
		return fmt.Errorf("DCA close state references a missing layer")
	}
	if math.Abs(totalQty-state.TotalQty) > entryQtyEpsilon || math.Abs(totalCost-state.TotalCost) > math.Max(1e-8, math.Abs(state.TotalCost)*1e-8) {
		return fmt.Errorf("DCA runtime state inventory totals do not reconcile")
	}
	if state.TotalQty == 0 && state.AvgEntryPrice != 0 || state.TotalQty > 0 &&
		(state.AvgEntryPrice <= 0 || math.Abs(state.AvgEntryPrice-state.TotalCost/state.TotalQty) > math.Max(1e-8, math.Abs(state.AvgEntryPrice)*1e-8)) {
		return fmt.Errorf("DCA runtime state average entry price does not match inventory cost")
	}
	if state.IsClosing {
		if state.CloseOrderID <= 0 || state.CloseRequestedQty <= 0 || state.CloseProgress.Quantity > state.CloseRequestedQty+entryQtyEpsilon ||
			state.CloseLayerIndex >= 0 && state.CloseRequestedQty > closeTargetLayer.Quantity+entryQtyEpsilon {
			return fmt.Errorf("DCA close state contains inconsistent execution progress")
		}
		if _, duplicate := activeOrderIDs[state.CloseOrderID]; duplicate {
			return fmt.Errorf("DCA close order ID conflicts with an active entry order")
		}
	} else if state.CloseOrderID != 0 || state.CloseProgress.Quantity != 0 || state.CloseProgress.Notional != 0 || state.CloseRequestedQty != 0 || state.CloseLimitPrice != 0 || state.CloseLayerIndex >= 0 {
		return fmt.Errorf("DCA runtime state has close progress without an active close order")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.layers = state.Layers
	s.totalCost, s.totalQty, s.avgEntryPrice = state.TotalCost, state.TotalQty, state.AvgEntryPrice
	s.currentLayer, s.dynamicInterval = state.CurrentLayer, state.DynamicInterval
	s.highestProfit, s.takeProfitTriggered = state.HighestProfit, state.TakeProfitTriggered
	s.isPaused, s.pauseUntil, s.isClosing = state.IsPaused, state.PauseUntil, state.IsClosing
	s.closeOrderID, s.closeProgress = state.CloseOrderID, state.CloseProgress
	s.closeRequestedQty, s.closeLimitPrice = state.CloseRequestedQty, state.CloseLimitPrice
	s.closeLayer = nil
	if state.IsClosing && state.CloseLayerIndex >= 0 {
		for _, layer := range s.layers {
			if layer.Index == state.CloseLayerIndex {
				s.closeLayer = layer
				break
			}
		}
	}
	if state.IsClosing && state.CloseOrderID <= 0 {
		return fmt.Errorf("DCA close state is missing its order identity")
	}
	s.stats = &state.Stats
	return nil
}

// effectiveBotID 與 SymbolRuntime / SuperPositionManager 一致，供寫入 trades.bot_id
func (s *DCAEnhancedStrategy) effectiveBotID() string {
	if s.cfg == nil {
		return ""
	}
	bid := strings.TrimSpace(s.cfg.Trading.BotID)
	if bid != "" {
		return bid
	}
	ex := "binance"
	if s.exchange != nil && strings.TrimSpace(s.exchange.GetName()) != "" {
		ex = strings.ToLower(strings.TrimSpace(s.exchange.GetName()))
	}
	sym := strings.TrimSpace(s.strategyCfg.Symbol)
	if sym == "" {
		sym = strings.TrimSpace(s.cfg.Trading.Symbol)
	}
	mt := strings.ToLower(strings.TrimSpace(s.cfg.Trading.MarketType))
	if mt == "" {
		mt = "futures"
	}
	return config.GenerateBotID(ex, sym, mt)
}

// Start 啟动策略
func (s *DCAEnhancedStrategy) Start(ctx context.Context) error {
	if s.runtimeStateStore == nil {
		return fmt.Errorf("DCA runtime state store is required")
	}
	if err := s.restoreRuntimeState(); err != nil {
		return err
	}
	if err := s.reconcilePersistedOrders(ctx); err != nil {
		return fmt.Errorf("DCA persisted order recovery failed; strategy remains stopped: %w", err)
	}
	s.mu.Lock()
	s.ctx = ctx
	s.isRunning = true
	s.mu.Unlock()

	logger.Info("✅ [%s] 增强型 DCA 策略已啟动", s.name)
	logger.Info("📊 配置: 最大层數=%d, 基础订單=%.2f, ATR周期=%d",
		s.strategyCfg.MaxSafetyOrders+1,
		s.strategyCfg.BaseOrderAmount,
		s.strategyCfg.ATRPeriod)

	return nil
}

// roundPrice 根據交易所精度格式化價格
func (s *DCAEnhancedStrategy) roundPrice(price float64) float64 {
	decimals := s.exchange.GetPriceDecimals()
	multiplier := math.Pow(10, float64(decimals))
	return math.Round(price*multiplier) / multiplier
}

// Stop 停止策略
func (s *DCAEnhancedStrategy) Stop() error {
	s.mu.Lock()
	s.isRunning = false
	s.mu.Unlock()

	if s.cancel != nil {
		s.cancel()
	}

	logger.Info("⏹️ [%s] 增强型 DCA 策略已停止", s.name)
	return nil
}

// IsRunning 回傳策略是否已成功啟动
func (s *DCAEnhancedStrategy) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.isRunning
}

// OnPriceChange 價格變化处理
func (s *DCAEnhancedStrategy) OnPriceChange(price float64) error {
	return s.onPrice(price, true)
}

// OnPriceChangeRiskOnly 只更新行情並執行止盈止损，不开倉/加倉（组合策略門控時使用）
func (s *DCAEnhancedStrategy) OnPriceChangeRiskOnly(price float64) error {
	return s.onPrice(price, false)
}

// onPrice 價格處理主流程；allowOpening=false 時跳过开倉/加倉
func (s *DCAEnhancedStrategy) onPrice(price float64, allowOpening bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.isRunning || s.isClosing {
		return nil
	}
	if s.runtimeStateErr != nil {
		return fmt.Errorf("DCA runtime state is not durable; new decisions are paused: %w", s.runtimeStateErr)
	}

	// 检查瀑布保护（S1：暂停只影响开倉/加倉，止盈止损照常执行）
	openingPaused := false
	if s.isPaused {
		if s.pauseUntil.IsZero() || time.Now().Before(s.pauseUntil) {
			openingPaused = true
		} else {
			s.isPaused = false
			s.pauseUntil = time.Time{}
		}
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

	// 更新 K線數據（简化处理，實際应該從 K線流獲取）
	s.updateCandle(price)

	// 计算动態间距
	s.calculateDynamicInterval()

	// 检查瀑布下跌
	if !openingPaused && s.strategyCfg.CascadeProtection && s.detectCascadeDrop() {
		s.isPaused = true
		s.pauseUntil = time.Now().Add(time.Duration(s.strategyCfg.CascadePauseDuration) * time.Second)
		logger.Warn("⚠️ [%s] 检测到瀑布式下跌，暂停加倉 %d 秒（止盈止损仍生效）", s.name, s.strategyCfg.CascadePauseDuration)
		openingPaused = true
	}

	// 检查止盈止损
	if err := s.checkTakeProfitStopLoss(price); err != nil {
		return err
	}

	// 已下平倉單、暂停中、被門控、或仍有未成交的开倉單時，不再下新的开倉/加倉單
	if s.isClosing || openingPaused || !allowOpening || s.hasPendingLayer() {
		return nil
	}

	// 检查是否需要开倉或加倉
	if len(s.layers) == 0 {
		// 首次开倉
		return s.openBaseOrder(price)
	}

	// 检查是否需要加倉
	return s.checkSafetyOrder(price)
}

// updateCandle 更新 K線數據
func (s *DCAEnhancedStrategy) updateCandle(price float64) {
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
	// 简化处理：每分钟一根 K線
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

// calculateDynamicInterval 计算动態间距
func (s *DCAEnhancedStrategy) calculateDynamicInterval() {
	if len(s.candles) < s.strategyCfg.ATRPeriod+1 {
		s.dynamicInterval = s.strategyCfg.MinPriceStep
		return
	}

	// 使用 ATR 计算动態间距
	atrValue := s.atr.CurrentATR(s.candles)
	if atrValue == 0 || s.lastPrice == 0 {
		s.dynamicInterval = s.strategyCfg.MinPriceStep
		return
	}

	// 动態间距 = ATR / 當前價格 * 100 * 乘數
	dynamicStep := (atrValue / s.lastPrice) * 100 * s.strategyCfg.ATRMultiplier

	// 限制在最小和最大间距之间
	s.dynamicInterval = math.Max(s.strategyCfg.MinPriceStep, math.Min(dynamicStep, s.strategyCfg.MaxPriceStep))
}

// detectCascadeDrop 检测瀑布式下跌
func (s *DCAEnhancedStrategy) detectCascadeDrop() bool {
	if len(s.priceHistory) < 10 {
		return false
	}

	// 计算最近10個價格的最大跌幅
	recent := s.priceHistory[len(s.priceHistory)-10:]
	maxPrice := recent[0]
	for _, p := range recent {
		if p > maxPrice {
			maxPrice = p
		}
	}

	currentDrop := (maxPrice - s.lastPrice) / maxPrice * 100
	return currentDrop >= s.strategyCfg.CascadeDropThreshold
}

// openBaseOrder 开啟基础订單
func (s *DCAEnhancedStrategy) openBaseOrder(price float64) error {
	// 检查趨勢過濾
	if s.strategyCfg.TrendFilterEnabled && !s.isTrendUp() {
		logger.Info("📊 [%s] 趋势向下，暂不开倉", s.logPrefix())
		return nil
	}

	// 格式化價格
	orderPrice := s.roundPrice(price)
	quantity := s.strategyCfg.BaseOrderAmount / orderPrice

	// 🔥 精度处理：根據交易所要求的精度截断數量
	qDec := s.exchange.GetQuantityDecimals()
	quantity = math.Floor(quantity*math.Pow(10, float64(qDec))) / math.Pow(10, float64(qDec))

	if quantity <= 0 {
		minQty := math.Pow10(-qDec)
		logger.Error("🚨 [%s] 基础订單數量過小 (%.8f)，低於交易所最小精度 (%.8f)，策略已自动暂停！请在配置中調大 BaseOrderAmount", s.name, quantity, minQty)
		s.isPaused = true

		// 发布事件
		if s.eventBus != nil {
			s.eventBus.Publish(&event.Event{
				Type:      event.EventTypePrecisionAdjustment,
				Timestamp: time.Now(),
				Data: map[string]interface{}{
					"symbol":         s.strategyCfg.Symbol,
					"strategy":       s.name,
					"order_amount":   s.strategyCfg.BaseOrderAmount,
					"calculated_qty": quantity,
					"min_qty":        minQty,
					"price":          orderPrice,
					"action":         "pause",
					"reason":         "基础订單數量低於交易所最小精度",
				},
			})
		}
		return nil
	}

	layer := &DCALayer{
		Index:    0,
		Price:    orderPrice,
		Quantity: quantity,
		Cost:     s.strategyCfg.BaseOrderAmount,
		Status:   entryStatusPending,
	}

	// 下單
	order, err := s.executor.PlaceOrder(&position.OrderRequest{
		Symbol:       s.strategyCfg.Symbol,
		Side:         "BUY",
		Quantity:     quantity,
		Price:        orderPrice,
		PostOnly:     true,
		PositionSide: position.PositionSideLong,
	})

	if err != nil {
		logger.Error("❌ [%s] 基础订單下單失败: %v", s.logPrefix(), err)
		return err
	}
	if order == nil {
		logger.Debug("🔒 [%s] 基础订單被执行器跳过，等待下一轮", s.logPrefix())
		return nil
	}

	// S3：限價單下單成功≠成交，保持 pending，等成交回報再計入持倉
	layer.OrderID = order.OrderID
	s.layers = append(s.layers, layer)
	s.currentLayer = 1
	if err := s.persistRuntimeStateLocked(); err != nil {
		s.requireDCAOrderReconciliation(&position.OrderUpdate{OrderID: order.OrderID}, "DCA order accepted but runtime state persistence failed")
		return err
	}

	logger.Info("📈 [%s:%s] [%s] 基础订單已挂單: 價格=%.2f, 數量=%.6f, 成本=%.2f",
		s.exchange.GetName(), s.strategyCfg.Symbol, s.name, price, quantity, s.strategyCfg.BaseOrderAmount)

	return nil
}

// checkSafetyOrder 检查是否需要下安全订單
func (s *DCAEnhancedStrategy) checkSafetyOrder(price float64) error {
	if s.currentLayer >= s.maxLayers {
		return nil // 已达最大层數
	}

	// 计算需要的下跌幅度
	lastLayer := s.layers[len(s.layers)-1]
	requiredDrop := s.getRequiredDrop(s.currentLayer)

	dropPercent := (lastLayer.Price - price) / lastLayer.Price * 100

	if dropPercent < requiredDrop {
		return nil // 未达到加倉条件
	}

	// 计算安全订單金額（遞增）
	orderAmount := s.strategyCfg.SafetyOrderAmount * math.Pow(s.strategyCfg.SafetyOrderScale, float64(s.currentLayer-1))
	orderPrice := s.roundPrice(price)
	quantity := orderAmount / orderPrice

	// 🔥 精度处理：根據交易所要求的精度截断數量
	qDec := s.exchange.GetQuantityDecimals()
	quantity = math.Floor(quantity*math.Pow(10, float64(qDec))) / math.Pow(10, float64(qDec))

	if quantity <= 0 {
		minQty := math.Pow10(-qDec)
		logger.Error("🚨 [%s] 安全订單 #%d 數量過小 (%.8f)，低於交易所最小精度 (%.8f)，策略已自动暂停！", s.name, s.currentLayer, quantity, minQty)
		s.isPaused = true

		// 发布事件
		if s.eventBus != nil {
			s.eventBus.Publish(&event.Event{
				Type:      event.EventTypePrecisionAdjustment,
				Timestamp: time.Now(),
				Data: map[string]interface{}{
					"symbol":         s.strategyCfg.Symbol,
					"strategy":       s.name,
					"layer":          s.currentLayer,
					"order_amount":   orderAmount,
					"calculated_qty": quantity,
					"min_qty":        minQty,
					"price":          orderPrice,
					"action":         "pause",
					"reason":         "安全订單數量低於交易所最小精度",
				},
			})
		}
		return nil
	}

	layer := &DCALayer{
		Index:    s.currentLayer,
		Price:    orderPrice,
		Quantity: quantity,
		Cost:     orderAmount,
		Status:   entryStatusPending,
	}

	// 下單
	order, err := s.executor.PlaceOrder(&position.OrderRequest{
		Symbol:       s.strategyCfg.Symbol,
		Side:         "BUY",
		Quantity:     quantity,
		Price:        orderPrice,
		PostOnly:     true,
		PositionSide: position.PositionSideLong,
	})

	if err != nil {
		logger.Error("❌ [%s] 安全订單 #%d 下單失败: %v", s.name, s.currentLayer, err)
		return err
	}
	if order == nil {
		logger.Debug("🔒 [%s] 安全订單 #%d 被执行器跳过，等待下一轮", s.name, s.currentLayer)
		return nil
	}

	// S3：保持 pending，等成交回報再計入持倉
	layer.OrderID = order.OrderID
	s.layers = append(s.layers, layer)
	s.currentLayer++
	if err := s.persistRuntimeStateLocked(); err != nil {
		s.requireDCAOrderReconciliation(&position.OrderUpdate{OrderID: order.OrderID}, "DCA safety order accepted but runtime state persistence failed")
		return err
	}

	logger.Info("📉 [%s:%s] [%s] 安全订單 #%d 已挂單: 價格=%.2f, 數量=%.6f, 成本=%.2f, 平均成本=%.2f",
		s.exchange.GetName(), s.strategyCfg.Symbol, s.name, layer.Index, price, quantity, orderAmount, s.avgEntryPrice)

	return nil
}

// getRequiredDrop 獲取需要的下跌幅度（考虑 ATR 動態調整和遞增）
func (s *DCAEnhancedStrategy) getRequiredDrop(layerIndex int) float64 {
	// 基础间距使用动態 ATR 间距
	baseStep := s.dynamicInterval

	// 应用遞增系數
	requiredDrop := baseStep * math.Pow(s.strategyCfg.SafetyOrderStep, float64(layerIndex-1))

	return requiredDrop
}

// updateTotals 更新總计數據
func (s *DCAEnhancedStrategy) updateTotals() {
	s.totalCost = 0
	s.totalQty = 0

	for _, layer := range s.layers {
		if entryHasFill(layer.Status) {
			s.totalCost += layer.Cost
			s.totalQty += layer.Quantity
		}
	}

	if s.totalQty > 0 {
		s.avgEntryPrice = s.totalCost / s.totalQty
	} else {
		s.avgEntryPrice = 0
	}
}

// filledLayers 返回已有成交的层（按下單顺序）
func (s *DCAEnhancedStrategy) filledLayers() []*DCALayer {
	filled := make([]*DCALayer, 0, len(s.layers))
	for _, layer := range s.layers {
		if entryHasFill(layer.Status) && layer.Quantity > 0 {
			filled = append(filled, layer)
		}
	}
	return filled
}

// hasPendingLayer 是否存在未完全成交的开倉單
func (s *DCAEnhancedStrategy) hasPendingLayer() bool {
	for _, layer := range s.layers {
		if layer.Status == entryStatusPending || layer.Status == entryStatusPartiallyFilled {
			return true
		}
	}
	return false
}

// checkTakeProfitStopLoss 检查止盈止损
func (s *DCAEnhancedStrategy) checkTakeProfitStopLoss(price float64) error {
	filled := s.filledLayers()
	if len(filled) == 0 || s.totalQty == 0 || s.totalCost <= 0 {
		return nil
	}

	// 计算當前盈亏
	currentValue := s.totalQty * price
	pnl := currentValue - s.totalCost
	pnlPercent := pnl / s.totalCost * 100

	// 更新最高盈利点
	if pnlPercent > s.highestProfit {
		s.highestProfit = pnlPercent
	}

	// 1. 首單止盈检查
	if len(filled) == 1 && pnlPercent >= s.strategyCfg.FirstOrderTakeProfit {
		logger.Info("💰 [%s] 首單止盈触发: 盈利=%.2f%%", s.name, pnlPercent)
		return s.closeAllPositions(price, "首單止盈")
	}

	// 2. 尾單止盈检查（S2：只平最后一层，保留其余层继续等待全倉止盈）
	if len(filled) > 1 {
		lastLayer := filled[len(filled)-1]
		lastPnlPercent := (price - lastLayer.Price) / lastLayer.Price * 100
		if lastPnlPercent >= s.strategyCfg.LastOrderTakeProfit {
			logger.Info("💰 [%s] 尾單止盈触发: 尾單(层级 %d)盈利=%.2f%%", s.name, lastLayer.Index, lastPnlPercent)
			return s.closeLastLayer(lastLayer, price)
		}
	}

	// 3. 全倉止盈检查
	if pnlPercent >= s.strategyCfg.TotalTakeProfit {
		logger.Info("💰 [%s] 全倉止盈触发: 總盈利=%.2f%%", s.name, pnlPercent)
		return s.closeAllPositions(price, "全倉止盈")
	}

	// 4. 追踪止盈
	if !s.takeProfitTriggered && pnlPercent >= s.strategyCfg.TrailingActivation {
		s.takeProfitTriggered = true
		logger.Info("🎯 [%s] 追踪止盈激活: 當前盈利=%.2f%%", s.name, pnlPercent)
	}

	if s.takeProfitTriggered {
		drawdown := s.highestProfit - pnlPercent
		if drawdown >= s.strategyCfg.TrailingTakeProfit {
			logger.Info("💰 [%s] 追踪止盈触发: 最高盈利=%.2f%%, 回撤=%.2f%%",
				s.name, s.highestProfit, drawdown)
			return s.closeAllPositions(price, "追踪止盈")
		}
	}

	// 5. 止损检查
	if pnlPercent <= -s.strategyCfg.StopLoss {
		logger.Warn("🛑 [%s] 止损触发: 亏损=%.2f%%", s.name, pnlPercent)
		return s.closeAllPositions(price, "止损")
	}

	return nil
}

// closeAllPositions 平倉所有倉位
func (s *DCAEnhancedStrategy) closeAllPositions(price float64, reason string) error {
	if s.totalQty <= 0 {
		return nil
	}

	// 🔥 精度处理：确保平倉數量符合交易所要求
	qDec := s.exchange.GetQuantityDecimals()
	qty := math.Floor(s.totalQty*math.Pow(10, float64(qDec))) / math.Pow(10, float64(qDec))

	if qty <= 0 {
		return nil
	}

	orderPrice := s.roundPrice(price)

	// 判斷订單來源（止损/止盈）
	orderSource := "stop_loss"
	if !strings.Contains(reason, "止损") {
		orderSource = "normal"
	}

	// 先撤掉未成交的开倉單，避免平倉後又成交出孤兒倉位
	if s.cancelPendingLayers() {
		logger.Warn("⚠️ [%s] 等待未成交 DCA 開倉單撤單終態，暫不提交平倉單", s.name)
		return nil
	}

	// 下賣單
	order, err := s.executor.PlaceOrder(&position.OrderRequest{
		Symbol:       s.strategyCfg.Symbol,
		Side:         "SELL",
		Quantity:     qty,
		Price:        orderPrice,
		ReduceOnly:   true,
		PositionSide: position.PositionSideLong,
		PostOnly:     orderSource != "stop_loss",
		OrderSource:  orderSource,
	})

	if err != nil {
		logger.Error("❌ [%s] 平倉失败 (%s, 數量=%.6f, 價格=%.2f): %v", s.name, reason, qty, orderPrice, err)
		return fmt.Errorf("DCA 策略 %s 平倉(%s)下單失败: %w", s.name, reason, err)
	}
	if order == nil {
		logger.Debug("🔒 [%s] 平倉单被执行器跳过，等待下一轮", s.name)
		return nil
	}

	// 计算盈亏
	pnl := s.totalQty*price - s.totalCost

	logger.Info("✅ [%s] 平倉單已下 (%s): 订單ID=%d, 數量=%.6f, 價格=%.2f, 預估盈亏=%.2f USDT",
		s.name, reason, order.OrderID, s.totalQty, price, pnl)

	s.isClosing = true
	s.closeOrderID = order.OrderID
	s.closeLayer = nil
	s.closeProgress = position.FillProgress{}
	s.closeRequestedQty, s.closeLimitPrice = qty, orderPrice
	if err := s.persistRuntimeStateLocked(); err != nil {
		s.requireDCAOrderReconciliation(&position.OrderUpdate{OrderID: order.OrderID}, "DCA close order accepted but runtime state persistence failed")
		return err
	}
	// A placement acknowledgement has no fee fields and may not carry authoritative
	// cumulative fills. Wait for the order stream/polling update before accounting.

	return nil
}

// closeLastLayer 尾單止盈：只平最后一层的成交數量（S2）
func (s *DCAEnhancedStrategy) closeLastLayer(layer *DCALayer, price float64) error {
	if layer == nil || layer.Quantity <= 0 {
		return nil
	}

	qDec := s.exchange.GetQuantityDecimals()
	qty := math.Floor(layer.Quantity*math.Pow(10, float64(qDec))) / math.Pow(10, float64(qDec))
	if qty <= 0 {
		return nil
	}
	orderPrice := s.roundPrice(price)

	if s.cancelPendingLayers() {
		logger.Warn("⚠️ [%s] 等待未成交 DCA 開倉單撤單終態，暫不提交尾層平倉單", s.name)
		return nil
	}

	order, err := s.executor.PlaceOrder(&position.OrderRequest{
		Symbol:       s.strategyCfg.Symbol,
		Side:         "SELL",
		Quantity:     qty,
		Price:        orderPrice,
		ReduceOnly:   true,
		PositionSide: position.PositionSideLong,
		PostOnly:     true,
		OrderSource:  "normal",
	})
	if err != nil {
		logger.Error("❌ [%s] 尾單止盈下單失败 (层级 %d, 數量=%.6f, 價格=%.2f): %v", s.name, layer.Index, qty, orderPrice, err)
		return fmt.Errorf("DCA 策略 %s 尾單止盈(层级 %d)下單失败: %w", s.name, layer.Index, err)
	}
	if order == nil {
		logger.Debug("🔒 [%s] 尾單止盈單被执行器跳过，等待下一轮", s.name)
		return nil
	}

	layerCost := layer.Cost * qty / layer.Quantity
	pnl := qty*price - layerCost

	logger.Info("✅ [%s] 尾單止盈單已下: 订單ID=%d, 层级=%d, 數量=%.6f, 價格=%.2f, 預估盈亏=%.2f USDT",
		s.name, order.OrderID, layer.Index, qty, price, pnl)

	s.isClosing = true
	s.closeOrderID = order.OrderID
	s.closeLayer = layer
	s.closeProgress = position.FillProgress{}
	s.closeRequestedQty, s.closeLimitPrice = qty, orderPrice
	if err := s.persistRuntimeStateLocked(); err != nil {
		s.requireDCAOrderReconciliation(&position.OrderUpdate{OrderID: order.OrderID}, "DCA layer close accepted but runtime state persistence failed")
		return err
	}
	// A placement acknowledgement has no fee fields and may not carry authoritative
	// cumulative fills. Wait for the order stream/polling update before accounting.
	return nil
}

// cancelPendingLayers 撤销所有未完全成交的开倉單（撤單回報到達後再回滚层状態）
func (s *DCAEnhancedStrategy) cancelPendingLayers() bool {
	ids := make([]int64, 0)
	pending := false
	now := time.Now()
	retryBefore := now.Add(-dcaPendingCancelRetryInterval)
	for _, layer := range s.layers {
		if layer == nil || (layer.Status != entryStatusPending && layer.Status != entryStatusPartiallyFilled) {
			continue
		}
		pending = true
		if layer.OrderID <= 0 {
			continue
		}
		if layer.CancelRequestedAt.IsZero() || !layer.CancelRequestedAt.After(retryBefore) {
			layer.CancelRequestedAt = now
			ids = append(ids, layer.OrderID)
		}
	}
	if len(ids) > 0 {
		if err := s.executor.BatchCancelOrders(ids); err != nil {
			logger.Warn("⚠️ [%s] 平倉前撤銷未成交開倉單未確認 (訂單=%v): %v", s.name, ids, err)
		}
	}
	return pending
}

// recordCloseStats 更新平倉统计
func (s *DCAEnhancedStrategy) recordCloseStats(pnl, volume float64) {
	s.stats.TotalTrades++
	s.stats.TotalPnL += pnl
	s.stats.TotalVolume += volume

	winCount := s.stats.WinRate * float64(s.stats.TotalTrades-1)
	if pnl > 0 {
		winCount++
	}
	s.stats.WinRate = winCount / float64(s.stats.TotalTrades)
}

// saveCloseTrade 保存平倉交易記錄
func (s *DCAEnhancedStrategy) saveCloseTrade(executionKey string, sellOrderID int64, avgBuyPrice, orderPrice, qty, pnl, fee, exchangePnL float64) bool {
	if s.tradeStorage == nil {
		logger.Error("[%s] 成交账本未配置，拒绝确认 DCA 平仓成交", s.name)
		return false
	}

	buyOrderID := int64(0) // DCA策略无法追溯历史买入订单ID
	exchangeName := strings.ToLower(s.exchange.GetName())
	if exchangeName == "" {
		exchangeName = "binance"
	}

	var err error
	// 成交账本必须使用幂等写入，否则提交成功但确认丢失后重试会重复记账。
	marketType := "futures"
	if s.cfg != nil && strings.TrimSpace(s.cfg.Trading.MarketType) != "" {
		marketType = strings.ToLower(strings.TrimSpace(s.cfg.Trading.MarketType))
	}
	idempotent, ok := s.tradeStorage.(interface{ SaveTradeIdempotent(*storage.Trade) error })
	if !ok {
		logger.Error("[%s] 成交账本不支持幂等写入，拒绝确认 DCA 平仓成交", s.name)
		return false
	}
	pnlAsset := strings.ToUpper(strings.TrimSpace(s.exchange.GetQuoteAsset()))
	if pnlAsset == "" {
		logger.Error("[%s] 计价币缺失，拒绝写入 DCA 平仓成交", s.name)
		return false
	}
	err = idempotent.SaveTradeIdempotent(&storage.Trade{
		ExecutionKey: executionKey, BuyOrderID: buyOrderID, SellOrderID: sellOrderID, BotID: s.effectiveBotID(),
		Exchange: exchangeName, MarketType: marketType, PnLAsset: pnlAsset, Symbol: s.strategyCfg.Symbol,
		BuyPrice: avgBuyPrice, SellPrice: orderPrice, Quantity: qty, PnL: pnl, ExchangePnL: exchangePnL,
		Fee: fee, FeeAsset: pnlAsset, CreatedAt: time.Now(),
	})

	if err != nil {
		logger.Warn("⚠️ [%s] 保存交易記錄失败: %v (買入價: %.2f, 賣出價: %.2f, 數量: %.6f, 盈亏: %.2f)",
			s.name, err, avgBuyPrice, orderPrice, qty, pnl)
		return false
	}
	if pnl < 0 {
		logger.Warn("🛑 [%s] [止损/亏损交易已保存] 買入價: %.2f, 賣出價: %.2f, 數量: %.6f, 盈亏: %.4f, OrderID: %d",
			s.name, avgBuyPrice, orderPrice, qty, pnl, sellOrderID)
	} else {
		logger.Debug("💰 [%s] [交易記錄已保存] 買入價: %.2f, 賣出價: %.2f, 數量: %.6f, 盈亏: %.4f",
			s.name, avgBuyPrice, orderPrice, qty, pnl)
	}
	return true
}

// removeLayer 移除指定层并重算層數與總计
func (s *DCAEnhancedStrategy) removeLayer(target *DCALayer) {
	kept := make([]*DCALayer, 0, len(s.layers))
	for _, layer := range s.layers {
		if layer != target {
			kept = append(kept, layer)
		}
	}
	s.layers = kept
	s.currentLayer = len(s.layers)
	s.updateTotals()
}

// reduceLayer 按平倉成交數量减少某一层；平完则移除
func (s *DCAEnhancedStrategy) reduceLayer(layer *DCALayer, qty float64) {
	if layer == nil || qty <= 0 {
		return
	}
	if qty >= layer.Quantity-entryQtyEpsilon {
		s.removeLayer(layer)
		return
	}
	remainRatio := (layer.Quantity - qty) / layer.Quantity
	layer.Cost *= remainRatio
	layer.OpeningFee *= remainRatio
	layer.Quantity -= qty
	s.updateTotals()
}

// reduceAllLayers 全倉平倉單部分成交後被撤：按比例缩减所有已成交层
func (s *DCAEnhancedStrategy) reduceAllLayers(qty float64) {
	if qty <= 0 || s.totalQty <= 0 {
		return
	}
	if qty >= s.totalQty-entryQtyEpsilon {
		s.resetPositionState()
		return
	}
	remainRatio := (s.totalQty - qty) / s.totalQty
	for _, layer := range s.filledLayers() {
		layer.Quantity *= remainRatio
		layer.Cost *= remainRatio
		layer.OpeningFee *= remainRatio
	}
	s.updateTotals()
}

func (s *DCAEnhancedStrategy) resetPositionState() {
	s.layers = make([]*DCALayer, 0, s.maxLayers)
	s.totalCost = 0
	s.totalQty = 0
	s.avgEntryPrice = 0
	s.currentLayer = 0
	s.highestProfit = 0
	s.takeProfitTriggered = false
	s.isClosing = false
	s.closeOrderID = 0
	s.closeLayer = nil
	s.closeProgress = position.FillProgress{}
	s.closeRequestedQty, s.closeLimitPrice = 0, 0
}

// isTrendUp 判断趋势是否向上
func (s *DCAEnhancedStrategy) isTrendUp() bool {
	if len(s.priceHistory) < s.strategyCfg.TrendPeriod*2 {
		return true // 數據不足，默认允許开倉
	}

	prices := s.priceHistory[len(s.priceHistory)-s.strategyCfg.TrendPeriod*2:]

	var shortMA, longMA float64
	shortPeriod := s.strategyCfg.TrendPeriod
	longPeriod := s.strategyCfg.TrendPeriod * 2

	switch s.strategyCfg.TrendMethod {
	case "ema":
		shortEMA := indicators.EMA(prices, shortPeriod)
		longEMA := indicators.EMA(prices, longPeriod)
		if shortEMA != nil && len(shortEMA) > 0 {
			shortMA = shortEMA[len(shortEMA)-1]
		}
		if longEMA != nil && len(longEMA) > 0 {
			longMA = longEMA[len(longEMA)-1]
		}
	default: // ma
		shortSMA := indicators.SMA(prices, shortPeriod)
		longSMA := indicators.SMA(prices, longPeriod)
		if shortSMA != nil && len(shortSMA) > 0 {
			shortMA = shortSMA[len(shortSMA)-1]
		}
		if longSMA != nil && len(longSMA) > 0 {
			longMA = longSMA[len(longSMA)-1]
		}
	}

	return shortMA >= longMA
}

// OnOrderUpdate 订單更新处理
func (s *DCAEnhancedStrategy) OnOrderUpdate(update *position.OrderUpdate) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if update == nil || update.OrderID == 0 {
		return nil
	}

	if s.isClosing && update.OrderID == s.closeOrderID {
		s.handleCloseOrderUpdate(update)
		return s.persistRuntimeStateLocked()
	}

	// 查找對应的层级
	for _, layer := range s.layers {
		if layer.OrderID == update.OrderID {
			s.handleLayerOrderUpdate(layer, update)
			return s.persistRuntimeStateLocked()
		}
	}

	return nil
}

func finiteNumber(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

// handleLayerOrderUpdate 处理开倉/加倉單回報：按實際成交數量/均價計入持倉；未成交即終止则回滚该层（S3）
func (s *DCAEnhancedStrategy) handleLayerOrderUpdate(layer *DCALayer, update *position.OrderUpdate) {
	filled := signalOrderStatusFilled(update.Status)
	terminal := signalOrderStatusTerminal(update.Status)
	if !filled && !terminal && !signalOrderStatusPartiallyFilled(update.Status) {
		return
	}
	if layer.RequestedQuantity == 0 {
		layer.RequestedQuantity = layer.Quantity
	}
	qty, _ := entryFillFromUpdate(update)
	if qty > layer.FillProgress.Quantity && update.AvgPrice <= 0 {
		return
	}
	nextProgress := layer.FillProgress
	delta, incrementalPrice := nextProgress.Advance(qty, update.AvgPrice, 0)
	if delta > 0 {
		baseFeeQty := update.BaseFeeQty
		if !finiteNumber(baseFeeQty) || baseFeeQty < 0 || baseFeeQty > delta+entryQtyEpsilon ||
			baseFeeQty > 0 && (!s.supportsSpotBaseFee() || !strings.EqualFold(strings.TrimSpace(update.Side), "BUY")) {
			s.requireDCAOrderReconciliation(update, "DCA entry base-asset fee is invalid for this market or execution")
			return
		}
		receivedQty := delta - baseFeeQty
		if !finiteNumber(receivedQty) || receivedQty <= 0 {
			s.requireDCAOrderReconciliation(update, "DCA entry base-asset fee consumes the entire fill")
			return
		}
		openingFee, feeKnown := s.commissionInQuote(update.Commission, update.CommissionAsset, incrementalPrice)
		if !feeKnown {
			s.requireDCAOrderReconciliation(update, "DCA open fee is not denominated in a supported quote asset")
			return
		}
		if layer.Status == entryStatusPending {
			layer.Quantity, layer.Cost = 0, 0
		}
		layer.Quantity += receivedQty
		layer.Cost += receivedQty * incrementalPrice
		layer.Price = layer.Cost / layer.Quantity
		layer.OpeningFee += openingFee
		layer.EntryBaseFeeQty += baseFeeQty
		layer.FillProgress = nextProgress
		layer.FilledAt = time.Now()
	} else {
		layer.FillProgress = nextProgress
	}
	if layer.FillProgress.Quantity > 0 || layer.Status == entryStatusFilled {
		if filled || terminal {
			layer.Status = entryStatusFilled
		} else {
			layer.Status = entryStatusPartiallyFilled
		}
		s.updateTotals()
	} else if terminal {
		s.removeLayer(layer)
	}
}

// GetPositions 獲取持倉
func (s *DCAEnhancedStrategy) GetPositions() []*Position {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.totalQty <= 0 {
		return []*Position{}
	}

	currentPnL := 0.0
	openingFee := 0.0
	for _, layer := range s.filledLayers() {
		openingFee += layer.OpeningFee
	}
	if s.lastPrice > 0 {
		currentPnL = s.totalQty*s.lastPrice - s.totalCost - openingFee
	}

	return []*Position{
		{
			Symbol:       s.strategyCfg.Symbol,
			Size:         s.totalQty,
			EntryPrice:   s.avgEntryPrice,
			OpeningFee:   openingFee,
			CurrentPrice: s.lastPrice,
			PnL:          currentPnL,
		},
	}
}

// GetOrders 獲取訂單
func (s *DCAEnhancedStrategy) GetOrders() []*Order {
	s.mu.RLock()
	defer s.mu.RUnlock()

	orders := make([]*Order, 0, len(s.layers))
	for _, layer := range s.layers {
		orders = append(orders, &Order{
			OrderID:  layer.OrderID,
			Symbol:   s.strategyCfg.Symbol,
			Side:     "BUY",
			Price:    layer.Price,
			Quantity: layer.Quantity,
			Status:   layer.Status,
		})
	}

	return orders
}

// GetStatistics 獲取统计
func (s *DCAEnhancedStrategy) GetStatistics() *StrategyStatistics {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.stats == nil {
		return &StrategyStatistics{}
	}
	statsCopy := *s.stats
	return &statsCopy
}

// GetLayerInfo 獲取层级信息
func (s *DCAEnhancedStrategy) GetLayerInfo() string {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return fmt.Sprintf("當前层數: %d/%d, 總成本: %.2f, 總持倉: %.6f, 平均成本: %.2f, 动態间距: %.2f%%",
		len(s.layers), s.maxLayers, s.totalCost, s.totalQty, s.avgEntryPrice, s.dynamicInterval)
}

// GetDynamicInterval 獲取當前动態间距
func (s *DCAEnhancedStrategy) GetDynamicInterval() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.dynamicInterval
}

// IsPaused 是否暂停加倉
func (s *DCAEnhancedStrategy) IsPaused() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.isPaused
}

// GetVisualizationData 獲取策略可视化數據
func (s *DCAEnhancedStrategy) GetVisualizationData() map[string]interface{} {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data := make(map[string]interface{})

	// 分层持仓数据
	layers := make([]map[string]interface{}, 0, len(s.layers))
	for i, layer := range s.layers {
		currentPnL := 0.0
		if s.lastPrice > 0 {
			currentPnL = layer.Quantity*s.lastPrice - layer.Cost
		}
		pnlPercent := 0.0
		if layer.Cost > 0 {
			pnlPercent = (currentPnL / layer.Cost) * 100
		}

		layers = append(layers, map[string]interface{}{
			"index":      i + 1,
			"price":      layer.Price,
			"quantity":   layer.Quantity,
			"cost":       layer.Cost,
			"pnl":        currentPnL,
			"pnlPercent": pnlPercent,
			"status":     layer.Status,
			"filledAt":   layer.FilledAt.Unix(),
		})
	}
	data["layers"] = layers

	// ATR和动态间距
	atrValue := 0.0
	if s.atr != nil && len(s.candles) >= s.strategyCfg.ATRPeriod+1 {
		atrValue = s.atr.CurrentATR(s.candles)
	}
	data["atr"] = atrValue
	data["dynamicInterval"] = s.dynamicInterval
	data["baseInterval"] = s.baseInterval
	data["minPriceStep"] = s.strategyCfg.MinPriceStep
	data["maxPriceStep"] = s.strategyCfg.MaxPriceStep

	// 当前价格和平均成本
	data["currentPrice"] = s.lastPrice
	data["avgEntryPrice"] = s.avgEntryPrice
	data["totalCost"] = s.totalCost
	data["totalQty"] = s.totalQty

	// 止盈止损线
	if s.avgEntryPrice > 0 {
		data["firstOrderTakeProfit"] = s.avgEntryPrice * (1 + s.strategyCfg.FirstOrderTakeProfit/100)
		data["lastOrderTakeProfit"] = s.avgEntryPrice * (1 + s.strategyCfg.LastOrderTakeProfit/100)
		data["totalTakeProfit"] = s.avgEntryPrice * (1 + s.strategyCfg.TotalTakeProfit/100)
		data["stopLoss"] = s.avgEntryPrice * (1 - s.strategyCfg.StopLoss/100)
	}

	// 下一买入点
	if len(s.layers) > 0 && s.currentLayer < s.maxLayers {
		lastLayer := s.layers[len(s.layers)-1]
		requiredDrop := s.getRequiredDrop(s.currentLayer)
		nextBuyPrice := lastLayer.Price * (1 - requiredDrop/100)
		data["nextBuyPrice"] = nextBuyPrice
		data["requiredDrop"] = requiredDrop
		if s.lastPrice > 0 {
			distanceToNextBuy := ((s.lastPrice - nextBuyPrice) / s.lastPrice) * 100
			data["distanceToNextBuy"] = distanceToNextBuy
		}
	}

	// 瀑布保护状态
	data["isPaused"] = s.isPaused
	data["pauseUntil"] = s.pauseUntil.Unix()
	data["cascadeProtection"] = s.strategyCfg.CascadeProtection

	// 趋势过滤状态
	if s.strategyCfg.TrendFilterEnabled {
		data["trendFilterEnabled"] = true
		data["isTrendUp"] = s.isTrendUp()
	} else {
		data["trendFilterEnabled"] = false
	}

	// 追踪止盈状态
	data["takeProfitTriggered"] = s.takeProfitTriggered
	data["highestProfit"] = s.highestProfit

	// 层级信息
	data["currentLayer"] = len(s.layers)
	data["maxLayers"] = s.maxLayers

	return data
}
