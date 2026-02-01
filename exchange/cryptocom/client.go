package cryptocom

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
	"sort"
	"strconv"
	"strings"
	"time"

	"quantmesh/logger"
)

const (
	CryptoComMainnetBaseURL = "https://api.crypto.com/v2"     // Crypto.com 主网
	CryptoComTestnetBaseURL = "https://uat-api.crypto.com/v2" // Crypto.com 测试网
)

// CryptoComClient Crypto.com 客户端
type CryptoComClient struct {
	apiKey     string
	secretKey  string
	baseURL    string
	httpClient *http.Client
	isTestnet  bool
}

// NewCryptoComClient 创建 Crypto.com 客户端
func NewCryptoComClient(apiKey, secretKey string, isTestnet bool) *CryptoComClient {
	baseURL := CryptoComMainnetBaseURL
	if isTestnet {
		baseURL = CryptoComTestnetBaseURL
	}

	return &CryptoComClient{
		apiKey:     apiKey,
		secretKey:  secretKey,
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		isTestnet:  isTestnet,
	}
}

// signRequest Crypto.com 签名：HMAC-SHA256
func (c *CryptoComClient) signRequest(method string, params map[string]interface{}, nonce int64) string {
	// 参数排序
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// 构建签名字符串
	var paramStr strings.Builder
	for _, k := range keys {
		paramStr.WriteString(k)
		paramStr.WriteString(fmt.Sprintf("%v", params[k]))
	}

	signPayload := method + strconv.FormatInt(nonce, 10) + c.apiKey + paramStr.String()

	h := hmac.New(sha256.New, []byte(c.secretKey))
	h.Write([]byte(signPayload))
	return hex.EncodeToString(h.Sum(nil))
}

// sendRequest 发送请求
func (c *CryptoComClient) sendRequest(ctx context.Context, method string, params map[string]interface{}) ([]byte, error) {
	nonce := time.Now().UnixMilli()

	// 构建请求体
	requestBody := map[string]interface{}{
		"id":      nonce,
		"method":  method,
		"api_key": c.apiKey,
		"params":  params,
		"nonce":   nonce,
	}

	// 签名
	sig := c.signRequest(method, params, nonce)
	requestBody["sig"] = sig

	reqBody, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("marshal body error: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("create request error: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

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

// GetInstruments 获取交易对信息
func (c *CryptoComClient) GetInstruments(ctx context.Context) ([]Instrument, error) {
	method := "public/get-instruments"
	params := map[string]interface{}{}

	respBody, err := c.sendRequest(ctx, method, params)
	if err != nil {
		return nil, err
	}

	var apiResp APIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	if apiResp.Code != 0 {
		return nil, fmt.Errorf("API error: code=%d", apiResp.Code)
	}

	var result InstrumentsResult
	dataBytes, _ := json.Marshal(apiResp.Result)
	if err := json.Unmarshal(dataBytes, &result); err != nil {
		return nil, fmt.Errorf("unmarshal data error: %w", err)
	}

	return result.Instruments, nil
}

// CreateOrder 下单
func (c *CryptoComClient) CreateOrder(ctx context.Context, req *OrderRequest) (*Order, error) {
	method := "private/create-order"
	params := map[string]interface{}{
		"instrument_name": req.InstrumentName,
		"side":            req.Side,
		"type":            req.Type,
		"quantity":        req.Quantity,
	}

	if req.Type == "LIMIT" {
		params["price"] = req.Price
	}

	if req.ClientOID != "" {
		params["client_oid"] = req.ClientOID
	}

	respBody, err := c.sendRequest(ctx, method, params)
	if err != nil {
		return nil, err
	}

	var apiResp APIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	if apiResp.Code != 0 {
		return nil, fmt.Errorf("API error: code=%d", apiResp.Code)
	}

	var order Order
	dataBytes, _ := json.Marshal(apiResp.Result)
	if err := json.Unmarshal(dataBytes, &order); err != nil {
		return nil, fmt.Errorf("unmarshal data error: %w", err)
	}

	logger.Info("Crypto.com order placed: %d", order.OrderID)
	return &order, nil
}

// CancelOrder 取消订单
func (c *CryptoComClient) CancelOrder(ctx context.Context, instrumentName string, orderID int64) error {
	method := "private/cancel-order"
	params := map[string]interface{}{
		"instrument_name": instrumentName,
		"order_id":        strconv.FormatInt(orderID, 10),
	}

	respBody, err := c.sendRequest(ctx, method, params)
	if err != nil {
		return err
	}

	var apiResp APIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return fmt.Errorf("unmarshal error: %w", err)
	}

	if apiResp.Code != 0 {
		return fmt.Errorf("API error: code=%d", apiResp.Code)
	}

	logger.Info("Crypto.com order cancelled: %d", orderID)
	return nil
}

// GetOrderDetail 查询订单
func (c *CryptoComClient) GetOrderDetail(ctx context.Context, orderID int64) (*Order, error) {
	method := "private/get-order-detail"
	params := map[string]interface{}{
		"order_id": strconv.FormatInt(orderID, 10),
	}

	respBody, err := c.sendRequest(ctx, method, params)
	if err != nil {
		return nil, err
	}

	var apiResp APIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	if apiResp.Code != 0 {
		return nil, fmt.Errorf("API error: code=%d", apiResp.Code)
	}

	var result OrderDetailResult
	dataBytes, _ := json.Marshal(apiResp.Result)
	if err := json.Unmarshal(dataBytes, &result); err != nil {
		return nil, fmt.Errorf("unmarshal data error: %w", err)
	}

	return &result.OrderInfo, nil
}

// GetOpenOrders 获取活跃订单
func (c *CryptoComClient) GetOpenOrders(ctx context.Context, instrumentName string) ([]Order, error) {
	method := "private/get-open-orders"
	params := map[string]interface{}{}
	if instrumentName != "" {
		params["instrument_name"] = instrumentName
	}

	respBody, err := c.sendRequest(ctx, method, params)
	if err != nil {
		return nil, err
	}

	var apiResp APIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	if apiResp.Code != 0 {
		return nil, fmt.Errorf("API error: code=%d", apiResp.Code)
	}

	var result OpenOrdersResult
	dataBytes, _ := json.Marshal(apiResp.Result)
	if err := json.Unmarshal(dataBytes, &result); err != nil {
		return nil, fmt.Errorf("unmarshal data error: %w", err)
	}

	return result.OrderList, nil
}

// GetAccountSummary 获取账户信息
func (c *CryptoComClient) GetAccountSummary(ctx context.Context) (*AccountSummary, error) {
	method := "private/get-account-summary"
	params := map[string]interface{}{}

	respBody, err := c.sendRequest(ctx, method, params)
	if err != nil {
		return nil, err
	}

	var apiResp APIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	if apiResp.Code != 0 {
		return nil, fmt.Errorf("API error: code=%d", apiResp.Code)
	}

	var result AccountSummaryResult
	dataBytes, _ := json.Marshal(apiResp.Result)
	if err := json.Unmarshal(dataBytes, &result); err != nil {
		return nil, fmt.Errorf("unmarshal data error: %w", err)
	}

	return &result.Accounts[0], nil
}

// GetTicker 获取最新价格
func (c *CryptoComClient) GetTicker(ctx context.Context, instrumentName string) (*Ticker, error) {
	method := "public/get-ticker"
	params := map[string]interface{}{
		"instrument_name": instrumentName,
	}

	respBody, err := c.sendRequest(ctx, method, params)
	if err != nil {
		return nil, err
	}

	var apiResp APIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	if apiResp.Code != 0 {
		return nil, fmt.Errorf("API error: code=%d", apiResp.Code)
	}

	var result TickerResult
	dataBytes, _ := json.Marshal(apiResp.Result)
	if err := json.Unmarshal(dataBytes, &result); err != nil {
		return nil, fmt.Errorf("unmarshal data error: %w", err)
	}

	return &result.Data, nil
}

// GetCandlestick 获取 K线数据
func (c *CryptoComClient) GetCandlestick(ctx context.Context, instrumentName, timeframe string) ([]Candlestick, error) {
	method := "public/get-candlestick"
	params := map[string]interface{}{
		"instrument_name": instrumentName,
		"timeframe":       timeframe,
	}

	respBody, err := c.sendRequest(ctx, method, params)
	if err != nil {
		return nil, err
	}

	var apiResp APIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	if apiResp.Code != 0 {
		return nil, fmt.Errorf("API error: code=%d", apiResp.Code)
	}

	var result CandlestickResult
	dataBytes, _ := json.Marshal(apiResp.Result)
	if err := json.Unmarshal(dataBytes, &result); err != nil {
		return nil, fmt.Errorf("unmarshal data error: %w", err)
	}

	return result.Data, nil
}

// 数据结构定义

type APIResponse struct {
	ID     int64       `json:"id"`
	Method string      `json:"method"`
	Code   int         `json:"code"`
	Result interface{} `json:"result"`
}

type Instrument struct {
	InstrumentName   string  `json:"instrument_name"`
	QuoteCurrency    string  `json:"quote_currency"`
	BaseCurrency     string  `json:"base_currency"`
	PriceDecimals    int     `json:"price_decimals"`
	QuantityDecimals int     `json:"quantity_decimals"`
	MinQuantity      float64 `json:"min_quantity,string"`
	MaxQuantity      float64 `json:"max_quantity,string"`
}

type InstrumentsResult struct {
	Instruments []Instrument `json:"instruments"`
}

type OrderRequest struct {
	InstrumentName string
	Side           string // BUY, SELL
	Type           string // LIMIT, MARKET
	Quantity       float64
	Price          float64
	ClientOID      string
}

type Order struct {
	OrderID        int64   `json:"order_id"`
	ClientOID      string  `json:"client_oid"`
	InstrumentName string  `json:"instrument_name"`
	Side           string  `json:"side"`
	Type           string  `json:"type"`
	Price          float64 `json:"price,string"`
	Quantity       float64 `json:"quantity,string"`
	CumQuantity    float64 `json:"cumulative_quantity,string"`
	Status         string  `json:"status"`
	CreateTime     int64   `json:"create_time"`
	UpdateTime     int64   `json:"update_time"`
}

type OrderDetailResult struct {
	OrderInfo Order `json:"order_info"`
}

type OpenOrdersResult struct {
	OrderList []Order `json:"order_list"`
}

type AccountSummary struct {
	Balance   float64 `json:"balance,string"`
	Available float64 `json:"available,string"`
	Order     float64 `json:"order,string"`
	Stake     float64 `json:"stake,string"`
	Currency  string  `json:"currency"`
}

type AccountSummaryResult struct {
	Accounts []AccountSummary `json:"accounts"`
}

type Ticker struct {
	InstrumentName string  `json:"i"`
	BestBid        float64 `json:"b,string"`
	BestAsk        float64 `json:"k,string"`
	LastPrice      float64 `json:"a,string"`
	Volume         float64 `json:"v,string"`
	Timestamp      int64   `json:"t"`
}

type TickerResult struct {
	Data Ticker `json:"data"`
}

type Candlestick struct {
	Timestamp int64   `json:"t"`
	Open      float64 `json:"o,string"`
	High      float64 `json:"h,string"`
	Low       float64 `json:"l,string"`
	Close     float64 `json:"c,string"`
	Volume    float64 `json:"v,string"`
}

type CandlestickResult struct {
	InstrumentName string        `json:"instrument_name"`
	Interval       string        `json:"interval"`
	Data           []Candlestick `json:"data"`
}
