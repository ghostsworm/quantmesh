package bingx

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

// BingXInterval K线周期
type BingXInterval string

const (
	BingXInterval1m  BingXInterval = "1m"
	BingXInterval5m  BingXInterval = "5m"
	BingXInterval15m BingXInterval = "15m"
	BingXInterval30m BingXInterval = "30m"
	BingXInterval1h  BingXInterval = "1h"
	BingXInterval4h  BingXInterval = "4h"
	BingXInterval1d  BingXInterval = "1d"
	BingXInterval1w  BingXInterval = "1w"
)

// ConvertInterval 转换 K线周期
func ConvertInterval(interval string) BingXInterval {
	switch interval {
	case "1m":
		return BingXInterval1m
	case "5m":
		return BingXInterval5m
	case "15m":
		return BingXInterval15m
	case "30m":
		return BingXInterval30m
	case "1h":
		return BingXInterval1h
	case "4h":
		return BingXInterval4h
	case "1d":
		return BingXInterval1d
	case "1w":
		return BingXInterval1w
	default:
		return BingXInterval1m
	}
}
