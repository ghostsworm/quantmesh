package order

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"quantmesh/exchange"
	"quantmesh/execution"
	"quantmesh/lock"
)

type ownedTestVenue struct {
	exchange.IExchange
	mu          sync.Mutex
	orders      map[int64]*exchange.Order
	cancelled   []int64
	nextID      int64
	ackOnly     bool
	queryCount  int
	placeStart  chan struct{}
	placeFinish chan struct{}
}

func (*ownedTestVenue) GetName() string       { return "fake" }
func (*ownedTestVenue) GetMarketType() string { return "futures" }
func (v *ownedTestVenue) PlaceOrder(ctx context.Context, req *exchange.OrderRequest) (*exchange.Order, error) {
	v.mu.Lock()
	v.nextID++
	o := &exchange.Order{OrderID: v.nextID, ClientOrderID: req.ClientOrderID, Symbol: req.Symbol, Side: req.Side, Status: exchange.OrderStatusNew, Quantity: req.Quantity}
	v.orders[o.OrderID] = o
	v.mu.Unlock()
	if v.placeStart != nil {
		close(v.placeStart)
		select {
		case <-v.placeFinish:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return o, nil
}
func (v *ownedTestVenue) GetOpenOrders(context.Context, string) ([]*exchange.Order, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.queryCount++
	var result []*exchange.Order
	for _, o := range v.orders {
		if !terminalOrderStatus(string(o.Status)) {
			copy := *o
			result = append(result, &copy)
		}
	}
	return result, nil
}
func (v *ownedTestVenue) GetOrder(_ context.Context, _ string, id int64) (*exchange.Order, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if o := v.orders[id]; o != nil {
		copy := *o
		return &copy, nil
	}
	return nil, errors.New("not found")
}
func (v *ownedTestVenue) CancelOrder(_ context.Context, _ string, id int64) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.cancelled = append(v.cancelled, id)
	if !v.ackOnly {
		v.orders[id].Status = exchange.OrderStatusCanceled
	}
	return nil
}

func newOwnedTestExecutor() (*ExchangeOrderExecutor, *ownedTestVenue, *execution.OpeningGate) {
	v := &ownedTestVenue{orders: make(map[int64]*exchange.Order)}
	oe := NewExchangeOrderExecutor(v, "BTCUSDT", 0, 0, lock.NewNopLock(), "bot-a")
	gate := &execution.OpeningGate{}
	oe.SetOpeningGate(gate, "BOTH")
	return oe, v, gate
}

func TestOwnedCancellationPreservesProtectiveAndForeignOrders(t *testing.T) {
	oe, v, gate := newOwnedTestExecutor()
	for _, req := range []*OrderRequest{
		{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1, PositionSide: "LONG", ClientOrderID: "open-long"},
		{Symbol: "BTCUSDT", Side: "SELL", Price: 101, Quantity: 1, PositionSide: "SHORT", ClientOrderID: "open-short"},
		{Symbol: "BTCUSDT", Side: "SELL", Price: 99, Quantity: 1, ReduceOnly: true, ClientOrderID: "protective-close"},
	} {
		if _, err := oe.PlaceOrder(req); err != nil {
			t.Fatal(err)
		}
	}
	v.orders[100] = &exchange.Order{OrderID: 100, Symbol: "BTCUSDT", Side: exchange.SideBuy, ClientOrderID: "bot-b-open", Status: exchange.OrderStatusNew}
	v.orders[101] = &exchange.Order{OrderID: 101, Symbol: "BTCUSDT", Side: exchange.SideSell, ClientOrderID: "manual-close", Status: exchange.OrderStatusNew}
	gate.Block("manual")
	if err := oe.CancelOwnedOpeningOrders(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(v.cancelled) != 2 || v.orders[3].Status != exchange.OrderStatusNew || v.orders[100].Status != exchange.OrderStatusNew || v.orders[101].Status != exchange.OrderStatusNew {
		t.Fatalf("unsafe cancellation: %v", v.cancelled)
	}
}

func TestLoweringExposureLimitCancelsOwnedOpeningsAndKeepsGateOnUncertainty(t *testing.T) {
	for _, ackOnly := range []bool{false, true} {
		name := "verified cancellation"
		if ackOnly {
			name = "unverified cancellation"
		}
		t.Run(name, func(t *testing.T) {
			oe, venue, gate := newOwnedTestExecutor()
			venue.ackOnly = ackOnly
			book, err := execution.NewExposureBook(execution.ExposureLimits{Quantity: 2}, time.Minute)
			if err != nil {
				t.Fatal(err)
			}
			if err := book.Seed(nil); err != nil {
				t.Fatal(err)
			}
			if err := book.SetMark(100, time.Now()); err != nil {
				t.Fatal(err)
			}
			oe.SetExposureBook(book)
			for _, id := range []string{"limit-open-a", "limit-open-b"} {
				if _, err := oe.PlaceOrder(&OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1, PositionSide: "LONG", ClientOrderID: id}); err != nil {
					t.Fatal(err)
				}
			}
			if err := oe.SetExposureLimits(execution.ExposureLimits{Quantity: 1}); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(3 * time.Second)
			for time.Now().Before(deadline) {
				venue.mu.Lock()
				cancelled := len(venue.cancelled)
				venue.mu.Unlock()
				if cancelled == 2 && (ackOnly || !gate.Blocked()) {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			venue.mu.Lock()
			cancelled := len(venue.cancelled)
			venue.mu.Unlock()
			if cancelled != 2 {
				t.Fatalf("lowered ceiling did not cancel both owned orders: %d", cancelled)
			}
			if ackOnly {
				if !gate.HasBlock(ExposureLimitBlock) || !gate.Blocked() {
					t.Fatal("unverified cancellation released opening gate")
				}
				return
			}
			if gate.Blocked() {
				t.Fatal("verified cancellations did not clear limit-reduction gate")
			}
			if snapshot := book.Snapshot(time.Now()); snapshot.PendingQuantity != 0 {
				t.Fatalf("verified cancel results did not release pending quota: %+v", snapshot)
			}
		})
	}
}

func TestRisingMarkConcurrentUpdatesScheduleOneFailClosedCancellation(t *testing.T) {
	oe, venue, gate := newOwnedTestExecutor()
	venue.ackOnly = true
	book, err := execution.NewExposureBook(execution.ExposureLimits{Notional: 150}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := book.Seed(nil); err != nil {
		t.Fatal(err)
	}
	if err := book.SetMark(100, time.Now().Add(-time.Second)); err != nil {
		t.Fatal(err)
	}
	oe.SetExposureBook(book)
	for _, id := range []string{"rising-mark-open-a", "rising-mark-open-b"} {
		if _, err := oe.PlaceOrder(&OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: .5,
			PositionSide: "LONG", ClientOrderID: id}); err != nil {
			t.Fatal(err)
		}
	}

	markAt := time.Now()
	var updates sync.WaitGroup
	for range 32 {
		updates.Add(1)
		go func() {
			defer updates.Done()
			if err := oe.ObserveExposureMark(200, markAt); err != nil {
				t.Errorf("fresh mark: %v", err)
			}
		}()
	}
	updates.Wait()
	if !gate.HasBlock(ExposureLimitBlock) {
		t.Fatal("mark-driven exposure overage did not immediately block opening admission")
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		venue.mu.Lock()
		cancelled := len(venue.cancelled)
		venue.mu.Unlock()
		if cancelled == 2 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	venue.mu.Lock()
	cancelled := len(venue.cancelled)
	queryCount := venue.queryCount
	venue.mu.Unlock()
	if cancelled != 2 {
		t.Fatalf("mark-driven overage did not cancel both owned opening orders: %d", cancelled)
	}
	if snapshot := book.Snapshot(time.Now()); snapshot.PendingQuantity != 1 || snapshot.ProjectedNotional <= snapshot.Limits.Notional {
		t.Fatalf("unverified cancel acknowledgements must retain projected exposure: %+v", snapshot)
	}
	if !gate.HasBlock(ExposureLimitBlock) || !gate.Blocked() {
		t.Fatal("unverified cancellations released the exposure-limit gate")
	}
	if queryCount != 1 {
		t.Fatalf("concurrent mark updates started %d cancellation workflows, want exactly one", queryCount)
	}
}

func TestUnavailableExposureMarkCancelsOwnedOpeningsAndKeepsGateClosed(t *testing.T) {
	for _, ackOnly := range []bool{false, true} {
		name := "verified cancellation"
		if ackOnly {
			name = "unverified cancellation"
		}
		t.Run(name, func(t *testing.T) {
			oe, venue, gate := newOwnedTestExecutor()
			venue.ackOnly = ackOnly
			book, err := execution.NewExposureBook(execution.ExposureLimits{Notional: 500}, 50*time.Millisecond)
			if err != nil {
				t.Fatal(err)
			}
			if err := book.Seed(nil); err != nil {
				t.Fatal(err)
			}
			markAt := time.Now()
			if err := book.SetMark(100, markAt); err != nil {
				t.Fatal(err)
			}
			oe.SetExposureBook(book)
			if _, err := oe.PlaceOrder(&OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1,
				PositionSide: "LONG", ClientOrderID: "stale-mark-open"}); err != nil {
				t.Fatal(err)
			}

			time.Sleep(60 * time.Millisecond)
			if err := oe.ObserveExposureMark(100, markAt); err == nil {
				t.Fatal("stale exposure quote was accepted")
			}
			deadline := time.Now().Add(3 * time.Second)
			for time.Now().Before(deadline) {
				venue.mu.Lock()
				cancelled := len(venue.cancelled)
				venue.mu.Unlock()
				if cancelled == 1 {
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			venue.mu.Lock()
			cancelled := len(venue.cancelled)
			venue.mu.Unlock()
			if cancelled != 1 {
				t.Fatalf("unavailable quote did not cancel owned opening: %d", cancelled)
			}
			if !gate.HasBlock(ExposureLimitBlock) || !gate.Blocked() {
				t.Fatal("opening gate released without a usable exposure quote")
			}
			if ackOnly && !gate.HasBlock(execution.UnverifiedCancellationBlock) {
				t.Fatal("unverified cancellation did not retain its independent gate")
			}
			if err := oe.ObserveExposureMark(100, time.Now()); err != nil {
				t.Fatalf("fresh exposure quote: %v", err)
			}
			if ackOnly {
				if !gate.Blocked() {
					t.Fatal("fresh quote released an unverified cancellation")
				}
			} else if gate.Blocked() {
				t.Fatal("verified cancellation and recovered quote did not release the owned gate")
			}
		})
	}
}

func TestOwnedCancellationWaitsForInFlightSubmission(t *testing.T) {
	oe, v, gate := newOwnedTestExecutor()
	v.placeStart, v.placeFinish = make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		_, err := oe.PlaceOrder(&OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1, ClientOrderID: "in-flight"})
		done <- err
	}()
	<-v.placeStart
	gate.Block("manual")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := oe.CancelOwnedOpeningOrders(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("uncompleted call incorrectly drained: %v", err)
	}
	if v.queryCount != 0 {
		t.Fatal("queried residual orders before the admission drained")
	}
	close(v.placeFinish)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if err := oe.CancelOwnedOpeningOrders(t.Context()); err != nil || len(v.cancelled) != 1 {
		t.Fatalf("late accepted order escaped cancellation: %v %v", err, v.cancelled)
	}
}

func TestOwnedCancellationRequiresTerminalEvidence(t *testing.T) {
	oe, v, gate := newOwnedTestExecutor()
	if _, err := oe.PlaceOrder(&OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1, ClientOrderID: "open"}); err != nil {
		t.Fatal(err)
	}
	v.ackOnly = true
	gate.Block("manual")
	if err := oe.CancelOwnedOpeningOrders(t.Context()); err == nil {
		t.Fatal("cancel acknowledgement was mistaken for terminal state")
	}
	gate.Unblock("manual")
	if !gate.HasBlock(execution.UnverifiedCancellationBlock) {
		t.Fatal("manual resume cleared unverified cancellation")
	}
	if _, err := oe.PlaceOrder(&OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Price: 99, Quantity: 1}); !errors.Is(err, execution.ErrOpeningPaused) {
		t.Fatalf("unverified cancellation allowed new exposure: %v", err)
	}
	v.ackOnly = false
	if err := oe.CancelOwnedOpeningOrders(t.Context()); err != nil {
		t.Fatal(err)
	}
	if gate.Blocked() {
		t.Fatal("verified retry did not release its own hold")
	}
}

func TestOwnedCancellationCannotInventPauseAfterResume(t *testing.T) {
	oe, v, gate := newOwnedTestExecutor()
	gate.Block("manual")
	gate.Unblock("manual")
	if err := oe.CancelOwnedOpeningOrders(t.Context()); err == nil {
		t.Fatal("delayed cancellation began after resume")
	}
	if v.queryCount != 0 || gate.Blocked() {
		t.Fatal("delayed worker queried orders or re-paused a resumed bot")
	}
}

func TestOwnedCancellationDoesNotClearIndependentUnknownBlock(t *testing.T) {
	oe, _, gate := newOwnedTestExecutor()
	gate.Block("unknown_orders")
	if err := oe.CancelOwnedOpeningOrders(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !gate.HasBlock("unknown_orders") || gate.HasBlock(execution.UnverifiedCancellationBlock) {
		t.Fatal("successful cancellation changed another source's hold")
	}
}

func TestOwnedIntentDoesNotRegressEarlyTerminalReport(t *testing.T) {
	oe, _, _ := newOwnedTestExecutor()
	req := &OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1, ClientOrderID: "early-fill"}
	if err := oe.beginIntent(req); err != nil {
		t.Fatal(err)
	}
	oe.ObserveOrder(&exchange.Order{OrderID: 1, ClientOrderID: req.ClientOrderID, Status: exchange.OrderStatusFilled})
	oe.finishIntent(req, &Order{OrderID: 1, Status: "NEW"}, nil)
	if got := oe.snapshotOwnedIntents()[0].order.Status; got != "FILLED" {
		t.Fatalf("early fill regressed to %s", got)
	}
}

type delayedAdmissionLock struct {
	lock.DistributedLock
	started, finish chan struct{}
}

func (l *delayedAdmissionLock) TryLock(context.Context, string, time.Duration) (bool, error) {
	close(l.started)
	<-l.finish
	return true, nil
}

func TestPauseDuringLockWaitCannotUseAnOldAdmission(t *testing.T) {
	oe, v, gate := newOwnedTestExecutor()
	l := &delayedAdmissionLock{DistributedLock: lock.NewNopLock(), started: make(chan struct{}), finish: make(chan struct{})}
	oe.lock = l
	done := make(chan error, 1)
	go func() {
		_, err := oe.PlaceOrder(&OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1, ClientOrderID: "queued"})
		done <- err
	}()
	<-l.started
	gate.Block("manual")
	if err := oe.CancelOwnedOpeningOrders(t.Context()); err != nil {
		t.Fatal(err)
	}
	close(l.finish)
	if err := <-done; !errors.Is(err, execution.ErrOpeningPaused) {
		t.Fatalf("queued request escaped pause: %v", err)
	}
	if v.nextID != 0 {
		t.Fatalf("queued request reached venue after pause: %d", v.nextID)
	}
}
