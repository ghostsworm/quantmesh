package web

import (
	"fmt"
	"quantmesh/config"
)

type VolatilityPausePatch struct {
	PauseOnHighVolatility    *bool    `json:"pause_on_high_volatility"`
	PauseOnExtremeVolatility *bool    `json:"pause_on_extreme_volatility"`
	PauseOnSuddenIncrease    *bool    `json:"pause_on_sudden_increase"`
	PauseOnDowntrend         *bool    `json:"pause_on_downtrend"`
	PauseOnUptrend           *bool    `json:"pause_on_uptrend"`
	AutoResumeOnNormal       *bool    `json:"auto_resume_on_normal"`
	ResumeThreshold          *float64 `json:"resume_threshold"`
	TrendCheckPeriod         *int     `json:"trend_check_period"`
	TrendDownThreshold       *float64 `json:"trend_down_threshold"`
	TrendUpThreshold         *float64 `json:"trend_up_threshold"`
}

func validateVolatilityPausePatch(p *VolatilityPausePatch) error {
	if p == nil {
		return nil
	}
	for name, value := range map[string]*float64{"resume_threshold": p.ResumeThreshold, "trend_down_threshold": p.TrendDownThreshold, "trend_up_threshold": p.TrendUpThreshold} {
		if value != nil && !validNonNegativeFinite(*value) {
			return fmt.Errorf("volatility_pause_config.%s must be finite and >= 0", name)
		}
	}
	if p.TrendCheckPeriod != nil && (*p.TrendCheckPeriod < 0 || *p.TrendCheckPeriod > 1440) {
		return fmt.Errorf("volatility_pause_config.trend_check_period must be between 0 and 1440 minutes")
	}
	return nil
}

func applyVolatilityPausePatch(dst *config.VolatilityPauseConfig, p *VolatilityPausePatch) {
	if p == nil {
		return
	}
	if p.PauseOnHighVolatility != nil {
		dst.PauseOnHighVolatility = *p.PauseOnHighVolatility
	}
	if p.PauseOnExtremeVolatility != nil {
		dst.PauseOnExtremeVolatility = *p.PauseOnExtremeVolatility
	}
	if p.PauseOnSuddenIncrease != nil {
		dst.PauseOnSuddenIncrease = *p.PauseOnSuddenIncrease
	}
	if p.PauseOnDowntrend != nil {
		dst.PauseOnDowntrend = *p.PauseOnDowntrend
	}
	if p.PauseOnUptrend != nil {
		dst.PauseOnUptrend = *p.PauseOnUptrend
	}
	if p.AutoResumeOnNormal != nil {
		dst.AutoResumeOnNormal = *p.AutoResumeOnNormal
	}
	if p.ResumeThreshold != nil {
		dst.ResumeThreshold = *p.ResumeThreshold
	}
	if p.TrendCheckPeriod != nil {
		dst.TrendCheckPeriod = *p.TrendCheckPeriod
	}
	if p.TrendDownThreshold != nil {
		dst.TrendDownThreshold = *p.TrendDownThreshold
	}
	if p.TrendUpThreshold != nil {
		dst.TrendUpThreshold = *p.TrendUpThreshold
	}
}
