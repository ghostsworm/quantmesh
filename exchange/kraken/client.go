package kraken

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"quantmesh/logger"
)

const (
	KrakenBaseURL = "https://futures.kraken.com" // Kraken 期货 API
)

// KrakenClient 结构体
type KrakenClient struct {
	apiKey     string
	secretKey  string
	httpClient *http.Client
}

// NewKrakenClient 创建 Kraken 客户端实例
func NewKrakenClient(apiKey, secretKey string) *KrakenClient {
	return &KrakenClient{
		apiKey:    apiKey,
		secretKey: secretKey,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// signRequest 对请求进行签名
func (c *KrakenClient) signRequest(path, nonce, postData string) string {
	// Kraken 签名算法：
	// 1. SHA256(nonce + postData)
	// 2. HMAC-SHA512(path + sha256Hash, base64DecodedSecret)
	// 3. Base64 编码结果

	// 步骤 1: SHA256(nonce + postData)
	sha := sha256.New()
	sha.Write([]byte(nonce + postData))
	shaSum := sha.Sum(nil)

	// 步骤 2: 解码 secret
	secretDecoded, err := base64.StdEncoding.DecodeString(c.secretKey)
	if err != nil {
		logger.Error("Kraken decode secret error: %v", err)
		return ""
	}

	// 步骤 3: HMAC-SHA512(path + sha256Hash, secret)
	h := hmac.New(sha512.New, secretDecoded)
	h.Write([]byte(path))
	h.Write(shaSum)
	signature := base64.StdEncoding.EncodeToString(h.Sum(nil))

	return signature
}

// sendRequest 发送 HTTP 请求
func (c *KrakenClient) sendRequest(ctx context.Context, method, path string, params map[string]interface{}) ([]byte, error) {
	reqURL := fmt.Sprintf("%s%s", KrakenBaseURL, path)

	var bodyStr string
	if params != nil && len(params) > 0 {
		values := url.Values{}
		for k, v := range params {
			values.Add(k, fmt.Sprintf("%v", v))
		}
		bodyStr = values.Encode()
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
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("APIKey", c.apiKey)
	req.Header.Set("Nonce", nonce)
	req.Header.Set("Authent", signature)

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

	var baseResp struct {
		Result string `json:"result"`
		Error  string `json:"error"`
	}
	if err := json.Unmarshal(respBody, &baseResp); err == nil {
		if baseResp.Result == "error" {
			return nil, fmt.Errorf("API error! Message: %s", baseResp.Error)
		}
	}

	return respBody, nil
}

// GetExchangeInfo 获取合约信息
func (c *KrakenClient) GetExchangeInfo(ctx context.Context) (*ExchangeInfo, error) {
	path := "/derivatives/api/v3/instruments"
	respBody, err := c.sendRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Result      string         `json:"result"`
		Instruments []ContractInfo `json:"instruments"`
	}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal exchange info error: %w", err)
	}

	exchangeInfo := &ExchangeInfo{
		Symbols: make(map[string]ContractInfo),
	}
	for _, info := range resp.Instruments {
		exchangeInfo.Symbols[info.Symbol] = info
	}
	return exchangeInfo, nil
}

// PlaceOrder 下单
func (c *KrakenClient) PlaceOrder(ctx context.Context, req *OrderRequest) (*OrderResponse, error) {
	path := "/derivatives/api/v3/sendorder"
	params := map[string]interface{}{
		"orderType": strings.ToLower(string(req.Type)), // "lmt" or "mkt"
		"symbol":    req.Symbol,
		"side":      strings.ToLower(string(req.Side)), // "buy" or "sell"
		"size":      int(req.Quantity),
	}

	if req.Type == "limit" || req.Type == "lmt" {
		params["orderType"] = "lmt"
		params["limitPrice"] = req.Price
	} else {
		params["orderType"] = "mkt"
	}

	if req.ClientOrderID != "" {
		params["cliOrdId"] = req.ClientOrderID
	}

	respBody, err := c.sendRequest(ctx, http.MethodPost, path, params)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Result     string `json:"result"`
		SendStatus struct {
			OrderID string `json:"order_id"`
			Status  string `json:"status"`
		} `json:"sendStatus"`
	}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal place order response error: %w", err)
	}

	if resp.Result != "success" {
		return nil, fmt.Errorf("place order failed: %s", string(respBody))
	}

	return &OrderResponse{OrderID: resp.SendStatus.OrderID}, nil
}

// CancelOrder 取消订单
func (c *KrakenClient) CancelOrder(ctx context.Context, orderID string) (*CancelOrderResponse, error) {
	path := "/derivatives/api/v3/cancelorder"
	params := map[string]interface{}{
		"order_id": orderID,
	}

	respBody, err := c.sendRequest(ctx, http.MethodPost, path, params)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Result       string `json:"result"`
		CancelStatus struct {
			Status string `json:"status"`
		} `json:"cancelStatus"`
	}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal cancel order response error: %w", err)
	}

	if resp.Result != "success" {
		return nil, fmt.Errorf("cancel order failed: %s", string(respBody))
	}

	return &CancelOrderResponse{OrderID: orderID}, nil
}

// GetOrderInfo 查询订单
func (c *KrakenClient) GetOrderInfo(ctx context.Context, orderID string) (*OrderInfo, error) {
	path := "/derivatives/api/v3/orders/status"
	params := map[string]interface{}{
		"orderIds": orderID,
	}

	respBody, err := c.sendRequest(ctx, http.MethodPost, path, params)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Result string      `json:"result"`
		Orders []OrderInfo `json:"orders"`
	}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal get order info response error: %w", err)
	}

	if resp.Result != "success" || len(resp.Orders) == 0 {
		return nil, fmt.Errorf("get order info failed: %s", string(respBody))
	}

	return &resp.Orders[0], nil
}

// GetOpenOrders 查询未完成订单
func (c *KrakenClient) GetOpenOrders(ctx context.Context) ([]OrderInfo, error) {
	path := "/derivatives/api/v3/openorders"
	respBody, err := c.sendRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Result     string      `json:"result"`
		OpenOrders []OrderInfo `json:"openOrders"`
	}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal get open orders response error: %w", err)
	}

	if resp.Result != "success" {
		return nil, fmt.Errorf("get open orders failed: %s", string(respBody))
	}

	return resp.OpenOrders, nil
}

// GetAccountInfo 获取账户信息
func (c *KrakenClient) GetAccountInfo(ctx context.Context) (*AccountInfo, error) {
	path := "/derivatives/api/v3/accounts"
	respBody, err := c.sendRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Result   string        `json:"result"`
		Accounts []AccountInfo `json:"accounts"`
	}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal get account info response error: %w", err)
	}

	if resp.Result != "success" || len(resp.Accounts) == 0 {
		return nil, fmt.Errorf("get account info failed: %s", string(respBody))
	}

	return &resp.Accounts[0], nil
}

// GetPositionInfo 获取持仓信息
func (c *KrakenClient) GetPositionInfo(ctx context.Context) ([]KrakenPositionInfo, error) {
	path := "/derivatives/api/v3/openpositions"
	respBody, err := c.sendRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Result        string               `json:"result"`
		OpenPositions []KrakenPositionInfo `json:"openPositions"`
	}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal get position info response error: %w", err)
	}

	if resp.Result != "success" {
		return nil, fmt.Errorf("get position info failed: %s", string(respBody))
	}

	return resp.OpenPositions, nil
}

// GetFundingRate 获取资金费率
func (c *KrakenClient) GetFundingRate(ctx context.Context, symbol string) (float64, error) {
	path := fmt.Sprintf("/derivatives/api/v3/tickers")
	respBody, err := c.sendRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return 0, err
	}

	var resp struct {
		Result  string `json:"result"`
		Tickers []struct {
			Symbol      string  `json:"symbol"`
			FundingRate float64 `json:"fundingRate"`
		} `json:"tickers"`
	}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return 0, fmt.Errorf("unmarshal funding rate response error: %w", err)
	}

	if resp.Result != "success" {
		return 0, fmt.Errorf("get funding rate failed: %s", string(respBody))
	}

	for _, ticker := range resp.Tickers {
		if ticker.Symbol == symbol {
			return ticker.FundingRate, nil
		}
	}

	return 0, fmt.Errorf("symbol %s not found", symbol)
}

// GetHistoricalKlines 获取历史K线数据
func (c *KrakenClient) GetHistoricalKlines(ctx context.Context, symbol string, resolution string, limit int) ([]Candle, error) {
	// Kraken 使用 resolution: 1m, 5m, 15m, 30m, 1h, 4h, 12h, 1d, 1w
	path := fmt.Sprintf("/api/charts/v1/trade/%s/%s", symbol, resolution)

	// 计算时间范围
	to := time.Now().Unix()
	from := to - int64(limit*getResolutionSeconds(resolution))

	reqURL := fmt.Sprintf("%s%s?from=%d&to=%d", KrakenBaseURL, path, from, to)
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

	var klineResp struct {
		Candles []Candle `json:"candles"`
	}
	if err := json.Unmarshal(respBody, &klineResp); err != nil {
		return nil, fmt.Errorf("unmarshal historical klines response error: %w", err)
	}

	return klineResp.Candles, nil
}

// getResolutionSeconds 获取时间分辨率的秒数
func getResolutionSeconds(resolution string) int {
	switch resolution {
	case "1m":
		return 60
	case "5m":
		return 300
	case "15m":
		return 900
	case "30m":
		return 1800
	case "1h":
		return 3600
	case "4h":
		return 14400
	case "12h":
		return 43200
	case "1d":
		return 86400
	case "1w":
		return 604800
	default:
		return 3600
	}
}

// ExchangeInfo 交易所信息
type ExchangeInfo struct {
	Symbols map[string]ContractInfo
}

// ContractInfo 合约信息
type ContractInfo struct {
	Symbol          string  `json:"symbol"`
	Type            string  `json:"type"`
	Underlying      string  `json:"underlying"`
	TickSize        float64 `json:"tickSize"`
	ContractSize    float64 `json:"contractSize"`
	MaxPositionSize int     `json:"maxPositionSize"`
	ImpactMidSize   int     `json:"impactMidSize"`
	OpeningDate     string  `json:"openingDate"`
	MarginLevels    []struct {
		Contracts         int     `json:"contracts"`
		InitialMargin     float64 `json:"initialMargin"`
		MaintenanceMargin float64 `json:"maintenanceMargin"`
	} `json:"marginLevels"`
	TradingHours           string  `json:"tradingHours"`
	FundingRateCoefficient float64 `json:"fundingRateCoefficient"`
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
	OrderID        string  `json:"order_id"`
	CliOrdId       string  `json:"cliOrdId"`
	Symbol         string  `json:"symbol"`
	Side           string  `json:"side"`
	OrderType      string  `json:"orderType"`
	LimitPrice     float64 `json:"limitPrice"`
	Quantity       int     `json:"quantity"`
	Filled         int     `json:"filled"`
	Timestamp      string  `json:"timestamp"`
	LastUpdateTime string  `json:"lastUpdateTimestamp"`
	ReduceOnly     bool    `json:"reduceOnly"`
}

// AccountInfo 账户信息
type AccountInfo struct {
	Name              string  `json:"name"`
	Type              string  `json:"type"`
	Currency          string  `json:"currency"`
	BalanceValue      float64 `json:"balanceValue"`
	AvailableMargin   float64 `json:"availableMargin"`
	MarginEquity      float64 `json:"marginEquity"`
	PortfolioValue    float64 `json:"portfolioValue"`
	UnrealisedFunding float64 `json:"unrealisedFunding"`
}

// KrakenPositionInfo 持仓信息
type KrakenPositionInfo struct {
	Symbol   string  `json:"symbol"`
	Side     string  `json:"side"` // "long" or "short"
	Size     int     `json:"size"`
	Price    float64 `json:"price"`
	FillTime string  `json:"fillTime"`
	PnL      float64 `json:"pnl"`
}

// Candle K线数据
type Candle struct {
	Time   int64  `json:"time"`
	Open   string `json:"open"`
	High   string `json:"high"`
	Low    string `json:"low"`
	Close  string `json:"close"`
	Volume string `json:"volume"`
}
