package xtcom

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"quantmesh/logger"
)

// Adapter XT.COM 适配器
type Adapter struct {
	client           *XTClient
	wsManager        *WebSocketManager
	klineWSManager   *KlineWebSocketManager
	symbol           string
	priceDecimals    int
	quantityDecimals int
	baseAsset        string
	quoteAsset       string
}

// NewAdapter 創建 XT.COM 适配器
func NewAdapter(config map[string]string, symbol string) (*Adapter, error) {
	apiKey := config["api_key"]
	secretKey := config["secret_key"]
	isTestnet := config["testnet"] == "true"

	if apiKey == "" || secretKey == "" {
		return nil, fmt.Errorf("XT.COM API key or secret key is empty")
	}

	client := NewXTClient(apiKey, secretKey, isTestnet)

	// XT.COM 符号格式：btc_usdt (小写，下划線分隔)
	xtSymbol := convertSymbolToXT(symbol)

	adapter := &Adapter{
		client:           client,
		symbol:           xtSymbol,
		priceDecimals:    2,
		quantityDecimals: 4,
		baseAsset:        "BTC",
		quoteAsset:       "USDT",
	}

	// 獲取交易對信息
	ctx := context.Background()
	symbolInfo, err := client.GetSymbol(ctx, xtSymbol)
	if err != nil {
		logger.Warn("Failed to get XT.COM symbol: %v", err)
	} else {
		adapter.priceDecimals = symbolInfo.PricePrecision
		adapter.quantityDecimals = symbolInfo.QuantityPrecision
		adapter.baseAsset = strings.ToUpper(symbolInfo.BaseCurrency)
		adapter.quoteAsset = strings.ToUpper(symbolInfo.QuoteCurrency)
	}

	return adapter, nil
}

// convertSymbolToXT 轉换交易對格式：BTCUSDT -> btc_usdt
func convertSymbolToXT(symbol string) string {
	symbol = strings.ToLower(symbol)
	if strings.HasSuffix(symbol, "usdt") {
		base := strings.TrimSuffix(symbol, "usdt")
		return base + "_usdt"
	}
	return symbol
}

// GetName 獲取交易所名称
func (a *Adapter) GetName() string {
	return "XT.COM"
}

// GetMarketType 獲取市場類型：futures 合約
func (a *Adapter) GetMarketType() string {
	return "futures"
}

// PlaceOrder 下單
func (a *Adapter) PlaceOrder(ctx context.Context, side OrderSide, price, quantity float64, clientOrderID string) (*OrderLocal, error) {
	var xtSide string
	if side == SideBuy {
		xtSide = "BUY"
	} else {
		xtSide = "SELL"
	}

	req := &OrderRequest{
		Symbol:        a.symbol,
		Side:          xtSide,
		Type:          "LIMIT",
		Quantity:      fmt.Sprintf("%.8f", quantity),
		Price:         fmt.Sprintf("%.8f", price),
		ClientOrderID: clientOrderID,
	}

	order, err := a.client.PlaceOrder(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("XT.COM place order error: %w", err)
	}

	return a.convertOrder(order), nil
}

// CancelOrder 取消訂單
func (a *Adapter) CancelOrder(ctx context.Context, orderID string) error {
	return a.client.CancelOrder(ctx, a.symbol, orderID)
}

// GetOrder 查詢訂單
func (a *Adapter) GetOrder(ctx context.Context, orderID string) (*OrderLocal, error) {
	order, err := a.client.GetOrder(ctx, a.symbol, orderID)
	if err != nil {
		return nil, err
	}

	return a.convertOrder(order), nil
}

// GetOpenOrders 獲取活跃订單
func (a *Adapter) GetOpenOrders(ctx context.Context) ([]*OrderLocal, error) {
	orders, err := a.client.GetOpenOrders(ctx, a.symbol)
	if err != nil {
		return nil, err
	}

	result := make([]*OrderLocal, 0, len(orders))
	for _, order := range orders {
		result = append(result, a.convertOrder(&order))
	}

	return result, nil
}

// GetAccount 獲取帳戶信息
func (a *Adapter) GetAccount(ctx context.Context) (*AccountLocal, error) {
	balance, err := a.client.GetBalance(ctx)
	if err != nil {
		return nil, err
	}

	available, _ := strconv.ParseFloat(balance.Available, 64)
	frozen, _ := strconv.ParseFloat(balance.Frozen, 64)
	total := available + frozen

	return &AccountLocal{
		TotalWalletBalance: total,
		TotalMarginBalance: total,
		AvailableBalance:   available,
	}, nil
}

// GetPositions 獲取持倉（XT.COM 現貨交易所，返回空）
func (a *Adapter) GetPositions(ctx context.Context) ([]*PositionLocal, error) {
	return []*PositionLocal{}, nil
}

// GetBalance 獲取餘額
func (a *Adapter) GetBalance(ctx context.Context) (float64, error) {
	balance, err := a.client.GetBalance(ctx)
	if err != nil {
		return 0, err
	}

	available, _ := strconv.ParseFloat(balance.Available, 64)
	return available, nil
}

// StartOrderStream 啟動訂單流
func (a *Adapter) StartOrderStream(ctx context.Context, callback func(interface{})) error {
	if a.wsManager != nil {
		return fmt.Errorf("order stream already started")
	}

	a.wsManager = NewWebSocketManager(a.client.apiKey, a.client.secretKey, a.client.isTestnet)
	return a.wsManager.Start(ctx, a.symbol, callback)
}

// StopOrderStream 停止訂單流
func (a *Adapter) StopOrderStream() error {
	if a.wsManager != nil {
		a.wsManager.Stop()
		a.wsManager = nil
	}
	return nil
}

// GetLatestPrice 獲取最新價格
func (a *Adapter) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	// 如果傳入 symbol,轉换格式並使用;否则使用預設 symbol
	targetSymbol := a.symbol
	if symbol != "" {
		targetSymbol = convertSymbolToXT(symbol)
	}

	ticker, err := a.client.GetTicker(ctx, targetSymbol)
	if err != nil {
		return 0, err
	}

	price, err := strconv.ParseFloat(ticker.Price, 64)
	if err != nil {
		return 0, err
	}

	return price, nil
}

// StartKlineStream 啟动 K線流
func (a *Adapter) StartKlineStream(ctx context.Context, interval string, callback CandleUpdateCallbackLocal) error {
	if a.klineWSManager != nil {
		return fmt.Errorf("kline stream already started")
	}

	xtInterval := string(ConvertInterval(interval))
	a.klineWSManager = NewKlineWebSocketManager(a.client.isTestnet)

	return a.klineWSManager.Start(ctx, a.symbol, xtInterval, func(kline *Kline) {
		open, _ := strconv.ParseFloat(kline.Open, 64)
		high, _ := strconv.ParseFloat(kline.High, 64)
		low, _ := strconv.ParseFloat(kline.Low, 64)
		close, _ := strconv.ParseFloat(kline.Close, 64)
		volume, _ := strconv.ParseFloat(kline.Volume, 64)

		candle := &CandleLocal{
			Symbol:    a.symbol,
			Timestamp: kline.Time,
			Open:      open,
			High:      high,
			Low:       low,
			Close:     close,
			Volume:    volume,
		}
		callback(candle)
	})
}

// StopKlineStream 停止 K線流
func (a *Adapter) StopKlineStream() error {
	if a.klineWSManager != nil {
		a.klineWSManager.Stop()
		a.klineWSManager = nil
	}
	return nil
}

// GetHistoricalKlines 獲取歷史 K線
func (a *Adapter) GetHistoricalKlines(ctx context.Context, interval string, limit int) ([]*CandleLocal, error) {
	xtInterval := string(ConvertInterval(interval))
	klines, err := a.client.GetKlines(ctx, a.symbol, xtInterval, limit)
	if err != nil {
		return nil, err
	}

	result := make([]*CandleLocal, 0, len(klines))
	for _, kline := range klines {
		open, _ := strconv.ParseFloat(kline.Open, 64)
		high, _ := strconv.ParseFloat(kline.High, 64)
		low, _ := strconv.ParseFloat(kline.Low, 64)
		close, _ := strconv.ParseFloat(kline.Close, 64)
		volume, _ := strconv.ParseFloat(kline.Volume, 64)

		result = append(result, &CandleLocal{
			Symbol:    a.symbol,
			Timestamp: kline.Time,
			Open:      open,
			High:      high,
			Low:       low,
			Close:     close,
			Volume:    volume,
		})
	}

	return result, nil
}

// GetPriceDecimals 獲取價格精度
func (a *Adapter) GetPriceDecimals() int {
	return a.priceDecimals
}

// GetQuantityDecimals 獲取數量精度
func (a *Adapter) GetQuantityDecimals() int {
	return a.quantityDecimals
}

// GetBaseAsset 獲取基础资產
func (a *Adapter) GetBaseAsset() string {
	return a.baseAsset
}

// GetQuoteAsset 獲取报價资產
func (a *Adapter) GetQuoteAsset() string {
	return a.quoteAsset
}

// GetFundingRate 獲取资金费率（XT.COM 現貨交易所，回傳 0）
func (a *Adapter) GetFundingRate(ctx context.Context) (float64, error) {
	return 0, nil
}

// convertOrder 轉换订單
func (a *Adapter) convertOrder(order *Order) *OrderLocal {
	var side OrderSide
	if order.Side == "BUY" {
		side = SideBuy
	} else {
		side = SideSell
	}

	var status OrderStatus
	switch order.Status {
	case "NEW":
		status = OrderStatusNew
	case "PARTIALLY_FILLED":
		status = OrderStatusPartiallyFilled
	case "FILLED":
		status = OrderStatusFilled
	case "CANCELED", "REJECTED", "EXPIRED":
		status = OrderStatusCanceled
	default:
		status = OrderStatusNew
	}

	price, _ := strconv.ParseFloat(order.Price, 64)
	quantity, _ := strconv.ParseFloat(order.Quantity, 64)
	executedQty, _ := strconv.ParseFloat(order.ExecutedQty, 64)

	return &OrderLocal{
		OrderID:       order.OrderID,
		ClientOrderID: order.ClientOrderID,
		Symbol:        order.Symbol,
		Side:          side,
		Price:         price,
		Quantity:      quantity,
		ExecutedQty:   executedQty,
		Status:        status,
		UpdateTime:    order.UpdateTime,
	}
}

// InternalTransfer 交易所內部轉帳（XT.com 暂未實現）
func (a *Adapter) InternalTransfer(ctx context.Context, fromAccount, toAccount, asset string, amount float64) (string, error) {
	return "", fmt.Errorf("internal transfer not implemented for XT.com")
}
