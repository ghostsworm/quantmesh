package coinsph

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"quantmesh/logger"
	"quantmesh/utils"
)

// 本地类型定义，避免循环导入
type Side string
type OrderType string
type OrderStatus string

const (
	SideBuy  Side = "BUY"
	SideSell Side = "SELL"
)

const (
	OrderTypeLimit      OrderType = "LIMIT"
	OrderTypeMarket     OrderType = "MARKET"
	OrderTypeLimitMaker OrderType = "LIMIT_MAKER"
)

const (
	OrderStatusNew             OrderStatus = "NEW"
	OrderStatusPartiallyFilled OrderStatus = "PARTIALLY_FILLED"
	OrderStatusFilled          OrderStatus = "FILLED"
	OrderStatusCanceled        OrderStatus = "CANCELED"
	OrderStatusRejected        OrderStatus = "REJECTED"
	OrderStatusExpired         OrderStatus = "EXPIRED"
)

// OrderRequest 下單请求
type OrderRequest struct {
	Symbol        string
	Side          Side
	Type          OrderType
	TimeInForce   string
	Quantity      float64
	Price         float64
	ReduceOnly    bool
	PostOnly      bool
	PriceDecimals int
	ClientOrderID string
}

// Order 订单信息
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

// Position 持仓信息（现货没有持仓）
type Position struct {
	Symbol         string
	Size           float64
	EntryPrice     float64
	MarkPrice      float64
	UnrealizedPNL  float64
	Leverage       int
	MarginType     string
	IsolatedMargin float64
}

// AdapterAccount 适配器账户信息
type AdapterAccount struct {
	TotalWalletBalance float64
	TotalMarginBalance float64
	AvailableBalance   float64
	Positions          []*Position
}

// OrderUpdate WebSocket订单更新
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
	Commission      float64
	CommissionAsset string
}

// Candle K线数据
type Candle struct {
	Symbol    string
	Open      float64
	High      float64
	Low       float64
	Close     float64
	Volume    float64
	Timestamp int64
	IsClosed  bool
}

// CandleUpdateCallback K线更新回调
type CandleUpdateCallback func(candle interface{})

// OrderBookLevel 订单簿档位
type OrderBookLevel struct {
	Price    float64
	Quantity float64
}

// OrderBook 订单簿
type OrderBook struct {
	Symbol    string
	Bids      []OrderBookLevel
	Asks      []OrderBookLevel
	Timestamp int64
}

// CoinsphSpotAdapter Coins.ph 現貨交易所適配器
type CoinsphSpotAdapter struct {
	client           *CoinsphClient
	symbol           string
	wsManager        *WebSocketManager
	priceDecimals    int
	quantityDecimals int
	tickSize         float64
	stepSize         float64
	baseAsset        string
	quoteAsset       string
	useTestnet       bool
}

// NewCoinsphSpotAdapter 創建 Coins.ph 現貨适配器
func NewCoinsphSpotAdapter(cfg map[string]string, symbol string) (*CoinsphSpotAdapter, error) {
	apiKey := cfg["api_key"]
	secretKey := cfg["secret_key"]
	testnetStr := cfg["testnet"]

	useTestnet := false
	if testnetStr == "true" {
		useTestnet = true
		logger.Info("🌐 [Coins.ph Spot] 使用測試模式（注意：Coins.ph没有测试网）")
	}

	if apiKey == "" || secretKey == "" {
		return nil, fmt.Errorf("Coins.ph API 配置不完整")
	}

	client := NewCoinsphClient(apiKey, secretKey, useTestnet)

	adapter := &CoinsphSpotAdapter{
		client:     client,
		symbol:     symbol,
		useTestnet: useTestnet,
		wsManager:  NewWebSocketManager(useTestnet),
	}

	ctxInit, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := adapter.fetchSymbolInfo(ctxInit); err != nil {
		logger.Warn("⚠️ [Coins.ph Spot] 獲取交易對信息失败: %v，使用默认精度", err)
		adapter.priceDecimals = 8
		adapter.quantityDecimals = 8
		adapter.baseAsset = "BTC"
		adapter.quoteAsset = "PHP"
	}

	return adapter, nil
}

// fetchSymbolInfo 獲取交易對信息
func (c *CoinsphSpotAdapter) fetchSymbolInfo(ctx context.Context) error {
	info, err := c.client.GetExchangeInfo(ctx, c.symbol)
	if err != nil {
		return fmt.Errorf("獲取交易所信息失败: %w", err)
	}

	// 查找对应的交易對
	var symbolInfo *Symbol
	for i := range info.Symbols {
		if strings.EqualFold(info.Symbols[i].Symbol, c.symbol) {
			symbolInfo = &info.Symbols[i]
			break
		}
	}

	if symbolInfo == nil {
		return fmt.Errorf("未找到交易對信息: %s", c.symbol)
	}

	c.priceDecimals = symbolInfo.QuoteAssetPrecision
	c.quantityDecimals = symbolInfo.BaseAssetPrecision
	c.baseAsset = symbolInfo.BaseAsset
	c.quoteAsset = symbolInfo.QuoteAsset

	// 解析過濾器
	for _, filter := range symbolInfo.Filters {
		switch filter.FilterType {
		case "PRICE_FILTER":
			if filter.TickSize != "" {
				c.tickSize, _ = strconv.ParseFloat(filter.TickSize, 64)
			}
		case "LOT_SIZE":
			if filter.StepSize != "" {
				c.stepSize, _ = strconv.ParseFloat(filter.StepSize, 64)
			}
		}
	}

	if c.tickSize <= 0 {
		c.tickSize = math.Pow10(-c.priceDecimals)
	}
	if c.stepSize <= 0 {
		c.stepSize = math.Pow10(-c.quantityDecimals)
	}

	logger.Info("ℹ️ [Coins.ph Spot] %s - 數量精度:%d, 價格精度:%d, 基础:%s, 计價:%s",
		c.symbol, c.quantityDecimals, c.priceDecimals, c.baseAsset, c.quoteAsset)

	return nil
}

// roundToTickSize 按價格步長四捨五入
func (c *CoinsphSpotAdapter) roundToTickSize(price float64, side Side) float64 {
	if c.tickSize <= 0 {
		return price
	}
	ticks := price / c.tickSize
	var roundedTicks float64
	if side == SideBuy {
		roundedTicks = math.Floor(ticks)
	} else {
		roundedTicks = math.Ceil(ticks)
	}
	return roundedTicks * c.tickSize
}

// roundToStepSize 按數量步長四捨五入
func (c *CoinsphSpotAdapter) roundToStepSize(quantity float64) float64 {
	if c.stepSize <= 0 {
		return quantity
	}
	steps := math.Floor(quantity / c.stepSize)
	return steps * c.stepSize
}

// GetName 獲取交易所名称
func (c *CoinsphSpotAdapter) GetName() string {
	return "Coins.ph Spot"
}

// GetMarketType 獲取市場類型：spot 現貨
func (c *CoinsphSpotAdapter) GetMarketType() string {
	return "spot"
}

// PlaceOrder 下單
func (c *CoinsphSpotAdapter) PlaceOrder(ctx context.Context, req *OrderRequest) (*Order, error) {
	adjustedPrice := c.roundToTickSize(req.Price, req.Side)
	adjustedQty := c.roundToStepSize(req.Quantity)
	if adjustedQty <= 0 {
		adjustedQty = c.stepSize
		if adjustedQty <= 0 {
			adjustedQty = math.Pow10(-c.quantityDecimals)
		}
	}

	orderType := "LIMIT"
	if req.Type == OrderTypeMarket {
		orderType = "MARKET"
		adjustedPrice = 0
	} else if req.PostOnly {
		orderType = "LIMIT_MAKER"
	}

	timeInForce := "GTC"
	if req.TimeInForce != "" {
		timeInForce = req.TimeInForce
	}

	clientOrderID := ""
	if req.ClientOrderID != "" {
		clientOrderID = utils.AddBrokerPrefix("coinsph", req.ClientOrderID)
	}

	orderReq := NewOrderRequest{
		Symbol:           c.symbol,
		Side:             string(req.Side),
		Type:             orderType,
		TimeInForce:      timeInForce,
		NewClientOrderID: clientOrderID,
		NewOrderRespType: "FULL",
	}

	if orderType == "MARKET" {
		if req.Side == SideBuy {
			// 買單：使用quoteOrderQty（花費的PHP金額）
			orderReq.QuoteOrderQty = fmt.Sprintf("%.*f", c.priceDecimals, adjustedQty*req.Price)
		} else {
			// 賣單：使用quantity（賣出的加密貨幣數量）
			orderReq.Quantity = fmt.Sprintf("%.*f", c.quantityDecimals, adjustedQty)
		}
	} else {
		orderReq.Quantity = fmt.Sprintf("%.*f", c.quantityDecimals, adjustedQty)
		orderReq.Price = fmt.Sprintf("%.*f", c.priceDecimals, adjustedPrice)
	}

	resp, err := c.client.PlaceOrder(ctx, orderReq)
	if err != nil {
		return nil, err
	}

	price, _ := strconv.ParseFloat(resp.Price, 64)
	qty, _ := strconv.ParseFloat(resp.OrigQty, 64)
	execQty, _ := strconv.ParseFloat(resp.ExecutedQty, 64)
	avgPrice := price
	if resp.CummulativeQuoteQty != "" && execQty > 0 {
		cumQuote, _ := strconv.ParseFloat(resp.CummulativeQuoteQty, 64)
		avgPrice = cumQuote / execQty
	}

	var status OrderStatus
	switch resp.Status {
	case "NEW":
		status = OrderStatusNew
	case "PARTIALLY_FILLED":
		status = OrderStatusPartiallyFilled
	case "FILLED":
		status = OrderStatusFilled
	case "CANCELED":
		status = OrderStatusCanceled
	case "EXPIRED":
		status = OrderStatusExpired
	default:
		status = OrderStatusRejected
	}

	return &Order{
		OrderID:       resp.OrderID,
		ClientOrderID: resp.ClientOrderID,
		Symbol:        resp.Symbol,
		Side:          Side(resp.Side),
		Type:          OrderType(resp.Type),
		Price:         price,
		Quantity:      qty,
		ExecutedQty:   execQty,
		AvgPrice:      avgPrice,
		Status:        status,
		CreatedAt:     time.Unix(0, resp.TransactTime*int64(time.Millisecond)),
		UpdateTime:    resp.TransactTime,
	}, nil
}

// BatchPlaceOrders 批量下單
func (c *CoinsphSpotAdapter) BatchPlaceOrders(ctx context.Context, orders []*OrderRequest) ([]*Order, bool) {
	placed := make([]*Order, 0, len(orders))
	hasBalanceError := false
	for _, req := range orders {
		order, err := c.PlaceOrder(ctx, req)
		if err != nil {
			logger.Warn("⚠️ [Coins.ph Spot] 下單失败 %.2f %s: %v", req.Price, req.Side, err)
			if strings.Contains(err.Error(), "insufficient") || strings.Contains(err.Error(), "balance") {
				hasBalanceError = true
			}
			continue
		}
		placed = append(placed, order)
	}
	return placed, hasBalanceError
}

// CancelOrder 取消訂單
func (c *CoinsphSpotAdapter) CancelOrder(ctx context.Context, symbol string, orderID int64) error {
	_, err := c.client.CancelOrder(ctx, symbol, orderID, "")
	if err != nil {
		errStr := err.Error()
		if strings.Contains(errStr, "not found") || strings.Contains(errStr, "不存在") {
			logger.Info("ℹ️ [Coins.ph Spot] 订單 %d 已不存在，跳過取消", orderID)
			return nil
		}
		return err
	}
	return nil
}

// BatchCancelOrders 批量取消訂單
func (c *CoinsphSpotAdapter) BatchCancelOrders(ctx context.Context, symbol string, orderIDs []int64) error {
	for _, orderID := range orderIDs {
		if err := c.CancelOrder(ctx, symbol, orderID); err != nil {
			logger.Warn("⚠️ [Coins.ph Spot] 取消訂單 %d 失败: %v", orderID, err)
		}
	}
	return nil
}

// CancelAllOrders 取消所有訂單
func (c *CoinsphSpotAdapter) CancelAllOrders(ctx context.Context, symbol string) error {
	_, err := c.client.CancelAllOrders(ctx, symbol)
	return err
}

// GetOrder 查詢訂單
func (c *CoinsphSpotAdapter) GetOrder(ctx context.Context, symbol string, orderID int64) (*Order, error) {
	orderInfo, err := c.client.GetOrder(ctx, symbol, orderID, "")
	if err != nil {
		return nil, err
	}

	return c.convertOrderInfoToOrder(orderInfo, symbol), nil
}

// convertOrderInfoToOrder 轉换OrderInfo為Order
func (c *CoinsphSpotAdapter) convertOrderInfoToOrder(info *OrderInfo, symbol string) *Order {
	price, _ := strconv.ParseFloat(info.Price, 64)
	qty, _ := strconv.ParseFloat(info.OrigQty, 64)
	execQty, _ := strconv.ParseFloat(info.ExecutedQty, 64)
	avgPrice := price
	if info.CummulativeQuoteQty != "" && execQty > 0 {
		cumQuote, _ := strconv.ParseFloat(info.CummulativeQuoteQty, 64)
		avgPrice = cumQuote / execQty
	}

	var status OrderStatus
	switch info.Status {
	case "NEW":
		status = OrderStatusNew
	case "PARTIALLY_FILLED":
		status = OrderStatusPartiallyFilled
	case "FILLED":
		status = OrderStatusFilled
	case "CANCELED":
		status = OrderStatusCanceled
	case "EXPIRED":
		status = OrderStatusExpired
	default:
		status = OrderStatusRejected
	}

	return &Order{
		OrderID:       info.OrderID,
		ClientOrderID: info.ClientOrderID,
		Symbol:        symbol,
		Side:          Side(info.Side),
		Type:          OrderType(info.Type),
		Price:         price,
		Quantity:      qty,
		ExecutedQty:   execQty,
		AvgPrice:      avgPrice,
		Status:        status,
		CreatedAt:     time.Unix(0, info.Time*int64(time.Millisecond)),
		UpdateTime:    info.UpdateTime,
	}
}

// GetOpenOrders 查詢未完成訂單
func (c *CoinsphSpotAdapter) GetOpenOrders(ctx context.Context, symbol string) ([]*Order, error) {
	openOrders, err := c.client.GetOpenOrders(ctx, symbol)
	if err != nil {
		return nil, err
	}

	result := make([]*Order, 0, len(openOrders))
	for i := range openOrders {
		result = append(result, c.convertOrderInfoToOrder(&openOrders[i], symbol))
	}

	return result, nil
}

// GetAccount 獲取帳戶信息
func (c *CoinsphSpotAdapter) GetAccount(ctx context.Context) (*AdapterAccount, error) {
	account, err := c.client.GetAccount(ctx)
	if err != nil {
		return nil, err
	}

	// 計算總餘額（PHP）
	totalBalance := 0.0
	availableBalance := 0.0

	for _, balance := range account.Balances {
		free, _ := strconv.ParseFloat(balance.Free, 64)
		locked, _ := strconv.ParseFloat(balance.Locked, 64)
		if balance.Asset == account.Token {
			totalBalance = free + locked
			availableBalance = free
			break
		}
	}

	return &AdapterAccount{
		TotalWalletBalance: totalBalance,
		TotalMarginBalance: totalBalance,
		AvailableBalance:   availableBalance,
		Positions:          []*Position{}, // 現貨沒有持倉
	}, nil
}

// GetPositions 獲取持倉信息（現貨沒有持倉）
func (c *CoinsphSpotAdapter) GetPositions(ctx context.Context, symbol string) ([]*Position, error) {
	return []*Position{}, nil
}

// GetBalance 獲取餘額
func (c *CoinsphSpotAdapter) GetBalance(ctx context.Context, asset string) (float64, error) {
	account, err := c.client.GetAccount(ctx)
	if err != nil {
		return 0, err
	}

	for _, balance := range account.Balances {
		if balance.Asset == asset {
			free, _ := strconv.ParseFloat(balance.Free, 64)
			return free, nil
		}
	}

	return 0, fmt.Errorf("未找到资产余额: %s", asset)
}

// StartOrderStream 啟動訂單流（使用User Data Stream）
func (c *CoinsphSpotAdapter) StartOrderStream(ctx context.Context, callback func(interface{})) error {
	// TODO: 实现User Data Stream
	return fmt.Errorf("Coins.ph現貨訂單流功能待实现")
}

// StopOrderStream 停止訂單流
func (c *CoinsphSpotAdapter) StopOrderStream() error {
	return nil
}

// GetLatestPrice 獲取最新價格
func (c *CoinsphSpotAdapter) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	ticker, err := c.client.GetPrice(ctx, symbol)
	if err != nil {
		return 0, err
	}

	price, err := strconv.ParseFloat(ticker.Price, 64)
	if err != nil {
		return 0, err
	}

	return price, nil
}

// StartPriceStream 啟動價格流
func (c *CoinsphSpotAdapter) StartPriceStream(ctx context.Context, symbol string, callback func(price float64)) error {
	return c.wsManager.StartPriceStream(ctx, symbol, callback)
}

// StartKlineStream 啟動K線流（Coins.ph現貨不支持）
func (c *CoinsphSpotAdapter) StartKlineStream(ctx context.Context, symbols []string, interval string, callback CandleUpdateCallback) error {
	return fmt.Errorf("Coins.ph現貨K線流功能待实现")
}

// StopKlineStream 停止K線流
func (c *CoinsphSpotAdapter) StopKlineStream() error {
	return c.wsManager.Stop()
}

// GetHistoricalKlines 獲取歷史K線數據（Coins.ph現貨不支持）
func (c *CoinsphSpotAdapter) GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*Candle, error) {
	return nil, fmt.Errorf("Coins.ph現貨歷史K線功能待实现")
}

// GetPriceDecimals 獲取價格精度
func (c *CoinsphSpotAdapter) GetPriceDecimals() int {
	return c.priceDecimals
}

// GetQuantityDecimals 獲取數量精度
func (c *CoinsphSpotAdapter) GetQuantityDecimals() int {
	return c.quantityDecimals
}

// GetBaseAsset 獲取基础资產
func (c *CoinsphSpotAdapter) GetBaseAsset() string {
	return c.baseAsset
}

// GetQuoteAsset 獲取计價资產
func (c *CoinsphSpotAdapter) GetQuoteAsset() string {
	return c.quoteAsset
}

// GetFundingRate 獲取资金费率（現貨沒有資金費率）
func (c *CoinsphSpotAdapter) GetFundingRate(ctx context.Context, symbol string) (float64, error) {
	return 0, fmt.Errorf("現貨交易沒有資金費率")
}

// GetSpotPrice 獲取現貨市场價格
func (c *CoinsphSpotAdapter) GetSpotPrice(ctx context.Context, symbol string) (float64, error) {
	return c.GetLatestPrice(ctx, symbol)
}

// EstimateFinalOrderAmount 預估最终下單金額
func (c *CoinsphSpotAdapter) EstimateFinalOrderAmount(symbol string, price, quantity float64, reduceOnly bool) float64 {
	return price * quantity
}

// GetOrderBook 獲取訂單簿深度（Coins.ph現貨不支持）
func (c *CoinsphSpotAdapter) GetOrderBook(ctx context.Context, symbol string, limit int) (*OrderBook, error) {
	return nil, fmt.Errorf("Coins.ph現貨訂單簿功能待实现")
}

// GetOrderFills 查詢訂單成交記錄
func (c *CoinsphSpotAdapter) GetOrderFills(ctx context.Context, symbol string, orderID int64) ([]interface{}, error) {
	trades, err := c.client.GetMyTrades(ctx, symbol, orderID, 1000)
	if err != nil {
		return nil, err
	}

	fills := make([]interface{}, len(trades))
	for i := range trades {
		fills[i] = &trades[i]
	}

	return fills, nil
}

// GetIncomeHistory 獲取收入歷史（現貨沒有）
func (c *CoinsphSpotAdapter) GetIncomeHistory(ctx context.Context, symbol, incomeType string, startTime, endTime int64) ([]interface{}, error) {
	return nil, nil
}

// InternalTransfer 交易所內部轉帳（Coins.ph現貨不支持）
func (c *CoinsphSpotAdapter) InternalTransfer(ctx context.Context, fromAccount, toAccount, asset string, amount float64) (string, error) {
	return "", fmt.Errorf("Coins.ph現貨內部轉帳功能待实现")
}
