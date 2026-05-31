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
// provider: gemini, openai, claude, poe（poe 走 openai 兼容协议）
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

	switch provider {
	case "gemini", "openai", "claude", "anthropic", "poe", "":
		// 已知 provider：直接构造统一队列客户端
	default:
		logger.Warn("⚠️ 未知的AI Provider: %s，回退到 Gemini", provider)
		provider = "gemini"
	}

	// poe 归入 openai 兼容协议（仅需 base_url），保留枚举字符串做向后兼容
	if provider == "poe" {
		if baseURL == "" {
			return nil, fmt.Errorf("poe provider 需要配置 base_url")
		}
		provider = "openai"
	}
	if provider == "anthropic" {
		provider = "claude"
	}

	return providers.NewQueueProvider(provider, apiKey, model, baseURL), nil
}
