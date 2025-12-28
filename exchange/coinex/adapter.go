package coinex

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"quantmesh/logger"
)

// Adapter CoinEx 适配器
type Adapter struct {
	client           *CoinExClient
	wsManager        *WebSocketManager
	klineWSManager   *KlineWebSocketManager
	market           string
	priceDecimals    int
	quantityDecimals int
	baseAsset        string
	quoteAsset       string
}

// NewAdapter 创建 CoinEx 适配器
func NewAdapter(config map[string]string, symbol string) (*Adapter, error) {
	apiKey := config["api_key"]
	secretKey := config["secret_key"]
	isTestnet := config["testnet"] == "true"

	if apiKey == "" || secretKey == "" {
		return nil, fmt.Errorf("CoinEx API key or secret key is empty")
	}

	client := NewCoinExClient(apiKey, secretKey, isTestnet)

	// CoinEx 市场格式：BTCUSDT
	coinexMarket := convertSymbolToCoinEx(symbol)

	adapter := &Adapter{
		client:           client,
		market:           coinexMarket,
		priceDecimals:    2,
		quantityDecimals: 4,
		baseAsset:        "BTC",
		quoteAsset:       "USDT",
	}

	// 获取市场信息
	ctx := context.Background()
	marketInfo, err := client.GetMarket(ctx, coinexMarket)
	if err != nil {
		logger.Warn("Failed to get CoinEx market: %v", err)
	} else {
		adapter.priceDecimals = marketInfo.PricingDecimal
		adapter.quantityDecimals = marketInfo.TradingDecimal
		adapter.baseAsset = marketInfo.TradingName
		adapter.quoteAsset = marketInfo.PricingName
	}

	return adapter, nil
}

// convertSymbolToCoinEx 转换交易对格式：BTCUSDT -> BTCUSDT
func convertSymbolToCoinEx(symbol string) string {
	return strings.ToUpper(symbol)
}

// GetName 获取交易所名称
func (a *Adapter) GetName() string {
	return "CoinEx"
}

// PlaceOrder 下单
func (a *Adapter) PlaceOrder(ctx context.Context, side OrderSide, price, quantity float64, clientOrderID string) (*OrderLocal, error) {
	var coinexSide string
	if side == SideBuy {
		coinexSide = "buy"
	} else {
		coinexSide = "sell"
	}

	req := &OrderRequest{
		Market:   a.market,
		Type:     "limit",
		Side:     coinexSide,
		Amount:   quantity,
		Price:    price,
		ClientID: clientOrderID,
	}

	order, err := a.client.PlaceOrder(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("CoinEx place order error: %w", err)
	}

	return a.convertOrder(order), nil
}

// CancelOrder 取消订单
func (a *Adapter) CancelOrder(ctx context.Context, orderID int64) error {
	return a.client.CancelOrder(ctx, a.market, orderID)
}

// GetOrder 查询订单
func (a *Adapter) GetOrder(ctx context.Context, orderID int64) (*OrderLocal, error) {
	order, err := a.client.GetOrder(ctx, a.market, orderID)
	if err != nil {
		return nil, err
	}

	return a.convertOrder(order), nil
}

// GetOpenOrders 获取活跃订单
func (a *Adapter) GetOpenOrders(ctx context.Context) ([]*OrderLocal, error) {
	orders, err := a.client.GetOpenOrders(ctx, a.market, 1, 100)
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

	// 计算总余额（USDT）
	totalBalance := 0.0
	availableBalance := 0.0

	if usdtAvailable, ok := balance.Available["USDT"]; ok {
		if val, err := strconv.ParseFloat(usdtAvailable, 64); err == nil {
			availableBalance = val
			totalBalance += val
		}
	}

	if usdtFrozen, ok := balance.Frozen["USDT"]; ok {
		if val, err := strconv.ParseFloat(usdtFrozen, 64); err == nil {
			totalBalance += val
		}
	}

	return &AccountLocal{
		TotalWalletBalance: totalBalance,
		TotalMarginBalance: totalBalance,
		AvailableBalance:   availableBalance,
	}, nil
}

// GetPositions 获取持仓（CoinEx 现货交易所，返回空）
func (a *Adapter) GetPositions(ctx context.Context) ([]*PositionLocal, error) {
	return []*PositionLocal{}, nil
}

// GetBalance 获取余额
func (a *Adapter) GetBalance(ctx context.Context) (float64, error) {
	balance, err := a.client.GetBalance(ctx)
	if err != nil {
		return 0, err
	}

	if usdtAvailable, ok := balance.Available["USDT"]; ok {
		if val, err := strconv.ParseFloat(usdtAvailable, 64); err == nil {
			return val, nil
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
	return a.wsManager.Start(ctx, a.market, callback)
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
	trades, err := a.client.GetTrades(ctx, a.market, 1)
	if err != nil {
		return 0, err
	}

	if len(trades) == 0 {
		return 0, fmt.Errorf("no trades found")
	}

	price, err := strconv.ParseFloat(trades[0].Price, 64)
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

	period := string(ConvertInterval(interval))
	a.klineWSManager = NewKlineWebSocketManager(a.client.isTestnet)

	return a.klineWSManager.Start(ctx, a.market, period, func(kline *Kline) {
		open, _ := strconv.ParseFloat(kline.Open, 64)
		high, _ := strconv.ParseFloat(kline.High, 64)
		low, _ := strconv.ParseFloat(kline.Low, 64)
		close, _ := strconv.ParseFloat(kline.Close, 64)
		volume, _ := strconv.ParseFloat(kline.Volume, 64)

		candle := &CandleLocal{
			Symbol:    kline.Market,
			Timestamp: kline.Timestamp * 1000, // 转换为毫秒
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
	period := string(ConvertInterval(interval))
	klines, err := a.client.GetKlines(ctx, a.market, period, limit)
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
			Symbol:    kline.Market,
			Timestamp: kline.Timestamp * 1000,
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

// GetFundingRate 获取资金费率（CoinEx 现货交易所，返回 0）
func (a *Adapter) GetFundingRate(ctx context.Context) (float64, error) {
	return 0, nil
}

// convertOrder 转换订单
func (a *Adapter) convertOrder(order *Order) *OrderLocal {
	var side OrderSide
	if order.Side == "buy" {
		side = SideBuy
	} else {
		side = SideSell
	}

	var status OrderStatus
	switch order.Status {
	case "not_deal":
		status = OrderStatusNew
	case "part_deal":
		status = OrderStatusPartiallyFilled
	case "done":
		status = OrderStatusFilled
	case "cancel":
		status = OrderStatusCanceled
	default:
		status = OrderStatusNew
	}

	price, _ := strconv.ParseFloat(order.Price, 64)
	amount, _ := strconv.ParseFloat(order.Amount, 64)
	dealAmount, _ := strconv.ParseFloat(order.DealAmount, 64)

	return &OrderLocal{
		OrderID:       order.ID,
		ClientOrderID: order.ClientID,
		Symbol:        order.Market,
		Side:          side,
		Price:         price,
		Quantity:      amount,
		ExecutedQty:   dealAmount,
		Status:        status,
		UpdateTime:    order.CreateTime,
	}
}

