package backtest

import (
	"fmt"
	"sync"
	"time"
)

// BacktestSignalBus 回测信号总线
// 用于在回测过程中实现策略间的信号通信和协作规则
type BacktestSignalBus struct {
	mu      sync.RWMutex
	signals map[string][]*BacktestSignal // key: signal_type, value: signals
	rules   []TaskCollaborationRule
	enabled bool
}

// BacktestSignal 回测信号
type BacktestSignal struct {
	ID        string                 `json:"id"`
	Type      string                 `json:"type"`       // signal_type
	Source    string                 `json:"source"`     // source_strategy_id
	Value     interface{}            `json:"value"`
	Metadata  map[string]interface{} `json:"metadata,omitempty"`
	Timestamp int64                  `json:"timestamp"` // K线时间戳
	KlineIndex int                   `json:"kline_index"` // K线索引
}

// NewBacktestSignalBus 创建回测信号总线
func NewBacktestSignalBus(rules []TaskCollaborationRule) *BacktestSignalBus {
	return &BacktestSignalBus{
		signals: make(map[string][]*BacktestSignal),
		rules:   rules,
		enabled: len(rules) > 0,
	}
}

// Publish 发布信号
func (bus *BacktestSignalBus) Publish(signal *BacktestSignal) {
	if !bus.enabled {
		return
	}

	bus.mu.Lock()
	defer bus.mu.Unlock()

	if bus.signals == nil {
		bus.signals = make(map[string][]*BacktestSignal)
	}

	// 设置时间戳
	if signal.Timestamp == 0 {
		signal.Timestamp = time.Now().UnixMilli()
	}

	// 添加到对应类型的信号列表
	bus.signals[signal.Type] = append(bus.signals[signal.Type], signal)
}

// GetLatest 获取最新信号
func (bus *BacktestSignalBus) GetLatest(signalType string) *BacktestSignal {
	bus.mu.RLock()
	defer bus.mu.RUnlock()

	signals := bus.signals[signalType]
	if len(signals) == 0 {
		return nil
	}

	return signals[len(signals)-1]
}

// GetLatestBySource 获取指定来源的最新信号
func (bus *BacktestSignalBus) GetLatestBySource(signalType, source string) *BacktestSignal {
	bus.mu.RLock()
	defer bus.mu.RUnlock()

	signals := bus.signals[signalType]
	for i := len(signals) - 1; i >= 0; i-- {
		if signals[i].Source == source {
			return signals[i]
		}
	}

	return nil
}

// EvaluateRules 评估协作规则
// 返回: map[target_strategy_id][]actions
func (bus *BacktestSignalBus) EvaluateRules() map[string][]RuleAction {
	if !bus.enabled {
		return nil
	}

	bus.mu.RLock()
	defer bus.mu.RUnlock()

	actions := make(map[string][]RuleAction)

	for _, rule := range bus.rules {
		if !rule.Enabled {
			continue
		}

		// 检查条件
		if matched := bus.evaluateCondition(rule.When); matched {
			// 执行动作
			for _, action := range rule.Then {
				targetID := action.TargetStrategy
				actions[targetID] = append(actions[targetID], RuleAction{
					TargetStrategy: targetID,
					Operation:      action.Operation,
					Condition:      action.Condition,
					Params:         action.Params,
					RuleID:         rule.ID,
					RuleName:       rule.Name,
					RulePriority:   rule.Priority,
				})
			}
		}
	}

	return actions
}

// evaluateCondition 评估单个条件
func (bus *BacktestSignalBus) evaluateCondition(condition TaskSignalCondition) bool {
	signal := bus.GetLatestBySource(condition.SignalType, condition.SourceStrategy)
	if signal == nil {
		return false
	}

	return bus.compareValues(signal.Value, condition.Operator, condition.Value)
}

// compareValues 比较值
func (bus *BacktestSignalBus) compareValues(actual interface{}, operator string, expected interface{}) bool {
	switch operator {
	case "==", "eq":
		return bus.compareEqual(actual, expected)
	case "!=", "ne":
		return !bus.compareEqual(actual, expected)
	case ">", "gt":
		return bus.compareGreater(actual, expected)
	case ">=", "gte":
		return bus.compareGreater(actual, expected) || bus.compareEqual(actual, expected)
	case "<", "lt":
		return bus.compareLess(actual, expected)
	case "<=", "lte":
		return bus.compareLess(actual, expected) || bus.compareEqual(actual, expected)
	case "in":
		return bus.compareIn(actual, expected)
	case "not_in":
		return !bus.compareIn(actual, expected)
	default:
		return false
	}
}

// compareEqual 相等比较
func (bus *BacktestSignalBus) compareEqual(a, b interface{}) bool {
	return fmt.Sprintf("%v", a) == fmt.Sprintf("%v", b)
}

// compareGreater 大于比较
func (bus *BacktestSignalBus) compareGreater(a, b interface{}) bool {
	af, okA := a.(float64)
	bf, okB := b.(float64)
	if okA && okB {
		return af > bf
	}

	ai, okA2 := a.(int)
	bi, okB2 := b.(int)
	if okA2 && okB2 {
		return ai > bi
	}

	return false
}

// compareLess 小于比较
func (bus *BacktestSignalBus) compareLess(a, b interface{}) bool {
	af, okA := a.(float64)
	bf, okB := b.(float64)
	if okA && okB {
		return af < bf
	}

	ai, okA2 := a.(int)
	bi, okB2 := b.(int)
	if okA2 && okB2 {
		return ai < bi
	}

	return false
}

// compareIn 包含比较
func (bus *BacktestSignalBus) compareIn(a, b interface{}) bool {
	switch v := b.(type) {
	case []interface{}:
		for _, item := range v {
			if bus.compareEqual(a, item) {
				return true
			}
		}
	case []string:
		strA := fmt.Sprintf("%v", a)
		for _, item := range v {
			if strA == item {
				return true
			}
		}
	}
	return false
}

// RuleAction 规则动作
type RuleAction struct {
	TargetStrategy string                 `json:"target_strategy"`
	Operation      string                 `json:"operation"`
	Condition      string                 `json:"condition,omitempty"`
	Params         map[string]interface{} `json:"params,omitempty"`
	RuleID         string                 `json:"rule_id"`
	RuleName       string                 `json:"rule_name"`
	RulePriority   int                    `json:"rule_priority"`
}

// Clear 清空信号（用于新的回测）
func (bus *BacktestSignalBus) Clear() {
	bus.mu.Lock()
	defer bus.mu.Unlock()

	bus.signals = make(map[string][]*BacktestSignal)
}

// GetSignalCount 获取信号数量
func (bus *BacktestSignalBus) GetSignalCount() int {
	bus.mu.RLock()
	defer bus.mu.RUnlock()

	count := 0
	for _, signals := range bus.signals {
		count += len(signals)
	}

	return count
}

// IsEnabled 检查是否启用
func (bus *BacktestSignalBus) IsEnabled() bool {
	return bus.enabled
}
