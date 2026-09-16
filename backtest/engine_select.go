package backtest

import (
	"fmt"
	"strings"

	"quantmesh/exchange"
)

// 網格回測引擎選擇（task.Params["engine"]）
const (
	// ParamKeyEngine 任務參數中的引擎鍵
	ParamKeyEngine = "engine"
	// EngineLegacy 舊 K 線觸價成交引擎（預設）
	EngineLegacy = "legacy"
	// EngineReplay 實盤同構回放引擎：驅動真實 position.SuperPositionManager（見 backtest/replay）
	EngineReplay = "replay"
)

// ReplayTaskRunner 回放引擎任務執行函數。
// backtest 包不能 import position（position → storage → backtest 循環），
// 因此回放引擎位於子包 backtest/replay，由上層（web）通過 TaskManager.SetReplayRunner 注入。
// 返回的 extra 為引擎專有指標（寫入結果 JSON 的 replay_metrics）。
type ReplayTaskRunner func(task *BacktestTask, candles []*exchange.Candle) (result *BacktestResult, extra interface{}, err error)

// NormalizeEngine 歸一化引擎名；空為 legacy，未知值返回錯誤
func NormalizeEngine(engine string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(engine)) {
	case "", EngineLegacy:
		return EngineLegacy, nil
	case EngineReplay:
		return EngineReplay, nil
	default:
		return "", fmt.Errorf("unsupported backtest engine %q (want %q or %q)", engine, EngineLegacy, EngineReplay)
	}
}

// taskEngine 讀取任務引擎（非字符串或未知值按錯誤處理）
func taskEngine(task *BacktestTask) (string, error) {
	if task == nil || task.Params == nil {
		return EngineLegacy, nil
	}
	raw, ok := task.Params[ParamKeyEngine]
	if !ok || raw == nil {
		return EngineLegacy, nil
	}
	s, ok := raw.(string)
	if !ok {
		return "", fmt.Errorf("task %s: params.%s must be a string, got %T", task.ID, ParamKeyEngine, raw)
	}
	return NormalizeEngine(s)
}

// SetReplayRunner 注入回放引擎執行函數（nil 表示不支持 replay 引擎）
func (m *TaskManager) SetReplayRunner(fn ReplayTaskRunner) {
	m.mu.Lock()
	m.replayRunner = fn
	m.mu.Unlock()
}

func (m *TaskManager) getReplayRunner() ReplayTaskRunner {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.replayRunner
}
