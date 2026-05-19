// Package polymarket 提供 Polymarket Gamma REST API 客戶端（無需登錄 token）。
package polymarket

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const maxGammaResponseBytes = 8 << 20

// MarketInfo 單個預測市場（從 Gamma events/markets 展開）。
type MarketInfo struct {
	ID             string
	Question       string
	Description    string
	EndDate        time.Time
	Outcomes       []string
	OutcomePrices  []float64
	YesProbability float64
	Volume         float64
	Liquidity      float64
	EventTitle     string
}

// FetchActiveMarkets 從 Gamma `/events` 拉取活躍市場，按 24h 成交量排序。
// keywords 非空時僅保留事件標題、問題或描述包含任一關鍵詞（不區分大小寫）的市場。
// baseURL 例如 https://gamma-api.polymarket.com
func FetchActiveMarkets(baseURL string, keywords []string, client *http.Client, eventLimit int) ([]MarketInfo, error) {
	return FetchActiveMarketsContext(context.Background(), baseURL, keywords, client, eventLimit)
}

// FetchActiveMarketsContext 從 Gamma `/events` 拉取活躍市場，支持調用方取消與超時控制。
func FetchActiveMarketsContext(ctx context.Context, baseURL string, keywords []string, client *http.Client, eventLimit int) ([]MarketInfo, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	if eventLimit <= 0 || eventLimit > 200 {
		eventLimit = 50
	}
	base := strings.TrimSuffix(strings.TrimSpace(baseURL), "/")
	if base == "" {
		base = "https://gamma-api.polymarket.com"
	}
	url := fmt.Sprintf("%s/events?limit=%d&active=true&closed=false&order=volume24hr&ascending=false", base, eventLimit)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("gamma API HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var events []struct {
		ID      string `json:"id"`
		Title   string `json:"title"`
		Markets []struct {
			ID            string  `json:"id"`
			Question      string  `json:"question"`
			Description   string  `json:"description"`
			Outcomes      string  `json:"outcomes"`
			OutcomePrices string  `json:"outcomePrices"`
			Volume        string  `json:"volume"`
			Liquidity     string  `json:"liquidity"`
			VolumeNum     float64 `json:"volumeNum"`
			LiquidityNum  float64 `json:"liquidityNum"`
			EndDate       string  `json:"endDate"`
		} `json:"markets"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxGammaResponseBytes)).Decode(&events); err != nil {
		return nil, err
	}
	kwLower := make(map[string]bool)
	for _, k := range keywords {
		k = strings.TrimSpace(k)
		if k != "" {
			kwLower[strings.ToLower(k)] = true
		}
	}
	var out []MarketInfo
	for _, evt := range events {
		for _, m := range evt.Markets {
			if len(kwLower) > 0 && !matchesMarketKeywords(evt.Title, m.Question, m.Description, kwLower) {
				continue
			}
			var outcomes []string
			_ = json.Unmarshal([]byte(m.Outcomes), &outcomes)
			outcomePrices := parseOutcomePrices(m.OutcomePrices)
			yesProbability := 0.0
			if len(outcomePrices) > 0 {
				yesProbability = outcomePrices[0]
			}
			vol, _ := strconv.ParseFloat(m.Volume, 64)
			liq, _ := strconv.ParseFloat(m.Liquidity, 64)
			if vol == 0 {
				vol = m.VolumeNum
			}
			if liq == 0 {
				liq = m.LiquidityNum
			}
			var endDate time.Time
			if m.EndDate != "" {
				endDate, _ = time.Parse(time.RFC3339, m.EndDate)
			}
			out = append(out, MarketInfo{
				ID:             m.ID,
				Question:       m.Question,
				Description:    m.Description,
				EndDate:        endDate,
				Outcomes:       outcomes,
				OutcomePrices:  outcomePrices,
				YesProbability: yesProbability,
				Volume:         vol,
				Liquidity:      liq,
				EventTitle:     evt.Title,
			})
		}
	}
	return out, nil
}

func matchesMarketKeywords(eventTitle, question, description string, keywords map[string]bool) bool {
	haystack := strings.ToLower(eventTitle + " " + question + " " + description)
	for k := range keywords {
		if strings.Contains(haystack, k) {
			return true
		}
	}
	return false
}

func parseOutcomePrices(raw string) []float64 {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "[]" {
		return nil
	}
	var stringValues []string
	if err := json.Unmarshal([]byte(raw), &stringValues); err == nil {
		out := make([]float64, 0, len(stringValues))
		for _, v := range stringValues {
			f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
			if err == nil {
				out = append(out, f)
			}
		}
		return out
	}
	var floatValues []float64
	if err := json.Unmarshal([]byte(raw), &floatValues); err == nil {
		return floatValues
	}
	return nil
}
