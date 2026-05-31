package web

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"quantmesh/ai"
	"quantmesh/config"
	"quantmesh/logger"

	"github.com/gin-gonic/gin"
)

const (
	marketIntelDigestCacheTTL = 15 * time.Minute
	maxDigestHeadlines        = 40
)

type marketIntelDigestCacheEntry struct {
	text      string
	expiresAt time.Time
	lang      string
	fp        string
}

var (
	marketIntelDigestMu   sync.Mutex
	marketIntelDigestLast *marketIntelDigestCacheEntry
)

func digestFingerprint(headlines []string) string {
	if len(headlines) == 0 {
		return ""
	}
	n := len(headlines)
	if n > 12 {
		n = 12
	}
	return strings.Join(headlines[:n], "|")
}

func collectRSSHeadlinesForDigest(limit int) ([]string, error) {
	if dataSourceProvider == nil {
		return nil, fmt.Errorf("data source not initialized")
	}
	feeds, err := dataSourceProvider.GetRSSFeeds()
	if err != nil {
		return nil, err
	}
	var lines []string
	for _, feed := range feeds {
		for _, item := range feed.Items {
			if len(lines) >= limit {
				return lines, nil
			}
			t := strings.TrimSpace(item.Title)
			if t == "" {
				continue
			}
			src := strings.TrimSpace(feed.Title)
			if src == "" {
				src = "RSS"
			}
			lines = append(lines, fmt.Sprintf("[%s] %s", src, t))
		}
	}
	return lines, nil
}

func buildMarketDigestPrompt(headlines []string, lang string) string {
	b := strings.Builder{}
	if lang == "en" {
		b.WriteString("You are a crypto market analyst. Based ONLY on the following RSS headlines (may include duplicates across sources), write a concise summary of 80–130 words in English covering: overall sentiment, main narratives, and implied short-term outlook for crypto. Do not invent facts not present in the headlines. If there are too few headlines, say the sample is thin.\n\nHeadlines:\n")
	} else {
		b.WriteString("你是加密市场分析师。仅根据以下 RSS 新闻标题（不同源可能有重复），用中文写一段约 120–200 字的简报：整体情绪、主要叙事、对短期加密/BTC 走势的隐含影响。不要编造标题中未出现的具体事实。若标题过少请说明样本不足。\n\n标题列表：\n")
	}
	for _, h := range headlines {
		b.WriteString(h)
		b.WriteByte('\n')
	}
	return b.String()
}

// getMarketIntelNewsDigest GET /api/market-intelligence/news-digest?lang=zh|en
func getMarketIntelNewsDigest(c *gin.Context) {
	cfg := GetConfig()
	if cfg == nil {
		respondError(c, http.StatusInternalServerError, "error.config_load_failed")
		return
	}
	lang := strings.TrimSpace(c.DefaultQuery("lang", "zh"))
	if lang != "en" {
		lang = "zh"
	}

	upstream := config.ResolveGlobalAI(cfg)
	if upstream.APIKey == "" {
		respondError(c, http.StatusBadRequest, "error.ai_api_key_not_configured")
		return
	}

	headlines, err := collectRSSHeadlinesForDigest(maxDigestHeadlines)
	if err != nil {
		logger.Warn("market digest: rss: %v", err)
		respondError(c, http.StatusInternalServerError, "error.market_intel_digest_failed", err)
		return
	}
	if len(headlines) == 0 {
		respondError(c, http.StatusBadRequest, "error.market_intel_no_headlines")
		return
	}
	fp := digestFingerprint(headlines)

	marketIntelDigestMu.Lock()
	if marketIntelDigestLast != nil &&
		time.Now().Before(marketIntelDigestLast.expiresAt) &&
		marketIntelDigestLast.lang == lang &&
		marketIntelDigestLast.fp == fp {
		text := marketIntelDigestLast.text
		marketIntelDigestMu.Unlock()
		c.JSON(http.StatusOK, gin.H{"digest": text, "cached": true})
		return
	}
	marketIntelDigestMu.Unlock()

	prompt := buildMarketDigestPrompt(headlines, lang)
	ctx, cancel := context.WithTimeout(c.Request.Context(), 8*time.Minute)
	defer cancel()

	client := ai.NewClientFromUpstream(upstream)
	text, err := client.GenerateContent(ctx, prompt, map[string]interface{}{})
	if err != nil {
		logger.Warn("market digest: ai: %v", err)
		respondError(c, http.StatusInternalServerError, "error.market_intel_digest_failed", err)
		return
	}
	text = strings.TrimSpace(text)
	if text == "" {
		respondError(c, http.StatusInternalServerError, "error.market_intel_digest_failed", fmt.Errorf("empty model output"))
		return
	}

	marketIntelDigestMu.Lock()
	marketIntelDigestLast = &marketIntelDigestCacheEntry{
		text:      text,
		expiresAt: time.Now().Add(marketIntelDigestCacheTTL),
		lang:      lang,
		fp:        fp,
	}
	marketIntelDigestMu.Unlock()

	c.JSON(http.StatusOK, gin.H{"digest": text, "cached": false})
}
