package execution

import (
	"errors"
	"testing"
)

func TestLiveAdmissionHoldInitializesZeroValueSources(t *testing.T) {
	var gate OpeningGate
	healthy := false
	gate.SetAdmissionCheck(func() bool { return healthy })
	if !gate.HoldIfBlocked(UnverifiedCancellationBlock) {
		t.Fatal("live risk failure did not install cancellation hold")
	}
	healthy = true
	if !gate.HasBlock(UnverifiedCancellationBlock) {
		t.Fatal("health recovery erased unverified cancellation")
	}
	if _, err := gate.Begin(); !errors.Is(err, ErrOpeningPaused) {
		t.Fatal("unverified residual cancellation allowed opening")
	}
	gate.Unblock(UnverifiedCancellationBlock)
	release, err := gate.Begin()
	if err != nil {
		t.Fatal(err)
	}
	release()
}
