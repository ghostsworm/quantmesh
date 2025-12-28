package kucoin

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"quantmesh/logger"
)

// Adapter KuCoin 适配器
type Adapter struct {
	client            *KuCoinClient
	wsManager         *WebSocketManager
	klineWSManager    *KlineWebSocketManager
	symbol            string
	priceDecimals     int
	quantityDecimals  int
	baseAsset         string
	quoteAsset        string
}

// NewKuCoinAdapter 创建 KuCoin 适配器
func NewKuCoinAdapter(config map[string]string, symbol string) (*Adapter, error) {
	apiKey := config["api_key"]
	secretKey := config["secret_key"]
	passphrase := config["passphrase"]

	if apiKey == "" || secretKey == "" || passphrase == "" {
		return nil, fmt.Errorf("KuCoin API key, secret key, and passphrase are required")
	}

	client := NewKuCoinClient(apiKey, secretKey, passphrase)

	// 解析交易对：KuCoin 使用 BTCUSDT 格式，需要转换为 BTC-USDT
	var parts []string
	if strings.Contains(symbol, "-") {
		parts = strings.Split(symbol, "-")
	} else {
		// 尝试解析 BTCUSDT 格式
		if strings.HasSuffix(symbol, "USDT") {
			base := strings.TrimSuffix(symbol, "USDT")
			parts = []string{base, "USDT"}
			symbol = base + "-USDT" // 转换为 KuCoin 格式
		} else {
			return nil, fmt.Errorf("invalid symbol format: %s, expected format: BTC-USDT or BTCUSDT", symbol)
		}
	}

	if len(parts) != 2 {
		return nil, fmt.Errorf("invalid symbol format: %s, expected format: BTC-USDT", symbol)
	}

	adapter := &Adapter{
		client:           client,
		symbol:           symbol,
		priceDecimals:    2,
		quantityDecimals: 0, // KuCoin 期货使用整数张数
		baseAsset:        parts[0],
		quoteAsset:       parts[1],
	}

	// 获取交易对精度信息
	ctx := context.Background()
	exchangeInfo, err := client.GetExchangeInfo(ctx)
	if err != nil {
		logger.Warn("Failed to get KuCoin exchange info: %v", err)
	} else {
		if info, exists := exchangeInfo.Symbols[symbol]; exists {
			// 根据 tickSize 计算价格精度
			tickSize := info.TickSize
			if tickSize > 0 {
				adapter.priceDecimals = getPrecision(tickSize)
			}
			logger.Info("KuCoin symbol %s precision: price=%d, quantity=%d", symbol, adapter.priceDecimals, adapter.quantityDecimals)
		}
	}

	return adapter, nil
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
	return "kucoin"
}

// PlaceOrder 下单
func (a *Adapter) PlaceOrder(ctx context.Context, req *KuCoinOrderRequest) (*Order, error) {
	clientOrderID := fmt.Sprintf("order_%d", req.Timestamp)
	
	orderReq := &OrderRequest{
		ClientOrderID:    clientOrderID,
		Symbol:           a.symbol,
		Side:             strings.ToLower(string(req.Side)),
		Type:             strings.ToLower(string(req.Type)),
		Price:            req.Price,
		Quantity:         req.Quantity,
		Leverage:         int(req.Leverage),
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

	logger.Info("KuCoin order placed: %s, side: %s, price: %.2f, quantity: %.2f", order.OrderID, order.Side, order.Price, order.Quantity)
	return order, nil
}

// BatchPlaceOrders 批量下单
func (a *Adapter) BatchPlaceOrders(ctx context.Context, orders []*KuCoinOrderRequest) ([]*Order, bool) {
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

	logger.Info("KuCoin order cancelled: %s", orderIDStr)
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
		if _, err := a.client.CancelOrder(ctx, order.ID); err != nil {
			logger.Error("Cancel order %s failed: %v", order.ID, err)
		}
	}

	logger.Info("KuCoin all orders cancelled for symbol: %s", symbol)
	return nil
}

// GetOrder 查询订单
func (a *Adapter) GetOrder(ctx context.Context, symbol string, orderID int64) (*Order, error) {
	orderIDStr := strconv.FormatInt(orderID, 10)
	orderInfo, err := a.client.GetOrderInfo(ctx, orderIDStr)
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
		TotalBalance:     accountInfo.AccountEquity,
		AvailableBalance: accountInfo.AvailableBalance,
		UnrealizedPnL:    accountInfo.UnrealisedPNL,
		MarginBalance:    accountInfo.MarginBalance,
	}

	return account, nil
}

// GetPositions 获取持仓信息
func (a *Adapter) GetPositions(ctx context.Context, symbol string) ([]*Position, error) {
	positions, err := a.client.GetPositionInfo(ctx, symbol)
	if err != nil {
		return nil, fmt.Errorf("get positions error: %w", err)
	}

	result := make([]*Position, 0, len(positions))
	for _, pos := range positions {
		if !pos.IsOpen {
			continue
		}

		side := "LONG"
		if pos.CurrentQty < 0 {
			side = "SHORT"
		}

		position := &Position{
			Symbol:           pos.Symbol,
			Side:             side,
			Size:             float64(abs(pos.CurrentQty)),
			EntryPrice:       pos.AvgEntryPrice,
			MarkPrice:        pos.MarkPrice,
			UnrealizedPnL:    pos.UnrealisedPnl,
			Leverage:         pos.RealLeverage,
			LiquidationPrice: pos.LiquidationPrice,
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

	return accountInfo.AvailableBalance, nil
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
	// KuCoin 没有单独的获取最新价格的 API，这里通过持仓信息获取标记价格
	positions, err := a.client.GetPositionInfo(ctx, symbol)
	if err != nil {
		return 0, fmt.Errorf("get latest price error: %w", err)
	}

	if len(positions) > 0 {
		return positions[0].MarkPrice, nil
	}

	return 0, fmt.Errorf("no position found for symbol: %s", symbol)
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
func (a *Adapter) GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*KuCoinCandle, error) {
	granularity := convertIntervalToGranularity(interval)
	candles, err := a.client.GetHistoricalKlines(ctx, symbol, granularity, limit)
	if err != nil {
		return nil, fmt.Errorf("get historical klines error: %w", err)
	}

	result := make([]*KuCoinCandle, 0, len(candles))
	for _, candle := range candles {
		result = append(result, &KuCoinCandle{
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
	return a.client.GetFundingRate(ctx, symbol)
}

// convertToOrder 将 KuCoin 订单转换为通用订单
func (a *Adapter) convertToOrder(orderInfo *OrderInfo) *Order {
	price, _ := strconv.ParseFloat(orderInfo.Price, 64)
	
	status := "NEW"
	if orderInfo.IsActive {
		status = "PARTIALLY_FILLED"
	}
	if orderInfo.Status == "done" {
		status = "FILLED"
	}
	if orderInfo.CancelExist {
		status = "CANCELED"
	}

	return &Order{
		OrderID:       orderInfo.ID,
		ClientOrderID: orderInfo.ClientOid,
		Symbol:        orderInfo.Symbol,
		Side:          strings.ToUpper(orderInfo.Side),
		Type:          strings.ToUpper(orderInfo.Type),
		Price:         price,
		Quantity:      float64(orderInfo.Size),
		ExecutedQty:   float64(orderInfo.FilledSize),
		Status:        status,
		Timestamp:     orderInfo.CreatedAt,
	}
}

// convertIntervalToGranularity 将时间间隔转换为 KuCoin 的 granularity
func convertIntervalToGranularity(interval string) int {
	switch interval {
	case "1m":
		return 1
	case "5m":
		return 5
	case "15m":
		return 15
	case "30m":
		return 30
	case "1h":
		return 60
	case "2h":
		return 120
	case "4h":
		return 240
	case "8h":
		return 480
	case "12h":
		return 720
	case "1d":
		return 1440
	case "1w":
		return 10080
	default:
		return 60 // 默认 1 小时
	}
}

// abs 返回绝对值
func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
