package poloniex

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
	PoloniexMainnetBaseURL = "https://api.poloniex.com"         // Poloniex 主网
	PoloniexTestnetBaseURL = "https://testnet-api.poloniex.com" // Poloniex 測試網
)

// PoloniexClient Poloniex 客戶端
type PoloniexClient struct {
	apiKey     string
	secretKey  string
	baseURL    string
	httpClient *http.Client
	isTestnet  bool
}

// NewPoloniexClient 創建 Poloniex 客戶端
func NewPoloniexClient(apiKey, secretKey string, isTestnet bool) *PoloniexClient {
	baseURL := PoloniexMainnetBaseURL
	if isTestnet {
		baseURL = PoloniexTestnetBaseURL
	}

	return &PoloniexClient{
		apiKey:     apiKey,
		secretKey:  secretKey,
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		isTestnet:  isTestnet,
	}
}

// signRequest Poloniex 签名：HMAC-SHA256
func (c *PoloniexClient) signRequest(body string) string {
	h := hmac.New(sha256.New, []byte(c.secretKey))
	h.Write([]byte(body))
	return hex.EncodeToString(h.Sum(nil))
}

// sendRequest 发送请求
func (c *PoloniexClient) sendRequest(ctx context.Context, method, path string, params url.Values, body interface{}, needSign bool) ([]byte, error) {
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
		timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
		signPayload := method + "\n" + path + queryString + "\n" + bodyStr + "\n" + timestamp
		signature := c.signRequest(signPayload)

		req.Header.Set("key", c.apiKey)
		req.Header.Set("signTimestamp", timestamp)
		req.Header.Set("signature", signature)
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

	return respBody, nil
}

// GetSymbol 獲取交易對信息
func (c *PoloniexClient) GetSymbol(ctx context.Context, symbol string) (*Symbol, error) {
	path := "/markets/" + symbol
	params := url.Values{}

	respBody, err := c.sendRequest(ctx, http.MethodGet, path, params, nil, false)
	if err != nil {
		return nil, err
	}

	var symbolInfo Symbol
	if err := json.Unmarshal(respBody, &symbolInfo); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	return &symbolInfo, nil
}

// PlaceOrder 下單
func (c *PoloniexClient) PlaceOrder(ctx context.Context, req *OrderRequest) (*Order, error) {
	path := "/orders"
	params := url.Values{}

	respBody, err := c.sendRequest(ctx, http.MethodPost, path, params, req, true)
	if err != nil {
		return nil, err
	}

	var order Order
	if err := json.Unmarshal(respBody, &order); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	logger.Info("Poloniex order placed: %s", order.ID)
	return &order, nil
}

// CancelOrder 取消訂單
func (c *PoloniexClient) CancelOrder(ctx context.Context, orderID string) error {
	path := "/orders/" + orderID
	params := url.Values{}

	_, err := c.sendRequest(ctx, http.MethodDelete, path, params, nil, true)
	if err != nil {
		return err
	}

	logger.Info("Poloniex order cancelled: %s", orderID)
	return nil
}

// GetOrder 查詢訂單
func (c *PoloniexClient) GetOrder(ctx context.Context, orderID string) (*Order, error) {
	path := "/orders/" + orderID
	params := url.Values{}

	respBody, err := c.sendRequest(ctx, http.MethodGet, path, params, nil, true)
	if err != nil {
		return nil, err
	}

	var order Order
	if err := json.Unmarshal(respBody, &order); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	return &order, nil
}

// GetOpenOrders 獲取活跃订單
func (c *PoloniexClient) GetOpenOrders(ctx context.Context, symbol string) ([]Order, error) {
	path := "/orders"
	params := url.Values{}
	params.Set("state", "NEW")
	if symbol != "" {
		params.Set("symbol", symbol)
	}

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

// GetBalance 獲取帳戶餘額
func (c *PoloniexClient) GetBalance(ctx context.Context) ([]Balance, error) {
	path := "/accounts/balances"
	params := url.Values{}

	respBody, err := c.sendRequest(ctx, http.MethodGet, path, params, nil, true)
	if err != nil {
		return nil, err
	}

	var balances []Balance
	if err := json.Unmarshal(respBody, &balances); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	return balances, nil
}

// GetTicker 獲取最新價格
func (c *PoloniexClient) GetTicker(ctx context.Context, symbol string) (*Ticker, error) {
	path := "/markets/" + symbol + "/price"
	params := url.Values{}

	respBody, err := c.sendRequest(ctx, http.MethodGet, path, params, nil, false)
	if err != nil {
		return nil, err
	}

	var ticker Ticker
	if err := json.Unmarshal(respBody, &ticker); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	return &ticker, nil
}

// GetKlines 獲取 K線數據
func (c *PoloniexClient) GetKlines(ctx context.Context, symbol, interval string, limit int) ([]Kline, error) {
	path := "/markets/" + symbol + "/candles"
	params := url.Values{}
	params.Set("interval", interval)
	if limit > 0 {
		params.Set("limit", strconv.Itoa(limit))
	}

	respBody, err := c.sendRequest(ctx, http.MethodGet, path, params, nil, false)
	if err != nil {
		return nil, err
	}

	var klines []Kline
	if err := json.Unmarshal(respBody, &klines); err != nil {
		return nil, fmt.Errorf("unmarshal error: %w", err)
	}

	return klines, nil
}

// 數據結構定义

type Symbol struct {
	Symbol            string  `json:"symbol"`
	BaseCurrency      string  `json:"baseCurrencyName"`
	QuoteCurrency     string  `json:"quoteCurrencyName"`
	PricePrecision    int     `json:"priceScale"`
	QuantityPrecision int     `json:"quantityScale"`
	MinQuantity       float64 `json:"minQuantity,string"`
	MaxQuantity       float64 `json:"maxQuantity,string"`
	MinAmount         float64 `json:"minAmount,string"`
}

type OrderRequest struct {
	Symbol        string `json:"symbol"`
	Side          string `json:"side"` // BUY, SELL
	Type          string `json:"type"` // LIMIT, MARKET
	Quantity      string `json:"quantity"`
	Price         string `json:"price,omitempty"`
	TimeInForce   string `json:"timeInForce,omitempty"`
	ClientOrderID string `json:"clientOrderId,omitempty"`
}

type Order struct {
	ID             string  `json:"id"`
	ClientOrderID  string  `json:"clientOrderId"`
	Symbol         string  `json:"symbol"`
	Side           string  `json:"side"`
	Type           string  `json:"type"`
	Price          float64 `json:"price,string"`
	Quantity       float64 `json:"quantity,string"`
	FilledQuantity float64 `json:"filledQuantity,string"`
	State          string  `json:"state"`
	CreateTime     int64   `json:"createTime"`
	UpdateTime     int64   `json:"updateTime"`
}

type Balance struct {
	Currency  string  `json:"currency"`
	Available float64 `json:"available,string"`
	Hold      float64 `json:"hold,string"`
}

type Ticker struct {
	Symbol string  `json:"symbol"`
	Price  float64 `json:"price,string"`
	Time   int64   `json:"time"`
}

type Kline struct {
	Low              float64 `json:"low,string"`
	High             float64 `json:"high,string"`
	Open             float64 `json:"open,string"`
	Close            float64 `json:"close,string"`
	Amount           float64 `json:"amount,string"`
	Quantity         float64 `json:"quantity,string"`
	BuyTakerAmount   float64 `json:"buyTakerAmount,string"`
	BuyTakerQuantity float64 `json:"buyTakerQuantity,string"`
	TradeCount       int64   `json:"tradeCount"`
	Ts               int64   `json:"ts"`
	WeightedAverage  float64 `json:"weightedAverage,string"`
	Interval         string  `json:"interval"`
	StartTime        int64   `json:"startTime"`
	CloseTime        int64   `json:"closeTime"`
}

func (k *Kline) UnmarshalJSON(data []byte) error {
	var row []interface{}
	if err := json.Unmarshal(data, &row); err != nil {
		type alias Kline
		var obj alias
		if objErr := json.Unmarshal(data, &obj); objErr != nil {
			return err
		}
		*k = Kline(obj)
		return nil
	}
	if len(row) < 14 {
		return fmt.Errorf("invalid poloniex kline row length: %d", len(row))
	}
	k.Low = float64FromAny(row[0])
	k.High = float64FromAny(row[1])
	k.Open = float64FromAny(row[2])
	k.Close = float64FromAny(row[3])
	k.Amount = float64FromAny(row[4])
	k.Quantity = float64FromAny(row[5])
	k.BuyTakerAmount = float64FromAny(row[6])
	k.BuyTakerQuantity = float64FromAny(row[7])
	k.TradeCount = int64FromAny(row[8])
	k.Ts = int64FromAny(row[9])
	k.WeightedAverage = float64FromAny(row[10])
	k.Interval = stringFromAny(row[11])
	k.StartTime = int64FromAny(row[12])
	k.CloseTime = int64FromAny(row[13])
	return nil
}

func int64FromAny(v interface{}) int64 {
	switch x := v.(type) {
	case float64:
		return int64(x)
	case string:
		n, _ := strconv.ParseInt(x, 10, 64)
		return n
	default:
		return 0
	}
}

func float64FromAny(v interface{}) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case string:
		n, _ := strconv.ParseFloat(x, 64)
		return n
	default:
		return 0
	}
}

func stringFromAny(v interface{}) string {
	switch x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	default:
		return ""
	}
}
