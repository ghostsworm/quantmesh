package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"quantmesh/config"
	"quantmesh/logger"
	"quantmesh/metrics"
	"quantmesh/storage"
)

// NewsItem 新闻条目
type NewsItem struct {
	Title       string    `json:"title"`
	Content     string    `json:"content"`
	Source      string    `json:"source"`
	URL         string    `json:"url"`
	PublishedAt time.Time `json:"published_at"`
	Keywords    []string  `json:"keywords"`
	RiskScore   float64   `json:"risk_score"`   // 0-100，风险评分
	Category    string    `json:"category"`      // 地缘政治、监管、技术、宏观经济等
	ImpactLevel string    `json:"impact_level"` // low, medium, high, critical
}

// PriceScenario 价格场景预测
type PriceScenario struct {
	Direction     string  `json:"direction"`      // "up", "down", "neutral"
	ChangePercent float64 `json:"change_percent"`
	Probability   float64 `json:"probability"` // 0-1
}

// PriceRange 价格区间
type PriceRange struct {
	Min float64 `json:"min"`
	Max float64 `json:"max"`
}

// PricePrediction 价格预测（单个时间窗口）
type PricePrediction struct {
	Timeframe        string          `json:"timeframe"` // "2h", "4h", etc.
	Scenarios        []PriceScenario `json:"scenarios"`
	TargetPriceRange PriceRange      `json:"target_price_range"`
}

// CurrentPriceAnalysis 当前价格分析
type CurrentPriceAnalysis struct {
	CurrentPrice     float64 `json:"current_price"`
	PriceTrend       string  `json:"price_trend"` // "up", "down", "neutral"
	SupportLevel     float64 `json:"support_level"`
	ResistanceLevel  float64 `json:"resistance_level"`
	Change24hPercent float64 `json:"change_24h_percent"`
}

// RiskFactor 风险因素
type RiskFactor struct {
	Factor   string `json:"factor"`
	Severity string `json:"severity"` // "low", "medium", "high", "critical"
	Impact   string `json:"impact"`
}

// NewsRiskAssessment 新闻风险评估结果
type NewsRiskAssessment struct {
	AssetType            string               `json:"asset_type"`            // crypto_btc, commodity_gold
	OverallRiskScore     float64              `json:"overall_risk_score"`    // 综合风险评分 0-100
	CrashProbability     float64              `json:"crash_probability"`     // 大跌概率 0-1
	RecentHighRiskNews   []NewsItem           `json:"recent_high_risk_news"` // 最近的高风险新闻
	RiskFactors          []string             `json:"risk_factors"`          // 风险因素列表（兼容旧字段）
	LastUpdated          time.Time            `json:"last_updated"`
	Recommendation       string               `json:"recommendation"`        // 建议：normal, caution, reduce_position, stop_trading
	AnalysisID           string               `json:"analysis_id"`           // 唯一ID
	PricePredictions     []PricePrediction    `json:"price_predictions"`
	CurrentPriceAnalysis CurrentPriceAnalysis `json:"current_price_analysis"`
	RelatedNews          []NewsItem           `json:"related_news"`
	RiskFactorDetails    []RiskFactor         `json:"risk_factor_details"`
	AnalysisSummary      string               `json:"analysis_summary"`
}

// PriceGetter 获取指定交易对当前价格的函数，用于 Gemini 分析
type PriceGetter func(symbol string) float64

// NewsMonitor 新闻监控器
type NewsMonitor struct {
	cfg              *config.Config
	storage          storage.Storage
	httpClient       *http.Client
	newsCache             []NewsItem
	lastAssessment        *NewsRiskAssessment       // 兼容：最后一次分析（任意资产）
	lastAssessmentByAsset map[string]*NewsRiskAssessment // 按资产类型缓存
	mu                    sync.RWMutex
	ctx              context.Context
	cancel           context.CancelFunc
	isRunning        bool
	analysisLoopDone chan struct{}

	// Gemini 新闻分析
	newsCollector   *NewsCollector
	geminiAnalyzer  *GeminiNewsAnalyzer
	getPrice        PriceGetter

	// 风险关键词库
	highRiskKeywords map[string]float64 // 关键词 -> 权重
	categories       map[string]float64 // 类别 -> 权重
}

// NewNewsMonitor 创建新闻监控器
func NewNewsMonitor(cfg *config.Config, storage storage.Storage) *NewsMonitor {
	ctx, cancel := context.WithCancel(context.Background())

	// 创建支持代理的 HTTP 客户端
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

	nm := &NewsMonitor{
		cfg:                   cfg,
		storage:               storage,
		httpClient:            &http.Client{Timeout: 30 * time.Second, Transport: transport},
		newsCache:             make([]NewsItem, 0),
		lastAssessmentByAsset: make(map[string]*NewsRiskAssessment),
		ctx:                   ctx,
		cancel:                cancel,
	}

	// 初始化风险关键词库
	nm.initRiskKeywords()

	return nm
}

// SetPriceGetter 设置价格获取函数（用于 Gemini 分析）
func (nm *NewsMonitor) SetPriceGetter(getPrice PriceGetter) {
	nm.mu.Lock()
	defer nm.mu.Unlock()
	nm.getPrice = getPrice
}

// initRiskKeywords 初始化风险关键词库
func (nm *NewsMonitor) initRiskKeywords() {
	// 高风险关键词及其权重（0-1）
	nm.highRiskKeywords = map[string]float64{
		// 地缘政治
		"战争": 0.9, "冲突": 0.85, "袭击": 0.9, "爆炸": 0.85, "军事": 0.7,
		"伊朗": 0.8, "以色列": 0.75, "俄罗斯": 0.7, "乌克兰": 0.7,
		"制裁": 0.75, "禁运": 0.8, "封锁": 0.85,
		// 监管政策
		"禁令": 0.9, "禁止": 0.85, "监管": 0.7, "SEC": 0.75, "证监会": 0.75,
		"立法": 0.65, "法案": 0.7, "法规": 0.65,
		// 宏观经济
		"关税": 0.8, "加息": 0.7, "降息": 0.5, "通胀": 0.6, "衰退": 0.85,
		"政府关门": 0.75, "债务危机": 0.8, "违约": 0.9,
		// 交易所/技术
		"黑客": 0.9, "攻击": 0.85, "被盗": 0.9, "交易所": 0.7, "暂停": 0.8,
		"故障": 0.7, "宕机": 0.75,
		// 市场情绪
		"暴跌": 0.85, "崩盘": 0.9, "恐慌": 0.8, "抛售": 0.75,
		"流动性危机": 0.85, "强制平仓": 0.8,
	}

	// 类别权重
	nm.categories = map[string]float64{
		"地缘政治": 0.9,
		"监管政策": 0.85,
		"交易所安全": 0.9,
		"宏观经济": 0.7,
		"市场异常": 0.8,
		"其他": 0.3,
	}
}

// Start 启动新闻监控
func (nm *NewsMonitor) Start() error {
	if !nm.cfg.NewsMonitor.Enabled {
		logger.Info("📰 新闻监控未启用")
		return nil
	}

	if nm.isRunning {
		return fmt.Errorf("新闻监控已在运行")
	}

	nm.isRunning = true

	// 创建并启动 NewsCollector（每5分钟收集新闻）
	nm.newsCollector = NewNewsCollector(nm.cfg)
	nm.newsCollector.Start()

	// 创建 Gemini 分析器
	nm.geminiAnalyzer = NewGeminiNewsAnalyzer(nm.cfg, nm.newsCollector)

	nm.analysisLoopDone = make(chan struct{})
	if nm.cfg.NewsMonitor.UseGeminiSearch {
		// Gemini 模式：每30分钟分析一次
		analysisInterval, err := time.ParseDuration(nm.cfg.NewsMonitor.AnalysisInterval)
		if err != nil {
			logger.Warn("⚠️ 解析分析间隔失败，使用默认30分钟: %v", err)
			analysisInterval = 30 * time.Minute
		}
		logger.Info("📰 启动新闻监控 (Gemini 分析间隔: %s)", analysisInterval)
		go nm.geminiAnalysisLoop(analysisInterval)
	} else {
		// 规则引擎模式：每5分钟检查
		checkInterval, err := time.ParseDuration(nm.cfg.NewsMonitor.CheckInterval)
		if err != nil {
			logger.Warn("⚠️ 解析检查间隔失败，使用默认5分钟: %v", err)
			checkInterval = 5 * time.Minute
		}
		logger.Info("📰 启动新闻监控 (规则引擎间隔: %s)", checkInterval)
		go nm.monitoringLoop(checkInterval)
	}

	return nil
}

// Stop 停止新闻监控
func (nm *NewsMonitor) Stop() {
	nm.cancel()
	if nm.newsCollector != nil {
		nm.newsCollector.Stop()
	}
	// 仅当已启动过监控循环时才等待
	if nm.analysisLoopDone != nil {
		<-nm.analysisLoopDone
	}
	nm.isRunning = false
	logger.Info("📰 新闻监控已停止")
}

// monitoringLoop 监控循环（规则引擎模式）
func (nm *NewsMonitor) monitoringLoop(interval time.Duration) {
	defer func() {
		if nm.analysisLoopDone != nil {
			close(nm.analysisLoopDone)
		}
	}()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// 立即执行一次
	nm.checkNews()

	for {
		select {
		case <-nm.ctx.Done():
			return
		case <-ticker.C:
			nm.checkNews()
		}
	}
}

// geminiAnalysisLoop Gemini 分析循环（每30分钟，多资产）
func (nm *NewsMonitor) geminiAnalysisLoop(interval time.Duration) {
	defer func() {
		if nm.analysisLoopDone != nil {
			close(nm.analysisLoopDone)
		}
	}()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	// 立即执行一次
	nm.runGeminiAnalysisForAllAssets("")

	for {
		select {
		case <-nm.ctx.Done():
			return
		case <-ticker.C:
			nm.runGeminiAnalysisForAllAssets("")
		}
	}
}

// runGeminiAnalysisForAllAssets 对所有启用资产执行分析
func (nm *NewsMonitor) runGeminiAnalysisForAllAssets(focusEvent string) {
	assets := nm.cfg.NewsMonitor.Assets
	if len(assets) == 0 {
		assets = []config.AssetConfig{
			{AssetType: AssetTypeCryptoBTC, Symbol: "BTCUSDT", Enabled: true},
			{AssetType: AssetTypeCommodityGold, Symbol: "PAXGUSDT", Enabled: true},
		}
	}
	for _, a := range assets {
		if !a.Enabled || a.Symbol == "" {
			continue
		}
		nm.runGeminiAnalysis(a.AssetType, a.Symbol, focusEvent)
	}
}

// runGeminiAnalysis 执行一次 Gemini 分析（单资产）
func (nm *NewsMonitor) runGeminiAnalysis(assetType, symbol, focusEvent string) {
	if nm.geminiAnalyzer == nil {
		return
	}
	if nm.geminiAnalyzer.IsAnalyzing() {
		logger.Debug("📰 跳过本次 Gemini 分析：上次分析尚未完成")
		return
	}

	currentPrice := 0.0
	if nm.getPrice != nil {
		currentPrice = nm.getPrice(symbol)
	}
	if currentPrice <= 0 {
		logger.Warn("📰 无法获取 %s 价格，跳过 Gemini 分析", symbol)
		if assetType == AssetTypeCryptoBTC {
			nm.checkNews()
		}
		return
	}

	ctx, cancel := context.WithTimeout(nm.ctx, 15*time.Minute)
	defer cancel()

	assessment, err := nm.geminiAnalyzer.AnalyzeAsset(ctx, assetType, symbol, currentPrice, focusEvent)
	if err != nil {
		logger.Warn("📰 Gemini 分析 %s 失败: %v", symbol, err)
		if assetType == AssetTypeCryptoBTC {
			nm.checkNews()
		}
		return
	}

	nm.mu.Lock()
	nm.lastAssessment = assessment
	if nm.lastAssessmentByAsset == nil {
		nm.lastAssessmentByAsset = make(map[string]*NewsRiskAssessment)
	}
	nm.lastAssessmentByAsset[assetType] = assessment
	nm.mu.Unlock()

	// 更新指标
	pm := metrics.GetPrometheusMetrics()
	pm.SetNewsRiskScore(assessment.OverallRiskScore)
	pm.SetBitcoinCrashProbability(assessment.CrashProbability)
	pm.SetHighRiskNewsCount(len(assessment.RecentHighRiskNews))
	pm.SetNewsRecommendation(assessment.Recommendation)
	pm.SetNewsLastAnalysisTimestamp(assessment.LastUpdated.Unix())
	if nm.newsCollector != nil {
		pm.SetNewsCollectedCount(nm.newsCollector.GetCacheCount())
	}
	for _, pred := range assessment.PricePredictions {
		for _, s := range pred.Scenarios {
			pm.SetNewsPredictionProbability(pred.Timeframe, s.Direction, s.ChangePercent, s.Probability)
		}
	}

	// 保存分析历史
	nm.saveAnalysisHistory(symbol, currentPrice, assessment, focusEvent)

	// 定期清理过期历史
	nm.cleanupOldAnalysisHistory()

	if assessment.OverallRiskScore >= 70 {
		logger.Warn("⚠️ 新闻风险评分较高: %.2f, 大跌概率: %.2f%%, 建议: %s",
			assessment.OverallRiskScore, assessment.CrashProbability*100, assessment.Recommendation)
	}
}

// SymbolToAssetType 交易对到资产类型映射
var SymbolToAssetType = map[string]string{
	"BTCUSDT":  AssetTypeCryptoBTC,
	"PAXGUSDT": AssetTypeCommodityGold,
}

// TriggerAnalysis 手动触发分析（支持指定焦点事件和 assetType）
func (nm *NewsMonitor) TriggerAnalysis(symbol string, focusEvent string) error {
	return nm.TriggerAnalysisWithAsset(symbol, "", focusEvent)
}

// TriggerAnalysisWithAsset 手动触发分析，可指定 assetType
func (nm *NewsMonitor) TriggerAnalysisWithAsset(symbol, assetType, focusEvent string) error {
	if !nm.cfg.NewsMonitor.Enabled {
		return fmt.Errorf("新闻监控未启用")
	}
	if !nm.cfg.NewsMonitor.UseGeminiSearch {
		return fmt.Errorf("Gemini 搜索未启用，无法触发分析")
	}
	if nm.geminiAnalyzer == nil {
		return fmt.Errorf("Gemini 分析器未初始化")
	}
	if nm.geminiAnalyzer.IsAnalyzing() {
		return fmt.Errorf("已有分析任务在执行中")
	}

	if symbol == "" {
		symbol = "BTCUSDT"
	}
	if assetType == "" {
		assetType = SymbolToAssetType[symbol]
		if assetType == "" {
			assetType = AssetTypeCryptoBTC
		}
	}
	currentPrice := 0.0
	if nm.getPrice != nil {
		currentPrice = nm.getPrice(symbol)
	}
	if currentPrice <= 0 {
		return fmt.Errorf("无法获取 %s 当前价格", symbol)
	}

	sym, at := symbol, assetType
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
		defer cancel()

		assessment, err := nm.geminiAnalyzer.AnalyzeAsset(ctx, at, sym, currentPrice, focusEvent)
		if err != nil {
			logger.Warn("📰 手动触发 Gemini 分析失败: %v", err)
			return
		}

		nm.mu.Lock()
		nm.lastAssessment = assessment
		if nm.lastAssessmentByAsset == nil {
			nm.lastAssessmentByAsset = make(map[string]*NewsRiskAssessment)
		}
		nm.lastAssessmentByAsset[at] = assessment
		nm.mu.Unlock()

		pm := metrics.GetPrometheusMetrics()
		pm.SetNewsRiskScore(assessment.OverallRiskScore)
		pm.SetBitcoinCrashProbability(assessment.CrashProbability)
		pm.SetHighRiskNewsCount(len(assessment.RecentHighRiskNews))

		nm.saveAnalysisHistory(sym, currentPrice, assessment, focusEvent)

		logger.Info("📰 手动触发分析完成 %s: 建议=%s", sym, assessment.Recommendation)
	}()
	return nil
}

// saveAnalysisHistory 保存分析历史到存储
func (nm *NewsMonitor) saveAnalysisHistory(symbol string, currentPrice float64, assessment *NewsRiskAssessment, focusEvent string) {
	if nm.storage == nil {
		return
	}
	assessmentJSON, err := json.Marshal(assessment)
	if err != nil {
		logger.Warn("📰 序列化分析结果失败: %v", err)
		return
	}
	recentSummary := ""
	if nm.newsCollector != nil {
		assetType := ""
		if assessment != nil {
			assetType = assessment.AssetType
		}
		recentSummary = nm.newsCollector.GetNewsSummaryText(assetType)
	}
	h := &storage.NewsAnalysisHistory{
		AnalysisTime:      time.Now(),
		Symbol:            symbol,
		CurrentPrice:      currentPrice,
		Assessment:        string(assessmentJSON),
		RecentNewsSummary: recentSummary,
		GeminiPrompt:      "", // 可选：可从 GeminiAnalyzer 扩展获取
		GeminiResponse:    "",
		CreatedAt:         time.Now(),
	}
	if focusEvent != "" {
		h.GeminiPrompt = "焦点事件: " + focusEvent
	}
	if err := nm.storage.SaveNewsAnalysisHistory(h); err != nil {
		logger.Warn("📰 保存分析历史失败: %v", err)
		return
	}
	// 创建预测验证记录
	if h.ID > 0 && assessment != nil && len(assessment.PricePredictions) > 0 {
		assetType := assessment.AssetType
		if assetType == "" {
			assetType = AssetTypeCryptoBTC
		}
		CreateVerificationRecords(nm.storage, h.ID, assetType, symbol, h.AnalysisTime, currentPrice, assessment.PricePredictions)
	}
}

// cleanupOldAnalysisHistory 清理过期的分析历史
func (nm *NewsMonitor) cleanupOldAnalysisHistory() {
	if nm.storage == nil {
		return
	}
	days := nm.cfg.NewsMonitor.HistoryRetentionDays
	if days <= 0 {
		days = 30
	}
	cutoff := time.Now().AddDate(0, 0, -days)
	if err := nm.storage.CleanupNewsAnalysisHistory(cutoff); err != nil {
		logger.Warn("📰 清理分析历史失败: %v", err)
	}
}

// GetCollectedNews 获取当前收集的新闻列表
func (nm *NewsMonitor) GetCollectedNews() []NewsItem {
	if nm.newsCollector == nil {
		return nil
	}
	return nm.newsCollector.GetRecentNews(2)
}

// IsAnalyzing 是否正在执行 Gemini 分析
func (nm *NewsMonitor) IsAnalyzing() bool {
	if nm.geminiAnalyzer == nil {
		return false
	}
	return nm.geminiAnalyzer.IsAnalyzing()
}

// checkNews 检查新闻
func (nm *NewsMonitor) checkNews() {
	// 从配置的新闻源获取新闻
	newsItems := nm.fetchNewsFromSources()

	if len(newsItems) == 0 {
		return
	}

	// 分析新闻风险
	for i := range newsItems {
		newsItems[i].RiskScore = nm.calculateRiskScore(newsItems[i])
		newsItems[i].Category = nm.categorizeNews(newsItems[i])
		newsItems[i].ImpactLevel = nm.determineImpactLevel(newsItems[i].RiskScore)
	}

	// 更新缓存
	nm.mu.Lock()
	nm.newsCache = append(nm.newsCache, newsItems...)
	// 只保留最近24小时的新闻
	cutoffTime := time.Now().Add(-24 * time.Hour)
	filtered := make([]NewsItem, 0)
	for _, item := range nm.newsCache {
		if item.PublishedAt.After(cutoffTime) {
			filtered = append(filtered, item)
		}
	}
	nm.newsCache = filtered
	nm.mu.Unlock()

	// 计算综合风险评估
	assessment := nm.assessRisk()

	// 更新评估结果
	nm.mu.Lock()
	nm.lastAssessment = assessment
	nm.mu.Unlock()

	// 记录指标
	pm := metrics.GetPrometheusMetrics()
	pm.SetNewsRiskScore(assessment.OverallRiskScore)
	pm.SetBitcoinCrashProbability(assessment.CrashProbability)
	pm.SetHighRiskNewsCount(len(assessment.RecentHighRiskNews))

	// 如果风险较高，记录日志
	if assessment.OverallRiskScore >= 70 {
		logger.Warn("⚠️ 新闻风险评分较高: %.2f, 大跌概率: %.2f%%, 建议: %s",
			assessment.OverallRiskScore, assessment.CrashProbability*100, assessment.Recommendation)
		logger.Warn("风险因素: %s", strings.Join(assessment.RiskFactors, ", "))
	}

	// 保存高风险新闻到存储
	if nm.storage != nil {
		for _, item := range newsItems {
			if item.RiskScore >= 70 {
				go nm.saveNewsItem(item)
			}
		}
	}
}

// fetchNewsFromSources 从新闻源获取新闻
func (nm *NewsMonitor) fetchNewsFromSources() []NewsItem {
	var allNews []NewsItem

	// 使用配置的新闻源
	for _, source := range nm.cfg.NewsMonitor.Sources {
		news := nm.fetchFromSource(source)
		allNews = append(allNews, news...)
	}

	return allNews
}

// fetchFromSource 从单个新闻源获取新闻
func (nm *NewsMonitor) fetchFromSource(source string) []NewsItem {
	var newsItems []NewsItem
	var err error

	switch source {
	case "newsapi":
		if nm.cfg.NewsMonitor.NewsAPIKey != "" {
			newsItems, err = nm.fetchFromNewsAPI(nm.cfg.NewsMonitor.NewsAPIKey, nm.cfg.NewsMonitor.Keywords)
			if err != nil {
				logger.Warn("⚠️ 从NewsAPI获取新闻失败: %v", err)
			}
		} else {
			logger.Debug("📰 NewsAPI密钥未配置，跳过NewsAPI源")
		}
	case "rss":
		// RSS源处理
		for _, feedURL := range nm.cfg.NewsMonitor.RSSFeeds {
			items, err := nm.fetchFromRSS(feedURL)
			if err != nil {
				logger.Warn("⚠️ 从RSS获取新闻失败 (%s): %v", feedURL, err)
				continue
			}
			newsItems = append(newsItems, items...)
		}
	default:
		logger.Debug("📰 未知的新闻源: %s", source)
	}

	return newsItems
}

// calculateRiskScore 计算新闻风险评分
func (nm *NewsMonitor) calculateRiskScore(news NewsItem) float64 {
	score := 0.0
	text := strings.ToLower(news.Title + " " + news.Content)

	// 检查关键词
	matchedKeywords := []string{}
	for keyword, weight := range nm.highRiskKeywords {
		if strings.Contains(text, strings.ToLower(keyword)) {
			score += weight * 20 // 每个关键词贡献最多20分
			matchedKeywords = append(matchedKeywords, keyword)
		}
	}

	// 关键词密度（匹配的关键词数量）
	keywordDensity := float64(len(matchedKeywords)) / 10.0
	if keywordDensity > 1.0 {
		keywordDensity = 1.0
	}
	score += keywordDensity * 30 // 最多30分

	// 时间衰减（越新的新闻权重越高）
	ageHours := time.Since(news.PublishedAt).Hours()
	timeDecay := 1.0
	if ageHours > 24 {
		timeDecay = 1.0 - (ageHours-24)/168.0 // 一周后衰减
		if timeDecay < 0.3 {
			timeDecay = 0.3
		}
	}
	score *= timeDecay

	// 来源权重
	sourceWeight := 1.0
	if strings.Contains(strings.ToLower(news.Source), "reuters") ||
		strings.Contains(strings.ToLower(news.Source), "bloomberg") ||
		strings.Contains(strings.ToLower(news.Source), "coindesk") {
		sourceWeight = 1.2
	}
	score *= sourceWeight

	// 限制在0-100之间
	if score > 100 {
		score = 100
	}
	if score < 0 {
		score = 0
	}

	return score
}

// categorizeNews 分类新闻
func (nm *NewsMonitor) categorizeNews(news NewsItem) string {
	text := strings.ToLower(news.Title + " " + news.Content)

	// 地缘政治
	if containsAny(text, []string{"战争", "冲突", "袭击", "爆炸", "伊朗", "以色列", "俄罗斯", "乌克兰", "制裁", "禁运"}) {
		return "地缘政治"
	}

	// 监管政策
	if containsAny(text, []string{"监管", "sec", "证监会", "禁令", "禁止", "立法", "法案", "法规"}) {
		return "监管政策"
	}

	// 交易所安全
	if containsAny(text, []string{"黑客", "攻击", "被盗", "交易所", "暂停", "故障", "宕机"}) {
		return "交易所安全"
	}

	// 宏观经济
	if containsAny(text, []string{"关税", "加息", "降息", "通胀", "衰退", "政府关门", "债务危机"}) {
		return "宏观经济"
	}

	// 市场异常
	if containsAny(text, []string{"暴跌", "崩盘", "恐慌", "抛售", "流动性危机", "强制平仓"}) {
		return "市场异常"
	}

	return "其他"
}

// determineImpactLevel 确定影响级别
func (nm *NewsMonitor) determineImpactLevel(riskScore float64) string {
	if riskScore >= 80 {
		return "critical"
	} else if riskScore >= 60 {
		return "high"
	} else if riskScore >= 40 {
		return "medium"
	}
	return "low"
}

// assessRisk 综合风险评估
func (nm *NewsMonitor) assessRisk() *NewsRiskAssessment {
	nm.mu.RLock()
	recentNews := make([]NewsItem, len(nm.newsCache))
	copy(recentNews, nm.newsCache)
	nm.mu.RUnlock()

	// 只考虑最近24小时的新闻
	cutoffTime := time.Now().Add(-24 * time.Hour)
	var relevantNews []NewsItem
	for _, item := range recentNews {
		if item.PublishedAt.After(cutoffTime) {
			relevantNews = append(relevantNews, item)
		}
	}

	if len(relevantNews) == 0 {
		return &NewsRiskAssessment{
			OverallRiskScore:   0,
			CrashProbability:   0,
			RecentHighRiskNews: []NewsItem{},
			RiskFactors:        []string{},
			LastUpdated:        time.Now(),
			Recommendation:     "normal",
		}
	}

	// 计算综合风险评分
	overallScore := 0.0
	var highRiskNews []NewsItem
	riskFactors := make(map[string]bool)

	for _, item := range relevantNews {
		overallScore += item.RiskScore
		if item.RiskScore >= 70 {
			highRiskNews = append(highRiskNews, item)
			riskFactors[item.Category] = true
		}
	}

	// 平均风险评分
	if len(relevantNews) > 0 {
		overallScore = overallScore / float64(len(relevantNews))
	}

	// 高风险新闻数量加成
	highRiskCount := len(highRiskNews)
	if highRiskCount > 0 {
		overallScore += float64(highRiskCount) * 5 // 每条高风险新闻额外加5分
	}

	// 限制在0-100
	if overallScore > 100 {
		overallScore = 100
	}

	// 计算大跌概率（基于历史数据和风险评分）
	crashProbability := nm.calculateCrashProbability(overallScore, highRiskCount)

	// 生成建议
	recommendation := nm.generateRecommendation(overallScore, crashProbability)

	// 构建风险因素列表
	var factors []string
	for factor := range riskFactors {
		factors = append(factors, factor)
	}

	return &NewsRiskAssessment{
		OverallRiskScore:   overallScore,
		CrashProbability:   crashProbability,
		RecentHighRiskNews: highRiskNews,
		RiskFactors:        factors,
		LastUpdated:         time.Now(),
		Recommendation:     recommendation,
	}
}

// calculateCrashProbability 计算大跌概率
func (nm *NewsMonitor) calculateCrashProbability(riskScore float64, highRiskCount int) float64 {
	// 基于风险评分的基础概率
	baseProbability := riskScore / 100.0

	// 高风险新闻数量加成
	countMultiplier := 1.0 + float64(highRiskCount)*0.1
	if countMultiplier > 2.0 {
		countMultiplier = 2.0
	}

	probability := baseProbability * countMultiplier

	// 限制在0-1之间
	if probability > 1.0 {
		probability = 1.0
	}
	if probability < 0 {
		probability = 0
	}

	return probability
}

// generateRecommendation 生成建议
func (nm *NewsMonitor) generateRecommendation(riskScore float64, crashProbability float64) string {
	if riskScore >= 80 || crashProbability >= 0.7 {
		return "stop_trading"
	} else if riskScore >= 60 || crashProbability >= 0.5 {
		return "reduce_position"
	} else if riskScore >= 40 || crashProbability >= 0.3 {
		return "caution"
	}
	return "normal"
}

// GetRiskAssessment 获取最新风险评估（兼容：返回最后一次任意资产的分析）
func (nm *NewsMonitor) GetRiskAssessment() *NewsRiskAssessment {
	return nm.GetRiskAssessmentBySymbol("")
}

// GetRiskAssessmentBySymbol 按交易对获取风险评估（PAXGUSDT→黄金，BTCUSDT→BTC）
func (nm *NewsMonitor) GetRiskAssessmentBySymbol(symbol string) *NewsRiskAssessment {
	nm.mu.RLock()
	defer nm.mu.RUnlock()

	assetType := SymbolToAssetType[symbol]
	if assetType != "" && nm.lastAssessmentByAsset != nil {
		if a := nm.lastAssessmentByAsset[assetType]; a != nil {
			r := *a
			return &r
		}
	}
	if nm.lastAssessment == nil {
		return &NewsRiskAssessment{
			OverallRiskScore:   0,
			CrashProbability:   0,
			RecentHighRiskNews: []NewsItem{},
			RiskFactors:        []string{},
			LastUpdated:        time.Now(),
			Recommendation:     "normal",
		}
	}

	// 返回副本
	assessment := *nm.lastAssessment
	return &assessment
}

// IsHighRisk 判断当前是否处于高风险状态
func (nm *NewsMonitor) IsHighRisk() bool {
	assessment := nm.GetRiskAssessment()
	return assessment.OverallRiskScore >= 70 || assessment.CrashProbability >= 0.5
}

// saveNewsItem 保存新闻条目到存储
func (nm *NewsMonitor) saveNewsItem(item NewsItem) {
	// 这里可以保存到数据库
	// 目前先记录日志
	logger.Info("📰 保存高风险新闻: %s (风险评分: %.2f)", item.Title, item.RiskScore)
}

// containsAny 检查文本是否包含任意关键词
func containsAny(text string, keywords []string) bool {
	for _, keyword := range keywords {
		if strings.Contains(text, strings.ToLower(keyword)) {
			return true
		}
	}
	return false
}

// 辅助函数：从RSS获取新闻（示例实现）
func (nm *NewsMonitor) fetchFromRSS(rssURL string) ([]NewsItem, error) {
	req, err := http.NewRequestWithContext(nm.ctx, "GET", rssURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := nm.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// 解析RSS XML（需要实现RSS解析器）
	// 这里简化处理，实际需要使用XML解析库
	_ = body

	return []NewsItem{}, nil
}

// 辅助函数：从NewsAPI获取新闻（示例实现）
func (nm *NewsMonitor) fetchFromNewsAPI(apiKey string, keywords []string) ([]NewsItem, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("NewsAPI key not configured")
	}

	// 构建请求URL
	baseURL := "https://newsapi.org/v2/everything"
	params := url.Values{}
	params.Set("apiKey", apiKey)
	params.Set("q", strings.Join(keywords, " OR "))
	params.Set("language", "zh,en")
	params.Set("sortBy", "publishedAt")
	params.Set("pageSize", "20")

	reqURL := baseURL + "?" + params.Encode()
	req, err := http.NewRequestWithContext(nm.ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := nm.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("NewsAPI error: HTTP %d, %s", resp.StatusCode, string(body))
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

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	if result.Status != "ok" {
		return nil, fmt.Errorf("NewsAPI returned status: %s", result.Status)
	}

	var newsItems []NewsItem
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
