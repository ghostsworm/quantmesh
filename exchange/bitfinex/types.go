package bitfinex

import "time"

// 交易方向
type Side string

const (
	SideBuy  Side = "BUY"
	SideSell Side = "SELL"
)

// 订单类型
type OrderType string

const (
	OrderTypeLimit  OrderType = "LIMIT"
	OrderTypeMarket OrderType = "MARKET"
)

// 订单状态
type OrderStatus string

const (
	OrderStatusNew             OrderStatus = "NEW"
	OrderStatusPartiallyFilled OrderStatus = "PARTIALLY_FILLED"
	OrderStatusFilled          OrderStatus = "FILLED"
	OrderStatusCanceled        OrderStatus = "CANCELED"
	OrderStatusRejected        OrderStatus = "REJECTED"
)

// TimeInForce 订单有效期
type TimeInForce string

const (
	TimeInForceGTC TimeInForce = "GTC" // Good Till Cancel
	TimeInForceIOC TimeInForce = "IOC" // Immediate or Cancel
	TimeInForceFOK TimeInForce = "FOK" // Fill or Kill
)

// BitfinexOrderRequest 下单请求
type BitfinexOrderRequest struct {
	Symbol        string
	Side          Side
	Type          OrderType
	TimeInForce   TimeInForce
	Quantity      float64
	Price         float64
	ReduceOnly    bool
	PostOnly      bool
	ClientOrderID string
	Timestamp     int64
}

// Order 订单信息
type Order struct {
	OrderID       string
	ClientOrderID string
	Symbol        string
	Side          string
	Type          string
	Price         float64
	Quantity      float64
	ExecutedQty   float64
	AvgPrice      float64
	Status        string
	CreatedAt     time.Time
	UpdateTime    int64
}

// Position 持仓信息
type Position struct {
	Symbol        string
	Side          string // LONG/SHORT
	Size          float64
	EntryPrice    float64
	MarkPrice     float64
	UnrealizedPnL float64
	Leverage      float64
}

// Account 账户信息
type Account struct {
	TotalBalance     float64
	AvailableBalance float64
	UnrealizedPnL    float64
	MarginBalance    float64
}

// BitfinexCandle K线数据
type BitfinexCandle struct {
	Symbol    string
	Open      float64
	High      float64
	Low       float64
	Close     float64
	Volume    float64
	OpenTime  int64
	CloseTime int64
	IsClosed  bool
}

// CandleUpdateCallback K线更新回调
type CandleUpdateCallback func(candle interface{})
