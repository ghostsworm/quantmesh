package main

// 本文件抽自 main.go，集中存放 position / exchange 相關的適配器類型。
// 拆分目的：避免單文件超過 3000 行硬上限。
// 行為與類型語意保持不變，僅做位置遷移。

import (
	"context"
	"fmt"
	"strings"

	"quantmesh/event"
	"quantmesh/exchange"
	"quantmesh/logger"
	"quantmesh/order"
	"quantmesh/position"
	"quantmesh/utils"
)

// loggerAdapter 适配 logger 到 WebAuthnLogger 接口
type loggerAdapter struct{}

func (l *loggerAdapter) Infof(format string, args ...interface{}) {
	logger.Info(format, args...)
}

func (l *loggerAdapter) Warnf(format string, args ...interface{}) {
	logger.Warn(format, args...)
}

func (l *loggerAdapter) Errorf(format string, args ...interface{}) {
	logger.Error(format, args...)
}

func (l *loggerAdapter) Debugf(format string, args ...interface{}) {
	logger.Debug(format, args...)
}

// positionExchangeAdapter 适配器，將 exchange.IExchange 轉换為 position.IExchange
type positionExchangeAdapter struct {
	exchange exchange.IExchange
}

func (a *positionExchangeAdapter) GetPositions(ctx context.Context, symbol string) (interface{}, error) {
	positions, err := a.exchange.GetPositions(ctx, symbol)
	if err != nil {
		return nil, err
	}

	result := make([]*position.PositionInfo, len(positions))
	for i, pos := range positions {
		result[i] = &position.PositionInfo{
			Symbol: pos.Symbol,
			Size:   pos.Size,
		}
	}

	return result, nil
}

func (a *positionExchangeAdapter) GetOpenOrders(ctx context.Context, symbol string) (interface{}, error) {
	return a.exchange.GetOpenOrders(ctx, symbol)
}

func (a *positionExchangeAdapter) GetOrder(ctx context.Context, symbol string, orderID int64) (interface{}, error) {
	return a.exchange.GetOrder(ctx, symbol, orderID)
}

// GetOrderForReconciler 為對賬服務提供 GetOrder 方法（返回 *exchange.Order）
func (a *positionExchangeAdapter) GetOrderForReconciler(ctx context.Context, symbol string, orderID int64) (*exchange.Order, error) {
	return a.exchange.GetOrder(ctx, symbol, orderID)
}

func (a *positionExchangeAdapter) GetBaseAsset() string {
	return a.exchange.GetBaseAsset()
}

func (a *positionExchangeAdapter) GetName() string {
	return a.exchange.GetName()
}

func (a *positionExchangeAdapter) CancelAllOrders(ctx context.Context, symbol string) error {
	return a.exchange.CancelAllOrders(ctx, symbol)
}

func (a *positionExchangeAdapter) GetAccount(ctx context.Context) (interface{}, error) {
	return a.exchange.GetAccount(ctx)
}

func (a *positionExchangeAdapter) GetPriceDecimals() int {
	return a.exchange.GetPriceDecimals()
}

func (a *positionExchangeAdapter) GetQuantityDecimals() int {
	return a.exchange.GetQuantityDecimals()
}

// GetOrderFills 查詢訂單成交記錄（透傳至 exchange）
func (a *positionExchangeAdapter) GetOrderFills(ctx context.Context, symbol string, orderID int64) (interface{}, error) {
	return a.exchange.GetOrderFills(ctx, symbol, orderID)
}

// GetLatestPrice 獲取最新價格
func (a *positionExchangeAdapter) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	return a.exchange.GetLatestPrice(ctx, symbol)
}

// GetOrderBook 獲取訂單簿深度，轉換為 position.OrderBook
func (a *positionExchangeAdapter) GetOrderBook(ctx context.Context, symbol string, limit int) (*position.OrderBook, error) {
	ob, err := a.exchange.GetOrderBook(ctx, symbol, limit)
	if err != nil {
		return nil, err
	}
	if ob == nil {
		return nil, nil
	}
	bids := make([]position.OrderBookLevel, len(ob.Bids))
	for i, b := range ob.Bids {
		bids[i] = position.OrderBookLevel{Price: b.Price, Quantity: b.Quantity}
	}
	asks := make([]position.OrderBookLevel, len(ob.Asks))
	for i, ask := range ob.Asks {
		asks[i] = position.OrderBookLevel{Price: ask.Price, Quantity: ask.Quantity}
	}
	return &position.OrderBook{
		Symbol:    ob.Symbol,
		Bids:      bids,
		Asks:      asks,
		Timestamp: ob.Timestamp,
	}, nil
}

func (a *positionExchangeAdapter) GetQuoteAsset() string {
	return a.exchange.GetQuoteAsset()
}

func (a *positionExchangeAdapter) GetBalance(ctx context.Context, asset string) (float64, error) {
	return a.exchange.GetBalance(ctx, asset)
}

// exchangeProviderAdapter 适配器，將 exchange.IExchange 轉换為 web.ExchangeProvider
type exchangeProviderAdapter struct {
	exchange exchange.IExchange
}

func (a *exchangeProviderAdapter) GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*exchange.Candle, error) {
	return a.exchange.GetHistoricalKlines(ctx, symbol, interval, limit)
}

func (a *exchangeProviderAdapter) GetFundingRate(ctx context.Context, symbol string) (float64, error) {
	return a.exchange.GetFundingRate(ctx, symbol)
}

func (a *exchangeProviderAdapter) GetPositions(ctx context.Context, symbol string) ([]*exchange.Position, error) {
	return a.exchange.GetPositions(ctx, symbol)
}

// exchangeExecutorAdapter 适配器，將 order.ExchangeOrderExecutor 轉换為 position.OrderExecutorInterface
type exchangeExecutorAdapter struct {
	executor  *order.ExchangeOrderExecutor
	eventBus  *event.EventBus
	symbol    string
	exchange  string // 交易所名稱，用於 order_placed 事件入庫時正確寫入 exchange 字段
	accountID string
}

func (a *exchangeExecutorAdapter) MarkOrderReconciliationRequired(orderID int64, clientOrderID, reason string) error {
	return a.executor.MarkOrderReconciliationRequired(orderID, clientOrderID, reason)
}

func (a *exchangeExecutorAdapter) CancelOwnedOpeningOrders(ctx context.Context) error {
	return a.executor.CancelOwnedOpeningOrders(ctx)
}

func (a *exchangeExecutorAdapter) BeginPositionReconciliation(ctx context.Context) (func(), error) {
	return a.executor.BeginPositionReconciliation(ctx)
}

func (a *exchangeExecutorAdapter) PlaceOrder(req *position.OrderRequest) (*position.Order, error) {
	return a.PlaceOrderContext(context.Background(), req)
}

func (a *exchangeExecutorAdapter) PlaceOrderContext(ctx context.Context, req *position.OrderRequest) (*position.Order, error) {
	if req == nil {
		return nil, fmt.Errorf("order request is nil")
	}
	orderReq := &order.OrderRequest{
		Symbol:        req.Symbol,
		Side:          req.Side,
		Type:          req.Type,
		TimeInForce:   req.TimeInForce,
		Price:         req.Price,
		Quantity:      req.Quantity,
		PriceDecimals: req.PriceDecimals,
		ReduceOnly:    req.ReduceOnly,
		PositionSide:  req.PositionSide,
		ExposureKey:   req.ExposureKey,
		BotWideClose:  req.BotWideClose,
		PostOnly:      req.PostOnly,
		ClientOrderID: req.ClientOrderID,
		StrategyName:  req.StrategyName,
		StrategyType:  req.StrategyType,
		OrderSource:   req.OrderSource,
	}
	ord, err := a.executor.PlaceOrderContext(ctx, orderReq)
	req.ClientOrderID = orderReq.ClientOrderID
	if err != nil {
		return nil, err
	}
	if ord == nil {
		// 防禦：執行器不應返回 (nil, nil)，按跳過處理，避免上層解引用 panic
		return nil, fmt.Errorf("下單未返回订單 symbol=%s price=%.8f: %w", req.Symbol, req.Price, order.ErrLockNotAcquired)
	}

	if a.eventBus != nil {
		a.eventBus.Publish(&event.Event{
			Type: event.EventTypeOrderPlaced,
			Data: map[string]interface{}{
				"order_id":        ord.OrderID,
				"client_order_id": ord.ClientOrderID,
				"symbol":          ord.Symbol,
				"side":            ord.Side,
				"price":           ord.Price,
				"quantity":        ord.Quantity,
				"status":          ord.Status,
				"exchange":        a.exchange,
				"account":         a.accountID,
				"strategy_name":   req.StrategyName,
				"strategy_type":   req.StrategyType,
				"order_source":    req.OrderSource,
				"created_at":      ord.CreatedAt,
			},
		})
	}

	return &position.Order{
		OrderID:       ord.OrderID,
		ClientOrderID: ord.ClientOrderID,
		Symbol:        ord.Symbol,
		Side:          ord.Side,
		Price:         ord.Price,
		Quantity:      ord.Quantity,
		Status:        ord.Status,
		CreatedAt:     ord.CreatedAt,
		ExecutedQty:   ord.ExecutedQty,
		AvgPrice:      ord.AvgPrice,
	}, nil
}

func (a *exchangeExecutorAdapter) BatchPlaceOrders(orders []*position.OrderRequest) ([]*position.Order, bool) {
	result := a.BatchPlaceOrdersWithDetails(orders)
	return result.PlacedOrders, result.HasMarginError
}

func (a *exchangeExecutorAdapter) BatchPlaceOrdersWithDetails(orders []*position.OrderRequest) *position.BatchPlaceOrdersResult {
	return a.BatchPlaceOrdersWithDetailsContext(context.Background(), orders)
}

func (a *exchangeExecutorAdapter) BatchPlaceOrdersWithDetailsContext(ctx context.Context, orders []*position.OrderRequest) *position.BatchPlaceOrdersResult {
	// 建立 ClientOrderID -> 策略信息 的映射，用於事件发布時回填
	strategyMap := make(map[string][3]string) // ClientOrderID -> [StrategyName, StrategyType, OrderSource]
	orderReqs := make([]*order.OrderRequest, len(orders))
	for i, req := range orders {
		if req == nil {
			continue
		}
		orderReqs[i] = &order.OrderRequest{
			Symbol:        req.Symbol,
			Side:          req.Side,
			Type:          req.Type,
			TimeInForce:   req.TimeInForce,
			Price:         req.Price,
			Quantity:      req.Quantity,
			PriceDecimals: req.PriceDecimals,
			ReduceOnly:    req.ReduceOnly,
			PositionSide:  req.PositionSide,
			ExposureKey:   req.ExposureKey,
			BotWideClose:  req.BotWideClose,
			PostOnly:      req.PostOnly,
			ClientOrderID: req.ClientOrderID,
			StrategyName:  req.StrategyName,
			StrategyType:  req.StrategyType,
			OrderSource:   req.OrderSource,
		}
		if req.ClientOrderID != "" {
			info := [3]string{req.StrategyName, req.StrategyType, req.OrderSource}
			strategyMap[req.ClientOrderID] = info
			// 部分交易所（如幣安）返回的 ClientOrderID 帶前綴，預先加入以便 lookup
			prefixed := utils.AddBrokerPrefix(strings.ToLower(a.exchange), req.ClientOrderID)
			if prefixed != req.ClientOrderID {
				strategyMap[prefixed] = info
			}
		}
	}
	batchResult := a.executor.BatchPlaceOrdersWithDetailsContext(ctx, orderReqs)
	for i, req := range orders {
		if req != nil {
			req.ClientOrderID = orderReqs[i].ClientOrderID
		}
	}

	result := &position.BatchPlaceOrdersResult{
		PlacedOrders:     make([]*position.Order, 0, len(batchResult.PlacedOrders)),
		HasMarginError:   batchResult.HasMarginError,
		ReduceOnlyErrors: batchResult.ReduceOnlyErrors,
		UnknownOrders:    batchResult.UnknownOrders,
	}

	for _, ord := range batchResult.PlacedOrders {
		if ord == nil {
			continue
		}
		result.PlacedOrders = append(result.PlacedOrders, &position.Order{
			OrderID:       ord.OrderID,
			ClientOrderID: ord.ClientOrderID,
			Symbol:        ord.Symbol,
			Side:          ord.Side,
			Price:         ord.Price,
			Quantity:      ord.Quantity,
			Status:        ord.Status,
			CreatedAt:     ord.CreatedAt,
			ExecutedQty:   ord.ExecutedQty,
			AvgPrice:      ord.AvgPrice,
		})

		// 发布订單下單事件（回填策略信息）
		sName, sType, oSource := "", "", ""
		if info, ok := strategyMap[ord.ClientOrderID]; ok {
			sName, sType, oSource = info[0], info[1], info[2]
		} else if oSource == "" {
			// strategyMap 未命中時，從 ClientOrderID 解析訂單來源（如 _SL 後綴表示止損）
			oSource = utils.ParseOrderSource(ord.ClientOrderID)
		}
		if a.eventBus != nil {
			a.eventBus.Publish(&event.Event{
				Type: event.EventTypeOrderPlaced,
				Data: map[string]interface{}{
					"order_id":        ord.OrderID,
					"client_order_id": ord.ClientOrderID,
					"symbol":          ord.Symbol,
					"side":            ord.Side,
					"price":           ord.Price,
					"quantity":        ord.Quantity,
					"status":          ord.Status,
					"exchange":        a.exchange,
					"account":         a.accountID,
					"strategy_name":   sName,
					"strategy_type":   sType,
					"order_source":    oSource,
					"created_at":      ord.CreatedAt,
				},
			})
		}
	}
	return result
}

func (a *exchangeExecutorAdapter) BatchCancelOrders(orderIDs []int64) error {
	return a.executor.BatchCancelOrders(orderIDs)
}
