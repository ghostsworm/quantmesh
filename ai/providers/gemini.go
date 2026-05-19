package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"quantmesh/ai/service"
	"quantmesh/logger"
)

// GeminiProvider Gemini AI Provider实现
type GeminiProvider struct {
	apiKey string
	model  string
}

// NewGeminiProvider 创建Gemini Provider
func NewGeminiProvider(apiKey, model string) *GeminiProvider {
	if model == "" {
		model = "gemini-3-flash-preview" // 默认模型
	}
	return &GeminiProvider{
		apiKey: apiKey,
		model:  model,
	}
}

// GenerateContent 实现AIClient接口
func (p *GeminiProvider) GenerateContent(ctx context.Context, prompt string, schema map[string]interface{}, useGoogleSearch bool) (string, error) {
	// 获取全局任务服务（通过函数参数传递，避免循环导入）
	// 这里我们需要通过一个全局变量或者函数来获取，暂时使用service包
	taskService := getGlobalTaskService()
	if taskService == nil {
		return "", fmt.Errorf("AI 任務服務未初始化")
	}

	// 使用现有的异步任务系统
	requestData := map[string]interface{}{
		"prompt":             prompt,
		"system_instruction": "你是 QuantMesh 的市场分析与配置助手。严格按请求的 JSON schema 输出，不要泄露密钥或凭据。",
		"gemini_api_key":     p.apiKey,
		"json_schema":        schema,
		"model":              p.model,
	}
	if useGoogleSearch {
		requestData["use_google_search"] = true
	}

	// 任务超时10分钟
	taskID, err := taskService.CreateTask(ctx, "generate_content", requestData, 600, 3)
	if err != nil {
		return "", fmt.Errorf("創建异步任務失败: %w", err)
	}

	logger.Info("🔄 [Gemini] 已創建 AI 异步任務: %s，开始輪詢結果...", taskID)

	// 轮询任务结果
	maxPolls := 600 // 约20分钟
	pollInterval := 2 * time.Second

	for i := 0; i < maxPolls; i++ {
		select {
		case <-ctx.Done():
			return "", fmt.Errorf("任務被取消: %w", ctx.Err())
		case <-time.After(pollInterval):
			task, err := taskService.GetTask(ctx, taskID)
			if err != nil {
				if i%10 == 0 {
					logger.Warn("⚠️ 輪詢任務 %s 失败 (第 %d 次): %v", taskID, i+1, err)
				}
				continue
			}

			if i%10 == 0 {
				logger.Info("🔄 輪詢任務 %s 状態: %s (第 %d 次)", taskID, task.Status, i+1)
			}

			if task.Status == "completed" {
				var resultData map[string]interface{}
				if err := json.Unmarshal([]byte(task.Result), &resultData); err != nil {
					return "", fmt.Errorf("解析任務結果失败: %w", err)
				}
				if text, ok := resultData["text"].(string); ok {
					return text, nil
				}
				return "", fmt.Errorf("任務結果中缺少文本内容")
			} else if task.Status == "failed" || task.Status == "timeout" {
				errMsg := "未知錯误"
				if task.ErrorMessage != nil {
					errMsg = *task.ErrorMessage
				}
				return "", fmt.Errorf("AI 任務執行失败: %s", errMsg)
			}
		}
	}

	return "", fmt.Errorf("AI 任務处理超時 (TaskID: %s)", taskID)
}

// getGlobalTaskService 获取全局任务服务（通过函数指针避免循环导入）
var getGlobalTaskService func() *service.TaskService

// SetGlobalTaskServiceGetter 设置全局任务服务获取函数
func SetGlobalTaskServiceGetter(getter func() *service.TaskService) {
	getGlobalTaskService = getter
}
