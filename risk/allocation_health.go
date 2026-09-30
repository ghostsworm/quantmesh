package risk

import (
	"fmt"
	"strings"

	"quantmesh/logger"
)

const allocationRiskPauseSource = "allocation_risk_unverified"

func (gcb *GlobalCircuitBreaker) refreshAllocationRisk() {
	if gcb == nil || gcb.config == nil || !gcb.config.Triggers.AllocationExceeded.Enabled {
		return
	}
	bots := []BotController(nil)
	if gcb.botProvider != nil {
		bots = gcb.botProvider.GetAllBots()
	}
	available, exceeded := true, false
	reason, healthError := "", ""
	for i, bot := range bots {
		if bot == nil {
			continue
		}
		provider, ok := bot.(AllocationRiskProvider)
		if !ok {
			available = false
			healthError = fmt.Sprintf("bot %d does not expose allocation risk status", i)
			break
		}
		botExceeded, botReason, err := provider.AllocationRiskStatus()
		if err != nil {
			available = false
			healthError = fmt.Sprintf("read bot %d allocation risk status: %v", i, err)
			break
		}
		if botExceeded {
			exceeded = true
			if reason == "" {
				reason = strings.TrimSpace(botReason)
			}
		}
	}
	if exceeded && reason == "" {
		reason = "position allocation usage exceeds its configured limit"
	}
	if !available {
		reason = "allocation limits could not be verified"
	}

	gcb.statusMu.Lock()
	changedError := gcb.allocationError != healthError
	gcb.allocationAvailable = available
	gcb.allocationExceeded = exceeded
	gcb.allocationReason = reason
	gcb.allocationError = healthError
	gcb.statusMu.Unlock()

	held := !available || exceeded
	if gcb.botProvider == nil {
		return
	}
	if _, pauser := gcb.deps(); pauser != nil {
		if held && !pauser.IsHeldBy(allocationRiskPauseSource) {
			pauser.Pause(allocationRiskPauseSource, reason, bots)
		} else if !held && pauser.IsHeldBy(allocationRiskPauseSource) {
			pauser.Release(allocationRiskPauseSource, bots)
		}
	} else {
		for _, bot := range bots {
			if bot == nil {
				continue
			}
			if owner, ok := bot.(allocationRiskHold); ok {
				owner.SetAllocationRiskHold(held)
			} else if held {
				pauseBotWithoutAutoResume(bot, reason)
			}
		}
	}
	if changedError && healthError != "" {
		logger.Warn("⚠️ [全局熔断] 配额状态未核实，已封锁新开仓: %s", healthError)
	}
}
