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
// provider: gemini, openai, claude, dashscope, kimi, deepseek, poe, custom（OpenAI 兼容协议）
// model: 模型名称，如 "gpt-4o", "claude-3-5-sonnet-latest"
// apiKey: Provider的API Key
// baseURL: 可选，自定义API端点（用于 Ollama / OneAPI 中转 / Poe 等）
//
// 所有 provider 统一走异步任务队列 + 轮询，共享重试 / 超时 / token 统计；
// 实际协议由 ai/service 的 transport 层按 provider 选择。
func NewAIClient(provider, model, apiKey, baseURL string) (AIClient, error) {
	if apiKey == "" {
		return nil, fmt.Errorf("%s API Key 未配置", provider)
	}

	provider = service.NormalizeProvider(provider)
	switch provider {
	case "gemini", "openai", "claude", "anthropic", "poe", "dashscope", "dashscope_sg", "kimi", "kimi_intl", "deepseek", "openai_compatible":
		// 已知 provider：直接构造统一队列客户端
	default:
		logger.Warn("⚠️ 未知的AI Provider: %s，回退到 Gemini", provider)
		provider = "gemini"
	}

	// poe / 自定义中转站必须显式配置 base_url。
	if provider == "poe" || provider == "openai_compatible" {
		if baseURL == "" {
			return nil, fmt.Errorf("%s provider 需要配置 base_url", provider)
		}
		provider = "openai"
	}

	return providers.NewQueueProvider(provider, apiKey, model, baseURL), nil
}
