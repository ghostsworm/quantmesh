package btcc

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

// OrderLocal 订單信息
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

// AccountLocal 帳戶資訊
type AccountLocal struct {
	TotalWalletBalance float64
	TotalMarginBalance float64
	AvailableBalance   float64
}

// PositionLocal 持倉資訊
type PositionLocal struct {
	Symbol        string
	Side          string
	Size          float64
	EntryPrice    float64
	MarkPrice     float64
	UnrealizedPNL float64
	Leverage      int
}

// CandleLocal K線數據
type CandleLocal struct {
	Symbol    string
	Open      float64
	High      float64
	Low       float64
	Close     float64
	Volume    float64
	Timestamp int64
}

// CandleUpdateCallbackLocal K線更新回呼
type CandleUpdateCallbackLocal func(candle *CandleLocal)

// BTCCInterval K線週期
type BTCCInterval string

const (
	BTCCInterval1m  BTCCInterval = "1m"
	BTCCInterval5m  BTCCInterval = "5m"
	BTCCInterval15m BTCCInterval = "15m"
	BTCCInterval30m BTCCInterval = "30m"
	BTCCInterval1h  BTCCInterval = "1h"
	BTCCInterval4h  BTCCInterval = "4h"
	BTCCInterval1d  BTCCInterval = "1d"
)

// ConvertInterval 轉换 K線週期
func ConvertInterval(interval string) BTCCInterval {
	switch interval {
	case "1m":
		return BTCCInterval1m
	case "5m":
		return BTCCInterval5m
	case "15m":
		return BTCCInterval15m
	case "30m":
		return BTCCInterval30m
	case "1h":
		return BTCCInterval1h
	case "4h":
		return BTCCInterval4h
	case "1d":
		return BTCCInterval1d
	default:
		return BTCCInterval1m
	}
}
