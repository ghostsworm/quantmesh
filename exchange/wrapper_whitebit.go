package exchange

import (
	"context"

	"quantmesh/exchange/income"
	"quantmesh/exchange/whitebit"
)

// whitebitWrapper WhiteBIT 包装器
type whitebitWrapper struct {
	adapter *whitebit.WhiteBITAdapter
}

// GetName 獲取交易所名称
func (w *whitebitWrapper) GetName() string {
	return w.adapter.GetName()
}

func (w *whitebitWrapper) GetMarketType() string {
	return w.adapter.GetMarketType()
}

// PlaceOrder 下單
func (w *whitebitWrapper) PlaceOrder(ctx context.Context, req *OrderRequest) (*Order, error) {
	whitebitReq := &whitebit.OrderRequest{
		Symbol:        req.Symbol,
		Side:          whitebit.Side(req.Side),
		Type:          whitebit.OrderType(req.Type),
		TimeInForce:   string(req.TimeInForce),
		Quantity:      req.Quantity,
		Price:         req.Price,
		ReduceOnly:    req.ReduceOnly,
		PostOnly:      req.PostOnly,
		PriceDecimals: req.PriceDecimals,
		ClientOrderID: req.ClientOrderID,
	}

	order, err := w.adapter.PlaceOrder(ctx, whitebitReq)
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

// BatchPlaceOrders 批量下單
func (w *whitebitWrapper) BatchPlaceOrders(ctx context.Context, orders []*OrderRequest) ([]*Order, bool) {
	whitebitOrders := make([]*whitebit.OrderRequest, len(orders))
	for i, req := range orders {
		whitebitOrders[i] = &whitebit.OrderRequest{
			Symbol:        req.Symbol,
			Side:          whitebit.Side(req.Side),
			Type:          whitebit.OrderType(req.Type),
			TimeInForce:   string(req.TimeInForce),
			Quantity:      req.Quantity,
			Price:         req.Price,
			ReduceOnly:    req.ReduceOnly,
			PostOnly:      req.PostOnly,
			PriceDecimals: req.PriceDecimals,
			ClientOrderID: req.ClientOrderID,
		}
	}

	placedOrders, hasMarginError := w.adapter.BatchPlaceOrders(ctx, whitebitOrders)

	result := make([]*Order, len(placedOrders))
	for i, order := range placedOrders {
		result[i] = &Order{
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
		}
	}

	return result, hasMarginError
}

// CancelOrder 取消訂單
func (w *whitebitWrapper) CancelOrder(ctx context.Context, symbol string, orderID int64) error {
	return w.adapter.CancelOrder(ctx, symbol, orderID)
}

// BatchCancelOrders 批量取消訂單
func (w *whitebitWrapper) BatchCancelOrders(ctx context.Context, symbol string, orderIDs []int64) error {
	return w.adapter.BatchCancelOrders(ctx, symbol, orderIDs)
}

// CancelAllOrders 取消所有订單
func (w *whitebitWrapper) CancelAllOrders(ctx context.Context, symbol string) error {
	return w.adapter.CancelAllOrders(ctx, symbol)
}

// GetOrder 查詢訂單
func (w *whitebitWrapper) GetOrder(ctx context.Context, symbol string, orderID int64) (*Order, error) {
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

// GetOpenOrders 查詢未完成订單
func (w *whitebitWrapper) GetOpenOrders(ctx context.Context, symbol string) ([]*Order, error) {
	orders, err := w.adapter.GetOpenOrders(ctx, symbol)
	if err != nil {
		return nil, err
	}

	result := make([]*Order, len(orders))
	for i, order := range orders {
		result[i] = &Order{
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
		}
	}

	return result, nil
}

// GetAccount 獲取帳戶信息
func (w *whitebitWrapper) GetAccount(ctx context.Context) (*Account, error) {
	account, err := w.adapter.GetAccount(ctx)
	if err != nil {
		return nil, err
	}

	positions := make([]*Position, len(account.Positions))
	for i, pos := range account.Positions {
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
		TotalWalletBalance: account.TotalWalletBalance,
		TotalMarginBalance: account.TotalMarginBalance,
		AvailableBalance:   account.AvailableBalance,
		Positions:          positions,
	}, nil
}

// GetPositions 獲取持倉信息
func (w *whitebitWrapper) GetPositions(ctx context.Context, symbol string) ([]*Position, error) {
	positions, err := w.adapter.GetPositions(ctx, symbol)
	if err != nil {
		return nil, err
	}

	result := make([]*Position, len(positions))
	for i, pos := range positions {
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

	return result, nil
}

// GetBalance 獲取餘額
func (w *whitebitWrapper) GetBalance(ctx context.Context, asset string) (float64, error) {
	return w.adapter.GetBalance(ctx, asset)
}

// StartOrderStream 啟動訂單流
func (w *whitebitWrapper) StartOrderStream(ctx context.Context, callback func(interface{})) error {
	return w.adapter.StartOrderStream(ctx, callback)
}

// StopOrderStream 停止訂單流
func (w *whitebitWrapper) StopOrderStream() error {
	return w.adapter.StopOrderStream()
}

// GetLatestPrice 獲取最新價格
func (w *whitebitWrapper) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	return w.adapter.GetLatestPrice(ctx, symbol)
}

// StartPriceStream 啟動價格流
func (w *whitebitWrapper) StartPriceStream(ctx context.Context, symbol string, callback func(price float64)) error {
	return w.adapter.StartPriceStream(ctx, symbol, callback)
}

// StartKlineStream 啟動K線流
func (w *whitebitWrapper) StartKlineStream(ctx context.Context, symbols []string, interval string, callback CandleUpdateCallback) error {
	whitebitCallback := func(candle interface{}) {
		if whitebitCandle, ok := candle.(whitebit.Candle); ok {
			genericCandle := &Candle{
				Symbol:    whitebitCandle.Symbol,
				Open:      whitebitCandle.Open,
				High:      whitebitCandle.High,
				Low:       whitebitCandle.Low,
				Close:     whitebitCandle.Close,
				Volume:    whitebitCandle.Volume,
				Timestamp: whitebitCandle.Timestamp,
				IsClosed:  whitebitCandle.IsClosed,
			}
			callback(genericCandle)
		}
	}
	return w.adapter.StartKlineStream(ctx, symbols, interval, whitebitCallback)
}

// StopKlineStream 停止K線流
func (w *whitebitWrapper) StopKlineStream() error {
	return w.adapter.StopKlineStream()
}

// GetHistoricalKlines 獲取歷史K線數據
func (w *whitebitWrapper) GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*Candle, error) {
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

// GetPriceDecimals 獲取價格精度
func (w *whitebitWrapper) GetPriceDecimals() int {
	return w.adapter.GetPriceDecimals()
}

// GetQuantityDecimals 獲取數量精度
func (w *whitebitWrapper) GetQuantityDecimals() int {
	return w.adapter.GetQuantityDecimals()
}

// GetBaseAsset 獲取基础资產
func (w *whitebitWrapper) GetBaseAsset() string {
	return w.adapter.GetBaseAsset()
}

// GetQuoteAsset 獲取计價资產
func (w *whitebitWrapper) GetQuoteAsset() string {
	return w.adapter.GetQuoteAsset()
}

// GetFundingRate 獲取资金费率
func (w *whitebitWrapper) GetFundingRate(ctx context.Context, symbol string) (float64, error) {
	return w.adapter.GetFundingRate(ctx, symbol)
}

func (w *whitebitWrapper) GetFundingInfo(ctx context.Context, symbol string) (*FundingInfo, error) {
	return nil, ErrNotImplemented
}

// GetIncomeHistory 獲取收入歷史
func (w *whitebitWrapper) GetIncomeHistory(ctx context.Context, symbol, incomeType string, startTime, endTime int64) ([]*income.Income, error) {
	return nil, nil
}

// GetOrderFills 查詢訂單成交記錄
func (w *whitebitWrapper) GetOrderFills(ctx context.Context, symbol string, orderID int64) ([]*OrderFill, error) {
	return nil, nil
}

// GetSpotPrice 獲取現貨市场價格
func (w *whitebitWrapper) GetSpotPrice(ctx context.Context, symbol string) (float64, error) {
	return w.adapter.GetSpotPrice(ctx, symbol)
}

// EstimateFinalOrderAmount 預估最终下單金額
func (w *whitebitWrapper) EstimateFinalOrderAmount(symbol string, price, quantity float64, reduceOnly bool) float64 {
	return w.adapter.EstimateFinalOrderAmount(symbol, price, quantity, reduceOnly)
}

// GetOrderBook 獲取訂單簿深度
func (w *whitebitWrapper) GetOrderBook(ctx context.Context, symbol string, limit int) (*OrderBook, error) {
	whitebitOrderBook, err := w.adapter.GetOrderBook(ctx, symbol, limit)
	if err != nil {
		return nil, err
	}

	bids := make([]OrderBookLevel, len(whitebitOrderBook.Bids))
	for i, bid := range whitebitOrderBook.Bids {
		bids[i] = OrderBookLevel{
			Price:    bid.Price,
			Quantity: bid.Quantity,
		}
	}

	asks := make([]OrderBookLevel, len(whitebitOrderBook.Asks))
	for i, ask := range whitebitOrderBook.Asks {
		asks[i] = OrderBookLevel{
			Price:    ask.Price,
			Quantity: ask.Quantity,
		}
	}

	return &OrderBook{
		Symbol:    symbol,
		Bids:      bids,
		Asks:      asks,
		Timestamp: whitebitOrderBook.Timestamp,
	}, nil
}

// InternalTransfer 交易所內部轉帳
func (w *whitebitWrapper) InternalTransfer(ctx context.Context, fromAccount, toAccount, asset string, amount float64) (string, error) {
	return w.adapter.InternalTransfer(ctx, fromAccount, toAccount, asset, amount)
}
