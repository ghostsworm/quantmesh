package web

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"quantmesh/config"
	"quantmesh/monitor"
)

// NewsMonitorProvider 新闻监控提供者接口
type NewsMonitorProvider interface {
	GetRiskAssessment() *monitor.NewsRiskAssessment
	GetRiskAssessmentBySymbol(symbol string) *monitor.NewsRiskAssessment
	TriggerAnalysis(symbol string, focusEvent string) error
	TriggerAnalysisWithAsset(symbol, assetType, focusEvent string) error
	GetCollectedNews() []monitor.NewsItem
	IsAnalyzing() bool
}

var newsMonitorProvider NewsMonitorProvider

// SetNewsMonitorProvider 设置新闻监控提供者
func SetNewsMonitorProvider(provider NewsMonitorProvider) {
	newsMonitorProvider = provider
}

// getNewsAnalysis 获取最新新闻分析结果
// GET /api/news/analysis
// GET /api/news/analysis/:asset_type 按资产类型获取（crypto_btc, commodity_gold）
func getNewsAnalysis(c *gin.Context) {
	if newsMonitorProvider == nil {
		c.JSON(http.StatusOK, gin.H{
			"assessment":   nil,
			"is_analyzing": false,
			"last_updated": nil,
		})
		return
	}

	assetType := c.Query("asset_type")
	var assessment *monitor.NewsRiskAssessment
	if assetType != "" {
		symbol := "BTCUSDT"
		if assetType == "commodity_gold" {
			symbol = "PAXGUSDT"
		}
		assessment = newsMonitorProvider.GetRiskAssessmentBySymbol(symbol)
	} else {
		assessment = newsMonitorProvider.GetRiskAssessment()
	}
	isAnalyzing := newsMonitorProvider.IsAnalyzing()

	resp := gin.H{
		"assessment":   assessment,
		"is_analyzing": isAnalyzing,
	}
	if assessment != nil {
		resp["last_updated"] = assessment.LastUpdated
	}

	c.JSON(http.StatusOK, resp)
}

// getNewsPredictions 获取价格预测概率
// GET /api/news/predictions
func getNewsPredictions(c *gin.Context) {
	if newsMonitorProvider == nil {
		c.JSON(http.StatusOK, gin.H{"predictions": []interface{}{}})
		return
	}

	assessment := newsMonitorProvider.GetRiskAssessment()
	if assessment == nil {
		c.JSON(http.StatusOK, gin.H{"predictions": []interface{}{}})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"price_predictions":     assessment.PricePredictions,
		"current_price_analysis": assessment.CurrentPriceAnalysis,
		"recommendation":        assessment.Recommendation,
		"last_updated":          assessment.LastUpdated,
	})
}

// triggerNewsAnalyzeRequest 手动触发分析请求体
type triggerNewsAnalyzeRequest struct {
	Symbol     string `json:"symbol"`
	AssetType  string `json:"asset_type"` // crypto_btc, commodity_gold
	FocusEvent string `json:"focus_event"`
}

// postNewsAnalyze 手动触发新闻分析
// POST /api/news/analyze
func postNewsAnalyze(c *gin.Context) {
	if newsMonitorProvider == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "新闻监控未初始化"})
		return
	}

	var req triggerNewsAnalyzeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		// 允许空 body
		req.Symbol = "BTCUSDT"
		req.FocusEvent = ""
	}

	if req.Symbol == "" {
		req.Symbol = "BTCUSDT"
	}

	var err error
	if prov, ok := newsMonitorProvider.(interface {
		TriggerAnalysisWithAsset(symbol, assetType, focusEvent string) error
	}); ok && req.AssetType != "" {
		err = prov.TriggerAnalysisWithAsset(req.Symbol, req.AssetType, req.FocusEvent)
	} else {
		err = newsMonitorProvider.TriggerAnalysis(req.Symbol, req.FocusEvent)
	}
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success":     true,
		"message":     "分析任务已提交",
		"symbol":      req.Symbol,
		"asset_type":  req.AssetType,
		"focus_event": req.FocusEvent,
	})
}

// getNewsCollected 获取当前收集的新闻列表
// GET /api/news/collected
func getNewsCollected(c *gin.Context) {
	if newsMonitorProvider == nil {
		c.JSON(http.StatusOK, gin.H{"news": []interface{}{}})
		return
	}

	news := newsMonitorProvider.GetCollectedNews()
	c.JSON(http.StatusOK, gin.H{"news": news})
}

// getNewsKeywords 获取当前关键词列表
// GET /api/news/keywords
func getNewsKeywords(c *gin.Context) {
	if configManager == nil {
		c.JSON(http.StatusOK, gin.H{"keywords": config.DefaultNewsKeywords()})
		return
	}

	cfg, err := configManager.GetConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "获取配置失败"})
		return
	}

	keywords := cfg.NewsMonitor.Keywords
	if len(keywords) == 0 {
		keywords = config.DefaultNewsKeywords()
	}

	c.JSON(http.StatusOK, gin.H{"keywords": keywords})
}

// putNewsKeywordsRequest 更新关键词请求体
type putNewsKeywordsRequest struct {
	Keywords []string `json:"keywords"`
}

// putNewsKeywords 更新关键词列表
// PUT /api/news/keywords
func putNewsKeywords(c *gin.Context) {
	if configManager == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "配置管理器未初始化"})
		return
	}

	var req putNewsKeywordsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "无效的请求体"})
		return
	}

	if len(req.Keywords) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "关键词列表不能为空"})
		return
	}

	cfg, err := configManager.GetConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "获取配置失败"})
		return
	}

	cfg.NewsMonitor.Keywords = req.Keywords
	if err := configManager.UpdateConfig(cfg); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "keywords": req.Keywords})
}

// getNewsHistory 查询新闻分析历史
// GET /api/news/history?symbol=&start_time=&end_time=&limit=20&offset=0
func getNewsHistory(c *gin.Context) {
	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "存储服务未就绪"})
		return
	}

	st := storageProv.GetStorage()
	if st == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "存储服务未就绪"})
		return
	}

	symbol := c.Query("symbol")
	startStr := c.DefaultQuery("start_time", "")
	endStr := c.DefaultQuery("end_time", "")
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))

	if limit <= 0 || limit > 100 {
		limit = 20
	}

	// 默认最近 7 天
	now := time.Now()
	startTime := now.AddDate(0, 0, -7)
	endTime := now

	if startStr != "" {
		if t, err := time.Parse(time.RFC3339, startStr); err == nil {
			startTime = t
		}
	}
	if endStr != "" {
		if t, err := time.Parse(time.RFC3339, endStr); err == nil {
			endTime = t
		}
	}

	list, total, err := st.QueryNewsAnalysisHistory(symbol, startTime, endTime, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// 简化列表项，不包含完整的 assessment JSON
	items := make([]gin.H, 0, len(list))
	for _, h := range list {
		rec := ""
		if h.Assessment != "" {
			var assessment map[string]interface{}
			if json.Unmarshal([]byte(h.Assessment), &assessment) == nil {
				rec = extractRecommendation(assessment)
			}
		}
		items = append(items, gin.H{
			"id":             h.ID,
			"analysis_time":  h.AnalysisTime,
			"symbol":         h.Symbol,
			"current_price":  h.CurrentPrice,
			"recommendation": rec,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"total": total,
		"items": items,
	})
}

func extractRecommendation(assessment map[string]interface{}) string {
	if assessment == nil {
		return ""
	}
	if r, ok := assessment["recommendation"].(string); ok {
		return r
	}
	return ""
}

// getNewsHistoryByID 获取指定历史记录详情
// GET /api/news/history/:id
func getNewsHistoryByID(c *gin.Context) {
	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "存储服务未就绪"})
		return
	}

	st := storageProv.GetStorage()
	if st == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "存储服务未就绪"})
		return
	}

	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的 ID"})
		return
	}

	h, err := st.GetNewsAnalysisHistoryByID(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if h == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "记录不存在"})
		return
	}

	var assessment map[string]interface{}
	if h.Assessment != "" {
		_ = json.Unmarshal([]byte(h.Assessment), &assessment)
	}

	c.JSON(http.StatusOK, gin.H{
		"id":                  h.ID,
		"analysis_time":       h.AnalysisTime,
		"symbol":              h.Symbol,
		"current_price":       h.CurrentPrice,
		"assessment":          assessment,
		"recent_news_summary": h.RecentNewsSummary,
		"gemini_prompt":       h.GeminiPrompt,
		"gemini_response":     h.GeminiResponse,
		"created_at":          h.CreatedAt,
	})
}

// getPredictionsAccuracy 获取预测准确率统计
// GET /api/predictions/accuracy?asset_type=&since=7d
func getPredictionsAccuracy(c *gin.Context) {
	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "存储服务未就绪"})
		return
	}
	st := storageProv.GetStorage()
	if st == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "存储服务未就绪"})
		return
	}

	assetType := c.Query("asset_type")
	sinceDays, _ := strconv.Atoi(c.DefaultQuery("since_days", "7"))
	if sinceDays <= 0 {
		sinceDays = 7
	}
	since := time.Now().AddDate(0, 0, -sinceDays)

	total, correct, err := st.GetPredictionAccuracyStats(assetType, since)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	accuracy := 0.0
	if total > 0 {
		accuracy = float64(correct) / float64(total) * 100
	}
	c.JSON(http.StatusOK, gin.H{
		"total":     total,
		"correct":   correct,
		"accuracy":  accuracy,
		"since":     since,
		"asset_type": assetType,
	})
}

// getPredictionsHistory 获取预测验证历史
// GET /api/predictions/history?asset_type=&symbol=&start_time=&end_time=&limit=20&offset=0
func getPredictionsHistory(c *gin.Context) {
	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "存储服务未就绪"})
		return
	}
	st := storageProv.GetStorage()
	if st == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "存储服务未就绪"})
		return
	}

	assetType := c.Query("asset_type")
	symbol := c.Query("symbol")
	startStr := c.DefaultQuery("start_time", "")
	endStr := c.DefaultQuery("end_time", "")
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if limit <= 0 || limit > 100 {
		limit = 20
	}

	now := time.Now()
	startTime := now.AddDate(0, 0, -7)
	endTime := now
	if startStr != "" {
		if t, err := time.Parse(time.RFC3339, startStr); err == nil {
			startTime = t
		}
	}
	if endStr != "" {
		if t, err := time.Parse(time.RFC3339, endStr); err == nil {
			endTime = t
		}
	}

	list, total, err := st.QueryPredictionVerifications(assetType, symbol, startTime, endTime, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	items := make([]gin.H, 0, len(list))
	for _, v := range list {
		items = append(items, gin.H{
			"id":                    v.ID,
			"analysis_id":           v.AnalysisID,
			"asset_type":            v.AssetType,
			"symbol":                v.Symbol,
			"prediction_time":       v.PredictionTime,
			"timeframe":             v.Timeframe,
			"predicted_direction":   v.PredictedDirection,
			"actual_direction":      v.ActualDirection,
			"is_correct":            v.IsCorrect,
			"status":                v.Status,
			"verified_at":           v.VerifiedAt,
		})
	}
	c.JSON(http.StatusOK, gin.H{"total": total, "items": items})
}
