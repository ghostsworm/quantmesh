package bitget

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"strconv"
	"strings"
	"time"

	"quantmesh/logger"
	"quantmesh/utils"
)

// BitgetSpotAdapter Bitget 現貨交易所适配器
type BitgetSpotAdapter struct {
	client           *Client
	apiKey           string
	secretKey        string
	passphrase       string
	symbol           string
	priceDecimals    int
	quantityDecimals int
	baseAsset        string
	quoteAsset       string
	testnet          bool
	spotPriceWS      *SpotPublicPriceWS
	spotOrderWS      *WebSocketManager
	spotKlineWS      *KlineWebSocketManager
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
		apiKey:      apiKey,
		secretKey:   secretKey,
		passphrase:  passphrase,
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

// NewBitgetSpotAccountEvidenceAdapter creates a REST-only Spot account source
// without fetching symbol metadata or initializing any trading WebSocket.
func NewBitgetSpotAccountEvidenceAdapter(apiKey, secretKey, passphrase string, testnet bool) (*BitgetSpotAdapter, error) {
	if apiKey == "" || secretKey == "" || passphrase == "" {
		return nil, fmt.Errorf("Bitget Spot account evidence credentials are incomplete")
	}
	return &BitgetSpotAdapter{
		client:     NewClient(apiKey, secretKey, passphrase, testnet),
		apiKey:     apiKey,
		secretKey:  secretKey,
		passphrase: passphrase,
		testnet:    testnet,
	}, nil
}

func (b *BitgetSpotAdapter) fetchSpotSymbol(ctx context.Context) error {
	bitgetSymbol := convertToBitgetSymbol(b.symbol)
	path := fmt.Sprintf("/api/v2/spot/public/symbols?symbol=%s", bitgetSymbol)
	resp, err := b.client.DoRequest(ctx, "GET", path, nil)
	if err != nil {
		return err
	}
	var list []struct {
		Symbol            string `json:"symbol"`
		BaseCoin          string `json:"baseCoin"`
		QuoteCoin         string `json:"quoteCoin"`
		PricePrecision    string `json:"pricePrecision"`
		QuantityPrecision string `json:"quantityPrecision"`
		MinOrderAmount    string `json:"minOrderAmount"`
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
		OrderID    string `json:"orderId"`
		ClientOid  string `json:"clientOid"`
		Symbol     string `json:"symbol"`
		Side       string `json:"side"`
		OrderType  string `json:"orderType"`
		Price      string `json:"price"`
		Size       string `json:"size"`
		FilledSize string `json:"filledSize"`
		AvgPrice   string `json:"avgPrice"`
		Status     string `json:"status"`
		UpdateTime string `json:"updateTime"`
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

// GetOrderByClientOrderID queries spot order details by clientOid.
func (b *BitgetSpotAdapter) GetOrderByClientOrderID(ctx context.Context, symbol, clientOrderID string) (*Order, error) {
	if clientOrderID == "" {
		return nil, fmt.Errorf("Bitget 現貨查詢訂單需要客戶端訂單 ID")
	}
	clientOrderID = utils.AddBrokerPrefix("bitget", clientOrderID)
	path := fmt.Sprintf("/api/v2/spot/trade/orderInfo?symbol=%s&clientOid=%s", b.symbol, clientOrderID)
	resp, err := b.client.DoRequest(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}
	var orders []struct {
		OrderID    string `json:"orderId"`
		ClientOid  string `json:"clientOid"`
		Symbol     string `json:"symbol"`
		Side       string `json:"side"`
		OrderType  string `json:"orderType"`
		Price      string `json:"price"`
		Size       string `json:"size"`
		FilledSize string `json:"filledSize"`
		AvgPrice   string `json:"avgPrice"`
		Status     string `json:"status"`
		UpdateTime string `json:"updateTime"`
	}
	if err := json.Unmarshal(resp.Data, &orders); err != nil {
		return nil, fmt.Errorf("解析 Bitget 現貨 CID 訂單详情失败: %w", err)
	}
	matchedIndex := -1
	for i := range orders {
		if orders[i].ClientOid != clientOrderID || !strings.EqualFold(orders[i].Symbol, b.symbol) {
			continue
		}
		if matchedIndex >= 0 {
			return nil, fmt.Errorf("Bitget 現貨 CID 訂單不唯一(clientOid=%s)", clientOrderID)
		}
		matchedIndex = i
	}
	if matchedIndex < 0 {
		return nil, fmt.Errorf("Bitget 現貨 CID 訂單不存在或無精確匹配")
	}
	matched := orders[matchedIndex]
	price, _ := strconv.ParseFloat(matched.Price, 64)
	quantity, _ := strconv.ParseFloat(matched.Size, 64)
	executedQty, _ := strconv.ParseFloat(matched.FilledSize, 64)
	avgPrice, _ := strconv.ParseFloat(matched.AvgPrice, 64)
	updateTime, _ := strconv.ParseInt(matched.UpdateTime, 10, 64)
	orderID, _ := strconv.ParseInt(matched.OrderID, 10, 64)
	side := SideBuy
	if strings.EqualFold(matched.Side, "sell") {
		side = SideSell
	}
	return &Order{OrderID: orderID, ClientOrderID: matched.ClientOid, Symbol: matched.Symbol, Side: side, Type: OrderType(matched.OrderType), Price: price, Quantity: quantity, ExecutedQty: executedQty, AvgPrice: avgPrice, Status: OrderStatus(matched.Status), UpdateTime: updateTime}, nil
}

// GetOpenOrders 未完成订單
func (b *BitgetSpotAdapter) GetOpenOrders(ctx context.Context, symbol string) ([]*Order, error) {
	path := fmt.Sprintf("/api/v2/spot/trade/unfilled-orders?symbol=%s", b.symbol)
	resp, err := b.client.DoRequest(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}
	var list []struct {
		OrderID    string `json:"orderId"`
		ClientOid  string `json:"clientOid"`
		Symbol     string `json:"symbol"`
		Side       string `json:"side"`
		OrderType  string `json:"orderType"`
		Price      string `json:"price"`
		Size       string `json:"size"`
		FilledSize string `json:"filledSize"`
		AvgPrice   string `json:"avgPrice"`
		Status     string `json:"status"`
		UpdateTime string `json:"updateTime"`
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

// GetAccountOpenOrders returns all open spot orders, including TPSL orders.
func (b *BitgetSpotAdapter) GetAccountOpenOrders(ctx context.Context) ([]*Order, error) {
	rows, err := b.client.GetAccountSpotOpenOrders(ctx)
	if err != nil {
		return nil, err
	}
	orders := make([]*Order, 0, len(rows))
	for _, row := range rows {
		orderID, idErr := strconv.ParseInt(row.OrderID, 10, 64)
		quantity, quantityErr := parseBitgetOrderNumber("spot size", row.Size, false)
		filled, filledErr := parseBitgetOrderNumber("spot filled base volume", row.FilledSize, true)
		price, priceErr := parseBitgetOrderNumber("spot price", row.Price, true)
		avgPrice, avgPriceErr := parseBitgetOrderNumber("spot average price", row.AvgPrice, true)
		updatedAt, timeErr := strconv.ParseInt(row.UpdateTime, 10, 64)
		side, sideErr := bitgetOpenOrderSide(row.Side)
		status, statusErr := bitgetOpenOrderStatus(row.Status)
		if idErr != nil || orderID <= 0 || quantityErr != nil || quantity < 0 || filledErr != nil || filled < 0 ||
			priceErr != nil || price < 0 || avgPriceErr != nil || avgPrice < 0 || timeErr != nil ||
			sideErr != nil || statusErr != nil || strings.TrimSpace(row.Symbol) == "" {
			return nil, fmt.Errorf("Bitget spot account snapshot contains invalid order %q", row.OrderID)
		}
		orders = append(orders, &Order{OrderID: orderID, ClientOrderID: row.ClientOID, Symbol: row.Symbol,
			Side: side, Type: OrderType(row.OrderType), Price: price, Quantity: quantity, ExecutedQty: filled,
			AvgPrice: avgPrice, Status: status, UpdateTime: updatedAt})
	}
	return orders, nil
}

// GetAccount 現貨账戶餘額
func (b *BitgetSpotAdapter) GetAccount(ctx context.Context) (*Account, error) {
	resp, err := b.client.DoRequest(ctx, "GET", "/api/v2/spot/account/assets", nil)
	if err != nil {
		return nil, err
	}
	var list []bitgetSpotAccountAsset
	if err := json.Unmarshal(resp.Data, &list); err != nil {
		return nil, fmt.Errorf("decode Bitget spot balances: %w", err)
	}
	total, available, err := summarizeBitgetSpotQuoteBalance(list, b.quoteAsset)
	if err != nil {
		return nil, err
	}
	return &Account{
		TotalWalletBalance: total,
		TotalMarginBalance: total,
		AvailableBalance:   available,
		BalanceAsset:       strings.ToUpper(strings.TrimSpace(b.quoteAsset)),
		Positions:          nil,
	}, nil
}

type bitgetSpotAccountAsset struct {
	Coin      string `json:"coin"`
	Available string `json:"available"`
	Frozen    string `json:"frozen"`
	Locked    string `json:"locked"`
}

func summarizeBitgetSpotQuoteBalance(balances []bitgetSpotAccountAsset, quoteAsset string) (total, available float64, err error) {
	quoteAsset = strings.ToUpper(strings.TrimSpace(quoteAsset))
	if quoteAsset == "" {
		return 0, 0, fmt.Errorf("Bitget spot quote asset is required for balance valuation")
	}
	found := false
	for _, balance := range balances {
		if !strings.EqualFold(balance.Coin, quoteAsset) {
			continue
		}
		if found {
			return 0, 0, fmt.Errorf("duplicate Bitget %s balance entries", quoteAsset)
		}
		found = true
		free, parseErr := strconv.ParseFloat(balance.Available, 64)
		if parseErr != nil || math.IsNaN(free) || math.IsInf(free, 0) || free < 0 {
			return 0, 0, fmt.Errorf("invalid Bitget %s available balance %q", quoteAsset, balance.Available)
		}
		locked, parseErr := strconv.ParseFloat(balance.Locked, 64)
		if parseErr != nil || math.IsNaN(locked) || math.IsInf(locked, 0) || locked < 0 {
			return 0, 0, fmt.Errorf("invalid Bitget %s locked balance %q", quoteAsset, balance.Locked)
		}
		frozen, parseErr := strconv.ParseFloat(balance.Frozen, 64)
		if parseErr != nil || math.IsNaN(frozen) || math.IsInf(frozen, 0) || frozen < 0 {
			return 0, 0, fmt.Errorf("invalid Bitget %s frozen balance %q", quoteAsset, balance.Frozen)
		}
		total, available = free+locked+frozen, free
		if math.IsInf(total, 0) {
			return 0, 0, fmt.Errorf("Bitget %s balance overflow", quoteAsset)
		}
	}
	return total, available, nil
}

func (b *BitgetSpotAdapter) SpotInventoryQty(ctx context.Context) (float64, error) {
	if b == nil || b.client == nil || strings.TrimSpace(b.baseAsset) == "" {
		return 0, fmt.Errorf("Bitget spot inventory requires a configured base asset")
	}
	resp, err := b.client.DoRequest(ctx, "GET", "/api/v2/spot/account/assets", nil)
	if err != nil {
		return 0, fmt.Errorf("query Bitget spot inventory: %w", err)
	}
	var balances []bitgetSpotAccountAsset
	if err := json.Unmarshal(resp.Data, &balances); err != nil {
		return 0, fmt.Errorf("decode Bitget spot inventory: %w", err)
	}
	total, _, err := summarizeBitgetSpotQuoteBalance(balances, b.baseAsset)
	return total, err
}

// GetPositions 現貨“持倉”由基础资產餘額構成
func (b *BitgetSpotAdapter) GetPositions(ctx context.Context, symbol string) ([]*Position, error) {
	resp, err := b.client.DoRequest(ctx, "GET", "/api/v2/spot/account/assets", nil)
	if err != nil {
		return nil, err
	}
	var list []bitgetSpotAccountAsset
	if err := json.Unmarshal(resp.Data, &list); err != nil {
		return nil, err
	}
	base := b.baseAsset
	if base == "" {
		base = strings.TrimSuffix(symbol, "USDT")
	}
	size, _, err := summarizeBitgetSpotQuoteBalance(list, base)
	if err != nil {
		return nil, err
	}
	if size <= 0 {
		return []*Position{}, nil
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

// StartOrderStream 現貨私有 orders（instType=SPOT）
func (b *BitgetSpotAdapter) StartOrderStream(ctx context.Context, callback func(interface{})) error {
	if b.spotOrderWS != nil {
		b.spotOrderWS.Stop()
	}
	b.spotOrderWS = NewWebSocketManagerForSpotOrders(
		b.apiKey, b.secretKey, b.passphrase, b.testnet,
	)
	return b.spotOrderWS.StartSpotOrdersOnly(ctx, callback)
}

// StopOrderStream 停止現貨訂單流
func (b *BitgetSpotAdapter) StopOrderStream() error {
	if b.spotOrderWS != nil {
		b.spotOrderWS.Stop()
		b.spotOrderWS = nil
	}
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

// StartKlineStream 現貨公共 K 線（instType=SPOT）
func (b *BitgetSpotAdapter) StartKlineStream(ctx context.Context, symbols []string, interval string, callback func(interface{})) error {
	if b.spotKlineWS != nil {
		b.spotKlineWS.Stop()
	}
	b.spotKlineWS = NewSpotKlineWebSocketManager(b.testnet)
	return b.spotKlineWS.Start(ctx, symbols, interval, callback)
}

// StopKlineStream 停止 K 線流
func (b *BitgetSpotAdapter) StopKlineStream() error {
	if b.spotKlineWS != nil {
		b.spotKlineWS.Stop()
		b.spotKlineWS = nil
	}
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

// BitgetSpotFill 現貨成交明細
type BitgetSpotFill struct {
	OrderId   string `json:"orderId"`
	TradeId   string `json:"tradeId"`
	Symbol    string `json:"symbol"`
	Side      string `json:"side"`
	PriceAvg  string `json:"priceAvg"`
	Size      string `json:"size"`
	FeeDetail []struct {
		Fee     string `json:"fee"`
		FeeCoin string `json:"feeCoin"`
	} `json:"feeDetail"`
	CTime string `json:"cTime"`
}

// GetOrderFills 查詢成交記錄（/api/v2/spot/trade/fills）
func (b *BitgetSpotAdapter) GetOrderFills(ctx context.Context, symbol string, orderID int64) ([]BitgetSpotFill, error) {
	if b == nil || b.client == nil || strings.TrimSpace(symbol) == "" || orderID < 0 || !strings.EqualFold(strings.TrimSpace(symbol), b.symbol) {
		return nil, fmt.Errorf("Bitget spot fill lookup requires the configured symbol and a non-negative order ID")
	}
	const pageSize = 100
	const maxPages = 1000
	path := fmt.Sprintf("/api/v2/spot/trade/fills?symbol=%s&limit=%d", url.QueryEscape(b.symbol), pageSize)
	if orderID != 0 {
		path += fmt.Sprintf("&orderId=%d", orderID)
	}
	allFills := make([]BitgetSpotFill, 0, pageSize)
	seenTradeIDs := make(map[string]struct{})
	for page := 0; page < maxPages; page++ {
		pagePath := path
		if len(allFills) > 0 {
			pagePath += "&idLessThan=" + url.QueryEscape(allFills[len(allFills)-1].TradeId)
		}
		resp, err := b.client.DoRequest(ctx, "GET", pagePath, nil)
		if err != nil {
			return nil, fmt.Errorf("query Bitget spot fill page %d: %w", page+1, err)
		}
		var rows []BitgetSpotFill
		if err := json.Unmarshal(resp.Data, &rows); err != nil {
			return nil, fmt.Errorf("解析 Bitget 現貨成交页 %d 失败: %w", page+1, err)
		}
		for _, row := range rows {
			if row.TradeId == "" || (orderID > 0 && row.OrderId != strconv.FormatInt(orderID, 10)) || !strings.EqualFold(row.Symbol, b.symbol) {
				return nil, fmt.Errorf("Bitget returned a fill with invalid identity for order %d", orderID)
			}
			if _, exists := seenTradeIDs[row.TradeId]; exists {
				return nil, fmt.Errorf("Bitget spot fill pagination repeated trade %s; completeness is unknown", row.TradeId)
			}
			seenTradeIDs[row.TradeId] = struct{}{}
			allFills = append(allFills, row)
		}
		if len(rows) < pageSize {
			return allFills, nil
		}
	}
	return nil, fmt.Errorf("Bitget spot order %d exceeded the fill pagination safety limit; completeness is unknown", orderID)
}
