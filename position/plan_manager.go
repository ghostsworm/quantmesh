package position

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"quantmesh/database"
	"quantmesh/event"
	"quantmesh/logger"
)

const (
	PlanStatusPending    = "pending"
	PlanStatusInProgress = "in_progress"
	PlanStatusCompleted  = "completed"
	PlanStatusCancelled  = "cancelled"

	PlanDirectionReduce  = "reduce"
	PlanDirectionIncrease = "increase"

	// TargetTolerance 目标容差（1%），避免浮动精度问题
	TargetTolerance = 0.01
)

// AllocationManagerProvider 按交易对获取资金分配管理器（由 main 注入，用于多交易对场景）
type AllocationManagerProvider interface {
	GetAllocationManager(exchange, symbol string) *AllocationManager
}

// PlanManager 仓位计划管理器
type PlanManager struct {
	db                         database.Database
	eventBus                   *event.EventBus
	allocationManagerProvider  AllocationManagerProvider
	mu                         sync.RWMutex
}

// NewPlanManager 创建仓位计划管理器
func NewPlanManager(db database.Database, eventBus *event.EventBus, allocationManagerProvider AllocationManagerProvider) *PlanManager {
	return &PlanManager{
		db:                        db,
		eventBus:                  eventBus,
		allocationManagerProvider: allocationManagerProvider,
	}
}

// CreatePlan 创建仓位计划
// 若当前仓位已是目标值则返回 nil, nil（无需创建）；若已有活跃计划则返回错误
func (pm *PlanManager) CreatePlan(ctx context.Context, plan *database.PositionPlan) (*database.PositionPlan, error) {
	if plan == nil {
		return nil, fmt.Errorf("plan is nil")
	}
	if plan.TargetAmountUSDT < 0 {
		return nil, fmt.Errorf("目标仓位不能为负数")
	}

	// 检查是否已有活跃计划（同一 exchange:symbol 只能有一个 pending/in_progress）
	existing, err := pm.GetActivePlan(ctx, plan.Exchange, plan.Symbol)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, fmt.Errorf("该交易对已有进行中的计划 (ID: %d)，请先取消或等待完成", existing.ID)
	}

	// 确定方向
	if plan.InitialAmount > plan.TargetAmountUSDT {
		plan.Direction = PlanDirectionReduce
	} else if plan.InitialAmount < plan.TargetAmountUSDT {
		plan.Direction = PlanDirectionIncrease
	} else {
		// 已是目标值，不需要创建
		return nil, nil
	}

	plan.Status = PlanStatusInProgress
	plan.CurrentAmount = plan.InitialAmount
	plan.CreatedAt = time.Now()
	plan.UpdatedAt = time.Now()

	// 若启用自动调整资金限制，先保存当前限额再设置为目标值
	if plan.AutoAdjustLimit && pm.allocationManagerProvider != nil {
		am := pm.allocationManagerProvider.GetAllocationManager(plan.Exchange, plan.Symbol)
		if am != nil {
			status := am.GetStatus(plan.Exchange, plan.Symbol)
			if status != nil {
				plan.OriginalLimit = status.MaxAmount
				if err := am.SetMaxAmount(plan.Exchange, plan.Symbol, plan.TargetAmountUSDT); err != nil {
					logger.Warn("⚠️ [仓位计划] 设置资金限制失败: %v", err)
				} else {
					logger.Info("📊 [仓位计划] %s:%s 资金限制已调整为 %.2f USDT（原 %.2f）",
						plan.Exchange, plan.Symbol, plan.TargetAmountUSDT, plan.OriginalLimit)
				}
			}
		}
	}

	if err := pm.db.SavePositionPlan(ctx, plan); err != nil {
		return nil, err
	}
	return plan, nil
}

// GetActivePlan 获取指定交易对的活跃计划（pending 或 in_progress）
func (pm *PlanManager) GetActivePlan(ctx context.Context, exchange, symbol string) (*database.PositionPlan, error) {
	plans, err := pm.db.GetPositionPlans(ctx, &database.PositionPlanFilter{
		Exchange: exchange,
		Symbol:   symbol,
		Status:   "",
		Limit:    20,
	})
	if err != nil {
		return nil, err
	}
	for _, p := range plans {
		if p.Status == PlanStatusPending || p.Status == PlanStatusInProgress {
			return p, nil
		}
	}
	return nil, nil
}

// GetPlan 获取单个计划
func (pm *PlanManager) GetPlan(ctx context.Context, id int64) (*database.PositionPlan, error) {
	return pm.db.GetPositionPlan(ctx, id)
}

// GetPlans 获取计划列表
func (pm *PlanManager) GetPlans(ctx context.Context, filter *database.PositionPlanFilter) ([]*database.PositionPlan, error) {
	return pm.db.GetPositionPlans(ctx, filter)
}

// IsTargetReached 判断是否达成目标（含 1% 容差）
func (pm *PlanManager) IsTargetReached(plan *database.PositionPlan) bool {
	if plan == nil {
		return false
	}
	target := plan.TargetAmountUSDT
	current := plan.CurrentAmount

	switch plan.Direction {
	case PlanDirectionReduce:
		return current <= target*(1+TargetTolerance)
	case PlanDirectionIncrease:
		return current >= target*(1-TargetTolerance)
	default:
		return math.Abs(current-target) <= target*TargetTolerance
	}
}

// CheckPlanProgress 更新当前仓位并检查是否达成目标；达成则标记完成并发送通知
func (pm *PlanManager) CheckPlanProgress(ctx context.Context, exchange, symbol string, currentAmountUSDT float64) error {
	plan, err := pm.GetActivePlan(ctx, exchange, symbol)
	if err != nil || plan == nil {
		return err
	}

	pm.mu.Lock()
	plan.CurrentAmount = currentAmountUSDT
	plan.UpdatedAt = time.Now()
	pm.mu.Unlock()

	if err := pm.db.UpdatePositionPlan(ctx, plan); err != nil {
		return err
	}

	if !pm.IsTargetReached(plan) {
		return nil
	}

	// 达成目标
	now := time.Now()
	plan.Status = PlanStatusCompleted
	plan.CompletedAt = &now
	plan.UpdatedAt = now
	if err := pm.db.UpdatePositionPlan(ctx, plan); err != nil {
		return err
	}

	logger.Info("✅ [仓位计划] %s:%s 已达成目标仓位 %.2f USDT（当前 %.2f）",
		exchange, symbol, plan.TargetAmountUSDT, currentAmountUSDT)

	if plan.NotifyOnComplete && pm.eventBus != nil {
		pm.eventBus.Publish(&event.Event{
			Type:      event.EventTypePositionPlanCompleted,
			Timestamp: now,
			Data: map[string]interface{}{
				"exchange": exchange,
				"symbol":   symbol,
				"target":   plan.TargetAmountUSDT,
				"current":  currentAmountUSDT,
				"plan_id":  plan.ID,
			},
		})
	}

	return nil
}

// CancelPlan 取消计划；restoreLimit 为 true 时恢复原始资金限制
func (pm *PlanManager) CancelPlan(ctx context.Context, id int64, restoreLimit bool) error {
	plan, err := pm.db.GetPositionPlan(ctx, id)
	if err != nil {
		return err
	}
	if plan == nil {
		return fmt.Errorf("计划不存在")
	}
	if plan.Status == PlanStatusCompleted || plan.Status == PlanStatusCancelled {
		return fmt.Errorf("计划已结束，无法取消")
	}

	plan.Status = PlanStatusCancelled
	plan.UpdatedAt = time.Now()
	if err := pm.db.UpdatePositionPlan(ctx, plan); err != nil {
		return err
	}

	if restoreLimit && plan.AutoAdjustLimit && plan.OriginalLimit > 0 && pm.allocationManagerProvider != nil {
		am := pm.allocationManagerProvider.GetAllocationManager(plan.Exchange, plan.Symbol)
		if am != nil {
			if err := am.SetMaxAmount(plan.Exchange, plan.Symbol, plan.OriginalLimit); err != nil {
				logger.Warn("⚠️ [仓位计划] 恢复资金限制失败: %v", err)
			} else {
				logger.Info("📊 [仓位计划] %s:%s 已恢复资金限制为 %.2f USDT", plan.Exchange, plan.Symbol, plan.OriginalLimit)
			}
		}
	}

	return nil
}

// UpdatePlanCurrentAmount 仅更新计划的当前仓位（供外部在无法调用 CheckPlanProgress 时同步）
func (pm *PlanManager) UpdatePlanCurrentAmount(ctx context.Context, planID int64, currentAmountUSDT float64) error {
	plan, err := pm.db.GetPositionPlan(ctx, planID)
	if err != nil || plan == nil {
		return err
	}
	if plan.Status != PlanStatusPending && plan.Status != PlanStatusInProgress {
		return nil
	}
	plan.CurrentAmount = currentAmountUSDT
	plan.UpdatedAt = time.Now()
	return pm.db.UpdatePositionPlan(ctx, plan)
}

// UpdatePlan 更新计划（仅允许更新目标金额、通知开关等，且仅限进行中的计划）
func (pm *PlanManager) UpdatePlan(ctx context.Context, plan *database.PositionPlan) error {
	if plan == nil {
		return fmt.Errorf("plan is nil")
	}
	plan.UpdatedAt = time.Now()
	return pm.db.UpdatePositionPlan(ctx, plan)
}
