package kucoin

import "time"

// Side 交易方向
type Side string

const (
	SideBuy  Side = "BUY"
	SideSell Side = "SELL"
)

// OrderType 订單類型
type OrderType string

const (
	OrderTypeLimit  OrderType = "LIMIT"
	OrderTypeMarket OrderType = "MARKET"
)

// OrderStatus 订單状態
type OrderStatus string

const (
	OrderStatusNew             OrderStatus = "NEW"
	OrderStatusPartiallyFilled OrderStatus = "PARTIALLY_FILLED"
	OrderStatusFilled          OrderStatus = "FILLED"
	OrderStatusCanceled        OrderStatus = "CANCELED"
	OrderStatusRejected        OrderStatus = "REJECTED"
	OrderStatusExpired         OrderStatus = "EXPIRED"
)

// TimeInForce 订單有效期
type TimeInForce string

const (
	TimeInForceGTC TimeInForce = "GTC" // Good Till Cancel
	TimeInForceIOC TimeInForce = "IOC" // Immediate or Cancel
	TimeInForceFOK TimeInForce = "FOK" // Fill or Kill
	TimeInForceGTX TimeInForce = "GTX" // Good Till Crossing (Post Only)
)

// KuCoinOrderRequest 下單请求（通用）
type KuCoinOrderRequest struct {
	Symbol        string
	Side          Side
	Type          OrderType
	TimeInForce   TimeInForce
	Quantity      float64
	Price         float64 // 市價單可為0
	ReduceOnly    bool    // 是否只减倉
	PostOnly      bool    // 是否只做 Maker（Post Only)
	PriceDecimals int     // 價格精度（用於格式化）
	ClientOrderID string  // 自定义订單ID
	Leverage      float64 // 杠杆倍數
	Timestamp     int64   // 時间戳
}

// Order 订單信息（通用）
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
	Timestamp     int64
}

// Position 持倉資訊（通用）
type Position struct {
	Symbol           string
	Side             string  // "LONG" or "SHORT"
	Size             float64 // 持倉數量（正數）
	EntryPrice       float64
	MarkPrice        float64
	UnrealizedPnL    float64
	Leverage         float64
	LiquidationPrice float64
	MarginType       string
	IsolatedMargin   float64
}

// Account 帳戶資訊（通用）
type Account struct {
	TotalBalance     float64
	AvailableBalance float64
	UnrealizedPnL    float64
	MarginBalance    float64
	Positions        []*Position
	AccountLeverage  int // 账戶级别的杠杆倍數（部分交易所支援）
}

// OrderUpdate WebSocket 订單更新事件（通用）
type OrderUpdate struct {
	OrderID         string
	ClientOrderID   string
	Symbol          string
	Side            string
	Type            string
	Status          string
	Price           float64
	Quantity        float64
	ExecutedQty     float64
	AvgPrice        float64
	UpdateTime      int64
	Commission      float64 // 本次成交手續費
	CommissionAsset string  // 手續費幣種
}

// OrderUpdateCallback 订單更新回呼函數
type OrderUpdateCallback func(update OrderUpdate)

// KuCoinCandle K線數據
type KuCoinCandle struct {
	Symbol    string
	Open      float64
	High      float64
	Low       float64
	Close     float64
	Volume    float64
	OpenTime  int64
	CloseTime int64
	IsClosed  bool // K線是否完結
}

// CandleUpdateCallback K線更新回呼函數
type CandleUpdateCallback func(candle interface{})
