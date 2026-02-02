package exchange

import (
	"context"

	"quantmesh/exchange/gate"
	"quantmesh/exchange/income"
)

// gateSpotWrapper 包装 Gate 現貨适配器以實現 IExchange 接口
type gateSpotWrapper struct {
	adapter *gate.GateSpotAdapter
}

func (w *gateSpotWrapper) GetName() string {
	return w.adapter.GetName()
}

func (w *gateSpotWrapper) GetMarketType() string {
	return w.adapter.GetMarketType()
}

func (w *gateSpotWrapper) PlaceOrder(ctx context.Context, req *OrderRequest) (*Order, error) {
	gateReq := &gate.OrderRequest{
		Symbol:        req.Symbol,
		Side:          gate.Side(req.Side),
		Type:          gate.OrderType(req.Type),
		TimeInForce:   gate.TimeInForce(req.TimeInForce),
		Quantity:      req.Quantity,
		Price:         req.Price,
		ReduceOnly:    false,
		PostOnly:      req.PostOnly,
		PriceDecimals: req.PriceDecimals,
		ClientOrderID: req.ClientOrderID,
	}
	order, err := w.adapter.PlaceOrder(ctx, gateReq)
	if err != nil {
		return nil, err
	}
	return &Order{
		OrderID:       order.OrderID,
		ClientOrderID: order.ClientOrderID,
		Symbol:        order.Symbol,
		Side:          Side(order.Side),
		Type:          OrderType(order.Type),
		Price:         order.Price,
		Quantity:      order.Quantity,
		ExecutedQty:   order.ExecutedQty,
		AvgPrice:      order.AvgPrice,
		Status:        OrderStatus(order.Status),
		CreatedAt:     order.CreatedAt,
		UpdateTime:    order.UpdateTime,
	}, nil
}

func (w *gateSpotWrapper) BatchPlaceOrders(ctx context.Context, orders []*OrderRequest) ([]*Order, bool) {
	gateOrders := make([]*gate.OrderRequest, len(orders))
	for i, req := range orders {
		gateOrders[i] = &gate.OrderRequest{
			Symbol:        req.Symbol,
			Side:          gate.Side(req.Side),
			Type:          gate.OrderType(req.Type),
			TimeInForce:   gate.TimeInForce(req.TimeInForce),
			Quantity:      req.Quantity,
			Price:         req.Price,
			ReduceOnly:    false,
			PostOnly:      req.PostOnly,
			PriceDecimals: req.PriceDecimals,
			ClientOrderID: req.ClientOrderID,
		}
	}
	placed, hasErr := w.adapter.BatchPlaceOrders(ctx, gateOrders)
	result := make([]*Order, len(placed))
	for i, ord := range placed {
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
	return result, hasErr
}

func (w *gateSpotWrapper) CancelOrder(ctx context.Context, symbol string, orderID int64) error {
	return w.adapter.CancelOrder(ctx, symbol, orderID)
}

func (w *gateSpotWrapper) BatchCancelOrders(ctx context.Context, symbol string, orderIDs []int64) error {
	return w.adapter.BatchCancelOrders(ctx, symbol, orderIDs)
}

func (w *gateSpotWrapper) CancelAllOrders(ctx context.Context, symbol string) error {
	return w.adapter.CancelAllOrders(ctx, symbol)
}

func (w *gateSpotWrapper) GetOrder(ctx context.Context, symbol string, orderID int64) (*Order, error) {
	order, err := w.adapter.GetOrder(ctx, symbol, orderID)
	if err != nil {
		return nil, err
	}
	return &Order{
		OrderID:       order.OrderID,
		ClientOrderID: order.ClientOrderID,
		Symbol:        order.Symbol,
		Side:          Side(order.Side),
		Type:          OrderType(order.Type),
		Price:         order.Price,
		Quantity:      order.Quantity,
		ExecutedQty:   order.ExecutedQty,
		AvgPrice:      order.AvgPrice,
		Status:        OrderStatus(order.Status),
		UpdateTime:    order.UpdateTime,
	}, nil
}

func (w *gateSpotWrapper) GetOpenOrders(ctx context.Context, symbol string) ([]*Order, error) {
	orders, err := w.adapter.GetOpenOrders(ctx, symbol)
	if err != nil {
		return nil, err
	}
	result := make([]*Order, len(orders))
	for i, ord := range orders {
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
			UpdateTime:    ord.UpdateTime,
		}
	}
	return result, nil
}

func (w *gateSpotWrapper) GetAccount(ctx context.Context) (*Account, error) {
	account, err := w.adapter.GetAccount(ctx)
	if err != nil {
		return nil, err
	}
	return &Account{
		TotalWalletBalance: account.TotalWalletBalance,
		TotalMarginBalance: account.TotalMarginBalance,
		AvailableBalance:   account.AvailableBalance,
		Positions:          nil,
	}, nil
}

func (w *gateSpotWrapper) GetPositions(ctx context.Context, symbol string) ([]*Position, error) {
	positions, err := w.adapter.GetPositions(ctx, symbol)
	if err != nil {
		return nil, err
	}
	result := make([]*Position, len(positions))
	for i, pos := range positions {
		if pos != nil {
			result[i] = &Position{
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
	}
	return result, nil
}

func (w *gateSpotWrapper) GetBalance(ctx context.Context, asset string) (float64, error) {
	return w.adapter.GetBalance(ctx, asset)
}

func (w *gateSpotWrapper) StartOrderStream(ctx context.Context, callback func(interface{})) error {
	return w.adapter.StartOrderStream(ctx, callback)
}

func (w *gateSpotWrapper) StopOrderStream() error {
	return w.adapter.StopOrderStream()
}

func (w *gateSpotWrapper) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	return w.adapter.GetLatestPrice(ctx, symbol)
}

func (w *gateSpotWrapper) StartPriceStream(ctx context.Context, symbol string, callback func(price float64)) error {
	return w.adapter.StartPriceStream(ctx, symbol, callback)
}

func (w *gateSpotWrapper) StartKlineStream(ctx context.Context, symbols []string, interval string, callback CandleUpdateCallback) error {
	return w.adapter.StartKlineStream(ctx, symbols, interval, func(candle interface{}) {
		if c, ok := candle.(*gate.Candle); ok {
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

func (w *gateSpotWrapper) StopKlineStream() error {
	return w.adapter.StopKlineStream()
}

func (w *gateSpotWrapper) GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*Candle, error) {
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

func (w *gateSpotWrapper) GetPriceDecimals() int {
	return w.adapter.GetPriceDecimals()
}

func (w *gateSpotWrapper) GetQuantityDecimals() int {
	return w.adapter.GetQuantityDecimals()
}

func (w *gateSpotWrapper) GetBaseAsset() string {
	return w.adapter.GetBaseAsset()
}

func (w *gateSpotWrapper) GetQuoteAsset() string {
	return w.adapter.GetQuoteAsset()
}

func (w *gateSpotWrapper) GetFundingRate(ctx context.Context, symbol string) (float64, error) {
	return w.adapter.GetFundingRate(ctx, symbol)
}

func (w *gateSpotWrapper) GetIncomeHistory(ctx context.Context, symbol, incomeType string, startTime, endTime int64) ([]*income.Income, error) {
	return nil, nil
}

// GetOrderFills 查詢訂單成交記錄（暂未實現）
func (w *gate_spotWrapper) GetOrderFills(ctx context.Context, symbol string, orderID int64) ([]*OrderFill, error) {
	return nil, nil
}

func (w *gateSpotWrapper) GetSpotPrice(ctx context.Context, symbol string) (float64, error) {
	return w.adapter.GetSpotPrice(ctx, symbol)
}

func (w *gateSpotWrapper) EstimateFinalOrderAmount(symbol string, price, quantity float64, reduceOnly bool) float64 {
	return w.adapter.EstimateFinalOrderAmount(symbol, price, quantity, reduceOnly)
}

func (w *gateSpotWrapper) GetOrderBook(ctx context.Context, symbol string, limit int) (*OrderBook, error) {
	ob, err := w.adapter.GetOrderBook(ctx, symbol, limit)
	if err != nil {
		return nil, err
	}
	bids := make([]OrderBookLevel, len(ob.Bids))
	for i, b := range ob.Bids {
		bids[i] = OrderBookLevel{Price: b.Price, Quantity: b.Quantity}
	}
	asks := make([]OrderBookLevel, len(ob.Asks))
	for i, a := range ob.Asks {
		asks[i] = OrderBookLevel{Price: a.Price, Quantity: a.Quantity}
	}
	return &OrderBook{
		Symbol:    symbol,
		Bids:      bids,
		Asks:      asks,
		Timestamp: ob.Timestamp,
	}, nil
}

func (w *gateSpotWrapper) InternalTransfer(ctx context.Context, fromAccount, toAccount, asset string, amount float64) (string, error) {
	return w.adapter.InternalTransfer(ctx, fromAccount, toAccount, asset, amount)
}
