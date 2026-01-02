package xtcom

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
	OrderID       string
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

// XTInterval K线周期
type XTInterval string

const (
	XTInterval1m  XTInterval = "1m"
	XTInterval5m  XTInterval = "5m"
	XTInterval15m XTInterval = "15m"
	XTInterval30m XTInterval = "30m"
	XTInterval1h  XTInterval = "1h"
	XTInterval4h  XTInterval = "4h"
	XTInterval1d  XTInterval = "1d"
)

// ConvertInterval 转换 K线周期
func ConvertInterval(interval string) XTInterval {
	switch interval {
	case "1m":
		return XTInterval1m
	case "5m":
		return XTInterval5m
	case "15m":
		return XTInterval15m
	case "30m":
		return XTInterval30m
	case "1h":
		return XTInterval1h
	case "4h":
		return XTInterval4h
	case "1d":
		return XTInterval1d
	default:
		return XTInterval1m
	}
}
