package bitfinex

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"quantmesh/logger"
)

// Adapter Bitfinex 适配器
type Adapter struct {
	client           *BitfinexClient
	wsManager        *WebSocketManager
	klineWSManager   *KlineWebSocketManager
	symbol           string
	priceDecimals    int
	quantityDecimals int
	baseAsset        string
	quoteAsset       string
}

// NewBitfinexAdapter 创建 Bitfinex 适配器
func NewBitfinexAdapter(config map[string]string, symbol string) (*Adapter, error) {
	apiKey := config["api_key"]
	secretKey := config["secret_key"]

	if apiKey == "" || secretKey == "" {
		return nil, fmt.Errorf("Bitfinex API key and secret key are required")
	}

	client := NewBitfinexClient(apiKey, secretKey)

	// 转换符号格式：BTCUSDT -> BTCUSD
	bitfinexSymbol := convertToBitfinexSymbol(symbol)
	
	// 解析基础资产和报价资产
	baseAsset, quoteAsset := parseSymbol(bitfinexSymbol)

	adapter := &Adapter{
		client:           client,
		symbol:           bitfinexSymbol,
		priceDecimals:    2,    // Bitfinex 默认价格精度
		quantityDecimals: 8,    // Bitfinex 默认数量精度
		baseAsset:        baseAsset,
		quoteAsset:       quoteAsset,
	}

	logger.Info("Bitfinex adapter created for symbol: %s (base: %s, quote: %s)", bitfinexSymbol, baseAsset, quoteAsset)
	return adapter, nil
}

// convertToBitfinexSymbol 转换为 Bitfinex 符号格式
func convertToBitfinexSymbol(symbol string) string {
	// BTCUSDT -> BTCUSD
	symbol = strings.ToUpper(symbol)
	symbol = strings.ReplaceAll(symbol, "USDT", "USD")
	return symbol
}

// parseSymbol 解析符号
func parseSymbol(symbol string) (string, string) {
	// BTCUSD -> BTC, USD
	if strings.HasSuffix(symbol, "USD") {
		base := strings.TrimSuffix(symbol, "USD")
		return base, "USD"
	}
	// 默认返回
	return symbol[:3], symbol[3:]
}

// GetName 获取交易所名称
func (a *Adapter) GetName() string {
	return "bitfinex"
}

// PlaceOrder 下单
func (a *Adapter) PlaceOrder(ctx context.Context, req *BitfinexOrderRequest) (*Order, error) {
	clientOrderID := fmt.Sprintf("order_%d", time.Now().UnixMilli())
	
	orderReq := &OrderRequest{
		Symbol:        a.symbol,
		Side:          string(req.Side),
		Type:          string(req.Type),
		Price:         req.Price,
		Quantity:      req.Quantity,
		ClientOrderID: clientOrderID,
	}

	resp, err := a.client.PlaceOrder(ctx, orderReq)
	if err != nil {
		return nil, fmt.Errorf("place order error: %w", err)
	}

	order := &Order{
		OrderID:       resp.OrderID,
		ClientOrderID: clientOrderID,
		Symbol:        a.symbol,
		Side:          string(req.Side),
		Type:          string(req.Type),
		Price:         req.Price,
		Quantity:      req.Quantity,
		Status:        string(OrderStatusNew),
		CreatedAt:     time.Now(),
	}

	logger.Info("Bitfinex order placed: %s, side: %s, price: %.2f, quantity: %.8f", 
		order.OrderID, order.Side, order.Price, order.Quantity)
	return order, nil
}

// BatchPlaceOrders 批量下单
func (a *Adapter) BatchPlaceOrders(ctx context.Context, orders []*BitfinexOrderRequest) ([]*Order, bool) {
	results := make([]*Order, 0, len(orders))
	allSuccess := true

	for _, orderReq := range orders {
		order, err := a.PlaceOrder(ctx, orderReq)
		if err != nil {
			logger.Error("Batch place order failed: %v", err)
			allSuccess = false
			continue
		}
		results = append(results, order)
	}

	return results, allSuccess
}

// CancelOrder 取消订单
func (a *Adapter) CancelOrder(ctx context.Context, symbol string, orderID int64) error {
	orderIDStr := strconv.FormatInt(orderID, 10)
	err := a.client.CancelOrder(ctx, orderIDStr)
	if err != nil {
		return fmt.Errorf("cancel order error: %w", err)
	}

	logger.Info("Bitfinex order cancelled: %d", orderID)
	return nil
}

// BatchCancelOrders 批量取消订单
func (a *Adapter) BatchCancelOrders(ctx context.Context, symbol string, orderIDs []int64) error {
	for _, orderID := range orderIDs {
		if err := a.CancelOrder(ctx, symbol, orderID); err != nil {
			logger.Error("Batch cancel order %d failed: %v", orderID, err)
		}
	}
	return nil
}

// CancelAllOrders 取消所有订单
func (a *Adapter) CancelAllOrders(ctx context.Context, symbol string) error {
	orders, err := a.client.GetActiveOrders(ctx, a.symbol)
	if err != nil {
		return fmt.Errorf("get active orders error: %w", err)
	}

	for _, order := range orders {
		if err := a.client.CancelOrder(ctx, order.ID); err != nil {
			logger.Error("Cancel order %s failed: %v", order.ID, err)
		}
	}

	logger.Info("Bitfinex all orders cancelled for symbol: %s", symbol)
	return nil
}

// GetOrder 查询订单
func (a *Adapter) GetOrder(ctx context.Context, symbol string, orderID int64) (*Order, error) {
	orders, err := a.client.GetActiveOrders(ctx, a.symbol)
	if err != nil {
		return nil, fmt.Errorf("get order error: %w", err)
	}

	orderIDStr := strconv.FormatInt(orderID, 10)
	for _, orderInfo := range orders {
		if orderInfo.ID == orderIDStr {
			return a.convertToOrder(&orderInfo), nil
		}
	}

	return nil, fmt.Errorf("order %d not found", orderID)
}

// GetOpenOrders 查询未完成订单
func (a *Adapter) GetOpenOrders(ctx context.Context, symbol string) ([]*Order, error) {
	orders, err := a.client.GetActiveOrders(ctx, a.symbol)
	if err != nil {
		return nil, fmt.Errorf("get open orders error: %w", err)
	}

	result := make([]*Order, 0, len(orders))
	for _, orderInfo := range orders {
		result = append(result, a.convertToOrder(&orderInfo))
	}

	return result, nil
}

// GetAccount 获取账户信息
func (a *Adapter) GetAccount(ctx context.Context) (*Account, error) {
	wallets, err := a.client.GetWallets(ctx)
	if err != nil {
		return nil, fmt.Errorf("get account error: %w", err)
	}

	var totalBalance, availableBalance float64
	for _, wallet := range wallets {
		if wallet.Type == "exchange" && wallet.Currency == a.quoteAsset {
			totalBalance += wallet.Balance
			availableBalance += wallet.BalanceAvailable
		}
	}

	account := &Account{
		TotalBalance:     totalBalance,
		AvailableBalance: availableBalance,
		MarginBalance:    totalBalance,
	}

	return account, nil
}

// GetPositions 获取持仓信息
func (a *Adapter) GetPositions(ctx context.Context, symbol string) ([]*Position, error) {
	positions, err := a.client.GetPositions(ctx)
	if err != nil {
		return nil, fmt.Errorf("get positions error: %w", err)
	}

	result := make([]*Position, 0, len(positions))
	for _, pos := range positions {
		if pos.Symbol != a.symbol {
			continue
		}

		side := "LONG"
		size := pos.Amount
		if pos.Amount < 0 {
			side = "SHORT"
			size = -pos.Amount
		}

		position := &Position{
			Symbol:        pos.Symbol,
			Side:          side,
			Size:          size,
			EntryPrice:    pos.BasePrice,
			MarkPrice:     pos.BasePrice,
			UnrealizedPnL: pos.PL,
		}
		result = append(result, position)
	}

	return result, nil
}

// GetBalance 获取余额
func (a *Adapter) GetBalance(ctx context.Context, asset string) (float64, error) {
	wallets, err := a.client.GetWallets(ctx)
	if err != nil {
		return 0, fmt.Errorf("get balance error: %w", err)
	}

	for _, wallet := range wallets {
		if wallet.Type == "exchange" && wallet.Currency == asset {
			return wallet.BalanceAvailable, nil
		}
	}

	return 0, nil
}

// StartOrderStream 启动订单流
func (a *Adapter) StartOrderStream(ctx context.Context, callback func(interface{})) error {
	if a.wsManager != nil {
		return fmt.Errorf("order stream already started")
	}

	wsManager, err := NewWebSocketManager(a.client, a.symbol)
	if err != nil {
		return fmt.Errorf("create websocket manager error: %w", err)
	}

	a.wsManager = wsManager
	return wsManager.StartOrderStream(ctx, callback)
}

// StopOrderStream 停止订单流
func (a *Adapter) StopOrderStream() error {
	if a.wsManager != nil {
		a.wsManager.Stop()
		a.wsManager = nil
	}
	return nil
}

// GetLatestPrice 获取最新价格
func (a *Adapter) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	ticker, err := a.client.GetTicker(ctx, a.symbol)
	if err != nil {
		return 0, fmt.Errorf("get latest price error: %w", err)
	}

	return ticker.LastPrice, nil
}

// StartPriceStream 启动价格流
func (a *Adapter) StartPriceStream(ctx context.Context, symbol string, callback func(price float64)) error {
	if a.wsManager == nil {
		wsManager, err := NewWebSocketManager(a.client, symbol)
		if err != nil {
			return fmt.Errorf("create websocket manager error: %w", err)
		}
		a.wsManager = wsManager
	}

	return a.wsManager.StartPriceStream(ctx, callback)
}

// StartKlineStream 启动K线流
func (a *Adapter) StartKlineStream(ctx context.Context, symbols []string, interval string, callback CandleUpdateCallback) error {
	if a.klineWSManager != nil {
		return fmt.Errorf("kline stream already started")
	}

	klineWSManager, err := NewKlineWebSocketManager(a.client, symbols, interval)
	if err != nil {
		return fmt.Errorf("create kline websocket manager error: %w", err)
	}

	a.klineWSManager = klineWSManager
	return klineWSManager.Start(ctx, callback)
}

// StopKlineStream 停止K线流
func (a *Adapter) StopKlineStream() error {
	if a.klineWSManager != nil {
		a.klineWSManager.Stop()
		a.klineWSManager = nil
	}
	return nil
}

// GetHistoricalKlines 获取历史K线数据
func (a *Adapter) GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*BitfinexCandle, error) {
	timeframe := convertIntervalToTimeframe(interval)
	candles, err := a.client.GetCandles(ctx, a.symbol, timeframe, limit)
	if err != nil {
		return nil, fmt.Errorf("get historical klines error: %w", err)
	}

	result := make([]*BitfinexCandle, 0, len(candles))
	for _, candle := range candles {
		result = append(result, &BitfinexCandle{
			Symbol:    a.symbol,
			Open:      candle.Open,
			High:      candle.High,
			Low:       candle.Low,
			Close:     candle.Close,
			Volume:    candle.Volume,
			OpenTime:  candle.Timestamp,
			CloseTime: candle.Timestamp,
			IsClosed:  true,
		})
	}

	return result, nil
}

// GetPriceDecimals 获取价格精度
func (a *Adapter) GetPriceDecimals() int {
	return a.priceDecimals
}

// GetQuantityDecimals 获取数量精度
func (a *Adapter) GetQuantityDecimals() int {
	return a.quantityDecimals
}

// GetBaseAsset 获取基础资产
func (a *Adapter) GetBaseAsset() string {
	return a.baseAsset
}

// GetQuoteAsset 获取报价资产
func (a *Adapter) GetQuoteAsset() string {
	return a.quoteAsset
}

// GetFundingRate 获取资金费率
func (a *Adapter) GetFundingRate(ctx context.Context, symbol string) (float64, error) {
	// Bitfinex 现货交易没有资金费率
	return 0, nil
}

// convertToOrder 将 Bitfinex 订单转换为通用订单
func (a *Adapter) convertToOrder(orderInfo *OrderInfo) *Order {
	side := "BUY"
	quantity := orderInfo.Amount
	if orderInfo.Amount < 0 {
		side = "SELL"
		quantity = -orderInfo.Amount
	}

	executedQty := orderInfo.AmountOrig - math.Abs(orderInfo.Amount)

	status := string(OrderStatusNew)
	if executedQty > 0 && executedQty < orderInfo.AmountOrig {
		status = string(OrderStatusPartiallyFilled)
	} else if executedQty >= orderInfo.AmountOrig {
		status = string(OrderStatusFilled)
	}

	return &Order{
		OrderID:       orderInfo.ID,
		ClientOrderID: orderInfo.CID,
		Symbol:        orderInfo.Symbol,
		Side:          side,
		Type:          orderInfo.Type,
		Quantity:      quantity,
		ExecutedQty:   executedQty,
		Status:        status,
		CreatedAt:     time.Unix(0, orderInfo.MTSCreate*int64(time.Millisecond)),
		UpdateTime:    orderInfo.MTSUpdate,
	}
}

// convertIntervalToTimeframe 将时间间隔转换为 Bitfinex 时间框架
func convertIntervalToTimeframe(interval string) string {
	switch interval {
	case "1m":
		return "1m"
	case "5m":
		return "5m"
	case "15m":
		return "15m"
	case "30m":
		return "30m"
	case "1h":
		return "1h"
	case "3h":
		return "3h"
	case "6h":
		return "6h"
	case "12h":
		return "12h"
	case "1d":
		return "1D"
	case "1w":
		return "7D"
	case "2w":
		return "14D"
	case "1M":
		return "1M"
	default:
		return "1h" // 默认 1 小时
	}
}
