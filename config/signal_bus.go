package config

import (
	"sync"
	"time"
)

// SignalBus 信号总线 - 用于策略间通信
type SignalBus struct {
	signals   map[string][]Signal
	mu        sync.RWMutex
	maxSize   int  // 每种类型最多保留的信号数量
	ttl       time.Duration
}

// Signal 策略信号
type Signal struct {
	ID        string                 `yaml:"id" json:"id"`
	Type      string                 `yaml:"type" json:"type"`           // 信号类型：trend_direction, market_state, etc.
	Source    string                 `yaml:"source" json:"source"`       // 信号来源策略ID
	Value     interface{}            `yaml:"value" json:"value"`         // 信号值
	Metadata  map[string]interface{} `yaml:"metadata,omitempty" json:"metadata,omitempty"`
	Timestamp time.Time              `yaml:"timestamp" json:"timestamp"`
}

// NewSignalBus 创建信号总线
func NewSignalBus() *SignalBus {
	return &SignalBus{
		signals: make(map[string][]Signal),
		maxSize: 100,  // 每种类型最多保留100个信号
		ttl:      5 * time.Minute,  // 信号有效期5分钟
	}
}

// Publish 发布信号
func (sb *SignalBus) Publish(signal Signal) {
	sb.mu.Lock()
	defer sb.mu.Unlock()

	signal.Timestamp = time.Now()
	if signal.ID == "" {
		signal.ID = generateSignalID(signal.Type, signal.Source)
	}

	// 添加到对应类型的信号列表
	signals := sb.signals[signal.Type]
	signals = append(signals, signal)

	// 限制数量
	if len(signals) > sb.maxSize {
		signals = signals[len(signals)-sb.maxSize:]
	}

	sb.signals[signal.Type] = signals
}

// Subscribe 订阅信号（返回信号通道）
func (sb *SignalBus) Subscribe(signalType string) <-chan Signal {
	ch := make(chan Signal, 10)

	go func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		defer close(ch)

		for range ticker.C {
			sb.mu.RLock()
			signals := sb.signals[signalType]
			sb.mu.RUnlock()

			// 发送最新的信号
			if len(signals) > 0 {
				latest := signals[len(signals)-1]

				// 检查信号是否过期
				if time.Since(latest.Timestamp) < sb.ttl {
					select {
					case ch <- latest:
					default:
					}
				}
			}
		}
	}()

	return ch
}

// GetLatest 获取最新信号
func (sb *SignalBus) GetLatest(signalType string) (Signal, bool) {
	sb.mu.RLock()
	defer sb.mu.RUnlock()

	signals, ok := sb.signals[signalType]
	if !ok || len(signals) == 0 {
		return Signal{}, false
	}

	latest := signals[len(signals)-1]

	// 检查信号是否过期
	if time.Since(latest.Timestamp) > sb.ttl {
		return Signal{}, false
	}

	return latest, true
}

// GetLatestBySource 获取指定来源的最新信号
func (sb *SignalBus) GetLatestBySource(signalType, source string) (Signal, bool) {
	sb.mu.RLock()
	defer sb.mu.RUnlock()

	signals, ok := sb.signals[signalType]
	if !ok {
		return Signal{}, false
	}

	// 从后往前查找
	for i := len(signals) - 1; i >= 0; i-- {
		if signals[i].Source == source {
			latest := signals[i]

			// 检查信号是否过期
			if time.Since(latest.Timestamp) > sb.ttl {
				continue
			}

			return latest, true
		}
	}

	return Signal{}, false
}

// GetAll 获取所有信号（指定类型）
func (sb *SignalBus) GetAll(signalType string) []Signal {
	sb.mu.RLock()
	defer sb.mu.RUnlock()

	signals, ok := sb.signals[signalType]
	if !ok {
		return []Signal{}
	}

	// 过滤过期信号
	now := time.Now()
	result := make([]Signal, 0)
	for _, s := range signals {
		if now.Sub(s.Timestamp) < sb.ttl {
			result = append(result, s)
		}
	}

	return result
}

// Clear 清除所有信号
func (sb *SignalBus) Clear() {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	sb.signals = make(map[string][]Signal)
}

// generateSignalID 生成信号ID
func generateSignalID(signalType, source string) string {
	return source + "_" + signalType
}

// ============================================================================
// 策略角色定义
// ============================================================================

// StrategyRole 策略角色
type StrategyRole string

const (
	RolePrimary   StrategyRole = "primary"   // 主策略：负责交易
	RoleSignal    StrategyRole = "signal"    // 信号策略：只做判断
	RoleHybrid    StrategyRole = "hybrid"    // 混合策略：包含子策略
	RoleMonitor   StrategyRole = "monitor"   // 监控策略：只监控不交易
)

// ============================================================================
// 信号条件定义
// ============================================================================

// SignalCondition 信号条件
type SignalCondition struct {
	SourceStrategy string                 `yaml:"source_strategy" json:"source_strategy"` // 信号来源策略
	SignalType     string                 `yaml:"signal_type" json:"signal_type"`         // 信号类型
	Operator       string                 `yaml:"operator" json:"operator"`               // "==", "!=", ">", "<", ">=", "<=", "in", "not_in"
	Value          interface{}            `yaml:"value" json:"value"`                   // 比较值
	Within         *time.Duration        `yaml:"within,omitempty" json:"within,omitempty"` // 信号时效：只考虑最近N时间内的信号
}

// Evaluate 评估条件
func (sc *SignalCondition) Evaluate(bus *SignalBus) (bool, error) {
	var signal Signal
	var ok bool

	// 获取信号
	if sc.SourceStrategy != "" {
		signal, ok = bus.GetLatestBySource(sc.SignalType, sc.SourceStrategy)
	} else {
		signal, ok = bus.GetLatest(sc.SignalType)
	}

	if !ok {
		return false, nil
	}

	// 检查时效
	if sc.Within != nil {
		if time.Since(signal.Timestamp) > *sc.Within {
			return false, nil
		}
	}

	// 比较值
	return compareValues(signal.Value, sc.Operator, sc.Value)
}

// compareValues 比较值
func compareValues(actual interface{}, operator string, expected interface{}) (bool, error) {
	switch operator {
	case "==":
		return compareEqual(actual, expected), nil
	case "!=":
		return !compareEqual(actual, expected), nil
	case ">":
		return compareGreaterThan(actual, expected), nil
	case "<":
		return compareLessThan(actual, expected), nil
	case ">=":
		return compareGreaterThanOrEqual(actual, expected), nil
	case "<=":
		return compareLessThanOrEqual(actual, expected), nil
	case "in":
		return compareIn(actual, expected), nil
	case "not_in":
		return !compareIn(actual, expected), nil
	default:
		return false, nil
	}
}

// compareEqual 相等比较
func compareEqual(a, b interface{}) bool {
	// 类型转换
	aFloat, aOk := toFloat64(a)
	bFloat, bOk := toFloat64(b)
	if aOk && bOk {
		return aFloat == bFloat
	}

	// 字符串比较
	aStr, aOk := a.(string)
	bStr, bOk := b.(string)
	if aOk && bOk {
		return aStr == bStr
	}

	// 布尔比较
	aBool, aOk := a.(bool)
	bBool, bOk := b.(bool)
	if aOk && bOk {
		return aBool == bBool
	}

	return false
}

func compareGreaterThan(a, b interface{}) bool {
	aFloat, aOk := toFloat64(a)
	bFloat, bOk := toFloat64(b)
	if aOk && bOk {
		return aFloat > bFloat
	}
	return false
}

func compareLessThan(a, b interface{}) bool {
	aFloat, aOk := toFloat64(a)
	bFloat, bOk := toFloat64(b)
	if aOk && bOk {
		return aFloat < bFloat
	}
	return false
}

func compareGreaterThanOrEqual(a, b interface{}) bool {
	return compareGreaterThan(a, b) || compareEqual(a, b)
}

func compareLessThanOrEqual(a, b interface{}) bool {
	return compareLessThan(a, b) || compareEqual(a, b)
}

func compareIn(a, b interface{}) bool {
	// 检查 a 是否在 b (数组/切片) 中
	bSlice, ok := b.([]interface{})
	if !ok {
		return false
	}

	for _, item := range bSlice {
		if compareEqual(a, item) {
			return true
		}
	}
	return false
}

func toFloat64(v interface{}) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case float32:
		return float64(val), true
	case int:
		return float64(val), true
	case int64:
		return float64(val), true
	case int32:
		return float64(val), true
	default:
		return 0, false
	}
}
