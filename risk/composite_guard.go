package risk

import (
	"sync"
	"time"

	"quantmesh/event"
	"quantmesh/logger"
)

// compositeRiskPauseSource 复合风控在暂停协调器中的来源名
const compositeRiskPauseSource = "composite_risk"

// CompositeRiskSignal 复合风控一次评估映射出的动作信号
type CompositeRiskSignal int

const (
	CompositeRiskHold    CompositeRiskSignal = iota // 维持当前状态（滞回区间，如 pause_buying）
	CompositeRiskStop                               // 达到 stop_trading：暂停所有 Bot 开仓
	CompositeRiskRelease                            // 回落到 reduce_position 及以下：解除本来源的暂停
)

// CompositeRiskGuard 把复合风控结果接到与熔断器相同的暂停开仓路径（审查报告 C4）。
// 使用 OpeningPauseCoordinator，解除时不会覆盖熔断器仍持有的暂停。
type CompositeRiskGuard struct {
	bots     BotProvider
	pauser   *OpeningPauseCoordinator
	notifier AlertNotifier

	mu     sync.Mutex
	paused bool
}

// NewCompositeRiskGuard 创建复合风控守卫；notifier 可为 nil
func NewCompositeRiskGuard(bots BotProvider, pauser *OpeningPauseCoordinator, notifier AlertNotifier) *CompositeRiskGuard {
	if pauser == nil {
		pauser = NewOpeningPauseCoordinator()
	}
	return &CompositeRiskGuard{
		bots:     bots,
		pauser:   pauser,
		notifier: notifier,
		paused:   pauser.IsHeldBy(compositeRiskPauseSource),
	}
}

// Apply 根据信号暂停/解除开仓；返回当前是否由复合风控暂停
func (g *CompositeRiskGuard) Apply(signal CompositeRiskSignal, level string, score float64, reasons []string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()

	switch signal {
	case CompositeRiskStop:
		if g.paused {
			return true
		}
		bots := g.bots.GetAllBots()
		reason := compositeRiskPauseSource + ":" + level
		if err := g.pauser.Pause(compositeRiskPauseSource, reason, bots); err != nil {
			logger.Error("[复合风控] 暂停已施加，但持久化风险来源失败，需保持人工核查: %v", err)
		}
		g.paused = true
		logger.Error("🛑 [复合风控] 综合分 %.1f 达到 %s，已暂停 %d 个 Bot 开仓，原因: %v", score, level, len(bots), reasons)
		g.notify(event.EventTypeRiskTriggered, level, score, reasons)
	case CompositeRiskRelease:
		if !g.paused {
			return false
		}
		bots := g.bots.GetAllBots()
		resumed, err := g.pauser.ReleaseChecked(compositeRiskPauseSource, bots)
		if err != nil {
			logger.Error("[复合风控] 持久化解除失败，继续保留本地暂停来源: %v", err)
			return true
		}
		g.paused = false
		logger.Info("▶️ [复合风控] 综合分回落至 %.1f (%s)，解除复合风控暂停，实际恢复=%v", score, level, resumed)
		g.notify(event.EventTypeRiskRecovered, level, score, reasons)
	}
	return g.paused
}

// IsPaused 当前是否由复合风控暂停
func (g *CompositeRiskGuard) IsPaused() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.paused
}

func (g *CompositeRiskGuard) notify(evtType event.EventType, level string, score float64, reasons []string) {
	if g.notifier == nil {
		return
	}
	g.notifier.Send(&event.Event{
		Type:      evtType,
		Timestamp: time.Now(),
		Data: map[string]interface{}{
			"source":  compositeRiskPauseSource,
			"level":   level,
			"score":   score,
			"reasons": reasons,
		},
	})
}
