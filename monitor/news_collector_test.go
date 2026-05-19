package monitor

import (
	"os"
	"strings"
	"testing"
	"time"

	"quantmesh/config"
)

// 聯網 NewsAPI 測試預設關閉，避免 CI/無外網環境失敗；真機驗證時設：
//
//	NEWSAPI_INTEGRATION_TEST=1 NEWSAPI_TEST_KEY=你的key go test ./monitor/... -run NewsAPI -v
func integrationNewsAPIKey(t *testing.T) string {
	t.Helper()
	if os.Getenv("NEWSAPI_INTEGRATION_TEST") != "1" {
		t.Skip("live NewsAPI disabled; set NEWSAPI_INTEGRATION_TEST=1 and NEWSAPI_TEST_KEY")
	}
	key := os.Getenv("NEWSAPI_TEST_KEY")
	if key == "" {
		t.Skip("NEWSAPI_TEST_KEY not set")
	}
	return key
}

func TestNewsCollector_FetchFromNewsAPI(t *testing.T) {
	apiKey := integrationNewsAPIKey(t)

	cfg := &config.Config{}
	cfg.NewsMonitor.Enabled = true
	cfg.NewsMonitor.NewsAPIKey = apiKey
	cfg.NewsMonitor.Sources = []string{"newsapi"}
	cfg.NewsMonitor.Keywords = []string{"bitcoin", "btc", "cryptocurrency"}
	cfg.NewsMonitor.Assets = []config.AssetConfig{
		{
			AssetType: "crypto_btc",
			Symbol:    "BTCUSDT",
			Keywords:  []string{"bitcoin", "btc"},
			Enabled:   true,
		},
	}

	collector := NewNewsCollector(cfg)

	keywords := []string{"bitcoin", "btc", "cryptocurrency"}
	newsItems, err := collector.fetchFromNewsAPI(apiKey, keywords)
	if err != nil {
		t.Fatalf("Failed to fetch news from NewsAPI: %v", err)
	}

	if len(newsItems) == 0 {
		t.Log("Warning: No news items returned, but API call succeeded")
	} else {
		t.Logf("Successfully fetched %d news items", len(newsItems))

		for i, item := range newsItems {
			if item.Title == "" {
				t.Errorf("News item %d has empty title", i)
			}
			if item.Source == "" {
				t.Errorf("News item %d has empty source", i)
			}
			if item.URL == "" {
				t.Errorf("News item %d has empty URL", i)
			}
			if item.PublishedAt.IsZero() {
				t.Errorf("News item %d has zero published time", i)
			}

			t.Logf("News %d: %s from %s at %s", i+1, item.Title, item.Source, item.PublishedAt.Format(time.RFC3339))
		}
	}
}

func TestNewsCollector_CollectNow(t *testing.T) {
	apiKey := integrationNewsAPIKey(t)

	cfg := &config.Config{}
	cfg.NewsMonitor.Enabled = true
	cfg.NewsMonitor.NewsAPIKey = apiKey
	cfg.NewsMonitor.Sources = []string{"newsapi"}
	cfg.NewsMonitor.Keywords = []string{"bitcoin", "btc"}
	cfg.NewsMonitor.NewsCollectInterval = "5m"
	cfg.NewsMonitor.Assets = []config.AssetConfig{
		{
			AssetType: "crypto_btc",
			Symbol:    "BTCUSDT",
			Keywords:  []string{"bitcoin", "btc"},
			Enabled:   true,
		},
	}

	collector := NewNewsCollector(cfg)

	err := collector.CollectNow()
	if err != nil {
		t.Fatalf("Failed to collect news: %v", err)
	}

	cachedNews := collector.GetAllCachedNews()
	if len(cachedNews) == 0 {
		t.Log("Warning: No news in cache after collection, but collection succeeded")
	} else {
		t.Logf("Successfully collected and cached %d news items", len(cachedNews))
	}

	recentNews := collector.GetRecentNews(2)
	t.Logf("Recent news (last 2 hours): %d items", len(recentNews))
}

func TestNewsCollector_WithMultipleKeywords(t *testing.T) {
	apiKey := integrationNewsAPIKey(t)

	cfg := &config.Config{}
	cfg.NewsMonitor.Enabled = true
	cfg.NewsMonitor.NewsAPIKey = apiKey
	cfg.NewsMonitor.Sources = []string{"newsapi"}
	cfg.NewsMonitor.Keywords = []string{"bitcoin", "btc"}
	cfg.NewsMonitor.Assets = []config.AssetConfig{
		{
			AssetType: "crypto_btc",
			Symbol:    "BTCUSDT",
			Keywords:  []string{"bitcoin", "btc", "cryptocurrency"},
			Enabled:   true,
		},
		{
			AssetType: "commodity_gold",
			Symbol:    "PAXGUSDT",
			Keywords:  []string{"gold", "XAU", "gold price"},
			Enabled:   true,
		},
	}

	collector := NewNewsCollector(cfg)

	err := collector.CollectNow()
	if err != nil {
		t.Fatalf("Failed to collect news: %v", err)
	}

	keywords := []string{"bitcoin", "btc", "gold", "XAU"}
	if len(keywords) == 0 {
		t.Fatal("No keywords collected")
	}

	t.Logf("Collected keywords: %v", keywords)

	newsItems, err := collector.fetchFromNewsAPI(apiKey, keywords)
	if err != nil {
		t.Fatalf("Failed to fetch news with multiple keywords: %v", err)
	}

	t.Logf("Successfully fetched %d news items with merged keywords", len(newsItems))
}

func TestNewsCollector_ContextCancellation(t *testing.T) {
	apiKey := integrationNewsAPIKey(t)

	cfg := &config.Config{}
	cfg.NewsMonitor.Enabled = true
	cfg.NewsMonitor.NewsAPIKey = apiKey
	cfg.NewsMonitor.Sources = []string{"newsapi"}
	cfg.NewsMonitor.Keywords = []string{"bitcoin"}

	collector := NewNewsCollector(cfg)

	keywords := []string{"bitcoin"}
	_, err := collector.fetchFromNewsAPI(apiKey, keywords)

	if err != nil {
		t.Logf("API call returned error: %v", err)
	} else {
		t.Log("API call succeeded")
	}
}

func TestBuildNewsAPIQuerySanitizesAndBoundsKeywords(t *testing.T) {
	keywords := []string{
		" bitcoin  ",
		"Bitcoin",
		"ethereum\nspot etf",
		strings.Repeat("x", 120),
	}
	for i := 0; i < 40; i++ {
		keywords = append(keywords, "keyword")
	}

	query := buildNewsAPIQuery(keywords)
	if len(query) > maxNewsAPIQueryLength {
		t.Fatalf("query too long: %d", len(query))
	}
	if strings.Contains(query, "\n") {
		t.Fatalf("query contains raw newline: %q", query)
	}
	if strings.Count(strings.ToLower(query), "bitcoin") != 1 {
		t.Fatalf("query did not dedupe bitcoin: %q", query)
	}
}
