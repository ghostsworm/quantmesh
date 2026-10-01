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
	"quantmesh/execution"
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
	Commission      float64 // 本次成交手續費（僅本筆成交，非累計；現貨適配器已換算為計價幣）
	CommissionAsset string  // 手續費幣種
	CommissionKnown bool    // true 表示適配器确认该成交的 Commission 字段权威（含明确零费用）
	RealizedPnL     float64 // 已實現盈虧（交易所計算）
	// BaseFeeQty 本次成交中以「基礎幣」扣收的手續費數量（基礎幣單位，>=0；0 表示未按基礎幣收費或未知）。
	// 僅現貨有意義：買單實際到帳數量 = 本次成交增量 − BaseFeeQty。
	// 該費用的計價幣價值仍包含在 Commission 中（用於盈虧），兩者不是重複扣費。
	BaseFeeQty float64
}

// BatchPlaceOrdersResult 批量下單結果
type BatchPlaceOrdersResult struct {
	PlacedOrders     []*Order        // 成功下單的订單列表
	HasMarginError   bool            // 是否出現保证金不足錯误
	ReduceOnlyErrors map[string]bool // ReduceOnly錯误的订單（key為ClientOrderID）
	UnknownOrders    map[string]bool // 結果不確定，必須保留槽位與資金
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
	Type          string // empty=LIMIT, MARKET allowed only through capable executors
	TimeInForce   string
	Price         float64
	Quantity      float64
	PriceDecimals int    // 價格小數位數（用於格式化價格字符串）
	ReduceOnly    bool   // 是否只减倉（平倉單）
	PositionSide  string // 可選：該單所屬持倉腿（PositionSideLong/PositionSideShort），與 Side 一起明確開/平倉；空表示按策略註冊或全局方向推斷
	PostOnly      bool   // 是否只做 Maker（Post Only）
	ClientOrderID string // 自定义订單ID
	StrategyName  string // 策略名称（可選，用於日志追踪）
	StrategyType  string // 策略類型（可選，如 "grid", "dca", "martingale"）
	OrderSource   string // 订單來源（"normal"=正常限價, "stop_loss"=止損平倉, "liquidation"=強制平倉）
	ExposureKey   string // stable owned inventory lot for pending-inclusive risk admission
	BotWideClose  bool   // explicit Bot-owned manual close, not strategy-generated
}

// OrderRequest.PositionSide 取值
const (
	PositionSideLong                  = "LONG"
	PositionSideShort                 = "SHORT"
	gridInitializationUnverifiedBlock = "grid_initialization_unverified"
)

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
	ExecutedQty   float64
	AvgPrice      float64
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
	OrderStatusUnknown         = "UNKNOWN"          // 提交可能已受理，待核實
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
	OrderID             int64     // 订單ID
	ClientOID           string    // 自定义订單ID
	OrderSide           string    // 订單方向 (BUY/SELL)
	OrderStatus         string    // 订單状態
	OrderPrice          float64   // 订單價格
	OrderFilledQty      float64   // 成交數量
	OrderFilledNotional float64   // 已處理累計成交額，與 OrderFilledQty 同一游標
	OrderCreatedAt      time.Time // 創建時间

	// 🔥 新增：槽位鎖定状態，防止並发重複操作
	SlotStatus string // FREE/PENDING/LOCKED

	// PostOnly 连续被拒/過期计數：平倉價按此計數逐 tick 遠離盤口（不再降級為普通單），成交後重置
	PostOnlyFailCount int

	// 買入手續費累計（該槽位持倉對應的買單手續費，賣出時按比例攤銷）
	BuyFee   float64
	FeeAsset string

	// feeClientOID/orderCommission 當前訂單（按 ClientOrderID）已由推送累計的手續費，
	// 用於判斷是否需要 REST 補查手續費，避免推送已帶手續費時補查重複累加。
	feeClientOID        string
	orderCommission     float64
	feeValuationUnknown bool
	// orderBaseFeeQty 當前訂單推送已攜帶並已從持倉扣除的基礎幣手續費數量（防止 REST 補查重複扣減）
	orderBaseFeeQty float64
	// cycleGen 持倉週期代號：槽位持倉清空（平倉完成/強制同步清倉）時遞增。
	// 異步手續費補查攜帶發起時的代號，回來時不一致即說明原週期已結束，不得寫入新週期的 BuyFee/持倉。
	cycleGen uint64
	// A closed grid trade can safely receive a late opening-fee correction only
	// when its position cycle came from exactly one identified opening order.
	PositionEntryOrderID        int64
	PositionEntryOrderAmbiguous bool
	// feeSupplementUntil 現貨開倉買單正在 REST 補查手續費（可能需扣減基礎幣到帳數量）的截止時間；
	// 截止前暫緩為該槽位掛平倉單，避免按毛數量掛單超出實際可用餘額。零值表示無補查在途。
	feeSupplementUntil time.Time
	// pendingFeeSupplementCount tracks every asynchronous REST fee lookup. A
	// non-zero count is persisted and prevents an "empty" startup shortcut.
	pendingFeeSupplementCount int
	// lastFilledClientOID 最近一筆已完全成交（FILLED）的 ClientOrderID：
	// 槽位訂單信息重置後，同一訂單的重放/延遲推送不得再次記賬。
	lastFilledClientOID string
	// Terminal execution survives clearing the active cursor for REST/WS checks.
	lastTerminalFill FillProgress
	// baseFeeUnfloored 當前開倉訂單扣過現貨基礎幣手續費、持倉尚未按數量精度向下取整
	baseFeeUnfloored bool

	// 🔥 实际平均买入价格（用於准确计算盈亏）
	// 当买入订单成交时，使用实际成交价格更新此字段
	// 计算公式：AvgBuyPrice = (旧AvgBuyPrice * 旧持仓 + 新买入价格 * 新买入数量) / 总持仓
	AvgBuyPrice         float64
	CostBasisUnverified bool // 恢复/补差仓位缺少可核实的实际入场成本

	// AllocatedMargin 該槽位持倉占用的資金分配額度（開倉成交時由訂單預留轉入，平倉成交時按比例釋放）
	AllocatedMargin float64

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

// GridRuntimeStateStore persists the grid slot ledger independently of the
// trade ledger. A snapshot is evidence for later reconciliation, not authority
// to reopen trading by itself.
type GridRuntimeStateStore interface {
	LoadRuntimeState(strategyName string) (int, string, bool, error)
	SaveRuntimeState(strategyName string, schemaVersion int, payload string) error
}

const gridRuntimeStateSchemaVersion = 4

type gridRuntimeStateSnapshot struct {
	Version      int                       `json:"version"`
	BotID        string                    `json:"bot_id"`
	Exchange     string                    `json:"exchange"`
	MarketType   string                    `json:"market_type"`
	Symbol       string                    `json:"symbol"`
	Direction    string                    `json:"direction"`
	AnchorPrice  float64                   `json:"anchor_price"`
	LastMarket   float64                   `json:"last_market_price"`
	TotalBuyQty  float64                   `json:"total_buy_qty"`
	TotalSellQty float64                   `json:"total_sell_qty"`
	Slots        []gridRuntimeSlotSnapshot `json:"slots"`
}

type gridRuntimeSlotSnapshot struct {
	Price                     float64      `json:"price"`
	PositionStatus            string       `json:"position_status"`
	PositionQty               float64      `json:"position_qty"`
	OrderID                   int64        `json:"order_id"`
	ClientOID                 string       `json:"client_oid"`
	OrderSide                 string       `json:"order_side"`
	OrderStatus               string       `json:"order_status"`
	OrderPrice                float64      `json:"order_price"`
	OrderFilledQty            float64      `json:"order_filled_qty"`
	OrderFilledNotional       float64      `json:"order_filled_notional"`
	OrderCreatedAt            time.Time    `json:"order_created_at"`
	SlotStatus                string       `json:"slot_status"`
	PostOnlyFailCount         int          `json:"post_only_fail_count"`
	BuyFee                    float64      `json:"buy_fee"`
	FeeAsset                  string       `json:"fee_asset"`
	FeeClientOID              string       `json:"fee_client_oid"`
	OrderCommission           float64      `json:"order_commission"`
	FeeValuationUnknown       bool         `json:"fee_valuation_unknown"`
	OrderBaseFeeQty           float64      `json:"order_base_fee_qty"`
	CycleGen                  uint64       `json:"cycle_gen"`
	PositionEntryOrderID      int64        `json:"position_entry_order_id"`
	PositionEntryOrderUnknown bool         `json:"position_entry_order_unknown"`
	FeeSupplementUntil        time.Time    `json:"fee_supplement_until"`
	PendingFeeSupplementCount int          `json:"pending_fee_supplement_count"`
	LastFilledClientOID       string       `json:"last_filled_client_oid"`
	LastTerminalFill          FillProgress `json:"last_terminal_fill"`
	BaseFeeUnfloored          bool         `json:"base_fee_unfloored"`
	AvgBuyPrice               float64      `json:"avg_buy_price"`
	CostBasisUnverified       bool         `json:"cost_basis_unverified"`
	AllocatedMargin           float64      `json:"allocated_margin"`
	PositionLeg               string       `json:"position_leg"`
	StrategyName              string       `json:"strategy_name"`
	StrategyType              string       `json:"strategy_type"`
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
	// GetSellBias 獲取做空開倉（賣開）偏向係數，語義與 GetBuyBias 鏡像
	GetSellBias() float64
	// IsHighRate 判斷是否為高費率
	IsHighRate() bool
	// GetCurrentRate 獲取當前資金費率
	GetCurrentRate() float64
	// ShouldPauseBuying 判斷是否應該暫停買入
	ShouldPauseBuying() bool
}

// SuperPositionManager 超级倉位管理器
type SuperPositionManager struct {
	config                *config.Config
	riskControls          atomic.Pointer[config.RiskControls]
	verifiedCapitalLimit  float64      // immutable startup-verified gross notional ceiling
	volatilityPauseReason atomic.Value // string; independent of manual/scheduled pauses
	executor              OrderExecutorInterface
	exchange              IExchange
	exchangeName          string // 交易所名称（配置中的名称，如 "binance"）
	botID                 string // Bot 唯一標識，用於日誌區分同交易所同幣多實例

	// 策略信息（用於追踪订單来源）
	strategyName string // 策略名称（如 "Grid-BTCUSDT-1"）
	strategyType string // 策略類型（固定為 "grid"）

	// 價格锚点（初始化時的市场價格），以 float64 bits 形式原子存儲。
	// 讀者遍布多個 goroutine（網格計算、Web API、自動重建定時器），
	// 寫者 ShiftGrid/rebuild 在 spm.mu 內完成讀-改-寫。
	// 讀路徑刻意不取鎖：findNearestGridPrice 等函數調用鏈很深，
	// 在其中取 spm.mu 會與已持鎖的上游形成死鎖（RWMutex 不可重入）。
	anchorPriceBits atomic.Uint64
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
	// 開倉訂單資金預留記賬（按 ClientOrderID）
	allocReservations allocationReservations
	// 槓桿倍數緩存（WS 回調只讀緩存，不做 REST）
	leverage leverageCache
	// 帳戶信息緩存（AdjustOrders 資金分配與權益止損共用，避免每個 tick 調 GetAccount）
	account accountCache
	// 手續費率（費率感知最小利差）
	fees feeRateState
	// AdjustOrders 去抖狀態
	adjust adjustDebounce
	// K 線 regime 過濾 / 自適應間隔 / 邊界冻结（未注入時不生效）
	regimeCtl regimeControl

	// 事件總線（用於发送告警）
	eventBus EventBus

	// 统计（注意：以下字段被 safety.Reconciler 和 PrintPositions 使用，不可刪除）
	totalBuyQty          atomic.Value // float64 - 累计買入數量
	totalSellQty         atomic.Value // float64 - 累计賣出數量
	reconcileCount       atomic.Int64 // 對账次數
	lastReconcileTime    atomic.Value // time.Time - 最后對账時间
	lastOptimizationTime atomic.Value // time.Time - 最后訂單簿優化時间

	// 交易存儲（可選，用於保存交易記錄）
	tradeStorage                 TradeStorage
	gridRuntimeStateMu           sync.RWMutex
	gridRuntimeStateSaveMu       sync.Mutex
	gridRuntimeStateStore        GridRuntimeStateStore
	gridRuntimeStateRestored     atomic.Bool
	gridRuntimeVenueFlatVerified atomic.Bool
	feeSupplementQueueMu         sync.Mutex
	pendingFeeSupplements        []pendingFeeSupplement

	// 初始化標志
	isInitialized atomic.Bool

	// 暂停標志
	isPaused atomic.Bool

	// 開倉管理：僅暫停開倉（區別於 isPaused 暫停所有交易）
	isOpeningPaused    atomic.Bool
	openingPauseReason atomic.Value // string - 暫停原因
	manualPauseReason  atomic.Value // string - 人工暂停原因
	// openingPauseMu 串行化暫停狀態的「檢查原因 + 修改」，供按來源的條件暫停/恢復使用
	openingPauseMu sync.Mutex
	openingGate    execution.OpeningGate

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
	protective      protectiveLiquidation

	liquidationMu                  sync.Mutex
	liquidationActive              atomic.Bool
	liquidationNeedsReconciliation atomic.Bool

	// 時鐘（默認牆鐘；回放注入模擬時鐘，見 clock.go）
	clk clockHolder

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
	if cfg.Trading.OpenPositionControl.PauseOpening ||
		(cfg.Trading.OpenPositionControl.BotRiskControl != nil && cfg.Trading.OpenPositionControl.BotRiskControl.PauseOpening) {
		spm.openingGate.Block("manual")
		spm.manualPauseReason.Store("configured opening pause")
	}

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
	spm.openingGate.Block("trading_pause")
	spm.isPaused.Store(true)
	logger.Warn("⏸️ [%s] 倉位管理器已暂停交易", spm.logPrefix())
}

// Resume 恢複交易
func (spm *SuperPositionManager) Resume() {
	spm.openingGate.Unblock("trading_pause")
	spm.isPaused.Store(false)
	spm.markAdjustDirty()
	logger.Info("▶️ [%s] 倉位管理器已恢複交易", spm.logPrefix())
}

// IsPaused 是否已暂停
func (spm *SuperPositionManager) IsPaused() bool {
	return spm.isPaused.Load()
}

// PauseOpening 暫停開倉（並撤銷所有開倉委託）
func (spm *SuperPositionManager) PauseOpening(reason string) {
	spm.openingPauseMu.Lock()
	spm.setOpeningPausedLocked(reason)
	spm.openingPauseMu.Unlock()
	spm.afterOpeningPaused(reason)
}

// HoldManualOpeningPause adds a separate operator-owned opening gate. Risk
// coordinator releases only clear opening_manager and cannot override it.
func (spm *SuperPositionManager) HoldManualOpeningPause() {
	spm.openingGate.Block("manual")
	if spm.manualPauseReason.Load() == nil {
		spm.manualPauseReason.Store("用户明确暂停开仓")
	}
}

// PauseOpeningManually creates only the operator-owned block; it never alters
// the risk coordinator's opening_manager block.
func (spm *SuperPositionManager) PauseOpeningManually(reason string) {
	spm.openingGate.Block("manual")
	spm.manualPauseReason.Store(reason)
	spm.afterOpeningPausedBy(reason, "manual")
}

// PauseOpeningForSource adds a named non-manual owner without changing the
// generic opening-manager state.
func (spm *SuperPositionManager) PauseOpeningForSource(source, reason string) {
	if source == "" || source == "manual" || source == "opening_manager" {
		return
	}
	spm.openingGate.Block(source)
	spm.afterOpeningPausedBy(reason, source)
}

// ResumeOpeningForSource releases only the named owner's opening gate.
func (spm *SuperPositionManager) ResumeOpeningForSource(source string) {
	if source == "" {
		return
	}
	spm.openingGate.Unblock(source)
	spm.afterOpeningResumed()
}

// PauseOpeningUnlessHeld 條件暫停開倉：若已被 owned 判定為「非本來源」的原因暫停（如熔斷、複合風控、手動），
// 則保留原暫停與原因不覆蓋，返回 false。用於定時/週期等低優先級來源，避免把風控暫停改寫成自己的原因後又被自己恢復。
func (spm *SuperPositionManager) PauseOpeningUnlessHeld(reason string, owned func(reason string) bool) bool {
	spm.openingPauseMu.Lock()
	currentReason := spm.GetOpeningPauseReason()
	if spm.IsOpeningPaused() && !owned(currentReason) {
		spm.openingPauseMu.Unlock()
		return false
	}
	for _, controllerReason := range openingControllerPauseReasons() {
		spm.openingGate.Unblock(openingControllerGateSource(controllerReason))
	}
	spm.openingGate.Block(openingControllerGateSource(reason))
	spm.openingPauseMu.Unlock()
	spm.afterOpeningPausedBy(reason, openingControllerGateSource(reason))
	return true
}

// ResumeOpeningIfOwned 僅當當前暫停原因屬於調用方（owned 返回 true）時恢復開倉，返回是否恢復。
// 風控（熔斷器 / 複合風控 / 手動）設置的暫停不會被定時規則等來源解除。
func (spm *SuperPositionManager) ResumeOpeningIfOwned(owned func(reason string) bool) bool {
	spm.openingPauseMu.Lock()
	currentReason := spm.GetOpeningPauseReason()
	if !spm.IsOpeningPaused() || !owned(currentReason) {
		spm.openingPauseMu.Unlock()
		return false
	}
	released := false
	for _, controllerReason := range openingControllerPauseReasons() {
		if owned(controllerReason) && spm.openingGate.HasBlock(openingControllerGateSource(controllerReason)) {
			spm.openingGate.Unblock(openingControllerGateSource(controllerReason))
			released = true
		}
	}
	if !released && spm.isOpeningPaused.Load() {
		spm.clearOpeningPausedLocked()
		released = true
	}
	spm.openingPauseMu.Unlock()
	if !released {
		return false
	}
	spm.afterOpeningResumed()
	return true
}

func openingControllerPauseReasons() []string {
	return []string{openingPauseReasonPositionLimit, openingPauseReasonSchedule, openingPauseReasonPeriodic}
}

func openingControllerGateSource(reason string) string {
	return "opening_controller:" + reason
}

func (spm *SuperPositionManager) setOpeningPausedLocked(reason string) {
	spm.openingGate.Block("opening_manager")
	spm.isOpeningPaused.Store(true)
	spm.openingPauseReason.Store(reason)
}

func (spm *SuperPositionManager) clearOpeningPausedLocked() {
	spm.openingGate.Unblock("opening_manager")
	spm.isOpeningPaused.Store(false)
	spm.openingPauseReason.Store("")
}

// afterOpeningPaused 暫停後的副作用（日誌、事件、撤開倉單），不持 openingPauseMu 執行
func (spm *SuperPositionManager) afterOpeningPaused(reason string) {
	spm.afterOpeningPausedBy(reason, "opening_manager")
}

func (spm *SuperPositionManager) afterOpeningPausedBy(reason, owner string) {
	logger.Warn("⏸️ [%s] 開倉管理：已暫停開倉，原因: %s", spm.logPrefix(), reason)

	storage.AppendBotRiskControlEvent(spm.botID, "paused", reason, owner)

	if _, ok := spm.executor.(interface {
		CancelOwnedOpeningOrders(context.Context) error
	}); ok {
		// The owned executor drains previously admitted opens and verifies their
		// terminal state. An immediate slot-based sweep could race an in-flight
		// submit and miss the order created after that sweep.
		go spm.CancelResidualOpeningOrders()
		return
	}

	// 撤銷所有開倉委託
	spm.CancelAllOpenOrders()

	go spm.CancelResidualOpeningOrders()
}

// ResumeOpening 恢復開倉
func (spm *SuperPositionManager) ResumeOpening() {
	spm.openingPauseMu.Lock()
	spm.clearOpeningPausedLocked()
	spm.openingPauseMu.Unlock()
	spm.afterOpeningResumed()
}

func (spm *SuperPositionManager) afterOpeningResumed() {
	spm.markAdjustDirty()
	if spm.IsOpeningPaused() {
		reason := spm.GetOpeningPauseReason()
		logger.Warn("[%s] 恢復請求已處理，但獨立風控仍阻止開倉: %s", spm.logPrefix(), reason)
		owner := "opening_manager"
		if spm.openingGate.HasBlock("manual") {
			owner = "manual"
		} else {
			for _, controllerReason := range openingControllerPauseReasons() {
				if spm.openingGate.HasBlock(openingControllerGateSource(controllerReason)) {
					owner = openingControllerGateSource(controllerReason)
					break
				}
			}
			if owner == "opening_manager" && !spm.openingGate.HasBlock(owner) {
				owner = "other_opening_gate"
				for _, source := range spm.openingGate.Sources() {
					owner = source
					break
				}
			}
		}
		storage.AppendBotRiskControlEvent(spm.botID, "paused", reason, owner)
		return
	}
	logger.Info("▶️ [%s] 開倉管理：已恢復開倉", spm.logPrefix())

	storage.AppendBotRiskControlEvent(spm.botID, "resumed", "", "opening_manager")
}

// IsOpeningPaused 是否已暫停開倉
func (spm *SuperPositionManager) IsOpeningPaused() bool {
	return spm.isOpeningPaused.Load() || spm.openingGate.Blocked()
}

// OpeningGate is shared by every strategy executor belonging to this bot.
func (spm *SuperPositionManager) OpeningGate() *execution.OpeningGate {
	return &spm.openingGate
}

// BeginReconciliation freezes and drains the physical executor's order
// submissions for the complete snapshot-and-sync critical section.
func (spm *SuperPositionManager) BeginReconciliation(ctx context.Context) (func(), error) {
	barrier, ok := spm.executor.(interface {
		BeginPositionReconciliation(context.Context) (func(), error)
	})
	if !ok {
		return nil, fmt.Errorf("order executor does not support reconciliation barrier")
	}
	return barrier.BeginPositionReconciliation(ctx)
}

// FailReconciliation leaves physical submissions blocked when reconciliation
// cannot establish trustworthy local/exchange position evidence.
func (spm *SuperPositionManager) FailReconciliation(err error) {
	barrier, ok := spm.executor.(interface{ FailPositionReconciliation(error) })
	if !ok {
		spm.openingGate.Block(execution.PositionReconciliationUnverifiedBlock)
		logger.Error("[%s] 持倉對账协调锁失效，executor 不支持全量提交屏障", spm.logPrefix())
		return
	}
	barrier.FailPositionReconciliation(err)
}

// CompleteReconciliation releases only the dedicated unverified-position
// block after a successful authoritative snapshot. Other gate sources remain.
func (spm *SuperPositionManager) CompleteReconciliation() {
	barrier, ok := spm.executor.(interface{ CompletePositionReconciliation() })
	if ok {
		barrier.CompletePositionReconciliation()
	} else {
		spm.openingGate.Unblock(execution.PositionReconciliationUnverifiedBlock)
	}
	// A restored non-empty grid snapshot has its own startup hold. Only this
	// callback, reached after the reconciler validates positions, orders, and
	// execution intents, may release that hold.
	spm.openingGate.Unblock("grid_runtime_state_reconciliation")
	if spm.openingGate.HasBlock("grid_runtime_state_unverified") {
		spm.gridRuntimeStateMu.RLock()
		hasStore := spm.gridRuntimeStateStore != nil
		spm.gridRuntimeStateMu.RUnlock()
		if !hasStore {
			logger.Error("[%s] 核账已完成，但网格运行态存储不可用；保留持久化失败门控", spm.logPrefix())
			return
		}
		if err := spm.PersistGridRuntimeState(); err != nil {
			logger.Error("[%s] 核账已完成，但无法持久化已核实网格运行态；保留门控: %v", spm.logPrefix(), err)
			return
		}
		spm.openingGate.Unblock("grid_runtime_state_unverified")
	}
}

// GetOpeningPauseReason 獲取開倉暫停原因
func (spm *SuperPositionManager) GetOpeningPauseReason() string {
	if spm.openingGate.HasBlock("strategy_accounting_unverified") {
		return "策略成交账未核实，等待策略持仓与交易所成交对账"
	}
	if spm.openingGate.HasBlock("strategy_startup_unverified") {
		return "策略启动或状态恢复失败，等待运行态核实"
	}
	if spm.openingGate.HasBlock(gridInitializationUnverifiedBlock) {
		return "网格初始化或首批订单未核实，已封锁开仓并等待对账"
	}
	if spm.openingGate.HasBlock("combo_runtime_state_unverified") {
		return "Combo 运行态恢复未核实，已封锁开仓并等待对账"
	}
	v := spm.openingPauseReason.Load()
	if v != nil && v.(string) != "" {
		return v.(string)
	}
	if spm.openingGate.HasBlock("manual") {
		if reason := spm.manualPauseReason.Load(); reason != nil && reason.(string) != "" {
			return reason.(string)
		}
		return "用户明确暂停开仓，等待人工恢复或指定时长到期"
	}
	if spm.openingGate.HasBlock("unknown_orders") {
		return "訂單結果 UNKNOWN，等待成交與持倉核實"
	}
	if spm.openingGate.HasBlock("trade_ledger_unverified") {
		return "成交账本持久化失败，等待财务记录与持仓对账"
	}
	if spm.openingGate.HasBlock(liquidationBlockSource) {
		return "全平倉尚未完成核實，等待成交與持倉對賬"
	}
	if spm.openingGate.HasBlock(protectiveLiquidationBlock) {
		return "保護性平倉已觸發；完成後須明確恢復，失敗時須先對賬"
	}
	if spm.openingGate.HasBlock(execution.UnverifiedCancellationBlock) {
		return "殘餘開倉單尚未確認終止，等待撤單核實"
	}
	if spm.openingGate.HasBlock("market_risk") {
		return "行情或深度風控尚未解除"
	}
	if spm.openingGate.HasBlock(priceFeedStaleBlock) {
		return "價格推送已過期，新開倉已封鎖，等待行情恢復"
	}
	if spm.openingGate.HasBlock(volatilityRiskBlock) {
		if reason := spm.volatilityPauseReason.Load(); reason != nil {
			return reason.(string)
		}
		return "波动率暂停：等待行情核实"
	}
	if spm.openingGate.HasBlock(equityDataBlock) {
		return "帳戶權益、現金流水或高水位持久化尚未核實"
	}
	for _, reason := range openingControllerPauseReasons() {
		if spm.openingGate.HasBlock(openingControllerGateSource(reason)) {
			return reason
		}
	}
	sources := spm.openingGate.Sources()
	for _, source := range sources {
		if strings.HasPrefix(source, "single_leg_group:") {
			return "对冲组仍存在未运行腿，等待组状态恢复一致"
		}
	}
	if len(sources) > 0 {
		return fmt.Sprintf("风险来源 %s 仍暂停开仓", sources[0])
	}
	if spm.openingGate.Blocked() {
		return "其他風控來源仍暫停開倉"
	}
	return ""
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
			// 已部分成交的開倉餘單仍需撤銷，不能只檢查空槽。
			if bothSideIsOpen(slot.OrderSide, slot) &&
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

	for attempt := 1; attempt <= cancelRetryAttempts; attempt++ {
		if len(orderIDs) == 0 {
			break
		}
		if err := spm.executor.BatchCancelOrders(orderIDs); err != nil {
			logger.Error("❌ [開倉管理] 批量撤單失敗: %v", err)
		} else {
			for _, price := range prices {
				slot := spm.getOrCreateSlot(price)
				slot.mu.Lock()
				if slot.OrderStatus != OrderStatusFilled && slot.OrderStatus != OrderStatusCanceled {
					slot.OrderStatus = OrderStatusCancelRequested
				}
				slot.mu.Unlock()
			}
		}

		// 按注入時鐘等待撤單回報；回放中不真實等待
		spm.sleep(cancelSettleWait)

		if attempt < cancelRetryAttempts {
			orderIDs = nil
			prices = nil
			spm.slots.Range(func(key, value interface{}) bool {
				price := key.(float64)
				slot := value.(*InventorySlot)
				slot.mu.RLock()
				match := false
				if spm.isBoth() {
					if bothSideIsOpen(slot.OrderSide, slot) &&
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
				logger.Info("[開倉管理] 本輪撤單請求已提交，終態仍以回報與查單核實為準")
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

// SetGridRuntimeStateStore enables durable grid snapshots. Snapshot persistence
// failures keep opening blocked; snapshots are not used to bypass startup
// reconciliation with venue positions and open orders.
func (spm *SuperPositionManager) SetGridRuntimeStateStore(store GridRuntimeStateStore) {
	spm.gridRuntimeStateMu.Lock()
	spm.gridRuntimeStateStore = store
	spm.gridRuntimeStateMu.Unlock()
}

// isSpot 是否為現貨交易（現貨不使用 ReduceOnly、杠杆固定為 1）
func (spm *SuperPositionManager) isSpot() bool {
	return spm.config.Trading.MarketType == "spot"
}

// isShort 是否為做空方向
func (spm *SuperPositionManager) isShort() bool {
	return spm.config.Trading.Direction == "SHORT"
}

// favorableTrend 單向模式下對開倉方向有利的趨勢（LONG=up，SHORT=down）
func (spm *SuperPositionManager) favorableTrend() string {
	if spm.isShort() {
		return "down"
	}
	return "up"
}

// adverseTrend 單向模式下對開倉方向不利的趨勢（LONG=down，SHORT=up）
func (spm *SuperPositionManager) adverseTrend() string {
	if spm.isShort() {
		return "up"
	}
	return "down"
}

// openingFundingBias 單向模式下開倉方向的資金費率偏向係數（LONG=GetBuyBias，SHORT=GetSellBias）
func (spm *SuperPositionManager) openingFundingBias() float64 {
	if spm.fundingMonitor == nil {
		return 1.0
	}
	if spm.isShort() {
		return spm.fundingMonitor.GetSellBias()
	}
	return spm.fundingMonitor.GetBuyBias()
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
// 只讀取杠杆緩存，不發起網絡請求（可能在持有槽位鎖的 WS 回調中被調用）。
func (spm *SuperPositionManager) getActualMargin(orderValue float64) float64 {
	if orderValue <= 0 {
		return 0
	}
	return orderValue / float64(spm.cachedLeverage())
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
	now := spm.now()
	spm.fillTimestamps = append(spm.fillTimestamps, now)
	// 保留最近 2 分鐘的記錄，避免無限增長
	cutoff := now.Add(-fillHistoryRetention)
	for len(spm.fillTimestamps) > 0 && spm.fillTimestamps[0].Before(cutoff) {
		spm.fillTimestamps = spm.fillTimestamps[1:]
	}
}

// GetFillCountInLastMinute 獲取過去 1 分鐘內的成交次數（會 prune 超過 1 分鐘的紀錄）
func (spm *SuperPositionManager) GetFillCountInLastMinute() int {
	spm.fillMu.Lock()
	defer spm.fillMu.Unlock()
	cutoff := spm.now().Add(-fillRateWindow)
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
		spm.openingGate.Block(gridInitializationUnverifiedBlock)
		return fmt.Errorf("初始價格無效: %.2f", initialPrice)
	}

	// 1. 設置價格锚点（精度信息已經在構造函數中設置，從交易所獲取）
	if !spm.gridRuntimeStateRestored.Load() {
		spm.setAnchorPrice(initialPrice)
	}
	spm.lastMarketPrice.Store(initialPrice) // 初始化最后市场價格
	logger.Info("✅ 價格锚点已設置: %s, 價格精度:%d, 數量精度:%d",
		formatPrice(initialPrice, spm.priceDecimals), spm.priceDecimals, spm.quantityDecimals)

	// 2. 直接使用锚点價格作為网格價格（不再對齐到整數）
	initialGridPrice := spm.anchorPrice()
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
		spm.openingGate.Unblock(gridInitializationUnverifiedBlock)
		// 標記為已初始化
		spm.isInitialized.Store(true)
		logger.Info("✅ 初始化完成，网格價格: %s", formatPrice(initialGridPrice, spm.priceDecimals))
	} else {
		spm.openingGate.Block(gridInitializationUnverifiedBlock)
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
	if v, ok := spm.reduceOnlyCooldown.Load(slotPrice); ok {
		t := v.(time.Time)
		return spm.since(t) < reduceOnlyCooldownDuration
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
	if spm.anchorPrice() > 1000 && price < 1000 && price > 0 {
		logger.Warn("⚠️ [價格解析异常] ClientOrderID=%s, 解析價格=%.2f, 锚点價格=%.2f, priceDecimals=%d",
			clientOrderID, price, spm.anchorPrice(), spm.priceDecimals)
		logger.Warn("💡 [可能原因] 1) 此订單属於其他交易對 2) priceDecimals 参數錯误 3) 历史遗留订單")
		logger.Warn("💡 [建议] 检查是否运行了多個交易對，确保订單推送已正确過滤 Symbol")

		// 尝試使用不同的 priceDecimals 重新解析（用於诊断）
		for testDecimals := 1; testDecimals <= 3; testDecimals++ {
			if testDecimals == spm.priceDecimals {
				continue
			}
			testPrice, _, _, testValid := utils.ParseOrderID(cleanID, testDecimals)
			if testValid && testPrice > 1000 && math.Abs(testPrice-spm.anchorPrice()) < spm.anchorPrice()*0.5 {
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
	if spm.gridRuntimeStateRestored.Load() {
		logger.Info("✅ [初始化] 已加载网格运行态快照，保留原槽位与持仓成本；待启动核账完成后再调整委托")
		return nil
	}
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
		if spm.anchorPrice() <= 0 {
			return roundPrice(currentPrice, spm.priceDecimals)
		}
		logRatio := math.Log(1 + ratio)
		k := math.Round(math.Log(currentPrice/spm.anchorPrice()) / logRatio)
		gridPrice := spm.anchorPrice() * math.Pow(1+ratio, k)
		return roundPrice(gridPrice, spm.priceDecimals)
	}
	// 等差
	offset := currentPrice - spm.anchorPrice()
	intervals := math.Round(offset / spm.config.Trading.PriceInterval)
	gridPrice := spm.anchorPrice() + intervals*spm.config.Trading.PriceInterval
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
	Price               float64
	PositionStatus      string
	PositionQty         float64
	CostBasisUnverified bool
	OrderID             int64
	OrderSide           string
	OrderStatus         string
	OrderCreatedAt      time.Time
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
			Price:               price,
			PositionStatus:      slot.PositionStatus,
			PositionQty:         slot.PositionQty,
			CostBasisUnverified: slot.CostBasisUnverified,
			OrderID:             slot.OrderID,
			OrderSide:           slot.OrderSide,
			OrderStatus:         slot.OrderStatus,
			OrderCreatedAt:      slot.OrderCreatedAt,
		}

		// 返回槽位數據
		return fn(price, data)
	})
}

// DetailedSlotData 详细槽位數據結構（包含所有字段）
type DetailedSlotData struct {
	Price                 float64
	PositionStatus        string
	PositionQty           float64
	AvgBuyPrice           float64
	BuyFee                float64
	CostBasisUnverified   bool
	FeeValuationUnknown   bool
	PendingFeeSupplements int
	PositionLeg           string
	OrderID               int64
	ClientOID             string
	OrderSide             string
	OrderStatus           string
	OrderPrice            float64
	OrderFilledQty        float64
	OrderCreatedAt        time.Time
	SlotStatus            string
	StrategyName          string // 策略名称
	StrategyType          string // 策略類型
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
			Price:                 price,
			PositionStatus:        slot.PositionStatus,
			PositionQty:           slot.PositionQty,
			AvgBuyPrice:           slot.AvgBuyPrice,
			BuyFee:                slot.BuyFee,
			CostBasisUnverified:   slot.CostBasisUnverified,
			FeeValuationUnknown:   slot.feeValuationUnknown,
			PendingFeeSupplements: slot.pendingFeeSupplementCount,
			PositionLeg:           slot.PositionLeg,
			OrderID:               slot.OrderID,
			ClientOID:             slot.ClientOID,
			OrderSide:             slot.OrderSide,
			OrderStatus:           slot.OrderStatus,
			OrderPrice:            slot.OrderPrice,
			OrderFilledQty:        slot.OrderFilledQty,
			OrderCreatedAt:        slot.OrderCreatedAt,
			SlotStatus:            slot.SlotStatus,
			StrategyName:          slot.StrategyName,
			StrategyType:          slot.StrategyType,
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

// GetPnLAsset returns the exchange-reported quote/settlement asset used by
// this position manager; an empty value means the denomination is unknown.
func (spm *SuperPositionManager) GetPnLAsset() string {
	if spm == nil || spm.exchange == nil {
		return ""
	}
	return strings.ToUpper(strings.TrimSpace(spm.exchange.GetQuoteAsset()))
}

func (spm *SuperPositionManager) GetDirection() string {
	return strings.ToUpper(strings.TrimSpace(spm.config.Trading.Direction))
}

// GetAllocationManager 獲取资金分配管理器（供倉位计划等模塊按交易對設置限額）
func (spm *SuperPositionManager) GetAllocationManager() *AllocationManager {
	return spm.allocationManager
}

// GetTotalPositionValueUSDT 獲取當前持倉總價值（USDT），用於倉位计划進度检查
func (spm *SuperPositionManager) GetTotalPositionValueUSDT() float64 {
	value, verified := spm.GetTotalPositionValueUSDTVerified()
	if !verified {
		return math.MaxFloat64
	}
	return value
}

// GetTotalPositionValueUSDTVerified returns slot-price exposure only when all
// filled inventory and its aggregate remain finite and non-negative.
func (spm *SuperPositionManager) GetTotalPositionValueUSDTVerified() (float64, bool) {
	var total float64
	verified := true
	spm.slots.Range(func(key, value interface{}) bool {
		slot := value.(*InventorySlot)
		slot.mu.RLock()
		if slot.PositionStatus == PositionStatusFilled {
			if !finiteGridValue(slot.PositionQty) || slot.PositionQty < 0 {
				verified = false
			} else if slot.PositionQty > 0 && (!finiteGridValue(slot.Price) || slot.Price <= 0) {
				verified = false
			} else if slot.PositionQty > 0 {
				value := slot.Price * slot.PositionQty
				nextTotal := total + value
				if !finiteGridValue(value) || !finiteGridValue(nextTotal) {
					verified = false
				} else {
					total = nextTotal
				}
			}
		}
		slot.mu.RUnlock()
		return true
	})
	if !verified {
		return 0, false
	}
	return total, true
}

// GetNetPositionQty 獲取本 Bot 槽位淨持倉數量（多為正、空為負），供平倉按實際持倉計算數量
// LONG/SHORT 模式按配置方向取符號；BOTH 模式按槽位腿別累加
func (spm *SuperPositionManager) GetNetPositionQty() float64 {
	var net float64
	spm.slots.Range(func(key, value interface{}) bool {
		slot := value.(*InventorySlot)
		slot.mu.RLock()
		if slot.PositionStatus == PositionStatusFilled && slot.PositionQty > 0 {
			if spm.liquidationIsShortLeg(slot.PositionLeg) {
				net -= slot.PositionQty
			} else {
				net += slot.PositionQty
			}
		}
		slot.mu.RUnlock()
		return true
	})
	return net
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

// getEffectiveProfitSpread 獲取有效利潤間距：ProfitSpread > 0 則使用，否則回退到 PriceInterval；
// 再與費率下界取大（參考價為最新市價，無市價時用錨點；網格單均為 PostOnly，按 maker 費率）
func (spm *SuperPositionManager) getEffectiveProfitSpread() float64 {
	refPrice, _ := spm.lastMarketPrice.Load().(float64)
	if refPrice <= 0 {
		refPrice = spm.anchorPrice()
	}
	return spm.applyFeeAwareSpread(spm.configuredProfitSpread(), refPrice, gridOrdersPostOnly)
}

// configuredProfitSpread 配置的利潤間距（不含費率下界）
func (spm *SuperPositionManager) configuredProfitSpread() float64 {
	if spm.config.Trading.ProfitSpread > 0 {
		return spm.config.Trading.ProfitSpread
	}
	return spm.config.Trading.PriceInterval
}

// anchorPrice 讀取價格錨點（無鎖，可在任意調用鏈上安全使用）
func (spm *SuperPositionManager) anchorPrice() float64 {
	return math.Float64frombits(spm.anchorPriceBits.Load())
}

// setAnchorPrice 寫入價格錨點。
// 讀-改-寫（如 ShiftGrid 的 +=）必須在 spm.mu 內完成，
// 本方法只保證單次寫入對其他 goroutine 立即可見。
func (spm *SuperPositionManager) setAnchorPrice(price float64) {
	spm.anchorPriceBits.Store(math.Float64bits(price))
	spm.markAdjustDirty()
}

// GetAnchorPrice 獲取價格锚点
func (spm *SuperPositionManager) GetAnchorPrice() float64 {
	return spm.anchorPrice()
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
		spm.markAdjustDirty()
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
		"anchor_price":     spm.anchorPrice(),
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

// GetUnrealizedPnLVerified returns both the local unrealized PnL and whether
// every open lot has verified cost and fee evidence at the supplied price.
func (spm *SuperPositionManager) GetUnrealizedPnLVerified(currentPrice float64) (float64, bool) {
	return spm.calculateUnrealizedPnLVerified(currentPrice)
}

// GetTotalPositionValueAtPrice 獲取在給定價格下的持倉總價值（供快照、API 等使用）
func (spm *SuperPositionManager) GetTotalPositionValueAtPrice(currentPrice float64) float64 {
	value, verified := spm.GetTotalPositionValueAtPriceVerified(currentPrice)
	if !verified {
		return math.MaxFloat64
	}
	return value
}

func (spm *SuperPositionManager) GetTotalPositionValueAtPriceVerified(currentPrice float64) (float64, bool) {
	return spm.calculateTotalPositionValueVerified(currentPrice)
}

// calculateUnrealizedPnL 计算未實現盈亏
func (spm *SuperPositionManager) calculateUnrealizedPnL(currentPrice float64) float64 {
	pnl, verified := spm.calculateUnrealizedPnLVerified(currentPrice)
	if !verified {
		return 0
	}
	return pnl
}

func (spm *SuperPositionManager) calculateUnrealizedPnLVerified(currentPrice float64) (float64, bool) {
	if math.IsNaN(currentPrice) || math.IsInf(currentPrice, 0) || currentPrice <= 0 {
		return 0, false
	}
	totalPnL := 0.0
	costBasisUnverified := false
	feeAsset := spm.feeQuoteAsset()
	spm.slots.Range(func(_, value interface{}) bool {
		slot := value.(*InventorySlot)
		slot.mu.RLock()
		if (slot.PositionStatus != PositionStatusFilled && slot.PositionStatus != PositionStatusEmpty) ||
			math.IsNaN(slot.PositionQty) || math.IsInf(slot.PositionQty, 0) || slot.PositionQty < 0 ||
			(slot.PositionStatus == PositionStatusEmpty && slot.PositionQty != 0) {
			costBasisUnverified = true
			slot.mu.RUnlock()
			return true
		}
		if slot.PositionStatus == PositionStatusFilled {
			qty := slot.PositionQty
			if qty == 0 {
				slot.mu.RUnlock()
				return true
			}
			isShortLeg := spm.isShort()
			if spm.isBoth() {
				switch slot.PositionLeg {
				case PositionLegLong:
					isShortLeg = false
				case PositionLegShort:
					isShortLeg = true
				default:
					costBasisUnverified = true
					slot.mu.RUnlock()
					return true
				}
			} else if slot.PositionLeg != PositionLegNone &&
				((isShortLeg && slot.PositionLeg != PositionLegShort) || (!isShortLeg && slot.PositionLeg != PositionLegLong)) {
				costBasisUnverified = true
				slot.mu.RUnlock()
				return true
			}
			if slot.CostBasisUnverified || !finiteGridValue(slot.AvgBuyPrice) || slot.AvgBuyPrice <= 0 ||
				slot.feeValuationUnknown || slot.pendingFeeSupplementCount != 0 ||
				!finiteGridValue(slot.BuyFee) || slot.BuyFee < 0 ||
				(slot.BuyFee > 0 && (feeAsset == "" || !strings.EqualFold(strings.TrimSpace(slot.FeeAsset), feeAsset))) {
				costBasisUnverified = true
				slot.mu.RUnlock()
				return true
			}
			// SHORT 模式下 AvgBuyPrice 儲存實際開空均價。
			entry := slot.AvgBuyPrice
			if isShortLeg {
				// 空頭盈虧 = (開倉價 - 當前價) * 數量
				totalPnL += (entry - currentPrice) * slot.PositionQty
			} else {
				// 多頭盈虧 = (當前價 - 開倉價) * 數量
				totalPnL += (currentPrice - entry) * slot.PositionQty
			}
			totalPnL -= slot.BuyFee
			if math.IsNaN(totalPnL) || math.IsInf(totalPnL, 0) {
				costBasisUnverified = true
			}
		}
		slot.mu.RUnlock()
		return true
	})
	if costBasisUnverified {
		return 0, false
	}
	return totalPnL, true
}

// calculateTotalPositionValue 计算當前持倉總價值
func (spm *SuperPositionManager) calculateTotalPositionValue(currentPrice float64) float64 {
	value, verified := spm.calculateTotalPositionValueVerified(currentPrice)
	if !verified {
		return math.MaxFloat64
	}
	return value
}

func (spm *SuperPositionManager) calculateTotalPositionValueVerified(currentPrice float64) (float64, bool) {
	totalValue := 0.0
	verified := finiteGridValue(currentPrice) && currentPrice > 0
	spm.slots.Range(func(key, value interface{}) bool {
		slot := value.(*InventorySlot)
		slot.mu.RLock()
		if slot.PositionStatus == PositionStatusFilled {
			if !finiteGridValue(slot.PositionQty) || slot.PositionQty < 0 {
				verified = false
			} else if slot.PositionQty > 0 && (!finiteGridValue(currentPrice) || currentPrice <= 0) {
				verified = false
			} else if slot.PositionQty > 0 {
				value := currentPrice * slot.PositionQty
				nextTotal := totalValue + value
				if !finiteGridValue(value) || !finiteGridValue(nextTotal) {
					verified = false
				} else {
					totalValue = nextTotal
				}
			}
		}
		slot.mu.RUnlock()
		return true
	})
	if !verified {
		return 0, false
	}
	return totalValue, true
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
	now := spm.now()
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

// interfaceSliceOf 把任意切片（含 []*T、[]T、[]interface{}）展開為 []interface{}；非切片返回 nil
func interfaceSliceOf(raw interface{}) []interface{} {
	if items, ok := raw.([]interface{}); ok {
		return items
	}
	rv := reflect.ValueOf(raw)
	if !rv.IsValid() || (rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array) {
		return nil
	}
	out := make([]interface{}, 0, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		item := rv.Index(i)
		if item.Kind() == reflect.Ptr && item.IsNil() {
			continue
		}
		out = append(out, item.Interface())
	}
	return out
}
