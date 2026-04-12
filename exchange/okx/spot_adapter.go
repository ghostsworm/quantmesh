package okx

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"quantmesh/logger"
	"quantmesh/utils"
)

// symbolToSpotInstId BTCUSDT -> BTC-USDT
func symbolToSpotInstId(symbol string) string {
	symbol = strings.TrimSpace(symbol)
	if strings.HasSuffix(symbol, "USDT") {
		base := strings.TrimSuffix(symbol, "USDT")
		return base + "-USDT"
	}
	if strings.Contains(symbol, "USDT") {
		return strings.ReplaceAll(symbol, "USDT", "-USDT")
	}
	return symbol
}

// OKXSpotAdapter OKX 現貨适配器
type OKXSpotAdapter struct {
	client           *OKXClient
	symbol           string
	instId           string // 如 BTC-USDT
	priceDecimals    int
	quantityDecimals int
	baseAsset        string
	quoteAsset       string
	useTestnet       bool
	wsManager        *WebSocketManager // 公共 tickers 價格流（與合約適配器相同）
}

// NewOKXSpotAdapter 創建 OKX 現貨适配器
func NewOKXSpotAdapter(cfg map[string]string, symbol string) (*OKXSpotAdapter, error) {
	apiKey := cfg["api_key"]
	secretKey := cfg["secret_key"]
	passphrase := cfg["passphrase"]
	testnet := false
	if v, ok := cfg["testnet"]; ok && (v == "true" || v == "1") {
		testnet = true
	}
	if apiKey == "" || secretKey == "" || passphrase == "" {
		return nil, fmt.Errorf("OKX API 配置不完整（現貨需要 api_key、secret_key、passphrase）")
	}
	client := NewOKXClient(apiKey, secretKey, passphrase, testnet)
	instId := symbolToSpotInstId(symbol)
	adapter := &OKXSpotAdapter{
		client:     client,
		symbol:     symbol,
		instId:     instId,
		useTestnet: testnet,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := adapter.fetchSpotInstrument(ctx); err != nil {
		logger.Warn("⚠️ [OKX Spot] 獲取交易對信息失败: %v，使用默认精度", err)
		adapter.priceDecimals = 2
		adapter.quantityDecimals = 5
		adapter.baseAsset = strings.Split(instId, "-")[0]
		adapter.quoteAsset = "USDT"
	}
	return adapter, nil
}

func (o *OKXSpotAdapter) fetchSpotInstrument(ctx context.Context) error {
	instruments, err := o.client.GetInstruments(ctx, "SPOT", o.instId)
	if err != nil || len(instruments) == 0 {
		return fmt.Errorf("未找到現貨交易對: %s", o.instId)
	}
	inst := instruments[0]
	tickSz, _ := strconv.ParseFloat(inst.TickSz, 64)
	lotSz, _ := strconv.ParseFloat(inst.LotSz, 64)
	o.priceDecimals = getPrecision(tickSz)
	o.quantityDecimals = getPrecision(lotSz)
	o.baseAsset = inst.CtValCcy
	if o.baseAsset == "" {
		parts := strings.Split(o.instId, "-")
		if len(parts) >= 1 {
			o.baseAsset = parts[0]
		}
	}
	o.quoteAsset = inst.SettleCcy
	if o.quoteAsset == "" {
		o.quoteAsset = "USDT"
	}
	logger.Info("ℹ️ [OKX Spot] %s - 數量精度:%d, 價格精度:%d", o.instId, o.quantityDecimals, o.priceDecimals)
	return nil
}

// GetName 交易所名称
func (o *OKXSpotAdapter) GetName() string {
	return "OKX Spot"
}

// GetMarketType 市場類型
func (o *OKXSpotAdapter) GetMarketType() string {
	return "spot"
}

// PlaceOrder 下單（現貨 tdMode=cash，忽略 ReduceOnly）
func (o *OKXSpotAdapter) PlaceOrder(ctx context.Context, req *OrderRequest) (*Order, error) {
	side := string(req.Side)
	orderType := string(req.Type)
	orderReq := map[string]interface{}{
		"instId":  o.instId,
		"tdMode":  "cash", // 現貨
		"side":    side,
		"ordType": orderType,
		"sz":      fmt.Sprintf("%.*f", o.quantityDecimals, req.Quantity),
		"px":      fmt.Sprintf("%.*f", req.PriceDecimals, req.Price),
	}
	if req.PostOnly {
		orderReq["postOnly"] = true
	}
	if req.ClientOrderID != "" {
		orderReq["clOrdId"] = utils.AddBrokerPrefix("okx", req.ClientOrderID)
	}
	resp, err := o.client.PlaceOrder(ctx, orderReq)
	if err != nil {
		return nil, err
	}
	if len(resp) == 0 {
		return nil, fmt.Errorf("下單响应為空")
	}
	r := resp[0]
	if r.SCode != "0" {
		return nil, fmt.Errorf("下單失败: %s - %s", r.SCode, r.SMsg)
	}
	orderID, _ := strconv.ParseInt(r.OrdId, 10, 64)
	return &Order{
		OrderID:       orderID,
		ClientOrderID: r.ClOrdId,
		Symbol:        o.symbol,
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
func (o *OKXSpotAdapter) BatchPlaceOrders(ctx context.Context, orders []*OrderRequest) ([]*Order, bool) {
	placed := make([]*Order, 0, len(orders))
	hasErr := false
	for _, req := range orders {
		order, err := o.PlaceOrder(ctx, req)
		if err != nil {
			logger.Warn("⚠️ [OKX Spot] 下單失败: %v", err)
			if strings.Contains(err.Error(), "51008") || strings.Contains(err.Error(), "insufficient") {
				hasErr = true
			}
			continue
		}
		placed = append(placed, order)
	}
	return placed, hasErr
}

// CancelOrder 取消訂單
func (o *OKXSpotAdapter) CancelOrder(ctx context.Context, symbol string, orderID int64) error {
	return o.client.CancelOrder(ctx, o.instId, strconv.FormatInt(orderID, 10), "")
}

// BatchCancelOrders 批量撤單
func (o *OKXSpotAdapter) BatchCancelOrders(ctx context.Context, symbol string, orderIDs []int64) error {
	if len(orderIDs) == 0 {
		return nil
	}
	ids := make([]string, len(orderIDs))
	for i, id := range orderIDs {
		ids[i] = strconv.FormatInt(id, 10)
	}
	return o.client.BatchCancelOrders(ctx, o.instId, ids)
}

// CancelAllOrders 取消該交易對下所有订單
func (o *OKXSpotAdapter) CancelAllOrders(ctx context.Context, symbol string) error {
	orders, err := o.client.GetOpenOrdersByInstType(ctx, "SPOT", o.instId)
	if err != nil {
		return err
	}
	for _, ord := range orders {
		id, _ := strconv.ParseInt(ord.OrdId, 10, 64)
		_ = o.CancelOrder(ctx, symbol, id)
		time.Sleep(50 * time.Millisecond)
	}
	return nil
}

// GetOrder 查詢訂單
func (o *OKXSpotAdapter) GetOrder(ctx context.Context, symbol string, orderID int64) (*Order, error) {
	ord, err := o.client.GetOrder(ctx, o.instId, strconv.FormatInt(orderID, 10), "")
	if err != nil {
		return nil, err
	}
	price, _ := strconv.ParseFloat(ord.Px, 64)
	qty, _ := strconv.ParseFloat(ord.Sz, 64)
	execQty, _ := strconv.ParseFloat(ord.AccFillSz, 64)
	avgPx, _ := strconv.ParseFloat(ord.AvgPx, 64)
	uTime, _ := strconv.ParseInt(ord.UTime, 10, 64)
	return &Order{
		OrderID:       orderID,
		ClientOrderID: ord.ClOrdId,
		Symbol:        o.symbol,
		Side:          Side(ord.Side),
		Type:          OrderType(ord.OrdType),
		Price:         price,
		Quantity:      qty,
		ExecutedQty:   execQty,
		AvgPrice:      avgPx,
		Status:        OrderStatus(ord.State),
		UpdateTime:    uTime,
	}, nil
}

// GetOpenOrders 未完成订單
func (o *OKXSpotAdapter) GetOpenOrders(ctx context.Context, symbol string) ([]*Order, error) {
	orders, err := o.client.GetOpenOrdersByInstType(ctx, "SPOT", o.instId)
	if err != nil {
		return nil, err
	}
	result := make([]*Order, 0, len(orders))
	for _, ord := range orders {
		price, _ := strconv.ParseFloat(ord.Px, 64)
		qty, _ := strconv.ParseFloat(ord.Sz, 64)
		execQty, _ := strconv.ParseFloat(ord.AccFillSz, 64)
		avgPx, _ := strconv.ParseFloat(ord.AvgPx, 64)
		orderID, _ := strconv.ParseInt(ord.OrdId, 10, 64)
		uTime, _ := strconv.ParseInt(ord.UTime, 10, 64)
		result = append(result, &Order{
			OrderID:       orderID,
			ClientOrderID: ord.ClOrdId,
			Symbol:        o.symbol,
			Side:          Side(ord.Side),
			Type:          OrderType(ord.OrdType),
			Price:         price,
			Quantity:      qty,
			ExecutedQty:   execQty,
			AvgPrice:      avgPx,
			Status:        OrderStatus(ord.State),
			UpdateTime:    uTime,
		})
	}
	return result, nil
}

// GetAccount 現貨账戶餘額
func (o *OKXSpotAdapter) GetAccount(ctx context.Context) (*Account, error) {
	balances, err := o.client.GetBalance(ctx)
	if err != nil {
		return nil, err
	}
	var total, available float64
	for _, b := range balances {
		for _, d := range b.Details {
			eq, _ := strconv.ParseFloat(d.Eq, 64)
			avail, _ := strconv.ParseFloat(d.AvailBal, 64)
			total += eq
			if d.Ccy == "USDT" || d.Ccy == "USDC" {
				available += avail
			}
		}
	}
	return &Account{
		TotalWalletBalance: total,
		TotalMarginBalance:  total,
		AvailableBalance:   available,
		Positions:          nil,
	}, nil
}

// GetPositions 現貨“持倉”由基础资產餘額構成
func (o *OKXSpotAdapter) GetPositions(ctx context.Context, symbol string) ([]*Position, error) {
	balances, err := o.client.GetBalance(ctx)
	if err != nil {
		return nil, err
	}
	base := o.baseAsset
	if base == "" {
		base = strings.Split(o.instId, "-")[0]
	}
	var free float64
	for _, b := range balances {
		for _, d := range b.Details {
			if d.Ccy == base {
				free, _ = strconv.ParseFloat(d.AvailBal, 64)
				eq, _ := strconv.ParseFloat(d.Eq, 64)
				if eq > free {
					free = eq
				}
				break
			}
		}
	}
	if free <= 0 {
		return nil, nil
	}
	price, _ := o.GetLatestPrice(ctx, symbol)
	if price <= 0 {
		price = 0
	}
	return []*Position{{
		Symbol:         o.symbol,
		Size:           free,
		EntryPrice:     price,
		MarkPrice:      price,
		UnrealizedPNL:  0,
		Leverage:       1,
		MarginType:     "spot",
		IsolatedMargin: 0,
	}}, nil
}

// GetBalance 某资產餘額
func (o *OKXSpotAdapter) GetBalance(ctx context.Context, asset string) (float64, error) {
	balances, err := o.client.GetBalance(ctx)
	if err != nil {
		return 0, err
	}
	for _, b := range balances {
		for _, d := range b.Details {
			if d.Ccy == asset {
				return strconv.ParseFloat(d.AvailBal, 64)
			}
		}
	}
	return 0, nil
}

// StartOrderStream 現貨訂單流暂不實現
func (o *OKXSpotAdapter) StartOrderStream(ctx context.Context, callback func(interface{})) error {
	return fmt.Errorf("OKX 現貨訂單流暂未實現")
}

// StopOrderStream 無操作
func (o *OKXSpotAdapter) StopOrderStream() error {
	return nil
}

// GetLatestPrice 最新價（優先使用 WebSocket 緩存，與 OKX 合約適配器一致）
func (o *OKXSpotAdapter) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	if o.wsManager != nil {
		price := o.wsManager.GetLatestPrice()
		if price > 0 {
			return price, nil
		}
	}
	ticker, err := o.client.GetTicker(ctx, o.instId)
	if err != nil {
		return 0, err
	}
	return strconv.ParseFloat(ticker.Last, 64)
}

// StartPriceStream 公共 WebSocket 訂閱 tickers（現貨 instId 如 BTC-USDT，與 REST 一致）
func (o *OKXSpotAdapter) StartPriceStream(ctx context.Context, symbol string, callback func(price float64)) error {
	if o.wsManager == nil {
		o.wsManager = NewWebSocketManager(o.client.apiKey, o.client.secretKey, o.client.passphrase, o.useTestnet)
	}
	return o.wsManager.StartPriceStream(ctx, o.instId, callback)
}

// StartKlineStream 暂不實現
func (o *OKXSpotAdapter) StartKlineStream(ctx context.Context, symbols []string, interval string, callback func(interface{})) error {
	return fmt.Errorf("OKX 現貨K線流暂未實現")
}

// StopKlineStream 無操作
func (o *OKXSpotAdapter) StopKlineStream() error {
	return nil
}

// GetHistoricalKlines 历史K線
func (o *OKXSpotAdapter) GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*Candle, error) {
	bar := interval
	if bar == "" {
		bar = "1m"
	}
	klines, err := o.client.GetKlines(ctx, o.instId, bar, limit)
	if err != nil {
		return nil, err
	}
	candles := make([]*Candle, 0, len(klines))
	for _, k := range klines {
		open, _ := strconv.ParseFloat(k.O, 64)
		high, _ := strconv.ParseFloat(k.H, 64)
		low, _ := strconv.ParseFloat(k.L, 64)
		closeP, _ := strconv.ParseFloat(k.C, 64)
		vol, _ := strconv.ParseFloat(k.Vol, 64)
		ts, _ := strconv.ParseInt(k.Ts, 10, 64)
		candles = append(candles, &Candle{
			Symbol:    o.symbol,
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
func (o *OKXSpotAdapter) GetPriceDecimals() int {
	return o.priceDecimals
}

// GetQuantityDecimals 數量精度
func (o *OKXSpotAdapter) GetQuantityDecimals() int {
	return o.quantityDecimals
}

// GetBaseAsset 基础资產
func (o *OKXSpotAdapter) GetBaseAsset() string {
	return o.baseAsset
}

// GetQuoteAsset 计價资產
func (o *OKXSpotAdapter) GetQuoteAsset() string {
	return o.quoteAsset
}

// EstimateFinalOrderAmount 預估订單金額
func (o *OKXSpotAdapter) EstimateFinalOrderAmount(symbol string, price, quantity float64, reduceOnly bool) float64 {
	return price * quantity
}

// GetFundingRate 現貨無资金费率
func (o *OKXSpotAdapter) GetFundingRate(ctx context.Context, symbol string) (float64, error) {
	return 0, nil
}

// GetSpotPrice 現貨最新價
func (o *OKXSpotAdapter) GetSpotPrice(ctx context.Context, symbol string) (float64, error) {
	return o.GetLatestPrice(ctx, symbol)
}

// GetOrderBook 订單簿
func (o *OKXSpotAdapter) GetOrderBook(ctx context.Context, symbol string, limit int) (*OrderBook, error) {
	if limit <= 0 {
		limit = 20
	}
	ob, err := o.client.GetOrderBook(ctx, o.instId, limit)
	if err != nil {
		return nil, err
	}
	bids := make([]OrderBookLevel, 0, len(ob.Bids))
	for _, b := range ob.Bids {
		if len(b) < 2 {
			continue
		}
		price, _ := strconv.ParseFloat(b[0], 64)
		qty, _ := strconv.ParseFloat(b[1], 64)
		bids = append(bids, OrderBookLevel{Price: price, Quantity: qty})
	}
	asks := make([]OrderBookLevel, 0, len(ob.Asks))
	for _, a := range ob.Asks {
		if len(a) < 2 {
			continue
		}
		price, _ := strconv.ParseFloat(a[0], 64)
		qty, _ := strconv.ParseFloat(a[1], 64)
		asks = append(asks, OrderBookLevel{Price: price, Quantity: qty})
	}
	ts, _ := strconv.ParseInt(ob.TS, 10, 64)
	return &OrderBook{
		Symbol:    symbol,
		Bids:      bids,
		Asks:      asks,
		Timestamp: ts,
	}, nil
}

// InternalTransfer 內部轉帳（OKX 現貨與资金账戶间）
func (o *OKXSpotAdapter) InternalTransfer(ctx context.Context, fromAccount, toAccount, asset string, amount float64) (string, error) {
	return "", fmt.Errorf("OKX 現貨适配器暫不支援內部轉帳，请在网页端操作")
}
