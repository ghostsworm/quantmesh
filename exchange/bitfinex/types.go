package bitfinex

import "time"

// 交易方向
type Side string

const (
	SideBuy  Side = "BUY"
	SideSell Side = "SELL"
)

// 订單類型
type OrderType string

const (
	OrderTypeLimit  OrderType = "LIMIT"
	OrderTypeMarket OrderType = "MARKET"
)

// 订單状態
type OrderStatus string

const (
	OrderStatusNew             OrderStatus = "NEW"
	OrderStatusPartiallyFilled OrderStatus = "PARTIALLY_FILLED"
	OrderStatusFilled          OrderStatus = "FILLED"
	OrderStatusCanceled        OrderStatus = "CANCELED"
	OrderStatusRejected        OrderStatus = "REJECTED"
)

// TimeInForce 订單有效期
type TimeInForce string

const (
	TimeInForceGTC TimeInForce = "GTC" // Good Till Cancel
	TimeInForceIOC TimeInForce = "IOC" // Immediate or Cancel
	TimeInForceFOK TimeInForce = "FOK" // Fill or Kill
)

// BitfinexOrderRequest 下單请求
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

// Order 订單信息
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

// Position 持倉資訊
type Position struct {
	Symbol        string
	Side          string // LONG/SHORT
	Size          float64
	EntryPrice    float64
	MarkPrice     float64
	UnrealizedPnL float64
	Leverage      float64
}

// Account 帳戶資訊
type Account struct {
	TotalBalance     float64
	AvailableBalance float64
	UnrealizedPnL    float64
	MarginBalance    float64
}

// BitfinexCandle K線數據
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

// CandleUpdateCallback K線更新回呼
type CandleUpdateCallback func(candle interface{})
