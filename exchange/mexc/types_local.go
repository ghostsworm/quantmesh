package mexc

// 本地類型定义，避免導入 exchange 包造成循环依赖

// OrderSide 订單方向
type OrderSide string

const (
	SideBuy  OrderSide = "BUY"
	SideSell OrderSide = "SELL"
)

// OrderStatus 订單状態
type OrderStatus string

const (
	OrderStatusNew             OrderStatus = "NEW"
	OrderStatusPartiallyFilled OrderStatus = "PARTIALLY_FILLED"
	OrderStatusFilled          OrderStatus = "FILLED"
	OrderStatusCanceled        OrderStatus = "CANCELED"
)

// OrderRequest 下單请求
type OrderRequestLocal struct {
	Symbol        string
	Side          OrderSide
	Quantity      float64
	Price         float64
	ClientOrderID string
}

// Order 订單信息
type OrderLocal struct {
	OrderID       int64
	ClientOrderID string
	Symbol        string
	Side          OrderSide
	Price         float64
	Quantity      float64
	ExecutedQty   float64
	Status        OrderStatus
	UpdateTime    int64
}

// Account 帳戶資訊
type AccountLocal struct {
	TotalWalletBalance float64
	TotalMarginBalance float64
	AvailableBalance   float64
}

// Position 持倉資訊
type PositionLocal struct {
	Symbol        string
	Size          float64
	EntryPrice    float64
	MarkPrice     float64
	UnrealizedPNL float64
	Leverage      int
}

// Candle K線數據
type CandleLocal struct {
	Symbol    string
	Open      float64
	High      float64
	Low       float64
	Close     float64
	Volume    float64
	Timestamp int64
}

// CandleUpdateCallback K線更新回呼
type CandleUpdateCallbackLocal func(candle *CandleLocal)
