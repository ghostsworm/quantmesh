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

	// TargetTolerance 目標容差（1%），避免浮动精度问题
	TargetTolerance = 0.01
)

// AllocationManagerProvider 按交易對獲取资金分配管理器（由 main 注入，用於多交易對场景）
type AllocationManagerProvider interface {
	GetAllocationManager(exchange, symbol string) *AllocationManager
}

// PlanManager 倉位计划管理器
type PlanManager struct {
	db                         database.Database
	eventBus                   *event.EventBus
	allocationManagerProvider  AllocationManagerProvider
	mu                         sync.RWMutex
}

// NewPlanManager 創建倉位计划管理器
func NewPlanManager(db database.Database, eventBus *event.EventBus, allocationManagerProvider AllocationManagerProvider) *PlanManager {
	return &PlanManager{
		db:                        db,
		eventBus:                  eventBus,
		allocationManagerProvider: allocationManagerProvider,
	}
}

// CreatePlan 創建倉位计划
// 若當前倉位已是目標值则回傳 nil, nil（無需創建）；若已有活跃计划则返回錯误
func (pm *PlanManager) CreatePlan(ctx context.Context, plan *database.PositionPlan) (*database.PositionPlan, error) {
	if plan == nil {
		return nil, fmt.Errorf("plan is nil")
	}
	if plan.TargetAmountUSDT < 0 {
		return nil, fmt.Errorf("目標倉位不能為负數")
	}

	// 检查是否已有活跃计划（同一 exchange:symbol 只能有一個 pending/in_progress）
	existing, err := pm.GetActivePlan(ctx, plan.Exchange, plan.Symbol)
	if err != nil {
		return nil, err
	}
	if existing != nil {
		return nil, fmt.Errorf("該交易對已有進行中的计划 (ID: %d)，请先取消或等待完成", existing.ID)
	}

	// 确定方向
	if plan.InitialAmount > plan.TargetAmountUSDT {
		plan.Direction = PlanDirectionReduce
	} else if plan.InitialAmount < plan.TargetAmountUSDT {
		plan.Direction = PlanDirectionIncrease
	} else {
		// 已是目標值，不需要創建
		return nil, nil
	}

	plan.Status = PlanStatusInProgress
	plan.CurrentAmount = plan.InitialAmount
	plan.CreatedAt = time.Now()
	plan.UpdatedAt = time.Now()

	// 若啟用自动調整资金限制，先保存當前限額再設置為目標值
	if plan.AutoAdjustLimit && pm.allocationManagerProvider != nil {
		am := pm.allocationManagerProvider.GetAllocationManager(plan.Exchange, plan.Symbol)
		if am != nil {
			status := am.GetStatus(plan.Exchange, plan.Symbol)
			if status != nil {
				plan.OriginalLimit = status.MaxAmount
				if err := am.SetMaxAmount(plan.Exchange, plan.Symbol, plan.TargetAmountUSDT); err != nil {
					logger.Warn("⚠️ [倉位计划] 設置资金限制失败: %v", err)
				} else {
					logger.Info("📊 [倉位计划] %s:%s 资金限制已調整為 %.2f USDT（原 %.2f）",
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

// GetActivePlan 獲取指定交易對的活跃计划（pending 或 in_progress）
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

// GetPlan 獲取單個计划
func (pm *PlanManager) GetPlan(ctx context.Context, id int64) (*database.PositionPlan, error) {
	return pm.db.GetPositionPlan(ctx, id)
}

// GetPlans 獲取计划列表
func (pm *PlanManager) GetPlans(ctx context.Context, filter *database.PositionPlanFilter) ([]*database.PositionPlan, error) {
	return pm.db.GetPositionPlans(ctx, filter)
}

// IsTargetReached 判断是否达成目標（含 1% 容差）
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

// CheckPlanProgress 更新當前倉位並检查是否达成目標；达成则標記完成並发送通知
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

	// 达成目標
	now := time.Now()
	plan.Status = PlanStatusCompleted
	plan.CompletedAt = &now
	plan.UpdatedAt = now
	if err := pm.db.UpdatePositionPlan(ctx, plan); err != nil {
		return err
	}

	logger.Info("✅ [倉位计划] %s:%s 已达成目標倉位 %.2f USDT（當前 %.2f）",
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

// CancelPlan 取消计划；restoreLimit 為 true 時恢複原始资金限制
func (pm *PlanManager) CancelPlan(ctx context.Context, id int64, restoreLimit bool) error {
	plan, err := pm.db.GetPositionPlan(ctx, id)
	if err != nil {
		return err
	}
	if plan == nil {
		return fmt.Errorf("计划不存在")
	}
	if plan.Status == PlanStatusCompleted || plan.Status == PlanStatusCancelled {
		return fmt.Errorf("计划已結束，無法取消")
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
				logger.Warn("⚠️ [倉位计划] 恢複资金限制失败: %v", err)
			} else {
				logger.Info("📊 [倉位计划] %s:%s 已恢複资金限制為 %.2f USDT", plan.Exchange, plan.Symbol, plan.OriginalLimit)
			}
		}
	}

	return nil
}

// UpdatePlanCurrentAmount 僅更新计划的當前倉位（供外部在無法調用 CheckPlanProgress 時同步）
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

// UpdatePlan 更新计划（僅允許更新目標金額、通知开关等，且僅限進行中的计划）
func (pm *PlanManager) UpdatePlan(ctx context.Context, plan *database.PositionPlan) error {
	if plan == nil {
		return fmt.Errorf("plan is nil")
	}
	plan.UpdatedAt = time.Now()
	return pm.db.UpdatePositionPlan(ctx, plan)
}
