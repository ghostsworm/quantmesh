package exchange

import (
	"context"
	"strconv"
	"time"

	"quantmesh/exchange/bitfinex"
)

// bitfinexWrapper Bitfinex 包装器
type bitfinexWrapper struct {
	adapter *bitfinex.Adapter
}

// GetName 获取交易所名称
func (w *bitfinexWrapper) GetName() string {
	return w.adapter.GetName()
}

// PlaceOrder 下单
func (w *bitfinexWrapper) PlaceOrder(ctx context.Context, req *OrderRequest) (*Order, error) {
	bitfinexReq := &bitfinex.BitfinexOrderRequest{
		Symbol:        req.Symbol,
		Side:          bitfinex.Side(req.Side),
		Type:          bitfinex.OrderType(req.Type),
		TimeInForce:   bitfinex.TimeInForce(req.TimeInForce),
		Quantity:      req.Quantity,
		Price:         req.Price,
		ReduceOnly:    req.ReduceOnly,
		PostOnly:      req.PostOnly,
		PriceDecimals: req.PriceDecimals,
		ClientOrderID: req.ClientOrderID,
		Timestamp:     time.Now().UnixMilli(),
	}

	bitfinexOrder, err := w.adapter.PlaceOrder(ctx, bitfinexReq)
	if err != nil {
		return nil, err
	}

	return convertBitfinexOrderToExchangeOrder(bitfinexOrder), nil
}

// BatchPlaceOrders 批量下单
func (w *bitfinexWrapper) BatchPlaceOrders(ctx context.Context, orders []*OrderRequest) ([]*Order, bool) {
	bitfinexOrders := make([]*bitfinex.BitfinexOrderRequest, 0, len(orders))
	for _, order := range orders {
		bitfinexOrders = append(bitfinexOrders, &bitfinex.BitfinexOrderRequest{
			Symbol:        order.Symbol,
			Side:          bitfinex.Side(order.Side),
			Type:          bitfinex.OrderType(order.Type),
			TimeInForce:   bitfinex.TimeInForce(order.TimeInForce),
			Quantity:      order.Quantity,
			Price:         order.Price,
			ReduceOnly:    order.ReduceOnly,
			PostOnly:      order.PostOnly,
			PriceDecimals: order.PriceDecimals,
			ClientOrderID: order.ClientOrderID,
			Timestamp:     time.Now().UnixMilli(),
		})
	}

	bitfinexResults, allSuccess := w.adapter.BatchPlaceOrders(ctx, bitfinexOrders)
	
	results := make([]*Order, 0, len(bitfinexResults))
	for _, bitfinexOrder := range bitfinexResults {
		results = append(results, convertBitfinexOrderToExchangeOrder(bitfinexOrder))
	}

	return results, allSuccess
}

// CancelOrder 取消订单
func (w *bitfinexWrapper) CancelOrder(ctx context.Context, symbol string, orderID int64) error {
	return w.adapter.CancelOrder(ctx, symbol, orderID)
}

// BatchCancelOrders 批量取消订单
func (w *bitfinexWrapper) BatchCancelOrders(ctx context.Context, symbol string, orderIDs []int64) error {
	return w.adapter.BatchCancelOrders(ctx, symbol, orderIDs)
}

// CancelAllOrders 取消所有订单
func (w *bitfinexWrapper) CancelAllOrders(ctx context.Context, symbol string) error {
	return w.adapter.CancelAllOrders(ctx, symbol)
}

// GetOrder 查询订单
func (w *bitfinexWrapper) GetOrder(ctx context.Context, symbol string, orderID int64) (*Order, error) {
	bitfinexOrder, err := w.adapter.GetOrder(ctx, symbol, orderID)
	if err != nil {
		return nil, err
	}
	return convertBitfinexOrderToExchangeOrder(bitfinexOrder), nil
}

// GetOpenOrders 查询未完成订单
func (w *bitfinexWrapper) GetOpenOrders(ctx context.Context, symbol string) ([]*Order, error) {
	bitfinexOrders, err := w.adapter.GetOpenOrders(ctx, symbol)
	if err != nil {
		return nil, err
	}

	orders := make([]*Order, 0, len(bitfinexOrders))
	for _, bitfinexOrder := range bitfinexOrders {
		orders = append(orders, convertBitfinexOrderToExchangeOrder(bitfinexOrder))
	}
	return orders, nil
}

// GetAccount 获取账户信息
func (w *bitfinexWrapper) GetAccount(ctx context.Context) (*Account, error) {
	bitfinexAccount, err := w.adapter.GetAccount(ctx)
	if err != nil {
		return nil, err
	}

	return &Account{
		TotalWalletBalance: bitfinexAccount.TotalBalance,
		TotalMarginBalance: bitfinexAccount.MarginBalance,
		AvailableBalance:   bitfinexAccount.AvailableBalance,
	}, nil
}

// GetPositions 获取持仓信息
func (w *bitfinexWrapper) GetPositions(ctx context.Context, symbol string) ([]*Position, error) {
	bitfinexPositions, err := w.adapter.GetPositions(ctx, symbol)
	if err != nil {
		return nil, err
	}

	positions := make([]*Position, 0, len(bitfinexPositions))
	for _, bitfinexPos := range bitfinexPositions {
		size := bitfinexPos.Size
		if bitfinexPos.Side == "SHORT" {
			size = -size
		}

		positions = append(positions, &Position{
			Symbol:         bitfinexPos.Symbol,
			Size:           size,
			EntryPrice:     bitfinexPos.EntryPrice,
			MarkPrice:      bitfinexPos.MarkPrice,
			UnrealizedPNL:  bitfinexPos.UnrealizedPnL,
			Leverage:       int(bitfinexPos.Leverage),
			MarginType:     bitfinexPos.MarginType,
			IsolatedMargin: bitfinexPos.IsolatedMargin,
		})
	}
	return positions, nil
}

// GetBalance 获取余额
func (w *bitfinexWrapper) GetBalance(ctx context.Context, asset string) (float64, error) {
	return w.adapter.GetBalance(ctx, asset)
}

// StartOrderStream 启动订单流
func (w *bitfinexWrapper) StartOrderStream(ctx context.Context, callback func(interface{})) error {
	return w.adapter.StartOrderStream(ctx, callback)
}

// StopOrderStream 停止订单流
func (w *bitfinexWrapper) StopOrderStream() error {
	return w.adapter.StopOrderStream()
}

// GetLatestPrice 获取最新价格
func (w *bitfinexWrapper) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	return w.adapter.GetLatestPrice(ctx, symbol)
}

// StartPriceStream 启动价格流
func (w *bitfinexWrapper) StartPriceStream(ctx context.Context, symbol string, callback func(price float64)) error {
	return w.adapter.StartPriceStream(ctx, symbol, callback)
}

// StartKlineStream 启动K线流
func (w *bitfinexWrapper) StartKlineStream(ctx context.Context, symbols []string, interval string, callback CandleUpdateCallback) error {
	// 类型转换：将 exchange.CandleUpdateCallback 转换为 bitfinex.CandleUpdateCallback
	bitfinexCallback := func(candle interface{}) {
		if bitfinexCandle, ok := candle.(*bitfinex.Candle); ok {
			exchangeCandle := &Candle{
				Symbol:    "",
				Open:      bitfinexCandle.Open,
				High:      bitfinexCandle.High,
				Low:       bitfinexCandle.Low,
				Close:     bitfinexCandle.Close,
				Volume:    bitfinexCandle.Volume,
				Timestamp: bitfinexCandle.Time,
				IsClosed:  true,
			}
			callback(exchangeCandle)
		}
	}
	return w.adapter.StartKlineStream(ctx, symbols, interval, bitfinexCallback)
}

// StopKlineStream 停止K线流
func (w *bitfinexWrapper) StopKlineStream() error {
	return w.adapter.StopKlineStream()
}

// GetHistoricalKlines 获取历史K线数据
func (w *bitfinexWrapper) GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*Candle, error) {
	bitfinexCandles, err := w.adapter.GetHistoricalKlines(ctx, symbol, interval, limit)
	if err != nil {
		return nil, err
	}

	candles := make([]*Candle, 0, len(bitfinexCandles))
	for _, bitfinexCandle := range bitfinexCandles {
		candles = append(candles, &Candle{
			Symbol:    symbol,
			Open:      bitfinexCandle.Open,
			High:      bitfinexCandle.High,
			Low:       bitfinexCandle.Low,
			Close:     bitfinexCandle.Close,
			Volume:    bitfinexCandle.Volume,
			Timestamp: bitfinexCandle.OpenTime,
			IsClosed:  bitfinexCandle.IsClosed,
		})
	}
	return candles, nil
}

// GetPriceDecimals 获取价格精度
func (w *bitfinexWrapper) GetPriceDecimals() int {
	return w.adapter.GetPriceDecimals()
}

// GetQuantityDecimals 获取数量精度
func (w *bitfinexWrapper) GetQuantityDecimals() int {
	return w.adapter.GetQuantityDecimals()
}

// GetBaseAsset 获取基础资产
func (w *bitfinexWrapper) GetBaseAsset() string {
	return w.adapter.GetBaseAsset()
}

// GetQuoteAsset 获取报价资产
func (w *bitfinexWrapper) GetQuoteAsset() string {
	return w.adapter.GetQuoteAsset()
}

// GetFundingRate 获取资金费率
func (w *bitfinexWrapper) GetFundingRate(ctx context.Context, symbol string) (float64, error) {
	return w.adapter.GetFundingRate(ctx, symbol)
}

// convertBitfinexOrderToExchangeOrder 将 Bitfinex 订单转换为 Exchange 订单
func convertBitfinexOrderToExchangeOrder(bitfinexOrder *bitfinex.Order) *Order {
	return &Order{
		OrderID:       parseBitfinexOrderID(bitfinexOrder.OrderID),
		ClientOrderID: bitfinexOrder.ClientOrderID,
		Symbol:        bitfinexOrder.Symbol,
		Side:          Side(bitfinexOrder.Side),
		Type:          OrderType(bitfinexOrder.Type),
		Price:         bitfinexOrder.Price,
		Quantity:      bitfinexOrder.Quantity,
		ExecutedQty:   bitfinexOrder.ExecutedQty,
		AvgPrice:      bitfinexOrder.AvgPrice,
		Status:        OrderStatus(bitfinexOrder.Status),
		CreatedAt:     bitfinexOrder.CreatedAt,
		UpdateTime:    bitfinexOrder.UpdateTime,
	}
}

// parseBitfinexOrderID 解析订单 ID（Bitfinex 使用字符串 ID，需要转换）
func parseBitfinexOrderID(orderID string) int64 {
	// Bitfinex 使用字符串 ID，这里简化处理，返回 0
	// 实际使用时，可以使用 hash 或其他方式转换
	id, _ := strconv.ParseInt(orderID, 10, 64)
	return id
}

