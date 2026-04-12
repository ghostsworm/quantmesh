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

// NewAdapter 創建 WOO X 适配器
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

	// 獲取交易對信息
	ctx := context.Background()
	symbolInfo, err := client.GetSymbol(ctx, wooxSymbol)
	if err != nil {
		logger.Warn("Failed to get WOO X symbol: %v", err)
	} else {
		adapter.priceDecimals = symbolInfo.PrecisionPrice
		adapter.quantityDecimals = symbolInfo.PrecisionQuantity

		// 解析基础资產和报價资產
		parts := strings.Split(strings.TrimPrefix(symbolInfo.Symbol, "PERP_"), "_")
		if len(parts) >= 2 {
			adapter.baseAsset = parts[0]
			adapter.quoteAsset = parts[1]
		}
	}

	return adapter, nil
}

// convertSymbolToWOOX 轉换交易對格式：BTCUSDT -> PERP_BTC_USDT
func convertSymbolToWOOX(symbol string) string {
	symbol = strings.ToUpper(symbol)
	if strings.HasSuffix(symbol, "USDT") {
		base := strings.TrimSuffix(symbol, "USDT")
		return fmt.Sprintf("PERP_%s_USDT", base)
	}
	return symbol
}

// GetName 獲取交易所名称
func (a *Adapter) GetName() string {
	return "WOO X"
}

// GetMarketType 獲取市場類型：futures 合約
func (a *Adapter) GetMarketType() string {
	return "futures"
}

// PlaceOrder 下單
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

// CancelOrder 取消訂單
func (a *Adapter) CancelOrder(ctx context.Context, orderID int64) error {
	return a.client.CancelOrder(ctx, a.symbol, orderID)
}

// GetOrder 查詢訂單
func (a *Adapter) GetOrder(ctx context.Context, orderID int64) (*OrderLocal, error) {
	order, err := a.client.GetOrder(ctx, orderID)
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

// GetPositions 獲取持倉
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

// GetBalance 獲取餘額
func (a *Adapter) GetBalance(ctx context.Context) (float64, error) {
	account, err := a.client.GetAccount(ctx)
	if err != nil {
		return 0, err
	}

	return account.TotalAccountValue - account.TotalCollateral, nil
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
		targetSymbol = convertSymbolToWOOX(symbol)
	}

	trades, err := a.client.GetTrades(ctx, targetSymbol, 1)
	if err != nil {
		return 0, err
	}

	if len(trades) == 0 {
		return 0, fmt.Errorf("no trades found")
	}

	return trades[0].Price, nil
}

// StartKlineStream 啟动 K線流
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

// GetFundingRate 獲取资金费率
func (a *Adapter) GetFundingRate(ctx context.Context) (float64, error) {
	// WOO X 资金费率需要單独查詢，这里回傳 0
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

// GetSpotPrice 與 GetLatestPrice 一致（合約參考價）
func (a *Adapter) GetSpotPrice(ctx context.Context, symbol string) (float64, error) {
	return a.GetLatestPrice(ctx, symbol)
}

// InternalTransfer 交易所內部轉帳（WOO X 暂未實現）
func (a *Adapter) InternalTransfer(ctx context.Context, fromAccount, toAccount, asset string, amount float64) (string, error) {
	return "", fmt.Errorf("internal transfer not implemented for WOO X")
}
