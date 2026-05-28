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
	"quantmesh/storage"
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

	// PositionLeg 槽位腿別（單向淨持倉雙向網格 BOTH）：多腿 / 空腿
	PositionLegNone  = ""
	PositionLegLong  = "LONG"
	PositionLegShort = "SHORT"
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

	// PositionLeg 單向淨持倉雙向網格（BOTH）專用：該槽位當前為多腿或空腿；LONG/SHORT 模式可為空
	PositionLeg string

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
	GetLatestPrice(ctx context.Context, symbol string) (float64, error)
	// GetQuoteAsset 计價資產（如 USDT），現貨買單預算裁剪用
	GetQuoteAsset() string
	GetBalance(ctx context.Context, asset string) (float64, error)
}

// TradeStorage 交易存儲介面（避免循環匯入）
// 用於保存交易記錄（買賣配對）
type TradeStorage interface {
	SaveTrade(buyOrderID, sellOrderID int64, exchange, symbol string, buyPrice, sellPrice, quantity, pnl, fee float64, feeAsset string, createdAt time.Time, botID string) error
	// 🔥 SaveTradeWithDeviation 保存交易記錄（包含價格偏差）
	SaveTradeWithDeviation(buyOrderID, sellOrderID int64, exchange, symbol string, buyPrice, sellPrice, quantity, pnl, fee float64, feeAsset string, buyPriceDeviation, sellPriceDeviation float64, createdAt time.Time, botID string) error
	// 🔥 SaveTradeWithExchangePnL 保存交易記錄（包含交易所盈虧）
	SaveTradeWithExchangePnL(buyOrderID, sellOrderID int64, exchange, symbol string, buyPrice, sellPrice, quantity, pnl, exchangePnL, fee float64, feeAsset string, buyPriceDeviation, sellPriceDeviation float64, createdAt time.Time, botID string) error
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
	slotFilter   *config.SlotFilterConfig
	slotFilterMu sync.RWMutex

	// 智能掛單管理器
	smartOrderMgr *SmartOrderManager

	// ReduceOnly 槽位冷却期：同一槽位 ReduceOnly 失败后，短期内不再尝试下平仓单（防止重复告警）
	reduceOnlyCooldown sync.Map // map[float64]time.Time

	// 网格自动重建管理器（可选，用于价格偏离时自动调整网格锚点）
	autoRebuilder *GridAutoRebuilder

	// 關閉條件：滿足時調用此回調以停止 Bot（由 symbol_manager 注入）
	requestStopFunc func()

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
		strategyName:       strategyName, // 策略名称
		strategyType:       "grid",       // 策略類型固定為 grid
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

	// 現貨不支援賣開空，BOTH 降級為 LONG
	if strings.EqualFold(cfg.Trading.Direction, "BOTH") && cfg.Trading.MarketType == "spot" {
		logger.Warn("⚠️ [%s] 現貨不支援雙向網格（合約賣開空），已將 direction 降級為 LONG", botID)
		cfg.Trading.Direction = "LONG"
	}

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

	storage.AppendBotRiskControlEvent(spm.botID, "paused", reason, "opening_manager")

	// 撤銷所有開倉委託
	spm.CancelAllOpenOrders()

	// 為了確保萬無一失，特別是在幣安合約等場景，
	// 如果本地 slots 狀態同步有延遲，直接調用交易所接口撤銷開倉方向的所有訂單
	go func() {
		// 延遲一小段時間，等待可能的本地狀態更新
		time.Sleep(1 * time.Second)

		openSideBuy := "BUY"
		if spm.isShort() {
			openSideBuy = "SELL"
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		// 獲取交易所所有掛單
		openOrdersInterface, err := spm.exchange.GetOpenOrders(ctx, spm.config.Trading.Symbol)
		if err != nil {
			logger.Error("❌ [%s] 暫停開倉時獲取掛單失敗: %v", spm.logPrefix(), err)
			return
		}

		var toCancel []int64

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
						if spm.isBoth() {
							if sideStr == "BUY" || sideStr == "SELL" {
								toCancel = append(toCancel, idField.Int())
							}
						} else if sideStr == openSideBuy {
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

	storage.AppendBotRiskControlEvent(spm.botID, "resumed", "", "opening_manager")
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
	var orderIDs []int64
	var prices []float64

	spm.slots.Range(func(key, value interface{}) bool {
		price := key.(float64)
		slot := value.(*InventorySlot)

		slot.mu.RLock()
		match := false
		if spm.isBoth() {
			// 空槽上的買開或賣開
			if slot.PositionStatus == PositionStatusEmpty && slot.PositionQty < 1e-12 &&
				slot.OrderID > 0 &&
				slot.OrderStatus != OrderStatusCanceled && slot.OrderStatus != OrderStatusCancelRequested {
				match = slot.OrderSide == "BUY" || slot.OrderSide == "SELL"
			}
		} else {
			openSide := "BUY"
			if spm.isShort() {
				openSide = "SELL"
			}
			match = slot.OrderSide == openSide && slot.OrderID > 0 &&
				slot.OrderStatus != OrderStatusCanceled && slot.OrderStatus != OrderStatusCancelRequested
		}
		if match {
			orderIDs = append(orderIDs, slot.OrderID)
			prices = append(prices, price)
		}
		slot.mu.RUnlock()
		return true
	})

	if len(orderIDs) == 0 {
		return
	}

	sideLabel := "開倉委託"
	if !spm.isBoth() {
		sideLabel = "買單"
		if spm.isShort() {
			sideLabel = "賣單"
		}
	}
	logger.Info("🔄 [開倉管理] 準備撤銷 %d 個%s", len(orderIDs), sideLabel)

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
				match := false
				if spm.isBoth() {
					if slot.PositionStatus == PositionStatusEmpty && slot.PositionQty < 1e-12 &&
						slot.OrderID > 0 &&
						slot.OrderStatus != OrderStatusCanceled && slot.OrderStatus != OrderStatusCancelRequested {
						match = slot.OrderSide == "BUY" || slot.OrderSide == "SELL"
					}
				} else {
					openSide := "BUY"
					if spm.isShort() {
						openSide = "SELL"
					}
					match = slot.OrderSide == openSide && slot.OrderID > 0 &&
						slot.OrderStatus != OrderStatusCanceled && slot.OrderStatus != OrderStatusCancelRequested
				}
				if match {
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

// StartAutoRebuild 啟動網格自動重建
func (spm *SuperPositionManager) StartAutoRebuild(cfg config.GridAutoRebuildConfig) {
	if spm.autoRebuilder != nil {
		logger.Warn("⚠️ [%s] 網格自動重建已經在運行中", spm.logPrefix())
		return
	}

	spm.autoRebuilder = NewGridAutoRebuilder(spm, cfg)
	spm.autoRebuilder.Start()
}

// StopAutoRebuild 停止網格自動重建
func (spm *SuperPositionManager) StopAutoRebuild() {
	if spm.autoRebuilder == nil {
		return
	}

	spm.autoRebuilder.Stop()
	spm.autoRebuilder = nil
}

// IsAutoRebuildEnabled 檢查是否啟用了自動重建
func (spm *SuperPositionManager) IsAutoRebuildEnabled() bool {
	return spm.autoRebuilder != nil
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

// SetRequestStopFunc 設置關閉條件觸發時的回調（用於自動停止 Bot）
func (spm *SuperPositionManager) SetRequestStopFunc(fn func()) {
	spm.mu.Lock()
	defer spm.mu.Unlock()
	spm.requestStopFunc = fn
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
// 格式: {price_int}_{side}_{timestamp}{seq} 或 {price_int}_{side}_{timestamp}{seq}_SL
// price_int: price * 10^decimals (轉為整數)
// side: B=Buy, S=Sell
// orderSource: 可選，傳 "stop_loss" 時追加 _SL，便於從交易所訂單中解析訂單來源
// OKX：clOrdId 僅允許字母數字且 ≤32，含下劃線會被拒（51000 Parameter clOrdId error），改用無下劃線格式（見 utils.GenerateOrderIDWithSourceOKX）
func (spm *SuperPositionManager) generateClientOrderID(price float64, side string, orderSource string) string {
	if spm.exchangeName == "okx" {
		return utils.GenerateOrderIDWithSourceOKX(price, side, spm.priceDecimals, orderSource)
	}
	return utils.GenerateOrderIDWithSource(price, side, spm.priceDecimals, orderSource)
}

func (spm *SuperPositionManager) findSlotByOrderID(orderID int64) (*InventorySlot, float64, bool) {
	if orderID <= 0 {
		return nil, 0, false
	}
	var (
		foundSlot  *InventorySlot
		foundPrice float64
		found      bool
	)
	spm.slots.Range(func(key, value interface{}) bool {
		slot, ok := value.(*InventorySlot)
		if !ok || slot == nil || slot.OrderID != orderID {
			return true
		}
		price, ok := key.(float64)
		if !ok {
			return true
		}
		foundSlot = slot
		foundPrice = price
		found = true
		return false
	})
	return foundSlot, foundPrice, found
}

// isReduceOnlyCooldown 检查槽位是否处于 ReduceOnly 冷却期（2 分钟内不再尝试平仓）
func (spm *SuperPositionManager) isReduceOnlyCooldown(slotPrice float64) bool {
	const cooldown = 2 * time.Minute
	if v, ok := spm.reduceOnlyCooldown.Load(slotPrice); ok {
		t := v.(time.Time)
		return time.Since(t) < cooldown
	}
	return false
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

// clipSpotBuyOrdersByQuoteBudget 現貨做多：按計價幣可用餘額裁剪開倉買單，優先保留更接近市價的買價（價格更高者）
func (spm *SuperPositionManager) clipSpotBuyOrdersByQuoteBudget(orders []*OrderRequest, openSide string) []*OrderRequest {
	if !spm.isSpot() || spm.isShort() || openSide != "BUY" || len(orders) == 0 {
		return orders
	}
	ctx := context.Background()
	quote := spm.exchange.GetQuoteAsset()
	if quote == "" {
		quote = "USDT"
	}
	avail, err := spm.exchange.GetBalance(ctx, quote)
	if err != nil {
		logger.Warn("⚠️ [%s] [現貨買單預算] 獲取 %s 可用餘額失败，跳過裁剪: %v", spm.logPrefix(), quote, err)
		return orders
	}
	if avail <= 0 {
		logger.Debug("💰 [%s] [現貨買單預算] %s 可用為 0，跳過裁剪", spm.logPrefix(), quote)
		return orders
	}
	type buyCand struct {
		req      *OrderRequest
		notional float64
		price    float64
	}
	var buys []buyCand
	var rest []*OrderRequest
	for _, o := range orders {
		if o.Side == openSide {
			buys = append(buys, buyCand{req: o, notional: o.Price * o.Quantity, price: o.Price})
		} else {
			rest = append(rest, o)
		}
	}
	if len(buys) == 0 {
		return orders
	}
	sort.Slice(buys, func(i, j int) bool { return buys[i].price > buys[j].price })
	remaining := avail
	var kept []*OrderRequest
	dropped := 0
	for _, b := range buys {
		if b.notional <= remaining+1e-8 {
			kept = append(kept, b.req)
			remaining -= b.notional
			continue
		}
		dropped++
		if price, _, valid := spm.parseClientOrderID(b.req.ClientOrderID); valid {
			slot := spm.getOrCreateSlot(price)
			slot.mu.Lock()
			if slot.SlotStatus == SlotStatusPending {
				slot.SlotStatus = SlotStatusFree
			}
			slot.mu.Unlock()
		}
	}
	if dropped > 0 {
		logger.Info("💰 [%s] [現貨買單預算] %s 可用 %.4f，保留 %d 筆買單，刪除 %d 筆超出可用計價資產的買單",
			spm.logPrefix(), quote, avail, len(kept), dropped)
	}
	out := append(kept, rest...)
	return out
}

// SetSpotInventoryPolicy 運行時同步現貨庫存策略（熱更新）
func (spm *SuperPositionManager) SetSpotInventoryPolicy(p string) {
	spm.config.Trading.SpotInventoryPolicy = config.NormalizeSpotInventoryPolicy(p)
}


// normalizeOrderStatus 將各交易所訂單狀態統一為 SPM 內使用的枚舉（與 Binance 等一致的大寫）。
// OKX v5 WebSocket 的 state 為 live / partially_filled / filled / canceled（小寫+下劃線），
// 若不在此處歸一化，OnOrderUpdate 的 switch 無法命中，槽位會永久卡在 CANCEL_REQUESTED+LOCKED。
func normalizeOrderStatus(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	lower := strings.ToLower(s)
	switch lower {
	case "live", "new":
		return "NEW"
	case "partially_filled":
		return "PARTIALLY_FILLED"
	case "filled":
		return "FILLED"
	case "canceled", "cancelled":
		return "CANCELED"
	case "rejected":
		return "REJECTED"
	case "expired":
		return "EXPIRED"
	default:
		// 已是 NEW、FILLED 等標準寫法則保持
		if s == "NEW" || s == "PARTIALLY_FILLED" || s == "FILLED" || s == "CANCELED" || s == "REJECTED" || s == "EXPIRED" {
			return s
		}
		return strings.ToUpper(strings.ReplaceAll(lower, " ", "_"))
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
	// 三級火箭模式：小波動小網格、大波動大網格
	if rtc := spm.config.Trading.RocketTieredGrid; rtc != nil && rtc.Enabled && len(rtc.Tiers) > 0 {
		return spm.calculateSlotPricesRocket(gridPrice, count, direction)
	}

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

// calculateSlotPricesRocket 三級火箭模式：按檔位生成槽位價格
// 檔位 0：前 4 格 100 間距；檔位 1：接下來 4 格 300 間距；檔位 2：其餘 600 間距
func (spm *SuperPositionManager) calculateSlotPricesRocket(gridPrice float64, count int, direction string) []float64 {
	tiers := spm.config.Trading.RocketTieredGrid.Tiers
	if len(tiers) == 0 {
		tiers = []config.RocketTier{
			{FilledThreshold: 4, Interval: 100, ProfitSpread: 100},
			{FilledThreshold: 8, Interval: 300, ProfitSpread: 300},
			{FilledThreshold: 0, Interval: 600, ProfitSpread: 600},
		}
	}
	baseInterval := spm.config.Trading.PriceInterval
	if baseInterval <= 0 {
		baseInterval = 100
	}

	var prices []float64
	cumulativeOffset := 0.0

	for i := 0; i < count; i++ {
		interval := spm.getRocketIntervalForSlotIndex(i, tiers, baseInterval)
		cumulativeOffset += interval

		var price float64
		if direction == "down" {
			price = gridPrice - cumulativeOffset
		} else {
			price = gridPrice + cumulativeOffset
		}
		price = roundPrice(price, spm.priceDecimals)

		if price <= 0 {
			logger.Warn("⚠️ [%s] 跳過無效火箭槽位價格 %.8f（方向=%s, 索引=%d, 网格價格=%.2f）",
				spm.logPrefix(), price, direction, i, gridPrice)
			continue
		}

		prices = append(prices, price)
	}

	return prices
}

// getRocketIntervalForSlotIndex 根據槽位索引返回該檔的間距
// filled_threshold：槽位索引小於此值時使用該檔
func (spm *SuperPositionManager) getRocketIntervalForSlotIndex(slotIndex int, tiers []config.RocketTier, defaultInterval float64) float64 {
	for _, t := range tiers {
		if t.FilledThreshold > 0 && slotIndex < t.FilledThreshold {
			if t.Interval > 0 {
				return t.Interval
			}
			return defaultInterval
		}
	}
	if len(tiers) > 0 {
		last := tiers[len(tiers)-1]
		if last.Interval > 0 {
			return last.Interval
		}
	}
	return defaultInterval
}

// getProfitSpreadForSlot 獲取指定槽位的平倉利差（三級火箭時按檔位，否則用全局）
func (spm *SuperPositionManager) getProfitSpreadForSlot(slotPrice, gridPrice float64) float64 {
	rtc := spm.config.Trading.RocketTieredGrid
	if rtc == nil || !rtc.Enabled {
		return spm.getEffectiveProfitSpread()
	}
	tiers := rtc.Tiers
	if len(tiers) == 0 {
		tiers = []config.RocketTier{
			{FilledThreshold: 4, Interval: 100, ProfitSpread: 100},
			{FilledThreshold: 8, Interval: 300, ProfitSpread: 300},
			{FilledThreshold: 0, Interval: 600, ProfitSpread: 600},
		}
	}

	// 根據 slot 與 gridPrice 的距離推斷槽位索引（LONG 時 slot 在下方，SHORT 時在上方）
	var slotIndex int
	if spm.isShort() {
		// SHORT：買單在上方，slotPrice > gridPrice
		diff := slotPrice - gridPrice
		slotIndex = spm.inferRocketSlotIndex(diff, tiers)
	} else {
		// LONG：買單在下方，slotPrice < gridPrice
		diff := gridPrice - slotPrice
		slotIndex = spm.inferRocketSlotIndex(diff, tiers)
	}

	for _, t := range tiers {
		if t.FilledThreshold > 0 && slotIndex < t.FilledThreshold {
			if t.ProfitSpread > 0 {
				return t.ProfitSpread
			}
			if t.Interval > 0 {
				return t.Interval
			}
		}
	}
	if len(tiers) > 0 {
		last := tiers[len(tiers)-1]
		if last.ProfitSpread > 0 {
			return last.ProfitSpread
		}
		if last.Interval > 0 {
			return last.Interval
		}
	}
	return spm.getEffectiveProfitSpread()
}

// inferRocketSlotIndex 根據價格差推斷槽位索引（用於 getProfitSpreadForSlot）
func (spm *SuperPositionManager) inferRocketSlotIndex(priceDiff float64, tiers []config.RocketTier) int {
	baseInterval := spm.config.Trading.PriceInterval
	if baseInterval <= 0 {
		baseInterval = 100
	}
	if len(tiers) == 0 {
		tiers = []config.RocketTier{
			{FilledThreshold: 4, Interval: 100},
			{FilledThreshold: 8, Interval: 300},
			{FilledThreshold: 0, Interval: 600},
		}
	}

	cumulative := 0.0
	for i := 0; i < 100; i++ {
		interval := spm.getRocketIntervalForSlotIndex(i, tiers, baseInterval)
		cumulative += interval
		if cumulative >= priceDiff-0.01 {
			return i
		}
	}
	return 99
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

// GetPendingBuyOrderValueUSDT 獲取當前掛單買單佔用的資金（USDT），用於資金管理展示
// 統計所有 OrderSide=BUY 且 OrderStatus 為 Placed/Confirmed/PartiallyFilled 的訂單金額
func (spm *SuperPositionManager) GetPendingBuyOrderValueUSDT() float64 {
	orderQty := spm.config.Trading.OrderQuantity
	var total float64
	spm.slots.Range(func(key, value interface{}) bool {
		slot := value.(*InventorySlot)
		slot.mu.RLock()
		if slot.OrderSide == "BUY" && slot.OrderPrice > 0 &&
			(slot.OrderStatus == OrderStatusPlaced || slot.OrderStatus == OrderStatusConfirmed ||
				slot.OrderStatus == OrderStatusPartiallyFilled) {
			orderValue := orderQty
			if slot.OrderFilledQty > 0 {
				filledValue := slot.OrderPrice * slot.OrderFilledQty
				orderValue = orderQty - filledValue
				if orderValue < 0 {
					orderValue = 0
				}
			}
			total += orderValue
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
		"profit_spread":    profitSpread,
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
			if spm.isBoth() && slot.PositionLeg == PositionLegShort {
				entry := slot.AvgBuyPrice
				if entry <= 0 {
					entry = slotPrice
				}
				totalPnL += (entry - currentPrice) * slot.PositionQty
			} else if spm.isBoth() && slot.PositionLeg == PositionLegLong {
				if slot.AvgBuyPrice > 0 {
					totalPnL += (currentPrice - slot.AvgBuyPrice) * slot.PositionQty
				} else {
					totalPnL += (currentPrice - slotPrice) * slot.PositionQty
				}
			} else {
				// 盈亏 = (當前價格 - 買入價格) * 數量（單向 LONG/SHORT 與舊行為一致）
				totalPnL += (currentPrice - slotPrice) * slot.PositionQty
			}
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
