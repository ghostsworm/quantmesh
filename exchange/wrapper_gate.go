package exchange

import (
	"context"
	"opensqt/exchange/gate"
	"opensqt/utils"
)

// gateWrapper 包装 Gate.io 适配器以实现 IExchange 接口
type gateWrapper struct {
	adapter *gate.GateAdapter
}

func (w *gateWrapper) GetName() string {
	return w.adapter.GetName()
}

func (w *gateWrapper) PlaceOrder(ctx context.Context, req *OrderRequest) (*Order, error) {
	// 转换请求类型
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

	// 转换返回类型，使用统一的 utils 包去掉 Gate.io 的 t- 前缀
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

// CancelAllOrders 撤销所有订单（Gate.io实现）
// 查询所有未完成订单后批量撤销
func (w *gateWrapper) CancelAllOrders(ctx context.Context, symbol string) error {
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

	// 转换类型
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
	// 从 adapter 获取价格精度
	return 2 // 默认值，实际应从 adapter 获取
}

func (w *gateWrapper) GetQuantityDecimals() int {
	// 从 adapter 获取数量精度
	return w.adapter.GetQuantityDecimals()
}

func (w *gateWrapper) GetBaseAsset() string {
	// 从交易对中提取基础资产
	return ""
}

func (w *gateWrapper) GetQuoteAsset() string {
	// 从交易对中提取计价资产
	return "USDT"
}
