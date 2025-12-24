package exchange

import (
	"context"
	"opensqt/exchange/bitget"
)

// bitgetWrapper 包装 Bitget 适配器以实现 IExchange 接口
type bitgetWrapper struct {
	adapter *bitget.BitgetAdapter
}

func (w *bitgetWrapper) GetName() string {
	return w.adapter.GetName()
}

func (w *bitgetWrapper) PlaceOrder(ctx context.Context, req *OrderRequest) (*Order, error) {
	// 转换请求类型
	bitgetReq := &bitget.OrderRequest{
		Symbol:        req.Symbol,
		Side:          bitget.Side(req.Side),
		Type:          bitget.OrderType(req.Type),
		TimeInForce:   bitget.TimeInForce(req.TimeInForce),
		Quantity:      req.Quantity,
		Price:         req.Price,
		ReduceOnly:    req.ReduceOnly,
		PostOnly:      req.PostOnly,
		PriceDecimals: req.PriceDecimals,
		ClientOrderID: req.ClientOrderID,
	}

	bitgetOrder, err := w.adapter.PlaceOrder(ctx, bitgetReq)
	if err != nil {
		return nil, err
	}

	// 转换返回类型
	return &Order{
		OrderID:       bitgetOrder.OrderID,
		ClientOrderID: bitgetOrder.ClientOrderID,
		Symbol:        bitgetOrder.Symbol,
		Side:          Side(bitgetOrder.Side),
		Type:          OrderType(bitgetOrder.Type),
		Price:         bitgetOrder.Price,
		Quantity:      bitgetOrder.Quantity,
		ExecutedQty:   bitgetOrder.ExecutedQty,
		AvgPrice:      bitgetOrder.AvgPrice,
		Status:        OrderStatus(bitgetOrder.Status),
		CreatedAt:     bitgetOrder.CreatedAt,
		UpdateTime:    bitgetOrder.UpdateTime,
	}, nil
}

func (w *bitgetWrapper) BatchPlaceOrders(ctx context.Context, orders []*OrderRequest) ([]*Order, bool) {
	bitgetOrders := make([]*bitget.OrderRequest, len(orders))
	for i, req := range orders {
		bitgetOrders[i] = &bitget.OrderRequest{
			Symbol:        req.Symbol,
			Side:          bitget.Side(req.Side),
			Type:          bitget.OrderType(req.Type),
			TimeInForce:   bitget.TimeInForce(req.TimeInForce),
			Quantity:      req.Quantity,
			Price:         req.Price,
			ReduceOnly:    req.ReduceOnly,
			PostOnly:      req.PostOnly,
			PriceDecimals: req.PriceDecimals,
			ClientOrderID: req.ClientOrderID,
		}
	}

	bitgetResult, hasMarginError := w.adapter.BatchPlaceOrders(ctx, bitgetOrders)

	result := make([]*Order, len(bitgetResult))
	for i, ord := range bitgetResult {
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

func (w *bitgetWrapper) CancelOrder(ctx context.Context, symbol string, orderID int64) error {
	return w.adapter.CancelOrder(ctx, symbol, orderID)
}

func (w *bitgetWrapper) BatchCancelOrders(ctx context.Context, symbol string, orderIDs []int64) error {
	return w.adapter.BatchCancelOrders(ctx, symbol, orderIDs)
}

// CancelAllOrders 撤销所有订单（Bitget实现）
// 使用Bitget一键全撤API，更高效且可靠
func (w *bitgetWrapper) CancelAllOrders(ctx context.Context, symbol string) error {
	// Bitget特有的一键全撤API，不需要查询订单列表
	return w.adapter.CancelAllOrders(ctx)
}

func (w *bitgetWrapper) GetOrder(ctx context.Context, symbol string, orderID int64) (*Order, error) {
	bitgetOrder, err := w.adapter.GetOrder(ctx, symbol, orderID)
	if err != nil {
		return nil, err
	}

	return &Order{
		OrderID:       bitgetOrder.OrderID,
		ClientOrderID: bitgetOrder.ClientOrderID,
		Symbol:        bitgetOrder.Symbol,
		Side:          Side(bitgetOrder.Side),
		Type:          OrderType(bitgetOrder.Type),
		Price:         bitgetOrder.Price,
		Quantity:      bitgetOrder.Quantity,
		ExecutedQty:   bitgetOrder.ExecutedQty,
		AvgPrice:      bitgetOrder.AvgPrice,
		Status:        OrderStatus(bitgetOrder.Status),
		CreatedAt:     bitgetOrder.CreatedAt,
		UpdateTime:    bitgetOrder.UpdateTime,
	}, nil
}

func (w *bitgetWrapper) GetOpenOrders(ctx context.Context, symbol string) ([]*Order, error) {
	bitgetOrders, err := w.adapter.GetOpenOrders(ctx, symbol)
	if err != nil {
		return nil, err
	}

	orders := make([]*Order, len(bitgetOrders))
	for i, ord := range bitgetOrders {
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

func (w *bitgetWrapper) GetAccount(ctx context.Context) (*Account, error) {
	bitgetAccount, err := w.adapter.GetAccount(ctx)
	if err != nil {
		return nil, err
	}

	positions := make([]*Position, len(bitgetAccount.Positions))
	for i, pos := range bitgetAccount.Positions {
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
		TotalWalletBalance: bitgetAccount.TotalWalletBalance,
		TotalMarginBalance: bitgetAccount.TotalMarginBalance,
		AvailableBalance:   bitgetAccount.AvailableBalance,
		Positions:          positions,
		AccountLeverage:    bitgetAccount.AccountLeverage,
	}, nil
}

func (w *bitgetWrapper) GetPositions(ctx context.Context, symbol string) ([]*Position, error) {
	bitgetPositions, err := w.adapter.GetPositions(ctx, symbol)
	if err != nil {
		return nil, err
	}

	positions := make([]*Position, len(bitgetPositions))
	for i, pos := range bitgetPositions {
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

func (w *bitgetWrapper) GetBalance(ctx context.Context, asset string) (float64, error) {
	return w.adapter.GetBalance(ctx, asset)
}

func (w *bitgetWrapper) StartOrderStream(ctx context.Context, callback func(interface{})) error {
	return w.adapter.StartOrderStream(ctx, callback)
}

func (w *bitgetWrapper) StopOrderStream() error {
	return w.adapter.StopOrderStream()
}

func (w *bitgetWrapper) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	return w.adapter.GetLatestPrice(ctx, symbol)
}

func (w *bitgetWrapper) StartPriceStream(ctx context.Context, symbol string, callback func(price float64)) error {
	return w.adapter.StartPriceStream(ctx, symbol, callback)
}

func (w *bitgetWrapper) StartKlineStream(ctx context.Context, symbols []string, interval string, callback CandleUpdateCallback) error {
	return w.adapter.StartKlineStream(ctx, symbols, interval, func(candle interface{}) {
		if c, ok := candle.(*bitget.Candle); ok {
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

func (w *bitgetWrapper) StopKlineStream() error {
	return w.adapter.StopKlineStream()
}

func (w *bitgetWrapper) GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*Candle, error) {
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

func (w *bitgetWrapper) GetPriceDecimals() int {
	return w.adapter.GetPriceDecimals()
}

func (w *bitgetWrapper) GetQuantityDecimals() int {
	return w.adapter.GetQuantityDecimals()
}

func (w *bitgetWrapper) GetBaseAsset() string {
	return w.adapter.GetBaseAsset()
}

func (w *bitgetWrapper) GetQuoteAsset() string {
	return w.adapter.GetQuoteAsset()
}
