package woox

// 本地类型定义，避免导入 exchange 包造成循环依赖

// OrderSide 订单方向
type OrderSide string

const (
	SideBuy  OrderSide = "BUY"
	SideSell OrderSide = "SELL"
)

// OrderStatus 订单状态
type OrderStatus string

const (
	OrderStatusNew             OrderStatus = "NEW"
	OrderStatusPartiallyFilled OrderStatus = "PARTIALLY_FILLED"
	OrderStatusFilled          OrderStatus = "FILLED"
	OrderStatusCanceled        OrderStatus = "CANCELED"
)

// OrderLocal 订单信息
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

// AccountLocal 账户信息
type AccountLocal struct {
	TotalWalletBalance float64
	TotalMarginBalance float64
	AvailableBalance   float64
}

// PositionLocal 持仓信息
type PositionLocal struct {
	Symbol        string
	Size          float64
	EntryPrice    float64
	MarkPrice     float64
	UnrealizedPNL float64
	Leverage      int
}

// CandleLocal K线数据
type CandleLocal struct {
	Symbol    string
	Open      float64
	High      float64
	Low       float64
	Close     float64
	Volume    float64
	Timestamp int64
}

// CandleUpdateCallbackLocal K线更新回调
type CandleUpdateCallbackLocal func(candle *CandleLocal)

// WOOXInterval K线周期
type WOOXInterval string

const (
	WOOXInterval1m  WOOXInterval = "1m"
	WOOXInterval5m  WOOXInterval = "5m"
	WOOXInterval15m WOOXInterval = "15m"
	WOOXInterval30m WOOXInterval = "30m"
	WOOXInterval1h  WOOXInterval = "1h"
	WOOXInterval4h  WOOXInterval = "4h"
	WOOXInterval1d  WOOXInterval = "1d"
)

// ConvertInterval 转换 K线周期
func ConvertInterval(interval string) WOOXInterval {
	switch interval {
	case "1m":
		return WOOXInterval1m
	case "5m":
		return WOOXInterval5m
	case "15m":
		return WOOXInterval15m
	case "30m":
		return WOOXInterval30m
	case "1h":
		return WOOXInterval1h
	case "4h":
		return WOOXInterval4h
	case "1d":
		return WOOXInterval1d
	default:
		return WOOXInterval1m
	}
}
