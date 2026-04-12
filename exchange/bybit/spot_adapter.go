package bybit

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"quantmesh/logger"
	"quantmesh/utils"
)

// BybitSpotAdapter Bybit 現貨交易所适配器
type BybitSpotAdapter struct {
	client           *BybitClient
	symbol           string
	priceDecimals    int
	quantityDecimals int
	baseAsset        string
	quoteAsset       string
	useTestnet       bool
	wsManager        *WebSocketManager // 公共現貨 tickers + 私有訂單流
	spotKlineWS      *KlineWebSocketManager
}

// NewBybitSpotAdapter 創建 Bybit 現貨适配器
func NewBybitSpotAdapter(cfg map[string]string, symbol string) (*BybitSpotAdapter, error) {
	apiKey := cfg["api_key"]
	secretKey := cfg["secret_key"]
	testnetStr := cfg["testnet"]

	useTestnet := false
	if testnetStr == "true" {
		useTestnet = true
		logger.Info("🌐 [Bybit Spot] 使用測試網模式")
	}

	if apiKey == "" || secretKey == "" {
		return nil, fmt.Errorf("Bybit API 配置不完整")
	}

	client := NewBybitClient(apiKey, secretKey, useTestnet)
	adapter := &BybitSpotAdapter{
		client:     client,
		symbol:     symbol,
		useTestnet: useTestnet,
	}

	ctxInit, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := adapter.fetchSpotInstrument(ctxInit); err != nil {
		logger.Warn("⚠️ [Bybit Spot] 獲取交易對信息失败: %v，使用默认精度", err)
		adapter.priceDecimals = 2
		adapter.quantityDecimals = 5
	}

	return adapter, nil
}

func (b *BybitSpotAdapter) fetchSpotInstrument(ctx context.Context) error {
	instruments, err := b.client.GetInstruments(ctx, "spot", b.symbol)
	if err != nil {
		return err
	}
	if len(instruments) == 0 {
		return fmt.Errorf("未找到現貨交易對: %s", b.symbol)
	}
	inst := instruments[0]
	tickSize, _ := strconv.ParseFloat(inst.PriceFilter.TickSize, 64)
	qtyStep, _ := strconv.ParseFloat(inst.LotSizeFilter.QtyStep, 64)
	b.priceDecimals = getPrecision(tickSize)
	b.quantityDecimals = getPrecision(qtyStep)
	b.baseAsset = inst.BaseCoin
	b.quoteAsset = inst.QuoteCoin
	logger.Info("ℹ️ [Bybit Spot] %s - 數量精度:%d, 價格精度:%d, 基础:%s, 计價:%s",
		b.symbol, b.quantityDecimals, b.priceDecimals, b.baseAsset, b.quoteAsset)
	return nil
}

// GetName 交易所名称
func (b *BybitSpotAdapter) GetName() string {
	return "Bybit Spot"
}

// GetMarketType 市場類型
func (b *BybitSpotAdapter) GetMarketType() string {
	return "spot"
}

// PlaceOrder 下單（現貨忽略 ReduceOnly）
func (b *BybitSpotAdapter) PlaceOrder(ctx context.Context, req *OrderRequest) (*Order, error) {
	sideStr := "Buy"
	if req.Side == SideSell {
		sideStr = "Sell"
	}
	orderType := string(req.Type)
	pDec := req.PriceDecimals
	if pDec <= 0 {
		pDec = b.priceDecimals
	}
	qDec := b.quantityDecimals
	qtyStr := fmt.Sprintf("%.*f", qDec, req.Quantity)
	priceStr := fmt.Sprintf("%.*f", pDec, req.Price)

	orderReq := map[string]interface{}{
		"category":  "spot",
		"symbol":    req.Symbol,
		"side":      sideStr,
		"orderType": orderType,
		"qty":       qtyStr,
		"price":     priceStr,
	}
	if req.PostOnly {
		orderReq["timeInForce"] = "PostOnly"
	} else {
		orderReq["timeInForce"] = "GTC"
	}
	if req.ClientOrderID != "" {
		orderReq["orderLinkId"] = utils.AddBrokerPrefix("bybit", req.ClientOrderID)
	}

	resp, err := b.client.PlaceOrder(ctx, orderReq)
	if err != nil {
		return nil, err
	}
	orderID, _ := strconv.ParseInt(resp.OrderId, 10, 64)
	return &Order{
		OrderID:       orderID,
		ClientOrderID: resp.OrderLinkId,
		Symbol:        req.Symbol,
		Side:          req.Side,
		Type:          req.Type,
		Price:         req.Price,
		Quantity:      req.Quantity,
		Status:        OrderStatusNew,
		CreatedAt:     time.Now(),
		UpdateTime:    time.Now().UnixMilli(),
	}, nil
}

// BatchPlaceOrders 批量下單
func (b *BybitSpotAdapter) BatchPlaceOrders(ctx context.Context, orders []*OrderRequest) ([]*Order, bool) {
	placed := make([]*Order, 0, len(orders))
	hasBalanceError := false
	for _, req := range orders {
		order, err := b.PlaceOrder(ctx, req)
		if err != nil {
			logger.Warn("⚠️ [Bybit Spot] 下單失败 %.2f %s: %v", req.Price, req.Side, err)
			if strings.Contains(err.Error(), "110007") || strings.Contains(err.Error(), "insufficient") {
				hasBalanceError = true
			}
			continue
		}
		placed = append(placed, order)
	}
	return placed, hasBalanceError
}

// CancelOrder 取消訂單
func (b *BybitSpotAdapter) CancelOrder(ctx context.Context, symbol string, orderID int64) error {
	err := b.client.CancelOrder(ctx, "spot", symbol, strconv.FormatInt(orderID, 10), "")
	if err != nil {
		if strings.Contains(err.Error(), "110001") || strings.Contains(err.Error(), "Order does not exist") {
			logger.Info("ℹ️ [Bybit Spot] 订單 %d 已不存在，跳過取消", orderID)
			return nil
		}
		return err
	}
	return nil
}

// BatchCancelOrders 批量撤單
func (b *BybitSpotAdapter) BatchCancelOrders(ctx context.Context, symbol string, orderIDs []int64) error {
	for _, id := range orderIDs {
		_ = b.CancelOrder(ctx, symbol, id)
		time.Sleep(50 * time.Millisecond)
	}
	return nil
}

// CancelAllOrders 取消該交易對下所有订單
func (b *BybitSpotAdapter) CancelAllOrders(ctx context.Context, symbol string) error {
	orders, err := b.client.GetOpenOrders(ctx, "spot", symbol)
	if err != nil {
		return err
	}
	for _, o := range orders {
		orderID, _ := strconv.ParseInt(o.OrderId, 10, 64)
		_ = b.CancelOrder(ctx, symbol, orderID)
		time.Sleep(50 * time.Millisecond)
	}
	return nil
}

// GetOrder 查詢訂單
func (b *BybitSpotAdapter) GetOrder(ctx context.Context, symbol string, orderID int64) (*Order, error) {
	order, err := b.client.GetOrder(ctx, "spot", symbol, strconv.FormatInt(orderID, 10), "")
	if err != nil {
		return nil, err
	}
	return b.convertOrder(order), nil
}

// GetOpenOrders 未完成订單
func (b *BybitSpotAdapter) GetOpenOrders(ctx context.Context, symbol string) ([]*Order, error) {
	orders, err := b.client.GetOpenOrders(ctx, "spot", symbol)
	if err != nil {
		return nil, err
	}
	result := make([]*Order, 0, len(orders))
	for i := range orders {
		result = append(result, b.convertOrder(&orders[i]))
	}
	return result, nil
}

func (b *BybitSpotAdapter) convertOrder(order *BybitOrder) *Order {
	orderID, _ := strconv.ParseInt(order.OrderId, 10, 64)
	price, _ := strconv.ParseFloat(order.Price, 64)
	qty, _ := strconv.ParseFloat(order.Qty, 64)
	execQty, _ := strconv.ParseFloat(order.CumExecQty, 64)
	avgPrice, _ := strconv.ParseFloat(order.AvgPrice, 64)
	updateTime, _ := strconv.ParseInt(order.UpdatedTime, 10, 64)
	var side Side
	if order.Side == "Buy" {
		side = SideBuy
	} else {
		side = SideSell
	}
	return &Order{
		OrderID:       orderID,
		ClientOrderID: order.OrderLinkId,
		Symbol:        order.Symbol,
		Side:          side,
		Type:          OrderType(order.OrderType),
		Price:         price,
		Quantity:      qty,
		ExecutedQty:   execQty,
		AvgPrice:      avgPrice,
		Status:        OrderStatus(order.OrderStatus),
		UpdateTime:    updateTime,
	}
}

// GetAccount 現貨账戶餘額
func (b *BybitSpotAdapter) GetAccount(ctx context.Context) (*Account, error) {
	balances, err := b.client.GetBalance(ctx, "SPOT")
	if err != nil {
		return nil, err
	}
	var totalEquity, totalAvail float64
	for _, bal := range balances {
		eq, _ := strconv.ParseFloat(bal.TotalEquity, 64)
		avail, _ := strconv.ParseFloat(bal.TotalAvailableBalance, 64)
		totalEquity += eq
		totalAvail += avail
	}
	return &Account{
		TotalWalletBalance: totalEquity,
		TotalMarginBalance:  totalEquity,
		AvailableBalance:   totalAvail,
		Positions:          nil,
	}, nil
}

// GetPositions 現貨“持倉”由基础资產餘額構成
func (b *BybitSpotAdapter) GetPositions(ctx context.Context, symbol string) ([]*Position, error) {
	balances, err := b.client.GetBalance(ctx, "SPOT")
	if err != nil {
		return nil, err
	}
	base := b.baseAsset
	if base == "" {
		base = strings.TrimSuffix(symbol, "USDT")
	}
	var size float64
	for _, bal := range balances {
		for _, c := range bal.Coin {
			if c.Coin == base {
				wb, _ := strconv.ParseFloat(c.WalletBalance, 64)
				aw, _ := strconv.ParseFloat(c.AvailableToWithdraw, 64)
				if wb > size {
					size = wb
				}
				if aw > size {
					size = aw
				}
				break
			}
		}
	}
	if size <= 0 {
		return nil, nil
	}
	price, _ := b.GetLatestPrice(ctx, symbol)
	if price <= 0 {
		price = 0
	}
	return []*Position{{
		Symbol:         symbol,
		Size:           size,
		EntryPrice:     price,
		MarkPrice:      price,
		UnrealizedPNL:  0,
		Leverage:       1,
		MarginType:     "spot",
		IsolatedMargin: 0,
	}}, nil
}

// GetBalance 某资產餘額
func (b *BybitSpotAdapter) GetBalance(ctx context.Context, asset string) (float64, error) {
	balances, err := b.client.GetBalance(ctx, "SPOT")
	if err != nil {
		return 0, err
	}
	for _, bal := range balances {
		for _, c := range bal.Coin {
			if c.Coin == asset {
				return strconv.ParseFloat(c.AvailableToWithdraw, 64)
			}
		}
	}
	return 0, nil
}

// StartOrderStream 現貨訂單流（v5/private，topic order，含現貨）
func (b *BybitSpotAdapter) StartOrderStream(ctx context.Context, callback func(interface{})) error {
	if b.wsManager == nil {
		b.wsManager = NewWebSocketManager(b.client.apiKey, b.client.secretKey, b.useTestnet)
	}
	return b.wsManager.Start(ctx, b.symbol, func(ou OrderUpdate) {
		callback(ou)
	})
}

// StopOrderStream 停止私有訂單流
func (b *BybitSpotAdapter) StopOrderStream() error {
	if b.wsManager != nil {
		b.wsManager.Stop()
	}
	return nil
}

// GetLatestPrice 最新價（優先 WebSocket 緩存）
func (b *BybitSpotAdapter) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	if b.wsManager != nil {
		if p := b.wsManager.GetLatestPrice(); p > 0 {
			return p, nil
		}
	}
	ticker, err := b.client.GetTicker(ctx, "spot", symbol)
	if err != nil {
		return 0, err
	}
	return strconv.ParseFloat(ticker.LastPrice, 64)
}

// StartPriceStream 公共現貨 WebSocket tickers.{symbol}
func (b *BybitSpotAdapter) StartPriceStream(ctx context.Context, symbol string, callback func(price float64)) error {
	if b.wsManager == nil {
		b.wsManager = NewWebSocketManager(b.client.apiKey, b.client.secretKey, b.useTestnet)
	}
	return b.wsManager.StartSpotPriceStream(ctx, b.symbol, callback)
}

// StartKlineStream 現貨公共 K 線（v5/public/spot）
func (b *BybitSpotAdapter) StartKlineStream(ctx context.Context, symbols []string, interval string, callback func(interface{})) error {
	if b.spotKlineWS != nil {
		b.spotKlineWS.Stop()
	}
	b.spotKlineWS = NewSpotKlineWebSocketManager(b.useTestnet)
	return b.spotKlineWS.Start(ctx, symbols, interval, callback)
}

// StopKlineStream 停止 K 線流
func (b *BybitSpotAdapter) StopKlineStream() error {
	if b.spotKlineWS != nil {
		b.spotKlineWS.Stop()
		b.spotKlineWS = nil
	}
	return nil
}

// GetHistoricalKlines 历史K線
func (b *BybitSpotAdapter) GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*Candle, error) {
	klines, err := b.client.GetKlines(ctx, "spot", symbol, interval, limit)
	if err != nil {
		return nil, err
	}
	candles := make([]*Candle, 0, len(klines))
	for _, k := range klines {
		open, _ := strconv.ParseFloat(k.OpenPrice, 64)
		high, _ := strconv.ParseFloat(k.HighPrice, 64)
		low, _ := strconv.ParseFloat(k.LowPrice, 64)
		closeP, _ := strconv.ParseFloat(k.ClosePrice, 64)
		vol, _ := strconv.ParseFloat(k.Volume, 64)
		ts, _ := strconv.ParseInt(k.StartTime, 10, 64)
		candles = append(candles, &Candle{
			Symbol:    symbol,
			Open:      open,
			High:      high,
			Low:       low,
			Close:     closeP,
			Volume:    vol,
			Timestamp: ts,
			IsClosed:  true,
		})
	}
	return candles, nil
}

// GetPriceDecimals 價格精度
func (b *BybitSpotAdapter) GetPriceDecimals() int {
	return b.priceDecimals
}

// GetQuantityDecimals 數量精度
func (b *BybitSpotAdapter) GetQuantityDecimals() int {
	return b.quantityDecimals
}

// GetBaseAsset 基础资產
func (b *BybitSpotAdapter) GetBaseAsset() string {
	return b.baseAsset
}

// GetQuoteAsset 计價资產
func (b *BybitSpotAdapter) GetQuoteAsset() string {
	return b.quoteAsset
}

// EstimateFinalOrderAmount 預估订單金額
func (b *BybitSpotAdapter) EstimateFinalOrderAmount(symbol string, price, quantity float64, reduceOnly bool) float64 {
	return price * quantity
}

// GetFundingRate 現貨無资金费率
func (b *BybitSpotAdapter) GetFundingRate(ctx context.Context, symbol string) (float64, error) {
	return 0, nil
}

// GetSpotPrice 現貨最新價
func (b *BybitSpotAdapter) GetSpotPrice(ctx context.Context, symbol string) (float64, error) {
	return b.GetLatestPrice(ctx, symbol)
}

// GetOrderBook 订單簿
func (b *BybitSpotAdapter) GetOrderBook(ctx context.Context, symbol string, limit int) (*OrderBook, error) {
	ob, err := b.client.GetOrderBook(ctx, "spot", symbol, limit)
	if err != nil {
		return nil, err
	}
	bids := make([]OrderBookLevel, 0, len(ob.Bids))
	for _, b := range ob.Bids {
		if len(b) >= 2 {
			price, _ := strconv.ParseFloat(b[0], 64)
			qty, _ := strconv.ParseFloat(b[1], 64)
			bids = append(bids, OrderBookLevel{Price: price, Quantity: qty})
		}
	}
	asks := make([]OrderBookLevel, 0, len(ob.Asks))
	for _, a := range ob.Asks {
		if len(a) >= 2 {
			price, _ := strconv.ParseFloat(a[0], 64)
			qty, _ := strconv.ParseFloat(a[1], 64)
			asks = append(asks, OrderBookLevel{Price: price, Quantity: qty})
		}
	}
	return &OrderBook{
		Symbol:    symbol,
		Bids:      bids,
		Asks:      asks,
		Timestamp: ob.TS,
	}, nil
}

// InternalTransfer 現貨适配器暫不支援內部轉帳
func (b *BybitSpotAdapter) InternalTransfer(ctx context.Context, fromAccount, toAccount, asset string, amount float64) (string, error) {
	return "", fmt.Errorf("Bybit 現貨适配器暫不支援內部轉帳，请在网页端操作")
}

// GetOrderFills 查詢成交明細（category=spot）
func (b *BybitSpotAdapter) GetOrderFills(ctx context.Context, symbol string, orderID int64) ([]*BybitOrderFill, error) {
	oid := ""
	if orderID != 0 {
		oid = strconv.FormatInt(orderID, 10)
	}
	executions, err := b.client.GetOrderFills(ctx, "spot", symbol, oid)
	if err != nil {
		return nil, err
	}
	fills := make([]*BybitOrderFill, 0, len(executions))
	for _, exec := range executions {
		price, _ := strconv.ParseFloat(exec.ExecPrice, 64)
		qty, _ := strconv.ParseFloat(exec.ExecQty, 64)
		commission, _ := strconv.ParseFloat(exec.ExecFee, 64)
		tradeTime, _ := strconv.ParseInt(exec.ExecTime, 10, 64)
		ordID, _ := strconv.ParseInt(exec.OrderId, 10, 64)
		if ordID == 0 {
			ordID = orderID
		}
		fills = append(fills, &BybitOrderFill{
			OrderID:         ordID,
			TradeID:         exec.TradeId,
			Symbol:          exec.Symbol,
			Side:            exec.Side,
			Price:           price,
			Quantity:        qty,
			Commission:      commission,
			CommissionAsset: exec.FeeCurrency,
			TradeTime:       tradeTime,
			IsMaker:         exec.IsMaker,
		})
	}
	return fills, nil
}
