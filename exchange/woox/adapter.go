package woox

import (
	"context"
	"fmt"
	"strings"

	"quantmesh/logger"
)

// Adapter WOO X 适配器
type Adapter struct {
	client           *WOOXClient
	wsManager        *WebSocketManager
	klineWSManager   *KlineWebSocketManager
	symbol           string
	priceDecimals    int
	quantityDecimals int
	baseAsset        string
	quoteAsset       string
}

// NewAdapter 创建 WOO X 适配器
func NewAdapter(config map[string]string, symbol string) (*Adapter, error) {
	apiKey := config["api_key"]
	secretKey := config["secret_key"]
	isTestnet := config["testnet"] == "true"

	if apiKey == "" || secretKey == "" {
		return nil, fmt.Errorf("WOO X API key or secret key is empty")
	}

	client := NewWOOXClient(apiKey, secretKey, isTestnet)

	// WOO X 符号格式：PERP_BTC_USDT
	wooxSymbol := convertSymbolToWOOX(symbol)

	adapter := &Adapter{
		client:           client,
		symbol:           wooxSymbol,
		priceDecimals:    2,
		quantityDecimals: 3,
		baseAsset:        "BTC",
		quoteAsset:       "USDT",
	}

	// 获取交易对信息
	ctx := context.Background()
	symbolInfo, err := client.GetSymbol(ctx, wooxSymbol)
	if err != nil {
		logger.Warn("Failed to get WOO X symbol: %v", err)
	} else {
		adapter.priceDecimals = symbolInfo.PrecisionPrice
		adapter.quantityDecimals = symbolInfo.PrecisionQuantity

		// 解析基础资产和报价资产
		parts := strings.Split(strings.TrimPrefix(symbolInfo.Symbol, "PERP_"), "_")
		if len(parts) >= 2 {
			adapter.baseAsset = parts[0]
			adapter.quoteAsset = parts[1]
		}
	}

	return adapter, nil
}

// convertSymbolToWOOX 转换交易对格式：BTCUSDT -> PERP_BTC_USDT
func convertSymbolToWOOX(symbol string) string {
	symbol = strings.ToUpper(symbol)
	if strings.HasSuffix(symbol, "USDT") {
		base := strings.TrimSuffix(symbol, "USDT")
		return fmt.Sprintf("PERP_%s_USDT", base)
	}
	return symbol
}

// GetName 获取交易所名称
func (a *Adapter) GetName() string {
	return "WOO X"
}

// PlaceOrder 下单
func (a *Adapter) PlaceOrder(ctx context.Context, side OrderSide, price, quantity float64, clientOrderID string) (*OrderLocal, error) {
	var wooxSide string
	if side == SideBuy {
		wooxSide = "BUY"
	} else {
		wooxSide = "SELL"
	}

	req := &OrderRequest{
		Symbol:        a.symbol,
		Side:          wooxSide,
		OrderType:     "LIMIT",
		OrderPrice:    price,
		OrderQuantity: quantity,
		ClientOrderID: clientOrderID,
	}

	order, err := a.client.PlaceOrder(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("WOO X place order error: %w", err)
	}

	return a.convertOrder(order), nil
}

// CancelOrder 取消订单
func (a *Adapter) CancelOrder(ctx context.Context, orderID int64) error {
	return a.client.CancelOrder(ctx, a.symbol, orderID)
}

// GetOrder 查询订单
func (a *Adapter) GetOrder(ctx context.Context, orderID int64) (*OrderLocal, error) {
	order, err := a.client.GetOrder(ctx, orderID)
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
	account, err := a.client.GetAccount(ctx)
	if err != nil {
		return nil, err
	}

	return &AccountLocal{
		TotalWalletBalance: account.TotalAccountValue,
		TotalMarginBalance: account.TotalCollateral,
		AvailableBalance:   account.TotalAccountValue - account.TotalCollateral,
	}, nil
}

// GetPositions 获取持仓
func (a *Adapter) GetPositions(ctx context.Context) ([]*PositionLocal, error) {
	position, err := a.client.GetPosition(ctx, a.symbol)
	if err != nil {
		return nil, err
	}

	if position.Holding == 0 {
		return []*PositionLocal{}, nil
	}

	return []*PositionLocal{
		{
			Symbol:        position.Symbol,
			Size:          position.Holding,
			EntryPrice:    position.AverageOpenPrice,
			MarkPrice:     position.MarkPrice,
			UnrealizedPNL: position.UnrealizedPNL,
			Leverage:      position.Leverage,
		},
	}, nil
}

// GetBalance 获取余额
func (a *Adapter) GetBalance(ctx context.Context) (float64, error) {
	account, err := a.client.GetAccount(ctx)
	if err != nil {
		return 0, err
	}

	return account.TotalAccountValue - account.TotalCollateral, nil
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
	trades, err := a.client.GetTrades(ctx, a.symbol, 1)
	if err != nil {
		return 0, err
	}

	if len(trades) == 0 {
		return 0, fmt.Errorf("no trades found")
	}

	return trades[0].Price, nil
}

// StartKlineStream 启动 K线流
func (a *Adapter) StartKlineStream(ctx context.Context, interval string, callback CandleUpdateCallbackLocal) error {
	if a.klineWSManager != nil {
		return fmt.Errorf("kline stream already started")
	}

	wooxInterval := string(ConvertInterval(interval))
	a.klineWSManager = NewKlineWebSocketManager(a.client.isTestnet)

	return a.klineWSManager.Start(ctx, a.symbol, wooxInterval, func(kline *Kline) {
		candle := &CandleLocal{
			Symbol:    kline.Symbol,
			Timestamp: kline.StartTimestamp,
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
	wooxInterval := string(ConvertInterval(interval))
	klines, err := a.client.GetKlines(ctx, a.symbol, wooxInterval, limit)
	if err != nil {
		return nil, err
	}

	result := make([]*CandleLocal, 0, len(klines))
	for _, kline := range klines {
		result = append(result, &CandleLocal{
			Symbol:    kline.Symbol,
			Timestamp: kline.StartTimestamp,
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
	// WOO X 资金费率需要单独查询，这里返回 0
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
	case "PARTIAL_FILLED":
		status = OrderStatusPartiallyFilled
	case "FILLED":
		status = OrderStatusFilled
	case "CANCELLED":
		status = OrderStatusCanceled
	default:
		status = OrderStatusNew
	}

	return &OrderLocal{
		OrderID:       order.OrderID,
		ClientOrderID: order.ClientOrderID,
		Symbol:        order.Symbol,
		Side:          side,
		Price:         order.OrderPrice,
		Quantity:      order.OrderQuantity,
		ExecutedQty:   order.ExecutedQuantity,
		Status:        status,
		UpdateTime:    order.UpdatedTime,
	}
}
