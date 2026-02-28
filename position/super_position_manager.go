package position

import (
	"context"
	"fmt"
	"math"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"quantmesh/config"
	"quantmesh/event"
	"quantmesh/logger"
	"quantmesh/utils"
)

// OrderUpdate 订單更新事件（避免依赖 websocket 包）
type OrderUpdate struct {
	OrderID         int64
	ClientOrderID   string
	Symbol          string
	Status          string
	ExecutedQty     float64
	Price           float64
	AvgPrice        float64
	Side            string
	Type            string
	UpdateTime      int64
	Commission      float64 // 本次成交手續費
	CommissionAsset string  // 手續費幣種
	RealizedPnL     float64 // 已實現盈虧（交易所計算）
}

// BatchPlaceOrdersResult 批量下單結果
type BatchPlaceOrdersResult struct {
	PlacedOrders     []*Order        // 成功下單的订單列表
	HasMarginError   bool            // 是否出現保证金不足錯误
	ReduceOnlyErrors map[string]bool // ReduceOnly錯误的订單（key為ClientOrderID）
}

// OrderExecutorInterface 订單執行器介面（避免循環匯入）
type OrderExecutorInterface interface {
	PlaceOrder(req *OrderRequest) (*Order, error)
	BatchPlaceOrders(orders []*OrderRequest) ([]*Order, bool)
	BatchPlaceOrdersWithDetails(orders []*OrderRequest) *BatchPlaceOrdersResult
	BatchCancelOrders(orderIDs []int64) error
}

// OrderRequest 订單请求（避免循環匯入）
type OrderRequest struct {
	Symbol        string
	Side          string
	Price         float64
	Quantity      float64
	PriceDecimals int    // 價格小數位數（用於格式化價格字符串）
	ReduceOnly    bool   // 是否只减倉（平倉單）
	PostOnly      bool   // 是否只做 Maker（Post Only）
	ClientOrderID string // 自定义订單ID
	StrategyName  string // 策略名称（可選，用於日志追踪）
	StrategyType  string // 策略類型（可選，如 "grid", "dca", "martingale"）
	OrderSource   string // 订單來源（"normal"=正常限價, "stop_loss"=止損平倉, "liquidation"=強制平倉）
}

// Order 订單信息（避免循環匯入）
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

// 订單状態常量
const (
	OrderStatusNotPlaced       = "NOT_PLACED"       // 未下單
	OrderStatusPlaced          = "PLACED"           // 已下單
	OrderStatusConfirmed       = "CONFIRMED"        // 已确认（WebSocket确认）
	OrderStatusPartiallyFilled = "PARTIALLY_FILLED" // 部分成交
	OrderStatusFilled          = "FILLED"           // 全部成交
	OrderStatusCancelRequested = "CANCEL_REQUESTED" // 已申请撤單
	OrderStatusCanceled        = "CANCELED"         // 已撤單
)

// 持倉状態常量
const (
	PositionStatusEmpty  = "EMPTY"  // 空倉
	PositionStatusFilled = "FILLED" // 有倉
)

// 槽位鎖定状態
const (
	SlotStatusFree    = "FREE"    // 空闲，可操作
	SlotStatusPending = "PENDING" // 等待下單确认
	SlotStatusLocked  = "LOCKED"  // 已鎖定，有活跃订單
)

// InventorySlot 库存槽位（每個價格点一個）
type InventorySlot struct {
	Price float64 // 價格（作為key，支援高精度）

	// 持倉資訊
	PositionStatus string  // 持倉状態：空倉/有倉
	PositionQty    float64 // 持倉數量（支援小數点后3位）

	// 订單信息 (買賣互斥)
	OrderID        int64     // 订單ID
	ClientOID      string    // 自定义订單ID
	OrderSide      string    // 订單方向 (BUY/SELL)
	OrderStatus    string    // 订單状態
	OrderPrice     float64   // 订單價格
	OrderFilledQty float64   // 成交數量
	OrderCreatedAt time.Time // 創建時间

	// 🔥 新增：槽位鎖定状態，防止並发重複操作
	SlotStatus string // FREE/PENDING/LOCKED

	// PostOnly失败计數（连续失败3次后降级為普通單）
	PostOnlyFailCount int

	// 買入手續費累計（該槽位持倉對應的買單手續費，賣出時按比例攤銷）
	BuyFee   float64
	FeeAsset string

	// 🔥 实际平均买入价格（用於准确计算盈亏）
	// 当买入订单成交时，使用实际成交价格更新此字段
	// 计算公式：AvgBuyPrice = (旧AvgBuyPrice * 旧持仓 + 新买入价格 * 新买入数量) / 总持仓
	AvgBuyPrice float64

	// 策略信息（用於追踪订單来源）
	StrategyName string // 策略名称（如 "Grid-BTCUSDT-1", "DCA-ETHUSDT"）
	StrategyType string // 策略類型（如 "grid", "dca", "martingale"）

	mu sync.RWMutex // 槽位级别的鎖（细粒度鎖）
}

// PositionInfo 持倉資訊（简化版，避免循環匯入）
type PositionInfo struct {
	Symbol string
	Size   float64
}

// OrderBookLevel 订單簿檔位（避免循環匯入）
type OrderBookLevel struct {
	Price    float64 // 價格
	Quantity float64 // 數量
}

// OrderBook 订單簿（避免循環匯入）
type OrderBook struct {
	Symbol    string           // 交易對
	Bids      []OrderBookLevel // 買盘 (價格從高到低)
	Asks      []OrderBookLevel // 賣盘 (價格從低到高)
	Timestamp int64            // 時间戳
}

// IExchange 交易所介面（避免循環匯入）
// 注意：这里不能直接使用 exchange.IExchange，否则會循環匯入
// 所以定义一個子集接口，只包含對账需要的方法
type IExchange interface {
	GetName() string // 獲取交易所名称
	GetPositions(ctx context.Context, symbol string) (interface{}, error)
	GetOpenOrders(ctx context.Context, symbol string) (interface{}, error)
	GetOrder(ctx context.Context, symbol string, orderID int64) (interface{}, error)
	GetBaseAsset() string                                                           // 獲取基础资產（交易币种）
	CancelAllOrders(ctx context.Context, symbol string) error                       // 取消所有订單
	GetAccount(ctx context.Context) (interface{}, error)                            // 獲取帳戶信息（回傳 *exchange.Account 或類似結構）
	GetPriceDecimals() int                                                          // 獲取價格精度
	GetQuantityDecimals() int                                                       // 獲取數量精度
	GetOrderBook(ctx context.Context, symbol string, limit int) (*OrderBook, error) // 獲取订單簿深度
	// GetOrderFills 查詢訂單成交記錄（用於獲取手續費）
	// 返回 nil, nil 表示不支援或查詢失敗
	GetOrderFills(ctx context.Context, symbol string, orderID int64) (interface{}, error)
}

// TradeStorage 交易存儲介面（避免循環匯入）
// 用於保存交易記錄（買賣配對）
type TradeStorage interface {
	SaveTrade(buyOrderID, sellOrderID int64, exchange, symbol string, buyPrice, sellPrice, quantity, pnl, fee float64, feeAsset string, createdAt time.Time) error
	// 🔥 SaveTradeWithDeviation 保存交易記錄（包含價格偏差）
	SaveTradeWithDeviation(buyOrderID, sellOrderID int64, exchange, symbol string, buyPrice, sellPrice, quantity, pnl, fee float64, feeAsset string, buyPriceDeviation, sellPriceDeviation float64, createdAt time.Time) error
}

// ReconciliationStorage 對账存儲介面（避免循環匯入）
// 用於恢複對账统计值
type ReconciliationStorage interface {
	GetLatestReconciliationHistory(exchange, symbol string) (interface{}, error) // 回傳 *storage.ReconciliationHistory
	GetReconciliationCount(exchange, symbol string) (int64, error)
}

// ITrendDetector 趋势检测器介面（避免循環匯入）
type ITrendDetector interface {
	GetCurrentTrend() string
}

// FundingMonitor 資金費率監控介面（避免循環匯入）
// 用於獲取資金費率偏向策略
type FundingMonitor interface {
	// GetBuyBias 獲取買入偏向係數
	// 返回 0-1.2 的值：1.0 為正常，<1.0 為減少買入，0 為暫停買入，>1.0 為增加買入
	GetBuyBias() float64
	// IsHighRate 判斷是否為高費率
	IsHighRate() bool
	// GetCurrentRate 獲取當前資金費率
	GetCurrentRate() float64
	// ShouldPauseBuying 判斷是否應該暫停買入
	ShouldPauseBuying() bool
}

// SuperPositionManager 超级倉位管理器
type SuperPositionManager struct {
	config       *config.Config
	executor     OrderExecutorInterface
	exchange     IExchange
	exchangeName string // 交易所名称（配置中的名称，如 "binance"）
	botID        string // Bot 唯一標識，用於日誌區分同交易所同幣多實例

	// 策略信息（用於追踪订單来源）
	strategyName string // 策略名称（如 "Grid-BTCUSDT-1"）
	strategyType string // 策略類型（固定為 "grid"）

	// 價格锚点（初始化時的市场價格）
	anchorPrice float64
	// 最后市场價格（用於打印状態）
	lastMarketPrice atomic.Value // float64
	// 價格精度（根據锚点價格检测得出的小數位數）
	priceDecimals int
	// 數量精度（從交易所獲取）
	quantityDecimals int

	// 库存槽位：價格 -> 槽位
	slots sync.Map // map[float64]*InventorySlot

	// 保证金管理
	insufficientMargin bool
	marginLockTime     time.Time
	marginLockDuration time.Duration

	// 风險監控状態
	peakPnL       float64        // 記錄最高未實現盈亏（用於回撤止盈）
	trendDetector ITrendDetector // 趋势检测器

	// 资金分配管理器
	allocationManager *AllocationManager

	// 事件總線（用於发送告警）
	eventBus EventBus

	// 统计（注意：以下字段被 safety.Reconciler 和 PrintPositions 使用，不可刪除）
	totalBuyQty          atomic.Value // float64 - 累计買入數量
	totalSellQty         atomic.Value // float64 - 累计賣出數量
	reconcileCount       atomic.Int64 // 對账次數
	lastReconcileTime    atomic.Value // time.Time - 最后對账時间
	lastOptimizationTime atomic.Value // time.Time - 最后訂單簿優化時间

	// 交易存儲（可選，用於保存交易記錄）
	tradeStorage TradeStorage

	// 初始化標志
	isInitialized atomic.Bool

	// 暂停標志
	isPaused atomic.Bool

	// 開倉管理：僅暫停開倉（區別於 isPaused 暫停所有交易）
	isOpeningPaused    atomic.Bool
	openingPauseReason atomic.Value // string - 暫停原因

	// 資金費率監控器（可選，用於費率偏向策略）
	fundingMonitor FundingMonitor

	// 套利管理器（可選，用於期現套利）
	arbitrageManager ArbitrageManager

	// 成交時間戳記錄（用於動態調整單筆金額的頻率統計）
	fillTimestamps []time.Time
	fillMu         sync.RWMutex

	// 槽位過濾器
	slotFilter     *config.SlotFilterConfig
	slotFilterMu   sync.RWMutex

	// 智能掛單管理器
	smartOrderMgr  *SmartOrderManager

	mu sync.RWMutex // 全局鎖（用於关键操作）
}

// EventBus 事件總線接口
type EventBus interface {
	Publish(evt *event.Event)
}

// NewSuperPositionManager 創建超级倉位管理器
func NewSuperPositionManager(cfg *config.Config, executor OrderExecutorInterface, exchange IExchange, priceDecimals, quantityDecimals int) *SuperPositionManager {
	marginLockSec := cfg.Trading.MarginLockDurationSec
	if marginLockSec <= 0 {
		marginLockSec = 10 // 預設 10秒
	}

	// 從配置中獲取交易所名称
	exchangeName := strings.ToLower(cfg.App.CurrentExchange)
	if exchangeName == "" {
		exchangeName = "binance" // 默认值
	}

	// Bot ID（用於日誌區分同交易所同幣多實例）
	botID := cfg.Trading.BotID
	if botID == "" {
		mt := cfg.Trading.MarketType
		if mt == "" {
			mt = "futures"
		}
		botID = config.GenerateBotID(exchangeName, cfg.Trading.Symbol, mt)
	}

	// 生成策略名称
	symbol := cfg.Trading.Symbol
	strategyName := fmt.Sprintf("Grid-%s", symbol)

	spm := &SuperPositionManager{
		config:             cfg,
		executor:           executor,
		exchange:           exchange,
		exchangeName:       exchangeName,
		botID:              botID,
		strategyName:       strategyName,  // 策略名称
		strategyType:       "grid",        // 策略類型固定為 grid
		insufficientMargin: false,
		marginLockDuration: time.Duration(marginLockSec) * time.Second,
		priceDecimals:      priceDecimals,
		quantityDecimals:   quantityDecimals,
		peakPnL:            -math.MaxFloat64,          // 初始化為一個极小值
		tradeStorage:       nil,                       // 默认不保存交易記錄，可通過 SetTradeStorage 設置
		allocationManager:  NewAllocationManager(cfg), // 初始化资金分配管理器
		slotFilter:         nil,                       // 初始化為空，可通過 SetSlotFilter 設置
	}
	spm.totalBuyQty.Store(0.0)
	spm.totalSellQty.Store(0.0)
	spm.lastReconcileTime.Store(time.Now())
	spm.lastMarketPrice.Store(0.0)

	// 初始化智能掛單管理器（如果配置啟用）
	if cfg.Trading.SmartOrder.Enabled {
		spm.smartOrderMgr = NewSmartOrderManager(spm, &cfg.Trading.SmartOrder)
		logger.Info("🧠 [%s] 智能掛單已啟用: MaxOpenOrders=%d Distance=%.1f",
			spm.logPrefix(), cfg.Trading.SmartOrder.MaxOpenOrders, cfg.Trading.SmartOrder.OpenOrderDistance)
	}

	return spm
}

// logPrefix 返回日誌前綴，含 bot ID 便於區分同交易所同幣多實例
func (spm *SuperPositionManager) logPrefix() string {
	if spm.botID != "" {
		return spm.botID
	}
	return spm.exchangeName + ":" + spm.config.Trading.Symbol
}

// Pause 暂停交易
func (spm *SuperPositionManager) Pause() {
	spm.isPaused.Store(true)
	logger.Warn("⏸️ [%s] 倉位管理器已暂停交易", spm.logPrefix())
}

// Resume 恢複交易
func (spm *SuperPositionManager) Resume() {
	spm.isPaused.Store(false)
	logger.Info("▶️ [%s] 倉位管理器已恢複交易", spm.logPrefix())
}

// IsPaused 是否已暂停
func (spm *SuperPositionManager) IsPaused() bool {
	return spm.isPaused.Load()
}

// PauseOpening 暫停開倉（並撤銷所有開倉委託）
func (spm *SuperPositionManager) PauseOpening(reason string) {
	spm.isOpeningPaused.Store(true)
	spm.openingPauseReason.Store(reason)
	logger.Warn("⏸️ [%s] 開倉管理：已暫停開倉，原因: %s", spm.logPrefix(), reason)
	
	// 撤銷所有開倉委託
	spm.CancelAllOpenOrders()
	
	// 為了確保萬無一失，特別是在幣安合約等場景，
	// 如果本地 slots 狀態同步有延遲，直接調用交易所接口撤銷開倉方向的所有訂單
	go func() {
		// 延遲一小段時間，等待可能的本地狀態更新
		time.Sleep(1 * time.Second)
		
		openSide := "BUY"
		if spm.isShort() {
			openSide = "SELL"
		}
		
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		
		// 獲取交易所所有掛單
		openOrdersInterface, err := spm.exchange.GetOpenOrders(ctx, spm.config.Trading.Symbol)
		if err != nil {
			logger.Error("❌ [%s] 暫停開倉時獲取掛單失敗: %v", spm.logPrefix(), err)
			return
		}
		
		// 類型斷言，因為 IExchange.GetOpenOrders 返回 interface{}
		// 這裡我們需要根據 IExchange 實際返回的類型進行斷言
		// 在 adapter 中通常返回的是 []*exchange.Order，但為了避免循環導入，
		// 我們可能需要處理成 []interface{} 或者反射處理，或者在這裡定義一個兼容的結構
		
		var toCancel []int64
		
		// 嘗試反射處理，這樣最通用
		v := reflect.ValueOf(openOrdersInterface)
		if v.Kind() == reflect.Slice {
			for i := 0; i < v.Len(); i++ {
				orderVal := v.Index(i)
				if orderVal.Kind() == reflect.Ptr {
					orderVal = orderVal.Elem()
				}
				
				if orderVal.Kind() == reflect.Struct {
					sideField := orderVal.FieldByName("Side")
					idField := orderVal.FieldByName("OrderID")
					
					if sideField.IsValid() && idField.IsValid() {
						sideStr := fmt.Sprintf("%v", sideField.Interface())
						if sideStr == openSide {
							toCancel = append(toCancel, idField.Int())
						}
					}
				}
			}
		}
		
		if len(toCancel) > 0 {
			logger.Warn("🔄 [%s] 暫停開倉：發現 %d 個殘留開倉委託，正在強制撤銷", spm.logPrefix(), len(toCancel))
			if err := spm.executor.BatchCancelOrders(toCancel); err != nil {
				logger.Error("❌ [%s] 強制撤銷殘留委託失敗: %v", spm.logPrefix(), err)
			} else {
				logger.Info("✅ [%s] 強制撤銷殘留委託完成", spm.logPrefix())
			}
		}
	}()
}

// ResumeOpening 恢復開倉
func (spm *SuperPositionManager) ResumeOpening() {
	spm.isOpeningPaused.Store(false)
	spm.openingPauseReason.Store("")
	logger.Info("▶️ [%s] 開倉管理：已恢復開倉", spm.logPrefix())
}

// IsOpeningPaused 是否已暫停開倉
func (spm *SuperPositionManager) IsOpeningPaused() bool {
	return spm.isOpeningPaused.Load()
}

// GetOpeningPauseReason 獲取開倉暫停原因
func (spm *SuperPositionManager) GetOpeningPauseReason() string {
	v := spm.openingPauseReason.Load()
	if v == nil {
		return ""
	}
	return v.(string)
}

// CancelAllOpenOrders 撤銷所有開倉委託（根據 direction 自動判斷 BUY 或 SELL）
func (spm *SuperPositionManager) CancelAllOpenOrders() {
	openSide := "BUY"
	if spm.isShort() {
		openSide = "SELL"
	}
	var orderIDs []int64
	var prices []float64

	spm.slots.Range(func(key, value interface{}) bool {
		price := key.(float64)
		slot := value.(*InventorySlot)

		slot.mu.RLock()
		if slot.OrderSide == openSide && slot.OrderID > 0 &&
			slot.OrderStatus != OrderStatusCanceled && slot.OrderStatus != OrderStatusCancelRequested {
			orderIDs = append(orderIDs, slot.OrderID)
			prices = append(prices, price)
		}
		slot.mu.RUnlock()
		return true
	})

	if len(orderIDs) == 0 {
		return
	}

	sideLabel := "買單"
	if spm.isShort() {
		sideLabel = "賣單"
	}
	logger.Info("🔄 [開倉管理] 準備撤銷 %d 個開倉 %s", len(orderIDs), sideLabel)

	for attempt := 1; attempt <= 3; attempt++ {
		if len(orderIDs) == 0 {
			break
		}
		if err := spm.executor.BatchCancelOrders(orderIDs); err != nil {
			logger.Error("❌ [開倉管理] 批量撤單失敗: %v", err)
		}

		for _, price := range prices {
			slot := spm.getOrCreateSlot(price)
			slot.mu.Lock()
			slot.OrderStatus = OrderStatusCancelRequested
			slot.mu.Unlock()
		}

		time.Sleep(2 * time.Second)

		if attempt < 3 {
			orderIDs = nil
			prices = nil
			spm.slots.Range(func(key, value interface{}) bool {
				price := key.(float64)
				slot := value.(*InventorySlot)
				slot.mu.RLock()
				if slot.OrderSide == openSide && slot.OrderID > 0 &&
					slot.OrderStatus != OrderStatusCanceled && slot.OrderStatus != OrderStatusCancelRequested {
					orderIDs = append(orderIDs, slot.OrderID)
					prices = append(prices, price)
				}
				slot.mu.RUnlock()
				return true
			})
			if len(orderIDs) == 0 {
				logger.Info("✅ [開倉管理] 所有開倉委託已清理完成")
				break
			}
			logger.Warn("⚠️ [開倉管理] 檢測到 %d 個殘留委託，繼續清理", len(orderIDs))
		}
	}
}

// SetEventBus 設置事件總線
func (spm *SuperPositionManager) SetEventBus(eventBus EventBus) {
	spm.eventBus = eventBus
	// 同時設置到 allocationManager
	if spm.allocationManager != nil {
		spm.allocationManager.SetEventBus(eventBus)
	}
}

// SetTradeStorage 設置交易存儲介面（用於保存交易記錄）
func (spm *SuperPositionManager) SetTradeStorage(storage TradeStorage) {
	spm.tradeStorage = storage
}

// isSpot 是否為現貨交易（現貨不使用 ReduceOnly、杠杆固定為 1）
func (spm *SuperPositionManager) isSpot() bool {
	return spm.config.Trading.MarketType == "spot"
}

// isShort 是否為做空方向
func (spm *SuperPositionManager) isShort() bool {
	return spm.config.Trading.Direction == "SHORT"
}

// isLong 是否為做多方向
func (spm *SuperPositionManager) isLong() bool {
	return spm.config.Trading.Direction == "LONG"
}

// isBoth 是否為雙向交易
func (spm *SuperPositionManager) isBoth() bool {
	return spm.config.Trading.Direction == "BOTH"
}

// isSlotEnabled 檢查槽位是否啟用
func (spm *SuperPositionManager) isSlotEnabled(price float64) bool {
	spm.slotFilterMu.RLock()
	defer spm.slotFilterMu.RUnlock()

	if spm.slotFilter == nil || len(spm.slotFilter.Rules) == 0 {
		return true // 無過濾規則，全部啟用
	}

	// 檢查每條規則
	for _, rule := range spm.slotFilter.Rules {
		matches := false

		// 檢查具體價格列表
		if len(rule.Prices) > 0 {
			for _, p := range rule.Prices {
				if math.Abs(p-price) < 0.000001 { // 浮點數比較
					matches = true
					break
				}
			}
		}

		// 檢查價格區間
		if !matches && rule.MinPrice > 0 && rule.MaxPrice > 0 {
			if price >= rule.MinPrice && price <= rule.MaxPrice {
				matches = true
			}
		}

		// 根據規則類型返回
		if matches {
			if rule.Type == "exclude" {
				return false // 排除
			}
			if rule.Type == "include" {
				return true // 包含（需要所有規則都是include才返回true）
			}
		}
	}

	// 默認：如果沒有include規則匹配，返回true
	// 如果只有exclude規則，返回true（因為沒有被排除）
	hasIncludeRule := false
	for _, rule := range spm.slotFilter.Rules {
		if rule.Type == "include" {
			hasIncludeRule = true
			break
		}
	}
	return !hasIncludeRule
}

// SetSlotFilter 設置槽位過濾器
func (spm *SuperPositionManager) SetSlotFilter(filter *config.SlotFilterConfig) {
	spm.slotFilterMu.Lock()
	defer spm.slotFilterMu.Unlock()
	spm.slotFilter = filter

	// 記錄日誌
	for _, rule := range filter.Rules {
		if rule.Type == "exclude" {
			if len(rule.Prices) > 0 {
				logger.Info("🚫 [槽位過濾] 禁用價格位: %v 原因: %s",
					rule.Prices, rule.Reason)
			}
			if rule.MinPrice > 0 || rule.MaxPrice > 0 {
				logger.Info("🚫 [槽位過濾] 禁用價格區間: [%.2f, %.2f] 原因: %s",
					rule.MinPrice, rule.MaxPrice, rule.Reason)
			}
		}
	}

	// 立即觸發訂單調整，取消被禁用槽位的訂單
	spm.cancelFilteredSlotOrders()
}

// cancelFilteredSlotOrders 取消被過濾槽位的訂單
func (spm *SuperPositionManager) cancelFilteredSlotOrders() {
	var orderIDsToCancel []int64

	spm.slots.Range(func(key, value interface{}) bool {
		price := key.(float64)
		slot := value.(*InventorySlot)

		if !spm.isSlotEnabled(price) {
			slot.mu.Lock()
			if slot.OrderID != 0 {
				orderIDsToCancel = append(orderIDsToCancel, slot.OrderID)
			}
			slot.mu.Unlock()
		}
		return true
	})

	if len(orderIDsToCancel) > 0 {
		logger.Info("🧹 [槽位過濾] 取消被禁用槽位的訂單: %d 個", len(orderIDsToCancel))
		spm.executor.BatchCancelOrders(orderIDsToCancel)
	}
}

// GetSlotFilter 獲取當前槽位過濾器
func (spm *SuperPositionManager) GetSlotFilter() *config.SlotFilterConfig {
	spm.slotFilterMu.RLock()
	defer spm.slotFilterMu.RUnlock()
	return spm.slotFilter
}

// getActualMargin 獲取實際使用的保证金（考虑杠杆）
// 現貨：實際占用 = 訂單價值（杠杆 1）；合約：實際保证金 = 訂單價值 / 杠杆倍數
func (spm *SuperPositionManager) getActualMargin(orderValue float64) float64 {
	if orderValue <= 0 {
		return 0
	}
	if spm.isSpot() {
		return orderValue
	}

	// 獲取杠杆倍數
	leverage := 1 // 默认1倍（無杠杆）
	ctx := context.Background()

	// 先尝試從帳戶資訊中獲取
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

	// 如果從账戶中獲取不到，尝試從持倉中獲取
	if leverage == 1 {
		if positionsInterface, err := spm.exchange.GetPositions(ctx, spm.config.Trading.Symbol); err == nil && positionsInterface != nil {
			// 使用反射处理不同類型的持倉資訊
			positionsValue := reflect.ValueOf(positionsInterface)
			if positionsValue.Kind() == reflect.Slice {
				for i := 0; i < positionsValue.Len(); i++ {
					posValue := positionsValue.Index(i)
					if posValue.Kind() == reflect.Ptr {
						posValue = posValue.Elem()
					} else if posValue.Kind() == reflect.Interface {
						posValue = posValue.Elem()
					}

					// 尝試獲取 Leverage 字段
					if leverageField := posValue.FieldByName("Leverage"); leverageField.IsValid() && leverageField.CanInterface() {
						if lev, ok := leverageField.Interface().(int); ok && lev > 0 {
							leverage = lev
							logger.Debug("🔍 [杠杆检测] 從持倉資訊中獲取到杠杆倍數: %dx", leverage)
							break
						}
					}
				}
			}
		}
	}

	// 计算實際保证金
	return orderValue / float64(leverage)
}

// SetTrendDetector 設置趋势检测器
func (spm *SuperPositionManager) SetTrendDetector(td ITrendDetector) {
	spm.mu.Lock()
	defer spm.mu.Unlock()
	spm.trendDetector = td
}

// SetFundingMonitor 設置資金費率監控器（用於費率偏向策略）
func (spm *SuperPositionManager) SetFundingMonitor(fm FundingMonitor) {
	spm.mu.Lock()
	defer spm.mu.Unlock()
	spm.fundingMonitor = fm
}

// GetFundingMonitor 獲取資金費率監控器
func (spm *SuperPositionManager) GetFundingMonitor() FundingMonitor {
	spm.mu.RLock()
	defer spm.mu.RUnlock()
	return spm.fundingMonitor
}

// ArbitrageManager 套利管理器介面
type ArbitrageManager interface {
	OnGridPositionChange(delta float64, price float64)
}

// SetArbitrageManager 設置套利管理器
func (spm *SuperPositionManager) SetArbitrageManager(am ArbitrageManager) {
	spm.mu.Lock()
	defer spm.mu.Unlock()
	spm.arbitrageManager = am
}

// GetArbitrageManager 獲取套利管理器
func (spm *SuperPositionManager) GetArbitrageManager() ArbitrageManager {
	spm.mu.RLock()
	defer spm.mu.RUnlock()
	return spm.arbitrageManager
}

// recordFill 記錄成交時間戳（用於動態調整單筆金額的頻率統計）
func (spm *SuperPositionManager) recordFill() {
	spm.fillMu.Lock()
	defer spm.fillMu.Unlock()
	spm.fillTimestamps = append(spm.fillTimestamps, time.Now())
	// 保留最近 2 分鐘的記錄，避免無限增長
	cutoff := time.Now().Add(-2 * time.Minute)
	for len(spm.fillTimestamps) > 0 && spm.fillTimestamps[0].Before(cutoff) {
		spm.fillTimestamps = spm.fillTimestamps[1:]
	}
}

// GetFillCountInLastMinute 獲取過去 1 分鐘內的成交次數（會 prune 超過 1 分鐘的紀錄）
func (spm *SuperPositionManager) GetFillCountInLastMinute() int {
	spm.fillMu.Lock()
	defer spm.fillMu.Unlock()
	cutoff := time.Now().Add(-1 * time.Minute)
	for len(spm.fillTimestamps) > 0 && spm.fillTimestamps[0].Before(cutoff) {
		spm.fillTimestamps = spm.fillTimestamps[1:]
	}
	return len(spm.fillTimestamps)
}

// Initialize 初始化管理器（設置價格锚点並創建初始槽位）
func (spm *SuperPositionManager) Initialize(initialPrice float64, initialPriceStr string) error {
	spm.mu.Lock()
	defer spm.mu.Unlock()

	if initialPrice <= 0 {
		return fmt.Errorf("初始價格無效: %.2f", initialPrice)
	}

	// 1. 設置價格锚点（精度信息已經在構造函數中設置，從交易所獲取）
	spm.anchorPrice = initialPrice
	spm.lastMarketPrice.Store(initialPrice) // 初始化最后市场價格
	logger.Info("✅ 價格锚点已設置: %s, 價格精度:%d, 數量精度:%d",
		formatPrice(initialPrice, spm.priceDecimals), spm.priceDecimals, spm.quantityDecimals)

	// 2. 直接使用锚点價格作為网格價格（不再對齐到整數）
	initialGridPrice := spm.anchorPrice
	logger.Info("✅ 初始网格價格: %s (使用锚点價格)", formatPrice(initialGridPrice, spm.priceDecimals))

	// 4. 使用统一的槽位價格计算方法創建初始槽位
	// LONG: 槽位在锚点下方（買低賣高）；SHORT: 槽位在锚点上方（賣高買低）
	slotDir := "down"
	if spm.isShort() {
		slotDir = "up"
	}
	slotPrices := spm.calculateSlotPrices(initialGridPrice, spm.config.Trading.BuyWindowSize, slotDir)
	for _, price := range slotPrices {
		spm.getOrCreateSlot(price)
	}
	// 格式化槽位價格用於日志输出
	slotPricesStr := make([]string, len(slotPrices))
	for i, p := range slotPrices {
		slotPricesStr[i] = formatPrice(p, spm.priceDecimals)
	}
	logger.Info("✅ [初始化] 计算出的槽位價格: %v", slotPricesStr)

	// 5. 為初始槽位下開倉單（LONG=買單，SHORT=賣單）或恢複持倉
	err := spm.placeInitialOpenOrders()
	if err == nil {
		// 標記為已初始化
		spm.isInitialized.Store(true)
		logger.Info("✅ 初始化完成，网格價格: %s", formatPrice(initialGridPrice, spm.priceDecimals))
	}
	return err
}

// generateClientOrderID 生成自定义订單ID
// 使用新的紧凑格式，最大长度不超過18字符
// 格式: {price_int}_{side}_{timestamp}{seq}
// price_int: price * 10^decimals (轉為整數)
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

	// 🔥 关键修複：不要對從ClientOrderID解析出的價格進行四舍五入！
	// 因為價格本身就是從整數还原的，已經是精确的值
	// 如果再次四舍五入，可能因為浮点數精度问题導致多個不同價格被映射到同一個槽位
	// 例如: 3116.85 和 3114.85 可能都被四舍五入成同一個值

	// 🔥 新增價格合理性检查：如果解析出的價格明显异常，記錄警告
	// 可能原因：
	// 1. priceDecimals 参數錯误
	// 2. 多交易對场景下，订單属於其他交易對（应該在上层過滤，但这里作為兜底检查）
	// 3. 历史遗留订單（切换交易對后的舊订單）
	if spm.anchorPrice > 1000 && price < 1000 && price > 0 {
		logger.Warn("⚠️ [價格解析异常] ClientOrderID=%s, 解析價格=%.2f, 锚点價格=%.2f, priceDecimals=%d",
			clientOrderID, price, spm.anchorPrice, spm.priceDecimals)
		logger.Warn("💡 [可能原因] 1) 此订單属於其他交易對 2) priceDecimals 参數錯误 3) 历史遗留订單")
		logger.Warn("💡 [建议] 检查是否运行了多個交易對，确保订單推送已正确過滤 Symbol")

		// 尝試使用不同的 priceDecimals 重新解析（用於诊断）
		for testDecimals := 1; testDecimals <= 3; testDecimals++ {
			if testDecimals == spm.priceDecimals {
				continue
			}
			testPrice, _, _, testValid := utils.ParseOrderID(cleanID, testDecimals)
			if testValid && testPrice > 1000 && math.Abs(testPrice-spm.anchorPrice) < spm.anchorPrice*0.5 {
				logger.Warn("⚠️ [價格解析修複] 使用 priceDecimals=%d 重新解析得到價格=%.2f", testDecimals, testPrice)
				return testPrice, side, true
			}
		}

		// 無法修複，返回無效（避免創建錯误的槽位）
		return 0, "", false
	}

	return price, side, true
}

// placeInitialOpenOrders 設定初始槽位（並恢複持倉槽位）
func (spm *SuperPositionManager) placeInitialOpenOrders() error {
	// 🔥 修改：只恢複持倉槽位，不再主动下單
	// 所有下單操作由 AdjustOrders 统一处理，避免時序问题
	existingPosition := spm.getExistingPosition()
	if existingPosition > 0 {
		if spm.isShort() {
			logger.Info("🔄 [持倉恢複] 检测到現有做空持倉: %.4f，开始初始化買單平倉槽位", existingPosition)
			spm.initializeBuySlotsFromPosition(existingPosition)
		} else {
			logger.Info("🔄 [持倉恢複] 检测到現有持倉: %.4f，开始初始化賣單槽位", existingPosition)
			spm.initializeSellSlotsFromPosition(existingPosition)
		}
	}

	logger.Info("✅ [初始化] 槽位已創建，订單下达將由 AdjustOrders 统一处理")
	return nil
}

// AdjustOrders 調整订單（交易入口）
func (spm *SuperPositionManager) AdjustOrders(currentPrice float64) error {
	// 🔥 移除初始化检查：現在完全由 AdjustOrders 控制所有下單
	// 初始化只负责恢複持倉状態，不再下單

	spm.mu.Lock()
	defer spm.mu.Unlock()

	// 检查是否暂停
	if spm.IsPaused() {
		logger.Debug("⏸️ [%s] 交易已暂停，跳過订單調整", spm.logPrefix())
		return nil
	}

	// 驗证價格有效性
	if currentPrice <= 0 {
		logger.Warn("⚠️ 收到無效價格: %.2f，跳過订單調整", currentPrice)
		return nil
	}

	// 對當前價格進行精度处理
	currentPrice = roundPrice(currentPrice, spm.priceDecimals)

	// 更新最后市场價格（用於打印状態）
	spm.lastMarketPrice.Store(currentPrice)

	// 觸發價格：未達到時不放置任何訂單
	if spm.config.Trading.TriggerPrice > 0 {
		trigger := spm.config.Trading.TriggerPrice
		if spm.isShort() {
			if currentPrice < trigger {
				logger.Debug("⏳ [觸發價] 當前價 %.2f < 觸發價 %.2f，等待後再啟動網格", currentPrice, trigger)
				return nil
			}
		} else {
			if currentPrice > trigger {
				logger.Debug("⏳ [觸發價] 當前價 %.2f > 觸發價 %.2f，等待後再啟動網格", currentPrice, trigger)
				return nil
			}
		}
	}

	// === 网格风控逻辑开始 ===
	if spm.config.Trading.GridRiskControl.Enabled {
		// 1. 硬為止损检查
		stopLossRatio := spm.config.Trading.GridRiskControl.StopLossRatio
		if stopLossRatio > 0 {
			unrealizedPnL := spm.calculateUnrealizedPnL(currentPrice)
			totalValue := spm.calculateTotalPositionValue(currentPrice)
			if totalValue > 0 {
				pnlRatio := unrealizedPnL / totalValue
				if pnlRatio <= -stopLossRatio {
					logger.Error("🚨 [网格风控] 触发硬為止损! 當前浮亏率: %.2f%%, 阈值: %.2f%%", pnlRatio*100, -stopLossRatio*100)
					spm.LiquidateAll()
					return nil
				}
			}
		}

		// 2. 动態止盈 (盈利回撤止盈) 检查
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

				// 如果盈利已經超過触发阈值，且從最高点回撤超過 trailingRatio
				if spm.peakPnL >= triggerRatio {
					drawdown := spm.peakPnL - currentProfitRatio
					if drawdown >= trailingRatio {
						logger.Warn("📈 [网格风控] 触发盈利回撤止盈! 最高盈利率: %.2f%%, 當前盈利率: %.2f%%, 回撤: %.2f%%, 阈值: %.2f%%",
							spm.peakPnL*100, currentProfitRatio*100, drawdown*100, trailingRatio*100)
						spm.LiquidateAll()
						spm.peakPnL = -math.MaxFloat64 // 重置最高点
						return nil
					}
				}
			} else {
				// 無持倉時重置最高盈利点
				spm.peakPnL = -math.MaxFloat64
			}
		}
	}
	// === 网格风控逻辑結束 ===

	// 检查保证金不足状態
	if spm.insufficientMargin {
		if time.Since(spm.marginLockTime) >= spm.marginLockDuration {
			logger.Info("✅ [保证金恢複] 鎖定時间已過，恢複下單功能")
			spm.insufficientMargin = false
		} else {
			remainingTime := spm.marginLockDuration - time.Since(spm.marginLockTime)
			logger.Warn("⏸️ [暂停下單] 保证金不足，暂停下單中... (剩餘時间: %.0f秒)", remainingTime.Seconds())
			return nil
		}
	}

	// 计算需要監控的價格範圍
	buyWindowSize := spm.config.Trading.BuyWindowSize
	sellWindowSize := spm.config.Trading.SellWindowSize
	profitSpread := spm.getEffectiveProfitSpread()

	// 动態计算网格價格
	currentGridPrice := spm.findNearestGridPrice(currentPrice)
	// logger.Debug("🔄 [實時調整] 當前價格: %s, 网格價格: %s, 買單窗口: %d, 賣單視窗: %d",
	// 	formatPrice(currentPrice, spm.priceDecimals), formatPrice(currentGridPrice, spm.priceDecimals), buyWindowSize, sellWindowSize)

	// 计算槽位價格：LONG 向下（買低賣高），SHORT 向上（賣高買低）
	slotDir := "down"
	if spm.isShort() {
		slotDir = "up"
	}
	slotPrices := spm.calculateSlotPrices(currentGridPrice, buyWindowSize, slotDir)

	// 價格範圍軟限制：將槽位價格裁剪到 [PriceLow, PriceHigh] 範圍內
	priceLow := spm.config.Trading.PriceLow
	priceHigh := spm.config.Trading.PriceHigh
	if priceLow > 0 || priceHigh > 0 {
		filtered := make([]float64, 0, len(slotPrices))
		for _, p := range slotPrices {
			if priceLow > 0 && p < priceLow {
				continue
			}
			if priceHigh > 0 && p > priceHigh {
				continue
			}
			filtered = append(filtered, p)
		}
		slotPrices = filtered
	}

	// 🔥 P2 新增：根據訂單簿深度優化槽位價格
	slotPrices = spm.optimizeSlotPricesWithOrderBook(context.Background(), spm.config.Trading.Symbol, slotPrices)

	var ordersToPlace []*OrderRequest
	var activeBuyOrdersInWindow int

	// 统计當前所有订單數量（分别统计買單和賣單）
	var currentOrderCount int
	var currentBuyOrderCount int
	var currentSellOrderCount int
	spm.slots.Range(func(key, value interface{}) bool {
		slot := value.(*InventorySlot)
		slot.mu.RLock()
		if slot.OrderStatus == OrderStatusPlaced || slot.OrderStatus == OrderStatusConfirmed ||
			slot.OrderStatus == OrderStatusPartiallyFilled {
			currentOrderCount++
			// LONG: 開倉=BUY 平倉=SELL；SHORT: 開倉=SELL 平倉=BUY
			openSide := "BUY"
			if spm.isShort() {
				openSide = "SELL"
			}
			if slot.OrderSide == openSide {
				currentBuyOrderCount++
			} else if slot.OrderSide != "" {
				currentSellOrderCount++
			}
		}
		slot.mu.RUnlock()
		return true
	})

	// 计算允許創建的订單數量上限
	threshold := spm.config.Trading.OrderCleanupThreshold
	if threshold <= 0 {
		threshold = 100
	}

	// 🔥 核心改進：不預留空间，允許订單數达到threshold上限
	// 剩餘可用订單數 = 阈值 - 當前订單數
	remainingOrders := threshold - currentOrderCount
	if remainingOrders < 0 {
		remainingOrders = 0
	}

	// 買單允許的新增數量
	allowedNewBuyOrders := buyWindowSize
	if allowedNewBuyOrders > remainingOrders {
		allowedNewBuyOrders = remainingOrders
	}

	// 1. 处理買單
	buyOrdersToCreate := 0

	// 趨勢過濾與层數限制預检查
	skipBuying := false
	// 價格範圍軟限制：超出範圍時暫停新開倉，保留平倉單
	if priceLow > 0 && currentPrice < priceLow {
		logger.Debug("⏸️ [價格範圍] 當前價 %.2f < 下限 %.2f，暫停新開倉", currentPrice, priceLow)
		skipBuying = true
	}
	if priceHigh > 0 && currentPrice > priceHigh {
		logger.Debug("⏸️ [價格範圍] 當前價 %.2f > 上限 %.2f，暫停新開倉", currentPrice, priceHigh)
		skipBuying = true
	}
	// 開倉管理：檢查是否暫停開倉
	if spm.IsOpeningPaused() {
		skipBuying = true
	}
	if spm.config.Trading.GridRiskControl.Enabled {
		// 趨勢過濾
		if spm.config.Trading.GridRiskControl.TrendFilterEnabled && spm.trendDetector != nil {
			trend := spm.trendDetector.GetCurrentTrend()
			if trend == "down" {
				logger.Warn("📉 [趨勢過濾] 检测到下跌趋势，暂停買入")
				skipBuying = true
			}
		}

		// 层數限制
		maxLayers := spm.config.Trading.GridRiskControl.MaxGridLayers
		if maxLayers > 0 {
			currentLayers := spm.GetActiveLayers()
			if currentLayers >= maxLayers {
				logger.Warn("🚫 [层數限制] 當前持倉层數 (%d) 已达到最大值 (%d)，暂停買入", currentLayers, maxLayers)
				skipBuying = true
			}
		}
	}

	// 🔥 開倉管理：檢查 Bot 獨立風控的倉位限制
	openControl := spm.config.Trading.OpenPositionControl

	// 一次性計算所有需要的倉位統計數據（避免多次遍歷）
	type positionStats struct {
		totalQty    float64
		totalValue  float64
		totalLayers int
	}
	stats := positionStats{}
	spm.slots.Range(func(key, value interface{}) bool {
		slot := value.(*InventorySlot)
		slot.mu.RLock()
		if slot.PositionStatus == PositionStatusFilled && slot.PositionQty > 0 {
			stats.totalQty += slot.PositionQty
			stats.totalLayers++
		}
		slot.mu.RUnlock()
		return true
	})
	stats.totalValue = stats.totalQty * currentPrice

	// 優先檢查 Bot 獨立風控
	if openControl.BotRiskControl != nil && openControl.BotRiskControl.Enabled {
		// 檢查暫停狀態
		if openControl.BotRiskControl.PauseOpening {
			logger.Warn("⏸️ [Bot風控] Bot 開倉已暫停（原因: %s）", openControl.BotRiskControl.PauseOpeningReason)
			skipBuying = true
		}

		// 檢查數量限制
		if openControl.BotRiskControl.MaxPositionQuantity > 0 && stats.totalQty >= openControl.BotRiskControl.MaxPositionQuantity {
			logger.Warn("🚫 [Bot風控] 當前持倉數量 (%.4f) 已達到 Bot 限制 (%.4f)，暂停開倉",
				stats.totalQty, openControl.BotRiskControl.MaxPositionQuantity)
			skipBuying = true
		}

		// 檢查價值限制
		if openControl.BotRiskControl.MaxPositionValue > 0 && stats.totalValue >= openControl.BotRiskControl.MaxPositionValue {
			logger.Warn("🚫 [Bot風控] 當前倉位價值 (%.2f) 已達到 Bot 限制 (%.2f)，暂停開倉",
				stats.totalValue, openControl.BotRiskControl.MaxPositionValue)
			skipBuying = true
		}

		// 檢查層數限制
		if openControl.BotRiskControl.MaxPositionLayers > 0 && stats.totalLayers >= openControl.BotRiskControl.MaxPositionLayers {
			logger.Warn("🚫 [Bot風控] 當前持倉層數 (%d) 已達到 Bot 限制 (%d)，暂停開倉",
				stats.totalLayers, openControl.BotRiskControl.MaxPositionLayers)
			skipBuying = true
		}
	} else {
		// 如果沒有啟用 Bot 獨立風控，檢查全局配置
		if openControl.MaxPositionQuantity > 0 && stats.totalQty >= openControl.MaxPositionQuantity {
			logger.Warn("🚫 [開倉管理] 當前持倉數量 (%.4f) 已達到限制 (%.4f)，暂停開倉",
				stats.totalQty, openControl.MaxPositionQuantity)
			skipBuying = true
		}

		if openControl.MaxPositionValue > 0 && stats.totalValue >= openControl.MaxPositionValue {
			logger.Warn("🚫 [開倉管理] 當前倉位價值 (%.2f) 已達到限制 (%.2f)，暂停開倉",
				stats.totalValue, openControl.MaxPositionValue)
			skipBuying = true
		}

		if openControl.MaxPositionLayers > 0 && stats.totalLayers >= openControl.MaxPositionLayers {
			logger.Warn("🚫 [開倉管理] 當前持倉層數 (%d) 已達到限制 (%d)，暂停開倉",
				stats.totalLayers, openControl.MaxPositionLayers)
			skipBuying = true
		}
	}

	// 資金費率偏向策略檢查
	if spm.fundingMonitor != nil && spm.config.FundingRate.BiasEnabled {
		buyBias := spm.fundingMonitor.GetBuyBias()

		if buyBias == 0 {
			// 極高費率：完全暫停買入
			rate := spm.fundingMonitor.GetCurrentRate()
			logger.Warn("💰 [資金費率] 費率過高 (%.4f%%)，暫停買入", rate*100)
			skipBuying = true
		} else if buyBias < 1.0 {
			// 高費率：減少買單數量
			originalOrders := allowedNewBuyOrders
			allowedNewBuyOrders = int(float64(allowedNewBuyOrders) * buyBias)
			if allowedNewBuyOrders < 1 && originalOrders > 0 {
				allowedNewBuyOrders = 1 // 至少保留一個買單
			}
			rate := spm.fundingMonitor.GetCurrentRate()
			logger.Info("💰 [資金費率] 費率 %.4f%%，買單數量從 %d 減少到 %d (偏向係數: %.2f)",
				rate*100, originalOrders, allowedNewBuyOrders, buyBias)
		} else if buyBias > 1.0 {
			// 負費率：可略微增加買入（但不超過剩餘訂單數）
			originalOrders := allowedNewBuyOrders
			allowedNewBuyOrders = int(float64(allowedNewBuyOrders) * buyBias)
			if allowedNewBuyOrders > remainingOrders {
				allowedNewBuyOrders = remainingOrders
			}
			rate := spm.fundingMonitor.GetCurrentRate()
			logger.Info("💰 [資金費率] 負費率 %.4f%%，買單數量從 %d 增加到 %d (偏向係數: %.2f)",
				rate*100, originalOrders, allowedNewBuyOrders, buyBias)
		}
	}

	// 🔥 P1 新增：資金費率與趨勢聯動邏輯
	if spm.config.FundingRate.TrendSyncEnabled &&
		spm.fundingMonitor != nil && spm.trendDetector != nil &&
		spm.config.FundingRate.BiasEnabled && spm.config.Trading.GridRiskControl.TrendFilterEnabled {

		buyBias := spm.fundingMonitor.GetBuyBias()
		trend := spm.trendDetector.GetCurrentTrend()

		if buyBias > 1 && trend == "up" {
			// 負費率 + 上漲趨勢：只放寬趨勢過濾限制，不再重複乘係數（之前已乘過 buyBias）
			if skipBuying {
				skipBuying = false
				if allowedNewBuyOrders == 0 {
					allowedNewBuyOrders = 1
				}
				logger.Info("🔥 [費率趨勢聯動] 負費率(%.2f) + 上漲趨勢：放寬趨勢過濾限制", buyBias)
			}
		} else if buyBias < 1 && trend == "down" {
			// 高正費率 + 下跌趨勢：強化賣出偏向
			skipBuying = true
			allowedNewBuyOrders = 0
			rate := spm.fundingMonitor.GetCurrentRate()
			logger.Warn("🔥 [費率趨勢聯動] 高費率(%.4f%%) + 下跌趨勢：強制暫停買入", rate*100)
		}
	}

	// 最大持倉預警：達到層數上限時，若開倉單數超過允許值，先撤多餘的開倉單（做多先撤高價買單，做空先撤低價賣單）
	if spm.config.Trading.GridRiskControl.Enabled {
		maxLayers := spm.config.Trading.GridRiskControl.MaxGridLayers
		maxOpenAtCap := spm.config.Trading.GridRiskControl.MaxOpenOrdersAtCap
		if maxLayers > 0 && maxOpenAtCap > 0 {
			currentLayers := spm.GetActiveLayers()
			if currentLayers >= maxLayers && currentBuyOrderCount > maxOpenAtCap {
				spm.CancelExcessOpenOrders(maxOpenAtCap)
			}
		}
	}

	for _, price := range slotPrices {
		if skipBuying {
			break
		}

		// 🔥 新增：槽位過濾檢查
		if !spm.isSlotEnabled(price) {
			logger.Debug("⏭️ [槽位過濾] 跳過被禁用的價格位: %.2f", price)
			continue
		}

		slot := spm.getOrCreateSlot(price)
		slot.mu.Lock()

		// 🔥 槽位鎖定检查：如果槽位正在被操作，跳過
		if slot.SlotStatus != SlotStatusFree {
			slot.mu.Unlock()
			continue
		}

		// 检查是否已有有效订單
		hasActiveOrder := false
		if slot.OrderStatus == OrderStatusPlaced || slot.OrderStatus == OrderStatusConfirmed ||
			slot.OrderStatus == OrderStatusPartiallyFilled {
			hasActiveOrder = true
			openSide := "BUY"
			if spm.isShort() {
				openSide = "SELL"
			}
			if slot.OrderSide == openSide {
				activeBuyOrdersInWindow++
			}
		}

		// 🔥 買單条件：持倉状態=EMPTY + 槽位鎖=FREE + 無订單ID + 無ClientOID
		if slot.PositionStatus != PositionStatusEmpty {
			slot.mu.Unlock()
			continue
		}

		// 🔥 新逻辑：只检查槽位鎖状態、OrderID和ClientOID，不检查OrderSide
		shouldCreateBuyOrder := !hasActiveOrder &&
			slot.SlotStatus == SlotStatusFree &&
			slot.OrderID == 0 &&
			slot.ClientOID == "" &&
			buyOrdersToCreate < allowedNewBuyOrders

		if shouldCreateBuyOrder {
			// 安全检查：LONG 買單價格應低於當前價格；SHORT 賣單價格應高於當前價格
			safetyBuffer := spm.config.Trading.PriceInterval * 0.1
			if spm.isShort() {
				if price <= currentPrice+safetyBuffer {
					slot.mu.Unlock()
					continue
				}
			} else {
				if price >= currentPrice-safetyBuffer {
					slot.mu.Unlock()
					continue
				}
			}

			quantity := spm.config.Trading.OrderQuantity / price
			// 使用從交易所獲取的數量精度
			quantity = roundPrice(quantity, spm.quantityDecimals)

			// 如果數量過小被取整為 0，发布告警並暂停
			if quantity <= 0 && spm.quantityDecimals >= 0 {
				minQty := math.Pow10(-spm.quantityDecimals)
				logger.Error("🚨 [%s] 下單數量過小 (%.8f)，低於交易所最小精度 (%.8f)，交易已自动暂停！请在配置中調大 order_quantity",
					spm.config.Trading.Symbol, spm.config.Trading.OrderQuantity/price, minQty)

				// 发布事件
				if spm.eventBus != nil {
					spm.eventBus.Publish(&event.Event{
						Type:      event.EventTypePrecisionAdjustment,
						Timestamp: time.Now(),
						Data: map[string]interface{}{
							"symbol":         spm.config.Trading.Symbol,
							"exchange":       spm.exchangeName,
							"order_quantity": spm.config.Trading.OrderQuantity,
							"calculated_qty": spm.config.Trading.OrderQuantity / price,
							"min_qty":        minQty,
							"price":          price,
							"action":         "pause",
							"reason":         "下單數量低於交易所最小精度",
						},
					})
				}

				// 暂停交易
				spm.Pause()
				slot.mu.Unlock()
				continue
			}

			// 生成 ClientOrderID：LONG=BUY，SHORT=SELL
			openSide := "BUY"
			if spm.isShort() {
				openSide = "SELL"
			}
			clientOID := spm.generateClientOrderID(price, openSide)

			// 🔥 鎖定槽位：標記為PENDING状態，防止並发操作
			slot.SlotStatus = SlotStatusPending

			// 检查PostOnly失败计數，失败3次后不再使用PostOnly
			usePostOnly := slot.PostOnlyFailCount < 3

			ordersToPlace = append(ordersToPlace, &OrderRequest{
				Symbol:        spm.config.Trading.Symbol,
				Side:          openSide,
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

	// 2. 处理平倉單（LONG=賣單，SHORT=買單）
	type closeCandidate struct {
		SlotPrice     float64
		ClosePrice    float64 // LONG: 賣出價=slot+interval；SHORT: 買入價=slot-interval
		Quantity      float64
		DistanceToMid float64
	}
	var closeCandidates []closeCandidate

	// LONG: 賣單窗口 above；SHORT: 買單窗口 below（窗口範圍用 profitSpread）
	sellWindowMaxPrice := currentPrice + float64(sellWindowSize)*profitSpread
	sellWindowMaxPrice = roundPrice(sellWindowMaxPrice, spm.priceDecimals)
	buyWindowMinPrice := currentPrice - float64(sellWindowSize)*profitSpread
	buyWindowMinPrice = roundPrice(buyWindowMinPrice, spm.priceDecimals)

	spm.slots.Range(func(key, value interface{}) bool {
		slotPrice := key.(float64)
		slot := value.(*InventorySlot)
		slot.mu.Lock()
		defer slot.mu.Unlock()

		if slot.PositionStatus == PositionStatusFilled &&
			slot.SlotStatus == SlotStatusFree &&
			slot.OrderID == 0 &&
			slot.ClientOID == "" {

			var closePrice float64
			if spm.isShort() {
				closePrice = slotPrice - profitSpread // SHORT: 買低平倉
			} else {
				closePrice = slotPrice + profitSpread // LONG: 賣高平倉
			}
			closePrice = roundPrice(closePrice, spm.priceDecimals)

			// 窗口检查：LONG 跳過 slot 高於上限；SHORT 跳過 close 低於下限
			if spm.isShort() {
				if closePrice < buyWindowMinPrice {
					return true
				}
			} else {
				if slotPrice > sellWindowMaxPrice {
					return true
				}
			}

			// 最小名义價值检查
			orderValue := closePrice * slot.PositionQty
			minValue := spm.config.Trading.MinOrderValue
			if minValue <= 0 {
				minValue = 6.0
			}

			if orderValue >= minValue {
				distance := math.Abs(slotPrice - currentPrice)
				closeCandidates = append(closeCandidates, closeCandidate{
					SlotPrice:     slotPrice,
					ClosePrice:    closePrice,
					Quantity:      slot.PositionQty,
					DistanceToMid: distance,
				})
			}
		}
		return true
	})

	// 按距离排序
	sort.Slice(closeCandidates, func(i, j int) bool {
		return closeCandidates[i].DistanceToMid < closeCandidates[j].DistanceToMid
	})

	// 🔥 重新计算賣單的剩餘配額（扣除新增買單后的剩餘空间）
	remainingOrdersForSell := threshold - currentOrderCount - buyOrdersToCreate
	if remainingOrdersForSell < 0 {
		remainingOrdersForSell = 0
	}

	allowedNewSellOrders := sellWindowSize
	if allowedNewSellOrders > remainingOrdersForSell {
		allowedNewSellOrders = remainingOrdersForSell
	}

	// 生成賣單请求
	sellOrdersToCreate := 0
	// 🔥 調試日志: 显示订單配額计算详情（包含買賣單分布），含 bot ID 便於區分多實例
	logger.Debug("📊 [%s] [订單配額] 阈值:%d, 當前订單:%d(開:%d/平:%d), 剩餘:%d, 新增開倉:%d, 平倉候选:%d, 允許平倉:%d",
		spm.logPrefix(), threshold, currentOrderCount, currentBuyOrderCount, currentSellOrderCount, remainingOrders, buyOrdersToCreate, len(closeCandidates), allowedNewSellOrders)
	if allowedNewSellOrders > 0 {
		closeSide := "SELL"
		if spm.isShort() {
			closeSide = "BUY"
		}
		for i := 0; i < len(closeCandidates) && sellOrdersToCreate < allowedNewSellOrders; i++ {
			candidate := closeCandidates[i]

			// 🔥 关键修複：最终驗证PositionStatus必須為FILLED且有持倉，並且SlotStatus為FREE
			slot := spm.getOrCreateSlot(candidate.SlotPrice)
			slot.mu.Lock()

			// 🔥 双重检查：确保槽位仍然是FREE状態
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

			// 🔥 立即鎖定槽位：標記為PENDING状態，防止並发操作
			slot.SlotStatus = SlotStatusPending
			// 检查PostOnly失败计數，失败3次后不再使用PostOnly
			usePostOnly := slot.PostOnlyFailCount < 3
			slot.mu.Unlock()

			// 生成 ClientOrderID
			clientOID := spm.generateClientOrderID(candidate.SlotPrice, closeSide)

			quantity := candidate.Quantity
			// 兜底检查：平倉單數量必須大於0
			if quantity <= 0 && spm.quantityDecimals >= 0 {
				minQty := math.Pow10(-spm.quantityDecimals)
				logger.Error("🚨 [%s] 平倉單數量异常 (%.8f)，低於交易所最小精度 (%.8f)，交易已自动暂停！",
					spm.config.Trading.Symbol, candidate.Quantity, minQty)

				// 发布事件
				if spm.eventBus != nil {
					spm.eventBus.Publish(&event.Event{
						Type:      event.EventTypePrecisionAdjustment,
						Timestamp: time.Now(),
						Data: map[string]interface{}{
							"symbol":   spm.config.Trading.Symbol,
							"exchange": spm.exchangeName,
							"quantity": candidate.Quantity,
							"min_qty":  minQty,
							"price":    candidate.ClosePrice,
							"action":   "pause",
							"reason":   "平倉單數量低於交易所最小精度",
						},
					})
				}

				// 暂停交易（slot 已在前面 unlock）
				spm.Pause()
				continue
			}

			ordersToPlace = append(ordersToPlace, &OrderRequest{
				Symbol:        spm.config.Trading.Symbol,
				Side:          closeSide,
				Price:         candidate.ClosePrice,
				Quantity:      quantity,
				PriceDecimals: spm.priceDecimals,
				ReduceOnly:    !spm.isSpot(), // 平倉單需要 ReduceOnly
				PostOnly:      usePostOnly,
				ClientOrderID: clientOID,
			})
			sellOrdersToCreate++
		}
	}

	// 🔥 去重检查：如果同一價格同時有開倉單和平倉單，移除開倉單（平倉優先）
	// 場景：LONG模式下，空倉槽位P挂買單，同時已持倉槽位(P-interval)的平倉價也是P
	// 同價掛買賣單毫無意義，且可能觸發自成交，因此移除開倉單
	openSideForDedup := "BUY"
	if spm.isShort() {
		openSideForDedup = "SELL"
	}
	closePriceSet := make(map[float64]bool)
	for _, order := range ordersToPlace {
		if order.Side != openSideForDedup {
			closePriceSet[order.Price] = true
		}
	}
	if len(closePriceSet) > 0 {
		var filteredOrders []*OrderRequest
		removedBuyCount := 0
		for _, order := range ordersToPlace {
			if order.Side == openSideForDedup && closePriceSet[order.Price] {
				// 同一價格有平倉單，跳過開倉單
				logger.Warn("⚠️ [%s] 同一價格 %s 同時有開倉和平倉單，移除開倉單（平倉優先）",
					spm.logPrefix(), formatPrice(order.Price, spm.priceDecimals))
				// 重置被移除的開倉單對應槽位狀態（之前被標記為PENDING）
				if slotRaw, ok := spm.slots.Load(order.Price); ok {
					pendingSlot := slotRaw.(*InventorySlot)
					pendingSlot.mu.Lock()
					if pendingSlot.SlotStatus == SlotStatusPending {
						pendingSlot.SlotStatus = SlotStatusFree
					}
					pendingSlot.mu.Unlock()
				}
				removedBuyCount++
				buyOrdersToCreate--
				continue
			}
			filteredOrders = append(filteredOrders, order)
		}
		if removedBuyCount > 0 {
			ordersToPlace = filteredOrders
			logger.Info("📊 [%s] 去重完成：移除了 %d 個與平倉單同價的開倉單",
				spm.logPrefix(), removedBuyCount)
		}
	}

	// 🔥 在下單前，先检查並調整资金限額（分级限額功能）
	// 计算當前持倉层數和未實現盈亏
	positionLayers := 0
	unrealizedPnL := 0.0
	spm.slots.Range(func(key, value interface{}) bool {
		slot := value.(*InventorySlot)
		slot.mu.RLock()
		if slot.PositionStatus == PositionStatusFilled && slot.PositionQty > 0 {
			positionLayers++
			// 计算未實現盈亏
			if currentPrice > 0 && slot.Price > 0 {
				unrealizedPnL += (currentPrice - slot.Price) * slot.PositionQty
			}
		}
		slot.mu.RUnlock()
		return true
	})

	// 調用分级限額检查（可能會自动切换到紧急限額或恢複正常限額）
	spm.allocationManager.CheckAndAdjustLimit(
		spm.exchangeName,
		spm.config.Trading.Symbol,
		currentPrice,
		spm.anchorPrice,
		positionLayers,
		unrealizedPnL,
	)

	// 執行下單前，检查资金分配
	if len(ordersToPlace) > 0 {
		// 獲取帳戶餘額（從交易所獲取實際餘額）
		var accountBalance float64 = 0
		var accountResult interface{} = nil
		ctx := context.Background()
		if spm.exchange != nil {
			var err error
			accountResult, err = spm.exchange.GetAccount(ctx)
			if err == nil && accountResult != nil {
				// 使用反射獲取 AvailableBalance 字段
				// 注意：不同交易所可能返回不同的類型，使用反射统一处理
				accountValue := reflect.ValueOf(accountResult)
				if accountValue.Kind() == reflect.Ptr {
					accountValue = accountValue.Elem()
				}
				if balanceField := accountValue.FieldByName("AvailableBalance"); balanceField.IsValid() && balanceField.CanInterface() {
					if balance, ok := balanceField.Interface().(float64); ok {
						accountBalance = balance
					}
				}
				// 使用可用餘額（AvailableBalance）進行资金分配检查
				// 注意：對於合約账戶，如果有持倉，AvailableBalance可能為0，这是正常的
				logger.Debug("💰 [%s] [资金分配] 账戶可用餘額: %.2f USDT", spm.logPrefix(), accountBalance)
			} else {
				logger.Warn("⚠️ [%s] [资金分配] 無法獲取帳戶餘額: %v，使用0作為默认值", spm.logPrefix(), err)
			}
		}

		// 獲取杠杆倍數（用於计算實際使用的保证金）
		leverage := 1 // 默认1倍（無杠杆）
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
		// 如果從账戶中獲取不到，尝試從持倉中獲取
		if leverage == 1 && spm.exchange != nil {
			if positionsInterface, err := spm.exchange.GetPositions(ctx, spm.config.Trading.Symbol); err == nil && positionsInterface != nil {
				// 使用反射處理不同類型的持倉資訊
				positionsValue := reflect.ValueOf(positionsInterface)
				if positionsValue.Kind() == reflect.Slice {
					for i := 0; i < positionsValue.Len(); i++ {
						posValue := positionsValue.Index(i)
						if posValue.Kind() == reflect.Interface {
							posValue = posValue.Elem()
						}
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

		// 過滤掉超出资金分配的订單
		var validOrders []*OrderRequest
		for _, req := range ordersToPlace {
			orderValue := req.Quantity * req.Price // 订單名义金額（倉位價值）
			// 對於有杠杆的交易，實際使用的保证金 = 訂單價值 / 杠杆倍數
			// 资金限額限制的是實際投入的资金，而不是倉位價值
			actualMargin := orderValue / float64(leverage)
			err := spm.allocationManager.CheckAndReserve(
				spm.exchangeName,
				spm.config.Trading.Symbol,
				actualMargin, // 使用實際保证金而不是訂單價值
				accountBalance,
			)

			if err != nil {
				logger.Warn("⚠️ [%s] [资金分配] %v (訂單價值: %.2f USDT, 實際保证金: %.2f USDT, 杠杆: %dx)",
					spm.logPrefix(), err, orderValue, actualMargin, leverage)
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
				// 释放槽位鎖
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

	// 執行下單
	if len(ordersToPlace) > 0 {
		logger.Debug("🔄 [%s] [實時調整] 需要新增: %d 個订單", spm.logPrefix(), len(ordersToPlace))
		result := spm.executor.BatchPlaceOrdersWithDetails(ordersToPlace)

		if result.HasMarginError {
			errLabel := "保证金不足"
			if spm.isSpot() {
				errLabel = "餘額不足"
			}
			logger.Warn("⚠️ [%s] 检测到錯误，暂停下單 %d 秒", errLabel, int(spm.marginLockDuration.Seconds()))
			spm.insufficientMargin = true
			spm.marginLockTime = time.Now()
			spm.CancelAllBuyOrders()

			// 发送保证金/餘額不足告警事件
			if spm.eventBus != nil {
				spm.eventBus.Publish(&event.Event{
					Type: event.EventTypeMarginInsufficient,
					Data: map[string]interface{}{
						"exchange":      spm.exchangeName,
						"symbol":        spm.config.Trading.Symbol,
						"failed_orders": len(result.PlacedOrders),
						"error_message": errLabel + "，已暂停下單",
						"lock_duration": int(spm.marginLockDuration.Seconds()),
					},
				})
			}
		}

		// 🔥 構建成功订單的ClientOrderID集合
		placedClientOIDs := make(map[string]bool)
		for _, ord := range result.PlacedOrders {
			placedClientOIDs[ord.ClientOrderID] = true
		}

		// 🔥 处理 ReduceOnly 錯误：清空對应槽位的持倉
		for clientOID := range result.ReduceOnlyErrors {
			price, side, valid := spm.parseClientOrderID(clientOID)
			if valid {
				if side == "SELL" {
					// SELL ReduceOnly：平多倉失败，清空槽位持倉状態
					slot := spm.getOrCreateSlot(price)
					slot.mu.Lock()
					if slot.PositionStatus == PositionStatusFilled {
						logger.Warn("⚠️ [ReduceOnly錯誤處理] 清空槽位持倉: 價格=%s, 原持倉=%.4f",
							formatPrice(price, spm.priceDecimals), slot.PositionQty)
						// 清空持倉状態
						slot.PositionStatus = PositionStatusEmpty
						slot.PositionQty = 0
						slot.SlotStatus = SlotStatusFree
					}
					slot.mu.Unlock()
				} else if side == "BUY" {
					// BUY ReduceOnly：平空倉失败，账戶中無空倉（系统不管理空倉状態，僅記錄日志）
					logger.Warn("⚠️ [ReduceOnly錯誤處理] BUY平空倉订單被拒绝: 價格=%s, 账戶中無空倉",
						formatPrice(price, spm.priceDecimals))
				}
			}
		}

		// 🔥 释放未成功提交订單的槽位鎖和资金
		for _, req := range ordersToPlace {
			if !placedClientOIDs[req.ClientOrderID] && !result.ReduceOnlyErrors[req.ClientOrderID] {
				// 這個订單没有成功提交（且不是ReduceOnly錯误，因為已經处理過了），需要释放槽位鎖和资金
				price, side, valid := spm.parseClientOrderID(req.ClientOrderID)
				if valid {
					slot := spm.getOrCreateSlot(price)
					slot.mu.Lock()
					if slot.SlotStatus == SlotStatusPending {
						slot.SlotStatus = SlotStatusFree
						logger.Debug("🔓 [释放槽位] 订單提交失败，释放槽位 %s 的鎖 (ClientOID: %s)",
							formatPrice(price, spm.priceDecimals), req.ClientOrderID)
					}
					slot.mu.Unlock()

					// 🔥 释放預留的资金（只有買單需要释放，賣單不占用资金）
					if side == "BUY" {
						orderValue := req.Quantity * req.Price
						actualMargin := spm.getActualMargin(orderValue)
						if actualMargin > 0 {
							spm.allocationManager.Release(spm.exchangeName, spm.config.Trading.Symbol, actualMargin)
							logger.Debug("💰 [资金释放] 订單提交失败，释放預留资金: %.2f USDT (訂單價值: %.2f USDT)", actualMargin, orderValue)
						}
					}
				}
			}
		}

		for _, ord := range result.PlacedOrders {
			// 解析 ClientOrderID
			price, side, valid := spm.parseClientOrderID(ord.ClientOrderID)

			if !valid {
				logger.Warn("⚠️ [%s] [實時調整] 無法解析 ClientOID: %s", spm.logPrefix(), ord.ClientOrderID)
				continue
			}

			// 獲取槽位 (注意：無論是買單还是賣單，ID中编碼的都是 SlotPrice)
			slot := spm.getOrCreateSlot(price)
			slot.mu.Lock()

			// 🔥 关键修複：检查是否是秒成交场景（買單或賣單都可能）
			// 秒成交的特征:
			// 1. 買單秒成交: PositionStatus=FILLED (刚成交) 且 OrderID=0 (已被WebSocket清空) 且 OrderSide=""
			// 2. 賣單秒成交: PositionStatus=EMPTY (已清空) 且 OrderID=0 (已被WebSocket清空) 且 OrderSide=""
			isInstantFill := false
			if side == "BUY" {
				// 買單秒成交: 有持倉但订單ID為0且OrderSide已清空
				isInstantFill = (slot.PositionStatus == PositionStatusFilled && slot.OrderID == 0 && slot.OrderSide == "")
			} else if side == "SELL" {
				// 🔥 賣單秒成交: 持倉已清空且订單ID為0且OrderSide已清空
				isInstantFill = (slot.PositionStatus == PositionStatusEmpty && slot.OrderID == 0 && slot.OrderSide == "" && slot.SlotStatus == SlotStatusFree)
			}

			if !isInstantFill {
				// 正常情况: 更新订單状態
				// 🔥 检查OrderID冲突：只有當ClientOID已設置且不匹配時才是真正的冲突
				// 如果ClientOID為空或匹配，說明是正常的WebSocket先到或批量处理顺序问题
				if slot.OrderID != 0 && slot.OrderID != ord.OrderID {
					if slot.ClientOID != "" && slot.ClientOID != ord.ClientOrderID {
						// 真正的冲突：槽位已被其他订單占用
						logger.Warn("⚠️ [OrderID冲突] 槽位 %.2f: 下單返回OrderID=%d (ClientOID=%s)，但槽位已被OrderID=%d (ClientOID=%s)占用",
							price, ord.OrderID, ord.ClientOrderID, slot.OrderID, slot.ClientOID)
					} else {
						// WebSocket推送先到达，这是正常現象
						logger.Debug("📝 [覆盖OrderID] 槽位 %.2f: WebSocket已設置OrderID=%d，現用下單返回的OrderID=%d (ClientOID: %s)",
							price, slot.OrderID, ord.OrderID, ord.ClientOrderID)
					}
				}

				slot.OrderID = ord.OrderID
				slot.ClientOID = ord.ClientOrderID
				slot.OrderSide = side // "BUY" or "SELL"
				slot.OrderStatus = OrderStatusPlaced
				slot.OrderPrice = ord.Price
				slot.OrderCreatedAt = time.Now()
				// 🔥 订單提交成功，設置為LOCKED状態
				slot.SlotStatus = SlotStatusLocked
				// 保存策略信息
				slot.StrategyName = spm.strategyName
				slot.StrategyType = spm.strategyType
				// 注意：不在这里重置PostOnlyFailCount，因為订單可能立即被撤销
				// PostOnly计數只在订單真正成交時重置

				logger.Debug("✅ [實時新增] 槽位價格: %s, %s订單, 订單價格: %s, 订單ID: %d, ClientOID: %s",
					formatPrice(price, spm.priceDecimals), side, formatPrice(ord.Price, spm.priceDecimals), ord.OrderID, ord.ClientOrderID)
			} else {
				// 🔍 秒成交场景：WebSocket已經处理了FILLED,跳過状態更新
				logger.Debug("🔍 [%s單秒成交] 槽位 %s 的订單已被WebSocket处理，跳過状態更新 (持倉: %.4f, SlotStatus: %s)",
					side, formatPrice(price, spm.priceDecimals), slot.PositionQty, slot.SlotStatus)
			}

			slot.mu.Unlock()
		}
	}

	return nil
}

// OnOrderUpdate 订單更新回呼（异步订單同步流）
func (spm *SuperPositionManager) OnOrderUpdate(update OrderUpdate) {
	// 🔥 重構：完全依赖 ClientOrderID 解析
	price, side, valid := spm.parseClientOrderID(update.ClientOrderID)

	if !valid {
		logger.Debug("⏳ [忽略] 無法识别的订單更新: ID=%d, ClientOID=%s", update.OrderID, update.ClientOrderID)
		return
	}

	slot := spm.getOrCreateSlot(price)
	slot.mu.Lock()
	defer slot.mu.Unlock()

	// 校驗：确保這個更新属於當前的订單 (防止舊订單的延迟推送干扰新订單)
	// 优先使用 ClientOrderID 匹配 (某些交易所如 Gate.io 的 OrderID 可能略有差异)
	if slot.ClientOID != "" && slot.ClientOID != update.ClientOrderID {
		// ClientOrderID 不匹配，忽略此更新
		logger.Info("⚠️ [订單更新被忽略] 槽位 %.2f: ClientOID不匹配 (槽位: %s, 推送: %s, OrderID: %d)",
			price, slot.ClientOID, update.ClientOrderID, update.OrderID)
		return
	}

	// 更新订單ID (如果是首個推送)
	if slot.OrderID == 0 {
		logger.Debug("📝 [首次設置OrderID] 槽位 %.2f: OrderID=%d, ClientOID=%s", price, update.OrderID, update.ClientOrderID)
		slot.OrderID = update.OrderID
		slot.ClientOID = update.ClientOrderID
		slot.OrderSide = side
	} else if slot.OrderID != update.OrderID {
		// OrderID 不一致但 ClientOrderID 匹配，更新 OrderID (Gate.io 批量下單可能出現此情况)
		logger.Debug("📝 [更新OrderID] 槽位 %.2f: %d -> %d (ClientOID: %s)", price, slot.OrderID, update.OrderID, update.ClientOrderID)
		slot.OrderID = update.OrderID
	}

	// 处理状態轉换
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

		// 根據方向更新持倉：LONG 時 BUY=開倉(加倉) SELL=平倉(減倉)；SHORT 時 SELL=開倉 BUY=平倉
		openSide := "BUY"
		if spm.isShort() {
			openSide = "SELL"
		}
		if side == openSide {
			if deltaQty > 0 {
				// 🔥 更新平均买入价格（使用实际成交价格）
				actualBuyPrice := update.AvgPrice
				if actualBuyPrice <= 0 {
					actualBuyPrice = update.Price
				}
				if actualBuyPrice <= 0 {
					actualBuyPrice = slot.OrderPrice
				}
				
				// 🔥 监控价格偏差：实际成交价格与委托价格的差异
				if slot.OrderPrice > 0 && actualBuyPrice > 0 {
					priceDeviation := (actualBuyPrice - slot.OrderPrice) / slot.OrderPrice * 100
					// 如果价格偏差超过0.1%（买入价格高于委托价格），记录警告
					if priceDeviation > 0.1 {
						logger.Warn("⚠️ [價格偏差警告] 買單實際成交價高於委託價: 委託價=%.2f, 實際價=%.2f, 偏差=%.4f%%, 數量=%.4f, OrderID=%d",
							slot.OrderPrice, actualBuyPrice, priceDeviation, deltaQty, update.OrderID)
					} else if priceDeviation < -0.1 {
						// 买入价格低于委托价格（有利偏差），记录信息
						logger.Info("💰 [價格偏差] 買單實際成交價低於委託價（有利）: 委託價=%.2f, 實際價=%.2f, 偏差=%.4f%%, 數量=%.4f",
							slot.OrderPrice, actualBuyPrice, priceDeviation, deltaQty)
					}
				}
				
				// 计算新的平均买入价格
				if slot.PositionQty > 0 && slot.AvgBuyPrice > 0 {
					// 加权平均：(旧价格 * 旧数量 + 新价格 * 新数量) / 总数量
					totalCost := slot.AvgBuyPrice*slot.PositionQty + actualBuyPrice*deltaQty
					slot.AvgBuyPrice = totalCost / (slot.PositionQty + deltaQty)
				} else {
					// 首次买入或之前没有持仓，直接使用当前买入价格
					slot.AvgBuyPrice = actualBuyPrice
				}
				
				slot.PositionQty += deltaQty
				// 累加统计
				oldTotal := spm.totalBuyQty.Load().(float64)
				spm.totalBuyQty.Store(oldTotal + deltaQty)
			}

			if update.Status == "FILLED" {
				slot.OrderStatus = OrderStatusNotPlaced // 重置订單状態
				slot.OrderID = 0
				slot.ClientOID = ""
				slot.OrderSide = "" // 🔥 清除订單方向，避免误判
				slot.OrderFilledQty = 0

				slot.PositionStatus = PositionStatusFilled // 標記為有倉
				// 🔥 累計買入手續費（賣出時按比例攤銷）
				slot.BuyFee += update.Commission
				if update.CommissionAsset != "" {
					slot.FeeAsset = update.CommissionAsset
				}
				// 🔥 如果 WebSocket 未提供手續費，異步查詢補充
				if update.Commission == 0 && update.OrderID > 0 {
					go spm.supplementCommission(context.Background(), update.OrderID, update.Symbol, side, slot)
				}
				// 🔥 释放槽位鎖：買單成交，允許后续挂賣單
				slot.SlotStatus = SlotStatusFree
				// 🔥 買單成交，重置PostOnly失败计數
				slot.PostOnlyFailCount = 0

				// 🔥 释放资金：買單成交后，资金已轉换為持倉，释放預留的资金
				orderValue := slot.OrderPrice * update.ExecutedQty
				actualMargin := spm.getActualMargin(orderValue)
				if actualMargin > 0 {
					spm.allocationManager.Release(spm.exchangeName, spm.config.Trading.Symbol, actualMargin)
					logger.Debug("💰 [资金释放] 買單成交，释放资金: %.2f USDT (訂單價值: %.2f USDT)", actualMargin, orderValue)
				}

				logger.Info("✅ [買單成交] 價格: %s, 持倉: %.4f, 槽位状態: %s -> %s, 订單状態: %s -> %s, SlotStatus: FREE",
					formatPrice(price, spm.priceDecimals), slot.PositionQty,
					PositionStatusEmpty, PositionStatusFilled,
					"FILLED", OrderStatusNotPlaced)
				logger.Debug("🔍 [買單成交后] 等待下次AdjustOrders調用時挂出賣單...")

				spm.recordFill()

				// 通知套利管理器：買入成交（正數表示買入）
				if spm.arbitrageManager != nil && update.ExecutedQty > 0 {
					spm.arbitrageManager.OnGridPositionChange(update.ExecutedQty, update.Price)
				}
			} else {
				slot.OrderStatus = OrderStatusPartiallyFilled
			}

		} else { // SELL
			if deltaQty > 0 {
				// 🔥 关键修复：在减少 PositionQty 之前，先计算买入手续费摊销
				// 这样可以正确处理全平仓的情况（止损单全平时，PositionQty 会变为0）
				var feeFromBuy float64
				positionQtyBeforeSell := slot.PositionQty // 保存卖出前的持仓数量
				if positionQtyBeforeSell > 0 {
					feeFromBuy = slot.BuyFee * (deltaQty / positionQtyBeforeSell)
				} else {
					// 如果卖出前持仓为0，说明是异常情况，使用全部买入手续费
					feeFromBuy = slot.BuyFee
				}

				slot.PositionQty -= deltaQty
				if slot.PositionQty < 0 {
					slot.PositionQty = 0
				}
				// 累加统计
				oldTotal := spm.totalSellQty.Load().(float64)
				spm.totalSellQty.Store(oldTotal + deltaQty)

				// 🔥 保存交易記錄（買賣配對完成）
				if spm.tradeStorage != nil {
					// 🔥 使用实际平均买入价格（而不是槽位基准价格）
					// 这样可以准确反映实际盈亏，特别是当实际买入价格与槽位价格不同时
					buyPrice := slot.AvgBuyPrice
					if buyPrice <= 0 {
						// 如果没有平均买入价格（异常情况），回退到槽位价格
						buyPrice = slot.Price
						logger.Warn("⚠️ [交易記錄] 槽位 %s 没有平均买入价格，使用槽位价格 %.2f", 
							formatPrice(slot.Price, spm.priceDecimals), buyPrice)
					}
					
					// 賣出價格使用成交均價，如果没有则使用订單價格
					sellPrice := update.AvgPrice
					if sellPrice <= 0 {
						sellPrice = update.Price
					}
					if sellPrice <= 0 {
						sellPrice = slot.OrderPrice
					}
					
					// 🔥 监控卖出价格偏差：实际成交价格与委托价格的差异
					if slot.OrderPrice > 0 && sellPrice > 0 {
						sellPriceDeviation := (sellPrice - slot.OrderPrice) / slot.OrderPrice * 100
						// 如果卖出价格低于委托价格超过0.1%（不利偏差），记录警告
						if sellPriceDeviation < -0.1 {
							logger.Warn("⚠️ [價格偏差警告] 賣單實際成交價低於委託價: 委託價=%.2f, 實際價=%.2f, 偏差=%.4f%%, 數量=%.4f, OrderID=%d",
								slot.OrderPrice, sellPrice, sellPriceDeviation, deltaQty, update.OrderID)
						} else if sellPriceDeviation > 0.1 {
							// 卖出价格高于委托价格（有利偏差），记录信息
							logger.Info("💰 [價格偏差] 賣單實際成交價高於委託價（有利）: 委託價=%.2f, 實際價=%.2f, 偏差=%.4f%%, 數量=%.4f",
								slot.OrderPrice, sellPrice, sellPriceDeviation, deltaQty)
						}
					}

					// 🔥 驗证價格和數量的合理性
					if buyPrice <= 0 || sellPrice <= 0 || deltaQty <= 0 {
						logger.Warn("⚠️ [交易記錄异常] 買入價: %.2f, 賣出價: %.2f, 數量: %.4f, 跳過保存",
							buyPrice, sellPrice, deltaQty)
					} else {
						// 计算盈亏：(賣出價格 - 實際買入價格) * 數量（毛利，未扣手續費）
						// 注意：對於USDT本位合約（如BTCUSDT），價格是USDT，數量是BTC，盈亏單位是USDT
						pnl := (sellPrice - buyPrice) * deltaQty
						
						// 🔥 检查价格偏差对策略的影响：如果实际盈亏与理论盈亏差异过大，警告
						theoreticalPnL := (slot.OrderPrice - slot.Price) * deltaQty // 理论盈亏（基于槽位价格）
						if theoreticalPnL > 0 && pnl < 0 {
							// 理论应该盈利，但实际亏损了（价格偏差导致策略失效）
							logger.Error("🚨 [策略失效警告] 理論應盈利但實際虧損: 槽位價=%.2f, 委託賣價=%.2f, 實際買價=%.2f, 實際賣價=%.2f, 理論盈虧=%.4f, 實際盈虧=%.4f, 數量=%.4f",
								slot.Price, slot.OrderPrice, buyPrice, sellPrice, theoreticalPnL, pnl, deltaQty)
						}

						// 🔥 手續費：買入攤銷 + 賣出本次手續費（feeFromBuy 已在上面计算）
						totalFee := feeFromBuy + update.Commission
						feeAsset := update.CommissionAsset
						if feeAsset == "" {
							feeAsset = slot.FeeAsset
						}
						// 🔥 如果 WebSocket 未提供賣出手續費，異步查詢補充
						if update.Commission == 0 && update.OrderID > 0 {
							go spm.supplementCommission(context.Background(), update.OrderID, update.Symbol, "SELL", slot)
						}

						// 🔥 添加合理性检查：如果盈亏异常大，記錄警告
						// 對於BTCUSDT，如果價格差是100 USDT，數量是0.01 BTC，盈亏应該是1 USDT
						// 如果盈亏超過订單金額的50%，可能是计算錯误
						orderAmount := buyPrice * deltaQty
						if orderAmount > 0 && math.Abs(pnl) > orderAmount*0.5 {
							logger.Warn("⚠️ [盈亏异常] 買入價: %.2f, 賣出價: %.2f, 數量: %.4f, 盈亏: %.2f, 订單金額: %.2f, 盈亏率: %.2f%%",
								buyPrice, sellPrice, deltaQty, pnl, orderAmount, (pnl/orderAmount)*100)
						}

						// 🔥 计算价格偏差（实际成交价格 - 委托价格）
						// 买入价格偏差：实际平均买入价格 - 槽位基准价格（槽位价格是买入委托价格）
						buyPriceDeviation := (buyPrice - slot.Price) * deltaQty // USDT单位
						
						// 卖出价格偏差：实际卖出价格 - 委托卖出价格
						sellPriceDeviation := (sellPrice - slot.OrderPrice) * deltaQty // USDT单位
						
						// 保存交易記錄（買入订單ID設為0，因為無法追溯历史订單）
						buyOrderID := int64(0)
						sellOrderID := update.OrderID
						// 🔥 添加详细日志，特别是对于亏损交易
						if pnl < 0 {
							logger.Warn("🛑 [亏损交易] 槽位價格: %s, 實際買入價: %s, 賣出價: %s, 數量: %.4f, 盈亏: %.4f, 手續費: %.4f %s, OrderID: %d, 買入偏差: %.4f, 賣出偏差: %.4f",
								formatPrice(slot.Price, spm.priceDecimals), formatPrice(buyPrice, spm.priceDecimals), formatPrice(sellPrice, spm.priceDecimals), deltaQty, pnl, totalFee, feeAsset, sellOrderID, buyPriceDeviation, sellPriceDeviation)
						}
						
						// 🔥 使用SaveTradeWithDeviation保存价格偏差
						if tradeStWithDev, ok := spm.tradeStorage.(interface {
							SaveTradeWithDeviation(buyOrderID, sellOrderID int64, exchange, symbol string, buyPrice, sellPrice, quantity, pnl, fee float64, feeAsset string, buyPriceDeviation, sellPriceDeviation float64, createdAt time.Time) error
						}); ok {
							if err := tradeStWithDev.SaveTradeWithDeviation(buyOrderID, sellOrderID, spm.exchangeName, update.Symbol, buyPrice, sellPrice, deltaQty, pnl, totalFee, feeAsset, buyPriceDeviation, sellPriceDeviation, time.Now()); err != nil {
								logger.Warn("⚠️ 保存交易記錄失败: %v (買入價: %.2f, 賣出價: %.2f, 數量: %.4f, 盈亏: %.4f)", err, buyPrice, sellPrice, deltaQty, pnl)
							} else {
								logger.Debug("💰 [交易記錄已保存] 買入價: %s, 賣出價: %s, 數量: %.4f, 盈亏: %.4f, 手續費: %.4f %s, 買入偏差: %.4f, 賣出偏差: %.4f",
									formatPrice(buyPrice, spm.priceDecimals), formatPrice(sellPrice, spm.priceDecimals), deltaQty, pnl, totalFee, feeAsset, buyPriceDeviation, sellPriceDeviation)
							}
						} else {
							// 降级：使用旧接口
							if err := spm.tradeStorage.SaveTrade(buyOrderID, sellOrderID, spm.exchangeName, update.Symbol, buyPrice, sellPrice, deltaQty, pnl, totalFee, feeAsset, time.Now()); err != nil {
								logger.Warn("⚠️ 保存交易記錄失败: %v (買入價: %.2f, 賣出價: %.2f, 數量: %.4f, 盈亏: %.4f)", err, buyPrice, sellPrice, deltaQty, pnl)
							} else {
								logger.Debug("💰 [交易記錄已保存] 買入價: %s, 賣出價: %s, 數量: %.4f, 盈亏: %.4f, 手續費: %.4f %s",
									formatPrice(buyPrice, spm.priceDecimals), formatPrice(sellPrice, spm.priceDecimals), deltaQty, pnl, totalFee, feeAsset)
							}
						}
						slot.BuyFee -= feeFromBuy
					}
				}
			}

			if update.Status == "FILLED" {
				slot.OrderStatus = OrderStatusNotPlaced // 重置订單状態
				slot.OrderID = 0
				slot.ClientOID = ""
				slot.OrderSide = "" // 🔥 清除订單方向，避免误判
				slot.OrderFilledQty = 0

				if slot.PositionQty < 0.000001 {
					slot.PositionStatus = PositionStatusEmpty // 標記為空倉
				}
				// 🔥 释放槽位鎖：賣單成交，允許后续挂買單
				slot.SlotStatus = SlotStatusFree
				// 🔥 賣單成交，重置PostOnly失败计數
				slot.PostOnlyFailCount = 0

				// 🔥 释放资金：賣單成交后，资金已收回，释放預留的资金（賣單不需要預留资金，但為了统一处理也释放）
				// 注意：賣單是平倉，不占用资金，但為了保持一致性，这里也处理
				// 賣單成交后，持倉减少，對应的買入资金应該被释放
				// 使用槽位價格（買入價）计算释放金額
				releaseValue := price * deltaQty
				actualMargin := spm.getActualMargin(releaseValue)
				if actualMargin > 0 {
					spm.allocationManager.Release(spm.exchangeName, spm.config.Trading.Symbol, actualMargin)
					logger.Debug("💰 [资金释放] 賣單成交，释放资金: %.2f USDT (持倉價值: %.2f USDT, 持倉减少: %.4f)", actualMargin, releaseValue, deltaQty)
				}

				logger.Info("✅ [賣單成交] 價格: %s, 剩餘持倉: %.4f, 槽位状態: %s, 订單状態: %s, SlotStatus: FREE",
					formatPrice(price, spm.priceDecimals), slot.PositionQty, slot.PositionStatus, slot.OrderStatus)

				spm.recordFill()

				// 通知套利管理器：賣出成交（負數表示賣出）
				if spm.arbitrageManager != nil && update.ExecutedQty > 0 {
					spm.arbitrageManager.OnGridPositionChange(-update.ExecutedQty, update.Price)
				}
			} else {
				slot.OrderStatus = OrderStatusPartiallyFilled
			}
		}

	case "CANCELED", "EXPIRED", "REJECTED":
		logger.Info("⚠️ [订單%s] 價格: %s, 方向: %s, 原因: %s, 已成交: %.4f",
			update.Status, formatPrice(price, spm.priceDecimals), side, update.Status, slot.OrderFilledQty)

		// 🔥 释放资金：订單取消后，释放未成交部分的預留资金
		// 注意：買單取消時，如果未成交，需要释放整個订單的預留资金
		// 由於我们不知道原始订單數量，使用订單價格和配置的订單金額来估算
		if side == "BUY" && slot.OrderPrice > 0 {
			// 對於買單，如果未成交或部分成交，释放未成交部分的资金
			// 使用配置的订單金額作為参考（因為每個槽位的订單金額是固定的）
			orderValue := spm.config.Trading.OrderQuantity
			if slot.OrderFilledQty > 0 {
				// 部分成交：释放未成交部分的资金
				filledValue := slot.OrderPrice * slot.OrderFilledQty
				unfilledValue := orderValue - filledValue
				actualMargin := spm.getActualMargin(unfilledValue)
				if actualMargin > 0 {
					spm.allocationManager.Release(spm.exchangeName, spm.config.Trading.Symbol, actualMargin)
					logger.Debug("💰 [资金释放] 買單部分成交后取消，释放未成交资金: %.2f USDT (訂單價值: %.2f USDT, 已成交: %.4f)",
						actualMargin, unfilledValue, slot.OrderFilledQty)
				}
			} else {
				// 完全未成交：释放整個订單的預留资金
				actualMargin := spm.getActualMargin(orderValue)
				if actualMargin > 0 {
					spm.allocationManager.Release(spm.exchangeName, spm.config.Trading.Symbol, actualMargin)
					logger.Debug("💰 [资金释放] 買單未成交取消，释放资金: %.2f USDT (訂單價值: %.2f USDT)", actualMargin, orderValue)
				}
			}
		}
		// 賣單取消不需要释放资金，因為賣單是平倉，不占用资金

		// 🔥 核心修複：根據订單方向和成交情况处理槽位状態
		if side == "BUY" {
			// 買單被取消/拒绝
			if slot.PositionQty > 0 || slot.OrderFilledQty > 0 {
				// 部分成交后被取消：保留持倉，允許后续挂賣單
				logger.Info("💡 [買單部分成交后取消] 價格: %s, 持倉: %.4f, 轉為有倉状態",
					formatPrice(price, spm.priceDecimals), slot.PositionQty)
				slot.PositionStatus = PositionStatusFilled
				slot.SlotStatus = SlotStatusFree // 允許挂賣單
			} else {
				// 完全未成交被取消：重置為空槽位
				logger.Info("🔄 [買單未成交取消] 價格: %s, 重置槽位為空闲",
					formatPrice(price, spm.priceDecimals))
				slot.PositionStatus = PositionStatusEmpty
				slot.SlotStatus = SlotStatusFree // 允許重新挂買單
			}
		} else if side == "SELL" {
			// 賣單被取消/拒绝：应該还持有币，保持持倉状態
			if slot.PositionQty > 0 {
				// 增加PostOnly失败计數（订單被交易所撤销通常是PostOnly失败）
				slot.PostOnlyFailCount++
				logger.Info("🔄 [賣單取消] 價格: %s, 保持持倉状態: %.4f, 等待重挂, PostOnly失败计數: %d",
					formatPrice(price, spm.priceDecimals), slot.PositionQty, slot.PostOnlyFailCount)
				slot.PositionStatus = PositionStatusFilled
				slot.SlotStatus = SlotStatusFree // 允許重新挂賣單
			} else {
				// 异常情况：賣單取消但没有持倉，重置為空
				logger.Warn("⚠️ [异常] 賣單取消但無持倉，價格: %s, 重置為空",
					formatPrice(price, spm.priceDecimals))
				slot.PositionStatus = PositionStatusEmpty
				slot.SlotStatus = SlotStatusFree
			}
		}

		// 清空订單信息
		slot.OrderStatus = OrderStatusCanceled
		slot.OrderID = 0
		slot.ClientOID = ""
		slot.OrderFilledQty = 0
		// 保留 OrderSide 用於日志調試
	}
}

// getOrCreateSlot 獲取或創建槽位
func (spm *SuperPositionManager) getOrCreateSlot(price float64) *InventorySlot {
	if slot, exists := spm.slots.Load(price); exists {
		return slot.(*InventorySlot)
	}

	// 創建新槽位
	slot := &InventorySlot{
		Price:          price,
		PositionStatus: PositionStatusEmpty,
		PositionQty:    0,
		OrderStatus:    OrderStatusNotPlaced,
		SlotStatus:     SlotStatusFree, // 🔥 初始化為FREE状態
	}
	spm.slots.Store(price, slot)
	return slot
}

// findNearestGridPrice 找到最近的网格價格
// 根據當前價格动態计算最近的网格對齐價格
func (spm *SuperPositionManager) findNearestGridPrice(currentPrice float64) float64 {
	gridMode := spm.config.Trading.GridMode
	if gridMode == "" {
		gridMode = "arithmetic"
	}
	if gridMode == "geometric" {
		ratio := spm.config.Trading.PriceInterval
		if ratio <= 0 || ratio >= 1 {
			ratio = 0.01 // 默認 1%
		}
		// 等比：gridPrice = anchor * (1+ratio)^k，k = round(log(current/anchor) / log(1+ratio))
		if spm.anchorPrice <= 0 {
			return roundPrice(currentPrice, spm.priceDecimals)
		}
		logRatio := math.Log(1 + ratio)
		k := math.Round(math.Log(currentPrice/spm.anchorPrice) / logRatio)
		gridPrice := spm.anchorPrice * math.Pow(1+ratio, k)
		return roundPrice(gridPrice, spm.priceDecimals)
	}
	// 等差
	offset := currentPrice - spm.anchorPrice
	intervals := math.Round(offset / spm.config.Trading.PriceInterval)
	gridPrice := spm.anchorPrice + intervals*spm.config.Trading.PriceInterval
	return roundPrice(gridPrice, spm.priceDecimals)
}

// calculateSlotPrices 计算槽位價格列表（统一的网格计算方法）
// 這個方法确保初始化和實時調整计算出完全相同的槽位價格
// 参數：
//   - gridPrice: 网格價格（使用锚点價格）
//   - count: 需要计算的槽位數量
//   - direction: 方向，"down"表示向下（買單），"up"表示向上（賣單）
//
// 回傳：槽位價格列表，從网格價格开始，按價格間隔遞减或遞增，使用检测到的價格精度
func (spm *SuperPositionManager) calculateSlotPrices(gridPrice float64, count int, direction string) []float64 {
	var prices []float64
	priceInterval := spm.config.Trading.PriceInterval
	gridMode := spm.config.Trading.GridMode
	if gridMode == "" {
		gridMode = "arithmetic"
	}

	for i := 0; i < count; i++ {
		var price float64
		if gridMode == "geometric" {
			ratio := priceInterval
			if ratio <= 0 || ratio >= 1 {
				ratio = 0.01
			}
			if direction == "down" {
				price = gridPrice * math.Pow(1+ratio, -float64(i))
			} else {
				price = gridPrice * math.Pow(1+ratio, float64(i))
			}
		} else {
			if direction == "down" {
				price = gridPrice - float64(i)*priceInterval
			} else {
				price = gridPrice + float64(i)*priceInterval
			}
		}
		price = roundPrice(price, spm.priceDecimals)

		if price <= 0 {
			logger.Warn("⚠️ [%s] 跳過無效槽位價格 %.8f（方向=%s, 索引=%d, 网格價格=%.2f, 间隔=%.4f）",
				spm.logPrefix(), price, direction, i, gridPrice, priceInterval)
			continue
		}

		prices = append(prices, price)
	}

	return prices
}

// ===== IPositionManager 接口實現（供 safety.Reconciler 使用）=====
// 注意：以下方法是 safety/reconciler.go 中 IPositionManager 接口的實現，
// 被 Reconciler 對账器調用，不可刪除或修改签名

// SlotData 槽位數據結構（用於傳遞给外部）
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
// 注意：為了避免類型冲突，这里使用 interface{} 返回槽位數據
// 調用者需要將其轉换為具体的槽位信息
func (spm *SuperPositionManager) IterateSlots(fn func(price float64, slot interface{}) bool) {
	spm.slots.Range(func(key, value interface{}) bool {
		price := key.(float64)
		slot := value.(*InventorySlot)
		slot.mu.RLock()
		defer slot.mu.RUnlock()

		// 構造槽位數據
		data := SlotData{
			Price:          price,
			PositionStatus: slot.PositionStatus,
			PositionQty:    slot.PositionQty,
			OrderID:        slot.OrderID,
			OrderSide:      slot.OrderSide,
			OrderStatus:    slot.OrderStatus,
			OrderCreatedAt: slot.OrderCreatedAt,
		}

		// 返回槽位數據
		return fn(price, data)
	})
}

// DetailedSlotData 详细槽位數據結構（包含所有字段）
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
	StrategyName   string // 策略名称
	StrategyType   string // 策略類型
}

// GetAllSlotsDetailed 獲取所有槽位的详细信息
// 注意：如果槽位數量很大，建议使用分页查詢或限制數量
func (spm *SuperPositionManager) GetAllSlotsDetailed() []DetailedSlotData {
	// 限制最大返回數量，防止記憶體占用過大
	maxSlots := 10000 // 最多返回1万個槽位
	var slots []DetailedSlotData
	count := 0

	spm.slots.Range(func(key, value interface{}) bool {
		if count >= maxSlots {
			logger.Warn("⚠️ [槽位查詢] 槽位數量超過限制 (%d)，只返回前 %d 個", maxSlots, maxSlots)
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
			StrategyName:   slot.StrategyName,
			StrategyType:   slot.StrategyType,
		})

		slot.mu.RUnlock()
		count++
		return true
	})
	return slots
}

// GetSlotCount 獲取槽位總數
func (spm *SuperPositionManager) GetSlotCount() int {
	count := 0
	spm.slots.Range(func(key, value interface{}) bool {
		count++
		return true
	})
	return count
}

// GetTotalBuyQty 獲取累计買入數量（IPositionManager 接口方法，供 Reconciler 使用）
func (spm *SuperPositionManager) GetTotalBuyQty() float64 {
	return spm.totalBuyQty.Load().(float64)
}

// GetTotalSellQty 獲取累计賣出數量（IPositionManager 接口方法，供 Reconciler 使用）
func (spm *SuperPositionManager) GetTotalSellQty() float64 {
	return spm.totalSellQty.Load().(float64)
}

// GetReconcileCount 獲取對账次數（IPositionManager 接口方法，供 Reconciler 使用）
func (spm *SuperPositionManager) GetReconcileCount() int64 {
	return spm.reconcileCount.Load()
}

// IncrementReconcileCount 增加對账次數（IPositionManager 接口方法，供 Reconciler 使用）
func (spm *SuperPositionManager) IncrementReconcileCount() {
	spm.reconcileCount.Add(1)
}

// UpdateLastReconcileTime 更新最后對账時间（IPositionManager 接口方法，供 Reconciler 使用）
func (spm *SuperPositionManager) UpdateLastReconcileTime(t time.Time) {
	spm.lastReconcileTime.Store(t)
}

// GetLastReconcileTime 獲取最后對账時间
func (spm *SuperPositionManager) GetLastReconcileTime() time.Time {
	v := spm.lastReconcileTime.Load()
	if v == nil {
		return time.Time{}
	}
	return v.(time.Time)
}

// GetSymbol 獲取交易符号
func (spm *SuperPositionManager) GetSymbol() string {
	return spm.config.Trading.Symbol
}

// GetExchange 獲取交易所名称
func (spm *SuperPositionManager) GetExchange() string {
	return spm.exchangeName
}

// GetAllocationManager 獲取资金分配管理器（供倉位计划等模塊按交易對設置限額）
func (spm *SuperPositionManager) GetAllocationManager() *AllocationManager {
	return spm.allocationManager
}

// GetTotalPositionValueUSDT 獲取當前持倉總價值（USDT），用於倉位计划進度检查
func (spm *SuperPositionManager) GetTotalPositionValueUSDT() float64 {
	var total float64
	spm.slots.Range(func(key, value interface{}) bool {
		slot := value.(*InventorySlot)
		slot.mu.RLock()
		if slot.PositionStatus == PositionStatusFilled && slot.PositionQty > 0 && slot.Price > 0 {
			total += slot.Price * slot.PositionQty
		}
		slot.mu.RUnlock()
		return true
	})
	return total
}

// GetPriceInterval 獲取價格间隔
func (spm *SuperPositionManager) GetPriceInterval() float64 {
	return spm.config.Trading.PriceInterval
}

// GetProfitSpread 獲取利潤間距（平倉價差）
func (spm *SuperPositionManager) GetProfitSpread() float64 {
	return spm.getEffectiveProfitSpread()
}

// getEffectiveProfitSpread 獲取有效利潤間距：ProfitSpread > 0 則使用，否則回退到 PriceInterval
func (spm *SuperPositionManager) getEffectiveProfitSpread() float64 {
	if spm.config.Trading.ProfitSpread > 0 {
		return spm.config.Trading.ProfitSpread
	}
	return spm.config.Trading.PriceInterval
}

// GetAnchorPrice 獲取價格锚点
func (spm *SuperPositionManager) GetAnchorPrice() float64 {
	return spm.anchorPrice
}

// UpdateTradingParams 运行時更新交易参數（热更新）
// 更新後会自动使用新参數计算网格和下單
func (spm *SuperPositionManager) UpdateTradingParams(priceInterval, profitSpread, orderQuantity float64, buyWindowSize, sellWindowSize int) (changed bool) {
	spm.mu.Lock()
	defer spm.mu.Unlock()

	var changes []string

	if priceInterval > 0 && priceInterval != spm.config.Trading.PriceInterval {
		old := spm.config.Trading.PriceInterval
		spm.config.Trading.PriceInterval = priceInterval
		changes = append(changes, fmt.Sprintf("price_interval: %.4f -> %.4f", old, priceInterval))
	}
	if profitSpread >= 0 && profitSpread != spm.config.Trading.ProfitSpread {
		old := spm.config.Trading.ProfitSpread
		spm.config.Trading.ProfitSpread = profitSpread
		changes = append(changes, fmt.Sprintf("profit_spread: %.4f -> %.4f", old, profitSpread))
	}
	if orderQuantity > 0 && orderQuantity != spm.config.Trading.OrderQuantity {
		old := spm.config.Trading.OrderQuantity
		spm.config.Trading.OrderQuantity = orderQuantity
		changes = append(changes, fmt.Sprintf("order_quantity: %.2f -> %.2f", old, orderQuantity))
	}
	if buyWindowSize > 0 && buyWindowSize != spm.config.Trading.BuyWindowSize {
		old := spm.config.Trading.BuyWindowSize
		spm.config.Trading.BuyWindowSize = buyWindowSize
		changes = append(changes, fmt.Sprintf("buy_window_size: %d -> %d", old, buyWindowSize))
	}
	if sellWindowSize > 0 && sellWindowSize != spm.config.Trading.SellWindowSize {
		old := spm.config.Trading.SellWindowSize
		spm.config.Trading.SellWindowSize = sellWindowSize
		changes = append(changes, fmt.Sprintf("sell_window_size: %d -> %d", old, sellWindowSize))
	}

	if len(changes) > 0 {
		logger.Info("🔄 [%s] 交易参數已热更新: %s",
			spm.logPrefix(), strings.Join(changes, ", "))
		return true
	}
	return false
}

// GetTradingParamsSummary 獲取當前交易参數摘要（用於前端显示）
func (spm *SuperPositionManager) GetTradingParamsSummary() map[string]interface{} {
	lastPrice := 0.0
	if v := spm.lastMarketPrice.Load(); v != nil {
		lastPrice = v.(float64)
	}

	priceInterval := spm.config.Trading.PriceInterval
	profitSpread := spm.getEffectiveProfitSpread()
	buyWindowSize := spm.config.Trading.BuyWindowSize
	sellWindowSize := spm.config.Trading.SellWindowSize

	result := map[string]interface{}{
		"price_interval":   priceInterval,
		"profit_spread":     profitSpread,
		"order_quantity":   spm.config.Trading.OrderQuantity,
		"buy_window_size":  buyWindowSize,
		"sell_window_size": sellWindowSize,
		"anchor_price":     spm.anchorPrice,
		"current_price":    lastPrice,
		"direction":        spm.config.Trading.Direction,
	}

	// 根据当前价格计算价格上下限
	if lastPrice > 0 && priceInterval > 0 {
		gridPrice := spm.findNearestGridPrice(lastPrice)
		// 买单价格范围（向下）
		buyLowPrice := gridPrice - float64(buyWindowSize-1)*priceInterval
		if buyLowPrice < 0 {
			buyLowPrice = priceInterval
		}
		buyHighPrice := gridPrice
		// 卖单价格范围（向上，使用 profitSpread）
		sellLowPrice := gridPrice + profitSpread
		sellHighPrice := gridPrice + float64(sellWindowSize)*profitSpread

		result["grid_price"] = gridPrice
		result["buy_price_low"] = roundPrice(buyLowPrice, spm.priceDecimals)
		result["buy_price_high"] = roundPrice(buyHighPrice, spm.priceDecimals)
		result["sell_price_low"] = roundPrice(sellLowPrice, spm.priceDecimals)
		result["sell_price_high"] = roundPrice(sellHighPrice, spm.priceDecimals)
	}

	return result
}

// GetLeverage 獲取杠杆倍數（用於计算實際资金占用）
func (spm *SuperPositionManager) GetLeverage() int {
	if spm.isSpot() {
		return 1
	}
	leverage := 1 // 默认1倍（無杠杆）
	ctx := context.Background()
	// 先尝試從帳戶資訊中的持倉獲取杠杆倍數
	if accountResult, err := spm.exchange.GetAccount(ctx); err == nil && accountResult != nil {
		accountValue := reflect.ValueOf(accountResult)
		if accountValue.Kind() == reflect.Ptr {
			accountValue = accountValue.Elem()
		}
		// 尝試從 Account.Positions 字段獲取持倉信息
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
							// 尝試獲取 Leverage 字段
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
		// 如果從持倉中獲取不到，尝試從账戶级别的杠杆字段獲取
		if leverage == 1 {
			if leverageField := accountValue.FieldByName("AccountLeverage"); leverageField.IsValid() && leverageField.CanInterface() {
				if lev, ok := leverageField.Interface().(int); ok && lev > 0 {
					leverage = lev
				}
			}
		}
	}

	// 如果從账戶中獲取不到，尝試從 GetPositions 獲取
	if leverage == 1 {
		if positionsInterface, err := spm.exchange.GetPositions(ctx, spm.config.Trading.Symbol); err == nil && positionsInterface != nil {
			// 使用反射处理不同類型的持倉資訊
			positionsValue := reflect.ValueOf(positionsInterface)
			if positionsValue.Kind() == reflect.Slice {
				for i := 0; i < positionsValue.Len(); i++ {
					posValue := positionsValue.Index(i)
					if posValue.Kind() == reflect.Ptr {
						posValue = posValue.Elem()
					} else if posValue.Kind() == reflect.Interface {
						posValue = posValue.Elem()
					}
					// 尝試獲取 Leverage 字段
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

// RestoreReconciliationStats 從數據库恢複對账统计值
// storage 是對账存儲接口，exchange/symbol 用於精确定位历史記錄
func (spm *SuperPositionManager) RestoreReconciliationStats(storage ReconciliationStorage, exchange, symbol string) error {
	if storage == nil {
		return nil // 存儲服務不可用，不报錯
	}

	// 1. 獲取最新對账記錄
	latestHistoryInterface, err := storage.GetLatestReconciliationHistory(exchange, symbol)
	if err != nil {
		return fmt.Errorf("獲取最新對账記錄失败: %w", err)
	}

	// 2. 獲取對账次數
	reconcileCount, err := storage.GetReconciliationCount(exchange, symbol)
	if err != nil {
		return fmt.Errorf("獲取對账次數失败: %w", err)
	}

	// 3. 如果没有历史記錄，不恢複（保持默认值）
	if latestHistoryInterface == nil {
		logger.Info("📊 [對账恢複] 未找到历史對账記錄，使用默认值")
		return nil
	}

	// 4. 使用反射提取對账記錄字段
	v := reflect.ValueOf(latestHistoryInterface)
	if v.Kind() == reflect.Ptr {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return fmt.Errorf("對账記錄類型錯误: %T", latestHistoryInterface)
	}

	// 提取字段的辅助函數
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

	// 5. 恢複统计值
	totalBuyQty := getFloat64Field("TotalBuyQty")
	totalSellQty := getFloat64Field("TotalSellQty")
	lastReconcileTime := getTimeField("ReconcileTime")

	spm.totalBuyQty.Store(totalBuyQty)
	spm.totalSellQty.Store(totalSellQty)
	spm.reconcileCount.Store(reconcileCount)
	spm.lastReconcileTime.Store(lastReconcileTime)

	logger.Info("✅ [對账恢複] 已恢複對账统计: 次數=%d, 累计買入=%.4f, 累计賣出=%.4f, 最后對账時间=%s",
		reconcileCount, totalBuyQty, totalSellQty, lastReconcileTime.Format("2006-01-02 15:04:05"))

	return nil
}

// ===== 訂單清理功能已迁移到 safety.OrderCleaner =====
// StartOrderCleanup 和 cleanupOrders 方法已移至 safety/order_cleaner.go

// UpdateSlotOrderStatus 更新槽位订單状態（供 OrderCleaner 使用）
func (spm *SuperPositionManager) UpdateSlotOrderStatus(price float64, status string) {
	slot := spm.getOrCreateSlot(price)
	slot.mu.Lock()
	slot.OrderStatus = status
	slot.mu.Unlock()
}

// CancelAllBuyOrders 撤销所有買單（风控触发時使用）
func (spm *SuperPositionManager) CancelAllBuyOrders() {
	var buyOrderIDs []int64
	var buyPrices []float64

	// 🔥 修複：收集所有OrderID>0且OrderSide=BUY的订單，不管OrderStatus
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

	logger.Info("🔄 [撤销買單] 准备撤销 %d 個買單以释放保证金", len(buyOrderIDs))

	// 🔥 重複尝試3次，确保撤單干净
	for attempt := 1; attempt <= 3; attempt++ {
		if len(buyOrderIDs) == 0 {
			break
		}

		logger.Info("🔄 [撤销買單] 第 %d 次尝試，剩餘 %d 個订單", attempt, len(buyOrderIDs))

		if err := spm.executor.BatchCancelOrders(buyOrderIDs); err != nil {
			logger.Error("❌ [撤销買單] 批量撤單失败: %v", err)
		}

		// 更新槽位状態
		for _, price := range buyPrices {
			slot := spm.getOrCreateSlot(price)
			slot.mu.Lock()
			slot.OrderStatus = OrderStatusCancelRequested
			slot.mu.Unlock()
		}

		// 等待2秒让撤單生效（WebSocket推送通知）
		time.Sleep(2 * time.Second)

		// 🔥 二次检查：重新扫描本地槽位状態
		if attempt < 3 {
			buyOrderIDs = nil
			buyPrices = nil

			spm.slots.Range(func(key, value interface{}) bool {
				price := key.(float64)
				slot := value.(*InventorySlot)

				slot.mu.RLock()
				// 如果OrderStatus不是CANCELED且OrderID>0，說明可能还有残留
				if slot.OrderSide == "BUY" && slot.OrderID > 0 &&
					slot.OrderStatus != OrderStatusCanceled {
					buyOrderIDs = append(buyOrderIDs, slot.OrderID)
					buyPrices = append(buyPrices, price)
				}
				slot.mu.RUnlock()
				return true
			})

			if len(buyOrderIDs) > 0 {
				logger.Warn("⚠️ [撤销買單] 检测到 %d 個残留買單，继续清理", len(buyOrderIDs))
			} else {
				logger.Info("✅ [撤销買單] 所有買單已清理完成")
				break
			}
		}
	}

	logger.Info("✅ [撤销買單] 清理完成")
}

// CancelExcessOpenOrders 達到最大持倉預警時，撤銷多餘的開倉單，使開倉單數不超過 maxAllowed。
// LONG：開倉單為買單，先撤委託價高的；SHORT：開倉單為賣單，先撤委託價低的。
func (spm *SuperPositionManager) CancelExcessOpenOrders(maxAllowed int) {
	if maxAllowed <= 0 {
		return
	}
	openSide := "BUY"
	if spm.isShort() {
		openSide = "SELL"
	}
	type slotOrder struct {
		price  float64
		orderID int64
	}
	var openOrders []slotOrder
	spm.slots.Range(func(key, value interface{}) bool {
		price := key.(float64)
		slot := value.(*InventorySlot)
		slot.mu.RLock()
		if slot.OrderSide == openSide && slot.OrderID > 0 &&
			slot.OrderStatus != OrderStatusCanceled && slot.OrderStatus != OrderStatusCancelRequested {
			openOrders = append(openOrders, slotOrder{price: price, orderID: slot.OrderID})
		}
		slot.mu.RUnlock()
		return true
	})
	if len(openOrders) <= maxAllowed {
		return
	}
	// LONG：價格降序，先撤高價買單；SHORT：價格升序，先撤低價賣單
	sort.Slice(openOrders, func(i, j int) bool {
		if spm.isShort() {
			return openOrders[i].price < openOrders[j].price
		}
		return openOrders[i].price > openOrders[j].price
	})
	toCancel := len(openOrders) - maxAllowed
	var orderIDs []int64
	for i := 0; i < toCancel && i < len(openOrders); i++ {
		orderIDs = append(orderIDs, openOrders[i].orderID)
	}
	if len(orderIDs) == 0 {
		return
	}
	sideLabel := "買單"
	if spm.isShort() {
		sideLabel = "賣單"
	}
	logger.Info("🔄 [最大持倉預警] 當前開倉單 %d 筆超過上限 %d，撤銷 %d 筆 %s（%s 先撤）",
		len(openOrders), maxAllowed, len(orderIDs), sideLabel, map[bool]string{false: "高價先撤", true: "低價先撤"}[spm.isShort()])
	if err := spm.executor.BatchCancelOrders(orderIDs); err != nil {
		logger.Error("❌ [最大持倉預警] 批量撤單失敗: %v", err)
		return
	}
	orderIDToPrice := make(map[int64]float64, len(openOrders))
	for _, s := range openOrders {
		orderIDToPrice[s.orderID] = s.price
	}
	for _, oid := range orderIDs {
		if price, ok := orderIDToPrice[oid]; ok {
			slot := spm.getOrCreateSlot(price)
			slot.mu.Lock()
			slot.OrderStatus = OrderStatusCancelRequested
			slot.mu.Unlock()
		}
	}
	logger.Info("✅ [最大持倉預警] 已提交撤銷 %d 筆 %s", len(orderIDs), sideLabel)
}

// LiquidateAll 全平倉位（风控或止损触发時使用）
func (spm *SuperPositionManager) LiquidateAll() {
	logger.Warn("🚨 [全平倉] 正在執行全平操作，撤销所有買單並市價平倉所有持倉...")

	// 1. 撤销所有買單
	spm.CancelAllBuyOrders()

	// 2. 收集所有持倉槽位並提交賣單
	var sellOrders []*OrderRequest
	spm.slots.Range(func(key, value interface{}) bool {
		price := key.(float64)
		slot := value.(*InventorySlot)

		slot.mu.Lock()
		if slot.PositionStatus == PositionStatusFilled && slot.PositionQty > 0 {
			// 如果已有订單，先尝試撤销
			if slot.OrderID > 0 {
				logger.Info("🔄 [全平倉] 撤销槽位 %s 的現有订單 %d", formatPrice(price, spm.priceDecimals), slot.OrderID)
				spm.executor.BatchCancelOrders([]int64{slot.OrderID})
			}

			// 標記為 PENDING
			slot.SlotStatus = SlotStatusPending

			// 構建賣單（使用當前市價或略低於市價的價格以确保成交，这里简單使用當前锚点價格附近的賣出逻辑）
			// 實際上由於是全平，最好的方式是下市價單或极优價格的限價單
			// 这里複用 AdjustOrders 中的逻辑，使用槽位價格加一個间隔作為賣價，或者根據當前價格調整

			// 獲取最后價格
			lastPrice, _ := spm.lastMarketPrice.Load().(float64)
			if lastPrice <= 0 {
				lastPrice = price
			}

			sellPrice := lastPrice * 0.99 // 使用略低於市價的價格确保成交（限價平倉）
			sellPrice = roundPrice(sellPrice, spm.priceDecimals)

			clientOID := spm.generateClientOrderID(price, "SELL")

			sellOrders = append(sellOrders, &OrderRequest{
				Symbol:        spm.config.Trading.Symbol,
				Side:          "SELL",
				Price:         sellPrice,
				Quantity:      slot.PositionQty,
				PriceDecimals: spm.priceDecimals,
				ReduceOnly:    !spm.isSpot(), // 現貨不支援 ReduceOnly
				PostOnly:      false,         // 强制平倉不使用 PostOnly
				ClientOrderID: clientOID,
				OrderSource:   "stop_loss", // 止損平倉
			})
		}
		slot.mu.Unlock()
		return true
	})

	if len(sellOrders) > 0 {
		logger.Info("🔄 [全平倉] 提交 %d 個平倉賣單", len(sellOrders))
		result := spm.executor.BatchPlaceOrdersWithDetails(sellOrders)

		// 更新槽位状態
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
		logger.Info("ℹ️ [全平倉] 没有发現需要平倉的持倉")
	}
}

// ShiftGrid 整體移動網格錨點（上移或下移），並撤銷開倉委託以便下一輪按新錨點掛單
func (spm *SuperPositionManager) ShiftGrid(direction string, step float64) {
	if step <= 0 {
		step = spm.config.Trading.GridShiftStep
	}
	if step <= 0 {
		step = spm.config.Trading.PriceInterval
	}
	spm.mu.Lock()
	if direction == "up" {
		spm.anchorPrice += step
		logger.Info("📈 [網格上移] 錨點 +%.2f，新錨點=%.2f", step, spm.anchorPrice)
	} else {
		spm.anchorPrice -= step
		if spm.anchorPrice < 0 {
			spm.anchorPrice = 0
		}
		logger.Info("📉 [網格下移] 錨點 -%.2f，新錨點=%.2f", step, spm.anchorPrice)
	}
	spm.mu.Unlock()
	spm.CancelAllOpenOrders()
}

// ===== 對账功能已迁移到 safety.Reconciler =====
// StartReconciliation 和 Reconcile 方法已移至 safety/reconciler.go
// SetPauseChecker 也已移至 Reconciler

// CancelAllOrders 撤销所有订單（退出時使用）
// 委托给交易所适配器實現具体逻辑
func (spm *SuperPositionManager) CancelAllOrders() {
	ctx := context.Background()
	if err := spm.exchange.CancelAllOrders(ctx, spm.config.Trading.Symbol); err != nil {
		logger.Error("❌ [%s] 撤销所有订單失败: %v", spm.exchange.GetName(), err)
	} else {
		logger.Info("✅ [%s] 撤销所有订單完成", spm.exchange.GetName())
	}
}

// getExistingPosition 獲取當前持倉數量（容錯处理）
func (spm *SuperPositionManager) getExistingPosition() float64 {
	ctx := context.Background()
	positionsInterface, err := spm.exchange.GetPositions(ctx, spm.config.Trading.Symbol)
	if err != nil || positionsInterface == nil {
		logger.Debug("🔍 [持倉恢複] 無法獲取持倉信息: %v", err)
		return 0
	}

	// 尝試類型断言 - 假設返回的是包含 Size 字段的結構体切片
	// 持倉方向：LONG 時取正數，SHORT 時取負數的絕對值（交易所 short 持倉為負）
	rawSize := 0.0
	switch positions := positionsInterface.(type) {
	case []*PositionInfo:
		for _, pos := range positions {
			if pos != nil && pos.Symbol == spm.config.Trading.Symbol {
				rawSize = pos.Size
				break
			}
		}
	case []interface{}:
		for _, pos := range positions {
			if posInfo, ok := pos.(*PositionInfo); ok {
				if posInfo.Symbol == spm.config.Trading.Symbol {
					rawSize = posInfo.Size
					break
				}
			}
			if posMap, ok := pos.(map[string]interface{}); ok {
				if symbol, ok := posMap["Symbol"].(string); ok && symbol == spm.config.Trading.Symbol {
					if size, ok := posMap["Size"].(float64); ok {
						rawSize = size
						break
					}
				}
			}
		}
	default:
		logger.Debug("🔍 [持倉恢複] 持倉類型: %T，未找到匹配的持倉", positionsInterface)
		return 0
	}

	// 按方向過濾：LONG 取正數持倉，SHORT 取負數持倉的絕對值
	if spm.isShort() {
		if rawSize < 0 {
			logger.Debug("🔍 [持倉恢複] 找到做空持倉: %.4f", -rawSize)
			return -rawSize
		}
		return 0
	}
	if rawSize > 0 {
		logger.Debug("🔍 [持倉恢複] 找到做多持倉: %.4f", rawSize)
		return rawSize
	}
	logger.Debug("🔍 [持倉恢複] 未找到匹配的持倉")
	return 0
}

// ForceSyncPositions 强制同步持倉（當對账发現重大不一致時調用）
func (spm *SuperPositionManager) ForceSyncPositions(exchangePosition float64) {
	// 注意：这里不需要全局鎖 spm.mu.Lock()，因為 slots 是 sync.Map，槽位更新有自己的鎖
	// 且我们不希望在對账時阻塞下單逻辑

	logger.Warn("🚨 [强制同步] 正在同步持倉状態，期望持倉: %.4f", exchangePosition)

	if exchangePosition <= 0.000001 {
		// 交易所持倉為空，清空本地所有槽位的持倉
		count := 0
		spm.slots.Range(func(key, value interface{}) bool {
			slot := value.(*InventorySlot)
			slot.mu.Lock()
			if slot.PositionStatus == PositionStatusFilled {
				logger.Info("🧹 [强制同步] 清空槽位價格 %s 的持倉 (原數量: %.4f)",
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
			logger.Info("✅ [强制同步] 已成功清空 %d 個槽位的持倉數據", count)
		} else {
			logger.Debug("ℹ️ [强制同步] 本地本来就没有持倉，無需操作")
		}
	} else {
		// 交易所仍有持倉，但本地可能超出交易所實際持倉
		// 需要修剪多餘的本地槽位，防止平倉委託超出實際持倉
		spm.trimExcessPositions(exchangePosition)
	}
}

// trimExcessPositions 修剪多餘的本地持倉槽位
// 當本地持倉总量 > 交易所實際持倉時，清除距離當前價格最遠的「幻影」槽位
func (spm *SuperPositionManager) trimExcessPositions(exchangePosition float64) {
	// 1. 收集所有 FILLED 槽位
	type filledSlot struct {
		Price    float64
		Qty      float64
		Distance float64 // 距離當前價格的距離
	}
	var filledSlots []filledSlot
	localTotal := 0.0

	// 獲取當前市場價格
	currentPrice, _ := spm.lastMarketPrice.Load().(float64)
	if currentPrice <= 0 {
		currentPrice = spm.anchorPrice
	}

	spm.slots.Range(func(key, value interface{}) bool {
		slot := value.(*InventorySlot)
		slot.mu.RLock()
		if slot.PositionStatus == PositionStatusFilled && slot.PositionQty > 0 {
			price := key.(float64)
			localTotal += slot.PositionQty
			filledSlots = append(filledSlots, filledSlot{
				Price:    price,
				Qty:      slot.PositionQty,
				Distance: math.Abs(price - currentPrice),
			})
		}
		slot.mu.RUnlock()
		return true
	})

	excess := localTotal - exchangePosition
	if excess <= 0.00000001 {
		logger.Info("✅ [强制同步] 本地持倉 %.6f 未超出交易所持倉 %.6f，無需修剪", localTotal, exchangePosition)
		return
	}

	logger.Warn("🚨 [强制同步] 本地持倉 %.6f > 交易所持倉 %.6f，多餘 %.6f，開始修剪幻影槽位",
		localTotal, exchangePosition, excess)

	// 2. 按距離當前價格的距離降序排序（最遠的排前面，優先清除）
	sort.Slice(filledSlots, func(i, j int) bool {
		return filledSlots[i].Distance > filledSlots[j].Distance
	})

	// 3. 從最遠的槽位開始清除，直到多餘量被消除
	trimmed := 0
	for _, fs := range filledSlots {
		if excess <= 0.00000001 {
			break
		}

		slotRaw, ok := spm.slots.Load(fs.Price)
		if !ok {
			continue
		}
		slot := slotRaw.(*InventorySlot)
		slot.mu.Lock()

		// 再次確認槽位仍然是 FILLED 狀態
		if slot.PositionStatus != PositionStatusFilled || slot.PositionQty <= 0 {
			slot.mu.Unlock()
			continue
		}

		if slot.PositionQty <= excess+0.00000001 {
			// 整個槽位都是多餘的，完全清除
			logger.Warn("🧹 [强制同步] 清除幻影槽位 價格=%s 數量=%.6f（距離當前價 %.2f）",
				formatPrice(slot.Price, spm.priceDecimals), slot.PositionQty, fs.Distance)
			excess -= slot.PositionQty
			slot.PositionStatus = PositionStatusEmpty
			slot.PositionQty = 0
			slot.OrderID = 0
			slot.OrderStatus = OrderStatusNotPlaced
			slot.OrderSide = ""
			slot.ClientOID = ""
			slot.SlotStatus = SlotStatusFree
			trimmed++
		} else {
			// 槽位數量大於多餘量，部分修剪（這種情況較少見）
			logger.Warn("✂️ [强制同步] 部分修剪槽位 價格=%s 數量 %.6f -> %.6f（扣除幻影 %.6f）",
				formatPrice(slot.Price, spm.priceDecimals), slot.PositionQty, slot.PositionQty-excess, excess)
			slot.PositionQty -= excess
			excess = 0
		}

		slot.mu.Unlock()
	}

	if trimmed > 0 {
		logger.Info("✅ [强制同步] 已修剪 %d 個幻影槽位，本地持倉已對齊交易所持倉 %.6f", trimmed, exchangePosition)
	}
}

// initializeSellSlotsFromPosition 從現有持倉初始化賣單槽位（用於程序重啟后恢複状態）
func (spm *SuperPositionManager) initializeSellSlotsFromPosition(totalPosition float64) {
	if totalPosition <= 0 {
		return
	}

	// 0. 獲取杠杆倍數（用於计算實際使用的保证金）
	leverage := 1
	if spm.isSpot() {
		leverage = 1
	} else {
		ctx := context.Background()
		// 先尝試從帳戶資訊中的持倉獲取杠杆倍數（GetAccount 返回的持倉資訊通常包含杠杆）
		if accountResult, err := spm.exchange.GetAccount(ctx); err == nil && accountResult != nil {
			accountValue := reflect.ValueOf(accountResult)
			if accountValue.Kind() == reflect.Ptr {
				accountValue = accountValue.Elem()
			}
			// 尝試從 Account.Positions 字段獲取持倉信息
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
								// 尝試獲取 Leverage 字段
								if leverageField := posValue.FieldByName("Leverage"); leverageField.IsValid() && leverageField.CanInterface() {
									if lev, ok := leverageField.Interface().(int); ok && lev > 0 {
										leverage = lev
										logger.Debug("🔍 [持倉恢複] 從账戶持倉資訊中獲取到杠杆倍數: %dx", leverage)
										break
									}
								}
							}
						}
					}
				}
			}
			// 如果從持倉中獲取不到，尝試從账戶级别的杠杆字段獲取
			if leverage == 1 {
				if leverageField := accountValue.FieldByName("AccountLeverage"); leverageField.IsValid() && leverageField.CanInterface() {
					if lev, ok := leverageField.Interface().(int); ok && lev > 0 {
						leverage = lev
						logger.Debug("🔍 [持倉恢複] 從账戶级别獲取到杠杆倍數: %dx", leverage)
					}
				}
			}
		}
		// 如果從账戶中獲取不到，尝試從 GetPositions 獲取
		if leverage == 1 {
			if positionsInterface, err := spm.exchange.GetPositions(ctx, spm.config.Trading.Symbol); err == nil && positionsInterface != nil {
				positionsValue := reflect.ValueOf(positionsInterface)
				if positionsValue.Kind() == reflect.Slice {
					for i := 0; i < positionsValue.Len(); i++ {
						posValue := positionsValue.Index(i)
						if posValue.Kind() == reflect.Ptr {
							posValue = posValue.Elem()
						} else if posValue.Kind() == reflect.Interface {
							posValue = posValue.Elem()
						}
						if leverageField := posValue.FieldByName("Leverage"); leverageField.IsValid() && leverageField.CanInterface() {
							if lev, ok := leverageField.Interface().(int); ok && lev > 0 {
								leverage = lev
								logger.Debug("🔍 [持倉恢複] 從 GetPositions 獲取到杠杆倍數: %dx", leverage)
								break
							}
						}
					}
				}
			}
		}
	}

	logger.Info("🔍 [持倉恢複] 检测到杠杆倍數: %dx，將使用實際保证金（倉位價值 / 杠杆）计算已用资金", leverage)

	// 1. 计算每單的理論數量（基於當前價格）
	// 使用锚点價格作為参考價格，使用從交易所獲取的數量精度

	// 每單的理論數量 = 目標金額 / 锚点價格
	theoryQtyPerSlot := spm.config.Trading.OrderQuantity / spm.anchorPrice
	theoryQtyPerSlot = roundPrice(theoryQtyPerSlot, spm.quantityDecimals)

	// 2. 计算需要創建的總槽位數
	totalSlotsNeeded := int(math.Ceil(totalPosition / theoryQtyPerSlot))
	logger.Info("🔄 [持倉恢複] 總持倉: %.4f，每單理論數量: %.4f，需要創建 %d 個槽位",
		totalPosition, theoryQtyPerSlot, totalSlotsNeeded)

	// 3. 确定窗口大小（前N個槽位可以立即挂賣單）
	sellWindowSize := spm.config.Trading.SellWindowSize
	if sellWindowSize <= 0 {
		sellWindowSize = spm.config.Trading.BuyWindowSize // 默认與買單窗口相同
	}

	// 4. 计算賣單槽位價格（從锚点價格 + 利潤間距开始）
	// 賣單最低價 = 锚点價格 + 利潤間距（避免與買單最高價冲突）
	sellStartPrice := spm.anchorPrice + spm.getEffectiveProfitSpread()
	sellPrices := spm.calculateSlotPrices(sellStartPrice, totalSlotsNeeded, "up")
	sellPrices = spm.optimizeSlotPricesWithOrderBook(context.Background(), spm.config.Trading.Symbol, sellPrices)

	logger.Info("🔄 [持倉恢複] 從價格 %s 向上創建 %d 個槽位（前 %d 個將挂賣單）",
		formatPrice(sellStartPrice, spm.priceDecimals), totalSlotsNeeded, sellWindowSize)

	// 5. 先计算所有槽位的理論數量總和（固定金額模式）
	var totalTheoryQty float64
	theoryQtys := make([]float64, len(sellPrices))
	for i, price := range sellPrices {
		theoryQty := spm.config.Trading.OrderQuantity / price
		theoryQty = roundPrice(theoryQty, spm.quantityDecimals)
		theoryQtys[i] = theoryQty
		totalTheoryQty += theoryQty
	}

	logger.Debug("🔍 [持倉恢複] 理論總數量: %.4f, 實際持倉: %.4f, 比例: %.4f",
		totalTheoryQty, totalPosition, totalPosition/totalTheoryQty)

	// 6. 按比例分配實際持倉到各個槽位，並累加已用资金
	var allocatedQty float64
	var totalUsedAmount float64 // 累加已用资金

	for i, price := range sellPrices {
		// 计算這個槽位应該分配的數量
		var slotQty float64
		if i == len(sellPrices)-1 {
			// 最后一個槽位：分配剩餘的所有持倉（避免舍入误差）
			slotQty = totalPosition - allocatedQty
		} else {
			// 按比例分配：實際數量 = 理論數量 × (總持倉 / 理論總數量)
			slotQty = theoryQtys[i] * (totalPosition / totalTheoryQty)
			slotQty = roundPrice(slotQty, spm.quantityDecimals)

			// 确保不超過剩餘持倉
			remaining := totalPosition - allocatedQty
			if slotQty > remaining {
				slotQty = remaining
			}
		}

		if slotQty <= 0 {
			logger.Warn("⚠️ [持倉恢複] 槽位 %s 分配數量過小 %.4f，跳過（已分配: %.4f / 總计: %.4f）",
				formatPrice(price, spm.priceDecimals), slotQty, allocatedQty, totalPosition)
			continue
		}

		// 7. 創建或更新槽位
		slot := spm.getOrCreateSlot(price)
		slot.mu.Lock()

		// 設置為有倉状態
		slot.PositionStatus = PositionStatusFilled
		slot.PositionQty = slotQty
		
		// 🔥 設置平均买入价格（恢复持仓时，使用槽位价格作为平均买入价格）
		// 因为无法知道实际买入价格，使用槽位价格作为近似值
		if slot.AvgBuyPrice <= 0 {
			slot.AvgBuyPrice = price
		}

		// 清空订單信息，但設置方向為SELL（因為这是恢複的持倉，將来要挂賣單）
		slot.OrderID = 0
		slot.OrderStatus = OrderStatusNotPlaced
		slot.OrderSide = "SELL" // 恢複持倉時標記為賣單方向
		slot.ClientOID = ""
		slot.OrderFilledQty = 0

		slot.mu.Unlock()

		allocatedQty += slotQty
		// 累加已用资金：使用實際保证金（倉位價值 / 杠杆倍數）而不是倉位價值
		// 锚点價格是市场當前價格，接近實際買入的平均價格
		// 不能用賣出價格（sellPrice），因為賣出價格是目標價，會高估成本
		// 對於有杠杆的交易，實際使用的保证金 = 倉位價值 / 杠杆倍數
		positionValue := spm.anchorPrice * slotQty        // 倉位價值
		actualMargin := positionValue / float64(leverage) // 實際使用的保证金
		totalUsedAmount += actualMargin

		// 日志標記：是否在窗口内（只打印前10個和最后10個）
		if i < 10 || i >= len(sellPrices)-10 {
			inWindow := ""
			if i < sellWindowSize {
				inWindow = " [可挂單]"
			} else {
				inWindow = " [暂不挂單]"
			}
			logger.Info("✅ [持倉恢複] 槽位 %s: 分配持倉 %.4f (理論: %.4f)%s",
				formatPrice(price, spm.priceDecimals), slotQty, theoryQtys[i], inWindow)
		} else if i == 10 {
			logger.Info("... （省略中间 %d 個槽位）", len(sellPrices)-20)
		}
	}

	logger.Info("✅ [持倉恢複] 完成持倉恢複，總持倉: %.4f，已分配: %.4f，差异: %.4f",
		totalPosition, allocatedQty, totalPosition-allocatedQty)

	// 🔥 初始化已用资金：使用實際保证金（倉位價值 / 杠杆倍數）而不是倉位價值
	// 这样资金限額限制的是實際投入的资金，而不是倉位價值
	if totalUsedAmount > 0 {
		spm.allocationManager.SetUsedAmount(spm.exchangeName, spm.config.Trading.Symbol, totalUsedAmount)
		positionValue := spm.anchorPrice * totalPosition // 總倉位價值
		logger.Info("💰 [%s] [资金分配] 恢複持倉，初始化已用资金: %.2f USDT (實際保证金，杠杆 %dx，倉位價值: %.2f USDT)",
			spm.logPrefix(), totalUsedAmount, leverage, positionValue)
	}

	// 8. 提示用戶后续會自动下賣單
	logger.Info("💡 [持倉恢複] 前 %d 個槽位的賣單將在價格調整時自动創建", sellWindowSize)
	logger.Info("💡 [持倉恢複] 其餘 %d 個槽位保持有倉状態，價格接近時自动挂單", totalSlotsNeeded-sellWindowSize)
}

// initializeBuySlotsFromPosition 從現有做空持倉初始化買單平倉槽位（SHORT 方向專用）
func (spm *SuperPositionManager) initializeBuySlotsFromPosition(totalPosition float64) {
	if totalPosition <= 0 {
		return
	}
	// 做空持倉：槽位價格 = 開倉賣價（高於錨點），平倉買價 = 槽位價格 - interval
	theoryQtyPerSlot := spm.config.Trading.OrderQuantity / spm.anchorPrice
	theoryQtyPerSlot = roundPrice(theoryQtyPerSlot, spm.quantityDecimals)
	totalSlotsNeeded := int(math.Ceil(totalPosition / theoryQtyPerSlot))
	sellWindowSize := spm.config.Trading.SellWindowSize
	if sellWindowSize <= 0 {
		sellWindowSize = spm.config.Trading.BuyWindowSize
	}
	sellStartPrice := spm.anchorPrice + spm.getEffectiveProfitSpread()
	sellPrices := spm.calculateSlotPrices(sellStartPrice, totalSlotsNeeded, "up")
	sellPrices = spm.optimizeSlotPricesWithOrderBook(context.Background(), spm.config.Trading.Symbol, sellPrices)

	var totalTheoryQty float64
	theoryQtys := make([]float64, len(sellPrices))
	for i, price := range sellPrices {
		theoryQty := spm.config.Trading.OrderQuantity / price
		theoryQty = roundPrice(theoryQty, spm.quantityDecimals)
		theoryQtys[i] = theoryQty
		totalTheoryQty += theoryQty
	}

	var allocatedQty float64
	for i, price := range sellPrices {
		var slotQty float64
		if i == len(sellPrices)-1 {
			slotQty = totalPosition - allocatedQty
		} else {
			slotQty = theoryQtys[i] * (totalPosition / totalTheoryQty)
			slotQty = roundPrice(slotQty, spm.quantityDecimals)
			if slotQty > totalPosition-allocatedQty {
				slotQty = totalPosition - allocatedQty
			}
		}
		if slotQty <= 0 {
			continue
		}
		slot := spm.getOrCreateSlot(price)
		slot.mu.Lock()
		slot.PositionStatus = PositionStatusFilled
		slot.PositionQty = slotQty
		if slot.AvgBuyPrice <= 0 {
			slot.AvgBuyPrice = price
		}
		slot.OrderID = 0
		slot.OrderStatus = OrderStatusNotPlaced
		slot.OrderSide = "BUY" // 做空平倉為買單
		slot.ClientOID = ""
		slot.OrderFilledQty = 0
		slot.mu.Unlock()
		allocatedQty += slotQty
	}
	logger.Info("✅ [持倉恢複] 做空持倉恢複完成，總持倉: %.4f，已分配: %.4f", totalPosition, allocatedQty)
}

// ===== 状態打印功能 =====

// PrintPositions 打印持倉状態（由 main.go 定期調用和退出時調用）
// 注意：該方法内部使用 totalBuyQty 和 totalSellQty 统计數據
func (spm *SuperPositionManager) PrintPositions() {
	// 從配置中獲取交易對信息
	symbol := spm.config.Trading.Symbol
	currentPositionsMsg := logger.Translate("log.position.current_positions", map[string]interface{}{"Symbol": symbol})
	logger.Info("%s", currentPositionsMsg)
	total := 0.0
	count := 0

	// 收集所有持倉數據
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

	// 按價格從高到低排序
	sort.Slice(positions, func(i, j int) bool {
		return positions[i].Price > positions[j].Price
	})

	// 從交易所接口獲取基础币种（支援U本位和币本位合約）
	baseCurrency := spm.exchange.GetBaseAsset()

	// 打印持倉（從高到低）
	for _, pos := range positions {
		statusIcon := "🟢" // 有持倉
		priceStr := formatPrice(pos.Price, spm.priceDecimals)

		// 使用翻譯函數獲取持倉信息
		positionDesc := logger.Translate("log.position.position_info", map[string]interface{}{
			"Qty":      fmt.Sprintf("%.4f", pos.Qty),
			"Currency": baseCurrency,
		})

		orderInfo := ""
		if pos.OrderStatus != OrderStatusNotPlaced && pos.OrderStatus != "" {
			orderInfo = ", " + logger.Translate("log.position.order_info", map[string]interface{}{
				"Side":    pos.OrderSide,
				"Status":  pos.OrderStatus,
				"OrderID": pos.OrderID,
			})
		}

		// 🔥 總是显示槽位状態,便於調試
		slotStatusInfo := ""
		if pos.SlotStatus != "" {
			slotStatusInfo = " [" + logger.Translate("log.position.slot_status", map[string]interface{}{
				"Status": pos.SlotStatus,
			}) + "]"
		} else {
			slotStatusInfo = " [" + logger.Translate("log.position.slot_empty") + "]"
		}

		// 格式化買入時间（使用订單創建時间作為買入時间参考）
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
	// 預计盈利 = 累计賣出數量 × 利潤間距（每笔盈利 = 利潤間距 × 數量）
	estimatedProfit := totalSellQty * spm.getEffectiveProfitSpread()
	logger.Info("[%s] 累计買入: %.2f, 累计賣出: %.2f, 預计盈利: %.2f U",
		spm.config.Trading.Symbol, totalBuyQty, totalSellQty, estimatedProfit)

	// === 新增：打印買單窗口详细信息 ===
	logger.Info("🔍 ===== 買單窗口状態 [%s] =====", spm.logPrefix())

	// 獲取最后的市场價格
	lastPrice, ok := spm.lastMarketPrice.Load().(float64)
	if !ok || lastPrice <= 0 {
		lastPrice = spm.anchorPrice // 如果没有更新過，使用锚点價格
	}
	logger.Info("[%s] 當前市場價格: %s", spm.logPrefix(), formatPrice(lastPrice, spm.priceDecimals))

	// 收集所有槽位信息（包括買單和空槽位）
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

	// 按價格從高到低排序
	sort.Slice(allSlots, func(i, j int) bool {
		return allSlots[i].Price > allSlots[j].Price
	})

	// 找到最接近當前價格的网格價格
	currentGridPrice := spm.findNearestGridPrice(lastPrice)
	logger.Info("[%s] 當前网格價格: %s", spm.logPrefix(), formatPrice(currentGridPrice, spm.priceDecimals))

	// 计算買單窗口範圍（當前网格價格下方的買單窗口）
	buyWindowSize := spm.config.Trading.BuyWindowSize
	buyWindowPrices := spm.calculateSlotPrices(currentGridPrice, buyWindowSize, "down")

	// 創建價格查找表
	buyWindowPriceMap := make(map[string]bool)
	for _, p := range buyWindowPrices {
		buyWindowPriceMap[formatPrice(p, spm.priceDecimals)] = true
	}

	// 打印買單窗口内的所有槽位
	logger.Info("[%s] 買單窗口大小: %d 個槽位 (當前网格價格下方)", spm.logPrefix(), buyWindowSize)
	buyOrderCount := 0
	emptySlotCount := 0
	filledSlotCount := 0

	for _, slot := range allSlots {
		priceStr := formatPrice(slot.Price, spm.priceDecimals)
		// 只打印買單窗口内的槽位
		if buyWindowPriceMap[priceStr] {
			statusIcon := "⚪" // 空槽位
			statusDesc := ""

			if slot.PositionStatus == PositionStatusFilled {
				statusIcon = "🟢" // 有持倉
				statusDesc = fmt.Sprintf("持倉: %.4f %s", slot.PositionQty, baseCurrency)
				filledSlotCount++
			} else {
				statusDesc = "無持倉"
				emptySlotCount++
			}

			orderInfo := ""
			if slot.OrderStatus != OrderStatusNotPlaced && slot.OrderStatus != "" {
				orderInfo = fmt.Sprintf(", 订單: %s/%s (ID:%d)", slot.OrderSide, slot.OrderStatus, slot.OrderID)
				if slot.OrderSide == "BUY" && (slot.OrderStatus == OrderStatusPlaced ||
					slot.OrderStatus == OrderStatusConfirmed ||
					slot.OrderStatus == OrderStatusPartiallyFilled) {
					buyOrderCount++
				}
			}

			// 🔥 總是显示槽位状態,便於調試
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

	logger.Info("[%s] 窗口统计: %d 個買單活跃, %d 個已持倉, %d 個空槽位",
		spm.logPrefix(), buyOrderCount, filledSlotCount, emptySlotCount)
	logger.Info("==========================")
}

// 辅助函數
// roundPrice 價格四舍五入
func roundPrice(price float64, decimals int) float64 {
	multiplier := math.Pow(10, float64(decimals))
	return math.Round(price*multiplier) / multiplier
}

// formatPrice 格式化價格字符串，使用指定的小數位數
func formatPrice(price float64, decimals int) string {
	return fmt.Sprintf("%.*f", decimals, price)
}

// GetUnrealizedPnL 獲取未實現盈虧（供快照、API 等使用）
func (spm *SuperPositionManager) GetUnrealizedPnL(currentPrice float64) float64 {
	return spm.calculateUnrealizedPnL(currentPrice)
}

// GetTotalPositionValueAtPrice 獲取在給定價格下的持倉總價值（供快照、API 等使用）
func (spm *SuperPositionManager) GetTotalPositionValueAtPrice(currentPrice float64) float64 {
	return spm.calculateTotalPositionValue(currentPrice)
}

// calculateUnrealizedPnL 计算未實現盈亏
func (spm *SuperPositionManager) calculateUnrealizedPnL(currentPrice float64) float64 {
	totalPnL := 0.0
	spm.slots.Range(func(key, value interface{}) bool {
		slotPrice := key.(float64)
		slot := value.(*InventorySlot)
		slot.mu.RLock()
		if slot.PositionStatus == PositionStatusFilled && slot.PositionQty > 0 {
			// 盈亏 = (當前價格 - 買入價格) * 數量
			totalPnL += (currentPrice - slotPrice) * slot.PositionQty
		}
		slot.mu.RUnlock()
		return true
	})
	return totalPnL
}

// calculateTotalPositionValue 计算當前持倉總價值
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

// GetLastMarketPrice 獲取最後市場價格（供開倉控制器等使用）
func (spm *SuperPositionManager) GetLastMarketPrice() float64 {
	v := spm.lastMarketPrice.Load()
	if v == nil {
		return 0
	}
	return v.(float64)
}

// GetActiveLayers 统计當前持倉层數
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

// CleanupEmptySlots 清理空槽位（定期調用，防止 sync.Map 記憶體泄漏）
// 清理条件：空倉、無订單、無訂單歷史
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

	// 刪除空槽位
	deletedCount := 0
	for _, price := range toDelete {
		spm.slots.Delete(price)
		deletedCount++
	}

	if deletedCount > 0 {
		logger.Debug("🧹 [槽位清理] 已清理 %d 個空槽位", deletedCount)
	}

	return deletedCount
}

// optimizeSlotPricesWithOrderBook 根據訂單簿深度優化槽位價格
// 🔥 P2 新增：订單簿優化掛單功能
func (spm *SuperPositionManager) optimizeSlotPricesWithOrderBook(ctx context.Context, symbol string, slotPrices []float64) []float64 {
	// 检查是否启用訂單簿優化
	if !spm.config.Trading.OrderbookOptimization.Enabled {
		return slotPrices
	}

	// 检查優化間隔
	now := time.Now()
	if spm.config.Trading.OrderbookOptimization.OptimizationInterval > 0 {
		lastOptTime, ok := spm.lastOptimizationTime.Load().(time.Time)
		if ok && now.Sub(lastOptTime).Seconds() < float64(spm.config.Trading.OrderbookOptimization.OptimizationInterval) {
			// 還未到優化時間
			return slotPrices
		}
	}

	// 獲取訂單簿數據
	orderbook, err := spm.exchange.GetOrderBook(ctx, symbol, spm.config.Trading.OrderbookOptimization.DepthLevels)
	if err != nil {
		logger.Warn("🔥 [訂單簿優化] 獲取訂單簿失敗: %v，使用原始價格", err)
		return slotPrices
	}

	// 更新最后優化時間
	spm.lastOptimizationTime.Store(now)

	optimizedPrices := make([]float64, 0, len(slotPrices))
	priceInterval := spm.config.Trading.PriceInterval

	for _, candidatePrice := range slotPrices {
		optimizedPrice := spm.optimizeSinglePrice(candidatePrice, orderbook, priceInterval)
		optimizedPrices = append(optimizedPrices, optimizedPrice)
	}

	return optimizedPrices
}

// optimizeSinglePrice 優化單個價格點
func (spm *SuperPositionManager) optimizeSinglePrice(candidatePrice float64, orderbook *OrderBook, priceInterval float64) float64 {
	cfg := &spm.config.Trading.OrderbookOptimization
	lookbackLevels := cfg.LookbackLevels
	minDepthUSDT := float64(cfg.MinDepthUSDT)

	// 判斷這是買單還是賣單（基於價格相對於當前市場的位置）
	// 這裡簡化處理：假設低於市場價的是買單，高於市場價的是賣單
	// 使用訂單簿中間價作為參考
	if len(orderbook.Bids) == 0 || len(orderbook.Asks) == 0 {
		// 訂單簿數據不完整，返回原價格
		return candidatePrice
	}

	midPrice := (orderbook.Bids[0].Price + orderbook.Asks[0].Price) / 2
	isBuyOrder := candidatePrice < midPrice

	if isBuyOrder {
		// 買單：檢查附近ask檔位深度，向下微調到有量的位置
		return spm.optimizeBuyPrice(candidatePrice, orderbook.Asks, lookbackLevels, minDepthUSDT, priceInterval)
	} else {
		// 賣單：檢查附近bid檔位深度，向上微調到有量的位置
		return spm.optimizeSellPrice(candidatePrice, orderbook.Bids, lookbackLevels, minDepthUSDT, priceInterval)
	}
}

// optimizeBuyPrice 優化買單價格（檢查ask檔位）
func (spm *SuperPositionManager) optimizeBuyPrice(candidatePrice float64, asks []OrderBookLevel, lookbackLevels int, minDepthUSDT, priceInterval float64) float64 {
	// 取前 N 檔 ask 的累計深度
	nearbyDepth := spm.calculateNearbyDepth(asks, lookbackLevels)

	if nearbyDepth >= minDepthUSDT {
		// 深度足夠，不需要調整
		return candidatePrice
	}

	// 深度不足，向下微調到下一個有量的ask檔位
	targetPrice := spm.findNextLiquidLevel(candidatePrice, asks, minDepthUSDT, -1, priceInterval)

	// 確保微調後價格不偏離太多
	maxAdjustment := priceInterval * 0.1 // 最大調整幅度為price_interval的10%
	if math.Abs(targetPrice-candidatePrice) > maxAdjustment {
		if targetPrice < candidatePrice {
			targetPrice = candidatePrice - maxAdjustment
		} else {
			targetPrice = candidatePrice + maxAdjustment
		}
	}

	if targetPrice != candidatePrice {
		logger.Debug("🔥 [訂單簿優化] 買單價格從 %.4f 調整到 %.4f (深度不足: %.0f < %.0f USDT)",
			candidatePrice, targetPrice, nearbyDepth, minDepthUSDT)
	}

	return targetPrice
}

// optimizeSellPrice 優化賣單價格（檢查bid檔位）
func (spm *SuperPositionManager) optimizeSellPrice(candidatePrice float64, bids []OrderBookLevel, lookbackLevels int, minDepthUSDT, priceInterval float64) float64 {
	// 取前 N 檔 bid 的累計深度
	nearbyDepth := spm.calculateNearbyDepth(bids, lookbackLevels)

	if nearbyDepth >= minDepthUSDT {
		// 深度足夠，不需要調整
		return candidatePrice
	}

	// 深度不足，向上微調到下一個有量的bid檔位
	targetPrice := spm.findNextLiquidLevel(candidatePrice, bids, minDepthUSDT, 1, priceInterval)

	// 確保微調後價格不偏離太多
	maxAdjustment := priceInterval * 0.1 // 最大調整幅度為price_interval的10%
	if math.Abs(targetPrice-candidatePrice) > maxAdjustment {
		if targetPrice < candidatePrice {
			targetPrice = candidatePrice - maxAdjustment
		} else {
			targetPrice = candidatePrice + maxAdjustment
		}
	}

	if targetPrice != candidatePrice {
		logger.Debug("🔥 [訂單簿優化] 賣單價格從 %.4f 調整到 %.4f (深度不足: %.0f < %.0f USDT)",
			candidatePrice, targetPrice, nearbyDepth, minDepthUSDT)
	}

	return targetPrice
}

// calculateNearbyDepth 計算前 N 檔的累計深度（depth_usdt = price * quantity）
func (spm *SuperPositionManager) calculateNearbyDepth(levels []OrderBookLevel, lookbackLevels int) float64 {
	totalDepth := 0.0
	for i := 0; i < len(levels) && i < lookbackLevels; i++ {
		totalDepth += levels[i].Price * levels[i].Quantity
	}
	return totalDepth
}

// findNextLiquidLevel 找到第一個有足夠流動性的價格檔位並微調
// 買單：在 asks 中從低到高找第一個深度足夠的檔位，返回略低於該價格（下移）
// 賣單：在 bids 中從高到低找第一個深度足夠的檔位，返回略高於該價格（上移）
func (spm *SuperPositionManager) findNextLiquidLevel(candidatePrice float64, levels []OrderBookLevel, minDepthUSDT float64, direction int, priceInterval float64) float64 {
	epsilon := priceInterval * 0.01
	for _, level := range levels {
		depth := level.Price * level.Quantity
		if depth >= minDepthUSDT {
			if direction < 0 {
				// 買單：略低於該檔位
				return level.Price - epsilon
			}
			// 賣單：略高於該檔位
			return level.Price + epsilon
		}
	}
	return candidatePrice
}

// supplementCommission 補充手續費（當 WebSocket 未提供時）
func (spm *SuperPositionManager) supplementCommission(ctx context.Context, orderID int64, symbol, side string, slot *InventorySlot) {
	if spm.exchange == nil {
		return
	}

	// 查詢訂單成交記錄
	fillsRaw, err := spm.exchange.GetOrderFills(ctx, symbol, orderID)
	if err != nil || fillsRaw == nil {
		logger.Debug("🔍 [手續費補充] 訂單 %d 查詢成交記錄失敗或不支援: %v", orderID, err)
		return
	}

	// 嘗試解析為 []*exchange.OrderFill
	fills, ok := fillsRaw.([]interface{})
	if !ok || len(fills) == 0 {
		logger.Debug("🔍 [手續費補充] 訂單 %d 無成交記錄", orderID)
		return
	}

	// 計算總手續費
	totalCommission := 0.0
	commissionAsset := "USDT"
	for _, fillRaw := range fills {
		// 使用反射或類型斷言解析結構
		fillMap, ok := fillRaw.(map[string]interface{})
		if !ok {
			// 嘗試反射獲取字段
			rv := reflect.ValueOf(fillRaw)
			if rv.Kind() == reflect.Ptr {
				rv = rv.Elem()
			}
			if rv.Kind() != reflect.Struct {
				continue
			}
			// 查找 Commission 字段
			commField := rv.FieldByName("Commission")
			assetField := rv.FieldByName("CommissionAsset")
			if commField.IsValid() && commField.Kind() == reflect.Float64 {
				totalCommission += commField.Float()
			}
			if assetField.IsValid() && assetField.Kind() == reflect.String && assetField.String() != "" {
				commissionAsset = assetField.String()
			}
			continue
		}

		// 從 map 中提取手續費
		if comm, ok := fillMap["Commission"].(float64); ok {
			totalCommission += comm
		} else if commStr, ok := fillMap["Commission"].(string); ok {
			if comm, err := strconv.ParseFloat(commStr, 64); err == nil {
				totalCommission += comm
			}
		}
		if asset, ok := fillMap["CommissionAsset"].(string); ok && asset != "" {
			commissionAsset = asset
		}
	}

	if totalCommission == 0 {
		logger.Debug("🔍 [手續費補充] 訂單 %d 手續費為 0", orderID)
		return
	}

	logger.Info("💰 [手續費補充] 訂單 %d (%s) 補充手續費: %.8f %s", orderID, side, totalCommission, commissionAsset)

	// 更新 slot 中的手續費
	spm.mu.Lock()
	if side == "BUY" {
		slot.BuyFee += totalCommission
		if commissionAsset != "" {
			slot.FeeAsset = commissionAsset
		}
	} else {
		// 賣單：需要更新已保存的交易記錄
		// 注意：這裡只能更新 slot，無法回溯更新已保存的 trades 記錄
		// 如果需要更新 trades 記錄，需要額外的機制（如定期同步）
		logger.Debug("💰 [手續費補充] 賣單 %d 手續費已補充，但無法回溯更新已保存的交易記錄", orderID)
	}
	spm.mu.Unlock()
}
