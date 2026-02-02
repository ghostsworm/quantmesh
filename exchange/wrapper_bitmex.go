package exchange

import (
	"context"

	"quantmesh/exchange/bitmex"
	"quantmesh/exchange/income"
)

// bitmexWrapper BitMEX 包装器
type bitmexWrapper struct {
	adapter *bitmex.Adapter
}

// GetName 獲取交易所名称
func (w *bitmexWrapper) GetName() string {
	return w.adapter.GetName()
}

func (w *bitmexWrapper) GetMarketType() string {
	return w.adapter.GetMarketType()
}

// PlaceOrder 下單
func (w *bitmexWrapper) PlaceOrder(ctx context.Context, req *OrderRequest) (*Order, error) {
	var side bitmex.OrderSide
	if req.Side == SideBuy {
		side = bitmex.SideBuy
	} else {
		side = bitmex.SideSell
	}

	order, err := w.adapter.PlaceOrder(ctx, side, req.Price, req.Quantity, req.ClientOrderID)
	if err != nil {
		return nil, err
	}

	return &Order{
		OrderID:       0, // BitMEX 使用字符串 ID
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

// BatchPlaceOrders 批量下單
func (w *bitmexWrapper) BatchPlaceOrders(ctx context.Context, orders []*OrderRequest) ([]*Order, bool) {
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

// CancelOrder 取消訂單
func (w *bitmexWrapper) CancelOrder(ctx context.Context, symbol string, orderID int64) error {
	// BitMEX 使用字符串 ID，这里需要轉换
	return w.adapter.CancelOrder(ctx, string(rune(orderID)))
}

// BatchCancelOrders 批量取消訂單
func (w *bitmexWrapper) BatchCancelOrders(ctx context.Context, symbol string, orderIDs []int64) error {
	for _, orderID := range orderIDs {
		_ = w.adapter.CancelOrder(ctx, string(rune(orderID)))
	}
	return nil
}

// CancelAllOrders 取消所有订單
func (w *bitmexWrapper) CancelAllOrders(ctx context.Context, symbol string) error {
	orders, err := w.adapter.GetOpenOrders(ctx)
	if err != nil {
		return err
	}

	for _, order := range orders {
		_ = w.adapter.CancelOrder(ctx, order.OrderID)
	}

	return nil
}

// GetOrder 查詢訂單
func (w *bitmexWrapper) GetOrder(ctx context.Context, symbol string, orderID int64) (*Order, error) {
	order, err := w.adapter.GetOrder(ctx, string(rune(orderID)))
	if err != nil {
		return nil, err
	}

	var side Side
	if order.Side == bitmex.SideBuy {
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

// GetOpenOrders 獲取活跃订單
func (w *bitmexWrapper) GetOpenOrders(ctx context.Context, symbol string) ([]*Order, error) {
	orders, err := w.adapter.GetOpenOrders(ctx)
	if err != nil {
		return nil, err
	}

	result := make([]*Order, 0, len(orders))
	for _, order := range orders {
		var side Side
		if order.Side == bitmex.SideBuy {
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

// GetAccount 獲取帳戶信息
func (w *bitmexWrapper) GetAccount(ctx context.Context) (*Account, error) {
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

// GetPositions 獲取持倉
func (w *bitmexWrapper) GetPositions(ctx context.Context, symbol string) ([]*Position, error) {
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

// GetBalance 獲取餘額
func (w *bitmexWrapper) GetBalance(ctx context.Context, asset string) (float64, error) {
	return w.adapter.GetBalance(ctx)
}

// StartOrderStream 啟動訂單流
func (w *bitmexWrapper) StartOrderStream(ctx context.Context, callback func(interface{})) error {
	return w.adapter.StartOrderStream(ctx, callback)
}

// StopOrderStream 停止訂單流
func (w *bitmexWrapper) StopOrderStream() error {
	return w.adapter.StopOrderStream()
}

// GetLatestPrice 獲取最新價格
func (w *bitmexWrapper) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	return w.adapter.GetLatestPrice(ctx, symbol)
}

// StartPriceStream 啟動價格流
func (w *bitmexWrapper) StartPriceStream(ctx context.Context, symbol string, callback func(price float64)) error {
	return w.adapter.StartOrderStream(ctx, func(data interface{}) {
		// BitMEX 價格流处理
		if trades, ok := data.([]interface{}); ok && len(trades) > 0 {
			if trade, ok := trades[0].(map[string]interface{}); ok {
				if price, ok := trade["price"].(float64); ok {
					callback(price)
				}
			}
		}
	})
}

// StartKlineStream 啟动 K線流
func (w *bitmexWrapper) StartKlineStream(ctx context.Context, symbols []string, interval string, callback CandleUpdateCallback) error {
	return w.adapter.StartKlineStream(ctx, interval, func(candle *bitmex.CandleLocal) {
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

// StopKlineStream 停止 K線流
func (w *bitmexWrapper) StopKlineStream() error {
	return w.adapter.StopKlineStream()
}

// GetHistoricalKlines 獲取歷史 K線
func (w *bitmexWrapper) GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*Candle, error) {
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

// GetPriceDecimals 獲取價格精度
func (w *bitmexWrapper) GetPriceDecimals() int {
	return w.adapter.GetPriceDecimals()
}

// GetQuantityDecimals 獲取數量精度
func (w *bitmexWrapper) GetQuantityDecimals() int {
	return w.adapter.GetQuantityDecimals()
}

// GetBaseAsset 獲取基础资產
func (w *bitmexWrapper) GetBaseAsset() string {
	return w.adapter.GetBaseAsset()
}

// GetQuoteAsset 獲取报價资產
func (w *bitmexWrapper) GetQuoteAsset() string {
	return w.adapter.GetQuoteAsset()
}

// GetFundingRate 獲取资金费率
func (w *bitmexWrapper) GetFundingRate(ctx context.Context, symbol string) (float64, error) {
	return w.adapter.GetFundingRate(ctx)
}

func (w *bitmexWrapper) GetIncomeHistory(ctx context.Context, symbol, incomeType string, startTime, endTime int64) ([]*income.Income, error) {
	return nil, nil
}

// GetOrderFills 查詢訂單成交記錄（暂未實現）
func (w *bitmexWrapper) GetOrderFills(ctx context.Context, symbol string, orderID int64) ([]*OrderFill, error) {
	return nil, nil
}

// GetSpotPrice 獲取現貨市场價格（未實現）
func (w *bitmexWrapper) GetSpotPrice(ctx context.Context, symbol string) (float64, error) {
	return 0, ErrNotImplemented
}

// EstimateFinalOrderAmount 預估最终下單金額（默认實現：返回原始金額）
func (w *bitmexWrapper) EstimateFinalOrderAmount(symbol string, price, quantity float64, reduceOnly bool) float64 {
	return price * quantity
}

// GetOrderBook 獲取訂單簿深度（暂未實現）
func (w *bitmexWrapper) GetOrderBook(ctx context.Context, symbol string, limit int) (*OrderBook, error) {
	return nil, ErrNotImplemented
}

// InternalTransfer 交易所內部轉帳
func (w *bitmexWrapper) InternalTransfer(ctx context.Context, fromAccount, toAccount, asset string, amount float64) (string, error) {
	return w.adapter.InternalTransfer(ctx, fromAccount, toAccount, asset, amount)
}
