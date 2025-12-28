package coinex

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
	"sort"
	"strconv"
	"strings"
	"time"

	"quantmesh/logger"
)

const (
	CoinExMainnetBaseURL = "https://api.coinex.com"           // CoinEx 主网
	CoinExTestnetBaseURL = "https://api.coinex.com"           // CoinEx 测试网（使用相同URL）
)

// CoinExClient CoinEx 客户端
type CoinExClient struct {
	apiKey     string
	secretKey  string
	baseURL    string
	httpClient *http.Client
	isTestnet  bool
}

// NewCoinExClient 创建 CoinEx 客户端
func NewCoinExClient(apiKey, secretKey string, isTestnet bool) *CoinExClient {
	baseURL := CoinExMainnetBaseURL
	if isTestnet {
		baseURL = CoinExTestnetBaseURL
	}

	return &CoinExClient{
		apiKey:     apiKey,
		secretKey:  secretKey,
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		isTestnet:  isTestnet,
	}
}

// signRequest CoinEx 签名：HMAC-SHA256
// 签名字符串：按字母顺序排列的参数字符串
func (c *CoinExClient) signRequest(params map[string]string) string {
	// 按字母顺序排序参数
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// 构建签名字符串
	var parts []string
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", k, params[k]))
	}
	message := strings.Join(parts, "&") + "&secret_key=" + c.secretKey

	h := hmac.New(sha256.New, []byte(c.secretKey))
	h.Write([]byte(message))
	return strings.ToUpper(hex.EncodeToString(h.Sum(nil)))
}

// sendRequest 发送请求
func (c *CoinExClient) sendRequest(ctx context.Context, method, path string, params map[string]string, body interface{}, needSign bool) ([]byte, error) {
	if params == nil {
		params = make(map[string]string)
	}

	if needSign {
		params["access_id"] = c.apiKey
		params["tonce"] = strconv.FormatInt(time.Now().UnixMilli(), 10)
	}

	var reqBody []byte
	var err error
	if body != nil {
		reqBody, err = json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal body error: %w", err)
		}
	}

	reqURL := c.baseURL + path
	if method == http.MethodGet && len(params) > 0 {
		values := url.Values{}
		for k, v := range params {
			values.Set(k, v)
		}
		reqURL += "?" + values.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("create request error: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "QuantMesh/1.0")

	if needSign {
		signature := c.signRequest(params)
		req.Header.Set("authorization", signature)
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

// GetMarket 获取交易对信息
func (c *CoinExClient) GetMarket(ctx context.Context, marketSymbol string) (*Market, error) {
	path := "/v1/market/detail"
	params := map[string]string{
		"market": marketSymbol,
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
		return nil, fmt.Errorf("API error: %s", apiResp.Message)
	}

	var marketInfo Market
	dataBytes, _ := json.Marshal(apiResp.Data)
	if err := json.Unmarshal(dataBytes, &marketInfo); err != nil {
		return nil, fmt.Errorf("unmarshal data error: %w", err)
	}

	return &marketInfo, nil
}

// PlaceOrder 下单
func (c *CoinExClient) PlaceOrder(ctx context.Context, req *OrderRequest) (*Order, error) {
	path := "/v1/order/limit"
	if req.Type == "market" {
		path = "/v1/order/market"
	}

	params := map[string]string{
		"market": req.Market,
		"type":   req.Side,
		"amount": fmt.Sprintf("%.8f", req.Amount),
	}

	if req.Type == "limit" {
		params["price"] = fmt.Sprintf("%.8f", req.Price)
	}

	if req.ClientID != "" {
		params["client_id"] = req.ClientID
	}

	respBody, err := c.sendRequest(ctx, http.MethodPost, path, params, nil, true)
	if err != nil {
		return nil, err
	}

	var apiResp APIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	if apiResp.Code != 0 {
		return nil, fmt.Errorf("API error: %s", apiResp.Message)
	}

	var order Order
	dataBytes, _ := json.Marshal(apiResp.Data)
	if err := json.Unmarshal(dataBytes, &order); err != nil {
		return nil, fmt.Errorf("unmarshal data error: %w", err)
	}

	logger.Info("CoinEx order placed: %d", order.ID)
	return &order, nil
}

// CancelOrder 取消订单
func (c *CoinExClient) CancelOrder(ctx context.Context, market string, orderID int64) error {
	path := "/v1/order/pending"
	params := map[string]string{
		"market": market,
		"id":     strconv.FormatInt(orderID, 10),
	}

	respBody, err := c.sendRequest(ctx, http.MethodDelete, path, params, nil, true)
	if err != nil {
		return err
	}

	var apiResp APIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return fmt.Errorf("unmarshal error: %w", err)
	}

	if apiResp.Code != 0 {
		return fmt.Errorf("API error: %s", apiResp.Message)
	}

	logger.Info("CoinEx order cancelled: %d", orderID)
	return nil
}

// GetOrder 查询订单
func (c *CoinExClient) GetOrder(ctx context.Context, market string, orderID int64) (*Order, error) {
	path := "/v1/order/status"
	params := map[string]string{
		"market": market,
		"id":     strconv.FormatInt(orderID, 10),
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
func (c *CoinExClient) GetOpenOrders(ctx context.Context, market string, page, limit int) ([]Order, error) {
	path := "/v1/order/pending"
	params := map[string]string{
		"market": market,
		"page":   strconv.Itoa(page),
		"limit":  strconv.Itoa(limit),
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
		return nil, fmt.Errorf("API error: %s", apiResp.Message)
	}

	var ordersResp OrdersResponse
	dataBytes, _ := json.Marshal(apiResp.Data)
	if err := json.Unmarshal(dataBytes, &ordersResp); err != nil {
		return nil, fmt.Errorf("unmarshal data error: %w", err)
	}

	return ordersResp.Data, nil
}

// GetBalance 获取账户余额
func (c *CoinExClient) GetBalance(ctx context.Context) (*Balance, error) {
	path := "/v1/balance/info"
	params := map[string]string{}

	respBody, err := c.sendRequest(ctx, http.MethodGet, path, params, nil, true)
	if err != nil {
		return nil, err
	}

	var apiResp APIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	if apiResp.Code != 0 {
		return nil, fmt.Errorf("API error: %s", apiResp.Message)
	}

	var balance Balance
	dataBytes, _ := json.Marshal(apiResp.Data)
	if err := json.Unmarshal(dataBytes, &balance); err != nil {
		return nil, fmt.Errorf("unmarshal data error: %w", err)
	}

	return &balance, nil
}

// GetTrades 获取最新成交
func (c *CoinExClient) GetTrades(ctx context.Context, market string, limit int) ([]Trade, error) {
	path := "/v1/market/deals"
	params := map[string]string{
		"market": market,
		"limit":  strconv.Itoa(limit),
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
		return nil, fmt.Errorf("API error: %s", apiResp.Message)
	}

	var trades []Trade
	dataBytes, _ := json.Marshal(apiResp.Data)
	if err := json.Unmarshal(dataBytes, &trades); err != nil {
		return nil, fmt.Errorf("unmarshal data error: %w", err)
	}

	return trades, nil
}

// GetKlines 获取 K线数据
func (c *CoinExClient) GetKlines(ctx context.Context, market, period string, limit int) ([]Kline, error) {
	path := "/v1/market/kline"
	params := map[string]string{
		"market": market,
		"type":   period,
	}
	if limit > 0 {
		params["limit"] = strconv.Itoa(limit)
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
		return nil, fmt.Errorf("API error: %s", apiResp.Message)
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

type Market struct {
	Name              string  `json:"name"`
	MinAmount         string  `json:"min_amount"`
	MakerFeeRate      string  `json:"maker_fee_rate"`
	TakerFeeRate      string  `json:"taker_fee_rate"`
	PricingName       string  `json:"pricing_name"`
	PricingDecimal    int     `json:"pricing_decimal"`
	TradingName       string  `json:"trading_name"`
	TradingDecimal    int     `json:"trading_decimal"`
	Last              string  `json:"last"`
	Open              string  `json:"open"`
	High              string  `json:"high"`
	Low               string  `json:"low"`
	Volume            string  `json:"volume"`
	Deal              string  `json:"deal"`
}

type OrderRequest struct {
	Market   string
	Type     string  // limit, market
	Side     string  // buy, sell
	Amount   float64
	Price    float64
	ClientID string
}

type Order struct {
	ID           int64   `json:"id"`
	CreateTime   int64   `json:"create_time"`
	FinishedTime int64   `json:"finished_time"`
	Market       string  `json:"market"`
	Type         string  `json:"type"`
	Side         string  `json:"side"`
	Amount       string  `json:"amount"`
	Price        string  `json:"price"`
	DealAmount   string  `json:"deal_amount"`
	DealFee      string  `json:"deal_fee"`
	DealMoney    string  `json:"deal_money"`
	Status       string  `json:"status"`
	ClientID     string  `json:"client_id"`
}

type OrdersResponse struct {
	Count  int     `json:"count"`
	Curr   int     `json:"curr_page"`
	Data   []Order `json:"data"`
	Total  int     `json:"total"`
}

type Balance struct {
	Available map[string]string `json:"available"`
	Frozen    map[string]string `json:"frozen"`
}

type Trade struct {
	ID         int64  `json:"id"`
	CreateTime int64  `json:"date_ms"`
	Amount     string `json:"amount"`
	Price      string `json:"price"`
	Type       string `json:"type"`
}

type Kline struct {
	Timestamp int64    `json:"timestamp"`
	Open      string   `json:"open"`
	High      string   `json:"high"`
	Low       string   `json:"low"`
	Close     string   `json:"close"`
	Volume    string   `json:"volume"`
	Market    string   `json:"market"`
}

