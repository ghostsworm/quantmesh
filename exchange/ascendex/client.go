package ascendex

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
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
	AscendEXMainnetBaseURL = "https://ascendex.com"           // AscendEX 主网
	AscendEXTestnetBaseURL = "https://testnet.ascendex.com"   // AscendEX 测试网
)

// AscendEXClient AscendEX 客户端
type AscendEXClient struct {
	apiKey     string
	secretKey  string
	baseURL    string
	httpClient *http.Client
	isTestnet  bool
	accountGroup string // AscendEX 需要账户组
}

// NewAscendEXClient 创建 AscendEX 客户端
func NewAscendEXClient(apiKey, secretKey string, isTestnet bool) *AscendEXClient {
	baseURL := AscendEXMainnetBaseURL
	if isTestnet {
		baseURL = AscendEXTestnetBaseURL
	}

	return &AscendEXClient{
		apiKey:       apiKey,
		secretKey:    secretKey,
		baseURL:      baseURL,
		httpClient:   &http.Client{Timeout: 10 * time.Second},
		isTestnet:    isTestnet,
		accountGroup: "0", // 默认账户组
	}
}

// signRequest AscendEX 签名：HMAC-SHA256 + Base64
func (c *AscendEXClient) signRequest(timestamp, path, body string) string {
	message := timestamp + path + body
	h := hmac.New(sha256.New, []byte(c.secretKey))
	h.Write([]byte(message))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// sendRequest 发送请求
func (c *AscendEXClient) sendRequest(ctx context.Context, method, path string, params url.Values, body interface{}, needSign bool) ([]byte, error) {
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
		signature := c.signRequest(timestamp, path, bodyStr)
		
		req.Header.Set("x-auth-key", c.apiKey)
		req.Header.Set("x-auth-timestamp", timestamp)
		req.Header.Set("x-auth-signature", signature)
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

// GetAccountGroup 获取账户组信息
func (c *AscendEXClient) GetAccountGroup(ctx context.Context) error {
	path := "/api/pro/v1/info"
	params := url.Values{}

	respBody, err := c.sendRequest(ctx, http.MethodGet, path, params, nil, true)
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

	if data, ok := apiResp.Data.(map[string]interface{}); ok {
		if accountGroup, ok := data["accountGroup"].(float64); ok {
			c.accountGroup = strconv.Itoa(int(accountGroup))
		}
	}

	return nil
}

// GetSymbol 获取交易对信息
func (c *AscendEXClient) GetSymbol(ctx context.Context, symbol string) (*Symbol, error) {
	path := "/api/pro/v1/products"
	params := url.Values{}

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

	var symbols []Symbol
	dataBytes, _ := json.Marshal(apiResp.Data)
	if err := json.Unmarshal(dataBytes, &symbols); err != nil {
		return nil, fmt.Errorf("unmarshal data error: %w", err)
	}

	// 查找指定交易对
	for _, s := range symbols {
		if s.Symbol == symbol {
			return &s, nil
		}
	}

	return nil, fmt.Errorf("symbol not found: %s", symbol)
}

// PlaceOrder 下单
func (c *AscendEXClient) PlaceOrder(ctx context.Context, req *OrderRequest) (*Order, error) {
	path := fmt.Sprintf("/api/pro/v1/%s/order", c.accountGroup)
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

	var orderResp OrderResponse
	dataBytes, _ := json.Marshal(apiResp.Data)
	if err := json.Unmarshal(dataBytes, &orderResp); err != nil {
		return nil, fmt.Errorf("unmarshal data error: %w", err)
	}

	logger.Info("AscendEX order placed: %s", orderResp.OrderID)
	
	// 查询订单详情
	return c.GetOrder(ctx, orderResp.OrderID)
}

// CancelOrder 取消订单
func (c *AscendEXClient) CancelOrder(ctx context.Context, orderID string) error {
	path := fmt.Sprintf("/api/pro/v1/%s/order", c.accountGroup)
	params := url.Values{}

	body := map[string]interface{}{
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

	if apiResp.Code != 0 {
		return fmt.Errorf("API error: code=%d, msg=%s", apiResp.Code, apiResp.Message)
	}

	logger.Info("AscendEX order cancelled: %s", orderID)
	return nil
}

// GetOrder 查询订单
func (c *AscendEXClient) GetOrder(ctx context.Context, orderID string) (*Order, error) {
	path := fmt.Sprintf("/api/pro/v1/%s/order/status", c.accountGroup)
	params := url.Values{}
	params.Set("orderId", orderID)

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
func (c *AscendEXClient) GetOpenOrders(ctx context.Context, symbol string) ([]Order, error) {
	path := fmt.Sprintf("/api/pro/v1/%s/order/open", c.accountGroup)
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

	var orders []Order
	dataBytes, _ := json.Marshal(apiResp.Data)
	if err := json.Unmarshal(dataBytes, &orders); err != nil {
		return nil, fmt.Errorf("unmarshal data error: %w", err)
	}

	// 过滤指定交易对
	if symbol != "" {
		filtered := make([]Order, 0)
		for _, order := range orders {
			if order.Symbol == symbol {
				filtered = append(filtered, order)
			}
		}
		return filtered, nil
	}

	return orders, nil
}

// GetBalance 获取账户余额
func (c *AscendEXClient) GetBalance(ctx context.Context) (*Balance, error) {
	path := fmt.Sprintf("/api/pro/v1/%s/balance", c.accountGroup)
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

	var balances []Balance
	dataBytes, _ := json.Marshal(apiResp.Data)
	if err := json.Unmarshal(dataBytes, &balances); err != nil {
		return nil, fmt.Errorf("unmarshal data error: %w", err)
	}

	// 返回 USDT 余额
	for _, balance := range balances {
		if balance.Asset == "USDT" {
			return &balance, nil
		}
	}

	return &Balance{Asset: "USDT", TotalBalance: 0, AvailableBalance: 0}, nil
}

// GetTicker 获取最新价格
func (c *AscendEXClient) GetTicker(ctx context.Context, symbol string) (*Ticker, error) {
	path := "/api/pro/v1/ticker"
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
func (c *AscendEXClient) GetKlines(ctx context.Context, symbol, interval string, limit int) ([]Kline, error) {
	path := "/api/pro/v1/barhist"
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("interval", interval)
	if limit > 0 {
		params.Set("n", strconv.Itoa(limit))
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

	var klineResp KlineResponse
	dataBytes, _ := json.Marshal(apiResp.Data)
	if err := json.Unmarshal(dataBytes, &klineResp); err != nil {
		return nil, fmt.Errorf("unmarshal data error: %w", err)
	}

	return klineResp.Data, nil
}

// 数据结构定义

type APIResponse struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data"`
}

type Symbol struct {
	Symbol            string  `json:"symbol"`
	BaseCurrency      string  `json:"baseAsset"`
	QuoteCurrency     string  `json:"quoteAsset"`
	PricePrecision    int     `json:"priceScale"`
	QuantityPrecision int     `json:"qtyScale"`
	MinQty            float64 `json:"minQty"`
	MaxQty            float64 `json:"maxQty"`
	MinNotional       float64 `json:"minNotional"`
}

type OrderRequest struct {
	Symbol    string  `json:"symbol"`
	Side      string  `json:"side"`      // buy, sell
	OrderType string  `json:"orderType"` // limit, market
	OrderQty  float64 `json:"orderQty"`
	OrderPrice float64 `json:"orderPrice,omitempty"`
}

type OrderResponse struct {
	OrderID string `json:"orderId"`
	Status  string `json:"status"`
}

type Order struct {
	OrderID     string  `json:"orderId"`
	Symbol      string  `json:"symbol"`
	Side        string  `json:"side"`
	OrderType   string  `json:"orderType"`
	Price       float64 `json:"price,string"`
	OrderQty    float64 `json:"orderQty,string"`
	FilledQty   float64 `json:"cumFilledQty,string"`
	Status      string  `json:"status"`
	CreateTime  int64   `json:"lastExecTime"`
	UpdateTime  int64   `json:"updateTime"`
}

type Balance struct {
	Asset            string  `json:"asset"`
	TotalBalance     float64 `json:"totalBalance,string"`
	AvailableBalance float64 `json:"availableBalance,string"`
}

type Ticker struct {
	Symbol string  `json:"symbol"`
	Close  float64 `json:"close,string"`
}

type Kline struct {
	Data KlineData `json:"data"`
}

type KlineData struct {
	Time   int64   `json:"ts"`
	Open   float64 `json:"o,string"`
	High   float64 `json:"h,string"`
	Low    float64 `json:"l,string"`
	Close  float64 `json:"c,string"`
	Volume float64 `json:"v,string"`
}

type KlineResponse struct {
	Symbol string      `json:"s"`
	Data   []Kline     `json:"data"`
}

