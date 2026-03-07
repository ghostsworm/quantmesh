package config

import (
	"fmt"
	"sync"
	"time"
)

// ============================================================================
// 混合策略配置
// ============================================================================

// HybridStrategyConfig 混合策略配置
type HybridStrategyConfig struct {
	Name                 string                     `yaml:"name" json:"name"`
	Description          string                     `yaml:"description,omitempty" json:"description,omitempty"`
	SubStrategies        []SubStrategyConfig        `yaml:"sub_strategies" json:"sub_strategies"`
	CollaborationRules   []CollaborationRule        `yaml:"collaboration_rules" json:"collaboration_rules"`
	GlobalSettings       map[string]interface{}     `yaml:"global_settings,omitempty" json:"global_settings,omitempty"`
}

// SubStrategyConfig 子策略配置
type SubStrategyConfig struct {
	ID          string                 `yaml:"id" json:"id"`
	Name        string                 `yaml:"name" json:"name"`
	Type        string                 `yaml:"type" json:"type"`               // 策略类型
	Role        StrategyRole           `yaml:"role" json:"role"`              // 策略角色
	Weight      float64                `yaml:"weight" json:"weight"`          // 资金权重（仅主策略使用）
	Config      map[string]interface{} `yaml:"config" json:"config"`          // 策略配置
	Enabled     bool                   `yaml:"enabled" json:"enabled"`        // 是否启用
	Metadata    map[string]interface{} `yaml:"metadata,omitempty" json:"metadata,omitempty"`
}

// CollaborationRule 协作规则
type CollaborationRule struct {
	ID          string            `yaml:"id" json:"id"`
	Name        string            `yaml:"name" json:"name"`
	Description string            `yaml:"description,omitempty" json:"description,omitempty"`
	Priority    int               `yaml:"priority,omitempty" json:"priority,omitempty"` // 优先级，数字越小优先级越高
	Enabled     bool              `yaml:"enabled" json:"enabled"`
	When        SignalCondition  `yaml:"when" json:"when"`              // 触发条件
	Then        []Action          `yaml:"then" json:"then"`             // 执行动作
}

// Action 执行动作
type Action struct {
	TargetStrategy string                 `yaml:"target_strategy" json:"target_strategy"` // 目标策略
	Operation      string                 `yaml:"operation" json:"operation"`             // 操作类型
	Condition      string                 `yaml:"condition,omitempty" json:"condition,omitempty"` // 额外条件
	Params         map[string]interface{} `yaml:"params,omitempty" json:"params,omitempty"`         // 参数
}

// 操作类型常量
const (
	ActionAllowOpen      = "allow_open"       // 允许开仓
	ActionDenyOpen       = "deny_open"        // 拒绝开仓
	ActionAllowClose     = "allow_close"      // 允许平仓
	ActionDenyClose      = "deny_close"       // 拒绝平仓
	ActionModifyParams   = "modify_params"    // 修改参数
	ActionEnableStrategy = "enable_strategy"  // 启用策略
	ActionDisableStrategy = "disable_strategy" // 禁用策略
	ActionEmitSignal     = "emit_signal"      // 发送信号
)

// ============================================================================
// 混合策略引擎
// ============================================================================

// HybridStrategyEngine 混合策略引擎
type HybridStrategyEngine struct {
	config    *HybridStrategyConfig
	signalBus *SignalBus
	strategies map[string]*StrategyInstance
	mu        sync.RWMutex
}

// NewHybridStrategyEngine 创建混合策略引擎
func NewHybridStrategyEngine(config *HybridStrategyConfig) *HybridStrategyEngine {
	return &HybridStrategyEngine{
		config:     config,
		signalBus:  NewSignalBus(),
		strategies: make(map[string]*StrategyInstance),
	}
}

// Initialize 初始化引擎
func (hse *HybridStrategyEngine) Initialize() error {
	hse.mu.Lock()
	defer hse.mu.Unlock()

	// 初始化子策略
	for _, subConfig := range hse.config.SubStrategies {
		if !subConfig.Enabled {
			continue
		}

		// 创建策略实例
		strategy := &StrategyInstance{
			Type:   subConfig.Type,
			Weight: subConfig.Weight,
			Config: subConfig.Config,
		}

		hse.strategies[subConfig.ID] = strategy
	}

	return nil
}

// EvaluateCollaborationRules 评估协作规则
func (hse *HybridStrategyEngine) EvaluateCollaborationRules(targetStrategyID string, operation string) (bool, []Action, error) {
	hse.mu.RLock()
	defer hse.mu.RUnlock()

	// 按优先级排序规则
	rules := hse.sortRulesByPriority()

	var applicableActions []Action

	for _, rule := range rules {
		if !rule.Enabled {
			continue
		}

		// 检查目标策略是否匹配
		if !hse.isRuleApplicable(rule, targetStrategyID, operation) {
			continue
		}

		// 评估触发条件
		shouldTrigger, err := rule.When.Evaluate(hse.signalBus)
		if err != nil {
			return false, nil, fmt.Errorf("评估规则 %s 失败: %w", rule.ID, err)
		}

		if shouldTrigger {
			// 收集动作
			applicableActions = append(applicableActions, rule.Then...)
		}
	}

	// 如果有匹配的规则，返回第一个匹配的结果
	if len(applicableActions) > 0 {
		return true, applicableActions, nil
	}

	return false, nil, nil
}

// GetSubStrategy 获取子策略
func (hse *HybridStrategyEngine) GetSubStrategy(id string) (*SubStrategyConfig, bool) {
	hse.mu.RLock()
	defer hse.mu.RUnlock()

	for _, subConfig := range hse.config.SubStrategies {
		if subConfig.ID == id {
			return &subConfig, true
		}
	}

	return nil, false
}

// ListSubStrategies 列出所有子策略
func (hse *HybridStrategyEngine) ListSubStrategies() []SubStrategyConfig {
	hse.mu.RLock()
	defer hse.mu.RUnlock()

	result := make([]SubStrategyConfig, 0, len(hse.config.SubStrategies))
	result = append(result, hse.config.SubStrategies...)
	return result
}

// GetSignalBus 获取信号总线
func (hse *HybridStrategyEngine) GetSignalBus() *SignalBus {
	return hse.signalBus
}

// sortRulesByPriority 按优先级排序规则
func (hse *HybridStrategyEngine) sortRulesByPriority() []CollaborationRule {
	rules := make([]CollaborationRule, len(hse.config.CollaborationRules))
	copy(rules, hse.config.CollaborationRules)

	// 按优先级排序（数字越小优先级越高）
	for i := 0; i < len(rules)-1; i++ {
		for j := i + 1; j < len(rules); j++ {
			if rules[j].Priority < rules[i].Priority {
				rules[i], rules[j] = rules[j], rules[i]
			}
		}
	}

	return rules
}

// isRuleApplicable 检查规则是否适用于目标策略和操作
func (hse *HybridStrategyEngine) isRuleApplicable(rule CollaborationRule, targetStrategyID, operation string) bool {
	for _, action := range rule.Then {
		// 检查目标策略
		if action.TargetStrategy != "" && action.TargetStrategy != targetStrategyID {
			continue
		}

		// 检查操作类型
		if operation != "" {
			switch operation {
			case "open":
				if action.Operation == ActionAllowOpen || action.Operation == ActionDenyOpen {
					return true
				}
			case "close":
				if action.Operation == ActionAllowClose || action.Operation == ActionDenyClose {
					return true
				}
			}
		} else {
			// 没有指定操作，检查是否有任何匹配
			return true
		}
	}

	return false
}

// ============================================================================
// 策略执行上下文
// ============================================================================

// StrategyExecutionContext 策略执行上下文
type StrategyExecutionContext struct {
	BotID        string
	StrategyID   string
	Operation    string  // "open", "close", "modify"
	Direction    string  // "LONG", "SHORT"
	Price        float64
	Quantity     float64
	Metadata     map[string]interface{}
	Timestamp    time.Time
}

// ShouldAllowOperation 判断是否允许执行操作
func (hse *HybridStrategyEngine) ShouldAllowOperation(ctx StrategyExecutionContext) (bool, []Action, error) {
	hasRules, actions, err := hse.EvaluateCollaborationRules(ctx.StrategyID, ctx.Operation)
	if err != nil {
		return false, nil, err
	}

	if !hasRules {
		// 没有匹配的规则，默认允许
		return true, nil, nil
	}

	// 检查动作
	for _, action := range actions {
		// 检查额外条件
		if action.Condition != "" {
			// TODO: 实现条件解析
			continue
		}

		// 根据操作类型返回结果
		switch ctx.Operation {
		case "open":
			if action.Operation == ActionDenyOpen {
				return false, actions, nil
			} else if action.Operation == ActionAllowOpen {
				return true, actions, nil
			}
		case "close":
			if action.Operation == ActionDenyClose {
				return false, actions, nil
			} else if action.Operation == ActionAllowClose {
				return true, actions, nil
			}
		}
	}

	// 默认允许
	return true, actions, nil
}

// ============================================================================
// 预定义的协作规则模板
// ============================================================================

// GetBuiltInRuleTemplates 获取内置规则模板
func GetBuiltInRuleTemplates() []CollaborationRule {
	return []CollaborationRule{
		{
			ID:          "trend_filter_long",
			Name:        "趋势过滤-做多",
			Description: "当趋势向下时，阻止做多开仓",
			Priority:    100,
			Enabled:     true,
			When: SignalCondition{
				SourceStrategy: "trend_following",
				SignalType:     "trend_direction",
				Operator:       "==",
				Value:          "down",
			},
			Then: []Action{
				{
					TargetStrategy: "grid",
					Operation:      ActionDenyOpen,
					Condition:      "direction == 'LONG'",
				},
			},
		},
		{
			ID:          "trend_accelerate_up",
			Name:        "趋势加速-向上",
			Description: "当强势向上趋势时，扩大网格间距",
			Priority:    200,
			Enabled:     true,
			When: SignalCondition{
				SourceStrategy: "trend_following",
				SignalType:     "trend_strength",
				Operator:       ">",
				Value:          0.7,
			},
			Then: []Action{
				{
					TargetStrategy: "grid",
					Operation:      ActionModifyParams,
					Params: map[string]interface{}{
						"price_interval_multiplier": 1.5,
					},
				},
			},
		},
		{
			ID:          "volatility_reduce_size",
			Name:        "波动率控制-减小规模",
			Description: "当波动率过高时，减小订单规模",
			Priority:    150,
			Enabled:     true,
			When: SignalCondition{
				SourceStrategy: "volatility_monitor",
				SignalType:     "volatility_index",
				Operator:       ">",
				Value:          2.0,
			},
			Then: []Action{
				{
					TargetStrategy: "grid",
					Operation:      ActionModifyParams,
					Params: map[string]interface{}{
						"order_quantity_multiplier": 0.7,
					},
				},
			},
		},
		{
			ID:          "market_state_shutdown",
			Name:        "市场状态-紧急平仓",
			Description: "当检测到极端市场状态时，平仓并暂停交易",
			Priority:    50,  // 最高优先级
			Enabled:     true,
			When: SignalCondition{
				SourceStrategy: "market_monitor",
				SignalType:     "market_state",
				Operator:       "==",
				Value:          "extreme",
			},
			Then: []Action{
				{
					TargetStrategy: "grid",
					Operation:      ActionDenyOpen,
				},
				{
					TargetStrategy: "grid",
					Operation:      ActionAllowClose,
				},
			},
		},
	}
}
