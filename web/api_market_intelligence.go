package web

import (
	"net/http"
	"strconv"
	"strings"

	"quantmesh/logger"

	"github.com/gin-gonic/gin"
)

// ========== 市场情报 API ==========

// getMarketIntelligence 獲取市場情报數據
// GET /api/market-intelligence
// 查詢参數：
//   - source: 數據源類型（rss, fear_greed, reddit, polymarket，默认全部）
//   - keyword: 搜索关键词（可選）
//   - limit: 返回數量限制（默认50）
func getMarketIntelligence(c *gin.Context) {
	if dataSourceProvider == nil {
		c.JSON(http.StatusOK, gin.H{
			"rss_feeds":    []interface{}{},
			"fear_greed":   nil,
			"reddit_posts": []interface{}{},
			"polymarket":   []interface{}{},
		})
		return
	}

	source := c.Query("source")
	keyword := c.Query("keyword")
	limitStr := c.DefaultQuery("limit", "50")
	limit := 50
	if l, err := strconv.Atoi(limitStr); err == nil && l > 0 {
		limit = l
		if limit > 200 {
			limit = 200 // 最大限制200
		}
	}

	result := make(map[string]interface{})

	// 獲取RSS新闻
	if source == "" || source == "rss" {
		rssFeeds, err := dataSourceProvider.GetRSSFeeds()
		if err == nil {
			// 如果有关键词，進行筛选
			if keyword != "" {
				filtered := make([]RSSFeedInfo, 0)
				keywordLower := strings.ToLower(keyword)
				for _, feed := range rssFeeds {
					filteredItems := make([]RSSItemInfo, 0)
					for _, item := range feed.Items {
						titleLower := strings.ToLower(item.Title)
						descLower := strings.ToLower(item.Description)
						if strings.Contains(titleLower, keywordLower) || strings.Contains(descLower, keywordLower) {
							filteredItems = append(filteredItems, item)
						}
					}
					if len(filteredItems) > 0 {
						feed.Items = filteredItems[:min(len(filteredItems), limit)]
						filtered = append(filtered, feed)
					}
				}
				result["rss_feeds"] = filtered
			} else {
				// 限制每個源的条目數
				for i := range rssFeeds {
					if len(rssFeeds[i].Items) > limit {
						rssFeeds[i].Items = rssFeeds[i].Items[:limit]
					}
				}
				result["rss_feeds"] = rssFeeds
			}
		} else {
			result["rss_feeds"] = []interface{}{}
		}
	}

	// 獲取恐慌贪婪指數
	if source == "" || source == "fear_greed" {
		fearGreed, err := dataSourceProvider.GetFearGreedIndex()
		if err == nil {
			result["fear_greed"] = fearGreed
		} else {
			result["fear_greed"] = nil
		}
	}

	// 獲取Reddit帖子
	if source == "" || source == "reddit" {
		// 默认子版塊
		subreddits := []string{"Bitcoin", "ethereum", "CryptoCurrency", "CryptoMarkets"}
		redditPosts, err := dataSourceProvider.GetRedditPosts(subreddits, limit)
		if err == nil {
			// 如果有关键词，進行筛选
			if keyword != "" {
				filtered := make([]RedditPostInfo, 0)
				keywordLower := strings.ToLower(keyword)
				for _, post := range redditPosts {
					titleLower := strings.ToLower(post.Title)
					contentLower := strings.ToLower(post.Content)
					if strings.Contains(titleLower, keywordLower) || strings.Contains(contentLower, keywordLower) {
						filtered = append(filtered, post)
					}
				}
				result["reddit_posts"] = filtered[:min(len(filtered), limit)]
			} else {
				result["reddit_posts"] = redditPosts
			}
		} else {
			result["reddit_posts"] = []interface{}{}
		}
	}

	// 獲取Polymarket市场
	if source == "" || source == "polymarket" {
		keywords := []string{}
		if keyword != "" {
			keywords = []string{keyword}
		}
		polymarketMarkets, err := dataSourceProvider.GetPolymarketMarkets(keywords)
		if err != nil {
			logger.Warn("獲取 Polymarket 市場失敗: %v", err)
			result["polymarket"] = []interface{}{}
		} else {
			if len(polymarketMarkets) > limit {
				result["polymarket"] = polymarketMarkets[:limit]
			} else {
				result["polymarket"] = polymarketMarkets
			}
		}
	}

	c.JSON(http.StatusOK, result)
}

// min 返回两個整數中的较小值
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
