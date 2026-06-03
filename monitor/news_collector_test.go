package monitor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"quantmesh/config"
)

func TestNewsCollectorKeywordsCacheSummaryAndNewsAPI(t *testing.T) {
	cfg := &config.Config{}
	cfg.NewsMonitor.Keywords = []string{" bitcoin ", "btc", "bitcoin", ""}
	cfg.NewsMonitor.Assets = []config.AssetConfig{
		{Enabled: true, Keywords: []string{"ethereum", "solana", "btc"}},
		{Enabled: false, Keywords: []string{"ignored"}},
	}
	collector := NewNewsCollector(cfg)
	keywords := collector.collectKeywords()
	if len(keywords) != 4 || keywords[0] != "bitcoin" {
		t.Fatalf("keywords=%#v", keywords)
	}
	longKeywords := make([]string, 0, 30)
	for i := 0; i < 30; i++ {
		longKeywords = append(longKeywords, strings.Repeat("x", 100))
	}
	normalized := normalizeNewsAPIKeywords(longKeywords)
	if len(normalized) != 1 || len(normalized[0]) != 80 {
		t.Fatalf("normalized=%#v", normalized)
	}
	if defaults := normalizeNewsAPIKeywords(nil); len(defaults) == 0 {
		t.Fatalf("default keywords should be populated")
	}
	if query := buildNewsAPIQuery(longKeywords); len(query) > maxNewsAPIQueryLength {
		t.Fatalf("query too long: %d", len(query))
	}

	now := time.Now()
	collector.newsCache = []NewsItem{
		{Title: "Fresh", Source: "Unit", Content: strings.Repeat("a", 250), PublishedAt: now.Add(-time.Hour)},
		{Title: "Old", Source: "Unit", PublishedAt: now.Add(-25 * time.Hour)},
	}
	if recent := collector.GetRecentNews(2); len(recent) != 1 || recent[0].Title != "Fresh" {
		t.Fatalf("recent=%#v", recent)
	}
	if all := collector.GetAllCachedNews(); len(all) != 2 {
		t.Fatalf("all len=%d", len(all))
	}
	if summary := collector.GetNewsSummaryText(AssetTypeCryptoBTC); !strings.Contains(summary, "Fresh") || !strings.Contains(summary, "...") {
		t.Fatalf("summary=%q", summary)
	}
	collector.newsCache = nil
	if summary := collector.GetNewsSummaryText(""); !strings.Contains(summary, "暂無") {
		t.Fatalf("empty summary=%q", summary)
	}
	if !collector.GetLastCollectTime().IsZero() || collector.GetCacheCount() != 0 {
		t.Fatalf("empty collect metadata mismatch")
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("apiKey") != "key" || r.URL.Query().Get("q") == "" {
			t.Fatalf("unexpected query: %s", r.URL.RawQuery)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok","articles":[{"title":"A","description":"D","url":"https://example.test/a","source":{"name":"S"},"publishedAt":"2026-06-04T00:00:00Z"}]}`))
	}))
	defer server.Close()
	collector.httpClient = server.Client()
	collector.ctx = context.Background()
	oldBase := newsAPIBaseURLForTest
	newsAPIBaseURLForTest = server.URL
	t.Cleanup(func() { newsAPIBaseURLForTest = oldBase })
	items, err := collector.fetchFromNewsAPI("key", []string{"btc"})
	if err != nil || len(items) != 1 || items[0].Title != "A" {
		t.Fatalf("items=%#v err=%v", items, err)
	}

	errorServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad", http.StatusTooManyRequests)
	}))
	defer errorServer.Close()
	collector.httpClient = errorServer.Client()
	newsAPIBaseURLForTest = errorServer.URL
	if _, err := collector.fetchFromNewsAPI("key", []string{"btc"}); err == nil {
		t.Fatalf("http error should fail")
	}
}
