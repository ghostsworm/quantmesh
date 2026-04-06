package exchange

import (
	"context"

	"quantmesh/exchange/binance"
	"quantmesh/exchange/income"
)

// binanceSpotMarginWrapper 包裝 Binance 現貨槓桿適配器（借幣做空）
type binanceSpotMarginWrapper struct {
	adapter *binance.BinanceSpotMarginAdapter
}

func (w *binanceSpotMarginWrapper) GetName() string {
	return w.adapter.GetName()
}

func (w *binanceSpotMarginWrapper) GetMarketType() string {
	return w.adapter.GetMarketType()
}

func (w *binanceSpotMarginWrapper) PlaceOrder(ctx context.Context, req *OrderRequest) (*Order, error) {
	binanceReq := &binance.OrderRequest{
		Symbol:        req.Symbol,
		Side:          binance.Side(req.Side),
		Type:          binance.OrderType(req.Type),
		TimeInForce:   binance.TimeInForce(req.TimeInForce),
		Quantity:      req.Quantity,
		Price:         req.Price,
		ReduceOnly:    false,
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

func (w *binanceSpotMarginWrapper) BatchPlaceOrders(ctx context.Context, orders []*OrderRequest) ([]*Order, bool) {
	binanceOrders := make([]*binance.OrderRequest, len(orders))
	for i, req := range orders {
		binanceOrders[i] = &binance.OrderRequest{
			Symbol:        req.Symbol,
			Side:          binance.Side(req.Side),
			Type:          binance.OrderType(req.Type),
			TimeInForce:   binance.TimeInForce(req.TimeInForce),
			Quantity:      req.Quantity,
			Price:         req.Price,
			ReduceOnly:    false,
			PostOnly:      req.PostOnly,
			PriceDecimals: req.PriceDecimals,
			ClientOrderID: req.ClientOrderID,
		}
	}
	binanceResult, hasErr := w.adapter.BatchPlaceOrders(ctx, binanceOrders)
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
	return result, hasErr
}

func (w *binanceSpotMarginWrapper) CancelOrder(ctx context.Context, symbol string, orderID int64) error {
	return w.adapter.CancelOrder(ctx, symbol, orderID)
}

func (w *binanceSpotMarginWrapper) BatchCancelOrders(ctx context.Context, symbol string, orderIDs []int64) error {
	return w.adapter.BatchCancelOrders(ctx, symbol, orderIDs)
}

func (w *binanceSpotMarginWrapper) CancelAllOrders(ctx context.Context, symbol string) error {
	return w.adapter.CancelAllOrders(ctx, symbol)
}

func (w *binanceSpotMarginWrapper) GetOrder(ctx context.Context, symbol string, orderID int64) (*Order, error) {
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

func (w *binanceSpotMarginWrapper) GetOpenOrders(ctx context.Context, symbol string) ([]*Order, error) {
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

func (w *binanceSpotMarginWrapper) GetAccount(ctx context.Context) (*Account, error) {
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
		TotalMarginBalance:  binanceAccount.TotalMarginBalance,
		AvailableBalance:   binanceAccount.AvailableBalance,
		Positions:          positions,
	}, nil
}

func (w *binanceSpotMarginWrapper) GetPositions(ctx context.Context, symbol string) ([]*Position, error) {
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

func (w *binanceSpotMarginWrapper) GetBalance(ctx context.Context, asset string) (float64, error) {
	return w.adapter.GetBalance(ctx, asset)
}

func (w *binanceSpotMarginWrapper) StartOrderStream(ctx context.Context, callback func(interface{})) error {
	return w.adapter.StartOrderStream(ctx, callback)
}

func (w *binanceSpotMarginWrapper) StopOrderStream() error {
	return w.adapter.StopOrderStream()
}

func (w *binanceSpotMarginWrapper) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	return w.adapter.GetLatestPrice(ctx, symbol)
}

func (w *binanceSpotMarginWrapper) StartPriceStream(ctx context.Context, symbol string, callback func(price float64)) error {
	return w.adapter.StartPriceStream(ctx, symbol, callback)
}

func (w *binanceSpotMarginWrapper) StartKlineStream(ctx context.Context, symbols []string, interval string, callback CandleUpdateCallback) error {
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

func (w *binanceSpotMarginWrapper) StopKlineStream() error {
	return w.adapter.StopKlineStream()
}

func (w *binanceSpotMarginWrapper) GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*Candle, error) {
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

func (w *binanceSpotMarginWrapper) GetPriceDecimals() int {
	return w.adapter.GetPriceDecimals()
}

func (w *binanceSpotMarginWrapper) GetQuantityDecimals() int {
	return w.adapter.GetQuantityDecimals()
}

func (w *binanceSpotMarginWrapper) GetBaseAsset() string {
	return w.adapter.GetBaseAsset()
}

func (w *binanceSpotMarginWrapper) GetQuoteAsset() string {
	return w.adapter.GetQuoteAsset()
}

func (w *binanceSpotMarginWrapper) GetFundingRate(ctx context.Context, symbol string) (float64, error) {
	return w.adapter.GetFundingRate(ctx, symbol)
}

func (w *binanceSpotMarginWrapper) GetFundingInfo(ctx context.Context, symbol string) (*FundingInfo, error) {
	return nil, ErrNotImplemented
}

func (w *binanceSpotMarginWrapper) GetIncomeHistory(ctx context.Context, symbol, incomeType string, startTime, endTime int64) ([]*income.Income, error) {
	return nil, nil
}

func (w *binanceSpotMarginWrapper) GetOrderFills(ctx context.Context, symbol string, orderID int64) ([]*OrderFill, error) {
	return nil, nil
}

func (w *binanceSpotMarginWrapper) GetSpotPrice(ctx context.Context, symbol string) (float64, error) {
	return w.adapter.GetSpotPrice(ctx, symbol)
}

func (w *binanceSpotMarginWrapper) EstimateFinalOrderAmount(symbol string, price, quantity float64, reduceOnly bool) float64 {
	return w.adapter.EstimateFinalOrderAmount(symbol, price, quantity, reduceOnly)
}

func (w *binanceSpotMarginWrapper) GetOrderBook(ctx context.Context, symbol string, limit int) (*OrderBook, error) {
	binanceOrderBook, err := w.adapter.GetOrderBook(ctx, symbol, limit)
	if err != nil {
		return nil, err
	}
	bids := make([]OrderBookLevel, len(binanceOrderBook.Bids))
	for i, bid := range binanceOrderBook.Bids {
		bids[i] = OrderBookLevel{Price: bid.Price, Quantity: bid.Quantity}
	}
	asks := make([]OrderBookLevel, len(binanceOrderBook.Asks))
	for i, ask := range binanceOrderBook.Asks {
		asks[i] = OrderBookLevel{Price: ask.Price, Quantity: ask.Quantity}
	}
	return &OrderBook{
		Symbol:    symbol,
		Bids:      bids,
		Asks:      asks,
		Timestamp: binanceOrderBook.Timestamp,
	}, nil
}

func (w *binanceSpotMarginWrapper) InternalTransfer(ctx context.Context, fromAccount, toAccount, asset string, amount float64) (string, error) {
	return w.adapter.InternalTransfer(ctx, fromAccount, toAccount, asset, amount)
}

// Borrow 借幣（ISpotMarginExchange）
func (w *binanceSpotMarginWrapper) Borrow(ctx context.Context, asset string, amount float64) (int64, error) {
	return w.adapter.Borrow(ctx, asset, amount)
}

// Repay 還幣（ISpotMarginExchange）
func (w *binanceSpotMarginWrapper) Repay(ctx context.Context, asset string, amount float64) (int64, error) {
	return w.adapter.Repay(ctx, asset, amount)
}
