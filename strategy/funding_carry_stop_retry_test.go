package strategy

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/exchange"
)

type fundingCarrySpotCloseIntentObserver struct {
	*mockFCExchange
	store          *memoryRuntimeStateStore
	called         bool
	intentInFlight bool
	intentPhase    string
}

func (e *fundingCarrySpotCloseIntentObserver) PlaceOrder(ctx context.Context, request *exchange.OrderRequest) (*exchange.Order, error) {
	e.called = true
	var state fundingCarryRuntimeState
	if err := json.Unmarshal([]byte(e.store.payload), &state); err != nil {
		return nil, err
	}
	e.intentInFlight = state.IntentInFlight
	e.intentPhase = state.IntentPhase
	return nil, errors.New("spot close acknowledgement lost")
}

func TestFundingCarryStopCancellationBeforeCloseCanRetry(t *testing.T) {
	s := NewFundingCarryStrategy("stop-preclose", nil, config.SymbolConfig{Symbol: "BTCUSDT"}, nil, nil, nil, nil)
	s.cancel = func() {}
	s.runDone = make(chan struct{})
	close(s.runDone)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	s.stopMu.Lock()
	locked := true
	defer func() {
		if locked {
			s.stopMu.Unlock()
		}
	}()
	result := make(chan error, 1)
	go func() { result <- s.StopContext(ctx) }()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for len(s.operationGate) != 0 {
		select {
		case <-deadline.C:
			t.Fatal("stop did not acquire operation gate")
		case <-ticker.C:
		}
	}
	cancel()
	s.stopMu.Unlock()
	locked = false
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("first stop = %v, want cancellation before close", err)
		}
	case <-deadline.C:
		t.Fatal("cancelled stop did not return")
	}
	if s.stopAttempted || s.stopErr != nil {
		t.Fatal("pre-close cancellation was latched as a financial attempt")
	}
	if err := s.StopContext(t.Context()); err != nil {
		t.Fatalf("retry with fresh context = %v; no financial action was submitted", err)
	}
	if !s.stopCompleted {
		t.Fatal("empty owned exposure did not finish stopping")
	}
}

func TestFundingCarryStoppedSpotClosePersistsDispatchPhaseBeforeOrder(t *testing.T) {
	store := &memoryRuntimeStateStore{}
	spot := &fundingCarrySpotCloseIntentObserver{
		mockFCExchange: &mockFCExchange{
			name: "binance", marketType: "spot", baseAsset: "BTC", balance: 0.01,
			latestPrice: 100, priceDecimals: 2, quantityDecimals: 8,
		},
		store: store,
	}
	futures := &mockFCExchange{name: "binance", marketType: "futures", baseAsset: "BTC", quantityDecimals: 8}
	s := NewFundingCarryStrategy("stop-owned-spot", nil, config.SymbolConfig{Symbol: "BTCUSDT"}, futures, spot, nil, nil)
	s.SetRuntimeStateStore(store)
	s.strategySpotKnown = true
	s.strategySpotQty = 0.01
	s.cancel = func() {}
	s.runDone = make(chan struct{})
	close(s.runDone)

	if err := s.StopContext(t.Context()); err == nil {
		t.Fatal("ambiguous spot close acknowledgement was accepted")
	}
	if !spot.called || !spot.intentInFlight || spot.intentPhase != fundingCarryIntentPhaseDispatching {
		t.Fatalf("spot order reached venue without durable dispatch intent: in_flight=%v phase=%q", spot.intentInFlight, spot.intentPhase)
	}
	if !s.intentInFlight || s.intentPhase != fundingCarryIntentPhaseDispatching || !s.unownedExposure {
		t.Fatalf("ambiguous stop close did not retain reconciliation hold: in_flight=%v phase=%q unknown=%v", s.intentInFlight, s.intentPhase, s.unownedExposure)
	}
}

func TestFundingCarryStoppedSpotCloseDoesNotReuseAnotherIntent(t *testing.T) {
	spot := &fundingCarrySpotCloseIntentObserver{mockFCExchange: &mockFCExchange{
		name: "binance", marketType: "spot", baseAsset: "BTC", balance: 0.01, latestPrice: 100,
		priceDecimals: 2, quantityDecimals: 8,
	}}
	futures := &mockFCExchange{name: "binance", marketType: "futures", baseAsset: "BTC", quantityDecimals: 8}
	s := NewFundingCarryStrategy("stop-existing-intent", nil, config.SymbolConfig{Symbol: "BTCUSDT"}, futures, spot, nil, nil)
	s.strategySpotKnown, s.strategySpotQty = true, 0.01
	s.intentInFlight, s.intentPhase = true, fundingCarryIntentPhaseDispatching
	s.cancel = func() {}
	s.runDone = make(chan struct{})
	close(s.runDone)

	if err := s.StopContext(t.Context()); err == nil {
		t.Fatal("standalone close reused another in-flight intent")
	}
	if spot.called {
		t.Fatal("standalone close submitted an order under another operation's intent")
	}
}

func TestFundingCarryLoadedExecutionHoldCannotInitializeOrdinaryTrading(t *testing.T) {
	s := NewFundingCarryStrategy("execution-hold", nil, config.SymbolConfig{Symbol: "BTCUSDT"}, nil, nil, nil, nil)
	store := &memoryRuntimeStateStore{}
	s.SetRuntimeStateStore(&borrowReceiptContextStore{store})
	s.RequireExecutionRecovery()
	if err := s.Start(t.Context()); err == nil || !strings.Contains(err.Error(), "shared execution intents") {
		t.Fatalf("pending execution admitted ordinary initialization: %v", err)
	}
	if s.started || store.found || store.payload != "" {
		t.Fatal("unresolved execution initialized producer or overwrote accounting")
	}
}

func TestFundingCarryStopUnknownRemainsLatched(t *testing.T) {
	s := NewFundingCarryStrategy("stop-unknown", nil, config.SymbolConfig{Symbol: "BTCUSDT"}, nil, nil, nil, nil)
	s.cancel = func() {}
	s.unownedExposure = true
	first := s.StopContext(t.Context())
	if first == nil || !s.stopAttempted || s.stopCompleted {
		t.Fatal("unknown exposure did not retain unresolved stop")
	}
	if IsFundingCarryFinalVerificationPending(first) {
		t.Fatal("generic unknown exposure became a final-verification retry candidate")
	}
	// Merely clearing an in-memory flag is not financial reconciliation.
	s.unownedExposure = false
	if retry := s.StopContext(t.Context()); retry != first || s.stopCompleted {
		t.Fatalf("unknown stop was replayed: %v", retry)
	}
}

func TestFundingCarryQuiesceWaitsWithoutFinancialStop(t *testing.T) {
	s := NewFundingCarryStrategy("quiesce", nil, config.SymbolConfig{Symbol: "BTCUSDT"}, nil, nil, nil, nil)
	s.cancel = func() {}
	s.runDone = make(chan struct{})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := s.QuiesceContext(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("producer wait = %v", err)
	}
	close(s.runDone)
	if err := s.acquireOperation(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := s.QuiesceContext(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("in-flight operation wait = %v", err)
	}
	s.releaseOperation()
	if err := s.QuiesceContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	if s.stopAttempted || s.stopCompleted || s.stopErr != nil {
		t.Fatal("quiesce performed or latched a financial stop")
	}
}
