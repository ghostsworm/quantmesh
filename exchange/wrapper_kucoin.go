package exchange

import (
	"context"
	"time"

	"quantmesh/exchange/income"
	"quantmesh/exchange/kucoin"
)

// kucoinWrapper KuCoin 包装器
type kucoinWrapper struct {
	adapter *kucoin.Adapter
}

// GetName 獲取交易所名称
func (w *kucoinWrapper) GetName() string {
	return w.adapter.GetName()
}

func (w *kucoinWrapper) GetMarketType() string {
	return w.adapter.GetMarketType()
}

// PlaceOrder 下單
func (w *kucoinWrapper) PlaceOrder(ctx context.Context, req *OrderRequest) (*Order, error) {
	kucoinReq := &kucoin.KuCoinOrderRequest{
		Symbol:        req.Symbol,
		Side:          kucoin.Side(req.Side),
		Type:          kucoin.OrderType(req.Type),
		TimeInForce:   kucoin.TimeInForce(req.TimeInForce),
		Quantity:      req.Quantity,
		Price:         req.Price,
		ReduceOnly:    req.ReduceOnly,
		PostOnly:      req.PostOnly,
		PriceDecimals: req.PriceDecimals,
		ClientOrderID: req.ClientOrderID,
		Timestamp:     time.Now().UnixMilli(),
	}

	kucoinOrder, err := w.adapter.PlaceOrder(ctx, kucoinReq)
	if err != nil {
		return nil, err
	}

	return convertKuCoinOrderToExchangeOrder(kucoinOrder), nil
}

// BatchPlaceOrders 批量下單
func (w *kucoinWrapper) BatchPlaceOrders(ctx context.Context, orders []*OrderRequest) ([]*Order, bool) {
	kucoinOrders := make([]*kucoin.KuCoinOrderRequest, 0, len(orders))
	for _, order := range orders {
		kucoinOrders = append(kucoinOrders, &kucoin.KuCoinOrderRequest{
			Symbol:        order.Symbol,
			Side:          kucoin.Side(order.Side),
			Type:          kucoin.OrderType(order.Type),
			TimeInForce:   kucoin.TimeInForce(order.TimeInForce),
			Quantity:      order.Quantity,
			Price:         order.Price,
			ReduceOnly:    order.ReduceOnly,
			PostOnly:      order.PostOnly,
			PriceDecimals: order.PriceDecimals,
			ClientOrderID: order.ClientOrderID,
			Timestamp:     time.Now().UnixMilli(),
		})
	}

	kucoinResults, allSuccess := w.adapter.BatchPlaceOrders(ctx, kucoinOrders)

	results := make([]*Order, 0, len(kucoinResults))
	for _, kucoinOrder := range kucoinResults {
		results = append(results, convertKuCoinOrderToExchangeOrder(kucoinOrder))
	}

	return results, allSuccess
}

// CancelOrder 取消訂單
func (w *kucoinWrapper) CancelOrder(ctx context.Context, symbol string, orderID int64) error {
	return w.adapter.CancelOrder(ctx, symbol, orderID)
}

// BatchCancelOrders 批量取消訂單
func (w *kucoinWrapper) BatchCancelOrders(ctx context.Context, symbol string, orderIDs []int64) error {
	return w.adapter.BatchCancelOrders(ctx, symbol, orderIDs)
}

// CancelAllOrders 取消所有订單
func (w *kucoinWrapper) CancelAllOrders(ctx context.Context, symbol string) error {
	return w.adapter.CancelAllOrders(ctx, symbol)
}

// GetOrder 查詢訂單
func (w *kucoinWrapper) GetOrder(ctx context.Context, symbol string, orderID int64) (*Order, error) {
	kucoinOrder, err := w.adapter.GetOrder(ctx, symbol, orderID)
	if err != nil {
		return nil, err
	}
	return convertKuCoinOrderToExchangeOrder(kucoinOrder), nil
}

// GetOpenOrders 查詢未完成订單
func (w *kucoinWrapper) GetOpenOrders(ctx context.Context, symbol string) ([]*Order, error) {
	kucoinOrders, err := w.adapter.GetOpenOrders(ctx, symbol)
	if err != nil {
		return nil, err
	}

	orders := make([]*Order, 0, len(kucoinOrders))
	for _, kucoinOrder := range kucoinOrders {
		orders = append(orders, convertKuCoinOrderToExchangeOrder(kucoinOrder))
	}
	return orders, nil
}

// GetAccount 獲取帳戶信息
func (w *kucoinWrapper) GetAccount(ctx context.Context) (*Account, error) {
	kucoinAccount, err := w.adapter.GetAccount(ctx)
	if err != nil {
		return nil, err
	}

	return &Account{
		TotalWalletBalance: kucoinAccount.TotalBalance,
		TotalMarginBalance: kucoinAccount.MarginBalance,
		AvailableBalance:   kucoinAccount.AvailableBalance,
	}, nil
}

// GetPositions 獲取持倉信息
func (w *kucoinWrapper) GetPositions(ctx context.Context, symbol string) ([]*Position, error) {
	kucoinPositions, err := w.adapter.GetPositions(ctx, symbol)
	if err != nil {
		return nil, err
	}

	positions := make([]*Position, 0, len(kucoinPositions))
	for _, kucoinPos := range kucoinPositions {
		size := kucoinPos.Size
		if kucoinPos.Side == "SHORT" {
			size = -size
		}

		positions = append(positions, &Position{
			Symbol:         kucoinPos.Symbol,
			Size:           size,
			EntryPrice:     kucoinPos.EntryPrice,
			MarkPrice:      kucoinPos.MarkPrice,
			UnrealizedPNL:  kucoinPos.UnrealizedPnL,
			Leverage:       int(kucoinPos.Leverage),
			MarginType:     kucoinPos.MarginType,
			IsolatedMargin: kucoinPos.IsolatedMargin,
		})
	}
	return positions, nil
}

// GetBalance 獲取餘額
func (w *kucoinWrapper) GetBalance(ctx context.Context, asset string) (float64, error) {
	return w.adapter.GetBalance(ctx, asset)
}

// StartOrderStream 啟動訂單流
func (w *kucoinWrapper) StartOrderStream(ctx context.Context, callback func(interface{})) error {
	return w.adapter.StartOrderStream(ctx, callback)
}

// StopOrderStream 停止訂單流
func (w *kucoinWrapper) StopOrderStream() error {
	return w.adapter.StopOrderStream()
}

// GetLatestPrice 獲取最新價格
func (w *kucoinWrapper) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	return w.adapter.GetLatestPrice(ctx, symbol)
}

// StartPriceStream 啟動價格流
func (w *kucoinWrapper) StartPriceStream(ctx context.Context, symbol string, callback func(price float64)) error {
	return w.adapter.StartPriceStream(ctx, symbol, callback)
}

// StartKlineStream 啟動K線流
func (w *kucoinWrapper) StartKlineStream(ctx context.Context, symbols []string, interval string, callback CandleUpdateCallback) error {
	// 類型轉换：將 exchange.CandleUpdateCallback 轉换為 kucoin.CandleUpdateCallback
	kucoinCallback := func(candle interface{}) {
		if kucoinCandle, ok := candle.(*kucoin.Candle); ok {
			exchangeCandle := &Candle{
				Symbol:    "",
				Open:      kucoinCandle.Open,
				High:      kucoinCandle.High,
				Low:       kucoinCandle.Low,
				Close:     kucoinCandle.Close,
				Volume:    kucoinCandle.Volume,
				Timestamp: kucoinCandle.Time,
				IsClosed:  true,
			}
			callback(exchangeCandle)
		}
	}
	return w.adapter.StartKlineStream(ctx, symbols, interval, kucoinCallback)
}

// StopKlineStream 停止K線流
func (w *kucoinWrapper) StopKlineStream() error {
	return w.adapter.StopKlineStream()
}

// GetHistoricalKlines 獲取歷史K線數據
func (w *kucoinWrapper) GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*Candle, error) {
	kucoinCandles, err := w.adapter.GetHistoricalKlines(ctx, symbol, interval, limit)
	if err != nil {
		return nil, err
	}

	candles := make([]*Candle, 0, len(kucoinCandles))
	for _, kucoinCandle := range kucoinCandles {
		candles = append(candles, &Candle{
			Symbol:    symbol,
			Open:      kucoinCandle.Open,
			High:      kucoinCandle.High,
			Low:       kucoinCandle.Low,
			Close:     kucoinCandle.Close,
			Volume:    kucoinCandle.Volume,
			Timestamp: kucoinCandle.OpenTime,
			IsClosed:  kucoinCandle.IsClosed,
		})
	}
	return candles, nil
}

// GetPriceDecimals 獲取價格精度
func (w *kucoinWrapper) GetPriceDecimals() int {
	return w.adapter.GetPriceDecimals()
}

// GetQuantityDecimals 獲取數量精度
func (w *kucoinWrapper) GetQuantityDecimals() int {
	return w.adapter.GetQuantityDecimals()
}

// GetBaseAsset 獲取基础资產
func (w *kucoinWrapper) GetBaseAsset() string {
	return w.adapter.GetBaseAsset()
}

// GetQuoteAsset 獲取报價资產
func (w *kucoinWrapper) GetQuoteAsset() string {
	return w.adapter.GetQuoteAsset()
}

// GetFundingRate 獲取资金费率
func (w *kucoinWrapper) GetFundingRate(ctx context.Context, symbol string) (float64, error) {
	return w.adapter.GetFundingRate(ctx, symbol)
}

func (w *kucoinWrapper) GetIncomeHistory(ctx context.Context, symbol, incomeType string, startTime, endTime int64) ([]*income.Income, error) {
	return nil, nil
}

// GetOrderFills 查詢訂單成交記錄（暂未實現）
func (w *kucoinWrapper) GetOrderFills(ctx context.Context, symbol string, orderID int64) ([]*OrderFill, error) {
	return nil, nil
}

// convertKuCoinOrderToExchangeOrder 將 KuCoin 订單轉换為 Exchange 订單
func convertKuCoinOrderToExchangeOrder(kucoinOrder *kucoin.Order) *Order {
	return &Order{
		OrderID:       parseOrderID(kucoinOrder.OrderID),
		ClientOrderID: kucoinOrder.ClientOrderID,
		Symbol:        kucoinOrder.Symbol,
		Side:          Side(kucoinOrder.Side),
		Type:          OrderType(kucoinOrder.Type),
		Price:         kucoinOrder.Price,
		Quantity:      kucoinOrder.Quantity,
		ExecutedQty:   kucoinOrder.ExecutedQty,
		AvgPrice:      kucoinOrder.AvgPrice,
		Status:        OrderStatus(kucoinOrder.Status),
		CreatedAt:     kucoinOrder.CreatedAt,
		UpdateTime:    kucoinOrder.UpdateTime,
	}
}

// parseOrderID 解析订單 ID（KuCoin 使用字符串 ID，需要轉换）
func parseOrderID(orderID string) int64 {
	// KuCoin 使用字符串 ID，这里简化处理，回傳 0
	// 實際使用時，可以使用 hash 或其他方式轉换
	return 0
}

// GetSpotPrice 獲取現貨市场價格（未實現）
func (w *kucoinWrapper) GetSpotPrice(ctx context.Context, symbol string) (float64, error) {
	return 0, ErrNotImplemented
}

// EstimateFinalOrderAmount 預估最终下單金額（默认實現：返回原始金額）
func (w *kucoinWrapper) EstimateFinalOrderAmount(symbol string, price, quantity float64, reduceOnly bool) float64 {
	return price * quantity
}

// GetOrderBook 獲取訂單簿深度（暂未實現）
func (w *kucoinWrapper) GetOrderBook(ctx context.Context, symbol string, limit int) (*OrderBook, error) {
	return nil, ErrNotImplemented
}

// InternalTransfer 交易所內部轉帳
func (w *kucoinWrapper) InternalTransfer(ctx context.Context, fromAccount, toAccount, asset string, amount float64) (string, error) {
	return w.adapter.InternalTransfer(ctx, fromAccount, toAccount, asset, amount)
}
