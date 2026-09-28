package binance

import (
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
	"strings"
	"time"
)

const equityResponseLimit = 4 * 1024 * 1024

// The pinned SDK lacks income page support. Use its per-instance endpoint and
// transport but do not log signed URLs, API keys, response bodies or URL errors.
func (b *BinanceAdapter) equityGET(ctx context.Context, path string, values url.Values, serverTime int64, target interface{}) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if b.client == nil {
		return fmt.Errorf("equity client unavailable")
	}
	if values == nil {
		values = make(url.Values)
	}
	if serverTime > 0 {
		if b.client.KeyType != "" && b.client.KeyType != "HMAC" {
			return fmt.Errorf("equity signing key type unsupported")
		}
		values.Set("timestamp", strconv.FormatInt(serverTime, 10))
		values.Set("recvWindow", "5000")
		mac := hmac.New(sha256.New, []byte(b.client.SecretKey))
		_, _ = mac.Write([]byte(values.Encode()))
		values.Set("signature", hex.EncodeToString(mac.Sum(nil)))
	}
	endpoint := strings.TrimRight(b.client.BaseURL, "/") + path + "?" + values.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return fmt.Errorf("invalid equity endpoint")
	}
	if serverTime > 0 {
		req.Header.Set("X-MBX-APIKEY", b.client.APIKey)
	}
	client := b.client.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	// Never follow a redirect that could forward authentication to another host.
	readClient := *client
	readClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := readClient.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("equity transport failure")
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("equity endpoint returned HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, equityResponseLimit+1))
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("equity response read failure")
	}
	if len(data) > equityResponseLimit {
		return fmt.Errorf("equity response exceeds size limit")
	}
	if err := json.Unmarshal(data, target); err != nil {
		return fmt.Errorf("invalid equity response JSON")
	}
	return nil
}

func (b *BinanceAdapter) equityServerTime(ctx context.Context) (time.Time, error) {
	var result struct {
		ServerTime int64 `json:"serverTime"`
	}
	if err := b.equityGET(ctx, "/fapi/v1/time", nil, 0, &result); err != nil {
		return time.Time{}, err
	}
	if result.ServerTime <= 0 {
		return time.Time{}, fmt.Errorf("invalid exchange clock")
	}
	remote := time.UnixMilli(result.ServerTime).UTC()
	if delta := time.Since(remote); delta < -equityClockTolerance || delta > equityClockTolerance {
		return time.Time{}, fmt.Errorf("exchange/local clock skew exceeds evidence window")
	}
	return remote, nil
}
