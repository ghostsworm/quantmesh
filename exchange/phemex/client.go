package phemex

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
	PhemexMainnetBaseURL = "https://api.phemex.com"         // Phemex 主网
	PhemexTestnetBaseURL = "https://testnet-api.phemex.com" // Phemex 測試網
)

// PhemexClient Phemex 客戶端
type PhemexClient struct {
	apiKey     string
	secretKey  string
	baseURL    string
	httpClient *http.Client
	isTestnet  bool
}

// NewPhemexClient 創建 Phemex 客戶端
func NewPhemexClient(apiKey, secretKey string, isTestnet bool) *PhemexClient {
	baseURL := PhemexMainnetBaseURL
	if isTestnet {
		baseURL = PhemexTestnetBaseURL
	}

	return &PhemexClient{
		apiKey:     apiKey,
		secretKey:  secretKey,
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		isTestnet:  isTestnet,
	}
}

// signRequest Phemex 签名：HMAC-SHA256
// 签名字符串：path + queryString + expiry + body
func (c *PhemexClient) signRequest(path, queryString, expiry, body string) string {
	message := path + queryString + expiry + body
	h := hmac.New(sha256.New, []byte(c.secretKey))
	h.Write([]byte(message))
	return hex.EncodeToString(h.Sum(nil))
}

// sendRequest 发送请求
func (c *PhemexClient) sendRequest(ctx context.Context, method, path string, params url.Values, body interface{}, needSign bool) ([]byte, error) {
	queryString := ""
	if len(params) > 0 {
		queryString = "?" + params.Encode()
	}

	reqURL := c.baseURL + path + queryString

	var reqBody []byte
	var err error
	bodyStr := ""
	if body != nil {
		reqBody, err = json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal body error: %w", err)
		}
		bodyStr = string(reqBody)
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("create request error: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	if needSign {
		expiry := strconv.FormatInt(time.Now().Unix()+60, 10) // 60秒過期時间
		signature := c.signRequest(path, queryString, expiry, bodyStr)

		req.Header.Set("x-phemex-access-token", c.apiKey)
		req.Header.Set("x-phemex-request-expiry", expiry)
		req.Header.Set("x-phemex-request-signature", signature)
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
		var apiResp APIResponse
		if err := json.Unmarshal(respBody, &apiResp); err == nil && apiResp.Code != 0 {
			return nil, fmt.Errorf("API error: code=%d, msg=%s", apiResp.Code, apiResp.Msg)
		}
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

// GetProduct 獲取交易對信息
func (c *PhemexClient) GetProduct(ctx context.Context, symbol string) (*Product, error) {
	path := "/public/products"
	params := url.Values{}

	respBody, err := c.sendRequest(ctx, http.MethodGet, path, params, nil, false)
	if err != nil {
		return nil, err
	}

	var apiResp APIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	if apiResp.Code != 0 {
		return nil, fmt.Errorf("API error: %s", apiResp.Msg)
	}

	var productsResp ProductsResponse
	dataBytes, _ := json.Marshal(apiResp.Data)
	if err := json.Unmarshal(dataBytes, &productsResp); err != nil {
		return nil, fmt.Errorf("unmarshal data error: %w", err)
	}

	// 查找指定交易對
	for _, product := range productsResp.Perpetual {
		if product.Symbol == symbol {
			return &product, nil
		}
	}

	return nil, fmt.Errorf("product not found: %s", symbol)
}

// PlaceOrder 下單
func (c *PhemexClient) PlaceOrder(ctx context.Context, req *OrderRequest) (*Order, error) {
	path := "/g-orders"
	params := url.Values{}

	respBody, err := c.sendRequest(ctx, http.MethodPost, path, params, req, true)
	if err != nil {
		return nil, err
	}

	var apiResp APIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	if apiResp.Code != 0 {
		return nil, fmt.Errorf("API error: %s", apiResp.Msg)
	}

	var orderResp OrderResponse
	dataBytes, _ := json.Marshal(apiResp.Data)
	if err := json.Unmarshal(dataBytes, &orderResp); err != nil {
		return nil, fmt.Errorf("unmarshal data error: %w", err)
	}

	logger.Info("Phemex order placed: %s", orderResp.OrderID)
	return &orderResp.Order, nil
}

// CancelOrder 取消訂單
func (c *PhemexClient) CancelOrder(ctx context.Context, symbol, orderID string) error {
	path := "/g-orders/cancel"
	params := url.Values{}

	body := map[string]interface{}{
		"symbol":  symbol,
		"orderID": orderID,
	}

	respBody, err := c.sendRequest(ctx, http.MethodDelete, path, params, body, true)
	if err != nil {
		return err
	}

	var apiResp APIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return fmt.Errorf("unmarshal error: %w", err)
	}

	if apiResp.Code != 0 {
		return fmt.Errorf("API error: %s", apiResp.Msg)
	}

	logger.Info("Phemex order cancelled: %s", orderID)
	return nil
}

// GetOrder 查詢訂單
func (c *PhemexClient) GetOrder(ctx context.Context, symbol, orderID string) (*Order, error) {
	path := "/exchange/order"
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("orderID", orderID)

	respBody, err := c.sendRequest(ctx, http.MethodGet, path, params, nil, true)
	if err != nil {
		return nil, err
	}

	var apiResp APIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	if apiResp.Code != 0 {
		return nil, fmt.Errorf("API error: %s", apiResp.Msg)
	}

	var order Order
	dataBytes, _ := json.Marshal(apiResp.Data)
	if err := json.Unmarshal(dataBytes, &order); err != nil {
		return nil, fmt.Errorf("unmarshal data error: %w", err)
	}

	return &order, nil
}

// GetOpenOrders 獲取活跃订單
func (c *PhemexClient) GetOpenOrders(ctx context.Context, symbol string) ([]Order, error) {
	path := "/g-orders/activeList"
	params := url.Values{}
	params.Set("symbol", symbol)

	respBody, err := c.sendRequest(ctx, http.MethodGet, path, params, nil, true)
	if err != nil {
		return nil, err
	}

	var apiResp APIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	if apiResp.Code != 0 {
		return nil, fmt.Errorf("API error: %s", apiResp.Msg)
	}

	var ordersResp OrdersResponse
	dataBytes, _ := json.Marshal(apiResp.Data)
	if err := json.Unmarshal(dataBytes, &ordersResp); err != nil {
		return nil, fmt.Errorf("unmarshal data error: %w", err)
	}

	return ordersResp.Rows, nil
}

// GetPosition 獲取持倉
func (c *PhemexClient) GetPosition(ctx context.Context, symbol string) (*Position, error) {
	path := "/g-accounts/accountPositions"
	params := url.Values{}
	params.Set("currency", "BTC") // Phemex 按币种查詢

	respBody, err := c.sendRequest(ctx, http.MethodGet, path, params, nil, true)
	if err != nil {
		return nil, err
	}

	var apiResp APIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	if apiResp.Code != 0 {
		return nil, fmt.Errorf("API error: %s", apiResp.Msg)
	}

	var positionsResp PositionsResponse
	dataBytes, _ := json.Marshal(apiResp.Data)
	if err := json.Unmarshal(dataBytes, &positionsResp); err != nil {
		return nil, fmt.Errorf("unmarshal data error: %w", err)
	}

	// 查找指定交易對的持倉
	for _, pos := range positionsResp.Positions {
		if pos.Symbol == symbol {
			return &pos, nil
		}
	}

	// 没有持倉返回空持倉
	return &Position{Symbol: symbol, Size: 0}, nil
}

// GetAccount 獲取帳戶信息
func (c *PhemexClient) GetAccount(ctx context.Context, currency string) (*Account, error) {
	path := "/g-accounts/accountPositions"
	params := url.Values{}
	params.Set("currency", currency)

	respBody, err := c.sendRequest(ctx, http.MethodGet, path, params, nil, true)
	if err != nil {
		return nil, err
	}

	var apiResp APIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	if apiResp.Code != 0 {
		return nil, fmt.Errorf("API error: %s", apiResp.Msg)
	}

	var positionsResp PositionsResponse
	dataBytes, _ := json.Marshal(apiResp.Data)
	if err := json.Unmarshal(dataBytes, &positionsResp); err != nil {
		return nil, fmt.Errorf("unmarshal data error: %w", err)
	}

	return &positionsResp.Account, nil
}

// GetTrades 獲取最新成交
func (c *PhemexClient) GetTrades(ctx context.Context, symbol string) ([]Trade, error) {
	path := "/md/trade"
	params := url.Values{}
	params.Set("symbol", symbol)

	respBody, err := c.sendRequest(ctx, http.MethodGet, path, params, nil, false)
	if err != nil {
		return nil, err
	}

	var apiResp APIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	if apiResp.Code != 0 {
		return nil, fmt.Errorf("API error: %s", apiResp.Msg)
	}

	var tradesResp TradesResponse
	dataBytes, _ := json.Marshal(apiResp.Data)
	if err := json.Unmarshal(dataBytes, &tradesResp); err != nil {
		return nil, fmt.Errorf("unmarshal data error: %w", err)
	}

	return tradesResp.Trades, nil
}

// GetKlines 獲取 K線數據
func (c *PhemexClient) GetKlines(ctx context.Context, symbol string, resolution int, limit int) ([]Kline, error) {
	path := "/exchange/public/md/v2/kline/list"
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("resolution", strconv.Itoa(resolution))
	params.Set("limit", strconv.Itoa(limit))

	respBody, err := c.sendRequest(ctx, http.MethodGet, path, params, nil, false)
	if err != nil {
		return nil, err
	}

	var apiResp APIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	if apiResp.Code != 0 {
		return nil, fmt.Errorf("API error: %s", apiResp.Msg)
	}

	var klinesResp KlinesResponse
	dataBytes, _ := json.Marshal(apiResp.Data)
	if err := json.Unmarshal(dataBytes, &klinesResp); err != nil {
		return nil, fmt.Errorf("unmarshal data error: %w", err)
	}

	return klinesResp.Rows, nil
}

// 數據結構定义

type APIResponse struct {
	Code int         `json:"code"`
	Msg  string      `json:"msg"`
	Data interface{} `json:"data"`
}

type ProductsResponse struct {
	Perpetual []Product `json:"perpProductsV2"`
}

type Product struct {
	Symbol         string  `json:"symbol"`
	DisplaySymbol  string  `json:"displaySymbol"`
	QuoteCurrency  string  `json:"quoteCurrency"`
	SettleCurrency string  `json:"settleCurrency"`
	PriceScale     int     `json:"priceScale"`   // 價格缩放因子
	RatioScale     int     `json:"ratioScale"`   // 比率缩放因子
	QtyStepSize    int64   `json:"qtyStepSize"`  // 數量步长
	MinPriceEp     int64   `json:"minPriceEp"`   // 最小價格
	MaxPriceEp     int64   `json:"maxPriceEp"`   // 最大價格
	MaxOrderQty    int64   `json:"maxOrderQty"`  // 最大订單數量
	LotSize        int64   `json:"lotSize"`      // 最小訂單數量
	TickSize       int64   `json:"tickSize"`     // 價格步长
	ContractSize   float64 `json:"contractSize"` // 合約大小
}

type OrderRequest struct {
	Symbol         string `json:"symbol"`
	ClOrdID        string `json:"clOrdID,omitempty"`
	Side           string `json:"side"`                  // Buy, Sell
	OrderQty       int64  `json:"orderQty"`              // 數量（整數）
	PriceEp        int64  `json:"priceEp"`               // 價格（整數，需要乘以 priceScale）
	OrdType        string `json:"ordType"`               // Limit, Market
	TimeInForce    string `json:"timeInForce,omitempty"` // GoodTillCancel, ImmediateOrCancel
	ReduceOnly     bool   `json:"reduceOnly,omitempty"`
	CloseOnTrigger bool   `json:"closeOnTrigger,omitempty"`
}

type OrderResponse struct {
	OrderID string `json:"orderID"`
	Order   Order  `json:"order"`
}

type Order struct {
	OrderID        string `json:"orderID"`
	ClOrdID        string `json:"clOrdID"`
	Symbol         string `json:"symbol"`
	Side           string `json:"side"`
	OrderQty       int64  `json:"orderQty"`
	PriceEp        int64  `json:"priceEp"`
	OrdType        string `json:"ordType"`
	OrdStatus      string `json:"ordStatus"`
	CumQty         int64  `json:"cumQty"`
	LeavesQty      int64  `json:"leavesQty"`
	AvgPriceEp     int64  `json:"avgPriceEp"`
	CreateTimeNs   int64  `json:"createTimeNs"`
	TransactTimeNs int64  `json:"transactTimeNs"`
}

type OrdersResponse struct {
	Rows []Order `json:"rows"`
}

type Position struct {
	AccountID       int64  `json:"accountID"`
	Symbol          string `json:"symbol"`
	Currency        string `json:"currency"`
	Side            string `json:"side"`            // Buy, Sell, None
	Size            int64  `json:"size"`            // 持倉數量
	AvgEntryPriceEp int64  `json:"avgEntryPriceEp"` // 平均入场價格
	MarkPriceEp     int64  `json:"markPriceEp"`     // 標記價格
	UnrealisedPnlEv int64  `json:"unrealisedPnlEv"` // 未實現盈亏
	RealisedPnlEv   int64  `json:"realisedPnlEv"`   // 已實現盈亏
	Leverage        int    `json:"leverageEr"`      // 杠杆倍數
	LiqPriceEp      int64  `json:"liqPriceEp"`      // 强平價格
}

type PositionsResponse struct {
	Account   Account    `json:"account"`
	Positions []Position `json:"positions"`
}

type Account struct {
	AccountID          int64  `json:"accountId"`
	Currency           string `json:"currency"`
	AccountBalanceEv   int64  `json:"accountBalanceEv"`   // 账戶餘額
	TotalUsedBalanceEv int64  `json:"totalUsedBalanceEv"` // 已用餘額
}

type Trade struct {
	Timestamp int64  `json:"timestamp"`
	Symbol    string `json:"symbol"`
	Side      string `json:"side"`
	PriceEp   int64  `json:"priceEp"`
	Qty       int64  `json:"qty"`
	TradeID   string `json:"tradeID"`
}

type TradesResponse struct {
	Trades []Trade `json:"trades"`
}

type Kline struct {
	Timestamp int64 `json:"timestamp"`
	Interval  int   `json:"interval"`
	OpenEp    int64 `json:"openEp"`
	HighEp    int64 `json:"highEp"`
	LowEp     int64 `json:"lowEp"`
	CloseEp   int64 `json:"closeEp"`
	Volume    int64 `json:"volume"`
	Turnover  int64 `json:"turnoverEv"`
}

type KlinesResponse struct {
	Rows []Kline `json:"rows"`
}
