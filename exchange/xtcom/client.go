package xtcom

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
	XTMainnetBaseURL = "https://sapi.xt.com"    // XT.COM 主网
	XTTestnetBaseURL = "https://testnet.xt.com" // XT.COM 測試網
)

// XTClient XT.COM 客戶端
type XTClient struct {
	apiKey     string
	secretKey  string
	baseURL    string
	httpClient *http.Client
	isTestnet  bool
}

// NewXTClient 創建 XT.COM 客戶端
func NewXTClient(apiKey, secretKey string, isTestnet bool) *XTClient {
	baseURL := XTMainnetBaseURL
	if isTestnet {
		baseURL = XTTestnetBaseURL
	}

	return &XTClient{
		apiKey:     apiKey,
		secretKey:  secretKey,
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		isTestnet:  isTestnet,
	}
}

// signRequest XT.COM 签名：HMAC-SHA256
func (c *XTClient) signRequest(method, path, timestamp, body string) string {
	message := method + path + timestamp + body
	h := hmac.New(sha256.New, []byte(c.secretKey))
	h.Write([]byte(message))
	return hex.EncodeToString(h.Sum(nil))
}

// sendRequest 发送请求
func (c *XTClient) sendRequest(ctx context.Context, method, path string, params url.Values, body interface{}, needSign bool) ([]byte, error) {
	queryString := ""
	if len(params) > 0 {
		queryString = "?" + params.Encode()
	}

	reqURL := c.baseURL + path + queryString

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
		signature := c.signRequest(method, path+queryString, timestamp, bodyStr)

		req.Header.Set("xt-validate-appkey", c.apiKey)
		req.Header.Set("xt-validate-timestamp", timestamp)
		req.Header.Set("xt-validate-signature", signature)
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

// GetSymbol 獲取交易對信息
func (c *XTClient) GetSymbol(ctx context.Context, symbol string) (*Symbol, error) {
	path := "/v4/public/symbol"
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

	if apiResp.Rc != 0 {
		return nil, fmt.Errorf("API error: code=%d, msg=%s", apiResp.Rc, apiResp.Mc)
	}

	var symbolInfo Symbol
	dataBytes, _ := json.Marshal(apiResp.Result)
	if err := json.Unmarshal(dataBytes, &symbolInfo); err != nil {
		return nil, fmt.Errorf("unmarshal data error: %w", err)
	}

	return &symbolInfo, nil
}

// PlaceOrder 下單
func (c *XTClient) PlaceOrder(ctx context.Context, req *OrderRequest) (*Order, error) {
	path := "/v4/order"
	params := url.Values{}

	respBody, err := c.sendRequest(ctx, http.MethodPost, path, params, req, true)
	if err != nil {
		return nil, err
	}

	var apiResp APIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	if apiResp.Rc != 0 {
		return nil, fmt.Errorf("API error: code=%d, msg=%s", apiResp.Rc, apiResp.Mc)
	}

	var order Order
	dataBytes, _ := json.Marshal(apiResp.Result)
	if err := json.Unmarshal(dataBytes, &order); err != nil {
		return nil, fmt.Errorf("unmarshal data error: %w", err)
	}

	logger.Info("XT.COM order placed: %s", order.OrderID)
	return &order, nil
}

// CancelOrder 取消訂單
func (c *XTClient) CancelOrder(ctx context.Context, symbol, orderID string) error {
	path := "/v4/order"
	params := url.Values{}

	body := map[string]interface{}{
		"symbol":  symbol,
		"orderId": orderID,
	}

	respBody, err := c.sendRequest(ctx, http.MethodDelete, path, params, body, true)
	if err != nil {
		return err
	}

	var apiResp APIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return fmt.Errorf("unmarshal error: %w", err)
	}

	if apiResp.Rc != 0 {
		return fmt.Errorf("API error: code=%d, msg=%s", apiResp.Rc, apiResp.Mc)
	}

	logger.Info("XT.COM order cancelled: %s", orderID)
	return nil
}

// GetOrder 查詢訂單
func (c *XTClient) GetOrder(ctx context.Context, symbol, orderID string) (*Order, error) {
	path := "/v4/order"
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("orderId", orderID)

	respBody, err := c.sendRequest(ctx, http.MethodGet, path, params, nil, true)
	if err != nil {
		return nil, err
	}

	var apiResp APIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	if apiResp.Rc != 0 {
		return nil, fmt.Errorf("API error: code=%d, msg=%s", apiResp.Rc, apiResp.Mc)
	}

	var order Order
	dataBytes, _ := json.Marshal(apiResp.Result)
	if err := json.Unmarshal(dataBytes, &order); err != nil {
		return nil, fmt.Errorf("unmarshal data error: %w", err)
	}

	return &order, nil
}

// GetOpenOrders 獲取活跃订單
func (c *XTClient) GetOpenOrders(ctx context.Context, symbol string) ([]Order, error) {
	path := "/v4/open-order"
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

	if apiResp.Rc != 0 {
		return nil, fmt.Errorf("API error: code=%d, msg=%s", apiResp.Rc, apiResp.Mc)
	}

	var orders []Order
	dataBytes, _ := json.Marshal(apiResp.Result)
	if err := json.Unmarshal(dataBytes, &orders); err != nil {
		return nil, fmt.Errorf("unmarshal data error: %w", err)
	}

	return orders, nil
}

// GetBalance 獲取帳戶餘額
func (c *XTClient) GetBalance(ctx context.Context) (*Balance, error) {
	path := "/v4/balances"
	params := url.Values{}

	respBody, err := c.sendRequest(ctx, http.MethodGet, path, params, nil, true)
	if err != nil {
		return nil, err
	}

	var apiResp APIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	if apiResp.Rc != 0 {
		return nil, fmt.Errorf("API error: code=%d, msg=%s", apiResp.Rc, apiResp.Mc)
	}

	var balances []Balance
	dataBytes, _ := json.Marshal(apiResp.Result)
	if err := json.Unmarshal(dataBytes, &balances); err != nil {
		return nil, fmt.Errorf("unmarshal data error: %w", err)
	}

	// 回傳 USDT 餘額
	for _, balance := range balances {
		if balance.Currency == "usdt" {
			return &balance, nil
		}
	}

	return &Balance{Currency: "usdt", Available: "0", Frozen: "0"}, nil
}

// GetTicker 獲取最新價格
func (c *XTClient) GetTicker(ctx context.Context, symbol string) (*Ticker, error) {
	path := "/v4/public/ticker/price"
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

	if apiResp.Rc != 0 {
		return nil, fmt.Errorf("API error: code=%d, msg=%s", apiResp.Rc, apiResp.Mc)
	}

	var ticker Ticker
	dataBytes, _ := json.Marshal(apiResp.Result)
	if err := json.Unmarshal(dataBytes, &ticker); err != nil {
		return nil, fmt.Errorf("unmarshal data error: %w", err)
	}

	return &ticker, nil
}

// GetKlines 獲取 K線數據
func (c *XTClient) GetKlines(ctx context.Context, symbol, interval string, limit int) ([]Kline, error) {
	path := "/v4/public/kline"
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

	if apiResp.Rc != 0 {
		return nil, fmt.Errorf("API error: code=%d, msg=%s", apiResp.Rc, apiResp.Mc)
	}

	var klines []Kline
	dataBytes, _ := json.Marshal(apiResp.Result)
	if err := json.Unmarshal(dataBytes, &klines); err != nil {
		return nil, fmt.Errorf("unmarshal data error: %w", err)
	}

	return klines, nil
}

// 數據結構定义

type APIResponse struct {
	Rc     int         `json:"rc"`
	Mc     string      `json:"mc"`
	Result interface{} `json:"result"`
}

type Symbol struct {
	Symbol            string `json:"symbol"`
	BaseCurrency      string `json:"baseCurrency"`
	QuoteCurrency     string `json:"quoteCurrency"`
	PricePrecision    int    `json:"pricePrecision"`
	QuantityPrecision int    `json:"quantityPrecision"`
	MinQuantity       string `json:"minQuantity"`
	MaxQuantity       string `json:"maxQuantity"`
	MinAmount         string `json:"minAmount"`
	MaxAmount         string `json:"maxAmount"`
}

type OrderRequest struct {
	Symbol        string `json:"symbol"`
	Side          string `json:"side"` // BUY, SELL
	Type          string `json:"type"` // LIMIT, MARKET
	Quantity      string `json:"quantity"`
	Price         string `json:"price,omitempty"`
	ClientOrderID string `json:"clientOrderId,omitempty"`
}

type Order struct {
	OrderID       string `json:"orderId"`
	ClientOrderID string `json:"clientOrderId"`
	Symbol        string `json:"symbol"`
	Side          string `json:"side"`
	Type          string `json:"type"`
	Price         string `json:"price"`
	Quantity      string `json:"quantity"`
	ExecutedQty   string `json:"executedQty"`
	Status        string `json:"state"`
	CreateTime    int64  `json:"createTime"`
	UpdateTime    int64  `json:"updateTime"`
}

type Balance struct {
	Currency  string `json:"currency"`
	Available string `json:"available"`
	Frozen    string `json:"frozen"`
}

type Ticker struct {
	Symbol string `json:"s"`
	Price  string `json:"p"`
	Time   int64  `json:"t"`
}

type Kline struct {
	Time   int64  `json:"t"`
	Open   string `json:"o"`
	High   string `json:"h"`
	Low    string `json:"l"`
	Close  string `json:"c"`
	Volume string `json:"v"`
}
