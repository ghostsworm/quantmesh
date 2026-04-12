package kraken

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const krakenSpotRestURL = "https://api.kraken.com"

func krakenSpotAssetCode(asset string) string {
	a := strings.ToUpper(strings.TrimSpace(asset))
	if a == "" {
		return "USDT"
	}
	if a == "BTC" {
		return "XBT"
	}
	return a
}

func krakenFuturesWithdrawalCurrency(asset string) string {
	return krakenSpotAssetCode(asset)
}

// SpotWalletTransferToFutures 現貨錢包 → 期貨錢包（POST api.kraken.com/0/private/WalletTransfer）
func (c *KrakenClient) SpotWalletTransferToFutures(ctx context.Context, asset string, amount float64) (string, error) {
	path := "/0/private/WalletTransfer"
	nonce := strconv.FormatInt(time.Now().UnixMilli(), 10)
	v := url.Values{}
	v.Set("nonce", nonce)
	v.Set("asset", krakenSpotAssetCode(asset))
	v.Set("amount", strconv.FormatFloat(amount, 'f', 8, 64))
	v.Set("from", "Spot Wallet")
	v.Set("to", "Futures Wallet")
	bodyStr := v.Encode()

	sig := c.signRequest(path, nonce, bodyStr)
	reqURL := krakenSpotRestURL + path
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, reqURL, strings.NewReader(bodyStr))
	if err != nil {
		return "", fmt.Errorf("create spot request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("API-Key", c.apiKey)
	req.Header.Set("API-Sign", sig)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("spot wallet transfer: %w", err)
	}
	defer resp.Body.Close()
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read spot response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("spot HTTP %s: %s", resp.Status, string(respBody))
	}

	var out struct {
		Error  []string `json:"error"`
		Result struct {
			Refid string `json:"refid"`
		} `json:"result"`
	}
	if err := json.Unmarshal(respBody, &out); err != nil {
		return "", fmt.Errorf("unmarshal spot transfer: %w", err)
	}
	if len(out.Error) > 0 {
		return "", fmt.Errorf("Kraken spot: %v", out.Error)
	}
	if out.Result.Refid != "" {
		return out.Result.Refid, nil
	}
	return string(respBody), nil
}

// FuturesWithdrawalToSpot 期貨錢包 → 現貨（POST /derivatives/api/v3/withdrawal）
func (c *KrakenClient) FuturesWithdrawalToSpot(ctx context.Context, asset string, amount float64) (string, error) {
	path := "/derivatives/api/v3/withdrawal"
	params := map[string]interface{}{
		"currency": krakenFuturesWithdrawalCurrency(asset),
		"amount":   amount,
	}
	respBody, err := c.sendRequest(ctx, http.MethodPost, path, params)
	if err != nil {
		return "", err
	}
	var out struct {
		Result     string `json:"result"`
		ServerTime string `json:"serverTime"`
	}
	if err := json.Unmarshal(respBody, &out); err != nil {
		return "", fmt.Errorf("unmarshal futures withdrawal: %w", err)
	}
	if out.Result != "success" {
		return "", fmt.Errorf("futures withdrawal: %s", string(respBody))
	}
	if out.ServerTime != "" {
		return "kraken-futures-withdraw@" + out.ServerTime, nil
	}
	return "kraken-futures-withdraw", nil
}
