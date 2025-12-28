package exchange

import (
	"context"
	"strconv"

	"quantmesh/exchange/deribit"
)

// deribitWrapper Deribit 包装器
type deribitWrapper struct {
	adapter *deribit.Adapter
}

// GetName 获取交易所名称
func (w *deribitWrapper) GetName() string {
	return w.adapter.GetName()
}

// PlaceOrder 下单
func (w *deribitWrapper) PlaceOrder(ctx context.Context, req *OrderRequest) (*Order, error) {
	// 转换类型
	var side deribit.OrderSide
	if req.Side == SideBuy {
		side = deribit.SideBuy
	} else {
		side = deribit.SideSell
	}

	order, err := w.adapter.PlaceOrder(ctx, side, req.Price, req.Quantity, req.ClientOrderID)
	if err != nil {
		return nil, err
	}

	// 转换回通用类型
	return &Order{
		OrderID:       0, // Deribit 使用字符串 ID
		ClientOrderID: order.ClientOrderID,
		Symbol:        order.Symbol,
		Side:          req.Side,
		Price:         order.Price,
		Quantity:      order.Quantity,
		ExecutedQty:   order.ExecutedQty,
		Status:        OrderStatus(order.Status),
		UpdateTime:    order.UpdateTime,
	}, nil
}

// BatchPlaceOrders 批量下单
func (w *deribitWrapper) BatchPlaceOrders(ctx context.Context, orders []*OrderRequest) ([]*Order, bool) {
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

// CancelOrder 取消订单
func (w *deribitWrapper) CancelOrder(ctx context.Context, symbol string, orderID int64) error {
	// Deribit 使用字符串 ID
	return w.adapter.CancelOrder(ctx, strconv.FormatInt(orderID, 10))
}

// BatchCancelOrders 批量取消订单
func (w *deribitWrapper) BatchCancelOrders(ctx context.Context, symbol string, orderIDs []int64) error {
	for _, orderID := range orderIDs {
		_ = w.adapter.CancelOrder(ctx, strconv.FormatInt(orderID, 10))
	}
	return nil
}

// CancelAllOrders 取消所有订单
func (w *deribitWrapper) CancelAllOrders(ctx context.Context, symbol string) error {
	orders, err := w.adapter.GetOpenOrders(ctx)
	if err != nil {
		return err
	}

	for _, order := range orders {
		_ = w.adapter.CancelOrder(ctx, order.OrderID)
	}

	return nil
}

// GetOrder 查询订单
func (w *deribitWrapper) GetOrder(ctx context.Context, symbol string, orderID int64) (*Order, error) {
	order, err := w.adapter.GetOrder(ctx, strconv.FormatInt(orderID, 10))
	if err != nil {
		return nil, err
	}

	var side Side
	if order.Side == deribit.SideBuy {
		side = SideBuy
	} else {
		side = SideSell
	}

	return &Order{
		OrderID:       0,
		ClientOrderID: order.ClientOrderID,
		Symbol:        order.Symbol,
		Side:          side,
		Price:         order.Price,
		Quantity:      order.Quantity,
		ExecutedQty:   order.ExecutedQty,
		Status:        OrderStatus(order.Status),
		UpdateTime:    order.UpdateTime,
	}, nil
}

// GetOpenOrders 获取活跃订单
func (w *deribitWrapper) GetOpenOrders(ctx context.Context, symbol string) ([]*Order, error) {
	orders, err := w.adapter.GetOpenOrders(ctx)
	if err != nil {
		return nil, err
	}

	result := make([]*Order, 0, len(orders))
	for _, order := range orders {
		var side Side
		if order.Side == deribit.SideBuy {
			side = SideBuy
		} else {
			side = SideSell
		}

		result = append(result, &Order{
			OrderID:       0,
			ClientOrderID: order.ClientOrderID,
			Symbol:        order.Symbol,
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

// GetAccount 获取账户信息
func (w *deribitWrapper) GetAccount(ctx context.Context) (*Account, error) {
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

// GetPositions 获取持仓
func (w *deribitWrapper) GetPositions(ctx context.Context, symbol string) ([]*Position, error) {
	positions, err := w.adapter.GetPositions(ctx)
	if err != nil {
		return nil, err
	}

	result := make([]*Position, 0, len(positions))
	for _, pos := range positions {
		result = append(result, &Position{
			Symbol:        pos.Symbol,
			Size:          pos.Size,
			EntryPrice:    pos.EntryPrice,
			MarkPrice:     pos.MarkPrice,
			UnrealizedPNL: pos.UnrealizedPNL,
			Leverage:      pos.Leverage,
		})
	}

	return result, nil
}

// GetBalance 获取余额
func (w *deribitWrapper) GetBalance(ctx context.Context, asset string) (float64, error) {
	return w.adapter.GetBalance(ctx)
}

// StartOrderStream 启动订单流
func (w *deribitWrapper) StartOrderStream(ctx context.Context, callback func(interface{})) error {
	return w.adapter.StartOrderStream(ctx, callback)
}

// StopOrderStream 停止订单流
func (w *deribitWrapper) StopOrderStream() error {
	return w.adapter.StopOrderStream()
}

// GetLatestPrice 获取最新价格
func (w *deribitWrapper) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	return w.adapter.GetLatestPrice(ctx)
}

// StartPriceStream 启动价格流
func (w *deribitWrapper) StartPriceStream(ctx context.Context, symbol string, callback func(price float64)) error {
	return w.adapter.StartOrderStream(ctx, func(data interface{}) {
		if ticker, ok := data.(map[string]interface{}); ok {
			if price, ok := ticker["last_price"].(float64); ok {
				callback(price)
			}
		}
	})
}

// StartKlineStream 启动 K线流
func (w *deribitWrapper) StartKlineStream(ctx context.Context, symbols []string, interval string, callback CandleUpdateCallback) error {
	return w.adapter.StartKlineStream(ctx, interval, func(candle *deribit.CandleLocal) {
		callback(&Candle{
			Symbol:    candle.Symbol,
			Open:      candle.Open,
			High:      candle.High,
			Low:       candle.Low,
			Close:     candle.Close,
			Volume:    candle.Volume,
			Timestamp: candle.Timestamp,
		})
	})
}

// StopKlineStream 停止 K线流
func (w *deribitWrapper) StopKlineStream() error {
	return w.adapter.StopKlineStream()
}

// GetHistoricalKlines 获取历史 K线
func (w *deribitWrapper) GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*Candle, error) {
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

// GetPriceDecimals 获取价格精度
func (w *deribitWrapper) GetPriceDecimals() int {
	return w.adapter.GetPriceDecimals()
}

// GetQuantityDecimals 获取数量精度
func (w *deribitWrapper) GetQuantityDecimals() int {
	return w.adapter.GetQuantityDecimals()
}

// GetBaseAsset 获取基础资产
func (w *deribitWrapper) GetBaseAsset() string {
	return w.adapter.GetBaseAsset()
}

// GetQuoteAsset 获取报价资产
func (w *deribitWrapper) GetQuoteAsset() string {
	return w.adapter.GetQuoteAsset()
}

// GetFundingRate 获取资金费率
func (w *deribitWrapper) GetFundingRate(ctx context.Context, symbol string) (float64, error) {
	return w.adapter.GetFundingRate(ctx)
}

