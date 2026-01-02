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

// NewAdapter 创建 BitMEX 适配器
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

	// 获取交易对信息
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

// convertSymbolToBitMEX 转换交易对格式：BTCUSDT -> XBTUSD
func convertSymbolToBitMEX(symbol string) string {
	// BitMEX 使用 XBT 代表 BTC
	if strings.HasPrefix(symbol, "BTC") {
		return "XBTUSD"
	}
	if strings.HasPrefix(symbol, "ETH") {
		return "ETHUSD"
	}
	// 默认返回 XBTUSD
	return "XBTUSD"
}

// GetName 获取交易所名称
func (a *Adapter) GetName() string {
	return "BitMEX"
}

// PlaceOrder 下单
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

// CancelOrder 取消订单
func (a *Adapter) CancelOrder(ctx context.Context, orderID string) error {
	return a.client.CancelOrder(ctx, orderID)
}

// GetOrder 查询订单
func (a *Adapter) GetOrder(ctx context.Context, orderID string) (*OrderLocal, error) {
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
	margin, err := a.client.GetMargin(ctx)
	if err != nil {
		return nil, err
	}

	// BitMEX 金额单位是 Satoshi (1 BTC = 100,000,000 Satoshi)
	return &AccountLocal{
		TotalWalletBalance: float64(margin.WalletBalance) / 100000000,
		TotalMarginBalance: float64(margin.MarginBalance) / 100000000,
		AvailableBalance:   float64(margin.AvailableMargin) / 100000000,
	}, nil
}

// GetPositions 获取持仓
func (a *Adapter) GetPositions(ctx context.Context) ([]*PositionLocal, error) {
	position, err := a.client.GetPosition(ctx, a.symbol)
	if err != nil {
		return nil, err
	}

	if position.CurrentQty == 0 {
		return []*PositionLocal{}, nil
	}

	// BitMEX 的 UnrealisedPnl 单位是 Satoshi
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

// GetBalance 获取余额
func (a *Adapter) GetBalance(ctx context.Context) (float64, error) {
	margin, err := a.client.GetMargin(ctx)
	if err != nil {
		return 0, err
	}

	return float64(margin.AvailableMargin) / 100000000, nil
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
	trades, err := a.client.GetTrade(ctx, a.symbol, 1)
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
	instrument, err := a.client.GetInstrument(ctx, a.symbol)
	if err != nil {
		return 0, err
	}

	// BitMEX 没有直接的 funding rate 字段，返回 0
	_ = instrument
	return 0, nil
}

// convertOrder 转换订单
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
