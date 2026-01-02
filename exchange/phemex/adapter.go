package phemex

import (
	"context"
	"fmt"
	"strings"

	"quantmesh/logger"
)

// Adapter Phemex 适配器
type Adapter struct {
	client           *PhemexClient
	wsManager        *WebSocketManager
	klineWSManager   *KlineWebSocketManager
	symbol           string
	priceScale       int
	priceDecimals    int
	quantityDecimals int
	baseAsset        string
	quoteAsset       string
}

// NewAdapter 创建 Phemex 适配器
func NewAdapter(config map[string]string, symbol string) (*Adapter, error) {
	apiKey := config["api_key"]
	secretKey := config["secret_key"]
	isTestnet := config["testnet"] == "true"

	if apiKey == "" || secretKey == "" {
		return nil, fmt.Errorf("Phemex API key or secret key is empty")
	}

	client := NewPhemexClient(apiKey, secretKey, isTestnet)

	// Phemex 符号格式：BTCUSD (永续)
	phemexSymbol := convertSymbolToPhemex(symbol)

	adapter := &Adapter{
		client:           client,
		symbol:           phemexSymbol,
		priceScale:       4, // 默认价格缩放因子
		priceDecimals:    2,
		quantityDecimals: 0,
		baseAsset:        "BTC",
		quoteAsset:       "USD",
	}

	// 获取交易对信息
	ctx := context.Background()
	product, err := client.GetProduct(ctx, phemexSymbol)
	if err != nil {
		logger.Warn("Failed to get Phemex product: %v", err)
	} else {
		adapter.priceScale = product.PriceScale
		adapter.priceDecimals = product.PriceScale
		adapter.quantityDecimals = 0

		// 解析基础资产和报价资产
		if strings.HasSuffix(product.Symbol, "USD") {
			adapter.baseAsset = strings.TrimSuffix(product.Symbol, "USD")
			adapter.quoteAsset = "USD"
		}
	}

	return adapter, nil
}

// convertSymbolToPhemex 转换交易对格式：BTCUSDT -> BTCUSD
func convertSymbolToPhemex(symbol string) string {
	// Phemex 永续合约格式：BTCUSD, ETHUSD
	symbol = strings.ToUpper(symbol)
	if strings.HasSuffix(symbol, "USDT") {
		return strings.TrimSuffix(symbol, "T") // BTCUSDT -> BTCUSD
	}
	return symbol
}

// GetName 获取交易所名称
func (a *Adapter) GetName() string {
	return "Phemex"
}

// PlaceOrder 下单
func (a *Adapter) PlaceOrder(ctx context.Context, side OrderSide, price, quantity float64, clientOrderID string) (*OrderLocal, error) {
	var phemexSide string
	if side == SideBuy {
		phemexSide = "Buy"
	} else {
		phemexSide = "Sell"
	}

	// 价格和数量需要缩放
	priceEp := ScalePrice(price, a.priceScale)
	orderQty := int64(quantity)

	req := &OrderRequest{
		Symbol:   a.symbol,
		Side:     phemexSide,
		OrderQty: orderQty,
		PriceEp:  priceEp,
		OrdType:  "Limit",
		ClOrdID:  clientOrderID,
	}

	order, err := a.client.PlaceOrder(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("Phemex place order error: %w", err)
	}

	return a.convertOrder(order), nil
}

// CancelOrder 取消订单
func (a *Adapter) CancelOrder(ctx context.Context, orderID string) error {
	return a.client.CancelOrder(ctx, a.symbol, orderID)
}

// GetOrder 查询订单
func (a *Adapter) GetOrder(ctx context.Context, orderID string) (*OrderLocal, error) {
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
	account, err := a.client.GetAccount(ctx, "BTC")
	if err != nil {
		return nil, err
	}

	// Phemex 金额单位是 Ev (1 BTC = 1e8 Ev)
	return &AccountLocal{
		TotalWalletBalance: UnscaleValue(account.AccountBalanceEv),
		TotalMarginBalance: UnscaleValue(account.AccountBalanceEv),
		AvailableBalance:   UnscaleValue(account.AccountBalanceEv - account.TotalUsedBalanceEv),
	}, nil
}

// GetPositions 获取持仓
func (a *Adapter) GetPositions(ctx context.Context) ([]*PositionLocal, error) {
	position, err := a.client.GetPosition(ctx, a.symbol)
	if err != nil {
		return nil, err
	}

	if position.Size == 0 {
		return []*PositionLocal{}, nil
	}

	// 计算持仓方向和大小
	size := float64(position.Size)
	if position.Side == "Sell" {
		size = -size
	}

	return []*PositionLocal{
		{
			Symbol:        position.Symbol,
			Size:          size,
			EntryPrice:    UnscalePrice(position.AvgEntryPriceEp, a.priceScale),
			MarkPrice:     UnscalePrice(position.MarkPriceEp, a.priceScale),
			UnrealizedPNL: UnscaleValue(position.UnrealisedPnlEv),
			Leverage:      position.Leverage,
		},
	}, nil
}

// GetBalance 获取余额
func (a *Adapter) GetBalance(ctx context.Context) (float64, error) {
	account, err := a.client.GetAccount(ctx, "BTC")
	if err != nil {
		return 0, err
	}

	return UnscaleValue(account.AccountBalanceEv - account.TotalUsedBalanceEv), nil
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
	trades, err := a.client.GetTrades(ctx, a.symbol)
	if err != nil {
		return 0, err
	}

	if len(trades) == 0 {
		return 0, fmt.Errorf("no trades found")
	}

	return UnscalePrice(trades[0].PriceEp, a.priceScale), nil
}

// StartKlineStream 启动 K线流
func (a *Adapter) StartKlineStream(ctx context.Context, interval string, callback CandleUpdateCallbackLocal) error {
	if a.klineWSManager != nil {
		return fmt.Errorf("kline stream already started")
	}

	resolution := int(ConvertInterval(interval))
	a.klineWSManager = NewKlineWebSocketManager(a.client.isTestnet)

	return a.klineWSManager.Start(ctx, a.symbol, resolution, func(kline *Kline) {
		candle := &CandleLocal{
			Symbol:    a.symbol,
			Timestamp: kline.Timestamp * 1000, // 转换为毫秒
			Open:      UnscalePrice(kline.OpenEp, a.priceScale),
			High:      UnscalePrice(kline.HighEp, a.priceScale),
			Low:       UnscalePrice(kline.LowEp, a.priceScale),
			Close:     UnscalePrice(kline.CloseEp, a.priceScale),
			Volume:    float64(kline.Volume),
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
	resolution := int(ConvertInterval(interval))
	klines, err := a.client.GetKlines(ctx, a.symbol, resolution, limit)
	if err != nil {
		return nil, err
	}

	result := make([]*CandleLocal, 0, len(klines))
	for _, kline := range klines {
		result = append(result, &CandleLocal{
			Symbol:    a.symbol,
			Timestamp: kline.Timestamp * 1000,
			Open:      UnscalePrice(kline.OpenEp, a.priceScale),
			High:      UnscalePrice(kline.HighEp, a.priceScale),
			Low:       UnscalePrice(kline.LowEp, a.priceScale),
			Close:     UnscalePrice(kline.CloseEp, a.priceScale),
			Volume:    float64(kline.Volume),
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
	// Phemex 资金费率需要单独查询，这里返回 0
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
		Price:         UnscalePrice(order.PriceEp, a.priceScale),
		Quantity:      float64(order.OrderQty),
		ExecutedQty:   float64(order.CumQty),
		Status:        status,
		UpdateTime:    order.TransactTimeNs / 1000000, // 纳秒转毫秒
	}
}
