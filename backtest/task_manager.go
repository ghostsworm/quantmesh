package backtest

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"quantmesh/logger"
)

// TaskStore 回测任务存储接口（由 storage.SQLiteStorage 实现）
type TaskStore interface {
	CreateBacktestTask(task *BacktestTask) error
	GetBacktestTask(id string) (*BacktestTask, error)
	ListBacktestTasks(limit, offset int) ([]*BacktestTask, error)
	UpdateBacktestTaskStatus(id, status string, progress int, startedAt, completedAt *time.Time, errMsg, resultPath, reportPath string) error
	DeleteBacktestTask(id string) error
}

// TaskManager 异步回测任务管理器
type TaskManager struct {
	store         TaskStore
	binanceConfig map[string]string
	resultsDir    string
	reportsDir    string
	mu            sync.Mutex
	running       map[string]struct{}
}

// NewTaskManager 创建任务管理器
func NewTaskManager(store TaskStore, binanceConfig map[string]string) *TaskManager {
	return &TaskManager{
		store:         store,
		binanceConfig: binanceConfig,
		resultsDir:    filepath.Join("backtest", "results"),
		reportsDir:    filepath.Join("backtest", "reports"),
		running:       make(map[string]struct{}),
	}
}

// GetStore 返回任务存储（供 API 查询任务列表等）
func (m *TaskManager) GetStore() TaskStore {
	return m.store
}

// CreateAndRun 创建任务并异步执行
func (m *TaskManager) CreateAndRun(task *BacktestTask) error {
	if task.ID == "" {
		task.ID = fmt.Sprintf("bt_%d", time.Now().UnixMilli())
	}
	task.Status = "pending"
	task.Progress = 0
	task.CreatedAt = time.Now()
	if err := m.store.CreateBacktestTask(task); err != nil {
		return err
	}
	go m.RunTask(task.ID)
	return nil
}

// RunTask 执行指定任务（阻塞，应在 goroutine 中调用）
func (m *TaskManager) RunTask(id string) error {
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

	task, err := m.store.GetBacktestTask(id)
	if err != nil || task == nil {
		m.failTask(id, "任务不存在")
		return nil
	}

	now := time.Now()
	_ = m.store.UpdateBacktestTaskStatus(id, "running", 0, &now, nil, "", "", "")

	// 1. 获取 K 线（优先缓存，无则拉取）
	candles, err := GetHistoricalData(task.Symbol, task.Interval, task.StartTime, task.EndTime, m.binanceConfig)
	if err != nil {
		m.failTask(id, fmt.Sprintf("获取历史数据失败: %v", err))
		return nil
	}
	if len(candles) == 0 {
		m.failTask(id, "未获取到历史数据")
		return nil
	}

	// 2. 根据策略类型运行回测
	var result *BacktestResult
	capital := task.TotalCapital
	switch task.Strategy {
	case "grid":
		params := m.gridParamsFromTask(task)
		result, err = RunGridBacktest(task.Symbol, candles, params, capital)
	case "dca":
		params := m.dcaParamsFromTask(task)
		result, err = RunDCABacktest(task.Symbol, task.Interval, candles, params, capital)
	case "martingale":
		params := m.martingaleParamsFromTask(task)
		result, err = RunMartingaleBacktest(task.Symbol, task.Interval, candles, params, capital)
	case "momentum", "mean_reversion", "trend_following":
		var strategy StrategyAdapter
		switch task.Strategy {
		case "momentum":
			strategy = NewMomentumAdapter()
		case "mean_reversion":
			strategy = NewMeanReversionAdapter()
		case "trend_following":
			strategy = NewTrendFollowingAdapter()
		}
		backtester := NewBacktester(task.Symbol, candles, strategy, capital)
		result, err = backtester.Run()
	default:
		m.failTask(id, fmt.Sprintf("不支持的策略: %s", task.Strategy))
		return nil
	}

	if err != nil {
		m.failTask(id, fmt.Sprintf("回测执行失败: %v", err))
		return nil
	}

	// 3. 保存结果 JSON
	if err := os.MkdirAll(m.resultsDir, 0755); err != nil {
		m.failTask(id, fmt.Sprintf("创建结果目录失败: %v", err))
		return nil
	}
	resultPath := filepath.Join(m.resultsDir, id+".json")
	payload := BacktestTaskResult{TaskID: id, Task: task, Result: result}
	body, _ := json.MarshalIndent(payload, "", "  ")
	if err := os.WriteFile(resultPath, body, 0644); err != nil {
		m.failTask(id, fmt.Sprintf("保存结果失败: %v", err))
		return nil
	}

	// 4. 生成报告 Markdown
	if err := os.MkdirAll(m.reportsDir, 0755); err != nil {
		logger.Warn("创建报告目录失败: %v", err)
	}
	reportPath := filepath.Join(m.reportsDir, id+".md")
	if err := GenerateReportToFile(result, reportPath); err != nil {
		logger.Warn("生成报告失败: %v", err)
		reportPath = ""
	}

	// 5. 更新任务为完成
	completed := time.Now()
	_ = m.store.UpdateBacktestTaskStatus(id, "completed", 100, nil, &completed, "", resultPath, reportPath)
	logger.Info("✅ 回测任务完成: %s, 收益率=%.2f%%", id, result.Metrics.TotalReturn)
	return nil
}

func (m *TaskManager) failTask(id, errMsg string) {
	completed := time.Now()
	_ = m.store.UpdateBacktestTaskStatus(id, "failed", 0, nil, &completed, errMsg, "", "")
	logger.Error("❌ 回测任务失败: %s, %s", id, errMsg)
}

func (m *TaskManager) gridParamsFromTask(task *BacktestTask) GridBacktestParams {
	p := GridBacktestParams{
		PriceLow:        getFloat(task.Params, "price_low", 0),
		PriceHigh:       getFloat(task.Params, "price_high", 0),
		GridCount:       getInt(task.Params, "grid_count", 20),
		OrderQuantity:   getFloat(task.Params, "order_quantity", 100),
		TotalCapital:    task.TotalCapital,
		FeeRate:         getFloat(task.Params, "fee_rate", 0.0004),
		SlippageRatio:   0.0003,
	}
	return p
}

func (m *TaskManager) dcaParamsFromTask(task *BacktestTask) DCABacktestParams {
	return DCABacktestParams{
		IntervalDays:   getInt(task.Params, "interval_days", 7),
		AmountPerTrade: getFloat(task.Params, "amount_per_trade", 100),
		TotalCapital:   task.TotalCapital,
		FeeRate:         getFloat(task.Params, "fee_rate", 0.0004),
	}
}

func (m *TaskManager) martingaleParamsFromTask(task *BacktestTask) MartingaleBacktestParams {
	return MartingaleBacktestParams{
		BaseAmount:    getFloat(task.Params, "base_amount", 100),
		Multiplier:     getFloat(task.Params, "multiplier", 2),
		TotalCapital:   task.TotalCapital,
		FeeRate:        getFloat(task.Params, "fee_rate", 0.0004),
		TakeProfitPct: 1,
		StopLossPct:    2,
	}
}

func getFloat(m map[string]interface{}, key string, def float64) float64 {
	if m == nil {
		return def
	}
	v, ok := m[key]
	if !ok {
		return def
	}
	switch t := v.(type) {
	case float64:
		return t
	case int:
		return float64(t)
	case int64:
		return float64(t)
	}
	return def
}

func getInt(m map[string]interface{}, key string, def int) int {
	if m == nil {
		return def
	}
	v, ok := m[key]
	if !ok {
		return def
	}
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case int64:
		return int(t)
	}
	return def
}

// LoadResult 加载任务结果（从 JSON 文件）
func LoadResult(resultsDir, taskID string) (*BacktestTaskResult, error) {
	path := filepath.Join(resultsDir, taskID+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out BacktestTaskResult
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return &out, nil
}
