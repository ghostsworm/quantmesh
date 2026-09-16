package position

import (
	"context"
	"fmt"

	"quantmesh/exchange"
)

// NewExchangeAdapterWrapper 創建交易所適配器包裝器
// 這將 exchange.IExchange 包裝成 ClosePositionManager 需要的接口
type ExchangeAdapterWrapper struct {
	exchange exchange.IExchange
	symbol   string
}

// NewExchangeAdapterWrapper 創建包裝器
func NewExchangeAdapterWrapper(ex exchange.IExchange) *ExchangeAdapterWrapper {
	return &ExchangeAdapterWrapper{
		exchange: ex,
		symbol:   "", // 將在調用時設置
	}
}

// GetName 獲取交易所名稱
func (w *ExchangeAdapterWrapper) GetName() string {
	return w.exchange.GetName()
}

// PlaceOrder 下單
func (w *ExchangeAdapterWrapper) PlaceOrder(ctx context.Context, req *ExchangeOrderRequest) (*ExchangeOrder, error) {
	side := exchange.Side(req.Side)
	orderType := exchange.OrderType(req.Type)
	tif := exchange.TimeInForce(req.TimeInForce)

	exchangeReq := &exchange.OrderRequest{
		Symbol:        req.Symbol,
		Side:          side,
		Type:          orderType,
		Quantity:      req.Quantity,
		Price:         req.Price,
		ReduceOnly:    req.ReduceOnly,
		PostOnly:      req.PostOnly,
		TimeInForce:   tif,
		PriceDecimals: req.PriceDecimals,
	}

	order, err := w.exchange.PlaceOrder(ctx, exchangeReq)
	if err != nil {
		return nil, err
	}

	return &ExchangeOrder{
		OrderID:     order.OrderID,
		Status:      string(order.Status),
		ExecutedQty: order.Quantity,
	}, nil
}

// GetOrder 獲取訂單
func (w *ExchangeAdapterWrapper) GetOrder(ctx context.Context, symbol string, orderID int64) (*ExchangeOrder, error) {
	order, err := w.exchange.GetOrder(ctx, symbol, orderID)
	if err != nil {
		return nil, err
	}

	return &ExchangeOrder{
		OrderID:     order.OrderID,
		Status:      string(order.Status),
		ExecutedQty: order.ExecutedQty,
	}, nil
}

// CancelOrder 取消訂單
func (w *ExchangeAdapterWrapper) CancelOrder(ctx context.Context, symbol string, orderID int64) error {
	return w.exchange.CancelOrder(ctx, symbol, orderID)
}

// GetLatestPrice 獲取最新價格（委託底層交易所實現）
func (w *ExchangeAdapterWrapper) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	if w.exchange == nil {
		return 0, fmt.Errorf("獲取最新價格失敗: %s 交易所未初始化", symbol)
	}
	price, err := w.exchange.GetLatestPrice(ctx, symbol)
	if err != nil {
		return 0, fmt.Errorf("獲取最新價格失敗: %s: %w", symbol, err)
	}
	return price, nil
}

// GetPriceDecimals 獲取價格精度（供平倉管理器限價單使用）
func (w *ExchangeAdapterWrapper) GetPriceDecimals() int {
	if w.exchange == nil {
		return -1
	}
	return w.exchange.GetPriceDecimals()
}
