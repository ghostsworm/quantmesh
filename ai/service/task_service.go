package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"quantmesh/database"
	"github.com/google/uuid"
)

type TaskService struct {
	db database.Database
}

func NewTaskService(db database.Database) *TaskService {
	return &TaskService{
		db: db,
	}
}

func (s *TaskService) CreateTask(ctx context.Context, taskType string, requestData map[string]interface{}, timeoutSeconds, maxRetries int) (string, error) {
	taskID := uuid.New().String()
	expiresAt := time.Now().Add(24 * time.Hour)

	requestDataJSON, err := json.Marshal(requestData)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request data: %w", err)
	}

	var modelPtr *string
	if modelValue, ok := requestData["model"]; ok {
		if modelStr, ok := modelValue.(string); ok && modelStr != "" {
			modelPtr = &modelStr
		}
	}

	task := &database.AsyncTask{
		ID:             taskID,
		TaskType:       taskType,
		Status:         "pending",
		RequestData:    string(requestDataJSON),
		Model:          modelPtr,
		RetryCount:     0,
		MaxRetries:     maxRetries,
		TimeoutSeconds: timeoutSeconds,
		ExpiresAt:      &expiresAt,
		CreatedAt:      time.Now(),
	}

	if err := s.db.SaveAsyncTask(ctx, task); err != nil {
		return "", fmt.Errorf("failed to create task: %w", err)
	}

	return taskID, nil
}

func (s *TaskService) GetTask(ctx context.Context, taskID string) (*database.AsyncTask, error) {
	return s.db.GetAsyncTask(ctx, taskID)
}

func (s *TaskService) UpdateTaskStatus(ctx context.Context, taskID, status string, result map[string]interface{}, errorMessage *string) error {
	task, err := s.db.GetAsyncTask(ctx, taskID)
	if err != nil {
		return err
	}

	task.Status = status
	now := time.Now()

	if status == "running" && task.StartedAt == nil {
		task.StartedAt = &now
	}

	if status == "completed" || status == "failed" || status == "timeout" {
		task.CompletedAt = &now
		if result != nil {
			resultJSON, _ := json.Marshal(result)
			task.Result = string(resultJSON)
			
			// 从结果中提取统计信息
			if aiInput, ok := result["ai_input"].(string); ok {
				task.AIInput = &aiInput
			}
			if aiOutput, ok := result["ai_output"].(string); ok {
				task.AIOutput = &aiOutput
			}
			if tokens, ok := result["input_tokens"].(float64); ok {
				task.InputTokens = int64(tokens)
			}
			if tokens, ok := result["output_tokens"].(float64); ok {
				task.OutputTokens = int64(tokens)
			}
			if ptime, ok := result["processing_time_ms"].(float64); ok {
				task.ProcessingTimeMs = int64(ptime)
			}
			if key, ok := result["used_api_key"].(string); ok {
				task.UsedAPIKey = &key
			}
		}
		if errorMessage != nil {
			task.ErrorMessage = errorMessage
		}
	}

	return s.db.UpdateAsyncTask(ctx, task)
}

func (s *TaskService) GetPendingTasks(ctx context.Context, limit int) ([]*database.AsyncTask, error) {
	return s.db.GetPendingAsyncTasks(ctx, limit)
}

func (s *TaskService) RetryTask(ctx context.Context, taskID string) error {
	task, err := s.db.GetAsyncTask(ctx, taskID)
	if err != nil {
		return err
	}

	task.RetryCount++
	task.Status = "pending"
	task.StartedAt = nil
	task.CompletedAt = nil
	task.ErrorMessage = nil

	return s.db.UpdateAsyncTask(ctx, task)
}

func (s *TaskService) CleanupExpiredTasks(ctx context.Context) (int64, error) {
	cutoff := time.Now().AddDate(0, 0, -7) // 清理 7 天前的已完成/失败任务
	return s.db.CleanupExpiredAsyncTasks(ctx, cutoff)
}
