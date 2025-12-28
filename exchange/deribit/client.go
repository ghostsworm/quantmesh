package deribit

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
	DeribitMainnetBaseURL = "https://www.deribit.com"       // Deribit 主网
	DeribitTestnetBaseURL = "https://test.deribit.com"      // Deribit 测试网
)

// DeribitClient Deribit 客户端
type DeribitClient struct {
	apiKey     string
	secretKey  string
	baseURL    string
	httpClient *http.Client
	isTestnet  bool
	accessToken string
	refreshToken string
}

// NewDeribitClient 创建 Deribit 客户端
func NewDeribitClient(apiKey, secretKey string, isTestnet bool) *DeribitClient {
	baseURL := DeribitMainnetBaseURL
	if isTestnet {
		baseURL = DeribitTestnetBaseURL
	}

	return &DeribitClient{
		apiKey:     apiKey,
		secretKey:  secretKey,
		baseURL:    baseURL,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		isTestnet:  isTestnet,
	}
}

// JSONRPCRequest JSON-RPC 请求
type JSONRPCRequest struct {
	JSONRPC string                 `json:"jsonrpc"`
	ID      int64                  `json:"id"`
	Method  string                 `json:"method"`
	Params  map[string]interface{} `json:"params"`
}

// JSONRPCResponse JSON-RPC 响应
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError RPC 错误
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// sendRequest 发送 JSON-RPC 请求
func (c *DeribitClient) sendRequest(ctx context.Context, method string, params map[string]interface{}) (json.RawMessage, error) {
	reqID := time.Now().UnixNano()
	
	rpcReq := JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      reqID,
		Method:  method,
		Params:  params,
	}

	reqBody, err := json.Marshal(rpcReq)
	if err != nil {
		return nil, fmt.Errorf("marshal request error: %w", err)
	}

	reqURL := c.baseURL + "/api/v2/public/" + method
	if c.accessToken != "" {
		reqURL = c.baseURL + "/api/v2/private/" + method
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("create request error: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	if c.accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.accessToken)
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

	var rpcResp JSONRPCResponse
	if err := json.Unmarshal(respBody, &rpcResp); err != nil {
		return nil, fmt.Errorf("unmarshal response error: %w", err)
	}

	if rpcResp.Error != nil {
		return nil, fmt.Errorf("RPC error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}

	return rpcResp.Result, nil
}

// Authenticate 认证
func (c *DeribitClient) Authenticate(ctx context.Context) error {
	timestamp := strconv.FormatInt(time.Now().UnixMilli(), 10)
	nonce := timestamp
	data := ""

	// 签名字符串：timestamp + "\n" + nonce + "\n" + data
	signStr := timestamp + "\n" + nonce + "\n" + data
	
	h := hmac.New(sha256.New, []byte(c.secretKey))
	h.Write([]byte(signStr))
	signature := hex.EncodeToString(h.Sum(nil))

	params := map[string]interface{}{
		"grant_type": "client_signature",
		"client_id":  c.apiKey,
		"timestamp":  timestamp,
		"signature":  signature,
		"nonce":      nonce,
		"data":       data,
	}

	result, err := c.sendRequest(ctx, "public/auth", params)
	if err != nil {
		return err
	}

	var authResp struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	if err := json.Unmarshal(result, &authResp); err != nil {
		return fmt.Errorf("unmarshal auth response error: %w", err)
	}

	c.accessToken = authResp.AccessToken
	c.refreshToken = authResp.RefreshToken

	logger.Info("Deribit authenticated successfully")
	return nil
}

// GetInstruments 获取交易对信息
func (c *DeribitClient) GetInstruments(ctx context.Context, currency string) ([]Instrument, error) {
	params := map[string]interface{}{
		"currency": currency,
		"kind":     "future",
	}

	result, err := c.sendRequest(ctx, "public/get_instruments", params)
	if err != nil {
		return nil, err
	}

	var instruments []Instrument
	if err := json.Unmarshal(result, &instruments); err != nil {
		return nil, fmt.Errorf("unmarshal instruments error: %w", err)
	}

	return instruments, nil
}

// Buy 买入
func (c *DeribitClient) Buy(ctx context.Context, req *OrderRequest) (*OrderResponse, error) {
	params := map[string]interface{}{
		"instrument_name": req.InstrumentName,
		"amount":          req.Amount,
		"type":            req.Type,
	}

	if req.Price > 0 {
		params["price"] = req.Price
	}
	if req.Label != "" {
		params["label"] = req.Label
	}

	result, err := c.sendRequest(ctx, "private/buy", params)
	if err != nil {
		return nil, err
	}

	var orderResp struct {
		Order OrderResponse `json:"order"`
	}
	if err := json.Unmarshal(result, &orderResp); err != nil {
		return nil, fmt.Errorf("unmarshal order response error: %w", err)
	}

	logger.Info("Deribit buy order placed: %s", orderResp.Order.OrderID)
	return &orderResp.Order, nil
}

// Sell 卖出
func (c *DeribitClient) Sell(ctx context.Context, req *OrderRequest) (*OrderResponse, error) {
	params := map[string]interface{}{
		"instrument_name": req.InstrumentName,
		"amount":          req.Amount,
		"type":            req.Type,
	}

	if req.Price > 0 {
		params["price"] = req.Price
	}
	if req.Label != "" {
		params["label"] = req.Label
	}

	result, err := c.sendRequest(ctx, "private/sell", params)
	if err != nil {
		return nil, err
	}

	var orderResp struct {
		Order OrderResponse `json:"order"`
	}
	if err := json.Unmarshal(result, &orderResp); err != nil {
		return nil, fmt.Errorf("unmarshal order response error: %w", err)
	}

	logger.Info("Deribit sell order placed: %s", orderResp.Order.OrderID)
	return &orderResp.Order, nil
}

// CancelOrder 取消订单
func (c *DeribitClient) CancelOrder(ctx context.Context, orderID string) error {
	params := map[string]interface{}{
		"order_id": orderID,
	}

	_, err := c.sendRequest(ctx, "private/cancel", params)
	if err != nil {
		return err
	}

	logger.Info("Deribit order cancelled: %s", orderID)
	return nil
}

// GetOrderState 查询订单
func (c *DeribitClient) GetOrderState(ctx context.Context, orderID string) (*OrderInfo, error) {
	params := map[string]interface{}{
		"order_id": orderID,
	}

	result, err := c.sendRequest(ctx, "private/get_order_state", params)
	if err != nil {
		return nil, err
	}

	var orderInfo OrderInfo
	if err := json.Unmarshal(result, &orderInfo); err != nil {
		return nil, fmt.Errorf("unmarshal order info error: %w", err)
	}

	return &orderInfo, nil
}

// GetOpenOrders 获取活跃订单
func (c *DeribitClient) GetOpenOrders(ctx context.Context, instrumentName string) ([]OrderInfo, error) {
	params := map[string]interface{}{}
	
	if instrumentName != "" {
		params["instrument_name"] = instrumentName
	}

	result, err := c.sendRequest(ctx, "private/get_open_orders", params)
	if err != nil {
		return nil, err
	}

	var orders []OrderInfo
	if err := json.Unmarshal(result, &orders); err != nil {
		return nil, fmt.Errorf("unmarshal orders error: %w", err)
	}

	return orders, nil
}

// GetAccountSummary 获取账户信息
func (c *DeribitClient) GetAccountSummary(ctx context.Context, currency string) (*AccountSummary, error) {
	params := map[string]interface{}{
		"currency": currency,
	}

	result, err := c.sendRequest(ctx, "private/get_account_summary", params)
	if err != nil {
		return nil, err
	}

	var account AccountSummary
	if err := json.Unmarshal(result, &account); err != nil {
		return nil, fmt.Errorf("unmarshal account error: %w", err)
	}

	return &account, nil
}

// GetPositions 获取持仓
func (c *DeribitClient) GetPositions(ctx context.Context, currency string) ([]PositionInfo, error) {
	params := map[string]interface{}{
		"currency": currency,
	}

	result, err := c.sendRequest(ctx, "private/get_positions", params)
	if err != nil {
		return nil, err
	}

	var positions []PositionInfo
	if err := json.Unmarshal(result, &positions); err != nil {
		return nil, fmt.Errorf("unmarshal positions error: %w", err)
	}

	return positions, nil
}

// GetTicker 获取行情
func (c *DeribitClient) GetTicker(ctx context.Context, instrumentName string) (*TickerInfo, error) {
	params := map[string]interface{}{
		"instrument_name": instrumentName,
	}

	result, err := c.sendRequest(ctx, "public/ticker", params)
	if err != nil {
		return nil, err
	}

	var ticker TickerInfo
	if err := json.Unmarshal(result, &ticker); err != nil {
		return nil, fmt.Errorf("unmarshal ticker error: %w", err)
	}

	return &ticker, nil
}

// GetTradingViewChartData 获取 K线数据
func (c *DeribitClient) GetTradingViewChartData(ctx context.Context, instrumentName, resolution string, startTime, endTime int64) (*ChartData, error) {
	params := map[string]interface{}{
		"instrument_name": instrumentName,
		"resolution":      resolution,
		"start_timestamp": startTime,
		"end_timestamp":   endTime,
	}

	result, err := c.sendRequest(ctx, "public/get_tradingview_chart_data", params)
	if err != nil {
		return nil, err
	}

	var chartData ChartData
	if err := json.Unmarshal(result, &chartData); err != nil {
		return nil, fmt.Errorf("unmarshal chart data error: %w", err)
	}

	return &chartData, nil
}

// 数据结构定义

type Instrument struct {
	InstrumentName    string  `json:"instrument_name"`
	Kind              string  `json:"kind"`
	BaseCurrency      string  `json:"base_currency"`
	QuoteCurrency     string  `json:"quote_currency"`
	SettlementPeriod  string  `json:"settlement_period"`
	ContractSize      float64 `json:"contract_size"`
	MinTradeAmount    float64 `json:"min_trade_amount"`
	TickSize          float64 `json:"tick_size"`
	IsActive          bool    `json:"is_active"`
	ExpirationTimestamp int64 `json:"expiration_timestamp"`
}

type OrderRequest struct {
	InstrumentName string
	Amount         float64
	Type           string  // limit, market
	Price          float64
	Label          string
}

type OrderResponse struct {
	OrderID        string  `json:"order_id"`
	InstrumentName string  `json:"instrument_name"`
	Direction      string  `json:"direction"`
	Price          float64 `json:"price"`
	Amount         float64 `json:"amount"`
	FilledAmount   float64 `json:"filled_amount"`
	OrderState     string  `json:"order_state"`
	Label          string  `json:"label"`
}

type OrderInfo struct {
	OrderID        string  `json:"order_id"`
	InstrumentName string  `json:"instrument_name"`
	Direction      string  `json:"direction"`
	Price          float64 `json:"price"`
	Amount         float64 `json:"amount"`
	FilledAmount   float64 `json:"filled_amount"`
	AveragePrice   float64 `json:"average_price"`
	OrderState     string  `json:"order_state"`
	OrderType      string  `json:"order_type"`
	Label          string  `json:"label"`
	CreationTimestamp int64 `json:"creation_timestamp"`
	LastUpdateTimestamp int64 `json:"last_update_timestamp"`
}

type AccountSummary struct {
	Currency            string  `json:"currency"`
	Balance             float64 `json:"balance"`
	Equity              float64 `json:"equity"`
	AvailableFunds      float64 `json:"available_funds"`
	AvailableWithdrawalFunds float64 `json:"available_withdrawal_funds"`
	InitialMargin       float64 `json:"initial_margin"`
	MaintenanceMargin   float64 `json:"maintenance_margin"`
	MarginBalance       float64 `json:"margin_balance"`
	TotalPL             float64 `json:"total_pl"`
}

type PositionInfo struct {
	InstrumentName     string  `json:"instrument_name"`
	Size               float64 `json:"size"`
	Direction          string  `json:"direction"`
	AveragePrice       float64 `json:"average_price"`
	MarkPrice          float64 `json:"mark_price"`
	IndexPrice         float64 `json:"index_price"`
	InitialMargin      float64 `json:"initial_margin"`
	MaintenanceMargin  float64 `json:"maintenance_margin"`
	TotalProfitLoss    float64 `json:"total_profit_loss"`
	RealizedProfitLoss float64 `json:"realized_profit_loss"`
	FloatingProfitLoss float64 `json:"floating_profit_loss"`
	Leverage           int     `json:"leverage"`
}

type TickerInfo struct {
	InstrumentName string  `json:"instrument_name"`
	LastPrice      float64 `json:"last_price"`
	BestBidPrice   float64 `json:"best_bid_price"`
	BestAskPrice   float64 `json:"best_ask_price"`
	MarkPrice      float64 `json:"mark_price"`
	IndexPrice     float64 `json:"index_price"`
	Stats          struct {
		Volume float64 `json:"volume"`
		High   float64 `json:"high"`
		Low    float64 `json:"low"`
	} `json:"stats"`
	State     string `json:"state"`
	Timestamp int64  `json:"timestamp"`
}

type ChartData struct {
	Status string    `json:"status"`
	Ticks  []int64   `json:"ticks"`
	Open   []float64 `json:"open"`
	High   []float64 `json:"high"`
	Low    []float64 `json:"low"`
	Close  []float64 `json:"close"`
	Volume []float64 `json:"volume"`
}

