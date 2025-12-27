package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// 默认数据源URL
const (
	// 默认RSS新闻源
	DefaultRSSFeedCoinDesk     = "https://www.coindesk.com/arc/outboundfeeds/rss/"
	DefaultRSSFeedCoinTelegraph = "https://cointelegraph.com/rss"
	DefaultRSSFeedCryptoNews    = "https://cryptonews.com/news/feed/"
	
	// 默认恐慌贪婪指数API
	DefaultFearGreedIndexAPI = "https://api.alternative.me/fng/"
	
	// 默认Polymarket API
	DefaultPolymarketAPI = "https://api.polymarket.com/graphql"
	
	// 默认Reddit API（公开JSON API，无需认证）
	DefaultRedditAPIBase = "https://www.reddit.com"
)

// DataSourceManager 数据源管理器
type DataSourceManager struct {
	httpClient *http.Client
	cache      map[string]*CachedData
	mu         sync.RWMutex
}

// CachedData 缓存数据
type CachedData struct {
	Data      interface{}
	ExpiresAt time.Time
}

// RSSFeed RSS源
type RSSFeed struct {
	XMLName xml.Name `xml:"rss"`
	Channel Channel  `xml:"channel"`
}

// Channel RSS频道
type Channel struct {
	Title       string    `xml:"title"`
	Description string    `xml:"description"`
	Items       []RSSItem `xml:"item"`
}

// RSSItem RSS项
type RSSItem struct {
	Title       string `xml:"title"`
	Description string `xml:"description"`
	Link        string `xml:"link"`
	PubDate     string `xml:"pubDate"`
}

// FearGreedAPIResponse 恐慌贪婪指数API响应
type FearGreedAPIResponse struct {
	Name      string `json:"name"`
	Data      []struct {
		Value         string `json:"value"`
		ValueClassification string `json:"value_classification"`
		Timestamp     string `json:"timestamp"`
		TimeUntilUpdate string `json:"time_until_update"`
	} `json:"data"`
}

// NewDataSourceManager 创建数据源管理器
func NewDataSourceManager() *DataSourceManager {
	return &DataSourceManager{
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		cache: make(map[string]*CachedData),
	}
}

// FetchRSSFeed 获取RSS源
func (dsm *DataSourceManager) FetchRSSFeed(url string) ([]NewsItem, error) {
	// 检查缓存
	cacheKey := fmt.Sprintf("rss:%s", url)
	if cached := dsm.getCached(cacheKey); cached != nil {
		if items, ok := cached.([]NewsItem); ok {
			return items, nil
		}
	}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	req.Header.Set("User-Agent", "QuantMesh/1.0")

	resp, err := dsm.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP错误: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	var feed RSSFeed
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil, fmt.Errorf("解析RSS失败: %w", err)
	}

	newsItems := make([]NewsItem, 0, len(feed.Channel.Items))
	for _, item := range feed.Channel.Items {
		pubTime, _ := parseRSSDate(item.PubDate)
		newsItems = append(newsItems, NewsItem{
			Title:       item.Title,
			Description: item.Description,
			URL:         item.Link,
			PublishedAt: pubTime,
			Source:      "RSS",
		})
	}

	// 缓存5分钟
	dsm.setCached(cacheKey, newsItems, 5*time.Minute)

	return newsItems, nil
}

// FetchFearGreedIndex 获取恐慌贪婪指数
func (dsm *DataSourceManager) FetchFearGreedIndex(apiURL string) (*FearGreedIndex, error) {
	// 检查缓存
	cacheKey := "fear_greed_index"
	if cached := dsm.getCached(cacheKey); cached != nil {
		if index, ok := cached.(*FearGreedIndex); ok {
			return index, nil
		}
	}

	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	resp, err := dsm.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP错误: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	var apiResp FearGreedAPIResponse
	if err := json.Unmarshal(body, &apiResp); err != nil {
		return nil, fmt.Errorf("解析JSON失败: %w", err)
	}

	if len(apiResp.Data) == 0 {
		return nil, fmt.Errorf("API返回数据为空")
	}

	latest := apiResp.Data[0]
	value := 0
	if v, err := parseInt(latest.Value); err == nil {
		value = v
	}

	timestamp := time.Now()
	if ts, err := parseTimestamp(latest.Timestamp); err == nil {
		timestamp = ts
	}

	index := &FearGreedIndex{
		Value:         value,
		Classification: latest.ValueClassification,
		Timestamp:     timestamp,
	}

	// 缓存1小时
	dsm.setCached(cacheKey, index, 1*time.Hour)

	return index, nil
}

// getCached 获取缓存
func (dsm *DataSourceManager) getCached(key string) interface{} {
	dsm.mu.RLock()
	cached, exists := dsm.cache[key]
	if !exists {
		dsm.mu.RUnlock()
		return nil
	}

	expired := time.Now().After(cached.ExpiresAt)
	data := cached.Data
	dsm.mu.RUnlock()

	// 如果缓存已过期，需要写锁来删除
	if expired {
		dsm.mu.Lock()
		// 双重检查：可能在释放读锁和获取写锁之间，其他协程已经删除了该键
		if cached, exists := dsm.cache[key]; exists && time.Now().After(cached.ExpiresAt) {
			delete(dsm.cache, key)
		}
		dsm.mu.Unlock()
		return nil
	}

	return data
}

// setCached 设置缓存
func (dsm *DataSourceManager) setCached(key string, data interface{}, ttl time.Duration) {
	dsm.mu.Lock()
	defer dsm.mu.Unlock()

	dsm.cache[key] = &CachedData{
		Data:      data,
		ExpiresAt: time.Now().Add(ttl),
	}
}

// parseRSSDate 解析RSS日期
func parseRSSDate(dateStr string) (time.Time, error) {
	formats := []string{
		time.RFC1123,
		time.RFC1123Z,
		time.RFC822,
		time.RFC822Z,
		"Mon, 2 Jan 2006 15:04:05 MST",
		"Mon, 2 Jan 2006 15:04:05 -0700",
	}

	for _, format := range formats {
		if t, err := time.Parse(format, dateStr); err == nil {
			return t, nil
		}
	}

	return time.Now(), fmt.Errorf("无法解析日期: %s", dateStr)
}

// parseInt 解析整数
func parseInt(s string) (int, error) {
	var v int
	_, err := fmt.Sscanf(s, "%d", &v)
	return v, err
}

// parseTimestamp 解析时间戳
func parseTimestamp(s string) (time.Time, error) {
	var ts int64
	if _, err := fmt.Sscanf(s, "%d", &ts); err != nil {
		return time.Now(), err
	}
	return time.Unix(ts, 0), nil
}

// CleanupCache 清理过期缓存
func (dsm *DataSourceManager) CleanupCache() {
	dsm.mu.Lock()
	defer dsm.mu.Unlock()

	now := time.Now()
	for key, cached := range dsm.cache {
		if now.After(cached.ExpiresAt) {
			delete(dsm.cache, key)
		}
	}
}

// StartCacheCleanup 启动缓存清理协程
func (dsm *DataSourceManager) StartCacheCleanup(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			dsm.CleanupCache()
		}
	}
}

// FetchPolymarketMarkets 获取Polymarket市场列表
// keywords: 关键词列表，用于筛选相关市场
func (dsm *DataSourceManager) FetchPolymarketMarkets(apiURL string, keywords []string) ([]*PolymarketMarket, error) {
	// 检查缓存
	cacheKey := fmt.Sprintf("polymarket:markets:%v", keywords)
	if cached := dsm.getCached(cacheKey); cached != nil {
		if markets, ok := cached.([]*PolymarketMarket); ok {
			return markets, nil
		}
	}

	// GraphQL 查询
	query := `
		query GetMarkets($active: Boolean) {
			markets(active: $active, limit: 100) {
				id
				question
				description
				endDate
				outcomes
				volume
				liquidity
			}
		}
	`

	reqBody := map[string]interface{}{
		"query": query,
		"variables": map[string]interface{}{
			"active": true,
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %w", err)
	}

	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "QuantMesh/1.0")

	resp, err := dsm.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP错误: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	var graphQLResp struct {
		Data struct {
			Markets []struct {
				ID          string    `json:"id"`
				Question    string    `json:"question"`
				Description string    `json:"description"`
				EndDate     string    `json:"endDate"`
				Outcomes    []string  `json:"outcomes"`
				Volume      float64   `json:"volume"`
				Liquidity  float64   `json:"liquidity"`
			} `json:"markets"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.Unmarshal(body, &graphQLResp); err != nil {
		return nil, fmt.Errorf("解析JSON失败: %w", err)
	}

	if len(graphQLResp.Errors) > 0 {
		return nil, fmt.Errorf("GraphQL错误: %v", graphQLResp.Errors)
	}

	// 转换为内部格式并筛选
	markets := make([]*PolymarketMarket, 0)
	for _, m := range graphQLResp.Data.Markets {
		// 关键词筛选
		if len(keywords) > 0 {
			matched := false
			text := m.Question + " " + m.Description
			for _, keyword := range keywords {
				if containsIgnoreCase(text, keyword) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}

		endDate, _ := time.Parse(time.RFC3339, m.EndDate)
		markets = append(markets, &PolymarketMarket{
			ID:          m.ID,
			Question:    m.Question,
			Description: m.Description,
			EndDate:     endDate,
			Outcomes:    m.Outcomes,
			Volume:      m.Volume,
			Liquidity:   m.Liquidity,
		})
	}

	// 缓存5分钟
	dsm.setCached(cacheKey, markets, 5*time.Minute)

	return markets, nil
}

// FetchPolymarketMarketData 获取特定市场的详细数据
func (dsm *DataSourceManager) FetchPolymarketMarketData(apiURL string, marketID string) (*PolymarketMarketData, error) {
	// 检查缓存
	cacheKey := fmt.Sprintf("polymarket:market:%s", marketID)
	if cached := dsm.getCached(cacheKey); cached != nil {
		if data, ok := cached.(*PolymarketMarketData); ok {
			return data, nil
		}
	}

	// GraphQL 查询市场数据
	query := `
		query GetMarket($marketId: ID!) {
			market(id: $marketId) {
				id
				question
				condition {
					id
					outcomes {
						id
						title
						price
					}
				}
				volume24h
				bestBid
				bestAsk
				lastPrice
			}
		}
	`

	reqBody := map[string]interface{}{
		"query": query,
		"variables": map[string]interface{}{
			"marketId": marketID,
		},
	}

	jsonData, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %w", err)
	}

	req, err := http.NewRequest("POST", apiURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "QuantMesh/1.0")

	resp, err := dsm.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP错误: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	var graphQLResp struct {
		Data struct {
			Market struct {
				ID         string `json:"id"`
				Question   string `json:"question"`
				Condition  struct {
					ID       string `json:"id"`
					Outcomes []struct {
						ID    string  `json:"id"`
						Title string  `json:"title"`
						Price float64 `json:"price"`
					} `json:"outcomes"`
				} `json:"condition"`
				Volume24h float64 `json:"volume24h"`
				BestBid   float64 `json:"bestBid"`
				BestAsk   float64 `json:"bestAsk"`
				LastPrice float64 `json:"lastPrice"`
			} `json:"market"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}

	if err := json.Unmarshal(body, &graphQLResp); err != nil {
		return nil, fmt.Errorf("解析JSON失败: %w", err)
	}

	if len(graphQLResp.Errors) > 0 {
		return nil, fmt.Errorf("GraphQL错误: %v", graphQLResp.Errors)
	}

	m := graphQLResp.Data.Market
	
	// 提取YES和NO价格
	yesPrice := 0.0
	noPrice := 0.0
	for _, outcome := range m.Condition.Outcomes {
		if outcome.Title == "Yes" || outcome.Title == "YES" {
			yesPrice = outcome.Price
		} else if outcome.Title == "No" || outcome.Title == "NO" {
			noPrice = outcome.Price
		}
	}

	data := &PolymarketMarketData{
		MarketID:   m.ID,
		YesPrice:   yesPrice,
		NoPrice:    noPrice,
		Volume24h:  m.Volume24h,
		BestBid:    m.BestBid,
		BestAsk:    m.BestAsk,
		LastPrice:  m.LastPrice,
		Timestamp:  time.Now(),
	}

	// 缓存2分钟
	dsm.setCached(cacheKey, data, 2*time.Minute)

	return data, nil
}

// containsIgnoreCase 检查字符串是否包含子串（忽略大小写）
func containsIgnoreCase(s, substr string) bool {
	return len(s) >= len(substr) && 
		(len(substr) == 0 || 
			indexOfIgnoreCase(s, substr) >= 0)
}

// indexOfIgnoreCase 查找子串位置（忽略大小写）
func indexOfIgnoreCase(s, substr string) int {
	sLower := strings.ToLower(s)
	substrLower := strings.ToLower(substr)
	return strings.Index(sLower, substrLower)
}

// RedditAPIResponse Reddit API响应
type RedditAPIResponse struct {
	Data struct {
		Children []struct {
			Data struct {
				Title       string  `json:"title"`
				Selftext    string  `json:"selftext"`
				URL         string  `json:"url"`
				Subreddit   string  `json:"subreddit"`
				Score       int     `json:"score"`
				UpvoteRatio float64 `json:"upvote_ratio"`
				Created     float64 `json:"created_utc"`
				Author      string  `json:"author"`
			} `json:"data"`
		} `json:"children"`
	} `json:"data"`
}

// FetchRedditPosts 获取Reddit帖子
// subreddits: 子版块列表，如 ["Bitcoin", "ethereum", "CryptoCurrency"]
// limit: 每个子版块获取的帖子数量（默认25，最大100）
func (dsm *DataSourceManager) FetchRedditPosts(subreddits []string, limit int) ([]RedditPost, error) {
	if len(subreddits) == 0 {
		// 使用默认加密货币相关子版块
		subreddits = []string{"Bitcoin", "ethereum", "CryptoCurrency", "CryptoMarkets"}
	}
	
	if limit <= 0 || limit > 100 {
		limit = 25 // 默认25条
	}
	
	// 检查缓存
	cacheKey := fmt.Sprintf("reddit:%v:%d", subreddits, limit)
	if cached := dsm.getCached(cacheKey); cached != nil {
		if posts, ok := cached.([]RedditPost); ok {
			return posts, nil
		}
	}
	
	allPosts := make([]RedditPost, 0)
	
	// 遍历每个子版块获取帖子
	for _, subreddit := range subreddits {
		// Reddit JSON API格式: https://www.reddit.com/r/{subreddit}/hot.json?limit={limit}
		url := fmt.Sprintf("%s/r/%s/hot.json?limit=%d", DefaultRedditAPIBase, subreddit, limit)
		
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			continue // 跳过这个子版块
		}
		
		req.Header.Set("User-Agent", "QuantMesh/1.0 (by /u/quantmesh)")
		
		resp, err := dsm.httpClient.Do(req)
		if err != nil {
			continue // 跳过这个子版块
		}
		defer resp.Body.Close()
		
		if resp.StatusCode != http.StatusOK {
			continue // 跳过这个子版块
		}
		
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			continue
		}
		
		var redditResp RedditAPIResponse
		if err := json.Unmarshal(body, &redditResp); err != nil {
			continue
		}
		
		// 转换为内部格式
		for _, child := range redditResp.Data.Children {
			postData := child.Data
			createdAt := time.Unix(int64(postData.Created), 0)
			
			allPosts = append(allPosts, RedditPost{
				Title:       postData.Title,
				Content:     postData.Selftext,
				URL:         postData.URL,
				Subreddit:   postData.Subreddit,
				Score:       postData.Score,
				UpvoteRatio: postData.UpvoteRatio,
				CreatedAt:   createdAt,
				Author:      postData.Author,
			})
		}
	}
	
	// 按分数排序（热门优先）
	sort.Slice(allPosts, func(i, j int) bool {
		return allPosts[i].Score > allPosts[j].Score
	})
	
	// 缓存10分钟
	dsm.setCached(cacheKey, allPosts, 10*time.Minute)
	
	return allPosts, nil
}

