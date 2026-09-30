package main

import (
	"context"
	"errors"
	"testing"

	"quantmesh/config"
)

// shutdownCloseFake 統計進程級與 Bot 級平倉提交次數（按交易對）
type shutdownCloseFake struct {
	processCalls map[string]int
	botCalls     map[string]int
	processErr   error
	exchangeFlat map[string]bool // Bot 級重查時交易所是否已無持倉
}

func newShutdownCloseFake() *shutdownCloseFake {
	return &shutdownCloseFake{
		processCalls: map[string]int{},
		botCalls:     map[string]int{},
		exchangeFlat: map[string]bool{},
	}
}

func (f *shutdownCloseFake) processClose(_ context.Context, runtimes []*SymbolRuntime) error {
	if f.processErr != nil {
		return f.processErr
	}
	for _, rt := range runtimes {
		if decideShutdownCloseOwner(true, rt.Config) != shutdownCloseProcess {
			continue
		}
		f.processCalls[rt.Config.Symbol]++
		// The fixture models each owner-scoped close as successful.
		f.exchangeFlat[rt.Config.Symbol] = true
	}
	return nil
}

// simulateStop 模擬 rt.Stop 中的 close_on_stop 流程（shouldRunBotCloseOnStop 門控 + runCloseOnStop）
func (f *shutdownCloseFake) simulateStop(rt *SymbolRuntime) {
	sc := rt.Config
	if !sc.CloseOnStop || rt.shutdownCloseHandledReason() != "" || rt.shutdownCloseUnverifiedReason() != "" {
		return
	}
	symbol := sc.Symbol
	runCloseOnStop(context.Background(), sc, closeOnStopActions{
		cancelAllOrders: func() {},
		liquidateAll:    func(context.Context) error { f.botCalls[symbol]++; return nil },
		closePositions: func(context.Context, config.ClosePositionConfig) error {
			f.botCalls[symbol]++
			return nil
		},
		exchangePositionFlat: func(context.Context) (bool, error) { return f.exchangeFlat[symbol], nil },
	})
}

func runShutdown(f *shutdownCloseFake, processEnabled bool, rts []*SymbolRuntime) {
	runProcessLevelCloseOnExit(processEnabled, rts, f.processClose)
	for _, rt := range rts {
		f.simulateStop(rt)
	}
}

func TestShutdownCloseBothFlagsExactlyOneSubmissionPerSymbol(t *testing.T) {
	limitCfg := config.ClosePositionConfig{Method: "limit", TimeoutSec: 10}
	cases := []struct {
		name        string
		processOn   bool
		sc          config.SymbolConfig
		processErr  error
		wantProcess int
		wantBot     int
	}{
		{name: "both on, no close_on_stop_config -> process only", processOn: true,
			sc: config.SymbolConfig{Symbol: "BTCUSDT", Exchange: "binance", CloseOnStop: true}, wantProcess: 1, wantBot: 0},
		{name: "both on, close_on_stop_config set -> bot only", processOn: true,
			sc: config.SymbolConfig{Symbol: "BTCUSDT", Exchange: "binance", CloseOnStop: true, CloseOnStopConfig: limitCfg}, wantProcess: 0, wantBot: 1},
		{name: "both on, spot -> bot only", processOn: true,
			sc: config.SymbolConfig{Symbol: "BTCUSDT", Exchange: "binance", MarketType: "spot", CloseOnStop: true}, wantProcess: 0, wantBot: 1},
		{name: "process only", processOn: true,
			sc: config.SymbolConfig{Symbol: "BTCUSDT", Exchange: "binance"}, wantProcess: 1, wantBot: 0},
		{name: "bot only", processOn: false,
			sc: config.SymbolConfig{Symbol: "BTCUSDT", Exchange: "binance", CloseOnStop: true}, wantProcess: 0, wantBot: 1},
		{name: "process close query fails -> bot fallback once", processOn: true, processErr: errors.New("positions unavailable"),
			sc: config.SymbolConfig{Symbol: "BTCUSDT", Exchange: "binance", CloseOnStop: true}, wantProcess: 0, wantBot: 1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newShutdownCloseFake()
			f.processErr = tc.processErr
			rt := &SymbolRuntime{Config: tc.sc}
			runShutdown(f, tc.processOn, []*SymbolRuntime{rt})
			if got := f.processCalls["BTCUSDT"]; got != tc.wantProcess {
				t.Fatalf("process close calls = %d, want %d", got, tc.wantProcess)
			}
			if got := f.botCalls["BTCUSDT"]; got != tc.wantBot {
				t.Fatalf("bot close calls = %d, want %d", got, tc.wantBot)
			}
			if total := f.processCalls["BTCUSDT"] + f.botCalls["BTCUSDT"]; total != 1 {
				t.Fatalf("total close submissions = %d, want exactly 1", total)
			}
		})
	}
}

func TestShutdownCloseRunsEachSharedAccountBotThroughItsOwnerPath(t *testing.T) {
	f := newShutdownCloseFake()
	a := &SymbolRuntime{Config: config.SymbolConfig{ID: "a", Symbol: "ETHUSDT", Exchange: "binance", CloseOnStop: true}, AccountID: "acc", AccountScope: "verified-account"}
	b := &SymbolRuntime{Config: config.SymbolConfig{ID: "b", Symbol: "ETHUSDT", Exchange: "binance", CloseOnStop: true}, AccountID: "acc", AccountScope: "verified-account"}
	c := &SymbolRuntime{Config: config.SymbolConfig{ID: "c", Symbol: "BTCUSDT", Exchange: "binance", CloseOnStop: true}, AccountID: "acc", AccountScope: "verified-account"}
	runShutdown(f, true, []*SymbolRuntime{a, b, c})
	for _, tc := range []struct {
		symbol string
		owners int
	}{{"ETHUSDT", 2}, {"BTCUSDT", 1}} {
		if total := f.processCalls[tc.symbol] + f.botCalls[tc.symbol]; total != tc.owners {
			t.Fatalf("%s total owner close operations = %d, want %d (process=%d bot=%d)", tc.symbol, total, tc.owners, f.processCalls[tc.symbol], f.botCalls[tc.symbol])
		}
	}
}

func TestRunCloseOnStopSkipsWhenExchangeFlat(t *testing.T) {
	calls := 0
	sc := config.SymbolConfig{Symbol: "BTCUSDT", CloseOnStop: true}
	runCloseOnStop(context.Background(), sc, closeOnStopActions{
		cancelAllOrders:      func() { calls++ },
		liquidateAll:         func(context.Context) error { calls++; return nil },
		closePositions:       func(context.Context, config.ClosePositionConfig) error { calls++; return nil },
		exchangePositionFlat: func(context.Context) (bool, error) { return true, nil },
	})
	if calls != 0 {
		t.Fatalf("exchange flat must skip close_on_stop, got %d calls", calls)
	}

	// 无法证明交易所持仓时，不应继续提交可能重复的平仓订单。
	err := runCloseOnStopWithError(context.Background(), sc, closeOnStopActions{
		cancelAllOrders:      func() {},
		liquidateAll:         func(context.Context) error { calls++; return nil },
		closePositions:       func(context.Context, config.ClosePositionConfig) error { return nil },
		exchangePositionFlat: func(context.Context) (bool, error) { return false, errors.New("timeout") },
	})
	if err == nil || calls != 0 {
		t.Fatalf("recheck failure must be returned without a new close, err=%v calls=%d", err, calls)
	}
}

func TestRunCloseOnStopWithErrorReturnsLiquidationFailure(t *testing.T) {
	closeErr := errors.New("venue rejected liquidation")
	err := runCloseOnStopWithError(context.Background(), config.SymbolConfig{
		Symbol: "BTCUSDT", CloseOnStop: true,
	}, closeOnStopActions{
		cancelAllOrders: func() {},
		liquidateAll:    func(context.Context) error { return closeErr },
	})
	if !errors.Is(err, closeErr) {
		t.Fatalf("runCloseOnStopWithError() = %v, want wrapped liquidation error", err)
	}
}

func TestShouldRunBotCloseOnStopHonoursHandledMark(t *testing.T) {
	sc := config.SymbolConfig{Symbol: "BTCUSDT", CloseOnStop: true}
	rt := &SymbolRuntime{Config: sc}
	rt.markShutdownCloseHandled("已由進程級 close_positions_on_exit 平倉")
	if shouldRunBotCloseOnStop(context.Background(), sc, rt) {
		t.Fatal("handled runtime must skip close_on_stop")
	}
	if shouldRunBotCloseOnStop(context.Background(), sc, nil) {
		t.Fatal("nil runtime must skip")
	}
}
