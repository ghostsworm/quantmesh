package kucoin

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"quantmesh/logger"
)

const (
	KuCoinBaseURL = "https://api-futures.kucoin.com" // KuCoin 期货 API
)

// KuCoinClient 结构体
type KuCoinClient struct {
	apiKey     string
	secretKey  string
	passphrase string
	httpClient *http.Client
}

// NewKuCoinClient 创建 KuCoin 客户端实例
func NewKuCoinClient(apiKey, secretKey, passphrase string) *KuCoinClient {
	return &KuCoinClient{
		apiKey:     apiKey,
		secretKey:  secretKey,
		passphrase: passphrase,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// signRequest 对请求进行签名
func (c *KuCoinClient) signRequest(timestamp, method, path, body string) (string, string) {
	// 构造签名字符串：timestamp + method + path + body
	signStr := timestamp + method + path + body

	// HMAC-SHA256 签名
	h := hmac.New(sha256.New, []byte(c.secretKey))
	h.Write([]byte(signStr))
	signature := base64.StdEncoding.EncodeToString(h.Sum(nil))

	// 加密 passphrase
	h2 := hmac.New(sha256.New, []byte(c.secretKey))
	h2.Write([]byte(c.passphrase))
	encryptedPassphrase := base64.StdEncoding.EncodeToString(h2.Sum(nil))

	return signature, encryptedPassphrase
}

// sendRequest 发送 HTTP 请求
func (c *KuCoinClient) sendRequest(ctx context.Context, method, path string, body interface{}) ([]byte, error) {
	reqURL := fmt.Sprintf("%s%s", KuCoinBaseURL, path)

	var bodyBytes []byte
	var err error
	bodyStr := ""

	if body != nil {
		bodyBytes, err = json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request body error: %w", err)
		}
		bodyStr = string(bodyBytes)
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, strings.NewReader(bodyStr))
	if err != nil {
		return nil, fmt.Errorf("create request error: %w", err)
	}

	// 生成时间戳（毫秒）
	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)

	// 签名
	signature, encryptedPassphrase := c.signRequest(timestamp, method, path, bodyStr)

	// 设置请求头
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("KC-API-KEY", c.apiKey)
	req.Header.Set("KC-API-SIGN", signature)
	req.Header.Set("KC-API-TIMESTAMP", timestamp)
	req.Header.Set("KC-API-PASSPHRASE", encryptedPassphrase)
	req.Header.Set("KC-API-KEY-VERSION", "2") // API v2 使用加密的 passphrase

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
		Code string `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.Unmarshal(respBody, &baseResp); err == nil {
		if baseResp.Code != "200000" {
			return nil, fmt.Errorf("API error! Code: %s, Message: %s", baseResp.Code, baseResp.Msg)
		}
	}

	return respBody, nil
}

// GetExchangeInfo 获取合约信息
func (c *KuCoinClient) GetExchangeInfo(ctx context.Context) (*ExchangeInfo, error) {
	path := "/api/v1/contracts/active"
	respBody, err := c.sendRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Code string         `json:"code"`
		Data []ContractInfo `json:"data"`
	}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal exchange info error: %w", err)
	}

	exchangeInfo := &ExchangeInfo{
		Symbols: make(map[string]ContractInfo),
	}
	for _, info := range resp.Data {
		exchangeInfo.Symbols[info.Symbol] = info
	}
	return exchangeInfo, nil
}

// PlaceOrder 下单
func (c *KuCoinClient) PlaceOrder(ctx context.Context, req *OrderRequest) (*OrderResponse, error) {
	path := "/api/v1/orders"
	body := map[string]interface{}{
		"clientOid": req.ClientOrderID,
		"side":      strings.ToLower(string(req.Side)), // "buy" or "sell"
		"symbol":    req.Symbol,
		"type":      strings.ToLower(string(req.Type)), // "limit" or "market"
		"leverage":  req.Leverage,
	}

	if req.Type == "limit" {
		body["price"] = fmt.Sprintf("%.*f", req.PriceDecimals, req.Price)
	}
	body["size"] = int(req.Quantity) // KuCoin 期货的 size 是整数（张数）

	respBody, err := c.sendRequest(ctx, http.MethodPost, path, body)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Code string `json:"code"`
		Data struct {
			OrderID string `json:"orderId"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal place order response error: %w", err)
	}

	return &OrderResponse{OrderID: resp.Data.OrderID}, nil
}

// CancelOrder 取消订单
func (c *KuCoinClient) CancelOrder(ctx context.Context, orderID string) (*CancelOrderResponse, error) {
	path := fmt.Sprintf("/api/v1/orders/%s", orderID)
	respBody, err := c.sendRequest(ctx, http.MethodDelete, path, nil)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Code string `json:"code"`
		Data struct {
			CancelledOrderIDs []string `json:"cancelledOrderIds"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal cancel order response error: %w", err)
	}

	return &CancelOrderResponse{OrderID: orderID}, nil
}

// GetOrderInfo 查询订单
func (c *KuCoinClient) GetOrderInfo(ctx context.Context, orderID string) (*OrderInfo, error) {
	path := fmt.Sprintf("/api/v1/orders/%s", orderID)
	respBody, err := c.sendRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Code string    `json:"code"`
		Data OrderInfo `json:"data"`
	}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal get order info response error: %w", err)
	}

	return &resp.Data, nil
}

// GetOpenOrders 查询未完成订单
func (c *KuCoinClient) GetOpenOrders(ctx context.Context, symbol string) ([]OrderInfo, error) {
	path := "/api/v1/orders?status=active"
	if symbol != "" {
		path += "&symbol=" + symbol
	}

	respBody, err := c.sendRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Code string `json:"code"`
		Data struct {
			Items []OrderInfo `json:"items"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal get open orders response error: %w", err)
	}

	return resp.Data.Items, nil
}

// GetAccountInfo 获取账户信息
func (c *KuCoinClient) GetAccountInfo(ctx context.Context) (*AccountInfo, error) {
	path := "/api/v1/account-overview"
	respBody, err := c.sendRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Code string      `json:"code"`
		Data AccountInfo `json:"data"`
	}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal get account info response error: %w", err)
	}

	return &resp.Data, nil
}

// GetPositionInfo 获取持仓信息
func (c *KuCoinClient) GetPositionInfo(ctx context.Context, symbol string) ([]KuCoinPositionInfo, error) {
	path := "/api/v1/positions"
	if symbol != "" {
		path += "?symbol=" + symbol
	}

	respBody, err := c.sendRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Code string               `json:"code"`
		Data []KuCoinPositionInfo `json:"data"`
	}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal get position info response error: %w", err)
	}

	return resp.Data, nil
}

// GetFundingRate 获取资金费率
func (c *KuCoinClient) GetFundingRate(ctx context.Context, symbol string) (float64, error) {
	path := fmt.Sprintf("/api/v1/funding-rate/%s/current", symbol)
	respBody, err := c.sendRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return 0, err
	}

	var resp struct {
		Code string `json:"code"`
		Data struct {
			Value float64 `json:"value"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return 0, fmt.Errorf("unmarshal funding rate response error: %w", err)
	}

	return resp.Data.Value, nil
}

// GetHistoricalKlines 获取历史K线数据
func (c *KuCoinClient) GetHistoricalKlines(ctx context.Context, symbol string, granularity int, limit int) ([]Candle, error) {
	// granularity: 1, 5, 15, 30, 60, 120, 240, 480, 720, 1440, 10080（分钟）
	path := fmt.Sprintf("/api/v1/kline/query?symbol=%s&granularity=%d", symbol, granularity)
	if limit > 0 {
		// KuCoin 使用时间范围而不是 limit，这里简化处理
		to := time.Now().Unix()
		from := to - int64(granularity*60*limit)
		path += fmt.Sprintf("&from=%d&to=%d", from, to)
	}

	respBody, err := c.sendRequest(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Code string   `json:"code"`
		Data []Candle `json:"data"`
	}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal historical klines response error: %w", err)
	}

	return resp.Data, nil
}

// GetWebSocketToken 获取 WebSocket 连接 token
func (c *KuCoinClient) GetWebSocketToken(ctx context.Context, private bool) (*WebSocketToken, error) {
	path := "/api/v1/bullet-public"
	if private {
		path = "/api/v1/bullet-private"
	}

	respBody, err := c.sendRequest(ctx, http.MethodPost, path, nil)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Code string `json:"code"`
		Data struct {
			Token           string `json:"token"`
			InstanceServers []struct {
				Endpoint     string `json:"endpoint"`
				Encrypt      bool   `json:"encrypt"`
				Protocol     string `json:"protocol"`
				PingInterval int    `json:"pingInterval"`
				PingTimeout  int    `json:"pingTimeout"`
			} `json:"instanceServers"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal websocket token response error: %w", err)
	}

	if len(resp.Data.InstanceServers) == 0 {
		return nil, fmt.Errorf("no websocket instance servers available")
	}

	server := resp.Data.InstanceServers[0]
	wsToken := &WebSocketToken{
		Token:        resp.Data.Token,
		Endpoint:     server.Endpoint,
		PingInterval: server.PingInterval,
		PingTimeout:  server.PingTimeout,
	}

	logger.Info("KuCoin WebSocket token obtained: %s", wsToken.Endpoint)
	return wsToken, nil
}

// ExchangeInfo 交易所信息
type ExchangeInfo struct {
	Symbols map[string]ContractInfo
}

// ContractInfo 合约信息
type ContractInfo struct {
	Symbol             string  `json:"symbol"`
	RootSymbol         string  `json:"rootSymbol"`
	Type               string  `json:"type"`
	FirstOpenDate      int64   `json:"firstOpenDate"`
	BaseCurrency       string  `json:"baseCurrency"`
	QuoteCurrency      string  `json:"quoteCurrency"`
	SettleCurrency     string  `json:"settleCurrency"`
	MaxOrderQty        int     `json:"maxOrderQty"`
	MaxPrice           float64 `json:"maxPrice"`
	LotSize            int     `json:"lotSize"`
	TickSize           float64 `json:"tickSize"`
	IndexPriceTickSize float64 `json:"indexPriceTickSize"`
	Multiplier         float64 `json:"multiplier"`
	InitialMargin      float64 `json:"initialMargin"`
	MaintainMargin     float64 `json:"maintainMargin"`
	MaxRiskLimit       int64   `json:"maxRiskLimit"`
	MinRiskLimit       int64   `json:"minRiskLimit"`
	RiskStep           int64   `json:"riskStep"`
	MakerFeeRate       float64 `json:"makerFeeRate"`
	TakerFeeRate       float64 `json:"takerFeeRate"`
	TakerFixFee        float64 `json:"takerFixFee"`
	MakerFixFee        float64 `json:"makerFixFee"`
	IsDeleverage       bool    `json:"isDeleverage"`
	IsQuanto           bool    `json:"isQuanto"`
	IsInverse          bool    `json:"isInverse"`
	MarkMethod         string  `json:"markMethod"`
	FairMethod         string  `json:"fairMethod"`
	FundingBaseSymbol  string  `json:"fundingBaseSymbol"`
	FundingQuoteSymbol string  `json:"fundingQuoteSymbol"`
	FundingRateSymbol  string  `json:"fundingRateSymbol"`
	IndexSymbol        string  `json:"indexSymbol"`
	SettlementSymbol   string  `json:"settlementSymbol"`
	Status             string  `json:"status"`
}

// OrderRequest 下单请求
type OrderRequest struct {
	ClientOrderID    string
	Symbol           string
	Side             string // "buy" or "sell"
	Type             string // "limit" or "market"
	Price            float64
	Quantity         float64
	Leverage         int
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
	ID             string `json:"id"`
	Symbol         string `json:"symbol"`
	Type           string `json:"type"`
	Side           string `json:"side"`
	Price          string `json:"price"`
	Size           int    `json:"size"`
	Value          string `json:"value"`
	DealValue      string `json:"dealValue"`
	DealSize       int    `json:"dealSize"`
	Stp            string `json:"stp"`
	Stop           string `json:"stop"`
	StopPriceType  string `json:"stopPriceType"`
	StopTriggered  bool   `json:"stopTriggered"`
	StopPrice      string `json:"stopPrice"`
	TimeInForce    string `json:"timeInForce"`
	PostOnly       bool   `json:"postOnly"`
	Hidden         bool   `json:"hidden"`
	Iceberg        bool   `json:"iceberg"`
	Leverage       string `json:"leverage"`
	ForceHold      bool   `json:"forceHold"`
	CloseOrder     bool   `json:"closeOrder"`
	VisibleSize    int    `json:"visibleSize"`
	ClientOid      string `json:"clientOid"`
	Remark         string `json:"remark"`
	Tags           string `json:"tags"`
	IsActive       bool   `json:"isActive"`
	CancelExist    bool   `json:"cancelExist"`
	CreatedAt      int64  `json:"createdAt"`
	UpdatedAt      int64  `json:"updatedAt"`
	EndAt          int64  `json:"endAt"`
	OrderTime      int64  `json:"orderTime"`
	SettleCurrency string `json:"settleCurrency"`
	Status         string `json:"status"`
	FilledValue    string `json:"filledValue"`
	FilledSize     int    `json:"filledSize"`
	ReduceOnly     bool   `json:"reduceOnly"`
}

// AccountInfo 账户信息
type AccountInfo struct {
	AccountEquity    float64 `json:"accountEquity"`
	UnrealisedPNL    float64 `json:"unrealisedPNL"`
	MarginBalance    float64 `json:"marginBalance"`
	PositionMargin   float64 `json:"positionMargin"`
	OrderMargin      float64 `json:"orderMargin"`
	FrozenFunds      float64 `json:"frozenFunds"`
	AvailableBalance float64 `json:"availableBalance"`
	Currency         string  `json:"currency"`
}

// KuCoinPositionInfo 持仓信息
type KuCoinPositionInfo struct {
	ID                string  `json:"id"`
	Symbol            string  `json:"symbol"`
	AutoDeposit       bool    `json:"autoDeposit"`
	MaintMarginReq    float64 `json:"maintMarginReq"`
	RiskLimit         int64   `json:"riskLimit"`
	RealLeverage      float64 `json:"realLeverage"`
	CrossMode         bool    `json:"crossMode"`
	DelevPercentage   float64 `json:"delevPercentage"`
	OpeningTimestamp  int64   `json:"openingTimestamp"`
	CurrentTimestamp  int64   `json:"currentTimestamp"`
	CurrentQty        int     `json:"currentQty"`
	CurrentCost       float64 `json:"currentCost"`
	CurrentComm       float64 `json:"currentComm"`
	UnrealisedCost    float64 `json:"unrealisedCost"`
	RealisedGrossCost float64 `json:"realisedGrossCost"`
	RealisedCost      float64 `json:"realisedCost"`
	IsOpen            bool    `json:"isOpen"`
	MarkPrice         float64 `json:"markPrice"`
	MarkValue         float64 `json:"markValue"`
	PosCost           float64 `json:"posCost"`
	PosCross          float64 `json:"posCross"`
	PosInit           float64 `json:"posInit"`
	PosComm           float64 `json:"posComm"`
	PosLoss           float64 `json:"posLoss"`
	PosMargin         float64 `json:"posMargin"`
	PosMaint          float64 `json:"posMaint"`
	MaintMargin       float64 `json:"maintMargin"`
	RealisedGrossPnl  float64 `json:"realisedGrossPnl"`
	RealisedPnl       float64 `json:"realisedPnl"`
	UnrealisedPnl     float64 `json:"unrealisedPnl"`
	UnrealisedPnlPcnt float64 `json:"unrealisedPnlPcnt"`
	UnrealisedRoePcnt float64 `json:"unrealisedRoePcnt"`
	AvgEntryPrice     float64 `json:"avgEntryPrice"`
	LiquidationPrice  float64 `json:"liquidationPrice"`
	BankruptPrice     float64 `json:"bankruptPrice"`
	SettleCurrency    string  `json:"settleCurrency"`
	MaintainMargin    float64 `json:"maintainMargin"`
	UserId            int64   `json:"userId"`
}

// Candle K线数据
type Candle struct {
	Time   int64   `json:"time"`
	Open   float64 `json:"open"`
	High   float64 `json:"high"`
	Low    float64 `json:"low"`
	Close  float64 `json:"close"`
	Volume float64 `json:"volume"`
}

// WebSocketToken WebSocket 连接 token
type WebSocketToken struct {
	Token        string
	Endpoint     string
	PingInterval int
	PingTimeout  int
}
