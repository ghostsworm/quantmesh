package exchange

import (
	"context"
	"strings"

	"quantmesh/exchange/income"
	"quantmesh/exchange/okx"
)

// okxSpotWrapper 包装 OKX 現貨适配器以實現 IExchange 接口
type okxSpotWrapper struct {
	adapter *okx.OKXSpotAdapter
}

func (w *okxSpotWrapper) GetName() string {
	return w.adapter.GetName()
}

func (w *okxSpotWrapper) GetMarketType() string {
	return w.adapter.GetMarketType()
}

func (w *okxSpotWrapper) PlaceOrder(ctx context.Context, req *OrderRequest) (*Order, error) {
	okxReq := &okx.OrderRequest{
		Symbol:        req.Symbol,
		Side:          okx.Side(strings.ToLower(string(req.Side))),
		Type:          okx.OrderType(strings.ToLower(string(req.Type))),
		TimeInForce:   okx.TimeInForce(req.TimeInForce),
		Quantity:      req.Quantity,
		Price:         req.Price,
		ReduceOnly:    false,
		PostOnly:      req.PostOnly,
		PriceDecimals: req.PriceDecimals,
		ClientOrderID: req.ClientOrderID,
	}
	order, err := w.adapter.PlaceOrder(ctx, okxReq)
	if err != nil {
		return nil, err
	}
	return okxSpotOrderToExchange(order), nil
}

func (w *okxSpotWrapper) BatchPlaceOrders(ctx context.Context, orders []*OrderRequest) ([]*Order, bool) {
	okxOrders := make([]*okx.OrderRequest, len(orders))
	for i, req := range orders {
		okxOrders[i] = &okx.OrderRequest{
			Symbol:        req.Symbol,
			Side:          okx.Side(strings.ToLower(string(req.Side))),
			Type:          okx.OrderType(strings.ToLower(string(req.Type))),
			TimeInForce:   okx.TimeInForce(req.TimeInForce),
			Quantity:      req.Quantity,
			Price:         req.Price,
			ReduceOnly:    false,
			PostOnly:      req.PostOnly,
			PriceDecimals: req.PriceDecimals,
			ClientOrderID: req.ClientOrderID,
		}
	}
	placed, hasErr := w.adapter.BatchPlaceOrders(ctx, okxOrders)
	result := make([]*Order, len(placed))
	for i, ord := range placed {
		result[i] = okxSpotOrderToExchange(ord)
	}
	return result, hasErr
}

func okxSpotOrderToExchange(ord *okx.Order) *Order {
	return &Order{
		OrderID:       ord.OrderID,
		ClientOrderID: ord.ClientOrderID,
		Symbol:        ord.Symbol,
		Side:          Side(strings.ToUpper(string(ord.Side))),
		Type:          OrderType(strings.ToUpper(string(ord.Type))),
		Price:         ord.Price,
		Quantity:      ord.Quantity,
		ExecutedQty:   ord.ExecutedQty,
		AvgPrice:      ord.AvgPrice,
		Status:        OrderStatus(okxSpotStatusToExchange(ord.Status)),
		CreatedAt:     ord.CreatedAt,
		UpdateTime:    ord.UpdateTime,
	}
}

func okxSpotStatusToExchange(s okx.OrderStatus) string {
	switch s {
	case okx.OrderStatusNew:
		return string(OrderStatusNew)
	case okx.OrderStatusPartiallyFilled:
		return string(OrderStatusPartiallyFilled)
	case okx.OrderStatusFilled:
		return string(OrderStatusFilled)
	case okx.OrderStatusCanceled:
		return string(OrderStatusCanceled)
	case okx.OrderStatusRejected:
		return string(OrderStatusRejected)
	case okx.OrderStatusExpired:
		return string(OrderStatusExpired)
	default:
		return string(OrderStatusNew)
	}
}

func (w *okxSpotWrapper) CancelOrder(ctx context.Context, symbol string, orderID int64) error {
	return w.adapter.CancelOrder(ctx, symbol, orderID)
}

func (w *okxSpotWrapper) BatchCancelOrders(ctx context.Context, symbol string, orderIDs []int64) error {
	return w.adapter.BatchCancelOrders(ctx, symbol, orderIDs)
}

func (w *okxSpotWrapper) CancelAllOrders(ctx context.Context, symbol string) error {
	return w.adapter.CancelAllOrders(ctx, symbol)
}

func (w *okxSpotWrapper) GetOrder(ctx context.Context, symbol string, orderID int64) (*Order, error) {
	ord, err := w.adapter.GetOrder(ctx, symbol, orderID)
	if err != nil {
		return nil, err
	}
	return okxSpotOrderToExchange(ord), nil
}

func (w *okxSpotWrapper) GetOpenOrders(ctx context.Context, symbol string) ([]*Order, error) {
	orders, err := w.adapter.GetOpenOrders(ctx, symbol)
	if err != nil {
		return nil, err
	}
	result := make([]*Order, len(orders))
	for i, ord := range orders {
		result[i] = okxSpotOrderToExchange(ord)
	}
	return result, nil
}

func (w *okxSpotWrapper) GetAccount(ctx context.Context) (*Account, error) {
	account, err := w.adapter.GetAccount(ctx)
	if err != nil {
		return nil, err
	}
	positions := make([]*Position, len(account.Positions))
	for i, pos := range account.Positions {
		if pos != nil {
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
	}
	return &Account{
		TotalWalletBalance: account.TotalWalletBalance,
		TotalMarginBalance: account.TotalMarginBalance,
		AvailableBalance:   account.AvailableBalance,
		Positions:          positions,
	}, nil
}

func (w *okxSpotWrapper) GetPositions(ctx context.Context, symbol string) ([]*Position, error) {
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

func (w *okxSpotWrapper) GetBalance(ctx context.Context, asset string) (float64, error) {
	return w.adapter.GetBalance(ctx, asset)
}

func (w *okxSpotWrapper) StartOrderStream(ctx context.Context, callback func(interface{})) error {
	return w.adapter.StartOrderStream(ctx, callback)
}

func (w *okxSpotWrapper) StopOrderStream() error {
	return w.adapter.StopOrderStream()
}

func (w *okxSpotWrapper) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	return w.adapter.GetLatestPrice(ctx, symbol)
}

func (w *okxSpotWrapper) StartPriceStream(ctx context.Context, symbol string, callback func(price float64)) error {
	return w.adapter.StartPriceStream(ctx, symbol, callback)
}

func (w *okxSpotWrapper) StartKlineStream(ctx context.Context, symbols []string, interval string, callback CandleUpdateCallback) error {
	return w.adapter.StartKlineStream(ctx, symbols, interval, func(candle interface{}) {
		if c, ok := candle.(okx.Candle); ok {
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

func (w *okxSpotWrapper) StopKlineStream() error {
	return w.adapter.StopKlineStream()
}

func (w *okxSpotWrapper) GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*Candle, error) {
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

func (w *okxSpotWrapper) GetPriceDecimals() int {
	return w.adapter.GetPriceDecimals()
}

func (w *okxSpotWrapper) GetQuantityDecimals() int {
	return w.adapter.GetQuantityDecimals()
}

func (w *okxSpotWrapper) GetBaseAsset() string {
	return w.adapter.GetBaseAsset()
}

func (w *okxSpotWrapper) GetQuoteAsset() string {
	return w.adapter.GetQuoteAsset()
}

func (w *okxSpotWrapper) GetFundingRate(ctx context.Context, symbol string) (float64, error) {
	return w.adapter.GetFundingRate(ctx, symbol)
}

func (w *okxSpotWrapper) GetFundingInfo(ctx context.Context, symbol string) (*FundingInfo, error) {
	return nil, ErrNotImplemented
}

func (w *okxSpotWrapper) GetIncomeHistory(ctx context.Context, symbol, incomeType string, startTime, endTime int64) ([]*income.Income, error) {
	return nil, nil
}

// GetOrderFills 查詢訂單成交記錄（暂未實現）
func (w *okxSpotWrapper) GetOrderFills(ctx context.Context, symbol string, orderID int64) ([]*OrderFill, error) {
	return nil, nil
}

func (w *okxSpotWrapper) GetSpotPrice(ctx context.Context, symbol string) (float64, error) {
	return w.adapter.GetSpotPrice(ctx, symbol)
}

func (w *okxSpotWrapper) EstimateFinalOrderAmount(symbol string, price, quantity float64, reduceOnly bool) float64 {
	return w.adapter.EstimateFinalOrderAmount(symbol, price, quantity, reduceOnly)
}

func (w *okxSpotWrapper) GetOrderBook(ctx context.Context, symbol string, limit int) (*OrderBook, error) {
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

func (w *okxSpotWrapper) InternalTransfer(ctx context.Context, fromAccount, toAccount, asset string, amount float64) (string, error) {
	return w.adapter.InternalTransfer(ctx, fromAccount, toAccount, asset, amount)
}
