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
	"strconv"
	"strings"
	"time"

	"quantmesh/logger"
)

const (
	MEXCMainnetBaseURL = "https://api.mexc.com"              // MEXC 期货 API 主域名
	MEXCTestnetBaseURL = "https://contract-testnet.mexc.com" // MEXC 測試網
)

// MEXCClient MEXC 客戶端
type MEXCClient struct {
	apiKey     string
	secretKey  string
	baseURL    string
	httpClient *http.Client
	isTestnet  bool
}

// MEXCDealDetail is one authenticated contract execution returned for an order.
type MEXCDealDetail struct {
	ID          json.Number `json:"id"`
	Symbol      string      `json:"symbol"`
	Side        int         `json:"side"`
	Volume      json.Number `json:"vol"`
	Price       json.Number `json:"price"`
	Fee         json.Number `json:"fee"`
	FeeCurrency string      `json:"feeCurrency"`
	Timestamp   json.Number `json:"timestamp"`
	Profit      json.Number `json:"profit"`
	IsTaker     bool        `json:"isTaker"`
	OrderID     json.Number `json:"orderId"`
}

// NewMEXCClient 創建 MEXC 客戶端
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

// signOpenAPIRequest signs MEXC's current Open-API request target.
func (c *MEXCClient) signOpenAPIRequest(requestTime, parameterString string) string {
	h := hmac.New(sha256.New, []byte(c.secretKey))
	h.Write([]byte(c.apiKey + requestTime + parameterString))
	return hex.EncodeToString(h.Sum(nil))
}

// sendRequest 发送请求
func (c *MEXCClient) sendRequest(ctx context.Context, method, path string, params url.Values, needSign bool) ([]byte, error) {
	reqURL := c.baseURL + path

	var req *http.Request
	var err error
	var requestBody []byte

	if method == http.MethodGet || method == http.MethodDelete {
		if len(params) > 0 {
			reqURL += "?" + params.Encode()
		}
		req, err = http.NewRequestWithContext(ctx, method, reqURL, nil)
	} else {
		if needSign {
			bodyValues := make(map[string]any, len(params))
			for key := range params {
				bodyValues[key] = mexcJSONBodyValue(key, params.Get(key))
			}
			requestBody, err = json.Marshal(bodyValues)
		} else {
			requestBody = []byte(params.Encode())
		}
		if err == nil {
			req, err = http.NewRequestWithContext(ctx, method, reqURL, strings.NewReader(string(requestBody)))
			if err == nil {
				if needSign {
					req.Header.Set("Content-Type", "application/json")
				} else {
					req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				}
			}
		}
	}

	if err != nil {
		return nil, fmt.Errorf("create request error: %w", err)
	}

	if needSign {
		requestTime := strconv.FormatInt(time.Now().UnixMilli(), 10)
		parameterString := params.Encode()
		if method != http.MethodGet && method != http.MethodDelete {
			parameterString = string(requestBody)
		}
		req.Header.Set("ApiKey", c.apiKey)
		req.Header.Set("Request-Time", requestTime)
		req.Header.Set("Signature", c.signOpenAPIRequest(requestTime, parameterString))
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

	// 检查 API 錯误
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

func mexcJSONBodyValue(key, value string) any {
	switch key {
	case "price", "vol", "side", "type", "openType", "leverage", "orderId":
		if json.Valid([]byte(value)) {
			return json.Number(value)
		}
		return value
	default:
		return value
	}
}

// GetExchangeInfo 獲取交易對信息
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

// PlaceOrder 下單
func (c *MEXCClient) PlaceOrder(ctx context.Context, req *OrderRequest) (*OrderResponse, error) {
	path := "/api/v1/private/order/create"
	params := url.Values{}
	params.Set("symbol", req.Symbol)
	params.Set("price", fmt.Sprintf("%.8f", req.Price))
	params.Set("vol", fmt.Sprintf("%.0f", req.Volume))
	params.Set("side", strconv.Itoa(req.Side))         // 1=开多, 2=平多, 3=开空, 4=平空
	params.Set("type", strconv.Itoa(req.Type))         // 1=限價, 2=市價
	params.Set("openType", strconv.Itoa(req.OpenType)) // 1=逐倉, 2=全倉
	params.Set("leverage", strconv.Itoa(req.Leverage))

	if req.ClientOrderID != "" {
		params.Set("externalOid", req.ClientOrderID)
	}

	respBody, err := c.sendRequest(ctx, http.MethodPost, path, params, true)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Code    int             `json:"code"`
		Data    json.RawMessage `json:"data"`
		Success bool            `json:"success"`
	}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	if !resp.Success {
		return nil, fmt.Errorf("place order failed")
	}
	var result struct {
		OrderID string `json:"orderId"`
	}
	if err := json.Unmarshal(resp.Data, &result); err != nil || strings.TrimSpace(result.OrderID) == "" {
		if err := json.Unmarshal(resp.Data, &result.OrderID); err != nil || strings.TrimSpace(result.OrderID) == "" {
			return nil, fmt.Errorf("MEXC place order response is missing orderId")
		}
	}
	logger.Info("MEXC order placed: %s", result.OrderID)
	return &OrderResponse{OrderID: result.OrderID}, nil
}

// CancelOrder 取消訂單
func (c *MEXCClient) CancelOrder(ctx context.Context, symbol, orderID string) error {
	path := "/api/v1/private/order/cancel"
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("orderId", orderID)

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

// GetOrderInfo 查詢訂單
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

// GetOrderDealDetails reads all authenticated executions belonging to one order.
func (c *MEXCClient) GetOrderDealDetails(ctx context.Context, orderID string) ([]MEXCDealDetail, error) {
	if strings.TrimSpace(orderID) == "" {
		return nil, fmt.Errorf("MEXC order ID is required for deal details")
	}
	path := "/api/v1/private/order/deal_details/" + url.PathEscape(orderID)
	body, err := c.sendRequest(ctx, http.MethodGet, path, url.Values{}, true)
	if err != nil {
		return nil, fmt.Errorf("query MEXC deal details for order %s: %w", orderID, err)
	}
	var response struct {
		Data []MEXCDealDetail `json:"data"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("decode MEXC deal details for order %s: %w", orderID, err)
	}
	return response.Data, nil
}

// GetOpenOrders 獲取活跃订單
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

// VerifyAccountHasNoOpenOrders uses MEXC's account-wide in-flight order count.
// The endpoint includes limit, TP/SL, plan, and trailing orders but does not
// return order details, so it is suitable only for an empty-account check.
func (c *MEXCClient) VerifyAccountHasNoOpenOrders(ctx context.Context) error {
	const path = "/api/v1/private/order/open_order_total_count"
	body, err := c.sendRequest(ctx, http.MethodPost, path, url.Values{}, true)
	if err != nil {
		return fmt.Errorf("query MEXC account-wide open-order counts: %w", err)
	}
	var response struct {
		Code    *int  `json:"code"`
		Success *bool `json:"success"`
		Data    *struct {
			SumCount        *int64 `json:"sumCount"`
			LimitOrderCount *int64 `json:"limitOrderCount"`
			StopOrderCount  *int64 `json:"stopOrderCount"`
			PlanOrderCount  *int64 `json:"planOrderCount"`
			TrackOrderCount *int64 `json:"trackOrderCount"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return fmt.Errorf("decode MEXC account-wide open-order counts: %w", err)
	}
	if response.Code == nil || *response.Code != 0 || response.Success == nil || !*response.Success || response.Data == nil {
		return fmt.Errorf("MEXC account-wide open-order count response is incomplete or unsuccessful")
	}
	counts := []*int64{response.Data.SumCount, response.Data.LimitOrderCount, response.Data.StopOrderCount, response.Data.PlanOrderCount, response.Data.TrackOrderCount}
	for _, count := range counts {
		if count == nil || *count < 0 {
			return fmt.Errorf("MEXC account-wide open-order count response has a missing or invalid count")
		}
	}
	categoryTotal := *response.Data.LimitOrderCount + *response.Data.StopOrderCount + *response.Data.PlanOrderCount + *response.Data.TrackOrderCount
	if categoryTotal != *response.Data.SumCount {
		return fmt.Errorf("MEXC account-wide open-order counts are inconsistent: sum=%d categories=%d", *response.Data.SumCount, categoryTotal)
	}
	if *response.Data.SumCount != 0 {
		return fmt.Errorf("MEXC account has %d open orders (limit=%d stop=%d plan=%d trailing=%d)", *response.Data.SumCount, *response.Data.LimitOrderCount, *response.Data.StopOrderCount, *response.Data.PlanOrderCount, *response.Data.TrackOrderCount)
	}
	return nil
}

// GetAccount 獲取帳戶信息
func (c *MEXCClient) GetAccount(ctx context.Context) ([]AccountInfo, error) {
	path := "/api/v1/private/account/assets"
	params := url.Values{}

	respBody, err := c.sendRequest(ctx, http.MethodGet, path, params, true)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Code    int           `json:"code"`
		Data    []AccountInfo `json:"data"`
		Success bool          `json:"success"`
	}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	if !resp.Success {
		return nil, fmt.Errorf("get account info failed")
	}

	return resp.Data, nil
}

// GetPositions 獲取持倉
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

// GetTicker 獲取行情
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

// GetContractDepth 合約深度快照（公共 GET /api/v1/contract/depth/{symbol}）
func (c *MEXCClient) GetContractDepth(ctx context.Context, symbol string) (bids [][]float64, asks [][]float64, ts int64, err error) {
	path := "/api/v1/contract/depth/" + symbol
	respBody, err := c.sendRequest(ctx, http.MethodGet, path, url.Values{}, false)
	if err != nil {
		return nil, nil, 0, err
	}
	var resp struct {
		Code    int  `json:"code"`
		Success bool `json:"success"`
		Data    struct {
			Bids      [][]float64 `json:"bids"`
			Asks      [][]float64 `json:"asks"`
			Timestamp int64       `json:"timestamp"`
		} `json:"data"`
	}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, nil, 0, fmt.Errorf("unmarshal depth: %w", err)
	}
	if !resp.Success {
		return nil, nil, 0, fmt.Errorf("mexc depth failed: %s", string(respBody))
	}
	return resp.Data.Bids, resp.Data.Asks, resp.Data.Timestamp, nil
}

// GetKlines 獲取 K線數據
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
		Code int `json:"code"`
		Data struct {
			Time  []int64   `json:"time"`
			Open  []float64 `json:"open"`
			Close []float64 `json:"close"`
			High  []float64 `json:"high"`
			Low   []float64 `json:"low"`
			Vol   []float64 `json:"vol"`
		} `json:"data"`
		Success bool `json:"success"`
	}
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	if !resp.Success {
		return nil, fmt.Errorf("get klines failed")
	}

	n := len(resp.Data.Time)
	klines := make([]Kline, 0, n)
	for i := 0; i < n; i++ {
		if i >= len(resp.Data.Open) || i >= len(resp.Data.High) || i >= len(resp.Data.Low) || i >= len(resp.Data.Close) || i >= len(resp.Data.Vol) {
			break
		}
		klines = append(klines, Kline{
			Time:  resp.Data.Time[i] * 1000,
			Open:  resp.Data.Open[i],
			High:  resp.Data.High[i],
			Low:   resp.Data.Low[i],
			Close: resp.Data.Close[i],
			Vol:   resp.Data.Vol[i],
		})
	}
	return klines, nil
}

// 數據結構定义

type ExchangeInfo struct {
	Symbols map[string]ContractDetail
}

type ContractDetail struct {
	Symbol           string  `json:"symbol"`
	DisplayName      string  `json:"displayName"`
	DisplayNameEn    string  `json:"displayNameEn"`
	PositionOpenType int     `json:"positionOpenType"` // 1=單向持倉, 2=双向持倉
	BaseCoin         string  `json:"baseCoin"`
	QuoteCoin        string  `json:"quoteCoin"`
	SettleCoin       string  `json:"settleCoin"`
	ContractSize     float64 `json:"contractSize"`
	MinLeverage      int     `json:"minLeverage"`
	MaxLeverage      int     `json:"maxLeverage"`
	PriceScale       int     `json:"priceScale"`
	VolScale         int     `json:"volScale"`
	AmountScale      int     `json:"amountScale"`
	PriceUnit        float64 `json:"priceUnit"`
	VolUnit          int     `json:"volUnit"`
	MinVol           int     `json:"minVol"`
	MaxVol           int     `json:"maxVol"`
	State            int     `json:"state"` // 0=已下線, 1=已上線
}

type OrderRequest struct {
	Symbol        string
	Price         float64
	Volume        float64
	Side          int // 1=开多, 2=平多, 3=开空, 4=平空
	Type          int // 1=限價, 2=市價
	OpenType      int // 1=逐倉, 2=全倉
	Leverage      int
	ClientOrderID string
}

type OrderResponse struct {
	OrderID string
}

type OrderInfo struct {
	OrderID        string  `json:"orderId"`
	Symbol         string  `json:"symbol"`
	PositionID     int64   `json:"positionId"`
	Price          float64 `json:"price"`
	Vol            float64 `json:"vol"`
	Leverage       int     `json:"leverage"`
	Side           int     `json:"side"`
	Category       int     `json:"category"`
	OrderType      int     `json:"orderType"`
	DealAvgPrice   float64 `json:"dealAvgPrice"`
	DealVol        float64 `json:"dealVol"`
	OrderMargin    float64 `json:"orderMargin"`
	UsedMargin     float64 `json:"usedMargin"`
	TakerFee       float64 `json:"takerFee"`
	MakerFee       float64 `json:"makerFee"`
	Profit         float64 `json:"profit"`
	FeeCurrency    string  `json:"feeCurrency"`
	OpenType       int     `json:"openType"`
	State          int     `json:"state"` // 1=未成交, 2=部分成交, 3=已成交, 4=已撤销, 5=部分成交已撤销
	ExternalOid    string  `json:"externalOid"`
	ErrorCode      int     `json:"errorCode"`
	UsedMarginRate float64 `json:"usedMarginRate"`
	CreateTime     int64   `json:"createTime"`
	UpdateTime     int64   `json:"updateTime"`
}

type AccountInfo struct {
	Currency         string  `json:"currency"`
	PositionMargin   float64 `json:"positionMargin"`
	FrozenBalance    float64 `json:"frozenBalance"`
	AvailableBalance float64 `json:"availableBalance"`
	CashBalance      float64 `json:"cashBalance"`
	Equity           float64 `json:"equity"`
	Unrealized       float64 `json:"unrealized"`
}

type PositionInfo struct {
	PositionID     int64   `json:"positionId"`
	Symbol         string  `json:"symbol"`
	PositionType   int     `json:"positionType"` // 1=多倉, 2=空倉
	OpenType       int     `json:"openType"`     // 1=逐倉, 2=全倉
	State          int     `json:"state"`        // 1=持倉中, 2=系统托管中, 3=已平倉
	HoldVol        float64 `json:"holdVol"`
	FrozenVol      float64 `json:"frozenVol"`
	CloseVol       float64 `json:"closeVol"`
	HoldAvgPrice   float64 `json:"holdAvgPrice"`
	CloseAvgPrice  float64 `json:"closeAvgPrice"`
	OpenAvgPrice   float64 `json:"openAvgPrice"`
	LiquidatePrice float64 `json:"liquidatePrice"`
	Oim            float64 `json:"oim"`
	Adl            int     `json:"adl"`
	Leverage       int     `json:"leverage"`
	UnrealizedPNL  float64 `json:"unrealizedPNL"`
	RealizedPNL    float64 `json:"realizedPNL"`
	CreateTime     int64   `json:"createTime"`
	UpdateTime     int64   `json:"updateTime"`
}

type TickerInfo struct {
	Symbol        string  `json:"symbol"`
	LastPrice     float64 `json:"lastPrice"`
	Bid1          float64 `json:"bid1"`
	Ask1          float64 `json:"ask1"`
	Volume24      float64 `json:"volume24"`
	Amount24      float64 `json:"amount24"`
	HoldVol       float64 `json:"holdVol"`
	Lower24Price  float64 `json:"lower24Price"`
	High24Price   float64 `json:"high24Price"`
	RiseFallRate  float64 `json:"riseFallRate"`
	RiseFallValue float64 `json:"riseFallValue"`
	IndexPrice    float64 `json:"indexPrice"`
	FairPrice     float64 `json:"fairPrice"`
	FundingRate   float64 `json:"fundingRate"`
	MaxBidPrice   float64 `json:"maxBidPrice"`
	MinAskPrice   float64 `json:"minAskPrice"`
	Timestamp     int64   `json:"timestamp"`
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
