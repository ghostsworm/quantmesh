package ai

import (
	"fmt"

	"quantmesh/ai/providers"
	"quantmesh/ai/service"
	"quantmesh/logger"
)

func init() {
	// 初始化providers包的全局任务服务获取函数
	providers.SetGlobalTaskServiceGetter(func() *service.TaskService {
		return GlobalTaskService
	})
}

// NewAIClient 创建AI客户端工厂函数
// provider: gemini, openai, claude, poe
// model: 模型名称，如 "gpt-4", "claude-3-opus"
// apiKey: Provider的API Key
// baseURL: 可选，自定义API端点（用于Poe等代理）
func NewAIClient(provider, model, apiKey, baseURL string) (AIClient, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("%s API Key 未配置", provider)
	}

	switch provider {
	case "gemini":
		return providers.NewGeminiProvider(apiKey, model), nil
	case "openai":
		return providers.NewOpenAIProvider(apiKey, model, baseURL), nil
	case "claude":
		return providers.NewClaudeProvider(apiKey, model, baseURL), nil
	case "poe":
		if baseURL == "" {
			return nil, fmt.Errorf("Poe provider 需要配置 base_url")
		}
		return providers.NewPoeProvider(apiKey, model, baseURL), nil
	default:
		logger.Warn("⚠️ 未知的AI Provider: %s，回退到Gemini", provider)
		return providers.NewGeminiProvider(apiKey, model), nil
	}
}
