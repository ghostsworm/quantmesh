package exchange

import (
	"context"
	"fmt"

	"quantmesh/exchange/income"
	"quantmesh/exchange/okx"
	"quantmesh/logger"
)

// okxWrapper OKX 包装器
type okxWrapper struct {
	adapter *okx.OKXAdapter
}

// GetName 獲取交易所名称
func (w *okxWrapper) GetName() string {
	return w.adapter.GetName()
}

func (w *okxWrapper) GetMarketType() string {
	return w.adapter.GetMarketType()
}

// PlaceOrder 下單
func (w *okxWrapper) PlaceOrder(ctx context.Context, req *OrderRequest) (*Order, error) {
	okxReq, err := toOKXOrderRequest(req)
	if err != nil {
		return nil, err
	}

	order, err := w.adapter.PlaceOrder(ctx, okxReq)
	if err != nil {
		return nil, err
	}

	return fromOKXOrder(order)
}

// toOKXOrderRequest 內部下單請求 → OKX 原生（方向/類型走顯式映射表，未知值直接報錯）
func toOKXOrderRequest(req *OrderRequest) (*okx.OrderRequest, error) {
	side, err := okx.ToNativeSide(string(req.Side))
	if err != nil {
		return nil, fmt.Errorf("OKX 下單參數轉換失败(clientOrderId=%s): %w", req.ClientOrderID, err)
	}
	orderType, err := okx.ToNativeOrderType(string(req.Type), req.PostOnly, string(req.TimeInForce))
	if err != nil {
		return nil, fmt.Errorf("OKX 下單參數轉換失败(clientOrderId=%s): %w", req.ClientOrderID, err)
	}
	postOnly := orderType == okx.OrderTypePostOnly
	if postOnly {
		// 適配器以 limit + PostOnly 表達 post_only
		orderType = okx.OrderTypeLimit
	}
	return &okx.OrderRequest{
		Symbol:        req.Symbol,
		Side:          side,
		Type:          orderType,
		TimeInForce:   okx.TimeInForce(req.TimeInForce),
		Quantity:      req.Quantity,
		Price:         req.Price,
		ReduceOnly:    req.ReduceOnly,
		PostOnly:      postOnly,
		PriceDecimals: req.PriceDecimals,
		ClientOrderID: req.ClientOrderID,
	}, nil
}

// fromOKXOrder OKX 訂單 → 內部訂單（Side/Type/Status 映射為內部常量，未知值報錯）
func fromOKXOrder(order *okx.Order) (*Order, error) {
	side, err := okx.ToInternalSide(order.Side)
	if err != nil {
		return nil, fmt.Errorf("OKX 訂單 %d 轉換失败: %w", order.OrderID, err)
	}
	orderType, err := okx.ToInternalOrderType(order.Type)
	if err != nil {
		return nil, fmt.Errorf("OKX 訂單 %d 轉換失败: %w", order.OrderID, err)
	}
	status, err := okx.ToInternalStatus(order.Status)
	if err != nil {
		return nil, fmt.Errorf("OKX 訂單 %d 轉換失败: %w", order.OrderID, err)
	}
	return &Order{
		OrderID:       order.OrderID,
		ClientOrderID: order.ClientOrderID,
		Symbol:        order.Symbol,
		Side:          Side(side),
		Type:          OrderType(orderType),
		Price:         order.Price,
		Quantity:      order.Quantity,
		ExecutedQty:   order.ExecutedQty,
		AvgPrice:      order.AvgPrice,
		Status:        OrderStatus(status),
		CreatedAt:     order.CreatedAt,
		UpdateTime:    order.UpdateTime,
	}, nil
}

// BatchPlaceOrders 批量下單
func (w *okxWrapper) BatchPlaceOrders(ctx context.Context, orders []*OrderRequest) ([]*Order, bool) {
	okxOrders := make([]*okx.OrderRequest, 0, len(orders))
	for _, req := range orders {
		okxReq, err := toOKXOrderRequest(req)
		if err != nil {
			logger.Warn("⚠️ [OKX] 跳過無法轉換的下單請求: %v", err)
			continue
		}
		okxOrders = append(okxOrders, okxReq)
	}

	placedOrders, hasMarginError := w.adapter.BatchPlaceOrders(ctx, okxOrders)

	result := make([]*Order, 0, len(placedOrders))
	for _, order := range placedOrders {
		converted, err := fromOKXOrder(order)
		if err != nil {
			logger.Error("❌ [OKX] 已下單但結果轉換失败: %v", err)
			continue
		}
		result = append(result, converted)
	}

	return result, hasMarginError
}

// CancelOrder 取消訂單
func (w *okxWrapper) CancelOrder(ctx context.Context, symbol string, orderID int64) error {
	return w.adapter.CancelOrder(ctx, symbol, orderID)
}

// BatchCancelOrders 批量取消訂單
func (w *okxWrapper) BatchCancelOrders(ctx context.Context, symbol string, orderIDs []int64) error {
	return w.adapter.BatchCancelOrders(ctx, symbol, orderIDs)
}

// CancelAllOrders 取消所有订單
func (w *okxWrapper) CancelAllOrders(ctx context.Context, symbol string) error {
	return w.adapter.CancelAllOrders(ctx, symbol)
}

// GetOrder 查詢訂單
func (w *okxWrapper) GetOrder(ctx context.Context, symbol string, orderID int64) (*Order, error) {
	order, err := w.adapter.GetOrder(ctx, symbol, orderID)
	if err != nil {
		return nil, err
	}

	return fromOKXOrder(order)
}

func (w *okxWrapper) GetOrderByClientOrderID(ctx context.Context, symbol, clientOrderID string) (*Order, error) {
	order, err := w.adapter.GetOrderByClientOrderID(ctx, symbol, clientOrderID)
	if err != nil || order == nil {
		return nil, err
	}
	return fromOKXOrder(order)
}

// GetOpenOrders 查詢未完成订單
func (w *okxWrapper) GetOpenOrders(ctx context.Context, symbol string) ([]*Order, error) {
	orders, err := w.adapter.GetOpenOrders(ctx, symbol)
	if err != nil {
		return nil, err
	}

	result := make([]*Order, 0, len(orders))
	for _, order := range orders {
		converted, err := fromOKXOrder(order)
		if err != nil {
			return nil, err
		}
		result = append(result, converted)
	}

	return result, nil
}

var _ OrderByClientIDQuerier = (*okxWrapper)(nil)

// GetAccount 獲取帳戶信息
func (w *okxWrapper) GetAccount(ctx context.Context) (*Account, error) {
	account, err := w.adapter.GetAccount(ctx)
	if err != nil {
		return nil, err
	}

	positions := make([]*Position, len(account.Positions))
	for i, pos := range account.Positions {
		positions[i] = &Position{
			Symbol:         pos.Symbol,
			Size:           pos.Size,
			PositionSide:   pos.PositionSide,
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
		BalanceAsset:       account.BalanceAsset,
		Positions:          positions,
	}, nil
}

// GetPositions 獲取持倉信息
func (w *okxWrapper) GetPositions(ctx context.Context, symbol string) ([]*Position, error) {
	positions, err := w.adapter.GetPositions(ctx, symbol)
	if err != nil {
		return nil, err
	}

	result := make([]*Position, len(positions))
	for i, pos := range positions {
		result[i] = &Position{
			Symbol:         pos.Symbol,
			Size:           pos.Size,
			PositionSide:   pos.PositionSide,
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
func (w *okxWrapper) GetBalance(ctx context.Context, asset string) (float64, error) {
	return w.adapter.GetBalance(ctx, asset)
}

// StartOrderStream 啟動訂單流
func (w *okxWrapper) StartOrderStream(ctx context.Context, callback func(interface{})) error {
	return w.adapter.StartOrderStream(ctx, callback)
}

// StopOrderStream 停止訂單流
func (w *okxWrapper) StopOrderStream() error {
	return w.adapter.StopOrderStream()
}

// GetLatestPrice 獲取最新價格
func (w *okxWrapper) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	return w.adapter.GetLatestPrice(ctx, symbol)
}

// StartPriceStream 啟動價格流
func (w *okxWrapper) StartPriceStream(ctx context.Context, symbol string, callback func(price float64)) error {
	return w.adapter.StartPriceStream(ctx, symbol, callback)
}

// StartKlineStream 啟動K線流
func (w *okxWrapper) StartKlineStream(ctx context.Context, symbols []string, interval string, callback CandleUpdateCallback) error {
	// 轉换回呼函數
	okxCallback := func(candle interface{}) {
		// 將 OKX Candle 轉换為通用 Candle
		if okxCandle, ok := candle.(okx.Candle); ok {
			genericCandle := &Candle{
				Symbol:    okxCandle.Symbol,
				Open:      okxCandle.Open,
				High:      okxCandle.High,
				Low:       okxCandle.Low,
				Close:     okxCandle.Close,
				Volume:    okxCandle.Volume,
				Timestamp: okxCandle.Timestamp,
				IsClosed:  okxCandle.IsClosed,
			}
			callback(genericCandle)
		}
	}
	return w.adapter.StartKlineStream(ctx, symbols, interval, okxCallback)
}

// StopKlineStream 停止K線流
func (w *okxWrapper) StopKlineStream() error {
	return w.adapter.StopKlineStream()
}

// GetHistoricalKlines 獲取歷史K線數據
func (w *okxWrapper) GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*Candle, error) {
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
func (w *okxWrapper) GetPriceDecimals() int {
	return w.adapter.GetPriceDecimals()
}

// GetQuantityDecimals 獲取數量精度
func (w *okxWrapper) GetQuantityDecimals() int {
	return w.adapter.GetQuantityDecimals()
}

// GetBaseAsset 獲取基础资產
func (w *okxWrapper) GetBaseAsset() string {
	return w.adapter.GetBaseAsset()
}

// GetQuoteAsset 獲取计價资產
func (w *okxWrapper) GetQuoteAsset() string {
	return w.adapter.GetQuoteAsset()
}

// GetFundingRate 獲取资金费率
func (w *okxWrapper) GetFundingRate(ctx context.Context, symbol string) (float64, error) {
	return w.adapter.GetFundingRate(ctx, symbol)
}

func (w *okxWrapper) GetFundingInfo(ctx context.Context, symbol string) (*FundingInfo, error) {
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

func (w *okxWrapper) GetIncomeHistory(ctx context.Context, symbol, incomeType string, startTime, endTime int64) ([]*income.Income, error) {
	return nil, nil
}

// GetOrderFills 查詢訂單成交記錄（GET /api/v5/trade/fills，用於補充手續費）
func (w *okxWrapper) GetOrderFills(ctx context.Context, symbol string, orderID int64) ([]*OrderFill, error) {
	okxFills, err := w.adapter.GetOrderFills(ctx, symbol, orderID)
	if err != nil {
		return nil, err
	}

	fills := make([]*OrderFill, 0, len(okxFills))
	for _, f := range okxFills {
		side, err := okx.ToInternalSide(f.Side)
		if err != nil {
			return nil, fmt.Errorf("OKX 訂單 %d 成交 %s 轉換失败: %w", orderID, f.TradeID, err)
		}
		fills = append(fills, &OrderFill{
			OrderID:         f.OrderID,
			TradeID:         f.TradeID,
			Symbol:          f.Symbol,
			Side:            Side(side),
			Price:           f.Price,
			Quantity:        f.Quantity,
			Commission:      f.Commission,
			CommissionAsset: f.CommissionAsset,
			TradeTime:       f.TradeTime,
			IsMaker:         f.IsMaker,
		})
	}

	return fills, nil
}

// GetSpotPrice 獲取現貨市场價格
func (w *okxWrapper) GetSpotPrice(ctx context.Context, symbol string) (float64, error) {
	return w.adapter.GetSpotPrice(ctx, symbol)
}

// EstimateFinalOrderAmount 預估最终下單金額（默认實現：返回原始金額）
func (w *okxWrapper) EstimateFinalOrderAmount(symbol string, price, quantity float64, reduceOnly bool) float64 {
	return price * quantity
}

// GetOrderBook 獲取訂單簿深度
func (w *okxWrapper) GetOrderBook(ctx context.Context, symbol string, limit int) (*OrderBook, error) {
	okxOrderBook, err := w.adapter.GetOrderBook(ctx, symbol, limit)
	if err != nil {
		return nil, err
	}

	// 轉换買盘數據
	bids := make([]OrderBookLevel, len(okxOrderBook.Bids))
	for i, bid := range okxOrderBook.Bids {
		bids[i] = OrderBookLevel{
			Price:    bid.Price,
			Quantity: bid.Quantity,
		}
	}

	// 轉换賣盘數據
	asks := make([]OrderBookLevel, len(okxOrderBook.Asks))
	for i, ask := range okxOrderBook.Asks {
		asks[i] = OrderBookLevel{
			Price:    ask.Price,
			Quantity: ask.Quantity,
		}
	}

	return &OrderBook{
		Symbol:    symbol,
		Bids:      bids,
		Asks:      asks,
		Timestamp: okxOrderBook.Timestamp,
	}, nil
}

// InternalTransfer 交易所內部轉帳
func (w *okxWrapper) InternalTransfer(ctx context.Context, fromAccount, toAccount, asset string, amount float64) (string, error) {
	return w.adapter.InternalTransfer(ctx, fromAccount, toAccount, asset, amount)
}
