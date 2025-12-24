package exchange

import (
	"context"
	"opensqt/exchange/binance"
)

// binanceWrapper 包装 Binance 适配器以实现 IExchange 接口
type binanceWrapper struct {
	adapter *binance.BinanceAdapter
}

func (w *binanceWrapper) GetName() string {
	return w.adapter.GetName()
}

func (w *binanceWrapper) PlaceOrder(ctx context.Context, req *OrderRequest) (*Order, error) {
	binanceReq := &binance.OrderRequest{
		Symbol:        req.Symbol,
		Side:          binance.Side(req.Side),
		Type:          binance.OrderType(req.Type),
		TimeInForce:   binance.TimeInForce(req.TimeInForce),
		Quantity:      req.Quantity,
		Price:         req.Price,
		ReduceOnly:    req.ReduceOnly,
		PostOnly:      req.PostOnly,
		PriceDecimals: req.PriceDecimals,
		ClientOrderID: req.ClientOrderID,
	}

	binanceOrder, err := w.adapter.PlaceOrder(ctx, binanceReq)
	if err != nil {
		return nil, err
	}

	return &Order{
		OrderID:       binanceOrder.OrderID,
		ClientOrderID: binanceOrder.ClientOrderID,
		Symbol:        binanceOrder.Symbol,
		Side:          Side(binanceOrder.Side),
		Type:          OrderType(binanceOrder.Type),
		Price:         binanceOrder.Price,
		Quantity:      binanceOrder.Quantity,
		ExecutedQty:   binanceOrder.ExecutedQty,
		AvgPrice:      binanceOrder.AvgPrice,
		Status:        OrderStatus(binanceOrder.Status),
		CreatedAt:     binanceOrder.CreatedAt,
		UpdateTime:    binanceOrder.UpdateTime,
	}, nil
}

func (w *binanceWrapper) BatchPlaceOrders(ctx context.Context, orders []*OrderRequest) ([]*Order, bool) {
	binanceOrders := make([]*binance.OrderRequest, len(orders))
	for i, req := range orders {
		binanceOrders[i] = &binance.OrderRequest{
			Symbol:        req.Symbol,
			Side:          binance.Side(req.Side),
			Type:          binance.OrderType(req.Type),
			TimeInForce:   binance.TimeInForce(req.TimeInForce),
			Quantity:      req.Quantity,
			Price:         req.Price,
			ReduceOnly:    req.ReduceOnly,
			PostOnly:      req.PostOnly,
			PriceDecimals: req.PriceDecimals,
			ClientOrderID: req.ClientOrderID,
		}
	}

	binanceResult, hasMarginError := w.adapter.BatchPlaceOrders(ctx, binanceOrders)

	result := make([]*Order, len(binanceResult))
	for i, ord := range binanceResult {
		result[i] = &Order{
			OrderID:       ord.OrderID,
			ClientOrderID: ord.ClientOrderID,
			Symbol:        ord.Symbol,
			Side:          Side(ord.Side),
			Type:          OrderType(ord.Type),
			Price:         ord.Price,
			Quantity:      ord.Quantity,
			ExecutedQty:   ord.ExecutedQty,
			AvgPrice:      ord.AvgPrice,
			Status:        OrderStatus(ord.Status),
			CreatedAt:     ord.CreatedAt,
			UpdateTime:    ord.UpdateTime,
		}
	}

	return result, hasMarginError
}

func (w *binanceWrapper) CancelOrder(ctx context.Context, symbol string, orderID int64) error {
	return w.adapter.CancelOrder(ctx, symbol, orderID)
}

func (w *binanceWrapper) BatchCancelOrders(ctx context.Context, symbol string, orderIDs []int64) error {
	return w.adapter.BatchCancelOrders(ctx, symbol, orderIDs)
}

// CancelAllOrders 撤销所有订单（Binance实现）
// 查询所有未完成订单后批量撤销
func (w *binanceWrapper) CancelAllOrders(ctx context.Context, symbol string) error {
	// 1. 查询所有未完成订单
	openOrders, err := w.adapter.GetOpenOrders(ctx, symbol)
	if err != nil {
		return err
	}

	if len(openOrders) == 0 {
		return nil // 没有订单需要撤销
	}

	// 2. 提取所有订单ID
	orderIDs := make([]int64, len(openOrders))
	for i, order := range openOrders {
		orderIDs[i] = order.OrderID
	}

	// 3. 批量撤销（adapter会自动分批处理）
	return w.adapter.BatchCancelOrders(ctx, symbol, orderIDs)
}

func (w *binanceWrapper) GetOrder(ctx context.Context, symbol string, orderID int64) (*Order, error) {
	binanceOrder, err := w.adapter.GetOrder(ctx, symbol, orderID)
	if err != nil {
		return nil, err
	}

	return &Order{
		OrderID:       binanceOrder.OrderID,
		ClientOrderID: binanceOrder.ClientOrderID,
		Symbol:        binanceOrder.Symbol,
		Side:          Side(binanceOrder.Side),
		Type:          OrderType(binanceOrder.Type),
		Price:         binanceOrder.Price,
		Quantity:      binanceOrder.Quantity,
		ExecutedQty:   binanceOrder.ExecutedQty,
		AvgPrice:      binanceOrder.AvgPrice,
		Status:        OrderStatus(binanceOrder.Status),
		CreatedAt:     binanceOrder.CreatedAt,
		UpdateTime:    binanceOrder.UpdateTime,
	}, nil
}

func (w *binanceWrapper) GetOpenOrders(ctx context.Context, symbol string) ([]*Order, error) {
	binanceOrders, err := w.adapter.GetOpenOrders(ctx, symbol)
	if err != nil {
		return nil, err
	}

	orders := make([]*Order, len(binanceOrders))
	for i, ord := range binanceOrders {
		orders[i] = &Order{
			OrderID:       ord.OrderID,
			ClientOrderID: ord.ClientOrderID,
			Symbol:        ord.Symbol,
			Side:          Side(ord.Side),
			Type:          OrderType(ord.Type),
			Price:         ord.Price,
			Quantity:      ord.Quantity,
			ExecutedQty:   ord.ExecutedQty,
			AvgPrice:      ord.AvgPrice,
			Status:        OrderStatus(ord.Status),
			CreatedAt:     ord.CreatedAt,
			UpdateTime:    ord.UpdateTime,
		}
	}

	return orders, nil
}

func (w *binanceWrapper) GetAccount(ctx context.Context) (*Account, error) {
	binanceAccount, err := w.adapter.GetAccount(ctx)
	if err != nil {
		return nil, err
	}

	positions := make([]*Position, len(binanceAccount.Positions))
	for i, pos := range binanceAccount.Positions {
		positions[i] = &Position{
			Symbol:         pos.Symbol,
			Size:           pos.Size,
			EntryPrice:     pos.EntryPrice,
			MarkPrice:      pos.MarkPrice,
			UnrealizedPNL:  pos.UnrealizedPNL,
			Leverage:       pos.Leverage,
			MarginType:     pos.MarginType,
			IsolatedMargin: pos.IsolatedMargin,
		}
	}

	return &Account{
		TotalWalletBalance: binanceAccount.TotalWalletBalance,
		TotalMarginBalance: binanceAccount.TotalMarginBalance,
		AvailableBalance:   binanceAccount.AvailableBalance,
		Positions:          positions,
	}, nil
}

func (w *binanceWrapper) GetPositions(ctx context.Context, symbol string) ([]*Position, error) {
	binancePositions, err := w.adapter.GetPositions(ctx, symbol)
	if err != nil {
		return nil, err
	}

	positions := make([]*Position, len(binancePositions))
	for i, pos := range binancePositions {
		positions[i] = &Position{
			Symbol:         pos.Symbol,
			Size:           pos.Size,
			EntryPrice:     pos.EntryPrice,
			MarkPrice:      pos.MarkPrice,
			UnrealizedPNL:  pos.UnrealizedPNL,
			Leverage:       pos.Leverage,
			MarginType:     pos.MarginType,
			IsolatedMargin: pos.IsolatedMargin,
		}
	}

	return positions, nil
}

func (w *binanceWrapper) GetBalance(ctx context.Context, asset string) (float64, error) {
	return w.adapter.GetBalance(ctx, asset)
}

func (w *binanceWrapper) StartOrderStream(ctx context.Context, callback func(interface{})) error {
	return w.adapter.StartOrderStream(ctx, callback)
}

func (w *binanceWrapper) StopOrderStream() error {
	return w.adapter.StopOrderStream()
}

func (w *binanceWrapper) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	return w.adapter.GetLatestPrice(ctx, symbol)
}

func (w *binanceWrapper) StartPriceStream(ctx context.Context, symbol string, callback func(price float64)) error {
	return w.adapter.StartPriceStream(ctx, symbol, callback)
}

func (w *binanceWrapper) StartKlineStream(ctx context.Context, symbols []string, interval string, callback CandleUpdateCallback) error {
	return w.adapter.StartKlineStream(ctx, symbols, interval, func(candle interface{}) {
		if c, ok := candle.(*binance.Candle); ok {
			callback(&Candle{
				Symbol:    c.Symbol,
				Open:      c.Open,
				High:      c.High,
				Low:       c.Low,
				Close:     c.Close,
				Volume:    c.Volume,
				Timestamp: c.Timestamp,
				IsClosed:  c.IsClosed,
			})
		}
	})
}

func (w *binanceWrapper) StopKlineStream() error {
	return w.adapter.StopKlineStream()
}

func (w *binanceWrapper) GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*Candle, error) {
	candles, err := w.adapter.GetHistoricalKlines(ctx, symbol, interval, limit)
	if err != nil {
		return nil, err
	}

	result := make([]*Candle, len(candles))
	for i, c := range candles {
		result[i] = &Candle{
			Symbol:    c.Symbol,
			Open:      c.Open,
			High:      c.High,
			Low:       c.Low,
			Close:     c.Close,
			Volume:    c.Volume,
			Timestamp: c.Timestamp,
			IsClosed:  c.IsClosed,
		}
	}
	return result, nil
}

func (w *binanceWrapper) GetPriceDecimals() int {
	return w.adapter.GetPriceDecimals()
}

func (w *binanceWrapper) GetQuantityDecimals() int {
	return w.adapter.GetQuantityDecimals()
}

func (w *binanceWrapper) GetBaseAsset() string {
	return w.adapter.GetBaseAsset()
}

func (w *binanceWrapper) GetQuoteAsset() string {
	return w.adapter.GetQuoteAsset()
}
