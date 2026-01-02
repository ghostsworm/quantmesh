package exchange

import (
	"context"
	"quantmesh/exchange/cryptocom"
)

type cryptocomWrapper struct {
	adapter *cryptocom.Adapter
}

func (w *cryptocomWrapper) GetName() string {
	return w.adapter.GetName()
}

func (w *cryptocomWrapper) PlaceOrder(ctx context.Context, req *OrderRequest) (*Order, error) {
	var side cryptocom.OrderSide
	if req.Side == SideBuy {
		side = cryptocom.SideBuy
	} else {
		side = cryptocom.SideSell
	}

	order, err := w.adapter.PlaceOrder(ctx, side, req.Price, req.Quantity, req.ClientOrderID)
	if err != nil {
		return nil, err
	}

	return &Order{
		OrderID:       order.OrderID,
		ClientOrderID: order.ClientOID,
		Symbol:        order.InstrumentName,
		Side:          req.Side,
		Price:         order.Price,
		Quantity:      order.Quantity,
		ExecutedQty:   order.ExecutedQty,
		Status:        OrderStatus(order.Status),
		UpdateTime:    order.UpdateTime,
	}, nil
}

func (w *cryptocomWrapper) BatchPlaceOrders(ctx context.Context, orders []*OrderRequest) ([]*Order, bool) {
	result := make([]*Order, 0, len(orders))
	allSuccess := true
	for _, req := range orders {
		order, err := w.PlaceOrder(ctx, req)
		if err != nil {
			allSuccess = false
			continue
		}
		result = append(result, order)
	}
	return result, allSuccess
}

func (w *cryptocomWrapper) CancelOrder(ctx context.Context, symbol string, orderID int64) error {
	return w.adapter.CancelOrder(ctx, orderID)
}

func (w *cryptocomWrapper) BatchCancelOrders(ctx context.Context, symbol string, orderIDs []int64) error {
	for _, orderID := range orderIDs {
		_ = w.adapter.CancelOrder(ctx, orderID)
	}
	return nil
}

func (w *cryptocomWrapper) CancelAllOrders(ctx context.Context, symbol string) error {
	orders, err := w.adapter.GetOpenOrders(ctx)
	if err != nil {
		return err
	}
	for _, order := range orders {
		_ = w.adapter.CancelOrder(ctx, order.OrderID)
	}
	return nil
}

func (w *cryptocomWrapper) GetOrder(ctx context.Context, symbol string, orderID int64) (*Order, error) {
	order, err := w.adapter.GetOrder(ctx, orderID)
	if err != nil {
		return nil, err
	}

	var side Side
	if order.Side == cryptocom.SideBuy {
		side = SideBuy
	} else {
		side = SideSell
	}

	return &Order{
		OrderID:       order.OrderID,
		ClientOrderID: order.ClientOID,
		Symbol:        order.InstrumentName,
		Side:          side,
		Price:         order.Price,
		Quantity:      order.Quantity,
		ExecutedQty:   order.ExecutedQty,
		Status:        OrderStatus(order.Status),
		UpdateTime:    order.UpdateTime,
	}, nil
}

func (w *cryptocomWrapper) GetOpenOrders(ctx context.Context, symbol string) ([]*Order, error) {
	orders, err := w.adapter.GetOpenOrders(ctx)
	if err != nil {
		return nil, err
	}

	result := make([]*Order, 0, len(orders))
	for _, order := range orders {
		var side Side
		if order.Side == cryptocom.SideBuy {
			side = SideBuy
		} else {
			side = SideSell
		}

		result = append(result, &Order{
			OrderID:       order.OrderID,
			ClientOrderID: order.ClientOID,
			Symbol:        order.InstrumentName,
			Side:          side,
			Price:         order.Price,
			Quantity:      order.Quantity,
			ExecutedQty:   order.ExecutedQty,
			Status:        OrderStatus(order.Status),
			UpdateTime:    order.UpdateTime,
		})
	}
	return result, nil
}

func (w *cryptocomWrapper) GetAccount(ctx context.Context) (*Account, error) {
	account, err := w.adapter.GetAccount(ctx)
	if err != nil {
		return nil, err
	}

	return &Account{
		TotalWalletBalance: account.TotalWalletBalance,
		TotalMarginBalance: account.TotalMarginBalance,
		AvailableBalance:   account.AvailableBalance,
	}, nil
}

func (w *cryptocomWrapper) GetPositions(ctx context.Context, symbol string) ([]*Position, error) {
	return []*Position{}, nil
}

func (w *cryptocomWrapper) GetBalance(ctx context.Context, asset string) (float64, error) {
	return w.adapter.GetBalance(ctx)
}

func (w *cryptocomWrapper) StartOrderStream(ctx context.Context, callback func(interface{})) error {
	return nil // WebSocket 占位符
}

func (w *cryptocomWrapper) StopOrderStream() error {
	return nil
}

func (w *cryptocomWrapper) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	return w.adapter.GetLatestPrice(ctx)
}

func (w *cryptocomWrapper) StartPriceStream(ctx context.Context, symbol string, callback func(price float64)) error {
	return nil // WebSocket 占位符
}

func (w *cryptocomWrapper) StartKlineStream(ctx context.Context, symbols []string, interval string, callback CandleUpdateCallback) error {
	return nil // WebSocket 占位符
}

func (w *cryptocomWrapper) StopKlineStream() error {
	return nil
}

func (w *cryptocomWrapper) GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*Candle, error) {
	klines, err := w.adapter.GetHistoricalKlines(ctx, interval, limit)
	if err != nil {
		return nil, err
	}

	result := make([]*Candle, 0, len(klines))
	for _, kline := range klines {
		result = append(result, &Candle{
			Symbol:    kline.Symbol,
			Open:      kline.Open,
			High:      kline.High,
			Low:       kline.Low,
			Close:     kline.Close,
			Volume:    kline.Volume,
			Timestamp: kline.Timestamp,
		})
	}
	return result, nil
}

func (w *cryptocomWrapper) GetPriceDecimals() int {
	return w.adapter.GetPriceDecimals()
}

func (w *cryptocomWrapper) GetQuantityDecimals() int {
	return w.adapter.GetQuantityDecimals()
}

func (w *cryptocomWrapper) GetBaseAsset() string {
	return w.adapter.GetBaseAsset()
}

func (w *cryptocomWrapper) GetQuoteAsset() string {
	return w.adapter.GetQuoteAsset()
}

func (w *cryptocomWrapper) GetFundingRate(ctx context.Context, symbol string) (float64, error) {
	return w.adapter.GetFundingRate(ctx)
}


// GetSpotPrice 获取现货市场价格（未实现）
func (w *cryptocomWrapper) GetSpotPrice(ctx context.Context, symbol string) (float64, error) {
	return 0, ErrNotImplemented
}
