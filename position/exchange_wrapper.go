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
	executor ContextOrderExecutor
	observe  func(*exchange.Order) bool
}

// NewExchangeAdapterWrapper 創建包裝器
func NewExchangeAdapterWrapper(ex exchange.IExchange) *ExchangeAdapterWrapper {
	return &ExchangeAdapterWrapper{
		exchange: ex,
	}
}

func NewOwnedExchangeAdapterWrapper(ex exchange.IExchange, executor ContextOrderExecutor, observe func(*exchange.Order) bool) *ExchangeAdapterWrapper {
	return &ExchangeAdapterWrapper{exchange: ex, executor: executor, observe: observe}
}

// GetName 獲取交易所名稱
func (w *ExchangeAdapterWrapper) GetName() string {
	return w.exchange.GetName()
}

// PlaceOrder 下單
func (w *ExchangeAdapterWrapper) PlaceOrder(ctx context.Context, req *ExchangeOrderRequest) (*ExchangeOrder, error) {
	if w.executor == nil {
		return nil, fmt.Errorf("close submission requires owned executor")
	}
	leg := PositionSideLong
	if req.Side == "BUY" {
		leg = PositionSideShort
	}
	exchangeReq := &OrderRequest{
		Symbol:        req.Symbol,
		Side:          req.Side,
		Type:          req.Type,
		Quantity:      req.Quantity,
		Price:         req.Price,
		ReduceOnly:    req.ReduceOnly,
		PostOnly:      req.PostOnly,
		TimeInForce:   req.TimeInForce,
		PriceDecimals: req.PriceDecimals,
		ClientOrderID: req.ClientOrderID,
		PositionSide:  leg,
		StrategyName:  "manual_close",
		BotWideClose:  true,
		OrderSource:   "liquidation",
	}

	order, err := w.executor.PlaceOrderContext(ctx, exchangeReq)
	if order == nil {
		return nil, err
	}
	return &ExchangeOrder{OrderID: order.OrderID, ClientOrderID: order.ClientOrderID, Symbol: order.Symbol, Side: order.Side,
		Status: order.Status, Quantity: order.Quantity, ExecutedQty: order.ExecutedQty, AvgPrice: order.AvgPrice}, err
}

// GetOrder 獲取訂單
func (w *ExchangeAdapterWrapper) GetOrder(ctx context.Context, symbol string, orderID int64) (*ExchangeOrder, error) {
	order, err := w.exchange.GetOrder(ctx, symbol, orderID)
	if err != nil {
		return nil, err
	}
	if order == nil {
		return nil, fmt.Errorf("empty close order query")
	}

	return &ExchangeOrder{
		OrderID:       order.OrderID,
		Status:        string(order.Status),
		ExecutedQty:   order.ExecutedQty,
		ClientOrderID: order.ClientOrderID, Symbol: order.Symbol, Side: string(order.Side), Quantity: order.Quantity, AvgPrice: order.AvgPrice,
	}, nil
}

// GetOrderByClientOrderID exposes the optional durable order lookup capability
// through the position adapter without widening its base exchange interface.
func (w *ExchangeAdapterWrapper) GetOrderByClientOrderID(ctx context.Context, symbol, clientOrderID string) (*exchange.Order, error) {
	query, ok := w.exchange.(exchange.OrderByClientIDQuerier)
	if !ok {
		return nil, fmt.Errorf("exchange %s cannot query orders by ClientOrderID", w.exchange.GetName())
	}
	return query.GetOrderByClientOrderID(ctx, symbol, clientOrderID)
}

func (w *ExchangeAdapterWrapper) ConfirmCloseOrder(o *ExchangeOrder) error {
	if w.observe == nil || !w.observe(&exchange.Order{OrderID: o.OrderID, ClientOrderID: o.ClientOrderID,
		Symbol: o.Symbol, Side: exchange.Side(o.Side), Status: exchange.OrderStatus(o.Status), Quantity: o.Quantity, ExecutedQty: o.ExecutedQty, AvgPrice: o.AvgPrice}) {
		return fmt.Errorf("close observation not durably owned")
	}
	return nil
}

// CancelOrder 取消訂單
func (w *ExchangeAdapterWrapper) CancelOrder(ctx context.Context, symbol string, orderID int64) error {
	canceler, ok := w.executor.(ContextOrderCanceler)
	if !ok {
		return fmt.Errorf("close cancellation requires managed executor")
	}
	return canceler.CancelOrderContext(ctx, orderID)
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
