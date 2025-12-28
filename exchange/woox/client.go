package woox

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
	WOOXMainnetBaseURL = "https://api.woo.org"           // WOO X 主网
	WOOXTestnetBaseURL = "https://api.staging.woo.org"   // WOO X 测试网
)

// WOOXClient WOO X 客户端
type WOOXClient struct {
	apiKey     string
	secretKey  string
	baseURL    string
	httpClient *http.Client
	isTestnet  bool
}

// NewWOOXClient 创建 WOO X 客户端
func NewWOOXClient(apiKey, secretKey string, isTestnet bool) *WOOXClient {
	baseURL := WOOXMainnetBaseURL
	if isTestnet {
		baseURL = WOOXTestnetBaseURL
	}

	return &WOOXClient{
		apiKey:     apiKey,
		secretKey:  secretKey,
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		isTestnet:  isTestnet,
	}
}

// signRequest WOO X 签名：HMAC-SHA256
// 签名字符串：timestamp + method + requestPath + body
func (c *WOOXClient) signRequest(timestamp, method, requestPath, body string) string {
	message := timestamp + method + requestPath + body
	h := hmac.New(sha256.New, []byte(c.secretKey))
	h.Write([]byte(message))
	return hex.EncodeToString(h.Sum(nil))
}

// sendRequest 发送请求
func (c *WOOXClient) sendRequest(ctx context.Context, method, path string, params url.Values, body interface{}, needSign bool) ([]byte, error) {
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
		signature := c.signRequest(timestamp, method, path+queryString, bodyStr)
		
		req.Header.Set("x-api-key", c.apiKey)
		req.Header.Set("x-api-signature", signature)
		req.Header.Set("x-api-timestamp", timestamp)
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
		var apiResp APIResponse
		if err := json.Unmarshal(respBody, &apiResp); err == nil && !apiResp.Success {
			return nil, fmt.Errorf("API error: code=%d, msg=%s", apiResp.Code, apiResp.Message)
		}
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

// GetSymbol 获取交易对信息
func (c *WOOXClient) GetSymbol(ctx context.Context, symbol string) (*Symbol, error) {
	path := "/v3/public/info/" + symbol
	params := url.Values{}

	respBody, err := c.sendRequest(ctx, http.MethodGet, path, params, nil, false)
	if err != nil {
		return nil, err
	}

	var apiResp APIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	if !apiResp.Success {
		return nil, fmt.Errorf("API error: %s", apiResp.Message)
	}

	var symbolInfo Symbol
	dataBytes, _ := json.Marshal(apiResp.Data)
	if err := json.Unmarshal(dataBytes, &symbolInfo); err != nil {
		return nil, fmt.Errorf("unmarshal data error: %w", err)
	}

	return &symbolInfo, nil
}

// PlaceOrder 下单
func (c *WOOXClient) PlaceOrder(ctx context.Context, req *OrderRequest) (*Order, error) {
	path := "/v3/order"
	params := url.Values{}

	respBody, err := c.sendRequest(ctx, http.MethodPost, path, params, req, true)
	if err != nil {
		return nil, err
	}

	var apiResp APIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	if !apiResp.Success {
		return nil, fmt.Errorf("API error: %s", apiResp.Message)
	}

	var order Order
	dataBytes, _ := json.Marshal(apiResp.Data)
	if err := json.Unmarshal(dataBytes, &order); err != nil {
		return nil, fmt.Errorf("unmarshal data error: %w", err)
	}

	logger.Info("WOO X order placed: %d", order.OrderID)
	return &order, nil
}

// CancelOrder 取消订单
func (c *WOOXClient) CancelOrder(ctx context.Context, symbol string, orderID int64) error {
	path := "/v3/order"
	params := url.Values{}

	body := map[string]interface{}{
		"symbol":   symbol,
		"order_id": orderID,
	}

	respBody, err := c.sendRequest(ctx, http.MethodDelete, path, params, body, true)
	if err != nil {
		return err
	}

	var apiResp APIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return fmt.Errorf("unmarshal error: %w", err)
	}

	if !apiResp.Success {
		return fmt.Errorf("API error: %s", apiResp.Message)
	}

	logger.Info("WOO X order cancelled: %d", orderID)
	return nil
}

// GetOrder 查询订单
func (c *WOOXClient) GetOrder(ctx context.Context, orderID int64) (*Order, error) {
	path := fmt.Sprintf("/v3/order/%d", orderID)
	params := url.Values{}

	respBody, err := c.sendRequest(ctx, http.MethodGet, path, params, nil, true)
	if err != nil {
		return nil, err
	}

	var apiResp APIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	if !apiResp.Success {
		return nil, fmt.Errorf("API error: %s", apiResp.Message)
	}

	var order Order
	dataBytes, _ := json.Marshal(apiResp.Data)
	if err := json.Unmarshal(dataBytes, &order); err != nil {
		return nil, fmt.Errorf("unmarshal data error: %w", err)
	}

	return &order, nil
}

// GetOpenOrders 获取活跃订单
func (c *WOOXClient) GetOpenOrders(ctx context.Context, symbol string) ([]Order, error) {
	path := "/v3/orders"
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

	if !apiResp.Success {
		return nil, fmt.Errorf("API error: %s", apiResp.Message)
	}

	var ordersResp OrdersResponse
	dataBytes, _ := json.Marshal(apiResp.Data)
	if err := json.Unmarshal(dataBytes, &ordersResp); err != nil {
		return nil, fmt.Errorf("unmarshal data error: %w", err)
	}

	return ordersResp.Rows, nil
}

// GetPosition 获取持仓
func (c *WOOXClient) GetPosition(ctx context.Context, symbol string) (*Position, error) {
	path := "/v3/position/" + symbol
	params := url.Values{}

	respBody, err := c.sendRequest(ctx, http.MethodGet, path, params, nil, true)
	if err != nil {
		return nil, err
	}

	var apiResp APIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	if !apiResp.Success {
		return nil, fmt.Errorf("API error: %s", apiResp.Message)
	}

	var position Position
	dataBytes, _ := json.Marshal(apiResp.Data)
	if err := json.Unmarshal(dataBytes, &position); err != nil {
		return nil, fmt.Errorf("unmarshal data error: %w", err)
	}

	return &position, nil
}

// GetAccount 获取账户信息
func (c *WOOXClient) GetAccount(ctx context.Context) (*Account, error) {
	path := "/v3/accountinfo"
	params := url.Values{}

	respBody, err := c.sendRequest(ctx, http.MethodGet, path, params, nil, true)
	if err != nil {
		return nil, err
	}

	var apiResp APIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	if !apiResp.Success {
		return nil, fmt.Errorf("API error: %s", apiResp.Message)
	}

	var account Account
	dataBytes, _ := json.Marshal(apiResp.Data)
	if err := json.Unmarshal(dataBytes, &account); err != nil {
		return nil, fmt.Errorf("unmarshal data error: %w", err)
	}

	return &account, nil
}

// GetTrades 获取最新成交
func (c *WOOXClient) GetTrades(ctx context.Context, symbol string, limit int) ([]Trade, error) {
	path := "/v3/public/market_trades"
	params := url.Values{}
	params.Set("symbol", symbol)
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

	if !apiResp.Success {
		return nil, fmt.Errorf("API error: %s", apiResp.Message)
	}

	var tradesResp TradesResponse
	dataBytes, _ := json.Marshal(apiResp.Data)
	if err := json.Unmarshal(dataBytes, &tradesResp); err != nil {
		return nil, fmt.Errorf("unmarshal data error: %w", err)
	}

	return tradesResp.Rows, nil
}

// GetKlines 获取 K线数据
func (c *WOOXClient) GetKlines(ctx context.Context, symbol, interval string, limit int) ([]Kline, error) {
	path := "/v3/public/kline"
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("type", interval)
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

	if !apiResp.Success {
		return nil, fmt.Errorf("API error: %s", apiResp.Message)
	}

	var klinesResp KlinesResponse
	dataBytes, _ := json.Marshal(apiResp.Data)
	if err := json.Unmarshal(dataBytes, &klinesResp); err != nil {
		return nil, fmt.Errorf("unmarshal data error: %w", err)
	}

	return klinesResp.Rows, nil
}

// 数据结构定义

type APIResponse struct {
	Success bool        `json:"success"`
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
}

type Symbol struct {
	Symbol           string  `json:"symbol"`
	QuoteMin         float64 `json:"quote_min"`
	QuoteMax         float64 `json:"quote_max"`
	QuoteTick        float64 `json:"quote_tick"`
	BaseMin          float64 `json:"base_min"`
	BaseMax          float64 `json:"base_max"`
	BaseTick         float64 `json:"base_tick"`
	MinNotional      float64 `json:"min_notional"`
	PriceRange       float64 `json:"price_range"`
	PrecisionPrice   int     `json:"precision_price"`
	PrecisionQuantity int    `json:"precision_quantity"`
}

type OrderRequest struct {
	Symbol       string  `json:"symbol"`
	ClientOrderID string `json:"client_order_id,omitempty"`
	Side         string  `json:"side"`         // BUY, SELL
	OrderType    string  `json:"order_type"`   // LIMIT, MARKET
	OrderPrice   float64 `json:"order_price,omitempty"`
	OrderQuantity float64 `json:"order_quantity"`
	ReduceOnly   bool    `json:"reduce_only,omitempty"`
}

type Order struct {
	OrderID       int64   `json:"order_id"`
	ClientOrderID string  `json:"client_order_id"`
	Symbol        string  `json:"symbol"`
	Side          string  `json:"side"`
	OrderType     string  `json:"order_type"`
	OrderPrice    float64 `json:"order_price"`
	OrderQuantity float64 `json:"order_quantity"`
	ExecutedQuantity float64 `json:"executed_quantity"`
	Status        string  `json:"status"`
	CreatedTime   int64   `json:"created_time"`
	UpdatedTime   int64   `json:"updated_time"`
}

type OrdersResponse struct {
	Rows []Order `json:"rows"`
}

type Position struct {
	Symbol        string  `json:"symbol"`
	Holding       float64 `json:"holding"`
	PendingLong   float64 `json:"pending_long"`
	PendingShort  float64 `json:"pending_short"`
	AverageOpenPrice float64 `json:"average_open_price"`
	MarkPrice     float64 `json:"mark_price"`
	UnrealizedPNL float64 `json:"unrealized_pnl"`
	Leverage      int     `json:"leverage"`
}

type Account struct {
	ApplicationID string  `json:"application_id"`
	Account       string  `json:"account"`
	TotalCollateral float64 `json:"total_collateral"`
	TotalAccountValue float64 `json:"total_account_value"`
	TotalVaultValue float64 `json:"total_vault_value"`
	TotalStakingValue float64 `json:"total_staking_value"`
}

type Trade struct {
	Symbol    string  `json:"symbol"`
	Side      string  `json:"side"`
	Price     float64 `json:"price"`
	Quantity  float64 `json:"quantity"`
	Timestamp int64   `json:"timestamp"`
}

type TradesResponse struct {
	Rows []Trade `json:"rows"`
}

type Kline struct {
	StartTimestamp int64   `json:"start_timestamp"`
	EndTimestamp   int64   `json:"end_timestamp"`
	Symbol         string  `json:"symbol"`
	Type           string  `json:"type"`
	Open           float64 `json:"open"`
	High           float64 `json:"high"`
	Low            float64 `json:"low"`
	Close          float64 `json:"close"`
	Volume         float64 `json:"volume"`
}

type KlinesResponse struct {
	Rows []Kline `json:"rows"`
}

