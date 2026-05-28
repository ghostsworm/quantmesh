package web

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"quantmesh/config"
	"quantmesh/logger"
	"quantmesh/polymarket"
)

// ========== 市场情报數據源相关API ==========

var (
	// 數據源管理器提供者（需要從main.go注入）
	dataSourceProvider DataSourceProvider
)

// DataSourceProvider 數據源提供者接口
type DataSourceProvider interface {
	GetRSSFeeds() ([]RSSFeedInfo, error)
	GetFearGreedIndex() (*FearGreedIndexInfo, error)
	GetRedditPosts(subreddits []string, limit int) ([]RedditPostInfo, error)
	GetPolymarketMarkets(keywords []string) ([]PolymarketMarketInfo, error)
}

// RSSFeedInfo RSS源信息
type RSSFeedInfo struct {
	Title       string        `json:"title"`
	Description string        `json:"description"`
	URL         string        `json:"url"`
	Items       []RSSItemInfo `json:"items"`
	LastUpdate  time.Time     `json:"last_update"`
}

// RSSItemInfo RSS项信息
type RSSItemInfo struct {
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Link        string    `json:"link"`
	PubDate     time.Time `json:"pub_date"`
	Source      string    `json:"source"`
}

// FearGreedIndexInfo 恐慌贪婪指數信息
type FearGreedIndexInfo struct {
	Value          int       `json:"value"`
	Classification string    `json:"classification"`
	Timestamp      time.Time `json:"timestamp"`
}

// RedditPostInfo Reddit帖子信息
type RedditPostInfo struct {
	Title       string    `json:"title"`
	Content     string    `json:"content"`
	URL         string    `json:"url"`
	Subreddit   string    `json:"subreddit"`
	Score       int       `json:"score"`
	UpvoteRatio float64   `json:"upvote_ratio"`
	CreatedAt   time.Time `json:"created_at"`
	Author      string    `json:"author"`
}

// PolymarketMarketInfo Polymarket市场信息
type PolymarketMarketInfo struct {
	ID             string    `json:"id"`
	Question       string    `json:"question"`
	Description    string    `json:"description"`
	EndDate        time.Time `json:"end_date"`
	Outcomes       []string  `json:"outcomes"`
	OutcomePrices  []float64 `json:"outcome_prices,omitempty"`
	YesProbability float64   `json:"yes_probability,omitempty"`
	Volume         float64   `json:"volume"`
	Liquidity      float64   `json:"liquidity"`
}

// SetDataSourceProvider 設置數據源提供者
func SetDataSourceProvider(provider DataSourceProvider) {
	dataSourceProvider = provider
}

// InitDefaultDataSourceProvider 初始化默認的數據源提供者（內置實現）
func InitDefaultDataSourceProvider() {
	// 如果已經設置了提供者，不要再初始化
	if dataSourceProvider != nil {
		return
	}

	provider := &builtinDataSourceProvider{
		httpClient: &http.Client{Timeout: 30 * time.Second},
		rssFeeds: []string{
			"https://www.coindesk.com/arc/outboundfeeds/rss/",
			"https://cointelegraph.com/rss",
			"https://cryptonews.com/news/feed/",
		},
		fearGreedAPIURL: "https://api.alternative.me/fng/",
		gammaBaseURL:    "https://gamma-api.polymarket.com",
	}
	dataSourceProvider = provider
	logger.Info("✅ 已初始化內置數據源提供者")
}

// ApplyDataSourcePolymarketConfig 根據配置設置 Gamma API 根地址（僅內置提供者）。
// 優先 macro_event.gamma_api_url，其次 ai.modules.polymarket_signal.api_url，默認 gamma-api.polymarket.com。
func ApplyDataSourcePolymarketConfig(cfg *config.Config) {
	if cfg == nil {
		return
	}
	p, ok := dataSourceProvider.(*builtinDataSourceProvider)
	if !ok {
		return
	}
	gamma := strings.TrimSpace(cfg.MacroEvent.GammaAPIURL)
	if gamma == "" {
		gamma = strings.TrimSpace(cfg.AI.Modules.PolymarketSignal.APIURL)
	}
	if gamma == "" {
		gamma = "https://gamma-api.polymarket.com"
	}
	p.mu.Lock()
	p.gammaBaseURL = strings.TrimSuffix(gamma, "/")
	p.mu.Unlock()
	logger.Info("✅ 市場情报 Polymarket Gamma 地址: %s", p.gammaBaseURL)
}

// builtinDataSourceProvider 內置數據源提供者（不依賴 AI 模塊）
type builtinDataSourceProvider struct {
	httpClient          *http.Client
	rssFeeds            []string
	fearGreedAPIURL     string
	gammaBaseURL        string
	mu                  sync.RWMutex
	cachedRSS           []RSSFeedInfo
	cachedFearGreed     *FearGreedIndexInfo
	lastRSSUpdate       time.Time
	lastFearGreedUpdate time.Time
	// Polymarket 列表緩存（減輕 Gamma 壓力；關鍵詞搜索不長期緩存）
	polyMu        sync.RWMutex
	cachedPoly    []PolymarketMarketInfo
	cachedPolyKW  string
	lastPolyFetch time.Time
}

// GetRSSFeeds 獲取RSS新聞
func (p *builtinDataSourceProvider) GetRSSFeeds() ([]RSSFeedInfo, error) {
	p.mu.RLock()
	if time.Since(p.lastRSSUpdate) < 5*time.Minute && len(p.cachedRSS) > 0 {
		defer p.mu.RUnlock()
		return p.cachedRSS, nil
	}
	p.mu.RUnlock()

	p.mu.Lock()
	defer p.mu.Unlock()

	// 雙重檢查
	if time.Since(p.lastRSSUpdate) < 5*time.Minute && len(p.cachedRSS) > 0 {
		return p.cachedRSS, nil
	}

	feeds := make([]RSSFeedInfo, 0)
	for _, feedURL := range p.rssFeeds {
		items, err := p.fetchRSSFeed(feedURL)
		if err != nil {
			logger.Warn("獲取 RSS 源失敗: %s, 錯誤: %v", feedURL, err)
			continue
		}

		sourceName := extractSourceName(feedURL)
		feeds = append(feeds, RSSFeedInfo{
			Title:       sourceName,
			Description: fmt.Sprintf("来自 %s 的加密貨幣新闻", sourceName),
			URL:         feedURL,
			Items:       items,
			LastUpdate:  time.Now(),
		})
	}

	p.cachedRSS = feeds
	p.lastRSSUpdate = time.Now()
	return feeds, nil
}

// fetchRSSFeed 獲取單個RSS源的內容
func (p *builtinDataSourceProvider) fetchRSSFeed(feedURL string) ([]RSSItemInfo, error) {
	resp, err := p.httpClient.Get(feedURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP status: %d", resp.StatusCode)
	}

	// 簡單的XML解析（使用標準庫）
	// 這是一個簡化實現，實際生產建議使用專門的RSS庫
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// 簡單的正則表達式提取（生產環境應該使用XML解析器）
	items := make([]RSSItemInfo, 0)

	// 嘗試解析為XML
	var rss struct {
		Channel struct {
			Title string `xml:"title"`
			Items []struct {
				Title       string `xml:"title"`
				Link        string `xml:"link"`
				Description string `xml:"description"`
				PubDate     string `xml:"pubDate"`
			} `xml:"item"`
		} `xml:"channel"`
	}

	err = xml.Unmarshal(body, &rss)
	if err != nil {
		// XML解析失敗，返回空列表
		return items, nil
	}

	sourceName := extractSourceName(feedURL)
	for _, item := range rss.Channel.Items {
		pubDate := time.Now()
		if item.PubDate != "" {
			pubDate, _ = time.Parse(time.RFC1123, item.PubDate)
		}

		items = append(items, RSSItemInfo{
			Title:       item.Title,
			Description: stripHTMLTags(item.Description),
			Link:        item.Link,
			PubDate:     pubDate,
			Source:      sourceName,
		})
	}

	return items, nil
}

// stripHTMLTags 移除 HTML 標簽，返回純文本
func stripHTMLTags(html string) string {
	if html == "" {
		return ""
	}

	// 簡單的 HTML 標簽移除（使用正則表達式）
	// 移除 <script> 和 <style> 標簽及其內容。
	// Go regexp（RE2）不支持 \1 回溯引用，須分開匹配；(?is) 使 . 可跨行匹配標簽內容。
	for _, pattern := range []string{
		`(?is)<script[^>]*>.*?</script>`,
		`(?is)<style[^>]*>.*?</style>`,
	} {
		re := regexp.MustCompile(pattern)
		html = re.ReplaceAllString(html, "")
	}

	// 移除 HTML 標簽
	tagRegex := `<[^>]*>`
	re := regexp.MustCompile(tagRegex)
	html = re.ReplaceAllString(html, "")

	// 移除連續的空白字符
	spaceRegex := `\s+`
	re = regexp.MustCompile(spaceRegex)
	html = re.ReplaceAllString(html, " ")

	// 移除 HTML 實體（如 &nbsp;, &amp;, 等）
	html = strings.ReplaceAll(html, "&nbsp;", " ")
	html = strings.ReplaceAll(html, "&amp;", "&")
	html = strings.ReplaceAll(html, "&lt;", "<")
	html = strings.ReplaceAll(html, "&gt;", ">")
	html = strings.ReplaceAll(html, "&quot;", "\"")
	html = strings.ReplaceAll(html, "&#39;", "'")

	// 去除首尾空白
	html = strings.TrimSpace(html)

	return html
}

// GetFearGreedIndex 獲取恐慌贪婪指數
func (p *builtinDataSourceProvider) GetFearGreedIndex() (*FearGreedIndexInfo, error) {
	p.mu.RLock()
	if time.Since(p.lastFearGreedUpdate) < 1*time.Hour && p.cachedFearGreed != nil {
		defer p.mu.RUnlock()
		return p.cachedFearGreed, nil
	}
	p.mu.RUnlock()

	p.mu.Lock()
	defer p.mu.Unlock()

	// 雙重檢查
	if time.Since(p.lastFearGreedUpdate) < 1*time.Hour && p.cachedFearGreed != nil {
		return p.cachedFearGreed, nil
	}

	resp, err := p.httpClient.Get(p.fearGreedAPIURL)
	if err != nil {
		// 返回緩存值（如果有）
		if p.cachedFearGreed != nil {
			return p.cachedFearGreed, nil
		}
		return nil, err
	}
	defer resp.Body.Close()

	// Alternative.me API 返回格式: data 為數組，value/timestamp 為字串
	// 例: {"data":[{"value":"12","value_classification":"Extreme Fear","timestamp":"1774051200"}]}
	var result struct {
		Data []struct {
			Value          string `json:"value"`
			Classification string `json:"value_classification"`
			Timestamp      string `json:"timestamp"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		// 返回緩存值（如果有）
		if p.cachedFearGreed != nil {
			return p.cachedFearGreed, nil
		}
		return nil, err
	}

	if len(result.Data) == 0 {
		if p.cachedFearGreed != nil {
			return p.cachedFearGreed, nil
		}
		return nil, fmt.Errorf("API 返回空數據")
	}

	item := result.Data[0]
	value, _ := strconv.Atoi(item.Value)
	timestampSec, _ := strconv.ParseInt(item.Timestamp, 10, 64)
	if timestampSec == 0 {
		timestampSec = time.Now().Unix()
	}

	fgi := &FearGreedIndexInfo{
		Value:          value,
		Classification: item.Classification,
		Timestamp:      time.Unix(timestampSec, 0),
	}

	p.cachedFearGreed = fgi
	p.lastFearGreedUpdate = time.Now()
	return fgi, nil
}

// GetRedditPosts 獲取Reddit帖子（暫未實現）
func (p *builtinDataSourceProvider) GetRedditPosts(subreddits []string, limit int) ([]RedditPostInfo, error) {
	// Reddit API 需要 OAuth，暫未實現
	return []RedditPostInfo{}, nil
}

// GetPolymarketMarkets 從 Polymarket Gamma REST 拉取活躍市場（無需 token）。
func (p *builtinDataSourceProvider) GetPolymarketMarkets(keywords []string) ([]PolymarketMarketInfo, error) {
	kwKey := strings.Join(keywords, "\x1e")
	p.polyMu.RLock()
	base := p.gammaBaseURL
	if base == "" {
		base = "https://gamma-api.polymarket.com"
	}
	if kwKey == p.cachedPolyKW && time.Since(p.lastPolyFetch) < 5*time.Minute && len(p.cachedPoly) > 0 {
		out := append([]PolymarketMarketInfo(nil), p.cachedPoly...)
		p.polyMu.RUnlock()
		return out, nil
	}
	p.polyMu.RUnlock()

	raw, err := polymarket.FetchActiveMarkets(base, keywords, p.httpClient, 50)
	if err != nil {
		return nil, err
	}
	out := make([]PolymarketMarketInfo, 0, len(raw))
	for _, m := range raw {
		out = append(out, PolymarketMarketInfo{
			ID:             m.ID,
			Question:       m.Question,
			Description:    m.Description,
			EndDate:        m.EndDate,
			Outcomes:       m.Outcomes,
			OutcomePrices:  m.OutcomePrices,
			YesProbability: m.YesProbability,
			Volume:         m.Volume,
			Liquidity:      m.Liquidity,
		})
	}
	p.polyMu.Lock()
	p.cachedPoly = append([]PolymarketMarketInfo(nil), out...)
	p.cachedPolyKW = kwKey
	p.lastPolyFetch = time.Now()
	p.polyMu.Unlock()
	return out, nil
}

// dataSourceAdapter 數據源适配器
// 注意：這個适配器使用反射来調用方法，避免循环依赖
type dataSourceAdapter struct {
	dsm              interface{}
	rssFeeds         []string
	fearGreedAPIURL  string
	polymarketAPIURL string
}

// NewDataSourceAdapter 創建數據源适配器
// dsm 应該是 *ai.DataSourceManager 類型，但使用 interface{} 避免循环依赖
func NewDataSourceAdapter(dsm interface{}, rssFeeds []string, fearGreedAPIURL, polymarketAPIURL string) DataSourceProvider {
	return &dataSourceAdapter{
		dsm:              dsm,
		rssFeeds:         rssFeeds,
		fearGreedAPIURL:  fearGreedAPIURL,
		polymarketAPIURL: polymarketAPIURL,
	}
}

// GetRSSFeeds 獲取RSS源
func (a *dataSourceAdapter) GetRSSFeeds() ([]RSSFeedInfo, error) {
	if a.dsm == nil {
		return nil, fmt.Errorf("數據源管理器未初始化")
	}

	// 使用反射調用方法（避免循环依赖）
	dsmValue := reflect.ValueOf(a.dsm)
	if !dsmValue.IsValid() {
		return nil, fmt.Errorf("無效的數據源管理器")
	}

	feeds := make([]RSSFeedInfo, 0)

	// 如果没有配置RSS源，使用默认源
	rssFeeds := a.rssFeeds
	if len(rssFeeds) == 0 {
		rssFeeds = []string{
			"https://www.coindesk.com/arc/outboundfeeds/rss/",
			"https://cointelegraph.com/rss",
			"https://cryptonews.com/news/feed/",
		}
	}

	for _, feedURL := range rssFeeds {
		method := dsmValue.MethodByName("FetchRSSFeed")
		if !method.IsValid() {
			continue
		}

		results := method.Call([]reflect.Value{reflect.ValueOf(feedURL)})
		if len(results) != 2 {
			continue
		}

		if !results[1].IsNil() {
			// 錯误，跳過這個源
			continue
		}

		itemsValue := results[0]
		if itemsValue.IsNil() {
			continue
		}

		// 轉换為[]NewsItem（ai包中的類型）
		items := itemsValue.Interface()
		itemsSlice := reflect.ValueOf(items)
		if itemsSlice.Kind() != reflect.Slice {
			continue
		}

		rssItems := make([]RSSItemInfo, 0)
		for i := 0; i < itemsSlice.Len(); i++ {
			item := itemsSlice.Index(i)
			if !item.IsValid() {
				continue
			}

			// 提取字段
			title := getFieldString(item, "Title")
			description := getFieldString(item, "Description")
			url := getFieldString(item, "URL")
			source := getFieldString(item, "Source")
			pubDate := getFieldTime(item, "PublishedAt")

			rssItems = append(rssItems, RSSItemInfo{
				Title:       title,
				Description: description,
				Link:        url,
				PubDate:     pubDate,
				Source:      source,
			})
		}

		if len(rssItems) > 0 {
			// 從URL提取源名称
			sourceName := extractSourceName(feedURL)
			feeds = append(feeds, RSSFeedInfo{
				Title:       sourceName,
				Description: fmt.Sprintf("来自 %s 的加密貨幣新闻", sourceName),
				URL:         feedURL,
				Items:       rssItems,
				LastUpdate:  time.Now(),
			})
		}
	}

	return feeds, nil
}

// GetFearGreedIndex 獲取恐慌贪婪指數
func (a *dataSourceAdapter) GetFearGreedIndex() (*FearGreedIndexInfo, error) {
	if a.dsm == nil {
		return nil, fmt.Errorf("數據源管理器未初始化")
	}

	apiURL := a.fearGreedAPIURL
	if apiURL == "" {
		apiURL = "https://api.alternative.me/fng/"
	}

	dsmValue := reflect.ValueOf(a.dsm)
	method := dsmValue.MethodByName("FetchFearGreedIndex")
	if !method.IsValid() {
		return nil, fmt.Errorf("方法不存在")
	}

	results := method.Call([]reflect.Value{reflect.ValueOf(apiURL)})
	if len(results) != 2 {
		return nil, fmt.Errorf("返回值數量錯误")
	}

	if !results[1].IsNil() {
		return nil, results[1].Interface().(error)
	}

	indexValue := results[0]
	if indexValue.IsNil() {
		return nil, fmt.Errorf("返回值為空")
	}

	index := indexValue.Elem()
	value := int(getFieldInt(index, "Value"))
	classification := getFieldString(index, "Classification")
	timestamp := getFieldTime(index, "Timestamp")

	return &FearGreedIndexInfo{
		Value:          value,
		Classification: classification,
		Timestamp:      timestamp,
	}, nil
}

// GetRedditPosts 獲取Reddit帖子
func (a *dataSourceAdapter) GetRedditPosts(subreddits []string, limit int) ([]RedditPostInfo, error) {
	if a.dsm == nil {
		return nil, fmt.Errorf("數據源管理器未初始化")
	}

	if len(subreddits) == 0 {
		subreddits = []string{"Bitcoin", "ethereum", "CryptoCurrency", "CryptoMarkets"}
	}

	dsmValue := reflect.ValueOf(a.dsm)
	method := dsmValue.MethodByName("FetchRedditPosts")
	if !method.IsValid() {
		return nil, fmt.Errorf("方法不存在")
	}

	results := method.Call([]reflect.Value{
		reflect.ValueOf(subreddits),
		reflect.ValueOf(limit),
	})

	if len(results) != 2 {
		return nil, fmt.Errorf("返回值數量錯误")
	}

	if !results[1].IsNil() {
		return nil, results[1].Interface().(error)
	}

	postsValue := results[0]
	if postsValue.IsNil() {
		return []RedditPostInfo{}, nil
	}

	postsSlice := reflect.ValueOf(postsValue.Interface())
	if postsSlice.Kind() != reflect.Slice {
		return []RedditPostInfo{}, nil
	}

	posts := make([]RedditPostInfo, 0)
	for i := 0; i < postsSlice.Len(); i++ {
		post := postsSlice.Index(i)
		if !post.IsValid() {
			continue
		}

		posts = append(posts, RedditPostInfo{
			Title:       getFieldString(post, "Title"),
			Content:     getFieldString(post, "Content"),
			URL:         getFieldString(post, "URL"),
			Subreddit:   getFieldString(post, "Subreddit"),
			Score:       int(getFieldInt(post, "Score")),
			UpvoteRatio: getFieldFloat(post, "UpvoteRatio"),
			CreatedAt:   getFieldTime(post, "CreatedAt"),
			Author:      getFieldString(post, "Author"),
		})
	}

	return posts, nil
}

// GetPolymarketMarkets 獲取Polymarket市场（優先使用 Gamma REST API，無需 dsm）
func (a *dataSourceAdapter) GetPolymarketMarkets(keywords []string) ([]PolymarketMarketInfo, error) {
	// 優先使用 Gamma REST API（無需認證、免費）
	gammaURL := "https://gamma-api.polymarket.com"
	if a.polymarketAPIURL != "" && strings.Contains(a.polymarketAPIURL, "gamma") {
		gammaURL = strings.TrimSuffix(a.polymarketAPIURL, "/")
	}
	markets, err := fetchPolymarketMarketsFromGamma(gammaURL, keywords)
	if err == nil && len(markets) > 0 {
		return markets, nil
	}
	markets = make([]PolymarketMarketInfo, 0)
	// 回退：嘗試通過 dsm 反射調用（兼容舊實現）
	if a.dsm != nil {
		dsmValue := reflect.ValueOf(a.dsm)
		method := dsmValue.MethodByName("FetchPolymarketMarkets")
		if method.IsValid() {
			apiURL := a.polymarketAPIURL
			if apiURL == "" {
				apiURL = "https://api.polymarket.com/graphql"
			}
			results := method.Call([]reflect.Value{
				reflect.ValueOf(apiURL),
				reflect.ValueOf(keywords),
			})
			if len(results) == 2 && results[1].IsNil() && !results[0].IsNil() {
				marketsSlice := reflect.ValueOf(results[0].Interface())
				if marketsSlice.Kind() == reflect.Slice {
					for i := 0; i < marketsSlice.Len(); i++ {
						market := marketsSlice.Index(i)
						if market.Kind() == reflect.Ptr {
							market = market.Elem()
						}
						outcomesValue := market.FieldByName("Outcomes")
						outcomes := []string{}
						if outcomesValue.IsValid() && outcomesValue.Kind() == reflect.Slice {
							for j := 0; j < outcomesValue.Len(); j++ {
								outcomes = append(outcomes, outcomesValue.Index(j).String())
							}
						}
						markets = append(markets, PolymarketMarketInfo{
							ID:             getFieldString(market, "ID"),
							Question:       getFieldString(market, "Question"),
							Description:    getFieldString(market, "Description"),
							EndDate:        getFieldTime(market, "EndDate"),
							Outcomes:       outcomes,
							YesProbability: getFieldFloat(market, "YesProbability"),
							Volume:         getFieldFloat(market, "Volume"),
							Liquidity:      getFieldFloat(market, "Liquidity"),
						})
					}
					return markets, nil
				}
			}
		}
	}
	return markets, err
}

// fetchPolymarketMarketsFromGamma 從 Polymarket Gamma REST API 拉取市場（與內置提供者共用 polymarket 包）。
func fetchPolymarketMarketsFromGamma(baseURL string, keywords []string) ([]PolymarketMarketInfo, error) {
	raw, err := polymarket.FetchActiveMarkets(baseURL, keywords, &http.Client{Timeout: 15 * time.Second}, 50)
	if err != nil {
		return nil, err
	}
	out := make([]PolymarketMarketInfo, 0, len(raw))
	for _, m := range raw {
		out = append(out, PolymarketMarketInfo{
			ID:             m.ID,
			Question:       m.Question,
			Description:    m.Description,
			EndDate:        m.EndDate,
			Outcomes:       m.Outcomes,
			OutcomePrices:  m.OutcomePrices,
			YesProbability: m.YesProbability,
			Volume:         m.Volume,
			Liquidity:      m.Liquidity,
		})
	}
	return out, nil
}

// 辅助函數：從反射值獲取字符串字段
func getFieldString(v reflect.Value, fieldName string) string {
	field := v.FieldByName(fieldName)
	if !field.IsValid() {
		return ""
	}
	return field.String()
}

// 辅助函數：從反射值獲取整數字段
func getFieldInt(v reflect.Value, fieldName string) int64 {
	field := v.FieldByName(fieldName)
	if !field.IsValid() {
		return 0
	}
	return field.Int()
}

// 辅助函數：從反射值獲取浮点數字段
func getFieldFloat(v reflect.Value, fieldName string) float64 {
	field := v.FieldByName(fieldName)
	if !field.IsValid() {
		return 0
	}
	return field.Float()
}

// 辅助函數：從反射值獲取時间字段
func getFieldTime(v reflect.Value, fieldName string) time.Time {
	field := v.FieldByName(fieldName)
	if !field.IsValid() {
		return time.Now()
	}
	if t, ok := field.Interface().(time.Time); ok {
		return t
	}
	return time.Now()
}

// 辅助函數：從URL提取源名称
func extractSourceName(url string) string {
	url = strings.TrimPrefix(url, "https://")
	url = strings.TrimPrefix(url, "http://")
	parts := strings.Split(url, "/")
	if len(parts) > 0 {
		return parts[0]
	}
	return url
}
