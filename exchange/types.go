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

// OrderType 订單類型
type OrderType string

const (
	OrderTypeLimit  OrderType = "LIMIT"
	OrderTypeMarket OrderType = "MARKET"
)

// OrderStatus 订單状態
type OrderStatus string

const (
	OrderStatusNew             OrderStatus = "NEW"
	OrderStatusPartiallyFilled OrderStatus = "PARTIALLY_FILLED"
	OrderStatusFilled          OrderStatus = "FILLED"
	OrderStatusCanceled        OrderStatus = "CANCELED"
	OrderStatusRejected        OrderStatus = "REJECTED"
	OrderStatusExpired         OrderStatus = "EXPIRED"
)

// TimeInForce 订單有效期
type TimeInForce string

const (
	TimeInForceGTC TimeInForce = "GTC" // Good Till Cancel
	TimeInForceIOC TimeInForce = "IOC" // Immediate or Cancel
	TimeInForceFOK TimeInForce = "FOK" // Fill or Kill
	TimeInForceGTX TimeInForce = "GTX" // Good Till Crossing (Post Only)
)

// OrderRequest 下單请求（通用）
type OrderRequest struct {
	Symbol        string
	Side          Side
	Type          OrderType
	TimeInForce   TimeInForce
	Quantity      float64
	Price         float64 // 市價單可為0
	ReduceOnly    bool    // 是否只减倉
	PostOnly      bool    // 是否只做 Maker（Post Only）
	PriceDecimals int     // 價格精度（用於格式化）
	ClientOrderID string  // 自定义订單ID
	StrategyName  string  // 策略名称（可選，用於日志追踪）
	StrategyType  string  // 策略類型（可選，如 "grid", "dca", "martingale"）
}

// Order 订單信息（通用）
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

// Position 持倉資訊（通用）
type Position struct {
	Symbol         string
	Size           float64 // 正數表示多倉，负數表示空倉
	EntryPrice     float64
	MarkPrice      float64
	UnrealizedPNL  float64
	Leverage       int
	MarginType     string
	IsolatedMargin float64
}

// Account 帳戶資訊（通用）
type Account struct {
	TotalWalletBalance float64
	TotalMarginBalance float64
	AvailableBalance   float64
	Positions          []*Position
	AccountLeverage    int // 账戶级别的杠杆倍數（部分交易所支援）
}

// OrderUpdate WebSocket 订單更新事件（通用）
type OrderUpdate struct {
	OrderID         int64
	ClientOrderID   string
	Symbol          string
	Side            Side
	Type            OrderType
	Status          OrderStatus
	Price           float64
	Quantity        float64
	ExecutedQty     float64
	AvgPrice        float64
	UpdateTime      int64
	Commission      float64 // 本次成交手續費
	CommissionAsset string  // 手續費幣種
	RealizedPnL     float64 // 已實現盈虧（交易所計算）
}

// OrderFill 訂單成交記錄（用於查詢手續費）
type OrderFill struct {
	OrderID         int64   // 訂單ID
	TradeID         string  // 成交ID（交易所內部）
	Symbol          string  // 交易對
	Side            Side    // 買賣方向
	Price           float64 // 成交價格
	Quantity        float64 // 成交數量
	Commission      float64 // 手續費
	CommissionAsset string  // 手續費幣種
	TradeTime       int64   // 成交時間（毫秒）
	IsMaker         bool    // 是否為 Maker 訂單
}

// OrderUpdateCallback 订單更新回呼函數
type OrderUpdateCallback func(update OrderUpdate)

// Candle K線數據
type Candle struct {
	Symbol    string
	Open      float64
	High      float64
	Low       float64
	Close     float64
	Volume    float64
	Timestamp int64
	IsClosed  bool // K線是否完結
}

// Validate 驗证K線數據的合理性
func (c *Candle) Validate() error {
	// 價格必須為正數
	if c.Open <= 0 || c.High <= 0 || c.Low <= 0 || c.Close <= 0 {
		return fmt.Errorf("價格必須為正數: Open=%.2f, High=%.2f, Low=%.2f, Close=%.2f", c.Open, c.High, c.Low, c.Close)
	}

	// OHLC 关系驗证
	if c.High < c.Low {
		return fmt.Errorf("最高價不能低於最低價: High=%.2f, Low=%.2f", c.High, c.Low)
	}
	if c.High < c.Open || c.High < c.Close {
		return fmt.Errorf("最高價必須大於等於开盘價和收盘價: High=%.2f, Open=%.2f, Close=%.2f", c.High, c.Open, c.Close)
	}
	if c.Low > c.Open || c.Low > c.Close {
		return fmt.Errorf("最低價必須小於等於开盘價和收盘價: Low=%.2f, Open=%.2f, Close=%.2f", c.Low, c.Open, c.Close)
	}

	// 成交量不能為负數
	if c.Volume < 0 {
		return fmt.Errorf("成交量不能為负數: Volume=%.2f", c.Volume)
	}

	return nil
}

// CandleUpdateCallback K線更新回呼函數
type CandleUpdateCallback func(candle *Candle)

// OrderBookLevel 订單簿檔位
type OrderBookLevel struct {
	Price    float64 // 價格
	Quantity float64 // 數量
}

// OrderBook 订單簿
type OrderBook struct {
	Symbol    string           // 交易對
	Bids      []OrderBookLevel // 買盘 (價格從高到低)
	Asks      []OrderBookLevel // 賣盘 (價格從低到高)
	Timestamp int64            // 時间戳
}

// FundingInfo 資金費率詳細信息
type FundingInfo struct {
	Symbol          string    // 交易對
	Rate            float64   // 當前資金費率
	NextFundingTime time.Time // 下次結算時間
	MarkPrice       float64   // 標記價格
	IndexPrice      float64   // 指數價格
	EstSettlePnl    float64   // 預估結算 PnL（可選）
}
