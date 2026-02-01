package bitfinex

import (
	"context"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"quantmesh/logger"
)

const (
	BitfinexBaseURL = "https://api.bitfinex.com" // Bitfinex API v2
)

// BitfinexClient Bitfinex 客户端
type BitfinexClient struct {
	apiKey     string
	secretKey  string
	httpClient *http.Client
}

// NewBitfinexClient 创建 Bitfinex 客户端
func NewBitfinexClient(apiKey, secretKey string) *BitfinexClient {
	return &BitfinexClient{
		apiKey:     apiKey,
		secretKey:  secretKey,
		httpClient: &http.Client{Timeout: 10 * time.Second},
	}
}

// signRequest Bitfinex v2 签名：HMAC-SHA384
func (c *BitfinexClient) signRequest(path, nonce, body string) string {
	// 签名字符串：/api + path + nonce + body
	payload := "/api" + path + nonce + body

	h := hmac.New(sha512.New384, []byte(c.secretKey))
	h.Write([]byte(payload))
	return hex.EncodeToString(h.Sum(nil))
}

// sendAuthRequest 发送认证请求
func (c *BitfinexClient) sendAuthRequest(ctx context.Context, method, path string, body interface{}) ([]byte, error) {
	reqURL := BitfinexBaseURL + path

	var bodyBytes []byte
	var err error
	bodyStr := ""

	if body != nil {
		bodyBytes, err = json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal body error: %w", err)
		}
		bodyStr = string(bodyBytes)
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, strings.NewReader(bodyStr))
	if err != nil {
		return nil, fmt.Errorf("create request error: %w", err)
	}

	// Bitfinex v2 认证头
	nonce := strconv.FormatInt(time.Now().UnixMilli(), 10)
	signature := c.signRequest(path, nonce, bodyStr)

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("bfx-nonce", nonce)
	req.Header.Set("bfx-apikey", c.apiKey)
	req.Header.Set("bfx-signature", signature)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request error: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response error: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	// 检查错误响应：["error", CODE, "ERROR_MESSAGE"]
	var errorCheck []interface{}
	if err := json.Unmarshal(respBody, &errorCheck); err == nil {
		if len(errorCheck) > 0 {
			if errType, ok := errorCheck[0].(string); ok && errType == "error" {
				return nil, fmt.Errorf("API error: %v", errorCheck)
			}
		}
	}

	return respBody, nil
}

// sendPublicRequest 发送公开请求
func (c *BitfinexClient) sendPublicRequest(ctx context.Context, path string) ([]byte, error) {
	reqURL := BitfinexBaseURL + path

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create request error: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("send request error: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response error: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

// GetTradingPairs 获取交易对列表
func (c *BitfinexClient) GetTradingPairs(ctx context.Context) ([]string, error) {
	path := "/v2/conf/pub:list:pair:exchange"
	respBody, err := c.sendPublicRequest(ctx, path)
	if err != nil {
		return nil, err
	}

	// 响应格式：[0, ["BTCUSD", "ETHUSD", ...]]
	var result []interface{}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	if len(result) < 2 {
		return nil, fmt.Errorf("invalid response format")
	}

	pairsInterface, ok := result[1].([]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid pairs format")
	}

	pairs := make([]string, 0, len(pairsInterface))
	for _, p := range pairsInterface {
		if pair, ok := p.(string); ok {
			pairs = append(pairs, pair)
		}
	}

	return pairs, nil
}

// PlaceOrder 下单
// Bitfinex 订单响应：[MTS, TYPE, MESSAGE_ID, null, [[ORDER_DATA]], null, "SUCCESS", "Order placed"]
func (c *BitfinexClient) PlaceOrder(ctx context.Context, req *OrderRequest) (*OrderResponse, error) {
	path := "/v2/auth/w/order/submit"

	// Bitfinex 订单类型：LIMIT, MARKET, STOP, STOP LIMIT, etc.
	orderType := "LIMIT"
	if strings.ToUpper(req.Type) == "MARKET" {
		orderType = "MARKET"
	}

	// 金额：正数=买入，负数=卖出
	amount := req.Quantity
	if strings.ToUpper(req.Side) == "SELL" {
		amount = -amount
	}

	body := map[string]interface{}{
		"type":   orderType,
		"symbol": "t" + req.Symbol, // 添加 t 前缀表示交易对
		"amount": fmt.Sprintf("%.8f", amount),
	}

	if orderType == "LIMIT" {
		body["price"] = fmt.Sprintf("%.2f", req.Price)
	}

	if req.ClientOrderID != "" {
		body["cid"] = req.ClientOrderID
	}

	respBody, err := c.sendAuthRequest(ctx, http.MethodPost, path, body)
	if err != nil {
		return nil, err
	}

	// 解析响应：[MTS, TYPE, MESSAGE_ID, null, [[ORDER_ID, GID, CID, ...]], ...]
	var resp []interface{}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal response error: %w", err)
	}

	if len(resp) < 5 {
		return nil, fmt.Errorf("invalid response format: %v", resp)
	}

	// 提取订单数据
	ordersData, ok := resp[4].([]interface{})
	if !ok || len(ordersData) == 0 {
		return nil, fmt.Errorf("no order data in response")
	}

	orderArray, ok := ordersData[0].([]interface{})
	if !ok || len(orderArray) == 0 {
		return nil, fmt.Errorf("invalid order array")
	}

	// ORDER_ID 在索引 0
	orderID := fmt.Sprintf("%v", orderArray[0])

	logger.Info("Bitfinex order placed: %s", orderID)
	return &OrderResponse{OrderID: orderID}, nil
}

// CancelOrder 取消订单
func (c *BitfinexClient) CancelOrder(ctx context.Context, orderID string) error {
	path := "/v2/auth/w/order/cancel"

	orderIDInt, err := strconv.ParseInt(orderID, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid order ID: %w", err)
	}

	body := map[string]interface{}{
		"id": orderIDInt,
	}

	respBody, err := c.sendAuthRequest(ctx, http.MethodPost, path, body)
	if err != nil {
		return err
	}

	logger.Info("Bitfinex order cancelled: %s, response: %s", orderID, string(respBody))
	return nil
}

// GetActiveOrders 获取活跃订单
// 响应格式：[[ORDER_ID, GID, CID, SYMBOL, MTS_CREATE, MTS_UPDATE, AMOUNT, AMOUNT_ORIG, TYPE, ...], ...]
func (c *BitfinexClient) GetActiveOrders(ctx context.Context, symbol string) ([]OrderInfo, error) {
	path := "/v2/auth/r/orders"
	if symbol != "" {
		path += "/t" + symbol
	}

	respBody, err := c.sendAuthRequest(ctx, http.MethodPost, path, nil)
	if err != nil {
		return nil, err
	}

	var ordersArray [][]interface{}
	if err := json.Unmarshal(respBody, &ordersArray); err != nil {
		return nil, fmt.Errorf("unmarshal orders error: %w", err)
	}

	orders := make([]OrderInfo, 0, len(ordersArray))
	for _, orderData := range ordersArray {
		order := parseOrderArray(orderData)
		orders = append(orders, order)
	}

	return orders, nil
}

// GetWallets 获取钱包余额
// 响应格式：[[WALLET_TYPE, CURRENCY, BALANCE, UNSETTLED_INTEREST, BALANCE_AVAILABLE], ...]
func (c *BitfinexClient) GetWallets(ctx context.Context) ([]WalletInfo, error) {
	path := "/v2/auth/r/wallets"

	respBody, err := c.sendAuthRequest(ctx, http.MethodPost, path, nil)
	if err != nil {
		return nil, err
	}

	var walletsArray [][]interface{}
	if err := json.Unmarshal(respBody, &walletsArray); err != nil {
		return nil, fmt.Errorf("unmarshal wallets error: %w", err)
	}

	wallets := make([]WalletInfo, 0, len(walletsArray))
	for _, walletData := range walletsArray {
		if len(walletData) >= 5 {
			wallet := WalletInfo{
				Type:              fmt.Sprintf("%v", walletData[0]),
				Currency:          fmt.Sprintf("%v", walletData[1]),
				Balance:           parseFloat64(walletData[2]),
				UnsettledInterest: parseFloat64(walletData[3]),
				BalanceAvailable:  parseFloat64(walletData[4]),
			}
			wallets = append(wallets, wallet)
		}
	}

	return wallets, nil
}

// GetPositions 获取持仓
// 响应格式：[[SYMBOL, STATUS, AMOUNT, BASE_PRICE, MARGIN_FUNDING, MARGIN_FUNDING_TYPE, PL, PL_PERC, ...], ...]
func (c *BitfinexClient) GetPositions(ctx context.Context) ([]PositionInfo, error) {
	path := "/v2/auth/r/positions"

	respBody, err := c.sendAuthRequest(ctx, http.MethodPost, path, nil)
	if err != nil {
		return nil, err
	}

	var positionsArray [][]interface{}
	if err := json.Unmarshal(respBody, &positionsArray); err != nil {
		return nil, fmt.Errorf("unmarshal positions error: %w", err)
	}

	positions := make([]PositionInfo, 0, len(positionsArray))
	for _, posData := range positionsArray {
		if len(posData) >= 8 {
			position := PositionInfo{
				Symbol:    strings.TrimPrefix(fmt.Sprintf("%v", posData[0]), "t"),
				Status:    fmt.Sprintf("%v", posData[1]),
				Amount:    parseFloat64(posData[2]),
				BasePrice: parseFloat64(posData[3]),
				PL:        parseFloat64(posData[6]),
				PLPerc:    parseFloat64(posData[7]),
			}
			positions = append(positions, position)
		}
	}

	return positions, nil
}

// GetTicker 获取行情
// 响应格式：[BID, BID_SIZE, ASK, ASK_SIZE, DAILY_CHANGE, DAILY_CHANGE_RELATIVE, LAST_PRICE, VOLUME, HIGH, LOW]
func (c *BitfinexClient) GetTicker(ctx context.Context, symbol string) (*TickerInfo, error) {
	path := fmt.Sprintf("/v2/ticker/t%s", symbol)

	respBody, err := c.sendPublicRequest(ctx, path)
	if err != nil {
		return nil, err
	}

	var tickerArray []interface{}
	if err := json.Unmarshal(respBody, &tickerArray); err != nil {
		return nil, fmt.Errorf("unmarshal ticker error: %w", err)
	}

	if len(tickerArray) < 10 {
		return nil, fmt.Errorf("invalid ticker format")
	}

	ticker := &TickerInfo{
		Bid:       parseFloat64(tickerArray[0]),
		BidSize:   parseFloat64(tickerArray[1]),
		Ask:       parseFloat64(tickerArray[2]),
		AskSize:   parseFloat64(tickerArray[3]),
		LastPrice: parseFloat64(tickerArray[6]),
		Volume:    parseFloat64(tickerArray[7]),
		High:      parseFloat64(tickerArray[8]),
		Low:       parseFloat64(tickerArray[9]),
	}

	return ticker, nil
}

// GetCandles 获取K线数据
// 响应格式：[[MTS, OPEN, CLOSE, HIGH, LOW, VOLUME], ...]
func (c *BitfinexClient) GetCandles(ctx context.Context, symbol, timeframe string, limit int) ([]Candle, error) {
	path := fmt.Sprintf("/v2/candles/trade:%s:t%s/hist?limit=%d", timeframe, symbol, limit)

	respBody, err := c.sendPublicRequest(ctx, path)
	if err != nil {
		return nil, err
	}

	var candlesArray [][]interface{}
	if err := json.Unmarshal(respBody, &candlesArray); err != nil {
		return nil, fmt.Errorf("unmarshal candles error: %w", err)
	}

	candles := make([]Candle, 0, len(candlesArray))
	for _, candleData := range candlesArray {
		if len(candleData) >= 6 {
			candle := Candle{
				Timestamp: int64(parseFloat64(candleData[0])),
				Open:      parseFloat64(candleData[1]),
				Close:     parseFloat64(candleData[2]),
				High:      parseFloat64(candleData[3]),
				Low:       parseFloat64(candleData[4]),
				Volume:    parseFloat64(candleData[5]),
			}
			candles = append(candles, candle)
		}
	}

	return candles, nil
}

// parseOrderArray 解析订单数组
// 格式：[ID, GID, CID, SYMBOL, MTS_CREATE, MTS_UPDATE, AMOUNT, AMOUNT_ORIG, TYPE, TYPE_PREV, ...]
func parseOrderArray(data []interface{}) OrderInfo {
	if len(data) < 10 {
		return OrderInfo{}
	}

	return OrderInfo{
		ID:         fmt.Sprintf("%v", data[0]),
		GID:        fmt.Sprintf("%v", data[1]),
		CID:        fmt.Sprintf("%v", data[2]),
		Symbol:     strings.TrimPrefix(fmt.Sprintf("%v", data[3]), "t"),
		MTSCreate:  int64(parseFloat64(data[4])),
		MTSUpdate:  int64(parseFloat64(data[5])),
		Amount:     parseFloat64(data[6]),
		AmountOrig: parseFloat64(data[7]),
		Type:       fmt.Sprintf("%v", data[8]),
		TypePrev:   fmt.Sprintf("%v", data[9]),
	}
}

// parseFloat64 安全解析 float64
func parseFloat64(v interface{}) float64 {
	switch val := v.(type) {
	case float64:
		return val
	case string:
		f, _ := strconv.ParseFloat(val, 64)
		return f
	case int:
		return float64(val)
	case int64:
		return float64(val)
	default:
		return 0
	}
}

// 数据结构定义

type OrderRequest struct {
	Symbol        string
	Side          string // BUY/SELL
	Type          string // LIMIT/MARKET
	Price         float64
	Quantity      float64
	ClientOrderID string
}

type OrderResponse struct {
	OrderID string
}

type OrderInfo struct {
	ID         string
	GID        string
	CID        string
	Symbol     string
	MTSCreate  int64
	MTSUpdate  int64
	Amount     float64 // 正数=买入，负数=卖出
	AmountOrig float64
	Type       string
	TypePrev   string
}

type WalletInfo struct {
	Type              string
	Currency          string
	Balance           float64
	UnsettledInterest float64
	BalanceAvailable  float64
}

type PositionInfo struct {
	Symbol    string
	Status    string
	Amount    float64 // 正数=多仓，负数=空仓
	BasePrice float64
	PL        float64
	PLPerc    float64
}

type TickerInfo struct {
	Bid       float64
	BidSize   float64
	Ask       float64
	AskSize   float64
	LastPrice float64
	Volume    float64
	High      float64
	Low       float64
}

type Candle struct {
	Timestamp int64
	Open      float64
	Close     float64
	High      float64
	Low       float64
	Volume    float64
}
