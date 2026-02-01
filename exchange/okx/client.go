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

// OKXClient OKX REST API 客户端
type OKXClient struct {
	apiKey      string
	secretKey   string
	passphrase  string
	baseURL     string
	useTestnet  bool // 是否使用模拟盘
	httpClient  *http.Client
}

// NewOKXClient 创建 OKX 客户端
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
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	// 生成时间戳（ISO 8601 格式）
	timestamp := time.Now().UTC().Format("2006-01-02T15:04:05.000Z")

	// 生成签名
	bodyStr := ""
	if len(bodyBytes) > 0 {
		bodyStr = string(bodyBytes)
	}
	signature := c.sign(timestamp, method, path, bodyStr)

	// 设置请求头
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("OK-ACCESS-KEY", c.apiKey)
	req.Header.Set("OK-ACCESS-SIGN", signature)
	req.Header.Set("OK-ACCESS-TIMESTAMP", timestamp)
	req.Header.Set("OK-ACCESS-PASSPHRASE", c.passphrase)

	// 模拟盘标识
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
		return nil, fmt.Errorf("HTTP 错误 %d: %s", resp.StatusCode, string(respBody))
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
		return nil, fmt.Errorf("API 错误 %s: %s", apiResp.Code, apiResp.Msg)
	}

	return apiResp.Data, nil
}

// Instrument 合约信息
type Instrument struct {
	InstId    string `json:"instId"`
	InstType  string `json:"instType"`
	CtValCcy  string `json:"ctValCcy"`  // 合约面值计价币种
	SettleCcy string `json:"settleCcy"` // 结算币种
	TickSz    string `json:"tickSz"`    // 价格最小变动单位
	LotSz     string `json:"lotSz"`     // 数量最小变动单位
}

// GetInstruments 获取合约信息
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
		return nil, fmt.Errorf("解析合约信息失败: %w", err)
	}

	return instruments, nil
}

// PlaceOrderResult 下单结果
type PlaceOrderResult struct {
	OrdId   string `json:"ordId"`
	ClOrdId string `json:"clOrdId"`
	SCode   string `json:"sCode"`
	SMsg    string `json:"sMsg"`
}

// PlaceOrder 下单
func (c *OKXClient) PlaceOrder(ctx context.Context, order map[string]interface{}) ([]PlaceOrderResult, error) {
	data, err := c.request(ctx, "POST", "/api/v5/trade/order", order, c.useTestnet)
	if err != nil {
		return nil, err
	}

	var results []PlaceOrderResult
	if err := json.Unmarshal(data, &results); err != nil {
		return nil, fmt.Errorf("解析下单结果失败: %w", err)
	}

	return results, nil
}

// CancelOrder 取消订单
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

// BatchCancelOrders 批量取消订单
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

// OKXOrder 订单信息
type OKXOrder struct {
	OrdId     string `json:"ordId"`
	ClOrdId   string `json:"clOrdId"`
	InstId    string `json:"instId"`
	Side      string `json:"side"`
	OrdType   string `json:"ordType"`
	Px        string `json:"px"`
	Sz        string `json:"sz"`
	AccFillSz string `json:"accFillSz"` // 累计成交数量
	AvgPx     string `json:"avgPx"`     // 成交均价
	State     string `json:"state"`     // 订单状态
	UTime     string `json:"uTime"`     // 更新时间
}

// GetOrder 查询订单
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
		return nil, fmt.Errorf("解析订单信息失败: %w", err)
	}

	if len(orders) == 0 {
		return nil, fmt.Errorf("订单不存在")
	}

	return &orders[0], nil
}

// GetOpenOrders 查询未完成订单
func (c *OKXClient) GetOpenOrders(ctx context.Context, instId string) ([]OKXOrder, error) {
	path := fmt.Sprintf("/api/v5/trade/orders-pending?instType=SWAP")
	if instId != "" {
		path += "&instId=" + instId
	}

	data, err := c.request(ctx, "GET", path, nil, c.useTestnet)
	if err != nil {
		return nil, err
	}

	var orders []OKXOrder
	if err := json.Unmarshal(data, &orders); err != nil {
		return nil, fmt.Errorf("解析订单列表失败: %w", err)
	}

	return orders, nil
}

// BalanceDetail 余额详情
type BalanceDetail struct {
	Ccy      string `json:"ccy"`      // 币种
	Eq       string `json:"eq"`       // 币种总权益
	AvailBal string `json:"availBal"` // 可用余额
}

// Balance 账户余额
type Balance struct {
	TotalEq string          `json:"totalEq"` // 总权益
	Details []BalanceDetail `json:"details"` // 币种详情
}

// GetBalance 获取账户余额
func (c *OKXClient) GetBalance(ctx context.Context) ([]Balance, error) {
	data, err := c.request(ctx, "GET", "/api/v5/account/balance", nil, c.useTestnet)
	if err != nil {
		return nil, err
	}

	var balances []Balance
	if err := json.Unmarshal(data, &balances); err != nil {
		return nil, fmt.Errorf("解析余额信息失败: %w", err)
	}

	return balances, nil
}

// OKXPosition 持仓信息
type OKXPosition struct {
	InstId   string `json:"instId"`
	Pos      string `json:"pos"`      // 持仓数量
	AvgPx    string `json:"avgPx"`    // 开仓均价
	MarkPx   string `json:"markPx"`   // 标记价格
	Upl      string `json:"upl"`      // 未实现收益
	Lever    string `json:"lever"`    // 杠杆倍数
	MgnMode  string `json:"mgnMode"`  // 保证金模式
	PosSide  string `json:"posSide"`  // 持仓方向
	UplRatio string `json:"uplRatio"` // 未实现收益率
}

// GetPositions 获取持仓信息
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
		return nil, fmt.Errorf("解析持仓信息失败: %w", err)
	}

	return positions, nil
}

// Kline K线数据
type Kline struct {
	Ts     string `json:"ts"`     // 时间戳
	O      string `json:"o"`      // 开盘价
	H      string `json:"h"`      // 最高价
	L      string `json:"l"`      // 最低价
	C      string `json:"c"`      // 收盘价
	Vol    string `json:"vol"`    // 成交量
	VolCcy string `json:"volCcy"` // 成交额
}

// GetKlines 获取K线数据
func (c *OKXClient) GetKlines(ctx context.Context, instId, bar string, limit int) ([]Kline, error) {
	path := fmt.Sprintf("/api/v5/market/candles?instId=%s&bar=%s", instId, bar)
	if limit > 0 {
		path += fmt.Sprintf("&limit=%d", limit)
	}

	data, err := c.request(ctx, "GET", path, nil, c.useTestnet)
	if err != nil {
		return nil, err
	}

	// OKX 返回的是二维数组
	var rawKlines [][]interface{}
	if err := json.Unmarshal(data, &rawKlines); err != nil {
		return nil, fmt.Errorf("解析K线数据失败: %w", err)
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
	FundingRate string `json:"fundingRate"` // 当前资金费率
	NextTime    string `json:"fundingTime"` // 下次结算时间
}

// GetFundingRate 获取资金费率
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

// Ticker 行情数据
type Ticker struct {
	InstId string `json:"instId"`
	Last   string `json:"last"` // 最新价格
}

// GetTicker 获取行情
func (c *OKXClient) GetTicker(ctx context.Context, instId string) (*Ticker, error) {
	path := fmt.Sprintf("/api/v5/market/ticker?instId=%s", instId)

	data, err := c.request(ctx, "GET", path, nil, c.useTestnet)
	if err != nil {
		return nil, err
	}

	var tickers []Ticker
	if err := json.Unmarshal(data, &tickers); err != nil {
		return nil, fmt.Errorf("解析行情数据失败: %w", err)
	}

	if len(tickers) == 0 {
		return nil, fmt.Errorf("未找到行情数据")
	}

	return &tickers[0], nil
}

// OKXOrderBookResponse OKX 订单簿响应结构
type OKXOrderBookResponse struct {
	InstID  string     `json:"instId"`  // 交易对
	Asks    [][]string `json:"asks"`    // 卖盘 [[价格, 数量, 0, 数量], ...]
	Bids    [][]string `json:"bids"`    // 买盘 [[价格, 数量, 0, 数量], ...]
	TS      string     `json:"ts"`       // 时间戳（毫秒）
}

// GetOrderBook 获取订单簿深度
func (c *OKXClient) GetOrderBook(ctx context.Context, instId string, sz int) (*OKXOrderBookResponse, error) {
	path := fmt.Sprintf("/api/v5/market/books?instId=%s&sz=%d", instId, sz)

	data, err := c.request(ctx, "GET", path, nil, false) // 订单簿是公开接口，不需要模拟盘标识
	if err != nil {
		return nil, err
	}

	var result struct {
		Data []OKXOrderBookResponse `json:"data"`
	}

	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("解析订单簿数据失败: %w", err)
	}

	if len(result.Data) == 0 {
		return nil, fmt.Errorf("订单簿数据为空")
	}

	return &result.Data[0], nil
}

func init() {
	logger.Info("📦 [OKX Client] REST API 客户端已初始化")
}
