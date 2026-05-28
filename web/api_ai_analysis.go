package web

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// ========== AI分析相关API ==========

var (
	// AI模塊提供者（需要從main.go注入）
	aiMarketAnalyzerProvider     AIMarketAnalyzerProvider
	aiParameterOptimizerProvider AIParameterOptimizerProvider
	aiRiskAnalyzerProvider       AIRiskAnalyzerProvider
	aiSentimentAnalyzerProvider  AISentimentAnalyzerProvider
	aiPolymarketSignalProvider   AIPolymarketSignalProvider
	aiPromptManagerProvider      AIPromptManagerProvider
)

// AI提供者接口
type AIMarketAnalyzerProvider interface {
	GetLastAnalysis() interface{}
	GetLastAnalysisTime() time.Time
	PerformAnalysis() error
}

type AIParameterOptimizerProvider interface {
	GetLastOptimization() interface{}
	GetLastOptimizationTime() time.Time
	PerformOptimization() error
}

type AIRiskAnalyzerProvider interface {
	GetLastAnalysis() interface{}
	GetLastAnalysisTime() time.Time
	PerformAnalysis() error
}

type AISentimentAnalyzerProvider interface {
	GetLastAnalysis() interface{}
	GetLastAnalysisTime() time.Time
	PerformAnalysis() error
}

type AIPolymarketSignalProvider interface {
	GetLastAnalysis() interface{}
	GetLastAnalysisTime() time.Time
	PerformAnalysis() error
}

type AIPromptManagerProvider interface {
	GetAllPrompts() (map[string]interface{}, error)
	UpdatePrompt(module, template, systemPrompt string) error
}

// SetAIProviders 設置AI提供者
func SetAIMarketAnalyzerProvider(provider AIMarketAnalyzerProvider) {
	aiMarketAnalyzerProvider = provider
}

func SetAIParameterOptimizerProvider(provider AIParameterOptimizerProvider) {
	aiParameterOptimizerProvider = provider
}

func SetAIRiskAnalyzerProvider(provider AIRiskAnalyzerProvider) {
	aiRiskAnalyzerProvider = provider
}

func SetAISentimentAnalyzerProvider(provider AISentimentAnalyzerProvider) {
	aiSentimentAnalyzerProvider = provider
}

func SetAIPolymarketSignalProvider(provider AIPolymarketSignalProvider) {
	aiPolymarketSignalProvider = provider
}

func SetAIPromptManagerProvider(provider AIPromptManagerProvider) {
	aiPromptManagerProvider = provider
}

// getAIAnalysisStatus 獲取AI系统状態
// GET /api/ai/status
func getAIAnalysisStatus(c *gin.Context) {
	status := map[string]interface{}{
		"enabled": true,
		"modules": map[string]interface{}{
			"market_analysis": map[string]interface{}{
				"enabled":     aiMarketAnalyzerProvider != nil,
				"last_update": nil,
				"has_data":    false,
			},
			"parameter_optimization": map[string]interface{}{
				"enabled":     aiParameterOptimizerProvider != nil,
				"last_update": nil,
				"has_data":    false,
			},
			"risk_analysis": map[string]interface{}{
				"enabled":     aiRiskAnalyzerProvider != nil,
				"last_update": nil,
				"has_data":    false,
			},
			"sentiment_analysis": map[string]interface{}{
				"enabled":     aiSentimentAnalyzerProvider != nil,
				"last_update": nil,
				"has_data":    false,
			},
			"polymarket_signal": map[string]interface{}{
				"enabled":     aiPolymarketSignalProvider != nil,
				"last_update": nil,
				"has_data":    false,
			},
		},
	}

	// 更新各模塊状態
	if aiMarketAnalyzerProvider != nil {
		lastTime := aiMarketAnalyzerProvider.GetLastAnalysisTime()
		lastAnalysis := aiMarketAnalyzerProvider.GetLastAnalysis()
		status["modules"].(map[string]interface{})["market_analysis"].(map[string]interface{})["last_update"] = lastTime
		status["modules"].(map[string]interface{})["market_analysis"].(map[string]interface{})["has_data"] = lastAnalysis != nil
	}

	if aiParameterOptimizerProvider != nil {
		lastTime := aiParameterOptimizerProvider.GetLastOptimizationTime()
		lastOptimization := aiParameterOptimizerProvider.GetLastOptimization()
		status["modules"].(map[string]interface{})["parameter_optimization"].(map[string]interface{})["last_update"] = lastTime
		status["modules"].(map[string]interface{})["parameter_optimization"].(map[string]interface{})["has_data"] = lastOptimization != nil
	}

	if aiRiskAnalyzerProvider != nil {
		lastTime := aiRiskAnalyzerProvider.GetLastAnalysisTime()
		lastAnalysis := aiRiskAnalyzerProvider.GetLastAnalysis()
		status["modules"].(map[string]interface{})["risk_analysis"].(map[string]interface{})["last_update"] = lastTime
		status["modules"].(map[string]interface{})["risk_analysis"].(map[string]interface{})["has_data"] = lastAnalysis != nil
	}

	if aiSentimentAnalyzerProvider != nil {
		lastTime := aiSentimentAnalyzerProvider.GetLastAnalysisTime()
		lastAnalysis := aiSentimentAnalyzerProvider.GetLastAnalysis()
		status["modules"].(map[string]interface{})["sentiment_analysis"].(map[string]interface{})["last_update"] = lastTime
		status["modules"].(map[string]interface{})["sentiment_analysis"].(map[string]interface{})["has_data"] = lastAnalysis != nil
	}

	if aiPolymarketSignalProvider != nil {
		lastTime := aiPolymarketSignalProvider.GetLastAnalysisTime()
		lastAnalysis := aiPolymarketSignalProvider.GetLastAnalysis()
		status["modules"].(map[string]interface{})["polymarket_signal"].(map[string]interface{})["last_update"] = lastTime
		status["modules"].(map[string]interface{})["polymarket_signal"].(map[string]interface{})["has_data"] = lastAnalysis != nil
	}

	c.JSON(http.StatusOK, status)
}

// getAIMarketAnalysis 獲取市場分析結果
// GET /api/ai/analysis/market
func getAIMarketAnalysis(c *gin.Context) {
	if aiMarketAnalyzerProvider == nil {
		c.JSON(http.StatusOK, gin.H{"error": "市场分析模塊未啟用"})
		return
	}

	analysis := aiMarketAnalyzerProvider.GetLastAnalysis()
	if analysis == nil {
		c.JSON(http.StatusOK, gin.H{"error": "暂無分析數據"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"analysis": analysis, "last_update": aiMarketAnalyzerProvider.GetLastAnalysisTime()})
}

// getAIParameterOptimization 獲取参數优化結果
// GET /api/ai/analysis/parameter
func getAIParameterOptimization(c *gin.Context) {
	if aiParameterOptimizerProvider == nil {
		c.JSON(http.StatusOK, gin.H{"error": "参數优化模塊未啟用"})
		return
	}

	optimization := aiParameterOptimizerProvider.GetLastOptimization()
	if optimization == nil {
		c.JSON(http.StatusOK, gin.H{"error": "暂無优化數據"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"optimization": optimization, "last_update": aiParameterOptimizerProvider.GetLastOptimizationTime()})
}

// getAIRiskAnalysis 獲取风險分析結果
// GET /api/ai/analysis/risk
func getAIRiskAnalysis(c *gin.Context) {
	if aiRiskAnalyzerProvider == nil {
		c.JSON(http.StatusOK, gin.H{"error": "风險分析模塊未啟用"})
		return
	}

	analysis := aiRiskAnalyzerProvider.GetLastAnalysis()
	if analysis == nil {
		c.JSON(http.StatusOK, gin.H{"error": "暂無分析數據"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"analysis": analysis, "last_update": aiRiskAnalyzerProvider.GetLastAnalysisTime()})
}

// getAISentimentAnalysis 獲取情绪分析結果
// GET /api/ai/analysis/sentiment
func getAISentimentAnalysis(c *gin.Context) {
	if aiSentimentAnalyzerProvider == nil {
		c.JSON(http.StatusOK, gin.H{"error": "情绪分析模塊未啟用"})
		return
	}

	analysis := aiSentimentAnalyzerProvider.GetLastAnalysis()
	if analysis == nil {
		c.JSON(http.StatusOK, gin.H{"error": "暂無分析數據"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"analysis": analysis, "last_update": aiSentimentAnalyzerProvider.GetLastAnalysisTime()})
}

// getAIPolymarketSignal 獲取Polymarket信号分析結果
// GET /api/ai/analysis/polymarket
func getAIPolymarketSignal(c *gin.Context) {
	if aiPolymarketSignalProvider == nil {
		c.JSON(http.StatusOK, gin.H{"error": "Polymarket信号模塊未啟用"})
		return
	}

	analysis := aiPolymarketSignalProvider.GetLastAnalysis()
	if analysis == nil {
		c.JSON(http.StatusOK, gin.H{"error": "暂無分析數據"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"analysis": analysis, "last_update": aiPolymarketSignalProvider.GetLastAnalysisTime()})
}

// triggerAIAnalysis 手动触发AI分析
// POST /api/ai/analysis/trigger/:module
func triggerAIAnalysis(c *gin.Context) {
	module := c.Param("module")
	var err error

	switch module {
	case "market":
		if aiMarketAnalyzerProvider != nil {
			err = aiMarketAnalyzerProvider.PerformAnalysis()
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"error": "市场分析模塊未啟用"})
			return
		}
	case "parameter":
		if aiParameterOptimizerProvider != nil {
			err = aiParameterOptimizerProvider.PerformOptimization()
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"error": "参數优化模塊未啟用"})
			return
		}
	case "risk":
		if aiRiskAnalyzerProvider != nil {
			err = aiRiskAnalyzerProvider.PerformAnalysis()
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"error": "风險分析模塊未啟用"})
			return
		}
	case "sentiment":
		if aiSentimentAnalyzerProvider != nil {
			err = aiSentimentAnalyzerProvider.PerformAnalysis()
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"error": "情绪分析模塊未啟用"})
			return
		}
	case "polymarket":
		if aiPolymarketSignalProvider != nil {
			err = aiPolymarketSignalProvider.PerformAnalysis()
		} else {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Polymarket信号模塊未啟用"})
			return
		}
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": "未知的模塊: " + module})
		return
	}

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "分析已触发"})
}

// getAIPrompts 獲取所有提示词模板
// GET /api/ai/prompts
func getAIPrompts(c *gin.Context) {
	if aiPromptManagerProvider == nil {
		c.JSON(http.StatusOK, gin.H{"prompts": map[string]interface{}{}})
		return
	}

	prompts, err := aiPromptManagerProvider.GetAllPrompts()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"prompts": prompts})
}

// updateAIPrompt 更新提示词模板
// POST /api/ai/prompts
func updateAIPrompt(c *gin.Context) {
	if aiPromptManagerProvider == nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "提示词管理器未啟用"})
		return
	}

	var req struct {
		Module       string `json:"module"`
		Template     string `json:"template"`
		SystemPrompt string `json:"system_prompt"`
	}

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Module == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "模塊名不能為空"})
		return
	}

	if req.Template == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "提示词模板不能為空"})
		return
	}

	if err := aiPromptManagerProvider.UpdatePrompt(req.Module, req.Template, req.SystemPrompt); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "提示词已更新"})
}

// AI模塊适配器
type aiMarketAnalyzerAdapter struct {
	analyzer interface {
		GetLastAnalysis() interface{}
		GetLastAnalysisTime() time.Time
		PerformAnalysis() error
	}
}

func (a *aiMarketAnalyzerAdapter) GetLastAnalysis() interface{} {
	return a.analyzer.GetLastAnalysis()
}

func (a *aiMarketAnalyzerAdapter) GetLastAnalysisTime() time.Time {
	return a.analyzer.GetLastAnalysisTime()
}

func (a *aiMarketAnalyzerAdapter) PerformAnalysis() error {
	return a.analyzer.PerformAnalysis()
}

type aiParameterOptimizerAdapter struct {
	optimizer interface {
		GetLastOptimization() interface{}
		GetLastOptimizationTime() time.Time
		PerformOptimization() error
	}
}

func (a *aiParameterOptimizerAdapter) GetLastOptimization() interface{} {
	return a.optimizer.GetLastOptimization()
}

func (a *aiParameterOptimizerAdapter) GetLastOptimizationTime() time.Time {
	return a.optimizer.GetLastOptimizationTime()
}

func (a *aiParameterOptimizerAdapter) PerformOptimization() error {
	return a.optimizer.PerformOptimization()
}

type aiRiskAnalyzerAdapter struct {
	analyzer interface {
		GetLastAnalysis() interface{}
		GetLastAnalysisTime() time.Time
		PerformAnalysis() error
	}
}

func (a *aiRiskAnalyzerAdapter) GetLastAnalysis() interface{} {
	return a.analyzer.GetLastAnalysis()
}

func (a *aiRiskAnalyzerAdapter) GetLastAnalysisTime() time.Time {
	return a.analyzer.GetLastAnalysisTime()
}

func (a *aiRiskAnalyzerAdapter) PerformAnalysis() error {
	return a.analyzer.PerformAnalysis()
}

type aiSentimentAnalyzerAdapter struct {
	analyzer interface {
		GetLastAnalysis() interface{}
		GetLastAnalysisTime() time.Time
		PerformAnalysis() error
	}
}

func (a *aiSentimentAnalyzerAdapter) GetLastAnalysis() interface{} {
	return a.analyzer.GetLastAnalysis()
}

func (a *aiSentimentAnalyzerAdapter) GetLastAnalysisTime() time.Time {
	return a.analyzer.GetLastAnalysisTime()
}

func (a *aiSentimentAnalyzerAdapter) PerformAnalysis() error {
	return a.analyzer.PerformAnalysis()
}

type aiPolymarketSignalAdapter struct {
	analyzer interface {
		GetLastAnalysis() interface{}
		GetLastAnalysisTime() time.Time
		PerformAnalysis() error
	}
}

func (a *aiPolymarketSignalAdapter) GetLastAnalysis() interface{} {
	return a.analyzer.GetLastAnalysis()
}

func (a *aiPolymarketSignalAdapter) GetLastAnalysisTime() time.Time {
	return a.analyzer.GetLastAnalysisTime()
}

func (a *aiPolymarketSignalAdapter) PerformAnalysis() error {
	return a.analyzer.PerformAnalysis()
}

type aiPromptManagerAdapter struct {
	manager interface {
		GetAllPrompts() (map[string]interface{}, error)
		UpdatePrompt(module, template, systemPrompt string) error
	}
}

func (a *aiPromptManagerAdapter) GetAllPrompts() (map[string]interface{}, error) {
	return a.manager.GetAllPrompts()
}

func (a *aiPromptManagerAdapter) UpdatePrompt(module, template, systemPrompt string) error {
	return a.manager.UpdatePrompt(module, template, systemPrompt)
}
