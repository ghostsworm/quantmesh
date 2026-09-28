package order

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"

	"quantmesh/exchange"
	"quantmesh/execution"
	"quantmesh/lock"
)

func shutdownRequest(cid string, close bool) *OrderRequest {
	r := &OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1, ClientOrderID: cid}
	if close {
		r.Side, r.ReduceOnly = "SELL", true
	}
	return r
}

func TestShutdownDrainsOpeningAndClosingSubmissions(t *testing.T) {
	for _, closing := range []bool{false, true} {
		t.Run(map[bool]string{false: "open", true: "close"}[closing], func(t *testing.T) {
			oe, v, gate := newOwnedTestExecutor()
			v.placeStart, v.placeFinish = make(chan struct{}), make(chan struct{})
			done := make(chan error, 1)
			go func() { _, err := oe.PlaceOrder(shutdownRequest("inflight", closing)); done <- err }()
			<-v.placeStart
			oe.BeginShutdown()
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Millisecond)
			defer cancel()
			if err := oe.DrainShutdown(ctx); !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("in-flight submission escaped drain: %v", err)
			}
			if err := oe.CancelOwnedShutdownOrders(ctx); err == nil || len(v.cancelled) != 0 {
				t.Fatalf("cancelled before drain: %v", err)
			}
			close(v.placeFinish)
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			if err := oe.DrainShutdown(t.Context()); err != nil {
				t.Fatal(err)
			}
			if err := oe.CancelOwnedShutdownOrders(t.Context()); err != nil {
				t.Fatal(err)
			}
			if len(v.cancelled) != 1 || !gate.HasBlock(RuntimeShutdownBlock) {
				t.Fatal("lost owned order or shutdown hold")
			}
		})
	}
}

func TestShutdownClosePermissionIsScopedAndNeverOpens(t *testing.T) {
	oe, v, gate := newOwnedTestExecutor()
	if _, err := oe.ShutdownCloseContext(t.Context()); err == nil {
		t.Fatal("permission before shutdown")
	}
	oe.BeginShutdown()
	gate.Unblock("opening_manager")
	if _, err := oe.PlaceOrder(shutdownRequest("ordinary-close", true)); !errors.Is(err, ErrRuntimeStopping) {
		t.Fatalf("ordinary close escaped: %v", err)
	}
	ctx, err := oe.ShutdownCloseContext(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := oe.PlaceOrderContext(ctx, shutdownRequest("new-open", false)); !errors.Is(err, execution.ErrOpeningPaused) {
		t.Fatalf("shutdown permit opened: %v", err)
	}
	other, _, _ := newOwnedTestExecutor()
	other.BeginShutdown()
	if _, err := other.PlaceOrderContext(ctx, shutdownRequest("foreign-permit", true)); !errors.Is(err, ErrRuntimeStopping) {
		t.Fatalf("foreign permit accepted: %v", err)
	}
	if _, err := oe.PlaceOrderContext(ctx, shutdownRequest("shutdown-close", true)); err != nil {
		t.Fatal(err)
	}
	if len(v.orders) != 1 {
		t.Fatalf("unexpected sends: %d", len(v.orders))
	}
}

func TestShutdownClosePermissionsComposeAcrossIndependentExecutors(t *testing.T) {
	first, firstVenue, _ := newOwnedTestExecutor()
	second, secondVenue, _ := newOwnedTestExecutor()
	first.BeginShutdown()
	second.BeginShutdown()
	ctx, err := first.ShutdownCloseContext(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	ctx, err = second.ShutdownCloseContext(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := first.PlaceOrderContext(ctx, shutdownRequest("first-close", true)); err != nil {
		t.Fatalf("first executor lost close permission: %v", err)
	}
	if _, err := second.PlaceOrderContext(ctx, shutdownRequest("second-close", true)); err != nil {
		t.Fatalf("second executor lost close permission: %v", err)
	}
	if len(firstVenue.orders) != 1 || len(secondVenue.orders) != 1 {
		t.Fatalf("composed permits did not reach both owners: %d %d", len(firstVenue.orders), len(secondVenue.orders))
	}
	if _, err := first.PlaceOrderContext(ctx, shutdownRequest("first-open", false)); !errors.Is(err, execution.ErrOpeningPaused) {
		t.Fatalf("composed close permit admitted a new open: %v", err)
	}
}

func TestShutdownRejectsOrdinaryCloseQueuedAtDistributedLock(t *testing.T) {
	oe, v, _ := newOwnedTestExecutor()
	l := &delayedAdmissionLock{DistributedLock: lock.NewNopLock(), started: make(chan struct{}), finish: make(chan struct{})}
	oe.lock = l
	done := make(chan error, 1)
	go func() { _, err := oe.PlaceOrder(shutdownRequest("queued", true)); done <- err }()
	<-l.started
	oe.BeginShutdown()
	if err := oe.DrainShutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
	close(l.finish)
	if err := <-done; !errors.Is(err, ErrRuntimeStopping) {
		t.Fatalf("queued close escaped: %v", err)
	}
	if len(v.orders) != 0 {
		t.Fatal("queued order reached venue")
	}
}

func TestShutdownCancellationKeepsForeignOrders(t *testing.T) {
	oe, v, _ := newOwnedTestExecutor()
	for i, closing := range []bool{false, true} {
		if _, err := oe.PlaceOrder(shutdownRequest([]string{"open", "close"}[i], closing)); err != nil {
			t.Fatal(err)
		}
	}
	v.orders[90] = &exchange.Order{OrderID: 90, Symbol: "BTCUSDT", ClientOrderID: "other-bot", Status: exchange.OrderStatusNew}
	v.orders[91] = &exchange.Order{OrderID: 91, Symbol: "BTCUSDT", ClientOrderID: "manual-protection", Status: exchange.OrderStatusNew}
	oe.BeginShutdown()
	if err := oe.CancelOwnedShutdownOrders(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(v.cancelled) != 2 || v.queryCount != 0 || v.orders[90].Status != exchange.OrderStatusNew || v.orders[91].Status != exchange.OrderStatusNew {
		t.Fatalf("unsafe sweep: %v", v.cancelled)
	}
	if err := oe.CancelOwnedShutdownOrders(t.Context()); err != nil || len(v.cancelled) != 2 {
		t.Fatalf("repeat cancelled settled orders: %v", err)
	}
}

func TestShutdownCancellationRequiresValidTerminalEvidence(t *testing.T) {
	for _, scenario := range []string{"ack_only", "foreign_cid", "new_fill", "nan", "regressed", "missing_identity", "unknown", "journal_failure", "missing_average"} {
		t.Run(scenario, func(t *testing.T) {
			oe, v, _ := newOwnedTestExecutor()
			if scenario == "journal_failure" {
				if err := oe.ConfigureIntentJournal(t.Context(), &memoryIntentJournal{failWrite: 4}, journalScope()); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := oe.PlaceOrder(shutdownRequest("owned", false)); err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "ack_only":
				v.ackOnly = true
			case "foreign_cid":
				v.orders[1].ClientOrderID = "somebody-else"
			case "new_fill":
				v.orders[1].ExecutedQty, v.orders[1].AvgPrice = 0.5, 100
			case "nan":
				v.orders[1].ExecutedQty = math.NaN()
			case "missing_average":
				v.orders[1].ExecutedQty = 0.5
			case "regressed":
				oe.intents["owned"].order.ExecutedQty = 0.5
			case "missing_identity":
				oe.intents["owned"].order = nil
			case "unknown":
				oe.intents["owned"].unknown = true
			}
			oe.BeginShutdown()
			if err := oe.CancelOwnedShutdownOrders(t.Context()); err == nil {
				t.Fatal("unverified cancellation reported success")
			}
			if !oe.intents["owned"].unknown {
				t.Fatal("uncertainty lost")
			}
			if scenario == "foreign_cid" && len(v.cancelled) != 0 {
				t.Fatal("cancelled another owner's ID")
			}
		})
	}
}

func TestShutdownCannotTreatMissingJournalAsNoOwnedOrders(t *testing.T) {
	oe, v, _ := newOwnedTestExecutor()
	if err := oe.ConfigureIntentJournal(t.Context(), nil, journalScope()); err == nil {
		t.Fatal("missing journal accepted")
	}
	oe.BeginShutdown()
	if err := oe.CancelOwnedShutdownOrders(t.Context()); err == nil || v.queryCount != 0 || len(v.cancelled) != 0 {
		t.Fatalf("missing history treated as empty: %v", err)
	}
}
