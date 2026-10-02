package order

import (
	"context"
	"errors"
	"testing"
	"time"

	"quantmesh/execution"
)

func TestCapitalReleaseIntentsLockWaitRespectsDeadline(t *testing.T) {
	oe, _, _ := newOwnedTestExecutor()
	if err := oe.ConfigureIntentJournal(t.Context(), &memoryIntentJournal{}, journalScope()); err != nil {
		t.Fatal(err)
	}
	oe.intentMu.Lock()
	oe.intents["unresolved"] = &ownedIntent{unknown: true}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- oe.VerifyCapitalReleaseIntents(ctx) }()
	var err error
	select {
	case err = <-done:
		oe.intentMu.Unlock()
	case <-time.After(time.Second):
		oe.intentMu.Unlock()
		err = <-done
		t.Error("intent proof remained blocked after request deadline")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("intent proof error = %v", err)
	}
	if err := oe.VerifyCapitalReleaseIntents(t.Context()); !errors.Is(err, execution.ErrOrderUnknown) {
		t.Fatalf("cancelled proof lost unresolved ownership: %v", err)
	}
	oe.intentMu.Lock()
	delete(oe.intents, "unresolved") // Test-only reconciliation; proof must never do this.
	oe.intentMu.Unlock()
	if err := oe.VerifyCapitalReleaseIntents(t.Context()); err != nil {
		t.Fatalf("legitimate retry failed: %v", err)
	}
}

func TestCapitalReleaseOwnerLockWaitRespectsDeadline(t *testing.T) {
	oe, venue, _ := newOwnedTestExecutor()
	if err := oe.ConfigureIntentJournal(t.Context(), &memoryIntentJournal{}, journalScope()); err != nil {
		t.Fatal(err)
	}
	oe.intentMu.Lock()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- oe.VerifyCapitalReleaseOwner(ctx, journalScope(), venue) }()
	var err error
	select {
	case err = <-done:
		oe.intentMu.Unlock()
	case <-time.After(time.Second):
		oe.intentMu.Unlock()
		err = <-done
		t.Error("owner proof remained blocked after request deadline")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("owner proof error = %v", err)
	}
	if err := oe.VerifyCapitalReleaseOwner(t.Context(), journalScope(), venue); err != nil {
		t.Fatalf("legitimate owner retry failed: %v", err)
	}
}

func TestCapitalReleaseProofRejectsMissingOrCancelledContext(t *testing.T) {
	oe, venue, _ := newOwnedTestExecutor()
	if err := oe.ConfigureIntentJournal(t.Context(), &memoryIntentJournal{}, journalScope()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, proof := range []func(context.Context) error{
		oe.VerifyCapitalReleaseIntents,
		func(ctx context.Context) error { return oe.VerifyCapitalReleaseOwner(ctx, journalScope(), venue) },
	} {
		if err := proof(nil); err == nil {
			t.Fatal("nil context accepted")
		}
		if err := proof(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled proof: %v", err)
		}
		if !oe.intentMu.TryLock() {
			t.Fatal("cancelled proof retained intent lock")
		}
		oe.intentMu.Unlock()
	}
}
