package deribit

import (
	"context"
	"fmt"
	"strings"
	"time"

	"quantmesh/logger"
)

// Adapter Deribit 适配器
type Adapter struct {
	client           *DeribitClient
	wsManager        *WebSocketManager
	klineWSManager   *KlineWebSocketManager
	instrumentName   string
	currency         string
	priceDecimals    int
	quantityDecimals int
	baseAsset        string
	quoteAsset       string
}

// NewAdapter 创建 Deribit 适配器
func NewAdapter(config map[string]string, symbol string) (*Adapter, error) {
	apiKey := config["api_key"]
	secretKey := config["secret_key"]
	isTestnet := config["testnet"] == "true"

	if apiKey == "" || secretKey == "" {
		return nil, fmt.Errorf("Deribit API key or secret key is empty")
	}

	client := NewDeribitClient(apiKey, secretKey, isTestnet)

	// 认证
	ctx := context.Background()
	if err := client.Authenticate(ctx); err != nil {
		return nil, fmt.Errorf("Deribit authentication failed: %w", err)
	}

	// 解析交易对：BTCUSDT -> BTC-PERPETUAL
	currency := "BTC"
	if strings.HasPrefix(symbol, "ETH") {
		currency = "ETH"
	}
	instrumentName := currency + "-PERPETUAL"

	adapter := &Adapter{
		client:           client,
		instrumentName:   instrumentName,
		currency:         currency,
		priceDecimals:    1,
		quantityDecimals: 0,
		baseAsset:        currency,
		quoteAsset:       "USD",
	}

	// 获取交易对信息
	instruments, err := client.GetInstruments(ctx, currency)
	if err != nil {
		logger.Warn("Failed to get Deribit instruments: %v", err)
	} else {
		for _, inst := range instruments {
			if inst.InstrumentName == instrumentName {
				adapter.priceDecimals = 1
				adapter.quantityDecimals = 0
				break
			}
		}
	}

	return adapter, nil
}

// GetName 获取交易所名称
func (a *Adapter) GetName() string {
	return "Deribit"
}

// PlaceOrder 下单
func (a *Adapter) PlaceOrder(ctx context.Context, side OrderSide, price, quantity float64, clientOrderID string) (*OrderLocal, error) {
	// Deribit 使用合约数量（整数）
	amount := quantity

	req := &OrderRequest{
		InstrumentName: a.instrumentName,
		Amount:         amount,
		Type:           "limit",
		Price:          price,
		Label:          clientOrderID,
	}

	var resp *OrderResponse
	var err error

	if side == SideBuy {
		resp, err = a.client.Buy(ctx, req)
	} else {
		resp, err = a.client.Sell(ctx, req)
	}

	if err != nil {
		return nil, fmt.Errorf("Deribit place order error: %w", err)
	}

	return &OrderLocal{
		OrderID:       resp.OrderID,
		ClientOrderID: clientOrderID,
		Symbol:        a.instrumentName,
		Side:          side,
		Price:         resp.Price,
		Quantity:      resp.Amount,
		ExecutedQty:   resp.FilledAmount,
		Status:        convertOrderState(resp.OrderState),
		UpdateTime:    time.Now().UnixMilli(),
	}, nil
}

// CancelOrder 取消订单
func (a *Adapter) CancelOrder(ctx context.Context, orderID string) error {
	return a.client.CancelOrder(ctx, orderID)
}

// GetOrder 查询订单
func (a *Adapter) GetOrder(ctx context.Context, orderID string) (*OrderLocal, error) {
	orderInfo, err := a.client.GetOrderState(ctx, orderID)
	if err != nil {
		return nil, err
	}

	return a.convertOrder(orderInfo), nil
}

// GetOpenOrders 获取活跃订单
func (a *Adapter) GetOpenOrders(ctx context.Context) ([]*OrderLocal, error) {
	orders, err := a.client.GetOpenOrders(ctx, a.instrumentName)
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
	account, err := a.client.GetAccountSummary(ctx, a.currency)
	if err != nil {
		return nil, err
	}

	return &AccountLocal{
		TotalWalletBalance: account.Balance,
		TotalMarginBalance: account.MarginBalance,
		AvailableBalance:   account.AvailableFunds,
	}, nil
}

// GetPositions 获取持仓
func (a *Adapter) GetPositions(ctx context.Context) ([]*PositionLocal, error) {
	positions, err := a.client.GetPositions(ctx, a.currency)
	if err != nil {
		return nil, err
	}

	result := make([]*PositionLocal, 0, len(positions))
	for _, pos := range positions {
		if pos.Size == 0 {
			continue
		}

		// Deribit 的 size 已经带方向（正数=多，负数=空）
		result = append(result, &PositionLocal{
			Symbol:        pos.InstrumentName,
			Size:          pos.Size,
			EntryPrice:    pos.AveragePrice,
			MarkPrice:     pos.MarkPrice,
			UnrealizedPNL: pos.FloatingProfitLoss,
			Leverage:      pos.Leverage,
		})
	}

	return result, nil
}

// GetBalance 获取余额
func (a *Adapter) GetBalance(ctx context.Context) (float64, error) {
	account, err := a.client.GetAccountSummary(ctx, a.currency)
	if err != nil {
		return 0, err
	}

	return account.AvailableFunds, nil
}

// StartOrderStream 启动订单流
func (a *Adapter) StartOrderStream(ctx context.Context, callback func(interface{})) error {
	if a.wsManager != nil {
		return fmt.Errorf("order stream already started")
	}

	a.wsManager = NewWebSocketManager(a.client.apiKey, a.client.secretKey, a.client.isTestnet)
	return a.wsManager.Start(ctx, a.instrumentName, callback)
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
	ticker, err := a.client.GetTicker(ctx, a.instrumentName)
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

	resolution := string(ConvertInterval(interval))
	a.klineWSManager = NewKlineWebSocketManager(a.client.isTestnet)

	return a.klineWSManager.Start(ctx, a.instrumentName, resolution, func(tick int64, open, high, low, close, volume float64) {
		candle := &CandleLocal{
			Symbol:    a.instrumentName,
			Timestamp: tick,
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
	resolution := string(ConvertInterval(interval))

	endTime := time.Now().UnixMilli()
	// 根据周期计算开始时间
	var duration time.Duration
	switch interval {
	case "1m":
		duration = time.Minute * time.Duration(limit)
	case "5m":
		duration = time.Minute * 5 * time.Duration(limit)
	case "15m":
		duration = time.Minute * 15 * time.Duration(limit)
	case "1h":
		duration = time.Hour * time.Duration(limit)
	case "1d":
		duration = time.Hour * 24 * time.Duration(limit)
	default:
		duration = time.Minute * time.Duration(limit)
	}
	startTime := time.Now().Add(-duration).UnixMilli()

	chartData, err := a.client.GetTradingViewChartData(ctx, a.instrumentName, resolution, startTime, endTime)
	if err != nil {
		return nil, err
	}

	result := make([]*CandleLocal, 0, len(chartData.Ticks))
	for i := range chartData.Ticks {
		result = append(result, &CandleLocal{
			Symbol:    a.instrumentName,
			Timestamp: chartData.Ticks[i],
			Open:      chartData.Open[i],
			High:      chartData.High[i],
			Low:       chartData.Low[i],
			Close:     chartData.Close[i],
			Volume:    chartData.Volume[i],
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
	// Deribit 没有资金费率（期权交易所）
	return 0, nil
}

// convertOrder 转换订单
func (a *Adapter) convertOrder(order *OrderInfo) *OrderLocal {
	var side OrderSide
	if order.Direction == "buy" {
		side = SideBuy
	} else {
		side = SideSell
	}

	return &OrderLocal{
		OrderID:       order.OrderID,
		ClientOrderID: order.Label,
		Symbol:        order.InstrumentName,
		Side:          side,
		Price:         order.Price,
		Quantity:      order.Amount,
		ExecutedQty:   order.FilledAmount,
		Status:        convertOrderState(order.OrderState),
		UpdateTime:    order.LastUpdateTimestamp,
	}
}

// convertOrderState 转换订单状态
func convertOrderState(state string) OrderStatus {
	switch state {
	case "open":
		return OrderStatusNew
	case "filled":
		return OrderStatusFilled
	case "cancelled":
		return OrderStatusCanceled
	default:
		return OrderStatusNew
	}
}
