package position

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"quantmesh/config"
	"quantmesh/event"
	"quantmesh/logger"
	"quantmesh/utils"
)

// OrderUpdate 订单更新事件（避免依赖 websocket 包）
type OrderUpdate struct {
	OrderID       int64
	ClientOrderID string
	Symbol        string
	Status        string
	ExecutedQty   float64
	Price         float64
	AvgPrice      float64
	Side          string
	Type          string
	UpdateTime    int64
}

// BatchPlaceOrdersResult 批量下单结果
type BatchPlaceOrdersResult struct {
	PlacedOrders     []*Order        // 成功下单的订单列表
	HasMarginError   bool            // 是否出现保证金不足错误
	ReduceOnlyErrors map[string]bool // ReduceOnly错误的订单（key为ClientOrderID）
}

// OrderExecutorInterface 订单执行器接口（避免循环导入）
type OrderExecutorInterface interface {
	PlaceOrder(req *OrderRequest) (*Order, error)
	BatchPlaceOrders(orders []*OrderRequest) ([]*Order, bool)
	BatchPlaceOrdersWithDetails(orders []*OrderRequest) *BatchPlaceOrdersResult
	BatchCancelOrders(orderIDs []int64) error
}

// OrderRequest 订单请求（避免循环导入）
type OrderRequest struct {
	Symbol        string
	Side          string
	Price         float64
	Quantity      float64
	PriceDecimals int    // 价格小数位数（用于格式化价格字符串）
	ReduceOnly    bool   // 是否只减仓（平仓单）
	PostOnly      bool   // 是否只做 Maker（Post Only）
	ClientOrderID string // 自定义订单ID
	StrategyName  string // 策略名称（可选，用于日志追踪）
	StrategyType  string // 策略类型（可选，如 "grid", "dca", "martingale"）
}

// Order 订单信息（避免循环导入）
type Order struct {
	OrderID       int64
	ClientOrderID string
	Symbol        string
	Side          string
	Price         float64
	Quantity      float64
	Status        string
	CreatedAt     time.Time
}

// 订单状态常量
const (
	OrderStatusNotPlaced       = "NOT_PLACED"       // 未下单
	OrderStatusPlaced          = "PLACED"           // 已下单
	OrderStatusConfirmed       = "CONFIRMED"        // 已确认（WebSocket确认）
	OrderStatusPartiallyFilled = "PARTIALLY_FILLED" // 部分成交
	OrderStatusFilled          = "FILLED"           // 全部成交
	OrderStatusCancelRequested = "CANCEL_REQUESTED" // 已申请撤单
	OrderStatusCanceled        = "CANCELED"         // 已撤单
)

// 持仓状态常量
const (
	PositionStatusEmpty  = "EMPTY"  // 空仓
	PositionStatusFilled = "FILLED" // 有仓
)

// 槽位锁定状态
const (
	SlotStatusFree    = "FREE"    // 空闲，可操作
	SlotStatusPending = "PENDING" // 等待下单确认
	SlotStatusLocked  = "LOCKED"  // 已锁定，有活跃订单
)

// InventorySlot 库存槽位（每个价格点一个）
type InventorySlot struct {
	Price float64 // 价格（作为key，支持高精度）

	// 持仓信息
	PositionStatus string  // 持仓状态：空仓/有仓
	PositionQty    float64 // 持仓数量（支持小数点后3位）

	// 订单信息 (买卖互斥)
	OrderID        int64     // 订单ID
	ClientOID      string    // 自定义订单ID
	OrderSide      string    // 订单方向 (BUY/SELL)
	OrderStatus    string    // 订单状态
	OrderPrice     float64   // 订单价格
	OrderFilledQty float64   // 成交数量
	OrderCreatedAt time.Time // 创建时间

	// 🔥 新增：槽位锁定状态，防止并发重复操作
	SlotStatus string // FREE/PENDING/LOCKED

	// PostOnly失败计数（连续失败3次后降级为普通单）
	PostOnlyFailCount int

	mu sync.RWMutex // 槽位级别的锁（细粒度锁）
}

// PositionInfo 持仓信息（简化版，避免循环导入）
type PositionInfo struct {
	Symbol string
	Size   float64
}

// IExchange 交易所接口（避免循环导入）
// 注意：这里不能直接使用 exchange.IExchange，否则会循环导入
// 所以定义一个子集接口，只包含对账需要的方法
type IExchange interface {
	GetName() string // 获取交易所名称
	GetPositions(ctx context.Context, symbol string) (interface{}, error)
	GetOpenOrders(ctx context.Context, symbol string) (interface{}, error)
	GetOrder(ctx context.Context, symbol string, orderID int64) (interface{}, error)
	GetBaseAsset() string                                     // 获取基础资产（交易币种）
	CancelAllOrders(ctx context.Context, symbol string) error // 取消所有订单
	GetAccount(ctx context.Context) (interface{}, error)      // 获取账户信息（返回 *exchange.Account 或类似结构）
	GetPriceDecimals() int                                    // 获取价格精度
	GetQuantityDecimals() int                                 // 获取数量精度
}

// TradeStorage 交易存储接口（避免循环导入）
// 用于保存交易记录（买卖配对）
type TradeStorage interface {
	SaveTrade(buyOrderID, sellOrderID int64, exchange, symbol string, buyPrice, sellPrice, quantity, pnl float64, createdAt time.Time) error
}

// ReconciliationStorage 对账存储接口（避免循环导入）
// 用于恢复对账统计值
type ReconciliationStorage interface {
	GetLatestReconciliationHistory(exchange, symbol string) (interface{}, error) // 返回 *storage.ReconciliationHistory
	GetReconciliationCount(exchange, symbol string) (int64, error)
}

// ITrendDetector 趋势检测器接口（避免循环导入）
type ITrendDetector interface {
	GetCurrentTrend() string
}

// SuperPositionManager 超级仓位管理器
type SuperPositionManager struct {
	config       *config.Config
	executor     OrderExecutorInterface
	exchange     IExchange
	exchangeName string // 交易所名称（配置中的名称，如 "binance"）

	// 价格锚点（初始化时的市场价格）
	anchorPrice float64
	// 最后市场价格（用于打印状态）
	lastMarketPrice atomic.Value // float64
	// 价格精度（根据锚点价格检测得出的小数位数）
	priceDecimals int
	// 数量精度（从交易所获取）
	quantityDecimals int

	// 库存槽位：价格 -> 槽位
	slots sync.Map // map[float64]*InventorySlot

	// 保证金管理
	insufficientMargin bool
	marginLockTime     time.Time
	marginLockDuration time.Duration

	// 风险监控状态
	peakPnL       float64        // 记录最高未实现盈亏（用于回撤止盈）
	trendDetector ITrendDetector // 趋势检测器

	// 资金分配管理器
	allocationManager *AllocationManager

	// 事件总线（用于发送告警）
	eventBus EventBus

	// 统计（注意：以下字段被 safety.Reconciler 和 PrintPositions 使用，不可删除）
	totalBuyQty       atomic.Value // float64 - 累计买入数量
	totalSellQty      atomic.Value // float64 - 累计卖出数量
	reconcileCount    atomic.Int64 // 对账次数
	lastReconcileTime atomic.Value // time.Time - 最后对账时间

	// 交易存储（可选，用于保存交易记录）
	tradeStorage TradeStorage

	// 初始化标志
	isInitialized atomic.Bool

	// 暂停标志
	isPaused atomic.Bool

	mu sync.RWMutex // 全局锁（用于关键操作）
}

// EventBus 事件总线接口
type EventBus interface {
	Publish(evt *event.Event)
}

// NewSuperPositionManager 创建超级仓位管理器
func NewSuperPositionManager(cfg *config.Config, executor OrderExecutorInterface, exchange IExchange, priceDecimals, quantityDecimals int) *SuperPositionManager {
	marginLockSec := cfg.Trading.MarginLockDurationSec
	if marginLockSec <= 0 {
		marginLockSec = 10 // 默认10秒
	}

	// 从配置中获取交易所名称
	exchangeName := strings.ToLower(cfg.App.CurrentExchange)
	if exchangeName == "" {
		exchangeName = "binance" // 默认值
	}

	spm := &SuperPositionManager{
		config:             cfg,
		executor:           executor,
		exchange:           exchange,
		exchangeName:       exchangeName,
		insufficientMargin: false,
		marginLockDuration: time.Duration(marginLockSec) * time.Second,
		priceDecimals:      priceDecimals,
		quantityDecimals:   quantityDecimals,
		peakPnL:            -math.MaxFloat64, // 初始化为一个极小值
		tradeStorage:       nil,              // 默认不保存交易记录，可通过 SetTradeStorage 设置
		allocationManager:  NewAllocationManager(cfg), // 初始化资金分配管理器
	}
	spm.totalBuyQty.Store(0.0)
	spm.totalSellQty.Store(0.0)
	spm.lastReconcileTime.Store(time.Now())
	spm.lastMarketPrice.Store(0.0)
	return spm
}

// Pause 暂停交易
func (spm *SuperPositionManager) Pause() {
	spm.isPaused.Store(true)
	logger.Warn("⏸️ [%s] 仓位管理器已暂停交易", spm.config.Trading.Symbol)
}

// Resume 恢复交易
func (spm *SuperPositionManager) Resume() {
	spm.isPaused.Store(false)
	logger.Info("▶️ [%s] 仓位管理器已恢复交易", spm.config.Trading.Symbol)
}

// IsPaused 是否已暂停
func (spm *SuperPositionManager) IsPaused() bool {
	return spm.isPaused.Load()
}

// SetEventBus 设置事件总线
func (spm *SuperPositionManager) SetEventBus(eventBus EventBus) {
	spm.eventBus = eventBus
}

// SetTradeStorage 设置交易存储接口（用于保存交易记录）
func (spm *SuperPositionManager) SetTradeStorage(storage TradeStorage) {
	spm.tradeStorage = storage
}

// getActualMargin 获取实际使用的保证金（考虑杠杆）
// 对于有杠杆的交易，实际保证金 = 订单价值 / 杠杆倍数
func (spm *SuperPositionManager) getActualMargin(orderValue float64) float64 {
	if orderValue <= 0 {
		return 0
	}

	// 获取杠杆倍数
	leverage := 1 // 默认1倍（无杠杆）
	ctx := context.Background()

	// 先尝试从账户信息中获取
	if accountResult, err := spm.exchange.GetAccount(ctx); err == nil && accountResult != nil {
		accountValue := reflect.ValueOf(accountResult)
		if accountValue.Kind() == reflect.Ptr {
			accountValue = accountValue.Elem()
		}
		if leverageField := accountValue.FieldByName("AccountLeverage"); leverageField.IsValid() && leverageField.CanInterface() {
			if lev, ok := leverageField.Interface().(int); ok && lev > 0 {
				leverage = lev
			}
		}
	}

	// 如果从账户中获取不到，尝试从持仓中获取
	if leverage == 1 {
		if positionsInterface, err := spm.exchange.GetPositions(ctx, spm.config.Trading.Symbol); err == nil && positionsInterface != nil {
			// 使用反射处理不同类型的持仓信息
			positionsValue := reflect.ValueOf(positionsInterface)
			if positionsValue.Kind() == reflect.Slice {
				for i := 0; i < positionsValue.Len(); i++ {
					posValue := positionsValue.Index(i)
					if posValue.Kind() == reflect.Ptr {
						posValue = posValue.Elem()
					} else if posValue.Kind() == reflect.Interface {
						posValue = posValue.Elem()
					}
					
					// 尝试获取 Leverage 字段
					if leverageField := posValue.FieldByName("Leverage"); leverageField.IsValid() && leverageField.CanInterface() {
						if lev, ok := leverageField.Interface().(int); ok && lev > 0 {
							leverage = lev
							logger.Debug("🔍 [杠杆检测] 从持仓信息中获取到杠杆倍数: %dx", leverage)
							break
						}
					}
				}
			}
		}
	}

	// 计算实际保证金
	return orderValue / float64(leverage)
}

// SetTrendDetector 设置趋势检测器
func (spm *SuperPositionManager) SetTrendDetector(td ITrendDetector) {
	spm.mu.Lock()
	defer spm.mu.Unlock()
	spm.trendDetector = td
}

// Initialize 初始化管理器（设置价格锚点并创建初始槽位）
func (spm *SuperPositionManager) Initialize(initialPrice float64, initialPriceStr string) error {
	spm.mu.Lock()
	defer spm.mu.Unlock()

	if initialPrice <= 0 {
		return fmt.Errorf("初始价格无效: %.2f", initialPrice)
	}

	// 1. 设置价格锚点（精度信息已经在构造函数中设置，从交易所获取）
	spm.anchorPrice = initialPrice
	spm.lastMarketPrice.Store(initialPrice) // 初始化最后市场价格
	logger.Info("✅ 价格锚点已设置: %s, 价格精度:%d, 数量精度:%d",
		formatPrice(initialPrice, spm.priceDecimals), spm.priceDecimals, spm.quantityDecimals)

	// 2. 直接使用锚点价格作为网格价格（不再对齐到整数）
	initialGridPrice := spm.anchorPrice
	logger.Info("✅ 初始网格价格: %s (使用锚点价格)", formatPrice(initialGridPrice, spm.priceDecimals))

	// 4. 使用统一的槽位价格计算方法创建初始槽位
	slotPrices := spm.calculateSlotPrices(initialGridPrice, spm.config.Trading.BuyWindowSize, "down")
	for _, price := range slotPrices {
		spm.getOrCreateSlot(price)
	}
	// 格式化槽位价格用于日志输出
	slotPricesStr := make([]string, len(slotPrices))
	for i, p := range slotPrices {
		slotPricesStr[i] = formatPrice(p, spm.priceDecimals)
	}
	logger.Info("✅ [初始化] 计算出的槽位价格: %v", slotPricesStr)

	// 5. 为初始槽位下买单
	err := spm.placeInitialBuyOrders()
	if err == nil {
		// 标记为已初始化
		spm.isInitialized.Store(true)
		logger.Info("✅ 初始化完成，网格价格: %s", formatPrice(initialGridPrice, spm.priceDecimals))
	}
	return err
}

// generateClientOrderID 生成自定义订单ID
// 使用新的紧凑格式，最大长度不超过18字符
// 格式: {price_int}_{side}_{timestamp}{seq}
// price_int: price * 10^decimals (转为整数)
// side: B=Buy, S=Sell
func (spm *SuperPositionManager) generateClientOrderID(price float64, side string) string {
	// 使用统一的 utils 包生成紧凑ID
	return utils.GenerateOrderID(price, side, spm.priceDecimals)
}

// parseClientOrderID 解析 ClientOrderID
// 返回: price, side, valid
func (spm *SuperPositionManager) parseClientOrderID(clientOrderID string) (float64, string, bool) {
	// 1. 先移除交易所前缀
	exchangeName := strings.ToLower(spm.exchange.GetName())
	cleanID := utils.RemoveBrokerPrefix(exchangeName, clientOrderID)

	// 2. 使用统一的 utils 包解析
	price, side, _, valid := utils.ParseOrderID(cleanID, spm.priceDecimals)
	if !valid {
		return 0, "", false
	}

	// 🔥 关键修复：不要对从ClientOrderID解析出的价格进行四舍五入！
	// 因为价格本身就是从整数还原的，已经是精确的值
	// 如果再次四舍五入，可能因为浮点数精度问题导致多个不同价格被映射到同一个槽位
	// 例如: 3116.85 和 3114.85 可能都被四舍五入成同一个值

	// 🔥 添加价格合理性检查：如果解析出的价格明显异常，记录警告
	// 可能原因：
	// 1. priceDecimals 参数错误
	// 2. 多交易对场景下，订单属于其他交易对（应该在上层过滤，但这里作为兜底检查）
	// 3. 历史遗留订单（切换交易对后的旧订单）
	if spm.anchorPrice > 1000 && price < 1000 && price > 0 {
		logger.Warn("⚠️ [价格解析异常] ClientOrderID=%s, 解析价格=%.2f, 锚点价格=%.2f, priceDecimals=%d",
			clientOrderID, price, spm.anchorPrice, spm.priceDecimals)
		logger.Warn("💡 [可能原因] 1) 此订单属于其他交易对 2) priceDecimals 参数错误 3) 历史遗留订单")
		logger.Warn("💡 [建议] 检查是否运行了多个交易对，确保订单推送已正确过滤 Symbol")

		// 尝试使用不同的 priceDecimals 重新解析（用于诊断）
		for testDecimals := 1; testDecimals <= 3; testDecimals++ {
			if testDecimals == spm.priceDecimals {
				continue
			}
			testPrice, _, _, testValid := utils.ParseOrderID(cleanID, testDecimals)
			if testValid && testPrice > 1000 && math.Abs(testPrice-spm.anchorPrice) < spm.anchorPrice*0.5 {
				logger.Warn("⚠️ [价格解析修复] 使用 priceDecimals=%d 重新解析得到价格=%.2f", testDecimals, testPrice)
				return testPrice, side, true
			}
		}

		// 无法修复，返回无效（避免创建错误的槽位）
		return 0, "", false
	}

	return price, side, true
}

// placeInitialBuyOrders 设定初始槽位（并恢复持仓槽位）
func (spm *SuperPositionManager) placeInitialBuyOrders() error {
	// 🔥 修改：只恢复持仓槽位，不再主动下单
	// 所有下单操作由 AdjustOrders 统一处理，避免时序问题
	existingPosition := spm.getExistingPosition()
	if existingPosition > 0 {
		logger.Info("🔄 [持仓恢复] 检测到现有持仓: %.4f，开始初始化卖单槽位", existingPosition)
		spm.initializeSellSlotsFromPosition(existingPosition)
	}

	logger.Info("✅ [初始化] 槽位已创建，订单下达将由 AdjustOrders 统一处理")
	return nil
}

// AdjustOrders 调整订单（交易入口）
func (spm *SuperPositionManager) AdjustOrders(currentPrice float64) error {
	// 🔥 移除初始化检查：现在完全由 AdjustOrders 控制所有下单
	// 初始化只负责恢复持仓状态，不再下单

	spm.mu.Lock()
	defer spm.mu.Unlock()

	// 检查是否暂停
	if spm.IsPaused() {
		logger.Debug("⏸️ [%s:%s] 交易已暂停，跳过订单调整", spm.exchangeName, spm.config.Trading.Symbol)
		return nil
	}

	// 验证价格有效性
	if currentPrice <= 0 {
		logger.Warn("⚠️ 收到无效价格: %.2f，跳过订单调整", currentPrice)
		return nil
	}

	// 对当前价格进行精度处理
	currentPrice = roundPrice(currentPrice, spm.priceDecimals)

	// 更新最后市场价格（用于打印状态）
	spm.lastMarketPrice.Store(currentPrice)

	// === 网格风控逻辑开始 ===
	if spm.config.Trading.GridRiskControl.Enabled {
		// 1. 硬为止损检查
		stopLossRatio := spm.config.Trading.GridRiskControl.StopLossRatio
		if stopLossRatio > 0 {
			unrealizedPnL := spm.calculateUnrealizedPnL(currentPrice)
			totalValue := spm.calculateTotalPositionValue(currentPrice)
			if totalValue > 0 {
				pnlRatio := unrealizedPnL / totalValue
				if pnlRatio <= -stopLossRatio {
					logger.Error("🚨 [网格风控] 触发硬为止损! 当前浮亏率: %.2f%%, 阈值: %.2f%%", pnlRatio*100, -stopLossRatio*100)
					spm.LiquidateAll()
					return nil
				}
			}
		}

		// 2. 动态止盈 (盈利回撤止盈) 检查
		triggerRatio := spm.config.Trading.GridRiskControl.TakeProfitTriggerRatio
		trailingRatio := spm.config.Trading.GridRiskControl.TrailingTakeProfitRatio
		if triggerRatio > 0 && trailingRatio > 0 {
			unrealizedPnL := spm.calculateUnrealizedPnL(currentPrice)
			totalValue := spm.calculateTotalPositionValue(currentPrice)
			if totalValue > 0 {
				currentProfitRatio := unrealizedPnL / totalValue
				
				// 更新最高盈利
				if currentProfitRatio > spm.peakPnL {
					spm.peakPnL = currentProfitRatio
					logger.Debug("💰 [网格风控] 更新最高盈利率: %.2f%%", spm.peakPnL*100)
				}

				// 如果盈利已经超过触发阈值，且从最高点回撤超过 trailingRatio
				if spm.peakPnL >= triggerRatio {
					drawdown := spm.peakPnL - currentProfitRatio
					if drawdown >= trailingRatio {
						logger.Warn("📈 [网格风控] 触发盈利回撤止盈! 最高盈利率: %.2f%%, 当前盈利率: %.2f%%, 回撤: %.2f%%, 阈值: %.2f%%",
							spm.peakPnL*100, currentProfitRatio*100, drawdown*100, trailingRatio*100)
						spm.LiquidateAll()
						spm.peakPnL = -math.MaxFloat64 // 重置最高点
						return nil
					}
				}
			} else {
				// 无持仓时重置最高盈利点
				spm.peakPnL = -math.MaxFloat64
			}
		}
	}
	// === 网格风控逻辑结束 ===

	// 检查保证金不足状态
	if spm.insufficientMargin {
		if time.Since(spm.marginLockTime) >= spm.marginLockDuration {
			logger.Info("✅ [保证金恢复] 锁定时间已过，恢复下单功能")
			spm.insufficientMargin = false
		} else {
			remainingTime := spm.marginLockDuration - time.Since(spm.marginLockTime)
			logger.Warn("⏸️ [暂停下单] 保证金不足，暂停下单中... (剩余时间: %.0f秒)", remainingTime.Seconds())
			return nil
		}
	}

	// 计算需要监控的价格范围
	buyWindowSize := spm.config.Trading.BuyWindowSize
	sellWindowSize := spm.config.Trading.SellWindowSize
	priceInterval := spm.config.Trading.PriceInterval

	// 动态计算网格价格
	currentGridPrice := spm.findNearestGridPrice(currentPrice)
	// logger.Debug("🔄 [实时调整] 当前价格: %s, 网格价格: %s, 买单窗口: %d, 卖单窗口: %d",
	// 	formatPrice(currentPrice, spm.priceDecimals), formatPrice(currentGridPrice, spm.priceDecimals), buyWindowSize, sellWindowSize)

	// 计算当前网格价格下方buy_window_size个价格
	slotPrices := spm.calculateSlotPrices(currentGridPrice, buyWindowSize, "down")

	var ordersToPlace []*OrderRequest
	var activeBuyOrdersInWindow int

	// 统计当前所有订单数量（分别统计买单和卖单）
	var currentOrderCount int
	var currentBuyOrderCount int
	var currentSellOrderCount int
	spm.slots.Range(func(key, value interface{}) bool {
		slot := value.(*InventorySlot)
		slot.mu.RLock()
		if slot.OrderStatus == OrderStatusPlaced || slot.OrderStatus == OrderStatusConfirmed ||
			slot.OrderStatus == OrderStatusPartiallyFilled {
			currentOrderCount++
			if slot.OrderSide == "BUY" {
				currentBuyOrderCount++
			} else if slot.OrderSide == "SELL" {
				currentSellOrderCount++
			}
		}
		slot.mu.RUnlock()
		return true
	})

	// 计算允许创建的订单数量上限
	threshold := spm.config.Trading.OrderCleanupThreshold
	if threshold <= 0 {
		threshold = 100
	}

	// 🔥 核心改进：不预留空间，允许订单数达到threshold上限
	// 剩余可用订单数 = 阈值 - 当前订单数
	remainingOrders := threshold - currentOrderCount
	if remainingOrders < 0 {
		remainingOrders = 0
	}

	// 买单允许的新增数量
	allowedNewBuyOrders := buyWindowSize
	if allowedNewBuyOrders > remainingOrders {
		allowedNewBuyOrders = remainingOrders
	}

	// 1. 处理买单
	buyOrdersToCreate := 0

	// 趋势过滤与层数限制预检查
	skipBuying := false
	if spm.config.Trading.GridRiskControl.Enabled {
		// 趋势过滤
		if spm.config.Trading.GridRiskControl.TrendFilterEnabled && spm.trendDetector != nil {
			trend := spm.trendDetector.GetCurrentTrend()
			if trend == "down" {
				logger.Warn("📉 [趋势过滤] 检测到下跌趋势，暂停买入")
				skipBuying = true
			}
		}

		// 层数限制
		maxLayers := spm.config.Trading.GridRiskControl.MaxGridLayers
		if maxLayers > 0 {
			currentLayers := spm.GetActiveLayers()
			if currentLayers >= maxLayers {
				logger.Warn("🚫 [层数限制] 当前持仓层数 (%d) 已达到最大值 (%d)，暂停买入", currentLayers, maxLayers)
				skipBuying = true
			}
		}
	}

	for _, price := range slotPrices {
		if skipBuying {
			break
		}
		slot := spm.getOrCreateSlot(price)
		slot.mu.Lock()

		// 🔥 槽位锁定检查：如果槽位正在被操作，跳过
		if slot.SlotStatus != SlotStatusFree {
			slot.mu.Unlock()
			continue
		}

		// 检查是否已有有效订单
		hasActiveOrder := false
		if slot.OrderStatus == OrderStatusPlaced || slot.OrderStatus == OrderStatusConfirmed ||
			slot.OrderStatus == OrderStatusPartiallyFilled {
			hasActiveOrder = true
			if slot.OrderSide == "BUY" {
				activeBuyOrdersInWindow++
			}
		}

		// 🔥 买单条件：持仓状态=EMPTY + 槽位锁=FREE + 无订单ID + 无ClientOID
		if slot.PositionStatus != PositionStatusEmpty {
			slot.mu.Unlock()
			continue
		}

		// 🔥 新逻辑：只检查槽位锁状态、OrderID和ClientOID，不检查OrderSide
		shouldCreateBuyOrder := !hasActiveOrder &&
			slot.SlotStatus == SlotStatusFree &&
			slot.OrderID == 0 &&
			slot.ClientOID == "" &&
			buyOrdersToCreate < allowedNewBuyOrders

		if shouldCreateBuyOrder {
			// 安全检查：买单价格不应高于当前价格
			safetyBuffer := spm.config.Trading.PriceInterval * 0.1
			if price >= currentPrice-safetyBuffer {
				slot.mu.Unlock()
				continue
			}

			quantity := spm.config.Trading.OrderQuantity / price
			// 使用从交易所获取的数量精度
			quantity = roundPrice(quantity, spm.quantityDecimals)

			// 如果数量过小被取整为 0，发布告警并暂停
			if quantity <= 0 && spm.quantityDecimals >= 0 {
				minQty := math.Pow10(-spm.quantityDecimals)
				logger.Error("🚨 [%s] 下单数量过小 (%.8f)，低于交易所最小精度 (%.8f)，交易已自动暂停！请在配置中调大 order_quantity", 
					spm.config.Trading.Symbol, spm.config.Trading.OrderQuantity/price, minQty)
				
				// 发布事件
				if spm.eventBus != nil {
					spm.eventBus.Publish(&event.Event{
						Type:      event.EventTypePrecisionAdjustment,
						Timestamp: time.Now(),
						Data: map[string]interface{}{
							"symbol":           spm.config.Trading.Symbol,
							"exchange":         spm.exchangeName,
							"order_quantity":   spm.config.Trading.OrderQuantity,
							"calculated_qty":   spm.config.Trading.OrderQuantity / price,
							"min_qty":          minQty,
							"price":            price,
							"action":           "pause",
							"reason":           "下单数量低于交易所最小精度",
						},
					})
				}
				
				// 暂停交易
				spm.Pause()
				slot.mu.Unlock()
				continue
			}

			// 生成 ClientOrderID
			clientOID := spm.generateClientOrderID(price, "BUY")

			// 🔥 锁定槽位：标记为PENDING状态，防止并发操作
			slot.SlotStatus = SlotStatusPending

			// 检查PostOnly失败计数，失败3次后不再使用PostOnly
			usePostOnly := slot.PostOnlyFailCount < 3

			ordersToPlace = append(ordersToPlace, &OrderRequest{
				Symbol:        spm.config.Trading.Symbol,
				Side:          "BUY",
				Price:         price,
				Quantity:      quantity,
				PriceDecimals: spm.priceDecimals,
				PostOnly:      usePostOnly,
				ClientOrderID: clientOID,
			})
			buyOrdersToCreate++
		}

		slot.mu.Unlock()
	}

	// 2. 处理卖单
	sellWindowMaxPrice := currentPrice + float64(sellWindowSize)*priceInterval
	sellWindowMaxPrice = roundPrice(sellWindowMaxPrice, spm.priceDecimals)

	type sellCandidate struct {
		SlotPrice     float64 // 槽位价格 (买入价)
		SellPrice     float64 // 目标卖出价
		Quantity      float64
		DistanceToMid float64
	}
	var sellCandidates []sellCandidate

	spm.slots.Range(func(key, value interface{}) bool {
		slotPrice := key.(float64) // 槽位Key = 买入价
		slot := value.(*InventorySlot)
		slot.mu.Lock()
		defer slot.mu.Unlock()

		// 🔥 卖单条件：持仓状态=FILLED + 槽位锁=FREE + 无订单ID + 无ClientOID
		if slot.PositionStatus == PositionStatusFilled &&
			slot.SlotStatus == SlotStatusFree &&
			slot.OrderID == 0 &&
			slot.ClientOID == "" {

			sellPrice := slotPrice + priceInterval
			sellPrice = roundPrice(sellPrice, spm.priceDecimals)

			// 窗口检查
			if slotPrice > sellWindowMaxPrice {
				return true
			}

			// 最小名义价值检查
			orderValue := sellPrice * slot.PositionQty
			minValue := spm.config.Trading.MinOrderValue
			if minValue <= 0 {
				minValue = 6.0
			}

			if orderValue >= minValue {
				distance := math.Abs(slotPrice - currentPrice)
				sellCandidates = append(sellCandidates, sellCandidate{
					SlotPrice:     slotPrice,
					SellPrice:     sellPrice,
					Quantity:      slot.PositionQty,
					DistanceToMid: distance,
				})
			}
		}
		return true
	})

	// 按距离排序
	sort.Slice(sellCandidates, func(i, j int) bool {
		return sellCandidates[i].DistanceToMid < sellCandidates[j].DistanceToMid
	})

	// 🔥 重新计算卖单的剩余配额（扣除新增买单后的剩余空间）
	remainingOrdersForSell := threshold - currentOrderCount - buyOrdersToCreate
	if remainingOrdersForSell < 0 {
		remainingOrdersForSell = 0
	}

	allowedNewSellOrders := sellWindowSize
	if allowedNewSellOrders > remainingOrdersForSell {
		allowedNewSellOrders = remainingOrdersForSell
	}

	// 生成卖单请求
	sellOrdersToCreate := 0
	// 🔥 调试日志: 显示订单配额计算详情（包含买卖单分布）
	logger.Debug("📊 [%s:%s] [订单配额] 阈值:%d, 当前订单:%d(买:%d/卖:%d), 剩余:%d, 新增买单:%d, 卖单候选:%d, 允许卖单:%d",
		spm.exchangeName, spm.config.Trading.Symbol, threshold, currentOrderCount, currentBuyOrderCount, currentSellOrderCount, remainingOrders, buyOrdersToCreate, len(sellCandidates), allowedNewSellOrders)
	if allowedNewSellOrders > 0 {
		for i := 0; i < len(sellCandidates) && sellOrdersToCreate < allowedNewSellOrders; i++ {
			candidate := sellCandidates[i]

			// 🔥 关键修复：最终验证PositionStatus必须为FILLED且有持仓，并且SlotStatus为FREE
			slot := spm.getOrCreateSlot(candidate.SlotPrice)
			slot.mu.Lock()

			// 🔥 双重检查：确保槽位仍然是FREE状态
			if slot.SlotStatus != SlotStatusFree {
				slot.mu.Unlock()
				continue
			}

			currentStatus := slot.PositionStatus
			currentQty := slot.PositionQty

			if currentStatus != PositionStatusFilled || currentQty <= 0 {
				slot.mu.Unlock()
				continue
			}

			// 🔥 立即锁定槽位：标记为PENDING状态，防止并发操作
			slot.SlotStatus = SlotStatusPending
			// 检查PostOnly失败计数，失败3次后不再使用PostOnly
			usePostOnly := slot.PostOnlyFailCount < 3
			slot.mu.Unlock()

			// 生成 ClientOrderID (注意：使用 SlotPrice 即买入价作为标识)
			clientOID := spm.generateClientOrderID(candidate.SlotPrice, "SELL")

			quantity := candidate.Quantity
			// 兜底检查：卖单数量也必须大于0
			if quantity <= 0 && spm.quantityDecimals >= 0 {
				minQty := math.Pow10(-spm.quantityDecimals)
				logger.Error("🚨 [%s] 卖单数量异常 (%.8f)，低于交易所最小精度 (%.8f)，交易已自动暂停！", 
					spm.config.Trading.Symbol, candidate.Quantity, minQty)
				
				// 发布事件
				if spm.eventBus != nil {
					spm.eventBus.Publish(&event.Event{
						Type:      event.EventTypePrecisionAdjustment,
						Timestamp: time.Now(),
						Data: map[string]interface{}{
							"symbol":           spm.config.Trading.Symbol,
							"exchange":         spm.exchangeName,
							"quantity":         candidate.Quantity,
							"min_qty":          minQty,
							"price":            candidate.SellPrice,
							"action":           "pause",
							"reason":           "卖单数量低于交易所最小精度",
						},
					})
				}
				
				// 暂停交易
				spm.Pause()
				slot.mu.Unlock()
				continue
			}

			ordersToPlace = append(ordersToPlace, &OrderRequest{
				Symbol:        spm.config.Trading.Symbol,
				Side:          "SELL",
				Price:         candidate.SellPrice,
				Quantity:      quantity,
				PriceDecimals: spm.priceDecimals,
				ReduceOnly:    true,
				PostOnly:      usePostOnly,
				ClientOrderID: clientOID, // 🔥
			})
			sellOrdersToCreate++
		}
	}

	// 执行下单前，检查资金分配
	if len(ordersToPlace) > 0 {
		// 获取账户余额（从交易所获取实际余额）
		var accountBalance float64 = 0
		var accountResult interface{} = nil
		ctx := context.Background()
		if spm.exchange != nil {
			var err error
			accountResult, err = spm.exchange.GetAccount(ctx)
			if err == nil && accountResult != nil {
				// 使用反射获取 AvailableBalance 字段
				// 注意：不同交易所可能返回不同的类型，使用反射统一处理
				accountValue := reflect.ValueOf(accountResult)
				if accountValue.Kind() == reflect.Ptr {
					accountValue = accountValue.Elem()
				}
				if balanceField := accountValue.FieldByName("AvailableBalance"); balanceField.IsValid() && balanceField.CanInterface() {
					if balance, ok := balanceField.Interface().(float64); ok {
						accountBalance = balance
					}
				}
				// 使用可用余额（AvailableBalance）进行资金分配检查
				// 注意：对于合约账户，如果有持仓，AvailableBalance可能为0，这是正常的
				logger.Debug("💰 [%s:%s] [资金分配] 账户可用余额: %.2f USDT", spm.exchangeName, spm.config.Trading.Symbol, accountBalance)
			} else {
				logger.Warn("⚠️ [%s:%s] [资金分配] 无法获取账户余额: %v，使用0作为默认值", spm.exchangeName, spm.config.Trading.Symbol, err)
			}
		}

		// 获取杠杆倍数（用于计算实际使用的保证金）
		leverage := 1 // 默认1倍（无杠杆）
		if accountResult != nil {
			accountValue := reflect.ValueOf(accountResult)
			if accountValue.Kind() == reflect.Ptr {
				accountValue = accountValue.Elem()
			}
			if leverageField := accountValue.FieldByName("AccountLeverage"); leverageField.IsValid() && leverageField.CanInterface() {
				if lev, ok := leverageField.Interface().(int); ok && lev > 0 {
					leverage = lev
				}
			}
		}
		// 如果从账户中获取不到，尝试从持仓中获取
		if leverage == 1 && spm.exchange != nil {
			if positionsInterface, err := spm.exchange.GetPositions(ctx, spm.config.Trading.Symbol); err == nil && positionsInterface != nil {
				switch positions := positionsInterface.(type) {
				case []interface{}:
					for _, pos := range positions {
						posValue := reflect.ValueOf(pos)
						if posValue.Kind() == reflect.Ptr {
							posValue = posValue.Elem()
						}
						if leverageField := posValue.FieldByName("Leverage"); leverageField.IsValid() && leverageField.CanInterface() {
							if lev, ok := leverageField.Interface().(int); ok && lev > 0 {
								leverage = lev
								break
							}
						}
					}
				}
			}
		}

		// 过滤掉超出资金分配的订单
		var validOrders []*OrderRequest
		for _, req := range ordersToPlace {
			orderValue := req.Quantity * req.Price // 订单名义金额（仓位价值）
			// 对于有杠杆的交易，实际使用的保证金 = 订单价值 / 杠杆倍数
			// 资金限额限制的是实际投入的资金，而不是仓位价值
			actualMargin := orderValue / float64(leverage)
			err := spm.allocationManager.CheckAndReserve(
				spm.exchangeName,
				spm.config.Trading.Symbol,
				actualMargin, // 使用实际保证金而不是订单价值
				accountBalance,
			)

			if err != nil {
				logger.Warn("⚠️ [%s:%s] [资金分配] %v (订单价值: %.2f USDT, 实际保证金: %.2f USDT, 杠杆: %dx)", 
					spm.exchangeName, spm.config.Trading.Symbol, err, orderValue, actualMargin, leverage)
				// 触发告警事件
				if spm.eventBus != nil {
					spm.eventBus.Publish(&event.Event{
						Type: event.EventTypeAllocationExceeded,
						Data: map[string]interface{}{
							"exchange":      spm.exchangeName,
							"symbol":        spm.config.Trading.Symbol,
							"error":         err.Error(),
							"order_value":   orderValue,
							"actual_margin": actualMargin,
							"leverage":      leverage,
						},
					})
				}
				// 释放槽位锁
				if price, _, valid := spm.parseClientOrderID(req.ClientOrderID); valid {
					slot := spm.getOrCreateSlot(price)
					slot.mu.Lock()
					if slot.SlotStatus == SlotStatusPending {
						slot.SlotStatus = SlotStatusFree
					}
					slot.mu.Unlock()
				}
				continue
			}
			validOrders = append(validOrders, req)
		}

		ordersToPlace = validOrders
	}

	// 执行下单
	if len(ordersToPlace) > 0 {
		logger.Debug("🔄 [%s:%s] [实时调整] 需要新增: %d 个订单", spm.exchangeName, spm.config.Trading.Symbol, len(ordersToPlace))
		result := spm.executor.BatchPlaceOrdersWithDetails(ordersToPlace)

		if result.HasMarginError {
			logger.Warn("⚠️ [保证金不足] 检测到保证金不足错误，暂停下单 %d 秒", int(spm.marginLockDuration.Seconds()))
			spm.insufficientMargin = true
			spm.marginLockTime = time.Now()
			spm.CancelAllBuyOrders()

			// 发送保证金不足告警事件
			if spm.eventBus != nil {
				spm.eventBus.Publish(&event.Event{
					Type: event.EventTypeMarginInsufficient,
					Data: map[string]interface{}{
						"exchange":      spm.exchangeName,
						"symbol":        spm.config.Trading.Symbol,
						"failed_orders": len(result.PlacedOrders),
						"error_message": "保证金不足，已暂停下单",
						"lock_duration": int(spm.marginLockDuration.Seconds()),
					},
				})
			}
		}

		// 🔥 构建成功订单的ClientOrderID集合
		placedClientOIDs := make(map[string]bool)
		for _, ord := range result.PlacedOrders {
			placedClientOIDs[ord.ClientOrderID] = true
		}

		// 🔥 处理 ReduceOnly 错误：清空对应槽位的持仓
		for clientOID := range result.ReduceOnlyErrors {
			price, side, valid := spm.parseClientOrderID(clientOID)
			if valid {
				if side == "SELL" {
					// SELL ReduceOnly：平多仓失败，清空槽位持仓状态
					slot := spm.getOrCreateSlot(price)
					slot.mu.Lock()
					if slot.PositionStatus == PositionStatusFilled {
						logger.Warn("⚠️ [ReduceOnly错误处理] 清空槽位持仓: 价格=%s, 原持仓=%.4f",
							formatPrice(price, spm.priceDecimals), slot.PositionQty)
						// 清空持仓状态
						slot.PositionStatus = PositionStatusEmpty
						slot.PositionQty = 0
						slot.SlotStatus = SlotStatusFree
					}
					slot.mu.Unlock()
				} else if side == "BUY" {
					// BUY ReduceOnly：平空仓失败，账户中无空仓（系统不管理空仓状态，仅记录日志）
					logger.Warn("⚠️ [ReduceOnly错误处理] BUY平空仓订单被拒绝: 价格=%s, 账户中无空仓",
						formatPrice(price, spm.priceDecimals))
				}
			}
		}

		// 🔥 释放未成功提交订单的槽位锁和资金
		for _, req := range ordersToPlace {
			if !placedClientOIDs[req.ClientOrderID] && !result.ReduceOnlyErrors[req.ClientOrderID] {
				// 这个订单没有成功提交（且不是ReduceOnly错误，因为已经处理过了），需要释放槽位锁和资金
				price, side, valid := spm.parseClientOrderID(req.ClientOrderID)
				if valid {
					slot := spm.getOrCreateSlot(price)
					slot.mu.Lock()
					if slot.SlotStatus == SlotStatusPending {
						slot.SlotStatus = SlotStatusFree
						logger.Debug("🔓 [释放槽位] 订单提交失败，释放槽位 %s 的锁 (ClientOID: %s)",
							formatPrice(price, spm.priceDecimals), req.ClientOrderID)
					}
					slot.mu.Unlock()
					
					// 🔥 释放预留的资金（只有买单需要释放，卖单不占用资金）
					if side == "BUY" {
						orderValue := req.Quantity * req.Price
						actualMargin := spm.getActualMargin(orderValue)
						if actualMargin > 0 {
							spm.allocationManager.Release(spm.exchangeName, spm.config.Trading.Symbol, actualMargin)
							logger.Debug("💰 [资金释放] 订单提交失败，释放预留资金: %.2f USDT (订单价值: %.2f USDT)", actualMargin, orderValue)
						}
					}
				}
			}
		}

		for _, ord := range result.PlacedOrders {
			// 解析 ClientOrderID
			price, side, valid := spm.parseClientOrderID(ord.ClientOrderID)

			if !valid {
				logger.Warn("⚠️ [%s:%s] [实时调整] 无法解析 ClientOID: %s", spm.exchangeName, spm.config.Trading.Symbol, ord.ClientOrderID)
				continue
			}

			// 获取槽位 (注意：无论是买单还是卖单，ID中编码的都是 SlotPrice)
			slot := spm.getOrCreateSlot(price)
			slot.mu.Lock()

			// 🔥 关键修复：检查是否是秒成交场景（买单或卖单都可能）
			// 秒成交的特征:
			// 1. 买单秒成交: PositionStatus=FILLED (刚成交) 且 OrderID=0 (已被WebSocket清空) 且 OrderSide=""
			// 2. 卖单秒成交: PositionStatus=EMPTY (已清空) 且 OrderID=0 (已被WebSocket清空) 且 OrderSide=""
			isInstantFill := false
			if side == "BUY" {
				// 买单秒成交: 有持仓但订单ID为0且OrderSide已清空
				isInstantFill = (slot.PositionStatus == PositionStatusFilled && slot.OrderID == 0 && slot.OrderSide == "")
			} else if side == "SELL" {
				// 🔥 卖单秒成交: 持仓已清空且订单ID为0且OrderSide已清空
				isInstantFill = (slot.PositionStatus == PositionStatusEmpty && slot.OrderID == 0 && slot.OrderSide == "" && slot.SlotStatus == SlotStatusFree)
			}

			if !isInstantFill {
				// 正常情况: 更新订单状态
				// 🔥 检查OrderID冲突：只有当ClientOID已设置且不匹配时才是真正的冲突
				// 如果ClientOID为空或匹配，说明是正常的WebSocket先到或批量处理顺序问题
				if slot.OrderID != 0 && slot.OrderID != ord.OrderID {
					if slot.ClientOID != "" && slot.ClientOID != ord.ClientOrderID {
						// 真正的冲突：槽位已被其他订单占用
						logger.Warn("⚠️ [OrderID冲突] 槽位 %.2f: 下单返回OrderID=%d (ClientOID=%s)，但槽位已被OrderID=%d (ClientOID=%s)占用",
							price, ord.OrderID, ord.ClientOrderID, slot.OrderID, slot.ClientOID)
					} else {
						// WebSocket推送先到达，这是正常现象
						logger.Debug("📝 [覆盖OrderID] 槽位 %.2f: WebSocket已设置OrderID=%d，现用下单返回的OrderID=%d (ClientOID: %s)",
							price, slot.OrderID, ord.OrderID, ord.ClientOrderID)
					}
				}

				slot.OrderID = ord.OrderID
				slot.ClientOID = ord.ClientOrderID
				slot.OrderSide = side // "BUY" or "SELL"
				slot.OrderStatus = OrderStatusPlaced
				slot.OrderPrice = ord.Price
				slot.OrderCreatedAt = time.Now()
				// 🔥 订单提交成功，设置为LOCKED状态
				slot.SlotStatus = SlotStatusLocked
				// 注意：不在这里重置PostOnlyFailCount，因为订单可能立即被撤销
				// PostOnly计数只在订单真正成交时重置

				logger.Debug("✅ [实时新增] 槽位价格: %s, %s订单, 订单价格: %s, 订单ID: %d, ClientOID: %s",
					formatPrice(price, spm.priceDecimals), side, formatPrice(ord.Price, spm.priceDecimals), ord.OrderID, ord.ClientOrderID)
			} else {
				// 🔍 秒成交场景：WebSocket已经处理了FILLED,跳过状态更新
				logger.Debug("🔍 [%s单秒成交] 槽位 %s 的订单已被WebSocket处理，跳过状态更新 (持仓: %.4f, SlotStatus: %s)",
					side, formatPrice(price, spm.priceDecimals), slot.PositionQty, slot.SlotStatus)
			}

			slot.mu.Unlock()
		}
	}

	return nil
}

// OnOrderUpdate 订单更新回调（异步订单同步流）
func (spm *SuperPositionManager) OnOrderUpdate(update OrderUpdate) {
	// 🔥 重构：完全依赖 ClientOrderID 解析
	price, side, valid := spm.parseClientOrderID(update.ClientOrderID)

	if !valid {
		logger.Debug("⏳ [忽略] 无法识别的订单更新: ID=%d, ClientOID=%s", update.OrderID, update.ClientOrderID)
		return
	}

	slot := spm.getOrCreateSlot(price)
	slot.mu.Lock()
	defer slot.mu.Unlock()

	// 校验：确保这个更新属于当前的订单 (防止旧订单的延迟推送干扰新订单)
	// 优先使用 ClientOrderID 匹配 (某些交易所如 Gate.io 的 OrderID 可能略有差异)
	if slot.ClientOID != "" && slot.ClientOID != update.ClientOrderID {
		// ClientOrderID 不匹配，忽略此更新
		logger.Info("⚠️ [订单更新被忽略] 槽位 %.2f: ClientOID不匹配 (槽位: %s, 推送: %s, OrderID: %d)",
			price, slot.ClientOID, update.ClientOrderID, update.OrderID)
		return
	}

	// 更新订单ID (如果是首个推送)
	if slot.OrderID == 0 {
		logger.Debug("📝 [首次设置OrderID] 槽位 %.2f: OrderID=%d, ClientOID=%s", price, update.OrderID, update.ClientOrderID)
		slot.OrderID = update.OrderID
		slot.ClientOID = update.ClientOrderID
		slot.OrderSide = side
	} else if slot.OrderID != update.OrderID {
		// OrderID 不一致但 ClientOrderID 匹配，更新 OrderID (Gate.io 批量下单可能出现此情况)
		logger.Debug("📝 [更新OrderID] 槽位 %.2f: %d -> %d (ClientOID: %s)", price, slot.OrderID, update.OrderID, update.ClientOrderID)
		slot.OrderID = update.OrderID
	}

	// 处理状态转换
	switch update.Status {
	case "NEW":
		if slot.OrderStatus == OrderStatusPlaced {
			slot.OrderStatus = OrderStatusConfirmed
		}

	case "PARTIALLY_FILLED", "FILLED":
		// 计算增量
		deltaQty := update.ExecutedQty - slot.OrderFilledQty
		if deltaQty < 0 {
			deltaQty = 0
		}

		slot.OrderFilledQty = update.ExecutedQty

		// 根据方向更新持仓
		if side == "BUY" {
			if deltaQty > 0 {
				slot.PositionQty += deltaQty
				// 累加统计
				oldTotal := spm.totalBuyQty.Load().(float64)
				spm.totalBuyQty.Store(oldTotal + deltaQty)
			}

			if update.Status == "FILLED" {
				slot.OrderStatus = OrderStatusNotPlaced // 重置订单状态
				slot.OrderID = 0
				slot.ClientOID = ""
				slot.OrderSide = "" // 🔥 清除订单方向，避免误判
				slot.OrderFilledQty = 0

				slot.PositionStatus = PositionStatusFilled // 标记为有仓
				// 🔥 释放槽位锁：买单成交，允许后续挂卖单
				slot.SlotStatus = SlotStatusFree
				// 🔥 买单成交，重置PostOnly失败计数
				slot.PostOnlyFailCount = 0
				
				// 🔥 释放资金：买单成交后，资金已转换为持仓，释放预留的资金
				orderValue := slot.OrderPrice * update.ExecutedQty
				actualMargin := spm.getActualMargin(orderValue)
				if actualMargin > 0 {
					spm.allocationManager.Release(spm.exchangeName, spm.config.Trading.Symbol, actualMargin)
					logger.Debug("💰 [资金释放] 买单成交，释放资金: %.2f USDT (订单价值: %.2f USDT)", actualMargin, orderValue)
				}
				
				logger.Info("✅ [买单成交] 价格: %s, 持仓: %.4f, 槽位状态: %s -> %s, 订单状态: %s -> %s, SlotStatus: FREE",
					formatPrice(price, spm.priceDecimals), slot.PositionQty,
					PositionStatusEmpty, PositionStatusFilled,
					"FILLED", OrderStatusNotPlaced)
				logger.Debug("🔍 [买单成交后] 等待下次AdjustOrders调用时挂出卖单...")
			} else {
				slot.OrderStatus = OrderStatusPartiallyFilled
			}

		} else { // SELL
			if deltaQty > 0 {
				slot.PositionQty -= deltaQty
				if slot.PositionQty < 0 {
					slot.PositionQty = 0
				}
				// 累加统计
				oldTotal := spm.totalSellQty.Load().(float64)
				spm.totalSellQty.Store(oldTotal + deltaQty)

				// 🔥 保存交易记录（买卖配对完成）
				if spm.tradeStorage != nil {
					// 买入价格就是槽位的价格（每个槽位对应一个买入价格点）
					buyPrice := slot.Price
					// 卖出价格使用成交均价，如果没有则使用订单价格
					sellPrice := update.AvgPrice
					if sellPrice <= 0 {
						sellPrice = update.Price
					}
					if sellPrice <= 0 {
						sellPrice = slot.OrderPrice
					}

					// 🔥 验证价格和数量的合理性
					if buyPrice <= 0 || sellPrice <= 0 || deltaQty <= 0 {
						logger.Warn("⚠️ [交易记录异常] 买入价: %.2f, 卖出价: %.2f, 数量: %.4f, 跳过保存",
							buyPrice, sellPrice, deltaQty)
					} else {
						// 计算盈亏：(卖出价格 - 买入价格) * 数量
						// 注意：对于USDT本位合约（如BTCUSDT），价格是USDT，数量是BTC，盈亏单位是USDT
						pnl := (sellPrice - buyPrice) * deltaQty

						// 🔥 添加合理性检查：如果盈亏异常大，记录警告
						// 对于BTCUSDT，如果价格差是100 USDT，数量是0.01 BTC，盈亏应该是1 USDT
						// 如果盈亏超过订单金额的50%，可能是计算错误
						orderAmount := buyPrice * deltaQty
						if orderAmount > 0 && math.Abs(pnl) > orderAmount*0.5 {
							logger.Warn("⚠️ [盈亏异常] 买入价: %.2f, 卖出价: %.2f, 数量: %.4f, 盈亏: %.2f, 订单金额: %.2f, 盈亏率: %.2f%%",
								buyPrice, sellPrice, deltaQty, pnl, orderAmount, (pnl/orderAmount)*100)
						}

						// 保存交易记录（买入订单ID设为0，因为无法追溯历史订单）
						buyOrderID := int64(0)
						sellOrderID := update.OrderID
						if err := spm.tradeStorage.SaveTrade(buyOrderID, sellOrderID, spm.exchangeName, update.Symbol, buyPrice, sellPrice, deltaQty, pnl, time.Now()); err != nil {
							logger.Warn("⚠️ 保存交易记录失败: %v", err)
						} else {
							logger.Debug("💰 [交易记录已保存] 买入价: %s, 卖出价: %s, 数量: %.4f, 盈亏: %.4f",
								formatPrice(buyPrice, spm.priceDecimals), formatPrice(sellPrice, spm.priceDecimals), deltaQty, pnl)
						}
					}
				}
			}

			if update.Status == "FILLED" {
				slot.OrderStatus = OrderStatusNotPlaced // 重置订单状态
				slot.OrderID = 0
				slot.ClientOID = ""
				slot.OrderSide = "" // 🔥 清除订单方向，避免误判
				slot.OrderFilledQty = 0

				if slot.PositionQty < 0.000001 {
					slot.PositionStatus = PositionStatusEmpty // 标记为空仓
				}
				// 🔥 释放槽位锁：卖单成交，允许后续挂买单
				slot.SlotStatus = SlotStatusFree
				// 🔥 卖单成交，重置PostOnly失败计数
				slot.PostOnlyFailCount = 0
				
				// 🔥 释放资金：卖单成交后，资金已收回，释放预留的资金（卖单不需要预留资金，但为了统一处理也释放）
				// 注意：卖单是平仓，不占用资金，但为了保持一致性，这里也处理
				// 卖单成交后，持仓减少，对应的买入资金应该被释放
				// 使用槽位价格（买入价）计算释放金额
				releaseValue := price * deltaQty
				actualMargin := spm.getActualMargin(releaseValue)
				if actualMargin > 0 {
					spm.allocationManager.Release(spm.exchangeName, spm.config.Trading.Symbol, actualMargin)
					logger.Debug("💰 [资金释放] 卖单成交，释放资金: %.2f USDT (持仓价值: %.2f USDT, 持仓减少: %.4f)", actualMargin, releaseValue, deltaQty)
				}
				
				logger.Info("✅ [卖单成交] 价格: %s, 剩余持仓: %.4f, 槽位状态: %s, 订单状态: %s, SlotStatus: FREE",
					formatPrice(price, spm.priceDecimals), slot.PositionQty, slot.PositionStatus, slot.OrderStatus)
			} else {
				slot.OrderStatus = OrderStatusPartiallyFilled
			}
		}

	case "CANCELED", "EXPIRED", "REJECTED":
		logger.Info("⚠️ [订单%s] 价格: %s, 方向: %s, 原因: %s, 已成交: %.4f",
			update.Status, formatPrice(price, spm.priceDecimals), side, update.Status, slot.OrderFilledQty)

		// 🔥 释放资金：订单取消后，释放未成交部分的预留资金
		// 注意：买单取消时，如果未成交，需要释放整个订单的预留资金
		// 由于我们不知道原始订单数量，使用订单价格和配置的订单金额来估算
		if side == "BUY" && slot.OrderPrice > 0 {
			// 对于买单，如果未成交或部分成交，释放未成交部分的资金
			// 使用配置的订单金额作为参考（因为每个槽位的订单金额是固定的）
			orderValue := spm.config.Trading.OrderQuantity
			if slot.OrderFilledQty > 0 {
				// 部分成交：释放未成交部分的资金
				filledValue := slot.OrderPrice * slot.OrderFilledQty
				unfilledValue := orderValue - filledValue
				actualMargin := spm.getActualMargin(unfilledValue)
				if actualMargin > 0 {
					spm.allocationManager.Release(spm.exchangeName, spm.config.Trading.Symbol, actualMargin)
					logger.Debug("💰 [资金释放] 买单部分成交后取消，释放未成交资金: %.2f USDT (订单价值: %.2f USDT, 已成交: %.4f)", 
						actualMargin, unfilledValue, slot.OrderFilledQty)
				}
			} else {
				// 完全未成交：释放整个订单的预留资金
				actualMargin := spm.getActualMargin(orderValue)
				if actualMargin > 0 {
					spm.allocationManager.Release(spm.exchangeName, spm.config.Trading.Symbol, actualMargin)
					logger.Debug("💰 [资金释放] 买单未成交取消，释放资金: %.2f USDT (订单价值: %.2f USDT)", actualMargin, orderValue)
				}
			}
		}
		// 卖单取消不需要释放资金，因为卖单是平仓，不占用资金

		// 🔥 核心修复：根据订单方向和成交情况处理槽位状态
		if side == "BUY" {
			// 买单被取消/拒绝
			if slot.PositionQty > 0 || slot.OrderFilledQty > 0 {
				// 部分成交后被取消：保留持仓，允许后续挂卖单
				logger.Info("💡 [买单部分成交后取消] 价格: %s, 持仓: %.4f, 转为有仓状态",
					formatPrice(price, spm.priceDecimals), slot.PositionQty)
				slot.PositionStatus = PositionStatusFilled
				slot.SlotStatus = SlotStatusFree // 允许挂卖单
			} else {
				// 完全未成交被取消：重置为空槽位
				logger.Info("🔄 [买单未成交取消] 价格: %s, 重置槽位为空闲",
					formatPrice(price, spm.priceDecimals))
				slot.PositionStatus = PositionStatusEmpty
				slot.SlotStatus = SlotStatusFree // 允许重新挂买单
			}
		} else if side == "SELL" {
			// 卖单被取消/拒绝：应该还持有币，保持持仓状态
			if slot.PositionQty > 0 {
				// 增加PostOnly失败计数（订单被交易所撤销通常是PostOnly失败）
				slot.PostOnlyFailCount++
				logger.Info("🔄 [卖单取消] 价格: %s, 保持持仓状态: %.4f, 等待重挂, PostOnly失败计数: %d",
					formatPrice(price, spm.priceDecimals), slot.PositionQty, slot.PostOnlyFailCount)
				slot.PositionStatus = PositionStatusFilled
				slot.SlotStatus = SlotStatusFree // 允许重新挂卖单
			} else {
				// 异常情况：卖单取消但没有持仓，重置为空
				logger.Warn("⚠️ [异常] 卖单取消但无持仓，价格: %s, 重置为空",
					formatPrice(price, spm.priceDecimals))
				slot.PositionStatus = PositionStatusEmpty
				slot.SlotStatus = SlotStatusFree
			}
		}

		// 清空订单信息
		slot.OrderStatus = OrderStatusCanceled
		slot.OrderID = 0
		slot.ClientOID = ""
		slot.OrderFilledQty = 0
		// 保留 OrderSide 用于日志调试
	}
}

// getOrCreateSlot 获取或创建槽位
func (spm *SuperPositionManager) getOrCreateSlot(price float64) *InventorySlot {
	if slot, exists := spm.slots.Load(price); exists {
		return slot.(*InventorySlot)
	}

	// 创建新槽位
	slot := &InventorySlot{
		Price:          price,
		PositionStatus: PositionStatusEmpty,
		PositionQty:    0,
		OrderStatus:    OrderStatusNotPlaced,
		SlotStatus:     SlotStatusFree, // 🔥 初始化为FREE状态
	}
	spm.slots.Store(price, slot)
	return slot
}

// findNearestGridPrice 找到最近的网格价格
// 根据当前价格动态计算最近的网格对齐价格
func (spm *SuperPositionManager) findNearestGridPrice(currentPrice float64) float64 {
	// 计算当前价格相对于锚点的偏移量
	offset := currentPrice - spm.anchorPrice
	// 计算离当前价格最近的网格间隔数（四舍五入）
	intervals := math.Round(offset / spm.config.Trading.PriceInterval)
	// 计算最近的网格价格
	gridPrice := spm.anchorPrice + intervals*spm.config.Trading.PriceInterval
	// 使用检测到的价格精度进行舍入
	return roundPrice(gridPrice, spm.priceDecimals)
}

// calculateSlotPrices 计算槽位价格列表（统一的网格计算方法）
// 这个方法确保初始化和实时调整计算出完全相同的槽位价格
// 参数：
//   - gridPrice: 网格价格（使用锚点价格）
//   - count: 需要计算的槽位数量
//   - direction: 方向，"down"表示向下（买单），"up"表示向上（卖单）
//
// 返回：槽位价格列表，从网格价格开始，按价格间隔递减或递增，使用检测到的价格精度
func (spm *SuperPositionManager) calculateSlotPrices(gridPrice float64, count int, direction string) []float64 {
	var prices []float64
	priceInterval := spm.config.Trading.PriceInterval

	for i := 0; i < count; i++ {
		var price float64
		if direction == "down" {
			// 向下：网格价格 - i * 间隔
			price = gridPrice - float64(i)*priceInterval
		} else {
			// 向上：网格价格 + i * 间隔
			price = gridPrice + float64(i)*priceInterval
		}
		// 使用检测到的价格精度进行舍入
		price = roundPrice(price, spm.priceDecimals)
		
		// 验证价格有效性：跳过无效价格（负数或零）
		if price <= 0 {
			logger.Warn("⚠️ [%s:%s] 跳过无效槽位价格 %.8f（方向=%s, 索引=%d, 网格价格=%.2f, 间隔=%.4f）",
				spm.exchangeName, spm.config.Trading.Symbol, price, direction, i, gridPrice, priceInterval)
			continue
		}
		
		prices = append(prices, price)
	}

	return prices
}

// ===== IPositionManager 接口实现（供 safety.Reconciler 使用）=====
// 注意：以下方法是 safety/reconciler.go 中 IPositionManager 接口的实现，
// 被 Reconciler 对账器调用，不可删除或修改签名

// SlotData 槽位数据结构（用于传递给外部）
type SlotData struct {
	Price          float64
	PositionStatus string
	PositionQty    float64
	OrderID        int64
	OrderSide      string
	OrderStatus    string
	OrderCreatedAt time.Time
}

// IterateSlots 遍历所有槽位（封装 sync.Map.Range）
// 注意：为了避免类型冲突，这里使用 interface{} 返回槽位数据
// 调用者需要将其转换为具体的槽位信息
func (spm *SuperPositionManager) IterateSlots(fn func(price float64, slot interface{}) bool) {
	spm.slots.Range(func(key, value interface{}) bool {
		price := key.(float64)
		slot := value.(*InventorySlot)
		slot.mu.RLock()
		defer slot.mu.RUnlock()

		// 构造槽位数据
		data := SlotData{
			Price:          price,
			PositionStatus: slot.PositionStatus,
			PositionQty:    slot.PositionQty,
			OrderID:        slot.OrderID,
			OrderSide:      slot.OrderSide,
			OrderStatus:    slot.OrderStatus,
			OrderCreatedAt: slot.OrderCreatedAt,
		}

		// 返回槽位数据
		return fn(price, data)
	})
}

// DetailedSlotData 详细槽位数据结构（包含所有字段）
type DetailedSlotData struct {
	Price          float64
	PositionStatus string
	PositionQty    float64
	OrderID        int64
	ClientOID      string
	OrderSide      string
	OrderStatus    string
	OrderPrice     float64
	OrderFilledQty float64
	OrderCreatedAt time.Time
	SlotStatus     string
}

// GetAllSlotsDetailed 获取所有槽位的详细信息
// 注意：如果槽位数量很大，建议使用分页查询或限制数量
func (spm *SuperPositionManager) GetAllSlotsDetailed() []DetailedSlotData {
	// 限制最大返回数量，防止内存占用过大
	maxSlots := 10000 // 最多返回1万个槽位
	var slots []DetailedSlotData
	count := 0
	
	spm.slots.Range(func(key, value interface{}) bool {
		if count >= maxSlots {
			logger.Warn("⚠️ [槽位查询] 槽位数量超过限制 (%d)，只返回前 %d 个", maxSlots, maxSlots)
			return false // 停止遍历
		}
		
		price := key.(float64)
		slot := value.(*InventorySlot)
		slot.mu.RLock()

		slots = append(slots, DetailedSlotData{
			Price:          price,
			PositionStatus: slot.PositionStatus,
			PositionQty:    slot.PositionQty,
			OrderID:        slot.OrderID,
			ClientOID:      slot.ClientOID,
			OrderSide:      slot.OrderSide,
			OrderStatus:    slot.OrderStatus,
			OrderPrice:     slot.OrderPrice,
			OrderFilledQty: slot.OrderFilledQty,
			OrderCreatedAt: slot.OrderCreatedAt,
			SlotStatus:     slot.SlotStatus,
		})

		slot.mu.RUnlock()
		count++
		return true
	})
	return slots
}

// GetSlotCount 获取槽位总数
func (spm *SuperPositionManager) GetSlotCount() int {
	count := 0
	spm.slots.Range(func(key, value interface{}) bool {
		count++
		return true
	})
	return count
}

// GetTotalBuyQty 获取累计买入数量（IPositionManager 接口方法，供 Reconciler 使用）
func (spm *SuperPositionManager) GetTotalBuyQty() float64 {
	return spm.totalBuyQty.Load().(float64)
}

// GetTotalSellQty 获取累计卖出数量（IPositionManager 接口方法，供 Reconciler 使用）
func (spm *SuperPositionManager) GetTotalSellQty() float64 {
	return spm.totalSellQty.Load().(float64)
}

// GetReconcileCount 获取对账次数（IPositionManager 接口方法，供 Reconciler 使用）
func (spm *SuperPositionManager) GetReconcileCount() int64 {
	return spm.reconcileCount.Load()
}

// IncrementReconcileCount 增加对账次数（IPositionManager 接口方法，供 Reconciler 使用）
func (spm *SuperPositionManager) IncrementReconcileCount() {
	spm.reconcileCount.Add(1)
}

// UpdateLastReconcileTime 更新最后对账时间（IPositionManager 接口方法，供 Reconciler 使用）
func (spm *SuperPositionManager) UpdateLastReconcileTime(t time.Time) {
	spm.lastReconcileTime.Store(t)
}

// GetLastReconcileTime 获取最后对账时间
func (spm *SuperPositionManager) GetLastReconcileTime() time.Time {
	v := spm.lastReconcileTime.Load()
	if v == nil {
		return time.Time{}
	}
	return v.(time.Time)
}

// GetSymbol 获取交易符号
func (spm *SuperPositionManager) GetSymbol() string {
	return spm.config.Trading.Symbol
}

// GetExchange 获取交易所名称
func (spm *SuperPositionManager) GetExchange() string {
	return spm.exchangeName
}

// GetPriceInterval 获取价格间隔
func (spm *SuperPositionManager) GetPriceInterval() float64 {
	return spm.config.Trading.PriceInterval
}

// GetAnchorPrice 获取价格锚点
func (spm *SuperPositionManager) GetAnchorPrice() float64 {
	return spm.anchorPrice
}

// GetLeverage 获取杠杆倍数（用于计算实际资金占用）
func (spm *SuperPositionManager) GetLeverage() int {
	leverage := 1 // 默认1倍（无杠杆）
	ctx := context.Background()
	
	// 先尝试从账户信息中的持仓获取杠杆倍数
	if accountResult, err := spm.exchange.GetAccount(ctx); err == nil && accountResult != nil {
		accountValue := reflect.ValueOf(accountResult)
		if accountValue.Kind() == reflect.Ptr {
			accountValue = accountValue.Elem()
		}
		// 尝试从 Account.Positions 字段获取持仓信息
		if positionsField := accountValue.FieldByName("Positions"); positionsField.IsValid() && positionsField.CanInterface() {
			positionsValue := reflect.ValueOf(positionsField.Interface())
			if positionsValue.Kind() == reflect.Slice {
				for i := 0; i < positionsValue.Len(); i++ {
					posValue := positionsValue.Index(i)
					if posValue.Kind() == reflect.Ptr {
						posValue = posValue.Elem()
					} else if posValue.Kind() == reflect.Interface {
						posValue = posValue.Elem()
					}
					// 检查 Symbol 是否匹配
					if symbolField := posValue.FieldByName("Symbol"); symbolField.IsValid() && symbolField.CanInterface() {
						if symbol, ok := symbolField.Interface().(string); ok && symbol == spm.config.Trading.Symbol {
							// 尝试获取 Leverage 字段
							if leverageField := posValue.FieldByName("Leverage"); leverageField.IsValid() && leverageField.CanInterface() {
								if lev, ok := leverageField.Interface().(int); ok && lev > 0 {
									leverage = lev
									break
								}
							}
						}
					}
				}
			}
		}
		// 如果从持仓中获取不到，尝试从账户级别的杠杆字段获取
		if leverage == 1 {
			if leverageField := accountValue.FieldByName("AccountLeverage"); leverageField.IsValid() && leverageField.CanInterface() {
				if lev, ok := leverageField.Interface().(int); ok && lev > 0 {
					leverage = lev
				}
			}
		}
	}
	
	// 如果从账户中获取不到，尝试从 GetPositions 获取
	if leverage == 1 {
		if positionsInterface, err := spm.exchange.GetPositions(ctx, spm.config.Trading.Symbol); err == nil && positionsInterface != nil {
			// 使用反射处理不同类型的持仓信息
			positionsValue := reflect.ValueOf(positionsInterface)
			if positionsValue.Kind() == reflect.Slice {
				for i := 0; i < positionsValue.Len(); i++ {
					posValue := positionsValue.Index(i)
					if posValue.Kind() == reflect.Ptr {
						posValue = posValue.Elem()
					} else if posValue.Kind() == reflect.Interface {
						posValue = posValue.Elem()
					}
					// 尝试获取 Leverage 字段
					if leverageField := posValue.FieldByName("Leverage"); leverageField.IsValid() && leverageField.CanInterface() {
						if lev, ok := leverageField.Interface().(int); ok && lev > 0 {
							leverage = lev
							break
						}
					}
				}
			}
		}
	}
	
	return leverage
}

// RestoreReconciliationStats 从数据库恢复对账统计值
// storage 是对账存储接口，exchange/symbol 用于精确定位历史记录
func (spm *SuperPositionManager) RestoreReconciliationStats(storage ReconciliationStorage, exchange, symbol string) error {
	if storage == nil {
		return nil // 存储服务不可用，不报错
	}

	// 1. 获取最新对账记录
	latestHistoryInterface, err := storage.GetLatestReconciliationHistory(exchange, symbol)
	if err != nil {
		return fmt.Errorf("获取最新对账记录失败: %w", err)
	}

	// 2. 获取对账次数
	reconcileCount, err := storage.GetReconciliationCount(exchange, symbol)
	if err != nil {
		return fmt.Errorf("获取对账次数失败: %w", err)
	}

	// 3. 如果没有历史记录，不恢复（保持默认值）
	if latestHistoryInterface == nil {
		logger.Info("📊 [对账恢复] 未找到历史对账记录，使用默认值")
		return nil
	}

	// 4. 使用反射提取对账记录字段
	v := reflect.ValueOf(latestHistoryInterface)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return fmt.Errorf("对账记录类型错误: %T", latestHistoryInterface)
	}

	// 提取字段的辅助函数
	getFloat64Field := func(name string) float64 {
		field := v.FieldByName(name)
		if field.IsValid() && field.CanFloat() {
			return field.Float()
		}
		return 0.0
	}

	getTimeField := func(name string) time.Time {
		field := v.FieldByName(name)
		if field.IsValid() && field.Kind() == reflect.Interface {
			if t, ok := field.Interface().(time.Time); ok {
				return t
			}
		} else if field.IsValid() && field.Type().String() == "time.Time" {
			if t, ok := field.Interface().(time.Time); ok {
				return t
			}
		}
		return time.Time{}
	}

	// 5. 恢复统计值
	totalBuyQty := getFloat64Field("TotalBuyQty")
	totalSellQty := getFloat64Field("TotalSellQty")
	lastReconcileTime := getTimeField("ReconcileTime")

	spm.totalBuyQty.Store(totalBuyQty)
	spm.totalSellQty.Store(totalSellQty)
	spm.reconcileCount.Store(reconcileCount)
	spm.lastReconcileTime.Store(lastReconcileTime)

	logger.Info("✅ [对账恢复] 已恢复对账统计: 次数=%d, 累计买入=%.4f, 累计卖出=%.4f, 最后对账时间=%s",
		reconcileCount, totalBuyQty, totalSellQty, lastReconcileTime.Format("2006-01-02 15:04:05"))

	return nil
}

// ===== 订单清理功能已迁移到 safety.OrderCleaner =====
// StartOrderCleanup 和 cleanupOrders 方法已移至 safety/order_cleaner.go

// UpdateSlotOrderStatus 更新槽位订单状态（供 OrderCleaner 使用）
func (spm *SuperPositionManager) UpdateSlotOrderStatus(price float64, status string) {
	slot := spm.getOrCreateSlot(price)
	slot.mu.Lock()
	slot.OrderStatus = status
	slot.mu.Unlock()
}

// CancelAllBuyOrders 撤销所有买单（风控触发时使用）
func (spm *SuperPositionManager) CancelAllBuyOrders() {
	var buyOrderIDs []int64
	var buyPrices []float64

	// 🔥 修复：收集所有OrderID>0且OrderSide=BUY的订单，不管OrderStatus
	spm.slots.Range(func(key, value interface{}) bool {
		price := key.(float64)
		slot := value.(*InventorySlot)

		slot.mu.RLock()
		if slot.OrderSide == "BUY" && slot.OrderID > 0 {
			buyOrderIDs = append(buyOrderIDs, slot.OrderID)
			buyPrices = append(buyPrices, price)
		}
		slot.mu.RUnlock()
		return true
	})

	if len(buyOrderIDs) == 0 {
		return
	}

	logger.Info("🔄 [撤销买单] 准备撤销 %d 个买单以释放保证金", len(buyOrderIDs))

	// 🔥 重复尝试3次，确保撤单干净
	for attempt := 1; attempt <= 3; attempt++ {
		if len(buyOrderIDs) == 0 {
			break
		}

		logger.Info("🔄 [撤销买单] 第 %d 次尝试，剩余 %d 个订单", attempt, len(buyOrderIDs))

		if err := spm.executor.BatchCancelOrders(buyOrderIDs); err != nil {
			logger.Error("❌ [撤销买单] 批量撤单失败: %v", err)
		}

		// 更新槽位状态
		for _, price := range buyPrices {
			slot := spm.getOrCreateSlot(price)
			slot.mu.Lock()
			slot.OrderStatus = OrderStatusCancelRequested
			slot.mu.Unlock()
		}

		// 等待2秒让撤单生效（WebSocket推送通知）
		time.Sleep(2 * time.Second)

		// 🔥 二次检查：重新扫描本地槽位状态
		if attempt < 3 {
			buyOrderIDs = nil
			buyPrices = nil

			spm.slots.Range(func(key, value interface{}) bool {
				price := key.(float64)
				slot := value.(*InventorySlot)

				slot.mu.RLock()
				// 如果OrderStatus不是CANCELED且OrderID>0，说明可能还有残留
				if slot.OrderSide == "BUY" && slot.OrderID > 0 &&
					slot.OrderStatus != OrderStatusCanceled {
					buyOrderIDs = append(buyOrderIDs, slot.OrderID)
					buyPrices = append(buyPrices, price)
				}
				slot.mu.RUnlock()
				return true
			})

			if len(buyOrderIDs) > 0 {
				logger.Warn("⚠️ [撤销买单] 检测到 %d 个残留买单，继续清理", len(buyOrderIDs))
			} else {
				logger.Info("✅ [撤销买单] 所有买单已清理完成")
				break
			}
		}
	}

	logger.Info("✅ [撤销买单] 清理完成")
}

// LiquidateAll 全平仓位（风控或止损触发时使用）
func (spm *SuperPositionManager) LiquidateAll() {
	logger.Warn("🚨 [全平仓] 正在执行全平操作，撤销所有买单并市价平仓所有持仓...")

	// 1. 撤销所有买单
	spm.CancelAllBuyOrders()

	// 2. 收集所有持仓槽位并提交卖单
	var sellOrders []*OrderRequest
	spm.slots.Range(func(key, value interface{}) bool {
		price := key.(float64)
		slot := value.(*InventorySlot)

		slot.mu.Lock()
		if slot.PositionStatus == PositionStatusFilled && slot.PositionQty > 0 {
			// 如果已有订单，先尝试撤销
			if slot.OrderID > 0 {
				logger.Info("🔄 [全平仓] 撤销槽位 %s 的现有订单 %d", formatPrice(price, spm.priceDecimals), slot.OrderID)
				spm.executor.BatchCancelOrders([]int64{slot.OrderID})
			}

			// 标记为 PENDING
			slot.SlotStatus = SlotStatusPending

			// 构建卖单（使用当前市价或略低于市价的价格以确保成交，这里简单使用当前锚点价格附近的卖出逻辑）
			// 实际上由于是全平，最好的方式是下市价单或极优价格的限价单
			// 这里复用 AdjustOrders 中的逻辑，使用槽位价格加一个间隔作为卖价，或者根据当前价格调整
			
			// 获取最后价格
			lastPrice, _ := spm.lastMarketPrice.Load().(float64)
			if lastPrice <= 0 {
				lastPrice = price
			}

			sellPrice := lastPrice * 0.99 // 使用略低于市价的价格确保成交（限价平仓）
			sellPrice = roundPrice(sellPrice, spm.priceDecimals)

			clientOID := spm.generateClientOrderID(price, "SELL")

			sellOrders = append(sellOrders, &OrderRequest{
				Symbol:        spm.config.Trading.Symbol,
				Side:          "SELL",
				Price:         sellPrice,
				Quantity:      slot.PositionQty,
				PriceDecimals: spm.priceDecimals,
				ReduceOnly:    true,
				PostOnly:      false, // 强制平仓不使用 PostOnly
				ClientOrderID: clientOID,
			})
		}
		slot.mu.Unlock()
		return true
	})

	if len(sellOrders) > 0 {
		logger.Info("🔄 [全平仓] 提交 %d 个平仓卖单", len(sellOrders))
		result := spm.executor.BatchPlaceOrdersWithDetails(sellOrders)
		
		// 更新槽位状态
		for _, ord := range result.PlacedOrders {
			price, _, valid := spm.parseClientOrderID(ord.ClientOrderID)
			if valid {
				slot := spm.getOrCreateSlot(price)
				slot.mu.Lock()
				slot.OrderID = ord.OrderID
				slot.ClientOID = ord.ClientOrderID
				slot.OrderSide = "SELL"
				slot.OrderStatus = OrderStatusPlaced
				slot.SlotStatus = SlotStatusLocked
				slot.mu.Unlock()
			}
		}
	} else {
		logger.Info("ℹ️ [全平仓] 没有发现需要平仓的持仓")
	}
}

// ===== 对账功能已迁移到 safety.Reconciler =====
// StartReconciliation 和 Reconcile 方法已移至 safety/reconciler.go
// SetPauseChecker 也已移至 Reconciler

// CancelAllOrders 撤销所有订单（退出时使用）
// 委托给交易所适配器实现具体逻辑
func (spm *SuperPositionManager) CancelAllOrders() {
	ctx := context.Background()
	if err := spm.exchange.CancelAllOrders(ctx, spm.config.Trading.Symbol); err != nil {
		logger.Error("❌ [%s] 撤销所有订单失败: %v", spm.exchange.GetName(), err)
	} else {
		logger.Info("✅ [%s] 撤销所有订单完成", spm.exchange.GetName())
	}
}

// getExistingPosition 获取当前持仓数量（容错处理）
func (spm *SuperPositionManager) getExistingPosition() float64 {
	ctx := context.Background()
	positionsInterface, err := spm.exchange.GetPositions(ctx, spm.config.Trading.Symbol)
	if err != nil || positionsInterface == nil {
		logger.Debug("🔍 [持仓恢复] 无法获取持仓信息: %v", err)
		return 0
	}

	// 尝试类型断言 - 假设返回的是包含 Size 字段的结构体切片
	// 我们使用反射来安全地提取持仓数量
	switch positions := positionsInterface.(type) {
	case []*PositionInfo:
		// PositionInfo 切片（简化版）
		for _, pos := range positions {
			if pos != nil && pos.Symbol == spm.config.Trading.Symbol {
				logger.Debug("🔍 [持仓恢复] 找到持仓 (PositionInfo): %.4f", pos.Size)
				return pos.Size
			}
		}
	case []interface{}:
		// 通用接口数组 - 尝试解析为持仓结构
		for _, pos := range positions {
			// 尝试直接类型断言为 PositionInfo
			if posInfo, ok := pos.(*PositionInfo); ok {
				if posInfo.Symbol == spm.config.Trading.Symbol {
					logger.Debug("🔍 [持仓恢复] 找到持仓 (interface->PositionInfo): %.4f", posInfo.Size)
					return posInfo.Size
				}
			}
			// 尝试解析为 map
			if posMap, ok := pos.(map[string]interface{}); ok {
				if symbol, ok := posMap["Symbol"].(string); ok && symbol == spm.config.Trading.Symbol {
					if size, ok := posMap["Size"].(float64); ok {
						logger.Debug("🔍 [持仓恢复] 找到持仓 (map): %.4f", size)
						return size
					}
				}
			}
		}
	default:
		// 其他情况：使用反射尝试提取 Size 字段
		logger.Debug("🔍 [持仓恢复] 持仓类型: %T，尝试使用反射提取", positionsInterface)
		// 尝试使用反射处理未知类型
		// 注意：实际上 exchange 返回的是 []*exchange.Position，但因为接口返回 interface{}，所以需要特殊处理
		return 0
	}

	logger.Debug("🔍 [持仓恢复] 未找到匹配的持仓")
	return 0
}

// ForceSyncPositions 强制同步持仓（当对账发现重大不一致时调用）
func (spm *SuperPositionManager) ForceSyncPositions(exchangePosition float64) {
	// 注意：这里不需要全局锁 spm.mu.Lock()，因为 slots 是 sync.Map，槽位更新有自己的锁
	// 且我们不希望在对账时阻塞下单逻辑

	logger.Warn("🚨 [强制同步] 正在同步持仓状态，期望持仓: %.4f", exchangePosition)

	if exchangePosition <= 0.000001 {
		// 交易所持仓为空，清空本地所有槽位的持仓
		count := 0
		spm.slots.Range(func(key, value interface{}) bool {
			slot := value.(*InventorySlot)
			slot.mu.Lock()
			if slot.PositionStatus == PositionStatusFilled {
				logger.Info("🧹 [强制同步] 清空槽位价格 %s 的持仓 (原数量: %.4f)", 
					formatPrice(slot.Price, spm.priceDecimals), slot.PositionQty)
				slot.PositionStatus = PositionStatusEmpty
				slot.PositionQty = 0
				slot.OrderID = 0
				slot.OrderStatus = OrderStatusNotPlaced
				slot.ClientOID = ""
				count++
			}
			slot.mu.Unlock()
			return true
		})
		
		if count > 0 {
			logger.Info("✅ [强制同步] 已成功清空 %d 个槽位的持仓数据", count)
		} else {
			logger.Debug("ℹ️ [强制同步] 本地本来就没有持仓，无需操作")
		}
	} else {
		// 如果交易所仍有持仓，目前只支持在启动时通过 initializeSellSlotsFromPosition 恢复
		// 在线动态同步逻辑较为复杂（涉及槽位重新分配），暂时仅提示
		logger.Warn("⚠️ [强制同步] 交易所仍有持仓 %.4f，暂不支持在线自动同步（非零持仓），请手动检查或重启程序", exchangePosition)
	}
}

// initializeSellSlotsFromPosition 从现有持仓初始化卖单槽位（用于程序重启后恢复状态）
func (spm *SuperPositionManager) initializeSellSlotsFromPosition(totalPosition float64) {
	if totalPosition <= 0 {
		return
	}

	// 0. 获取杠杆倍数（用于计算实际使用的保证金）
	leverage := 1 // 默认1倍（无杠杆）
	ctx := context.Background()
	
	// 先尝试从账户信息中的持仓获取杠杆倍数（GetAccount 返回的持仓信息通常包含杠杆）
	if accountResult, err := spm.exchange.GetAccount(ctx); err == nil && accountResult != nil {
		accountValue := reflect.ValueOf(accountResult)
		if accountValue.Kind() == reflect.Ptr {
			accountValue = accountValue.Elem()
		}
		// 尝试从 Account.Positions 字段获取持仓信息
		if positionsField := accountValue.FieldByName("Positions"); positionsField.IsValid() && positionsField.CanInterface() {
			positionsValue := reflect.ValueOf(positionsField.Interface())
			if positionsValue.Kind() == reflect.Slice {
				for i := 0; i < positionsValue.Len(); i++ {
					posValue := positionsValue.Index(i)
					if posValue.Kind() == reflect.Ptr {
						posValue = posValue.Elem()
					} else if posValue.Kind() == reflect.Interface {
						posValue = posValue.Elem()
					}
					// 检查 Symbol 是否匹配
					if symbolField := posValue.FieldByName("Symbol"); symbolField.IsValid() && symbolField.CanInterface() {
						if symbol, ok := symbolField.Interface().(string); ok && symbol == spm.config.Trading.Symbol {
							// 尝试获取 Leverage 字段
							if leverageField := posValue.FieldByName("Leverage"); leverageField.IsValid() && leverageField.CanInterface() {
								if lev, ok := leverageField.Interface().(int); ok && lev > 0 {
									leverage = lev
									logger.Debug("🔍 [持仓恢复] 从账户持仓信息中获取到杠杆倍数: %dx", leverage)
									break
								}
							}
						}
					}
				}
			}
		}
		// 如果从持仓中获取不到，尝试从账户级别的杠杆字段获取
		if leverage == 1 {
			if leverageField := accountValue.FieldByName("AccountLeverage"); leverageField.IsValid() && leverageField.CanInterface() {
				if lev, ok := leverageField.Interface().(int); ok && lev > 0 {
					leverage = lev
					logger.Debug("🔍 [持仓恢复] 从账户级别获取到杠杆倍数: %dx", leverage)
				}
			}
		}
	}
	
	// 如果从账户中获取不到，尝试从 GetPositions 获取
	if leverage == 1 {
		if positionsInterface, err := spm.exchange.GetPositions(ctx, spm.config.Trading.Symbol); err == nil && positionsInterface != nil {
			// 使用反射处理不同类型的持仓信息
			positionsValue := reflect.ValueOf(positionsInterface)
			if positionsValue.Kind() == reflect.Slice {
				for i := 0; i < positionsValue.Len(); i++ {
					posValue := positionsValue.Index(i)
					if posValue.Kind() == reflect.Ptr {
						posValue = posValue.Elem()
					} else if posValue.Kind() == reflect.Interface {
						posValue = posValue.Elem()
					}
					// 尝试获取 Leverage 字段
					if leverageField := posValue.FieldByName("Leverage"); leverageField.IsValid() && leverageField.CanInterface() {
						if lev, ok := leverageField.Interface().(int); ok && lev > 0 {
							leverage = lev
							logger.Debug("🔍 [持仓恢复] 从 GetPositions 获取到杠杆倍数: %dx", leverage)
							break
						}
					}
				}
			}
		}
	}
	
	logger.Info("🔍 [持仓恢复] 检测到杠杆倍数: %dx，将使用实际保证金（仓位价值 / 杠杆）计算已用资金", leverage)

	// 1. 计算每单的理论数量（基于当前价格）
	// 使用锚点价格作为参考价格，使用从交易所获取的数量精度

	// 每单的理论数量 = 目标金额 / 锚点价格
	theoryQtyPerSlot := spm.config.Trading.OrderQuantity / spm.anchorPrice
	theoryQtyPerSlot = roundPrice(theoryQtyPerSlot, spm.quantityDecimals)

	// 2. 计算需要创建的总槽位数
	totalSlotsNeeded := int(math.Ceil(totalPosition / theoryQtyPerSlot))
	logger.Info("🔄 [持仓恢复] 总持仓: %.4f，每单理论数量: %.4f，需要创建 %d 个槽位",
		totalPosition, theoryQtyPerSlot, totalSlotsNeeded)

	// 3. 确定窗口大小（前N个槽位可以立即挂卖单）
	sellWindowSize := spm.config.Trading.SellWindowSize
	if sellWindowSize <= 0 {
		sellWindowSize = spm.config.Trading.BuyWindowSize // 默认与买单窗口相同
	}

	// 4. 计算卖单槽位价格（从锚点价格 + 价格间隔开始）
	// 卖单最低价 = 锚点价格 + 价格间隔（避免与买单最高价冲突）
	sellStartPrice := spm.anchorPrice + spm.config.Trading.PriceInterval
	sellPrices := spm.calculateSlotPrices(sellStartPrice, totalSlotsNeeded, "up")

	logger.Info("🔄 [持仓恢复] 从价格 %s 向上创建 %d 个槽位（前 %d 个将挂卖单）",
		formatPrice(sellStartPrice, spm.priceDecimals), totalSlotsNeeded, sellWindowSize)

	// 5. 先计算所有槽位的理论数量总和（固定金额模式）
	var totalTheoryQty float64
	theoryQtys := make([]float64, len(sellPrices))
	for i, price := range sellPrices {
		theoryQty := spm.config.Trading.OrderQuantity / price
		theoryQty = roundPrice(theoryQty, spm.quantityDecimals)
		theoryQtys[i] = theoryQty
		totalTheoryQty += theoryQty
	}

	logger.Debug("🔍 [持仓恢复] 理论总数量: %.4f, 实际持仓: %.4f, 比例: %.4f",
		totalTheoryQty, totalPosition, totalPosition/totalTheoryQty)

	// 6. 按比例分配实际持仓到各个槽位，并累加已用资金
	var allocatedQty float64
	var totalUsedAmount float64 // 累加已用资金

	for i, price := range sellPrices {
		// 计算这个槽位应该分配的数量
		var slotQty float64
		if i == len(sellPrices)-1 {
			// 最后一个槽位：分配剩余的所有持仓（避免舍入误差）
			slotQty = totalPosition - allocatedQty
		} else {
			// 按比例分配：实际数量 = 理论数量 × (总持仓 / 理论总数量)
			slotQty = theoryQtys[i] * (totalPosition / totalTheoryQty)
			slotQty = roundPrice(slotQty, spm.quantityDecimals)

			// 确保不超过剩余持仓
			remaining := totalPosition - allocatedQty
			if slotQty > remaining {
				slotQty = remaining
			}
		}

		if slotQty <= 0 {
			logger.Warn("⚠️ [持仓恢复] 槽位 %s 分配数量过小 %.4f，跳过（已分配: %.4f / 总计: %.4f）",
				formatPrice(price, spm.priceDecimals), slotQty, allocatedQty, totalPosition)
			continue
		}

		// 7. 创建或更新槽位
		slot := spm.getOrCreateSlot(price)
		slot.mu.Lock()

		// 设置为有仓状态
		slot.PositionStatus = PositionStatusFilled
		slot.PositionQty = slotQty

		// 清空订单信息，但设置方向为SELL（因为这是恢复的持仓，将来要挂卖单）
		slot.OrderID = 0
		slot.OrderStatus = OrderStatusNotPlaced
		slot.OrderSide = "SELL" // 恢复持仓时标记为卖单方向
		slot.ClientOID = ""
		slot.OrderFilledQty = 0

		slot.mu.Unlock()

		allocatedQty += slotQty
		// 累加已用资金：使用实际保证金（仓位价值 / 杠杆倍数）而不是仓位价值
		// 锚点价格是市场当前价格，接近实际买入的平均价格
		// 不能用卖出价格（sellPrice），因为卖出价格是目标价，会高估成本
		// 对于有杠杆的交易，实际使用的保证金 = 仓位价值 / 杠杆倍数
		positionValue := spm.anchorPrice * slotQty // 仓位价值
		actualMargin := positionValue / float64(leverage) // 实际使用的保证金
		totalUsedAmount += actualMargin

		// 日志标记：是否在窗口内（只打印前10个和最后10个）
		if i < 10 || i >= len(sellPrices)-10 {
			inWindow := ""
			if i < sellWindowSize {
				inWindow = " [可挂单]"
			} else {
				inWindow = " [暂不挂单]"
			}
			logger.Info("✅ [持仓恢复] 槽位 %s: 分配持仓 %.4f (理论: %.4f)%s",
				formatPrice(price, spm.priceDecimals), slotQty, theoryQtys[i], inWindow)
		} else if i == 10 {
			logger.Info("... （省略中间 %d 个槽位）", len(sellPrices)-20)
		}
	}

	logger.Info("✅ [持仓恢复] 完成持仓恢复，总持仓: %.4f，已分配: %.4f，差异: %.4f",
		totalPosition, allocatedQty, totalPosition-allocatedQty)

	// 🔥 初始化已用资金：使用实际保证金（仓位价值 / 杠杆倍数）而不是仓位价值
	// 这样资金限额限制的是实际投入的资金，而不是仓位价值
	if totalUsedAmount > 0 {
		spm.allocationManager.SetUsedAmount(spm.exchangeName, spm.config.Trading.Symbol, totalUsedAmount)
		positionValue := spm.anchorPrice * totalPosition // 总仓位价值
		logger.Info("💰 [%s:%s] [资金分配] 恢复持仓，初始化已用资金: %.2f USDT (实际保证金，杠杆 %dx，仓位价值: %.2f USDT)", 
			spm.exchangeName, spm.config.Trading.Symbol, totalUsedAmount, leverage, positionValue)
	}

	// 8. 提示用户后续会自动下卖单
	logger.Info("💡 [持仓恢复] 前 %d 个槽位的卖单将在价格调整时自动创建", sellWindowSize)
	logger.Info("💡 [持仓恢复] 其余 %d 个槽位保持有仓状态，价格接近时自动挂单", totalSlotsNeeded-sellWindowSize)
}

// ===== 状态打印功能 =====

// PrintPositions 打印持仓状态（由 main.go 定期调用和退出时调用）
// 注意：该方法内部使用 totalBuyQty 和 totalSellQty 统计数据
func (spm *SuperPositionManager) PrintPositions() {
	// 从配置中获取交易对信息
	symbol := spm.config.Trading.Symbol
	currentPositionsMsg := logger.Translate("log.position.current_positions", map[string]interface{}{"Symbol": symbol})
	logger.Info("%s", currentPositionsMsg)
	total := 0.0
	count := 0

	// 收集所有持仓数据
	type positionInfo struct {
		Price          float64
		Qty            float64
		OrderStatus    string
		OrderSide      string
		OrderID        int64
		SlotStatus     string
		OrderCreatedAt time.Time
	}
	var positions []positionInfo

	spm.slots.Range(func(key, value interface{}) bool {
		price := key.(float64)
		slot := value.(*InventorySlot)
		slot.mu.RLock()
		if slot.PositionStatus == PositionStatusFilled && slot.PositionQty > 0.001 {
			positions = append(positions, positionInfo{
				Price:          price,
				Qty:            slot.PositionQty,
				OrderStatus:    slot.OrderStatus,
				OrderSide:      slot.OrderSide,
				OrderID:        slot.OrderID,
				SlotStatus:     slot.SlotStatus,
				OrderCreatedAt: slot.OrderCreatedAt,
			})
			total += slot.PositionQty
			count++
		}
		slot.mu.RUnlock()
		return true
	})

	// 按价格从高到低排序
	sort.Slice(positions, func(i, j int) bool {
		return positions[i].Price > positions[j].Price
	})

	// 从交易所接口获取基础币种（支持U本位和币本位合约）
	baseCurrency := spm.exchange.GetBaseAsset()

	// 打印持仓（从高到低）
	for _, pos := range positions {
		statusIcon := "🟢" // 有持仓
		priceStr := formatPrice(pos.Price, spm.priceDecimals)
		
		// 使用翻译函数获取持仓信息
		positionDesc := logger.Translate("log.position.position_info", map[string]interface{}{
			"Qty":      fmt.Sprintf("%.4f", pos.Qty),
			"Currency": baseCurrency,
		})

		orderInfo := ""
		if pos.OrderStatus != OrderStatusNotPlaced && pos.OrderStatus != "" {
			orderInfo = ", " + logger.Translate("log.position.order_info", map[string]interface{}{
				"Side":   pos.OrderSide,
				"Status":  pos.OrderStatus,
				"OrderID": pos.OrderID,
			})
		}

		// 🔥 总是显示槽位状态,便于调试
		slotStatusInfo := ""
		if pos.SlotStatus != "" {
			slotStatusInfo = " [" + logger.Translate("log.position.slot_status", map[string]interface{}{
				"Status": pos.SlotStatus,
			}) + "]"
		} else {
			slotStatusInfo = " [" + logger.Translate("log.position.slot_empty") + "]"
		}

		// 格式化买入时间（使用订单创建时间作为买入时间参考）
		buyTimeStr := ""
		if !pos.OrderCreatedAt.IsZero() {
			buyTimeStr = ", " + logger.Translate("log.position.buy_time", map[string]interface{}{
				"Time": pos.OrderCreatedAt.Format("2006/01/02 15:04:05"),
			})
		}

		// 添加交易所、币种、策略信息
		strategyName := logger.Translate("log.position.strategy_grid")
		exchangeSymbolInfo := fmt.Sprintf("[%s:%s:%s]", spm.exchangeName, spm.config.Trading.Symbol, strategyName)

		logger.Info("  %s %s %s: %s%s%s%s",
			statusIcon, exchangeSymbolInfo, priceStr, positionDesc, buyTimeStr, orderInfo, slotStatusInfo)
	}

	positionSummaryMsg := logger.Translate("log.position.position_summary", map[string]interface{}{
		"Symbol":   spm.config.Trading.Symbol,
		"Total":    fmt.Sprintf("%.4f", total),
		"Currency": baseCurrency,
		"Count":    count,
	})
	logger.Info("%s", positionSummaryMsg)
	totalBuyQty := spm.totalBuyQty.Load().(float64)
	totalSellQty := spm.totalSellQty.Load().(float64)
	// 预计盈利 = 累计卖出数量 × 价格间距（每笔盈利 = 价格间距 × 数量）
	estimatedProfit := totalSellQty * spm.config.Trading.PriceInterval
	logger.Info("[%s] 累计买入: %.2f, 累计卖出: %.2f, 预计盈利: %.2f U",
		spm.config.Trading.Symbol, totalBuyQty, totalSellQty, estimatedProfit)

	// === 新增：打印买单窗口详细信息 ===
	logger.Info("🔍 ===== 买单窗口状态 [%s] =====", spm.config.Trading.Symbol)

	// 获取最后的市场价格
	lastPrice, ok := spm.lastMarketPrice.Load().(float64)
	if !ok || lastPrice <= 0 {
		lastPrice = spm.anchorPrice // 如果没有更新过，使用锚点价格
	}
	logger.Info("[%s] 当前市场价格: %s", spm.config.Trading.Symbol, formatPrice(lastPrice, spm.priceDecimals))

	// 收集所有槽位信息（包括买单和空槽位）
	type slotInfo struct {
		Price          float64
		PositionStatus string
		PositionQty    float64
		OrderSide      string
		OrderStatus    string
		OrderID        int64
		ClientOID      string
		SlotStatus     string
	}
	var allSlots []slotInfo

	spm.slots.Range(func(key, value interface{}) bool {
		price := key.(float64)
		slot := value.(*InventorySlot)
		slot.mu.RLock()
		allSlots = append(allSlots, slotInfo{
			Price:          price,
			PositionStatus: slot.PositionStatus,
			PositionQty:    slot.PositionQty,
			OrderSide:      slot.OrderSide,
			OrderStatus:    slot.OrderStatus,
			OrderID:        slot.OrderID,
			ClientOID:      slot.ClientOID,
			SlotStatus:     slot.SlotStatus,
		})
		slot.mu.RUnlock()
		return true
	})

	// 按价格从高到低排序
	sort.Slice(allSlots, func(i, j int) bool {
		return allSlots[i].Price > allSlots[j].Price
	})

	// 找到最接近当前价格的网格价格
	currentGridPrice := spm.findNearestGridPrice(lastPrice)
	logger.Info("[%s] 当前网格价格: %s", spm.config.Trading.Symbol, formatPrice(currentGridPrice, spm.priceDecimals))

	// 计算买单窗口范围（当前网格价格下方的买单窗口）
	buyWindowSize := spm.config.Trading.BuyWindowSize
	buyWindowPrices := spm.calculateSlotPrices(currentGridPrice, buyWindowSize, "down")

	// 创建价格查找表
	buyWindowPriceMap := make(map[string]bool)
	for _, p := range buyWindowPrices {
		buyWindowPriceMap[formatPrice(p, spm.priceDecimals)] = true
	}

	// 打印买单窗口内的所有槽位
	logger.Info("[%s] 买单窗口大小: %d 个槽位 (当前网格价格下方)", spm.config.Trading.Symbol, buyWindowSize)
	buyOrderCount := 0
	emptySlotCount := 0
	filledSlotCount := 0

	for _, slot := range allSlots {
		priceStr := formatPrice(slot.Price, spm.priceDecimals)
		// 只打印买单窗口内的槽位
		if buyWindowPriceMap[priceStr] {
			statusIcon := "⚪" // 空槽位
			statusDesc := ""

			if slot.PositionStatus == PositionStatusFilled {
				statusIcon = "🟢" // 有持仓
				statusDesc = fmt.Sprintf("持仓: %.4f %s", slot.PositionQty, baseCurrency)
				filledSlotCount++
			} else {
				statusDesc = "无持仓"
				emptySlotCount++
			}

			orderInfo := ""
			if slot.OrderStatus != OrderStatusNotPlaced && slot.OrderStatus != "" {
				orderInfo = fmt.Sprintf(", 订单: %s/%s (ID:%d)", slot.OrderSide, slot.OrderStatus, slot.OrderID)
				if slot.OrderSide == "BUY" && (slot.OrderStatus == OrderStatusPlaced ||
					slot.OrderStatus == OrderStatusConfirmed ||
					slot.OrderStatus == OrderStatusPartiallyFilled) {
					buyOrderCount++
				}
			}

			// 🔥 总是显示槽位状态,便于调试
			slotStatusInfo := ""
			if slot.SlotStatus != "" {
				slotStatusInfo = fmt.Sprintf(" [槽位:%s]", slot.SlotStatus)
			} else {
				slotStatusInfo = " [槽位:空]"
			}

			logger.Info("  %s %s: %s%s%s",
				statusIcon, priceStr, statusDesc, orderInfo, slotStatusInfo)
		}
	}

	logger.Info("[%s] 窗口统计: %d 个买单活跃, %d 个已持仓, %d 个空槽位",
		spm.config.Trading.Symbol, buyOrderCount, filledSlotCount, emptySlotCount)
	logger.Info("==========================")
}

// 辅助函数
// roundPrice 价格四舍五入
func roundPrice(price float64, decimals int) float64 {
	multiplier := math.Pow(10, float64(decimals))
	return math.Round(price*multiplier) / multiplier
}

// formatPrice 格式化价格字符串，使用指定的小数位数
func formatPrice(price float64, decimals int) string {
	return fmt.Sprintf("%.*f", decimals, price)
}

// calculateUnrealizedPnL 计算未实现盈亏
func (spm *SuperPositionManager) calculateUnrealizedPnL(currentPrice float64) float64 {
	totalPnL := 0.0
	spm.slots.Range(func(key, value interface{}) bool {
		slotPrice := key.(float64)
		slot := value.(*InventorySlot)
		slot.mu.RLock()
		if slot.PositionStatus == PositionStatusFilled && slot.PositionQty > 0 {
			// 盈亏 = (当前价格 - 买入价格) * 数量
			totalPnL += (currentPrice - slotPrice) * slot.PositionQty
		}
		slot.mu.RUnlock()
		return true
	})
	return totalPnL
}

// calculateTotalPositionValue 计算当前持仓总价值
func (spm *SuperPositionManager) calculateTotalPositionValue(currentPrice float64) float64 {
	totalValue := 0.0
	spm.slots.Range(func(key, value interface{}) bool {
		slot := value.(*InventorySlot)
		slot.mu.RLock()
		if slot.PositionStatus == PositionStatusFilled && slot.PositionQty > 0 {
			totalValue += currentPrice * slot.PositionQty
		}
		slot.mu.RUnlock()
		return true
	})
	return totalValue
}

// GetActiveLayers 统计当前持仓层数
func (spm *SuperPositionManager) GetActiveLayers() int {
	layers := 0
	spm.slots.Range(func(key, value interface{}) bool {
		slot := value.(*InventorySlot)
		slot.mu.RLock()
		if slot.PositionStatus == PositionStatusFilled && slot.PositionQty > 0 {
			layers++
		}
		slot.mu.RUnlock()
		return true
	})
	return layers
}

// CleanupEmptySlots 清理空槽位（定期调用，防止 sync.Map 内存泄漏）
// 清理条件：空仓、无订单、无订单历史
func (spm *SuperPositionManager) CleanupEmptySlots() int {
	var toDelete []float64

	spm.slots.Range(func(key, value interface{}) bool {
		price := key.(float64)
		slot := value.(*InventorySlot)

		slot.mu.RLock()
		isEmpty := slot.PositionStatus == PositionStatusEmpty &&
			slot.PositionQty < 0.000001 &&
			slot.OrderID == 0 &&
			slot.OrderStatus == OrderStatusNotPlaced &&
			slot.SlotStatus == SlotStatusFree
		slot.mu.RUnlock()

		if isEmpty {
			toDelete = append(toDelete, price)
		}
		return true
	})

	// 删除空槽位
	deletedCount := 0
	for _, price := range toDelete {
		spm.slots.Delete(price)
		deletedCount++
	}

	if deletedCount > 0 {
		logger.Debug("🧹 [槽位清理] 已清理 %d 个空槽位", deletedCount)
	}

	return deletedCount
}
