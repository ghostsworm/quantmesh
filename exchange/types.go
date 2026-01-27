package exchange

import (
	"fmt"
	"time"
)

// Side 交易方向
type Side string

const (
	SideBuy  Side = "BUY"
	SideSell Side = "SELL"
)

// OrderType 订单类型
type OrderType string

const (
	OrderTypeLimit  OrderType = "LIMIT"
	OrderTypeMarket OrderType = "MARKET"
)

// OrderStatus 订单状态
type OrderStatus string

const (
	OrderStatusNew             OrderStatus = "NEW"
	OrderStatusPartiallyFilled OrderStatus = "PARTIALLY_FILLED"
	OrderStatusFilled          OrderStatus = "FILLED"
	OrderStatusCanceled        OrderStatus = "CANCELED"
	OrderStatusRejected        OrderStatus = "REJECTED"
	OrderStatusExpired         OrderStatus = "EXPIRED"
)

// TimeInForce 订单有效期
type TimeInForce string

const (
	TimeInForceGTC TimeInForce = "GTC" // Good Till Cancel
	TimeInForceIOC TimeInForce = "IOC" // Immediate or Cancel
	TimeInForceFOK TimeInForce = "FOK" // Fill or Kill
	TimeInForceGTX TimeInForce = "GTX" // Good Till Crossing (Post Only)
)

// OrderRequest 下单请求（通用）
type OrderRequest struct {
	Symbol        string
	Side          Side
	Type          OrderType
	TimeInForce   TimeInForce
	Quantity      float64
	Price         float64 // 市价单可为0
	ReduceOnly    bool    // 是否只减仓
	PostOnly      bool    // 是否只做 Maker（Post Only）
	PriceDecimals int     // 价格精度（用于格式化）
	ClientOrderID string  // 自定义订单ID
	StrategyName  string  // 策略名称（可选，用于日志追踪）
	StrategyType  string  // 策略类型（可选，如 "grid", "dca", "martingale"）
}

// Order 订单信息（通用）
type Order struct {
	OrderID       int64
	ClientOrderID string
	Symbol        string
	Side          Side
	Type          OrderType
	Price         float64
	Quantity      float64
	ExecutedQty   float64
	AvgPrice      float64
	Status        OrderStatus
	CreatedAt     time.Time
	UpdateTime    int64
}

// Position 持仓信息（通用）
type Position struct {
	Symbol         string
	Size           float64 // 正数表示多仓，负数表示空仓
	EntryPrice     float64
	MarkPrice      float64
	UnrealizedPNL  float64
	Leverage       int
	MarginType     string
	IsolatedMargin float64
}

// Account 账户信息（通用）
type Account struct {
	TotalWalletBalance float64
	TotalMarginBalance float64
	AvailableBalance   float64
	Positions          []*Position
	AccountLeverage    int // 账户级别的杠杆倍数（部分交易所支持）
}

// OrderUpdate WebSocket 订单更新事件（通用）
type OrderUpdate struct {
	OrderID       int64
	ClientOrderID string
	Symbol        string
	Side          Side
	Type          OrderType
	Status        OrderStatus
	Price         float64
	Quantity      float64
	ExecutedQty   float64
	AvgPrice      float64
	UpdateTime    int64
}

// OrderUpdateCallback 订单更新回调函数
type OrderUpdateCallback func(update OrderUpdate)

// Candle K线数据
type Candle struct {
	Symbol    string
	Open      float64
	High      float64
	Low       float64
	Close     float64
	Volume    float64
	Timestamp int64
	IsClosed  bool // K线是否完结
}

// Validate 验证K线数据的合理性
func (c *Candle) Validate() error {
	// 价格必须为正数
	if c.Open <= 0 || c.High <= 0 || c.Low <= 0 || c.Close <= 0 {
		return fmt.Errorf("价格必须为正数: Open=%.2f, High=%.2f, Low=%.2f, Close=%.2f", c.Open, c.High, c.Low, c.Close)
	}

	// OHLC 关系验证
	if c.High < c.Low {
		return fmt.Errorf("最高价不能低于最低价: High=%.2f, Low=%.2f", c.High, c.Low)
	}
	if c.High < c.Open || c.High < c.Close {
		return fmt.Errorf("最高价必须大于等于开盘价和收盘价: High=%.2f, Open=%.2f, Close=%.2f", c.High, c.Open, c.Close)
	}
	if c.Low > c.Open || c.Low > c.Close {
		return fmt.Errorf("最低价必须小于等于开盘价和收盘价: Low=%.2f, Open=%.2f, Close=%.2f", c.Low, c.Open, c.Close)
	}

	// 成交量不能为负数
	if c.Volume < 0 {
		return fmt.Errorf("成交量不能为负数: Volume=%.2f", c.Volume)
	}

	return nil
}

// CandleUpdateCallback K线更新回调函数
type CandleUpdateCallback func(candle *Candle)
