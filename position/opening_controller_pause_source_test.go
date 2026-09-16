package position

import (
	"fmt"
	"testing"
	"time"

	"quantmesh/config"
)

const riskPauseReasonForTest = "熔断器触发: 回撤超限"

func newPauseSourceTestController(t *testing.T) (*SuperPositionManager, *OpeningController, *config.SymbolConfig) {
	t.Helper()
	cfg := &config.Config{}
	cfg.Trading.Symbol = "BTCUSDT"
	cfg.Trading.PriceInterval = 100
	cfg.Trading.OrderQuantity = 100
	spm := NewSuperPositionManager(cfg, &MockExecutor{}, &MockExchange{}, 2, 3)
	symbolCfg := &config.SymbolConfig{Exchange: "binance", Symbol: "BTCUSDT"}
	return spm, NewOpeningController(spm, symbolCfg), symbolCfg
}

// runScheduleAtCurrentMinute 以當前 UTC 分鐘配置定時規則並執行檢查；跨分鐘時重試，避免邊界抖動
func runScheduleAtCurrentMinute(t *testing.T, oc *OpeningController, symbolCfg *config.SymbolConfig, action string) {
	t.Helper()
	for attempt := 0; attempt < 3; attempt++ {
		now := time.Now().UTC()
		symbolCfg.OpenPositionControl.ScheduleRules = []config.ScheduleRule{{
			Enabled: true, Action: action, Time: fmt.Sprintf("%02d:%02d", now.Hour(), now.Minute()),
		}}
		oc.check()
		if time.Now().UTC().Minute() == now.Minute() {
			return
		}
	}
	t.Fatalf("could not run schedule rule within a single minute")
}

func TestOpeningControllerScheduleResumeDoesNotLiftRiskPause(t *testing.T) {
	spm, oc, symbolCfg := newPauseSourceTestController(t)
	spm.PauseOpening(riskPauseReasonForTest)

	runScheduleAtCurrentMinute(t, oc, symbolCfg, "resume")

	if !spm.IsOpeningPaused() || spm.GetOpeningPauseReason() != riskPauseReasonForTest {
		t.Fatalf("risk pause lifted by schedule rule: paused=%v reason=%q", spm.IsOpeningPaused(), spm.GetOpeningPauseReason())
	}
}

func TestOpeningControllerSchedulePauseDoesNotOverwriteRiskReason(t *testing.T) {
	spm, oc, symbolCfg := newPauseSourceTestController(t)
	spm.PauseOpening(riskPauseReasonForTest)

	runScheduleAtCurrentMinute(t, oc, symbolCfg, "pause")
	if spm.GetOpeningPauseReason() != riskPauseReasonForTest {
		t.Fatalf("schedule pause overwrote risk reason: %q", spm.GetOpeningPauseReason())
	}
}

func TestOpeningControllerScheduleResumesOwnPause(t *testing.T) {
	spm, oc, symbolCfg := newPauseSourceTestController(t)

	runScheduleAtCurrentMinute(t, oc, symbolCfg, "pause")
	if !spm.IsOpeningPaused() || spm.GetOpeningPauseReason() != openingPauseReasonSchedule {
		t.Fatalf("schedule pause not applied: paused=%v reason=%q", spm.IsOpeningPaused(), spm.GetOpeningPauseReason())
	}
	runScheduleAtCurrentMinute(t, oc, symbolCfg, "resume")
	if spm.IsOpeningPaused() {
		t.Fatalf("schedule resume should lift its own pause")
	}
}

func TestOpeningControllerPeriodicOpenPhaseDoesNotLiftRiskPause(t *testing.T) {
	cases := []struct {
		name        string
		pauseReason string
		wantPaused  bool
	}{
		{name: "risk pause kept", pauseReason: riskPauseReasonForTest, wantPaused: true},
		{name: "own periodic pause lifted", pauseReason: openingPauseReasonPeriodic, wantPaused: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spm, oc, symbolCfg := newPauseSourceTestController(t)
			symbolCfg.OpenPositionControl.PeriodicRule = &config.PeriodicRule{Enabled: true, OpenDurationMin: 5, CloseDurationMin: 5}
			spm.PauseOpening(tc.pauseReason)
			oc.periodicState = false // 關倉期已到期，下一次檢查切換到開倉期

			oc.check()

			if spm.IsOpeningPaused() != tc.wantPaused {
				t.Fatalf("paused=%v want %v (reason=%q)", spm.IsOpeningPaused(), tc.wantPaused, spm.GetOpeningPauseReason())
			}
		})
	}
}

func TestOpeningControllerPositionLimitRecoveryOnlyLiftsLimitPause(t *testing.T) {
	cases := []struct {
		name        string
		pauseReason string
		wantPaused  bool
	}{
		{name: "position_limit lifted", pauseReason: openingPauseReasonPositionLimit, wantPaused: false},
		{name: "risk pause kept", pauseReason: riskPauseReasonForTest, wantPaused: true},
		{name: "schedule pause kept", pauseReason: openingPauseReasonSchedule, wantPaused: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spm, oc, _ := newPauseSourceTestController(t)
			spm.PauseOpening(tc.pauseReason)

			oc.check()

			if spm.IsOpeningPaused() != tc.wantPaused {
				t.Fatalf("paused=%v want %v", spm.IsOpeningPaused(), tc.wantPaused)
			}
		})
	}
}
