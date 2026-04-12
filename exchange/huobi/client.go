package huobi

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
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
	// 主网 API 地址
	MainnetRestURL = "https://api.hbdm.com"
	// WebSocket 地址
	MainnetWsURL = "wss://api.hbdm.com/linear-swap-notification"
)

// HuobiClient Huobi REST API 客戶端
type HuobiClient struct {
	apiKey     string
	secretKey  string
	baseURL    string
	httpClient *http.Client
}

// NewHuobiClient 創建 Huobi 客戶端
func NewHuobiClient(apiKey, secretKey string) *HuobiClient {
	return &HuobiClient{
		apiKey:    apiKey,
		secretKey: secretKey,
		baseURL:   MainnetRestURL,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// sign 生成签名
func (c *HuobiClient) sign(method, host, path string, params map[string]string) string {
	// 按字母序排序参數
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// 構建查詢字符串
	var queryParts []string
	for _, k := range keys {
		queryParts = append(queryParts, fmt.Sprintf("%s=%s", k, url.QueryEscape(params[k])))
	}
	queryString := strings.Join(queryParts, "&")

	// 構建签名字符串
	signStr := fmt.Sprintf("%s\n%s\n%s\n%s", method, host, path, queryString)

	// HMAC-SHA256
	h := hmac.New(sha256.New, []byte(c.secretKey))
	h.Write([]byte(signStr))
	signature := base64.StdEncoding.EncodeToString(h.Sum(nil))

	return signature
}

// request 发送 HTTP 请求
func (c *HuobiClient) request(ctx context.Context, method, path string, params map[string]string, body interface{}) ([]byte, error) {
	// 添加公共参數
	if params == nil {
		params = make(map[string]string)
	}
	params["AccessKeyId"] = c.apiKey
	params["SignatureMethod"] = "HmacSHA256"
	params["SignatureVersion"] = "2"
	params["Timestamp"] = time.Now().UTC().Format("2006-01-02T15:04:05")

	// 生成签名
	u, _ := url.Parse(c.baseURL)
	signature := c.sign(method, u.Host, path, params)
	params["Signature"] = signature

	// 構建 URL
	values := url.Values{}
	for k, v := range params {
		values.Add(k, v)
	}
	fullURL := c.baseURL + path + "?" + values.Encode()

	// 構建请求体
	var bodyBytes []byte
	if body != nil {
		var err error
		bodyBytes, err = json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("序列化请求体失败: %w", err)
		}
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("創建请求失败: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "QuantMesh/1.0")

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

	// 检查 Huobi API 响应
	var apiResp struct {
		Status  string          `json:"status"`
		ErrCode int             `json:"err_code"`
		ErrMsg  string          `json:"err_msg"`
		Data    json.RawMessage `json:"data"`
	}

	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	if apiResp.Status != "ok" {
		return nil, fmt.Errorf("API 錯误 %d: %s", apiResp.ErrCode, apiResp.ErrMsg)
	}

	return apiResp.Data, nil
}

// ContractInfo 合約信息
type ContractInfo struct {
	Symbol         string `json:"symbol"`
	ContractCode   string `json:"contract_code"`
	PriceTick      string `json:"price_tick"`
	ContractSize   float64 `json:"contract_size"`
	SettlementDate string `json:"settlement_date"`
}

// GetContractInfo 獲取合約信息
func (c *HuobiClient) GetContractInfo(ctx context.Context, symbol string) ([]ContractInfo, error) {
	params := map[string]string{}
	if symbol != "" {
		params["contract_code"] = symbol
	}

	data, err := c.request(ctx, "GET", "/linear-swap-api/v1/swap_contract_info", params, nil)
	if err != nil {
		return nil, err
	}

	var contracts []ContractInfo
	if err := json.Unmarshal(data, &contracts); err != nil {
		return nil, fmt.Errorf("解析合約信息失败: %w", err)
	}

	return contracts, nil
}

// PlaceOrderResult 下單結果
type PlaceOrderResult struct {
	OrderId       int64  `json:"order_id"`
	ClientOrderId string `json:"client_order_id"`
}

// PlaceOrder 下單
func (c *HuobiClient) PlaceOrder(ctx context.Context, order map[string]interface{}) (*PlaceOrderResult, error) {
	data, err := c.request(ctx, "POST", "/linear-swap-api/v1/swap_order", nil, order)
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
func (c *HuobiClient) CancelOrder(ctx context.Context, symbol, orderId, clientOrderId string) error {
	body := map[string]interface{}{
		"contract_code": symbol,
	}

	if orderId != "" {
		body["order_id"] = orderId
	}
	if clientOrderId != "" {
		body["client_order_id"] = clientOrderId
	}

	_, err := c.request(ctx, "POST", "/linear-swap-api/v1/swap_cancel", nil, body)
	return err
}

// HuobiOrder 订單信息
type HuobiOrder struct {
	OrderId       int64   `json:"order_id"`
	ClientOrderId string  `json:"client_order_id"`
	Symbol        string  `json:"symbol"`
	ContractCode  string  `json:"contract_code"`
	Direction     string  `json:"direction"` // buy, sell
	Offset        string  `json:"offset"`    // open, close
	Price         float64 `json:"price"`
	Volume        float64 `json:"volume"`
	TradeVolume   float64 `json:"trade_volume"`
	TradeAvgPrice float64 `json:"trade_avg_price"`
	Status        int     `json:"status"`
	OrderType     int     `json:"order_type"`
	CreatedAt     int64   `json:"created_at"`
}

// GetOrder 查詢訂單
func (c *HuobiClient) GetOrder(ctx context.Context, symbol, orderId, clientOrderId string) (*HuobiOrder, error) {
	body := map[string]interface{}{
		"contract_code": symbol,
	}

	if orderId != "" {
		body["order_id"] = orderId
	}
	if clientOrderId != "" {
		body["client_order_id"] = clientOrderId
	}

	data, err := c.request(ctx, "POST", "/linear-swap-api/v1/swap_order_info", nil, body)
	if err != nil {
		return nil, err
	}

	var orders []HuobiOrder
	if err := json.Unmarshal(data, &orders); err != nil {
		return nil, fmt.Errorf("解析订單信息失败: %w", err)
	}

	if len(orders) == 0 {
		return nil, fmt.Errorf("订單不存在")
	}

	return &orders[0], nil
}

// GetOpenOrders 查詢未完成订單
func (c *HuobiClient) GetOpenOrders(ctx context.Context, symbol string) ([]HuobiOrder, error) {
	body := map[string]interface{}{
		"contract_code": symbol,
	}

	data, err := c.request(ctx, "POST", "/linear-swap-api/v1/swap_openorders", nil, body)
	if err != nil {
		return nil, err
	}

	var result struct {
		Orders []HuobiOrder `json:"orders"`
	}

	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("解析订單列表失败: %w", err)
	}

	return result.Orders, nil
}

// AccountInfo 帳戶資訊
type AccountInfo struct {
	Symbol            string  `json:"symbol"`
	MarginBalance     float64 `json:"margin_balance"`
	MarginAvailable   float64 `json:"margin_available"`
	WithdrawAvailable float64 `json:"withdraw_available"`
	RiskRate          float64 `json:"risk_rate"`
}

// GetAccountInfo 獲取帳戶信息
func (c *HuobiClient) GetAccountInfo(ctx context.Context, symbol string) ([]AccountInfo, error) {
	body := map[string]interface{}{}
	if symbol != "" {
		body["contract_code"] = symbol
	}

	data, err := c.request(ctx, "POST", "/linear-swap-api/v1/swap_account_info", nil, body)
	if err != nil {
		return nil, err
	}

	var accounts []AccountInfo
	if err := json.Unmarshal(data, &accounts); err != nil {
		return nil, fmt.Errorf("解析帳戶資訊失败: %w", err)
	}

	return accounts, nil
}

// HuobiPositionInfo 持倉資訊
type HuobiPositionInfo struct {
	Symbol       string  `json:"symbol"`
	ContractCode string  `json:"contract_code"`
	Volume       float64 `json:"volume"`
	Available    float64 `json:"available"`
	CostOpen     float64 `json:"cost_open"`
	CostHold     float64 `json:"cost_hold"`
	ProfitUnreal float64 `json:"profit_unreal"`
	LeverRate    int     `json:"lever_rate"`
	Direction    string  `json:"direction"` // buy, sell
}

// GetPositionInfo 獲取持倉信息
func (c *HuobiClient) GetPositionInfo(ctx context.Context, symbol string) ([]HuobiPositionInfo, error) {
	body := map[string]interface{}{}
	if symbol != "" {
		body["contract_code"] = symbol
	}

	data, err := c.request(ctx, "POST", "/linear-swap-api/v1/swap_position_info", nil, body)
	if err != nil {
		return nil, err
	}

	var positions []HuobiPositionInfo
	if err := json.Unmarshal(data, &positions); err != nil {
		return nil, fmt.Errorf("解析持倉資訊失败: %w", err)
	}

	return positions, nil
}

// Kline K線數據
type Kline struct {
	Id     int64   `json:"id"`
	Open   float64 `json:"open"`
	High   float64 `json:"high"`
	Low    float64 `json:"low"`
	Close  float64 `json:"close"`
	Amount float64 `json:"amount"`
	Vol    float64 `json:"vol"`
	Count  int     `json:"count"`
}

// GetKlines 獲取K線數據
func (c *HuobiClient) GetKlines(ctx context.Context, symbol, period string, size int) ([]Kline, error) {
	params := map[string]string{
		"contract_code": symbol,
		"period":        period,
	}
	if size > 0 {
		params["size"] = strconv.Itoa(size)
	}

	data, err := c.request(ctx, "GET", "/linear-swap-ex/market/history/kline", params, nil)
	if err != nil {
		return nil, err
	}

	var klines []Kline
	if err := json.Unmarshal(data, &klines); err != nil {
		return nil, fmt.Errorf("解析K線數據失败: %w", err)
	}

	return klines, nil
}

// FundingRate 资金费率
type FundingRate struct {
	Symbol       string `json:"symbol"`
	ContractCode string `json:"contract_code"`
	FundingRate  string `json:"funding_rate"`
	FundingTime  string `json:"funding_time"`
}

// GetFundingRate 獲取资金费率
func (c *HuobiClient) GetFundingRate(ctx context.Context, symbol string) (*FundingRate, error) {
	params := map[string]string{
		"contract_code": symbol,
	}

	data, err := c.request(ctx, "GET", "/linear-swap-api/v1/swap_funding_rate", params, nil)
	if err != nil {
		return nil, err
	}

	var rate FundingRate
	if err := json.Unmarshal(data, &rate); err != nil {
		return nil, fmt.Errorf("解析资金费率失败: %w", err)
	}

	return &rate, nil
}

// GetPublicMergedClose 公共 merged 行情（GET /linear-swap-ex/market/detail/merged，無需帳戶参數）
func (c *HuobiClient) GetPublicMergedClose(ctx context.Context, contractCode string) (close float64, tsMillis int64, err error) {
	u := fmt.Sprintf("%s/linear-swap-ex/market/detail/merged?contract_code=%s", c.baseURL, url.QueryEscape(contractCode))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return 0, 0, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return 0, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, 0, err
	}
	if resp.StatusCode != http.StatusOK {
		return 0, 0, fmt.Errorf("merged HTTP %s: %s", resp.Status, string(body))
	}
	var out struct {
		Status string `json:"status"`
		Tick   struct {
			Close string `json:"close"`
			Ts    int64  `json:"ts"`
		} `json:"tick"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return 0, 0, fmt.Errorf("解析 merged: %w", err)
	}
	if out.Status != "ok" {
		return 0, 0, fmt.Errorf("merged status: %s", string(body))
	}
	close, err = strconv.ParseFloat(out.Tick.Close, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("解析 close: %w", err)
	}
	return close, out.Tick.Ts, nil
}

// GetPublicDepth 公共深度（GET /linear-swap-ex/market/depth，type=step0）
func (c *HuobiClient) GetPublicDepth(ctx context.Context, contractCode string) (bids [][]float64, asks [][]float64, ts int64, err error) {
	u := fmt.Sprintf("%s/linear-swap-ex/market/depth?contract_code=%s&type=step0", c.baseURL, url.QueryEscape(contractCode))
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, nil, 0, err
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, nil, 0, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, nil, 0, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, nil, 0, fmt.Errorf("depth HTTP %s: %s", resp.Status, string(body))
	}
	var out struct {
		Status string `json:"status"`
		Tick   struct {
			Bids [][]float64 `json:"bids"`
			Asks [][]float64 `json:"asks"`
			Ts   int64       `json:"ts"`
		} `json:"tick"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, nil, 0, fmt.Errorf("解析 depth: %w", err)
	}
	if out.Status != "ok" {
		return nil, nil, 0, fmt.Errorf("depth status: %s", string(body))
	}
	return out.Tick.Bids, out.Tick.Asks, out.Tick.Ts, nil
}

func init() {
	logger.Info("📦 [Huobi Client] REST API 客戶端已初始化")
}
