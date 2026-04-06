package exchange

import (
	"context"
	"strconv"
	"time"

	"quantmesh/exchange/bitfinex"
	"quantmesh/exchange/income"
)

// bitfinexWrapper Bitfinex 包装器
type bitfinexWrapper struct {
	adapter *bitfinex.Adapter
}

// GetName 獲取交易所名称
func (w *bitfinexWrapper) GetName() string {
	return w.adapter.GetName()
}

func (w *bitfinexWrapper) GetMarketType() string {
	return w.adapter.GetMarketType()
}

// PlaceOrder 下單
func (w *bitfinexWrapper) PlaceOrder(ctx context.Context, req *OrderRequest) (*Order, error) {
	bitfinexReq := &bitfinex.BitfinexOrderRequest{
		Symbol:        req.Symbol,
		Side:          bitfinex.Side(req.Side),
		Type:          bitfinex.OrderType(req.Type),
		TimeInForce:   bitfinex.TimeInForce(req.TimeInForce),
		Quantity:      req.Quantity,
		Price:         req.Price,
		ReduceOnly:    req.ReduceOnly,
		PostOnly:      req.PostOnly,
		ClientOrderID: req.ClientOrderID,
		Timestamp:     time.Now().UnixMilli(),
	}

	bitfinexOrder, err := w.adapter.PlaceOrder(ctx, bitfinexReq)
	if err != nil {
		return nil, err
	}

	return convertBitfinexOrderToExchangeOrder(bitfinexOrder), nil
}

// BatchPlaceOrders 批量下單
func (w *bitfinexWrapper) BatchPlaceOrders(ctx context.Context, orders []*OrderRequest) ([]*Order, bool) {
	bitfinexOrders := make([]*bitfinex.BitfinexOrderRequest, 0, len(orders))
	for _, order := range orders {
		bitfinexOrders = append(bitfinexOrders, &bitfinex.BitfinexOrderRequest{
			Symbol:        order.Symbol,
			Side:          bitfinex.Side(order.Side),
			Type:          bitfinex.OrderType(order.Type),
			TimeInForce:   bitfinex.TimeInForce(order.TimeInForce),
			Quantity:      order.Quantity,
			Price:         order.Price,
			ReduceOnly:    order.ReduceOnly,
			PostOnly:      order.PostOnly,
			ClientOrderID: order.ClientOrderID,
			Timestamp:     time.Now().UnixMilli(),
		})
	}

	bitfinexResults, allSuccess := w.adapter.BatchPlaceOrders(ctx, bitfinexOrders)

	results := make([]*Order, 0, len(bitfinexResults))
	for _, bitfinexOrder := range bitfinexResults {
		results = append(results, convertBitfinexOrderToExchangeOrder(bitfinexOrder))
	}

	return results, allSuccess
}

// CancelOrder 取消訂單
func (w *bitfinexWrapper) CancelOrder(ctx context.Context, symbol string, orderID int64) error {
	return w.adapter.CancelOrder(ctx, symbol, orderID)
}

// BatchCancelOrders 批量取消訂單
func (w *bitfinexWrapper) BatchCancelOrders(ctx context.Context, symbol string, orderIDs []int64) error {
	return w.adapter.BatchCancelOrders(ctx, symbol, orderIDs)
}

// CancelAllOrders 取消所有订單
func (w *bitfinexWrapper) CancelAllOrders(ctx context.Context, symbol string) error {
	return w.adapter.CancelAllOrders(ctx, symbol)
}

// GetOrder 查詢訂單
func (w *bitfinexWrapper) GetOrder(ctx context.Context, symbol string, orderID int64) (*Order, error) {
	bitfinexOrder, err := w.adapter.GetOrder(ctx, symbol, orderID)
	if err != nil {
		return nil, err
	}
	return convertBitfinexOrderToExchangeOrder(bitfinexOrder), nil
}

// GetOpenOrders 查詢未完成订單
func (w *bitfinexWrapper) GetOpenOrders(ctx context.Context, symbol string) ([]*Order, error) {
	bitfinexOrders, err := w.adapter.GetOpenOrders(ctx, symbol)
	if err != nil {
		return nil, err
	}

	orders := make([]*Order, 0, len(bitfinexOrders))
	for _, bitfinexOrder := range bitfinexOrders {
		orders = append(orders, convertBitfinexOrderToExchangeOrder(bitfinexOrder))
	}
	return orders, nil
}

// GetAccount 獲取帳戶信息
func (w *bitfinexWrapper) GetAccount(ctx context.Context) (*Account, error) {
	bitfinexAccount, err := w.adapter.GetAccount(ctx)
	if err != nil {
		return nil, err
	}

	return &Account{
		TotalWalletBalance: bitfinexAccount.TotalBalance,
		TotalMarginBalance: bitfinexAccount.MarginBalance,
		AvailableBalance:   bitfinexAccount.AvailableBalance,
	}, nil
}

// GetPositions 獲取持倉信息
func (w *bitfinexWrapper) GetPositions(ctx context.Context, symbol string) ([]*Position, error) {
	bitfinexPositions, err := w.adapter.GetPositions(ctx, symbol)
	if err != nil {
		return nil, err
	}

	positions := make([]*Position, 0, len(bitfinexPositions))
	for _, bitfinexPos := range bitfinexPositions {
		size := bitfinexPos.Size
		if bitfinexPos.Side == "SHORT" {
			size = -size
		}

		positions = append(positions, &Position{
			Symbol:        bitfinexPos.Symbol,
			Size:          size,
			EntryPrice:    bitfinexPos.EntryPrice,
			MarkPrice:     bitfinexPos.MarkPrice,
			UnrealizedPNL: bitfinexPos.UnrealizedPnL,
			Leverage:      int(bitfinexPos.Leverage),
		})
	}
	return positions, nil
}

// GetBalance 獲取餘額
func (w *bitfinexWrapper) GetBalance(ctx context.Context, asset string) (float64, error) {
	return w.adapter.GetBalance(ctx, asset)
}

// StartOrderStream 啟動訂單流
func (w *bitfinexWrapper) StartOrderStream(ctx context.Context, callback func(interface{})) error {
	return w.adapter.StartOrderStream(ctx, callback)
}

// StopOrderStream 停止訂單流
func (w *bitfinexWrapper) StopOrderStream() error {
	return w.adapter.StopOrderStream()
}

// GetLatestPrice 獲取最新價格
func (w *bitfinexWrapper) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	return w.adapter.GetLatestPrice(ctx, symbol)
}

// StartPriceStream 啟動價格流
func (w *bitfinexWrapper) StartPriceStream(ctx context.Context, symbol string, callback func(price float64)) error {
	return w.adapter.StartPriceStream(ctx, symbol, callback)
}

// StartKlineStream 啟動K線流
func (w *bitfinexWrapper) StartKlineStream(ctx context.Context, symbols []string, interval string, callback CandleUpdateCallback) error {
	// 類型轉换：將 exchange.CandleUpdateCallback 轉换為 bitfinex.CandleUpdateCallback
	bitfinexCallback := func(candle interface{}) {
		if bitfinexCandle, ok := candle.(*bitfinex.Candle); ok {
			exchangeCandle := &Candle{
				Symbol:    "",
				Open:      bitfinexCandle.Open,
				High:      bitfinexCandle.High,
				Low:       bitfinexCandle.Low,
				Close:     bitfinexCandle.Close,
				Volume:    bitfinexCandle.Volume,
				Timestamp: bitfinexCandle.Timestamp,
				IsClosed:  true,
			}
			callback(exchangeCandle)
		}
	}
	return w.adapter.StartKlineStream(ctx, symbols, interval, bitfinexCallback)
}

// StopKlineStream 停止K線流
func (w *bitfinexWrapper) StopKlineStream() error {
	return w.adapter.StopKlineStream()
}

// GetHistoricalKlines 獲取歷史K線數據
func (w *bitfinexWrapper) GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*Candle, error) {
	bitfinexCandles, err := w.adapter.GetHistoricalKlines(ctx, symbol, interval, limit)
	if err != nil {
		return nil, err
	}

	candles := make([]*Candle, 0, len(bitfinexCandles))
	for _, bitfinexCandle := range bitfinexCandles {
		candles = append(candles, &Candle{
			Symbol:    bitfinexCandle.Symbol,
			Open:      bitfinexCandle.Open,
			High:      bitfinexCandle.High,
			Low:       bitfinexCandle.Low,
			Close:     bitfinexCandle.Close,
			Volume:    bitfinexCandle.Volume,
			Timestamp: bitfinexCandle.OpenTime,
			IsClosed:  bitfinexCandle.IsClosed,
		})
	}
	return candles, nil
}

// GetPriceDecimals 獲取價格精度
func (w *bitfinexWrapper) GetPriceDecimals() int {
	return w.adapter.GetPriceDecimals()
}

// GetQuantityDecimals 獲取數量精度
func (w *bitfinexWrapper) GetQuantityDecimals() int {
	return w.adapter.GetQuantityDecimals()
}

// GetBaseAsset 獲取基础资產
func (w *bitfinexWrapper) GetBaseAsset() string {
	return w.adapter.GetBaseAsset()
}

// GetQuoteAsset 獲取报價资產
func (w *bitfinexWrapper) GetQuoteAsset() string {
	return w.adapter.GetQuoteAsset()
}

// GetFundingRate 獲取资金费率
func (w *bitfinexWrapper) GetFundingRate(ctx context.Context, symbol string) (float64, error) {
	return w.adapter.GetFundingRate(ctx, symbol)
}

func (w *bitfinexWrapper) GetFundingInfo(ctx context.Context, symbol string) (*FundingInfo, error) {
	return nil, ErrNotImplemented
}

func (w *bitfinexWrapper) GetIncomeHistory(ctx context.Context, symbol, incomeType string, startTime, endTime int64) ([]*income.Income, error) {
	return nil, nil
}

// GetOrderFills 查詢訂單成交記錄（暂未實現）
func (w *bitfinexWrapper) GetOrderFills(ctx context.Context, symbol string, orderID int64) ([]*OrderFill, error) {
	return nil, nil
}

// convertBitfinexOrderToExchangeOrder 將 Bitfinex 订單轉换為 Exchange 订單
func convertBitfinexOrderToExchangeOrder(bitfinexOrder *bitfinex.Order) *Order {
	return &Order{
		OrderID:       parseBitfinexOrderID(bitfinexOrder.OrderID),
		ClientOrderID: bitfinexOrder.ClientOrderID,
		Symbol:        bitfinexOrder.Symbol,
		Side:          Side(bitfinexOrder.Side),
		Type:          OrderType(bitfinexOrder.Type),
		Price:         bitfinexOrder.Price,
		Quantity:      bitfinexOrder.Quantity,
		ExecutedQty:   bitfinexOrder.ExecutedQty,
		AvgPrice:      bitfinexOrder.AvgPrice,
		Status:        OrderStatus(bitfinexOrder.Status),
		CreatedAt:     bitfinexOrder.CreatedAt,
		UpdateTime:    bitfinexOrder.UpdateTime,
	}
}

// parseBitfinexOrderID 解析订單 ID
func parseBitfinexOrderID(orderID string) int64 {
	id, _ := strconv.ParseInt(orderID, 10, 64)
	return id
}

// GetSpotPrice 獲取現貨市场價格（未實現）
func (w *bitfinexWrapper) GetSpotPrice(ctx context.Context, symbol string) (float64, error) {
	return 0, ErrNotImplemented
}

// EstimateFinalOrderAmount 預估最终下單金額（默认實現：返回原始金額）
func (w *bitfinexWrapper) EstimateFinalOrderAmount(symbol string, price, quantity float64, reduceOnly bool) float64 {
	return price * quantity
}

// GetOrderBook 獲取訂單簿深度（暂未實現）
func (w *bitfinexWrapper) GetOrderBook(ctx context.Context, symbol string, limit int) (*OrderBook, error) {
	return nil, ErrNotImplemented
}

// InternalTransfer 交易所內部轉帳
func (w *bitfinexWrapper) InternalTransfer(ctx context.Context, fromAccount, toAccount, asset string, amount float64) (string, error) {
	return w.adapter.InternalTransfer(ctx, fromAccount, toAccount, asset, amount)
}
