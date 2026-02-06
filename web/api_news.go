package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"quantmesh/config"
	"quantmesh/logger"
	"quantmesh/monitor"
)

// NewsMonitorProvider 新聞監控提供者接口
type NewsMonitorProvider interface {
	GetRiskAssessment() *monitor.NewsRiskAssessment
	GetRiskAssessmentBySymbol(symbol string) *monitor.NewsRiskAssessment
	TriggerAnalysis(symbol string, focusEvent string) error
	TriggerAnalysisWithAsset(symbol, assetType, focusEvent string) error
	GetCollectedNews() []monitor.NewsItem
	IsAnalyzing() bool
}

var newsMonitorProvider NewsMonitorProvider

// SetNewsMonitorProvider 設置新聞監控提供者
func SetNewsMonitorProvider(provider NewsMonitorProvider) {
	newsMonitorProvider = provider
}

// getNewsAnalysis 獲取最新新聞分析結果
// GET /api/news/analysis
// GET /api/news/analysis/:asset_type 按资產類型獲取（crypto_btc, commodity_gold）
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

// getNewsPredictions 獲取價格預测概率
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

// postNewsAnalyze 手动触发新聞分析
// POST /api/news/analyze
func postNewsAnalyze(c *gin.Context) {
	if newsMonitorProvider == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"success": false, "message": "新聞監控未初始化"})
		return
	}

	var req triggerNewsAnalyzeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		// 允許空 body
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
		"message":     "分析任務已提交",
		"symbol":      req.Symbol,
		"asset_type":  req.AssetType,
		"focus_event": req.FocusEvent,
	})
}

// getNewsCollected 獲取當前收集的新闻列表
// GET /api/news/collected
func getNewsCollected(c *gin.Context) {
	if newsMonitorProvider == nil {
		c.JSON(http.StatusOK, gin.H{"news": []interface{}{}})
		return
	}

	news := newsMonitorProvider.GetCollectedNews()
	c.JSON(http.StatusOK, gin.H{"news": news})
}

// getNewsKeywords 獲取當前关键词列表
// GET /api/news/keywords
func getNewsKeywords(c *gin.Context) {
	if configManager == nil {
		c.JSON(http.StatusOK, gin.H{"keywords": config.DefaultNewsKeywords()})
		return
	}

	cfg, err := configManager.GetConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "獲取配置失败"})
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
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "無效的请求体"})
		return
	}

	if len(req.Keywords) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"success": false, "message": "关键词列表不能為空"})
		return
	}

	cfg, err := configManager.GetConfig()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": "獲取配置失败"})
		return
	}

	cfg.NewsMonitor.Keywords = req.Keywords
	if err := configManager.UpdateConfig(cfg); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"success": false, "message": err.Error()})
		return
	}

	// 保存到历史
	if configHistoryMgr != nil {
		currentContent, err := os.ReadFile(configManager.GetConfigPath())
		if err == nil {
			description := fmt.Sprintf("通過 Web UI 更新新聞監控關鍵詞: %d 個關鍵詞", len(req.Keywords))
			_, _ = configHistoryMgr.SaveHistory(string(currentContent), description, "web")
		}
	}

	c.JSON(http.StatusOK, gin.H{"success": true, "keywords": req.Keywords})
}

// getNewsHistory 查詢新聞分析历史
// GET /api/news/history?symbol=&start_time=&end_time=&limit=20&offset=0
func getNewsHistory(c *gin.Context) {
	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "存儲服務未就绪"})
		return
	}

	st := storageProv.GetStorage()
	if st == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "存儲服務未就绪"})
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
		} else if t, err := time.Parse("2006-01-02", startStr); err == nil {
			startTime = t
		}
	}
	if endStr != "" {
		if t, err := time.Parse(time.RFC3339, endStr); err == nil {
			endTime = t
		} else if t, err := time.Parse("2006-01-02", endStr); err == nil {
			endTime = t.Add(23*time.Hour + 59*time.Minute + 59*time.Second)
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
		var riskScore float64
		var crashProb float64
		if h.Assessment != "" {
			var assessment map[string]interface{}
			if json.Unmarshal([]byte(h.Assessment), &assessment) == nil {
				rec = extractRecommendation(assessment)
				if score, ok := assessment["overall_risk_score"].(float64); ok {
					riskScore = score
				}
				if prob, ok := assessment["crash_probability"].(float64); ok {
					crashProb = prob
				}
			}
		}
		items = append(items, gin.H{
			"id":                 h.ID,
			"analysis_time":      h.AnalysisTime,
			"symbol":             h.Symbol,
			"current_price":      h.CurrentPrice,
			"recommendation":     rec,
			"overall_risk_score": riskScore,
			"crash_probability":  crashProb,
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

// getNewsHistoryByID 獲取指定历史記錄详情
// GET /api/news/history/:id
func getNewsHistoryByID(c *gin.Context) {
	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "存儲服務未就绪"})
		return
	}

	st := storageProv.GetStorage()
	if st == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "存儲服務未就绪"})
		return
	}

	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "無效的 ID"})
		return
	}

	h, err := st.GetNewsAnalysisHistoryByID(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if h == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "記錄不存在"})
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

// getPredictionsAccuracy 獲取預测准确率统计
// GET /api/predictions/accuracy?asset_type=&since=7d
func getPredictionsAccuracy(c *gin.Context) {
	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "存儲服務未就绪"})
		return
	}
	st := storageProv.GetStorage()
	if st == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "存儲服務未就绪"})
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

	// 獲取按 timeframe 分组的统计
	tfStats, err := st.GetPredictionAccuracyStatsByTimeframe(assetType, since)
	if err != nil {
		logger.Warn("📊 獲取按 timeframe 分组统计失败: %v", err)
	}

	// 獲取按 timeframe 和方向分组的统计
	dirStats, err := st.GetPredictionDirectionStatsByTimeframe(assetType, since)
	if err != nil {
		logger.Warn("📊 獲取按方向分组统计失败: %v", err)
	}

	accuracy := 0.0
	if total > 0 {
		accuracy = float64(correct) / float64(total) * 100
	}

	// 格式化 timeframe 统计
	timeframeBreakdown := make(map[string]interface{})
	for tf, s := range tfStats {
		acc := 0.0
		if s.Total > 0 {
			acc = float64(s.Correct) / float64(s.Total) * 100
		}

		// 处理方向统计
		directions := make(map[string]interface{})
		if ds, ok := dirStats[tf]; ok {
			for dir, dstat := range ds {
				dacc := 0.0
				if dstat.Total > 0 {
					dacc = float64(dstat.Correct) / float64(dstat.Total) * 100
				}
				directions[dir] = gin.H{
					"total":    dstat.Total,
					"correct":  dstat.Correct,
					"accuracy": dacc,
				}
			}
		}

		timeframeBreakdown[tf] = gin.H{
			"total":      s.Total,
			"correct":    s.Correct,
			"accuracy":   acc,
			"directions": directions,
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"total":               total,
		"correct":             correct,
		"accuracy":            accuracy,
		"timeframe_breakdown": timeframeBreakdown,
		"since":               since,
		"asset_type":          assetType,
	})
}

// getPredictionsHistory 獲取預测驗证历史
// GET /api/predictions/history?asset_type=&symbol=&start_time=&end_time=&limit=20&offset=0
func getPredictionsHistory(c *gin.Context) {
	storageProv := PickStorageProvider(c)
	if storageProv == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "存儲服務未就绪"})
		return
	}
	st := storageProv.GetStorage()
	if st == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "存儲服務未就绪"})
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
