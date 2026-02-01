package bitmex

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
	BitMEXMainnetBaseURL = "https://www.bitmex.com"     // BitMEX 主网
	BitMEXTestnetBaseURL = "https://testnet.bitmex.com" // BitMEX 测试网
)

// BitMEXClient BitMEX 客户端
type BitMEXClient struct {
	apiKey     string
	secretKey  string
	baseURL    string
	httpClient *http.Client
	isTestnet  bool
}

// NewBitMEXClient 创建 BitMEX 客户端
func NewBitMEXClient(apiKey, secretKey string, isTestnet bool) *BitMEXClient {
	baseURL := BitMEXMainnetBaseURL
	if isTestnet {
		baseURL = BitMEXTestnetBaseURL
	}

	return &BitMEXClient{
		apiKey:     apiKey,
		secretKey:  secretKey,
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		isTestnet:  isTestnet,
	}
}

// signRequest BitMEX 签名：HMAC-SHA256
// 签名字符串：verb + path + nonce + data
func (c *BitMEXClient) signRequest(verb, path, nonce, data string) string {
	message := verb + path + nonce + data
	h := hmac.New(sha256.New, []byte(c.secretKey))
	h.Write([]byte(message))
	return hex.EncodeToString(h.Sum(nil))
}

// sendRequest 发送请求
func (c *BitMEXClient) sendRequest(ctx context.Context, method, path string, params url.Values, body interface{}, needSign bool) ([]byte, error) {
	reqURL := c.baseURL + path
	if len(params) > 0 {
		reqURL += "?" + params.Encode()
	}

	var reqBody []byte
	var err error
	if body != nil {
		reqBody, err = json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal body error: %w", err)
		}
	}

	req, err := http.NewRequestWithContext(ctx, method, reqURL, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("create request error: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	if needSign {
		nonce := strconv.FormatInt(time.Now().Unix()+5, 10) // 5秒过期时间
		signature := c.signRequest(method, path+"?"+params.Encode(), nonce, string(reqBody))

		req.Header.Set("api-expires", nonce)
		req.Header.Set("api-key", c.apiKey)
		req.Header.Set("api-signature", signature)
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
		var apiErr APIError
		if err := json.Unmarshal(respBody, &apiErr); err == nil {
			return nil, fmt.Errorf("API error: %s", apiErr.Error.Message)
		}
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

// GetInstrument 获取交易对信息
func (c *BitMEXClient) GetInstrument(ctx context.Context, symbol string) (*Instrument, error) {
	path := "/api/v1/instrument"
	params := url.Values{}
	params.Set("symbol", symbol)

	respBody, err := c.sendRequest(ctx, http.MethodGet, path, params, nil, false)
	if err != nil {
		return nil, err
	}

	var instruments []Instrument
	if err := json.Unmarshal(respBody, &instruments); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	if len(instruments) == 0 {
		return nil, fmt.Errorf("instrument not found: %s", symbol)
	}

	return &instruments[0], nil
}

// PlaceOrder 下单
func (c *BitMEXClient) PlaceOrder(ctx context.Context, req *OrderRequest) (*Order, error) {
	path := "/api/v1/order"
	params := url.Values{}

	respBody, err := c.sendRequest(ctx, http.MethodPost, path, params, req, true)
	if err != nil {
		return nil, err
	}

	var order Order
	if err := json.Unmarshal(respBody, &order); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	logger.Info("BitMEX order placed: %s", order.OrderID)
	return &order, nil
}

// CancelOrder 取消订单
func (c *BitMEXClient) CancelOrder(ctx context.Context, orderID string) error {
	path := "/api/v1/order"
	params := url.Values{}

	body := map[string]interface{}{
		"orderID": orderID,
	}

	_, err := c.sendRequest(ctx, http.MethodDelete, path, params, body, true)
	if err != nil {
		return err
	}

	logger.Info("BitMEX order cancelled: %s", orderID)
	return nil
}

// GetOrder 查询订单
func (c *BitMEXClient) GetOrder(ctx context.Context, orderID string) (*Order, error) {
	path := "/api/v1/order"
	params := url.Values{}
	params.Set("filter", fmt.Sprintf(`{"orderID":"%s"}`, orderID))

	respBody, err := c.sendRequest(ctx, http.MethodGet, path, params, nil, true)
	if err != nil {
		return nil, err
	}

	var orders []Order
	if err := json.Unmarshal(respBody, &orders); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	if len(orders) == 0 {
		return nil, fmt.Errorf("order not found: %s", orderID)
	}

	return &orders[0], nil
}

// GetOpenOrders 获取活跃订单
func (c *BitMEXClient) GetOpenOrders(ctx context.Context, symbol string) ([]Order, error) {
	path := "/api/v1/order"
	params := url.Values{}
	params.Set("filter", fmt.Sprintf(`{"open":true,"symbol":"%s"}`, symbol))

	respBody, err := c.sendRequest(ctx, http.MethodGet, path, params, nil, true)
	if err != nil {
		return nil, err
	}

	var orders []Order
	if err := json.Unmarshal(respBody, &orders); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	return orders, nil
}

// GetPosition 获取持仓
func (c *BitMEXClient) GetPosition(ctx context.Context, symbol string) (*Position, error) {
	path := "/api/v1/position"
	params := url.Values{}
	params.Set("filter", fmt.Sprintf(`{"symbol":"%s"}`, symbol))

	respBody, err := c.sendRequest(ctx, http.MethodGet, path, params, nil, true)
	if err != nil {
		return nil, err
	}

	var positions []Position
	if err := json.Unmarshal(respBody, &positions); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	if len(positions) == 0 {
		return &Position{Symbol: symbol, CurrentQty: 0}, nil
	}

	return &positions[0], nil
}

// GetMargin 获取账户信息
func (c *BitMEXClient) GetMargin(ctx context.Context) (*Margin, error) {
	path := "/api/v1/user/margin"
	params := url.Values{}

	respBody, err := c.sendRequest(ctx, http.MethodGet, path, params, nil, true)
	if err != nil {
		return nil, err
	}

	var margin Margin
	if err := json.Unmarshal(respBody, &margin); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	return &margin, nil
}

// GetTrade 获取最新成交
func (c *BitMEXClient) GetTrade(ctx context.Context, symbol string, count int) ([]Trade, error) {
	path := "/api/v1/trade"
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("count", strconv.Itoa(count))
	params.Set("reverse", "true")

	respBody, err := c.sendRequest(ctx, http.MethodGet, path, params, nil, false)
	if err != nil {
		return nil, err
	}

	var trades []Trade
	if err := json.Unmarshal(respBody, &trades); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	return trades, nil
}

// GetTradeBucketed 获取 K线数据
func (c *BitMEXClient) GetTradeBucketed(ctx context.Context, symbol, binSize string, count int) ([]TradeBucket, error) {
	path := "/api/v1/trade/bucketed"
	params := url.Values{}
	params.Set("symbol", symbol)
	params.Set("binSize", binSize)
	params.Set("count", strconv.Itoa(count))
	params.Set("reverse", "true")

	respBody, err := c.sendRequest(ctx, http.MethodGet, path, params, nil, false)
	if err != nil {
		return nil, err
	}

	var buckets []TradeBucket
	if err := json.Unmarshal(respBody, &buckets); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	return buckets, nil
}

// 数据结构定义

type APIError struct {
	Error struct {
		Message string `json:"message"`
		Name    string `json:"name"`
	} `json:"error"`
}

type Instrument struct {
	Symbol           string    `json:"symbol"`
	RootSymbol       string    `json:"rootSymbol"`
	State            string    `json:"state"`
	Typ              string    `json:"typ"`
	Listing          time.Time `json:"listing"`
	Front            time.Time `json:"front"`
	Expiry           time.Time `json:"expiry"`
	Settle           time.Time `json:"settle"`
	QuoteCurrency    string    `json:"quoteCurrency"`
	UnderlyingSymbol string    `json:"underlyingSymbol"`
	TickSize         float64   `json:"tickSize"`
	LotSize          float64   `json:"lotSize"`
	MaxOrderQty      float64   `json:"maxOrderQty"`
	MaxPrice         float64   `json:"maxPrice"`
	IsQuanto         bool      `json:"isQuanto"`
	IsInverse        bool      `json:"isInverse"`
	InitMargin       float64   `json:"initMargin"`
	MaintMargin      float64   `json:"maintMargin"`
	LastPrice        float64   `json:"lastPrice"`
	MarkPrice        float64   `json:"markPrice"`
	IndexPrice       float64   `json:"indexPrice"`
}

type OrderRequest struct {
	Symbol      string  `json:"symbol"`
	Side        string  `json:"side"` // Buy, Sell
	OrderQty    float64 `json:"orderQty"`
	Price       float64 `json:"price,omitempty"`
	OrdType     string  `json:"ordType"` // Limit, Market
	ClOrdID     string  `json:"clOrdID,omitempty"`
	ExecInst    string  `json:"execInst,omitempty"`
	TimeInForce string  `json:"timeInForce,omitempty"` // GoodTillCancel, ImmediateOrCancel
}

type Order struct {
	OrderID      string    `json:"orderID"`
	ClOrdID      string    `json:"clOrdID"`
	Symbol       string    `json:"symbol"`
	Side         string    `json:"side"`
	OrderQty     float64   `json:"orderQty"`
	Price        float64   `json:"price"`
	OrdType      string    `json:"ordType"`
	OrdStatus    string    `json:"ordStatus"`
	CumQty       float64   `json:"cumQty"`
	LeavesQty    float64   `json:"leavesQty"`
	AvgPx        float64   `json:"avgPx"`
	Timestamp    time.Time `json:"timestamp"`
	TransactTime time.Time `json:"transactTime"`
}

type Position struct {
	Account          int64   `json:"account"`
	Symbol           string  `json:"symbol"`
	Currency         string  `json:"currency"`
	CurrentQty       float64 `json:"currentQty"`
	MarkPrice        float64 `json:"markPrice"`
	MarkValue        float64 `json:"markValue"`
	HomeNotional     float64 `json:"homeNotional"`
	ForeignNotional  float64 `json:"foreignNotional"`
	AvgCostPrice     float64 `json:"avgCostPrice"`
	AvgEntryPrice    float64 `json:"avgEntryPrice"`
	UnrealisedPnl    float64 `json:"unrealisedPnl"`
	RealisedPnl      float64 `json:"realisedPnl"`
	Leverage         float64 `json:"leverage"`
	LiquidationPrice float64 `json:"liquidationPrice"`
	InitMargin       float64 `json:"initMargin"`
	MaintMargin      float64 `json:"maintMargin"`
}

type Margin struct {
	Account            int64   `json:"account"`
	Currency           string  `json:"currency"`
	Amount             float64 `json:"amount"`
	WalletBalance      float64 `json:"walletBalance"`
	MarginBalance      float64 `json:"marginBalance"`
	AvailableMargin    float64 `json:"availableMargin"`
	WithdrawableMargin float64 `json:"withdrawableMargin"`
	UnrealisedPnl      float64 `json:"unrealisedPnl"`
	RealisedPnl        float64 `json:"realisedPnl"`
}

type Trade struct {
	Timestamp     time.Time `json:"timestamp"`
	Symbol        string    `json:"symbol"`
	Side          string    `json:"side"`
	Size          float64   `json:"size"`
	Price         float64   `json:"price"`
	TickDirection string    `json:"tickDirection"`
	TrdMatchID    string    `json:"trdMatchID"`
	GrossValue    float64   `json:"grossValue"`
	HomeNotional  float64   `json:"homeNotional"`
}

type TradeBucket struct {
	Timestamp    time.Time `json:"timestamp"`
	Symbol       string    `json:"symbol"`
	Open         float64   `json:"open"`
	High         float64   `json:"high"`
	Low          float64   `json:"low"`
	Close        float64   `json:"close"`
	Trades       float64   `json:"trades"`
	Volume       float64   `json:"volume"`
	Vwap         float64   `json:"vwap"`
	LastSize     float64   `json:"lastSize"`
	Turnover     float64   `json:"turnover"`
	HomeNotional float64   `json:"homeNotional"`
}
