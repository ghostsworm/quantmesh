package ai

import (
	"context"
)

// AIClient AI客户端统一接口
type AIClient interface {
	// GenerateContent 生成内容
	// ctx: 上下文
	// prompt: 提示词
	// schema: JSON Schema（可选，用于结构化输出）
	// useGoogleSearch: 是否使用Google搜索（仅Gemini原生支持，其他provider通过prompt增强）
	GenerateContent(ctx context.Context, prompt string, schema map[string]interface{}, useGoogleSearch bool) (string, error)
}
