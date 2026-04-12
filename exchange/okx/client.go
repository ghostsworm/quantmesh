package okx

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
	"time"

	"quantmesh/logger"
)

const (
	// 主网 API 地址
	MainnetRestURL = "https://www.okx.com"
	// 模拟盘 API 地址
	TestnetRestURL = "https://www.okx.com"
)

// OKXClient OKX REST API 客戶端
type OKXClient struct {
	apiKey      string
	secretKey   string
	passphrase  string
	baseURL     string
	useTestnet  bool // 是否使用模拟盘
	httpClient  *http.Client
}

// NewOKXClient 創建 OKX 客戶端
func NewOKXClient(apiKey, secretKey, passphrase string, useTestnet bool) *OKXClient {
	baseURL := MainnetRestURL
	if useTestnet {
		baseURL = TestnetRestURL
	}

	return &OKXClient{
		apiKey:     apiKey,
		secretKey:  secretKey,
		passphrase: passphrase,
		baseURL:    baseURL,
		useTestnet: useTestnet,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// sign 生成签名
func (c *OKXClient) sign(timestamp, method, requestPath, body string) string {
	message := timestamp + method + requestPath + body
	h := hmac.New(sha256.New, []byte(c.secretKey))
	h.Write([]byte(message))
	return base64.StdEncoding.EncodeToString(h.Sum(nil))
}

// request 发送 HTTP 请求
func (c *OKXClient) request(ctx context.Context, method, path string, body interface{}, isSimulated bool) ([]byte, error) {
	var bodyBytes []byte
	var err error

	if body != nil {
		bodyBytes, err = json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("序列化请求体失败: %w", err)
		}
	}

	url := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, method, url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("創建请求失败: %w", err)
	}

	// 生成時间戳（ISO 8601 格式）
	timestamp := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")

	// 生成签名
	bodyStr := ""
	if len(bodyBytes) > 0 {
		bodyStr = string(bodyBytes)
	}
	signature := c.sign(timestamp, method, path, bodyStr)

	// 設置请求头
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("OK-ACCESS-KEY", c.apiKey)
	req.Header.Set("OK-ACCESS-SIGN", signature)
	req.Header.Set("OK-ACCESS-TIMESTAMP", timestamp)
	req.Header.Set("OK-ACCESS-PASSPHRASE", c.passphrase)

	// 模拟盘標识
	if isSimulated {
		req.Header.Set("x-simulated-trading", "1")
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
		return nil, fmt.Errorf("HTTP 錯误 %d: %s", resp.StatusCode, string(respBody))
	}

	// 检查 OKX API 响应
	var apiResp struct {
		Code string          `json:"code"`
		Msg  string          `json:"msg"`
		Data json.RawMessage `json:"data"`
	}

	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w", err)
	}

	if apiResp.Code != "0" {
		// 附帶 data 便於排查（例如 OKX 外層 code=1 時 data 內常有 sCode/sMsg）
		dataStr := string(apiResp.Data)
		if dataStr == "" || dataStr == "null" {
			return nil, fmt.Errorf("API 錯误 %s: %s", apiResp.Code, apiResp.Msg)
		}
		return nil, fmt.Errorf("API 錯误 %s: %s; data=%s", apiResp.Code, apiResp.Msg, dataStr)
	}

	return apiResp.Data, nil
}

// Instrument 合約信息
type Instrument struct {
	InstId    string `json:"instId"`
	InstType  string `json:"instType"`
	CtValCcy  string `json:"ctValCcy"`  // 合約面值计價币种
	SettleCcy string `json:"settleCcy"` // 結算币种
	TickSz    string `json:"tickSz"`    // 價格最小变动單位
	LotSz     string `json:"lotSz"`     // 數量最小变动單位
	MinSz     string `json:"minSz"`     // 最小下單數量（現貨/合約標的，單位為張或基礎幣）
}

// GetInstruments 獲取合約信息
func (c *OKXClient) GetInstruments(ctx context.Context, instType, instId string) ([]Instrument, error) {
	path := fmt.Sprintf("/api/v5/public/instruments?instType=%s", instType)
	if instId != "" {
		path += "&instId=" + instId
	}

	data, err := c.request(ctx, "GET", path, nil, c.useTestnet)
	if err != nil {
		return nil, err
	}

	var instruments []Instrument
	if err := json.Unmarshal(data, &instruments); err != nil {
		return nil, fmt.Errorf("解析合約信息失败: %w", err)
	}

	return instruments, nil
}

// PlaceOrderResult 下單結果
type PlaceOrderResult struct {
	OrdId   string `json:"ordId"`
	ClOrdId string `json:"clOrdId"`
	SCode   string `json:"sCode"`
	SMsg    string `json:"sMsg"`
}

// PlaceOrder 下單
func (c *OKXClient) PlaceOrder(ctx context.Context, order map[string]interface{}) ([]PlaceOrderResult, error) {
	data, err := c.request(ctx, "POST", "/api/v5/trade/order", order, c.useTestnet)
	if err != nil {
		return nil, err
	}

	var results []PlaceOrderResult
	if err := json.Unmarshal(data, &results); err != nil {
		return nil, fmt.Errorf("解析下單結果失败: %w", err)
	}

	return results, nil
}

// CancelOrder 取消訂單
func (c *OKXClient) CancelOrder(ctx context.Context, instId, ordId, clOrdId string) error {
	body := map[string]interface{}{
		"instId": instId,
	}

	if ordId != "" {
		body["ordId"] = ordId
	}
	if clOrdId != "" {
		body["clOrdId"] = clOrdId
	}

	_, err := c.request(ctx, "POST", "/api/v5/trade/cancel-order", body, c.useTestnet)
	return err
}

// BatchCancelOrders 批量取消訂單
func (c *OKXClient) BatchCancelOrders(ctx context.Context, instId string, orderIds []string) error {
	orders := make([]map[string]interface{}, len(orderIds))
	for i, ordId := range orderIds {
		orders[i] = map[string]interface{}{
			"instId": instId,
			"ordId":  ordId,
		}
	}

	_, err := c.request(ctx, "POST", "/api/v5/trade/cancel-batch-orders", orders, c.useTestnet)
	return err
}

// OKXOrder 订單信息
type OKXOrder struct {
	OrdId     string `json:"ordId"`
	ClOrdId   string `json:"clOrdId"`
	InstId    string `json:"instId"`
	Side      string `json:"side"`
	OrdType   string `json:"ordType"`
	Px        string `json:"px"`
	Sz        string `json:"sz"`
	AccFillSz string `json:"accFillSz"` // 累计成交數量
	AvgPx     string `json:"avgPx"`     // 成交均價
	State     string `json:"state"`     // 订單状態
	UTime     string `json:"uTime"`     // 更新時间
}

// GetOrder 查詢訂單
func (c *OKXClient) GetOrder(ctx context.Context, instId, ordId, clOrdId string) (*OKXOrder, error) {
	path := fmt.Sprintf("/api/v5/trade/order?instId=%s", instId)
	if ordId != "" {
		path += "&ordId=" + ordId
	}
	if clOrdId != "" {
		path += "&clOrdId=" + clOrdId
	}

	data, err := c.request(ctx, "GET", path, nil, c.useTestnet)
	if err != nil {
		return nil, err
	}

	var orders []OKXOrder
	if err := json.Unmarshal(data, &orders); err != nil {
		return nil, fmt.Errorf("解析订單信息失败: %w", err)
	}

	if len(orders) == 0 {
		return nil, fmt.Errorf("订單不存在")
	}

	return &orders[0], nil
}

// GetOpenOrders 查詢未完成订單（合約 SWAP）
func (c *OKXClient) GetOpenOrders(ctx context.Context, instId string) ([]OKXOrder, error) {
	return c.GetOpenOrdersByInstType(ctx, "SWAP", instId)
}

// GetOpenOrdersByInstType 按 instType 查詢未完成订單（SWAP/SPOT）
func (c *OKXClient) GetOpenOrdersByInstType(ctx context.Context, instType, instId string) ([]OKXOrder, error) {
	path := fmt.Sprintf("/api/v5/trade/orders-pending?instType=%s", instType)
	if instId != "" {
		path += "&instId=" + instId
	}
	data, err := c.request(ctx, "GET", path, nil, c.useTestnet)
	if err != nil {
		return nil, err
	}
	var orders []OKXOrder
	if err := json.Unmarshal(data, &orders); err != nil {
		return nil, fmt.Errorf("解析订單列表失败: %w", err)
	}
	return orders, nil
}

// BalanceDetail 餘額详情
type BalanceDetail struct {
	Ccy      string `json:"ccy"`      // 币种
	Eq       string `json:"eq"`       // 币种總权益
	AvailBal string `json:"availBal"` // 可用餘額
}

// Balance 账戶餘額
type Balance struct {
	TotalEq string          `json:"totalEq"` // 總权益
	Details []BalanceDetail `json:"details"` // 币种详情
}

// GetBalance 獲取帳戶餘額
func (c *OKXClient) GetBalance(ctx context.Context) ([]Balance, error) {
	data, err := c.request(ctx, "GET", "/api/v5/account/balance", nil, c.useTestnet)
	if err != nil {
		return nil, err
	}

	var balances []Balance
	if err := json.Unmarshal(data, &balances); err != nil {
		return nil, fmt.Errorf("解析餘額信息失败: %w", err)
	}

	return balances, nil
}

// OKXPosition 持倉資訊
type OKXPosition struct {
	InstId   string `json:"instId"`
	Pos      string `json:"pos"`      // 持倉數量
	AvgPx    string `json:"avgPx"`    // 开倉均價
	MarkPx   string `json:"markPx"`   // 標記價格
	Upl      string `json:"upl"`      // 未實現收益
	Lever    string `json:"lever"`    // 杠杆倍數
	MgnMode  string `json:"mgnMode"`  // 保证金模式
	PosSide  string `json:"posSide"`  // 持倉方向
	UplRatio string `json:"uplRatio"` // 未實現收益率
}

// GetPositions 獲取持倉信息
func (c *OKXClient) GetPositions(ctx context.Context, instId string) ([]OKXPosition, error) {
	path := "/api/v5/account/positions?instType=SWAP"
	if instId != "" {
		path += "&instId=" + instId
	}

	data, err := c.request(ctx, "GET", path, nil, c.useTestnet)
	if err != nil {
		return nil, err
	}

	var positions []OKXPosition
	if err := json.Unmarshal(data, &positions); err != nil {
		return nil, fmt.Errorf("解析持倉資訊失败: %w", err)
	}

	return positions, nil
}

// Kline K線數據
type Kline struct {
	Ts     string `json:"ts"`     // 時间戳
	O      string `json:"o"`      // 开盘價
	H      string `json:"h"`      // 最高價
	L      string `json:"l"`      // 最低價
	C      string `json:"c"`      // 收盘價
	Vol    string `json:"vol"`    // 成交量
	VolCcy string `json:"volCcy"` // 成交額
}

// GetKlines 獲取K線數據
func (c *OKXClient) GetKlines(ctx context.Context, instId, bar string, limit int) ([]Kline, error) {
	path := fmt.Sprintf("/api/v5/market/candles?instId=%s&bar=%s", instId, bar)
	if limit > 0 {
		path += fmt.Sprintf("&limit=%d", limit)
	}

	data, err := c.request(ctx, "GET", path, nil, c.useTestnet)
	if err != nil {
		return nil, err
	}

	// OKX 返回的是二维數组
	var rawKlines [][]interface{}
	if err := json.Unmarshal(data, &rawKlines); err != nil {
		return nil, fmt.Errorf("解析K線數據失败: %w", err)
	}

	klines := make([]Kline, 0, len(rawKlines))
	for _, raw := range rawKlines {
		if len(raw) < 7 {
			continue
		}

		kline := Kline{
			Ts:     fmt.Sprintf("%v", raw[0]),
			O:      fmt.Sprintf("%v", raw[1]),
			H:      fmt.Sprintf("%v", raw[2]),
			L:      fmt.Sprintf("%v", raw[3]),
			C:      fmt.Sprintf("%v", raw[4]),
			Vol:    fmt.Sprintf("%v", raw[5]),
			VolCcy: fmt.Sprintf("%v", raw[6]),
		}
		klines = append(klines, kline)
	}

	return klines, nil
}

// FundingRate 资金费率
type FundingRate struct {
	InstId      string `json:"instId"`
	FundingRate string `json:"fundingRate"` // 當前资金费率
	NextTime    string `json:"fundingTime"` // 下次結算時间
}

// GetFundingRate 獲取资金费率
func (c *OKXClient) GetFundingRate(ctx context.Context, instId string) (*FundingRate, error) {
	path := fmt.Sprintf("/api/v5/public/funding-rate?instId=%s", instId)

	data, err := c.request(ctx, "GET", path, nil, c.useTestnet)
	if err != nil {
		return nil, err
	}

	var rates []FundingRate
	if err := json.Unmarshal(data, &rates); err != nil {
		return nil, fmt.Errorf("解析资金费率失败: %w", err)
	}

	if len(rates) == 0 {
		return nil, fmt.Errorf("未找到资金费率")
	}

	return &rates[0], nil
}

// Ticker 行情數據
type Ticker struct {
	InstId string `json:"instId"`
	Last   string `json:"last"` // 最新價格
}

// GetTicker 獲取行情
func (c *OKXClient) GetTicker(ctx context.Context, instId string) (*Ticker, error) {
	path := fmt.Sprintf("/api/v5/market/ticker?instId=%s", instId)

	data, err := c.request(ctx, "GET", path, nil, c.useTestnet)
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

// OKXOrderBookResponse OKX 订單簿响应結構
type OKXOrderBookResponse struct {
	InstID  string     `json:"instId"`  // 交易對
	Asks    [][]string `json:"asks"`    // 賣盘 [[價格, 數量, 0, 數量], ...]
	Bids    [][]string `json:"bids"`    // 買盘 [[價格, 數量, 0, 數量], ...]
	TS      string     `json:"ts"`       // 時间戳（毫秒）
}

// GetOrderBook 獲取訂單簿深度
func (c *OKXClient) GetOrderBook(ctx context.Context, instId string, sz int) (*OKXOrderBookResponse, error) {
	path := fmt.Sprintf("/api/v5/market/books?instId=%s&sz=%d", instId, sz)

	data, err := c.request(ctx, "GET", path, nil, false) // 订單簿是公开接口，不需要模拟盘標识
	if err != nil {
		return nil, err
	}

	// request() 已剝離外層 envelope，此處 data 即 OKX 的 data 欄位：對 books 接口為 **數組** [{instId,bids,asks,ts}]
	var rows []OKXOrderBookResponse
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, fmt.Errorf("解析订單簿數據失败: %w", err)
	}

	if len(rows) == 0 {
		return nil, fmt.Errorf("订單簿數據為空")
	}

	return &rows[0], nil
}

// OKXTradeFill REST /api/v5/trade/fills 單筆成交
type OKXTradeFill struct {
	InstId  string `json:"instId"`
	OrdId   string `json:"ordId"`
	TradeId string `json:"tradeId"`
	Side    string `json:"side"`
	FillSz  string `json:"fillSz"`
	FillPx  string `json:"fillPx"`
	Fee     string `json:"fee"`
	FeeCcy  string `json:"feeCcy"`
	Ts      string `json:"ts"`
}

// GetTradeFills 查詢成交明細（現貨 instType=SPOT / 合約 SWAP 等）
func (c *OKXClient) GetTradeFills(ctx context.Context, instType, instId, ordId string) ([]OKXTradeFill, error) {
	path := fmt.Sprintf("/api/v5/trade/fills?instType=%s", instType)
	if instId != "" {
		path += "&instId=" + instId
	}
	if ordId != "" {
		path += "&ordId=" + ordId
	}
	data, err := c.request(ctx, "GET", path, nil, c.useTestnet)
	if err != nil {
		return nil, err
	}
	var fills []OKXTradeFill
	if err := json.Unmarshal(data, &fills); err != nil {
		return nil, fmt.Errorf("解析成交明細失败: %w", err)
	}
	return fills, nil
}

// AssetTransfer POST /api/v5/asset/transfer（內部劃轉）
func (c *OKXClient) AssetTransfer(ctx context.Context, body map[string]interface{}) (string, error) {
	data, err := c.request(ctx, "POST", "/api/v5/asset/transfer", body, c.useTestnet)
	if err != nil {
		return "", err
	}
	var rows []struct {
		TransId string `json:"transId"`
	}
	if err := json.Unmarshal(data, &rows); err != nil {
		return "", fmt.Errorf("解析劃轉結果失败: %w", err)
	}
	if len(rows) == 0 {
		return "", fmt.Errorf("劃轉結果為空")
	}
	return rows[0].TransId, nil
}

func init() {
	logger.Info("📦 [OKX Client] REST API 客戶端已初始化")
}
