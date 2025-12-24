package gate

import "time"

// 为了避免循环导入，在这里定义需要的接口和类型
// 这些类型应该与 exchange/types.go 中的定义保持一致

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
	ClientOrderID string // 自定义订单ID
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
	AccountLeverage    int    // 账户级别的杠杆倍数
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

// Candle K线数据
type Candle struct {
	Symbol    string
	Open      float64
	High      float64
	Low       float64
	Close     float64
	Volume    float64
	Timestamp int64
	IsClosed  bool // K线是否完结
}

// ============ Gate.io API 专用结构体 ============

// GateResponse Gate.io API 通用响应结构
type GateResponse struct {
	// Gate.io API 在错误时返回 label 和 message
	Label   string `json:"label,omitempty"`
	Message string `json:"message,omitempty"`
}

// ContractInfo 合约信息
type ContractInfo struct {
	Name              string  `json:"name"`                // 合约名称，如 BTC_USDT
	Type              string  `json:"type"`                // 合约类型 inverse/direct
	QuantoMultiplier  string  `json:"quanto_multiplier"`   // 合约乘数
	OrderPriceRound   string  `json:"order_price_round"`   // 价格精度
	OrderSizeMin      float64 `json:"order_size_min"`      // 最小下单数量
	OrderSizeMax      float64 `json:"order_size_max"`      // 最大下单数量
	OrderSizeRound    string  `json:"order_size_round"`    // 数量精度
	OrderPriceDeviate string  `json:"order_price_deviate"` // 价格偏离百分比
	RefDiscountRate   string  `json:"ref_discount_rate"`   // 推荐返佣率
	OrderbookID       int64   `json:"orderbook_id"`        // 订单簿ID
	TradeSize         float64 `json:"trade_size"`          // 最小交易张数
	MarkPriceRound    string  `json:"mark_price_round"`    // 标记价格精度
}

// FuturesAccount Gate.io 合约账户信息
type FuturesAccount struct {
	User                  int64  `json:"user"`                    // 用户ID
	Currency              string `json:"currency"`                // 币种
	Total                 string `json:"total"`                   // 总资产
	UnrealisedPnl         string `json:"unrealised_pnl"`          // 未实现盈亏
	PositionMargin        string `json:"position_margin"`         // 持仓保证金
	OrderMargin           string `json:"order_margin"`            // 挂单保证金
	Available             string `json:"available"`               // 可用余额
	Point                 string `json:"point"`                   // 点卡余额
	Bonus                 string `json:"bonus"`                   // 体验金
	InDualMode            bool   `json:"in_dual_mode"`            // 是否双向持仓模式
	EnableCredit          bool   `json:"enable_credit"`           // 是否启用统一账户
	PositionInitialMargin string `json:"position_initial_margin"` // 持仓初始保证金
	MaintenanceMargin     string `json:"maintenance_margin"`      // 维持保证金
}

// FuturesPosition Gate.io 合约持仓
type FuturesPosition struct {
	User            int64  `json:"user"`             // 用户ID
	Contract        string `json:"contract"`         // 合约名称
	Size            int64  `json:"size"`             // 持仓数量（正数做多，负数做空）
	Leverage        string `json:"leverage"`         // 杠杆倍数
	RiskLimit       string `json:"risk_limit"`       // 风险限额
	LeverageMax     string `json:"leverage_max"`     // 最大杠杆
	MaintenanceRate string `json:"maintenance_rate"` // 维持保证金比例
	Value           string `json:"value"`            // 持仓价值
	Margin          string `json:"margin"`           // 保证金
	EntryPrice      string `json:"entry_price"`      // 开仓均价
	LiqPrice        string `json:"liq_price"`        // 强平价格
	MarkPrice       string `json:"mark_price"`       // 标记价格
	UnrealisedPnl   string `json:"unrealised_pnl"`   // 未实现盈亏
	RealisedPnl     string `json:"realised_pnl"`     // 已实现盈亏
	HistoryPnl      string `json:"history_pnl"`      // 历史总盈亏
	LastClosePnl    string `json:"last_close_pnl"`   // 上次平仓盈亏
	RealisedPoint   string `json:"realised_point"`   // 已实现点卡收益
	HistoryPoint    string `json:"history_point"`    // 历史总点卡收益
	AdlRanking      int    `json:"adl_ranking"`      // ADL排名
	PendingOrders   int    `json:"pending_orders"`   // 挂单数量
	CloseOrder      *struct {
		ID    int64  `json:"id"`
		Price string `json:"price"`
		IsLiq bool   `json:"is_liq"`
	} `json:"close_order"` // 平仓单
	Mode               string `json:"mode"`                 // dual_long, dual_short, single
	CrossLeverageLimit string `json:"cross_leverage_limit"` // 全仓杠杆上限
}

// FuturesOrder Gate.io 合约订单
type FuturesOrder struct {
	ID            int64   `json:"id"`             // 订单ID
	User          int64   `json:"user"`           // 用户ID
	Contract      string  `json:"contract"`       // 合约名称
	CreateTime    float64 `json:"create_time"`    // 创建时间（秒级时间戳）
	FinishTime    float64 `json:"finish_time"`    // 完成时间
	FinishAs      string  `json:"finish_as"`      // 完成类型 filled/cancelled/liquidated/ioc/auto_deleveraged/reduce_only/position_closed
	Status        string  `json:"status"`         // 订单状态 open/finished
	Size          int64   `json:"size"`           // 订单数量（正数买入，负数卖出）
	Price         string  `json:"price"`          // 委托价格（0表示市价）
	FillPrice     string  `json:"fill_price"`     // 成交均价
	Left          int64   `json:"left"`           // 未成交数量
	Text          string  `json:"text"`           // 用户自定义信息
	Tif           string  `json:"tif"`            // Time in force: gtc/ioc/poc
	IsLiq         bool    `json:"is_liq"`         // 是否强平单
	IsClose       bool    `json:"is_close"`       // 是否平仓单
	IsReduceOnly  bool    `json:"is_reduce_only"` // 是否只减仓
	IsPostOnly    bool    `json:"is_post_only"`   // 是否只做maker
	Iceberg       int64   `json:"iceberg"`        // 冰山委托显示数量
	AutoSize      string  `json:"auto_size"`      // 自动减仓策略
	RefundedFee   string  `json:"refunded_fee"`   // 返还手续费
	Fee           string  `json:"fee"`            // 手续费
	FillSize      int64   `json:"fill_size"`      // 已成交数量
	RealisedPnl   string  `json:"realised_pnl"`   // 已实现盈亏
	RealisedPoint string  `json:"realised_point"` // 已实现点卡收益
}

// WSRequest WebSocket 请求结构
type WSRequest struct {
	Time    int64                  `json:"time"`
	Channel string                 `json:"channel"`
	Event   string                 `json:"event"`
	Payload map[string]interface{} `json:"payload,omitempty"`
}

// WSOrderPayload WebSocket 下单 Payload
type WSOrderPayload struct {
	ReqHeader map[string]string      `json:"req_header"` // 必须包含 X-Gate-Channel-Id
	ReqID     string                 `json:"req_id"`
	ReqParam  map[string]interface{} `json:"req_param"`
}

// WSResponse WebSocket 响应结构
type WSResponse struct {
	Time    int64                  `json:"time"`
	Channel string                 `json:"channel"`
	Event   string                 `json:"event"`
	Error   *WSError               `json:"error,omitempty"`
	Result  map[string]interface{} `json:"result,omitempty"`
}

// WSError WebSocket 错误
type WSError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
