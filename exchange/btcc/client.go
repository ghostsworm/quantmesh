package btcc

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"quantmesh/logger"
)

const (
	BTCCMainnetBaseURL = "https://api.btcc.com"           // BTCC 主网
	BTCCTestnetBaseURL = "https://testnet-api.btcc.com"   // BTCC 测试网
)

// BTCCClient BTCC 客户端
type BTCCClient struct {
	apiKey     string
	secretKey  string
	baseURL    string
	httpClient *http.Client
	isTestnet  bool
}

// NewBTCCClient 创建 BTCC 客户端
func NewBTCCClient(apiKey, secretKey string, isTestnet bool) *BTCCClient {
	baseURL := BTCCMainnetBaseURL
	if isTestnet {
		baseURL = BTCCTestnetBaseURL
	}

	return &BTCCClient{
		apiKey:     apiKey,
		secretKey:  secretKey,
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		isTestnet:  isTestnet,
	}
}

// signRequest BTCC 签名：HMAC-SHA256
func (c *BTCCClient) signRequest(method, path, timestamp, queryString, body string) string {
	message := timestamp + method + path
	if queryString != "" {
		message += "?" + queryString
	}
	if body != "" {
		message += body
	}
	
	h := hmac.New(sha256.New, []byte(c.secretKey))
	h.Write([]byte(message))
	return hex.EncodeToString(h.Sum(nil))
}

// sendRequest 发送请求
func (c *BTCCClient) sendRequest(ctx context.Context, method, path string, params url.Values, body interface{}, needSign bool) ([]byte, error) {
	queryString := ""
	if len(params) > 0 {
		queryString = params.Encode()
	}

	reqURL := c.baseURL + path
	if queryString != "" {
		reqURL += "?" + queryString
	}

	var reqBody []byte
	var err error
	bodyStr := ""
	if body != nil {
		reqBody, err = json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal body error: %w", err)
		}
		bodyStr = string(reqBody)
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("create request error: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	if needSign {
		timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
		signature := c.signRequest(method, path, timestamp, queryString, bodyStr)
		
		req.Header.Set("BTCC-API-KEY", c.apiKey)
		req.Header.Set("BTCC-TIMESTAMP", timestamp)
		req.Header.Set("BTCC-SIGNATURE", signature)
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

// GetSymbol 获取交易对信息
func (c *BTCCClient) GetSymbol(ctx context.Context, symbol string) (*Symbol, error) {
	path := "/api/v1/market/symbol"
	params := url.Values{}
	params.Set("symbol", symbol)

	respBody, err := c.sendRequest(ctx, http.MethodGet, path, params, nil, false)
	if err != nil {
		return nil, err
	}

	var apiResp APIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	if apiResp.Code != 0 {
		return nil, fmt.Errorf("API error: code=%d, msg=%s", apiResp.Code, apiResp.Message)
	}

	var symbolInfo Symbol
	dataBytes, _ := json.Marshal(apiResp.Data)
	if err := json.Unmarshal(dataBytes, &symbolInfo); err != nil {
		return nil, fmt.Errorf("unmarshal data error: %w", err)
	}

	return &symbolInfo, nil
}

// PlaceOrder 下单
func (c *BTCCClient) PlaceOrder(ctx context.Context, req *OrderRequest) (*Order, error) {
	path := "/api/v1/order/place"
	params := url.Values{}

	respBody, err := c.sendRequest(ctx, http.MethodPost, path, params, req, true)
	if err != nil {
		return nil, err
	}

	var apiResp APIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	if apiResp.Code != 0 {
		return nil, fmt.Errorf("API error: code=%d, msg=%s", apiResp.Code, apiResp.Message)
	}

	var order Order
	dataBytes, _ := json.Marshal(apiResp.Data)
	if err := json.Unmarshal(dataBytes, &order); err != nil {
		return nil, fmt.Errorf("unmarshal data error: %w", err)
	}

	logger.Info("BTCC order placed: %d", order.OrderID)
	return &order, nil
}

// CancelOrder 取消订单
func (c *BTCCClient) CancelOrder(ctx context.Context, symbol string, orderID int64) error {
	path := "/api/v1/order/cancel"
	params := url.Values{}

	body := map[string]interface{}{
		"symbol":  symbol,
		"orderId": orderID,
	}

	respBody, err := c.sendRequest(ctx, http.MethodPost, path, params, body, true)
	if err != nil {
		return err
	}

	var apiResp APIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return fmt.Errorf("unmarshal error: %w", err)
	}

	if apiResp.Code != 0 {
		return fmt.Errorf("API error: code=%d, msg=%s", apiResp.Code, apiResp.Message)
	}

	logger.Info("BTCC order cancelled: %d", orderID)
	return nil
}

// GetOrder 查询订单
func (c *BTCCClient) GetOrder(ctx context.Context, symbol string, orderID int64) (*Order, error) {
	path := "/api/v1/order/query"
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("orderId", strconv.FormatInt(orderID, 10))

	respBody, err := c.sendRequest(ctx, http.MethodGet, path, params, nil, true)
	if err != nil {
		return nil, err
	}

	var apiResp APIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	if apiResp.Code != 0 {
		return nil, fmt.Errorf("API error: code=%d, msg=%s", apiResp.Code, apiResp.Message)
	}

	var order Order
	dataBytes, _ := json.Marshal(apiResp.Data)
	if err := json.Unmarshal(dataBytes, &order); err != nil {
		return nil, fmt.Errorf("unmarshal data error: %w", err)
	}

	return &order, nil
}

// GetOpenOrders 获取活跃订单
func (c *BTCCClient) GetOpenOrders(ctx context.Context, symbol string) ([]Order, error) {
	path := "/api/v1/order/open"
	params := url.Values{}
	if symbol != "" {
		params.Set("symbol", symbol)
	}

	respBody, err := c.sendRequest(ctx, http.MethodGet, path, params, nil, true)
	if err != nil {
		return nil, err
	}

	var apiResp APIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	if apiResp.Code != 0 {
		return nil, fmt.Errorf("API error: code=%d, msg=%s", apiResp.Code, apiResp.Message)
	}

	var orders []Order
	dataBytes, _ := json.Marshal(apiResp.Data)
	if err := json.Unmarshal(dataBytes, &orders); err != nil {
		return nil, fmt.Errorf("unmarshal data error: %w", err)
	}

	return orders, nil
}

// GetBalance 获取账户余额
func (c *BTCCClient) GetBalance(ctx context.Context) (*Balance, error) {
	path := "/api/v1/account/balance"
	params := url.Values{}

	respBody, err := c.sendRequest(ctx, http.MethodGet, path, params, nil, true)
	if err != nil {
		return nil, err
	}

	var apiResp APIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	if apiResp.Code != 0 {
		return nil, fmt.Errorf("API error: code=%d, msg=%s", apiResp.Code, apiResp.Message)
	}

	var balance Balance
	dataBytes, _ := json.Marshal(apiResp.Data)
	if err := json.Unmarshal(dataBytes, &balance); err != nil {
		return nil, fmt.Errorf("unmarshal data error: %w", err)
	}

	return &balance, nil
}

// GetPositions 获取持仓
func (c *BTCCClient) GetPositions(ctx context.Context, symbol string) ([]Position, error) {
	path := "/api/v1/position/list"
	params := url.Values{}
	if symbol != "" {
		params.Set("symbol", symbol)
	}

	respBody, err := c.sendRequest(ctx, http.MethodGet, path, params, nil, true)
	if err != nil {
		return nil, err
	}

	var apiResp APIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	if apiResp.Code != 0 {
		return nil, fmt.Errorf("API error: code=%d, msg=%s", apiResp.Code, apiResp.Message)
	}

	var positions []Position
	dataBytes, _ := json.Marshal(apiResp.Data)
	if err := json.Unmarshal(dataBytes, &positions); err != nil {
		return nil, fmt.Errorf("unmarshal data error: %w", err)
	}

	return positions, nil
}

// GetTicker 获取最新价格
func (c *BTCCClient) GetTicker(ctx context.Context, symbol string) (*Ticker, error) {
	path := "/api/v1/market/ticker"
	params := url.Values{}
	params.Set("symbol", symbol)

	respBody, err := c.sendRequest(ctx, http.MethodGet, path, params, nil, false)
	if err != nil {
		return nil, err
	}

	var apiResp APIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	if apiResp.Code != 0 {
		return nil, fmt.Errorf("API error: code=%d, msg=%s", apiResp.Code, apiResp.Message)
	}

	var ticker Ticker
	dataBytes, _ := json.Marshal(apiResp.Data)
	if err := json.Unmarshal(dataBytes, &ticker); err != nil {
		return nil, fmt.Errorf("unmarshal data error: %w", err)
	}

	return &ticker, nil
}

// GetKlines 获取 K线数据
func (c *BTCCClient) GetKlines(ctx context.Context, symbol, interval string, limit int) ([]Kline, error) {
	path := "/api/v1/market/kline"
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("interval", interval)
	if limit > 0 {
		params.Set("limit", strconv.Itoa(limit))
	}

	respBody, err := c.sendRequest(ctx, http.MethodGet, path, params, nil, false)
	if err != nil {
		return nil, err
	}

	var apiResp APIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	if apiResp.Code != 0 {
		return nil, fmt.Errorf("API error: code=%d, msg=%s", apiResp.Code, apiResp.Message)
	}

	var klines []Kline
	dataBytes, _ := json.Marshal(apiResp.Data)
	if err := json.Unmarshal(dataBytes, &klines); err != nil {
		return nil, fmt.Errorf("unmarshal data error: %w", err)
	}

	return klines, nil
}

// 数据结构定义

type APIResponse struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
}

type Symbol struct {
	Symbol            string  `json:"symbol"`
	BaseCurrency      string  `json:"baseCurrency"`
	QuoteCurrency     string  `json:"quoteCurrency"`
	PricePrecision    int     `json:"pricePrecision"`
	QuantityPrecision int     `json:"quantityPrecision"`
	MinQuantity       float64 `json:"minQuantity"`
	MaxQuantity       float64 `json:"maxQuantity"`
	MinNotional       float64 `json:"minNotional"`
}

type OrderRequest struct {
	Symbol       string  `json:"symbol"`
	Side         string  `json:"side"`         // BUY, SELL
	Type         string  `json:"type"`         // LIMIT, MARKET
	Quantity     float64 `json:"quantity"`
	Price        float64 `json:"price,omitempty"`
	ClientOrderID string  `json:"clientOrderId,omitempty"`
}

type Order struct {
	OrderID       int64   `json:"orderId"`
	ClientOrderID string  `json:"clientOrderId"`
	Symbol        string  `json:"symbol"`
	Side          string  `json:"side"`
	Type          string  `json:"type"`
	Price         float64 `json:"price"`
	Quantity      float64 `json:"quantity"`
	ExecutedQty   float64 `json:"executedQty"`
	Status        string  `json:"status"`
	CreateTime    int64   `json:"createTime"`
	UpdateTime    int64   `json:"updateTime"`
}

type Balance struct {
	TotalBalance     float64 `json:"totalBalance"`
	AvailableBalance float64 `json:"availableBalance"`
	FrozenBalance    float64 `json:"frozenBalance"`
}

type Position struct {
	Symbol        string  `json:"symbol"`
	Side          string  `json:"side"`
	Size          float64 `json:"size"`
	EntryPrice    float64 `json:"entryPrice"`
	MarkPrice     float64 `json:"markPrice"`
	UnrealizedPNL float64 `json:"unrealizedPnl"`
	Leverage      int     `json:"leverage"`
}

type Ticker struct {
	Symbol string  `json:"symbol"`
	Price  float64 `json:"price"`
	Time   int64   `json:"time"`
}

type Kline struct {
	Time   int64   `json:"time"`
	Open   float64 `json:"open"`
	High   float64 `json:"high"`
	Low    float64 `json:"low"`
	Close  float64 `json:"close"`
	Volume float64 `json:"volume"`
}

