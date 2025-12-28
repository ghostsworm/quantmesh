package btcc

import (
	"context"
	"fmt"
	"strings"

	"quantmesh/logger"
)

// Adapter BTCC 适配器
type Adapter struct {
	client           *BTCCClient
	wsManager        *WebSocketManager
	klineWSManager   *KlineWebSocketManager
	symbol           string
	priceDecimals    int
	quantityDecimals int
	baseAsset        string
	quoteAsset       string
}

// NewAdapter 创建 BTCC 适配器
func NewAdapter(config map[string]string, symbol string) (*Adapter, error) {
	apiKey := config["api_key"]
	secretKey := config["secret_key"]
	isTestnet := config["testnet"] == "true"

	if apiKey == "" || secretKey == "" {
		return nil, fmt.Errorf("BTCC API key or secret key is empty")
	}

	client := NewBTCCClient(apiKey, secretKey, isTestnet)

	// BTCC 符号格式：BTC-USDT
	btccSymbol := convertSymbolToBTCC(symbol)

	adapter := &Adapter{
		client:           client,
		symbol:           btccSymbol,
		priceDecimals:    2,
		quantityDecimals: 4,
		baseAsset:        "BTC",
		quoteAsset:       "USDT",
	}

	// 获取交易对信息
	ctx := context.Background()
	symbolInfo, err := client.GetSymbol(ctx, btccSymbol)
	if err != nil {
		logger.Warn("Failed to get BTCC symbol: %v", err)
	} else {
		adapter.priceDecimals = symbolInfo.PricePrecision
		adapter.quantityDecimals = symbolInfo.QuantityPrecision
		adapter.baseAsset = strings.ToUpper(symbolInfo.BaseCurrency)
		adapter.quoteAsset = strings.ToUpper(symbolInfo.QuoteCurrency)
	}

	return adapter, nil
}

// convertSymbolToBTCC 转换交易对格式：BTCUSDT -> BTC-USDT
func convertSymbolToBTCC(symbol string) string {
	symbol = strings.ToUpper(symbol)
	if strings.HasSuffix(symbol, "USDT") {
		base := strings.TrimSuffix(symbol, "USDT")
		return base + "-USDT"
	}
	return symbol
}

// GetName 获取交易所名称
func (a *Adapter) GetName() string {
	return "BTCC"
}

// PlaceOrder 下单
func (a *Adapter) PlaceOrder(ctx context.Context, side OrderSide, price, quantity float64, clientOrderID string) (*OrderLocal, error) {
	var btccSide string
	if side == SideBuy {
		btccSide = "BUY"
	} else {
		btccSide = "SELL"
	}

	req := &OrderRequest{
		Symbol:        a.symbol,
		Side:          btccSide,
		Type:          "LIMIT",
		Quantity:      quantity,
		Price:         price,
		ClientOrderID: clientOrderID,
	}

	order, err := a.client.PlaceOrder(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("BTCC place order error: %w", err)
	}

	return a.convertOrder(order), nil
}

// CancelOrder 取消订单
func (a *Adapter) CancelOrder(ctx context.Context, orderID int64) error {
	return a.client.CancelOrder(ctx, a.symbol, orderID)
}

// GetOrder 查询订单
func (a *Adapter) GetOrder(ctx context.Context, orderID int64) (*OrderLocal, error) {
	order, err := a.client.GetOrder(ctx, a.symbol, orderID)
	if err != nil {
		return nil, err
	}

	return a.convertOrder(order), nil
}

// GetOpenOrders 获取活跃订单
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

// GetAccount 获取账户信息
func (a *Adapter) GetAccount(ctx context.Context) (*AccountLocal, error) {
	balance, err := a.client.GetBalance(ctx)
	if err != nil {
		return nil, err
	}

	return &AccountLocal{
		TotalWalletBalance: balance.TotalBalance,
		TotalMarginBalance: balance.TotalBalance,
		AvailableBalance:   balance.AvailableBalance,
	}, nil
}

// GetPositions 获取持仓
func (a *Adapter) GetPositions(ctx context.Context) ([]*PositionLocal, error) {
	positions, err := a.client.GetPositions(ctx, a.symbol)
	if err != nil {
		return nil, err
	}

	result := make([]*PositionLocal, 0, len(positions))
	for _, pos := range positions {
		result = append(result, &PositionLocal{
			Symbol:        pos.Symbol,
			Side:          pos.Side,
			Size:          pos.Size,
			EntryPrice:    pos.EntryPrice,
			MarkPrice:     pos.MarkPrice,
			UnrealizedPNL: pos.UnrealizedPNL,
			Leverage:      pos.Leverage,
		})
	}

	return result, nil
}

// GetBalance 获取余额
func (a *Adapter) GetBalance(ctx context.Context) (float64, error) {
	balance, err := a.client.GetBalance(ctx)
	if err != nil {
		return 0, err
	}

	return balance.AvailableBalance, nil
}

// StartOrderStream 启动订单流
func (a *Adapter) StartOrderStream(ctx context.Context, callback func(interface{})) error {
	if a.wsManager != nil {
		return fmt.Errorf("order stream already started")
	}

	a.wsManager = NewWebSocketManager(a.client.apiKey, a.client.secretKey, a.client.isTestnet)
	return a.wsManager.Start(ctx, a.symbol, callback)
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
func (a *Adapter) GetLatestPrice(ctx context.Context) (float64, error) {
	ticker, err := a.client.GetTicker(ctx, a.symbol)
	if err != nil {
		return 0, err
	}

	return ticker.Price, nil
}

// StartKlineStream 启动 K线流
func (a *Adapter) StartKlineStream(ctx context.Context, interval string, callback CandleUpdateCallbackLocal) error {
	if a.klineWSManager != nil {
		return fmt.Errorf("kline stream already started")
	}

	btccInterval := string(ConvertInterval(interval))
	a.klineWSManager = NewKlineWebSocketManager(a.client.isTestnet)

	return a.klineWSManager.Start(ctx, a.symbol, btccInterval, func(kline *Kline) {
		candle := &CandleLocal{
			Symbol:    a.symbol,
			Timestamp: kline.Time,
			Open:      kline.Open,
			High:      kline.High,
			Low:       kline.Low,
			Close:     kline.Close,
			Volume:    kline.Volume,
		}
		callback(candle)
	})
}

// StopKlineStream 停止 K线流
func (a *Adapter) StopKlineStream() error {
	if a.klineWSManager != nil {
		a.klineWSManager.Stop()
		a.klineWSManager = nil
	}
	return nil
}

// GetHistoricalKlines 获取历史 K线
func (a *Adapter) GetHistoricalKlines(ctx context.Context, interval string, limit int) ([]*CandleLocal, error) {
	btccInterval := string(ConvertInterval(interval))
	klines, err := a.client.GetKlines(ctx, a.symbol, btccInterval, limit)
	if err != nil {
		return nil, err
	}

	result := make([]*CandleLocal, 0, len(klines))
	for _, kline := range klines {
		result = append(result, &CandleLocal{
			Symbol:    a.symbol,
			Timestamp: kline.Time,
			Open:      kline.Open,
			High:      kline.High,
			Low:       kline.Low,
			Close:     kline.Close,
			Volume:    kline.Volume,
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
func (a *Adapter) GetFundingRate(ctx context.Context) (float64, error) {
	// BTCC 暂不支持，返回 0
	return 0, nil
}

// convertOrder 转换订单
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

	return &OrderLocal{
		OrderID:       order.OrderID,
		ClientOrderID: order.ClientOrderID,
		Symbol:        order.Symbol,
		Side:          side,
		Price:         order.Price,
		Quantity:      order.Quantity,
		ExecutedQty:   order.ExecutedQty,
		Status:        status,
		UpdateTime:    order.UpdateTime,
	}
}

