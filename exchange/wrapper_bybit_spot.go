package exchange

import (
	"context"
	"fmt"

	"quantmesh/exchange/bybit"
	"quantmesh/exchange/income"
	"quantmesh/logger"
)

// bybitSpotWrapper 包装 Bybit 現貨适配器以實現 IExchange 接口
type bybitSpotWrapper struct {
	adapter *bybit.BybitSpotAdapter
}

func (w *bybitSpotWrapper) GetName() string {
	return w.adapter.GetName()
}

func (w *bybitSpotWrapper) GetMarketType() string {
	return w.adapter.GetMarketType()
}

// toBybitSpotOrderRequest 與合約共用映射（方向/類型/TimeInForce 未知值報錯），現貨不支援 ReduceOnly
func toBybitSpotOrderRequest(req *OrderRequest) (*bybit.OrderRequest, error) {
	bybitReq, err := toBybitOrderRequest(req)
	if err != nil {
		return nil, err
	}
	bybitReq.ReduceOnly = false
	return bybitReq, nil
}

func (w *bybitSpotWrapper) PlaceOrder(ctx context.Context, req *OrderRequest) (*Order, error) {
	bybitReq, err := toBybitSpotOrderRequest(req)
	if err != nil {
		return nil, err
	}
	order, err := w.adapter.PlaceOrder(ctx, bybitReq)
	if err != nil {
		return nil, err
	}
	return fromBybitOrder(order)
}

func (w *bybitSpotWrapper) BatchPlaceOrders(ctx context.Context, orders []*OrderRequest) ([]*Order, bool) {
	bybitOrders := make([]*bybit.OrderRequest, 0, len(orders))
	for _, req := range orders {
		bybitReq, err := toBybitSpotOrderRequest(req)
		if err != nil {
			logger.Warn("⚠️ [Bybit Spot] 跳過無法轉換的下單請求: %v", err)
			continue
		}
		bybitOrders = append(bybitOrders, bybitReq)
	}
	placed, hasErr := w.adapter.BatchPlaceOrders(ctx, bybitOrders)
	result := make([]*Order, 0, len(placed))
	for _, ord := range placed {
		converted, err := fromBybitOrder(ord)
		if err != nil {
			logger.Error("❌ [Bybit Spot] 已下單但結果轉換失败: %v", err)
			continue
		}
		result = append(result, converted)
	}
	return result, hasErr
}

func (w *bybitSpotWrapper) CancelOrder(ctx context.Context, symbol string, orderID int64) error {
	return w.adapter.CancelOrder(ctx, symbol, orderID)
}

func (w *bybitSpotWrapper) BatchCancelOrders(ctx context.Context, symbol string, orderIDs []int64) error {
	return w.adapter.BatchCancelOrders(ctx, symbol, orderIDs)
}

func (w *bybitSpotWrapper) CancelAllOrders(ctx context.Context, symbol string) error {
	return w.adapter.CancelAllOrders(ctx, symbol)
}

func (w *bybitSpotWrapper) GetOrder(ctx context.Context, symbol string, orderID int64) (*Order, error) {
	order, err := w.adapter.GetOrder(ctx, symbol, orderID)
	if err != nil {
		return nil, err
	}
	return fromBybitOrder(order)
}

func (w *bybitSpotWrapper) GetOpenOrders(ctx context.Context, symbol string) ([]*Order, error) {
	orders, err := w.adapter.GetOpenOrders(ctx, symbol)
	if err != nil {
		return nil, err
	}
	result := make([]*Order, 0, len(orders))
	for _, ord := range orders {
		converted, err := fromBybitOrder(ord)
		if err != nil {
			return nil, err
		}
		result = append(result, converted)
	}
	return result, nil
}

func (w *bybitSpotWrapper) GetAccount(ctx context.Context) (*Account, error) {
	account, err := w.adapter.GetAccount(ctx)
	if err != nil {
		return nil, err
	}
	positions := make([]*Position, len(account.Positions))
	for i, pos := range account.Positions {
		if pos != nil {
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
	}
	return &Account{
		TotalWalletBalance: account.TotalWalletBalance,
		TotalMarginBalance: account.TotalMarginBalance,
		AvailableBalance:   account.AvailableBalance,
		Positions:          positions,
	}, nil
}

func (w *bybitSpotWrapper) GetPositions(ctx context.Context, symbol string) ([]*Position, error) {
	positions, err := w.adapter.GetPositions(ctx, symbol)
	if err != nil {
		return nil, err
	}
	result := make([]*Position, len(positions))
	for i, pos := range positions {
		if pos != nil {
			result[i] = &Position{
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
	}
	return result, nil
}

func (w *bybitSpotWrapper) GetBalance(ctx context.Context, asset string) (float64, error) {
	return w.adapter.GetBalance(ctx, asset)
}

func (w *bybitSpotWrapper) StartOrderStream(ctx context.Context, callback func(interface{})) error {
	return w.adapter.StartOrderStream(ctx, callback)
}

func (w *bybitSpotWrapper) StopOrderStream() error {
	return w.adapter.StopOrderStream()
}

func (w *bybitSpotWrapper) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	return w.adapter.GetLatestPrice(ctx, symbol)
}

func (w *bybitSpotWrapper) StartPriceStream(ctx context.Context, symbol string, callback func(price float64)) error {
	return w.adapter.StartPriceStream(ctx, symbol, callback)
}

func (w *bybitSpotWrapper) StartKlineStream(ctx context.Context, symbols []string, interval string, callback CandleUpdateCallback) error {
	bybitCallback := func(candle interface{}) {
		if c, ok := candle.(*bybit.Candle); ok {
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
	}
	return w.adapter.StartKlineStream(ctx, symbols, interval, bybitCallback)
}

func (w *bybitSpotWrapper) StopKlineStream() error {
	return w.adapter.StopKlineStream()
}

func (w *bybitSpotWrapper) GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*Candle, error) {
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

func (w *bybitSpotWrapper) GetPriceDecimals() int {
	return w.adapter.GetPriceDecimals()
}

func (w *bybitSpotWrapper) GetQuantityDecimals() int {
	return w.adapter.GetQuantityDecimals()
}

func (w *bybitSpotWrapper) GetBaseAsset() string {
	return w.adapter.GetBaseAsset()
}

func (w *bybitSpotWrapper) GetQuoteAsset() string {
	return w.adapter.GetQuoteAsset()
}

func (w *bybitSpotWrapper) GetFundingRate(ctx context.Context, symbol string) (float64, error) {
	return w.adapter.GetFundingRate(ctx, symbol)
}

func (w *bybitSpotWrapper) GetFundingInfo(ctx context.Context, symbol string) (*FundingInfo, error) {
	return nil, ErrNotImplemented
}

func (w *bybitSpotWrapper) GetIncomeHistory(ctx context.Context, symbol, incomeType string, startTime, endTime int64) ([]*income.Income, error) {
	return nil, nil
}

// GetOrderFills 查詢訂單成交記錄（現貨 category=spot）。Commission 已換算為計價幣。
func (w *bybitSpotWrapper) GetOrderFills(ctx context.Context, symbol string, orderID int64) ([]*OrderFill, error) {
	bybitFills, err := w.adapter.GetOrderFills(ctx, symbol, orderID)
	if err != nil {
		return nil, err
	}
	out := make([]*OrderFill, 0, len(bybitFills))
	for _, bf := range bybitFills {
		side, err := bybit.ToInternalSide(bybit.Side(bf.Side))
		if err != nil {
			return nil, fmt.Errorf("Bybit 現貨訂單 %d 成交 %s 轉換失败: %w", orderID, bf.TradeID, err)
		}
		out = append(out, &OrderFill{
			OrderID:         bf.OrderID,
			TradeID:         bf.TradeID,
			Symbol:          bf.Symbol,
			Side:            Side(side),
			Price:           bf.Price,
			Quantity:        bf.Quantity,
			Commission:      bf.Commission,
			CommissionAsset: bf.CommissionAsset,
			TradeTime:       bf.TradeTime,
			IsMaker:         bf.IsMaker,
			BaseFeeQty:      bf.BaseFeeQty,
		})
	}
	return out, nil
}

func (w *bybitSpotWrapper) GetSpotPrice(ctx context.Context, symbol string) (float64, error) {
	return w.adapter.GetSpotPrice(ctx, symbol)
}

func (w *bybitSpotWrapper) EstimateFinalOrderAmount(symbol string, price, quantity float64, reduceOnly bool) float64 {
	return w.adapter.EstimateFinalOrderAmount(symbol, price, quantity, reduceOnly)
}

func (w *bybitSpotWrapper) GetOrderBook(ctx context.Context, symbol string, limit int) (*OrderBook, error) {
	ob, err := w.adapter.GetOrderBook(ctx, symbol, limit)
	if err != nil {
		return nil, err
	}
	bids := make([]OrderBookLevel, len(ob.Bids))
	for i, b := range ob.Bids {
		bids[i] = OrderBookLevel{Price: b.Price, Quantity: b.Quantity}
	}
	asks := make([]OrderBookLevel, len(ob.Asks))
	for i, a := range ob.Asks {
		asks[i] = OrderBookLevel{Price: a.Price, Quantity: a.Quantity}
	}
	return &OrderBook{
		Symbol:    symbol,
		Bids:      bids,
		Asks:      asks,
		Timestamp: ob.Timestamp,
	}, nil
}

func (w *bybitSpotWrapper) InternalTransfer(ctx context.Context, fromAccount, toAccount, asset string, amount float64) (string, error) {
	return w.adapter.InternalTransfer(ctx, fromAccount, toAccount, asset, amount)
}
