package config

import "testing"

func TestRiskControlsCloneOwnsNestedData(t *testing.T) {
	src := RiskControls{Open: OpenPositionControl{
		BotRiskControl: &BotRiskControl{Enabled: true, MaxPositionValue: 100},
		PeriodicRule:   &PeriodicRule{OpenDurationMin: 5},
		ScheduleRules:  []ScheduleRule{{Weekdays: []int{1}}},
	}}
	copy := src.Clone()
	copy.Open.BotRiskControl.MaxPositionValue = 200
	copy.Open.PeriodicRule.OpenDurationMin = 10
	copy.Open.ScheduleRules[0].Weekdays[0] = 2
	if src.Open.BotRiskControl.MaxPositionValue != 100 || src.Open.PeriodicRule.OpenDurationMin != 5 || src.Open.ScheduleRules[0].Weekdays[0] != 1 {
		t.Fatal("clone mutated source")
	}
}

func TestRiskControlsPositionLimitsOverrideWholeTuple(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		c := OpenPositionControl{MaxPositionQuantity: 2, MaxPositionValue: 100, MaxPositionLayers: 3,
			BotRiskControl: &BotRiskControl{Enabled: enabled}}
		q, v, l := c.PositionLimits()
		if enabled && (q != 0 || v != 0 || l != 0) {
			t.Fatal("enabled zero must mean unlimited, not per-field inheritance")
		}
		if !enabled && (q != 2 || v != 100 || l != 3) {
			t.Fatal("disabled override must retain parent limits")
		}
	}
}

func TestApplyBotRiskControlsStartupOwnsBotConfiguration(t *testing.T) {
	local := &Config{}
	local.Trading.GridRiskControl.StopLossBasis = StopLossBasisEquity
	bot := SymbolConfig{OpenPositionControl: OpenPositionControl{BotRiskControl: &BotRiskControl{Enabled: true, MaxPositionValue: 100}}}
	ApplyBotRiskControls(local, bot)
	bot.OpenPositionControl.BotRiskControl.MaxPositionValue = 999
	if local.Trading.OpenPositionControl.BotRiskControl.MaxPositionValue != 100 || local.Trading.GridRiskControl.GetStopLossBasis() != StopLossBasisEquity {
		t.Fatal("startup did not copy Bot controls or inherit stop loss basis")
	}
}
