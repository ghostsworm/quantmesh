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

// NewAdapter 創建 CoinEx 适配器
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

	// 獲取市場信息
	ctx := context.Background()
	marketInfo, err := client.GetMarket(ctx, coinexMarket)
	if err != nil {
		logger.Warn("Failed to get CoinEx market: %v", err)
	} else {
		if marketInfo.PricingDecimal > 0 {
			adapter.priceDecimals = marketInfo.PricingDecimal
		}
		if marketInfo.TradingDecimal > 0 {
			adapter.quantityDecimals = marketInfo.TradingDecimal
		}
		if marketInfo.TradingName != "" {
			adapter.baseAsset = marketInfo.TradingName
		}
		if marketInfo.PricingName != "" {
			adapter.quoteAsset = marketInfo.PricingName
		}
	}

	return adapter, nil
}

// convertSymbolToCoinEx 轉换交易對格式：BTCUSDT -> BTCUSDT
func convertSymbolToCoinEx(symbol string) string {
	return strings.ToUpper(symbol)
}

// GetName 獲取交易所名称
func (a *Adapter) GetName() string {
	return "CoinEx"
}

// GetMarketType 獲取市場類型：futures 合約
func (a *Adapter) GetMarketType() string {
	return "futures"
}

// PlaceOrder 下單
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

// CancelOrder 取消訂單
func (a *Adapter) CancelOrder(ctx context.Context, orderID int64) error {
	return a.client.CancelOrder(ctx, a.market, orderID)
}

// GetOrder 查詢訂單
func (a *Adapter) GetOrder(ctx context.Context, orderID int64) (*OrderLocal, error) {
	order, err := a.client.GetOrder(ctx, a.market, orderID)
	if err != nil {
		return nil, err
	}

	return a.convertOrder(order), nil
}

// GetOpenOrders 獲取活跃订單
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

// GetAccount 獲取帳戶信息
func (a *Adapter) GetAccount(ctx context.Context) (*AccountLocal, error) {
	balance, err := a.client.GetBalance(ctx)
	if err != nil {
		return nil, err
	}

	// 计算總餘額（USDT）
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

// GetPositions 獲取持倉（CoinEx 現貨交易所，返回空）
func (a *Adapter) GetPositions(ctx context.Context) ([]*PositionLocal, error) {
	return []*PositionLocal{}, nil
}

// GetBalance 獲取餘額
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

// StartOrderStream 啟動訂單流
func (a *Adapter) StartOrderStream(ctx context.Context, callback func(interface{})) error {
	if a.wsManager != nil {
		return fmt.Errorf("order stream already started")
	}

	a.wsManager = NewWebSocketManager(a.client.apiKey, a.client.secretKey, a.client.isTestnet)
	return a.wsManager.Start(ctx, a.market, callback)
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
	// 如果傳入 symbol,轉换格式並使用;否则使用預設 market
	targetMarket := a.market
	if symbol != "" {
		targetMarket = convertSymbolToCoinEx(symbol)
	}

	trades, err := a.client.GetTrades(ctx, targetMarket, 1)
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

// StartKlineStream 啟动 K線流
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
			Timestamp: kline.Timestamp * 1000, // 轉换為毫秒
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

// GetFundingRate 獲取资金费率（CoinEx 現貨交易所，回傳 0）
func (a *Adapter) GetFundingRate(ctx context.Context) (float64, error) {
	return 0, nil
}

// convertOrder 轉换订單
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

// InternalTransfer 交易所內部轉帳（CoinEx 暂未實現）
func (a *Adapter) InternalTransfer(ctx context.Context, fromAccount, toAccount, asset string, amount float64) (string, error) {
	return "", fmt.Errorf("internal transfer not implemented for CoinEx")
}
