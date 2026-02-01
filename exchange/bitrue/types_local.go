package bitrue

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

// BitrueInterval K線週期
type BitrueInterval string

const (
	BitrueInterval1m  BitrueInterval = "1m"
	BitrueInterval5m  BitrueInterval = "5m"
	BitrueInterval15m BitrueInterval = "15m"
	BitrueInterval30m BitrueInterval = "30m"
	BitrueInterval1h  BitrueInterval = "1h"
	BitrueInterval4h  BitrueInterval = "4h"
	BitrueInterval1d  BitrueInterval = "1d"
)

// ConvertInterval 轉换 K線週期
func ConvertInterval(interval string) BitrueInterval {
	switch interval {
	case "1m":
		return BitrueInterval1m
	case "5m":
		return BitrueInterval5m
	case "15m":
		return BitrueInterval15m
	case "30m":
		return BitrueInterval30m
	case "1h":
		return BitrueInterval1h
	case "4h":
		return BitrueInterval4h
	case "1d":
		return BitrueInterval1d
	default:
		return BitrueInterval1m
	}
}
