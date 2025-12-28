package bitfinex

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"quantmesh/logger"
)

// Adapter Bitfinex 适配器
type Adapter struct {
	client            *BitfinexClient
	wsManager         *WebSocketManager
	klineWSManager    *KlineWebSocketManager
	symbol            string
	priceDecimals     int
	quantityDecimals  int
	baseAsset         string
	quoteAsset        string
}

// NewBitfinexAdapter 创建 Bitfinex 适配器
func NewBitfinexAdapter(config map[string]string, symbol string) (*Adapter, error) {
	apiKey := config["api_key"]
	secretKey := config["secret_key"]

	if apiKey == "" || secretKey == "" {
		return nil, fmt.Errorf("Bitfinex API key and secret key are required")
	}

	client := NewBitfinexClient(apiKey, secretKey)

	// 解析交易对：Bitfinex 使用 PI_XBTUSD 格式（永续合约）
	// 将 BTCUSDT 转换为 PI_XBTUSD
	bitfinexSymbol := convertToBitfinexSymbol(symbol)

	adapter := &Adapter{
		client:           client,
		symbol:           bitfinexSymbol,
		priceDecimals:    1,
		quantityDecimals: 0, // Bitfinex 期货使用整数张数
		baseAsset:        extractBaseAsset(symbol),
		quoteAsset:       "USD",
	}

	// Bitfinex 使用默认精度
	logger.Info("Bitfinex symbol %s precision: price=%d, quantity=%d", bitfinexSymbol, adapter.priceDecimals, adapter.quantityDecimals)

	return adapter, nil
}

// convertToBitfinexSymbol 将标准符号转换为 Bitfinex 符号
func convertToBitfinexSymbol(symbol string) string {
	// BTCUSDT -> PI_XBTUSD
	// ETHUSDT -> PI_ETHUSD
	symbol = strings.ToUpper(symbol)
	symbol = strings.ReplaceAll(symbol, "USDT", "")
	
	// BTC -> XBT (Bitfinex 使用 XBT 代表 BTC)
	if strings.HasPrefix(symbol, "BTC") {
		symbol = strings.ReplaceAll(symbol, "BTC", "XBT")
	}
	
	return "PI_" + symbol + "USD"
}

// extractBaseAsset 提取基础资产
func extractBaseAsset(symbol string) string {
	symbol = strings.ToUpper(symbol)
	symbol = strings.ReplaceAll(symbol, "USDT", "")
	symbol = strings.ReplaceAll(symbol, "USD", "")
	return symbol
}

// getPrecision 根据 tickSize 计算精度
func getPrecision(tickSize float64) int {
	str := fmt.Sprintf("%.10f", tickSize)
	str = strings.TrimRight(str, "0")
	parts := strings.Split(str, ".")
	if len(parts) == 2 {
		return len(parts[1])
	}
	return 0
}

// GetName 获取交易所名称
func (a *Adapter) GetName() string {
	return "bitfinex"
}

// PlaceOrder 下单
func (a *Adapter) PlaceOrder(ctx context.Context, req *BitfinexOrderRequest) (*Order, error) {
	clientOrderID := fmt.Sprintf("order_%d", req.Timestamp)
	
	orderReq := &OrderRequest{
		ClientOrderID:    clientOrderID,
		Symbol:           a.symbol,
		Side:             strings.ToLower(string(req.Side)),
		Type:             strings.ToLower(string(req.Type)),
		Price:            req.Price,
		Quantity:         req.Quantity,
		PriceDecimals:    a.priceDecimals,
		QuantityDecimals: a.quantityDecimals,
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
		Status:        "NEW",
		Timestamp:     req.Timestamp,
	}

	logger.Info("Bitfinex order placed: %s, side: %s, price: %.2f, quantity: %.2f", order.OrderID, order.Side, order.Price, order.Quantity)
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
	_, err := a.client.CancelOrder(ctx, orderIDStr)
	if err != nil {
		return fmt.Errorf("cancel order error: %w", err)
	}

	logger.Info("Bitfinex order cancelled: %s", orderIDStr)
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
	orders, err := a.client.GetOpenOrders(ctx, symbol)
	if err != nil {
		return fmt.Errorf("get open orders error: %w", err)
	}

	for _, order := range orders {
		if _, err := a.client.CancelOrder(ctx, order.OrderID); err != nil {
			logger.Error("Cancel order %s failed: %v", order.OrderID, err)
		}
	}

	logger.Info("Bitfinex all orders cancelled")
	return nil
}

// GetOrder 查询订单
func (a *Adapter) GetOrder(ctx context.Context, symbol string, orderID int64) (*Order, error) {
	orderIDStr := strconv.FormatInt(orderID, 10)
	orderInfo, err := a.client.GetOrderInfo(ctx, symbol, orderIDStr)
	if err != nil {
		return nil, fmt.Errorf("get order error: %w", err)
	}

	return a.convertToOrder(orderInfo), nil
}

// GetOpenOrders 查询未完成订单
func (a *Adapter) GetOpenOrders(ctx context.Context, symbol string) ([]*Order, error) {
	orders, err := a.client.GetOpenOrders(ctx, symbol)
	if err != nil {
		return nil, fmt.Errorf("get open orders error: %w", err)
	}

	result := make([]*Order, 0, len(orders))
	for _, order := range orders {
		result = append(result, a.convertToOrder(&order))
	}

	return result, nil
}

// GetAccount 获取账户信息
func (a *Adapter) GetAccount(ctx context.Context) (*Account, error) {
	accountInfo, err := a.client.GetAccountInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("get account error: %w", err)
	}

	account := &Account{
		TotalBalance:     accountInfo.TotalBalance,
		AvailableBalance: accountInfo.TotalBalance,
		UnrealizedPnL:    0,
		MarginBalance:    accountInfo.TotalBalance,
	}

	return account, nil
}

// GetPositions 获取持仓信息
func (a *Adapter) GetPositions(ctx context.Context, symbol string) ([]*Position, error) {
	positions, err := a.client.GetPositionInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("get positions error: %w", err)
	}

	result := make([]*Position, 0, len(positions))
	for _, pos := range positions {
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
	accountInfo, err := a.client.GetAccountInfo(ctx)
	if err != nil {
		return 0, fmt.Errorf("get balance error: %w", err)
	}

	return accountInfo.TotalBalance, nil
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
	positions, err := a.client.GetPositionInfo(ctx)
	if err != nil {
		return 0, fmt.Errorf("get latest price error: %w", err)
	}

	if len(positions) > 0 {
		return positions[0].BasePrice, nil
	}

	return 0, fmt.Errorf("no position found")
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
	resolution := convertIntervalToResolution(interval)
	candles, err := a.client.GetHistoricalKlines(ctx, a.symbol, resolution, limit)
	if err != nil {
		return nil, fmt.Errorf("get historical klines error: %w", err)
	}

	result := make([]*BitfinexCandle, 0, len(candles))
	for _, candle := range candles {
		result = append(result, &BitfinexCandle{
			OpenTime:  candle.Time,
			Open:      candle.Open,
			High:      candle.High,
			Low:       candle.Low,
			Close:     candle.Close,
			Volume:    candle.Volume,
			CloseTime: candle.Time,
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
	return a.client.GetFundingRate(ctx, a.symbol)
}

// convertToOrder 将 Bitfinex 订单转换为通用订单
func (a *Adapter) convertToOrder(orderInfo *OrderInfo) *Order {
	status := orderInfo.Status
	if status == "" {
		status = "NEW"
	}

	return &Order{
		OrderID:       orderInfo.OrderID,
		ClientOrderID: "",
		Symbol:        orderInfo.Symbol,
		Side:          "BUY",
		Type:          "LIMIT",
		Price:         orderInfo.Price,
		Quantity:      orderInfo.Amount,
		ExecutedQty:   0,
		Status:        status,
		CreatedAt:     time.Now(),
	}
}

// convertIntervalToResolution 将时间间隔转换为 Bitfinex 的 resolution
func convertIntervalToResolution(interval string) string {
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
	case "4h":
		return "4h"
	case "12h":
		return "12h"
	case "1d":
		return "1d"
	case "1w":
		return "1w"
	default:
		return "1h" // 默认 1 小时
	}
}

