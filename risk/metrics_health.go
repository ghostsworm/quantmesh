package risk

import (
	"time"

	"quantmesh/logger"
)

const metricsUnavailablePauseSource = "risk_metrics_unavailable"

// MetricsHealth keeps data availability separate from a numerical zero loss.
type MetricsHealth struct {
	Available         bool      `json:"available"`
	DrawdownAvailable bool      `json:"drawdown_available"`
	CashFlowAdjusted  bool      `json:"cash_flow_adjusted"`
	Persisted         bool      `json:"persisted"`
	CheckedAt         time.Time `json:"checked_at"`
	ValidUntil        time.Time `json:"valid_until"`
	EquityObservedAt  time.Time `json:"equity_observed_at,omitempty"`
	Error             string    `json:"error,omitempty"`
}

type observedMetricsSink interface {
	UpdateMetricsObservation(MetricsSnapshot, MetricsHealth)
}

func (f *MetricsFeeder) reportHealth(snap MetricsSnapshot, err error) {
	now := f.opts.Now()
	health := MetricsHealth{Available: err == nil, DrawdownAvailable: snap.DrawdownAvailable, CashFlowAdjusted: snap.CashFlowAdjusted,
		Persisted: err == nil && snap.DrawdownAvailable && f.opts.EquityStore != nil, CheckedAt: now, ValidUntil: now.Add(f.opts.MaxEquityAge), EquityObservedAt: snap.EquityObservedAt}
	if !snap.EquityObservedAt.IsZero() {
		health.ValidUntil = snap.EquityObservedAt.Add(f.opts.MaxEquityAge)
	}
	if err != nil {
		health.Error = err.Error()
	}
	if sink, ok := f.sink.(observedMetricsSink); ok {
		sink.UpdateMetricsObservation(snap, health)
	} else if sink, ok := f.sink.(interface{ UpdateMetricsHealth(MetricsHealth) }); ok {
		sink.UpdateMetricsHealth(health)
	}
}

// InvalidateEquityScope serializes a scope change with Tick. The previous
// observation is invalidated before publish runs, and no in-flight old-scope
// observation can overwrite that invalidation afterward.
func (f *MetricsFeeder) InvalidateEquityScope(publish func()) {
	if f == nil {
		if publish != nil {
			publish()
		}
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	// Force the next tick to reload the durable checkpoint. Keep the cached
	// state itself so an absent checkpoint cannot be mistaken for first startup;
	// a later explicit CAS baseline reset will then be observed without allowing
	// this scope change to silently initialize a new baseline.
	f.equityLoaded = false
	if sink, ok := f.sink.(interface{ UpdateMetricsHealth(MetricsHealth) }); ok {
		sink.UpdateMetricsHealth(MetricsHealth{Error: "equity account scope changed; awaiting fresh evidence"})
	}
	if publish != nil {
		publish()
	}
}

// Publish values and their provenance atomically, so a background check cannot
// combine a new unadjusted drawdown with the previous sample's healthy status.
func (gcb *GlobalCircuitBreaker) UpdateMetricsObservation(snap MetricsSnapshot, health MetricsHealth) {
	if !finiteEquity(snap.DailyPnL) || !finiteEquity(snap.MaxDrawdownPct) || snap.MaxDrawdownPct < 0 || snap.ConsecutiveLosses < 0 {
		health.Available, health.DrawdownAvailable, health.Persisted = false, false, false
		health.Error = "invalid risk metric observation"
	}
	gcb.statusMu.Lock()
	if health.Available {
		gcb.dailyPnL, gcb.maxDrawdown, gcb.consecutiveLosses = snap.DailyPnL, snap.MaxDrawdownPct, snap.ConsecutiveLosses
	}
	gcb.metricsHealth = health
	gcb.statusMu.Unlock()
	gcb.applyMetricsHealthGate()
}

func (gcb *GlobalCircuitBreaker) UpdateMetricsHealth(health MetricsHealth) {
	gcb.statusMu.Lock()
	gcb.metricsHealth = health
	gcb.statusMu.Unlock()
	gcb.applyMetricsHealthGate()
}

func (h MetricsHealth) verifiedDrawdown() bool {
	return h.Available && h.DrawdownAvailable && h.CashFlowAdjusted && h.Persisted
}

// Admission checks the current observation's expiry, not a cached pause flag.
// This method intentionally has no coordinator, Bot, SQL or exchange calls.
func (gcb *GlobalCircuitBreaker) metricsOpeningAdmissionAllowed() bool {
	if !gcb.config.Enabled {
		return true
	}
	health := gcb.GetMetricsHealth()
	tradeMetrics := gcb.config.Triggers.TotalDailyLoss.Enabled || gcb.config.Triggers.ConsecutiveLosses.Enabled
	if (tradeMetrics || gcb.config.Triggers.MaxDrawdown.Enabled) && (health.CheckedAt.IsZero() || health.ValidUntil.IsZero()) {
		return false
	}
	return (!tradeMetrics || health.Available) &&
		(!gcb.config.Triggers.MaxDrawdown.Enabled || health.verifiedDrawdown())
}

// Data uncertainty owns an independent opening hold. Never call ResumeOpening
// on recovery: that could remove a user's pause or another risk owner's hold.
func (gcb *GlobalCircuitBreaker) applyMetricsHealthGate() {
	gcb.metricsHealthApplyMu.Lock()
	defer gcb.metricsHealthApplyMu.Unlock()
	health := gcb.GetMetricsHealth()
	tradeMetricsEnabled := gcb.config.Triggers.TotalDailyLoss.Enabled || gcb.config.Triggers.ConsecutiveLosses.Enabled
	paused := gcb.config.Enabled && ((tradeMetricsEnabled && !health.Available) ||
		(gcb.config.Triggers.MaxDrawdown.Enabled && !health.verifiedDrawdown()))
	var bots []BotController
	if gcb.botProvider != nil {
		bots = gcb.botProvider.GetAllBots()
	}
	// A per-runtime gate alone misses bots constructed after this observation.
	// Publish the independent source through the existing durable startup
	// admission path, including when there are no current runtimes.
	_, coordinator := gcb.deps()
	if coordinator != nil {
		if paused {
			if err := coordinator.Pause(metricsUnavailablePauseSource, "账户权益或现金流水尚未核实", bots); err != nil {
				logger.Error("权益风控启动暂停持久化未核实: %v", err)
			}
		} else if coordinator.IsHeldBy(metricsUnavailablePauseSource) {
			if _, err := coordinator.ReleaseChecked(metricsUnavailablePauseSource, bots); err != nil {
				logger.Error("权益风控启动暂停解除未核实，保留封锁: %v", err)
			}
		}
	}
	for _, bot := range bots {
		if owned, ok := bot.(interface{ SetRiskDataUnavailable(bool) }); ok {
			owned.SetRiskDataUnavailable(paused)
		} else if paused {
			pauseBotWithoutAutoResume(bot, "账户权益或现金流水尚未核实")
		}
	}
}

func (gcb *GlobalCircuitBreaker) RequiresTradeHistory() bool {
	if gcb == nil || gcb.config == nil || !gcb.config.Enabled {
		return false
	}
	return gcb.config.Triggers.TotalDailyLoss.Enabled || gcb.config.Triggers.ConsecutiveLosses.Enabled
}

func (gcb *GlobalCircuitBreaker) GetMetricsHealth() MetricsHealth {
	gcb.statusMu.RLock()
	health := gcb.metricsHealth
	gcb.statusMu.RUnlock()
	return health.validAt(time.Now())
}

func (health MetricsHealth) validAt(now time.Time) MetricsHealth {
	if !health.CheckedAt.IsZero() && now.After(health.ValidUntil) {
		health.Available, health.DrawdownAvailable = false, false
		health.Error = "risk metrics are stale"
	}
	return health
}
