package phemex

import (
	"math"
)

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

// PhemexResolution K線週期
type PhemexResolution int

const (
	PhemexResolution1m  PhemexResolution = 60    // 1分钟
	PhemexResolution5m  PhemexResolution = 300   // 5分钟
	PhemexResolution15m PhemexResolution = 900   // 15分钟
	PhemexResolution30m PhemexResolution = 1800  // 30分钟
	PhemexResolution1h  PhemexResolution = 3600  // 1小時
	PhemexResolution4h  PhemexResolution = 14400 // 4小時
	PhemexResolution1d  PhemexResolution = 86400 // 1天
)

// ConvertInterval 轉换 K線週期
func ConvertInterval(interval string) PhemexResolution {
	switch interval {
	case "1m":
		return PhemexResolution1m
	case "5m":
		return PhemexResolution5m
	case "15m":
		return PhemexResolution15m
	case "30m":
		return PhemexResolution30m
	case "1h":
		return PhemexResolution1h
	case "4h":
		return PhemexResolution4h
	case "1d":
		return PhemexResolution1d
	default:
		return PhemexResolution1m
	}
}

// ScalePrice 價格缩放：將浮点數轉换為整數（乘以 10^priceScale）
func ScalePrice(price float64, priceScale int) int64 {
	return int64(price * math.Pow10(priceScale))
}

// UnscalePrice 價格反缩放：將整數轉换為浮点數（除以 10^priceScale）
func UnscalePrice(priceEp int64, priceScale int) float64 {
	return float64(priceEp) / math.Pow10(priceScale)
}

// ScaleValue 數值缩放：將浮点數轉换為整數（乘以 10^8）
func ScaleValue(value float64) int64 {
	return int64(value * 1e8)
}

// UnscaleValue 數值反缩放：將整數轉换為浮点數（除以 10^8）
func UnscaleValue(valueEv int64) float64 {
	return float64(valueEv) / 1e8
}
