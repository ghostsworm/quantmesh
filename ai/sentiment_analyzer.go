package ai

import (
	"context"
	"fmt"
	"sync"
	"time"

	"quantmesh/config"
	"quantmesh/logger"
)

// SentimentAnalyzer AI市场情绪分析器
type SentimentAnalyzer struct {
	aiService       AIService
	cfg             *config.Config
	dataSourceMgr   *DataSourceManager
	
	lastAnalysis     *SentimentAnalysisResponse
	lastAnalysisTime time.Time
	mu               sync.RWMutex
	
	ctx         context.Context
	cancel      context.CancelFunc
}

// NewSentimentAnalyzer 创建情绪分析器
func NewSentimentAnalyzer(
	aiService AIService,
	cfg *config.Config,
	dataSourceMgr *DataSourceManager,
) *SentimentAnalyzer {
	ctx, cancel := context.WithCancel(context.Background())
	
	return &SentimentAnalyzer{
		aiService:     aiService,
		cfg:            cfg,
		dataSourceMgr: dataSourceMgr,
		ctx:            ctx,
		cancel:         cancel,
	}
}

// Start 启动情绪分析器
func (sa *SentimentAnalyzer) Start() {
	if !sa.cfg.AI.Modules.SentimentAnalysis.Enabled {
		return
	}
	
	interval := time.Duration(sa.cfg.AI.Modules.SentimentAnalysis.AnalysisInterval) * time.Second
	if interval <= 0 {
		interval = 600 * time.Second // 默认10分钟
	}
	
	// 启动数据源缓存清理
	go sa.dataSourceMgr.StartCacheCleanup(sa.ctx)
	
	go sa.analysisLoop(interval)
	
	// 输出数据源配置信息
	logger.Info("✅ AI市场情绪分析器已启动 (分析间隔: %v)", interval)
	
	// 显示数据源配置信息
	newsEnabled := sa.cfg.AI.Modules.SentimentAnalysis.DataSources.News.Enabled
	fearGreedEnabled := sa.cfg.AI.Modules.SentimentAnalysis.DataSources.FearGreedIndex.Enabled
	
	if newsEnabled || fearGreedEnabled {
		logger.Info("📰 [AI情绪分析] 数据源配置:")
		if newsEnabled {
			rssFeeds := sa.cfg.AI.Modules.SentimentAnalysis.DataSources.News.RSSFeeds
			if len(rssFeeds) == 0 {
				logger.Info("   - 新闻源: 使用默认RSS源 (CoinDesk, CoinTelegraph, CryptoNews)")
			} else {
				logger.Info("   - 新闻源: %d 个自定义RSS源", len(rssFeeds))
				for i, feed := range rssFeeds {
					if i < 3 { // 只显示前3个
						logger.Info("     • %s", feed)
					}
				}
				if len(rssFeeds) > 3 {
					logger.Info("     ... 还有 %d 个源", len(rssFeeds)-3)
				}
			}
		}
		if fearGreedEnabled {
			apiURL := sa.cfg.AI.Modules.SentimentAnalysis.DataSources.FearGreedIndex.APIURL
			if apiURL == "" {
				logger.Info("   - 恐慌贪婪指数: 使用默认API (alternative.me)")
			} else {
				logger.Info("   - 恐慌贪婪指数: %s", apiURL)
			}
		}
		socialMediaEnabled := sa.cfg.AI.Modules.SentimentAnalysis.DataSources.SocialMedia.Enabled
		if socialMediaEnabled {
			subreddits := sa.cfg.AI.Modules.SentimentAnalysis.DataSources.SocialMedia.Subreddits
			if len(subreddits) == 0 {
				logger.Info("   - Reddit: 使用默认子版块 (Bitcoin, ethereum, CryptoCurrency, CryptoMarkets)")
			} else {
				logger.Info("   - Reddit: %d 个自定义子版块", len(subreddits))
				for i, sub := range subreddits {
					if i < 3 {
						logger.Info("     • r/%s", sub)
					}
				}
				if len(subreddits) > 3 {
					logger.Info("     ... 还有 %d 个子版块", len(subreddits)-3)
				}
			}
		}
		logger.Info("📰 [AI情绪分析] ✨ 开箱即用，无需额外配置！")
	}
}

// Stop 停止情绪分析器
func (sa *SentimentAnalyzer) Stop() {
	if sa.cancel != nil {
		sa.cancel()
	}
}

// analysisLoop 分析循环
func (sa *SentimentAnalyzer) analysisLoop(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	
	for {
		select {
		case <-sa.ctx.Done():
			return
		case <-ticker.C:
			if err := sa.performAnalysis(); err != nil {
				logger.Warn("⚠️ AI情绪分析失败: %v", err)
			}
		}
	}
}

// performAnalysis 执行分析
func (sa *SentimentAnalyzer) performAnalysis() error {
	symbol := sa.cfg.Trading.Symbol
	
	// 收集新闻数据
	newsItems := make([]NewsItem, 0)
	
	if sa.cfg.AI.Modules.SentimentAnalysis.DataSources.News.Enabled {
		// 获取RSS源列表（如果配置为空，使用默认源）
		rssFeeds := sa.cfg.AI.Modules.SentimentAnalysis.DataSources.News.RSSFeeds
		if len(rssFeeds) == 0 {
			// 使用默认RSS源
			rssFeeds = []string{
				DefaultRSSFeedCoinDesk,
				DefaultRSSFeedCoinTelegraph,
				DefaultRSSFeedCryptoNews,
			}
			logger.Info("📰 [AI情绪分析] 使用默认RSS新闻源: CoinDesk, CoinTelegraph, CryptoNews")
		}
		
		for _, feedURL := range rssFeeds {
			items, err := sa.dataSourceMgr.FetchRSSFeed(feedURL)
			if err != nil {
				logger.Warn("⚠️ 获取RSS源失败 %s: %v", feedURL, err)
				continue
			}
			newsItems = append(newsItems, items...)
		}
		
		if len(newsItems) > 0 {
			logger.Info("📰 [AI情绪分析] 已获取 %d 条新闻", len(newsItems))
		}
	}
	
	// 获取恐慌贪婪指数
	var fearGreedIndex *FearGreedIndex
	if sa.cfg.AI.Modules.SentimentAnalysis.DataSources.FearGreedIndex.Enabled {
		apiURL := sa.cfg.AI.Modules.SentimentAnalysis.DataSources.FearGreedIndex.APIURL
		if apiURL == "" {
			// 使用默认API URL
			apiURL = DefaultFearGreedIndexAPI
			logger.Info("📰 [AI情绪分析] 使用默认恐慌贪婪指数API: %s", apiURL)
		}
		
		index, err := sa.dataSourceMgr.FetchFearGreedIndex(apiURL)
		if err != nil {
			logger.Warn("⚠️ 获取恐慌贪婪指数失败: %v", err)
		} else {
			fearGreedIndex = index
			logger.Info("📰 [AI情绪分析] 恐慌贪婪指数: %d (%s)", index.Value, index.Classification)
		}
	}
	
	// 获取Reddit帖子
	var redditPosts []RedditPost
	if sa.cfg.AI.Modules.SentimentAnalysis.DataSources.SocialMedia.Enabled {
		subreddits := sa.cfg.AI.Modules.SentimentAnalysis.DataSources.SocialMedia.Subreddits
		postLimit := sa.cfg.AI.Modules.SentimentAnalysis.DataSources.SocialMedia.PostLimit
		if postLimit <= 0 {
			postLimit = 25 // 默认25条
		}
		
		if len(subreddits) == 0 {
			// 使用默认子版块
			subreddits = []string{"Bitcoin", "ethereum", "CryptoCurrency", "CryptoMarkets"}
			logger.Info("📰 [AI情绪分析] 使用默认Reddit子版块: Bitcoin, ethereum, CryptoCurrency, CryptoMarkets")
		}
		
		posts, err := sa.dataSourceMgr.FetchRedditPosts(subreddits, postLimit)
		if err != nil {
			logger.Warn("⚠️ 获取Reddit帖子失败: %v", err)
		} else {
			redditPosts = posts
			logger.Info("📰 [AI情绪分析] 已获取 %d 条Reddit帖子", len(redditPosts))
		}
	}
	
	// 如果没有任何数据，跳过分析
	if len(newsItems) == 0 && fearGreedIndex == nil && len(redditPosts) == 0 {
		return fmt.Errorf("没有可用的情绪数据")
	}
	
	req := &SentimentAnalysisRequest{
		Symbol:        symbol,
		NewsItems:     newsItems,
		FearGreedIndex: fearGreedIndex,
		RedditPosts:   redditPosts,
		Timestamp:     time.Now(),
	}
	
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	
	response, err := sa.aiService.AnalyzeSentiment(ctx, req)
	if err != nil {
		return fmt.Errorf("AI分析失败: %w", err)
	}
	
	sa.mu.Lock()
	sa.lastAnalysis = response
	sa.lastAnalysisTime = time.Now()
	sa.mu.Unlock()
	
	logger.Info("📰 [AI情绪分析] 情绪评分: %.2f, 趋势: %s", response.SentimentScore, response.Trend)
	
	if len(response.KeyFactors) > 0 {
		logger.Info("📰 [AI情绪分析] 关键因素: %v", response.KeyFactors)
	}
	
	return nil
}

// GetLastAnalysis 获取最后一次分析结果
func (sa *SentimentAnalyzer) GetLastAnalysis() *SentimentAnalysisResponse {
	sa.mu.RLock()
	defer sa.mu.RUnlock()
	return sa.lastAnalysis
}

// GetLastAnalysisTime 获取最后一次分析时间
func (sa *SentimentAnalyzer) GetLastAnalysisTime() time.Time {
	sa.mu.RLock()
	defer sa.mu.RUnlock()
	return sa.lastAnalysisTime
}

