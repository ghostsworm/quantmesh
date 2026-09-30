package storage

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
)

func fundingSpreadTestClaim(wallet int, amount, available float64) FundingSpreadCapitalClaim {
	return FundingSpreadCapitalClaim{WalletKey: fmt.Sprintf("%064x", wallet), Amount: amount, Available: available}
}

func TestFundingSpreadCapitalReservationsAreAtomicAcrossWallets(t *testing.T) {
	store, err := NewSQLStorage(filepath.Join(t.TempDir(), "capital-reservations.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()

	if err := store.ReserveFundingSpreadCapital(ctx, "spread-a", []FundingSpreadCapitalClaim{
		fundingSpreadTestClaim(1, 70, 100), fundingSpreadTestClaim(2, 70, 100),
	}); err != nil {
		t.Fatalf("reserve first two-wallet budget: %v", err)
	}
	if err := store.ReserveFundingSpreadCapital(ctx, "spread-b", []FundingSpreadCapitalClaim{
		fundingSpreadTestClaim(1, 40, 100), fundingSpreadTestClaim(2, 40, 100),
	}); err == nil {
		t.Fatal("overcommitted two-wallet reservation succeeded")
	}
	for _, wallet := range []int{1, 2} {
		if err := store.ReserveFundingSpreadCapital(ctx, fmt.Sprintf("spread-c-%d", wallet), []FundingSpreadCapitalClaim{fundingSpreadTestClaim(wallet, 30, 100)}); err != nil {
			t.Fatalf("failed two-wallet claim left a partial reservation on wallet %d: %v", wallet, err)
		}
	}
}

func TestFundingSpreadCapitalReservationsSerializeConcurrentBots(t *testing.T) {
	store, err := NewSQLStorage(filepath.Join(t.TempDir(), "capital-race.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	start := make(chan struct{})
	results := make(chan error, 2)
	var wg sync.WaitGroup
	for _, botID := range []string{"spread-a", "spread-b"} {
		wg.Add(1)
		go func(botID string) {
			defer wg.Done()
			<-start
			results <- store.ReserveFundingSpreadCapital(context.Background(), botID, []FundingSpreadCapitalClaim{fundingSpreadTestClaim(3, 60, 100)})
		}(botID)
	}
	close(start)
	wg.Wait()
	close(results)
	twins := 0
	for err := range results {
		if err == nil {
			twins++
		}
	}
	if twins != 1 {
		t.Fatalf("successful concurrent claims = %d, want exactly one", twins)
	}
}

func TestFundingSpreadCapitalReservationCannotShrinkUntilReleased(t *testing.T) {
	store, err := NewSQLStorage(filepath.Join(t.TempDir(), "capital-retained.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	ctx := context.Background()

	if err := store.ReserveFundingSpreadCapital(ctx, "spread-owner", []FundingSpreadCapitalClaim{fundingSpreadTestClaim(4, 80, 100)}); err != nil {
		t.Fatal(err)
	}
	if err := store.ReserveFundingSpreadCapital(ctx, "spread-owner", []FundingSpreadCapitalClaim{fundingSpreadTestClaim(4, 30, 10)}); err != nil {
		t.Fatalf("restart must preserve an existing reservation despite a lower free-balance snapshot: %v", err)
	}
	if err := store.ReserveFundingSpreadCapital(ctx, "spread-other", []FundingSpreadCapitalClaim{fundingSpreadTestClaim(4, 21, 100)}); err == nil {
		t.Fatal("another Bot was allowed to consume capacity still reserved by the owner")
	}
	if err := store.ReleaseFundingSpreadCapital(ctx, "spread-owner", []FundingSpreadCapitalClaim{fundingSpreadTestClaim(4, 0, 0)}); err != nil {
		t.Fatalf("release verified flat reservation: %v", err)
	}
	if err := store.ReserveFundingSpreadCapital(ctx, "spread-other", []FundingSpreadCapitalClaim{fundingSpreadTestClaim(4, 100, 100)}); err != nil {
		t.Fatalf("capacity was not released: %v", err)
	}
}
