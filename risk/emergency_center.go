package risk

import (
	"context"
	"fmt"
	"sync"
	"time"

	"quantmesh/config"
	"quantmesh/event"
	"quantmesh/logger"
)

// EmergencyAction 紧急操作类型
type EmergencyAction string

const (
	EmergencyActionStopAll           EmergencyAction = "stop_all"            // 停止所有Bot
	EmergencyActionCancelAllOrders   EmergencyAction = "cancel_all_orders"  // 撤销所有挂单
	EmergencyActionCloseAllPositions EmergencyAction = "close_all_positions" // 平掉所有仓位
	EmergencyActionPauseAll          EmergencyAction = "pause_all"          // 暂停所有Bot开仓
	EmergencyActionReducePosition    EmergencyAction = "reduce_position"    // 减仓（50%）
	EmergencyActionEmergencyMode     EmergencyAction = "emergency_mode"     // 进入紧急模式
)

// EmergencyScenario 预定义紧急场景
type EmergencyScenario struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Actions     []EmergencyAction `json:"actions"`
	CloseMethod string            `json:"close_method"` // market/limit
	Timeout     int               `json:"timeout"`      // 秒
}

// 预定义场景
var (
	EmergencyScenarioMarketCrash = &EmergencyScenario{
		Name:        "market_crash",
		Description: "市场崩盘 - 停止交易、撤销挂单、市价平仓",
		Actions:     []EmergencyAction{EmergencyActionStopAll, EmergencyActionCancelAllOrders, EmergencyActionCloseAllPositions},
		CloseMethod: "market",
		Timeout:     30,
	}

	EmergencyScenarioAPIFailure = &EmergencyScenario{
		Name:        "api_failure",
		Description: "API故障 - 暂停开仓、撤销挂单",
		Actions:     []EmergencyAction{EmergencyActionPauseAll, EmergencyActionCancelAllOrders},
		CloseMethod: "",
		Timeout:     0,
	}

	EmergencyScenarioLargeLoss = &EmergencyScenario{
		Name:        "large_loss",
		Description: "大额亏损 - 暂停开仓、减仓50%",
		Actions:     []EmergencyAction{EmergencyActionPauseAll, EmergencyActionReducePosition},
		CloseMethod: "limit",
		Timeout:     60,
	}

	EmergencyScenarioNetworkIssue = &EmergencyScenario{
		Name:        "network_issue",
		Description: "网络问题 - 暂停开仓",
		Actions:     []EmergencyAction{EmergencyActionPauseAll},
		CloseMethod: "",
		Timeout:     0,
	}

	EmergencyScenarioFullShutdown = &EmergencyScenario{
		Name:        "full_shutdown",
		Description: "完全关闭 - 停止所有、撤销挂单、平仓",
		Actions:     []EmergencyAction{EmergencyActionStopAll, EmergencyActionCancelAllOrders, EmergencyActionCloseAllPositions},
		CloseMethod: "market",
		Timeout:     30,
	}
)

// DefaultEmergencyScenarios 默认紧急场景列表
var DefaultEmergencyScenarios = map[string]*EmergencyScenario{
	"market_crash":   EmergencyScenarioMarketCrash,
	"api_failure":    EmergencyScenarioAPIFailure,
	"large_loss":     EmergencyScenarioLargeLoss,
	"network_issue":  EmergencyScenarioNetworkIssue,
	"full_shutdown":  EmergencyScenarioFullShutdown,
}

// EmergencyOperation 紧急操作记录
type EmergencyOperation struct {
	ID          string            `json:"id"`
	Scenario    string            `json:"scenario"`
	Actions     []EmergencyAction `json:"actions"`
	TriggeredBy  string            `json:"triggered_by"`
	Reason      string            `json:"reason"`
	Timestamp   time.Time         `json:"timestamp"`
	Status      string            `json:"status"` // executing, completed, failed, rolled_back
	Results     map[string]string `json:"results"`
	Error       string            `json:"error,omitempty"`
	RollbackAvailable bool        `json:"rollback_available"`
}

// EmergencyCenter 紧急操作中心
type EmergencyCenter struct {
	config         *config.EmergencyCenterConfig
	statusMu       sync.RWMutex
	eventBus       *event.EventBus
	botProvider    BotProvider
	scenarios      map[string]*EmergencyScenario
	operations     []*EmergencyOperation
	operationsMu   sync.RWMutex
	emergencyMode  bool
	emergencyModeMu sync.RWMutex
}

// NewEmergencyCenter 创建紧急操作中心
func NewEmergencyCenter(cfg *config.EmergencyCenterConfig, eventBus *event.EventBus, botProvider BotProvider) *EmergencyCenter {
	ec := &EmergencyCenter{
		config:      cfg,
		eventBus:    eventBus,
		botProvider: botProvider,
		scenarios:   DefaultEmergencyScenarios,
		operations:  make([]*EmergencyOperation, 0, 100),
	}

	// 加载自定义场景
	ec.loadCustomScenarios()

	return ec
}

// loadCustomScenarios 加载自定义场景
func (ec *EmergencyCenter) loadCustomScenarios() {
	if ec.config == nil || !ec.config.Enabled {
		return
	}

	for name, scenarioCfg := range ec.config.CustomScenarios {
		scenario := &EmergencyScenario{
			Name:        name,
			Description: scenarioCfg.Description,
			CloseMethod: scenarioCfg.CloseMethod,
			Timeout:     scenarioCfg.Timeout,
		}

		for _, actionStr := range scenarioCfg.Actions {
			scenario.Actions = append(scenario.Actions, EmergencyAction(actionStr))
		}

		ec.scenarios[name] = scenario
		logger.Info("📋 [紧急中心] 加载自定义场景: %s", name)
	}
}

// ExecuteScenario 执行预定义场景
func (ec *EmergencyCenter) ExecuteScenario(scenarioName, triggeredBy, reason string) (*EmergencyOperation, error) {
	scenario, ok := ec.scenarios[scenarioName]
	if !ok {
		return nil, fmt.Errorf("场景不存在: %s", scenarioName)
	}

	logger.Warn("🚨 [紧急中心] 执行紧急场景: %s, 操作人: %s, 原因: %s", scenarioName, triggeredBy, reason)

	// 创建操作记录
	op := &EmergencyOperation{
		ID:        fmt.Sprintf("op_%d", time.Now().UnixNano()),
		Scenario:  scenarioName,
		Actions:   scenario.Actions,
		TriggeredBy: triggeredBy,
		Reason:    reason,
		Timestamp: time.Now(),
		Status:    "executing",
		Results:   make(map[string]string),
		RollbackAvailable: true,
	}

	ec.operationsMu.Lock()
	ec.operations = append(ec.operations, op)
	ec.operationsMu.Unlock()

	// 异步执行
	go ec.executeOperation(op, scenario)

	return op, nil
}

// executeOperation 执行操作
func (ec *EmergencyCenter) executeOperation(op *EmergencyOperation, scenario *EmergencyScenario) {
	logger.Info("⚡ [紧急中心] 开始执行操作 %s...", op.ID)

	bots := ec.botProvider.GetAllBots()
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(scenario.Timeout)*time.Second)
	defer cancel()

	for _, action := range scenario.Actions {
		result, err := ec.executeAction(ctx, action, scenario.CloseMethod, scenario.Timeout, bots)
		if err != nil {
			op.Status = "failed"
			op.Error = err.Error()
			logger.Error("❌ [紧急中心] 操作失败: %s, 错误: %v", action, err)
			ec.publishOperationEvent(op)
			return
		}
		op.Results[string(action)] = result
	}

	op.Status = "completed"
	logger.Info("✅ [紧急中心] 操作 %s 执行完成", op.ID)
	ec.publishOperationEvent(op)
}

// executeAction 执行单个动作
func (ec *EmergencyCenter) executeAction(ctx context.Context, action EmergencyAction, closeMethod string, timeout int, bots []BotController) (string, error) {
	switch action {
	case EmergencyActionStopAll:
		return ec.stopAllBots(bots)
	case EmergencyActionCancelAllOrders:
		return ec.cancelAllOrders(bots)
	case EmergencyActionCloseAllPositions:
		return ec.closeAllPositions(ctx, bots, closeMethod, timeout)
	case EmergencyActionPauseAll:
		return ec.pauseAllBots(bots)
	case EmergencyActionReducePosition:
		return ec.reducePositions(ctx, bots, closeMethod, timeout)
	case EmergencyActionEmergencyMode:
		return ec.enableEmergencyMode()
	default:
		return "", fmt.Errorf("未知操作: %s", action)
	}
}

// stopAllBots 停止所有Bot
func (ec *EmergencyCenter) stopAllBots(bots []BotController) (string, error) {
	successCount := 0
	for _, bot := range bots {
		bot.PauseOpening("紧急停止")
		successCount++
	}
	return fmt.Sprintf("已停止 %d 个Bot", successCount), nil
}

// cancelAllOrders 撤销所有挂单
func (ec *EmergencyCenter) cancelAllOrders(bots []BotController) (string, error) {
	successCount := 0
	for _, bot := range bots {
		if err := bot.CancelAllOpenOrders(); err != nil {
			logger.Warn("⚠️ [紧急中心] Bot撤单失败: %v", err)
		} else {
			successCount++
		}
	}
	return fmt.Sprintf("已撤销 %d/%d 个Bot的挂单", successCount, len(bots)), nil
}

// closeAllPositions 平掉所有仓位
func (ec *EmergencyCenter) closeAllPositions(ctx context.Context, bots []BotController, method string, timeout int) (string, error) {
	successCount := 0
	for _, bot := range bots {
		if err := bot.CloseAllPositions(ctx, method, timeout); err != nil {
			logger.Warn("⚠️ [紧急中心] Bot平仓失败: %v", err)
		} else {
			successCount++
		}
	}
	return fmt.Sprintf("已平仓 %d/%d 个Bot", successCount, len(bots)), nil
}

// pauseAllBots 暂停所有Bot开仓
func (ec *EmergencyCenter) pauseAllBots(bots []BotController) (string, error) {
	successCount := 0
	for _, bot := range bots {
		bot.PauseOpening("紧急暂停")
		successCount++
	}
	return fmt.Sprintf("已暂停 %d 个Bot开仓", successCount), nil
}

// reducePositions 减仓50%
func (ec *EmergencyCenter) reducePositions(ctx context.Context, bots []BotController, method string, timeout int) (string, error) {
	// TODO: 实现减仓逻辑（需要获取当前仓位并平掉50%）
	return "减仓功能待实现", nil
}

// enableEmergencyMode 启用紧急模式
func (ec *EmergencyCenter) enableEmergencyMode() (string, error) {
	ec.emergencyModeMu.Lock()
	ec.emergencyMode = true
	ec.emergencyModeMu.Unlock()
	logger.Warn("🚨 [紧急中心] 已进入紧急模式")
	return "已进入紧急模式", nil
}

// DisableEmergencyMode 禁用紧急模式
func (ec *EmergencyCenter) DisableEmergencyMode(triggeredBy string) error {
	ec.emergencyModeMu.Lock()
	defer ec.emergencyModeMu.Unlock()

	if !ec.emergencyMode {
		return fmt.Errorf("当前未处于紧急模式")
	}

	ec.emergencyMode = false
	logger.Info("✅ [紧急中心] 已退出紧急模式，操作人: %s", triggeredBy)

	// 恢复所有Bot
	bots := ec.botProvider.GetAllBots()
	for _, bot := range bots {
		bot.ResumeOpening()
	}

	return nil
}

// IsEmergencyMode 是否处于紧急模式
func (ec *EmergencyCenter) IsEmergencyMode() bool {
	ec.emergencyModeMu.RLock()
	defer ec.emergencyModeMu.RUnlock()
	return ec.emergencyMode
}

// GetOperations 获取操作历史
func (ec *EmergencyCenter) GetOperations(limit int) []*EmergencyOperation {
	ec.operationsMu.RLock()
	defer ec.operationsMu.RUnlock()

	if limit <= 0 || limit > len(ec.operations) {
		limit = len(ec.operations)
	}

	start := len(ec.operations) - limit
	if start < 0 {
		start = 0
	}

	return ec.operations[start:]
}

// GetScenarios 获取所有场景
func (ec *EmergencyCenter) GetScenarios() map[string]*EmergencyScenario {
	return ec.scenarios
}

// publishOperationEvent 发布操作事件
func (ec *EmergencyCenter) publishOperationEvent(op *EmergencyOperation) {
	if ec.eventBus == nil {
		return
	}

	ec.eventBus.Publish(&event.Event{
		Type: event.EventType("emergency:operation"),
		Data: map[string]interface{}{
			"operation": op,
		},
	})
}
