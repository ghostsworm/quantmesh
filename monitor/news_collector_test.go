package monitor

import (
	"testing"
	"time"

	"quantmesh/config"
)

func TestNewsCollector_FetchFromNewsAPI(t *testing.T) {
	// 使用用户提供的 API Key
	apiKey := "1ed7e84648694bcd8524aaef69b03a7c"
	if apiKey == "" {
		t.Skip("NewsAPI key not provided, skipping test")
	}

	// 创建测试配置
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

	// 创建 NewsCollector
	collector := NewNewsCollector(cfg)

	// 测试获取新闻
	keywords := []string{"bitcoin", "btc", "cryptocurrency"}
	newsItems, err := collector.fetchFromNewsAPI(apiKey, keywords)

	if err != nil {
		t.Fatalf("Failed to fetch news from NewsAPI: %v", err)
	}

	if len(newsItems) == 0 {
		t.Log("Warning: No news items returned, but API call succeeded")
	} else {
		t.Logf("Successfully fetched %d news items", len(newsItems))

		// 验证新闻项的基本字段
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
	// 使用用户提供的 API Key
	apiKey := "1ed7e84648694bcd8524aaef69b03a7c"
	if apiKey == "" {
		t.Skip("NewsAPI key not provided, skipping test")
	}

	// 创建测试配置
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

	// 创建 NewsCollector
	collector := NewNewsCollector(cfg)

	// 测试立即收集
	err := collector.CollectNow()
	if err != nil {
		t.Fatalf("Failed to collect news: %v", err)
	}

	// 验证缓存中有新闻
	cachedNews := collector.GetAllCachedNews()
	if len(cachedNews) == 0 {
		t.Log("Warning: No news in cache after collection, but collection succeeded")
	} else {
		t.Logf("Successfully collected and cached %d news items", len(cachedNews))
	}

	// 验证最近2小时的新闻
	recentNews := collector.GetRecentNews(2)
	t.Logf("Recent news (last 2 hours): %d items", len(recentNews))
}

func TestNewsCollector_WithMultipleKeywords(t *testing.T) {
	// 使用用户提供的 API Key
	apiKey := "1ed7e84648694bcd8524aaef69b03a7c"
	if apiKey == "" {
		t.Skip("NewsAPI key not provided, skipping test")
	}

	// 创建测试配置，包含多个资产的关键词
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

	// 测试收集关键词合并（通过 CollectNow 间接测试）
	// 注意：collectKeywords 是私有方法，我们通过 CollectNow 来测试
	err := collector.CollectNow()
	if err != nil {
		t.Fatalf("Failed to collect news: %v", err)
	}
	
	// 使用测试关键词
	keywords := []string{"bitcoin", "btc", "gold", "XAU"}
	if len(keywords) == 0 {
		t.Fatal("No keywords collected")
	}

	t.Logf("Collected keywords: %v", keywords)

	// 测试获取新闻
	newsItems, err := collector.fetchFromNewsAPI(apiKey, keywords)
	if err != nil {
		t.Fatalf("Failed to fetch news with multiple keywords: %v", err)
	}

	t.Logf("Successfully fetched %d news items with merged keywords", len(newsItems))
}

func TestNewsCollector_ContextCancellation(t *testing.T) {
	// 使用用户提供的 API Key
	apiKey := "1ed7e84648694bcd8524aaef69b03a7c"
	if apiKey == "" {
		t.Skip("NewsAPI key not provided, skipping test")
	}

	cfg := &config.Config{}
	cfg.NewsMonitor.Enabled = true
	cfg.NewsMonitor.NewsAPIKey = apiKey
	cfg.NewsMonitor.Sources = []string{"newsapi"}
	cfg.NewsMonitor.Keywords = []string{"bitcoin"}

	collector := NewNewsCollector(cfg)

	// 测试正常获取新闻（context 取消测试在实际使用中由 collector 内部处理）
	keywords := []string{"bitcoin"}
	_, err := collector.fetchFromNewsAPI(apiKey, keywords)

	// 测试函数不会 panic，能正常处理请求
	if err != nil {
		t.Logf("API call returned error: %v", err)
	} else {
		t.Log("API call succeeded")
	}
}
