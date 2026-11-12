package optimrun

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"quantmesh/backtest"
	"quantmesh/backtest/optimizer"
	"quantmesh/backtest/visionfeed"
	"quantmesh/exchange"
	"quantmesh/logger"
)

// OptimTaskManager 参数优化任务管理器
type OptimTaskManager struct {
	store        backtest.OptimTaskStore
	loadData     HistoricalDataLoader
	visionRoot   string
	replayVision replayVisionLoader
	resultsDir   string
	mu           sync.Mutex
	running      map[string]*taskRun
	deleting     map[string]bool
}

// HistoricalDataLoader lets offline callers use isolated fixtures without accounts or network access.
type HistoricalDataLoader func(context.Context, string, string, time.Time, time.Time) ([]*exchange.Candle, error)

type ManagerOption func(*OptimTaskManager)

func WithHistoricalDataLoader(loader HistoricalDataLoader) ManagerOption {
	return func(m *OptimTaskManager) { m.loadData = loader }
}

func WithResultsDir(dir string) ManagerOption {
	return func(m *OptimTaskManager) { m.resultsDir = dir }
}

// WithVisionRoot selects the local directory of checksum-verified Binance
// USD-M 1m files. A blank value keeps the default gitignored directory.
func WithVisionRoot(root string) ManagerOption {
	return func(m *OptimTaskManager) {
		if strings.TrimSpace(root) == "" {
			m.visionRoot = visionfeed.DefaultRoot
			return
		}
		m.visionRoot = root
	}
}

// WithReplayVisionLoader replaces the verified-data reader. Tests use it to
// avoid real market files; production uses visionfeed.Load.
func WithReplayVisionLoader(loader replayVisionLoader) ManagerOption {
	return func(m *OptimTaskManager) { m.replayVision = loader }
}

// NewOptimTaskManager 创建优化任务管理器；options must be applied before any run.
func NewOptimTaskManager(store backtest.OptimTaskStore, binanceConfig map[string]string, options ...ManagerOption) *OptimTaskManager {
	m := &OptimTaskManager{
		store: store,
		loadData: func(ctx context.Context, symbol, interval string, start, end time.Time) ([]*exchange.Candle, error) {
			return backtest.GetHistoricalDataContext(ctx, symbol, interval, start, end, binanceConfig)
		},
		visionRoot:   visionfeed.DefaultRoot,
		replayVision: visionfeed.Load,
		resultsDir:   filepath.Join("backtest", "optim_results"),
		running:      make(map[string]*taskRun),
		deleting:     make(map[string]bool),
	}
	for _, option := range options {
		option(m)
	}
	return m
}

// ReadResult reads both replay and legacy JSON results from this manager's directory.
func (m *OptimTaskManager) ReadResult(id string) ([]byte, error) {
	if !IsValidTaskID(id) {
		return nil, fmt.Errorf("invalid optimizer task id")
	}
	return os.ReadFile(filepath.Join(m.resultsDir, id+".json"))
}

// GetStore 返回任务存储
func (m *OptimTaskManager) GetStore() backtest.OptimTaskStore {
	return m.store
}

// CreateAndRun 创建任务并异步执行
func (m *OptimTaskManager) CreateAndRun(task *backtest.OptimTask) error {
	total, err := ValidateTask(task)
	if err != nil {
		return err
	}
	if m.store == nil || m.loadData == nil {
		return fmt.Errorf("optimizer storage or historical data loader is unavailable")
	}
	if task.ID == "" {
		var randomID [16]byte
		if _, err := rand.Read(randomID[:]); err != nil {
			return err
		}
		task.ID = fmt.Sprintf("opt_%x", randomID)
	}
	task.Status = "pending"
	task.Progress = 0
	task.CompletedCombos = 0
	task.CreatedAt = time.Now()

	// Replay includes candidates, training baseline, test winner and test baseline per fold.
	task.TotalCombos = total

	run, err := m.beginRun(task.ID)
	if err != nil {
		return err
	}
	// The slot is held, so a missing window must release it before anything is stored.
	if task.SearchSpace.ReplayWalkForward != nil {
		if err := m.preflightReplayData(task); err != nil {
			m.finishRun(task.ID, run)
			return err
		}
	}
	if err := m.store.CreateOptimTask(task); err != nil {
		m.finishRun(task.ID, run)
		return err
	}
	id := task.ID
	go func() {
		if err := m.executeRun(id, run); err != nil {
			logger.Error("optimizer task %s ended: %v", id, err)
		}
	}()
	return nil
}

// RunTask 执行指定任务
func (m *OptimTaskManager) RunTask(id string) error {
	run, err := m.beginRun(id)
	if err != nil {
		return err
	}
	return m.executeRun(id, run)
}

func (m *OptimTaskManager) runTask(ctx context.Context, id string) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	task, err := m.store.GetOptimTask(id)
	if err != nil || task == nil {
		m.failTask(id, "任务不存在")
		return nil
	}

	total, err := ValidateTask(task)
	if err != nil {
		m.failTask(id, err.Error())
		return nil
	}
	if task.SearchSpace.ReplayWalkForward != nil && task.TotalCombos != total {
		m.failTask(id, "persisted replay total does not match the requested experiment")
		return nil
	}
	now := time.Now()
	if err := m.store.UpdateOptimTaskStatus(id, "running", &now, nil, "", ""); err != nil {
		return fmt.Errorf("mark optimizer task %s running: %w", id, err)
	}

	var candles []*exchange.Candle
	var provenance optimizer.ReplayProvenance
	if task.SearchSpace.ReplayWalkForward != nil {
		candles, provenance, err = m.loadReplayData(ctx, task)
	} else {
		candles, err = m.loadData(ctx, task.Symbol, task.Interval, task.StartTime, task.EndTime)
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		m.failTask(id, err.Error())
		return nil
	}
	if len(candles) == 0 {
		m.failTask(id, "未获取到历史数据")
		return nil
	}

	// 执行优化
	opt := &optimizer.UniversalOptimizer{}
	if provenance.Source != "" {
		optimizer.WithReplayProvenance(provenance)(opt)
	}
	space := toOptimizerSearchSpace(task.SearchSpace)
	applyReplayFunding(&space, provenance)
	runCtx, stop := context.WithCancelCause(ctx)
	defer stop(nil)
	onProgress := func(completed, total int) {
		if total != task.TotalCombos || completed < 0 || completed > total {
			stop(fmt.Errorf("optimizer progress %d/%d differs from persisted total %d", completed, total, task.TotalCombos))
			return
		}
		progress := 0
		if total > 0 {
			progress = completed * 100 / total
		}
		if err := m.store.UpdateOptimTaskProgress(id, completed, progress); err != nil {
			stop(fmt.Errorf("persist optimizer task %s progress: %w", id, err))
		}
	}

	result, err := opt.Run(runCtx, id, task.Symbol, task.Interval, candles, space, task.TotalCapital, onProgress)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if cause := context.Cause(runCtx); cause != nil {
		err = cause
	}
	if err != nil {
		m.failTask(id, fmt.Sprintf("优化执行失败: %v", err))
		return nil
	}

	// 写入任务上下文，供前端结果弹窗标题等展示
	result.Symbol = task.Symbol
	result.Interval = task.Interval
	if result.ReplayWalkForward == nil {
		result.StartTime = task.StartTime
		result.EndTime = task.EndTime
	}
	result.UseOrderDepth = false // 当前参数优化未接入订单深度
	result.RiskInfo = "无"
	if space.Ranges != nil {
		if _, ok := space.Ranges["risk_volume_multiplier"]; ok {
			result.RiskInfo = "成交量风控"
		}
		if _, ok := space.Ranges["risk_average_window"]; ok {
			result.RiskInfo = "成交量风控"
		}
	}

	// 保存结果
	if err := ctx.Err(); err != nil {
		return err
	}
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
	if err := m.store.UpdateOptimTaskStatus(id, "completed", nil, &completed, "", resultPath); err != nil {
		return fmt.Errorf("mark optimizer task %s completed: %w", id, err)
	}
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
	if err := m.store.UpdateOptimTaskStatus(id, "failed", nil, &completed, errMsg, ""); err != nil {
		logger.Error("persist failed optimizer task %s: %v", id, err)
	}
	logger.Error("❌ 参数优化任务失败: %s, %s", id, errMsg)
}

// toOptimizerSearchSpace 转换 OptimSearchSpace 为 optimizer.UniversalSearchSpace
func toOptimizerSearchSpace(s backtest.OptimSearchSpace) optimizer.UniversalSearchSpace {
	space := optimizer.UniversalSearchSpace{
		Strategy:          s.Strategy,
		Ranges:            make(map[string]optimizer.ParamRange),
		ReplayWalkForward: s.ReplayWalkForward,
	}
	for k, v := range s.Ranges {
		space.Ranges[k] = optimizer.ParamRange{Min: v.Min, Max: v.Max, Step: v.Step}
	}
	return space
}

// LoadOptimResult 加载优化结果
func LoadOptimResult(resultsDir, taskID string) (*optimizer.UniversalOptimResult, error) {
	if !IsValidTaskID(taskID) {
		return nil, fmt.Errorf("invalid optimizer task id")
	}
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
