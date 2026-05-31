package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"quantmesh/database"
)

const RedactedAPIKey = "***redacted***"

var taskSecrets sync.Map

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

	storedRequestData := redactSensitiveRequestData(taskID, requestData)
	requestDataJSON, err := json.Marshal(storedRequestData)
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
		ForgetTaskSecrets(taskID)
		task.CompletedAt = &now
		if result != nil {
			resultJSON, _ := json.Marshal(result)
			task.Result = string(resultJSON)

			// 從結果中提取统计信息
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

// GetStaleRunningTasks 獲取已超過 TimeoutSeconds 仍為 running 的任務（用於超時標記）
func (s *TaskService) GetStaleRunningTasks(ctx context.Context, limit int) ([]*database.AsyncTask, error) {
	tasks, err := s.db.GetAsyncTasks(ctx, &database.AsyncTaskFilter{Status: "running", Limit: limit})
	if err != nil {
		return nil, err
	}
	now := time.Now()
	var stale []*database.AsyncTask
	for _, t := range tasks {
		if t.StartedAt == nil {
			continue
		}
		deadline := t.StartedAt.Add(time.Duration(t.TimeoutSeconds) * time.Second)
		if now.After(deadline) {
			stale = append(stale, t)
		}
	}
	return stale, nil
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
	cutoff := time.Now().AddDate(0, 0, -7) // 清理 7 天前的已完成/失败任務
	return s.db.CleanupExpiredAsyncTasks(ctx, cutoff)
}

// sensitiveKeyNames 需要从持久化请求体中脱敏并存入内存的字段名
var sensitiveKeyNames = []string{"api_key", "gemini_api_key"}

func redactSensitiveRequestData(taskID string, requestData map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(requestData)+1)
	for k, v := range requestData {
		out[k] = v
	}
	for _, name := range sensitiveKeyNames {
		if key, ok := requestData[name].(string); ok && strings.TrimSpace(key) != "" && key != RedactedAPIKey {
			taskSecrets.Store(secretKey(taskID, name), key)
			out[name] = RedactedAPIKey
			out[name+"_ref"] = "memory"
		}
	}
	return out
}

// ResolveTaskAPIKey 还原任务的 API Key：通用 api_key 优先，回退旧 gemini_api_key
func ResolveTaskAPIKey(taskID string) string {
	for _, name := range sensitiveKeyNames {
		if key, ok := taskSecrets.Load(secretKey(taskID, name)); ok {
			if s, ok := key.(string); ok && s != "" {
				return s
			}
		}
	}
	return ""
}

// ResolveTaskGeminiAPIKey 向后兼容别名
func ResolveTaskGeminiAPIKey(taskID string) string {
	if key, ok := taskSecrets.Load(secretKey(taskID, "gemini_api_key")); ok {
		if s, ok := key.(string); ok {
			return s
		}
	}
	return ResolveTaskAPIKey(taskID)
}

func ForgetTaskSecrets(taskID string) {
	for _, name := range sensitiveKeyNames {
		taskSecrets.Delete(secretKey(taskID, name))
	}
}

func secretKey(taskID, name string) string {
	return taskID + ":" + name
}
