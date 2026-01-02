package bitrue

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"quantmesh/logger"
)

// Adapter Bitrue 适配器
type Adapter struct {
	client           *BitrueClient
	wsManager        *WebSocketManager
	klineWSManager   *KlineWebSocketManager
	symbol           string
	priceDecimals    int
	quantityDecimals int
	baseAsset        string
	quoteAsset       string
}

// NewAdapter 创建 Bitrue 适配器
func NewAdapter(config map[string]string, symbol string) (*Adapter, error) {
	apiKey := config["api_key"]
	secretKey := config["secret_key"]
	isTestnet := config["testnet"] == "true"

	if apiKey == "" || secretKey == "" {
		return nil, fmt.Errorf("Bitrue API key or secret key is empty")
	}

	client := NewBitrueClient(apiKey, secretKey, isTestnet)

	// Bitrue 符号格式：BTCUSDT
	bitrueSymbol := convertSymbolToBitrue(symbol)

	adapter := &Adapter{
		client:           client,
		symbol:           bitrueSymbol,
		priceDecimals:    2,
		quantityDecimals: 4,
		baseAsset:        "BTC",
		quoteAsset:       "USDT",
	}

	// 获取交易对信息
	ctx := context.Background()
	exchangeInfo, err := client.GetExchangeInfo(ctx)
	if err != nil {
		logger.Warn("Failed to get Bitrue exchange info: %v", err)
	} else {
		for _, s := range exchangeInfo.Symbols {
			if s.Symbol == bitrueSymbol {
				adapter.priceDecimals = s.QuoteAssetPrecision
				adapter.quantityDecimals = s.BaseAssetPrecision
				adapter.baseAsset = s.BaseAsset
				adapter.quoteAsset = s.QuoteAsset
				break
			}
		}
	}

	return adapter, nil
}

// convertSymbolToBitrue 转换交易对格式：BTCUSDT -> BTCUSDT
func convertSymbolToBitrue(symbol string) string {
	return strings.ToUpper(symbol)
}

// GetName 获取交易所名称
func (a *Adapter) GetName() string {
	return "Bitrue"
}

// PlaceOrder 下单
func (a *Adapter) PlaceOrder(ctx context.Context, side OrderSide, price, quantity float64, clientOrderID string) (*OrderLocal, error) {
	var bitrueSide string
	if side == SideBuy {
		bitrueSide = "BUY"
	} else {
		bitrueSide = "SELL"
	}

	req := &OrderRequest{
		Symbol:           a.symbol,
		Side:             bitrueSide,
		Type:             "LIMIT",
		Quantity:         quantity,
		Price:            price,
		NewClientOrderID: clientOrderID,
	}

	order, err := a.client.PlaceOrder(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("Bitrue place order error: %w", err)
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
	account, err := a.client.GetAccount(ctx)
	if err != nil {
		return nil, err
	}

	// 计算总余额（USDT）
	totalBalance := 0.0
	availableBalance := 0.0

	for _, balance := range account.Balances {
		if balance.Asset == "USDT" {
			if free, err := strconv.ParseFloat(balance.Free, 64); err == nil {
				availableBalance = free
				totalBalance += free
			}
			if locked, err := strconv.ParseFloat(balance.Locked, 64); err == nil {
				totalBalance += locked
			}
			break
		}
	}

	return &AccountLocal{
		TotalWalletBalance: totalBalance,
		TotalMarginBalance: totalBalance,
		AvailableBalance:   availableBalance,
	}, nil
}

// GetPositions 获取持仓（Bitrue 现货交易所，返回空）
func (a *Adapter) GetPositions(ctx context.Context) ([]*PositionLocal, error) {
	return []*PositionLocal{}, nil
}

// GetBalance 获取余额
func (a *Adapter) GetBalance(ctx context.Context) (float64, error) {
	account, err := a.client.GetAccount(ctx)
	if err != nil {
		return 0, err
	}

	for _, balance := range account.Balances {
		if balance.Asset == "USDT" {
			if free, err := strconv.ParseFloat(balance.Free, 64); err == nil {
				return free, nil
			}
		}
	}

	return 0, nil
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

	price, err := strconv.ParseFloat(ticker.Price, 64)
	if err != nil {
		return 0, err
	}

	return price, nil
}

// StartKlineStream 启动 K线流
func (a *Adapter) StartKlineStream(ctx context.Context, interval string, callback CandleUpdateCallbackLocal) error {
	if a.klineWSManager != nil {
		return fmt.Errorf("kline stream already started")
	}

	bitrueInterval := string(ConvertInterval(interval))
	a.klineWSManager = NewKlineWebSocketManager(a.client.isTestnet)

	return a.klineWSManager.Start(ctx, a.symbol, bitrueInterval, func(kline *Kline) {
		open, _ := strconv.ParseFloat(kline.Open, 64)
		high, _ := strconv.ParseFloat(kline.High, 64)
		low, _ := strconv.ParseFloat(kline.Low, 64)
		close, _ := strconv.ParseFloat(kline.Close, 64)
		volume, _ := strconv.ParseFloat(kline.Volume, 64)

		candle := &CandleLocal{
			Symbol:    a.symbol,
			Timestamp: kline.OpenTime,
			Open:      open,
			High:      high,
			Low:       low,
			Close:     close,
			Volume:    volume,
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
	bitrueInterval := string(ConvertInterval(interval))
	klines, err := a.client.GetKlines(ctx, a.symbol, bitrueInterval, limit)
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
			Timestamp: kline.OpenTime,
			Open:      open,
			High:      high,
			Low:       low,
			Close:     close,
			Volume:    volume,
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

// GetFundingRate 获取资金费率（Bitrue 现货交易所，返回 0）
func (a *Adapter) GetFundingRate(ctx context.Context) (float64, error) {
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

	price, _ := strconv.ParseFloat(order.Price, 64)
	origQty, _ := strconv.ParseFloat(order.OrigQty, 64)
	executedQty, _ := strconv.ParseFloat(order.ExecutedQty, 64)

	return &OrderLocal{
		OrderID:       order.OrderID,
		ClientOrderID: order.ClientOrderID,
		Symbol:        order.Symbol,
		Side:          side,
		Price:         price,
		Quantity:      origQty,
		ExecutedQty:   executedQty,
		Status:        status,
		UpdateTime:    order.UpdateTime,
	}
}
