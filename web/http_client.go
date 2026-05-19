package web

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const (
	publicAPIHTTPTimeout = 8 * time.Second
	publicAPIResponseMax = 2 << 20
)

var publicAPIHTTPClient = &http.Client{
	Timeout: publicAPIHTTPTimeout,
}

func fetchPublicJSON(ctx context.Context, url string, dst interface{}) error {
	if ctx == nil {
		ctx = context.Background()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "QuantMesh/"+appVersion)
	resp, err := publicAPIHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		if len(body) > 0 {
			return fmt.Errorf("public API status %d: %s", resp.StatusCode, string(body))
		}
		return fmt.Errorf("public API status %d", resp.StatusCode)
	}

	limited := io.LimitReader(resp.Body, publicAPIResponseMax)
	if err := json.NewDecoder(limited).Decode(dst); err != nil {
		return err
	}
	return nil
}
