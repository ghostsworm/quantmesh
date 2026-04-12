package whitebit

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/base64"
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
	MainnetRestURL = "https://whitebit.com"
	// WhiteBIT没有测试网，使用主网地址
	TestnetRestURL = "https://whitebit.com"
)

// WhiteBITClient WhiteBIT REST API 客戶端
type WhiteBITClient struct {
	apiKey     string
	secretKey  string
	baseURL    string
	useTestnet bool // WhiteBIT没有测试网，但保留此字段以保持接口一致性
	httpClient *http.Client
	nonce      int64 // nonce计数器，使用时间戳毫秒
}

// NewWhiteBITClient 創建 WhiteBIT 客戶端
func NewWhiteBITClient(apiKey, secretKey string, useTestnet bool) *WhiteBITClient {
	baseURL := MainnetRestURL
	if useTestnet {
		baseURL = TestnetRestURL
	}

	return &WhiteBITClient{
		apiKey:     apiKey,
		secretKey:  secretKey,
		baseURL:    baseURL,
		useTestnet: useTestnet,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// getNonce 獲取nonce值（使用Unix時間戳毫秒）
func (c *WhiteBITClient) getNonce() int64 {
	nonce := time.Now().UnixMilli()
	// 確保nonce遞增
	if nonce <= c.nonce {
		nonce = c.nonce + 1
	}
	c.nonce = nonce
	return nonce
}

// sign 生成签名（HMAC-SHA512，hex编码）
func (c *WhiteBITClient) sign(payload string) string {
	h := hmac.New(sha512.New, []byte(c.secretKey))
	h.Write([]byte(payload))
	return hex.EncodeToString(h.Sum(nil))
}

// request 发送 HTTP 请求
func (c *WhiteBITClient) request(ctx context.Context, method, path string, params map[string]interface{}) ([]byte, error) {
	// WhiteBIT公开API可以使用GET方法
	if method == "GET" {
		url := c.baseURL + path
		// 添加查询参数
		if len(params) > 0 {
			query := ""
			for k, v := range params {
				if query != "" {
					query += "&"
				}
				query += fmt.Sprintf("%s=%v", k, v)
			}
			if query != "" {
				if len(path) > 0 && path[len(path)-1] != '?' {
					url += "?"
				}
				url += query
			}
		}

		req, err := http.NewRequestWithContext(ctx, method, url, nil)
		if err != nil {
			return nil, fmt.Errorf("創建请求失败: %w", err)
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

		return respBody, nil
	}

	// 构建请求体
	body := make(map[string]interface{})
	body["request"] = path
	body["nonce"] = strconv.FormatInt(c.getNonce(), 10)
	body["nonceWindow"] = true // 启用时间验证

	// 添加其他参数
	for k, v := range params {
		body[k] = v
	}

	// 序列化请求体
	bodyBytes, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("序列化请求体失败: %w", err)
	}

	// Base64编码payload
	payload := base64.StdEncoding.EncodeToString(bodyBytes)

	// 生成签名
	signature := c.sign(payload)

	// 创建请求
	url := c.baseURL + path
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("創建请求失败: %w", err)
	}

	// 設置请求头
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-TXC-APIKEY", c.apiKey)
	req.Header.Set("X-TXC-PAYLOAD", payload)
	req.Header.Set("X-TXC-SIGNATURE", signature)

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
			Code    int                    `json:"code"`
			Message string                 `json:"message"`
			Errors  map[string]interface{} `json:"errors"`
		}
		if err := json.Unmarshal(respBody, &errResp); err == nil {
			return nil, fmt.Errorf("API 錯误 %d: %s", errResp.Code, errResp.Message)
		}
		return nil, fmt.Errorf("HTTP 錯误 %d: %s", resp.StatusCode, string(respBody))
	}

	// 检查响应格式
	var apiResp struct {
		Success bool            `json:"success"`
		Message string          `json:"message"`
		Result  json.RawMessage `json:"result"`
		Error   interface{}     `json:"error"`
	}

	if err := json.Unmarshal(respBody, &apiResp); err == nil {
		// 如果是标准格式
		if !apiResp.Success && apiResp.Message != "" {
			return nil, fmt.Errorf("API 錯误: %s", apiResp.Message)
		}
		if apiResp.Error != nil {
			return nil, fmt.Errorf("API 錯误: %v", apiResp.Error)
		}
		// 如果有result字段，返回result
		if len(apiResp.Result) > 0 {
			return apiResp.Result, nil
		}
	}

	// 直接返回响应体
	return respBody, nil
}

// MarketInfo 市场信息
type MarketInfo struct {
	Name       string `json:"name"`
	Stock      string `json:"stock"`
	Money      string `json:"money"`
	StockPrec  string `json:"stockPrec"`
	MoneyPrec  string `json:"moneyPrec"`
	FeePrec    string `json:"feePrec"`
	MakerFee   string `json:"makerFee"`
	TakerFee   string `json:"takerFee"`
	MinAmount  string `json:"minAmount"`
	MinTotal   string `json:"minTotal"`
	MaxTotal   string `json:"maxTotal"`
	Type       string `json:"type"` // "spot" or "futures"
	IsCollateral bool `json:"isCollateral"`
}

// GetMarkets 獲取所有市场信息
func (c *WhiteBITClient) GetMarkets(ctx context.Context) ([]MarketInfo, error) {
	data, err := c.request(ctx, "GET", "/api/v4/public/markets", nil)
	if err != nil {
		return nil, err
	}

	var markets []MarketInfo
	if err := json.Unmarshal(data, &markets); err != nil {
		return nil, fmt.Errorf("解析市场信息失败: %w", err)
	}

	return markets, nil
}

// FuturesMarket 期货市场信息
type FuturesMarket struct {
	TickerID              string  `json:"ticker_id"`
	StockCurrency         string  `json:"stock_currency"`
	MoneyCurrency         string  `json:"money_currency"`
	LastPrice             string  `json:"last_price"`
	StockVolume           string  `json:"stock_volume"`
	MoneyVolume           string  `json:"money_volume"`
	Bid                   string  `json:"bid"`
	Ask                   string  `json:"ask"`
	High                  string  `json:"high"`
	Low                   string  `json:"low"`
	ProductType           string  `json:"product_type"`
	OpenInterest           string  `json:"open_interest"`
	IndexPrice            string  `json:"index_price"`
	IndexName             string  `json:"index_name"`
	IndexCurrency         string  `json:"index_currency"`
	FundingRate           string  `json:"funding_rate"`
	NextFundingRateTimestamp string `json:"next_funding_rate_timestamp"`
	MaxLeverage           int     `json:"max_leverage"`
	FundingIntervalMinutes int    `json:"funding_interval_minutes"`
}

// FuturesMarketsResponse 期货市场列表响应
type FuturesMarketsResponse struct {
	Success bool            `json:"success"`
	Message string          `json:"message"`
	Result  []FuturesMarket `json:"result"`
}

// GetFuturesMarkets 獲取期货市场列表
func (c *WhiteBITClient) GetFuturesMarkets(ctx context.Context) ([]FuturesMarket, error) {
	data, err := c.request(ctx, "GET", "/api/v4/public/futures", nil)
	if err != nil {
		return nil, err
	}

	var resp FuturesMarketsResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("解析期货市场信息失败: %w", err)
	}

	if !resp.Success {
		return nil, fmt.Errorf("获取期货市场失败: %s", resp.Message)
	}

	return resp.Result, nil
}

// GetFuturesMarketByTicker 按 ticker_id（如 BTC_PERP）獲取單個期货市场信息（含下次資金費時間戳）
func (c *WhiteBITClient) GetFuturesMarketByTicker(ctx context.Context, tickerID string) (*FuturesMarket, error) {
	markets, err := c.GetFuturesMarkets(ctx)
	if err != nil {
		return nil, err
	}
	for i := range markets {
		if markets[i].TickerID == tickerID {
			return &markets[i], nil
		}
	}
	return nil, fmt.Errorf("未找到期货市场: %s", tickerID)
}

// Balance 余额信息
type Balance struct {
	Available string `json:"available"` // 可用余额
	Freeze    string `json:"freeze"`    // 冻结余额（在订单中）
}

// GetBalance 獲取交易账户余额
func (c *WhiteBITClient) GetBalance(ctx context.Context, ticker string) (map[string]Balance, error) {
	params := make(map[string]interface{})
	if ticker != "" {
		params["ticker"] = ticker
	}

	data, err := c.request(ctx, "POST", "/api/v4/trade-account/balance", params)
	if err != nil {
		return nil, err
	}

	var balances map[string]Balance
	if err := json.Unmarshal(data, &balances); err != nil {
		return nil, fmt.Errorf("解析余额信息失败: %w", err)
	}

	return balances, nil
}

// Order 订单信息
type Order struct {
	OrderID      int64   `json:"orderId"`
	ClientOrderID string `json:"clientOrderId"`
	Market       string  `json:"market"`
	Side         string  `json:"side"` // "buy" or "sell"
	Type         string  `json:"type"` // "limit", "market", etc.
	Timestamp    float64 `json:"timestamp"`
	DealMoney    string  `json:"dealMoney"`
	DealStock    string  `json:"dealStock"`
	Amount       string  `json:"amount"`
	Left         string  `json:"left"`
	DealFee      string  `json:"dealFee"`
	Price         string  `json:"price"`
	PostOnly     bool    `json:"postOnly"`
	IOC          bool    `json:"ioc"`
	Status       string  `json:"status"` // "OPEN", "FILLED", "CANCELED", etc.
	STP          string  `json:"stp"`
	RPI          bool    `json:"rpi"`
}

// PlaceOrder 下單
func (c *WhiteBITClient) PlaceOrder(ctx context.Context, market, side, amount, price string, clientOrderID string, postOnly, ioc bool) (*Order, error) {
	params := map[string]interface{}{
		"market":  market,
		"side":    side,
		"amount":  amount,
		"price":   price,
		"postOnly": postOnly,
		"ioc":     ioc,
	}

	if clientOrderID != "" {
		params["clientOrderId"] = clientOrderID
	}

	data, err := c.request(ctx, "POST", "/api/v4/order/new", params)
	if err != nil {
		return nil, err
	}

	var order Order
	if err := json.Unmarshal(data, &order); err != nil {
		return nil, fmt.Errorf("解析订单信息失败: %w", err)
	}

	return &order, nil
}

// BulkOrderRequest 批量订单请求
type BulkOrderRequest struct {
	Side         string `json:"side"`
	Amount       string `json:"amount"`
	Price        string `json:"price"`
	Market       string `json:"market"`
	PostOnly     bool   `json:"postOnly"`
	IOC          bool   `json:"ioc"`
	ClientOrderID string `json:"clientOrderId,omitempty"`
	RPI          bool   `json:"rpi,omitempty"`
}

// BulkOrderResult 批量订单结果
type BulkOrderResult struct {
	Result *Order                `json:"result"`
	Error  map[string]interface{} `json:"error"`
}

// BatchPlaceOrders 批量下單（最多20个）
func (c *WhiteBITClient) BatchPlaceOrders(ctx context.Context, orders []BulkOrderRequest, stopOnFail bool) ([]BulkOrderResult, error) {
	if len(orders) > 20 {
		return nil, fmt.Errorf("批量订单数量不能超过20个")
	}

	params := map[string]interface{}{
		"orders":     orders,
		"stopOnFail": stopOnFail,
	}

	data, err := c.request(ctx, "POST", "/api/v4/order/bulk", params)
	if err != nil {
		return nil, err
	}

	var results []BulkOrderResult
	if err := json.Unmarshal(data, &results); err != nil {
		return nil, fmt.Errorf("解析批量订单结果失败: %w", err)
	}

	return results, nil
}

// CancelOrder 取消訂單
func (c *WhiteBITClient) CancelOrder(ctx context.Context, market string, orderID int64, clientOrderID string) (*Order, error) {
	params := map[string]interface{}{
		"market": market,
	}

	if clientOrderID != "" {
		params["clientOrderId"] = clientOrderID
	} else {
		params["orderId"] = orderID
	}

	data, err := c.request(ctx, "POST", "/api/v4/order/cancel", params)
	if err != nil {
		return nil, err
	}

	var order Order
	if err := json.Unmarshal(data, &order); err != nil {
		return nil, fmt.Errorf("解析订单信息失败: %w", err)
	}

	return &order, nil
}

// CancelAllOrders 取消所有訂單
func (c *WhiteBITClient) CancelAllOrders(ctx context.Context, market string, orderTypes []string) error {
	params := make(map[string]interface{})
	if market != "" {
		params["market"] = market
	}
	if len(orderTypes) > 0 {
		params["type"] = orderTypes
	}

	_, err := c.request(ctx, "POST", "/api/v4/order/cancel/all", params)
	return err
}

// GetOrder 查詢訂單
func (c *WhiteBITClient) GetOrder(ctx context.Context, market string, orderID int64, clientOrderID string) (*Order, error) {
	params := map[string]interface{}{
		"market": market,
	}

	if clientOrderID != "" {
		params["clientOrderId"] = clientOrderID
	} else {
		params["orderId"] = strconv.FormatInt(orderID, 10)
	}

	data, err := c.request(ctx, "POST", "/api/v4/orders", params)
	if err != nil {
		return nil, err
	}

	var orders []Order
	if err := json.Unmarshal(data, &orders); err != nil {
		return nil, fmt.Errorf("解析订单信息失败: %w", err)
	}

	if len(orders) == 0 {
		return nil, fmt.Errorf("订单不存在")
	}

	return &orders[0], nil
}

// GetOpenOrders 查詢未完成訂單
func (c *WhiteBITClient) GetOpenOrders(ctx context.Context, market string, limit, offset int) ([]Order, error) {
	params := map[string]interface{}{
		"limit":  limit,
		"offset": offset,
	}

	if market != "" {
		params["market"] = market
	}

	data, err := c.request(ctx, "POST", "/api/v4/orders", params)
	if err != nil {
		return nil, err
	}

	var orders []Order
	if err := json.Unmarshal(data, &orders); err != nil {
		return nil, fmt.Errorf("解析订单列表失败: %w", err)
	}

	return orders, nil
}

// CollateralBalance 保证金账户余额
type CollateralBalance struct {
	Balance              string `json:"balance"`
	Borrow               string `json:"borrow"`
	AvailableWithoutBorrow string `json:"available_without_borrow"`
	AvailableWithBorrow  string `json:"available_with_borrow"`
}

// GetCollateralBalance 獲取保证金账户余额
func (c *WhiteBITClient) GetCollateralBalance(ctx context.Context, ticker string) (map[string]CollateralBalance, error) {
	params := make(map[string]interface{})
	if ticker != "" {
		params["ticker"] = ticker
	}

	data, err := c.request(ctx, "POST", "/api/v4/collateral-account/balance", params)
	if err != nil {
		return nil, err
	}

	var balances map[string]CollateralBalance
	if err := json.Unmarshal(data, &balances); err != nil {
		return nil, fmt.Errorf("解析保证金余额信息失败: %w", err)
	}

	return balances, nil
}

// Position 持仓信息
type Position struct {
	ID                int64   `json:"id"`
	Market            string  `json:"market"`
	Ctime             int64   `json:"ctime"`
	Mtime             int64   `json:"mtime"`
	Amount            string  `json:"amount"`
	AmountInMoney     string  `json:"amount_in_money"`
	BasePrice         string  `json:"base_price"`
	PnL               string  `json:"pnl"`
	LiqPrice          string  `json:"liq_price"`
	LiqStage          string  `json:"liq_stage"`
	UnrealizedFunding string  `json:"unrealized_funding"`
	Funding           string  `json:"funding"`
	Margin            string  `json:"margin"`
	FreeMargin        string  `json:"free_margin"`
	RealizedPnL       string  `json:"realized_pnl"`
	PositionSide      string  `json:"position_side"`
}

// GetPositions 獲取持仓信息
func (c *WhiteBITClient) GetPositions(ctx context.Context, market string) ([]Position, error) {
	params := make(map[string]interface{})
	if market != "" {
		params["market"] = market
	}

	data, err := c.request(ctx, "POST", "/api/v4/collateral-account/positions", params)
	if err != nil {
		return nil, err
	}

	var resp struct {
		Total   int       `json:"total"`
		Records []Position `json:"records"`
	}

	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("解析持仓信息失败: %w", err)
	}

	return resp.Records, nil
}

// FundingRateHistory 资金费率历史
type FundingRateHistory struct {
	FundingTime       string `json:"fundingTime"`
	FundingRate       string `json:"fundingRate"`
	Market            string `json:"market"`
	SettlementPrice   string `json:"settlementPrice"`
	RateCalculatedTime string `json:"rateCalculatedTime"`
}

// GetFundingRateHistory 獲取资金费率历史
func (c *WhiteBITClient) GetFundingRateHistory(ctx context.Context, market string, startDate, endDate, limit, offset int64) ([]FundingRateHistory, error) {
	params := make(map[string]interface{})

	if startDate > 0 {
		params["startDate"] = startDate
	}
	if endDate > 0 {
		params["endDate"] = endDate
	}
	if limit > 0 {
		params["limit"] = limit
	}
	if offset > 0 {
		params["offset"] = offset
	}

	data, err := c.request(ctx, "GET", fmt.Sprintf("/api/v4/public/funding-history/%s", market), params)
	if err != nil {
		return nil, err
	}

	var history []FundingRateHistory
	if err := json.Unmarshal(data, &history); err != nil {
		return nil, fmt.Errorf("解析资金费率历史失败: %w", err)
	}

	return history, nil
}

// GetFundingRate 獲取當前资金费率
func (c *WhiteBITClient) GetFundingRate(ctx context.Context, market string) (float64, error) {
	// 获取最新的资金费率历史记录
	history, err := c.GetFundingRateHistory(ctx, market, 0, 0, 1, 0)
	if err != nil {
		return 0, err
	}

	if len(history) == 0 {
		return 0, fmt.Errorf("未找到资金费率")
	}

	rate, err := strconv.ParseFloat(history[0].FundingRate, 64)
	if err != nil {
		return 0, fmt.Errorf("解析资金费率失败: %w", err)
	}

	return rate, nil
}

// Ticker 行情數據
type Ticker struct {
	BaseID     int    `json:"base_id"`
	QuoteID    int    `json:"quote_id"`
	LastPrice  string `json:"last_price"`
	QuoteVolume string `json:"quote_volume"`
	BaseVolume string `json:"base_volume"`
	IsFrozen   bool   `json:"isFrozen"`
	Change     string `json:"change"`
}

// GetTicker 獲取行情（24小时价格和成交量摘要）
func (c *WhiteBITClient) GetTicker(ctx context.Context, market string) (map[string]Ticker, error) {
	data, err := c.request(ctx, "GET", "/api/v4/public/ticker", nil)
	if err != nil {
		return nil, err
	}

	var tickers map[string]Ticker
	if err := json.Unmarshal(data, &tickers); err != nil {
		return nil, fmt.Errorf("解析行情數據失败: %w", err)
	}

	return tickers, nil
}

// OrderBook 訂單簿
type OrderBook struct {
	TickerID  string     `json:"ticker_id"`
	Timestamp int64      `json:"timestamp"`
	Asks      [][]string `json:"asks"`
	Bids      [][]string `json:"bids"`
}

// GetOrderBook 獲取訂單簿深度
func (c *WhiteBITClient) GetOrderBook(ctx context.Context, market string, limit int) (*OrderBook, error) {
	path := fmt.Sprintf("/api/v4/public/orderbook/%s", market)
	if limit > 0 {
		path += fmt.Sprintf("?limit=%d&level=2", limit)
	}

	data, err := c.request(ctx, "GET", path, nil)
	if err != nil {
		return nil, err
	}

	var orderBook OrderBook
	if err := json.Unmarshal(data, &orderBook); err != nil {
		return nil, fmt.Errorf("解析訂單簿數據失败: %w", err)
	}

	return &orderBook, nil
}

// OrderDealRecord 訂單成交明细（POST /api/v4/trade-account/order）
type OrderDealRecord struct {
	Time          float64 `json:"time"`
	Fee           string  `json:"fee"`
	Price         string  `json:"price"`
	Amount        string  `json:"amount"`
	ID            int64   `json:"id"`
	DealOrderID   int64   `json:"dealOrderId"`
	ClientOrderID string  `json:"client_order_id"`
	Role          int     `json:"role"`
	Deal          string  `json:"deal"`
	FeeAsset      string  `json:"feeAsset"`
}

// GetOrderDeals 查詢指定訂單的成交明细（需 order_id>0）
func (c *WhiteBITClient) GetOrderDeals(ctx context.Context, orderID int64, limit, offset int) ([]OrderDealRecord, error) {
	if orderID <= 0 {
		return nil, fmt.Errorf("order_id 必填")
	}
	params := map[string]interface{}{
		"order_id": orderID,
	}
	if limit > 0 {
		params["limit"] = limit
	} else {
		params["limit"] = 50
	}
	if offset > 0 {
		params["offset"] = offset
	}
	data, err := c.request(ctx, "POST", "/api/v4/trade-account/order", params)
	if err != nil {
		return nil, err
	}
	var wrap struct {
		Records []OrderDealRecord `json:"records"`
	}
	if err := json.Unmarshal(data, &wrap); err == nil && wrap.Records != nil {
		return wrap.Records, nil
	}
	var direct []OrderDealRecord
	if err := json.Unmarshal(data, &direct); err == nil {
		return direct, nil
	}
	return nil, fmt.Errorf("解析成交明细失败: %s", string(data))
}

// WebSocketToken WebSocket Token
type WebSocketToken struct {
	WebSocketToken string `json:"websocket_token"`
}

// GetWebSocketToken 獲取WebSocket Token
func (c *WhiteBITClient) GetWebSocketToken(ctx context.Context) (string, error) {
	data, err := c.request(ctx, "POST", "/api/v4/profile/websocket_token", nil)
	if err != nil {
		return "", err
	}

	var token WebSocketToken
	if err := json.Unmarshal(data, &token); err != nil {
		return "", fmt.Errorf("解析WebSocket Token失败: %w", err)
	}

	return token.WebSocketToken, nil
}

func init() {
	logger.Info("📦 [WhiteBIT Client] REST API 客戶端已初始化")
}
