package bitget

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

// BitgetSpotAdapter Bitget 現貨交易所适配器
type BitgetSpotAdapter struct {
	client           *Client
	symbol           string
	priceDecimals    int
	quantityDecimals int
	baseAsset        string
	quoteAsset       string
	testnet          bool
	spotPriceWS      *SpotPublicPriceWS
}

// NewBitgetSpotAdapter 創建 Bitget 現貨适配器
func NewBitgetSpotAdapter(cfg map[string]string, symbol string) (*BitgetSpotAdapter, error) {
	apiKey := cfg["api_key"]
	secretKey := cfg["secret_key"]
	passphrase := cfg["passphrase"]
	testnet := cfg["testnet"] == "true" || cfg["testnet"] == "1"

	if apiKey == "" || secretKey == "" || passphrase == "" {
		return nil, fmt.Errorf("Bitget API 配置不完整（現貨需要 api_key、secret_key、passphrase）")
	}

	client := NewClient(apiKey, secretKey, passphrase, testnet)
	adapter := &BitgetSpotAdapter{
		client:      client,
		symbol:      symbol,
		testnet:     testnet,
		spotPriceWS: NewSpotPublicPriceWS(testnet),
	}

	ctxInit, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := adapter.fetchSpotSymbol(ctxInit); err != nil {
		logger.Warn("⚠️ [Bitget Spot] 獲取交易對信息失败: %v，使用默认精度", err)
		adapter.priceDecimals = 2
		adapter.quantityDecimals = 5
	}

	return adapter, nil
}

func (b *BitgetSpotAdapter) fetchSpotSymbol(ctx context.Context) error {
	bitgetSymbol := convertToBitgetSymbol(b.symbol)
	path := fmt.Sprintf("/api/v2/spot/public/symbols?symbol=%s", bitgetSymbol)
	resp, err := b.client.DoRequest(ctx, "GET", path, nil)
	if err != nil {
		return err
	}
	var list []struct {
		Symbol             string `json:"symbol"`
		BaseCoin           string `json:"baseCoin"`
		QuoteCoin          string `json:"quoteCoin"`
		PricePrecision     string `json:"pricePrecision"`
		QuantityPrecision  string `json:"quantityPrecision"`
		MinOrderAmount     string `json:"minOrderAmount"`
	}
	if err := json.Unmarshal(resp.Data, &list); err != nil || len(list) == 0 {
		return fmt.Errorf("未找到現貨交易對: %s", b.symbol)
	}
	inst := list[0]
	b.baseAsset = inst.BaseCoin
	b.quoteAsset = inst.QuoteCoin
	b.priceDecimals, _ = strconv.Atoi(inst.PricePrecision)
	b.quantityDecimals, _ = strconv.Atoi(inst.QuantityPrecision)
	if b.priceDecimals <= 0 {
		b.priceDecimals = 2
	}
	if b.quantityDecimals <= 0 {
		b.quantityDecimals = 5
	}
	logger.Info("ℹ️ [Bitget Spot] %s - 數量精度:%d, 價格精度:%d, 基础:%s, 计價:%s",
		b.symbol, b.quantityDecimals, b.priceDecimals, b.baseAsset, b.quoteAsset)
	return nil
}

// GetName 交易所名称
func (b *BitgetSpotAdapter) GetName() string {
	return "Bitget Spot"
}

// GetMarketType 市場類型
func (b *BitgetSpotAdapter) GetMarketType() string {
	return "spot"
}

// PlaceOrder 下單（現貨忽略 ReduceOnly）
func (b *BitgetSpotAdapter) PlaceOrder(ctx context.Context, req *OrderRequest) (*Order, error) {
	sideStr := strings.ToLower(string(req.Side))
	orderTypeStr := strings.ToLower(string(req.Type))
	priceDecimals := req.PriceDecimals
	if priceDecimals <= 0 {
		priceDecimals = b.priceDecimals
	}
	body := map[string]interface{}{
		"symbol":    b.symbol,
		"side":      sideStr,
		"orderType": orderTypeStr,
		"size":      fmt.Sprintf("%.*f", b.quantityDecimals, req.Quantity),
		"price":     fmt.Sprintf("%.*f", priceDecimals, req.Price),
	}
	if req.PostOnly {
		body["force"] = "post_only"
	} else {
		body["force"] = "gtc"
	}
	if req.ClientOrderID != "" {
		body["clientOid"] = utils.AddBrokerPrefix("bitget", req.ClientOrderID)
	}

	resp, err := b.client.DoRequest(ctx, "POST", "/api/v2/spot/trade/place-order", body)
	if err != nil {
		return nil, err
	}
	var result struct {
		OrderID   string `json:"orderId"`
		ClientOid string `json:"clientOid"`
	}
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("解析下單响应失败: %w", err)
	}
	orderID, _ := strconv.ParseInt(result.OrderID, 10, 64)
	return &Order{
		OrderID:       orderID,
		ClientOrderID: result.ClientOid,
		Symbol:        b.symbol,
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
func (b *BitgetSpotAdapter) BatchPlaceOrders(ctx context.Context, orders []*OrderRequest) ([]*Order, bool) {
	placed := make([]*Order, 0, len(orders))
	hasBalanceError := false
	for _, req := range orders {
		order, err := b.PlaceOrder(ctx, req)
		if err != nil {
			logger.Warn("⚠️ [Bitget Spot] 下單失败 %.2f %s: %v", req.Price, req.Side, err)
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
func (b *BitgetSpotAdapter) CancelOrder(ctx context.Context, symbol string, orderID int64) error {
	body := map[string]interface{}{
		"symbol":  b.symbol,
		"orderId": strconv.FormatInt(orderID, 10),
	}
	_, err := b.client.DoRequest(ctx, "POST", "/api/v2/spot/trade/cancel-order", body)
	if err != nil {
		if strings.Contains(err.Error(), "order does not exist") || strings.Contains(err.Error(), "40029") {
			logger.Info("ℹ️ [Bitget Spot] 订單 %d 已不存在，跳過取消", orderID)
			return nil
		}
		return err
	}
	return nil
}

// BatchCancelOrders 批量撤單
func (b *BitgetSpotAdapter) BatchCancelOrders(ctx context.Context, symbol string, orderIDs []int64) error {
	for _, id := range orderIDs {
		_ = b.CancelOrder(ctx, symbol, id)
		time.Sleep(80 * time.Millisecond)
	}
	return nil
}

// CancelAllOrders 取消該交易對下所有订單
func (b *BitgetSpotAdapter) CancelAllOrders(ctx context.Context, symbol string) error {
	path := fmt.Sprintf("/api/v2/spot/trade/unfilled-orders?symbol=%s", b.symbol)
	resp, err := b.client.DoRequest(ctx, "GET", path, nil)
	if err != nil {
		return err
	}
	var list []struct {
		OrderID string `json:"orderId"`
	}
	if err := json.Unmarshal(resp.Data, &list); err != nil {
		return err
	}
	for _, o := range list {
		orderID, _ := strconv.ParseInt(o.OrderID, 10, 64)
		_ = b.CancelOrder(ctx, symbol, orderID)
		time.Sleep(80 * time.Millisecond)
	}
	return nil
}

// GetOrder 查詢訂單
func (b *BitgetSpotAdapter) GetOrder(ctx context.Context, symbol string, orderID int64) (*Order, error) {
	path := fmt.Sprintf("/api/v2/spot/trade/order-info?symbol=%s&orderId=%d", b.symbol, orderID)
	resp, err := b.client.DoRequest(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}
	var list []struct {
		OrderID     string `json:"orderId"`
		ClientOid   string `json:"clientOid"`
		Symbol      string `json:"symbol"`
		Side        string `json:"side"`
		OrderType   string `json:"orderType"`
		Price       string `json:"price"`
		Size        string `json:"size"`
		FilledSize  string `json:"filledSize"`
		AvgPrice    string `json:"avgPrice"`
		Status      string `json:"status"`
		UpdateTime  string `json:"updateTime"`
	}
	if err := json.Unmarshal(resp.Data, &list); err != nil || len(list) == 0 {
		return nil, fmt.Errorf("订單不存在: %d", orderID)
	}
	o := list[0]
	price, _ := strconv.ParseFloat(o.Price, 64)
	qty, _ := strconv.ParseFloat(o.Size, 64)
	execQty, _ := strconv.ParseFloat(o.FilledSize, 64)
	avgPrice, _ := strconv.ParseFloat(o.AvgPrice, 64)
	uTime, _ := strconv.ParseInt(o.UpdateTime, 10, 64)
	var side Side
	if strings.ToUpper(o.Side) == "BUY" || o.Side == "buy" {
		side = SideBuy
	} else {
		side = SideSell
	}
	ordID, _ := strconv.ParseInt(o.OrderID, 10, 64)
	return &Order{
		OrderID:       ordID,
		ClientOrderID: o.ClientOid,
		Symbol:        o.Symbol,
		Side:          side,
		Type:          OrderType(o.OrderType),
		Price:         price,
		Quantity:      qty,
		ExecutedQty:   execQty,
		AvgPrice:      avgPrice,
		Status:        OrderStatus(o.Status),
		UpdateTime:    uTime,
	}, nil
}

// GetOpenOrders 未完成订單
func (b *BitgetSpotAdapter) GetOpenOrders(ctx context.Context, symbol string) ([]*Order, error) {
	path := fmt.Sprintf("/api/v2/spot/trade/unfilled-orders?symbol=%s", b.symbol)
	resp, err := b.client.DoRequest(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}
	var list []struct {
		OrderID     string `json:"orderId"`
		ClientOid   string `json:"clientOid"`
		Symbol      string `json:"symbol"`
		Side        string `json:"side"`
		OrderType   string `json:"orderType"`
		Price       string `json:"price"`
		Size        string `json:"size"`
		FilledSize  string `json:"filledSize"`
		AvgPrice    string `json:"avgPrice"`
		Status      string `json:"status"`
		UpdateTime  string `json:"updateTime"`
	}
	if err := json.Unmarshal(resp.Data, &list); err != nil {
		return nil, err
	}
	orders := make([]*Order, 0, len(list))
	for _, o := range list {
		price, _ := strconv.ParseFloat(o.Price, 64)
		qty, _ := strconv.ParseFloat(o.Size, 64)
		execQty, _ := strconv.ParseFloat(o.FilledSize, 64)
		avgPrice, _ := strconv.ParseFloat(o.AvgPrice, 64)
		uTime, _ := strconv.ParseInt(o.UpdateTime, 10, 64)
		var side Side
		if strings.ToUpper(o.Side) == "BUY" || o.Side == "buy" {
			side = SideBuy
		} else {
			side = SideSell
		}
		ordID, _ := strconv.ParseInt(o.OrderID, 10, 64)
		orders = append(orders, &Order{
			OrderID:       ordID,
			ClientOrderID: o.ClientOid,
			Symbol:        o.Symbol,
			Side:          side,
			Type:          OrderType(o.OrderType),
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
func (b *BitgetSpotAdapter) GetAccount(ctx context.Context) (*Account, error) {
	resp, err := b.client.DoRequest(ctx, "GET", "/api/v2/spot/account/assets", nil)
	if err != nil {
		return nil, err
	}
	var list []struct {
		Coin      string `json:"coin"`
		Available string `json:"available"`
		Locked    string `json:"locked"`
	}
	if err := json.Unmarshal(resp.Data, &list); err != nil {
		return nil, err
	}
	var total, available float64
	for _, c := range list {
		avail, _ := strconv.ParseFloat(c.Available, 64)
		locked, _ := strconv.ParseFloat(c.Locked, 64)
		total += avail + locked
		if c.Coin == "USDT" || c.Coin == "USDC" {
			available += avail
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
func (b *BitgetSpotAdapter) GetPositions(ctx context.Context, symbol string) ([]*Position, error) {
	resp, err := b.client.DoRequest(ctx, "GET", "/api/v2/spot/account/assets", nil)
	if err != nil {
		return nil, err
	}
	var list []struct {
		Coin      string `json:"coin"`
		Available string `json:"available"`
		Locked    string `json:"locked"`
	}
	if err := json.Unmarshal(resp.Data, &list); err != nil {
		return nil, err
	}
	base := b.baseAsset
	if base == "" {
		base = strings.TrimSuffix(symbol, "USDT")
	}
	var size float64
	for _, c := range list {
		if c.Coin == base {
			avail, _ := strconv.ParseFloat(c.Available, 64)
			locked, _ := strconv.ParseFloat(c.Locked, 64)
			size = avail + locked
			break
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
func (b *BitgetSpotAdapter) GetBalance(ctx context.Context, asset string) (float64, error) {
	resp, err := b.client.DoRequest(ctx, "GET", "/api/v2/spot/account/assets", nil)
	if err != nil {
		return 0, err
	}
	var list []struct {
		Coin      string `json:"coin"`
		Available string `json:"available"`
	}
	if err := json.Unmarshal(resp.Data, &list); err != nil {
		return 0, err
	}
	for _, c := range list {
		if c.Coin == asset {
			return strconv.ParseFloat(c.Available, 64)
		}
	}
	return 0, nil
}

// StartOrderStream 現貨訂單流暂不實現
func (b *BitgetSpotAdapter) StartOrderStream(ctx context.Context, callback func(interface{})) error {
	return fmt.Errorf("Bitget 現貨訂單流暂未實現")
}

// StopOrderStream 無操作
func (b *BitgetSpotAdapter) StopOrderStream() error {
	return nil
}

// GetLatestPrice 最新價（優先 WebSocket 緩存）
func (b *BitgetSpotAdapter) GetLatestPrice(ctx context.Context, symbol string) (float64, error) {
	if b.spotPriceWS != nil {
		if p := b.spotPriceWS.GetLatestPrice(); p > 0 {
			return p, nil
		}
	}
	bitgetSymbol := convertToBitgetSymbol(symbol)
	path := fmt.Sprintf("/api/v2/spot/market/tickers?symbol=%s", bitgetSymbol)
	resp, err := b.client.DoRequest(ctx, "GET", path, nil)
	if err != nil {
		return 0, err
	}
	var results []struct {
		Symbol string `json:"symbol"`
		LastPr string `json:"lastPr"`
	}
	if err := json.Unmarshal(resp.Data, &results); err != nil || len(results) == 0 {
		return 0, fmt.Errorf("無價格數據: %s", symbol)
	}
	return strconv.ParseFloat(results[0].LastPr, 64)
}

// StartPriceStream 公共 WebSocket SPOT ticker
func (b *BitgetSpotAdapter) StartPriceStream(ctx context.Context, symbol string, callback func(price float64)) error {
	if b.spotPriceWS == nil {
		b.spotPriceWS = NewSpotPublicPriceWS(b.testnet)
	}
	inst := convertToBitgetSymbol(b.symbol)
	return b.spotPriceWS.Start(ctx, inst, callback)
}

// StartKlineStream 暂不實現
func (b *BitgetSpotAdapter) StartKlineStream(ctx context.Context, symbols []string, interval string, callback func(interface{})) error {
	return fmt.Errorf("Bitget 現貨K線流暂未實現")
}

// StopKlineStream 無操作
func (b *BitgetSpotAdapter) StopKlineStream() error {
	return nil
}

// GetHistoricalKlines 历史K線（Bitget 現貨 K 線接口）
func (b *BitgetSpotAdapter) GetHistoricalKlines(ctx context.Context, symbol string, interval string, limit int) ([]*Candle, error) {
	bitgetSymbol := convertToBitgetSymbol(symbol)
	bar := interval
	if bar == "1m" {
		bar = "1m"
	}
	path := fmt.Sprintf("/api/v2/spot/market/candles?symbol=%s&granularity=%s&limit=%d", bitgetSymbol, bar, limit)
	resp, err := b.client.DoRequest(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}
	var raw [][]string
	if err := json.Unmarshal(resp.Data, &raw); err != nil {
		return nil, err
	}
	candles := make([]*Candle, 0, len(raw))
	for _, r := range raw {
		if len(r) < 6 {
			continue
		}
		ts, _ := strconv.ParseInt(r[0], 10, 64)
		open, _ := strconv.ParseFloat(r[1], 64)
		high, _ := strconv.ParseFloat(r[2], 64)
		low, _ := strconv.ParseFloat(r[3], 64)
		closeP, _ := strconv.ParseFloat(r[4], 64)
		vol, _ := strconv.ParseFloat(r[5], 64)
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
func (b *BitgetSpotAdapter) GetPriceDecimals() int {
	return b.priceDecimals
}

// GetQuantityDecimals 數量精度
func (b *BitgetSpotAdapter) GetQuantityDecimals() int {
	return b.quantityDecimals
}

// GetBaseAsset 基础资產
func (b *BitgetSpotAdapter) GetBaseAsset() string {
	return b.baseAsset
}

// GetQuoteAsset 计價资產
func (b *BitgetSpotAdapter) GetQuoteAsset() string {
	return b.quoteAsset
}

// EstimateFinalOrderAmount 預估订單金額
func (b *BitgetSpotAdapter) EstimateFinalOrderAmount(symbol string, price, quantity float64, reduceOnly bool) float64 {
	return price * quantity
}

// GetFundingRate 現貨無资金费率
func (b *BitgetSpotAdapter) GetFundingRate(ctx context.Context, symbol string) (float64, error) {
	return 0, nil
}

// GetSpotPrice 現貨最新價
func (b *BitgetSpotAdapter) GetSpotPrice(ctx context.Context, symbol string) (float64, error) {
	return b.GetLatestPrice(ctx, symbol)
}

// GetOrderBook 订單簿
func (b *BitgetSpotAdapter) GetOrderBook(ctx context.Context, symbol string, limit int) (*OrderBook, error) {
	bitgetSymbol := convertToBitgetSymbol(symbol)
	path := fmt.Sprintf("/api/v2/spot/market/orderbook?symbol=%s&limit=%d", bitgetSymbol, limit)
	resp, err := b.client.DoRequest(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}
	var depth struct {
		Asks [][]string `json:"asks"`
		Bids [][]string `json:"bids"`
		TS   int64      `json:"ts"`
	}
	if err := json.Unmarshal(resp.Data, &depth); err != nil {
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
	return &OrderBook{
		Symbol:    symbol,
		Bids:      bids,
		Asks:      asks,
		Timestamp: depth.TS,
	}, nil
}

// InternalTransfer 現貨适配器暫不支援內部轉帳
func (b *BitgetSpotAdapter) InternalTransfer(ctx context.Context, fromAccount, toAccount, asset string, amount float64) (string, error) {
	return "", fmt.Errorf("Bitget 現貨适配器暫不支援內部轉帳，请在网页端操作")
}
