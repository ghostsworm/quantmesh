package position

import (
	"context"
	"errors"
	"math"
	"sync"
	"testing"
	"time"

	"quantmesh/config"
	"quantmesh/execution"
)

type closeWorkflowVenue struct {
	mu                  sync.Mutex
	requests            []ExchangeOrderRequest
	canceled            bool
	cancelCalls         int
	ackOnly             bool
	partial, cancelFill float64
	queryErr, placeErr  error
	malformed           string
	queryEntered        chan struct{}
	queryOnce           sync.Once
}

func (*closeWorkflowVenue) GetName() string                                         { return "fake" }
func (*closeWorkflowVenue) GetLatestPrice(context.Context, string) (float64, error) { return 100, nil }
func (v *closeWorkflowVenue) PlaceOrder(ctx context.Context, r *ExchangeOrderRequest) (*ExchangeOrder, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.requests = append(v.requests, *r)
	if v.placeErr != nil {
		return nil, v.placeErr
	}
	o := &ExchangeOrder{OrderID: int64(len(v.requests)), ClientOrderID: r.ClientOrderID, Symbol: r.Symbol, Side: r.Side, Quantity: r.Quantity, Status: "NEW"}
	if r.Type == "MARKET" {
		o.Status, o.ExecutedQty, o.AvgPrice = "FILLED", r.Quantity, 100
	}
	return o, nil
}
func (v *closeWorkflowVenue) GetOrder(ctx context.Context, symbol string, id int64) (*ExchangeOrder, error) {
	if v.queryEntered != nil {
		v.queryOnce.Do(func() { close(v.queryEntered) })
		<-ctx.Done()
		return nil, ctx.Err()
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.queryErr != nil {
		return nil, v.queryErr
	}
	r := v.requests[id-1]
	o := &ExchangeOrder{OrderID: id, ClientOrderID: r.ClientOrderID, Symbol: symbol, Side: r.Side, Quantity: r.Quantity, Status: "NEW", ExecutedQty: v.partial, AvgPrice: 100}
	if o.ExecutedQty > 0 {
		o.Status = "PARTIALLY_FILLED"
	}
	if v.canceled && !v.ackOnly {
		o.Status, o.ExecutedQty = "CANCELED", v.cancelFill
	}
	if r.Type == "MARKET" {
		o.Status, o.ExecutedQty = "FILLED", r.Quantity
	}
	switch v.malformed {
	case "foreign":
		o.ClientOrderID = "foreign"
	case "nan":
		o.ExecutedQty = math.NaN()
	case "nil":
		return nil, nil
	case "false_filled":
		o.Status, o.ExecutedQty = "FILLED", 0
	case "overfilled":
		o.ExecutedQty = 2
	case "no_average":
		o.ExecutedQty, o.AvgPrice = 0.5, 0
	}
	return o, nil
}
func (v *closeWorkflowVenue) CancelOrder(context.Context, string, int64) error {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.canceled = true
	v.cancelCalls++
	return nil
}

func startExpiringClose(t *testing.T, v *closeWorkflowVenue) (*ClosePositionManager, *ClosePositionRecord) {
	t.Helper()
	m := NewClosePositionManager(v, "bot", "BTCUSDT")
	m.pollInterval = time.Millisecond
	t.Cleanup(m.Stop)
	r, err := m.ClosePositions(t.Context(), "SELL", 1, config.ClosePositionConfig{Method: "limit", TimeoutSec: 1, AutoRetry: true, MaxRetries: 3})
	if err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	op := m.records[r.RecordID]
	m.mu.Unlock()
	op.mu.Lock()
	op.record.TimeoutAt = time.Now().Add(-time.Second)
	op.mu.Unlock()
	return m, r
}

func TestManagedClosePartialCancelRetryUsesVerifiedRemainderAndSameRecord(t *testing.T) {
	v := &closeWorkflowVenue{partial: 0.25, cancelFill: 0.6}
	m, r := startExpiringClose(t, v)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	final, err := m.WaitRecord(ctx, r.RecordID)
	if err != nil {
		t.Fatal(err)
	}
	if final.FilledQty != 1 || final.RetryCount != 1 || final.Status != CloseStatusFilled || len(m.ListRecords()) != 1 {
		t.Fatalf("lost aggregate record: %+v", final)
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if len(v.requests) != 2 || v.cancelCalls != 1 || v.requests[1].Quantity != 0.4 || v.requests[1].Type != "MARKET" || v.requests[0].ClientOrderID == v.requests[1].ClientOrderID {
		t.Fatalf("unsafe replacement: %+v", v.requests)
	}
	if r.OrderID != 1 || r.Method != CloseMethodLimit {
		t.Fatal("returned snapshot was mutated")
	}
}

func TestManagedCloseFullFillDuringCancellationNeedsNoReplacement(t *testing.T) {
	v := &closeWorkflowVenue{partial: 0.5, cancelFill: 1}
	m, r := startExpiringClose(t, v)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if _, err := m.WaitRecord(ctx, r.RecordID); err != nil {
		t.Fatal(err)
	}
	v.mu.Lock()
	defer v.mu.Unlock()
	if len(v.requests) != 1 {
		t.Fatal("extra market order after full fill")
	}
}

func TestManagedCloseUncertainEvidenceNeverRetries(t *testing.T) {
	for _, scenario := range []string{"ack_only", "query_error", "foreign", "nan", "nil", "false_filled", "overfilled", "no_average", "regression"} {
		t.Run(scenario, func(t *testing.T) {
			v := &closeWorkflowVenue{}
			switch scenario {
			case "ack_only":
				v.ackOnly = true
			case "query_error":
				v.queryErr = errors.New("query failed")
			case "regression":
				v.partial = 0.5
			default:
				v.malformed = scenario
			}
			m, r := startExpiringClose(t, v)
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			final, err := m.WaitRecord(ctx, r.RecordID)
			if err == nil || final.Status != CloseStatusUnknown {
				t.Fatalf("unknown became success: %+v %v", final, err)
			}
			if _, err := m.ClosePositions(t.Context(), "SELL", 1, config.ClosePositionConfig{Method: "market"}); !errors.Is(err, execution.ErrIntentPending) {
				t.Fatalf("second close admitted: %v", err)
			}
			v.mu.Lock()
			defer v.mu.Unlock()
			if len(v.requests) != 1 {
				t.Fatal("blind resubmit")
			}
		})
	}
}

func TestManagedCloseStopCancelsPollingAndRejectsNewRequests(t *testing.T) {
	v := &closeWorkflowVenue{queryEntered: make(chan struct{})}
	m := NewClosePositionManager(v, "bot", "BTCUSDT")
	r, err := m.ClosePositions(t.Context(), "SELL", 1, config.ClosePositionConfig{Method: "limit"})
	if err != nil {
		t.Fatal(err)
	}
	<-v.queryEntered
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if err := m.StopContext(ctx); err != nil {
		t.Fatal(err)
	}
	if final, _ := m.GetRecord(r.RecordID); final.Status != CloseStatusUnknown {
		t.Fatalf("lost live order on stop: %+v", final)
	}
	if _, err := m.ClosePositions(t.Context(), "SELL", 1, config.ClosePositionConfig{Method: "market"}); err == nil {
		t.Fatal("stopped manager accepted submission")
	}
}

func TestManagedCloseUnknownSubmissionRetainsCIDAndRecord(t *testing.T) {
	v := &closeWorkflowVenue{placeErr: execution.ErrOrderUnknown}
	m := NewClosePositionManager(v, "bot", "BTCUSDT")
	defer m.Stop()
	r, err := m.ClosePositions(t.Context(), "SELL", 1, config.ClosePositionConfig{Method: "market"})
	if !errors.Is(err, execution.ErrOrderUnknown) || r.Status != CloseStatusUnknown || r.ClientOrderID == "" || len(m.ListRecords()) != 1 {
		t.Fatalf("lost unknown: %+v %v", r, err)
	}
}

type rejectingCloseObservation struct {
	*closeWorkflowVenue
	observations int
}

func (v *rejectingCloseObservation) ConfirmCloseOrder(*ExchangeOrder) error {
	v.observations++
	if v.observations > 1 {
		return errors.New("journal write failed")
	}
	return nil
}

func TestManagedCloseDoesNotAdvanceFillBeforeDurableObservation(t *testing.T) {
	v := &rejectingCloseObservation{closeWorkflowVenue: &closeWorkflowVenue{partial: 0.5}}
	m := NewClosePositionManager(v, "bot", "BTCUSDT")
	defer m.Stop()
	r, err := m.ClosePositions(t.Context(), "SELL", 1, config.ClosePositionConfig{Method: "limit"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	final, err := m.WaitRecord(ctx, r.RecordID)
	if err == nil || final.Status != CloseStatusUnknown || final.FilledQty != 0 {
		t.Fatalf("unlogged fill advanced state: %+v %v", final, err)
	}
}

func TestManagedCloseRejectsConcurrentIntentAndReturnsIndependentSnapshots(t *testing.T) {
	v := &closeWorkflowVenue{queryEntered: make(chan struct{})}
	m := NewClosePositionManager(v, "bot", "BTCUSDT")
	defer m.Stop()
	r, err := m.ClosePositions(t.Context(), "SELL", 1, config.ClosePositionConfig{Method: "limit"})
	if err != nil {
		t.Fatal(err)
	}
	<-v.queryEntered
	if _, err := m.ClosePositions(t.Context(), "SELL", 1, config.ClosePositionConfig{Method: "market"}); !errors.Is(err, execution.ErrIntentPending) {
		t.Fatalf("concurrent economic close admitted: %v", err)
	}
	copy, _ := m.GetRecord(r.RecordID)
	copy.FilledQty = 100
	copy.Status = CloseStatusFilled
	actual, _ := m.GetRecord(r.RecordID)
	if actual.FilledQty != 0 || actual.Status != CloseStatusPending {
		t.Fatal("caller mutated live record")
	}
}
