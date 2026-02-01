package exchange

import (
	"context"
	"strconv"
	"time"

	"quantmesh/exchange/kraken"
)

// krakenWrapper Kraken 包装器
type krakenWrapper struct {
	adapter *kraken.Adapter
}

// GetName 獲取交易所名称
func (w *krakenWrapper) GetName() string {
	return w.adapter.GetName()
}

func (w *krakenWrapper) GetMarketType() string {
	return w.adapter.GetMarketType()
}

// PlaceOrder 下單
func (w *krakenWrapper) PlaceOrder(ctx context.Context, req *OrderRequest) (*Order, error) {
	krakenReq := &kraken.KrakenOrderRequest{
		Symbol:        req.Symbol,
		Side:          kraken.Side(req.Side),
		Type:          kraken.OrderType(req.Type),
		TimeInForce:   kraken.TimeInForce(req.TimeInForce),
		Quantity:      req.Quantity,
		Price:         req.Price,
		ReduceOnly:    req.ReduceOnly,
		PostOnly:      req.PostOnly,
		PriceDecimals: req.PriceDecimals,
		ClientOrderID: req.ClientOrderID,
		Timestamp:     time.Now().UnixMilli(),
	}

	krakenOrder, err := w.adapter.PlaceOrder(ctx, krakenReq)
	if err != nil {
		return nil, err
	}

	return convertKrakenOrderToExchangeOrder(krakenOrder), nil
}

// BatchPlaceOrders 批量下單
func (w *krakenWrapper) BatchPlaceOrders(ctx context.Context, orders []*OrderRequest) ([]*Order, bool) {
	krakenOrders := make([]*kraken.KrakenOrderRequest, 0, len(orders))
	for _, order := range orders {
		krakenOrders = append(krakenOrders, &kraken.KrakenOrderRequest{
			Symbol:        order.Symbol,
			Side:          kraken.Side(order.Side),
			Type:          kraken.OrderType(order.Type),
			TimeInForce:   kraken.TimeInForce(order.TimeInForce),
			Quantity:      order.Quantity,
			Price:         order.Price,
			ReduceOnly:    order.ReduceOnly,
			PostOnly:      order.PostOnly,
			PriceDecimals: order.PriceDecimals,
			ClientOrderID: order.ClientOrderID,
			Timestamp:     time.Now().UnixMilli(),
		})
	}

	krakenResults, allSuccess := w.adapter.BatchPlaceOrders(ctx, krakenOrders)

	results := make([]*Order, 0, len(krakenResults))
	for _, krakenOrder := range krakenResults {
		results = append(results, convertKrakenOrderToExchangeOrder(krakenOrder))
	}

	return results, allSuccess
}

// CancelOrder 取消訂單
func (w *krakenWrapper) CancelOrder(ctx context.Context, symbol string, orderID int64) error {
	return w.adapter.CancelOrder(ctx, symbol, orderID)
}

// BatchCancelOrders 批量取消訂單
func (w *krakenWrapper) BatchCancelOrders(ctx context.Context, symbol string, orderIDs []int64) error {
	return w.adapter.BatchCancelOrders(ctx, symbol, orderIDs)
}

// CancelAllOrders 取消所有订單
func (w *krakenWrapper) CancelAllOrders(ctx context.Context, symbol string) error {
	return w.adapter.CancelAllOrders(ctx, symbol)
}

// GetOrder 查詢訂單
func (w *krakenWrapper) GetOrder(ctx context.Context, symbol string, orderID int64) (*Order, error) {
	krakenOrder, err := w.adapter.GetOrder(ctx, symbol, orderID)
	if err != nil {
		return nil, err
	}
	return convertKrakenOrderToExchangeOrder(krakenOrder), nil
}

// GetOpenOrders 查詢未完成订單
func (w *krakenWrapper) GetOpenOrders(ctx context.Context, symbol string) ([]*Order, error) {
	krakenOrders, err := w.adapter.GetOpenOrders(ctx, symbol)
	if err != nil {
		return nil, err
	}

	orders := make([]*Order, 0, len(krakenOrders))
	for _, krakenOrder := range krakenOrders {
		orders = append(orders, convertKrakenOrderToExchangeOrder(krakenOrder))
	}
	return orders, nil
}

// GetAccount 獲取帳戶信息
func (w *krakenWrapper) GetAccount(ctx context.Context) (*Account, error) {
	krakenAccount, err := w.adapter.GetAccount(ctx)
	if err != nil {
		return nil, err
	}

	return &Account{
		TotalWalletBalance: krakenAccount.TotalBalance,
		TotalMarginBalance: krakenAccount.MarginBalance,
		AvailableBalance:   krakenAccount.AvailableBalance,
	}, nil
}

// GetPositions 獲取持倉信息
func (w *krakenWrapper) GetPositions(ctx context.Context, symbol string) ([]*Position, error) {
	krakenPositions, err := w.adapter.GetPositions(ctx, symbol)
	if err != nil {
		return nil, err
	}

	positions := make([]*Position, 0, len(krakenPositions))
	for _, krakenPos := range krakenPositions {
		size := krakenPos.Size
		if krakenPos.Side == "SHORT" {
			size = -size
		}

		positions = append(positions, &Position{
			Symbol:         krakenPos.Symbol,
			Size:           size,
			EntryPrice:     krakenPos.EntryPrice,
			MarkPrice:      krakenPos.MarkPrice,
			UnrealizedPNL:  krakenPos.UnrealizedPnL,
			Leverage:       int(krakenPos.Leverage),
			MarginType:     krakenPos.MarginType,
			IsolatedMargin: krakenPos.IsolatedMargin,
		})
	}
	return positions, nil
}

// GetBalance 獲取餘額
func (w *krakenWrapper) GetBalance(ctx context.Context, asset string) (float64, error) {
	return w.adapter.GetBalance(ctx, asset)
}

// StartOrderStream 啟動訂單流
func (w *krakenWrapper) StartOrderStream(ctx context.Context, callback func(interface{})) error {
	return w.adapter.StartOrderStream(ctx, callback)
}

// StopOrderStream 停止訂單流
func (w *krakenWrapper) StopOrderStream() error {
	return w.adapter.StopOrderStream()
}

// GetLatestPrice 獲取最新價格
func (w *krakenWrapper) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	return w.adapter.GetLatestPrice(ctx, symbol)
}

// StartPriceStream 啟動價格流
func (w *krakenWrapper) StartPriceStream(ctx context.Context, symbol string, callback func(price float64)) error {
	return w.adapter.StartPriceStream(ctx, symbol, callback)
}

// StartKlineStream 啟動K線流
func (w *krakenWrapper) StartKlineStream(ctx context.Context, symbols []string, interval string, callback CandleUpdateCallback) error {
	// 類型轉换：將 exchange.CandleUpdateCallback 轉换為 kraken.CandleUpdateCallback
	krakenCallback := func(candle interface{}) {
		if krakenCandle, ok := candle.(*kraken.Candle); ok {
			open, _ := strconv.ParseFloat(krakenCandle.Open, 64)
			high, _ := strconv.ParseFloat(krakenCandle.High, 64)
			low, _ := strconv.ParseFloat(krakenCandle.Low, 64)
			close, _ := strconv.ParseFloat(krakenCandle.Close, 64)
			volume, _ := strconv.ParseFloat(krakenCandle.Volume, 64)

			exchangeCandle := &Candle{
				Symbol:    "",
				Open:      open,
				High:      high,
				Low:       low,
				Close:     close,
				Volume:    volume,
				Timestamp: krakenCandle.Time,
				IsClosed:  true,
			}
			callback(exchangeCandle)
		}
	}
	return w.adapter.StartKlineStream(ctx, symbols, interval, krakenCallback)
}

// StopKlineStream 停止K線流
func (w *krakenWrapper) StopKlineStream() error {
	return w.adapter.StopKlineStream()
}

// GetHistoricalKlines 獲取歷史K線數據
func (w *krakenWrapper) GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*Candle, error) {
	krakenCandles, err := w.adapter.GetHistoricalKlines(ctx, symbol, interval, limit)
	if err != nil {
		return nil, err
	}

	candles := make([]*Candle, 0, len(krakenCandles))
	for _, krakenCandle := range krakenCandles {
		candles = append(candles, &Candle{
			Symbol:    symbol,
			Open:      krakenCandle.Open,
			High:      krakenCandle.High,
			Low:       krakenCandle.Low,
			Close:     krakenCandle.Close,
			Volume:    krakenCandle.Volume,
			Timestamp: krakenCandle.OpenTime,
			IsClosed:  krakenCandle.IsClosed,
		})
	}
	return candles, nil
}

// GetPriceDecimals 獲取價格精度
func (w *krakenWrapper) GetPriceDecimals() int {
	return w.adapter.GetPriceDecimals()
}

// GetQuantityDecimals 獲取數量精度
func (w *krakenWrapper) GetQuantityDecimals() int {
	return w.adapter.GetQuantityDecimals()
}

// GetBaseAsset 獲取基础资產
func (w *krakenWrapper) GetBaseAsset() string {
	return w.adapter.GetBaseAsset()
}

// GetQuoteAsset 獲取报價资產
func (w *krakenWrapper) GetQuoteAsset() string {
	return w.adapter.GetQuoteAsset()
}

// GetFundingRate 獲取资金费率
func (w *krakenWrapper) GetFundingRate(ctx context.Context, symbol string) (float64, error) {
	return w.adapter.GetFundingRate(ctx, symbol)
}

// convertKrakenOrderToExchangeOrder 將 Kraken 订單轉换為 Exchange 订單
func convertKrakenOrderToExchangeOrder(krakenOrder *kraken.Order) *Order {
	return &Order{
		OrderID:       parseKrakenOrderID(krakenOrder.OrderID),
		ClientOrderID: krakenOrder.ClientOrderID,
		Symbol:        krakenOrder.Symbol,
		Side:          Side(krakenOrder.Side),
		Type:          OrderType(krakenOrder.Type),
		Price:         krakenOrder.Price,
		Quantity:      krakenOrder.Quantity,
		ExecutedQty:   krakenOrder.ExecutedQty,
		AvgPrice:      krakenOrder.AvgPrice,
		Status:        OrderStatus(krakenOrder.Status),
		CreatedAt:     krakenOrder.CreatedAt,
		UpdateTime:    krakenOrder.UpdateTime,
	}
}

// parseKrakenOrderID 解析订單 ID（Kraken 使用字符串 ID，需要轉换）
func parseKrakenOrderID(orderID string) int64 {
	// Kraken 使用字符串 ID，这里简化处理，回傳 0
	// 實際使用時，可以使用 hash 或其他方式轉换
	id, _ := strconv.ParseInt(orderID, 10, 64)
	return id
}

// GetSpotPrice 獲取現貨市场價格（未實現）
func (w *krakenWrapper) GetSpotPrice(ctx context.Context, symbol string) (float64, error) {
	return 0, ErrNotImplemented
}

// EstimateFinalOrderAmount 預估最终下單金額（默认實現：返回原始金額）
func (w *krakenWrapper) EstimateFinalOrderAmount(symbol string, price, quantity float64, reduceOnly bool) float64 {
	return price * quantity
}

// GetOrderBook 獲取訂單簿深度（暂未實現）
func (w *krakenWrapper) GetOrderBook(ctx context.Context, symbol string, limit int) (*OrderBook, error) {
	return nil, ErrNotImplemented
}

// InternalTransfer 交易所內部轉帳
func (w *krakenWrapper) InternalTransfer(ctx context.Context, fromAccount, toAccount, asset string, amount float64) (string, error) {
	return w.adapter.InternalTransfer(ctx, fromAccount, toAccount, asset, amount)
}
