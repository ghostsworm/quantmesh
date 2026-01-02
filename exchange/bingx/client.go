package bingx

import (
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
	BingXMainnetBaseURL = "https://open-api.bingx.com"     // BingX 主网
	BingXTestnetBaseURL = "https://open-api-vst.bingx.com" // BingX 测试网
)

// BingXClient BingX 客户端
type BingXClient struct {
	apiKey     string
	secretKey  string
	baseURL    string
	httpClient *http.Client
	isTestnet  bool
}

// NewBingXClient 创建 BingX 客户端
func NewBingXClient(apiKey, secretKey string, isTestnet bool) *BingXClient {
	baseURL := BingXMainnetBaseURL
	if isTestnet {
		baseURL = BingXTestnetBaseURL
	}

	return &BingXClient{
		apiKey:     apiKey,
		secretKey:  secretKey,
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		isTestnet:  isTestnet,
	}
}

// signRequest BingX 签名：HMAC-SHA256
func (c *BingXClient) signRequest(params url.Values) string {
	// 按字母序排序参数
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// 构造签名字符串
	var signStr strings.Builder
	for i, k := range keys {
		if i > 0 {
			signStr.WriteString("&")
		}
		signStr.WriteString(k)
		signStr.WriteString("=")
		signStr.WriteString(params.Get(k))
	}

	// HMAC-SHA256 签名
	h := hmac.New(sha256.New, []byte(c.secretKey))
	h.Write([]byte(signStr.String()))
	return hex.EncodeToString(h.Sum(nil))
}

// sendRequest 发送请求
func (c *BingXClient) sendRequest(ctx context.Context, method, path string, params url.Values, needSign bool) ([]byte, error) {
	reqURL := c.baseURL + path

	if needSign {
		// 添加时间戳
		params.Set("timestamp", strconv.FormatInt(time.Now().UnixMilli(), 10))
		// 生成签名
		signature := c.signRequest(params)
		params.Set("signature", signature)
	}

	var req *http.Request
	var err error

	if method == http.MethodGet || method == http.MethodDelete {
		if len(params) > 0 {
			reqURL += "?" + params.Encode()
		}
		req, err = http.NewRequestWithContext(ctx, method, reqURL, nil)
	} else {
		req, err = http.NewRequestWithContext(ctx, method, reqURL, strings.NewReader(params.Encode()))
		if err == nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		}
	}

	if err != nil {
		return nil, fmt.Errorf("create request error: %w", err)
	}

	// 设置请求头
	req.Header.Set("X-BX-APIKEY", c.apiKey)

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

	// 检查 API 错误
	var apiResp struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.Unmarshal(respBody, &apiResp); err == nil {
		if apiResp.Code != 0 {
			return nil, fmt.Errorf("API error %d: %s", apiResp.Code, apiResp.Msg)
		}
	}

	return respBody, nil
}

// GetExchangeInfo 获取交易对信息
func (c *BingXClient) GetExchangeInfo(ctx context.Context) (*ExchangeInfo, error) {
	path := "/openApi/swap/v2/quote/contracts"
	params := url.Values{}

	respBody, err := c.sendRequest(ctx, http.MethodGet, path, params, false)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Code int              `json:"code"`
		Data []ContractDetail `json:"data"`
	}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	if resp.Code != 0 {
		return nil, fmt.Errorf("get exchange info failed")
	}

	exchangeInfo := &ExchangeInfo{
		Symbols: make(map[string]ContractDetail),
	}
	for _, detail := range resp.Data {
		exchangeInfo.Symbols[detail.Symbol] = detail
	}

	return exchangeInfo, nil
}

// PlaceOrder 下单
func (c *BingXClient) PlaceOrder(ctx context.Context, req *OrderRequest) (*OrderResponse, error) {
	path := "/openApi/swap/v2/trade/order"
	params := url.Values{}
	params.Set("symbol", req.Symbol)
	params.Set("side", req.Side)                 // BUY, SELL
	params.Set("positionSide", req.PositionSide) // LONG, SHORT
	params.Set("type", req.Type)                 // LIMIT, MARKET

	if req.Price > 0 {
		params.Set("price", fmt.Sprintf("%.8f", req.Price))
	}
	params.Set("quantity", fmt.Sprintf("%.4f", req.Quantity))

	if req.ClientOrderID != "" {
		params.Set("clientOrderID", req.ClientOrderID)
	}

	respBody, err := c.sendRequest(ctx, http.MethodPost, path, params, true)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Code int           `json:"code"`
		Data OrderResponse `json:"data"`
	}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	if resp.Code != 0 {
		return nil, fmt.Errorf("place order failed")
	}

	logger.Info("BingX order placed: %d", resp.Data.OrderID)
	return &resp.Data, nil
}

// CancelOrder 取消订单
func (c *BingXClient) CancelOrder(ctx context.Context, symbol string, orderID int64) error {
	path := "/openApi/swap/v2/trade/order"
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("orderId", strconv.FormatInt(orderID, 10))

	respBody, err := c.sendRequest(ctx, http.MethodDelete, path, params, true)
	if err != nil {
		return err
	}

	var resp struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return fmt.Errorf("unmarshal error: %w", err)
	}

	if resp.Code != 0 {
		return fmt.Errorf("cancel order failed")
	}

	logger.Info("BingX order cancelled: %d", orderID)
	return nil
}

// GetOrderInfo 查询订单
func (c *BingXClient) GetOrderInfo(ctx context.Context, symbol string, orderID int64) (*OrderInfo, error) {
	path := "/openApi/swap/v2/trade/order"
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("orderId", strconv.FormatInt(orderID, 10))

	respBody, err := c.sendRequest(ctx, http.MethodGet, path, params, true)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Code int       `json:"code"`
		Data OrderInfo `json:"data"`
	}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	if resp.Code != 0 {
		return nil, fmt.Errorf("get order info failed")
	}

	return &resp.Data, nil
}

// GetOpenOrders 获取活跃订单
func (c *BingXClient) GetOpenOrders(ctx context.Context, symbol string) ([]OrderInfo, error) {
	path := "/openApi/swap/v2/trade/openOrders"
	params := url.Values{}
	params.Set("symbol", symbol)

	respBody, err := c.sendRequest(ctx, http.MethodGet, path, params, true)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Code int         `json:"code"`
		Data []OrderInfo `json:"data"`
	}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	if resp.Code != 0 {
		return nil, fmt.Errorf("get open orders failed")
	}

	return resp.Data, nil
}

// GetAccount 获取账户信息
func (c *BingXClient) GetAccount(ctx context.Context) (*AccountInfo, error) {
	path := "/openApi/swap/v2/user/balance"
	params := url.Values{}

	respBody, err := c.sendRequest(ctx, http.MethodGet, path, params, true)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Code int         `json:"code"`
		Data AccountInfo `json:"data"`
	}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	if resp.Code != 0 {
		return nil, fmt.Errorf("get account info failed")
	}

	return &resp.Data, nil
}

// GetPositions 获取持仓
func (c *BingXClient) GetPositions(ctx context.Context, symbol string) ([]PositionInfo, error) {
	path := "/openApi/swap/v2/user/positions"
	params := url.Values{}
	if symbol != "" {
		params.Set("symbol", symbol)
	}

	respBody, err := c.sendRequest(ctx, http.MethodGet, path, params, true)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Code int            `json:"code"`
		Data []PositionInfo `json:"data"`
	}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	if resp.Code != 0 {
		return nil, fmt.Errorf("get positions failed")
	}

	return resp.Data, nil
}

// GetTicker 获取行情
func (c *BingXClient) GetTicker(ctx context.Context, symbol string) (*TickerInfo, error) {
	path := "/openApi/swap/v2/quote/ticker"
	params := url.Values{}
	params.Set("symbol", symbol)

	respBody, err := c.sendRequest(ctx, http.MethodGet, path, params, false)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Code int        `json:"code"`
		Data TickerInfo `json:"data"`
	}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	if resp.Code != 0 {
		return nil, fmt.Errorf("get ticker failed")
	}

	return &resp.Data, nil
}

// GetKlines 获取 K线数据
func (c *BingXClient) GetKlines(ctx context.Context, symbol, interval string, limit int) ([]Kline, error) {
	path := "/openApi/swap/v3/quote/klines"
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("interval", interval)
	if limit > 0 {
		params.Set("limit", strconv.Itoa(limit))
	}

	respBody, err := c.sendRequest(ctx, http.MethodGet, path, params, false)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Code int     `json:"code"`
		Data []Kline `json:"data"`
	}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	if resp.Code != 0 {
		return nil, fmt.Errorf("get klines failed")
	}

	return resp.Data, nil
}

// 数据结构定义

type ExchangeInfo struct {
	Symbols map[string]ContractDetail
}

type ContractDetail struct {
	Symbol            string  `json:"symbol"`
	Size              float64 `json:"size"`
	QuantityPrecision int     `json:"quantityPrecision"`
	PricePrecision    int     `json:"pricePrecision"`
	FeeRate           float64 `json:"feeRate"`
	TradeMinLimit     float64 `json:"tradeMinLimit"`
	Currency          string  `json:"currency"`
	Asset             string  `json:"asset"`
}

type OrderRequest struct {
	Symbol        string
	Side          string // BUY, SELL
	PositionSide  string // LONG, SHORT
	Type          string // LIMIT, MARKET
	Price         float64
	Quantity      float64
	ClientOrderID string
}

type OrderResponse struct {
	OrderID int64  `json:"orderId"`
	Symbol  string `json:"symbol"`
}

type OrderInfo struct {
	OrderID       int64   `json:"orderId"`
	Symbol        string  `json:"symbol"`
	Price         float64 `json:"price,string"`
	Quantity      float64 `json:"quantity,string"`
	ExecutedQty   float64 `json:"executedQty,string"`
	Side          string  `json:"side"`
	PositionSide  string  `json:"positionSide"`
	Type          string  `json:"type"`
	Status        string  `json:"status"`
	ClientOrderID string  `json:"clientOrderID"`
	UpdateTime    int64   `json:"updateTime"`
	WorkingType   string  `json:"workingType"`
	AvgPrice      float64 `json:"avgPrice,string"`
}

type AccountInfo struct {
	Balance          BalanceInfo `json:"balance"`
	AvailableMargin  float64     `json:"availableMargin,string"`
	UsedMargin       float64     `json:"usedMargin,string"`
	UnrealizedProfit float64     `json:"unrealizedProfit,string"`
}

type BalanceInfo struct {
	Balance          float64 `json:"balance,string"`
	AvailableMargin  float64 `json:"availableMargin,string"`
	UsedMargin       float64 `json:"usedMargin,string"`
	UnrealizedProfit float64 `json:"unrealizedProfit,string"`
}

type PositionInfo struct {
	Symbol           string  `json:"symbol"`
	PositionSide     string  `json:"positionSide"` // LONG, SHORT
	PositionAmt      float64 `json:"positionAmt,string"`
	AvailableAmt     float64 `json:"availableAmt,string"`
	UnrealizedProfit float64 `json:"unrealizedProfit,string"`
	RealisedProfit   float64 `json:"realisedProfit,string"`
	InitialMargin    float64 `json:"initialMargin,string"`
	AvgPrice         float64 `json:"avgPrice,string"`
	Leverage         int     `json:"leverage"`
	PositionValue    float64 `json:"positionValue,string"`
	MarkPrice        float64 `json:"markPrice,string"`
	LiquidationPrice float64 `json:"liquidationPrice,string"`
}

type TickerInfo struct {
	Symbol      string  `json:"symbol"`
	LastPrice   float64 `json:"lastPrice,string"`
	BidPrice    float64 `json:"bidPrice,string"`
	AskPrice    float64 `json:"askPrice,string"`
	Volume      float64 `json:"volume,string"`
	OpenPrice   float64 `json:"openPrice,string"`
	HighPrice   float64 `json:"highPrice,string"`
	LowPrice    float64 `json:"lowPrice,string"`
	PriceChange float64 `json:"priceChange,string"`
	FundingRate float64 `json:"fundingRate,string"`
	Time        int64   `json:"time"`
}

type Kline struct {
	Time   int64   `json:"time"`
	Open   float64 `json:"open,string"`
	High   float64 `json:"high,string"`
	Low    float64 `json:"low,string"`
	Close  float64 `json:"close,string"`
	Volume float64 `json:"volume,string"`
}
