package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"quantmesh/config"
	"quantmesh/logger"
)

const (
	defaultNewsCollectInterval = 5 * time.Minute
	maxNewsAPIQueryLength      = 450
	maxNewsAPIKeywords         = 20
	maxNewsAPIErrorBodyBytes   = 4096
	maxNewsAPIResponseBytes    = 4 << 20
)

// NewsCollector 新闻收集器：每5分钟静默收集NewsAPI新闻，维护最近2小時缓存
type NewsCollector struct {
	cfg         *config.Config
	httpClient  *http.Client
	newsCache   []NewsItem
	cacheMutex  sync.RWMutex
	lastCollect time.Time
	ctx         context.Context
	cancel      context.CancelFunc
	collectDone chan struct{} // 在 Start() 中創建，goroutine 退出時关闭
}

// NewNewsCollector 創建新闻收集器
func NewNewsCollector(cfg *config.Config) *NewsCollector {
	ctx, cancel := context.WithCancel(context.Background())

	transport := &http.Transport{}
	proxyURL := os.Getenv("HTTPS_PROXY")
	if proxyURL == "" {
		proxyURL = os.Getenv("https_proxy")
	}
	if proxyURL == "" {
		proxyURL = os.Getenv("HTTP_PROXY")
	}
	if proxyURL == "" {
		proxyURL = os.Getenv("http_proxy")
	}
	if proxyURL != "" {
		if proxy, err := url.Parse(proxyURL); err == nil {
			transport.Proxy = http.ProxyURL(proxy)
		}
	}

	return &NewsCollector{
		cfg:        cfg,
		httpClient: &http.Client{Timeout: 30 * time.Second, Transport: transport},
		newsCache:  make([]NewsItem, 0),
		ctx:        ctx,
		cancel:     cancel,
	}
}

// Start 啟动5分钟定時收集
func (nc *NewsCollector) Start() {
	if !nc.cfg.NewsMonitor.Enabled || nc.cfg.NewsMonitor.NewsAPIKey == "" {
		logger.Debug("📰 NewsCollector: 未啟用或未配置NewsAPI Key，跳過啟动")
		return
	}

	interval, err := time.ParseDuration(nc.cfg.NewsMonitor.NewsCollectInterval)
	if err != nil || interval <= 0 {
		logger.Warn("📰 NewsCollector: 解析收集间隔失败，使用默认5分钟: %v", err)
		interval = defaultNewsCollectInterval
	}

	logger.Info("📰 NewsCollector 啟动 (收集间隔: %s)", interval)

	nc.collectDone = make(chan struct{})

	// 立即執行一次
	_ = nc.CollectNow()

	go func() {
		defer close(nc.collectDone)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-nc.ctx.Done():
				return
			case <-ticker.C:
				_ = nc.CollectNow()
			}
		}
	}()
}

// Stop 停止收集
func (nc *NewsCollector) Stop() {
	nc.cancel()
	if nc.collectDone != nil {
		<-nc.collectDone
	}
	logger.Info("📰 NewsCollector 已停止")
}

// CollectNow 立即收集一次（合並所有啟用资產的关键词）
func (nc *NewsCollector) CollectNow() error {
	if nc.cfg.NewsMonitor.NewsAPIKey == "" {
		logger.Warn("📰 NewsCollector: NewsAPI key 未配置")
		return fmt.Errorf("NewsAPI key not configured")
	}

	keywords := nc.collectKeywords()
	if len(keywords) == 0 {
		keywords = config.DefaultNewsKeywords()
	}
	logger.Debug("📰 NewsCollector 使用关键词: %v", keywords)

	items, err := nc.fetchFromNewsAPI(nc.cfg.NewsMonitor.NewsAPIKey, keywords)
	if err != nil {
		logger.Warn("📰 NewsCollector 獲取新闻失败: %v", err)
		return err
	}
	logger.Info("📰 NewsCollector 從 NewsAPI 獲取到 %d 条新闻", len(items))

	nc.cacheMutex.Lock()
	defer nc.cacheMutex.Unlock()

	// 去重合並：相同標题+来源的新闻只保留最新的
	seen := make(map[string]bool)
	// 先标记已有的新闻
	for _, item := range nc.newsCache {
		key := item.Title + "|" + item.Source
		seen[key] = true
	}
	// 添加新的新闻
	newCount := 0
	for _, item := range items {
		key := item.Title + "|" + item.Source
		if seen[key] {
			continue
		}
		seen[key] = true
		nc.newsCache = append(nc.newsCache, item)
		newCount++
	}
	if newCount > 0 {
		logger.Info("📰 NewsCollector 新增 %d 条新闻", newCount)
	}

	// 保留最近 24 小時的新闻用於缓存（實際顯示時會按需過濾）
	cutoffTime := time.Now().Add(-24 * time.Hour)
	filtered := make([]NewsItem, 0)
	expiredCount := 0
	for _, item := range nc.newsCache {
		if item.PublishedAt.After(cutoffTime) {
			filtered = append(filtered, item)
		} else {
			expiredCount++
		}
	}
	if expiredCount > 0 {
		logger.Debug("📰 NewsCollector 過期 %d 条新闻（超過 24 小時）", expiredCount)
	}
	nc.newsCache = filtered

	// 按時间倒序排列（最新的在前）
	sort.Slice(nc.newsCache, func(i, j int) bool {
		return nc.newsCache[i].PublishedAt.After(nc.newsCache[j].PublishedAt)
	})

	nc.lastCollect = time.Now()
	logger.Info("📰 NewsCollector 收集完成，當前缓存 %d 条新闻", len(nc.newsCache))
	return nil
}

// collectKeywords 合並所有啟用资產的关键词，优先使用最重要的关键词
func (nc *NewsCollector) collectKeywords() []string {
	seen := make(map[string]bool)
	var result []string

	// 优先使用配置的关键词（用户自定义的最重要）
	if len(nc.cfg.NewsMonitor.Keywords) > 0 {
		for _, k := range nc.cfg.NewsMonitor.Keywords {
			k = strings.TrimSpace(k)
			if k != "" && !seen[k] {
				seen[k] = true
				result = append(result, k)
			}
		}
	}

	// 然后添加启用的资产关键词（限制每个资产的关键词数量）
	for _, a := range nc.cfg.NewsMonitor.Assets {
		if !a.Enabled || len(a.Keywords) == 0 {
			continue
		}
		// 每个资产最多取前 10 个最重要的关键词
		maxPerAsset := 10
		count := 0
		for _, k := range a.Keywords {
			if count >= maxPerAsset {
				break
			}
			k = strings.TrimSpace(k)
			if k != "" && !seen[k] {
				seen[k] = true
				result = append(result, k)
				count++
			}
		}
	}

	// 如果还没有关键词，使用默认关键词（优先使用 BTC 相关的）
	if len(result) == 0 {
		// 优先使用 BTC 相关的核心关键词
		priorityKeywords := []string{
			"bitcoin", "btc", "cryptocurrency", "crypto",
			"binance", "coinbase", "bitcoin etf",
			"sec crypto", "crypto regulation",
			"bitcoin crash", "crypto crash", "bitcoin rally",
		}
		for _, k := range priorityKeywords {
			if !seen[k] {
				seen[k] = true
				result = append(result, k)
			}
		}
		// 如果还需要更多，添加其他默认关键词
		for _, k := range config.DefaultNewsKeywords() {
			if len(result) >= 15 { // 限制总数
				break
			}
			if !seen[k] {
				seen[k] = true
				result = append(result, k)
			}
		}
	}

	// 最终限制：最多 15 个关键词（确保查询字符串不会太长）
	if len(result) > 15 {
		result = result[:15]
		logger.Debug("📰 NewsCollector: 关键词数量过多，已限制到 15 个最重要的关键词")
	}

	return result
}

// GetRecentNews 獲取最近N小時的新闻
func (nc *NewsCollector) GetRecentNews(hours int) []NewsItem {
	nc.cacheMutex.RLock()
	defer nc.cacheMutex.RUnlock()

	cutoffTime := time.Now().Add(-time.Duration(hours) * time.Hour)
	result := make([]NewsItem, 0)
	for _, item := range nc.newsCache {
		if item.PublishedAt.After(cutoffTime) {
			result = append(result, item)
		}
	}
	return result
}

// GetAllCachedNews 獲取所有缓存的新闻（最近 24 小時）
func (nc *NewsCollector) GetAllCachedNews() []NewsItem {
	nc.cacheMutex.RLock()
	defer nc.cacheMutex.RUnlock()

	result := make([]NewsItem, len(nc.newsCache))
	copy(result, nc.newsCache)
	return result
}

// GetNewsSummaryText 生成给Gemini的新闻摘要文本（assetType 用於扩展，當前合並所有资產新闻）
func (nc *NewsCollector) GetNewsSummaryText(assetType string) string {
	news := nc.GetRecentNews(2) // 最近2小時
	if len(news) == 0 {
		return "（暂無最近2小時内的新闻）"
	}

	var sb strings.Builder
	for _, item := range news {
		sb.WriteString(fmt.Sprintf("時间: %s\n", item.PublishedAt.Format("2006-01-02 15:04:05")))
		sb.WriteString(fmt.Sprintf("標题: %s\n", item.Title))
		sb.WriteString(fmt.Sprintf("来源: %s\n", item.Source))
		if item.Content != "" {
			content := item.Content
			if len(content) > 200 {
				content = content[:200] + "..."
			}
			sb.WriteString(fmt.Sprintf("摘要: %s\n", content))
		}
		sb.WriteString("\n")
	}
	return strings.TrimSpace(sb.String())
}

// GetLastCollectTime 返回最近一次收集時间
func (nc *NewsCollector) GetLastCollectTime() time.Time {
	nc.cacheMutex.RLock()
	defer nc.cacheMutex.RUnlock()
	return nc.lastCollect
}

// GetCacheCount 回傳當前缓存新闻數量
func (nc *NewsCollector) GetCacheCount() int {
	nc.cacheMutex.RLock()
	defer nc.cacheMutex.RUnlock()
	return len(nc.newsCache)
}

// fetchFromNewsAPI 從NewsAPI獲取新闻
func (nc *NewsCollector) fetchFromNewsAPI(apiKey string, keywords []string) ([]NewsItem, error) {
	query := buildNewsAPIQuery(keywords)

	baseURL := "https://newsapi.org/v2/everything"
	params := url.Values{}
	params.Set("apiKey", apiKey)
	params.Set("q", query)
	// NewsAPI 只支援單一語言參數，不支援逗號分隔；這裡優先使用英文以獲取更多結果
	params.Set("language", "en")
	params.Set("sortBy", "publishedAt")
	params.Set("pageSize", "50")

	reqURL := baseURL + "?" + params.Encode()
	req, err := http.NewRequestWithContext(nc.ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := nc.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		// 讀取錯誤響應體以便調試
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxNewsAPIErrorBodyBytes))
		return nil, fmt.Errorf("NewsAPI error: HTTP %d - %s", resp.StatusCode, string(body))
	}

	var result struct {
		Status   string `json:"status"`
		Articles []struct {
			Title       string `json:"title"`
			Description string `json:"description"`
			URL         string `json:"url"`
			Source      struct {
				Name string `json:"name"`
			} `json:"source"`
			PublishedAt string `json:"publishedAt"`
		} `json:"articles"`
	}

	if err := json.NewDecoder(io.LimitReader(resp.Body, maxNewsAPIResponseBytes)).Decode(&result); err != nil {
		return nil, err
	}

	if result.Status != "ok" {
		return nil, fmt.Errorf("NewsAPI returned status: %s", result.Status)
	}

	newsItems := make([]NewsItem, 0, len(result.Articles))
	for _, article := range result.Articles {
		publishedAt, _ := time.Parse(time.RFC3339, article.PublishedAt)
		newsItems = append(newsItems, NewsItem{
			Title:       article.Title,
			Content:     article.Description,
			Source:      article.Source.Name,
			URL:         article.URL,
			PublishedAt: publishedAt,
		})
	}
	return newsItems, nil
}

func buildNewsAPIQuery(keywords []string) string {
	keywords = normalizeNewsAPIKeywords(keywords)
	query := strings.Join(keywords, " OR ")
	if len(query) <= maxNewsAPIQueryLength {
		return query
	}
	for len(query) > maxNewsAPIQueryLength && len(keywords) > 1 {
		keywords = keywords[:len(keywords)-1]
		query = strings.Join(keywords, " OR ")
	}
	if len(query) > maxNewsAPIQueryLength {
		return query[:maxNewsAPIQueryLength]
	}
	return query
}

func normalizeNewsAPIKeywords(keywords []string) []string {
	seen := make(map[string]bool)
	out := make([]string, 0, len(keywords))
	for _, k := range keywords {
		k = strings.Join(strings.Fields(strings.TrimSpace(k)), " ")
		if k == "" {
			continue
		}
		if len(k) > 80 {
			k = k[:80]
		}
		key := strings.ToLower(k)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, k)
		if len(out) >= maxNewsAPIKeywords {
			break
		}
	}
	if len(out) == 0 {
		for _, k := range config.DefaultNewsKeywords() {
			k = strings.Join(strings.Fields(strings.TrimSpace(k)), " ")
			if k == "" {
				continue
			}
			key := strings.ToLower(k)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, k)
			if len(out) >= maxNewsAPIKeywords {
				break
			}
		}
	}
	return out
}
