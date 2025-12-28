package bingx

import (
	"context"
	"fmt"
	"strings"

	"quantmesh/logger"
)

// Adapter BingX 适配器
type Adapter struct {
	client          *BingXClient
	wsManager       *WebSocketManager
	klineWSManager  *KlineWebSocketManager
	symbol          string
	priceDecimals   int
	quantityDecimals int
	baseAsset       string
	quoteAsset      string
}

// NewAdapter 创建 BingX 适配器
func NewAdapter(config map[string]string, symbol string) (*Adapter, error) {
	apiKey := config["api_key"]
	secretKey := config["secret_key"]
	isTestnet := config["testnet"] == "true"

	if apiKey == "" || secretKey == "" {
		return nil, fmt.Errorf("BingX API key or secret key is empty")
	}

	client := NewBingXClient(apiKey, secretKey, isTestnet)

	// 解析交易对
	parts := strings.Split(symbol, "USDT")
	baseAsset := "BTC"
	if len(parts) > 0 && parts[0] != "" {
		baseAsset = parts[0]
	}

	adapter := &Adapter{
		client:           client,
		symbol:           convertSymbolToBingX(symbol),
		priceDecimals:    2,
		quantityDecimals: 3,
		baseAsset:        baseAsset,
		quoteAsset:       "USDT",
	}

	// 获取交易对信息
	ctx := context.Background()
	exchangeInfo, err := client.GetExchangeInfo(ctx)
	if err != nil {
		logger.Warn("Failed to get BingX exchange info: %v", err)
	} else {
		if detail, ok := exchangeInfo.Symbols[adapter.symbol]; ok {
			adapter.priceDecimals = detail.PricePrecision
			adapter.quantityDecimals = detail.QuantityPrecision
		}
	}

	return adapter, nil
}

// convertSymbolToBingX 转换交易对格式：BTCUSDT -> BTC-USDT
func convertSymbolToBingX(symbol string) string {
	if strings.Contains(symbol, "-") {
		return symbol
	}
	// BTCUSDT -> BTC-USDT
	if strings.HasSuffix(symbol, "USDT") {
		base := strings.TrimSuffix(symbol, "USDT")
		return base + "-USDT"
	}
	return symbol
}

// convertSymbolFromBingX 转换交易对格式：BTC-USDT -> BTCUSDT
func convertSymbolFromBingX(symbol string) string {
	return strings.ReplaceAll(symbol, "-", "")
}

// GetName 获取交易所名称
func (a *Adapter) GetName() string {
	return "BingX"
}

// PlaceOrder 下单
func (a *Adapter) PlaceOrder(ctx context.Context, side OrderSide, price, quantity float64, clientOrderID string) (*OrderLocal, error) {
	// 转换订单方向
	var bingxSide string
	var positionSide string
	
	if side == SideBuy {
		bingxSide = "BUY"
		positionSide = "LONG"
	} else {
		bingxSide = "SELL"
		positionSide = "SHORT"
	}

	// 构造 BingX 订单请求
	bingxReq := &OrderRequest{
		Symbol:        a.symbol,
		Side:          bingxSide,
		PositionSide:  positionSide,
		Type:          "LIMIT",
		Price:         price,
		Quantity:      quantity,
		ClientOrderID: clientOrderID,
	}

	resp, err := a.client.PlaceOrder(ctx, bingxReq)
	if err != nil {
		return nil, fmt.Errorf("BingX place order error: %w", err)
	}

	return &OrderLocal{
		OrderID:       resp.OrderID,
		ClientOrderID: clientOrderID,
		Symbol:        convertSymbolFromBingX(a.symbol),
		Side:          side,
		Price:         price,
		Quantity:      quantity,
		Status:        OrderStatusNew,
	}, nil
}

// CancelOrder 取消订单
func (a *Adapter) CancelOrder(ctx context.Context, orderID int64) error {
	return a.client.CancelOrder(ctx, a.symbol, orderID)
}

// GetOrder 查询订单
func (a *Adapter) GetOrder(ctx context.Context, orderID int64) (*OrderLocal, error) {
	orderInfo, err := a.client.GetOrderInfo(ctx, a.symbol, orderID)
	if err != nil {
		return nil, err
	}

	return a.convertOrder(orderInfo), nil
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
	accountInfo, err := a.client.GetAccount(ctx)
	if err != nil {
		return nil, err
	}

	return &AccountLocal{
		TotalWalletBalance: accountInfo.Balance.Balance,
		TotalMarginBalance: accountInfo.Balance.Balance - accountInfo.Balance.UnrealizedProfit,
		AvailableBalance:   accountInfo.Balance.AvailableMargin,
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
		if pos.PositionAmt == 0 {
			continue
		}

		size := pos.PositionAmt
		if pos.PositionSide == "SHORT" {
			size = -size // 空仓用负数表示
		}

		result = append(result, &PositionLocal{
			Symbol:        convertSymbolFromBingX(pos.Symbol),
			Size:          size,
			EntryPrice:    pos.AvgPrice,
			MarkPrice:     pos.MarkPrice,
			UnrealizedPNL: pos.UnrealizedProfit,
			Leverage:      pos.Leverage,
		})
	}

	return result, nil
}

// GetBalance 获取余额
func (a *Adapter) GetBalance(ctx context.Context) (float64, error) {
	accountInfo, err := a.client.GetAccount(ctx)
	if err != nil {
		return 0, err
	}

	return accountInfo.Balance.AvailableMargin, nil
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

	return ticker.LastPrice, nil
}

// StartKlineStream 启动 K线流
func (a *Adapter) StartKlineStream(ctx context.Context, interval string, callback CandleUpdateCallbackLocal) error {
	if a.klineWSManager != nil {
		return fmt.Errorf("kline stream already started")
	}

	bingxInterval := string(ConvertInterval(interval))
	a.klineWSManager = NewKlineWebSocketManager(a.client.isTestnet)

	return a.klineWSManager.Start(ctx, a.symbol, bingxInterval, func(kline *Kline) {
		candle := &CandleLocal{
			Symbol:    convertSymbolFromBingX(a.symbol),
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
	bingxInterval := string(ConvertInterval(interval))
	klines, err := a.client.GetKlines(ctx, a.symbol, bingxInterval, limit)
	if err != nil {
		return nil, err
	}

	result := make([]*CandleLocal, 0, len(klines))
	for _, kline := range klines {
		result = append(result, &CandleLocal{
			Symbol:    convertSymbolFromBingX(a.symbol),
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
	ticker, err := a.client.GetTicker(ctx, a.symbol)
	if err != nil {
		return 0, err
	}

	return ticker.FundingRate, nil
}

// convertOrder 转换订单
func (a *Adapter) convertOrder(order *OrderInfo) *OrderLocal {
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
	case "CANCELED", "EXPIRED":
		status = OrderStatusCanceled
	default:
		status = OrderStatusNew
	}

	return &OrderLocal{
		OrderID:       order.OrderID,
		ClientOrderID: order.ClientOrderID,
		Symbol:        convertSymbolFromBingX(order.Symbol),
		Side:          side,
		Price:         order.Price,
		Quantity:      order.Quantity,
		ExecutedQty:   order.ExecutedQty,
		Status:        status,
		UpdateTime:    order.UpdateTime,
	}
}

