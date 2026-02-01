package cryptocom

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
	OrderStatusActive   OrderStatus = "ACTIVE"
	OrderStatusFilled   OrderStatus = "FILLED"
	OrderStatusCanceled OrderStatus = "CANCELED"
	OrderStatusRejected OrderStatus = "REJECTED"
	OrderStatusExpired  OrderStatus = "EXPIRED"
)

// OrderLocal 订單信息
type OrderLocal struct {
	OrderID        int64
	ClientOID      string
	InstrumentName string
	Side           OrderSide
	Price          float64
	Quantity       float64
	ExecutedQty    float64
	Status         OrderStatus
	UpdateTime     int64
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

// CryptoComTimeframe K線週期
type CryptoComTimeframe string

const (
	CryptoComTimeframe1m  CryptoComTimeframe = "1m"
	CryptoComTimeframe5m  CryptoComTimeframe = "5m"
	CryptoComTimeframe15m CryptoComTimeframe = "15m"
	CryptoComTimeframe30m CryptoComTimeframe = "30m"
	CryptoComTimeframe1h  CryptoComTimeframe = "1h"
	CryptoComTimeframe4h  CryptoComTimeframe = "4h"
	CryptoComTimeframe1D  CryptoComTimeframe = "1D"
)

// ConvertInterval 轉换 K線週期
func ConvertInterval(interval string) CryptoComTimeframe {
	switch interval {
	case "1m":
		return CryptoComTimeframe1m
	case "5m":
		return CryptoComTimeframe5m
	case "15m":
		return CryptoComTimeframe15m
	case "30m":
		return CryptoComTimeframe30m
	case "1h":
		return CryptoComTimeframe1h
	case "4h":
		return CryptoComTimeframe4h
	case "1d":
		return CryptoComTimeframe1D
	default:
		return CryptoComTimeframe1m
	}
}
