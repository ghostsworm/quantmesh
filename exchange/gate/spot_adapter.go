package gate

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"quantmesh/logger"
	"quantmesh/utils"
)

// GateSpotAdapter Gate.io 現貨交易所适配器
type GateSpotAdapter struct {
	client           *Client
	apiKey           string
	secretKey        string
	symbol           string
	gateSymbol       string // Gate格式（如 BTC_USDT）
	priceDecimals    int
	quantityDecimals int
	baseAsset        string
	quoteAsset       string
	testnet          bool
	spotPriceWS      *SpotPriceWebSocketManager
	spotOrderWS      *SpotOrderWebSocketManager
	spotKlineWS      *SpotKlineWebSocketManager
}

// NewGateSpotAdapter 創建 Gate.io 現貨适配器
func NewGateSpotAdapter(cfg map[string]string, symbol string) (*GateSpotAdapter, error) {
	apiKey := cfg["api_key"]
	secretKey := cfg["secret_key"]
	testnet := cfg["testnet"] == "true" || cfg["testnet"] == "1"

	if apiKey == "" || secretKey == "" {
		return nil, fmt.Errorf("Gate.io API 配置不完整")
	}

	client := NewClient(apiKey, secretKey, testnet)
	gateSymbol := convertToGateSymbol(symbol)

	adapter := &GateSpotAdapter{
		client:      client,
		apiKey:      apiKey,
		secretKey:   secretKey,
		symbol:      symbol,
		gateSymbol:  gateSymbol,
		testnet:     testnet,
		spotPriceWS: NewSpotPriceWebSocketManager(testnet),
	}

	ctxInit, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := adapter.fetchSpotSymbol(ctxInit); err != nil {
		logger.Warn("⚠️ [Gate Spot] 獲取交易對信息失败: %v，使用默认精度", err)
		adapter.priceDecimals = 2
		adapter.quantityDecimals = 5
		parts := strings.Split(gateSymbol, "_")
		if len(parts) == 2 {
			adapter.baseAsset = parts[0]
			adapter.quoteAsset = parts[1]
		}
	}

	return adapter, nil
}

func (g *GateSpotAdapter) fetchSpotSymbol(ctx context.Context) error {
	path := fmt.Sprintf("/spot/currency_pairs/%s", g.gateSymbol)
	resp, err := g.client.DoRequest(ctx, "GET", path, "", nil)
	if err != nil {
		return err
	}
	var info struct {
		ID              string `json:"id"`
		Base            string `json:"base"`
		Quote           string `json:"quote"`
		AmountPrecision int    `json:"amount_precision"`
		Precision       int    `json:"precision"`
	}
	if err := json.Unmarshal(resp, &info); err != nil {
		return err
	}
	g.baseAsset = info.Base
	g.quoteAsset = info.Quote
	g.priceDecimals = info.Precision
	g.quantityDecimals = info.AmountPrecision
	if g.priceDecimals <= 0 {
		g.priceDecimals = 2
	}
	if g.quantityDecimals <= 0 {
		g.quantityDecimals = 5
	}
	logger.Info("ℹ️ [Gate Spot] %s - 數量精度:%d, 價格精度:%d, 基础:%s, 计價:%s",
		g.symbol, g.quantityDecimals, g.priceDecimals, g.baseAsset, g.quoteAsset)
	return nil
}

// GetName 交易所名称
func (g *GateSpotAdapter) GetName() string {
	return "Gate Spot"
}

// GetMarketType 市場類型
func (g *GateSpotAdapter) GetMarketType() string {
	return "spot"
}

// PlaceOrder 下單（現貨忽略 ReduceOnly）
func (g *GateSpotAdapter) PlaceOrder(ctx context.Context, req *OrderRequest) (*Order, error) {
	sideStr := strings.ToLower(string(req.Side))
	body := map[string]interface{}{
		"currency_pair": g.gateSymbol,
		"type":          "limit",
		"side":          sideStr,
		"amount":        fmt.Sprintf("%.*f", g.quantityDecimals, req.Quantity),
		"price":         fmt.Sprintf("%.*f", g.priceDecimals, req.Price),
	}
	if req.PostOnly {
		body["time_in_force"] = "poc"
	} else {
		body["time_in_force"] = "gtc"
	}
	if req.ClientOrderID != "" {
		body["text"] = utils.AddBrokerPrefix("gate", req.ClientOrderID)
	}

	resp, err := g.client.DoRequest(ctx, "POST", "/spot/orders", "", body)
	if err != nil {
		return nil, err
	}
	var result struct {
		ID            string `json:"id"`
		Text          string `json:"text"`
		CurrencyPair  string `json:"currency_pair"`
		Side          string `json:"side"`
		Type          string `json:"type"`
		Amount        string `json:"amount"`
		Price         string `json:"price"`
		FilledAmount  string `json:"filled_amount"`
		AvgDealPrice  string `json:"avg_deal_price"`
		Status        string `json:"status"`
		UpdateTime    string `json:"update_time"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, fmt.Errorf("解析下單响应失败: %w", err)
	}
	orderID, _ := strconv.ParseInt(result.ID, 10, 64)
	return &Order{
		OrderID:       orderID,
		ClientOrderID: result.Text,
		Symbol:        g.symbol,
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
func (g *GateSpotAdapter) BatchPlaceOrders(ctx context.Context, orders []*OrderRequest) ([]*Order, bool) {
	placed := make([]*Order, 0, len(orders))
	hasBalanceError := false
	for _, req := range orders {
		order, err := g.PlaceOrder(ctx, req)
		if err != nil {
			logger.Warn("⚠️ [Gate Spot] 下單失败 %.2f %s: %v", req.Price, req.Side, err)
			if strings.Contains(err.Error(), "insufficient") || strings.Contains(err.Error(), "balance") {
				hasBalanceError = true
			}
			continue
		}
		placed = append(placed, order)
	}
	return placed, hasBalanceError
}

// CancelOrder 取消訂單
func (g *GateSpotAdapter) CancelOrder(ctx context.Context, symbol string, orderID int64) error {
	path := fmt.Sprintf("/spot/orders/%d", orderID)
	query := fmt.Sprintf("currency_pair=%s", g.gateSymbol)
	_, err := g.client.DoRequest(ctx, "DELETE", path, query, nil)
	if err != nil {
		if strings.Contains(err.Error(), "ORDER_NOT_FOUND") || strings.Contains(err.Error(), "not found") {
			logger.Info("ℹ️ [Gate Spot] 订單 %d 已不存在，跳過取消", orderID)
			return nil
		}
		return err
	}
	return nil
}

// BatchCancelOrders 批量撤單
func (g *GateSpotAdapter) BatchCancelOrders(ctx context.Context, symbol string, orderIDs []int64) error {
	for _, id := range orderIDs {
		_ = g.CancelOrder(ctx, symbol, id)
		time.Sleep(80 * time.Millisecond)
	}
	return nil
}

// CancelAllOrders 取消該交易對下所有订單
func (g *GateSpotAdapter) CancelAllOrders(ctx context.Context, symbol string) error {
	orders, err := g.GetOpenOrders(ctx, symbol)
	if err != nil {
		return err
	}
	for _, o := range orders {
		_ = g.CancelOrder(ctx, symbol, o.OrderID)
		time.Sleep(80 * time.Millisecond)
	}
	return nil
}

// GetOrder 查詢訂單
func (g *GateSpotAdapter) GetOrder(ctx context.Context, symbol string, orderID int64) (*Order, error) {
	path := fmt.Sprintf("/spot/orders/%d", orderID)
	query := fmt.Sprintf("currency_pair=%s", g.gateSymbol)
	resp, err := g.client.DoRequest(ctx, "GET", path, query, nil)
	if err != nil {
		return nil, err
	}
	var result struct {
		ID            string `json:"id"`
		Text          string `json:"text"`
		CurrencyPair  string `json:"currency_pair"`
		Side          string `json:"side"`
		Type          string `json:"type"`
		Amount        string `json:"amount"`
		Price         string `json:"price"`
		FilledAmount  string `json:"filled_amount"`
		AvgDealPrice  string `json:"avg_deal_price"`
		Status        string `json:"status"`
		UpdateTimeMs  string `json:"update_time_ms"`
	}
	if err := json.Unmarshal(resp, &result); err != nil {
		return nil, err
	}
	price, _ := strconv.ParseFloat(result.Price, 64)
	qty, _ := strconv.ParseFloat(result.Amount, 64)
	execQty, _ := strconv.ParseFloat(result.FilledAmount, 64)
	avgPrice, _ := strconv.ParseFloat(result.AvgDealPrice, 64)
	uTime, _ := strconv.ParseInt(result.UpdateTimeMs, 10, 64)
	ordID, _ := strconv.ParseInt(result.ID, 10, 64)
	var side Side
	if strings.ToUpper(result.Side) == "BUY" {
		side = SideBuy
	} else {
		side = SideSell
	}
	return &Order{
		OrderID:       ordID,
		ClientOrderID: result.Text,
		Symbol:        g.symbol,
		Side:          side,
		Type:          OrderType(result.Type),
		Price:         price,
		Quantity:      qty,
		ExecutedQty:   execQty,
		AvgPrice:      avgPrice,
		Status:        OrderStatus(result.Status),
		UpdateTime:    uTime,
	}, nil
}

// GetOpenOrders 未完成订單
func (g *GateSpotAdapter) GetOpenOrders(ctx context.Context, symbol string) ([]*Order, error) {
	path := "/spot/orders"
	query := fmt.Sprintf("currency_pair=%s&status=open", g.gateSymbol)
	resp, err := g.client.DoRequest(ctx, "GET", path, query, nil)
	if err != nil {
		return nil, err
	}
	var list []struct {
		ID            string `json:"id"`
		Text          string `json:"text"`
		CurrencyPair  string `json:"currency_pair"`
		Side          string `json:"side"`
		Type          string `json:"type"`
		Amount        string `json:"amount"`
		Price         string `json:"price"`
		FilledAmount  string `json:"filled_amount"`
		AvgDealPrice  string `json:"avg_deal_price"`
		Status        string `json:"status"`
		UpdateTimeMs  string `json:"update_time_ms"`
	}
	if err := json.Unmarshal(resp, &list); err != nil {
		return nil, err
	}
	orders := make([]*Order, 0, len(list))
	for _, o := range list {
		price, _ := strconv.ParseFloat(o.Price, 64)
		qty, _ := strconv.ParseFloat(o.Amount, 64)
		execQty, _ := strconv.ParseFloat(o.FilledAmount, 64)
		avgPrice, _ := strconv.ParseFloat(o.AvgDealPrice, 64)
		uTime, _ := strconv.ParseInt(o.UpdateTimeMs, 10, 64)
		ordID, _ := strconv.ParseInt(o.ID, 10, 64)
		var side Side
		if strings.ToUpper(o.Side) == "BUY" {
			side = SideBuy
		} else {
			side = SideSell
		}
		orders = append(orders, &Order{
			OrderID:       ordID,
			ClientOrderID: o.Text,
			Symbol:        g.symbol,
			Side:          side,
			Type:          OrderType(o.Type),
			Price:         price,
			Quantity:      qty,
			ExecutedQty:   execQty,
			AvgPrice:      avgPrice,
			Status:        OrderStatus(o.Status),
			UpdateTime:    uTime,
		})
	}
	return orders, nil
}

// GetAccount 現貨账戶餘額
func (g *GateSpotAdapter) GetAccount(ctx context.Context) (*Account, error) {
	resp, err := g.client.DoRequest(ctx, "GET", "/spot/accounts", "", nil)
	if err != nil {
		return nil, err
	}
	var list []struct {
		Currency  string `json:"currency"`
		Available string `json:"available"`
		Locked    string `json:"locked"`
	}
	if err := json.Unmarshal(resp, &list); err != nil {
		return nil, err
	}
	var total, available float64
	for _, c := range list {
		avail, _ := strconv.ParseFloat(c.Available, 64)
		locked, _ := strconv.ParseFloat(c.Locked, 64)
		total += avail + locked
		if c.Currency == "USDT" || c.Currency == "USDC" {
			available += avail
		}
	}
	return &Account{
		TotalWalletBalance: total,
		TotalMarginBalance: total,
		AvailableBalance:   available,
		Positions:          nil,
	}, nil
}

// GetPositions 現貨"持倉"由基础资產餘額構成
func (g *GateSpotAdapter) GetPositions(ctx context.Context, symbol string) ([]*Position, error) {
	resp, err := g.client.DoRequest(ctx, "GET", "/spot/accounts", "", nil)
	if err != nil {
		return nil, err
	}
	var list []struct {
		Currency  string `json:"currency"`
		Available string `json:"available"`
		Locked    string `json:"locked"`
	}
	if err := json.Unmarshal(resp, &list); err != nil {
		return nil, err
	}
	base := g.baseAsset
	if base == "" {
		parts := strings.Split(g.gateSymbol, "_")
		if len(parts) > 0 {
			base = parts[0]
		}
	}
	var size float64
	for _, c := range list {
		if c.Currency == base {
			avail, _ := strconv.ParseFloat(c.Available, 64)
			locked, _ := strconv.ParseFloat(c.Locked, 64)
			size = avail + locked
			break
		}
	}
	if size <= 0 {
		return nil, nil
	}
	price, _ := g.GetLatestPrice(ctx, symbol)
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
func (g *GateSpotAdapter) GetBalance(ctx context.Context, asset string) (float64, error) {
	resp, err := g.client.DoRequest(ctx, "GET", "/spot/accounts", "", nil)
	if err != nil {
		return 0, err
	}
	var list []struct {
		Currency  string `json:"currency"`
		Available string `json:"available"`
	}
	if err := json.Unmarshal(resp, &list); err != nil {
		return 0, err
	}
	for _, c := range list {
		if c.Currency == asset {
			return strconv.ParseFloat(c.Available, 64)
		}
	}
	return 0, nil
}

// StartOrderStream 現貨私有訂單流（spot.orders）
func (g *GateSpotAdapter) StartOrderStream(ctx context.Context, callback func(interface{})) error {
	if g.spotOrderWS != nil {
		g.spotOrderWS.Stop()
	}
	g.spotOrderWS = NewSpotOrderWebSocketManager(g.apiKey, g.secretKey, g.gateSymbol, g.testnet)
	return g.spotOrderWS.Start(ctx, callback)
}

// StopOrderStream 停止現貨訂單流
func (g *GateSpotAdapter) StopOrderStream() error {
	if g.spotOrderWS != nil {
		g.spotOrderWS.Stop()
		g.spotOrderWS = nil
	}
	return nil
}

// GetLatestPrice 最新價（優先 WebSocket 緩存）
func (g *GateSpotAdapter) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	if g.spotPriceWS != nil {
		if p := g.spotPriceWS.GetLatestPrice(); p > 0 {
			return p, nil
		}
	}
	path := "/spot/tickers"
	query := fmt.Sprintf("currency_pair=%s", g.gateSymbol)
	resp, err := g.client.DoRequest(ctx, "GET", path, query, nil)
	if err != nil {
		return 0, err
	}
	var list []struct {
		CurrencyPair string `json:"currency_pair"`
		Last         string `json:"last"`
	}
	if err := json.Unmarshal(resp, &list); err != nil || len(list) == 0 {
		return 0, fmt.Errorf("無價格數據: %s", symbol)
	}
	return strconv.ParseFloat(list[0].Last, 64)
}

// StartPriceStream 公共 WebSocket spot.tickers
func (g *GateSpotAdapter) StartPriceStream(ctx context.Context, symbol string, callback func(price float64)) error {
	if g.spotPriceWS == nil {
		g.spotPriceWS = NewSpotPriceWebSocketManager(g.testnet)
	}
	return g.spotPriceWS.Start(ctx, g.gateSymbol, callback)
}

// StartKlineStream 現貨公共 K 線（spot.candlesticks）
func (g *GateSpotAdapter) StartKlineStream(ctx context.Context, symbols []string, interval string, callback func(interface{})) error {
	if g.spotKlineWS != nil {
		g.spotKlineWS.Stop()
	}
	gs := make([]string, 0, len(symbols))
	for _, s := range symbols {
		gs = append(gs, convertToGateSymbol(s))
	}
	g.spotKlineWS = NewSpotKlineWebSocketManager(g.testnet)
	return g.spotKlineWS.Start(ctx, gs, interval, callback)
}

// StopKlineStream 停止 K 線流
func (g *GateSpotAdapter) StopKlineStream() error {
	if g.spotKlineWS != nil {
		g.spotKlineWS.Stop()
		g.spotKlineWS = nil
	}
	return nil
}

// GetHistoricalKlines 历史K線
func (g *GateSpotAdapter) GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*Candle, error) {
	path := "/spot/candlesticks"
	query := fmt.Sprintf("currency_pair=%s&interval=%s&limit=%d", g.gateSymbol, interval, limit)
	resp, err := g.client.DoRequest(ctx, "GET", path, query, nil)
	if err != nil {
		return nil, err
	}
	var raw [][]interface{}
	if err := json.Unmarshal(resp, &raw); err != nil {
		return nil, err
	}
	candles := make([]*Candle, 0, len(raw))
	for _, r := range raw {
		if len(r) < 6 {
			continue
		}
		tsStr, _ := r[0].(string)
		ts, _ := strconv.ParseInt(tsStr, 10, 64)
		vol, _ := strconv.ParseFloat(r[1].(string), 64)
		closeP, _ := strconv.ParseFloat(r[2].(string), 64)
		high, _ := strconv.ParseFloat(r[3].(string), 64)
		low, _ := strconv.ParseFloat(r[4].(string), 64)
		open, _ := strconv.ParseFloat(r[5].(string), 64)
		candles = append(candles, &Candle{
			Symbol:    symbol,
			Open:      open,
			High:      high,
			Low:       low,
			Close:     closeP,
			Volume:    vol,
			Timestamp: ts * 1000,
			IsClosed:  true,
		})
	}
	return candles, nil
}

// GetPriceDecimals 價格精度
func (g *GateSpotAdapter) GetPriceDecimals() int {
	return g.priceDecimals
}

// GetQuantityDecimals 數量精度
func (g *GateSpotAdapter) GetQuantityDecimals() int {
	return g.quantityDecimals
}

// GetBaseAsset 基础资產
func (g *GateSpotAdapter) GetBaseAsset() string {
	return g.baseAsset
}

// GetQuoteAsset 计價资產
func (g *GateSpotAdapter) GetQuoteAsset() string {
	return g.quoteAsset
}

// EstimateFinalOrderAmount 預估订單金額
func (g *GateSpotAdapter) EstimateFinalOrderAmount(symbol string, price, quantity float64, reduceOnly bool) float64 {
	return price * quantity
}

// GetFundingRate 現貨無资金费率
func (g *GateSpotAdapter) GetFundingRate(ctx context.Context, symbol string) (float64, error) {
	return 0, nil
}

// GetSpotPrice 現貨最新價
func (g *GateSpotAdapter) GetSpotPrice(ctx context.Context, symbol string) (float64, error) {
	return g.GetLatestPrice(ctx, symbol)
}

// GetOrderBook 订單簿
func (g *GateSpotAdapter) GetOrderBook(ctx context.Context, symbol string, limit int) (*OrderBook, error) {
	path := "/spot/order_book"
	query := fmt.Sprintf("currency_pair=%s&limit=%d", g.gateSymbol, limit)
	resp, err := g.client.DoRequest(ctx, "GET", path, query, nil)
	if err != nil {
		return nil, err
	}
	var depth struct {
		Asks         [][]string `json:"asks"`
		Bids         [][]string `json:"bids"`
		OrderBookID  int64      `json:"order_book_id"`   // 订單簿版本 ID
		UpdateTimeMs int64      `json:"update_time_ms"`  // 更新時间（毫秒）
		Current      float64    `json:"current"`         // 部分接口返回秒级時间戳
	}
	if err := json.Unmarshal(resp, &depth); err != nil {
		return nil, err
	}
	bids := make([]OrderBookLevel, 0, len(depth.Bids))
	for _, bid := range depth.Bids {
		if len(bid) >= 2 {
			price, _ := strconv.ParseFloat(bid[0], 64)
			qty, _ := strconv.ParseFloat(bid[1], 64)
			bids = append(bids, OrderBookLevel{Price: price, Quantity: qty})
		}
	}
	asks := make([]OrderBookLevel, 0, len(depth.Asks))
	for _, ask := range depth.Asks {
		if len(ask) >= 2 {
			price, _ := strconv.ParseFloat(ask[0], 64)
			qty, _ := strconv.ParseFloat(ask[1], 64)
			asks = append(asks, OrderBookLevel{Price: price, Quantity: qty})
		}
	}
	ts := depth.UpdateTimeMs
	if ts == 0 && depth.Current > 0 {
		ts = int64(depth.Current * 1000)
	}
	if ts == 0 {
		ts = time.Now().UnixMilli()
	}
	return &OrderBook{
		Symbol:    symbol,
		Bids:      bids,
		Asks:      asks,
		Timestamp: ts,
	}, nil
}

// InternalTransfer 現貨适配器暫不支援內部轉帳
func (g *GateSpotAdapter) InternalTransfer(ctx context.Context, fromAccount, toAccount, asset string, amount float64) (string, error) {
	return "", fmt.Errorf("Gate 現貨适配器暫不支援內部轉帳，请在网页端操作")
}

// GateSpotTrade 現貨成交（/spot/my_trades）
type GateSpotTrade struct {
	ID           string `json:"id"`
	OrderID      string `json:"order_id"`
	CurrencyPair string `json:"currency_pair"`
	Side         string `json:"side"`
	Amount       string `json:"amount"`
	Price        string `json:"price"`
	Fee          string `json:"fee"`
	FeeCurrency  string `json:"fee_currency"`
	CreateTime   string `json:"create_time"`
}

// GetOrderFills 查詢成交記錄
func (g *GateSpotAdapter) GetOrderFills(ctx context.Context, symbol string, orderID int64) ([]GateSpotTrade, error) {
	path := "/spot/my_trades"
	query := fmt.Sprintf("currency_pair=%s", g.gateSymbol)
	if orderID != 0 {
		query += fmt.Sprintf("&order_id=%d", orderID)
	}
	resp, err := g.client.DoRequest(ctx, "GET", path, query, nil)
	if err != nil {
		return nil, err
	}
	var list []GateSpotTrade
	if err := json.Unmarshal(resp, &list); err != nil {
		return nil, fmt.Errorf("解析成交记录失败: %w", err)
	}
	return list, nil
}
