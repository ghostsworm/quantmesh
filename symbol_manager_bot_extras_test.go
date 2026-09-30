package main

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/execution"
	"quantmesh/position"
	"quantmesh/storage"
)

func TestApplyStartupOpeningPauseHoldersKeepsEachOwnerIndependent(t *testing.T) {
	gate := &execution.OpeningGate{}
	applyStartupOpeningPauseHolders(gate, []storage.OpeningPauseHolder{
		{Source: "circuit_breaker", Reason: "loss"},
		{Source: "composite_risk", Reason: "stop"},
	})
	if !gate.HasBlock("circuit_breaker") || !gate.HasBlock("composite_risk") {
		t.Fatalf("startup holders not installed: %v", gate.Sources())
	}
	gate.Unblock("circuit_breaker")
	if gate.HasBlock("circuit_breaker") || !gate.HasBlock("composite_risk") || !gate.Blocked() {
		t.Fatalf("releasing one restored owner affected another: %v", gate.Sources())
	}
}

const autoRebuilderRunFrame = "(*GridAutoRebuilder).run"

func TestValidateBotRuntimeExtras(t *testing.T) {
	validRebuild := config.GetDefaultAutoRebuildConfig()
	validRebuild.Enabled = true

	cases := []struct {
		name    string
		mutate  func(sc *config.SymbolConfig)
		wantErr string // 空表示應通過
	}{
		{name: "zero values", mutate: func(sc *config.SymbolConfig) {}},
		{name: "valid auto_rebuild", mutate: func(sc *config.SymbolConfig) { sc.AutoRebuild = validRebuild }},
		{name: "disabled auto_rebuild not validated", mutate: func(sc *config.SymbolConfig) {
			sc.AutoRebuild = config.GridAutoRebuildConfig{Enabled: false, CheckIntervalMinutes: -1}
		}},
		{name: "negative interval", wantErr: "check_interval_minutes", mutate: func(sc *config.SymbolConfig) {
			sc.AutoRebuild = validRebuild
			sc.AutoRebuild.CheckIntervalMinutes = -1
		}},
		{name: "ratio out of range", wantErr: "expired_order_ratio", mutate: func(sc *config.SymbolConfig) {
			sc.AutoRebuild = validRebuild
			sc.AutoRebuild.ExpiredOrderRatio = 1.5
		}},
		{name: "bad rebuild mode", wantErr: "rebuild_mode", mutate: func(sc *config.SymbolConfig) {
			sc.AutoRebuild = validRebuild
			sc.AutoRebuild.RebuildMode = "aggressive"
		}},
		{name: "trend confirm not implemented", wantErr: "require_trend_confirm", mutate: func(sc *config.SymbolConfig) {
			sc.AutoRebuild = validRebuild
			sc.AutoRebuild.RequireTrendConfirm = true
		}},
		{name: "valid slot filter", mutate: func(sc *config.SymbolConfig) {
			sc.SlotFilter.Rules = []config.SlotFilterRule{
				{Type: "exclude", Prices: []float64{100}},
				{Type: "include", MinPrice: 90, MaxPrice: 110},
			}
		}},
		{name: "bad slot filter type", wantErr: "rules[0].type", mutate: func(sc *config.SymbolConfig) {
			sc.SlotFilter.Rules = []config.SlotFilterRule{{Type: "block", Prices: []float64{100}}}
		}},
		{name: "half range", wantErr: "同時設置", mutate: func(sc *config.SymbolConfig) {
			sc.SlotFilter.Rules = []config.SlotFilterRule{{Type: "exclude", MinPrice: 100}}
		}},
		{name: "inverted range", wantErr: "min_price 大於 max_price", mutate: func(sc *config.SymbolConfig) {
			sc.SlotFilter.Rules = []config.SlotFilterRule{{Type: "exclude", MinPrice: 110, MaxPrice: 100}}
		}},
		{name: "empty rule", wantErr: "必須設置 prices", mutate: func(sc *config.SymbolConfig) {
			sc.SlotFilter.Rules = []config.SlotFilterRule{{Type: "exclude"}}
		}},
		{name: "valid close config", mutate: func(sc *config.SymbolConfig) {
			sc.CloseOnStopConfig = config.ClosePositionConfig{Method: "limit", TimeoutSec: 30, AutoRetry: true, MaxRetries: 2, QuantityRatio: 0.5}
		}},
		{name: "close config without method", wantErr: "method", mutate: func(sc *config.SymbolConfig) {
			sc.CloseOnStopConfig = config.ClosePositionConfig{TimeoutSec: 30}
		}},
		{name: "close ratio out of range", wantErr: "quantity_ratio", mutate: func(sc *config.SymbolConfig) {
			sc.CloseOnStopConfig = config.ClosePositionConfig{Method: "market", QuantityRatio: 2}
		}},
		{name: "both partial close", wantErr: "BOTH", mutate: func(sc *config.SymbolConfig) {
			sc.Direction = "BOTH"
			sc.CloseOnStopConfig = config.ClosePositionConfig{Method: "market", QuantityRatio: 0.5}
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sc := config.SymbolConfig{Exchange: "binance", Symbol: "BTCUSDT"}
			tc.mutate(&sc)
			err := validateBotRuntimeExtras(sc)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

// 非法配置在創建交易所實例前拒絕啟動
func TestStartSymbolRuntimeRejectsInvalidBotExtras(t *testing.T) {
	symCfg := config.SymbolConfig{
		ID:         "bad-extras",
		Exchange:   "binance",
		Symbol:     "BTCUSDT",
		MarketType: "futures",
		SlotFilter: config.SlotFilterConfig{Rules: []config.SlotFilterRule{{Type: "oops", Prices: []float64{1}}}},
	}
	rt, err := startSymbolRuntime(context.Background(), &config.Config{}, symCfg, nil, nil, nil, nil, nil)
	if err == nil || rt != nil {
		t.Fatalf("expected start failure, got rt=%v err=%v", rt, err)
	}
	if !strings.Contains(err.Error(), "slot_filter") || !strings.Contains(err.Error(), "BTCUSDT") {
		t.Fatalf("error should name bot and slot_filter: %v", err)
	}
}

func newExtrasTestSPM() *position.SuperPositionManager {
	cfg := &config.Config{}
	cfg.Trading.Symbol = "BTCUSDT"
	cfg.Trading.Direction = "LONG"
	cfg.Trading.PriceInterval = 10
	return position.NewSuperPositionManager(cfg, nil, nil, 2, 3)
}

func TestApplyConfiguredSlotFilter(t *testing.T) {
	spm := newExtrasTestSPM()
	applyConfiguredSlotFilter(context.Background(), config.SymbolConfig{Symbol: "BTCUSDT"}, spm)
	if spm.GetSlotFilter() != nil {
		t.Fatal("empty rules must not install a filter")
	}

	sc := config.SymbolConfig{Symbol: "BTCUSDT"}
	sc.SlotFilter.Rules = []config.SlotFilterRule{{Type: "exclude", Prices: []float64{100}, Reason: "test"}}
	applyConfiguredSlotFilter(context.Background(), sc, spm)
	got := spm.GetSlotFilter()
	if got == nil || len(got.Rules) != 1 || got.Rules[0].Prices[0] != 100 {
		t.Fatalf("filter not applied: %+v", got)
	}
	sc.SlotFilter.Rules[0].Type = "include"
	if got.Rules[0].Type != "exclude" {
		t.Fatal("runtime filter must not alias the config slice")
	}
}

func autoRebuilderRunning() bool {
	buf := make([]byte, 1<<20)
	n := runtime.Stack(buf, true)
	return strings.Contains(string(buf[:n]), autoRebuilderRunFrame)
}

func waitAutoRebuilderGone(t *testing.T) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for autoRebuilderRunning() {
		if time.Now().After(deadline) {
			t.Fatal("GridAutoRebuilder goroutine leaked after stop")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestStartConfiguredAutoRebuildStartsAndStops(t *testing.T) {
	waitAutoRebuilderGone(t)
	baseline := runtime.NumGoroutine()

	sc := config.SymbolConfig{Symbol: "BTCUSDT", AutoRebuild: config.GetDefaultAutoRebuildConfig()}
	sc.AutoRebuild.Enabled = true
	spm := newExtrasTestSPM()

	stop := startConfiguredAutoRebuild(context.Background(), sc, spm, true)
	if !spm.IsAutoRebuildEnabled() {
		t.Fatal("auto rebuild should be enabled")
	}
	// 協程可能尚未被調度，輪詢確認它確實在跑（否則後面的洩漏檢查沒有意義）
	startDeadline := time.Now().Add(2 * time.Second)
	for !autoRebuilderRunning() {
		if time.Now().After(startDeadline) {
			t.Fatal("GridAutoRebuilder goroutine never started")
		}
		time.Sleep(time.Millisecond)
	}
	stop()
	if spm.IsAutoRebuildEnabled() {
		t.Fatal("auto rebuild should be stopped")
	}
	waitAutoRebuilderGone(t)
	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > baseline && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := runtime.NumGoroutine(); n > baseline {
		t.Fatalf("goroutines not released: baseline=%d now=%d", baseline, n)
	}
	stop() // 冪等
}

func TestStartConfiguredAutoRebuildSkipped(t *testing.T) {
	enabled := config.SymbolConfig{Symbol: "BTCUSDT", AutoRebuild: config.GetDefaultAutoRebuildConfig()}
	enabled.AutoRebuild.Enabled = true

	cases := []struct {
		name       string
		sc         config.SymbolConfig
		gridActive bool
	}{
		{name: "disabled", sc: config.SymbolConfig{Symbol: "BTCUSDT"}, gridActive: true},
		{name: "non-grid mode", sc: enabled, gridActive: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			spm := newExtrasTestSPM()
			stop := startConfiguredAutoRebuild(context.Background(), tc.sc, spm, tc.gridActive)
			if spm.IsAutoRebuildEnabled() {
				t.Fatal("auto rebuild must not start")
			}
			stop()
		})
	}
	if stop := startConfiguredAutoRebuild(context.Background(), enabled, nil, true); stop == nil {
		t.Fatal("nil spm must return a no-op stop")
	} else {
		stop()
	}
}

type closeOnStopRecorder struct {
	calls    []string
	gotCfg   config.ClosePositionConfig
	closeErr error
}

func (r *closeOnStopRecorder) actions() closeOnStopActions {
	return closeOnStopActions{
		cancelAllOrders: func() { r.calls = append(r.calls, "cancel") },
		liquidateAll:    func(context.Context) error { r.calls = append(r.calls, "liquidate"); return nil },
		closePositions: func(ctx context.Context, cfg config.ClosePositionConfig) error {
			r.calls = append(r.calls, "close")
			r.gotCfg = cfg
			return r.closeErr
		},
	}
}

func TestRunCloseOnStop(t *testing.T) {
	limitCfg := config.ClosePositionConfig{Method: "limit", PriceOffset: -0.1, TimeoutSec: 20, AutoRetry: true, MaxRetries: 2}
	partialCfg := config.ClosePositionConfig{Method: "market", QuantityRatio: 0.5}
	closeErr := errors.New("place failed")

	cases := []struct {
		name      string
		sc        config.SymbolConfig
		closeErr  error
		wantCalls string
	}{
		{name: "close_on_stop off", sc: config.SymbolConfig{CloseOnStopConfig: limitCfg}, wantCalls: ""},
		{name: "legacy bool only", sc: config.SymbolConfig{CloseOnStop: true}, wantCalls: "liquidate"},
		{name: "configured method", sc: config.SymbolConfig{CloseOnStop: true, CloseOnStopConfig: limitCfg}, wantCalls: "cancel,close"},
		{name: "both direction", sc: config.SymbolConfig{CloseOnStop: true, Direction: "BOTH", CloseOnStopConfig: limitCfg}, wantCalls: "liquidate"},
		{name: "full close failure never blindly submits a second close", sc: config.SymbolConfig{CloseOnStop: true, CloseOnStopConfig: limitCfg}, closeErr: closeErr, wantCalls: "cancel,close"},
		{name: "partial close fails no fallback", sc: config.SymbolConfig{CloseOnStop: true, CloseOnStopConfig: partialCfg}, closeErr: closeErr, wantCalls: "cancel,close"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.sc.Symbol = "BTCUSDT"
			rec := &closeOnStopRecorder{closeErr: tc.closeErr}
			runCloseOnStop(context.Background(), tc.sc, rec.actions())
			if got := strings.Join(rec.calls, ","); got != tc.wantCalls {
				t.Fatalf("calls = %q, want %q", got, tc.wantCalls)
			}
			if strings.Contains(tc.wantCalls, "close") && rec.gotCfg != tc.sc.CloseOnStopConfig {
				t.Fatalf("close config not passed through: %+v", rec.gotCfg)
			}
		})
	}
}

func TestCloseOnStopForRuntimeNilSafe(t *testing.T) {
	sc := config.SymbolConfig{Symbol: "BTCUSDT", CloseOnStop: true}
	closeOnStopForRuntime(context.Background(), sc, nil)
	closeOnStopForRuntime(context.Background(), sc, &SymbolRuntime{})
}
