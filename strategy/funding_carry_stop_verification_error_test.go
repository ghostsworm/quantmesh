package strategy

import (
	"errors"
	"fmt"
	"testing"
	"time"
)

func TestFundingCarryFinalVerificationErrorUsesActualStoppedCheckpoint(t *testing.T) {
	s, venue, _ := stoppedCloseCheckpointFixture(t)
	first := s.stopErr
	if !IsFundingCarryFinalVerificationPending(first) {
		t.Fatalf("actual stopped checkpoint was not classified: %v", first)
	}
	if s.StopContext(t.Context()) != first {
		t.Fatal("cached stop lost its original classified error")
	}
	venue.queryErr, venue.afterRead = nil, nil
	if err := s.ReconcileStoppedMarginClose(t.Context()); err != nil {
		t.Fatal(err)
	}
	if s.stopErr != nil || !s.stopCompleted || venue.repayCalls != 1 || len(venue.placedOrders) != 1 {
		t.Fatal("classification replayed finances or failed to retain verifier boundary")
	}
}

func TestFundingCarryFinalVerificationErrorRejectsMixedFailures(t *testing.T) {
	cause := errors.New("last liability query failed")
	pending := &fundingCarryFinalVerificationPendingError{cause: cause}
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"pending", pending, true},
		{"wrapped", fmt.Errorf("close: %w", pending), true},
		{"duplicate_manager_error", errors.Join(pending, fmt.Errorf("manager stop: %w", pending)), true},
		{"prepare_failure", errors.Join(pending, errors.New("prepare failed")), false},
		{"ownership_failure", fmt.Errorf("stop: %w", errors.Join(errors.New("owner lost"), pending)), false},
		{"nested_cleanup_failure", errors.Join(fmt.Errorf("close: %w", pending), errors.Join(pending, errors.New("other strategy failed"))), false},
		{"plain_cause", cause, false},
		{"same_message", errors.New(pending.Error()), false},
		{"empty_candidate", &fundingCarryFinalVerificationPendingError{}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsFundingCarryFinalVerificationPending(tc.err); got != tc.want {
				t.Fatalf("classification = %v, want %v", got, tc.want)
			}
		})
	}
	if !errors.Is(pending, cause) {
		t.Fatal("classification hid original cause")
	}
}

func TestFundingCarryFinalVerificationErrorRejectsIncompleteLocalEvidence(t *testing.T) {
	donor, _, _ := stoppedCloseCheckpointFixture(t)
	for _, mode := range []string{"marker", "ownership", "intent", "scope", "debt", "futures", "spot", "execution", "repay_intent", "cover_intent", "borrow_identity", "borrow_time", "wrong_borrow_identity", "wrong_borrow_time", "ledger", "cover_orders", "cover_remaining"} {
		t.Run(mode, func(t *testing.T) {
			s, _, _ := newFundingCarryRepayIntentFixture()
			s.direction, s.marginDebt, s.futQty, s.strategySpotQty = donor.direction, 0, 0, 0
			s.marginCloseVerificationPending = true
			s.strategySpotKnown, s.intentInFlight = true, true
			s.marginBorrowTransferID, s.marginBorrowedAt = donor.marginBorrowTransferID, donor.marginBorrowedAt
			s.marginDebtEvents = append([]fundingCarryMarginDebtEvent(nil), donor.marginDebtEvents...)
			s.marginCoverOrders = cloneFundingCarryCoverOrders(donor.marginCoverOrders)
			s.marginAccountScope = donor.marginAccountScope
			switch mode {
			case "marker":
				s.marginCloseVerificationPending = false
			case "ownership":
				s.strategySpotKnown = false
			case "intent":
				s.intentInFlight = false
			case "scope":
				s.marginAccountScope = ""
			case "debt":
				s.marginDebt = 0.00001
			case "futures":
				s.futQty = 0.00001
			case "spot":
				s.strategySpotQty = 0.00001
			case "execution":
				s.executionRecoveryRequired = true
			case "repay_intent":
				s.marginRepayIntent = &fundingCarryRepayIntent{}
			case "cover_intent":
				s.marginCoverIntent = &fundingCarryCoverIntent{}
			case "borrow_identity":
				s.marginBorrowTransferID = 0
			case "borrow_time":
				s.marginBorrowedAt = time.Time{}
			case "wrong_borrow_identity":
				s.marginBorrowTransferID++
			case "wrong_borrow_time":
				s.marginBorrowedAt = s.marginBorrowedAt.Add(time.Second)
			case "ledger":
				s.marginDebtEvents = nil
			case "cover_orders":
				s.marginCoverOrders = nil
			case "cover_remaining":
				s.marginCoverOrders[0].Consumed = 0
			}
			original := errors.New("stop failed")
			if got := s.classifyStoppedMarginCloseError(original); got != original || IsFundingCarryFinalVerificationPending(got) {
				t.Fatal("incomplete financial evidence became a readonly retry candidate")
			}
		})
	}
}
