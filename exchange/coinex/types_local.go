package coinex

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

// CoinExPeriod K线周期
type CoinExPeriod string

const (
	CoinExPeriod1min  CoinExPeriod = "1min"
	CoinExPeriod5min  CoinExPeriod = "5min"
	CoinExPeriod15min CoinExPeriod = "15min"
	CoinExPeriod30min CoinExPeriod = "30min"
	CoinExPeriod1hour CoinExPeriod = "1hour"
	CoinExPeriod4hour CoinExPeriod = "4hour"
	CoinExPeriod1day  CoinExPeriod = "1day"
)

// ConvertInterval 转换 K线周期
func ConvertInterval(interval string) CoinExPeriod {
	switch interval {
	case "1m":
		return CoinExPeriod1min
	case "5m":
		return CoinExPeriod5min
	case "15m":
		return CoinExPeriod15min
	case "30m":
		return CoinExPeriod30min
	case "1h":
		return CoinExPeriod1hour
	case "4h":
		return CoinExPeriod4hour
	case "1d":
		return CoinExPeriod1day
	default:
		return CoinExPeriod1min
	}
}
