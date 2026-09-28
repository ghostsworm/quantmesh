package config

// RiskControls is published as one immutable runtime revision. Clone before
// giving it to another owner; schedules and Bot controls contain references.
type RiskControls struct {
	Open OpenPositionControl
	Grid GridRiskControl
}

func CloneOpenPositionControl(src OpenPositionControl) OpenPositionControl {
	dst := src
	if src.BotRiskControl != nil {
		copy := *src.BotRiskControl
		dst.BotRiskControl = &copy
	}
	if src.PeriodicRule != nil {
		copy := *src.PeriodicRule
		dst.PeriodicRule = &copy
	}
	dst.ScheduleRules = append([]ScheduleRule(nil), src.ScheduleRules...)
	for i := range dst.ScheduleRules {
		dst.ScheduleRules[i].Weekdays = append([]int(nil), src.ScheduleRules[i].Weekdays...)
	}
	return dst
}

func (r RiskControls) Clone() RiskControls {
	r.Open = CloneOpenPositionControl(r.Open)
	return r
}

// PositionLimits uses nominal value, not leveraged margin. An enabled Bot
// overrides the entire tuple, including zero (unlimited); disabled falls back.
func (c OpenPositionControl) PositionLimits() (quantity, notional float64, layers int) {
	if b := c.BotRiskControl; b != nil && b.Enabled {
		return b.MaxPositionQuantity, b.MaxPositionValue, b.MaxPositionLayers
	}
	return c.MaxPositionQuantity, c.MaxPositionValue, c.MaxPositionLayers
}

func ApplyBotRiskControls(local *Config, bot SymbolConfig) {
	globalGrid := local.Trading.GridRiskControl
	local.Trading.OpenPositionControl = CloneOpenPositionControl(bot.OpenPositionControl)
	local.Trading.GridRiskControl = bot.GridRiskControl
	InheritStopLossBasis(&local.Trading.GridRiskControl, globalGrid)
}
