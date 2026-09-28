package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/exchange"
)

type shutdownFaultExchange struct {
	*fakeCloseExchange
	positionQueries int
	submissions     int
	cancellations   int
	cancelACKOnly   bool
	queryPositions  func(int) ([]*exchange.Position, error)
	queryOrder      func(*exchange.Order) (*exchange.Order, error)
	placeError      error
}

func (f *shutdownFaultExchange) GetPositions(ctx context.Context, symbol string) ([]*exchange.Position, error) {
	f.positionQueries++
	if f.queryPositions != nil {
		return f.queryPositions(f.positionQueries)
	}
	return f.fakeCloseExchange.GetPositions(ctx, symbol)
}
func (f *shutdownFaultExchange) PlaceOrder(ctx context.Context, req *exchange.OrderRequest) (*exchange.Order, error) {
	f.submissions++
	if f.placeError != nil {
		return nil, f.placeError
	}
	return f.fakeCloseExchange.PlaceOrder(ctx, req)
}
func (f *shutdownFaultExchange) GetOrder(ctx context.Context, symbol string, id int64) (*exchange.Order, error) {
	o, err := f.fakeCloseExchange.GetOrder(ctx, symbol, id)
	if err == nil && f.queryOrder != nil {
		return f.queryOrder(o)
	}
	return o, err
}
func (f *shutdownFaultExchange) CancelOrder(ctx context.Context, symbol string, id int64) error {
	f.cancellations++
	if f.cancelACKOnly {
		return nil
	}
	return f.fakeCloseExchange.CancelOrder(ctx, symbol, id)
}

func TestShutdownCloseSubmissionFailureNeverReplacesOrSucceeds(t *testing.T) {
	f := &shutdownFaultExchange{fakeCloseExchange: newFakeCloseExchange(100, 1), placeError: errors.New("lost acknowledgement")}
	failed, err := closeAllPositionsMarketable(context.Background(), f, "BTCUSDT", 100, fastCloseOpts())
	if failed != 1 || !errors.Is(err, errShutdownCloseUnverified) || f.submissions != 1 || f.position != 1 {
		t.Fatalf("ambiguous submission followed by replacement/success: fail=%d err=%v calls=%d", failed, err, f.submissions)
	}
}

func TestShutdownCloseCancelACKMustReachTerminalBeforeMarket(t *testing.T) {
	f := &shutdownFaultExchange{fakeCloseExchange: newFakeCloseExchange(100, 1), cancelACKOnly: true}
	f.limitNeverFills = true
	failed, err := closeAllPositionsMarketable(context.Background(), f, "BTCUSDT", 100, fastCloseOpts())
	if failed != 1 || !errors.Is(err, errShutdownCloseUnverified) || f.submissions != 1 || f.cancellations != 1 || f.position != 1 {
		t.Fatalf("cancel ACK freed risk: failed=%d err=%v calls=%d cancels=%d", failed, err, f.submissions, f.cancellations)
	}
}

func TestShutdownFlatnessQueryFailuresNeverMeanFlat(t *testing.T) {
	f := &shutdownFaultExchange{fakeCloseExchange: newFakeCloseExchange(100, 1), queryPositions: func(int) ([]*exchange.Position, error) { return nil, errors.New("offline") }}
	left, err := waitPositionsFlat(context.Background(), f, "BTCUSDT", fastCloseOpts())
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("missing explicit unverified result: left=%v err=%v", left, err)
	}
}

func TestShutdownCloseFinalQueryOutageIsNotSuccess(t *testing.T) {
	f := &shutdownFaultExchange{fakeCloseExchange: newFakeCloseExchange(100, 1)}
	f.queryPositions = func(n int) ([]*exchange.Position, error) {
		if n >= 3 {
			return nil, errors.New("final verification offline")
		}
		return []*exchange.Position{{Symbol: "BTCUSDT", Size: f.position, MarkPrice: 100}}, nil
	}
	failed, err := closeAllPositionsMarketable(context.Background(), f, "BTCUSDT", 100, fastCloseOpts())
	if failed == 0 || !errors.Is(err, errShutdownCloseUnverified) {
		t.Fatalf("final outage reported successful: failed=%d err=%v", failed, err)
	}
}

func TestShutdownRejectsInvalidPositionEvidence(t *testing.T) {
	for _, qty := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		f := &shutdownFaultExchange{fakeCloseExchange: newFakeCloseExchange(100, qty)}
		if _, err := closeAllPositionsMarketable(context.Background(), f, "BTCUSDT", 100, fastCloseOpts()); err == nil || f.submissions != 0 {
			t.Fatalf("invalid quantity accepted: %v", err)
		}
	}
	f := &shutdownFaultExchange{fakeCloseExchange: newFakeCloseExchange(100, 1), queryPositions: func(int) ([]*exchange.Position, error) { return []*exchange.Position{nil}, nil }}
	if _, err := queryShutdownPositions(context.Background(), f, "BTCUSDT"); err == nil {
		t.Fatal("nil position accepted")
	}
}

func TestShutdownRetainsGrossLegsAndDust(t *testing.T) {
	f := &shutdownFaultExchange{fakeCloseExchange: newFakeCloseExchange(100, 1), queryPositions: func(int) ([]*exchange.Position, error) {
		return []*exchange.Position{{Symbol: "BTCUSDT", Size: 1}, {Symbol: "BTCUSDT", Size: -1}, {Symbol: "BTCUSDT", Size: 1e-14}}, nil
	}}
	positions, err := queryShutdownPositions(context.Background(), f, "BTCUSDT")
	if err != nil || len(positions) != 3 {
		t.Fatalf("gross exposure discarded: %v %v", positions, err)
	}
}

func TestShutdownTerminalEvidenceCannotBeReplacedByLaterReport(t *testing.T) {
	for _, mutate := range []func(*exchange.Order){
		func(o *exchange.Order) { o.OrderID++ },
		func(o *exchange.Order) { o.ClientOrderID = "foreign" },
		func(o *exchange.Order) { o.ExecutedQty = math.NaN() },
		func(o *exchange.Order) { o.ExecutedQty = .4; o.Quantity = 0 },
		func(o *exchange.Order) { o.Quantity = 2 },
	} {
		f := &shutdownFaultExchange{fakeCloseExchange: newFakeCloseExchange(100, 1)}
		f.queryOrder = func(o *exchange.Order) (*exchange.Order, error) { mutate(o); return o, nil }
		failed, err := closeAllPositionsMarketable(context.Background(), f, "BTCUSDT", 100, fastCloseOpts())
		if failed == 0 || !errors.Is(err, errShutdownOrderEvidence) || f.submissions != 1 || f.cancellations != 0 {
			t.Fatalf("conflicting evidence reinterpreted: failed=%d err=%v", failed, err)
		}
	}
}

func TestShutdownAlreadyCanceledDoesNotSubmitOrQuery(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	f := &shutdownFaultExchange{fakeCloseExchange: newFakeCloseExchange(100, 1)}
	_, err := closeAllPositionsMarketable(ctx, f, "BTCUSDT", 100, fastCloseOpts())
	if !errors.Is(err, context.Canceled) || f.submissions != 0 || f.positionQueries != 0 {
		t.Fatalf("ignored cancellation: %v", err)
	}
}

func TestShutdownCanceledPollingDoesNotStartDetachedCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	f := &shutdownFaultExchange{fakeCloseExchange: newFakeCloseExchange(100, 1)}
	f.limitNeverFills = true
	_, err := closeAllPositionsMarketable(ctx, f, "BTCUSDT", 100, fastCloseOpts())
	if !errors.Is(err, context.DeadlineExceeded) || f.submissions != 1 || f.cancellations != 0 {
		t.Fatalf("detached post-cancel work: err=%v submit=%d cancel=%d", err, f.submissions, f.cancellations)
	}
}

func TestShutdownResidualDoesNotChaseNewRisk(t *testing.T) {
	before := []*exchange.Position{{Size: 1}}
	for _, qty := range []float64{2, -1} {
		if err := validateShutdownResidual(before, []*exchange.Position{{Size: qty}}); err == nil {
			t.Fatal("new exposure was eligible for fallback")
		}
	}
}

func TestShutdownUnknownPropagatesWithoutBotFallback(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		a := &SymbolRuntime{Config: config.SymbolConfig{Symbol: "BTCUSDT", Exchange: "binance", CloseOnStop: true}, AccountScope: "scope"}
		b := &SymbolRuntime{Config: a.Config, AccountScope: "scope"}
		b.Config.CloseOnStopConfig = config.ClosePositionConfig{Method: "limit"}
		rts := []*SymbolRuntime{a, b}
		if reverse {
			rts = []*SymbolRuntime{b, a}
		}
		calls := 0
		runProcessLevelCloseOnExit(true, rts, func(context.Context, []*SymbolRuntime) error {
			calls++
			return fmt.Errorf("uncertain: %w", errShutdownCloseUnverified)
		})
		for _, rt := range rts {
			if rt.shutdownCloseHandledReason() != "" || rt.shutdownCloseUnverifiedReason() == "" || shouldRunBotCloseOnStop(context.Background(), rt.Config, rt) {
				t.Fatal("unverified close cleared or allowed independent fallback")
			}
		}
		runProcessLevelCloseOnExit(true, rts, func(context.Context, []*SymbolRuntime) error { calls++; return nil })
		if calls != 1 {
			t.Fatalf("uncertain operation retried %d times", calls)
		}
	}
}

func TestShutdownDedupeUsesFullAccountScope(t *testing.T) {
	a := &SymbolRuntime{Config: config.SymbolConfig{Symbol: "BTCUSDT", Exchange: "binance"}, AccountID: "same-prefix", AccountScope: "first"}
	b := &SymbolRuntime{Config: a.Config, AccountID: a.AccountID, AccountScope: "second"}
	if shutdownRuntimeScopeKey(a) == shutdownRuntimeScopeKey(b) {
		t.Fatal("different accounts aliased")
	}
	a.AccountScope = ""
	b.AccountScope = ""
	if shutdownRuntimeScopeKey(a) == shutdownRuntimeScopeKey(b) {
		t.Fatal("missing account proof deduped")
	}
}
