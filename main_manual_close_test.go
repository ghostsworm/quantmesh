package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/exchange"
)

// The venue accepts every order but never fills it. Queries return a real NEW
// receipt; a cancellation ACK is deliberately not a terminal-state proof.
type pendingManualExchange struct{ *fakeCloseExchange }

func (f *pendingManualExchange) PlaceOrder(_ context.Context, req *exchange.OrderRequest) (*exchange.Order, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	f.placed = append(f.placed, *req)
	o := &exchange.Order{OrderID: f.nextID, ClientOrderID: req.ClientOrderID, Symbol: req.Symbol,
		Side: req.Side, Type: req.Type, Quantity: req.Quantity, Status: exchange.OrderStatusNew}
	f.orders[o.OrderID] = o
	copy := *o
	return &copy, nil
}
func (f *pendingManualExchange) CancelOrder(context.Context, string, int64) error { return nil }

func TestManualCloseAcceptedButUnfilledNeverReportsSuccess(t *testing.T) {
	f := &pendingManualExchange{newFakeCloseExchange(100, 1)}
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	success, failed, err := closePositionsVerifiedResult(ctx, f, "BTCUSDT", 0, fastCloseOpts())
	if success != 0 || failed != 1 || !errors.Is(err, errShutdownCloseUnverified) || f.position != 1 || len(f.placed) != 1 {
		t.Fatalf("ACK mistaken for flat: success=%d failed=%d err=%v remaining=%v submissions=%d", success, failed, err, f.position, len(f.placed))
	}
}

func TestManualCloseConfirmedCountsAndOtherOrders(t *testing.T) {
	for _, qty := range []float64{0, 1, -1} {
		f := newFakeCloseExchange(100, qty)
		f.orders[999] = &exchange.Order{OrderID: 999, Symbol: "BTCUSDT", Status: exchange.OrderStatusNew}
		success, failed, err := closePositionsVerifiedResult(t.Context(), f, "BTCUSDT", 0, fastCloseOpts())
		want := 1
		if qty == 0 {
			want = 0
		}
		if err != nil || success != want || failed != 0 || f.position != 0 || f.orders[999].Status != exchange.OrderStatusNew {
			t.Fatalf("qty=%v success=%d failed=%d err=%v remaining=%v unrelated=%s", qty, success, failed, err, f.position, f.orders[999].Status)
		}
	}
}

func TestManualCloseFinalQueryOutageCannotCountSuccessfulSubmission(t *testing.T) {
	f := &shutdownFaultExchange{fakeCloseExchange: newFakeCloseExchange(100, 1)}
	f.queryPositions = func(n int) ([]*exchange.Position, error) {
		if n >= 3 {
			return nil, errors.New("final query unavailable")
		}
		return f.fakeCloseExchange.GetPositions(t.Context(), "BTCUSDT")
	}
	success, failed, err := closePositionsVerifiedResult(t.Context(), f, "BTCUSDT", 100, fastCloseOpts())
	if success != 0 || failed != 1 || !errors.Is(err, errShutdownCloseUnverified) || f.position != 0 {
		t.Fatalf("unverified success: success=%d failed=%d err=%v", success, failed, err)
	}
}

func TestManualCloseMarketFallbackCountsOnlyFinalVerifiedPositions(t *testing.T) {
	f := newFakeCloseExchange(100, 1)
	f.limitNeverFills = true
	success, failed, err := closePositionsVerifiedResult(t.Context(), f, "BTCUSDT", 100, fastCloseOpts())
	if err != nil || success != 1 || failed != 0 || len(f.placed) != 2 || f.placed[1].Type != exchange.OrderTypeMarket || f.position != 0 {
		t.Fatalf("fallback: success=%d failed=%d err=%v submissions=%d", success, failed, err, len(f.placed))
	}
}

func newManualRuntime(t *testing.T, sm *SymbolManager, id, scope string, ex exchange.IExchange) *SymbolRuntime {
	t.Helper()
	rt := newShutdownRuntime(ex, id, scope)
	rt.Config.Exchange = "binance"
	sm.Add(rt)
	return rt
}

func newManualManager() *SymbolManager {
	return NewSymbolManager(&config.Config{Exchanges: map[string]config.ExchangeConfig{"binance": {}}}, nil, nil, nil, "")
}

func TestManualCloseRefusesAmbiguousSharedAccountPosition(t *testing.T) {
	sm := newManualManager()
	f := &shutdownFaultExchange{fakeCloseExchange: newFakeCloseExchange(100, 1)}
	a := newManualRuntime(t, sm, "a", "account-a", f)
	newManualRuntime(t, sm, "b", "account-a", f)
	c := newManualRuntime(t, sm, "c", "account-c", newFakeCloseExchange(100, 1))
	_, _, err := sm.closeLegacyPositions(t.Context(), a)
	if err == nil || f.submissions != 0 || f.positionQueries != 0 {
		t.Fatalf("ambiguous shared position reached exchange: err=%v submits=%d queries=%d", err, f.submissions, f.positionQueries)
	}
	if c.shutdownCloseUnverifiedReason() != "" || c.SuperPositionManager.IsOpeningPaused() {
		t.Fatal("another account was blocked")
	}
	if a.shutdownCloseUnverifiedReason() != "" || a.SuperPositionManager.IsOpeningPaused() {
		t.Fatal("preflight refusal poisoned Bot recovery state")
	}
}

func TestManualCloseBlocksAllPeersAndPreservesIndependentPause(t *testing.T) {
	sm := newManualManager()
	f := &shutdownFaultExchange{fakeCloseExchange: newFakeCloseExchange(100, 1)}
	a := newManualRuntime(t, sm, "a", "account-a", f)
	b := newManualRuntime(t, sm, "b", "account-a", f)
	b.SuperPositionManager.OpeningGate().Block("independent-risk")
	f.queryPositions = func(int) ([]*exchange.Position, error) {
		for _, rt := range []*SymbolRuntime{a, b} {
			if !rt.SuperPositionManager.OpeningGate().HasBlock(legacyManualCloseBlock) {
				t.Error("peer admitted new opening during close")
			}
		}
		return f.fakeCloseExchange.GetPositions(t.Context(), "BTCUSDT")
	}
	if success, failed, err := sm.closeLegacyPositions(t.Context(), a); err == nil || success != 0 || failed == 0 {
		t.Fatalf("ambiguous shared ownership was not rejected: %d %d %v", success, failed, err)
	}
	if a.SuperPositionManager.IsOpeningPaused() || !b.SuperPositionManager.OpeningGate().HasBlock("independent-risk") || a.SuperPositionManager.OpeningGate().HasBlock(legacyManualCloseBlock) || b.SuperPositionManager.OpeningGate().HasBlock(legacyManualCloseBlock) {
		t.Fatal("ambiguous close left a temporary hold or affected an independent pause")
	}
}

func TestManualCloseBusyAndUnfinishedOpeningDoNotSubmit(t *testing.T) {
	sm := newManualManager()
	f := &shutdownFaultExchange{fakeCloseExchange: newFakeCloseExchange(100, 1)}
	rt := newManualRuntime(t, sm, "a", "account-a", f)
	sm.legacyCloseMu.Lock()
	_, _, err := sm.closeLegacyPositions(t.Context(), rt)
	sm.legacyCloseMu.Unlock()
	if err == nil || f.submissions != 0 || f.positionQueries != 0 {
		t.Fatal("concurrent close was admitted")
	}
	release, err := rt.SuperPositionManager.OpeningGate().Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	_, _, err = sm.closeLegacyPositions(ctx, rt)
	if !errors.Is(err, context.DeadlineExceeded) || f.submissions != 0 || f.positionQueries != 0 || rt.shutdownCloseUnverifiedReason() != "" {
		t.Fatalf("undrained opening: %v", err)
	}
}

func TestManualCloseMissingIdentityAndQueryFailureDoNotPoisonRetry(t *testing.T) {
	sm := newManualManager()
	f := &shutdownFaultExchange{fakeCloseExchange: newFakeCloseExchange(100, 1)}
	rt := newManualRuntime(t, sm, "a", "", f)
	if _, _, err := sm.closeLegacyPositions(t.Context(), rt); err == nil || f.positionQueries != 0 {
		t.Fatal("missing identity reached venue")
	}
	rt.AccountScope = "account-a"
	f.queryPositions = func(int) ([]*exchange.Position, error) { return nil, errors.New("offline before submission") }
	if _, _, err := sm.closeLegacyPositions(t.Context(), rt); err == nil || f.submissions != 0 || rt.SuperPositionManager.IsOpeningPaused() {
		t.Fatal("pre-submit error did not release temporary gate")
	}
	f.queryPositions = nil
	if success, failed, err := sm.closeLegacyPositions(t.Context(), rt); err == nil || success != 0 || failed == 0 || f.submissions != 0 {
		t.Fatalf("unowned position was not rejected on retry: success=%d failed=%d err=%v submits=%d", success, failed, err, f.submissions)
	}
}

func TestManualCloseWebAdapterReturnsOnlyVerifiedResult(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		sm := newManualManager()
		f := &shutdownFaultExchange{fakeCloseExchange: newFakeCloseExchange(100, 1)}
		if unknown {
			f.placeError = errors.New("acknowledgement lost")
		}
		id := config.GenerateBotID("binance", "BTCUSDT", "futures")
		newManualRuntime(t, sm, id, "account-a", f)
		adapter := &symbolManagerWebAdapter{manager: sm, ctx: t.Context(), cfg: sm.botManager.cfg}
		result, err := adapter.ClosePositions("binance", "BTCUSDT")
		if result != nil || err == nil || errors.Is(err, errShutdownCloseUnverified) || f.submissions != 0 || f.position != 1 {
			t.Fatalf("unowned account position was not rejected before order submission: unknown=%v result=%+v err=%v submits=%d pos=%v", unknown, result, err, f.submissions, f.position)
		}
	}
}
