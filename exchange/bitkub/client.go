package bitkub

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
	"strconv"
	"time"

	"quantmesh/logger"
)

const (
	// 主网 API 地址
	MainnetRestURL = "https://api.bitkub.com"
	// Bitkub没有测试网
	TestnetRestURL = "https://api.bitkub.com"
)

// BitkubClient Bitkub REST API 客戶端
type BitkubClient struct {
	apiKey     string
	secretKey  string
	baseURL    string
	useTestnet bool // Bitkub没有测试网，但保留此字段以保持接口一致性
	httpClient *http.Client
}

// NewBitkubClient 創建 Bitkub 客戶端
func NewBitkubClient(apiKey, secretKey string, useTestnet bool) *BitkubClient {
	baseURL := MainnetRestURL
	if useTestnet {
		baseURL = TestnetRestURL
	}

	return &BitkubClient{
		apiKey:     apiKey,
		secretKey:  secretKey,
		baseURL:    baseURL,
		useTestnet: useTestnet,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// getServerTime 獲取服務器時間（毫秒）
func (c *BitkubClient) getServerTime(ctx context.Context) (int64, error) {
	url := c.baseURL + "/api/v3/servertime"
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return 0, fmt.Errorf("創建请求失败: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, fmt.Errorf("发送请求失败: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("读取响应失败: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("HTTP 錯误 %d: %s", resp.StatusCode, string(respBody))
	}

	timestamp, err := strconv.ParseInt(string(respBody), 10, 64)
	if err != nil {
		return 0, fmt.Errorf("解析時間戳失败: %w", err)
	}

	return timestamp, nil
}

// sign 生成签名（HMAC-SHA256，hex编码）
func (c *BitkubClient) sign(timestamp int64, method, path, query, body string) string {
	// 签名格式: {timestamp}{METHOD}{path}{query}{body}
	signStr := strconv.FormatInt(timestamp, 10) + method + path
	if query != "" {
		signStr += "?" + query
	}
	if body != "" {
		signStr += body
	}

	h := hmac.New(sha256.New, []byte(c.secretKey))
	h.Write([]byte(signStr))
	return hex.EncodeToString(h.Sum(nil))
}

// request 发送 HTTP 请求
func (c *BitkubClient) request(ctx context.Context, method, path string, query map[string]string, body interface{}) ([]byte, error) {
	// 获取服务器时间
	timestamp, err := c.getServerTime(ctx)
	if err != nil {
		return nil, fmt.Errorf("獲取服務器時間失败: %w", err)
	}

	var bodyBytes []byte
	var bodyStr string
	if body != nil {
		bodyBytes, err = json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("序列化请求体失败: %w", err)
		}
		bodyStr = string(bodyBytes)
	}

	// 构建查询字符串
	queryStr := ""
	if query != nil && len(query) > 0 {
		first := true
		for k, v := range query {
			if !first {
				queryStr += "&"
			}
			queryStr += k + "=" + v
			first = false
		}
	}

	// 生成签名
	signature := c.sign(timestamp, method, path, queryStr, bodyStr)

	// 构建URL
	url := c.baseURL + path
	if queryStr != "" {
		url += "?" + queryStr
	}

	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("創建请求失败: %w", err)
	}

	// 設置请求头
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-BTK-APIKEY", c.apiKey)
	req.Header.Set("X-BTK-TIMESTAMP", strconv.FormatInt(timestamp, 10))
	req.Header.Set("X-BTK-SIGN", signature)

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
			Error   int    `json:"error"`
			Message string `json:"message"`
		}
		if err := json.Unmarshal(respBody, &errResp); err == nil {
			return nil, fmt.Errorf("API 錯误 %d: %s", errResp.Error, errResp.Message)
		}
		return nil, fmt.Errorf("HTTP 錯误 %d: %s", resp.StatusCode, string(respBody))
	}

	// 检查API响应格式
	var apiResp struct {
		Error  int             `json:"error"`
		Result json.RawMessage `json:"result"`
	}

	if err := json.Unmarshal(respBody, &apiResp); err == nil {
		if apiResp.Error != 0 {
			return nil, fmt.Errorf("API 錯误 %d", apiResp.Error)
		}
		return apiResp.Result, nil
	}

	// 如果没有result字段，直接返回响应体
	return respBody, nil
}

// Symbol 交易對信息
type Symbol struct {
	ID                int     `json:"id"`
	Symbol            string  `json:"symbol"`
	BaseAsset         string  `json:"base_asset"`
	QuoteAsset        string  `json:"quote_asset"`
	PriceScale        int     `json:"price_scale"`
	QuantityScale     int     `json:"quantity_scale"`
	PriceStep         string  `json:"price_step"`
	QuantityStep      string  `json:"quantity_step"`
	MinQuoteSize      float64 `json:"min_quote_size"`
	Status            string  `json:"status"`
	FreezeBuy         bool    `json:"freeze_buy"`
	FreezeSell        bool    `json:"freeze_sell"`
	FreezeCancel      bool    `json:"freeze_cancel"`
	BuyPriceGap       float64 `json:"buy_price_gap_as_percent"`
	SellPriceGap      float64 `json:"sell_price_gap_as_percent"`
}

// GetSymbols 獲取所有交易對信息
func (c *BitkubClient) GetSymbols(ctx context.Context) ([]Symbol, error) {
	data, err := c.request(ctx, "GET", "/api/v3/market/symbols", nil, nil)
	if err != nil {
		return nil, err
	}

	var symbols []Symbol
	if err := json.Unmarshal(data, &symbols); err != nil {
		return nil, fmt.Errorf("解析交易對信息失败: %w", err)
	}

	return symbols, nil
}

// Balance 餘額信息
type Balance struct {
	Available float64 `json:"available"`
	Reserved  float64 `json:"reserved"`
}

// GetBalances 獲取餘額（包含可用和凍結）
func (c *BitkubClient) GetBalances(ctx context.Context) (map[string]Balance, error) {
	data, err := c.request(ctx, "POST", "/api/v3/market/balances", nil, nil)
	if err != nil {
		return nil, err
	}

	var balances map[string]Balance
	if err := json.Unmarshal(data, &balances); err != nil {
		return nil, fmt.Errorf("解析餘額信息失败: %w", err)
	}

	return balances, nil
}

// GetWallet 獲取錢包餘額（僅可用餘額）
func (c *BitkubClient) GetWallet(ctx context.Context) (map[string]float64, error) {
	data, err := c.request(ctx, "POST", "/api/v3/market/wallet", nil, nil)
	if err != nil {
		return nil, err
	}

	var wallet map[string]float64
	if err := json.Unmarshal(data, &wallet); err != nil {
		return nil, fmt.Errorf("解析錢包信息失败: %w", err)
	}

	return wallet, nil
}

// PlaceBidRequest 買單请求
type PlaceBidRequest struct {
	Sym      string  `json:"sym"`       // 交易對，如 btc_thb
	Amt      float64 `json:"amt"`       // 花費金額（THB）
	Rat      float64 `json:"rat"`       // 價格
	Typ      string  `json:"typ"`       // 訂單類型: limit 或 market
	ClientID string  `json:"client_id,omitempty"` // 客戶訂單ID（可選）
	PostOnly bool    `json:"post_only,omitempty"` // Post-only標志（可選）
}

// PlaceBidResponse 買單响应
type PlaceBidResponse struct {
	ID   string  `json:"id"`   // 訂單ID
	Typ  string  `json:"typ"`  // 訂單類型
	Amt  float64 `json:"amt"`  // 花費金額
	Rat  float64 `json:"rat"`  // 價格
	Fee  float64 `json:"fee"`  // 手續費
	Cre  float64 `json:"cre"`  // 使用的費用信用
	Rec  float64 `json:"rec"`  // 將收到的數量
	TS   string  `json:"ts"`   // 時間戳
	CI   string  `json:"ci,omitempty"` // 客戶ID
}

// PlaceBid 下買單
func (c *BitkubClient) PlaceBid(ctx context.Context, req PlaceBidRequest) (*PlaceBidResponse, error) {
	data, err := c.request(ctx, "POST", "/api/v3/market/place-bid", nil, req)
	if err != nil {
		return nil, err
	}

	var resp PlaceBidResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("解析買單响应失败: %w", err)
	}

	return &resp, nil
}

// PlaceAskRequest 賣單请求
type PlaceAskRequest struct {
	Sym      string  `json:"sym"`       // 交易對，如 btc_thb
	Amt      float64 `json:"amt"`       // 賣出數量（加密貨幣）
	Rat      float64 `json:"rat"`       // 價格
	Typ      string  `json:"typ"`       // 訂單類型: limit 或 market
	ClientID string  `json:"client_id,omitempty"` // 客戶訂單ID（可選）
	PostOnly bool    `json:"post_only,omitempty"` // Post-only標志（可選）
}

// PlaceAskResponse 賣單响应
type PlaceAskResponse struct {
	ID   string  `json:"id"`   // 訂單ID
	Typ  string  `json:"typ"`  // 訂單類型
	Amt  float64 `json:"amt"`  // 賣出數量
	Rat  float64 `json:"rat"`  // 價格
	Fee  float64 `json:"fee"`  // 手續費
	Cre  float64 `json:"cre"`  // 使用的費用信用
	Rec  float64 `json:"rec"`  // 將收到的金額
	TS   string  `json:"ts"`   // 時間戳
	CI   string  `json:"ci,omitempty"` // 客戶ID
}

// PlaceAsk 下賣單
func (c *BitkubClient) PlaceAsk(ctx context.Context, req PlaceAskRequest) (*PlaceAskResponse, error) {
	data, err := c.request(ctx, "POST", "/api/v3/market/place-ask", nil, req)
	if err != nil {
		return nil, err
	}

	var resp PlaceAskResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("解析賣單响应失败: %w", err)
	}

	return &resp, nil
}

// CancelOrderRequest 取消訂單请求
type CancelOrderRequest struct {
	Sym string `json:"sym"` // 交易對，如 btc_thb
	ID  string `json:"id"`  // 訂單ID
	SD  string `json:"sd"`  // 訂單方向: buy 或 sell
}

// CancelOrder 取消訂單
func (c *BitkubClient) CancelOrder(ctx context.Context, sym, orderID, side string) error {
	req := CancelOrderRequest{
		Sym: sym,
		ID:  orderID,
		SD:  side,
	}

	_, err := c.request(ctx, "POST", "/api/v3/market/cancel-order", nil, req)
	return err
}

// OpenOrder 未完成訂單
type OpenOrder struct {
	ID        string  `json:"id"`         // 訂單ID
	Side      string  `json:"side"`       // 訂單方向: buy 或 sell
	Type      string  `json:"type"`       // 訂單類型: limit 或 market
	Rate      string  `json:"rate"`       // 價格
	Fee       string  `json:"fee"`        // 手續費
	Credit    string  `json:"credit"`     // 使用的費用信用
	Amount    string  `json:"amount"`     // 數量（買單是THB數量，賣單是加密貨幣數量）
	Receive   string  `json:"receive"`    // 將收到的數量（買單是加密貨幣，賣單是THB）
	ParentID  string  `json:"parent_id"`  // 父訂單ID
	SuperID   string  `json:"super_id"`   // 超級父訂單ID
	ClientID  string  `json:"client_id"`  // 客戶ID
	TS        int64   `json:"ts"`         // 時間戳
}

// GetOpenOrders 查詢未完成訂單
func (c *BitkubClient) GetOpenOrders(ctx context.Context, sym string) ([]OpenOrder, error) {
	query := map[string]string{
		"sym": sym,
	}

	data, err := c.request(ctx, "GET", "/api/v3/market/my-open-orders", query, nil)
	if err != nil {
		return nil, err
	}

	var orders []OpenOrder
	if err := json.Unmarshal(data, &orders); err != nil {
		return nil, fmt.Errorf("解析未完成訂單失败: %w", err)
	}

	return orders, nil
}

// OrderInfo 訂單詳情
type OrderInfo struct {
	ID            string      `json:"id"`             // 訂單ID
	First         string      `json:"first"`          // 第一個訂單ID
	Parent        string      `json:"parent"`         // 父訂單ID
	Last          string      `json:"last"`          // 最後一個訂單ID
	ClientID      string      `json:"client_id"`     // 客戶ID
	PostOnly      bool        `json:"post_only"`     // Post-only標志
	Amount        string      `json:"amount"`        // 訂單數量
	Rate          float64     `json:"rate"`          // 價格
	Fee           string      `json:"fee"`           // 手續費
	Credit        string      `json:"credit"`        // 使用的費用信用
	Filled        string      `json:"filled"`        // 已成交數量
	Total         string      `json:"total"`         // 總數量
	Status        string      `json:"status"`        // 狀態: filled, unfilled, cancelled
	PartialFilled bool        `json:"partial_filled"` // 是否部分成交
	Remaining     string      `json:"remaining"`     // 剩餘數量
	History       []OrderFill `json:"history"`       // 成交歷史
}

// OrderFill 訂單成交記錄
type OrderFill struct {
	Amount    string `json:"amount"`    // 成交數量
	Credit    string `json:"credit"`    // 使用的費用信用
	Fee       string `json:"fee"`      // 手續費
	ID        string `json:"id"`        // 訂單ID
	Rate      float64 `json:"rate"`     // 成交價格
	Timestamp int64   `json:"timestamp"` // 時間戳
	TxnID     string  `json:"txn_id"`   // 交易ID
}

// GetOrderInfo 查詢訂單詳情
func (c *BitkubClient) GetOrderInfo(ctx context.Context, sym, orderID, side string) (*OrderInfo, error) {
	query := map[string]string{
		"sym": sym,
		"id":  orderID,
		"sd":  side,
	}

	data, err := c.request(ctx, "GET", "/api/v3/market/order-info", query, nil)
	if err != nil {
		return nil, err
	}

	var orderInfo OrderInfo
	if err := json.Unmarshal(data, &orderInfo); err != nil {
		return nil, fmt.Errorf("解析訂單詳情失败: %w", err)
	}

	return &orderInfo, nil
}

// Ticker 行情數據
type Ticker struct {
	Symbol       string  `json:"symbol"`
	BaseVolume   string  `json:"base_volume"`
	High24Hr     string  `json:"high_24_hr"`
	HighestBid   string  `json:"highest_bid"`
	Last         string  `json:"last"`
	Low24Hr      string  `json:"low_24_hr"`
	LowestAsk    string  `json:"lowest_ask"`
	PercentChange string `json:"percent_change"`
	QuoteVolume  string  `json:"quote_volume"`
}

// GetTicker 獲取行情
func (c *BitkubClient) GetTicker(ctx context.Context, sym string) (*Ticker, error) {
	query := map[string]string{
		"sym": sym,
	}

	data, err := c.request(ctx, "GET", "/api/v3/market/ticker", query, nil)
	if err != nil {
		return nil, err
	}

	var tickers []Ticker
	if err := json.Unmarshal(data, &tickers); err != nil {
		return nil, fmt.Errorf("解析行情數據失败: %w", err)
	}

	if len(tickers) == 0 {
		return nil, fmt.Errorf("未找到行情數據")
	}

	return &tickers[0], nil
}

func init() {
	logger.Info("📦 [Bitkub Client] REST API 客戶端已初始化")
}
