package gate

import "time"

// 為了避免循環匯入，在这里定义需要的接口和類型
// 这些類型应該與 exchange/types.go 中的定义保持一致

type Side string
type OrderType string
type OrderStatus string
type TimeInForce string

const (
	SideBuy  Side = "BUY"
	SideSell Side = "SELL"
)

const (
	OrderTypeLimit OrderType = "LIMIT"
)

const (
	OrderStatusNew OrderStatus = "NEW"
)

const (
	TimeInForceGTC TimeInForce = "GTC"
)

type OrderRequest struct {
	Symbol        string
	Side          Side
	Type          OrderType
	TimeInForce   TimeInForce
	Quantity      float64
	Price         float64
	ReduceOnly    bool
	PostOnly      bool // 是否只做 Maker（Post Only）
	PriceDecimals int
	ClientOrderID string // 自定义订單ID
}

type Order struct {
	OrderID       int64
	ClientOrderID string
	Symbol        string
	Side          Side
	Type          OrderType
	Price         float64
	Quantity      float64
	ExecutedQty   float64
	AvgPrice      float64
	Status        OrderStatus
	CreatedAt     time.Time
	UpdateTime    int64
}

type Position struct {
	Symbol         string
	Size           float64
	EntryPrice     float64
	MarkPrice      float64
	UnrealizedPNL  float64
	Leverage       int
	MarginType     string
	IsolatedMargin float64
}

type Account struct {
	TotalWalletBalance float64
	TotalMarginBalance float64
	AvailableBalance   float64
	Positions          []*Position
	PosMode            string // "dual_long_short" or "single"
	AccountLeverage    int    // 账戶级别的杠杆倍數
}

type OrderUpdate struct {
	OrderID       int64
	ClientOrderID string
	Symbol        string
	Side          Side
	Type          OrderType
	Status        OrderStatus
	Price         float64
	Quantity      float64
	ExecutedQty   float64
	AvgPrice      float64
	UpdateTime    int64
}

// Candle K線數據
type Candle struct {
	Symbol    string
	Open      float64
	High      float64
	Low       float64
	Close     float64
	Volume    float64
	Timestamp int64
	IsClosed  bool // K線是否完結
}

// OrderBookLevel 订單簿檔位（本地類型，避免循環匯入）
type OrderBookLevel struct {
	Price    float64
	Quantity float64
}

// OrderBook 订單簿（本地類型，避免循環匯入）
type OrderBook struct {
	Symbol    string
	Bids      []OrderBookLevel
	Asks      []OrderBookLevel
	Timestamp int64
}

// ============ Gate.io API 专用結構体 ============

// GateResponse Gate.io API 通用响应結構
type GateResponse struct {
	// Gate.io API 在錯误時回傳 label 和 message
	Label   string `json:"label,omitempty"`
	Message string `json:"message,omitempty"`
}

// ContractInfo 合約信息
type ContractInfo struct {
	Name              string  `json:"name"`                // 合約名称，如 BTC_USDT
	Type              string  `json:"type"`                // 合約類型 inverse/direct
	QuantoMultiplier  string  `json:"quanto_multiplier"`   // 合約乘數
	OrderPriceRound   string  `json:"order_price_round"`   // 價格精度
	OrderSizeMin      float64 `json:"order_size_min"`      // 最小下單數量
	OrderSizeMax      float64 `json:"order_size_max"`      // 最大下單數量
	OrderSizeRound    string  `json:"order_size_round"`    // 數量精度
	OrderPriceDeviate string  `json:"order_price_deviate"` // 價格偏离百分比
	RefDiscountRate   string  `json:"ref_discount_rate"`   // 推荐返佣率
	OrderbookID       int64   `json:"orderbook_id"`        // 订單簿ID
	TradeSize         float64 `json:"trade_size"`          // 最小交易张數
	MarkPriceRound    string  `json:"mark_price_round"`    // 標記價格精度
}

// FuturesAccount Gate.io 合約帳戶資訊
type FuturesAccount struct {
	User                  int64  `json:"user"`                    // 用戶ID
	Currency              string `json:"currency"`                // 币种
	Total                 string `json:"total"`                   // 總资產
	UnrealisedPnl         string `json:"unrealised_pnl"`          // 未實現盈亏
	PositionMargin        string `json:"position_margin"`         // 持倉保证金
	OrderMargin           string `json:"order_margin"`            // 挂單保证金
	Available             string `json:"available"`               // 可用餘額
	Point                 string `json:"point"`                   // 点卡餘額
	Bonus                 string `json:"bonus"`                   // 体驗金
	InDualMode            bool   `json:"in_dual_mode"`            // 是否双向持倉模式
	EnableCredit          bool   `json:"enable_credit"`           // 是否啟用统一账戶
	PositionInitialMargin string `json:"position_initial_margin"` // 持倉初始保证金
	MaintenanceMargin     string `json:"maintenance_margin"`      // 维持保证金
}

// FuturesPosition Gate.io 合約持倉
type FuturesPosition struct {
	User            int64  `json:"user"`             // 用戶ID
	Contract        string `json:"contract"`         // 合約名称
	Size            int64  `json:"size"`             // 持倉數量（正數做多，负數做空）
	Leverage        string `json:"leverage"`         // 杠杆倍數
	RiskLimit       string `json:"risk_limit"`       // 风險限額
	LeverageMax     string `json:"leverage_max"`     // 最大杠杆
	MaintenanceRate string `json:"maintenance_rate"` // 维持保证金比例
	Value           string `json:"value"`            // 持倉價值
	Margin          string `json:"margin"`           // 保证金
	EntryPrice      string `json:"entry_price"`      // 开倉均價
	LiqPrice        string `json:"liq_price"`        // 强平價格
	MarkPrice       string `json:"mark_price"`       // 標記價格
	UnrealisedPnl   string `json:"unrealised_pnl"`   // 未實現盈亏
	RealisedPnl     string `json:"realised_pnl"`     // 已實現盈亏
	HistoryPnl      string `json:"history_pnl"`      // 历史總盈亏
	LastClosePnl    string `json:"last_close_pnl"`   // 上次平倉盈亏
	RealisedPoint   string `json:"realised_point"`   // 已實現点卡收益
	HistoryPoint    string `json:"history_point"`    // 历史總点卡收益
	AdlRanking      int    `json:"adl_ranking"`      // ADL排名
	PendingOrders   int    `json:"pending_orders"`   // 挂單數量
	CloseOrder      *struct {
		ID    int64  `json:"id"`
		Price string `json:"price"`
		IsLiq bool   `json:"is_liq"`
	} `json:"close_order"` // 平倉單
	Mode               string `json:"mode"`                 // dual_long, dual_short, single
	CrossLeverageLimit string `json:"cross_leverage_limit"` // 全倉杠杆上限
}

// FuturesOrder Gate.io 合約订單
type FuturesOrder struct {
	ID            int64   `json:"id"`             // 订單ID
	User          int64   `json:"user"`           // 用戶ID
	Contract      string  `json:"contract"`       // 合約名称
	CreateTime    float64 `json:"create_time"`    // 創建時间（秒级時间戳）
	FinishTime    float64 `json:"finish_time"`    // 完成時间
	FinishAs      string  `json:"finish_as"`      // 完成類型 filled/cancelled/liquidated/ioc/auto_deleveraged/reduce_only/position_closed
	Status        string  `json:"status"`         // 订單状態 open/finished
	Size          int64   `json:"size"`           // 订單數量（正數買入，负數賣出）
	Price         string  `json:"price"`          // 委托價格（0表示市價）
	FillPrice     string  `json:"fill_price"`     // 成交均價
	Left          int64   `json:"left"`           // 未成交數量
	Text          string  `json:"text"`           // 用戶自定义信息
	Tif           string  `json:"tif"`            // Time in force: gtc/ioc/poc
	IsLiq         bool    `json:"is_liq"`         // 是否强平單
	IsClose       bool    `json:"is_close"`       // 是否平倉單
	IsReduceOnly  bool    `json:"is_reduce_only"` // 是否只减倉
	IsPostOnly    bool    `json:"is_post_only"`   // 是否只做maker
	Iceberg       int64   `json:"iceberg"`        // 冰山委托显示數量
	AutoSize      string  `json:"auto_size"`      // 自动减倉策略
	RefundedFee   string  `json:"refunded_fee"`   // 返还手续费
	Fee           string  `json:"fee"`            // 手续费
	FillSize      int64   `json:"fill_size"`      // 已成交數量
	RealisedPnl   string  `json:"realised_pnl"`   // 已實現盈亏
	RealisedPoint string  `json:"realised_point"` // 已實現点卡收益
}

// WSRequest WebSocket 请求結構
type WSRequest struct {
	Time    int64                  `json:"time"`
	Channel string                 `json:"channel"`
	Event   string                 `json:"event"`
	Payload map[string]interface{} `json:"payload,omitempty"`
}

// WSOrderPayload WebSocket 下單 Payload
type WSOrderPayload struct {
	ReqHeader map[string]string      `json:"req_header"` // 必須包含 X-Gate-Channel-Id
	ReqID     string                 `json:"req_id"`
	ReqParam  map[string]interface{} `json:"req_param"`
}

// WSResponse WebSocket 响应結構
type WSResponse struct {
	Time    int64                  `json:"time"`
	Channel string                 `json:"channel"`
	Event   string                 `json:"event"`
	Error   *WSError               `json:"error,omitempty"`
	Result  map[string]interface{} `json:"result,omitempty"`
}

// WSError WebSocket 錯误
type WSError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
