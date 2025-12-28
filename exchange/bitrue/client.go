package bitrue

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
	BitrueMainnetBaseURL = "https://openapi.bitrue.com"           // Bitrue 主网
	BitrueTestnetBaseURL = "https://testnet-openapi.bitrue.com"   // Bitrue 测试网
)

// BitrueClient Bitrue 客户端
type BitrueClient struct {
	apiKey     string
	secretKey  string
	baseURL    string
	httpClient *http.Client
	isTestnet  bool
}

// NewBitrueClient 创建 Bitrue 客户端
func NewBitrueClient(apiKey, secretKey string, isTestnet bool) *BitrueClient {
	baseURL := BitrueMainnetBaseURL
	if isTestnet {
		baseURL = BitrueTestnetBaseURL
	}

	return &BitrueClient{
		apiKey:     apiKey,
		secretKey:  secretKey,
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		isTestnet:  isTestnet,
	}
}

// signRequest Bitrue 签名：HMAC-SHA256
func (c *BitrueClient) signRequest(queryString string) string {
	h := hmac.New(sha256.New, []byte(c.secretKey))
	h.Write([]byte(queryString))
	return hex.EncodeToString(h.Sum(nil))
}

// sendRequest 发送请求
func (c *BitrueClient) sendRequest(ctx context.Context, method, path string, params url.Values, body interface{}, needSign bool) ([]byte, error) {
	if params == nil {
		params = url.Values{}
	}

	if needSign {
		params.Set("timestamp", strconv.FormatInt(time.Now().UnixMilli(), 10))
		params.Set("recvWindow", "5000")
		
		queryString := params.Encode()
		signature := c.signRequest(queryString)
		params.Set("signature", signature)
	}

	reqURL := c.baseURL + path
	queryString := params.Encode()
	
	var reqBody []byte
	var err error
	if body != nil {
		reqBody, err = json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal body error: %w", err)
		}
	}

	if method == http.MethodGet && queryString != "" {
		reqURL += "?" + queryString
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("create request error: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-MBX-APIKEY", c.apiKey)

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
		var apiErr APIError
		if err := json.Unmarshal(respBody, &apiErr); err == nil {
			return nil, fmt.Errorf("API error: code=%d, msg=%s", apiErr.Code, apiErr.Msg)
		}
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

// GetExchangeInfo 获取交易对信息
func (c *BitrueClient) GetExchangeInfo(ctx context.Context) (*ExchangeInfo, error) {
	path := "/api/v1/exchangeInfo"
	params := url.Values{}

	respBody, err := c.sendRequest(ctx, http.MethodGet, path, params, nil, false)
	if err != nil {
		return nil, err
	}

	var exchangeInfo ExchangeInfo
	if err := json.Unmarshal(respBody, &exchangeInfo); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	return &exchangeInfo, nil
}

// PlaceOrder 下单
func (c *BitrueClient) PlaceOrder(ctx context.Context, req *OrderRequest) (*Order, error) {
	path := "/api/v1/order"
	params := url.Values{}
	params.Set("symbol", req.Symbol)
	params.Set("side", req.Side)
	params.Set("type", req.Type)
	params.Set("quantity", fmt.Sprintf("%.8f", req.Quantity))

	if req.Type == "LIMIT" {
		params.Set("price", fmt.Sprintf("%.8f", req.Price))
		params.Set("timeInForce", "GTC")
	}

	if req.NewClientOrderID != "" {
		params.Set("newClientOrderId", req.NewClientOrderID)
	}

	respBody, err := c.sendRequest(ctx, http.MethodPost, path, params, nil, true)
	if err != nil {
		return nil, err
	}

	var order Order
	if err := json.Unmarshal(respBody, &order); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	logger.Info("Bitrue order placed: %d", order.OrderID)
	return &order, nil
}

// CancelOrder 取消订单
func (c *BitrueClient) CancelOrder(ctx context.Context, symbol string, orderID int64) error {
	path := "/api/v1/order"
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("orderId", strconv.FormatInt(orderID, 10))

	_, err := c.sendRequest(ctx, http.MethodDelete, path, params, nil, true)
	if err != nil {
		return err
	}

	logger.Info("Bitrue order cancelled: %d", orderID)
	return nil
}

// GetOrder 查询订单
func (c *BitrueClient) GetOrder(ctx context.Context, symbol string, orderID int64) (*Order, error) {
	path := "/api/v1/order"
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("orderId", strconv.FormatInt(orderID, 10))

	respBody, err := c.sendRequest(ctx, http.MethodGet, path, params, nil, true)
	if err != nil {
		return nil, err
	}

	var order Order
	if err := json.Unmarshal(respBody, &order); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	return &order, nil
}

// GetOpenOrders 获取活跃订单
func (c *BitrueClient) GetOpenOrders(ctx context.Context, symbol string) ([]Order, error) {
	path := "/api/v1/openOrders"
	params := url.Values{}
	if symbol != "" {
		params.Set("symbol", symbol)
	}

	respBody, err := c.sendRequest(ctx, http.MethodGet, path, params, nil, true)
	if err != nil {
		return nil, err
	}

	var orders []Order
	if err := json.Unmarshal(respBody, &orders); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	return orders, nil
}

// GetAccount 获取账户信息
func (c *BitrueClient) GetAccount(ctx context.Context) (*Account, error) {
	path := "/api/v1/account"
	params := url.Values{}

	respBody, err := c.sendRequest(ctx, http.MethodGet, path, params, nil, true)
	if err != nil {
		return nil, err
	}

	var account Account
	if err := json.Unmarshal(respBody, &account); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	return &account, nil
}

// GetTicker 获取最新价格
func (c *BitrueClient) GetTicker(ctx context.Context, symbol string) (*Ticker, error) {
	path := "/api/v1/ticker/price"
	params := url.Values{}
	params.Set("symbol", symbol)

	respBody, err := c.sendRequest(ctx, http.MethodGet, path, params, nil, false)
	if err != nil {
		return nil, err
	}

	var ticker Ticker
	if err := json.Unmarshal(respBody, &ticker); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	return &ticker, nil
}

// GetKlines 获取 K线数据
func (c *BitrueClient) GetKlines(ctx context.Context, symbol, interval string, limit int) ([]Kline, error) {
	path := "/api/v1/klines"
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

	var rawKlines [][]interface{}
	if err := json.Unmarshal(respBody, &rawKlines); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	klines := make([]Kline, 0, len(rawKlines))
	for _, raw := range rawKlines {
		if len(raw) < 6 {
			continue
		}

		kline := Kline{
			OpenTime:  int64(raw[0].(float64)),
			Open:      raw[1].(string),
			High:      raw[2].(string),
			Low:       raw[3].(string),
			Close:     raw[4].(string),
			Volume:    raw[5].(string),
		}
		klines = append(klines, kline)
	}

	return klines, nil
}

// 数据结构定义

type APIError struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

type ExchangeInfo struct {
	Timezone   string   `json:"timezone"`
	ServerTime int64    `json:"serverTime"`
	Symbols    []Symbol `json:"symbols"`
}

type Symbol struct {
	Symbol              string   `json:"symbol"`
	Status              string   `json:"status"`
	BaseAsset           string   `json:"baseAsset"`
	QuoteAsset          string   `json:"quoteAsset"`
	BaseAssetPrecision  int      `json:"baseAssetPrecision"`
	QuoteAssetPrecision int      `json:"quotePrecision"`
	OrderTypes          []string `json:"orderTypes"`
}

type OrderRequest struct {
	Symbol           string
	Side             string  // BUY, SELL
	Type             string  // LIMIT, MARKET
	Quantity         float64
	Price            float64
	NewClientOrderID string
}

type Order struct {
	Symbol              string `json:"symbol"`
	OrderID             int64  `json:"orderId"`
	ClientOrderID       string `json:"clientOrderId"`
	Price               string `json:"price"`
	OrigQty             string `json:"origQty"`
	ExecutedQty         string `json:"executedQty"`
	CummulativeQuoteQty string `json:"cummulativeQuoteQty"`
	Status              string `json:"status"`
	TimeInForce         string `json:"timeInForce"`
	Type                string `json:"type"`
	Side                string `json:"side"`
	Time                int64  `json:"time"`
	UpdateTime          int64  `json:"updateTime"`
}

type Account struct {
	MakerCommission  int       `json:"makerCommission"`
	TakerCommission  int       `json:"takerCommission"`
	BuyerCommission  int       `json:"buyerCommission"`
	SellerCommission int       `json:"sellerCommission"`
	CanTrade         bool      `json:"canTrade"`
	CanWithdraw      bool      `json:"canWithdraw"`
	CanDeposit       bool      `json:"canDeposit"`
	UpdateTime       int64     `json:"updateTime"`
	Balances         []Balance `json:"balances"`
}

type Balance struct {
	Asset  string `json:"asset"`
	Free   string `json:"free"`
	Locked string `json:"locked"`
}

type Ticker struct {
	Symbol string `json:"symbol"`
	Price  string `json:"price"`
}

type Kline struct {
	OpenTime  int64  `json:"openTime"`
	Open      string `json:"open"`
	High      string `json:"high"`
	Low       string `json:"low"`
	Close     string `json:"close"`
	Volume    string `json:"volume"`
}

