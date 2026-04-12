package bitmex

import (
	"context"
	"fmt"
	"strings"

	"quantmesh/logger"
)

// Adapter BitMEX 适配器
type Adapter struct {
	client           *BitMEXClient
	wsManager        *WebSocketManager
	klineWSManager   *KlineWebSocketManager
	symbol           string
	priceDecimals    int
	quantityDecimals int
	baseAsset        string
	quoteAsset       string
}

// NewAdapter 創建 BitMEX 适配器
func NewAdapter(config map[string]string, symbol string) (*Adapter, error) {
	apiKey := config["api_key"]
	secretKey := config["secret_key"]
	isTestnet := config["testnet"] == "true"

	if apiKey == "" || secretKey == "" {
		return nil, fmt.Errorf("BitMEX API key or secret key is empty")
	}

	client := NewBitMEXClient(apiKey, secretKey, isTestnet)

	// BitMEX 符号格式：XBTUSD (永续), XBTM25 (季度)
	bitmexSymbol := convertSymbolToBitMEX(symbol)

	adapter := &Adapter{
		client:           client,
		symbol:           bitmexSymbol,
		priceDecimals:    1,
		quantityDecimals: 0,
		baseAsset:        "BTC",
		quoteAsset:       "USD",
	}

	// 獲取交易對信息
	ctx := context.Background()
	instrument, err := client.GetInstrument(ctx, bitmexSymbol)
	if err != nil {
		logger.Warn("Failed to get BitMEX instrument: %v", err)
	} else {
		adapter.priceDecimals = 1
		adapter.quantityDecimals = 0
		adapter.baseAsset = instrument.RootSymbol
	}

	return adapter, nil
}

// convertSymbolToBitMEX 轉换交易對格式：BTCUSDT -> XBTUSD
func convertSymbolToBitMEX(symbol string) string {
	// BitMEX 使用 XBT 代表 BTC
	if strings.HasPrefix(symbol, "BTC") {
		return "XBTUSD"
	}
	if strings.HasPrefix(symbol, "ETH") {
		return "ETHUSD"
	}
	// 默认回傳 XBTUSD
	return "XBTUSD"
}

// GetName 獲取交易所名称
func (a *Adapter) GetName() string {
	return "BitMEX"
}

// GetMarketType 獲取市場類型：futures 合約
func (a *Adapter) GetMarketType() string {
	return "futures"
}

// PlaceOrder 下單
func (a *Adapter) PlaceOrder(ctx context.Context, side OrderSide, price, quantity float64, clientOrderID string) (*OrderLocal, error) {
	var bitmexSide string
	if side == SideBuy {
		bitmexSide = "Buy"
	} else {
		bitmexSide = "Sell"
	}

	req := &OrderRequest{
		Symbol:   a.symbol,
		Side:     bitmexSide,
		OrderQty: quantity,
		Price:    price,
		OrdType:  "Limit",
		ClOrdID:  clientOrderID,
	}

	order, err := a.client.PlaceOrder(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("BitMEX place order error: %w", err)
	}

	return a.convertOrder(order), nil
}

// CancelOrder 取消訂單
func (a *Adapter) CancelOrder(ctx context.Context, orderID string) error {
	return a.client.CancelOrder(ctx, orderID)
}

// GetOrder 查詢訂單
func (a *Adapter) GetOrder(ctx context.Context, orderID string) (*OrderLocal, error) {
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
	margin, err := a.client.GetMargin(ctx)
	if err != nil {
		return nil, err
	}

	// BitMEX 金額單位是 Satoshi (1 BTC = 100,000,000 Satoshi)
	return &AccountLocal{
		TotalWalletBalance: float64(margin.WalletBalance) / 100000000,
		TotalMarginBalance: float64(margin.MarginBalance) / 100000000,
		AvailableBalance:   float64(margin.AvailableMargin) / 100000000,
	}, nil
}

// GetPositions 獲取持倉
func (a *Adapter) GetPositions(ctx context.Context) ([]*PositionLocal, error) {
	position, err := a.client.GetPosition(ctx, a.symbol)
	if err != nil {
		return nil, err
	}

	if position.CurrentQty == 0 {
		return []*PositionLocal{}, nil
	}

	// BitMEX 的 UnrealisedPnl 單位是 Satoshi
	return []*PositionLocal{
		{
			Symbol:        position.Symbol,
			Size:          position.CurrentQty,
			EntryPrice:    position.AvgEntryPrice,
			MarkPrice:     position.MarkPrice,
			UnrealizedPNL: float64(position.UnrealisedPnl) / 100000000,
			Leverage:      int(position.Leverage),
		},
	}, nil
}

// GetBalance 獲取餘額
func (a *Adapter) GetBalance(ctx context.Context) (float64, error) {
	margin, err := a.client.GetMargin(ctx)
	if err != nil {
		return 0, err
	}

	return float64(margin.AvailableMargin) / 100000000, nil
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
		targetSymbol = convertSymbolToBitMEX(symbol)
	}

	trades, err := a.client.GetTrade(ctx, targetSymbol, 1)
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

	binSize := string(ConvertInterval(interval))
	a.klineWSManager = NewKlineWebSocketManager(a.client.isTestnet)

	return a.klineWSManager.Start(ctx, a.symbol, binSize, func(bucket *TradeBucket) {
		candle := &CandleLocal{
			Symbol:    bucket.Symbol,
			Timestamp: bucket.Timestamp.UnixMilli(),
			Open:      bucket.Open,
			High:      bucket.High,
			Low:       bucket.Low,
			Close:     bucket.Close,
			Volume:    bucket.Volume,
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
	binSize := string(ConvertInterval(interval))
	buckets, err := a.client.GetTradeBucketed(ctx, a.symbol, binSize, limit)
	if err != nil {
		return nil, err
	}

	result := make([]*CandleLocal, 0, len(buckets))
	for _, bucket := range buckets {
		result = append(result, &CandleLocal{
			Symbol:    bucket.Symbol,
			Timestamp: bucket.Timestamp.UnixMilli(),
			Open:      bucket.Open,
			High:      bucket.High,
			Low:       bucket.Low,
			Close:     bucket.Close,
			Volume:    bucket.Volume,
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
	instrument, err := a.client.GetInstrument(ctx, a.symbol)
	if err != nil {
		return 0, err
	}

	// BitMEX 没有直接的 funding rate 欄位，回傳 0
	_ = instrument
	return 0, nil
}

// convertOrder 轉换订單
func (a *Adapter) convertOrder(order *Order) *OrderLocal {
	var side OrderSide
	if order.Side == "Buy" {
		side = SideBuy
	} else {
		side = SideSell
	}

	var status OrderStatus
	switch order.OrdStatus {
	case "New":
		status = OrderStatusNew
	case "PartiallyFilled":
		status = OrderStatusPartiallyFilled
	case "Filled":
		status = OrderStatusFilled
	case "Canceled", "Rejected":
		status = OrderStatusCanceled
	default:
		status = OrderStatusNew
	}

	return &OrderLocal{
		OrderID:       order.OrderID,
		ClientOrderID: order.ClOrdID,
		Symbol:        order.Symbol,
		Side:          side,
		Price:         order.Price,
		Quantity:      order.OrderQty,
		ExecutedQty:   order.CumQty,
		Status:        status,
		UpdateTime:    order.TransactTime.UnixMilli(),
	}
}

// GetSpotPrice 與 GetLatestPrice 一致（合約參考價）
func (a *Adapter) GetSpotPrice(ctx context.Context, symbol string) (float64, error) {
	return a.GetLatestPrice(ctx, symbol)
}

// InternalTransfer 交易所內部轉帳（BitMEX 暂未實現）
func (a *Adapter) InternalTransfer(ctx context.Context, fromAccount, toAccount, asset string, amount float64) (string, error) {
	return "", fmt.Errorf("internal transfer not implemented for BitMEX")
}
