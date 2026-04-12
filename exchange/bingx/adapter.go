package bingx

import (
	"context"
	"fmt"
	"strings"

	"quantmesh/logger"
)

// Adapter BingX 适配器
type Adapter struct {
	client           *BingXClient
	wsManager        *WebSocketManager
	klineWSManager   *KlineWebSocketManager
	symbol           string
	priceDecimals    int
	quantityDecimals int
	baseAsset        string
	quoteAsset       string
}

// NewAdapter 創建 BingX 适配器
func NewAdapter(config map[string]string, symbol string) (*Adapter, error) {
	apiKey := config["api_key"]
	secretKey := config["secret_key"]
	isTestnet := config["testnet"] == "true"

	if apiKey == "" || secretKey == "" {
		return nil, fmt.Errorf("BingX API key or secret key is empty")
	}

	client := NewBingXClient(apiKey, secretKey, isTestnet)

	// 解析交易對
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

	// 獲取交易對信息
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

// convertSymbolToBingX 轉换交易對格式：BTCUSDT -> BTC-USDT
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

// convertSymbolFromBingX 轉换交易對格式：BTC-USDT -> BTCUSDT
func convertSymbolFromBingX(symbol string) string {
	return strings.ReplaceAll(symbol, "-", "")
}

// GetName 獲取交易所名称
func (a *Adapter) GetName() string {
	return "BingX"
}

// GetMarketType 獲取市場類型：futures 合約
func (a *Adapter) GetMarketType() string {
	return "futures"
}

// PlaceOrder 下單
func (a *Adapter) PlaceOrder(ctx context.Context, side OrderSide, price, quantity float64, clientOrderID string) (*OrderLocal, error) {
	// 轉换订單方向
	var bingxSide string
	var positionSide string

	if side == SideBuy {
		bingxSide = "BUY"
		positionSide = "LONG"
	} else {
		bingxSide = "SELL"
		positionSide = "SHORT"
	}

	// 構造 BingX 订單请求
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

// CancelOrder 取消訂單
func (a *Adapter) CancelOrder(ctx context.Context, orderID int64) error {
	return a.client.CancelOrder(ctx, a.symbol, orderID)
}

// GetOrder 查詢訂單
func (a *Adapter) GetOrder(ctx context.Context, orderID int64) (*OrderLocal, error) {
	orderInfo, err := a.client.GetOrderInfo(ctx, a.symbol, orderID)
	if err != nil {
		return nil, err
	}

	return a.convertOrder(orderInfo), nil
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

// GetPositions 獲取持倉
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
			size = -size // 空倉用负數表示
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

// GetBalance 獲取餘額
func (a *Adapter) GetBalance(ctx context.Context) (float64, error) {
	accountInfo, err := a.client.GetAccount(ctx)
	if err != nil {
		return 0, err
	}

	return accountInfo.Balance.AvailableMargin, nil
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
		targetSymbol = convertSymbolToBingX(symbol)
	}

	ticker, err := a.client.GetTicker(ctx, targetSymbol)
	if err != nil {
		return 0, err
	}

	return ticker.LastPrice, nil
}

// StartKlineStream 啟动 K線流
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
	ticker, err := a.client.GetTicker(ctx, a.symbol)
	if err != nil {
		return 0, err
	}

	return ticker.FundingRate, nil
}

// convertOrder 轉换订單
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

// GetSpotPrice 與 GetLatestPrice 一致（合約參考價）
func (a *Adapter) GetSpotPrice(ctx context.Context, symbol string) (float64, error) {
	return a.GetLatestPrice(ctx, symbol)
}

// InternalTransfer 交易所內部轉帳（BingX 暂未實現）
func (a *Adapter) InternalTransfer(ctx context.Context, fromAccount, toAccount, asset string, amount float64) (string, error) {
	return "", fmt.Errorf("internal transfer not implemented for BingX")
}
