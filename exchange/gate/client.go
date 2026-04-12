package gate

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"
)

// Client Gate.io HTTP 客戶端
type Client struct {
	httpClient *http.Client
	signer     *Signer
	baseURL    string
}

// NewClient 創建 Gate.io 客戶端
func NewClient(apiKey, secretKey string, testnet bool) *Client {
	baseURL := GateBaseURL
	if testnet {
		baseURL = GateTestnetBaseURL
	}
	return &Client{
		httpClient: &http.Client{Timeout: 10 * time.Second},
		signer:     NewSigner(apiKey, secretKey),
		baseURL:    baseURL,
	}
}

// DoRequest 发送 HTTP 请求（带签名）
func (c *Client) DoRequest(ctx context.Context, method, path, queryString string, body interface{}) ([]byte, error) {
	var bodyBytes []byte
	var err error

	if body != nil {
		bodyBytes, err = json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("序列化请求体失败: %w", err)
		}
	}

	timestamp := c.signer.GetTimestamp()
	bodyStr := string(bodyBytes)

	// 签名時使用完整的API路径（包括 /api/v4）
	signPath := "/api/v4" + path
	signature := c.signer.SignREST(method, signPath, queryString, bodyStr, timestamp)

	// 構造完整 URL（baseURL 已包含 /api/v4）
	fullURL := c.baseURL + path
	if queryString != "" {
		fullURL += "?" + queryString
	}

	req, err := http.NewRequestWithContext(ctx, method, fullURL, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("創建请求失败: %w", err)
	}

	// 添加 Gate.io 必需的请求头
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("KEY", c.signer.GetAPIKey())
	req.Header.Set("SIGN", signature)
	req.Header.Set("Timestamp", strconv.FormatInt(timestamp, 10))

	// 🔥 重要：添加渠道返佣標识
	req.Header.Set("X-Gate-Channel-Id", GateChannelID)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	// Gate.io API 在錯误時返回非 2xx 状態碼
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var gateResp GateResponse
		if err := json.Unmarshal(respBody, &gateResp); err == nil {
			// 針對特定錯误提供更友好的提示
			switch gateResp.Label {
			case "USER_NOT_FOUND":
				return nil, fmt.Errorf("Gate.io 合約账戶未激活: %s。请先在 Gate.io 网站將资金轉入 USDT 永续合約账戶", gateResp.Message)
			case "INVALID_SIGNATURE":
				return nil, fmt.Errorf("Gate.io API 签名錯误: %s。请检查 API Key 和 Secret Key 是否正确", gateResp.Message)
			case "INVALID_KEY":
				return nil, fmt.Errorf("Gate.io API Key 無效: %s。请检查配置文件中的 api_key", gateResp.Message)
			default:
				return nil, fmt.Errorf("Gate.io API 錯误: [%s] %s (状態碼: %d)",
					gateResp.Label, gateResp.Message, resp.StatusCode)
			}
		}
		return nil, fmt.Errorf("Gate.io API 錯误: 状態碼=%d, 响应=%s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

// GetContract 獲取合約信息
func (c *Client) GetContract(ctx context.Context, settle, contract string) (*ContractInfo, error) {
	path := fmt.Sprintf("/futures/%s/contracts/%s", settle, contract)

	respBody, err := c.DoRequest(ctx, "GET", path, "", nil)
	if err != nil {
		return nil, err
	}

	var contractInfo ContractInfo
	if err := json.Unmarshal(respBody, &contractInfo); err != nil {
		return nil, fmt.Errorf("解析合約信息失败: %w", err)
	}

	return &contractInfo, nil
}

// GetAccount 獲取合約帳戶資訊
func (c *Client) GetAccount(ctx context.Context, settle string) (*FuturesAccount, error) {
	path := fmt.Sprintf("/futures/%s/accounts", settle)

	respBody, err := c.DoRequest(ctx, "GET", path, "", nil)
	if err != nil {
		return nil, err
	}

	var account FuturesAccount
	if err := json.Unmarshal(respBody, &account); err != nil {
		return nil, fmt.Errorf("解析帳戶資訊失败: %w", err)
	}

	return &account, nil
}

// GetPositions 獲取持倉信息
func (c *Client) GetPositions(ctx context.Context, settle string) ([]*FuturesPosition, error) {
	path := fmt.Sprintf("/futures/%s/positions", settle)

	respBody, err := c.DoRequest(ctx, "GET", path, "", nil)
	if err != nil {
		return nil, err
	}

	var positions []*FuturesPosition
	if err := json.Unmarshal(respBody, &positions); err != nil {
		return nil, fmt.Errorf("解析持倉資訊失败: %w", err)
	}

	return positions, nil
}

// GetPosition 獲取指定合約的持倉資訊
func (c *Client) GetPosition(ctx context.Context, settle, contract string) (*FuturesPosition, error) {
	path := fmt.Sprintf("/futures/%s/positions/%s", settle, contract)

	respBody, err := c.DoRequest(ctx, "GET", path, "", nil)
	if err != nil {
		return nil, err
	}

	// Gate.io 可能在某些情况下返回數组格式
	// 先尝試解析為對象
	var position FuturesPosition
	if err := json.Unmarshal(respBody, &position); err != nil {
		// 如果失败,尝試解析為數组
		var positions []FuturesPosition
		if err2 := json.Unmarshal(respBody, &positions); err2 == nil && len(positions) > 0 {
			return &positions[0], nil
		}
		return nil, fmt.Errorf("解析持倉資訊失败: %w", err)
	}

	return &position, nil
}

// PlaceOrder 通過 REST API 下單
func (c *Client) PlaceOrder(ctx context.Context, settle string, order map[string]interface{}) (*FuturesOrder, error) {
	path := fmt.Sprintf("/futures/%s/orders", settle)

	respBody, err := c.DoRequest(ctx, "POST", path, "", order)
	if err != nil {
		return nil, err
	}

	var futuresOrder FuturesOrder
	if err := json.Unmarshal(respBody, &futuresOrder); err != nil {
		return nil, fmt.Errorf("解析订單响应失败: %w", err)
	}

	return &futuresOrder, nil
}

// GetOrder 查詢訂單
func (c *Client) GetOrder(ctx context.Context, settle, orderID string) (*FuturesOrder, error) {
	path := fmt.Sprintf("/futures/%s/orders/%s", settle, orderID)

	respBody, err := c.DoRequest(ctx, "GET", path, "", nil)
	if err != nil {
		return nil, err
	}

	var order FuturesOrder
	if err := json.Unmarshal(respBody, &order); err != nil {
		return nil, fmt.Errorf("解析订單信息失败: %w", err)
	}

	return &order, nil
}

// BatchCancelOrders 批量取消訂單
// POST /futures/{settle}/batch_cancel_orders
// 一次最多撤销20個订單
func (c *Client) BatchCancelOrders(ctx context.Context, settle string, orderIDs []string) ([]map[string]interface{}, error) {
	if len(orderIDs) == 0 {
		return nil, nil
	}

	// 限制每次最多20個
	if len(orderIDs) > 20 {
		orderIDs = orderIDs[:20]
	}

	path := fmt.Sprintf("/futures/%s/batch_cancel_orders", settle)

	// 直接傳遞字符串數组，DoRequest 會自动序列化
	resp, err := c.DoRequest(ctx, "POST", path, "", orderIDs)
	if err != nil {
		return nil, err
	}

	var results []map[string]interface{}
	if err := json.Unmarshal(resp, &results); err != nil {
		return nil, fmt.Errorf("解析批量撤單响应失败: %w", err)
	}

	return results, nil
}

// CancelOrder 取消訂單
func (c *Client) CancelOrder(ctx context.Context, settle, orderID string) (*FuturesOrder, error) {
	path := fmt.Sprintf("/futures/%s/orders/%s", settle, orderID)

	respBody, err := c.DoRequest(ctx, "DELETE", path, "", nil)
	if err != nil {
		return nil, err
	}

	var order FuturesOrder
	if err := json.Unmarshal(respBody, &order); err != nil {
		return nil, fmt.Errorf("解析取消訂單响应失败: %w", err)
	}

	return &order, nil
}

// CandlestickData K線數據結構
type CandlestickData struct {
	Timestamp int64  `json:"t"` // 時间戳
	Volume    int64  `json:"v"` // 成交量
	Close     string `json:"c"` // 收盘價
	High      string `json:"h"` // 最高價
	Low       string `json:"l"` // 最低價
	Open      string `json:"o"` // 开盘價
}

// GetCandlesticks 獲取歷史K線數據
// GET /futures/{settle}/candlesticks
func (c *Client) GetCandlesticks(ctx context.Context, settle, contract, interval string, limit int) ([]CandlestickData, error) {
	path := fmt.Sprintf("/futures/%s/candlesticks", settle)
	query := fmt.Sprintf("contract=%s&interval=%s&limit=%d", contract, interval, limit)

	resp, err := c.DoRequest(ctx, "GET", path, query, nil)
	if err != nil {
		return nil, err
	}

	var candlesticks []CandlestickData
	if err := json.Unmarshal(resp, &candlesticks); err != nil {
		return nil, fmt.Errorf("解析K線數據失败: %w", err)
	}

	return candlesticks, nil
}

// GateOrderBookItem Gate.io 订單簿條目結構
type GateOrderBookItem struct {
	P string `json:"p"` // 價格 (字符串)
	S int64  `json:"s"` // 數量
}

// GateOrderBookResponse Gate.io 订單簿响应結構
type GateOrderBookResponse struct {
	ID      int64               `json:"id"`      // 订單簿ID
	Current float64             `json:"current"` // 當前時间
	Update  float64             `json:"update"`  // 更新時间
	Asks    []GateOrderBookItem `json:"asks"`    // 賣盘 [{p: 價格, s: 數量}, ...]
	Bids    []GateOrderBookItem `json:"bids"`    // 買盘 [{p: 價格, s: 數量}, ...]
}

// GetOrderBook 獲取訂單簿深度
// GET /futures/{settle}/order_book
func (c *Client) GetOrderBook(ctx context.Context, settle, contract string, limit int) (*GateOrderBookResponse, error) {
	path := fmt.Sprintf("/futures/%s/order_book", settle)
	query := fmt.Sprintf("contract=%s&limit=%d", contract, limit)

	resp, err := c.DoRequest(ctx, "GET", path, query, nil)
	if err != nil {
		return nil, err
	}

	var orderBook GateOrderBookResponse
	if err := json.Unmarshal(resp, &orderBook); err != nil {
		return nil, fmt.Errorf("解析订單簿數據失败: %w", err)
	}

	return &orderBook, nil
}

// GetOpenOrders 獲取未完成订單
func (c *Client) GetOpenOrders(ctx context.Context, settle, contract string) ([]*FuturesOrder, error) {
	path := fmt.Sprintf("/futures/%s/orders", settle)
	queryString := fmt.Sprintf("contract=%s&status=open", contract)

	respBody, err := c.DoRequest(ctx, "GET", path, queryString, nil)
	if err != nil {
		return nil, err
	}

	var orders []*FuturesOrder
	if err := json.Unmarshal(respBody, &orders); err != nil {
		return nil, fmt.Errorf("解析订單列表失败: %w", err)
	}

	return orders, nil
}

// SetLeverage 設置全倉杠杆倍數
// PUT /futures/{settle}/positions/{contract}/leverage
func (c *Client) SetLeverage(ctx context.Context, settle, contract string, leverage int) error {
	path := fmt.Sprintf("/futures/%s/positions/%s/leverage", settle, contract)
	
	body := map[string]interface{}{
		"leverage": strconv.Itoa(leverage),
	}

	_, err := c.DoRequest(ctx, "PUT", path, "", body)
	if err != nil {
		return fmt.Errorf("設置杠杆失败: %w", err)
	}

	return nil
}

// WalletTransfer POST /wallet/transfers — 現貨與合約（永续）帳戶間劃轉
// settle：合約結算幣種（如 usdt），當 from/to 涉及 futures 時建議傳入
func (c *Client) WalletTransfer(ctx context.Context, currency, amount, from, to, settle string) (int64, error) {
	body := map[string]interface{}{
		"currency": currency,
		"amount":   amount,
		"from":     from,
		"to":       to,
	}
	if settle != "" {
		body["settle"] = settle
	}
	respBody, err := c.DoRequest(ctx, "POST", "/wallet/transfers", "", body)
	if err != nil {
		return 0, err
	}
	var out struct {
		TxID int64 `json:"tx_id"`
	}
	if err := json.Unmarshal(respBody, &out); err != nil {
		return 0, fmt.Errorf("解析劃轉响应失败: %w", err)
	}
	return out.TxID, nil
}
