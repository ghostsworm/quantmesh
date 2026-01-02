package ascendex

// 本地类型定义，避免导入 exchange 包造成循环依赖

// OrderSide 订单方向
type OrderSide string

const (
	SideBuy  OrderSide = "buy"
	SideSell OrderSide = "sell"
)

// OrderStatus 订单状态
type OrderStatus string

const (
	OrderStatusNew             OrderStatus = "New"
	OrderStatusPartiallyFilled OrderStatus = "PartiallyFilled"
	OrderStatusFilled          OrderStatus = "Filled"
	OrderStatusCanceled        OrderStatus = "Canceled"
)

// OrderLocal 订单信息
type OrderLocal struct {
	OrderID     string
	Symbol      string
	Side        OrderSide
	Price       float64
	Quantity    float64
	ExecutedQty float64
	Status      OrderStatus
	UpdateTime  int64
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

// AscendEXInterval K线周期
type AscendEXInterval string

const (
	AscendEXInterval1m  AscendEXInterval = "1"
	AscendEXInterval5m  AscendEXInterval = "5"
	AscendEXInterval15m AscendEXInterval = "15"
	AscendEXInterval30m AscendEXInterval = "30"
	AscendEXInterval1h  AscendEXInterval = "60"
	AscendEXInterval4h  AscendEXInterval = "240"
	AscendEXInterval1d  AscendEXInterval = "1d"
)

// ConvertInterval 转换 K线周期
func ConvertInterval(interval string) AscendEXInterval {
	switch interval {
	case "1m":
		return AscendEXInterval1m
	case "5m":
		return AscendEXInterval5m
	case "15m":
		return AscendEXInterval15m
	case "30m":
		return AscendEXInterval30m
	case "1h":
		return AscendEXInterval1h
	case "4h":
		return AscendEXInterval4h
	case "1d":
		return AscendEXInterval1d
	default:
		return AscendEXInterval1m
	}
}
