package risk

import (
	"context"
	"errors"
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
	EmergencyActionCancelAllOrders   EmergencyAction = "cancel_all_orders"   // 撤销所有挂单
	EmergencyActionCloseAllPositions EmergencyAction = "close_all_positions" // 平掉所有仓位
	EmergencyActionPauseAll          EmergencyAction = "pause_all"           // 暂停所有Bot开仓
	EmergencyActionReducePosition    EmergencyAction = "reduce_position"     // 减仓（50%）
	EmergencyActionEmergencyMode     EmergencyAction = "emergency_mode"      // 进入紧急模式
)

const emergencyCenterPauseSource = "emergency_center"

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
	"market_crash":  EmergencyScenarioMarketCrash,
	"api_failure":   EmergencyScenarioAPIFailure,
	"large_loss":    EmergencyScenarioLargeLoss,
	"network_issue": EmergencyScenarioNetworkIssue,
	"full_shutdown": EmergencyScenarioFullShutdown,
}

// EmergencyOperation 紧急操作记录
type EmergencyOperation struct {
	ID                string            `json:"id"`
	Scenario          string            `json:"scenario"`
	Actions           []EmergencyAction `json:"actions"`
	TriggeredBy       string            `json:"triggered_by"`
	Reason            string            `json:"reason"`
	Timestamp         time.Time         `json:"timestamp"`
	Status            string            `json:"status"` // executing, completed, failed, rolled_back
	Results           map[string]string `json:"results"`
	Error             string            `json:"error,omitempty"`
	RollbackAvailable bool              `json:"rollback_available"`
}

// EmergencyCenter 紧急操作中心
type EmergencyCenter struct {
	config          *config.EmergencyCenterConfig
	statusMu        sync.RWMutex
	eventBus        *event.EventBus
	botProvider     BotProvider
	scenarios       map[string]*EmergencyScenario
	operations      []*EmergencyOperation
	pauser          *OpeningPauseCoordinator
	operationsMu    sync.RWMutex
	emergencyMode   bool
	emergencyModeMu sync.RWMutex
}

// SetPauseCoordinator connects emergency pauses to shared risk-source ownership.
func (ec *EmergencyCenter) SetPauseCoordinator(pauser *OpeningPauseCoordinator) {
	ec.pauser = pauser
}

// NewEmergencyCenter 创建紧急操作中心
func NewEmergencyCenter(cfg *config.EmergencyCenterConfig, eventBus *event.EventBus, botProvider BotProvider) *EmergencyCenter {
	// 复制默认场景表，避免加载自定义场景时污染包级全局 map
	scenarios := make(map[string]*EmergencyScenario, len(DefaultEmergencyScenarios))
	for name, s := range DefaultEmergencyScenarios {
		scenarios[name] = s
	}
	ec := &EmergencyCenter{
		config:      cfg,
		eventBus:    eventBus,
		botProvider: botProvider,
		scenarios:   scenarios,
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
		ID:                fmt.Sprintf("op_%d", time.Now().UnixNano()),
		Scenario:          scenarioName,
		Actions:           scenario.Actions,
		TriggeredBy:       triggeredBy,
		Reason:            reason,
		Timestamp:         time.Now(),
		Status:            "executing",
		Results:           make(map[string]string),
		RollbackAvailable: true,
	}

	ec.operationsMu.Lock()
	ec.operations = append(ec.operations, op)
	snapshot := op.clone()
	ec.operationsMu.Unlock()

	// 异步执行
	go ec.executeOperation(op, scenario)

	return snapshot, nil
}

// RequiresConfirmation 是否要求执行前显式确认（config.require_confirmation）
func (ec *EmergencyCenter) RequiresConfirmation() bool {
	return ec.config != nil && ec.config.RequireConfirmation
}

// ValidateConfirmation 校验执行确认：启用 require_confirmation 时，
// 请求必须携带 confirm=true 且 confirm_scenario 与场景名完全一致（防误点/误传场景）。
func (ec *EmergencyCenter) ValidateConfirmation(scenarioName string, confirmed bool, confirmScenario string) error {
	if !ec.RequiresConfirmation() {
		return nil
	}
	if !confirmed {
		return fmt.Errorf("场景 %s 需要确认: 请求须携带 confirm=true", scenarioName)
	}
	if confirmScenario != scenarioName {
		return fmt.Errorf("场景 %s 确认不匹配: confirm_scenario=%q", scenarioName, confirmScenario)
	}
	return nil
}

// clone 复制操作记录（避免调用方与执行协程并发读写）
func (op *EmergencyOperation) clone() *EmergencyOperation {
	cp := *op
	cp.Actions = append([]EmergencyAction(nil), op.Actions...)
	cp.Results = make(map[string]string, len(op.Results))
	for k, v := range op.Results {
		cp.Results[k] = v
	}
	return &cp
}

// executeOperation 执行操作：逐个动作执行并汇总每个 Bot 的错误，不因单个动作失败中断后续保护动作
func (ec *EmergencyCenter) executeOperation(op *EmergencyOperation, scenario *EmergencyScenario) {
	logger.Info("⚡ [紧急中心] 开始执行操作 %s...", op.ID)

	bots := ec.botProvider.GetAllBots()
	ctx := context.Background()

	var errs []error
	anySuccess := false
	anyFailure := false
	for _, action := range scenario.Actions {
		result, err := ec.executeAction(ctx, action, scenario.CloseMethod, scenario.Timeout, bots)
		ec.operationsMu.Lock()
		op.Results[string(action)] = result
		ec.operationsMu.Unlock()

		if err != nil {
			anyFailure = true
			errs = append(errs, fmt.Errorf("%s: %w", action, err))
			logger.Error("❌ [紧急中心] 动作失败: %s, 结果: %s, 错误: %v", action, result, err)
			if !isTotalFailure(err) {
				anySuccess = true
			}
			continue
		}
		anySuccess = true
	}

	status := OperationStatusCompleted
	switch {
	case anyFailure && anySuccess:
		status = OperationStatusPartial
	case anyFailure:
		status = OperationStatusFailed
	}

	ec.operationsMu.Lock()
	op.Status = status
	if len(errs) > 0 {
		op.Error = errors.Join(errs...).Error()
	}
	snapshot := op.clone()
	ec.operationsMu.Unlock()

	if status == OperationStatusCompleted {
		logger.Info("✅ [紧急中心] 操作 %s 执行完成", op.ID)
	} else {
		logger.Error("❌ [紧急中心] 操作 %s 执行结束，状态: %s，错误: %s", op.ID, status, snapshot.Error)
	}
	ec.publishOperationEvent(snapshot)
}

// partialFailureError 部分 Bot 失败（至少一个成功）
type partialFailureError struct{ err error }

func (e *partialFailureError) Error() string { return e.err.Error() }
func (e *partialFailureError) Unwrap() error { return e.err }

// isTotalFailure 动作是否整体失败（非部分失败）
func isTotalFailure(err error) bool {
	var pe *partialFailureError
	return !errors.As(err, &pe)
}

// reportResult 把 Bot 执行汇总转成 (结果, 错误)；部分失败包装为 partialFailureError
func reportResult(report BotActionReport, verb string) (string, error) {
	summary := report.Summary(verb)
	err := report.Err()
	if err == nil {
		return summary, nil
	}
	if report.Succeeded > 0 {
		return summary, &partialFailureError{err: err}
	}
	return summary, err
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
	if ec.pauser != nil {
		ec.pauser.Pause(emergencyCenterPauseSource, "紧急停止", bots)
		successCount = len(bots)
	} else {
		for _, bot := range bots {
			pauseBotWithoutAutoResume(bot, "紧急停止")
			successCount++
		}
	}
	return fmt.Sprintf("已停止 %d 个Bot", successCount), nil
}

// cancelAllOrders 撤销所有挂单
func (ec *EmergencyCenter) cancelAllOrders(bots []BotController) (string, error) {
	return reportResult(cancelOrdersOnBots(bots), "已撤销挂单")
}

// closeAllPositions 平掉所有仓位（每个 Bot 独立超时；timeout<=0 使用 DefaultBotActionTimeout）
func (ec *EmergencyCenter) closeAllPositions(ctx context.Context, bots []BotController, method string, timeout int) (string, error) {
	return reportResult(closePositionsOnBots(ctx, bots, method, timeout), "已平仓")
}

// pauseAllBots 暂停所有Bot开仓
func (ec *EmergencyCenter) pauseAllBots(bots []BotController) (string, error) {
	successCount := 0
	if ec.pauser != nil {
		ec.pauser.Pause(emergencyCenterPauseSource, "紧急暂停", bots)
		successCount = len(bots)
	} else {
		for _, bot := range bots {
			pauseBotWithoutAutoResume(bot, "紧急暂停")
			successCount++
		}
	}
	return fmt.Sprintf("已暂停 %d 个Bot开仓", successCount), nil
}

// reducePositions 减仓保护。
// 当前 BotController 尚未暴露“按比例减仓”能力；在大额亏损场景下，宁可保守全平，也不能返回“减仓成功”的假安全感。
func (ec *EmergencyCenter) reducePositions(ctx context.Context, bots []BotController, method string, timeout int) (string, error) {
	result, err := ec.closeAllPositions(ctx, bots, method, timeout)
	return "减仓接口未实现，已执行全平保护：" + result, err
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
	modeEnabled := ec.emergencyMode
	if !modeEnabled && (ec.pauser == nil || !ec.pauser.IsHeldBy(emergencyCenterPauseSource)) {
		ec.emergencyModeMu.Unlock()
		return fmt.Errorf("当前未处于紧急模式")
	}
	ec.emergencyMode = false
	ec.emergencyModeMu.Unlock()
	logger.Info("✅ [紧急中心] 已解除紧急暂停/退出紧急模式，操作人: %s", triggeredBy)
	if ec.pauser != nil && ec.pauser.IsHeldBy(emergencyCenterPauseSource) {
		var bots []BotController
		if ec.botProvider != nil {
			bots = ec.botProvider.GetAllBots()
		}
		ec.pauser.Release(emergencyCenterPauseSource, bots)
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

	out := make([]*EmergencyOperation, 0, len(ec.operations)-start)
	for _, op := range ec.operations[start:] {
		out = append(out, op.clone())
	}
	return out
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
