package order

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"quantmesh/exchange"
	"quantmesh/execution"
	"quantmesh/lock"
)

type exposureEvidenceExchange struct {
	fakeOrderExchange
	place func(*exchange.OrderRequest) (*exchange.Order, error)
	calls int
}

type concurrentExposureExchange struct {
	fakeOrderExchange
	calls atomic.Int32
}

func (f *concurrentExposureExchange) PlaceOrder(_ context.Context, req *exchange.OrderRequest) (*exchange.Order, error) {
	id := f.calls.Add(1)
	return &exchange.Order{OrderID: int64(id), ClientOrderID: req.ClientOrderID, Symbol: req.Symbol,
		Side: req.Side, Price: req.Price, Quantity: req.Quantity, Status: exchange.OrderStatusNew}, nil
}

func (f *exposureEvidenceExchange) PlaceOrder(_ context.Context, req *exchange.OrderRequest) (*exchange.Order, error) {
	f.calls++
	return f.place(req)
}

func bindTestExposureBook(t *testing.T, oe *ExchangeOrderExecutor) *execution.ExposureBook {
	t.Helper()
	b, err := execution.NewExposureBook(execution.ExposureLimits{Quantity: 1}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Seed(nil); err != nil {
		t.Fatal(err)
	}
	if err := b.SetMark(100, time.Now()); err != nil {
		t.Fatal(err)
	}
	oe.SetExposureBook(b)
	return b
}

func TestExposureRESTMappingDoesNotManufactureConsistentFilledQuantity(t *testing.T) {
	for _, qty := range []float64{0, 1} {
		f := &exposureEvidenceExchange{place: func(req *exchange.OrderRequest) (*exchange.Order, error) {
			return &exchange.Order{OrderID: 1, ClientOrderID: req.ClientOrderID, Symbol: req.Symbol, Status: exchange.OrderStatusFilled, Quantity: qty, ExecutedQty: .4, AvgPrice: 100}, nil
		}}
		oe := NewExchangeOrderExecutor(f, "BTCUSDT", 0, 0, lock.NewNopLock(), "")
		book := bindTestExposureBook(t, oe)
		_, err := oe.PlaceOrder(&OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1, ClientOrderID: "one"})
		if !errors.Is(err, execution.ErrOrderUnknown) {
			t.Fatalf("inconsistent FILLED succeeded: %v", err)
		}
		if s := book.Snapshot(time.Now()); s.Ready || s.ProjectedQuantity != 1 {
			t.Fatalf("inconsistent mapping freed quota: %+v", s)
		}
	}
}

func TestExposureWSWithoutQuantityUsesSubmittedQuantity(t *testing.T) {
	f := &exposureEvidenceExchange{place: func(req *exchange.OrderRequest) (*exchange.Order, error) {
		return &exchange.Order{OrderID: 1, ClientOrderID: req.ClientOrderID, Symbol: req.Symbol, Status: exchange.OrderStatusNew}, nil
	}}
	oe := NewExchangeOrderExecutor(f, "BTCUSDT", 0, 0, lock.NewNopLock(), "")
	book := bindTestExposureBook(t, oe)
	if _, err := oe.PlaceOrder(&OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1, ClientOrderID: "one"}); err != nil {
		t.Fatal(err)
	}
	oe.ObserveOrder(&exchange.Order{OrderID: 1, ClientOrderID: "one", Symbol: "BTCUSDT", Status: exchange.OrderStatusFilled, ExecutedQty: .4})
	if s := book.Snapshot(time.Now()); s.Ready || s.ProjectedQuantity != 1 {
		t.Fatalf("incomplete WS terminal freed quota: %+v", s)
	}
	if !oe.IsOpeningPaused() {
		t.Fatal("inconsistent fill did not stop admission")
	}
}

func TestRequiredExposureBookBlocksOpeningWhenMissingButAllowsReduction(t *testing.T) {
	f := &exposureEvidenceExchange{place: func(req *exchange.OrderRequest) (*exchange.Order, error) {
		return &exchange.Order{OrderID: 1, ClientOrderID: req.ClientOrderID, Symbol: req.Symbol, Side: req.Side,
			Price: req.Price, Quantity: req.Quantity, Status: exchange.OrderStatusNew}, nil
	}}
	oe := NewExchangeOrderExecutor(f, "BTCUSDT", 0, 0, lock.NewNopLock(), "bot-a")
	oe.RequireExposureBook()

	_, err := oe.PlaceOrder(&OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1, ClientOrderID: "missing-book-open"})
	if !errors.Is(err, execution.ErrExposureUnverified) {
		t.Fatalf("opening without required exposure book error = %v, want ErrExposureUnverified", err)
	}
	if f.calls != 0 {
		t.Fatalf("opening without exposure book reached venue %d times", f.calls)
	}

	_, err = oe.PlaceOrder(&OrderRequest{Symbol: "BTCUSDT", Side: "SELL", Price: 100, Quantity: 1,
		ReduceOnly: true, PositionSide: "LONG", ClientOrderID: "missing-book-close"})
	if err != nil {
		t.Fatalf("risk-reducing close should remain available without exposure book: %v", err)
	}
	if f.calls != 1 {
		t.Fatalf("expected only the close to reach venue, got %d calls", f.calls)
	}
}

func TestSetExposureLimitsWithoutBookReturnsUnverifiedError(t *testing.T) {
	oe := NewExchangeOrderExecutor(nil, "BTCUSDT", 0, 0, lock.NewNopLock(), "bot-a")
	if err := oe.SetExposureLimits(execution.ExposureLimits{Notional: 100}); !errors.Is(err, execution.ErrExposureUnverified) {
		t.Fatalf("SetExposureLimits() error = %v, want ErrExposureUnverified", err)
	}
}

func TestObservedAcceptanceNeverRetriesContradictoryRESTRefusal(t *testing.T) {
	for _, withBook := range []bool{false, true} {
		for _, refusal := range []string{"code=-5022", "code=-1003"} {
			f := &exposureEvidenceExchange{}
			oe := NewExchangeOrderExecutor(f, "BTCUSDT", 0, 0, lock.NewNopLock(), "")
			if withBook {
				bindTestExposureBook(t, oe)
			}
			f.place = func(req *exchange.OrderRequest) (*exchange.Order, error) {
				oe.ObserveOrder(&exchange.Order{OrderID: 1, ClientOrderID: req.ClientOrderID, Symbol: req.Symbol, Status: exchange.OrderStatusNew, Quantity: req.Quantity})
				return nil, errors.New(refusal)
			}
			_, err := oe.PlaceOrder(&OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1, ClientOrderID: "one", PostOnly: true})
			if !errors.Is(err, execution.ErrOrderUnknown) || f.calls != 1 {
				t.Fatalf("contradictory refusal retried: calls=%d err=%v", f.calls, err)
			}
		}
	}
}

func TestExposureDuplicateCloseNeverReachesVenue(t *testing.T) {
	f := &exposureEvidenceExchange{place: func(req *exchange.OrderRequest) (*exchange.Order, error) {
		return &exchange.Order{OrderID: 1, ClientOrderID: req.ClientOrderID, Symbol: req.Symbol, Status: exchange.OrderStatusNew, Quantity: req.Quantity}, nil
	}}
	oe := NewExchangeOrderExecutor(f, "BTCUSDT", 0, 0, lock.NewNopLock(), "")
	book, err := execution.NewExposureBook(execution.ExposureLimits{}, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if err := book.Seed([]execution.ExposurePosition{{Key: "held", Group: "grid", Leg: "LONG", Quantity: 1}}); err != nil {
		t.Fatal(err)
	}
	oe.SetExposureBook(book)
	// No fresh mark is necessary to reduce known inventory.
	for n, id := range []string{"first", "duplicate"} {
		_, err := oe.PlaceOrder(&OrderRequest{Symbol: "BTCUSDT", Side: "SELL", Price: 100, Quantity: 1, ReduceOnly: true, PositionSide: "LONG", ClientOrderID: id})
		if n == 0 && err != nil {
			t.Fatal(err)
		}
		if n == 1 && !errors.Is(err, execution.ErrExposureLimit) {
			t.Fatalf("duplicate close admitted: %v", err)
		}
	}
	if f.calls != 1 {
		t.Fatalf("physical submissions=%d", f.calls)
	}
}

func TestConcurrentStrategyOpeningsCannotExceedSharedLayerLimit(t *testing.T) {
	const requestCount = 24
	venue := &concurrentExposureExchange{}
	oe := NewExchangeOrderExecutor(venue, "BTCUSDT", 0, 0, lock.NewNopLock(), "bot-a")
	book := bindTestExposureBook(t, oe)
	if err := book.SetLimits(execution.ExposureLimits{Quantity: requestCount, Layers: 1}); err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	var workers sync.WaitGroup
	var accepted atomic.Int32
	var failuresMu sync.Mutex
	var failures []error
	for i := 0; i < requestCount; i++ {
		workers.Add(1)
		go func(index int) {
			defer workers.Done()
			<-start
			id := fmt.Sprintf("strategy-%d-open", index)
			_, err := oe.PlaceOrder(&OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1,
				StrategyName: fmt.Sprintf("strategy-%d", index), ExposureKey: id, ClientOrderID: id})
			if err == nil {
				accepted.Add(1)
				return
			}
			if errors.Is(err, execution.ErrExposureLimit) {
				return
			}
			failuresMu.Lock()
			failures = append(failures, err)
			failuresMu.Unlock()
		}(i)
	}
	close(start)
	workers.Wait()

	if len(failures) != 0 {
		t.Fatalf("unexpected concurrent submission errors: %v", failures)
	}
	if got := accepted.Load(); got != 1 {
		t.Fatalf("accepted openings = %d, want exactly one under a one-layer cap", got)
	}
	if got := venue.calls.Load(); got != 1 {
		t.Fatalf("physical venue submissions = %d, want exactly one", got)
	}
	snapshot := book.Snapshot(time.Now())
	if snapshot.Layers != 1 || snapshot.ProjectedQuantity != 1 || snapshot.PendingQuantity != 1 {
		t.Fatalf("shared exposure book exceeded or lost its one-layer reservation: %+v", snapshot)
	}
}
