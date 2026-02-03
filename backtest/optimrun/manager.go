package optimrun

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"quantmesh/backtest"
	"quantmesh/backtest/optimizer"
	"quantmesh/logger"
)

// OptimTaskManager 参数优化任务管理器
type OptimTaskManager struct {
	store         backtest.OptimTaskStore
	binanceConfig map[string]string
	resultsDir    string
	mu            sync.Mutex
	running       map[string]struct{}
}

// NewOptimTaskManager 创建优化任务管理器
func NewOptimTaskManager(store backtest.OptimTaskStore, binanceConfig map[string]string) *OptimTaskManager {
	return &OptimTaskManager{
		store:         store,
		binanceConfig: binanceConfig,
		resultsDir:    filepath.Join("backtest", "optim_results"),
		running:       make(map[string]struct{}),
	}
}

// GetStore 返回任务存储
func (m *OptimTaskManager) GetStore() backtest.OptimTaskStore {
	return m.store
}

// CreateAndRun 创建任务并异步执行
func (m *OptimTaskManager) CreateAndRun(task *backtest.OptimTask) error {
	if task.ID == "" {
		task.ID = fmt.Sprintf("opt_%d", time.Now().UnixMilli())
	}
	task.Status = "pending"
	task.Progress = 0
	task.CompletedCombos = 0
	task.CreatedAt = time.Now()

	// 计算总组合数
	opt := &optimizer.UniversalOptimizer{}
	space := toOptimizerSearchSpace(task.SearchSpace)
	task.TotalCombos = len(opt.EnumerateParamCombos(space))
	if task.TotalCombos <= 0 {
		return fmt.Errorf("搜索空间为空，请检查参数范围")
	}

	if err := m.store.CreateOptimTask(task); err != nil {
		return err
	}
	go m.RunTask(task.ID)
	return nil
}

// RunTask 执行指定任务
func (m *OptimTaskManager) RunTask(id string) error {
	m.mu.Lock()
	if _, ok := m.running[id]; ok {
		m.mu.Unlock()
		return fmt.Errorf("task %s already running", id)
	}
	m.running[id] = struct{}{}
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.running, id)
		m.mu.Unlock()
	}()

	task, err := m.store.GetOptimTask(id)
	if err != nil || task == nil {
		m.failTask(id, "任务不存在")
		return nil
	}

	now := time.Now()
	_ = m.store.UpdateOptimTaskStatus(id, "running", &now, nil, "", "")

	// 获取 K 线数据
	candles, err := backtest.GetHistoricalData(task.Symbol, task.Interval, task.StartTime, task.EndTime, m.binanceConfig)
	if err != nil {
		m.failTask(id, fmt.Sprintf("获取历史数据失败: %v", err))
		return nil
	}
	if len(candles) == 0 {
		m.failTask(id, "未获取到历史数据")
		return nil
	}

	// 执行优化
	opt := &optimizer.UniversalOptimizer{}
	space := toOptimizerSearchSpace(task.SearchSpace)
	onProgress := func(completed, total int) {
		progress := 0
		if total > 0 {
			progress = completed * 100 / total
		}
		_ = m.store.UpdateOptimTaskProgress(id, completed, progress)
	}

	ctx := context.Background()
	result, err := opt.Run(ctx, id, task.Symbol, task.Interval, candles, space, task.TotalCapital, onProgress)
	if err != nil {
		m.failTask(id, fmt.Sprintf("优化执行失败: %v", err))
		return nil
	}

	// 保存结果
	if err := os.MkdirAll(m.resultsDir, 0755); err != nil {
		m.failTask(id, fmt.Sprintf("创建结果目录失败: %v", err))
		return nil
	}
	resultPath := filepath.Join(m.resultsDir, id+".json")
	body, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		m.failTask(id, fmt.Sprintf("编码结果失败: %v", err))
		return nil
	}
	if err := os.WriteFile(resultPath, body, 0644); err != nil {
		m.failTask(id, fmt.Sprintf("保存结果失败: %v", err))
		return nil
	}

	completed := time.Now()
	_ = m.store.UpdateOptimTaskStatus(id, "completed", nil, &completed, "", resultPath)
	bestRet := 0.0
	if result.BestByReturn != nil {
		bestRet = result.BestByReturn.TotalReturn
	}
	logger.Info("✅ 参数优化任务完成: %s, 策略=%s, 完成%d/%d组, 最佳收益率=%.2f%%",
		id, task.Strategy, result.Completed, result.TotalCombos, bestRet)
	return nil
}

func (m *OptimTaskManager) failTask(id, errMsg string) {
	completed := time.Now()
	_ = m.store.UpdateOptimTaskStatus(id, "failed", nil, &completed, errMsg, "")
	logger.Error("❌ 参数优化任务失败: %s, %s", id, errMsg)
}

// toOptimizerSearchSpace 转换 OptimSearchSpace 为 optimizer.UniversalSearchSpace
func toOptimizerSearchSpace(s backtest.OptimSearchSpace) optimizer.UniversalSearchSpace {
	space := optimizer.UniversalSearchSpace{
		Strategy: s.Strategy,
		Ranges:   make(map[string]optimizer.ParamRange),
	}
	for k, v := range s.Ranges {
		space.Ranges[k] = optimizer.ParamRange{Min: v.Min, Max: v.Max, Step: v.Step}
	}
	return space
}

// LoadOptimResult 加载优化结果
func LoadOptimResult(resultsDir, taskID string) (*optimizer.UniversalOptimResult, error) {
	path := filepath.Join(resultsDir, taskID+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out optimizer.UniversalOptimResult
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
