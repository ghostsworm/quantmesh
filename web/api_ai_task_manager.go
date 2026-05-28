package web

import (
	"fmt"
	"sync"
	"time"

	"quantmesh/ai"
)

// ========== AI 任務管理器（in-memory）==========

// AITaskStatus AI 任務状態
type AITaskStatus string

const (
	TaskStatusPending   AITaskStatus = "pending"
	TaskStatusRunning   AITaskStatus = "running"
	TaskStatusCompleted AITaskStatus = "completed"
	TaskStatusFailed    AITaskStatus = "failed"
)

// AITask AI 任務信息
type AITask struct {
	TaskID    string                     `json:"task_id"`
	Status    AITaskStatus               `json:"status"`
	CreatedAt time.Time                  `json:"created_at"`
	UpdatedAt time.Time                  `json:"updated_at"`
	Result    *ai.GenerateConfigResponse `json:"result,omitempty"`
	Error     string                     `json:"error,omitempty"`
	Progress  int                        `json:"progress"` // 0-100
}

// AITaskManager AI 任務管理器
type AITaskManager struct {
	tasks map[string]*AITask
	mu    sync.RWMutex
}

var aiTaskManager = &AITaskManager{
	tasks: make(map[string]*AITask),
}

// CreateTask 創建新任務
func (m *AITaskManager) CreateTask() *AITask {
	m.mu.Lock()
	defer m.mu.Unlock()

	taskID := fmt.Sprintf("task_%d", time.Now().UnixNano())
	task := &AITask{
		TaskID:    taskID,
		Status:    TaskStatusPending,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Progress:  0,
	}
	m.tasks[taskID] = task
	return task
}

// GetTask 獲取任務
func (m *AITaskManager) GetTask(taskID string) (*AITask, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	task, ok := m.tasks[taskID]
	return task, ok
}

// UpdateTask 更新任務状態
func (m *AITaskManager) UpdateTask(taskID string, status AITaskStatus, result *ai.GenerateConfigResponse, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if task, ok := m.tasks[taskID]; ok {
		task.Status = status
		task.UpdatedAt = time.Now()
		if result != nil {
			task.Result = result
			task.Progress = 100
		}
		if err != nil {
			task.Error = err.Error()
		}
		if status == TaskStatusRunning {
			task.Progress = 50 // 运行中設置為 50%
		}
	}
}

// CleanupOldTasks 清理舊任務（超過1小時）
func (m *AITaskManager) CleanupOldTasks() {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	for taskID, task := range m.tasks {
		if now.Sub(task.CreatedAt) > time.Hour {
			delete(m.tasks, taskID)
		}
	}
}
