package processor

import (
	"context"
	"encoding/json"
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
		checkInterval: 30 * time.Second,
		workerPool:    make(chan struct{}, 10), // 默认 10 个并发
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

	logger.Info("AI 任务处理器已启动")
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
	logger.Info("AI 任务处理器已停止")
}

func (p *TaskProcessor) processTasks() {
	tasks, err := p.taskService.GetPendingTasks(p.ctx, 10)
	if err != nil {
		logger.Error("获取待处理 AI 任务失败: %v", err)
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
	logger.Info("开始执行 AI 任务: %s (类型: %s)", task.ID, task.TaskType)

	// 更新状态为运行中
	if err := p.taskService.UpdateTaskStatus(p.ctx, task.ID, "running", nil, nil); err != nil {
		logger.Error("更新任务状态失败: %v", err)
		return
	}

	var reqData map[string]interface{}
	if err := json.Unmarshal([]byte(task.RequestData), &reqData); err != nil {
		errMsg := fmt.Sprintf("解析请求数据失败: %v", err)
		p.taskService.UpdateTaskStatus(p.ctx, task.ID, "failed", nil, &errMsg)
		return
	}

	// 构建 AI 请求
	aiReq := service.AIRequest{
		Prompt:            reqData["prompt"].(string),
		SystemInstruction: reqData["system_instruction"].(string),
		Model:             "",
		GeminiAPIKey:      reqData["gemini_api_key"].(string),
	}

	if task.Model != nil && *task.Model != "" {
		aiReq.Model = *task.Model
	}

	if schema, ok := reqData["json_schema"].(map[string]interface{}); ok {
		aiReq.JSONSchema = schema
	} else if schemaStr, ok := reqData["json_schema"].(string); ok && schemaStr != "" {
		var schemaMap map[string]interface{}
		if err := json.Unmarshal([]byte(schemaStr), &schemaMap); err == nil {
			aiReq.JSONSchema = schemaMap
		}
	}

	// 执行 AI 调用
	ctx, cancel := context.WithTimeout(p.ctx, time.Duration(task.TimeoutSeconds)*time.Second)
	defer cancel()

	resp, err := p.aiService.GenerateContent(ctx, aiReq)
	if err != nil {
		errMsg := fmt.Sprintf("AI 调用异常: %v", err)
		logger.Error("任务 %s 执行异常: %s", task.ID, errMsg)
		
		// 重试逻辑
		if task.RetryCount < task.MaxRetries {
			p.taskService.RetryTask(p.ctx, task.ID)
			logger.Info("任务 %s 将进行重试 (当前重试次数: %d)", task.ID, task.RetryCount+1)
		} else {
			p.taskService.UpdateTaskStatus(p.ctx, task.ID, "failed", nil, &errMsg)
		}
		return
	}

	if !resp.Success {
		logger.Error("任务 %s 执行失败: %s", task.ID, resp.Error)
		if task.RetryCount < task.MaxRetries {
			p.taskService.RetryTask(p.ctx, task.ID)
		} else {
			p.taskService.UpdateTaskStatus(p.ctx, task.ID, "failed", nil, &resp.Error)
		}
		return
	}

	// 任务完成
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
		logger.Error("更新任务完成状态失败: %v", err)
	} else {
		logger.Info("任务 %s 执行成功", task.ID)
	}
}
