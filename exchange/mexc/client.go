package mexc

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
	MEXCMainnetBaseURL = "https://contract.mexc.com"    // MEXC 主网
	MEXCTestnetBaseURL = "https://contract-testnet.mexc.com" // MEXC 测试网
)

// MEXCClient MEXC 客户端
type MEXCClient struct {
	apiKey     string
	secretKey  string
	baseURL    string
	httpClient *http.Client
	isTestnet  bool
}

// NewMEXCClient 创建 MEXC 客户端
func NewMEXCClient(apiKey, secretKey string, isTestnet bool) *MEXCClient {
	baseURL := MEXCMainnetBaseURL
	if isTestnet {
		baseURL = MEXCTestnetBaseURL
	}

	return &MEXCClient{
		apiKey:     apiKey,
		secretKey:  secretKey,
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		isTestnet:  isTestnet,
	}
}

// signRequest MEXC 签名：HMAC-SHA256
func (c *MEXCClient) signRequest(params url.Values) string {
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
func (c *MEXCClient) sendRequest(ctx context.Context, method, path string, params url.Values, needSign bool) ([]byte, error) {
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
	req.Header.Set("X-MEXC-APIKEY", c.apiKey)

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
		if apiResp.Code != 0 && apiResp.Code != 200 {
			return nil, fmt.Errorf("API error %d: %s", apiResp.Code, apiResp.Msg)
		}
	}

	return respBody, nil
}

// GetExchangeInfo 获取交易对信息
func (c *MEXCClient) GetExchangeInfo(ctx context.Context) (*ExchangeInfo, error) {
	path := "/api/v1/contract/detail"
	params := url.Values{}

	respBody, err := c.sendRequest(ctx, http.MethodGet, path, params, false)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Code    int              `json:"code"`
		Data    []ContractDetail `json:"data"`
		Success bool             `json:"success"`
	}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	if !resp.Success {
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
func (c *MEXCClient) PlaceOrder(ctx context.Context, req *OrderRequest) (*OrderResponse, error) {
	path := "/api/v1/private/order/submit"
	params := url.Values{}
	params.Set("symbol", req.Symbol)
	params.Set("price", fmt.Sprintf("%.8f", req.Price))
	params.Set("vol", fmt.Sprintf("%.0f", req.Volume))
	params.Set("side", strconv.Itoa(req.Side))          // 1=开多, 2=平多, 3=开空, 4=平空
	params.Set("type", strconv.Itoa(req.Type))          // 1=限价, 2=市价
	params.Set("openType", strconv.Itoa(req.OpenType))  // 1=逐仓, 2=全仓
	params.Set("leverage", strconv.Itoa(req.Leverage))

	if req.ClientOrderID != "" {
		params.Set("externalOid", req.ClientOrderID)
	}

	respBody, err := c.sendRequest(ctx, http.MethodPost, path, params, true)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Code    int    `json:"code"`
		Data    string `json:"data"` // 订单 ID
		Success bool   `json:"success"`
	}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	if !resp.Success {
		return nil, fmt.Errorf("place order failed")
	}

	logger.Info("MEXC order placed: %s", resp.Data)
	return &OrderResponse{OrderID: resp.Data}, nil
}

// CancelOrder 取消订单
func (c *MEXCClient) CancelOrder(ctx context.Context, symbol, orderID string) error {
	path := "/api/v1/private/order/cancel"
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("order_id", orderID)

	respBody, err := c.sendRequest(ctx, http.MethodPost, path, params, true)
	if err != nil {
		return err
	}

	var resp struct {
		Code    int  `json:"code"`
		Success bool `json:"success"`
	}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return fmt.Errorf("unmarshal error: %w", err)
	}

	if !resp.Success {
		return fmt.Errorf("cancel order failed")
	}

	logger.Info("MEXC order cancelled: %s", orderID)
	return nil
}

// GetOrderInfo 查询订单
func (c *MEXCClient) GetOrderInfo(ctx context.Context, symbol, orderID string) (*OrderInfo, error) {
	path := "/api/v1/private/order/get"
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("order_id", orderID)

	respBody, err := c.sendRequest(ctx, http.MethodGet, path, params, true)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Code    int       `json:"code"`
		Data    OrderInfo `json:"data"`
		Success bool      `json:"success"`
	}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	if !resp.Success {
		return nil, fmt.Errorf("get order info failed")
	}

	return &resp.Data, nil
}

// GetOpenOrders 获取活跃订单
func (c *MEXCClient) GetOpenOrders(ctx context.Context, symbol string) ([]OrderInfo, error) {
	path := "/api/v1/private/order/list/open_orders/" + symbol
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("page_num", "1")
	params.Set("page_size", "100")

	respBody, err := c.sendRequest(ctx, http.MethodGet, path, params, true)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Code    int         `json:"code"`
		Data    []OrderInfo `json:"data"`
		Success bool        `json:"success"`
	}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	if !resp.Success {
		return nil, fmt.Errorf("get open orders failed")
	}

	return resp.Data, nil
}

// GetAccount 获取账户信息
func (c *MEXCClient) GetAccount(ctx context.Context) (*AccountInfo, error) {
	path := "/api/v1/private/account/assets"
	params := url.Values{}

	respBody, err := c.sendRequest(ctx, http.MethodGet, path, params, true)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Code    int         `json:"code"`
		Data    AccountInfo `json:"data"`
		Success bool        `json:"success"`
	}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	if !resp.Success {
		return nil, fmt.Errorf("get account info failed")
	}

	return &resp.Data, nil
}

// GetPositions 获取持仓
func (c *MEXCClient) GetPositions(ctx context.Context, symbol string) ([]PositionInfo, error) {
	path := "/api/v1/private/position/open_positions"
	params := url.Values{}
	if symbol != "" {
		params.Set("symbol", symbol)
	}

	respBody, err := c.sendRequest(ctx, http.MethodGet, path, params, true)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Code    int            `json:"code"`
		Data    []PositionInfo `json:"data"`
		Success bool           `json:"success"`
	}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	if !resp.Success {
		return nil, fmt.Errorf("get positions failed")
	}

	return resp.Data, nil
}

// GetTicker 获取行情
func (c *MEXCClient) GetTicker(ctx context.Context, symbol string) (*TickerInfo, error) {
	path := "/api/v1/contract/ticker"
	params := url.Values{}
	params.Set("symbol", symbol)

	respBody, err := c.sendRequest(ctx, http.MethodGet, path, params, false)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Code    int        `json:"code"`
		Data    TickerInfo `json:"data"`
		Success bool       `json:"success"`
	}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	if !resp.Success {
		return nil, fmt.Errorf("get ticker failed")
	}

	return &resp.Data, nil
}

// GetKlines 获取 K线数据
func (c *MEXCClient) GetKlines(ctx context.Context, symbol, interval string, limit int) ([]Kline, error) {
	path := "/api/v1/contract/kline/" + symbol
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
		Code    int     `json:"code"`
		Data    []Kline `json:"data"`
		Success bool    `json:"success"`
	}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	if !resp.Success {
		return nil, fmt.Errorf("get klines failed")
	}

	return resp.Data, nil
}

// 数据结构定义

type ExchangeInfo struct {
	Symbols map[string]ContractDetail
}

type ContractDetail struct {
	Symbol          string  `json:"symbol"`
	DisplayName     string  `json:"displayName"`
	DisplayNameEn   string  `json:"displayNameEn"`
	PositionOpenType int    `json:"positionOpenType"` // 1=单向持仓, 2=双向持仓
	BaseCoin        string  `json:"baseCoin"`
	QuoteCoin       string  `json:"quoteCoin"`
	SettleCoin      string  `json:"settleCoin"`
	ContractSize    float64 `json:"contractSize"`
	MinLeverage     int     `json:"minLeverage"`
	MaxLeverage     int     `json:"maxLeverage"`
	PriceScale      int     `json:"priceScale"`
	VolScale        int     `json:"volScale"`
	AmountScale     int     `json:"amountScale"`
	PriceUnit       float64 `json:"priceUnit"`
	VolUnit         int     `json:"volUnit"`
	MinVol          int     `json:"minVol"`
	MaxVol          int     `json:"maxVol"`
	State           int     `json:"state"` // 0=已下线, 1=已上线
}

type OrderRequest struct {
	Symbol          string
	Price           float64
	Volume          float64
	Side            int    // 1=开多, 2=平多, 3=开空, 4=平空
	Type            int    // 1=限价, 2=市价
	OpenType        int    // 1=逐仓, 2=全仓
	Leverage        int
	ClientOrderID   string
}

type OrderResponse struct {
	OrderID string
}

type OrderInfo struct {
	OrderID         string  `json:"orderId"`
	Symbol          string  `json:"symbol"`
	PositionID      int64   `json:"positionId"`
	Price           float64 `json:"price"`
	Vol             float64 `json:"vol"`
	Leverage        int     `json:"leverage"`
	Side            int     `json:"side"`
	Category        int     `json:"category"`
	OrderType       int     `json:"orderType"`
	DealAvgPrice    float64 `json:"dealAvgPrice"`
	DealVol         float64 `json:"dealVol"`
	OrderMargin     float64 `json:"orderMargin"`
	UsedMargin      float64 `json:"usedMargin"`
	TakerFee        float64 `json:"takerFee"`
	MakerFee        float64 `json:"makerFee"`
	Profit          float64 `json:"profit"`
	FeeCurrency     string  `json:"feeCurrency"`
	OpenType        int     `json:"openType"`
	State           int     `json:"state"` // 1=未成交, 2=部分成交, 3=已成交, 4=已撤销, 5=部分成交已撤销
	ExternalOid     string  `json:"externalOid"`
	ErrorCode       int     `json:"errorCode"`
	UsedMarginRate  float64 `json:"usedMarginRate"`
	CreateTime      int64   `json:"createTime"`
	UpdateTime      int64   `json:"updateTime"`
}

type AccountInfo struct {
	Currency        string  `json:"currency"`
	PositionMargin  float64 `json:"positionMargin"`
	FrozenBalance   float64 `json:"frozenBalance"`
	AvailableBalance float64 `json:"availableBalance"`
	CashBalance     float64 `json:"cashBalance"`
	Equity          float64 `json:"equity"`
	Unrealized      float64 `json:"unrealized"`
}

type PositionInfo struct {
	PositionID      int64   `json:"positionId"`
	Symbol          string  `json:"symbol"`
	PositionType    int     `json:"positionType"` // 1=多仓, 2=空仓
	OpenType        int     `json:"openType"`     // 1=逐仓, 2=全仓
	State           int     `json:"state"`        // 1=持仓中, 2=系统托管中, 3=已平仓
	HoldVol         float64 `json:"holdVol"`
	FrozenVol       float64 `json:"frozenVol"`
	CloseVol        float64 `json:"closeVol"`
	HoldAvgPrice    float64 `json:"holdAvgPrice"`
	CloseAvgPrice   float64 `json:"closeAvgPrice"`
	OpenAvgPrice    float64 `json:"openAvgPrice"`
	LiquidatePrice  float64 `json:"liquidatePrice"`
	Oim             float64 `json:"oim"`
	Adl             int     `json:"adl"`
	Leverage        int     `json:"leverage"`
	UnrealizedPNL   float64 `json:"unrealizedPNL"`
	RealizedPNL     float64 `json:"realizedPNL"`
	CreateTime      int64   `json:"createTime"`
	UpdateTime      int64   `json:"updateTime"`
}

type TickerInfo struct {
	Symbol          string  `json:"symbol"`
	LastPrice       float64 `json:"lastPrice"`
	Bid1            float64 `json:"bid1"`
	Ask1            float64 `json:"ask1"`
	Volume24        float64 `json:"volume24"`
	Amount24        float64 `json:"amount24"`
	HoldVol         float64 `json:"holdVol"`
	Lower24Price    float64 `json:"lower24Price"`
	High24Price     float64 `json:"high24Price"`
	RiseFallRate    float64 `json:"riseFallRate"`
	RiseFallValue   float64 `json:"riseFallValue"`
	IndexPrice      float64 `json:"indexPrice"`
	FairPrice       float64 `json:"fairPrice"`
	FundingRate     float64 `json:"fundingRate"`
	MaxBidPrice     float64 `json:"maxBidPrice"`
	MinAskPrice     float64 `json:"minAskPrice"`
	Timestamp       int64   `json:"timestamp"`
}

type Kline struct {
	Time   int64   `json:"time"`
	Open   float64 `json:"open"`
	High   float64 `json:"high"`
	Low    float64 `json:"low"`
	Close  float64 `json:"close"`
	Vol    float64 `json:"vol"`
	Amount float64 `json:"amount"`
}

