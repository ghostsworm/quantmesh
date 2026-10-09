package order

import (
	"errors"
	"sync/atomic"
	"testing"

	"quantmesh/execution"
)

func TestOwnedCancellationFromLiveRiskFailureRetainsHoldUntilVerified(t *testing.T) {
	oe, venue, gate := newOwnedTestExecutor()
	var healthy atomic.Bool
	healthy.Store(true)
	gate.SetAdmissionCheck(healthy.Load)
	if _, err := oe.PlaceOrder(&OrderRequest{Symbol: "BTCUSDT", Side: "BUY", Price: 100, Quantity: 1, PositionSide: "LONG", ClientOrderID: "live-risk-open"}); err != nil {
		t.Fatal(err)
	}
	if len(gate.Sources()) != 0 {
		t.Fatal("fixture installed a static hold before risk expiry")
	}
	healthy.Store(false)
	venue.ackOnly = true
	if err := oe.CancelOwnedOpeningOrders(t.Context()); err == nil {
		t.Fatal("cancel ACK without termination was accepted")
	}
	healthy.Store(true)
	if !gate.HasBlock(execution.UnverifiedCancellationBlock) {
		t.Fatal("health recovery erased unverified cancellation")
	}
	if _, err := gate.Begin(); !errors.Is(err, execution.ErrOpeningPaused) {
		t.Fatal("opening admitted before terminal verification")
	}
	venue.ackOnly = false
	if err := oe.CancelOwnedOpeningOrders(t.Context()); err != nil {
		t.Fatal(err)
	}
	if len(venue.cancelled) != 2 || gate.HasBlock(execution.UnverifiedCancellationBlock) {
		t.Fatal("verified cancellation failed to retire its own source")
	}
	release, err := gate.Begin()
	if err != nil {
		t.Fatal(err)
	}
	release()
}
