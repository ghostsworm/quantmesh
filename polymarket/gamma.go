// Package polymarket 提供 Polymarket Gamma REST API 客戶端（無需登錄 token）。
package polymarket

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// MarketInfo 單個預測市場（從 Gamma events/markets 展開）。
type MarketInfo struct {
	ID          string
	Question    string
	Description string
	EndDate     time.Time
	Outcomes    []string
	Volume      float64
	Liquidity   float64
	EventTitle  string
}

// FetchActiveMarkets 從 Gamma `/events` 拉取活躍市場，按 24h 成交量排序。
// keywords 非空時僅保留事件標題（title）包含任一關鍵詞（不區分大小寫）的市場。
// baseURL 例如 https://gamma-api.polymarket.com
func FetchActiveMarkets(baseURL string, keywords []string, client *http.Client, eventLimit int) ([]MarketInfo, error) {
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
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gamma API HTTP %d", resp.StatusCode)
	}
	var events []struct {
		ID      string `json:"id"`
		Title   string `json:"title"`
		Markets []struct {
			ID           string  `json:"id"`
			Question     string  `json:"question"`
			Description  string  `json:"description"`
			Outcomes     string  `json:"outcomes"`
			Volume       string  `json:"volume"`
			Liquidity    string  `json:"liquidity"`
			VolumeNum    float64 `json:"volumeNum"`
			LiquidityNum float64 `json:"liquidityNum"`
			EndDate      string  `json:"endDate"`
		} `json:"markets"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&events); err != nil {
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
		titleLower := strings.ToLower(evt.Title)
		if len(kwLower) > 0 {
			matched := false
			for k := range kwLower {
				if strings.Contains(titleLower, k) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		for _, m := range evt.Markets {
			var outcomes []string
			_ = json.Unmarshal([]byte(m.Outcomes), &outcomes)
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
				ID:          m.ID,
				Question:    m.Question,
				Description: m.Description,
				EndDate:     endDate,
				Outcomes:    outcomes,
				Volume:      vol,
				Liquidity:   liq,
				EventTitle:  evt.Title,
			})
		}
	}
	return out, nil
}
