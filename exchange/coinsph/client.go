package coinsph

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
	// 主网 API 地址
	MainnetRestURL = "https://api.pro.coins.ph"
	// Coins.ph没有测试网
	TestnetRestURL = "https://api.pro.coins.ph"
)

// CoinsphClient Coins.ph REST API 客戶端
type CoinsphClient struct {
	apiKey     string
	secretKey  string
	baseURL    string
	useTestnet bool // Coins.ph没有测试网，但保留此字段以保持接口一致性
	httpClient *http.Client
}

// NewCoinsphClient 創建 Coins.ph 客戶端
func NewCoinsphClient(apiKey, secretKey string, useTestnet bool) *CoinsphClient {
	baseURL := MainnetRestURL
	if useTestnet {
		baseURL = TestnetRestURL
	}

	return &CoinsphClient{
		apiKey:     apiKey,
		secretKey:  secretKey,
		baseURL:    baseURL,
		useTestnet: useTestnet,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// sign 生成签名（HMAC-SHA256，hex编码）
func (c *CoinsphClient) sign(queryStr, bodyStr string) string {
	// 签名格式: queryString + requestBody（排除signature参数）
	totalParams := queryStr + bodyStr

	h := hmac.New(sha256.New, []byte(c.secretKey))
	h.Write([]byte(totalParams))
	return hex.EncodeToString(h.Sum(nil))
}

// request 发送 HTTP 请求
func (c *CoinsphClient) request(ctx context.Context, method, path string, query map[string]string, body map[string]interface{}, signed bool) ([]byte, error) {
	timestamp := time.Now().UnixMilli()
	recvWindow := int64(5000)

	var queryStr string
	var bodyStr string
	var bodyBytes []byte

	// 构建查询字符串
	if query != nil && len(query) > 0 {
		values := url.Values{}
		for k, v := range query {
			values.Add(k, v)
		}
		if signed {
			values.Add("timestamp", strconv.FormatInt(timestamp, 10))
			values.Add("recvWindow", strconv.FormatInt(recvWindow, 10))
		}
		queryStr = values.Encode()
	} else if signed {
		queryStr = fmt.Sprintf("timestamp=%d&recvWindow=%d", timestamp, recvWindow)
	}

	// 对于POST请求，Coins.ph推荐使用query string而不是request body
	// 但为了兼容性，我们同时支持两种方式
	if body != nil {
		// 对于signed请求，将参数放入query string
		if signed && method == "POST" {
			// 将body参数转换为query string
			for k, v := range body {
				if k != "timestamp" && k != "recvWindow" {
					if queryStr != "" {
						queryStr += "&"
					}
					queryStr += fmt.Sprintf("%s=%v", k, v)
				}
			}
		} else {
			// 非signed请求或非POST请求，使用JSON body
			if signed {
				body["timestamp"] = timestamp
				body["recvWindow"] = recvWindow
			}
			var err error
			bodyBytes, err = json.Marshal(body)
			if err != nil {
				return nil, fmt.Errorf("序列化请求体失败: %w", err)
			}
			bodyStr = string(bodyBytes)
		}
	}

	// 生成签名（如果需要）
	signature := ""
	if signed {
		// 对于POST请求，如果使用query string，bodyStr为空
		signature = c.sign(queryStr, bodyStr)
		if queryStr != "" {
			queryStr += "&signature=" + signature
		} else {
			queryStr = "signature=" + signature
		}
	}

	// 构建URL
	fullURL := c.baseURL + path
	if queryStr != "" {
		fullURL += "?" + queryStr
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("創建请求失败: %w", err)
	}

	// 設置请求头
	if bodyBytes != nil && len(bodyBytes) > 0 {
		req.Header.Set("Content-Type", "application/json")
	} else {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if signed {
		req.Header.Set("X-COINS-APIKEY", c.apiKey)
		// 签名已经在query string中，不需要在header中
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("发送请求失败: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		// 尝试解析错误响应
		var errResp struct {
			Code int    `json:"code"`
			Msg  string `json:"msg"`
		}
		if err := json.Unmarshal(respBody, &errResp); err == nil {
			return nil, fmt.Errorf("API 錯误 %d: %s", errResp.Code, errResp.Msg)
		}
		return nil, fmt.Errorf("HTTP 錯误 %d: %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

// ExchangeInfo 交易所信息
type ExchangeInfo struct {
	Timezone   string    `json:"timezone"`
	ServerTime int64     `json:"serverTime"`
	Symbols    []Symbol  `json:"symbols"`
}

// Symbol 交易對信息
type Symbol struct {
	Symbol             string   `json:"symbol"`
	Status             string   `json:"status"`
	BaseAsset          string   `json:"baseAsset"`
	BaseAssetPrecision int      `json:"baseAssetPrecision"`
	QuoteAsset         string   `json:"quoteAsset"`
	QuoteAssetPrecision int      `json:"quoteAssetPrecision"`
	OrderTypes         []string `json:"orderTypes"`
	Filters            []Filter `json:"filters"`
}

// Filter 過濾器
type Filter struct {
	FilterType string `json:"filterType"`
	MinPrice   string `json:"minPrice,omitempty"`
	MaxPrice   string `json:"maxPrice,omitempty"`
	TickSize   string `json:"tickSize,omitempty"`
	MinQty     string `json:"minQty,omitempty"`
	MaxQty     string `json:"maxQty,omitempty"`
	StepSize   string `json:"stepSize,omitempty"`
	MinNotional string `json:"minNotional,omitempty"`
}

// GetExchangeInfo 獲取交易所信息
func (c *CoinsphClient) GetExchangeInfo(ctx context.Context, symbol string) (*ExchangeInfo, error) {
	query := make(map[string]string)
	if symbol != "" {
		query["symbol"] = symbol
	}

	data, err := c.request(ctx, "GET", "/openapi/v1/exchangeInfo", query, nil, false)
	if err != nil {
		return nil, err
	}

	var info ExchangeInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, fmt.Errorf("解析交易所信息失败: %w", err)
	}

	return &info, nil
}

// Account 賬戶信息
type Account struct {
	AccountType      string    `json:"accountType"`
	CanDeposit       bool      `json:"canDeposit"`
	CanTrade         bool      `json:"canTrade"`
	CanWithdraw      bool      `json:"canWithdraw"`
	Email            string    `json:"email"`
	Balances         []Balance `json:"balances"`
	Token            string    `json:"token"`
}

// Balance 餘額信息
type Balance struct {
	Asset  string `json:"asset"`
	Free   string `json:"free"`
	Locked string `json:"locked"`
}

// GetAccount 獲取賬戶信息
func (c *CoinsphClient) GetAccount(ctx context.Context) (*Account, error) {
	data, err := c.request(ctx, "GET", "/openapi/v1/account", nil, nil, true)
	if err != nil {
		return nil, err
	}

	var account Account
	if err := json.Unmarshal(data, &account); err != nil {
		return nil, fmt.Errorf("解析賬戶信息失败: %w", err)
	}

	return &account, nil
}

// NewOrderRequest 新訂單请求
type NewOrderRequest struct {
	Symbol           string  `json:"symbol"`
	Side             string  `json:"side"` // BUY or SELL
	Type             string  `json:"type"` // LIMIT, MARKET, LIMIT_MAKER
	TimeInForce      string  `json:"timeInForce,omitempty"` // GTC, IOC, FOK
	Quantity         string  `json:"quantity,omitempty"`
	QuoteOrderQty    string  `json:"quoteOrderQty,omitempty"`
	Price            string  `json:"price,omitempty"`
	NewClientOrderID string  `json:"newClientOrderId,omitempty"`
	StopPrice        string  `json:"stopPrice,omitempty"`
	NewOrderRespType string  `json:"newOrderRespType,omitempty"` // ACK, RESULT, FULL
	StpFlag          string  `json:"stpFlag,omitempty"`
}

// NewOrderResponse 新訂單响应
type NewOrderResponse struct {
	Symbol            string  `json:"symbol"`
	OrderID           int64   `json:"orderId"`
	ClientOrderID     string  `json:"clientOrderId"`
	TransactTime      int64   `json:"transactTime"`
	Price             string  `json:"price"`
	OrigQty           string  `json:"origQty"`
	ExecutedQty       string  `json:"executedQty"`
	CummulativeQuoteQty string `json:"cummulativeQuoteQty"`
	Status            string  `json:"status"`
	TimeInForce       string  `json:"timeInForce"`
	Type              string  `json:"type"`
	Side              string  `json:"side"`
	StopPrice         string  `json:"stopPrice"`
	OrigQuoteOrderQty string `json:"origQuoteOrderQty"`
	Fills             []Fill  `json:"fills,omitempty"`
}

// Fill 成交記錄
type Fill struct {
	Price           string `json:"price"`
	Qty             string `json:"qty"`
	Commission      string `json:"commission"`
	CommissionAsset string `json:"commissionAsset"`
	TradeID         int64  `json:"tradeId"`
}

// PlaceOrder 下單
func (c *CoinsphClient) PlaceOrder(ctx context.Context, req NewOrderRequest) (*NewOrderResponse, error) {
	body := map[string]interface{}{
		"symbol": req.Symbol,
		"side":   req.Side,
		"type":   req.Type,
	}

	if req.TimeInForce != "" {
		body["timeInForce"] = req.TimeInForce
	}
	if req.Quantity != "" {
		body["quantity"] = req.Quantity
	}
	if req.QuoteOrderQty != "" {
		body["quoteOrderQty"] = req.QuoteOrderQty
	}
	if req.Price != "" {
		body["price"] = req.Price
	}
	if req.NewClientOrderID != "" {
		body["newClientOrderId"] = req.NewClientOrderID
	}
	if req.StopPrice != "" {
		body["stopPrice"] = req.StopPrice
	}
	if req.NewOrderRespType != "" {
		body["newOrderRespType"] = req.NewOrderRespType
	} else {
		// 默认使用FULL
		body["newOrderRespType"] = "FULL"
	}
	if req.StpFlag != "" {
		body["stpFlag"] = req.StpFlag
	}

	data, err := c.request(ctx, "POST", "/openapi/v1/order", nil, body, true)
	if err != nil {
		return nil, err
	}

	var resp NewOrderResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("解析訂單响应失败: %w", err)
	}

	return &resp, nil
}

// CancelOrderRequest 取消訂單请求
type CancelOrderRequest struct {
	Symbol            string `json:"symbol"`
	OrderID           int64  `json:"orderId,omitempty"`
	OrigClientOrderID string `json:"origClientOrderId,omitempty"`
}

// CancelOrderResponse 取消訂單响应
type CancelOrderResponse struct {
	Symbol            string `json:"symbol"`
	OrderID           int64  `json:"orderId"`
	ClientOrderID     string `json:"clientOrderId"`
	Price             string `json:"price"`
	OrigQty           string `json:"origQty"`
	ExecutedQty       string `json:"executedQty"`
	CummulativeQuoteQty string `json:"cummulativeQuoteQty"`
	Status            string `json:"status"`
	TimeInForce       string `json:"timeInForce"`
	Type              string `json:"type"`
	Side              string `json:"side"`
	StopPrice         string `json:"stopPrice"`
	OrigQuoteOrderQty string `json:"origQuoteOrderQty"`
}

// CancelOrder 取消訂單
func (c *CoinsphClient) CancelOrder(ctx context.Context, symbol string, orderID int64, clientOrderID string) (*CancelOrderResponse, error) {
	query := map[string]string{
		"symbol": symbol,
	}

	if clientOrderID != "" {
		query["origClientOrderId"] = clientOrderID
	} else {
		query["orderId"] = strconv.FormatInt(orderID, 10)
	}

	data, err := c.request(ctx, "DELETE", "/openapi/v1/order", query, nil, true)
	if err != nil {
		return nil, err
	}

	var resp CancelOrderResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("解析取消訂單响应失败: %w", err)
	}

	return &resp, nil
}

// CancelAllOrders 取消所有訂單
func (c *CoinsphClient) CancelAllOrders(ctx context.Context, symbol string) ([]CancelOrderResponse, error) {
	query := map[string]string{
		"symbol": symbol,
	}

	data, err := c.request(ctx, "DELETE", "/openapi/v1/openOrders", query, nil, true)
	if err != nil {
		return nil, err
	}

	var resp []CancelOrderResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("解析取消訂單响应失败: %w", err)
	}

	return resp, nil
}

// OrderInfo 訂單信息
type OrderInfo struct {
	ClientOrderID       string `json:"clientOrderId"`
	CummulativeQuoteQty string `json:"cummulativeQuoteQty"`
	ExecutedQty         string `json:"executedQty"`
	IsWorking           bool   `json:"isWorking"`
	OrderID             int64  `json:"orderId"`
	OrigQty             string `json:"origQty"`
	OrigQuoteOrderQty   string `json:"origQuoteOrderQty"`
	Price               string `json:"price"`
	Side                string `json:"side"`
	Status              string `json:"status"`
	StopPrice           string `json:"stopPrice"`
	Symbol              string `json:"symbol"`
	Time                int64  `json:"time"`
	TimeInForce         string `json:"timeInForce"`
	Type                string `json:"type"`
	UpdateTime          int64  `json:"updateTime"`
}

// GetOrder 查詢訂單
func (c *CoinsphClient) GetOrder(ctx context.Context, symbol string, orderID int64, clientOrderID string) (*OrderInfo, error) {
	query := map[string]string{
		"symbol": symbol,
	}

	if clientOrderID != "" {
		query["origClientOrderId"] = clientOrderID
	} else {
		query["orderId"] = strconv.FormatInt(orderID, 10)
	}

	data, err := c.request(ctx, "GET", "/openapi/v1/order", query, nil, true)
	if err != nil {
		return nil, err
	}

	// 可能是單個訂單或訂單列表
	var singleOrder OrderInfo
	if err := json.Unmarshal(data, &singleOrder); err == nil {
		return &singleOrder, nil
	}

	var orders []OrderInfo
	if err := json.Unmarshal(data, &orders); err == nil && len(orders) > 0 {
		return &orders[0], nil
	}

	return nil, fmt.Errorf("解析訂單信息失败")
}

// GetOpenOrders 查詢未完成訂單
func (c *CoinsphClient) GetOpenOrders(ctx context.Context, symbol string) ([]OrderInfo, error) {
	query := make(map[string]string)
	if symbol != "" {
		query["symbol"] = symbol
	}

	data, err := c.request(ctx, "GET", "/openapi/v1/openOrders", query, nil, true)
	if err != nil {
		return nil, err
	}

	var orders []OrderInfo
	if err := json.Unmarshal(data, &orders); err != nil {
		return nil, fmt.Errorf("解析未完成訂單失败: %w", err)
	}

	return orders, nil
}

// Ticker24hr 24小时行情
type Ticker24hr struct {
	Symbol             string `json:"symbol"`
	PriceChange         string `json:"priceChange"`
	PriceChangePercent string `json:"priceChangePercent"`
	WeightedAvgPrice   string `json:"weightedAvgPrice"`
	PrevClosePrice     string `json:"prevClosePrice"`
	LastPrice          string `json:"lastPrice"`
	LastQty            string `json:"lastQty"`
	BidPrice           string `json:"bidPrice"`
	BidQty             string `json:"bidQty"`
	AskPrice           string `json:"askPrice"`
	AskQty             string `json:"askQty"`
	OpenPrice          string `json:"openPrice"`
	HighPrice          string `json:"highPrice"`
	LowPrice           string `json:"lowPrice"`
	Volume             string `json:"volume"`
	QuoteVolume        string `json:"quoteVolume"`
	OpenTime           int64  `json:"openTime"`
	CloseTime          int64  `json:"closeTime"`
	FirstID            int64  `json:"firstId"`
	LastID             int64  `json:"lastId"`
	Count              int64  `json:"count"`
}

// GetTicker24hr 獲取24小时行情
func (c *CoinsphClient) GetTicker24hr(ctx context.Context, symbol string) (*Ticker24hr, error) {
	query := make(map[string]string)
	if symbol != "" {
		query["symbol"] = symbol
	}

	data, err := c.request(ctx, "GET", "/openapi/quote/v1/ticker/24hr", query, nil, false)
	if err != nil {
		return nil, err
	}

	// 可能是單個ticker或ticker列表
	var singleTicker Ticker24hr
	if err := json.Unmarshal(data, &singleTicker); err == nil {
		return &singleTicker, nil
	}

	var tickers []Ticker24hr
	if err := json.Unmarshal(data, &tickers); err == nil && len(tickers) > 0 {
		return &tickers[0], nil
	}

	return nil, fmt.Errorf("解析行情數據失败")
}

// PriceTicker 價格行情
type PriceTicker struct {
	Symbol string `json:"symbol"`
	Price  string `json:"price"`
}

// GetPrice 獲取最新價格
func (c *CoinsphClient) GetPrice(ctx context.Context, symbol string) (*PriceTicker, error) {
	query := make(map[string]string)
	if symbol != "" {
		query["symbol"] = symbol
	}

	data, err := c.request(ctx, "GET", "/openapi/quote/v1/ticker/price", query, nil, false)
	if err != nil {
		return nil, err
	}

	// 可能是單個價格或價格列表
	var singlePrice PriceTicker
	if err := json.Unmarshal(data, &singlePrice); err == nil {
		return &singlePrice, nil
	}

	var prices []PriceTicker
	if err := json.Unmarshal(data, &prices); err == nil && len(prices) > 0 {
		return &prices[0], nil
	}

	return nil, fmt.Errorf("解析價格數據失败")
}

// Trade 成交記錄
type Trade struct {
	ID           int64  `json:"id"`
	OrderID      int64  `json:"orderId"`
	Price        string `json:"price"`
	Qty          string `json:"qty"`
	QuoteQty     string `json:"quoteQty"`
	Commission   string `json:"commission"`
	CommissionAsset string `json:"commissionAsset"`
	Time         int64  `json:"time"`
	IsBuyer      bool   `json:"isBuyer"`
	IsMaker      bool   `json:"isMaker"`
	IsBestMatch  bool   `json:"isBestMatch"`
}

// GetMyTrades 獲取賬戶成交記錄
func (c *CoinsphClient) GetMyTrades(ctx context.Context, symbol string, orderID int64, limit int) ([]Trade, error) {
	query := map[string]string{
		"symbol": symbol,
	}

	if orderID > 0 {
		query["orderId"] = strconv.FormatInt(orderID, 10)
	}
	if limit > 0 {
		query["limit"] = strconv.Itoa(limit)
	}

	data, err := c.request(ctx, "GET", "/openapi/v1/myTrades", query, nil, true)
	if err != nil {
		return nil, err
	}

	var trades []Trade
	if err := json.Unmarshal(data, &trades); err != nil {
		return nil, fmt.Errorf("解析成交記錄失败: %w", err)
	}

	return trades, nil
}

// UserDataStream 用戶數據流
type UserDataStream struct {
	ListenKey string `json:"listenKey"`
}

// StartUserDataStream 啟動用戶數據流
func (c *CoinsphClient) StartUserDataStream(ctx context.Context) (string, error) {
	data, err := c.request(ctx, "POST", "/openapi/v1/userDataStream", nil, nil, false)
	if err != nil {
		return "", err
	}

	var stream UserDataStream
	if err := json.Unmarshal(data, &stream); err != nil {
		return "", fmt.Errorf("解析用戶數據流失败: %w", err)
	}

	return stream.ListenKey, nil
}

// KeepaliveUserDataStream 保持用戶數據流活躍
func (c *CoinsphClient) KeepaliveUserDataStream(ctx context.Context, listenKey string) error {
	query := map[string]string{
		"listenKey": listenKey,
	}

	_, err := c.request(ctx, "PUT", "/openapi/v1/userDataStream", query, nil, false)
	return err
}

// CloseUserDataStream 關閉用戶數據流
func (c *CoinsphClient) CloseUserDataStream(ctx context.Context, listenKey string) error {
	query := map[string]string{
		"listenKey": listenKey,
	}

	_, err := c.request(ctx, "DELETE", "/openapi/v1/userDataStream", query, nil, false)
	return err
}

func init() {
	logger.Info("📦 [Coins.ph Client] REST API 客戶端已初始化")
}
