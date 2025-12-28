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
)

const (
	BitfinexBaseURL = "https://api.bitfinex.com" // Bitfinex API
)

// BitfinexClient 结构体
type BitfinexClient struct {
	apiKey    string
	secretKey string
	httpClient *http.Client
}

// NewBitfinexClient 创建 Bitfinex 客户端实例
func NewBitfinexClient(apiKey, secretKey string) *BitfinexClient {
	return &BitfinexClient{
		apiKey:    apiKey,
		secretKey: secretKey,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// signRequest 对请求进行签名
func (c *BitfinexClient) signRequest(path, nonce, body string) string {
	// Bitfinex 签名算法：HMAC-SHA384
	// 签名字符串：/api + path + nonce + body
	signStr := "/api" + path + nonce + body

	h := hmac.New(sha512.New384, []byte(c.secretKey))
	h.Write([]byte(signStr))
	signature := hex.EncodeToString(h.Sum(nil))

	return signature
}

// sendRequest 发送 HTTP 请求
func (c *BitfinexClient) sendRequest(ctx context.Context, method, path string, body interface{}) ([]byte, error) {
	reqURL := fmt.Sprintf("%s%s", BitfinexBaseURL, path)

	var bodyBytes []byte
	var err error
	bodyStr := ""

	if body != nil {
		bodyBytes, err = json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request body error: %w", err)
		}
		bodyStr = string(bodyBytes)
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, strings.NewReader(bodyStr))
	if err != nil {
		return nil, fmt.Errorf("create request error: %w", err)
	}

	// 生成 nonce（毫秒时间戳）
	nonce := strconv.FormatInt(time.Now().UnixMilli(), 10)

	// 签名
	signature := c.signRequest(path, nonce, bodyStr)

	// 设置请求头
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
		return nil, fmt.Errorf("read response body error: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP error! Status: %s, Body: %s", resp.Status, string(respBody))
	}

	// 检查是否有错误响应
	var errorResp []interface{}
	if err := json.Unmarshal(respBody, &errorResp); err == nil {
		if len(errorResp) > 0 {
			if errStr, ok := errorResp[0].(string); ok && errStr == "error" {
				return nil, fmt.Errorf("API error! Message: %v", errorResp)
			}
		}
	}

	return respBody, nil
}

// GetExchangeInfo 获取合约信息
func (c *BitfinexClient) GetExchangeInfo(ctx context.Context) (*ExchangeInfo, error) {
	path := "/v2/conf/pub:list:pair:exchange"
	
	reqURL := fmt.Sprintf("%s%s", BitfinexBaseURL, path)
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
		return nil, fmt.Errorf("read response body error: %w", err)
	}

	var symbols [][]string
	if err := json.Unmarshal(respBody, &symbols); err != nil {
		return nil, fmt.Errorf("unmarshal exchange info error: %w", err)
	}

	exchangeInfo := &ExchangeInfo{
		Symbols: make(map[string]ContractInfo),
	}
	
	if len(symbols) > 0 {
		for _, symbol := range symbols[0] {
			exchangeInfo.Symbols[symbol] = ContractInfo{
				Symbol: symbol,
			}
		}
	}

	return exchangeInfo, nil
}

// PlaceOrder 下单
func (c *BitfinexClient) PlaceOrder(ctx context.Context, req *OrderRequest) (*OrderResponse, error) {
	path := "/v2/auth/w/order/submit"
	
	orderType := "LIMIT"
	if req.Type == "market" {
		orderType = "MARKET"
	}

	body := map[string]interface{}{
		"type":   orderType,
		"symbol": req.Symbol,
		"amount": fmt.Sprintf("%.8f", req.Quantity),
	}

	if req.Type == "limit" {
		body["price"] = fmt.Sprintf("%.2f", req.Price)
	}

	respBody, err := c.sendRequest(ctx, http.MethodPost, path, body)
	if err != nil {
		return nil, err
	}

	var resp []interface{}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal place order response error: %w", err)
	}

	if len(resp) < 4 {
		return nil, fmt.Errorf("invalid place order response: %v", resp)
	}

	// Bitfinex 返回格式：[MTS, TYPE, MESSAGE_ID, null, [[ORDER_ID, GID, CID, ...]]]
	if orders, ok := resp[4].([]interface{}); ok && len(orders) > 0 {
		if order, ok := orders[0].([]interface{}); ok && len(order) > 0 {
			orderID := fmt.Sprintf("%v", order[0])
			return &OrderResponse{OrderID: orderID}, nil
		}
	}

	return nil, fmt.Errorf("failed to parse order ID from response")
}

// CancelOrder 取消订单
func (c *BitfinexClient) CancelOrder(ctx context.Context, orderID string) (*CancelOrderResponse, error) {
	path := "/v2/auth/w/order/cancel"
	
	orderIDInt, _ := strconv.ParseInt(orderID, 10, 64)
	body := map[string]interface{}{
		"id": orderIDInt,
	}

	respBody, err := c.sendRequest(ctx, http.MethodPost, path, body)
	if err != nil {
		return nil, err
	}

	var resp []interface{}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal cancel order response error: %w", err)
	}

	return &CancelOrderResponse{OrderID: orderID}, nil
}

// GetOrderInfo 查询订单
func (c *BitfinexClient) GetOrderInfo(ctx context.Context, symbol string, orderID string) (*OrderInfo, error) {
	path := "/v2/auth/r/orders"
	respBody, err := c.sendRequest(ctx, http.MethodPost, path, nil)
	if err != nil {
		return nil, err
	}

	var orders [][]interface{}
	if err := json.Unmarshal(respBody, &orders); err != nil {
		return nil, fmt.Errorf("unmarshal get order info response error: %w", err)
	}

	// 查找指定订单
	for _, order := range orders {
		if len(order) > 0 {
			currentOrderID := fmt.Sprintf("%v", order[0])
			if currentOrderID == orderID {
				return parseOrderInfo(order), nil
			}
		}
	}

	return nil, fmt.Errorf("order %s not found", orderID)
}

// GetOpenOrders 查询未完成订单
func (c *BitfinexClient) GetOpenOrders(ctx context.Context, symbol string) ([]OrderInfo, error) {
	path := "/v2/auth/r/orders"
	if symbol != "" {
		path += "/" + symbol
	}

	respBody, err := c.sendRequest(ctx, http.MethodPost, path, nil)
	if err != nil {
		return nil, err
	}

	var orders [][]interface{}
	if err := json.Unmarshal(respBody, &orders); err != nil {
		return nil, fmt.Errorf("unmarshal get open orders response error: %w", err)
	}

	result := make([]OrderInfo, 0, len(orders))
	for _, order := range orders {
		result = append(result, *parseOrderInfo(order))
	}

	return result, nil
}

// GetAccountInfo 获取账户信息
func (c *BitfinexClient) GetAccountInfo(ctx context.Context) (*AccountInfo, error) {
	path := "/v2/auth/r/wallets"
	respBody, err := c.sendRequest(ctx, http.MethodPost, path, nil)
	if err != nil {
		return nil, err
	}

	var wallets [][]interface{}
	if err := json.Unmarshal(respBody, &wallets); err != nil {
		return nil, fmt.Errorf("unmarshal get account info response error: %w", err)
	}

	accountInfo := &AccountInfo{
		Wallets: make([]WalletInfo, 0, len(wallets)),
	}

	for _, wallet := range wallets {
		if len(wallet) >= 5 {
			walletType := fmt.Sprintf("%v", wallet[0])
			currency := fmt.Sprintf("%v", wallet[1])
			balance, _ := parseFloat(wallet[2])
			
			accountInfo.Wallets = append(accountInfo.Wallets, WalletInfo{
				Type:     walletType,
				Currency: currency,
				Balance:  balance,
			})

			if walletType == "exchange" {
				accountInfo.TotalBalance += balance
			}
		}
	}

	return accountInfo, nil
}

// GetPositionInfo 获取持仓信息
func (c *BitfinexClient) GetPositionInfo(ctx context.Context) ([]BitfinexPositionInfo, error) {
	path := "/v2/auth/r/positions"
	respBody, err := c.sendRequest(ctx, http.MethodPost, path, nil)
	if err != nil {
		return nil, err
	}

	var positions [][]interface{}
	if err := json.Unmarshal(respBody, &positions); err != nil {
		return nil, fmt.Errorf("unmarshal get position info response error: %w", err)
	}

	result := make([]BitfinexPositionInfo, 0, len(positions))
	for _, pos := range positions {
		if len(pos) >= 10 {
			symbol := fmt.Sprintf("%v", pos[0])
			amount, _ := parseFloat(pos[2])
			basePrice, _ := parseFloat(pos[3])
			pl, _ := parseFloat(pos[6])

			result = append(result, BitfinexPositionInfo{
				Symbol:    symbol,
				Amount:    amount,
				BasePrice: basePrice,
				PL:        pl,
			})
		}
	}

	return result, nil
}

// GetFundingRate 获取资金费率
func (c *BitfinexClient) GetFundingRate(ctx context.Context, symbol string) (float64, error) {
	// Bitfinex 没有直接的资金费率 API，返回 0
	return 0, nil
}

// GetHistoricalKlines 获取历史K线数据
func (c *BitfinexClient) GetHistoricalKlines(ctx context.Context, symbol string, timeframe string, limit int) ([]Candle, error) {
	// Bitfinex K线 API: /v2/candles/trade:TIMEFRAME:SYMBOL/hist
	path := fmt.Sprintf("/v2/candles/trade:%s:%s/hist?limit=%d", timeframe, symbol, limit)
	
	reqURL := fmt.Sprintf("%s%s", BitfinexBaseURL, path)
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
		return nil, fmt.Errorf("read response body error: %w", err)
	}

	var candles [][]interface{}
	if err := json.Unmarshal(respBody, &candles); err != nil {
		return nil, fmt.Errorf("unmarshal historical klines response error: %w", err)
	}

	result := make([]Candle, 0, len(candles))
	for _, candle := range candles {
		if len(candle) >= 6 {
			timestamp, _ := parseFloat(candle[0])
			open, _ := parseFloat(candle[1])
			close, _ := parseFloat(candle[2])
			high, _ := parseFloat(candle[3])
			low, _ := parseFloat(candle[4])
			volume, _ := parseFloat(candle[5])

			result = append(result, Candle{
				Time:   int64(timestamp),
				Open:   open,
				High:   high,
				Low:    low,
				Close:  close,
				Volume: volume,
			})
		}
	}

	return result, nil
}

// parseOrderInfo 解析订单信息
func parseOrderInfo(order []interface{}) *OrderInfo {
	if len(order) < 10 {
		return &OrderInfo{}
	}

	orderID := fmt.Sprintf("%v", order[0])
	symbol := fmt.Sprintf("%v", order[3])
	amount, _ := parseFloat(order[6])
	price, _ := parseFloat(order[16])
	status := fmt.Sprintf("%v", order[13])

	return &OrderInfo{
		OrderID: orderID,
		Symbol:  symbol,
		Amount:  amount,
		Price:   price,
		Status:  status,
	}
}

// parseFloat 解析浮点数
func parseFloat(v interface{}) (float64, error) {
	switch val := v.(type) {
	case float64:
		return val, nil
	case string:
		return strconv.ParseFloat(val, 64)
	case int:
		return float64(val), nil
	case int64:
		return float64(val), nil
	default:
		return 0, fmt.Errorf("unsupported type: %T", v)
	}
}

// ExchangeInfo 交易所信息
type ExchangeInfo struct {
	Symbols map[string]ContractInfo
}

// ContractInfo 合约信息
type ContractInfo struct {
	Symbol string
}

// OrderRequest 下单请求
type OrderRequest struct {
	ClientOrderID    string
	Symbol           string
	Side             string // "buy" or "sell"
	Type             string // "limit" or "market"
	Price            float64
	Quantity         float64
	PriceDecimals    int
	QuantityDecimals int
}

// OrderResponse 下单响应
type OrderResponse struct {
	OrderID string
}

// CancelOrderResponse 取消订单响应
type CancelOrderResponse struct {
	OrderID string
}

// OrderInfo 订单信息
type OrderInfo struct {
	OrderID string
	Symbol  string
	Amount  float64
	Price   float64
	Status  string
}

// AccountInfo 账户信息
type AccountInfo struct {
	TotalBalance float64
	Wallets      []WalletInfo
}

// WalletInfo 钱包信息
type WalletInfo struct {
	Type     string
	Currency string
	Balance  float64
}

// BitfinexPositionInfo 持仓信息
type BitfinexPositionInfo struct {
	Symbol    string
	Amount    float64
	BasePrice float64
	PL        float64
}

// Candle K线数据
type Candle struct {
	Time   int64
	Open   float64
	High   float64
	Low    float64
	Close  float64
	Volume float64
}

