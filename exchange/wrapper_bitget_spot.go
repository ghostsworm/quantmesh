package exchange

import (
	"context"
	"strconv"
	"strings"

	"quantmesh/exchange/bitget"
	"quantmesh/exchange/income"
)

// bitgetSpotWrapper 包装 Bitget 現貨适配器以實現 IExchange 接口
type bitgetSpotWrapper struct {
	adapter *bitget.BitgetSpotAdapter
}

func (w *bitgetSpotWrapper) GetName() string {
	return w.adapter.GetName()
}

func (w *bitgetSpotWrapper) GetMarketType() string {
	return w.adapter.GetMarketType()
}

func (w *bitgetSpotWrapper) PlaceOrder(ctx context.Context, req *OrderRequest) (*Order, error) {
	bitgetReq := &bitget.OrderRequest{
		Symbol:        req.Symbol,
		Side:          bitget.Side(req.Side),
		Type:          bitget.OrderType(req.Type),
		TimeInForce:   bitget.TimeInForce(req.TimeInForce),
		Quantity:      req.Quantity,
		Price:         req.Price,
		ReduceOnly:    false,
		PostOnly:      req.PostOnly,
		PriceDecimals: req.PriceDecimals,
		ClientOrderID: req.ClientOrderID,
	}
	order, err := w.adapter.PlaceOrder(ctx, bitgetReq)
	if err != nil {
		return nil, err
	}
	return &Order{
		OrderID:       order.OrderID,
		ClientOrderID: order.ClientOrderID,
		Symbol:        order.Symbol,
		Side:          Side(order.Side),
		Type:          OrderType(order.Type),
		Price:         order.Price,
		Quantity:      order.Quantity,
		ExecutedQty:   order.ExecutedQty,
		AvgPrice:      order.AvgPrice,
		Status:        OrderStatus(order.Status),
		CreatedAt:     order.CreatedAt,
		UpdateTime:    order.UpdateTime,
	}, nil
}

func (w *bitgetSpotWrapper) BatchPlaceOrders(ctx context.Context, orders []*OrderRequest) ([]*Order, bool) {
	bitgetOrders := make([]*bitget.OrderRequest, len(orders))
	for i, req := range orders {
		bitgetOrders[i] = &bitget.OrderRequest{
			Symbol:        req.Symbol,
			Side:          bitget.Side(req.Side),
			Type:          bitget.OrderType(req.Type),
			TimeInForce:   bitget.TimeInForce(req.TimeInForce),
			Quantity:      req.Quantity,
			Price:         req.Price,
			ReduceOnly:    false,
			PostOnly:      req.PostOnly,
			PriceDecimals: req.PriceDecimals,
			ClientOrderID: req.ClientOrderID,
		}
	}
	placed, hasErr := w.adapter.BatchPlaceOrders(ctx, bitgetOrders)
	result := make([]*Order, len(placed))
	for i, ord := range placed {
		result[i] = &Order{
			OrderID:       ord.OrderID,
			ClientOrderID: ord.ClientOrderID,
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
	return result, hasErr
}

func (w *bitgetSpotWrapper) CancelOrder(ctx context.Context, symbol string, orderID int64) error {
	return w.adapter.CancelOrder(ctx, symbol, orderID)
}

func (w *bitgetSpotWrapper) BatchCancelOrders(ctx context.Context, symbol string, orderIDs []int64) error {
	return w.adapter.BatchCancelOrders(ctx, symbol, orderIDs)
}

func (w *bitgetSpotWrapper) CancelAllOrders(ctx context.Context, symbol string) error {
	return w.adapter.CancelAllOrders(ctx, symbol)
}

func (w *bitgetSpotWrapper) GetOrder(ctx context.Context, symbol string, orderID int64) (*Order, error) {
	order, err := w.adapter.GetOrder(ctx, symbol, orderID)
	if err != nil {
		return nil, err
	}
	return &Order{
		OrderID:       order.OrderID,
		ClientOrderID: order.ClientOrderID,
		Symbol:        order.Symbol,
		Side:          Side(order.Side),
		Type:          OrderType(order.Type),
		Price:         order.Price,
		Quantity:      order.Quantity,
		ExecutedQty:   order.ExecutedQty,
		AvgPrice:      order.AvgPrice,
		Status:        OrderStatus(order.Status),
		UpdateTime:    order.UpdateTime,
	}, nil
}

func (w *bitgetSpotWrapper) GetOpenOrders(ctx context.Context, symbol string) ([]*Order, error) {
	orders, err := w.adapter.GetOpenOrders(ctx, symbol)
	if err != nil {
		return nil, err
	}
	result := make([]*Order, len(orders))
	for i, ord := range orders {
		result[i] = &Order{
			OrderID:       ord.OrderID,
			ClientOrderID: ord.ClientOrderID,
			Symbol:        ord.Symbol,
			Side:          Side(ord.Side),
			Type:          OrderType(ord.Type),
			Price:         ord.Price,
			Quantity:      ord.Quantity,
			ExecutedQty:   ord.ExecutedQty,
			AvgPrice:      ord.AvgPrice,
			Status:        OrderStatus(ord.Status),
			UpdateTime:    ord.UpdateTime,
		}
	}
	return result, nil
}

func (w *bitgetSpotWrapper) GetAccount(ctx context.Context) (*Account, error) {
	account, err := w.adapter.GetAccount(ctx)
	if err != nil {
		return nil, err
	}
	return &Account{
		TotalWalletBalance: account.TotalWalletBalance,
		TotalMarginBalance: account.TotalMarginBalance,
		AvailableBalance:   account.AvailableBalance,
		Positions:          nil,
	}, nil
}

func (w *bitgetSpotWrapper) GetPositions(ctx context.Context, symbol string) ([]*Position, error) {
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

func (w *bitgetSpotWrapper) GetBalance(ctx context.Context, asset string) (float64, error) {
	return w.adapter.GetBalance(ctx, asset)
}

func (w *bitgetSpotWrapper) StartOrderStream(ctx context.Context, callback func(interface{})) error {
	return w.adapter.StartOrderStream(ctx, callback)
}

func (w *bitgetSpotWrapper) StopOrderStream() error {
	return w.adapter.StopOrderStream()
}

func (w *bitgetSpotWrapper) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	return w.adapter.GetLatestPrice(ctx, symbol)
}

func (w *bitgetSpotWrapper) StartPriceStream(ctx context.Context, symbol string, callback func(price float64)) error {
	return w.adapter.StartPriceStream(ctx, symbol, callback)
}

func (w *bitgetSpotWrapper) StartKlineStream(ctx context.Context, symbols []string, interval string, callback CandleUpdateCallback) error {
	return w.adapter.StartKlineStream(ctx, symbols, interval, func(candle interface{}) {
		if c, ok := candle.(*bitget.Candle); ok {
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

func (w *bitgetSpotWrapper) StopKlineStream() error {
	return w.adapter.StopKlineStream()
}

func (w *bitgetSpotWrapper) GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*Candle, error) {
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

func (w *bitgetSpotWrapper) GetPriceDecimals() int {
	return w.adapter.GetPriceDecimals()
}

func (w *bitgetSpotWrapper) GetQuantityDecimals() int {
	return w.adapter.GetQuantityDecimals()
}

func (w *bitgetSpotWrapper) GetBaseAsset() string {
	return w.adapter.GetBaseAsset()
}

func (w *bitgetSpotWrapper) GetQuoteAsset() string {
	return w.adapter.GetQuoteAsset()
}

func (w *bitgetSpotWrapper) GetFundingRate(ctx context.Context, symbol string) (float64, error) {
	return w.adapter.GetFundingRate(ctx, symbol)
}

func (w *bitgetSpotWrapper) GetFundingInfo(ctx context.Context, symbol string) (*FundingInfo, error) {
	return nil, ErrNotImplemented
}

func (w *bitgetSpotWrapper) GetIncomeHistory(ctx context.Context, symbol, incomeType string, startTime, endTime int64) ([]*income.Income, error) {
	return nil, nil
}

// GetOrderFills 查詢訂單成交記錄（現貨 fills）
func (w *bitgetSpotWrapper) GetOrderFills(ctx context.Context, symbol string, orderID int64) ([]*OrderFill, error) {
	rows, err := w.adapter.GetOrderFills(ctx, symbol, orderID)
	if err != nil {
		return nil, err
	}
	out := make([]*OrderFill, 0, len(rows))
	for _, r := range rows {
		side := SideBuy
		if strings.EqualFold(r.Side, "sell") {
			side = SideSell
		}
		price, _ := strconv.ParseFloat(r.PriceAvg, 64)
		qty, _ := strconv.ParseFloat(r.Size, 64)
		ts, _ := strconv.ParseInt(r.CTime, 10, 64)
		oid, _ := strconv.ParseInt(r.OrderId, 10, 64)
		comm := 0.0
		commAsset := "USDT"
		if len(r.FeeDetail) > 0 {
			comm, _ = strconv.ParseFloat(r.FeeDetail[0].Fee, 64)
			if r.FeeDetail[0].FeeCoin != "" {
				commAsset = r.FeeDetail[0].FeeCoin
			}
		}
		out = append(out, &OrderFill{
			OrderID:         oid,
			TradeID:         r.TradeId,
			Symbol:          r.Symbol,
			Side:            side,
			Price:           price,
			Quantity:        qty,
			Commission:      comm,
			CommissionAsset: commAsset,
			TradeTime:       ts,
			IsMaker:         false,
		})
	}
	return out, nil
}

func (w *bitgetSpotWrapper) GetSpotPrice(ctx context.Context, symbol string) (float64, error) {
	return w.adapter.GetSpotPrice(ctx, symbol)
}

func (w *bitgetSpotWrapper) EstimateFinalOrderAmount(symbol string, price, quantity float64, reduceOnly bool) float64 {
	return w.adapter.EstimateFinalOrderAmount(symbol, price, quantity, reduceOnly)
}

func (w *bitgetSpotWrapper) GetOrderBook(ctx context.Context, symbol string, limit int) (*OrderBook, error) {
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

func (w *bitgetSpotWrapper) InternalTransfer(ctx context.Context, fromAccount, toAccount, asset string, amount float64) (string, error) {
	return w.adapter.InternalTransfer(ctx, fromAccount, toAccount, asset, amount)
}
