package main

import (
	"context"
	"errors"
	"testing"
)

type startupReservationChecker func(context.Context, string) (bool, error)

func (f startupReservationChecker) HasAccountWalletCapitalReservation(ctx context.Context, botID string) (bool, error) {
	return f(ctx, botID)
}

func TestFundingCarryFailedStartupReservationAudit(t *testing.T) {
	startupCause := errors.New("original startup failure")
	readCause := errors.New("fixture storage unavailable")
	for _, tc := range []struct {
		name    string
		held    bool
		readErr error
		missing bool
		want    bool
	}{
		{name: "confirmed_no_claim"},
		{name: "remaining_claim", held: true, want: true},
		{name: "failed_read_is_unknown_not_empty", readErr: readCause, want: true},
		{name: "checker_unavailable", missing: true, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var checker startupReservationChecker
			if !tc.missing {
				checker = func(ctx context.Context, botID string) (bool, error) {
					if ctx.Err() != nil || botID != "fixture-bot" {
						t.Fatal("audit has canceled context or wrong identity")
					}
					if _, ok := ctx.Deadline(); !ok {
						t.Fatal("audit must have a deadline")
					}
					return tc.held, tc.readErr
				}
			}
			// Do not pass a typed-nil function as a non-nil interface.
			got := startupCause
			if tc.missing {
				got = auditFundingCarryFailedStartup(startupCause, "fixture-bot", nil)
			} else {
				got = auditFundingCarryFailedStartup(startupCause, "fixture-bot", checker)
			}
			var retained *fundingCarryStartupRetentionError
			if !errors.Is(got, startupCause) || errors.As(got, &retained) != tc.want {
				t.Fatalf("lost cause or incorrect reservation status: %v", got)
			}
			if tc.readErr != nil && !errors.Is(got, tc.readErr) {
				t.Fatal("audit read cause lost")
			}
			if !tc.want && got != startupCause {
				t.Fatal("confirmed empty read changed original failure")
			}
		})
	}
}

func TestFundingCarryFailedStartupAuditSkipsSuccessAndExistingRetention(t *testing.T) {
	checker := startupReservationChecker(func(context.Context, string) (bool, error) {
		t.Fatal("success or existing unresolved cleanup must not re-query")
		return false, nil
	})
	if auditFundingCarryFailedStartup(nil, "fixture-bot", checker) != nil {
		t.Fatal("success became failure")
	}
	retained := &fundingCarryStartupRetentionError{Cause: errors.New("already unresolved")}
	if auditFundingCarryFailedStartup(retained, "fixture-bot", checker) != retained {
		t.Fatal("existing diagnostic overwritten")
	}
}
