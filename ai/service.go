package ai

import (
	"context"
	"fmt"
)

// AIService AI服务接口
type AIService interface {
	// AnalyzeMarket 分析市场
	AnalyzeMarket(ctx context.Context, req *MarketAnalysisRequest) (*MarketAnalysisResponse, error)
	
	// OptimizeParameters 优化参数
	OptimizeParameters(ctx context.Context, req *ParameterOptimizationRequest) (*ParameterOptimizationResponse, error)
	
	// AnalyzeRisk 分析风险
	AnalyzeRisk(ctx context.Context, req *RiskAnalysisRequest) (*RiskAnalysisResponse, error)
	
	// AnalyzeSentiment 分析情绪
	AnalyzeSentiment(ctx context.Context, req *SentimentAnalysisRequest) (*SentimentAnalysisResponse, error)
	
	// GenerateStrategy 生成策略（实验性）
	GenerateStrategy(ctx context.Context, description string, constraints map[string]interface{}) (string, error)
	
	// Close 关闭服务
	Close() error
}

// AIServiceFactory AI服务工厂
type AIServiceFactory struct{}

// NewAIServiceFactory 创建AI服务工厂
func NewAIServiceFactory() *AIServiceFactory {
	return &AIServiceFactory{}
}

// CreateService 创建AI服务实例
func (f *AIServiceFactory) CreateService(serviceType AIServiceType, apiKey string, baseURL string) (AIService, error) {
	switch serviceType {
	case AIServiceGemini:
		return NewGeminiService(apiKey, baseURL)
	case AIServiceOpenAI:
		return NewOpenAIService(apiKey, baseURL)
	default:
		return nil, fmt.Errorf("不支持的AI服务类型: %s", serviceType)
	}
}

