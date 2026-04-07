package exchange

import (
	"context"

	"quantmesh/exchange/gate"
	"quantmesh/exchange/income"
	"quantmesh/utils"
)

// gateWrapper 包装 Gate.io 适配器以實現 IExchange 接口
type gateWrapper struct {
	adapter *gate.GateAdapter
}

func (w *gateWrapper) GetName() string {
	return w.adapter.GetName()
}

func (w *gateWrapper) GetMarketType() string {
	return w.adapter.GetMarketType()
}

func (w *gateWrapper) PlaceOrder(ctx context.Context, req *OrderRequest) (*Order, error) {
	// 轉换请求類型
	gateReq := &gate.OrderRequest{
		Symbol:        req.Symbol,
		Side:          gate.Side(req.Side),
		Type:          gate.OrderType(req.Type),
		TimeInForce:   gate.TimeInForce(req.TimeInForce),
		Quantity:      req.Quantity,
		Price:         req.Price,
		ReduceOnly:    req.ReduceOnly,
		PostOnly:      req.PostOnly,
		PriceDecimals: req.PriceDecimals,
		ClientOrderID: req.ClientOrderID,
	}

	gateOrder, err := w.adapter.PlaceOrder(ctx, gateReq)
	if err != nil {
		return nil, err
	}

	// 轉换返回類型，使用统一的 utils 包去掉 Gate.io 的 t- 前缀
	clientOrderID := utils.RemoveBrokerPrefix("gate", gateOrder.ClientOrderID)

	return &Order{
		OrderID:       gateOrder.OrderID,
		ClientOrderID: clientOrderID,
		Symbol:        gateOrder.Symbol,
		Side:          Side(gateOrder.Side),
		Type:          OrderType(gateOrder.Type),
		Price:         gateOrder.Price,
		Quantity:      gateOrder.Quantity,
		ExecutedQty:   gateOrder.ExecutedQty,
		AvgPrice:      gateOrder.AvgPrice,
		Status:        OrderStatus(gateOrder.Status),
		CreatedAt:     gateOrder.CreatedAt,
		UpdateTime:    gateOrder.UpdateTime,
	}, nil
}

func (w *gateWrapper) BatchPlaceOrders(ctx context.Context, orders []*OrderRequest) ([]*Order, bool) {
	gateOrders := make([]*gate.OrderRequest, len(orders))
	for i, req := range orders {
		gateOrders[i] = &gate.OrderRequest{
			Symbol:        req.Symbol,
			Side:          gate.Side(req.Side),
			Type:          gate.OrderType(req.Type),
			TimeInForce:   gate.TimeInForce(req.TimeInForce),
			Quantity:      req.Quantity,
			Price:         req.Price,
			ReduceOnly:    req.ReduceOnly,
			PostOnly:      req.PostOnly,
			PriceDecimals: req.PriceDecimals,
			ClientOrderID: req.ClientOrderID,
		}
	}

	gateResult, hasMarginError := w.adapter.BatchPlaceOrders(ctx, gateOrders)

	result := make([]*Order, len(gateResult))
	for i, ord := range gateResult {
		// 使用统一的 utils 包去掉 Gate.io 的 t- 前缀
		clientOrderID := utils.RemoveBrokerPrefix("gate", ord.ClientOrderID)

		result[i] = &Order{
			OrderID:       ord.OrderID,
			ClientOrderID: clientOrderID,
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

func (w *gateWrapper) CancelOrder(ctx context.Context, symbol string, orderID int64) error {
	return w.adapter.CancelOrder(ctx, symbol, orderID)
}

func (w *gateWrapper) BatchCancelOrders(ctx context.Context, symbol string, orderIDs []int64) error {
	return w.adapter.BatchCancelOrders(ctx, symbol, orderIDs)
}

// CancelAllOrders 撤销所有订單（Gate.io實現）
// 查詢所有未完成订單后批量撤銷
func (w *gateWrapper) CancelAllOrders(ctx context.Context, symbol string) error {
	// 1. 查詢所有未完成订單
	openOrders, err := w.adapter.GetOpenOrders(ctx, symbol)
	if err != nil {
		return err
	}

	if len(openOrders) == 0 {
		return nil // 没有订單需要撤销
	}

	// 2. 提取所有订單ID
	orderIDs := make([]int64, len(openOrders))
	for i, order := range openOrders {
		orderIDs[i] = order.OrderID
	}

	// 3. 批量撤銷（adapter會自动分批处理）
	return w.adapter.BatchCancelOrders(ctx, symbol, orderIDs)
}

func (w *gateWrapper) GetOrder(ctx context.Context, symbol string, orderID int64) (*Order, error) {
	gateOrder, err := w.adapter.GetOrder(ctx, symbol, orderID)
	if err != nil {
		return nil, err
	}

	// 使用统一的 utils 包去掉 Gate.io 的 t- 前缀
	clientOrderID := utils.RemoveBrokerPrefix("gate", gateOrder.ClientOrderID)

	return &Order{
		OrderID:       gateOrder.OrderID,
		ClientOrderID: clientOrderID,
		Symbol:        gateOrder.Symbol,
		Side:          Side(gateOrder.Side),
		Type:          OrderType(gateOrder.Type),
		Price:         gateOrder.Price,
		Quantity:      gateOrder.Quantity,
		ExecutedQty:   gateOrder.ExecutedQty,
		AvgPrice:      gateOrder.AvgPrice,
		Status:        OrderStatus(gateOrder.Status),
		CreatedAt:     gateOrder.CreatedAt,
		UpdateTime:    gateOrder.UpdateTime,
	}, nil
}

func (w *gateWrapper) GetOpenOrders(ctx context.Context, symbol string) ([]*Order, error) {
	gateOrders, err := w.adapter.GetOpenOrders(ctx, symbol)
	if err != nil {
		return nil, err
	}

	orders := make([]*Order, len(gateOrders))
	for i, ord := range gateOrders {
		// 使用统一的 utils 包去掉 Gate.io 的 t- 前缀
		clientOrderID := utils.RemoveBrokerPrefix("gate", ord.ClientOrderID)

		orders[i] = &Order{
			OrderID:       ord.OrderID,
			ClientOrderID: clientOrderID,
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

func (w *gateWrapper) GetAccount(ctx context.Context) (*Account, error) {
	gateAccount, err := w.adapter.GetAccount(ctx)
	if err != nil {
		return nil, err
	}

	positions := make([]*Position, len(gateAccount.Positions))
	for i, pos := range gateAccount.Positions {
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
		TotalWalletBalance: gateAccount.TotalWalletBalance,
		TotalMarginBalance: gateAccount.TotalMarginBalance,
		AvailableBalance:   gateAccount.AvailableBalance,
		Positions:          positions,
		AccountLeverage:    gateAccount.AccountLeverage,
	}, nil
}

func (w *gateWrapper) GetPositions(ctx context.Context, symbol string) ([]*Position, error) {
	gatePositions, err := w.adapter.GetPositions(ctx, symbol)
	if err != nil {
		return nil, err
	}

	positions := make([]*Position, len(gatePositions))
	for i, pos := range gatePositions {
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

func (w *gateWrapper) GetBalance(ctx context.Context, asset string) (float64, error) {
	return w.adapter.GetBalance(ctx, asset)
}

func (w *gateWrapper) StartOrderStream(ctx context.Context, callback func(interface{})) error {
	return w.adapter.StartOrderStream(ctx, callback)
}

func (w *gateWrapper) StopOrderStream() error {
	return w.adapter.StopOrderStream()
}

func (w *gateWrapper) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	return w.adapter.GetLatestPrice(ctx, symbol)
}

func (w *gateWrapper) StartPriceStream(ctx context.Context, symbol string, callback func(price float64)) error {
	return w.adapter.StartPriceStream(ctx, func(s string, price float64) {
		callback(price)
	})
}

func (w *gateWrapper) StartKlineStream(ctx context.Context, symbols []string, interval string, callback CandleUpdateCallback) error {
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

func (w *gateWrapper) StopKlineStream() error {
	w.adapter.StopKlineStream()
	return nil
}

func (w *gateWrapper) GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*Candle, error) {
	gateCandles, err := w.adapter.GetHistoricalKlines(ctx, symbol, interval, limit)
	if err != nil {
		return nil, err
	}

	// 轉换類型
	candles := make([]*Candle, len(gateCandles))
	for i, gc := range gateCandles {
		candles[i] = &Candle{
			Symbol:    gc.Symbol,
			Open:      gc.Open,
			High:      gc.High,
			Low:       gc.Low,
			Close:     gc.Close,
			Volume:    gc.Volume,
			Timestamp: gc.Timestamp,
			IsClosed:  gc.IsClosed,
		}
	}

	return candles, nil
}

func (w *gateWrapper) GetPriceDecimals() int {
	return w.adapter.GetPriceDecimals()
}

func (w *gateWrapper) GetQuantityDecimals() int {
	return w.adapter.GetQuantityDecimals()
}

func (w *gateWrapper) GetBaseAsset() string {
	return w.adapter.GetBaseAsset()
}

func (w *gateWrapper) GetQuoteAsset() string {
	return w.adapter.GetQuoteAsset()
}

func (w *gateWrapper) GetFundingRate(ctx context.Context, symbol string) (float64, error) {
	return w.adapter.GetFundingRate(ctx, symbol)
}

func (w *gateWrapper) GetFundingInfo(ctx context.Context, symbol string) (*FundingInfo, error) {
	info, err := w.adapter.GetFundingInfo(ctx, symbol)
	if err != nil {
		return nil, err
	}
	return &FundingInfo{
		Symbol:          info.Symbol,
		Rate:            info.Rate,
		NextFundingTime: info.NextFundingTime,
		MarkPrice:       info.MarkPrice,
		IndexPrice:      info.IndexPrice,
	}, nil
}

func (w *gateWrapper) GetIncomeHistory(ctx context.Context, symbol, incomeType string, startTime, endTime int64) ([]*income.Income, error) {
	return nil, nil
}

// GetOrderFills 查詢訂單成交記錄（Gate.io WebSocket 已提供手續費，此方法可選實現）
func (w *gateWrapper) GetOrderFills(ctx context.Context, symbol string, orderID int64) ([]*OrderFill, error) {
	return nil, nil
}

// GetSpotPrice 獲取現貨市场價格
func (w *gateWrapper) GetSpotPrice(ctx context.Context, symbol string) (float64, error) {
	return w.adapter.GetSpotPrice(ctx, symbol)
}

// EstimateFinalOrderAmount 預估最终下單金額（默认實現：返回原始金額）
func (w *gateWrapper) EstimateFinalOrderAmount(symbol string, price, quantity float64, reduceOnly bool) float64 {
	return price * quantity
}

// GetOrderBook 獲取訂單簿深度
func (w *gateWrapper) GetOrderBook(ctx context.Context, symbol string, limit int) (*OrderBook, error) {
	gateOrderBook, err := w.adapter.GetOrderBook(ctx, symbol, limit)
	if err != nil {
		return nil, err
	}

	// 轉换買盘數據
	bids := make([]OrderBookLevel, len(gateOrderBook.Bids))
	for i, bid := range gateOrderBook.Bids {
		bids[i] = OrderBookLevel{
			Price:    bid.Price,
			Quantity: bid.Quantity,
		}
	}

	// 轉换賣盘數據
	asks := make([]OrderBookLevel, len(gateOrderBook.Asks))
	for i, ask := range gateOrderBook.Asks {
		asks[i] = OrderBookLevel{
			Price:    ask.Price,
			Quantity: ask.Quantity,
		}
	}

	return &OrderBook{
		Symbol:    symbol,
		Bids:      bids,
		Asks:      asks,
		Timestamp: gateOrderBook.Timestamp,
	}, nil
}

// InternalTransfer 交易所內部轉帳
func (w *gateWrapper) InternalTransfer(ctx context.Context, fromAccount, toAccount, asset string, amount float64) (string, error) {
	return w.adapter.InternalTransfer(ctx, fromAccount, toAccount, asset, amount)
}
