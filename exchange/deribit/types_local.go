package deribit

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

// OrderBookLevelLocal 訂單簿檔位
type OrderBookLevelLocal struct {
	Price    float64
	Quantity float64
}

// OrderBookLocal 訂單簿（供 exchange wrapper 轉換）
type OrderBookLocal struct {
	Symbol    string
	Bids      []OrderBookLevelLocal
	Asks      []OrderBookLevelLocal
	Timestamp int64
}

// DeribitResolution K線週期
type DeribitResolution string

const (
	DeribitResolution1   DeribitResolution = "1"   // 1 分钟
	DeribitResolution3   DeribitResolution = "3"   // 3 分钟
	DeribitResolution5   DeribitResolution = "5"   // 5 分钟
	DeribitResolution15  DeribitResolution = "15"  // 15 分钟
	DeribitResolution30  DeribitResolution = "30"  // 30 分钟
	DeribitResolution60  DeribitResolution = "60"  // 1 小時
	DeribitResolution120 DeribitResolution = "120" // 2 小時
	DeribitResolution180 DeribitResolution = "180" // 3 小時
	DeribitResolution360 DeribitResolution = "360" // 6 小時
	DeribitResolution720 DeribitResolution = "720" // 12 小時
	DeribitResolution1D  DeribitResolution = "1D"  // 1 天
)

// ConvertInterval 轉换 K線週期
func ConvertInterval(interval string) DeribitResolution {
	switch interval {
	case "1m":
		return DeribitResolution1
	case "3m":
		return DeribitResolution3
	case "5m":
		return DeribitResolution5
	case "15m":
		return DeribitResolution15
	case "30m":
		return DeribitResolution30
	case "1h":
		return DeribitResolution60
	case "2h":
		return DeribitResolution120
	case "3h":
		return DeribitResolution180
	case "6h":
		return DeribitResolution360
	case "12h":
		return DeribitResolution720
	case "1d":
		return DeribitResolution1D
	default:
		return DeribitResolution1
	}
}
