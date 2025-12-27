package ai

import (
	"context"
	"fmt"
	"sync"

	"quantmesh/config"
)

// DecisionEngine AI决策引擎
type DecisionEngine struct {
	cfg                *config.Config
	marketAnalyzer     *MarketAnalyzer
	parameterOptimizer *ParameterOptimizer
	riskAnalyzer       *RiskAnalyzer
	sentimentAnalyzer  *SentimentAnalyzer
	polymarketSignalAnalyzer *PolymarketSignalAnalyzer
	
	mu sync.RWMutex
}

// NewDecisionEngine 创建决策引擎
func NewDecisionEngine(
	cfg *config.Config,
	marketAnalyzer *MarketAnalyzer,
	parameterOptimizer *ParameterOptimizer,
	riskAnalyzer *RiskAnalyzer,
	sentimentAnalyzer *SentimentAnalyzer,
	polymarketSignalAnalyzer *PolymarketSignalAnalyzer,
) *DecisionEngine {
	return &DecisionEngine{
		cfg:                cfg,
		marketAnalyzer:     marketAnalyzer,
		parameterOptimizer: parameterOptimizer,
		riskAnalyzer:       riskAnalyzer,
		sentimentAnalyzer:  sentimentAnalyzer,
		polymarketSignalAnalyzer: polymarketSignalAnalyzer,
	}
}

// MakeDecision 做出交易决策
func (de *DecisionEngine) MakeDecision(ctx context.Context) (*TradingDecision, error) {
	// 获取各模块的分析结果
	marketAnalysis := de.marketAnalyzer.GetLastAnalysis()
	riskAnalysis := de.riskAnalyzer.GetLastAnalysis()
	sentimentAnalysis := de.sentimentAnalyzer.GetLastAnalysis()
	polymarketSignal := de.polymarketSignalAnalyzer.GetLastAnalysis()
	
	// 如果没有任何分析结果，返回hold
	if marketAnalysis == nil && riskAnalysis == nil && sentimentAnalysis == nil && polymarketSignal == nil {
		return &TradingDecision{
			Action:     "hold",
			Confidence:  0.0,
			Reasoning:   "等待AI分析结果",
			RiskLevel:   "unknown",
		}, nil
	}
	
	// 根据决策模式做出决策
	decisionMode := DecisionMode(de.cfg.AI.DecisionMode)
	if decisionMode == "" {
		decisionMode = DecisionModeHybrid
	}
	
	switch decisionMode {
	case DecisionModeAdvisor:
		return de.makeAdvisorDecision(marketAnalysis, riskAnalysis, sentimentAnalysis, polymarketSignal)
	case DecisionModeExecutor:
		return de.makeExecutorDecision(marketAnalysis, riskAnalysis, sentimentAnalysis, polymarketSignal)
	case DecisionModeHybrid:
		return de.makeHybridDecision(marketAnalysis, riskAnalysis, sentimentAnalysis, polymarketSignal)
	default:
		return de.makeHybridDecision(marketAnalysis, riskAnalysis, sentimentAnalysis, polymarketSignal)
	}
}

// makeAdvisorDecision 建议模式决策
func (de *DecisionEngine) makeAdvisorDecision(
	marketAnalysis *MarketAnalysisResponse,
	riskAnalysis *RiskAnalysisResponse,
	sentimentAnalysis *SentimentAnalysisResponse,
	polymarketSignal *PolymarketSignalAnalysisResponse,
) (*TradingDecision, error) {
	decision := &TradingDecision{
		Action:              "hold",
		Confidence:          0.5,
		Reasoning:           "",
		RiskLevel:           "medium",
		RequiresConfirmation: true, // 建议模式总是需要确认
	}
	
	// 综合各模块的分析
	var reasons []string
	
	if marketAnalysis != nil {
		decision.Action = marketAnalysis.Signal
		decision.Confidence = marketAnalysis.Confidence
		reasons = append(reasons, fmt.Sprintf("市场分析: %s", marketAnalysis.Reasoning))
	}
	
	if riskAnalysis != nil {
		decision.RiskLevel = riskAnalysis.RiskLevel
		if riskAnalysis.RiskScore > de.cfg.AI.ExecutionRules.HighRiskThreshold {
			decision.Action = "hold" // 高风险时建议hold
			reasons = append(reasons, fmt.Sprintf("风险警告: %s", riskAnalysis.Reasoning))
		} else {
			reasons = append(reasons, fmt.Sprintf("风险分析: %s", riskAnalysis.Reasoning))
		}
	}
	
	if sentimentAnalysis != nil {
		// 情绪分析影响置信度
		if sentimentAnalysis.SentimentScore > 0.5 {
			decision.Confidence *= 1.1 // 乐观情绪提高置信度
		} else if sentimentAnalysis.SentimentScore < -0.5 {
			decision.Confidence *= 0.9 // 悲观情绪降低置信度
		}
		reasons = append(reasons, fmt.Sprintf("情绪分析: %s", sentimentAnalysis.Reasoning))
	}
	
	if polymarketSignal != nil {
		// 预测市场信号影响决策
		if polymarketSignal.AggregateSignal == "buy" && polymarketSignal.AggregateConfidence > 0.6 {
			if decision.Action == "hold" {
				decision.Action = "buy"
			}
			decision.Confidence *= (1.0 + polymarketSignal.AggregateStrength*0.2) // 提高置信度
		} else if polymarketSignal.AggregateSignal == "sell" && polymarketSignal.AggregateConfidence > 0.6 {
			if decision.Action == "hold" {
				decision.Action = "sell"
			}
		}
		reasons = append(reasons, fmt.Sprintf("预测市场信号: %s (强度: %.2f)", 
			polymarketSignal.AggregateSignal, polymarketSignal.AggregateStrength))
	}
	
	decision.Reasoning = fmt.Sprintf("AI建议（需要确认）: %v", reasons)
	
	return decision, nil
}

// makeExecutorDecision 执行模式决策
func (de *DecisionEngine) makeExecutorDecision(
	marketAnalysis *MarketAnalysisResponse,
	riskAnalysis *RiskAnalysisResponse,
	sentimentAnalysis *SentimentAnalysisResponse,
	polymarketSignal *PolymarketSignalAnalysisResponse,
) (*TradingDecision, error) {
	decision := &TradingDecision{
		Action:              "hold",
		Confidence:          0.5,
		Reasoning:           "",
		RiskLevel:           "medium",
		RequiresConfirmation: false, // 执行模式不需要确认
	}
	
	var reasons []string
	
	if marketAnalysis != nil {
		decision.Action = marketAnalysis.Signal
		decision.Confidence = marketAnalysis.Confidence
		reasons = append(reasons, fmt.Sprintf("市场分析: %s", marketAnalysis.Reasoning))
	}
	
	if riskAnalysis != nil {
		decision.RiskLevel = riskAnalysis.RiskLevel
		// 执行模式下，只有极高风险才hold
		if riskAnalysis.RiskScore > 0.9 {
			decision.Action = "hold"
			reasons = append(reasons, fmt.Sprintf("极高风险，暂停交易: %s", riskAnalysis.Reasoning))
		} else {
			reasons = append(reasons, fmt.Sprintf("风险分析: %s", riskAnalysis.Reasoning))
		}
	}
	
	if sentimentAnalysis != nil {
		if sentimentAnalysis.SentimentScore > 0.5 {
			decision.Confidence *= 1.1
		} else if sentimentAnalysis.SentimentScore < -0.5 {
			decision.Confidence *= 0.9
		}
		reasons = append(reasons, fmt.Sprintf("情绪分析: %s", sentimentAnalysis.Reasoning))
	}
	
	if polymarketSignal != nil && polymarketSignal.AggregateConfidence > 0.7 {
		// 执行模式下，高置信度的预测市场信号可以影响决策
		if polymarketSignal.AggregateSignal == "buy" {
			decision.Action = "buy"
			decision.Confidence *= (1.0 + polymarketSignal.AggregateStrength*0.15)
		} else if polymarketSignal.AggregateSignal == "sell" {
			decision.Action = "sell"
		}
		reasons = append(reasons, fmt.Sprintf("预测市场信号: %s (强度: %.2f)", 
			polymarketSignal.AggregateSignal, polymarketSignal.AggregateStrength))
	}
	
	decision.Reasoning = fmt.Sprintf("AI自动执行: %v", reasons)
	
	return decision, nil
}

// makeHybridDecision 混合模式决策
func (de *DecisionEngine) makeHybridDecision(
	marketAnalysis *MarketAnalysisResponse,
	riskAnalysis *RiskAnalysisResponse,
	sentimentAnalysis *SentimentAnalysisResponse,
	polymarketSignal *PolymarketSignalAnalysisResponse,
) (*TradingDecision, error) {
	decision := &TradingDecision{
		Action:              "hold",
		Confidence:          0.5,
		Reasoning:           "",
		RiskLevel:           "medium",
		RequiresConfirmation: false,
	}
	
	var reasons []string
	
	if marketAnalysis != nil {
		decision.Action = marketAnalysis.Signal
		decision.Confidence = marketAnalysis.Confidence
		reasons = append(reasons, fmt.Sprintf("市场分析: %s", marketAnalysis.Reasoning))
	}
	
	if riskAnalysis != nil {
		decision.RiskLevel = riskAnalysis.RiskLevel
		
		// 根据风险等级决定是否需要确认
		riskScore := riskAnalysis.RiskScore
		if riskScore > de.cfg.AI.ExecutionRules.HighRiskThreshold {
			decision.RequiresConfirmation = true
			decision.Action = "hold" // 高风险时hold
			reasons = append(reasons, fmt.Sprintf("高风险警告，需要确认: %s", riskAnalysis.Reasoning))
		} else if riskScore < de.cfg.AI.ExecutionRules.LowRiskThreshold {
			// 低风险可以直接执行
			decision.RequiresConfirmation = false
			reasons = append(reasons, fmt.Sprintf("低风险，可直接执行: %s", riskAnalysis.Reasoning))
		} else {
			// 中等风险需要确认
			decision.RequiresConfirmation = de.cfg.AI.ExecutionRules.RequireConfirmation
			reasons = append(reasons, fmt.Sprintf("风险分析: %s", riskAnalysis.Reasoning))
		}
	}
	
	if sentimentAnalysis != nil {
		if sentimentAnalysis.SentimentScore > 0.5 {
			decision.Confidence *= 1.1
		} else if sentimentAnalysis.SentimentScore < -0.5 {
			decision.Confidence *= 0.9
		}
		reasons = append(reasons, fmt.Sprintf("情绪分析: %s", sentimentAnalysis.Reasoning))
	}
	
	if polymarketSignal != nil {
		// 混合模式下，根据置信度决定影响程度
		if polymarketSignal.AggregateConfidence > 0.7 {
			// 高置信度：直接影响决策
			if polymarketSignal.AggregateSignal == "buy" {
				if decision.Action == "hold" {
					decision.Action = "buy"
				}
				decision.Confidence *= (1.0 + polymarketSignal.AggregateStrength*0.15)
			} else if polymarketSignal.AggregateSignal == "sell" {
				if decision.Action == "hold" {
					decision.Action = "sell"
				}
			}
		} else if polymarketSignal.AggregateConfidence > 0.5 {
			// 中等置信度：影响置信度
			decision.Confidence *= (1.0 + polymarketSignal.AggregateStrength*0.1)
		}
		
		// 根据信号强度决定是否需要确认
		if polymarketSignal.AggregateStrength > 0.7 && polymarketSignal.AggregateConfidence > 0.6 {
			decision.RequiresConfirmation = false // 强信号可直接执行
		}
		
		reasons = append(reasons, fmt.Sprintf("预测市场信号: %s (强度: %.2f, 置信度: %.2f)", 
			polymarketSignal.AggregateSignal, polymarketSignal.AggregateStrength, polymarketSignal.AggregateConfidence))
	}
	
	decision.Reasoning = fmt.Sprintf("AI混合决策: %v", reasons)
	
	return decision, nil
}

// GetParameterOptimization 获取参数优化建议
func (de *DecisionEngine) GetParameterOptimization() *ParameterOptimizationResponse {
	return de.parameterOptimizer.GetLastOptimization()
}

// GetMarketAnalysis 获取市场分析
func (de *DecisionEngine) GetMarketAnalysis() *MarketAnalysisResponse {
	return de.marketAnalyzer.GetLastAnalysis()
}

// GetRiskAnalysis 获取风险分析
func (de *DecisionEngine) GetRiskAnalysis() *RiskAnalysisResponse {
	return de.riskAnalyzer.GetLastAnalysis()
}

// GetSentimentAnalysis 获取情绪分析
func (de *DecisionEngine) GetSentimentAnalysis() *SentimentAnalysisResponse {
	return de.sentimentAnalyzer.GetLastAnalysis()
}

// GetPolymarketSignal 获取预测市场信号
func (de *DecisionEngine) GetPolymarketSignal() *PolymarketSignalAnalysisResponse {
	if de.polymarketSignalAnalyzer == nil {
		return nil
	}
	return de.polymarketSignalAnalyzer.GetLastAnalysis()
}

