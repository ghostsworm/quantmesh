package bitget

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	BitgetBaseURL       = "https://api.bitget.com"
	BitgetTestnetBaseURL = "https://testnetapi.bitget.com"
)

// Client Bitget HTTP 客戶端
type Client struct {
	httpClient *http.Client
	signer     *Signer
	baseURL    string
}

// NewClient 創建 Bitget 客戶端
func NewClient(apiKey, secretKey, passphrase string, testnet bool) *Client {
	baseURL := BitgetBaseURL
	if testnet {
		baseURL = BitgetTestnetBaseURL
	}
	return &Client{
		httpClient: &http.Client{Timeout: 10 * time.Second},
		signer:     NewSigner(apiKey, secretKey, passphrase),
		baseURL:    baseURL,
	}
}

// BitgetResponse Bitget API 通用响应結構
type BitgetResponse struct {
	Code    string          `json:"code"`
	Msg     string          `json:"msg"`
	Data    json.RawMessage `json:"data"`
	ReqTime int64           `json:"requestTime"`
}

// DoRequest 发送 HTTP 请求（带签名）
func (c *Client) DoRequest(ctx context.Context, method, path string, body interface{}) (*BitgetResponse, error) {
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
	if bodyStr == "" {
		bodyStr = ""
	}

	signature := c.signer.Sign(timestamp, method, path, bodyStr)

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bytes.NewBuffer(bodyBytes))
	if err != nil {
		return nil, fmt.Errorf("創建请求失败: %w", err)
	}

	// 添加 Bitget 必需的请求头
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("ACCESS-KEY", c.signer.GetAPIKey())
	req.Header.Set("ACCESS-SIGN", signature)
	req.Header.Set("ACCESS-TIMESTAMP", timestamp)
	req.Header.Set("ACCESS-PASSPHRASE", c.signer.GetPassphrase())
	req.Header.Set("locale", "en-US")
	req.Header.Set("X-CHANNEL-API-CODE", "3xh1b")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	var bitgetResp BitgetResponse
	if err := json.Unmarshal(respBody, &bitgetResp); err != nil {
		return nil, fmt.Errorf("解析响应失败: %w, 响应体: %s", err, string(respBody))
	}

	if bitgetResp.Code != "00000" {
		return nil, fmt.Errorf("bitget API 錯误: code=%s, msg=%s", bitgetResp.Code, bitgetResp.Msg)
	}

	return &bitgetResp, nil
}

// WalletTransferV2 POST /api/spot/v1/wallet/transfer-v2 — 主帳戶內現貨與合約（mix_usdt 等）劃轉
func (c *Client) WalletTransferV2(ctx context.Context, fromType, toType, coin, amount string) (string, error) {
	body := map[string]interface{}{
		"fromType": fromType,
		"toType":   toType,
		"coin":     coin,
		"amount":   amount,
	}
	resp, err := c.DoRequest(ctx, "POST", "/api/spot/v1/wallet/transfer-v2", body)
	if err != nil {
		return "", err
	}
	var data struct {
		TransferID string `json:"transferId"`
	}
	if len(resp.Data) == 0 {
		return "", nil
	}
	if err := json.Unmarshal(resp.Data, &data); err != nil {
		return "", fmt.Errorf("解析劃轉 data 失败: %w", err)
	}
	return data.TransferID, nil
}
