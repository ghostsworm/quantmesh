package exchange

import (
	"context"
	"time"

	"quantmesh/exchange/kucoin"
)

// kucoinWrapper KuCoin 包装器
type kucoinWrapper struct {
	adapter *kucoin.Adapter
}

// GetName 获取交易所名称
func (w *kucoinWrapper) GetName() string {
	return w.adapter.GetName()
}

// PlaceOrder 下单
func (w *kucoinWrapper) PlaceOrder(ctx context.Context, req *OrderRequest) (*Order, error) {
	kucoinReq := &kucoin.KuCoinOrderRequest{
		Symbol:        req.Symbol,
		Side:          kucoin.Side(req.Side),
		Type:          kucoin.OrderType(req.Type),
		TimeInForce:   kucoin.TimeInForce(req.TimeInForce),
		Quantity:      req.Quantity,
		Price:         req.Price,
		ReduceOnly:    req.ReduceOnly,
		PostOnly:      req.PostOnly,
		PriceDecimals: req.PriceDecimals,
		ClientOrderID: req.ClientOrderID,
		Timestamp:     time.Now().UnixMilli(),
	}

	kucoinOrder, err := w.adapter.PlaceOrder(ctx, kucoinReq)
	if err != nil {
		return nil, err
	}

	return convertKuCoinOrderToExchangeOrder(kucoinOrder), nil
}

// BatchPlaceOrders 批量下单
func (w *kucoinWrapper) BatchPlaceOrders(ctx context.Context, orders []*OrderRequest) ([]*Order, bool) {
	kucoinOrders := make([]*kucoin.KuCoinOrderRequest, 0, len(orders))
	for _, order := range orders {
		kucoinOrders = append(kucoinOrders, &kucoin.KuCoinOrderRequest{
			Symbol:        order.Symbol,
			Side:          kucoin.Side(order.Side),
			Type:          kucoin.OrderType(order.Type),
			TimeInForce:   kucoin.TimeInForce(order.TimeInForce),
			Quantity:      order.Quantity,
			Price:         order.Price,
			ReduceOnly:    order.ReduceOnly,
			PostOnly:      order.PostOnly,
			PriceDecimals: order.PriceDecimals,
			ClientOrderID: order.ClientOrderID,
			Timestamp:     time.Now().UnixMilli(),
		})
	}

	kucoinResults, allSuccess := w.adapter.BatchPlaceOrders(ctx, kucoinOrders)
	
	results := make([]*Order, 0, len(kucoinResults))
	for _, kucoinOrder := range kucoinResults {
		results = append(results, convertKuCoinOrderToExchangeOrder(kucoinOrder))
	}

	return results, allSuccess
}

// CancelOrder 取消订单
func (w *kucoinWrapper) CancelOrder(ctx context.Context, symbol string, orderID int64) error {
	return w.adapter.CancelOrder(ctx, symbol, orderID)
}

// BatchCancelOrders 批量取消订单
func (w *kucoinWrapper) BatchCancelOrders(ctx context.Context, symbol string, orderIDs []int64) error {
	return w.adapter.BatchCancelOrders(ctx, symbol, orderIDs)
}

// CancelAllOrders 取消所有订单
func (w *kucoinWrapper) CancelAllOrders(ctx context.Context, symbol string) error {
	return w.adapter.CancelAllOrders(ctx, symbol)
}

// GetOrder 查询订单
func (w *kucoinWrapper) GetOrder(ctx context.Context, symbol string, orderID int64) (*Order, error) {
	kucoinOrder, err := w.adapter.GetOrder(ctx, symbol, orderID)
	if err != nil {
		return nil, err
	}
	return convertKuCoinOrderToExchangeOrder(kucoinOrder), nil
}

// GetOpenOrders 查询未完成订单
func (w *kucoinWrapper) GetOpenOrders(ctx context.Context, symbol string) ([]*Order, error) {
	kucoinOrders, err := w.adapter.GetOpenOrders(ctx, symbol)
	if err != nil {
		return nil, err
	}

	orders := make([]*Order, 0, len(kucoinOrders))
	for _, kucoinOrder := range kucoinOrders {
		orders = append(orders, convertKuCoinOrderToExchangeOrder(kucoinOrder))
	}
	return orders, nil
}

// GetAccount 获取账户信息
func (w *kucoinWrapper) GetAccount(ctx context.Context) (*Account, error) {
	kucoinAccount, err := w.adapter.GetAccount(ctx)
	if err != nil {
		return nil, err
	}

	return &Account{
		TotalWalletBalance: kucoinAccount.TotalBalance,
		TotalMarginBalance: kucoinAccount.MarginBalance,
		AvailableBalance:   kucoinAccount.AvailableBalance,
	}, nil
}

// GetPositions 获取持仓信息
func (w *kucoinWrapper) GetPositions(ctx context.Context, symbol string) ([]*Position, error) {
	kucoinPositions, err := w.adapter.GetPositions(ctx, symbol)
	if err != nil {
		return nil, err
	}

	positions := make([]*Position, 0, len(kucoinPositions))
	for _, kucoinPos := range kucoinPositions {
		size := kucoinPos.Size
		if kucoinPos.Side == "SHORT" {
			size = -size
		}

		positions = append(positions, &Position{
			Symbol:         kucoinPos.Symbol,
			Size:           size,
			EntryPrice:     kucoinPos.EntryPrice,
			MarkPrice:      kucoinPos.MarkPrice,
			UnrealizedPNL:  kucoinPos.UnrealizedPnL,
			Leverage:       int(kucoinPos.Leverage),
			MarginType:     kucoinPos.MarginType,
			IsolatedMargin: kucoinPos.IsolatedMargin,
		})
	}
	return positions, nil
}

// GetBalance 获取余额
func (w *kucoinWrapper) GetBalance(ctx context.Context, asset string) (float64, error) {
	return w.adapter.GetBalance(ctx, asset)
}

// StartOrderStream 启动订单流
func (w *kucoinWrapper) StartOrderStream(ctx context.Context, callback func(interface{})) error {
	return w.adapter.StartOrderStream(ctx, callback)
}

// StopOrderStream 停止订单流
func (w *kucoinWrapper) StopOrderStream() error {
	return w.adapter.StopOrderStream()
}

// GetLatestPrice 获取最新价格
func (w *kucoinWrapper) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	return w.adapter.GetLatestPrice(ctx, symbol)
}

// StartPriceStream 启动价格流
func (w *kucoinWrapper) StartPriceStream(ctx context.Context, symbol string, callback func(price float64)) error {
	return w.adapter.StartPriceStream(ctx, symbol, callback)
}

// StartKlineStream 启动K线流
func (w *kucoinWrapper) StartKlineStream(ctx context.Context, symbols []string, interval string, callback CandleUpdateCallback) error {
	// 类型转换：将 exchange.CandleUpdateCallback 转换为 kucoin.CandleUpdateCallback
	kucoinCallback := func(candle interface{}) {
		if kucoinCandle, ok := candle.(*kucoin.Candle); ok {
			exchangeCandle := &Candle{
				Symbol:    "",
				Open:      kucoinCandle.Open,
				High:      kucoinCandle.High,
				Low:       kucoinCandle.Low,
				Close:     kucoinCandle.Close,
				Volume:    kucoinCandle.Volume,
				Timestamp: kucoinCandle.Time,
				IsClosed:  true,
			}
			callback(exchangeCandle)
		}
	}
	return w.adapter.StartKlineStream(ctx, symbols, interval, kucoinCallback)
}

// StopKlineStream 停止K线流
func (w *kucoinWrapper) StopKlineStream() error {
	return w.adapter.StopKlineStream()
}

// GetHistoricalKlines 获取历史K线数据
func (w *kucoinWrapper) GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*Candle, error) {
	kucoinCandles, err := w.adapter.GetHistoricalKlines(ctx, symbol, interval, limit)
	if err != nil {
		return nil, err
	}

	candles := make([]*Candle, 0, len(kucoinCandles))
	for _, kucoinCandle := range kucoinCandles {
		candles = append(candles, &Candle{
			Symbol:    symbol,
			Open:      kucoinCandle.Open,
			High:      kucoinCandle.High,
			Low:       kucoinCandle.Low,
			Close:     kucoinCandle.Close,
			Volume:    kucoinCandle.Volume,
			Timestamp: kucoinCandle.OpenTime,
			IsClosed:  kucoinCandle.IsClosed,
		})
	}
	return candles, nil
}

// GetPriceDecimals 获取价格精度
func (w *kucoinWrapper) GetPriceDecimals() int {
	return w.adapter.GetPriceDecimals()
}

// GetQuantityDecimals 获取数量精度
func (w *kucoinWrapper) GetQuantityDecimals() int {
	return w.adapter.GetQuantityDecimals()
}

// GetBaseAsset 获取基础资产
func (w *kucoinWrapper) GetBaseAsset() string {
	return w.adapter.GetBaseAsset()
}

// GetQuoteAsset 获取报价资产
func (w *kucoinWrapper) GetQuoteAsset() string {
	return w.adapter.GetQuoteAsset()
}

// GetFundingRate 获取资金费率
func (w *kucoinWrapper) GetFundingRate(ctx context.Context, symbol string) (float64, error) {
	return w.adapter.GetFundingRate(ctx, symbol)
}

// convertKuCoinOrderToExchangeOrder 将 KuCoin 订单转换为 Exchange 订单
func convertKuCoinOrderToExchangeOrder(kucoinOrder *kucoin.Order) *Order {
	return &Order{
		OrderID:       parseOrderID(kucoinOrder.OrderID),
		ClientOrderID: kucoinOrder.ClientOrderID,
		Symbol:        kucoinOrder.Symbol,
		Side:          Side(kucoinOrder.Side),
		Type:          OrderType(kucoinOrder.Type),
		Price:         kucoinOrder.Price,
		Quantity:      kucoinOrder.Quantity,
		ExecutedQty:   kucoinOrder.ExecutedQty,
		AvgPrice:      kucoinOrder.AvgPrice,
		Status:        OrderStatus(kucoinOrder.Status),
		CreatedAt:     kucoinOrder.CreatedAt,
		UpdateTime:    kucoinOrder.UpdateTime,
	}
}

// parseOrderID 解析订单 ID（KuCoin 使用字符串 ID，需要转换）
func parseOrderID(orderID string) int64 {
	// KuCoin 使用字符串 ID，这里简化处理，返回 0
	// 实际使用时，可以使用 hash 或其他方式转换
	return 0
}


// GetSpotPrice 获取现货市场价格（未实现）
func (w *kucoinWrapper) GetSpotPrice(ctx context.Context, symbol string) (float64, error) {
	return 0, ErrNotImplemented
}
