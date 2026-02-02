package bybit

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
	"time"

	"quantmesh/logger"
)

const (
	// 主网 API 地址
	MainnetRestURL = "https://api.bybit.com"
	// 測試網 API 地址
	TestnetRestURL = "https://api-testnet.bybit.com"
)

// BybitClient Bybit REST API 客戶端
type BybitClient struct {
	apiKey     string
	secretKey  string
	baseURL    string
	httpClient *http.Client
}

// NewBybitClient 創建 Bybit 客戶端
func NewBybitClient(apiKey, secretKey string, useTestnet bool) *BybitClient {
	baseURL := MainnetRestURL
	if useTestnet {
		baseURL = TestnetRestURL
	}

	return &BybitClient{
		apiKey:    apiKey,
		secretKey: secretKey,
		baseURL:   baseURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// sign 生成签名
func (c *BybitClient) sign(params string) string {
	h := hmac.New(sha256.New, []byte(c.secretKey))
	h.Write([]byte(params))
	return hex.EncodeToString(h.Sum(nil))
}

// request 发送 HTTP 请求
func (c *BybitClient) request(ctx context.Context, method, path string, params map[string]interface{}) ([]byte, error) {
	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
	recvWindow := "5000"

	var queryString string
	var bodyBytes []byte

	if method == "GET" {
		// GET 请求：参數放在 URL 中
		values := url.Values{}
		for k, v := range params {
			values.Add(k, fmt.Sprintf("%v", v))
		}
		queryString = values.Encode()
	} else {
		// POST 请求：参數放在 body 中
		if params != nil {
			var err error
			bodyBytes, err = json.Marshal(params)
			if err != nil {
				return nil, fmt.Errorf("序列化请求体失败: %w", err)
			}
		}
	}

	// 生成签名字符串
	signStr := timestamp + c.apiKey + recvWindow
	if method == "GET" && queryString != "" {
		signStr += queryString
	} else if len(bodyBytes) > 0 {
		signStr += string(bodyBytes)
	}

	signature := c.sign(signStr)

	// 構造 URL
	fullURL := c.baseURL + path
	if queryString != "" {
		fullURL += "?" + queryString
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("創建请求失败: %w", err)
	}

	// 設置请求头
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-BAPI-API-KEY", c.apiKey)
	req.Header.Set("X-BAPI-SIGN", signature)
	req.Header.Set("X-BAPI-TIMESTAMP", timestamp)
	req.Header.Set("X-BAPI-RECV-WINDOW", recvWindow)

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
		return nil, fmt.Errorf("HTTP 錯误 %d: %s", resp.StatusCode, string(respBody))
	}

	// 检查 Bybit API 响应
	var apiResp struct {
		RetCode int             `json:"retCode"`
		RetMsg  string          `json:"retMsg"`
		Result  json.RawMessage `json:"result"`
	}

	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	if apiResp.RetCode != 0 {
		return nil, fmt.Errorf("API 錯误 %d: %s", apiResp.RetCode, apiResp.RetMsg)
	}

	return apiResp.Result, nil
}

// Instrument 合約信息
type Instrument struct {
	Symbol        string        `json:"symbol"`
	BaseCoin      string        `json:"baseCoin"`
	QuoteCoin     string        `json:"quoteCoin"`
	PriceFilter   PriceFilter   `json:"priceFilter"`
	LotSizeFilter LotSizeFilter `json:"lotSizeFilter"`
}

type PriceFilter struct {
	TickSize string `json:"tickSize"`
}

type LotSizeFilter struct {
	QtyStep string `json:"qtyStep"`
}

// GetInstruments 獲取合約信息
func (c *BybitClient) GetInstruments(ctx context.Context, category, symbol string) ([]Instrument, error) {
	params := map[string]interface{}{
		"category": category,
	}
	if symbol != "" {
		params["symbol"] = symbol
	}

	data, err := c.request(ctx, "GET", "/v5/market/instruments-info", params)
	if err != nil {
		return nil, err
	}

	var result struct {
		List []Instrument `json:"list"`
	}

	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("解析合約信息失败: %w", err)
	}

	return result.List, nil
}

// PlaceOrderResult 下單結果
type PlaceOrderResult struct {
	OrderId     string `json:"orderId"`
	OrderLinkId string `json:"orderLinkId"`
}

// PlaceOrder 下單
func (c *BybitClient) PlaceOrder(ctx context.Context, params map[string]interface{}) (*PlaceOrderResult, error) {
	data, err := c.request(ctx, "POST", "/v5/order/create", params)
	if err != nil {
		return nil, err
	}

	var result PlaceOrderResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("解析下單結果失败: %w", err)
	}

	return &result, nil
}

// CancelOrder 取消訂單
func (c *BybitClient) CancelOrder(ctx context.Context, category, symbol, orderId, orderLinkId string) error {
	params := map[string]interface{}{
		"category": category,
		"symbol":   symbol,
	}

	if orderId != "" {
		params["orderId"] = orderId
	}
	if orderLinkId != "" {
		params["orderLinkId"] = orderLinkId
	}

	_, err := c.request(ctx, "POST", "/v5/order/cancel", params)
	return err
}

// BybitOrder 订單信息
type BybitOrder struct {
	OrderId     string `json:"orderId"`
	OrderLinkId string `json:"orderLinkId"`
	Symbol      string `json:"symbol"`
	Side        string `json:"side"`
	OrderType   string `json:"orderType"`
	Price       string `json:"price"`
	Qty         string `json:"qty"`
	CumExecQty  string `json:"cumExecQty"`
	AvgPrice    string `json:"avgPrice"`
	OrderStatus string `json:"orderStatus"`
	UpdatedTime string `json:"updatedTime"`
}

// GetOrder 查詢訂單
func (c *BybitClient) GetOrder(ctx context.Context, category, symbol, orderId, orderLinkId string) (*BybitOrder, error) {
	params := map[string]interface{}{
		"category": category,
		"symbol":   symbol,
	}

	if orderId != "" {
		params["orderId"] = orderId
	}
	if orderLinkId != "" {
		params["orderLinkId"] = orderLinkId
	}

	data, err := c.request(ctx, "GET", "/v5/order/realtime", params)
	if err != nil {
		return nil, err
	}

	var result struct {
		List []BybitOrder `json:"list"`
	}

	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("解析订單信息失败: %w", err)
	}

	if len(result.List) == 0 {
		return nil, fmt.Errorf("订單不存在")
	}

	return &result.List[0], nil
}

// GetOpenOrders 查詢未完成订單
func (c *BybitClient) GetOpenOrders(ctx context.Context, category, symbol string) ([]BybitOrder, error) {
	params := map[string]interface{}{
		"category": category,
	}
	if symbol != "" {
		params["symbol"] = symbol
	}

	data, err := c.request(ctx, "GET", "/v5/order/realtime", params)
	if err != nil {
		return nil, err
	}

	var result struct {
		List []BybitOrder `json:"list"`
	}

	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("解析订單列表失败: %w", err)
	}

	return result.List, nil
}

// Balance 账戶餘額
type Balance struct {
	TotalEquity           string        `json:"totalEquity"`
	TotalAvailableBalance string        `json:"totalAvailableBalance"`
	TotalMarginBalance    string        `json:"totalMarginBalance"`
	Coin                  []BalanceCoin `json:"coin"` // SPOT 账戶時有值
}

// BalanceCoin 單幣種餘額（SPOT 账戶）
type BalanceCoin struct {
	Coin                string `json:"coin"`
	WalletBalance       string `json:"walletBalance"`
	AvailableToWithdraw string `json:"availableToWithdraw"`
}

// GetBalance 獲取帳戶餘額
func (c *BybitClient) GetBalance(ctx context.Context, accountType string) ([]Balance, error) {
	params := map[string]interface{}{
		"accountType": accountType,
	}

	data, err := c.request(ctx, "GET", "/v5/account/wallet-balance", params)
	if err != nil {
		return nil, err
	}

	var result struct {
		List []Balance `json:"list"`
	}

	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("解析餘額信息失败: %w", err)
	}

	return result.List, nil
}

// BybitPosition 持倉資訊
type BybitPosition struct {
	Symbol        string `json:"symbol"`
	Size          string `json:"size"`
	AvgPrice      string `json:"avgPrice"`
	MarkPrice     string `json:"markPrice"`
	UnrealisedPnl string `json:"unrealisedPnl"`
	Leverage      string `json:"leverage"`
	TradeMode     string `json:"tradeMode"`
}

// GetPositions 獲取持倉信息
func (c *BybitClient) GetPositions(ctx context.Context, category, symbol string) ([]BybitPosition, error) {
	params := map[string]interface{}{
		"category": category,
	}
	if symbol != "" {
		params["symbol"] = symbol
	}

	data, err := c.request(ctx, "GET", "/v5/position/list", params)
	if err != nil {
		return nil, err
	}

	var result struct {
		List []BybitPosition `json:"list"`
	}

	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("解析持倉資訊失败: %w", err)
	}

	return result.List, nil
}

// BybitExecution 訂單執行記錄
type BybitExecution struct {
	OrderId     string `json:"orderId"`
	OrderLinkId string `json:"orderLinkId"`
	Symbol      string `json:"symbol"`
	Side        string `json:"side"`
	ExecPrice   string `json:"execPrice"`
	ExecQty     string `json:"execQty"`
	ExecValue   string `json:"execValue"`
	ExecFee     string `json:"execFee"`     // 手續費
	FeeRate     string `json:"feeRate"`     // 費率
	FeeCurrency string `json:"feeCurrency"` // 手續費幣種
	IsMaker     bool   `json:"isMaker"`     // 是否為 Maker
	ExecTime    string `json:"execTime"`    // 執行時間（毫秒）
	TradeId     string `json:"tradeId"`     // 成交ID
}

// GetOrderFills 查詢訂單成交記錄
func (c *BybitClient) GetOrderFills(ctx context.Context, category, symbol string, orderId string) ([]BybitExecution, error) {
	params := map[string]interface{}{
		"category": category,
	}
	if symbol != "" {
		params["symbol"] = symbol
	}
	if orderId != "" {
		params["orderId"] = orderId
	}

	data, err := c.request(ctx, "GET", "/v5/execution/list", params)
	if err != nil {
		return nil, err
	}

	var result struct {
		List []BybitExecution `json:"list"`
	}

	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("解析執行記錄失败: %w", err)
	}

	return result.List, nil
}

// Kline K線數據
type Kline struct {
	StartTime  string `json:"0"` // 开始時间
	OpenPrice  string `json:"1"` // 开盘價
	HighPrice  string `json:"2"` // 最高價
	LowPrice   string `json:"3"` // 最低價
	ClosePrice string `json:"4"` // 收盘價
	Volume     string `json:"5"` // 成交量
}

// GetKlines 獲取K線數據
func (c *BybitClient) GetKlines(ctx context.Context, category, symbol, interval string, limit int) ([]Kline, error) {
	params := map[string]interface{}{
		"category": category,
		"symbol":   symbol,
		"interval": interval,
	}
	if limit > 0 {
		params["limit"] = limit
	}

	data, err := c.request(ctx, "GET", "/v5/market/kline", params)
	if err != nil {
		return nil, err
	}

	var result struct {
		List [][]string `json:"list"`
	}

	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("解析K線數據失败: %w", err)
	}

	klines := make([]Kline, 0, len(result.List))
	for _, item := range result.List {
		if len(item) < 6 {
			continue
		}

		kline := Kline{
			StartTime:  item[0],
			OpenPrice:  item[1],
			HighPrice:  item[2],
			LowPrice:   item[3],
			ClosePrice: item[4],
			Volume:     item[5],
		}
		klines = append(klines, kline)
	}

	return klines, nil
}

// FundingRate 资金费率
type FundingRate struct {
	Symbol      string `json:"symbol"`
	FundingRate string `json:"fundingRate"`
}

// GetFundingRate 獲取资金费率
func (c *BybitClient) GetFundingRate(ctx context.Context, category, symbol string) (*FundingRate, error) {
	params := map[string]interface{}{
		"category": category,
		"symbol":   symbol,
	}

	data, err := c.request(ctx, "GET", "/v5/market/funding/history", params)
	if err != nil {
		return nil, err
	}

	var result struct {
		List []FundingRate `json:"list"`
	}

	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("解析资金费率失败: %w", err)
	}

	if len(result.List) == 0 {
		return nil, fmt.Errorf("未找到资金费率")
	}

	return &result.List[0], nil
}

// Ticker 行情數據
type Ticker struct {
	Symbol    string `json:"symbol"`
	LastPrice string `json:"lastPrice"`
}

// GetTicker 獲取行情
func (c *BybitClient) GetTicker(ctx context.Context, category, symbol string) (*Ticker, error) {
	params := map[string]interface{}{
		"category": category,
		"symbol":   symbol,
	}

	data, err := c.request(ctx, "GET", "/v5/market/tickers", params)
	if err != nil {
		return nil, err
	}

	var result struct {
		List []Ticker `json:"list"`
	}

	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("解析行情數據失败: %w", err)
	}

	if len(result.List) == 0 {
		return nil, fmt.Errorf("未找到行情數據")
	}

	return &result.List[0], nil
}

// sortParams 對参數排序（用於签名）
func sortParams(params map[string]interface{}) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var result string
	for _, k := range keys {
		result += fmt.Sprintf("%s=%v&", k, params[k])
	}

	if len(result) > 0 {
		result = result[:len(result)-1]
	}

	return result
}

// BybitOrderBookResponse Bybit 订單簿响应結構
type BybitOrderBookResponse struct {
	Symbol string     `json:"s"`  // 交易對
	Bids   [][]string `json:"b"`  // 買盘 [[價格, 數量], ...]
	Asks   [][]string `json:"a"`  // 賣盘 [[價格, 數量], ...]
	TS     int64      `json:"ts"` // 時间戳（毫秒）
}

// GetOrderBook 獲取訂單簿深度
func (c *BybitClient) GetOrderBook(ctx context.Context, category, symbol string, limit int) (*BybitOrderBookResponse, error) {
	params := map[string]interface{}{
		"category": category,
		"symbol":   symbol,
	}
	if limit > 0 {
		params["limit"] = limit
	}

	data, err := c.request(ctx, "GET", "/v5/market/orderbook", params)
	if err != nil {
		return nil, err
	}

	var result struct {
		Result BybitOrderBookResponse `json:"result"`
	}

	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("解析订單簿數據失败: %w", err)
	}

	return &result.Result, nil
}

func init() {
	logger.Info("📦 [Bybit Client] REST API 客戶端已初始化")
}
