package backtest

import (
	"encoding/json"
	"time"
)

// OptimParamRange 参数取值范围（避免 import cycle）
type OptimParamRange struct {
	Min  float64 `json:"min"`
	Max  float64 `json:"max"`
	Step float64 `json:"step"`
}

// OptimSearchSpace 参数搜索空间（避免 import cycle）
type OptimSearchSpace struct {
	Strategy string                     `json:"strategy"`
	Ranges   map[string]OptimParamRange `json:"ranges"`
}

// OptimTaskStore 优化任务存储接口
type OptimTaskStore interface {
	CreateOptimTask(task *OptimTask) error
	GetOptimTask(id string) (*OptimTask, error)
	ListOptimTasks(limit, offset int) ([]*OptimTask, error)
	UpdateOptimTaskProgress(id string, completed int, progress int) error
	UpdateOptimTaskStatus(id, status string, startedAt, completedAt *time.Time, errMsg, resultPath string) error
	DeleteOptimTask(id string) error
}

// OptimTask 参数优化任务
type OptimTask struct {
	ID              string           `json:"id"`
	Status          string           `json:"status"` // pending, running, completed, failed
	Strategy        string           `json:"strategy"`
	Symbol          string           `json:"symbol"`
	Interval        string           `json:"interval"`
	StartTime       time.Time        `json:"start_time"`
	EndTime         time.Time        `json:"end_time"`
	TotalCapital    float64          `json:"total_capital"`
	SearchSpace     OptimSearchSpace `json:"search_space"`
	Progress        int              `json:"progress"`         // 0-100
	TotalCombos     int              `json:"total_combos"`     // 总组合数
	CompletedCombos int              `json:"completed_combos"` // 已完成数
	CreatedAt       time.Time        `json:"created_at"`
	StartedAt       *time.Time       `json:"started_at,omitempty"`
	CompletedAt     *time.Time       `json:"completed_at,omitempty"`
	ResultPath      string           `json:"result_path,omitempty"`
	Error           string           `json:"error,omitempty"`
}

// SearchSpaceToJSON 将 SearchSpace 序列化为 JSON 字符串
func (t *OptimTask) SearchSpaceToJSON() (string, error) {
	data, err := json.Marshal(t.SearchSpace)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// SearchSpaceFromJSON 从 JSON 字符串反序列化 SearchSpace
func (t *OptimTask) SearchSpaceFromJSON(s string) error {
	return json.Unmarshal([]byte(s), &t.SearchSpace)
}
