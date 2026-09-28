package order

import (
	"context"
	"errors"
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
