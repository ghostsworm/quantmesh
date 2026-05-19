package processor

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"quantmesh/ai/service"
	"quantmesh/database"
	"quantmesh/logger"
)

type TaskProcessor struct {
	taskService    *service.TaskService
	aiService      *service.AIService
	isRunning      bool
	checkInterval  time.Duration
	workerPool     chan struct{}
	ctx            context.Context
	cancel         context.CancelFunc
	mu             sync.Mutex
}

func NewTaskProcessor(taskService *service.TaskService, aiService *service.AIService) *TaskProcessor {
	ctx, cancel := context.WithCancel(context.Background())
	return &TaskProcessor{
		taskService:   taskService,
		aiService:     aiService,
		checkInterval: 2 * time.Second, // 每 2 秒检查一次，提升响应速度
		workerPool:    make(chan struct{}, 10), // 預設 10 個並发
		ctx:           ctx,
		cancel:        cancel,
	}
}

func (p *TaskProcessor) Start() {
	p.mu.Lock()
	if p.isRunning {
		p.mu.Unlock()
		return
	}
	p.isRunning = true
	p.mu.Unlock()

	logger.Info("AI 任務处理器已啟动")
	ticker := time.NewTicker(p.checkInterval)
	defer ticker.Stop()

	// 立即检查一次
	p.processTasks()

	for {
		select {
		case <-p.ctx.Done():
			return
		case <-ticker.C:
			p.processTasks()
		}
	}
}

func (p *TaskProcessor) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.isRunning {
		return
	}
	p.isRunning = false
	p.cancel()
	logger.Info("AI 任務处理器已停止")
}

func (p *TaskProcessor) processTasks() {
	// 先標記已超時仍顯示為 running 的任務（HTTP 未響應 context 取消時會出現）
	stale, err := p.taskService.GetStaleRunningTasks(p.ctx, 20)
	if err != nil {
		logger.Error("獲取超時運行中任務失败: %v", err)
	} else {
		for _, t := range stale {
			msg := fmt.Sprintf("任務運行超過 %d 秒未完成，已標記為超時", t.TimeoutSeconds)
			if err := p.taskService.UpdateTaskStatus(p.ctx, t.ID, "timeout", nil, &msg); err != nil {
				logger.Error("標記任務 %s 超時失败: %v", t.ID, err)
			} else {
				logger.Info("已將超時任務 %s 標記為 timeout", t.ID)
			}
		}
	}

	tasks, err := p.taskService.GetPendingTasks(p.ctx, 10)
	if err != nil {
		logger.Error("獲取待处理 AI 任務失败: %v", err)
		return
	}

	if len(tasks) == 0 {
		return
	}

	for _, task := range tasks {
		p.workerPool <- struct{}{}
		go func(t *database.AsyncTask) {
			defer func() { <-p.workerPool }()
			p.executeTask(t)
		}(task)
	}
}

func (p *TaskProcessor) executeTask(task *database.AsyncTask) {
	logger.Info("开始執行 AI 任務: %s (類型: %s)", task.ID, task.TaskType)

	// 更新状態為运行中
	if err := p.taskService.UpdateTaskStatus(p.ctx, task.ID, "running", nil, nil); err != nil {
		logger.Error("更新任務状態失败: %v", err)
		return
	}

	var reqData map[string]interface{}
	if err := json.Unmarshal([]byte(task.RequestData), &reqData); err != nil {
		errMsg := fmt.Sprintf("解析请求數據失败: %v", err)
		p.taskService.UpdateTaskStatus(p.ctx, task.ID, "failed", nil, &errMsg)
		return
	}

	// 構建 AI 请求
	prompt, _ := reqData["prompt"].(string)
	sysInst, _ := reqData["system_instruction"].(string)
	apiKey, _ := reqData["gemini_api_key"].(string)
	if apiKey == "" || apiKey == service.RedactedAPIKey {
		apiKey = service.ResolveTaskGeminiAPIKey(task.ID)
	}

	aiReq := service.AIRequest{
		Prompt:            prompt,
		SystemInstruction: sysInst,
		Model:             "",
		GeminiAPIKey:      apiKey,
	}

	if task.Model != nil && *task.Model != "" {
		aiReq.Model = *task.Model
	}
	if useSearch, ok := reqData["use_google_search"].(bool); ok && useSearch {
		aiReq.UseGoogleSearch = true
	}

	if schema, ok := reqData["json_schema"].(map[string]interface{}); ok {
		aiReq.JSONSchema = schema
	} else if schemaStr, ok := reqData["json_schema"].(string); ok && schemaStr != "" {
		var schemaMap map[string]interface{}
		if err := json.Unmarshal([]byte(schemaStr), &schemaMap); err == nil {
			aiReq.JSONSchema = schemaMap
		}
	}

	// 執行 AI 調用
	ctx, cancel := context.WithTimeout(p.ctx, time.Duration(task.TimeoutSeconds)*time.Second)
	defer cancel()

	resp, err := p.aiService.GenerateContent(ctx, aiReq)
	if err != nil {
		errMsg := fmt.Sprintf("AI 調用异常: %v", err)
		logger.Error("任務 %s 執行异常: %s", task.ID, errMsg)

		// 超時時標記為 timeout，其餘為 failed
		finalStatus := "failed"
		if errors.Is(err, context.DeadlineExceeded) {
			finalStatus = "timeout"
			timeoutMsg := fmt.Sprintf("任務在 %d 秒內未完成，已超時", task.TimeoutSeconds)
			errMsg = timeoutMsg
		}

		// 重試逻辑（超時也允許重試）
		if task.RetryCount < task.MaxRetries {
			p.taskService.RetryTask(p.ctx, task.ID)
			logger.Info("任務 %s 將進行重試 (當前重試次數: %d)", task.ID, task.RetryCount+1)
		} else {
			p.taskService.UpdateTaskStatus(p.ctx, task.ID, finalStatus, nil, &errMsg)
		}
		return
	}

	if !resp.Success {
		logger.Error("任務 %s 執行失败: %s", task.ID, resp.Error)
		if task.RetryCount < task.MaxRetries {
			p.taskService.RetryTask(p.ctx, task.ID)
		} else {
			p.taskService.UpdateTaskStatus(p.ctx, task.ID, "failed", nil, &resp.Error)
		}
		return
	}

	// 任務完成
	result := map[string]interface{}{
		"text":               resp.Content,
		"ai_input":           resp.AIInput,
		"ai_output":          resp.AIOutput,
		"input_tokens":       resp.InputTokens,
		"output_tokens":      resp.OutputTokens,
		"processing_time_ms": resp.ProcessingTimeMs,
		"used_api_key":       resp.UsedAPIKey,
	}

	if err := p.taskService.UpdateTaskStatus(p.ctx, task.ID, "completed", result, nil); err != nil {
		logger.Error("更新任務完成状態失败: %v", err)
	} else {
		logger.Info("任務 %s 執行成功", task.ID)
	}
}
